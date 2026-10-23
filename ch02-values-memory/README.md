# Ch02 · 值语义与内存表示

> Part 1 · 估时 10h · 状态：⚪ 未开始 · 一个 struct 在内存里长什么样、什么时候被拷贝。后面的接口、逃逸、GC 章节都以这个判断为地基

## 🎯 目标（资深标准）

- **L1**：熟练使用 struct、数组、指针、值/指针接收者；知道哪些操作会拷贝、哪些不可寻址。
- **L2**：手算任意 struct 的 `Sizeof` / `Alignof` / 字段偏移，并能用 `unsafe` 验证；讲清 Go"没有对象头"时，GC 元数据和类型信息放在哪里；讲清 HotSpot 对象头（mark word + klass）以及 JEP 519 紧凑对象头省掉的是什么。
- **L3**：先手算再验证：给定 struct 的大小、偏移、Java 对象的浅大小（默认头与紧凑头两种），用 `unsafe`、JOL 和 MemStats 核对；先预测 `-gcflags=-m` 对本章代码的逃逸结论，再看编译器输出，解释每一处偏差。
- **L4**：能讲清"值类型 vs 引用类型"这一设计分叉的全部后果：Go 为什么让 struct 和数组都是值、Java 为什么到 25 仍只有原始类型是值（Valhalla 不在 Java 25 中），这个选择如何决定两边的内存布局、相等性、身份、null 和 GC 扫描成本。

## 🔗 前置

[Ch00](../ch00-setup/)

## 💡 关键洞察

1. **Go 的 struct 是真正的值类型**：没有对象头、没有隐式引用；`[]Vec3` 就是一块连续的 `float32` 内存。Java 25 里 `record Vec3` 仍是堆上的引用对象，`Vec3[]` 是一个**指针数组**（值类型 Valhalla 不在 Java 25 中）。
2. **"传值还是传指针"不是风格问题，而是三件事的取舍**：拷贝成本、逃逸到堆（GC 压力）、共享可变状态（并发安全）。默认传值，有理由才传指针。
3. **Go 编译器不重排字段，HotSpot 会。** Go 的内存布局严格按声明顺序 + 对齐规则，字段顺序就是你的责任；JVM 会按字段大小重排来减少填充。

## ☕ Java 25 深度复习

### 对象在 HotSpot 里的样子
- **对象头**：64 位 JVM 上，默认是 mark word（8 字节：哈希、GC 年龄、锁状态）+ klass pointer。开启压缩类指针（`UseCompressedClassPointers`，默认开）后 klass pointer 为 4 字节，头部共 12 字节；对象按 `ObjectAlignmentInBytes`（默认 8）对齐。所以 `java.lang.Integer` 是 12 + 4 = 16 字节，`java.lang.Long` 是 12 + 8 = 20 → 对齐到 24 字节。
- **JEP 519 紧凑对象头（Java 25 正式，需显式开启）**：`-XX:+UseCompactObjectHeaders`（本机 JDK 25.0.2 实测默认值是 `false`）。它把类指针压进 mark word，头部从 12 字节降到 8 字节。小对象越多，节省越明显，同时减轻 GC 压力。代价和限制要读 JEP 的 Risks 一节（例如能编码的类数量有上限）。
- **压缩普通对象指针**：`UseCompressedOops`（堆小于约 32GB 时由 JVM 自动开启），引用字段占 4 字节。所以 `Object[]` 的每个元素是 4 字节的压缩引用，指向分散在堆上的对象。
- **字段布局**：HotSpot 按字段大小分组重排，以减少填充（实现位于 HotSpot `src/hotspot/share/classfile/` 的字段布局代码）。所以 Java 程序员很少关心字段顺序，这个习惯带到 Go 会吃亏。
- **伪共享防护**：`@jdk.internal.vm.annotation.Contended` 在字段前后填充（`ContendedPaddingWidth` 默认 128 字节）。用户代码要用它，需要 `-XX:-RestrictContended`。Go 没有对应注解，只能手工填充（见 [Ch12](../ch12-sync-memory-model/)）。
- **record**：`java.lang.Record` 的 `equals/hashCode/toString` 由 `java/lang/runtime/ObjectMethods.java` 通过 invokedynamic 引导生成。它是不可变的数据载体，但仍然是引用类型：有对象头，有身份（identity），可以为 `null`。
- **逃逸分析会消掉部分分配**：C2 对不逃逸的对象做标量替换，但这是 JIT 的不可见优化，不能写进设计假设（深入见 [Ch15](../ch15-alloc-escape-compiler/)）。

