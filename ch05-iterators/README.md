# Ch05 · 迭代器与数据管线

> Part 1 · 估时 8h · 状态：⚪ 未开始 · range-over-func 把"推式"迭代做成了语言特性，iter.Pull 用运行时协程把它反转成"拉式"；对照 Java 的 Iterator / Stream / Gatherers，弄清两边惰性管线的执行模型和代价

## 🎯 目标（资深标准）

- **L1**：会写和用 `iter.Seq` / `iter.Seq2`，熟练使用 `slices`、`maps`、`strings` 的迭代器函数；能写出 Filter / Map / Take 等组合器。
- **L2**：讲清 range-over-func 的编译器改写（循环体 → `yield` 闭包，`break`/`return`/`defer` 如何处理）、`yield` 返回 false 的契约及违约时的运行时检查；讲清 `iter.Pull` 基于 `runtime/coro.go` 的协程切换；讲清 Java Stream 的 `AbstractPipeline` + `Sink` 链执行模型和短路机制。
- **L3**：用 benchmark 证明某个迭代器管线在内联后与手写循环同速，或者找到它没能内联、产生闭包分配的原因并修掉。
- **L4**：在游戏服数据处理（AOI 查询、战报统计、配置表遍历、排行榜分页）中判断什么时候用迭代器、什么时候用 slice、什么时候用 channel，并和 Java Stream / Gatherers 的做法比较。

## 🔗 前置

[Ch02](../ch02-slice-map-string/)、[Ch04](../ch04-generics/)

## 💡 关键洞察

1. **`iter.Seq` 本质是"把循环体当回调传给数据源"。** 数据源掌握控制流，循环体通过 `yield` 的返回值告诉它要不要继续。这和 Java Stream 的 `Sink.accept` + `cancellationRequested()` 是同一套推式模型。
2. **推式迭代的性能取决于内联。** 迭代器函数和循环体都被内联时，闭包消失，代码和手写 for 循环一样；否则每个元素都要多一次间接调用，闭包还可能逃逸。
3. **`iter.Pull` 不是 goroutine + channel**，而是运行时协程（`coroswitch`）：两段代码在同一个线程上交替运行，切换比 channel 便宜得多，但用完必须调用 `stop`，否则协程会泄漏。

## ☕ Java 25 深度复习

### 三代迭代抽象
- **Iterator（拉式）**：`hasNext/next`，调用方掌握控制流。实现复杂遍历（树、图）时，要手工维护状态机。
- **Spliterator**：`tryAdvance`（逐个推送）+ `forEachRemaining`（批量推送）+ `trySplit`（拆分以支持并行）+ `characteristics`（SIZED、ORDERED、DISTINCT 等，影响优化）。它是 Stream 的数据源抽象。
- **Stream 执行模型**：
  - 中间操作只构建 `AbstractPipeline` 链表（`ReferencePipeline` 等），终端操作触发时才把各阶段包装成 `Sink` 链（`begin` / `accept` / `end`），由 Spliterator 推送数据。
  - 短路操作（`findFirst`、`limit`、`anyMatch`）通过 `Sink.cancellationRequested()` 通知上游停止。这正是 Go `yield` 返回 false 的对应物。
  - 并行流把 Spliterator 拆分后交给公共 `ForkJoinPool`。游戏服里要警惕它和其他任务争用公共池。
- **Stream Gatherers（JEP 485，已定稿）**：`Gatherer` 由 initializer / integrator / combiner / finisher 组成，用来写自定义中间操作（有状态、可短路、一对多）。内置的有 `windowFixed`、`windowSliding`、`fold`、`scan`、`mapConcurrent`；其中 `mapConcurrent` 在内部用**虚拟线程**并发执行映射，并限制最大并发度（见 `Gatherers.java` 中的 `Thread.ofVirtual()`）。
- **代价来源**：lambda 捕获可能分配对象；装箱（`Stream<Integer>` vs `IntStream`）；管线对象本身的分配。JIT 能内联消除其中一部分，但效果依赖调用点的单态性（参考 [Ch03](../ch03-interfaces/)）。

## 🐹 Go 1.27 深入

