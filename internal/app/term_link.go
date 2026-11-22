package app

import (
	"errors"
	"fmt"
	"net/url"
	"path"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"mojian/internal/icons"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
)

// parseTerminalLink parses a terminal Command/Ctrl+Click link into a file path
// plus optional 1-based line and column. Returns isFile=false for web/mailto URLs
// so MyGo falls back to opening them with the system browser.
func parseTerminalLink(raw string) (filePath string, line, col int, isFile bool) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return "", 0, 0, false
	}
	lower := strings.ToLower(s)
	if strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://") ||
		strings.HasPrefix(lower, "mailto:") || strings.HasPrefix(lower, "ftp://") {
		return "", 0, 0, false
	}
	if strings.HasPrefix(lower, "file://") {
		if u, err := url.Parse(s); err == nil && u.Scheme == "file" {
			frag := u.Fragment
			p := u.Path
			if runtime.GOOS == "windows" && len(p) >= 3 && p[0] == '/' && p[2] == ':' {
				p = p[1:]
			}
			s = p
			if frag != "" {
				l, c := parseHashLineCol(frag)
				if l > 0 {
					return s, l, c, true
				}
			}
		}
	}
	// Strip trailing #Lline[Ccol]
	if idx := strings.LastIndex(s, "#L"); idx > 0 {
		if l, c := parseHashLineCol(s[idx+1:]); l > 0 {
			return s[:idx], l, c, true
		}
	}
	// Strip trailing (line,col) or (line)
	if strings.HasSuffix(s, ")") {
		if open := strings.LastIndexByte(s, '('); open > 0 {
			inner := s[open+1 : len(s)-1]
			parts := strings.Split(inner, ",")
			if len(parts) == 1 || len(parts) == 2 {
				if l, err := strconv.Atoi(strings.TrimSpace(parts[0])); err == nil && l > 0 {
					c := 0
					if len(parts) == 2 {
						c, _ = strconv.Atoi(strings.TrimSpace(parts[1]))
					}
					return s[:open], l, c, true
				}
			}
		}
	}
	// Strip trailing :line:col or :line (preserving Windows drive prefix like C:\...)
	s = strings.TrimSuffix(s, ":")
	if base, l, c, ok := splitColonLineCol(s); ok {
		return base, l, c, true
	}
	return s, 0, 0, true
}

func parseHashLineCol(frag string) (int, int) {
	if !strings.HasPrefix(frag, "L") {
		return 0, 0
	}
	rest := frag[1:]
	if cIdx := strings.IndexByte(rest, 'C'); cIdx >= 0 {
		l, err1 := strconv.Atoi(rest[:cIdx])
		c, err2 := strconv.Atoi(rest[cIdx+1:])
		if err1 == nil && l > 0 && err2 == nil {
			return l, max(c, 0)
		}
		return 0, 0
	}
	if l, err := strconv.Atoi(rest); err == nil && l > 0 {
		return l, 0
	}
	return 0, 0
}

func splitColonLineCol(s string) (string, int, int, bool) {
	last := strings.LastIndexByte(s, ':')
	if last <= 1 {
		return "", 0, 0, false
	}
	v1, err1 := strconv.Atoi(s[last+1:])
	if err1 != nil || v1 <= 0 {
		return "", 0, 0, false
	}
	prefix := s[:last]
	if prev := strings.LastIndexByte(prefix, ':'); prev > 1 {
		if v2, err2 := strconv.Atoi(prefix[prev+1:]); err2 == nil && v2 > 0 {
			return prefix[:prev], v2, v1, true
		}
	}
	return prefix, v1, 0, true
}

