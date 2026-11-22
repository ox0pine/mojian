package app

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"mojian/internal/icons"
	"mojian/internal/session"

	"github.com/egoist/mygo/ui"
)

// openSSHPicker lists the hosts configured in Settings › Hosts. The app
// does not scan ~/.ssh/config: a host is added once, in Settings, and the
// system ssh still reads its own config for aliases, ProxyJump and keys.
func (a *app) openSSHPicker() {
	if !remoteSupported() {
		a.fail(errors.New("SSH terminals are unavailable in this build"))
		return
	}
	a.sshQuery = ""
	a.sshSel = 0
	a.sshList = ui.ListState{}
	a.closeModals()
	a.sshPick = true
	a.invalidate()
}

func (a *app) connectSSH(address string) {
	if address == "" {
		return
	}
	tt, err := a.startTerminalSpec(termSpec{Host: address})
	if err != nil {
		a.fail(err)
		return
	}
	a.git.Open = false
	if a.tab() == nil {
		t := &tab{ID: nextID("t")}
		a.tabs = append(a.tabs, t)
		a.active = len(a.tabs) - 1
	}
	a.addTerminalPane(tt)
}

func (a *app) sshPicker(c *ui.Context, k *colors) {
	if !a.sshPick {
		return
	}
	q := strings.ToLower(strings.TrimSpace(a.sshQuery))
	var filtered []hostConfig
	for _, h := range a.hosts() {
		if q == "" || fuzzy(strings.ToLower(h.Name+" "+h.Address), q) {
			filtered = append(filtered, h)
		}
	}
	a.sshSel = min(max(a.sshSel, 0), max(len(filtered)-1, 0))
	was := a.sshPick
	connected := ""
	commandDialog(c, k, &a.sshPick, func() {
		reveal := false
		in := sheetHeader(c, k, &a.sshQuery, "Search hosts…", "Connect to Server", &a.sshPick)
		if listNav(in, &a.sshSel, len(filtered), &reveal) {
			connected = filtered[a.sshSel].Address
		}
		if len(a.hosts()) == 0 {
			ui.Column(c).Height(sheetListHeight(c)).Children(func() {
				emptyState(c, k, "server", k.TextMuted, "No servers configured",
					"Add a server in Settings › Hosts, then come back here.", func() {
						if ui.Button(c, "Open Settings › Hosts").Clicked() {
							a.sshPick = false
							a.later(func() {
								a.settings.Cat = settingsHosts
								a.openSettingsWindow()
							})
						}
					})
			})
			return
		}
		a.sshList.Selected = &a.sshSel
		a.sshList.Key = func(i int) any { return filtered[i].Address }
		list := ui.List(c, &a.sshList, len(filtered), func(i int) {
			h := filtered[i]
			label := h.Name
			if label == "" {
				label = h.Address
			}
			row := ui.Row(c.Key(h.Address)).Height(rowH).Padding(0, rowPadX).Gap(rowGap).
				Radius(radius).AlignItems(ui.Center).Cursor(ui.CursorPointer).
				Label(label + " " + h.Address)
			fg, muted, icon := k.Text, k.TextFaint, k.TextMuted
			if i == a.sshSel {
				fg = c.Theme().AccentText
				muted, icon = fg.Alpha(0.9), fg.Alpha(0.9)
			} else if row.Hovered() {
				row.Background(k.Hover)
			}
			if row.Clicked() {
				connected = h.Address
			}
			row.Children(func() {
				ui.Icon(c, icons.Icon("server")).Size(15, 15).TextColor(icon)
				ui.Text(c, label).FontSize(rowTitle).FontWeight(500).TextColor(fg).
					SingleLine().Ellipsis("…").Shrink(0.3).MinWidth(0)
				ui.Text(c, h.Address).FontSize(rowDetail).TextColor(muted).
					SingleLine().Ellipsis("…").Shrink(1).MinWidth(0)
			})
		}).MaxHeight(sheetListHeight(c)).Padding(6)
		if len(filtered) == 0 {
			list.Children(func() { emptyLine(c, k, "No hosts match") })
		}
		if reveal {
			a.sshList.ScrollIntoView(a.sshSel)
		}
	})
	if connected != "" {
		a.sshPick = false
		a.later(func() { a.connectSSH(connected) })
	}
	if was && !a.sshPick {
		if t := a.tab(); t != nil && a.focusReq == nil {
			if t.Focus != nil {
				a.focusReq = t.Focus
			} else {
				a.focusReq = t.editorPane()
			}
		}
	}
}

