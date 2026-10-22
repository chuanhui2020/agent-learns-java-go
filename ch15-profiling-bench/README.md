# Ch15 · 剖析与基准方法论 ⭐

> Part 3 · 估时 16h · 状态：⚪ 未开始 · 建立"假设 → 采样 → 定位 → 修复 → 统计证明"的闭环，把 pprof/trace 与 JFR/JMH 用到同一个水平

## 🎯 目标（资深标准）

- **L2**：讲清每种剖析数据的**采样原理与盲区**：
  - Go：CPU 是信号定时采样；heap 按分配字节采样；block 和 mutex 由速率参数控制；trace 是事件流。
  - Java：JFR 执行采样（JEP 518 协作式采样）、CPU 时间采样（JEP 509）、方法计时与追踪（JEP 520，基于字节码插桩）。
- **L2**：讲清 `testing.B.Loop` 和 JMH 各自怎样对抗编译器优化（死代码消除、常量折叠、循环外提）和 JIT 预热。
- **L3**：能用 FlightRecorder 抓到一次偶发卡顿并定位元凶；能用 mutex/block profile 和 `jfr view contention-by-site` 找出锁竞争；所有性能结论都附 benchstat 或 JMH 的统计结果。
- **L4**：为游戏后端设计**持续剖析 + 基准回归门禁**：采什么、多久采一次、开销预算、权限与安全、PGO profile 如何回流到构建。

## 🔗 前置

- [Ch13](../ch13-alloc-escape-compiler/)：逃逸、内联、PGO
- [Ch14](../ch14-gc/)：GC 指标与 assist
- [Ch07](../ch07-scheduler/)、[Ch09](../ch09-sync-memory-model/)：调度与锁（读懂 trace 和 mutex profile 的前提）

## 💡 关键洞察

1. **CPU profile 只回答"CPU 花在哪"，不回答"时间花在哪"。** 游戏服的卡顿大多发生在"不在 CPU 上"的时候：等锁、等调度、等 GC assist、等网络。要用 trace、block/mutex profile 或墙钟采样，单看 CPU 火焰图会误判。
2. **偶发问题要靠"一直录、出事时保存"。** Go 的 `runtime/trace.FlightRecorder` 和 JFR 的持续录制是同一个思路：环形缓冲区常驻，检测到异常就把最近几秒落盘。这比"复现后再开 profile"可靠得多。
3. **没有统计就没有结论。** Go 用 `-count` 多跑几轮再交给 `benchstat`，Java 用 JMH 的多 fork 加预热。单次跑分、只看平均值、不报置信区间的"优化"，一律不予采信（见 [方法论第六节](../docs/methodology.md)）。

## ☕ Java 25 深度复习

### JFR 架构与开销
- 事件分三类：瞬时事件、持续事件、采样事件。线程先写本地缓冲区，再汇入全局缓冲区，最后以 chunk 文件的形式写进录制仓库。默认配置（`default.jfc`）的开销很低，可以在生产环境常开；`profile.jfc` 采样更密，开销也更高。
- 开启方式：
  - 启动参数：`-XX:StartFlightRecording=duration=60s,filename=rec.jfr,settings=profile`；
  - 运行中：`jcmd <pid> JFR.start`、`JFR.dump`、`JFR.stop`；
  - 进程内：`jdk.jfr.Recording`、`jdk.jfr.consumer.RecordingStream`。
- **自定义事件**：继承 `jdk.jfr.Event`，在 tick 或请求的入口出口调用 `begin()` / `commit()`。对照 Go 的 `trace.NewTask` / `StartRegion`。

