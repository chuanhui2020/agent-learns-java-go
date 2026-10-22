# Ch22 · 模块、测试与静态分析

> Part 5 · 估时 10h · 状态：⚪ 未开始 · 依赖可预测、测试可信、静态检查能自动修复：工程化的三根柱子

## 🎯 目标（资深标准）

- **L2**：讲清 MVS（最小版本选择）的算法和"为什么不需要 lock 文件也能复现构建"；讲清 Maven 的"最近者优先"、Gradle 的"最高版本优先"各自会在什么情况下坑你。
- **L2**：讲清 `go test` 默认会跑哪些 vet 分析器、为什么只跑这些；讲清 fuzz 的覆盖率引导原理；讲清 `go fix` 现代化器和 `//go:fix inline` 的工作方式。
- **L3**：能定位并修复"依赖被意外升级 / 降级"、"测试在 CI 偶发失败"、"缓存导致测试没真正执行"这几类工程问题；能用 fuzz 找出一个编解码 bug，并把崩溃用例固化成回归测试。
- **L4**：能为多模块游戏后端设计依赖治理（私有模块、漏洞扫描、升级节奏）和测试分层（单元 / 集成 / 模糊 / 压测）方案。

## 🔗 前置

[Ch00](../ch00-setup/)（`go` 行与工具链）、[Ch06](../ch06-errors-panic/)（错误设计）、[Ch12](../ch12-concurrency-testing/)（并发测试）、[Ch15](../ch15-profiling-bench/)（benchmark）

## 💡 关键洞察

1. **MVS 选的是"满足所有约束的最小版本"，不是"最新版本"。** 构建列表由 go.mod 图唯一确定，与时间、仓库状态无关，所以 `go.sum` 只负责完整性校验，不负责锁版本。升级永远是显式动作（`go get`）。Maven 的"最近者优先"可能**悄悄降级**传递依赖，这是另一种失败模式。
2. **Go 的测试体系是工具链的一部分。** `go test` + `testing` + vet + fuzz + 覆盖率 + benchmark + 测试缓存是一套整体设计。你不需要 JUnit + Mockito + Jacoco + Surefire 的组合，但也没有"注解驱动的魔法"，所有东西都是普通代码。
3. **静态分析从"报告问题"进化到"自动修复"。** vet 和 fix 共用 analysis 框架：analyzer 给出 `SuggestedFix`，`go fix` 负责应用。库作者用 `//go:fix inline` 标注弃用 API，下游一条 `go fix` 就能完成迁移。这比 Java 的 `@Deprecated` 加 OpenRewrite 配方更内建。

## ☕ Java 25 深度复习

### 依赖仲裁
- **Maven**：依赖调解规则是**最近定义优先**（依赖树里离根最近的声明胜出），深度相同则**先声明者优先**。因此传递依赖可能被一个浅层的老版本覆盖，即降级。治理手段：`dependencyManagement` / BOM 统一版本，`maven-enforcer-plugin` 的 `dependencyConvergence` 规则把冲突变成构建失败，`mvn dependency:tree -Dverbose` 查看被淘汰的版本。
- **Gradle**：默认冲突解决是**选最高版本**；可以用 `strictly`、`constraints`、platform 加以约束，用 dependency locking 生成锁文件来保证可复现。
- 对照 MVS：Gradle 的"最高版本"在结果上和 MVS 接近（都取约束中的最大值），区别在于 Gradle 解析时可能要拉取动态版本（`1.+`），MVS 只看已声明的确切版本。

### JPMS 与 JEP 511
- `module-info.java`：`requires` / `requires transitive` / `exports` / `opens`，强封装（反射访问非开放包会失败），禁止拆分包（split package）。
- **JEP 511 `import module`（正式）只是导入语法糖**，不会把代码变成模块化代码。类路径上的普通代码也能用。易混点：`import module` 导入的是"模块导出的包"，而 JPMS 管的是"运行时可读性与封装"。
- 对照 Go：Go 的模块（module）是**版本化发布单元**，包（package）是**编译和封装单元**；`internal/` 是编译器强制的封装边界，没有反射后门。

