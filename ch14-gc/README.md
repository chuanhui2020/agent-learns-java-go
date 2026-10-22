# Ch14 · GC 深度：Green Tea vs G1/ZGC/Shenandoah ⭐

> Part 3 · 估时 20h · 状态：⚪ 未开始 · 从写屏障、步调器到 Green Tea 的 span 批量扫描，讲清两种 GC 哲学，并用同一份游戏负载的数据做选型

## 🎯 目标（资深标准）

- **L2**：画出 Go GC 一个周期的四个阶段（清扫终止 STW → 并发标记 → 标记终止 STW → 并发清扫），讲清混合写屏障为什么同时需要 Yuasa 删除屏障和 Dijkstra 插入屏障；讲清 Green Tea 的"按 span 攒批扫描"；讲清 G1 的 Region / Remembered Set / Mixed GC、分代 ZGC 的着色指针与屏障、分代 Shenandoah（JEP 521）。
- **L2**：能推导堆目标公式 `目标堆 = 存活堆 + (存活堆 + GC 根) × GOGC / 100`，讲清 GOMEMLIMIT 加上 GC CPU 限制器怎样避免"死亡螺旋"。
- **L3**：能读 `GODEBUG=gctrace=1` 的每一个字段，以及 `-Xlog:gc*` / `jfr view gc-pauses`；能复现并消除 mark assist 导致的尾延迟；能用 runtime/metrics 搭建 GC 监控面板。
- **L4**：针对给定的游戏服负载（存活堆、分配率、延迟 SLO、容器内存上限），在 Go 的 GOGC/GOMEMLIMIT 组合和 Java 的 G1/ZGC/Shenandoah 之间做出有数据支撑的选型。

## 🔗 前置

- [Ch13](../ch13-alloc-escape-compiler/)：分配器、noscan span、`deductAssistCredit`
- [Ch01](../ch01-values-memory/)：对象布局与紧凑对象头
- [Ch07](../ch07-scheduler/)：P、STW 与抢占

后续：[Ch15](../ch15-profiling-bench/) 讲 GC 相关的剖析手段；[Ch16](../ch16-zero-alloc-dod/) 讲怎样从数据布局上降低 GC 成本；[Ch23](../ch23-build-observability/) 讲容器里怎样设置 GOMEMLIMIT。

## 💡 关键洞察

1. **Go GC 不分代、不移动对象，是刻意的选择。** 好处：不需要读屏障，对象地址稳定（对 cgo 友好），实现简单，STW 很短。代价：
   - 回收效率完全取决于存活堆的大小和分配速率；
   - 碎片只能靠 size class 隔离来缓解；
   - 吞吐上限一般不如分代收集器。
   所以在 Go 里调 GC，本质上是在**调分配**。
2. **Go 的停顿短，不等于延迟低。** STW 通常在亚毫秒级，但并发标记期间分配太快的 goroutine 会被征收 **mark assist**，被迫替 GC 干活。这才是 Go 服务 p99 抖动的常见元凶。它不出现在"GC 停顿"指标里，要在 trace 和 `/cpu/classes/gc/mark/assist` 指标里找。
3. **Java 的 ZGC 和分代 Shenandoah 用屏障换来了"停顿与堆大小基本无关"。** 代价是屏障带来的吞吐损耗和更高的内存占用。Java 25 下的选型：一般场景 G1，超大堆或严格的延迟 SLO 选 ZGC，Shenandoah 需要换用带它的 OpenJDK 构建。不要再用十年前"Go GC 碾压 Java"的结论。

## ☕ Java 25 深度复习