### 资料与源码
- HotSpot `src/hotspot/share/oops/`：对象头、markWord、klass 的定义。
- JEP 519、JEP 450（紧凑对象头的实验版）的 Description 和 Risks 两节。
- JOL（OpenJDK Code Tools 子项目，需单独下载 jar）：`java -jar jol-cli.jar internals java.lang.Long`，可以打印真实布局。

## 🐹 Go 1.27 深入

### 规范层
- **类型的大小与对齐**：spec「Size and alignment guarantees」一节规定了 `unsafe.Sizeof/Alignof/Offsetof` 的语义。参数类型不含类型参数时，它们都是**编译期常量**。
- **结构体比较**：所有字段都可比较时，struct 才可比较（`==`），也才能做 map key。含 slice、map、func 字段时不可比较。
- **数组是值**：`[4]int` 赋值、传参都是整体拷贝；slice 只拷贝 24 字节的 header（深入见 [Ch03](../ch03-slice-map-string/)）。
- **可寻址性**（spec「Address operators」）：变量、指针解引用、slice 元素、可寻址 struct 的字段、可寻址数组的元素都可寻址；**map 元素、函数返回值、常量不可寻址**。所以 `m[k].HP = 1` 编译失败。
- **方法集**（spec「Method sets」）：`T` 的方法集只包含接收者为 `T` 的方法；`*T` 的方法集包含 `T` 和 `*T` 两种接收者的方法。对可寻址的值调用指针方法时，编译器会自动取地址（`v.Scale()` → `(&v).Scale()`），但放进接口时没有这层便利（见 [Ch05](../ch05-interfaces/)）。
- **零值**：每个类型都有零值，`var e Entity` 就是一个可用的实体。设计类型时，应该让零值有意义（`sync.Mutex`、`bytes.Buffer` 是范例）。
- **`new(expr)`**：内建 `new` 既可以接类型（`new(T)` 得到指向零值的 `*T`），也可以接表达式（`new(42)` 得到指向 42 的 `*int`）。它只说明"得到一个指针"，**不说明分配在哪里**——堆还是栈由逃逸分析决定。

### 运行时与编译器层
- **布局计算**：编译器在 `cmd/compile/internal/types/size.go` 里按声明顺序计算偏移，每个字段按自身对齐向上取整，整体大小按最大对齐取整。有一个细节：非零大小的 struct 如果以零大小字段结尾，会额外填充，防止对它取地址时得到一个指向对象外部的指针。
- **常见类型大小（amd64）**：`int`/指针/`map`/`chan`/`func` 都是 8 字节；`string` 16 字节（指针 + 长度）；slice 24 字节（指针 + len + cap）；接口 16 字节（类型/itab 指针 + 数据指针）。
- **没有对象头，元数据在哪里**：GC 需要的"哪些字是指针"信息来自类型元数据（`internal/abi/type.go` 的 `Type`）和堆上的指针位图（`runtime/mbitmap.go`）。对象本身不带任何头部。**完全不含指针**的类型会分配到 noscan span，GC 不需要扫描（这是 [Ch16](../ch16-gc/) 的关键优化点）。
- **`structs.HostLayout`**：在 struct 里嵌入这个标记字段，表示按宿主平台 C ABI 的规则布局，cgo/系统调用场景用（[Ch19](../ch19-syscall-cgo-ffi/)）。普通 Go struct 的布局规则不对外承诺。
- **逃逸入门**：`go build -gcflags='-m'` 打印 `moved to heap` / `escapes to heap` / `does not escape`。返回局部变量的地址并不等于一定在堆上，要看逃逸分析的结论。本章只要求看懂，原理在 [Ch15](../ch15-alloc-escape-compiler/)。

### 源码阅读清单（`$GOROOT/src`）
| 文件 | 函数 / 类型 | 带着什么问题读 |
|---|---|---|
| `unsafe/unsafe.go` | `Sizeof`、`Alignof`、`Offsetof` 的文档 | 它们对什么样的表达式求值？为什么结果是 `uintptr` 常量？ |
| `cmd/compile/internal/types/size.go` | `calcStructOffset`、`CalcStructSize`、`CalcSize` | 偏移和尾部填充是怎么算的？零大小尾字段为什么要额外填充？ |
| `internal/abi/type.go` | `Type`（`Size_`、`PtrBytes`、`Align_`、`Kind_`） | `PtrBytes` 表示什么？它如何让 GC 只扫描对象的前缀部分？ |
| `runtime/mbitmap.go` | 堆指针位图相关函数 | 没有对象头时，GC 如何知道堆对象里哪些字是指针？ |
| `structs/hostlayout.go` | `HostLayout` | 为什么 Go 要显式区分"Go 布局"和"宿主布局"？ |

