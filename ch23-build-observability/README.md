# Ch23 · 构建、部署与可观测性

> Part 5 · 估时 12h · 状态：⚪ 未开始 · 从"能编译"到"能上线、能看见、能事后还原现场"

## 🎯 目标（资深标准）

- **L2**：讲清 Go 静态二进制的构成（运行时 + 依赖 + 构建信息）、`CGO_ENABLED` 对链接方式的影响、交叉编译为什么几乎免费；讲清 JVM 的 jlink、CDS / AOT 缓存（JEP 483/514/515）分别优化了启动过程的哪一段。
- **L2**：讲清容器里 GOMAXPROCS（cgroup 感知）、GOMEMLIMIT 与 JVM 的 `ActiveProcessorCount`、`MaxRAMPercentage` 的取值逻辑。
- **L3**：给定一次线上 panic 或 OOM，能用 `GOTRACEBACK`、`debug.SetCrashOutput`、core dump + delve、pprof / trace 还原现场；Java 侧能用 JFR + `jcmd` 做同样的事。
- **L3**：能为一个服务建立完整的可观测性：结构化日志、指标（含 runtime/metrics）、分布式追踪、按需剖析端点，并保证端点安全。
- **L4**：能为游戏后端制定发布与运行规范：镜像、资源配额、版本注入、崩溃收集、告警指标与阈值。

## 🔗 前置

[Ch00](../ch00-setup/)（工具链）、[Ch07](../ch07-scheduler/)（GOMAXPROCS）、[Ch14](../ch14-gc/)（GOGC / GOMEMLIMIT）、[Ch15](../ch15-profiling-bench/)（pprof / trace / JFR）、[Ch22](../ch22-modules-testing/)（模块与构建）

## 💡 关键洞察

1. **Go 的部署单元是一个自描述的二进制。** 它自带运行时，`go version -m` 能读出模块版本、构建参数、VCS 修订号和 DefaultGODEBUG。很多"线上跑的到底是哪个版本"的问题，二进制自己就能回答，不需要额外的版本文件。
2. **容器资源必须同时告诉运行时。** cgroup 的 CPU limit 会被 Go 自动感知（GOMAXPROCS），**内存 limit 不会**；不设 GOMEMLIMIT，GC 就只按 GOGC 走，容易在 limit 附近被 OOM Kill。JVM 正好相反：默认按容器内存的百分比设堆（`MaxRAMPercentage`）。
3. **崩溃现场要提前布置。** panic 时默认只打印到 stderr，容器重启后就没了。`debug.SetCrashOutput` 把崩溃信息写到持久文件，`GOTRACEBACK=crash` 生成 core dump，**1.27 新增**的 traceback 标签让你直接看到崩溃的 goroutine 属于哪个房间、哪个玩家。

## ☕ Java 25 深度复习

### 运行时裁剪与启动优化
- **jlink**：基于 JPMS 依赖（`jdeps --print-module-deps` 计算）生成只包含所需模块的自定义运行时镜像，可配合 `--strip-debug`、`--no-header-files` 缩小体积。对照 Go：Go 二进制天然只链接用到的包。
- **CDS / AOT 缓存（Leyden 系列）**：
  - **JEP 483 AOT 类加载与链接**：训练运行时记录加载和链接过的类，生成 AOT 缓存，生产启动时直接映射，跳过类解析、校验、链接。
  - **JEP 514 AOT 命令行简化（正式）**：`-XX:AOTCacheOutput=app.aot` 一步完成"训练 + 生成缓存"，生产启动用 `-XX:AOTCache=app.aot`。原来的两步式写法用 `-XX:AOTMode=record` / `create` 配合 `-XX:AOTConfiguration`。
  - **JEP 515 AOT 方法 profiling（正式）**：把训练期收集的方法执行 profile 存进缓存，JIT 启动后能更早编译热点方法，缩短达峰时间（细节见 [Ch13](../ch13-alloc-escape-compiler/)）。
  - 约束：训练和生产必须使用**同一 JDK 构建、同一 classpath**，否则缓存会被拒绝使用（JVM 会打印原因）。
- **GraalVM native-image**：可以把 Java 编译成原生二进制，但不属于 JDK 25，反射和动态类加载需要额外配置。这里只作为对照，不深入。

