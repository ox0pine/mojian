package app

import (
	"context"
	"errors"
	"fmt"

	"github.com/egoist/mygo/ui/editor"
)

// tab returns the active workspace.
func (a *app) tab() *tab {
	if a.active < 0 || a.active >= len(a.tabs) {
		return nil
	}
	return a.tabs[a.active]
}

func (a *app) currentNote() *note {
	if t := a.tab(); t != nil && t.Focus != nil {
		return t.Focus.Note
	}
	return nil
}

// focusFirstNote opens the most recent note, in the first tab.
func (a *app) focusFirstNote() {
	if a.store == nil {
		return
	}
	notes := a.store.Sorted()
	if len(notes) == 0 {
		for _, f := range a.store.Assets {
			t := &tab{ID: nextID("t")}
			p := newAssetPane(f)
			t.Root = &node{Pane: p}
			t.Focus = p
			a.tabs = []*tab{t}
			a.active = 0
			a.focusReq = p
			a.revealTab = true
			a.revealRel(f.Rel)
			return
		}
		return
	}
	n := notes[0]
	if !a.loadNote(n) {
		return
	}
	a.tabs = []*tab{newTab(n)}
	a.active = 0
	a.focusReq = a.tabs[0].Focus
	a.revealTab = true
	a.revealInTree(n)
}

// newTab adds an empty workspace to the right of the tabs. It does not
// copy the current file; picking or creating a file fills it.
func (a *app) newTab() {
	a.git.Open = false
	t := &tab{ID: nextID("t")}
	a.tabs = append(a.tabs, t)
	a.active = len(a.tabs) - 1
	a.focusReq = nil
	a.revealTab = true
	a.treeDraft = nil
	a.changed()
	a.invalidate()
}

func (a *app) isAsyncRemote() bool {
	return a.store != nil && a.store.IsRemote() && a.uiPoster() != nil
}

// startLoadNote fetches an unloaded remote note off the UI thread so slow SSH
// links never freeze the window.
func (a *app) startLoadNote(p *pane, n *note) {
	if p == nil || n == nil || a.store == nil {
		return
	}
	if n.Loaded {
		p.loading = false
		p.loadErr = ""
		a.applyPendingPanePos(p)
		a.invalidate()
		return
	}
	if !a.isAsyncRemote() {
		if err := a.store.LoadNote(n); err != nil {
			p.loadErr = err.Error()
			a.fail(fmt.Errorf("reading %s: %w", n.Rel, err))
			a.invalidate()
			return
		}
		p.loadErr = ""
		a.applyPendingPanePos(p)
		a.invalidate()
		return
	}
	p.cancelLoad()
	p.loading = true
	p.loadSeq++
	seq := p.loadSeq
	ctx, cancel := context.WithCancel(context.Background())
	p.loadCancel = cancel
	s := a.store
	ws := a.workspace
	rel := n.Rel
	winSeq := a.windowSeq
	post := a.uiPoster()
	go func() {
		body, err := s.ReadNoteContext(ctx, rel)
		post(func() {
			if a.windowSeq != winSeq || p.loadSeq != seq || p.Note != n || ws == nil || ws.store != s {
				cancel()
				return
			}
			p.loading = false
			p.loadCancel = nil
			cancel()
			if err != nil {
				if !errors.Is(err, context.Canceled) {
					p.loadErr = err.Error()
				}
				if a.workspace == ws {
					a.invalidate()
				}
				return
			}
			s.CommitLoadedNote(n, body)
			a.applyPendingPanePos(p)
			for _, t := range ws.tabs {
				for _, other := range t.panes() {
					if other != p && other.Note == n {
						other.cancelLoad()
						a.applyPendingPanePos(other)
					}
				}
			}
			if a.workspace == ws {
				a.invalidate()
			}
		})
	}()
	a.invalidate()
}

