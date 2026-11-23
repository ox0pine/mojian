package app

import (
	"fmt"

	"mojian/internal/app/vim"
	"mojian/internal/lsp"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
	"github.com/egoist/mygo/ui/markdown"
)

// pane is one editor view of a file, a picture, or a terminal.
type pane struct {
	ID    string
	Note  *note
	Asset *assetFile
	Term  *termTab
	state *editor.State

	// Preview shows Markdown rendered rather than as source; assetView
	// holds what a picture or a PDF viewer has loaded.
	Preview         bool
	previewScroll   ui.ScrollState
	md              *markdown.Options
	assetView       *assetView
	dragging        bool
	closing         bool // fading out before the split closes
	closeStarted    bool
	hideStarted     bool    // a zoom hid it; its fade over the old place has begun
	lastBounds      ui.Rect // where the card showed, for its overlay
	enter           bool    // fades and slides in as a new pane, or back from a zoom
	enterX          float32
	enterY          float32
	emptyQuery      string // the file search in an empty pane
	emptySel        int
	blameLine       int
	blameVer        uint64
	blameSeq        int
	blameText       string
	blameInlay      *editor.Inlay
	inlayNote       *note
	inlayVer        uint64
	inlaySeq        int
	inlayLoaded     bool
	inlayHints      []editor.Inlay
	sigOffset       int
	sigVer          uint64
	sigSeq          int
	sigHelp         *lsp.SignatureHelp
	sigDismissedVer uint64
	sigDismissedOff int
	symNote         *note
	symVer          uint64
	symSeq          int
	symLoaded       bool
	symLSP          bool
	symList         []lsp.Symbol
	symLine         int
	symCrumb        string
	hlOffset        int
	hlVer           uint64
	hlSeq           int
	hlRanges        []editor.TextBackground
	indentOverride  editor.IndentStyle
	tabSizeOverride int
	lastScrollY     float64
	vim             *vim.State
}

// assetView is what a picture or PDF pane has loaded, and its headings.
type assetView struct {
	pdfDir     string
	pdfCount   int // the document's pages, 0 until the count is read
	pdfPage    int
	pdfErr     string // why the document could not be opened
	pdfLoading bool
	pdfStarted bool
	pdfGen     int            // invalidates work whose pane was repurposed
	pdfImage   ui.ImageSource // the page pdfImageAt was decoded for
	pdfImageAt int
	pdfFiles   map[int]string // page (0-based) to the PNG drawn for it
	pdfPending map[int]bool   // pages being drawn now
	pdfPageErr map[int]string // why a page could not be drawn
}

// node is a leaf pane, or two panes side by side or stacked. Share is the
// first side's percentage of the room, from 0 to 100; the divider between
// the two sides sets it, and zero takes half.
type node struct {
	Vertical bool // the panes are stacked, one above the other
	Share    float32
	A, B     *node
	Pane     *pane
}

// tab is a workspace: a tree of panes, the one with the focus, and the
// one zoomed, if any.
type tab struct {
	ID    string
	Name  string // a name of its own, else the focused note's title
	Root  *node
	Focus *pane
	Zoom  *pane
}

var idSeq int

func nextID(prefix string) string {
	idSeq++
	return fmt.Sprintf("%s%d", prefix, idSeq)
}

func newPane(n *note) *pane {
	return &pane{ID: nextID("p"), Note: n}
}

func newTermPane(t *termTab) *pane {
	return &pane{ID: nextID("p"), Term: t}
}

func newTermPaneSpec(spec termSpec, sid string) *termTab {
	return &termTab{Spec: spec, SID: sid}
}

func newAssetPane(f *assetFile) *pane {
	return &pane{ID: nextID("p"), Asset: f, assetView: &assetView{}}
}

// panes returns the panes of the tab, in layout order.
func (t *tab) panes() []*pane {
	var out []*pane
	t.Root.walk(func(p *pane) { out = append(out, p) })
	return out
}

func (n *node) walk(fn func(*pane)) {
	if n == nil {
		return
	}
	if n.Pane != nil {
		fn(n.Pane)
		return
	}
	n.A.walk(fn)
	n.B.walk(fn)
}

func newTab(n *note) *tab {
	p := newPane(n)
	return &tab{ID: nextID("t"), Root: &node{Pane: p}, Focus: p}
}

// label is what the tab strip shows: the focused note's title, or the
// tab's own name, and how many panes it holds.
func (t *tab) label() (name, detail string) {
	name = t.Name
	if name == "" && t.Focus != nil {
		name = paneName(t.Focus)
	}
	if name == "" {
		name = "Welcome"
	}
	if n := len(t.panes()); n > 1 {
		detail = fmt.Sprintf("· %d", n)
	}
	return name, detail
}

