# Ch24 · 运行时边界：热更新与原生互操作

> Part 5 · 估时 12h · 状态：⚪ 未开始 · Go 没有类加载器：热更新要换一种思路做；原生库调用要算清跨边界的成本

## 🎯 目标（资深标准）

- **L2**：讲清 Java 热更新的能力边界（`Instrumentation.redefineClasses` 能改什么、不能改什么，类卸载的条件），以及 Go 为什么在语言层面没有对应能力、`plugin` 包为什么基本不适合生产。
- **L2**：讲清 cgo 调用经过运行时的路径（`cgocall → entersyscall`）、指针传递规则和 `cgocheck`；讲清 FFM 的 downcall / upcall 与 `Arena` 生命周期模型。
- **L3**：能实现并压测一套"配置 / 数值表热加载"（原子快照、校验、回滚、灰度），以及进程级热重启（监听 fd 交接 + 房间排空）；能测量 cgo 与 FFM 的单次调用开销。
- **L4**：能给出游戏服"哪些东西热更、用什么方式热更"的分层方案（数据 / 脚本 / 进程 / 节点），以及 Java 和 Go 各自的边界和风险。

## 🔗 前置

[Ch07](../ch07-scheduler/)（syscall 与 M/P）、[Ch09](../ch09-sync-memory-model/)（`atomic.Pointer`）、[Ch10](../ch10-context-structured/)（优雅关闭）、[Ch16](../ch16-zero-alloc-dod/)（FFM 堆外内存）、[Ch18](../ch18-tcp-gateway/)（连接排空）

## 💡 关键洞察

1. **Java 热更新的本质是"替换方法体"，不是"替换程序"。** `redefineClasses` 不能增删字段和方法、不能改签名和继承关系；Arthas 之类的工具也受这个约束。超出这个范围，就只能用新的类加载器加载新版本，并让旧版本可以被卸载。
2. **Go 的热更新只能分层做。** 数据层（配置、数值表）用原子快照替换；易变逻辑放进脚本层（如 Lua）；代码变更走**进程级替换**（fd 交接、优雅排空、玩家迁移）。`plugin` 要求宿主和插件用完全相同的工具链、构建标签和依赖源码一起构建，不能卸载，也不支持 Windows，实际上等于"重新发版"。
3. **跨边界调用的成本在运行时，不在 C 函数本身。** cgo 调用会把当前 G 标记为处于系统调用状态，P 可能被 sysmon 抢走；C 代码阻塞时 M 被占住。FFM 的 downcall 在 JIT 编译后开销很低，但 upcall 和 native 帧会让虚拟线程 pin 住载体（[Ch07](../ch07-scheduler/)）。

## ☕ Java 25 深度复习

### 类加载与热更新
- **`java.lang.instrument.Instrumentation`**（`java.instrument/java/lang/instrument/Instrumentation.java`）：
  - `redefineClasses` / `retransformClasses`：可以修改方法体、常量池和属性；**不能**增加、删除或重命名字段和方法，不能修改方法签名、修饰符和继承关系。
  - agent 入口：启动时 `-javaagent:x.jar`（`premain`），运行中通过 Attach API 动态加载（`agentmain`）。JEP 451 之后，动态加载 agent 默认会打印警告，可以用 `-XX:+EnableDynamicAgentLoading` 显式允许。
  - 已经在执行中的旧方法栈帧会继续执行旧代码，新的调用才会进入新代码。
- **类卸载**：一个类只有在其**类加载器**变得不可达时（加载器本身、它加载的所有类、这些类的所有实例都不可达）才可能在 GC 时被卸载。因此"真正的热替换"要用独立的 ClassLoader 加载模块，并确保旧加载器上没有残留引用（线程、ThreadLocal、静态注册表、JDBC 驱动等是常见的泄漏源）。
- **Class-File API（JEP 484，正式）**：`java.lang.classfile.ClassFile` 提供解析、转换和生成字节码的标准 API，可以替代 ASM 写 agent 或做代码生成。
- **Arthas**（第三方）：在线诊断和 `redefine`，本质上是基于 Attach + Instrumentation，受相同的限制。
- 游戏服的常见做法：脚本引擎（如 Lua / JavaScript）承载活动逻辑；数值表热加载；用独立 ClassLoader 做"逻辑模块热替换"（工程复杂度高）。

