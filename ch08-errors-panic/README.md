# Ch08 · 错误、panic 与 defer 机制

> Part 1 · 估时 10h · 状态：⚪ 未开始 · 错误是值，异常是栈展开：两种失败模型的规范、实现和成本；以及 Go 的一条硬规则：任何一个 goroutine 里未恢复的 panic 都会终止整个程序

## 🎯 目标（资深标准）

- **L1**：设计和使用哨兵错误、自定义错误类型、`%w` 包装链；熟练使用 `errors.Is`、`errors.As`、`errors.AsType`、`errors.Join`；在 goroutine 顶层正确 recover；能读懂 panic 和 fatal error 的 traceback。
- **L2**：讲清错误链的遍历算法（单链 + 多分支树）；讲清 `gopanic` → 执行 defer → `recover` → `recovery` 的运行时流程，以及 recover 只在"被 defer 直接调用的函数"中生效的原因；讲清 open-coded defer 的适用条件；讲清 Java 异常的成本构成（`fillInStackTrace`、栈深度、`OmitStackTraceInFastThrow`）。
- **L3**：先预测再验证：给定调用深度，预测错误返回、panic/recover、Java 异常（带栈 / 不带栈）的相对成本；预测某个函数里的每个 defer 会被编译成哪种实现（open-coded / 栈上 / 堆上），用 `-d=defer` 和 objdump 验证；预测一个程序在不同 `GOTRACEBACK` 级别下崩溃时会打印哪些 goroutine。
- **L4**：能讲清"错误即值"与"受检 / 非受检异常"两种失败模型的设计动机与代价；为什么 Go 规定未恢复的 panic 终止整个程序，而 Java 只结束当前线程；为什么有些错误（fatal error）被设计成根本不能 recover。

## 🔗 前置

[Ch00](../ch00-setup/)、[Ch05](../ch05-interfaces/)

## 💡 关键洞察

1. **Go 的 error 只是一个接口值，传播靠显式 return。** 它没有栈展开，也没有栈快照，所以便宜、可预测，而且控制流在代码里一目了然。代价是需要你自己写清"在哪一层加上下文"。
2. **panic 不是异常。** 它表示程序进入了不该进入的状态。**一个 goroutine 里没被 recover 的 panic 会让整个进程退出**，而 Java 线程里未捕获的异常只会结束那个线程。这是规范层面的规定（spec「Handling panics」），不是运行时的可配置策略：panic 一路展开到 goroutine 的顶层，程序就终止，在别的 goroutine 里 recover 救不了它。
3. **有些错误根本 recover 不了**：并发写 map、栈溢出、内存耗尽、所有 goroutine 都阻塞（死锁）等都是 fatal error，进程会直接退出。能依赖的只有"别触发它们"（测试、`-race`）和"读懂它们的输出"。

## ☕ Java 25 深度复习

### 异常的实现与成本
- **对象构造成本**：`Throwable` 构造时默认调用 `fillInStackTrace()`，遍历当前栈帧并记录下来，成本随栈深度增长（最多记录 `MaxJavaStackTraceDepth`，本机默认 1024 帧）。`getStackTrace()` 时才把内部表示转成 `StackTraceElement[]`。
- **无栈异常**：构造器 `Throwable(String message, Throwable cause, boolean enableSuppression, boolean writableStackTrace)` 传 `writableStackTrace = false` 可以跳过栈记录。高频业务异常（例如"金币不足"）常用这种方式，或者重写 `fillInStackTrace`。
- **`OmitStackTraceInFastThrow`**（本机默认 `true`）：C2 编译后的代码中，频繁抛出的隐式异常（NPE、数组越界、类型转换失败、除零等）会被替换为预分配的、没有栈信息的异常对象。线上日志里出现"没有栈的 NPE"就是这个原因，排查时可以加 `-XX:-OmitStackTraceInFastThrow`。
- **栈展开**：JIT 编译后的代码通过异常表找到处理器；同一编译单元内的 throw 和 catch 可能被优化成跳转，但这不能作为设计依据。
- **`StackWalker`**：按需、惰性遍历栈，比 `new Throwable().getStackTrace()` 更便宜，适合日志框架取调用位置。
- **checked / unchecked**：checked 异常让错误成为签名的一部分（类似 Go 的显式 error），但和 lambda / Stream 配合很糟糕。
- **try-with-resources 与抑制异常**：资源关闭时抛出的异常通过 `addSuppressed` 附加到主异常上，不会把它覆盖掉。Go 的 `defer` 中关闭资源出错时，需要自己决定怎么合并（`errors.Join`）。
- **未捕获异常**：只结束当前线程，交给 `Thread.UncaughtExceptionHandler`。虚拟线程也一样。结构化并发中子任务的异常如何传播见 [Ch13](../ch13-context-structured/)。

