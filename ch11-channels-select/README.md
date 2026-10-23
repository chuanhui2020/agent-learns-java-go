# Ch11 · channel 与 select 源码级 ⭐

> Part 2 · 估时 16h · 状态：⚪ 未开始 · channel 不是无锁队列，而是"锁 + 环形缓冲 + 与调度器直接握手"；select 是 Java 没有的那块拼图

## 🎯 目标（资深标准）

- **L2**：能画出 `hchan` 和 `sudog` 的结构，按分支讲清 `chansend` / `chanrecv` / `closechan`：快速失败、直接交接、走缓冲、阻塞入队。能讲清 `selectgo` 的三趟流程，以及 pollorder（随机顺序）和 lockorder（按地址加锁）各自解决什么问题。
- **L2**：能背出 channel 的 happens-before 规则（包括"第 k 次接收先于第 k+C 次发送完成"），并用它解释"用有缓冲 channel 做信号量"为什么是正确的。
- **L3**：先预测再验证：给定生产者 / 消费者拓扑和缓冲容量，预测吞吐、排队延迟、生产者阻塞时间和锁竞争热点；预测 select 在多个分支同时就绪时的选择分布；用 benchmark、goroutine profile、mutex profile 验证。
- **L4**：能讲清 channel / select 的设计取舍：channel 为什么用锁而不是无锁队列、为什么只有有界 channel、select 为什么随机选择就绪分支、为什么只允许发送方关闭；以及 Java 为什么没有 select（`BlockingQueue` 的接口设计如何排除了原子的多路等待）。

## 🔗 前置

[Ch10](../ch10-scheduler/)（gopark / goready / runnext）、[Ch02](../ch02-values-memory/)（值拷贝语义）

## 💡 关键洞察

1. **channel 是带锁的。** `hchan` 里有一把运行时 `mutex`。它的优势不是"无锁、快"，而是**和调度器直接握手**：如果已经有接收者在等，发送方把数据直接拷贝到接收者的栈上，然后把它 `goready` 进 runnext。这样一次交接只需要一次加锁、零次缓冲拷贝，接收方很快就能被调度。
2. **select 才是真正的差异点。** 它能原子地等待"多个 channel 中任意一个就绪"，没被选中的分支不会消费任何东西。Java 没有这个原语：用 `CompletableFuture.anyOf` 去拼多个队列的 `take()`，输掉的那些分支**照样会取走消息**。所以 Java 的设计通常收敛成"一个合并队列 + 带类型标签的事件"。
3. **channel 表达的是所有权转移和协作，不是"线程安全的容器"。** 保护一份共享状态用 mutex；把数据或任务交给另一个执行体、通知事件、限流，用 channel。选错了，两种写法都会变得别扭。

## ☕ Java 25 深度复习

### BlockingQueue 家族的实现（`java.base/java/util/concurrent/`）
| 类 | 实现要点 | 对照 Go |
|---|---|---|
| `ArrayBlockingQueue` | 定长数组环形缓冲 + **单把** `ReentrantLock` + `notEmpty` / `notFull` 两个 `Condition` | 结构上最接近有缓冲 channel（单锁 + 环形缓冲） |
| `LinkedBlockingQueue` | 链表，`putLock` / `takeLock` **两把锁**分离生产和消费；默认容量 `Integer.MAX_VALUE` | Go 没有无界 channel；"默认无界"是 Java 常见的内存事故源 |
| `SynchronousQueue` | 没有容量；在 JDK 25 中内部的 `Transferer` 继承自 `LinkedTransferQueue`，先自旋再 park | 对应无缓冲 channel 的交接语义 |
| `LinkedTransferQueue` | 双队列（dual queue），`transfer()` 等到消费者取走才返回 | 类似"发送方等接收方"的无缓冲语义，但容量无界 |
| `ConcurrentLinkedQueue` | Michael-Scott 无锁队列，不阻塞 | Go 标准库没有对应物 |
| JCTools `MpscArrayQueue` 等（**第三方**） | 单消费者假设下的无锁环形缓冲 | 放宽"多消费者"假设就能去掉锁；Go 标准库不提供这类特化队列 |

