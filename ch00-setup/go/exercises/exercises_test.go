package exercises

import (
	"errors"
	"strings"
	"testing"
)

// 表驱动测试（table-driven tests）是 Go 的惯用法：
// 用一个 struct 切片列出所有用例，循环跑，每个用例用 t.Run 起子测试。
// 对照 Java：类似 JUnit 5 的 @ParameterizedTest，但无需注解、无需依赖。

func TestDivide(t *testing.T) {
	tests := []struct {
		name    string
		a, b    int
		want    int
		wantErr error
	}{
		{"basic", 6, 2, 3, nil},
		{"truncates", 7, 2, 3, nil}, // 整数除法向零截断
		{"negative", -6, 2, -3, nil},
		{"zero divisor", 1, 0, 0, ErrDivideByZero},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Divide(tt.a, tt.b)
			// errors.Is 沿错误链比较（Ch08 细讲）；这里判断是否是预期的错误。
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("Divide(%d, %d) err = %v, want %v", tt.a, tt.b, err, tt.wantErr)
			}
			if tt.wantErr == nil && got != tt.want {
				t.Errorf("Divide(%d, %d) = %d, want %d", tt.a, tt.b, got, tt.want)
			}
		})
	}
}

func TestFizzBuzz(t *testing.T) {
	tests := []struct {
		in   int
		want string
	}{
		{1, "1"},
		{3, "Fizz"},
		{5, "Buzz"},
		{9, "Fizz"},
		{10, "Buzz"},
		{15, "FizzBuzz"},
		{4, "4"},
	}
	for _, tt := range tests {
		if got := FizzBuzz(tt.in); got != tt.want {
			t.Errorf("FizzBuzz(%d) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestSumEven(t *testing.T) {
	tests := []struct {
		name string
		in   []int
		want int
	}{
		{"empty", nil, 0},
		{"mixed", []int{1, 2, 3, 4}, 6},
		{"all odd", []int{1, 3, 5}, 0},
		{"with negatives", []int{-2, -3, 4}, 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := SumEven(tt.in); got != tt.want {
				t.Errorf("SumEven(%v) = %d, want %d", tt.in, got, tt.want)
			}
		})
	}
}

func TestParseEndpoint(t *testing.T) {
	tests := []struct {
		in       string
		wantName string
		wantPort int
		wantErr  error
	}{
		{"gate-01:7000", "gate-01", 7000, nil},
		{"zone:3:7000", "zone:3", 7000, nil}, // 按最后一个冒号切
		{"room:1", "room", 1, nil},
		{"room:65535", "room", 65535, nil},
		{"gate-01", "", 0, ErrNoPort},
		{":7000", "", 0, ErrEmptyName},
		{"gate-01:", "", 0, ErrBadPort},
		{"gate-01:abc", "", 0, ErrBadPort},
		{"gate-01:0", "", 0, ErrBadPort},
		{"gate-01:65536", "", 0, ErrBadPort},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			name, port, err := ParseEndpoint(tt.in)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("ParseEndpoint(%q) err = %v, want %v", tt.in, err, tt.wantErr)
			}
			if tt.wantErr != nil {
				return
			}
			if name != tt.wantName || port != tt.wantPort {
				t.Errorf("ParseEndpoint(%q) = (%q, %d), want (%q, %d)", tt.in, name, port, tt.wantName, tt.wantPort)
			}
		})
	}
}

// 端口错误要「包装」而不是「替换」：既能 errors.Is 命中哨兵，信息里又保留现场。
func TestParseEndpointBadPortKeepsContext(t *testing.T) {
	_, _, err := ParseEndpoint("gate-01:7x00")
	if !errors.Is(err, ErrBadPort) {
		t.Fatalf("err = %v, want wrapping ErrBadPort", err)
	}
	if err == ErrBadPort {
		t.Errorf("err 直接返回了哨兵本身，应该用 %%w 包装并带上原始端口串")
	}
	if !strings.Contains(err.Error(), "7x00") {
		t.Errorf("err = %q, 错误信息里应包含原始端口串 \"7x00\"", err)
	}
}
