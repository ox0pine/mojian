//go:build darwin || linux || windows

package session

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"sync"

	"mojian/internal/acp"
)

// acpWireMsg is one JSON-line message carried over a multiplexed ACP stream
// between the session server and the client.
type acpWireMsg struct {
	Type    string       `json:"type"`
	Agent   string       `json:"agent,omitempty"`
	Text    string       `json:"text,omitempty"`
	Tool    *acp.Tool    `json:"tool,omitempty"`
	PermID  string       `json:"permId,omitempty"`
	Title   string       `json:"title,omitempty"`
	Options []acp.Option `json:"options,omitempty"`
	Option  string       `json:"option,omitempty"`
	Stop    string       `json:"stop,omitempty"`
	Err     string       `json:"err,omitempty"`
}

// ACPAgentsContext returns the names of ACP agents currently available on
// the server's PATH.
func (c *Client) ACPAgentsContext(ctx context.Context) (out []string, err error) {
	err = c.callContext(ctx, Request{Op: "acpAgents"}, &out)
	return
}

// ACPSession is a handle to a remote ACP task running on the session server.
type ACPSession struct {
	ms     *muxStream
	mu     sync.Mutex
	closed bool
}

// Cancel asks the remote server to cancel the running ACP turn.
func (s *ACPSession) Cancel() {
	s.send(acpWireMsg{Type: "cancel"})
}

// Close ends the remote ACP stream and terminates the remote agent process.
func (s *ACPSession) Close() {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.closed = true
	s.mu.Unlock()
	_ = s.ms.Close()
}

func (s *ACPSession) send(msg acpWireMsg) {
	b, err := json.Marshal(msg)
	if err != nil {
		return
	}
	b = append(b, '\n')
	_, _ = s.ms.Write(b)
}

// StartACP runs an ACP prompt on the session server in dir, forwarding all
// typed events and interactive permission replies over a multiplexed stream.
func (c *Client) StartACP(ctx context.Context, dir, agent, prompt string, events func(acp.Event)) (*ACPSession, error) {
	if events == nil {
		events = func(acp.Event) {}
	}
	id := c.mux.newStreamID()
	ms := c.mux.openStream(id)
	b, _ := json.Marshal(openRequest{
		Kind:   "acp",
		Dir:    dir,
		Agent:  agent,
		Prompt: prompt,
	})
	if err := c.mux.writeFrameContext(ctx, frame{typ: frameOpen, stream: id, payload: b}); err != nil {
		ms.finish(err)
		return nil, err
	}
	sess := &ACPSession{ms: ms}
	go func() {
		<-ctx.Done()
		sess.Cancel()
	}()
	go sess.readLoop(events)
	return sess, nil
}

func (s *ACPSession) readLoop(events func(acp.Event)) {
	defer s.Close()
	scanner := bufio.NewScanner(s.ms)
	scanner.Buffer(make([]byte, 64<<10), 4<<20)

	var mu sync.Mutex
	perms := map[string]*acp.Permission{}
	terminal := false

	for scanner.Scan() {
		var msg acpWireMsg
		if json.Unmarshal(scanner.Bytes(), &msg) != nil {
			continue
		}
		switch msg.Type {
		case "ready":
			events(acp.Event{Kind: acp.KindReady, Agent: msg.Agent})
		case "text":
			events(acp.Event{Kind: acp.KindAgentText, Text: msg.Text})
		case "tool":
			events(acp.Event{Kind: acp.KindTool, Tool: msg.Tool})
		case "permission":
			pid := msg.PermID
			pm := acp.NewPermission(msg.Title, msg.Options, func(opt string) {
				s.send(acpWireMsg{Type: "permission", PermID: pid, Option: opt})
			})
			mu.Lock()
			perms[pid] = pm
			mu.Unlock()
			events(acp.Event{Kind: acp.KindPermission, Permission: pm})
		case "permission_resolved":
			mu.Lock()
			pm := perms[msg.PermID]
			delete(perms, msg.PermID)
			mu.Unlock()
			if pm != nil {
				events(acp.Event{Kind: acp.KindPermissionResolved, Permission: pm})
			}
		case "turn_done":
			terminal = true
			events(acp.Event{Kind: acp.KindTurnDone, Stop: msg.Stop})
		case "failed":
			terminal = true
			events(acp.Event{Kind: acp.KindFailed, Err: errors.New(msg.Err)})
		case "closed":
			var err error
			if msg.Err != "" {
				err = errors.New(msg.Err)
			}
			events(acp.Event{Kind: acp.KindClosed, Err: err})
		}
	}
	if !terminal {
		err := scanner.Err()
		if err == nil {
			err = io.ErrUnexpectedEOF
		}
		if !errors.Is(err, io.EOF) {
			events(acp.Event{Kind: acp.KindFailed, Err: fmt.Errorf("remote agent disconnected: %w", err)})
		} else {
			events(acp.Event{Kind: acp.KindFailed, Err: errors.New("remote agent connection closed")})
		}
	}
}

