// Package acp runs one Agent Client Protocol agent for an app pane: it
// starts the agent over stdio, opens a session, and reports the turn's
// updates. The protocol plumbing is the ACP Go SDK; this package gives the
// window a small event stream instead of the wire types.
package acp

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	sdk "github.com/coder/acp-go-sdk"
)

// Kind is what an Event reports.
type Kind int

const (
	// KindReady is the first event: the agent answered initialize and
	// opened a session.
	KindReady Kind = iota
	// KindAgentText is a chunk of the agent's answer.
	KindAgentText
	// KindThought is a chunk of the agent's reasoning.
	KindThought
	// KindUserText is a chunk of the user's message the agent streamed
	// back.
	KindUserText
	// KindTool is a tool call, new or changed. Zero fields mean unchanged.
	KindTool
	// KindPermission asks the user to choose an option of a tool call.
	KindPermission
	// KindTurnDone ends a prompt turn.
	KindTurnDone
	// KindFailed reports why a prompt could not run.
	KindFailed
	// KindClosed reports that the agent process ended.
	KindClosed
)

// Tool is the part of a tool call a pane shows.
type Tool struct {
	ID     string
	Title  string
	Kind   ToolKind
	Status ToolStatus
}

// Option is one answer to a permission request.
type Option struct {
	ID   string
	Name string
}

// Permission is a pending request. Answer once, with an option ID or the
// empty string to cancel it.
type Permission struct {
	Title   string
	Options []Option
	once    sync.Once
	answer  chan string
}

// Answer sends the user's choice; later calls do nothing.
func (p *Permission) Answer(id string) {
	if p == nil {
		return
	}
	p.once.Do(func() { p.answer <- id })
}

// Event is one update from an agent.
type Event struct {
	Kind       Kind
	Agent      string // Ready: the agent's name and version
	Text       string
	Tool       *Tool
	Permission *Permission
	Stop       string // TurnDone: why the turn ended
	Err        error
}

// ToolKind and ToolStatus name the protocol's categories without
// exposing the SDK to the window.
type ToolKind = sdk.ToolKind
type ToolStatus = sdk.ToolCallStatus

const (
	ToolRead    = sdk.ToolKindRead
	ToolEdit    = sdk.ToolKindEdit
	ToolExecute = sdk.ToolKindExecute
	ToolSearch  = sdk.ToolKindSearch
	ToolFetch   = sdk.ToolKindFetch
	ToolOther   = sdk.ToolKindOther

	StatusPending    = sdk.ToolCallStatusPending
	StatusInProgress = sdk.ToolCallStatusInProgress
	StatusCompleted  = sdk.ToolCallStatusCompleted
	StatusFailed     = sdk.ToolCallStatusFailed
)

// ErrClosed is returned by Prompt after Close.
var ErrClosed = errors.New("the agent has stopped")

// Agent is one ACP agent command the window knows how to run.
type Agent struct {
	Name    string
	Command []string
}

// Agents are tried in order; the first command on PATH that answers the
// protocol is the one a task runs. Claude Code and Codex join through
// their ACP adapters, not their own interactive commands.
var Agents = []Agent{
	{Name: "omp", Command: []string{"omp", "acp"}},
	{Name: "Claude Code", Command: []string{"claude-code-acp"}},
	{Name: "Claude Code", Command: []string{"claude-code-acp-rs"}},
	{Name: "Codex", Command: []string{"codex-acp"}},
	{Name: "Gemini CLI", Command: []string{"gemini", "--experimental-acp"}},
	{Name: "Qwen Code", Command: []string{"qwen", "--experimental-acp"}},
	{Name: "Goose", Command: []string{"goose", "acp"}},
	{Name: "OpenCode", Command: []string{"opencode", "acp"}},
	{Name: "Kimi", Command: []string{"kimi", "--acp"}},
	{Name: "Amp", Command: []string{"amp", "acp"}},
	{Name: "Cursor", Command: []string{"cursor-agent", "acp"}},
}

