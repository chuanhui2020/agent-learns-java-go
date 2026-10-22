// Ch00 · Go 语法差异速通（可运行）
//
// 运行： go run ./ch00-setup/go/demo
//
// 每一段都标注了「对照 Java」。目标不是学概念，而是看清同一件事 Go 换了什么写法。
package main

import (
	"errors"
	"fmt"
	"runtime"
	"runtime/debug"
	"strings"
)

// iota：常量组里自动递增。配合自定义类型做枚举（对照 Java 的 enum，
// 但本质是「有类型的整型常量」，零成本、无对象）。
type Weapon int

const (
	Fist  Weapon = iota // 0
	Sword               // 1（沿用上一行的 = iota 表达式）
	Bow                 // 2
	Staff               // 3
)

// String() 让 Weapon 满足 fmt.Stringer 接口 —— 隐式实现，无需 implements（Ch02 细讲）。
func (w Weapon) String() string {
	// switch 默认「不穿透」：每个 case 自带 break，不用手写。
	// （对照 Java 必须写 break，否则 fallthrough 到下一个 case。）
	switch w {
	case Fist:
		return "拳头"
	case Sword:
		return "剑"
	case Bow:
		return "弓"
	case Staff:
		return "法杖"
	default:
		return "未知"
	}
}

// 多返回值：Go 惯用 (结果, error)，而不是抛异常。
// 对照 Java：这里你会 throw NumberFormatException 或返回 Optional。
func parseLevel(s string) (int, error) {
	if s == "" {
		return 0, errors.New("空字符串")
	}
	level := 0
	for _, ch := range s { // for range 遍历字符串的 rune
		if ch < '0' || ch > '9' {
			return 0, errors.New("非法字符: " + string(ch))
		}
		level = level*10 + int(ch-'0')
	}
	return level, nil
}

func main() {
	// 1) := 短声明：类型自动推断（对照 Java 的 var，但连 var 都省了）。
	name := "勇者"
	hp := 100 // 推断为 int
	fmt.Printf("[:=]     %s HP=%d\n", name, hp)

	// 2) defer：函数返回前执行，LIFO 顺序。最先注册的最后跑。
	//    对照 try-with-resources / finally，但不绑定某个块，可紧挨获取处写释放。
	defer fmt.Println("[defer]  最后执行（注册最早）")
	defer fmt.Println("[defer]  中间执行")
	defer fmt.Println("[defer]  最先执行（注册最晚）")

	// 3) 多返回值 + 错误即值：if err != nil 是 Go 的日常，不是缺陷。
	if lvl, err := parseLevel("42"); err != nil {
		fmt.Println("[err]    解析失败:", err)
	} else {
		fmt.Printf("[multi]  等级解析成功: %d\n", lvl)
	}
	if _, err := parseLevel("4x"); err != nil { // _ 丢弃不需要的返回值
		fmt.Println("[err]    预期内的失败:", err)
	}

	// 4) iota 枚举 + Stringer：%v 会自动调用 String()
	fmt.Printf("[iota]   武器: %v / %v / %v\n", Sword, Bow, Staff)

	// 5) for 是唯一循环关键字（没有 while）；没有三元运算符。
	best := 0
	for i := 1; i <= 5; i++ {
		if i > best { // 无三元，只能 if
			best = i
		}
	}
	fmt.Printf("[for]    1..5 里最大的是 %d（for 也能当 while 用）\n", best)
	// 真要取较大值，用内建 max/min（任意个数、任意有序类型），而不是手写三元。
	fmt.Printf("[max]    max(3, 7, 5) = %d\n", max(3, 7, 5))

	// 6) 热路径字符串拼接用 strings.Builder（对照 Java StringBuilder）。
	var b strings.Builder
	for range 3 { // for range 整数
		b.WriteString("go")
	}
	fmt.Printf("[sb]     %s\n", b.String())

	// 7) new(expr)：直接拿到一个指向初值的指针。
	//    常用于"指针表示可选字段"（JSON/protobuf），对照 Java 里用包装类型 Integer 表示可空。
	lv := new(parseLevelOrZero("7"))
	fmt.Printf("[new]    *lv = %d（%T）\n", *lv, lv)

	// 8) 工具链自检：二进制自己知道是谁、用什么设置编出来的（对照 jcmd VM.version / VM.flags）。
	//    同一份代码，go.mod 的 go 行不同，DefaultGODEBUG 就可能不同 —— 见 README 实验 1。
	fmt.Printf("[tool]   runtime=%s %s/%s GOMAXPROCS=%d NumCPU=%d\n",
		runtime.Version(), runtime.GOOS, runtime.GOARCH, runtime.GOMAXPROCS(0), runtime.NumCPU())
	if bi, ok := debug.ReadBuildInfo(); ok {
		for _, s := range bi.Settings {
			switch s.Key {
			case "-compiler", "CGO_ENABLED", "GOAMD64", "DefaultGODEBUG":
				fmt.Printf("[tool]   %s=%s\n", s.Key, s.Value)
			}
		}
	}
}

func parseLevelOrZero(s string) int {
	lvl, err := parseLevel(s)
	if err != nil {
		return 0
	}
	return lvl
}