### G1（默认收集器）
- 堆被分成等大的 **Region**（本机默认 4MB）。年轻代和老年代都是逻辑上的 Region 集合，大对象放进 humongous Region。
- **Remembered Set + 卡表**：记录"其他 Region 指向本 Region 的引用"，这样回收年轻代时不用扫描整个老年代。写屏障负责维护卡表。
- **并发标记**基于 SATB（初始快照），由写前屏障维护快照。标记完成后，**Mixed GC** 在回收年轻代的同时，挑选回收收益最高的老年代 Region 一起疏散。疏散就是复制，因此天然带压缩。
- 停顿目标 `-XX:MaxGCPauseMillis`（默认 200ms，本机已确认）。G1 会据此调整每次回收的 Region 数量，但这只是"目标"，不是保证。

### 分代 ZGC
- JDK 25 里 ZGC 只有分代模式（非分代模式已被移除，`ZGenerational` 开关不复存在，本机 `PrintFlagsFinal` 中已查不到）。开启方式：`-XX:+UseZGC`。
- **着色指针**：引用里携带元数据位。**加载屏障**在读引用时检查和修正指针；分代模式下还有**存储屏障**，用来维护跨代引用。
- 标记、**并发重定位**（移动对象）、引用修正都与应用并发执行，停顿只用于少量根的处理，所以停顿时间基本与堆大小无关。
- 常用旋钮：`-Xmx`、`-XX:SoftMaxHeapSize`。

### 分代 Shenandoah（JEP 521，JDK 25 正式）
- 开启方式：`-XX:+UseShenandoahGC -XX:ShenandoahGCMode=generational`。
- 同样是并发压缩，靠加载引用屏障保证移动对象期间的访问正确。
- **本机注意**：Oracle JDK 25.0.2 不带 Shenandoah，`-XX:+UseShenandoahGC` 会报 `not supported`。做 Shenandoah 实验要换 Temurin 等 OpenJDK 构建。

### 对象头与内存
- **JEP 519 紧凑对象头**（`-XX:+UseCompactObjectHeaders`）：对象头从 12 字节降到 8 字节。小对象越多，堆占用和 GC 的扫描、复制量下降越明显。实验 1 要测开关前后的差异。
- 引用对象：`java.lang.ref.Cleaner`（对照 Go 的 `runtime.AddCleanup`）、`WeakReference`（对照 `weak.Pointer`）。

### 观测
- 日志：`-Xlog:gc*:file=gc.log:time,uptime,level,tags`。
- JFR 视图（本机已确认存在）：`jfr view gc-pauses`、`gc-pause-phases`、`gc-concurrent-phases`、`gc-cpu-time`、`gc-configuration`、`heap-configuration`。
- 进程内订阅：`jdk.jfr.consumer.RecordingStream` 订阅 `jdk.GarbageCollection` 事件。

### 源码位置
- HotSpot：`src/hotspot/share/gc/g1/`、`gc/z/`、`gc/shenandoah/`、`gc/shared/`。
- 对象头：`src/hotspot/share/oops/`。

## 🐹 Go 1.27 深入

### 规范层
- 规范只规定可达性语义，不规定回收算法。与 GC 相关的语义边界有三条：
  - `runtime.KeepAlive`：保证对象至少活到这一点；
  - finalizer / cleanup：执行时机不确定；
  - `unsafe.Pointer` 规则：违反规则会让 GC 看不到指针（[Ch16](../ch16-zero-alloc-dod/)）。

### 运行时层
- **周期**（`runtime/mgc.go`）：`gcStart` → 清扫终止（STW）→ 开启写屏障、并发标记（`gcBgMarkWorker` / `gcDrain`）→ `gcMarkDone` → 标记终止（STW，`gcMarkTermination`）→ 关闭写屏障、并发清扫（`gcSweep`，分配时顺带做的懒清扫 + 后台清扫）。
- **三色与混合写屏障**（`runtime/mbarrier.go` 文件头注释）：Yuasa 风格的删除屏障给被覆盖的旧指针着色，Dijkstra 风格的插入屏障给新写入的指针着色。两者合用，使得**栈不需要在标记终止时重扫**，这是 STW 能做到很短的关键。屏障的写入先进入每个 P 的缓冲区（`runtime/mwbbuf.go`）批量处理。
- **步调器**（`runtime/mgcpacer.go`，`gcControllerState`）：
  - 堆目标：`存活堆 + (存活堆 + GC 根：栈 + 全局变量) × GOGC / 100`，由 `heapGoal` 计算；
  - 后台标记占用 25% 的 P（`gcBackgroundUtilization = 0.25`）；
  - 跟不上时，用 **mark assist** 让分配者补足差额（`gcAssistAlloc`）。
