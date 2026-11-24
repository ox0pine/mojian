package prefs

import (
	"slices"
	"testing"
)

func TestParseWrapGuides(t *testing.T) {
	if got := ParseWrapGuides(""); len(got) != 0 {
		t.Fatalf("empty input = %v, want nil/empty", got)
	}
	got := ParseWrapGuides("100, 80; foo 10 450 80 120")
	want := []int{80, 100, 120}
	if !slices.Equal(got, want) {
		t.Fatalf("ParseWrapGuides = %v, want %v", got, want)
	}
}

func TestSanitizeWhitespaceAndWrapGuides(t *testing.T) {
	p := Default()
	p.EditorWhitespace = 99
	p.EditorWrapGuides = "  80, 100  "
	p.Sanitize()
	if p.EditorWhitespace != 0 {
		t.Fatalf("EditorWhitespace = %d, want 0", p.EditorWhitespace)
	}
	if p.EditorWrapGuides != "80, 100" {
		t.Fatalf("EditorWrapGuides = %q, want trimmed", p.EditorWrapGuides)
	}

	p.EditorWhitespace = 4
	p.Sanitize()
	if p.EditorWhitespace != 4 {
		t.Fatalf("valid EditorWhitespace = %d, want 4", p.EditorWhitespace)
	}
}

func TestSanitizeStatusBarPrefs(t *testing.T) {
	def := Default()
	if def.ShowStatusBar || def.StatusBarCPU || def.StatusBarMemory || def.StatusBarGPU {
		t.Fatalf("status bar and CPU/Mem/GPU toggles must default to false: %+v", def)
	}
	if def.StatusBarSysMode != 0 {
		t.Fatalf("default StatusBarSysMode = %d, want 0 (Terminal panes)", def.StatusBarSysMode)
	}
	if def.StatusBarInterval != 2 {
		t.Fatalf("default StatusBarInterval = %d, want 2", def.StatusBarInterval)
	}

	p := def
	p.StatusBarSysMode = 7
	p.StatusBarInterval = 99
	p.Sanitize()
	if p.StatusBarSysMode != 0 {
		t.Fatalf("sanitized StatusBarSysMode = %d, want 0", p.StatusBarSysMode)
	}
	if p.StatusBarInterval != 2 {
		t.Fatalf("sanitized StatusBarInterval = %d, want 2", p.StatusBarInterval)
	}

	p.StatusBarSysMode = 1
	p.StatusBarInterval = 10
	p.Sanitize()
	if p.StatusBarSysMode != 1 || p.StatusBarInterval != 10 {
		t.Fatalf("valid status bar prefs clobbered: mode=%d interval=%d", p.StatusBarSysMode, p.StatusBarInterval)
	}
}
