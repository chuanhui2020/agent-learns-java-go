# agent-learns-java-go

> 面向**资深 Java 游戏服务器开发**的 Go 深入学习仓库 —— 对照 Java 25，逐章带教、可运行、可动手。
>
> **一句话总纲**：把 Go 当成"Java 游戏服经验的另一种表达方式"来学 —— 你不是在学新领域，而是在学同一套硬功夫的第二种母语。

学习方法论详见 [`docs/methodology.md`](docs/methodology.md)（先花 10 分钟读「心态转变 8 条」，能少走一半弯路）。

---

## 怎么用这个仓库

每一章一个目录 `chNN-xxx/`，结构统一：

```
chNN-xxx/
├─ README.md        # 讲解笔记 + Java25↔Go 对照表 + 动手清单 + 验收标准 + 常见坑
├─ go/
│  ├─ demo/         # 可运行的讲解示例（我写）：go run ./chNN-xxx/go/demo
│  └─ exercises/    # 练习桩 + 表驱动测试（你实现，我 review）：go test ./chNN-xxx/go/...
└─ java/            # Java 25 对照示例（可运行）：bin/jrun chNN-xxx/java/Xxx.java
```

**带教循环**（每章一个回合）：
1. 我写满该章 `README.md` + `go/demo` 可运行示例 + `java/` 对照 demo；
2. 我留 `go/exercises` 练习桩（表驱动测试，初始是**红**的）；
3. 你实现练习让测试转**绿**，我 review + 纠"用 Java 思维写 Go"的坑；
4. 勾掉下面进度表，进入下一章。

### 运行命令速查

| 目的 | 命令 |
|---|---|
| 跑某章 Go 示例 | `go run ./ch00-setup/go/demo` |
| 跑某章 Go 练习测试 | `go test ./ch00-setup/go/...` |
| 竞态检测（Ch09 起养成习惯） | `go test -race ./...` |
| 全仓库测试 | `go test ./...` |
| 静态检查 | `go vet ./...` |
| 跑某章 Java 对照 | `bin/jrun ch00-setup/java/Hello.java` |

---

## 环境

- **Go** `1.26.4`（已就位）
- **JDK 25**（Eclipse Temurin，LTS）—— 用于 Java 对照示例。安装：
  ```bash
  brew install --cask temurin@25
  # 若需要： echo 'export JAVA_HOME=$(/usr/libexec/java_home -v 25)' >> ~/.zshrc
  ```
- 后期章节按需安装：`protoc`（Ch14/16）、`redis`（Ch14/16）、`golangci-lint`/`delve`（Ch15）。

---

## 章节地图（进度）

映射《Go 深入学习路线图》阶段 0–7。重头戏是 **04–09（并发）** 和 **11–14（内存/网络）**，投入最多时间；01–03 速通即可。

| 章 | 标题 | 阶段 | 状态 |
|----|------|------|------|
| [00](ch00-setup/) | 环境与语法速通 | 0 | 🟢 已带教（练习待你完成） |
| [01](ch01-types/) | struct 与值/指针语义 | 1 | ⚪ 未开始 |
| [02](ch02-interfaces/) | 接口 · 组合 · 嵌入 | 1 | ⚪ 未开始 |
| [03](ch03-generics-options/) | 泛型 · 工厂 · 选项模式 | 1 | ⚪ 未开始 |
| [04](ch04-goroutines/) | Goroutine vs 虚拟线程 ⭐ | 2 | ⚪ 未开始 |
| [05](ch05-channels-select/) | Channel 与 select ⭐ | 2 | ⚪ 未开始 |
| [06](ch06-sync/) | 同步原语 ⭐ | 2 | ⚪ 未开始 |
| [07](ch07-context/) | context vs 结构化并发 ⭐ | 2 | ⚪ 未开始 |
| [08](ch08-actor/) | 手写 Actor（Pekko 桥接）⭐ | 2 | ⚪ 未开始 |
| [09](ch09-concurrency-pitfalls/) | 并发陷阱与 -race ⭐ | 2 | ⚪ 未开始 |
| [10](ch10-errors/) | 错误处理与健壮性 | 3 | ⚪ 未开始 |
| [11](ch11-gc-memory/) | GC 与内存布局 ⭐ | 4 | ⚪ 未开始 |
| [12](ch12-alloc-profiling/) | 减少分配与剖析 ⭐ | 4 | ⚪ 未开始 |
| [13](ch13-net/) | net 与网络模型 ⭐ | 5 | ⚪ 未开始 |
| [14](ch14-serialization/) | 序列化与服务集成 ⭐ | 5 | ⚪ 未开始 |
| [15](ch15-tooling/) | 工具链与工程化 | 6 | ⚪ 未开始 |
| [16](ch16-capstone/) | 实战 Capstone：房间制游戏服 ⭐ | 7 | ⚪ 未开始 |

状态图例：⚪ 未开始 · 🟡 带教中 · 🟢 已带教 · ✅ 练习完成并 review

---

## 目录结构

```
agent-learns-java-go/
├─ README.md              # 本文件：主索引 + 进度
├─ go.mod                 # 单一根 module（go test ./... 一键跑全仓库）
├─ bin/jrun               # Java 25 单文件源码运行脚本
├─ docs/methodology.md    # 心态转变 8 条 + 对照模板
├─ ch00-setup/ … ch16-capstone/
└─ ...
```
