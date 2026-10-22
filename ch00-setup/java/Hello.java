// Ch00 · Java 25：JEP 512 紧凑源文件 + 实例 main、JEP 511 模块导入、JEP 456 未命名变量
//
// 运行：  bin/jrun ch00-setup/java/Hello.java
//
// 注意这里没有 `public class Hello`，也没有 `public static void main(String[] args)`：
// 整个文件被隐式包成一个 final 顶层类，main 是无参的实例方法，由启动器负责实例化。
// 对照 Go 的 func main()：两边都在往「低仪式感入口」靠拢。

import module java.net.http;   // JEP 511：一次导入 java.net.http 模块导出的全部包（HttpClient 等）

void main() {
    // java.lang.IO（JEP 512）：IO.println 是 System.out.println 的轻量替代
    IO.println("Hello from Java " + Runtime.version() + " — 紧凑源文件 + 实例 main（JEP 512）");

    // 紧凑源文件自动导入 java.base 模块导出的全部包：List / Map 不用写 import
    var langs = List.of("Java 25", "Go 1.27");
    for (var lang : langs) {                     // 增强 for（对照 Go 的 for range）
        IO.println("  learning: " + lang);
    }

    // JEP 511：HttpClient 来自上面的 import module java.net.http，而不是 java.base
    IO.println("HttpClient 所在模块: " + HttpClient.class.getModule().getName());

    // JEP 456：用 _ 丢弃用不到的变量（对照 Go 的空白标识符 _）
    try {
        Integer.parseInt("4x");
    } catch (NumberFormatException _) {
        IO.println("解析 \"4x\" 失败 —— Java 抛异常，Go 返回 (0, err)");
    }
}