- **关闭语义**：Java 队列没有 close。通常用毒丸（poison pill）对象，或者靠中断取消。注意 BlockingQueue 不允许放 `null`，毒丸必须是一个哨兵对象。
- **虚拟线程下的阻塞**：`take()` 最终调用 `LockSupport.park`，虚拟线程会卸载，不占用载体线程。所以"一个虚拟线程 + 一个 BlockingQueue"在结构上和"一个 goroutine + 一个 channel"等价，差别在于没有 select 和 close。
- **`CompletableFuture.anyOf` 模拟 select**（`CompletableFuture.java`）：每个分支都需要一个线程或任务去 `take()`，输掉的分支已经把消息取走了，只能放回去或者丢失；同时还有分配和回调链的开销。这是 Java 侧"没有 select"的真实代价。

### 定时器
- **`ScheduledThreadPoolExecutor`**：`DelayedWorkQueue` 是按触发时间排序的二叉堆，添加 / 取消 O(log n)；`scheduleAtFixedRate` 在任务执行超时后会"追赶"，`scheduleWithFixedDelay` 不会。
- **JDK 25 的 ForkJoinPool 实现了 `ScheduledExecutorService`**：每个池有一个专用的 `DelayScheduler` 线程（`java/util/concurrent/DelayScheduler.java`），维护一个 **4 叉堆**；其他线程提交的定时任务先以 Treiber 栈的方式压进 pending 队列，由调度线程整批取走处理。源码注释写明它针对"大多数任务在到期前就被取消"（I/O 超时）的场景优化。
- **虚拟线程的定时 park**（`Thread.sleep`、`parkNanos`）：默认调度器下由所在 ForkJoinPool 的 `schedule` 安排唤醒；使用自定义调度器时退回到若干个 `ScheduledThreadPoolExecutor`（`VirtualThread.DelayedTaskSchedulers`，数量由 `jdk.virtualThreadScheduler.timerQueues` 控制）。

## 🐹 Go 1.27 深入

### 规范层
- **类型与方向**：`chan T`、`chan<- T`（只发）、`<-chan T`（只收）。双向 channel 可以隐式转换为单向。用单向类型在 API 上表达所有权。
- **操作语义表**（必须背下来）：

| 操作 | nil channel | 已关闭的 channel | 正常 channel |
|---|---|---|---|
| 发送 `ch <- v` | 永久阻塞 | **panic** | 阻塞，直到有接收者或缓冲有空位 |
| 接收 `<-ch` | 永久阻塞 | 先取完剩余数据，之后立即返回零值，`ok == false` | 阻塞，直到有数据 |
| `close(ch)` | **panic** | **panic** | 唤醒所有等待者 |
| `len` / `cap` | 0 | 剩余元素数 / 容量 | 当前值（只是快照，并发下不可靠） |

- **select**：多个分支同时就绪时**伪随机均匀选择**；有 `default` 就不阻塞；`select {}` 永久阻塞。把 channel 变量置为 nil 可以"关掉"对应分支，这是常用技巧。
- **内存模型中的 channel 规则**（go.dev/ref/mem）：
  1. 一次发送 happens-before 对应的接收完成。
  2. `close` happens-before"因为关闭而返回"的接收。
  3. 无缓冲 channel 上，一次接收 happens-before 对应的发送完成。
  4. 容量为 C 的 channel 上，第 k 次接收 happens-before 第 k+C 次发送完成。这条规则是"用有缓冲 channel 实现计数信号量"正确性的依据。

