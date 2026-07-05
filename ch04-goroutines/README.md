# Ch04 · Goroutine vs 虚拟线程 ⭐

> 对应路线图 **阶段 2**（并发重头戏）。状态：⚪ 未开始（待带教）。

## 🎯 目标
- 起百万 goroutine 观察内存，和虚拟线程 benchmark 对比。
- 理解 GMP 调度模型（G/M/P 关系、work-stealing、async 抢占）。

## 🔗 前置
Ch00–Ch03

## 📊 Java 25 → Go 对照表（雏形，带教时展开）
| 维度 | Java 虚拟线程（Loom, JEP 444） | Goroutine |
|---|---|---|
| 调度 | JVM 挂载到载体线程（ForkJoinPool），阻塞卸载 | Go runtime GMP，M:N 映射 OS 线程 |
| 栈 | 阻塞时栈存堆上 | 初始 ~2KB，可增长/收缩 |
| 成熟度 | 语言后期加装（早期 pin 问题） | 第一天就是核心，标准库为其设计 |

> 关键洞察：虚拟线程本质是在追 goroutine 十几年前的模型。理解一个就理解另一个。

## 📝 带教时会补
讲解笔记 · `go/demo`（百万 goroutine 可运行）· `go/exercises` · `java/`（虚拟线程对照 demo）。