### 三种"方法热点"数据（JDK 25）
| 机制 | 事件 | 原理 | 注意 |
|---|---|---|---|
| 执行采样 + **JEP 518 协作式采样** | `jdk.ExecutionSample` | 采样线程只记录目标线程当时的 PC 和栈指针，目标线程在下一个安全点自己完成栈遍历 | 不再异步遍历栈，更稳定，同时避免了安全点偏差；采样间隔可配置 |
| **JEP 509 CPU 时间采样**（experimental，仅 Linux） | `jdk.CPUTimeSample`、`jdk.CPUTimeSamplesLost` | 按线程**实际消耗的 CPU 时间**采样（基于 Linux 内核的 CPU 定时器），而不是按固定墙钟间隔 | 开启：`-XX:StartFlightRecording=jdk.CPUTimeSample#enabled=true,filename=cpu.jfr`；查看：`jfr view cpu-time-hot-methods` |
| **JEP 520 方法计时与追踪** | `jdk.MethodTiming`、`jdk.MethodTrace` | 对过滤器选中的方法做**字节码插桩**，精确统计调用次数和耗时，或者记录每次调用及其调用栈 | 开启：`-XX:StartFlightRecording:method-timing=com.game.Room::tick`（本机已验证可用），也可以用 `method-trace=...`；`jfr configure` 可以把它们写进自定义的 `.jfc` 文件 |

### 分析工具
- `jfr view <视图> rec.jfr`（以下视图名均已在本机 JDK 25 的 `jfr help view` 中确认）：
  - 热点：`hot-methods`、`cpu-time-hot-methods`、`method-timing`、`method-calls`；
  - 分配：`allocation-by-site`；
  - 锁与阻塞：`contention-by-site`、`contention-by-thread`、`pinned-threads`；
  - GC：`gc-pauses`；
  - 编译：`deoptimizations-by-reason`、`longest-compilations`；
  - 总览：`latencies-by-type`。
- `jfr print --events <事件名>`、`jfr summary`、`jfr scrub`（脱敏后再交给别人）。JMC 用于图形化分析。

### JMH：Java 微基准的唯一正解
- **为什么必须用 JMH**：手写的 `nanoTime` 循环会被 JIT 的死代码消除、常量折叠、循环展开或外提、OSR 编译所扭曲，还会把预热阶段混进测量结果。
- **核心概念**：
  - `@Benchmark`；
  - `@State(Scope.Thread / Scope.Benchmark)`，配合 `@Setup(Level.Trial / Iteration)`；
  - `@Warmup`、`@Measurement`、`@Fork`（每个 fork 是一个新 JVM，用来隔离 profile 污染）；
  - 用 `Blackhole` 消费结果，或者直接返回结果；
  - `@CompilerControl` 控制内联。
- **内置剖析器**：`-prof gc`（每次操作的分配量）、`-prof stack`；Linux 上可以接 async-profiler。
- **本仓库的计划**：Ch15 带教时新建一个最小 Maven 工程 `java-bench/`，用 `org.openjdk.jmh:jmh-java-benchmark-archetype` 生成（本机已装 Maven 3.9）。运行方式：`mvn -q package && java -jar target/benchmarks.jar -f 2 -wi 5 -i 10 -prof gc`。

### async-profiler（第三方，Linux/macOS）
- 结合 `perf_events` 和 HotSpot 的栈遍历接口，可以同时看到 Java 帧和本地帧。支持 CPU、墙钟、分配、锁四种模式，直接输出火焰图。墙钟模式正好用来回答"时间花在哪"。

## 🐹 Go 1.27 深入

### 规范层：`testing` 包的基准契约
- 基准函数签名：`func BenchmarkXxx(b *testing.B)`。标准写法是 `for b.Loop() { ... }`（`testing/benchmark.go` 的文档原话要点）：
  - 第一次调用 `Loop` 时重置计时器，返回 false 时停止计时，所以循环前的 setup 和循环后的清理都不计入；
  - 循环体内**函数调用的参数、返回值和被赋值的变量都会保持存活**，防止编译器把整个循环优化掉。这只对字面上写在花括号里的语句生效，而且条件必须原样写成 `b.Loop()`；
  - 每次测量只执行一次基准函数（传统的 `b.N` 写法要把整个函数执行多次）；
  - `go fix` 的 `bloop` 现代化器可以把旧的 `for i := 0; i < b.N; i++` 写法自动改过来。
- 常用参数：`-bench`、`-benchmem`、`-count=10`、`-benchtime=2s` 或 `-benchtime=1000x`、`-cpu=1,4,8`、`-run=^$`（不跑单元测试）。
- 常用方法：`b.ReportAllocs`、`b.ReportMetric`（自定义指标，如 msgs/s）、`b.RunParallel`。

