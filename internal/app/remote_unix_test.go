//go:build darwin || linux

package app

import (
	"context"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	gitpage "mojian/internal/git"
	"mojian/internal/remote"
	"mojian/internal/session"
	"mojian/internal/testutil"

	"github.com/egoist/mygo/ui"
)

// TestRemoteServerSetupFedora exercises the real upload, server start,
// stdio bridge and file operations against a host named in
// MOJIAN_REMOTE_TEST_HOST. It is skipped unless that variable is set:
//
//	MOJIAN_REMOTE_TEST_HOST=user@host go test ./internal/app -run TestRemoteServerSetupFedora -v -timeout 10m
func TestRemoteServerSetupFedora(t *testing.T) {
	host := os.Getenv("MOJIAN_REMOTE_TEST_HOST")
	if host == "" {
		t.Skip("set MOJIAN_REMOTE_TEST_HOST=user@host to run this integration test")
	}
	first, err := remote.Provision(host, nil)
	if err != nil {
		t.Fatalf("provisioning %s: %v", host, err)
	}
	if first.Protocol != session.ProtocolVersion {
		t.Fatalf("server protocol %d, want %d", first.Protocol, session.ProtocolVersion)
	}
	if first.Conn == nil {
		t.Fatal("provisioning returned no connection")
	}
	defer remote.Close(first.Conn)
	second, err := remote.Provision(host, first.Conn)
	if err != nil {
		t.Fatalf("checking %s twice: %v", host, err)
	}
	if second.Uploaded {
		t.Fatal("the second setup uploaded the binaries again")
	}
	if second.Conn != first.Conn {
		t.Fatal("the second setup did not reuse the live connection")
	}

	rc, err := remote.Dial(host)
	if err != nil {
		t.Fatalf("connecting to %s: %v", host, err)
	}
	defer remote.Close(rc)

	root := "/tmp/mojian-test-" + strconv.Itoa(os.Getpid())
	defer func() { _, _ = rc.Client.Exec("", []string{"rm", "-rf", root}, nil, 10*time.Second) }()
	if err := rc.Client.Mkdir(root); err != nil {
		t.Fatal(err)
	}
	s, err := openServerStore(rc.Client, root, t.TempDir(), false)
	if err != nil {
		t.Fatalf("opening the server project: %v", err)
	}
	n, err := s.NewFileIn("", "fedora.md", "# Fedora\n\nUploaded by the app.\n")
	if err != nil {
		t.Fatalf("creating a file: %v", err)
	}
	n.Buffer().Set("# Fedora\n\nSaved through the server.\n")
	if err := s.Save(n); err != nil {
		t.Fatalf("saving: %v", err)
	}
	data, err := rc.Client.ReadFile(path.Join(root, "fedora.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "# Fedora\n\nSaved through the server.\n" {
		t.Fatalf("remote file: %q", data)
	}
	if _, _, err := s.Rename("fedora.md", "renamed.md"); err != nil {
		t.Fatalf("renaming: %v", err)
	}
	if s.ByRelPath("renamed.md") == nil {
		t.Fatal("the renamed file is missing from the store")
	}
	if _, err := rc.Client.Stat(path.Join(root, "fedora.md")); err == nil {
		t.Fatal("the old remote file remained")
	}
	if err := s.Refresh(); err != nil {
		t.Fatalf("refreshing: %v", err)
	}
	if s.ByRelPath("renamed.md") == nil {
		t.Fatal("the server listing lost the renamed file")
	}
	pic := []byte("\x89PNG\r\n\x1a\nremote-asset")
	if err := rc.Client.WriteFile(path.Join(root, "pic.png"), pic); err != nil {
		t.Fatal(err)
	}
	if err := s.Refresh(); err != nil {
		t.Fatal(err)
	}
	asset := s.AssetByRel("pic.png")
	if asset == nil || asset.Hash == "" {
		t.Fatalf("remote asset listing: %+v", asset)
	}
	local, err := s.MaterializeAsset(asset)
	if err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(local); err != nil || string(data) != string(pic) {
		t.Fatalf("remote asset cache: %q, %v", data, err)
	}
}

// The loading card cancels its task context when the connection lands;
// the live SSH connection must outlive that context.
func TestRemoteConnectionSurvivesItsTaskContext(t *testing.T) {
	host := os.Getenv("MOJIAN_REMOTE_TEST_HOST")
	if host == "" {
		t.Skip("set MOJIAN_REMOTE_TEST_HOST=user@host to run this integration test")
	}
	ctx, cancel := context.WithCancel(context.Background())
	rc, err := remote.DialContext(ctx, host)
	if err != nil {
		t.Fatalf("dialing %s: %v", host, err)
	}
	defer remote.Close(rc)
	cancel()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if !rc.Healthy() {
			t.Fatal("the SSH connection died with the task context")
		}
		time.Sleep(20 * time.Millisecond)
	}
	if _, err := rc.Client.List(); err != nil {
		t.Fatalf("the connection stopped working: %v", err)
	}
}

