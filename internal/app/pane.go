package app

import (
	"path/filepath"
	"sort"
	"strings"

	"mojian/internal/app/explorer"
	"mojian/internal/app/vim"
	"mojian/internal/icons"
	"mojian/internal/motion"

	"github.com/egoist/mygo/plugins/terminal"
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
)

// paneCard draws one file: a card with a header over its editor.
func (a *app) paneCard(c *ui.Context, k *colors, t *tab, p *pane) ui.Element {
	return a.paneCardMode(c, k, t, p, paneInLayout)
}

// paneCopy is how a pane card is drawn: where the layout puts it, or as a
// copy over the place it last showed, leaving the layout or hidden by a
// zoom. A copy takes no input and does not animate its place: only its
// fade runs.
type paneCopy int

const (
	paneInLayout paneCopy = iota
	paneCopyClosing
	paneCopyHidden
)

func (a *app) paneCardMode(c *ui.Context, k *colors, t *tab, p *pane, mode paneCopy) ui.Element {
	overlay := mode != paneInLayout
	focused := t.Focus == p
	card := ui.Column(c.Key(p.ID)).Radius(radius).Clip().MinWidth(0).MinHeight(0)
	if overlay {
		card.Fill()
	} else {
		if b := card.Bounds(); b.W > 0 && b.H > 0 {
			p.lastBounds = b
		}
		// A pane moved into another split comes from where it showed, and
		// one a zoom shows comes from its split; its colors follow focus.
		// A terminal flies too: its grid settles at each width on the way.
		tr := motion.Pane
		tr.Match = p.ID
		card.Transition(tr)
	}
	if mode == paneInLayout && p.enter {
		// The split is new with the pane, or a zoom revealed it, so no
		// Enter can run: start transparent, at the edge if it is new, and
		// animate both here.
		card.Jump("enter-alpha", 0)
		card.Jump("enter-x", p.enterX)
		card.Jump("enter-y", p.enterY)
		p.enter = false
	}
	ex := card.AnimateTo("enter-x", 0, motion.PaneSlide)
	ey := card.AnimateTo("enter-y", 0, motion.PaneSlide)
	switch mode {
	case paneCopyClosing:
		if !p.closeStarted {
			card.Jump("close-alpha", 1)
			card.Jump("close-x", 0)
			card.Jump("close-y", 0)
			p.closeStarted = true
		}
		ex += card.AnimateTo("close-x", p.enterX, motion.Close)
		ey += card.AnimateTo("close-y", p.enterY, motion.Close)
		o := card.AnimateTo("close-alpha", 0, motion.Close)
		if o < 1 {
			s := 0.985 + 0.015*o
			card.Scale(s, s).Opacity(o)
		}
		if card.Settled("close-alpha") && card.Settled("close-x") && card.Settled("close-y") {
			a.later(func() { a.finishPaneOverlay(p, paneCopyClosing) })
		}
	case paneCopyHidden:
		if !p.hideStarted {
			card.Jump("hide-alpha", 1)
			p.hideStarted = true
		}
		o := card.AnimateTo("hide-alpha", 0, motion.Close)
		if o < 1 {
			card.Opacity(o)
		}
		if card.Settled("hide-alpha") {
			a.later(func() { a.finishPaneOverlay(p, paneCopyHidden) })
		}
	}
	if ex != 0 || ey != 0 {
		card.Translate(ex, ey)
	}
	if mode == paneInLayout {
		if o := card.AnimateTo("enter-alpha", 1, motion.FadeIn); o < 1 {
			card.Opacity(o)
		}
	}
	bg, border, shadow := k.Card, k.CardBorder, k.Shadow
	borderW := float32(1)
	if focused {
		bg, border, shadow = k.CardFocused, k.CardBorderFocused, k.ShadowFocused
	}
	card.Background(bg).Border(borderW, border).
		Shadow(0, 1, 2, 0, shadow).
		Shadow(0, 6, 22, -2, shadow)
	width, height := card.Bounds().W, card.Bounds().H
	overZone := dropNone
	overTarget := false
	overExplorerFile := false
	if !overlay {
		if value, ok := ui.DragOver[any](card); ok {
			switch value := value.(type) {
			case *pane:
				if value != nil && value != p && t.contains(value) {
					overTarget = true
					if x, y, inside := card.PointerPosition(); inside && width > 0 && height > 0 {
						overZone = dropZoneAt(x, y, width, height)
					}
				}
			case explorer.DragItem:
				if value.Rel != "" && !value.Dir {
					overTarget = true
					overExplorerFile = true
					if x, y, inside := card.PointerPosition(); inside && width > 0 && height > 0 {
						overZone = dropZoneAt(x, y, width, height)
					}
				}
			}
		}
	}
	card.Children(func() {
		if p.Note != nil && !p.Preview {
			if p.state == nil {
				p.state = p.Note.NewState()
			}
			a.syncPaneBreadcrumb(p, p.state, focused && !overlay)
		}
		a.paneHeader(c, k, t, p, width, overlay)
		body := ui.Box(c).Grow(1).MinHeight(0).Padding(0, 0, 6, 0)
		body.Children(func() {
			if p.Note == nil && p.Asset == nil && p.Term == nil {
				a.emptyPaneBody(c, k, t, p)
				return
			}
			if p.Term != nil {
				if p.Term.Term == nil {
					a.startRestoredTerminal(p)
					if p.Term.startErr != "" {
						emptyState(c, k, "square-terminal", k.Danger, "Could not open the terminal", p.Term.startErr, func() {
							if ui.Button(c, "Try Again").Clicked() {
								p.Term.startErr = ""
								a.startRestoredTerminal(p)
							}
						})
					} else {
						ui.Column(c).Fill().Center().Padding(20).Children(func() {
							ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
								if p.Term.connecting {
									ui.Spinner(c).Size(12, 12).Label("Attaching terminal")
								}
								ui.Text(c, "Attaching…").FontSize(rowDetail).TextColor(k.TextFaint)
							})
						})
					}
					return
				}
				tv := terminal.View(c, p.Term.Term).Fill()
				if !overlay && a.focusReq == p {
					tv.Focus()
					a.focusReq = nil
				}
				if !overlay && tv.Focused() {
					clearPaneAttention(p)
					if t.Focus != p {
						t.Focus = p
						a.changed()
					}
				}
				if !overlay && p.Term.findOpen {
					a.terminalSearchBar(c, k, p)
				}
				return
			}
			if p.Asset != nil {
				a.assetBody(c, k, p)
				return
			}
			if p.Note == nil {
				return
			}
			if p.Preview && isMarkdownPath(p.Note.Path) {
				a.markdownBody(c, k, p, overlay)
				return
			}
			st := p.state
			if st == nil {
				st = p.Note.NewState()
				p.state = st
			}
			st.Theme = a.editorTheme(c)
			st.Font = a.editorFont()
			st.LineHeight = float32(a.lineHeight())
			st.Indent, st.TabSize = a.effectiveIndent(p, p.Note)
			st.SoftWrap = a.EditorSoftWrap || wrapFor(p.Note.Path)
			st.NoActiveLine = a.NoActiveLine
			st.NoIndentGuides = a.NoIndentGuides
			st.NoAutoClose = a.NoAutoClose
			st.NoScrollPastEnd = a.NoScrollPastEnd
			st.ScrollMargin = a.EditorScrollMargin
			st.DiffBase = a.noteDiffBase(p.Note)
			st.Label = p.Note.Title
			if a.HideLineNumbers {
				st.LineNumbers = editor.LineNumbersOff
			} else {
				st.LineNumbers = lineNumbersFor(p.Note.Path)
			}
			a.syncEditorLSP(p, st)
			a.syncEditorInlays(p, st, focused && !overlay)
			a.syncEditorBlame(p, st, focused && !overlay)
			a.syncEditorHighlights(p, st, focused && !overlay, k)
			a.syncEditorSignatureHelp(p, st, focused && !overlay)
			st.ContextMenu = func(m *ui.Menu) {
				a.editorContextMenu(m, p)
			}
			if a.EditorVimMode {
				if p.vim == nil {
					p.vim = &vim.State{}
				}
				p.vim.Configure(st, a.HideLineNumbers)
				st.OnInput = func(ev ui.InputEvent) bool {
					return p.vim.Handle(st, ev, vim.Actions{
						Save: func() {
							a.later(func() {
								a.saveNowAsync(func(err error) {
									if err != nil {
										a.handleSaveError(err)
									}
								})
							})
						},
						Close: func() {
							a.later(func() { a.closePane(p) })
						},
						SplitDown: func() {
							a.later(func() { a.splitFocusedSide(splitDown) })
						},
						SplitRight: func() {
							a.later(func() { a.splitFocusedSide(splitRight) })
						},
						NewTab: func() {
							a.later(func() { a.newTab() })
						},
						CloseTab: func() {
							a.later(func() { a.closeTab(a.tab()) })
						},
						OpenFile: func(name string) {
							a.later(func() { a.vimOpenFile(name) })
						},
						ReadClipboard:  c.ReadClipboard,
						WriteClipboard: c.WriteClipboard,
					})
				}
			} else {
				st.NoInputMethod = false
				st.CursorShape = editor.CursorBar
				st.OnInput = func(ev ui.InputEvent) bool {
					if p.sigHelp != nil && ev.Kind == ui.InputKeyDown && ev.Key == ui.KeyEscape {
						a.dismissSignatureHelp(p)
						return true
					}
					return false
				}
			}
			tv := editor.View(c, st).Fill()
			if !overlay && focused && p.sigHelp != nil {
				a.signatureHelpOverlay(c, k, p)
			}
			if !overlay {
				a.syncMarkdownPreviewScroll(t, p)
			}
			if !overlay && a.focusReq == p {
				tv.Focus()
				a.focusReq = nil
			}
			if !overlay && tv.Focused() && t.Focus != p {
				t.Focus = p
				a.changed()
			}
		})
		if overZone != dropNone && overZone != dropCenter {
			a.dropIndicator(c, k, overZone)
		}
	})
	if !overlay {
		if value, ok := ui.Drop[any](card); ok {
			switch value := value.(type) {
			case *pane:
				if value != nil && value != p && t.contains(value) {
					if x, y, inside := card.PointerPosition(); inside && width > 0 && height > 0 {
						zone := dropZoneAt(x, y, width, height)
						if zone != dropNone && zone != dropCenter {
							a.later(func() { a.movePaneTo(t, value, p, zone) })
						}
					}
				}
			case explorer.DragItem:
				if value.Rel != "" && !value.Dir {
					zone := dropCenter
					if x, y, inside := card.PointerPosition(); inside && width > 0 && height > 0 {
						zone = dropZoneAt(x, y, width, height)
					}
					rel := value.Rel
					a.later(func() { a.openDroppedExplorerFile(t, p, rel, zone) })
				}
			}
		}
	}
	if p.dragging || (overTarget && (overExplorerFile || (overZone != dropNone && overZone != dropCenter))) {
		card.Border(2, k.Text)
	}
	if !overlay && card.Pressed() {
		clearPaneAttention(p)
		if t.Focus != p {
			t.Focus = p
			if !p.Preview && (p.Note != nil || p.Term != nil) {
				a.focusReq = p
			}
			a.changed()
		}
	}
	return card
}

