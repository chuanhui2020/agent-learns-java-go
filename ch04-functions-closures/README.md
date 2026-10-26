# Ch04 · 函数、闭包与初始化顺序

> Part 1 · 估时 10h · 状态：⚪ 未开始 · 从捕获变量到初始化依赖，分清语言保证与编译器实现
>
> 本章已有 Go / Java 示例、闭包基准、初始化实验和练习桩。学习状态仍为未开始；代码可运行不代表已完成 L3 验收。

## 运行入口

以下命令均从仓库根目录运行。先写预测，再运行示例；可复制 [实验报告模板](notes/lab-template.md) 记录证据。

```text
go run ./ch04-functions-closures/go/demo
go run ./ch04-functions-closures/go/initorder
go test ./ch04-functions-closures/go/demo ./ch04-functions-closures/go/lab ./ch04-functions-closures/go/initorder/...
```

Java 示例使用当前终端对应的入口：

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File .\bin\jrun.ps1 ch04-functions-closures/java/CaptureDemo.java
powershell -NoProfile -ExecutionPolicy Bypass -File .\bin\jrun.ps1 ch04-functions-closures/java/InitializationDemo.java
```

```bash
bin/jrun ch04-functions-closures/java/CaptureDemo.java
bin/jrun ch04-functions-closures/java/InitializationDemo.java
```

两个 Java demo 都自校验并打印 `ALL PASS`，使用的特性在 JDK 25 已定稿，不依赖 preview。查看字节码时可直接编译：

```text
javac --release 25 -d out ch04-functions-closures/java/CaptureDemo.java ch04-functions-closures/java/InitializationDemo.java
javap -c -v -p -classpath out CaptureDemo
```

练习运行后**初始失败**，只改 TODO，保留自校验与测试：

```text
go test ./ch04-functions-closures/go/exercises
```

Java drill 使用上面同一个 Java 入口，源文件分别为 [`CaptureDrill.java`](java/drills/CaptureDrill.java) 和 [`InitializationDrill.java`](java/drills/InitializationDrill.java)，完成后应打印 `ALL PASS`。

## 🎯 目标（资深标准）

- **L1**：正确使用函数值、闭包、方法值与方法表达式；区分短声明和赋值的循环变量语义；写出可解释的初始化顺序。
- **L2**：讲清 Go 闭包环境与逃逸分析、Java lambda 的 `invokedynamic` 与捕获参数；区分包初始化、类初始化和实例构造。
- **L3**：先预测捕获结果、接收者快照与初始化日志，再用测试、`-gcflags='-m=2'` 和 `javap -c -v -p` 验证；用分配数据检验“闭包一定上堆”的说法。
- **L4**：说明 Java 的 effectively final 限制与 Go 捕获可变变量各自的代价，以及 eager 包初始化和按需类初始化的取舍。

## 🔗 前置

[Ch00](../ch00-setup/)、[Ch01](../ch01-types-constants/)、[Ch02](../ch02-values-memory/)、[Ch03](../ch03-slice-map-string/)。后续 [Ch07](../ch07-iterators/) 用闭包理解迭代器，[Ch15](../ch15-alloc-escape-compiler/) 深入逃逸和内联。

## 💡 关键洞察

1. **Go 闭包在语义上捕获变量，编译器可以在不改变行为的前提下按值保存。** 不能把编译器的捕获策略当成规范保证，也不能从“返回闭包”直接推出每次调用都分配。
2. **Java effectively final 限制的是局部变量的重新赋值。** 捕获一个 `List` 引用后仍然能修改列表内容；这既不代表对象不可变，也不保证线程安全。
3. **初始化顺序是依赖关系的一部分。** Go 包初始化和 Java 类初始化的触发条件不同；构造函数执行顺序又是另一层问题。

## ☕ Java 25 深度复习

### lambda 与方法引用

- **JLS §15.27、§4.12.4**：lambda 捕获的局部变量必须是 final 或 effectively final，且使用前已明确赋值。捕获引用不等于复制对象；lambda 里的 `this` 指向外层实例，和匿名类不同。
- **目标类型**：lambda 需要函数式接口作为目标类型，不能写 `var f = () -> 1`；用 `IntSupplier`、`IntUnaryOperator` 等原始类型特化接口时，检查是否避免了装箱。
- **JLS §15.13**：绑定方法引用 `object::method` 在表达式求值时确定接收者；lambda `() -> holder.field.method()` 则在调用时读取字段。比较这两种写法时，先明确到底保存了哪个引用。
- **字节码与类库实现**：查看调用点的 `invokedynamic`、`BootstrapMethods` 与合成的 lambda 方法；沿 `LambdaMetafactory.metafactory`、`InnerClassLambdaMetafactory` 阅读适配过程。该实现路径不是语言规范对对象身份或分配次数的保证。
- **对象身份**：不要用 `==` 或锁住 lambda 对象来判断“是不是同一个闭包”；JLS §15.27.4 不保证每次求值都创建不同对象。HotSpot 也可能通过逃逸分析消除分配。

### 类初始化与实例构造

- **JLS §12.4.1 / §12.4.2、JVMS §5.5**：创建实例、调用静态方法、访问声明类的非编译期常量静态字段等会触发类初始化；仅引用编译期常量不触发声明类初始化（对照 [Ch01](../ch01-types-constants/)）。
- `<clinit>` 合并静态字段初始化与静态初始化块，按语言规定的次序执行；类初始化有同步协议。首次初始化失败与后续使用的错误表现不同，应分别记录。
- **JEP 513 / JLS §8.8.7**：Java 25 的灵活构造函数体允许在显式 `super(...)` / `this(...)` 前做参数检查和计算，但 early construction context 仍限制对尚未构造完成实例的访问。分别编译合法参数校验和非法实例方法调用，查看诊断；不要把它理解成任意移动构造顺序。

## 🐹 Go 1.27 深入

### 规范层

- **Function types / Function literals**：函数值可以赋值、传参、返回；零值为 nil，调用 nil 函数会 panic。函数值只能与 nil 比较，不能互相比相等，也不能作 map key。
- **捕获变量**：函数字面量是闭包，被引用的外层变量与外层函数共享，变量在仍可访问时继续存活。是否分配到堆由实现决定。
- **Method values / Method expressions**：`x.M` 创建方法值时求值并保存接收者；`T.M` 创建的方法表达式把接收者变成第一个显式参数。值接收者保存值的副本；指针接收者保存指针，后续仍能观察指向对象的修改。
- **For statements / Range clauses**：`for _, v := range xs` 每次迭代产生新变量；`var v T; for _, v = range xs` 则复用已有变量。三段式 `for` 的新迭代变量规则也要单独读规范。先用延迟调用的函数切片验证，再引入 goroutine，避免把捕获语义与调度顺序混在一起。
- **Package initialization**：先初始化依赖包，再初始化当前包的包级变量，最后调用 `init`。包级变量按依赖分析与声明顺序确定先后，不是简单从上到下执行；跨文件顺序与提交给编译器的文件顺序有关，不用文件名排序制造隐式业务依赖。
- **作用域与遮蔽**：`:=` 是否复用变量取决于当前块；内层块的 `err` 可以遮蔽外层 `err`。`init` 无参数、无返回值，不能被普通调用；初始化错误需要可控处理时，考虑显式返回错误的函数。

先预测以下两段代码的输出，再把它们放进 `main` 验证：

```go
var fresh []func() int
for _, v := range []int{1, 2, 3} {
    fresh = append(fresh, func() int { return v })
}
fmt.Println(fresh[0](), fresh[1](), fresh[2]())

