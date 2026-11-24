package app

import (
	"fmt"
	"runtime"
	"sort"
	"strings"

	"github.com/egoist/mygo/ui"

	"mojian/internal/app/commands"
	"mojian/internal/icons"
)

var (
	cmdOpenProject         = commands.ByID("open-project")
	cmdNewNote             = commands.ByID("new-file")
	cmdNewFolder           = commands.ByID("new-folder")
	cmdRenameNode          = commands.ByID("rename")
	cmdTrashNode           = commands.ByID("trash")
	cmdNewTab              = commands.ByID("new-tab")
	cmdReopenClosed        = commands.ByID("reopen-closed")
	cmdSave                = commands.ByID("save")
	cmdFocusLeft           = commands.ByID("focus-left")
	cmdFocusRight          = commands.ByID("focus-right")
	cmdFocusUp             = commands.ByID("focus-up")
	cmdFocusDown           = commands.ByID("focus-down")
	cmdMoveLeft            = commands.ByID("move-pane-left")
	cmdMoveRight           = commands.ByID("move-pane-right")
	cmdMoveUp              = commands.ByID("move-pane-up")
	cmdMoveDown            = commands.ByID("move-pane-down")
	cmdSplitLeft           = commands.ByID("split-left")
	cmdSplitRight          = commands.ByID("split-right")
	cmdSplitUp             = commands.ByID("split-up")
	cmdSplitDown           = commands.ByID("split-down")
	cmdZoom                = commands.ByID("zoom")
	cmdClosePane           = commands.ByID("close-pane")
	cmdCloseTab            = commands.ByID("close-tab")
	cmdRenameTab           = commands.ByID("rename-tab")
	cmdNextTab             = commands.ByID("next-tab")
	cmdPrevTab             = commands.ByID("prev-tab")
	cmdPalette             = commands.ByID("palette")
	cmdSearch              = commands.ByID("find-in-files")
	cmdSearchInPane        = commands.ByID("search-in-pane")
	cmdSymbols             = commands.ByID("goto-symbol")
	cmdWorkspaceSymbols    = commands.ByID("workspace-symbols")
	cmdFindReferences      = commands.ByID("find-references")
	cmdGotoImplementation  = commands.ByID("goto-implementation")
	cmdGotoTypeDefinition  = commands.ByID("goto-type-definition")
	cmdShowProblems        = commands.ByID("show-problems")
	cmdNextDiagnostic      = commands.ByID("next-diagnostic")
	cmdPrevDiagnostic      = commands.ByID("prev-diagnostic")
	cmdRenameSymbol        = commands.ByID("rename-symbol")
	cmdCodeActions         = commands.ByID("code-actions")
	cmdSignatureHelp       = commands.ByID("signature-help")
	cmdToggleInlayHints    = commands.ByID("toggle-inlay-hints")
	cmdFormatDocument      = commands.ByID("format-document")
	cmdFold                = commands.ByID("fold")
	cmdUnfold              = commands.ByID("unfold")
	cmdToggleFold          = commands.ByID("toggle-fold")
	cmdFoldAll             = commands.ByID("fold-all")
	cmdUnfoldAll           = commands.ByID("unfold-all")
	cmdFoldImports         = commands.ByID("fold-imports")
	cmdSelectNextMatch     = commands.ByID("select-next-match")
	cmdSelectAllMatches    = commands.ByID("select-all-matches")
	cmdAddCursorAbove      = commands.ByID("add-cursor-above")
	cmdAddCursorBelow      = commands.ByID("add-cursor-below")
	cmdSplitSelectionLines = commands.ByID("split-selection-lines")
	cmdExpandSelection     = commands.ByID("expand-selection")
	cmdShrinkSelection     = commands.ByID("shrink-selection")
	cmdMarkdownPreviewSide = commands.ByID("markdown-preview-side")
	cmdLSPStatus           = commands.ByID("lsp-status")
	cmdRestartLSP          = commands.ByID("restart-lsp")
	cmdNavBack             = commands.ByID("nav-back")
	cmdNavForward          = commands.ByID("nav-forward")
	cmdNextHunk            = commands.ByID("next-hunk")
	cmdPrevHunk            = commands.ByID("prev-hunk")
	cmdRevertHunk          = commands.ByID("revert-hunk")
	cmdNotes               = commands.ByID("open-file")
	cmdTree                = commands.ByID("toggle-tree")
	cmdTerminal            = commands.ByID("toggle-terminal")
	cmdSendToTerminal      = commands.ByID("send-to-terminal")
	cmdFindInTerminal      = commands.ByID("find-in-terminal")
	cmdAttachSession       = commands.ByID("attach-terminal")
	cmdJumpAttention       = commands.ByID("jump-attention")
	cmdGit                 = commands.ByID("git")
	cmdBranch              = commands.ByID("switch-branch")
	cmdSettings            = commands.ByID("settings")
	cmdSSH                 = commands.ByID("ssh")
)

