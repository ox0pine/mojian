//go:build darwin || linux

package app

import (
	"context"
	"errors"
	"testing"
	"time"

	"mojian/internal/remote"
	"mojian/internal/session"
)

func waitTerminalSignal(t *testing.T, ch <-chan struct{}, what string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(3 * time.Second):
		t.Fatalf("timed out waiting for %s", what)
	}
}

func waitTerminalCondition(t *testing.T, what string, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for !condition() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func waitTerminalKill(t *testing.T, killed <-chan string) string {
	t.Helper()
	select {
	case sid := <-killed:
		return sid
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for the stale session to be killed")
		return ""
	}
}

func TestRemoteTerminalStartDoesNotBlockAndReapsClosedPane(t *testing.T) {
	oldDial, oldCreate, oldKill := dialTerminalRemoteFn, createTerminalSessionFn, killTerminalSessionFn
	t.Cleanup(func() {
		dialTerminalRemoteFn, createTerminalSessionFn, killTerminalSessionFn = oldDial, oldCreate, oldKill
	})
	dialStarted := make(chan struct{})
	dialRelease := make(chan struct{})
	createStarted := make(chan struct{})
	createRelease := make(chan struct{})
	killed := make(chan string, 1)
	dialTerminalRemoteFn = func(ctx context.Context, host string) (*remote.Conn, error) {
		close(dialStarted)
		<-dialRelease
		return &remote.Conn{Host: host}, nil
	}
	createTerminalSessionFn = func(_ *session.Client, opts session.CreateOptions) (session.SessionInfo, error) {
		close(createStarted)
		<-createRelease
		if opts.Dir != "/srv/app" {
			t.Errorf("create directory = %q, want the captured remote path", opts.Dir)
		}
		return session.SessionInfo{ID: "late-session"}, nil
	}
	killTerminalSessionFn = func(_ *session.Client, sid string) error {
		killed <- sid
		return nil
	}

	from := newTermPane(&termTab{Spec: termSpec{Server: "box"}})
	tb := &tab{ID: nextID("t"), Root: &node{Pane: from}, Focus: from}
	a := &app{workspace: &workspace{
		serverAlias: "box", remotePath: "/srv/app",
		tabs: []*tab{tb}, active: 0,
	}}
	a.splitTerminalSide(from, splitRight)
	opening := tb.Focus
	if opening == from || opening.Term == nil || !opening.Term.connecting {
		t.Fatal("split did not immediately add a connecting terminal pane")
	}
	waitTerminalSignal(t, dialStarted, "Dial to start")
	close(dialRelease)
	waitTerminalSignal(t, createStarted, "Create to start")

	// Closing while Create is unanswered must not attach its result to the
	// surviving pane, and must still reap the session once its SID arrives.
	a.closePaneNow(tb, opening)
	if tb.contains(opening) {
		t.Fatal("the closing pane stayed in the layout")
	}
	close(createRelease)
	select {
	case sid := <-killed:
		if sid != "late-session" {
			t.Fatalf("killed session %q, want late-session", sid)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for the stale session to be killed")
	}
	if opening.Term.Term != nil || from.Term.Term != nil || from.Term.SID != "" {
		t.Fatal("a stale terminal result attached to a pane")
	}
}

func TestRemoteTerminalExistingClientCreateIsAsyncAndKeptUntilCleanup(t *testing.T) {
	pooled, serverDone := startTestSessionServer(t, t.TempDir())
	control, err := session.Connect()
	if err != nil {
		t.Fatal(err)
	}
	defer control.Close()
	oldCreate, oldKill := createTerminalSessionFn, killTerminalSessionFn
	t.Cleanup(func() {
		createTerminalSessionFn, killTerminalSessionFn = oldCreate, oldKill
	})
	createStarted := make(chan struct{})
	createRelease := make(chan struct{})
	killed := make(chan string, 1)
	createTerminalSessionFn = func(_ *session.Client, _ session.CreateOptions) (session.SessionInfo, error) {
		close(createStarted)
		<-createRelease
		return session.SessionInfo{ID: "late-existing"}, nil
	}
	killTerminalSessionFn = func(_ *session.Client, sid string) error {
		killed <- sid
		return nil
	}

	p := newTermPane(&termTab{Spec: termSpec{Server: "box"}})
	tb := &tab{ID: nextID("t"), Root: &node{Pane: p}, Focus: p}
	a := &app{workspace: &workspace{tabs: []*tab{tb}, active: 0}}
	a.setRemoteConn(&remote.Conn{Host: "box", Client: pooled})
	a.startRemoteTerminal(p)
	waitTerminalSignal(t, createStarted, "Create on the existing client")
	a.closeTab(tb)
	select {
	case <-pooled.Closed():
		t.Fatal("closing the pane released the client while Create was pending")
	default:
	}
	close(createRelease)
	select {
	case sid := <-killed:
		if sid != "late-existing" {
			t.Fatalf("killed session %q, want late-existing", sid)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for the existing-client session cleanup")
	}
	select {
	case <-pooled.Closed():
	case <-time.After(3 * time.Second):
		t.Fatal("the unused pooled connection survived terminal cleanup")
	}
	if p.Term.Term != nil || p.Term.SID != "" {
		t.Fatal("the closed pane accepted the late Create result")
	}
	if err := control.Shutdown(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-serverDone:
	case <-time.After(3 * time.Second):
		t.Fatal("session server did not stop")
	}
}

func TestRemoteTerminalDialPrefersPooledConnectionAndClosesDuplicate(t *testing.T) {
	pooledClient, serverDone := startTestSessionServer(t, t.TempDir())
	dialedClient, err := session.Connect()
	if err != nil {
		t.Fatal(err)
	}
	defer dialedClient.Close()
	oldDial, oldCreate := dialTerminalRemoteFn, createTerminalSessionFn
	t.Cleanup(func() {
		dialTerminalRemoteFn, createTerminalSessionFn = oldDial, oldCreate
	})
	dialStarted := make(chan struct{})
	dialRelease := make(chan struct{})
	dialTerminalRemoteFn = func(_ context.Context, host string) (*remote.Conn, error) {
		close(dialStarted)
		<-dialRelease
		return &remote.Conn{Host: host, Client: dialedClient}, nil
	}
	createTerminalSessionFn = func(_ *session.Client, _ session.CreateOptions) (session.SessionInfo, error) {
		return session.SessionInfo{}, errors.New("stop after connection selection")
	}

	from := newTermPane(&termTab{Spec: termSpec{Server: "box"}})
	tb := &tab{ID: nextID("t"), Root: &node{Pane: from}, Focus: from}
	a := &app{workspace: &workspace{tabs: []*tab{tb}, active: 0}}
	a.splitTerminalSide(from, splitRight)
	opening := tb.Focus
	waitTerminalSignal(t, dialStarted, "the duplicate Dial")
	pooled := &remote.Conn{Host: "box", Client: pooledClient}
	a.setRemoteConn(pooled)
	close(dialRelease)
	select {
	case <-dialedClient.Closed():
	case <-time.After(3 * time.Second):
		t.Fatal("the duplicate dialed connection was not closed")
	}
	if got := a.remoteConn("box"); got != pooled {
		t.Fatalf("pool connection = %p, want the existing pooled connection %p", got, pooled)
	}
	select {
	case <-pooledClient.Closed():
		t.Fatal("the selected pooled connection was closed")
	default:
	}
	if opening.Term.connecting || opening.Term.startErr != "session: stop after connection selection" {
		t.Fatalf("terminal state: connecting=%v err=%q", opening.Term.connecting, opening.Term.startErr)
	}
	if err := pooledClient.Shutdown(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-serverDone:
	case <-time.After(3 * time.Second):
		t.Fatal("session server did not stop")
	}
}
