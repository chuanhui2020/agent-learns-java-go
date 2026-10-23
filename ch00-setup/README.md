# Ch00 · 工具链、语言版本语义与语法速通

> Part 0 · 估时 6h · 状态：🟡 按新标准重修 · 弄清"语言版本"在两条工具链里是怎么生效的，再把语法差异一次过完；规范细节在 Ch01、Ch04 深入

## 🎯 目标（资深标准）

- **L1**：跑通 Go 1.27 + JDK 25 两条链路；读 Go 代码不再卡在语法上；完成 4 个 Go 练习 + 1 个 Java drill。
- **L2**：讲清 `go.mod` 里 `go` 行的三重语义（语言版本 / 最低工具链 / 默认 GODEBUG），讲清 `GOTOOLCHAIN` 的切换规则；讲清 Java 的 preview / incubator / experimental 三种"未定稿"机制在 class 文件和启动参数上的区别。
- **L3**：能在运行前**预测**同一份代码在不同 `go` 行 / GODEBUG / `--enable-preview` 设置下是编译失败、vet 报错还是行为改变，并用 `go build`、`go vet`、`go version -m`、`javap` 的输出验证。
- **L4**：能比较两种语言演进机制的设计：Go（Go 1 兼容性承诺 + GODEBUG + 模块级语言版本）与 Java（preview / incubator + 弃用流程 + 编译单元级 `--release`），说清各自解决了什么问题、付出了什么代价。

## 🔗 前置

无。这是第一章。

## 💡 关键洞察

1. **`go.mod` 的 `go 1.27.0` 不是注释，而是语义开关。** 它决定本模块能用哪些语言特性、vet 允许用哪些标准库符号、二进制默认的 GODEBUG 取值。同一个 go1.27.3 工具链，模块写 `go 1.26.0` 时，二进制里会被写入 `DefaultGODEBUG=tracebacklabels=0,x509sslcertoverrideplatform=0`，即保留旧行为。
2. **两边都在解决"演进但不破坏"，手段不同。** Java 靠六个月一版的发布节奏，新特性先 preview / incubator，定稿后才正式；Go 靠 Go 1 兼容性承诺，用 GODEBUG 渐进切换行为变更，再用 `go` 行把切换点绑定到模块上。
3. **Go 语法的"少"是刻意的**：没有三元、没有 `while`、`switch` 默认不穿透、格式化不可配置。代价是表达力，换来的是"读任何人的代码都像读自己写的"。这一点在读 runtime 源码时（Part 2、3）价值巨大。

## ☕ Java 25 深度复习

### 启动器与源码直跑
- **JEP 458 多文件源码启动**：`java Main.java` 在内存里编译并运行，Main 引用到的同目录（按包层级）其他源文件也会被找到并编译。`bin/jrun` 就建立在这上面。注意：每次启动都要重新编译，启动成本比预编译的 class 高（启动优化见 [Ch15](../ch15-alloc-escape-compiler/) 的 AOT 缓存）。
- **JEP 512 紧凑源文件 + 实例 main（正式）**：
  - 没有类声明的源文件会被隐式包成一个 final 顶层类，文件里的字段和方法都是它的成员。
  - 启动协议允许 `main` 是实例方法、可以无参。实例 `main` 要求类有非 private 的无参构造器，由启动器负责实例化。
  - 紧凑源文件**自动导入 `java.base` 模块**导出的全部包（相当于隐式写了 `import module java.base;`）。
  - 新的 `java.lang.IO` 提供 `IO.println` / `IO.readln`。
- **JEP 511 模块导入声明（正式）**：`import module M;` 按需导入模块 M 导出的所有包（包括它 `requires transitive` 的模块）。遇到同名类型冲突时，用单类型导入消除歧义。注意它和 JPMS 的模块化编译是两回事，普通类路径代码也能用。
- **JEP 456 未命名变量 `_`**：`catch (Exception _)`、`case Point(var x, _)`，对应 Go 的空白标识符 `_`。

### "未定稿特性"的三种机制
| 机制 | 例子（JDK 25） | 怎么开启 | 实现层面 |
|---|---|---|---|
| Preview 语言/API | JEP 505 结构化并发、JEP 502 StableValue、JEP 507 原始类型模式、JEP 470 PEM | `javac/java --enable-preview --source 25`（或 `--release 25`） | 编译产物 class 文件的 minor_version = 0xFFFF；只能在**同一 JDK 特性版本**上带 `--enable-preview` 运行 |
| Incubator 模块 | JEP 508 Vector API | `--add-modules jdk.incubator.vector` | 独立的 `jdk.incubator.*` 模块，启动时打印警告 |
| Experimental VM 特性 | JEP 509 JFR CPU 时间采样（仅 Linux） | 视特性而定（JFR 事件开关 / `-XX:+UnlockExperimentalVMOptions`） | HotSpot 内部开关 |