// movePaneTo drops from on the named edge of to and inserts it there.
func (a *app) movePaneTo(t *tab, from, to *pane, zone paneDrop) {
	if t == nil || t != a.tab() {
		return
	}
	a.clearZoom(t)
	if !t.movePane(from, to, zone) {
		return
	}
	t.Focus = from
	a.focusReq = from
	a.changed()
	a.invalidate()
}

// dropZoneAt divides a pane into a middle and four edges.
func dropZoneAt(x, y, w, h float32) paneDrop {
	if w <= 0 || h <= 0 {
		return dropCenter
	}
	switch {
	case y < h*0.3:
		return dropAbove
	case y > h*0.7:
		return dropBelow
	case x < w*0.25:
		return dropLeft
	case x > w*0.75:
		return dropRight
	default:
		return dropCenter
	}
}

// dropIndicator paints the edge that will take the dragged pane.
func (a *app) dropIndicator(c *ui.Context, k *colors, zone paneDrop) {
	bar := ui.Box(c.Key("drop-indicator")).Absolute().PassThrough().
		Background(k.Text).Radius(2)
	switch zone {
	case dropAbove:
		bar.Top(2).Left(8).Right(8).Height(3)
	case dropBelow:
		bar.Bottom(2).Left(8).Right(8).Height(3)
	case dropLeft:
		bar.Top(8).Bottom(8).Left(2).Width(3)
	case dropRight:
		bar.Top(8).Bottom(8).Right(2).Width(3)
	}
}

