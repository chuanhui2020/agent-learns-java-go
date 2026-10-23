# Ch09 · 反射、unsafe 与运行时类型信息

> Part 1 · 估时 12h · 状态：⚪ 未开始 · 类型信息从哪里来，反射凭什么能读写任意值，unsafe 又绕过了谁的假设。Go 的答案是"链接期定死的类型描述符 + 带权限位的 Value"，Java 的答案是"运行时加载的 Class + 可被 JIT 内联的方法句柄"

## 🎯 目标（资深标准）

- **L1**：能用 `reflect` 按 struct tag 遍历、读写字段，准确说出 `CanAddr` / `CanSet` / `CanInterface` 各自何时为真；能正确使用 `unsafe.String` / `StringData` / `Slice` / `SliceData` / `Add`，并写出每个调用的前置条件。Java 侧熟练使用 `Method` / `Field` / `MethodHandle` / `VarHandle`，知道 `--add-opens` 和 `--sun-misc-unsafe-memory-access` 各管什么。
- **L2**：讲清 `abi.Type` 及其扩展结构（`StructType`、`UncommonType`、方法表 `[]abi.Method`）的布局；讲清 `reflect.Value` 的三字表示和 flag 位，`ValueOf → unpackEface`、`Value.Call → funcLayout → runtime.reflectcall` 的路径，以及链接器为什么会因为 `Method` / `MethodByName` 放弃方法裁剪。Java 侧讲清 JVMS 第 5 章的加载、链接、初始化，双亲委派与类卸载条件，核心反射如何改由方法句柄实现（JEP 416），以及 MethodHandle 的访问检查为什么在创建时完成。
- **L3（能验证/能预测）**：运行前写下预测，再用数据证实或解释偏差，包括：反射读写、反射调用相对直接调用和接口调用的 ns/op 与 allocs/op；`MethodByName` 传常量与传变量时二进制里保留了哪些方法符号；`go vet` 和 `-d=checkptr` 各能抓住哪些非法 unsafe 转换；json/v2 首次编码一个新类型和稳态编码的分配次数差；Java 中 `static final` MethodHandle、非常量 MethodHandle 和 `Method.invoke` 的相对开销。
- **L4（能论设计）**：论证 Go 为什么没有类加载器和运行时代码生成、`reflect` 为什么被设计成"慢但安全"、`unsafe` 为什么是一个包而不是语言特性；论证 Java 为什么要收紧 `Unsafe`、推行强封装。引用 JEP 416/403/471/498/484、Go 1 兼容性文档和 linkname 限制的 issue。

## 🔗 前置

[Ch02](../ch02-values-memory/)（`abi.Type` 的 `Size_`/`PtrBytes`、可寻址性、`Sizeof`/`Offsetof`）· [Ch03](../ch03-slice-map-string/)（string/slice header）· [Ch05](../ch05-interfaces/)（eface/iface/itab、direct-iface 类型）· [Ch06](../ch06-generics/)（泛型作为反射的替代，以及泛型方法对 `reflect` 不可见）。后续会用到本章的有：[Ch12](../ch12-sync-memory-model/)（VarHandle 访问模式）、[Ch15](../ch15-alloc-escape-compiler/)（内联与去虚拟化）、[Ch16](../ch16-gc/)（`GCData` 指针位图）、[Ch19](../ch19-syscall-cgo-ffi/)（cgo 指针规则、`runtime.Pinner`、FFM）。

## 💡 关键洞察

1. **Go 的类型信息是链接期写进二进制只读段的数据。** 运行期不会出现新的"带方法的类型"，也不会生成新的机器码。Java 的 `Class` 则是运行时加载、链接、初始化出来的，还能在运行时现场生成（hidden class、Class-File API）。两边反射的能力边界、优化由谁来做（Go 靠链接器裁剪，Java 靠 JIT 专门化），都由这一点决定。
2. **`reflect.Value` 由类型指针、数据指针和权限位三部分组成，可设置性不是类型的属性，而是沿获取路径一路继承下来的 flag。** 经过指针解引用才得到 `flagAddr`，经过未导出字段就会带上 `flagRO`。Go 反射每次操作都检查这些位，又没有 JIT 去专门化，所以反射调用比直接调用慢约两个数量级。Java 的 `Method.invoke` 底层就是方法句柄，持有在 `static final` 字段里时，JIT 能做常量折叠并把它内联。
3. **unsafe 并不意味着"类型系统不管了"，而是 GC 和编译器的假设改由你来维持。** `uintptr` 不是引用：GC 不追踪它，栈搬迁后它也会失效。`unsafe.Pointer` 的六种合法模式之外都是未定义行为，`checkptr` 和 `vet` 只能抓住其中一部分。Java 走的是反方向：把 `sun.misc.Unsafe` 的内存访问方法标记为弃用并默认告警，用 VarHandle 和 FFM 替代。

## ☕ Java 25 深度复习

### 类型信息从哪里来：类加载
- **JVMS 第 5 章**：加载（§5.3）→ 链接（§5.4：验证 §5.4.1、准备 §5.4.2、解析 §5.4.3）→ 初始化（§5.5，触发时机见 JLS §12.4.1）。解析允许惰性进行。运行时一个类由"二进制名 + 定义加载器"这一对标识，同一份字节码被两个加载器各定义一次，就是两个互不相等的 `Class`。
- `java.lang.Class` 是堆上的镜像对象，真正的类元数据在 Metaspace 里（HotSpot `src/hotspot/share/oops/` 的 klass 系列、`classfile/` 的解析与加载）。
- **双亲委派**（`java/lang/ClassLoader.java` 的 `loadClass(String, boolean)`）：先在 `getClassLoadingLock(name)` 上加锁，依次 `findLoadedClass` → `parent.loadClass`（parent 为 null 时用 `findBootstrapClassOrNull`）→ 都找不到才调用自己的 `findClass`。
- **类卸载**（JLS §12.7）：只有当类的定义加载器能被 GC 回收时，类才可能被卸载；bootstrap 加载器加载的类永不卸载。hidden class 是例外：默认可以独立于加载器卸载，传 `ClassOption.STRONG` 才和加载器同生同死（见 `MethodHandles.java` 中 `defineHiddenClass` 的文档）。
- **运行时生成类**：`MethodHandles.Lookup.defineClass(byte[])`、`defineHiddenClass(byte[], boolean, ClassOption...)`（`NESTMATE` / `STRONG`）。hidden class 没有可解析的名字，`Class.forName` 找不到它（本机实测类名形如 `GenHidden$Doubler/0x...`）。**Class-File API（JEP 484）**：`java/lang/classfile/ClassFile.java` 的 `ClassFile.of().build(ClassDesc, Consumer<ClassBuilder>)` / `parse` / `transformClass`。JDK 25 的 lambda 代理类就是 `InnerClassLambdaMetafactory` 用 Class-File API 生成、按 `NESTMATE | STRONG` 定义的 hidden class（`src.zip` 中已经没有内部的 ASM 副本）。
- **Instrumentation（只作对照）**：`java.instrument/java/lang/instrument/Instrumentation.java` 的 `redefineClasses` / `retransformClasses` 可以在运行时替换类的字节码。已经在执行的栈帧继续跑旧字节码，静态变量保持原值，允许改动的范围以 JVM TI RedefineClasses 为准。Go 没有对应能力。

