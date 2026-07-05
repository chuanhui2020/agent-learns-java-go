# Ch08 · 手写 Actor（Pekko 桥接）⭐

> 对应路线图 **阶段 2**。状态：⚪ 未开始（待带教）。这一章直接对接你的 Pekko/Akka 经验。

## 🎯 目标
- 手写极简 actor：goroutine 持有私有状态，只通过 mailbox channel 收消息处理，永不共享内存。
- 理解这如何天然消除数据竞争 —— 正是你在 Pekko 里享受的"单线程内无锁"保证。
- 这个模式是 Ch16 游戏房间/玩家实体的基础。

## 🔗 前置
Ch04, Ch05, Ch06

## 📊 Pekko → Go 对照（雏形，带教时展开）
| Pekko/Akka | Go |
|---|---|
| 一个 actor | 一个 goroutine（私有状态，无锁） |
| mailbox | 一个 channel（消息队列） |
| receive 循环 | `for msg := range mailbox { ... }` |
| ask 模式 | 消息里带一个 reply channel |

> 一个 actor ≈ 一个 goroutine +一个 mailbox channel + 一个 `for range mailbox` 消息循环。

## 📝 带教时会补
讲解笔记 · `go/demo`（可运行的 counter/room actor）· `go/exercises`（实现一个带 ask 的 actor）· `java/`（Pekko 伪代码对照）。
