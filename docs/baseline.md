# 特性基线：Go 1.27 × Java 25

> 只追踪这两个版本。下面是**本仓库要求掌握**的特性清单，以及它落在哪一章。
> 事实来源：go.dev/doc/go1.27、openjdk.org/projects/jdk/25，并已在 go1.27.3 / JDK 25.0.2 上实测关键 API。

---

## 一、Go 1.27

Go 1.27 于 2026-08 发布，补丁版本按月发布（截至 2026-10 为 1.27.3）。下表分两部分：**1.27 新增**（必须专门讲）和 **1.27 现状**（之前版本引入、但属于当前 Go 的核心机制）。

### 1.1 Go 1.27 新增

| 类别 | 特性 | 章节 |
|---|---|---|
| 语言 | **泛型方法**：方法可以声明自己的类型参数（如 `(*rand.Rand).N[Int intType](Int) Int`）；接口方法不能有类型参数，泛型方法也不能实现接口方法 | [Ch04](../ch04-generics/) |
| 语言 | 结构体字面量的 key 可以是任意合法字段选择器（可直接写嵌入字段提升出来的字段） | [Ch03](../ch03-interfaces/) |
| 语言 | 函数类型推断推广到所有"把泛型函数赋值/转换为匹配函数类型"的场景 | [Ch04](../ch04-generics/) |
| 运行时 | **`goroutineleak` profile 转正**（`runtime/pprof` + `/debug/pprof/goroutineleak`），基于 GC 可达性检测永久阻塞的 goroutine | [Ch12](../ch12-concurrency-testing/) |
| 运行时 | 编译器生成**按尺寸特化的分配函数**，<80B 小对象分配最多便宜 ~30%（`GOEXPERIMENT=nosizespecializedmalloc` 可关闭） | [Ch13](../ch13-alloc-escape-compiler/) |
| 运行时 | timer channel 恒为无缓冲（同步）语义，`asynctimerchan` GODEBUG 已删除 | [Ch08](../ch08-channels-select/) |
| 运行时 | `go 1.27` 模块的 traceback 头行带 pprof goroutine labels（`GODEBUG=tracebacklabels=0` 关闭） | [Ch23](../ch23-build-observability/) |
| 编译器 | 闭包命名简化且与内联无关，多个内联实例可共享代码 | [Ch13](../ch13-alloc-escape-compiler/) |
| 新包 | **`encoding/json/v2` + `encoding/json/jsontext` 转正**；`encoding/json` v1 改由 v2 实现（`GOEXPERIMENT=nojsonv2` 可回退） | [Ch19](../ch19-serialization/) |
| 新包 | `uuid`（`New`=`NewV4`、`NewV7`、`Parse`） | [Ch19](../ch19-serialization/) |
| 新包 | `crypto/mldsa`（ML-DSA，FIPS 204），TLS 1.3 支持 ML-DSA 签名、`MLKEM1024` 密钥交换 | [Ch20](../ch20-http-rpc/) |
| 实验 | `simd`（可移植、向量长度无关）+ `simd/archsimd`（新增 arm64 Neon / wasm），需 `GOEXPERIMENT=simd` | [Ch16](../ch16-zero-alloc-dod/) |
| 标准库 | `strings.CutLast` / `bytes.CutLast` | [Ch00](../ch00-setup/) · [Ch02](../ch02-slice-map-string/) |
| 标准库 | `hash/maphash.Hasher` 接口 + `ComparableHasher`（为自定义哈希表/布隆过滤器定契约） | [Ch02](../ch02-slice-map-string/) |
| 标准库 | `testing/synctest.Sleep`；`httptest.NewTestServer`（内存假网络，配合 synctest） | [Ch12](../ch12-concurrency-testing/) · [Ch20](../ch20-http-rpc/) |
| 标准库 | `net/http`：HTTP/1 `Response.Body` 关闭时自动排空、HTTP/2 服务端遵循 RFC 9218 优先级、`Server.MaxHeaderValueCount`、用户自带 `net.Conn` 的 ALPN | [Ch20](../ch20-http-rpc/) |
| 标准库 | `database/sql.ConvertAssign`、`driver.RowsColumnScanner` | [Ch21](../ch21-storage-cache/) |
| 标准库 | `compress/flate` 压缩更快（输出字节可能与旧版不同） | [Ch19](../ch19-serialization/) |
| 标准库 | `net.UnixConn` 读方法直接返回 `io.EOF` | [Ch17](../ch17-netpoll-io/) |
| 工具 | `go test` 默认跑 `stdversion` vet 检查；`go test -json` 新增 `OutputType` | [Ch22](../ch22-modules-testing/) |
| 工具 | `go doc pkg@version`、`go doc -ex`、按示例名打印示例源码 | [Ch00](../ch00-setup/) · [Ch22](../ch22-modules-testing/) |
| 工具 | `go fix` 新增现代化器 `atomictypes`、`embedlit`、`slicesbackward`、`unsafefuncs`；`waitgroup` 改名 `waitgroupgo` | [Ch22](../ch22-modules-testing/) |
| 工具 | vet `printf` 检查报告 `%w` 传入"指向本身实现了 error 的类型"的指针 | [Ch06](../ch06-errors-panic/) |
| 工具 | `go mod tidy` 合并重复 require 块（`go 1.27` 模块） | [Ch22](../ch22-modules-testing/) |
| 工具 | `go tool trace -http=:port` 默认只监听 localhost | [Ch15](../ch15-profiling-bench/) |

