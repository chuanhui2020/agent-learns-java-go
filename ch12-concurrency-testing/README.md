# Ch12 · 并发正确性工程

> Part 2 · 估时 12h · 状态：⚪ 未开始 · 用 race detector、goroutineleak profile、synctest 三件套，把"偶发并发 bug"变成"可复现、可回归的测试失败"

## 🎯 目标（资深标准）

- **L1**：所有并发代码过 `go test -race`；会用 `testing/synctest` 写确定性的时间相关测试；会抓 goroutine dump 和 `goroutineleak` profile。
- **L2**：讲清数据竞争在 Go 内存模型里的定义；讲清 race detector 的原理（编译期插桩 + ThreadSanitizer 运行时 + happens-before 追踪）以及它**只能发现实际执行到的竞争**；讲清 goroutineleak profile 如何借助 GC 可达性判定"永远无法被唤醒"，以及它的漏报边界。
- **L3**：给定一个线上偶发问题（数据错乱、内存缓慢上涨、偶发卡死），能选对工具、构造复现、写出回归测试；能把一个依赖真实时间、要跑 30 秒的测试改成毫秒级的确定性测试。
- **L4**：为团队制定并发正确性的工程规范：CI 里的 `-race` 策略、泄漏检测接入、并发代码 review 清单；并说明 Java 侧（jcstress、JFR）对应的规范怎么定。

## 🔗 前置

[Ch07](../ch07-scheduler/) · [Ch08](../ch08-channels-select/) · [Ch09](../ch09-sync-memory-model/)（内存模型是本章的理论基础）· [Ch10](../ch10-context-structured/) · [Ch11](../ch11-actor/)

## 💡 关键洞察

1. **Go 有官方 race detector，Java 没有。** 这是 Go 并发工程的最大红利。代价是它只是动态检测：没被测试执行到的路径、没在测试里实际交错发生的竞争，它都看不见。所以**测试的并发覆盖率决定了 race detector 的价值**。
2. **goroutine 泄漏是 Go 服务"内存缓慢上涨"的头号原因。** Go 没有"线程池满了就拒绝"这层天然边界，泄漏的 goroutine 会一直攥着它的栈和它引用的对象。`goroutineleak` profile（**1.27 新增，转正**）第一次让"哪些 goroutine 永远醒不过来"可以被机器判定，而不是靠人读 dump 猜。
3. **时间是并发测试的头号不确定来源。** `testing/synctest` 把时间变成气泡内的假时钟，并且能精确判断"所有 goroutine 都已经停稳"。`time.Sleep(100*time.Millisecond)` 式的测试应该从代码库里消失。

## ☕ Java 25 深度复习

### JMM 下的"竞争"与验证手段
- JMM 的数据竞争定义：两个线程访问同一变量，至少一个是写，且两者之间没有 happens-before 关系。**正确同步的程序表现为顺序一致（DRF-SC）**，这一点和 Go 内存模型一致（[Ch09](../ch09-sync-memory-model/)）。
- Java **没有官方的 race detector**。实践中靠三样：(1) 代码 review + 不变量设计；(2) **jcstress**（OpenJDK Code Tools 项目，第三方构建依赖）：用 `@JCStressTest`、`@Actor`、`@Arbiter`、`@State`、`@Outcome(expect = ACCEPTABLE / FORBIDDEN / ACCEPTABLE_INTERESTING)` 穷举并发交错，验证内存模型层面的结果集；(3) 压测 + 断言。
- jcstress 和 race detector 是两类工具：前者验证"这段同步原语的所有可能结果是否都可接受"（适合写并发数据结构），后者发现"业务代码里哪里忘了同步"（适合大规模业务代码）。

### 死锁、泄漏与线程状态的观测
- **线程转储**：`jcmd <pid> Thread.print` 覆盖平台线程；虚拟线程要用 `jcmd <pid> Thread.dump_to_file -format=json <file>`（或纯文本格式），带上 STS 的作用域树（[Ch10](../ch10-context-structured/)）。
- **`ThreadMXBean.findDeadlockedThreads()`**：能发现 monitor 和 `ownable synchronizer`（如 `ReentrantLock`）的循环等待。它的管理接口面向平台线程，虚拟线程不在其主要覆盖范围内（以 `java/lang/management/ThreadMXBean.java` 的 Javadoc 为准）。
- **JFR 虚拟线程事件**（本机 `jfr metadata` 已核实）：`jdk.VirtualThreadStart`、`jdk.VirtualThreadEnd`、`jdk.VirtualThreadPinned`、`jdk.VirtualThreadSubmitFailed`。"虚拟线程泄漏"在 Java 里的表现是 Start 远多于 End，并且转储里有大量线程停在同一个 park 点。和 Go 一样，Java 也没有"判定这个虚拟线程永远醒不过来"的机制。
- 源码：`java.management/java/lang/management/ThreadMXBean.java`、`java/lang/VirtualThread.java`（状态机，用于读懂转储）。

