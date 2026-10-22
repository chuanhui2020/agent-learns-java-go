# Ch19 · 序列化与协议设计

> Part 4 · 估时 12h · 状态：⚪ 未开始 · 按"消息热度"选格式：战斗帧手写二进制、业务 RPC 用 protobuf、配置和运营接口用 JSON v2，兼容性靠纪律而不是格式

## 🎯 目标（资深标准）

- **L1**：用 protobuf-go 定义、生成、编解码游戏协议；用 `encoding/json/v2` 严格解析配置与运营接口；为热消息手写零分配的二进制编解码。
- **L2**：讲清 protobuf 线格式（tag = 字段号<<3 | wire type、varint、zigzag、length-delimited、未知字段保留）以及 protobuf-go 运行时的通用实现成本；讲清 json/v2 的分层架构（`jsontext` 语法层 + `v2` 语义层 + `Options`）、严格默认值，以及 v1 如何改由 v2 实现；对照 protobuf-java 和 Jackson 的实现差异。
- **L3**：拿出同一消息在 protobuf / json v2 / 手写二进制下的 ns/op、B/op、allocs/op、编码字节数；做 v1 与 v2 的解码对比；用 fuzz 找出自定义解码器的 bug。
- **L4**：制定协议 schema 的演进规范（字段号、保留字、兼容测试、灰度）与全局 ID 方案（UUID v7 vs 雪花），判断什么时候值得手写编解码。

## 🔗 前置

[Ch18](../ch18-tcp-gateway/)（分帧之上才是消息体）· [Ch02](../ch02-slice-map-string/)（`[]byte`/string 转换）· [Ch04](../ch04-generics/)（`MarshalFunc[T]` 等泛型 API）· [Ch16](../ch16-zero-alloc-dod/)（零分配与 `unsafe` 边界）

## 💡 关键洞察

1. **序列化是游戏服 CPU 的大户之一，一套格式包打天下是错的。** 高频的战斗帧、状态同步走手写或专用生成的二进制；业务 RPC 走 protobuf；配置、运营后台、对外接口走 JSON。先按"每秒多少条 × 每条多大"排出热度，再选格式。
2. **json/v2 在 Go 1.27 转正，`encoding/json`（v1）也改由 v2 实现。** 老代码不改一行就能拿到更快的解码，但**错误信息文本可能变了**，所有对错误字符串的断言都会失效。v2 的默认值更严格（拒绝非法 UTF-8 和重复键），处理外部输入更安全。
3. **"可演进"是纪律，不是格式自带的。** protobuf 的前后兼容只在字段号永不复用、类型不乱改、未知字段被保留的前提下成立。要靠 `reserved`、代码评审和兼容性测试来守住。

## ☕ Java 25 深度复习

- **protobuf-java**：
  - 生成代码默认是专用的序列化代码，底层是 `CodedOutputStream` / `CodedInputStream`；
  - 消息不可变，修改要走 `Builder` → `build()`，高频场景的分配压力明显；
  - 对照：protobuf-go 的消息是可变结构体，可以 `Reset` 后原地复用。
- **Jackson**（第三方）：
  - 两层架构：streaming（`JsonParser` / `JsonGenerator`）+ databind（`ObjectMapper`，反射，或用 Blackbird 模块借 `LambdaMetafactory` 生成访问器）；
  - `ObjectMapper` 线程安全，应全局复用；
  - 重复键默认不报错（取后一个值），需开启 `JsonParser.Feature.STRICT_DUPLICATE_DETECTION`；
  - 支持 record（按规范构造器反序列化）。
- **Java 原生序列化**：`java.io.Serializable` 是历史上反序列化漏洞的重灾区。`ObjectInputFilter`（JEP 290 引入、JEP 415 增加上下文过滤器）只能缓解。record 的反序列化必然经过规范构造器（JEP 395），校验逻辑不会被绕过。结论：**不要用于网络协议**。对照 Go 的 `encoding/gob`：只适合 Go 进程之间。
- **UUID**：`java.util.UUID` 只内置 `randomUUID()`（v4）和 `nameUUIDFromBytes()`（v3）两种生成方式，有序的 v7 需要自写或用第三方库。UUID 是对象，作为 map key 有对象头和指针开销。
- **压缩**：`java.util.zip.Deflater` / `Inflater` 包装原生 zlib。

**源码**（`src.zip`，`java.base/` 下）：`java/util/UUID.java`、`java/util/zip/Deflater.java`、`java/io/ObjectInputFilter.java`、`java/lang/Record.java`、`java/nio/ByteBuffer.java`。

## 🐹 Go 1.27 深入

