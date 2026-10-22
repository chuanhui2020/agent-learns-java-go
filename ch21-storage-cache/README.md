# Ch21 · 存储与缓存集成

> Part 4 · 估时 10h · 状态：⚪ 未开始 · `*sql.DB` 是池不是连接；游戏服让玩家 actor 持有权威状态，数据库只承接可控的批量写，Redis 的瓶颈几乎总是往返次数

## 🎯 目标（资深标准）

- **L1**：正确配置 `database/sql` 连接池；写出带 context 超时、`defer tx.Rollback()` 的事务；用 go-redis 实现排行榜（ZSET）、pipeline 批量和 Lua 原子脚本。
- **L2**：
  - 讲清 `database/sql` 连接池的实现：`DB.conn` 先取空闲连接 → 达到 `MaxOpenConns` 就在 `connRequests` 排队 → `putConn` 归还时优先交给等待者 → `connectionCleaner` 按生命周期清理 → 遇到 `driver.ErrBadConn` 的 `retry` 策略；
  - 讲清事务与 context 取消的关系（`Tx.awaitDone`）、预处理语句在多连接池里的重新预处理（`Stmt.connStmt`）；
  - 讲清 1.27 新增的 `driver.RowsColumnScanner` 与 `sql.ConvertAssign`；
  - 对照 HikariCP 的 `ConcurrentBag` 与 housekeeper。
- **L3**：用 `DBStats`（`WaitCount`、`WaitDuration`、`MaxIdleClosed`…）和 pprof 诊断池耗尽与连接抖动；拿出池参数对吞吐和尾延迟影响的曲线；拿出 Redis pipeline 与逐条命令的对比数据。
- **L4**：设计玩家数据的读写模型（上线加载 → 内存权威 → 脏标记批量落盘 → 停服存盘）、缓存一致性策略和排行榜分片方案。

## 🔗 前置

[Ch10](../ch10-context-structured/)（context 超时与取消）· [Ch11](../ch11-actor/)（玩家 actor 持有状态）· [Ch20](../ch20-http-rpc/)（超时预算、重试）· [Ch19](../ch19-serialization/)（玩家数据的序列化）

## 💡 关键洞察

1. **`*sql.DB` 是连接池，不是连接**：全局一个、长期持有、并发安全。它的并发上限就是 `MaxOpenConns`，超过就在 `connRequests` 里排队。很多"数据库慢"其实是"池在排队"，看一眼 `DBStats.WaitDuration` 就知道。
2. **游戏服的数据库通常不该在请求热路径上。** 玩家上线时把数据加载进内存，玩家 actor 持有权威状态；运行期只改内存，打脏标记，定时批量落盘；下线和停服时强制落盘。数据库压力从"随玩家操作波动"变成"可控的批量写"。
3. **Redis 的瓶颈几乎总是 RTT，而不是 Redis 本身。** pipeline、批量命令、Lua 脚本都是把 N 次往返压成一次。

## ☕ Java 25 深度复习

- **HikariCP**（第三方）：
  - `ConcurrentBag`：借连接时先查本线程最近用过的（ThreadLocal 偏向），再扫共享列表，最后经 `SynchronousQueue` 等待别人归还时直接交接；
  - housekeeper 线程按 `idleTimeout` / `maxLifetime` 清理；
  - 关键参数：`maximumPoolSize`（默认 10）、`minimumIdle`、`connectionTimeout`、`maxLifetime`、`idleTimeout`、`leakDetectionThreshold`。
- **JDBC + 虚拟线程**：
  - JDBC 是阻塞 API，驱动走纯 Java socket I/O 时，虚拟线程阻塞会被 park；
  - JEP 491 之后 `synchronized` 内阻塞不再 pin 载体线程，老驱动里的大量 `synchronized` 不再是扩展瓶颈；
  - 但**真正的并发上限仍是连接池大小**："百万虚拟线程 + 20 个连接"等于百万个任务排队。应该在前面用 `Semaphore` 限制访问数据库的并发数，而不是依赖池的获取超时。
- **事务模型**：Spring `@Transactional` 依靠代理 + ThreadLocal 绑定连接，有"同类自调用事务失效"等经典陷阱。Go 的 `*sql.Tx` 是显式传递的值，没有隐式传播。
- **批量写**：`addBatch` / `executeBatch`；MySQL 驱动的 `rewriteBatchedStatements`。
- **Redis 客户端**（第三方）：
  - Lettuce：基于 Netty，单连接多路复用，命令天然流水线化，线程安全共享；
  - Jedis：每线程借一个连接（连接池）。
  - 对照 go-redis：连接池 + 每条命令借还，批量要显式 `Pipeline`。

