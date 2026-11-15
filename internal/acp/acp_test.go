package acp

import (
	"context"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	sdk "github.com/coder/acp-go-sdk"
)

// TestMain lets the test binary act as the agent when Start asks for it.
func TestMain(m *testing.M) {
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
