# Ch18 · TCP 长连接网关实战

> Part 4 · 估时 18h · 状态：⚪ 未开始 · 网关是一个"有界缓冲的转发器"：分帧、读写分离、写合并、背压、心跳、优雅排空，一样都不能靠运气

## 🎯 目标（资深标准）

- **L1**：写出生产可用的长度前缀分帧编解码（先校验最大包长再分配）、"一读一写 + 有界写队列"的连接对象、基于 deadline 的心跳踢人、可中断的 Accept 循环与优雅排空。
- **L2**：讲清粘包/半包的成因（TCP 字节流 + 发送合并 + 接收缓冲）；讲清 Go 的 TCP 默认值（`TCP_NODELAY` 默认开启，KeepAlive 默认 15s 空闲 / 15s 间隔 / 9 次探测，Linux 上 backlog 取 `somaxconn`）；讲清 Netty `ChannelOutboundBuffer` + 高低水位线 + `isWritable` 的背压机制，并能在 Go 里重建等价机制。
- **L3**：在 1 万连接 × 20Hz 广播的负载下拿出 CPU、内存、分配率、syscall 次数、端到端延迟的数据；用写合并和"编码一次、共享多次"把 syscall 与分配压下来；能复现并治理慢客户端。
- **L4**：设计网关的连接生命周期、背压与降级策略、停服维护与滚动发布方案；在 TCP / WebSocket / UDP+KCP 之间按游戏类型选型。

## 🔗 前置

[Ch17](../ch17-netpoll-io/)（netpoller、deadline、零拷贝）· [Ch11](../ch11-actor/)（mailbox、背压、房间 actor）· [Ch10](../ch10-context-structured/)（取消与优雅关闭）· [Ch16](../ch16-zero-alloc-dod/)（零分配热路径）· 消息体的编码见 [Ch19](../ch19-serialization/)

## 💡 关键洞察

1. **每一个无界队列都是内存炸弹。** Netty 用高低水位线 + `isWritable` 把背压显式化，但它默认只"提示"不"处理"；Go 里没有现成机制，必须用有界写 channel + 非阻塞投递 + 明确的慢客户端策略（丢弃 / 降级 / 断开）自己实现。
2. **"一读一写两个 goroutine + 一个有界写队列"是 Go 网关的标准形态。** 写 goroutine 正是写合并的天然位置：拿到一条消息后非阻塞地把队列里积压的消息一起取出，用一次 `writev`（`net.Buffers`）或一次 `bufio.Flush` 发出。
3. **广播要"编码一次、引用多次"。** 房间把快照编码成一个 `[]byte`，所有连接的写队列共享同一个只读切片（约定不可变），而不是 N 次编码、N 次分配、N 次拷贝。

## ☕ Java 25 深度复习

### Netty 编解码（第三方，要能讲到参数级）
- `ByteToMessageDecoder` 的累积缓冲（cumulator：合并拷贝 vs 组合缓冲）是半包处理的基础；`LengthFieldBasedFrameDecoder` 的参数：`maxFrameLength`、`lengthFieldOffset`、`lengthFieldLength`、`lengthAdjustment`、`initialBytesToStrip`、`failFast`，超长抛 `TooLongFrameException`；编码侧是 `LengthFieldPrepender`。
- 写路径：`write()` 只是把消息放进 `ChannelOutboundBuffer`，`flush()` 才真正写 socket；`FlushConsolidationHandler` 合并频繁的 flush。
- 背压：`WriteBufferWaterMark`（低/高水位），待发送字节越过高水位时 `isWritable()` 变为 false，并触发 `channelWritabilityChanged`；回落到低水位以下才恢复。业务不检查 `isWritable` 的话，出站缓冲会无限增长。
- 空闲检测：`IdleStateHandler`（读空闲 / 写空闲 / 全空闲）通过 `userEventTriggered` 通知；`ReadTimeoutHandler` 超时直接关闭。
- 线程模型：同一连接的事件在其 `EventLoop` 上串行；业务线程调用 `channel.write` 时会被包装成任务投递到 EventLoop 队列，这一跨线程投递就是成本所在。
- 优雅关闭：`EventLoopGroup.shutdownGracefully(quietPeriod, timeout)`。

