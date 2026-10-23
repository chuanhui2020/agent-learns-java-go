# Ch18 · netpoller 与 I/O 模型

> Part 4 · 估时 14h · 状态：⚪ 未开始 · 看清"同步写法、异步执行"的 Go 网络栈：一次 `conn.Read` 从用户代码到 `epoll_wait` 再回来的完整路径

## 🎯 目标（资深标准）

- **L1**：熟练组合 `io` 接口族写流式处理代码；正确使用 deadline；能分类处理 `io.EOF`、超时、本端关闭、对端重置四类网络错误。
- **L2**：能按文件和函数画出 `conn.Read` 阻塞 → gopark → epoll 就绪 → 重新调度的完整路径；讲清 `pollDesc` 的 `rg/wg` 状态机，以及 deadline 定时器怎样唤醒读者；讲清 epoll（边沿触发）、kqueue（`EV_CLEAR`）、IOCP（完成端口）三种模型的差异，以及 Go 如何把它们统一起来；讲清 JDK 虚拟线程里阻塞 socket 是怎样被 `Poller` 托管的。
- **L3**：能用 `strace`/`perf`/`runtime/trace`/`runtime/metrics` 拿数据证明 goroutine-per-connection 的真实成本（内存、syscall 次数、就绪到运行的延迟），并给出 10 万连接的容量估算。
- **L4**：能在 goroutine-per-conn、事件驱动库（第三方，如 gnet）、Netty、Java 25 虚拟线程四种模型之间，按连接数、消息频率、延迟目标和团队能力做选型。

## 🔗 前置

[Ch10](../ch10-scheduler/)（GMP、`gopark`/`goready`、`findRunnable`）· [Ch11](../ch11-channels-select/)（运行时定时器）· [Ch08](../ch08-errors-panic/)（错误分类与包装）· [Ch03](../ch03-slice-map-string/)（`[]byte` 缓冲）

## 💡 关键洞察

1. **对用户是同步阻塞，对内核是非阻塞 + 多路复用。** 真正"阻塞"的只是 goroutine（`gopark`），M 和 P 立刻去跑别的 G。所以 goroutine-per-connection 能扛住海量连接。Java 25 的虚拟线程 + `NioSocketImpl` + `sun.nio.ch.Poller` 是同一思路，只是在库层后装的。
2. **Go 在 Linux 上用边沿触发 epoll，fd 打开时一次性注册读写事件，之后不再 `epoll_ctl`。** "必须读到 EAGAIN" 的要求被 `internal/poll` 的读写循环吞掉了。对照：NIO `Selector` 是水平触发语义，要靠反复改 `interestOps`。
3. **单连接成本 = goroutine 栈 + 用户缓冲 + 内核 socket 缓冲。** 栈下限是 2KB（`stackMin = 2048`），运行时还会按平均栈用量调整新 goroutine 的起始栈（`startingStackSize`）；`bufio` 默认 4KB，读写各一个就是 8KB。多数时候大头是缓冲而不是 goroutine，优化应从缓冲策略入手。

## ☕ Java 25 深度复习

### NIO 与 Selector
- `java.nio.channels.Selector` 提供**水平触发**语义：只要 fd 可读，每次 `select()` 都会报告。平台实现：
  - Linux：`EPollSelectorImpl`（在 openjdk 仓库 `src/java.base/linux/classes/sun/nio/ch/` 下，Windows 版 `src.zip` 里没有）；
  - Windows：`WEPollSelectorImpl`（基于 wepoll 适配层）、`WindowsSelectorImpl`；
  - 公共骨架：`SelectorImpl`。
- 写半包要注册 `OP_WRITE`、写完再取消。`interestOps` 的修改和 `wakeup()` 的代价，是 NIO 代码难写难调的根源。
- **heap ByteBuffer 写 socket 会多一次拷贝**：`sun/nio/ch/IOUtil.java` 会用 `Util.getTemporaryDirectBuffer` 中转到 direct buffer。这就是 Netty 偏爱 direct buffer + 池化的原因。

