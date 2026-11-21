package app

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestProjectSearchFindsAndJumps(t *testing.T) {
	registerFonts()
	a := newProjectApp(t)
	if err := os.WriteFile(filepath.Join(a.store.Dir, "alpha.go"), []byte("package alpha\n\nfunc Hello() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(a.store.Dir, "beta.md"), []byte("# Beta\n\nMentions Hello on line three\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := a.store.Refresh(); err != nil {
		t.Fatal(err)
	}
	// Make beta.md dirty with a new unsaved match so override search is verified too.
	beta := a.store.ByRelPath("beta.md")
	if beta == nil {
		t.Fatal("beta.md missing")
	}
	beta.Buffer().Set(beta.Text() + "\nUnsavedMarker\n")

	tt := ui.NewTester(a.view, 1100, 700)
	a.openSearch()
	a.searchQuery = "Hello"
	tt.Frame()
	tt.Frame()

	if len(a.searchResult.Matches) != 2 {
		t.Fatalf("got %d matches, want 2: %+v", len(a.searchResult.Matches), a.searchResult.Matches)
	}
	if !tt.HasText("2 matches in 2 files") {
		t.Fatalf("expected summary in search modal, texts=%v", tt.Texts())
	}

	// Verify dirty unsaved text is searchable immediately.
	a.searchQuery = "UnsavedMarker"
	tt.Frame()
	tt.Frame()
	if len(a.searchResult.Matches) != 1 || a.searchResult.Matches[0].Rel != "beta.md" {
		t.Fatalf("expected unsaved match in beta.md, got %+v", a.searchResult.Matches)
	}

	// Open the alpha.go match on line 3 and verify the editor jumps there.
	a.searchQuery = "func Hello"
	tt.Frame()
	tt.Frame()
	if len(a.searchResult.Matches) != 1 {
		t.Fatalf("expected 1 match for func Hello, got %+v", a.searchResult.Matches)
	}
	m := a.searchResult.Matches[0]
	a.closeSearch()
	a.openNoteAt(filepath.FromSlash(m.Rel), m.Line, m.Col, m.End)
	tt.Frame()

	p := a.focusPane()
	if p == nil || p.Note == nil || p.Note.Rel != "alpha.go" {
		t.Fatalf("expected alpha.go focused, got %+v", p)
	}
	if got := p.state.CursorPos().Line; got != 2 {
		t.Fatalf("cursor line = %d, want 2 (0-based for line 3)", got)
	}
}
