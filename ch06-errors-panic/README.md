# Ch06 · 错误、panic 与 defer 机制

> Part 1 · 估时 10h · 状态：⚪ 未开始 · 错误是值，异常是栈展开：两种失败模型的设计、实现和成本；再加一个游戏服必须守住的底线：一个连接或房间的 bug 不能拖垮整个进程

## 🎯 目标（资深标准）

- **L1**：设计和使用哨兵错误、自定义错误类型、`%w` 包装链；熟练使用 `errors.Is`、`errors.As`、`errors.AsType`、`errors.Join`；在 goroutine 顶层正确 recover；用 `log/slog` 打结构化日志。
- **L2**：讲清错误链的遍历算法（单链 + 多分支树）；讲清 `gopanic` → 执行 defer → `recover` → `recovery` 的运行时流程，以及 recover 只在"被 defer 直接调用的函数"中生效的原因；讲清 open-coded defer 的适用条件；讲清 Java 异常的成本构成（`fillInStackTrace`、栈深度、`OmitStackTraceInFastThrow`）。
- **L3**：用 benchmark 比较 Go 的错误返回、Go 的 panic/recover、Java 的异常（带栈 / 不带栈）在热路径上的成本；能根据 panic 输出和 traceback 定位线上崩溃。
- **L4**：为游戏服制定错误处理规范：错误分类（可重试 / 业务拒绝 / bug）、错误码映射给客户端、哪里 recover、哪里让进程崩溃、日志和监控如何打点。

## 🔗 前置

[Ch00](../ch00-setup/)、[Ch03](../ch03-interfaces/)

## 💡 关键洞察

1. **Go 的 error 只是一个接口值，传播靠显式 return。** 它没有栈展开，也没有栈快照，所以便宜、可预测，而且控制流在代码里一目了然。代价是需要你自己写清"在哪一层加上下文"。
2. **panic 不是异常。** 它表示程序进入了不该进入的状态。**一个 goroutine 里没被 recover 的 panic 会让整个进程退出**，而 Java 线程里未捕获的异常只会结束那个线程。所以游戏服的每个连接 goroutine、房间 goroutine 都必须在顶层兜底。
3. **有些错误根本 recover 不了**：并发写 map、栈溢出、内存耗尽、所有 goroutine 都阻塞（死锁）等都是 fatal error，进程会直接退出。线上能依赖的只有"别触发它们"和"崩溃后能快速定位"。

## ☕ Java 25 深度复习

### 异常的实现与成本
- **对象构造成本**：`Throwable` 构造时默认调用 `fillInStackTrace()`，遍历当前栈帧并记录下来，成本随栈深度增长（最多记录 `MaxJavaStackTraceDepth`，本机默认 1024 帧）。`getStackTrace()` 时才把内部表示转成 `StackTraceElement[]`。
- **无栈异常**：构造器 `Throwable(String message, Throwable cause, boolean enableSuppression, boolean writableStackTrace)` 传 `writableStackTrace = false` 可以跳过栈记录。高频业务异常（例如"金币不足"）常用这种方式，或者重写 `fillInStackTrace`。
- **`OmitStackTraceInFastThrow`**（本机默认 `true`）：C2 编译后的代码中，频繁抛出的隐式异常（NPE、数组越界、类型转换失败、除零等）会被替换为预分配的、没有栈信息的异常对象。线上日志里出现"没有栈的 NPE"就是这个原因，排查时可以加 `-XX:-OmitStackTraceInFastThrow`。
- **栈展开**：JIT 编译后的代码通过异常表找到处理器；同一编译单元内的 throw 和 catch 可能被优化成跳转，但这不能作为设计依据。
- **`StackWalker`**：按需、惰性遍历栈，比 `new Throwable().getStackTrace()` 更便宜，适合日志框架取调用位置。
- **checked / unchecked**：checked 异常让错误成为签名的一部分（类似 Go 的显式 error），但和 lambda / Stream 配合很糟糕。
- **try-with-resources 与抑制异常**：资源关闭时抛出的异常通过 `addSuppressed` 附加到主异常上，不会把它覆盖掉。Go 的 `defer` 中关闭资源出错时，需要自己决定怎么合并（`errors.Join`）。
- **未捕获异常**：只结束当前线程，交给 `Thread.UncaughtExceptionHandler`。虚拟线程也一样。结构化并发中子任务的异常如何传播见 [Ch10](../ch10-context-structured/)。