### 测试与静态分析生态
- **JUnit Jupiter**：扩展模型（Extension）、`@ParameterizedTest`（对照 Go 表驱动测试）、`@Nested`、`@Timeout`。
- **Mockito**：基于字节码生成（Byte Buddy）的 mock。现代 JDK 对运行时动态加载 agent 会给出警告（JEP 451），新版 Mockito 建议显式配置 `-javaagent`。对照 Go：小接口 + 手写 fake，基本不需要 mock 框架。
- **性质测试 / 模糊测试**：jqwik（第三方）、Jazzer（第三方，基于 libFuzzer）。对照 Go 的内建 fuzz。
- **静态分析**：`javac -Xlint`、Error Prone（javac 插件，可自动修复）、SpotBugs（字节码层）、NullAway、ArchUnit（架构规则测试）。
- **JEP 467 Markdown 文档注释**：`///` 开头的 Markdown 风格 Javadoc。对照 Go doc 注释约定（[Go Doc Comments](https://go.dev/doc/comment)）。
- **代码生成**：注解处理器（`javax.annotation.processing`）在编译期参与；对照 Go 的 `go generate`，后者只是显式执行的命令，不参与编译。

## 🐹 Go 1.27 深入

### 规范层
- **import path 与模块路径**：包的导入路径 = 模块路径 + 目录。**语义导入版本**：v2 及以上的大版本必须出现在模块路径里（`example.com/lib/v2`），因此同一构建里可以同时存在 v1 和 v2。
- **构建约束**：`//go:build linux && amd64`；文件级的 `//go:build go1.27` 会把该文件的语言版本提升到 1.27（模块的 `go` 行更低时也一样）。
- **`internal` 规则**：`a/b/internal/c` 只能被以 `a/b` 为前缀的包导入。

### 工具链层
- **go.mod 指令**：`module`、`go`、`toolchain`、`godebug`、`require`、`exclude`、`replace`、`retract`、`tool`、`ignore`。
  - `tool`：`go get -tool golang.org/x/tools/cmd/stringer` 把工具版本纳入 go.mod，`go tool stringer` 运行。替代 Java 生态里"构建插件 + 版本写在插件配置里"的做法。
  - `ignore`：让某些目录不参与 `./...` 和 `all` 的匹配（比如前端资源、生成物）。
  - **1.27 新增**：对于声明 `go 1.27` 及以上的模块，`go mod tidy` 会把重复的 `require` 块合并，最多保留两个（直接依赖、间接依赖各一个）。
- **go.work**：`use`、`replace`、`go`、`toolchain`、`godebug`。本地多模块联调用；**不要提交**给下游依赖方（它只对工作区生效）。
- **MVS**：从主模块出发遍历 require 图，每个模块取图中出现过的**最大**版本（"最小"指的是不会超出任何约束所需的版本），得到构建列表。`go get m@v` 升级时会连带抬升其依赖；`go get m@none` 移除。go.mod 记录完整的直接和间接依赖，用于模块图剪枝，避免加载无关的传递依赖。
- **完整性与来源**：`go.sum` 记录模块哈希；`GOSUMDB`（默认 sum.golang.org）是透明日志，防止"同版本被篡改"；`GOPROXY` 控制下载来源；`GOPRIVATE`（及 `GONOPROXY` / `GONOSUMDB`）用于公司私有模块；`-mod=readonly`（默认）/ `vendor` / `mod`。
- **漏洞扫描**：`govulncheck`（`golang.org/x/vuln`，官方维护的独立工具）基于调用图判断漏洞函数**是否真的可达**，噪声远低于按依赖清单报警。
- **测试体系**：
  - 表驱动 + `t.Run` 子测试；`t.Parallel`；`t.Cleanup`；`t.Context()`（测试结束前自动取消）；`t.Chdir`；`TestMain` 做全局准备。
  - `t.ArtifactDir()`：测试产物目录，配合 `go test -artifacts` 保留下来（比如把 pprof、golden 差异落盘）；`t.Attr` 输出结构化属性；`t.Output()` 输出不带文件行号的缩进日志。
  - **golden 文件**：期望输出放 `testdata/`，自定义 `-update` 标志刷新。`testdata/` 目录会被 go 工具忽略为包。
  - **可测试示例**：`func ExampleXxx()` 加 `// Output:` 注释，既是文档也是测试。**1.27 新增**：`go doc -ex` 列出可执行示例，`go doc bytes.ExampleBuffer` 直接打印示例源码。
  - **fuzz**：`func FuzzXxx(f *testing.F)`，`f.Add` 提供种子，`f.Fuzz(func(t *testing.T, ...))` 写性质断言。`go test -fuzz=FuzzXxx -fuzztime=30s` 运行，覆盖率引导的变异器在 `internal/fuzz`；发现的崩溃输入写入 `testdata/fuzz/FuzzXxx/`，之后普通 `go test` 会自动当回归用例跑。
  - **覆盖率**：`-cover`、`-coverprofile`、`-coverpkg=./...`（跨包统计）。集成测试覆盖率：`go build -cover` 构建二进制，设 `GOCOVERDIR` 运行，再用 `go tool covdata` 合并。
  - **测试缓存**：输入（源码、环境变量、读取的文件）不变时直接复用结果，输出里显示 `(cached)`。`-count=1` 强制重跑。
  - **1.27 新增**：`go test -json` 的输出行可以带 `OutputType` 字段（`error` / `error-continue` / `frame`），CI 可以据此精确提取失败信息。
- **vet**：
  - `go test` 默认只跑**高精度、几乎没有误报**的子集：`atomic`、`bools`、`buildtag`、`directive`、`errorsas`、`ifaceassert`、`nilfunc`、`printf`、`slog`、`stdversion`、`stringintconv`、`tests`。其中 **`stdversion` 是 1.27 新增的默认项**。
  - 注意 `copylocks`、`loopclosure`、`lostcancel`、`waitgroup`、`hostport` 等**不在** `go test` 默认集合里，要单独跑完整的 `go vet ./...`，CI 两个都要跑。
- **go fix**：
  - 与 vet 共用 analysis 框架；`go fix -diff ./...` 预览，`go fix ./...` 应用。
  - modernize 套件包括 `any`、`minmax`、`rangeint`、`stringscut`、`slicescontains`、`waitgroupgo`、`newexpr`、`errorsastype`、`testingcontext`、`bloop` 等。**1.27 新增**：`atomictypes`、`embedlit`、`slicesbackward`、`unsafefuncs`，`waitgroup` 改名为 `waitgroupgo`。
  - `//go:fix inline`：标注在函数或常量上，`inline` 分析器会把调用点替换成其函数体。库作者用它把"弃用 API → 新 API"的迁移交给下游一键完成。
- **第三方**：golangci-lint v2（聚合 staticcheck、errcheck、gosec 等）、staticcheck。
- **代码生成**：`//go:generate stringer -type=Weapon`，`go generate ./...` 显式执行，生成物提交进仓库（不在构建时隐式运行）。

### 源码阅读清单（`$GOROOT/src`）
| 文件 | 函数 / 类型 | 带着什么问题读 |
|---|---|---|
| `cmd/go/internal/mvs/mvs.go` | `BuildList`、`Req`、`Upgrade`、`Downgrade` | MVS 为什么是多项式时间、不需要 SAT 求解器？`Downgrade` 为什么比 `Upgrade` 复杂？ |
| `cmd/vendor/golang.org/x/mod/modfile/rule.go` | 指令解析（`tool`、`ignore`、`godebug`） | 哪些指令可以写成块？未知指令如何处理（向前兼容）？ |
| `cmd/go/internal/test/test.go` | `defaultVetFlags` | 被注释掉的分析器为什么不默认开启？`stdversion` 为什么有资格成为默认项？ |
| `cmd/vendor/golang.org/x/tools/go/analysis/passes/modernize/waitgroupgo.go` | 分析器主体与 `SuggestedFix` | 一个现代化器怎么证明改写"不改变行为"？它跳过了哪些不安全的情形？ |
| `cmd/vendor/golang.org/x/tools/go/analysis/passes/inline/inline.go` | `//go:fix inline` 处理 | 内联替换时如何处理参数求值顺序和副作用？ |
| `testing/fuzz.go` · `internal/fuzz/` | `F.Add`、`F.Fuzz`、`mutator.go`、`coverage.go` | 变异器如何利用覆盖率反馈？崩溃输入如何最小化（`minimize.go`）？ |

## 📊 Java 25 ↔ Go 1.27 机制级对照表

| 维度 | Java 25 生态 | Go 1.27 | 取舍 / 游戏服影响 |
|---|---|---|---|
| 版本选择算法 | Maven 最近者优先 / Gradle 最高版本优先 | MVS：约束中的最大值，确定性 | Go 不会"静默降级"；但"想用最新版"必须显式 `go get -u` |
| 可复现性 | 依赖锁文件（Gradle locking）或 BOM 约束 | go.mod 图即锁定，go.sum 只做完整性校验 | Go 不需要额外锁文件；CI 只需 `-mod=readonly` |
| 同库多大版本 | 一个 classpath 只能有一个版本（否则需 shade） | v2+ 进入路径，可以共存 | 协议库、SDK 升大版本时 Go 能渐进迁移 |
| 封装边界 | JPMS `exports` / `opens`（反射可配置） | 包导出规则 + `internal/`（编译期强制） | Go 的边界不可绕过，适合做"框架层 vs 业务层"隔离 |
| 工具版本管理 | 构建插件版本写在 pom / gradle 里 | go.mod 的 `tool` 指令 + `go tool` | 代码生成器（协议、stringer）版本随仓库锁定 |
| 参数化测试 | `@ParameterizedTest` + 注解源 | 表驱动 + `t.Run`，就是普通代码 | Go 的用例可以随意组合、复用 |
| mock | Mockito（字节码生成） | 小接口 + 手写 fake；不鼓励 mock 框架 | 接口在使用方定义（[Ch03](../ch03-interfaces/)），fake 很便宜 |
| 模糊测试 | Jazzer / jqwik（第三方） | 内建 `go test -fuzz`，崩溃用例自动回归 | 协议解码、配置解析这类入口一定要上 fuzz |
| 静态分析 | Error Prone / SpotBugs / NullAway | vet（`go test` 自动跑子集）+ golangci-lint | Go 的默认集合零误报，可以设为阻断 |
| API 迁移 | `@Deprecated(forRemoval)` + OpenRewrite 配方 | `//go:fix inline` + `go fix` | 内部框架升级时，Go 可以一条命令迁移所有调用点 |
| 漏洞扫描 | OWASP Dependency-Check（按清单） | govulncheck（按调用图可达性） | Go 的告警更少、更准，可以接入发布阻断 |

## 🔬 实验

### 实验 1：钻石依赖下 MVS vs Maven（必做）
- **目的**：亲眼看到两种算法在同一依赖形状下选出不同版本。
- **方法**：
  - Go：在 `go/lab/mvs/` 下用 `replace` 指向本地目录，构造 `main → A v1.0 → C v1.2`、`main → B v1.0 → C v1.4`、`main → C v1.1`。执行 `go list -m all` 和 `go mod graph`。
  - Java：用三个本地 Maven 模块构造同样的形状（`mvn install` 到本地仓库），执行 `mvn dependency:tree -Dverbose`。
- **预期现象**：Go 选 C v1.4（约束中的最大值）；Maven 选 C v1.1（main 直接声明的最近者），即 B 实际拿到的 C 比它声明的低。
- **记录**：两边的依赖树输出；写一段话说明在什么情况下各自会出生产事故。

### 实验 2：fuzz 找出解码 bug（必做）
- **目的**：体验覆盖率引导 fuzz 的效率，以及崩溃用例如何变成回归测试。
- **方法**：在 `go/lab/fuzz/` 里写一个有意埋了越界 bug 的长度前缀帧解码器（声明长度大于剩余字节时没有检查），写 `FuzzDecode`，性质是"不 panic，且 `Decode(Encode(x)) == x`"。
- **命令**：`go test -fuzz=FuzzDecode -fuzztime=60s ./ch22-modules-testing/go/lab/fuzz`
- **预期现象**：几秒到几十秒内找到 panic，崩溃输入落到 `testdata/fuzz/FuzzDecode/`；修复后普通 `go test` 也会跑这个用例。
- **记录**：发现 bug 用时、执行次数（execs）、新增覆盖（new interesting）。Windows 和 Linux 都能跑。

### 实验 3：go fix 现代化一个"老写法"代码库
- **方法**：准备一份用 `wg.Add(1) + go func(){ defer wg.Done() }()`、手写 `for i := 0; i < n; i++`、`interface{}`、`strings.Index` + 切片等老写法的代码，执行 `go fix -diff ./...`。
- **预期现象**：`waitgroupgo` 把它改写成 `wg.Go(...)`，`rangeint`、`any`、`stringscut` 等也会给出改写；有些看起来能改的地方没被改（分析器无法证明行为不变）。
- **记录**：每类改写的 diff；挑一个"没被改"的例子，读对应分析器源码，解释为什么跳过。

## 🧪 练习规划

**Go：`go/exercises`**（`go test ./ch22-modules-testing/go/...`）
1. `func Render(w io.Writer, s RoomSnapshot) error` + golden 测试：期望输出放在 `testdata/*.golden`，实现 `-update` 标志刷新 golden，失败时把实际输出写入 `t.ArtifactDir()`。
2. `func DecodeFrame(b []byte) (Frame, int, error)` + `FuzzDecodeFrame`：性质断言（不 panic、往返一致、消费字节数不超过输入长度），至少 3 个种子。
3. `type Clock interface{ Now() time.Time }` + 依赖它的 `CooldownTracker`：用手写 fake 时钟测试技能冷却，不用任何 mock 库。要求接口定义在使用方包内。
4. `ExampleParseEndpoint`：为 [Ch00](../ch00-setup/) 的 `ParseEndpoint` 写可测试示例（含 `// Output:`），并用 `go doc -ex` 确认能被列出。
5. 工程题：给本仓库加一个 `tool` 指令（如 `stringer`），用 `go generate` 为一个枚举生成 `String()`，并说明为什么生成物要提交。

**Java：`java/drills`**
1. `DependencyMediationDrill.java`：给定一棵用 record 表示的依赖树，分别实现"Maven 最近者优先"和"MVS 取最大值"两种选择算法，自校验实验 1 的形状下的结果。
2. `ModuleImportDrill.java`：用 `import module java.sql;` 与同名类型（`java.sql.Date` vs `java.util.Date`）制造歧义，自校验单类型导入如何消解歧义。

## 🎮 游戏服落地

- **私有协议库与 SDK**：公司内部的协议 / 公共库发成私有模块，`GOPRIVATE=git.company.com/*`；协议库升大版本（v2）时网关和逻辑服可以分批迁移，不用"全服同时升级"。
- **测试分层**：协议编解码、配置表解析一律 fuzz；战斗公式用表驱动 + golden；房间逻辑用 synctest（[Ch12](../ch12-concurrency-testing/)）；压测单独做（[Ch25](../ch25-capstone/)）。
- **框架升级**：内部框架要废弃一个 API 时，用 `//go:fix inline` 标注新实现，下游服务的负责人执行 `go fix` 即可，不需要人工逐个改调用点。
- **CI 基线**：`go vet ./...`（完整集合）+ `go test ./...`（含默认 vet 子集）+ `govulncheck ./...` + `golangci-lint run`；Java 服务对应 Error Prone + enforcer 收敛规则 + 依赖漏洞扫描。

## ✅ 验收答辩题

- [ ] [L2] 用一个钻石依赖的例子讲清 MVS 怎么选版本，以及为什么 Go 不需要 lock 文件。
- [ ] [L2] `go.sum` 和 GOSUMDB 分别防御什么攻击？私有模块应该怎么配置？
- [ ] [L2] `go test` 默认跑哪些 vet 分析器？为什么 `copylocks` 不在里面？CI 应该怎么补齐？
- [ ] [L2] fuzz 的覆盖率引导是怎么工作的？崩溃输入保存在哪里，之后如何参与回归？
- [ ] [L2] `//go:fix inline` 解决的是什么问题？和 Java 的 `@Deprecated` 加 OpenRewrite 相比有什么优劣？
- [ ] [L3] CI 里测试显示 `(cached)`，但你怀疑它其实应该失败。测试缓存的键由什么组成？怎么证明并修复？
- [ ] [L3] 某次 `go get -u` 之后线上出现行为变化，怎么用 `go mod graph`、`go mod why`、`go version -m` 定位是哪个依赖、从哪个版本变的？
- [ ] [L3] 用实验 2 的数据说明：为什么协议解码器必须做 fuzz，而表驱动测试不够？
- [ ] [L4] 为 20 个 Go 服务设计依赖治理方案：私有模块、版本升级节奏、漏洞扫描阻断规则、工具版本管理。
- [ ] [L4] 设计游戏服的测试金字塔：哪些逻辑用表驱动、哪些用 fuzz、哪些用 synctest、哪些只能靠压测，分别给出理由。

## 🕳️ 常见坑

- **把 go.work 提交进库仓库**：下游依赖方不受 go.work 影响，你本地能编译，别人不能。
- **以为 `go.sum` 是锁文件**：版本由 go.mod 决定，go.sum 只是哈希清单；手动删 go.sum 不会改变版本选择。
- **v2+ 模块不改模块路径**：发布 `v2.0.0` 标签但 go.mod 仍写 `module example.com/lib`，下游无法正常使用。
- **只跑 `go test` 不跑 `go vet`**：`copylocks`、`loopclosure`、`lostcancel` 这些重要检查不在默认集合里。
- **用 mock 框架的思路写 Go 测试**：先抽一个巨大的接口再生成 mock。应该在使用方定义只含所需方法的小接口。
- **测试依赖外部状态却被缓存**：读取了 go 工具感知不到的外部资源（比如数据库），缓存结果掩盖了失败；集成测试用 `-count=1`。
- **go generate 当成构建步骤**：它不会在 `go build` 时自动运行，生成物必须提交。
- **fuzz 目标里做非确定性操作**（时间、随机数、全局状态）：崩溃无法复现，最小化也会失效。

## 📚 一手资料

- Go：[Go Modules Reference](https://go.dev/ref/mod)（MVS、go.mod / go.work 指令、GOPROXY / GOSUMDB）、[Minimal Version Selection（Russ Cox）](https://research.swtch.com/vgo-mvs)、[Go Fuzzing](https://go.dev/doc/security/fuzz/)、[Coverage for integration tests](https://go.dev/doc/build-cover)、[Go Doc Comments](https://go.dev/doc/comment)、[Go 1.27 Release Notes](https://go.dev/doc/go1.27)、`go help test`、`go help testflag`、`go tool fix help`
- Java：[Maven: Introduction to the Dependency Mechanism](https://maven.apache.org/guides/introduction/introduction-to-dependency-mechanism.html)、[JEP 511](https://openjdk.org/jeps/511)、[JEP 467](https://openjdk.org/jeps/467)、[JEP 451](https://openjdk.org/jeps/451)、JUnit 5 User Guide
- 工具：[govulncheck](https://go.dev/doc/security/vuln/)、golangci-lint 文档（第三方）
