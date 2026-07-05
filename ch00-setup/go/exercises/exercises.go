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