### 运行时层：每种 profile 怎么来的
| profile | 采样机制 | 默认 | 盲区 |
|---|---|---|---|
| CPU | 信号定时采样（`sigprof` → `cpuProfile.add`），默认 100Hz | 需要显式开始（`pprof.StartCPUProfile` 或 `/debug/pprof/profile?seconds=30`） | 不在 CPU 上的时间（等锁、调度、I/O） |
| heap / allocs | 平均每分配 `runtime.MemProfileRate`（512KB）采一个样本（`mProf_Malloc`）；同一份数据，heap 默认看 `inuse_*`，allocs 默认看 `alloc_*` | 一直开启 | 显示的是最近一次 GC 时的状态；短命对象要看 `alloc_space` |
| block | 阻塞时长超过 `SetBlockProfileRate(ns)` 的事件按比例记录（`blockevent`） | **默认关闭** | 开销与阻塞事件数量成正比 |
| mutex | 竞争事件按 `SetMutexProfileFraction(n)` 以 1/n 采样，栈记在**释放锁的一方**（`mutexevent`，运行时内部锁由 `mLockProfile` 处理） | **默认关闭** | 看到的是"谁持锁太久"，而不是"谁在等" |
| goroutine | 所有 goroutine 的栈快照；`?debug=2` 输出类似 panic 时的完整栈 | 随时可取 | goroutine 数量很大时，抓取本身有成本 |
| goroutineleak | GC 可达性分析（1.27 新增，正式可用，见 [Ch12](../ch12-concurrency-testing/)） | 随时可取 | 通过全局变量可达的原语会漏报 |
| threadcreate | 创建 OS 线程时的栈 | — | 很少用；排查 M 数量暴涨时有用 |

- **标签（labels）**：`pprof.Do(ctx, pprof.Labels("room", id), fn)`、`pprof.WithLabels`、`pprof.SetGoroutineLabels`、`pprof.Label` / `ForLabels`。
  - 标签会被子 goroutine 继承，CPU 样本会带上标签；
  - 分析时用 `go tool pprof -tagfocus=room=1001` 过滤，可以按房间或协议拆分热点；
  - 1.27 起，`go 1.27` 模块的 traceback 头行也会带上标签（[Ch23](../ch23-build-observability/)）。
- **执行追踪 `runtime/trace`**：
  - 记录 goroutine 的调度、阻塞、系统调用、GC、STW、堆大小等事件；
  - 用户注解：`trace.NewTask`、`StartRegion`、`WithRegion`、`Log`；未开启追踪时 `trace.IsEnabled()` 为 false，开销几乎为零；
  - `go tool trace trace.out` 提供：时间线、goroutine 分析、**最小增变器利用率（MMU）**曲线（衡量 GC 对应用的影响）；
  - `go tool trace -pprof=net|sync|syscall|sched trace.out` 生成"等待型" profile，正好补上 CPU profile 的盲区；
  - **1.27 新增**：`-http=:6060` 这种只写端口的形式只监听 localhost，要对外监听需写 `-http=0.0.0.0:6060`。
- **FlightRecorder**（`runtime/trace/flightrecorder.go`）：
  - `trace.NewFlightRecorder(trace.FlightRecorderConfig{MinAge: 5 * time.Second, MaxBytes: 16 << 20})`，然后 `Start()`；
  - 检测到异常时调用 `WriteTo(w)` 把窗口内的 trace 落盘；用 `Enabled()` 查询状态，`Stop()` 停止；
  - `MinAge` 是窗口时长的下限，`MaxBytes` 优先级更高，会覆盖它。
- **HTTP 端点**（`net/http/pprof`）：`/debug/pprof/profile?seconds=30`、`/trace?seconds=5`、`/heap?gc=1`、`/goroutine?debug=2`、`/goroutineleak`；`?debug=N` 大于 0 时输出文本。**只在内网或调试端口暴露**，导入这个包会在 `http.DefaultServeMux` 上注册处理器。
- **`go tool pprof`**：
  - `-http=:8080` 打开网页界面（默认显示火焰图）；
  - `-diff_base old.pprof` 做差异对比，`-base` 做相减；
  - `top -cum`、`list 函数名`、`peek`、`-sample_index=alloc_space`。