- **GOMEMLIMIT**：针对 Go 运行时总内存的**软上限**，用 `debug.SetMemoryLimit` 或 `/gc/gomemlimit:bytes` 设置和查看。接近上限时 GC 会更频繁；**GC CPU 限制器**（`runtime/mgclimit.go`）把 GC 的 CPU 占比限制在保守的 50% 左右，避免"内存不够 → 一直 GC → 应用跑不动"的死亡螺旋。常见组合：`GOGC=off` + GOMEMLIMIT，或调高 GOGC + GOMEMLIMIT 兜底。
- **Green Tea（当前默认标记算法）**（`runtime/mgcmark_greenteagc.go` 文件头注释）：
  - 核心思路：**推迟扫描，在同一个 span 上攒够对象再一起扫**，以改善局部性、摊薄读取元数据的成本，并为预取和向量化创造机会；
  - 实现：针对小对象，span 内联两组位图——marks（第一次发现指针时置位，并把整个 span 入队）和 scans（已扫描）；span 工作队列用 FIFO（传统 workbuf 是 LIFO），出队时用两组位图的差集决定要扫描哪些对象，保持精确性；
  - 构建期开关 `GOEXPERIMENT=nogreenteagc` 在 1.27 源码中仍然存在，可用于对照实验。
- **回收内存给操作系统**：scavenger（`runtime/mgcscavenge.go`）保留大约比堆目标多 10% 的内存（`retainExtraPercent = 10`），把其余空闲页归还操作系统；`debug.FreeOSMemory` 强制执行一次。
- **栈也是根**：每个 goroutine 的栈都要扫描（`/gc/scan/stack:bytes`）。十万个 goroutine 的服务，栈扫描本身就是可观的成本。
- **finalizer、cleanup、弱引用、驻留**：
  - `runtime.SetFinalizer` 的问题：会复活对象，每个对象只能设一个，对象成环时会泄漏，回收要推迟一个周期；
  - `runtime.AddCleanup(ptr, fn, arg)`（`runtime/mcleanup.go`）：不复活对象，可以设置多个，返回的 `Cleanup` 可以 `Stop`，清理函数并发执行；
  - `weak.Make` / `weak.Pointer.Value`（`weak/pointer.go`）：弱引用；
  - `unique.Make` / `Handle.Value`（`unique/handle.go`）：值驻留，比较两个 handle 只需比较指针。
- **观测**：
  - `GODEBUG=gctrace=1` 每个周期一行：`gc N @T秒 P%: 清扫终止+并发标记+标记终止 ms clock, …assist/后台/空闲… ms cpu, 开始->结束->存活 MB, 目标 MB …`；在 1.27 里，检测 goroutine 泄漏的周期还会带 `(checking for goroutine leaks)` 标记；
  - `GODEBUG=gcpacertrace=1` 打印步调器内部状态；
  - runtime/metrics（`metrics.Read` 不停顿世界，`runtime.ReadMemStats` 会 STW）：`/sched/pauses/total/gc:seconds`（停顿直方图）、`/gc/heap/goal:bytes`、`/gc/heap/live:bytes`、`/cpu/classes/gc/mark/assist:cpu-seconds`、`/cpu/classes/gc/total:cpu-seconds`、`/gc/gogc:percent`、`/gc/gomemlimit:bytes`、`/gc/limiter/last-enabled:gc-cycle`。

