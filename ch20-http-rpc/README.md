# Ch20 · HTTP、gRPC 与服务治理

> Part 4 · 估时 12h · 状态：⚪ 未开始 · 标准库 `net/http` 就是生产级服务器：超时、连接池、HTTP/2、优雅关闭都要讲到实现层；gRPC 双向流是游戏服内部长链路的主力

## 🎯 目标（资深标准）

- **L1**：写出超时完备、可优雅关闭的 `net/http` 服务端和能正确复用连接的客户端；写出带拦截器的 gRPC 单次调用与双向流服务。
- **L2**：
  - 讲清 `net/http` 服务端结构：`Serve` → 每连接 goroutine `conn.serve` → `readRequest` → handler；
  - 讲清 `ServeMux` 的模式匹配（方法 + 通配符 + 最具体优先）和五种超时各自覆盖的阶段；
  - 讲清 `Transport` 连接池（`getConn` / `queueForIdleConn` / `tryPutIdleConn`，`persistConn` 的读写循环）；
  - 讲清 1.27 新增的 HTTP 行为变化，以及 gRPC 如何在 HTTP/2 上实现流、流控、截止时间与取消；
  - 对照 JDK `HttpClient` 和 gRPC-java 的实现。
- **L3**：用实验证明"不读完 Body / 不关闭 Body""`MaxIdleConnsPerHost` 太小"对连接复用和延迟的影响并修复；测出 gRPC 双向流的往返延迟分布。
- **L4**：在 HTTP/JSON、gRPC、自研 TCP RPC 之间做服务间通信选型；设计超时预算、重试、熔断和服务发现方案。

## 🔗 前置

[Ch17](../ch17-netpoll-io/)（每连接 goroutine 与 deadline）· [Ch10](../ch10-context-structured/)（context 传播与优雅关闭）· [Ch19](../ch19-serialization/)（protobuf / json v2）· [Ch12](../ch12-concurrency-testing/)（`testing/synctest`）

## 💡 关键洞察

1. **`net/http` 服务端没有线程池可调。** 它的模型是每连接一个 goroutine，HTTP/1 的请求在这个 goroutine 里串行处理，HTTP/2 每个流一个 goroutine。并发上限来自你配置的超时、`MaxConcurrentStreams` 和限流，而不是线程数。漏配一个超时，就多一个慢速攻击面（Slowloris）。
2. **客户端复用连接有两个前提：Body 读完并关闭，且每个 host 的空闲连接数够用。** `DefaultMaxIdleConnsPerHost = 2`，在服务间高并发调用下几乎总是太小，会出现大量新建连接和 TIME_WAIT。1.27 起 HTTP/1 的 `Response.Body` 关闭时会自动排空未读内容（上限 `maxPostCloseReadBytes` = 256KB）来提高复用，但这不能代替正确使用。
3. **gRPC 不只是"更快的 HTTP"。** 它把流、流控、截止时间（`grpc-timeout` 头）和取消（context 取消 → `RST_STREAM`）标准化了。匹配、聊天、跨服战场这类游戏内部长链路，天然适合双向流。

## ☕ Java 25 深度复习

- **`java.net.http.HttpClient`**：
  - 支持 HTTP/1.1 和 HTTP/2，同步 `send` 与异步 `sendAsync`（返回 `CompletableFuture`）；
  - 内部由 `HttpClientImpl` 的 `SelectorManager` 线程驱动 NIO，连接按 host 复用；
  - 在虚拟线程里直接用同步 `send`，阻塞时虚拟线程被 park，不占载体线程（[Ch17](../ch17-netpoll-io/)）。
