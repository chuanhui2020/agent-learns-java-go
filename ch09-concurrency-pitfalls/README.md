# Ch09 · 并发陷阱与 -race ⭐

> 对应路线图 **阶段 2**。状态：⚪ 未开始（待带教）。

## 🎯 目标
- 认清并发坑：goroutine 泄漏、nil/closed channel、循环变量捕获、数据竞争。
- 养成 `go test -race` / `go run -race` 习惯（Go 内置竞争检测器，很强）。
- 对照 Go 内存模型 happens-before 与 JMM。

## 🔗 前置
Ch04–Ch08

## 📊 Java 25 → Go 对照表（雏形，带教时展开）
| 主题 | Java 25 | Go |
|---|---|---|
| 竞争检测 | review + JCStress | `go test -race`（内置） |
| 内存模型 | JMM happens-before | Go 内存模型 happens-before |
| 经典坑 | lambda 捕获 | 循环变量（1.22 已改每次迭代新变量，但读老代码要知道） |
| channel 坑 | —— | nil channel 永久阻塞；向已关闭 channel 发送 → panic |

## 📝 带教时会补
讲解笔记 · `go/demo`（故意制造泄漏/竞争，用 -race 抓）· `go/exercises` · `java/`（JMM 对照）。
