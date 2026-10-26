package main

// Executable specification: these results follow from language semantics, not
// scheduling, compiler optimizations or function value addresses.
func Example_main() {
	main()
	// Output:
	// range := 1 2 3
	// range =  3 3 3
	// method values: 10 20
	// shared backing array: 9
	// method expressions: 20 20
	// mutable capture: 8
	// zero function is nil: true
}
