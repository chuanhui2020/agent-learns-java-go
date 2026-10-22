# 源码地图：Go 1.27 × JDK 25

> 每章「源码阅读清单」的总索引。Go 的文件路径和函数名都已在 go1.27.3 源码中逐一核实；JDK 类路径已在 JDK 25.0.2 的 `lib/src.zip` 中核实。
> 读法见 [methodology.md 第五节](methodology.md#五源码阅读方法)：**带着问题读，画图做笔记，不从头读到尾。**

## 怎么打开源码

```bash
# Go：工具链自带完整源码
go env GOROOT                      # → $GOROOT/src/...
go doc -src runtime.gopark         # 直接打印某个函数的源码
go doc -src sync.Mutex.Lock

# Java：类库源码在 src.zip（按模块分目录，如 java.base/java/lang/VirtualThread.java）
java -XshowSettings:properties -version 2>&1 | grep java.home   # → $JAVA_HOME/lib/src.zip
# HotSpot（C++）：github.com/openjdk/jdk25u，切到与 `java -version` 构建号一致的 tag（如 jdk-25.0.2+10）
```

> 注意：`src.zip` 只包含**当前平台**的实现类。Windows 版 JDK 里能看到 `sun/nio/ch/WEPollPoller.java`；Linux 的 `EPollPoller` 等要去 openjdk 仓库的 `src/java.base/linux/classes/` 下看。

---

## Go 1.27（`$GOROOT/src`）

| 主题 | 文件 | 关键函数/类型 | 章节 |
|---|---|---|---|
| 运行时总约定 | `runtime/HACKING.md` | g/m/p、systemstack、写屏障约束 | 全部 |
| 调度器数据结构 | `runtime/runtime2.go` | `g`、`m`、`p`、`schedt`、`sudog` | [Ch07](../ch07-scheduler/) |
| 调度循环 | `runtime/proc.go` | `schedule`、`findRunnable`、`runqput`/`runqget`/`runqsteal`、`newproc1` | [Ch07](../ch07-scheduler/) |
| 阻塞与唤醒 | `runtime/proc.go` | `gopark`、`goready` | [Ch07](../ch07-scheduler/) · [Ch08](../ch08-channels-select/) |
| 系统调用与抢占 | `runtime/proc.go` · `runtime/preempt.go` | `entersyscall`/`exitsyscall`、`handoffp`、`sysmon`、`retake`、`preemptone` | [Ch07](../ch07-scheduler/) |
| goroutine 栈 | `runtime/stack.go` | `newstack`、`copystack`、`shrinkstack` | [Ch07](../ch07-scheduler/) |
| channel | `runtime/chan.go` | `hchan`、`chansend`、`chanrecv`、`closechan` | [Ch08](../ch08-channels-select/) |
| select | `runtime/select.go` | `selectgo` | [Ch08](../ch08-channels-select/) |
| 运行时定时器 | `runtime/time.go` | `timer`、`timers`（每 P 一个堆，`addHeap`/`deleteMin`） | [Ch08](../ch08-channels-select/) · [Ch11](../ch11-actor/) |
| 信号量 | `runtime/sema.go` | `semacquire1`、等待者树 | [Ch09](../ch09-sync-memory-model/) |
| Mutex | `internal/sync/mutex.go`（`sync.Mutex` 是它的包装） | `Lock`、`lockSlow`、`starvationThresholdNs` | [Ch09](../ch09-sync-memory-model/) |
| 其他同步原语 | `sync/rwmutex.go` · `sync/waitgroup.go` · `sync/once.go` · `sync/oncefunc.go` · `sync/cond.go` | `WaitGroup.Go`、`OnceValue` | [Ch09](../ch09-sync-memory-model/) |
| sync.Map | `sync/map.go` → `internal/sync/hashtriemap.go` | `HashTrieMap[K,V]` | [Ch09](../ch09-sync-memory-model/) |
| sync.Pool | `sync/pool.go` · `sync/poolqueue.go` | `Get`、`pinSlow`、`poolCleanup`、`poolChain` | [Ch09](../ch09-sync-memory-model/) · [Ch16](../ch16-zero-alloc-dod/) |
| 原子类型 | `sync/atomic/type.go` | `Int64`、`Pointer[T]` | [Ch09](../ch09-sync-memory-model/) |
| 运行时内部锁 | `runtime/lock_spinbit.go` | `lock2` | [Ch09](../ch09-sync-memory-model/) |
| context | `context/context.go` | `cancelCtx`、`propagateCancel`、`AfterFunc` | [Ch10](../ch10-context-structured/) |
| goroutine 泄漏检测 | `runtime/mgc.go` · `runtime/mprof.go` | `goroutineLeakGC`、`goroutineLeakProfileWithLabels` | [Ch12](../ch12-concurrency-testing/) |
| 结构体/接口表示 | `runtime/runtime2.go` · `runtime/iface.go` | `iface`、`eface`、`itab`、`getitab`、`typeAssert` | [Ch03](../ch03-interfaces/) |
| slice | `runtime/slice.go` | `growslice` | [Ch02](../ch02-slice-map-string/) |
| string | `runtime/string.go` | `slicebytetostring` 等转换 | [Ch02](../ch02-slice-map-string/) |
| map（Swiss table） | `internal/runtime/maps/{map,table,group}.go` · `runtime/map.go` | `Map`、`table`、`getWithKeySmall`、`PutSlot`、`grow`、`rehash` | [Ch02](../ch02-slice-map-string/) |
| 迭代器 / 协程 | `iter/iter.go` · `runtime/coro.go` | `Pull`、`coroswitch` | [Ch05](../ch05-iterators/) |
| panic / defer | `runtime/panic.go` | `gopanic`、`gorecover`、`deferreturn` | [Ch06](../ch06-errors-panic/) |
| 分配器 | `runtime/malloc.go` · `malloc_generated.go` · `malloc_stubs.go` · `mcache.go` · `mcentral.go` · `mheap.go` · `mbitmap.go` | `mallocgc`、按尺寸特化的分配函数 | [Ch13](../ch13-alloc-escape-compiler/) |
| size class | `internal/runtime/gc/sizeclasses.go` | size class 表 | [Ch13](../ch13-alloc-escape-compiler/) |
| 逃逸 / 内联 / 去虚拟化 | `cmd/compile/internal/escape/` · `inline/inl.go` · `devirtualize/` | 逃逸图、内联预算、PGO 驱动去虚拟化 | [Ch13](../ch13-alloc-escape-compiler/) |
| 边界检查消除 | `cmd/compile/internal/ssa/prove.go` | prove pass | [Ch13](../ch13-alloc-escape-compiler/) · [Ch16](../ch16-zero-alloc-dod/) |
| 泛型实现 | `cmd/compile/internal/noder/` · `types2/infer.go` | 字典 + GC shape、类型推断 | [Ch04](../ch04-generics/) |
| GC 主流程 | `runtime/mgc.go` · `mgcmark.go` · `mgcsweep.go` | `gcStart`、`gcBgMarkWorker`、`gcDrain`、`gcAssistAlloc` | [Ch14](../ch14-gc/) |
| Green Tea | `runtime/mgcmark_greenteagc.go` | `tryDeferToSpanScan`、`scanSpan` | [Ch14](../ch14-gc/) |
| 写屏障 | `runtime/mbarrier.go` · `runtime/mwbbuf.go` | 混合写屏障、写屏障缓冲 | [Ch14](../ch14-gc/) |
| GC 步调 | `runtime/mgcpacer.go` | `setGCPercent`、`setMemoryLimit` | [Ch14](../ch14-gc/) |
| finalizer / cleanup / 弱引用 | `runtime/mfinal.go` · `runtime/mcleanup.go` · `weak/pointer.go` · `unique/handle.go` | `AddCleanup`、`weak.Pointer`、`unique.Handle` | [Ch14](../ch14-gc/) |
| 执行追踪 | `runtime/trace/flightrecorder.go` | `NewFlightRecorder` | [Ch15](../ch15-profiling-bench/) |
| 可移植 SIMD（实验） | `simd/`、`simd/archsimd/` | `Int8s`、`Float32s`…（`GOEXPERIMENT=simd`） | [Ch16](../ch16-zero-alloc-dod/) |
| netpoller | `runtime/netpoll.go` · `netpoll_epoll.go` · `netpoll_kqueue.go` · `netpoll_windows.go` | `pollDesc`、`netpollblock`、`netpollready`、`netpoll` | [Ch17](../ch17-netpoll-io/) |
| fd 封装 | `internal/poll/fd_unix.go` · `fd_windows.go` · `fd_poll_runtime.go` · `net/fd_posix.go` | 读写、deadline | [Ch17](../ch17-netpoll-io/) |
| HTTP | `net/http/server.go` · `net/http/transport.go` | `conn.serve`、连接池 | [Ch20](../ch20-http-rpc/) |
| JSON v2 | `encoding/json/v2/` · `encoding/json/jsontext/` | `Marshal`/`Unmarshal` + `Options`、`Encoder`/`Decoder` | [Ch19](../ch19-serialization/) |
| UUID | `uuid/uuid.go` | `New`（=`NewV4`）、`NewV7`、`Parse` | [Ch19](../ch19-serialization/) |
| database/sql | `database/sql/sql.go` | 连接池 | [Ch21](../ch21-storage-cache/) |
| cgo | `runtime/cgocall.go` · `cmd/cgo/` | `cgocall` | [Ch24](../ch24-runtime-boundaries/) |

---

## JDK 25

### 类库（`$JAVA_HOME/lib/src.zip`）

| 主题 | 路径（`java.base/` 下，另注明的除外） | 章节 |
|---|---|---|
| 紧凑源文件的 IO | `java/lang/IO.java` | [Ch00](../ch00-setup/) |
| 字符串 | `java/lang/String.java` · `StringLatin1.java` · `StringUTF16.java` | [Ch02](../ch02-slice-map-string/) |
| 集合 | `java/util/ArrayList.java` · `HashMap.java` · `SequencedCollection.java` · `concurrent/ConcurrentHashMap.java` | [Ch02](../ch02-slice-map-string/) |
| Stream / Gatherers | `java/util/Spliterator.java` · `stream/AbstractPipeline.java` · `stream/Gatherer.java` · `stream/Gatherers.java` | [Ch05](../ch05-iterators/) |
| 虚拟线程 | `java/lang/VirtualThread.java` · `jdk/internal/vm/Continuation.java` · `java/lang/Thread.java` | [Ch07](../ch07-scheduler/) |
| 调度器 | `java/util/concurrent/ForkJoinPool.java` | [Ch07](../ch07-scheduler/) |
| 阻塞队列 | `java/util/concurrent/{ArrayBlockingQueue,LinkedBlockingQueue,SynchronousQueue,LinkedTransferQueue}.java` | [Ch08](../ch08-channels-select/) |
| 异步组合 | `java/util/concurrent/CompletableFuture.java` | [Ch08](../ch08-channels-select/) |
| 锁与 AQS | `java/util/concurrent/locks/{AbstractQueuedSynchronizer,ReentrantLock,StampedLock}.java` | [Ch09](../ch09-sync-memory-model/) |
| 内存访问模式 | `java/lang/invoke/VarHandle.java` | [Ch09](../ch09-sync-memory-model/) |
| 分段计数 | `java/util/concurrent/atomic/{LongAdder,Striped64}.java` | [Ch09](../ch09-sync-memory-model/) |
| 稳定值（JEP 502，preview） | `java/lang/StableValue.java` | [Ch09](../ch09-sync-memory-model/) |
| 结构化并发（JEP 505，preview） | `java/util/concurrent/StructuredTaskScope.java` · `StructuredTaskScopeImpl.java` | [Ch10](../ch10-context-structured/) |
| 作用域值（JEP 506） | `java/lang/ScopedValue.java` · 对照 `java/lang/ThreadLocal.java` | [Ch10](../ch10-context-structured/) |
| 定时调度 | `java/util/concurrent/ScheduledThreadPoolExecutor.java` | [Ch11](../ch11-actor/) |
| 清理器 | `java/lang/ref/Cleaner.java` | [Ch14](../ch14-gc/) |
| FFM（JEP 454） | `java/lang/foreign/{MemorySegment,Arena,Linker}.java` | [Ch16](../ch16-zero-alloc-dod/) · [Ch24](../ch24-runtime-boundaries/) |
| Unsafe（被 FFM 取代） | `jdk/internal/misc/Unsafe.java` | [Ch16](../ch16-zero-alloc-dod/) |
| Vector API（JEP 508，孵化） | `jdk.incubator.vector/jdk/incubator/vector/{Vector,FloatVector}.java` | [Ch16](../ch16-zero-alloc-dod/) |
| 虚拟线程的 I/O 托管 | `sun/nio/ch/{Poller,PollerProvider,DefaultPollerProvider}.java` + 平台实现（Windows：`WEPollPoller.java`） | [Ch17](../ch17-netpoll-io/) |
| HTTP 客户端 | `java.net.http/java/net/http/HttpClient.java` | [Ch20](../ch20-http-rpc/) |
| JFR API | `jdk.jfr/jdk/jfr/Recording.java` | [Ch15](../ch15-profiling-bench/) |
| Class-File API（JEP 484） | `java/lang/classfile/ClassFile.java` | [Ch24](../ch24-runtime-boundaries/) |
| Instrumentation | `java.instrument/java/lang/instrument/Instrumentation.java` | [Ch24](../ch24-runtime-boundaries/) |

### HotSpot（`openjdk/jdk25u`，目录级）

| 主题 | 目录 | 章节 |
|---|---|---|
| 对象头 / markWord / 紧凑对象头 | `src/hotspot/share/oops/` | [Ch01](../ch01-values-memory/) · [Ch14](../ch14-gc/) |
| Continuation / 虚拟线程支持 | `src/hotspot/share/runtime/` | [Ch07](../ch07-scheduler/) |
| C2 编译器（逃逸分析、内联） | `src/hotspot/share/opto/` | [Ch13](../ch13-alloc-escape-compiler/) |
| G1 / ZGC / Shenandoah | `src/hotspot/share/gc/g1/` · `gc/z/` · `gc/shenandoah/` | [Ch14](../ch14-gc/) |
| JFR | `src/hotspot/share/jfr/` | [Ch15](../ch15-profiling-bench/) |
| AOT 缓存 / CDS | `src/hotspot/share/cds/` | [Ch13](../ch13-alloc-escape-compiler/) · [Ch23](../ch23-build-observability/) |
