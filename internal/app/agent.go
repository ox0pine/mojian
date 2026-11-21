package app

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"mojian/internal/acp"
	"mojian/internal/icons"
	"mojian/internal/motion"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// agentSession is the process cleanup a task needs. Tests can use a
// deterministic session without starting an agent process.
type agentSession interface {
	Cancel()
	Close()
}

// agentTask is the window's one background ACP task: a prompt run
// without a pane, whose only voice is a system notification.
type agentTask struct {
	prompt      string
	agent       string
	remote      bool
	currentTool string
	sess        agentSession
	cancel      context.CancelFunc
	perm        *acp.Permission
	perms       []*acp.Permission
	tools       map[string]string
	reply       strings.Builder
	started     time.Time
	notify      func(title, subtitle, body string) // tests replace the native notification
}

// openAgentTask asks for the task, or shows the permission a running
// task waits for.
func (a *app) openAgentTask() {
	if a.store == nil || a.store.IsMemory() {
		a.fail(errors.New("agent tasks need a project on disk"))
		return
	}
	if a.store.IsRemote() && a.store.Client() == nil {
		a.fail(errors.New("remote host is not connected"))
		return
	}
	a.closeModals()
	a.agentOpen = true
	a.agentInput = ""
	a.refreshAgentChoices()
	a.invalidate()
}

// refreshAgentChoices discovers available ACP agents on this machine or on
// the active remote host so the sheet can offer explicit selection.
func (a *app) refreshAgentChoices() {
	if a.store == nil || a.store.IsMemory() {
		return
	}
	a.agentChoiceSeq++
	seq := a.agentChoiceSeq
	winSeq := a.windowSeq
	remote := a.store.IsRemote()
	client := a.store.Client()
	apply := func(names []string) {
		if a.windowSeq != winSeq || a.agentChoiceSeq != seq {
			return
		}
		a.agentChoices = names
		if a.agentSelected != "" {
			found := false
			for _, n := range names {
				if n == a.agentSelected {
					found = true
					break
				}
			}
			if !found {
				a.agentSelected = ""
			}
		}
		a.invalidate()
	}
	post := a.uiPoster()
	if post == nil {
		if !remote {
			avail := acp.Available()
			names := make([]string, 0, len(avail))
			for _, ag := range avail {
				names = append(names, ag.Name)
			}
			apply(names)
		}
		return
	}
	go func() {
		var names []string
		if remote && client != nil {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			names, _ = client.ACPAgentsContext(ctx)
			cancel()
		} else if !remote {
			avail := acp.Available()
			names = make([]string, 0, len(avail))
			for _, ag := range avail {
				names = append(names, ag.Name)
			}
		}
		post(func() { apply(names) })
	}()
}

// startAgentTask runs prompt in the background and closes the sheet.
func (a *app) startAgentTask(prompt string) {
	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		return
	}
	if a.agentTask != nil {
		a.fail(errors.New("an agent task is already running"))
		return
	}
	post := a.uiPoster()
	if post == nil {
		a.fail(errors.New("agent tasks need a window"))
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	remote := a.store != nil && a.store.IsRemote()
	client := a.store.Client()
	selected := a.agentSelected
	t := &agentTask{
		prompt:  prompt,
		agent:   selected,
		remote:  remote,
		cancel:  cancel,
		tools:   map[string]string{},
		started: time.Now(),
	}
	a.agentTask = t
	a.agentOpen = false
	dir := a.store.Dir
	go func() {
		if remote {
			if client == nil {
				post(func() {
					if a.agentTask != t {
						return
					}
					a.agentTask = nil
					a.notifyAgentTask(t, "Agent could not start", "", "remote host is not connected")
					a.invalidate()
				})
				return
			}
			s, err := client.StartACP(ctx, dir, selected, prompt, func(ev acp.Event) {
				post(func() { a.agentTaskEvent(t, ev) })
			})
			if err != nil {
				post(func() {
					if a.agentTask != t {
						return
					}
					a.agentTask = nil
					a.notifyAgentTask(t, "Agent could not start", "", err.Error())
					a.invalidate()
				})
				return
			}
			if ctx.Err() != nil {
				s.Close()
				return
			}
			post(func() {
				if a.agentTask != t {
					s.Close()
					return
				}
				t.sess = s
			})
			return
		}
		s, _, err := acp.StartAgent(ctx, dir, selected, func(ev acp.Event) {
			post(func() { a.agentTaskEvent(t, ev) })
		})
		if err != nil {
			post(func() {
				if a.agentTask != t {
					return
				}
				a.agentTask = nil
				a.notifyAgentTask(t, "Agent could not start", "", err.Error())
				a.invalidate()
			})
			return
		}
		if ctx.Err() != nil {
			s.Close()
			return
		}
		post(func() {
			if a.agentTask != t {
				s.Close()
				return
			}
			t.sess = s
		})
		if err := s.Prompt(ctx, prompt); err != nil {
			post(func() { a.failAgentTask(t, err) })
		}
	}()
	a.invalidate()
}