// resolveTerminalPath matches a clicked terminal path against the active project.
func (a *app) resolveTerminalPath(rawPath, termDir string) (rel string, isAsset bool, ok bool) {
	if a.store == nil || rawPath == "" {
		return "", false, false
	}
	tryRel := func(cand string) (string, bool, bool) {
		cand = strings.TrimPrefix(filepath.ToSlash(cand), "./")
		cand = strings.TrimPrefix(cand, "/")
		if cand == "" || strings.HasPrefix(cand, "..") {
			return "", false, false
		}
		osRel := filepath.FromSlash(cand)
		if n := a.store.ByRelPath(osRel); n != nil {
			return n.Rel, false, true
		}
		if f := a.store.AssetByRel(osRel); f != nil {
			return f.Rel, true, true
		}
		return "", false, false
	}

	// 1. Direct project-relative path.
	if r, asset, found := tryRel(rawPath); found {
		return r, asset, true
	}

	// 2. Relative to the terminal's current working directory (OSC 7), if inside project.
	if termDir != "" && !strings.HasPrefix(rawPath, "/") && !(len(rawPath) >= 2 && rawPath[1] == ':') {
		joined := path.Clean(path.Join(filepath.ToSlash(termDir), filepath.ToSlash(rawPath)))
		root := strings.TrimRight(filepath.ToSlash(a.store.Dir), "/")
		if root != "" && strings.HasPrefix(joined, root+"/") {
			if r, asset, found := tryRel(strings.TrimPrefix(joined, root+"/")); found {
				return r, asset, true
			}
		}
	}

	// 3. Absolute path inside the project root (works for both local and remote POSIX roots).
	rootSlash := strings.TrimRight(filepath.ToSlash(a.store.Dir), "/")
	rawSlash := filepath.ToSlash(rawPath)
	if rootSlash != "" && strings.HasPrefix(rawSlash, rootSlash+"/") {
		if r, asset, found := tryRel(strings.TrimPrefix(rawSlash, rootSlash+"/")); found {
			return r, asset, true
		}
	}

	// 4. Unique suffix / basename match inside the project (e.g., compiler output printing "lsp.go:42").
	cleanSuffix := strings.TrimPrefix(path.Clean(rawSlash), "./")
	if cleanSuffix != "" && cleanSuffix != "." && !strings.HasPrefix(cleanSuffix, "..") {
		var matchedRel string
		matchedAsset := false
		matches := 0
		for _, n := range a.store.Notes {
			ns := filepath.ToSlash(n.Rel)
			if ns == cleanSuffix || strings.HasSuffix(ns, "/"+cleanSuffix) {
				matchedRel = n.Rel
				matchedAsset = false
				matches++
				if matches > 1 {
					break
				}
			}
		}
		if matches <= 1 {
			for _, f := range a.store.Assets {
				fs := filepath.ToSlash(f.Rel)
				if fs == cleanSuffix || strings.HasSuffix(fs, "/"+cleanSuffix) {
					matchedRel = f.Rel
					matchedAsset = true
					matches++
					if matches > 1 {
						break
					}
				}
			}
		}
		if matches == 1 {
			return matchedRel, matchedAsset, true
		}
	}
	return "", false, false
}

// openTerminalLink opens a clicked terminal file link in an editor pane beside
// the terminal (or in an existing editor pane in the current tab) and jumps to line/col.
func (a *app) openTerminalLink(tt *termTab, raw string) bool {
	filePath, line, col, isFile := parseTerminalLink(raw)
	if !isFile {
		return false
	}
	termDir := ""
	if tt != nil && tt.Term != nil {
		termDir = tt.Term.Dir()
	}
	rel, isAsset, ok := a.resolveTerminalPath(filePath, termDir)
	if !ok {
		return false
	}
	a.recordNav()
	a.git.Open = false
	if isAsset {
		a.openRel(rel)
		return true
	}
	n := a.store.ByRelPath(rel)
	if n == nil || !a.loadNote(n) {
		return false
	}
	t := a.tab()
	// If the current tab has only terminal panes, split an editor pane beside the
	// terminal so the user keeps both the terminal output and the opened file visible.
	if t != nil && t.firstEditorPane() == nil && t.Focus != nil && t.Focus.Term != nil {
		from := t.Focus
		ed := newPane(n)
		ed.Preview = false
		ed.enterSide(splitLeft)
		if t.splitSide(from, ed, splitLeft) {
			t.Focus = ed
			a.focusReq = ed
			a.revealRel(n.Rel)
			a.changed()
		}
	} else {
		a.openNote(n)
	}
	p := a.focusPane()
	if p != nil && p.Note == n {
		p.Preview = false
		if p.state == nil {
			p.state = n.NewState()
		}
		if line > 0 {
			colIdx := max(col-1, 0)
			p.state.GoTo(editor.Pos{Line: line - 1, Col: colIdx})
		}
		a.focusReq = p
	}
	a.invalidate()
	return true
}

// formatEditorReference formats the focused editor file (and cursor/selection line range)
// as a compact @path[:line[-end]] reference for pasting into a CLI tool or shell.
func (a *app) formatEditorReference() (string, bool) {
	p := a.focusPane()
	if p != nil && p.Note != nil {
		rel := filepath.ToSlash(p.Note.Rel)
		if p.state != nil && !p.Preview {
			sel := p.state.Selection()
			if sel.Anchor != sel.Head {
				text := p.Note.Text()
				lo, hi := sel.Anchor, sel.Head
				if lo > hi {
					lo, hi = hi, lo
				}
				startLine := byteOffsetLine(text, lo) + 1
				endOff := hi
				if endOff > lo && endOff <= len(text) && text[endOff-1] == '\n' {
					endOff--
				}
				endLine := byteOffsetLine(text, endOff) + 1
				if endLine > startLine {
					return fmt.Sprintf("@%s:%d-%d ", rel, startLine, endLine), true
				}
				return fmt.Sprintf("@%s:%d ", rel, startLine), true
			}
			line := p.state.CursorPos().Line + 1
			return fmt.Sprintf("@%s:%d ", rel, line), true
		}
		return "@" + rel + " ", true
	}
	if p != nil && p.Asset != nil {
		return "@" + filepath.ToSlash(p.Asset.Rel) + " ", true
	}
	if a.treeSel != "" {
		return "@" + filepath.ToSlash(a.treeSel) + " ", true
	}
	return "", false
}

