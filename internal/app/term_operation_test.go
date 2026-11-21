//go:build darwin || linux

package app

import (
	"context"
	"errors"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"mojian/internal/remote"
	"mojian/internal/session"
)

type terminalCreateResult struct {
	sid string
	err error
}

// queueWindowCallbacks runs nil-window worker completions on the test
// goroutine, as the native window's Update would on the UI thread.
func queueWindowCallbacks(t *testing.T) <-chan func() {
	t.Helper()
	posts := make(chan func(), 16)
	old := onWindowPostFn
	onWindowPostFn = func(fn func()) { posts <- fn }
	t.Cleanup(func() { onWindowPostFn = old })
	return posts
}

func runWindowUntil(t *testing.T, posts <-chan func(), what string, done func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for !done() {
		select {
		case fn := <-posts:
			fn()
		case <-time.After(10 * time.Millisecond):
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
	}
}

func TestRemoteTerminalLatestStartWinsWhenOlderFinishesFirst(t *testing.T) {
	testRemoteTerminalLatestStartWins(t, false)
}

func TestRemoteTerminalLatestStartWinsWhenNewerFinishesFirst(t *testing.T) {
	testRemoteTerminalLatestStartWins(t, true)
}

func testRemoteTerminalLatestStartWins(t *testing.T, newerFirst bool) {
	pooledClient, serverDone := startTestSessionServer(t, t.TempDir())
	control, err := session.Connect()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = control.Shutdown()
		select {
		case <-serverDone:
		case <-time.After(3 * time.Second):
			t.Error("session server did not stop")
		}
	})
	dialedClient, err := session.Connect()
	if err != nil {
		t.Fatal(err)
	}
	defer dialedClient.Close()
	oldDial, oldCreate, oldKill := dialTerminalRemoteFn, createTerminalSessionFn, killTerminalSessionFn
	t.Cleanup(func() {
		dialTerminalRemoteFn, createTerminalSessionFn, killTerminalSessionFn = oldDial, oldCreate, oldKill
	})
	firstStarted, secondStarted := make(chan struct{}), make(chan struct{})
	firstRelease, secondRelease := make(chan struct{}), make(chan struct{})
	firstCreated, secondCreated := make(chan terminalCreateResult, 1), make(chan terminalCreateResult, 1)
	killed := make(chan string, 2)
	var creates int32
	dialTerminalRemoteFn = func(_ context.Context, host string) (*remote.Conn, error) {
		return &remote.Conn{Host: host, Client: dialedClient}, nil
	}
	createTerminalSessionFn = func(client *session.Client, opts session.CreateOptions) (session.SessionInfo, error) {
		n := atomic.AddInt32(&creates, 1)
		if n > 2 {
			return session.SessionInfo{}, errors.New("unexpected extra terminal Create")
		}
		if n == 1 {
			close(firstStarted)
			<-firstRelease
		} else {
			close(secondStarted)
			<-secondRelease
		}
		opts.Command = []string{"/bin/sh", "-c", "sleep 30"}
		opts.Dir = ""
		info, err := client.Create(opts)
		created := terminalCreateResult{sid: info.ID, err: err}
		if n == 1 {
			firstCreated <- created
		} else {
			secondCreated <- created
		}
		return info, err
	}
	killTerminalSessionFn = func(client *session.Client, sid string) error {
		err := client.Kill(sid)
		if err == nil {
			killed <- sid
		}
		return err
	}

	posts := queueWindowCallbacks(t)
	p := newTermPane(&termTab{Spec: termSpec{Server: "box"}})
	tb := &tab{ID: nextID("t"), Root: &node{Pane: p}, Focus: p}
	a := &app{workspace: &workspace{
		serverAlias: "box", remotePath: "/srv/app", tabs: []*tab{tb}, active: 0,
	}}
	a.startRemoteTerminal(p)
	waitTerminalSignal(t, firstStarted, "the first Create")
	pooled := &remote.Conn{Host: "box", Client: pooledClient}
	a.installRemoteConn(pooled)
	if p.Term.connecting {
		t.Fatal("installing the new connection did not release the pane for reconnect")
	}
	a.startRemoteTerminal(p)
	waitTerminalSignal(t, secondStarted, "the second Create")

	var first, second terminalCreateResult
	var killedSID string
	waitKill := func() {
		runWindowUntil(t, posts, "the superseded session to be killed", func() bool {
			select {
			case killedSID = <-killed:
				return true
			default:
				return false
			}
		})
	}
	waitAttach := func() {
		runWindowUntil(t, posts, "the newer terminal to attach", func() bool {
			return p.Term.Term != nil && p.Term.SID == second.sid
		})
	}
	if newerFirst {
		close(secondRelease)
		second = <-secondCreated
		if second.err != nil {
			t.Fatal(second.err)
		}
		waitAttach()
		close(firstRelease)
		first = <-firstCreated
		if first.err != nil {
			t.Fatal(first.err)
		}
		waitKill()
	} else {
		close(firstRelease)
		first = <-firstCreated
		if first.err != nil {
			t.Fatal(first.err)
		}
		waitKill()
		close(secondRelease)
		second = <-secondCreated
		if second.err != nil {
			t.Fatal(second.err)
		}
		waitAttach()
	}

	if killedSID != first.sid {
		t.Fatalf("killed session %q, want superseded session %q", killedSID, first.sid)
	}
	if p.Term.Term == nil || p.Term.SID != second.sid || p.Term.Client != pooledClient {
		t.Fatalf("attached terminal = %q on %p, want %q on the latest connection", p.Term.SID, p.Term.Client, second.sid)
	}
	if p.Term.connecting || p.Term.startErr != "" {
		t.Fatalf("terminal state: connecting=%v err=%q", p.Term.connecting, p.Term.startErr)
	}
	select {
	case sid := <-killed:
		t.Fatalf("an additional session %q was killed", sid)
	default:
	}
	waitTerminalCondition(t, "the superseded connection to close", func() bool {
		select {
		case <-dialedClient.Closed():
			return true
		default:
			return false
		}
	})
	infos, err := control.List()
	if err != nil {
		t.Fatal(err)
	}
	latestActive := false
	for _, info := range infos {
		if info.ID == first.sid && !info.Exited {
			t.Fatalf("superseded session %q is still active", first.sid)
		}
		if info.ID == second.sid && !info.Exited {
			latestActive = true
		}
	}
	if !latestActive {
		t.Fatalf("latest session %q is not active", second.sid)
	}
	detachTerminalView(p)
}