// emptyPaneBody is the file picker inside a blank split: type to filter,
// Enter or a click opens the file in this pane.
func (a *app) emptyPaneBody(c *ui.Context, k *colors, t *tab, p *pane) {
	choices := a.paneChoices(p.emptyQuery)
	p.emptySel = min(max(p.emptySel, 0), max(len(choices)-1, 0))
	reveal := false
	ui.Column(c).Fill().Padding(14, 16).Gap(8).Children(func() {
		ui.Text(c, "Open a file in this pane").FontSize(c.Theme().Rem(1)).
			FontWeight(600).TextColor(k.Text)
		ui.Row(c).Children(func() {
			in := ui.SearchField(c.Key("empty-pane-search:"+p.ID), &p.emptyQuery).Grow(1).
				Height(32).Padding(0, 9).Gap(7).Radius(radius).Background(k.Hover).
				Border(0, ui.Transparent).FontSize(c.Theme().Rem(1)).Placeholder("Type a file name…").
				Label("Search files")
			if t.Focus == p {
				in.AutoFocus()
			}
			if in.Focused() && t.Focus != p {
				t.Focus = p
				a.changed()
			}
			if listNav(in, &p.emptySel, len(choices), &reveal) {
				ch := choices[p.emptySel]
				a.later(func() { a.openPaneChoice(t, p, ch) })
			}
		})
		ui.Scroll(c).Grow(1).MinHeight(0).Padding(2).Gap(2).Children(func() {
			if len(choices) == 0 {
				emptyLine(c, k, "No files match")
				return
			}
			for i, ch := range choices {
				i, ch := i, ch
				selected := i == p.emptySel
				row, clicked := fileChoiceRow(c, k, "open:"+p.ID+":"+ch.rel, ch, selected)
				if selected {
					row.Background(c.Theme().Accent)
				}
				if selected && reveal {
					row.ScrollIntoView()
				}
				if clicked {
					a.later(func() { a.openPaneChoice(t, p, ch) })
				}
			}
		})
	})
}