### Java 25 原生方案：虚拟线程网关
- 每连接一个读虚拟线程 + 一个写虚拟线程 + 一个有界 `ArrayBlockingQueue`：`offer` 失败即判定为慢客户端。写线程阻塞在 socket 输出流上时，`NioSocketImpl` 把它挂到 Poller 上并 park，载体线程不被占用（机制见 [Ch17](../ch17-netpoll-io/)）。
- JEP 491 之后 `synchronized` 内阻塞不再 pin 载体线程；`StructuredTaskScope`（JEP 505，preview）可以把"每连接的读写子任务"组织成一个作用域，一边失败另一边自动取消（[Ch10](../ch10-context-structured/)）。
- `java.net.Socket` 的 `TCP_NODELAY` 默认关闭（沿用 OS 默认），这一点和 Go 相反。

**源码**（`src.zip`，`java.base/` 下）：`java/net/Socket.java`、`sun/nio/ch/NioSocketImpl.java`、`java/util/concurrent/ArrayBlockingQueue.java`。

## 🐹 Go 1.27 深入

### 规范层（库契约）
- **TCP 是字节流**：一次 `Read` 可能拿到半个包，也可能拿到多个包。标准做法是 `io.ReadFull` 读定长头，再按长度读包体。**最大包长检查必须在分配之前**，否则一个伪造的 4 字节长度头就能触发巨量分配。
- **`encoding/binary`**：`binary.BigEndian.Uint32` / `AppendUint32` 用于定长头；`AppendUvarint` / `Uvarint` 用于变长长度前缀。
- **`bufio` 上的零拷贝分帧**：`Reader.Peek(n)` 返回指向内部缓冲的切片，配合 `Discard` 消费。这个切片在下一次读操作后就失效，若要跨 goroutine 传递必须拷贝。写侧 `Writer.AvailableBuffer()` 返回可直接 `append` 的空闲缓冲，用来在缓冲区上原地编码。
- **批量写**：`net.Buffers`（`[][]byte`）的 `WriteTo` 在支持的平台上走 `writev`。`internal/poll/writev.go` 在 Linux 上每次最多提交 1024 个 iovec，超过的部分循环提交。
- **TCP 选项**（`net/tcpsock.go`、`net/dial.go`）：
  - `newTCPConn` 对每个 TCP 连接默认 `setNoDelay(fd, true)`，即关闭 Nagle；
  - KeepAlive 默认值 `defaultTCPKeepAliveIdle = 15s`、`defaultTCPKeepAliveInterval = 15s`、`defaultTCPKeepAliveCount = 9`，可通过 `ListenConfig.KeepAliveConfig` 或 `TCPConn.SetKeepAliveConfig` 调整；
  - `SetLinger`、`CloseWrite`（半关闭，发 FIN 但还能读）、`SetReadBuffer` / `SetWriteBuffer`（SO_RCVBUF/SO_SNDBUF）。
- **监听**：
  - `ListenConfig.Control` 回调在 socket 创建后、bind 之前执行，可以通过 `syscall.RawConn` 设置 `SO_REUSEPORT`（标准库 `syscall` 在 Linux 上没有这个常量，用 `golang.org/x/sys/unix.SO_REUSEPORT`）；
  - Linux 上 backlog 由 `net/sock_linux.go` 的 `maxListenerBacklog` 读取 `/proc/sys/net/core/somaxconn`；
  - `TCPListener.SetDeadline` 让 Accept 循环可以周期性醒来检查退出条件；`Close` 监听器后 `Accept` 返回 `net.ErrClosed`。
- **UDP**：`UDPConn.ReadFromUDPAddrPort` / `WriteToUDPAddrPort` 使用 `netip.AddrPort`，避免每个包分配地址对象（KCP 类协议的基础）。