// newRemoteApp opens root through a local session server, as a remote
// project would through its connection.
func newRemoteApp(t *testing.T, client *session.Client, root string) *app {
	t.Helper()
	s, err := openServerStore(client, root, t.TempDir(), false)
	if err != nil {
		t.Fatal(err)
	}
	a := &app{
		workspace: &workspace{
			store: s, treeOpen: true, openDirs: map[string]bool{},
			serverAlias: "buildbox", remotePath: root,
		},
	}
	a.setRemoteConn(&remote.Conn{Host: "buildbox", Client: client})
	n := s.ByRelPath("a.md")
	if n == nil {
		t.Fatalf("the listing has no a.md: %+v", s.Notes)
	}
	a.openNote(n)
	if a.currentNote() == nil {
		t.Fatal("the remote note did not open")
	}
	return a
}

func TestRemoteProjectReadsAndSavesOnTheServer(t *testing.T) {
	client, done := startTestSessionServer(t, t.TempDir())
	defer client.Close()
	defer func() { _ = client.Shutdown(); <-done }()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.md"), []byte("# A\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	a := newRemoteApp(t, client, root)
	if got := a.currentNote().Text(); got != "# A\n" {
		t.Fatalf("note text: %q", got)
	}
	a.currentNote().Buffer().Set("# changed on the server\n")
	if err := a.saveNow(); err != nil {
		t.Fatalf("save: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(root, "a.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "# changed on the server\n" {
		t.Fatalf("server file: %q", data)
	}
}

func TestRemoteProjectSavesBeforeLeaving(t *testing.T) {
	client, done := startTestSessionServer(t, t.TempDir())
	defer client.Close()
	defer func() { _ = client.Shutdown(); <-done }()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.md"), []byte("# A\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	a := newRemoteApp(t, client, root)
	a.currentNote().Buffer().Set("# saved before switch\n")
	target := t.TempDir()
	a.openProject(target)
	if a.remotePath != "" || a.activeRemote() != nil {
		t.Fatalf("the remote project stayed open: %q %+v", a.remotePath, a.activeRemote())
	}
	data, err := os.ReadFile(filepath.Join(root, "a.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "# saved before switch\n" {
		t.Fatalf("server file after switching: %q", data)
	}
}

func TestFailedRemoteSaveKeepsTheNoteDirty(t *testing.T) {
	client, done := startTestSessionServer(t, t.TempDir())
	defer func() { _ = client.Shutdown(); <-done }()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.md"), []byte("# A\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	a := newRemoteApp(t, client, root)
	n := a.currentNote()
	n.Buffer().Set("# unsaved\n")
	_ = client.Close()
	if err := a.saveNow(); err == nil {
		t.Fatal("saveNow hid a lost remote connection")
	}
	if !n.Dirty() {
		t.Fatal("a failed remote save marked the note saved")
	}
	if c, err := session.ConnectAt(session.SocketPath()); err == nil {
		_ = c.Shutdown()
		_ = c.Close()
	}
}

func TestRemoteProjectRenamesOnTheServer(t *testing.T) {
	client, done := startTestSessionServer(t, t.TempDir())
	defer client.Close()
	defer func() { _ = client.Shutdown(); <-done }()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.md"), []byte("# A\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	a := newRemoteApp(t, client, root)
	if _, _, err := a.store.Rename("a.md", "b.md"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "a.md")); err == nil {
		t.Fatal("the old remote file remained")
	}
	data, err := os.ReadFile(filepath.Join(root, "b.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "# A\n" {
		t.Fatalf("renamed file: %q", data)
	}
	if a.store.ByRelPath("b.md") == nil || a.store.ByRelPath("a.md") != nil {
		t.Fatalf("notes after rename: %+v", a.store.Notes)
	}
}

func TestRemoteGitRunsOnTheServer(t *testing.T) {
	client, done := startTestSessionServer(t, t.TempDir())
	defer client.Close()
	defer func() { _ = client.Shutdown(); <-done }()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.md"), []byte("# A\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := client.Exec(root, []string{"git", "-C", root, "init", "-q"}, nil, 5*time.Second); err != nil {
		t.Fatal(err)
	}
	runner := serverGitRunner{client: client}
	data := gitpage.ReadStatus(runner, root)
	if data.Err != "" || len(data.Changes) != 1 || data.Changes[0].Status != "U" {
		t.Fatalf("remote status: %+v", data)
	}
	diff, err := gitpage.ReadDiff(runner, data.Root, data.Changes[0])
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(diff, "new file") || !strings.Contains(diff, "+# A") {
		t.Fatalf("remote untracked diff: %q", diff)
	}
}

func TestRemoteGitRealPathPreservesTrailingSpace(t *testing.T) {
	client, done := startTestSessionServer(t, t.TempDir())
	defer client.Close()
	defer func() { _ = client.Shutdown(); <-done }()
	root := filepath.Join(t.TempDir(), "project ")
	if err := os.Mkdir(root, 0o755); err != nil {
		t.Fatal(err)
	}
	want, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	runner := serverGitRunner{client: client}
	got, err := runner.RealPath(root)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("real path = %q, want %q", got, want)
	}
	if _, err := runner.RealPath(filepath.Join(root, "missing")); err == nil {
		t.Fatal("a failed pwd was reported as a valid path")
	}
}

func TestRemoteBinaryListsWithoutMaterializing(t *testing.T) {
	client, done := startTestSessionServer(t, t.TempDir())
	stopped := false
	defer func() {
		if !stopped {
			_ = client.Shutdown()
			select {
			case <-done:
			case <-time.After(3 * time.Second):
				t.Error("session server did not stop")
			}
		}
		_ = client.Close()
	}()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "model.pt"), []byte{0, 1, 2}, 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := openServerStore(client, root, t.TempDir(), false)
	if err != nil {
		t.Fatal(err)
	}
	asset := s.AssetByRel("model.pt")
	if asset == nil || !asset.IsBinary() {
		t.Fatalf("remote binary listing: %+v", asset)
	}
	if err := client.Shutdown(); err != nil {
		t.Fatal(err)
	}
	<-done
	stopped = true
	path, err := s.MaterializeAsset(asset)
	if err != nil || path != asset.Path || asset.Local != "" {
		t.Fatalf("binary metadata required a server read: path=%q local=%q err=%v", path, asset.Local, err)
	}
}

