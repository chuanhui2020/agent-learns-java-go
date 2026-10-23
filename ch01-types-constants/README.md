# Ch01 · 类型系统、常量与数值语义

> Part 1 · 估时 8h · 状态：⚪ 未开始 · Java 老手对 Go 类型系统的直觉错误，集中在"类型同一性 / 可赋值性 / 常量 / 数值转换"四处。本章把它们钉在规范条款和 types2 源码上，[Ch02](../ch02-values-memory/)、[Ch05](../ch05-interfaces/)、[Ch06](../ch06-generics/) 都引用这里的概念

## 🎯 目标（资深标准）

- **L1**：正确使用类型定义与类型别名（含泛型别名）、`iota`、显式数值转换；能准确说出规范对整数溢出、截断除法、移位计数、浮点转整数、FMA 融合的规定，以及 Java 在同一点上的规定。
- **L2**：讲清 types2 如何判定类型同一性、可赋值性、可转换性（`Identical`、`(*operand).assignableTo`、`(*operand).convertibleTo`）；讲清 `go/constant` 怎样用 `int64Val`/`intVal`/`ratVal`/`floatVal` 表示任意精度常量，常量表达式在哪一层被折叠成值；讲清 javac 怎样实现 JLS §5.2 的常量窄化（`Types.isAssignable`），以及 JEP 507 的 `instanceof` 怎样被改写成 `ExactConversionsSupport` 调用。
- **L3**：拿到一段代码，能先**预测**它是编译错误、运行期 panic、静默回绕还是"实现相关"，并写出具体输出；再用 `go build -gcflags=-e`、`go vet`、`go tool objdump`、`javap` 证实。能预测同一表达式在 Java 里的结果（移位掩码、`(int)NaN`、装箱 `==`、常量内联后的陈旧值）。
- **L4**：能论述无类型常量、禁止隐式数值转换、确定性回绕溢出、类型别名这四个设计的动机和被否决的替代方案（引用 Go FAQ、《Constants》博客、类型别名设计文档、#19623），并对照 Java 的编译期常量内联、原始类型/装箱二元体系和 JEP 507。

## 🔗 前置

[Ch00](../ch00-setup/)（语法速通、`go vet`、`javap`、`--enable-preview`）

## 💡 关键洞察

1. **Go 的类型同一性是"名字优先、结构兜底"。** 命名类型（预声明类型、定义类型、类型参数）只和自己相同；类型字面量按结构比较。所以 `type IDs []int` 和 `[]int` 是不同类型但可以互相赋值（有一方没有名字）；`type A int` 和 `type B int` 不能赋值，但可以转换。Java 的类和接口是纯名义的，原始类型之间则有一套隐式拓宽关系。
2. **Go 的常量是数学意义上的数，不是"带类型的只读变量"。** 无类型常量在编译器里精确表示（`big.Int`/`big.Rat`），只在落到一个有类型的位置时才做一次**可表示性**检查，放不下就是编译错误；而变量运算按位宽回绕，永不 panic。Java 的编译期常量从字面量开始就有类型（`1` 是 `int`，`1.0` 是 `double`），按目标类型的二补码 / IEEE 语义折叠，然后内联进使用方的 class 文件。
3. **Go 没有隐式数值转换，但显式转换本身不做检查。** `int8(x)` 截断，`int64(NaN)` 的结果实现相关，`x << 64 == 0`。Java 有拓宽、窄化、数值提升、装箱这几套隐式规则，但转换结果全部由规范定义（`(int)NaN == 0`，移位计数取模）。JEP 507 把"这个值能不能无损转换"做成了运行期的 `instanceof` 测试；Go 只在常量上做这件事，变量要自己写往返比较。

## ☕ Java 25 深度复习

### 原始类型与转换（JLS 第 4、5 章）
- **§4.2.2 整数运算**：原文 "The integer operators do not indicate overflow or underflow in any way."，只有 `/` 和 `%` 在除数为 0 时抛 `ArithmeticException`；`Integer.MIN_VALUE / -1 == MIN_VALUE`，`% -1 == 0`（本机实测）。
- **§5.1.2 拓宽原始类型转换**：`int→float`、`long→float`、`long→double` 会丢精度，但仍然算"拓宽"，在赋值和调用里**隐式**发生。实测 `float f = 16777217;` 得到 `1.6777216E7`，`(long)(double)((1L<<53)+1)` 和原值不等。
- **§5.1.3 窄化原始类型转换**：浮点转整数时，NaN 变成 0，超出范围的值饱和到 MIN/MAX；转 `byte`/`short`/`char` 时先转到 `int`，再截断低位。所以 `(int)1e20 == Integer.MAX_VALUE`、`(long)NaN == 0`、`(byte)1e20 == -1`（先饱和成 `0x7fffffff`，再取低 8 位）、`(byte)300 == 44`。
- **§5.1.7 装箱 / §5.1.8 拆箱**：规范只保证常量表达式装箱后 `true`/`false`、`'\u0000'..'\u007f'`、`-128..127` 的 `==` 成立，并称之为 "a pragmatic compromise"，其他值的身份不做保证。拆箱 `null` 会抛 NPE（实测消息为 `Cannot invoke "java.lang.Integer.intValue()" because "<parameter1>" is null`）。
- **§5.2 赋值上下文**：常量表达式可以隐式窄化到 `byte`/`short`/`char`（及其包装类），前提是值可表示（`byte b = 100;`）。**这就是 Java 版的"可表示性"**，但只在赋值上下文生效，§5.3 调用上下文不允许（`m(byte)` 不接受 `m(100)`）。javac 的实现在 `Types.isAssignable`：常量值先过 `checkRange`，再回落到 `isConvertible`。
- **§5.6 数值上下文（数值提升）**：`byte`/`short`/`char` 参与运算前先提升为 `int`。`long r = a * b`（`a`、`b` 都是 `int`）的字节码是 `imul; i2l`，先溢出再拓宽，实测 `100000 * 100000` 得到 `1410065408`。
- **§15.26.2 复合赋值**：`E1 op= E2` 隐含一次强转回 `E1` 的类型，所以 `byte b = 10; b += 300;` 能编译，结果是 54。Go 里 `b += 300` 直接报 `300 (untyped int constant) overflows byte`。
- **§15.19 移位**：`int` 只取计数的低 5 位，`long` 只取低 6 位。实测 `1 << 32 == 1`、`1L << 64 == 1`、`1 << -1 == Integer.MIN_VALUE`，和 JVM 字节码 `ishl`/`lshl` 的定义一致。
- **§15.17.2 / §15.17.3**：除法向零截断，余数符号跟被除数（`-7/2 == -3`、`-7%2 == -1`）；向下取整用 `Math.floorDiv`/`floorMod`（得到 -4、1）。
- **§4.2.3 / §4.2.4 浮点**：IEEE 754 binary32/binary64。JEP 306 之后所有浮点表达式都按严格语义求值，`strictfp` 已经多余（JDK 25 的 `javac -Xlint` 报：`as of release 17, all floating-point expressions are evaluated strictly and 'strictfp' is not required`）。严格语义意味着**不允许隐式融合 `x*y+z`**，需要 FMA 只能显式调用 `Math.fma`。
- **两套相等性**：`==` 对 `double` 按 IEEE 比较（`NaN != NaN`，`0.0 == -0.0`）；`Double.equals` 比较 `doubleToLongBits`（NaN 等于自己，`0.0` 不等于 `-0.0`）。所以 `HashMap<Double, V>` 的键语义和 `==` 不一致。

