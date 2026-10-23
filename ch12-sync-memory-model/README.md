# Ch12 · 同步原语与内存模型 ⭐

> Part 2 · 估时 18h · 状态：⚪ 未开始 · 从 happens-before 到 Mutex 饥饿模式：两门语言的"正确性契约"和它们的实现代价

## 🎯 目标（资深标准）

- **L2**：能用 Go 内存模型和 JMM 分别论证一段并发代码是否正确：找出每一条 happens-before 边，判断是否存在数据竞争。讲清"无数据竞争的程序表现为顺序一致"（DRF-SC）在两边的含义，以及数据竞争在两边后果的差异。
- **L2**：能讲清 `sync.Mutex` 的状态字、自旋条件、正常/饥饿两种模式和切换阈值，以及 `runtime/sema.go` 怎么挂起和唤醒等待者；讲清 sync.Pool 的 per-P 结构和 victim cache；讲清 sync.Map 现在基于 HashTrieMap。
- **L3**：先预测再验证：给定一段并发代码，先列出它依赖的 happens-before 边、判断有没有数据竞争，再用 `-race` 验证；先预测锁竞争、伪共享、饥饿模式对吞吐和尾延迟的影响，再用 mutex / block profile 和 benchmark 证明。
- **L4**：能讲清两门语言内存模型与同步原语的设计取舍：Go 为什么只提供顺序一致的原子操作而 Java 提供四档访问模式；`sync.Mutex` 为什么刻意不可重入；为什么 Go 用"正常 / 饥饿"自动切换而不是让用户选择公平锁；`StableValue` 能被 JIT 常量折叠而 `sync.Once` 不能，背后是 AOT 与 JIT 的什么差异。

## 🔗 前置

[Ch10](../ch10-scheduler/)（gopark / 调度器）、[Ch11](../ch11-channels-select/)（channel 的 happens-before 规则）

## 💡 关键洞察

1. **两门语言的契约都是 DRF-SC，但"有数据竞争"时的后果不同。** Java 对有竞争的程序仍然给出有限的保证：不会凭空产生值（out-of-thin-air），`final` 字段有特殊语义。Go 的内存模型同样禁止凭空产生值；但对于多字长的值（接口、slice、string），竞争读写可能读到"撕裂"的值，进而导致内存损坏。所以在 Go 里，数据竞争是必须清零的缺陷（[Ch14](../ch14-race-leak-detection/) 用 `-race` 抓）。
2. **Go 的 Mutex 是"不可重入 + 可饥饿切换"的。** 不可重入是刻意的设计：持锁期间不变量可能暂时被破坏，重入会让你在一个不一致的状态上继续操作。默认的正常模式偏向吞吐（新来的 goroutine 可以插队）；等待超过 1ms 的等待者会把锁切换成饥饿模式，所有权直接交接，用来压尾延迟。
3. **先想所有权，再想锁。** 大多数状态都可以归属于一个 goroutine，由它独占读写，其他 goroutine 通过 channel 与它通信（[Ch11](../ch11-channels-select/)）。只有真正需要共享的少量数据才用锁或原子操作。读多写少的数据用 `atomic.Pointer[T]` 发布不可变快照，往往比 RWMutex 好得多。

## ☕ Java 25 深度复习

### JMM
- **happens-before 规则**：程序次序、监视器锁的释放和获取、`volatile` 的写和读、线程 `start` / `join`、传递性。JMM 保证"正确同步的程序（无数据竞争）表现为顺序一致"。
- **`final` 字段语义**：构造函数中正确发布的 `final` 字段，对其他线程保证可见，即使对象本身是通过竞争发布的。Go 没有对应的保证。
- **`volatile`**：顺序一致的读写。HotSpot 在 x86 上 volatile 写之后需要一个 StoreLoad 屏障（实现中常用 `lock` 前缀指令）。

### VarHandle 的访问模式（`java.base/java/lang/invoke/VarHandle.java`）
| 模式 | 方法 | 语义 | 对照 Go |
|---|---|---|---|
| Plain | `get` / `set` | 普通读写，无顺序保证 | 普通变量 |
| Opaque | `getOpaque` / `setOpaque` | 对同一变量保证一致性，不建立跨变量的顺序 | 没有对应物 |
| Acquire / Release | `getAcquire` / `setRelease` | 单向屏障，适合发布和订阅 | 没有对应物（Go 不提供弱于顺序一致的原子操作） |
| Volatile | `getVolatile` / `setVolatile`、`compareAndSet` | 顺序一致 | `sync/atomic` 的全部操作 |
| 栅栏 | `fullFence` / `acquireFence` / `releaseFence` | 显式屏障 | 没有（Go 不暴露屏障） |