### Netty（第三方，你的老本行，要能讲到机制层）
- 一个 `EventLoop` = 一个线程 + 一个多路复用器 + 任务队列。`Channel` 终身绑定一个 `EventLoop`，**每连接天然串行，handler 内无锁**。
- `ChannelPipeline` 的 inbound/outbound 链；`ByteBuf` 引用计数 + `PooledByteBufAllocator`；`ioRatio` 平衡 I/O 与任务。
- 传输层：NIO transport（JDK Selector，水平触发）vs native epoll transport（边沿触发，支持 `SO_REUSEPORT` 等 Linux 专有选项）。

### 虚拟线程里的阻塞 socket（JDK 25 实现）
- `java.net.Socket` 的底层实现是 `sun/nio/ch/NioSocketImpl.java`（JEP 353 重写的 Socket API）：
  - 在虚拟线程中，`configureNonBlockingIfNeeded` 把 fd 设成非阻塞；
  - 读到 EAGAIN 时进入 `park(fd, event, nanos)`，如果当前是虚拟线程，就调 `Poller.poll(fdVal, event, nanos, ...)`：把 fd 注册到 Poller，然后 park 虚拟线程（Continuation 让出载体线程）。
- `sun/nio/ch/Poller.java` 的两种模式（系统属性 `jdk.pollerMode`）：
  - `SYSTEM_THREADS`：读/写 Poller 是专用平台线程，阻塞等待事件，就绪后 unpark 虚拟线程；
  - `VTHREAD_POLLERS`：Poller 本身也是虚拟线程，无事件时 park，把轮询融进虚拟线程调度。
  - `jdk.readPollers` / `jdk.writePollers` 控制 Poller 数量；默认值由平台的 `PollerProvider` 决定。Windows 版 `DefaultPollerProvider` 只支持 `SYSTEM_THREADS`，平台实现是 `WEPollPoller`。
- JEP 491 之后，`synchronized` 里阻塞不再 pin 载体线程，大量老的阻塞 I/O 代码可以直接放进虚拟线程扩展。
- **对照 Go**：`Poller` ≈ runtime netpoller，`NioSocketImpl.park` ≈ `internal/poll` 的 `pollDesc.waitRead`。本质区别：Go 的 netpoll 是**调度循环的一部分**（`findRunnable` 直接拿到就绪的 G）；JDK 的 Poller 是库层线程，就绪后再走一次 unpark → 提交到调度器。

**源码**（`src.zip`，`java.base/` 下）：`sun/nio/ch/{Poller,PollerProvider,DefaultPollerProvider,NioSocketImpl,SocketChannelImpl,SelectorImpl,IOUtil,Util}.java`、Windows 平台实现 `sun/nio/ch/{WEPollPoller,WEPollSelectorImpl}.java`、`java/nio/channels/Selector.java`、`java/nio/ByteBuffer.java`。

## 🐹 Go 1.27 深入

### 规范层（库契约）
- **`io.Reader` 契约**：`Read` 可能在 `n > 0` 的同时返回 `err`，必须先处理 n 个字节再看 err；`io.EOF` 表示正常结束，不是错误。配套工具：`io.ReadFull`、`io.ReadAtLeast`、`io.LimitReader`、`io.TeeReader`、`io.MultiWriter`、`io.Pipe`（同步、无缓冲）。
- **`io.WriterTo` / `io.ReaderFrom`**：`io.Copy` 的快路径协商。`copyBuffer` 先看 src 有没有实现 `WriterTo`，再看 dst 有没有实现 `ReaderFrom`，都没有才退化成 32KB 缓冲循环。
- **`net.Conn` 的 deadline**：设的是**绝对时间点**，到期后所有阻塞操作返回 `os.ErrDeadlineExceeded`（`net.Error` 的 `Timeout()` 为 true）。重设 deadline 后连接还能继续用。这和 Java 的 `SO_TIMEOUT`（每次读的相对超时）语义不同。
- **并发约定**：同一个 Conn 可以一个 goroutine 读、另一个写；多个 goroutine 并发 `Write` 也不会让 fd 状态损坏（`internal/poll` 的 `fdMutex` 串行化），但**多次 Write 之间会交错**，帧可能被拆散，所以协议层必须单写者（[Ch18](../ch18-tcp-gateway/)）。
- **`bufio`**：默认缓冲 4096 字节；`bufio.Writer` 必须 `Flush`，且不是并发安全的；`bufio.Reader.Peek` 是分帧的利器。
- **1.27 新增**：`net.UnixConn` 的读方法遇到对端关闭时直接返回 `io.EOF`，不再包在 `*net.OpError` 里。

