# Ch03 · 接口、动态派发与组合

> Part 1 · 估时 12h · 状态：⚪ 未开始 · 从 itab 到 vtable：同样是"多态"，两个运行时付出的代价和优化手段完全不同；没有继承时如何组织一个游戏服的领域模型

## 🎯 目标（资深标准）

- **L1**：用小接口 + 结构体嵌入组织代码；正确使用类型断言和 type switch；避开"持有 nil 指针的非 nil 接口"。
- **L2**：讲清 `iface`/`eface` 的内存表示、itab 的生成与缓存、接口方法调用的指令路径、值装箱进接口何时分配；讲清 HotSpot 的 vtable/itable、CHA、内联缓存（单态/双态/超多态）以及去优化。
- **L3**：用 benchmark 和 `-gcflags=-m` 量化接口调用相比直接调用的代价（含内联损失和装箱分配），并用编译器去虚拟化或改写把热路径的代价消除。
- **L4**：为"技能 / Buff / 消息处理器"体系设计类型结构：Go 的接口 + 嵌入 + type switch，对比 Java 25 的 sealed + record + 模式匹配，论证扩展性、穷尽性检查和性能。

## 🔗 前置

[Ch01](../ch01-values-memory/)、[Ch02](../ch02-slice-map-string/)

## 💡 关键洞察

1. **Go 接口是"类型指针 + 数据指针"两个字，方法表（itab）在运行时按（接口, 具体类型）对生成并缓存。** 没有类层次结构，所以也就没有 CHA。去虚拟化只能靠编译器在编译期看清具体类型，或者靠 PGO。
2. **Java 的虚调用通常"免费"，是因为 JIT 在运行时投机。** 单态调用点被内联，类加载改变层次结构时再去优化。Go 没有运行时投机：编译期没看清的接口调用，就一直是间接调用，并且挡住了内联和逃逸分析。
3. **嵌入是组合，不是继承。** 提升出来的方法，其接收者仍然是被嵌入的那个字段，不存在 override 和 `super` 的动态绑定。想用"模板方法模式"的 Java 思路去套嵌入，一定会出问题。

## ☕ Java 25 深度复习

### 虚调用的实现
- **invokevirtual**：经由对象 klass 的 **vtable** 按固定下标查找，代价是一次加载加一次间接跳转。
- **invokeinterface**：类可以实现任意多个接口，方法在 itable 中的位置不固定，需要先找到对应接口的 itable 段再取方法，比 vtable 慢。HotSpot 用 itable stub 加速（HotSpot `src/hotspot/share/oops/` 中的 klassVtable 相关代码）。
- **CHA（类层次分析）**：如果加载的类里某个方法只有一个实现，C2 就直接内联，并登记一个依赖（HotSpot `src/hotspot/share/code/` 的 dependencies）；之后加载的新子类打破这个假设时，触发**去优化**，回到解释执行再重新编译。
- **内联缓存与类型 profile**：解释器和 C1 收集调用点的接收者类型。C2 对单态调用点直接内联，对双态调用点做"类型判断 + 两路内联"（`TypeProfileWidth` 本机默认值 2），超多态调用点才退化为 vtable/itable 调用。所以**游戏服 Java 代码的性能对调用点的多态程度很敏感**。
- **sealed + record + 模式匹配**（JEP 409/395/440/441，已定稿）：`switch` 对 sealed 层次结构做**穷尽性检查**；类型 switch 经 invokedynamic 由 `java/lang/runtime/SwitchBootstraps.java` 引导；如果 sealed 层次在分离编译时发生变化，运行时会抛 `MatchException`。
- **JEP 507 原始类型模式（Java 25 第三次预览）**：`instanceof` 和 `switch` 可以匹配 `int`、`byte` 等原始类型，并带安全收窄检查（例如 `x instanceof byte b` 只有在值可以无损转换时才匹配）。需要 `--enable-preview`。
- **默认方法与菱形问题**：接口默认方法冲突时必须显式 override。Go 的嵌入冲突规则（同一深度的同名成员）是另一套机制。

## 🐹 Go 1.27 深入

