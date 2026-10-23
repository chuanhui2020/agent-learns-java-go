# Ch19 · 系统调用、cgo 与 FFM

> Part 4 · 估时 12h · 状态：⚪ 未开始 · 运行时与操作系统、原生代码的边界：一次跨边界调用要付哪些固定成本，原生代码阻塞时谁让出线程，跨边界的内存由谁负责生命周期

## 🎯 目标（资深标准）

- **L1**：能用 `syscall` 直接发一次系统调用；能用 cgo 调 C 函数并遵守指针传递规则（`C.CString`/`C.free`、`runtime.Pinner`、`cgo.Handle`）；会用 `runtime.LockOSThread`。Java 侧能用 FFM 完成 downcall 和 upcall，并为不同场景选对 `Arena`。
- **L2**：讲清一次 cgo 调用的完整往返：`cgocall → entersyscall → asmcgocall`（切到 g0 栈）→ C →`exitsyscall`；C 回调 Go 时走 `crosscall2 → cgocallback → cgocallbackg`。讲清 Linux（直接 `SYSCALL`）、Darwin/OpenBSD（经 libc）、Windows（经 DLL，并且走 `cgocall`）三条系统调用路径。讲清 FFM downcall 的线程状态转换、`Linker.Option.critical`、四种 Arena 的关闭语义，以及虚拟线程在本地调用里什么时候 pin。
- **L3**：先预测再验证。预测各类调用单次开销的数量级（纯 Go、`SyscallN`、cgo、FFM 默认、FFM `critical`）；预测 N 个阻塞调用期间和结束后的线程数；判断一段 cgo 代码会在编译期报错、被 `cgocheck=1` 拦下、只有 `cgocheck2` 能抓到，还是谁都抓不到；预测 FFM 操作抛哪种异常、会不会产生 `jdk.VirtualThreadPinned`。预测错了，要能用机制解释。
- **L4**：能论设计：Go 为什么绕开 libc 自己实现系统调用层，又为什么在部分平台退回系统库；为什么 cgo 每次调用都要 `entersyscall`，而 FFM 允许用 `critical` 跳过状态转换；Java 为什么用 FFM 取代 JNI（JEP 454 Motivation）；两边对不安全操作采取什么默认策略。

## 🔗 前置

- [Ch10](../ch10-scheduler/)：G/M/P、sysmon、`entersyscall`/`retake`/`handoffp` 的基本流程、异步抢占、栈拷贝。**本章不重复这些内容**，只讲系统调用和 cgo 如何与调度器协作。
- [Ch02](../ch02-values-memory/)：`structs.HostLayout`。
- [Ch09](../ch09-reflect-unsafe/)：`unsafe.Pointer` 规则、`sun.misc.Unsafe` 退场。
- [Ch15](../ch15-alloc-escape-compiler/)：逃逸分析，用来解释传给 C 的指针为什么会逃逸。
- [Ch16](../ch16-gc/)：非移动堆、GOMEMLIMIT。
- [Ch18](../ch18-netpoll-io/)：走 netpoller 的 I/O 不占 M；本章讲不走 netpoller 的那一半。

## 💡 关键洞察

1. **跨边界的固定成本花在"记账"和"切栈"上，不在 C 函数本身。** Go 每次 cgo 调用都走 `cgocall`：`entersyscall` 把 G 标成 `_Gsyscall`，切到 g0 栈执行 C，返回后 `exitsyscall` 重新确认 P。Windows 上的每次 `syscall.SyscallN` 也走这条路，即使 `CGO_ENABLED=0`。JVM 这边对应的是线程状态在 Java 与 native 之间来回切换，而 FFM 的 `critical` 选项可以省掉这一步。两边"让 GC 不必等你"的语义同构：Go 的 STW 不等 `_Gsyscall` 状态的 G，HotSpot 的安全点不等处于 native 的线程。
2. **原生代码阻塞时，两边的补偿策略相反。** Go 这边，sysmon 发现 P 被系统调用占住超过 20µs、并且还有活要干，就把 P 抢走交给别的 M。并行度不掉，代价是线程数上涨，而且空闲的 M 不会还给 OS。Java 25 这边，虚拟线程在 downcall 里阻塞时直接占住载体线程，调度器**不补偿**（本机实测）；在 upcall 里 park 则会 pin 住载体，并记一条 `jdk.VirtualThreadPinned`。
3. **跨边界内存的生命周期：Go 靠规则，Java 靠对象。** Go 的堆不移动，所以可以把 Go 指针直接交给 C，前提是 GC 知道它在哪。规则是：调用期间隐式 pin；越过调用期要用 `runtime.Pinner`；C 不能保留 string 和 slice。`cgocheck` 在运行时做抽查。HotSpot 的 GC 会移动对象，所以 JNI 只给句柄，FFM 默认禁止把堆段当地址传出去。堆外内存的生命周期绑定在 `Arena` 对象上，每次访问都检查存活、所属线程和边界。这些检查是 API 语义的一部分，不是调试开关。

## ☕ Java 25 深度复习

### JNI：老路径与它的问题
- **调用模型**：`native` 方法加 `System.loadLibrary`，函数表定义在 `$JAVA_HOME/include/jni.h`。进入 native 方法时，HotSpot 把线程状态从 in-Java 切到 in-native。**安全点不等待处于 native 的线程**；线程返回 Java 时要检查安全点和挂起请求。VM 侧实现在 `src/hotspot/share/prims/`。
- **引用而不是地址**：native 代码拿到的是 `jobject` 句柄。GC 会移动对象，句柄就是那层间接。局部引用在 native 方法返回时释放，VM 至少保证能创建 16 个，需要更多时用 `EnsureLocalCapacity` 或 `PushLocalFrame`。需要跨调用保存的，用 `NewGlobalRef` / `NewWeakGlobalRef`，忘记 `DeleteGlobalRef` 就是泄漏。
- **`GetPrimitiveArrayCritical`**：拿到数组的直接指针，临界区内不能调用其他 JNI 函数，也不能阻塞。G1 从 JEP 423 起只钉住对象所在的 region，不再在临界区期间禁止 GC。FFM 里对应的是 `Linker.Option.critical(true)`。
- **诊断与授权**：`-Xcheck:jni` 对应 `CheckJNICalls`，本机默认 `false`。JDK 25 中 `System.loadLibrary` 也属于受限方法（JEP 472）：本机实测，未授权调用时会打印 `WARNING: A restricted method in java.lang.System has been called`。

### FFM（JEP 454，JDK 22 正式）
- **核心类型**（`java.base/java/lang/foreign/`）：
  - `Linker`：`nativeLinker()`、`downcallHandle`、`upcallStub`、`defaultLookup()`、`canonicalLayouts()`。C 的 `long` 在本机 Windows 上是 `i4`，Linux x64 上是 8 字节，所以 C 类型的宽度应该从 `canonicalLayouts()` 取，不要写死。
  - `SymbolLookup`（`libraryLookup(name|path, arena)`、`loaderLookup()`）、`FunctionDescriptor`、`MemoryLayout`/`ValueLayout`/`StructLayout`、`MemorySegment`、`Arena`、`SegmentAllocator`。
