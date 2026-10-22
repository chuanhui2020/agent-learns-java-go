# Ch25 · Capstone：分布式房间制游戏服

> Part 6 · 估时 40h+ · 状态：⚪ 未开始 · 把 25 章的零件装成一台能压测、能排障、能滚动发布的游戏服，再用 Java 25 重写关键服务做同负载对比

## 🎯 目标（资深标准）

- **L3**：交付一个可运行的分布式房间制实时对战服务端（网关 + 匹配 + 房间节点 + 存储 + 可观测性），在 Linux 上用机器人压到 **1 万在线、20Hz tick**，并给出完整压测报告（延迟分位、GC 暂停、CPU / 内存、每 tick 分配次数）。
- **L3**：能演示并解释三类故障的处理：房间 actor panic、房间节点宕机、网关滚动重启，玩家侧影响有数据。
- **L4**：用 Java 25（虚拟线程 + 结构化并发 + ScopedValue）重写房间节点，在同协议、同负载下对比，写出一页"Go vs Java 选型备忘录"：结论、证据、适用边界、风险。
- **L4**：产出架构设计文档（含 ADR：每个关键取舍的备选方案和理由），让一个新同事看完就能维护。

## 🔗 前置

全部章节，重点：[Ch10](../ch10-context-structured/)（取消与优雅关闭）、[Ch11](../ch11-actor/)（actor 与 tick）、[Ch12](../ch12-concurrency-testing/)（synctest、泄漏检测）、[Ch14](../ch14-gc/)–[Ch16](../ch16-zero-alloc-dod/)（GC、剖析、零分配）、[Ch18](../ch18-tcp-gateway/)（网关）、[Ch19](../ch19-serialization/)（协议）、[Ch21](../ch21-storage-cache/)（存储）、[Ch23](../ch23-build-observability/)（部署与可观测）、[Ch24](../ch24-runtime-boundaries/)（热更与重启）

## 💡 关键洞察

1. **游戏服的核心指标是"tick 的确定性"，不是 QPS。** 玩家感受到的是"输入到看见结果"的延迟和抖动。架构里的每个决定（actor 粒度、GC 设置、广播方式、线程模型）都要回到两个数字上：输入到快照的端到端 p99，以及 tick 超时次数。
2. **单房间单线程，跨房间无共享。** 房间是天然的一致性边界：房间内状态只由房间 actor 修改（无锁），跨房间只通过消息交互。这条原则在 Go（goroutine + channel）和 Java（单线程执行器 / 虚拟线程 + 队列）里都成立，差别只在调度和内存行为。
3. **有状态服务的发布和容灾要在设计阶段就想好。** 房间状态在内存里，节点重启就会丢。要么让房间可以排空（等房间自然结束），要么让房间状态可以序列化迁移。两者都需要在协议和房间生命周期里预留位置，事后补救代价很大。

## 🏗️ 架构

```
            ┌──────────── HTTP 登录 / 运营后台（net/http + json/v2）────────────┐
            │                                                                 │
 客户端 ─TCP/WebSocket─▶ 网关 Gateway ──内部长连接（多路复用帧）──▶ 房间节点 Room ×N
 (机器人)                 │  会话 / 鉴权 / 心跳 / 路由                 │  房间 actor（每房间一个 goroutine）
                         │                                          │  20Hz tick、输入缓冲、状态同步
                         └──gRPC（控制面）──▶ 匹配 Match ─分配房间──▶ │  帧同步模式（可选房间类型）
                                              │                       │
                          服务注册 / 发现（接口；静态配置或 etcd）◀──负载上报─┘
                                              │
                          Redis：会话、排行榜 ZSET、匹配队列快照      PostgreSQL：战绩（异步写回）
```