// startLoadAsset fetches an unloaded remote image or PDF off the UI thread.
func (a *app) startLoadAsset(p *pane, f *assetFile) {
	if p == nil || f == nil || a.store == nil {
		return
	}
	if f.IsBinary() || !a.store.IsRemote() || f.Local != "" {
		p.loading = false
		p.loadErr = ""
		a.invalidate()
		return
	}
	if !a.isAsyncRemote() {
		_ = a.loadAsset(f)
		return
	}
	p.cancelLoad()
	p.loading = true
	p.loadSeq++
	seq := p.loadSeq
	ctx, cancel := context.WithCancel(context.Background())
	p.loadCancel = cancel
	s := a.store
	ws := a.workspace
	rel := f.Rel
	size := f.Size
	mod := f.Mod
	hash := f.Hash
	isBinary := f.IsBinary()
	path := f.Path
	winSeq := a.windowSeq
	post := a.uiPoster()
	go func() {
		localPath, err := s.FetchAssetContext(ctx, rel, size, mod, hash, isBinary, path)
		post(func() {
			if a.windowSeq != winSeq || p.loadSeq != seq || p.Asset != f || ws == nil || ws.store != s {
				cancel()
				return
			}
			p.loading = false
			p.loadCancel = nil
			cancel()
			if err != nil {
				if !errors.Is(err, context.Canceled) {
					p.loadErr = err.Error()
				}
				if a.workspace == ws {
					a.invalidate()
				}
				return
			}
			s.CommitMaterializedAsset(f, localPath)
			for _, t := range ws.tabs {
				for _, other := range t.panes() {
					if other != p && other.Asset == f {
						other.cancelLoad()
					}
				}
			}
			if a.workspace == ws {
				a.invalidate()
			}
		})
	}()
	a.invalidate()
}

// applyPendingPanePos applies a deferred cursor/selection target once n is loaded.
func (a *app) applyPendingPanePos(p *pane) {
	if p == nil || p.Note == nil || !p.Note.Loaded || !p.hasPendingPos {
		return
	}
	if p.state == nil {
		p.state = p.Note.NewState()
	}
	lineIdx := max(p.pendingLine-1, 0)
	colIdx := max(p.pendingCol, 0)
	p.state.GoTo(editor.Pos{Line: lineIdx, Col: colIdx})
	if p.pendingEnd > colIdx {
		startOff := p.state.Cursor()
		p.state.Select(startOff, startOff+(p.pendingEnd-colIdx))
	}
	pos := p.state.CursorPos()
	p.lastSavedLine = pos.Line
	p.lastSavedCol = pos.Col
	p.hasPendingPos = false
}

// loadNote reads a remote note's text before its pane is built. A local
// note is already loaded; a remote one is fetched once.
func (a *app) loadNote(n *note) bool {
	if n == nil {
		return false
	}
	if a.store != nil {
		if err := a.store.LoadNote(n); err != nil {
			a.fail(fmt.Errorf("reading %s: %w", n.Rel, err))
			return false
		}
	}
	return true
}

// loadAsset fetches a remote picture or PDF into the local cache its
// viewer reads.
func (a *app) loadAsset(f *assetFile) bool {
	if f == nil {
		return false
	}
	if a.store != nil {
		if _, err := a.store.MaterializeAsset(f); err != nil {
			a.fail(fmt.Errorf("reading %s: %w", f.Rel, err))
			return false
		}
	}
	return true
}

// openNoteInTab shows n in a tab of its own.
func (a *app) openNoteInTab(n *note) {
	if n == nil {
		return
	}
	if !a.isAsyncRemote() && !a.loadNote(n) {
		return
	}
	a.git.Open = false
	t := newTab(n)
	a.tabs = append(a.tabs, t)
	a.active = len(a.tabs) - 1
	a.focusReq = t.Focus
	a.revealTab = true
	a.touchMRU(n.Rel)
	a.treeSel = n.Rel
	a.openPath(n.Rel)
	a.treeReveal = n.Rel
	if a.isAsyncRemote() && !n.Loaded {
		a.startLoadNote(t.Focus, n)
	}
	a.changed()
	a.invalidate()
}