### 运行时层：网关里的 goroutine 怎么动
- **读 goroutine**：阻塞在 `Read` 时被 park 在 netpoller 上（[Ch17](../ch17-netpoll-io/)），每条消息解码后投递给房间 / 玩家 actor 的 mailbox。
- **写 goroutine**：阻塞在写 channel 的接收上；拿到第一条后用 `select { case m := <-ch: ... default: }` 循环非阻塞地取完积压（设上限：最多 N 条或 M 字节），然后一次 `writev`。
- **慢客户端链路**：对端不读 → 对端接收窗口归零 → 本端内核发送缓冲填满 → `Write` 返回 EAGAIN，写 goroutine park 在 netpoller 上 → 写 channel 开始积压 → 投递方的非阻塞 `select` 走到 `default`，触发慢客户端策略。这条链路就是 Go 版的"水位线"。
- **定时器选择**：心跳超时优先用 `SetReadDeadline`（不额外占用 goroutine，只是一个运行时 timer）；需要大量业务定时（技能 CD、断线保留）时用共享时间轮（[Ch11](../ch11-actor/)）。不要为每个连接起一个带 `time.Ticker` 的 goroutine。
- **优雅排空顺序**：注册中心摘流量 → `ln.Close()` 停止 Accept → 广播"即将维护"消息 → 停止接收新的业务请求 → 等写队列排空（带超时）→ `CloseWrite` → 等对端 FIN 或超时 → `Close`。用 `context` 传递关闭信号，用 `sync.WaitGroup.Go` 跟踪每个连接的读写 goroutine。
- **WebSocket**（H5 / 小游戏）：第三方库 `github.com/coder/websocket`、`github.com/gorilla/websocket`。它仍然是 TCP 上的消息帧，多了握手、掩码、ping/pong。网关应把 TCP/WebSocket 抽象成同一个"消息帧 Transport"接口。
- **UDP + KCP**（动作 / 射击类）：第三方库 `github.com/xtaci/kcp-go`，在 UDP 上实现 ARQ，可调重传与窗口，用带宽换延迟。

### 源码阅读清单（`$GOROOT/src`）
| 文件 | 函数/类型 | 带着什么问题读 |
|---|---|---|
| `net/tcpsock.go` | `newTCPConn`、`SetNoDelay`、`KeepAliveConfig`、`SetKeepAliveConfig`、`CloseWrite`、`SetLinger` | Go 给每个 TCP 连接默认开了哪些选项？为什么默认关闭 Nagle？ |
| `net/dial.go` | `ListenConfig`、`ListenConfig.Listen`、`defaultTCPKeepAlive*` 常量 | `Control` 回调在 socket 生命周期的哪个时机执行？KeepAlive 默认值对移动端意味着什么？ |
| `net/sock_linux.go` | `maxListenerBacklog` | backlog 为什么去读 `somaxconn`？容器里这个值通常是多少，登录风暴时会怎样？ |
| `net/net.go` · `internal/poll/writev.go` | `Buffers.WriteTo`、`FD.Writev` | 一次 writev 最多多少段？超过怎么办？部分写之后 `Buffers` 的状态如何推进？ |
| `bufio/bufio.go` | `Reader.Peek`、`Reader.Discard`、`Writer.AvailableBuffer`、`Writer.Flush` | 在 bufio 内部缓冲上原地解析帧，生命周期约束是什么？什么时候必须拷贝？ |
| `encoding/binary/varint.go` · `binary.go` | `AppendUvarint`、`Uvarint`、`BigEndian.AppendUint32` | varint 长度前缀 vs 固定 4 字节头：对防御恶意输入和对性能各有什么影响？ |
| `internal/poll/fd_unix.go` | `FD.Write` | 内核发送缓冲满时 `Write` 怎样阻塞？部分写如何续写？对写超时有什么影响？ |

## 📊 Java 25 ↔ Go 1.27 机制级对照表