## 🐹 Go 1.27 深入

### 规范层（The Go Memory Model）
- **数据竞争**：对同一内存位置的读写或写写，至少一个不是同步操作（atomic 等），而且两者之间没有 happens-before 关系。
- **有竞争的程序在 Go 里不是"未定义行为一切皆有可能"**：内存模型限制了结果（比如单字长读写不会产生凭空值）。但对于多字长值（interface、string、slice），竞争可能读到**撕裂的值**，进而导致内存不安全、崩溃。这就是为什么"偶发崩在 runtime 里"的问题经常源于数据竞争。
- **map 并发读写**：运行时尽力检测，直接 `fatal error: concurrent map writes` / `concurrent map read and map write`（`internal/runtime/maps/map.go`）。它不是 panic，`recover` 不住，进程直接退出。

### 运行时与工具实现层
- **race detector**：
  - `-race` 让编译器在内存访问和函数进出处插入 `raceread` / `racewrite` / `racefuncenter` 等调用（`runtime/race.go`）。
  - 运行时链接 ThreadSanitizer，为每个 goroutine 维护向量时钟。channel、mutex、`sync/atomic`、`WaitGroup` 等同步操作会被告知运行时，用来建立 happens-before 边。
  - 官方文档给出的开销量级：内存约 5–10 倍，执行时间约 2–20 倍。所以线上不常开，CI 和压测环境要开。
  - **平台提醒**：`-race` 需要 cgo。本机（Windows）目前 `CGO_ENABLED=0` 且 PATH 里没有 gcc，直接跑会报 `-race requires cgo`。两个办法：装 MinGW-w64 这类 gcc 工具链并开启 cgo；或者在 WSL2 / Linux 上跑 `-race`。**推荐后者**，Part 3、4 的实验本来就要在 Linux 上做。
- **goroutineleak profile（1.27 新增，转正）**：
  - 取用：`pprof.Lookup("goroutineleak").WriteTo(w, debug)`，HTTP 端点 `/debug/pprof/goroutineleak`。
  - 原理：取 profile 时先调用 `goroutineLeakGC`（`runtime/mgc.go`），置位 `work.goroutineLeak.pending`，触发一次带泄漏检测的 GC。GC 标记阶段只从**可运行的 goroutine**出发做可达性分析：一个阻塞在某个原语上的 goroutine，如果该原语既不能从任何可运行 goroutine 到达，也不能从"它们可能唤醒的 goroutine"到达，就判定为泄漏。
  - 漏报边界：原语如果能经由**全局变量**或**可运行 goroutine 的局部变量**到达，就不会被判为泄漏（比如一个永远没人读的全局 channel）。
  - 平时没有开销，只在取 profile 时多跑一轮 GC。
- **`testing/synctest`**：
  - `synctest.Test(t, f)` 创建一个"气泡"，气泡内的 `time` 使用假时钟（起点固定为 2000-01-01 00:00 UTC）。
  - **持久阻塞（durably blocked）**：阻塞在气泡内创建的 channel 上、所有分支都是气泡内 channel 的 select、`sync.Cond.Wait`、在气泡内 `Add` 过的 `WaitGroup.Wait`、`time.Sleep`。**以下不算**：`sync.Mutex` / `RWMutex` 加锁、网络 I/O、系统调用。
  - 所有 goroutine 都持久阻塞时：有人调了 `Wait` 就返回；否则时间跳到下一个能唤醒某个 goroutine 的时刻；都不满足就判定死锁，`Test` panic。
  - `synctest.Sleep(d)`（**1.27 新增**）等价于 `time.Sleep(d)` 后紧跟 `synctest.Wait()`，解决"测试和被测代码同时睡同样长时间，谁先醒不确定"的问题。
  - 网络代码怎么办：`net.Pipe` 是气泡内可用的内存连接；HTTP 用 `httptest.NewTestServer`（**1.27 新增**，内存假网络，[Ch20](../ch20-http-rpc/)）。
