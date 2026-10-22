# Ch04 · 泛型深入与 API 设计

> Part 1 · 估时 12h · 状态：⚪ 未开始 · 类型集、GC shape 单态化 + 字典，以及 1.27 的泛型方法；对照 Java 的类型擦除，弄清两种泛型各自的能力边界和性能代价，再落到构造与配置 API 的设计

## 🎯 目标（资深标准）

- **L1**：熟练写泛型函数、泛型类型、约束（含 `~T`、联合类型、`comparable`、`cmp.Ordered`）、指针方法约束；会用 1.27 的泛型方法；会写 functional options。
- **L2**：讲清 Go 泛型的实现（GC shape stenciling + 字典），以及它为什么导致"类型参数上的方法调用不能内联"；讲清**为什么泛型方法不能实现接口方法**；讲清 Java 的擦除、桥方法、装箱和堆污染。
- **L3**：用 benchmark 和符号表证明某个泛型函数被实例化成了几个 shape、每个 shape 的性能如何，并判断什么时候应该把泛型改回具体类型或接口。
- **L4**：为游戏服基础库（对象池、事件总线、配置表、ECS 组件存储）设计泛型 API：说明哪里用泛型、哪里用接口、哪里用代码生成，并和 Java 25 的同类设计比较。

## 🔗 前置

[Ch01](../ch01-values-memory/)、[Ch02](../ch02-slice-map-string/)、[Ch03](../ch03-interfaces/)

## 💡 关键洞察

1. **Go 泛型既不是 C++ 那样的完全单态化，也不是 Java 那样的擦除，而是"按 GC shape 单态化 + 运行时字典"。** 所有指针类型共享同一个 shape，所以 `F[*A]` 和 `F[*B]` 是同一份机器码，类型相关的操作都要查字典。
2. **1.27 的泛型方法让 API 能"挂在类型上"，但它们不进入方法集。** 泛型方法不能实现接口方法，`reflect` 也看不到它们（本机实测 `NumMethod` 不计入）。原因很直接：itab 只能存有限个函数指针，而泛型方法有无穷多个实例。Java 的泛型方法可以是虚方法，因为擦除后只有一份代码。
3. **"泛型 vs 接口"的选择看数据流向**：容器和算法这类"值穿过代码"的场景用泛型；"行为因类型而不同"的场景用接口。拿泛型去模拟多态，通常比接口更慢，也更难读。

## ☕ Java 25 深度复习

### 擦除式泛型的实现
- **擦除**：编译后类型参数替换为上界（默认 `Object`），类型信息只保留在签名属性（`Signature`）中供反射读取；不能 `new T()`、不能 `T.class`、不能 `instanceof List<String>`。
- **桥方法**：`class IntBox implements Box<Integer>` 实现 `Integer get()` 时，编译器会额外生成一个 `Object get()` 桥方法（`javap -c` 中标记为 `bridge synthetic`），以维持多态。`Method.isBridge()` 可以检测。
- **装箱**：类型实参不能是原始类型，`List<Integer>` 里每个元素都是一个 `Integer` 对象（参考 [Ch01](../ch01-values-memory/) 的对象头开销）。这正是 Go 泛型在数值容器上的主场优势。
- **通配符与 PECS**：`? extends T` 用于读（Producer Extends），`? super T` 用于写（Consumer Super）。Go 没有型变，`[]Dog` 不能当 `[]Animal` 用，靠类型参数化来表达。
- **堆污染**：泛型可变参数会制造 `T[]` 堆污染，用 `@SafeVarargs` 声明安全。
- **Valhalla（值类型 / 泛型特化）不在 Java 25 中**，所以 Java 25 的泛型容器仍然是"引用 + 装箱"。