### 容器感知
- Linux 上 JVM 默认开启容器支持：从 cgroup 读取 CPU 和内存限额。
- CPU：`Runtime.availableProcessors()` 依据 CPU limit 计算；可以用 `-XX:ActiveProcessorCount=N` 覆盖。它决定 GC 线程数、ForkJoinPool 公共池和虚拟线程调度器的默认并行度。
- 内存：默认最大堆 = 容器内存 × `MaxRAMPercentage`（默认 25%）。游戏服通常要显式调高（比如 60–75%），并为堆外内存（直接内存、线程栈、元空间、JIT 代码缓存）留出余量。用 `-XX:NativeMemoryTracking=summary` + `jcmd VM.native_memory` 核算。

### 生产诊断
- **JFR 常开**：`-XX:StartFlightRecording=disk=true,maxage=6h,settings=default`，出事时 `jcmd <pid> JFR.dump filename=...` 导出最近的记录（类似 Go 的 FlightRecorder，见 [Ch15](../ch15-profiling-bench/)）。
- **崩溃与 OOM**：`-XX:+HeapDumpOnOutOfMemoryError`、`-XX:HeapDumpPath`、`-XX:ErrorFile`（hs_err 日志）、`-XX:+ExitOnOutOfMemoryError`（让编排系统重启，而不是带病运行）。
- **线程转储**：`jcmd <pid> Thread.dump_to_file -format=json`（包含虚拟线程）。

## 🐹 Go 1.27 深入

### 规范层
- 规范之外的内容为主。相关的只有：`package main` + `func main()` 是程序入口；`os.Exit` 不会执行 defer；main 返回即进程退出（不等其他 goroutine）。这三条决定了优雅退出必须显式编排（[Ch10](../ch10-context-structured/)）。

### 工具链与运行时层
- **静态链接与 cgo**：`CGO_ENABLED=0` 时 net、os/user 等包使用纯 Go 实现，产物不依赖 libc，可以放进 `scratch` / distroless 镜像。开了 cgo 就会动态链接 libc（还会影响 DNS 解析器的选择）。本机 Windows 默认就是 `CGO_ENABLED=0`。
- **交叉编译**：`GOOS=linux GOARCH=amd64 go build`，不需要额外工具链（cgo 除外）。`GOAMD64=v3` 等微架构级别会影响可用指令（比如 FMA），要和线上 CPU 匹配。
- **构建信息**：
  - `-ldflags="-s -w"` 去掉符号表和 DWARF 以缩小体积，但会影响调试和部分剖析体验，要权衡。
  - `-ldflags="-X main.version=..."` 注入版本号；`-trimpath` 去掉本机路径，使构建可复现、避免泄露路径。
  - VCS 信息默认会被写入（`-buildvcs`）；运行时用 `debug.ReadBuildInfo()` 读取，离线用 `go version -m <bin>`。
- **容器资源**：
  - GOMAXPROCS：Linux 上尊重 cgroup CPU limit 并周期性更新（机制见 [Ch07](../ch07-scheduler/)）。**不要再手动写死**，否则会关闭自动行为。
  - GOMEMLIMIT：设为容器内存的 85–90% 左右（给非堆内存留余量），GOGC 保持默认或按 [Ch14](../ch14-gc/) 的实验结论调整。可以在启动时用 `debug.SetMemoryLimit` 根据 cgroup 文件动态计算。
- **崩溃诊断**：
  - `GOTRACEBACK`：`none` / `single`（默认）/ `all` / `system` / `crash`（打印后 abort，Linux 上产生 core dump），Windows 上另有 `wer`。程序内可用 `debug.SetTraceback` 调整。
  - `debug.SetCrashOutput(f, opts)`：把致命错误的输出**额外**写到一个文件（比如持久卷上的崩溃日志，或者交给一个监控子进程上报）。
  - **1.27 新增**：模块声明 `go 1.27` 及以上时，traceback 的 goroutine 头行会带上 `runtime/pprof` 的 goroutine 标签。用 `pprof.Do(ctx, pprof.Labels("room", id), ...)` 给房间 goroutine 打标签，崩溃时直接定位到房间。标签里如果可能有敏感信息，用 `GODEBUG=tracebacklabels=0` 关闭。
  - core dump + delve：`dlv core <bin> <core>` 查看所有 goroutine 的栈和变量（delve 是第三方工具）。`debug.WriteHeapDump` 可以导出堆转储做离线分析。
  - `debug.SetMaxThreads`：线程数上限（默认 10000），超过直接崩溃；阻塞型 cgo 多的服务要关注。
