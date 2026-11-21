//go:build darwin || linux

package fs

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDirectListReportsUnreadableRoot(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root can read an unreadable directory")
	}
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.md"), []byte("# A\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(root, 0o000); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(root, 0o755)
	if entries, _, err := NewDirect(root).List(); err == nil {
		t.Fatalf("an unreadable root listed as %d entries", len(entries))
	}
}

// TestDirectListHidesGitignoredFolders checks that a folder the project
// ignores leaves the listing, while a tracked folder keeps its files.
func TestDirectListHidesGitignoredFolders(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, ".gitignore"), []byte("generated/\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, dir := range []string{"generated", "src"} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, dir, "a.md"), []byte("# A\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	entries, bodies, err := NewDirect(root).List()
	if err != nil {
		t.Fatal(err)
	}
	rels := map[string]bool{}
	for _, e := range entries {
		rels[e.Rel] = true
	}
	if rels["generated"] || rels["generated/a.md"] {
		t.Error("a gitignored folder was listed")
	}
	if !rels["src"] || !rels["src/a.md"] {
		t.Error("a tracked folder is missing from the listing")
	}
	if _, ok := bodies[filepath.Join("src", "a.md")]; !ok {
		t.Error("a tracked file's body is missing")
	}
}

func TestDirectCaseRenamePreservesExistingTemporaryName(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "a.md")
	if err := os.WriteFile(source, []byte("source"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "A.md")); err != nil {
		t.Skip("the filesystem is case-sensitive")
	}
	bystander := source + ".mojian-rename"
	if err := os.WriteFile(bystander, []byte("keep me"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := NewDirect(root).Rename("a.md", "A.md"); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(bystander); err != nil || string(data) != "keep me" {
		t.Fatalf("the existing temporary name was consumed: data=%q err=%v", data, err)
	}
	if data, err := os.ReadFile(filepath.Join(root, "A.md")); err != nil || string(data) != "source" {
		t.Fatalf("the renamed file has data=%q err=%v", data, err)
	}
}

func TestWriteFileAtomicPreservesPermissions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "run.sh")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := WriteFileAtomic(path, []byte("#!/bin/sh\necho ok\n")); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o755 {
		t.Fatalf("mode = %o, want 755", got)
	}
}