type fileChoice struct {
	title string
	rel   string
	note  *note
	asset *assetFile
}

// paneChoices lists the files a blank pane can open, filtered by q.
func (a *app) paneChoices(q string) []fileChoice {
	if a.store == nil {
		return nil
	}
	q = strings.ToLower(strings.TrimSpace(q))
	match := func(s string) bool { return q == "" || fuzzy(strings.ToLower(s), q) }
	var out []fileChoice
	for _, n := range a.store.Sorted() {
		if !match(n.Title + " " + n.Rel) {
			continue
		}
		out = append(out, fileChoice{title: n.Title, rel: n.Rel, note: n})
	}
	var assets []*assetFile
	for _, f := range a.store.Assets {
		assets = append(assets, f)
	}
	sort.Slice(assets, func(i, j int) bool { return assets[i].Rel < assets[j].Rel })
	for _, f := range assets {
		if !match(f.Name + " " + f.Rel) {
			continue
		}
		out = append(out, fileChoice{title: f.Name, rel: f.Rel, asset: f})
	}
	return out
}

func (a *app) openPaneChoice(t *tab, p *pane, ch fileChoice) {
	if t == nil || t != a.tab() || p == nil || !t.contains(p) {
		return
	}
	t.Focus = p
	switch {
	case ch.note != nil:
		a.openNote(ch.note)
	case ch.asset != nil:
		a.openAsset(ch.asset)
	}
}

func paneName(p *pane) string {
	switch {
	case p == nil:
		return "New Pane"
	case p.Note != nil:
		return p.Note.Title
	case p.Asset != nil:
		return p.Asset.Name
	case p.Term != nil:
		return p.Term.displayTitle()
	default:
		return "New Pane"
	}
}