- **PGO 采集闭环**（承接 [Ch13](../ch13-alloc-escape-compiler/)）：在生产或压测环境定期采 CPU profile，多份合并（`go tool pprof -proto a.pprof b.pprof > merged.pprof`）后作为 `default.pgo` 提交，构建时自动使用。

### 源码阅读清单
| 文件 → 函数/类型 | 带着什么问题读 |
|---|---|
| `runtime/pprof/pprof.go` → 内置 profile 表、`StartCPUProfile`、`WriteHeapProfile` | 7 种内置 profile 各自从哪里取数据？heap profile 怎样把采样值换算回估计的真实值？ |
| `runtime/mprof.go` → `mProf_Malloc`、`blockevent`、`saveblockevent`、`mutexevent`、`mLockProfile` | block 和 mutex 的采样概率怎样作用？为什么 mutex 的栈记在释放锁的一方？ |
| `runtime/cpuprof.go` → `cpuProfile.add`；`runtime/proc.go` → `sigprof` | 信号到来时运行时做了什么？哪些情况下样本会被丢弃或记成 `runtime._ExternalCode`？ |
| `runtime/pprof/label.go` · `runtime/pprof/runtime.go` → `Do`、`Labels`、`SetGoroutineLabels` | 标签存在哪里？怎样被子 goroutine 继承，又怎样附加到 CPU 样本上？ |
| `runtime/trace/flightrecorder.go` → `NewFlightRecorder`、`Start`、`WriteTo` | 环形窗口怎样维护？同时 `WriteTo` 两次会怎样？ |
| `runtime/trace/annotation.go` → `NewTask`、`StartRegion`、`IsEnabled` | 追踪关闭时注解的开销是多少？task 和 region 的层级关系是什么？ |
| `testing/benchmark.go` → `B.Loop`、`stopOrScaleBLoop` | 迭代次数怎样确定？"保持存活"是怎样在编译器里实现的？ |
| `cmd/trace/main.go` · `internal/trace/traceviewer/mmu.go` | MMU 曲线的横轴和纵轴分别是什么？怎样从曲线上读出"GC 让应用在某个时间窗口内最多损失多少"？ |

## 📊 Java 25 ↔ Go 1.27 机制级对照表

| 维度 | Java 25 | Go 1.27 | 取舍 / 游戏服影响 |
|---|---|---|---|
| 常驻低开销录制 | JFR 持续录制（`default.jfc`） | FlightRecorder（只保存 trace 窗口）+ runtime/metrics | 两边都可以在生产环境常开；JFR 的事件种类更丰富 |
| CPU 热点 | `jdk.ExecutionSample`（JEP 518 协作式）、`jdk.CPUTimeSample`（JEP 509，仅 Linux） | 基于 SIGPROF 的 CPU profile | Java 可以在"按墙钟间隔采样"和"按 CPU 时间采样"之间选择；Go 的 CPU profile 本身就按 CPU 时间计 |
| 精确的方法耗时 | JEP 520 字节码插桩（按过滤器选择方法） | 没有现成的插桩；靠 trace region 或手动埋点 | Java 可以在线上临时给某个方法开精确计时；Go 要提前埋好 region |
| 等待型分析 | `contention-by-site`、`ThreadPark`、`JavaMonitorEnter` 事件，async-profiler 墙钟模式 | block/mutex profile、`go tool trace -pprof=sync/sched/net` | Go 的 block/mutex 默认关闭，要在启动时设置采样率 |
| 分配分析 | `jdk.ObjectAllocationSample`、`allocation-by-site` | heap/allocs profile（512KB 采样） | 两边都是采样，看相对比例，不看绝对值 |
| 调度与 GC 时间线 | JFR 事件时间线（JMC） | `go tool trace` 时间线 + MMU | Go 的 trace 能精确到每个 goroutine 的状态切换，是 Go 的独有利器 |
| 业务维度拆分 | 自定义 JFR 事件的字段 | pprof labels + `-tagfocus` | 都能按房间、协议拆分热点 |
| 微基准框架 | JMH（独立工程，多 fork，可控预热） | `testing` + `B.Loop` + benchstat | Java 必须解决 JIT 预热和 profile 污染；Go 没有预热，但要防止死代码消除和噪声 |
| 统计检验 | JMH 输出误差范围 | benchstat：置信区间 + 显著性检验 | 两边都要多轮、多 fork，报告误差 |
| 生产暴露面 | `jcmd` 本机 attach、JMX（需鉴权） | `/debug/pprof` HTTP 端点 | Go 的端点务必只在内网或带鉴权的调试端口上开放 |