// openNote shows n in the focused pane, or in a new tab when none is
// open.
func (a *app) openNote(n *note) {
	if n == nil {
		return
	}
	if !a.isAsyncRemote() && !a.loadNote(n) {
		return
	}
	a.git.Open = false
	a.treeDraft = nil
	a.treeQuery = ""
	t := a.tab()
	if t == nil {
		t = newTab(n)
		a.tabs = append(a.tabs, t)
		a.active = len(a.tabs) - 1
		a.focusReq = t.Focus
		a.revealTab = true
		a.touchMRU(n.Rel)
		a.revealRel(n.Rel)
		if a.isAsyncRemote() && !n.Loaded {
			a.startLoadNote(t.Focus, n)
		}
		a.changed()
		a.invalidate()
		return
	}
	p := t.Focus
	if p != nil && (p.Term != nil || p.Search != nil) {
		p = t.firstEditorPane()
		if p == nil {
			a.openNoteInTab(n)
			return
		}
	}
	if p == nil {
		p = firstPane(t.Root)
	}
	if p != nil && (p.Term != nil || p.Search != nil) {
		// A tab can hold only terminals or search panes. A note needs an
		// editor pane, so give it a tab instead of replacing one.
		if editor := t.firstEditorPane(); editor != nil {
			p = editor
		} else {
			a.openNoteInTab(n)
			return
		}
	}
	if p == nil {
		p = newPane(n)
		t.Root = &node{Pane: p}
		t.Focus = p
	}
	p.cancelLoad()
	if p.Note != n {
		p.state = nil
		p.md = nil
		p.Preview = false
		p.hasPendingPos = false
		p.hasPendingScroll = false
	}
	if t.Zoom != nil && t.Zoom != p {
		a.unzoom(t)
	}
	releaseAsset(p)
	p.Note = n
	p.Asset = nil
	p.assetView = nil
	t.Focus = p
	a.focusReq = p
	a.touchMRU(n.Rel)
	a.revealRel(n.Rel)
	if a.isAsyncRemote() && !n.Loaded {
		a.startLoadNote(p, n)
	}
	a.changed()
	a.invalidate()
}

// openFileInSplit opens n or f in a right split of the active tab, or in place
// if no non-empty pane exists.
func (a *app) openFileInSplit(n *note, f *assetFile, line, col int) {
	if n == nil && f == nil {
		return
	}
	t := a.tab()
	if t == nil || t.Root == nil || (t.Focus != nil && t.Focus.Note == nil && t.Focus.Asset == nil && t.Focus.Term == nil && t.Focus.Search == nil) {
		if n != nil {
			if line > 0 {
				a.openNoteAt(n.Rel, line, col, col)
			} else {
				a.openNote(n)
			}
			return
		}
		a.openAsset(f)
		return
	}
	a.git.Open = false
	a.clearZoom(t)
	target := t.Focus
	if target == nil {
		target = firstPane(t.Root)
	}
	if target == nil {
		return
	}
	var newP *pane
	var rel string
	if n != nil {
		if !a.isAsyncRemote() && !a.loadNote(n) {
			return
		}
		newP = newPane(n)
		rel = n.Rel
		if line > 0 {
			newP.setPendingPos(line, col, 0, 0)
		}
	} else {
		if !a.isAsyncRemote() && !a.loadAsset(f) {
			return
		}
		newP = newAssetPane(f)
		rel = f.Rel
	}
	newP.enterSide(splitRight)
	if !t.splitSide(target, newP, splitRight) {
		return
	}
	t.Focus = newP
	a.focusReq = newP
	a.touchMRU(rel)
	a.revealRel(rel)
	if a.isAsyncRemote() {
		if n != nil && !n.Loaded {
			a.startLoadNote(newP, n)
		} else if f != nil && !f.IsBinary() && f.Local == "" {
			a.startLoadAsset(newP, f)
		} else if n != nil {
			a.applyPendingPanePos(newP)
		}
	} else if n != nil {
		a.applyPendingPanePos(newP)
	}
	a.changed()
	a.invalidate()
}

// openRel opens a path from the project: a text file in the editor, or a
// picture and PDF in a viewer.
func (a *app) openRel(rel string) {
	if a.store == nil {
		return
	}
	if n := a.store.ByRelPath(rel); n != nil {
		a.openNote(n)
		return
	}
	if f := a.store.AssetByRel(rel); f != nil {
		a.openAsset(f)
	}
}

