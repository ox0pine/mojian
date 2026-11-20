package app

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/egoist/mygo/ui"
)

// TestExplorerTreeArrowKeys checks the native tree keys: a click on a
// folder focuses its row and closes it, Right opens it, Down moves to its
// first file and Enter opens it.
func TestExplorerTreeArrowKeys(t *testing.T) {
	registerFonts()
	a := newProjectApp(t)
	tt := ui.NewTester(a.view, 1100, 700)
	tt.Frame()

	if err := tt.Click("scripts"); err != nil {
		t.Fatalf("click scripts: %v", err)
	}
	tt.Frame()
	if !tt.Focused("scripts") {
		t.Fatal("the click did not focus the folder row")
	}
	tt.Key(0, ui.KeyRight)
	tt.Frame()
	tt.Key(0, ui.KeyDown)
	tt.Frame()
	if !tt.Focused("app.js") {
		t.Fatal("Down did not focus the first file")
	}
	tt.Key(0, ui.KeyEnter)
	tt.Frame()

	got := a.currentNote()
	if got == nil || got.Rel != filepath.Join("scripts", "app.js") {
		t.Fatalf("Enter opened %v, want scripts/app.js", got)
	}
}

func TestExplorerSearchFindsAndOpensAssets(t *testing.T) {
	registerFonts()
	a := newProjectApp(t)
	if err := os.WriteFile(filepath.Join(a.store.Dir, "diagram.png"), []byte("asset search fixture"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := a.store.Refresh(); err != nil {
		t.Fatal(err)
	}
	a.treeQuery = "diagram.png"
	tt := ui.NewTester(a.view, 800, 500)
	tt.Frame()
	if !tt.HasText("diagram.png") || tt.HasText("No files match") {
		t.Fatalf("asset search results: %q", tt.Texts())
	}
	if err := tt.Click("diagram.png"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	p := a.tab().Focus
	if p == nil || p.Asset == nil || p.Asset.Rel != "diagram.png" {
		t.Fatal("clicking the asset search result did not open the image pane")
	}
}
