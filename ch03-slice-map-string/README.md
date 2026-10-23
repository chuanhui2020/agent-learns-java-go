# Ch03 · slice · string · map 底层

> Part 1 · 估时 12h · 状态：⚪ 未开始 · 三种最常用的"内建引用类型"，各自的内存结构、增长策略和陷阱。Go 程序的大部分分配和别名 bug 都和它们有关

## 🎯 目标（资深标准）

- **L1**：正确使用 append、切片表达式、copy、clear、`strings.Builder`、map 的增删查和遍历；能写出 UTF-8 安全的字符串处理。
- **L2**：讲清 slice header 与底层数组的关系、`growslice` 的容量增长公式（含 size class 取整）；讲清 Swiss table map 的 group / 控制字 / h1-h2 / 目录 / 表分裂；讲清 `[]byte`↔`string` 转换在哪些情况下被编译器优化掉。
- **L3**：先预测再验证：给定 append 序列预测每次扩容后的 cap（含 size class 取整），给定一段字符串处理代码预测 allocs/op；用数据比较 Go map 与 Java HashMap 在大规模插入时的**单次插入最大延迟**，并用"增量分裂 vs 一次性 resize"解释差异。
- **L4**：能讲清三处设计选择的动机与代价：slice 为什么是"共享底层数组的 header"而不是 Java 那样的封装对象；map 为什么刻意随机化遍历顺序、为什么不支持并发访问；string 为什么是 UTF-8 字节序列而 Java 选了 UTF-16（以及紧凑字符串补救了什么）。

## 🔗 前置

[Ch02](../ch02-values-memory/)

## 💡 关键洞察

1. **slice 是"值语义的 header + 共享的底层数组"。** 绝大多数 slice bug 都是别名问题：两个 header 指向同一块数组，其中一个 append 时容量还够，就覆盖了另一个看得见的数据。
2. **Go 的 map 扩容是增量的、按表分裂的。** 每张表最多 1024 个槽位，满了只分裂这一张，不会像 Java `HashMap.resize()` 那样一次性重排整个表。大 map 的插入延迟毛刺因此有上界。
3. **Go 字符串是 UTF-8 字节序列，Java 字符串是 Latin-1/UTF-16 编码的 char 序列。** `len(s)` 是字节数，`range s` 按 rune 遍历；Java 的 `length()` 是 UTF-16 码元数。玩家昵称、聊天截断这种需求，两边的正确写法完全不同。

## ☕ Java 25 深度复习

### 集合
- **ArrayList**：`elementData` 是 `Object[]`。扩容在 `grow(int minCapacity)` 里调用 `ArraysSupport.newLength(old, minGrowth, old >> 1)`，即约 1.5 倍。元素是引用，扩容只拷贝引用。`subList` 返回**视图**，和 Go 的子切片一样共享底层数组；原列表发生结构性修改后，视图会抛 `ConcurrentModificationException`（Go 没有这层保护）。
- **HashMap**：`hash()` 做 `h ^ (h >>> 16)` 扰动；负载因子 `DEFAULT_LOAD_FACTOR = 0.75`；桶内节点数达到 `TREEIFY_THRESHOLD = 8` 且表容量 ≥ `MIN_TREEIFY_CAPACITY = 64` 时树化为红黑树，降到 `UNTREEIFY_THRESHOLD = 6` 时退化回链表。`resize()` 一次性把容量翻倍，并把每个桶拆成高低两条链表，**整个过程在某一次 put 里同步完成**，这就是扩容毛刺的来源。
- **ConcurrentHashMap**：空桶用 CAS 插入，非空桶对桶头加 `synchronized`；计数用 `CounterCell` 分散（与 `LongAdder` 同一思路）；扩容时用 `ForwardingNode` 标记已迁移的桶，**多个线程协作迁移**。Go 没有内建的并发 map，对应的方案见 [Ch12](../ch12-sync-memory-model/)（`sync.Map`、分片加锁）。
- **Sequenced Collections（JEP 431）**：`SequencedCollection` 的 `getFirst`/`getLast`/`reversed`，统一了 List、Deque、LinkedHashSet 等有序集合的接口。Go 用 `slices` 包的函数和迭代器做同样的事（[Ch07](../ch07-iterators/)）。