| 组件 | 职责 | 关键技术点 | 对应章节 |
|---|---|---|---|
| 网关 | 长连接、鉴权、心跳、按 roomID 路由、背压、慢客户端踢出、断线重连（会话保留窗口 + 消息序号重放） | goroutine-per-conn 读 + 有界写队列、写合并、`net.Buffers` | [Ch17](../ch17-netpoll-io/)、[Ch18](../ch18-tcp-gateway/) |
| 协议 | 客户端协议、网关↔房间的内部帧格式 | 长度前缀 + 消息 ID + 序号；protobuf 或手写二进制；fuzz | [Ch19](../ch19-serialization/)、[Ch22](../ch22-modules-testing/) |
| 匹配 | 按模式和分段排队、等待时间越长分段越宽、选择负载最低的房间节点分配房间 | 队列 actor、gRPC 控制面 | [Ch11](../ch11-actor/)、[Ch20](../ch20-http-rpc/) |
| 房间节点 | 房间 actor、tick 循环、输入缓冲与校验、状态同步（快照 + 增量 + AOI）、帧同步、结算 | 固定步长 tick、时间轮、零分配广播、`pprof.Do` 打房间标签 | [Ch11](../ch11-actor/)、[Ch16](../ch16-zero-alloc-dod/) |
| 存储 | 会话、排行榜、战绩 | Redis pipeline、ZSET；`database/sql` 连接池；写回队列 | [Ch21](../ch21-storage-cache/) |
| 可观测性 | 结构化日志、指标、追踪、剖析、崩溃现场 | slog、runtime/metrics、OTel、FlightRecorder、traceback 标签 | [Ch15](../ch15-profiling-bench/)、[Ch23](../ch23-build-observability/) |
| 发布与容灾 | 房间节点排空、网关无损重启、节点宕机后的房间结算 | Drainer、fd 交接、注册中心摘除 | [Ch10](../ch10-context-structured/)、[Ch24](../ch24-runtime-boundaries/) |

**目录规划**（带教时创建）：

```
ch25-capstone/
├─ proto/                 # 协议定义
├─ go/
│  ├─ cmd/{gateway,match,room,bot}/
│  ├─ internal/{codec,actor,session,registry,sync,store,obs}/
│  └─ deploy/             # docker compose（Redis / PostgreSQL / 服务），在 WSL2 / Linux 上运行
├─ java/room-node/        # Java 25 房间节点（Maven 工程；本机已有 Maven 3.9）
└─ notes/                 # 设计文档、ADR、压测报告、选型备忘录
```

### 必须写成 ADR 的取舍
1. **同步模型**：状态同步（服务器权威，适合防作弊和中途加入）vs 帧同步（只转发输入，带宽小但要求客户端确定性）。本项目两种都实现，按房间类型选择。
2. **网关↔房间传输**：自研多路复用 TCP（热路径，零拷贝转发）vs gRPC 双向流（开发效率高、有现成治理）。建议热路径自研、控制面 gRPC，ADR 里写清依据。
3. **actor 粒度**：每房间一个 actor vs 每玩家一个 actor。房间内交互频繁，按房间划分能消除大部分跨 actor 消息。
4. **房间状态是否可迁移**：可迁移意味着状态必须可序列化，tick 逻辑必须能在新节点恢复；不可迁移就接受"节点宕机 = 房间提前结算"。
5. **持久化策略**：结算结果同步写 vs 异步写回队列（写回失败的重试和幂等）。

## ☕ Java 25 深度复习（M7 对照实现要用到的能力）

- **线程模型**：每个连接一个虚拟线程负责收发（[Ch17](../ch17-netpoll-io/)）；房间 tick **不放在虚拟线程上**（没有时间片抢占，见 [Ch07](../ch07-scheduler/)），而是放在固定数量的平台线程上，每个房间的 tick 在同一个线程上串行执行，保证房间内单线程语义。
- **结构化并发（JEP 505，preview）**：房间生命周期中的并发子任务，比如"等待全部玩家加载完成，超时就踢出未就绪玩家"用 `StructuredTaskScope.open(Joiner.allSuccessfulOrThrow(), cfg -> cfg.withTimeout(...))`；结算时并发写 Redis 和数据库，任何一个失败都会取消其余子任务。
- **ScopedValue（JEP 506）**：把 roomID、traceID 绑定到处理链上，替代 ThreadLocal（百万虚拟线程下 ThreadLocal 成本高，见 [Ch10](../ch10-context-structured/)）。
- **内存与 GC**：对比 G1 和分代 ZGC；开关 `-XX:+UseCompactObjectHeaders`（JEP 519）看堆占用和 GC 变化（[Ch14](../ch14-gc/)）。
- **启动**：用 AOT 缓存（JEP 514/515）缩短扩容时的达峰时间，并在报告里量化（[Ch23](../ch23-build-observability/)）。
- **剖析**：JFR 常开，重点看 GC 暂停、safepoint、`jdk.VirtualThreadPinned`；热点方法用 JFR 方法计时（JEP 520）确认（[Ch15](../ch15-profiling-bench/)）。

