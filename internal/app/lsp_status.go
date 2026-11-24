package app

import (
	"fmt"
	"sort"
	"strings"

	"mojian/internal/icons"
	"mojian/internal/motion"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

func (a *app) openLSPStatus() {
	a.closeModals()
	a.lspStatusOpen = true
	if p := a.focusPane(); p != nil && p.Note != nil {
		if st := a.lspServerStateFor(p.Note.Rel); st != nil {
			a.lspStatusSel = st.group
		}
	}
	a.invalidate()
}

func (a *app) restartLSPGroup(group string) {
	if a.workspace == nil || a.workspace.lsp == nil || group == "" {
		return
	}
	if st, ok := a.workspace.lsp.servers[group]; ok {
		if st.client != nil {
			_ = st.client.Close()
		}
		delete(a.workspace.lsp.servers, group)
	}
	a.invalidate()
}

func (a *app) lspServerList() []*lspServerState {
	if a.workspace == nil || a.workspace.lsp == nil {
		return nil
	}
	out := make([]*lspServerState, 0, len(a.workspace.lsp.servers))
	for _, st := range a.workspace.lsp.servers {
		if st != nil {
			out = append(out, st)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].group < out[j].group })
	return out
}

func lspStateLabel(st *lspServerState) (label string, status int) {
	// status: 0=ready, 1=starting, 2=failed, 3=disabled
	if st == nil {
		return "Idle", 0
	}
	switch {
	case st.disabled:
		return "Disabled", 3
	case st.starting:
		return "Starting…", 1
	case st.failed:
		return "Failed", 2
	case st.client != nil && st.client.Ready():
		return "Ready", 0
	default:
		return "Idle", 0
	}
}