### 运行时与编译器层
- **`hchan` 结构**（`runtime/chan.go`）：`qcount`、`dataqsiz`、`buf`（环形缓冲）、`elemsize`、`closed`、`timer`（定时器 channel 专用）、`elemtype`、`sendx` / `recvx`、`recvq` / `sendq`（`waitq`，一个 `sudog` 双向链表）、`bubble`（synctest 气泡）、`lock`。
- **分配**（`makechan`）：元素大小必须小于 64KB（`1<<16`）。元素类型不含指针时，`hchan` 和缓冲区**一次分配**；含指针时缓冲区单独分配，让 GC 能扫描它。
- **`chansend` 的分支**：
  1. nil channel：阻塞模式下永久 park，非阻塞模式直接返回 false。
  2. 非阻塞且明显发不出去：**不加锁**直接快速失败。
  3. 加锁后，如果已关闭，panic。
  4. `recvq` 里有等待者：调用 `send` → `sendDirect`，**直接把值拷贝到接收者栈上**（`sudog.elem` 指向接收者的变量），然后 `goready`。
  5. 缓冲区有空位：拷贝到 `buf[sendx]`。
  6. 否则：`acquireSudog`，入队 `sendq`，`gopark` 等待。被唤醒后检查是"成功交接"还是"被 close 唤醒"（后者会 panic）。
- **`chanrecv`** 和 `chansend` 对称。有个细节：缓冲区满、同时还有发送者在等时，接收方从**缓冲区头部**取值，再把等待发送者的值放到**缓冲区尾部**，以此保持 FIFO。
- **`closechan`**：设置 `closed`，把所有等待的接收者（拿到零值）和发送者（醒来后 panic）都收集起来，**释放锁之后**再统一 `goready`。源码注释强调，持有 channel 锁时不要改变其他 G 的状态，否则会和栈收缩死锁。
- **select 的编译器改写**（`cmd/compile/internal/walk/select.go` 中的 `walkSelectCases`）：
  - 0 个分支：调用 `block()`。
  - 1 个分支、没有 default：退化为普通的 channel 操作。
  - 1 个分支 + default：改写为 `selectnbsend` / `selectnbrecv`。
  - 其余情况：在栈上构造 `scase` 数组，调用 `selectgo`。
- **`selectgo`**（`runtime/select.go`）：
  - 先用 `cheaprandn` 生成随机的 **pollorder**（保证公平），再按 channel 地址堆排序得到 **lockorder**（保证多把锁总是按同一顺序获取，避免死锁），然后 `sellock` 锁住所有涉及的 channel。
  - **第一趟**：按 pollorder 找已经就绪的分支。
  - **第二趟**：都没就绪，就为每个分支创建一个 `sudog` 挂到对应 channel 的等待队列上，然后 `gopark(selparkcommit)`。
  - **第三趟**：被唤醒后，把自己从其他 channel 的等待队列上摘下来。
  - 推论：select 的成本和分支数成正比，而且每次都要锁住所有 channel。
- **定时器 channel**（`runtime/time.go`、`time/sleep.go`）：
  - 每个 P 有一个定时器堆（`runtime/time.go` 的 `timers`，**4 叉堆**，`timerHeapN = 4`），添加 / 删除 O(log n)，由 `findRunnable` 和 `checkTimers` 驱动触发。和 JDK 25 的 `DelayScheduler` 相比，结构几乎相同，差别在于 Go 没有专用的定时器线程：哪个 M 在跑调度循环，就由它顺手检查本 P 的堆，到期的 goroutine 直接进入就绪队列。
  - `time.Timer.C` 内部实现上是容量为 1 的缓冲 channel，但对用户呈现为**无缓冲**：`len` 和 `cap` 都返回 0（见 `chanlen` / `chancap` 对 `c.timer != nil` 的特判）。这样运行时可以在 `Stop` / `Reset` 时撤回已经放进去的值，保证返回后不会再收到过期的时间。
  - **1.27 新增**：`asynctimerchan` GODEBUG 已删除，定时器 channel 恒为同步语义。
  - 不再被引用的 Timer 可以被 GC 回收，所以 `time.After` 不再导致泄漏。但在热循环里每轮 `time.After` 仍然每次都分配一个定时器，热路径应复用 `Timer` 加 `Reset`。

