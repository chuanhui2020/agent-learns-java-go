# agent-learns-java-go

> 面向**资深 Java 服务端开发**的双轨深入学习仓库：**深度复习 Java 25（LTS）× 深入学习 Go 1.27**。只学**语言与运行时**，不学工程实践。
>
> **一句话总纲**：不是"会写 Go 语法"，而是能对 Go 1.27 和 Java 25 **讲清规范、讲清实现、用数据验证、讲清设计取舍** —— 同一条语义（拷贝、派发、闭包、泛型、调度、内存可见性、分配、回收），两门语言各自怎么规定、怎么实现、为什么这样设计，你都能下到规范条款和源码层给出判断。

- 版本基线：**Go 1.27**（`go.mod` 声明 `go 1.27.0`，本机用 1.27.x 最新补丁）· **Java 25**（LTS，preview 特性用 `--enable-preview`）。只追踪这两个版本，不讲版本演进史。
- 方法论与验收标准：[`docs/methodology.md`](docs/methodology.md)（**开始前必读**：学习范围、双轨结构、L1–L4 能力分级、实验报告规范）
- 特性基线：[`docs/baseline.md`](docs/baseline.md)（Go 1.27 新增与 Java 25 全部 18 个 JEP → 落在哪一章）
- 源码地图：[`docs/source-map.md`](docs/source-map.md)（Go 1.27 runtime/编译器 与 JDK 25 / HotSpot 源码 → 对应章节）

### 学什么、不学什么

- **学**：语言规范（类型系统、值语义、函数与闭包、接口、泛型、迭代器、错误模型、反射与 unsafe）、并发模型与内存模型、运行时（调度、channel、同步原语、分配器、GC、netpoller、系统调用与 cgo）、编译器优化，以及验证这些机制所需的测量手段的原理。
- **不学**：网关 / RPC / HTTP 服务、序列化协议、数据库与缓存集成、依赖管理、CI、构建部署、可观测性体系、热更新、项目实战。

---

## 怎么用这个仓库

每一章一个目录 `chNN-xxx/`，结构统一：

```
chNN-xxx/
├─ README.md          # 大纲 → 带教时扩写为完整讲义（双轨讲解 + 机制级对照表 + 实验 + 答辩题）
├─ go/
│  ├─ demo/           # 可运行讲解示例：go run ./chNN-xxx/go/demo
│  ├─ exercises/      # 练习桩 + 表驱动测试/基准（你实现）：go test ./chNN-xxx/go/...
│  └─ lab/            # 实验代码（benchmark / pprof / trace / 编译器诊断），产出数据写进 notes/
├─ java/
│  ├─ *.java          # Java 25 深度复习 demo（单文件直跑）：bin/jrun chNN-xxx/java/Xxx.java
│  └─ drills/         # Java 复习练习（自校验，失败退出码非 0）：bin/jrun chNN-xxx/java/drills/Xxx.java
└─ notes/             # 你的源码阅读笔记 + 实验报告（验收材料）
```

**带教循环**（每章一个回合）：
1. 我把本章 `README.md` 从大纲扩写成完整讲义，写好 `go/demo`、`java/` demo，留好 `go/exercises`（初始为**红**）和 `java/drills`；
2. 你读规范原文、按「源码阅读」写笔记（`notes/`），按「🔬 实验」**先预测、再跑数据**；
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
| 看汇编 / 边界检查 | `go build -gcflags='-S' ./...` / `go build -gcflags='-d=ssa/check_bce' ./...` |
| 全仓库测试 / 静态检查 | `go test ./...` / `go vet ./...` |
| 跑某章 Java demo | `bin/jrun ch00-setup/java/Hello.java` |
| 看 Java 字节码 | `javac -d out X.java && javap -c -v -p out/X.class` |
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
- **竞态检测 `-race` 和 cgo（Ch19）都依赖 C 工具链**：Windows 上默认 `CGO_ENABLED=0` 且没有 gcc，`go test -race` 会直接报错。二选一：
  1. 装 C 工具链（如 MSYS2 的 mingw-w64 gcc），然后 `go env -w CGO_ENABLED=1`；
  2. **推荐**：从 Part 2 起在 **WSL2（Linux）** 里跑并发和运行时实验。epoll、cgroup、信号、`perf`、async-profiler 也只在 Linux 上有意义（见方法论第六节）。
- 后期按需安装（到对应章节再装）：JOL（Ch02）、`benchstat` / JMH / async-profiler（Ch17，async-profiler 仅 Linux/macOS）、hsdis（Ch15，可选，看 C2 汇编）。

---

## 章节地图（进度）

5 个 Part、20 章。⭐ = 重头戏。工时只是估算（含读规范、读源码和做实验），按"能讲清原理并拿出数据"的标准算。