## 📊 Java 25 ↔ Go 1.27 机制级对照表

| 维度 | Java 25（HotSpot） | Go 1.27 | 取舍 / 设计动机 |
|---|---|---|---|
| 每对象固定开销 | 对象头 12 字节（紧凑对象头 8 字节）+ 对齐 | 0 字节，没有对象头 | Java 的头部承载身份哈希、锁、GC 年龄；Go 把这些移出对象（类型元数据、堆位图、外部锁），换来零开销的值 |
| 用户类型的值语义 | 只有原始类型是值；record 是引用 | 所有 struct 和数组都是值 | Java 的"一切对象皆引用"让 GC 和并发语义统一；Go 让程序员决定拷贝什么 |
| 数组元素 | `Vec3[]` = 引用数组，元素分散在堆上 | `[]Vec3` = 元素内联、连续存放 | Go 的连续布局是值语义的直接结果；Java 要得到同样布局只能手写 SoA 或等 Valhalla |
| 字段布局 | JVM 按大小重排字段 | 按声明顺序，不重排 | Go 选择可预测（和 C 一样按声明顺序），把优化责任交给程序员（x/tools 有 fieldalignment 分析器） |
| 身份与 null | 每个对象有身份，可以 `synchronized`，可以为 null | 值没有身份；只有指针可以为 nil | 没有身份意味着值不能被"锁住"、不能用 `==` 区分"同一个"，但也不会为 null |
| 相等性 | `equals`/`hashCode` 约定，record 自动生成 | `==` 逐字段比较（要求字段可比较） | Go 把相等性做成语言规则而不是可覆盖的方法：可预测，但不能自定义 |
| GC 元数据 | 在对象头的 klass 中 | 类型元数据 + 堆位图；无指针对象走 noscan | 没有对象头，GC 只能从外部元数据得知指针位置；回报是无指针对象完全不用扫描 |
| 伪共享 | `@Contended` | 手工填充 `[N]byte` | 不重排字段的另一面：填充写在哪里就在哪里，不需要注解 |
| 方法接收者 | 永远是 `this` 引用 | 由你选值或指针接收者，影响方法集和逃逸 | 接收者就是普通参数，所以"传值还是传指针"的规则对它同样适用 |

## 🔬 实验

### 实验 1：字段顺序与填充（必做）
- **目的**：验证 Go 不重排字段，并量化填充的代价。
- **方法**：在 `go/lab/layout` 里定义 `LooseEntity{Alive bool; ID int64; Team uint8; HP int32; Flag bool; X, Y float64}` 和重排后的 `PackedEntity`。用 `unsafe.Sizeof/Offsetof` 打印每个字段的偏移；各分配 100 万个元素的 slice，读 `runtime.MemStats.HeapAlloc` 前后差值。
- **工具**：`go test -run TestLayout -v`；benchmark 遍历两个 slice 累加 HP：`go test -bench=Iterate -count=10 | benchstat`。
- **预期现象**：Loose 是 48 字节，Packed 是 32 字节（前者大 50%）；遍历速度有可测差异，幅度取决于 slice 是否超出 LLC。
- **记录**：两种布局的偏移表、HeapAlloc、benchstat 结果、CPU 型号和缓存大小。

### 实验 2：同一数据在两种运行时里的内存（必做）
- **目的**：量化"对象头 + 引用数组" vs "连续值数组"。
- **方法**：
  - Java（`java/LayoutLab.java`）：分别分配 `record Vec2(float x, float y)` 和 `record Vec3(float x, float y, float z)` 的 1000 万元素数组。`System.gc()` 后用 `Runtime` 读已用堆，分别在默认参数和 `-XX:+UseCompactObjectHeaders` 下运行。
  - 对照组：Java 用 SoA（三个 `float[]`），Go 用 `[]Vec3`。