### JDK 工具箱（后面每章都会用）
- `jcmd <pid> help`：在线诊断入口。后续用到 `VM.flags`、`VM.system_properties`、`Thread.dump_to_file`（虚拟线程转储）、`GC.heap_info`、`JFR.start/JFR.dump`。
- `jfr print / summary / view`：离线分析飞行记录（[Ch17](../ch17-profiling-bench/)）。
- `javap -c -v`：看字节码和常量池，验证 preview class 的版本号、lambda 的 invokedynamic、模式匹配的翻译。
- `jshell`：REPL，验证小语义（Ch01 的数值转换、Ch04 的 lambda 捕获都可以先在这里试）。
- **JEP 503**：JDK 25 移除了 32 位 x86 移植。了解即可：本仓库所有关于对象头、压缩指针的讨论都默认 64 位 HotSpot。

## 🐹 Go 1.27 深入

### 规范层：Java 老手必须改掉的语法直觉
- **声明**：`x := v` 只能在函数内用，且左侧在**当前作用域**至少要有一个新变量；`if v, err := f(); err != nil {}` 里的 `v` 作用域只在 if/else 内。**遮蔽（shadowing）**是 Go 头号隐性 bug 来源（尤其是 `err`）。作用域规则的完整讲解见 [Ch04](../ch04-functions-closures/)。
- **常量**：无类型常量（untyped constant）以任意精度参与常量运算，用到时才确定类型；`iota` 在每个 `const` 块里从 0 开始递增。这和 Java 的 `static final` 是两套模型（深入见 [Ch01](../ch01-types-constants/)）。
- **控制流**：只有 `for`（三段式 / 条件式 / 无限 / `range`，包括 `range` 整数和函数）；`switch` 默认不穿透，要穿透显式写 `fallthrough`，`switch {}` 等价于 `switch true {}`；带标签的 `break/continue` 能跳出外层循环。
- **函数**：多返回值、具名返回值；`defer` 的**参数在 defer 语句执行时求值**，函数体在返回前按 LIFO 执行（实现见 [Ch08](../ch08-errors-panic/)）。
- **可见性与包**：首字母大写才导出；`internal/` 目录由 go 命令强制只能被父目录树内的包导入；每个包可以有多个 `init()`，按依赖顺序执行（包初始化顺序见 [Ch04](../ch04-functions-closures/)）。
- **内建函数**：`min`、`max`、`clear`、`new(expr)`（例如 `new(42)` 得到一个指向 42 的 `*int`）。

### 工具链层：版本是语义的一部分
- **`go` 行**（`go 1.27.0`）同时规定三件事：
  1. **语言版本**：编译器按模块开关语言特性。比如在 `go 1.26` 的模块里写泛型方法，会报 "requires go1.27 or later"。
  2. **最低工具链**：本地工具链更老时，按 `GOTOOLCHAIN` 规则切换。
  3. **默认 GODEBUG**：主模块的 `go` 行越老，被写进二进制的"旧行为"设置越多（`go version -m` 里的 `DefaultGODEBUG`）。
- **`toolchain` 行与 `GOTOOLCHAIN`**：`GOTOOLCHAIN=auto`（默认）时，本地工具链低于 go.mod 要求就自动下载对应工具链（以模块 `golang.org/toolchain` 的形式缓存在 `GOMODCACHE`）。`GOTOOLCHAIN=local` 禁止切换，保证用的就是本机工具链；`GOTOOLCHAIN=go1.27.3+auto` 可以指定基线。
- **GODEBUG**：运行时和标准库行为变更的兼容开关。go.mod 的 `godebug` 块和主包的 `//go:debug` 指令决定**编进二进制的默认值**（`go list -f '{{.DefaultGODEBUG}}'` 可查看），运行时环境变量 `GODEBUG` 再覆盖这些默认值。**1.27 新增**：go 命令认识"已删除支持"的设置（如 `asynctimerchan`），设成最终默认值时接受，设成旧值时直接报错。
- **GOEXPERIMENT**：构建期开关，编进二进制，比如 `GOEXPERIMENT=simd`。它和 GODEBUG（运行期为主）是两回事。
- **vet / fix / doc**：
  - `go test` 会自动跑一组 vet 检查。**1.27 新增**：默认包含 `stdversion`，用了比文件 Go 版本更新的标准库符号就报错。
  - `go fix` 是"现代化器"的家，`go fix -diff ./...` 先看改动建议。现代化器本身就是"新语言特性 / 新 API 替代旧写法"的清单，读它等于读一份语言演进的对照表。**1.27 新增** `atomictypes`（`atomic.AddInt32(&x, 1)` 这类函数调用改用 `atomic.Int32` 等类型）、`embedlit`（利用 1.27 新语法，直接在字面量里初始化嵌入字段提升出来的字段，去掉嵌套字面量）、`slicesbackward`（倒序下标循环改成 `slices.Backward`）、`unsafefuncs`（`uintptr` 指针运算改成 `unsafe.Add`）；`waitgroup` 改名为 `waitgroupgo`。
  - `go doc`（**1.27 新增**）支持 `pkg@version`、用 `-ex` 列出可执行示例，传示例名可以直接打印示例源码。