### 反射的实现
- **元数据缓存**：`Class` 里有一个 `SoftReference<ReflectionData<T>> reflectionData` 缓存声明的字段和方法，但 `getDeclaredFields()` 这类方法返回的是拷贝（`copyFields`）。所以每次查找都会分配，`Method` / `Field` 要自己缓存。
- **`Method.invoke`**（`@CallerSensitive`、`@IntrinsicCandidate`）：如果没调用过 `setAccessible(true)`（`override` 为 false），**每次调用**都会按调用者类做 `checkAccess`，然后读取 `@Stable` 的 `methodAccessor` 去调用。
- **JEP 416：核心反射基于方法句柄**。`jdk/internal/reflect/MethodHandleAccessorFactory.java` 的 `newMethodAccessor` 生成 `DirectMethodHandleAccessor`，它持有一个经 `asSpreader` / `asType` 适配成统一形状的目标 MethodHandle。只有 5 种情况会回退到 VM 原生实现（`useNativeAccessor`）：`java.lang.invoke` 尚未初始化、`-Djdk.reflect.useNativeAccessorOnly=true`、签名多态方法、最后一个参数不是数组的 varargs 成员、参数槽位数 ≥ 255。
- **MethodHandle 的访问检查在创建时做**。`MethodHandles.java` 文档原文："Method handles do not perform access checks when they are called, but rather when they are created"，并明确说这是与核心反射的关键区别。`invokeExact` 是签名多态调用。`static final` 字段里的 MH 对 JIT 来说是常量，C2 可以把目标方法内联到调用点；非常量 MH 走间接调用。VarHandle 同理（访问模式见 [Ch12](../ch12-sync-memory-model/)），可以用 `Lookup.findVarHandle` / `MethodHandles.privateLookupIn` 获得。
- **强封装（JEP 403）**：对 JDK 内部包的非公开成员调用 `setAccessible(true)` 会抛 `InaccessibleObjectException`（本机实测 `String.value`）。加上 `--add-opens java.base/java.lang=ALL-UNNAMED` 后才能访问。

### `sun.misc.Unsafe` 的退场（JEP 471 / 498）
- `jdk.unsupported/sun/misc/Unsafe.java` 中的内存访问方法都标了 `@Deprecated(forRemoval=true)`，入口第一行调用 `beforeMemoryAccess()`。是否放行由 `--sun-misc-unsafe-memory-access=allow|warn|debug|deny`（对应系统属性 `sun.misc.unsafe.memory.access`）决定。**JDK 25 的默认值是 `warn`**（`MemoryAccessOption.defaultValue()`）：第一次使用时打印多行警告（本机实测），`deny` 时抛 `UnsupportedOperationException`。
- 替代方案：堆内访问用 VarHandle，堆外内存用 FFM（[Ch19](../ch19-syscall-cgo-ffi/)）。JDK 自己用的是 `java.base/jdk/internal/misc/Unsafe.java`，这个包不导出。
- 一个和 [Ch02](../ch02-values-memory/) 呼应的细节：本机 `objectFieldOffset` 对第一个 `int` 字段返回 12，正好是对象头的大小。

## 🐹 Go 1.27 深入

### 规范层
- **spec「Package unsafe」**：定义了 `Pointer`、`Sizeof` / `Alignof` / `Offsetof`、`Add` / `Slice` / `SliceData` / `String` / `StringData`。`Pointer` 和 `uintptr` 之间互转的效果是 **implementation-defined**。`String` 的约束："the bytes passed to String must not be modified afterwards"；`StringData` 返回的字节同样不能修改。`Slice(nil, 0)` 返回 nil；运行时 `len` 为负，或者 `ptr == nil` 而 `len != 0`，都会 panic。`unsafe` 包文档："not protected by the Go 1 compatibility guidelines"。
- **spec「Struct types」**：tag 是可选的字符串字面量，"made visible through a reflection interface and take part in type identity for structs"。也就是说 tag 参与类型同一性（「Type identity」），但「Conversions」在比较 struct 类型时忽略 tag。本机实测：两个只有 tag 不同的 struct 类型，`TypeOf(a) == TypeOf(b)` 为 false，`ConvertibleTo` 为 true。
- **tag 的 `key:"value"` 语法不是规范，只是 `reflect.StructTag` 的约定**。`Get` 不区分"没有这个 key"和"值为空"，`Lookup` 能区分。格式不合约定时两者的返回值都是 unspecified：`json: "b"`（冒号后多一个空格）时 `Get` 静默返回 `""`，`go vet` 的 `structtag` 检查能报出来。
- **reflect 的契约**（"The Laws of Reflection" 三定律）：从接口值得到反射对象；从反射对象还原接口值；要修改反射对象，值必须可设置。`CanAddr` 的条件与 spec「Address operators」一一对应：slice 元素、可寻址数组的元素、可寻址 struct 的字段、指针解引用。`CanSet` = 可寻址，且不是经由未导出字段获得的。未导出字段可以用 `Int()` 之类的方法读，但不能 `Set`，也不能 `Interface()`。

