# Ch10 · GMP 调度器 vs Loom 虚拟线程 ⭐

> Part 2 · 估时 20h · 状态：⚪ 未开始 · 两种 M:N 调度器的同题对决：谁来抢占、栈放哪、阻塞时谁让出线程

## 🎯 目标（资深标准）

- **L2**：能画出 G/M/P/schedt 的关系图和 G 的状态机；能逐步讲清 `schedule → findRunnable` 的找活顺序（runnext → 本地队列 → 全局队列 → netpoll → 偷取）；能讲清 Loom 的 mount/unmount、freeze/thaw，以及默认 ForkJoinPool 调度器怎么工作。
- **L2**：讲清 Go 的协作式加异步（信号）抢占和 10ms 时间片；讲清**虚拟线程没有时间片抢占**这一根本差异。
- **L3**：先预测再验证：给定 GOMAXPROCS 和 goroutine 的计算 / 阻塞模式，预测调度延迟、线程数（M）、CPU 密集 goroutine 对其他 goroutine 的影响，用 `GODEBUG=schedtrace`、runtime/metrics 的 `/sched/latencies:seconds`、`go tool trace` 验证；Java 侧预测 pinning 和载体补偿，用 `jcmd Thread.dump_to_file` 和 JFR 的 `jdk.VirtualThreadPinned` 验证。
- **L4**：能讲清两种 M:N 调度器的设计取舍：Go 为什么要有 P；为什么 Go 必须做异步抢占，而 Loom 可以不做时间片抢占；goroutine 为什么用可增长的连续栈，而虚拟线程把帧冻结到堆上；调度器做在运行时里（Go）还是做在库里（Java）各有什么后果。

## 🔗 前置

[Ch00](../ch00-setup/)–[Ch08](../ch08-errors-panic/)（尤其 [Ch02](../ch02-values-memory/) 的值语义、[Ch08](../ch08-errors-panic/) 的 panic 会终止进程）

## 💡 关键洞察

1. **两者都是 M:N 调度，但抢占模型相反。** Go 运行时会主动抢占跑满 10ms 的 goroutine（栈检查点上协作抢占，加上基于信号的异步抢占）。Java 25 的虚拟线程只在阻塞点让出载体，CPU 密集的虚拟线程会一直占着载体线程。这一条决定了两边放计算任务的方式完全不同。
2. **P 是 Go 调度器的灵魂。** P 把"能跑 Go 代码的资格"和"OS 线程"解耦：系统调用阻塞时 M 被挂住，P 被 sysmon 或 handoff 拿走交给别的 M。P 还承载了 mcache、定时器堆等每 P 本地资源，这正是 Go 分配与定时器能做到少锁的原因（[Ch15](../ch15-alloc-escape-compiler/)、[Ch11](../ch11-channels-select/)）。
3. **虚拟线程是在库层（Java 代码）实现的调度器，加上 JVM 提供的 Continuation 原语。** 你能在 `VirtualThread.java` 里读到完整的状态机；goroutine 则是运行时和编译器协作的产物：函数序言里的栈检查是编译器插入的。

## ☕ Java 25 深度复习

### 虚拟线程 = Continuation + 调度器
- **`java.lang.VirtualThread`**（`java.base/java/lang/VirtualThread.java`）：状态常量包括 `NEW / STARTED / RUNNING / PARKING / PARKED / PINNED / TIMED_PARKING / TIMED_PARKED / TIMED_PINNED / UNPARKED / YIELDING / YIELDED / BLOCKING / BLOCKED / UNBLOCKED / WAITING / WAIT / TIMED_WAITING / TIMED_WAIT / TERMINATED`。读源码时把它画成状态机，重点看 PARKING 到 PARKED（卸载成功）和 PARKING 到 PINNED（卸载失败、钉住载体）两条边。
- **`jdk.internal.vm.Continuation`**（`java.base/jdk/internal/vm/Continuation.java`）：`run()` 在载体线程上挂载执行，`yield` 卸载。卸载时 HotSpot 把栈帧冻结（freeze）到堆上的 stack chunk 对象里，恢复时再解冻（thaw）回载体栈。实现在 HotSpot 的 `src/hotspot/share/runtime/`（continuation 相关代码）。
- **pinning 的原因**：`Continuation.Pinned` 枚举列出了 `NATIVE`（栈上有本地帧）、`MONITOR`、`CRITICAL_SECTION` 等原因。**JEP 491** 之后（Java 25 已包含）在 `synchronized` 块里阻塞和 `Object.wait()` 一般不再钉住载体，因为监视器改为可以由虚拟线程而不是载体线程持有。仍然会 pin 的典型场景：栈上有 native 帧（JNI、FFM upcall 回调里阻塞）、类初始化等。用 JFR 的 `jdk.VirtualThreadPinned` 事件确认，不要凭经验猜。

