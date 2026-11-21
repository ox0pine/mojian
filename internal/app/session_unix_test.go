//go:build darwin || linux

package app

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"mojian/internal/fs"
	gitpage "mojian/internal/git"
	"mojian/internal/remote"
	"mojian/internal/session"
	"mojian/internal/testutil"

	"github.com/egoist/mygo/ui"
)

// startTestSessionServer starts a server in dir and returns a client and
// the channel its Serve call reports on.
func startTestSessionServer(t *testing.T, dir string) (*session.Client, <-chan error) {
	t.Helper()
	t.Setenv("MOJIAN_DIR", dir)
	done := make(chan error, 1)
	go func() { done <- session.Serve() }()
	deadline := time.Now().Add(3 * time.Second)
	for {
		if _, err := os.Stat(session.SocketPath()); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("session server did not start")
		}
		time.Sleep(10 * time.Millisecond)
	}
	c, err := session.Connect()
	if err != nil {
		t.Fatal(err)
	}
	return c, done
}

func TestRestoredTerminalUsesItsOwnServer(t *testing.T) {
	localDir := t.TempDir()
	oneDir := t.TempDir()
	twoDir := t.TempDir()
	local, localDone := startTestSessionServer(t, localDir)
	defer local.Close()
	one, oneDone := startTestSessionServer(t, oneDir)
	defer one.Close()
	two, twoDone := startTestSessionServer(t, twoDir)
	defer two.Close()

	info, err := two.Create(session.CreateOptions{
		Command: []string{"/bin/sh", "-c", "sleep 5"},
		Cols:    40, Rows: 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	a := &app{
		workspace: &workspace{
			serverAlias: "one",
			remotePath:  "/srv/app",
		},
		sessionClient: local,
	}
	a.setRemoteConn(&remote.Conn{Host: "one", Client: one})
	a.setRemoteConn(&remote.Conn{Host: "two", Client: two})
	p := newTermPane(newTermPaneSpec(termSpec{Server: "two"}, info.ID))
	tb := &tab{ID: nextID("t"), Root: &node{Pane: p}, Focus: p}
	a.tabs = []*tab{tb}
	a.active = 0
	a.startRestoredTerminal(p)
	if p.Term.startErr != "" {
		t.Fatalf("restoring the terminal: %s", p.Term.startErr)
	}
	if p.Term.Term == nil {
		t.Fatal("the terminal was not attached")
	}
	if p.Term.SID != info.ID || p.Term.Client != two {
		t.Fatalf("terminal used %q on %p, want %q on the second server", p.Term.SID, p.Term.Client, info.ID)
	}
	if rc := a.activeRemote(); rc == nil || rc.Host != "one" || a.serverAlias != "one" {
		t.Fatal("restoring a terminal changed the active project's server")
	}
	if p.Term.stream != nil {
		_ = p.Term.stream.Close()
	}
	if p.Term.Term != nil {
		_ = p.Term.Term.Close()
	}
	for _, c := range []*session.Client{local, one, two} {
		if err := c.Shutdown(); err != nil {
			t.Fatal(err)
		}
	}
	for name, done := range map[string]<-chan error{"local": localDone, "one": oneDone, "two": twoDone} {
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			t.Fatalf("%s server did not stop", name)
		}
	}
}

func TestRestoredTerminalUsesTheRemoteServersSocket(t *testing.T) {
	localDir, err := os.MkdirTemp("", "mj-local")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(localDir)
	remoteDir, err := os.MkdirTemp("", "mj-remote")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(remoteDir)

	local, localDone := startTestSessionServer(t, localDir)
	defer local.Close()
	remoteClient, remoteDone := startTestSessionServer(t, remoteDir)
	defer remoteClient.Close()

	info, err := remoteClient.Create(session.CreateOptions{
		Command: []string{"/bin/sh", "-c", "sleep 5"},
		Cols:    40, Rows: 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	a := &app{
		workspace:     &workspace{serverAlias: "remote", remotePath: "/srv/app"},
		sessionClient: local,
	}
	a.setRemoteConn(&remote.Conn{Host: "remote", Client: remoteClient})
	p := newTermPane(newTermPaneSpec(termSpec{Server: "remote"}, info.ID))
	a.startRestoredTerminal(p)
	if p.Term.startErr != "" {
		t.Fatalf("restoring the terminal: %s", p.Term.startErr)
	}
	if p.Term.Term == nil {
		t.Fatal("the terminal was not attached")
	}
	if p.Term.SID != info.ID {
		t.Fatalf("restored a new session %q instead of %q", p.Term.SID, info.ID)
	}
	if p.Term.Client != remoteClient {
		t.Fatal("the terminal attached through the wrong client")
	}
	if p.Term.stream != nil {
		_ = p.Term.stream.Close()
	}
	if p.Term.Term != nil {
		_ = p.Term.Term.Close()
	}

	if err := remoteClient.Shutdown(); err != nil {
		t.Fatal(err)
	}
	if err := local.Shutdown(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-remoteDone:
	case <-time.After(3 * time.Second):
		t.Fatal("remote server did not stop")
	}
	select {
	case <-localDone:
	case <-time.After(3 * time.Second):
		t.Fatal("local server did not stop")
	}
}

func TestTerminalErrorOffersRetry(t *testing.T) {
	a := newProjectApp(t)
	p := newTermPane(newTermPaneSpec(termSpec{}, "missing"))
	p.Term.startErr = "the server is not connected"
	tb := &tab{ID: nextID("t"), Root: &node{Pane: p}, Focus: p}
	a.tabs = append(a.tabs, tb)
	a.active = len(a.tabs) - 1
	tt := ui.NewTester(a.view, 1100, 700)
	tt.Frame()
	if !tt.HasText("Could not open the terminal") || !tt.HasText("Try Again") {
		t.Fatalf("terminal error texts: %q", tt.Texts())
	}
}

func TestTerminalSessionSurvivesReopen(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("MOJIAN_DIR", dir)
	go session.Serve()
	deadline := time.Now().Add(3 * time.Second)
	for {
		if _, err := os.Stat(session.SocketPath()); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("session server did not start")
		}
		time.Sleep(10 * time.Millisecond)
	}
	a := newProjectApp(t)
	a.layoutPath = dir + "/layout.json"
	tt, err := a.startTerminal()
	if err != nil {
		t.Fatal(err)
	}
	a.addTerminalPane(tt)
	a.saveLayout()
	sid := tt.SID
	if sid == "" {
		t.Fatal("no session id")
	}
	closeWorkspaceTerminals(a.workspace)
	_ = a.sessionClient.Close()
	a.sessionClient = nil
	a.tabs = nil
	a.active = -1

	b := &app{workspace: &workspace{store: a.store, layoutPath: a.layoutPath, treeOpen: true}}
	if !b.restoreLayout() {
		t.Fatal("layout did not restore")
	}
	p := b.tab().Focus
	if p == nil || p.Term == nil || p.Term.SID != sid {
		t.Fatalf("restored pane: %+v", p)
	}
	b.startRestoredTerminal(p)
	if p.Term.Term == nil || p.Term.startErr != "" {
		t.Fatalf("reattach failed: %v", p.Term.startErr)
	}
	b.endTerminal(p.Term)
	if b.sessionClient != nil {
		_ = b.sessionClient.Shutdown()
		_ = b.sessionClient.Close()
	}
}

func TestLocalProjectStoreUsesSessionServer(t *testing.T) {
	client, done := startTestSessionServer(t, t.TempDir())
	defer client.Close()
	defer func() { _ = client.Shutdown(); <-done }()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "folder"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "a.md"), []byte("# A\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "folder", "nested.md"), []byte("# Nested\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := openServerStore(client, root, t.TempDir(), true)
	if err != nil {
		t.Fatal(err)
	}
	if s.ByRelPath(filepath.Join("folder", "nested.md")) == nil {
		t.Fatalf("the server listing lost a nested file: %+v", s.Notes)
	}
	if !s.Dirs["folder"] {
		t.Fatal("the server listing lost a folder")
	}
	if s.FS.Kind() != fs.KindServerLocal {
		t.Fatal("the store was not marked as local-server backed")
	}
	n := s.ByRelPath("a.md")
	if n == nil {
		t.Fatalf("the listing has no a.md: %+v", s.Notes)
	}
	if err := s.LoadNote(n); err != nil {
		t.Fatal(err)
	}
	if got := n.Text(); got != "# A\n" {
		t.Fatalf("note text: %q", got)
	}
	n.Buffer().Set("# changed through the server\n")
	if err := s.Save(n); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(root, "a.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "# changed through the server\n" {
		t.Fatalf("server file: %q", data)
	}
	if _, _, err := s.Rename("a.md", "b.md"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "a.md")); err == nil {
		t.Fatal("the old server file remained")
	}
	data, err = os.ReadFile(filepath.Join(root, "b.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "# changed through the server\n" {
		t.Fatalf("renamed file: %q", data)
	}
}

func TestAppOpenProjectUsesLocalServer(t *testing.T) {
	client, done := startTestSessionServer(t, t.TempDir())
	defer client.Close()
	defer func() { _ = client.Shutdown(); <-done }()
	project := t.TempDir()
	if err := os.WriteFile(filepath.Join(project, "a.md"), []byte("# A\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	a := &app{
		workspace:     &workspace{treeOpen: true},
		dataDir:       t.TempDir(),
		sessionClient: client,
	}
	a.openProject(project)
	if a.store == nil || a.store.FS.Kind() != fs.KindServerLocal {
		t.Fatalf("project did not use the local server: %+v", a.store)
	}
	n := a.currentNote()
	if n == nil || n.Rel != "a.md" {
		t.Fatalf("current note: %+v", n)
	}
	n.Buffer().Set("# saved through the local server\n")
	if err := a.saveNow(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(project, "a.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "# saved through the local server\n" {
		t.Fatalf("saved file: %q", data)
	}
}

func TestLocalServerGitRunner(t *testing.T) {
	client, done := startTestSessionServer(t, t.TempDir())
	defer client.Close()
	defer func() { _ = client.Shutdown(); <-done }()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "base.md"), []byte("# Base\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	testutil.InitRepo(t, root)
	s, err := openServerStore(client, root, t.TempDir(), true)
	if err != nil {
		t.Fatal(err)
	}
	a := &app{workspace: &workspace{store: s}}
	if _, ok := a.GitRunner().(serverGitRunner); !ok {
		t.Fatalf("Git runner is %T, want serverGitRunner", a.GitRunner())
	}
	if _, err := s.NewFileIn("", "new.md", "# New\n"); err != nil {
		t.Fatal(err)
	}
	data := gitpage.ReadStatus(a.GitRunner(), root)
	if data.Err != "" {
		t.Fatal(data.Err)
	}
	found := false
	for _, c := range data.Changes {
		if c.Path == "new.md" && c.Status == "U" {
			found = true
		}
	}
	if !found {
		t.Fatalf("local server Git status: %+v", data.Changes)
	}
}

func TestDetachRemoteConnKeepsServer(t *testing.T) {
	client, done := startTestSessionServer(t, t.TempDir())
	defer func() {
		if c, err := session.ConnectAt(session.SocketPath()); err == nil {
			_ = c.Shutdown()
			_ = c.Close()
		}
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			t.Error("session server did not stop")
		}
	}()
	rc := &remote.Conn{Host: "buildbox", Client: client}
	a := &app{workspace: &workspace{}}
	a.detachRemoteConn(rc)
	select {
	case <-client.Closed():
	case <-time.After(time.Second):
		t.Fatal("detaching did not close the control client")
	}
	next, err := session.ConnectAt(session.SocketPath())
	if err != nil {
		t.Fatalf("the server stopped with the connection: %v", err)
	}
	_ = next.Close()
}

func TestLocalServerFileOperations(t *testing.T) {
	client, done := startTestSessionServer(t, t.TempDir())
	defer client.Close()
	defer func() { _ = client.Shutdown(); <-done }()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "folder"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "a.md"), []byte("# A\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := openServerStore(client, root, t.TempDir(), true)
	if err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	copyPath := filepath.Join(outside, "copy.md")
	if err := os.WriteFile(copyPath, []byte("# Copy\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	dest, err := s.CopyInto(copyPath, "")
	if err != nil {
		t.Fatal(err)
	}
	if dest == "" {
		t.Fatal("copyInto returned no path")
	}
	if data, err := os.ReadFile(filepath.Join(root, dest)); err != nil || string(data) != "# Copy\n" {
		t.Fatalf("copied file: %q %v", data, err)
	}
	if _, err := os.Stat(copyPath); err != nil {
		t.Fatal("copyInto removed its source")
	}

	dest, err = s.MoveInto(filepath.Join(root, "a.md"), "folder")
	if err != nil {
		t.Fatal(err)
	}
	if dest == "" {
		t.Fatal("moveInto returned no path")
	}
	if _, err := os.Stat(filepath.Join(root, "a.md")); err == nil {
		t.Fatal("moveInto left the old file")
	}
	if data, err := os.ReadFile(filepath.Join(root, dest)); err != nil || string(data) != "# A\n" {
		t.Fatalf("moved file: %q %v", data, err)
	}

	movePath := filepath.Join(outside, "move.md")
	if err := os.WriteFile(movePath, []byte("# Move\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	dest, err = s.MoveInto(movePath, "")
	if err != nil {
		t.Fatal(err)
	}
	if dest == "" {
		t.Fatal("moveInto returned no path")
	}
	if _, err := os.Stat(movePath); err == nil {
		t.Fatal("moveInto left the outside source")
	}
	if data, err := os.ReadFile(filepath.Join(root, dest)); err != nil || string(data) != "# Move\n" {
		t.Fatalf("moved outside file: %q %v", data, err)
	}
}

// startLocalServerAgain brings up another server in the test binary
// after a test stopped the first one. The app spawns a new process; a
// test binary cannot, so the test starts Serve itself.
func startLocalServerAgain(t *testing.T) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		if _, err := os.Stat(session.SocketPath()); os.IsNotExist(err) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the stopped server did not remove its socket")
		}
		time.Sleep(10 * time.Millisecond)
	}
	time.Sleep(50 * time.Millisecond)
	client, done := startTestSessionServer(t, os.Getenv("MOJIAN_DIR"))
	t.Cleanup(func() {
		_ = client.Close()
		_ = session.Stop()
		<-done
	})
}

func TestLocalServerStopsAndRestarts(t *testing.T) {
	startLocalServerForTest(t)
	t.Cleanup(func() { _ = session.Stop() })
	root := t.TempDir()
	testutil.WriteVault(t, root, map[string]string{"a.md": "# A\n"})
	a := &app{workspace: &workspace{treeOpen: true}, dataDir: t.TempDir()}
	a.project = root
	a.openProject(root)
	if a.store == nil || !a.store.IsServer() {
		t.Fatal("the project is not backed by the local server")
	}
	store := a.store
	if !a.localServerRunning() {
		t.Fatal("the local server is not running")
	}

	a.stopLocalServer()
	if !a.localServerStopped || a.localServerRunning() {
		t.Fatalf("after stop: stopped=%v running=%v", a.localServerStopped, a.localServerRunning())
	}
	if a.sessionClient != nil {
		t.Fatal("the stopped server kept its control client")
	}

	startLocalServerAgain(t)
	a.startLocalServer()
	if a.localServerStopped || !a.localServerRunning() {
		t.Fatalf("after start: stopped=%v running=%v", a.localServerStopped, a.localServerRunning())
	}
	if a.store != store {
		t.Fatal("starting the server replaced the loaded project")
	}
	if err := store.Refresh(); err != nil {
		t.Fatalf("the restarted server did not serve the project: %v", err)
	}
}

func TestLocalServerSettingsButtons(t *testing.T) {
	registerFonts()
	startLocalServerForTest(t)
	t.Cleanup(func() { _ = session.Stop() })
	a := newProjectApp(t)
	a.settings.Cat = settingsHosts
	tt := ui.NewTester(a.settingsWindowView, 900, 1200)
	tt.Frame()
	if !tt.HasText("Stop Server") {
		t.Fatalf("a running local server has no stop button: %q", tt.Texts())
	}
	if err := tt.Click("Stop Server"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !tt.HasText("Stop this server? Its terminals and the processes in them end.") {
		t.Fatalf("stopping the local server has no confirmation: %q", tt.Texts())
	}
	if err := tt.Click("Stop"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !a.localServerStopped {
		t.Fatal("the local server did not stop")
	}
	if !tt.HasText("Start Server") {
		t.Fatalf("a stopped local server has no start button: %q", tt.Texts())
	}
	startLocalServerAgain(t)
	if err := tt.Click("Start Server"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.localServerStopped || !a.localServerRunning() {
		t.Fatalf("the local server did not start: stopped=%v running=%v", a.localServerStopped, a.localServerRunning())
	}
}

func TestServerProjectRefreshAppliesListing(t *testing.T) {
	client, done := startTestSessionServer(t, t.TempDir())
	defer client.Close()
	defer func() { _ = client.Shutdown(); <-done }()
	root := t.TempDir()
	testutil.WriteVault(t, root, map[string]string{"a.md": "# A\n"})
	s, err := openServerStore(client, root, t.TempDir(), true)
	if err != nil {
		t.Fatal(err)
	}
	a := &app{workspace: &workspace{store: s, treeOpen: true, openDirs: map[string]bool{}}, dataDir: t.TempDir()}
	if err := os.WriteFile(filepath.Join(root, "new.md"), []byte("# New\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	a.reloadProject(true)
	if a.reloadStore != nil {
		t.Fatal("the listing stayed in flight")
	}
	if s.ByRelPath("new.md") == nil {
		t.Fatal("the refreshed listing did not reach the store")
	}
}

func TestFocusRefreshIsThrottled(t *testing.T) {
	client, done := startTestSessionServer(t, t.TempDir())
	defer client.Close()
	defer func() { _ = client.Shutdown(); <-done }()
	root := t.TempDir()
	testutil.WriteVault(t, root, map[string]string{"a.md": "# A\n"})
	s, err := openServerStore(client, root, t.TempDir(), true)
	if err != nil {
		t.Fatal(err)
	}
	a := &app{workspace: &workspace{store: s, treeOpen: true, openDirs: map[string]bool{}}, dataDir: t.TempDir()}
	a.reloadProject(true)
	if err := os.WriteFile(filepath.Join(root, "later.md"), []byte("# Later\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	a.reloadProject(false)
	if s.ByRelPath("later.md") != nil {
		t.Fatal("a focus refresh ignored the throttle window")
	}
	a.reloadProject(true)
	if s.ByRelPath("later.md") == nil {
		t.Fatal("the forced refresh did not apply the listing")
	}
}