- **诊断命令**：`go env`（看 GOROOT/GOPATH/GOCACHE/GOTOOLCHAIN）、`go version -m <二进制>`（构建信息与 DefaultGODEBUG）、`go build -x`（看实际调用的 compile/link 命令）、`go list -m all`（依赖图）。

### 源码阅读清单（`$GOROOT/src`）
| 文件 | 看什么 | 带着什么问题读 |
|---|---|---|
| `cmd/go/internal/toolchain/select.go` | `Select`、`Exec` | go 命令在什么时机、依据哪些输入决定切换工具链？切换后如何避免无限递归？ |
| `cmd/go/internal/gover/gover.go` | 版本比较 | `1.27`、`1.27rc1`、`1.27.0` 谁大谁小？为什么要这样定义？ |
| `internal/godebugs/table.go` | `Changed` / `Old` 字段 | 一个行为变更如何绑定到"从哪个 Go 版本开始默认改变"？找出 `tracebacklabels` 和 `x509sslcertoverrideplatform` 两条。 |
| `cmd/go/internal/load/godebug.go` | 计算 DefaultGODEBUG | 主模块 `go` 行如何被翻译成写进二进制的默认值？ |
| `internal/goexperiment/flags.go` | `Flags` 结构 | 当前有哪些实验开关（`SIMD`、`JSONv2`、`GreenTeaGC`、`SizeSpecializedMalloc`…）？它们为什么必须在构建期决定？ |
| `cmd/vendor/golang.org/x/tools/go/analysis/passes/stdversion/stdversion.go` | 分析器主体 | 它怎么知道一个标准库符号"是哪个版本引入的"？ |

## 📊 Java 25 ↔ Go 1.27 机制级对照表

| 维度 | Java 25 | Go 1.27 | 取舍 / 设计动机 |
|---|---|---|---|
| 版本单位 | JDK 特性版本（25），六个月一版，LTS 每两年一版 | 工具链版本（1.27.x），六个月一版，**只维护最近两个大版本** | Go 的兼容性承诺让升级几乎零成本，所以敢只维护两个版本；Java 的 LTS 服务于不愿频繁升级的生态 |
| 新特性试用 | preview（class 版本标记）/ incubator（独立模块） | GOEXPERIMENT（构建期）；语言特性没有 preview，定稿才发布 | Java 让特性在正式 JDK 里接受几轮真实反馈再定稿；Go 在提案阶段用原型迭代，发布即进入兼容性承诺 |
| 行为变更兼容 | 弃用 → `forRemoval` → 删除；系统属性兜底 | GODEBUG 默认值绑定 `go` 行，可按模块 / 按二进制覆盖 | GODEBUG 把"必须修的行为"做成有名字、有删除期限的开关，兼容性承诺因此能覆盖语义修正 |
| 语言版本粒度 | 编译单元级（`--release`） | **模块级**（每个依赖按自己的 `go` 行编译） | 模块级语言版本让"改变语义"的特性（如每次迭代新变量）可以按模块逐步生效，而不分裂生态 |
| 工具链分发 | JDK 安装包 / SDKMAN / 容器镜像 | `GOTOOLCHAIN` 自动按需下载并校验 | 工具链本身成了可声明的依赖（`toolchain` 行）；代价是隐式下载，要用 `GOTOOLCHAIN=local` 关掉 |
| 程序入口 | `void main()`（JEP 512）+ 启动器选择协议 | `package main` + `func main()`，无参无返回 | JEP 512 是在不改 JVM 的前提下降低仪式感；Go 从一开始就只有一种入口形态 |
| 源码直跑 | `java Foo.java`（内存编译，多文件 JEP 458） | `go run .`（完整编译，有构建缓存） | 两者都只是启动方式，不改变语义；Java 每次重编译，Go 依赖构建缓存 |
| 格式与风格 | Checkstyle / Spotless，可配置 | `gofmt`，不可配置 | 不可配置是刻意的：把风格从讨论里彻底移除 |
| 构建信息 | 运行参数在进程里，用 `jcmd VM.flags` / `VM.system_properties` 查 | 构建设置和 DefaultGODEBUG 嵌在二进制里，`go version -m` 离线可查 | Go 的行为默认值在构建期确定，所以必须能从二进制本身还原 |