// openNoteAt opens a project text file in editor mode and moves the caret
// to the 1-based line and 0-based byte span [col, end).
func (a *app) openNoteAt(rel string, line, col, end int) {
	if a.store == nil {
		return
	}
	n := a.store.ByRelPath(rel)
	if n == nil {
		return
	}
	a.openNote(n)
	p := a.focusPane()
	if p == nil || p.Note != n {
		return
	}
	p.Preview = false
	p.setPendingPos(line, col, end, 0)
	if n.Loaded {
		a.applyPendingPanePos(p)
	}
	a.focusReq = p
	a.invalidate()
}

// openAssetInTab shows a picture or PDF in a tab of its own.
func (a *app) openAssetInTab(f *assetFile) {
	if f == nil {
		return
	}
	if !a.isAsyncRemote() && !a.loadAsset(f) {
		return
	}
	a.git.Open = false
	t := &tab{ID: nextID("t")}
	p := newAssetPane(f)
	t.Root = &node{Pane: p}
	t.Focus = p
	a.tabs = append(a.tabs, t)
	a.active = len(a.tabs) - 1
	a.focusReq = p
	a.revealTab = true
	a.touchMRU(f.Rel)
	a.treeSel = f.Rel
	a.openPath(f.Rel)
	a.treeReveal = f.Rel
	if a.isAsyncRemote() && !f.IsBinary() && f.Local == "" {
		a.startLoadAsset(p, f)
	}
	a.changed()
	a.invalidate()
}

// openAsset shows a picture or PDF in the focused pane, or in a new tab
// when none is open.
func (a *app) openAsset(f *assetFile) {
	if f == nil {
		return
	}
	if !a.isAsyncRemote() && !a.loadAsset(f) {
		return
	}
	a.git.Open = false
	a.treeDraft = nil
	a.treeQuery = ""
	t := a.tab()
	if t == nil {
		t = &tab{ID: nextID("t")}
		p := newAssetPane(f)
		t.Root = &node{Pane: p}
		t.Focus = p
		a.tabs = append(a.tabs, t)
		a.active = len(a.tabs) - 1
		a.focusReq = p
		a.revealTab = true
		a.touchMRU(f.Rel)
		a.revealRel(f.Rel)
		if a.isAsyncRemote() && !f.IsBinary() && f.Local == "" {
			a.startLoadAsset(p, f)
		}
		a.changed()
		a.invalidate()
		return
	}
	p := t.Focus
	if p != nil && (p.Term != nil || p.Search != nil) {
		p = t.firstEditorPane()
	}
	if p == nil {
		p = firstPane(t.Root)
	}
	if p != nil && (p.Term != nil || p.Search != nil) {
		// A tab can hold only terminals or search panes. A picture or PDF
		// needs a viewer pane, so give it a tab instead of replacing one.
		if editor := t.firstEditorPane(); editor != nil {
			p = editor
		} else {
			a.openAssetInTab(f)
			return
		}
	}
	if p == nil {
		p = newAssetPane(f)
		t.Root = &node{Pane: p}
		t.Focus = p
	}
	p.cancelLoad()
	if t.Zoom != nil && t.Zoom != p {
		a.unzoom(t)
	}
	releaseAsset(p)
	p.Note = nil
	p.state = nil
	p.hasPendingPos = false
	p.hasPendingScroll = false
	p.Asset = f
	p.assetView = &assetView{}
	p.Preview = false
	t.Focus = p
	a.focusReq = p
	a.touchMRU(f.Rel)
	a.revealRel(f.Rel)
	if a.isAsyncRemote() && !f.IsBinary() && f.Local == "" {
		a.startLoadAsset(p, f)
	}
	a.changed()
	a.invalidate()
}