// serveACP runs one ACP agent task on the server and bridges its events
// and permission prompts over ms until the turn finishes or ms closes.
func (s *Server) serveACP(ms *muxStream, o openRequest) {
	defer ms.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var writeMu sync.Mutex
	send := func(msg acpWireMsg) {
		b, err := json.Marshal(msg)
		if err != nil {
			return
		}
		b = append(b, '\n')
		writeMu.Lock()
		_, _ = ms.Write(b)
		writeMu.Unlock()
	}

	var permMu sync.Mutex
	permSeq := 0
	permByID := map[string]*acp.Permission{}
	idByPerm := map[*acp.Permission]string{}

	var sessMu sync.Mutex
	var acpSess *acp.Session

	// Read client commands (permission answers, cancel) and detect disconnect.
	go func() {
		scanner := bufio.NewScanner(ms)
		scanner.Buffer(make([]byte, 16<<10), 1<<20)
		for scanner.Scan() {
			var msg acpWireMsg
			if json.Unmarshal(scanner.Bytes(), &msg) != nil {
				continue
			}
			switch msg.Type {
			case "permission":
				permMu.Lock()
				pm := permByID[msg.PermID]
				permMu.Unlock()
				if pm != nil {
					pm.Answer(msg.Option)
				}
			case "cancel":
				cancel()
				sessMu.Lock()
				cur := acpSess
				sessMu.Unlock()
				if cur != nil {
					cur.Cancel()
				}
			}
		}
		cancel()
		sessMu.Lock()
		cur := acpSess
		sessMu.Unlock()
		if cur != nil {
			cur.Close()
		}
	}()

	onEvent := func(ev acp.Event) {
		switch ev.Kind {
		case acp.KindReady:
			send(acpWireMsg{Type: "ready", Agent: ev.Agent})
		case acp.KindAgentText:
			send(acpWireMsg{Type: "text", Text: ev.Text})
		case acp.KindTool:
			send(acpWireMsg{Type: "tool", Tool: ev.Tool})
		case acp.KindPermission:
			if ev.Permission == nil {
				return
			}
			permMu.Lock()
			permSeq++
			pid := strconv.Itoa(permSeq)
			permByID[pid] = ev.Permission
			idByPerm[ev.Permission] = pid
			permMu.Unlock()
			send(acpWireMsg{
				Type:    "permission",
				PermID:  pid,
				Title:   ev.Permission.Title,
				Options: ev.Permission.Options,
			})
		case acp.KindPermissionResolved:
			if ev.Permission == nil {
				return
			}
			permMu.Lock()
			pid := idByPerm[ev.Permission]
			delete(idByPerm, ev.Permission)
			delete(permByID, pid)
			permMu.Unlock()
			if pid != "" {
				send(acpWireMsg{Type: "permission_resolved", PermID: pid})
			}
		case acp.KindTurnDone:
			send(acpWireMsg{Type: "turn_done", Stop: ev.Stop})
		case acp.KindFailed:
			msg := "agent failed"
			if ev.Err != nil {
				msg = ev.Err.Error()
			}
			send(acpWireMsg{Type: "failed", Err: msg})
		case acp.KindClosed:
			msg := ""
			if ev.Err != nil {
				msg = ev.Err.Error()
			}
			send(acpWireMsg{Type: "closed", Err: msg})
		}
	}

	started, _, err := acp.StartAgent(ctx, o.Dir, o.Agent, onEvent)
	if err != nil {
		send(acpWireMsg{Type: "failed", Err: err.Error()})
		return
	}
	sessMu.Lock()
	acpSess = started
	sessMu.Unlock()
	defer started.Close()
	if ctx.Err() != nil {
		send(acpWireMsg{Type: "turn_done", Stop: "cancelled"})
		return
	}
	if err := started.Prompt(ctx, o.Prompt); err != nil {
		send(acpWireMsg{Type: "failed", Err: err.Error()})
	}
}
