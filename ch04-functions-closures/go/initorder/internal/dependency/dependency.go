// Package dependency has visible initialization solely for this experiment.
package dependency

import "fmt"

var Value = initialize()

func initialize() int {
	fmt.Println("dependency.variable")
	return 7
}

func init() {
	fmt.Println("dependency.init")
}