### 编译期常量（§4.12.4、§15.29、§13.1、§13.4.9）
- **constant variable**：用常量表达式初始化的 `final` 原始类型或 `String` 变量（§4.12.4）。`static final Integer X = 100` 和 `static final int Y = Integer.parseInt("7")` 都**不是**常量变量。
- **§13.1 要求对常量变量的引用在编译期解析成值**，class 文件里不出现对这个字段的引用。`javap -c` 实测：`int`/`long`/`String` 常量分别变成 `bipush 100`、`ldc2_w`、`ldc "ab"`，`Integer` 字段和 `parseInt` 的结果走 `getstatic`；声明方字段带 `ConstantValue` 属性。
- **§13.4.9 "inconstant constants"**：只重编声明方、不重编使用方时，使用方继续读到旧值（实测 `LIMIT` 从 100 改成 200 后仍打印 100，而 `BOXED` 读到了 200）。JLS 自己的建议是："we recommend that source code make very sparing use of static constant variables"。
- **javac 实现**：`jdk.compiler/com/sun/tools/javac/comp/ConstFold.java`（`fold1`、`fold2`）按操作数类型的运行期语义折叠，所以 Java 里常量表达式 `0.1 + 0.2 == 0.3` 和运行期一样是 `false`。

### 装箱缓存与溢出检测（JDK 源码）
- `java/lang/Integer.java` 的 `IntegerCache`：`low = -128`；`high` 默认 127，读取 `VM.getSavedProperty("java.lang.Integer.IntegerCache.high")` 后取 `max(值, 127)`；先尝试用 `CDS.initializeFromArchive` 加载 CDS 归档里的缓存。`Integer.valueOf(int)` 标注了 `@IntrinsicCandidate`。
- `-XX:AutoBoxCacheMax`（C2 product flag，本机默认 128）：实测 `-XX:AutoBoxCacheMax=1000` 和 `-Djava.lang.Integer.IntegerCache.high=1000` 都让 `Integer` 的 `128 == 128` 变成 `true`。`LongCache` 固定为 -128..127，`CharacterCache` 固定为 0..127，都不读这个属性。
- C2 的 `EliminateAutoBox`（默认 `true`）能消掉不逃逸的装箱/拆箱对，实现在 HotSpot `src/hotspot/share/opto/`。它不改变语义，只影响分配。
- `java/lang/Math.java`：`addExact`/`subtractExact`/`multiplyExact`/`toIntExact`/`absExact` 溢出时抛 `ArithmeticException("integer overflow")`，其中 `addExact` 等带 `@IntrinsicCandidate`；另有 `multiplyHigh`、`unsignedMultiplyHigh`、`floorDiv`/`floorMod`/`ceilDiv`、`clamp`。Java 没有无符号类型，靠 `Integer.compareUnsigned`/`divideUnsigned`/`toUnsignedLong` 这类方法补。

### JEP 507：原始类型模式（JDK 25 第三次预览，与第二次预览相比没有变化）
- **语义**：`instanceof` 和 `switch` 的模式可以是原始类型。**匹配成功的条件是值能"精确"转换**（no loss of information）。某些类型对是 unconditionally exact 的，不需要运行期测试：整数类型之间的拓宽、`float→double`、`byte/short/char→浮点`、`int→double`，以及装箱。
- **本机实测边界**（`javac --release 25 --enable-preview`）：`1000 instanceof byte` 为 false，`100` 为 true；`16777216 instanceof float` 为 true，`16777217` 为 false，`Integer.MAX_VALUE` 也为 false；`1.0 instanceof int` 为 true，`1.5` 和 `-0.0` 为 false；`NaN` 和 `-0.0` 对 `float` 为 true；`1e20 instanceof long` 和 `Long.MAX_VALUE instanceof double` 都为 false。
- **switch**：选择子可以是 `long`/`float`/`double`/`boolean`；`case` 常量必须与选择子同类型（`float` 选择子不能写 `case 0`）；同时列出 `true` 和 `false` 的 `boolean` switch 视为穷尽。`switch (double)` 依次写 `case int i`、`case float f`、`case double x` 时，`-0.0` 落到 `float` 分支。
- **实现**：javac 的 `comp/Lower.java`（`visitTypeTest` → `getExactnessCheck`）把 `x instanceof byte` 改写成 `ExactConversionsSupport.isIntToByteExact(x)`（`javap` 可见 `invokestatic`）。`switch` 走 `invokedynamic` + `SwitchBootstraps.typeSwitch`，调用点描述符直接是原始类型，例如 `(JI)I`、`(DI)I`。preview class 文件的 `minor version: 65535`。
- **`java/lang/runtime/ExactConversionsSupport.java`**：每个方法都是一次"往返比较"，外加几个边界处理。例如 `isIntToFloatExact` 是 `n == (int)(float)n && n != Integer.MAX_VALUE`（MAX_VALUE 转成 `float` 是 2³¹，再转回 `int` 会饱和成 MAX_VALUE，往返比较会误判成"精确"）；浮点转整数时排除 `-0.0`；`isDoubleToFloatExact` 对 NaN 返回 true。类注释用 `@jls 5.7.1 Exact Testing Conversions`、`@jls 5.7.2` 引用的是 JEP 507 的预览规范，正式的 JLS 25 正文只有 §5.7 Testing Contexts。

## 🐹 Go 1.27 深入

