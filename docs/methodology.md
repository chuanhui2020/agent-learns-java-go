# 学习方法论（资深标准）

> 目标读者：已经能独立扛 Java 游戏服（Netty / Pekko / 虚拟线程 / JVM 调优）的工程师。
> 这里不教"语法怎么写"，而是带你在 **Go 1.27** 与 **Java 25** 两个运行时之间，建立**机制级**的对照和判断力。

---

## 一、心态转变 10 条（每写一段 Go 回来对一遍）

1. **错误是值，不是控制流。** 没有异常栈展开；`if err != nil` 是显式的控制流图。`panic` 只留给"程序已经不变量破坏"的场景。
2. **没有继承，只有组合。** 嵌入（embedding）是**字段提升 + 方法提升**，不是 is-a；不存在 override 与 `super`。
3. **接口是结构化、隐式满足的，而且应由使用方定义。** "Accept interfaces, return structs"。大接口是 Java 味。
4. **值语义是默认。** 赋值、传参、`range` 都在**拷贝**；你要决定的是"拷贝什么"（值 vs 指针 vs slice header）。这直接决定逃逸与 GC 压力。
5. **零值可用是一等设计。** `sync.Mutex{}`、`bytes.Buffer{}`、`strings.Builder{}` 开箱即用；构造函数是可选项，不是必需品。
6. **没有构造重载 / 默认参数 / 注解 / AOP。** 用工厂函数 + functional options + 代码生成，换来的是"读任何人的代码都像读自己的"。
7. **并发是语言内核，不是库。** goroutine、channel、select、调度器、netpoller、race detector 是一个整体设计；不要用"线程池 + 锁"的思路去套。
8. **运行时就在你手边。** `$GOROOT/src/runtime` 是可读的 Go 代码（不像 HotSpot 是 C++）。遇到"为什么"，先读源码，再读博客。
9. **可观测先于优化。** Go 的 pprof / trace / runtime/metrics 是标准库自带的；Java 的 JFR 是 JDK 自带的。没有数据的"优化"一律不算数。
10. **简单是刻意的，但不是简陋的。** Go 把复杂度从语言挪到了运行时和工具链；资深的价值在于知道它挪到了哪里。

---

## 二、双轨结构：每章都是"复习 Java 25 + 深入 Go 1.27"

每个主题按五段展开，前两段**同等分量**：

> **☕ Java 25 深度复习 → 🐹 Go 1.27 深入 → 📊 机制级对照 → 🔬 实验出数据 → 🎮 游戏服取舍**

- **☕ Java 25 深度复习**：不是"Java 里怎么写"，而是"JVM/JDK 25 是怎么实现的"——对应的 JEP、JDK 类库源码、HotSpot 机制、调优旋钮。你本来就会用，这里要补到能讲实现。
- **🐹 Go 1.27 深入**：分三层——**规范层**（spec 怎么规定）→ **运行时/编译器层**（runtime、cmd/compile 怎么实现）→ **源码阅读**（带着问题读指定文件和函数）。
- **📊 机制级对照**：对照表的列是"机制"，不是"语法"。比如不写"Java 用 `synchronized`、Go 用 `Mutex`"，而写"可重入性、自旋策略、饥饿处理、与调度器的交互、是否 pin 载体线程"。
- **🔬 实验**：每章至少 2 个**可复现、出数据**的实验。没有数据的结论只算假说。
- **🎮 游戏服取舍**：落到房间、玩家、网关、战斗帧、排行榜等真实场景，给出"选 Go 还是 Java、为什么、代价是什么"。不迷信任何一方。

---

## 三、能力分级 L1–L4（过章标准：L3）

| 级别 | 含义 | 证据 |
|---|---|---|
| **L1 会用** | 能写出正确、地道的代码 | 练习测试全绿、过 `-race`、`go vet` 干净 |
| **L2 懂原理** | 能讲清运行时/编译器/JVM 怎么实现 | 源码阅读笔记；能画出关键数据结构与状态机 |
| **L3 能调优排障** | 能用工具定位问题、用数据证明改进 | 实验报告（benchstat / pprof / trace / JFR 数据）；能复盘一个故障场景 |
| **L4 能设计取舍** | 能在 Go / Java 之间、在多种方案之间做有依据的架构决策 | 能写一页设计备忘：方案、指标、风险、回退 |

每章「✅ 验收」里的答辩题都标了级别。**L3 全过才算本章完成**；L4 题是加分项，Capstone 里会集中考。

---

## 四、每章 README 模板（带教时按此扩写）

```
# ChNN · 标题
> Part · 估时 · 状态 · 一句话定位

## 🎯 目标（按 L2/L3/L4 写，可验证）
## 🔗 前置
## 💡 关键洞察（1–3 条最该记住的判断）
## ☕ Java 25 深度复习（机制 + JEP + JDK/HotSpot 源码位置）
## 🐹 Go 1.27 深入（规范层 / 运行时与编译器层 / 源码阅读清单）
## 📊 Java 25 ↔ Go 1.27 机制级对照表
## 🔬 实验（目的 · 方法 · 工具 · 预期现象 · 要记录的指标）
## 🧪 练习规划（go/exercises · java/drills）
## 🎮 游戏服落地（场景 + 取舍）
## ✅ 验收答辩题（标注 L2/L3/L4）
## 🕳️ 常见坑（Java 老手专属）
## 📚 一手资料（spec / 源码 / JEP / 官方博客优先）
```