## 🔬 实验

### 实验 1：`go` 行的三重语义（必做）
- **目的**：亲眼看到"语言版本"、"标准库版本检查"、"默认 GODEBUG"分别由 `go` 行控制。
- **方法**：在临时目录建两个模块，`go.mod` 分别写 `go 1.26.0` 和 `go 1.27.0`，代码相同：调用 `strings.CutLast`，再加一个泛型方法。
- **命令**：`go build`、`go vet .`、`go version -m <二进制> | grep GODEBUG`。
- **预期现象**：
  - `go 1.26.0` 的模块：泛型方法编译失败（requires go1.27）；删掉泛型方法后能编译，但 `go vet` 报 `strings.CutLast requires go1.27 or later (file is go1.26)`；二进制带 `DefaultGODEBUG=tracebacklabels=0,x509sslcertoverrideplatform=0`。
  - `go 1.27.0` 的模块：三项都干净。
- **记录**：三条输出原文；对照 `internal/godebugs/table.go` 里两条设置的 `Changed: 27`。

### 实验 2：工具链切换
- **方法**：本机装的 Go 低于 1.27 时，分别用 `GOTOOLCHAIN=local` 和 `GOTOOLCHAIN=auto` 在本仓库执行 `go version`；用 `go env GOROOT` 看实际使用的工具链路径。
- **预期现象**：`local` 报错，提示 go.mod 要求更高版本；`auto` 下载 1.27 工具链并切换过去，GOROOT 指向模块缓存。

### 实验 3：Java preview class 文件
- **方法**：`javac --release 25 --enable-preview -d out X.java`（X 里用 `StructuredTaskScope`），然后 `javap -v out/X.class | grep -i version`；再分别带 / 不带 `--enable-preview` 运行。
- **预期现象**：`minor version: 65535`；不带开关运行时报 preview 相关错误。
- **记录**：对比源码直跑（`bin/jrun`）和预编译运行的启动耗时，为 [Ch15](../ch15-alloc-escape-compiler/) 的 AOT 缓存实验做基线。

## 🧪 练习规划

**Go：[`go/exercises/exercises.go`](go/exercises/exercises.go)**（`go test ./ch00-setup/go/...`，初始为红）
1. `Divide(a, b int) (int, error)`：多返回值 + 错误即值，除零返回哨兵错误 `ErrDivideByZero`。
2. `FizzBuzz(n int) string`：`switch` 不穿透。
3. `SumEven(nums []int) int`：`for range` 遍历 slice。
4. `ParseEndpoint(s string) (name string, port int, err error)`：把 `"gate-01:7000"` 拆成名字和端口。练 `strings.CutLast`（1.27 新增）、`strconv.Atoi`、多个哨兵错误（`ErrNoPort`、`ErrBadPort`）和 `%w` 包装。

**Java：[`java/drills/WarmupDrill.java`](java/drills/WarmupDrill.java)**（`bin/jrun ch00-setup/java/drills/WarmupDrill.java`，初始失败）
- 用 Java 25 实现同样 4 个函数：异常 vs 错误值、switch 表达式、`record Endpoint(String name, int port)`、`lastIndexOf`。实现完对比两边的错误处理风格。

## 🧭 设计取舍