## 🔬 实验

### 实验 1：用 FlightRecorder 抓一次注入的 100ms 卡顿（必做）
- **目的**：建立"常驻录制 → 异常触发 → 事后定位"的流程。
- **方法**（`go/lab/stall/`）：
  - 一个 20Hz 的房间主循环；
  - 随机注入三种卡顿之一：另一个 goroutine 持锁 100ms；一次大块分配突发（引发 assist）；一次阻塞的系统调用；
  - 主循环检测到 tick 超过 50ms 时调用 `fr.WriteTo` 落盘。
- **工具**：`go tool trace stall-<ts>.trace`。在时间线上找到超时的那次 tick，确认元凶是哪个 goroutine、处于什么状态。
- **预期现象**：三类卡顿在 trace 上的特征各不相同：
  - 持锁：tick goroutine 处于阻塞等待（sync）；
  - 分配突发：GC 期间出现 mark assist 区段；
  - 系统调用：P 被移交（handoff），对应的 M 卡在 syscall。
- **记录**：每类卡顿的识别特征（截图或文字描述）、dump 文件大小、开启 FlightRecorder 前后主循环吞吐的 benchstat 对比（即录制开销）。

### 实验 2：同一热点，Go 的 benchstat 与 Java 的 JMH 方法论对照
- **目的**：两边都做出"统计上站得住"的基准，并亲手制造一次"错误的基准"。
- **方法**：选两个热点——协议 varint 编码和 AOI 距离判定：
  - Go：`go/lab/bench/`，`for b.Loop()` + `-count=10` + benchstat；
  - Java：`java-bench/`，JMH `@Fork(2) @Warmup(iterations=5) @Measurement(iterations=10)`，用 Blackhole 消费结果。
  - 再各写一个错误版本：
    - Java：手写 `nanoTime` 循环，结果不使用；
    - Go：传统 `b.N` 循环，把结果赋给局部变量后丢弃，并对被测函数加 `//go:noinline` 和不加各跑一次，观察编译器优化对结果的影响。
- **预期现象**：错误版本出现不可能的数字（比如每次操作不到 1ns），或者波动极大；正确版本的误差范围收敛。
- **记录**：两边的正确结果与误差、错误版本的结果，以及原因分析（JIT 死代码消除、常量折叠、预热）。

### 实验 3：找出并消除锁竞争
- **目的**：用"等待型" profile 定位竞争，并证明修复有效。
- **方法**：
  - Go（`go/lab/contention/`）：8 个 goroutine 读写同一个由 `sync.Mutex` 保护的玩家 map。设置 `runtime.SetMutexProfileFraction(5)` 和 `runtime.SetBlockProfileRate(1)`，抓 mutex 和 block profile；把 map 改成分片锁后重测。
  - Java（`java/lab/Contention.java`）：同样的结构分别用 `synchronized` 和 `ReentrantLock` 实现，用 JFR 录制后 `jfr view contention-by-site`。
- **工具**：`go tool pprof -http=:8080 mutex.pprof`、benchstat、`jfr view contention-by-site`。
- **记录**：修复前后的吞吐（benchstat）、mutex profile 的 top 条目、JFR 的竞争站点表。

### 实验 4（选做，Linux/WSL2）：墙钟采样 vs CPU 时间采样
- **方法**：负载混合 CPU 计算和 I/O 等待。Java 侧对比 `jfr view hot-methods`（执行采样）和 `jfr view cpu-time-hot-methods`（JEP 509），以及 async-profiler 的 `cpu` 和 `wall` 两种模式；Go 侧对比 CPU profile 和 `go tool trace -pprof=net`。
- **预期现象**：CPU 时间采样里看不到等待 I/O 的时间，墙钟采样里可以看到。结论：先确定问题是"CPU 不够"还是"在等东西"，再选工具。