Go 刻意只提供顺序一致的原子操作：用一点性能换来更简单的推理。

### 锁的实现
- **`synchronized`**：JDK 25 默认使用轻量级锁定（本机 `-XX:+PrintFlagsFinal` 显示 `LockingMode = 2`，即 LM_LIGHTWEIGHT）。无竞争时在线程的 lock-stack 上记录；发生竞争或 `wait()` 时膨胀为 ObjectMonitor。偏向锁已经不存在。紧凑对象头（JEP 519，[Ch02](../ch02-values-memory/)）依赖这套新的锁实现。
- **AQS**（`java.base/java/util/concurrent/locks/AbstractQueuedSynchronizer.java`）：一个 `state` 加一条 CLH 变体的等待队列。`ReentrantLock` 的公平和非公平模式，对应的正是 Go Mutex 饥饿模式和正常模式要解决的那个权衡。
- **`StampedLock`**：乐观读（`tryOptimisticRead` + `validate`），不可重入。对照 Go 的 `atomic.Pointer` 快照读。
- **`LongAdder` / `Striped64`**：分段计数，`Cell` 上标注了 `@jdk.internal.vm.annotation.Contended` 来避免伪共享。用户代码要用 `@Contended`，需要加 `-XX:-RestrictContended`；填充宽度由 `ContendedPaddingWidth` 控制（本机默认 128 字节）。
- **`StableValue`（JEP 502，preview）**（`java.base/java/lang/StableValue.java`）：`StableValue.of()` + `orElseSet(supplier)`，或者 `StableValue.supplier(...)`、`list(...)`、`map(...)`。至多设置一次，而且设置后 JIT 可以把它当作常量折叠（[Ch15](../ch15-alloc-escape-compiler/)）。对照 Go 的 `sync.OnceValue`：语义相近，但 Go 没有常量折叠这一层。

## 🐹 Go 1.27 深入

### 规范层：Go 内存模型（go.dev/ref/mem）
- **核心定义**：如果一次写 w 和一次读 r 访问同一位置，且至少一个是写，二者之间又没有 happens-before 关系，就是**数据竞争**。无数据竞争的程序是 **DRF-SC** 的。
- **同步原语建立的边**：
  - `go` 语句 happens-before 新 goroutine 开始执行；goroutine 的退出**不**和任何事件同步（必须显式等待）。
  - channel 的四条规则（见 [Ch11](../ch11-channels-select/)）。
  - `sync.Mutex` / `RWMutex`：第 n 次 `Unlock` happens-before 第 m 次 `Lock` 返回（n < m）。
  - `sync.Once`：`f()` 的完成 happens-before 任何 `Do(f)` 返回。
  - `sync/atomic`：所有原子操作表现为**某个顺序一致的全序**。如果原子读 B 观察到了原子写 A 的结果，那么 A 同步于 B。
- **有竞争时的限制**：Go 禁止凭空产生值，单字长、对齐的读写不会撕裂；但多字长值（接口、slice、string、大结构体）的竞争读写可能读到不一致的组合，进而破坏内存安全。实践结论：数据竞争一律当作缺陷处理。
- **不要依赖"良性竞争"**：比如双重检查锁定、用普通 bool 做停止标志。在 Go 里应该改成 `sync.Once` / `atomic.Bool`。

### 运行时与编译器层
- **`sync.Mutex`**：`sync.Mutex` 是 `internal/sync.Mutex` 的包装。
  - **状态字 `state`**：`mutexLocked`、`mutexWoken`、`mutexStarving` 三个标志位，高位是等待者计数（`mutexWaiterShift`）。另有一个 `sema` 字段。
  - **快路径**：一次 CAS 把 `state` 从 0 改成 `mutexLocked`，可以内联。
  - **慢路径 `lockSlow`**：满足自旋条件时用 `procyield` 自旋；否则 `runtime_SemacquireMutex` 挂起。被唤醒但抢锁失败的等待者以 LIFO 方式插回队头。等待时间超过 `starvationThresholdNs = 1e6`（1ms）就切换到饥饿模式。
  - **自旋条件**（`runtime/proc.go` 中的 `internal_sync_runtime_canSpin`）：自旋次数少于 `active_spin = 4`、多核、GOMAXPROCS 大于"空闲 P + 自旋 M + 1"，并且当前 P 的本地队列为空。每次自旋执行 `procyield(active_spin_cnt = 30)`。
  - **饥饿模式**：`Unlock` 直接把所有权交给队头等待者（`runtime_Semrelease(&m.sema, true, ...)` 的 handoff 参数）；新来者不自旋、不插队，直接排到队尾。拿到锁的等待者如果是最后一个，或者等待时间不到 1ms，就切回正常模式。
  - `TryLock` 存在，但官方文档提醒：需要用它的场景通常说明设计有问题。
