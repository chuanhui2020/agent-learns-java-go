// Command check validates repository structure, local Markdown link targets and
// Go formatting. It does not run exercises, change files or access the network.
package main

import (
	"bytes"
	"flag"
	"fmt"
	"go/format"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

var (
	chapterName = regexp.MustCompile(`^ch([0-9]{2})-`)
	// Repository docs use inline links; reference links and anchors are not checked.
	inlineLink = regexp.MustCompile(`\[[^\]\n]+\]\((<[^>]+>|[^\s)]+)(?:\s+"[^"]*")?\)`)
)

func main() {
	root := flag.String("root", ".", "repository root")
	flag.Parse()
	if flag.NArg() != 0 {
		fmt.Fprintln(os.Stderr, "usage: go run ./tools/check [-root path]")
		os.Exit(2)
	}
	issues := checkRepository(*root)
	for _, issue := range issues {
		fmt.Fprintln(os.Stderr, issue)
	}
	if len(issues) > 0 {
		fmt.Fprintf(os.Stderr, "FAIL: %d repository issue(s)\n", len(issues))
		os.Exit(1)
	}
	fmt.Println("PASS: Ch00-Ch19, local Markdown link targets and Go formatting")
	fmt.Println("Exercise answers, Markdown anchors and external URLs are not checked.")
}

func checkRepository(root string) []string {
	root, err := filepath.Abs(root)
	if err != nil {
		return []string{err.Error()}
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return []string{err.Error()}
	}
	var issues []string
	for _, name := range []string{"go.mod", "README.md"} {
		if info, err := os.Stat(filepath.Join(root, name)); err != nil || info.IsDir() {
			issues = append(issues, name+": required file is missing")
		}
	}
	var chapters [20]int
	for _, entry := range entries {
		match := chapterName.FindStringSubmatch(entry.Name())
		if !entry.IsDir() || match == nil {
			continue
		}
		number, _ := strconv.Atoi(match[1])
		if number >= len(chapters) {
			issues = append(issues, entry.Name()+": chapter is outside Ch00-Ch19")
			continue
		}
		chapters[number]++
		readme := filepath.Join(entry.Name(), "README.md")
		if info, err := os.Stat(filepath.Join(root, readme)); err != nil || info.IsDir() {
			issues = append(issues, readme+": chapter README is missing")
		}
	}
	for number, count := range chapters {
		if count != 1 {
			issues = append(issues, fmt.Sprintf("Ch%02d: expected one chapter directory, found %d", number, count))
		}
	}

	err = filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			if path != root && (strings.HasPrefix(entry.Name(), ".") || entry.Name() == "node_modules" || entry.Name() == "vendor" || entry.Name() == "out" || entry.Name() == "dist") {
				return filepath.SkipDir
			}
			return nil
		}
		if filepath.Ext(path) != ".md" && filepath.Ext(path) != ".go" {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		name, _ := filepath.Rel(root, path)
		if filepath.Ext(path) == ".md" {
			issues = append(issues, checkLinks(root, name, string(data))...)
			return nil
		}
		formatted, err := format.Source(data)
		if err != nil {
			issues = append(issues, fmt.Sprintf("%s: %v", name, err))
		} else if !bytes.Equal(formatted, bytes.ReplaceAll(data, []byte("\r\n"), []byte("\n"))) {
			issues = append(issues, name+": run gofmt -w on this file")
		}
		return nil
	})
	if err != nil {
		issues = append(issues, err.Error())
	}
	return issues
}

func checkLinks(root, name, content string) []string {
	var issues []string
	var fence byte
	var fenceLength int
	for index, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if len(trimmed) >= 3 && (trimmed[0] == '`' || trimmed[0] == '~') {
			length := 0
			for length < len(trimmed) && trimmed[length] == trimmed[0] {
				length++
			}
			if length >= 3 {
				if fence == 0 {
					fence, fenceLength = trimmed[0], length
				} else if trimmed[0] == fence && length >= fenceLength && strings.TrimSpace(trimmed[length:]) == "" {
					fence = 0
				}
				continue
			}
		}
		if fence != 0 {
			continue
		}
		for _, match := range inlineLink.FindAllStringSubmatch(maskCodeSpans(line), -1) {
			target := strings.Trim(match[1], "<>")
			link, err := url.Parse(target)
			if err != nil {
				issues = append(issues, fmt.Sprintf("%s:%d: invalid link %q", name, index+1, target))
				continue
			}
			if link.IsAbs() || link.Host != "" || link.Path == "" {
				continue
			}
			path := filepath.Join(root, filepath.Dir(name), filepath.FromSlash(link.Path))
			relative, err := filepath.Rel(root, path)
			if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) || strings.HasPrefix(link.Path, "/") {
				issues = append(issues, fmt.Sprintf("%s:%d: link %q is outside the repository", name, index+1, target))
				continue
			}
			if _, err := os.Stat(path); err != nil {
				issues = append(issues, fmt.Sprintf("%s:%d: link %q: %v", name, index+1, target, err))
			}
		}
	}
	return issues
}

// Hide code spans while keeping link labels such as [`file`](path) recognizable.
// A delimiter closes a span only when its number of backticks matches the opener.
func maskCodeSpans(line string) string {
	masked := []byte(line)
	for start := 0; start < len(line); {
		if line[start] != '`' {
			start++
			continue
		}
		end := start
		for end < len(line) && line[end] == '`' {
			end++
		}
		closed := false
		for next := end; next < len(line); {
			if line[next] != '`' {
				next++
				continue
			}
			closeEnd := next
			for closeEnd < len(line) && line[closeEnd] == '`' {
				closeEnd++
			}
			if closeEnd-next == end-start {
				for i := start; i < closeEnd; i++ {
					masked[i] = ' '
				}
				start, closed = closeEnd, true
				break
			}
			next = closeEnd
		}
		if !closed {
			start = end
		}
	}
	return string(masked)
}