// paletteCommands are the commands the palette lists.
var paletteCommands = commands.Palette

// focusPane returns the focused pane of the active tab.
func (a *app) focusPane() *pane {
	if t := a.tab(); t != nil {
		return t.Focus
	}
	return nil
}

func (a *app) openPalette() {
	a.closeModals()
	a.paletteOpen = true
	a.paletteQuery = ""
	a.paletteSel = 0
	a.paletteList = ui.ListState{}
	a.invalidate()
}

// paletteItem is a row of the command palette: a command, a file, or a tab.
type paletteItem struct {
	id, title, detail, keys, glyph string
	score                          int
	titlePos, detailPos            []int
	note                           *note
	asset                          *assetFile
	run                            func()
}

func (a *app) paletteItems() []paletteItem {
	var items []paletteItem
	q := strings.TrimSpace(a.paletteQuery)
	for _, c := range paletteCommands {
		if !commandAvailable(c) {
			continue
		}
		score, tPos, ok := fuzzyScore(c.Title, q)
		if !ok {
			continue
		}
		if q != "" {
			score += 5
		}
		c := c
		items = append(items, paletteItem{
			id: "cmd:" + c.ID, title: c.Title,
			keys: a.commandKeys(c), glyph: c.Glyph,
			score: score, titlePos: tPos,
			run: func() { a.run(c) },
		})
	}
	if a.store != nil {
		if q == "" {
			for _, ch := range a.mruFileChoices() {
				ch := ch
				if ch.note != nil {
					n := ch.note
					items = append(items, paletteItem{
						id: "note:" + n.Rel, title: n.Title, detail: n.Rel, glyph: "file-text",
						note: n,
						run: func() {
							a.openNote(n)
							a.updateTitle()
						},
					})
				} else if ch.asset != nil {
					f := ch.asset
					items = append(items, paletteItem{
						id: "asset:" + f.Rel, title: f.Name, detail: f.Rel, glyph: "file-text",
						asset: f,
						run: func() {
							a.openAsset(f)
							a.updateTitle()
						},
					})
				}
			}
		} else {
			for _, n := range a.store.Sorted() {
				score, tPos, rPos, ok := scoreFileMatch(n.Title, n.Rel, q)
				if !ok {
					continue
				}
				if rank := a.mruRank(n.Rel); rank >= 0 {
					score += max(40-rank*2, 4)
				}
				n := n
				items = append(items, paletteItem{
					id: "note:" + n.Rel, title: n.Title, detail: n.Rel, glyph: "file-text",
					score: score, titlePos: tPos, detailPos: rPos, note: n,
					run: func() {
						a.openNote(n)
						a.updateTitle()
					},
				})
			}
			var assets []*assetFile
			for _, f := range a.store.Assets {
				assets = append(assets, f)
			}
			sort.Slice(assets, func(i, j int) bool { return assets[i].Rel < assets[j].Rel })
			for _, f := range assets {
				score, tPos, rPos, ok := scoreFileMatch(f.Name, f.Rel, q)
				if !ok {
					continue
				}
				if rank := a.mruRank(f.Rel); rank >= 0 {
					score += max(40-rank*2, 4)
				}
				f := f
				items = append(items, paletteItem{
					id: "asset:" + f.Rel, title: f.Name, detail: f.Rel, glyph: "file-text",
					score: score, titlePos: tPos, detailPos: rPos, asset: f,
					run: func() {
						a.openAsset(f)
						a.updateTitle()
					},
				})
			}
		}
	}
	for i, t := range a.tabs {
		name, detail := t.label()
		score, tPos, tOK := fuzzyScore(name, q)
		dScore, dPos, dOK := fuzzyScore(detail, q)
		if q != "" && !tOK && !dOK {
			if cScore, _, cOK := fuzzyScore(name+" "+detail, q); cOK {
				score = cScore - 10
			} else {
				continue
			}
		} else if dOK && (!tOK || dScore > score) {
			score = dScore
		}
		if !tOK {
			tPos = nil
		}
		if !dOK {
			dPos = nil
		}
		i := i
		keys := ""
		if i < 9 {
			keys = shortcutDisplay(showTabAccel(i + 1))
		}
		tabDetail := "Tab"
		if detail != "" {
			tabDetail += " " + detail
		}
		items = append(items, paletteItem{
			id: "tab:" + t.ID, title: name, detail: tabDetail, glyph: "layout-grid", keys: keys,
			score: score, titlePos: tPos, detailPos: dPos,
			run: func() { a.selectTab(i); a.updateTitle() },
		})
	}
	if q != "" {
		sort.SliceStable(items, func(i, j int) bool {
			return items[i].score > items[j].score
		})
	}
	return items
}

