# Ch11 · Actor 与游戏服线程模型

> Part 2 · 估时 16h · 状态：⚪ 未开始 · 把 Pekko / Netty EventLoop 的"单线程拥有状态"经验，翻译成 goroutine + mailbox，并能讲清每个设计参数的代价

## 🎯 目标（资深标准）

- **L1**：手写一个生产可用的最小 actor：私有状态、有界 mailbox、tell/ask、带超时的 ask、panic 隔离与重启。用它实现房间 actor 和玩家 actor。
- **L2**：讲清 actor 模型给出的保证（单线程处理、每对发送方/接收方之间消息有序）和不给的保证（跨发送方的全局顺序、投递可靠性）；讲清 Go channel mailbox 和 Pekko mailbox / dispatcher 在调度上的差异（goroutine 常驻 vs actor 按批次借用线程）。
- **L3**：能用数据回答"mailbox 容量设多大"、"房间粒度还是玩家粒度"、"百万定时器用什么结构"；能定位 actor 间互相 ask 导致的死锁和 mailbox 堆积。
- **L4**：为一款 MMO / 房间制游戏给出完整的线程模型设计：哪些状态归哪个 actor、跨 actor 交互协议、tick 主循环、背压与降级策略，并说明 Go 与 Java 方案的取舍。

## 🔗 前置

[Ch07](../ch07-scheduler/)（goroutine 调度）· [Ch08](../ch08-channels-select/)（channel 与 timer）· [Ch09](../ch09-sync-memory-model/)（为什么"不共享"就不用锁）· [Ch10](../ch10-context-structured/)（actor 生命周期与 context）

## 💡 关键洞察

1. **一个 Go actor = 一个 goroutine + 一个 mailbox channel + 一个 `for { select {...} }` 消息循环。** 状态只被这个 goroutine 读写，所以不需要锁。这正是 Pekko 的"actor 内单线程"保证，只不过 Go 用**常驻 goroutine** 实现，Pekko 用**按批次借用 dispatcher 线程**实现（`throughput` 参数决定一次处理多少条再让出线程）。
2. **actor 不能消灭并发问题，只是把它们换了个位置**：数据竞争变成了消息顺序问题；锁死锁变成了"互相 ask"死锁；锁争用变成了 mailbox 堆积。资深的价值在于知道问题被换到了哪里。
3. **游戏服的线程模型是"actor + tick"的混合体。** 房间 / 场景需要固定帧率的主循环（战斗、AOI、同步），外部事件通过 mailbox 进入，在 tick 边界统一处理。Go 用 `select` 同时等 ticker 和 mailbox，比 Java 里"EventLoop + scheduleAtFixedRate"写起来更直接。

## ☕ Java 25 深度复习

### Pekko（Apache Pekko，第三方，Akka 2.6 的开源分支）
- **Typed actor**：`Behavior<T>`、`Behaviors.receive` / `setup` / `withTimers`、`ActorContext`；状态通过"返回下一个 Behavior"或可变字段维护。
- **Dispatcher**：默认基于 fork-join 执行器；actor 有消息时被调度到线程上，处理至多 `throughput` 条消息后让出线程。这是"M 个 actor 复用 N 个线程"的协作式调度，**单条消息里不能阻塞**（阻塞 I/O 要放专用 dispatcher）。
- **Mailbox**：默认无界；可配有界（满了以后入队阻塞或超时，具体看配置）、优先级、控制消息优先（control-aware）等类型。
- **监督**：父 actor 决定子 actor 失败后的处理，常用 `restart` / `resume` / `stop` 和带退避的重启；重启时状态丢失，需要从持久化或快照恢复。
- **ask**：返回 `CompletionStage`，内部创建临时 actor 接收回复，必须设置超时。

### LMAX Disruptor（第三方）
- 预分配的 `RingBuffer`、`Sequence`（带缓存行填充，避免伪共享）、`SequenceBarrier`，以及 `BlockingWaitStrategy` / `YieldingWaitStrategy` / `BusySpinWaitStrategy` 等等待策略。
- 单生产者模式下无锁、无分配。它适合"一个逻辑线程吃一个高吞吐事件流"，是游戏服战斗线程的经典 Java 方案。

