package app

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	"mojian/internal/icons"
	"mojian/internal/lsp"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
)

// refItem is one location in the Find References sheet.
type refItem struct {
	Rel     string
	Line    int // 1-based
	Col     int // 0-based byte offset
	Preview string
}

// problemItem is one diagnostic in the Workspace Problems sheet.
type problemItem struct {
	Rel      string
	Line     int // 1-based
	Col      int // 0-based byte offset
	Severity editor.Severity
	Message  string
	Source   string
}

// wsSymbolItem is one result in the Workspace Symbols sheet.
type wsSymbolItem struct {
	Name   string
	Detail string
	Kind   int
	Rel    string
	Line   int // 1-based
	Col    int // 0-based byte offset
}

// openReferences queries LSP textDocument/references for the symbol under cursor
// and opens a CommandDialog listing all locations.
func (a *app) openReferences() {
	p := a.focusPane()
	if p == nil || p.Note == nil || p.state == nil || a.workspace == nil {
		return
	}
	ws := a.workspace
	n := p.Note
	cl := a.lspClientForNote(ws, n)
	if cl == nil {
		a.fail(fmt.Errorf("no language server active for %s", n.Title))
		return
	}
	a.closeModals()
	a.refsOpen = true
	a.refsTitle = "References"
	a.refsQuery = ""
	a.refsSel = 0
	a.refsList = ui.ListState{}
	a.refsItems = nil
	a.refsErr = ""
	a.refsBusy = true
	a.refsSeq++
	seq := a.refsSeq

	rel := filepath.ToSlash(n.Rel)
	text := n.Text()
	pos := lsp.OffsetToPosition(text, p.state.Cursor())
	uri := a.lspURIForRel(ws, rel)
	post := a.uiPoster()
	if post == nil {
		a.refsBusy = false
		return
	}
	winSeq := a.windowSeq
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
		locs, err := cl.References(ctx, uri, pos)
		cancel()
		post(func() {
			if a.windowSeq != winSeq || a.refsSeq != seq || !a.refsOpen || a.workspace != ws {
				return
			}
			a.refsBusy = false
			if err != nil {
				a.refsErr = err.Error()
				a.invalidate()
				return
			}
			items := make([]refItem, 0, len(locs))
			for _, loc := range locs {
				targetRel, ok := a.lspRelFromURI(ws, loc.URI)
				if !ok {
					continue
				}
				preview := ""
				if tn := ws.store.ByRelPath(filepath.FromSlash(targetRel)); tn != nil {
					if tn.Loaded {
						preview = lineSnippet(tn.Text(), loc.Range.Start.Line)
					}
				}
				items = append(items, refItem{
					Rel:     targetRel,
					Line:    loc.Range.Start.Line + 1,
					Col:     loc.Range.Start.Character,
					Preview: preview,
				})
			}
			a.refsItems = items
			a.refsSel = 0
			a.invalidate()
		})
	}()
	a.invalidate()
}

func (a *app) refsModal(c *ui.Context, k *colors) {
	if !a.refsOpen {
		return
	}
	q := strings.ToLower(strings.TrimSpace(a.refsQuery))
	var filtered []refItem
	for _, it := range a.refsItems {
		if q == "" || fuzzy(strings.ToLower(it.Rel+" "+it.Preview), q) {
			filtered = append(filtered, it)
		}
	}
	a.refsSel = min(max(a.refsSel, 0), max(len(filtered)-1, 0))
	was := a.refsOpen
	reveal := false
	chosen := -1
	a.commandDialog(c, k, &a.refsOpen, func() {
		title := a.refsTitle
		if title == "" {
			title = "References"
		}
		in := sheetHeader(c, k, &a.refsQuery, "Filter "+strings.ToLower(title)+"…", title, &a.refsOpen)
		if listNav(in, &a.refsSel, len(filtered), &reveal) && len(filtered) > 0 {
			chosen = a.refsSel
		}
		a.refsList.Selected = &a.refsSel
		a.refsList.Key = func(i int) any {
			it := filtered[i]
			return it.Rel + ":" + strconv.Itoa(it.Line) + ":" + strconv.Itoa(i)
		}
		list := ui.List(c, &a.refsList, len(filtered), func(i int) {
			it := filtered[i]
			selected := i == a.refsSel
			row := ui.Row(c.Key("ref:"+it.Rel+":"+strconv.Itoa(it.Line)+":"+strconv.Itoa(i))).
				Height(rowH).Padding(0, rowPadX).Gap(rowGap).
				Radius(radius).AlignItems(ui.Center).Cursor(ui.CursorPointer)
			fg, muted := k.Text, k.TextFaint
			if selected {
				fg = c.Theme().AccentText
				muted = fg.Alpha(0.9)
			} else if row.Hovered() {
				row.Background(k.Hover)
			}
			if row.Clicked() {
				chosen = i
			}
			row.Children(func() {
				ui.Text(c, it.Rel+":"+strconv.Itoa(it.Line)).Font(noteFont.Family).
					FontSize(rowDetail).FontWeight(500).TextColor(fg).SingleLine().Shrink(0)
				if it.Preview != "" {
					ui.Text(c, it.Preview).Font(noteFont.Family).FontSize(rowDetail).
						TextColor(muted).SingleLine().Ellipsis("…").Shrink(1).MinWidth(0)
				}
			})
		}).MaxHeight(sheetListHeight(c)).Padding(6)
		if len(filtered) == 0 {
			msg := "No references found"
			if a.refsBusy {
				msg = "Finding references…"
			} else if a.refsErr != "" {
				msg = a.refsErr
			}
			list.Children(func() { emptyLine(c, k, msg) })
		}
		if reveal {
			a.refsList.ScrollIntoView(a.refsSel)
		}
	})
	if chosen >= 0 && chosen < len(filtered) {
		it := filtered[chosen]
		a.refsOpen = false
		a.later(func() {
			a.recordNav()
			a.openNoteAt(filepath.FromSlash(it.Rel), it.Line, it.Col, it.Col)
		})
	}
	if was && !a.refsOpen {
		a.restoreModalFocus()
	}
}