// probeTimeout bounds one agent's handshake, so an installed program that
// is not an ACP server is skipped instead of holding the task forever.
const probeTimeout = 20 * time.Second

// StartAny runs the first known agent that answers, so a task does not
// depend on one vendor's command.
func StartAny(ctx context.Context, dir string, events func(Event)) (*Session, Agent, error) {
	var failures []string
	for _, agent := range Agents {
		if _, err := exec.LookPath(agent.Command[0]); err != nil {
			continue
		}
		probe, cancel := context.WithTimeout(ctx, probeTimeout)
		s, err := Start(probe, Options{Dir: dir, Command: agent.Command, Events: events})
		cancel()
		if err == nil {
			return s, agent, nil
		}
		failures = append(failures, agent.Name+": "+err.Error())
	}
	if len(failures) == 0 {
		return nil, Agent{}, errors.New("no ACP agent found; looked for " + agentNames() + ". Claude Code and Codex need claude-code-acp or codex-acp installed.")
	}
	return nil, Agent{}, errors.New(strings.Join(failures, "; "))
}

func agentNames() string {
	names := make([]string, 0, len(Agents))
	for _, a := range Agents {
		names = append(names, a.Command[0])
	}
	return strings.Join(names, ", ")
}

// Options starts an agent.
type Options struct {
	// Dir is the working directory the session names, usually the project.
	Dir string
	// Command runs the agent; empty means "omp acp".
	Command []string
	// Env is appended to the environment; tests use it to pick a helper.
	Env []string
	// Events is called from the connection's goroutines; the window must
	// hand each event to its own thread before touching its state.
	Events func(Event)
}

// Session is one running agent and its open session.
type Session struct {
	cmd    *exec.Cmd
	conn   *sdk.ClientSideConnection
	id     sdk.SessionId
	events func(Event)
	stderr *tail
	ctx    context.Context
	cancel context.CancelFunc
	done   chan struct{}

	mu           sync.Mutex
	promptCancel context.CancelFunc
	promptSeq    int
	closed       bool
}

// Start runs the agent, negotiates the protocol and opens a session. It
// returns only when the agent is ready to take a prompt.
func Start(ctx context.Context, o Options) (*Session, error) {
	if o.Events == nil {
		o.Events = func(Event) {}
	}
	command := o.Command
	if len(command) == 0 {
		command = []string{"omp", "acp"}
	}
	cmd := exec.Command(command[0], command[1:]...)
	cmd.Dir = o.Dir
	cmd.Env = append(os.Environ(), o.Env...)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	s := &Session{
		cmd:    cmd,
		events: o.Events,
		stderr: &tail{},
		done:   make(chan struct{}),
	}
	cmd.Stderr = s.stderr
	s.ctx, s.cancel = context.WithCancel(context.Background())
	s.conn = sdk.NewClientSideConnection(&client{s: s}, stdin, stdout)
	if err := cmd.Start(); err != nil {
		s.cancel()
		return nil, fmt.Errorf("%s: %w", strings.Join(command, " "), err)
	}
	go s.wait()
	initRes, err := s.conn.Initialize(ctx, sdk.InitializeRequest{
		ProtocolVersion: sdk.ProtocolVersionNumber,
		ClientInfo:      &sdk.Implementation{Name: "Mojian", Version: "1"},
	})
	if err != nil {
		s.Close()
		return nil, s.wrap("initialize", err)
	}
	if initRes.ProtocolVersion != sdk.ProtocolVersionNumber {
		s.Close()
		return nil, fmt.Errorf("the agent speaks ACP version %d, not %d", initRes.ProtocolVersion, sdk.ProtocolVersionNumber)
	}
	sess, err := s.conn.NewSession(ctx, sdk.NewSessionRequest{Cwd: o.Dir, McpServers: []sdk.McpServer{}})
	if err != nil {
		s.Close()
		return nil, s.wrap("new session", err)
	}
	s.id = sess.SessionId
	name := "agent"
	if initRes.AgentInfo != nil {
		name = strings.TrimSpace(initRes.AgentInfo.Name + " " + initRes.AgentInfo.Version)
	}
	s.events(Event{Kind: KindReady, Agent: name})
	return s, nil
}

