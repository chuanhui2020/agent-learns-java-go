# 学习方法论（资深标准）

> 目标读者：已经能独立扛 Java 服务端（Netty / 虚拟线程 / JVM 调优）的资深工程师。
> 这里不教"语法怎么写"，也不教"项目怎么搭"，而是把 **Go 1.27** 与 **Java 25** 这两门语言及其运行时学到**机制级**：规范怎么规定、编译器和运行时怎么实现、设计者为什么这样取舍。

---

## 〇、范围：只学语言与运行时，不学工程实践

| 在范围内 | 不在范围内 |
|---|---|
| **语言规范**：类型系统与常量、值语义、slice/string/map、函数与闭包、接口、泛型、迭代器、错误/panic/defer、反射与 unsafe | 网关、HTTP/gRPC 服务、序列化协议设计 |
| **并发模型**：调度器、channel/select、同步原语、内存模型、取消与结构化并发、竞态与泄漏检测原理 | 数据库 / 缓存集成、连接池调参 |
| **运行时**：分配器、GC、netpoller、系统调用、cgo 与 FFM | 模块与依赖管理实践、CI、构建与容器部署 |
| **编译器**：逃逸分析、内联、去虚拟化、BCE、PGO、SIMD intrinsic；JVM 侧的 JIT、AOT 缓存 | 日志与可观测性体系、热更新方案、服务治理 |
| **测量手段**：benchmark、pprof、trace、JFR、JMH 的**工作原理**（它们是验证上面这些机制的工具） | 完整项目实战 |

**标准库只在它暴露了语言或运行时机制时才讲**：`sync`、`sync/atomic`、`context`、`iter`、`reflect`、`unsafe`、`runtime`、`weak`、`unique`、`testing`（基准与 synctest）属于这一类；`net/http`、`database/sql`、`encoding/*` 这类"用法型"API 不展开（`encoding/json/v2` 只作为反射的案例出现在 Ch09）。

例子里可以出现 `Player`、`Entity`、`Room` 这样的名字，只是为了让你有熟悉的抓手，**不讨论业务架构**。

---

## 一、心态转变 10 条（每写一段 Go 回来对一遍）

1. **错误是值，不是控制流。** 没有异常栈展开；`if err != nil` 是显式的控制流图。`panic` 只留给"程序已经不变量破坏"的场景。
2. **没有继承，只有组合。** 嵌入（embedding）是**字段提升 + 方法提升**，不是 is-a；不存在 override 与 `super`。
3. **接口是结构化、隐式满足的，而且应由使用方定义。** "Accept interfaces, return structs"。大接口是 Java 味。
4. **值语义是默认。** 赋值、传参、`range` 都在**拷贝**；你要决定的是"拷贝什么"（值 vs 指针 vs slice header）。这直接决定逃逸与 GC 压力。
5. **零值可用是一等设计。** `sync.Mutex{}`、`bytes.Buffer{}`、`strings.Builder{}` 开箱即用；构造函数是可选项，不是必需品。
6. **没有构造重载 / 默认参数 / 注解 / AOP。** 只有函数、方法和组合。少一层"魔法"，读任何人的代码都像读自己的。
7. **并发是语言内核，不是库。** goroutine、channel、select、调度器、netpoller、race detector 是一个整体设计；不要用"线程池 + 锁"的思路去套。
8. **运行时就在你手边。** `$GOROOT/src/runtime` 是可读的 Go 代码（不像 HotSpot 是 C++）。遇到"为什么"，先读源码，再读博客。
9. **没有数据的结论只算假说。** "这里会逃逸""这个更快""这里会被内联"都要能用编译器输出或 benchmark 证明。
10. **简单是刻意的，但不是简陋的。** Go 把复杂度从语言挪到了运行时和工具链；资深的价值在于知道它挪到了哪里。

---

## 二、双轨结构：每章都是"复习 Java 25 + 深入 Go 1.27"

每个主题按五段展开，前两段**同等分量**：

> **☕ Java 25 深度复习 → 🐹 Go 1.27 深入 → 📊 机制级对照 → 🔬 实验出数据 → 🧭 设计取舍**

