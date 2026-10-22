// Package exercises 是 Ch00 的练习。
//
// 把每个函数里的 `// TODO` 换成你的实现，然后运行：
//
//	go test ./ch00-setup/go/exercises
//
// 初始一定是红的（桩函数返回零值）。实现正确后应全绿。
package exercises

import "errors"

// ErrDivideByZero 在除数为 0 时返回。
// Go 里错误是「值」，不是异常：调用方用 if err != nil 判断，用 errors.Is 比较。
var ErrDivideByZero = errors.New("divide by zero")

// Divide 返回 a/b 的整数商；当 b == 0 时返回 (0, ErrDivideByZero)。
// 练习点：多返回值 +「错误即值」。对照 Java：这里你会 throw ArithmeticException。
func Divide(a, b int) (int, error) {
	// TODO: 实现我
	return 0, nil
}

// FizzBuzz 规则：
//
//	能被 3 整除        -> "Fizz"
//	能被 5 整除        -> "Buzz"
//	同时被 3 和 5 整除 -> "FizzBuzz"
//	其它               -> n 的十进制字符串（提示：strconv.Itoa）
//
// 练习点：switch 默认不穿透（不用写 break）。
func FizzBuzz(n int) string {
	// TODO: 实现我
	return ""
}

// SumEven 返回 nums 中所有偶数之和。
// 练习点：for range 遍历 slice + 取模。Go 里 for 是唯一的循环关键字。
func SumEven(nums []int) int {
	// TODO: 实现我
	return 0
}

// ParseEndpoint 的哨兵错误。调用方用 errors.Is 判断类别，而不是比较错误字符串。
var (
	ErrEmptyName = errors.New("endpoint: empty name")
	ErrNoPort    = errors.New("endpoint: missing port")
	ErrBadPort   = errors.New("endpoint: bad port")
)

// ParseEndpoint 把 "名字:端口" 形式的服务地址拆开，例如 "gate-01:7000" -> ("gate-01", 7000)。
// 名字本身可以含冒号（如 "zone:3:7000" -> ("zone:3", 7000)），所以要按**最后一个**冒号切。
//
// 错误约定：
//
//	没有冒号                    -> ErrNoPort
//	名字为空（":7000"）          -> ErrEmptyName
//	端口不是整数或不在 1..65535  -> 包装了 ErrBadPort 的错误（errors.Is(err, ErrBadPort) 为真），
//	                               错误信息里要带上原始端口串，便于排查
//
// 练习点：strings.CutLast（Go 1.27 新增）、strconv.Atoi、用 fmt.Errorf("%w ...") 包装哨兵错误。
// 对照 Java：你会写 lastIndexOf(':') + Integer.parseInt，再把 NumberFormatException 包成 IllegalArgumentException。
func ParseEndpoint(s string) (name string, port int, err error) {
	// TODO: 实现我
	return "", 0, nil
}
