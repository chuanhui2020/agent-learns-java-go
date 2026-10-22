# Ch16 · 零分配热路径与数据导向设计 ⭐

> Part 3 · 估时 16h · 状态：⚪ 未开始 · 把前三章的原理落到代码上：热路径零分配、数据按访问模式布局、向量化批处理，Java 侧对照 FFM 与 Vector API

## 🎯 目标（资深标准）

- **L1**：熟练使用 append 风格 API、分级 `sync.Pool`、预分配、共享不可变 buffer，把一条广播热路径从 N allocs/op 降到 0。
- **L2**：讲清 `sync.Pool` 在什么情况下有效、什么情况下有害（实现见 [Ch09](../ch09-sync-memory-model/)）；讲清 `unsafe.String`、`StringData`、`Slice`、`SliceData` 的合法边界；讲清 AoS 和 SoA 对缓存行、预取、GC 扫描的影响。讲清 **1.27 新增的 `simd` 包**（`GOEXPERIMENT=simd`）与 `simd/archsimd` 的分工。
- **L3**：用 benchstat、`-benchmem` 和 Linux 上的 `perf stat`（缓存未命中）证明一次布局改造或向量化的收益；能定位并修复"池化导致的内存膨胀"。
- **L4**：在可读性、可维护性和极致性能之间划线：哪些路径值得做数据导向设计和 SIMD，哪些不值得；Go（实验性的 simd）和 Java（孵化中的 Vector API、FFM 堆外内存）各自的生产风险。

## 🔗 前置

- [Ch13](../ch13-alloc-escape-compiler/)：逃逸分析、BCE
- [Ch14](../ch14-gc/)：noscan 内存、GC 扫描成本
- [Ch09](../ch09-sync-memory-model/)：sync.Pool 的实现、伪共享
- [Ch15](../ch15-profiling-bench/)：基准方法论

## 💡 关键洞察

1. **零分配不是目的，可预测才是。** 战斗帧、广播这类每秒执行百万次的路径，每次分配都会转化为 GC 频率和 assist 税。冷路径上为了零分配牺牲可读性是负收益。先用 profile 找到"每秒执行最多的那 5% 代码"再动手。
2. **数据布局决定性能上限。** 一个按 `[]*Entity` 组织的世界，每访问一个实体就是一次指针跳转和潜在的缓存未命中，GC 还要扫描每一个指针。按访问模式组织成 SoA（结构的数组）后，遍历是顺序的、可预取、可向量化，而且不含指针（noscan）。这一点 Go 比 Java 25 有天然优势：`[]struct` 是连续的值，而 Java 的对象数组是引用数组（Valhalla 不在 Java 25 中）。
3. **共享不可变数据是零拷贝的前提。** Go 里"编码一次，把同一个 `[]byte` 发给 100 个玩家"是零成本的，GC 会管理它的生命周期，代价是发布之后**谁都不能再修改它**。Netty 用引用计数实现同样的效果，代价是要手动 `retain` / `release`，以及泄漏风险。

## ☕ Java 25 深度复习

### 对象池在现代 GC 下还值得吗
- 小对象：TLAB 分配加上年轻代回收几乎免费；对象池反而让对象变成长寿对象，被晋升到老年代后，增加卡表维护和老年代回收的工作量。**一般不值得**。
- 值得池化的：大块缓冲区（尤其是直接内存）、初始化很昂贵的对象（编解码器状态、加密上下文）、需要和本地代码交互的内存。
- 逃逸分析和标量替换（[Ch13](../ch13-alloc-escape-compiler/)）能消除一部分临时对象，但前提是内联成功。

### Netty 的池化 ByteBuf（第三方，对照用）
- `PooledByteBufAllocator`：按 arena 分片，带线程本地缓存，按大小分级（与 jemalloc 的思路相似）。
- 引用计数：`retain()`、`release()`、`retainedDuplicate()`。广播场景下"编码一次、多次发送"依赖引用计数，泄漏要靠 `ResourceLeakDetector` 发现。

### FFM API（JEP 454，正式）：堆外内存
- 核心类型：`Arena`（`ofConfined`、`ofShared`、`ofAuto`、`global`）管理生命周期；`MemorySegment` 是带边界检查和生命周期检查的内存视图；`ValueLayout`、`MemoryLayout.structLayout` 描述布局；通过 `VarHandle` 或 `getAtIndex` / `setAtIndex` 访问。
- 适合：大块的实体数据（不受 GC 扫描、不受堆大小影响）、与本地库零拷贝交互（[Ch24](../ch24-runtime-boundaries/)）、内存映射文件。
- 关闭 `Arena` 之后再访问它分配的段会抛异常：比 `Unsafe` 安全，但仍然是手动管理生命周期。
- 源码：`java.base/java/lang/foreign/{Arena,MemorySegment,Linker}.java`。