// wrap adds the agent's last stderr line to a protocol error, which is
// often the only place it says what went wrong.
func (s *Session) wrap(what string, err error) error {
	if line := s.stderr.lastLine(); line != "" {
		return fmt.Errorf("%s: %w (%s)", what, err, line)
	}
	return fmt.Errorf("%s: %w", what, err)
}

// wait reaps the process and reports its end once.
func (s *Session) wait() {
	_ = s.cmd.Wait()
	s.cancel()
	select {
	case <-s.done:
	default:
		close(s.done)
	}
	s.events(Event{Kind: KindClosed, Err: s.stderr.err()})
}

// Prompt sends text and blocks until the turn ends.
func (s *Session) Prompt(ctx context.Context, text string) error {
	ctx, cancel := context.WithCancel(ctx)
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		cancel()
		return ErrClosed
	}
	s.promptSeq++
	seq := s.promptSeq
	s.promptCancel = cancel
	s.mu.Unlock()
	defer func() {
		cancel()
		s.mu.Lock()
		if s.promptSeq == seq {
			s.promptCancel = nil
		}
		s.mu.Unlock()
	}()
	resp, err := s.conn.Prompt(ctx, sdk.PromptRequest{
		SessionId: s.id,
		Prompt:    []sdk.ContentBlock{sdk.TextBlock(text)},
	})
	if err != nil {
		if errors.Is(err, context.Canceled) {
			s.events(Event{Kind: KindTurnDone, Stop: "cancelled"})
			return nil
		}
		return err
	}
	s.events(Event{Kind: KindTurnDone, Stop: string(resp.StopReason)})
	return nil
}

// Cancel asks the agent to stop the running turn.
func (s *Session) Cancel() {
	s.mu.Lock()
	cancel := s.promptCancel
	closed := s.closed
	s.mu.Unlock()
	if closed {
		return
	}
	_ = s.conn.Cancel(context.Background(), sdk.CancelNotification{SessionId: s.id})
	if cancel != nil {
		cancel()
	}
}

// Close stops the agent. It is safe more than once.
func (s *Session) Close() {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.closed = true
	cancel := s.promptCancel
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	s.cancel()
	if s.cmd.Process != nil {
		_ = s.cmd.Process.Kill()
	}
}

// Done closes when the agent process has ended.
func (s *Session) Done() <-chan struct{} { return s.done }

// client answers the agent's requests. The window advertises no file
// system or terminal support, so those requests should never arrive.
type client struct{ s *Session }

func (c *client) SessionUpdate(ctx context.Context, n sdk.SessionNotification) error {
	u := n.Update
	switch {
	case u.AgentMessageChunk != nil:
		c.text(KindAgentText, u.AgentMessageChunk.Content)
	case u.AgentThoughtChunk != nil:
		c.text(KindThought, u.AgentThoughtChunk.Content)
	case u.UserMessageChunk != nil:
		c.text(KindUserText, u.UserMessageChunk.Content)
	case u.ToolCall != nil:
		t := u.ToolCall
		c.s.events(Event{Kind: KindTool, Tool: &Tool{
			ID: string(t.ToolCallId), Title: t.Title, Kind: t.Kind, Status: t.Status,
		}})
	case u.ToolCallUpdate != nil:
		up := u.ToolCallUpdate
		t := &Tool{ID: string(up.ToolCallId)}
		if up.Title != nil {
			t.Title = *up.Title
		}
		if up.Kind != nil {
			t.Kind = *up.Kind
		}
		if up.Status != nil {
			t.Status = *up.Status
		}
		c.s.events(Event{Kind: KindTool, Tool: t})
	}
	return nil
}