### 运行时与编译器层
- **类型描述符**（`internal/abi/type.go`）：`Type{Size_, PtrBytes, Hash, TFlag, Align_, FieldAlign_, Kind_, Equal, GCData, Str NameOff, PtrToThis TypeOff}`。按 Kind 再扩展成 `StructType{Type; PkgPath Name; Fields []StructField{Name, Typ, Offset}}`、`PtrType`、`FuncType`、`InterfaceType{PkgPath, Methods []Imethod}` 等。设置了 `TFlagUncommon` 的类型，描述符后面紧跟 `UncommonType{PkgPath, Mcount, Xcount, Moff}`，`Moff` 指向方法表 `[Mcount]Method{Name NameOff, Mtyp TypeOff, Ifn, Tfn TextOff}`。方法表按名字排序，导出方法排在前面，所以 `ExportedMethods` 直接取前 `Xcount` 项。`abi.Name` 的编码是：一个标志字节（exported / 带 tag / 带 pkgPath / embedded）+ varint 长度 + 名字 + 可选的 tag。**tag 就存放在字段名后面**。
- **描述符从哪里来**：编译器的 `cmd/compile/internal/reflectdata/reflect.go`（`writeType`、`dcommontype`、`dextratype`、`methods`）生成描述符，链接器把它们放进模块的 types 段。`NameOff` / `TypeOff` / `TextOff` 都是相对偏移，由 `runtime/type.go` 的 `resolveTypeOff` 等函数解析。被裁剪掉的方法，链接器会把它的 `TextOff` 写成哨兵值 `-1`（`cmd/link/internal/ld/data.go` 的 `R_METHODOFF` 分支）。运行时通过 `reflect.StructOf` / `FuncOf` / `PointerTo` 新造的类型放在堆上，由 reflect 的全局缓存保持可达。但 `StructOf` 不支持嵌入字段的提升方法，`MakeFunc` 也只是让通用汇编桩 `makeFuncStub` 转调你的闭包。**Go 运行时不会产生新代码**。
- **`reflect.Type`**：`rtype{ t abi.Type }` 实现了 `Type` 接口。`TypeOf(i)` 就是取 eface 的类型字（`abi.TypeOf`）。`TypeFor[T]()` 的实现是 `TypeOf((*T)(nil))` 再取 `Elem`，所以对接口类型同样成立，不再需要 `reflect.TypeOf((*io.Reader)(nil)).Elem()` 这种写法（`go fix` 的 `reflecttypefor` 现代化器会自动改写）。`MethodByName` 在排好序的方法表上二分查找；`FieldByName` 先线性扫描顶层字段，遇到嵌入字段才走 BFS（`FieldByNameFunc`）。
- **`reflect.Value`**（`reflect/value.go`）：`{typ_ *abi.Type; ptr unsafe.Pointer; flag}`，amd64 上共 24 字节。flag 低 5 位是 Kind，往上依次是 `flagStickyRO`(1<<5)、`flagEmbedRO`(1<<6)、`flagIndir`(1<<7)、`flagAddr`(1<<8)、`flagMethod`(1<<9)，再往上（`flagMethodShift = 10`）存方法号。
  - `ValueOf(i)` 调用 `unpackEface`：取出 eface 的类型字和数据字，类型不是 direct-iface 时置 `flagIndir`，**不会置 `flagAddr`**。所以 `ValueOf(p)` 拿到的是一份不可设置的副本，`ValueOf(&p).Elem()` 才可寻址。1.27 中 `ValueOf` 本身不强制参数逃逸（本机 `AllocsPerRun` 测得 `ValueOf([8]int{})` 为 0 次分配）。
  - `Field(i)` 从父值继承 `flagStickyRO | flagIndir | flagAddr`（清掉 `flagEmbedRO`），如果字段未导出，再叠加 `flagStickyRO`（嵌入字段则叠加 `flagEmbedRO`）。正因为 `flagEmbedRO` 在下一层被清掉，未导出的嵌入类型里的导出字段仍然可以访问。`mustBeAssignable` / `mustBeExported` 在每次 `Set*` / `Interface` 时都做这组位检查。
  - `Interface()` 调用 `packEface`。值带 `flagAddr` 时要先 `unsafe_New` + `typedmemmove` 拷贝出一个新对象（本机实测 1 次分配）。`reflect.TypeAssert[T]` 先用 nil 数据字试做一次断言，成功后再填数据，可以做到 0 次分配。
