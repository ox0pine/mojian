package app

import (
	"context"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"mojian/internal/icons"
	"mojian/internal/lsp"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
)

// openSymbols opens the Go to Symbol (⇧⌘O) picker for the focused file.
// Fallback regex symbols appear immediately; if an LSP server is active for
// the file, its richer documentSymbol list replaces them asynchronously.
func (a *app) openSymbols() {
	p := a.focusPane()
	if p == nil || p.Note == nil {
		return
	}
	a.closeModals()
	a.symbolsOpen = true
	a.symbolsQuery = ""
	a.symbolsSel = 0
	a.symbolsList = ui.ListState{}
	a.symbolsSeq++
	seq := a.symbolsSeq
	n := p.Note
	text := n.Text()
	a.symbolsItems = lsp.FallbackSymbols(n.Rel, text)

	ws := a.workspace
	if cl := a.lspClientForNote(ws, n); cl != nil {
		if post := a.uiPoster(); post != nil {
			uri := a.lspURIForRel(ws, filepath.ToSlash(n.Rel))
			winSeq := a.windowSeq
			go func() {
				ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
				syms, err := cl.DocumentSymbols(ctx, uri, text)
				cancel()
				if err != nil || len(syms) == 0 {
					return
				}
				post(func() {
					if a.windowSeq != winSeq || a.symbolsSeq != seq || !a.symbolsOpen {
						return
					}
					a.symbolsItems = syms
					a.symbolsSel = min(max(a.symbolsSel, 0), max(len(syms)-1, 0))
					a.invalidate()
				})
			}()
		}
	}
	a.invalidate()
}

func (a *app) symbolsModal(c *ui.Context, k *colors) {
	if !a.symbolsOpen {
		return
	}
	q := strings.ToLower(strings.TrimSpace(a.symbolsQuery))
	var filtered []lsp.Symbol
	for _, s := range a.symbolsItems {
		if q == "" || fuzzy(strings.ToLower(s.Name+" "+s.Detail+" "+lsp.SymbolKindName(s.Kind)), q) {
			filtered = append(filtered, s)
		}
	}
	a.symbolsSel = min(max(a.symbolsSel, 0), max(len(filtered)-1, 0))
	was := a.symbolsOpen
	reveal := false
	chosen := -1
	a.commandDialog(c, k, &a.symbolsOpen, func() {
		in := sheetHeader(c, k, &a.symbolsQuery, "Go to symbol…", "Go to symbol", &a.symbolsOpen)
		if listNav(in, &a.symbolsSel, len(filtered), &reveal) && len(filtered) > 0 {
			chosen = a.symbolsSel
		}
		a.symbolsList.Selected = &a.symbolsSel
		a.symbolsList.Key = func(i int) any {
			s := filtered[i]
			return s.Name + ":" + strconv.Itoa(s.Line) + ":" + strconv.Itoa(i)
		}
		list := ui.List(c, &a.symbolsList, len(filtered), func(i int) {
			s := filtered[i]
			selected := i == a.symbolsSel
			row := ui.Row(c.Key("sym:"+s.Name+":"+strconv.Itoa(s.Line)+":"+strconv.Itoa(i))).
				Height(rowH).Padding(0, rowPadX).Gap(rowGap).
				Radius(radius).AlignItems(ui.Center).Cursor(ui.CursorPointer)
			fg, muted, icon := k.Text, k.TextFaint, k.TextMuted
			if selected {
				fg = c.Theme().AccentText
				muted, icon = fg.Alpha(0.9), fg.Alpha(0.9)
			} else if row.Hovered() {
				row.Background(k.Hover)
			}
			if row.Clicked() {
				chosen = i
			}
			row.Children(func() {
				ui.Icon(c, icons.Icon("file-text")).Size(14, 14).TextColor(icon)
				ui.Text(c, s.Name).Font(noteFont.Family).FontSize(rowTitle).FontWeight(500).TextColor(fg).
					SingleLine().Ellipsis("…").Shrink(0.3).MinWidth(0)
				if s.Detail != "" {
					ui.Text(c, s.Detail).FontSize(rowDetail).TextColor(muted).
						SingleLine().Ellipsis("…").Shrink(1).MinWidth(0)
				}
				ui.Spacer(c)
				ui.Text(c, lsp.SymbolKindName(s.Kind)).FontSize(c.Theme().Rem(10.5 / 13)).TextColor(muted).Shrink(0)
				ui.Text(c, ":"+strconv.Itoa(s.Line+1)).Font(noteFont.Family).FontSize(rowDetail).TextColor(muted).Shrink(0)
			})
		}).MaxHeight(sheetListHeight(c)).Padding(6)
		if len(filtered) == 0 {
			list.Children(func() { emptyLine(c, k, "No symbols found") })
		}
		if reveal {
			a.symbolsList.ScrollIntoView(a.symbolsSel)
		}
	})
	if chosen >= 0 && chosen < len(filtered) {
		sym := filtered[chosen]
		a.symbolsOpen = false
		a.later(func() { a.jumpToSymbol(sym) })
	}
	if was && !a.symbolsOpen {
		if t := a.tab(); t != nil && a.focusReq == nil {
			if t.Focus != nil {
				a.focusReq = t.Focus
			} else {
				a.focusReq = t.editorPane()
			}
		}
	}
}