### 未捕获异常与线程终止
- 未捕获的异常只结束**当前线程**：依次交给线程自己的 `UncaughtExceptionHandler`、所在 `ThreadGroup`、`Thread.setDefaultUncaughtExceptionHandler` 设置的默认处理器，最后打印栈。
- JVM 在"存活的非 daemon 线程数降到 0"（或调用 `System.exit`）时退出。**虚拟线程都是 daemon 线程**（`Thread.java` 文档注释明确写了），不会阻止 JVM 退出。
- 对照 Go：goroutine 没有处理器链，也没有 daemon 的概念；`main` 函数返回时程序直接退出，不等其他 goroutine。

## 🐹 Go 1.27 深入

### 规范层
- **error 接口**：`type error interface{ Error() string }`，没有任何特殊语义。
- **panic / recover**（spec「Handling panics」）：`panic(v)` 开始执行当前 goroutine 的 defer 链；只有**被 defer 的函数直接调用**的 `recover()` 才能停止 panic 并返回 panic 的值，其他地方调用都返回 nil。
- **defer**：参数在 defer 语句执行时求值；函数返回前按 LIFO 执行；deferred 函数可以通过**具名返回值**修改函数的返回结果。这是"在 defer 里把 panic 转成 error"的基础。
- **`runtime.Goexit`**：执行完所有 defer 后终止当前 goroutine，不是 panic，recover 拦不住它（`t.FailNow` 就是用它实现的）。

### 标准库层：错误链
- **包装**：`fmt.Errorf("load player %d: %w", id, err)` 产生 `*fmt.wrapError`（实现 `Unwrap() error`）；使用多个 `%w` 时产生 `*fmt.wrapErrors`（实现 `Unwrap() []error`）。
- **合并**：`errors.Join(errs...)` 产生实现 `Unwrap() []error` 的 `joinError`。
- **遍历**：`errors.Is` 和 `errors.As` 按**深度优先**遍历整棵错误树；类型可以自定义 `Is(error) bool` / `As(any) bool` 方法改变匹配规则。
- **`errors.AsType[E error](err) (E, bool)`**：泛型版 As，类型安全，不需要预先声明目标变量，也比 `errors.As` 更快。
- **1.27 新增 vet 检查**：`printf` 分析器会报告 `fmt.Errorf("...: %w", p)` 这样的写法，其中 `p` 的类型是 `*E`，而 `E` 本身已经实现了 error，报错信息是 "defeats errors.Is"。这种写法让链上存的是指针，`errors.Is` 和类型断言都会出现意外的不匹配。
- **错误设计的三种风格**：
  - **哨兵错误**：`var ErrNotFound = errors.New(...)`，调用方用 `Is` 判断。
  - **错误类型**：`type QuotaError struct{...}`，调用方用 `As` / `AsType` 取出字段。
  - **不透明错误**：只暴露行为接口，例如 `interface{ Temporary() bool }`。
  对外 API 应尽量少暴露哨兵和类型，因为它们一旦暴露就成了兼容性承诺。

### 运行时与编译器层
- **panic 流程**（`runtime/panic.go`）：`gopanic` 创建 `_panic` 记录（`runtime2.go`），依次执行 defer。某个 defer 中调用 `gorecover` 后，标记已恢复；该 defer 返回后，由 `recovery` 把 goroutine 的执行恢复到"defer 所在函数正常返回"的位置。如果没有任何 recover，就调用 `preprintpanics` 和 `fatalpanic` 打印 panic 值和栈，然后退出进程。recover 后再次 panic，输出会标注 `[recovered, repanicked]`。
- **defer 的三种实现**：
  1. **open-coded defer**：直接在函数的每个返回点内联生成 defer 调用，用一个字节的位图记录哪些 defer 已经激活，几乎零成本。适用条件（`cmd/compile/internal/walk/stmt.go`、`ssagen/ssa.go` 中已核实）：
     - 函数内 defer 不超过 8 个；
     - defer 不在循环中；
     - 返回语句数 × defer 数 ≤ 15；
     - 结果参数没有分配在堆上；
     - 不在 range-over-func 的循环体中；
     - 未用 `-N` 禁用优化。
  2. **栈上 defer 记录**：`deferprocStack`。
  3. **堆上 defer 记录**：`deferproc`，用于循环中的 defer 等情况。
  panic 时，运行时通过 funcdata 找到 open-coded defer 并执行它们。
