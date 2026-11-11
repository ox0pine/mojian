package app

import (
	"path/filepath"
	"testing"

	"mojian/internal/testutil"
)

func TestSecondInstanceOpensProjectAndFile(t *testing.T) {
	startLocalServerForTest(t)
	root := t.TempDir()
	active := filepath.Join(root, "Active")
	other := filepath.Join(root, "Other")
	testutil.WriteVault(t, active, map[string]string{"a.md": "# A\n"})
	testutil.WriteVault(t, other, map[string]string{"b.md": "# B\n"})
	a := &app{workspace: &workspace{treeOpen: true}, dataDir: t.TempDir()}
	s, err := openStoreAt(active)
	if err != nil {
		t.Fatal(err)
	}
	a.store = s
	a.layoutPath = a.projectLayoutPath(s.Dir)
	a.project = active
	a.focusFirstNote()
	a.registerActiveWorkspace()

	// A later launch naming a file in another project switches the window
	// to that project and shows the file.
	a.applyStartup(Startup{Dir: other, File: filepath.Join(other, "b.md")})
	if a.store == nil || a.store.Dir != resolveProjectRoot(other) {
		t.Fatalf("the second launch did not switch project: %+v", a.store)
	}
	if n := a.currentNote(); n == nil || n.Rel != "b.md" {
		t.Fatalf("the second launch did not open its file: %+v", n)
	}

	// Naming only a project switches without touching the open tab.
	a.applyStartup(Startup{Dir: active})
	if a.store == nil || a.store.Dir != resolveProjectRoot(active) {
		t.Fatalf("the second launch did not switch back: %+v", a.store)
	}
	if n := a.currentNote(); n != nil && n.Rel != "a.md" {
		t.Fatalf("a project-only launch changed the tab: %+v", n)
	}
}