// palette draws the command palette over the window.
func (a *app) palette(c *ui.Context, k *colors) {
	if !a.paletteOpen {
		return
	}
	items := a.paletteItems()
	a.paletteSel = min(max(a.paletteSel, 0), max(len(items)-1, 0))
	was := a.paletteOpen
	reveal := false
	chosen := -1
	splitChosen := -1
	a.commandDialog(c, k, &a.paletteOpen, func() {
		in := sheetHeader(c, k, &a.paletteQuery, "Type a command, file, or tab name…",
			"Search commands, files, and tabs", &a.paletteOpen)
		submit, split := listNavSplit(in, &a.paletteSel, len(items), &reveal)
		if split && len(items) > 0 {
			splitChosen = a.paletteSel
		} else if submit && len(items) > 0 {
			chosen = a.paletteSel
		}
		a.paletteList.Selected = &a.paletteSel
		a.paletteList.Key = func(i int) any { return items[i].id }
		list := ui.List(c, &a.paletteList, len(items), func(i int) {
			it := items[i]
			row := ui.Row(c.Key(it.id)).Height(rowH).Padding(0, rowPadX).Gap(rowGap).
				Radius(radius).AlignItems(ui.Center).Cursor(ui.CursorPointer)
			fg, muted, icon := k.Text, k.TextFaint, k.TextMuted
			hiColor, hiWeight, underline := c.Theme().Accent, 700, false
			if i == a.paletteSel {
				fg = c.Theme().AccentText
				muted, icon = fg.Alpha(0.9), fg.Alpha(0.9)
				hiColor, hiWeight, underline = fg, 750, true
			} else if row.Hovered() {
				row.Background(k.Hover)
			}
			if row.Clicked() {
				chosen = i
			}
			row.Children(func() {
				ui.Icon(c, icons.Icon(it.glyph)).Size(15, 15).TextColor(icon)
				highlightText(c, it.title, it.titlePos, fg, hiColor, 500, hiWeight, rowTitle, underline).
					SingleLine().Ellipsis("…").Shrink(0.3).MinWidth(0)
				if it.detail != "" {
					highlightText(c, it.detail, it.detailPos, muted, hiColor, 400, hiWeight, rowDetail, underline).
						SingleLine().Ellipsis("…").Shrink(1).MinWidth(0)
				}
				ui.Spacer(c)
				if it.keys != "" && it.keys != "None" {
					chipFg, chipBg := k.TextFaint, k.Hover
					if i == a.paletteSel {
						chipFg, chipBg = fg, fg.Alpha(0.16)
					}
					ui.Text(c, it.keys).FontSize(c.Theme().Rem(12.0/13)).TextColor(chipFg).
						Padding(2, 7).Radius(radius).Background(chipBg).Shrink(0)
				}
			})
		}).MaxHeight(sheetListHeight(c)).Padding(6)
		if len(items) == 0 {
			list.Children(func() { emptyLine(c, k, "No matches") })
		}
		if reveal {
			a.paletteList.ScrollIntoView(a.paletteSel)
		}
	})
	if splitChosen >= 0 && splitChosen < len(items) {
		it := items[splitChosen]
		a.paletteOpen = false
		if it.note != nil || it.asset != nil {
			a.later(func() { a.openFileInSplit(it.note, it.asset, 0, 0) })
		} else {
			it.run()
		}
	} else if chosen >= 0 && chosen < len(items) {
		a.paletteOpen = false
		items[chosen].run()
	}
	if was && !a.paletteOpen {
		if t := a.tab(); t != nil && a.focusReq == nil {
			if t.Focus != nil {
				a.focusReq = t.Focus
			} else {
				a.focusReq = t.editorPane()
			}
		}
	}
}