// sshArgs connects to a configured host address with the system ssh, so
// ssh_config, the agent and ProxyJump keep working.
func sshArgs(host string) []string {
	return []string{"ssh", "-tt",
		"-o", "ServerAliveInterval=15", "-o", "ServerAliveCountMax=3",
		"--", host}
}

// Test seams for session listing, attaching and killing.
var (
	listSessionsFn = func(ctx context.Context, client *session.Client) ([]session.SessionInfo, error) {
		return client.ListContext(ctx)
	}
	killPickedSessionFn = func(client *session.Client, sid string) error {
		return client.Kill(sid)
	}
)

// clearPaneAttention drops the unread bell/exit marker on a terminal pane.
func clearPaneAttention(p *pane) {
	if p != nil && p.Term != nil {
		p.Term.attention = false
		p.Term.attentionNote = ""
	}
}

// markTerminalAttention marks a terminal pane as needing attention when it
// is not the active focused pane on a focused window.
func (a *app) markTerminalAttention(tt *termTab, note string) {
	if tt == nil {
		return
	}
	focused := a.win != nil && a.win.IsFocused() && !a.git.Open &&
		a.tab() != nil && a.tab().Focus != nil && a.tab().Focus.Term == tt
	if focused {
		return
	}
	tt.attention = true
	tt.attentionNote = note
}

// hasAttention reports whether any terminal pane in the tab has an unread
// bell or exit marker.
func (t *tab) hasAttention() bool {
	if t == nil {
		return false
	}
	for _, p := range t.panes() {
		if p.Term != nil && p.Term.attention {
			return true
		}
	}
	return false
}

// jumpToAttention focuses the next terminal pane with an unread bell,
// exit or OSC desktop notification marker.
func (a *app) jumpToAttention() {
	if len(a.tabs) == 0 {
		return
	}
	start := max(a.active, 0)
	for step := 0; step < len(a.tabs); step++ {
		idx := (start + step) % len(a.tabs)
		t := a.tabs[idx]
		if t == nil {
			continue
		}
		for _, p := range t.panes() {
			if p.Term == nil || !p.Term.attention {
				continue
			}
			a.closeModals()
			a.git.Open = false
			a.selectTab(idx)
			if t.Zoom != nil && t.Zoom != p {
				a.unzoom(t)
			}
			t.Focus = p
			clearPaneAttention(p)
			a.focusReq = p
			a.changed()
			a.invalidate()
			return
		}
	}
}

// openSessionPicker opens the terminal session picker for the active server
// (local or the open remote host) so detached or other-device sessions can
// be attached into a pane or terminated.
func (a *app) openSessionPicker() {
	if !terminalsSupported() {
		a.fail(errors.New("terminals are unavailable in this build"))
		return
	}
	a.closeModals()
	a.sessionPick = true
	a.sessionQuery = ""
	a.sessionSel = 0
	a.sessionList = ui.ListState{}
	a.sessionItems = nil
	a.sessionErr = ""
	a.sessionServer = a.defaultTerminalServer()
	a.refreshSessionPicker()
	a.invalidate()
}

func (a *app) refreshSessionPicker() {
	server := a.sessionServer
	if server == "" {
		server = localServerID
	}
	a.sessionSeq++
	seq := a.sessionSeq
	winSeq := a.windowSeq
	a.sessionBusy = true
	a.sessionErr = ""

	apply := func(items []session.SessionInfo, err error) {
		if a.windowSeq != winSeq || a.sessionSeq != seq {
			return
		}
		a.sessionBusy = false
		if err != nil {
			a.sessionErr = err.Error()
			a.sessionItems = nil
		} else {
			a.sessionErr = ""
			a.sessionItems = items
			a.sessionSel = min(max(a.sessionSel, 0), max(len(items)-1, 0))
		}
		a.invalidate()
	}

	client, err := a.terminalClientExisting(termSpec{Server: server})
	if err != nil {
		apply(nil, err)
		return
	}
	post := a.uiPoster()
	if post == nil {
		items, listErr := listSessionsFn(context.Background(), client)
		apply(items, listErr)
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		items, listErr := listSessionsFn(ctx, client)
		post(func() { apply(items, listErr) })
	}()
}