- **`runtime/sema.go`**：`semTable` 有 `semTabSize = 251` 个桶，按地址哈希到 `semaRoot`。每个桶里是一棵按地址组织的树，同一地址的等待者挂成链表。`semacquire1` 负责挂起，`semrelease1` 负责唤醒。`sync.Cond` 用的是另一套机制 `notifyList`（`notifyListAdd` / `notifyListWait` / `notifyListNotifyOne` / `notifyListNotifyAll`），基于票号，避免丢失唤醒。
- **`RWMutex`**：`readerCount` 加 `readerWait`。写者到来时把 `readerCount` 减去 `rwmutexMaxReaders = 1<<30`，让新来的读者阻塞，即**写者优先**。所以"读锁里再拿读锁"在有写者排队时会死锁。
- **`WaitGroup`**：一个 64 位状态（计数 + 等待者数，另有一位标记 synctest 气泡）加一个 sema。`WaitGroup.Go(f)` 把 `Add(1)` + `go` + `defer Done()` 合在一起，消除"在 goroutine 里面调用 `Add`"这类竞态（vet 的 `waitgroup` 分析器专门抓这个）。
- **`Once` / `OnceFunc` / `OnceValue` / `OnceValues`**：`Once` 是 `done atomic.Bool` + `Mutex`，快路径只有一次原子读。`OnceValue(s)` 返回带缓存结果的闭包，`f` panic 时，每次调用都会重新 panic 同一个值。
- **`sync.Map`**：内部是 `internal/sync.HashTrieMap[any, any]`，一棵并发哈希前缀树。支持 `Clear`、`Swap`、`CompareAndSwap`、`Range`。适用场景依然是"键集合稳定、读远多于写"，或者"不同 goroutine 操作不相交的键"。其他场景用 `map` + `Mutex` 或分片 map。
- **`sync.Pool`**：每个 P 一个 `poolLocal`，包含 `private`（只有本 P 访问，无需同步）和 `shared`（`poolChain`，一个无锁的双端队列链：本 P 从头部 push/pop，其他 P 从尾部偷）。`Get` 的顺序是：private → 本 P 的 shared → 偷其他 P → **victim cache**。每次 GC 时 `poolCleanup` 把主缓存移到 victim，并丢弃上一轮的 victim。所以对象最多活过一次 GC，平滑了"GC 后全部清空"带来的分配尖峰。
  - **使用上的三个推论**：①往池里放非指针值（`[]byte`、小 struct）时，`Put(any)` 每次都要装箱分配，所以应该放 `*[]byte` 这类指针；②池中对象随时可能被 GC 清空，所以它只能缓存"随时可以重建"的东西，不能当连接池或会话表；③不限容量的 buffer 放回池里，一次超大请求就会让池里长期留着大 buffer（内存膨胀），所以要按尺寸分级，超过上限的直接丢弃。
- **`sync/atomic` 类型**：`atomic.Int64`、`Uint64`、`Bool`、`Pointer[T]`、`Value` 等。类型化的版本带 `noCopy`；`Int64` / `Uint64` 在 32 位平台上也保证 8 字节对齐（`align64`）。**1.27 新增**：`go fix` 的 `atomictypes` 现代化器会把"普通字段 + `atomic.AddInt64(&x, ...)`"改写成类型化的原子类型。
- **伪共享**：两个被不同核心频繁写入的变量落在同一条 cache line（通常 64 字节；部分平台上相邻行预取让有效粒度更大）。Go 没有 `@Contended`，要手动填充，比如 `_ [64]byte`，或者用标准库内部的 `cpu.CacheLinePad` 思路，后者不对外导出。
- **vet**：`copylocks` 检查锁被值拷贝（`noCopy` 字段就是给它看的），`waitgroup` 检查错误位置的 `Add`。

