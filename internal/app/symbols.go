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
	commandDialog(c, k, &a.symbolsOpen, func() {
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
		p.Preview = false
		if p.state == nil {
			p.state = p.Note.NewState()
		}
		p.state.GoTo(editor.Pos{Line: sym.Line, Col: sym.Col})
		a.focusReq = p
		a.invalidate()
	}
}