### Netty EventLoop（第三方）
- `SingleThreadEventLoop`：一个线程串行执行 I/O 事件和任务队列（Netty 用 JCTools 的 MPSC 队列）。`inEventLoop()` 判断当前是否在本线程，跨线程提交就 `execute()`。很多 Java 游戏服直接把"房间"绑定到某个 EventLoop 上实现单线程化。
- `HashedWheelTimer`：哈希时间轮，O(1) 添加 / 取消，精度受 tick 粒度限制，适合海量超时任务。

### JDK 自带
- `ScheduledThreadPoolExecutor`：用 `DelayedWorkQueue`（二叉堆）管理定时任务，添加 / 取消是 O(log n)。`scheduleAtFixedRate` 在任务执行超时后会"追赶"，`scheduleWithFixedDelay` 不会。对比游戏帧循环的"追帧"策略。
- **虚拟线程 per actor 可行吗？** 可以用"虚拟线程 + `BlockingQueue` mailbox"写出和 Go 几乎一样的 actor。代价是：每个 actor 一个 Continuation 栈；`LinkedBlockingQueue` 的锁和 Go channel 的成本不同；没有 `select`，同时等"mailbox + 定时器 + 取消"要绕路（[Ch08](../ch08-channels-select/)）。实验 2 会量化这一点。
- 源码：`java/util/concurrent/ScheduledThreadPoolExecutor.java`、`LinkedBlockingQueue.java`、`LinkedTransferQueue.java`。

## 🐹 Go 1.27 深入

### 规范层
- channel 是 FIFO：同一个发送方 goroutine 先后发出的两条消息，接收方按序收到。**多个发送方之间没有全局顺序。**
- `select` 在多个就绪分支之间**均匀随机**选择，不存在优先级。"控制消息优先"必须自己实现：先非阻塞地检查控制 channel，再进入主 select（反例和正解都要写出来）。
- 向已关闭的 channel 发送会 panic。所以 **mailbox 不应由接收方关闭**；actor 的退出用 ctx 或专门的停止消息表达。

### 运行时与标准库实现层
- **mailbox 的成本模型**：有缓冲 channel 的发送在不阻塞时，是"加锁 + 拷贝进环形缓冲 + 解锁"。如果有等待中的接收者，就直接拷贝到对方栈上并 `goready`（[Ch08](../ch08-channels-select/) 的 `chansend` / `chanrecv`）。消息类型要小：传指针或小结构体，避免大值拷贝。
- **actor goroutine 的唤醒延迟**：被唤醒的 goroutine 进入发送方所在 P 的 `runnext`，通常很快就会被调度。高负载下要考虑本地队列长度和 work stealing（[Ch07](../ch07-scheduler/)）。
- **定时器**：`time.Timer` / `time.Ticker` 由每个 P 的 timer 堆管理（`runtime/time.go`，4 叉堆，`timerHeapN = 4`），添加 / 删除是 O(log n)。timer channel 恒为同步语义（1.27 新增：`asynctimerchan` 已删除），`Stop` / `Reset` 之后不会再读到过期值。`Ticker` 在接收方处理慢时会**丢 tick**，不会累积。
- **panic 隔离**：actor 循环里每处理一条消息就用 `defer` + `recover` 包一层，把 panic 转成"该消息失败 + 上报 + 按策略重启"。注意 `recover` 只在 defer 函数里直接调用才生效（[Ch06](../ch06-errors-panic/)）。
- **ask**：消息里带一个容量为 1 的 reply channel（容量 1 保证回复方永远不会阻塞），调用方用 `select { case r := <-reply: case <-ctx.Done(): }` 等待。超时后回复方仍能写入缓冲，不会泄漏。