### 源码阅读清单（`$GOROOT/src`）
| 文件 | 函数 / 类型 | 带着什么问题读 |
|---|---|---|
| `internal/sync/mutex.go` | 文件头的"Mutex fairness"注释、`Lock`、`lockSlow`、`Unlock`、`unlockSlow`、`TryLock` | 正常模式为什么吞吐更好？饥饿模式下 `Unlock` 怎么做到"直接交接"？`mutexWoken` 防止的是什么？ |
| `runtime/proc.go` | `internal_sync_runtime_canSpin`、`internal_sync_runtime_doSpin` | 为什么"本地队列非空"就不该自旋？单核时自旋为什么没有意义？ |
| `runtime/sema.go` | `semaRoot`、`semTable`、`semacquire1`、`semrelease1`、`notifyList*` | 251 个桶怎么在地址冲突下仍然正确？`Cond` 为什么不直接复用 sema？ |
| `sync/rwmutex.go` | `RLock`、`Lock`、`rwmutexMaxReaders`、`readerWait` | 写者是怎么挡住新读者的？构造一个"读锁重入 + 写者排队"导致死锁的时序。 |
| `sync/waitgroup.go` · `sync/once.go` · `sync/oncefunc.go` | `Add`、`Wait`、`Go`；`doSlow`；`OnceValue` | `Once` 为什么不能只用一个原子 CAS 实现？`OnceValue` 怎么处理 panic？ |
| `sync/pool.go` · `sync/poolqueue.go` | `Get`、`Put`、`pinSlow`、`poolCleanup`、`poolChain`、`poolDequeue` | 为什么 `private` 不需要任何同步？victim cache 平滑了什么？`poolDequeue` 为什么只允许一端 push？ |
| `sync/map.go` → `internal/sync/hashtriemap.go` | `HashTrieMap.Load`、`LoadOrStore` | 读路径为什么不需要加锁？写路径锁的粒度是多大？ |
| `sync/atomic/type.go` · `sync/atomic/doc.go` | `Int64`、`Pointer[T]`、`noCopy`、`align64` | 为什么推荐类型化原子而不是 `atomic.AddInt64(&x)`？32 位平台上的对齐问题是什么？ |

## 📊 Java 25 ↔ Go 1.27 机制级对照表

| 机制 | Java 25 | Go 1.27 | 取舍 / 设计动机 |
|---|---|---|---|
| 内存模型契约 | JMM：DRF-SC，有竞争时仍有限保证，`final` 字段语义 | Go MM：DRF-SC，禁止凭空产生值，多字长值竞争可能撕裂 | JMM 要给有竞争的程序也留安全底线（Java 曾要求能安全运行不可信代码）；Go 的接口、slice 是多字长值，干脆要求程序无竞争 |
| 原子操作强度 | plain / opaque / acquire-release / volatile 四档 + 栅栏 | 只有顺序一致的一档 | Go 用一点性能换推理简单；Java 把弱内存序暴露给库作者，换取极端热路径上的性能 |
| 互斥锁可重入 | `synchronized` / `ReentrantLock` 都可重入 | `sync.Mutex` **不可重入** | 持锁期间不变量可能暂时被破坏，重入就是在不一致的状态上继续操作；Go 也没有 goroutine 身份来实现"持有者" |
| 公平性 | `ReentrantLock(true)` 显式选择公平 | 自动切换：正常模式求吞吐，等待超过 1ms 切换到饥饿模式 | 让用户在吞吐和公平之间二选一往往选错；Go 用一个阈值自动兜住尾延迟 |
| 自旋 | HotSpot 在膨胀前有自适应自旋 | 最多 4 轮 `procyield(30)`，且要求多核、有空闲 P、本地队列为空 | goroutine park 很便宜，所以 Go 的自旋很保守，长临界区几乎立刻挂起 |
| 读写锁 | `ReentrantReadWriteLock`、`StampedLock` 乐观读 | `RWMutex`（写者优先，不可重入） | 读者计数本身是一个被所有核写的原子变量；读多写少时两边都应优先考虑"不可变快照 + 原子指针" |
| 惰性初始化 | `StableValue`（preview，可被 JIT 常量折叠）、holder 类惯用法 | `sync.Once` / `OnceValue(s)` | JIT 可以在值确定之后把它当常量重新编译；AOT 编译的 Go 每次访问都要做一次原子读 |
| 计数器 | `LongAdder`（分段 + `@Contended`） | 没有内置分段计数，要手写分片 + 填充；或改用每 goroutine 本地计数再汇总 | Go 标准库倾向提供少量通用原语，分段计数这类特化结构留给用户 |
| 并发 map | `ConcurrentHashMap`（分桶锁 + CAS，可用于通用场景） | `sync.Map`（HashTrieMap，适用场景较窄）/ `map` + `Mutex` / 分片 | 内建 map 不为并发付费（[Ch03](../ch03-slice-map-string/)），`sync.Map` 只针对"键稳定、读远多于写"这类场景 |
| 对象池 | 自己写；现代 GC 下通常不值得 | `sync.Pool`（per-P、victim、GC 时清理） | Go 的分配器没有 TLAB 的指针碰撞那么便宜（[Ch15](../ch15-alloc-escape-compiler/)），Pool 是降低分配率的标准工具 |
| 防误用 | 没有（锁对象可以随意共享） | `noCopy` + vet `copylocks` | 值语义下锁可能被意外拷贝，这是 Go 特有的坑，所以需要静态检查 |

