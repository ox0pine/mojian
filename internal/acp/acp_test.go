package acp

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	sdk "github.com/coder/acp-go-sdk"
)

const testMarkerDelay = 2 * time.Second

// TestMain lets the test binary act as the agent when Start asks for it.
func TestMain(m *testing.M) {
	if os.Getenv("MOJIAN_TEST_ACP_MARKER_CHILD") == "1" {
		time.Sleep(testMarkerDelay)
		if err := os.WriteFile(os.Getenv("MOJIAN_TEST_ACP_MARKER"), []byte("survived"), 0o600); err != nil {
			os.Exit(1)
		}
		return
	}
	if os.Getenv("MOJIAN_TEST_ACP_AGENT") == "1" {
		runTestAgent()
		return
	}
	os.Exit(m.Run())
}

// runTestAgent speaks ACP as a small, deterministic agent: one greeting,
// one failing tool call and one permission question per turn.
func runTestAgent() {
	agent := &testAgent{}
	conn := sdk.NewAgentSideConnection(agent, os.Stdout, os.Stdin)
	agent.conn = conn
	<-conn.Done()
}

type testAgent struct{ conn *sdk.AgentSideConnection }

func (a *testAgent) Initialize(ctx context.Context, p sdk.InitializeRequest) (sdk.InitializeResponse, error) {
	return sdk.InitializeResponse{
		ProtocolVersion: p.ProtocolVersion,
		AgentInfo:       &sdk.Implementation{Name: "test-agent", Version: "1"},
	}, nil
}

func (a *testAgent) Authenticate(context.Context, sdk.AuthenticateRequest) (sdk.AuthenticateResponse, error) {
	return sdk.AuthenticateResponse{}, nil
}

func (a *testAgent) Logout(context.Context, sdk.LogoutRequest) (sdk.LogoutResponse, error) {
	return sdk.LogoutResponse{}, nil
}

func (a *testAgent) NewSession(context.Context, sdk.NewSessionRequest) (sdk.NewSessionResponse, error) {
	if os.Getenv("MOJIAN_TEST_ACP_MARKER") != "" {
		child := exec.Command(os.Args[0])
		child.Env = append(os.Environ(), "MOJIAN_TEST_ACP_MARKER_CHILD=1")
		if err := child.Start(); err != nil {
			return sdk.NewSessionResponse{}, err
		}
		// The agent deliberately leaves this tool child running until Close.
		_ = child.Process.Release()
	}
	return sdk.NewSessionResponse{SessionId: "test-session"}, nil
}

func (a *testAgent) CloseSession(context.Context, sdk.CloseSessionRequest) (sdk.CloseSessionResponse, error) {
	return sdk.CloseSessionResponse{}, nil
}

func (a *testAgent) ListSessions(context.Context, sdk.ListSessionsRequest) (sdk.ListSessionsResponse, error) {
	return sdk.ListSessionsResponse{}, nil
}

func (a *testAgent) ResumeSession(context.Context, sdk.ResumeSessionRequest) (sdk.ResumeSessionResponse, error) {
	return sdk.ResumeSessionResponse{}, nil
}

func (a *testAgent) SetSessionConfigOption(context.Context, sdk.SetSessionConfigOptionRequest) (sdk.SetSessionConfigOptionResponse, error) {
	return sdk.SetSessionConfigOptionResponse{}, nil
}

func (a *testAgent) SetSessionMode(context.Context, sdk.SetSessionModeRequest) (sdk.SetSessionModeResponse, error) {
	return sdk.SetSessionModeResponse{}, nil
}

func (a *testAgent) Cancel(context.Context, sdk.CancelNotification) error { return nil }

func (a *testAgent) Prompt(ctx context.Context, p sdk.PromptRequest) (sdk.PromptResponse, error) {
	if err := a.conn.SessionUpdate(ctx, sdk.SessionNotification{
		SessionId: p.SessionId,
		Update:    sdk.UpdateAgentMessageText("hello "),
	}); err != nil {
		return sdk.PromptResponse{}, err
	}
	if err := a.conn.SessionUpdate(ctx, sdk.SessionNotification{
		SessionId: p.SessionId,
		Update:    sdk.UpdateAgentMessageText("world"),
	}); err != nil {
		return sdk.PromptResponse{}, err
	}
	if err := a.conn.SessionUpdate(ctx, sdk.SessionNotification{
		SessionId: p.SessionId,
		Update: sdk.SessionUpdate{ToolCall: &sdk.SessionUpdateToolCall{
			ToolCallId: "tool-1",
			Title:      "Run the tests",
			Kind:       sdk.ToolKindExecute,
			Status:     sdk.ToolCallStatusInProgress,
		}},
	}); err != nil {
		return sdk.PromptResponse{}, err
	}
	failed := sdk.ToolCallStatusFailed
	if err := a.conn.SessionUpdate(ctx, sdk.SessionNotification{
		SessionId: p.SessionId,
		Update: sdk.SessionUpdate{ToolCallUpdate: &sdk.SessionToolCallUpdate{
			ToolCallId: "tool-1",
			Status:     &failed,
		}},
	}); err != nil {
		return sdk.PromptResponse{}, err
	}
	if _, err := a.conn.RequestPermission(ctx, sdk.RequestPermissionRequest{
		SessionId: p.SessionId,
		ToolCall:  sdk.ToolCallUpdate{ToolCallId: "tool-1"},
		Options: []sdk.PermissionOption{
			{OptionId: "allow", Name: "Allow", Kind: sdk.PermissionOptionKindAllowOnce},
			{OptionId: "reject", Name: "Reject", Kind: sdk.PermissionOptionKindRejectOnce},
		},
	}); err != nil {
		return sdk.PromptResponse{}, err
	}
	return sdk.PromptResponse{StopReason: sdk.StopReasonEndTurn}, nil
}