### 1.2 Go 1.27 现状（核心机制，非 1.27 新增）

| 机制 | 要点 | 章节 |
|---|---|---|
| Green Tea GC（默认） | 以 span 为单位批量标记/扫描小对象，提升局部性与多核扩展性；新 amd64 CPU 上用向量指令 | [Ch14](../ch14-gc/) |
| 容器感知 GOMAXPROCS | Linux 下尊重 cgroup CPU 限额，运行中周期性更新 | [Ch07](../ch07-scheduler/) · [Ch23](../ch23-build-observability/) |
| Swiss table map | `internal/runtime/maps`：group + 控制字、SIMD 探测、增量扩容 | [Ch02](../ch02-slice-map-string/) |
| 迭代器 | range-over-func、`iter.Seq/Seq2`、`iter.Pull`（运行时协程）、slices/maps/strings 迭代器函数 | [Ch05](../ch05-iterators/) |
| 每次迭代新变量 | `for` 循环变量按迭代作用域（读老代码时仍要识别旧写法） | [Ch05](../ch05-iterators/) · [Ch12](../ch12-concurrency-testing/) |
| 泛型 | 类型集、自引用泛型类型、泛型类型别名、`new(expr)` | [Ch04](../ch04-generics/) |
| `errors.AsType` | 泛型版 `errors.As` | [Ch06](../ch06-errors-panic/) |
| `sync.WaitGroup.Go`、`sync.OnceValue(s)` | 结构化地起 goroutine / 惰性初始化 | [Ch09](../ch09-sync-memory-model/) · [Ch10](../ch10-context-structured/) |
| `weak`、`unique`、`runtime.AddCleanup` | 弱引用、值驻留、替代 finalizer 的清理函数 | [Ch14](../ch14-gc/) |
| `testing/synctest` | 气泡内假时钟，确定性测试并发代码 | [Ch12](../ch12-concurrency-testing/) |
| `testing.B.Loop` | 标准 benchmark 写法 | [Ch15](../ch15-profiling-bench/) |
| `runtime/trace.FlightRecorder` | 内存环形缓冲持续记录 trace，事后快照 | [Ch15](../ch15-profiling-bench/) |
| PGO | `default.pgo` 驱动内联与去虚拟化 | [Ch13](../ch13-alloc-escape-compiler/) |
| `log/slog` | 结构化日志、`NewMultiHandler` | [Ch06](../ch06-errors-panic/) |
| `go.mod` `tool` / `ignore` 指令、`go fix` 现代化 + `//go:fix inline` | 工程化基线 | [Ch22](../ch22-modules-testing/) |
| `os.Root` | 目录受限的文件访问 | [Ch24](../ch24-runtime-boundaries/) |

---

## 二、Java 25（LTS）

### 2.1 JDK 25 的 18 个 JEP