### 字符串
- **紧凑字符串**：`String` 内部是 `byte[] value` + `byte coder`（LATIN1 或 UTF16），由 `COMPACT_STRINGS` 控制（`-XX:+CompactStrings`，默认开）。纯 Latin-1 内容每字符 1 字节，否则 2 字节。
- **substring 会拷贝**：Java 的 `substring` 复制出新数组，不共享原字符串的内存；Go 的 `s[i:j]` 共享，小子串可能拖住大字符串不释放。
- **字符串去重**：`-XX:+UseStringDeduplication`（默认关）让 GC 合并内容相同的 `byte[]`。Go 的对应物是 `unique.Make` 驻留（[Ch16](../ch16-gc/)）。
- **字符串拼接**：`+` 由 `StringConcatFactory` 经 invokedynamic 生成拼接策略（`StringConcatHelper` 实现）。Go 的 `+` 由运行时 `concatstrings` 实现，热路径用 `strings.Builder`。
- **码点**：`codePointAt`、`codePoints()`。emoji 这类补充平面字符在 Java 里占两个 `char`（代理对），截断时必须按码点处理。

## 🐹 Go 1.27 深入

### 规范层
- **slice**：`s[low:high]` 共享底层数组，新 cap = `cap(s) - low`；**三下标切片** `s[low:high:max]` 把 cap 限制为 `max - low`，下次 append 必然分配新数组，是切断别名的标准手段。
- **append**：容量够时原地写入并返回同一底层数组；不够时分配新数组并拷贝。所以必须写成 `s = append(s, x)`。
- **copy / clear**：`copy` 返回实际拷贝的元素数，支持重叠区间；`clear(s)` 把元素置零（长度不变），`clear(m)` 删除 map 的所有条目。
- **string**：不可变的字节序列；`range` 按 UTF-8 解码出 rune，非法字节解码为 `U+FFFD`；`string(rune)` 和 `string(int)` 的含义容易出错（vet 有 `stringintconv` 检查）。
- **map**：key 必须可比较；遍历顺序未定义（实现上会刻意随机化）；遍历过程中删除是安全的，遍历过程中插入的新 key 可能被遍历到，也可能不会；**map 元素不可寻址**。
- **1.27 新增**：`strings.CutLast` / `bytes.CutLast`，按**最后一次**出现的分隔符切分。

### 运行时与编译器层
- **slice header**：`runtime/slice.go` 中的 `type slice struct { array unsafe.Pointer; len, cap int }`，在 amd64 上 24 字节。
- **增长策略**：`growslice` → `nextslicecap`。需求超过 2 倍时直接用需求值；旧容量 < 256 时翻倍；之后每轮 `newcap += (newcap + 3*256) >> 2`，从 2 倍平滑过渡到约 1.25 倍。最后按元素大小换算成字节数，再由 `roundupsize` 对齐到 size class，所以**实际 cap 往往比公式算出来的大**。
- **string 与 []byte 的转换**：一般情况下都要分配并拷贝（`slicebytetostring`、`stringtoslicebyte`）。编译器会在以下场景省掉拷贝：
  - `m[string(b)]` 查 map；
  - 比较 `string(b) == "x"`；
  - 字符串拼接中的临时转换；
  - `for i, c := range []byte(s)`。
  对于不逃逸的短结果，运行时还会用 32 字节的栈上临时缓冲（`tmpBuf`）。