// editorPane returns the pane to focus when the tab appears: an editor
// if it has one, else the terminal.
func (t *tab) editorPane() *pane {
	if t == nil {
		return nil
	}
	if t.Focus != nil && (t.Focus.Note != nil || t.Focus.Asset != nil) {
		return t.Focus
	}
	if p := t.firstEditorPane(); p != nil {
		return p
	}
	return firstPane(t.Root)
}

// firstEditorPane returns the first pane that shows a file, not a shell.
func (t *tab) firstEditorPane() *pane {
	for _, p := range t.panes() {
		if p.Note != nil || p.Asset != nil {
			return p
		}
	}
	return nil
}

// terminalPane returns the tab's terminal, if it has one.
func (t *tab) terminalPane() *pane {
	for _, p := range t.panes() {
		if p.Term != nil {
			return p
		}
	}
	return nil
}

// dirty reports whether any note of the tab has unsaved edits.
func (t *tab) dirty() bool {
	for _, p := range t.panes() {
		if p.Note != nil && p.Note.Dirty() {
			return true
		}
	}
	return false
}

// splitSide is where a split puts the new pane relative to the pane it
// splits.
type splitSide int

const (
	splitRight splitSide = iota
	splitDown
	splitLeft
	splitUp
)

func (s splitSide) vertical() bool { return s == splitDown || s == splitUp }
func (s splitSide) before() bool   { return s == splitLeft || s == splitUp }

// paneDrop is where a dragged pane is let go over another pane: the
// middle does nothing, an edge inserts it on that side.
type paneDrop int

const (
	dropNone paneDrop = iota - 1
	dropCenter
	dropAbove
	dropBelow
	dropLeft
	dropRight
)

func (d paneDrop) vertical() bool { return d == dropAbove || d == dropBelow }
func (d paneDrop) before() bool   { return d == dropAbove || d == dropLeft }

// splitSide puts with to one side of p. It reports whether p is in this
// tab.
func (t *tab) splitSide(p, with *pane, side splitSide) bool {
	if t.Root == nil || p == nil || with == nil || p == with || t.contains(with) {
		return false
	}
	if !t.Root.splitSide(p, with, side) {
		return false
	}
	t.Focus = with
	t.Zoom = nil
	return true
}

func (n *node) splitSide(p, with *pane, side splitSide) bool {
	if n == nil {
		return false
	}
	if n.Pane != nil {
		if n.Pane != p {
			return false
		}
		old := n.Pane
		left, right := &node{Pane: old}, &node{Pane: with}
		if side.before() {
			left, right = right, left
		}
		*n = node{Vertical: side.vertical(), Share: 50, A: left, B: right}
		return true
	}
	if n.A != nil && n.A.splitSide(p, with, side) {
		return true
	}
	return n.B != nil && n.B.splitSide(p, with, side)
}

// movePane takes from out of the tree and puts it on the edge of to.
func (t *tab) movePane(from, to *pane, zone paneDrop) bool {
	if t == nil || from == nil || to == nil || from == to || t.Root == nil {
		return false
	}
	if !t.contains(from) || !t.contains(to) || zone == dropNone || zone == dropCenter {
		return false
	}
	if t.Root.Pane == from {
		return false // from is the only pane; to cannot also be here
	}
	if !t.Root.remove(from) {
		return false
	}
	if !t.wrapPane(to, from, zone) {
		return false
	}
	t.Focus = from
	t.Zoom = nil
	return true
}

// wrapPane replaces to's leaf with a split holding from on the named
// side. to must still be in the tree.
func (t *tab) wrapPane(to, from *pane, zone paneDrop) bool {
	if t.Root == nil || !t.contains(to) || zone == dropNone || zone == dropCenter {
		return false
	}
	split := &node{Vertical: zone.vertical(), Share: 50}
	toLeaf, fromLeaf := &node{Pane: to}, &node{Pane: from}
	if zone.before() {
		split.A, split.B = fromLeaf, toLeaf
	} else {
		split.A, split.B = toLeaf, fromLeaf
	}
	if t.Root.Pane == to {
		t.Root = split
		return true
	}
	return t.Root.wrapLeaf(to, split)
}

// wrapLeaf replaces the leaf showing p with replacement.
func (n *node) wrapLeaf(p *pane, replacement *node) bool {
	if n == nil || n.Pane != nil {
		return false
	}
	if n.A != nil && n.A.Pane == p {
		n.A = replacement
		return true
	}
	if n.B != nil && n.B.Pane == p {
		n.B = replacement
		return true
	}
	if n.A != nil && n.A.wrapLeaf(p, replacement) {
		return true
	}
	return n.B != nil && n.B.wrapLeaf(p, replacement)
}