- **`Value.Call`**（`value.go` 的 `call`）：先 `methodReceiver`（方法值的情况）→ `funcLayout`（按"函数类型 + 接收者"缓存在 `layoutCache sync.Map`，帧对象从 `framePool` 取）→ 对每个参数 `assignTo` 检查可赋值性 → 按 ABI 把参数放进栈帧或 `abi.RegArgs` → `runtime.reflectcall`（reflect 通过 `//go:linkname call runtime.reflectcall` 引用它；amd64 汇编按帧大小 `DISPATCH` 到 `call16` … `call1073741824`）→ 把返回值包装成新的 `[]Value`。**整条路径编译器都看不到目标函数**，内联和逃逸分析在这里全部失效。
- **链接器方法裁剪**（`cmd/link/internal/ld/deadcode.go` 的注释和 `deadcode`）：方法被判定可达有三种途径——直接调用、与某个可达接口的方法签名匹配、反射。编译器在 `cmd/compile/internal/walk/expr.go` 的 `usemethod` 里识别对 `Method(int)` 和**非常量参数** `MethodByName` 的调用，给调用方函数打上 `AttrReflectMethod`。链接器一旦看到它，就**保留所有可达类型的所有导出方法**。如果 `MethodByName` 的参数是常量，编译器发出 `R_USENAMEDMETHOD` 重定位，只保留同名方法。`reflect` 文档里直接写着："Calling this method will force the linker to retain all exported methods in all packages."（`Value.Methods()` 迭代器也一样）
- **unsafe 的实现细节**
  - `uintptr` 不是指针：GC 不会更新它，也不会因为它而保活对象。goroutine 栈会被 `copystack` 整体搬迁（`runtime/stack.go`，`adjustpointers` 只调整真正的指针，见 [Ch10](../ch10-scheduler/)），指向栈变量的 `uintptr` 在搬迁后就成了野值。堆对象目前不移动（`reflect/deepequal.go` 的注释："Assumes non-moving garbage collector"），但这不是语言承诺。
  - **6 种合法模式**（`unsafe/unsafe.go` 文档注释的原编号）：(1) `*T1 → Pointer → *T2`，要求 T2 不大于 T1 且内存布局等价；(2) `Pointer → uintptr`，只用于打印等用途，不能再转回；(3) `Pointer → uintptr → 算术 → Pointer`，必须写在同一个表达式里，结果仍要落在原分配之内（与 C 不同，**不允许指向末尾之后**），也不能对 nil 做；(4) 在 `syscall.Syscall` 这类调用的实参列表里转换；(5) `reflect.Value.Pointer` / `UnsafeAddr` 的结果在同一表达式里立刻转成 `Pointer`；(6) `reflect.SliceHeader` / `StringHeader` 的 `Data` 字段（两者都已 Deprecated，改用 `unsafe.Slice` / `unsafe.String`）。`unsafe.Add` 同样受模式 (3) 的约束。
  - **`-d=checkptr`**：`cmd/compile/internal/base/flag.go` 规定，如果没有显式设置，`-race` / `-msan` / `-asan` 会隐含 `checkptr=1`。运行时检查在 `runtime/checkptr.go`：`checkptrAlignment` 抓含指针类型的未对齐转换，以及转换后的范围跨越多个堆对象；`checkptrArithmetic` 要求算术结果落在某个原始指针所在的对象里。`=2` 还会让转换为 `Pointer` 的值强制分配到堆上。Windows 上 `-race` 依赖 cgo，可以直接用 `-gcflags=all=-d=checkptr`。
  - **静态工具**：`go vet` 的 `unsafeptr` 检查可疑的 `uintptr → Pointer` 转换（报 "possible misuse of unsafe.Pointer"）；`go fix` 的 `unsafefuncs` 把 `unsafe.Pointer(uintptr(p) + n)` 改写成 `unsafe.Add(p, n)`。
  - **`//go:linkname`**：只能出现在 import 了 `"unsafe"` 的文件里（否则编译报 `//go:linkname only allowed in Go files that import "unsafe"`）。链接器默认 `-checklinkname=true`（`cmd/link/internal/loader/loader.go` 的 `checkLinkname`）：引用（pull）标准库符号时，只有定义方用 `//go:linkname` 显式 push 过的才放行，比如 `runtime/badlinkname.go`、`reflect/badlinkname.go` 中那批被广泛依赖的符号（见 go.dev/issue/67401）。用 `//go:linknamestd` push 的符号只允许标准库自己引用。其余情况链接失败，报 `invalid reference to runtime.xxx`（本机用 `runtime.gcount` 实测）。`-ldflags=-checklinkname=0` 可以关掉这项检查。
- **案例：json/v2 怎样用反射**（只看反射机制，不讲协议）
  - **入口**（`encoding/json/v2/arshal.go` 的 `marshalEncode`）：如果输入不是指针，先 `reflect.New` + `Set` 浅拷贝一份，得到可寻址的值。源码注释原文："It is beneficial to performance to always pass pointers to avoid this"。本机实测传值 2 allocs/128B，传指针 1 alloc/32B。拿到的值用 `addressableValue` 类型包装，保证之后调用 `Addr` / `Set` 不会 panic。
  - **类型缓存**：`lookupArshalerCache sync.Map // map[reflect.Type]*arshaler`。未命中时依次调用 `makeDefaultArshaler`（按 Kind 生成 marshal/unmarshal 闭包）、`makeMethodArshaler`（建缓存时一次性用 `Implements` 判断类型是否实现 `MarshalerTo` / `Marshaler` / `TextMarshaler` 等，调用时用 `reflect.TypeAssert` 取接口）、`makeTimeArshaler`，最后 `LoadOrStore`，保留先存进去的那一份。`MarshalToFunc[T]` 这类自定义函数用 `reflect.TypeFor[T]()` 作 key，缓存在 `typedArshalers.fncCache` 里。
  - **struct**（`arshal_default.go` 的 `makeStructArshaler`）：字段表用 `sync.Once` 惰性构建（`makeStructFields`）。这样递归类型在构造自身的 arshaler 时不会无限递归。`fields.go` 的 `makeStructFields` 用 BFS 展开嵌入字段并处理同名冲突，`parseFieldOptions` 解析 `sf.Tag.Lookup("json")`。规则：给未导出的非嵌入字段写 json tag 直接报错；`omitEmpty`、`omit_empty` 这种近似拼写报错；其余不认识的选项忽略。
  - **首次调用与稳态**：本机测得，第一次 `Marshal` 一个新类型有 91 次分配（在建缓存），之后每次只有 1 次。
  - **`jsontext` 的流式 API**：`Decoder.ReadToken` / `ReadValue` / `PeekKind` / `SkipValue`，`Encoder.WriteToken` / `WriteValue`。类型实现 `MarshalerTo`（`MarshalJSONTo(*jsontext.Encoder) error`）/ `UnmarshalerFrom` 后，可以自己驱动 token 流，不经过中间的 `[]byte`。
  - **v1 由 v2 实现**：`encoding/json/v2_*.go` 带 `//go:build goexperiment.jsonv2`（`internal/buildcfg/exp.go` 中 `JSONv2` 默认为 true），`json.Marshal` 就是 `jsonv2.Marshal(v, DefaultOptionsV1())`，`v2_inject.go` 在 `init` 里把 v1 的错误类型等钩子注入 `encoding/json/internal`。`GOEXPERIMENT=nojsonv2` 时回退到旧的 `decode.go` / `encode.go`（`!goexperiment.jsonv2`），此时 `encoding/json/v2` 没有可编译的文件，import 它会失败（本机实测）。
  - 整个 `encoding/json` 树**不 import `unsafe`，也不调用 `Method` / `MethodByName`**。方法探测全靠接口断言，所以不会破坏链接器的方法裁剪。