### Unsafe 的退场（JEP 471 / 498）
- `sun.misc.Unsafe` 的内存访问方法已被标记为**弃用并计划移除**，首次使用时默认打印警告。可以用 `--sun-misc-unsafe-memory-access=allow|warn|debug|deny` 控制（本机已确认该参数可用）。
- 替代方案：堆内内存用 `VarHandle`，堆外内存用 FFM。
- 依赖 Unsafe 的老库（部分序列化和网络库）升级到 JDK 25 时，要检查启动日志里的警告。

### Vector API（JEP 508，第十次孵化）
- 运行需要 `--add-modules jdk.incubator.vector`（用 `bin/jrun` 时把参数放在文件名前），启动时会打印孵化模块警告。
- 核心用法：
  - 选择向量形状：`FloatVector.SPECIES_PREFERRED`；本机（AVX2）实测 8 个 float 通道；
  - 加载：`FloatVector.fromArray(S, a, i)`；
  - 运算：`fma`（乘加）、`compare` 生成掩码；
  - 归约：`reduceLanes(VectorOperators.ADD)`；
  - 尾部处理：`S.loopBound(n)` 之后用掩码或标量收尾。
- 长期处于孵化状态，在等 Valhalla 的值类型。**生产环境使用要承担 API 变动的风险**。

### 对象布局对照
- `Entity[]` 是引用数组：遍历时每个元素都要跳转一次指针，对象分散在堆里。
- 要做 SoA 只能手工拆成多个原始类型数组（`float[] xs, ys, zs`），或者用 FFM 的段。
- record 仍然是引用类型，享受不到连续布局。

## 🐹 Go 1.27 深入

### 规范层
- **`unsafe` 包规则**（`unsafe/unsafe.go` 文档）：
  - `unsafe.String(ptr, len)` 和 `unsafe.StringData(s)`：从字节构造字符串之后，**底层字节在字符串存活期间绝不能被修改**；
  - `unsafe.Slice(ptr, len)` 和 `unsafe.SliceData(s)`：同样要求不越界、对象存活；
  - `unsafe.Pointer` 的 6 种合法使用模式之外的转换都是未定义行为，GC 可能看不到这些指针。
- **编译器已经帮你免掉的拷贝**（不需要 unsafe，用 `AllocsPerRun` 验证）：`m[string(b)]` 形式的 map 查找、`string(b) == "..."` 比较、`for i, c := range []byte(s)`。
- **go fix 现代化器**：`unsafefuncs`（**1.27 新增**）把 `unsafe.Pointer(uintptr(p) + uintptr(n))` 这类指针运算改写成 `unsafe.Add(p, n)`。在已有的 unsafe 代码上跑 `go fix -diff ./...` 检查一遍。

### 运行时层
- **`sync.Pool` 的使用要点**（实现细节见 [Ch09](../ch09-sync-memory-model/)：每个 P 有私有槽和共享链，还有 victim 缓存，GC 时会清空）：
  1. 放**指针**：`Put(&buf)`，用 `*[]byte`。直接放 `[]byte` 会把 slice header 装箱进 `any`，每次 Put 都要分配；
  2. **限制容量**：超过上限的 buffer 直接丢弃，不放回池里，否则一个超大消息会让池里长期留着大块内存；
  3. 按大小**分级**：比如 512B、4KB、64KB 各一个池；
  4. **池中对象随时可能被 GC 清掉**：不能用来管理有状态、必须归还的资源（比如连接）；
  5. 取出之后要重置状态（`buf = buf[:0]`），避免把上一次的数据泄漏出去。