func TestHostEditRejectsPendingTerminal(t *testing.T) {
	oldClient, serverDone := startTestSessionServer(t, t.TempDir())
	control, err := session.Connect()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = control.Shutdown()
		select {
		case <-serverDone:
		case <-time.After(3 * time.Second):
			t.Error("session server did not stop")
		}
	})
	oldDial, oldCreate, oldKill := dialTerminalRemoteFn, createTerminalSessionFn, killTerminalSessionFn
	t.Cleanup(func() {
		dialTerminalRemoteFn, createTerminalSessionFn, killTerminalSessionFn = oldDial, oldCreate, oldKill
	})
	createStarted := make(chan struct{})
	createRelease := make(chan struct{})
	created := make(chan terminalCreateResult, 1)
	killed := make(chan string, 1)
	dialTerminalRemoteFn = func(_ context.Context, host string) (*remote.Conn, error) {
		return &remote.Conn{Host: host, Client: oldClient}, nil
	}
	createTerminalSessionFn = func(client *session.Client, opts session.CreateOptions) (session.SessionInfo, error) {
		close(createStarted)
		<-createRelease
		opts.Command = []string{"/bin/sh", "-c", "sleep 30"}
		opts.Dir = ""
		info, err := client.Create(opts)
		created <- terminalCreateResult{sid: info.ID, err: err}
		return info, err
	}
	killTerminalSessionFn = func(client *session.Client, sid string) error {
		err := client.Kill(sid)
		if err == nil {
			killed <- sid
		}
		return err
	}

	posts := queueWindowCallbacks(t)
	p := newTermPane(&termTab{Spec: termSpec{Server: "old-box"}})
	tb := &tab{ID: nextID("t"), Root: &node{Pane: p}, Focus: p}
	key := workspaceKeyRemote("old-box", "/srv/app")
	ws := &workspace{key: key, serverAlias: "old-box", remotePath: "/srv/app", tabs: []*tab{tb}, active: 0}
	a := &app{
		workspace: ws, workspaces: map[string]*workspace{key: ws},
		settingsPath: filepath.Join(t.TempDir(), "settings.json"), hostEditAddr: "old-box",
	}
	a.hostService().AddHost("Old", "old-box")
	a.hostEditAddress, a.hostEditName = "new-box", "New"
	a.startRemoteTerminal(p)
	waitTerminalSignal(t, createStarted, "the old host Create")

	a.saveHostEdit("old-box")
	close(createRelease)
	result := <-created
	if result.err != nil {
		t.Fatal(result.err)
	}
	var killedSID string
	runWindowUntil(t, posts, "the old host session to be killed", func() bool {
		select {
		case killedSID = <-killed:
			return true
		default:
			return false
		}
	})
	if killedSID != result.sid {
		t.Fatalf("killed session %q, want old host session %q", killedSID, result.sid)
	}
	if p.Term.Term != nil || p.Term.SID != "" || p.Term.Client != nil {
		t.Fatalf("old host result attached: sid=%q client=%p", p.Term.SID, p.Term.Client)
	}
	if p.Term.Spec.Server != "new-box" || p.Term.connecting || p.Term.startErr != "" {
		t.Fatalf("edited terminal state: server=%q connecting=%v err=%q", p.Term.Spec.Server, p.Term.connecting, p.Term.startErr)
	}
	if a.remoteConn("old-box") != nil || a.remoteConn("new-box") != nil {
		t.Fatal("the stale worker installed a connection in the pool")
	}
	infos, err := control.List()
	if err != nil {
		t.Fatal(err)
	}
	for _, info := range infos {
		if info.ID == result.sid && !info.Exited {
			t.Fatalf("old host session %q is still active", result.sid)
		}
	}
}
