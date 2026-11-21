package app

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	gitpage "mojian/internal/git"
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

// The menu takes the keys MyGo's menus have: the chip opens it, Down
// highlights the first branch and Enter checks it out.
func TestBranchMenuKeyboard(t *testing.T) {
	registerFonts()
	dir, base := branchRepo(t)
	a, tt := branchTestApp(t, dir)
	if err := tt.Click("Switch branch: " + base); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	tt.Key(0, ui.KeyDown)
	tt.Frame()
	tt.Key(0, ui.KeyEnter)
	for range 3 {
		tt.Frame()
	}
	if a.branch.data.Current != "feature" {
		t.Fatalf("after Down, Enter: %+v", a.branch.data)
	}
	if a.branch.open {
		t.Fatal("the menu stayed open")
	}
	if n := a.store.ByRel["a.md"]; n == nil || !strings.Contains(n.Text(), "feature text") {
		t.Fatalf("note after switch: %+v", n)
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

func TestBranchReadCannotLandInCurrentWorkspace(t *testing.T) {
	old := &workspace{branch: branchState{seq: 3, loading: true}}
	current := &workspace{branch: branchState{
		seq: 2, loading: true, loaded: true,
		data: gitpage.BranchData{Root: "/current", Current: "current"},
	}}
	a := &app{workspace: current}

	// This read started in old at seq 2. Switching away invalidated it at
	// seq 3, while the new project independently reached seq 2 as well.
	a.applyBranch(old, 2, gitpage.BranchData{Root: "/old", Current: "old"})
	if current.branch.data.Root != "/current" || current.branch.data.Current != "current" {
		t.Fatalf("old read replaced current branch: %+v", current.branch)
	}
	if !current.branch.loading || !old.branch.loading {
		t.Fatalf("stale read changed loading state: old=%v current=%v", old.branch.loading, current.branch.loading)
	}
}

func TestBranchChangeErrorDoesNotCrossWorkspaces(t *testing.T) {
	oldDir := t.TempDir()
	testutil.WriteVault(t, oldDir, map[string]string{"a.md": "# A\n"})
	currentDir := t.TempDir()
	testutil.WriteVault(t, currentDir, map[string]string{"b.md": "# B\n"})
	oldStore, err := openStoreAt(oldDir)
	if err != nil {
		t.Fatal(err)
	}
	currentStore, err := openStoreAt(currentDir)
	if err != nil {
		t.Fatal(err)
	}
	old := &workspace{store: oldStore}
	current := &workspace{store: currentStore, branch: branchState{seq: 1, loading: true}}
	a := &app{workspace: old}
	a.runBranchChange("old checkout", func(gitpage.Runner) error {
		a.workspace = current
		a.store = currentStore
		return errors.New("old failure")
	})
	if a.err != "" {
		t.Fatalf("old checkout reported in current workspace: %q", a.err)
	}
	if !current.branch.loading || current.branch.seq != 1 {
		t.Fatalf("old checkout completed into current workspace: %+v", current.branch)
	}
	if old.branch.loading {
		t.Fatal("originating checkout never cleared its loading state")
	}
}

func TestBranchChangeSuccessDoesNotRefreshCurrentWorkspace(t *testing.T) {
	oldDir := t.TempDir()
	testutil.WriteVault(t, oldDir, map[string]string{"a.md": "# A\n"})
	currentDir := t.TempDir()
	testutil.WriteVault(t, currentDir, map[string]string{"b.md": "# B\n"})
	oldStore, err := openStoreAt(oldDir)
	if err != nil {
		t.Fatal(err)
	}
	currentStore, err := openStoreAt(currentDir)
	if err != nil {
		t.Fatal(err)
	}
	old := &workspace{store: oldStore}
	current := &workspace{store: currentStore, branch: branchState{seq: 1}}
	a := &app{workspace: old}
	a.runBranchChange("old checkout", func(gitpage.Runner) error {
		a.workspace = current
		a.store = currentStore
		return nil
	})
	if current.branch.seq != 1 || current.branch.loaded {
		t.Fatalf("old success refreshed current branch: %+v", current.branch)
	}
	if a.err != "" {
		t.Fatalf("old success reported an error: %q", a.err)
	}
}

func TestBranchChangeSurvivesWorkspaceActivation(t *testing.T) {
	dir := t.TempDir()
	testutil.WriteVault(t, dir, map[string]string{"a.md": "# A\n"})
	store, err := openStoreAt(dir)
	if err != nil {
		t.Fatal(err)
	}
	ws := &workspace{store: store}
	a := &app{workspace: ws}
	a.runBranchChange("checkout", func(gitpage.Runner) error {
		// Activation invalidates UI callbacks and clears read loading, but
		// must not make the still-running checkout look finished.
		ws.branch.seq += 2
		ws.branch.loading = false
		a.refreshBranch()
		if !ws.branch.loading || !ws.branch.changing {
			t.Fatalf("activation released the running checkout: %+v", ws.branch)
		}
		return nil
	})
	if ws.branch.loading || ws.branch.changing {
		t.Fatalf("completed checkout stayed busy: %+v", ws.branch)
	}
}
