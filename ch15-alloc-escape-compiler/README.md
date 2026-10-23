# Ch15 · 分配器、逃逸分析与编译优化 ⭐

> Part 3 · 估时 18h · 状态：⚪ 未开始 · 搞清"一次分配到底花多少钱、谁决定它上不上堆"，把编译器当成可观察、可引导的伙伴

## 🎯 目标（资深标准）

- **L2**：画出 Go 的分配路径：`mallocgc` 分派到 tiny / small noscan / small scan / large 四条路，经过 mcache → mcentral → mheap 三层。讲清 span、size class、spanClass（scan / noscan）三个概念，以及 **1.27 新增的按尺寸特化 malloc** 改了什么。讲清 JVM 的 TLAB 指针碰撞和 C2 标量替换。
- **L2**：能读懂 `-gcflags='-m=2'` 输出的逃逸理由链，能说出至少 6 种常见逃逸原因；能讲清内联预算、PGO 去虚拟化、边界检查消除（BCE）分别在编译器的哪个阶段发生。
- **L3**：用 `benchstat` 加 `allocs/op` 证明一次"去逃逸"改造的收益；用 PGO 前后的数据说明内联和去虚拟化的效果；用 `-XX:+PrintCompilation` 和 JFR 解释一条 Java 预热曲线，用 AOT 缓存缩短它。
- **L4**：为游戏服热路径制定**分配预算**（每帧、每条消息允许多少次分配），并能在"Go 编译期静态优化"和"JVM 运行期投机优化"之间给出有数据支撑的取舍。

## 🔗 前置

- [Ch02](../ch02-values-memory/)：值语义、逃逸入门
- [Ch05](../ch05-interfaces/)：接口与动态派发
- [Ch06](../ch06-generics/)：泛型的字典实现
- [Ch10](../ch10-scheduler/)：g/m/p（mcache 挂在 P 上）

后续：[Ch16](../ch16-gc/) GC 会接着讲"分配如何推动 GC、mark assist 如何向分配者征税"；[Ch16](../ch16-zero-alloc-dod/) 把本章的结论落到零分配热路径上。

## 💡 关键洞察

1. **Go 在编译期优化，静态而保守；JVM 在运行期优化，投机而可回退。** Go 的逃逸分析一旦判定"可能逃逸"就上堆，运行期没有第二次机会；PGO 也只是拿上一次的 profile 去引导下一次编译。C2 则基于本次运行的 profile 做激进的内联和标量替换，假设失效就去优化（deopt）。所以写 Go 要"写出编译器看得懂的代码"，写 Java 要"让 JIT 看到稳定的 profile"。
2. **Go 的堆分配代价不只在 `mallocgc` 本身。** 每分配一个字节都在推进下一次 GC，分配者还可能被征收 mark assist（[Ch16](../ch16-gc/)）。所以在 Go 里，"减少分配"是一等的性能手段。Java 的 TLAB 分配几乎免费，代价主要在对象晋升和老年代回收上。
3. **1.27 的按尺寸特化 malloc 让小对象分配更便宜**（<80B 的对象最多便宜约 30%，分配密集型程序整体约提升 1%），但它不改变"该不该分配"这个判断：逃逸分析仍然是第一道闸。

## ☕ Java 25 深度复习

### 分配：TLAB + 指针碰撞
- 每个线程从 Eden 区领一块私有的 **TLAB**（Thread-Local Allocation Buffer），在里面分配时只需移动一个指针（bump pointer），快路径只有几条指令，不需要加锁。
- TLAB 用完时，要么领一块新的（refill），要么在 Eden 共享区用 CAS 分配，要么触发 GC。`-XX:+UseTLAB` 和 `-XX:+ResizeTLAB` 默认都开着（已在本机用 `PrintFlagsFinal` 确认）。
- 大对象：G1 下，大小 ≥ region 一半的对象是 **humongous 对象**，直接分配到专门的 region（本机默认 `G1HeapRegionSize` 为 4MB）。
- 每个对象都有对象头：默认 12 字节（开启压缩类指针时），开启 **JEP 519 紧凑对象头**（`-XX:+UseCompactObjectHeaders`，JDK 25 已正式，但需显式开启）后是 8 字节，见 [Ch02](../ch02-values-memory/)。
- 观测：JFR 事件 `jdk.ObjectAllocationSample`，`jfr view allocation-by-site` / `allocation-by-class`。

