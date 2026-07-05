# Ch00 · 环境与语法速通

> 对应路线图 **阶段 0**。目标不是"Hello World 教程"，而是快速把语法差异过一遍，把工具链跑通。

## 🎯 目标

- 装好并跑通 Go + JDK 25 两条链路。
- 一眼认出 Go 和 Java 的高频语法差异，读 Go 代码不再卡壳。
- 亲手实现 3 个小函数，把"多返回值 + 错误即值"、"switch 不穿透"、"for 是唯一循环"变成肌肉记忆。

## 🔗 前置

无。这是第一章。

## 💡 关键洞察

Go 的"少"是刻意的：没有三元、没有 `while`、没有分号、`switch` 默认不穿透、格式化不可配置。
一开始觉得"不够用"，习惯后你会发现**读任何人的 Go 代码都像读自己写的** —— 这对团队协作和读框架源码是实打实的优势。

## 📊 Java 25 → Go 对照表

| 主题 | Java 25 | Go | 取舍 |
|---|---|---|---|
| 依赖/构建 | Maven / Gradle（XML / DSL） | `go.mod` + `go build`（极简，无插件地狱） | Go 上手快、无配置地狱；Java 生态插件更全 |
| 项目布局 | Maven standard layout | 约定 `cmd/` `internal/` `pkg/` | `internal/` 是**编译器强制**的包私有边界，比 Java 的可见性更硬 |
| 程序入口 | `public static void main(String[])` → **JEP 512** 可写实例 `void main()` | `func main()`（`package main`） | 两边都在往"低仪式感入口"靠拢 |
| 局部变量 | `var x = ...`（Java 10+） | `x := ...`（连 `var` 都省） | Go 更短；类型仍是静态推断的 |
| 多值返回 | 单返回值（多值靠对象/记录/数组） | 原生多返回值 `(T, error)` | Go 的"错误即值"就建立在这上面 |
| 循环 | `for` / `while` / `do-while` | **只有 `for`**（可当 while 用） | 少即是多，一种循环覆盖所有 |
| 条件表达式 | 三元 `a>b?a:b` | **无三元**，只能 `if/else` | 逼你写清楚；热爱三元的会不适应 |
| switch | 默认穿透，要写 `break`；Java 25 有 `switch` 模式匹配 | **默认不穿透**（要穿透显式写 `fallthrough`） | Go 消除了"忘写 break"这类经典 bug |
| 资源清理 | try-with-resources / `finally` | `defer`（LIFO，不限于某个块） | `defer` 更灵活，紧挨获取处写释放 |
| 格式化 | Checkstyle / Spotless（要配、要吵） | `gofmt`（不可配、无风格之争） | 没得争，很爽 |

## 📖 讲解笔记

### 1. `go.mod` ≈ `pom.xml`，但极简
`go mod init <module>` 生成 `go.mod`（本仓库 module 名是 `agent-learns-java-go`）。没有 XML、没有 DSL。一个仓库一个 `go.mod`，`go test ./...` 就能跑遍所有包。

### 2. 项目布局约定
- `cmd/xxx/` —— 可执行程序入口（每个子目录一个 `main`）
- `internal/` —— **只有本 module 能 import** 的包（编译器强制，比 Java package-private 更硬）
- `pkg/` —— 打算给外部复用的库代码

本仓库按"每章一目录"组织，`go/demo` 就是本章的 `package main`。

### 3. 高频语法差异（详见 `go/demo/main.go`，边跑边看）
- `:=` 短声明：`name := "勇者"`，类型自动推断，只能在函数内用。
- 多返回值：`func parseLevel(s string) (int, error)` —— Go 的日常，不是返回一个"包装对象"。
- `defer`：函数返回前执行，**LIFO**。常用来紧挨着 `Open` 写 `defer Close()`。
- `iota`：常量组里自动递增，配合自定义类型做枚举。
- `for` 是唯一循环：`for i:=0;i<n;i++{}`、`for cond {}`（当 while）、`for {}`（死循环）、`for i,v := range xs {}`。
- `switch` 默认不穿透：每个 `case` 自带 `break`；要落到下一个 case 得显式 `fallthrough`。
- 没有三元：`max` 要用 `if/else` 或写个函数。
- `_` 空白标识符：丢弃不需要的返回值 `_, err := f()`。

### 4. `gofmt` —— 不可配置的格式化
存盘即格式化（编辑器接 `gofmt`/`goimports`）。没有 tab/空格之争、没有大括号换行之争。团队零成本统一。

### 5. 顺带复习 Java 25：JEP 512
Java 25 的**紧凑源文件 + 实例 main 方法**让你写：
```java
void main() { System.out.println("hi"); }
```
省掉 `public class`、`public static void main(String[])`、`String[] args` 一堆样板 —— Java 也在往 Go 一贯的极简 `func main()` 靠拢。见 `java/Hello.java`。

## ⌨️ 动手清单

- [x] 跑通 Go 示例：`go run ./ch00-setup/go/demo`，逐行对照注释里的「对照 Java」。 ✅
- [x] 装好 JDK 25（Temurin 25.0.3）后跑通 Java 对照：`bin/jrun ch00-setup/java/Hello.java`。 ✅
- [ ] **实现 `go/exercises/exercises.go` 里的 3 个 `// TODO`，让 `go test ./ch00-setup/go/...` 从红变绿。** ← 待完成
- [x] 跑一次 `go vet ./...` 确认干净。 ✅
- [ ] （可选）故意在 `switch` 的某个 case 里写 `fallthrough`，跑测试看它怎么"穿透"。

## 🧪 代码练习

编辑 [`go/exercises/exercises.go`](go/exercises/exercises.go)，实现 3 个函数：

1. `Divide(a, b int) (int, error)` —— 多返回值 + 错误即值（除零返回 `ErrDivideByZero`）。
2. `FizzBuzz(n int) string` —— 练 `switch` 不穿透。
3. `SumEven(nums []int) int` —— 练 `for range` 遍历 slice。

测试是**表驱动**的（Go 惯用法），已经写好在 `exercises_test.go`。初始一定是红的 —— 这就是你的第一份作业。

## ✅ 验收标准

- `go run ./ch00-setup/go/demo` 正常打印。
- `bin/jrun ch00-setup/java/Hello.java` 正常打印。
- `go test ./ch00-setup/go/...` **全绿**。
- `go vet ./...` 无输出（干净）。

## 🕳️ 常见坑

- **未使用的变量/import 直接编译报错**（不是 warning）。Go 强制你删掉死代码。
- `:=` 只能在函数内；包级变量用 `var`。
- 忘了 `switch` 不穿透，照 Java 习惯到处写 `break` —— 无害但多余；反过来指望它穿透会踩空。
- 想写三元结果卡住 —— 老实 `if/else`。

## ☕ 顺带复习 Java 25

- **JEP 512** 紧凑源文件 + 实例 `void main()`（已转正）。
- **JEP 458** 多文件源码启动（`java Foo.java` 直接跑，无需先 `javac`）—— 这正是 `bin/jrun` 能免构建运行的基础。

## 📚 延伸阅读

- A Tour of Go（官方交互式，2–3 小时刷完）
- Effective Go、Go by Example
- `go help mod`、`go help build`
