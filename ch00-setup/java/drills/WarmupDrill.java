// Ch00 · Java 25 热身 drill：用 Java 25 实现与 go/exercises 相同的 4 个函数，再对比两边的错误处理风格。
//
// 运行：  bin/jrun ch00-setup/java/drills/WarmupDrill.java
//
// 初始一定失败（桩返回默认值，退出码 1）。实现 4 个 TODO 后应打印 ALL PASS。
// 约定（见 docs/methodology.md 第七节）：只改 TODO 处，底部的自校验不要改。

/** 对照 Go 的 ParseEndpoint 返回 (name, port, err)：Java 用 record 承载结果，用异常表达失败。 */
record Endpoint(String name, int port) {}

/** 返回 a/b 的整数商；b == 0 时抛 ArithmeticException（对照 Go：返回 (0, ErrDivideByZero)）。 */
int divide(int a, int b) {
    // TODO: 实现我。想一想：int 除零本来就会抛 ArithmeticException，还要不要显式检查？
    return 0;
}

/** 规则同 Go 版：3 的倍数 "Fizz"，5 的倍数 "Buzz"，15 的倍数 "FizzBuzz"，其余为十进制字符串。 */
String fizzBuzz(int n) {
    // TODO: 用 switch 表达式实现（箭头形式的 case 不穿透 —— 和 Go 的 switch 一样）
    return "";
}

/** 返回所有偶数之和。 */
int sumEven(int[] nums) {
    // TODO: 实现我（循环或 IntStream 都行，写完想想哪个在热路径上更合适）
    return 0;
}

/**
 * "gate-01:7000" → Endpoint("gate-01", 7000)；按最后一个冒号切（"zone:3:7000" → ("zone:3", 7000)）。
 * 没有冒号、名字为空、端口非整数或不在 1..65535 时，抛 IllegalArgumentException，
 * 端口解析失败时要把 NumberFormatException 作为 cause 保留（对照 Go 的 %w 包装）。
 */
Endpoint parseEndpoint(String s) {
    // TODO: 提示 lastIndexOf(':')、Integer.parseInt、new IllegalArgumentException(msg, cause)
    return null;
}

// ---------------- 自校验（不要改） ----------------

int failures = 0;

void check(boolean cond, String msg) {
    if (!cond) {
        failures++;
        IO.println("FAIL  " + msg);
    }
}

Throwable thrown(Runnable body) {
    try {
        body.run();
        return null;
    } catch (Throwable t) {
        return t;
    }
}

void main() {
    check(thrown(() -> divide(6, 2)) == null && divide(6, 2) == 3, "divide(6, 2) == 3");
    check(thrown(() -> divide(7, 2)) == null && divide(7, 2) == 3, "divide(7, 2) == 3（向零截断）");
    check(thrown(() -> divide(1, 0)) instanceof ArithmeticException, "divide(1, 0) 应抛 ArithmeticException");

    var fizz = new int[]{1, 3, 5, 9, 10, 15, 4};
    var want = new String[]{"1", "Fizz", "Buzz", "Fizz", "Buzz", "FizzBuzz", "4"};
    for (int i = 0; i < fizz.length; i++) {
        check(want[i].equals(fizzBuzz(fizz[i])), "fizzBuzz(" + fizz[i] + ") == \"" + want[i] + "\"");
    }

    check(sumEven(new int[]{}) == 0, "sumEven([]) == 0");
    check(sumEven(new int[]{1, 2, 3, 4}) == 6, "sumEven([1,2,3,4]) == 6");
    check(sumEven(new int[]{-2, -3, 4}) == 2, "sumEven([-2,-3,4]) == 2");

    check(new Endpoint("gate-01", 7000).equals(parseEndpoint("gate-01:7000")), "parseEndpoint(\"gate-01:7000\")");
    check(new Endpoint("zone:3", 7000).equals(parseEndpoint("zone:3:7000")), "parseEndpoint(\"zone:3:7000\") 按最后一个冒号切");
    for (var bad : List.of("gate-01", ":7000", "gate-01:", "gate-01:0", "gate-01:65536")) {
        check(thrown(() -> parseEndpoint(bad)) instanceof IllegalArgumentException,
                "parseEndpoint(\"" + bad + "\") 应抛 IllegalArgumentException");
    }
    var e = thrown(() -> parseEndpoint("gate-01:7x00"));
    check(e instanceof IllegalArgumentException && e.getCause() instanceof NumberFormatException,
            "端口解析失败时 cause 应为 NumberFormatException（对照 Go 的 %w）");

    if (failures == 0) {
        IO.println("ALL PASS");
    } else {
        IO.println(failures + " FAILED");
        System.exit(1);
    }
}