### 规范层
- **range over function**（spec「For statements with range clause」）：`for x := range f` 中，f 的类型可以是 `func(yield func() bool)`、`func(yield func(V) bool)` 或 `func(yield func(K, V) bool)`。标准库为后两种定义了 `iter.Seq[V]` 和 `iter.Seq2[K, V]`。
- **契约**：循环体执行 `break`、`return` 或 `panic` 后，`yield` 返回 false，迭代器必须立即停止，不能再调用 `yield`。
- **每次迭代新变量**：`for` 语句的循环变量按迭代创建（模块 `go` 行 ≥ 1.22 时生效）。闭包捕获循环变量的经典 bug 在当前 Go 中已不存在，但读老代码时仍要认得。
- **range 整数**：`for i := range n`。
- **标准库迭代器函数**：
  - `slices`：`All`、`Values`、`Backward`、`Collect`、`Sorted`、`Chunk`、`AppendSeq`；
  - `maps`：`All`、`Keys`、`Values`、`Insert`、`Collect`；
  - `strings` / `bytes`：`Lines`、`SplitSeq`、`SplitAfterSeq`、`FieldsSeq`、`FieldsFuncSeq`（不再为结果分配 `[]string`）；
  - `reflect`：`Type.Fields`、`Type.Methods` 等。
- 标准库**没有** Filter / Map / Take 之类的组合器，要自己写，这正是本章的练习内容。

### 运行时与编译器层
- **编译器改写**（`cmd/compile/internal/rangefunc/rewrite.go`）：循环体被改写为一个闭包作为 `yield` 传给迭代器函数。
  - `continue` 变为闭包 `return true`；`break` 变为设置状态后 `return false`。
  - 外层函数的 `return` 和 `goto` 通过状态变量在迭代器返回后再执行。
  - 循环体中的 `defer` 必须挂在**外层函数**上，由运行时的 `deferrangefunc` 支持。
- **运行时检查**（`runtime/panic.go`）：迭代器违约时，`panicrangestate` 会报出几种错误，例如：
  - `range function continued iteration after function for loop body returned false`
  - `range function continued iteration after loop body panic`
  - `range function recovered a loop body panic and did not resume panicking`
- **iter.Pull**（`iter/iter.go` 的 `Pull` / `Pull2`）：通过 `runtime/coro.go` 的 `newcoro` 和 `coroswitch` 创建协程。`next()` 切换到迭代器协程运行到下一次 `yield`，再切回调用方；`stop()` 让迭代器的 `yield` 返回 false，使其结束。协程切换直接交接执行权，不经过调度器的运行队列。
- **性能**：迭代器函数和循环体闭包都被内联后，整个循环展平为普通循环；任一方没能内联，就会产生间接调用，闭包也可能逃逸到堆上。用 `-gcflags=-m` 检查 `can inline`、`func literal escapes`。

### 源码阅读清单（`$GOROOT/src`）
| 文件 | 函数 / 类型 | 带着什么问题读 |
|---|---|---|
| `iter/iter.go` | `Seq`、`Seq2`、`Pull`、`Pull2` 及包文档 | 包文档对"单次使用 / 可重复使用迭代器"有什么约定？`Pull` 如何处理迭代器内部的 panic？ |
| `runtime/coro.go` | `newcoro`、`coroswitch`、`coroexit` | 协程切换和 goroutine 调度的区别是什么？为什么它比通过 channel 交接更快？ |
| `cmd/compile/internal/rangefunc/rewrite.go` | 文件头注释中的改写规则 | `break`、`continue`、`return`、`defer`、`goto` 分别被改写成什么？状态变量有哪几种取值？ |
| `runtime/panic.go` | `panicrangestate`、`deferrangefunc` | 运行时如何检测迭代器在 `yield` 返回 false 之后继续调用它？ |
| `slices/iter.go` · `maps/iter.go` · `strings/iter.go` | `All`、`Collect`、`Chunk`、`SplitSeq` | 这些函数为什么都很短？`Chunk` 返回的子切片和原切片是什么关系（联系 [Ch02](../ch02-slice-map-string/) 的别名问题）？ |

## 📊 Java 25 ↔ Go 1.27 机制级对照表

