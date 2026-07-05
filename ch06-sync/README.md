# Ch06 · 同步原语 ⭐

> 对应路线图 **阶段 2**。状态：⚪ 未开始（待带教）。

## 🎯 目标
- 掌握 `sync.Mutex`/`RWMutex`（**注意 Go 锁不可重入！**）、`WaitGroup`、`Once`、`sync/atomic`。
- 知道什么时候用锁、什么时候用 channel（下一章 actor 会给出"尽量不用锁"的答案）。

## 🔗 前置
Ch05

## 📊 Java 25 → Go 对照表（雏形，带教时展开）
| 场景 | Java 25 | Go |
|---|---|---|
| 互斥 | `ReentrantLock` / `synchronized`（可重入） | `sync.Mutex` / `RWMutex`（**不可重入**） |
| 等一组任务 | `CountDownLatch` / 结构化并发 | `sync.WaitGroup` |
| 单次初始化 | `StableValue`(JEP 502) / DCL | `sync.Once` |
| 原子操作 | `j.u.c.atomic` | `sync/atomic` |

## 📝 带教时会补
讲解笔记 · `go/demo`（可运行）· `go/exercises` · `java/`（ReentrantLock/CountDownLatch 对照）。