### 构造与配置 API
- **JEP 513 灵活构造体（Java 25 正式）**：允许在 `super(...)` / `this(...)` 之前写语句（参数校验、准备参数），但不能引用正在构造的实例（"早期构造上下文"）。它解决的是"校验必须在父类构造之前"的老问题。Go 没有构造器，工厂函数里本来就能随意写代码。
- **Builder 模式**：用 Builder 应对大量可选参数、链式调用和不可变结果。Go 的对应物是 functional options 或配置 struct。
- **record 的紧凑构造器**：在规范构造器里做参数校验和规范化，对照 Go 的 `NewXxx` 校验后返回 `(*T, error)`。

## 🐹 Go 1.27 深入

### 规范层
- **类型参数与约束**：约束就是接口，接口定义一个**类型集**。`~int` 表示"底层类型为 int 的所有类型"；`int | float64` 是联合类型；`comparable` 表示可以用 `==` 比较的类型。规范按"类型集中所有类型都支持"来判断某个操作是否合法。
- **指针方法约束**：`func NewPool[T any, PT interface{ *T; Reset() }]()` 这种模式让你能在泛型代码里 `new(T)`，又能调用指针接收者上的方法。这是写对象池、反序列化框架的关键技巧。
- **类型推断**：从函数实参推断，也从约束推断。**1.27 新增**：把泛型函数赋值给（或转换为）匹配的函数类型时，在所有上下文中都可以推断。本机 go1.27.3 实测，以下写法都能编译：
  - `var f func(int) int = Identity`
  - 结构体字段 `Op{F: Identity}`
  - map 元素 `map[string]func(int) int{"id": Identity}`
  - 返回值 `return Identity`
  - 显式转换 `(func(int) int)(Identity)`
- **1.27 新增：泛型方法**。方法可以声明自己的类型参数，例如标准库的 `func (r *Rand) N[Int intType](n Int) Int`，也可以是泛型类型上的方法再带额外参数，如 `func (s *Stack[T]) Map[U any](f func(T) U) []U`。限制和本机实测结果：
  - 接口里的方法不能声明类型参数；泛型方法不能用来实现接口方法。
  - `reflect.TypeOf(v).NumMethod()` **不包含**泛型方法。
  - 可以显式实例化后取方法值：`f := b.First[int]`。
  - 模块 `go` 行低于 1.27 时报 "generic method requires go1.27 or later"。
- **其他现行特性**：
  - 泛型类型可以在自己的类型参数列表里引用自身（`type Adder[A Adder[A]] interface{ Add(A) A }`），这让"F-bounded"约束成为可能，对照 Java 的 `<T extends Comparable<T>>`。
  - 泛型类型别名：`type Set[T comparable] = map[T]struct{}`。
  - `new(expr)`。

### 运行时与编译器层
- **GC shape stenciling**：编译器按类型实参的 "shape" 生成代码。shape 由底层类型决定，**所有指针类型共享一个 shape**。实现位于 `cmd/compile/internal/noder/reader.go` 的 `Shapify`；在符号表和 pprof 中能看到形如 `go.shape.int64` 的实例名。
- **字典**：每次调用都隐式传入一个字典（`readerDict`），里面有类型描述符、类型参数上方法的 itab / 函数指针、子字典等。泛型代码中的 `T` 转 `any`、调用 `T` 的方法、`new(T)` 都要查字典。
- **性能后果**：
  - 当类型实参是值类型（`int64`、小 struct）时，每个 shape 都得到专门的代码，性能接近手写。
  - 当类型实参是指针或接口时，多个类型共享代码，方法调用经字典间接进行，**通常无法内联**，有时比直接用接口还慢。
  - 泛型函数自身被内联到调用点后，编译器能看到具体类型，代价可能被消除。所以必须**测量**，不能想当然。
- **二进制与编译时间**：每多一个 shape，就多一份代码。大量不同值类型的实例化会让二进制和编译时间增长。
- **类型检查实现**：`cmd/compile/internal/types2/infer.go`（推断）和 `unify.go`（合一）。