### 规范层
- **隐式实现**：类型的方法集包含接口的全部方法，就实现了这个接口，不需要声明。结合 [Ch01](../ch01-values-memory/) 的方法集规则：只有指针接收者方法时，只有 `*T` 实现接口。
- **接口值的比较**：两个接口值相等，要求动态类型相同且动态值相等。**动态类型不可比较时（例如 slice），运行期直接 panic**。拿接口做 map key 时尤其要小心。
- **类型断言与 type switch**：`v, ok := x.(T)`；`switch v := x.(type)`。**没有穷尽性检查**，对"封闭类型集合"只能靠约定（例如带一个未导出方法的标记接口）加第三方 linter。
- **嵌入**（spec「Struct types」「Selectors」）：嵌入字段的字段和方法被**提升**。选择器解析遵循"最浅深度优先"，同一深度出现多个同名成员时属于歧义，只有在真正使用时才报错。可以嵌入接口类型（未实现的方法调用时会因为 nil 而 panic），也可以嵌入指针类型。
- **1.27 新增：结构体字面量的 key 可以是提升字段。** `type Mid struct{ Inner }; type Outer struct{ Mid; Y int }` 之后，可以直接写 `Outer{X: 1, Y: 2}`（X 来自 `Inner`）。本机 go1.27.3 实测的边界规则：
  - 不能同时指定提升字段和包含它的嵌入字段（`Outer{Mid: Mid{}, X: 4}` 报 "cannot specify promoted field X and enclosing embedded field Mid"）；
  - 不能经由嵌入的**指针**隐式解引用（`struct{ *Inner }{X: 5}` 报 "invalid implicit pointer indirection"）；
  - key 只能写字段名，不能写 `Mid.Inner.X` 这样的完整路径；
  - 模块 `go` 行低于 1.27 时报 "requires go1.27 or later"。
- **接口嵌入接口**：`type ReadWriter interface{ Reader; Writer }`，方法集取并集，重复的同名同签名方法允许存在。

### 运行时与编译器层
- **表示**（`runtime/runtime2.go`）：`eface{ _type, data }` 表示 `any`；`iface{ tab *itab, data }` 表示非空接口。itab 的定义在 `internal/abi/iface.go`：`ITab{ Inter, Type, Hash, Fun [1]uintptr }`，`Fun` 是变长的方法指针数组，`Fun[0] == 0` 表示该类型不实现这个接口。
- **itab 的来源**：编译期能确定的（具体类型赋值给接口）由编译器静态生成；运行期的断言和转换由 `getitab` 查全局 itab 表，没有就构造并通过 `itabAdd` 加入，启动时 `itabsinit` 载入各模块的静态 itab。
- **断言缓存**：`typeAssert` 和 `interfaceSwitch` 带调用点级缓存（`internal/abi/switch.go` 的 `TypeAssertCache`、`InterfaceSwitchCache`），重复的断言不必每次都查全局表。
- **装箱何时分配**：值放进接口时，如果不是指针形状，就需要把值放到堆上（`convT`、`convTnoptr`，以及 `convT64`/`convTstring` 等专用版本）。也有例外：小整数（< 256）会指向静态数组 `staticuint64s`；空字符串、nil slice 指向共享的零值区（`zeroVal`）。这就是 `fmt.Println(x)`、`any` 参数在热路径上产生分配的根源。
- **调用路径**：`x.M()` 编译为：从 `tab.Fun[i]` 取函数指针，把 `data` 作为接收者，间接调用。代价本身不大，**真正的代价是挡住了内联**，进而让参数因为无法被分析而逃逸。
- **去虚拟化**：`cmd/compile/internal/devirtualize/devirtualize.go` 在编译期能证明接口的具体类型时（例如刚刚赋值过），把接口调用改写为直接调用；`devirtualize/pgo.go` 根据 PGO profile 对热点调用点插入"if 具体类型是 T 就直接调用，否则走接口"（[Ch13](../ch13-alloc-escape-compiler/)）。