func (a *app) gotoImplementation() {
	a.openLocationQuery("Implementations", func(ctx context.Context, cl *lsp.Client, uri string, pos lsp.Position) ([]lsp.Location, error) {
		return cl.Implementation(ctx, uri, pos)
	})
}

func (a *app) gotoTypeDefinition() {
	a.openLocationQuery("Type Definitions", func(ctx context.Context, cl *lsp.Client, uri string, pos lsp.Position) ([]lsp.Location, error) {
		return cl.TypeDefinition(ctx, uri, pos)
	})
}

func (a *app) openLocationQuery(title string, query func(context.Context, *lsp.Client, string, lsp.Position) ([]lsp.Location, error)) {
	p := a.focusPane()
	if p == nil || p.Note == nil || p.state == nil || a.workspace == nil {
		return
	}
	ws := a.workspace
	n := p.Note
	cl := a.lspClientForNote(ws, n)
	if cl == nil {
		a.fail(fmt.Errorf("no language server active for %s", n.Title))
		return
	}
	post := a.uiPoster()
	if post == nil {
		return
	}
	rel := filepath.ToSlash(n.Rel)
	text := n.Text()
	pos := lsp.OffsetToPosition(text, p.state.Cursor())
	uri := a.lspURIForRel(ws, rel)
	a.refsSeq++
	seq := a.refsSeq
	winSeq := a.windowSeq
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
		locs, err := query(ctx, cl, uri, pos)
		cancel()
		post(func() {
			if a.windowSeq != winSeq || a.refsSeq != seq || a.workspace != ws {
				return
			}
			if err != nil {
				a.fail(err)
				return
			}
			var items []refItem
			for _, loc := range locs {
				targetRel, ok := a.lspRelFromURI(ws, loc.URI)
				if !ok {
					continue
				}
				preview := ""
				if tn := ws.store.ByRelPath(filepath.FromSlash(targetRel)); tn != nil && tn.Loaded {
					preview = lineSnippet(tn.Text(), loc.Range.Start.Line)
				}
				items = append(items, refItem{
					Rel:     targetRel,
					Line:    loc.Range.Start.Line + 1,
					Col:     loc.Range.Start.Character,
					Preview: preview,
				})
			}
			if len(items) == 0 {
				a.fail(fmt.Errorf("no %s found", strings.ToLower(title)))
				return
			}
			if len(items) == 1 {
				a.recordNav()
				a.openNoteAt(filepath.FromSlash(items[0].Rel), items[0].Line, items[0].Col, items[0].Col)
				return
			}
			a.closeModals()
			a.refsOpen = true
			a.refsTitle = title
			a.refsQuery = ""
			a.refsSel = 0
			a.refsList = ui.ListState{}
			a.refsItems = items
			a.refsErr = ""
			a.refsBusy = false
			a.invalidate()
		})
	}()
}

// openWorkspaceSymbols opens the project-wide symbol search sheet.
// Uses active LSP workspace/symbol when available, plus instant fallback symbols
// across loaded project files.
func (a *app) openWorkspaceSymbols() {
	if a.store == nil {
		return
	}
	a.closeModals()
	a.wsSymbolsOpen = true
	a.wsSymbolsQuery = ""
	a.wsSymbolsLastQ = "\x00"
	a.wsSymbolsSel = 0
	a.wsSymbolsList = ui.ListState{}
	a.wsSymbolsItems = nil
	a.queryWorkspaceSymbols("")
	a.invalidate()
}