### 源码阅读清单（`$GOROOT/src`）
| 文件 | 函数 / 类型 | 带着什么问题读 |
|---|---|---|
| `cmd/compile/internal/noder/reader.go` | `Shapify` | 哪些类型会被归并成同一个 shape？`basic` 参数控制什么？ |
| `cmd/compile/internal/noder/reader.go` | `readerDict`、`objDictIdx`、`shapeSig` | 字典里到底放了哪些条目？shaped 和 non-shaped 字典的区别是什么？ |
| `cmd/compile/internal/types2/infer.go` · `unify.go` | `infer`、合一算法 | 约束类型推断在什么时候发生？1.27 推广的"函数类型赋值推断"走的是哪条路径？ |
| `math/rand/v2/rand.go` | `(*Rand).N` | 标准库第一个泛型方法长什么样？它为什么只能是方法、不能进接口？ |
| `slices/slices.go`、`maps/maps.go` | `Index`、`Clone` 等 | 标准库在什么地方用 `S ~[]E` 这种约束写法？为什么不直接写 `[]E`？ |
| `sync/atomic/type.go` | `Pointer[T]` | 一个泛型类型如何在零开销的前提下提供类型安全？ |

## 📊 Java 25 ↔ Go 1.27 机制级对照表

| 维度 | Java 25 | Go 1.27 | 取舍 / 游戏服影响 |
|---|---|---|---|
| 实现策略 | 擦除，所有实例共享一份字节码 | 按 GC shape 单态化 + 字典 | Go 值类型实例化快，指针实例化和接口差不多；Java 靠 JIT 投机内联 |
| 原始类型实参 | 不支持，必须装箱 | 支持，`[]int64` 就是连续内存 | 数值容器（伤害统计、坐标数组）Go 完胜 |
| 运行时类型信息 | 擦除，只在签名里保留 | 字典中有完整的类型描述符 | Go 可以在泛型代码里 `new(T)`、做类型断言 |
| 泛型方法 | 一直支持，并且可以是虚方法 | **1.27 新增**，但不进方法集，不能实现接口方法 | Go 的泛型方法只是"命名空间上的便利"，不能用于多态 |
| 型变 | 通配符 `? extends/super` | 无型变，靠类型参数表达 | Go 的 API 更简单，但表达力弱一些 |
| F-bounded 约束 | `<T extends Comparable<T>>` | 自引用泛型类型（现行特性） | 两边都能写"可以和自己比较的类型" |
| 构造 | 构造器 + JEP 513 前置语句 + Builder | 工厂函数 + functional options / 配置 struct | Go 不需要构造器语法；但没有强制，零值必须有意义 |
| 代码体积 | 不随实例化增长 | 每个 shape 一份代码 | 大量泛型实例化时要关注 Go 二进制和编译时间 |
| 反射可见性 | 泛型方法正常可见 | 泛型方法对 `reflect` 不可见 | 依赖反射的框架（RPC 注册、命令分发）不能使用泛型方法 |

## 🔬 实验

### 实验 1：泛型的真实性能（必做）
- **目的**：看清"值 shape"和"指针 shape"下泛型代码的代价。
- **方法**：`go/lab/generic`：
  - `Sum[T Number]([]T)`，分别对比 `[]int64` 的泛型版、手写版和 `[]Number` 接口版；
  - `UpdateAll[T Updater](xs []T)`，T 分别为值类型和指针类型，方法调用经类型参数进行，与接口版对比。
- **工具**：`go test -bench=. -count=10 | benchstat`；`go build -gcflags='-m=2'` 看内联；`go tool nm <二进制> | grep go.shape` 数 shape。
- **预期现象**：值 shape 的泛型版和手写版持平；指针 shape 下方法调用无法内联，和接口版接近甚至更慢。
- **记录**：ns/op 表、各实例对应的 shape 名称、内联决策。