### 源码阅读清单
| 文件 → 函数/类型 | 带着什么问题读 |
|---|---|
| `runtime/mgc.go` → `gcStart`、`gcMarkDone`、`gcMarkTermination`、`gcSweep` 及 gctrace 打印处 | 两次 STW 各做了什么？为什么标记终止前需要"确认没有剩余工作"的分布式终止检测？ |
| `runtime/mbarrier.go`（文件头注释）· `runtime/mwbbuf.go` | 混合屏障如何免去栈重扫？屏障为什么先写缓冲区？ |
| `runtime/mgcmark.go` → `gcDrain`、`gcAssistAlloc` | assist 的工作量怎样按分配字节换算？什么情况下 goroutine 会被 park 等待？ |
| `runtime/mgcmark_greenteagc.go` → `tryDeferToSpanScan`、`scanSpan` | 哪些对象走 span 批量扫描，哪些仍走传统路径？FIFO 为什么比 LIFO 更利于攒批？ |
| `runtime/mgcpacer.go` → `gcControllerState`、`heapGoal`、`setGCPercent`、`setMemoryLimit` | GOGC 和 GOMEMLIMIT 同时生效时，堆目标取哪个？ |
| `runtime/mgclimit.go` → `gcCPULimiterState` | 限制器怎样计量 GC 的 CPU？触发后对应用意味着什么（内存可能超出上限）？ |
| `runtime/mgcscavenge.go` | 归还内存的节奏如何避免与分配互相打架？ |
| `runtime/mcleanup.go` · `runtime/mfinal.go` · `weak/pointer.go` · `unique/handle.go` | cleanup 为什么不会复活对象？弱指针在什么时刻失效？ |

## 📊 Java 25 ↔ Go 1.27 机制级对照表

| 维度 | Java 25（G1 / 分代 ZGC / 分代 Shenandoah） | Go 1.27 | 取舍 / 游戏服影响 |
|---|---|---|---|
| 分代 | 都分代 | 不分代 | Java 对"朝生夕死"的消息对象回收极便宜；Go 需要靠逃逸分析和复用减少这类分配 |
| 移动 / 压缩 | G1 疏散复制；ZGC、Shenandoah 并发重定位 | 不移动，用 size class 隔离碎片 | Go 对象地址稳定，cgo 友好，不需要读屏障；但长期运行可能有碎片，RSS 高于存活堆 |
| 屏障 | G1：写前（SATB）和写后（卡表）屏障；ZGC：加载和存储屏障；Shenandoah：加载引用屏障 | 只在标记期间开启的混合写屏障 | Go 平时零屏障开销；ZGC、Shenandoah 用常驻屏障换来并发移动 |
| STW 内容 | G1：疏散停顿与堆的活跃度相关；ZGC：少量根处理 | 两次很短的 STW（清扫终止、标记终止） | 两边停顿都可以做到毫秒级以下；Go 的尾延迟主要来自 assist |
| 分配者付费 | 很少（TLAB 用尽，或 GC 跟不上时分配停顿） | mark assist：按分配量强制参与标记 | Go 服务分配突发时，延迟敏感的 goroutine 也会被拖慢 |
| 主要旋钮 | `-Xmx`、`MaxGCPauseMillis`、`SoftMaxHeapSize`、收集器选择 | `GOGC`、`GOMEMLIMIT` | Go 旋钮少，可以讲清楚；Java 旋钮多，上限高，调优成本也高 |
| 内存上限语义 | `-Xmx` 是硬上限，超出即 OOM | GOMEMLIMIT 是软上限，有 CPU 限制器兜底 | 容器里 Go 要预留余量，防止被 OOM-kill；Java 还要算上非堆内存 |
| 对象头 / 元数据 | 12B，开启 JEP 519 后 8B | 无对象头；含指针的小对象用 span 位图，较大的用对象头里的类型指针 | 大量小实体对象，Go 的内存密度更高 |
| 弱引用 / 清理 | `WeakReference`、`Cleaner` | `weak.Pointer`、`runtime.AddCleanup` | 语义已经接近；两边都不应该用 GC 来管理连接、文件等资源 |
| 观测 | `-Xlog:gc*`、JFR GC 视图、GC MXBean | gctrace、runtime/metrics、`go tool trace` | 两边都要把停顿直方图、GC CPU、assist 占比放进监控面板 |

