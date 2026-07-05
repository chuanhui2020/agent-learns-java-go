# Ch02 · 接口 · 组合 · 嵌入

> 对应路线图 **阶段 1**。状态：⚪ 未开始（待带教）。

## 🎯 目标
- 掌握**隐式/结构化**接口：方法签名匹配即满足，无需 `implements`。
- 用结构体嵌入（embedding）+ 小接口替代继承。
- 体会"面向接口"更彻底、mock/测试更容易。

## 🔗 前置
Ch01

## 📊 Java 25 → Go 对照表（雏形，带教时展开）
| 主题 | Java 25 | Go |
|---|---|---|
| 抽象 | `interface` + `implements`（名义） | `interface`（结构化、隐式） |
| 复用 | 继承 / `sealed` | 结构体嵌入（组合，非 is-a） |
| 粒度 | 常见大接口 | 小接口（`io.Reader` 只一个方法） |

## 📝 带教时会补
讲解笔记 · `go/demo`（可运行）· `go/exercises`（表驱动测试）· `java/`（implements/sealed 对照 demo）。