func (a *app) queryWorkspaceSymbols(query string) {
	a.wsSymbolsLastQ = query
	a.wsSymbolsSeq++
	seq := a.wsSymbolsSeq
	ws := a.workspace
	if ws == nil || ws.store == nil {
		a.wsSymbolsItems = nil
		return
	}

	// Build fast fallback items from loaded notes immediately so the picker feels instant.
	qLow := strings.ToLower(strings.TrimSpace(query))
	var fallback []wsSymbolItem
	for _, n := range ws.store.Sorted() {
		if !n.Loaded {
			continue
		}
		rel := filepath.ToSlash(n.Rel)
		for _, s := range lsp.FallbackSymbols(rel, n.Text()) {
			if qLow != "" && !fuzzy(strings.ToLower(s.Name+" "+s.Detail+" "+rel), qLow) {
				continue
			}
			fallback = append(fallback, wsSymbolItem{
				Name:   s.Name,
				Detail: s.Detail,
				Kind:   s.Kind,
				Rel:    rel,
				Line:   s.Line + 1,
				Col:    s.Col,
			})
			if len(fallback) >= 200 {
				break
			}
		}
		if len(fallback) >= 200 {
			break
		}
	}
	a.wsSymbolsItems = fallback
	a.wsSymbolsSel = 0

	// Query every ready LSP server in the workspace in the background.
	var clients []*lsp.Client
	if ws.lsp != nil {
		for _, st := range ws.lsp.servers {
			if st != nil && st.client != nil && st.client.Ready() {
				clients = append(clients, st.client)
			}
		}
	}
	if len(clients) == 0 {
		return
	}
	post := a.uiPoster()
	if post == nil {
		return
	}
	winSeq := a.windowSeq
	a.wsSymbolsBusy = true
	go func() {
		var collected []lsp.WorkspaceSymbol
		for _, cl := range clients {
			ctx, cancel := context.WithTimeout(context.Background(), 2500*time.Millisecond)
			syms, err := cl.WorkspaceSymbols(ctx, query)
			cancel()
			if err == nil && len(syms) > 0 {
				collected = append(collected, syms...)
			}
		}
		post(func() {
			if a.windowSeq != winSeq || a.wsSymbolsSeq != seq || !a.wsSymbolsOpen || a.workspace != ws {
				return
			}
			a.wsSymbolsBusy = false
			if len(collected) == 0 {
				a.invalidate()
				return
			}
			out := make([]wsSymbolItem, 0, len(collected))
			for _, s := range collected {
				rel, ok := a.lspRelFromURI(ws, s.URI)
				if !ok {
					continue
				}
				out = append(out, wsSymbolItem{
					Name:   s.Name,
					Detail: s.Detail,
					Kind:   s.Kind,
					Rel:    rel,
					Line:   s.Line + 1,
					Col:    s.Col,
				})
				if len(out) >= 200 {
					break
				}
			}
			if len(out) > 0 {
				a.wsSymbolsItems = out
				a.wsSymbolsSel = min(max(a.wsSymbolsSel, 0), max(len(out)-1, 0))
			}
			a.invalidate()
		})
	}()
}

func (a *app) workspaceSymbolsModal(c *ui.Context, k *colors) {
	if !a.wsSymbolsOpen {
		return
	}
	if a.wsSymbolsQuery != a.wsSymbolsLastQ {
		a.queryWorkspaceSymbols(a.wsSymbolsQuery)
	}
	items := a.wsSymbolsItems
	a.wsSymbolsSel = min(max(a.wsSymbolsSel, 0), max(len(items)-1, 0))
	was := a.wsSymbolsOpen
	reveal := false
	chosen := -1
	a.commandDialog(c, k, &a.wsSymbolsOpen, func() {
		in := sheetHeader(c, k, &a.wsSymbolsQuery, "Search workspace symbols…", "Workspace symbols", &a.wsSymbolsOpen)
		if listNav(in, &a.wsSymbolsSel, len(items), &reveal) && len(items) > 0 {
			chosen = a.wsSymbolsSel
		}
		a.wsSymbolsList.Selected = &a.wsSymbolsSel
		a.wsSymbolsList.Key = func(i int) any {
			it := items[i]
			return it.Rel + ":" + it.Name + ":" + strconv.Itoa(it.Line) + ":" + strconv.Itoa(i)
		}
		list := ui.List(c, &a.wsSymbolsList, len(items), func(i int) {
			it := items[i]
			selected := i == a.wsSymbolsSel
			row := ui.Row(c.Key("wssym:"+it.Rel+":"+it.Name+":"+strconv.Itoa(i))).
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
				ui.Text(c, it.Name).Font(noteFont.Family).FontSize(rowTitle).FontWeight(500).TextColor(fg).
					SingleLine().Ellipsis("…").Shrink(0.3).MinWidth(0)
				ui.Text(c, it.Rel+":"+strconv.Itoa(it.Line)).Font(noteFont.Family).FontSize(rowDetail).TextColor(muted).
					SingleLine().Ellipsis("…").Shrink(1).MinWidth(0)
				ui.Spacer(c)
				ui.Text(c, lsp.SymbolKindName(it.Kind)).FontSize(c.Theme().Rem(10.5 / 13)).TextColor(muted).Shrink(0)
			})
		}).MaxHeight(sheetListHeight(c)).Padding(6)
		if len(items) == 0 {
			msg := "No workspace symbols found"
			if a.wsSymbolsBusy {
				msg = "Searching symbols…"
			}
			list.Children(func() { emptyLine(c, k, msg) })
		}
		if reveal {
			a.wsSymbolsList.ScrollIntoView(a.wsSymbolsSel)
		}
	})
	if chosen >= 0 && chosen < len(items) {
		it := items[chosen]
		a.wsSymbolsOpen = false
		a.later(func() {
			a.recordNav()
			a.openNoteAt(filepath.FromSlash(it.Rel), it.Line, it.Col, it.Col)
		})
	}
	if was && !a.wsSymbolsOpen {
		a.restoreModalFocus()
	}
}

