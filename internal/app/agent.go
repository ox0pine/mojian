package app

import (
	"context"
	"errors"
	"math"
	"strings"

	"mojian/internal/acp"
	"mojian/internal/icons"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// agentPane is what an agent pane has: the running ACP session, the
// transcript it streamed, and the question waiting for the user.
type agentPane struct {
	status agentStatus
	agent  string // the name and version the agent reported
	err    string
	gen    int // invalidates work whose pane was closed
	sess   *acp.Session

	input  string
	scroll ui.ScrollState
	items  []*agentItem
	tools  map[string]*agentItem
	perm   *acp.Permission
}

// agentStatus is where an agent pane is in its life.
type agentStatus int

const (
	agentStarting agentStatus = iota
	agentReady
	agentRunning
	agentDown
)

// agentItemKind is one line of the transcript.
type agentItemKind int

const (
	itemUser agentItemKind = iota
	itemAgent
	itemThought
	itemTool
	itemError
)

// agentItem is one message or tool call of a transcript. A tool call is
// updated in place by the ID the agent sends.
type agentItem struct {
	kind agentItemKind
	text string
	tool *acp.Tool
}

// newAgent adds an agent pane to the right of the active tab and starts
// an agent in the project's directory.
func (a *app) newAgent() {
	if a.store == nil || a.store.IsMemory() {
		a.fail(errors.New("agent panes need a project on disk"))
		return
	}
	if a.remotePath != "" {
		a.fail(errors.New("agent panes need a local project"))
		return
	}
	t := a.terminalTab()
	if t == nil {
		return
	}
	p := &pane{ID: nextID("p"), Agent: &agentPane{status: agentStarting, tools: map[string]*agentItem{}}}
	p.enterSide(splitRight)
	a.unzoom(t)
	if t.Root == nil {
		t.Root = &node{Pane: p}
	} else {
		t.Root = &node{Vertical: false, Share: 50, A: t.Root, B: &node{Pane: p}}
	}
	t.Focus = p
	a.git.Open = false
	a.focusReq = p
	a.changed()
	a.invalidate()
	a.startAgent(p)
}

// startAgent runs the agent and hands its events to the window thread.
func (a *app) startAgent(p *pane) {
	ag := p.Agent
	if ag == nil {
		return
	}
	ag.status = agentStarting
	ag.err = ""
	ag.gen++
	gen := ag.gen
	dir := a.store.Dir
	win := a.win
	command := strings.Fields(a.AgentCommand)
	if len(command) == 0 {
		command = []string{"omp", "acp"}
	}
	if win == nil {
		ag.status = agentDown
		ag.err = "agent panes need a window"
		return
	}
	post := func(fn func()) {
		win.Update(fn)
	}
	go func() {
		s, err := acp.Start(context.Background(), acp.Options{
			Dir:     dir,
			Command: command,
			Events: func(ev acp.Event) {
				post(func() { a.agentEvent(p, gen, ev) })
			},
		})
		if err != nil {
			post(func() {
				if p.Agent == nil || p.Agent.gen != gen {
					return
				}
				p.Agent.status = agentDown
				p.Agent.err = err.Error()
				a.invalidate()
			})
			return
		}
		post(func() {
			if p.Agent == nil || p.Agent.gen != gen {
				s.Close()
				return
			}
			p.Agent.sess = s
			a.invalidate()
		})
	}()
}

// agentEvent applies one event on the window thread.
func (a *app) agentEvent(p *pane, gen int, ev acp.Event) {
	ag := p.Agent
	if ag == nil || ag.gen != gen {
		return
	}
	switch ev.Kind {
	case acp.KindReady:
		ag.agent = ev.Agent
		ag.status = agentReady
	case acp.KindAgentText:
		ag.appendText(itemAgent, ev.Text)
	case acp.KindThought:
		ag.appendText(itemThought, ev.Text)
	case acp.KindTool:
		ag.applyTool(ev.Tool)
		if ev.Tool != nil && ev.Tool.Status == acp.StatusFailed {
			a.notifyAgent("Agent tool failed", ev.Tool.Title)
		}
	case acp.KindPermission:
		ag.perm = ev.Permission
		a.notifyAgent("Agent needs permission", ev.Permission.Title)
	case acp.KindTurnDone:
		ag.status = agentReady
		if ev.Stop != "cancelled" {
			a.notifyAgent("Agent finished", a.projectName()+" · "+ev.Stop)
		}
	case acp.KindFailed:
		ag.status = agentReady
		ag.appendText(itemError, ev.Err.Error())
		a.notifyAgent("Agent error", ev.Err.Error())
	case acp.KindClosed:
		if ag.sess == nil && ag.status != agentDown {
			return
		}
		ag.status = agentDown
		if ev.Err != nil {
			ag.appendText(itemError, ev.Err.Error())
		}
	}
	a.followAgent(ag)
	a.invalidate()
}

// followAgent keeps the transcript at its end, unless the user scrolled
// away from it.
func (a *app) followAgent(ag *agentPane) {
	if ag.scroll.MaxY-ag.scroll.Y > 80 {
		return
	}
	ag.scroll.Y = math.MaxFloat32
}

func (ag *agentPane) appendText(kind agentItemKind, text string) {
	if text == "" {
		return
	}
	if n := len(ag.items); n > 0 && ag.items[n-1].kind == kind && kind != itemTool {
		ag.items[n-1].text += text
		return
	}
	ag.items = append(ag.items, &agentItem{kind: kind, text: text})
}

// applyTool adds a tool call or changes the one already in the rows.
func (ag *agentPane) applyTool(t *acp.Tool) {
	if t == nil || t.ID == "" {
		return
	}
	it := ag.tools[t.ID]
	if it == nil {
		it = &agentItem{kind: itemTool, tool: &acp.Tool{ID: t.ID}}
		ag.tools[t.ID] = it
		ag.items = append(ag.items, it)
	}
	if t.Title != "" {
		it.tool.Title = t.Title
	}
	if t.Kind != "" {
		it.tool.Kind = t.Kind
	}
	if t.Status != "" {
		it.tool.Status = t.Status
	}
}

// sendAgent starts a turn with the composer's text.
func (a *app) sendAgent(p *pane) {
	ag := p.Agent
	if ag == nil || ag.sess == nil || ag.status != agentReady {
		return
	}
	text := strings.TrimSpace(ag.input)
	if text == "" {
		return
	}
	ag.input = ""
	ag.appendText(itemUser, text)
	ag.status = agentRunning
	gen := ag.gen
	sess := ag.sess
	win := a.win
	if win == nil {
		return
	}
	go func() {
		if err := sess.Prompt(context.Background(), text); err != nil {
			win.Update(func() { a.agentEvent(p, gen, acp.Event{Kind: acp.KindFailed, Err: err}) })
		}
	}()
	a.followAgent(ag)
	a.invalidate()
}

// stopAgent cancels the running turn, answering a pending permission
// request with a cancel.
func (a *app) stopAgent(p *pane) {
	ag := p.Agent
	if ag == nil {
		return
	}
	if ag.perm != nil {
		ag.perm.Answer("")
		ag.perm = nil
	}
	if ag.sess != nil {
		ag.sess.Cancel()
	}
}

// answerAgent answers the pending permission request.
func (a *app) answerAgent(p *pane, option string) {
	ag := p.Agent
	if ag == nil || ag.perm == nil {
		return
	}
	ag.perm.Answer(option)
	ag.perm = nil
	a.invalidate()
}

// endAgent stops the agent when its pane goes away.
func (a *app) endAgent(p *pane) {
	if p == nil || p.Agent == nil {
		return
	}
	p.Agent.gen++
	if p.Agent.perm != nil {
		p.Agent.perm.Answer("")
		p.Agent.perm = nil
	}
	if p.Agent.sess != nil {
		p.Agent.sess.Close()
		p.Agent.sess = nil
	}
}

// notifyAgent shows a system notification when the window is not the one
// the user is looking at.
func (a *app) notifyAgent(title, body string) {
	if a.harness || a.quitting || !a.NotifyAgent {
		return
	}
	if a.win != nil && a.win.IsFocused() {
		return
	}
	mygo.NewNotification(mygo.NotificationOptions{Title: title, Body: body}).Show()
}

// agentBody draws the transcript and the composer.
func (a *app) agentBody(c *ui.Context, k *colors, t *tab, p *pane) {
	ag := p.Agent
	if ag == nil {
		return
	}
	ui.Column(c).Fill().MinWidth(0).MinHeight(0).Children(func() {
		scroll := ui.Scroll(c).Grow(1).MinHeight(0).TrackScroll(&ag.scroll)
		scroll.Children(func() {
			ui.Column(c).Padding(14, 16, 8, 16).Gap(10).Children(func() {
				switch ag.status {
				case agentStarting:
					ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
						ui.Spinner(c).Size(12, 12).Label("Starting the agent")
						ui.Text(c, "Starting the agent…").FontSize(12).TextColor(k.TextFaint)
					})
				case agentDown:
					ui.Column(c).Gap(6).Children(func() {
						ui.Text(c, "The agent is not running").FontSize(13).FontWeight(600).TextColor(k.Text)
						if ag.err != "" {
							ui.Text(c, ag.err).FontSize(11.5).TextColor(k.Danger)
						}
						ui.Text(c, "It runs "+a.agentCommandLabel()+", an ACP agent, in the project folder.").
							FontSize(11.5).TextColor(k.TextFaint)
						if ui.Button(c, "Start Again").Clicked() {
							a.later(func() { a.startAgent(p) })
						}
					})
				}
				if len(ag.items) == 0 && ag.status == agentReady {
					a.emptyAgent(c, k, ag)
				}
				for _, it := range ag.items {
					a.agentItemView(c, k, it)
				}
				if ag.perm != nil {
					a.agentPermission(c, k, p, ag.perm)
				}
			})
		})
		ui.Row(c).Height(52).Padding(0, 12).Gap(8).AlignItems(ui.Center).
			BorderWidth(1, 0, 0, 0).BorderColor(k.CardBorder).Children(func() {
			in := ui.TextInputBase(c, &ag.input).Grow(1).FontSize(12.5).
				Placeholder("Ask the agent…").Label("Agent message")
			if t.Focus == p && a.focusReq == p {
				in.Focus()
				a.focusReq = nil
			}
			submit := in.Submitted()
			if ag.status == agentRunning {
				if ui.Button(c, "Stop").Clicked() {
					a.later(func() { a.stopAgent(p) })
				}
			} else if ag.status == agentReady && (submit || ui.PrimaryButton(c, "Send").Clicked()) {
				a.later(func() { a.sendAgent(p) })
			}
		})
	})
}