### 规范层（本机 `$GOROOT/doc/go_spec.html`，Language version go1.27）
- **Types**：原文 "Predeclared types (excluding `any`), defined types, and type parameters are called named types. ... All named types are distinct." 别名只有在指向命名类型时才算命名类型，所以 `any`（`interface{}` 的别名）不是命名类型。
- **Underlying types**：预声明的布尔、数值、字符串类型和类型字面量，底层类型是它自己；其他类型取声明右侧那个类型的底层类型。`type B2 B1`、`type B1 string` 的底层类型都是 `string`。
- **Type identity**：命名类型和任何其他类型都不同；其余按结构比较。struct 要求字段名、类型、**tag**、是否嵌入都一致，来自不同包的非导出字段名永远不同；函数类型不比较参数名。
- **Assignability**（逐条读）：类型相同；底层类型相同、都不是类型参数、且**至少一方不是命名类型**；双向 channel 赋给同元素类型的 channel 类型；实现接口；`nil`；**无类型常量可被 T 表示**。另外 1.27 新增一条（标 [Go 1.27]）：泛型函数可以赋给函数类型，缺的类型实参由推断补齐（详见 [Ch06](../ch06-generics/)）。
- **Representability**：浮点类型允许"舍入后不溢出"，按 IEEE round-to-even，**IEEE 负零进一步简化为无符号零**；"constant values never result in an IEEE negative zero, NaN, or infinity"。所以 `var _ float32 = 16777217` 合法（舍入成 16777216），`var _ byte = 1000` 不合法，`42.0` 可以赋给 `byte`，`-1e-1000` 变成 `0.0`。
- **Type declarations**：别名声明 `type T = U`，可以带类型参数（泛型别名 `type set[P comparable] = map[P]bool`），但右侧不能是本声明里的类型参数。类型定义 `type T U` 产生新的定义类型，**不继承** U 上声明的方法，但接口类型的方法集、复合类型元素的方法集保持不变（规范里 `NewMutex`/`PtrMutex`/`PrintableMutex` 三个例子，接收者与方法集在 [Ch05](../ch05-interfaces/) 展开）。
- **Numeric types**：`byte` 是 `uint8` 的别名，`rune` 是 `int32` 的别名；`int`/`uint` "either 32 or 64 bits"，`uintptr` 足以容纳指针的位；除这两个别名外，所有数值类型都是互不相同的命名类型，即使 `int` 和 `int64` 一样宽也必须显式转换。
- **Constants**：数值常量精确、任意精度、不溢出。默认类型依次是 `bool`、`rune`、`int`、`float64`、`complex128`、`string`。Implementation restriction：整数常量至少 256 位；浮点常量尾数至少 256 位、指数至少 16 位；整数放不下要报错，浮点因精度限制要舍入。
- **Constant declarations / Iota**：括号里省略表达式列表等于**文本重复**上一个非空列表**及其类型**（所以 `Read Perm = 1 << iota` 后面的 `Write` 也是 `Perm`）；`iota` 是该 ConstSpec 在声明里的下标，同一行多次使用值相同；`_` 可以跳值。
- **Constant expressions**：无类型操作数的种类不同时，结果取 integer < rune < floating-point < complex 中靠后的那一种；整数常量除法截断（`const Θ float64 = 3/2` 等于 1.0）；比较结果永远是**无类型布尔**（所以 `var b MyBool = x == y` 合法）；`^1` 的掩码对无类型常量是 -1（`uint8(^1)` 非法）；常量除数不能为 0。
- **Operators（移位）**：非常量移位的左操作数如果是无类型常量，它的类型取"把整个移位表达式换成左操作数本身时"会得到的类型。所以 `var j int32 = 1 << s` 合法（结果为 0），`var f float64 = 1 << s` 非法（`shifted operand 1 (type float64) must be integer`）。
- **Integer operators / Integer overflow**：截断除法；`MinInt / -1 == MinInt`；运行期除数为 0 触发 panic；移位计数必须非负（运行期为负则 panic），**没有上限**，按"移 n 次"定义，所以 `uint64(1) << 64 == 0`、`int64(-8) >> 100 == -1`。无符号运算模 2ⁿ；有符号溢出"deterministically defined"，"A compiler may not optimize code under the assumption that overflow does not occur"。
- **Floating-point operators**：实现**可以**把多个浮点运算融合成一个（可以跨语句），例如 FMA；显式的 `float64(x*y)` 转换会强制舍入，从而阻止融合。
- **Conversions**：常量转换要求可表示，结果是类型化常量（`int(1.2)`、`uint(-1)`、`string(65.0)` 非法）。非常量转换：整数之间先符号扩展或零扩展到无限精度，再截断（`uint32(int8(uint16(0x10F0))) == 0xFFFFFFF0`），"there is no indication of overflow"；浮点转整数向零截断；**涉及浮点的非常量转换如果目标类型放不下，结果 implementation-dependent**。string 与 `[]byte`/`[]rune` 的转换细节放在 [Ch03](../ch03-slice-map-string/)；slice 转数组或数组指针时，长度不足会 panic；`unsafe.Pointer` 的转换规则见 [Ch09](../ch09-reflect-unsafe/)。
- **Allocation**：`new` 接受表达式，`new(123)` 返回 `*int`（无类型常量先取默认类型）。

### 编译器与运行时层
- **类型检查器**：编译器用 `cmd/compile/internal/types2`；`go/types` 是由它生成的镜像（`api_predicates.go`、`const.go`、`conversions.go`、`operand.go`、`predicates.go` 的文件头都是 `Code generated by "go test -run=Generate -write=all"; DO NOT EDIT.`），所以用 `go/types` 写的实验程序和编译器的判定一致。
- **同一性**：`predicates.go` 的 `(*comparer).identical` 先对两边做 `Unalias`，再按 `*Basic`/`*Array`/…/`*Named` 分支比较。`*Basic` 比较 `kind`，因为 `byte`/`rune` 和 `uint8`/`int32` 是不同的 `*Basic` 对象（`universe.go` 里有单独的 `universeByte`/`universeRune`）。`*Named` 要求类型实参逐个相同，并且 `identicalOrigin` 来自同一个声明，这就是名义判定。规范里的"有名字"在实现里叫 `hasName`（`*Basic`、`*Named`、`*TypeParam`）。
- **别名**：`alias.go` 的 `Alias` 是类型图里的一个独立节点（有 `obj`、`fromRHS`、`actual`，泛型别名还有 `tparams`/`targs`），`Unalias` 一路解到非别名类型。`universe.go` 里 `any` 就是 `NewAlias(obj, &emptyInterface)`。所以 `fmt.Printf("%T")` 打印泛型别名 `Set[int]` 的结果是 `map[int]struct {}`。
- **可赋值**：`operand.go` 的 `(*operand).assignableTo` 的顺序就是规范的顺序：`Identical` → 无类型值走 `implicitTypeAndValue`（`expr.go`，同时算出常量在目标类型下的值）→ `Identical(Vu, Tu) && (!hasName(V) || !hasName(T))` → 接口 → channel → 类型参数。
- **可转换**：`conversions.go` 的 `(*Checker).conversion` 把常量和非常量分开处理。常量走 `constConvertibleTo` → `representableConst`，整数常量溢出时给出简洁报错 `constant 256 overflows uint8`；非常量走 `(*operand).convertibleTo`。
- **常量的表示**：`go/constant/value.go`。整数用 `int64Val`，放不下时换成 `intVal{*big.Int}`；浮点能精确表示成分数时用 `ratVal{*big.Rat}`，分子或分母超过 `maxExp = 4 << 10` 位时才退化成 `floatVal{*big.Float}`，精度 `prec = 512`（注释："The spec requires at least 256 bits; typical implementations use 512 bits"）。**`0.1` 存成精确的 1/10**，所以常量 `0.1 + 0.2 == 0.3` 为 true，而 `float64` 变量相加是 `0.30000000000000004`。
- **常量的边界检查**：`types2/const.go` 的 `(*Checker).overflow` 规定，类型化常量**每做一步运算**都要过 `representable`（所以 `Four * 100` 在中间结果就报错），无类型整数常量位长超过 `prec = 512` 就报 `constant ... overflow`。实测 `1 << 511` 合法，`1 << 512` 报 `constant shift overflow`。浮点用 `roundFloat32`/`roundFloat64` 先舍入再判断是否溢出。
- **无类型表达式的延迟定型**：`types2` 把还没有最终类型的表达式记在 `check.untyped` 里，等上下文确定后由 `updateExprType` 回填类型并检查可表示性。非常量移位左操作数的类型就是这样从赋值目标"倒推"出来的。
- **常量折叠分两层**：① 常量表达式在类型检查阶段由 `go/constant`（`BinaryOp`、`Shift`、`Compare`）求值；`noder/writer.go` 遇到 `tv.Value != nil` 的表达式直接写成 `exprConst`，IR 里根本没有这棵表达式树。仍然无类型的常量由 `noder/helpers.go` 的 `idealType` 补一个具体类型。② 内联、常量传播之后才出现的"运行期常量"由 SSA 规则折叠，例如 `ssa/_gen/generic.rules` 的 `(Add64 (Const64 [c]) (Const64 [d])) => (Const64 [c+d])`。SSA 的其他优化在 [Ch15](../ch15-alloc-escape-compiler/) 展开。
- **运行期数值语义怎么落地（amd64，`go tool objdump` 实测）**：
  - 除法：`ssagen/ssa.go` 的 `(*state).intDivide` 插入零检查，命中时调用 `runtime.panicdivide`（`runtime/panic.go`，错误文本 `integer divide by zero`，panic 机制见 [Ch08](../ch08-errors-panic/)）。`amd64/ssa.go` 在 `ssa.DivisionNeedsFixUp` 成立时为除数 -1 生成 `NEGQ` 的 fixup 分支，因为 `IDIVQ` 在 `MinInt64 / -1` 时会触发硬件异常。
  - 移位：`AMD64.rules` 把 `Lsh64x64` 在 `!shiftIsBounded(v)` 时下降为 `SHLQ` + `CMPQ $64` + `SBBQ` + `ANDQ`。硬件 `SHL` 只看计数的低 6 位，Go 多用三条指令保证 `x << 64 == 0`；写成 `x << (s & 63)` 后 `shiftIsBounded` 成立，只剩一条 `SHLQ`。计数是有符号类型时，还会多一个 `TESTQ` 和到 `runtime.panicshift` 的跳转。
  - FMA：`AMD64.rules` 只在 `buildcfg.GOAMD64 >= 3` 时把 `MULSD`+`ADDSD` 融合成 `VFMADD231SD`；`ARM64.rules` 的融合规则没有这个前提。同一份源码在 `GOAMD64=v1` 和 `v3` 下的结果可以不同（实验 4）。
  - 浮点转整数：直接用 `CVTTSD2SIQ`/`CVTTSD2SIL`。越界或 NaN 时硬件返回 "integer indefinite"（`0x8000…`），所以本机实测 `int64(NaN)` 和 `int64(1e20)` 都是 `MinInt64`，`uint8(-1.5)` 是 255。这就是规范里 "implementation-dependent" 的实际含义，ARM64 上的结果待实测。