### 源码阅读清单（`$GOROOT/src`）
| 文件 | 函数 / 类型 | 带着什么问题读 |
|---|---|---|
| `runtime/chan.go` | `hchan`、`waitq`、`makechan` | 为什么无指针元素可以和 `hchan` 一起分配？`hchanSize` 为什么要按 `maxAlign` 对齐？ |
| `runtime/chan.go` | `chansend`、`send`、`sendDirect` | 直接交接时，往别的 goroutine 的栈上写数据，为什么是安全的？需要什么写屏障配合？ |
| `runtime/chan.go` | `chanrecv`、`recv`、`recvDirect` | 缓冲区满且有发送者等待时，怎么保持 FIFO？ |
| `runtime/chan.go` | `closechan` | 为什么要先收集所有 G、释放锁之后再 `goready`？ |
| `runtime/runtime2.go` | `sudog`（`elem`、`isSelect`、`c`） | 一个 G 为什么可能同时挂在多个 `sudog` 上？`isSelect` 用来解决什么竞争？ |
| `runtime/select.go` | `selectgo`、`sellock`、`selunlock`、`selparkcommit`、`block` | pollorder 和 lockorder 各自解决什么问题？同一个 channel 在多个分支里出现时，加锁怎么去重？ |
| `cmd/compile/internal/walk/select.go` | `walkSelectCases` | 哪些 select 形态根本不会调用 `selectgo`？ |
| `runtime/time.go` · `time/sleep.go` | `timer`（`isChan`）、`newTimer`、`(*timer).stop`；`NewTimer`、`After` 的文档注释 | 定时器 channel "实现上有缓冲、表现上无缓冲"是怎么做到的？`Reset` 之后为什么一定不会收到旧值？ |

## 📊 Java 25 ↔ Go 1.27 机制级对照表

| 机制 | Java 25 | Go 1.27 | 取舍 / 设计动机 |
|---|---|---|---|
| 数据结构 | 数组或链表 + `ReentrantLock` + `Condition`（ABQ 单锁，LBQ 双锁） | 环形缓冲 + 运行时 mutex + 两条 `sudog` 等待队列 | channel 要同时维护等待者队列、支持 select 和 close，用短临界区的锁实现最简单可靠 |
| 阻塞与唤醒 | `LockSupport.park` / `unpark`（虚拟线程会卸载） | `gopark` / `goready`，被唤醒的 G 进入 runnext | channel 是运行时的一部分，唤醒时可以直接把接收方放进 runnext；Java 队列只能经过通用的 park/unpark |
| 直接交接 | `SynchronousQueue` / `transfer()` | 有等待者时直接栈到栈拷贝，不经过缓冲 | 运行时知道接收方的栈，所以能省掉一次缓冲拷贝；无缓冲 channel 是"握手"，不是性能缺陷 |
| 容量与背压 | 有界或无界（LBQ 默认无界） | **只有有界**（容量在创建时固定） | 无界队列把背压问题推迟成内存问题；Go 强制在创建时决定容量 |
| 关闭 | 没有 close，用毒丸或中断 | `close`：接收方能取完剩余数据再收到 `ok == false`；向已关闭 channel 发送会 panic | close 是一次广播，所有接收方都能观察到；毒丸只能被一个消费者取走 |
| 多路等待 | 没有原语；`anyOf` 会误消费，只能用合并队列 + 事件类型 | `select`：原子、公平、未选中的分支不消费 | select 需要"同时挂到多个 channel 的等待队列上、只有一个能成功"的协议，这只有运行时统一管理所有 channel 才能做到 |
| 公平性与顺序 | ABQ 可选公平锁；多源之间没有顺序保证 | 单个 channel 内 FIFO；select 在多个就绪分支间随机 | 随机选择防止书写在前的分支饿死后面的；代价是不能表达优先级，要用嵌套 select |
| 内存模型 | 放入队列 happens-before 取出（j.u.c 包文档） | 发送 happens-before 接收完成，外加关闭与容量规则 | Go 把 channel 的同步语义写进了语言的内存模型；Java 写在库文档里 |
| 取消 | `Thread.interrupt()` / `Future.cancel` | `select` 监听 `ctx.Done()` | 取消本身就是一个 channel，可以组合进任意阻塞点（[Ch13](../ch13-context-structured/)） |
| 定时器 | STPE 二叉堆；JDK 25 FJP 的 `DelayScheduler` 4 叉堆 + 专用线程 | 每 P 一个 4 叉堆，由调度循环驱动，没有专用线程 | 同样的数据结构，不同的驱动方式：Go 把定时器检查嵌进调度循环，省掉一次线程间唤醒 |
| 观测 | 线程转储里显示等待在某个 `Condition` 上 | goroutine profile 的等待原因 `chan send` / `chan receive` / `select` | 运行时知道每个 G 为什么 park，所以能按等待原因聚合 |

