// Ch00 · Java 25 对照：JEP 512「紧凑源文件 + 实例 main 方法」
//
// 注意：这里没有 `public class Hello`，也没有 `public static void main(String[] args)`。
// 这就是紧凑源文件 —— 整个文件隐式属于一个类，main 可以是无参的实例方法。
//
// 运行：  bin/jrun ch00-setup/java/Hello.java
//
// 对照 Go 的 func main()：两边都在往「低仪式感入口」靠拢。Java 25 正在补这一课。

void main() {
    System.out.println("Hello from Java 25 — 紧凑源文件 + 实例 main（JEP 512）");

    // 传统 Java 得写成：
    //   public class Hello {
    //       public static void main(String[] args) { ... }
    //   }
    // 这些样板在 Java 25 里都能省掉。

    var langs = new String[]{"Java", "Go"};   // var：局部变量类型推断（对照 Go 的 :=）
    for (var lang : langs) {                    // 增强 for（对照 Go 的 for range）
        System.out.println("  learning: " + lang);
    }

    // JEP 512 还带来两点（这里用注释说明，避免依赖尚不熟悉的 API）：
    //   1) 紧凑源文件会自动按需导入 java.base 的公共顶层类型，可直接用 List/Map 免 import。
    //   2) 新的 java.lang.IO 提供 IO.println(...)，作为 System.out.println 的轻量替代。
    //
    // 对照 Go：func main() 从第一天起就是这么简洁的。
}