// emptyAgent is the first line of a fresh pane.
func (a *app) emptyAgent(c *ui.Context, k *colors, ag *agentPane) {
	name := ag.agent
	if name == "" {
		name = "the agent"
	}
	ui.Text(c, "Ask "+name+" about this project.").FontSize(12.5).TextColor(k.TextFaint)
}

// agentItemView draws one message, thought, tool call or error.
func (a *app) agentItemView(c *ui.Context, k *colors, it *agentItem) {
	switch it.kind {
	case itemUser:
		ui.Column(c).Gap(2).Children(func() {
			ui.Text(c, "You").FontSize(10.5).FontWeight(600).TextColor(k.TextFaint)
			ui.Text(c, it.text).FontSize(12.5).TextColor(k.Text)
		})
	case itemAgent:
		ui.Text(c, it.text).FontSize(12.5).TextColor(k.Text)
	case itemThought:
		ui.Column(c).Gap(2).Children(func() {
			ui.Text(c, "Thinking").FontSize(10.5).FontWeight(600).TextColor(k.TextFaint)
			ui.Text(c, it.text).FontSize(11.5).Italic().TextColor(k.TextFaint)
		})
	case itemError:
		ui.Text(c, it.text).FontSize(11.5).TextColor(k.Danger)
	case itemTool:
		if it.tool == nil {
			return
		}
		icon := agentToolIcon(it.tool.Kind)
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Icon(c, icon).Size(13, 13).TextColor(k.TextMuted)
			ui.Text(c, it.tool.Title).FontSize(12).TextColor(k.TextMuted).
				SingleLine().Ellipsis("…").Shrink(1).MinWidth(0)
			switch it.tool.Status {
			case acp.StatusFailed:
				ui.Icon(c, icons.Icon("x")).Size(12, 12).TextColor(k.Danger)
			case acp.StatusCompleted:
				ui.Icon(c, icons.Icon("check")).Size(12, 12).TextColor(k.TextFaint)
			default:
				ui.Spinner(c).Size(11, 11).Label("Running")
			}
		})
	}
}

