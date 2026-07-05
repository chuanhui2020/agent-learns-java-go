# Ch07 · context vs 结构化并发 ⭐

> 对应路线图 **阶段 2**。状态：⚪ 未开始（待带教）。

## 🎯 目标
- 用 `context.Context` 实现超时取消、优雅关闭、请求域传值。
- 拿 Java 25 `StructuredTaskScope` + `ScopedValue` 写等价逻辑，对比可读性与类型安全。

## 🔗 前置
Ch04, Ch05

## 📊 Java 25 → Go 对照表（雏形，带教时展开）
| 需求 | Java 25 | Go |
|---|---|---|
| 取消/超时 | StructuredTaskScope（JEP 505 preview） | `context.WithCancel/WithTimeout` |
| 请求域值 | Scoped Values（JEP 506 转正） | `context.WithValue`（无类型 `any`） |
| 传递方式 | 结构化作用域 | 约定：第一个参数层层手动传 |

> 关键洞察：Java 25 用结构化并发 + Scoped Values 把 Go `context` 那类需求做得更结构化、更类型安全 —— 几乎是官方在"补 Go 的 context 课"。preview 特性用 `bin/jrun`（已带 `--enable-preview`）。

## 📝 带教时会补
讲解笔记 · `go/demo`（优雅关闭可运行）· `go/exercises` · `java/`（StructuredTaskScope 对照，需 --enable-preview）。