**源码**（`src.zip`）：`java.sql/java/sql/Connection.java`、`java.sql/java/sql/ResultSet.java`、`java.base/java/util/concurrent/Semaphore.java`、`java.base/java/util/concurrent/ConcurrentSkipListMap.java`（对照 Redis ZSET 的跳表）。

## 🐹 Go 1.27 深入

### 规范层（库契约）
- **打开与验证**：`sql.Open` 只创建池对象，不建立连接；用 `PingContext` 验证可达。`*sql.DB` 并发安全，全局复用。
- **池参数**：
  - `SetMaxOpenConns`：默认 0，**不限制**；
  - `SetMaxIdleConns`：默认 2（`defaultMaxIdleConns`）；
  - `SetConnMaxLifetime`、`SetConnMaxIdleTime`；
  - `DB.Stats()` 返回 `DBStats`：`MaxOpenConnections`、`OpenConnections`、`InUse`、`Idle`、`WaitCount`、`WaitDuration`、`MaxIdleClosed`、`MaxIdleTimeClosed`、`MaxLifetimeClosed`。
- **查询**：`QueryContext` / `QueryRowContext` / `ExecContext`。`rows.Close()` 必须调用（`defer`），`rows.Err()` 必须检查，否则连接不会归还。
- **事务**：`BeginTx(ctx, &sql.TxOptions{Isolation: ..., ReadOnly: ...})`，紧跟 `defer tx.Rollback()`（`Commit` 之后再 `Rollback` 只会返回 `ErrTxDone`，无副作用）。ctx 取消后事务被自动回滚。
- **固定连接**：`db.Conn(ctx)` 拿到一条专属连接，用于会话变量、advisory lock 等需要同一连接的场景。
- **预处理语句**：`db.PrepareContext` 返回的 `*sql.Stmt` 会在需要时到其他连接上重新预处理，高并发多连接下 prepare 次数会被放大。
- **1.27 新增**：
  - `driver.RowsColumnScanner`：`NextRow() error` + `ScanColumn(scanCtx ScanContext, index int, dest any) error`。驱动实现它之后，`database/sql` 不再调用 `Rows.Next`，驱动可以跳过 `[]driver.Value` 中间层，直接扫描到用户的目标变量，减少分配；
  - `sql.ConvertAssign(scanCtx, dest, src)`：把 `Rows.Scan` 的类型转换规则暴露给驱动复用。
- **驱动**（第三方）：PostgreSQL 用 `github.com/jackc/pgx/v5`（可通过 `database/sql` 适配，也可用原生 `pgxpool`，支持二进制协议、`Batch`、COPY）；MySQL 用 `github.com/go-sql-driver/mysql`。
- **go-redis**（第三方，`github.com/redis/go-redis/v9`）：
  - 连接池（`PoolSize` 等选项）；
  - `Pipeline` / `TxPipeline`（MULTI/EXEC）；`Script`（先 EVALSHA，失败回退 EVAL）；
  - `ClusterClient`：按哈希槽路由，跨槽的 pipeline 会被拆分；
  - 排行榜命令：`ZAdd`、`ZIncrBy`、`ZRevRank`、`ZRevRangeWithScores`；所有命令都接受 context。

### 实现层：连接池怎么运转（`database/sql/sql.go`）
- **获取**：`DB.conn(ctx, strategy)`：
  1. 先从空闲列表取，并检查是否超过生命周期 / 空闲时间；
  2. 没有可用的且 `numOpen < maxOpen`，就新建连接；
  3. 否则创建一个请求放进 `connRequests`，`select` 等待"连接被交付"或 `ctx.Done()`。`WaitCount` / `WaitDuration` 就是在这里统计的。
- **归还**：`putConn` → `putConnDBLocked`：优先把连接交给排队的等待者；没有等待者才放回空闲列表；超过 `MaxIdle` 就关闭（`MaxIdleClosed++`）。
- **后台 goroutine**：`connectionOpener` 负责异步补连接，`connectionCleaner` 按生命周期 / 空闲时间周期清理。
- **坏连接重试**：`retry` 在驱动返回 `driver.ErrBadConn` 时，先用 `cachedOrNewConn` 策略重试 `maxBadConnRetries = 2` 次，再用 `alwaysNewConn` 强制新建一次。前提是驱动只在"确定请求没有发出"时返回 `ErrBadConn`，否则非幂等写可能被重复执行。
- **事务与 context**：`Tx.awaitDone` 等待 `ctx.Done()`，一旦触发就回滚并按情况丢弃连接，保证不会把一条处于事务中的连接还回池里。