### FFM（JEP 454，正式）
- **核心类型**（`java.base/java/lang/foreign/`）：`Linker.nativeLinker()`、`SymbolLookup`（`defaultLookup()` / `libraryLookup`）、`FunctionDescriptor`、`MethodHandle downcallHandle(...)`、`MemorySegment upcallStub(...)`、`Arena`（`ofConfined` / `ofShared` / `ofAuto` / `global`）。
- **生命周期**：`MemorySegment` 绑定在 `Arena` 上，Arena 关闭后访问会抛异常，而不是读到野指针。这是相对 JNI 和 `Unsafe` 的根本改进。
- **jextract**：独立工具（不随 JDK 发布），从 C 头文件生成 FFM 绑定代码。
- **受限方法**：`Linker` 的 downcall 等属于受限操作，会打印警告；用 `--enable-native-access=ALL-UNNAMED`（或指定模块）显式授权。
- **与虚拟线程**：native 帧上的阻塞会 pin 载体；upcall 回到 Java 后在里面阻塞，同样会 pin。

## 🐹 Go 1.27 深入

### 规范层
- 规范里没有"动态加载"的概念：程序 = 编译期确定的包集合。`init()` 按依赖顺序执行一次。这是 Go 能做激进内联和全程序优化的前提，也是它没有类加载器式热更新的根本原因。
- `unsafe.Pointer` 的转换规则（规范和 `unsafe` 包文档）决定了 Go 指针能不能交给 C、在什么时候失效。

### 运行时层
- **数据热加载**：
  - 配置快照用 `atomic.Pointer[Config]`：读路径是一次原子加载，没有锁；写路径是"解析 → 校验 → 构建不可变快照 → `Store`"。旧快照由 GC 回收，正在使用旧快照的请求不受影响。
  - 校验失败就不替换，保留旧版本并告警；替换后保留上一版本，支持一键回滚。
  - 加载外部文件时用 `os.OpenRoot` / `os.Root`，把访问限制在指定目录内，防止 `../` 路径穿越（比如运营后台上传的数值表）。
- **`plugin` 包的真实约束**（来自 `plugin/plugin.go` 包文档）：仅支持 Linux、FreeBSD、macOS；不能关闭（不能卸载）；宿主和所有插件必须用**完全相同的工具链版本、构建标签和某些标志 / 环境变量**构建，公共依赖必须来自**完全相同的源码**；race detector 支持很差。结论：它只是"把一个程序拆成多个文件一起发布"，不是热更新机制。
- **进程级热重启**：
  - 新进程继承监听 socket 的 fd（`os/exec` 的 `ExtraFiles` + `net.FileListener`），或者用 `SO_REUSEPORT` 让新旧进程同时监听（Linux）。tableflip 是常用的第三方实现。
  - 旧进程停止 accept → 通知客户端重连或迁移 → 等待房间结束或把房间状态序列化后交给新进程 → 超时强制退出（[Ch10](../ch10-context-structured/)、[Ch18](../ch18-tcp-gateway/)）。
  - 有状态的房间服更常用的方案是**节点级滚动**：新节点上线后不再给旧节点分配新房间，旧节点等存量房间自然结束后下线（[Ch25](../ch25-capstone/)）。
- **脚本层**：gopher-lua（第三方，纯 Go 实现的 Lua 5.1 虚拟机）承载活动规则、任务条件等易变逻辑。代价是性能和类型安全，需要设计好 Go 与脚本之间的接口边界和沙箱（限制可调用的 API、执行步数或超时）。
- **cgo**：
  - 调用路径：`runtime.cgocall` 先 `entersyscall`，把当前 G 标记为系统调用状态，P 可以被 sysmon 抢走交给其他 M；C 函数返回后 `exitsyscall`。所以 cgo 调用的固定开销远大于普通函数调用，且 C 代码阻塞会占住一个 M（上限见 `debug.SetMaxThreads`）。
  - C 回调 Go：`//export` 导出函数，经过 `cgocallbackg` 回到 Go 调度器。
  - **指针传递规则**：Go 代码可以把 Go 指针传给 C，但它指向的内存里不能再包含 Go 指针（pinned 内存另有规则）；C 不能在调用返回后保存 Go 指针（goroutine 栈会移动，GC 不知道 C 持有它）。`GODEBUG=cgocheck=1`（默认）做运行时检查。
  - 要把 Go 值的"引用"交给 C 长期持有，用 `runtime/cgo.Handle`（`NewHandle` / `Value` / `Delete`）。
  - 代价：失去纯静态二进制和免费交叉编译（[Ch23](../ch23-build-observability/)），构建需要 C 工具链（本机 Windows 默认没有 gcc）。