// closeTab closes a workspace.
func (a *app) closeTab(t *tab) {
	a.dropPaneOverlays(t)
	i := -1
	for j, x := range a.tabs {
		if x == t {
			i = j
			break
		}
	}
	if i < 0 {
		return
	}
	for _, p := range t.panes() {
		p.cancelLoad()
		a.recordClosedPane(t, p, true)
		a.endTerminalPane(p, true)
		releaseAsset(p)
	}
	a.tabs = append(a.tabs[:i], a.tabs[i+1:]...)
	wasActive := a.active == i
	switch {
	case len(a.tabs) == 0:
		a.active = -1
	case i < a.active:
		a.active--
	case wasActive:
		a.active = min(i, len(a.tabs)-1)
	}
	if a.rename == t {
		a.rename = nil
	}
	if next := a.tab(); next != nil {
		if next.Focus != nil {
			a.focusReq = next.Focus
		} else {
			a.focusReq = next.editorPane()
		}
		a.revealTab = true
		if next.Focus != nil {
			if next.Focus.Note != nil {
				a.revealRel(next.Focus.Note.Rel)
			} else if next.Focus.Asset != nil {
				a.revealRel(next.Focus.Asset.Rel)
			}
		}
	} else {
		a.focusReq = nil
	}
	a.reapRemotes()
	a.changed()
	a.invalidate()
}

// closePane closes the pane focused in its tab.
func (a *app) closePane(p *pane) {
	if p == nil || p.closing {
		return
	}
	t := a.tabOf(p)
	if t == nil {
		return
	}
	a.unzoom(t)
	rect := p.lastBounds
	if a.quitting || len(t.panes()) <= 1 || rect.W <= 1 || rect.H <= 1 {
		a.closePaneNow(t, p)
		return
	}
	a.recordClosedPane(t, p, false)
	// Take the pane out of the layout now, so the survivor expands at
	// once, and keep a copy of the card over its old place while it fades
	// and slides back the way it entered. The engine cannot keep an Exit
	// copy because the split node goes with the pane.
	p.closing = true
	p.closeStarted = false
	a.paneOverlays = append(a.paneOverlays, &paneOverlay{p: p, t: t, rect: rect, kind: paneCopyClosing})
	if !t.closePane(p) {
		a.closeTab(t)
		return
	}
	a.focusReq = t.Focus
	a.changed()
	a.invalidate()
}

// tabOf returns the tab holding p.
func (a *app) tabOf(p *pane) *tab {
	for _, t := range a.tabs {
		if t.contains(p) {
			return t
		}
	}
	return nil
}

// finishPaneOverlay drops a pane's copy once its fade has rested. A
// closing pane ends here; a pane a zoom hid stays loaded.
func (a *app) finishPaneOverlay(p *pane, kind paneCopy) {
	if p == nil {
		return
	}
	removed := false
	for i, o := range a.paneOverlays {
		if o.p == p && o.kind == kind {
			a.paneOverlays = append(a.paneOverlays[:i], a.paneOverlays[i+1:]...)
			removed = true
			break
		}
	}
	if kind == paneCopyHidden {
		if !removed {
			return
		}
		p.hideStarted = false
		a.invalidate()
		return
	}
	if !p.closing {
		return
	}
	p.cancelLoad()
	a.endTerminalPane(p, true)
	releaseAsset(p)
	p.closing = false
	p.closeStarted = false
	a.reapRemotes()
	a.changed()
	a.invalidate()
}

// dropPaneOverlays finishes every copy a tab has, as the tab goes away or
// another one is shown.
func (a *app) dropPaneOverlays(t *tab) {
	for _, o := range append([]*paneOverlay(nil), a.paneOverlays...) {
		if t != nil && o.t != t {
			continue
		}
		a.finishPaneOverlay(o.p, o.kind)
	}
}

// dropHiddenOverlays drops the copies a zoom hid, without touching their
// panes, when the zoom changes direction before the fade rests.
func (a *app) dropHiddenOverlays(t *tab) {
	for _, o := range append([]*paneOverlay(nil), a.paneOverlays...) {
		if o.t == t && o.kind == paneCopyHidden {
			a.finishPaneOverlay(o.p, paneCopyHidden)
		}
	}
}

// closePaneNow removes the pane at once, ending its terminal and closing
// the tab when it was the last pane.
func (a *app) closePaneNow(t *tab, p *pane) {
	if p.closing {
		a.finishPaneOverlay(p, paneCopyClosing)
		return
	}
	if len(t.panes()) > 1 {
		a.recordClosedPane(t, p, false)
	}
	p.cancelLoad()
	a.unzoom(t)
	a.endTerminalPane(p, true)
	releaseAsset(p)
	if !t.closePane(p) {
		a.closeTab(t)
		return
	}
	a.focusReq = t.Focus
	a.reapRemotes()
	a.changed()
	a.invalidate()
}