## 🔬 实验

### 实验 1：Mutex 正常模式 vs 饥饿模式的尾延迟（必做）
- **目的**：观察饥饿模式切换对尾延迟和吞吐的影响。
- **方法**（`go/lab/mutex`）：16 个 goroutine 争抢一把锁，临界区 1µs，每个 goroutine 记录"请求锁到获得锁"的延迟直方图；对照一个临界区 2ms 的版本（会频繁触发饥饿模式）。
- **工具**：`go test -bench -count=10`、benchstat；`-mutexprofile` 加 `runtime.SetMutexProfileFraction`；`go tool trace` 看锁等待。
- **预期现象**：短临界区时吞吐高，但个别等待者的延迟很长（新来者插队）；长临界区时进入饥饿模式，p999 被压住，吞吐下降。
- **记录**：p50 / p99 / p999 获锁延迟、吞吐、mutex profile 的 top 输出。

### 实验 2：伪共享前后的 benchmark
- **方法**：一个结构体里放 8 个计数器，8 个 goroutine 各自只写自己的那个；对比有无 `_ [64]byte` 填充的两个版本。Java 对照：`long[]` 相邻元素 vs `LongAdder` vs 用 `@Contended` 字段（`-XX:-RestrictContended`）。
- **工具**：benchstat；Linux 上可以用 `perf stat -e cache-misses` 交叉验证（WSL2 / Linux）。
- **预期现象**：填充后吞吐有数倍差距（具体倍数取决于 CPU）。
- **记录**：ns/op、cache miss（如果有）；结论写成"什么时候值得填充"。

### 实验 3：读多写少——RWMutex vs atomic.Pointer 快照
- **方法**：一份只读数据（如一个 map），每秒整体更新 1 次，64 个 goroutine 高频读取。三种实现：`RWMutex` 保护的 map、`atomic.Pointer[Config]` 发布不可变快照、`sync.Map`。
- **工具**：benchstat（`-cpu=1,4,16`）；block profile。
- **预期现象**：核数越多，RWMutex 的 `readerCount` 原子变量越成为热点，扩展性越差；快照读几乎是线性扩展的。
- **记录**：不同 CPU 数下的 ns/op 曲线；用 `RWMutex.RLock` 的实现（对 `readerCount` 的原子加）解释扩展性差异。

### 实验 4：sync.Pool 的 victim 行为
- **方法**：Put 1000 个对象，分别在调用 1 次和 2 次 `runtime.GC()` 后 Get，统计命中数；用 runtime/metrics 观察分配率。
- **预期现象**：GC 1 次后仍有部分命中（来自 victim），GC 2 次后基本清空。
- **追加**：先往池里放一批 1MB 的 buffer，再切换成只用 200B 的负载，观察 `/gc/heap/live:bytes`；然后改成"按尺寸分级 + 超过上限不放回"，对比结果，验证上面的第③条推论。

## 🧪 练习规划