## 🔬 实验

### 实验 1：同一份"房间 + 实体"负载，Go 与 Java 的 GC 对比（必做，建议 Linux/WSL2）
- **目的**：在可控的存活堆和分配率下，比较两边的停顿、尾延迟、GC CPU 和内存占用。
- **方法**：
  - `go/lab/gcload/` 和 `java/lab/GcLoad.java` 实现同一模型：N 个房间 × M 个实体是长寿状态，用来控制存活堆；每个 tick 生成临时消息和事件对象，用来控制分配率；测量每个 tick 的处理耗时。
  - Go 组合：`GOGC=100`、`GOGC=200`、`GOGC=off` + `GOMEMLIMIT`、`GOEXPERIMENT=nogreenteagc`（对照）。
  - Java 组合：G1（默认）、`-XX:+UseZGC`、分代 Shenandoah（**需要 Temurin/OpenJDK**），以及 `-XX:+UseCompactObjectHeaders` 开和关。
- **工具**：
  - Go：runtime/metrics 导出停顿直方图和 GC CPU，gctrace；
  - Java：`-Xlog:gc*`，`jfr view gc-pauses` 和 `gc-cpu-time`；
  - 两边：用 `/proc/<pid>/status` 的 VmRSS 测内存。
- **预期现象**：
  - Go 的停顿很短，但分配率上升后 tick 的 p99.9 主要被 assist 拖高；
  - G1 停顿更长，但吞吐好；ZGC 停顿极短，但 RSS 更高；
  - 开启紧凑对象头后 Java 堆占用下降。
- **记录**：每种组合的 tick p50/p99/p99.9、最大停顿、GC CPU 占比、RSS、吞吐。最后画一张"内存 – 延迟"权衡图。

### 实验 2：复现并消除 mark assist
- **目的**：亲眼看到 assist，并验证三种缓解手段。
- **方法**（`go/lab/assist/`）：一个 goroutine 以固定频率做 tick（延迟敏感），另一组 goroutine 周期性地突发大量分配。
- **工具**：
  - `go test -trace=trace.out`，然后 `go tool trace trace.out`，在时间线上找 GC 期间的 mark assist 区段；
  - 对比 `/cpu/classes/gc/mark/assist:cpu-seconds`。
- **缓解手段**：①减少突发路径的分配（复用缓冲区，[Ch16](../ch16-zero-alloc-dod/)）；②调高 GOGC 并用 GOMEMLIMIT 兜底；③给突发任务限速。
- **记录**：每种手段下 tick 的 p99、assist 的 CPU 时间、峰值内存。

### 实验 3：指针密集 vs 无指针布局的 GC 成本
- **目的**：验证"noscan 内存不用扫描"，并测出 Green Tea 对指针密集小对象的收益。
- **方法**（`go/lab/layout/`）：
  - 同样 100 万个实体，分别用 `map[int64]*Entity`、`[]*Entity`、`[]Entity`（实体内不含指针）加下标索引；
  - 字符串分别用 `[]string`，以及"一个大 `[]byte` + 偏移量"；
  - 每种布局强制 `runtime.GC()` 若干次。
- **工具**：gctrace 中的 mark CPU、`/gc/scan/heap:bytes`；再用 `GOEXPERIMENT=nogreenteagc` 重跑做对照。
- **预期现象**：无指针布局的标记时间降低一个数量级；Green Tea 对 `[]*Entity` 这类指针密集的小对象改善最明显。

### 实验 4（选做）：unique 驻留与 weak 缓存
- **方法**：10 万个玩家的公会名、地图名等重复度很高的字符串，用 `unique.Make` 驻留，测量存活堆的变化；再用 `weak.Pointer` 实现一个头像或配置缓存，观察 GC 之后条目被回收。

