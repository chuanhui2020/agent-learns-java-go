package main

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

// Package initialization runs before tests. A child test process lets us assert
// the complete startup sequence, including both packages' initialization.
func TestInitializationOrder(t *testing.T) {
	if os.Getenv("CH04_INIT_CHILD") == "1" {
		main()
		return
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(executable, "-test.run=^TestInitializationOrder$")
	cmd.Env = append(os.Environ(), "CH04_INIT_CHILD=1")
	// inittrace writes to stderr; it must not affect our stdout ordering contract.
	output, err := cmd.Output()
	if err != nil {
		t.Fatalf("child process: %v\n%s", err, output)
	}
	want := "dependency.variable\ndependency.init\nmain.second\nmain.first\nmain.init\nmain.body first=8 second=7\nPASS\n"
	if got := strings.ReplaceAll(string(output), "\r\n", "\n"); got != want {
		t.Fatalf("initialization order:\n%s\nwant:\n%s", got, want)
	}
}
