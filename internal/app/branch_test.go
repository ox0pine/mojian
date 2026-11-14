package app

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"mojian/internal/testutil"

	"github.com/egoist/mygo/ui"
)

// branchTestGit runs one Git command in dir for a branch test.
func branchTestGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL="+os.DevNull, "GIT_CONFIG_SYSTEM="+os.DevNull)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// branchTestApp opens dir in a bare window, without the Git page.
func branchTestApp(t *testing.T, dir string) (*app, *ui.Tester) {
	t.Helper()
	a := &app{workspace: &workspace{treeOpen: true}}
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
	return a, tt
}

// branchRepo writes a note, commits it on the default branch, then adds a
// feature branch that holds different text, and returns to the default.
func branchRepo(t *testing.T) (dir, base string) {
	t.Helper()
	dir = t.TempDir()
	testutil.WriteVault(t, dir, map[string]string{
		"a.md":    "# A\n\nmain text\n",
		"gone.md": "# Gone\n",
	})
	testutil.InitRepo(t, dir)
	base = branchTestGit(t, dir, "symbolic-ref", "--short", "HEAD")
	branchTestGit(t, dir, "checkout", "-q", "-b", "feature")
	if err := os.WriteFile(filepath.Join(dir, "a.md"), []byte("# A\n\nfeature text\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "only.md"), []byte("# Only\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(dir, "gone.md")); err != nil {
		t.Fatal(err)
	}
	branchTestGit(t, dir, "add", "-A")
	branchTestGit(t, dir, "commit", "-qm", "feature")
	branchTestGit(t, dir, "checkout", "-q", base)
	return dir, base
}

func TestBranchChipSwitchesAndReloads(t *testing.T) {
	registerFonts()
	dir, base := branchRepo(t)
	a, tt := branchTestApp(t, dir)
	if !a.branch.loaded || a.branch.data.Current != base {
		t.Fatalf("branch: %+v", a.branch)
	}
	if _, ok := tt.Find("Switch branch: " + base); !ok {
		t.Fatalf("branch chip missing: %q", tt.Texts())
	}

	// The chip opens the menu, and the menu lists the branches.
	if err := tt.Click("Switch branch: " + base); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !a.branch.open || !tt.HasText("feature") || !tt.HasText(base) {
		t.Fatalf("branch menu: %q", tt.Texts())
	}

	// Choosing a branch checks it out and the open note follows.
	if err := tt.Click("feature"); err != nil {
		t.Fatal(err)
	}
	for range 3 {
		tt.Frame()
	}
	if a.branch.data.Current != "feature" {
		t.Fatalf("after switch: %+v", a.branch.data)
	}
	if n := a.store.ByRel["a.md"]; n == nil || !strings.Contains(n.Text(), "feature text") {
		t.Fatalf("note after switch: %+v", n)
	}
	if a.dirtyNotes() != 0 {
		t.Fatalf("a reload left %d dirty notes", a.dirtyNotes())
	}
	// The Explorer tree follows the checkout: the file only the branch
	// holds appears, and the one it dropped leaves.
	if a.store.ByRel["only.md"] == nil || a.store.ByRel["gone.md"] != nil {
		t.Fatalf("tree after switch: %v", a.store.ByRel)
	}
	if !tt.HasText("only.md") || tt.HasText("gone.md") {
		t.Fatalf("explorer after switch: %q", tt.Texts())
	}

	// The menu's New Branch row takes a name and creates it.
	a.openBranchPicker()
	tt.Frame()
	if err := tt.Click("New Branch…"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	tt.Type("hotfix")
	tt.Frame()
	if !tt.HasText("Creates a branch from feature") {
		t.Fatalf("create hint missing: %q", tt.Texts())
	}
	tt.Key(0, ui.KeyEnter)
	for range 3 {
		tt.Frame()
	}
	if a.branch.data.Current != "hotfix" {
		t.Fatalf("after create: %+v", a.branch.data)
	}
	found := false
	for _, b := range a.branch.data.Branches {
		found = found || b.Name == "hotfix"
	}
	if !found {
		t.Fatalf("hotfix not listed: %+v", a.branch.data.Branches)
	}
}

func TestBranchSwitchKeepsUnsavedEdits(t *testing.T) {
	registerFonts()
	dir, base := branchRepo(t)
	a, tt := branchTestApp(t, dir)
	n := a.store.ByRel["a.md"]
	if n == nil {
		t.Fatal("a.md missing")
	}
	n.Buffer().Set("# A\n\nunsaved text\n")
	if !n.Dirty() {
		t.Fatal("the buffer did not take the edit")
	}
	a.checkoutBranch(base)
	if a.branch.err == "" {
		t.Fatal("no warning for unsaved edits")
	}
	if a.branch.data.Current != base {
		t.Fatalf("the switch ran: %+v", a.branch.data)
	}
	if !strings.Contains(n.Text(), "unsaved text") {
		t.Fatalf("the edits were dropped: %q", n.Text())
	}
	tt.Frame()
	if !tt.HasText("Save or discard your edits before switching branches.") {
		t.Fatalf("no banner: %q", tt.Texts())
	}
}

func TestBranchChipNeedsRepository(t *testing.T) {
	registerFonts()
	dir := t.TempDir()
	testutil.WriteVault(t, dir, map[string]string{"a.md": "# A\n"})
	a, tt := branchTestApp(t, dir)
	if !a.branch.loaded || a.branch.data.Root != "" {
		t.Fatalf("branch: %+v", a.branch)
	}
	if _, ok := tt.Find("Switch branch: " + a.branch.data.Current); ok {
		t.Fatalf("a folder without Git shows a branch chip: %q", tt.Texts())
	}
	a.openBranchPicker()
	tt.Frame()
	if !tt.HasText("This folder is not a Git repository.") {
		t.Fatalf("branch sheet: %q", tt.Texts())
	}
}