## 🔬 实验

### 实验 1：channel vs mutex vs 无锁环（必做）
- **目的**：校准直觉：channel 在什么场景下快，什么场景下慢。
- **方法**（`go/lab/queue`）：实现 3 种队列：有缓冲 channel、`sync.Mutex` + slice 环形缓冲、单生产者单消费者的原子环。分别测 1P1C、8P1C、8P8C 三种拓扑。
- **命令**：`go test -bench=. -count=10 -cpu=1,4,8 ./ch11-channels-select/go/lab/queue > out.txt`，再用 `benchstat` 汇总。
- **预期现象**：1P1C 下原子环最快；多生产者高竞争时 channel 和 mutex 都会退化。同时观察批处理（每次传 64 条）带来的数量级变化。
- **记录**：ns/op、allocs/op；用 `-cpuprofile` 看锁竞争热点（`lock2`）。

### 实验 2：缓冲区大小对吞吐和延迟的影响
- **方法**：生产者按突发模式（每 10ms 一次发出 500 条）发送，消费者匀速处理。缓冲容量分别取 0、1、64、1024，记录**每条消息的排队延迟直方图**，以及生产者被阻塞的总时间。
- **预期现象**：容量越大，吞吐越平滑，但尾延迟越高（消息在缓冲里排队）。容量 0 时延迟最低，但生产者被阻塞得最多。
- **记录**：p50 / p99 / p999 延迟、生产者阻塞时间；画出"容量 vs 吞吐 / 排队延迟"曲线，用 Little 定律（队列长度 = 到达率 × 排队时间）解释。

### 实验 3：多生产者单消费者（MPSC），Go vs Java
- **方法**：1000 个生产者向 1 个消费者发消息。
  - Go：有缓冲 channel；"批量排空"版本（先阻塞收一条，再非阻塞最多收 N 条）；`sync.Mutex` + 双缓冲 slice（消费者一次性交换整个 slice）。
  - Java（`java/MpscBench.java`）：虚拟线程生产者，消费者分别用 `LinkedBlockingQueue`、`ArrayBlockingQueue`、`LinkedTransferQueue`、`ConcurrentLinkedQueue` + park。
- **记录**：消息吞吐、生产者入队 p99、消费者 CPU 占用，以及 mutex profile 里的锁竞争。Java 侧注明预热方式，数据只作为初步对照，严谨对比在 [Ch17](../ch17-profiling-bench/) 用 JMH 做。

### 实验 4：select 成本随分支数线性增长
- **方法**：benchmark 1、2、4、8、16 个分支的 select（都不就绪 + default，以及只有一个就绪两种情况）。
- **预期现象**：成本大致和分支数成正比（随机排序、排序、全部加锁）。热路径上不要写巨大的 select。

### 实验 5（选做）：百万定时器的数据结构
- **方法**：100 万个随机到期（1s–60s）的定时任务，其中 50% 在到期前取消。方案：(a) 每个任务一个 `time.AfterFunc`；(b) 单 goroutine 内的单层时间轮（tick = 50ms，不加锁）；(c) Java `ScheduledThreadPoolExecutor`；(d) Java 25 `ForkJoinPool.schedule`。
- **预期现象**：(a)(c)(d) 都是堆，添加 / 取消 O(log n)；时间轮 O(1)，代价是触发误差最多一个 tick。(d) 在大量取消的场景下应该优于 (c)，对照 `DelayScheduler` 源码注释解释原因。
- **记录**：添加、取消、触发的耗时，常驻内存，触发误差分布。

## 🧪 练习规划

