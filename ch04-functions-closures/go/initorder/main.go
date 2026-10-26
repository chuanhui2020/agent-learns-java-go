// Run as a new process to observe package initialization before main.
package main

import (
	"fmt"

	"agent-learns-java-go/ch04-functions-closures/go/initorder/internal/dependency"
)

// Despite the source order, first depends on second and must wait for it.
var first = record("main.first", second+1)
var second = record("main.second", dependency.Value)

func record(name string, value int) int {
	fmt.Println(name)
	return value
}

func init() {
	fmt.Println("main.init")
}

func main() {
	fmt.Printf("main.body first=%d second=%d\n", first, second)
}