// paneInDirection finds the nearest pane in a direction, climbing out
// of splits that run the other way.
func (t *tab) paneInDirection(dir paneDirection) *pane {
	if t.Root == nil || t.Focus == nil {
		return nil
	}
	path := t.Root.pathTo(t.Focus)
	for i := len(path) - 2; i >= 0; i-- {
		n := path[i]
		child := path[i+1]
		var target *pane
		switch {
		case n.Vertical && child == n.B && dir == paneUp:
			target = lastPane(n.A)
		case n.Vertical && child == n.A && dir == paneDown:
			target = firstPane(n.B)
		case !n.Vertical && child == n.B && dir == paneLeft:
			target = lastPane(n.A)
		case !n.Vertical && child == n.A && dir == paneRight:
			target = firstPane(n.B)
		}
		if target != nil && target != t.Focus {
			return target
		}
	}
	return nil
}

// paneDirection is one of the four screen directions.
type paneDirection int

const (
	paneLeft paneDirection = iota
	paneRight
	paneUp
	paneDown
)

// pathTo returns the nodes from the root to p, or nil.
func (n *node) pathTo(p *pane) []*node {
	if n == nil {
		return nil
	}
	if n.Pane == p {
		return []*node{n}
	}
	if n.A != nil {
		if path := n.A.pathTo(p); path != nil {
			return append([]*node{n}, path...)
		}
	}
	if n.B != nil {
		if path := n.B.pathTo(p); path != nil {
			return append([]*node{n}, path...)
		}
	}
	return nil
}

func lastPane(n *node) *pane {
	if n == nil {
		return nil
	}
	if n.Pane != nil {
		return n.Pane
	}
	if p := lastPane(n.B); p != nil {
		return p
	}
	return lastPane(n.A)
}

// closePane removes p. It returns false when the tab is left with no
// pane.
func (t *tab) closePane(p *pane) bool {
	if t.Root == nil {
		return false
	}
	if t.Root.Pane == p {
		t.Root = nil
		t.Focus, t.Zoom = nil, nil
		return false
	}
	if !t.Root.remove(p) {
		return true
	}
	if t.Zoom == p {
		t.Zoom = nil
	}
	if t.Focus == p {
		t.Focus = firstPane(t.Root)
	}
	return true
}

func (n *node) remove(p *pane) bool {
	if n == nil {
		return false
	}
	if n.A != nil && n.A.Pane == p {
		n.adopt(n.B)
		return true
	}
	if n.B != nil && n.B.Pane == p {
		n.adopt(n.A)
		return true
	}
	if n.A != nil && n.A.remove(p) {
		return true
	}
	return n.B != nil && n.B.remove(p)
}

// adopt makes n the node other, as a split collapses into its one
// remaining pane, or into the tree of the other side.
func (n *node) adopt(other *node) {
	if other == nil {
		*n = node{}
		return
	}
	*n = *other
}

func firstPane(n *node) *pane {
	if n == nil {
		return nil
	}
	if n.Pane != nil {
		return n.Pane
	}
	if p := firstPane(n.A); p != nil {
		return p
	}
	return firstPane(n.B)
}

func (t *tab) contains(p *pane) bool {
	return t.Root.pathTo(p) != nil
}

// enterSide points a new pane's entrance away from where it lands, so it
// slides in from the edge of the split: a pane on the right starts to the
// right and slides left into place.
func (p *pane) enterSide(side splitSide) {
	const d = float32(28)
	p.enter = true
	switch side {
	case splitLeft:
		p.enterX = -d
	case splitRight:
		p.enterX = d
	case splitUp:
		p.enterY = -d
	case splitDown:
		p.enterY = d
	}
}

// emptyPaneSide opens a blank pane on the named side of the focused one,
// ready for the next file opened from the Explorer or the palette.
func (a *app) emptyPaneSide(side splitSide) {
	t := a.tab()
	if t == nil || t.Focus == nil {
		return
	}
	with := &pane{ID: nextID("p")}
	with.enterSide(side)
	if !t.splitSide(t.Focus, with, side) {
		return
	}
	t.Focus = with
	a.focusReq = nil
	a.changed()
	a.invalidate()
}

// focusPaneDirection gives the focus to the pane on the side, and no
// pane there leaves it where it is.
func (a *app) focusPaneDirection(dir paneDirection) {
	t := a.tab()
	if t == nil {
		return
	}
	target := t.paneInDirection(dir)
	if target == nil || target == t.Focus {
		return
	}
	a.unzoom(t)
	t.Focus = target
	a.focusReq = target
	a.changed()
	a.invalidate()
}