## 🧪 练习规划

**Go：`go/exercises`**
1. `func ReadGCSnapshot() GCSnapshot`：用 `runtime/metrics` 读出堆目标、存活堆、GC 次数、assist CPU 秒数、停顿 p99（根据直方图计算），**禁止使用 `runtime.ReadMemStats`**。
   - 测试：调用 `runtime.GC()` 后，GC 次数增加、堆目标大于 0；
   - 直方图百分位的计算单独写表驱动测试。
2. `func Intern(s string) unique.Handle[string]` 加上 `type Names struct{...}`：用驻留的名字作为 map 的键。测试：相等的字符串得到相等的 handle；基准对比用 handle 比较和用字符串比较的耗时。
3. `type WeakCache[K comparable, V any] struct{...}`，提供 `Get(k K) (*V, bool)` 和 `Put(k K, v *V)`：基于 `weak.Pointer` 实现，并用 `runtime.AddCleanup` 在值被回收后删除对应条目。
   - 测试：丢掉强引用并循环调用几次 `runtime.GC()` 后，条目消失；
   - 必须通过 `go test -race`（Linux/WSL2）。
4. `func Pack(es []*Entity) EntityTable`：把指针图转换成不含指针的表结构（下标代替指针、名字放进一个大 `[]byte`）。
   - 测试用 `reflect` 断言行类型不含任何指针字段；
   - 基准测量 `runtime.GC()` 的耗时，对比转换前后。
5. `func MemoryLimitFor(containerLimit int64, nonHeapReserve int64, headroom float64) int64`：根据容器内存上限计算 GOMEMLIMIT（预留非堆内存和余量），表驱动测试覆盖边界情况。在 [Ch23](../ch23-build-observability/) 中会用到。

**Java：`java/drills`**
1. `GcObserveDrill.java`：用 `jdk.jfr.consumer.RecordingStream` 在进程内订阅 `jdk.GarbageCollection` 事件，制造垃圾后触发 GC。自校验收到至少 1 个事件，并打印收集器名称、停顿时长。分别用默认参数和 `-XX:+UseZGC` 运行，对比输出。
2. `CleanerDrill.java`：用 `java.lang.ref.Cleaner` 注册清理动作，对象不可达后反复 `System.gc()` 并在超时时间内轮询。自校验清理动作执行了；在注释里写明为什么这不是确定性的，以及它和 Go `runtime.AddCleanup` 的语义对照。

## 🎮 游戏服落地

1. **MMO 场景服（存活堆大：地图、实体、AOI 索引）**：
   - Go：实体表用无指针布局，配合 GOMEMLIMIT，GC 成本与存活堆的大小脱钩；
   - Java：大堆优先考虑分代 ZGC；
   - 关键指标：停顿直方图和 GC CPU 占比。
2. **战斗房间服（分配率高：每帧的事件和消息）**：Go 要盯住 assist；手段是复用 buffer、给突发任务限速、调高 GOGC。Java 的分代收集器天然擅长回收这类短命对象，G1 往往就够用。
3. **容器部署**：
   - Go：`GOMEMLIMIT ≈ 容器上限 – 非堆预留 – 余量`，GOGC 可以适当调高；
   - Java：`-XX:MaxRAMPercentage`，并为元空间、线程栈、直接内存留出余量；
   - 两边都要压测验证"内存打满时的行为"：Go 是限制器介入、CPU 升高；Java 是 OOM 或 Full GC。
4. **缓存与驻留**：玩家名、道具模板 ID 等用 `unique` 驻留；头像、配置的派生数据用 `weak` 缓存。**连接、文件这类资源不要依赖 cleanup 回收**，必须显式关闭。

## ✅ 验收答辩题