// collectWorkspaceProblems returns all cached LSP diagnostics sorted by severity then file & line.
func (a *app) collectWorkspaceProblems() []problemItem {
	if a.workspace == nil || a.workspace.lsp == nil || a.store == nil {
		return nil
	}
	var out []problemItem
	for rel, doc := range a.workspace.lsp.diags {
		if len(doc.diags) == 0 {
			continue
		}
		text := ""
		if n := a.store.ByRelPath(filepath.FromSlash(rel)); n != nil && n.Loaded {
			text = n.Text()
		}
		for _, d := range doc.diags {
			line, col := offsetToLineCol(text, d.Start)
			out = append(out, problemItem{
				Rel:      rel,
				Line:     line + 1,
				Col:      col,
				Severity: d.Severity,
				Message:  strings.Join(strings.Fields(d.Message), " "),
				Source:   d.Source,
			})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Severity != out[j].Severity {
			return out[i].Severity < out[j].Severity
		}
		if out[i].Rel != out[j].Rel {
			return out[i].Rel < out[j].Rel
		}
		if out[i].Line != out[j].Line {
			return out[i].Line < out[j].Line
		}
		return out[i].Col < out[j].Col
	})
	return out
}

func (a *app) openProblems() {
	a.closeModals()
	a.problemsOpen = true
	a.problemsQuery = ""
	a.problemsSel = 0
	a.problemsList = ui.ListState{}
	a.invalidate()
}

func (a *app) problemsModal(c *ui.Context, k *colors) {
	if !a.problemsOpen {
		return
	}
	all := a.collectWorkspaceProblems()
	q := strings.ToLower(strings.TrimSpace(a.problemsQuery))
	var filtered []problemItem
	for _, it := range all {
		if q == "" || fuzzy(strings.ToLower(it.Rel+" "+it.Message+" "+it.Source), q) {
			filtered = append(filtered, it)
		}
	}
	a.problemsSel = min(max(a.problemsSel, 0), max(len(filtered)-1, 0))
	was := a.problemsOpen
	reveal := false
	chosen := -1
	a.commandDialog(c, k, &a.problemsOpen, func() {
		in := sheetHeader(c, k, &a.problemsQuery, "Filter problems…", "Problems", &a.problemsOpen)
		if listNav(in, &a.problemsSel, len(filtered), &reveal) && len(filtered) > 0 {
			chosen = a.problemsSel
		}
		a.problemsList.Selected = &a.problemsSel
		a.problemsList.Key = func(i int) any {
			it := filtered[i]
			return it.Rel + ":" + strconv.Itoa(it.Line) + ":" + strconv.Itoa(i)
		}
		list := ui.List(c, &a.problemsList, len(filtered), func(i int) {
			it := filtered[i]
			selected := i == a.problemsSel
			row := ui.Row(c.Key("prob:"+it.Rel+":"+strconv.Itoa(it.Line)+":"+strconv.Itoa(i))).
				Height(rowH).Padding(0, rowPadX).Gap(rowGap).
				Radius(radius).AlignItems(ui.Center).Cursor(ui.CursorPointer)
			fg, muted := k.Text, k.TextFaint
			badge := "INFO"
			badgeColor := k.TextMuted
			switch it.Severity {
			case editor.SeverityError:
				badge = "ERR"
				badgeColor = k.Danger
			case editor.SeverityWarning:
				badge = "WARN"
				badgeColor = k.Attention
			}
			if selected {
				fg = c.Theme().AccentText
				muted = fg.Alpha(0.9)
				badgeColor = fg
			} else if row.Hovered() {
				row.Background(k.Hover)
			}
			if row.Clicked() {
				chosen = i
			}
			row.Children(func() {
				ui.Text(c, badge).Font(noteFont.Family).FontSize(c.Theme().Rem(10.0 / 13)).
					FontWeight(650).TextColor(badgeColor).Shrink(0)
				ui.Text(c, it.Message).FontSize(rowTitle).TextColor(fg).
					SingleLine().Ellipsis("…").Shrink(1).MinWidth(0)
				ui.Text(c, it.Rel+":"+strconv.Itoa(it.Line)).Font(noteFont.Family).
					FontSize(rowDetail).TextColor(muted).SingleLine().Shrink(0)
			})
		}).MaxHeight(sheetListHeight(c)).Padding(6)
		if len(filtered) == 0 {
			list.Children(func() { emptyLine(c, k, "No problems in workspace") })
		}
		if reveal {
			a.problemsList.ScrollIntoView(a.problemsSel)
		}
	})
	if chosen >= 0 && chosen < len(filtered) {
		it := filtered[chosen]
		a.problemsOpen = false
		a.later(func() {
			a.recordNav()
			a.openNoteAt(filepath.FromSlash(it.Rel), it.Line, it.Col, it.Col)
		})
	}
	if was && !a.problemsOpen {
		a.restoreModalFocus()
	}
}