### 规范层（库契约）
- **`encoding/json/v2`（1.27 新增）**：
  - 入口：`Marshal`、`MarshalWrite`、`MarshalEncode`、`Unmarshal`、`UnmarshalRead`、`UnmarshalDecode`，都接受可变参数 `Options`；
  - 选项组合：`JoinOptions`、`DefaultOptionsV2`；
  - 语义选项：`StringifyNumbers`、`Deterministic`、`FormatNilSliceAsNull`、`FormatNilMapAsNull`、`OmitZeroStructFields`、`MatchCaseInsensitiveNames`、`RejectUnknownMembers`；
  - 自定义编解码：`WithMarshalers(MarshalFunc[T] / MarshalToFunc[T])`、`WithUnmarshalers(UnmarshalFunc[T] / UnmarshalFromFunc[T])`，**不改类型定义**就能替换编码方式；
  - 方法接口：除了 `MarshalJSON` / `UnmarshalJSON`，新增流式的 `MarshalerTo`（`MarshalJSONTo(*jsontext.Encoder) error`）和 `UnmarshalerFrom`（`UnmarshalJSONFrom(*jsontext.Decoder) error`），省掉中间的 `[]byte`；
  - 结构体标签：`omitzero`、`omitempty`（v2 语义：省略编码结果为空 JSON 值的字段）、`string`、`case:ignore` / `case:strict`、`embed`（实验期叫 `inline`）；
  - 默认行为：大小写敏感匹配；nil slice / nil map 编码为 `[]` / `{}`；
  - 错误类型：`*json.SemanticError`、`*jsontext.SyntacticError`，哨兵错误 `json.ErrUnknownName`、`jsontext.ErrDuplicateName`。
- **`encoding/json/jsontext`（1.27 新增）**：
  - `Encoder` / `Decoder` 以 `Token` / `Value` 为单位读写，内部状态机保证输出永远是合法 JSON；
  - 常用方法：`PeekKind`、`ReadToken`、`ReadValue`、`WriteToken`、`WriteValue`；错误定位用 `InputOffset`、`StackDepth`；
  - `Value` 的 `IsValid` / `Compact` / `Canonicalize`；数值 Token 的访问器 `Int()` / `Float()` 返回 `(值, error)`；
  - 选项：`AllowDuplicateNames`、`AllowInvalidUTF8`、`EscapeForHTML`、`Multiline`、`WithIndent`，以及**防御恶意输入**用的 `WithByteLimit`、`WithDepthLimit`。
- **`encoding/json`（v1）现由 v2 实现**：`v2_decode.go` 等文件带 `goexperiment.jsonv2` 构建标签，1.27 默认开启（`GOEXPERIMENT=nojsonv2` 可回退到原实现，这个开关将来会删除）。`DefaultOptionsV1` 加一组 `...WithLegacySemantics` 选项，让 v2 以 v1 语义运行，便于逐个调用点迁移。
- **`encoding/binary`**：`BigEndian` / `LittleEndian` 的 `AppendUint16/32/64`，`AppendUvarint` / `AppendVarint`（zigzag）是零分配的 append 风格；`binary.Append` / `Encode` / `Decode` / `Size` 基于反射，热路径慎用。
- **`uuid`（1.27 新增）**：
  - `UUID` 是 `[16]byte` 值类型：可直接当 map key，比较不分配；
  - `New()` 目前等价于 `NewV4()`；`NewV7()` 按 RFC 9562 生成，高 48 位是毫秒时间戳，至少 62 位随机，除时钟回拨外保证递增；
  - 其他：`Parse`、`MustParse`、`Nil`、`Max`、`String`、`MarshalText`。
- **`compress/flate`（1.27 更快）**：压缩输出字节可能和 1.26 不同，影响 gzip/zlib/zip/png。任何"对压缩结果做签名或当缓存键"的逻辑都要改。`Writer.Reset` 复用压缩器能省大量分配。
- **protobuf-go（第三方，`google.golang.org/protobuf`）**：
  - `proto.Marshal` / `Unmarshal` / `Size` / `Equal`；`proto.MarshalOptions{Deterministic: true}` 让 map 字段输出稳定；
  - 运行时基于 `protoreflect` 的通用实现；未知字段默认保留；
  - Opaque API（go.dev 博客 2024 年发布）：生成代码隐藏字段、只暴露访问器，为惰性解码和内存布局优化留出空间。
- **vtprotobuf（第三方，`github.com/planetscale/vtprotobuf`）**：为消息生成专用的 `MarshalVT` / `UnmarshalVT` / `SizeVT` 等方法，可选对象池，用代码体积换掉反射成本。