### 源码阅读清单
| 文件 | 函数 / 类型 | 带着什么问题读 |
|---|---|---|
| `runtime/chan.go` | `chansend`、`chanrecv`、`hchan` | 有缓冲 mailbox 在"满 / 空 / 有等待者"三种状态下各走什么路径？发送方被阻塞时挂在哪里？ |
| `runtime/select.go` | `selectgo` | 为什么 select 必须随机？"控制消息优先"为什么不能靠分支书写顺序实现？ |
| `runtime/time.go` | `timers`、`addHeap`、`deleteMin`、`timerHeapN` | 每个 P 的 timer 堆如何被调度循环驱动？10 万个房间各一个 Ticker 时，代价在哪里？ |
| `time/tick.go` | `NewTicker`、`Reset` | "丢 tick"在实现上是怎么发生的？对游戏帧循环意味着什么？ |
| `runtime/proc.go` | `goready`、`runqput`（`next` 参数） | actor 被唤醒后为什么常常紧接着就运行？这对 ask 的往返延迟有什么影响？ |
| `runtime/proc.go` | `checkdead` | "all goroutines are asleep - deadlock!" 只在什么条件下触发？为什么线上进程里 actor 互相 ask 的死锁**不会**触发它？ |

## 📊 Java 25 ↔ Go 1.27 机制级对照表

| 维度 | Pekko / Netty / Java 25 | Go 1.27 | 取舍 / 游戏服影响 |
|---|---|---|---|
| actor 执行载体 | 按需借用 dispatcher 线程，`throughput` 条后让出 | 常驻 goroutine，阻塞在 select 上 | Go 的 actor 可以在消息处理中阻塞（只阻塞自己）；Pekko 里阻塞会占住共享线程 |
| 空闲成本 | 无消息时几乎为零（只有 mailbox 对象） | 一个 goroutine 栈（KB 级）+ channel 缓冲 | 百万级空闲实体时 Pekko 更省；Go 要考虑"空闲实体不常驻 goroutine"（按需激活） |
| mailbox | 多种实现（MPSC 无界、有界、优先级、控制优先） | channel（MPMC、有界、FIFO） | Go 要无界或优先级 mailbox 只能自己实现（切片队列 + 信号 channel） |
| 多路等待 | 定时器、消息、取消都统一成消息 | `select` 同时等 mailbox / ticker / ctx | Go 写 tick 循环更自然；Pekko 把一切都变成消息，模型更统一 |
| ask | 临时 actor + `CompletionStage` + 超时 | reply channel（cap=1）+ `select` 超时 | 两边都必须有超时；Go 的版本零框架、易于推理 |
| 监督与重启 | 内建层级监督与退避策略 | 手写 recover + 重启循环 | Go 要自己约定"重启时状态从哪里恢复" |
| 定时器结构 | `HashedWheelTimer`（O(1)）/ STPE 二叉堆 | 每 P 4 叉堆（O(log n)），可自建时间轮 | 海量 buff / CD 计时，两边都倾向在 actor 内部用时间轮，而不是每个计时一个 Timer |
| 跨节点 | Pekko Cluster / Sharding（位置透明） | 无内建，自己做路由 + RPC | Go 需要自研或用框架（[Ch25](../ch25-capstone/)），但也更可控 |
| 单线程事件流极致吞吐 | Disruptor：预分配、无锁、批量消费 | channel 有锁；可用批量出队、环形缓冲替代 | 极致吞吐的战斗线程，Java + Disruptor 依然是强对手，要用数据说话 |

## 🔬 实验

### 实验 1：mailbox 容量 vs 吞吐 / 延迟
- **目的**：为"mailbox 设多大"给出数据依据。
- **方法**（`go/lab/mailbox`）：1 个 actor、P 个生产者（P = 1 / 8 / 64），mailbox 容量取 0 / 1 / 64 / 1024 / 65536，每条消息处理 1µs（自旋模拟）。测吞吐，以及"入队到开始处理"的延迟分布。
- **工具**：`go test -bench` + `benchstat`；延迟用 HDR 直方图风格的分桶统计（自己实现或用第三方库）；`GODEBUG=schedtrace=1000` 观察调度。
- **预期现象**：容量 0 时吞吐最低（每条都要握手）；容量增大后吞吐提升并很快饱和，但排队延迟的长尾随容量线性增长。"大 mailbox"只是把过载藏起来了。
- **记录**：msgs/s、p50 / p99 / p999 排队延迟、goroutine 切换次数。

### 实验 2：ask 往返延迟，Go vs Java 25 虚拟线程
- **方法**：Go 用 reply channel 实现 ask；Java（`java/AskLatency.java`）用"虚拟线程 + `LinkedBlockingQueue` mailbox + `CompletableFuture` 回复"实现同样的 ask。各跑 100 万次往返。
- **预期现象**：两者都在微秒量级，Go 的版本更稳定；Java 侧要注意 JIT 预热（先跑 10 万次再计时，JMH 版本在 [Ch15](../ch15-profiling-bench/) 补）。
- **记录**：p50 / p99 往返延迟；Java 侧 JFR 中 `jdk.VirtualThreadPinned` 事件应为 0。