// jumpDiagnostic steps to the next (dir > 0) or previous (dir < 0) diagnostic in
// the current file, or across the workspace if the current file has none.
func (a *app) jumpDiagnostic(dir int) {
	if a.workspace == nil || a.workspace.lsp == nil {
		return
	}
	p := a.focusPane()
	if p != nil && p.Note != nil && p.state != nil {
		rel := filepath.ToSlash(p.Note.Rel)
		if doc, ok := a.workspace.lsp.diags[rel]; ok && len(doc.diags) > 0 {
			ds := append([]editor.Diagnostic(nil), doc.diags...)
			sort.Slice(ds, func(i, j int) bool { return ds[i].Start < ds[j].Start })
			cur := p.state.Cursor()
			var target editor.Diagnostic
			if dir >= 0 {
				target = ds[0]
				for _, d := range ds {
					if d.Start > cur {
						target = d
						break
					}
				}
			} else {
				target = ds[len(ds)-1]
				for i := len(ds) - 1; i >= 0; i-- {
					if ds[i].Start < cur {
						target = ds[i]
						break
					}
				}
			}
			line, col := offsetToLineCol(p.Note.Text(), target.Start)
			a.recordNav()
			p.state.GoTo(editor.Pos{Line: line, Col: col})
			a.focusReq = p
			a.invalidate()
			return
		}
	}
	all := a.collectWorkspaceProblems()
	if len(all) == 0 {
		return
	}
	it := all[0]
	if dir < 0 {
		it = all[len(all)-1]
	}
	a.recordNav()
	a.openNoteAt(filepath.FromSlash(it.Rel), it.Line, it.Col, it.Col)
}

// openRenameSymbol opens the LSP symbol rename prompt pre-filled with the word at cursor.
func (a *app) openRenameSymbol() {
	p := a.focusPane()
	if p == nil || p.Note == nil || p.state == nil || a.workspace == nil {
		return
	}
	if a.lspClientForNote(a.workspace, p.Note) == nil {
		a.fail(fmt.Errorf("no language server active for %s", p.Note.Title))
		return
	}
	a.closeModals()
	a.symRenameOpen = true
	a.symRenameText = wordAtOffset(p.Note.Text(), p.state.Cursor())
	a.symRenameErr = ""
	a.symRenameBusy = false
	a.invalidate()
}

func (a *app) renameSymbolModal(c *ui.Context, k *colors) {
	if !a.symRenameOpen {
		return
	}
	was := a.symRenameOpen
	submit := false
	a.commandDialog(c, k, &a.symRenameOpen, func() {
		in := sheetHeader(c, k, &a.symRenameText, "New symbol name…", "Rename symbol", &a.symRenameOpen)
		if in.Submitted() {
			submit = true
		}
		ui.Row(c).Padding(8, 12).Gap(8).AlignItems(ui.Center).Children(func() {
			msg := "Press Enter to rename across project (edits stay in memory until saved)"
			col := k.TextFaint
			if a.symRenameBusy {
				msg = "Renaming…"
			} else if a.symRenameErr != "" {
				msg = a.symRenameErr
				col = k.Danger
			}
			ui.Text(c, msg).FontSize(rowDetail).TextColor(col).Shrink(1).MinWidth(0)
			ui.Spacer(c)
			btn := ui.ButtonBase(c.Key("sym-rename-go")).Height(24).Padding(0, 10).Radius(radius - 2).
				Cursor(ui.CursorPointer).Label("Rename").
				Disabled(a.symRenameBusy || strings.TrimSpace(a.symRenameText) == "")
			if btn.Hovered() {
				btn.Background(k.Pressed)
			} else {
				btn.Background(k.Hover)
			}
			btn.Children(func() {
				ui.Text(c, "Rename").FontSize(c.Theme().Rem(11.5 / 13)).FontWeight(550).TextColor(k.Text)
			})
			if btn.Clicked() {
				submit = true
			}
		})
	})
	if submit && !a.symRenameBusy {
		name := strings.TrimSpace(a.symRenameText)
		if name != "" {
			a.later(func() { a.applyLSPRename(name) })
		}
	}
	if was && !a.symRenameOpen {
		a.restoreModalFocus()
	}
}

