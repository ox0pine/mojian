package app

import (
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