### JIT：分层编译与投机
- **分层编译**：解释执行（0 层）→ C1（1–3 层，3 层带完整 profiling）→ C2（4 层）。`-XX:TieredStopAtLevel` 默认是 4，`-XX:+PrintCompilation` 能看到每个方法的编译事件和层级。
- **内联启发式**（本机 JDK 25.0.2 实测默认值）：
  - `MaxInlineSize=35`：非热点调用，被调方字节码不超过 35 字节才内联；
  - `FreqInlineSize=325`：热点调用的上限；
  - `InlineSmallCode=2500`：被调方已编译代码的大小上限。
  - 诊断开关：`-XX:+UnlockDiagnosticVMOptions -XX:+PrintInlining`。
- **基于类型 profile 的投机**：调用点是单态或双态时，内联缓存直接内联目标方法；超多态时退化为 vtable/itable 调用。投机失败触发 uncommon trap → **deopt**，退回解释执行，之后重新编译。用 `jfr view deoptimizations-by-reason` 查看去优化原因。
- **C2 逃逸分析 + 标量替换**（`DoEscapeAnalysis` 和 `EliminateAllocations` 默认开）：对象在内联后的编译单元内不逃逸，就把它**拆成若干标量**放进寄存器或栈槽，并消除相关的锁。局限：
  - 效果受内联边界限制，对象一旦作为参数传给未内联的方法就算逃逸；
  - 控制流合并处的对象（phi 节点）常常导致失败；
  - 长度不是常量的数组通常无法替换。
  - 实现在 HotSpot 的 `src/hotspot/share/opto/` 目录（C2）中。

### 启动与预热：Leyden 系列 JEP
- **JEP 483**（AOT 类加载与链接）+ **JEP 514**（命令行简化）：
  - 训练和生成一步完成：`java -XX:AOTCacheOutput=app.aot -cp out App`；
  - 生产启动：`java -XX:AOTCache=app.aot -cp out App`。
  - 本机实测：示例程序生成约 9.5MB 的缓存，`-Xlog:aot` 可以看到 `Opened AOT cache`。
  - 也可以分两步：`-XX:AOTMode=record -XX:AOTConfiguration=...`，再 `-XX:AOTMode=create`。
- **JEP 515**（AOT 方法 profiling）：训练运行收集到的方法 profile 也写进缓存，生产运行时 JIT 一开始就有 profile 可用，缩短到达峰值性能的时间。
- **前提**：训练和生产用同一个 JDK、同一条类路径。**源码启动模式（`bin/jrun`）不适用**，实验要先 `javac` 预编译。
- **StableValue（JEP 502，preview）**：`StableValue.of()` 加 `orElseSet(...)` 至多设置一次，设置后 JIT 可以像对待受信任的 `final` 字段一样做常量折叠。对照 Go：编译器没有运行期常量折叠，`sync.OnceValue` 只是一个正确性工具（[Ch12](../ch12-sync-memory-model/)）。

### 源码与工具位置
- HotSpot 目录：`src/hotspot/share/opto/`（C2）、`src/hotspot/share/c1/`（C1）、`src/hotspot/share/compiler/`（编译调度）、`src/hotspot/share/gc/shared/`（TLAB 等 GC 公共设施）、`src/hotspot/share/cds/`（CDS / AOT 缓存）。
- 本机核对默认值：`java -XX:+UnlockDiagnosticVMOptions -XX:+PrintFlagsFinal -version | grep -E 'Inline|EscapeAnalysis|TLAB|AOT'`。

## 🐹 Go 1.27 深入

### 规范层
- 规范**不谈栈和堆**，只保证变量至少活到不再被引用为止；放栈上还是堆上是实现的自由（Go FAQ："How do I know whether a variable is allocated on the heap or the stack?"）。所以：**取地址 ≠ 上堆，`new` ≠ 上堆**。
- 闭包按引用捕获变量。把一个非指针形状的值装进接口通常需要分配（编译器对零值、小整数、常量等有优化），这正是"接口装箱"这类隐形分配的来源。

### 运行时层：分配器
- **三层结构**：
  - `mcache`：每个 P 一个，访问不加锁；
  - `mcentral`：每个 spanClass 一个，有锁；
  - `mheap`：全局，以页为单位管理内存。
  - `mspan` 是一段连续的页，被切成同样大小的对象槽。