- **可观测性**：
  - 日志：`log/slog` 输出 JSON（[Ch06](../ch06-errors-panic/)），带 trace ID、房间 ID、玩家 ID。
  - 指标：`runtime/metrics` 提供 GC、调度、内存的稳定指标名（`/sched/pauses/total/gc:seconds`、`/sched/latencies:seconds`、`/memory/classes/...`），用 Prometheus client（第三方）导出。
  - 追踪：OpenTelemetry Go SDK（第三方），HTTP / gRPC 用中间件自动传播上下文；自定义协议的网关要在消息头里手动携带 trace context。
  - 剖析端点：`net/http/pprof` 只挂在**内网管理端口**，不要和业务端口混用；`/debug/pprof/goroutineleak` 用于泄漏巡检（[Ch12](../ch12-concurrency-testing/)）。

### 源码阅读清单（`$GOROOT/src`）
| 文件 | 函数 / 类型 | 带着什么问题读 |
|---|---|---|
| `runtime/debug/stack.go` | `SetCrashOutput`、`CrashOptions` | 崩溃输出为什么要在启动早期设置？fatal error 发生时运行时处于什么状态，为什么只能写文件描述符？ |
| `runtime/runtime1.go` | GOTRACEBACK 解析、`tracebacklabels` 调试变量 | `crash` 和 `system` 的区别是什么？traceback 标签是怎么按模块版本默认开启的？ |
| `runtime/debug/mod.go` | `ReadBuildInfo`、`BuildInfo` | 构建信息存在二进制的哪里？`go version -m` 为什么不运行程序也能读出来？ |
| `runtime/debug/garbage.go` | `SetMemoryLimit`、`SetGCPercent`、`SetMaxThreads`、`WriteHeapDump` | 运行中修改内存上限，对正在进行的 GC 周期有什么影响？ |
| `runtime/metrics/description.go` | 指标描述表 | 哪些指标是累计值、哪些是瞬时值、哪些是直方图？怎么把直方图正确地导出给 Prometheus？ |
| `net/http/pprof/pprof.go` | 各端点注册 | 为什么 `import _ "net/http/pprof"` 会把端点挂到 `DefaultServeMux`？这在安全上意味着什么？ |

## 📊 Java 25 ↔ Go 1.27 机制级对照表

| 维度 | Java 25 | Go 1.27 | 取舍 / 游戏服影响 |
|---|---|---|---|
| 部署产物 | JDK（或 jlink 运行时）+ jar / 类路径 | 单个静态二进制 | Go 镜像可以做到几 MB 到几十 MB；Java 需要基础镜像加运行时 |
| 启动到达峰 | 类加载 + 解释执行 + JIT 预热；AOT 缓存（483/514/515）缩短 | 原生代码，启动即达峰（PGO 在编译期生效） | 扩容和故障恢复时 Go 更快；Java 用 AOT 缓存可以显著缩小差距 |
| 交叉编译 | 字节码跨平台，JDK 按平台分发 | `GOOS/GOARCH` 一行搞定（cgo 除外） | Go 在 Windows 开发机上直接产出 Linux 二进制 |
| CPU 配额 | 容器感知 → `availableProcessors`，可以用 `ActiveProcessorCount` 覆盖 | 感知 cgroup CPU limit 并动态更新 GOMAXPROCS | 两边默认都对；手动覆盖要有理由 |
| 内存配额 | 默认堆 = 容器内存 × `MaxRAMPercentage`（25%） | **不感知内存 limit**，必须设 GOMEMLIMIT | Go 服务不设 GOMEMLIMIT 是常见的 OOM Kill 根因 |
| 版本可追溯 | MANIFEST / 自定义版本文件 | `go version -m`、`debug.ReadBuildInfo`（含 VCS 修订号） | Go 不需要额外约定，二进制自描述 |
| 崩溃现场 | hs_err 日志、heap dump、JFR dump | traceback（含 1.27 标签）、`SetCrashOutput`、core + delve | 两边都要把输出写到持久化位置，容器重启前收集 |
| 持续剖析 | JFR 常开，低开销 | pprof 按需采集 + FlightRecorder | Java 的"永远在录"更成熟；Go 要自己搭采集链路 |
| 运行时指标 | JMX / JFR 事件 | `runtime/metrics` 稳定指标名 | Go 的指标名有兼容性承诺，可以直接做告警 |

## 🔬 实验