- **服务端**：JDK 没有生产级 HTTP 服务器。`jdk.httpserver`（`com.sun.net.httpserver`）只适合工具和测试；生产用 Netty、Jetty、Tomcat、Undertow + 框架（第三方），新版本的 Jetty / Tomcat 可以配置虚拟线程执行器。对照：Go 标准库的 `net/http` 就是生产级服务器。
- **gRPC-java**（第三方）：
  - 默认基于 Netty 传输；`StreamObserver` 回调模型；
  - 服务端 Executor 可换成虚拟线程执行器；
  - 截止时间和取消通过 `io.grpc.Context` 传播。
- **密码学与证书**：
  - JEP 510 KDF API（正式）：`javax.crypto.KDF`，如 `KDF.getInstance("HKDF-SHA256")`；
  - JEP 470 PEM 编码（preview）：`java.security.PEMEncoder` / `PEMDecoder`；
  - 此前版本已提供 ML-KEM / ML-DSA 算法（JEP 496/497）。
  - 对照 Go 1.27：`crypto/mldsa`，以及 TLS 1.3 对 ML-DSA 签名和 `MLKEM1024` 的支持。

**源码**：`src.zip` 中的 `java.net.http/jdk/internal/net/http/HttpClientImpl.java`、`java.net.http/java/net/http/HttpClient.java`、`java.base/java/security/PEMEncoder.java`。

## 🐹 Go 1.27 深入

### 规范层（库契约）
- **服务端模型**：`Server.Serve(ln)` 循环 Accept，每个连接 `go c.serve(ctx)`。一个 HTTP/1.1 连接上的请求串行处理；HTTP/2 每个流起一个 goroutine 跑 handler。
- **`ServeMux` 路由**：
  - 模式可以带方法、通配符和剩余匹配，如 `"GET /rooms/{id}"`、`"/static/{path...}"`，也可以带 host 前缀；
  - 最具体的模式优先，注册冲突的模式会 panic；handler 里用 `Request.PathValue("id")` 取参数；
  - 末尾斜杠的重定向使用 307，保留请求方法。
- **超时全家福**：
  | 超时 | 覆盖阶段 |
  |---|---|
  | `Server.ReadHeaderTimeout` | 读请求头 |
  | `Server.ReadTimeout` | 读请求头 + 请求体 |
  | `Server.WriteTimeout` | 从读完请求头到写完响应 |
  | `Server.IdleTimeout` | keep-alive 空闲等待下一个请求 |
  | `Client.Timeout` | 客户端整次请求，包括读完 Body |
  - handler 内可用 `ResponseController.SetReadDeadline` / `SetWriteDeadline` 按请求覆盖（长轮询、流式下载）；
  - 限制输入大小：`MaxBytesReader`（请求体）、`MaxHeaderBytes`（请求头）、**1.27 新增** `Server.MaxHeaderValueCount`（头部值的个数，默认 `DefaultMaxHeaderValueCount = 500`）。
- **优雅关闭**：`Shutdown(ctx)` 先关监听器，再关空闲连接，然后等活跃连接变空闲，`ctx` 到期就返回错误；`Serve` 返回 `ErrServerClosed`。被 hijack 的连接（如 WebSocket）不会被等待，要用 `RegisterOnShutdown` 自己通知。
- **协议与 HTTP/2 配置**：`Server` 和 `Transport` 都有 `Protocols`（`SetHTTP2`、`SetUnencryptedHTTP2`）和 `HTTP2 *HTTP2Config`（`MaxConcurrentStreams`、`StrictMaxConcurrentRequests`、`MaxReadFrameSize`、`PingTimeout`、`SendPingTimeout`、`WriteByteTimeout` 等）。
- **1.27 新增的行为变化**：
  - HTTP/2 服务端遵循 RFC 9218 的客户端优先级信号，`Server.DisableClientPriority = true` 恢复轮转调度；
  - HTTP/1 `Response.Body` 关闭时自动排空未读内容（有上限）；极少数配置下（`Transport.MaxIdleConns = 0`、每个请求新建一个 `Client`）可能出现回归，用 `Transport.DisableKeepAlives = true` 关闭复用；
  - 用户自带的 `net.Conn` 只要实现 `ConnectionState() tls.ConnectionState`，`Server` 和 `Transport` 就能在上面做 TLS ALPN；
  - `httptest.NewTestServer` 在内存假网络上起测试服务器，配合 `testing/synctest` 做确定性测试。