func (a *app) movePaneDirection(dir paneDirection) {
	t := a.tab()
	if t == nil || t.Focus == nil {
		return
	}
	target := t.paneInDirection(dir)
	if target == nil || target == t.Focus {
		return
	}
	from := t.Focus
	zone := dropLeft
	switch dir {
	case paneRight:
		zone = dropRight
	case paneUp:
		zone = dropAbove
	case paneDown:
		zone = dropBelow
	}
	a.clearZoom(t)
	if !t.movePane(from, target, zone) {
		return
	}
	t.Focus = from
	a.focusReq = from
	a.changed()
	a.invalidate()
}

const (
	// minPaneW and minPaneH are the narrowest a divider leaves a pane,
	// in DIPs: enough for the header's buttons and a line of text. The
	// split keeps the share the user chose when the window cannot show
	// both minimums, and takes it back when there is room again.
	minPaneW = 120
	minPaneH = 90
	// splitStep is how far an arrow key moves a focused divider, in DIPs.
	splitStep = 10
	// splitInset is how far the divider's visible line stays from the
	// ends of the gap, so it reads as a handle rather than a rule.
	splitInset = 8
)

// splitShare returns the percent of its room a split gives its first side,
// keeping both panes at least minSize wide when the room allows it. The
// stored share is left alone: a window too small to show it does not
// overwrite the size the user chose.
func splitShare(n *node, room, minSize float32) float32 {
	share := float32(50)
	if n != nil && n.Share > 0 && n.Share < 100 {
		share = n.Share
	}
	if room <= 0 {
		return share
	}
	usable := room - gap
	if usable <= 2*minSize {
		return min(max(share, 1), 99)
	}
	minPct := minSize / usable * 100
	return min(max(share, minPct), 100-minPct)
}

// resizeSplit moves a split's divider by delta percentage points, clamped
// so both panes keep their minimum. It reports whether the share moved.
func (a *app) resizeSplit(n *node, delta, room, minSize float32) bool {
	if n == nil || room <= 0 || delta == 0 {
		return false
	}
	share := splitShare(n, room, minSize) + delta
	usable := room - gap
	if usable > 2*minSize {
		minPct := minSize / usable * 100
		share = min(max(share, minPct), 100-minPct)
	} else {
		share = min(max(share, 1), 99)
	}
	if share == n.Share {
		return false
	}
	n.Share = share
	a.changed()
	return true
}

// splitDivider draws the gap between the two sides of a split as a
// draggable handle: the pointer over it shows a resize cursor, dragging it
// resizes the panes, and the arrows move it once it has the keyboard
// focus. room is the split's size along its axis from the frame before.
func (a *app) splitDivider(c *ui.Context, k *colors, n *node, room float32) {
	minSize := float32(minPaneW)
	if n.Vertical {
		minSize = minPaneH
	}
	d := ui.Box(c).Shrink(0).Focusable().FocusRing(false).
		Role(ui.RoleSplitter).Label("Divider")
	if n.Vertical {
		d.Height(gap).Cursor(ui.CursorResizeNS)
	} else {
		d.Width(gap).Cursor(ui.CursorResizeEW)
	}
	dx, dy, held := d.Dragged()
	if held {
		d.Focus()
		delta := dx
		if n.Vertical {
			delta = dy
		}
		if usable := room - gap; usable > 0 && a.resizeSplit(n, delta/usable*100, room, minSize) {
			// The layout this frame used the share from before the move;
			// draw the new one next.
			a.invalidate()
		}
	}
	back, forth := ui.KeyLeft, ui.KeyRight
	if n.Vertical {
		back, forth = ui.KeyUp, ui.KeyDown
	}
	step := float32(0)
	if usable := room - gap; usable > 0 {
		step = splitStep / usable * 100
	}
	d.OnShortcut(0, back, func() {
		if a.resizeSplit(n, -step, room, minSize) {
			a.invalidate()
		}
	})
	d.OnShortcut(0, forth, func() {
		if a.resizeSplit(n, step, room, minSize) {
			a.invalidate()
		}
	})
	if d.DoubleClicked() && n.Share != 50 {
		n.Share = 50
		a.changed()
		a.invalidate()
	}
	hovered := d.Hovered()
	focused := d.FocusVisible()
	// The draw callback outlives the build pass, so it may not ask the
	// element anything: take what it shows now.
	show := hovered || held || focused
	col := k.TextFaint
	if held || focused {
		col = k.Text
	}
	vertical := n.Vertical
	d.Draw(func(p *ui.Painter, r ui.Rect) {
		if !show {
			return
		}
		if vertical {
			p.Fill(ui.Rect{X: r.X + splitInset, Y: r.Y + r.H/2 - 1, W: max(r.W-2*splitInset, 0), H: 2}, col, 1)
		} else {
			p.Fill(ui.Rect{X: r.X + r.W/2 - 1, Y: r.Y + splitInset, W: 2, H: max(r.H-2*splitInset, 0)}, col, 1)
		}
	})
}
