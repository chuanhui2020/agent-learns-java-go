// Package lab compares closure lifetimes. Allocation counts are measurements of
// a particular compiler, not part of these functions' contracts.
package lab

func Immediate(x int) int {
	return func() int { return x + 1 }()
}

func NewAdder(x int) func() int {
	return func() int { return x + 1 }
}

func NewCounter(x int) func() int {
	return func() int {
		x++
		return x
	}
}

// Payload exceeds the current compiler's small by-value capture threshold on
// both 32-bit and 64-bit systems. Read the diagnostic rather than assuming it.
type Payload [64]int

func NewReader(payload Payload) func() int {
	return func() int { return payload[0] }
}