## 🧪 练习规划

**Go：`go/exercises`**
1. `func WithRoomLabels(ctx context.Context, roomID, proto string, fn func(context.Context))`：用 `pprof.Do` 打上 `room` 和 `proto` 两个标签。
   - 测试在 `fn` 内用 `pprof.Label` 读回两个标签；
   - 测试从 `fn` 内启动的子 goroutine 也能读到标签（继承）。
2. `type StallDetector struct{...}`，构造函数 `NewStallDetector(threshold, cooldown time.Duration, dump func() error)`，方法 `Observe(tick time.Duration)`。
   - 超过阈值时调用 `dump`，冷却期内最多调用一次；
   - 测试用 `testing/synctest` 驱动假时钟验证冷却逻辑，用假的 dump 函数计数；
   - `Observe` 在快路径上 `AllocsPerRun` 为 0。
3. `type LatencyHist struct{...}`：固定对数分桶的延迟直方图，提供 `Record(d time.Duration)` 和 `Quantile(q float64) time.Duration`。
   - `Record` 零分配，可以被多个 goroutine 并发调用（用 atomic 实现）；
   - 表驱动测试验证百分位的误差界；
   - 必须通过 `go test -race`（Linux/WSL2）。
4. 修复 `bench_bad_test.go` 里 3 个有缺陷的基准：结果未使用、setup 被计入计时、`b.N` 和 `b.Loop` 混用。
   - 用 `go vet` 和 `go fix -diff`（`bloop` 现代化器）辅助；
   - 交付修复前后的 benchstat 对比和说明（review 验收）。
5. `func NewDebugServer(addr, token string) *http.Server`：在**独立的** ServeMux 上注册 `pprof.Index`、`Profile`、`Trace`、`Symbol`、`Handler("goroutineleak")`，带 token 校验。
   - 测试用 `httptest` 验证：没有 token 返回 403，有 token 返回 200；
   - 测试验证 `addr` 只接受回环地址。

**Java：`java/drills`**
1. `JfrStreamDrill.java`：定义自定义事件 `@Name("game.Tick") class TickEvent extends jdk.jfr.Event { long roomId; }`，每个 tick 都 commit。用 `RecordingStream` 同时订阅这个事件和 `jdk.ExecutionSample`。自校验收到至少 N 个 tick 事件，并且至少有一个执行采样落在热点方法上。
2. `ContentionDrill.java`：8 个线程竞争同一个 `synchronized` 块，用 `RecordingStream` 订阅 `jdk.JavaMonitorEnter`（阈值设为 0）。自校验至少捕获 1 个事件，且事件里的 monitor 类名正确；在注释里对照 Go 的 mutex profile 为什么记录释放方。

**JMH 工程 `java-bench/`**（本章带教时创建）：`VarintBench`、`AoiBench`，与 Go 的 `go/lab/bench` 一一对应。

## 🎮 游戏服落地

1. **线上偶发卡顿**（玩家反馈"偶尔顿一下"）：
   - Go：主循环常驻 FlightRecorder，tick 超过阈值就自动落盘并上报；
   - Java：JFR 持续录制，配合 `jdk.ExecutionSample` 和自定义 tick 事件，用 `jcmd JFR.dump` 导出；
   - 两边都不需要复现问题就能拿到现场。
2. **按房间、按协议拆分热点**："哪个副本 / 哪个协议最耗 CPU"。Go 用 pprof labels 加 `-tagfocus`；Java 在自定义 JFR 事件里带上 roomId 和协议号字段。
3. **压测与版本对比**：每个版本压测时同时采 CPU、heap、mutex profile，与上一个版本做 `-diff_base` 对比。基准测试用 benchstat 做门禁，退化超过阈值就阻止合并。
4. **PGO 闭环**：从生产环境定期采样合并 profile，提交成 `default.pgo`；发布后在 Ch23 的面板上观察 CPU 变化，验证收益。
5. **安全**：调试端口只绑定内网或回环地址并要求鉴权；`jfr scrub` 脱敏后才能把录制文件交给外部。

