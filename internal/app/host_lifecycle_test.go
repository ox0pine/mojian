package app

import (
	"context"
	"testing"

	"mojian/internal/fs"
	"mojian/internal/remote"
	"mojian/internal/session"
)

func TestHostRemovalCancelsOnlyMatchingRemoteTask(t *testing.T) {
	a := &app{workspace: &workspace{}}
	a.hostService().SetHosts([]hostConfig{{Name: "Box", Address: "buildbox"}, {Name: "Other", Address: "other"}})
	seq, _, ctx := a.beginRemoteTask("buildbox", "Opening buildbox")
	a.removeHost("other")
	if !a.remoteBusy {
		t.Fatal("removing another host canceled the active open")
	}
	select {
	case <-ctx.Done():
		t.Fatal("removing another host canceled its context")
	default:
	}
	a.removeHost("buildbox")
	if a.remoteBusy {
		t.Fatal("removing the task's host left the loading card up")
	}
	select {
	case <-ctx.Done():
	default:
		t.Fatal("removing the task's host did not cancel its context")
	}
	if a.endRemoteTask(seq) {
		t.Fatal("the removed host's open could still publish")
	}
}

func TestHostRemovalDuringSaveCancelsSecondOpen(t *testing.T) {
	remoteStore := &store{FS: fs.NewServer(nil, "/srv/app", "", false)}
	a := &app{workspace: &workspace{store: remoteStore, serverAlias: "buildbox", remotePath: "/srv/app"}}
	a.uiPost = func(func()) {}
	a.saveTask.active = true
	a.saveTask.store = remoteStore
	a.hostService().AddHost("Box", "buildbox")
	firstSeq, _, firstCtx := a.beginRemoteTask("buildbox", "First open")
	a.removeHost("buildbox")
	select {
	case <-firstCtx.Done():
	default:
		t.Fatal("removal did not cancel the first open")
	}
	if a.endRemoteTask(firstSeq) {
		t.Fatal("the first open could still publish")
	}
	if len(a.saveWaiters) != 1 {
		t.Fatal("removal did not wait for the remote save")
	}
	secondSeq, _, secondCtx := a.beginRemoteTask("buildbox", "Second open")
	a.saveTask.active = false
	a.saveTask.store = nil
	a.finishRemoteSaveWaiters(remoteStore, nil)
	select {
	case <-secondCtx.Done():
	default:
		t.Fatal("the save-completion removal did not cancel the second open")
	}
	if a.endRemoteTask(secondSeq) {
		t.Fatal("the second open could resurrect the removed host")
	}
	for _, h := range a.hosts() {
		if h.Address == "buildbox" {
			t.Fatal("the host remained configured")
		}
	}
}

func TestHostEditCancelsOldRemoteTask(t *testing.T) {
	a := &app{workspace: &workspace{}, hostEditAddr: "old-box", hostEditAddress: "new-box", hostEditName: "New"}
	a.hostService().AddHost("Old", "old-box")
	seq, _, ctx := a.beginRemoteTask("old-box", "Opening old-box")
	a.saveHostEdit("old-box")
	select {
	case <-ctx.Done():
	default:
		t.Fatal("editing the host did not cancel its old open")
	}
	if a.endRemoteTask(seq) {
		t.Fatal("the old address's open could still publish")
	}
	if a.remoteTask.host != "" {
		t.Fatalf("old task host remained %q", a.remoteTask.host)
	}
}

func TestHostEditInvalidatesOldRemotePicker(t *testing.T) {
	a := &app{workspace: &workspace{}, hostEditAddr: "old-box", hostEditAddress: "new-box", hostEditName: "New"}
	a.hostService().AddHost("Old", "old-box")
	a.setRemoteConn(&remote.Conn{Host: "old-box"})
	a.remotePicker = true
	a.remoteHost, a.remotePickHost = "old-box", "old-box"
	a.remoteDirsBusy = true
	a.remoteDirsSeq = 7
	a.remoteDirs = []session.FileEntry{{Rel: "old"}}

	a.saveHostEdit("old-box")

	if a.remotePicker || a.remoteDirsBusy || a.remoteHost != "" || a.remotePickHost != "" {
		t.Fatalf("the edited host kept its picker: picker=%v busy=%v host=%q pick=%q", a.remotePicker, a.remoteDirsBusy, a.remoteHost, a.remotePickHost)
	}
	if a.remoteDirsSeq != 8 || a.remoteDirs != nil {
		t.Fatalf("the old directory result was not invalidated: seq=%d dirs=%+v", a.remoteDirsSeq, a.remoteDirs)
	}
	if a.remoteConn("old-box") != nil {
		t.Fatal("the edited host's unused picker connection was not reaped")
	}
}