### 源码阅读清单（`$GOROOT/src`）
| 文件 | 函数/类型 | 带着什么问题读 |
|---|---|---|
| `database/sql/sql.go` | `DB.conn`、`connRequests` | 池满时请求怎样排队？ctx 取消时怎样从队列移除？`WaitDuration` 在哪里累加？ |
| `database/sql/sql.go` | `putConn`、`putConnDBLocked` | 归还时优先交给谁？超过 `MaxIdle` 时发生什么？为什么空闲数太小会造成连接抖动？ |
| `database/sql/sql.go` | `connectionCleaner`、`connectionCleanerRunLocked`、`connectionOpener` | 生命周期和空闲时间两个参数各解决什么问题？清理周期怎样确定？ |
| `database/sql/sql.go` | `retry`、`maxBadConnRetries` | 什么错误会触发重试？对非幂等写有什么风险？ |
| `database/sql/sql.go` | `BeginTx`、`Tx.awaitDone`、`Tx.Rollback` | ctx 取消后事务和连接怎样处理？`ErrTxDone` 什么时候出现？ |
| `database/sql/sql.go` | `Stmt.connStmt` | 预处理语句在多连接池里的真实成本是多少？什么时候不如直接 `QueryContext`？ |
| `database/sql/driver/driver.go` · `database/sql/convert.go` | `RowsColumnScanner`、`ScanContext`、`ConvertAssign` | 新接口省掉了哪一层分配？驱动要怎样兼容旧版本 Go？ |

## 📊 Java 25 ↔ Go 1.27 机制级对照表

| 维度 | Java 25 | Go 1.27 | 取舍 / 游戏服影响 |
|---|---|---|---|
| 连接池来源 | 第三方（HikariCP 等） | 标准库 `database/sql` 内置 | Go 不需要额外选型，但默认值必须改 |
| 池实现 | `ConcurrentBag`：ThreadLocal 偏向 + `SynchronousQueue` 交接 | 互斥锁 + 空闲切片 + `connRequests` 等待集合 | HikariCP 在高争用下更省锁；Go 实现简单，看源码一下午就能读完 |
| 默认上限 | `maximumPoolSize` 默认 10 | `MaxOpenConns` 默认不限，`MaxIdleConns` 默认 2 | Go 默认值上线前必须调整：不限上限可能打爆数据库，空闲 2 会导致连接频繁建立和关闭 |
| 获取超时 | `connectionTimeout` | ctx 截止时间 | Go 用 context 统一了所有超时 |
| 泄漏检测 | `leakDetectionThreshold` | 无内置，看 `DBStats.InUse` + goroutine profile | Go 侧依靠 `defer rows.Close()` / `tx.Rollback()` 的纪律加监控 |
| 阻塞与并发上限 | JDBC 阻塞，虚拟线程 park（JEP 491 后 `synchronized` 不 pin） | 驱动基于 `net.Conn`，goroutine park | 两边的上限都是连接数，需要在前面加限流 |
| 事务传播 | `@Transactional`：代理 + ThreadLocal 绑定连接 | 显式传递 `*sql.Tx` | Go 写起来啰嗦，但没有"自调用事务失效"之类的隐式陷阱 |
| 结果扫描 | `ResultSet.getXxx` | `Rows.Scan` → 类型转换；1.27 `RowsColumnScanner` 直接扫描 | Go 的新接口让驱动减少中间分配 |
| Redis 客户端 | Lettuce 单连接多路复用 / Jedis 连接池 | go-redis 连接池 + 显式 `Pipeline` | Lettuce 天然流水线；go-redis 要主动批量 |

## 🔬 实验

### 实验 1：连接池参数扫描
- **目的**：拿到池参数对吞吐和尾延迟的影响曲线，建立调参直觉。
- **方法**：用 Docker 起 PostgreSQL（或 MySQL），`go/lab/dbpool/` 以 200 并发持续执行"读一行 + 更新一行"。扫描 `MaxOpenConns` ∈ {5, 10, 20, 50, 100} × `MaxIdleConns` ∈ {2, 等于 MaxOpen}。Java 侧可选用 HikariCP 跑同一负载（需 Maven）。
- **工具**：每秒采集一次 `DBStats`；数据库侧查看连接数（如 PostgreSQL 的 `pg_stat_activity`）；客户端统计吞吐和 p99。
- **预期**：`MaxIdle = 2` 时 `MaxIdleClosed` 飙升（连接抖动）；`MaxOpen` 太小时 `WaitDuration` 主导延迟；太大时数据库端争用让吞吐反降。
- **记录**：每组参数的吞吐、p99、`WaitCount`、`WaitDuration`、`MaxIdleClosed`。

