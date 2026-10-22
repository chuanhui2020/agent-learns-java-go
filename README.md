# agent-learns-java-go

> 面向**资深 Java 游戏服务器开发**的双轨深入学习仓库：**深度复习 Java 25（LTS）× 深入学习 Go 1.27**。
>
> **一句话总纲**：不是"会写 Go 语法"，而是能对 Go 1.27 和 Java 25 **讲清实现、拿出数据、做出取舍** —— 在同一个游戏服问题上，两种运行时各自怎么调度、怎么分配、怎么回收、怎么扛连接，你都能下到源码层给出判断。

- 版本基线：**Go 1.27**（`go.mod` 声明 `go 1.27.0`，本机用 1.27.x 最新补丁）· **Java 25**（LTS，preview 特性用 `--enable-preview`）。只追踪这两个版本，不讲版本演进史。
- 方法论与验收标准：[`docs/methodology.md`](docs/methodology.md)（**开始前必读**：双轨结构、L1–L4 能力分级、实验报告规范）
- 特性基线：[`docs/baseline.md`](docs/baseline.md)（Go 1.27 新增与 Java 25 全部 18 个 JEP → 落在哪一章）
- 源码地图：[`docs/source-map.md`](docs/source-map.md)（Go 1.27 runtime/编译器 与 JDK 25 / HotSpot 源码 → 对应章节）

---

## 怎么用这个仓库

每一章一个目录 `chNN-xxx/`，结构统一：

```
chNN-xxx/
├─ README.md          # 大纲 → 带教时扩写为完整讲义（双轨讲解 + 机制级对照表 + 实验 + 答辩题）
├─ go/
│  ├─ demo/           # 可运行讲解示例：go run ./chNN-xxx/go/demo
│  ├─ exercises/      # 练习桩 + 表驱动测试/基准（你实现）：go test ./chNN-xxx/go/...
│  └─ lab/            # 实验代码（benchmark / pprof / trace），产出数据写进 notes/
├─ java/
│  ├─ *.java          # Java 25 深度复习 demo（单文件直跑）：bin/jrun chNN-xxx/java/Xxx.java
│  └─ drills/         # Java 复习练习（自校验，失败退出码非 0）：bin/jrun chNN-xxx/java/drills/Xxx.java
└─ notes/             # 你的源码阅读笔记 + 实验报告（验收材料）
```

**带教循环**（每章一个回合）：
1. 我把本章 `README.md` 从大纲扩写成完整讲义，写好 `go/demo`、`java/` demo，留好 `go/exercises`（初始为**红**）和 `java/drills`；
2. 你按「🔬 实验」跑出数据、按「源码阅读」写笔记（`notes/`）；
3. 你实现练习让测试转**绿**（并过 `-race`），我 review，专门纠"用 Java 思维写 Go"和"只会用不懂原理"；
4. 你回答「✅ 验收答辩题」，达到 **L3** 才算过章（见方法论），勾掉进度表。

### 运行命令速查

| 目的 | 命令 |
|---|---|
| 跑某章 Go 示例 | `go run ./ch00-setup/go/demo` |
| 跑某章 Go 练习测试 | `go test ./ch00-setup/go/...` |
| 带竞态检测（Part 2 起必跑，需 cgo，见「环境」） | `go test -race ./chNN-xxx/go/...` |
| 跑基准并做统计对比 | `go test -bench=. -count=10 ./chNN-xxx/go/lab > new.txt` → `benchstat old.txt new.txt` |
| 看逃逸/内联决策 | `go build -gcflags='-m=2' ./chNN-xxx/go/lab` |
| 全仓库测试 / 静态检查 | `go test ./...` / `go vet ./...` |
| 现代化改写建议 | `go fix -diff ./...` |
| 跑某章 Java demo | `bin/jrun ch00-setup/java/Hello.java` |
| Java 飞行记录 | `bin/jrun -XX:StartFlightRecording=duration=30s,filename=rec.jfr chNN-xxx/java/Xxx.java` |

---

## 环境

- **Go 1.27.x**：`go.mod` 声明 `go 1.27.0`。本机 Go 低于 1.27 时，`GOTOOLCHAIN=auto`（默认值）会自动下载 1.27 工具链；仍建议直接装 1.27.x 最新补丁，IDE（GoLand/IDEA）也指向它。
  ```bash
  # Windows
  winget install GoLang.Go
  # macOS
  brew install go
  ```
- **JDK 25**（任意发行版：Oracle / Temurin 均可）：
  ```bash
  # Windows
  winget install EclipseAdoptium.Temurin.25.JDK
  # macOS
  brew install --cask temurin@25
  ```