- **append 风格 API 是零分配的基石**：`strconv.AppendInt` / `AppendFloat` / `AppendQuote`、`encoding/binary` 的 `LittleEndian.AppendUint32` 和 `AppendUvarint`、`utf8.AppendRune`、`fmt.Appendf`、`time.Time.AppendFormat`。约定：调用方提供 `dst []byte`，函数返回追加后的切片。
- **共享不可变 buffer**：编码一次，同一个 `[]byte` 发给多个连接，没有引用计数，由 GC 回收。规则是发布之后只读。另一个陷阱是对共享 slice 做 `append`：如果它还有剩余容量，会覆盖别人看到的数据，必要时用三下标切片 `b[:n:n]` 截断容量。
- **数据导向设计**：
  - 缓存行通常是 64 字节；
  - SoA 让热循环只触碰需要的字段；
  - 冷热字段分离：把很少访问的字段（名字、装备详情）挪到另一张表；
  - 用下标代替指针：实体之间的引用用 `int32` 下标或"代数 + 下标"句柄，GC 不用扫描，也避免了悬挂引用；
  - 伪共享的消除见 [Ch09](../ch09-sync-memory-model/)（`golang.org/x/sys/cpu.CacheLinePad`）。
- **BCE**：热循环开头先断言长度（`_ = xs[len(ys)-1]`），或者对所有切片用统一的长度变量重新切片，让 prove 阶段证明下标在界内（[Ch13](../ch13-alloc-escape-compiler/)）。
- **`simd` 包（1.27 新增，实验性，需 `GOEXPERIMENT=simd`）**：
  - 可移植、与向量长度无关：类型有 `Int8s`…`Float64s` 和掩码类型 `Mask8s`…；向量长度至少 128 位，一次执行中所有向量等长，用 `Len()` 获取（本机 AVX2 实测 `Float32s` 有 8 个通道）；
  - 加载和存储：`LoadFloat32s`、`LoadFloat32sPart`（处理尾部，返回实际加载的元素数）、`BroadcastFloat32s`、`Store`、`StorePart`；
  - 运算：`Add`、`Sub`、`Mul`、`Div`、`MulAdd`、`Min`、`Max`、`Sqrt`、`Abs`；比较运算（`Greater`、`LessEqual` 等）产生掩码，配合 `IfElse` 和 `Masked` 使用；
  - **可移植 API 里没有水平归约**：点积要先在向量里累加，最后 `Store` 到小数组，再用标量求和（本机实测通过）；
  - 不开 `GOEXPERIMENT=simd` 时，这个包的所有文件都会被构建约束排除，所以 SIMD 代码要放在带 `//go:build goexperiment.simd` 的文件里，并保留一份标量实现作为回退。
- **`simd/archsimd`**：与架构绑定的类型，如 `Float32x4`、`Float32x8`、`Float64x8`、`Int8x16`，可以用到 256 位和 512 位的 AVX 指令。1.27 新增 arm64 Neon 和 wasm 128 位支持。用 `archsimd.X86.AVX2()` 这类 CPU 特性检测选择代码路径。**API 不稳定，而且刻意不可移植**。

### 源码阅读清单
| 文件 → 函数/类型 | 带着什么问题读 |
|---|---|
| `sync/pool.go` → `Get`、`Put`、`pinSlow`、`poolCleanup` | 为什么 Put 一个 `[]byte` 会分配？victim 缓存让对象最多能撑过几次 GC？ |
| `unsafe/unsafe.go` → `String`、`StringData`、`Slice`、`SliceData` 的文档注释 | 每个函数的前置条件是什么？违反之后会出现哪些症状（数据被改、GC 漏扫）？ |
| `strconv/number.go`（实现在 `internal/strconv/itoa.go`）、`encoding/binary/binary.go` · `varint.go` 里的 `Append*` 系列 | append 风格 API 的容量增长策略和零分配条件是什么？ |
| `simd/doc.go` · `simd/simd_types.go` · `simd/simd_emulated.go` | 可移植类型怎样映射到 archsimd 或纯 Go 模拟？`Len()` 在什么时候确定？ |
| `simd/archsimd/cpu.go` · `ops_amd64.go` | CPU 特性检测怎样做？同一个运算在 AVX2 和 AVX-512 下有什么不同？ |
| `cmd/vendor/golang.org/x/tools/go/analysis/passes/modernize/unsafefuncs.go` | 这个现代化器识别哪些指针运算模式？哪些情况它不会改写？ |

## 📊 Java 25 ↔ Go 1.27 机制级对照表