// agentToolIcon names the icon of a tool call's category.
func agentToolIcon(kind acp.ToolKind) *ui.SVG {
	switch kind {
	case acp.ToolRead:
		return icons.Icon("file-text")
	case acp.ToolEdit:
		return icons.Icon("pencil")
	case acp.ToolExecute:
		return icons.Icon("square-terminal")
	case acp.ToolSearch:
		return icons.Icon("search")
	default:
		return icons.Icon("info")
	}
}

// agentPermission draws the options of a pending permission request.
func (a *app) agentPermission(c *ui.Context, k *colors, p *pane, pm *acp.Permission) {
	ui.Column(c).Gap(8).Padding(10).Radius(radius).Background(k.Hover).Children(func() {
		ui.Text(c, pm.Title).FontSize(12).FontWeight(600).TextColor(k.Text)
		ui.Row(c).Gap(6).Children(func() {
			for _, o := range pm.Options {
				option := o.ID
				if ui.Button(c, o.Name).Clicked() {
					a.later(func() { a.answerAgent(p, option) })
				}
			}
		})
	})
}

// agentCommandLabel is the command the pane starts, for its error card.
func (a *app) agentCommandLabel() string {
	if command := strings.TrimSpace(a.AgentCommand); command != "" {
		return command
	}
	return "omp acp"
}