- **runtime 的死锁检测**：`checkdead`（`runtime/proc.go`）只在**所有** goroutine 都阻塞、而且没有任何可能唤醒它们的东西时报 `all goroutines are asleep - deadlock!`。线上服务总有监听 socket、timer、信号处理在等待，所以局部死锁永远不会触发它，要靠 goroutineleak profile 和 dump 分析。
- **vet 相关分析器**：
  - `copylocks`：值拷贝了 `sync.Mutex` 等。
  - `waitgroup`：`WaitGroup.Add` 放在新起的 goroutine 内部。
  - `testinggoroutine`：在测试起的 goroutine 里调用 `t.Fatal`。
  - `lostcancel`：忘记调用 cancel。
  - `loopclosure`：闭包捕获循环变量。循环变量按迭代作用域之后，它主要和老代码相关。
  - 另外，`go fix` 的现代化器 `waitgroupgo`（**1.27 新增**，由 `waitgroup` 改名而来）会把 `wg.Add(1); go func(){ defer wg.Done() ... }()` 改写成 `wg.Go(...)`，从根上消除 Add 位置错误这一类 bug。

### 源码阅读清单
| 文件 | 函数 / 类型 | 带着什么问题读 |
|---|---|---|
| `runtime/race.go` | `raceread`、`racewrite`、`racefuncenter`、`raceacquire` 系列 | 哪些操作会向 TSan 报告"同步"？`sync/atomic` 是怎么被识别成同步操作的？ |
| `runtime/mgc.go` | `goroutineLeakGC`、`work.goroutineLeak` | 泄漏检测为什么要搭在一次完整 GC 上？`pending` 标志为什么要"自旋直到被清除"？ |
| `runtime/mprof.go` | `goroutineLeakProfileWithLabels` | 泄漏 goroutine 的栈和 pprof labels 是怎么收集出来的？ |
| `runtime/pprof/pprof.go` | `goroutineleak` profile 注册、`goroutineLeakProfile` | `debug >= 2` 时为什么退化成普通 goroutine dump？ |
| `testing/synctest/synctest.go` | `Test`、`Wait`、`Sleep` 的文档与实现 | "持久阻塞"的边界在哪里？为什么 Mutex 阻塞不算？包级 `var wg sync.WaitGroup` 有什么限制？ |
| `runtime/proc.go` | `checkdead` | 什么条件下才会报全局死锁？为什么有一个活着的 timer 就不报？ |
| `internal/runtime/maps/map.go` | `fatal("concurrent map writes")` 附近 | 并发写检测靠什么标志位？为什么它只是"尽力而为"，会漏报？ |

## 📊 Java 25 ↔ Go 1.27 机制级对照表

| 维度 | Java 25 | Go 1.27 | 取舍 / 游戏服影响 |
|---|---|---|---|
| 数据竞争检测 | 无官方工具；jcstress 验证原语级结果集 | `-race`：编译插桩 + TSan，业务代码级 | Go 可以在 CI 上对全部业务测试开 `-race`，并发 bug 的发现成本低一个数量级 |
| 竞争的后果 | JMM 保证类型安全：引用读写原子，不会读到"半个对象引用" | 多字长值（interface / slice / string）可能撕裂，进而导致崩溃 | Go 里"偶发 runtime 崩溃"要首先怀疑数据竞争 |
| 集合并发误用 | `HashMap` 并发写导致数据损坏或死循环，静默 | map 并发读写直接 `fatal error`，进程退出 | Go 失败得响亮，但线上就是宕机；必须靠 -race 和 actor 化提前消灭 |
| 泄漏判定 | 无自动判定，看 JFR Start/End 差值和转储 | `goroutineleak` profile 基于 GC 可达性自动判定（有漏报边界） | Go 可以把泄漏检查做进测试和线上巡检 |
| 局部死锁 | `ThreadMXBean.findDeadlockedThreads`（平台线程、锁层面） | 无锁级死锁检测；`checkdead` 只管全局死锁 | Go 的 channel 死锁表现为泄漏，用 goroutineleak / dump 分析 |
| 时间相关测试 | 注入 `Clock`、手写假调度器 | `synctest` 气泡：假时钟 + 持久阻塞判定，标准库支持 | Go 写确定性的超时 / 重试 / tick 测试成本很低 |
| 转储信息 | JSON 转储带 STS 作用域树 | 扁平 goroutine 列表 + 等待原因 + 等待时长 + pprof labels | Go 侧要用 labels（房间 id、玩家 id）补足归属信息 |
| 静态检查 | Error Prone 等第三方 | `go vet`（copylocks / waitgroup / lostcancel / testinggoroutine），`go test` 默认跑一个子集 | Go 的静态并发检查开箱即用 |

## 🔬 实验