### 源码阅读清单（`$GOROOT/src`）
| 文件 | 函数 / 类型 | 带着什么问题读 |
|---|---|---|
| `runtime/runtime2.go` · `internal/abi/iface.go` | `iface`、`eface`、`ITab` | 为什么 `ITab` 里要冗余一份类型 `Hash`？`Fun[0] == 0` 用在哪里？ |
| `runtime/iface.go` | `getitab`、`itabAdd`、`itabsinit`、`typeAssert`、`interfaceSwitch` | 全局 itab 表如何做到无锁读？断言失败的结果也会被缓存吗？ |
| `runtime/iface.go` | `convT`、`convTnoptr`、`convT64`、`convTstring`、`staticuint64s` | 哪些值装箱不分配？为什么是 256？ |
| `internal/abi/switch.go` | `TypeAssertCache`、`InterfaceSwitchCache` | 调用点缓存如何扩容？多线程同时更新时为什么安全？ |
| `cmd/compile/internal/devirtualize/devirtualize.go` | 静态去虚拟化 | 编译器需要满足什么条件才敢把接口调用改成直接调用？ |
| `cmd/compile/internal/devirtualize/pgo.go` | PGO 去虚拟化 | 它生成的"守卫 + 直接调用"和 HotSpot 的双态内联有什么相同和不同？ |

## 📊 Java 25 ↔ Go 1.27 机制级对照表

| 维度 | Java 25（HotSpot） | Go 1.27 | 取舍 / 游戏服影响 |
|---|---|---|---|
| 实现关系 | 名义类型，显式 `implements` | 结构化类型，隐式满足 | Go 可以在使用方定义接口，便于 mock 和解耦；代价是"谁实现了它"要靠工具查（gopls） |
| 方法表 | 每个类一份 vtable / itable，类加载时生成 | 每个（接口, 类型）对一份 itab，编译期或运行期生成并缓存 | Go 没有继承层次，所以没有 vtable 下标继承 |
| 接口引用的大小 | 一个引用（4/8 字节） | 两个字（16 字节） | Go 的 `[]Shape` 每元素 16 字节，而且每个元素指向一个独立的值 |
| 去虚拟化依据 | 运行时类型 profile + CHA，可以去优化 | 编译期静态证明 + PGO 守卫，不能去优化 | Java 长时间运行后多态开销接近零；Go 的热点接口调用要靠 PGO 或手工改写 |
| 内联 | 单态 / 双态调用点被投机内联 | 未去虚拟化的接口调用不能内联 | Go 热路径上的小接口方法（如 `Len()`）可能成为瓶颈 |
| 装箱 | 原始类型装箱有缓存（`Integer.valueOf` 默认 -128~127） | 非指针值进接口时分配（< 256 的整数、空字符串等少数情况例外） | 两边的热路径都要避开 `Object` / `any` 参数 |
| 封闭类型集合 | sealed + `switch` 穷尽性检查 | 无；靠标记接口 + linter | Java 在协议消息 / 指令集建模上更安全 |
| 组合复用 | 继承 + 默认方法 | 嵌入（字段和方法提升，没有 override） | Go 不能实现"父类调用被子类重写的钩子"，要改成显式注入函数或接口 |
| nil 语义 | 引用为 null 就是 null | 接口为 nil 要求类型和值都是 nil | Go 返回 `error` 时的经典 bug（见常见坑） |

## 🔬 实验

### 实验 1：接口调用的真实代价（必做）
- **目的**：区分"间接调用本身"和"失去内联"两部分代价。
- **方法**：`go/lab/dispatch`：同一个 `Damage(int) int` 方法，分别通过①直接调用、②接口调用（单一实现）、③接口调用（4 种实现轮换，即超多态）、④ type switch 分发、⑤函数值调用。
- **工具**：`go test -bench=. -count=10 | benchstat`；`go build -gcflags='-m=2'` 看哪些调用被内联、哪些被去虚拟化。
- **预期现象**：①和被去虚拟化的②最快；③明显变慢；④在类型少时接近直接调用。
- **记录**：每种方式的 ns/op，以及 `-m` 输出中对应调用点的结论。