### 源码阅读清单（`$GOROOT/src`）
| 文件 | 函数 / 类型 | 带着什么问题读 |
|---|---|---|
| `internal/abi/type.go` | `Type`、`TFlag*`、`UncommonType`、`Method`、`StructType`、`Name`、`TypeFor` | 方法表为什么用偏移量而不是指针？`TFlagUncommon` 让哪些类型省掉了这块内存？tag 存在哪里？ |
| `cmd/compile/internal/reflectdata/reflect.go` | `writeType`、`dcommontype`、`dextratype`、`methods` | 哪些类型的描述符会被生成？方法表的排序在哪里决定？ |
| `reflect/value.go` | `Value`、`flag*`、`unpackEface`、`packEface`、`Field`、`CanSet`、`call` | `flagAddr` 从哪里来、在哪里丢失？`Interface()` 什么时候分配？一次 `Call` 有几处分配？ |
| `runtime/stubs.go` · `runtime/asm_amd64.s` | `reflectcall`、`DISPATCH`、`CALLFN` | 为什么要按帧大小准备一组 `callNN`？返回值里的指针怎么对 GC 可见？ |
| `cmd/compile/internal/walk/expr.go` · `cmd/link/internal/ld/deadcode.go` | `usemethod`、`deadcode`、`markableMethods` | 常量和非常量 `MethodByName` 在链接器眼里有什么不同？为什么只保留"导出"方法？ |
| `unsafe/unsafe.go` · `runtime/checkptr.go` | 6 种模式、`checkptrAlignment`、`checkptrArithmetic`、`checkptrBase` | 哪些违规是 checkptr 原理上抓不到的？ |
| `cmd/link/internal/loader/loader.go` · `runtime/badlinkname.go` | `checkLinkname`、`blockedLinknames` | push 与 pull 的不对称是怎样实现的？为什么只检查标准库符号？ |
| `encoding/json/v2/arshal.go` · `arshal_default.go` · `fields.go` | `lookupArshaler`、`makeStructArshaler`、`makeStructFields`、`parseFieldOptions` | 缓存 key 为什么是 `reflect.Type`？递归类型怎样避免无限递归？ |

## 📊 Java 25 ↔ Go 1.27 机制级对照表

| 维度 | Java 25（HotSpot） | Go 1.27 | 取舍 / 设计动机 |
|---|---|---|---|
| 类型信息来源 | 运行时加载、链接、初始化，`Class` 是 Metaspace 元数据的镜像 | 编译器生成、链接器放进只读 types 段的 `abi.Type`，用偏移量互相引用 | Go 是封闭世界：所有类型在链接期已知；Java 是开放世界：任何时刻都可能出现新类 |
| 运行时生成代码 | `defineHiddenClass` + Class-File API；lambda 代理类就这样生成 | 没有。`StructOf` 不带提升方法，`MakeFunc` 只是通用桩 | Go 不需要在运行时装载代码的机制，换来静态二进制和全程序裁剪 |
| 类身份 | 二进制名 + 定义加载器 | 描述符地址（`TypeOf(a) == TypeOf(b)`），tag 参与同一性 | Java 的类可以按加载器隔离、按加载器卸载；Go 的类型同一性在编译期就定了 |
| 反射读写字段 | `Field.get/set`，底层方法句柄；`setAccessible` 绕过语言访问控制（受模块强封装约束） | `Value.Field(i).SetInt`，每次检查 `flagAddr`/`flagRO`；未导出字段没有合法的写入后门 | Go 让包的封装在反射下同样成立；想突破只能用 unsafe，代价明面上看得见 |
| 反射调用 | `Method.invoke` → `DirectMethodHandleAccessor`；常量句柄可被 JIT 内联 | `Value.Call` → `funcLayout` → `reflectcall`，有分配，不可内联 | Java 把优化交给 JIT；Go 没有 JIT，反射只能保证正确，不追求快 |
| 访问检查时机 | MethodHandle 在创建时检查；`Method.invoke` 每次调用都检查（除非 override） | 权限位随 `Value` 一路传递，每次操作做一次位测试 | 本质都是"能力"模型：Java 的能力是 Lookup，Go 的能力是 flag |
| 方法裁剪 | JDK 不做全程序方法裁剪（jlink 只按模块裁剪） | 链接器默认裁剪；`Method`/非常量 `MethodByName` 让裁剪退化 | 静态裁剪的前提是名字可知，反射按名字调用正好破坏这个前提 |
| 越权内存访问 | `sun.misc.Unsafe` 内存访问方法弃用，默认 `warn`，可设 `deny`；替代品是 VarHandle / FFM | `unsafe` 一直可用，规则写在文档里，由 `vet`/`checkptr` 部分兜底 | Java 把后门换成有生命周期和边界检查的 API；Go 保留后门，但豁免兼容性承诺 |
| 指针与 GC | 引用由 GC 精确追踪，对象会移动；Unsafe 用 (对象, 偏移) 寻址而不是裸地址 | `unsafe.Pointer` 被 GC 追踪，`uintptr` 不被追踪；栈会搬迁，堆目前不移动 | 两边都不让裸地址跨越 GC 点；Java 用"对象 + 偏移"的 API 形态来保证，Go 用规则加工具 |
| 运行时内部的后门 | JEP 403 强封装，用 `--add-opens` 显式开口 | `-checklinkname` 默认开启，对被广泛依赖的符号 push 白名单 | 同一个问题：生态依赖内部细节，导致运行时无法演进 |

## 🔬 实验

### 实验 1：反射成本谱（必做）
- **目的**：给"反射慢多少、慢在哪里"拿出数据，并找出每一次分配的来源。
- **方法**：在 `go/lab/reflcost` 里用 `type Player struct{ ID int64; HP int32; Name string; mp int32 }` 和一个 `//go:noinline` 的 `(*Player).AddHP`，写一组 benchmark：直接写字段、`v.Field(1).SetInt`、`v.FieldByName("HP").SetInt`、直接调用、接口调用、`MethodByName("AddHP").Call`（方法值）、`ValueOf((*Player).AddHP).Call`（方法表达式）。再加上 `Field(i).Interface()` 与 `reflect.TypeAssert[int32]` 的分配对比。
- **工具**：`go test -bench=. -benchmem -count=10 | benchstat`；用 `-memprofile` 加 `go tool pprof -list 'reflect.Value.call'` 定位分配。
- **预期现象（先写数量级预测）**：按下标设字段是几 ns、0 次分配；`FieldByName` 慢一个数量级（字符串比较）；`Call` 比直接调用慢约两个数量级，每次 2–3 次分配；方法值版本比方法表达式版本多一次分配。
- **Java 对照**（`java/ReflectTiming.java`，简单计时加充分预热，结论在 [Ch17](../ch17-profiling-bench/) 用 JMH 复核）：直接调用、`static final` MH 的 `invokeExact`、非常量 MH、`static final Method` 的 `invoke`、非常量 `Method.invoke`。预期常量 MH 和直接调用处于同一量级，非常量版本慢数倍，而且都远快于 Go 的 `Value.Call`。
- **记录**：两边各自的 ns/op、allocs/op 表；每次分配对应的源码行；对"为什么 Java 的反射能被 JIT 救回来"的解释。