### 实验 1：镜像大小与启动到达峰（Linux / WSL2 + Docker）
- **目的**：量化部署形态的差异，以及 AOT 缓存对 Java 的帮助。
- **方法**：同一个"HTTP 健康检查 + 简单房间逻辑"服务：
  - Go：`CGO_ENABLED=0 go build -trimpath`，放进 `scratch` 或 distroless 镜像。
  - Java：分三组：普通 JDK 镜像；jlink 运行时；jlink + AOT 缓存（用 `-XX:AOTCacheOutput` 训练生成，生产用 `-XX:AOTCache`）。
- **指标**：镜像大小；从容器启动到第一个请求成功的时间；前 30 秒的 p99 延迟曲线（反映 JIT 预热）。
- **预期现象**：Go 镜像最小、启动最快且无预热曲线；Java 的 AOT 缓存显著缩短启动时间并让延迟更早稳定。

### 实验 2：容器 CPU / 内存限额下的行为（Linux / WSL2 + Docker）
- **方法**：
  - `docker run --cpus=2 --memory=512m` 运行 Go 服务，打印 `runtime.GOMAXPROCS(0)`；分别在不设 GOMEMLIMIT 和设 `GOMEMLIMIT=450MiB` 两种情况下，施加逐渐增长的分配负载。
  - Java 同样限额，打印 `availableProcessors()` 和最大堆，对比默认 `MaxRAMPercentage` 与调到 70% 的行为。
- **指标**：GOMAXPROCS / 处理器数；是否被 OOM Kill；GC 频率（`gctrace` / `-Xlog:gc`）；吞吐。
- **预期现象**：Go 自动把 GOMAXPROCS 设为 2；不设 GOMEMLIMIT 时更容易被 OOM Kill，设了以后 GC 在接近上限时变得积极。Java 默认堆只有约 128MB，容易过早 OOM 或 GC 过于频繁。

### 实验 3：崩溃现场还原
- **方法**：给房间 goroutine 用 `pprof.Do` 打上 `room` 标签，故意触发一个空指针 panic。分别在 `go 1.26.0` 和 `go 1.27.0` 的模块下运行，对比 traceback 头行；再配置 `debug.SetCrashOutput` 写文件，Linux 上用 `GOTRACEBACK=crash` 生成 core，用 delve 打开。
- **预期现象**：1.27 模块的 traceback 头行带房间标签；崩溃文件内容和 stderr 一致；delve 能看到所有 goroutine。
- **记录**：traceback 原文对比、定位到问题房间的步骤。

## 🧪 练习规划

**Go：`go/exercises`**（`go test ./ch23-build-observability/go/...`）
1. `func VersionInfo() Info`：从 `debug.ReadBuildInfo` 提取模块版本、VCS 修订号、是否有未提交改动、Go 版本，并提供 `/version` HTTP 处理器。测试用 `httptest` 验证 JSON 输出。
2. `func MemoryLimitFromCgroup(fsys fs.FS, ratio float64) (int64, bool)`：解析 cgroup v2 的 `memory.max`（处理 `max` 字面量、非法内容），按比例返回 GOMEMLIMIT。用 `fstest.MapFS` 做表驱动测试。
3. `func NewAdminMux(opts AdminOptions) *http.ServeMux`：组装内网管理端口，包括 pprof（不使用 `DefaultServeMux`）、`/metrics`（导出 runtime/metrics 的若干指标）、`/healthz`。测试确认业务 mux 上**没有**任何 `/debug/` 路由。
4. `func InstallCrashOutput(dir string) (io.Closer, error)`：创建带时间戳的崩溃文件并调用 `debug.SetCrashOutput`。测试在子进程里触发 panic（`os/exec` 重新执行测试二进制），断言文件里有 panic 信息。

**Java：`java/drills`**
1. `ContainerAwareDrill.java`：打印并自校验 `availableProcessors()`、最大堆，以及它们与 `-XX:ActiveProcessorCount`、`-XX:MaxRAMPercentage` 参数的关系（drill 里用 `ProcessBuilder` 带不同参数启动子 JVM）。
2. `JfrAlwaysOnDrill.java`：用 `jdk.jfr.Recording` API 在程序内开启录制，制造一段分配压力后导出文件，再用 `jdk.jfr.consumer.RecordingFile` 读回，自校验存在 GC 事件。

## 🎮 游戏服落地