### 源码阅读清单（`$GOROOT/src`）
| 文件 | 函数 / 类型 | 带着什么问题读 |
|---|---|---|
| `plugin/plugin.go` · `plugin/plugin_dlopen.go` | 包文档、`Open`、`Lookup` | 为什么要求宿主和插件"完全相同的工具链和依赖源码"？加载时如何校验包哈希？ |
| `runtime/cgocall.go` | `cgocall`、`cgocallbackg` | 一次 cgo 调用在 G、M、P 上依次发生了什么？为什么 C 回调 Go 比 Go 调 C 更贵？ |
| `runtime/cgo/handle.go` | `Handle`、`NewHandle`、`Value`、`Delete` | 为什么不能直接把 Go 指针交给 C 长期保存？Handle 用什么数据结构实现，忘记 `Delete` 会怎样？ |
| `os/root.go` | `OpenRoot`、`Root.Open`、`Root.FS` | Root 如何防止路径穿越和符号链接逃逸？和简单的 `filepath.Clean` 前缀检查相比强在哪里？ |
| `sync/atomic/type.go` | `Pointer[T]` 的 `Load` / `Store` / `CompareAndSwap` | 配置快照替换为什么不需要锁？读者看到的是否总是完整初始化的快照（内存模型依据）？ |

## 📊 Java 25 ↔ Go 1.27 机制级对照表

| 维度 | Java 25 | Go 1.27 | 取舍 / 游戏服影响 |
|---|---|---|---|
| 运行时加载代码 | ClassLoader 随时加载新类 | 无；`plugin` 受极强约束且不能卸载 | Java 可以做模块级热替换（代价是复杂度和泄漏风险）；Go 只能进程级替换 |
| 修改已加载代码 | `redefineClasses`：只能改方法体 | 不支持 | 线上紧急修 bug：Java 可以热修小改动；Go 必须发版 |
| 卸载 | 加载器不可达时随 GC 卸载 | 不适用 | Java 热替换要严防加载器泄漏 |
| 数据热加载 | `volatile` 引用 / `AtomicReference` 替换不可变快照 | `atomic.Pointer[T]` 替换不可变快照 | 两边做法一致，都是读无锁、写替换 |
| 脚本层 | GraalJS、LuaJ 等（第三方） | gopher-lua 等（第三方） | 易变的活动逻辑两边都应该放脚本层 |
| 原生调用 API | FFM（`Linker`、`MemorySegment`、`Arena`） | cgo（`import "C"`） | FFM 不需要 C 编译器；cgo 需要 C 工具链并且失去静态二进制 |
| 调用开销 | JIT 后 downcall 很低；upcall 较贵 | 固定开销明显（经过 `entersyscall` / `exitsyscall`） | 高频小函数（比如每帧调用几万次）两边都要批量化 |
| 内存安全 | `Arena` 关闭后访问抛异常 | 指针规则 + `cgocheck` 运行时检查 | Java 的生命周期模型更安全；Go 需要靠规约 |
| 阻塞的影响 | native 帧阻塞会 pin 虚拟线程载体 | C 阻塞占住 M，可能导致线程暴涨 | 阻塞型原生库都要隔离并限制并发 |

## 🔬 实验