// paneHeader draws a note's icon, title, its unsaved dot, and its
// buttons.
func (a *app) paneHeader(c *ui.Context, k *colors, t *tab, p *pane, width float32, overlay bool) {
	// At ordinary split widths the full toolbar leaves only a few title
	// characters. Compact before that happens, not only at the old 230px.
	compact := width > 0 && width < 360
	ultra := width > 0 && width < 160
	name := "New Pane"
	tooltip := "New Pane"
	ic := icons.Icon("file-plus")
	switch {
	case p.Note != nil:
		name, ic = p.Note.Title, icons.FileIcon(p.Note.Path)
		tooltip = filepath.ToSlash(p.Note.Rel)
	case p.Asset != nil:
		name, ic = p.Asset.Name, icons.FileIcon(p.Asset.Path)
		tooltip = filepath.ToSlash(p.Asset.Rel)
	case p.Term != nil:
		name, ic = p.Term.displayTitle(), icons.Icon("square-terminal")
		tooltip = name
	}
	subtitle := ""
	if !compact {
		subtitle = paneSubtitle(t, p)
	}
	hpad, hgap := float32(12), float32(7)
	btn, isize := float32(iconBtnMd), float32(iconGlyphMd)
	if compact {
		hpad, hgap, btn, isize = 4, 5, iconBtnSm, iconGlyphSm
	}
	// With more than one pane on screen, name each action's accessible
	// label after its pane, so the four visible button sets stay distinct.
	suffix := ""
	if len(t.panes()) > 1 {
		suffix = " " + name
	}
	h := ui.Row(c).Height(headerH).Padding(0, 6, 0, hpad).Gap(hgap).
		AlignItems(ui.Center).MinWidth(0)
	if !overlay && h.DoubleClicked() {
		a.later(func() {
			t.Focus = p
			a.toggleZoom()
		})
	}
	if !overlay && h.Clicked() {
		clearPaneAttention(p)
		t.Focus = p
		if !p.Preview {
			a.focusReq = p
		}
		a.changed()
	}
	h.Children(func() {
		iconSize := float32(16)
		handleW, gripSize := float32(15), float32(12)
		if compact {
			iconSize, handleW, gripSize = 14, 12, 10
		}
		grip := ui.Row(c).Width(handleW).Height(headerH).Center().
			Cursor(ui.CursorGrab).Role(ui.RoleButton).Label("Move Pane " + name)
		if !overlay {
			grip.Drag(p)
		}
		if grip.Hovered() {
			grip.Background(k.Hover)
		}
		p.dragging = grip.Dragging()
		grip.Children(func() {
			ui.Icon(c, icons.Icon("grip-vertical")).Size(gripSize, gripSize).TextColor(k.TextFaint)
		})
		if !ultra {
			ui.Icon(c, ic).Size(iconSize, iconSize).TextColor(k.Text).Tooltip(tooltip)
		}
		ui.Row(c).Grow(1).MinWidth(0).Gap(5).AlignItems(ui.Center).ClipX().Children(func() {
			ui.Text(c, name).FontSize(c.Theme().Rem(12.5 / 13)).FontWeight(600).TextColor(k.Text).
				Label("Pane title " + name).Tooltip(tooltip).SingleLine().Ellipsis("…").Shrink(1).MinWidth(0)
			if subtitle != "" {
				ui.Text(c, subtitle).FontSize(c.Theme().Rem(11.0 / 13)).TextColor(k.TextFaint).
					SingleLine().TruncateStart().Shrink(2).MinWidth(0)
			}
			if !compact && p.Note != nil && !p.Preview && p.symCrumb != "" {
				crumb := ui.Row(c.Key("crumb:"+p.ID)).Gap(4).AlignItems(ui.Center).
					Padding(1, 5).Radius(5).Cursor(ui.CursorPointer).Shrink(2).MinWidth(0).ClipX().
					Tooltip(a.tip("Go to Symbol", cmdSymbols))
				if !overlay && crumb.Hovered() {
					crumb.Background(k.Hover)
				}
				if !overlay && crumb.Clicked() {
					a.later(func() {
						t.Focus = p
						a.openSymbols()
					})
				}
				crumb.Children(func() {
					ui.Text(c, "›").FontSize(c.Theme().Rem(11.0 / 13)).TextColor(k.TextFaint).Shrink(0)
					ui.Text(c, p.symCrumb).Font(noteFont.Family).FontSize(c.Theme().Rem(11.0 / 13)).
						TextColor(k.TextMuted).SingleLine().Ellipsis("…").Shrink(1).MinWidth(0)
				})
			}
			if p.Note != nil && p.Note.Dirty() {
				ui.Box(c).Size(dotSize, dotSize).Radius(dotSize/2).Background(k.Attention).Margin(0, 0, 0, 2).
					Tooltip("Unsaved edits")
			} else if p.Term != nil && p.Term.attention {
				tip := "Needs attention"
				if p.Term.attentionNote != "" {
					tip = p.Term.attentionNote
				}
				ui.Box(c).Size(dotSize, dotSize).Radius(dotSize/2).Background(k.Attention).Margin(0, 0, 0, 2).
					Tooltip(tip)
			}
		})
		previewSideAction := func() {
			if p.Note == nil || !isMarkdownPath(p.Note.Path) || p.Preview {
				return
			}
			sideBtn := iconButton(c, k, "layout-grid", "Preview to the Side"+suffix, btn, isize).
				Tooltip(a.tip("Open Markdown Preview to the Side", cmdMarkdownPreviewSide)).Disabled(overlay)
			if !overlay && sideBtn.Clicked() {
				a.later(func() {
					t.Focus = p
					a.openMarkdownPreviewSide()
				})
			}
		}
		if !compact {
			previewSideAction()
		}
		splitRightAction := func() {
			if iconButton(c, k, "columns-2", "Split Right"+suffix, btn, isize).
				Tooltip(a.tip("Split Right", cmdSplitRight)).Clicked() {
				a.later(func() {
					t.Focus = p
					a.splitFocusedSide(splitRight)
				})
			}
		}
		splitDownAction := func() {
			if iconButton(c, k, "rows-2", "Split Down"+suffix, btn, isize).
				Tooltip(a.tip("Split Down", cmdSplitDown)).Clicked() {
				a.later(func() {
					t.Focus = p
					a.splitFocusedSide(splitDown)
				})
			}
		}
		zoomAction := func() {
			zoom, label := "maximize-2", "Zoom"
			if t.Zoom == p {
				zoom, label = "minimize-2", "Unzoom"
			}
			if iconButton(c, k, zoom, label+suffix, btn, isize).Tooltip(a.tip(label, cmdZoom)).Clicked() {
				a.later(func() {
					t.Focus = p
					a.toggleZoom()
				})
			}
		}
		closeAction := func() {
			if iconButton(c, k, "x", "Close Pane"+suffix, btn, isize).
				Tooltip(a.tip("Close Pane", cmdClosePane)).Clicked() {
				a.laterPane(p)
			}
		}
		actions := ui.Toolbar(c, func() {
			if compact {
				closeAction()
				previewSideAction()
				splitRightAction()
				splitDownAction()
				zoomAction()
			} else {
				splitRightAction()
				splitDownAction()
				zoomAction()
				closeAction()
			}
		}).Label("Pane actions" + suffix).Padding(0).Gap(1).Shrink(0)
		if compact {
			// Leave enough of the title to distinguish adjacent files. MyGo
			// keeps the close action and moves the rest into its overflow.
			actions.MaxWidth(44)
		}
		if ultra {
			actions.MaxWidth(22)
		}
		if overlay {
			// Keep the buttons visible while the copy fades, but let no
			// click reach them.
			actions.Disabled(true)
		}
	})
}