### 实验 2：装箱的代价（必做）
- **目的**：量化 Java 擦除泛型在数值场景的成本。
- **方法**：Java 对 1000 万元素分别用 `ArrayList<Integer>` 求和与 `int[]` 求和（充分预热，各跑多轮），用 Runtime 统计内存；Go 用泛型 `Sum[int32]`。
- **工具**：`bin/jrun -Xmx2g`；JMH 留到 [Ch15](../ch15-profiling-bench/) 再做严格版。
- **预期现象**：元素取值超出 `Integer` 缓存范围（-128～127）时，`ArrayList<Integer>` 每元素约 16 字节对象 + 4 字节引用，约为 `int[]` 的 5 倍，遍历速度也明显更慢；Go 泛型版和 `int[]` 一个量级。

### 实验 3：实例化对二进制的影响
- **方法**：用代码生成 50 个不同的 struct 类型，分别实例化同一个泛型函数；再改成 50 个指针类型实例化。比较二进制大小、编译时间和 `go.shape` 符号数。
- **预期现象**：值类型版本的 shape 数接近 50；指针类型版本只有 1 个。

## 🧪 练习规划

**Go：`go/exercises`**（`go test ./ch04-generics/go/...`）
1. `type Pool[T any, PT interface{ *T; Reset() }] struct{...}`，`func NewPool[T any, PT interface{ *T; Reset() }](max int) *Pool[T, PT]`，提供 `Get() PT` 和 `Put(PT)`：一个带容量上限的泛型对象池骨架（练指针方法约束）。测试检查 Put 后 Reset 被调用、超过上限的对象被丢弃。
2. `func TopK[T any](xs []T, k int, less func(a, b T) bool) []T`：基于堆的 Top-K（排行榜前 100 名）。测试覆盖 k > len、重复值、稳定性说明；附 benchmark，与 `slices.SortFunc` 后截断的方案对比。
3. **1.27 泛型方法**：`type Inventory struct{ items []Item }`，`func (inv *Inventory) First[T Item]() (T, bool)` 和 `func (inv *Inventory) All[T Item]() []T`，按具体类型查找背包物品。测试中额外断言 `reflect.TypeOf(inv).NumMethod()` 不计入这两个方法；在注释里解释为什么不能把 `First` 放进接口。
4. `func NewRoomServer(addr string, opts ...Option) (*RoomServer, error)`，提供 `WithMaxPlayers`、`WithTickRate`、`WithLogger`：functional options。要求有默认值、参数校验（非法 tick rate 返回错误）、选项之间的冲突检测；测试覆盖各种组合。
5. `type Entity[E any] interface{ ID() int64; Clone() E }`，`func CloneAll[E Entity[E]](xs []E) []E`：用自引用约束表达"克隆后得到同一类型"。

**Java：`java/drills/GenericsDrill.java`**
- `static <T extends Comparable<? super T>> T max(Collection<? extends T> xs)`：练 PECS；自校验时用到父类实现 `Comparable` 的子类集合。
- 反射检测桥方法：`IntBox implements Box<Integer>`，断言存在一个 `isBridge()` 为 true 的 `get` 方法。
- JEP 513：`class Room extends Base`，在 `super(...)` 之前校验参数并抛异常；自校验确认非法参数不会进入父类构造器。

## 🎮 游戏服落地

- **配置表加载**：`func LoadTable[T any, PT interface{ *T; Validate() error }](path string) (map[int32]T, error)`，一个泛型加载器服务所有表。对照 Java：`Class<T>` 参数 + Jackson 反射。Go 版本无需传 Class 对象，也没有装箱。
- **ECS 组件存储**：`type Storage[C any] struct{ dense []C; sparse []int32 }`，每种组件是一个值 shape，迭代紧凑、无装箱。Java 要么每种组件手写 `float[]` 存储，要么接受 `ArrayList<Component>` 的指针追逐。
- **事件总线**：`Subscribe[E Event](bus *Bus, fn func(E))`。在 1.27 中可以写成 `bus.Subscribe[E](fn)` 形式的泛型方法，但注意它不能放进接口，需要 mock 时只能 mock `Bus` 的具体实现。
- **服务启动参数**：网关、逻辑服用 functional options 统一构造方式；可选参数多、需要校验和兼容老配置时，配置 struct + `Validate()` 往往比 options 更好维护（配置可以直接从文件反序列化）。