func byteOffsetLine(text string, off int) int {
	if off < 0 {
		off = 0
	}
	if off > len(text) {
		off = len(text)
	}
	return strings.Count(text[:off], "\n")
}

// sendReferenceToTerminal pastes the current file/line/selection reference
// (@path/to/file.go:12-40) into the current tab's terminal pane, opening a
// split terminal beside the editor if none is open yet.
func (a *app) sendReferenceToTerminal() {
	if !terminalsSupported() {
		a.fail(errors.New("terminals are unavailable in this build"))
		return
	}
	ref, ok := a.formatEditorReference()
	if !ok {
		return
	}
	t := a.tab()
	if t != nil {
		if tp := t.terminalPane(); tp != nil && tp.Term != nil {
			a.git.Open = false
			if t.Zoom != nil && t.Zoom != tp {
				a.unzoom(t)
			}
			t.Focus = tp
			a.focusReq = tp
			if tp.Term.Term != nil {
				tp.Term.Term.Paste(ref)
			} else {
				tp.Term.pendingPaste += ref
			}
			a.changed()
			a.invalidate()
			return
		}
	}
	// No terminal in this tab: split one to the right and queue the paste.
	a.newTerminalWithPaste(ref)
}

func (a *app) newTerminalWithPaste(paste string) {
	spec := a.localTermSpec()
	a.startTerminalInPane(spec, func(tt *termTab) *pane {
		if tt != nil {
			tt.pendingPaste += paste
			if tt.Term != nil {
				tt.Term.Paste(tt.pendingPaste)
				tt.pendingPaste = ""
			}
		}
		return a.addTerminalPane(tt)
	})
}

// openTerminalSearch opens the floating find bar on the focused terminal pane.
func (a *app) openTerminalSearch() {
	p := a.focusPane()
	if p == nil || p.Term == nil || p.Term.Term == nil {
		return
	}
	tt := p.Term
	tt.findOpen = true
	tt.findFocus = true
	if tt.findQuery != "" {
		tt.Term.Search(tt.findQuery)
	}
	a.invalidate()
}

func (a *app) closeTerminalSearch(tt *termTab, p *pane) {
	if tt == nil {
		return
	}
	tt.findOpen = false
	tt.findFocus = false
	if tt.Term != nil {
		tt.Term.EndSearch()
	}
	if p != nil {
		a.focusReq = p
	}
	a.invalidate()
}

// terminalSearchBar draws a compact floating find strip at the top-right of a terminal pane.
func (a *app) terminalSearchBar(c *ui.Context, k *colors, p *pane) {
	if p == nil || p.Term == nil || !p.Term.findOpen || p.Term.Term == nil {
		return
	}
	tt := p.Term
	st := tt.Term.SearchStatus()
	bar := ui.Row(c.Key("term-find:"+p.ID)).Absolute().Top(8).Right(10).
		Height(32).Padding(0, 6).Gap(4).AlignItems(ui.Center).
		Background(k.Panel).Border(1, k.PanelBorder).Radius(radius).
		Shadow(0, 4, 14, -2, k.ShadowFocused)
	bar.Children(func() {
		ui.Icon(c, icons.Icon("search")).Size(13, 13).TextColor(k.TextMuted).Shrink(0)
		in := ui.TextInputBase(c.Key("term-find-in:"+p.ID), &tt.findQuery).
			Placeholder("Find in terminal…").Label("Find in terminal").
			Width(180).Height(24).Padding(0, 4).
			FontSize(rowDetail)
		if tt.findFocus {
			in.Focus()
			tt.findFocus = false
		}
		if in.Changed() {
			tt.Term.Search(tt.findQuery)
		}
		if in.Submitted() {
			tt.Term.SearchNext()
		}
		c.OnShortcut(0, ui.KeyEscape, func() {
			a.later(func() { a.closeTerminalSearch(tt, p) })
		})
		c.OnShortcut(ui.Shift, ui.KeyEnter, func() {
			tt.Term.SearchPrevious()
		})
		count := ""
		if tt.findQuery != "" {
			if st.Total == 0 {
				if st.Searching {
					count = "…"
				} else {
					count = "0"
				}
			} else {
				count = fmt.Sprintf("%d/%d", st.Selected, st.Total)
			}
		}
		if count != "" {
			ui.Text(c, count).Font(noteFont.Family).FontSize(c.Theme().Rem(11.0/13)).
				TextColor(k.TextFaint).Padding(0, 4).Shrink(0)
		}
		if iconButton(c.Key("term-find-prev:"+p.ID), k, "chevron-left", "Previous match", iconBtnSm, iconGlyphSm).Clicked() {
			tt.Term.SearchPrevious()
		}
		if iconButton(c.Key("term-find-next:"+p.ID), k, "chevron-right", "Next match", iconBtnSm, iconGlyphSm).Clicked() {
			tt.Term.SearchNext()
		}
		if iconButton(c.Key("term-find-close:"+p.ID), k, "x", "Close find", iconBtnSm, iconGlyphSm).Clicked() {
			a.later(func() { a.closeTerminalSearch(tt, p) })
		}
	})
}
