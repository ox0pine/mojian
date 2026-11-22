package app

import "path/filepath"

const maxNavHistory = 100

// navEntry remembers one editor cursor location in a workspace.
type navEntry struct {
	Rel  string // slash-separated project-relative path
	Line int    // 1-based line number
	Col  int    // 0-based byte column
}

// navHistory keeps per-workspace Go Back / Go Forward jump locations.
type navHistory struct {
	back       []navEntry
	forward    []navEntry
	navigating bool
}

func (a *app) currentNavEntry() (navEntry, bool) {
	p := a.focusPane()
	if p == nil || p.Note == nil {
		return navEntry{}, false
	}
	line, col := 1, 0
	if p.state != nil {
		pos := p.state.CursorPos()
		line = pos.Line + 1
		col = pos.Col
	}
	return navEntry{
		Rel:  filepath.ToSlash(p.Note.Rel),
		Line: line,
		Col:  col,
	}, true
}

// recordNav pushes the current editor location onto the workspace back stack
// before a jump (Go to Definition, Go to Symbol, Search, Terminal click, etc.).
func (a *app) recordNav() {
	if a.workspace == nil || a.nav.navigating {
		return
	}
	cur, ok := a.currentNavEntry()
	if !ok {
		return
	}
	if n := len(a.nav.back); n > 0 {
		last := a.nav.back[n-1]
		if last.Rel == cur.Rel && absInt(last.Line-cur.Line) <= 3 {
			a.nav.back[n-1] = cur
			a.nav.forward = nil
			return
		}
	}
	a.nav.back = append(a.nav.back, cur)
	if len(a.nav.back) > maxNavHistory {
		a.nav.back = a.nav.back[len(a.nav.back)-maxNavHistory:]
	}
	a.nav.forward = nil
}

// navBack jumps to the previous location in the workspace navigation history.
func (a *app) navBack() {
	if a.workspace == nil || len(a.nav.back) == 0 {
		return
	}
	cur, hasCur := a.currentNavEntry()
	target := a.nav.back[len(a.nav.back)-1]
	a.nav.back = a.nav.back[:len(a.nav.back)-1]
	if hasCur && (cur.Rel != target.Rel || cur.Line != target.Line) {
		a.nav.forward = append(a.nav.forward, cur)
		if len(a.nav.forward) > maxNavHistory {
			a.nav.forward = a.nav.forward[len(a.nav.forward)-maxNavHistory:]
		}
	}
	a.jumpNavEntry(target)
}

// navForward jumps to the next location in the workspace navigation history.
func (a *app) navForward() {
	if a.workspace == nil || len(a.nav.forward) == 0 {
		return
	}
	cur, hasCur := a.currentNavEntry()
	target := a.nav.forward[len(a.nav.forward)-1]
	a.nav.forward = a.nav.forward[:len(a.nav.forward)-1]
	if hasCur && (cur.Rel != target.Rel || cur.Line != target.Line) {
		a.nav.back = append(a.nav.back, cur)
		if len(a.nav.back) > maxNavHistory {
			a.nav.back = a.nav.back[len(a.nav.back)-maxNavHistory:]
		}
	}
	a.jumpNavEntry(target)
}

func (a *app) jumpNavEntry(e navEntry) {
	a.nav.navigating = true
	defer func() { a.nav.navigating = false }()
	a.openNoteAt(filepath.FromSlash(e.Rel), e.Line, e.Col, e.Col)
}

func absInt(v int) int {
	if v < 0 {
		return -v
	}
	return v
}