- **平台相关大小**：`types2/sizes.go` 的 `gcArchSizes`（如 `"amd64": {8, 8}`、`"386": {4, 4}`）决定 `int`/`uintptr` 的宽度。本机 `unsafe.Sizeof(int(0)) == 8`、`bits.UintSize == 64`、`strconv.IntSize == 64`。`x := 1 << 40` 在 386 上是编译错误。

### 源码阅读清单（`$GOROOT/src`）
| 文件 | 函数 / 类型 | 带着什么问题读 |
|---|---|---|
| `cmd/compile/internal/types2/predicates.go` | `(*comparer).identical`、`hasName`、`identicalOrigin` | `byte` 和 `uint8` 是两个 `*Basic` 对象，为什么判为同一类型？命名类型的同一性依据是什么？ |
| `cmd/compile/internal/types2/api_predicates.go` | `Identical`、`AssignableTo`、`ConvertibleTo` | 导出 API 和 checker 内部方法是什么关系？为什么 `check` 参数可以为 nil？ |
| `cmd/compile/internal/types2/operand.go` | `(*operand).assignableTo` | 规范 Assignability 的每一条分别对应哪几行？无类型值为什么要单独走一条路？ |
| `cmd/compile/internal/types2/conversions.go` | `(*Checker).conversion`、`(*operand).convertibleTo` | 常量转换和非常量转换为什么是两套规则？`string(65)` 是哪个分支放行的？ |
| `cmd/compile/internal/types2/const.go` | `(*Checker).overflow`、`representableConst`、`roundFloat32` | 512 位上限写在哪里？类型化常量为什么每一步都要检查？浮点常量"舍入后可表示"是怎么实现的？ |
| `cmd/compile/internal/types2/expr.go` | `implicitTypeAndValue`、`updateExprType`、`shift`、`binary` | 无类型表达式的类型怎么回填？`division by zero` 和 `negative shift count` 在哪里报？ |
| `cmd/compile/internal/types2/alias.go`、`universe.go` | `Alias`、`Unalias`；`any`、`byte`、`rune` 的定义 | 别名在类型图里是透明的还是一个节点？为什么 `any` 不是命名类型？ |
| `go/constant/value.go` | `int64Val`/`intVal`/`ratVal`/`floatVal`、`prec`、`maxExp`、`BinaryOp`、`Shift` | `0.1` 用什么表示？什么时候从精确分数退化成 `big.Float`？退化之后为什么不再变回去？ |
| `cmd/compile/internal/noder/writer.go`、`helpers.go` | `exprConst` 分支、`idealType` | 常量表达式进入 IR 之前就已经是一个值；仍然无类型的常量会被补成什么类型？ |
| `cmd/compile/internal/ssa/_gen/generic.rules`、`AMD64.rules` | 常量折叠规则、`Lsh64x64` 的下降、FMA 融合条件 | 运行期常量在什么时候折叠？Go 的移位语义在 x86 上要付出什么代价？ |
| `cmd/compile/internal/ssagen/ssa.go`、`amd64/ssa.go` | `(*state).intDivide`、DIVQ 的 -1 fixup | 除零 panic 和 `MinInt / -1` 分别怎么实现？ |
| `runtime/panic.go`、`runtime/alg.go` | `panicdivide`、`panicshift`；`f64hash` | NaN 作为 map 键为什么永远查不到？`+0` 和 `-0` 为什么是同一个键？ |

## 📊 Java 25 ↔ Go 1.27 机制级对照表