func (c *client) text(kind Kind, b sdk.ContentBlock) {
	if b.Text != nil && b.Text.Text != "" {
		c.s.events(Event{Kind: kind, Text: b.Text.Text})
	}
}

func (c *client) RequestPermission(ctx context.Context, p sdk.RequestPermissionRequest) (sdk.RequestPermissionResponse, error) {
	pm := &Permission{Options: make([]Option, 0, len(p.Options)), answer: make(chan string, 1)}
	if p.ToolCall.Title != nil {
		pm.Title = *p.ToolCall.Title
	}
	for _, o := range p.Options {
		pm.Options = append(pm.Options, Option{ID: string(o.OptionId), Name: o.Name})
	}
	c.s.events(Event{Kind: KindPermission, Permission: pm})
	select {
	case id := <-pm.answer:
		if id == "" {
			return sdk.RequestPermissionResponse{Outcome: sdk.NewRequestPermissionOutcomeCancelled()}, nil
		}
		return sdk.RequestPermissionResponse{Outcome: sdk.NewRequestPermissionOutcomeSelected(sdk.PermissionOptionId(id))}, nil
	case <-ctx.Done():
		return sdk.RequestPermissionResponse{Outcome: sdk.NewRequestPermissionOutcomeCancelled()}, nil
	case <-c.s.done:
		return sdk.RequestPermissionResponse{Outcome: sdk.NewRequestPermissionOutcomeCancelled()}, nil
	}
}

func (c *client) ReadTextFile(ctx context.Context, p sdk.ReadTextFileRequest) (sdk.ReadTextFileResponse, error) {
	return sdk.ReadTextFileResponse{}, errors.New("reading files is not supported")
}

func (c *client) WriteTextFile(ctx context.Context, p sdk.WriteTextFileRequest) (sdk.WriteTextFileResponse, error) {
	return sdk.WriteTextFileResponse{}, errors.New("writing files is not supported")
}

func (c *client) CreateTerminal(ctx context.Context, p sdk.CreateTerminalRequest) (sdk.CreateTerminalResponse, error) {
	return sdk.CreateTerminalResponse{}, errors.New("terminals are not supported")
}

func (c *client) KillTerminal(ctx context.Context, p sdk.KillTerminalRequest) (sdk.KillTerminalResponse, error) {
	return sdk.KillTerminalResponse{}, errors.New("terminals are not supported")
}

func (c *client) TerminalOutput(ctx context.Context, p sdk.TerminalOutputRequest) (sdk.TerminalOutputResponse, error) {
	return sdk.TerminalOutputResponse{}, errors.New("terminals are not supported")
}

func (c *client) ReleaseTerminal(ctx context.Context, p sdk.ReleaseTerminalRequest) (sdk.ReleaseTerminalResponse, error) {
	return sdk.ReleaseTerminalResponse{}, errors.New("terminals are not supported")
}

func (c *client) WaitForTerminalExit(ctx context.Context, p sdk.WaitForTerminalExitRequest) (sdk.WaitForTerminalExitResponse, error) {
	return sdk.WaitForTerminalExitResponse{}, errors.New("terminals are not supported")
}

// tail keeps the last few kilobytes an agent wrote to stderr, so a
// failure can say what the agent complained about.
type tail struct {
	mu  sync.Mutex
	buf []byte
}

const tailSize = 8 << 10

func (t *tail) Write(p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.buf = append(t.buf, p...)
	if len(t.buf) > tailSize {
		t.buf = t.buf[len(t.buf)-tailSize:]
	}
	return len(p), nil
}

func (t *tail) lastLine() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	lines := strings.Split(strings.TrimSpace(string(t.buf)), "\n")
	return strings.TrimSpace(lines[len(lines)-1])
}

func (t *tail) err() error {
	if line := t.lastLine(); line != "" {
		return errors.New(line)
	}
	return nil
}

var _ io.Writer = (*tail)(nil)
