package app

import (
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

func TestZoomFadesHiddenPanesAndRevealsThem(t *testing.T) {
	registerFonts()
	a := newProjectApp(t)
	a.splitWith(a.store.ByRelPath("Reading notes.md"))
	tt := ui.NewTester(a.view, 1100, 700)
	tt.Frame()
	tt.Frame()
	tb := a.tab()
	if len(tb.panes()) != 2 {
		t.Fatalf("panes: %d", len(tb.panes()))
	}
	zoomed := tb.Focus
	hidden := tb.panes()[0]
	if hidden == zoomed {
		t.Fatal("the split left no second pane")
	}
	a.toggleZoom()
	tt.Frame()
	if tb.Zoom != zoomed {
		t.Fatal("the focused pane did not zoom")
	}
	if len(a.paneOverlays) != 1 || a.paneOverlays[0].p != hidden || a.paneOverlays[0].kind != paneCopyHidden {
		t.Fatalf("hidden panes are not fading over their old place: %+v", a.paneOverlays)
	}
	tt.Advance(60 * time.Millisecond)
	tt.Frame()
	if len(a.paneOverlays) != 1 {
		t.Fatal("the hidden pane's copy left mid-fade")
	}
	tt.Advance(400 * time.Millisecond)
	tt.Frame()
	tt.Frame()
	if len(a.paneOverlays) != 0 || hidden.hideStarted {
		t.Fatal("the hidden pane's copy never finished")
	}
	a.toggleZoom()
	if tb.Zoom != nil || !hidden.enter {
		t.Fatal("the hidden pane does not fade back in when the zoom ends")
	}
	tt.Frame()
	tt.Frame()
	if got := len(tb.panes()); got != 2 || tb.Focus != zoomed {
		t.Fatalf("unzoom changed the split: panes=%d focus=%v", got, tb.Focus)
	}
}

func TestZoomMarkdownPreviewDoesNotDoubleBindScrollHandle(t *testing.T) {
	registerFonts()
	a := newProjectApp(t)
	first := a.tab().Focus
	first.Preview = true
	a.splitWith(a.store.ByRelPath("Reading notes.md"))
	tt := ui.NewTester(a.view, 1100, 700)
	tt.Frame()
	tt.Frame()

	// Click the Zoom button on the second pane while the first pane is a
	// Markdown preview; this previously double-bound p.previewScroll in the
	// same frame when the hidden pane overlay rendered.
	if err := tt.Click("Zoom Reading notes"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	tt.Frame()
	if a.tab().Zoom == nil {
		t.Fatal("expected zoomed pane after clicking Zoom")
	}
	// Also test rendering an overlay copy alongside the live pane directly.
	a.unzoom(a.tab())
	a.paneOverlays = append(a.paneOverlays, &paneOverlay{p: first, t: a.tab(), rect: first.lastBounds, kind: paneCopyHidden})
	tt.Frame()
	tt.Frame()
}
