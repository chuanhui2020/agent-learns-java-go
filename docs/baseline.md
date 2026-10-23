# 特性基线：Go 1.27 × Java 25

> 只追踪这两个版本。下面是**本仓库要求掌握**的特性清单，以及它落在哪一章。
> 事实来源：go.dev/doc/go1.27、openjdk.org/projects/jdk/25，并已在 go1.27.3 / JDK 25 上实测关键 API。
> 本仓库只学语言与运行时（见 [methodology.md 第〇节](methodology.md#〇范围只学语言与运行时不学工程实践)），偏工程用法的新增特性列在每节末尾的「不展开」里，知道有这回事即可。

---

## 一、Go 1.27

Go 1.27 于 2026-08 发布，补丁版本按月发布（截至 2026-10 为 1.27.3）。下表分两部分：**1.27 新增**（必须专门讲）和 **1.27 现状**（之前版本引入、但属于当前 Go 的核心机制）。

### 1.1 Go 1.27 新增

| 类别 | 特性 | 章节 |
|---|---|---|
| 语言 | **泛型方法**：方法可以声明自己的类型参数（如 `(*rand.Rand).N[Int intType](Int) Int`）；接口方法不能有类型参数，泛型方法也不能实现接口方法 | [Ch06](../ch06-generics/) |
| 语言 | 结构体字面量的 key 可以是任意合法字段选择器（可直接写嵌入字段提升出来的字段） | [Ch05](../ch05-interfaces/) |
| 语言 | 函数类型推断推广到所有"把泛型函数赋值/转换为匹配函数类型"的场景 | [Ch06](../ch06-generics/) |
| 编译器 | 闭包命名简化且与内联无关，多个内联实例可共享代码 | [Ch04](../ch04-functions-closures/) · [Ch15](../ch15-alloc-escape-compiler/) |
| 运行时 | **`goroutineleak` profile 转正**（`runtime/pprof` + `/debug/pprof/goroutineleak`），基于 GC 可达性检测永久阻塞的 goroutine | [Ch14](../ch14-race-leak-detection/) |
| 运行时 | 编译器生成**按尺寸特化的分配函数**，<80B 小对象分配最多便宜 ~30%（`GOEXPERIMENT=nosizespecializedmalloc` 可关闭） | [Ch15](../ch15-alloc-escape-compiler/) |
| 运行时 | timer channel 恒为无缓冲（同步）语义，`asynctimerchan` GODEBUG 已删除 | [Ch11](../ch11-channels-select/) |
| 运行时 | `go 1.27` 模块的 traceback 头行带 pprof goroutine labels（`GODEBUG=tracebacklabels=0` 关闭） | [Ch08](../ch08-errors-panic/) |
| 实验 | `simd`（可移植、向量长度无关）+ `simd/archsimd`（新增 arm64 Neon / wasm），需 `GOEXPERIMENT=simd` | [Ch15](../ch15-alloc-escape-compiler/) |
| 标准库 | **`encoding/json/v2` + `encoding/json/jsontext` 转正**；`encoding/json` v1 改由 v2 实现（`GOEXPERIMENT=nojsonv2` 可回退）——只作为反射驱动编解码的案例 | [Ch09](../ch09-reflect-unsafe/) |
| 标准库 | `strings.CutLast` / `bytes.CutLast` | [Ch00](../ch00-setup/) · [Ch03](../ch03-slice-map-string/) |
| 标准库 | `hash/maphash.Hasher` 接口 + `ComparableHasher`（为自定义哈希表定契约） | [Ch03](../ch03-slice-map-string/) |
| 标准库 | `testing/synctest.Sleep` | [Ch14](../ch14-race-leak-detection/) |
| 标准库 | `net.UnixConn` 读方法直接返回 `io.EOF` | [Ch18](../ch18-netpoll-io/) |
| 工具 | `go test` 默认跑 `stdversion` vet 检查 | [Ch00](../ch00-setup/) |
| 工具 | `go doc pkg@version`、`go doc -ex`、按示例名打印示例源码 | [Ch00](../ch00-setup/) |
| 工具 | `go fix` 新增现代化器 `atomictypes`、`embedlit`、`slicesbackward`、`unsafefuncs`；`waitgroup` 改名 `waitgroupgo` | [Ch00](../ch00-setup/) |
| 工具 | vet `printf` 检查报告 `%w` 传入"指向本身实现了 error 的类型"的指针 | [Ch08](../ch08-errors-panic/) |
| 工具 | `go tool trace -http=:port` 默认只监听 localhost | [Ch17](../ch17-profiling-bench/) |

**不展开**（工程向）：`uuid` 包；`crypto/mldsa`（ML-DSA）与 TLS 1.3 的 ML-DSA 签名、`MLKEM1024`；`httptest.NewTestServer`；`net/http` 的 Body 自动排空、RFC 9218 优先级、`Server.MaxHeaderValueCount`、自带 `net.Conn` 的 ALPN；`database/sql.ConvertAssign`、`driver.RowsColumnScanner`；`compress/flate` 提速；`go test -json` 的 `OutputType`；`go mod tidy` 合并 require 块。

### 1.2 Go 1.27 现状（核心机制，非 1.27 新增）

| 机制 | 要点 | 章节 |
|---|---|---|
| 类型别名（含泛型别名） | `type A = B`、`type Set[T comparable] = map[T]struct{}` | [Ch01](../ch01-types-constants/) · [Ch06](../ch06-generics/) |
| 每次迭代新变量 | `for` 循环变量按迭代作用域（读老代码时仍要识别旧写法） | [Ch04](../ch04-functions-closures/) · [Ch07](../ch07-iterators/) |
| `new(expr)` | `new(42)` 得到指向 42 的 `*int` | [Ch02](../ch02-values-memory/) |
| Swiss table map | `internal/runtime/maps`：group + 控制字、SIMD 探测、增量扩容 | [Ch03](../ch03-slice-map-string/) |
| 泛型 | 类型集、自引用泛型类型、核心类型 | [Ch06](../ch06-generics/) |
| 迭代器 | range-over-func、`iter.Seq/Seq2`、`iter.Pull`（运行时协程）、slices/maps/strings 迭代器函数 | [Ch07](../ch07-iterators/) |
| `errors.AsType` | 泛型版 `errors.As` | [Ch08](../ch08-errors-panic/) |
| `reflect.TypeFor`、`unsafe.String/Slice/StringData/SliceData` | 反射与 unsafe 的现代 API | [Ch09](../ch09-reflect-unsafe/) |
| 容器感知 GOMAXPROCS | Linux 下尊重 cgroup CPU 限额，运行中周期性更新（GODEBUG `updatemaxprocs`） | [Ch10](../ch10-scheduler/) |
| `sync.WaitGroup.Go`、`sync.OnceValue(s)` | 结构化地起 goroutine / 惰性初始化 | [Ch12](../ch12-sync-memory-model/) · [Ch13](../ch13-context-structured/) |
| `testing/synctest` | 气泡内假时钟，确定性地运行并发代码 | [Ch14](../ch14-race-leak-detection/) |
| PGO | `default.pgo` 驱动内联与去虚拟化 | [Ch15](../ch15-alloc-escape-compiler/) |
| Green Tea GC（默认） | 以 span 为单位批量标记/扫描小对象，提升局部性与多核扩展性；新 amd64 CPU 上用向量指令 | [Ch16](../ch16-gc/) |
| `weak`、`unique`、`runtime.AddCleanup` | 弱引用、值驻留、替代 finalizer 的清理函数 | [Ch16](../ch16-gc/) |
| `testing.B.Loop` | 标准 benchmark 写法，防止被测代码被优化掉 | [Ch17](../ch17-profiling-bench/) |
| `runtime/trace.FlightRecorder` | 内存环形缓冲持续记录 trace，事后快照 | [Ch17](../ch17-profiling-bench/) |
| `runtime.Pinner` | 固定 Go 对象，使其地址可以安全地交给 C | [Ch19](../ch19-syscall-cgo-ffi/) |

**不展开**（工程向）：`log/slog`；`go.mod` 的 `tool` / `ignore` 指令；`//go:fix inline`；`os.Root`。

---

## 二、Java 25（LTS）

### 2.1 JDK 25 的 18 个 JEP

| JEP | 标题 | 状态 | 章节 |
|---|---|---|---|
| 470 | PEM Encodings of Cryptographic Objects | Preview | 不展开（安全 API） |
| 502 | Stable Values | Preview | [Ch12](../ch12-sync-memory-model/) · [Ch15](../ch15-alloc-escape-compiler/) |
| 503 | Remove the 32-bit x86 Port | 正式 | [Ch00](../ch00-setup/)（了解） |
| 505 | Structured Concurrency（第五次预览） | Preview | [Ch13](../ch13-context-structured/) |
| 506 | Scoped Values | 正式 | [Ch13](../ch13-context-structured/) |
| 507 | Primitive Types in Patterns, instanceof, and switch（第三次预览） | Preview | [Ch01](../ch01-types-constants/) · [Ch05](../ch05-interfaces/) |
| 508 | Vector API（第十次孵化） | Incubator | [Ch15](../ch15-alloc-escape-compiler/) |
| 509 | JFR CPU-Time Profiling | Experimental（仅 Linux） | [Ch17](../ch17-profiling-bench/) |
| 510 | Key Derivation Function API | 正式 | 不展开（安全 API） |
| 511 | Module Import Declarations | 正式 | [Ch00](../ch00-setup/) |
| 512 | Compact Source Files and Instance Main Methods | 正式 | [Ch00](../ch00-setup/) |
| 513 | Flexible Constructor Bodies | 正式 | [Ch04](../ch04-functions-closures/) |
| 514 | Ahead-of-Time Command-Line Ergonomics | 正式 | [Ch15](../ch15-alloc-escape-compiler/) |
| 515 | Ahead-of-Time Method Profiling | 正式 | [Ch15](../ch15-alloc-escape-compiler/) |
| 518 | JFR Cooperative Sampling | 正式 | [Ch17](../ch17-profiling-bench/) |
| 519 | Compact Object Headers | 正式（需显式开启） | [Ch02](../ch02-values-memory/) · [Ch16](../ch16-gc/) |
| 520 | JFR Method Timing & Tracing | 正式 | [Ch17](../ch17-profiling-bench/) |
| 521 | Generational Shenandoah | 正式 | [Ch16](../ch16-gc/) |

### 2.2 Java 25 现状（此前版本定稿、属于当前 Java 的核心能力）

| 能力 | 要点 | 章节 |
|---|---|---|
| 未命名变量与模式 `_`（JEP 456） | 与 Go 的空白标识符对照 | [Ch00](../ch00-setup/) |
| 多文件源码启动（JEP 458） | `bin/jrun` 的基础 | [Ch00](../ch00-setup/) |
| 原始类型、装箱与数值转换（JLS 第 4、5 章） | 拓宽/窄化、数值提升、编译期常量内联 | [Ch01](../ch01-types-constants/) |
| Sequenced Collections（JEP 431） | 集合接口体系 | [Ch03](../ch03-slice-map-string/) |
| lambda 与 invokedynamic | `LambdaMetafactory`、隐藏类、捕获 vs 非捕获 | [Ch04](../ch04-functions-closures/) |
| record / sealed / 模式匹配 switch / record 模式（JEP 395/409/440/441） | 代数数据类型 + 穷尽性检查 | [Ch05](../ch05-interfaces/) |
| Stream Gatherers（JEP 485） | 自定义中间操作 | [Ch07](../ch07-iterators/) |
| 核心反射基于方法句柄（JEP 416）、强封装（JEP 403） | 反射的实现与访问控制 | [Ch09](../ch09-reflect-unsafe/) |
| Class-File API（JEP 484） | 运行时生成 / 改写字节码 | [Ch09](../ch09-reflect-unsafe/) |
| Unsafe 内存访问方法弃用与告警（JEP 471/498） | 由 VarHandle / FFM 取代 | [Ch09](../ch09-reflect-unsafe/) |
| 虚拟线程 + 同步不再 pin 载体（JEP 444 / 491） | Continuation、FJP 调度器、剩余 pinning 场景 | [Ch10](../ch10-scheduler/) |
| AOT 类加载与链接（JEP 483） | Leyden 第一步，配合 514/515 | [Ch15](../ch15-alloc-escape-compiler/) |
| 分代 ZGC（唯一模式）、G1 | 低延迟 GC 的两种设计 | [Ch16](../ch16-gc/) |
| FFM API（JEP 454） | 堆外内存、原生调用（downcall/upcall） | [Ch19](../ch19-syscall-cgo-ffi/) |

**不展开**（工程向）：Markdown 文档注释（JEP 467）；JPMS 模块化工程实践；jlink / jpackage。

> Preview / Incubator 特性运行需要 `--enable-preview`（`bin/jrun` 已默认带上），孵化模块还需 `--add-modules jdk.incubator.vector`。