**Go：`go/exercises`**（`go test -race ./ch11-channels-select/go/...`）
1. `func Merge[T any](ctx context.Context, ins ...<-chan T) <-chan T`：扇入。所有输入关闭或 ctx 取消后关闭输出。测试：值不丢不重、输出一定会关闭、测试结束后没有遗留 goroutine（在 `testing/synctest` 气泡里运行，断言气泡能正常结束）。
2. `func DrainBatch[T any](ch <-chan T, max int, buf []T) ([]T, bool)`：先阻塞收一条，再非阻塞地收满 `max` 条或收空为止；`bool` 表示 channel 是否已关闭。benchmark 用 `b.Loop`，并用 `testing.AllocsPerRun` 断言 buf 容量足够时零分配。
3. `func Debounce[T any](ctx context.Context, in <-chan T, d time.Duration) <-chan T`：去抖，用一个复用的 `time.Timer` 加 `Reset` 实现。测试必须在 `synctest.Test` 里用假时钟跑，做到确定性、毫秒级完成。
4. `func FirstOf[T any](ctx context.Context, fs ...func(context.Context) (T, error)) (T, error)`：取第一个成功的结果并取消其他任务。测试：慢分支不会泄漏（结果 channel 的容量要足够）、全部失败时用 `errors.Join` 返回所有错误。
5. `type Sem chan struct{}`，带 `Acquire(ctx) error` / `Release()`：用有缓冲 channel 实现信号量。在注释里用内存模型第 4 条规则论证它的正确性；测试并发上限确实不会被突破。

**Java：`java/drills`**
1. `QueueSelectDrill.java`：先用 `CompletableFuture.anyOf` 在两个 `BlockingQueue` 上模拟 select，自校验**确实会丢消息**（输掉的分支已经取走了消息）；再改成"合并队列 + sealed 事件类型 + 模式匹配 switch"的正确版本，自校验消息数守恒。
2. `ClosableChannelDrill.java`：在 `LinkedBlockingQueue` 之上实现 Go 风格的 `close`：关闭后发送抛异常，接收方能取完剩余数据，之后返回 `Optional.empty()`。用虚拟线程做并发自校验。

## 🧭 设计取舍

- **为什么 channel 带锁。** channel 要同时满足 FIFO 的等待者队列、select 的"多 channel 中只成交一个"、close 对所有等待者的广播，还要和调度器交接 goroutine。用无锁结构同时满足这些约束非常复杂，而运行时 mutex 的临界区很短。所以 channel 的定位是"正确、可组合的同步原语"，不是"最快的队列"；极致吞吐的场景应该批处理，或者回到 mutex + slice。
- **为什么只有有界 channel。** 无界队列让生产者永远不阻塞，背压问题被推迟成内存问题，`LinkedBlockingQueue` 默认容量 `Integer.MAX_VALUE` 就是典型。Go 要求在创建时决定容量，0 也是一个合法且常用的选择。
- **为什么 select 随机选择。** 如果按书写顺序选择第一个就绪的分支，排在前面的高频 channel 会让后面的分支永远饿死。随机化给出概率意义上的公平；`lockorder` 按 channel 地址排序加锁，是为了让多个 select 同时锁多个 channel 时不会死锁。代价是 select 不能表达优先级。
- **为什么 Java 没有 select。** `BlockingQueue.take()` 是"取走即消费"的单步操作，没有"先在多个队列上登记、只有一个成交、其余撤销登记"的两阶段协议；要支持 select，所有队列必须共享同一种等待者结构，就像 Go 的 `sudog` 可以同时挂在多个 channel 上。JVM 上的 CSP 实现（例如 Kotlin 协程的 `select`）都是在库里自建一套 channel 类型来做到这一点。
- **为什么只允许发送方关闭。** 关闭是"不会再有数据"的声明，只有发送方知道这件事；向已关闭的 channel 发送会 panic，是为了让"关早了"这种 bug 立刻暴露。1.27 把 timer channel 固定为同步语义（删掉 `asynctimerchan`）也是同一种思路：`Stop` / `Reset` 之后保证收不到旧值，用语义上的保证消灭一类竞态 bug。

## ✅ 验收答辩题