### 运行时与编译器层
- **一次阻塞读的全路径**：
  1. `conn.Read` → `netFD.Read` → `poll.FD.Read`（`internal/poll/fd_unix.go`）；
  2. `syscall.Read` 返回 EAGAIN → `fd.pd.waitRead` → `runtime_pollWait`（链接到 `runtime.poll_runtime_pollWait`）；
  3. → `netpollblock`：把 `pd.rg` 从 `pdNil` CAS 成 `pdWait`，再 `gopark`，提交回调把 `rg` 换成当前 G 的指针 → 调度器去跑别的 G。
- **就绪**：`netpoll(delay)`（`netpoll_epoll.go`）从 `epoll_wait` 拿到事件，对每个 fd 调 `netpollready` → `netpollunblock` 取出等待的 G 放进 `gList`；调度器通过 `injectglist`，或在 `findRunnable` 里直接返回其中一个 G。
- **谁来调用 netpoll**（`runtime/proc.go`）：
  - `findRunnable`：本地/全局队列为空时先非阻塞 `netpoll(0)`；所有 P 都空闲时阻塞 `netpoll(delay)`，delay 取最近的 timer；
  - `sysmon`：超过 10ms 没人轮询就做一次 `netpoll(0)`；
  - STW 结束重启世界时也会轮询。
  - 这决定了"网络就绪 → G 开始运行"的延迟上界。
- **注册方式**：`poll_runtime_pollOpen` → `netpollopen` 在 fd 创建时一次性 `EPOLL_CTL_ADD`，事件为 `EPOLLIN|EPOLLOUT|EPOLLRDHUP|EPOLLET`（边沿触发）；kqueue 用 `EV_CLEAR`；Windows 的 `netpoll_windows.go` 用 IOCP + `GetQueuedCompletionStatusEx`，属于**完成模型**：先提交 overlapped I/O，内核完成后才通知，`internal/poll/fd_windows.go` 的 `execIO` 负责衔接。
- **`pollDesc` 状态机**：`rg/wg` ∈ {`pdNil`、`pdReady`、`pdWait`、G 指针}；`fdseq` 防止 pollDesc 被复用后收到陈旧通知；`rseq/wseq` 防止陈旧的 deadline 定时器误触发。
- **deadline 实现**：`poll_runtime_pollSetDeadline` 设置 `rd/wd` 并启动 `rt/wt` 运行时定时器 → 到期执行 `netpolldeadlineimpl`，把 `rd` 置为 -1 并 `netpollunblock` 唤醒读者 → `netpollcheckerr` 返回超时 → `internal/poll` 的 `convertErr` 转成 `ErrDeadlineExceeded`（由 `os` 包导出）。
- **零拷贝快路径**：
  - `TCPConn.ReadFrom` → `readFrom`（`net/tcpsock_posix.go`）→ Linux 上的 `spliceFrom`（`net/splice_linux.go`），或源是文件时的 `sendFile`（`net/sendfile.go`）；
  - `TCPConn.WriteTo` → `spliceTo`；
  - `net.Buffers.WriteTo` → `poll.FD.Writev`（`internal/poll/writev.go`），批量写，[Ch18](../ch18-tcp-gateway/) 的写合并会用到。
