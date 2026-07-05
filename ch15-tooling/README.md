# Ch15 · 工具链与工程化

> 对应路线图 **阶段 6**（贯穿全程，这里系统收口）。状态：⚪ 未开始（待带教）。**需额外安装**：`golangci-lint`、`delve`。

## 🎯 目标
- 表驱动测试（table-driven tests）—— Go 测试惯用法（Ch00 已见雏形）。
- `go vet` + `golangci-lint`（聚合一堆 linter）接入 CI。
- Delve（`dlv`）调试。
- 体会**单静态二进制**部署 + `GOOS/GOARCH` 交叉编译的爽感。

## 🔗 前置
Ch00（贯穿各章）

## 📊 Java 25 → Go 对照表（雏形，带教时展开）
| 用途 | Java | Go |
|---|---|---|
| 依赖/构建 | Maven / Gradle | `go mod` / `go build` |
| 测试 | JUnit / Mockito | `go test`（表驱动） |
| 静态检查 | Checkstyle / SpotBugs / Error Prone | `go vet` + `golangci-lint` |
| 调试 | IDE debugger | Delve（`dlv`） |
| 分发 | JVM + jar/容器（基础镜像上百 MB） | 单静态二进制（镜像可几 MB） |

## 🔧 安装（推进到本章时）
```bash
brew install golangci-lint
go install github.com/go-delve/delve/cmd/dlv@latest
```

## 📝 带教时会补
讲解笔记 · `go/demo`（交叉编译 + dlv）· `go/exercises`（接 golangci-lint）· Uber Go Style Guide 通读。
