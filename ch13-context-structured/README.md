# Ch13 · 取消、超时与结构化并发

> Part 2 · 估时 12h · 状态：⚪ 未开始 · 让每个 goroutine / 虚拟线程都有主人、有截止时间、有退出路径

## 🎯 目标（资深标准）

- **L1**：用 `context` 给阻塞操作加超时与取消；用 `errgroup` / `sync.WaitGroup.Go` 组织并发子任务，保证每个 goroutine 都有退出路径、都有人等待它结束。
- **L2**：讲清 `cancelCtx` 取消树怎么建立（`propagateCancel` 的三条路径）、取消信号怎么向下广播、`Value` 查找为什么是 O(深度)；讲清 Java 25 `StructuredTaskScope` 的 Joiner 决策模型、"取消 = 中断子线程"、`ScopedValue` 绑定如何继承给子任务。
- **L3**：先预测再验证：给定 context 树的形状，预测取消传播延迟和非标准 Context 带来的额外 goroutine；预测 STS 取消时处于不同阻塞状态的子任务能否及时退出；预测 ScopedValue、ThreadLocal、`context.Value` 在百万并发体下的内存和查找成本；用实验数据证实或解释偏差。
- **L4**：能讲清取消与结构化并发的设计取舍：Go 为什么用显式参数传递 context，而不是隐式的 goroutine 本地上下文；为什么 Java 25 在 API 层强制结构化并发（STS），而 Go 只靠约定；为什么两边都只提供协作式取消，都不能强行终止一个执行体。

## 🔗 前置

[Ch08](../ch08-errors-panic/)（错误链）· [Ch10](../ch10-scheduler/)（调度与阻塞）· [Ch11](../ch11-channels-select/)（channel 关闭即广播）· [Ch12](../ch12-sync-memory-model/)（happens-before）

## 💡 关键洞察

1. **Go 的取消是纯协作式的。** `cancel()` 做的事只是关闭一个 channel，再递归关闭子节点。被取消的一方必须自己 `select <-ctx.Done()`，或者把 ctx 传给会检查它的 I/O。**没有任何机制能强杀一个 goroutine。** Java 也是协作式（interrupt），但 STS 关闭时会中断所有未完成的子线程，而且虚拟线程上的阻塞调用大多能响应中断，"取消够得着"的范围更大。
2. **结构化并发是一个约束，不是一个库。** 子任务的生命周期必须严格嵌套在父作用域内。Java 25 用 STS（preview）在 API 层强制这一点：只有 owner 线程能 fork/join，`close()` 必须等所有子线程结束，嵌套关闭顺序不对会抛 `StructureViolationException`。Go 只能靠约定 + `errgroup` / `WaitGroup.Go` + code review 维持，也就是"每个 goroutine 必须有主人"。
3. **`context.Value` 是请求作用域的元数据通道**（trace id、请求 id 这类元数据），不是依赖注入容器。`ScopedValue` 是它的类型安全、不可变、有界作用域版本。

## ☕ Java 25 深度复习

### 中断模型（Java 取消的底座）
- `Thread.interrupt()` 只设置中断标志；`sleep` / `wait` / `BlockingQueue.take` / `LockSupport.park` 这类阻塞点检查到标志后抛 `InterruptedException`（`park` 是直接返回）。
- 吞掉 `InterruptedException` 而不恢复中断标志（`Thread.currentThread().interrupt()`），是 Java 侧"取消信号丢失"的头号原因。对应到 Go，就是 select 里漏写了 `case <-ctx.Done()`。
- 虚拟线程：阻塞在 `java.net` socket 操作上的虚拟线程可以被中断唤醒（具体语义以 JEP 444 文本为准，实验 2 会验证）。平台线程上的经典 `Socket` I/O 不响应中断。这是虚拟线程让"结构化取消"真正可用的前提之一。
- `ExecutorService` 实现了 `AutoCloseable`，`close()` 会等待已提交任务结束；`shutdownNow()` 则中断工作线程。