- **栈成本**：`runtime/stack.go` 里 `stackMin = 2048`；`startingStackSize` 每次 GC 按平均栈用量更新。所以"每连接 2KB"只是下限，读循环里调用链很深时会更大。

### 源码阅读清单（`$GOROOT/src`）
| 文件 | 函数/类型 | 带着什么问题读 |
|---|---|---|
| `runtime/netpoll.go` | `pollDesc`、`netpollblock`、`netpollunblock`、`netpollready`、`poll_runtime_pollWait` | 为什么 `rg` 需要 `pdWait` 这个中间态？I/O 就绪和 park 并发发生时，谁保证唤醒不丢？ |
| `runtime/netpoll.go` | `poll_runtime_pollSetDeadline`、`netpolldeadlineimpl`、`netpollcheckerr` | deadline 为什么靠 `seq` 防陈旧，而不是简单地 stop timer？每次读前重设 deadline 的成本是多少？ |
| `runtime/netpoll_epoll.go` | `netpollinit`、`netpollopen`、`netpoll` | 为什么选边沿触发并且只注册一次？`EPOLLRDHUP` 用来做什么？ |
| `runtime/netpoll_windows.go` | `netpollopen`、`netpoll` | 完成模型下，"就绪"是怎样映射成 G 的唤醒的？和 epoll 的本质差别是什么？ |
| `runtime/proc.go` | `findRunnable` 里的 `netpoll` 调用、`sysmon` 里的 `netpoll(0)`、`injectglist` | 一个网络事件最坏要等多久才被处理？什么情况下会更久？ |
| `internal/poll/fd_unix.go` · `fd_mutex.go` | `FD.Read`、`FD.Write`、`fdMutex` | EAGAIN 循环在哪？部分写怎么处理？`fdMutex` 保证了什么，没保证什么？ |
| `internal/poll/fd_poll_runtime.go` | `pollDesc.init`、`pollDesc.wait`、`convertErr` | 超时错误怎样一路变成 `os.ErrDeadlineExceeded`？ |
| `io/io.go` · `net/tcpsock_posix.go` · `net/splice_linux.go` · `net/sendfile.go` | `copyBuffer`、`readFrom`、`spliceFrom`、`sendFile` | `io.Copy` 在什么条件下零拷贝？什么时候退化成 32KB 循环？ |

## 📊 Java 25 ↔ Go 1.27 机制级对照表

| 维度 | Java 25 | Go 1.27 | 取舍 / 游戏服影响 |
|---|---|---|---|
| 编程模型 | Netty：回调 + Future；虚拟线程：同步阻塞 | goroutine 同步阻塞 | 业务可读性 Go ≈ 虚拟线程 > Netty；Netty 胜在对 pipeline 的精细控制 |
| 多路复用器归属 | Selector 是库对象，由 EventLoop 线程驱动；虚拟线程场景由 Poller 线程驱动 | netpoller 是调度器的一部分，`findRunnable`/`sysmon` 驱动 | Go 就绪 G 直接进调度，少一次跨线程 unpark |
| 触发模式 | NIO Selector 水平触发；Netty native epoll 边沿触发 | epoll 边沿触发、一次注册；kqueue `EV_CLEAR` | 边沿触发要求读到 EAGAIN，Go 在 `internal/poll` 内部兜住 |
| Windows 实现 | `WEPollSelectorImpl` / `WEPollPoller`（wepoll 适配） | IOCP 完成模型 | **本机 Windows 的实验结论不能外推到 Linux 线上** |
| 连接与线程关系 | Netty Channel 终身绑定一个 EventLoop 线程 | Conn 不绑定任何 M/P，谁就绪谁跑 | Netty 天然每连接串行；Go 要自己保证单写者、单读者 |
| 超时语义 | `SO_TIMEOUT`（相对）、`IdleStateHandler`/`ReadTimeoutHandler` | 绝对时间点 deadline，按操作重设，超时后连接仍可用 | Go 心跳要在每次读之前重设 deadline |
| 缓冲管理 | `ByteBuf` 池化 + 引用计数；heap → direct 中转拷贝 | `[]byte` 由用户或 `bufio` 管理，GC 回收 | Go 少一层拷贝，但缓冲的生命周期要自己设计（[Ch16](../ch16-zero-alloc-dod/)） |
| 零拷贝 | `FileChannel.transferTo`（sendfile） | `io.Copy` 自动协商 sendfile/splice | 都要求两端是"原生 fd 类型"；中间包一层 wrapper 就退化 |
| 每连接常驻成本 | Netty：Channel 对象 + 缓冲，无线程；虚拟线程：栈 chunk 在堆上 | goroutine 栈（≥2KB，按需增长）+ 缓冲 | 10 万连接三者都可行，差别主要在缓冲策略 |