### 实验 1：三类泄漏，用 goroutineleak profile 抓
- **目的**：建立"泄漏模式 → profile 表现"的直觉，并亲手验证漏报边界。
- **方法**（`go/lab/leaks`）：
  1. **提前 return**：N 个 worker 向无缓冲 channel 发结果，收集方遇到第一个错误就 return，剩下的发送方永久阻塞（就是 Go 1.26 发布说明里的那个例子）。
  2. **忘记 cancel / 无人关闭**：后台 goroutine `for range ch`，生产方退出时没有 close。
  3. **互相等待**：两个 actor 互相 ask，并且都不带超时。
  4. **对照组**：把第 1 类里的 channel 存进一个全局变量，验证此时 profile 抓不到。
- **工具**：`pprof.Lookup("goroutineleak").WriteTo(os.Stdout, 1)`、`go tool pprof`、`/debug/pprof/goroutineleak`。
- **预期现象**：前三类都能被报告，栈指向阻塞点；对照组漏报，和文档描述一致。
- **记录**：每类的 profile 输出片段、取一次 profile 的耗时（它会触发一轮 GC）。

### 实验 2：synctest 改造依赖时间的测试
- **方法**：先写一个"真实时间"版本的测试，覆盖带超时重试的 RPC 客户端（3 次重试、指数退避、总超时 10s）和 20Hz 房间 tick（[Ch11](../ch11-actor/)）。再用 `synctest.Test` + `synctest.Sleep` 重写。
- **预期现象**：运行时间从秒级降到毫秒级；连续跑 1000 次（`-count=1000`）结果完全一致；把被测代码里的 `sync.Mutex` 换成 channel 前后，观察"持久阻塞"判定的差异。
- **记录**：两版测试的耗时、稳定性（失败次数）。

### 实验 3：race detector 的覆盖边界（Linux / WSL2）
- **方法**：构造一个只在特定交错下才会发生的竞争（比如只有当某 goroutine 恰好在第二次 tick 之前完成时才会并发访问）。分别用普通单测、`-race -count=100`、`-race` 加压测并发（`t.Parallel` + 多 goroutine 放大）来跑。
- **预期现象**：普通单测和低并发的 `-race` 可能抓不到，提高并发与次数后才能稳定抓到。这说明 `-race` 的效果取决于测试本身制造交错的能力。
- **记录**：各方案的检出率、运行耗时与内存放大倍数。

## 🧪 练习规划

**Go：`go/exercises`**（全部 `go test -race`，时间相关一律用 `synctest`）
1. `func CollectAll[T any](ctx context.Context, tasks []func(context.Context) (T, error)) ([]T, error)`：修复给定的"提前 return 泄漏"实现。测试里用 `goroutineleak` profile 断言泄漏数为 0（写一个辅助函数 `assertNoLeaks(t)`）。
2. `type Retrier struct{...}` + `func (r *Retrier) Do(ctx context.Context, fn func(context.Context) error) error`：指数退避加抖动、最大次数、总超时。用 `synctest` 精确断言每次重试发生的虚拟时间点。
3. `type Counter struct{...}`：给定一个有数据竞争的"在线人数统计"实现，先写一个能让 `-race` 稳定报警的测试，再分别用 `atomic.Int64` 和 actor 修复，并比较两者的 benchmark。
4. `func DetectDeadlock(dump []byte) []Cycle`（挑战）：解析 `debug=2` 格式的 goroutine dump，找出"互相 ask"的等待环（根据练习约定的 pprof label 或等待原因）。

**Java：`java/drills`**
1. `JmmDrill.java`：用若干线程和 `volatile` / `VarHandle` 的不同访问模式重现"可见性丢失"和"重排序"（多跑若干轮统计结果分布）。自校验只检查**禁止结果从未出现**，并在注释里写出每个结果对应的 happens-before 推理。完整的 jcstress 版本放到 Ch15 的 JMH 工程里一起做。
2. `LeakDrill.java`：制造一批永远阻塞的虚拟线程，用 JFR（`jdk.VirtualThreadStart` / `End`）和 JSON 线程转储定位它们，对照 Go 的 goroutineleak profile 体验差异。

## 🎮 游戏服落地

- **CI 规范**：所有包 `go test -race -count=3`（在 Linux runner 上）；并发敏感的包另外跑 `-count=50` 的夜间任务；每个服务的集成测试结束时调用 `assertNoLeaks`。Java 服务对应：核心并发组件写 jcstress 测试，集成测试检查 JFR 里虚拟线程的 Start/End 平衡。
- **线上巡检**：网关和房间服暴露 `/debug/pprof/goroutineleak`（只对内网开放，[Ch23](../ch23-build-observability/)）。巡检脚本每天抓一次，泄漏数非零就告警。goroutine 带上房间 id / 玩家 id 的 pprof labels，泄漏报告能直接定位到业务实体。
- **房间逻辑的确定性测试**：战斗、技能 CD、超时踢人这类逻辑全部用 `synctest` 写测试，模拟几个小时的对局只需几毫秒。Java 侧通常要先抽象出 `Clock` 和调度器接口才能做到。
- **"偶发数据错乱"排查剧本**：先在压测环境开 `-race` 复现；复现不了就加大并发、开启 `GODEBUG` 相关调试项；还不行就审查所有跨 goroutine 共享的可变状态，确认它们都有唯一的所有者。