| 维度 | Java 25 | Go 1.27 | 取舍 / 设计动机 |
|---|---|---|---|
| 类型同一性 | 类/接口按全限定名（加类加载器）名义判定；原始类型之间有 §4.10.1 的子类型关系 | 命名类型名义判定，类型字面量结构判定 | 结构等价让 `[]int`、`func(int) error` 这类无名类型无需声明就能跨包通用；定义类型保留名义上的类型安全 |
| 类型别名 | 没有（只有 import 层面的简写） | `type T = U`，含泛型别名；`byte`、`rune`、`any` 都是别名 | 别名为跨包迁移类型而设计（设计文档 18130）：类型同一性不变，迁移可以拆成多次提交完成 |
| 用户定义数值类型 | 不能定义新的原始类型，只能包成类，付出对象头和装箱的代价 | `type Celsius float64` 零开销，有独立方法集，不能和 `float64` 混算 | 用定义类型表达单位和 ID，类型安全不需要付运行期代价 |
| 隐式数值转换 | 拓宽（含有损的 `long→float`）、数值提升、复合赋值隐含窄化 | 没有；类型不同就编译错误 | 规避 C 式 "usual arithmetic conversions" 带来的混乱（Go FAQ）；代价是转换噪音，靠无类型常量缓解 |
| 字面量的类型 | 字面量本身有类型（`1` 是 int，另有 `1L`、`1.0f`） | 字面量和未指定类型的 const 都无类型，按上下文定型，没有上下文时用默认类型 | `5 * time.Second`、`var f float32 = 0.1` 不需要后缀，也不需要隐式转换 |
| 常量精度 | 按声明类型的二补码 / IEEE 语义折叠（`ConstFold`），`0.1+0.2==0.3` 为 false | 精确运算（`big.Int`/`big.Rat`，512 位上限），`0.1+0.2==0.3` 为 true | Java 的常量结果和运行期严格一致；Go 的常量和变量结果可能不同，换来常量表达式不会丢精度 |
| 常量越界 | 赋值上下文只允许可表示的常量窄化到 byte/short/char（§5.2）；显式强转一律静默截断 | 任何定型都检查可表示性，越界是编译错误；`T(c)` 也不允许截断 | 常量截断在 Go 里不可能静默发生 |
| 常量跨编译单元 | 内联进使用方 class（§13.1），声明方改值后，不重编的使用方读到旧值（§13.4.9） | 常量没有存储，信息在导出数据里；整个程序从源码静态链接成一个二进制 | Java 的二进制兼容单位是 class 文件；Go 没有能单独替换的依赖二进制，也就没有陈旧常量问题 |
| 整数溢出 | 静默回绕（§4.2.2）；`Math.*Exact` 抛异常 | 静默回绕，由规范定义，禁止编译器假设不溢出；`math/bits` 提供带进位的运算 | 两边都拒绝 C 的 UB，也都不默认检查，检查交给库函数 |
| 除零与 `MIN / -1` | `ArithmeticException`；`MIN / -1 == MIN` | 常量除零是编译错误，运行期除零 panic；`MinInt / -1 == MinInt`（amd64 靠 -1 fixup 实现） | 硬件 `IDIV` 在这两种情况下都会触发异常，两边都得插入检查或 fixup |
| 移位计数 | 掩码：int 取低 5 位，long 取低 6 位，负数也被掩码 | 不掩码：大于等于位宽时结果为 0（有符号右移为全符号位），负计数 panic | Go 选择不依赖平台的语义，amd64 上多三条指令，`s & 63` 可以消掉；Java 和 `ishl`/x86 的行为一致 |
| 浮点转整数越界 | 规范定义：NaN 得 0，其余饱和到 MIN/MAX | implementation-dependent；amd64 上 NaN 和 1e20 都得到 `MinInt64` | Java 为可移植性付出额外检查；Go 直接用硬件指令的行为 |
| 浮点融合 | 永远严格求值（JEP 306），只有显式 `Math.fma` | 规范允许 FMA 融合（可以跨语句），显式 `float64(...)` 阻止；amd64 需要 `GOAMD64=v3` | Java 选择逐位可复现；Go 选择性能，可复现需要自己显式舍入 |
| −0 与 NaN | `-0.0` 字面量就是负零；`==` 用 IEEE，`Double.equals` 比较位 | 常量没有 -0/NaN/Inf（`-0.0` 是 +0）；`==` 用 IEEE；map 中 NaN 永远查不到，±0 是同一个键 | Java 的分裂点在 `==` 和 `equals` 之间；Go 的分裂点在常量和变量之间 |
| 原始类型与装箱 | 二元体系；装箱缓存让 `==` 的结果依赖取值 | 没有装箱类型；值转成接口时可能分配，`convT64` 对小于 256 的值复用 `staticuint64s`（见 [Ch05](../ch05-interfaces/)） | Java 为擦除式泛型保留装箱；Go 的接口 `==` 比较值而不是身份，缓存只是分配优化，对语义不可见 |
| 运行期精确转换测试 | JEP 507：`x instanceof byte`，`switch` 原始类型模式 | 没有；只在常量上检查可表示性，变量要手写往返比较 | Java 把"值能否无损转换"纳入模式匹配；Go 坚持转换总是成功、不报告信息丢失 |
| 推断入口 | `var x = 1`：取字面量的类型 `int` | `x := 1`、`var x = 1`：取无类型常量的默认类型 `int`；`var x T = c` 指定类型 | 结果相同、机制不同：Java 的字面量本来就有类型，Go 是常量定型时才取默认类型 |

## 🔬 实验

### 实验 1：无类型常量的精度与可表示性边界（必做）
- **目的**：验证"常量精确，定型时检查；变量回绕，从不检查"，并把每条编译错误对应到 types2 的具体函数。
- **方法**：
  - `go/lab/consts/bad_consts.go` 文件头加 `//go:build ignore`，这样 `go vet ./...` 不会扫到它。里面逐行写 15 条左右的声明：`const c uint8 = 256`、`b8 += 300`、`fmt.Println(math.MaxUint64)`、`1 / 0`、`3.14 / 0.0`、`int(1.5)`、`var u = 1.0 << s`、`var v float32 = 1 << s`、`const huge = 1 << 512`、`1 << 511` 赋给 `_`、`1 << -1`、`string(65.0)`、`var n uint = -1`、`var k int8 = 1 << 7`、`n * time.Millisecond`。
  - `go/lab/typecheck/main.go` 用 `go/types` 把同一批判断程序化：`types.Identical`/`AssignableTo`/`ConvertibleTo` 作用于 `IDs`、`Other`、`Alias = []int`、`Set[T] = map[T]struct{}`、`byte`/`uint8`；用 `types.Eval` 求 `1 << 100 >> 98`、`0.1 + 0.2 == 0.3`、`uint8(256)`。
- **工具**：`go build -gcflags=-e ./ch01-types-constants/go/lab/consts/bad_consts.go`（`-e` 列出全部错误）、`go run ./ch01-types-constants/go/lab/typecheck`。
- **预期现象（先写下自己的预测）**：本机实测的错误文本包括 `cannot use 256 (untyped int constant) as uint8 value in constant declaration (overflows)`、`invalid operation: division by zero`、`cannot convert 1.5 (untyped float constant) to type int`、`invalid operation: shifted operand 1.0 (type float64) must be integer`、`constant shift overflow`、`invalid operation: negative shift count -1 (untyped int constant)`、`invalid operation: n * time.Millisecond (mismatched types int and time.Duration)`。容易漏掉的是：`1 << 511` 本身合法，赋给 `_` 时才因为默认类型 `int` 报 overflows。`go/types` 的结果：`Identical(IDs, []int)` 为 false，`AssignableTo([]int, IDs)` 为 true，`AssignableTo(IDs, Other)` 为 false，`ConvertibleTo(IDs, Other)` 为 true，`Identical(byte, uint8)` 为 true；`Eval` 得到 `untyped int 4`、`untyped bool true` 和错误 `constant 256 overflows uint8`。
- **记录**：预测与实际对照表；每条错误对应的规范小节和 types2 函数（`overflow` / `representable` / `conversion` / `shift` / `binary` / `assignableTo`）。

