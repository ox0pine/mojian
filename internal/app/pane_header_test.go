package app

import (
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestPreviewPaneKeepsHeaderButtonsWhenUnfocused(t *testing.T) {
	registerFonts()
	a := newProjectApp(t)
	img := image.NewRGBA(image.Rect(0, 0, 8, 8))
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			img.Set(x, y, color.RGBA{uint8(x * 20), uint8(y * 20), 180, 255})
		}
	}
	path := filepath.Join(a.store.Dir, "cover.png")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := png.Encode(f, img); err != nil {
		t.Fatal(err)
	}
	f.Close()
	if err := a.store.Refresh(); err != nil {
		t.Fatal(err)
	}
	asset := a.store.AssetByRel("cover.png")
	if asset == nil {
		t.Fatal("the image was not indexed")
	}
	a.openAsset(asset)
	a.splitWith(a.store.ByRelPath("Reading notes.md"))
	tt := ui.NewTester(a.view, 1100, 700)
	tt.Frame()
	tt.Frame()
	if a.tab().Focus.Asset != nil {
		t.Fatal("the asset pane still has the focus")
	}
	if got := countText(tt.Texts(), "Close Pane"); got != 2 {
		t.Fatalf("close buttons: %d, want 2 (preview and focused editor)", got)
	}
}

func TestMarkdownPreviewKeepsHeaderButtonsWhenUnfocused(t *testing.T) {
	registerFonts()
	a := newProjectApp(t)
	a.openNote(a.store.ByRelPath("Welcome.md"))
	p := a.tab().Focus
	p.Preview = true
	a.splitWith(a.store.ByRelPath("Reading notes.md"))
	tt := ui.NewTester(a.view, 1100, 700)
	tt.Frame()
	tt.Frame()
	if a.tab().Focus == p {
		t.Fatal("the preview pane still has the focus")
	}
	if got := countText(tt.Texts(), "Close Pane"); got != 2 {
		t.Fatalf("close buttons: %d, want 2 (preview and focused editor)", got)
	}
	if !tt.HasText("Edit") {
		t.Fatalf("the preview's Edit button is gone: %q", tt.Texts())
	}
}

func TestCompactPaneKeepsTitle(t *testing.T) {
	registerFonts()
	a := newProjectApp(t)
	a.treeOpen = false
	a.splitWith(a.store.ByRelPath("Reading notes.md"))
	a.splitWith(a.store.ByRelPath("Welcome.md"))
	tt := ui.NewTester(a.view, 760, 460)
	tt.Frame()
	tt.Frame()
	header, ok := tt.Find("Move Pane Reading notes")
	if !ok {
		t.Fatalf("compact pane header not found: %q", tt.Texts())
	}
	title, ok := tt.Find("Pane title Reading notes")
	if !ok {
		t.Fatal("compact pane has no file title")
	}
	if title.Y < header.Y || title.Y >= header.Y+header.H {
		t.Fatalf("the only file title is outside the compact header: header=%+v title=%+v", header, title)
	}
	if title.W < 80 {
		t.Fatalf("compact title has no useful visible width: %+v", title)
	}
	actions, ok := tt.Find("Pane actions Reading notes")
	if !ok || actions.W > 44 {
		t.Fatalf("compact pane kept the full toolbar: %+v", actions)
	}
}

func TestUltraCompactPaneKeepsTitleAndDragHandle(t *testing.T) {
	registerFonts()
	a := newProjectApp(t)
	a.splitWith(a.store.ByRelPath("Reading notes.md"))
	a.splitWith(a.store.ByRelPath("Welcome.md"))
	tt := ui.NewTester(a.view, 760, 460)
	tt.Frame()
	tt.Frame()
	title, ok := tt.Find("Pane title Reading notes")
	if !ok || title.W < 56 {
		t.Fatalf("ultra compact pane lost its readable title: %+v", title)
	}
	if _, ok := tt.Find("Move Pane Reading notes"); !ok {
		t.Fatal("ultra compact pane lost its drag handle")
	}
	actions, ok := tt.Find("Pane actions Reading notes")
	if !ok || actions.W > 22 {
		t.Fatalf("ultra compact pane kept more than overflow: %+v", actions)
	}
}

func countText(texts []string, want string) int {
	n := 0
	for _, text := range texts {
		if strings.HasPrefix(text, want) {
			n++
		}
	}
	return n
}

func TestEveryPaneKeepsHeaderButtonsWhenUnfocused(t *testing.T) {
	registerFonts()
	a := newProjectApp(t)
	a.openNote(a.store.ByRelPath("Reading notes.md"))
	first := a.tab().Focus
	a.splitWith(a.store.ByRelPath("Welcome.md"))
	tt := ui.NewTester(a.view, 1100, 700)
	tt.Frame()
	tt.Frame()
	if a.tab().Focus == first {
		t.Fatal("the first pane still has the focus")
	}
	if got := countText(tt.Texts(), "Close Pane"); got != 2 {
		t.Fatalf("close buttons: %d, want 2", got)
	}
	if got := countText(tt.Texts(), "Split Right"); got != 2 {
		t.Fatalf("split buttons: %d, want 2", got)
	}
}
