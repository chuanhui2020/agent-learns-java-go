# Ch03 · 泛型 · 工厂 · 选项模式

> 对应路线图 **阶段 1**。状态：⚪ 未开始（待带教）。

## 🎯 目标
- 用类型参数 + 约束（`any`/`comparable`/联合类型）写通用容器（如泛型对象池骨架）。
- 用工厂函数 `NewXxx()` + functional options 替代构造重载/默认参数（游戏服框架里到处是）。

## 🔗 前置
Ch01, Ch02

## 📊 Java 25 → Go 对照表（雏形，带教时展开）
| 主题 | Java 25 | Go |
|---|---|---|
| 泛型 | 类型擦除、`? extends`、不支持原始类型 | 类型参数 + 约束（GC-shape stenciling，常见路径无装箱） |
| 构造 | 构造函数重载、JEP 513 灵活构造体 | 工厂函数 + functional options |
| 场景覆盖 | `record` + `sealed` + 模式匹配 | struct + 小接口 + 类型 switch |

## 📝 带教时会补
讲解笔记 · `go/demo`（可运行）· `go/exercises`（表驱动测试）· `java/`（泛型/record/sealed 对照 demo）。