// editorContextMenu appends Mojian's LSP, Git and Terminal actions to the
// editor's built-in right-click menu.
func (a *app) editorContextMenu(m *ui.Menu, p *pane) {
	if p == nil || p.Note == nil || p.state == nil {
		return
	}
	m.Separator()
	if m.Item("Find References").Chosen() {
		a.later(func() { a.openReferences() })
	}
	if m.Item("Quick Fix / Code Actions…").Chosen() {
		a.later(func() { a.openCodeActions() })
	}
	if m.Item("Signature Help").Chosen() {
		a.later(func() { a.triggerSignatureHelp() })
	}
	if m.Item("Rename Symbol…").Chosen() {
		a.later(func() { a.openRenameSymbol() })
	}
	if m.Item("Format Document").Chosen() {
		a.later(func() { a.formatDocument() })
	}
	m.Separator()
	m.Submenu("Folding", func(sub *ui.Menu) {
		if sub.Item("Fold").Chosen() {
			a.later(func() { a.editorFold() })
		}
		if sub.Item("Unfold").Chosen() {
			a.later(func() { a.editorUnfold() })
		}
		if sub.Item("Toggle Fold").Chosen() {
			a.later(func() { a.editorToggleFold() })
		}
		sub.Separator()
		if sub.Item("Fold All").Chosen() {
			a.later(func() { a.editorFoldAll() })
		}
		if sub.Item("Unfold All").Chosen() {
			a.later(func() { a.editorUnfoldAll() })
		}
		if sub.Item("Fold Imports").Chosen() {
			a.later(func() { a.editorFoldImports() })
		}
	})
	if isMarkdownPath(p.Note.Path) {
		m.Separator()
		if m.Item("Open Markdown Preview to the Side").Chosen() {
			a.later(func() { a.openMarkdownPreviewSide() })
		}
	}
	if terminalsSupported() {
		m.Separator()
		if m.Item("Send Reference to Terminal").Chosen() {
			a.later(func() { a.sendReferenceToTerminal() })
		}
	}
	if p.state.DiffBase != nil {
		m.Separator()
		if m.Item("Revert Git Change").Chosen() {
			a.later(func() { a.editorRevertHunk() })
		}
	}
}

