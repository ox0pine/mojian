package app

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/egoist/mygo/ui/editor"
)

// closedEntry remembers a recently closed file pane or tab so Reopen Closed
// (⇧⌘T) can restore it cleanly.
type closedEntry struct {
	rel     string
	isAsset bool
	preview bool
	tabName string
	wasTab  bool
}

const maxClosedStack = 24

func (a *app) recordClosedPane(t *tab, p *pane, wasTab bool) {
	if a.workspace == nil || p == nil {
		return
	}
	var entry closedEntry
	switch {
	case p.Note != nil:
		entry = closedEntry{rel: p.Note.Rel, preview: p.Preview, wasTab: wasTab}
	case p.Asset != nil:
		entry = closedEntry{rel: p.Asset.Rel, isAsset: true, wasTab: wasTab}
	default:
		return
	}
	if t != nil && wasTab {
		entry.tabName = t.Name
	}
	a.closedStack = append(a.closedStack, entry)
	if len(a.closedStack) > maxClosedStack {
		a.closedStack = a.closedStack[len(a.closedStack)-maxClosedStack:]
	}
}

// reopenClosed restores the most recently closed file pane or tab that still
// exists in the active project.
func (a *app) reopenClosed() {
	if a.workspace == nil || a.store == nil {
		return
	}
	for len(a.closedStack) > 0 {
		last := a.closedStack[len(a.closedStack)-1]
		a.closedStack = a.closedStack[:len(a.closedStack)-1]
		if last.isAsset {
			f := a.store.AssetByRel(last.rel)
			if f == nil {
				continue
			}
			if last.wasTab || a.tab() == nil {
				a.openAssetInTab(f)
				if t := a.tab(); t != nil && last.tabName != "" {
					t.Name = last.tabName
				}
			} else {
				a.openAsset(f)
			}
			return
		}
		n := a.store.ByRelPath(last.rel)
		if n == nil {
			continue
		}
		if last.wasTab || a.tab() == nil {
			a.openNoteInTab(n)
			if t := a.tab(); t != nil {
				if last.tabName != "" {
					t.Name = last.tabName
				}
				if t.Focus != nil && isMarkdownPath(n.Path) {
					t.Focus.Preview = last.preview
				}
			}
		} else {
			a.openNote(n)
			if p := a.focusPane(); p != nil && p.Note == n && isMarkdownPath(n.Path) {
				p.Preview = last.preview
			}
		}
		a.changed()
		a.invalidate()
		return
	}
}

// moveTab reorders tabs when a tab is dragged onto another tab in the strip.
func (a *app) moveTab(from *tab, targetIdx int) {
	if from == nil || targetIdx < 0 || targetIdx >= len(a.tabs) {
		return
	}
	srcIdx := -1
	for i, t := range a.tabs {
		if t == from {
			srcIdx = i
			break
		}
	}
	if srcIdx < 0 || srcIdx == targetIdx {
		return
	}
	activeTab := a.tab()
	a.tabs = append(a.tabs[:srcIdx], a.tabs[srcIdx+1:]...)
	if targetIdx > len(a.tabs) {
		targetIdx = len(a.tabs)
	}
	a.tabs = append(a.tabs[:targetIdx], append([]*tab{from}, a.tabs[targetIdx:]...)...)
	for i, t := range a.tabs {
		if t == activeTab {
			a.active = i
			break
		}
	}
	a.revealTab = true
	a.changed()
	a.invalidate()
}

// handleFileDrop handles files or folders dropped onto the window from the OS
// file manager (Finder / Explorer / Files).
func (a *app) handleFileDrop(paths []string) {
	if len(paths) == 0 {
		return
	}
	// If a folder is dropped, open it as the project.
	for _, raw := range paths {
		abs, err := filepath.Abs(raw)
		if err != nil {
			continue
		}
		info, err := os.Stat(abs)
		if err != nil {
			continue
		}
		if info.IsDir() {
			a.applyStartup(Startup{Dir: abs})
			return
		}
	}
	// Otherwise open dropped regular files. Files inside the current local
	// project open directly; a file outside opens its parent folder as project.
	openedInCurrent := false
	for _, raw := range paths {
		abs, err := filepath.Abs(raw)
		if err != nil {
			continue
		}
		info, err := os.Stat(abs)
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		if a.store != nil && !a.store.IsRemote() && !a.store.IsMemory() {
			if rel, ok := a.store.RelOf(abs); ok {
				if a.store.ByRelPath(rel) == nil && a.store.AssetByRel(rel) == nil {
					_ = a.store.Refresh()
				}
				if !openedInCurrent {
					a.openRel(rel)
					openedInCurrent = true
				} else {
					if n := a.store.ByRelPath(rel); n != nil {
						a.openNoteInTab(n)
					} else if f := a.store.AssetByRel(rel); f != nil {
						a.openAssetInTab(f)
					}
				}
				continue
			}
		}
		if !openedInCurrent {
			a.applyStartup(Startup{Dir: filepath.Dir(abs), File: abs})
			return
		}
	}
}