- **downcall 的实现**：
  - 入口在 `jdk/internal/foreign/abi/DowncallLinker.java`。各平台的 `CallArranger`（`x64/sysv`、`x64/windows`、`aarch64/*` 等）用 `Binding` 和 `CallingSequence` 把参数映射到寄存器或栈，再由 `NativeEntryPoint.makeDowncallStub` 让 VM 生成调用桩。
  - downcall 句柄本身是 MethodHandle，可以被 JIT 内联到调用点。
  - 调用期间，Javadoc 对每个地址参数段保证三件事：作用域存活、能被当前线程访问、在调用期间保持存活。比如共享 arena 的段正在被 downcall 使用时去关闭这个 arena，会抛 `IllegalStateException`。
- **`Linker.Option`**：
  - `firstVariadicArg`：声明从第几个参数开始是变参。
  - `captureCallState("errno" | "GetLastError" | "WSAGetLastError")`：在调用桩里紧接着读出线程局部的错误码。本机 Windows 上 `captureStateLayout()` 实测为 `[i4(GetLastError)i4(WSAGetLastError)i4(errno)]`。
  - `critical(boolean allowHeapAccess)`：声明目标函数运行时间极短、不会回调 Java。实现上 `CallingSequence.needsTransition()` 返回 `!isCritical()`，也就是**省掉线程状态转换**。`allowHeapAccess=true` 时，可以把堆段当地址传入，拿到的是只在本次调用期间有效的临时地址。Javadoc 写明，错用的后果是"性能下降或 JVM 崩溃"。
- **upcall**：`upcallStub(MethodHandle, FunctionDescriptor, Arena)` 返回一个函数指针段，它的生命周期绑定在这个 arena 上。目标方法抛出异常时 JVM 会直接终止，所以要自己用 `MethodHandles.catchException` 兜底。实现见 `abi/UpcallLinker.java`。
- **四种 Arena**（实现在 `jdk/internal/foreign/` 下的 `ConfinedSession`、`SharedSession`、`ImplicitSession`、`GlobalSession`）：

  | Arena | 生命周期 | 能否显式关闭 | 多线程访问 |
  |---|---|---|---|
  | `global()` | 不限 | 否（`close()` 抛 `UnsupportedOperationException`，实测） | 可以 |
  | `ofAuto()` | 由 GC 决定 | 否（同上） | 可以 |
  | `ofConfined()` | 手动 | 可以，只能由所属线程关闭 | 否，其他线程访问或关闭都抛 `WrongThreadException`（实测） |
  | `ofShared()` | 手动 | 可以，任何线程都能关闭 | 可以 |

  - **共享 arena 关闭更贵**：`SharedSession.justClose` 会调用 `ScopedMemoryAccess.closeScope0`，与所有线程做线程本地握手，确认没有正在进行的访问。
  - **只有 `ofAuto()` 计入 `-XX:MaxDirectMemorySize`**：`ArenaImpl` 中 `shouldReserveMemory = session instanceof ImplicitSession`。原因是它和 DirectByteBuffer 一样要靠 GC 触发释放。
- **受限方法与 native access**：
  - 被标注 `@Restricted` 的有：`Linker.downcallHandle`/`upcallStub`、`SymbolLookup.libraryLookup`、`MemorySegment.reinterpret`、`AddressLayout.withTargetLayout`。
  - JDK 25 默认 `--illegal-native-access=warn`（`java --help` 可见），每个模块第一次调用时打印警告。`--enable-native-access=ALL-UNNAMED`（或模块名）可以消除警告；`--illegal-native-access=deny` 时调用抛 `IllegalCallerException`（本机实测）。
  - 另外实测到一点：只给其他模块授权时，未授权的模块仍然只是收到警告，不会抛异常。
- **jextract**：独立的第三方工具，从 C 头文件生成绑定代码。JEP 454 在 Non-Goals 里明确不把这类工具放进 JDK。
- **虚拟线程与本地调用**（本机实测，`-Djdk.virtualThreadScheduler.parallelism=1`）：
  - 在 downcall 里阻塞（kernel32 `Sleep(500)`）：载体线程被占住，另一个虚拟线程要等约 504ms 才能运行，没有补偿，也**没有** `jdk.VirtualThreadPinned` 事件。
  - 在 upcall 里调用 `Thread.sleep`：`VirtualThread.parkNanos` 卸载失败，转入 `parkOnCarrierThread`，再调用 `postPinnedEvent("LockSupport.park")`。JFR 记到的事件里 `pinnedReason = "Native or VM frame on stack"`（`default.jfc` 中该事件的阈值是 20 ms）。
  - 根因是 `Continuation.Pinned.NATIVE`：本地帧无法冻结到堆上。JNI 的 native 帧是同一回事。

## 🐹 Go 1.27 深入

### 规范层（与工具契约）
- **语言规范里没有 cgo。** cgo 是 `cmd/cgo` 工具和运行时之间的契约，一手文档是 `cmd/cgo/doc.go`（`go doc cmd/cgo`）。
  - 紧挨着 `import "C"` 的注释是 preamble；`#cgo CFLAGS/LDFLAGS/pkg-config` 指令用来设置编译和链接参数。
  - 类型映射：`C.int`、`C.long` 等数值类型；`void*` ↔ `unsafe.Pointer`；union 映射为等长的字节数组；用 `C.struct_xxx` 访问结构体，用 `C.sizeof_T` 取大小。C 类型会被翻译成**未导出**的 Go 类型，所以不要在导出 API 里暴露它们。
  - 拷贝式转换：`C.CString`/`C.CBytes` 在 C 堆上 malloc，由调用方负责 free；`C.GoString`/`GoStringN`/`GoBytes` 把数据拷回 Go。
  - 其他约定：`n, err := C.f()` 可以同时取回 errno；不能直接调用 C 函数指针和变参函数；`C.malloc` 永远不返回 nil，内存不足时直接让程序崩溃。
- **指针传递规则**（doc.go「Passing pointers」一节，按原文逐条）：
  1. "Go 指针"指向 Go 分配的内存。它是内存的动态属性，与指针的类型无关。interface、chan、map、func 类型的非零值总是包含 Go 指针。
  2. 传给 C 的 Go 指针必须指向 pinned 内存。作为函数参数传入的指针，所指内存在调用期间**隐式 pin**。
  3. 可以把 Go 指针传给 C，前提是它指向的内存里**不含指向未 pin 内存的 Go 指针**。指向 struct 字段时只考虑这个字段；指向数组或 slice 元素时考虑**整个**数组或底层数组。
  4. 调用返回后，C 不能保留 Go 指针，除非这块内存已用 `runtime.Pinner` pin 住且尚未 Unpin。由此可以推出：C 不能保留 string、slice、chan，因为它们无法被 pin；`_GoString_` 也不能保留。
  5. 被 C 调用的 Go 函数可以返回指向 pinned 内存的 Go 指针（所以不能返回 string 或 slice）；不能通过 C 指针写入指向未 pin 内存的 Go 指针。
  6. **检查机制**：
     - 默认 `GODEBUG=cgocheck=1`，只做廉价的参数检查；`cgocheck=0` 完全关闭。
     - 完整检查要在**构建期**打开 `GOEXPERIMENT=cgocheck2`。
     - 运行时设 `GODEBUG=cgocheck=2` 会在启动时直接报 `fatal error: cgocheck > 1 mode is no longer supported at runtime. Use GOEXPERIMENT=cgocheck2 at build time instead.`（本机实测）。
  7. 需要把 Go 值长期交给 C 持有时，用 `runtime/cgo.Handle`。它是一个整数句柄，零值无效，可以在 C API 里当哨兵值。