// paneSubtitle returns the shortest distinguishing parent directory suffix
// when two or more open panes in t share the same file title.
func paneSubtitle(t *tab, p *pane) string {
	if t == nil || p == nil {
		return ""
	}
	title, rel := paneTitleAndRel(p)
	if title == "" || rel == "" {
		return ""
	}
	var peers []string
	for _, other := range t.panes() {
		if other == p {
			continue
		}
		ot, orel := paneTitleAndRel(other)
		if ot == title && orel != "" && orel != rel {
			peers = append(peers, filepath.ToSlash(orel))
		}
	}
	if len(peers) == 0 {
		return ""
	}
	dir := filepath.ToSlash(filepath.Dir(rel))
	if dir == "." || dir == "" {
		return "./"
	}
	parts := strings.Split(dir, "/")
	for depth := 1; depth <= len(parts); depth++ {
		cand := strings.Join(parts[len(parts)-depth:], "/")
		unique := true
		for _, orel := range peers {
			odir := filepath.ToSlash(filepath.Dir(orel))
			if odir == "." {
				odir = ""
			}
			if odir == cand || strings.HasSuffix(odir, "/"+cand) {
				unique = false
				break
			}
		}
		if unique {
			return cand
		}
	}
	return dir
}

func paneTitleAndRel(p *pane) (string, string) {
	switch {
	case p.Note != nil:
		return p.Note.Title, p.Note.Rel
	case p.Asset != nil:
		return p.Asset.Name, p.Asset.Rel
	default:
		return "", ""
	}
}

// laterPane closes the pane after the frame that shows it, so the tab and
// its siblings are not changed while they build.
func (a *app) laterPane(p *pane) {
	a.later(func() { a.closePane(p) })
}

// vimOpenFile opens a relative path requested by Vim :e/:sp/:vs/:tabnew, or
// opens Quick Open when name is empty.
func (a *app) vimOpenFile(name string) {
	name = strings.TrimSpace(name)
	if name == "" {
		a.openQuickOpen()
		return
	}
	if a.store == nil {
		return
	}
	if n := a.store.ByRelPath(filepath.FromSlash(name)); n != nil {
		a.openNote(n)
		return
	}
	low := strings.ToLower(filepath.ToSlash(name))
	for _, n := range a.store.Sorted() {
		rel := strings.ToLower(filepath.ToSlash(n.Rel))
		if rel == low || strings.HasSuffix(rel, "/"+low) || strings.ToLower(n.Title) == low {
			a.openNote(n)
			return
		}
	}
	a.openQuickOpen()
	a.quickQuery = name
	a.invalidate()
}

// paneOverlay is a copy of a pane card over the place it last showed: a
// pane leaving the layout, or one a zoom hid. It outlives the card in the
// layout only until its fade rests.
type paneOverlay struct {
	p    *pane
	t    *tab
	rect ui.Rect
	kind paneCopy // paneCopyClosing or paneCopyHidden
}

// overlayKey is the identity of a pane's copy, so a closing copy and a
// hidden copy never take one.
type overlayKey struct {
	id   string
	kind paneCopy
}