## 🔬 实验

### 实验 1：10 万长连接的内存账（**必须在 Linux/WSL2**）
- **目的**：拿到每连接的真实内存构成，破除"goroutine 很重 / 很轻"的直觉。
- **方法**：`go/lab/conns/` 写服务端，每连接一个读 goroutine，阻塞在 `Read`；客户端另起进程建 10 万连接（本地端口不够时绑定多个回环地址 `127.0.0.x`，并先 `ulimit -n`）。Java 侧 `java/ConnsVT.java` 用虚拟线程 per-conn 实现同样的服务。Netty 版可选（需 Maven）。
- **工具**：Go 用 `runtime/metrics` 的 `/memory/classes/heap/stacks:bytes`、`/sched/goroutines:goroutines` 加 RSS；Java 用 `-XX:NativeMemoryTracking=summary` + `jcmd <pid> VM.native_memory summary`，再看堆用量。
- **预期**：空闲连接每个在几 KB 量级；把 `bufio` 从 4KB 改成 64KB，内存会明显上升，说明缓冲才是大头。
- **记录**：每连接栈 / 堆 / 缓冲 / RSS 增量；两边的连接建立速率。

### 实验 2：网络就绪到运行的调度延迟
- **目的**：量化"就绪 → 被调度"的延迟，验证 `findRunnable`/`sysmon` 的轮询时机。
- **方法**：echo 服务 + 压测客户端，采 `runtime/trace`；分别在空闲、`GOMAXPROCS=1`、CPU 打满（加若干 CPU 密集 goroutine）三种场景下运行。
- **工具**：`go tool trace`，看 goroutine 从 "blocked on network" 到 running 的时间分布。
- **预期**：CPU 饱和时延迟显著上升，并出现 sysmon 10ms 量级的长尾。
- **记录**：三种场景下的 p50/p99。

### 实验 3：缓冲策略与 syscall 次数（**Linux/WSL2**）
- **目的**：证明小包场景下瓶颈在 syscall 次数，而不是 goroutine。
- **方法**：`go/lab/writes/` 每条消息 32 字节、连发 10 万条，三种写法：直接 `conn.Write`、`bufio.Writer`（4KB）、攒批后 `net.Buffers.WriteTo`。
- **工具**：`strace -c -f` 或 `perf trace -s` 统计 write/writev 次数；`go test -bench -count=10` + `benchstat` 看吞吐。
- **记录**：syscall 次数、吞吐、尾延迟。这组数据是 [Ch18](../ch18-tcp-gateway/) 写合并设计的基线。

### 实验 4：deadline 的成本与精度
- **方法**：在 `B.Loop` 里每次读前 `SetReadDeadline`，测单次开销；再实测"设 50ms 超时"实际触发时间的误差，Windows 与 Linux 各跑一遍。
- **预期**：开销在百纳秒量级；Windows 上 IOCP 超时精度较差（`netpoll_windows.go` 里有注释说明）。

### 实验 5（选做）：`io.Copy` 零拷贝的生效与退化（Linux）
- **方法**：文件 → TCP 用 `io.Copy`，`strace` 观察 `sendfile`；在源外面包一层自定义 Reader 再测，观察退化成 read/write 循环。