func TestRemoteAssetCacheUsesContentHash(t *testing.T) {
	client, done := startTestSessionServer(t, t.TempDir())
	defer client.Close()
	defer func() { _ = client.Shutdown(); <-done }()
	root := t.TempDir()
	path := filepath.Join(root, "pic.png")
	first := []byte("\x89PNG\r\n\x1a\nfirst")
	second := []byte("\x89PNG\r\n\x1a\nother")
	if len(first) != len(second) {
		t.Fatal("fixture lengths differ")
	}
	if err := os.WriteFile(path, first, 0o644); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	cache := t.TempDir()
	s, err := openServerStore(client, root, cache, false)
	if err != nil {
		t.Fatal(err)
	}
	asset := s.AssetByRel("pic.png")
	if asset == nil || asset.Hash == "" {
		t.Fatalf("asset listing: %+v", asset)
	}
	firstPath, err := s.MaterializeAsset(asset)
	if err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(firstPath); err != nil || string(data) != string(first) {
		t.Fatalf("first cache: %q, %v", data, err)
	}
	// Same size and timestamp, different bytes.
	if err := os.WriteFile(path, second, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, info.ModTime(), info.ModTime()); err != nil {
		t.Fatal(err)
	}
	if err := s.Refresh(); err != nil {
		t.Fatal(err)
	}
	next := s.AssetByRel("pic.png")
	if next == nil || next.Hash == "" || next.Hash == asset.Hash {
		t.Fatalf("refreshed asset: %+v", next)
	}
	secondPath, err := s.MaterializeAsset(next)
	if err != nil {
		t.Fatal(err)
	}
	if secondPath == firstPath {
		t.Fatal("the cache key did not change with the content")
	}
	if data, err := os.ReadFile(secondPath); err != nil || string(data) != string(second) {
		t.Fatalf("second cache: %q, %v", data, err)
	}
}