// uiPoster captures the way background work reaches the UI thread; nil
// means this app has no thread to hand it to.
func (a *app) uiPoster() func(func()) {
	if a.uiPost != nil {
		return a.uiPost
	}
	if a.win != nil {
		win := a.win
		return func(fn func()) { win.Update(fn) }
	}
	return nil
}

// agentTaskEvent applies one event of the running task on the UI thread.
func (a *app) agentTaskEvent(t *agentTask, ev acp.Event) {
	if a.agentTask != t {
		return
	}
	switch ev.Kind {
	case acp.KindReady:
		t.agent = ev.Agent
	case acp.KindAgentText:
		if left := 8000 - t.reply.Len(); left > 0 {
			if len(ev.Text) > left {
				t.reply.WriteString(ev.Text[:left])
			} else {
				t.reply.WriteString(ev.Text)
			}
		}
	case acp.KindTool:
		if ev.Tool != nil {
			if ev.Tool.Title != "" && ev.Tool.ID != "" {
				if t.tools == nil {
					t.tools = map[string]string{}
				}
				t.tools[ev.Tool.ID] = ev.Tool.Title
			}
			if title := ev.Tool.Title; title != "" {
				t.currentTool = title
			} else if saved := t.tools[ev.Tool.ID]; saved != "" {
				t.currentTool = saved
			}
			if ev.Tool.Status == acp.StatusFailed {
				title := ev.Tool.Title
				if title == "" {
					title = t.tools[ev.Tool.ID]
				}
				a.notifyAgentTask(t, "Agent tool failed", t.agentSub(false), title)
			}
		}
	case acp.KindPermission:
		if ev.Permission == nil {
			return
		}
		if t.perm == nil {
			t.perm = ev.Permission
			a.agentOpen = true
		} else {
			t.perms = append(t.perms, ev.Permission)
		}
		a.notifyAgentTask(t, "Agent needs permission", t.agentSub(false), ev.Permission.Title)
	case acp.KindPermissionResolved:
		a.resolveAgentPermission(t, ev.Permission)
	case acp.KindTurnDone:
		title, body := agentTurnNotification(ev.Stop, t.reply.String(), t.prompt)
		a.agentLastAgent = t.agent
		a.agentLastSummary = body
		a.agentLastDuration = shortDuration(time.Since(t.started))
		a.notifyAgentTask(t, title, t.agentSub(true), body)
		a.finishAgentTask(t)
	case acp.KindFailed:
		a.failAgentTask(t, ev.Err)
	case acp.KindClosed:
		// Prompt owns the turn result. Its response or error is posted even
		// when process reaping wins the race, so Closed must not erase it.
	}
	a.invalidate()
}

// failAgentTask reports the prompt error after any concurrent Closed event.
func (a *app) failAgentTask(t *agentTask, err error) {
	if a.agentTask != t {
		return
	}
	msg := "agent failed"
	if err != nil {
		msg = err.Error()
	}
	a.notifyAgentTask(t, "Agent error", t.agentSub(false), msg)
	a.finishAgentTask(t)
}

// resolveAgentPermission removes exactly the request the agent resolved,
// leaving any newer request untouched.
func (a *app) resolveAgentPermission(t *agentTask, p *acp.Permission) {
	if p == nil {
		return
	}
	if t.perm == p {
		t.perm = nil
		a.nextAgentPermission(t)
		return
	}
	for i, pending := range t.perms {
		if pending == p {
			t.perms = append(t.perms[:i], t.perms[i+1:]...)
			return
		}
	}
}