### 运行时层：分配和 CPU 花在哪里
- **反射与类型缓存**：json/v2 和 protobuf-go 都会按类型缓存编解码计划（字段表、函数指针），首次使用某类型有一次性成本，之后主要是逐字段的间接调用。生成代码（vtprotobuf、手写）的优势就在于消除这层间接调用并能被内联。
- **分配来源**：输出缓冲（改用 `MarshalWrite` / `MarshalEncode` 写进复用的缓冲）、string 字段从 `[]byte` 拷贝、map、指针字段、`any` 字段装箱。
- **零拷贝解码的代价**：解码时让字段直接引用输入 `[]byte` 的子切片可以避免拷贝，但输入缓冲（如 `bufio.Peek` 的结果）一旦被复用，字段就变成脏数据。这与 [Ch18](../ch18-tcp-gateway/) 的生命周期问题同源；`unsafe.String` 零拷贝转换的合法边界见 [Ch16](../ch16-zero-alloc-dod/)。

### 源码阅读清单（`$GOROOT/src`）
| 文件 | 函数/类型 | 带着什么问题读 |
|---|---|---|
| `encoding/json/v2/arshal.go` | `Marshal`、`MarshalEncode`、`Unmarshal`、`UnmarshalDecode` | 顶层 API 怎样共用同一套 Encoder/Decoder 状态？`Options` 是怎么合并、怎么被查询的？ |
| `encoding/json/v2/fields.go` | 标签解析（`embed`、`omitzero`、`case`） | 字段表什么时候构建、怎样缓存？`omitEmpty` 这种拼写错误为什么会直接报错？ |
| `encoding/json/jsontext/decode.go` · `state.go` | `Decoder.ReadToken`、`ReadValue`、`PeekKind`、`ErrDuplicateName` | 状态机怎样保证合法性？重复键检测的时间和内存代价是多少？ |
| `encoding/json/v2_decode.go` · `v2_options.go` | `DefaultOptionsV1`、`...WithLegacySemantics` | v1 是怎样"翻译"成 v2 调用的？哪些 v1 怪癖被保留成了选项？ |
| `encoding/binary/varint.go` | `AppendUvarint`、`Uvarint` | varint 最多几个字节？溢出和截断分别怎样报告？ |
| `uuid/uuid.go` | `NewV7` | 同一毫秒内怎样保证单调？时钟回拨时会怎样？那把全局锁在高并发下的成本有多大？ |
| `compress/flate/deflate.go` | `NewWriter`、`Writer.Reset` | 复用 Writer 能省掉多少分配？压缩级别对 CPU 和压缩率的影响？ |

## 📊 Java 25 ↔ Go 1.27 机制级对照表

| 维度 | Java 25 | Go 1.27 | 取舍 / 游戏服影响 |
|---|---|---|---|
| 业务协议主力 | protobuf-java：不可变消息 + Builder | protobuf-go：可变结构体 + `protoreflect` 通用运行时 | Go 消息可以 `Reset` 复用；Java Builder 模式分配更多 |
| 生成代码风格 | 默认生成专用序列化代码 | 标准 protobuf-go 走通用运行时；vtprotobuf 生成专用方法 | 热消息在 Go 侧用 vtprotobuf 可以接近手写性能 |
| JSON 架构 | Jackson：streaming + databind 两层 | `jsontext`（语法）+ `json/v2`（语义）两层 | 架构同构，Go 的严格性默认更高 |
| 外部输入防御 | 重复键默认放过；深度/大小限制要单独配置 | 默认拒绝重复键和非法 UTF-8；`WithByteLimit` / `WithDepthLimit` | 运营接口、客户端上报的 JSON 在 Go v2 下更安全 |
| 二进制工具 | `ByteBuffer`（可设字节序）、`DataOutputStream` | `encoding/binary` 的 Append 系列 | Go 的 append 风格天然支持缓冲复用、零分配 |
| UUID | `java.util.UUID` 对象；只有 v4、v3 | `uuid.UUID` 是 `[16]byte` 值类型；v4、v7 | Go 1.27 的 v7 可直接用作有序主键；Java 需要自写 |
| 原生序列化 | `Serializable`（安全风险，`ObjectInputFilter`） | `encoding/gob`（只限 Go 之间） | 两者都不该出现在跨语言网络协议里 |
| 压缩 | `Deflater` 包装原生 zlib | 纯 Go `compress/flate`（1.27 更快） | Go 的压缩输出会随版本变化，不要拿它做签名或缓存键 |

## 🔬 实验