| 维度 | Java 25（Netty / 虚拟线程） | Go 1.27 | 取舍 / 游戏服影响 |
|---|---|---|---|
| 分帧解码 | `LengthFieldBasedFrameDecoder` + 累积缓冲 | `io.ReadFull` 读头和体，或在 `bufio.Peek` 上切片 | Go 手写更直观，但最大包长防御、半包处理全靠自己 |
| 写路径 | `write` 进 `ChannelOutboundBuffer`，`flush` 落 socket | 写 goroutine + 有界 channel，批量 `writev` | 两边都把"写"和"刷"分离；Go 的批处理点在写 goroutine |
| 背压信号 | 高低水位线 + `isWritable` + `channelWritabilityChanged` | 有界写队列 + 非阻塞投递失败 | Netty 默认只提示不处理；Go 必须显式选择丢弃、降级还是断开 |
| 空闲检测 | `IdleStateHandler`（EventLoop 定时任务） | `SetReadDeadline` / 共享时间轮 | Go 用 deadline 不需要额外 goroutine |
| 线程与连接 | 连接绑定 EventLoop；业务线程写要跨线程投递任务 | 读写各一个 goroutine；业务 actor 直接往写 channel 投递 | 跨线程投递两边都有成本：Netty 任务队列 vs Go channel |
| TCP 默认值 | `java.net.Socket` 的 `TCP_NODELAY` 默认关；Netty 由 `ChannelOption` 显式配置 | `TCP_NODELAY` 默认开；KeepAlive 15s/15s/9 | 迁移时最容易忽略的差异，直接影响小包延迟 |
| `SO_REUSEPORT` | 仅 Netty native epoll transport 支持 | `ListenConfig.Control` + `x/sys/unix` | 都是 Linux 专属，用于多进程/多监听分摊 accept |
| 优雅关闭 | `shutdownGracefully(quietPeriod, timeout)` | `ln.Close` + `context` 取消 + `WaitGroup` + 排空超时 | Go 需要自己编排顺序，但每一步都看得见 |
| 原生阻塞模型 | 虚拟线程 per-conn + `ArrayBlockingQueue` | goroutine per-conn + channel | Java 25 能写出与 Go 同构的网关，可以拿来做纯运行时差异的对照实验 |

## 🔬 实验

### 实验 1：1 万连接 × 20Hz 广播（**Linux/WSL2**）
- **目的**：拿到网关在典型房间广播负载下的完整画像。
- **方法**：`go/lab/gateway/` 实现网关 + 100 个房间 × 100 人，房间每 50ms 广播一条 200 字节的状态；机器人客户端（单独进程）在消息里带发送时间戳，统计端到端延迟。Java 侧 `java/VtGateway.java` 用虚拟线程实现同构网关；Netty 版可选（需 Maven）。
- **工具**：`pidstat -u -r`（CPU/RSS）、Go 的 `runtime/metrics`（分配速率、GC 次数与暂停、goroutine 数）、Java 的 `-Xlog:gc*` 与 JFR；`perf stat` 或 `strace -c -f`（抽样，开销大）统计 syscall。
- **预期**：瓶颈首先出现在 syscall 次数和分配上，而不是 goroutine 数量。
- **记录**：CPU、RSS、allocs/s、GC 暂停分布、p50/p99/p99.9 延迟、syscall/s。

### 实验 2：写合并前后对比（**Linux/WSL2**）
- **方法**：同一负载下，A 版每条消息一次 `Write`，B 版写 goroutine 批量取出 + `net.Buffers.WriteTo`，C 版在 B 的基础上让广播共享编码结果。
- **工具**：`strace -c -f` 统计 write/writev 次数；`go test -bench -count=10` + `benchstat`；实验 1 的延迟统计。
- **预期**：syscall 次数下降一个数量级以上，CPU 明显下降；p99 可能因为攒批略升，需要设"最多等多久"的上限来权衡。
- **记录**：三版的 syscall/s、CPU、p50/p99。

### 实验 3：慢客户端注入
- **方法**：让 5% 的机器人只连不读。对比三种网关：无界队列版、有界队列 + 丢弃版、有界队列 + 三次失败即断开版。
- **工具**：RSS 曲线、被丢弃消息数、被踢连接数、正常客户端的 p99。Java 侧可选：Netty 不检查 `isWritable` 时观察出站缓冲增长。
- **预期**：无界版内存线性上涨直至 OOM；有界版内存平稳，正常客户端延迟不受影响。
- **记录**：内存曲线截图、策略触发次数。

### 实验 4：优雅排空
- **方法**：压测中触发 Shutdown（Linux 上发 `SIGTERM` + `signal.NotifyContext`），统计排空时长、丢失消息数、客户端收到"维护通知"的比例。
- **记录**：不同排空超时（1s/5s/30s）下的结果。

