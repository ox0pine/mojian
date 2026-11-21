//go:build darwin || linux

package fs_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"mojian/internal/fs"
	"mojian/internal/project"
	"mojian/internal/session"
)

func startFilesystemServer(t *testing.T) *session.Client {
	t.Helper()
	t.Setenv("MOJIAN_DIR", t.TempDir())
	done := make(chan error, 1)
	go func() { done <- session.Serve() }()
	deadline := time.Now().Add(3 * time.Second)
	for !session.Ready() {
		if time.Now().After(deadline) {
			t.Fatal("session server did not start")
		}
		time.Sleep(10 * time.Millisecond)
	}
	client, err := session.Connect()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = client.Shutdown()
		_ = client.Close()
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			t.Error("session server did not stop")
		}
	})
	return client
}

func TestServerStoreRenameCaseOnly(t *testing.T) {
	client := startFilesystemServer(t)
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.md"), []byte("contents\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	s := project.New(fs.NewServer(client, root, t.TempDir(), true))
	if err := s.Refresh(); err != nil {
		t.Fatal(err)
	}
	n := s.ByRelPath("a.md")
	if err := s.LoadNote(n); err != nil {
		t.Fatal(err)
	}
	rel, changed, err := s.Rename("a.md", "A.md")
	if err != nil {
		t.Fatal(err)
	}
	if rel != "A.md" || len(changed) != 1 || changed[0] != n || n.Rel != rel {
		t.Fatalf("case-only rename did not remap the note: rel=%q note=%+v", rel, n)
	}
	if s.ByRelPath("a.md") != nil || s.ByRelPath("A.md") != n {
		t.Fatal("the store did not replace the old spelling")
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "A.md" {
		t.Fatalf("directory entries after rename: %+v", entries)
	}
	data, err := os.ReadFile(filepath.Join(root, "A.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "contents\n" {
		t.Fatalf("renamed contents = %q", data)
	}
}

func TestServerStoreCaseRenameKeepsTemporarySymlink(t *testing.T) {
	client := startFilesystemServer(t)
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.md"), []byte("contents\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	bystander := filepath.Join(root, "a.md.mojian-rename")
	if err := os.Symlink("missing-target", bystander); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	s := project.New(fs.NewServer(client, root, t.TempDir(), true))
	if err := s.Refresh(); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.Rename("a.md", "A.md"); err != nil {
		t.Fatal(err)
	}
	if target, err := os.Readlink(bystander); err != nil || target != "missing-target" {
		t.Fatalf("the temporary symlink was replaced: target=%q err=%v", target, err)
	}
	if data, err := os.ReadFile(filepath.Join(root, "A.md")); err != nil || string(data) != "contents\n" {
		t.Fatalf("renamed contents = %q, %v", data, err)
	}
}

func TestServerStoreCaseRenameKeepsDistinctDestination(t *testing.T) {
	root := t.TempDir()
	lower := filepath.Join(root, "a.md")
	upper := filepath.Join(root, "A.md")
	if err := os.WriteFile(lower, []byte("lower\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(upper, []byte("upper\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	lowerInfo, err := os.Lstat(lower)
	if err != nil {
		t.Fatal(err)
	}
	upperInfo, err := os.Lstat(upper)
	if err != nil {
		t.Fatal(err)
	}
	if os.SameFile(lowerInfo, upperInfo) {
		t.Skip("the filesystem has no distinct case-sensitive destination")
	}
	client := startFilesystemServer(t)
	s := project.New(fs.NewServer(client, root, t.TempDir(), true))
	if err := s.Refresh(); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.Rename("a.md", "A.md"); err == nil {
		t.Fatal("case-only rename replaced a distinct destination")
	}
	for path, want := range map[string]string{lower: "lower\n", upper: "upper\n"} {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if string(data) != want {
			t.Fatalf("%s = %q, want %q", path, data, want)
		}
	}
}