var shared []func() int
var v int
for _, v = range []int{1, 2, 3} {
    shared = append(shared, func() int { return v })
}
fmt.Println(shared[0](), shared[1](), shared[2]())
```

### 运行时、编译器与源码阅读

| 文件 / 函数 | 带着什么问题读 |
|---|---|
| `runtime/runtime2.go`：`funcval` | 函数入口之外的闭包环境存在哪里？为什么这是实现细节，不能用 `unsafe` 固定假设布局？ |
| `cmd/compile/internal/escape/escape.go`：`flowClosure` | 哪些条件允许按值捕获？重新赋值、取地址和捕获值大小怎样影响决策？ |
| `cmd/compile/internal/walk/closure.go` | 立即调用的闭包怎样改写？方法值的接收者怎样传递？ |
| `cmd/compile/internal/loopvar/loopvar.go` | 循环变量按迭代区分如何进入编译器处理流程？ |
| `cmd/compile/internal/pkginit/`、`runtime/proc.go`：`doInit` / `doInit1` | 初始化任务如何表达依赖？什么时候调用用户的 `init`？ |

## 📊 Java 25 ↔ Go 1.27 机制级对照表

| 机制 | Java 25 | Go 1.27 | 取舍 / 设计动机 |
|---|---|---|---|
| 可调用值的类型 | 函数式接口 | 函数类型 | Java 接入已有名义类型系统；Go 直接把函数作为值 |
| 局部变量捕获 | final / effectively final | 可以捕获可变变量 | Java 限制局部变量重新赋值；Go 表达共享状态更直接，也需要处理同步 |
| 引用的对象 | 捕获引用后仍可修改对象 | 指针、slice 等的副本仍可共享底层数据 | 两边都要区分变量与变量指向的数据 |
| 分配与优化 | HotSpot 可消除 lambda 对象分配 | 编译器可内联、消除环境或将环境放栈上 | 都必须用编译器诊断和分配测量验证 |
| 初始化触发 | 类按首次主动使用初始化 | 依赖包先于主程序初始化 | 按需初始化推迟成本；包初始化把依赖成本前置 |
| 初始化同步 | 类初始化锁和可见性规则 | 包初始化顺序；自行启动的 goroutine 仍需同步 | 初始化协议不等于后续共享状态自动安全 |

## 🔬 实验

实验代码已经创建。原始结果保存到 `notes/`；[维护者验证记录](notes/validation-2026-10-26.md)提供本机数据供复核，不替代你自己的预测和实验报告。

### 实验 1：捕获语义与接收者快照

- **目的**：区分新迭代变量、复用变量、值接收者副本和指针接收者。
- **方法**：运行 [`go/demo/main.go`](go/demo/main.go) 的两组函数切片、值/指针接收者方法值与含 slice 字段的副本；Java 运行 [`CaptureDemo.java`](java/CaptureDemo.java)，对比对象修改、字段替换、绑定方法引用与读取字段的 lambda。
- **工具**：Go 表驱动测试；Java 自校验；`javap -c -v -p`。
- **预期现象**：Go 两组输出分别是 `1 2 3` 与 `3 3 3`；值接收者保留创建时的副本，指针接收者读到修改后的对象。
- **记录**：预测、实际输出、保存接收者的时机。这里的单线程结果不构成并发安全证据。

### 实验 2：闭包是否分配

- **目的**：检验“有闭包就有堆分配”的假说。
- **方法**：[`go/lab`](go/lab/) 提供六组基准：直接计算、立即调用、返回后局部调用、保存按值捕获的闭包、保存可变捕获的闭包、保存捕获大数组的闭包。使用 `for b.Loop()`、`b.ReportAllocs()` 和结果接收变量；保存到包级变量使生命周期成为实验变量。Java 先检查捕获参数的字节码，分配测量沿用 [Ch17](../ch17-profiling-bench/) 的 JMH 方法，避免手写计时循环。
- **工具**：`go test '-gcflags=-m=2' ./ch04-functions-closures/go/lab`；`go test '-run=^$' '-bench=.' -benchmem -count=10 ./ch04-functions-closures/go/lab`。PowerShell 要保留 `'-bench=.'` 的引号，避免参数被拆分。首次验证可加 `-benchtime=100ms`；正式耗时比较使用充分采样与 benchstat。
- **预期现象**：至少存在可消除环境或不需要堆分配的场景；具体数字取决于内联和逃逸结论，不预设“返回闭包必定分配”。
- **记录**：捕获策略、逃逸诊断、allocs/op、B/op 与重复运行统计。
- **边界**：`Direct` / `Immediate` 的计算很小且可能被常量折叠，ns/op 可能主要反映循环成本；不要用它们推算业务吞吐或声称闭包快了多少倍。返回闭包的函数体显示 `escapes to heap`，不代表内联到具体调用点后仍然分配，要同时看调用点诊断。

### 实验 3：初始化依赖与失败

- **目的**：比较包初始化和类初始化的触发与顺序。
- **方法**：运行 [`go/initorder`](go/initorder/) 的主包和依赖包，比较变量声明顺序、依赖顺序与 `init` 日志；运行 [`InitializationDemo.java`](java/InitializationDemo.java)，依次读取编译期常量、普通静态字段、创建实例，再制造两次类初始化失败。JEP 513 合法例子也在这个 demo 内。
- **工具**：`GODEBUG=inittrace=1`（PowerShell 用 `$env:GODEBUG='inittrace=1'`，实验后移除）；Java `-Xlog:class+init=info`、`javap` 和 `javac` 诊断。
- **预期现象**：Go 依赖包先初始化；Java 常量读取不会触发声明类初始化；非法 early construction 访问编译失败。
- **记录**：日志次序、触发点、首次与后续初始化失败的异常类型，并区分自己程序的日志与标准库日志。

独立编译下面两个非法示例，**预期退出码非 0**。文件分别验证 effectively final 与 early construction 限制，不参与正常示例编译：

```text
javac --release 25 -d out ch04-functions-closures/java/compile-fail/InvalidCapture.java
javac --release 25 -d out ch04-functions-closures/java/compile-fail/InvalidConstruction.java
```

不要只记住某一语言的错误消息；记录诊断位置和违反的规则，诊断文本会随 JDK 语言环境而变化。

## 🧪 练习

**Go：[`go/exercises`](go/exercises/)**（初始失败）

1. `MakeCounters(n int) []func() int`：每个计数器持有独立状态；交错调用验证状态没有串用。本练习只要求单线程语义。
2. `CaptureMethods`：保存值接收者和指针接收者的方法值，验证原值快照与后续修改；结合 demo 解释拷贝结构体时哪些字段仍引用共享数据。
3. `WithLimit` / `WithName` / `Configure`：函数式选项按调用者给定的顺序应用，后者覆盖前者，忽略 nil 选项；每次从零值配置开始，选项可以复用。
4. `LoadPositive`：修复 `:=` 遮蔽导致错误丢失的函数，确保回调只执行一次；失败时返回 `(0, 原错误)`，不吞错、不替换错误。

**Java：[`java/drills`](java/drills/)**（初始失败）

1. `CaptureDrill.java`：实现互不干扰的计数器，预测对象修改与接收者替换后的结果；局部变量重新赋值的限制由独立编译失败示例验证。
2. `InitializationDrill.java`：填写常量读取、静态初始化和 JEP 513 构造顺序的预测，程序运行实际实验并自校验。

## 🧭 设计取舍

- **可变捕获与限制捕获**：可变捕获方便表达局部状态机，但并发共享需要同步；effectively final 限制减少了局部变量共享歧义，无法阻止所引用对象的竞争。
- **语义与优化分离**：规范约束可观察行为，编译器选择表示与分配位置。不要为追求某次测试的零分配牺牲语义正确性。
- **初始化的隐式成本**：Go 包导入带来前置初始化，Java 主动使用可能触发延迟初始化与锁等待。比较启动行为时必须记录测量边界。
- **构造期间的安全边界**：JEP 513 允许尽早校验输入，同时限制未构造完成实例的使用；与 Go 显式返回值和错误的初始化函数比较各自的失败路径。

## ✅ 验收答辩题

- [ ] [L2] Go 闭包语义上的捕获变量与编译器的按值捕获为什么不矛盾？
- [ ] [L2] Java lambda 不能重新赋值捕获的局部引用，为什么仍然可能发生数据竞争？
- [ ] [L2] `x.M` 与 `T.M` 在接收者求值时机、函数签名和拷贝行为上有什么区别？
- [ ] [L3] 给出 `range` 的 `:=` 与 `=` 两组输出、对应作用域解释和测试证据。
- [ ] [L3] 用实验数据说明一个闭包何时无需堆分配，并指出内联对结论的影响。
- [ ] [L3] 预测并解释 Go 包初始化日志与 Java 类初始化日志；说明常量读取的差异。
- [ ] [L3] 提供一个 JEP 513 合法例子和一个非法例子，解释编译器诊断对应的 early construction 限制。
- [ ] [L4] 比较 Go 包初始化与 Java 按需类初始化，在启动成本、错误暴露时机和并发可见性上的取舍。

## 🕳️ 常见坑

- 把“闭包捕获变量”理解成“所有捕获都一定是堆指针”。
- 认为 `range` 的新变量规则也会给已经存在的变量 `=` 创建新副本。
- 忽略值接收者里的 slice、map、指针字段仍可共享底层数据。
- 把 lambda 的对象身份、分配次数或生成类名当成规范保证。
- 用包级 `init` 隐藏需要显式错误处理的初始化；或认为 `init` 中启动的 goroutine 一定先于 `main` 完成。

## 📚 一手资料

- Go：[规范](https://go.dev/ref/spec) 的 Function literals、Method values、Method expressions、For statements、Package initialization；本机编译器源码；[循环变量设计文档](https://go.googlesource.com/proposal/+/master/design/60078-loopvar.md)。
- Java：[JLS §15.27](https://docs.oracle.com/javase/specs/jls/se25/html/jls-15.html#jls-15.27)、[JLS §12.4](https://docs.oracle.com/javase/specs/jls/se25/html/jls-12.html#jls-12.4)、[JEP 513](https://openjdk.org/jeps/513)、JDK 25 `java.lang.invoke` 源码。
