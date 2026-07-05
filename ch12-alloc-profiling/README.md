# Ch12 · 减少分配与剖析 ⭐

> 对应路线图 **阶段 4**。状态：⚪ 未开始（待带教）。

## 🎯 目标
- 用逃逸分析 `go build -gcflags="-m"` 看哪些逃逸到堆。
- 用 `sync.Pool` 复用高频对象（消息、buffer）—— 降 GC 压力的核心手段。
- 预分配 slice、复用 buffer、避免热路径 string 拼接。
- 用 `pprof`（CPU/heap/goroutine/mutex）+ `go test -bench` + `benchstat` 剖析。

## 🔗 前置
Ch11

## 📊 Java 25 → Go 对照表（雏形，带教时展开）
| 主题 | Java 25 | Go |
|---|---|---|
| 逃逸分析 | JIT 内部（不可见） | `-gcflags=-m`（可见可控） |
| 对象池 | 手写对象池 | `sync.Pool` |
| 剖析 | JFR / async-profiler | `pprof`（火焰图一条命令） |
| 基准 | JMH | `go test -bench` + `benchstat` |

## 📝 带教时会补
讲解笔记 · `go/demo`（sync.Pool + bench 可运行）· `go/exercises`（给热路径减分配）· `java/`（对象池对照）。
