# Ch16 · 实战 Capstone：房间制游戏服 ⭐

> 对应路线图 **阶段 7**。状态：⚪ 未开始（待带教）。**需**：`redis`（+ `protoc`）。前面全是零件，这里组装。

## 🎯 目标
做一个**房间制实时多人小游戏服务端**（联机对战/棋牌/简单 MMO 房间），把前面所学串起来。

## 🔗 前置
Ch04–Ch14（尤其 Ch08 actor、Ch10 错误、Ch13 net、Ch14 序列化）

## 🏗️ 建议架构（正好复用各章）
- **网关层**：goroutine-per-connection 收发（Ch13），protobuf 编解码（Ch14），心跳。
- **逻辑层**：每个房间 = 一个 goroutine + mailbox channel（Ch08 actor），房间内状态单 goroutine 无锁处理。
- **玩家实体**：同理，消息驱动。
- **机制**：广播、帧同步/状态同步、优雅关闭（`context` 取消，Ch07）。
- **集成**：Redis 做会话/排行榜（Ch14），`pprof` 压测调优 + `sync.Pool` 复用消息对象（Ch12）。

## 📚 读源码（对照你的 Java 认知看别人怎么架构）
- **Pitaya** —— 分布式游戏服框架，actor-ish，生产级。
- **nano** —— 轻量、组件化，适合先读。
- **Leaf** —— 经典国产框架，模块化清晰。

> 读这些时刻意对比：用 Netty + Pekko 你会怎么搭？它们用 goroutine+channel 又怎么解决同样的房间/玩家/广播/分布式问题？这一步把你的 Java 游戏服经验完整迁移过来。

## 📝 带教时会补
分阶段搭建：网关 → 房间 actor → 玩家 → 广播/同步 → Redis → pprof 调优。每步一个可运行里程碑。