func (a *app) applyLSPRename(newName string) {
	p := a.focusPane()
	if p == nil || p.Note == nil || p.state == nil || a.workspace == nil {
		a.symRenameOpen = false
		return
	}
	ws := a.workspace
	n := p.Note
	cl := a.lspClientForNote(ws, n)
	if cl == nil {
		a.symRenameErr = "Language server unavailable"
		a.invalidate()
		return
	}
	post := a.uiPoster()
	if post == nil {
		return
	}
	rel := filepath.ToSlash(n.Rel)
	uri := a.lspURIForRel(ws, rel)
	pos := lsp.OffsetToPosition(n.Text(), p.state.Cursor())
	a.symRenameBusy = true
	a.symRenameErr = ""
	a.symRenameSeq++
	seq := a.symRenameSeq
	winSeq := a.windowSeq
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		changes, err := cl.Rename(ctx, uri, pos, newName)
		cancel()
		post(func() {
			if a.windowSeq != winSeq || a.symRenameSeq != seq || a.workspace != ws {
				return
			}
			a.symRenameBusy = false
			if err != nil {
				a.symRenameErr = err.Error()
				a.invalidate()
				return
			}
			if len(changes) == 0 {
				a.symRenameOpen = false
				a.invalidate()
				return
			}
			a.applyLSPWorkspaceEdit(ws, changes)
			a.symRenameOpen = false
			a.restoreModalFocus()
			a.invalidate()
		})
	}()
	a.invalidate()
}

func (a *app) applyLSPWorkspaceEdit(ws *workspace, changes map[string][]lsp.TextEdit) {
	if ws == nil || ws.store == nil || len(changes) == 0 {
		return
	}
	for targetURI, edits := range changes {
		targetRel, ok := a.lspRelFromURI(ws, targetURI)
		if !ok {
			continue
		}
		tn := ws.store.ByRelPath(filepath.FromSlash(targetRel))
		if tn == nil || !a.loadNote(tn) {
			continue
		}
		applyLSPTextEditsToNote(tn, edits)
	}
}

// openCodeActions queries LSP textDocument/codeAction for the focused caret or
// selection and opens a CommandDialog listing available quick fixes and refactors.
func (a *app) openCodeActions() {
	p := a.focusPane()
	if p == nil || p.Note == nil || p.state == nil || a.workspace == nil {
		return
	}
	ws := a.workspace
	n := p.Note
	a.closeModals()
	a.codeActionsOpen = true
	a.codeActionsQuery = ""
	a.codeActionsSel = 0
	a.codeActionsList = ui.ListState{}
	a.codeActionsItems = nil
	a.codeActionsErr = ""
	a.codeActionsNote = n
	a.codeActionsWS = ws
	a.codeActionsSeq++
	seq := a.codeActionsSeq

	cl := a.lspClientForNote(ws, n)
	if cl == nil {
		a.codeActionsBusy = false
		a.codeActionsErr = "No language server active for " + n.Title
		a.invalidate()
		return
	}
	post := a.uiPoster()
	if post == nil {
		return
	}
	rel := filepath.ToSlash(n.Rel)
	uri := a.lspURIForRel(ws, rel)
	text := n.Text()
	sel := p.state.Selection()
	startOff, endOff := sel.Anchor, sel.Head
	if startOff > endOff {
		startOff, endOff = endOff, startOff
	}
	rng := lsp.Range{
		Start: lsp.OffsetToPosition(text, startOff),
		End:   lsp.OffsetToPosition(text, endOff),
	}
	var diags []lsp.Diagnostic
	if ws.lsp != nil {
		if d, ok := ws.lsp.diags[rel]; ok {
			for _, rd := range d.raw {
				if rd.Range.End.Line >= rng.Start.Line && rd.Range.Start.Line <= rng.End.Line {
					diags = append(diags, rd)
				}
			}
		}
	}
	a.codeActionsBusy = true
	winSeq := a.windowSeq
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
		actions, err := cl.CodeActions(ctx, uri, rng, diags)
		cancel()
		post(func() {
			if a.windowSeq != winSeq || a.codeActionsSeq != seq || !a.codeActionsOpen {
				return
			}
			a.codeActionsBusy = false
			if err != nil {
				a.codeActionsErr = err.Error()
			} else {
				a.codeActionsItems = actions
				a.codeActionsSel = 0
			}
			a.invalidate()
		})
	}()
	a.invalidate()
}

func (a *app) applyCodeAction(act lsp.CodeAction) {
	ws := a.codeActionsWS
	n := a.codeActionsNote
	a.codeActionsOpen = false
	a.restoreModalFocus()
	if ws == nil || ws != a.workspace || n == nil {
		a.invalidate()
		return
	}
	if act.Edit != nil {
		a.applyLSPWorkspaceEdit(ws, lsp.NormalizeWorkspaceEdit(*act.Edit))
	}
	if act.Command != nil && act.Command.Command != "" {
		if cl := a.lspClientForNote(ws, n); cl != nil {
			cmd := act.Command.Command
			args := act.Command.Arguments
			go func() {
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				_ = cl.ExecuteCommand(ctx, cmd, args)
				cancel()
			}()
		}
	}
	a.invalidate()
}