| 章 | 标题 | 核心内容（Go 1.27 ⟷ Java 25） | 估时 | 状态 |
|----|------|------|------|------|
| **Part 0** | **起步** | | | |
| [00](ch00-setup/) | 工具链、语言版本语义与语法速通 | `go` 行三重语义、GOTOOLCHAIN、GODEBUG/GOEXPERIMENT ⟷ JEP 512/511/458、preview/incubator 机制、javap/jshell | 6h | 🟡 按新标准重修 |
| **Part 1** | **语言内核：规范 × 实现** | | | |
| [01](ch01-types-constants/) | 类型系统、常量与数值语义 | 类型定义 vs 别名、底层类型、可赋值性/可转换性、无类型常量与任意精度、iota、溢出与浮点 ⟷ 原始类型与装箱、数值提升、编译期常量、JEP 507 原始类型模式 | 8h | ⚪ |
| [02](ch02-values-memory/) | 值语义与内存表示 | struct 布局/对齐/填充、指针与可寻址性、方法集 ⟷ 对象头、JEP 519 紧凑对象头、JOL | 10h | ⚪ |
| [03](ch03-slice-map-string/) | slice · string · map 底层 | growslice、别名陷阱、string/[]byte 转换、UTF-8 与 rune、Swiss table map ⟷ ArrayList/HashMap/CHM、紧凑字符串 | 12h | ⚪ |
| [04](ch04-functions-closures/) | 函数、闭包与初始化顺序 | 函数值表示、闭包捕获变量、方法值/方法表达式、每次迭代新变量、包初始化顺序、函数式选项 ⟷ lambda + invokedynamic、effectively final、类初始化、JEP 513 | 10h | ⚪ |
| [05](ch05-interfaces/) | 接口、动态派发与组合 | iface/eface/itab、nil 接口陷阱、嵌入与提升 ⟷ vtable/itable、CHA/内联缓存、sealed + 模式匹配 | 12h | ⚪ |
| [06](ch06-generics/) | 泛型：类型集、实现与泛型方法 | 类型集、类型推断、GC shape stenciling + 字典、**1.27 泛型方法** ⟷ 类型擦除、桥方法、通配符与变型 | 12h | ⚪ |
| [07](ch07-iterators/) | 迭代器与惰性求值 | range-over-func 的编译改写、iter.Pull 与协程、slices/maps 迭代器 ⟷ Iterator/Spliterator/Stream、Gatherers | 8h | ⚪ |
| [08](ch08-errors-panic/) | 错误、panic 与 defer 机制 | 错误链、errors.AsType、panic/recover 实现、open-coded defer、goroutine 崩溃语义 ⟷ 异常表与栈展开成本、try-with-resources | 10h | ⚪ |
| [09](ch09-reflect-unsafe/) | 反射、unsafe 与运行时类型信息 | `abi.Type` 与 reflect 实现、struct tag、json/v2 反射案例、unsafe.Pointer 合法模式 ⟷ Class 与类加载、反射/MethodHandle/VarHandle、Unsafe 退场（JEP 471/498） | 12h | ⚪ |
| **Part 2** | **并发模型与运行时（重头戏）** | | | |
| [10](ch10-scheduler/) | GMP 调度器 vs Loom 虚拟线程 ⭐ | G/M/P、work stealing、sysmon、异步抢占、栈增长 ⟷ Continuation、FJP 载体、JEP 491 去 pinning | 20h | ⚪ |
| [11](ch11-channels-select/) | channel 与 select 源码级 ⭐ | hchan/sudog、直接拷贝、select 加锁顺序、timer channel ⟷ BlockingQueue 家族、无 select 的代价 | 16h | ⚪ |
| [12](ch12-sync-memory-model/) | 同步原语与内存模型 ⭐ | Mutex 饥饿模式、sema、sync.Pool/Map、atomic、Go 内存模型 ⟷ JMM、VarHandle、AQS、StableValue | 18h | ⚪ |
| [13](ch13-context-structured/) | 取消、超时与结构化并发 | context 取消树、AfterFunc、WaitGroup.Go ⟷ 中断模型、JEP 505 StructuredTaskScope、JEP 506 ScopedValue | 12h | ⚪ |
| [14](ch14-race-leak-detection/) | 竞态与泄漏检测的实现原理 | race detector（TSan 影子内存与向量时钟）、**1.27 goroutineleak profile**、synctest 气泡与假时钟 ⟷ jcstress、线程转储、JFR 虚拟线程事件 | 10h | ⚪ |
| **Part 3** | **内存管理与编译器** | | | |
| [15](ch15-alloc-escape-compiler/) | 分配器、逃逸分析与编译优化 ⭐ | mcache/mcentral/mheap、**1.27 按尺寸特化 malloc**、逃逸/内联/PGO/BCE、**1.27 simd** ⟷ TLAB、C2 标量替换、分层编译、AOT 缓存、Vector API | 18h | ⚪ |
| [16](ch16-gc/) | GC 深度：Green Tea vs G1/ZGC/Shenandoah ⭐ | 三色标记、混合写屏障、pacer、GOGC/GOMEMLIMIT、Green Tea、weak/cleanup ⟷ G1、分代 ZGC、JEP 521 | 20h | ⚪ |
| [17](ch17-profiling-bench/) | 剖析与基准的原理 | pprof 采样机制、trace、FlightRecorder、B.Loop、benchstat 统计 ⟷ JFR（JEP 509/518/520）、JMH、安全点偏差 | 12h | ⚪ |
| **Part 4** | **运行时与操作系统的边界** | | | |
| [18](ch18-netpoll-io/) | netpoller 与 I/O 模型 | epoll/kqueue/IOCP 集成、park/ready、deadline 实现、io 接口族 ⟷ NIO Selector、虚拟线程 poller | 14h | ⚪ |
| [19](ch19-syscall-cgo-ffi/) | 系统调用、cgo 与 FFM | entersyscall/exitsyscall、cgocall 与 M 绑定、cgo 指针传递规则、LockOSThread、信号 ⟷ JNI、FFM Linker downcall/upcall、Arena 生命周期 | 12h | ⚪ |

状态图例：⚪ 未开始 · 🟡 带教中 · 🟢 已带教（练习待完成）· ✅ 练习完成并通过 L3 答辩

**建议节奏**：Part 1 别跳——类型系统、值语义和函数模型是后面所有运行时章节的地基；Part 2、3 是投入最多的地方；Part 4 依赖 Ch10 的调度器知识，放在最后。