### 实验 2：Redis pipeline vs 逐条
- **方法**：用本机 Redis（或 Docker），对排行榜做 1 万次 `ZINCRBY`：逐条、pipeline 每批 100、Lua 脚本批量三种方式。再在 Linux/WSL2 上用 `tc qdisc ... netem delay 1ms` 模拟 1ms 网络延迟重跑。
- **预期**：本地回环下 pipeline 已有数倍提升；加上 1ms 延迟后，逐条方式的耗时被 RTT 线性放大，差距扩大到两个数量级。
- **记录**：总耗时、QPS、客户端 CPU。

### 实验 3：脏标记批量落盘 vs 即时写
- **方法**：模拟 1 万在线玩家，每人每秒随机修改属性。对比"每次修改立即 UPDATE"和"每 5 秒批量 UPSERT 脏玩家"两种策略；最后模拟停服，测全量存盘耗时。
- **记录**：数据库 QPS、写放大倍数、停服存盘耗时、批量大小对单批延迟的影响。

### 实验 4：事务中途取消
- **方法**：事务执行到一半取消 ctx，验证数据被回滚；观察 `DBStats` 中连接是被丢弃还是被复用。

## 🧪 练习规划

**Go：`go/exercises`**（数据库相关测试使用练习 3 的假驱动，不依赖真实数据库）
1. `func ConfigurePool(db *sql.DB, cfg PoolConfig)` 与 `func DiagnosePool(prev, cur sql.DBStats, interval time.Duration) []Finding`：根据两次采样的差值判断"池排队严重""连接抖动""疑似泄漏（`InUse` 只增不减）"。测试：构造 `DBStats` 序列，断言诊断结果。
2. `func Transfer(ctx context.Context, db *sql.DB, from, to, amount int64) error`：`BeginTx` + `defer tx.Rollback()` + 余额检查 + `Commit`。测试：余额不足回滚、ctx 取消回滚、成功路径提交；断言事务里的所有语句都走 `tx` 而不是 `db`。
3. `package fakedb`：实现一个最小的 `driver.Driver` / `Conn` / `Stmt` / `Rows`，并让 `Rows` 实现 1.27 的 `driver.RowsColumnScanner`（`ScanColumn` 内部调用 `sql.ConvertAssign`）。测试：`database/sql` 不再调用 `Next`（计数断言）；扫描到 `int64`、`string`、`[]byte`、自定义 `Scanner` 都正确；用 `testing.AllocsPerRun` 对比实现前后的分配差异。
4. `type Leaderboard struct{...}`：基于最小接口 `ZStore`（`ZIncrBy`、`ZRevRank`、`ZRevRangeWithScores`），提供内存实现，可选再写 go-redis 实现。测试：同分时先达到者排名靠前（score 编码为"分数在高位、反向时间戳在低位"）、TopN、名次查询。
5. `type DirtyTracker struct{...}`：`MarkDirty(id int64)`、`Flush(ctx context.Context, save func(context.Context, []int64) error) error`，支持批量、去重、失败重新入队。测试：`-race`；用 `testing/synctest` 驱动定时 flush。

**Java：`java/drills`**
- `PoolDrill.java`：用 `Semaphore` + `ArrayBlockingQueue` 手写最小连接池（借还、获取超时、最大连接数），在 1 万个虚拟线程并发下自校验"同时借出数从不超过上限"、超时抛出预期异常；注释中对照 HikariCP 与 `database/sql` 的设计差异。
- `LeaderboardDrill.java`：用 `ConcurrentSkipListMap` 实现排行榜（同分先到先得、TopN、名次），自校验正确性；对照 Redis ZSET 的跳表实现。

## 🎮 游戏服落地