### 实验 2：JIT 如何看待同一份代码（必做）
- **目的**：观察 HotSpot 的投机内联和去优化。
- **方法**：`java/DispatchLab.java`：先只用一种实现跑热，再引入第二种、第三种实现。
- **工具**：`bin/jrun -XX:+UnlockDiagnosticVMOptions -XX:+PrintCompilation -XX:+PrintInlining java/DispatchLab.java`，查看 `made not entrant`（去优化）和内联决策。
- **预期现象**：单态阶段方法被内联；引入新类型后出现去优化和重新编译；超多态后性能下降。
- **记录**：三个阶段的吞吐，以及对应的编译日志片段。

### 实验 3：装箱分配
- **目的**：量化 `any` 参数的成本。
- **方法**：日志 / 埋点函数分别写成 `func Log(args ...any)` 和强类型版本（或 `slog.Attr`），在循环里传 int（< 256 和 ≥ 256 两组）、string、struct。
- **工具**：`b.ReportAllocs()`、`-gcflags=-m` 看 `escapes to heap`。
- **预期现象**：≥ 256 的整数、非空字符串、struct 都产生分配；< 256 的整数不分配。

## 🧪 练习规划

**Go：`go/exercises`**（`go test ./ch03-interfaces/go/...`）
1. `type Skill interface{ ID() int32; Cast(*Battle, *Unit) error }`，实现 `Fireball`、`Heal`、`Shield` 三种技能；`type BaseSkill struct{ id int32; cd time.Duration }` 通过嵌入复用 `ID()` 和冷却逻辑。测试检查方法提升，以及"BaseSkill 无法回调外层类型方法"（需要显式注入）。
2. `func FindError(err error) *GameError`：处理**持有 nil 指针的非 nil 接口**。测试中会构造一个返回 `(*GameError)(nil)` 的函数，要求调用方判断正确，并写出修正后的返回方式。
3. `func Dispatch(msg Message) (Reply, error)`：用 type switch 分发协议消息，消息类型集合用"未导出方法的标记接口"封闭。测试覆盖每种消息和未知消息。
4. `type Writer interface{ Write([]byte) (int, error) }` 的装饰器链：`CountingWriter`、`LimitWriter` 通过嵌入组合，套在 `bytes.Buffer` 上。测试验证组合顺序和 `io.Writer` 的兼容性。
5. 1.27 语法练习：`NewHero` 构造一个三层嵌入的 `Hero`，字面量直接写提升字段（如 `Hero{HP: 100, Speed: 5}`）。测试在 `go 1.27.0` 的模块下运行；在 README 里回答它和"写完整嵌套字面量"有什么区别，以及什么时候会被禁止。

**Java：`java/drills/DispatchDrill.java`**
- `sealed interface Message permits Login, Move, Chat` + record。用模式匹配 `switch` 实现 `handle`；自校验覆盖每种消息。在注释里写出：新增一个 permits 子类型而不改 switch 时，编译器会报什么。
- `primitive patterns`（preview）：`switch (opcode)` 用 `case byte b when ...` 做安全收窄，对比 Go 里手写范围检查。

## 🎮 游戏服落地

- **消息处理器注册表**：`map[MsgID]Handler`，其中 `type Handler interface{ Handle(*Session, []byte) error }` 或者直接用函数类型 `HandlerFunc`。热路径上函数值调用和接口调用的代价相近，都无法内联。Java 侧常见的是 `Map<Integer, Handler>` 加注解扫描，JIT 会对超多态调用点做 itable 调用。
- **技能 / Buff 系统**：Java 习惯是抽象基类 + 模板方法（`onApply` → 子类重写）。Go 的做法是接口定义行为，嵌入复用数据和通用逻辑，**需要回调的钩子显式作为接口或函数字段注入**。不要试图用嵌入模拟 `super`。
- **协议消息建模**：Java 25 用 sealed + record + `switch` 穷尽性检查，加一个消息类型时编译器会把所有遗漏的处理点找出来。Go 用标记接口 + type switch，必须配合 linter 或测试覆盖来补上穷尽性。这是 Java 明显占优的地方。
- **Mock 与测试**：Go 在使用方定义的小接口（例如逻辑服只依赖 `type PlayerStore interface{ Load(id) (*Player, error) }`），测试中直接写一个假实现，不需要 Mockito。