- **为什么 Go 没有 preview 语言特性。** Go 的语言变更走提案流程，先在 GOEXPERIMENT 下做原型（迭代器、泛型类型别名都这样试过），定稿发布后立刻进入 Go 1 兼容性承诺。Java 的 preview（JEP 12）让特性在正式 JDK 里接受真实使用的反馈，再决定是否修改；代价是 preview class 文件被锁在同一个特性版本上（minor_version = 0xFFFF），而 Structured Concurrency 已经是第五次预览。
- **为什么语言版本跟着模块走，而不是跟着编译器开关。** `go` 行写在代码旁边，每个依赖按自己的语言版本编译。"每次迭代新变量"这种**改变已有代码语义**的变更，正是靠它才能按模块逐步生效，而不需要全生态同时迁移。Java 的 `--release` 是编译单元级的，JLS 基本不做改变已有代码含义的变更，所以不需要这种粒度。
- **GODEBUG：兼容性承诺和"必须修复"之间的折中。** Go 1 兼容性承诺不允许破坏已有程序，但安全或正确性问题有时必须改行为。GODEBUG 把旧行为做成有名字的开关，默认值绑定 `go` 行，并且有生命周期：1.27 的 go 命令认识"已删除支持"的设置（如 `asynctimerchan`），设成旧值直接报错。Java 的对应手段是弃用流程加系统属性，但没有"按模块绑定默认值"这一层。
- **小语法是为了让规范可以通读。** 没有三元、`while`、重载、注解，Go spec 短到可以从头读完，本仓库要求每章读 spec 原文就建立在这个前提上。JLS 体量大得多，日常靠 IDE 和约定弥补。代价是表达力，Go 用"多写几行"换"读任何人的代码都像读自己的"。
- **未定稿特性怎么和稳定代码隔离。** Java 用 class 文件版本号强制隔离 preview，用独立的 `jdk.incubator.*` 模块名加启动警告隔离 incubator；Go 用 GOEXPERIMENT 构建标签，不开开关连包里的符号都看不到（例如 `simd`）。两边都在保证"不主动选择就不会误用"。

## ✅ 验收答辩题

- [ ] [L2] `go 1.27.0` 这一行控制哪三件事？各举一个可以观察到的现象。
- [ ] [L2] `GOTOOLCHAIN=auto` 时，本地 go1.26 遇到 `go 1.27.0` 的模块会发生什么？下载的工具链放在哪里，如何校验？
- [ ] [L2] Java preview 特性的 class 文件和普通 class 文件有什么区别？为什么不能拿到另一个 JDK 特性版本上运行？
- [ ] [L2] 紧凑源文件为什么可以直接用 `List`、`Map` 而不写 import？和 JEP 511 是什么关系？
- [ ] [L2] `defer f(x)` 中的 `x` 是什么时候求值的？写一个会让 Java 程序员误判输出的例子。
- [ ] [L3] 先预测：同一份使用泛型方法和 `strings.CutLast` 的代码，在 `go 1.26.0` 和 `go 1.27.0` 两个模块里，`go build`、`go vet`、`go version -m` 分别输出什么？再跑实验 1 验证。
- [ ] [L3] `:=` 遮蔽 `err` 导致错误被吞，举一个真实写法并说明 `go vet` 能 / 不能发现它。
- [ ] [L3] 用实验 1 的数据解释：为什么"标准库符号版本"由 vet 检查，而"语言特性版本"由编译器检查？
- [ ] [L4] Go 用 GODEBUG + 模块级 `go` 行处理行为变更，Java 用 preview + 弃用（`forRemoval`）流程。以"每次迭代新变量"和 JEP 12 为例，写一页评述：两种机制各自解决了什么问题，各自的代价是什么。

## 🕳️ 常见坑

- **未使用的变量 / import 是编译错误**，不是警告。
- **`:=` 遮蔽外层变量**，尤其是在 `if`/`for` 里重新声明 `err`。
- **以为 `go` 行只是"最低版本"**，随手改它，结果默认 GODEBUG 跟着变了（行为变更）。
- **没意识到 `GOTOOLCHAIN=auto` 会静默切换工具链**：`go version` 显示的可能不是你装的那个版本，用 `go env GOROOT` 确认实际用的是哪一个。
- **照 Java 习惯在每个 case 后面写 `break`**：无害但多余；反过来指望 case 会穿透，就会出错。
- **想写三元表达式**：老实写 `if/else`；取较大值直接用内建 `max`。
- **`internal/` 当成约定**：它是 go 命令强制的边界，跨树导入直接构建失败。

## 📚 一手资料

- Go：[Go Toolchains](https://go.dev/doc/toolchain)、[Go, Backwards Compatibility, and GODEBUG](https://go.dev/doc/godebug)、[Go 1 and the Future of Go Programs](https://go.dev/doc/go1compat)、[Go 1.27 Release Notes](https://go.dev/doc/go1.27)、[The Go Programming Language Specification](https://go.dev/ref/spec)、`go help mod`、`go help environment`
- Java：[JEP 512](https://openjdk.org/jeps/512)、[JEP 511](https://openjdk.org/jeps/511)、[JEP 458](https://openjdk.org/jeps/458)、[JEP 12: Preview Features](https://openjdk.org/jeps/12)、[JEP 11: Incubator Modules](https://openjdk.org/jeps/11)、`jcmd <pid> help`
- 入门补课（只在语法卡壳时用）：A Tour of Go、Go by Example