### 实验 3：百万定时器
- **目的**：比较 buff / 技能 CD 场景下的定时器结构。
- **方法**：100 万个随机到期（1s–60s）的计时任务，其中 50% 会在到期前被取消。方案：(a) 每个计时一个 `time.AfterFunc`；(b) actor 内单层时间轮（tick = 50ms，和帧率对齐）；(c) Java `ScheduledThreadPoolExecutor`；(d) Java 手写时间轮（对照 Netty `HashedWheelTimer` 的思路）。
- **指标**：添加 / 取消的 ns/op、内存占用、到期触发误差分布、GC 压力（Go 看 `gctrace`，Java 看 GC 日志）。
- **预期现象**：(a) 和 (c) 是 O(log n)，内存高；时间轮添加 / 取消 O(1)，触发误差 ≤ 1 个 tick，并且因为在 actor 内触发，不用加锁。

## 🧪 练习规划

**Go：`go/exercises`**（`go test -race ./ch11-actor/go/...`，涉及时间的测试用 `testing/synctest`）
1. `type Actor[S any] struct{...}`、`func Spawn[S any](ctx context.Context, init S, opts ...Option) *Ref[S]`、`func (r *Ref[S]) Tell(fn func(*S)) error`、`func (r *Ref[S]) Ask(ctx context.Context, fn func(*S) any) (any, error)`：泛型最小 actor。测试点：状态只在 actor goroutine 内访问（-race 干净）、mailbox 满时 `Tell` 返回 `ErrMailboxFull`、ctx 取消后 actor 退出、Ask 超时后无泄漏。
2. `WithSupervisor(strategy RestartStrategy)`：消息处理 panic 后，按"最多 N 次 / 时间窗口"策略重启，重启时调用 `OnRestart` 钩子恢复状态。超过上限就停止并上报。
3. `type Room struct{...}` + `func (r *Room) Run(ctx context.Context)`：20Hz tick 主循环。外部输入经 mailbox 进入，在 tick 边界批量处理。处理时间超过一帧时最多追 3 帧，再多就丢帧并计数。用 `synctest` 验证帧数与追帧逻辑。
4. `type TimingWheel struct{...}`、`Add(d time.Duration, fn func()) TimerID`、`Cancel(id TimerID) bool`、`Tick()`：单 goroutine 内使用的时间轮（不加锁）。附 benchmark 与 `AllocsPerRun`，要求 `Add` 稳态零分配。
5. （挑战）`PriorityMailbox`：控制消息（踢人、停服）优先于普通消息。要求写出"只靠 select 分支顺序"的错误版本和正确版本，并用测试证明前者会饿死控制消息。

**Java：`java/drills`**
1. `VirtualActorDrill.java`：用"虚拟线程 + `BlockingQueue`"实现与练习 1 同语义的 actor（tell / ask / 超时），自校验状态一致性。
2. `TickLoopDrill.java`：用 `ScheduledExecutorService` 分别以 `scheduleAtFixedRate` 和 `scheduleWithFixedDelay` 实现 20Hz 房间循环，注入慢帧，验证两者的追帧行为差异，与 Go 版本的帧计数对比。

## 🎮 游戏服落地

- **房间制对战**：一个房间 = 一个 actor，内部 20–60Hz tick；玩家输入是消息；广播在 tick 末尾批量生成并交给网关写协程（[Ch18](../ch18-tcp-gateway/)）。Go 和 Java（每房间绑定一个 EventLoop）都很成熟，Go 的写法更直白。
- **MMO 场景 / 大世界**：玩家 actor 粒度太细，跨实体交互会变成大量 ask；场景 actor 粒度太粗，单线程扛不住。常见折中是"场景按格子或分线拆成多个 actor，玩家实体归属场景 actor，跨场景通过消息迁移"。要用实验 1 的数据估算单个场景 actor 的消息预算。
- **玩家离线状态**：百万注册玩家不能都常驻 goroutine。在线玩家起 actor，离线玩家只在被访问时临时激活（类似 Pekko 的 passivation），空闲超时后存盘并退出。
- **公会 / 排行榜等全局服务**：单 actor 很容易成为热点。按 key 分片成多个 actor，或者读多写少的部分改用 `atomic.Pointer` 快照（[Ch09](../ch09-sync-memory-model/)）。

