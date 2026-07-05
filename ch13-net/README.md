# Ch13 · net 与网络模型 ⭐

> 对应路线图 **阶段 5**（游戏服核心战场）。状态：⚪ 未开始（待带教）。

## 🎯 目标
- 用标准库 `net` 写 TCP/UDP 服务器，采用 **goroutine-per-connection** 模型。
- 掌握 `io.Reader`/`io.Writer` 接口组合。
- 理解 goroutine-per-conn 与虚拟线程 thread-per-conn 理念殊途同归；与 Netty Reactor 是另一条路。

## 🔗 前置
Ch04, Ch05, Ch10

## 📊 Java 25 → Go 对照表（雏形，带教时展开）
| 主题 | Java 25 | Go |
|---|---|---|
| 连接模型 | 虚拟线程 thread-per-connection / Netty Reactor | goroutine-per-connection |
| 流抽象 | Java IO/NIO 流 | `io.Reader` / `io.Writer`（小接口可组合） |
| 超高连接数 | Netty | `gnet`（事件驱动 epoll，Ch14 提及） |

## 📝 带教时会补
讲解笔记 · `go/demo`（可运行 TCP echo/房间广播）· `go/exercises`（写个带心跳的 TCP server）· `java/`（虚拟线程 thread-per-conn 对照）。