// nextAgentPermission shows the oldest queued request, or closes the
// permission sheet when none remain.
func (a *app) nextAgentPermission(t *agentTask) {
	if len(t.perms) != 0 {
		t.perm = t.perms[0]
		t.perms = t.perms[1:]
		a.agentOpen = true
		return
	}
	a.agentOpen = false
}

// finishAgentTask drops the task and stops its agent.
func (a *app) finishAgentTask(t *agentTask) {
	if a.agentTask != t {
		return
	}
	a.agentTask = nil
	a.agentOpen = false
	if t.cancel != nil {
		t.cancel()
		t.cancel = nil
	}
	cancelAgentPermissions(t)
	if t.sess != nil {
		t.sess.Close()
		t.sess = nil
	}
	a.refreshProject()
	a.invalidate()
}

// stopAgentTask cancels the running task from the sheet.
func (a *app) stopAgentTask() {
	t := a.agentTask
	if t == nil {
		return
	}
	if t.cancel != nil {
		t.cancel()
		t.cancel = nil
	}
	cancelAgentPermissions(t)
	a.agentTask = nil
	a.agentOpen = false
	if t.sess != nil {
		t.sess.Cancel()
		t.sess.Close()
	}
	a.refreshProject()
	a.invalidate()
}

// cancelAgentPermissions releases every blocked ACP handler, including
// requests waiting behind the one visible in the sheet.
func cancelAgentPermissions(t *agentTask) {
	if t.perm != nil {
		t.perm.Answer("")
	}
	for _, p := range t.perms {
		p.Answer("")
	}
	t.perm = nil
	t.perms = nil
}

// answerAgentTask answers the permission the task waits for.
func (a *app) answerAgentTask(option string) {
	t := a.agentTask
	if t == nil || t.perm == nil {
		return
	}
	t.perm.Answer(option)
	t.perm = nil
	a.nextAgentPermission(t)
	a.invalidate()
}

// notifyAgentTask is the task's only result: the window has no pane to
// print into. The subtitle names the agent, and the time it took once
// the task ended, so a notification says who ran and for how long.
func (a *app) notifyAgentTask(t *agentTask, title, subtitle, body string) {
	if t != nil && t.notify != nil {
		t.notify(title, subtitle, body)
		return
	}
	if a.harness || a.quitting {
		return
	}
	mygo.NewNotification(mygo.NotificationOptions{
		Title: title, Subtitle: subtitle, Body: body,
	}).Show()
}

// agentSub names the agent in a notification's subtitle.
func (t *agentTask) agentSub(done bool) string {
	name := t.agent
	if name == "" {
		return ""
	}
	if !done {
		return name
	}
	return name + " · " + shortDuration(time.Since(t.started))
}

// shortDuration is a task's running time as a notification says it.
func shortDuration(d time.Duration) string {
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
	return fmt.Sprintf("%dh %dm", int(d.Hours()), int(d.Minutes())%60)
}

// agentTurnNotification distinguishes a completed answer from refusal,
// truncation and cancellation. A non-success stop never inherits the
// successful "Finished" fallback.
func agentTurnNotification(stop, reply, prompt string) (title, body string) {
	if stop == "" || stop == "end_turn" {
		return "Agent finished", agentNotificationBody(reply, prompt)
	}
	var fallback string
	switch stop {
	case "refusal":
		title = "Agent refused"
		fallback = "The agent refused the request."
	case "max_tokens":
		title = "Agent incomplete"
		fallback = "The agent reached its token limit."
	case "max_turn_requests":
		title = "Agent incomplete"
		fallback = "The agent reached its turn-request limit."
	case "cancelled":
		title = "Agent stopped"
		fallback = "Stopped: " + prompt
	default:
		title = "Agent stopped"
		fallback = "Stopped (" + stop + "): " + prompt
	}
	if strings.TrimSpace(reply) != "" {
		body = agentNotificationBody(reply, prompt)
	} else {
		body = agentNotificationText(fallback)
	}
	return title, body
}