## 🐹 Go 1.27 深入（本项目要综合运用的能力）

### 规范层
- 房间状态用值语义的 struct 组织，实体用 SoA 或 `[]Entity` 连续存储（[Ch01](../ch01-values-memory/)、[Ch16](../ch16-zero-alloc-dod/)）；消息类型用小接口 + type switch 分发（[Ch03](../ch03-interfaces/)）。
- 用泛型写通用的 actor 骨架和类型安全的 ask（[Ch04](../ch04-generics/)）。**1.27 新增**的泛型方法可以把"按消息类型注册处理器"这类 API 放在类型的方法集里，而不是包级函数。
- 迭代器暴露房间内的实体遍历（AOI 查询返回 `iter.Seq`，[Ch05](../ch05-iterators/)）。

### 运行时与工具链层
- **tick 循环**：固定步长 + 累加器，`time.Ticker` 的漂移处理和追帧上限（[Ch11](../ch11-actor/)）；每个房间的 goroutine 用 `pprof.Do(ctx, pprof.Labels("room", id), ...)` 打标签，CPU profile 和崩溃 traceback（**1.27 新增**带标签）都能定位到房间。
- **tick 超时现场**：常驻 `runtime/trace.FlightRecorder`，单个 tick 超过阈值（比如 3 倍 tick 间隔）时 `WriteTo` 落盘，事后用 `go tool trace` 分析（[Ch15](../ch15-profiling-bench/)）。
- **零分配广播**：快照编码到复用的 buffer（`sync.Pool` + `AppendXxx` 风格编码），同一份字节广播给房间内所有玩家；目标是稳态下每 tick 0 次堆分配（`testing.AllocsPerRun` 断言，[Ch16](../ch16-zero-alloc-dod/)）。
- **ID**：房间 ID、对局 ID 用 `uuid.NewV7()`（**1.27 新增**的 `uuid` 包），按时间有序，利于数据库索引和日志排查（[Ch19](../ch19-serialization/)）。
- **后台接口**：运营后台 HTTP 接口用 `encoding/json/v2`（**1.27 新增**转正），测试用 `httptest.NewTestServer` + `testing/synctest`（[Ch20](../ch20-http-rpc/)）。
- **确定性测试**：房间逻辑（倒计时、超时踢人、结算）全部用 `testing/synctest` 测试，假时钟下秒级逻辑在毫秒内跑完（[Ch12](../ch12-concurrency-testing/)）。
- **泄漏巡检**：混沌测试结束后抓 `goroutineleak` profile（**1.27 新增**转正），要求为零（[Ch12](../ch12-concurrency-testing/)）。
- **资源配额**：容器里依赖 GOMAXPROCS 的 cgroup 感知，GOMEMLIMIT 按容器内存的 85–90% 设置（[Ch23](../ch23-build-observability/)）。

### 源码阅读清单（第三方框架，对照你自己的设计读）
| 项目（第三方） | 读什么 | 带着什么问题读 |
|---|---|---|
| Nakama（heroiclabs/nakama） | 权威对局处理器：`MatchInit` / `MatchJoinAttempt` / `MatchJoin` / `MatchLeave` / `MatchLoop` / `MatchTerminate` 这套接口和它的 tick 驱动 | 它的 tick 循环如何处理输入缓冲和超时？对局处理器崩溃时怎么隔离？和你的房间 actor 有什么不同？ |
| Pitaya（topfreegames/pitaya） | 前端 / 后端服务器划分、集群 RPC、会话绑定 | 会话状态放在哪里？跨服务器 RPC 的延迟和失败怎么处理？ |
| nano（lonng/nano） | 组件、会话、分组广播 | 广播是怎么组织的？和你的零分配广播相比，分配发生在哪里？ |
| Leaf（name5566/leaf） | 模块、chanrpc、skeleton | 它的"模块 = goroutine + channel"模型和 actor 有何异同？ |
| Agones（googleforgames/agones） | GameServer、Fleet、GameServerAllocation 的概念 | 在 Kubernetes 上，有状态的房间进程如何被分配、排空和回收？ |
| Go 标准库 | `runtime/trace/flightrecorder.go`、`testing/synctest/synctest.go`、`net/http/pprof/pprof.go` | 你在项目里用到的这些能力，各自的开销和限制是什么？ |