| 维度 | Java 25 | Go 1.27 | 取舍 / 游戏服影响 |
|---|---|---|---|
| 基本模型 | Iterator 拉式；Stream 推式（Sink 链） | `iter.Seq` 推式；`iter.Pull` 转拉式 | 两边都以推式为主，拉式用于需要"同时遍历两个序列"的场景 |
| 短路 | `Sink.cancellationRequested()` | `yield` 返回 false | 机制相同；Go 由语言强制契约，违约时运行时 panic |
| 惰性 | 中间操作惰性，终端操作触发 | 迭代器函数在 range 时才执行 | 一样惰性；Go 迭代器每次 range 都会重新执行，Stream 只能消费一次 |
| 组合器 | 内置丰富（map / filter / flatMap / Gatherers） | 标准库不提供，需要自己写 | Java 写数据处理更方便；Go 的手写循环往往更清晰、更快 |
| 并行 | `parallel()` + 公共 FJP | 不内置，需要显式使用 goroutine | Go 不会意外地把工作分发到共享线程池上 |
| 元素装箱 | `Stream<T>` 装箱，原始类型有专门的 `IntStream` 等 | 泛型实例化，无装箱 | 数值管线 Go 无需特殊 API |
| 拉式实现 | 手写状态机或使用 Spliterator | `iter.Pull` + 运行时协程 | Go 可以把任意推式迭代器自动转成拉式，代价是一个协程，用完要 stop |
| 资源清理 | `Stream.onClose` + try-with-resources | 迭代器函数内部 `defer`（循环提前退出时也会执行） | Go 的迭代器天然支持"遍历结束即释放"，例如关闭数据库游标 |

## 🔬 实验

### 实验 1：迭代器 vs 手写循环（必做）
- **目的**：验证"内联决定迭代器代价"。
- **方法**：`go/lab/iter`：对 100 万个实体分别用①手写 for 循环、②`slices.Values` + range、③自写 `Filter(Map(...))` 组合器链、④故意加 `//go:noinline` 的迭代器函数，求满足条件的实体血量总和。
- **工具**：`go test -bench=. -benchmem -count=10 | benchstat`；`go build -gcflags='-m=2'` 查看内联和闭包逃逸。
- **预期现象**：①和②持平；③取决于组合器是否被内联；④明显变慢，可能还伴有分配。
- **记录**：ns/op、allocs/op，以及 `-m` 输出中对应的关键行。

### 实验 2：iter.Pull 的切换成本（必做）
- **目的**：量化协程切换，并和 channel 方案对比。
- **方法**：用 `iter.Pull` 实现两个有序序列的归并（合并两个排行榜分片），对比"每个序列一个 goroutine + 无缓冲 channel"的实现，以及直接操作 slice 下标的实现。
- **工具**：benchmark + `go tool trace` 观察 channel 方案产生的调度事件。
- **预期现象**：Pull 版本的每元素开销明显低于 channel 版本，但高于下标版本；channel 版本在 trace 中能看到大量 goroutine 切换。

### 实验 3：Java Stream 的执行与代价
- **方法**：`java/StreamLab.java`：对同一个数据集分别用 for 循环、`Stream<Integer>`、`IntStream`、带 `Gatherers.windowFixed` 的管线求和。充分预热后计时；在 `limit` 前面的 `peek` 中打印日志，观察短路是如何传播的。
- **预期现象**：`Stream<Integer>` 最慢（装箱）；`IntStream` 预热后接近 for 循环；`peek` 日志显示 `limit` 之后上游停止推送。

## 🧪 练习规划

**Go：`go/exercises`**（`go test ./ch05-iterators/go/...`）
1. `func Filter[V any](seq iter.Seq[V], pred func(V) bool) iter.Seq[V]`、`func Map[V, R any](seq iter.Seq[V], f func(V) R) iter.Seq[R]`、`func Take[V any](seq iter.Seq[V], n int) iter.Seq[V]`：测试中用一个"违约检测"包装器确认 `yield` 返回 false 后，上游不会再推送（`Take` 是重点）。
2. `func (g *Grid) Neighbors(x, y int32, r int32) iter.Seq[EntityID]`：AOI 网格中按半径遍历周围实体，不分配中间 slice。测试检查结果正确，并用 `AllocsPerRun` 断言零分配。
3. `func MergeSorted[V cmp.Ordered](a, b iter.Seq[V]) iter.Seq[V]`：用 `iter.Pull` 归并两个有序序列。测试覆盖提前 `break` 的情况，确保两个 `stop` 都被调用（不泄漏协程）。
4. `func ReadRecords(r io.Reader) iter.Seq2[Record, error]`：逐行解析战报文件，出错时 yield `(zero, err)` 并停止。测试覆盖格式错误、提前 break，以及迭代器内部的 `defer` 清理是否执行。
5. `func Paginate[V any](xs []V, size int) iter.Seq2[int, []V]`：排行榜分页。在注释中说明返回的页切片和原切片的别名关系，以及调用方怎样安全地持有它。