## ✅ 验收答辩题

- [ ] [L2] 什么是 GC shape？`F[int]`、`F[MyInt]`、`F[*A]`、`F[*B]` 会生成几份代码？
- [ ] [L2] 泛型函数里调用类型参数的方法，底层怎么执行？为什么它往往无法内联？
- [ ] [L2] 为什么 Go 的泛型方法不能实现接口方法，而 Java 的泛型方法可以是虚方法？从 itab 和擦除两个角度回答。
- [ ] [L2] 解释桥方法：什么时候生成？如果没有它会出什么问题？
- [ ] [L2] `PT interface{ *T; Reset() }` 这种约束解决了什么问题？不用它的话，`Pool[T]` 能在内部 `new` 出对象并调用 `Reset` 吗？
- [ ] [L2] JEP 513 允许 `super()` 之前写什么、不允许写什么？为什么要有这些限制？
- [ ] [L3] 拿出实验 1 的数据：值 shape 和指针 shape 的性能差多少？你用了什么证据证明代价来自"无法内联"？
- [ ] [L3] 某个泛型容器在 pprof 中出现大量 `go.shape.*` 相关的热点，你会如何判断要不要把它改回具体类型？
- [ ] [L3] 拿出实验 2 的数据，解释 `ArrayList<Integer>` 和 `int[]` 的内存与速度差距来自哪些因素。
- [ ] [L4] 为游戏服基础库设计"对象池 / 事件总线 / 配置表"三个 API：哪些用泛型、哪些用接口、哪些用代码生成？给出理由。
- [ ] [L4] functional options 和配置 struct 各适合什么场景？在一个需要热加载配置的网关服务里你选哪种？

## 🕳️ 常见坑

- **用泛型模拟多态**：`func Handle[T Message](m T)` 里再对 T 做 type switch，既慢又绕；行为因类型而不同，就应该用接口。
- **以为泛型等于零成本**：指针 shape 下的方法调用有字典间接成本，必须测量。
- **期待 Java 式的型变**：`[]*Dog` 不能传给 `[]*Animal` 参数，要用类型参数或手动转换。
- **把泛型方法写进接口，或期望它出现在反射中**：编译器不允许前者，反射看不到后者。
- **约束写成 `[]E` 而不是 `S ~[]E`**：返回值丢失了调用方的具名 slice 类型。
- **在 functional options 里吞掉错误**：选项校验失败时应该让构造函数返回 error，而不是 panic 或静默忽略。
- **在泛型代码中对 `T` 做 `==` 比较但约束写的是 `any`**：编译不过，应改成 `comparable`。

## 📚 一手资料

- Go spec：[Type parameter declarations](https://go.dev/ref/spec#Type_parameter_declarations)、[Interface types（类型集）](https://go.dev/ref/spec#Interface_types)、[Type inference](https://go.dev/ref/spec#Type_inference)、[Method declarations](https://go.dev/ref/spec#Method_declarations)
- Go 1.27 Release Notes「Changes to the language」：泛型方法（issue #77273）、函数类型推断（issue #77245）
- 设计文档：Generics implementation - GC Shape Stenciling、Dictionaries（go.googlesource.com/proposal 的 design 目录）
- 官方博客：An Introduction To Generics、When To Use Generics、Generic interfaces
- 源码：`cmd/compile/internal/noder/reader.go`、`cmd/compile/internal/types2/infer.go`、`math/rand/v2/rand.go`
- Java：JLS §4.6（类型擦除）、`javap -c -v` 观察桥方法、[JEP 513](https://openjdk.org/jeps/513)、《Effective Java》第 5 章（泛型）