- **size class**：表在 `internal/runtime/gc/sizeclasses.go`（`MaxSmallSize = 32768`，`TinySize = 16`）。`spanClass` 由 size class 加 scan / noscan 一位组成：**不含指针的对象放进 noscan span，GC 完全不用扫描它们**。
- **`mallocgc` 的分派**（`runtime/malloc.go`）：
  - `mallocgcTiny`：小于 16B 且不含指针，多个小对象合并进同一个 16B 块；
  - `mallocgcSmallNoscan`：不含指针的小对象；
  - `mallocgcSmallScanNoHeader`：含指针且较小，指针位图放在 span 里；
  - `mallocgcSmallScanHeader`：含指针且较大，对象头里存类型指针；
  - `mallocgcLarge`：大于 32KB，直接向 mheap 要一个专属 span。
- **快路径与慢路径**：快路径是 `mcache.nextFree`，用 span 的 allocCache 位图找空闲槽。span 用完后走 `mcache.refill` → `mcentral.cacheSpan` → `mheap.alloc`，越往下锁越重。
- **1.27 新增：按尺寸特化 malloc**：
  - 编译器对大小已知的分配，直接调用 `runtime/malloc_generated.go` 里生成的特化函数（如 `mallocgcSmallScanNoHeaderSC1`、`SC2`…）。size class 作为常量编进代码，省掉了查表和分支。
  - 生成器在 `runtime/_mkmalloc`，桩函数在 `malloc_stubs.go`。
  - 代价是二进制大约多 60KB。用 `GOEXPERIMENT=nosizespecializedmalloc` 可以关闭，这个开关预计在 1.28 移除。
- **分配与 GC 的耦合**：`malloc_stubs.go` 里的 `deductAssistCredit` 在分配时扣减 assist 额度，欠账就进入 `gcAssistAlloc`（[Ch16](../ch16-gc/)）。
- **heap profile 的来源**：按 `runtime.MemProfileRate` 采样（默认平均每分配 512KB 采一个样本）。

### 编译器层
- **逃逸分析**（`cmd/compile/internal/escape/`）：
  - 建图：顶点叫"位置"（location），代表变量和隐式分配；边代表赋值，带解引用次数作为权重（`graph.go`）。
  - 求解：遍历图，找出地址被存进堆、或会比对象本身活得更久的路径（`solve.go`）。
  - 泄漏标签：参数"泄漏到返回值 / 堆"的标签写进导出数据，跨包调用时也能用（`leaks.go`）。
  - 文件头注释写明了两条不变量：栈对象的指针不能存进堆；栈对象的指针不能比对象活得更久。
- **常见逃逸原因**（每条都要能用 `-m=2` 复现）：
  1. 返回局部变量的地址；
  2. 值被赋给接口并逃出函数，例如 `fmt.Println(x)` 会报 `x escapes to heap`；
  3. 闭包捕获了变量，而闭包本身逃逸（存进结构体、作为 goroutine 启动）；
  4. 指针被存进逃逸的 slice、map 元素，或经 channel 发送出去；
  5. 对象超过栈上限：显式变量 `MaxStackVarSize = 128KB`，隐式分配（`new`、`make`、复合字面量）`MaxImplicitStackVarSize = 64KB`（`cmd/compile/internal/ir/cfg.go`）；
  6. 大小不是常量的 `make([]T, n)`：编译器会对一部分这类分配尝试栈分配（`-gcflags=all=-d=variablemakehash=n` 可以关掉），结果以 `-m` 输出为准；
  7. 通过接口调用方法，编译器无法证明被调方不保留参数。
- **内联**（`cmd/compile/internal/inline/inl.go`）：
  - 预算 `inlineMaxBudget = 80`（以 IR 节点成本计）；超出时 `-m=2` 打印 `function too complex: cost N exceeds budget 80`；
  - 支持中间栈内联，即非叶子函数也可以内联；
  - PGO 识别出的热点调用边，预算提升到 `inlineHotMaxBudget = 2000`；
  - 用 `//go:noinline` 禁止内联（基准实验里常用）。
- **去虚拟化**（`cmd/compile/internal/devirtualize/`）：
  - 静态：能证明接口变量的具体类型时，直接改成静态调用；
  - PGO（`pgo.go`）：对热点接口调用点插入"类型判断 + 直接调用（可内联）+ 兜底的接口调用"。`-m=2` 下能看到 `PGO devirtualize considering call ...` 这类诊断。
