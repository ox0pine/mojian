package app

import (
	"fmt"
	"os"
	"strings"

	"mojian/internal/motion"
	"mojian/internal/sysstat"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
)

const statusBarH = 24

// statusBar draws a quiet bottom strip when ShowStatusBar is enabled:
// Vim mode, LSP server & diagnostic counts, terminal host/dir, optional
// machine telemetry (CPU/MEM/GPU), terminal size, clickable cursor
// line/column, indentation menu, line ending toggle, and file language.
func (a *app) statusBar(c *ui.Context, k *colors) {
	if !a.ShowStatusBar {
		a.stopSysStats()
		return
	}
	p := a.focusPane()
	a.syncSysStats(p)
	isTerm := p != nil && p.Term != nil
	ui.Row(c.Key("status-bar")).Height(statusBarH).Padding(0, 12).Gap(12).
		AlignItems(ui.Center).BorderWidth(1, 0, 0, 0).BorderColor(k.PanelBorder).
		Children(func() {
			if isTerm {
				hostBadge := "local · " + localHostLabel()
				switch {
				case p.Term.Spec.Server != "" && p.Term.Spec.Server != localServerID:
					hostBadge = "ssh · " + a.hostLabel(p.Term.Spec.Server)
				case p.Term.Spec.Host != "":
					hostBadge = "ssh · " + p.Term.Spec.Host
				}
				ui.Text(c, hostBadge).Font(noteFont.Family).FontSize(c.Theme().Rem(10.5/13)).
					FontWeight(600).TextColor(k.TextMuted).Padding(1, 6).Radius(4).Background(k.Hover).Shrink(0)
				if p.Term.Term != nil {
					if dir := strings.TrimSpace(p.Term.Term.Dir()); dir != "" {
						short := shortTermDir(dir, p.Term.Spec.Server == "" || p.Term.Spec.Server == localServerID)
						ui.Text(c, short).Font(noteFont.Family).FontSize(c.Theme().Rem(11.0 / 13)).
							TextColor(k.TextMuted).SingleLine().TruncateStart().MaxWidth(320).Shrink(1).MinWidth(0).
							Tooltip(dir)
					}
				}
			} else {
				// Left side: Vim mode, LSP status, diagnostics.
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
			}
			if a.serverAlias != "" && a.remoteReconnecting[a.serverAlias] {
				ui.Row(c).Gap(6).AlignItems(ui.Center).Shrink(0).Children(func() {
					ui.Spinner(c).Size(10, 10).Label("Reconnecting")
					ui.Text(c, "Reconnecting "+a.serverAlias+"…").FontSize(c.Theme().Rem(11.0 / 13)).
						TextColor(k.Attention)
				})
			}

			ui.Spacer(c)

			a.statusSysMetrics(c, k, p)

			if isTerm {
				if p.Term.Term != nil {
					if cols, rows := p.Term.Term.Size(); cols > 0 && rows > 0 {
						ui.Text(c, fmt.Sprintf("%d×%d", cols, rows)).Font(noteFont.Family).
							FontSize(c.Theme().Rem(11.0 / 13)).TextColor(k.TextFaint).Shrink(0)
					}
				}
				return
			}

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

func (a *app) statusSysMetrics(c *ui.Context, k *colors, p *pane) {
	if !a.sysSnapOK || a.resolveSysTarget(p).key == "" {
		return
	}
	snap := a.sysSnap
	metricColor := func(pct float64) ui.Color {
		if pct >= 85 {
			return k.Attention
		}
		return k.TextMuted
	}
	ui.Row(c.Key("status-sys-metrics")).Gap(10).AlignItems(ui.Center).Shrink(0).Children(func() {
		if a.StatusBarCPU && snap.HasCPU {
			ui.Text(c, fmt.Sprintf("CPU %.0f%%", snap.CPUPercent)).Font(noteFont.Family).
				FontSize(c.Theme().Rem(11.0 / 13)).TextColor(metricColor(snap.CPUPercent)).Shrink(0)
		}
		if a.StatusBarMemory && snap.HasMem {
			label := fmt.Sprintf("MEM %s/%s (%.0f%%)",
				sysstat.FormatGiB(snap.MemUsedBytes),
				sysstat.FormatGiB(snap.MemTotalBytes),
				snap.MemPercent,
			)
			ui.Text(c, label).Font(noteFont.Family).
				FontSize(c.Theme().Rem(11.0 / 13)).TextColor(metricColor(snap.MemPercent)).Shrink(0)
		}
		if a.StatusBarGPU && snap.HasGPU {
			label := fmt.Sprintf("GPU %.0f%%", snap.GPUPercent)
			if snap.HasVRAM && snap.GPUVRAMTotal > 0 {
				label = fmt.Sprintf("GPU %.0f%% · %s/%s",
					snap.GPUPercent,
					sysstat.FormatGiB(snap.GPUVRAMUsed),
					sysstat.FormatGiB(snap.GPUVRAMTotal),
				)
			}
			el := ui.Text(c, label).Font(noteFont.Family).
				FontSize(c.Theme().Rem(11.0 / 13)).TextColor(metricColor(snap.GPUPercent)).Shrink(0)
			if snap.GPUName != "" {
				el.Tooltip(snap.GPUName)
			}
		}
	})
}

func shortTermDir(dir string, local bool) string {
	if !local {
		return dir
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		if dir == home {
			return "~"
		}
		if strings.HasPrefix(dir, home+"/") || strings.HasPrefix(dir, home+`\`) {
			return "~" + dir[len(home):]
		}
	}
	return dir
}