func (a *app) codeActionsModal(c *ui.Context, k *colors) {
	if !a.codeActionsOpen {
		return
	}
	q := strings.ToLower(strings.TrimSpace(a.codeActionsQuery))
	var items []lsp.CodeAction
	for _, it := range a.codeActionsItems {
		if q == "" || fuzzy(strings.ToLower(it.Title+" "+it.Kind), q) {
			items = append(items, it)
		}
	}
	a.codeActionsSel = min(max(a.codeActionsSel, 0), max(len(items)-1, 0))
	was := a.codeActionsOpen
	reveal := false
	chosen := -1
	a.commandDialog(c, k, &a.codeActionsOpen, func() {
		in := sheetHeader(c, k, &a.codeActionsQuery, "Filter code actions…",
			"Code actions", &a.codeActionsOpen)
		if listNav(in, &a.codeActionsSel, len(items), &reveal) {
			chosen = a.codeActionsSel
		}
		if a.codeActionsBusy {
			ui.Row(c).Padding(12, rowPadX).Gap(8).AlignItems(ui.Center).Children(func() {
				ui.Spinner(c).Size(12, 12).Label("Loading code actions")
				ui.Text(c, "Fetching code actions…").FontSize(rowDetail).TextColor(k.TextFaint)
			})
			return
		}
		if a.codeActionsErr != "" {
			emptyLine(c, k, a.codeActionsErr)
			return
		}
		a.codeActionsList.Selected = &a.codeActionsSel
		a.codeActionsList.Key = func(i int) any { return fmt.Sprintf("ca:%d:%s", i, items[i].Title) }
		list := ui.List(c, &a.codeActionsList, len(items), func(i int) {
			it := items[i]
			row := ui.Row(c.Key(fmt.Sprintf("ca:%d:%s", i, it.Title))).Height(rowH).
				Padding(0, rowPadX).Gap(rowGap).Radius(radius).AlignItems(ui.Center).Cursor(ui.CursorPointer)
			fg, muted := k.Text, k.TextFaint
			if i == a.codeActionsSel {
				fg = c.Theme().AccentText
				muted = fg.Alpha(0.9)
			} else if row.Hovered() {
				row.Background(k.Hover)
			}
			if row.Clicked() {
				chosen = i
			}
			row.Children(func() {
				ui.Icon(c, icons.Icon("sliders-horizontal")).Size(15, 15).TextColor(muted)
				ui.Text(c, it.Title).FontSize(rowTitle).FontWeight(500).TextColor(fg).
					SingleLine().Ellipsis("…").Grow(1).MinWidth(0)
				if it.Kind != "" {
					ui.Text(c, it.Kind).FontSize(c.Theme().Rem(11.0 / 13)).TextColor(muted).Shrink(0)
				}
			})
		}).MaxHeight(sheetListHeight(c)).Padding(6)
		if len(items) == 0 {
			list.Children(func() { emptyLine(c, k, "No code actions available here") })
		}
		if reveal {
			a.codeActionsList.ScrollIntoView(a.codeActionsSel)
		}
	})
	if chosen >= 0 && chosen < len(items) {
		act := items[chosen]
		a.later(func() { a.applyCodeAction(act) })
	}
	if was && !a.codeActionsOpen {
		a.restoreModalFocus()
	}
}

// formatDirtyNotesBeforeSave formats dirty notes that have a ready LSP client
// before an explicit save, bounded by a short timeout so saving never stalls.
func (a *app) formatDirtyNotesBeforeSave(done func()) {
	ws := a.workspace
	if ws == nil || ws.store == nil || ws.lsp == nil {
		done()
		return
	}
	type fmtJob struct {
		n            *note
		cl           *lsp.Client
		uri          string
		ver          uint64
		tabSize      int
		insertSpaces bool
	}
	var jobs []fmtJob
	for _, n := range ws.store.Notes {
		if !n.Dirty() {
			continue
		}
		lang := lsp.LanguageForPath(n.Rel)
		if lang == "" {
			continue
		}
		st := ws.lsp.servers[lsp.ServerGroup(lang)]
		if st == nil || st.client == nil || !st.client.Ready() {
			continue
		}
		var paneMatch *pane
		if t := a.tab(); t != nil {
			for _, p := range t.panes() {
				if p.Note == n {
					paneMatch = p
					break
				}
			}
		}
		_, tabSize := a.effectiveIndent(paneMatch, n)
		jobs = append(jobs, fmtJob{
			n:            n,
			cl:           st.client,
			uri:          a.lspURIForRel(ws, filepath.ToSlash(n.Rel)),
			ver:          n.Buffer().Version(),
			tabSize:      tabSize,
			insertSpaces: a.insertSpacesFor(paneMatch, n),
		})
	}
	if len(jobs) == 0 {
		done()
		return
	}
	post := a.uiPoster()
	if post == nil {
		// Synchronous test path.
		for _, j := range jobs {
			ctx, cancel := context.WithTimeout(context.Background(), 1500*time.Millisecond)
			edits, err := j.cl.Formatting(ctx, j.uri, j.tabSize, j.insertSpaces)
			cancel()
			if err == nil && len(edits) > 0 && j.n.Buffer().Version() == j.ver {
				applyLSPTextEditsToNote(j.n, edits)
			}
		}
		done()
		return
	}
	winSeq := a.windowSeq
	go func() {
		type fmtResult struct {
			n     *note
			ver   uint64
			edits []lsp.TextEdit
		}
		results := make([]fmtResult, 0, len(jobs))
		for _, j := range jobs {
			ctx, cancel := context.WithTimeout(context.Background(), 1500*time.Millisecond)
			edits, err := j.cl.Formatting(ctx, j.uri, j.tabSize, j.insertSpaces)
			cancel()
			if err == nil && len(edits) > 0 {
				results = append(results, fmtResult{n: j.n, ver: j.ver, edits: edits})
			}
		}
		post(func() {
			if a.windowSeq != winSeq || a.workspace != ws {
				return
			}
			for _, r := range results {
				if r.n.Buffer().Version() == r.ver {
					applyLSPTextEditsToNote(r.n, r.edits)
				}
			}
			done()
		})
	}()
}