- **PGO 流程**：
  1. 采一份生产或压测的 CPU pprof；
  2. 放到主包目录，命名为 `default.pgo`；
  3. `go build` 默认带 `-pgo=auto`，会自动使用它。
  - 采集流程见 [Ch17](../ch17-profiling-bench/)。
- **BCE**：SSA 的 prove 阶段（`cmd/compile/internal/ssa/prove.go`）证明下标在界内后，删掉边界检查。`-gcflags='-d=ssa/check_bce/debug=1'` 打印剩下的边界检查（对应 `ssa/compile.go` 里的 `"check bce"` 阶段）。常见技巧：在循环前先写一句 `_ = s[n-1]` 断言长度。
- **闭包命名（1.27 新增）**：函数字面量的名字更简单，且与是否内联无关，多个内联实例可能共享同一份代码。影响：pprof 里闭包的符号名会变；按符号名匹配的测试和告警规则需要更新；错误地比较函数代码指针的代码，会更常看到"相等"。

### 源码阅读清单
| 文件 → 函数/类型 | 带着什么问题读 |
|---|---|
| `runtime/malloc.go` → `mallocgc`、`mallocgcTiny`、`mallocgcSmallNoscan`、`mallocgcSmallScanNoHeader`、`mallocgcSmallScanHeader`、`mallocgcLarge` | 为什么 tiny 分配器只服务不含指针的对象？small scan 为什么要分"带头 / 不带头"两种？ |
| `runtime/malloc_generated.go` · `runtime/malloc_stubs.go` · `runtime/_mkmalloc/` | 特化版本相比通用路径省掉了哪些分支和查表？`deductAssistCredit` 放在哪一步？ |
| `runtime/mcache.go` → `mcache`、`refill`；`runtime/malloc.go` → `nextFree`；`runtime/mcentral.go` → `cacheSpan` | mcache 为什么可以不加锁？一个 span 用完后，要经过几层、拿哪些锁？ |
| `runtime/mheap.go` → `mheap`、`mspan`、`spanClass` | spanClass 怎样编码 noscan？内存是怎样还给操作系统的（scavenger）？ |
| `internal/runtime/gc/sizeclasses.go` | size class 怎样在内部碎片和 class 数量之间权衡（看表里的 waste 列）？ |
| `cmd/compile/internal/escape/escape.go`（文件头注释）· `graph.go` · `solve.go` · `leaks.go` | "参数泄漏到返回值"这种标签，怎样让调用方免于分配？ |
| `cmd/compile/internal/inline/inl.go` → `inlineMaxBudget`、`inlineHotMaxBudget` | 内联成本怎么计算？PGO 怎样改变预算？ |
| `cmd/compile/internal/devirtualize/pgo.go` | PGO 去虚拟化生成的代码长什么样？猜错类型时代价有多大？ |
| `cmd/compile/internal/ssa/prove.go` | prove 能证明哪些下标模式？哪些写法会让它失败？ |

## 📊 Java 25 ↔ Go 1.27 机制级对照表

| 维度 | Java 25 | Go 1.27 | 取舍 / 游戏服影响 |
|---|---|---|---|
| 分配快路径 | TLAB 指针碰撞，几条指令 | mcache 中按 size class 用位图找空槽，1.27 起有按尺寸特化版本 | Java 单次分配更便宜；Go 的对象不移动（无压缩），换来不需要读屏障 |
| 线程本地缓存 | 每个线程一块 TLAB | 每个 P 一个 mcache | 两边快路径都无锁；Go 的缓存数量跟 GOMAXPROCS 走，不跟 goroutine 数走 |
| 大对象 | G1 humongous region | >32KB 走 `mallocgcLarge`，独占一个 span | 大缓冲区两边都应复用，不要每次重新分配 |
| 逃逸分析时机 | 运行期（C2），依赖内联和 profile | 编译期，保守、确定，`-m` 可见 | Go 结果可预测、可在 CI 守住；Java 上限更高，但会随 profile 波动 |
| "不上堆"的形式 | 标量替换：把对象拆散成标量 | 真正的栈分配：整个对象放在栈帧里，栈可增长、可拷贝 | Go 能把整块 struct、数组放栈上；Java 的对象一旦被整体使用就无法替换 |
| 内联决策 | 运行期按热度和字节码大小（35/325），可以内联虚调用 | 编译期按 IR 成本预算（80），PGO 热点放宽到 2000 | 热路径函数在 Go 里要刻意控制体积，冷路径要拆出去 |
| 去虚拟化 | CHA + 内联缓存 + 投机，失败就 deopt | 静态证明 + PGO 条件分支 | 超多态分发（几百个协议 handler）两边都无法内联，应该改用表驱动分派 |
| profile 来源 | 本次运行在线采集；JEP 515 可以从训练运行带入 | 上一次生产运行的 pprof（`default.pgo`） | Go 需要把"刷新 profile"做进 CI；Java 需要训练流程 |
| 预热 | 有：解释 → C1 → C2，达到峰值需要时间；AOT 缓存可以缓解 | 没有：一启动就是最终机器码 | 滚动发布、弹性扩容后的第一分钟，Go 没有冷启动抖动 |
| 对象头 | 12B（开启 JEP 519 后 8B） | 无（类型信息在 span 元数据或接口里） | 大量小对象场景（实体组件、坐标）Go 内存占用更低 |
| 可观测性 | `PrintCompilation`、`PrintInlining`（诊断）、JFR 编译与去优化视图 | `-gcflags=-m=2`、`-d=ssa/check_bce/debug=1`、pprof | Go 的决策在构建时就能看到并做门禁；Java 要在运行时观察 |