- **☕ Java 25 深度复习**：不是"Java 里怎么写"，而是"JLS/JVMS 怎么规定、JDK 25 和 HotSpot 怎么实现"——对应的 JEP、JDK 类库源码、字节码形态、JIT 行为。你本来就会用，这里要补到能讲实现。
- **🐹 Go 1.27 深入**：分三层——**规范层**（spec 原文怎么规定，标出章节名）→ **运行时/编译器层**（runtime、cmd/compile 怎么实现）→ **源码阅读**（带着问题读指定文件和函数）。
- **📊 机制级对照**：对照表的列是"机制"，不是"语法"。比如不写"Java 用 `synchronized`、Go 用 `Mutex`"，而写"可重入性、自旋策略、饥饿处理、与调度器的交互、是否 pin 载体线程"。
- **🔬 实验**：每章至少 2 个**可复现、出数据**的实验。实验的目的是验证机制，而不是调一个业务系统。
- **🧭 设计取舍**：回答"为什么这样设计"——设计者面对的约束、被否决的替代方案（Go 提案与设计文档、JEP 的 Alternatives 一节）、这个决定带来的后果，以及 Java 为什么走了另一条路。只讨论语言和运行时的设计，不落到业务架构。

---

## 三、能力分级 L1–L4（过章标准：L3）

| 级别 | 含义 | 证据 |
|---|---|---|
| **L1 会用** | 能写出正确、地道的代码，能准确说出规范规定的行为 | 练习测试全绿、过 `-race`、`go vet` 干净 |
| **L2 懂实现** | 能讲清编译器 / 运行时 / JVM 怎么实现这条语义 | 源码阅读笔记；能画出关键数据结构与状态机 |
| **L3 能验证** | 能在运行前**预测**程序的输出、分配次数、逃逸结论、调度行为，并设计实验证实或证伪；预测错了能用机制解释偏差 | 实验报告（benchstat / pprof / trace / JFR / `-gcflags=-m` / `javap` 输出） |
| **L4 能论设计** | 能讲清设计动机与替代方案：为什么 Go / Java 这样设计，换一种设计会怎样 | 一页设计评述：问题、约束、方案、被否决的替代、代价（引用提案 / JEP / 设计文档原文） |

每章「✅ 验收」里的答辩题都标了级别。**L3 全过才算本章完成**；L4 题是加分项。

---

## 四、每章 README 模板（带教时按此扩写）

```
# ChNN · 标题
> Part · 估时 · 状态 · 一句话定位

## 🎯 目标（按 L1–L4 写，可验证）
## 🔗 前置
## 💡 关键洞察（1–3 条最该记住的判断）
## ☕ Java 25 深度复习（JLS/JVMS 条款 + JEP + JDK/HotSpot 源码位置）
## 🐹 Go 1.27 深入（规范层 / 运行时与编译器层 / 源码阅读清单）
## 📊 Java 25 ↔ Go 1.27 机制级对照表（最后一列：取舍 / 设计动机）
## 🔬 实验（目的 · 方法 · 工具 · 预期现象 · 要记录的指标）
## 🧪 练习规划（go/exercises · java/drills）
## 🧭 设计取舍（为什么这样设计、替代方案、代价）
## ✅ 验收答辩题（标注 L2/L3/L4）
## 🕳️ 常见坑（Java 老手专属）
## 📚 一手资料（spec / 源码 / JEP / 设计文档优先）
```

---

## 五、源码阅读方法

**Go 1.27**
- 规范是第一手资料：每章「规范层」都标了 spec 的章节名，**读原文**，不要只读二手总结。并发章节同时读 The Go Memory Model。
- 源码就在本机：`go env GOROOT` → `src/`。用 `go doc -src runtime.gopark` 直接看函数源码。
- 先读 `src/runtime/HACKING.md`（runtime 编码约定：`g`/`m`/`p`、`systemstack`、写屏障限制）。
- **带着问题读**：每章源码清单都列了"读哪个文件、哪几个函数、回答什么问题"。不要从头读到尾。
- 笔记写到 `chNN-xxx/notes/source-<主题>.md`：画数据结构图 + 状态机 + 关键路径伪代码，标注文件:行号。

**Java 25**
- 规范：JLS / JVMS（Java SE 25 版），语言章节标出条款号（如 JLS §5.1.2 拓宽原始类型转换）。
- 类库源码：`$JAVA_HOME/lib/src.zip`（`java.lang.invoke`、`java.util.concurrent`、`java.lang.VirtualThread`、`jdk.internal.vm.Continuation` 等）。
- 字节码：`javap -c -v -p` 是看 Java 语义如何落地的第一工具（lambda 的 invokedynamic、switch 模式匹配的 `SwitchBootstraps`、record 的 `ObjectMethods`）。
- HotSpot（C++）：GitHub `openjdk/jdk25u`，切到与本机 `java -version` 对应的 tag。重点目录：`src/hotspot/share/gc/`、`oops/`、`runtime/`、`opto/`（C2）、`prims/`（JNI / FFM 的 VM 侧）。
- JEP 页面是一手设计文档：先读 JEP 的 Motivation / Description / Alternatives / Risks，再看实现。