### JEP 505 结构化并发（第五次预览，需 `--enable-preview`）
- **打开作用域**：`StructuredTaskScope.open()` 使用默认策略：所有子任务都要成功，任一失败就取消作用域，`join()` 抛 `FailedException`。`open(Joiner)` 指定策略，`open(Joiner, cf -> cf.withTimeout(d).withName(n).withThreadFactory(f))` 再加配置。
- **Joiner 内置策略**：
  - `allSuccessfulOrThrow()` → `Stream<Subtask<T>>`
  - `anySuccessfulResultOrThrow()` → `T`（第一个成功的结果，其余取消）
  - `awaitAllSuccessfulOrThrow()` → `Void`
  - `awaitAll()` → `Void`（不因失败取消）
  - `allUntil(Predicate)` → 满足条件就取消其余子任务
- **自定义 Joiner**：实现 `onFork` / `onComplete`，返回 `true` 表示"取消作用域"；再实现 `result()` 产出 `join()` 的返回值。它是一个**决策函数**，和 Go 里"收集 channel 结果、决定何时 cancel"的那段手写逻辑等价。
- **子任务**：`fork` 返回 `Subtask`，有 `state()`（`UNAVAILABLE` / `SUCCESS` / `FAILED`）、`get()`、`exception()`。默认线程工厂创建**虚拟线程**。
- **取消与超时**：取消（Joiner 决定、超时、owner 被中断）会中断所有未完成子任务的线程；超时后 `join()` 抛 `TimeoutException`；`close()` 一定等所有子线程退出后才返回。
- **可观测**：`jcmd <pid> Thread.dump_to_file -format=json <file>` 会把作用域输出成线程容器树。这是 Go 的 goroutine dump 给不出来的父子结构信息。
- **源码**：`java/util/concurrent/StructuredTaskScope.java`（API 与契约）、`StructuredTaskScopeImpl.java`（fork / join / cancel / close 的实现）、`jdk/internal/misc/ThreadFlock.java`（线程"群"：等待所有成员退出的底层设施）、`jdk/internal/vm/ThreadContainer.java`（线程转储里的容器树）。

### JEP 506 作用域值（正式）
- API：`ScopedValue.newInstance()`、`ScopedValue.where(K, v).run(...)` / `.call(...)`（`Carrier` 可以链式绑定多个值）、`get()`、`isBound()`、`orElse`、`orElseThrow`。
- 语义：绑定**不可变**，只在 `run/call` 的动态范围内有效，嵌套作用域可以重新绑定（遮蔽）。`StructuredTaskScope.fork` 出来的子任务**继承** owner 打开作用域时的绑定快照。
- 实现：每个线程持有一份绑定快照链（`ScopedValue.Snapshot`），`get()` 前面有一个小缓存。对比 `ThreadLocal`：后者每个线程一张可变的 `ThreadLocalMap`，生命周期不受控（线程池里极易泄漏）；`InheritableThreadLocal` 在**创建线程时复制整张表**，百万虚拟线程场景下内存和创建成本都会放大。
- 源码：`java/lang/ScopedValue.java`、`jdk/internal/vm/ScopedValueContainer.java`，对照 `java/lang/ThreadLocal.java`。

## 🐹 Go 1.27 深入

### 规范层
- 内存模型：**关闭 channel 同步先于（synchronized before）因该 channel 已关闭而返回的接收**。context 广播取消正是建立在这条 happens-before 边上（[Ch12](../ch12-sync-memory-model/)）。
- `Context` 接口只有四个方法：`Deadline`、`Done`、`Err`、`Value`。`Err()` 只返回 `Canceled` 或 `DeadlineExceeded`；具体原因用 `context.Cause(ctx)` 取（配合 `WithCancelCause` / `WithDeadlineCause` / `WithTimeoutCause`）。
- 约定：ctx 作为第一个参数显式传递；不要传 nil（用 `context.TODO()`）；一般不要存进结构体（长生命周期对象例外，比如一个后台 worker 持有代表自己生命周期的 ctx）；`cancel` 必须调用（`go vet` 的 `lostcancel` 会检查）。

### 运行时与标准库实现层
- **节点类型**（`context/context.go`）：
  - `backgroundCtx` / `todoCtx`：嵌入 `emptyCtx`，`Done()` 返回 nil，表示永远不会被取消。
  - `cancelCtx`：持有父节点、互斥锁、懒创建的 `done`（`atomic.Value`）、`children` 集合、`err`、`cause`。
  - `timerCtx`：`cancelCtx` 加 timer 加 deadline。父节点 deadline 更早时，`WithDeadline` 直接退化成 `WithCancel`。
  - `valueCtx`：单个 key/value 的链表节点。
  - `withoutCancelCtx`、`afterFuncCtx`、`stopCtx`。