- **`#cgo noescape` / `#cgo nocallback`**（doc.go「Optimizing calls of C code」）：
  - `noescape` 告诉编译器 Go 指针不会经过这个 C 函数逃逸，于是不必把参数强制分配到堆上。
  - `nocallback` 声明这个 C 函数不会回调 Go；如果实际回调了，会 panic `runtime: function marked with #cgo nocallback called back into Go`。
  - **实现细节**：`cmd/cgo/out.go` 只有在两个指令**同时**出现时，才把参数保活方式从 `_Cgo_use` 换成 `_Cgo_keepalive`（后者不强制逃逸）。原因是回调可能触发栈增长并移动栈，单独的 noescape 并不安全。
  - 这两个指令都**不会**省掉 `entersyscall`。
- **`runtime.Pinner`**（`Pin` / `Unpin`）：
  - 参数必须是指针。`Pin(&local)` 会让局部变量逃逸到堆上（`-gcflags=-m` 实测输出 `moved to heap`）。
  - 同一个对象可以 pin 多次，必须 unpin 同样次数。
  - Pinner 不可达时如果还 pin 着对象，终结器会 panic：`runtime.Pinner: found leaking pinned pointer; forgot to call Unpin()?`（本机实测）。
- **`runtime.LockOSThread`**（`go doc`）：
  - 按嵌套计数，Unlock 次数与 Lock 相等才解绑。
  - 在 `init` 里调用，会让 `main` 跑在启动线程上。
  - **goroutine 未解锁就退出时，线程被终止**：`gdestroy` 让线程回到 `mstart` 退出，而不是放回线程池，注释的理由是"可能被置于不寻常的内核状态"（plan9 例外）。
  - 本机 Windows 实测：锁定的 G 经过 1000 次 `Gosched` 始终在同一个线程 ID 上；它未解锁退出后，后续 200 个 goroutine 都没再见到这个线程 ID。
- **标准库 `syscall`**：包文档说它主要供 `os`、`time`、`net` 使用，新代码优先用 `golang.org/x/sys`。
  - `Syscall`/`Syscall6` 前后有 `entersyscall`/`exitsyscall`；`RawSyscall` 不通知调度器。生成代码里，`//sysnb` 标注的调用（如 Linux 的 `Getpid`）走 raw 路径。
  - 这些函数都带 `//go:uintptrkeepalive` 和 `//go:nosplit`：`uintptr` 参数可能其实是指针，所以调用期间栈不能移动。

### 运行时与编译器层
- **Go → OS 的三条路径**：
  - **Linux、FreeBSD 等**：`syscall.Syscall` → `runtime.entersyscall` → `RawSyscall6` → `internal/runtime/syscall/linux` 里的汇编执行 `SYSCALL` 指令 → `exitsyscall`。不经过 libc，也不切栈。
  - **Darwin、OpenBSD**：经 libc 调用。`runtime/sys_libc.go` 的 `libcCall` 用 `asmcgocall` 切到 g0 栈执行 libc 函数；`syscall_syscalln` 在外面包一层 entersyscall/exitsyscall。Solaris、illumos、AIX 同样经 libc（`asmsysvicall6`）。
  - **Windows**：`syscall.SyscallN` → `runtime.syscall_syscalln`（`runtime/syscall_windows.go`）→ **`cgocall(asmstdcallAddr, …)`**。本机实测每次 `SyscallN` 都让 `runtime.NumCgoCall()` 加 1。运行时内部调用则用 `os_windows.go` 的 `stdcall`：直接 `asmcgocall`，不 entersyscall。
- **系统调用怎样与调度器协作**（基础见 [Ch10](../ch10-scheduler/)）：
  - **进入**：`reentersyscall` 保存 pc/sp（GC 从 `syscallsp` 开始扫描这个栈），把 `stackguard0` 设为 `stackPreempt` 并置 `throwsplit`（禁止栈增长），然后 CAS `_Grunning → _Gsyscall`。此时 **P 仍挂在 M 上**，同时记录在 `m.oldp`。
  - **快路径**：返回时 P 还在，CAS 回 `_Grunning` 就继续执行。所以短系统调用几乎不碰调度器。
  - **sysmon 抢 P**：`retake` 先用 `setBlockOnExitSyscall` 拿到 G 的 scan 位，把它暂时卡在系统调用里，再 `takeP` + `handoffp`。抢的条件是：P 在系统调用里超过一个 sysmon tick（至少 20µs），并且本地队列非空，或者没有自旋 M 和空闲 P，或者已经超过 10ms。
  - **慢路径**：`exitsyscallTryGetP` 先抢回旧 P，再找空闲 P；都拿不到时 `exitsyscallNoP` 把 G 放进全局队列，M 执行 `stopm`（如果 G 锁定了这个线程，就 `stoplockedm`）。
  - **`entersyscallblock`**：用于已知会阻塞的调用（运行时内部的 futex/sema 睡眠、部分平台的 libc 调用），进入时直接交出 P。
  - **STW 不等系统调用**：`entersyscallHandleGCWait` 会主动交出 P；STW 遇到系统调用中的 P，用 `gcstopP` 收走。
  - **不发抢占信号**：`preemptone` 对 `_Gsyscall` 状态的 G 直接返回（注释："Don't bother trying to preempt a goroutine in a syscall"）。处于系统调用或 cgo 中的 G 不会收到 SIGURG，运行时的回收手段是拿走 P。
- **cgocall 路径**（`runtime/cgocall.go` 的文件头注释是最好的地图）：
  1. `_Cfunc_f` 调用 `runtime.cgocall`：`entersyscall` → `osPreemptExtEnter` → `mp.incgo = true`。
  2. `asmcgocall(fn, arg)`（`runtime/asm_amd64.s`）切到 m->g0 栈，按 C ABI 对齐后调用 cgo 生成的 C 包装函数。g0 栈由操作系统分配，注释的说法是 "safe to run gcc-compiled code on"。
  3. C 函数返回后，切回 curg 栈，执行 `exitsyscall`。
  - **为什么要切栈**：goroutine 栈初始只有 2KB，靠 Go 编译器在函数序言里插入的检查来扩容，gcc 编译的代码没有这些检查；而且 Go 栈会被 `copystack` 整体移动。
  - **为什么每次都 entersyscall**：运行时无法知道 C 函数会不会阻塞，只能按"可能阻塞"记账，这样 sysmon 才能在需要时拿走 P，STW 也不用等它。
  - **开 cgo 后的线程创建**：`newm1` 改用 `_cgo_thread_start`（pthread）创建 M，而不是 `newosproc`，让 C 库看到的是标准的 pthread 线程。