// formatDocument runs LSP textDocument/formatting on the focused file and applies
// edits in memory via buf.Edit so ⌘Z undo and explicit ⌘S save semantics hold.
func (a *app) formatDocument() {
	p := a.focusPane()
	if p == nil || p.Note == nil || a.workspace == nil {
		return
	}
	ws := a.workspace
	n := p.Note
	cl := a.lspClientForNote(ws, n)
	if cl == nil {
		a.fail(fmt.Errorf("no language server active for %s", n.Title))
		return
	}
	post := a.uiPoster()
	if post == nil {
		return
	}
	rel := filepath.ToSlash(n.Rel)
	uri := a.lspURIForRel(ws, rel)
	_, tabSize := a.effectiveIndent(p, n)
	insertSpaces := a.insertSpacesFor(p, n)
	ver := n.Buffer().Version()
	winSeq := a.windowSeq
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
		edits, err := cl.Formatting(ctx, uri, tabSize, insertSpaces)
		cancel()
		if err != nil || len(edits) == 0 {
			return
		}
		post(func() {
			if a.windowSeq != winSeq || a.workspace != ws || n.Buffer().Version() != ver {
				return
			}
			applyLSPTextEditsToNote(n, edits)
			a.invalidate()
		})
	}()
}

// restartLSP closes and clears all language servers in the active workspace so
// the next frame starts them fresh (clearing any prior failed state).
func (a *app) restartLSP() {
	if a.workspace == nil {
		return
	}
	a.closeWorkspaceLSP(a.workspace)
	a.invalidate()
}

func applyLSPTextEditsToNote(n *note, edits []lsp.TextEdit) {
	if n == nil || len(edits) == 0 {
		return
	}
	cur := n.Text()
	updated := lsp.ApplyTextEdits(cur, edits)
	if updated == cur {
		return
	}
	buf := n.Buffer()
	buf.Edit(editor.Edit{Start: 0, End: len(cur), Text: updated})
}

func (a *app) restoreModalFocus() {
	if t := a.tab(); t != nil && a.focusReq == nil {
		if t.Focus != nil {
			a.focusReq = t.Focus
		} else {
			a.focusReq = t.editorPane()
		}
	}
}

func lineSnippet(text string, line int) string {
	if line < 0 {
		return ""
	}
	cur := 0
	off := 0
	for cur < line && off < len(text) {
		nl := strings.IndexByte(text[off:], '\n')
		if nl < 0 {
			return ""
		}
		off += nl + 1
		cur++
	}
	end := len(text)
	if nl := strings.IndexByte(text[off:], '\n'); nl >= 0 {
		end = off + nl
	}
	return strings.TrimSpace(text[off:end])
}

func offsetToLineCol(text string, off int) (line, col int) {
	if off < 0 {
		off = 0
	}
	if off > len(text) {
		off = len(text)
	}
	lineStart := 0
	for i := 0; i < off; i++ {
		if text[i] == '\n' {
			line++
			lineStart = i + 1
		}
	}
	return line, off - lineStart
}

func wordAtOffset(text string, off int) string {
	if len(text) == 0 {
		return ""
	}
	if off > len(text) {
		off = len(text)
	}
	if off < 0 {
		off = 0
	}
	isIdent := func(b byte) bool {
		return b == '_' || unicode.IsLetter(rune(b)) || unicode.IsDigit(rune(b))
	}
	start := off
	for start > 0 && isIdent(text[start-1]) {
		start--
	}
	end := off
	for end < len(text) && isIdent(text[end]) {
		end++
	}
	return text[start:end]
}