func TestRemoteProjectCacheSwitchKeepsStoresAndConnection(t *testing.T) {
	client, done := startTestSessionServer(t, t.TempDir())
	defer client.Close()
	defer func() { _ = client.Shutdown(); <-done }()
	rootA, rootB := t.TempDir(), t.TempDir()
	for _, root := range []string{rootA, rootB} {
		if err := os.WriteFile(filepath.Join(root, "a.md"), []byte("# A\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	sA, err := openServerStore(client, rootA, t.TempDir(), false)
	if err != nil {
		t.Fatal(err)
	}
	sB, err := openServerStore(client, rootB, t.TempDir(), false)
	if err != nil {
		t.Fatal(err)
	}
	keyA := workspaceKeyRemote("buildbox", rootA)
	keyB := workspaceKeyRemote("buildbox", rootB)
	wsA := &workspace{key: keyA, store: sA, serverAlias: "buildbox", remotePath: rootA, openDirs: map[string]bool{}}
	wsB := &workspace{key: keyB, store: sB, serverAlias: "buildbox", remotePath: rootB, openDirs: map[string]bool{}}
	rc := &remote.Conn{Host: "buildbox", Client: client}
	a := &app{
		workspace:  wsA,
		workspaces: map[string]*workspace{keyA: wsA, keyB: wsB},
	}
	a.setRemoteConn(rc)
	a.openRemoteProjectAsync("buildbox", rootB)
	if a.workspace != wsB || a.store != sB {
		t.Fatal("switching to the cached project reopened it")
	}
	if a.remoteConn("buildbox") != rc {
		t.Fatal("switching projects dropped the connection")
	}
	a.openRemoteProjectAsync("buildbox", rootA)
	if a.workspace != wsA || a.store != sA {
		t.Fatal("switching back reopened the project")
	}
	if a.remoteConn("buildbox") != rc {
		t.Fatal("switching back dropped the connection")
	}
}

// Switching to Local and back used to show the folder picker although the
// remote workspace, its tabs and its connection were still loaded.
func TestServerRowResumesLoadedProject(t *testing.T) {
	registerFonts()
	startLocalServerForTest(t)
	local := t.TempDir()
	remoteRoot := t.TempDir()
	testutil.WriteVault(t, local, map[string]string{"local.md": "# Local\n"})
	testutil.WriteVault(t, remoteRoot, map[string]string{"server.md": "# Server\n"})

	client, err := session.Connect()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	rs, err := openServerStore(client, remoteRoot, t.TempDir(), false)
	if err != nil {
		t.Fatal(err)
	}
	key := workspaceKeyRemote("buildbox", remoteRoot)
	ws := &workspace{
		key:         key,
		project:     local,
		store:       rs,
		serverAlias: "buildbox",
		remotePath:  remoteRoot,
		layoutPath:  "",
		treeOpen:    true,
		active:      -1,
		openDirs:    map[string]bool{},
	}
	a := &app{workspace: ws, dataDir: t.TempDir()}
	a.workspaces = map[string]*workspace{key: ws}
	a.projects = []string{local}
	a.project = local
	a.rememberRemoteProject(remoteProject{Host: "buildbox", Path: remoteRoot})
	a.setRemoteConn(&remote.Conn{Host: "buildbox", Client: client})
	a.keepRemote("buildbox")
	a.addHost("buildbox", "buildbox")
	a.remoteHomes = map[string]string{"buildbox": filepath.Dir(remoteRoot)}
	n := rs.ByRelPath("server.md")
	if n == nil {
		t.Fatalf("the server listing has no server.md: %+v", rs.Notes)
	}
	a.openNote(n)
	if a.currentNote() != n {
		t.Fatal("the server note did not open")
	}

	tt := ui.NewTester(a.view, 1100, 700)
	tt.Frame()
	if err := tt.Click("Switch server: buildbox"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if err := tt.Click("Local"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	tt.Frame()
	if a.store == rs || a.remotePath != "" {
		t.Fatalf("switch to Local kept the server project: path=%q", a.remotePath)
	}
	if a.workspaces[key] != ws {
		t.Fatal("switching to Local dropped the loaded server workspace")
	}

	if err := tt.Click("Switch server: Local"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if err := tt.Click("buildbox"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	tt.Frame()

	if a.remotePicker {
		t.Fatal("choosing a connected server opened the folder picker")
	}
	if a.serverAlias != "buildbox" || a.remotePath != remoteRoot {
		t.Fatalf("server=%q path=%q, want the loaded project", a.serverAlias, a.remotePath)
	}
	if a.workspace != ws || a.store != rs {
		t.Fatal("the loaded workspace was not restored")
	}
	if a.currentNote() != n {
		t.Fatal("the restored workspace lost its open note")
	}
}
