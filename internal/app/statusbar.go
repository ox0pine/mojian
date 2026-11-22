package app

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
)

const statusBarH = 24

// statusBar draws a quiet bottom strip when ShowStatusBar is enabled:
// Vim mode, LSP server & diagnostic counts, remote reconnect state,
// cursor line/column, indentation, line ending, and file language.
func (a *app) statusBar(c *ui.Context, k *colors) {
	if !a.ShowStatusBar {
		return
	}
	p := a.focusPane()
	ui.Row(c.Key("status-bar")).Height(statusBarH).Padding(0, 12).Gap(12).
		AlignItems(ui.Center).BorderWidth(1, 0, 0, 0).BorderColor(k.PanelBorder).
		Children(func() {
			// Left side: Vim mode, LSP status, diagnostics, remote reconnect.
			if a.EditorVimMode && p != nil && p.Note != nil && !p.Preview {
				mode := a.paneVimModeLabel(p)
				bg := k.Hover
				fg := k.Text
				if mode == "INSERT" {
					bg = c.Theme().Accent
					fg = c.Theme().AccentText
				} else if strings.HasPrefix(mode, "VISUAL") {
					bg = k.Attention.Alpha(0.18)
					fg = k.Attention
				}
				ui.Text(c, mode).Font(noteFont.Family).FontSize(c.Theme().Rem(10.5/13)).
					FontWeight(650).TextColor(fg).Padding(1, 6).Radius(4).Background(bg).Shrink(0)
			}
			if p != nil && p.Note != nil {
				if srv := a.lspServerName(p.Note.Rel); srv != "" {
					btn := ui.ButtonBase(c.Key("status-lsp")).Padding(1, 5).Radius(4).
						Cursor(ui.CursorPointer).Tooltip("Click to restart " + srv).Label("Restart " + srv)
					if btn.Hovered() {
						btn.Background(k.Hover)
					}
					if btn.Clicked() {
						a.later(func() { a.restartLSP() })
					}
					btn.Children(func() {
						ui.Text(c, srv).FontSize(c.Theme().Rem(11.0 / 13)).TextColor(k.TextMuted).Shrink(0)
					})
				}
			}
			errs, warns := a.lspDiagCounts("")
			if errs > 0 || warns > 0 {
				btn := ui.ButtonBase(c.Key("status-problems")).Padding(1, 5).Radius(4).
					Cursor(ui.CursorPointer).Tooltip("Show workspace problems").Label("Show problems")
				if btn.Hovered() {
					btn.Background(k.Hover)
				}
				if btn.Clicked() {
					a.later(func() { a.openProblems() })
				}
				btn.Children(func() {
					ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
						if errs > 0 {
							ui.Text(c, fmt.Sprintf("%d err", errs)).FontSize(c.Theme().Rem(11.0 / 13)).
								FontWeight(600).TextColor(k.Danger).Shrink(0)
						}
						if warns > 0 {
							ui.Text(c, fmt.Sprintf("%d warn", warns)).FontSize(c.Theme().Rem(11.0 / 13)).
								FontWeight(550).TextColor(k.Attention).Shrink(0)
						}
					})
				})
			}
			if a.serverAlias != "" && a.remoteReconnecting[a.serverAlias] {
				ui.Row(c).Gap(6).AlignItems(ui.Center).Shrink(0).Children(func() {
					ui.Spinner(c).Size(10, 10).Label("Reconnecting")
					ui.Text(c, "Reconnecting "+a.serverAlias+"…").FontSize(c.Theme().Rem(11.0 / 13)).
						TextColor(k.Attention)
				})
			}

			ui.Spacer(c)

			// Right side: cursor position, indent, line ending, language.
			if p == nil || p.Note == nil || p.Preview || p.state == nil {
				return
			}
			st := p.state
			pos := st.CursorPos()
			cursorText := fmt.Sprintf("Ln %d, Col %d", pos.Line+1, pos.Col+1)
			if sel := st.Selection(); sel.Anchor != sel.Head {
				n := sel.Head - sel.Anchor
				if n < 0 {
					n = -n
				}
				cursorText += fmt.Sprintf(" (%d selected)", n)
			}
			ui.Text(c, cursorText).Font(noteFont.Family).FontSize(c.Theme().Rem(11.0 / 13)).
				TextColor(k.TextMuted).Shrink(0)

			indent := fmt.Sprintf("Spaces: %d", a.tabSize())
			if st.Indent == editor.IndentTabs || (st.Indent == editor.IndentAuto && strings.EqualFold(filepath.Ext(p.Note.Path), ".go")) {
				indent = fmt.Sprintf("Tabs: %d", a.tabSize())
			}
			ui.Text(c, indent).FontSize(c.Theme().Rem(11.0 / 13)).TextColor(k.TextFaint).Shrink(0)

			eol := "LF"
			if p.Note.Buffer().LineEnding() == "\r\n" {
				eol = "CRLF"
			}
			ui.Text(c, eol).FontSize(c.Theme().Rem(11.0 / 13)).TextColor(k.TextFaint).Shrink(0)

			if lang := p.Note.Buffer().Language(); lang != nil && lang.Name != "" {
				ui.Text(c, lang.Name).FontSize(c.Theme().Rem(11.0 / 13)).TextColor(k.TextFaint).Shrink(0)
			}
		})
}

func (a *app) paneVimModeLabel(p *pane) string {
	if p == nil || p.vim == nil {
		return "NORMAL"
	}
	return p.vim.Label()
}