- **C → Go 回调**（`//export`）：
  1. C 调用 cgo 生成的 `GoF` → `crosscall2`（`runtime/cgo/asm_amd64.s`，把 C ABI 转换为 Go ABI）→ `runtime.cgocallback`（从 g0 切回 curg 栈）。
  2. `cgocallbackg` 依次执行：
     - `lockOSThread`：C 帧在这个 M 的 g0 栈上，回调期间必须留在这个 M 上；
     - `exitsyscall`：**可能阻塞，直到拿到 P**；
     - 检查 nocallback 标记，违反则 panic；
     - `cgocallbackg1` 执行 Go 函数；
     - `reentersyscall`：恢复成"系统调用中"的状态，返回 C。
  3. 如果调用来自 C 自己创建的线程，先用 `needm` 从 extra M 链表借一个 M。链表上始终保留一个备用；在 pthread 平台上，要等这个线程退出时才 `dropm` 归还。按 `checkmcount` 的注释，extra M 不计入 `SetMaxThreads` 上限。
  - **结论：回调比下行调用贵**，多出来的是拿 P、锁线程，可能还要借 M。
- **线程上限**：每个阻塞在 C 里的调用占住一个 M。`sched.maxmcount` 默认 10000，超过后打印 `runtime: program exceeds 10000-thread limit` 并 `fatal error: thread exhaustion`。本机 Windows 实测：200 个 goroutine 阻塞在 `SyscallN(Sleep)` 上，`/sched/threads/total:threads` 从 5 涨到 203，`/sched/goroutines/not-in-go:goroutines` 为 200；全部返回后线程数**仍是 203**，空闲的 M 被保留下来。
- **vDSO**（`runtime/vdso_linux.go`、`vdso_linux_amd64.go`）：
  - 启动时 `vdsoauxv` 读取 auxv 中的 `AT_SYSINFO_EHDR`，解析内核映射进来的 ELF，查找 `__vdso_clock_gettime`、`__vdso_gettimeofday`、`__vdso_getrandom` 三个符号。
  - 使用方：`time·now`（`time_linux_amd64.s`）、`nanotime1`（`sys_linux_amd64.s`）直接调用这些函数，`vgetrandom_linux.go` 使用 vDSO 版的 getrandom。
  - 这些调用**不进内核，也不走 entersyscall**；但仍要切到 g0 栈，因为"不知道 vDSO 代码要多少栈"。同时会记下 `m.vdsoPC/vdsoSP`，供 SIGPROF 回溯使用。
- **信号**：
  - **安装与入口**：`initsig` 安装处理器，入口是 `sigtrampgo` → `sighandler`（`runtime/signal_unix.go`）。处理器跑在 gsignal 的备用信号栈上；Linux 的 `setsig` 使用 `SA_SIGINFO|SA_ONSTACK|SA_RESTORER|SA_RESTART`。
  - **异步抢占**：`sigPreempt = _SIGURG`，源码注释列出了选择标准：调试器默认透传、libc 内部不使用、偶尔误收到也无害、不依赖 macOS 没有的实时信号。`preemptM` 调用 `signalM`；`doSigPreempt` 在 `wantAsyncPreempt` 和 `isAsyncSafePoint` 都满足时注入 `asyncPreempt`。Windows 上用 SuspendThread 实现，`osPreemptExtEnter` 在进入外部代码前防止与 SuspendThread 死锁。
  - **`os/signal` 投递路径**：信号处理器里不能分配内存、不能加锁、不能阻塞，所以 `sigsend` 只在 `sig.mask` 里置一位；`os/signal` 的 `loop` goroutine 通过 `signal_recv` 取出信号，再由 `process` 分发到用户的 channel。因此用户看到的信号处理发生在普通 goroutine 里，是异步的，同一种信号多次到达可能合并成一次。
  - **与 C 共存**（`go doc os/signal` 的「Go programs that use cgo or SWIG」一节）：
    - C 代码安装信号处理器时必须带 `SA_ONSTACK`，否则可能崩溃；
    - 同步信号（SIGBUS/SIGFPE/SIGSEGV）发生在 Go 代码里时，C 的处理器要转交给 Go 的处理器，否则 nil 解引用等运行时 panic 不会出现；
    - C 不应修改 Go 线程的信号屏蔽字；
    - 非 Go 线程上收到的信号，由 `sigfwdgo` 转发给原来的处理器；
    - Linux 部分架构上开 cgo 时，Go 通过 libc 的 `sigaction` 安装处理器（`runtime/cgo_sigaction.go`，主要是为了让 sanitizer 能拦截）。

### 源码阅读清单（`$GOROOT/src`）
| 文件 | 函数 / 类型 | 带着什么问题读 |
|---|---|---|
| `cmd/cgo/doc.go` | 「Passing pointers」「Optimizing calls of C code」，文件后半部分的「Implementation details」 | 每条规则在防什么：GC 漏扫、栈移动，还是给将来的移动 GC 留后路？`_Cfunc_puts` 的生成代码长什么样？ |
| `cmd/cgo/out.go` | 生成 `_Cfunc_*` 的代码（`_Cgo_no_callback`、`_Cgo_use` / `_Cgo_keepalive`） | 为什么 noescape 要配合 nocallback 才生效？ |
| `runtime/cgocall.go` | 文件头注释、`cgocall`、`cgocallbackg`、`cgocallbackg1`、`cgoCheckPointer`、`cgoCheckArg` | Go→C、C→Go 各经历几次栈切换和调度器状态变化？`cgocheck=1` 检查了什么、漏掉了什么？ |
| `runtime/asm_amd64.s` · `runtime/cgo/asm_amd64.s` | `asmcgocall`、`cgocallback`；`crosscall2` | 切到 g0 时保存了什么？C ABI 的 callee-saved 寄存器由谁负责？ |
| `runtime/cgocheck.go` · `runtime/mbarrier.go` · `cmd/compile/internal/ssa/writebarrier.go` | `cgoCheckPtrWrite`、`cgoCheckMemmove`；各处的 `CgoCheck2` 分支 | 为什么 cgocheck2 只能在构建期打开？（提示：编译器要在每个指针写入点插桩） |
| `runtime/pinner.go` | `Pinner.Pin`、`Unpin`、`setPinned`、`isPinned`、`pinnerLeakPanic` | pin 状态记在哪里（mspan 的 pinnerBits）？多次 pin 怎么计数？ |
| `runtime/proc.go` | `reentersyscall`、`entersyscallblock`、`exitsyscall`、`exitsyscallTryGetP`、`exitsyscallNoP`、`retake`、`setBlockOnExitSyscall`、`preemptone` | 1.27 中 P 在系统调用期间留在 M 上，sysmon 怎样安全地把它拿走？ |
| `runtime/proc.go` | `LockOSThread`、`gdestroy`、`needm`、`dropm`、`newm1`、`startTemplateThread`、`checkmcount` | 锁定线程的 G 退出时为什么要杀掉线程？C 线程回调 Go 时 M 从哪里来？ |
| `syscall/syscall_linux.go` · `syscall/dll_windows.go` · `runtime/syscall_windows.go` · `runtime/sys_libc.go` | `Syscall`/`RawSyscall6`；`SyscallN`/`syscall_syscalln`；`libcCall` | 三类平台的系统调用分别在哪里切栈、在哪里通知调度器？ |
| `runtime/vdso_linux.go` · `runtime/time_linux_amd64.s` | `vdsoauxv`、`vdsoParseSymbols`、`time·now` | `time.Now` 为什么不需要 entersyscall，却仍要切到 g0？ |
| `runtime/signal_unix.go` · `runtime/sigqueue.go` · `os/signal/signal_unix.go` | `sigtrampgo`、`sighandler`、`sigPreempt` 的注释、`doSigPreempt`、`sigfwdgo`；`sigsend`/`signal_recv`；`loop` | 为什么选 SIGURG？一个信号从内核到你的 channel 经过了哪几层？ |