## 🧪 练习规划

**Go：`go/exercises`**（网络类用 `net.Pipe` 或本地回环；并发类在 Linux/WSL2 下加 `-race`）
1. `type CountingReader struct { R io.Reader; N int64 }` 实现 `Read`：测试覆盖"`n > 0` 且 `err != nil`"的同次返回、`io.EOF` 透传、配合 `io.Copy` 计数正确。
2. `func ClassifyNetErr(err error) ErrKind`：区分 `KindEOF`、`KindTimeout`（`errors.Is(err, os.ErrDeadlineExceeded)` 或 `net.Error.Timeout()`）、`KindClosed`（`net.ErrClosed`）、`KindOther`。测试用 `net.Pipe` 构造各类场景。
3. `func ProxyWithIdleTimeout(dst, src net.Conn, idle time.Duration) (int64, error)`：每次读前重设 deadline，src 正常关闭时返回 `nil`，空闲超时返回能被 `errors.Is(err, os.ErrDeadlineExceeded)` 命中的错误。测试用 `net.Pipe`，并断言没有 goroutine 残留。
4. `func ReadLineLimited(r *bufio.Reader, max int) ([]byte, error)`：超长行返回 `ErrLineTooLong`，不能无限增长内存（防御恶意客户端）。附 fuzz 测试。
5. `BenchmarkSmallWrites`：直接写 / `bufio` / `net.Buffers` 三种实现写入 `io.Discard` 包装的计数 Writer，统计"底层 Write 调用次数 / op"，用 `testing.AllocsPerRun` 断言 `net.Buffers` 路径的分配上限。

**Java：`java/drills`**
- `VirtualThreadEchoDrill.java`：用 `Executors.newVirtualThreadPerTaskExecutor()` 写 echo 服务和 1000 个并发客户端，自校验所有回显正确；用 `setSoTimeout` 触发 `SocketTimeoutException` 后，断言连接仍可继续读写（对照 Go 的 deadline 重设）。
- `SelectorEchoDrill.java`：手写 NIO Selector echo，正确处理写半包（注册 / 取消 `OP_WRITE`）。体会 Go 的 `internal/poll` 替你藏起了什么。

## 🎮 游戏服落地

- **网关容量规划**：10 万在线时，每连接一个读 goroutine + 一个写 goroutine（或共享写协程池，见 [Ch18](../ch18-tcp-gateway/)）。内存预算 = 栈 + 读写缓冲 + 写队列 + 内核缓冲（`net.core.rmem_default/wmem_default`）。取舍：Netty 共享 EventLoop 线程，内存更紧凑但代码复杂；Go 代码直观，可以用"空闲时不持有大缓冲"（读到数据再从池里借）把内存压下来。
- **服务器间内部长连接**：连接少、吞吐高，瓶颈在 syscall 次数和写合并，不在 goroutine 数。优先做批量写和协议层的消息聚合。
- **心跳与踢人**：每次读之前 `SetReadDeadline(now + 3×心跳间隔)`，超时即判定掉线。Java 侧对应 `IdleStateHandler`。注意 deadline 是绝对时间，必须每次重设。
- **开发机与线上不一致**：本机 Windows 走 IOCP，线上 Linux 走 epoll，连接数、延迟、零拷贝行为都不同。压测和容量结论必须在 Linux 上得出。

## ✅ 验收答辩题

