package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeFixture(t *testing.T, root, name, content string) {
	t.Helper()
	path := filepath.Join(root, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestCheckLinks(t *testing.T) {
	root := t.TempDir()
	writeFixture(t, root, "docs/target file.md", "# Target\n")
	writeFixture(t, root, "chapter/README.md", "# Chapter\n")
	content := "[file](target%20file.md#heading)\n" +
		"[space](<target file.md>)\n[directory](../chapter/)\n" +
		"[external](https://example.com/missing)\n[anchor](#heading)\n" +
		"`func[T](value)` and ``code ` [fake](missing.md)``\n" +
		"[`file`](target%20file.md)\n" +
		"````md\n```go\n[example](missing-example.md)\n````\n" +
		"~~~md\n[example](also-missing.md)\n~~~\n" +
		"[broken](missing.md)\n[outside](../../outside.md)\n"
	issues := checkLinks(root, "docs/README.md", content)
	if len(issues) != 2 || !strings.Contains(issues[0], "missing.md") || !strings.Contains(issues[1], "outside the repository") {
		t.Fatalf("unexpected issues: %v", issues)
	}
}

func TestCheckRepository(t *testing.T) {
	root := t.TempDir()
	writeFixture(t, root, "go.mod", "module fixture\n\ngo 1.27.0\n")
	writeFixture(t, root, "README.md", "# Fixture\n")
	for n := range 20 {
		writeFixture(t, root, fmt.Sprintf("ch%02d-topic/README.md", n), "# Chapter\n")
	}
	writeFixture(t, root, "demo/main.go", "package main\r\n\r\nfunc main() {}\r\n")
	writeFixture(t, root, ".git/ignored.md", "[ignored](missing.md)\n")
	if issues := checkRepository(root); len(issues) != 0 {
		t.Fatalf("valid repository: %v", issues)
	}
	if err := os.Remove(filepath.Join(root, "ch04-topic/README.md")); err != nil {
		t.Fatal(err)
	}
	writeFixture(t, root, "ch05-duplicate/README.md", "# Duplicate\n")
	writeFixture(t, root, "demo/main.go", "package main\nfunc main(){ }\n")
	issues := checkRepository(root)
	if len(issues) != 3 {
		t.Fatalf("expected missing README, duplicate chapter and formatting error; got %v", issues)
	}
}