### 默认调度器
- 默认调度器是一个 **ForkJoinPool**（`java.base/java/util/concurrent/ForkJoinPool.java`，FIFO 异步模式），载体线程是平台线程。
- 可调的系统属性（在 `VirtualThread.java` 中读取）：`jdk.virtualThreadScheduler.parallelism`（默认等于 CPU 核数）、`jdk.virtualThreadScheduler.maxPoolSize`、`jdk.virtualThreadScheduler.minRunnable`、`jdk.virtualThreadScheduler.timerQueues`。
- 遇到无法卸载的阻塞（比如部分文件 I/O），调度器会临时**补偿**额外的载体线程，上限是 `maxPoolSize`。这在思路上对应 Go 的 syscall handoff，但触发点和成本模型不同。
- 网络 I/O：虚拟线程在 socket 上阻塞时，由 JDK 内部的 `sun.nio.ch.Poller` 托管 fd 并卸载线程，就绪后再 unpark（细节见 [Ch18](../ch18-netpoll-io/)）。

### 平台线程的成本基线
- 平台线程与 OS 线程 1:1，默认栈大小由 `-Xss` / `ThreadStackSize` 决定（64 位 Linux 通常是 1MB 的虚拟内存保留）。创建要走系统调用，上下文切换由内核完成。
- 虚拟线程**不应该池化**：用 `Executors.newVirtualThreadPerTaskExecutor()` 每任务一个。真正需要限流时用 `Semaphore`，不要靠线程池大小。

### 诊断工具
- `jcmd <pid> Thread.dump_to_file -format=json <file>`：输出包含虚拟线程的线程转储（传统 `jstack` 看不到大量虚拟线程的全貌）。
- JFR 事件：`jdk.VirtualThreadPinned`（pin 超过阈值时记录）、`jdk.VirtualThreadSubmitFailed`；`jdk.VirtualThreadStart/End` 默认关闭（量太大）。
- `Thread.ofVirtual().name("room-", 0).factory()`：给虚拟线程命名，方便在转储里定位。

## 🐹 Go 1.27 深入

### 规范层
- 规范对 goroutine 只说了一件事：`go` 语句让函数调用"作为独立的并发控制流"在同一地址空间里执行。**函数值和参数在调用方 goroutine 里求值**，之后才交给新 goroutine。
- 规范里**没有** goroutine ID、优先级、线程亲和性，也没有从外部杀死 goroutine 的手段。这是刻意的设计：取消必须协作完成（[Ch13](../ch13-context-structured/)）。
- `main` 函数返回时程序直接退出，不等待其他 goroutine，这和 JVM 等待非守护线程结束的行为相反。

### 运行时与编译器层
- **三个核心结构**（`runtime/runtime2.go`）：
  - `g`：goroutine，包含栈边界、保存的调度上下文 `sched`（gobuf）、状态、等待原因。
  - `m`：OS 线程，带一个专门用来跑调度代码的 `g0` 栈，以及当前 `curg`、绑定的 `p`、`spinning` 标志、`lockedg`。
  - `p`：逻辑处理器，包含 `runq [256]guintptr` 本地环形队列、`runnext`、mcache、定时器堆、GC 工作缓冲。
  - `schedt`：全局调度状态，包括全局队列、空闲 M/P 链表、`nmspinning` 等。