### 日志
- SLF4J + Logback / Log4j2 的参数化日志、MDC（基于 ThreadLocal，在虚拟线程下有成本问题，[Ch10](../ch10-context-structured/) 讨论 ScopedValue）、异步 appender。对照 Go 的 `log/slog`。

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
- **log/slog**：
  - `Handler` 接口有 4 个方法：`Enabled`、`Handle`、`WithAttrs`、`WithGroup`；`TextHandler` 和 `JSONHandler` 是内置实现。
  - `Logger.LogAttrs` 搭配 `slog.Int` 等强类型 Attr，可以避开 `...any` 的装箱分配（参考 [Ch03](../ch03-interfaces/)）。
  - `LogValuer` 用于延迟求值和脱敏；`LevelVar` 支持运行时调整日志级别。
  - `NewMultiHandler` 把同一条记录分发给多个 Handler，例如同时写本地文件和日志采集端。

### 源码阅读清单（`$GOROOT/src`）
| 文件 | 函数 / 类型 | 带着什么问题读 |
|---|---|---|
| `errors/wrap.go` · `errors/join.go` | `Is`、`As`、`AsType`、`joinError` | 遍历算法如何同时处理 `Unwrap() error` 和 `Unwrap() []error`？`AsType` 为什么比 `As` 快？ |
| `fmt/errors.go` | `Errorf`、`wrapError`、`wrapErrors` | 有多个 `%w` 时是怎么处理的？`%w` 传入非 error 类型的参数会发生什么？ |
| `runtime/runtime2.go` · `runtime/panic.go` | `_panic`、`_defer`、`gopanic`、`gorecover`、`recovery`、`deferreturn`、`fatalpanic` | recover 为什么只在被 defer 直接调用的函数中生效？运行时是怎样判断"直接调用"的？ |
| `cmd/compile/internal/walk/stmt.go` · `ssagen/ssa.go` | open-coded defer 的判定条件 | 为什么上限是 8 个？为什么"返回点 × defer 数"也有上限？ |
| `cmd/vendor/golang.org/x/tools/go/analysis/passes/printf/printf.go` | `%w` 的指针检查（1.27 新增） | 它为什么判定这种写法会让 `errors.Is` 失效？举一个真实会出 bug 的例子。 |
| `log/slog/handler.go` · `logger.go` · `multi_handler.go` | `Handler`、`LogAttrs`、`MultiHandler` | 一条日志从 `Info` 调用到写出字节，中间有几次分配？`Enabled` 为什么要最先检查？ |

## 📊 Java 25 ↔ Go 1.27 机制级对照表

| 维度 | Java 25 | Go 1.27 | 取舍 / 游戏服影响 |
|---|---|---|---|
| 传播方式 | 栈展开，自动传播 | 显式 return | Go 每一层都看得到错误的去向；Java 代码更短，但失败路径是隐式的 |
| 构造成本 | 默认记录栈（与栈深度成正比） | 一个小对象，或者一个零分配的哨兵 | 高频业务失败（扣费失败、条件不满足）在 Go 里几乎没有成本 |
| 上下文 | 异常链 `cause` + 栈 | `%w` 包装链，上下文靠人工添加（没有栈） | Go 的错误信息要靠规范保证可读；需要栈时自己记录或在 recover 时打印 |
| 类型匹配 | `catch (T e)` 沿继承层次匹配 | `errors.Is` / `As` / `AsType` 沿错误树匹配 | Go 的匹配是显式的、可自定义的；没有继承层次 |
| 未处理的后果 | 线程结束，进程继续 | **进程退出** | Go 的连接 / 房间 goroutine 必须在顶层 recover |
| 不可恢复错误 | `Error` 子类（OOM、StackOverflowError，仍可捕获） | fatal error，无法 recover | Go 里"并发写 map"这类 bug 一定会让进程退出，只能靠测试和 `-race` 预防 |
| 资源清理 | try-with-resources + suppressed | `defer` + `errors.Join` | Go 的 defer 可以放在任何位置，但关闭失败的错误需要自己合并 |
| 清理的成本 | finally 由编译器复制到各出口 | open-coded defer 接近零成本；循环中的 defer 需要堆分配 | 不要在热循环里写 defer |
| 结构化日志 | SLF4J + Logback + MDC | `log/slog` + `context` | Go 标准库自带；请求级上下文用 context 传递，不依赖 ThreadLocal |

## 🔬 实验

### 实验 1：失败路径的成本（必做）
- **目的**：量化"失败"在两种模型里的代价。
- **方法**：
  - Go（`go/lab/errcost`）：在 20 层调用深度上，分别测试①返回哨兵错误、②每层 `%w` 包装、③最底层 panic、顶层 recover。
  - Java（`java/ErrCostLab.java`）：在相同深度上测试①普通异常、②`writableStackTrace = false` 的异常、③返回结果对象（不抛异常）。