### 实验 2：Java 编译期常量内联与装箱缓存（必做）
- **目的**：用字节码确认 §13.1 的"引用在编译期解析为值"，复现 §13.4.9 的陈旧常量问题，并观察装箱缓存的边界和调节开关。
- **方法**：`java/ConstInline/Consts.java` 声明 `static final int LIMIT = 100`、`long BIG = 1L << 40`、`Integer BOXED = 100`、`String NAME = "a" + "b"`、`int COMPUTED = Integer.parseInt("7")`，`User.java` 读取它们。`javac -d out *.java` 后执行 `javap -c -p out/User.class` 和 `javap -v out/Consts.class`；接着只把 `Consts` 里的 100 改成 200 并只重编 `Consts.java`，再运行 `User`。装箱部分：比较 `Integer` 和 `Long` 的 127/128，分别加 `-XX:AutoBoxCacheMax=1000` 和 `-Djava.lang.Integer.IntegerCache.high=1000` 各运行一次。
- **预期现象**：`User` 的字节码里出现 `bipush 100`、`ldc2_w`（long 1099511627776）、`ldc "ab"`，而 `BOXED` 和 `COMPUTED` 是 `getstatic`；只重编声明方之后，`User` 打印 `100 ... 200 ... 7`，`LIMIT` 是旧值。`Integer` 的 128 默认 `==` 为 false，加任一开关后为 true；`Long` 不受影响。
- **Go 对照**：常量在 `go build -gcflags=-S` 输出里只是立即数，二进制里没有对应的符号；用 `go tool nm` 验证这一点。
- **记录**：`javap` 原文、陈旧值复现步骤、两个开关的效果，以及 `IntegerCache` 源码中对应的行号。

### 实验 3：JEP 507 精确转换边界 vs Go 可表示性（必做）
- **目的**：对比"运行期值能否无损转换"（Java）和"常量能否被类型表示"（Go）这两条规则在边界上的差异。
- **方法**：
  - Java（`java/PrimitivePatterns.java`，通过 `bin/jrun` 运行，它自带 `--enable-preview`）：测试 `1000`/`100 instanceof byte`、`16777216`/`16777217`/`Integer.MAX_VALUE instanceof float`、`1.0`/`1.5`/`-0.0 instanceof int`、`NaN`/`-0.0 instanceof float`、`1e20 instanceof long`、`Long.MAX_VALUE instanceof double`；写 `switch (long)`、`switch (double)`、`switch (boolean)` 各一个。再用 `javac --release 25 --enable-preview -d out` 编译、`javap -c -v` 查看。
  - Go：把同样的值写成常量声明（`var _ byte = 1000`、`var _ float32 = 16777217`、`var _ int = 1.0`、`var _ int = 1.5`），看哪些能通过编译；运行期部分调用练习 4 的 `ExactInt64`/`ExactFloat32`。
- **预期现象**：Java 侧结果见上文"本机实测边界"；字节码中有 `invokestatic ExactConversionsSupport.isIntToByteExact:(I)Z` 和 `typeSwitch:(JI)I`。Go 侧 `16777217` **可以**赋给 `float32`（浮点可表示性允许舍入），而 Java 判为不精确；`-0.0` 在 Go 里根本不存在。
- **记录**：一张"值 × Java instanceof × Go 常量可表示性 × Go 运行期转换结果"的四列表，并对每一处差异写出规范依据。

### 实验 4：数值边界跨语言对拍 + 汇编观察
- **目的**：用同一组表达式（操作数全部是变量，避免常量折叠）对拍两门语言的输出，并在汇编层确认移位、除法、FMA 的实现。
- **方法**：`go/lab/numeric` 和 `java/NumericLab.java` 打印同一份清单：`MaxInt32 + 1`、`MinInt64 / -1`、`-7 / 2`、`-7 % 2`、`1 << 32`/`1 << 64`（计数是变量）、负移位计数、`int64(1e20)` 与 `(long)1e20`、`int64(NaN)` 与 `(long)NaN`、`uint8(300)` 与 `(byte)300`、`-0.0` 字面量的符号位、`0.1 + 0.2 == 0.3`（常量和变量各一次）、`x*y + z`（取 `0.1, 10, -1`）。Go 侧再给 `shl`、`shl(s & 63)`、`shl(int 计数)`、`div` 加 `//go:noinline`，执行 `go tool objdump -s 'main\.shl'`；`mad` 分别用 `GOAMD64=v1` 和 `GOAMD64=v3` 构建。
- **预期现象（本机实测）**：Go 的 `1<<64` 为 0，Java 的 `1L<<64` 为 1；Go 负移位 panic `negative shift amount`，Java 得到 `MIN_VALUE`；`int64(1e20)` 为 `-9223372036854775808`，`(long)1e20` 为 `Long.MAX_VALUE`；Go 的 `-0.0` 符号位为 false，Java 打印 `-0.0`。`mad(0.1, 10, -1)` 在 v1 下为 `0`，在 v3 下为 `5.551115123125783e-17`（出现 `VFMADD231SD`），`float64(x*y) + z` 在两种配置下都是 `0`；Java 的 `x*y+z` 为 `0.0`，`Math.fma` 为 `5.551115123125783E-17`。
- **有效性威胁**：浮点转整数和 FMA 的结果依赖 GOARCH 和 GOAMD64，报告里必须写清楚；有条件的话在 WSL2 或 ARM64 机器上重跑，补上"待实测"的那几项。

## 🧪 练习规划

**Go：`go/exercises`**（`go test ./ch01-types-constants/go/...`；本章不依赖泛型，泛型版本留到 [Ch06](../ch06-generics/)）
1. `type Perm uint16` + `iota` 位掩码（`Read`、`Write`、`Exec`、`_` 跳过一位、`Admin`），实现 `Has`、`Set`、`Clear`（用 `&^`）和 `String()`（如 `"rwx-A"`）。测试断言常量值、**隐式重复保留了类型**（`reflect.TypeOf(Admin)` 是 `Perm`），以及组合位的字符串形式。
2. `SatAddInt32(a, b int32) int32`、`SatSubUint8(a, b uint8) uint8`、`AddInt64Checked(a, b int64) (int64, bool)`、`MulUint64Checked(a, b uint64) (uint64, bool)`：不允许借助更宽的类型，后两个分别用符号位技巧和 `bits.Mul64`。表驱动测试覆盖 `MinInt64`、`MaxInt64`、0、-1 这些边界。
3. `FloorDiv`/`FloorMod(a, b int64)`：结果与 Java 的 `Math.floorDiv`/`floorMod` 一致。测试包括负数、`MinInt64 / -1`，以及除数为 0 时 `recover` 得到的值是 `runtime.Error`，消息包含 `integer divide by zero`。
4. `ExactInt64(f float64) (int64, bool)`、`ExactFloat32(f float64) (float32, bool)`：Go 版的 `ExactConversionsSupport.isDoubleToLongExact`/`isDoubleToFloatExact`。测试表直接来自实验 3（NaN、±Inf、`-0.0`、2⁶³、`1.5`、`1e20`、`2⁵³+1`），专门抓"`int64(f)` 越界时结果实现相关，往返比较不可靠"这个坑。
5. `predict.go`：在 20 个常量里填写对表达式结果的**预测**（不许运行），比如 `uint8` 回绕、`int32(int8(v))`、`x << 64`、`-7 % 2`、`MinInt64 / -1`、`int64(-8) >> 100`、`math.Signbit(-z)`。测试在运行期计算真实值并逐个比对，失败信息里打印对应的规范小节名。