| 维度 | Java 25 | Go 1.27 | 取舍 / 游戏服影响 |
|---|---|---|---|
| 小对象复用 | 一般不池化（TLAB + 年轻代回收很便宜，池化反而促进晋升） | 热路径上池化或预分配有效（每次分配都推进 GC） | 同样的"对象池"建议，在两门语言里结论相反 |
| 缓冲区池 | Netty `PooledByteBufAllocator` + 引用计数 | 分级 `sync.Pool` + 容量上限，GC 回收 | Netty 更精细但有泄漏风险；Go 更简单，但池随时可能被 GC 清空 |
| 一次编码多次发送 | `retainedDuplicate()` / 引用计数 | 共享不可变 `[]byte` | Go 零成本共享，但要遵守"发布后只读"的约定 |
| 连续值布局 | 只有原始类型数组可以；对象数组是引用数组 | `[]struct` 天然连续 | 实体遍历、ECS 场景，Go 的写法更自然 |
| 堆外内存 | FFM `Arena` + `MemorySegment`（有边界和生命周期检查） | 一般不需要（无指针的 `[]T` 本身就是 noscan）；需要时用 cgo 或 mmap | Java 要绕开 GC 就得走堆外；Go 用无指针布局就能绕开扫描成本 |
| 零拷贝的字节/字符串转换 | 没有直接对应（String 内部表示不对外开放） | `unsafe.String` / `unsafe.Slice`（有前置条件） | Go 可以零拷贝，但违反约定会悄悄破坏数据 |
| SIMD | Vector API（孵化中），`SPECIES_PREFERRED` 自适应通道数，提供 `reduceLanes` | `simd`（实验性，可移植，无水平归约）+ `archsimd`（与架构绑定） | 两边都不是稳定 API；生产使用要保留标量回退并加开关 |
| 边界检查 | JIT 做范围检查消除，运行期投机 | 编译期 prove 阶段，可以用 `check_bce` 验证 | Go 可以在 CI 里守住"热循环零边界检查" |
| 弃用的危险手段 | `sun.misc.Unsafe` 内存访问被弃用，默认告警 | `unsafe` 一直可用，规则写在文档里，`go vet` 能抓一部分 | Java 正在把人推向安全 API；Go 依赖开发者自律和 review |

## 🔬 实验

### 实验 1：广播热路径从 N allocs/op 降到 0（必做）
- **目的**：完整走一遍"测量 → 定位 → 改造 → 证明"的流程。
- **方法**（`go/lab/broadcast/`）：一个 100 人房间每个 tick 广播位置更新。
  - 基线版本：每个玩家单独 `json.Marshal`，每次发送 `make([]byte)`，每次发送创建一个闭包；
  - 优化版本依次加入：只编码一次 → append 风格的二进制编码 → 共享不可变帧 → 分级 `sync.Pool` → 预分配批次切片。
- **工具**：`go test -bench=Broadcast -benchmem -count=10` 加 benchstat；`go test -memprofile` 定位剩余的分配；runtime/metrics 统计每 1 万次广播触发的 GC 次数。
- **预期现象**：每一步都能看到 allocs/op 和 ns/op 下降，最终 0 allocs/op，GC 次数大幅减少。
- **记录**：每一步的 benchstat 表格和 heap profile 的 top 条目。

### 实验 2：AoS vs `[]*T` vs SoA 遍历
- **目的**：量化布局对缓存和 GC 的影响。
- **方法**：
  - Go（`go/lab/layout/`）：100 万个实体，每个包含位置、速度、血量、名字、buff 列表；位置积分循环分别用 `[]*Entity`、`[]Entity`、SoA（`xs, ys, zs []float32`）实现；
  - Java（`java-bench/` 中的 JMH）：`Entity[]`、原始类型数组 SoA、FFM `MemorySegment`。
- **工具**：benchstat / JMH；**Linux/WSL2** 上用 `perf stat -e cache-misses,cache-references` 统计缓存未命中；Go 侧再用 gctrace 对比 GC 标记时间。
- **预期现象**：SoA 最快，且无指针、GC 标记成本最低；`[]*Entity` 最慢。Java 侧对象数组和原始类型数组之间的差距比 Go 更大。
- **记录**：ns/op、缓存未命中率、GC 标记 CPU。

### 实验 3：SIMD —— 标量 vs `simd` vs `archsimd` vs Java Vector API
- **目的**：评估 1.27 实验性 SIMD 在 AOI 判定上的真实收益和代价。
- **方法**：4096 个实体，判断哪些落在以某点为圆心、r 为半径的圆内（距离平方比较）；再加一个点积。
  - Go：标量（已消除边界检查）、`simd.Float32s`（`GOEXPERIMENT=simd`）、`archsimd.Float32x8`；
  - Java：标量、Vector API（`--add-modules jdk.incubator.vector`，JMH）。