- **G 的状态**：`_Gidle → _Grunnable → _Grunning → _Gsyscall / _Gwaiting → _Gdead`，另外还有 `_Gcopystack`（栈拷贝中）、`_Gpreempted`（被异步抢占后挂起）、`_Gleaked`（被 GC 判定为泄漏，配合 [Ch14](../ch14-race-leak-detection/) 的 goroutineleak profile）。**P 的状态**只有 `_Pidle / _Prunning / _Pgcstop / _Pdead`：在 Go 1.27 中，处于系统调用的 G 所在的 P 仍是 `_Prunning`，由 sysmon 用 `setBlockOnExitSyscall` 抢回。读老文章时看到的 `_Psyscall` 已经不存在了。
- **找活顺序**（`findRunnable`）：
  1. 每 61 次调度检查一次全局队列（`schedtick%61`），防止全局队列饿死。
  2. 本地 `runnext`，然后本地 `runq`。
  3. 全局队列（批量取）。
  4. 非阻塞地 poll 一次 netpoll。
  5. `stealWork`：最多 4 轮（`stealTries = 4`）随机挑 P 偷一半，最后一轮也偷定时器和 runnext。
  6. 都没有活时，释放 P，进入阻塞 netpoll 或 `stopm` 休眠。
- **runnext 与时间片继承**：新创建的 goroutine 和刚被唤醒的 goroutine 放进 `runnext`，并继承当前时间片（`inheritTime`）。这让生产者/消费者"乒乓"时延迟极低，又不会靠 runnext 互相接力霸占 P（共享同一个 `schedtick`）。
- **自旋 M**：`wakep` 在有空闲 P、且没有自旋 M 时唤醒一个 M 去找活。运行时会限制自旋 M 的数量，在"不丢失并行度"和"不空转烧 CPU"之间取平衡。
- **系统调用**：`entersyscall` 时 P 仍挂在 M 上；sysmon 的 `retake` 发现 P 在系统调用里超过一个 sysmon tick（至少 20µs），而且还有别的活要干（或者已经超过 10ms），就把 P 抢走 `handoffp` 给别的 M。`exitsyscall` 先尝试拿回原来的 P，拿不到再找空闲 P，最后把 G 放进全局队列、自己 `stopm`。**后果**：大量阻塞型系统调用或 cgo 调用会让 M 数量增长，默认上限 10000（`sched.maxmcount`，可以用 `debug.SetMaxThreads` 调整），超过上限进程直接崩溃。
- **sysmon**：一个不需要 P 的专用 M。空闲时从 20µs 开始睡眠，逐步翻倍到最多 10ms。职责包括：`retake`（抢占运行超过 `forcePreemptNS = 10ms` 的 G、抢回处于系统调用中的 P）、长时间没人 poll 时补一次 netpoll、按 `forcegcperiod` 强制触发 GC。
- **抢占**：
  - **协作式**：把 `stackguard0` 设成 `stackPreempt`，函数序言的栈检查就会进入 `morestack → newstack`，然后让出。
  - **异步**：`preemptone → preemptM` 向线程发信号（Unix 上是 `SIGURG`；Windows 上挂起线程并修改上下文），在异步安全点（`isAsyncSafePoint`）注入 `asyncPreempt`。所以不含函数调用的死循环也能被抢占。`GODEBUG=asyncpreemptoff=1` 可以关闭异步抢占做对照实验。