- **客户端连接池**（`Transport`）：
  - 字段：`MaxIdleConns`、`MaxIdleConnsPerHost`（默认 2）、`MaxConnsPerHost`、`IdleConnTimeout`、`ResponseHeaderTimeout`、`DisableKeepAlives`；
  - `Transport.NewClientConn` 用于自己管理单条连接，大多数场景继续用 `RoundTrip`。
- **安全**：
  - `NewCrossOriginProtection` 基于 `Sec-Fetch-Site` / `Origin` 头做 CSRF 防护，适合 GM 后台；
  - TLS：`Config.CurvePreferences` 控制密钥交换，默认集合包含混合后量子方案（如 `X25519MLKEM768`）。**1.27 新增**：可以显式启用 `MLKEM1024`，TLS 1.3 支持 ML-DSA 签名（`SignatureScheme` 的 `MLDSA44` / `MLDSA65` / `MLDSA87`），新包 `crypto/mldsa` 提供 `MLDSA44()` / `MLDSA65()` / `MLDSA87()` 参数集。
- **gRPC-go**（第三方，`google.golang.org/grpc`）：
  - 使用自己的 HTTP/2 传输实现，而不是 `net/http` 服务器；
  - 拦截器：`UnaryServerInterceptor` / `StreamServerInterceptor`；
  - `keepalive.ServerParameters` / `EnforcementPolicy`：客户端 ping 过于频繁会被 GOAWAY；
  - 截止时间通过 `grpc-timeout` 头传播，context 取消映射成 `RST_STREAM`；流控基于 HTTP/2 窗口；
  - 负载均衡策略（`pick_first` / `round_robin`）+ resolver 插件，可接入服务发现。
- **服务发现 / 配置中心**（第三方，`go.etcd.io/etcd/client/v3`）：用 lease + keepalive 注册实例，用 watch 订阅变更，租约过期即视为下线。

### 运行时层
- **goroutine 账**：每个 HTTP 连接一个 `conn.serve` goroutine；客户端每条 `persistConn` 各有一个 `readLoop` 和一个 `writeLoop` goroutine。连接池越大，常驻 goroutine 越多，这在 goroutine profile 里一目了然。
- **超时的实现**：服务端各类超时最终都落到连接的 deadline 上（[Ch17](../ch17-netpoll-io/)）；超时后连接被关闭，而不是只取消当前请求。所以 `WriteTimeout` 设得太短会误伤流式响应。
- **context 传播**：`Request.Context()` 在客户端断开、HTTP/2 流被重置或 `Shutdown` 时取消。handler 里所有下游调用都必须传这个 context，否则客户端走了下游还在算。

### 源码阅读清单（`$GOROOT/src`）
| 文件 | 函数/类型 | 带着什么问题读 |
|---|---|---|
| `net/http/server.go` | `Server.Serve`、`conn.serve`、`conn.readRequest` | 一个 HTTP/1.1 连接上的请求怎样串行处理？`ReadHeaderTimeout` 在哪里生效？ALPN 判断（`connectionStater`）在哪里？ |
| `net/http/server.go` | `Server.Shutdown`、`RegisterOnShutdown` | 怎样判定连接"空闲"？为什么被 hijack 的连接不会被等待？ |
| `net/http/pattern.go` · `routing_tree.go` | 模式解析与路由树 | 模式冲突怎样检测？"最具体优先"是怎样实现的？ |
| `net/http/transport.go` | `getConn`、`queueForIdleConn`、`tryPutIdleConn`、`persistConn.readLoop` / `writeLoop`、`maxPostCloseReadBytes` | Body 没读完为什么不能复用连接？1.27 的自动排空发生在哪里，上限多少？ |
| `net/http/clientconn.go` | `Transport.NewClientConn` | 什么场景需要自己管理连接？ |
| `net/http/csrf.go` | `CrossOriginProtection` | 依靠哪些请求头判断跨源请求？哪些请求被视为安全方法而放行？ |
| `net/http/httptest/server.go` | `NewTestServer` | 内存假网络如何与 synctest 气泡协作，让超时测试瞬间完成？ |