## 🔬 实验

### 实验 1：逃逸 vs 不逃逸 vs 接口装箱（必做）
- **目的**：量化一次堆分配的完整代价，并验证 1.27 特化 malloc 的收益。
- **方法**（`go/lab/escape/`）：同一个 `Vec3` 运算写三个版本：
  - A：返回 `*Vec3`；
  - B：返回 `Vec3` 值；
  - C：经由 `interface{ Len() float32 }` 调用。
- **命令**：
  - `go build -gcflags='-m=2' ./ch15-alloc-escape-compiler/go/lab/escape 2>&1 | grep -E 'escape|inline'`；
  - `go test -bench=. -benchmem -count=10 ./ch15-alloc-escape-compiler/go/lab/escape > new.txt`；
  - 再用 `GOEXPERIMENT=nosizespecializedmalloc` 跑一遍 A，输出存为 `old.txt`，然后 `benchstat old.txt new.txt`。
- **预期现象**：A、C 是 1 allocs/op，B 是 0；B 的 ns/op 明显更低；关闭特化 malloc 后 A 稍慢。
- **记录**：逃逸理由链原文、三个版本的 ns/op / B/op / allocs/op、benchstat 的差异和 p 值。

### 实验 2：PGO 前后
- **目的**：观察 PGO 驱动的内联和去虚拟化，并量化收益。
- **方法**（`go/lab/pgo/`）：一个按协议 ID 分发的消息处理器，handler 是接口，有 8 种实现，其中一种占 90% 流量。
  1. 不带 profile 构建并压测；
  2. 采 30 秒 CPU profile，存为 `default.pgo`；
  3. 重新构建，再压测。
- **命令**：`go test -bench=. -cpuprofile=cpu.pprof`，然后 `cp cpu.pprof default.pgo`；用 `go build -gcflags='-m=2'` 对比前后的内联和 `PGO devirtualize` 诊断；最后 benchstat。
- **预期现象**：热点 handler 被条件去虚拟化并内联，吞吐提升；二进制略变大。
- **记录**：吞吐变化、二进制大小变化、诊断行的差异。

### 实验 3：Java 预热曲线与 AOT 缓存
- **目的**：看清 JIT 的分层过程，量化 JEP 514/515 对启动和达峰时间的影响。
- **方法**（`java/lab/WarmupCurve.java`，**先 `javac -d out` 预编译**，源码启动模式不适用 AOT 缓存）：把同一批消息处理分成 200 批，每批打印耗时。分别跑：
  1. 默认参数；
  2. `-XX:TieredStopAtLevel=1`（只用 C1）；
  3. 先用 `-XX:AOTCacheOutput=app.aot` 训练，再用 `-XX:AOTCache=app.aot` 启动。
- **工具**：`-XX:+PrintCompilation`（看热点方法从 3 层升到 4 层的时间点），JFR 的 `jfr view longest-compilations` 和 `jfr view deoptimizations-by-reason`。
- **预期现象**：
  - 默认参数下，前若干批明显慢，C2 编译完成后进入稳态；
  - 只用 C1 时稳态更慢；
  - AOT 缓存缩短了启动时间和到达稳态所需的批次。