- **工具**：benchstat、JMH；同时记录向量通道数（`Len()` / `SPECIES_PREFERRED.length()`）。
- **预期现象**：向量化版本有数倍提升，但受内存带宽限制，达不到理论倍数。点积的结果和标量版本可能有微小差异，原因是浮点加法顺序不同，测试要用误差容忍比较。
- **记录**：吞吐、通道数、代码行数和可维护性的主观评估，以及"是否值得上生产"的结论。

### 实验 4：池化导致的内存膨胀
- **方法**（`go/lab/poolbloat/`）：网关读缓冲区用单个不限容量的 `sync.Pool`，先注入一波 1MB 的大包，再恢复为 200B 的小包流量；观察 `/gc/heap/live:bytes` 和 RSS。然后改成分级池并限制容量，对比结果。
- **预期现象**：不限容量时，大 buffer 被反复取出放回，长期占用内存；分级并限容后，内存回落。

## 🧪 练习规划

**Go：`go/exercises`**
1. `func AppendPosUpdate(dst []byte, id uint32, x, y, z float32) []byte` 和 `func DecodePosUpdate(b []byte) (PosUpdate, int, error)`：用 `binary.LittleEndian.AppendUint32` 和 `math.Float32bits` 实现。
   - 容量足够时 `AllocsPerRun` 为 0；
   - fuzz 测试验证编码后解码能还原（round-trip）；
   - 截断的输入返回错误，而不是 panic。
2. `type BufPool struct{...}`，方法 `Get(n int) *[]byte` 和 `Put(b *[]byte)`：分级的 `sync.Pool`。
   - 返回的 buffer 长度为 n、容量至少为 n；
   - 容量超过上限的 buffer 在 Put 时被丢弃（通过计数器观测）；
   - 稳态下分配次数接近 0；
   - 并发测试必须通过 `-race`（Linux/WSL2）。
3. `func (r *Room) BroadcastPos(ups []PosUpdate)`：只编码一次，把同一个帧放进每个玩家的发送队列。
   - 测试验证所有玩家收到完全相同的字节；
   - 测试验证帧容量已被截断（`len == cap`），防止有人 append 覆盖共享数据；
   - 每次广播的分配不超过 1 次（使用池时为 0 次）。
4. `type Positions struct{ X, Y, Z []float32 }`，方法 `Integrate(vx, vy, vz []float32, dt float32)` 和 `WithinRadius(cx, cy, cz, r float32, out []int32) []int32`。
   - 标量版本要求循环内零边界检查（用 `-d=ssa/check_bce/debug=1` 验收）；
   - 选做：在带 `//go:build goexperiment.simd` 的文件里写 SIMD 版本，测试与标量结果一致（允许浮点误差）；
   - 附与 AoS 版本对比的基准。
5. `func UnsafeString(b []byte) string` 和 `func UnsafeBytes(s string) []byte`：用 `unsafe.String`、`unsafe.StringData`、`unsafe.Slice` 实现。
   - 测试验证零分配、内容正确；
   - 在文档注释里写明调用方必须遵守的不变量（review 验收）。

**Java：`java/drills`**
1. `OffHeapEntitiesDrill.java`：用 FFM 的 `Arena.ofConfined()` 加 `MemoryLayout.structLayout` 在堆外存放 N 个实体（x、y、z、hp），实现位置积分。
   - 自校验结果与堆内 `float[]` 版本一致；
   - 自校验 Arena 关闭后再访问段会抛 `IllegalStateException`。
2. `VectorAoiDrill.java`（运行：`bin/jrun --add-modules jdk.incubator.vector ch16-zero-alloc-dod/java/drills/VectorAoiDrill.java`）：用 Vector API 实现点积和半径判定，含尾部处理（`loopBound` 加标量收尾或掩码）。自校验在随机数据和奇数长度下与标量结果一致（带误差容忍）。

## 🎮 游戏服落地

1. **状态同步广播**（位置、血量、技能事件，每 tick 发给视野内的所有人）：
   - Go：编码一次，共享截断容量后的不可变帧，帧 buffer 来自分级池，按 tick 批量发送；
   - Java：Netty 的 `retainedDuplicate` 或 `CompositeByteBuf`，必须严格配对 `release`，否则就是内存泄漏；
   - 取舍：Go 没有引用计数的心智负担，但要靠约定和测试守住"只读"。