- **挂树：`(*cancelCtx).propagateCancel`**，三条路径：
  1. 父节点 `Done()==nil`（永不取消）：什么都不做。
  2. 父节点已经取消：立刻取消子节点。
  3. 通过 `parentCancelCtx`（用 `parent.Value(&cancelCtxKey)` 找到最近的 `*cancelCtx`）把子节点挂进父节点的 `children`；父节点实现了 `afterFuncer` 时改用 `AfterFunc` 注册；**以上都不满足时**（比如自定义 Context 实现），额外起一个 goroutine 监听父节点的 `Done()`。包级计数器 `goroutines` 专门统计这种情况。
- **取消：`(*cancelCtx).cancel`**：加锁设置 `err` / `cause`，关闭 `done`（从未创建过就直接存入包级复用的 `closedchan`），逐个取消子节点（传 `removeFromParent=false`，避免重复加锁），最后从父节点摘除自己（`removeChild`）。
- **`Value` 查找**：包级函数 `value()` 沿父链循环向上，用 type switch 识别各类节点，复杂度 O(深度)。key 应该用未导出的自定义类型，避免冲突。
- **`AfterFunc(ctx, f)`**：ctx 结束后在**新 goroutine** 里执行 f；返回的 `stop()` 能解除注册。典型用法是让不接受 ctx 的阻塞调用响应取消：`stop := context.AfterFunc(ctx, func() { conn.SetReadDeadline(time.Now()) })`。这正是"不接受 ctx 的 API"与 context 衔接的标准方式（deadline 在运行时里的实现见 [Ch18](../ch18-netpoll-io/)）。
- **`WithoutCancel`**：保留 Value，断开取消链。用于父任务结束后仍要完成的收尾工作。
- **`signal.NotifyContext`**：收到操作系统信号时取消 ctx，cause 里带上信号名。也就是说，进程级的"该停了"被翻译成了 context 树根节点的一次取消，后面的传播机制和普通取消完全一样。
- **组织子任务**：
  - `sync.WaitGroup.Go(f)`（标准库）：只负责等待，不传播错误。
  - `golang.org/x/sync/errgroup`（官方扩展库，**不在标准库**）：`WithContext` 在第一个错误时取消派生 ctx，`Go` / `Wait` / `SetLimit` / `TryGo`，提供有界并发。这是 Go 里最接近 `StructuredTaskScope.open()` 默认策略的东西。

### 源码阅读清单
| 文件 | 函数 / 类型 | 带着什么问题读 |
|---|---|---|
| `context/context.go` | `(*cancelCtx).propagateCancel`、`parentCancelCtx` | 三条挂树路径分别什么时候触发？哪条会多起一个 goroutine？为什么要借 `Value(&cancelCtxKey)` 找父节点，而不是直接做类型断言？ |
| `context/context.go` | `(*cancelCtx).cancel`、`removeChild` | 为什么先关闭自己的 done 再递归子节点？递归时为什么传 `removeFromParent=false`？锁的获取顺序怎样避免父子死锁？ |
| `context/context.go` | `(*cancelCtx).Done`、`closedchan`、`init` | done 为什么懒创建？"从没调用过 Done 就被取消"时为什么能复用一个全局已关闭的 channel？ |
| `context/context.go` | `WithDeadlineCause`、`timerCtx.cancel` | 父 deadline 更早时为什么退化为 WithCancel？timer 在哪里 Stop？忘记调 cancel 会泄漏什么？ |
| `context/context.go` | `value`、`valueCtx` | 查找为什么是 O(深度)？"每层中间件都 WithValue 一次"在热路径上的代价是多少？ |
| `context/context.go` | `AfterFunc`、`afterFuncCtx`、`stopCtx` | `stop()` 返回 true/false 各代表什么？f 为什么必须在新 goroutine 里执行？ |
| `os/signal/signal.go` | `NotifyContext` | 信号到达之后，取消是怎样投递到 ctx 的？`stop()` 为什么既要取消 ctx 又要恢复信号的默认行为？ |
| JDK `StructuredTaskScopeImpl.java` | fork / join / close 与取消路径 | Joiner 的 `onComplete` 返回 true 之后发生了什么？close 是怎么等待所有子线程的（对照 `ThreadFlock`）？ |

## 📊 Java 25 ↔ Go 1.27 机制级对照表