## 📊 Java 25 ↔ Go 1.27 机制级对照表

| 维度 | Java 25 | Go 1.27 | 取舍 / 游戏服影响 |
|---|---|---|---|
| 标准库服务端 | 无生产级实现，依赖 Netty / Jetty / Tomcat | `net/http` 即生产级 | Go 的 GM 后台、运营接口不需要任何框架 |
| 服务端并发模型 | 线程池或虚拟线程执行器 | 每连接 goroutine，HTTP/2 每流 goroutine | Go 没有"线程池大小"这个旋钮，靠超时和限流控制并发 |
| 客户端实现 | `HttpClient`：`SelectorManager` 线程 + NIO | `Transport`：每连接 `readLoop` / `writeLoop` goroutine | 两边都有连接池，但 Go 的每 host 空闲上限默认只有 2 |
| 超时体系 | `HttpClient` 的 connectTimeout / 请求 timeout；服务端看容器 | 服务端四个超时 + `Client.Timeout` + `ResponseController` | Go 的服务端超时要显式配置，否则默认不超时 |
| 请求取消 | `CompletableFuture.cancel`、线程中断 | `Request.Context()` 自动取消 | Go 的取消信号更统一，前提是下游都传 context |
| gRPC 实现 | gRPC-java：Netty 传输、`StreamObserver` 回调 | gRPC-go：自有 HTTP/2 传输、同步阻塞的流 API | Go 的双向流代码是顺序写法，更容易读写 |
| 截止时间传播 | `io.grpc.Context` | `context.Context` → `grpc-timeout` 头 | 两边都能跨服务传递超时预算 |
| 后量子 TLS | 提供 ML-KEM / ML-DSA 算法 API | TLS 默认混合 PQ 密钥交换；1.27 新增 ML-DSA 证书签名、`MLKEM1024` | 对外接口的 TLS 能力 Go 侧更开箱即用 |
| 测试工具 | MockWebServer 等第三方 | `httptest`，1.27 新增 `NewTestServer`（假网络 + synctest） | Go 的 HTTP 超时测试可以做到确定性、毫秒级完成 |

## 🔬 实验

### 实验 1：连接复用的三个杀手
- **目的**：拿数据证明连接复用的前提条件。
- **方法**：`go/lab/httpreuse/` 起一个本地 HTTP 服务，客户端 200 并发持续请求，对比四种写法：
  - A：不关闭 Body；
  - B：关闭 Body 但不读完（响应 1MB，超过 1.27 自动排空的上限）；
  - C：读完并关闭，但 `MaxIdleConnsPerHost` 保持默认 2；
  - D：读完并关闭 + `MaxIdleConnsPerHost = 200`。
- **工具**：用 `httptrace.ClientTrace` 的 `GotConn` 统计 `Reused` 比例；Linux 上 `ss -s` 看 TIME_WAIT 数；`benchstat` 比较延迟。
- **预期**：A、B 几乎每次新建连接；C 复用率低、TIME_WAIT 堆积；D 接近全部复用，延迟最低。
- **记录**：四组的复用率、TIME_WAIT 数、p50 / p99。

### 实验 2：超时缺失的代价（慢速攻击）
- **方法**：写一个"每秒只发 1 字节请求头"的慢客户端，同时开 1000 个，分别攻击"不配任何超时"和"配置 `ReadHeaderTimeout`"的服务端。
- **记录**：服务端 goroutine 数和内存随时间的曲线；正常请求的延迟变化。