- **栈**：初始 2KB（`stackMin = 2048`）。栈不够时 `newstack → copystack` 按 2 倍扩容并**整体拷贝**，调整栈内指针。GC 时 `shrinkstack` 把用量很低的栈缩小。推论：goroutine 的栈地址会移动，所以 C 代码不能持有 Go 栈指针（[Ch19](../ch19-syscall-cgo-ffi/)）。
- **GOMAXPROCS**：默认等于 CPU 数。Linux 上还会参考 cgroup 的 CPU 限额（`GODEBUG=containermaxprocs`），运行中会周期性更新（`updatemaxprocs`）。显式设置环境变量或调用 `runtime.GOMAXPROCS` 后，这两个自动行为都会关闭；`runtime.SetDefaultGOMAXPROCS()` 恢复默认行为。
- **cgroup 感知的实现**（`runtime/cgroup_linux.go` + `internal/runtime/cgroup/`）：启动时 `defaultGOMAXPROCSInit` 读 `/proc/self/cgroup` 和 `/proc/self/mountinfo` 找到所在的 CPU cgroup，打开限额文件并一直保持打开；`defaultGOMAXPROCS` 取"`sched_getaffinity` 得到的逻辑 CPU 数"和"cgroup 平均限额（quota / period，向上取整，最小为 2）"中的较小值。sysmon 每秒最多检查一次（`sysmonUpdateGOMAXPROCS`），限额变了就调整。源码注释写明了两个局限：只读进程所在叶子 cgroup 的限额（父 cgroup 更紧时看不到），以及运行中被迁移到别的 cgroup 不会察觉。
- **`runtime.LockOSThread`**：把 G 和当前 M 绑死。用于要求固定线程的 C 库（例如必须在主线程调用的图形或物理库）。被锁住的 M 不会再跑其他 G。
- **观测**：
  - `GODEBUG=schedtrace=1000,scheddetail=1`：周期打印每个 P 的队列长度和 M 的状态。
  - runtime/metrics：`/sched/latencies:seconds`（G 从可运行到真正运行的等待时间直方图，即**调度延迟**）、`/sched/goroutines/{running,runnable,waiting,not-in-go}:goroutines`、`/sched/threads/total:threads`、`/sched/gomaxprocs:threads`、`/sched/goroutines-created:goroutines`。
  - `go tool trace`：看 Proc 时间线和 G 的阻塞原因（[Ch17](../ch17-profiling-bench/)）。

### 源码阅读清单（`$GOROOT/src`）
| 文件 | 函数 / 类型 | 带着什么问题读 |
|---|---|---|
| `runtime/HACKING.md` | g0、systemstack、nosplit 约定 | 为什么调度代码必须跑在 g0 栈上？哪些函数不能触发栈增长？ |
| `runtime/runtime2.go` | `g`、`m`、`p`、`schedt`、`_G*` / `_P*` 状态 | P 上挂了哪些"每 P 本地资源"？哪些字段只能由持有 P 的 M 修改？ |
| `runtime/proc.go` | `schedule`、`findRunnable`、`stealWork`、`runqput`、`runqputslow`、`runqsteal`、`runqgrab` | runnext 的"时间片继承"怎么防止两个 G 互相接力霸占 P？本地队列满了时为什么搬一半到全局队列？ |
| `runtime/proc.go` | `newproc1`、`gfget`、`malg` | 创建 goroutine 的成本由哪几部分组成？死掉的 G 如何复用？ |
| `runtime/proc.go` | `gopark`、`goready`、`park_m`、`goschedImpl` | 阻塞和唤醒的最小协议是什么？`unlockf` 回调解决的是什么竞态？ |
| `runtime/proc.go` | `entersyscall`、`exitsyscall`、`handoffp`、`retake`、`sysmon` | 一个阻塞 50ms 的系统调用，从进入到返回，P 和 M 各经历了什么？ |
| `runtime/preempt.go` · `runtime/signal_unix.go` · `runtime/os_windows.go` | `suspendG`、`wantAsyncPreempt`、`isAsyncSafePoint`、`preemptM` | 异步抢占为什么只能停在"安全点"？Windows 上没有信号，运行时怎么实现抢占？ |
| `runtime/stack.go` | `newstack`、`copystack`、`shrinkstack`、`stackPreempt` | 协作式抢占为什么能复用栈溢出检查？拷贝栈时怎么找到需要调整的指针？ |
| `runtime/debug.go` | `GOMAXPROCS`、`SetDefaultGOMAXPROCS` | 改变 GOMAXPROCS 为什么需要 STW（`procresize`）？ |
| `runtime/cgroup_linux.go` · `internal/runtime/cgroup/` | `defaultGOMAXPROCSInit`、`defaultGOMAXPROCS`、`adjustCgroupGOMAXPROCS` | 为什么限额小于 2 时要向上取到 2？为什么限额文件在启动时打开、之后反复重读？ |

## 📊 Java 25 ↔ Go 1.27 机制级对照表