### 实验 1：配置热加载的读路径开销（必做）
- **目的**：证明 `atomic.Pointer` 快照在高并发读下几乎零成本，并对比加锁方案。
- **方法**：`go/lab/hotconfig/`：100 个 goroutine 持续读配置（模拟每条消息都查数值表），后台每 100ms 替换一次快照。对比三种实现：`atomic.Pointer`、`sync.RWMutex`、`sync.Mutex`。用 `b.RunParallel` 加 `-cpu=1,4,8,16` 做 benchmark。
- **指标**：每次读的 ns/op 和随核数的扩展曲线；替换时读路径的尾延迟（用 `/sched/latencies:seconds` 交叉验证）。
- **预期现象**：`atomic.Pointer` 的读开销接近一次普通加载，并且随核数线性扩展；`RWMutex` 在核数多时因为读计数器的缓存行争用而退化。
- **Java 对照**：`AtomicReference` vs `ReentrantReadWriteLock`，用 JMH（[Ch15](../ch15-profiling-bench/)）。

### 实验 2：cgo vs FFM 的单次调用开销
- **目的**：给"能不能每帧调用原生库"一个数量级判断。
- **方法**：调用同一个极简 C 函数（`int add(int, int)`）：
  - Go：cgo 版本 vs 纯 Go 版本，`B.Loop` + benchstat。需要 gcc，在 WSL2 / Linux 上做。
  - Java：FFM downcall vs 纯 Java 方法，用 JMH，带 `--enable-native-access=ALL-UNNAMED`。
- **指标**：ns/call；批量版本（一次调用处理 1000 个元素）摊薄后的 ns/元素。
- **预期现象**：两边的跨边界调用都比普通调用贵一到两个数量级；批量化以后差距大幅缩小。写清楚你的结论：寻路、物理这类库应该怎样设计接口。

### 实验 3：进程级热重启（Linux / WSL2）
- **方法**：一个简单的 TCP 回显服务，实现"收到 SIGHUP → 启动子进程并通过 `ExtraFiles` 交出监听 fd → 父进程停止 accept、排空连接后退出"。用压测客户端持续建连发消息。
- **指标**：重启过程中失败的连接数和消息数（目标为零）、新进程开始 accept 的耗时。
- **预期现象**：已建立的连接由老进程处理完毕，新连接由新进程接收，客户端感知不到中断。

## 🧪 练习规划

**Go：`go/exercises`**（`go test ./ch24-runtime-boundaries/go/...`）
1. `type Store[T any] struct{...}`，方法 `Load() *T`、`Update(raw []byte, parse func([]byte) (*T, error), validate func(*T) error) error`、`Rollback() error`：原子快照、校验失败不替换、保留一个历史版本用于回滚。测试覆盖并发读写（`-race`，在 WSL2 跑）、校验失败、回滚。
2. `func LoadTables(root *os.Root, names []string) (map[string][]byte, error)`：只允许读取 Root 目录下的数值表。测试 `../etc/passwd`、绝对路径、符号链接逃逸都被拒绝（用 `t.TempDir()` 构造）。
3. `type Drainer struct{...}`，方法 `Track(ctx) (done func(), ok bool)`、`Drain(ctx) error`：开始排空后拒绝新任务，等存量任务结束或超时。用 `testing/synctest` 测试超时路径。
4. `func HandoffListener(l *net.TCPListener) (*os.File, error)` + `func InheritListener(f *os.File) (net.Listener, error)`：fd 交接的两半。测试在同一进程里模拟交接：交接后旧 listener 关闭，新 listener 仍然能 accept。

**Java：`java/drills`**
1. `RedefineLimitsDrill.java`：用 Class-File API（JEP 484）生成同一个类的两个版本（一个只改方法体，一个增加字段），通过 `Instrumentation` 尝试重定义，自校验前者成功、后者抛 `UnsupportedOperationException`。实现方式：drill 在临时目录用 `jar` 工具打一个带 `Premain-Class` 和 `Can-Redefine-Classes: true` 的 agent jar，再用 `ProcessBuilder` 带 `-javaagent` 启动子 JVM 执行重定义。
2. `FfmDowncallDrill.java`：用 FFM 调用 C 标准库函数（如 `strlen`），在 confined `Arena` 里分配字符串；自校验结果正确，以及 Arena 关闭后访问该段会抛异常。需要 `--enable-native-access=ALL-UNNAMED`。

## 🎮 游戏服落地

