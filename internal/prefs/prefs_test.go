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
