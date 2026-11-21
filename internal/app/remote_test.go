package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"mojian/internal/fs"
	"mojian/internal/remote"
	"mojian/internal/session"

	"github.com/egoist/mygo/ui"
)

func TestRemoteBusyCoversTheWindow(t *testing.T) {
	registerFonts()
	a := newProjectApp(t)
	a.remoteBusy = true
	a.remoteBusyMsg = "Connecting to Buildbox…"
	tt := ui.NewTester(a.view, 1100, 700)
	tt.Frame()
	if !tt.HasText("Connecting to a server") {
		t.Fatalf("loading text: %q", tt.Texts())
	}
	if !tt.HasText("Connecting to Buildbox…") {
		t.Fatalf("loading detail: %q", tt.Texts())
	}
	if !tt.HasText("Cancel") {
		t.Fatalf("no cancel button: %q", tt.Texts())
	}
}

func TestRemoteBusyCancelStopsTask(t *testing.T) {
	registerFonts()
	a := newProjectApp(t)
	seq, _, ctx := a.beginRemoteTask("buildbox", "Connecting to Buildbox…")
	tt := ui.NewTester(a.view, 1100, 700)
	tt.Frame()
	if err := tt.Click("Cancel"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.remoteBusy {
		t.Fatal("Cancel left the loading card up")
	}
	select {
	case <-ctx.Done():
	default:
		t.Fatal("Cancel did not stop the connection task")
	}
	if a.endRemoteTask(seq) {
		t.Fatal("a cancelled task's result still landed")
	}
	if a.remoteTask.cancel != nil {
		t.Fatal("Cancel left the task's cancel function behind")
	}
}

func TestClosingTheWindowCancelsTheRemoteTask(t *testing.T) {
	a := newProjectApp(t)
	seq, _, ctx := a.beginRemoteTask("buildbox", "Reconnecting to Buildbox…")
	a.windowClosed()
	if a.remoteBusy {
		t.Fatal("the closed window left its loading card on")
	}
	select {
	case <-ctx.Done():
	default:
		t.Fatal("the window close did not stop the connection task")
	}
	if a.endRemoteTask(seq) {
		t.Fatal("the closed window's task can still publish its result")
	}
}

func TestRemotePickerReusesConnectionAndHome(t *testing.T) {
	if !remoteSupported() {
		t.Skip("remote projects are unavailable")
	}
	startLocalServerForTest(t)
	a := newProjectApp(t)
	client, err := session.Connect()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	a.setRemoteConn(&remote.Conn{Host: "buildbox", Client: client})
	a.keepRemote("buildbox")
	a.remoteHomes = map[string]string{"buildbox": t.TempDir()}

	a.openRemotePicker("buildbox")
	if a.remoteBusy {
		t.Fatal("a kept connection still used the loading card")
	}
	if !a.remotePicker || a.remotePickHost != "buildbox" || a.remoteHost != "buildbox" {
		t.Fatalf("picker: open=%v host=%q shown=%q", a.remotePicker, a.remotePickHost, a.remoteHost)
	}
}

func TestRemotePickerScrollsItsFolders(t *testing.T) {
	registerFonts()
	a := newProjectApp(t)
	a.remotePicker = true
	a.remoteHost = "Buildbox"
	a.remoteQuery = "/home/me"
	for i := 0; i < 30; i++ {
		name := "Folder" + string(rune('A'+i%26)) + string(rune('0'+i/26))
		a.remoteDirs = append(a.remoteDirs, session.FileEntry{Rel: "/home/me/" + name, Dir: true})
	}
	tt := ui.NewTester(a.view, 900, 640)
	tt.Frame()
	before, ok := tt.Find("Open /home/me/FolderF0")
	if !ok {
		t.Fatal("no folder row")
	}
	tt.Scroll(450, 400, 0, 240)
	tt.Frame()
	after, ok := tt.Find("Open /home/me/FolderF0")
	if !ok {
		t.Fatal("the folder row disappeared")
	}
	if after.Y >= before.Y {
		t.Fatalf("the list did not scroll: before y=%.1f after y=%.1f", before.Y, after.Y)
	}
}

func poolApp() *app {
	a := &app{workspace: &workspace{}, workspaces: map[string]*workspace{}}
	a.setRemoteConn(&remote.Conn{Host: "box"})
	return a
}

func TestKeptConnectionSurvivesReapAndPicker(t *testing.T) {
	a := poolApp()
	a.keepRemote("box")
	a.reapRemotes()
	if a.remoteConn("box") == nil {
		t.Fatal("a kept connection was released by reaping")
	}

	a.remotePickHost = "box"
	a.dropPickerRemote()
	if a.remoteConn("box") == nil {
		t.Fatal("closing the picker released a kept connection")
	}
	if a.remotePickHost != "" {
		t.Fatalf("the picker host stayed: %q", a.remotePickHost)
	}

	delete(a.remoteKeep, "box")
	a.reapRemotes()
	if a.remoteConn("box") != nil {
		t.Fatal("the connection stayed after its last project closed")
	}
}

func TestRemotePoolReleasesTheLastUser(t *testing.T) {
	a := poolApp()
	a.reapRemotes()
	if a.remoteConn("box") != nil {
		t.Fatal("an unused connection stayed in the pool")
	}

	a.workspaces["project"] = &workspace{serverAlias: "box", remotePath: "/srv/app"}
	a.setRemoteConn(&remote.Conn{Host: "box"})
	a.reapRemotes()
	if a.remoteConn("box") == nil {
		t.Fatal("a project's connection was released")
	}

	// A terminal alone still holds the connection.
	p := newTermPane(newTermPaneSpec(termSpec{Server: "box"}, "sid"))
	a.workspaces = map[string]*workspace{
		"terminal": {tabs: []*tab{{ID: nextID("t"), Root: &node{Pane: p}, Focus: p}}},
	}
	a.reapRemotes()
	if a.remoteConn("box") == nil {
		t.Fatal("a terminal's connection was released")
	}

	// The picker, while open, is a user too.
	a.workspaces = map[string]*workspace{}
	a.remotePickHost = "box"
	a.reapRemotes()
	if a.remoteConn("box") == nil {
		t.Fatal("the picker's connection was released")
	}
	a.remotePickHost = ""
	a.reapRemotes()
	if a.remoteConn("box") != nil {
		t.Fatal("the last-user release did not close the connection")
	}
}

func TestPickerConnectionSurvivesPendingOpen(t *testing.T) {
	a := poolApp()
	a.setRemoteConn(&remote.Conn{Host: "box"})
	a.remotePickHost = "box"
	a.remoteOpening = true
	a.dropPickerRemote()
	if a.remoteConn("box") == nil {
		t.Fatal("the picker connection was dropped while a project was opening")
	}
	if a.remotePickHost != "box" {
		t.Fatal("the pending picker host was cleared")
	}
	a.remoteOpening = false
	a.dropPickerRemote()
	if a.remoteConn("box") != nil {
		t.Fatal("the picker connection stayed after the pending open ended")
	}
}

func TestInstallRemoteConnPatchesCachedStores(t *testing.T) {
	oldClient, newClient := &session.Client{}, &session.Client{}
	s := &store{FS: fs.NewServer(oldClient, "/srv/app", "", false)}
	p := newTermPane(newTermPaneSpec(termSpec{Server: "box"}, "sid"))
	p.Term.Client = oldClient
	p.Term.startErr = "lost"
	ws := &workspace{
		serverAlias: "box", remotePath: "/srv/app", store: s,
		tabs: []*tab{{ID: nextID("t"), Root: &node{Pane: p}, Focus: p}},
	}
	a := &app{
		workspace:  &workspace{},
		workspaces: map[string]*workspace{"box": ws},
	}
	rc := &remote.Conn{Host: "box", Client: newClient}
	a.installRemoteConn(rc)
	if s.Client() != newClient {
		t.Fatal("the cached store kept its old client")
	}
	if p.Term.Client != newClient || p.Term.startErr != "" {
		t.Fatal("the cached terminal kept its old client")
	}
	if a.remoteConn("box") != rc {
		t.Fatal("the connection was not pooled")
	}
}

func TestTerminalWaitStateFollowsTheConnection(t *testing.T) {
	p := &pane{Term: &termTab{Spec: termSpec{Server: "box"}, connecting: true}}
	tb := &tab{Root: &node{Pane: p}, Focus: p}
	ws := &workspace{serverAlias: "box", remotePath: "/srv/app", tabs: []*tab{tb}}
	a := &app{workspace: ws, workspaces: map[string]*workspace{"remote:box:/srv/app": ws}}

	detachTerminalsOn(ws, "box", nil)
	if p.Term.connecting || p.Term.startErr != "" {
		t.Fatalf("a new connection did not reset the terminal: connecting=%v err=%q", p.Term.connecting, p.Term.startErr)
	}

	p.Term.connecting = true
	a.failTerminalsOn("box", errors.New("unreachable"))
	if p.Term.connecting {
		t.Fatal("a failed reconnect left the terminal waiting forever")
	}
	if p.Term.startErr != "unreachable" {
		t.Fatalf("a failed reconnect did not surface its error: %q", p.Term.startErr)
	}
}

func TestHostEditMovesTerminalsToNewAddress(t *testing.T) {
	p := newTermPane(newTermPaneSpec(termSpec{Server: "old-box"}, "session-1"))
	p.Term.Client = &session.Client{}
	p.Term.startErr = "disconnected"
	tb := &tab{ID: nextID("t"), Root: &node{Pane: p}, Focus: p}
	key := workspaceKeyRemote("old-box", "/srv/app")
	ws := &workspace{key: key, serverAlias: "old-box", remotePath: "/srv/app", tabs: []*tab{tb}}
	a := &app{
		workspace:    ws,
		workspaces:   map[string]*workspace{key: ws},
		settingsPath: filepath.Join(t.TempDir(), "settings.json"),
		remoteKeep:   map[string]bool{"old-box": true},
		remoteHomes:  map[string]string{"old-box": "/home/me"},
		hostEditAddr: "old-box",
	}
	a.hostService().AddHost("Old", "old-box")
	a.hostService().RememberProject(remoteProject{Host: "old-box", Path: "/srv/app"})
	a.hostEditAddress, a.hostEditName = "new-box", "New"

	a.saveHostEdit("old-box")

	if p.Term.Spec.Server != "new-box" || p.Term.Client != nil {
		t.Fatalf("terminal stayed on the old address: server=%q client=%p", p.Term.Spec.Server, p.Term.Client)
	}
	if p.Term.startErr != "" || p.Term.connecting {
		t.Fatalf("terminal did not reset for reconnect: err=%q connecting=%v", p.Term.startErr, p.Term.connecting)
	}
	if ws.serverAlias != "new-box" || a.workspaces[workspaceKeyRemote("new-box", "/srv/app")] != ws {
		t.Fatalf("workspace was not rekeyed: alias=%q key=%q old=%p new=%p ws=%p", ws.serverAlias, ws.key, a.workspaces[key], a.workspaces[workspaceKeyRemote("new-box", "/srv/app")], ws)
	}
	newClient := &session.Client{}
	a.installRemoteConn(&remote.Conn{Host: "new-box", Client: newClient})
	if p.Term.Client != newClient {
		t.Fatal("the terminal did not receive the new connection")
	}
}

func TestHostEditMovesLayoutToNewAddress(t *testing.T) {
	fixture := newProjectApp(t)
	tb := fixture.tab()
	dataDir := t.TempDir()
	key := workspaceKeyRemote("old-box", "/srv/app")
	ws := &workspace{
		key: key, store: fixture.store, serverAlias: "old-box", remotePath: "/srv/app",
		tabs: []*tab{tb}, active: 0,
	}
	a := &app{
		workspace: ws, workspaces: map[string]*workspace{key: ws}, dataDir: dataDir,
		settingsPath: filepath.Join(dataDir, "settings.json"), hostEditAddr: "old-box",
	}
	ws.layoutPath = a.projectLayoutPath("remote:old-box:/srv/app")
	a.hostService().AddHost("Old", "old-box")
	a.saveLayout()
	oldLayout := ws.layoutPath
	newLayout := a.projectLayoutPath("remote:new-box:/srv/app")
	if err := os.MkdirAll(filepath.Dir(newLayout), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(newLayout, []byte(`{"tabs":[],"active":0}`), 0o644); err != nil {
		t.Fatal(err)
	}
	a.hostEditAddress, a.hostEditName = "new-box", "New"

	a.saveHostEdit("old-box")

	if ws.layoutPath != newLayout {
		t.Fatalf("layout path = %q, want new host path %q", ws.layoutPath, newLayout)
	}
	if _, err := os.Stat(oldLayout); !os.IsNotExist(err) {
		t.Fatalf("old layout still exists: %v", err)
	}
	b := &app{workspace: &workspace{
		store: ws.store, serverAlias: "new-box", remotePath: "/srv/app", layoutPath: newLayout,
	}}
	if !b.restoreLayout() {
		t.Fatal("the renamed host could not restore its layout")
	}
	if len(b.tabs) != 1 || len(b.tabs[0].panes()) != 1 {
		t.Fatalf("restored layout has unexpected tabs: %+v", b.tabs)
	}
	if got, want := b.tabs[0].panes()[0].Note.Rel, tb.Focus.Note.Rel; got != want {
		t.Fatalf("restored note = %q, want %q", got, want)
	}
}

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

func TestSSHConfigImportInSettings(t *testing.T) {
	registerFonts()
	if !remoteSupported() {
		t.Skip("remote hosts unsupported on this platform")
	}
	cfg := filepath.Join(t.TempDir(), "config")
	if err := os.WriteFile(cfg, []byte("Host *\n  User root\nHost alpha\n  HostName 10.0.0.1\nHost beta\n  HostName 10.0.0.2\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	orig := sshConfigPathFn
	sshConfigPathFn = func() string { return cfg }
	defer func() { sshConfigPathFn = orig }()

	a := newProjectApp(t)
	a.settingsPath = filepath.Join(t.TempDir(), "settings.json")
	a.settings.Cat = settingsHosts
	a.addHost("existing", "alpha")

	a.openSSHConfigImport()
	if !a.sshImportOpen || len(a.sshImportCandidates) != 1 || a.sshImportCandidates[0].Address != "beta" {
		t.Fatalf("candidates = %+v, open=%v", a.sshImportCandidates, a.sshImportOpen)
	}

	tt := ui.NewTester(a.settingsWindowView, 900, 640)
	tt.Frame()
	if !tt.HasText("Hosts from ~/.ssh/config") || !tt.HasText("beta") {
		t.Fatalf("expected import panel in Settings > Hosts, texts=%q", tt.Texts())
	}

	a.importAllSSHHosts()
	if a.sshImportOpen || len(a.hosts()) != 2 {
		t.Fatalf("after import: open=%v hosts=%+v", a.sshImportOpen, a.hosts())
	}
}