func TestSessionStreamsATurn(t *testing.T) {
	var mu sync.Mutex
	var events []Event
	answer := func(ev Event) {
		if ev.Kind == KindPermission {
			ev.Permission.Answer(ev.Permission.Options[0].ID)
		}
	}
	s, err := Start(context.Background(), Options{
		Dir:     t.TempDir(),
		Command: []string{os.Args[0]},
		Env:     []string{"MOJIAN_TEST_ACP_AGENT=1"},
		Events: func(ev Event) {
			mu.Lock()
			events = append(events, ev)
			mu.Unlock()
			answer(ev)
		},
	})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	defer s.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := s.Prompt(ctx, "hi"); err != nil {
		t.Fatalf("prompt: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	var ready, permission, done bool
	var text string
	toolStatus := ToolStatus("")
	for _, ev := range events {
		switch ev.Kind {
		case KindReady:
			ready = ev.Agent == "test-agent 1"
		case KindAgentText:
			text += ev.Text
		case KindTool:
			if ev.Tool.ID == "tool-1" && ev.Tool.Status != "" {
				toolStatus = ev.Tool.Status
			}
		case KindPermission:
			permission = len(ev.Permission.Options) == 2
		case KindTurnDone:
			done = ev.Stop == string(sdk.StopReasonEndTurn)
		}
	}
	if !ready {
		t.Errorf("no ready event with the agent name: %+v", events)
	}
	if text != "hello world" {
		t.Errorf("agent text = %q, want %q", text, "hello world")
	}
	if toolStatus != StatusFailed {
		t.Errorf("tool status = %q, want failed", toolStatus)
	}
	if !permission {
		t.Errorf("no permission event with both options: %+v", events)
	}
	if !done {
		t.Errorf("no end_turn event: %+v", events)
	}
}

func TestSessionCloseEndsTheProcess(t *testing.T) {
	s, err := Start(context.Background(), Options{
		Dir:     t.TempDir(),
		Command: []string{os.Args[0]},
		Env:     []string{"MOJIAN_TEST_ACP_AGENT=1"},
	})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	s.Close()
	select {
	case <-s.Done():
	case <-time.After(10 * time.Second):
		t.Fatal("the agent process did not end")
	}
	s.Close()
}

func TestSessionCloseKillsProcessGroup(t *testing.T) {
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		t.Skip("process-group regression runs on Unix")
	}
	marker := filepath.Join(t.TempDir(), "tool-child-survived")
	s, err := Start(context.Background(), Options{
		Dir:     t.TempDir(),
		Command: []string{os.Args[0]},
		Env: []string{
			"MOJIAN_TEST_ACP_AGENT=1",
			"MOJIAN_TEST_ACP_MARKER=" + marker,
		},
	})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	s.Close()
	select {
	case <-s.Done():
	case <-time.After(10 * time.Second):
		t.Fatal("the agent process group did not end")
	}
	time.Sleep(testMarkerDelay + 500*time.Millisecond)
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("the agent's tool child survived Close and wrote its marker")
	} else if !os.IsNotExist(err) {
		t.Fatalf("stat marker: %v", err)
	}
}

func TestStartAnyReportsWhenNoAgentIsInstalled(t *testing.T) {
	old := Agents
	Agents = []Agent{{Name: "Missing", Command: []string{"mojian-no-such-agent"}}}
	defer func() { Agents = old }()
	_, _, err := StartAny(context.Background(), t.TempDir(), nil)
	if err == nil {
		t.Fatal("no agent should have started")
	}
	if want := "no ACP agent found"; !strings.Contains(err.Error(), want) {
		t.Fatalf("error = %q, want it to mention %q", err, want)
	}
}

func TestStartAnySkipsAProgramThatIsNotAnACPAgent(t *testing.T) {
	old := Agents
	// go version starts, answers nothing and exits: not an ACP server.
	Agents = []Agent{{Name: "Fake", Command: []string{"go", "version"}}}
	defer func() { Agents = old }()
	_, _, err := StartAny(context.Background(), t.TempDir(), nil)
	if err == nil {
		t.Fatal("a program that exits without speaking ACP should not start")
	}
	if !strings.Contains(err.Error(), "Fake") {
		t.Fatalf("error = %q, want it to name the failed agent", err)
	}
}