## ✅ 验收答辩题

- [ ] [L2] 画出 `var w io.Writer = &bytes.Buffer{}` 之后 `w` 的内存表示，包括 itab 中的各个字段。
- [ ] [L2] `getitab` 什么时候被调用？全局 itab 表是怎么做到读操作无锁的？
- [ ] [L2] 为什么 `var e *MyErr = nil; var err error = e; err != nil` 为 true？在 `iface` 层面解释。
- [ ] [L2] HotSpot 的 invokevirtual 和 invokeinterface 在实现上有什么区别？CHA 和去优化是怎么配合的？
- [ ] [L2] 嵌入字段的方法被提升后，它的接收者是谁？为什么 Go 无法实现模板方法模式里的"父类调用子类重写"？
- [ ] [L2] 1.27 允许在结构体字面量中直接写提升字段，说出它的三条限制，并解释"不能经由嵌入指针"这条限制的理由。
- [ ] [L3] 拿出实验 1 的数据：接口调用比直接调用慢多少？其中多少来自失去内联？`-gcflags=-m` 中的哪一行能证明？
- [ ] [L3] 某个热路径函数的 allocs/op 一直不为 0，pprof 指向 `runtime.convT`。你会怎样定位和修复？
- [ ] [L3] 拿出实验 2 的编译日志，指出 HotSpot 何时内联、何时去优化，以及超多态之后性能下降了多少。
- [ ] [L4] 为一个有 200 种技能的 MMO 战斗系统设计类型结构，分别给出 Go 和 Java 25 的方案，比较扩展性、穷尽性保障和运行时性能。
- [ ] [L4] 为什么说"接口应该由使用方定义"？在一个包含网关、逻辑服、存储层的游戏服仓库里，接口应该放在哪些包里？

## 🕳️ 常见坑

- **返回具体类型的 nil 指针作为 `error`**：调用方 `err != nil` 永远为 true。正确做法是直接 `return nil`。
- **接口定义在实现方并且很大**（`IPlayerService` 带 30 个方法），这是 Java 习惯；Go 里应该在使用方定义只含所需方法的小接口。
- **用嵌入模拟继承**，期望"父类"方法调用到"子类"的同名方法，实际调用的永远是被嵌入类型自己的方法。
- **拿包含 slice 的类型作为接口值放进 map key 或做 `==` 比较**：运行期 panic。
- **热路径传 `any`**（日志、埋点、事件总线），每次调用都在分配。
- **值类型只实现了指针接收者的方法**，赋给接口时编译报错，然后到处加 `&` 修修补补。应该一开始就统一接收者。
- **以为 type switch 有穷尽性检查**：新增消息类型后，旧的 switch 默默落进 `default`。
- **嵌入接口来"部分实现"一个接口**，忘了实现的方法在运行时因为 nil 接收者而 panic。

## 📚 一手资料

- Go spec：[Interface types](https://go.dev/ref/spec#Interface_types)、[Struct types](https://go.dev/ref/spec#Struct_types)、[Selectors](https://go.dev/ref/spec#Selectors)、[Type assertions](https://go.dev/ref/spec#Type_assertions)、[Type switches](https://go.dev/ref/spec#Type_switches)、[Composite literals](https://go.dev/ref/spec#Composite_literals)
- Go 1.27 Release Notes「Changes to the language」中关于结构体字面量 key 的说明（issue #9859）
- 源码：`runtime/iface.go`、`internal/abi/iface.go`、`internal/abi/switch.go`、`cmd/compile/internal/devirtualize/`
- Effective Go：Interfaces and other types、Embedding
- Java：[JEP 409](https://openjdk.org/jeps/409)、[JEP 440](https://openjdk.org/jeps/440)、[JEP 441](https://openjdk.org/jeps/441)、[JEP 507](https://openjdk.org/jeps/507)、`java/lang/runtime/SwitchBootstraps.java`、HotSpot `src/hotspot/share/oops/`、`src/hotspot/share/code/`