- `bin/jrun` 是 bash 脚本：Windows 下用 Git Bash 运行（脚本已把 Java 输出统一为 UTF-8，避免中文乱码）。
- **竞态检测 `-race` 依赖 cgo**：Windows 上默认 `CGO_ENABLED=0` 且没有 gcc，`go test -race` 会直接报错。二选一：
  1. 装 C 工具链（如 MSYS2 的 mingw-w64 gcc），然后 `go env -w CGO_ENABLED=1`；
  2. **推荐**：从 Part 2 起在 **WSL2（Linux）** 里跑并发和性能实验。epoll、cgroup、`perf`、async-profiler 也只在 Linux 上有意义（见方法论第六节）。
- 后期按需安装（到对应章节再装）：`benchstat`（Ch15）、JMH（Ch15）、async-profiler（Ch15，Linux/macOS）、`protoc`（Ch19）、Redis / PostgreSQL（Ch21）、`golangci-lint` / `govulncheck` / `dlv`（Ch22–23）。

---

## 章节地图（进度）

6 个 Part、26 章。⭐ = 重头戏。工时只是估算（含实验和源码阅读），按"能讲清原理并拿出数据"的标准算。

| 章 | 标题 | 核心内容（Go 1.27 ⟷ Java 25） | 估时 | 状态 |
|----|------|------|------|------|
| **Part 0** | **基建** | | | |
| [00](ch00-setup/) | 工具链、版本治理与语法速通 | go/toolchain 指令、GODEBUG/GOEXPERIMENT、go fix/vet/doc ⟷ JEP 512/511/458、preview 机制、JDK 工具箱 | 8h | 🟡 按新标准重修 |
| **Part 1** | **语言内核** | | | |
| [01](ch01-values-memory/) | 值语义与内存表示 | struct 布局/对齐/填充、指针与可寻址性、方法集 ⟷ 对象头、JEP 519 紧凑对象头、JOL | 10h | ⚪ |
| [02](ch02-slice-map-string/) | slice · string · map 底层 | growslice、别名陷阱、string/[]byte 转换、Swiss table map ⟷ ArrayList/HashMap/CHM、紧凑字符串 | 12h | ⚪ |
| [03](ch03-interfaces/) | 接口、动态派发与组合 | iface/eface/itab、nil 接口陷阱、嵌入与提升 ⟷ vtable/itable、CHA/内联缓存、sealed + 模式匹配 | 12h | ⚪ |
| [04](ch04-generics/) | 泛型深入与 API 设计 | 类型集、GC shape stenciling + 字典、**1.27 泛型方法**、选项模式 ⟷ 类型擦除、桥方法、JEP 513 | 12h | ⚪ |
| [05](ch05-iterators/) | 迭代器与数据管线 | range-over-func、iter.Pull 与协程实现、slices/maps 迭代器 ⟷ Stream/Spliterator、Gatherers | 8h | ⚪ |
| [06](ch06-errors-panic/) | 错误、panic 与 defer 机制 | 错误链、errors.AsType、panic/recover 实现、open-coded defer、slog ⟷ 异常成本、try-with-resources | 10h | ⚪ |
| **Part 2** | **并发（重头戏）** | | | |
| [07](ch07-scheduler/) | GMP 调度器 vs Loom 虚拟线程 ⭐ | G/M/P、work stealing、sysmon、异步抢占、栈增长 ⟷ Continuation、FJP 载体、JEP 491 去 pinning | 20h | ⚪ |
| [08](ch08-channels-select/) | channel 与 select 源码级 ⭐ | hchan/sudog、直接拷贝、select 加锁顺序、timer channel ⟷ BlockingQueue 家族、JCTools、无 select 的代价 | 16h | ⚪ |
| [09](ch09-sync-memory-model/) | 同步原语与内存模型 ⭐ | Mutex 饥饿模式、sema、sync.Pool/Map、atomic、Go 内存模型 ⟷ JMM、VarHandle、AQS、StableValue | 18h | ⚪ |
| [10](ch10-context-structured/) | 取消、超时与结构化并发 ⭐ | context 取消树、AfterFunc、errgroup、优雅关闭 ⟷ JEP 505 StructuredTaskScope、JEP 506 ScopedValue | 12h | ⚪ |
| [11](ch11-actor/) | Actor 与游戏服线程模型 ⭐ | mailbox/ask/监督、tick 主循环、时间轮、背压 ⟷ Pekko dispatcher、Disruptor、Netty EventLoop | 16h | ⚪ |
| [12](ch12-concurrency-testing/) | 并发正确性工程 ⭐ | race detector 原理、**1.27 goroutineleak profile**、synctest ⟷ jcstress、JFR 虚拟线程事件、线程转储 | 12h | ⚪ |
| **Part 3** | **内存与性能（游戏服命脉）** | | | |
| [13](ch13-alloc-escape-compiler/) | 分配器、逃逸分析与编译优化 ⭐ | mcache/mcentral/mheap、**1.27 按尺寸特化 malloc**、逃逸/内联/PGO/BCE ⟷ TLAB、C2 标量替换、分层编译、AOT 缓存 | 18h | ⚪ |
| [14](ch14-gc/) | GC 深度：Green Tea vs ZGC/Shenandoah ⭐ | 三色标记、混合写屏障、pacer、GOGC/GOMEMLIMIT、Green Tea、weak/cleanup ⟷ G1、分代 ZGC、JEP 521 | 20h | ⚪ |
| [15](ch15-profiling-bench/) | 剖析与基准方法论 ⭐ | pprof 全家桶、trace、FlightRecorder、B.Loop、benchstat ⟷ JFR（JEP 509/518/520）、JMH、async-profiler | 16h | ⚪ |
| [16](ch16-zero-alloc-dod/) | 零分配热路径与数据导向设计 ⭐ | sync.Pool、SoA/ECS、伪共享、unsafe 规则、**1.27 simd** ⟷ Vector API（JEP 508）、FFM 堆外内存 | 16h | ⚪ |
| **Part 4** | **网络与服务（游戏服主战场）** | | | |
| [17](ch17-netpoll-io/) | netpoller 与 I/O 模型 ⭐ | epoll/kqueue/IOCP 集成、park/ready、deadline 实现、io 接口族 ⟷ NIO Selector、Netty、虚拟线程 poller | 14h | ⚪ |
| [18](ch18-tcp-gateway/) | TCP 长连接网关实战 ⭐ | 分帧、读写分离、写合并、慢客户端、心跳、优雅排空 ⟷ Netty codec/水位线/IdleStateHandler | 18h | ⚪ |
| [19](ch19-serialization/) | 序列化与协议设计 | protobuf-go、**1.27 encoding/json/v2 + jsontext**、手写二进制编解码、schema 演进 ⟷ protobuf-java、Jackson | 12h | ⚪ |
| [20](ch20-http-rpc/) | HTTP、gRPC 与服务治理 | net/http 内部、五种超时、连接池、HTTP/2、gRPC 流 ⟷ HttpClient、gRPC-java、Netty 服务端 | 12h | ⚪ |
| [21](ch21-storage-cache/) | 存储与缓存集成 | database/sql 连接池、事务、pgx、Redis 管线/Lua/排行榜 ⟷ HikariCP、JDBC + 虚拟线程、Lettuce | 10h | ⚪ |
| **Part 5** | **生产工程** | | | |
| [22](ch22-modules-testing/) | 模块、测试与静态分析 | MVS、go.work、tool 指令、fuzz、vet、go fix 现代化 ⟷ Maven 依赖仲裁、JPMS、JUnit 5、Error Prone | 10h | ⚪ |
| [23](ch23-build-observability/) | 构建、部署与可观测性 | 静态二进制、容器内 GOMAXPROCS/GOMEMLIMIT、崩溃诊断、OTel ⟷ jlink、AOT 缓存、容器感知 JVM、生产 JFR | 12h | ⚪ |
| [24](ch24-runtime-boundaries/) | 运行时边界：热更新与原生互操作 | 配置热加载、plugin 局限、进程级热重启、脚本层、cgo ⟷ Instrumentation/Arthas、类卸载、FFM + jextract | 12h | ⚪ |
| **Part 6** | **实战** | | | |
| [25](ch25-capstone/) | Capstone：分布式房间制游戏服 ⭐ | 网关 + 房间 actor + 匹配 + 帧/状态同步 + 存储 + 可观测 + 压测报告；关键服务做 Java 25 对照实现 | 40h+ | ⚪ |

状态图例：⚪ 未开始 · 🟡 带教中 · 🟢 已带教（练习待完成）· ✅ 练习完成并通过 L3 答辩

**建议节奏**：Part 1 别跳（Go 的内存表示是后面所有性能章的地基）；Part 2、3 是投入最多的地方；Part 4 结束后就可以开始 Capstone 的网关部分，和 Part 5 并行推进。