| 维度 | Java 25 | Go 1.27 | 取舍 / 设计动机 |
|---|---|---|---|
| 取消信号载体 | 线程中断标志 + `InterruptedException` | `Done()` channel 关闭（广播） | 关闭 channel 是一次广播，可以被任意多个 select 同时观察；Java 的中断是"每线程一个位"，捕获后不重新设置就丢了 |
| 取消能"够到"哪里 | 阻塞点响应中断；STS 取消时主动中断子线程 | 只有显式 select ctx 或接受 ctx 的 API 才响应 | 中断是线程的属性，所有阻塞点都能检查；ctx 是一个参数，只有拿到它的代码才能响应 |
| 结构约束 | STS：owner 才能 fork/join，`close` 等子线程，结构违例抛异常（preview） | 无语言级约束，靠 errgroup / WaitGroup.Go + 约定 | Java 在 API 层强制"子任务不能活过父作用域"；Go 的 `go` 语句保持最简单的形态，结构交给库和约定 |
| 结果汇聚策略 | Joiner（all / any / until / 自定义），声明式 | 手写 channel 收集 + 决定何时 cancel | 把常见策略做成 Joiner，减少"提前 return 导致发送方泄漏"这类手写错误；Go 选择少抽象、多样板 |
| 超时 | `Configuration.withTimeout`，join 抛 `TimeoutException` | `WithTimeout` / `WithDeadline`，自动取消整棵子树 | Go 的 deadline 是一个时间点值，可以被读出、序列化、沿调用链继续传递 |
| 取消原因 | 异常链（`FailedException` 的 cause） | `context.Cause(ctx)` | `Err()` 只区分两类，具体原因放在 `Cause` 里，老代码对 `Err()` 的比较保持成立 |
| 请求作用域值 | `ScopedValue`：类型安全、不可变、有界、继承给子任务 | `context.WithValue`：`any` 类型、链表查找 O(深度) | ctx 不可变、派生要便宜，所以用链表而不是 map；代价是查找随深度变慢，值不宜多 |
| 父子关系的可见性 | JSON 线程转储带作用域树 | goroutine dump 只有扁平列表（可用 pprof labels 补充） | STS 让运行时知道任务树，所以能在转储里呈现；Go 的 goroutine 之间没有父子关系 |

## 🔬 实验

### 实验 1：取消传播延迟与 context 树形状
- **目的**：量化"取消信号从根到叶要多久"，以及非标准 Context 实现引入的额外 goroutine。
- **方法**（`go/lab/cancelprop`）：分别构造深度 1 / 10 / 100、扇出 1 / 100 / 10000 的 context 树，每个叶子挂一个 `<-ctx.Done()` 等待者；测量根 cancel 到全部叶子唤醒的时间。再用一个"包装了 cancelCtx 但没有正确暴露 cancelCtxKey 的自定义 Context"重复实验。
- **工具**：`go test -bench=. -count=10` + `benchstat`；用 `runtime.NumGoroutine()` 观察自定义 Context 情况下额外多出来的 goroutine。
- **预期现象**：延迟随叶子总数近似线性增长（递归取消 + 唤醒），与深度关系较小；自定义 Context 会让 goroutine 数随节点数增长。
- **记录**：ns/叶子、goroutine 增量、`allocs/op`。

### 实验 2：Java 侧"取消能够到哪里"
- **目的**：验证 STS 取消时，处于不同阻塞状态的子任务是否都能及时退出。
- **方法**（`java/CancelReach.java`，需 `--enable-preview`）：在 `anySuccessfulResultOrThrow` 作用域里 fork 四类子任务：`Thread.sleep`、`BlockingQueue.take`、阻塞在本地 socket 读、纯 CPU 循环（不检查中断）。一个子任务快速成功后，测量其余子任务从被取消到退出的时间。
- **预期现象**：前三类很快退出；CPU 循环不会退出，`close()` 会一直等它。对照 Go：同样的 CPU 循环不检查 ctx 也永远不会停。
- **记录**：各类子任务的退出延迟；`jcmd Thread.dump_to_file -format=json` 里的作用域树截图。