- **工具**：Go 用 `B.Loop` + `benchstat`；Java 充分预热后计时（JMH 留到 [Ch15](../ch15-profiling-bench/)）；另外跑一次 `-XX:-OmitStackTraceInFastThrow` 观察隐式异常的变化。
- **预期现象**：Go 哨兵错误最便宜；每层包装会线性增加分配；panic/recover 比返回错误贵一到两个数量级。Java 普通异常的成本随栈深度增长，无栈异常接近返回结果对象。
- **记录**：ns/op 和 allocs/op 对照表，以及栈深度从 5 加到 50 时的变化曲线。

### 实验 2：defer 的三种实现（必做）
- **目的**：看到 open-coded defer 和堆上 defer 的差别。
- **方法**：分别写①一个 defer、②9 个 defer、③循环里的 defer 三个函数，做 benchmark。
- **工具**：`go build -gcflags='-d=defer'` 打印每个 defer 被编译成哪种形式（以本机实际输出为准）；`go tool objdump` 查看是否调用了 `runtime.deferproc` 或 `deferprocStack`。
- **预期现象**：①不调用 `deferproc*`；②退化为栈上记录；③使用堆上记录，并产生分配。

### 实验 3：崩溃演练
- **目的**：熟悉线上崩溃的各种现场。
- **方法**：写一个 TCP echo 服务，每个连接一个 goroutine，分别注入：①连接 handler 里的 nil 指针 panic（有顶层 recover / 没有顶层 recover）；②并发写 map；③无限递归。
- **工具**：观察进程是否存活和标准错误输出；设置 `GOTRACEBACK=all`、`GOTRACEBACK=crash` 对比输出差异。
- **预期现象**：有 recover 时只断开出问题的连接；没有 recover 时整个进程退出；②和③即使有 recover，进程也会退出。
- **记录**：每种情况的输出摘要，以及从输出中定位到出错代码行的方法。

## 🧪 练习规划

**Go：`go/exercises`**（`go test -race ./ch06-errors-panic/go/...`）
1. 设计 `var ErrNotEnoughGold = errors.New(...)` 和 `type QuotaError struct{ Limit, Used int }`，实现 `func Purchase(p *Player, item ItemID) error`，每层用 `%w` 添加上下文。测试用 `errors.Is` 和 `errors.AsType[*QuotaError]` 判断，并检查最终的错误字符串。
2. `func SafeGo(name string, fn func(), onPanic func(name string, v any, stack []byte))`：带 recover 的 goroutine 启动器，记录 panic 值和 `debug.Stack()`。测试包括：普通 panic 被捕获、`runtime.Goexit` 不会触发 onPanic、panic 值是 error 时能被 `errors.Is` 识别。
3. `func HandleConn(conn net.Conn, h Handler) (err error)`：在 defer 中把 panic 转成 error（通过具名返回值），同时用 `errors.Join` 合并关闭连接时的错误。测试用 `net.Pipe` 构造连接，覆盖正常返回、handler 返回错误、handler panic 三种情况。
4. `func ToClientCode(err error) (code int32, msg string)`：把内部错误树映射成给客户端的错误码。未知错误不能泄露内部信息；`errors.Join` 产生的复合错误取优先级最高的那个错误码。
5. `type redactedToken string`，实现 `slog.LogValuer` 做脱敏；再用 `slog.NewMultiHandler` 同时输出到两个 Handler。测试断言输出里不包含原始 token，并用 `AllocsPerRun` 比较 `LogAttrs` 和 `Info(msg, args...)` 的分配次数。

**Java：`java/drills/ErrorsDrill.java`**
- 实现一个无栈的业务异常 `GameException`（`writableStackTrace = false`），自校验 `getStackTrace().length == 0`。
- 用 try-with-resources 让两个资源在关闭时都抛异常，自校验主异常上 `getSuppressed()` 的数量和顺序；在注释里写出 Go 中用 `defer` + `errors.Join` 的等价写法。
- 用 `Thread.ofVirtual()` 启动一个会抛异常的任务，自校验 `UncaughtExceptionHandler` 被调用、主程序继续运行，并与 Go 中进程退出的行为对比。

## 🎮 游戏服落地