- **fatal error 和 panic 的区别**：`fatal(...)` / `throw(...)` 直接终止进程，不执行 defer，也不能 recover。典型情况有 `concurrent map writes`、`all goroutines are asleep - deadlock!`、栈超过上限、内存耗尽。
- **崩溃输出与 `GOTRACEBACK`**（`runtime/runtime1.go` 的 `setTraceback`）：`none` 不打印 goroutine 栈；`single`（默认）只打印出错的 goroutine；`all` 打印所有用户 goroutine；`system` 额外包含运行时内部的帧和 goroutine；`crash` 在 `system` 的基础上 abort（Linux 上可产生 core dump）；Windows 上另有 `wer`（交给 Windows 错误报告）。程序内可以用 `debug.SetTraceback` 调高级别，`debug.SetCrashOutput` 把致命错误的输出额外写到一个文件。
- **1.27 新增：traceback 带 goroutine 标签**。主模块声明 `go 1.27` 及以上时，traceback 的 goroutine 头行末尾会以 `{key: value, ...}` 的形式打印这个 goroutine 的 pprof 标签（`pprof.Do`、`pprof.SetGoroutineLabels` 设置的那些），实现在 `runtime/traceback.go` 的 `goroutineheader`。它由 GODEBUG `tracebacklabels` 控制，`internal/godebugs/table.go` 里这一项是 `Changed: 27, Old: "0"`，所以 `go` 行更老的模块默认不打印（这就是 [Ch00](../ch00-setup/) 实验 1 里看到的 `DefaultGODEBUG=tracebacklabels=0`）。

### 源码阅读清单（`$GOROOT/src`）
| 文件 | 函数 / 类型 | 带着什么问题读 |
|---|---|---|
| `errors/wrap.go` · `errors/join.go` | `Is`、`As`、`AsType`、`joinError` | 遍历算法如何同时处理 `Unwrap() error` 和 `Unwrap() []error`？`AsType` 为什么比 `As` 快？ |
| `fmt/errors.go` | `Errorf`、`wrapError`、`wrapErrors` | 有多个 `%w` 时是怎么处理的？`%w` 传入非 error 类型的参数会发生什么？ |
| `runtime/runtime2.go` · `runtime/panic.go` | `_panic`、`_defer`、`gopanic`、`gorecover`、`recovery`、`deferreturn`、`fatalpanic` | recover 为什么只在被 defer 直接调用的函数中生效？运行时是怎样判断"直接调用"的？ |
| `cmd/compile/internal/walk/stmt.go` · `ssagen/ssa.go` | open-coded defer 的判定条件 | 为什么上限是 8 个？为什么"返回点 × defer 数"也有上限？ |
| `cmd/vendor/golang.org/x/tools/go/analysis/passes/printf/printf.go` | `%w` 的指针检查（1.27 新增） | 它为什么判定这种写法会让 `errors.Is` 失效？举一个真实会出 bug 的例子。 |
| `runtime/runtime1.go` · `runtime/traceback.go` | `setTraceback`、`goroutineheader` | `system` 和 `crash` 级别多打印了什么？goroutine 标签是在什么条件下被打印的？ |
| `runtime/debug/stack.go` | `SetCrashOutput`、`CrashOptions` | fatal error 发生时运行时处于什么状态？为什么崩溃输出只能写到一个事先打开的文件描述符？ |

## 📊 Java 25 ↔ Go 1.27 机制级对照表