## 📊 Java 25 ↔ Go 1.27 机制级对照表（M7 要用数据填满）

| 维度 | Java 25 房间节点 | Go 1.27 房间节点 | 取舍 / 要回答的问题 |
|---|---|---|---|
| 房间执行模型 | 固定平台线程池，房间 tick 在同一线程串行执行 | 每房间一个 goroutine，运行时抢占 | 房间数远大于核数时，谁的 tick 抖动更小？ |
| 连接收发 | 每连接一个虚拟线程 | 每连接一个读 goroutine + 写队列 | 1 万连接下内存和 CPU 各是多少？ |
| 房间内状态 | 单线程封闭，无锁 | 单 goroutine 封闭，无锁 | 一致：都要求跨房间只通过消息交互 |
| 生命周期子任务 | `StructuredTaskScope`（preview） | `context` + `WaitGroup.Go` / errgroup | 取消传播和错误聚合，哪个更不容易出错？ |
| 上下文传递 | `ScopedValue` | `context.Context` 显式参数 | 代码侵入性 vs 类型安全 |
| 内存布局 | 对象数组（引用）+ 紧凑对象头（可选） | 连续 struct / SoA | 同样的实体数量，堆大小和 GC 扫描成本差多少？ |
| GC | G1 / 分代 ZGC | Green Tea（默认）+ GOGC / GOMEMLIMIT | p99.9 暂停、GC CPU 占比、内存占用 |
| 预热 | JIT 预热；AOT 缓存缓解 | 无预热 | 扩容后多久达到稳态延迟？ |
| 故障现场 | JFR 常开 + `jcmd` | FlightRecorder + pprof 标签 + traceback 标签 | 一次 tick 超时，谁能更快定位到原因？ |
| 部署 | jlink 运行时 + AOT 缓存 | 静态二进制 | 镜像大小、启动时间、运维复杂度 |

## 🗺️ 里程碑

每个里程碑都要：代码能跑、测试全绿（并发部分在 WSL2 里过 `-race`）、`notes/` 里有一页小结。

| 里程碑 | 估时 | 产出 | 验收 |
|---|---|---|---|
| **M0 骨架与规范** | 2h | 目录结构、模块划分、CI 脚本（vet / test / govulncheck）、ADR 模板、`deploy/` 下的 docker compose（Redis、PostgreSQL） | `go vet ./...` 和 `go test ./...` 通过；ADR 1–5 写出初稿 |
| **M1 单机房间** | 6h | 房间 actor、20Hz tick、输入缓冲与校验、状态同步（全量快照）、房间生命周期（等待 → 倒计时 → 进行中 → 结算 → 关闭）、进程内机器人 | synctest 覆盖倒计时、超时踢人、结算；100 个房间稳定运行，每 tick 0 次分配 |
| **M2 网关 + 协议** | 8h | 协议与编解码（fuzz）、网关会话、心跳、鉴权、背压与慢客户端踢出、网关↔房间多路复用传输 | 机器人经网关跑通完整对局；fuzz 运行 10 分钟无崩溃；注入慢客户端时网关内存平稳 |
| **M3 匹配 + 多节点** | 6h | 匹配服务（分段扩大）、房间分配、服务注册与发现接口、房间节点负载上报、增量快照 + AOI | 3 个房间节点、2000 名机器人匹配成功率 100%，P99 匹配耗时有数据 |
| **M4 存储** | 4h | Redis 会话与排行榜、PostgreSQL 战绩写回队列（重试 + 幂等键） | 杀掉 PostgreSQL 30 秒再恢复，战绩不丢、不重 |
| **M5 可靠性** | 6h | 断线重连（会话保留窗口 + 序号重放）、房间 panic 隔离与结算、房间节点排空、网关无损重启、节点宕机处理 | 三类故障演练都有数据（受影响玩家数、恢复时间）；混沌测试后 `goroutineleak` profile 为零 |
| **M6 压测调优** | 8h | 机器人集群、压测场景脚本、剖析与优化记录、GC 调优 | 1 万在线 20Hz：输入到快照 p99、tick 超时次数、GC 暂停、CPU、RSS 全部有数据；至少 3 项优化有 benchstat 或压测前后对比 |
| **M7 Java 25 对照** | 8h+ | 用 Java 25 重写房间节点（同协议，接入同一个网关），G1 / ZGC、紧凑对象头开关、AOT 缓存几组配置 | 同负载对比报告 + 一页选型备忘录（L4） |

