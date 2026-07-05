# 学习方法论

## 心态转变（开始前先内化，能少走一半弯路）

这些是 Java 老手最容易"用 Java 思维写 Go"翻车的地方。每写一段 Go，回来对一下这 8 条：

1. **没有异常，错误是值。** 到处 `if err != nil`，这是特性不是缺陷。别用 `panic` 模拟 `try/catch`。
2. **组合优于继承，而且根本没有继承。** 没有 `extends`、类层次、方法重写。用结构体嵌入 + 接口。
3. **接口是隐式实现的（结构化类型）。** 类型不需要 `implements`，方法签名匹配就自动满足接口。
4. **零值可用（zero value）是一等设计。** 变量默认不是 null 崩溃，而是有意义的零值（`0`/`""`/`nil`/空 struct）。很多类型"开箱即用"。
5. **没有构造函数/重载/默认参数/泛型协变。** 用工厂函数 `NewXxx()` + 选项模式（functional options）替代。
6. **可见性靠大小写。** 首字母大写 = 导出（public），小写 = 包私有。没有 `public/private`。
7. **并发是语言级公民。** `go f()` 起一个 goroutine，配 channel。和你熟悉的 Pekko actor 模型有奇妙呼应。
8. **简单是刻意的。** Go 故意不给继承、泛型元编程、注解、AOP。习惯后你会发现团队代码高度一致、可读性极强。

---

## 每章的三段式结构

每个主题都用这个结构展开，顺带复习 Java 25：

> **Java 25 里我怎么做 → Go 里怎么做 → 差异与取舍**

- **Java 25 里我怎么做**：先用你已经会的东西锚定问题（这一步也是复习 Java 25 新特性）。
- **Go 里怎么做**：看同一个问题 Go 换了什么写法/机制。
- **差异与取舍**：什么时候 Go 更优、什么时候 Java 更优、游戏服场景怎么选。不迷信任何一方。

---

## 每章 README 模板

带教时每章按这个骨架填满：

- **🎯 目标** —— 学完能做到什么
- **🔗 前置** —— 依赖哪些章
- **💡 关键洞察** —— 一两句最该记住的
- **📊 Java 25 → Go 对照表**
- **📖 讲解笔记** —— 正文
- **⌨️ 动手清单** —— 可勾选的练习步骤
- **🧪 代码练习** —— `go/exercises`（带测试，你实现）+ `java/`（对照 demo，可运行）
- **✅ 验收标准** —— 怎么算学完
- **🕳️ 常见坑** —— Java 老手尤其会栽的
- **☕ 顺带复习 Java 25** —— 对应的 JEP / 新特性
- **📚 延伸阅读**

---

## 推荐资料（精选，不贪多）

- 《The Go Programming Language》(Donovan & Kernighan) —— Go 界的 K&R
- 《Concurrency in Go》(Cox-Buday) —— 为并发背景定制
- 《100 Go Mistakes and How to Avoid Them》(Harsanyi) —— **专治老手用别的语言思维写 Go 的坑，强烈推荐**
- 官方：A Tour of Go / Effective Go / Go by Example / Go Blog
- Uber Go Style Guide、Google Go Style Guide