| 机制 | Java 25 虚拟线程 | Go 1.27 goroutine | 取舍 / 设计动机 |
|---|---|---|---|
| 实现层级 | JDK 库层调度器（Java 代码）+ HotSpot Continuation 原语 | 运行时调度器 + 编译器插入的栈检查 | Loom 把调度器放在库里，JVM 只提供 Continuation；Go 的调度器和 GC、netpoller、栈增长深度耦合，不可替换但能做跨组件优化 |
| 队列结构 | ForkJoinPool：每个载体一个工作队列 + 提交队列，支持窃取 | 每 P 一个 256 槽本地环形队列 + runnext + 全局队列，偷一半 | 两边都是 work stealing；runnext 让"刚被唤醒的 G"优先运行，对乒乓式消息传递特别友好 |
| 抢占 | **无时间片抢占**，只在阻塞点让出 | 协作抢占 + 异步信号抢占，时间片 10ms | Go 的 GC 需要停下所有 goroutine，必须能抢占紧循环；JVM 的 GC 在载体（平台线程）的安全点上停顿，不依赖虚拟线程调度 |
| 栈 | 运行时在载体栈上；卸载时帧冻结到堆上的 stack chunk | 独立的连续栈，初始 2KB，翻倍拷贝，GC 时收缩 | Go 有精确的栈指针图，可以整体搬动栈；HotSpot 的帧布局绑定原生栈，所以选择"冻结 / 解冻"拷贝 |
| 阻塞系统调用 | 能卸载的走 Poller；不能卸载的由 FJP 补偿载体（上限 maxPoolSize）或 pin | `entersyscall` → sysmon `retake` → `handoffp`，新增 M（上限 10000） | 两边的思路相同：线程被系统调用占住时补一个线程，让并行度不掉；Go 的 P 让"交出运行资格"变得很轻 |
| 锁内阻塞 | JEP 491 后 `synchronized` 不再 pin；j.u.c 锁 park 虚拟线程 | `sync.Mutex` 通过 sema 让 G park，M 不被占用 | Go 的同步原语从一开始就建在调度器之上；Java 要改造已有的 monitor 实现（JEP 491），native 帧里阻塞仍然会 pin |
| 网络 I/O | `sun.nio.ch.Poller` 托管 fd，unpark 虚拟线程 | netpoller 集成进 `findRunnable` 和 sysmon | Go 的调度循环本身会查询 netpoller，少一次跨线程唤醒（[Ch18](../ch18-netpoll-io/)） |
| 并行度 | `jdk.virtualThreadScheduler.parallelism`（默认 `availableProcessors()`，启动时确定） | GOMAXPROCS（默认 CPU 数，感知 cgroup，运行中自动更新） | Linux 上 JVM 启动时按容器限额计算 `availableProcessors()`（`-XX:+UseContainerSupport`，仅 Linux）；Go 把限额变化当成运行期事件 |
| 线程本地 | ThreadLocal 可用，但百万虚拟线程下成本高，推荐 ScopedValue | 没有 goroutine-local storage，靠 `context` 显式传递 | Go 刻意不暴露 goroutine 身份；Java 线程有身份，ThreadLocal 的代价在虚拟线程下被放大（[Ch13](../ch13-context-structured/)） |
| 观测 | `jcmd Thread.dump_to_file -format=json`、JFR `jdk.VirtualThreadPinned` | `schedtrace`、`/sched/latencies:seconds`、trace、goroutine profile | Go 运行时直接维护调度延迟直方图；Java 的观测围绕"线程"展开 |

## 🔬 实验

### 实验 1：百万并发体的内存与创建成本
- **目的**：量化 goroutine 和虚拟线程"便宜"到什么程度，以及钱花在哪。
- **方法**：
  - Go（`go/lab/million`）：起 100 万个 goroutine，每个阻塞在同一个 channel 上，记录创建耗时，用 `runtime.ReadMemStats` 读 `StackInuse`、`Sys`，再读 runtime/metrics 的 `/sched/goroutines:goroutines`。
  - Java（`java/MillionVThreads.java`）：起 100 万个虚拟线程阻塞在 `CountDownLatch` 上，带 `-XX:NativeMemoryTracking=summary`，用 `jcmd <pid> VM.native_memory summary` 看原生内存，用 `GC.heap_info` 看堆。