**Go：`go/exercises`**（`go test -race ./ch12-sync-memory-model/go/...`）
1. `type ShardedCounter struct` 带 `Add(n int64)` / `Load() int64`：按 GOMAXPROCS 分片，每个分片填充到 cache line。测试正确性（并发累加的结果准确，过 `-race`）；benchmark 对比单个 `atomic.Int64`。
2. `type Snapshot[T any] struct` 带 `Load() *T` / `Update(func(old *T) *T)`：`atomic.Pointer[T]` 加 CAS 重试循环，实现无锁读、串行化写。测试：并发 Update 不丢更新；Load 读到的永远是完整的快照。
3. `type KeyedMutex[K comparable] struct` 带 `Lock(k K) (unlock func())`：按 key 加锁，key 不再使用时回收锁对象（引用计数）。测试：同 key 互斥、不同 key 并行、长时间运行后内部 map 不会无限增长。
4. `func LazyResource[T any](init func() (T, error)) func() (T, error)`：在 `sync.OnceValues` 之上实现"失败可重试"的惰性初始化（`OnceValues` 本身会缓存错误）。测试：首次失败后，下一次调用会重试；并发调用 `init` 只执行一次。
5. **找出竞争**：给出 4 段代码片段（双重检查锁定、用普通 bool 做停止标志、map 读写竞争、在 goroutine 内部调用 `WaitGroup.Add`），在测试里用 `-race` 复现，再修复。每段在注释里写出缺失的 happens-before 边。

**Java：`java/drills`**
1. `JmmDrill.java`：用 `VarHandle` 实现一个单生产者单消费者的环形缓冲（`setRelease` / `getAcquire`），自校验数据完整；在注释里画出每条 happens-before 边，并说明为什么用 opaque 就不够。
2. `StableValueDrill.java`（需要 `--enable-preview`，`bin/jrun` 已默认带上）：分别用 `StableValue.supplier` 和 holder 类惯用法实现惰性单例，自校验 supplier 只执行一次。对照 Go 的 `sync.OnceValue` 写注释。

## 🧭 设计取舍

- **为什么 Go 只有顺序一致的原子操作。** Java 把 plain / opaque / acquire-release / volatile 四档访问模式通过 `VarHandle` 暴露给库作者，让 `j.u.c` 能在弱内存序硬件上榨出性能，代价是这些代码极难推理和验证。Go 的内存模型文档明确选择只提供顺序一致的原子操作，Russ Cox 的系列文章《Memory Models》解释了理由：大多数程序员用不对弱内存序，标准库内部需要更弱语义时由运行时自己处理。
- **为什么 `sync.Mutex` 不可重入。** 可重入锁让"持锁调用另一个也要加这把锁的方法"变得合法，但持锁期间不变量可能暂时被破坏，重入就是在不一致的状态上继续操作。Go 选择让这种代码直接死锁，逼你拆成"外部加锁、内部无锁"的两层函数。另一个原因是 goroutine 没有身份，实现"持有者是谁"本身就不自然。
- **为什么是自动的饥饿模式，而不是公平锁开关。** 非公平锁吞吐高，但个别等待者可能长期抢不到；公平锁每次都要交接，吞吐大幅下降。Go 不让用户选择，而是在任何等待者等待超过 1ms（`starvationThresholdNs`）时切换到饥饿模式（直接交接），等待者队列清空或等待时间变短后再切回去。代价是这个阈值是固定的。
- **为什么 StableValue 能被常量折叠，`sync.Once` 不能。** HotSpot 的 JIT 可以在值确定之后把被 `@Stable` 修饰的字段当作常量，重新编译依赖它的代码；Go 是 AOT 编译，二进制在运行前就定了，每次访问 `OnceValue` 的结果都要先原子读一次"是否已完成"。这是 AOT 与 JIT 的根本差异在同步原语上的投影（[Ch15](../ch15-alloc-escape-compiler/)）。
- **为什么数据竞争在 Go 里是硬错误。** JMM 要求有竞争的程序也不能凭空产生值、`final` 字段在构造后安全可见，这部分是因为 Java 早期需要安全地运行不可信代码。Go 的接口、slice、string 都是多个字长组成的值，竞争读写可能读到"类型指针来自新值、数据指针来自旧值"的撕裂结果，直接破坏内存安全。所以 Go 不为有竞争的程序提供有意义的保证，而是提供 `-race` 让你把竞争清零（[Ch14](../ch14-race-leak-detection/)）。

## ✅ 验收答辩题