| JEP | 标题 | 状态 | 章节 |
|---|---|---|---|
| 470 | PEM Encodings of Cryptographic Objects | Preview | [Ch20](../ch20-http-rpc/) |
| 502 | Stable Values | Preview | [Ch09](../ch09-sync-memory-model/) · [Ch13](../ch13-alloc-escape-compiler/) |
| 503 | Remove the 32-bit x86 Port | 正式 | [Ch00](../ch00-setup/)（了解） |
| 505 | Structured Concurrency（第五次预览） | Preview | [Ch10](../ch10-context-structured/) |
| 506 | Scoped Values | 正式 | [Ch10](../ch10-context-structured/) |
| 507 | Primitive Types in Patterns, instanceof, and switch（第三次预览） | Preview | [Ch03](../ch03-interfaces/) |
| 508 | Vector API（第十次孵化） | Incubator | [Ch16](../ch16-zero-alloc-dod/) |
| 509 | JFR CPU-Time Profiling | Experimental（仅 Linux） | [Ch15](../ch15-profiling-bench/) |
| 510 | Key Derivation Function API | 正式 | [Ch20](../ch20-http-rpc/) |
| 511 | Module Import Declarations | 正式 | [Ch00](../ch00-setup/) · [Ch22](../ch22-modules-testing/) |
| 512 | Compact Source Files and Instance Main Methods | 正式 | [Ch00](../ch00-setup/) |
| 513 | Flexible Constructor Bodies | 正式 | [Ch04](../ch04-generics/) |
| 514 | Ahead-of-Time Command-Line Ergonomics | 正式 | [Ch13](../ch13-alloc-escape-compiler/) · [Ch23](../ch23-build-observability/) |
| 515 | Ahead-of-Time Method Profiling | 正式 | [Ch13](../ch13-alloc-escape-compiler/) · [Ch23](../ch23-build-observability/) |
| 518 | JFR Cooperative Sampling | 正式 | [Ch15](../ch15-profiling-bench/) |
| 519 | Compact Object Headers | 正式（需显式开启） | [Ch01](../ch01-values-memory/) · [Ch14](../ch14-gc/) |
| 520 | JFR Method Timing & Tracing | 正式 | [Ch15](../ch15-profiling-bench/) |
| 521 | Generational Shenandoah | 正式 | [Ch14](../ch14-gc/) |

### 2.2 Java 25 现状（此前版本定稿、属于当前 Java 的核心能力）

| 能力 | 要点 | 章节 |
|---|---|---|
| 虚拟线程 + 同步不再 pin 载体（JEP 444 / 491） | Continuation、FJP 调度器、剩余 pinning 场景 | [Ch07](../ch07-scheduler/) |
| record / sealed / 模式匹配 switch / record 模式（JEP 395/409/440/441） | 代数数据类型 + 穷尽性检查 | [Ch03](../ch03-interfaces/) |
| 未命名变量与模式 `_`（JEP 456） | 与 Go 的空白标识符对照 | [Ch00](../ch00-setup/) |
| Sequenced Collections（JEP 431） | 集合接口体系 | [Ch02](../ch02-slice-map-string/) |
| Stream Gatherers（JEP 485） | 自定义中间操作 | [Ch05](../ch05-iterators/) |
| FFM API（JEP 454）、Unsafe 内存访问方法弃用与告警（JEP 471/498） | 堆外内存、原生调用 | [Ch16](../ch16-zero-alloc-dod/) · [Ch24](../ch24-runtime-boundaries/) |
| 分代 ZGC（唯一模式）、G1 | 低延迟 GC 选型 | [Ch14](../ch14-gc/) |
| AOT 类加载与链接（JEP 483） | Leyden 第一步，配合 514/515 | [Ch13](../ch13-alloc-escape-compiler/) · [Ch23](../ch23-build-observability/) |
| Class-File API（JEP 484） | 字节码生成/改写，Agent 与热更新 | [Ch24](../ch24-runtime-boundaries/) |
| Markdown 文档注释（JEP 467） | 对照 Go doc 注释约定 | [Ch22](../ch22-modules-testing/) |
| 多文件源码启动（JEP 458） | `bin/jrun` 的基础 | [Ch00](../ch00-setup/) |

> Preview / Incubator 特性运行需要 `--enable-preview`（`bin/jrun` 已默认带上），孵化模块还需 `--add-modules jdk.incubator.vector`。
