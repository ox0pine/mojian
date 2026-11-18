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