// attachedSessionPane finds an open terminal pane already showing sid on server.
func (a *app) attachedSessionPane(server, sid string) (tabIdx int, p *pane) {
	for i, t := range a.tabs {
		if t == nil {
			continue
		}
		for _, pane := range t.panes() {
			if pane.Term == nil || pane.Term.SID != sid {
				continue
			}
			s := pane.Term.Spec.Server
			if s == "" {
				s = localServerID
			}
			if s == server {
				return i, pane
			}
		}
	}
	return -1, nil
}

func (a *app) attachExistingSession(server, sid string) {
	if sid == "" {
		return
	}
	if server == "" {
		server = localServerID
	}
	if idx, p := a.attachedSessionPane(server, sid); p != nil {
		a.git.Open = false
		a.selectTab(idx)
		t := a.tabs[idx]
		if t.Zoom != nil && t.Zoom != p {
			a.unzoom(t)
		}
		t.Focus = p
		clearPaneAttention(p)
		a.focusReq = p
		a.invalidate()
		return
	}
	client, err := a.terminalClientExisting(termSpec{Server: server})
	if err != nil {
		a.fail(err)
		return
	}
	tt, err := a.attachTerminal(client, sid, termSpec{Server: server})
	if err != nil {
		a.fail(err)
		return
	}
	if a.terminalTab() == nil {
		return
	}
	a.addTerminalPane(tt)
}

func (a *app) killPickedSession(server, sid string) {
	if sid == "" {
		return
	}
	if server == "" {
		server = localServerID
	}
	client, err := a.terminalClientExisting(termSpec{Server: server})
	if err != nil {
		a.sessionErr = err.Error()
		a.invalidate()
		return
	}
	if err := killPickedSessionFn(client, sid); err != nil {
		a.sessionErr = err.Error()
		a.invalidate()
		return
	}
	a.refreshSessionPicker()
}