## 📊 Java 25 ↔ Go 1.27 机制级对照表

| 机制 | Java 25（HotSpot） | Go 1.27 | 取舍 / 设计动机 |
|---|---|---|---|
| 跨边界调用桩 | FFM：VM 按 `FunctionDescriptor` 现场生成桩，句柄是可内联的 MethodHandle；JNI：native 方法加 C 实现 | `_Cfunc_f` → `cgocall` → `asmcgocall`（切 g0）→ gcc 编译的 C 包装函数 | Java 不需要 C 编译器；Go 的胶水代码由 gcc 编译，所以能直接用 C 头文件里的宏和 struct 布局 |
| 每次调用的记账 | 线程状态在 Java 与 native 之间来回切换；`critical` 可以省掉 | 每次都 `entersyscall`/`exitsyscall`，没有跳过的选项（`nocallback` 只省回调准备） | JVM 允许用户声明"极短且不回调"来换性能，错用后果自负；Go 统一按"可能阻塞"记账，保证 sysmon 和 STW 总能处理卡住的调用 |
| 原生代码里阻塞 | 虚拟线程占住载体，调度器不补偿（实测）；平台线程不受影响 | sysmon `retake` → `handoffp`，另起 M；上限 10000，空闲 M 不还给 OS | Go 保住并行度，代价是线程数；Java 保住线程数，代价是并行度。P 让 Go"交出运行资格"变得很便宜 |
| GC 与跨边界的线程 | 安全点不等处于 native 的线程，返回 Java 时检查安全点 | STW 不等 `_Gsyscall` 的 G（`gcstopP` 收走 P），GC 从 `syscallsp` 开始扫栈 | 两边同构：跨边界的线程被视为已经停在安全点，回来时必须重新入场 |
| 把托管堆内存交给原生代码 | JNI 给句柄；FFM 默认禁止把堆段当地址（`Heap segment not allowed`，实测），`critical(true)` 或 `GetPrimitiveArrayCritical` 只给临时地址 | 直接传 Go 指针，调用期间隐式 pin；被传对象会逃逸到堆 | HotSpot 的 GC 会移动对象，只能给句柄或临时地址；Go 堆不移动，只有栈会移动，所以对象放到堆上就能直接交给 C |
| 越过调用期的引用 | `NewGlobalRef`；FFM 没有 pin 堆对象的 API，长期共享的数据应放进 Arena | `runtime.Pinner`（string/slice 不能 pin）、`cgo.Handle` 整数句柄 | pin 位记在 mspan 的 pinnerBits 里，cgo 规则是按"pinned"来表述的，给将来的移动 GC 留了余地（设计文档 12416） |
| 原生内存的生命周期 | `Arena` 四种模式，每次访问都检查存活、所属线程和边界 | `C.malloc`/`C.free` 手工管理，Go 侧不做任何检查 | FFM 的 Goals 之一是"no use-after-free"；Go 把原生内存完全交给 C 的规矩 |
| 原生内存与 GC 记账 | 不计入 `-Xmx`；只有 `ofAuto` 计入 `MaxDirectMemorySize` | 不计入 GOMEMLIMIT（`SetMemoryLimit` 文档明确列出 "memory allocated by C code"） | 两边的 GC 都看不到原生内存；只有依赖 GC 来释放的那部分（`ofAuto`）需要额外的上限来触发回收 |
| 回调 | `upcallStub` 绑定在 Arena 上；目标方法抛异常会让 JVM 终止；栈上有 native 帧，在里面 park 会 pin 载体 | `//export` → `crosscall2` → `cgocallbackg`（lockOSThread、exitsyscall 等待 P）；非 Go 线程要借 extra M | 回调都比下行调用贵：要重新进入托管世界，托管栈和原生栈交错在一起 |
| 错误码 | `captureCallState("errno"/"GetLastError"/"WSAGetLastError")` | `n, err := C.f()`；`syscall.Errno` | errno 是线程局部的，必须在同一线程上紧接着调用读取，所以两边都把它做进了调用桩 |
| 线程亲和 | 没有把虚拟线程钉在某个载体上的 API，需要固定线程就用平台线程 | `LockOSThread`；未解锁就退出时线程被销毁 | Go 的 G 默认在 M 之间迁移，必须有显式的绑定原语；Java 平台线程本来就是 1:1 |
| 安全检查与授权 | 受限方法加 `--enable-native-access`/`--illegal-native-access`（默认 warn）；Arena 检查常开；`-Xcheck:jni` | 默认 `cgocheck=1` 做廉价检查，`GOEXPERIMENT=cgocheck2` 在构建期打开完整检查；没有授权机制 | Java 追求 integrity by default，并在逐步收紧；Go 把检查当作调试辅助，安全承诺只覆盖不用 unsafe/cgo 的程序 |
| 构建影响 | 不需要 C 编译器；jextract 可选 | 需要 C 工具链；交叉编译或找不到 C 编译器时默认 `CGO_ENABLED=0`（本机即如此）；开 cgo 后动态链接 libc | 纯 Go 的静态二进制和免费交叉编译是 Go 的核心优势，cgo 会直接拿掉这两样 |
| 信号 | Linux 上 HotSpot 自己也使用信号，提供信号链（`UseSignalChaining` 默认 true；Linux 版 JDK 带 `libjsig`） | Go 安装全部信号处理器，SIGURG 用于抢占；C 的处理器必须带 `SA_ONSTACK` | 两个运行时都要"占用"信号，与 C 库共存只能靠约定 |

## 🔬 实验

### 实验 1：单次跨边界调用的固定开销（必做）
- **目的**：给"一次跨边界调用要付多少"一个数量级，并把开销对应到具体路径上。
- **方法**：
  - Go A（本机 Windows 即可，不需要 gcc）：`//go:noinline` 的纯 Go 函数 vs `syscall.SyscallN(GetCurrentThreadId)`。后者只读 TEB，几乎全部开销都花在 cgocall 路径上。
  - Go B（WSL2 / Linux，需要 gcc）：cgo 调用空函数 `static int add(int a, int b)`；加上 `#cgo noescape add` + `#cgo nocallback add` 再测一次；另外对比 `syscall.Syscall(SYS_GETPID)`、`syscall.Getpid()`（`//sysnb`，走 raw 路径）、`time.Now()`（vDSO）。
  - Java：用 JMH（[Ch17](../ch17-profiling-bench/)）对比纯 Java 方法、FFM downcall 调空 C 函数或 `strlen`、同一函数加 `Linker.Option.critical(false)` 的版本，JNI 可选。
- **工具**：`go test -bench -count=10` + benchstat；JMH `-f 2`，带 `--enable-native-access=ALL-UNNAMED`。
- **预期现象**：`SyscallN` 与 cgo 空调用处于同一量级（走的是同一条 cgocall 路径），比纯 Go 调用高一到两个数量级；noescape+nocallback 只减少逃逸分配，ns/op 变化不大；`time.Now` 远低于真正的系统调用；FFM `critical` 应低于默认 downcall（待实测）。
- **本机参考**（i7-12700 / Windows 11 / go1.27.3，`count=3`，未做 benchstat，只作量级锚点）：纯 Go 调用 ≈ 1.8 ns/op，`SyscallN(GetCurrentThreadId)` ≈ 67 ns/op。
- **记录**：各路径的 ns/op 和 allocs/op、平台、`CGO_ENABLED`；再算一次"批量传 1000 个元素"摊薄后的 ns/元素。

