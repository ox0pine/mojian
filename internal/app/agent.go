package app

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"mojian/internal/acp"
	"mojian/internal/icons"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// agentTask is the window's one background ACP task: a prompt run
// without a pane, whose only voice is a system notification.
type agentTask struct {
	prompt  string
	agent   string
	sess    *acp.Session
	perm    *acp.Permission
	reply   strings.Builder
	started time.Time
}

// openAgentTask asks for the task, or shows the permission a running
// task waits for.
func (a *app) openAgentTask() {
	if a.store == nil || a.store.IsMemory() {
		a.fail(errors.New("agent tasks need a project on disk"))
		return
	}
	if a.remotePath != "" {
		a.fail(errors.New("agent tasks need a local project"))
		return
	}
	a.closeModals()
	a.agentOpen = true
	a.agentInput = ""
	a.invalidate()
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
	t := &agentTask{prompt: prompt, started: time.Now()}
	a.agentTask = t
	a.agentOpen = false
	dir := a.store.Dir
	go func() {
		s, _, err := acp.StartAny(context.Background(), dir, func(ev acp.Event) {
			post(func() { a.agentTaskEvent(t, ev) })
		})
		if err != nil {
			post(func() {
				if a.agentTask != t {
					return
				}
				a.agentTask = nil
				a.notifyAgentTask("Agent could not start", "", err.Error())
				a.invalidate()
			})
			return
		}
		post(func() {
			if a.agentTask != t {
				s.Close()
				return
			}
			t.sess = s
		})
		if err := s.Prompt(context.Background(), prompt); err != nil {
			post(func() {
				if a.agentTask == t {
					a.notifyAgentTask("Agent error", t.agentSub(false), err.Error())
					a.finishAgentTask(t)
				}
			})
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
		if t.reply.Len() > 8000 {
			t.reply.Reset()
		}
		t.reply.WriteString(ev.Text)
	case acp.KindTool:
		if ev.Tool != nil && ev.Tool.Status == acp.StatusFailed {
			a.notifyAgentTask("Agent tool failed", t.agentSub(false), ev.Tool.Title)
		}
	case acp.KindPermission:
		t.perm = ev.Permission
		a.agentOpen = true
		a.notifyAgentTask("Agent needs permission", t.agentSub(false), ev.Permission.Title)
	case acp.KindTurnDone:
		body := agentNotificationBody(t.reply.String(), t.prompt)
		a.notifyAgentTask("Agent finished", t.agentSub(true), body)
		a.finishAgentTask(t)
	case acp.KindFailed:
		a.notifyAgentTask("Agent error", t.agentSub(false), ev.Err.Error())
		a.finishAgentTask(t)
	case acp.KindClosed:
		if t.sess != nil {
			a.notifyAgentTask("Agent stopped", t.agentSub(false), t.prompt)
			a.finishAgentTask(t)
		}
	}
	a.invalidate()
}

// finishAgentTask drops the task and stops its agent.
func (a *app) finishAgentTask(t *agentTask) {
	if a.agentTask != t {
		return
	}
	a.agentTask = nil
	a.agentOpen = false
	if t.perm != nil {
		t.perm.Answer("")
		t.perm = nil
	}
	if t.sess != nil {
		t.sess.Close()
		t.sess = nil
	}
	a.invalidate()
}

// stopAgentTask cancels the running task from the sheet.
func (a *app) stopAgentTask() {
	t := a.agentTask
	if t == nil {
		return
	}
	if t.perm != nil {
		t.perm.Answer("")
		t.perm = nil
	}
	a.agentTask = nil
	a.agentOpen = false
	if t.sess != nil {
		t.sess.Cancel()
		t.sess.Close()
	}
	a.invalidate()
}

// answerAgentTask answers the permission the task waits for.
func (a *app) answerAgentTask(option string) {
	t := a.agentTask
	if t == nil || t.perm == nil {
		return
	}
	t.perm.Answer(option)
	t.perm = nil
	a.invalidate()
}

// notifyAgentTask is the task's only result: the window has no pane to
// print into. The subtitle names the agent, and the time it took once
// the task ended, so a notification says who ran and for how long.
func (a *app) notifyAgentTask(title, subtitle, body string) {
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

// agentNotificationBody condenses an answer for a notification.
func agentNotificationBody(reply, prompt string) string {
	text := strings.Join(strings.Fields(reply), " ")
	if text == "" {
		text = "Finished: " + strings.Join(strings.Fields(prompt), " ")
	}
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
			if t != nil && t.perm != nil {
				ui.Text(c, "Permission").FontSize(15).FontWeight(600).TextColor(k.Text).Grow(1).MinWidth(0)
			} else {
				in := ui.TextInputBase(c, &a.agentInput).Grow(1).FontSize(15).
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
			ui.Column(c).Padding(10).Gap(8).Children(func() {
				ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
					ui.Spinner(c).Size(12, 12).Label("Agent task running")
					ui.Text(c, "Running "+t.prompt).FontSize(12.5).TextColor(k.TextMuted).
						SingleLine().Ellipsis("…").Shrink(1).MinWidth(0)
				})
				if ui.Button(c, "Stop").Clicked() {
					a.later(func() { a.stopAgentTask() })
				}
			})
			return
		}
		ui.Column(c).Padding(10).Gap(10).Children(func() {
			ui.Text(c, "Runs the first ACP agent it finds — omp, Claude Code, Codex, Gemini CLI, Goose and others — in the background, and notifies you when it finishes, fails or asks for permission.").
				FontSize(12).TextColor(k.TextFaint)
			if ui.PrimaryButton(c, "Run").Clicked() {
				submit = true
			}
		})
	})
	if was && !a.agentOpen {
		if t := a.tab(); t != nil && a.focusReq == nil {
			a.focusReq = t.editorPane()
		}
	}
	if submit {
		a.startAgentTask(a.agentInput)
	}
}

// agentPermission shows the options the agent asked the user to choose.
func (a *app) agentPermission(c *ui.Context, k *colors, t *agentTask) {
	ui.Column(c).Padding(10).Gap(10).Children(func() {
		ui.Text(c, t.perm.Title).FontSize(12.5).TextColor(k.Text)
		ui.Row(c).Gap(8).Wrap().Children(func() {
			for _, o := range t.perm.Options {
				option := o.ID
				if ui.Button(c, o.Name).Clicked() {
					a.later(func() { a.answerAgentTask(option) })
				}
			}
			if ui.Button(c, "Reject").Clicked() {
				a.later(func() { a.answerAgentTask("") })
			}
		})
	})
}