### 实验 1：同一消息的三种编码
- **目的**：给"按热度选格式"拿出数据。
- **方法**：`go/lab/codec/` 定义一个"战斗帧状态"消息（20 个实体 × 位置 / 血量 / 状态位），分别用 protobuf-go（可选再加 vtprotobuf）、`encoding/json/v2`、手写二进制（`encoding/binary` 的 Append 系列）实现编解码。Java 侧用 protobuf-java 与 Jackson 实现同一消息（严谨数据用 [Ch15](../ch15-profiling-bench/) 的 JMH 工程；没有 JMH 时只做粗测，并在报告里注明）。
- **工具**：`go test -bench=. -benchmem -count=10` + `benchstat`。
- **记录**：ns/op、B/op、allocs/op、编码后字节数，以及每秒 20 帧 × 1 万连接下折算的 CPU 占比。

### 实验 2：json v1 vs v2 解码
- **方法**：用一份约 5MB 的道具配置表，比较三种 `Unmarshal`：默认的 `encoding/json`（1.27 由 v2 实现）、`GOEXPERIMENT=nojsonv2` 构建的原 v1 实现、直接调用 `encoding/json/v2`。同时对一批非法输入记录三者的错误信息文本。
- **预期**：解码明显变快；错误文本存在差异。
- **记录**：ns/op、allocs/op，以及错误文本差异清单（迁移时要排查的断言）。

### 实验 3：schema 演进的兼容矩阵
- **方法**：准备 v1 / v2 两版 `.proto`（新增字段、删除字段并 `reserved`、重命名字段）。验证四种组合：老客户端 ↔ 新服务器、新客户端 ↔ 老服务器；验证 Go 中转"解码 → 修改 → 重新编码"后未知字段不丢；再做 Go 编码 → Java 解码的互通测试。
- **记录**：兼容矩阵表；哪些改动是安全的，哪些会破坏兼容。

### 实验 4：fuzz 手写解码器
- **方法**：对练习 1 的解码器 `go test -fuzz=FuzzDecodeEntityState -fuzztime=60s`，记录发现的 panic / 越界 / 无限循环，修复后把崩溃样例加入 `testdata/fuzz` 语料。

## 🧪 练习规划

**Go：`go/exercises`**
1. `type EntityState struct { ID uint32; X, Y float32; HP int32; Flags uint16 }`：实现 `func AppendEntityState(dst []byte, s *EntityState) []byte` 与 `func DecodeEntityState(b []byte) (s EntityState, n int, err error)`（ID 用 varint，其余定长）。测试：编码-解码往返、截断返回 `ErrShortBuffer`、`testing.AllocsPerRun` 断言预分配 `dst` 时编码 0 分配；附 fuzz 测试。
2. `func DecodeItemTable(r io.Reader) (*ItemTable, error)`：用 json/v2 + `RejectUnknownMembers(true)` + `jsontext.WithByteLimit`。测试：未知字段、重复键、非法 UTF-8、超限输入分别报错，并用 `errors.Is` / `errors.As` 断言到 `json.ErrUnknownName`、`jsontext.ErrDuplicateName`、`*jsontext.SyntacticError`。
3. `type Gold int64`（单位：分）实现 `MarshalJSONTo` / `UnmarshalJSONFrom`，以字符串 `"12.34"` 形式编解码。测试：往返、非法格式报错、`AllocsPerRun` 断言流式实现的分配上限。
4. `func NewOrderID() uuid.UUID` 与 `func OrderTime(id uuid.UUID) time.Time`：用 `uuid.NewV7` 生成订单号，并从高 48 位取回毫秒时间戳。测试：批量生成后已排序、时间误差在 1ms 内、`Version` 位正确。
5. （可选，需要 protoc）给定 `.proto`，测试 `MarshalOptions{Deterministic: true}` 对含 map 消息的输出稳定性，以及未知字段的往返保留。

**Java：`java/drills`**
- `BinaryCodecDrill.java`：用 `ByteBuffer` 实现与练习 1 相同的 `EntityState` 编解码（含 varint），自校验往返与截断异常；和 Go 版对比"返回错误 vs 抛 `BufferUnderflowException`"。
- `UuidV7Drill.java`：按 RFC 9562 的位布局手写 UUID v7 生成器（48 位毫秒 + 版本 7 + 变体位），自校验 `version() == 7`、`variant() == 2` 和单调性，对照 Go 的 `uuid.NewV7`。

## 🎮 游戏服落地