// openDroppedExplorerFile opens a file dragged from the Explorer onto target
// pane either in place (center) or in a new split on zone's edge.
func (a *app) openDroppedExplorerFile(t *tab, target *pane, rel string, zone paneDrop) {
	if t == nil || t != a.tab() || target == nil || !t.contains(target) || a.store == nil || rel == "" {
		return
	}
	var newP *pane
	if n := a.store.ByRelPath(rel); n != nil {
		if !a.loadNote(n) {
			return
		}
		if zone == dropNone || zone == dropCenter {
			t.Focus = target
			a.openNote(n)
			return
		}
		newP = newPane(n)
	} else if f := a.store.AssetByRel(rel); f != nil {
		if !a.loadAsset(f) {
			return
		}
		if zone == dropNone || zone == dropCenter {
			t.Focus = target
			a.openAsset(f)
			return
		}
		newP = newAssetPane(f)
	} else {
		return
	}
	a.clearZoom(t)
	var side splitSide
	switch zone {
	case dropLeft:
		side = splitLeft
	case dropAbove:
		side = splitUp
	case dropBelow:
		side = splitDown
	default:
		side = splitRight
	}
	newP.enter = true
	switch side {
	case splitLeft:
		newP.enterX = -12
	case splitRight:
		newP.enterX = 12
	case splitUp:
		newP.enterY = -12
	case splitDown:
		newP.enterY = 12
	}
	t.splitSide(target, newP, side)
	t.Focus = newP
	a.focusReq = newP
	a.revealRel(rel)
	a.changed()
	a.invalidate()
}

// openMarkdownPreviewSide opens (or focuses) a live rendered Markdown preview
// pane beside the focused Markdown editor pane, sharing the same buffer.
func (a *app) openMarkdownPreviewSide() {
	t := a.tab()
	p := a.focusPane()
	if t == nil || p == nil || p.Note == nil || !isMarkdownPath(p.Note.Path) {
		return
	}
	// If a preview pane for this same note already exists in the tab, focus it.
	for _, other := range t.panes() {
		if other != p && other.Note == p.Note && other.Preview {
			a.clearZoom(t)
			t.Focus = other
			a.changed()
			a.invalidate()
			return
		}
	}
	a.clearZoom(t)
	previewPane := newPane(p.Note)
	previewPane.Preview = true
	previewPane.enter = true
	previewPane.enterX = 12
	t.splitSide(p, previewPane, splitRight)
	// Keep focus on the source editor so the user can keep typing while watching preview.
	p.Preview = false
	t.Focus = p
	a.focusReq = p
	a.changed()
	a.invalidate()
}

// syncMarkdownPreviewScroll mirrors the editor pane's vertical scroll ratio to
// any paired Markdown preview pane showing the same note in the active tab.
func (a *app) syncMarkdownPreviewScroll(t *tab, edPane *pane) {
	if t == nil || edPane == nil || edPane.Note == nil || edPane.Preview || edPane.state == nil {
		return
	}
	if !isMarkdownPath(edPane.Note.Path) {
		return
	}
	sy := edPane.state.ScrollY()
	if sy == edPane.lastScrollY {
		return
	}
	edPane.lastScrollY = sy
	totalLines := edPane.Note.Buffer().LineCount()
	if totalLines <= 1 {
		return
	}
	first, last := edPane.state.VisibleLines()
	denom := float32(max(totalLines-(last-first), 1))
	ratio := float32(first) / denom
	if ratio < 0 {
		ratio = 0
	} else if ratio > 1 {
		ratio = 1
	}
	for _, other := range t.panes() {
		if other == edPane || other.Note != edPane.Note || !other.Preview {
			continue
		}
		if other.previewScroll.MaxY > 0 {
			other.previewScroll.Y = ratio * other.previewScroll.MaxY
		}
	}
}

// toggleMarkdownTask updates a GFM task checkbox ([ ] <-> [x]) on 1-based line
// in n's shared buffer so edits remain undoable and respect explicit ⌘S save.
func toggleMarkdownTask(n *note, line int, checked bool) {
	if n == nil || line < 1 {
		return
	}
	text := n.Text()
	curLine := 1
	off := 0
	for curLine < line && off < len(text) {
		nl := strings.IndexByte(text[off:], '\n')
		if nl < 0 {
			return
		}
		off += nl + 1
		curLine++
	}
	end := len(text)
	if nl := strings.IndexByte(text[off:], '\n'); nl >= 0 {
		end = off + nl
	}
	row := text[off:end]
	idx := -1
	if checked {
		idx = strings.Index(row, "[ ]")
	} else {
		idx = strings.Index(row, "[x]")
		if idx < 0 {
			idx = strings.Index(row, "[X]")
		}
	}
	if idx < 0 {
		return
	}
	repl := "[x]"
	if !checked {
		repl = "[ ]"
	}
	n.Buffer().Edit(editor.Edit{
		Start: off + idx,
		End:   off + idx + 3,
		Text:  repl,
	})
}