## 📈 压测与报告

### 场景
1. **稳态**：1 万在线，1250 个房间 × 8 人，20Hz 输入，持续 30 分钟。
2. **突发**：10 秒内 2000 人同时进入匹配。
3. **慢客户端**：5% 的机器人只以正常速度的 1/10 读取数据。
4. **节点故障**：压测中途 `kill -9` 一个房间节点。
5. **滚动发布**：压测中依次排空并重启所有房间节点和网关。

### 指标（每个场景都要记录）
- 在线数、消息吞吐（入 / 出）。
- **输入到快照的端到端延迟** p50 / p99 / p999（机器人记录"输入序号出现在快照中"的时间）。
- **tick 超时次数**与 tick 实际间隔分布（房间节点上报）。
- GC：Go 用 `/sched/pauses/total/gc:seconds` 和 GC CPU 占比；Java 用 JFR 的 GC 事件。
- CPU、RSS、goroutine 数 / 线程数、每 tick 分配次数。
- 故障场景：受影响玩家数、玩家感知的中断时长、数据一致性检查结果。

### 环境要求
- 在 **Linux 或 WSL2** 上运行（epoll、cgroup、`perf`、`-race` 都需要）；机器人和服务分开部署，避免互相抢 CPU。
- 报告按 [方法论第六节](../docs/methodology.md) 的实验规范写：环境、假设、方法、原始数据、结论、有效性威胁。

### 最终交付（放 `notes/`）
1. `design.md`：架构设计文档 + ADR 1–5。
2. `loadtest-report.md`：5 个场景的数据、火焰图、trace 截图、优化前后对比。
3. `go-vs-java-memo.md`：一页选型备忘录：在什么规模、什么玩法、什么团队条件下选 Go 或 Java 25，证据是哪些数据，风险和回退方案是什么。

## 🔬 实验（贯穿里程碑的专项实验）

1. **actor 粒度**（M1）：同样 8 人房间，"每房间一个 actor"vs"每玩家一个 actor + 房间协调者"，对比消息数、延迟和代码复杂度。
2. **同步模型**（M3）：同一玩法分别用状态同步和帧同步，对比带宽（字节/秒/玩家）、服务器 CPU、断线重连的复杂度。
3. **GC 调优**（M6）：GOGC 取 50 / 100 / 200 与 GOMEMLIMIT 的组合，对比 tick 抖动和内存占用；Java 对照 G1 与 ZGC。
4. **零分配改造**（M6）：从初始实现到每 tick 0 分配，每一步都用 benchstat 记录 allocs/op、ns/op，以及对 GC 暂停的实际影响。

## 🧪 练习规划

Capstone 不再是"填桩"，而是按里程碑交付。每个里程碑都要有以下测试：

**Go**
- `internal/actor`：泛型 actor 骨架的 tell / ask / 停止 / panic 恢复，过 `-race`；ask 超时用 synctest。
- `internal/codec`：编解码表驱动测试 + `FuzzDecode` + 零分配断言。
- `internal/sync`：增量快照的正确性（对同一序列的状态变化，"全量"和"基线 + 增量"结果一致），用性质测试。
- `cmd/room`：房间生命周期的 synctest 测试；稳态 tick 的 `AllocsPerRun == 0` 断言。
- 集成测试：docker compose 起 Redis / PostgreSQL，`-count=1`，覆盖结算写回的幂等。

**Java（M7）**
- 房间 tick 执行器的单线程语义测试；`StructuredTaskScope` 加载超时路径测试；与 Go 网关的协议互通测试。

## 🎮 游戏服落地

- **玩法适配**：这套骨架覆盖 MOBA-lite、吃鸡类小局、棋牌（帧同步或回合制）、休闲对战；MMO 大世界需要在此基础上增加场景分区（AOI 跨节点）和无缝迁移，ADR 里说明边界。
- **上线清单**：资源配额、GOMEMLIMIT、管理端口隔离、崩溃收集、告警（tick 超时率、输入到快照 p99、GC 暂停、在线数断崖）都要有。
- **团队协作**：协议定义和代码生成器版本锁在仓库里（`tool` 指令，[Ch22](../ch22-modules-testing/)）；房间逻辑的测试必须是确定性的（synctest），否则 CI 不稳定。