- **记录**：JVM 启动耗时、第 1 批耗时、到达稳态的批次号、稳态耗时。

### 实验 4（选做）：两边"消除分配"的边界
- **方法**：同一个短生命周期对象（如 `Damage{src, dst, value}`）分别在 Go 和 Java 中构造、使用、丢弃。Java 侧用 `-XX:-EliminateAllocations` 做对照；两边都加入"把对象存进字段 / 传给未内联方法"的变体。
- **预期现象**：两边"不分配"的条件都会被同一类操作破坏（逃出作用域、越过内联边界），但 Go 的结果在编译期就能确定，而 Java 的结果依赖预热程度。

## 🧪 练习规划

**Go：`go/exercises`**
1. `type Vec3 struct{ X, Y, Z float32 }`，实现 `func (v Vec3) Add(o Vec3) Vec3`、`func (v Vec3) Scale(k float32) Vec3`、`func Dist2(a, b Vec3) float32`。
   - 测试用 `testing.AllocsPerRun` 断言 0 次分配；
   - `go build -gcflags=-m` 应显示三个函数都 `can inline`（验收时人工核对）。
2. `func AppendDamageLog(dst []byte, attacker, target uint64, dmg int32) []byte`：append 风格格式化，对照 `fmt.Sprintf`。
   - 当 `dst` 容量足够时，`AllocsPerRun` 为 0；
   - 附 `for b.Loop()` 基准，与 Sprintf 版本做 benchstat 对比。
3. `type Dispatcher struct{...}`，实现 `Register(id uint16, h Handler)` 和 `Dispatch(m *Msg) error`。
   - 要求用按 ID 索引的切片代替 `map[uint16]Handler`，并避免闭包逃逸；
   - 基准断言 0 allocs/op；
   - 测试覆盖未注册 ID 的错误路径。
4. `func SumHP(es []Entity) int64` 和 `func ApplyDamage(hp []int32, idx []int32, dmg int32)`：用 `-d=ssa/check_bce/debug=1` 把循环内的边界检查消到 0，测试验证结果正确，基准对比优化前后。

**Java：`java/drills`**
1. `EscapeDrill.java`：用 `com.sun.management.ThreadMXBean.getCurrentThreadAllocatedBytes()` 测量循环充分预热后的分配字节数。
   - 版本 A（对象不逃逸）应接近 0 字节；
   - 版本 B（对象存入字段而逃逸）应明显大于 0；
   - 自校验两者的量级关系，并在注释里解释 C2 标量替换为什么对 A 生效、对 B 不生效。
2. `StableConfigDrill.java`（JEP 502 preview）：用 `StableValue` 实现一个"至多初始化一次"的配置持有者，16 个虚拟线程并发读取；自校验初始化函数只执行了 1 次。

## 🎮 游戏服落地

1. **战斗帧热路径的分配预算**：伤害计算、Buff 结算、位置更新，每帧要处理上万个实体。
   - Go：值类型的 `Vec3`、预分配的 slice、append 风格的日志格式化，可以做到每帧 0 次分配，再用基准测试在 CI 里守住；
   - Java：依赖 C2 标量替换和对象复用，上限高，但要通过 JFR `allocation-by-site` 持续盯着；
   - 取舍：Go 赢在可预测，Java 赢在 JIT 上限。
2. **协议分发层**：几百个协议 handler 是典型的超多态调用点。两边都应该改成"ID → 函数表"的表驱动分派。Go 在此基础上可以用 PGO 针对少数热点协议做条件去虚拟化。
3. **滚动发布和弹性扩容后的第一分钟**：Java 冷启动加 JIT 预热，会让首批玩家请求的 p99 飙高，对策是 AOT 缓存（JEP 514/515）加预热流量。Go 没有这个问题，但 PGO profile 要定期从生产环境刷新（做进发布流水线）。
4. **日志与埋点**：`fmt.Sprintf` 和把值传给 `any` 参数，是热路径上隐形分配的大户。改用 `slog` 的强类型 `Attr`（[Ch08](../ch08-errors-panic/)）或 append 风格的编码。

## ✅ 验收答辩题