- [ ] [L2] 画出 `hchan` 的字段和两条等待队列，讲清 `chansend` 的 6 个分支。
- [ ] [L2] 什么是"直接交接"？为什么往另一个 goroutine 的栈上写数据是安全的？
- [ ] [L2] 背出 nil / 已关闭 / 正常 channel 在发送、接收、关闭三种操作下的行为表。
- [ ] [L2] `selectgo` 为什么既要随机的 pollorder，又要按地址排序的 lockorder？只保留其中一个会出什么问题？
- [ ] [L2] 用内存模型的容量规则证明"有缓冲 channel 做信号量"是正确的。
- [ ] [L2] 定时器 channel 为什么 `cap` 是 0？`Stop` / `Reset` 之后为什么一定不会收到旧值？
- [ ] [L3] 用实验 1 的数据回答：什么时候用 channel，什么时候用 mutex，什么时候用无锁结构？批处理带来了多少提升？
- [ ] [L3] 写一个因 channel 发送阻塞而不断堆积 goroutine 的程序，分别制造两种根因（消费者太慢 / 消费者已经退出），先预测 goroutine profile 里的等待原因和数量变化，再验证；说明两种根因在 profile 上怎么区分。
- [ ] [L3] 用实验 2 的数据画出"容量 vs 吞吐 / p99 排队延迟"曲线，并用 Little 定律解释曲线的形状。
- [ ] [L3] 一个热循环里写了 `select { case <-time.After(d): ... }`，有什么问题？怎么改？
- [ ] [L4] channel 选择了"带锁、有界、可关闭、可 select"的设计，而不是"无锁、高吞吐的队列"。论证这个取舍，并说明在什么情况下 Go 程序应该放弃 channel，改用 mutex + slice 或批处理。
- [ ] [L4] 为什么说 `CompletableFuture.anyOf` 不是 select？在 Java 里实现"多源事件等待"的正确模式是什么？

## 🕳️ 常见坑

- **接收方关闭 channel，或者多个发送方各自关闭**：会 panic。规则是"只由唯一的发送方关闭"；多个发送方时，用 `sync.WaitGroup` 在最后统一关闭，或者用 ctx 通知退出。
- **把 channel 当成高性能无锁队列**：它有锁，高竞争下要靠批处理或分片。
- **把缓冲开得很大来"解决"阻塞**：只是把背压问题藏进内存和尾延迟里。这是 Java 默认无界 `LinkedBlockingQueue` 习惯的翻版。
- **热循环里每轮 `time.After`**：每次都分配定时器；应该复用 `Timer` 加 `Reset`，或者用 `Ticker`。
- **忘了未初始化的 channel 是 nil**：`var ch chan T` 收发都会永久阻塞。
- **提前 return 导致发送方永远阻塞**：结果 channel 要有足够容量，或者发送方同时 select `ctx.Done()`（泄漏检测见 [Ch14](../ch14-race-leak-detection/)）。
- **以为 select 按书写顺序选择**：多个分支同时就绪时是随机的；要实现优先级，就要用嵌套 select。
- **通过 channel 传递大结构体值**：每次发送都会拷贝（元素大小上限 64KB）；应该传指针，同时明确所有权已经转移。
- **用 `len(ch)` 做业务判断**：它只是一个快照，并发下随时会变。

## 📚 一手资料

- Go：[The Go Memory Model](https://go.dev/ref/mem)（Channel communication 一节）；[Spec · Channel types / Send statements / Select statements](https://go.dev/ref/spec#Select_statements)；`src/runtime/chan.go`、`src/runtime/select.go`、`src/runtime/time.go`；`time/sleep.go` 中 `NewTimer` / `After` 的文档注释；[Go 1.27 Release Notes](https://go.dev/doc/go1.27)；Go Blog：[Go Concurrency Patterns: Pipelines and cancellation](https://go.dev/blog/pipelines)
- Java：`java.base/java/util/concurrent/` 下的 `ArrayBlockingQueue`、`LinkedBlockingQueue`、`SynchronousQueue`、`LinkedTransferQueue`、`CompletableFuture`（src.zip）；`java.util.concurrent` 包文档中关于内存一致性的说明；JCTools 项目文档（第三方）