| 维度 | Java 25 | Go 1.27 | 取舍 / 设计动机 |
|---|---|---|---|
| 传播方式 | 栈展开，自动传播 | 显式 return | Go 让失败路径出现在控制流图里；Java 让正常路径更短，失败路径隐式 |
| 构造成本 | 默认记录栈（与栈深度成正比） | 一个小对象，或者一个零分配的哨兵 | 错误是普通值，所以"失败"可以很便宜；Java 的栈快照是为调试付的预付款 |
| 上下文 | 异常链 `cause` + 栈 | `%w` 包装链，上下文靠人工添加（没有栈） | Go 用"人写的上下文"代替"机器记录的栈"，可读性取决于纪律 |
| 类型匹配 | `catch (T e)` 沿继承层次匹配 | `errors.Is` / `As` / `AsType` 沿错误树匹配 | Go 没有继承层次，匹配被做成库函数，`Is`/`As` 方法可以自定义 |
| 签名中的失败 | 受检异常写进 `throws` | `error` 作为最后一个返回值 | 两边都把"可能失败"写进签名；受检异常与 lambda / Stream 不兼容，Go 的 error 只是普通返回值 |
| 未处理的后果 | 线程结束，进程继续 | **程序终止**（spec 规定） | goroutine 没有身份、没有处理器链，panic 无处上报；带着可能损坏的状态继续运行更危险 |
| 不可恢复错误 | `Error` 子类（OOM、StackOverflowError，仍可捕获） | fatal error，无法 recover | Go 认为此时运行时状态已不可信，不给 defer 执行的机会；Java 允许捕获，但状态是否可信要自己判断 |
| 资源清理 | try-with-resources + suppressed | `defer` + `errors.Join` | `defer` 是语句，可以出现在任何位置；关闭时的错误需要自己合并 |
| 清理的实现 | javac 把 finally 块复制到每个出口 | open-coded defer 在每个返回点内联；不满足条件时退化为栈上 / 堆上记录 | 同一个思路：把清理代码复制到出口，以免运行时维护链表 |
| 崩溃输出 | 未捕获异常打印栈；VM 崩溃写 hs_err 日志 | traceback（`GOTRACEBACK` 控制范围，1.27 起带 goroutine 标签） | Go 把"哪些 goroutine 要打印"交给环境变量决定，标签让匿名的 goroutine 有了可识别的名字 |

## 🔬 实验

### 实验 1：失败路径的成本（必做）
- **目的**：量化"失败"在两种模型里的代价。
- **方法**：
  - Go（`go/lab/errcost`）：在 20 层调用深度上，分别测试①返回哨兵错误、②每层 `%w` 包装、③最底层 panic、顶层 recover。
  - Java（`java/ErrCostLab.java`）：在相同深度上测试①普通异常、②`writableStackTrace = false` 的异常、③返回结果对象（不抛异常）。
- **工具**：Go 用 `B.Loop` + `benchstat`；Java 充分预热后计时（JMH 留到 [Ch17](../ch17-profiling-bench/)）；另外跑一次 `-XX:-OmitStackTraceInFastThrow` 观察隐式异常的变化。
- **预期现象**：Go 哨兵错误最便宜；每层包装会线性增加分配；panic/recover 比返回错误贵一到两个数量级。Java 普通异常的成本随栈深度增长，无栈异常接近返回结果对象。
- **记录**：ns/op 和 allocs/op 对照表，以及栈深度从 5 加到 50 时的变化曲线。

### 实验 2：defer 的三种实现（必做）
- **目的**：看到 open-coded defer 和堆上 defer 的差别。
- **方法**：分别写①一个 defer、②9 个 defer、③循环里的 defer 三个函数，做 benchmark。
- **工具**：`go build -gcflags='-d=defer'` 打印每个 defer 被编译成哪种形式（以本机实际输出为准）；`go tool objdump` 查看是否调用了 `runtime.deferproc` 或 `deferprocStack`。
- **预期现象**：①不调用 `deferproc*`；②退化为栈上记录；③使用堆上记录，并产生分配。

### 实验 3：崩溃语义验证
- **目的**：亲眼确认规范里"未恢复的 panic 终止程序"和"fatal error 不能 recover"这两条规则，并熟悉 traceback 的格式。
- **方法**：一个 `main` 启动若干 goroutine，分别注入：①子 goroutine 里的 nil 指针 panic，recover 分别写在**子 goroutine 自己**和 **main** 里；②并发写 map；③无限递归；④`runtime.Goexit()` 在 main goroutine 里调用。再给出错的 goroutine 用 `pprof.Do` 打上标签，分别在 `go 1.26.0` 和 `go 1.27.0` 的模块下运行，对比 traceback 头行。
- **工具**：观察进程退出码和标准错误输出；`GOTRACEBACK=none / single / all / system` 逐一对比。
- **预期现象（先写下预测）**：①只有 recover 写在子 goroutine 里才有效，写在 main 里程序照样终止；②③即使有 recover 也会终止，输出以 `fatal error:` 开头；④其他 goroutine 继续运行，全部结束后报告死锁；1.27 模块的 traceback 头行带 `{key: value}` 标签，1.26 模块不带。
- **记录**：每种情况的退出码和输出摘要，以及从输出中定位到出错代码行的方法。

