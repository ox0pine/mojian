package app

import (
	"strings"
	"testing"
)

// TestLastScreenLine checks the line a terminal notification carries: the
// last line with text, its spacing collapsed, cut for the body.
func TestLastScreenLine(t *testing.T) {
	cases := []struct {
		name   string
		screen string
		want   string
	}{
		{"empty", "", ""},
		{"blank lines", "\n  \n\t\n", ""},
		{"last text", "$ go test ./...\nok  \tmojian/internal/app\t1.2s\n\n$ ", "ok mojian/internal/app 1.2s"},
		{"agent answer", "omp\n\nDone: fixed the parser.\n", "Done: fixed the parser."},
	}
	for _, tc := range cases {
		if got := lastScreenLine(tc.screen); got != tc.want {
			t.Errorf("%s: got %q, want %q", tc.name, got, tc.want)
		}
	}

	long := strings.Repeat("a", 400)
	got := lastScreenLine(long)
	if r := []rune(got); len(r) != 160 || !strings.HasSuffix(got, "…") {
		t.Fatalf("long line: %d runes, %q", len(r), got)
	}
}