- [ ] [L2] 画出 `mallocgc` 处理 8B 无指针对象、48B 含指针对象、1KB 含指针对象、64KB 对象时，分别走哪条路径、经过 mcache/mcentral/mheap 的哪几层。
- [ ] [L2] noscan span 和 scan span 为什么要分开？对 GC 有什么好处？
- [ ] [L2] 1.27 的按尺寸特化 malloc 省掉了什么？为什么单次小对象分配最多便宜约 30%，整体收益却只有约 1%？
- [ ] [L2] 说出 6 种 Go 常见逃逸原因，每种给一行能复现的代码。
- [ ] [L2] C2 标量替换和 Go 栈分配的本质区别是什么？各自在什么情况下失败？
- [ ] [L2] PGO 去虚拟化生成的代码是什么结构？它和 JIT 的内联缓存有哪些异同？
- [ ] [L3] 给你一段 `-m=2` 输出，指出逃逸链上的每一步，给出改造方案，并用 benchstat 证明收益。
- [ ] [L3] 一个热点函数成本 95、超出预算无法内联，你有哪些手段（拆出冷路径、PGO……）？如何验证改造确实生效？
- [ ] [L3] 用 `PrintCompilation` 和 JFR 解释一条 Java 预热曲线：哪段是解释执行、哪段是 C1、什么时候进入 C2、有没有发生 deopt。
- [ ] [L3] AOT 缓存（JEP 483/514/515）需要什么前提？哪些情况下会失效或被拒绝使用？
- [ ] [L3] 线上 Go 服务的 allocs/op 看起来不高，但 GC 的 CPU 占比很高，你如何用 heap profile 的 `alloc_space` 和 `inuse_space` 定位？
- [ ] [L4] 为一个承载 1 万个并发房间的战斗服，制定分配预算和 CI 门禁（基准回归检测、`-m` 输出比对）。
- [ ] [L4] 同一个协议分发层，Go（PGO）和 Java（JIT + AOT 缓存）各自的最佳实践和上限是什么？你会怎么选？

## 🕳️ 常见坑

- **以为"取地址就上堆、`new` 就上堆"**：看 `-m` 的输出，不要猜。
- **以为 Go 程序会像 JIT 一样"跑久了自动变快"**：不会。代码在编译期就定型了，PGO 影响的是下一次编译。
- **热路径上的隐形分配**：`fmt.Sprintf`、`errors.New`、字符串拼接、把值传给 `any` 参数。
- **为了"避免拷贝"把所有东西都改成指针**：指针让对象逃逸，还增加 GC 的扫描量。几十字节的小 struct 传值往往更快。
- **微基准被编译器优化掉**：Go 用 `for b.Loop()`（循环体内调用的参数和结果会被保持存活）；Java 用 JMH 的 `Blackhole`（[Ch17](../ch17-profiling-bench/)）。
- **看到 `inlining call` 就以为没有分配**：内联之后对象仍然可能逃逸，要结合 `-benchmem` 的数据看。
- **把 PGO 当一次性工作**：代码演进后旧 profile 会失配，收益逐渐衰减，需要定期刷新。
- **Java 侧在 JMH 之外用 `System.nanoTime` 测冷代码**，或者忘了用 `-XX:-EliminateAllocations` 做对照，得出"Java 分配免费"的错误结论。
- **把 AOT 缓存（JEP 483/514/515）和 GraalVM native-image 混为一谈**：前者仍然是 HotSpot 加 JIT，只是把类加载、链接和 profile 提前做好。

## 📚 一手资料

- Go：
  - [Profile-guided optimization](https://go.dev/doc/pgo)
  - [Go FAQ：变量分配在堆上还是栈上](https://go.dev/doc/faq#stack_or_heap)
  - [Go 1.27 Release Notes](https://go.dev/doc/go1.27)（Runtime 一节的 Faster memory allocation、Compiler 一节的闭包命名）
  - 源码注释：`src/runtime/malloc.go` 文件头（分配器总览）、`src/cmd/compile/internal/escape/escape.go` 文件头（逃逸分析总览）
  - `go doc cmd/compile`（`-m`、`-d` 等调试选项）
- Java：
  - [JEP 483](https://openjdk.org/jeps/483)、[JEP 514](https://openjdk.org/jeps/514)、[JEP 515](https://openjdk.org/jeps/515)、[JEP 502](https://openjdk.org/jeps/502)、[JEP 519](https://openjdk.org/jeps/519)
  - HotSpot 源码：`src/hotspot/share/opto/`、`c1/`、`cds/`
  - Aleksey Shipilëv，JVM Anatomy Quarks（TLAB 分配、标量替换相关篇目）