**Java：`java/drills/StreamDrill.java`**
- 实现一个 `Gatherer<Integer, ?, Integer>`，做"滑动窗口平均伤害"（窗口大小为 n）。自校验：对已知序列验证输出。
- 用 `Spliterator` 手写一个按半径遍历网格邻居的数据源，再用 `StreamSupport.stream` 包装，和 Go 的 `Neighbors` 迭代器对比实现复杂度。

## 🎮 游戏服落地

- **AOI 查询**：`grid.Neighbors(x, y, r)` 返回 `iter.Seq[EntityID]`，广播逻辑直接 `for id := range ...`，不分配中间 slice。每帧调用成千上万次时，这比"先收集到 slice 再遍历"省下大量分配。Java 侧通常写回调接口或复用的 `List` 缓冲，Stream 在这种热路径上很少用。
- **配置表遍历与校验**：`for row := range table.Rows()` 加组合器做校验和索引构建。这类代码不在热路径上，用组合器提升可读性是值得的。
- **战报和日志处理**：`ReadRecords` 返回 `Seq2[Record, error]`，从文件或网络流式处理，内存占用恒定。Java 对应的是 `BufferedReader.lines()` 加 Stream。
- **不要用迭代器替代 channel**：迭代器是同步的、单线程的；跨 goroutine 传递数据（例如从网关到房间）仍然要用 channel 或 actor 的 mailbox（[Ch08](../ch08-channels-select/)、[Ch11](../ch11-actor/)）。

## ✅ 验收答辩题

- [ ] [L2] 写出 `for x := range seq { if x > 3 { break }; use(x) }` 被编译器改写之后的大致形态，说明 `break` 是如何传递给迭代器的。
- [ ] [L2] 迭代器在 `yield` 返回 false 后又调用了一次 `yield`，会发生什么？运行时是怎么检测到的？
- [ ] [L2] 循环体中的 `defer` 什么时候执行？为什么需要 `deferrangefunc` 这种特殊支持？
- [ ] [L2] `iter.Pull` 是怎么实现的？和"goroutine + channel"相比，它省掉了哪些开销？为什么必须调用 `stop`？
- [ ] [L2] Java Stream 的终端操作被调用时发生了什么？`limit(10)` 是如何让上游停止的？
- [ ] [L3] 拿出实验 1 的数据：组合器链在什么条件下和手写循环一样快？`-m` 的哪几行证明了这一点？
- [ ] [L3] 拿出实验 2 的数据：Pull、channel、下标三种归并的每元素开销分别是多少？
- [ ] [L3] 一个用迭代器实现的 AOI 查询在压测中出现大量分配，你会如何定位？可能的原因有哪些？
- [ ] [L4] 在"战斗帧内的实体遍历"、"配置表加载"、"排行榜分页导出"三个场景里，分别选择 slice、迭代器还是 channel，并说明理由；再给出 Java 25 中的对应选择。

## 🕳️ 常见坑

- **忽略 `yield` 的返回值**：自己写的迭代器在调用方 break 之后继续推送，运行时直接 panic。
- **以为迭代器只能遍历一次**：`iter.Seq` 每次 range 都会重新执行数据源函数，如果它有副作用（例如读网络），第二次 range 会得到不同的结果。
- **`iter.Pull` 不调用 `stop`**：提前退出的循环会让协程一直挂着，造成泄漏。习惯写法是 `next, stop := iter.Pull(seq); defer stop()`。
- **为了"函数式"把热路径写成多层组合器**，结果没能内联，比手写循环慢好几倍。热路径先手写，再测量。
- **把 Java 的并行流思维带过来**，期待迭代器自动并行；Go 的迭代器总是在当前 goroutine 里同步执行。
- **在迭代器内部持有锁并调用 `yield`**：循环体可能执行很久，甚至在里面尝试获取同一把锁，导致死锁。
- **`slices.Chunk` 返回的块直接存起来再 append**，覆盖了原切片的数据。

## 📚 一手资料

- Go spec：[For statements with range clause](https://go.dev/ref/spec#For_range)
- `iter` 包文档（`go doc iter`），其中详细说明了迭代器的约定
- 官方博客：Range Over Function Types、Fixing For Loops in Go 1.22
- 源码：`iter/iter.go`、`runtime/coro.go`、`cmd/compile/internal/rangefunc/rewrite.go`、`runtime/panic.go`
- Java：`java/util/stream/AbstractPipeline.java`、`Sink.java`、`Gatherer.java`、`Gatherers.java`、`java/util/Spliterator.java`（src.zip）、[JEP 485](https://openjdk.org/jeps/485)