- [ ] [L2] 写出 Go 内存模型中 `go` 语句、channel、Mutex、Once、atomic 各自建立的 happens-before 边。为什么"goroutine 退出"不和任何事件同步？
- [ ] [L2] 对比 DRF-SC 在 JMM 和 Go 内存模型里的含义；有数据竞争时两边各自还保证什么？举一个在 Go 里会读到撕裂值的例子。
- [ ] [L2] 讲清 `sync.Mutex` 的状态字、快路径、自旋条件、正常与饥饿模式，以及两种模式之间的切换规则。
- [ ] [L2] 为什么 Go 的 Mutex 不可重入？Java 的可重入锁在 Go 里应该怎么重构？
- [ ] [L2] 讲清 sync.Pool 的 per-P 结构、`Get` 的查找顺序和 victim cache 的作用。为什么 Pool 不能用来做连接池？
- [ ] [L2] RWMutex 怎么实现写者优先？写出一个因此导致死锁的时序。
- [ ] [L2] VarHandle 的四种访问模式分别保证什么？Go 为什么只提供顺序一致的原子操作？
- [ ] [L3] 先预测饥饿模式对 p999 等待时间和吞吐的影响，再用实验 1 的数据验证；如果观察到尾延迟尖峰，怎样用 mutex profile 和 trace 证明它来自锁竞争？
- [ ] [L3] 用实验 3 的数据说明：什么情况下 RWMutex 反而比 Mutex 还慢？快照方案的代价是什么？
- [ ] [L3] mutex profile 和 block profile 分别在哪里采样、统计的是什么时间？`SetMutexProfileFraction` 和 `SetBlockProfileRate` 的参数含义有什么不同？读 `runtime/mprof.go` 验证你的回答。
- [ ] [L3] 一段代码用普通 `bool` 做停止标志，测试从来没有失败过。用内存模型解释它为什么是错的，并说明 `-race` 能不能抓到。
- [ ] [L4] Go 只提供顺序一致的原子操作，Java 通过 `VarHandle` 提供四档访问模式。读 Go Memory Model 文档和 Russ Cox 的《Memory Models》系列，论证这两种选择各自服务于谁、代价是什么。
- [ ] [L4] 可重入锁和不可重入锁各自鼓励什么样的代码结构？一段 Java 代码大量依赖 `synchronized` 方法互相调用，用 Go 的不可重入 `Mutex` 改写时，结构上必须做什么改变？这种改变是负担还是收益？

## 🕳️ 常见坑

- **重入加锁**：持有 `Mutex` 时调用另一个也要加同一把锁的方法，直接自死锁。Go 没有可重入锁。
- **锁被值拷贝**：结构体按值传递或按值接收者调用时，里面的 `Mutex` 被复制。用指针接收者，并让 vet `copylocks` 守住。
- **"良性竞争"思维**：用普通变量做标志位、双重检查锁定。在 Go 里一律用 `atomic` 或 `Once`。
- **读锁里再拿读锁**：有写者排队时会死锁。
- **滥用 `sync.Map`**：在键频繁变化、读写比例接近的场景下，它比 `map + Mutex` 还慢，而且失去了类型安全。
- **在 goroutine 内部调用 `wg.Add(1)`**：`Wait` 可能在 `Add` 之前返回。用 `wg.Go(f)`，或者在 `go` 语句之前调用 `Add`。
- **期待 sync.Pool 里的对象一直存在**：GC 两轮后就清空了，它不是缓存。
- **以为 `atomic.Value` 能存不同类型**：同一个 `Value` 中存放不同的具体类型会 panic；优先用 `atomic.Pointer[T]`。
- **照搬 Java 的 `volatile` 心智**：Go 没有 `volatile` 关键字，所有跨 goroutine 的可见性都必须通过同步原语或原子操作建立。

## 📚 一手资料

- Go：[The Go Memory Model](https://go.dev/ref/mem)；Russ Cox 的 [Memory Models 系列](https://research.swtch.com/mm)（硬件 / 编程语言 / Go 内存模型更新三篇）；`src/internal/sync/mutex.go` 文件头注释；`src/sync/pool.go`；[sync 包文档](https://pkg.go.dev/sync)；[Go 1.27 Release Notes](https://go.dev/doc/go1.27)
- Java：JLS 第 17 章（Threads and Locks）；Doug Lea 的 [Using JDK 9 Memory Order Modes](https://gee.cs.oswego.edu/dl/html/j9mm.html)；Aleksey Shipilëv 的《Java Memory Model Pragmatics》；[JEP 502 Stable Values](https://openjdk.org/jeps/502)；src.zip 中的 `VarHandle.java`、`AbstractQueuedSynchronizer.java`、`StampedLock.java`、`Striped64.java`、`StableValue.java`