## ✅ 验收答辩题

- [ ] [L3] 画出一次"玩家输入 → 房间处理 → 快照广播"的完整路径，标出每一段的排队点和延迟来源，并用压测数据说明哪一段占比最大。
- [ ] [L3] 你的房间 tick 在 1 万在线时 p99 是多少？tick 超时主要由什么引起？你是怎么用 FlightRecorder 和 pprof 标签找到原因的？
- [ ] [L3] 把稳态每 tick 的分配从 N 次降到 0 次，你做了哪几步？每一步对 GC 暂停和 CPU 的实际影响是多少？
- [ ] [L3] 慢客户端场景下，网关如何保证不拖垮其他玩家？背压和踢人的阈值是怎么定的？
- [ ] [L3] 房间 actor panic 时，系统会发生什么？玩家看到什么？数据如何保持一致？
- [ ] [L3] 一个房间节点被 `kill -9`，从检测到恢复的完整过程是什么？受影响的玩家怎么处理？
- [ ] [L3] 网关滚动重启时，如何做到玩家无感？你的数据显示中断了多久？
- [ ] [L3] 战绩写回队列如何保证"不丢不重"？PostgreSQL 宕机 30 秒期间发生了什么？
- [ ] [L4] 状态同步和帧同步你分别在什么场景下选？用实验 2 的数据说明。
- [ ] [L4] 网关到房间节点的热路径为什么自研传输（或者为什么用 gRPC）？如果规模扩大十倍，这个决定还成立吗？
- [ ] [L4] Java 25 版房间节点的 tick 为什么不放在虚拟线程上？如果放了，压测会出现什么现象？
- [ ] [L4] 在你的对比数据里，Go 和 Java 25 各自赢在哪些指标、输在哪些指标？差异的机制原因分别是什么？
- [ ] [L4] 如果玩法从 8 人小局变成 200 人同场景，你的架构哪些地方要改？Go 和 Java 哪个改起来更顺？
- [ ] [L4] 给出你的选型备忘录结论，并说明什么样的新证据会推翻它。

## 🕳️ 常见坑

- **跨房间共享可变状态**（全局 map 存玩家、跨房间直接调用方法）：锁和竞态又回来了。跨房间一律走消息。
- **在房间 actor 里做阻塞 I/O**（同步写数据库、调用外部 HTTP）：一个房间卡住整个 tick。I/O 要异步化，结果以消息形式回到 actor。
- **tick 用 `time.Sleep` 驱动**：误差累积导致漂移。用固定步长 + 累加器，并限制追帧次数。
- **广播时为每个玩家单独编码同一份快照**：分配和 CPU 都乘以玩家数。编码一次，多次发送。
- **压测机器人和服务跑在同一台机器上**：互相抢 CPU，数据失真。
- **只看平均延迟**：游戏体验由 p99 / p999 和抖动决定。
- **Java 对照实现照搬 Go 的结构**：比如每个房间开一个虚拟线程跑 tick。对照实现应该用 Java 25 的最佳实践，否则对比没有意义。
- **最后才考虑发布和容灾**：房间状态是否可迁移、会话是否可恢复，必须在 M1、M2 就定下来。

## 📚 一手资料

- Go：[A Guide to the Go Garbage Collector](https://go.dev/doc/gc-guide)、[Diagnostics](https://go.dev/doc/diagnostics)、`go doc runtime/trace.FlightRecorder`、`go doc testing/synctest`、`go doc runtime/pprof.Do`、[Go 1.27 Release Notes](https://go.dev/doc/go1.27)
- Java：[JEP 505](https://openjdk.org/jeps/505)、[JEP 506](https://openjdk.org/jeps/506)、[JEP 519](https://openjdk.org/jeps/519)、[JEP 514](https://openjdk.org/jeps/514)、[JEP 515](https://openjdk.org/jeps/515)、[JEP 520](https://openjdk.org/jeps/520)
- 第三方：Nakama 权威对局文档（Authoritative Multiplayer）、Pitaya、nano、Leaf 源码、Agones 文档
- 网络同步经典资料：Gabriel Gambetta《Fast-Paced Multiplayer》系列、Glenn Fiedler《Networked Physics》《Fix Your Timestep!》
