package app

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"mojian/internal/testutil"

	"github.com/egoist/mygo/ui"
)

func TestGitFocusRefreshReloadsEditorDiffBase(t *testing.T) {
	registerFonts()
	dir := t.TempDir()
	testutil.WriteVault(t, dir, map[string]string{"note.go": "package x\n\nvar a = 1\n"})
	testutil.InitRepo(t, dir)

	a := &app{workspace: &workspace{}}
	s, err := openStoreAt(dir)
	if err != nil {
		t.Fatal(err)
	}
	a.store = s
	a.focusFirstNote()
	tt := ui.NewTester(a.view, 1100, 700)
	for range 3 {
		tt.Frame()
	}

	p := a.focusPane()
	if p == nil || p.state == nil || p.state.DiffBase == nil {
		t.Fatalf("expected DiffBase loaded for tracked file, got pane=%+v", p)
	}
	if got := p.state.DiffBase.String(); got != "package x\n\nvar a = 1\n" {
		t.Fatalf("initial DiffBase = %q, want original HEAD content", got)
	}

	a.toggleGit()
	for range 3 {
		tt.Frame()
	}
	if !a.git.Open {
		t.Fatal("the Git page did not open")
	}

	if err := os.WriteFile(filepath.Join(dir, "note.go"), []byte("package x\n\nvar a = 2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("git", "-C", dir, "add", "--", "note.go")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("external stage: %v\n%s", err, out)
	}
	cmd = exec.Command("git", "-C", dir, "commit", "-qm", "external commit")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("external commit: %v\n%s", err, out)
	}

	a.refreshProjectOnFocus()
	for range 3 {
		tt.Frame()
	}
	a.toggleGit()
	for range 3 {
		tt.Frame()
	}

	if p.state == nil || p.state.DiffBase == nil {
		t.Fatal("expected DiffBase reloaded after returning to the editor")
	}
	if got := p.state.DiffBase.String(); got != "package x\n\nvar a = 2\n" {
		t.Fatalf("DiffBase after focus = %q, want external commit's HEAD content", got)
	}
}