### 实验 3：gRPC 双向流延迟（Linux/WSL2 推荐）
- **方法**：实现一个"匹配服务"双向流：客户端持续发送匹配请求，服务端推送匹配进度。在 1 / 100 / 1000 条并发流下测往返延迟；Java 侧可选用 gRPC-java 实现同一服务（需 Maven）。
- **工具**：在消息里带时间戳统计延迟；用 `pprof` 看服务端 CPU 分布。
- **记录**：三档并发下的 p50 / p99、CPU、goroutine 数。

### 实验 4：用 synctest 做确定性的超时测试
- **方法**：用 `httptest.NewTestServer`（1.27 新增）+ `testing/synctest` 测试"handler 超过 5s 返回 503"的逻辑。
- **记录**：普通测试（真实等待）和 synctest 版本的耗时对比。

## 🧪 练习规划

**Go：`go/exercises`**
1. `func NewServer(h http.Handler, cfg ServerConfig) *http.Server`：按配置设置全部超时、`MaxHeaderBytes`、`MaxHeaderValueCount`。测试：断言字段取值；用慢客户端验证 `ReadHeaderTimeout` 生效。
2. `func NewRouter(svc RoomService) *http.ServeMux`：注册 `GET /rooms/{id}`、`POST /rooms/{id}/join` 等路由。测试：`PathValue` 解析、方法不匹配返回 405、未知路由返回 404。
3. `func Fetch(ctx context.Context, c *http.Client, url string, maxBody int64) ([]byte, error)`：限制最大 Body，**始终读完并关闭**。测试：用 `httptrace` 断言连续调用复用了连接；超限返回 `ErrBodyTooLarge`。
4. `func RunWithGracefulShutdown(ctx context.Context, srv *http.Server, ln net.Listener, grace time.Duration) error`：ctx 取消后优雅关闭，超出 grace 后强制 `Close`。测试：进行中的慢请求在 grace 内完成；超出 grace 的请求被中断；函数返回后无 goroutine 残留。
5. `func TestTimeoutHandler`：用 `httptest.NewTestServer` + `synctest.Test` 验证 `http.TimeoutHandler` 在假时钟下准确返回 503。

**Java：`java/drills`**
- `HttpClientReuseDrill.java`：用 `jdk.httpserver` 起本地服务，用 `HttpClient` 在虚拟线程里并发请求；自校验响应正确，并通过服务端记录的远端端口数判断连接复用情况。
- `KdfDrill.java`：用 JEP 510 的 `KDF.getInstance("HKDF-SHA256")` 从会话主密钥派生"加密密钥 + MAC 密钥"，自校验派生结果的确定性与长度；注释中对照 Go 的 `crypto/hkdf`。

## 🎮 游戏服落地

- **GM 后台与运营接口**：直接用 `net/http` + `ServeMux` 模式路由 + `CrossOriginProtection`，配全超时和 `MaxBytesReader`。Java 侧通常要拉上 Spring 全家桶，Go 这里零依赖。
- **服务间调用**：匹配、聊天、排行榜、跨服战场之间用 gRPC。短调用用 unary + 截止时间；持续推送用双向流。超时预算从网关一路往下传：入口预算 500ms，每一跳扣掉自己的耗时再传给下游。
- **第三方回调（支付、登录 SDK）**：HTTP 客户端必须设置 `Client.Timeout`，调大 `MaxIdleConnsPerHost`，Body 读完并关闭；重试只针对幂等请求，并加上指数退避和抖动。
- **服务注册与发现**：游戏服实例启动时用 etcd lease 注册（带负载、版本号），网关 watch 实例列表做路由；实例下线时先摘除注册，再走优雅关闭（与 [Ch18](../ch18-tcp-gateway/) 的排空顺序一致）。

## ✅ 验收答辩题

