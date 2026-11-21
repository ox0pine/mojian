//go:build darwin || linux

package session

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func startOpsServer(t *testing.T) (*Client, string) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("MOJIAN_DIR", dir)
	done := make(chan error, 1)
	go func() { done <- Serve() }()
	deadline := time.Now().Add(3 * time.Second)
	for {
		if _, err := os.Stat(SocketPath()); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("session server did not start")
		}
		time.Sleep(10 * time.Millisecond)
	}
	c, err := ConnectAt(SocketPath())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = c.Shutdown()
		_ = c.Close()
		select {
		case <-done:
		case <-time.After(3 * time.Second):
		}
	})
	return c, dir
}

func TestExecRunsCommandsWithOutputAndCode(t *testing.T) {
	c, _ := startOpsServer(t)
	dir := t.TempDir()
	res, err := c.Exec(dir, []string{"sh", "-c", "printf out; printf err >&2; exit 3"}, nil, 5*time.Second)
	if err != nil {
		t.Fatalf("exec: %v", err)
	}
	if res.Stdout != "out" || res.Stderr != "err" || res.Code != 3 {
		t.Fatalf("result: %+v", res)
	}
}

func TestExecDropsInheritedGitVariables(t *testing.T) {
	t.Setenv("GIT_DIR", "/tmp/not-this-repository")
	c, _ := startOpsServer(t)
	res, err := c.Exec(t.TempDir(), []string{"sh", "-c", `printf %s "$GIT_DIR"`}, nil, 5*time.Second)
	if err != nil {
		t.Fatalf("exec: %v", err)
	}
	if res.Stdout != "" {
		t.Fatalf("GIT_DIR leaked: %q", res.Stdout)
	}
}

func TestExecTimesOut(t *testing.T) {
	c, _ := startOpsServer(t)
	if _, err := c.Exec(t.TempDir(), []string{"sh", "-c", "sleep 5"}, nil, 150*time.Millisecond); err == nil {
		t.Fatal("a timed out command returned no error")
	}
}