- **strings.Builder**：内部是 `[]byte`，`String()` 用 unsafe 直接把字节转成字符串，不再拷贝。它保存了指向自身的 `addr` 字段，按值拷贝 Builder 后再写入会 panic。
- **Swiss table map（`internal/runtime/maps`）**：
  - **小 map**：不超过 8 个元素时只有一个 group，`dirPtr` 直接指向它，不需要目录。
  - **group**：8 个槽位（`abi.MapGroupSlots = 8`）加一个 8 字节控制字（`ctrlGroup`），每个槽一个控制字节：空 / 已删除 / 已占用 + 哈希低 7 位（h2）。
  - **查找**：哈希的高位（h1）决定从哪个 group 开始探测（`probeSeq`）。在 group 内用 `matchH2` 一次比较 8 个控制字节（amd64 上是 SIMD intrinsic），命中后才比较真正的 key。
  - **负载上限**：平均每个 group 最多 7 个元素（`maxAvgGroupLoad = 7`，即 7/8）。
  - **扩容**：map 是一个由多张表组成的目录（`directoryIndex` 用哈希高位选表），每张表最多 `maxTableCapacity = 1024` 个槽位。表没满时 `grow` 翻倍；满了就 `split` 成两张，目录必要时翻倍。**每次只动一张表**，扩容成本有上界。
  - **不缩容**：删除元素不会缩小 map，源码里还留着 `TODO: shrink directory?`。大 map 清空以后想释放内存，只能新建一个 map。
  - **并发检测**：有写标志位；检测到并发写会触发 `fatal("concurrent map writes")`。这是 fatal error，**`recover` 接不住**，进程直接退出。
  - **遍历随机化**：`Iter.Init` 中 `entryOffset`、`dirOffset` 取随机数，刻意打破任何对顺序的依赖。
- **1.27 新增 `hash/maphash.Hasher[T]`**：定义了"类型 T 的值如何哈希、如何判等"（`Hash(*maphash.Hash, T)` + `Equal(T, T) bool`），作为自定义哈希表、布隆过滤器的标准契约；`ComparableHasher[T]` 是可比较类型的默认实现（基于 `WriteComparable` 和 `==`）。

### 源码阅读清单（`$GOROOT/src`）
| 文件 | 函数 / 类型 | 带着什么问题读 |
|---|---|---|
| `runtime/slice.go` | `slice`、`growslice`、`nextslicecap` | 为什么阈值是 256？为什么增长后的 cap 还要再取整一次？溢出是怎么检测的？ |
| `runtime/msize.go` | `roundupsize` | 容量向上取整到 size class，对内存利用率有什么影响？ |
| `runtime/string.go` | `slicebytetostring`、`stringtoslicebyte`、`slicebytetostringtmp`、`concatstrings`、`tmpBuf` | 哪些转换用了栈上临时缓冲？`slicebytetostringtmp` 为什么可以不拷贝？ |
| `strings/builder.go` | `Builder`、`copyCheck`、`String` | `String()` 为什么能做到零拷贝？为什么不能按值拷贝 Builder？ |
| `internal/runtime/maps/group.go` | `ctrlGroup`、`matchH2`、`maxAvgGroupLoad` | 一次比较 8 个控制字节的位运算技巧是什么？已删除的槽位为什么不能直接标成空？ |
| `internal/runtime/maps/map.go` | `Map`、`h1`、`h2`、`directoryIndex`、`getWithKeySmall`、`PutSlot`、`growToSmall`、`growToTable` | 小 map 和大 map 的结构切换发生在什么时候？并发写是怎么检测出来的？ |
| `internal/runtime/maps/table.go` | `probeSeq`、`grow`、`split`、`rehash`、`Iter` | 表分裂时，正在进行的迭代器怎么保证不重复、不遗漏？ |
| `hash/maphash/hasher.go` | `Hasher`、`ComparableHasher` | 为什么契约里要同时有 Hash 和 Equal？自定义 Hasher 必须满足什么不变量？ |

## 📊 Java 25 ↔ Go 1.27 机制级对照表