// agentNotificationBody condenses an answer for a notification.
func agentNotificationBody(reply, prompt string) string {
	text := strings.Join(strings.Fields(reply), " ")
	if text == "" {
		text = "Finished: " + strings.Join(strings.Fields(prompt), " ")
	}
	return agentNotificationText(text)
}

func agentNotificationText(text string) string {
	text = strings.Join(strings.Fields(text), " ")
	if r := []rune(text); len(r) > 160 {
		text = string(r[:159]) + "…"
	}
	return text
}

// agentSheet is the one dialog of a background task: its prompt before
// it runs, its permission while it waits.
func (a *app) agentSheet(c *ui.Context, k *colors) {
	if !a.agentOpen {
		return
	}
	was := a.agentOpen
	submit := false
	commandDialog(c, k, &a.agentOpen, func() {
		ui.Row(c).Height(48).Padding(0, 16).Gap(10).AlignItems(ui.Center).
			BorderWidth(0, 0, 1, 0).BorderColor(k.PanelBorder).Children(func() {
			ui.Icon(c, icons.Icon("bot")).Size(17, 17).TextColor(k.TextMuted)
			t := a.agentTask
			switch {
			case t != nil && t.perm != nil:
				ui.Text(c, "Permission").FontSize(c.Theme().Rem(15.0 / 13)).FontWeight(600).TextColor(k.Text).Grow(1).MinWidth(0)
			case t != nil:
				ui.Text(c, "Agent task").FontSize(c.Theme().Rem(15.0 / 13)).FontWeight(600).TextColor(k.Text).Grow(1).MinWidth(0)
			default:
				in := ui.TextInputBase(c, &a.agentInput).Grow(1).FontSize(c.Theme().Rem(15.0 / 13)).
					Placeholder("What should the agent do in this project?").
					Label("Agent task").AutoFocus()
				submit = in.Submitted()
			}
			if iconButton(c, k, "x", "Close", iconBtnSm, iconGlyphSm).Clicked() {
				a.agentOpen = false
			}
		})
		if t := a.agentTask; t != nil {
			if t.perm != nil {
				a.agentPermission(c, k, t)
				return
			}
			ui.Column(c).Padding(12, 16).Gap(10).Children(func() {
				ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
					ui.Spinner(c).Size(12, 12).Label("Agent task running")
					ui.Text(c, "Running "+t.prompt).FontSize(c.Theme().Rem(12.5 / 13)).TextColor(k.TextMuted).
						SingleLine().Ellipsis("…").Shrink(1).MinWidth(0)
				})
				meta := t.agentSub(true)
				if t.currentTool != "" {
					if meta != "" {
						meta += " · "
					}
					meta += t.currentTool
				}
				if meta != "" {
					ui.Text(c, meta).FontSize(c.Theme().Rem(11.5 / 13)).TextColor(k.TextFaint).
						SingleLine().Ellipsis("…")
				}
				if ui.Button(c, "Stop").Clicked() {
					a.later(func() { a.stopAgentTask() })
				}
			})
			return
		}
		ui.Column(c).Padding(12, 16).Gap(10).Children(func() {
			if len(a.agentChoices) > 0 {
				ui.Row(c).Gap(6).Wrap().AlignItems(ui.Center).Children(func() {
					if agentChoicePill(c, k, "Auto", a.agentSelected == "").Clicked() {
						a.agentSelected = ""
					}
					for _, name := range a.agentChoices {
						name := name
						if agentChoicePill(c, k, name, a.agentSelected == name).Clicked() {
							a.agentSelected = name
						}
					}
				})
			}
			ui.Text(c, "Runs an ACP agent — omp, Claude Code, Codex, Gemini CLI, Goose and others — in the background (locally or on the open SSH host), and notifies you when it finishes, fails or asks for permission.").FontSize(c.Theme().Rem(12.0 / 13)).TextColor(k.TextFaint)
			ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
				if ui.PrimaryButton(c, "Run").Clicked() {
					submit = true
				}
				if a.agentLastSummary != "" && a.store != nil && !a.store.IsMemory() {
					if ui.Button(c, "Review Diff").Clicked() {
						a.agentOpen = false
						a.later(func() {
							if !a.git.Open {
								a.toggleGit()
							} else {
								a.refreshGit()
							}
						})
					}
				}
			})
			if a.agentLastSummary != "" {
				label := "Last result"
				if a.agentLastAgent != "" {
					label = a.agentLastAgent
				}
				if a.agentLastDuration != "" {
					label += " · " + a.agentLastDuration
				}
				ui.Column(c).Padding(8, 10).Gap(2).Radius(radius).Background(k.Hover).Children(func() {
					ui.Text(c, label).FontSize(c.Theme().Rem(11.0 / 13)).FontWeight(600).TextColor(k.TextMuted)
					ui.Text(c, a.agentLastSummary).FontSize(c.Theme().Rem(12.0 / 13)).TextColor(k.TextFaint)
				})
			}
		})
	})
	if was && !a.agentOpen {
		if t := a.tab(); t != nil && a.focusReq == nil {
			if t.Focus != nil {
				a.focusReq = t.Focus
			} else {
				a.focusReq = t.editorPane()
			}
		}
	}
	if submit {
		a.startAgentTask(a.agentInput)
	}
}

