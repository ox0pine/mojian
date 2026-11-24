package app

import (
	"mojian/internal/lsp"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
)

func editorWhitespaceFor(v int) editor.Whitespace {
	switch v {
	case 1:
		return editor.WhitespaceNone
	case 2:
		return editor.WhitespaceBoundary
	case 3:
		return editor.WhitespaceTrailing
	case 4:
		return editor.WhitespaceAll
	default:
		return editor.WhitespaceSelection
	}
}

func (a *app) runEditorCommand(cmd editor.Command) {
	if p := a.focusPane(); p != nil && p.Note != nil && !p.Preview && p.state != nil {
		p.state.Run(cmd)
		a.invalidate()
	}
}

// configureEditorOverlays styles the editor's built-in floating overlays
// (find bar, go-to-line bar, hover card and completion menu) with Mojian's
// Liquid Glass surface and theme borders.
func (a *app) configureEditorOverlays(p *pane, st *editor.State, k *colors) {
	if st == nil || k == nil {
		return
	}
	st.RenderFindBar = func(c *ui.Context, bar ui.Element, f *editor.FindBar) {
		editor.DefaultFindBar(c, bar, f)
		a.glassEditorSurface(bar, k)
	}
	st.RenderGoToLine = func(c *ui.Context, bar ui.Element, g *editor.GoToLineBar) {
		editor.DefaultGoToLine(c, bar, g)
		a.glassEditorSurface(bar, k)
	}
	st.RenderHover = func(c *ui.Context, panel ui.Element, h *editor.HoverCard) {
		editor.DefaultHover(c, panel, h)
		a.glassEditorSurface(panel, k)
	}
	st.RenderCompletions = func(c *ui.Context, panel ui.Element, m *editor.CompletionMenu) {
		a.renderGlassCompletions(c, panel, m, p, k)
	}
}

func (a *app) glassEditorSurface(el ui.Element, k *colors) ui.Element {
	return a.surface(el.Background(ui.Transparent), k.Panel).
		Border(1, k.PanelBorder).Radius(radius).
		Shadow(0, 4, 16, -2, k.ShadowDialog)
}

func (a *app) renderGlassCompletions(c *ui.Context, panel ui.Element, m *editor.CompletionMenu, p *pane, k *colors) {
	t := c.Theme()
	ct := m.Theme()
	font := m.Font()
	lh := m.LineHeight()
	a.glassEditorSurface(panel, k).
		Padding(4).MaxHeight(lh*9 + 140).MinWidth(240).MaxWidth(520)
	items := m.Items()
	sel := m.Selected()
	if p != nil {
		p.compOpen = true
		if sel >= 0 && sel < len(items) {
			if origIdx := findCompletionResultIndex(p.compResults, items[sel]); origIdx >= 0 {
				p.compSelectedIdx = origIdx
				res := p.compResults[origIdx]
				if _, resolved := p.compResolved[origIdx]; !resolved && len(res.Raw) > 0 && (res.HasData || len(res.AdditionalTextEdits) == 0) {
					a.resolveCompletionItemAsync(p, origIdx, res.Raw)
				}
			}
		}
	}
	const maxRows = 200
	panel.Children(func() {
		ui.Scroll(c).Key("rows").MaxHeight(lh * 9).Children(func() {
			for i, it := range items {
				if i >= maxRows {
					break
				}
				detail := it.Detail
				if p != nil {
					if origIdx := findCompletionResultIndex(p.compResults, it); origIdx >= 0 {
						if res, ok := p.compResolved[origIdx]; ok && res.Item.Detail != "" {
							detail = res.Item.Detail
						}
					}
				}
				row := m.Item(i).Row().Justify(ui.Start).Gap(8).AlignItems(ui.Center).
					Padding(2, 6).Radius(radius - 2).Height(lh + 2)
				if i == sel {
					row.Background(t.Accent.Alpha(0.18))
				} else if row.Hovered() {
					row.Background(k.Hover)
				}
				if row.Clicked() && p != nil {
					p.compAcceptHint = true
					if origIdx := findCompletionResultIndex(p.compResults, it); origIdx >= 0 {
						p.compSelectedIdx = origIdx
					}
				}
				row.Children(func() {
					ui.Text(c, it.Kind.Icon()).Font(noteFont.Family).FontSize(font.Size - 1).
						TextColor(ct.TokenColor(it.Kind.Token())).Width(16)
					ui.Text(c, it.Label).Font(font.Family).FontSize(font.Size).
						TextColor(ct.Text).SingleLine().Shrink(1)
					if detail != "" {
						ui.Text(c, detail).FontSize(font.Size - 1).
							TextColor(k.TextMuted).SingleLine().Shrink(1).Grow(1).TextAlign(ui.End)
					}
				})
			}
		})
		if sel >= 0 && sel < len(items) {
			doc := items[sel].Documentation
			if p != nil {
				if origIdx := findCompletionResultIndex(p.compResults, items[sel]); origIdx >= 0 {
					if res, ok := p.compResolved[origIdx]; ok && res.Item.Documentation != "" {
						doc = res.Item.Documentation
					}
				}
			}
			if doc != "" {
				ui.Box(c).Height(1).Background(k.PanelBorder).Margin(4, 0)
				ui.Scroll(c).Key("comp-doc").MaxHeight(120).Children(func() {
					ui.Text(c, doc).FontSize(t.FontSize-1).
						TextColor(k.TextMuted).MaxWidth(500).Padding(2, 6)
				})
			}
		}
	})
}

func findCompletionResultIndex(results []lsp.CompletionResult, target editor.Completion) int {
	for i, r := range results {
		if r.Item.Label == target.Label && r.Item.Detail == target.Detail && r.Item.Insert == target.Insert {
			return i
		}
	}
	for i, r := range results {
		if r.Item.Label == target.Label {
			return i
		}
	}
	return -1
}