| 维度 | Java 25 | Go 1.27 | 取舍 / 设计动机 |
|---|---|---|---|
| 动态数组 | `ArrayList`：对象，元素是引用，约 1.5 倍扩容 | slice：24 字节 header + 连续数组，2 倍过渡到 1.25 倍，再按 size class 取整 | slice 是语言内建的轻量视图，传参只拷 header；代价是 header 是值，别名问题要自己管 |
| 子序列 | `subList` 视图 + CME 保护 | 子切片共享数组，没有任何保护 | Go 把子切片做成零成本操作，不维护 modCount；要隔离就用三下标切片 / `slices.Clone` |
| 哈希表结构 | 桶数组 + 链表 / 红黑树 | Swiss table：group + 控制字 + 目录分表 | Swiss table 用控制字批量探测换取连续访存；Java 的树化是为了抵御哈希碰撞攻击，Go 靠每个 map 的随机种子 |
| 扩容 | `resize()` 一次性翻倍并迁移全部条目 | 按表增量分裂（每表 ≤ 1024 槽） | 分表让单次插入的最坏代价有上界；HashMap 的单表设计更简单 |
| 缩容 | 不缩容 | 不缩容 | 两边都认为缩容的抖动不值得，要回收只能"新建替换" |
| 并发 | `ConcurrentHashMap`（CAS + 桶锁 + 协作扩容） | 内建 map 不支持并发，检测到就 fatal | Go 不让常见情况（单 goroutine 访问）为同步付费；并发写直接 fatal，而不是悄悄损坏数据 |
| 遍历顺序 | HashMap 顺序不保证但稳定；LinkedHashMap 有序 | 每次遍历刻意随机 | 随机化防止代码依赖实现细节，也给实现留出改动空间（Swiss table 替换就受益于此） |
| 字符串编码 | Latin-1 / UTF-16（紧凑字符串） | UTF-8 字节序列 | Java 诞生时 Unicode 还是 16 位，选了 UTF-16；Go 的设计者也是 UTF-8 的设计者，`len` 就是字节数 |
| 子串 | 拷贝 | 共享原字符串内存 | Go 子串零成本，代价是小子串会让大字符串无法回收，需要 `strings.Clone` |
| 自定义哈希 | `hashCode/equals` 写在类型自身 | **1.27 新增** `maphash.Hasher[T]`：哈希策略与类型分离 | 把"怎么哈希"从类型里拿出来，同一个类型可以有多种策略，也不会被继承体系绑死 |

## 🔬 实验

### 实验 1：append 增长曲线（必做）
- **目的**：验证 `nextslicecap` 公式和 size class 取整。
- **方法**：`go/lab/grow`：对 `[]int64`、`[]byte`、`[]struct{a,b,c int64}` 逐个 append 到 100 万个元素，记录每次 cap 变化。Java 侧用 `--add-opens java.base/java.util=ALL-UNNAMED` 反射读 `elementData.length` 做同样记录。
- **工具**：`go run`；输出 CSV 后画成"容量-次数"曲线。
- **预期现象**：Go 在 256 前翻倍、之后增速递减，且不同元素大小的曲线不同（取整导致）；Java 稳定约 1.5 倍。
- **记录**：扩容总次数、总拷贝字节数、最终的空间浪费率。

### 实验 2：大 map 插入的尾延迟（必做，建议 Linux）
- **目的**：用数据验证"增量分裂 vs 一次性 resize"。
- **方法**：Go `map[int64]int64` 和 Java `HashMap<Long, Long>`（另加一组 `new HashMap<>(expected)` 预设容量），都不预分配，插入 1000 万个 key，记录每次插入的耗时，输出 p50 / p99 / p99.99 / max。
- **工具**：Go 用 `time.Now()` 差值或 `runtime/metrics`；Java 用 `System.nanoTime()`，预热后再测，并打开 `-Xlog:gc` 排除 GC 干扰。
- **预期现象**：Java 的 max 出现在 resize 那几次，量级随表大小增长；Go 的 max 明显更低且基本平稳。注意 GC 也会造成毛刺，报告中要区分清楚。
- **记录**：两边的延迟分位数，以及 max 发生在第几次插入。

### 实验 3：字符串转换与拼接的分配
- **目的**：确认编译器优化的边界。
- **方法**：benchmark 对比 `m[string(b)]` 与 `k := string(b); m[k]`；`+`、`fmt.Sprintf`、`strings.Builder`、预先 `Grow` 的 Builder 四种拼接；对 1MB 字符串反复截取 8 字节子串并持有 1 万个，看 HeapInuse，再改用 `strings.Clone` 对比。
- **工具**：`for b.Loop()`、`b.ReportAllocs()`、`testing.AllocsPerRun`、`benchstat`。
- **预期现象**：`m[string(b)]` 是 0 allocs/op；子串持有的场景下，堆占用约等于 1 万个大字符串的总大小，Clone 后降到 KB 级。