- **预期现象**：Go 的成本主要在栈（每个至少 2KB）；Java 被卸载的虚拟线程的帧在**堆上**，会体现为堆占用和 GC 压力。
- **记录**：平均单个并发体的字节数（分 Go 栈、Java 堆、原生内存三项）、创建耗时、进程 RSS。

### 实验 2：CPU 密集任务与抢占（必做）
- **目的**：亲眼看到"有抢占"和"无抢占"的差别。
- **方法**：
  - Go：`GOMAXPROCS=1`，起一个纯计算死循环的 goroutine（循环体里不调函数），另一个 goroutine 每 1ms 记录一次时间戳，统计最大间隔。再用 `GODEBUG=asyncpreemptoff=1` 重跑。
  - Java：`bin/jrun -Djdk.virtualThreadScheduler.parallelism=1 ...`，一个虚拟线程做纯计算，另一个虚拟线程 `Thread.sleep(1)` 循环打点。
- **预期现象**：Go 默认情况下最大间隔在 10ms 量级；关闭异步抢占后，打点 goroutine 会被长时间饿死。Java 的打点虚拟线程在计算结束前基本拿不到载体。
- **记录**：三组最大间隔和 p99 间隔；同时用 `/sched/latencies:seconds` 交叉验证。

### 实验 3：阻塞系统调用让 M 暴涨（Linux / WSL2）
- **目的**：观察 syscall handoff 的代价。
- **方法**：用 `syscall.Read` 读一个阻塞模式的 fd（比如没有数据的 FIFO），并发 1000 个 goroutine；每 100ms 读一次 `/sched/threads/total:threads`，同时 `go tool trace` 抓 5 秒。Java 对照：1000 个虚拟线程做同样的阻塞文件读，用线程转储数载体线程。
- **预期现象**：Go 的线程数接近并发数；Java 的载体线程被补偿扩容，但受 `maxPoolSize` 限制，多出来的虚拟线程排队等待。
- **记录**：线程数曲线、trace 里 Proc 与 syscall 的时间线截图。

### 实验 4：调度延迟与 GOMAXPROCS
- **方法**：合成负载：N 个 goroutine 各自按固定周期（如 50ms）醒来做一小段计算，再加 M 个纯计算 goroutine 制造竞争；GOMAXPROCS 分别取 1、2、4、NumCPU，读 `/sched/latencies:seconds` 的 p50/p99/p999。
- **cgroup 验证**（Linux / WSL2）：用 cgroup 把 CPU 限额设为 2（如 `docker --cpus=2` 或 `systemd-run --scope -p CPUQuota=200%`），打印 `runtime.GOMAXPROCS(0)`；运行中再把限额改成 3，观察 1 秒内是否更新；最后设置 `GOMAXPROCS` 环境变量，确认自动更新被关闭。
- **记录**：延迟随 GOMAXPROCS 的变化；cgroup 实验中每一步的 GOMAXPROCS 取值，与 `defaultGOMAXPROCS` 的取整规则对照。

## 🧪 练习规划

**Go：`go/exercises`**（`go test -race ./ch10-scheduler/go/...`）
1. `func HistogramQuantile(h *metrics.Float64Histogram, q float64) float64`：从 runtime/metrics 的直方图里算分位数。用合成的桶数据做表驱动测试，覆盖空直方图、单桶、q=0/1 的边界。
2. `type SchedSnapshot struct{ Running, Runnable, Waiting, Threads uint64 }` 和 `func ReadSched() SchedSnapshot`：读 `/sched/*` 指标。测试先起 N 个阻塞的 goroutine，断言 `Waiting` 至少增加 N，测试结束要清理掉这些 goroutine。
3. `func ParallelFor(n, grain int, body func(i int))`：用 GOMAXPROCS 个 worker 加原子计数器按 `grain` 分块领任务。测试正确性（每个 i 恰好执行一次，过 `-race`）；benchmark（`b.Loop`）对比"每个 i 一个 goroutine"的朴素版本。
4. `func ProbeStarvation(spinners int, window time.Duration) (maxGap time.Duration)`：在 `GOMAXPROCS(1)` 下（用 `t.Cleanup` 恢复）起若干个纯计算 goroutine，测量打点 goroutine 的最大间隔。测试断言 `maxGap < 50ms`，证明异步抢占在起作用。