func (a *app) sessionPickerModal(c *ui.Context, k *colors) {
	if !a.sessionPick {
		return
	}
	q := strings.ToLower(strings.TrimSpace(a.sessionQuery))
	var filtered []session.SessionInfo
	for _, s := range a.sessionItems {
		if s.Exited {
			continue
		}
		hay := strings.ToLower(s.Title + " " + s.Shell + " " + s.Dir + " " + s.ID)
		if q == "" || fuzzy(hay, q) {
			filtered = append(filtered, s)
		}
	}
	a.sessionSel = min(max(a.sessionSel, 0), max(len(filtered)-1, 0))
	was := a.sessionPick
	attachSID := ""
	killSID := ""
	server := a.sessionServer
	if server == "" {
		server = localServerID
	}
	commandDialog(c, k, &a.sessionPick, func() {
		reveal := false
		placeholder := "Filter terminal sessions…"
		if server != localServerID {
			placeholder = "Filter sessions on " + server + "…"
		}
		in := sheetHeader(c, k, &a.sessionQuery, placeholder, "Attach Terminal Session", &a.sessionPick)
		if listNav(in, &a.sessionSel, len(filtered), &reveal) && len(filtered) > 0 {
			attachSID = filtered[a.sessionSel].ID
		}
		if a.sessionBusy {
			ui.Row(c).Height(sheetListHeight(c)).Center().Gap(8).Children(func() {
				ui.Spinner(c).Size(12, 12).Label("Loading sessions")
				ui.Text(c, "Reading sessions…").FontSize(rowDetail).TextColor(k.TextFaint)
			})
			return
		}
		if a.sessionErr != "" {
			ui.Column(c).Height(sheetListHeight(c)).Children(func() {
				emptyState(c, k, "square-terminal", k.Danger, "Sessions unavailable", a.sessionErr, nil)
			})
			return
		}
		if len(a.sessionItems) == 0 {
			ui.Column(c).Height(sheetListHeight(c)).Children(func() {
				emptyState(c, k, "square-terminal", k.TextMuted, "No running sessions",
					"Open a terminal to start one on this server.", nil)
			})
			return
		}
		a.sessionList.Selected = &a.sessionSel
		a.sessionList.Key = func(i int) any { return filtered[i].ID }
		list := ui.List(c, &a.sessionList, len(filtered), func(i int) {
			info := filtered[i]
			selected := i == a.sessionSel
			title := strings.TrimSpace(info.Title)
			if title == "" {
				title = info.Shell
			}
			if title == "" {
				title = "Terminal"
			}
			detailParts := []string{"#" + info.ID}
			if info.Dir != "" {
				dir := info.Dir
				if server == localServerID {
					dir = shortPath(dir)
				}
				detailParts = append(detailParts, dir)
			}
			if !info.Created.IsZero() {
				detailParts = append(detailParts, shortDuration(time.Since(info.Created)))
			}
			detail := strings.Join(detailParts, " · ")
			_, openPane := a.attachedSessionPane(server, info.ID)
			alreadyOpen := openPane != nil

			row := ui.Row(c.Key("sess:"+info.ID)).Height(rowH).Padding(0, rowPadX).Gap(rowGap).
				Radius(radius).AlignItems(ui.Center).Cursor(ui.CursorPointer).
				Label(title + " " + detail)
			fg, muted, icon := k.Text, k.TextFaint, k.TextMuted
			if selected {
				fg = c.Theme().AccentText
				muted, icon = fg.Alpha(0.9), fg.Alpha(0.9)
			} else if row.Hovered() {
				row.Background(k.Hover)
			}
			row.Children(func() {
				body := ui.Row(c).Grow(1).MinWidth(0).Gap(rowGap).AlignItems(ui.Center)
				if body.Clicked() {
					attachSID = info.ID
				}
				body.Children(func() {
					ui.Icon(c, icons.Icon("square-terminal")).Size(15, 15).TextColor(icon)
					ui.Text(c, title).FontSize(rowTitle).FontWeight(500).TextColor(fg).
						SingleLine().Ellipsis("…").Shrink(0.3).MinWidth(0)
					ui.Text(c, detail).FontSize(rowDetail).TextColor(muted).
						SingleLine().TruncateStart().Shrink(1).MinWidth(0)
					if alreadyOpen {
						badgeBg := k.Hover
						if selected {
							badgeBg = fg.Alpha(0.16)
						}
						ui.Text(c, "Open").FontSize(c.Theme().Rem(10.0/13)).FontWeight(600).
							TextColor(muted).Padding(2, 6).Radius(radius).Background(badgeBg).Shrink(0)
					}
				})
				killBtn := iconButton(c, k, "trash-2", fmt.Sprintf("Kill session %s", info.ID), iconBtnSm, iconGlyphSm).
					Tooltip("Kill Session")
				if killBtn.Clicked() {
					killSID = info.ID
				}
			})
		}).MaxHeight(sheetListHeight(c)).Padding(6)
		if len(filtered) == 0 {
			list.Children(func() { emptyLine(c, k, "No sessions match") })
		}
		if reveal {
			a.sessionList.ScrollIntoView(a.sessionSel)
		}
	})
	if killSID != "" {
		sid := killSID
		a.later(func() { a.killPickedSession(server, sid) })
	} else if attachSID != "" {
		a.sessionPick = false
		sid := attachSID
		a.later(func() { a.attachExistingSession(server, sid) })
	}
	if was && !a.sessionPick {
		if t := a.tab(); t != nil && a.focusReq == nil {
			if t.Focus != nil {
				a.focusReq = t.Focus
			} else {
				a.focusReq = t.editorPane()
			}
		}
	}
}

func shortDuration(d time.Duration) string {
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
	return fmt.Sprintf("%dh %dm", int(d.Hours()), int(d.Minutes())%60)
}