## 🧪 练习规划

**Go：`go/exercises`**（`go test -race ./ch03-slice-map-string/go/...`）
1. `func SplitBatches[T any](xs []T, n int) [][]T`：把一个切片切成每批 n 个元素的子切片（只用到最简单的类型参数，泛型本身在 [Ch06](../ch06-generics/) 讲）。测试会对 `batch[0]` 做 append，断言不会覆盖 `batch[1]`（必须用三下标切片）。
2. `func CompactAlive(ps []*Player) []*Player`：原地过滤掉 `Alive == false` 的元素，并把尾部清零（`clear`），让被移除的对象能被 GC 回收。测试会检查 `ps[len(result):cap]` 是否全为 nil。
3. `func TruncateRunes(s string, max int) string` 和 `func ParseWhisper(s string) (target, msg string, ok bool)`：按字符（rune）截断字符串（测试用中文、emoji 和非法 UTF-8 字节）；解析 `"/w alice hello"`。要求 `ParseWhisper` 零分配（AllocsPerRun == 0）。
4. `type Set[T any] struct{...}`，`func NewSet[T any](h maphash.Hasher[T]) *Set[T]`，提供 `Add`、`Has`、`Delete`、`Len`：用 **1.27 新增**的 `maphash.Hasher` 实现开放寻址哈希集合。测试会用 `ComparableHasher` 和一个故意制造大量碰撞的 Hasher 各跑一遍，并附 fuzz 测试和内建 map 做差分对比。
5. `func ParseKV(line string) (map[string]string, error)`：解析 `"k1=v1;k2=v2"`。练 `strings.SplitSeq` 和 `strings.Cut`；附 benchmark，对比预设 `make(map, n)` 容量前后的 allocs。

**Java：`java/drills/CollectionsDrill.java`**
- `int[] growthSequence(int n)`：复刻 ArrayList 的扩容序列，自校验前若干项。
- `String truncateCodePoints(String s, int max)`：按码点截断，测试中包含代理对 emoji，确保不会截出半个字符。
- `int bucketIndex(Object key, int capacity)`：复刻 HashMap 的扰动和下标计算，并在注释中对比 Go 的 h1/h2 划分。

## 🧭 设计取舍

- **slice 为什么是一个裸露的三元组。** `(ptr, len, cap)` 让切片表达式、传参、`append` 都几乎零成本，也让 `[]byte` 可以直接当 I/O 缓冲区。代价是别名：两个 slice 可能共享底层数组，`append` 可能写进对方能看到的位置。Java 的 `ArrayList` 用封装和 `modCount` 换来了安全，但每次访问多一层间接。
- **`append` 为什么返回新 slice，而不是原地修改。** slice header 是值，函数改不了调用方手里的 header，所以 `s = append(s, x)` 必须写回。这是值语义（[Ch02](../ch02-values-memory/)）的直接后果，而不是 API 设计的偶然。
- **map 为什么不加锁、遍历为什么随机。** 大多数 map 只被一个 goroutine 访问，内建加锁会让所有人付费；运行时检测到并发写就直接 fatal，宁可崩溃也不悄悄损坏数据。遍历随机化让代码无法依赖实现细节，Go 才能在不破坏兼容性的前提下把 map 换成 Swiss table。
- **为什么 Go 的 map 增量分裂而 Java 一次性 resize。** Swiss table 的目录 + 多表结构让每次分裂只涉及一张表（≤ 1024 槽），单次插入的最坏代价有上界。HashMap 是单表，翻倍时必须整体迁移，靠预设容量回避。ConcurrentHashMap 让多个线程协作迁移，是同一个问题在并发场景下的另一种答案。
- **UTF-8 vs UTF-16。** Java 在 Unicode 还是 16 位时定下 `char` = UTF-16 码元，后来不得不引入代理对，再用 JEP 254 紧凑字符串（Latin-1）把常见情况的内存省回来。Go 的 string 只是不可变字节序列，UTF-8 是约定而不是强制（可以存非法字节），`range` 才按 UTF-8 解码。两边"字符长度"的定义因此完全不同。