func TestHostRemovalInvalidatesOldRemotePicker(t *testing.T) {
	a := &app{workspace: &workspace{}}
	a.hostService().AddHost("Old", "old-box")
	a.setRemoteConn(&remote.Conn{Host: "old-box"})
	a.remotePicker = true
	a.remoteHost, a.remotePickHost = "old-box", "old-box"
	a.remoteDirsBusy = true
	a.remoteDirsSeq = 4

	a.removeHostNow("old-box")

	if a.remotePicker || a.remoteDirsBusy || a.remoteHost != "" || a.remotePickHost != "" {
		t.Fatal("the removed host kept its remote picker")
	}
	if a.remoteDirsSeq != 5 {
		t.Fatalf("directory sequence = %d, want 5", a.remoteDirsSeq)
	}
	if a.remoteConn("old-box") != nil {
		t.Fatal("the removed host's picker connection was not reaped")
	}
}

func TestHostStopLeavesTerminalStoppedUntilExplicitStart(t *testing.T) {
	oldDial := dialTerminalRemoteFn
	t.Cleanup(func() { dialTerminalRemoteFn = oldDial })
	dialed := false
	dialTerminalRemoteFn = func(context.Context, string) (*remote.Conn, error) {
		dialed = true
		return nil, nil
	}
	p := newTermPane(&termTab{Spec: termSpec{Server: "box"}})
	tb := &tab{ID: nextID("t"), Root: &node{Pane: p}, Focus: p}
	ws := &workspace{serverAlias: "box", remotePath: "/srv/app", tabs: []*tab{tb}, active: 0}
	a := &app{workspace: ws, workspaces: map[string]*workspace{"box": ws}, hostSetupAddr: "box", hostSetupSeq: 1}

	a.finishHostStop("box", 1, 0, nil)
	if p.Term.startErr != remoteServerStoppedError {
		t.Fatalf("stopped terminal error = %q, want %q", p.Term.startErr, remoteServerStoppedError)
	}
	p.Term.startErr = ""
	a.startRestoredTerminal(p)
	if dialed || a.remoteBusy {
		t.Fatal("a visible terminal redialed or restarted its stopped server")
	}
	if p.Term.startErr != remoteServerStoppedError {
		t.Fatalf("terminal did not retain its stopped state: %q", p.Term.startErr)
	}
	a.setHostRunning("box", true)
	if a.remoteTerminalStopped("box") {
		t.Fatal("an explicit start did not release the stopped state")
	}
}

func TestStaleHostStopDoesNotClearNewOperation(t *testing.T) {
	a := &app{workspace: &workspace{}, windowSeq: 2, hostSetupSeq: 5, hostSetupAddr: "new-box", hostSetupBusy: true}
	rc := &remote.Conn{Host: "old-box"}
	a.setRemoteConn(rc)
	a.finishHostStop("old-box", 4, 2, nil)
	if !a.hostSetupBusy {
		t.Fatal("an old sequence cleared the new operation's busy state")
	}
	a.finishHostStop("old-box", 5, 1, nil)
	if !a.hostSetupBusy || a.hostSetupAddr != "new-box" {
		t.Fatal("an old window changed the new operation")
	}
	if a.remoteConn("old-box") != rc {
		t.Fatal("an old stop detached the replacement connection")
	}
}

