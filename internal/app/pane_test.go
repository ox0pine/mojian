package app

import (
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

// splitWith opens n in a new pane; tests use it to set up a layout
// without going through a blank pane and the file picker.
func (a *app) splitWith(n *note) {
	a.splitWithSide(n, splitRight)
}

// splitWithSide opens n on the named side of the focused pane.
func (a *app) splitWithSide(n *note, side splitSide) {
	t := a.tab()
	if t == nil || t.Focus == nil || !a.loadNote(n) {
		return
	}
	with := newPane(n)
	with.enterSide(side)
	if !t.splitSide(t.Focus, with, side) {
		return
	}
	t.Focus = with
	a.focusReq = with
	a.revealInTree(n)
	a.changed()
	a.invalidate()
}

func TestClosingPaneFadesOverItsOldPlace(t *testing.T) {
	registerFonts()
	a := newProjectApp(t)
	a.splitWith(a.store.ByRelPath("Reading notes.md"))
	tt := ui.NewTester(a.view, 1100, 700)
	tt.Frame()
	if got := len(a.tab().panes()); got != 2 {
		t.Fatalf("panes: %d", got)
	}
	target := a.tab().Focus
	var survivor *pane
	for _, p := range a.tab().panes() {
		if p != target {
			survivor = p
		}
	}
	a.closePane(target)
	tt.Frame()
	if !target.closing {
		t.Fatal("the closing pane did not start fading")
	}
	if got := len(a.paneOverlays); got != 1 {
		t.Fatalf("closing overlays: %d", got)
	}
	if got := len(a.tab().panes()); got != 1 || a.tab().panes()[0] != survivor {
		t.Fatalf("the layout did not close at once: %+v", a.tab().panes())
	}
	if a.tab().Focus != survivor {
		t.Fatal("focus did not move to the survivor")
	}
	tt.Advance(60 * time.Millisecond)
	tt.Frame()
	if !target.closing || len(a.paneOverlays) != 1 {
		t.Fatal("the overlay left mid-fade")
	}
	tt.Advance(400 * time.Millisecond)
	tt.Frame()
	tt.Frame()
	if target.closing || len(a.paneOverlays) != 0 {
		t.Fatal("the closing overlay never finished")
	}
}

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

func TestTerminalWithoutFileOpensAtProjectRoot(t *testing.T) {
	if !terminalsSupported() {
		t.Skip("terminals need macOS or Linux")
	}
	a := newProjectApp(t)
	a.shellStarter = func() (*termTab, error) { return &termTab{}, nil }
	a.tabs = nil
	a.active = -1
	a.treeSel = "scripts/tasks.py"
	if got := a.terminalDir(); got != a.store.Dir {
		t.Fatalf("terminal directory %q, want project root %q", got, a.store.Dir)
	}
	a.toggleTerminal()
	if len(a.tabs) != 1 {
		t.Fatalf("tabs: %d, want a terminal tab", len(a.tabs))
	}
	t0 := a.tab()
	if t0 == nil || t0.Root == nil || t0.Root.Pane == nil || t0.Root.Pane.Term == nil {
		t.Fatalf("no terminal pane: %+v", t0)
	}
	if t0.Focus != t0.Root.Pane {
		t.Fatal("the terminal pane is not focused")
	}
	if a.currentNote() != nil {
		t.Fatal("opening a terminal should not open a file")
	}
}

func TestSplitPutsPaneOnEdge(t *testing.T) {
	a := newProjectApp(t)
	first := a.tab().Focus
	a.splitWith(a.store.ByRelPath("Reading notes.md"))
	second := a.tab().Focus
	a.splitWithSide(a.store.ByRelPath("scripts/tasks.py"), splitDown)
	third := a.tab().Focus
	root := a.tab().Root
	if root == nil || root.Vertical || root.A.Pane != first || !root.B.Vertical {
		t.Fatalf("root after right and down splits: %+v", root)
	}
	if root.B.A.Pane != second || root.B.B.Pane != third {
		t.Fatalf("down split did not stack below the right pane: %+v", root.B)
	}
	if a.tab().Focus != third {
		t.Fatal("the last split did not focus its pane")
	}
}

func TestPaneFocusShortcutsMoveInAllDirections(t *testing.T) {
	a := newProjectApp(t)
	first := a.tab().Focus
	a.splitWith(a.store.ByRelPath("Reading notes.md"))
	second := a.tab().Focus
	a.splitWithSide(a.store.ByRelPath("scripts/tasks.py"), splitDown)
	third := a.tab().Focus
	// A harness window has no menu bar, so the view binds the commands
	// itself, as the browser does.
	a.harness = true
	tt := ui.NewTester(a.view, 1100, 700)
	tt.Frame()
	tt.Key(ui.Cmd|ui.Alt, ui.KeyUp)
	tt.Frame()
	if a.tab().Focus != second {
		t.Fatal("Cmd+Option+Up did not focus the pane above")
	}
	tt.Key(ui.Cmd|ui.Alt, ui.KeyDown)
	tt.Frame()
	if a.tab().Focus != third {
		t.Fatal("Cmd+Option+Down did not focus the pane below")
	}
	tt.Key(ui.Cmd|ui.Alt, ui.KeyLeft)
	tt.Frame()
	if a.tab().Focus != first {
		t.Fatal("Cmd+Option+Left did not focus the pane on the left")
	}
	tt.Key(ui.Cmd|ui.Alt, ui.KeyRight)
	tt.Frame()
	if a.tab().Focus != second {
		t.Fatal("Cmd+Option+Right did not focus the pane on the right")
	}
}

// A focus switch must leave the cards' entrance fades alone. unzoom
// used to mark every pane to fade in even when nothing was zoomed,
// which blinked the whole workspace on each Option+Cmd+arrow.
func TestPaneFocusDoesNotReplayEntrance(t *testing.T) {
	registerFonts()
	a := newProjectApp(t)
	first := a.tab().Focus
	a.splitWith(a.store.ByRelPath("Reading notes.md"))
	a.harness = true
	tt := ui.NewTester(a.view, 1100, 700)
	tt.Frame()
	tt.Frame()
	tt.Key(ui.Cmd|ui.Alt, ui.KeyLeft)
	tt.Frame()
	if a.tab().Focus != first {
		t.Fatal("Cmd+Option+Left did not move the focus")
	}
	for _, p := range a.tab().panes() {
		if p.enter {
			t.Fatalf("pane %s replays its entrance fade after a focus switch", p.ID)
		}
	}
}

func TestPaneSplitShortcutsSplitRightAndDown(t *testing.T) {
	for _, accel := range []string{cmdSplitRight.Default(), cmdSplitDown.Default()} {
		mods, key, ok := parseAccel(accel)
		if !ok {
			t.Fatalf("%q does not parse", accel)
		}
		a := newProjectApp(t)
		a.harness = true
		tt := ui.NewTester(a.view, 1100, 700)
		tt.Frame()
		tt.Key(mods, key)
		tt.Frame()
		if got := len(a.tab().panes()); got != 2 {
			t.Fatalf("%s left %d panes, want 2", accel, got)
		}
	}
}

func TestPaneZoomShortcutTogglesTheZoom(t *testing.T) {
	a := newProjectApp(t)
	a.harness = true
	tt := ui.NewTester(a.view, 1100, 700)
	tt.Frame()
	focus := a.tab().Focus
	tt.Key(ui.Cmd|ui.Shift, ui.KeyEnter)
	tt.Frame()
	if a.tab().Zoom != focus {
		t.Fatal("Shift+Cmd+Enter did not zoom the focused pane")
	}
	tt.Key(ui.Cmd|ui.Shift, ui.KeyEnter)
	tt.Frame()
	if a.tab().Zoom != nil {
		t.Fatal("Shift+Cmd+Enter did not restore the panes")
	}
}

func TestMovePaneDirectionInsertsOnEdge(t *testing.T) {
	a := newProjectApp(t)
	first := a.tab().Focus
	a.splitWith(a.store.ByRelPath("Reading notes.md"))
	second := a.tab().Focus
	a.splitWithSide(a.store.ByRelPath("scripts/tasks.py"), splitDown)
	third := a.tab().Focus

	a.movePaneDirection(paneUp)
	root := a.tab().Root
	if root == nil || root.Vertical || !root.B.Vertical || root.B.A.Pane != third || root.B.B.Pane != second {
		t.Fatalf("moving up did not put the pane above its neighbor: %+v", root)
	}
	if a.tab().Focus != third {
		t.Fatal("moving a pane lost its focus")
	}

	a.movePaneDirection(paneLeft)
	panes := a.tab().panes()
	if len(panes) != 3 || panes[0] != third || panes[1] != first || panes[2] != second {
		t.Fatalf("moving left did not insert the pane before its neighbor: %v", panes)
	}
}

func TestDropOnEdgeInsertsPane(t *testing.T) {
	a := newProjectApp(t)
	first := a.tab().Focus
	a.splitWith(a.store.ByRelPath("Reading notes.md"))
	second := a.tab().Focus
	a.movePaneTo(a.tab(), first, second, dropRight)
	panes := a.tab().panes()
	if len(panes) != 2 || panes[0] != second || panes[1] != first {
		t.Fatalf("dropping on the right edge did not insert the pane there: %v", panes)
	}
	if a.tab().Focus != first {
		t.Fatal("the dragged pane is not focused after the drop")
	}
}

func TestPaneHeaderStartsADrag(t *testing.T) {
	a := newProjectApp(t)
	first := a.tab().Focus
	a.splitWith(a.store.ByRelPath("Reading notes.md"))
	tt := ui.NewTester(a.view, 1100, 700)
	tt.Frame()
	handle, ok := tt.Find("Move Pane " + paneName(first))
	if !ok {
		t.Fatal("no move handle for the pane")
	}
	tt.Press(handle.X+handle.W/2, handle.Y+handle.H/2)
	tt.Frame()
	tt.Move(handle.X+20, handle.Y+20)
	tt.Frame()
	if !first.dragging {
		t.Fatal("dragging the pane header did not start a drag")
	}
	tt.Release(handle.X+20, handle.Y+20)
	tt.Frame()
}

func TestOpenedFileFillsAnEmptyTab(t *testing.T) {
	a := newProjectApp(t)
	if a.tab().Root == nil || a.tab().Root.Pane == nil {
		t.Fatalf("the new tab has no pane: %+v", a.tab())
	}
	if got := a.terminalDir(); got != a.store.Dir {
		t.Fatalf("terminal directory: %q", got)
	}
}

func TestEmptyPaneSearchOpensFile(t *testing.T) {
	a := newProjectApp(t)
	a.splitFocusedSide(splitRight)
	tt := ui.NewTester(a.view, 1100, 700)
	tt.Frame()
	p := a.tab().Focus
	if p == nil || p.Note != nil {
		t.Fatal("the split did not focus an empty pane")
	}
	tt.Type("Reading")
	tt.Frame()
	tt.Key(0, ui.KeyEnter)
	tt.Frame()
	if p.Note == nil || p.Note.Title != "Reading notes" {
		t.Fatalf("the empty pane search opened %+v", p.Note)
	}
	if a.tab().Focus != p {
		t.Fatal("the opened file lost the pane focus")
	}
}

func TestEmptyPaneSearchFiltersAssets(t *testing.T) {
	a := newProjectApp(t)
	choices := a.paneChoices("tasks")
	if len(choices) != 1 || choices[0].note == nil || choices[0].rel != filepath.Join("scripts", "tasks.py") {
		t.Fatalf("search choices: %+v", choices)
	}
	if a.paneChoices("no such file") != nil {
		t.Fatal("an impossible query returned choices")
	}
}

func TestSplitDividerResizesPanes(t *testing.T) {
	registerFonts()
	a := newProjectApp(t)
	a.splitWith(a.store.ByRelPath("Reading notes.md"))
	tt := ui.NewTester(a.view, 1100, 700)
	tt.Frame()
	tt.Frame()
	root := a.tab().Root
	if root == nil || root.Pane != nil || root.Share != 50 {
		t.Fatalf("split: %+v", root)
	}
	first, second := a.tab().panes()[0], a.tab().panes()[1]
	if first.lastBounds.W != second.lastBounds.W {
		t.Fatalf("the split did not start even: %.1f vs %.1f", first.lastBounds.W, second.lastBounds.W)
	}
	d, ok := tt.Find("Divider")
	if !ok {
		t.Fatalf("no divider: %q", tt.Texts())
	}
	cx, cy := d.X+d.W/2, d.Y+d.H/2
	tt.Move(cx, cy)
	tt.Frame()
	if got := tt.Cursor(); got != ui.CursorResizeEW {
		t.Fatalf("the divider shows cursor %v, want a horizontal resize", got)
	}
	tt.Press(cx, cy)
	tt.Frame()
	tt.Move(cx+80, cy)
	tt.Frame()
	tt.Release(cx+80, cy)
	tt.Frame()
	tt.Frame()
	if root.Share <= 50 {
		t.Fatalf("dragging right did not grow the first pane: share %.1f", root.Share)
	}
	if first.lastBounds.W <= second.lastBounds.W {
		t.Fatalf("the panes did not follow the divider: %.1f vs %.1f", first.lastBounds.W, second.lastBounds.W)
	}
	// A double click puts the divider back in the middle.
	if err := tt.Click("Divider"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if err := tt.Click("Divider"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	tt.Frame()
	if root.Share != 50 {
		t.Fatalf("a double click did not reset the split: %.1f", root.Share)
	}
}

func TestSplitDividerArrowKeysMoveIt(t *testing.T) {
	registerFonts()
	a := newProjectApp(t)
	a.splitWith(a.store.ByRelPath("Reading notes.md"))
	tt := ui.NewTester(a.view, 1100, 700)
	tt.Frame()
	tt.Frame()
	root := a.tab().Root
	d, ok := tt.Find("Divider")
	if !ok {
		t.Fatalf("no divider: %q", tt.Texts())
	}
	cx, cy := d.X+d.W/2, d.Y+d.H/2
	tt.Press(cx, cy)
	tt.Frame()
	tt.Release(cx, cy)
	tt.Frame()
	before := root.Share
	tt.Key(0, ui.KeyRight)
	tt.Frame()
	tt.Frame()
	if root.Share <= before {
		t.Fatalf("the right arrow did not move the divider: %.1f", root.Share)
	}
}

func TestSplitShareKeepsBothPanesVisible(t *testing.T) {
	if got := splitShare(&node{}, 0, minPaneW); got != 50 {
		t.Fatalf("a split before its first layout: %.1f", got)
	}
	if got := splitShare(&node{Share: 10}, 1000, minPaneW); got <= 10 {
		t.Fatalf("a share narrower than the minimum was kept: %.1f", got)
	}
	if got := splitShare(&node{Share: 90}, 1000, minPaneW); got >= 90 {
		t.Fatalf("a share wider than the maximum was kept: %.1f", got)
	}
	if got := splitShare(&node{Share: 90}, 200, minPaneW); got != 90 {
		t.Fatalf("a window too narrow to clamp changed the stored share: %.1f", got)
	}
	a := &app{}
	n := &node{Share: 50}
	if !a.resizeSplit(n, 100, 1000, minPaneW) {
		t.Fatal("the drag did not move the divider")
	}
	if n.Share < 87 || n.Share > 88 {
		t.Fatalf("the share was not clamped to the minimum pane: %.2f", n.Share)
	}
}

func TestSplitVerticalDividerResizesPanes(t *testing.T) {
	registerFonts()
	a := newProjectApp(t)
	a.splitWithSide(a.store.ByRelPath("Reading notes.md"), splitDown)
	tt := ui.NewTester(a.view, 1100, 700)
	tt.Frame()
	tt.Frame()
	root := a.tab().Root
	if root == nil || !root.Vertical {
		t.Fatalf("split: %+v", root)
	}
	first, second := a.tab().panes()[0], a.tab().panes()[1]
	d, ok := tt.Find("Divider")
	if !ok {
		t.Fatalf("no divider: %q", tt.Texts())
	}
	cx, cy := d.X+d.W/2, d.Y+d.H/2
	tt.Move(cx, cy)
	tt.Frame()
	if got := tt.Cursor(); got != ui.CursorResizeNS {
		t.Fatalf("the divider shows cursor %v, want a vertical resize", got)
	}
	tt.Press(cx, cy)
	tt.Frame()
	tt.Move(cx, cy+60)
	tt.Frame()
	tt.Release(cx, cy+60)
	tt.Frame()
	tt.Frame()
	if root.Share <= 50 {
		t.Fatalf("dragging down did not grow the first pane: share %.1f", root.Share)
	}
	if first.lastBounds.H <= second.lastBounds.H {
		t.Fatalf("the panes did not follow the divider: %.1f vs %.1f", first.lastBounds.H, second.lastBounds.H)
	}
}

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