### 实验 3：ScopedValue vs ThreadLocal vs context.Value
- **方法**：Java 侧起 100 万个虚拟线程，分别用 `ThreadLocal`、`InheritableThreadLocal`、`ScopedValue` 携带 3 个请求级值，测堆占用（JFR 或 `jcmd GC.heap_info`）和创建耗时；Go 侧 100 万 goroutine 用 3 层 `WithValue` 携带同样的值，测 `runtime/metrics` 堆大小与 `Value` 查找 ns/op。
- **预期现象**：`InheritableThreadLocal` 创建成本和内存最高；ScopedValue 绑定是共享的快照；Go 的值查找随链深线性变慢。

## 🧪 练习规划

**Go：`go/exercises`**（`go test -race ./ch13-context-structured/go/...`，依赖时间的测试用 `testing/synctest`）
1. `func FirstSuccess[T any](ctx context.Context, fns ...func(context.Context) (T, error)) (T, error)`：返回第一个成功结果并取消其余任务，全部失败时用 `errors.Join` 返回。测试点：其余任务确实收到取消、无 goroutine 泄漏（测试结束时检查 goroutine 数），对照 `anySuccessfulResultOrThrow`。
2. `func ReadWithContext(ctx context.Context, conn net.Conn, buf []byte) (int, error)`：用 `context.AfterFunc` + `SetReadDeadline` 让一个不接受 ctx 的阻塞读响应取消，并且能区分"被取消"和"真的读超时"。测试用 `net.Pipe`。
3. `type Lifecycle struct{...}` + `func (l *Lifecycle) Go(name string, fn func(ctx context.Context) error)` + `func (l *Lifecycle) Shutdown(timeout time.Duration) error`：一个最小的"进程级结构化并发"容器。Shutdown 取消根 ctx，在超时内等待所有任务退出，超时则返回未退出任务的名字列表。
4. `func WithRequestID(ctx context.Context, id int64) context.Context` / `func RequestID(ctx context.Context) (int64, bool)`：类型安全的 ctx 值封装（未导出 key 类型），附 benchmark 比较 1 层和 20 层 `WithValue` 的查找开销。

**Java：`java/drills`**
1. `ScopeDrill.java`：用 `StructuredTaskScope.open(Joiner.allSuccessfulOrThrow(), cf -> cf.withTimeout(...))` 并发执行三个子任务，任一失败或超时就整体失败；自校验检查超时后所有子任务都已退出。
2. `CustomJoinerDrill.java`：实现一个"收集到 N 个成功结果就取消其余"的自定义 Joiner（对照练习 1 的 Go 版本）。
3. `ScopedValueDrill.java`：用 `ScopedValue` 携带 traceId，验证嵌套重绑定和 fork 子任务的继承语义。

## 🧭 设计取舍

- **显式参数 vs 隐式上下文。** Go 没有 goroutine-local storage，取消信号和请求元数据只能作为第一个参数一路传下去。代价是几乎每个函数签名都多了 `ctx`；收益是依赖关系写在签名上，也不会因为换了一个 goroutine 执行就悄悄丢失或泄漏上下文。Java 的 `ThreadLocal` / `InheritableThreadLocal` 是隐式传播，ScopedValue 把它收紧成"不可变、有界作用域、只沿结构化的子任务继承"。
- **为什么两边都不能强杀。** 强行终止一个执行体，会让它持有的锁、写到一半的数据结构处于未知状态。Java 很早就弃用了 `Thread.stop`，JDK 25 里它直接抛 `UnsupportedOperationException`；Go 从一开始就没有提供"从外部杀死 goroutine"的能力。所以取消只能是协作式的：被取消方自己检查信号、自己清理、自己返回。
- **结构化并发：API 强制 vs 约定。** STS 规定只有 owner 线程能 fork 和 join，`close()` 必须等所有子任务结束，嵌套顺序错了直接抛 `StructureViolationException`，"子任务不会活过父作用域"由 API 保证。Go 的 `go` 语句可以在任何地方启动一个没有主人的 goroutine，结构只能靠 errgroup、`WaitGroup.Go` 和 code review 维持，泄漏要靠 goroutineleak profile 事后发现（[Ch14](../ch14-race-leak-detection/)）。STS 已经是第五次预览，说明在 API 层把这件事做对并不容易。
- **为什么 `Value` 用链表。** context 是不可变的，每次 `WithValue` 都派生一个新节点；如果每个节点都复制一份 map，派生成本会随值的数量增长。链表让派生是 O(1)，代价是查找 O(深度)，这也在暗示"不要往 ctx 里塞很多值"。ScopedValue 的实现则在每个线程上缓存最近查找过的绑定。
- **为什么 `Err()` 只有两种取值。** `Err()` 只返回 `Canceled` 或 `DeadlineExceeded`，具体原因通过 `WithCancelCause` / `Cause` 单独提供。这样新增"取消原因"不会破坏大量 `err == context.Canceled` 的既有代码，是在兼容性承诺下扩展 API 的典型做法。

