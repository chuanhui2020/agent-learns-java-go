# Ch10 · 错误处理与健壮性

> 对应路线图 **阶段 3**。状态：⚪ 未开始（待带教）。

## 🎯 目标
- 建立错误包装链，用 `errors.Is/As` 定位。
- 用 `defer` 管理连接/锁/文件释放（体会它比 finally 好用在哪）。
- 明确 `panic/recover` 边界：只在 goroutine 顶层兜底（如每个玩家连接 handler 加 recover 防止一个连接崩整个进程），不当异常用。
- 结构化日志 `log/slog`。

## 🔗 前置
Ch00, Ch01

## 📊 Java 25 → Go 对照表（雏形，带教时展开）
| 主题 | Java 25 | Go |
|---|---|---|
| 传播 | 抛异常 `throw` | 返回 `error` 值，`if err != nil` |
| 包装/链 | `new X(cause)` / `getCause()` | `fmt.Errorf("...: %w", err)` |
| 类型判断 | `catch (SpecificException)` | `errors.Is` / `errors.As` |
| 检查异常 | checked/unchecked | 无此概念，全显式返回 |
| 不可恢复 | `RuntimeException` | `panic`/`recover`（仅真正的 bug） |
| 资源清理 | try-with-resources / `finally` | `defer`（LIFO，不限于 try 块） |
| 日志 | Logback / Log4j2 | `log/slog`（标准库） |

## 📝 带教时会补
讲解笔记 · `go/demo`（可运行）· `go/exercises`（实现错误包装链 + connection handler recover）· `java/`（异常/try-with-resources 对照）。
