# Ch05 · Channel 与 select ⭐

> 对应路线图 **阶段 2**。状态：⚪ 未开始（待带教）。

## 🎯 目标
- 掌握 channel：有/无缓冲、`close` 语义、`range` 遍历、方向类型。
- 掌握 `select` 多路复用 —— **Java 没有直接对应物的杀手锏**。
- 内化："Don't communicate by sharing memory; share memory by communicating."

## 🔗 前置
Ch04

## 📊 Java 25 → Go 对照表（雏形，带教时展开）
| 场景 | Java 25 | Go |
|---|---|---|
| 队列通信 | `BlockingQueue` + 锁 | channel |
| 多路等待 | `CompletableFuture` 组合（较绕） | `select`（直观） |
| 关闭/遍历 | poison pill 约定 | `close(ch)` + `for range ch` |

## 📝 带教时会补
讲解笔记 · `go/demo`（select 可运行）· `go/exercises` · `java/`（BlockingQueue/CompletableFuture 对照）。
