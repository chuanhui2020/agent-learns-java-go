package main

import (
	"strconv"
	"strings"
	"testing"
)

func TestParseLevel(t *testing.T) {
	maxInt := int(^uint(0) >> 1)
	for _, tc := range []struct {
		input string
		want  int
	}{
		{"0", 0},
		{"42", 42},
		{"0007", 7},
		{strconv.Itoa(maxInt), maxInt},
		{strings.Repeat("0", 100) + "7", 7},
	} {
		t.Run(tc.input, func(t *testing.T) {
			got, err := parseLevel(tc.input)
			if err != nil || got != tc.want {
				t.Fatalf("parseLevel(%q) = (%d, %v), want (%d, nil)", tc.input, got, err, tc.want)
			}
		})
	}

	for _, input := range []string{
		"", "4x", "-1", "+1", " 7", "7 ", "４２",
		strconv.FormatUint(uint64(maxInt)+1, 10),
		strconv.Itoa(maxInt) + "0", strings.Repeat("9", 100),
	} {
		t.Run(input, func(t *testing.T) {
			if got, err := parseLevel(input); err == nil || got != 0 {
				t.Fatalf("parseLevel(%q) = (%d, %v), want (0, error)", input, got, err)
			}
		})
	}
}