## 🧪 练习规划

**Go：`go/exercises`**（网络类用 `net.Pipe` 或 `127.0.0.1:0` 回环；并发练习在 Linux/WSL2 下必须过 `-race`）
1. `type FrameCodec struct { MaxSize int }`，实现 `func (c FrameCodec) ReadFrame(r *bufio.Reader) ([]byte, error)` 与 `func (c FrameCodec) AppendFrame(dst, payload []byte) ([]byte, error)`（4 字节大端长度头）。测试：用 `testing/iotest` 的 `OneByteReader` / `HalfReader` 模拟半包、多帧拼接模拟粘包、超长返回 `ErrFrameTooLarge`（断言**没有**按伪造长度分配）、截断返回 `io.ErrUnexpectedEOF`；fuzz 测试编码-解码往返。
2. `type Conn struct{...}`：`func NewConn(c net.Conn, queueLen int) *Conn`、`func (c *Conn) Send(msg []byte) error`（非阻塞，队列满返回 `ErrSlowConsumer`）、`func (c *Conn) Close() error`。写 goroutine 批量取出 + `net.Buffers`。测试：消息顺序、`Close` 之后 `Send` 返回 `ErrClosed`、`-race`、关闭后无 goroutine 残留。
3. `func Broadcast(conns []*Conn, payload []byte) (sent, dropped int)`：所有连接共享同一 payload。测试：慢客户端被计入 `dropped`；`testing.AllocsPerRun` 断言广播本身 0 分配。
4. `func (c *Conn) ReadLoop(idle time.Duration, handle func([]byte)) error`：每次读前重设 deadline，空闲超时返回可被 `errors.Is(err, os.ErrDeadlineExceeded)` 命中的错误。可尝试放进 `testing/synctest` 气泡里让超时测试瞬间完成（`net.Pipe` 的 deadline 基于 `time.AfterFunc`）。
5. `type Server struct{...}`：`Serve(ctx context.Context, ln net.Listener) error` 与 `Shutdown(ctx context.Context) error`。测试：Shutdown 后不再 Accept、已排队消息在超时内发完、所有连接 goroutine 退出、超时到期时强制关闭。

**Java：`java/drills`**
- `FrameCodecDrill.java`：用 `ByteBuffer` 实现同一分帧协议（长度头 + 最大包长），自校验半包、粘包、超长三类输入；在注释里写出等价的 `LengthFieldBasedFrameDecoder` 参数组合。
- `VtGatewayDrill.java`：虚拟线程网关骨架，每连接读/写虚拟线程 + `ArrayBlockingQueue`（`offer` 失败即慢客户端），自校验广播顺序和慢客户端被踢。

## 🎮 游戏服落地

- **房间广播（MOBA / 棋牌 / 休闲竞技）**：房间 actor 每帧编码一次快照，把同一个 `[]byte` 投递给所有成员连接；某个连接连续 3 次投递失败，就降级为"只发关键帧"或断开让其重连。Java 侧等价做法：编码成一个 `ByteBuf` 后 `retainedDuplicate()` 分发，并检查 `isWritable`。
- **登录风暴（开服 / 维护后）**：数万连接同时涌入，要关注 backlog（`somaxconn`）、accept 速率、鉴权握手限流（令牌桶），必要时用 `SO_REUSEPORT` 多监听分摊。Go 和 Netty 的瓶颈点一致，都在内核队列和鉴权后端。
- **停服维护 / 滚动发布**：网关先从注册中心摘除，再停 Accept → 广播重连提示 → 排空 → 关闭；客户端凭 token + 消息序列号断线重连，恢复会话（完整方案在 [Ch25](../ch25-capstone/)）。
- **多协议接入**：H5 / 小游戏走 WebSocket，动作类走 KCP/UDP，主客户端走 TCP。网关抽象统一的 Transport 接口，上层房间逻辑只看到"消息帧"。

## ✅ 验收答辩题