func (a *app) filteredFiles() ([]*note, []*assetFile) {
	if a.store == nil {
		return nil, nil
	}
	q := strings.ToLower(strings.TrimSpace(a.treeQuery))
	match := func(s string) bool { return q == "" || fuzzy(strings.ToLower(s), q) }
	var notes []*note
	for _, n := range a.store.Sorted() {
		if match(n.Title + " " + n.Rel) {
			notes = append(notes, n)
		}
	}
	var assets []*assetFile
	for _, f := range a.store.Assets {
		if match(f.Name + " " + f.Rel) {
			assets = append(assets, f)
		}
	}
	sort.Slice(assets, func(i, j int) bool { return assets[i].Rel < assets[j].Rel })
	return notes, assets
}

// shortcuts gives the browser the keys the menus give a native window,
// where it has no menu bar. The harness has no menu either, so it binds
// the same commands in the view.
func (a *app) shortcuts(c *ui.Context) {
	if (runtime.GOOS != "js" && !a.harness) ||
		a.paletteOpen || a.quickOpen || a.symbolsOpen || a.wsSymbolsOpen || a.refsOpen || a.problemsOpen || a.symRenameOpen || a.codeActionsOpen || a.lspStatusOpen || a.searchOpen || a.sessionPick || a.sshPick || a.projectPicker || a.remotePicker {
		return
	}
	// Fixed tab switching takes precedence when a custom command has the
	// same keys: MyGo dispatches the first matching scoped shortcut.
	for i := 1; i <= 9; i++ {
		i := i
		mods, key, ok := parseAccel(showTabAccel(i))
		if !ok {
			continue
		}
		c.OnShortcut(mods, key, func() { a.showTab(i) })
	}
	for _, cmd := range paletteCommands {
		if !commandAvailable(cmd) {
			continue
		}
		accel := a.commandAccel(cmd)
		if accel == "" {
			continue
		}
		mods, key, ok := parseAccel(accel)
		if !ok {
			continue
		}
		cmd := cmd
		c.OnShortcut(mods, key, func() { a.run(cmd) })
	}
}

// openQuickOpen shows the file picker over the window: every file of the
// project is listed before typing, the field filters them, and Enter or a
// click opens one in the active tab. The welcome page and ⌘P both lead
// here, so the action is visible even when the Explorer is already open.
func (a *app) openQuickOpen() {
	a.closeModals()
	a.quickOpen = true
	a.quickQuery = ""
	a.quickSel = 0
	a.quickList = ui.ListState{}
	a.invalidate()
}

// quickOpenChoice opens the file chosen in the picker, optionally jumping to line/col.
func (a *app) quickOpenChoice(ch fileChoice, line, col int) {
	switch {
	case ch.note != nil:
		if line > 0 {
			a.openNoteAt(ch.note.Rel, line, col, col)
		} else {
			a.openNote(ch.note)
		}
	case ch.asset != nil:
		a.openAsset(ch.asset)
	}
}