## ✅ 验收答辩题

- [ ] [L2] actor 模型保证了什么顺序、不保证什么顺序？举一个"两个玩家同时操作同一物品"的例子，说明在 actor 模型下怎么保证正确性。
- [ ] [L2] Go 的 actor 常驻 goroutine，Pekko 的 actor 借用 dispatcher 线程，各自的空闲成本和阻塞容忍度如何？
- [ ] [L2] 为什么 reply channel 的容量应该是 1？换成 0 会在什么情况下泄漏 goroutine？
- [ ] [L2] 为什么不能靠 select 分支顺序实现消息优先级？正确做法是什么？
- [ ] [L3] 两个 actor 互相 ask 导致死锁，线上表现是什么？为什么 runtime 的死锁检测不会报？你如何从 goroutine dump 中定位它？
- [ ] [L3] 某房间 actor 的 mailbox 持续堆积，给出排查步骤：哪些指标、哪些 profile、可能的三个根因。
- [ ] [L3] 用实验 1 的数据说明你会把 mailbox 容量设为多少，以及满了以后采取什么策略（阻塞 / 丢弃 / 断开），依据是什么。
- [ ] [L3] tick 循环处理超过一帧时，"追帧"和"丢帧"各会带来什么问题？`time.Ticker` 的行为是哪一种？
- [ ] [L3] 用实验 3 的数据解释：为什么技能 CD 应该放在 actor 内部的时间轮里，而不是每个 CD 一个 `time.Timer`？
- [ ] [L4] 为一款 5000 人同屏的 MMO 场景设计线程模型：actor 划分、跨 actor 协议、tick 频率、背压与降级。对比 Java（Pekko 或 EventLoop + Disruptor）方案的优劣。
- [ ] [L4] 什么情况下你会放弃 actor，改用"共享状态 + 细粒度锁"或"无锁快照"？给出判断标准。

## 🕳️ 常见坑

- **把 Pekko 的 `become` 状态机生搬到 Go**：Go 里直接用 actor 内的字段和 switch 就行，不需要框架。
- **actor 内调用阻塞 I/O**（同步写 DB、同步 RPC）：一条慢消息会卡住整个房间 tick。要把 I/O 交给别的 goroutine，结果再作为消息投回来。
- **接收方关闭 mailbox**：任何仍在发送的 goroutine 都会 panic。应该用 ctx 或停止消息表达退出。
- **ask 不设超时**：对端 actor 已经退出时调用方永久阻塞。所有 ask 必须带 ctx。
- **消息里传可变共享对象**（比如同一个 `*Player` 指针发给多个 actor）：actor 的隔离性被悄悄破坏，-race 会报，但线上可能只是偶发错乱。消息要么不可变，要么转移所有权。
- **每个实体一个 `time.Ticker`**：10 万实体就是 10 万个运行时 timer 和 10 万次 channel 唤醒。应该由场景 actor 统一 tick。
- **以为 `Ticker` 会补发 tick**：接收慢时它会丢 tick，帧计数要自己维护。
- **无界 mailbox**：从 Java 无界队列搬过来，过载时内存无限增长直到 OOM。

## 📚 一手资料

- Go：[The Go Memory Model](https://go.dev/ref/mem)、[Go Concurrency Patterns: Pipelines and cancellation](https://go.dev/blog/pipelines)、`runtime/chan.go`、`runtime/select.go`、`runtime/time.go`、`time/tick.go` 源码
- Java：Apache Pekko 文档（Actors、Dispatchers、Mailboxes、Fault Tolerance 章节）、LMAX Disruptor 技术论文、Netty `SingleThreadEventLoop` / `HashedWheelTimer` 源码、`ScheduledThreadPoolExecutor.java`
- 时间轮：Varghese & Lauck, *Hashed and Hierarchical Timing Wheels*（1987），Kafka 的分层时间轮实现可作对照