## ✅ 验收答辩题

- [ ] [L2] 给出 `s := make([]int, 3, 10); t := s[1:2]; t = append(t, 99)` 之后 `s` 的内容，并画出 header 与底层数组的关系。
- [ ] [L2] `nextslicecap` 的增长规则是什么？为什么 `append` 一个元素后 `cap` 可能不等于公式结果？
- [ ] [L2] 描述 Swiss table 一次查找的完整路径：哈希如何拆成 h1、h2，group 内怎样并行匹配，什么时候继续探测下一个 group。
- [ ] [L2] Go map 扩容和 Java HashMap 扩容的根本区别是什么？分别会对延迟造成什么影响？
- [ ] [L2] 为什么 Go map 的并发写是 fatal 而不是 panic？这对写服务端代码意味着什么？
- [ ] [L2] 列出编译器会省掉 `[]byte`→`string` 拷贝的三种场景，并解释为什么这些场景是安全的。
- [ ] [L3] 拿出实验 2 的数据：两边 p99.99 和 max 插入延迟分别是多少？如何排除 GC 的干扰？
- [ ] [L3] 一个程序反复从 1MB 的 `[]byte` 里截取几个小字段并长期持有，HeapInuse 持续很高。先从切片/子串的共享语义预测根因，再用实验 3 的方法（HeapInuse + `strings.Clone` / `bytes.Clone` 对照）证明。
- [ ] [L3] 用 AllocsPerRun 证明你的 `ParseWhisper` 是零分配，并解释它为什么不需要分配。
- [ ] [L4] Go 把 slice 做成裸露的 `(ptr, len, cap)`，Java 把动态数组做成封装对象 `ArrayList`。比较两种设计在性能、安全性（别名、并发修改检测）、与 I/O 和泛型交互上的得失。
- [ ] [L4] 1.27 的 `maphash.Hasher` 把哈希策略从类型中分离出来，Java 则把 `hashCode/equals` 绑定在类型上。这两种设计各自适合什么场景？Java 的 `Comparator` 和 Go 的 `Hasher` 是不是同一种思路？

## 🕳️ 常见坑

- **`append` 的结果不赋值回去**，或者对共享底层数组的两个切片分别 append，导致数据被互相覆盖。
- **把 `subList` 的直觉带过来**：以为子切片是"安全视图"，但 Go 不会抛 CME，只会静默地写坏数据。
- **从大 slice 里删除元素后不清零尾部**：被删除的指针仍被底层数组引用，GC 回收不了。
- **`len(s)` 当字符数**：中文、emoji 的昵称长度校验全错。
- **依赖 map 遍历顺序**，例如生成签名、做配置 diff、写测试断言。
- **多个 goroutine 读写同一个 map 而不加保护**：进程直接退出，`recover` 救不了。
- **以为 `delete` 会释放内存**：大 map 清空以后仍然占着原来的内存。
- **在 `for range` 里取元素地址**（`&v`）当成指向原元素的指针：拿到的是循环变量的地址，不是 slice 元素的地址。

## 📚 一手资料

- Go spec：[Slice types](https://go.dev/ref/spec#Slice_types)、[Slice expressions](https://go.dev/ref/spec#Slice_expressions)、[Appending and copying slices](https://go.dev/ref/spec#Appending_and_copying_slices)、[Map types](https://go.dev/ref/spec#Map_types)、[For statements with range clause](https://go.dev/ref/spec#For_range)
- 官方博客：[Go Slices: usage and internals](https://go.dev/blog/slices-intro)、[Strings, bytes, runes and characters in Go](https://go.dev/blog/strings)、Swiss table map 的实现介绍文章（go.dev/blog）
- 源码：`runtime/slice.go`、`runtime/string.go`、`internal/runtime/maps/`、`hash/maphash/hasher.go`
- Java：`java/util/ArrayList.java`、`HashMap.java`、`concurrent/ConcurrentHashMap.java`、`java/lang/String.java`（src.zip）、[JEP 431](https://openjdk.org/jeps/431)、[JEP 254 Compact Strings](https://openjdk.org/jeps/254)
