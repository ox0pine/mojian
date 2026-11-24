package app

import (
	"fmt"
	"strings"

	"mojian/internal/motion"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
)

const statusBarH = 24

// statusBar draws a quiet bottom strip when ShowStatusBar is enabled:
// Vim mode, LSP server & diagnostic counts, remote reconnect state,
// clickable cursor line/column, indentation menu, line ending toggle, and file language.
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
				if st := a.lspServerStateFor(p.Note.Rel); st != nil {
					label := st.name
					fg := k.TextMuted
					tip := "LSP: " + st.name + " (click for status & logs)"
					switch {
					case st.disabled:
						label = st.name + " (off)"
						fg = k.TextFaint
						tip = "LSP disabled in settings.json (click for status)"
					case st.starting:
						label = st.name + "…"
						fg = k.Attention
						tip = "Starting " + st.name + "… (click for status)"
					case st.failed:
						label = st.name + " !"
						fg = k.Danger
						if st.errMsg != "" {
							tip = st.name + " failed: " + st.errMsg + " (click for logs)"
						} else {
							tip = st.name + " failed (click for logs)"
						}
					}
					btn := ui.ButtonBase(c.Key("status-lsp")).Padding(1, 5).Radius(4).
						Cursor(ui.CursorPointer).Tooltip(tip).Label("LSP Status " + st.name)
					if btn.Hovered() {
						btn.Background(k.Hover)
					}
					if btn.Clicked() {
						a.later(func() { a.openLSPStatus() })
					}
					btn.Children(func() {
						ui.Text(c, label).FontSize(c.Theme().Rem(11.0 / 13)).TextColor(fg).Shrink(0)
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

			// Right side: clickable cursor position, indentation menu, line ending toggle, language.
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
			posBtn := ui.ButtonBase(c.Key("status-cursor")).Padding(1, 5).Radius(4).
				Cursor(ui.CursorPointer).Tooltip("Go to Line").Label("Go to line")
			if posBtn.Hovered() {
				posBtn.Background(k.Hover)
			}
			if posBtn.Clicked() {
				a.later(func() {
					if p.state != nil {
						p.state.Run(editor.GoToLine)
						a.invalidate()
					}
				})
			}
			posBtn.Children(func() {
				ui.Text(c, cursorText).Font(noteFont.Family).FontSize(c.Theme().Rem(11.0 / 13)).
					TextColor(k.TextMuted).Shrink(0)
			})

			a.statusIndentMenu(c, k, p)

			eol := "LF"
			isCRLF := p.Note.Buffer().LineEnding() == "\r\n"
			if isCRLF {
				eol = "CRLF"
			}
			eolBtn := ui.ButtonBase(c.Key("status-eol")).Padding(1, 5).Radius(4).
				Cursor(ui.CursorPointer).Tooltip("Click to toggle LF / CRLF").Label("Line ending " + eol)
			if eolBtn.Hovered() {
				eolBtn.Background(k.Hover)
			}
			if eolBtn.Clicked() {
				noteRef := p.Note
				nextEnding := "\r\n"
				if isCRLF {
					nextEnding = "\n"
				}
				a.later(func() {
					noteRef.Buffer().SetLineEnding(nextEnding)
					a.invalidate()
				})
			}
			eolBtn.Children(func() {
				ui.Text(c, eol).FontSize(c.Theme().Rem(11.0 / 13)).TextColor(k.TextFaint).Shrink(0)
			})

			if lang := p.Note.Buffer().Language(); lang != nil && lang.Name != "" {
				ui.Text(c, lang.Name).FontSize(c.Theme().Rem(11.0 / 13)).TextColor(k.TextFaint).Shrink(0)
			}
		})
}

func (a *app) statusIndentMenu(c *ui.Context, k *colors, p *pane) {
	_, size := a.effectiveIndent(p, p.Note)
	useSpaces := a.insertSpacesFor(p, p.Note)
	indentLabel := fmt.Sprintf("Spaces: %d", size)
	if !useSpaces {
		indentLabel = fmt.Sprintf("Tabs: %d", size)
	}
	m := ui.MenuBase(c.Key("status-indent-menu"), &a.indentMenuOpen)
	trig := m.Trigger.Padding(1, 5).Radius(4).Cursor(ui.CursorPointer).
		Tooltip("Change indentation for this pane").Label("Indentation " + indentLabel)
	if a.indentMenuOpen || trig.Hovered() {
		trig.Background(k.Hover)
	}
	drop := float32(0)
	if a.indentMenuOpen {
		drop = 1
	}
	drop = trig.AnimateTo("indent-menu", drop, motion.PopoverIn)
	trig.Children(func() {
		ui.Text(c, indentLabel).FontSize(c.Theme().Rem(11.0 / 13)).TextColor(k.TextFaint).Shrink(0)
	})
	m.Popup(func(panel ui.Element) {
		a.menuSurface(panel, k, drop)
		panel.Margin(0, 0, 4, 0)
		menuHeader(c, k, "INDENTATION", "")
		if menuChoice(c, k, m, "indent:spaces", "sliders-horizontal", "Use Spaces", useSpaces).Clicked() {
			p.indentOverride = editor.IndentSpaces
			a.invalidate()
		}
		if menuChoice(c, k, m, "indent:tabs", "sliders-horizontal", "Use Tabs", !useSpaces).Clicked() {
			p.indentOverride = editor.IndentTabs
			a.invalidate()
		}
		ui.Divider(c).Margin(4, 6)
		for _, w := range []int{2, 4, 8} {
			w := w
			if menuChoice(c, k, m, fmt.Sprintf("indent:w:%d", w), "sliders-horizontal", fmt.Sprintf("Tab Width: %d", w), size == w).Clicked() {
				p.tabSizeOverride = w
				a.invalidate()
			}
		}
		if p.indentOverride != 0 || p.tabSizeOverride != 0 {
			ui.Divider(c).Margin(4, 6)
			if menuAction(c, k, m, "indent:reset", "refresh-cw", "Reset to Default").Clicked() {
				p.indentOverride = 0
				p.tabSizeOverride = 0
				a.invalidate()
			}
		}
	})
}

func (a *app) paneVimModeLabel(p *pane) string {
	if p == nil || p.vim == nil {
		return "NORMAL"
	}
	return p.vim.Label()
}