## ✅ 验收答辩题

- [ ] [L2] 按 Go 内存模型给出数据竞争的定义。为什么对 `interface` 变量的竞争可能导致进程崩溃，而对 `int64` 的竞争（在 64 位平台）通常"只是"读到旧值？
- [ ] [L2] race detector 是怎么知道两个访问之间"有 happens-before"的？`sync/atomic` 操作在其中扮演什么角色？
- [ ] [L2] goroutineleak profile 的判定原理是什么？举一个它一定会漏报的例子。
- [ ] [L2] `synctest` 里哪些阻塞算"持久阻塞"？为什么 `sync.Mutex` 不算？这对被测代码的写法有什么要求？
- [ ] [L2] 为什么线上服务里两个 goroutine 互相等待，永远不会触发 `all goroutines are asleep - deadlock!`？
- [ ] [L3] 一个 Go 服务 RSS 每天上涨 200MB，GC 后不回落。给出排查步骤：先看哪些指标，再抓哪些 profile，怎么区分"goroutine 泄漏"和"缓存无限增长"。
- [ ] [L3] CI 里 `-race` 一直是绿的，线上却出现了数据竞争导致的崩溃。可能的原因有哪些？怎么改进测试，让 `-race` 能抓到它？
- [ ] [L3] 把一个依赖真实时间的重试测试改写成 `synctest` 版本时，哪些被测代码的写法会让 `Wait` 永远等不到"全部停稳"？
- [ ] [L3] 对比 jcstress 和 race detector：分别适合验证什么样的代码？如果要验证一个自研无锁 MPSC 队列，两边各怎么测？
- [ ] [L4] 为一个 30 人的 Go + Java 混合游戏服团队写一页"并发正确性规范"：工具、CI 门禁、线上巡检、review 清单。
- [ ] [L4] 本机是 Windows，`-race` 需要 cgo。你会如何规划团队的本地开发与 CI 环境，保证每个人提交前都跑过 `-race`？

## 🕳️ 常见坑

- **以为 `-race` 绿了就没有竞争**：它只报告执行到并且实际交错的访问。
- **只在线下开 `-race`，压测不开**：很多竞争只在高并发时才发生，压测环境恰恰最值得开（注意 5–10 倍内存开销）。
- **用 `time.Sleep` 等待 goroutine "跑完"**：既慢又不稳定，应该用 `synctest.Wait` 或显式同步。
- **把 `fatal error: concurrent map writes` 当 panic 去 recover**：它是 fatal，进程一定退出。
- **在测试起的 goroutine 里调 `t.Fatal`**：只会让该 goroutine 退出，测试本身继续跑（`testinggoroutine` 会报）。
- **Java 思维：用 `ConcurrentHashMap` 心态写 Go map**：Go 的内建 map 不是并发安全的，要用 actor 化、`sync.Map`（[Ch09](../ch09-sync-memory-model/)）或加锁。
- **泄漏检测只靠 `runtime.NumGoroutine()`**：数量会随负载正常波动，要用 goroutineleak profile 判定"永远醒不过来的那部分"。
- **在 Windows 上直接 `go test -race`**：没有 cgo 工具链时会直接失败。要么配好 gcc，要么用 WSL2。

## 📚 一手资料

- Go：[The Go Memory Model](https://go.dev/ref/mem)、[Data Race Detector](https://go.dev/doc/articles/race_detector)、[Go 1.27 Release Notes](https://go.dev/doc/go1.27)（goroutine leak profile 一节）、`testing/synctest` 包文档、`runtime/race.go`、`runtime/mgc.go`、`runtime/pprof/pprof.go` 源码
- Java：[JLS 第 17 章（Threads and Locks）](https://docs.oracle.com/javase/specs/jls/se25/html/jls-17.html)、OpenJDK jcstress 项目（openjdk/jcstress）及其 samples、`ThreadMXBean` Javadoc、JFR 事件参考（`jfr metadata`）
- 论文：Serebryany & Iskhodzhanov, *ThreadSanitizer: data race detection in practice*（2009）