**Java：`java/drills/NumericDrill.java`**（`bin/jrun ch01-types-constants/java/drills/NumericDrill.java`）
- `int shl(int x, int s)` / `long shlL(long x, int s)`：先在注释里写出 `s = 32、64、-1` 的预测，再用自校验断言（§15.19）。
- `long safeMul(int a, int b)`：自校验专门抓 `imul; i2l` 的提升陷阱。
- `boolean sameBoxed(int v)`：断言 127 为 true、128 为 false（默认参数），以及 `Long` 的行为。
- `boolean fitsByte(int v)` 用 `v instanceof byte` 实现，`String classify(double d)` 用原始类型模式 `switch` 实现，`boolean exactLong(double d)` 与 Go 练习 4 共用同一张测试表。
- `doubleKeySemantics()`：用 `HashMap<Double, …>` 放入 NaN 两次、`0.0` 和 `-0.0` 各一次，断言 `size()`，并与 Go 的 `map[float64]…`（实测长度为 3：两个 NaN 加一个 0）对比。

## 🧭 设计取舍

- **为什么 Go 不做隐式数值转换。** Go FAQ 的回答是：C 的自动转换带来的便利"is outweighed by the confusion it causes"。表达式什么时候是无符号的、值有多大、会不会溢出、结果是否与机器无关，这些都说不清，而且 "usual arithmetic conversions" 难以实现，在不同架构上也不一致。所以 Go 连 `int` 和 `int64` 也区分开，同一份代码在 386 和 amd64 上的类型检查结果必须相同。Java 用另一种办法解决可移植性：把所有原始类型的宽度固定下来，于是可以放心地允许拓宽。但 Java 仍然保留了两个 Go 刻意排除的陷阱：有损的 `long→float` 拓宽（§5.1.2）和复合赋值隐含的窄化（§15.26.2）。Go 的代价是到处写 `float64(n)`，这一点由无类型常量和 [Ch06](../ch06-generics/) 的类型集来缓解。
- **为什么常量是无类型的、精确的，而不是 Java 那样有类型的编译期常量。** 如果禁止隐式转换、常量又有类型，就得写 `i = int(0)`、`float32(1)`。Rob Pike 在《Constants》里的方案是让数值常量 "live in an arbitrary-precision numeric space; they are just regular numbers"，默认类型 "is exposed when, and only when, no other type information is available"。代价有三：常量和变量的语义出现分裂（`0.1+0.2==0.3`）；编译器要做大数运算并设上限（types2 的 512 位）；`fmt.Println(math.MaxUint64)` 这类写法会因为默认类型而报错。Java 选择了有类型的字面量加后缀（`1L`、`1.0f`），常量折叠和运行期结果因此完全一致；但为了不让 `byte b = 42` 必须写强转，它又在 §5.2 为 byte/short/char 打了一个"可表示就允许"的局部补丁，本质上是同一个问题的局部解。常量内联的副作用则留下了 §13.4.9 的二进制兼容陷阱。
- **为什么有类型别名，而且只给类型。** 设计文档 `design/18130-type-alias.md`（Russ Cox、Robert Griesemer）的首要动机是 "gradual code repair during large-scale refactorings"：把类型从一个包移到另一个包时，新旧两个名字必须在过渡期内指向**同一个**类型。const、func、var 已经可以用转发的方式迁移，唯独类型做不到，因为定义类型会产生新的同一性，类型断言和 type switch 会对不上。被否决的替代方案是"泛化别名"（任何声明都能起别名），原因是它 "overlap with and complicate other declaration forms"，而且真正迫切的只有类型这一种。泛型别名（#46477）要等 `go/types` 增加 `Alias` 节点、导出数据格式随之修改之后才能落地。代价是同一个类型有两个名字，`%T` 和报错信息里显示的可能是别名背后的名字。Java 没有别名：类的身份由全限定名加类加载器决定，迁移只能靠弃用加委托子类或包装类。
- **为什么溢出是确定的回绕，而不是 UB 或异常。** 规范同时排除了两个方向。一是 C 的 UB 方案：编译器 "may not assume that x < x + 1 is always true"，放弃了基于"不溢出"假设的循环优化，换来可预测性。二是"默认检查"或"任意精度 int"：Rob Pike 的提案 #19623 想让 `int` 变成任意精度，最终以 not planned 关闭，反对意见集中在乘法可能需要运行期调用、带标记的字会让 GC 区分指针变复杂、把 `int` 转成 `int64` 的现有代码会在大值时静默出错。Java 做了同样的选择（§4.2.2），检查交给 `Math.*Exact` 这类内建函数；Go 对应的是 `math/bits` 的 `Add64`/`Mul64`。除零是例外，两边都选择 trap，因为它没有有意义的结果，硬件也会触发异常。移位计数上两边分道扬镳：Go 要求"移 n 次"这种不依赖平台的语义，愿意在 amd64 上多付三条指令；Java 直接沿用字节码 `ishl` 的掩码语义。
- **Java 为什么保留原始类型/装箱二元体系，JEP 507 在补什么。** 原始类型是为 1990 年代 JVM 的性能保留的；擦除式泛型只能接受引用类型，于是需要自动装箱，再用 -128..127 的缓存这个 "pragmatic compromise" 压低分配，结果是 `==` 的语义依赖取值。值类型（JEP 401）和增强装箱（JEP 402，目前仍是 Draft）都不在 Java 25 中。JEP 507 的 Non-goal 写明 "It is not a goal to add new kinds of conversions"，它做的是让原始类型参与模式匹配，用"精确转换"定义匹配，"restores alignment between the semantics of `instanceof T` and `instanceof T t`"。Go 从来没有装箱问题：它没有 `Object` 根类型，也不用擦除实现泛型，接口值是"类型字加数据指针"，`==` 比较的是值。Go 也没有 JEP 507 的对应物，因为它的哲学是转换总是成功、"no indication of overflow"，要不要检查交给调用方决定。

## ✅ 验收答辩题