- [ ] [L2] Go GC 一个周期的四个阶段，各自做什么？哪两段会 STW？
- [ ] [L2] 混合写屏障里，删除屏障和插入屏障各解决什么问题？合用之后为什么栈不需要重扫？
- [ ] [L2] 写出堆目标公式。GOGC=100、存活堆 1GB、栈加全局变量 100MB 时，堆目标是多少？设置 GOMEMLIMIT=1.5GB 后会怎样？
- [ ] [L2] Green Tea 为什么要"推迟扫描"？marks 和 scans 两组位图是怎么配合的？哪些对象不走这条路径？
- [ ] [L2] 分代 ZGC 的加载屏障和存储屏障分别做什么？为什么它的停顿基本与堆大小无关？
- [ ] [L2] G1 的 Remembered Set 解决什么问题？Mixed GC 怎样挑选老年代 Region？
- [ ] [L3] 逐个字段解释一行 gctrace 输出，并据此判断"是停顿问题还是 assist 问题"。
- [ ] [L3] 某个 Go 服务 GC 停顿只有 200µs，但 p99 延迟每隔几秒抖到 30ms。给出排查步骤（trace、metrics）和三种修复方案。
- [ ] [L3] 容器限制 4GB，Go 服务频繁被 OOM-kill。如何设置 GOMEMLIMIT 和 GOGC？限制器触发后会出现什么现象？
- [ ] [L3] 用实验 3 的数据解释：为什么把 `[]*Entity` 改成 `[]Entity` 加下标，GC 的标记时间会下降一个数量级？
- [ ] [L3] `SetFinalizer` 有哪些问题？`AddCleanup` 怎样避免了这些问题？
- [ ] [L4] 给定负载：存活堆 6GB、分配率 800MB/s、延迟 SLO p99.9 < 20ms、单 Pod 8GB。分别给出 Go 方案和 Java 方案（收集器、参数、数据布局），以及你的最终推荐和理由。
- [ ] [L4] 团队有人说"Go GC 比 Java 先进"，也有人说相反。用本章的实验数据写一段客观的结论。

## 🕳️ 常见坑

- **只看 GC 停顿指标就说"GC 没问题"**：Go 的尾延迟主要来自 mark assist。
- **照搬 JVM 的调优习惯**：Go 没有 `-Xmn`、Survivor 区这类旋钮；也不要一上来就调 GOGC，先减少分配。
- **把 GOMEMLIMIT 设成等于容器上限**：非堆内存（栈、cgo、操作系统页缓存的计量方式）会让进程超限被杀。要留出余量。
- **用 `runtime.ReadMemStats` 做高频监控**：它会 STW。用 runtime/metrics。
- **用 finalizer 或 cleanup 关闭连接、文件**：执行时机不确定，甚至可能永远不执行。要显式 `Close`，cleanup 只做兜底和告警。
- **以为 Go 的堆会被压缩**：它不会。长期运行后 RSS 高于存活堆是正常的。
- **在 Oracle JDK 上跑 Shenandoah 实验，然后以为参数写错了**：Oracle JDK 25 不带 Shenandoah。
- **只比较平均延迟**：GC 的影响体现在 p99、p99.9 和最大值上，平均值会掩盖问题。

## 📚 一手资料

- Go：
  - [A Guide to the Go Garbage Collector](https://go.dev/doc/gc-guide)（GOGC、GOMEMLIMIT、堆目标公式，必读）
  - [Go 1.27 Release Notes](https://go.dev/doc/go1.27)
  - 源码注释：`src/runtime/mgc.go` 文件头（GC 总览）、`src/runtime/mbarrier.go` 文件头（混合屏障）、`src/runtime/mgcmark_greenteagc.go` 文件头（Green Tea 算法）
  - `go doc runtime/metrics`（全部指标说明）
- Java：
  - [JEP 521](https://openjdk.org/jeps/521)（分代 Shenandoah）、[JEP 519](https://openjdk.org/jeps/519)（紧凑对象头）
  - Oracle《HotSpot Virtual Machine Garbage Collection Tuning Guide》（JDK 25 版）
  - HotSpot 源码：`src/hotspot/share/gc/{g1,z,shenandoah,shared}/`