- [ ] [L2] 画出 `conn.Read` 从用户代码到 `epoll_wait` 再回到用户代码的调用链（至少 8 个函数，标注文件）。
- [ ] [L2] `pollDesc.rg` 的四种取值分别表示什么？为什么需要 `pdWait` 这个中间态？
- [ ] [L2] Go 为什么用边沿触发并且只注册一次？这对 `internal/poll` 的读写循环提出了什么要求？
- [ ] [L2] deadline 到期时，运行时怎样唤醒正在 `Read` 的 goroutine？返回什么错误？连接之后还能用吗？和 Java `SO_TIMEOUT` 有什么不同？
- [ ] [L2] 虚拟线程里 `socket.getInputStream().read()` 阻塞时，JDK 25 内部发生了什么？`SYSTEM_THREADS` 和 `VTHREAD_POLLERS` 两种模式有何区别？
- [ ] [L2] `io.Copy(tcpConn, file)` 在 Linux 上什么时候走 `sendfile`？哪些写法会让它退化？
- [ ] [L3] 拿出 10 万空闲长连接的内存构成实测数据（栈 / 堆 / 缓冲 / 内核），并给出两种把单连接内存减半的手段。
- [ ] [L3] 线上网关 p99 抖动，trace 显示 goroutine 网络就绪后很久才运行。列出可能原因（CPU 饱和、`GOMAXPROCS` 设置、长时间运行的热循环、GC assist 等），说明怎样逐一验证。
- [ ] [L3] `strace` 发现每个小包一次 `write`，你怎么改？给出改前改后的 syscall 次数与吞吐数据。
- [ ] [L3] 多个 goroutine 同时 `conn.Write` 安全吗？为什么生产上仍然坚持单写者？
- [ ] [L4] 设计一个 30 万连接的 Go 网关：每连接 goroutine 数、缓冲策略、是否引入事件驱动库、什么条件下改用 Netty。写出决策依据。
- [ ] [L4] 团队已有 Netty 网关，评估迁移到 Java 25 虚拟线程或迁移到 Go 的收益与风险。

## 🕳️ 常见坑

- **以为 goroutine 阻塞等于线程阻塞**，于是自建"连接池 + worker 线程池"去读 socket，多此一举还引入额外拷贝和排队。
- **把 deadline 当相对超时**：只在建连时 `SetReadDeadline(time.Now().Add(30*time.Second))` 一次，30 秒后所有读都超时。心跳场景要每次读前重设。
- **`Read` 返回 `n > 0` 且 `err != nil` 时把 n 个字节丢了**：Java `InputStream` 不会同时返回数据和异常，这里是 Go 的独特契约。
- **只用 `err == io.EOF` 判断断线**，漏掉 `net.ErrClosed`（本端已关闭）和连接重置；或者用字符串匹配错误信息。
- **忘了 `bufio.Writer.Flush`**，或者在多个 goroutine 之间共享同一个 `bufio.Writer`（它不是并发安全的）。
- **在 Windows 本机测性能**，并据此推断 Linux 线上表现。
- **`io.Copy` 中间包了一层 Reader/Writer**，让 sendfile/splice 静悄悄地失效。
- **关闭连接时读 goroutine 没退出**：只取消了 context，没有 `Close` 连接（或没设 deadline），`Read` 永远阻塞，造成泄漏（[Ch14](../ch14-race-leak-detection/) 的 `goroutineleak` profile 能抓到一部分）。

## 📚 一手资料

- Go 源码：`runtime/netpoll.go`、`runtime/netpoll_epoll.go`、`runtime/netpoll_windows.go`、`internal/poll/`、`net/fd_posix.go`、`io/io.go`
- Go 文档：`go doc io.Reader`、`go doc net.Conn`、`go doc os.ErrDeadlineExceeded`、[Go 1.27 Release Notes（net 小节）](https://go.dev/doc/go1.27)
- Linux：`man 7 epoll`、`man 2 sendfile`、`man 2 splice`、`man 2 writev`；Windows：Microsoft Learn 上的 I/O Completion Ports 文档
- JDK：`src.zip` 中的 `sun/nio/ch/Poller.java`、`NioSocketImpl.java`、`SelectorImpl.java`；[JEP 353](https://openjdk.org/jeps/353)（重写 Socket API）、[JEP 444](https://openjdk.org/jeps/444)（虚拟线程）、[JEP 491](https://openjdk.org/jeps/491)（synchronized 不再 pin）
- Netty（第三方）：netty.io 官方文档与 `EventLoop` / `ChannelPipeline` 的 Javadoc