### 实验 2：阻塞调用、线程数与载体线程（必做；与 [Ch10](../ch10-scheduler/) 实验 3 互补）
- **目的**：区分三种情况：短系统调用（P 不被拿走）、长时间阻塞调用（P 被 retake，M 增长）、park（不占 M）；同时看 Java 侧对应的行为。
- **方法**：
  - Go：N = 200 个 goroutine，分别做：
    - (a) 长阻塞：Windows 用 `SyscallN(Sleep, 1000)`，Linux 用 cgo `usleep(1000000)` 或 `syscall.Syscall(SYS_NANOSLEEP, …)`；
    - (b) `time.Sleep(time.Second)`；
    - (c) 每个 goroutine 连续做 10 万次微秒级的短系统调用。
  - Go 侧观测：调用前、调用中、全部返回后，各读一次 `/sched/threads/total:threads` 和 `/sched/goroutines/not-in-go:goroutines`。Linux 上再加 `GODEBUG=schedtrace=1000`，看 `threads=`、`idlethreads=` 和每个 P 的 `syscalltick`。
  - Go 变体：让一半 goroutine 先 `LockOSThread` 再阻塞，退出时不解锁。
  - Java：`-Djdk.virtualThreadScheduler.parallelism=1`，一个虚拟线程在 FFM downcall 里阻塞 500ms，另一个虚拟线程记录自己实际被调度的时刻；再改成在 upcall 里 `Thread.sleep`，录 JFR（`-XX:StartFlightRecording`），用 `jfr print --events jdk.VirtualThreadPinned` 查看。
- **预期现象**：
  - (a) 线程数约为 N 加基线，not-in-go = N，返回后线程数不回落。本机 Windows 已实测：5 → 203 → 203。
  - (b) 线程数几乎不变。
  - (c) 线程数基本不涨：调用在一个 sysmon tick 内返回，走的是 `exitsyscall` 快路径。
  - LockOSThread 变体：这些线程在 goroutine 退出时被销毁，线程数应回落约 N/2（待实测）。
  - Java（本机已实测）：downcall 版本中，另一个虚拟线程约 504ms 后才运行，没有 pinned 事件；upcall 版本产生 `jdk.VirtualThreadPinned`，`pinnedReason = "Native or VM frame on stack"`、`blockingOperation = "LockSupport.park"`。
- **记录**：三种情况的线程数曲线、schedtrace 片段、JFR 事件原文；用 `retake` 的条件（20µs、本地队列是否为空、10ms）解释 (c)。

### 实验 3：触发 cgocheck（需要 cgo，WSL2 / Linux）
- **目的**：弄清每条指针规则分别由哪一层负责检查，以及哪些违规谁都抓不到。
- **方法**：每个用例先写下预测：编译期报错 / `cgocheck=1` panic / 只有 `cgocheck2` 能抓到 / 都抓不到，然后再运行。
  1. `type Node struct{ next *Node; v C.int }`，`next` 指向另一个 Go 对象，把 `&n` 传给 C 函数。预期 panic，消息中含 `argument of cgo function has Go pointer to unpinned Go pointer`（来自 `cgoFormatErr`）。用 `GODEBUG=cgocheck=0` 重跑应不再报错（但规则仍然被违反）；用 Pinner pin 住 `n.next` 后应通过。
  2. 传 `&xs[0]`：`xs` 是 `[]*int` 时，整个底层数组都会被检查，应同样 panic；`xs` 是 `[]int32` 时没问题。
  3. 在 Go 代码里把 Go 指针写进 `C.malloc` 分配的内存：`*(*unsafe.Pointer)(cmem) = unsafe.Pointer(&x)`。默认不报错；用 `GOEXPERIMENT=cgocheck2` 构建后，应报 `fatal error: unpinned Go pointer stored into non-Go memory`（来自 `cgoCheckPtrWrite`）。
  4. 一个会回调 Go 的 C 函数，却被标了 `#cgo nocallback`。预期 panic。
  5. 对同一个"把局部变量地址传给 C"的函数，分别不加指令、只加 `noescape`、两个都加，各跑一次 `go build -gcflags=-m`，记录这个局部变量是否 `moved to heap`。
- **记录**：预测表与实际结果对照；对每条规则，写出它防的是哪种失败（GC 漏扫、栈移动、悬挂指针）。

### 实验 4：Arena 语义与 FFM 边界（Java，本机 Windows 可做）
- **方法**：依次做以下操作，每一步先写预测再运行：
  - 在其他线程访问 confined arena 的段，以及在其他线程关闭它；
  - 关闭 arena 之后再访问它分配的段；
  - 把堆段直接当地址传给 `strlen`；
  - 同样的调用改用 `critical(true)`；
  - 用 `captureCallState("errno")` 调用 `strtol` 并让它溢出；
  - 加 `--illegal-native-access=deny` 运行；
  - 用 JMH 对比 shared arena 和 confined arena 的 `close()` 耗时。
- **预期现象**（本机 JDK 25.0.5 已实测前六项）：`WrongThreadException` ×2、`IllegalStateException`、`IllegalArgumentException: Heap segment not allowed`、`critical(true)` 正常返回 2、`errno = 34`（ERANGE）、`IllegalCallerException`。shared 的 `close()` 应明显更贵，因为要做线程握手（待实测）。

## 🧪 练习规划

**Go：`go/exercises`**（`go test ./ch19-syscall-cgo-ffi/go/...`；标注"需要 cgo"的文件带 `//go:build cgo`，在 WSL2 中用 `CGO_ENABLED=1` 运行）
1. `func ThreadID() uint64`（`tid_linux.go` 用 `syscall.Gettid`，`tid_windows.go` 用 `SyscallN(GetCurrentThreadId)`）和 `func StaysOnThread(n int) bool`。测试覆盖三点：
   - `LockOSThread` 之后经过 n 次 `Gosched`，线程 ID 不变；
   - 锁定的 G 未解锁就退出后，它的线程 ID 不再出现在随后 200 个 goroutine 中；
   - 嵌套 Lock 两次、Unlock 一次时，仍然保持绑定。
   OS 可能复用线程 ID，所以第二点只做概率性断言。
2. `func RawGetpid() int`：直接发系统调用。Linux 用 `syscall.Syscall(syscall.SYS_GETPID, 0, 0, 0)`，Windows 用 `SyscallN(GetCurrentProcessId)`。测试断言结果与 `os.Getpid()` 相等，并断言 `runtime.NumCgoCall()` 的增量：Windows 上不小于调用次数，Linux 上为 0。先写预测，再跑。
3. `func PeakThreads(n int, block func()) uint64`：并发执行 n 个 `block`，期间采样 `/sched/threads/total:threads` 的峰值。表驱动覆盖三种 `block`：长阻塞系统调用（峰值 ≥ n）、`time.Sleep`（远小于 n）、短系统调用（远小于 n）。
4. （需要 cgo）`func SumC(xs []int32) int64`：把 `&xs[0]` 传给 C 求和。`func TotalLen(bufs [][]byte) int`：在 Go 内存里构造一个 C struct 数组（每个元素是 `{ptr, len}`，指向各个 buf），用 `runtime.Pinner` pin 住每个 buf，再把数组交给 C 统计总长度。测试覆盖：
   - 两个函数的结果都正确；
   - 去掉 Pinner 的版本会触发 cgocheck panic，用 `recover` 捕获并断言；
   - `defer p.Unpin()` 没有遗漏。