## ✅ 验收答辩题

- [ ] [L2] Go 的 7 种内置 profile 分别怎样采样？哪两种默认关闭，应该在哪里打开？
- [ ] [L2] heap profile 的 `inuse_space` 和 `alloc_space` 分别回答什么问题？为什么短命对象要看后者？
- [ ] [L2] 为什么 Go 的 mutex profile 把栈记在释放锁的一方？这对定位问题意味着什么？
- [ ] [L2] JEP 518 协作式采样解决了什么问题？JEP 509 的 CPU 时间采样和执行采样有什么区别？JEP 520 为什么要用插桩而不是采样？
- [ ] [L2] `for b.Loop()` 怎样防止基准被优化掉？它和 JMH 的 Blackhole 有什么异同？
- [ ] [L3] 给你一个 `go tool trace` 的截图，指出卡顿是由锁、GC assist、系统调用还是调度延迟造成的，并说明判断依据。
- [ ] [L3] 一张 CPU 火焰图很"平"，没有明显热点，但延迟就是很高。你接下来用什么工具、怎样看？
- [ ] [L3] 用 FlightRecorder 设计一套"自动抓卡顿"的方案：阈值怎么定、窗口怎么设、怎样限流、开销多少（用实验 1 的数据）。
- [ ] [L3] 解释你在实验 2 中看到的"错误基准"数字是怎么产生的，在 Java 和 Go 两边分别说明。
- [ ] [L3] 用 benchstat 的输出说明：一次改动是"显著提升"还是"噪声"？p 值和置信区间怎么读？
- [ ] [L4] 为 30 个服务的游戏后端设计持续剖析方案：采哪些数据、频率、开销预算、存储、权限，以及 PGO profile 怎样回流到构建。
- [ ] [L4] Go 和 Java 的剖析工具链各有什么是对方缺失的？在你的团队里怎样互补？

## 🕳️ 常见坑

- **只跑一次基准就下结论**，或者只看平均值。至少 `-count=10` 再用 benchstat。
- **在 Windows 开发机上剖析，然后推断 Linux 生产环境的行为**：netpoll、定时器、调度的特征都不一样。重要的剖析在 Linux/WSL2 上做。
- **只看 CPU 火焰图**：等锁、等 I/O、等调度的时间不会出现在 CPU profile 里。
- **忘了设置 `SetMutexProfileFraction` 和 `SetBlockProfileRate`**，拿到空的 profile 后以为"没有竞争"。
- **混淆 `inuse` 和 `alloc`**：内存泄漏看 inuse，GC 压力看 alloc。
- **把 `/debug/pprof` 暴露在公网**，或者把 `go tool trace` 绑定到 `0.0.0.0` 后忘了关。
- **Java 不用 JMH 写微基准**；或者 JMH 只用 1 个 fork，被 profile 污染误导。
- **在生产环境长时间开启完整的 `runtime/trace`**：完整追踪有明显开销。常驻用 FlightRecorder，需要时才短时间完整追踪。

## 📚 一手资料

- Go：
  - [Diagnostics](https://go.dev/doc/diagnostics)（官方诊断总览）
  - `go doc runtime/pprof`、`go doc runtime/trace`、`go doc net/http/pprof`、`go doc testing.B.Loop`
  - [Profile-guided optimization](https://go.dev/doc/pgo)
  - [Go 1.27 Release Notes](https://go.dev/doc/go1.27)（Trace 一节）
  - benchstat（`golang.org/x/perf/cmd/benchstat`，Go 团队维护的 x 仓库工具）
- Java：
  - [JEP 509](https://openjdk.org/jeps/509)、[JEP 518](https://openjdk.org/jeps/518)、[JEP 520](https://openjdk.org/jeps/520)
  - `jfr help`、`jfr help view`、`jfr help configure`
  - JMH 官方仓库里的 samples（`JMHSample_08_DeadCode`、`JMHSample_09_Blackholes`、`JMHSample_10_ConstantFold` 等）
  - async-profiler README（第三方）
