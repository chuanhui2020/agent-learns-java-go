# 源码地图：Go 1.27 × JDK 25

> 每章「源码阅读清单」的总索引。Go 的文件路径和函数名都已在 go1.27.3 源码中逐一核实；JDK 类路径已在 JDK 25 的 `lib/src.zip` 中核实。
> 读法见 [methodology.md 第五节](methodology.md#五源码阅读方法)：**带着问题读，画图做笔记，不从头读到尾。**

## 怎么打开源码

```bash
# Go：工具链自带完整源码
go env GOROOT                      # → $GOROOT/src/...
go doc -src runtime.gopark         # 直接打印某个函数的源码
go doc -src sync.Mutex.Lock

# Java：类库源码在 src.zip（按模块分目录，如 java.base/java/lang/VirtualThread.java）
java -XshowSettings:properties -version 2>&1 | grep java.home   # → $JAVA_HOME/lib/src.zip
unzip -l "$JAVA_HOME/lib/src.zip" | grep LambdaMetafactory     # 按类名找路径
# HotSpot（C++）：github.com/openjdk/jdk25u，切到与 `java -version` 构建号对应的 tag
```

> 注意：`src.zip` 只包含**当前平台**的实现类。Windows 版 JDK 里能看到 `sun/nio/ch/WEPollPoller.java`；Linux 的 `EPollPoller` 等要去 openjdk 仓库的 `src/java.base/linux/classes/` 下看。

---

## Go 1.27（`$GOROOT/src`）

### Part 1 · 语言内核

| 主题 | 文件 | 关键函数/类型 | 章节 |
|---|---|---|---|
| 运行时总约定 | `runtime/HACKING.md` | g/m/p、systemstack、写屏障约束 | 全部 |
| 类型同一性 / 可赋值 / 可转换 | `cmd/compile/internal/types2/api_predicates.go` · `operand.go` · `conversions.go` | `Identical`、`(*operand).assignableTo`、`(*operand).convertibleTo` | [Ch01](../ch01-types-constants/) |
| 常量与任意精度运算 | `go/constant/value.go` · `cmd/compile/internal/types2/const.go` | `constant.Value`、常量可表示性检查 | [Ch01](../ch01-types-constants/) |
| 结构体布局 | `cmd/compile/internal/types/size.go` | `CalcSize`、`CalcStructSize` | [Ch02](../ch02-values-memory/) |
| 类型元数据 | `internal/abi/type.go` | `Type`（`Size_`、`PtrBytes`、`Kind_`…）、`StructType`、`UncommonType` | [Ch02](../ch02-values-memory/) · [Ch09](../ch09-reflect-unsafe/) |
| slice | `runtime/slice.go` | `growslice` | [Ch03](../ch03-slice-map-string/) |
| string | `runtime/string.go` | `slicebytetostring` 等转换 | [Ch03](../ch03-slice-map-string/) |
| map（Swiss table） | `internal/runtime/maps/{map,table,group}.go` · `runtime/map.go` | `Map`、`table`、`getWithKeySmall`、`PutSlot`、`grow`、`rehash` | [Ch03](../ch03-slice-map-string/) |
| 函数值与闭包 | `runtime/runtime2.go` · `cmd/compile/internal/escape/escape.go` | `funcval`、`(*batch).flowClosure`（捕获变量按值/按引用） | [Ch04](../ch04-functions-closures/) |
| 包初始化 | `runtime/proc.go` · `cmd/compile/internal/pkginit/` | `doInit`、`doInit1`、`initTask` | [Ch04](../ch04-functions-closures/) |
| 接口表示与断言 | `runtime/runtime2.go` · `runtime/iface.go` | `iface`、`eface`、`itab`、`getitab`、`typeAssert` | [Ch05](../ch05-interfaces/) |
| 泛型实现 | `cmd/compile/internal/noder/` · `types2/infer.go` | 字典 + GC shape、类型推断 | [Ch06](../ch06-generics/) |
| 迭代器 / 协程 | `iter/iter.go` · `runtime/coro.go` | `Pull`、`coroswitch` | [Ch07](../ch07-iterators/) |
| panic / defer | `runtime/panic.go` | `gopanic`、`gorecover`、`deferreturn` | [Ch08](../ch08-errors-panic/) |
| 反射 | `reflect/type.go` · `reflect/value.go` | `rtype`、`Value`（`typ_`/`ptr`/`flag`）、`Value.Call` | [Ch09](../ch09-reflect-unsafe/) |
| unsafe | `unsafe/unsafe.go` | `Pointer` 的合法模式（文档注释）、`Add`/`Slice`/`String`/`StringData`/`SliceData` | [Ch09](../ch09-reflect-unsafe/) |
| 反射驱动的编解码（案例） | `encoding/json/v2/` · `encoding/json/jsontext/` · `encoding/json/internal/` | `Marshal`/`Unmarshal` + `Options`、`Encoder`/`Decoder` | [Ch09](../ch09-reflect-unsafe/) |

### Part 2 · 并发模型与运行时

| 主题 | 文件 | 关键函数/类型 | 章节 |
|---|---|---|---|
| 调度器数据结构 | `runtime/runtime2.go` | `g`、`m`、`p`、`schedt`、`sudog` | [Ch10](../ch10-scheduler/) |
| 调度循环 | `runtime/proc.go` | `schedule`、`findRunnable`、`runqput`/`runqget`/`runqsteal`、`newproc1` | [Ch10](../ch10-scheduler/) |
| 阻塞与唤醒 | `runtime/proc.go` | `gopark`、`goready` | [Ch10](../ch10-scheduler/) · [Ch11](../ch11-channels-select/) |
| 系统调用与抢占 | `runtime/proc.go` · `runtime/preempt.go` | `entersyscall`/`exitsyscall`、`handoffp`、`sysmon`、`retake`、`preemptone` | [Ch10](../ch10-scheduler/) · [Ch19](../ch19-syscall-cgo-ffi/) |
| 容器感知 GOMAXPROCS | `runtime/cgroup_linux.go` · `internal/runtime/cgroup/` | `defaultGOMAXPROCSInit`、`defaultGOMAXPROCS`（GODEBUG `updatemaxprocs`） | [Ch10](../ch10-scheduler/) |
| goroutine 栈 | `runtime/stack.go` | `newstack`、`copystack`、`shrinkstack` | [Ch10](../ch10-scheduler/) |
| channel | `runtime/chan.go` | `hchan`、`chansend`、`chanrecv`、`closechan` | [Ch11](../ch11-channels-select/) |
| select | `runtime/select.go` | `selectgo` | [Ch11](../ch11-channels-select/) |
| 运行时定时器 | `runtime/time.go` | `timer`、`timers`（每 P 一个堆，`addHeap`/`deleteMin`） | [Ch11](../ch11-channels-select/) |
| 信号量 | `runtime/sema.go` | `semacquire1`、等待者树 | [Ch12](../ch12-sync-memory-model/) |
| Mutex | `internal/sync/mutex.go`（`sync.Mutex` 是它的包装） | `Lock`、`lockSlow`、`starvationThresholdNs` | [Ch12](../ch12-sync-memory-model/) |
| 其他同步原语 | `sync/rwmutex.go` · `sync/waitgroup.go` · `sync/once.go` · `sync/oncefunc.go` · `sync/cond.go` | `WaitGroup.Go`、`OnceValue` | [Ch12](../ch12-sync-memory-model/) |
| sync.Map | `sync/map.go` → `internal/sync/hashtriemap.go` | `HashTrieMap[K,V]` | [Ch12](../ch12-sync-memory-model/) |
| sync.Pool | `sync/pool.go` · `sync/poolqueue.go` | `Get`、`pinSlow`、`poolCleanup`、`poolChain` | [Ch12](../ch12-sync-memory-model/) |
| 原子类型 | `sync/atomic/type.go` | `Int64`、`Pointer[T]` | [Ch12](../ch12-sync-memory-model/) |
| 运行时内部锁 | `runtime/lock_spinbit.go` | `lock2` | [Ch12](../ch12-sync-memory-model/) |
| context | `context/context.go` | `cancelCtx`、`propagateCancel`、`AfterFunc` | [Ch13](../ch13-context-structured/) |
| race detector | `runtime/race.go` · `runtime/race/` · `cmd/compile/internal/ssagen/ssa.go` | `raceinit`、`raceread`/`racewrite`、编译器插桩 `(*state).instrument` | [Ch14](../ch14-race-leak-detection/) |
| goroutine 泄漏检测 | `runtime/mgc.go` · `runtime/mprof.go` | `goroutineLeakGC`、`goroutineLeakProfileWithLabels` | [Ch14](../ch14-race-leak-detection/) |
| synctest 气泡 | `testing/synctest/` · `internal/synctest/` · `runtime/synctest.go` | `synctestRun`、气泡关联 | [Ch14](../ch14-race-leak-detection/) |

### Part 3 · 内存管理与编译器

| 主题 | 文件 | 关键函数/类型 | 章节 |
|---|---|---|---|
| 分配器 | `runtime/malloc.go` · `malloc_generated.go` · `malloc_stubs.go` · `mcache.go` · `mcentral.go` · `mheap.go` · `mbitmap.go` | `mallocgc`、按尺寸特化的分配函数 | [Ch15](../ch15-alloc-escape-compiler/) |
| size class | `internal/runtime/gc/sizeclasses.go` | size class 表 | [Ch15](../ch15-alloc-escape-compiler/) |
| 逃逸 / 内联 / 去虚拟化 | `cmd/compile/internal/escape/` · `inline/inl.go` · `devirtualize/` | 逃逸图、内联预算、PGO 驱动去虚拟化 | [Ch15](../ch15-alloc-escape-compiler/) |
| 边界检查消除 | `cmd/compile/internal/ssa/prove.go` | prove pass | [Ch15](../ch15-alloc-escape-compiler/) |
| 编译器 intrinsic | `cmd/compile/internal/ssagen/intrinsics.go` | intrinsic 注册表 | [Ch15](../ch15-alloc-escape-compiler/) |
| 可移植 SIMD（实验） | `simd/`、`simd/archsimd/` | 需 `GOEXPERIMENT=simd` | [Ch15](../ch15-alloc-escape-compiler/) |
| GC 主流程 | `runtime/mgc.go` · `mgcmark.go` · `mgcsweep.go` | `gcStart`、`gcBgMarkWorker`、`gcDrain`、`gcAssistAlloc` | [Ch16](../ch16-gc/) |
| Green Tea | `runtime/mgcmark_greenteagc.go` | `tryDeferToSpanScan`、`scanSpan` | [Ch16](../ch16-gc/) |
| 写屏障 | `runtime/mbarrier.go` · `runtime/mwbbuf.go` | 混合写屏障、写屏障缓冲 | [Ch16](../ch16-gc/) |
| GC 步调 | `runtime/mgcpacer.go` | `setGCPercent`、`setMemoryLimit` | [Ch16](../ch16-gc/) |
| finalizer / cleanup / 弱引用 | `runtime/mfinal.go` · `runtime/mcleanup.go` · `weak/pointer.go` · `unique/handle.go` | `AddCleanup`、`weak.Pointer`、`unique.Handle` | [Ch16](../ch16-gc/) |
| CPU profile 采样 | `runtime/cpuprof.go` · `runtime/proc.go` · `runtime/signal_unix.go` · `runtime/os_linux.go` | `sigprof`、`setProcessCPUProfilerTimer`、`setThreadCPUProfiler` | [Ch17](../ch17-profiling-bench/) |
| 内存 / 阻塞 / 互斥 profile | `runtime/mprof.go` · `runtime/pprof/` | `MemProfileRate`、`mProf_Malloc`、`blockevent` | [Ch17](../ch17-profiling-bench/) |
| 基准循环 | `testing/benchmark.go` | `(*B).Loop` | [Ch17](../ch17-profiling-bench/) |
| 执行追踪 | `runtime/trace/flightrecorder.go` | `NewFlightRecorder` | [Ch17](../ch17-profiling-bench/) |

### Part 4 · 运行时与操作系统的边界

| 主题 | 文件 | 关键函数/类型 | 章节 |
|---|---|---|---|
| netpoller | `runtime/netpoll.go` · `netpoll_epoll.go` · `netpoll_kqueue.go` · `netpoll_windows.go` | `pollDesc`、`netpollblock`、`netpollready`、`netpoll` | [Ch18](../ch18-netpoll-io/) |
| fd 封装 | `internal/poll/fd_unix.go` · `fd_windows.go` · `fd_poll_runtime.go` · `net/fd_posix.go` | 读写、deadline | [Ch18](../ch18-netpoll-io/) |
| cgo 调用与回调 | `runtime/cgocall.go` · `cmd/cgo/doc.go` | `cgocall`、`cgocallbackg`；指针传递规则（doc.go） | [Ch19](../ch19-syscall-cgo-ffi/) |
| cgo 指针检查与固定 | `runtime/cgocheck.go` · `runtime/pinner.go` | `Pinner` | [Ch19](../ch19-syscall-cgo-ffi/) |
| 信号 | `runtime/signal_unix.go` · `runtime/preempt.go` | `sighandler`、异步抢占信号 | [Ch19](../ch19-syscall-cgo-ffi/) · [Ch10](../ch10-scheduler/) |
| vDSO | `runtime/vdso_linux.go` | Linux 上 `time.Now` 等的快速路径 | [Ch19](../ch19-syscall-cgo-ffi/) |

---

## JDK 25

### 类库（`$JAVA_HOME/lib/src.zip`）

| 主题 | 路径（`java.base/` 下，另注明的除外） | 章节 |
|---|---|---|
| 紧凑源文件的 IO | `java/lang/IO.java` | [Ch00](../ch00-setup/) |
| 装箱缓存与溢出检测 | `java/lang/Integer.java`（`IntegerCache`）· `java/lang/Math.java`（`addExact` 等） | [Ch01](../ch01-types-constants/) |
| 模式匹配 switch 的引导方法 | `java/lang/runtime/SwitchBootstraps.java` | [Ch01](../ch01-types-constants/) · [Ch05](../ch05-interfaces/) |
| record 的 equals/hashCode/toString 生成 | `java/lang/runtime/ObjectMethods.java` | [Ch02](../ch02-values-memory/) · [Ch05](../ch05-interfaces/) |
| 字符串 | `java/lang/String.java` · `StringLatin1.java` · `StringUTF16.java` | [Ch03](../ch03-slice-map-string/) |
| 集合 | `java/util/ArrayList.java` · `HashMap.java` · `SequencedCollection.java` · `concurrent/ConcurrentHashMap.java` | [Ch03](../ch03-slice-map-string/) |
| lambda 的实现 | `java/lang/invoke/LambdaMetafactory.java` · `InnerClassLambdaMetafactory.java` | [Ch04](../ch04-functions-closures/) |
| Stream / Gatherers | `java/util/Spliterator.java` · `stream/AbstractPipeline.java` · `stream/Gatherer.java` · `stream/Gatherers.java` | [Ch07](../ch07-iterators/) |
| 类加载 | `java/lang/ClassLoader.java` | [Ch09](../ch09-reflect-unsafe/) |
| 反射与方法句柄 | `jdk/internal/reflect/MethodHandleAccessorFactory.java` · `java/lang/invoke/MethodHandles.java`（`Lookup`、`defineHiddenClass`） | [Ch09](../ch09-reflect-unsafe/) |
| Class-File API（JEP 484） | `java/lang/classfile/ClassFile.java` | [Ch09](../ch09-reflect-unsafe/) |
| Instrumentation | `java.instrument/java/lang/instrument/Instrumentation.java` | [Ch09](../ch09-reflect-unsafe/) |
| Unsafe（被 VarHandle / FFM 取代） | `jdk.unsupported/sun/misc/Unsafe.java` · `java.base/jdk/internal/misc/Unsafe.java` | [Ch09](../ch09-reflect-unsafe/) |
| 虚拟线程 | `java/lang/VirtualThread.java` · `jdk/internal/vm/Continuation.java` · `java/lang/Thread.java` | [Ch10](../ch10-scheduler/) |
| 调度器 | `java/util/concurrent/ForkJoinPool.java` | [Ch10](../ch10-scheduler/) |
| 阻塞队列 | `java/util/concurrent/{ArrayBlockingQueue,LinkedBlockingQueue,SynchronousQueue,LinkedTransferQueue}.java` | [Ch11](../ch11-channels-select/) |
| 异步组合 | `java/util/concurrent/CompletableFuture.java` | [Ch11](../ch11-channels-select/) |
| 定时调度 | `java/util/concurrent/ScheduledThreadPoolExecutor.java` | [Ch11](../ch11-channels-select/) |
| 锁与 AQS | `java/util/concurrent/locks/{AbstractQueuedSynchronizer,ReentrantLock,StampedLock}.java` | [Ch12](../ch12-sync-memory-model/) |
| 内存访问模式 | `java/lang/invoke/VarHandle.java` | [Ch12](../ch12-sync-memory-model/) · [Ch09](../ch09-reflect-unsafe/) |
| 分段计数 | `java/util/concurrent/atomic/{LongAdder,Striped64}.java` | [Ch12](../ch12-sync-memory-model/) |
| 稳定值（JEP 502，preview） | `java/lang/StableValue.java` | [Ch12](../ch12-sync-memory-model/) |
| 结构化并发（JEP 505，preview） | `java/util/concurrent/StructuredTaskScope.java` · `StructuredTaskScopeImpl.java` | [Ch13](../ch13-context-structured/) |
| 作用域值（JEP 506） | `java/lang/ScopedValue.java` · 对照 `java/lang/ThreadLocal.java` | [Ch13](../ch13-context-structured/) |
| Vector API（JEP 508，孵化） | `jdk.incubator.vector/jdk/incubator/vector/{Vector,FloatVector}.java` | [Ch15](../ch15-alloc-escape-compiler/) |
| 清理器 | `java/lang/ref/Cleaner.java` | [Ch16](../ch16-gc/) |
| JFR API | `jdk.jfr/jdk/jfr/Recording.java` | [Ch17](../ch17-profiling-bench/) |
| 虚拟线程的 I/O 托管 | `sun/nio/ch/{Poller,PollerProvider,DefaultPollerProvider}.java` + 平台实现（Windows：`WEPollPoller.java`） | [Ch18](../ch18-netpoll-io/) |
| FFM（JEP 454） | `java/lang/foreign/{Linker,Arena,MemorySegment,SymbolLookup}.java` · 实现 `jdk/internal/foreign/abi/` | [Ch19](../ch19-syscall-cgo-ffi/) |

### HotSpot（`openjdk/jdk25u`，目录级）

| 主题 | 目录 | 章节 |
|---|---|---|
| 对象头 / markWord / 紧凑对象头 | `src/hotspot/share/oops/` | [Ch02](../ch02-values-memory/) · [Ch16](../ch16-gc/) |
| 类加载、字段布局 | `src/hotspot/share/classfile/` | [Ch02](../ch02-values-memory/) · [Ch09](../ch09-reflect-unsafe/) |
| Continuation / 虚拟线程支持 | `src/hotspot/share/runtime/` | [Ch10](../ch10-scheduler/) |
| C2 编译器（逃逸分析、内联、SuperWord 向量化） | `src/hotspot/share/opto/` | [Ch15](../ch15-alloc-escape-compiler/) |
| AOT 缓存 / CDS | `src/hotspot/share/cds/` | [Ch15](../ch15-alloc-escape-compiler/) |
| G1 / ZGC / Shenandoah | `src/hotspot/share/gc/g1/` · `gc/z/` · `gc/shenandoah/` | [Ch16](../ch16-gc/) |
| JFR | `src/hotspot/share/jfr/` | [Ch17](../ch17-profiling-bench/) |
| JNI / FFM 的 VM 侧 | `src/hotspot/share/prims/` | [Ch19](../ch19-syscall-cgo-ffi/) |