## ✅ 验收答辩题

- [ ] [L2] 画出 `WithCancel(WithValue(WithTimeout(Background)))` 的节点结构；调用最外层 cancel 时，哪些节点受影响？哪些锁被获取？
- [ ] [L2] `propagateCancel` 什么情况下会额外起一个 goroutine？怎样写一个自定义 Context 才不会触发它？
- [ ] [L2] `ctx.Err()` 和 `context.Cause(ctx)` 有什么区别？为什么排障日志里应该打 Cause？
- [ ] [L2] STS 的 Joiner `onComplete` 返回 true 之后，子任务线程上会发生什么？`close()` 为什么必须等待它们？
- [ ] [L2] ScopedValue 的绑定为什么能安全地被 fork 出来的子任务继承？为什么 ThreadLocal 做不到同等安全？
- [ ] [L3] 构造一个"父 ctx 已经超时、子 goroutine 还在跑"的程序，分别制造三种原因（没有 select `Done()`、阻塞在不接受 ctx 的调用上、用了错误的 ctx），先预测每种情况在 goroutine dump 里的样子，再逐一验证。
- [ ] [L3] `conn.Read` 不接受 ctx，怎样让它在 ctx 取消后立即返回？先预测 `AfterFunc` + `SetReadDeadline` 方案的竞态窗口（取消和读完成同时发生），再写测试验证你的处理是正确的。
- [ ] [L3] 用实验 1 的数据解释：取消延迟主要由什么决定？什么情况下会出现毫秒级的取消延迟？
- [ ] [L3] `go vet` 的 `lostcancel` 能发现哪些"忘记 cancel"？哪些场景它发现不了？
- [ ] [L4] Go 让 ctx 作为显式参数传遍调用链，Java 用 ThreadLocal / ScopedValue 隐式携带上下文。比较两种设计在依赖可见性、性能、跨 goroutine / 线程边界时的行为，以及各自最容易出的错。
- [ ] [L4] STS + ScopedValue 能覆盖 `context` 的哪些职责？哪些覆盖不了（提示：deadline 作为一个可以读出和传递的值）？反过来，Go 缺少的哪些保证是 STS 提供的？

## 🕳️ 常见坑

- **只 `cancel` 不 `select`**：以为调了 cancel，下游 goroutine 就会停。实际上没人看 `Done()` 的话什么都不会发生。
- **忘记调用 cancel**：`WithTimeout` 返回的 cancel 不调用，timer 和子节点要等到父节点取消才释放。正确写法是在拿到 cancel 的下一行 `defer cancel()`。
- **把 ctx 当依赖注入容器**：往里塞 DB 连接、配置对象，导致函数签名"看不出依赖"，测试也很难构造。
- **用 string 当 ctx key**：不同包之间互相覆盖。要用未导出的自定义类型。
- **在长生命周期对象里随手存请求级 ctx**：请求结束后 ctx 被取消，后续任务"莫名其妙"全部失败。
- **Java 习惯：吞掉中断**：`catch (InterruptedException e) {}` 移植到 Go 就变成 select 里漏写 Done 分支，取消信号在中间断了。
- **errgroup 的 ctx 用错**：子任务里用了外层原始 ctx，而不是 `WithContext` 返回的派生 ctx，第一个错误发生后其余任务不会被取消。

## 📚 一手资料

- Go：`context` 包文档与 `context/context.go` 源码、[Go Concurrency Patterns: Context](https://go.dev/blog/context)、[Contexts and structs](https://go.dev/blog/context-and-structs)、[The Go Memory Model](https://go.dev/ref/mem)（channel close 的 happens-before）、`golang.org/x/sync/errgroup` 文档
- Java：[JEP 505: Structured Concurrency (Fifth Preview)](https://openjdk.org/jeps/505)、[JEP 506: Scoped Values](https://openjdk.org/jeps/506)、[JEP 444: Virtual Threads](https://openjdk.org/jeps/444)（中断与阻塞 I/O 的描述）、`StructuredTaskScope.java` 的类级 Javadoc（契约写得非常完整）