### 实验 2：`MethodByName` 让链接器放弃裁剪（必做）
- **目的**：验证 deadcode 的三种可达途径，以及常量参数的特殊处理。
- **方法**：给 `type Big struct{...}` 定义 3 个体积较大的导出方法 `ExportedA/B/C`，都不直接调用，然后把 `&Big{}` 赋给 `any`。写三个变体：A 只调用 `NumMethod()`；B 调用 `MethodByName("ExportedA").Call(nil)`；C 调用 `MethodByName(os.Args[0])`。
- **工具**：`go build` 后比较文件大小；`go tool nm x.exe | grep 'main.(\*Big)'`。
- **预期现象**：A 不保留任何 `Big` 方法的代码，但 `NumMethod()` 仍返回 3（方法表条目还在，只是 `TextOff` 被写成 -1）；B 只保留 `ExportedA`；C 保留全部三个，而且二进制明显变大（所有包里可达类型的导出方法都被留下，本机是 MB 量级）。
- **记录**：三个变体的大小和符号列表，以及你对 `-1` 哨兵为什么不会被调用到的解释。

### 实验 3：`vet`、`checkptr` 与违约后果（必做）
- **目的**：分清静态检查和动态检查各自的覆盖范围，并亲眼看到违约造成的静默损坏。
- **方法**：在 `go/lab/unsafebad` 里写三个非法转换：① 把 `&buf[1]` 转成 `*[2]*int`（含指针类型且未对齐）；② 把 24 字节分配的首地址转成 `*[8]uint64` 再读第 5 个元素；③ 先把 `uintptr(p)` 存进变量，再加偏移转回 `Pointer`。另外写两个违约演示：④ `s := unsafe.String(unsafe.SliceData(b), len(b))` 作为 map key 之后修改 `b[0]`，再用新旧两个字符串分别查找；⑤ 对字符串字面量用 `unsafe.Slice(unsafe.StringData(lit), n)` 写入。
- **工具**：`go vet ./...`；`go build -gcflags=-d=checkptr` 与默认构建分别运行。
- **预期现象（先逐条预测"vet 抓不抓、checkptr 抓不抓"）**：`vet` 只能报出 ③；`checkptr` 对 ①②③ 分别报 `misaligned pointer conversion`、`converted pointer straddles multiple allocations`、`pointer arithmetic result points to invalid allocation`；默认构建下三者都"正常运行"。④ 的结果是新旧两个键都查不到（哈希按旧字节算，比较时用新字节）；⑤ 写只读段，进程 fault。
- **记录**：一张"违规 × 工具"矩阵，以及 checkptr 原理上抓不到的情形（例如结果恰好落在另一个合法对象里）。

### 实验 4：json/v2 的类型缓存
- **目的**：验证"一次反射建计划，稳态只走缓存的闭包"。
- **方法**：用 `runtime.ReadMemStats` 统计首次 `json.Marshal(&Fresh{})` 和后续调用的 `Mallocs` 差值（Windows 上计时分辨率不够，改用分配次数作指标）；benchmark 对比传值和传指针；对同一个 `encoding/json`（v1）程序分别在默认构建和 `GOEXPERIMENT=nojsonv2` 构建下比较 allocs/op。
- **预期现象**：首次调用的分配次数比稳态高一到两个数量级；传值比传指针多一次分配和一次整值拷贝；两种 v1 实现的 allocs/op 不同，差异的方向留给你预测。
- **记录**：三组数据，以及与 `lookupArshaler`、`marshalEncode` 源码的对应关系。

## 🧪 练习规划

**Go：`go/exercises`**（`go test ./ch09-reflect-unsafe/go/...`；unsafe 相关测试另跑一遍 `go test -gcflags=all=-d=checkptr`）
1. `func Validate(v any) error`：按 `check:"min=1,max=100,nonzero"` 这样的 tag 校验结构体。只接受指向 struct 的指针；跳过未导出字段；递归进入嵌入 struct；用 `Lookup` 区分"没有 tag"和"空 tag"；解析结果按 `reflect.Type` 缓存在 `sync.Map` 里。测试覆盖各种非法 tag，并用 `testing.AllocsPerRun` 断言缓存命中之后校验合法输入为 0 次分配。
2. `func SetPath(dst any, path string, val any) error`：按 `"Stats.HP"` 这样的路径设置嵌套字段。测试必须覆盖：传值而不是指针（不可寻址）、路径经过未导出字段、`val` 不可赋值（`AssignableTo`）、路径经过 nil 指针。每种情况都返回明确的错误，而不是 panic。
3. `func BytesToString(b []byte) string` 与 `func StringToBytes(s string) []byte`：零拷贝实现。在注释里写清各自的安全前提（谁不能再写、返回的切片能不能写、空串和 nil 时返回什么）。测试断言 0 次分配、空输入、nil 输入；另写一个默认 `t.Skip` 的测试复现实验 3 中 ④ 的 map 键损坏。
4. `func FieldPtr[T any](p any, name string) (*T, error)`：用 `reflect.StructField.Offset` 加 `unsafe.Add` 取字段指针，要求在 `-d=checkptr` 下通过，并用 `reflect.TypeFor[T]()` 校验字段类型。在注释里标明这段代码用的是合法模式里的哪一条。
5. `func DeepEqualLite(a, b any) bool`：自己实现 `reflect.DeepEqual` 的一个子集（bool、数值、string、array、slice、map、struct、pointer），用 visited 集合处理环。和 `reflect.DeepEqual` 做表驱动对拍，必须覆盖：循环链表、NaN、nil slice 与空 slice、含未导出字段的 struct。