## 🧪 练习规划

**Go：`go/exercises`**（`go test -race ./ch08-errors-panic/go/...`）
1. 设计哨兵错误 `var ErrNotEnough = errors.New(...)` 和错误类型 `type QuotaError struct{ Limit, Used int }`，实现一个三层调用链 `func Purchase(p *Player, item ItemID) error`，每层用 `%w` 添加上下文。测试用 `errors.Is` 和 `errors.AsType[*QuotaError]` 判断，并检查最终的错误字符串。
2. `func SafeGo(name string, fn func(), onPanic func(name string, v any, stack []byte))`：带 recover 的 goroutine 启动器，记录 panic 值和 `debug.Stack()`。测试包括：普通 panic 被捕获、`runtime.Goexit` 不会触发 onPanic、panic 值是 error 时能被 `errors.Is` 识别。
3. `func Run(fn func() error, closers ...io.Closer) (err error)`：在 defer 中把 panic 转成 error（通过具名返回值），同时用 `errors.Join` 合并所有 `Close` 的错误。测试覆盖正常返回、fn 返回错误、fn panic、`Close` 也失败四种情况。
4. recover 的生效位置：写几个函数，分别把 `recover()` 放在 defer 直接调用的函数里、defer 调用的函数再调用的辅助函数里、非 defer 的普通调用里、另一个 goroutine 里。测试断言哪些能恢复、哪些不能，并在注释里引用 spec「Handling panics」解释原因。
5. `func DeferOrder() (log []string)`：用测试断言下面这些行为：defer 的参数在 defer 语句执行时求值、多个 defer 按 LIFO 执行、defer 可以修改具名返回值、循环里每次迭代的 defer 捕获的是当次的循环变量。先写下预测，再让测试转绿。

**Java：`java/drills/ErrorsDrill.java`**
- 实现一个无栈异常 `RejectException`（`writableStackTrace = false`），自校验 `getStackTrace().length == 0`。
- 用 try-with-resources 让两个资源在关闭时都抛异常，自校验主异常上 `getSuppressed()` 的数量和顺序；在注释里写出 Go 中用 `defer` + `errors.Join` 的等价写法。
- 用 `Thread.ofVirtual()` 启动一个会抛异常的任务，自校验 `UncaughtExceptionHandler` 被调用、主程序继续运行，并与 Go 中进程退出的行为对比。

## 🧭 设计取舍

- **错误即值 vs 异常。** Go 让每一处失败都显式出现在控制流里，代价是 `if err != nil` 的冗长；Java 的异常让正常路径简洁，失败路径则是隐式的。受检异常试图把失败写进签名，但和 lambda、Stream 冲突（`Function` 不能抛受检异常），最后大量代码退回非受检异常。Go 的 error 只是最后一个返回值，和函数值、泛型没有冲突。Go 团队讨论过多轮错误处理的语法糖（`try`、`check/handle` 等），最终在官方博客《[ On | No ] syntactic support for error handling》中宣布不再推进语法改动。
- **为什么未恢复的 panic 终止整个程序。** goroutine 没有父子关系、没有句柄、没有异常处理器链，panic 展开到顶层之后无处上报。如果只结束这个 goroutine，程序会带着可能已经被破坏的共享状态继续运行。Java 选择只结束线程，但同样的风险存在：一个持有锁、改到一半的线程死掉之后，其他线程看到的可能是不一致的状态。
- **为什么 fatal error 不能 recover。** 并发写 map 时，运行时的数据结构本身可能已经损坏；死锁时没有任何 goroutine 能运行；栈溢出时没有栈空间执行 defer。在这些状态下执行用户代码不安全，所以运行时直接 `throw`，不执行 defer。Java 允许捕获 `StackOverflowError` 和 `OutOfMemoryError`，但捕获之后程序处于什么状态，JLS 不做保证。
- **为什么 recover 只在被 defer 直接调用的函数里生效。** 这让"哪里会吞掉 panic"在静态上可以判断：只有 defer 语句直接调用的函数才能拦截，库代码里任意位置的 `recover()` 都不起作用。
- **为什么 defer 要做 open-coded。** defer 最初是运行时链表，成本高到人们会在热路径上避免使用它。open-coded defer 把 defer 调用直接内联到每个返回点，用位图记录哪些 defer 已激活，常见情况几乎零成本；代价是 panic 时要借助 funcdata 找到这些内联的 defer，panic 路径更复杂。javac 处理 finally 用的是同一个思路：把 finally 块复制到每个出口。