- **工具**：`bin/jrun -Xmx2g ...`；可选用 JOL 打印实际大小。
- **预期现象（先自己算，再看数据）**：
  - 默认参数下：Vec2 = 12 + 8 = 20 → 24 字节，Vec3 = 12 + 12 = 24 字节，每个元素再加 4 字节引用。
  - 紧凑头下：Vec2 = 8 + 8 = 16 字节，**下降**；Vec3 = 8 + 12 = 20 → 对齐后**仍是 24 字节**，紧凑头的收益被对齐吃掉了。
  - Java SoA 和 Go `[]Vec3` 都接近 12 字节/元素。
- **记录**：各组数据的字节/元素，以及与理论值的偏差和原因。结论应该能回答："紧凑对象头对哪些对象有效？"

### 实验 3：值接收者 vs 指针接收者
- **目的**：看清拷贝成本和逃逸的分界。
- **方法**：`type Big struct{ data [256]byte }`，分别写值接收者和指针接收者的方法，在循环里调用；再写一个把 `*Big` 存进全局变量的版本。
- **工具**：`go build -gcflags='-m=2' ./ch02-values-memory/go/lab/recv` + benchmark。
- **预期现象**：值接收者每次调用拷贝 256 字节（内联后可能被优化掉，以数据为准）；存入全局变量的那个版本出现 `moved to heap`。

## 🧪 练习规划

**Go：`go/exercises`**（`go test -race ./ch02-values-memory/go/...`）
1. `type Vec3 struct{ X, Y, Z float32 }`，实现 `func (v Vec3) Add(o Vec3) Vec3` 和 `func (v *Vec3) Scale(k float32)`。测试断言 Add 不修改原值、Scale 原地修改，并验证 `Vec3` 值能否满足一个要求 `Scale` 的接口（方法集）。
2. `type PackedEntity struct{...}`：把给定的 `LooseEntity` 重排到最小。测试用 `unsafe.Sizeof` 断言大小等于目标值，用反射断言字段集合不变。
3. `func MoveAll(es []Entity, dx, dy float32)`：原地移动所有实体。测试专门抓 `for _, e := range es` 改的是副本这个 bug。
4. `func Heal(players map[int64]Player, id int64, amount int32) bool`：值类型 map 的更新（不可寻址 → 读出、修改、写回）。另写一个 `map[int64]*Player` 版本，在注释里比较两者在 GC 扫描和并发安全上的差别。
5. `func Snapshot(p Player) Player`：返回与原值不共享任何可变状态的副本（深拷贝）。`Player` 里有 `Items []Item`，测试会在快照之后修改原对象，抓"浅拷贝共享底层数组"的问题。

**Java：`java/drills/LayoutDrill.java`**
- `long shallowSize(int headerBytes, int... fieldSizes)`：按"头 + 按大小降序排列字段 + 8 字节对齐"的模型估算对象大小。自校验：`Integer` = 16、`Long` = 24（12 字节头时）。
- `record Vec3` 的 `add`/`scale`：返回新实例，对比 Go 的值接收者语义。
- `moveAll(Entity[] es, float dx)`：Java 里通过引用修改天然生效。在注释中写出为什么同样的写法在 Go 里会失效。

## 🧭 设计取舍

- **为什么 Go 没有对象头。** 对象头里的东西在 Go 里各有去处：类型信息在接口值或编译期已知的类型里，GC 需要的指针信息在类型元数据和堆位图里，锁是显式的 `sync.Mutex` 字段，没有身份哈希。代价是 Go 的值不能 `synchronized`、没有 `System.identityHashCode`；回报是值可以内联进数组和别的 struct，零开销。
- **为什么 Go 不重排字段。** 按声明顺序布局让 `unsafe.Offsetof`、与 C 交互、手工填充都可以预测。HotSpot 可以重排，是因为 Java 程序无法观察字段偏移（`Unsafe` 除外）。代价是 Go 程序员要自己排字段；`structs.HostLayout` 说明 Go 也不想把"Go 布局 = C 布局"写成承诺。
- **值语义 + 指针，而不是"值类型 vs 引用类型"两种类型。** C# 和 Valhalla 的思路是在**类型**上区分值和引用；Go 在**使用处**区分：同一个 `Player` 既可以当值用，也可以取地址当引用用。灵活，但"这里拷贝了没有"要靠读代码判断，`range` 拷贝、map 元素不可寻址这些坑都来源于此。
- **Java 为什么到 25 仍没有用户定义的值类型。** 引用语义贯穿 JVM：对象身份、`synchronized`、null、数组协变、泛型擦除都建立在"所有对象都是引用"上。Valhalla 要在不破坏这些的前提下引入值对象，所以迟迟不能定稿。JEP 519 紧凑对象头是在引用模型内部做减法：头部从 12 字节压到 8 字节。
- **AoS 与 SoA 是布局问题，不是语言问题。** Go 的 `[]Entity` 天然是 AoS 连续内存，需要按字段批量遍历时可以改成 SoA（`xs, ys []float32`），且不含指针、GC 不扫描。Java 只有原始类型数组是连续的，SoA 是它得到连续布局的唯一办法。两边做 SoA 的动机因此不同：Go 是为了访问模式，Java 是为了绕开引用数组。