- **每个长生命周期的 goroutine 都要兜底**：连接读循环、写循环、房间 actor、定时任务，统一通过 `SafeGo` 启动。recover 之后要记录日志和指标，**并且只清理这一个单元**（踢掉这个连接、关闭这个房间并通知玩家），而不是若无其事地继续运行，因为此时状态可能已经损坏。
- **错误分类与客户端错误码**：业务拒绝（金币不足、冷却中）用哨兵错误或错误类型，映射为客户端错误码，不打 error 级别日志；依赖故障（数据库超时）可重试，打 warn 并上报指标；bug（不变量被破坏）直接 panic，由兜底层记录栈。Java 侧通常用异常层次加全局异常处理器来完成同样的分流。
- **不要用 panic 做业务控制流**：Java 习惯用"抛异常一路跳出"来简化代码，在 Go 里这既慢又危险（漏掉一处 recover 就是整个进程崩溃）。
- **结构化日志**：玩家 ID、房间 ID、请求 ID 作为 Attr，通过 `context` 传递，在 Handler 中统一注入。热路径（每帧、每个消息）只在 debug 级别打日志，并且先检查 `Enabled`。

## ✅ 验收答辩题

- [ ] [L2] `errors.Is` 遇到 `errors.Join(a, fmt.Errorf("x: %w, %w", b, c))` 时的遍历顺序是什么？
- [ ] [L2] 为什么在 `defer func() { helper() }()` 里，`helper` 内部调用的 `recover()` 拿不到 panic？
- [ ] [L2] 描述从 `panic(v)` 到 `recover()` 之后函数正常返回的完整运行时流程，具名返回值在其中起什么作用？
- [ ] [L2] open-coded defer 的适用条件有哪些？为什么循环中的 defer 不能被 open-coded？
- [ ] [L2] Java 异常的成本主要来自哪里？`OmitStackTraceInFastThrow` 对线上排障有什么影响？
- [ ] [L2] 1.27 的 vet 为什么要报告 `%w` 传入 `*E`（E 已实现 error）的写法？给出一个因此让 `errors.Is` 失效的例子。
- [ ] [L3] 拿出实验 1 的数据：在 20 层调用深度下，Go 的错误返回、Go 的 panic/recover、Java 的普通异常和无栈异常各自的成本是多少？
- [ ] [L3] 拿出实验 3 的输出：线上进程退出，标准错误里有一段 `fatal error: concurrent map writes`，你怎么从输出定位到代码，又怎么防止它再次发生？
- [ ] [L3] 某服务日志里每秒出现数千条"没有栈的 NPE"，你会怎么定位？
- [ ] [L4] 为一个包含网关、逻辑服、匹配服的游戏后端写一页错误处理规范：错误分类、错误码、recover 位置、日志级别、监控指标，并说明 Java 25 版本的对应做法有什么不同。

## 🕳️ 常见坑

- **用 panic 当异常**：业务失败也 panic，指望上层 recover，结果漏掉一处就让整个进程退出。
- **goroutine 没有顶层 recover**：Java 习惯是"未捕获异常只结束这个线程"，在 Go 里是整个进程退出。
- **以为 recover 能救一切**：并发写 map、死锁、栈溢出都是 fatal error。
- **吞掉错误**：`_ = f()` 或者只打日志不返回。对应 Java 中 `catch (Exception e) {}` 的坏习惯。
- **每一层都打日志再返回**：同一个错误在日志里出现 N 次。规范应该是"要么处理并记录，要么包装后返回"，二选一。
- **返回具体类型的 nil 指针作为 error**（见 [Ch03](../ch03-interfaces/)）。
- **在循环里 defer 关闭资源**：资源要到整个函数返回时才释放，而且每次 defer 都要堆分配；应该把循环体抽成函数。
- **用字符串比较判断错误**（`err.Error() == "..."`），一旦上游改了措辞就失效；应该用 `Is` / `As`。

## 📚 一手资料

- Go spec：[Handling panics](https://go.dev/ref/spec#Handling_panics)、[Defer statements](https://go.dev/ref/spec#Defer_statements)、[Run-time panics](https://go.dev/ref/spec#Run_time_panics)
- 官方博客：Working with Errors in Go 1.13、Error handling and Go、Defer, Panic, and Recover、Structured Logging with slog
- Go 1.27 Release Notes「Vet」一节（`printf` 对 `%w` 的检查）
- 源码：`errors/`、`fmt/errors.go`、`runtime/panic.go`、`cmd/compile/internal/walk/stmt.go`、`log/slog/`
- 设计文档：Open-coded defers（go.googlesource.com/proposal 的 design 目录）
- Java：`java/lang/Throwable.java`、`StackWalker.java`（src.zip）；JLS §14.20.3（try-with-resources）；《Effective Java》第 10 章（异常）
