package app

import (
	"fmt"
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

func TestRevealRelClearsExplorerSearch(t *testing.T) {
	registerFonts()
	a := newProjectApp(t)
	a.treeQuery = "Reading notes"
	tt := ui.NewTester(a.view, 900, 500)
	tt.Frame()
	if _, ok := tt.Find("app.js"); ok {
		t.Fatal("the search fixture did not filter app.js out")
	}

	target := filepath.Join("scripts", "app.js")
	a.revealRel(target)
	if a.treeQuery != "" {
		t.Fatalf("reveal left the Explorer search query as %q", a.treeQuery)
	}
	if a.treeSel != target {
		t.Fatalf("reveal selected %q, want %q", a.treeSel, target)
	}
	tt.Frame()
	row, ok := tt.Find("app.js")
	if !ok {
		t.Fatal("the revealed file is not in the tree")
	}
	if row.Y < 0 || row.Y+row.H > 500 {
		t.Fatalf("the revealed file is outside the viewport: %+v", row)
	}
}

func TestSelectFolderScrollsIntoView(t *testing.T) {
	registerFonts()
	a := newProjectApp(t)
	for i := range 40 {
		if err := os.Mkdir(filepath.Join(a.store.Dir, fmt.Sprintf("folder-%02d", i)), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir(filepath.Join(a.store.Dir, "zz-folder"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := a.store.Refresh(); err != nil {
		t.Fatal(err)
	}

	const height = 200
	tt := ui.NewTester(a.view, 900, height)
	tt.Frame()
	before := a.explorer.TreeScroll.Y
	if before == 0 {
		t.Fatal("the folder fixture did not start scrolled away from the top")
	}

	a.selectRel("folder-00")
	tt.Frame()
	if _, ok := tt.Find("folder-00"); !ok {
		t.Fatal("the selected folder row disappeared")
	}
	if got := a.explorer.TreeScroll.Y; got >= before {
		t.Fatalf("revealing the top folder did not scroll up: before=%v after=%v", before, got)
	}
}