func TestConcurrentPermissionRequestsResolveIndependently(t *testing.T) {
	events := make(chan Event, 4)
	s := &Session{done: make(chan struct{}), events: func(ev Event) { events <- ev }}
	type result struct {
		want sdk.PermissionOptionId
		resp sdk.RequestPermissionResponse
		err  error
	}
	results := make(chan result, 2)
	request := func(title string) {
		go func() {
			resp, err := (&client{s: s}).RequestPermission(context.Background(), sdk.RequestPermissionRequest{
				ToolCall: sdk.ToolCallUpdate{Title: &title},
				Options:  []sdk.PermissionOption{{OptionId: sdk.PermissionOptionId("allow-" + title), Name: "Allow"}},
			})
			results <- result{want: sdk.PermissionOptionId("allow-" + title), resp: resp, err: err}
		}()
	}
	request("A")
	request("B")
	pending := map[string]*Permission{}
	for range 2 {
		select {
		case ev := <-events:
			if ev.Kind != KindPermission || ev.Permission == nil {
				t.Fatalf("event = %+v, want permission", ev)
			}
			pending[ev.Permission.Title] = ev.Permission
		case <-time.After(5 * time.Second):
			t.Fatal("concurrent permission request did not reach the client")
		}
	}
	// Answer in reverse arrival-independent order: neither handler may be
	// overwritten or receive the other request's option.
	pending["B"].Answer("allow-B")
	pending["A"].Answer("allow-A")
	for range 2 {
		select {
		case got := <-results:
			if got.err != nil {
				t.Fatal(got.err)
			}
			if got.resp.Outcome.Selected == nil || got.resp.Outcome.Selected.OptionId != got.want {
				t.Errorf("permission outcome = %+v, want selected %q", got.resp.Outcome, got.want)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("a concurrent permission handler remained blocked")
		}
	}
	for range 2 {
		select {
		case ev := <-events:
			if ev.Kind != KindPermissionResolved || pending[ev.Permission.Title] != ev.Permission {
				t.Errorf("resolution event = %+v", ev)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("missing permission resolution event")
		}
	}
}

func TestPermissionCancellationEmitsResolution(t *testing.T) {
	events := make(chan Event, 2)
	s := &Session{done: make(chan struct{}), events: func(ev Event) { events <- ev }}
	ctx, cancel := context.WithCancel(context.Background())
	type result struct {
		resp sdk.RequestPermissionResponse
		err  error
	}
	resultCh := make(chan result, 1)
	go func() {
		resp, err := (&client{s: s}).RequestPermission(ctx, sdk.RequestPermissionRequest{
			Options: []sdk.PermissionOption{{OptionId: "allow", Name: "Allow"}},
		})
		resultCh <- result{resp: resp, err: err}
	}()
	var pending *Permission
	select {
	case ev := <-events:
		if ev.Kind != KindPermission {
			t.Fatalf("event kind = %v, want permission", ev.Kind)
		}
		pending = ev.Permission
	case <-time.After(5 * time.Second):
		t.Fatal("permission request did not arrive")
	}
	cancel()
	select {
	case got := <-resultCh:
		if got.err != nil {
			t.Fatal(got.err)
		}
		if got.resp.Outcome.Cancelled == nil {
			t.Fatalf("cancelled request outcome = %+v", got.resp.Outcome)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("cancelled request remained blocked")
	}
	select {
	case ev := <-events:
		if ev.Kind != KindPermissionResolved || ev.Permission != pending {
			t.Fatalf("resolution event = %+v, want the cancelled request", ev)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("cancellation did not emit a UI resolution")
	}
}

func TestCancellingAllPermissionsReleasesEveryHandler(t *testing.T) {
	events := make(chan Event, 4)
	s := &Session{done: make(chan struct{}), events: func(ev Event) { events <- ev }}
	results := make(chan sdk.RequestPermissionResponse, 2)
	for range 2 {
		go func() {
			resp, err := (&client{s: s}).RequestPermission(context.Background(), sdk.RequestPermissionRequest{
				Options: []sdk.PermissionOption{{OptionId: "allow", Name: "Allow"}},
			})
			if err != nil {
				t.Errorf("RequestPermission: %v", err)
			}
			results <- resp
		}()
	}
	var pending []*Permission
	for range 2 {
		select {
		case ev := <-events:
			if ev.Kind == KindPermission {
				pending = append(pending, ev.Permission)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("permission request did not arrive")
		}
	}
	// This is the same empty answer Stop sends to the visible and queued requests.
	for _, p := range pending {
		p.Answer("")
	}
	for range 2 {
		select {
		case resp := <-results:
			if resp.Outcome.Cancelled == nil {
				t.Errorf("stop outcome = %+v, want cancelled", resp.Outcome)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("Stop left a permission handler blocked")
		}
	}
}