**Java：`java/drills`**
1. `ReflectDrill.java`：分别用 `Method.invoke`、`MethodHandle`（`findVirtual`）、`VarHandle`（`privateLookupIn` + `findVarHandle`）读写同一个私有字段。自校验三条：对 `String.value` 调用 `setAccessible(true)` 抛 `InaccessibleObjectException`；从一个没有权限的 Lookup 去 `findVirtual` 私有方法时，**在创建句柄那一刻**就抛 `IllegalAccessException`；两次 `getDeclaredFields()` 返回的数组不是同一个对象。
2. `HiddenClassDrill.java`：用 Class-File API 生成一个实现 `IntUnaryOperator` 的类（`applyAsInt` 返回 `x * 2`），通过 `defineHiddenClass(..., NESTMATE)` 加载。自校验 `isHidden()`、`Class.forName` 失败、调用结果正确。再把同一份字节码交给两个独立的 `ClassLoader` 各定义一次（非 hidden），校验得到的两个 `Class` 不相等。
3. （可选）`UnsafeOptionDrill.java`：用 `ProcessBuilder` 启动子 JVM，分别带 `--sun-misc-unsafe-memory-access=warn` 和 `deny` 调用 `Unsafe.getInt`。自校验前者的 stderr 中含 `WARNING: A terminally deprecated method in sun.misc.Unsafe`，后者以 `UnsupportedOperationException` 退出。

## 🧭 设计取舍

1. **Go 为什么没有类加载器和运行时代码生成。** 按 spec「Program initialization and execution」，程序就是链接期确定的一组包。由此带来的好处是：类型描述符可以是只读数据，itab 可以静态生成（[Ch05](../ch05-interfaces/)），链接器可以按可达性裁剪方法，产物是不依赖运行时装载器的自包含二进制。代价同样直接：运行时造不出带方法的类型（`StructOf` 不支持提升方法），`MakeFunc` 只能走一个通用桩，反射调用无从专门化，也做不了类级别的热替换。Java 选了相反的一端：动态加载是 JVMS 第 5 章的核心，换来 Instrumentation、hidden class、按需生成的 lambda 代理类，以及 JIT 基于"当前已加载的类层次"做的 CHA 和去优化。代价是类加载器泄漏、预热时间和 Metaspace。两种选择都不是偶然的：Go 的封闭世界假设让链接器拥有全局视角，Java 的开放世界假设把这份全局视角交给了运行时的 JIT。
2. **为什么 `reflect` 被设计成"慢但安全"。** "The Laws of Reflection" 的第三定律（要修改必须可设置）落到实现上，就是 flag 位随 `Value` 传递、每次操作都检查。`flagRO` 让包的封装在反射下依然成立：Go 没有 `setAccessible` 那样的合法后门，想越界只能 import `unsafe`，而这在代码里是看得见的。性能路径上 Go 鼓励的是代码生成（`go generate`）或泛型（[Ch06](../ch06-generics/)），json/v2 自己的做法则是"首次用反射构建计划，稳态只跑缓存的闭包"。Java 的路线是 JEP 416：用方法句柄这一套基础设施取代原先"VM 原生 accessor + 运行时生成字节码 accessor"两套实现，再靠 JIT 把常量句柄内联。**这个设计在 Go 里换不来性能，因为 Go 没有 JIT**。所以 Go 反射的定位只能是保证正确，而不是追求快。
3. **为什么 unsafe 是一个包而不是语言特性。** `import "unsafe"` 能被 grep 到，也能被 vet 和代码审查精确定位；spec 只规定"这些转换存在"，效果是 implementation-defined；具体规则写在包文档里，而不是写进类型系统。Go 1 兼容性文档把它明确列为例外，原文："Packages that import unsafe may depend on internal properties of the Go implementation. We reserve the right to make changes to the implementation that may break such programs."（go.dev/doc/go1compat）。之所以要留这扇有名字的门，是因为 runtime、reflect、syscall、`strings.Builder` 自身都要用它来自举。代价是违约后果往往是静默的数据损坏，而不是崩溃（实验 3 的 ④）。"`uintptr` 不是指针"则是精确 GC 加上栈搬迁的直接推论：如果允许整数冒充指针，GC 就必须保守扫描，栈也就不能移动。
4. **两边都在收紧通向运行时内部的后门。** 问题是同一个：生态依赖了运行时的内部细节，运行时就无法演进。Go 的做法：链接器默认 `-checklinkname`，对已被广泛 pull 的符号在定义侧补上 push 来维持兼容（源码里称之为 "hall of shame"，go.dev/issue/67401），新代码不能再 pull 未公开的符号。Java 的做法：JEP 403 强封装，`--add-opens` 作为显式的逃生口；JEP 471/498 对 `Unsafe` 内存访问分阶段推进（弃用 → 默认告警 → 计划默认拒绝 → 移除），并且是在替代品 VarHandle / FFM 就位之后才开始。差别在于表达能力：Java 有模块系统，可以声明"谁可以访问"；Go 只能在链接器里按符号维护名单。
5. **反射为什么会让链接器"失明"。** 静态裁剪的前提是知道每个方法会不会被调用，而 `MethodByName(s)` 在编译期不知道 `s` 是什么。Go 的做法是保守但精确地分级：常量名字只保留同名方法，非常量名字或 `Method(i)` 保留全部导出方法（未导出方法反射本来就调不到，所以可以照样裁掉）。json/v2 刻意只用接口断言探测方法，就是为了不触发这个退化。Java 处在开放世界里，任何类都可能在运行时被加载或被反射调用，所以 JDK 不做全程序方法裁剪，jlink 只按模块裁剪。这是同一枚硬币的两面：Go 是"默认裁剪，反射时退化"，Java 是"默认不裁剪，所以反射不需要特殊对待"。

## ✅ 验收答辩题