## ✅ 验收答辩题

- [ ] [L2] 手算 `struct{ a bool; b int64; c bool }` 和 `struct{ b int64; a, c bool }` 的大小，并解释差异；再用 `unsafe.Offsetof` 验证。
- [ ] [L2] Go 对象没有头部，GC 怎么知道一个堆对象里哪些字是指针？无指针对象为什么扫描更便宜？
- [ ] [L2] HotSpot 64 位下 `new Long(1)` 占多少字节？开启 `-XX:+UseCompactObjectHeaders` 以后呢？分别由哪几部分组成？
- [ ] [L2] 为什么 `m[k].HP = 10` 编译失败，而 `s[i].HP = 10` 可以？从可寻址性的定义回答。
- [ ] [L2] 一个只有指针接收者方法的类型，它的值能赋给对应的接口吗？为什么直接调用 `v.Method()` 却能编译？
- [ ] [L3] 先预测、再拿出实验 1 的 benchstat 数据：字段重排在你机器上带来了多少收益？在什么条件下收益会消失（提示：slice 是否超出 LLC）？
- [ ] [L3] 拿出实验 2 的数据，解释 Java AoS、Java SoA、Go `[]Vec3` 每元素字节数的差异，误差控制在 10% 以内。
- [ ] [L3] 一个 `Snapshot` 函数返回了 struct 副本，但副本在之后"自己变了"。从拷贝语义解释原因；如果原对象在另一个 goroutine 里被修改，`-race` 能抓到吗？写一个测试证明你的判断。
- [ ] [L4] Go 在"使用处"区分值和引用（同一类型可以传值也可以取地址），Valhalla 在"类型声明处"区分。比较两种设计在可预测性、API 设计、泛型交互上的得失。
- [ ] [L4] 读 JEP 519 的 Description 和 Risks：紧凑对象头把类指针压进 mark word 之后，失去了什么、限制了什么？为什么它在 JDK 25 仍需要显式开启？

## 🕳️ 常见坑

- **`for _, v := range xs { v.X++ }` 改的是副本。** 要用 `xs[i].X++` 或 `for i := range xs`。
- **以为 struct 赋值是"引用复制"**：Java 直觉里 `a = b` 之后两者指向同一个对象；Go 里是两份独立的值，只有内部的 slice、map、指针字段是共享的。
- **值接收者和指针接收者混用**，导致某些值不满足接口，或者修改不生效。同一类型的接收者应保持一致。
- **字段随手排**：照 Java 习惯写，结果大量填充；高频实体类型要专门排序。
- **为了"避免拷贝"到处用指针**：大量小对象逃逸到堆，GC 压力反而更大。小 struct 传值往往更快。
- **在 map 里存大 struct 值并频繁修改**：每次读、改、写回都拷贝整个 struct。
- **把 `unsafe.Sizeof` 当成深度大小**：它只算 header（例如 string 恒为 16），不算指向的数据。

## 📚 一手资料

- Go spec：[Size and alignment guarantees](https://go.dev/ref/spec#Size_and_alignment_guarantees)、[Method sets](https://go.dev/ref/spec#Method_sets)、[Address operators](https://go.dev/ref/spec#Address_operators)、[Comparison operators](https://go.dev/ref/spec#Comparison_operators)
- 源码：`cmd/compile/internal/types/size.go`、`internal/abi/type.go`、`runtime/mbitmap.go`
- 官方：[Go FAQ: Should I define methods on values or pointers?](https://go.dev/doc/faq#methods_on_values_or_pointers)
- Java：[JEP 519](https://openjdk.org/jeps/519)、[JEP 450](https://openjdk.org/jeps/450)、HotSpot `src/hotspot/share/oops/`、JOL（openjdk.org/projects/code-tools/jol）
- Aleksey Shipilëv：《JVM Anatomy Quarks》中关于对象头、字段布局、压缩指针的几篇