**Java：`java/drills`**
1. `VirtualThreadDrill.java`：用 `newVirtualThreadPerTaskExecutor` 跑 10 万个任务，自校验全部完成、`isVirtual()` 为真、命名工厂生成的名字符合预期。
2. `NoPreemptionDrill.java`：用 `-Djdk.virtualThreadScheduler.parallelism=1` 运行，自校验"CPU 密集虚拟线程会推迟另一个虚拟线程"（断言延迟超过阈值）。再把计算任务移到平台线程池，断言延迟回落。注释里写清为什么。

## 🧭 设计取舍

- **为什么要有 P。** 早期的 Go 调度器只有 G 和 M，所有 M 共享一个全局加锁的运行队列，M 进入系统调用时它手上的资源也一起被占住。Dmitry Vyukov 的《Scalable Go Scheduler Design Doc》引入 P：把"运行 Go 代码的资格"、本地运行队列、mcache 绑定到 P 上，M 阻塞时 P 可以整体转交给别的 M。Loom 没有 P 的对应物：载体就是 ForkJoinPool 的 worker 线程，阻塞时靠 FJP 的补偿机制临时扩线程。
- **为什么 Go 必须做异步抢占，Loom 可以不做。** Go 的 GC 需要让所有 goroutine 停在安全点上（STW 阶段、栈扫描），一个不调用函数的紧循环会让整个程序卡住，所以 Go 引入了基于信号的异步抢占。JVM 的 GC 在**载体线程**（平台线程）的安全点上停顿，与虚拟线程调度无关。JEP 444 明确写了调度器目前不对虚拟线程做时间分片：虚拟线程面向 I/O 密集任务，CPU 密集任务应留给平台线程。
- **连续可增长栈 vs 冻结到堆。** goroutine 的栈是独立的连续内存，不够时整体拷贝到 2 倍大的新栈，这依赖编译器提供精确的栈指针信息，才能调整栈内指针。HotSpot 的解释器帧和编译帧都建立在原生栈上，JNI 帧更是不能搬动，所以 Loom 选择让虚拟线程直接在载体栈上运行，卸载时把帧复制到堆上的 stack chunk 对象（freeze），重新挂载时再拷回（thaw）。两种方案的成本出现在不同时刻：Go 在栈增长时付，Loom 在每次卸载 / 挂载时付。
- **调度器在运行时里还是在库里。** Go 的调度器与 GC、netpoller、定时器、栈增长深度耦合，用户不能替换，但可以做 runnext、每 P 定时器堆、netpoll 集成进调度循环这类跨组件优化。Loom 的调度器是 Java 代码（`VirtualThread` + `ForkJoinPool`），JVM 只提供 Continuation 原语；JDK 25 只开放了少量系统属性调参，没有公开的自定义调度器 API。
- **为什么 Go 不暴露 goroutine ID。** 没有 goroutine ID，就无法实现 goroutine-local storage，也无法写出"按当前线程身份做决定"的代码；上下文只能通过参数（`context.Context`）显式传递。Java 线程从一开始就有身份（`Thread` 对象、`ThreadLocal`），虚拟线程继承了这套语义，于是百万虚拟线程把 ThreadLocal 的内存和继承成本放大了，才有了 ScopedValue（[Ch13](../ch13-context-structured/)）。

## ✅ 验收答辩题