func (a *app) jumpToSymbol(sym lsp.Symbol) {
	if p := a.focusPane(); p != nil && p.Note != nil {
		a.recordNav()
		p.Preview = false
		if p.state == nil {
			p.state = p.Note.NewState()
		}
		p.state.GoTo(editor.Pos{Line: sym.Line, Col: sym.Col})
		a.focusReq = p
		a.invalidate()
	}
}

// syncPaneBreadcrumb keeps p.symCrumb updated with the symbol enclosing the
// editor's caret. It seeds instantaneously from FallbackSymbols and upgrades
// asynchronously with hierarchical LSP DocumentSymbols when a server is active.
func (a *app) syncPaneBreadcrumb(p *pane, st *editor.State, focused bool) {
	if p == nil || p.Note == nil || st == nil {
		return
	}
	ver := p.Note.Buffer().Version()
	line := st.CursorPos().Line
	if !p.symLoaded || p.symNote != p.Note || p.symVer != ver {
		p.symNote = p.Note
		p.symVer = ver
		p.symLoaded = true
		p.symLSP = false
		p.symList = lsp.FallbackSymbols(p.Note.Rel, p.Note.Text())
		p.symLine = line
		p.symCrumb = enclosingSymbolLabel(p.symList, line)
	} else if p.symLine != line {
		p.symLine = line
		p.symCrumb = enclosingSymbolLabel(p.symList, line)
	}
	if !focused || p.symLSP || a.workspace == nil {
		return
	}
	cl := a.lspClientForNote(a.workspace, p.Note)
	if cl == nil {
		return
	}
	post := a.uiPoster()
	if post == nil {
		return
	}
	p.symLSP = true
	p.symSeq++
	seq := p.symSeq
	ws := a.workspace
	n := p.Note
	text := n.Text()
	uri := a.lspURIForRel(ws, filepath.ToSlash(n.Rel))
	winSeq := a.windowSeq
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		syms, err := cl.DocumentSymbols(ctx, uri, text)
		cancel()
		if err != nil || len(syms) == 0 {
			return
		}
		post(func() {
			if a.windowSeq != winSeq || p.symSeq != seq || p.Note != n || p.state != st || n.Buffer().Version() != ver {
				return
			}
			p.symList = syms
			p.symLine = st.CursorPos().Line
			p.symCrumb = enclosingSymbolLabel(syms, p.symLine)
			a.invalidate()
		})
	}()
}

// enclosingSymbolLabel returns the display breadcrumb for the symbol enclosing
// 0-based line (e.g. "Session › Attach" or "main"), or "" if none applies.
func enclosingSymbolLabel(syms []lsp.Symbol, line int) string {
	if len(syms) == 0 || line < 0 {
		return ""
	}
	// First pass: prefer symbols with explicit EndLine ranges, picking the innermost.
	bestIdx := -1
	bestSpan := int(^uint(0) >> 1)
	for i, s := range syms {
		if s.EndLine > 0 && s.Line <= line && line <= s.EndLine {
			span := s.EndLine - s.Line
			if bestIdx < 0 || span < bestSpan || (span == bestSpan && s.Line >= syms[bestIdx].Line) {
				bestIdx = i
				bestSpan = span
			}
		}
	}
	if bestIdx >= 0 {
		s := syms[bestIdx]
		if s.Container != "" {
			return s.Container + " › " + s.Name
		}
		return s.Name
	}
	// Fallback pass (for regex symbols without EndLine): last symbol starting at or before line.
	lastIdx := -1
	for i, s := range syms {
		if s.Line <= line && (lastIdx < 0 || s.Line >= syms[lastIdx].Line) {
			lastIdx = i
		}
	}
	if lastIdx < 0 {
		return ""
	}
	s := syms[lastIdx]
	if s.Container != "" {
		return s.Container + " › " + s.Name
	}
	return s.Name
}