// agentPermission shows the options the agent asked the user to choose.
func (a *app) agentPermission(c *ui.Context, k *colors, t *agentTask) {
	ui.Column(c).Padding(12, 16).Gap(10).Children(func() {
		title := t.perm.Title
		if title == "" {
			title = "The agent needs permission to continue."
		}
		ui.Text(c, title).FontSize(c.Theme().Rem(12.5 / 13)).TextColor(k.Text)
		ui.Row(c).Gap(8).Wrap().Children(func() {
			hasReject := false
			for _, o := range t.perm.Options {
				if strings.EqualFold(strings.TrimSpace(o.Name), "Reject") {
					hasReject = true
				}
				option := o.ID
				if ui.Button(c, o.Name).Clicked() {
					a.later(func() { a.answerAgentTask(option) })
				}
			}
			if !hasReject {
				if ui.Button(c, "Reject").Clicked() {
					a.later(func() { a.answerAgentTask("") })
				}
			}
		})
	})
}

// agentStatusChip is the quiet title-bar indicator shown only while a
// background ACP task is running or waiting for permission.
func (a *app) agentStatusChip(c *ui.Context, k *colors) {
	t := a.agentTask
	if t == nil {
		return
	}
	label := "Agent"
	if t.perm != nil {
		label = "Permission"
	} else if t.agent != "" {
		label = strings.Fields(t.agent)[0]
	}
	tip := t.prompt
	if t.perm != nil && t.perm.Title != "" {
		tip = "Permission needed: " + t.perm.Title
	} else if t.currentTool != "" {
		tip = t.prompt + " — " + t.currentTool
	}
	chip := ui.ButtonBase(c).Height(tabH).Padding(0, 9).Gap(6).Radius(radius - 2).
		AlignItems(ui.Center).Cursor(ui.CursorPointer).Shrink(0).
		Label("Agent task: " + label).Tooltip(tip).Transition(motion.Colors)
	if chip.Hovered() {
		chip.Background(k.Pressed)
	} else {
		chip.Background(k.Hover)
	}
	if chip.Clicked() {
		a.later(func() { a.openAgentTask() })
	}
	chip.Children(func() {
		if t.perm != nil {
			ui.Box(c).Size(dotSize, dotSize).Radius(dotSize / 2).Background(k.Attention)
		} else {
			ui.Spinner(c).Size(12, 12).Label("Agent running")
		}
		ui.Text(c, label).FontSize(c.Theme().Rem(12.0 / 13)).FontWeight(500).TextColor(k.Text)
	})
}

func agentChoicePill(c *ui.Context, k *colors, name string, active bool) ui.Element {
	b := ui.ButtonBase(c).Height(24).Padding(0, 9).Radius(radius - 2).
		Cursor(ui.CursorPointer).Label("Agent " + name).Transition(motion.Colors)
	fg := k.TextMuted
	switch {
	case active:
		b.Background(c.Theme().Accent)
		fg = c.Theme().AccentText
	case b.Hovered():
		b.Background(k.Pressed)
		fg = k.Text
	default:
		b.Background(k.Hover)
	}
	b.Children(func() {
		ui.Text(c, name).FontSize(c.Theme().Rem(11.5 / 13)).FontWeight(500).TextColor(fg)
	})
	return b
}