- [ ] [L2] 粘包和半包分别是怎么产生的？为什么 TCP 层面根本没有"包"这个概念？
- [ ] [L2] Go 对新建的 TCP 连接默认设置了哪些选项、取值是多少？`java.net.Socket` 的默认值有什么不同？对小包延迟的影响是什么？
- [ ] [L2] 解释 Netty 高低水位线的工作方式。你在 Go 里用什么结构实现等价的背压？二者在"默认行为"上有什么差别？
- [ ] [L2] 慢客户端从"对端不读"到"投递方察觉"，中间经过了哪些环节（内核窗口、发送缓冲、netpoller、写队列）？
- [ ] [L2] `bufio.Reader.Peek` 返回的切片什么时候失效？在什么场景下必须拷贝？
- [ ] [L3] 用实验 2 的数据说明写合并对 syscall 次数、CPU、p99 的影响，以及你如何设定"攒批上限"来平衡吞吐与延迟。
- [ ] [L3] 一个伪造长度头为 2GB 的包打到网关，你的实现会怎样？写出防御代码，并说明在 Netty 里对应哪个参数。
- [ ] [L3] 线上网关内存持续上涨但 goroutine 数稳定，你怎样用 heap profile 和 `runtime/metrics` 判断是写队列积压、缓冲泄漏还是广播 payload 被意外持有？
- [ ] [L3] 设计并验证优雅排空：给出关闭顺序、每一步的超时，以及证明"维护通知送达率"的数据。
- [ ] [L4] 一款 10Hz 状态同步的 MOBA 和一款 60Hz 的动作游戏，网关传输层分别怎么选（TCP / WebSocket / KCP）？写出理由和代价。
- [ ] [L4] 同样的网关需求，Netty、Java 25 虚拟线程、Go 三种实现方案在开发效率、性能上限、可观测性、团队技能上的取舍。
- [ ] [L4] 设计登录风暴下的保护措施（backlog、accept 速率、鉴权限流、`SO_REUSEPORT`），并说明如何压测验证。

## 🕳️ 常见坑

- **把一次 `Read` 当成一个包**，半包时解码失败，粘包时丢消息。
- **先按长度头分配再检查上限**：一个恶意 4 字节头就能让进程 OOM。
- **每条消息 `go conn.Write(msg)`**，或者使用无界写队列：慢客户端拖垮内存，而且消息乱序。
- **多个 goroutine 直接 `conn.Write`**，导致帧在字节层面交错。
- **广播时为每个连接重复编码 / 拷贝 payload**；或者共享 payload 之后又有人修改了它（违反不可变约定）。
- **不知道 Go 默认 `TCP_NODELAY=true`、KeepAlive 15 秒**：从 Java 迁移时手动关 Nagle 属于多余操作；KeepAlive 间隔在移动网络下要结合心跳设计重新评估。
- **关闭顺序错误**：先 `Close` 连接，写队列里的"维护通知"根本没发出去。
- **每个连接一个 `time.Ticker` goroutine 做心跳**：10 万连接就是 10 万个 goroutine + ticker；改用 deadline 或共享时间轮。

## 📚 一手资料

- RFC 9293（TCP）、RFC 6455（WebSocket）；`man 7 tcp`、`man 7 socket`（`SO_REUSEPORT`、`SO_SNDBUF`）、`man 2 writev`
- Go 源码：`net/tcpsock.go`、`net/dial.go`、`net/sock_linux.go`、`net/net.go`（`Buffers`）、`internal/poll/writev.go`、`bufio/bufio.go`、`testing/iotest/`
- Go 文档：`go doc net.ListenConfig`、`go doc net.KeepAliveConfig`、`go doc net.Buffers`、`go doc bufio.Writer.AvailableBuffer`
- JDK：`src.zip` 中的 `java/net/Socket.java`、`sun/nio/ch/NioSocketImpl.java`；[JEP 491](https://openjdk.org/jeps/491)、[JEP 505](https://openjdk.org/jeps/505)
- 第三方：Netty 官方文档（`LengthFieldBasedFrameDecoder`、`WriteBufferWaterMark`、`IdleStateHandler` 的 Javadoc）；`github.com/coder/websocket`、`github.com/gorilla/websocket`、`github.com/xtaci/kcp-go` 的 README