详细路径见 [`source-map.md`](source-map.md)。

---

## 六、实验规范（L3 的硬性要求）

每个实验报告写到 `chNN-xxx/notes/lab-<主题>.md`，包含：

1. **环境**：CPU 型号/核数、OS、`go version`、`java -version`、`GOMAXPROCS`、JVM 关键参数（GC、堆大小）。
2. **预测**：先写下你预期看到什么、为什么（引用规范条款或实现机制）。**先预测再运行**是 L3 的核心。
3. **方法**：代码位置、命令行、样本数。
4. **原始数据**：
   - Go：`go test -bench=. -count=10` + `benchstat` 输出（必须带置信区间 / p 值，不接受单次跑分）；编译器诊断（`-gcflags=-m=2`、`-d=ssa/check_bce`）原文。
   - Java：JMH（Ch17 引入）输出，`-f` ≥ 2、预热充分；或 JFR 记录摘要；`javap` / `-XX:+PrintCompilation` 原文。
   - 剖析：pprof / trace / JFR 截图或 `top` 文本。
5. **结论**：数据是否支持预测；不支持时用机制解释为什么。
6. **有效性威胁**：JIT 预热、CPU 降频、后台噪声、Windows 与 Linux 差异、GC 时机、内联让被测代码消失等。

> **平台提醒**：本机是 Windows。netpoller 在 Windows 上走 IOCP，Linux 才是 epoll；cgroup 感知的 GOMAXPROCS、`perf`、async-profiler、信号相关实验也只在 Linux 上有意义。Part 2–4 的运行时实验建议在 **WSL2 或 Linux 机器**上跑，并在报告里写清平台。

---

## 七、练习约定

**Go（`go/exercises`）**
- 表驱动测试 + `t.Run` 子测试；并发练习必须 `go test -race` 通过（`-race` 依赖 cgo，Windows 上需要 gcc + `CGO_ENABLED=1`，推荐在 WSL2 里跑，见 README「环境」）。
- 练习考的是语义和机制：很多测试会断言"规范规定的行为"（例如求值顺序、拷贝语义、零值），而不只是函数结果。
- 性能练习附 benchmark，统一用 `for b.Loop() { ... }` 写法；有"零分配"要求的用 `testing.AllocsPerRun` 断言。

**Java（`java/drills`）**
- 单文件、`bin/jrun` 直跑（JEP 512 紧凑源文件 + JEP 458 多文件启动），不引入 Maven/Gradle（JMH 例外，Ch17 单独建工程）。
- 文件内自带 `check(cond, msg)` 自校验：全部通过打印 `ALL PASS`，任一失败 `System.exit(1)`。
- 并发 drill 要求说明可见性/有序性依据（JMM happens-before 边），不接受"跑了没出错"。

**Review 时重点抓的"Java 味"**
- 到处定义大接口、先抽象后实现；接口定义在实现方。
- 用 `panic` 当异常，或把 `error` 吞掉。
- 用 `sync.Mutex` 保护一切，而不是先考虑所有权（单 goroutine 拥有状态）。
- goroutine 无主（没有退出路径、没有 `context`、没有人 `Wait`）。
- 热路径上 `interface{}`/`any` 装箱、闭包逃逸、`fmt.Sprintf` 拼字符串。
- getter/setter、`IXxx` 命名、`util` 包、包名用复数或驼峰。

---

## 八、一手资料（精选）

**Go 1.27**
- The Go Programming Language Specification（go.dev/ref/spec）· The Go Memory Model（go.dev/ref/mem）
- Go 1.27 Release Notes（go.dev/doc/go1.27）· `src/runtime/HACKING.md`
- 设计文档：go.googlesource.com/proposal（`design/` 目录：泛型、迭代器、loopvar、Green Tea 等）
- 官方博客：调度、GC 指南（go.dev/doc/gc-guide）、PGO、iterators、synctest、Swiss table 相关文章
- 书：《100 Go Mistakes and How to Avoid Them》《Efficient Go》（Płotka）《Concurrency in Go》

**Java 25**
- JLS / JVMS（Java SE 25 版）· JDK 25 全部 JEP（openjdk.org/projects/jdk/25）
- Aleksey Shipilëv：《Java Memory Model Pragmatics》、JVM Anatomy Quarks 系列
- 书：《Java Concurrency in Practice》《Java Performance（2nd ed., Oaks）》
- 工具文档：JFR / JMC、JMH samples、async-profiler README