5. （需要 cgo）`//export goVisit` 加 C 侧的 `walk(uintptr_t h, int n)`：用 `cgo.Handle` 把一个 Go 闭包交给 C，由 C 回调 n 次。测试断言回调次数正确、`h.Delete()` 之后不再泄漏。benchmark 比较"每个元素回调一次"和"一次传入整个数组"的 ns/元素，用 `cgocallbackg` 的路径解释差距。

**Java：`java/drills`**（`bin/jrun --enable-native-access=ALL-UNNAMED ch19-syscall-cgo-ffi/java/drills/Xxx.java`）
1. `FfmBasicsDrill.java`：
   - 通过 `defaultLookup()` 找到 `strlen` 并做 downcall；
   - 从 `canonicalLayouts()` 取 `long` 和 `size_t` 的布局，不写死宽度；
   - 用 `captureCallState("errno")` 调用 `strtol` 并让它溢出。
   自校验：长度正确、errno 为 34、堆段在不加 `critical` 时抛 `IllegalArgumentException`、加 `critical(true)` 后可以调用。
2. `ArenaLifecycleDrill.java`：四种 Arena 各分配一次，自校验以下行为：
   - confined arena 被其他线程访问或关闭时抛 `WrongThreadException`；
   - 关闭后访问抛 `IllegalStateException`；
   - shared arena 可以由其他线程关闭；
   - `global()` 和 `ofAuto()` 调用 `close()` 抛 `UnsupportedOperationException`；
   - upcall stub 的 `scope().isAlive()` 随所属 arena 的关闭变为 false。
3. `NativePinDrill.java`：用 `-Djdk.virtualThreadScheduler.parallelism=1` 运行，用 `jdk.jfr.consumer.RecordingStream` 订阅 `jdk.VirtualThreadPinned`。自校验：downcall 里阻塞不产生事件，upcall 里 `Thread.sleep(30)` 产生事件，且 `pinnedReason` 含 "Native"。注释里写清这两种情况对载体线程的影响有什么不同。

## 🧭 设计取舍

- **Go 为什么自己实现系统调用层。** Linux 承诺系统调用 ABI 稳定，所以 Go 可以不依赖 libc，直接执行 `SYSCALL`。这换来三样东西：纯 Go 程序是静态二进制；交叉编译不需要 C 工具链；运行时完全掌控线程创建（`clone`）、栈和信号，不必迁就 libc 的假设。代价是 libc 替你做的事要自己重做。例子：Linux 内核里 `setuid` 只作用于单个线程，所以 `syscall.Setuid` 在没有 cgo 时用 `AllThreadsSyscall` 逐个线程执行；链接了 cgo 时改走 libc（`cgo_libc_setuid`），而 `AllThreadsSyscall` 在 cgo 二进制里直接返回 `ENOTSUP`。在系统调用 ABI 不对外承诺的平台上（Darwin、OpenBSD 经 libc，Windows 只能经 DLL），Go 退回系统库，而且同样要用 `asmcgocall` 切栈。Java 从来没有这个选择：JVM 本身就是链接 libc 的 C++ 程序，JDK 的本地库也都通过 libc 访问 OS。
- **"cgo is not Go"（Go Proverbs）：成本来自哪里。** 一次调用要切到 g0 栈，要 `entersyscall`/`exitsyscall` 记账，传入的参数会逃逸到堆，还有 `cgocheck`。回调时还要拿 P、锁线程，可能要借 extra M。此外是信号共存的约定，以及构建侧失去静态二进制和免费交叉编译。Go 不提供类似 FFM `critical` 的"跳过记账"选项，原因是：一个持有 P 的 M 如果卡在 C 里，会同时卡住 GOMAXPROCS 份额和 STW，而运行时没有任何手段把它拉回来。`noescape`/`nocallback` 只开放了**不影响调度正确性**的那部分优化。Java 的赌注相反：允许用户声明"极短且不回调"，错了就承担 GC 停顿甚至崩溃。
- **为什么 goroutine 栈与 C 栈不兼容。** goroutine 能做到便宜，靠的是 2KB 起步、按需拷贝扩容的栈：编译器在每个函数序言里插入栈检查，栈可以整体移动。gcc 编译的代码既没有栈检查，又假设栈有 MB 级大小并且地址固定。因此每次调用 C 都必须切到 OS 分配的 g0 栈；指向 Go 栈的指针不能交给 C，被传出的对象要挪到堆上。这也是 `noescape` 必须配合 `nocallback` 的原因：回调可能触发栈拷贝。被否决的替代方案是让 goroutine 跑在大栈上，那等于放弃廉价的 goroutine。Loom 撞上的是同一堵墙：本地帧无法冻结到堆上，所以虚拟线程遇到 native 帧只能 pin（`Continuation.Pinned.NATIVE`）。两边都没有办法让"托管栈可移动"和"原生帧地址固定"兼容，Go 的做法是每次调用都切栈，Java 的做法是 pin 住载体。
- **Java 为什么用 FFM 取代 JNI**（JEP 454 Motivation / Goals / Non-Goals）：
  - JNI 的问题：一个函数要维护 `native` 方法、生成的 C 头文件、C 实现三份制品，跨多套工具链同步；它不调和 Java 与 C 的类型系统；开发者为了堆外内存常常退回 `Unsafe`，用 `long` 传地址，JEP 原话是 "tragically unsafe"。
  - 现有堆外方案的问题：ByteBuffer 单块不能超过 2GB，而且只能等 GC 释放；Unsafe 给了开发者"太多控制"，乱序释放会留下悬挂指针。
  - FFM 的回应：纯 Java API；性能目标是不低于 JNI 和 Unsafe；Soundness 目标是即使跨线程分配和释放也不会出现 use-after-free，Arena 加每次访问的检查就是为此设计的；Integrity 目标是不安全操作允许使用，但默认要发出警告。Non-Goals 明确：不改 JNI，不在 FFM 上重建 Unsafe，不把 jextract 放进 JDK。
  - 默认禁止把堆段当地址传出，是移动式 GC 的直接后果；Go 的堆不移动，所以没有这个限制。
- **两边对不安全操作的默认策略。** Go 的态度是：违反规则的程序 "likely to fail in unexpected and unpredictable ways"（doc.go 原话）。`cgocheck=1` 默认开启但只做廉价检查，完整检查要在构建期用 cgocheck2 打开，没有授权机制，cgo 只要找到 C 编译器就默认启用。也就是说，内存安全承诺只覆盖不使用 unsafe 和 cgo 的程序，检查是调试辅助。Java 的态度是 integrity by default：受限方法目前打印警告，未来版本将默认拒绝（警告原文是 "Restricted methods will be blocked in a future release"），现在就可以用 `--illegal-native-access=deny` 提前收紧；Arena 的存活、线程、边界检查始终开启，因为平台把内存安全当作对所有代码的承诺，越过这条线必须显式授权。