---

## 五、源码阅读方法

**Go 1.27**
- 源码就在本机：`go env GOROOT` → `src/`。用 `go doc -src runtime.gopark` 直接看函数源码。
- 先读 `src/runtime/HACKING.md`（runtime 编码约定：`g`/`m`/`p`、`systemstack`、写屏障限制）。
- **带着问题读**：每章源码清单都列了"读哪个文件、哪几个函数、回答什么问题"。不要从头读到尾。
- 笔记写到 `chNN-xxx/notes/source-<主题>.md`：画数据结构图 + 状态机 + 关键路径伪代码，标注文件:行号。

**Java 25**
- 类库源码：`$JAVA_HOME/lib/src.zip`（`java.util.concurrent`、`java.lang.VirtualThread`、`jdk.internal.vm.Continuation` 等）。
- HotSpot（C++）：GitHub `openjdk/jdk25u`，切到与本机一致的 tag（`java -version` 看构建号，如 `25.0.2+10` → `jdk-25.0.2+10`）。重点目录：`src/hotspot/share/gc/`、`oops/`、`runtime/`、`opto/`（C2）。
- JEP 页面是一手设计文档：先读 JEP 的 Motivation / Description / Risks，再看实现。

详细路径见 [`source-map.md`](source-map.md)。

---

## 六、实验规范（L3 的硬性要求）

每个实验报告写到 `chNN-xxx/notes/lab-<主题>.md`，包含：

1. **环境**：CPU 型号/核数、OS、`go version`、`java -version`、`GOMAXPROCS`、JVM 关键参数（GC、堆大小）。
2. **假设**：你预期看到什么、为什么（引用机制）。
3. **方法**：代码位置、命令行、样本数。
4. **原始数据**：
   - Go：`go test -bench=. -count=10` + `benchstat` 输出（必须带置信区间 / p 值，不接受单次跑分）。
   - Java：JMH（Ch15 引入）输出，`-f` ≥ 2、预热充分；或 JFR 记录摘要。
   - 剖析：pprof / trace / JFR 截图或 `top` 文本。
5. **结论**：数据是否支持假设；不支持时解释为什么。
6. **有效性威胁**：JIT 预热、CPU 降频、后台噪声、Windows 与 Linux 差异、GC 时机等。

> **平台提醒**：本机是 Windows。netpoller 在 Windows 上走 IOCP，Linux 才是 epoll；cgroup 感知的 GOMAXPROCS、`perf`、async-profiler 也只在 Linux 上有意义。Part 3、Part 4 的实验建议在 **WSL2 或 Linux 机器**上跑，并在报告里写清平台。

---

## 七、练习约定

**Go（`go/exercises`）**
- 表驱动测试 + `t.Run` 子测试；并发练习必须 `go test -race` 通过（`-race` 依赖 cgo，Windows 上需要 gcc + `CGO_ENABLED=1`，推荐在 WSL2 里跑，见 README「环境」）。
- 性能练习附 benchmark，统一用 `for b.Loop() { ... }` 写法；有"零分配"要求的用 `testing.AllocsPerRun` 断言。
- 解析 / 编解码类练习附 fuzz 测试（`go test -fuzz`）。

**Java（`java/drills`）**
- 单文件、`bin/jrun` 直跑（JEP 512 紧凑源文件 + JEP 458 多文件启动），不引入 Maven/Gradle（JMH 例外，Ch15 单独建工程）。
- 文件内自带 `check(cond, msg)` 自校验：全部通过打印 `ALL PASS`，任一失败 `System.exit(1)`。
- 并发 drill 要求说明可见性/有序性依据（JMM happens-before 边），不接受"跑了没出错"。

**Review 时重点抓的"Java 味"**
- 到处定义大接口、先抽象后实现；接口定义在实现方。
- 用 `panic` 当异常，或把 `error` 吞掉只打日志。
- 用 `sync.Mutex` 保护一切，而不是先考虑所有权（单 goroutine 拥有状态）。
- goroutine 无主（没有退出路径、没有 `context`、没有人 `Wait`）。
- 热路径上 `interface{}`/`any` 装箱、闭包逃逸、`fmt.Sprintf` 拼字符串。
- getter/setter、`IXxx` 命名、`util` 包、包名用复数或驼峰。

---

## 八、一手资料（精选）

**Go 1.27**
- The Go Programming Language Specification（go.dev/ref/spec）· The Go Memory Model（go.dev/ref/mem）
- Go 1.27 Release Notes（go.dev/doc/go1.27）· `src/runtime/HACKING.md`
- 官方博客：调度、GC 指南（go.dev/doc/gc-guide）、PGO、Green Tea、iterators、synctest 相关文章
- 书：《100 Go Mistakes and How to Avoid Them》《Efficient Go》（Płotka）《Concurrency in Go》

**Java 25**
- JLS / JVMS（Java SE 25 版）· JDK 25 全部 JEP（openjdk.org/projects/jdk/25）
- Aleksey Shipilëv：《Java Memory Model Pragmatics》、JVM Anatomy Quarks 系列
- 书：《Java Concurrency in Practice》《Java Performance（2nd ed., Oaks）》《Netty in Action》
- 工具文档：JFR / JMC、JMH samples、async-profiler README
