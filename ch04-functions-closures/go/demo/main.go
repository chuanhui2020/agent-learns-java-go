// Ch04: predict each line before running go run ./ch04-functions-closures/go/demo.
package main

import "fmt"

type counter struct {
	n     int
	items []int
}

func (c counter) value() int    { return c.n }
func (c *counter) pointer() int { return c.n }
func (c counter) first() int    { return c.items[0] }

func main() {
	var fresh, shared []func() int
	for _, v := range []int{1, 2, 3} {
		fresh = append(fresh, func() int { return v })
	}
	var v int
	for _, v = range []int{1, 2, 3} {
		shared = append(shared, func() int { return v })
	}
	fmt.Println("range :=", fresh[0](), fresh[1](), fresh[2]())
	fmt.Println("range = ", shared[0](), shared[1](), shared[2]())

	c := counter{n: 10, items: []int{1}}
	value, pointer, first := c.value, c.pointer, c.first
	c.n = 20
	c.items[0] = 9
	fmt.Println("method values:", value(), pointer())
	// A copied slice header still points at the original backing array.
	fmt.Println("shared backing array:", first())
	fmt.Println("method expressions:", counter.value(c), (*counter).pointer(&c))

	x := 7
	read := func() int { return x }
	x = 8
	fmt.Println("mutable capture:", read())
	var missing func() int
	fmt.Println("zero function is nil:", missing == nil)
}