- [ ] [L2] 画出 `net/http` 服务端从 Accept 到 handler 返回的路径（函数 + goroutine），以及 HTTP/2 下的差异。
- [ ] [L2] 五种超时分别覆盖哪个阶段？只配 `ReadTimeout` 不配 `ReadHeaderTimeout` 会有什么问题？`WriteTimeout` 对流式响应有什么副作用？
- [ ] [L2] `Transport` 是怎样决定复用还是新建连接的？为什么"不读完 Body"会破坏复用？1.27 的自动排空改变了什么、没改变什么？
- [ ] [L2] `Shutdown` 的完整流程是什么？WebSocket 这类被 hijack 的连接怎样参与优雅关闭？
- [ ] [L2] gRPC 的截止时间和取消在线路上分别是怎样表示的？context 取消后服务端会发生什么？
- [ ] [L3] 用实验 1 的数据解释复用率、TIME_WAIT 和延迟之间的关系，并给出线上配置建议。
- [ ] [L3] 线上服务 goroutine 数持续上涨，goroutine profile 显示大量 `persistConn.readLoop`，你怎样定位和修复？
- [ ] [L3] 用 `httptest.NewTestServer` + synctest 写一个确定性的超时测试，并说明它为什么比 `time.Sleep` 版本可靠。
- [ ] [L4] 游戏服内部通信在 HTTP/JSON、gRPC、自研 TCP RPC 之间怎样选？分别给出适用场景与代价。
- [ ] [L4] 设计一套跨 4 跳服务调用的超时预算 + 重试 + 熔断方案，说明如何避免重试风暴。

## 🕳️ 常见坑

- **`http.ListenAndServe(addr, h)` 直接上线**：默认 `Server` 没有任何超时，慢速攻击即可耗尽资源。
- **不关闭或不读完 Body**：连接无法复用，`readLoop` goroutine 和 fd 持续增长。
- **每次请求新建 `http.Client` / `Transport`**：连接池形同虚设；1.27 的自动排空在这种用法下也可能带来回归。
- **以为 `Client.Timeout` 只管到收到响应头**：它覆盖到 Body 读完为止，下载大文件时会被截断；分阶段控制用 `ResponseHeaderTimeout` 或 context。
- **handler 里调用下游时不传 `r.Context()`**：客户端早已断开，下游还在计算。
- **对非幂等请求做自动重试**：重复扣款、重复发奖。
- **gRPC 客户端 keepalive ping 太频繁**：被服务端 `EnforcementPolicy` 判定违规并 GOAWAY，表现为连接周期性断开。
- **在 Java 习惯里找"线程池大小"来限并发**：Go 服务端要用超时、`MaxConcurrentStreams`、信号量或限流中间件。

## 📚 一手资料

- RFC 9110（HTTP 语义）、RFC 9112（HTTP/1.1）、RFC 9113（HTTP/2）、RFC 9218（HTTP 优先级）、gRPC over HTTP/2 协议规范（grpc/grpc 仓库的 PROTOCOL-HTTP2.md）
- Go：[Go 1.27 Release Notes（net/http、crypto/tls、crypto/mldsa）](https://go.dev/doc/go1.27)、`go doc net/http.Server`、`go doc net/http.Transport`、`go doc net/http.ServeMux`、`go doc net/http/httptest.NewTestServer`
- Go 源码：`net/http/server.go`、`transport.go`、`pattern.go`、`routing_tree.go`、`csrf.go`、`clientconn.go`
- JDK：`src.zip` 中的 `java.net.http/jdk/internal/net/http/HttpClientImpl.java`；[JEP 510](https://openjdk.org/jeps/510)、[JEP 470](https://openjdk.org/jeps/470)、[JEP 496](https://openjdk.org/jeps/496)、[JEP 497](https://openjdk.org/jeps/497)
- 第三方：grpc.io 官方文档（gRPC-go / gRPC-java）、etcd 官方文档（lease、watch）
