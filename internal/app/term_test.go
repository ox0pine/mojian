package app

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mojian/internal/acp"
	"mojian/internal/session"

	"github.com/egoist/mygo/ui"
)

func TestSSHPickerListsHosts(t *testing.T) {
	if !remoteSupported() {
		t.Skip("SSH is unavailable")
	}
	a := newProjectApp(t)
	a.settingsPath = ""
	a.hostService().SetHosts([]hostConfig{{Name: "Build box", Address: "buildbox"}})
	a.openSSHPicker()
	tt := ui.NewTester(a.view, 1100, 700)
	tt.Frame()
	if !a.sshPick || !tt.HasText("Connect to Server") || !tt.HasText("Build box") || !tt.HasText("buildbox") {
		t.Fatalf("ssh picker: open=%v texts=%q", a.sshPick, tt.Texts())
	}
}

func TestTerminalLayoutRoundTrip(t *testing.T) {
	if !remoteSupported() {
		t.Skip("remote terminals are unavailable")
	}
	dir := t.TempDir()
	a := &app{workspace: &workspace{layoutPath: filepath.Join(dir, "layout.json"), store: &store{}}}
	p := &pane{ID: nextID("p"), Term: &termTab{SID: "sess-1", Spec: termSpec{Host: "buildbox", Server: "buildbox"}}}
	tb := &tab{ID: nextID("t"), Root: &node{Pane: p}, Focus: p}
	a.tabs = []*tab{tb}
	a.active = 0
	a.saveLayout()

	b := &app{workspace: &workspace{layoutPath: a.layoutPath, store: &store{}}}
	if !b.restoreLayout() {
		t.Fatal("restoreLayout failed")
	}
	rp := b.tabs[0].panes()[0]
	if rp.Term == nil || rp.Term.SID != "sess-1" || rp.Term.Spec.Host != "buildbox" || rp.Term.Spec.Server != "buildbox" {
		t.Fatalf("restored term: %+v", rp.Term)
	}
}

// TestLastScreenLine checks the line a terminal notification carries: the
// last line with text, its spacing collapsed, cut for the body.
func TestLastScreenLine(t *testing.T) {
	cases := []struct {
		name   string
		screen string
		want   string
	}{
		{"empty", "", ""},
		{"blank lines", "\n  \n\t\n", ""},
		{"last text", "$ go test ./...\nok  \tmojian/internal/app\t1.2s\n\n$ ", "ok mojian/internal/app 1.2s"},
		{"agent answer", "omp\n\nDone: fixed the parser.\n", "Done: fixed the parser."},
	}
	for _, tc := range cases {
		if got := lastScreenLine(tc.screen); got != tc.want {
			t.Errorf("%s: got %q, want %q", tc.name, got, tc.want)
		}
	}

	long := strings.Repeat("a", 400)
	got := lastScreenLine(long)
	if r := []rune(got); len(r) != 160 || !strings.HasSuffix(got, "…") {
		t.Fatalf("long line: %d runes, %q", len(r), got)
	}
}

func TestJumpToAttentionCyclesTerminalsAndPermissions(t *testing.T) {
	registerFonts()
	a := newProjectApp(t)
	a.focusFirstNote()

	t1 := &tab{ID: "t-term-1"}
	p1 := newTermPane(&termTab{SID: "s1", Spec: termSpec{Server: localServerID}})
	t1.Root = &node{Pane: p1}
	t1.Focus = p1

	t2 := &tab{ID: "t-term-2"}
	p2 := newTermPane(&termTab{SID: "s2", Spec: termSpec{Server: localServerID}})
	t2.Root = &node{Pane: p2}
	t2.Focus = p2

	a.tabs = append(a.tabs, t1, t2)
	a.active = 0

	a.markTerminalAttention(p1.Term, "Bell")
	a.markTerminalAttention(p2.Term, "exit 0")
	if !t1.hasAttention() || !t2.hasAttention() {
		t.Fatal("expected tabs to report terminal attention")
	}

	// First jump lands on t1 / p1 and clears its attention.
	a.jumpToAttention()
	if a.active != 1 || a.focusPane() != p1 || p1.Term.attention {
		t.Fatalf("after first jump: active=%d focus=%v attention=%v", a.active, a.focusPane(), p1.Term.attention)
	}

	// Second jump lands on t2 / p2 and clears its attention.
	a.jumpToAttention()
	if a.active != 2 || a.focusPane() != p2 || p2.Term.attention {
		t.Fatalf("after second jump: active=%d focus=%v attention=%v", a.active, a.focusPane(), p2.Term.attention)
	}

	// Pending agent permission takes priority over terminals.
	a.markTerminalAttention(p1.Term, "Bell")
	a.agentTask = &agentTask{
		prompt: "fix tests",
		perm:   &acp.Permission{Title: "Run go test?"},
	}
	a.jumpToAttention()
	if !a.agentOpen {
		t.Fatal("expected jumpToAttention to open pending agent permission sheet")
	}
}

func TestSessionPickerListsAndKillsSessions(t *testing.T) {
	registerFonts()
	a := newProjectApp(t)
	a.sessionClient = &session.Client{}

	origList := listSessionsFn
	origKill := killPickedSessionFn
	defer func() {
		listSessionsFn = origList
		killPickedSessionFn = origKill
	}()

	sessions := []session.SessionInfo{
		{ID: "abc123", Shell: "zsh", Title: "claude-code", Dir: "/tmp/proj", Created: time.Now().Add(-2 * time.Minute)},
		{ID: "def456", Shell: "bash", Dir: "/tmp/other", Created: time.Now().Add(-5 * time.Minute)},
	}
	listSessionsFn = func(context.Context, *session.Client) ([]session.SessionInfo, error) {
		return append([]session.SessionInfo(nil), sessions...), nil
	}
	killed := ""
	killPickedSessionFn = func(_ *session.Client, sid string) error {
		killed = sid
		sessions = sessions[1:]
		return nil
	}

	tt := ui.NewTester(a.view, 1100, 700)
	a.openSessionPicker()
	tt.Frame()
	if !tt.HasText("claude-code") || !tt.HasText("bash") {
		t.Fatalf("expected listed sessions in picker, texts=%q", tt.Texts())
	}

	a.killPickedSession(localServerID, "abc123")
	tt.Frame()
	if killed != "abc123" || len(a.sessionItems) != 1 || a.sessionItems[0].ID != "def456" {
		t.Fatalf("kill result: killed=%q items=%+v", killed, a.sessionItems)
	}
}