- **玩家数据生命周期**：上线时加载（单条或少量多表查询）→ 玩家 actor 持有权威状态 → 修改时打脏标记 → 定时批量 UPSERT → 下线 / 停服强制落盘。落盘失败进入重试队列并告警，必要时用本地 WAL 兜底。Java 侧常见的 Spring Data + 缓存注解方案，在游戏服里不如这种显式控制可靠。
- **排行榜**：Redis ZSET，score 同时编码分数和时间戳来打破平局；百万级榜单按区服或分段分片；赛季结算用 `ZREVRANGE` 分页导出，避免一次性拉取大 key。
- **缓存一致性**：公会信息、公共配置这类读多写少的数据用旁路缓存（先写库再删缓存，必要时延迟双删）；玩家私有数据不要放进共享缓存，玩家 actor 本身就是缓存。
- **停服存盘**：接在 [Ch18](../ch18-tcp-gateway/) 的排空流程之后，通知所有玩家 actor flush，等待全部完成或超时；按批限速写入，并实时观察 `DBStats` 和数据库负载，避免停服瞬间把数据库打爆。

## ✅ 验收答辩题

- [ ] [L2] 描述 `DB.conn` 获取连接的完整流程，以及 `putConn` 归还时的优先级。
- [ ] [L2] `MaxOpenConns`、`MaxIdleConns`、`ConnMaxLifetime`、`ConnMaxIdleTime` 各自解决什么问题？Go 的默认值为什么在生产上不合适？
- [ ] [L2] 事务的 ctx 被取消后，`database/sql` 内部发生了什么？连接还会回到池里吗？
- [ ] [L2] 1.27 的 `driver.RowsColumnScanner` 改变了 `Rows.Scan` 的哪条路径？对驱动作者和使用者分别意味着什么？
- [ ] [L2] HikariCP 的 `ConcurrentBag` 和 `database/sql` 的池在高争用下的行为有什么不同？
- [ ] [L3] 线上接口 p99 变高，数据库监控却显示很空闲。你怎样用 `DBStats` 证明是池在排队，并给出调整方案？
- [ ] [L3] `DBStats.InUse` 只涨不降，怎样定位是哪段代码没有关闭 `rows` 或没有结束事务？
- [ ] [L3] 用实验 2 的数据说明 RTT 对 Redis 吞吐的影响，以及跨机房部署时的应对方案。
- [ ] [L4] 为一款 5 万同时在线的 MMO 设计玩家数据存储方案：加载、内存权威、落盘频率、停服存盘、崩溃恢复，以及数据库容量估算。
- [ ] [L4] 百万玩家的全服排行榜和分服排行榜，Redis 数据结构与分片方案怎样设计？赛季结算如何避免大 key 问题？

## 🕳️ 常见坑

- **每个请求 `sql.Open` 一次**，或者把 `*sql.DB` 当成单条连接来用。
- **不改默认值**：`MaxOpenConns` 不限，突发流量打满数据库的 `max_connections`；`MaxIdleConns = 2`，连接被频繁创建和关闭。
- **忘记 `rows.Close()`、不检查 `rows.Err()`**：连接泄漏，`InUse` 只涨不降。
- **事务里用 `db.QueryContext` 而不是 `tx.QueryContext`**：语句跑在另一条连接上，根本不在事务里。
- **以为 `ErrBadConn` 的自动重试总是安全的**：驱动如果在请求可能已发出后返回 `ErrBadConn`，非幂等写会被重复执行。
- **以为 goroutine / 虚拟线程多了，数据库并发就上去了**：上限永远是连接池大小，多出来的只是在排队。
- **Redis 逐条命令循环、用 `KEYS` 扫全库、一次性 `ZRANGE 0 -1` 拉取百万成员的大 key。**
- **Cluster 模式下 Lua 脚本或事务涉及跨槽的 key**，报 CROSSSLOT 错误；需要用 hash tag（`{...}`）把相关 key 放到同一个槽。

## 📚 一手资料

- Go：`go doc database/sql`、`go doc database/sql/driver.RowsColumnScanner`、[Go 1.27 Release Notes（database/sql）](https://go.dev/doc/go1.27)；go.dev 文档 "Accessing relational databases" 系列
- Go 源码：`database/sql/sql.go`、`database/sql/convert.go`、`database/sql/driver/driver.go`
- JDK：`src.zip` 中的 `java.sql/java/sql/Connection.java`、`java.base/java/util/concurrent/ConcurrentSkipListMap.java`；[JEP 491](https://openjdk.org/jeps/491)
- Redis 官方文档：Pipelining、Scripting with Lua、Cluster specification（哈希槽与 hash tag）、Sorted Sets
- 第三方：HikariCP README 与 "About Pool Sizing" wiki、`github.com/jackc/pgx`、`github.com/redis/go-redis` 文档