## ✅ 验收答辩题

- [ ] [L2] 画出一次 cgo 调用 Go→C→Go 的完整路径，标出每一步在哪个栈上执行、G 处于什么状态、P 归谁。为什么必须切到 g0？为什么每次都要 entersyscall？
- [ ] [L2] C 回调 Go 时，`cgocallbackg` 为什么要 `lockOSThread`？为什么其中的 `exitsyscall` 可能阻塞？C 自己创建的线程回调 Go 时，M 从哪里来？
- [ ] [L2] 逐条复述 cgo 指针传递规则，并说明每条在防什么。为什么 C 不能保留 slice，却可以保留被 Pinner pin 住的 `*T`？
- [ ] [L2] Linux、Darwin、Windows 上一次 `syscall` 调用分别走哪条路径？为什么 Windows 上的 `syscall.SyscallN` 会让 `runtime.NumCgoCall()` 增加？`time.Now` 在 Linux 上为什么不进内核？
- [ ] [L2] `LockOSThread` 的嵌套计数、在 `init` 中调用、goroutine 未解锁就退出，三种情况各会怎样？运行时为什么选择杀掉线程，而不是回收复用？
- [ ] [L2] FFM 四种 Arena 的生命周期和线程约束分别是什么？shared arena 的 `close()` 为什么比 confined 贵？只有哪一种计入 `MaxDirectMemorySize`，为什么？
- [ ] [L2] 虚拟线程在 FFM downcall 中阻塞，和在 upcall 里 `Thread.sleep`，分别会发生什么？哪一种会产生 `jdk.VirtualThreadPinned`？从 `parkOnCarrierThread` 解释。
- [ ] [L3] 先预测、再拿实验 1 的数据：纯 Go 调用、`SyscallN`、cgo 空调用、加 noescape+nocallback 的 cgo、FFM 默认、FFM `critical` 六者的 ns/op 排序和数量级。偏差用机制解释。
- [ ] [L3] 先预测：200 个 goroutine 阻塞在 C 或系统调用里 1 秒，线程数的峰值和结束后的值各是多少？换成 `time.Sleep`、换成大量短系统调用呢？用 `retake` 的条件解释实验 2 的数据。
- [ ] [L3] 拿实验 3 的五个用例，先判断各自属于"编译期报错 / cgocheck=1 panic / 只有 cgocheck2 能抓到 / 都抓不到"中的哪一类，再运行验证；同时预测 `-gcflags=-m` 在三种指令组合下的逃逸结论。
- [ ] [L4] Go 为什么在 Linux 上绕开 libc，在 Darwin/OpenBSD/Windows 上又走系统库？这个选择换来了什么，又要付出什么（用 `Setuid`/`AllThreadsSyscall` 举例）？
- [ ] [L4] 如果要给 cgo 加一个类似 FFM `critical` 的选项（跳过 entersyscall），它需要哪些前提？会破坏 sysmon、STW、抢占的哪些假设？对照 JEP 454 的设计说明 Java 为什么敢提供这个选项。
- [ ] [L4] 读 JEP 454 的 Motivation 与 Non-Goals：JNI 的问题有哪些，FFM 分别怎样解决？为什么 FFM 默认禁止把堆段当地址传出，而 Go 可以直接传 Go 指针？
- [ ] [L4] 比较 Go 的 cgocheck（默认只做廉价检查）和 Java 的 integrity by default（受限方法、native access）：它们各自服务于什么样的安全承诺？

## 🕳️ 常见坑

- **以为 `#cgo noescape` 单独就能避免参数逃逸**：只有与 `nocallback` 同时出现才生效。而且两者都不会省掉 entersyscall。
- **为了"更快"到处用 `RawSyscall`**：它不通知调度器，调用阻塞时 P 和 M 一起被占住，sysmon 拿不回这个 P。
- **把 Go 指针藏进 `uintptr` 来绕过 cgocheck**：检查是绕过了，规则仍然被违反；GC 和栈移动照样会让 C 拿到失效的地址。
- **`runtime.Pinner` 忘记 `Unpin`**：Pinner 不可达时，终结器会 panic。另外，`Pin(&local)` 会让局部变量逃逸到堆。
- **`LockOSThread` 后修改了线程状态**（命名空间、调度优先级、信号屏蔽字）又调用 `UnlockOSThread`：等于把一个"脏"线程还给了调度器。正确做法是保持锁定直到 goroutine 退出，让运行时销毁这个线程。
- **C 库安装信号处理器时没带 `SA_ONSTACK`**，或者修改了 Go 线程的信号屏蔽字。
- **以为 cgo 默认就是开的**：交叉编译时，或者找不到 C 编译器时，默认 `CGO_ENABLED=0`，含 `import "C"` 的文件会被构建约束直接排除（本机 Windows 就是这种情况）。
- **把 confined arena 的段交给别的线程**（包括线程池里的虚拟线程），结果抛 `WrongThreadException`。
- **upcall 目标方法抛异常**：JVM 会直接终止。
- **在会阻塞或会回调的函数上用 `critical`**。
- **以为虚拟线程在阻塞的 downcall 里会被补偿**：不会，载体线程直接被占住。
- **把 C `long` 写死成 `JAVA_LONG`**：Windows 上 C 的 `long` 是 4 字节，应该从 `canonicalLayouts()` 取。

## 📚 一手资料

- Go：
  - 工具与包文档：`go doc cmd/cgo`（`cmd/cgo/doc.go`）、`runtime/cgocall.go` 文件头注释、`go doc runtime.Pinner`、`go doc runtime.LockOSThread`、`go doc runtime/cgo.Handle`、`go doc syscall`、`go doc os/signal`（「Go programs that use cgo or SWIG」一节）
  - 设计文档与提案：[Proposal: Rules for passing pointers between Go and C](https://go.googlesource.com/proposal/+/master/design/12416-cgo-pointers.md)（Ian Lance Taylor）、[#46787 runtime: provide Pinner API for object pinning](https://github.com/golang/go/issues/46787)、[Proposal: Non-cooperative goroutine preemption](https://go.googlesource.com/proposal/+/master/design/24543-non-cooperative-preemption.md)（Austin Clements，含「Choosing a signal」一节）
  - Go Proverbs："Cgo is not Go"
- Java：
  - JEP：[JEP 454 FFM API](https://openjdk.org/jeps/454)、[JEP 472 Prepare to Restrict the Use of JNI](https://openjdk.org/jeps/472)、[JEP 423 Region Pinning for G1](https://openjdk.org/jeps/423)、[JEP 444 Virtual Threads](https://openjdk.org/jeps/444)
  - src.zip：`java.base/java/lang/foreign/`；`jdk/internal/foreign/` 下的 `ArenaImpl`、`SharedSession`、`abi/DowncallLinker`、`abi/UpcallLinker`、`abi/CallingSequence`；`jdk/internal/misc/ScopedMemoryAccess.java`；`java/lang/VirtualThread.java`（`parkOnCarrierThread`）；`jdk/internal/vm/Continuation.java`（`Pinned`）
  - 其他：`$JAVA_HOME/include/jni.h`、JNI Specification（Java SE 25）、HotSpot `src/hotspot/share/prims/`、jextract（github.com/openjdk/jextract）