func (a *app) lspStatusModal(c *ui.Context, k *colors) {
	if !a.lspStatusOpen {
		return
	}
	servers := a.lspServerList()
	var selected *lspServerState
	for _, st := range servers {
		if st.group == a.lspStatusSel {
			selected = st
			break
		}
	}
	if selected == nil && len(servers) > 0 {
		selected = servers[0]
		a.lspStatusSel = selected.group
	}
	was := a.lspStatusOpen
	a.commandDialog(c, k, &a.lspStatusOpen, func() {
		// Header bar
		ui.Row(c).Height(46).Padding(0, 16).Gap(8).AlignItems(ui.Center).
			BorderWidth(0, 0, 1, 0).BorderColor(k.PanelBorder).Children(func() {
			ui.Icon(c, icons.Icon("info")).Size(16, 16).TextColor(k.TextMuted)
			ui.Text(c, "Language Servers").FontSize(c.Theme().Rem(14.0 / 13)).
				FontWeight(600).TextColor(k.Text).Grow(1).MinWidth(0)
			if a.settingsPath != "" {
				cfg := ui.ButtonBase(c.Key("lsp-config-btn")).Height(24).Padding(0, 10).Radius(radius - 2).
					Cursor(ui.CursorPointer).Label("Configure").Tooltip("Open settings.json to customize lspServers").
					Transition(motion.Colors)
				if cfg.Hovered() {
					cfg.Background(k.Pressed)
				} else {
					cfg.Background(k.Hover)
				}
				cfg.Children(func() {
					ui.Text(c, "Configure").FontSize(c.Theme().Rem(11.5 / 13)).FontWeight(550).TextColor(k.Text)
				})
				if cfg.Clicked() {
					a.later(func() {
						a.saveSettings()
						if err := mygo.Shell.OpenPath(a.settingsPath); err != nil {
							a.fail(err)
						}
					})
				}
			}
			restartAll := ui.ButtonBase(c.Key("lsp-restart-all")).Height(24).Padding(0, 10).Radius(radius - 2).
				Cursor(ui.CursorPointer).Label("Restart All").Disabled(len(servers) == 0).
				Transition(motion.Colors)
			if restartAll.Hovered() {
				restartAll.Background(k.Pressed)
			} else {
				restartAll.Background(k.Hover)
			}
			restartAll.Children(func() {
				ui.Text(c, "Restart All").FontSize(c.Theme().Rem(11.5 / 13)).FontWeight(550).TextColor(k.Text)
			})
			if restartAll.Clicked() {
				a.later(func() { a.restartLSP() })
			}
			if iconButton(c, k, "x", "Close", iconBtnSm, iconGlyphSm).Clicked() {
				a.lspStatusOpen = false
			}
		})

		if len(servers) == 0 {
			ui.Column(c).Padding(24, 16).Gap(6).AlignItems(ui.Center).Children(func() {
				ui.Text(c, "No language servers active in this workspace").FontSize(rowTitle).TextColor(k.TextMuted)
				ui.Text(c, "Open a source file or configure lspServers in settings.json.").
					FontSize(rowDetail).TextColor(k.TextFaint)
			})
			return
		}

		// Server list
		ui.Column(c).Padding(6).Gap(2).Children(func() {
			for _, st := range servers {
				st := st
				sel := selected != nil && selected.group == st.group
				label, code := lspStateLabel(st)
				row := ui.Row(c.Key("lsp-srv:"+st.group)).MinHeight(38).Padding(4, 10).Gap(8).
					Radius(radius).AlignItems(ui.Center).Cursor(ui.CursorPointer)
				fg, muted := k.Text, k.TextMuted
				if sel {
					row.Background(k.PanelSel)
				} else if row.Hovered() {
					row.Background(k.Hover)
				}
				if row.Clicked() {
					a.lspStatusSel = st.group
				}
				row.Children(func() {
					dotColor := k.TextMuted
					switch code {
					case 0:
						dotColor = c.Theme().Accent
					case 1:
						dotColor = k.Attention
					case 2:
						dotColor = k.Danger
					}
					ui.Box(c).Size(7, 7).Radius(3.5).Background(dotColor).Shrink(0)
					ui.Column(c).Grow(1).MinWidth(0).Gap(1).Children(func() {
						ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
							ui.Text(c, st.name).FontSize(rowTitle).FontWeight(550).TextColor(fg).SingleLine()
							ui.Text(c, "("+st.group+")").FontSize(rowDetail).TextColor(muted).SingleLine()
						})
						sub := strings.Join(st.command, " ")
						if st.errMsg != "" {
							sub = st.errMsg
						}
						if sub != "" {
							col := muted
							if st.errMsg != "" {
								col = k.Danger
							}
							ui.Text(c, sub).Font(noteFont.Family).FontSize(c.Theme().Rem(11.0 / 13)).
								TextColor(col).SingleLine().Ellipsis("…").MinWidth(0)
						}
					})
					ui.Text(c, label).FontSize(c.Theme().Rem(11.0 / 13)).FontWeight(550).
						TextColor(dotColor).Shrink(0)
					if !st.disabled {
						rbtn := ui.ButtonBase(c.Key("lsp-restart:"+st.group)).Height(22).Padding(0, 8).
							Radius(radius - 2).Cursor(ui.CursorPointer).Label("Restart " + st.name).
							Transition(motion.Colors)
						if rbtn.Hovered() {
							rbtn.Background(k.Pressed)
						} else {
							rbtn.Background(k.Hover)
						}
						rbtn.Children(func() {
							ui.Text(c, "Restart").FontSize(c.Theme().Rem(11.0 / 13)).TextColor(k.Text)
						})
						if rbtn.Clicked() {
							grp := st.group
							a.later(func() { a.restartLSPGroup(grp) })
						}
					}
				})
			}
		})

		if selected != nil {
			ui.Box(c).Height(1).Background(k.PanelBorder)
			ui.Row(c).Height(30).Padding(0, 14).Gap(8).AlignItems(ui.Center).
				BorderWidth(0, 0, 1, 0).BorderColor(k.PanelBorder).Children(func() {
				ui.Text(c, fmt.Sprintf("Logs · %s (%d lines)", selected.name, len(selected.logs))).
					FontSize(c.Theme().Rem(11.5 / 13)).FontWeight(550).TextColor(k.TextMuted).Grow(1).MinWidth(0)
				if len(selected.logs) > 0 {
					copyBtn := ui.ButtonBase(c.Key("lsp-copy-logs")).Height(20).Padding(0, 8).
						Radius(radius - 2).Cursor(ui.CursorPointer).Label("Copy Logs").
						Transition(motion.Colors)
					if copyBtn.Hovered() {
						copyBtn.Background(k.Pressed)
					} else {
						copyBtn.Background(k.Hover)
					}
					copyBtn.Children(func() {
						ui.Text(c, "Copy").FontSize(c.Theme().Rem(11.0 / 13)).TextColor(k.Text)
					})
					if copyBtn.Clicked() {
						joined := strings.Join(selected.logs, "\n")
						c.WriteClipboard(joined)
					}
				}
			})
			ui.Scroll(c).Key("lsp-logs-scroll").MaxHeight(200).Padding(8, 14).Children(func() {
				if len(selected.logs) == 0 {
					ui.Text(c, "No stderr or log messages recorded.").FontSize(rowDetail).TextColor(k.TextFaint)
					return
				}
				ui.Column(c).Gap(2).Children(func() {
					for i, line := range selected.logs {
						ui.Text(c.Key(fmt.Sprintf("log:%s:%d", selected.group, i)), line).
							Font(noteFont.Family).FontSize(c.Theme().Rem(11.0 / 13)).
							TextColor(k.TextMuted)
					}
				})
			})
		}
	})
	if was && !a.lspStatusOpen {
		a.restoreModalFocus()
	}
}