func TestStaleSetupDoesNotClearNewOperation(t *testing.T) {
	a := &app{workspace: &workspace{}, windowSeq: 2, hostSetupSeq: 4, hostSetupAddr: "new-box", hostSetupBusy: true}
	a.finishHostSetup("old-box", 3, 2, &remote.Setup{}, nil)
	if !a.hostSetupBusy || a.hostSetupAddr != "new-box" || a.hostSetupErr != "" {
		t.Fatalf("stale setup changed current state: busy=%v addr=%q err=%q", a.hostSetupBusy, a.hostSetupAddr, a.hostSetupErr)
	}
}

func TestOldWindowHostCheckDoesNotPublish(t *testing.T) {
	a := &app{workspace: &workspace{}, windowSeq: 2}
	st := a.hostStateFor("buildbox")
	st.checkSeq = 3
	st.checking = true
	a.finishHostCheck("buildbox", 3, 1, true)
	if st.running || !st.checking {
		t.Fatalf("old-window check published: running=%v checking=%v", st.running, st.checking)
	}
}

func TestOnUpdateUIWithoutWindowsDropsProductionCallback(t *testing.T) {
	a := &app{workspace: &workspace{}, native: true}
	ran := false
	a.onUpdateUI(func() { ran = true })
	if ran {
		t.Fatal("a production callback ran directly after every window closed")
	}
	a.native = false
	a.onUpdateUI(func() { ran = true })
	if !ran {
		t.Fatal("the no-window test callback did not run")
	}
}

func TestSettingsOnlyCloseInvalidatesHostOperationsAndAllowsRetry(t *testing.T) {
	oldRunning, oldProvision, oldStop := hostServerRunningFn, provisionRemoteFn, stopRemoteFn
	t.Cleanup(func() {
		hostServerRunningFn, provisionRemoteFn, stopRemoteFn = oldRunning, oldProvision, oldStop
	})
	a := &app{workspace: &workspace{}, windowSeq: 2, hostSetupAddr: "buildbox", hostSetupBusy: true, hostSetupSeq: 4}
	st := a.hostStateFor("buildbox")
	st.checked, st.checking = true, true
	st.checkSeq = 2

	a.settingsWindowClosed()

	if a.hostSetupBusy || a.hostSetupSeq == 4 || a.windowSeq == 2 {
		t.Fatalf("Settings-only close left setup active: busy=%v setupSeq=%d windowSeq=%d", a.hostSetupBusy, a.hostSetupSeq, a.windowSeq)
	}
	if st.checking || st.checked || st.checkSeq == 2 {
		t.Fatalf("Settings-only close left the old check active: checking=%v checked=%v seq=%d", st.checking, st.checked, st.checkSeq)
	}
	hostServerRunningFn = func(context.Context, string) (bool, error) { return true, nil }
	a.checkHostServer("buildbox")
	if st.checking || !st.checked || !st.running {
		t.Fatalf("reopened Settings could not retry its host check: checking=%v checked=%v running=%v", st.checking, st.checked, st.running)
	}
	provisionRemoteFn = func(string, *remote.Conn) (*remote.Setup, error) { return &remote.Setup{}, nil }
	a.setUpHost("buildbox")
	if a.hostSetupBusy || !st.ready {
		t.Fatalf("reopened Settings could not retry setup: busy=%v ready=%v", a.hostSetupBusy, st.ready)
	}
	stopRemoteFn = func(string) error { return nil }
	a.stopHostServer("buildbox")
	if a.hostSetupBusy || !st.stopped {
		t.Fatalf("reopened Settings could not retry stop: busy=%v stopped=%v", a.hostSetupBusy, st.stopped)
	}
}

func TestWindowCloseInvalidatesHostOperations(t *testing.T) {
	a := &app{workspace: &workspace{}, hostSetupAddr: "buildbox", hostSetupBusy: true, hostSetupSeq: 4}
	st := a.hostStateFor("buildbox")
	st.checkSeq = 2
	st.checking = true
	a.windowClosed()
	if a.hostSetupBusy || a.hostSetupSeq == 4 {
		t.Fatalf("window close left setup active: busy=%v seq=%d", a.hostSetupBusy, a.hostSetupSeq)
	}
	if st.checking || st.checkSeq == 2 {
		t.Fatalf("window close left the old check active: checking=%v seq=%d", st.checking, st.checkSeq)
	}
}