func TestWriteFileAtomicKeepsMode(t *testing.T) {
	path := filepath.Join(t.TempDir(), "run.sh")
	if err := os.WriteFile(path, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := writeFileAtomic(path, []byte("new")); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o755 {
		t.Fatalf("mode: %v", info.Mode().Perm())
	}
	data, _ := os.ReadFile(path)
	if string(data) != "new" {
		t.Fatalf("content: %q", data)
	}
}

func TestKillEndsTheWholeProcessGroup(t *testing.T) {
	c, dir := startOpsServer(t)
	pidFile := filepath.Join(dir, "child.pid")
	info, err := c.Create(CreateOptions{
		Command: []string{"sh", "-c", "sleep 60 & echo $! > " + pidFile + "; wait"},
		Dir:     dir,
		Cols:    40, Rows: 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	var child int
	deadline := time.Now().Add(3 * time.Second)
	for {
		data, err := os.ReadFile(pidFile)
		if err == nil && strings.TrimSpace(string(data)) != "" {
			child, err = strconv.Atoi(strings.TrimSpace(string(data)))
			if err != nil {
				t.Fatal(err)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the child sleep did not start")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err := c.Kill(info.ID); err != nil {
		t.Fatal(err)
	}
	// The child lives in the shell's process group; the group signal must
	// reach it, not only the shell.
	deadline = time.Now().Add(3 * time.Second)
	for {
		err := syscall.Kill(child, 0)
		if err != nil {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("child process %d survived the session kill", child)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestSlowAttachedStreamIsRemoved(t *testing.T) {
	ss, err := newSession("slow-attach", CreateOptions{
		Command: []string{"sh", "-c", "while :; do printf x; done"},
		Cols:    40, Rows: 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ss.kill()
		select {
		case <-ss.done:
		case <-time.After(3 * time.Second):
			t.Fatal("overflow session did not stop")
		}
	})
	ma, mb := newTestMuxes(t)
	serverStream := ma.openStream(1)
	_ = mb.openStream(1)
	go ss.attach(serverStream, 40, 10)

	deadline := time.Now().Add(3 * time.Second)
	attached := false
	for {
		ss.mu.Lock()
		clients := len(ss.clients)
		ss.mu.Unlock()
		attached = attached || clients > 0
		if attached && clients == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("slow attachment leaked %d client references", clients)
		}
		time.Sleep(time.Millisecond)
	}
	select {
	case <-ma.quit:
		t.Fatal("slow attachment closed the whole mux")
	default:
	}
}

func TestListTreeHashesAssetsByContent(t *testing.T) {
	c, dir := startOpsServer(t)
	root := filepath.Join(dir, "project")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
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
	entries, err := c.ListTree(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Hash == "" {
		t.Fatalf("asset hash: %+v", entries)
	}
	firstHash := entries[0].Hash
	// Same size and timestamp, different bytes: the hash must still change.
	if err := os.WriteFile(path, second, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, info.ModTime(), info.ModTime()); err != nil {
		t.Fatal(err)
	}
	entries, err = c.ListTreeFresh(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Hash == "" || entries[0].Hash == firstHash {
		t.Fatalf("changed asset hash: %+v", entries)
	}
}

func TestListTreeCacheAndFresh(t *testing.T) {
	c, dir := startOpsServer(t)
	root := filepath.Join(dir, "project")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "a.md"), []byte("# A\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if entries, err := c.ListTree(root); err != nil || len(entries) != 1 {
		t.Fatalf("first listing: %d %v", len(entries), err)
	}
	if err := os.WriteFile(filepath.Join(root, "b.md"), []byte("# B\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if entries, err := c.ListTree(root); err != nil || len(entries) != 1 {
		t.Fatalf("cached listing did not reuse the walk: %d %v", len(entries), err)
	}
	if entries, err := c.ListTreeFresh(root); err != nil || len(entries) != 2 {
		t.Fatalf("fresh listing: %d %v", len(entries), err)
	}
	if err := c.WriteFile(filepath.Join(root, "c.md"), []byte("# C\n")); err != nil {
		t.Fatal(err)
	}
	if entries, err := c.ListTree(root); err != nil || len(entries) != 3 {
		t.Fatalf("mutation did not clear the cache: %d %v", len(entries), err)
	}
}

func TestListTreeReportsUnreadableRoot(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root can read an unreadable directory")
	}
	c, dir := startOpsServer(t)
	root := filepath.Join(dir, "locked")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "a.md"), []byte("# A\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(root, 0o000); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(root, 0o755)
	entries, err := c.ListTreeFresh(root)
	if err == nil {
		t.Fatalf("an unreadable root listed as %d entries", len(entries))
	}
}

func TestCreateAfterShutdownDoesNotStartPTY(t *testing.T) {
	s := &Server{sessions: map[string]*session{}, quit: make(chan struct{})}
	t.Cleanup(func() {
		s.mu.Lock()
		all := make([]*session, 0, len(s.sessions))
		for _, ss := range s.sessions {
			all = append(all, ss)
		}
		s.mu.Unlock()
		for _, ss := range all {
			ss.kill()
		}
	})
	marker := filepath.Join(t.TempDir(), "started")
	s.shutdown()
	_, err := s.do(Request{Op: "create", Create: &CreateOptions{
		Command: []string{"sh", "-c", "printf started > " + marker + "; sleep 60"},
		Cols:    40, Rows: 10,
	}})
	if err != errServerShutdown {
		t.Fatalf("create after shutdown returned %v", err)
	}
	s.mu.Lock()
	registered := len(s.sessions)
	s.mu.Unlock()
	if registered != 0 {
		t.Fatalf("create after shutdown registered %d sessions", registered)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("create after shutdown started its command: %v", err)
	}
}

func TestListTreeFollowsRootSymlink(t *testing.T) {
	c, _ := startOpsServer(t)
	root := t.TempDir()
	real := filepath.Join(root, "real")
	if err := os.Mkdir(real, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(real, "a.md"), []byte("# A\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "link")
	if err := os.Symlink("real", link); err != nil {
		t.Fatal(err)
	}
	entries, err := c.ListTreeFresh(link)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Rel != "a.md" {
		t.Fatalf("symlinked root listing: %+v", entries)
	}
}

func TestCopyTreeRejectsCopyingDirectoryOntoItself(t *testing.T) {
	root := filepath.Join(t.TempDir(), "dir")
	if err := os.Mkdir(root, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "a.txt")
	if err := os.WriteFile(path, []byte("contents"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := copyTree(root, root); err == nil {
		t.Fatal("copying a directory onto itself returned no error")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "contents" {
		t.Fatalf("self copy changed the source to %q", data)
	}
}

func TestCopyTreeRejectsDescendantDestinations(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "dir")
	if err := os.Mkdir(source, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "a.txt"), []byte("contents"), 0o644); err != nil {
		t.Fatal(err)
	}
	descendant := filepath.Join(source, "nested", "copy")
	if err := copyTree(source, descendant); err == nil {
		t.Fatal("copying into a descendant returned no error")
	}
	if _, err := os.Stat(filepath.Join(source, "nested")); !os.IsNotExist(err) {
		t.Fatalf("rejected copy created part of its destination: %v", err)
	}
	alias := filepath.Join(root, "alias")
	if err := os.Symlink(source, alias); err != nil {
		t.Fatal(err)
	}
	if err := copyTree(source, filepath.Join(alias, "copy")); err == nil {
		t.Fatal("copying through a symlink into a descendant returned no error")
	}
	if _, err := os.Stat(filepath.Join(source, "copy")); !os.IsNotExist(err) {
		t.Fatalf("rejected symlinked copy created its destination: %v", err)
	}
}

func TestCopyTreePreservesSymlinks(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "target.txt"), []byte("contents"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "target-dir"), 0o755); err != nil {
		t.Fatal(err)
	}
	links := []struct {
		name   string
		target string
	}{
		{name: "file-link", target: "target.txt"},
		{name: "dir-link", target: "target-dir"},
		{name: "broken-link", target: "missing"},
	}
	for _, link := range links {
		from := filepath.Join(root, link.name)
		to := filepath.Join(root, "copied-"+link.name)
		if err := os.Symlink(link.target, from); err != nil {
			t.Fatal(err)
		}
		if err := copyTree(from, to); err != nil {
			t.Fatalf("copying %s: %v", link.name, err)
		}
		info, err := os.Lstat(to)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode()&os.ModeSymlink == 0 {
			t.Fatalf("%s became a %v", link.name, info.Mode())
		}
		target, err := os.Readlink(to)
		if err != nil {
			t.Fatal(err)
		}
		if target != link.target {
			t.Fatalf("%s target = %q, want %q", link.name, target, link.target)
		}
	}
}