- [ ] [L2] 画出一个带两个方法的具名 struct 类型的描述符内存图：`abi.Type`、`StructType`、`UncommonType`、方法表，以及 tag 存在哪里。
- [ ] [L2] `reflect.ValueOf(p).Field(0).SetInt(1)` 为什么 panic，而 `reflect.ValueOf(&p).Elem().Field(0).SetInt(1)` 不会？用 `flagAddr` 的来源和去向回答。未导出字段为什么能 `Int()` 却不能 `Interface()`？
- [ ] [L2] 逐步说明一次 `Value.Call` 在 reflect 和 runtime 里经过了哪些函数，以及它为什么不可能被内联。
- [ ] [L2] JVMS 第 5 章里，加载、验证、准备、解析、初始化各做什么？一个类什么时候可以被卸载？hidden class 在卸载上有什么不同？
- [ ] [L2] JEP 416 之后，`Method.invoke` 的调用链是什么？哪 5 种情况会回退到原生 accessor？MethodHandle 和 `Method.invoke` 的访问检查分别发生在什么时候？
- [ ] [L3] 先预测、再用实验 1 的数据说明：按下标设字段、`FieldByName`、`Value.Call` 分别比直接操作慢多少？每一次分配来自哪一行源码？Java 的常量 MH 为什么能接近直接调用？
- [ ] [L3] 给出实验 2 三个变体的符号表和大小，解释 A 变体里 `NumMethod()` 为什么仍然是 3。
- [ ] [L3] 拿出实验 3 的矩阵：哪些违规只有 checkptr 能抓，哪些只有 vet 能抓？构造一个两者都抓不到、但确实违反 6 条模式的例子。
- [ ] [L3] 预测 `json.Marshal(v)` 传值与传指针在 allocs/op 上的差别，用 `marshalEncode` 的源码解释，再用 benchmark 证实。
- [ ] [L4] Go 为什么不提供类加载器和运行时代码生成？换成 Java 式的开放世界，链接器裁剪、itab 静态生成、静态二进制分别会失去什么？
- [ ] [L4] 对比 Go 的 linkname 限制和 Java 的强封装加 Unsafe 退场：两者要解决的问题、采用的机制、兼容性策略各是什么？引用 go.dev/issue/67401 与 JEP 403/471/498 的原文。

## 🕳️ 常见坑

- **对 `reflect.ValueOf(x)` 调用 `Set`**：拿到的是副本，不可寻址。要用 `ValueOf(&x).Elem()`。
- **把 `reflect.Type` 当成 Java 的 `Class` 那样随手查**：`FieldByName` 和 `MethodByName` 都要查找；热路径上应该按 `reflect.Type` 缓存字段下标或闭包，json/v2 就是这样做的。
- **反射里用 `Method(i)` 或变量名调用 `MethodByName`**：二进制会悄悄保留所有导出方法。能用接口断言就别按名字反射调用。
- **tag 写成 `json: "x"` 或 `json:'x'`**：`Get` 静默返回空串。让 `go vet` 的 `structtag` 检查一直开着。
- **`u := uintptr(p)` 存起来稍后再转回 `Pointer`**：GC 不保活、栈搬迁后失效。必须写在同一个表达式里，或者直接用 `unsafe.Add`。
- **零拷贝转换之后又改了原 `[]byte`**：字符串的不可变性被破坏，map 键静默损坏；从字符串零拷贝得到的 `[]byte` 一旦写入，碰上只读段就直接 fault。
- **以为 `vet` 干净就安全**：`unsafe` 包文档原文是 "silence from go vet is not a guarantee that the code is valid"。用到 unsafe 的测试要再跑一遍 `-d=checkptr`。
- **Java 侧每次调用都 `getDeclaredMethod`**：返回的是拷贝，还要重复做访问检查。应该缓存 `Method`，或者干脆用 `static final MethodHandle`。
- **升级到 JDK 25 后忽略启动日志里的 `sun.misc.Unsafe` 警告**：默认 `warn` 只打印第一次，可以用 `debug` 拿到每次调用的栈，再用 `deny` 提前验证依赖库能否离开 Unsafe。

## 📚 一手资料

- Go spec：[Package unsafe](https://go.dev/ref/spec#Package_unsafe)、[Struct types](https://go.dev/ref/spec#Struct_types)、[Type identity](https://go.dev/ref/spec#Type_identity)、[Conversions](https://go.dev/ref/spec#Conversions)、[Address operators](https://go.dev/ref/spec#Address_operators)、[Program initialization and execution](https://go.dev/ref/spec#Program_initialization_and_execution)
- Go 文档：`go doc unsafe`（6 种模式）、`go doc reflect`、[The Laws of Reflection](https://go.dev/blog/laws-of-reflection)、[Go 1 compatibility](https://go.dev/doc/go1compat)、`go doc encoding/json/v2`、`go doc encoding/json/jsontext`、go.dev/issue/67401（linkname 限制）
- Go 源码：`internal/abi/type.go`、`reflect/type.go`、`reflect/value.go`、`reflect/deepequal.go`、`runtime/type.go`、`runtime/checkptr.go`、`cmd/compile/internal/reflectdata/reflect.go`、`cmd/compile/internal/walk/expr.go`、`cmd/link/internal/ld/deadcode.go`、`cmd/link/internal/loader/loader.go`、`encoding/json/v2/`、`encoding/json/v2_*.go`
- Java 规范：JVMS 第 5 章（Loading, Linking, and Initializing）、JLS §12.4（类初始化）、§12.7（类卸载）、JVM TI RedefineClasses
- JEP：[416](https://openjdk.org/jeps/416)（核心反射基于方法句柄）、[403](https://openjdk.org/jeps/403)（强封装）、[371](https://openjdk.org/jeps/371)（hidden class）、[484](https://openjdk.org/jeps/484)（Class-File API）、[471](https://openjdk.org/jeps/471) / [498](https://openjdk.org/jeps/498)（Unsafe 内存访问的弃用与告警）、[193](https://openjdk.org/jeps/193)（VarHandle）
- JDK 源码（`src.zip`）：`java/lang/ClassLoader.java`、`java/lang/Class.java`、`java/lang/reflect/Method.java`、`java/lang/invoke/MethodHandles.java`、`java/lang/invoke/InnerClassLambdaMetafactory.java`、`java/lang/classfile/ClassFile.java`、`jdk/internal/reflect/MethodHandleAccessorFactory.java`、`jdk/internal/reflect/DirectMethodHandleAccessor.java`、`jdk.unsupported/sun/misc/Unsafe.java`、`java.instrument/java/lang/instrument/Instrumentation.java`