- **战斗帧 / 状态同步（20–60Hz）**：手写二进制或 vtprotobuf，编码进复用缓冲，广播共享同一个 `[]byte`（[Ch18](../ch18-tcp-gateway/)）。Java 侧通常用 protobuf-java + 池化 `ByteBuf`，Builder 带来的分配要在 GC 日志里重点盯。
- **业务 RPC（背包、邮件、商城）**：protobuf，按协议号路由。字段演进遵守"只增不改、删除即 `reserved`"，CI 里跑兼容性测试（如对比 `.proto` 的破坏性变更检查）。
- **配置表与运营后台**：JSON v2 严格模式，`RejectUnknownMembers` 让策划配错字段名时直接报错，而不是静默忽略；配置热加载见 [Ch24](../ch24-runtime-boundaries/)。
- **全局 ID**：订单、邮件、日志追踪用 `uuid.NewV7()`（时间有序，数据库索引友好）；玩家 ID 等需要更短、更紧凑的场景仍可用雪花算法。代价对比：v7 不依赖机器号分配，但占 16 字节。

## ✅ 验收答辩题

- [ ] [L2] 手工解码一段 protobuf 字节（给出十六进制），说明每个字节的 tag、wire type 和值。
- [ ] [L2] protobuf-go 的消息为什么能 `Reset` 复用，而 protobuf-java 的消息不行？两种设计对 GC 的影响分别是什么？
- [ ] [L2] json/v2 相对 v1 有哪些默认行为变化？列出至少 4 条，并说明各自的安全或性能意义。
- [ ] [L2] Go 1.27 里 `encoding/json` v1 是怎样由 v2 实现的？升级后哪些东西可能"看起来坏了"？
- [ ] [L2] `MarshalerTo` / `UnmarshalerFrom` 相比 `MarshalJSON` / `UnmarshalJSON` 省掉了什么？什么时候值得实现？
- [ ] [L3] 用实验 1 的数据说明：在 1 万连接 × 20Hz 的广播场景下，三种格式各自要占多少 CPU 核？你会怎样选？
- [ ] [L3] 零拷贝解码让字段引用输入缓冲，线上出现"偶发数据错乱"，你怎样定位和修复？
- [ ] [L3] fuzz 找到的一个 bug：写出最小复现输入、根因和修复方式。
- [ ] [L4] 为一个 200 条协议、5 个客户端版本并存的项目制定 schema 演进规范与兼容性测试方案。
- [ ] [L4] UUID v7、雪花算法、数据库自增三种 ID 方案，在分库分表、索引局部性、可读性、时钟回拨风险上的取舍。

## 🕳️ 常见坑

- **复用字段号或修改字段类型**：老客户端静默读出错误的数据，而且不会报错。
- **把压缩结果或 JSON 文本当签名 / 缓存键**：`compress/flate` 1.27 的输出字节已经变了；JSON 字段顺序在没有 `Deterministic` 时也不保证稳定。
- **测试里断言错误字符串**：v1 改由 v2 实现后错误文本变化，测试全部失败。用 `errors.Is` / `errors.As`。
- **以为 json/v2 和 v1 一样大小写不敏感**：v2 默认大小写敏感，`"hp"` 不再匹配字段 `HP`，需要 `case:ignore` 或 `MatchCaseInsensitiveNames`。
- **热路径用 `binary.Write` / `binary.Read`**：它们基于反射，比 Append 系列慢一个数量级还会分配。
- **零拷贝解码后缓冲被复用**，字段读到脏数据。
- **用 `encoding/gob` 或 Java `Serializable` 做跨服协议**：语言绑定，而且有安全风险。

## 📚 一手资料

- [Protocol Buffers Encoding](https://protobuf.dev/programming-guides/encoding/)、[Proto 语言指南（字段号与 reserved）](https://protobuf.dev/programming-guides/proto3/)、RFC 9562（UUID）、RFC 8259（JSON）
- Go：[Go 1.27 Release Notes](https://go.dev/doc/go1.27)、`go doc encoding/json/v2`、`go doc encoding/json/jsontext`、`go doc uuid`；proposal #71497（json/v2）；go.dev 博客关于 protobuf Opaque API 的文章
- Go 源码：`encoding/json/v2/`、`encoding/json/jsontext/`、`encoding/json/v2_decode.go`、`encoding/binary/`、`uuid/uuid.go`、`compress/flate/`
- JDK：`src.zip` 中的 `java/util/UUID.java`、`java/io/ObjectInputFilter.java`；[JEP 290](https://openjdk.org/jeps/290)、[JEP 415](https://openjdk.org/jeps/415)、[JEP 395](https://openjdk.org/jeps/395)
- 第三方：`google.golang.org/protobuf`、`github.com/planetscale/vtprotobuf`、protobuf-java、Jackson 官方文档