- **热更分层**：
  - 数值表 / 配置：原子快照热加载（两边一致），版本号写进日志和指标。
  - 活动 / 任务规则：脚本层（Lua 等），沙箱 + 超时，脚本版本和数据版本一起管理。
  - 代码 bug：Java 可以用 `redefineClasses` 热修方法体内的小改动（要有流程审批）；Go 走快速发版 + 进程级替换。
  - 大版本：节点级滚动，新节点只接新房间，旧节点自然排空。
- **玩家无感重启**：网关用 fd 交接或 `SO_REUSEPORT` 实现无损重启；逻辑服房间跨进程迁移需要可序列化的房间状态（[Ch19](../ch19-serialization/)），否则就只能等房间结束。
- **原生库**：寻路（Recast/Detour）、物理引擎这类 C/C++ 库，Go 用 cgo、Java 用 FFM。接口按"一帧一次批量调用"设计，不要"每个实体调用一次"；阻塞调用放到专用、有并发上限的 worker。
- **安全**：运营后台上传的数值表用 `os.Root` 限定目录；脚本层禁止访问文件和网络。

## ✅ 验收答辩题

- [ ] [L2] `Instrumentation.redefineClasses` 能改什么、不能改什么？已经在执行的旧方法会怎样？
- [ ] [L2] 一个类在什么条件下才会被卸载？为什么 ThreadLocal 和静态注册表是热替换的常见泄漏源？
- [ ] [L2] Go 的 `plugin` 包有哪些硬约束？为什么说它不是热更新机制？
- [ ] [L2] 一次 cgo 调用在运行时里经历了什么？为什么 C 代码不能保存 Go 指针？`cgo.Handle` 解决了什么问题？
- [ ] [L2] FFM 的 `Arena` 生命周期模型相比 JNI / `Unsafe` 好在哪里？
- [ ] [L3] 用实验 1 的数据解释：为什么配置快照用 `atomic.Pointer` 而不是 `RWMutex`？在什么情况下 RWMutex 反而更合适？
- [ ] [L3] 设计并演示一次网关无损重启：fd 如何交接，已有连接如何排空，如何验证零失败？
- [ ] [L3] 根据实验 2 的数据，给一个"每帧需要对 5000 个实体调用寻路库"的需求设计调用方式。
- [ ] [L4] 为一款 MMO 设计完整的热更新体系：哪些内容热更、用什么机制、如何灰度和回滚，Java 方案和 Go 方案分别怎么做。
- [ ] [L4] 团队想把核心战斗逻辑从 Go 挪到 Lua 脚本里以便热更，你怎么评估性能、正确性和运维风险？

## 🕳️ 常见坑

- **指望 `plugin` 做热更新**：工具链和依赖必须完全一致、不能卸载、不支持 Windows，等于换一种方式发版。
- **配置热加载直接修改共享 map**：读者看到半更新的状态，或者触发 `concurrent map writes` 崩溃。应该构建新的不可变快照再整体替换。
- **热加载不校验就替换**：一张坏表让整服崩溃。必须"先校验后替换"，并保留回滚版本。
- **Java 热替换后旧 ClassLoader 泄漏**：线程、ThreadLocal、静态缓存、JDBC 驱动注册表持有旧类，元空间持续增长。
- **以为 `redefineClasses` 能加字段**：结构性修改直接抛异常；Arthas 也绕不过。
- **cgo 里做阻塞调用不限并发**：M 数量暴涨，撞上线程上限后进程崩溃。
- **C 代码保存 Go 指针**：goroutine 栈会移动、GC 不知道这个引用，结果是随机的内存错误。用 `cgo.Handle`。
- **每个实体调用一次原生函数**：跨边界的固定开销被放大几千倍，应该批量化。

## 📚 一手资料

- Go：`go doc plugin`（包文档里的 Warnings 一节）、[cgo 文档](https://pkg.go.dev/cmd/cgo)（指针传递规则）、`go doc runtime/cgo.Handle`、`go doc os.Root`、[The Go Memory Model](https://go.dev/ref/mem)
- Java：`java.lang.instrument` 包文档、[JEP 451](https://openjdk.org/jeps/451)、[JEP 454 FFM](https://openjdk.org/jeps/454)、[JEP 484 Class-File API](https://openjdk.org/jeps/484)、jextract 项目文档
- 第三方：tableflip、gopher-lua、Arthas 文档