## ✅ 验收答辩题

- [ ] [L2] `errors.Is` 遇到 `errors.Join(a, fmt.Errorf("x: %w, %w", b, c))` 时的遍历顺序是什么？
- [ ] [L2] 为什么在 `defer func() { helper() }()` 里，`helper` 内部调用的 `recover()` 拿不到 panic？
- [ ] [L2] 描述从 `panic(v)` 到 `recover()` 之后函数正常返回的完整运行时流程，具名返回值在其中起什么作用？
- [ ] [L2] open-coded defer 的适用条件有哪些？为什么循环中的 defer 不能被 open-coded？
- [ ] [L2] Java 异常的成本主要来自哪里？`OmitStackTraceInFastThrow` 改变了哪种异常的可观察行为（栈长度、对象身份）？
- [ ] [L2] 1.27 的 vet 为什么要报告 `%w` 传入 `*E`（E 已实现 error）的写法？给出一个因此让 `errors.Is` 失效的例子。
- [ ] [L3] 拿出实验 1 的数据：在 20 层调用深度下，Go 的错误返回、Go 的 panic/recover、Java 的普通异常和无栈异常各自的成本是多少？
- [ ] [L3] 先预测实验 3 每种注入下进程是否存活、退出码是什么、traceback 会包含哪些 goroutine，再运行验证；说明如何从 `fatal error: concurrent map writes` 的输出定位到代码。
- [ ] [L3] 先预测：一个热循环里反复触发 NPE，C2 编译之后捕获到的异常 `getStackTrace().length` 是多少？用 `-XX:-OmitStackTraceInFastThrow` 对比验证，并解释 HotSpot 为什么要做这个优化。
- [ ] [L4] Go 规定未恢复的 panic 终止整个程序，Java 只结束当前线程。从"共享状态可能已损坏"的角度论证两种设计各自的风险，并说明 goroutine 没有身份这一点如何决定了 Go 的选择。
- [ ] [L4] 读《[ On | No ] syntactic support for error handling》：Go 团队为什么最终放弃了错误处理的语法糖？对照 Java 受检异常在 lambda 时代遇到的问题，评价"失败写进签名"这一设计。

## 🕳️ 常见坑

- **用 panic 当异常**：业务失败也 panic，指望上层 recover，结果漏掉一处就让整个进程退出。
- **goroutine 没有顶层 recover**：Java 习惯是"未捕获异常只结束这个线程"，在 Go 里是整个进程退出。
- **以为 recover 能救一切**：并发写 map、死锁、栈溢出都是 fatal error。
- **吞掉错误**：`_ = f()` 或者只打日志不返回。对应 Java 中 `catch (Exception e) {}` 的坏习惯。
- **每一层都打日志再返回**：同一个错误在日志里出现 N 次。规范应该是"要么处理并记录，要么包装后返回"，二选一。
- **返回具体类型的 nil 指针作为 error**（见 [Ch05](../ch05-interfaces/)）。
- **在循环里 defer 关闭资源**：资源要到整个函数返回时才释放，而且每次 defer 都要堆分配；应该把循环体抽成函数。
- **用字符串比较判断错误**（`err.Error() == "..."`），一旦上游改了措辞就失效；应该用 `Is` / `As`。

## 📚 一手资料

- Go spec：[Handling panics](https://go.dev/ref/spec#Handling_panics)、[Defer statements](https://go.dev/ref/spec#Defer_statements)、[Run-time panics](https://go.dev/ref/spec#Run_time_panics)
- 官方博客：Working with Errors in Go 1.13、Error handling and Go、Defer, Panic, and Recover、[ On | No ] syntactic support for error handling
- Go 1.27 Release Notes「Vet」一节（`printf` 对 `%w` 的检查）和「Runtime」一节（traceback 中的 goroutine 标签）
- 源码：`errors/`、`fmt/errors.go`、`runtime/panic.go`、`cmd/compile/internal/walk/stmt.go`、`runtime/traceback.go`、`runtime/runtime1.go`
- 设计文档：Open-coded defers（go.googlesource.com/proposal 的 design 目录）
- Java：`java/lang/Throwable.java`、`StackWalker.java`（src.zip）；JLS §14.20.3（try-with-resources）；《Effective Java》第 10 章（异常）