2. **AOI、碰撞、技能范围判定**：SoA 加 BCE 是稳妥的第一步。SIMD（Go 实验性 / Java 孵化中）放在独立的构建标签或开关后面，保留标量实现，用基准决定是否开启。
3. **ECS 实体存储**：按 archetype 组织的 SoA 表，实体引用用"代数 + 下标"句柄。Go 用 `[]T` 实现很自然，而且无指针，GC 零扫描；Java 只能用原始类型数组或 FFM 段手工实现，代码复杂度更高。
4. **网关收发缓冲**：分级 `sync.Pool` 加容量上限；大包走专用路径，不放回池；Java 侧对照 Netty 的池化分配器和高低水位线（[Ch18](../ch18-tcp-gateway/)）。

## ✅ 验收答辩题

- [ ] [L2] 为什么往 `sync.Pool` 里 `Put([]byte)` 每次都会分配？正确的写法是什么？
- [ ] [L2] `sync.Pool` 为什么不能当连接池用？池中对象最多能撑过几次 GC？
- [ ] [L2] `unsafe.String(unsafe.SliceData(b), len(b))` 之后再修改 `b` 会发生什么？举一个会导致 map 查找出错的例子。
- [ ] [L2] SoA 相比 AoS，在缓存、预取、向量化、GC 扫描四个方面各有什么好处？什么访问模式下 AoS 反而更好？
- [ ] [L2] `simd.Float32s` 的向量长度什么时候确定？为什么可移植 API 没有水平归约？点积该怎么写？
- [ ] [L2] JEP 471/498 对 `sun.misc.Unsafe` 做了什么？FFM 的 `Arena` 怎样保证生命周期安全？
- [ ] [L3] 用实验 1 的数据讲一遍：每一步优化去掉了哪些分配，各自的收益是多少。
- [ ] [L3] 线上网关的 RSS 在一次大包攻击后不再回落，你怎样确认是池化导致的膨胀并修复？
- [ ] [L3] 共享广播帧被某个连接的写协程 append 覆盖，导致其他玩家收到错乱的数据。怎样复现？怎样从设计上防止？
- [ ] [L3] 给出实验 3 的结论：在你的机器上 SIMD 的收益是多少？瓶颈在计算还是在内存带宽？
- [ ] [L4] 一个 MMO 场景服要做 ECS 改造：给出数据布局方案、实体句柄设计、GC 影响评估，以及 Go 与 Java 实现的工作量对比。
- [ ] [L4] 团队想在战斗服上使用 Go 1.27 的实验性 `simd` 或 Java 的 Vector API。写一页风险评估和上线方案（开关、回退、版本升级策略）。

## 🕳️ 常见坑

- **`sync.Pool` 里放非指针值**（`[]byte`、小 struct），每次 Put 都装箱分配。
- **把 `sync.Pool` 当对象生命周期管理器**（连接池、会话池）：GC 会清空它。
- **池里的 buffer 不限容量**：一次超大消息就会让内存长期膨胀。
- **共享 slice 发布后又被修改，或者被 append 覆盖**：用三下标切片截断容量，并通过 review 和测试守住"只读"约定。
- **unsafe 零拷贝转换后又修改了原 `[]byte`**：字符串的不可变性被破坏，map 的键会悄悄损坏。
- **到处做 SoA**：可读性大幅下降，收益只在热循环里。先测量再动手。
- **把实验性的 `simd` 当稳定 API 用**：没有构建标签、没有标量回退；另外，测试里直接比较浮点结果，会被加法顺序不同带来的误差坑到。
- **Java 侧在 ZGC/G1 下池化小对象**：对象变成长寿对象，增加老年代的工作量；Netty ByteBuf 忘记 `release` 导致泄漏；FFM `Arena` 关闭后，仍有代码持有段的引用。

## 📚 一手资料

- Go：
  - `go doc unsafe`（`String`、`Slice` 等函数的前置条件）、`go doc sync.Pool`
  - `go doc simd`、`go doc simd/archsimd`（需要 `GOEXPERIMENT=simd`）
  - [Go 1.27 Release Notes](https://go.dev/doc/go1.27)（simd 和 go fix 两节）
  - `go doc encoding/binary.AppendUvarint`、`go doc strconv.AppendInt`
- Java：
  - [JEP 454](https://openjdk.org/jeps/454)（FFM）、[JEP 508](https://openjdk.org/jeps/508)（Vector API）、[JEP 471](https://openjdk.org/jeps/471) / [JEP 498](https://openjdk.org/jeps/498)（Unsafe 内存访问的弃用与告警）
  - `java.base/java/lang/foreign/` 源码与 Javadoc
  - Netty 的 `PooledByteBufAllocator` 文档（第三方）
- 数据导向设计：Mike Acton 的 "Data-Oriented Design and C++"（CppCon 2014）演讲，思想与语言无关