- [ ] [L2] `type IDs []int`、`type Other []int`、`[]int` 两两之间哪些可以赋值、哪些可以转换？分别指出 `(*operand).assignableTo` 和 `convertibleTo` 里放行或拒绝的那一行。
- [ ] [L2] `byte` 和 `uint8` 在 types2 里是两个 `*Basic` 对象，为什么 `Identical` 返回 true？`any` 为什么不是命名类型？泛型别名 `Set[int]` 在 `%T` 里显示成什么，为什么？
- [ ] [L2] `go/constant` 里 `0.1`、`1 << 100`、`1e1000 / 1e999` 分别用什么表示？为什么规范只要求 256 位，而 `1 << 512` 却在 types2 里报错？
- [ ] [L2] `byte b = 100;` 合法而 `m(100)`（`m(byte)`）不合法，javac 在哪里实现这个差别？与 Go 的可表示性规则有什么异同？
- [ ] [L2] `x instanceof byte` 编译成什么字节码？`isIntToFloatExact` 为什么要单独排除 `Integer.MAX_VALUE`？浮点转整数为什么要排除 `-0.0`？
- [ ] [L2] Go 的 `x << s` 和 `x / y` 在 amd64 上各生成哪几条额外指令？分别对应规范的哪句话？
- [ ] [L3] 先预测实验 1 那 15 行声明中哪些编译失败、错误文本是什么，再用 `-gcflags=-e` 验证；预测错了的，用 `updateExprType` 或默认类型规则解释原因。
- [ ] [L3] 先预测实验 4 清单在两门语言里的全部输出，再对拍；对每一处"implementation-dependent"的结果，说明它在 ARM64 上可能是什么，以及你打算怎么验证。
- [ ] [L3] 预测 `mad(0.1, 10, -1)` 在 `GOAMD64=v1`/`v3` 下的结果，用 objdump 证实；再写出一个在任何 GOARCH 下结果都确定的版本。
- [ ] [L3] 给定一组 `static final` 声明，预测哪些在 `javap` 里被内联、哪些走 `getstatic`，以及只重编声明方之后使用方打印什么；用实验 2 验证。
- [ ] [L4] 写一页评述：Go 的无类型常量与 Java 的有类型编译期常量（加 §5.2 窄化补丁、§13.4.9 内联陷阱）。各自解决了什么问题，代价落在哪里？引用 Go FAQ、《Constants》和 JLS 原文。
- [ ] [L4] 如果 Go 默认对有符号整数做溢出检查（panic），规范、编译器（BCE、prove、循环优化）和现有代码会受到什么影响？结合 #19623 的讨论以及 Java `Math.*Exact` 的内建实现论证。

## 🕳️ 常见坑（Java 老手专属）

- **Go 的 `byte` 是无符号的**：它是 `uint8`，取值 0..255；Java 的 `byte` 是 -128..127。从 Java 移植二进制协议代码时，`(byte)0xFF == -1` 的直觉会出错。
- **`n * time.Second` 编译失败**（`n` 是 `int` 变量）：常量 `5 * time.Second` 能通过，是因为 5 是无类型常量。要写 `time.Duration(n) * time.Second`。
- **以为 `-0.0` 是负零**：Go 常量里没有负零，要用 `math.Copysign(0, -1)`，或者对一个值为 0 的变量取负。
- **以为浮点转整数越界会饱和**（Java 习惯）：Go 的结果实现相关，amd64 上是 `MinInt64`。需要饱和或精确判断时，先用 `math.IsNaN` 和范围比较。
- **以为移位计数会取模**：Go 里 `x << 64 == 0`，负计数直接 panic。`go vet` 的 `shift` 分析器能抓常量计数越界（`x (32 bits) too small for shift of 40`），变量计数抓不到。
- **`fmt.Println(math.MaxUint64)` 编译失败**：无类型常量传给 `any` 参数时取默认类型 `int`，放不下。要写 `uint64(math.MaxUint64)`。
- **`var f float64 = 1 << s` 编译失败**：非常量移位的左操作数从上下文取类型，`float64` 不能移位。
- **`string(i)` 不是 `strconv.Itoa`**：它得到码点 `i` 对应的字符，`go vet` 的 `stringintconv` 会报 `conversion from int to string yields a string of one rune, not a string of digits`。
- **`x == math.NaN()` 永远是 false**：要用 `math.IsNaN`。以 `float64` 为键的 map 每放一次 NaN 就多一个查不到的条目，只能 `clear` 或者遍历删除。
- **以为 `type MyMutex sync.Mutex` 会带上原类型的方法**：类型定义不继承方法，需要方法就嵌入或者用别名。
- **期待 `const` 能声明 struct、slice、map**：Go 的常量只有布尔、数值和字符串（以及它们的定义类型），只读的复合值要用 `var` 加约定。Java 的 `static final List` 同样不是常量变量。

## 📚 一手资料

- Go spec（本机 `$GOROOT/doc/go_spec.html`）：[Constants](https://go.dev/ref/spec#Constants)、[Types](https://go.dev/ref/spec#Types)、[Numeric types](https://go.dev/ref/spec#Numeric_types)、[Underlying types](https://go.dev/ref/spec#Underlying_types)、[Type identity](https://go.dev/ref/spec#Type_identity)、[Assignability](https://go.dev/ref/spec#Assignability)、[Representability](https://go.dev/ref/spec#Representability)、[Type declarations](https://go.dev/ref/spec#Type_declarations)、[Iota](https://go.dev/ref/spec#Iota)、[Integer operators](https://go.dev/ref/spec#Integer_operators)、[Integer overflow](https://go.dev/ref/spec#Integer_overflow)、[Floating-point operators](https://go.dev/ref/spec#Floating_point_operators)、[Conversions](https://go.dev/ref/spec#Conversions)、[Constant expressions](https://go.dev/ref/spec#Constant_expressions)
- Go 设计与官方文章：[Go FAQ](https://go.dev/doc/faq)（"Why does Go not provide implicit numeric conversions?"、"How do constants work in Go?"）、Rob Pike [Constants](https://go.dev/blog/constants)、Robert Griesemer [What's in an (Alias) Name?](https://go.dev/blog/alias-names)、[design/18130-type-alias.md](https://go.googlesource.com/proposal/+/master/design/18130-type-alias.md)、[#46477 泛型类型别名](https://go.dev/issue/46477)、[#19623 任意精度 int](https://go.dev/issue/19623)
- Go 源码：`cmd/compile/internal/types2/{predicates,api_predicates,operand,conversions,const,expr,alias,universe,sizes}.go`、`go/constant/value.go`、`cmd/compile/internal/noder/{writer,helpers}.go`、`cmd/compile/internal/ssa/_gen/{generic,AMD64,ARM64}.rules`、`cmd/compile/internal/ssagen/ssa.go`、`runtime/{panic,alg}.go`
- JLS（Java SE 25）：§4.2 Primitive Types and Values、§4.10.1、§4.12.4 `final` Variables、第 5 章 Conversions and Contexts（§5.1.2、§5.1.3、§5.1.7、§5.2、§5.6、§5.7）、§13.1、§13.4.9、§15.17.2、§15.19、§15.20.2、§15.26.2、§15.29 Constant Expressions
- JEP：[JEP 507](https://openjdk.org/jeps/507)（Primitive Types in Patterns, instanceof, and switch，第三次预览）、[JEP 306](https://openjdk.org/jeps/306)（Restore Always-Strict Floating-Point Semantics）、[JEP 401](https://openjdk.org/jeps/401) / [JEP 402](https://openjdk.org/jeps/402)（Valhalla，不在 Java 25 中）
- JDK 源码（`lib/src.zip`）：`java.base/java/lang/{Integer,Long,Double,Math}.java`、`java.base/java/lang/runtime/{ExactConversionsSupport,SwitchBootstraps}.java`、`jdk.compiler/com/sun/tools/javac/code/Types.java`（`isAssignable`、`isConvertible`、`isCastable`、`isUnconditionallyExact`）、`jdk.compiler/com/sun/tools/javac/comp/{ConstFold,Lower}.java`；HotSpot `src/hotspot/share/opto/`