func (a *app) toggleZoom() {
	t := a.tab()
	if t == nil || t.Focus == nil {
		return
	}
	if t.Zoom == t.Focus {
		a.unzoom(t)
	} else {
		a.zoom(t, t.Focus)
	}
	a.focusReq = t.Focus
	a.changed()
	a.invalidate()
}

// zoom fills the tab with p. The panes it hides fade over their old
// places, and the zoomed card, note or terminal, flies to the tab's box
// through its Match; a terminal's grid settles at each width on the way.
func (a *app) zoom(t *tab, p *pane) {
	a.dropHiddenOverlays(t)
	for _, other := range t.panes() {
		if other == p {
			continue
		}
		if r := other.lastBounds; r.W > 1 && r.H > 1 {
			a.paneOverlays = append(a.paneOverlays, &paneOverlay{p: other, t: t, rect: r, kind: paneCopyHidden})
		}
	}
	t.Zoom = p
}

// unzoom returns the split and fades in the panes the zoom hid. The
// zoomed card flies back through its Match; its siblings fade in where
// they showed.
func (a *app) unzoom(t *tab) {
	zoomed := t.Zoom
	if zoomed == nil {
		// Nothing was hidden, so the cards must stay as they are.
		// Marking them would replay their fade on every focus switch,
		// which reads as the whole workspace blinking.
		return
	}
	a.dropHiddenOverlays(t)
	t.Zoom = nil
	for _, p := range t.panes() {
		if p == zoomed {
			continue
		}
		p.enter, p.enterX, p.enterY = true, 0, 0
	}
}

// clearZoom leaves the zoom without a fade, for a layout change that
// moves the panes at once.
func (a *app) clearZoom(t *tab) {
	if t == nil || t.Zoom == nil {
		return
	}
	a.dropHiddenOverlays(t)
	t.Zoom = nil
}

func (a *app) cycleTab(delta int) {
	if len(a.tabs) == 0 {
		return
	}
	a.git.Open = false
	a.active = (a.active + delta + len(a.tabs)) % len(a.tabs)
	a.revealTab = true
	a.treeDraft = nil
	if t := a.tab(); t != nil {
		if t.Focus != nil {
			a.focusReq = t.Focus
			if t.Focus.Note != nil {
				a.touchMRU(t.Focus.Note.Rel)
				a.revealInTree(t.Focus.Note)
			} else if t.Focus.Asset != nil {
				a.touchMRU(t.Focus.Asset.Rel)
				a.revealRel(t.Focus.Asset.Rel)
			}
		} else {
			a.focusReq = t.editorPane()
		}
	}
	a.changed()
	a.invalidate()
}

// selectTab shows the tab at index i.
func (a *app) selectTab(i int) {
	if i < 0 || i >= len(a.tabs) {
		return
	}
	a.git.Open = false
	a.active = i
	a.revealTab = true
	a.treeDraft = nil
	if t := a.tab(); t != nil {
		if t.Focus != nil {
			a.focusReq = t.Focus
			if t.Focus.Note != nil {
				a.touchMRU(t.Focus.Note.Rel)
				a.revealInTree(t.Focus.Note)
			} else if t.Focus.Asset != nil {
				a.touchMRU(t.Focus.Asset.Rel)
				a.revealRel(t.Focus.Asset.Rel)
			}
		} else {
			a.focusReq = t.editorPane()
		}
	}
	a.changed()
	a.invalidate()
}

// showTab shows the nth tab, from 1; 9 shows the last.
func (a *app) showTab(n int) {
	if len(a.tabs) == 0 {
		return
	}
	i := n - 1
	if n >= 9 {
		i = len(a.tabs) - 1
	}
	a.selectTab(i)
}

func (a *app) startRename(t *tab) {
	if t == nil {
		return
	}
	for i, x := range a.tabs {
		if x == t {
			a.active = i
			break
		}
	}
	a.rename = t
	a.renameText = t.Name
	a.renameFocus = false
	a.revealTab = true
	a.invalidate()
}

// updateTitle names the window after the focused note.
