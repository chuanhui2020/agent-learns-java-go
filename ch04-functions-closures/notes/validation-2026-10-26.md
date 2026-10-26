# Ch04 维护者验证记录 · 2026-10-26

这份记录用于复核随仓库提供的示例、基准和练习验收条件，不代替学习者的 L3 实验报告。学习时仍应先填写 [预测模板](lab-template.md)，再运行代码。

## 环境与方法

- Windows / amd64，12th Gen Intel Core i7-12700；基准报告的并行度为 20。
- Go `go1.27.3`；`CGO_ENABLED=0`，`GOEXPERIMENT`、`GOFLAGS` 均为空，默认内联优化。
- Java `25.0.5.0.2+1-LTS-2`，64 位 HotSpot；默认 GC / 堆设置，没有额外调优参数。
- 代码为本次 Ch04 示例新增后的工作区状态，尚未提交；原始数据可供复核，不构成对其他版本或平台的性能承诺。
- 闭包基准每组采样 100ms、重复 10 次；这次关注分配行为，没有用 ns/op 宣称性能提升。

```text
go test ./ch04-functions-closures/go/demo ./ch04-functions-closures/go/lab ./ch04-functions-closures/go/initorder/...
go test '-run=^$' '-bench=.' -benchmem -benchtime=100ms -count=10 ./ch04-functions-closures/go/lab
go test '-gcflags=-m=2' ./ch04-functions-closures/go/lab
```

## 闭包分配观察

完整的 60 条基准样本见 [原始输出](closure-bench-windows-amd64.txt)。各组 10 次采样中的分配计数一致：

| 基准 | 场景 | B/op | allocs/op |
|---|---|---:|---:|
| Direct | 直接加法基线 | 0 | 0 |
| Immediate | 立即调用闭包 | 0 | 0 |
| ReturnedLocal | 返回闭包后在局部调用 | 0 | 0 |
| SavedValue | 将按值捕获的闭包保存到包级变量 | 16 | 1 |
| SavedMutable | 将可变捕获的闭包保存到包级变量 | 24 | 2 |
| SavedLarge | 将捕获 64 个 int 的数组的闭包保存到包级变量 | 528 | 2 |

[编译器诊断摘录](closure-escape-windows-amd64.txt)保留了捕获策略、内联与分配结论的原文，并标注筛选规则。观察到：

- `NewAdder` 捕获 `x` 时使用按值策略；`NewCounter` 因修改 `x` 使用按引用策略；`NewReader` 的大数组也使用按引用策略。
- `BenchmarkReturnedLocal` 的 `NewAdder` 调用被内联，调用点的函数字面量不逃逸，与 0 allocs/op 一致。
- 保存到包级变量的闭包环境逃逸；可变捕获和大数组还需要保留其捕获状态。这里的大小与 amd64 的 int 宽度有关。
- `b.Cleanup` 的注册位于计时循环之前；清理只为释放基准留下的全局引用，不是每次操作的工作量。

**有效性限制**：`Direct` / `Immediate` 的计算可能折叠成常量，测得的亚纳秒值主要体现循环和优化，不能当成任意业务函数的调用成本。100ms 样本受调度和 CPU 频率影响；没有做 benchstat 耗时比较，因此不报告加速比。0 allocs/op 表示这个场景中没有观测到每次操作的堆分配，不表示闭包在所有上下文都不分配。

## 语义与初始化

Go demo 的输出由 Example 测试校验：`range :=` 输出 `1 2 3`，复用变量的 `range =` 输出 `3 3 3`；接收者方法值分别保留值快照与指针；含 slice 的值副本仍能观察底层数组修改。

Go 初始化实验使用新的子进程检查完整输出，避免漏掉测试执行前的初始化阶段：

```text
dependency.variable
dependency.init
main.second
main.first
main.init
main.body first=8 second=7
```

`main.first` 在源码中声明得更早，但依赖 `main.second`，因此后初始化。规范上的依赖关系解释了日志，不能把源码行顺序当成唯一依据。

两个 Java demo 通过自校验。`javac --release 25` 不带 preview 也能编译；`javap -c -v -p` 中能观察到 lambda 的 `invokedynamic`、`LambdaMetafactory` 引导方法和捕获参数。

- `CaptureDemo`：修改同一个对象后两种读取都返回 `11`；替换字段后，绑定方法引用返回 `11`，延迟读取字段的 lambda 返回 `20`。
- `InitializationDemo`：读取编译期常量时事件列表为空；主动使用触发字段、静态块，再进入实例构造；构造参数校验发生在父类构造前。
- 故意让静态初始化抛出 `IllegalStateException` 后，首次使用得到 `ExceptionInInitializerError`；再次使用该类得到 `NoClassDefFoundError`。
- `InvalidCapture.java` 与 `InvalidConstruction.java` 分别在捕获非 effectively final 变量、在 `super(...)` 前调用实例方法处编译失败。失败是实验预期，文件没有混入正常示例编译。

## 练习验收条件的验证

仓库中的 Go 练习、`CaptureDrill.java`、`InitializationDrill.java` 保持未完成状态，运行后返回失败。它们可以编译，失败来源是语义断言或预测尚未填写。

为确认测试可以通过，维护验证使用临时目录中的正确实现：Go 通过 `go test -overlay` 替换练习源文件，Java 运行临时副本。三组均通过，随后仍保留仓库中的 TODO 和遮蔽错误供学习者完成，没有把答案写入练习文件。

本轮代码均为单线程语义实验；未运行 `-race`，也未由这些结果推断并发安全。