- **镜像与发布**：逻辑服、网关用 `CGO_ENABLED=0` 的静态二进制 + distroless；版本号、Git 修订号写进二进制，GM 后台直接展示"每台服的构建版本"。Java 服务用 jlink + AOT 缓存缩短扩容和故障恢复时间。
- **资源配额**：Kubernetes 里给 Go 服务同时设 CPU limit 和 GOMEMLIMIT（由启动代码按 cgroup 计算，或通过环境变量注入）；Java 服务显式设置 `MaxRAMPercentage`，并核算堆外内存。
- **崩溃收集**：所有 Go 服务启动时安装 `SetCrashOutput` 到持久卷，sidecar 负责上报；房间、玩家 goroutine 用 pprof 标签标记，配合 1.27 的 traceback 标签，崩溃日志直接写着是哪个房间。
- **告警指标**：`/sched/latencies:seconds` p99（房间 tick 抖动）、`/sched/pauses/total/gc:seconds`、goroutine 数、线程数、GOMEMLIMIT 使用率；Java 对应 JFR 的 GC 暂停、safepoint、虚拟线程 pinning 事件。

## ✅ 验收答辩题

- [ ] [L2] 一个 `CGO_ENABLED=0` 的 Go 二进制里都有什么？为什么可以直接放进 `scratch` 镜像？开启 cgo 后有哪些变化？
- [ ] [L2] JEP 483、514、515 分别优化启动过程中的哪一段？AOT 缓存为什么要求训练和生产用同一个 JDK 和 classpath？
- [ ] [L2] 容器限制为 2 核 1GB 时，Go 和 Java 默认的并行度与内存上限分别是多少？各自需要做哪些显式配置？
- [ ] [L2] `GOTRACEBACK` 各个级别有什么区别？`debug.SetCrashOutput` 解决了什么问题？
- [ ] [L3] 一个 Go 服务在 Kubernetes 里每隔几小时被 OOM Kill，但 heap profile 看起来不大。你怎么排查？（提示：GOMEMLIMIT、非堆内存、`/memory/classes/` 指标）
- [ ] [L3] 线上偶发 panic，容器已经重启，日志被截断。下次你会提前布置哪些手段来保住现场？
- [ ] [L3] 用实验 1 的数据说明 AOT 缓存对 Java 启动和预热的实际收益，以及它的局限。
- [ ] [L3] 设计一个管理端口：pprof、指标、健康检查、版本信息，说明怎么防止它暴露到公网。
- [ ] [L4] 为一个"网关 Go + 逻辑服 Go + 后台 Java 25"的游戏后端制定发布规范：镜像、版本注入、资源配额、崩溃收集、核心告警及阈值。

## 🕳️ 常见坑

- **容器里不设 GOMEMLIMIT**：Go 不感知内存 limit，堆按 GOGC 增长直到被 OOM Kill。
- **手动写死 GOMAXPROCS**：关闭了 cgroup 感知和动态更新，limit 调整后不会跟着变。
- **`import _ "net/http/pprof"` 加上业务服务用 `DefaultServeMux`**：pprof 端点直接暴露在业务端口上。
- **`-ldflags="-s -w"` 一刀切**：体积小了，但 core dump 分析和部分调试体验变差。
- **`os.Exit` 跳过 defer**：日志没刷盘、连接没关闭；优雅退出要走正常返回路径。
- **Java 默认 `MaxRAMPercentage`（25%）**：在容器里堆过小，GC 频繁或过早 OOM；调高后又忘了给堆外内存留余量。
- **AOT 缓存训练环境和生产不一致**：缓存被静默拒绝，以为有优化其实没有。启动时检查 JVM 的 AOT 相关日志。
- **在 traceback 标签里放敏感信息**（token、手机号）：崩溃日志会原样打印出来。

## 📚 一手资料

- Go：[Go 1.27 Release Notes](https://go.dev/doc/go1.27)（traceback 标签）、[A Guide to the Go Garbage Collector](https://go.dev/doc/gc-guide)（GOMEMLIMIT）、[Diagnostics](https://go.dev/doc/diagnostics)、`go help build`、`go doc runtime/debug`、`go doc runtime/metrics`、`go doc runtime`（GOTRACEBACK）
- Java：[JEP 483](https://openjdk.org/jeps/483)、[JEP 514](https://openjdk.org/jeps/514)、[JEP 515](https://openjdk.org/jeps/515)、`jlink` 与 `jdeps` 工具文档、JFR 与 `jcmd` 文档
- 第三方：delve 文档、OpenTelemetry Go、Prometheus Go client