// quickOpenModal draws the file picker over the window.
func (a *app) quickOpenModal(c *ui.Context, k *colors) {
	if !a.quickOpen {
		return
	}
	trimmed := strings.TrimSpace(a.quickQuery)
	if strings.HasPrefix(trimmed, ">") {
		rest := strings.TrimSpace(strings.TrimPrefix(trimmed, ">"))
		a.quickOpen = false
		a.openPalette()
		a.paletteQuery = rest
		return
	}
	if strings.HasPrefix(trimmed, "@") {
		rest := strings.TrimSpace(strings.TrimPrefix(trimmed, "@"))
		a.quickOpen = false
		a.openSymbols()
		a.symbolsQuery = rest
		return
	}
	if strings.HasPrefix(trimmed, "#") {
		rest := strings.TrimSpace(strings.TrimPrefix(trimmed, "#"))
		a.quickOpen = false
		a.openWorkspaceSymbols()
		if rest != "" {
			a.wsSymbolsQuery = rest
			a.queryWorkspaceSymbols(rest)
		}
		return
	}

	fileQuery, line, col, hasLine := parseFileLineQuery(a.quickQuery)
	curNote := a.currentNote()
	lineOnly := fileQuery == "" && hasLine && curNote != nil
	var choices []fileChoice
	nRows := 0
	if lineOnly {
		nRows = 1
	} else {
		choices = a.paneChoices(fileQuery)
		nRows = len(choices)
	}
	a.quickSel = min(max(a.quickSel, 0), max(nRows-1, 0))
	was := a.quickOpen
	reveal := false
	chosen := -1
	splitChosen := -1
	badge := formatLineColBadge(line, col, hasLine)
	a.commandDialog(c, k, &a.quickOpen, func() {
		in := sheetHeader(c, k, &a.quickQuery, "Open a file… (file:line, > command, @ symbol)", "Open a file", &a.quickOpen)
		submit, split := listNavSplit(in, &a.quickSel, nRows, &reveal)
		if split && nRows > 0 {
			splitChosen = a.quickSel
		} else if submit && nRows > 0 {
			chosen = a.quickSel
		}
		a.quickList.Selected = &a.quickSel
		a.quickList.Key = func(i int) any {
			if lineOnly {
				return "goto-line"
			}
			return choices[i].rel
		}
		list := ui.List(c, &a.quickList, nRows, func(i int) {
			if lineOnly {
				row := ui.Row(c.Key("goto-line")).Height(rowH).Padding(0, rowPadX).Gap(rowGap).
					Radius(radius).AlignItems(ui.Center).Cursor(ui.CursorPointer)
				fg, muted := k.Text, k.TextFaint
				if i == a.quickSel {
					fg = c.Theme().AccentText
					muted = fg.Alpha(0.9)
				} else if row.Hovered() {
					row.Background(k.Hover)
				}
				if row.Clicked() {
					chosen = 0
				}
				row.Children(func() {
					ui.Icon(c, icons.Icon("arrow-right")).Size(15, 15).TextColor(muted)
					label := fmt.Sprintf("Go to line %d", line)
					if col > 0 {
						label = fmt.Sprintf("Go to line %d, column %d", line, col)
					}
					ui.Text(c, label).FontSize(rowTitle).FontWeight(500).TextColor(fg)
					ui.Text(c, "in "+curNote.Title).FontSize(rowDetail).TextColor(muted).
						SingleLine().Ellipsis("…").Shrink(1).MinWidth(0)
				})
				return
			}
			ch := choices[i]
			_, clicked := fileChoiceRowMatch(c, k, ch.rel, ch, badge, i == a.quickSel)
			if clicked {
				chosen = i
			}
		}).MaxHeight(sheetListHeight(c)).Padding(6)
		if nRows == 0 {
			list.Children(func() { emptyLine(c, k, "No files match") })
		}
		if reveal {
			a.quickList.ScrollIntoView(a.quickSel)
		}
	})
	if lineOnly && (chosen == 0 || splitChosen == 0) {
		a.quickOpen = false
		rel := curNote.Rel
		colIdx := max(col-1, 0)
		a.later(func() { a.openNoteAt(rel, line, colIdx, colIdx) })
	} else if !lineOnly && splitChosen >= 0 && splitChosen < len(choices) {
		a.quickOpen = false
		ch := choices[splitChosen]
		colIdx := max(col-1, 0)
		a.later(func() { a.openFileInSplit(ch.note, ch.asset, line, colIdx) })
	} else if !lineOnly && chosen >= 0 && chosen < len(choices) {
		a.quickOpen = false
		ch := choices[chosen]
		colIdx := max(col-1, 0)
		a.later(func() { a.quickOpenChoice(ch, line, colIdx) })
	}
	if was && !a.quickOpen {
		if t := a.tab(); t != nil && a.focusReq == nil {
			if t.Focus != nil {
				a.focusReq = t.Focus
			} else {
				a.focusReq = t.editorPane()
			}
		}
	}
}