- [ ] [L2] 画出 G/M/P/schedt 的关系图，说明 P 上有哪些本地资源，以及为什么"P 的数量 = 并行度"。
- [ ] [L2] 按顺序讲出 `findRunnable` 的找活步骤，说明为什么要"每 61 次调度看一次全局队列"。
- [ ] [L2] runnext 的时间片继承解决了什么问题？如果没有它，生产者/消费者乒乓会怎样？
- [ ] [L2] 协作式抢占和异步抢占分别怎么触发？异步抢占为什么只能停在安全点？Windows 上怎么实现？
- [ ] [L2] goroutine 的栈怎么增长、怎么收缩？为什么 C 代码不能保存 Go 栈上的指针？
- [ ] [L2] 讲清虚拟线程从 PARKING 到 PARKED 和到 PINNED 的两条路径；JEP 491 改变了哪一条？Java 25 里还有哪些情况会 pin？
- [ ] [L3] 先预测：1000 个 goroutine 同时阻塞在 `syscall.Read`（阻塞 fd）上，`/sched/threads/total:threads` 会涨到多少？如果改成阻塞在 TCP 连接的 `Read` 上呢？跑实验 3 验证，并用 `entersyscall` 和 netpoller 两条路径的区别解释。
- [ ] [L3] 用实验 2 的数据解释：为什么 Java 25 的虚拟线程里不应该跑重计算，而 Go 里"可以跑但要付出代价"？代价体现在哪个指标上？
- [ ] [L3] 先预测：cgroup CPU 限额为 1.5、宿主机 64 核时，Go 和 Java 默认的并行度分别是多少？手动设置 `GOMAXPROCS=64` 之后，限额变化还会被跟踪吗？用实验 4 的 cgroup 验证部分证明。
- [ ] [L3] 在实验 2 的 `go tool trace` 里找到异步抢占发生的那一刻：trace 上能看到什么？关掉异步抢占（`GODEBUG=asyncpreemptoff=1`）后，打点 goroutine 的最大间隔变成多少？
- [ ] [L4] Go 做异步抢占，Loom 不做时间片抢占。结合两边 GC 让线程停下来的方式（goroutine 安全点 vs 载体线程的 JVM 安全点）论证：为什么 Go 必须能抢占任意 goroutine，而 Java 可以不抢占虚拟线程？
- [ ] [L4] P 这一层解决了什么问题？如果去掉 P、回到 G-M 模型，哪些机制会变差（运行队列、mcache、系统调用交接）？Loom 为什么不需要 P 的对应物？

## 🕳️ 常见坑

- **用线程池的思维池化 goroutine 或虚拟线程来"省创建成本"**。两边创建都很便宜；需要的是**限流**（信号量、有界队列），不是池。
- **找 goroutine ID、想做 goroutine-local storage**：规范里故意没有。上下文要显式传递（`context`）。
- **以为 `main` 返回会等待其他 goroutine**：不会，程序直接退出。要用 `WaitGroup` 或 `errgroup` 等待。
- **在虚拟线程里做 CPU 密集计算**：没有时间片抢占，会饿死同载体上的其他虚拟线程。
- **到处调用 `runtime.Gosched()`**：异步抢占出现之后基本不需要，乱用只会增加调度开销。
- **大量阻塞系统调用或 cgo 调用不限并发**：M 数量撞上 10000 的上限，进程直接崩溃。
- **手动写死 GOMAXPROCS**：会关闭 cgroup 感知和自动更新；值大于 CPU 限额时，线程会被 CFS 限流，表现为周期性的调度延迟尖峰。
- **凭旧经验认为 `synchronized` 一定会 pin 虚拟线程**：Java 25 已经不是这样；但 JNI / FFM 回调里阻塞仍然会 pin。

## 📚 一手资料

- Go：`src/runtime/HACKING.md`；`src/runtime/proc.go` 文件头的调度器设计注释；[runtime/metrics 文档](https://pkg.go.dev/runtime/metrics)；[The Go Programming Language Specification · Go statements](https://go.dev/ref/spec#Go_statements)；[Go 1.27 Release Notes](https://go.dev/doc/go1.27)
- Java：[JEP 444 Virtual Threads](https://openjdk.org/jeps/444)；[JEP 491 Synchronize Virtual Threads without Pinning](https://openjdk.org/jeps/491)；`java.base/java/lang/VirtualThread.java`、`jdk/internal/vm/Continuation.java`、`java/util/concurrent/ForkJoinPool.java`（src.zip）；[JFR 事件参考](https://docs.oracle.com/en/java/javase/25/)（`jdk.VirtualThreadPinned`）
