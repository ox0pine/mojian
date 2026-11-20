package app

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	gitpage "mojian/internal/git"
	"mojian/internal/testutil"

	"github.com/egoist/mygo/ui"
)

func TestGitPage(t *testing.T) {
	registerFonts()
	dir := t.TempDir()
	testutil.WriteVault(t, dir, map[string]string{"a.md": "# A\n\nold\n", "keep.go": "package x\n"})
	testutil.InitRepo(t, dir)
	if err := os.WriteFile(filepath.Join(dir, "a.md"), []byte("# A\n\nnew\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	a := &app{workspace: &workspace{treeOpen: true}}
	s, err := openStoreAt(dir)
	if err != nil {
		t.Fatal(err)
	}
	a.store = s
	a.focusFirstNote()
	tt := ui.NewTester(a.view, 1100, 700)
	tt.Frame()
	a.toggleGit()
	for range 3 {
		tt.Frame()
	}
	if !a.git.Open {
		t.Fatal("the diff page did not open")
	}
	if !tt.HasText("Changes") || !tt.HasText("a.md") {
		t.Fatalf("diff page texts: %q", tt.Texts())
	}
	if a.git.Diff == "" || !strings.Contains(a.git.Diff, "+new") {
		t.Fatalf("diff: %q", a.git.Diff)
	}
}

func TestGitCloseRestoresEditorFocus(t *testing.T) {
	registerFonts()
	dir := t.TempDir()
	testutil.WriteVault(t, dir, map[string]string{"a.md": "# A\n\nold\n", "keep.go": "package x\n"})
	testutil.InitRepo(t, dir)
	if err := os.WriteFile(filepath.Join(dir, "a.md"), []byte("# A\n\nnew\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	a := &app{workspace: &workspace{treeOpen: true}}
	s, err := openStoreAt(dir)
	if err != nil {
		t.Fatal(err)
	}
	a.store = s
	a.focusFirstNote()
	n := a.currentNote()
	tt := ui.NewTester(a.view, 1100, 700)
	a.focusReq = a.tab().Focus
	tt.Frame()
	a.toggleGit()
	for range 3 {
		tt.Frame()
	}
	if !a.git.Open {
		t.Fatal("the diff page did not open")
	}
	a.toggleGit()
	for range 3 {
		tt.Frame()
	}
	tt.Type("focus-return")
	if !strings.Contains(n.Text(), "focus-return") {
		t.Fatal("typing after closing Git did not reach the editor")
	}
}

func TestGitListUsesCompactWidth(t *testing.T) {
	registerFonts()
	dir := t.TempDir()
	testutil.WriteVault(t, dir, map[string]string{"a.md": "# A\n\nold\n"})
	testutil.InitRepo(t, dir)
	if err := os.WriteFile(filepath.Join(dir, "a.md"), []byte("# A\n\nnew\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	a := &app{workspace: &workspace{}}
	s, err := openStoreAt(dir)
	if err != nil {
		t.Fatal(err)
	}
	a.store = s
	a.focusFirstNote()
	tt := ui.NewTester(a.view, 800, 500)
	a.toggleGit()
	for range 3 {
		tt.Frame()
	}
	row, ok := tt.Find("a.md")
	if !ok {
		t.Fatalf("change row not found: %q", tt.Texts())
	}
	if row.W <= 0 || row.W > 190 {
		t.Fatalf("change row did not use the compact list width: %+v", row)
	}
}

func TestGitOutsideRepo(t *testing.T) {
	registerFonts()
	dir := t.TempDir()
	testutil.WriteVault(t, dir, map[string]string{"a.md": "# A\n"})
	a := &app{workspace: &workspace{treeOpen: true}}
	s, err := openStoreAt(dir)
	if err != nil {
		t.Fatal(err)
	}
	a.store = s
	tt := ui.NewTester(a.view, 1100, 700)
	a.toggleGit()
	for range 3 {
		tt.Frame()
	}
	if !strings.Contains(a.git.Err, "not a Git repository") {
		t.Fatalf("error: %q", a.git.Err)
	}
	if !tt.HasText("This folder is not a Git repository.") {
		t.Fatalf("texts: %q", tt.Texts())
	}
}

func TestGitHistoryPage(t *testing.T) {
	registerFonts()
	dir := t.TempDir()
	testutil.WriteVault(t, dir, map[string]string{"a.md": "# A\n\nold\n"})
	testutil.InitRepo(t, dir)
	if err := os.WriteFile(filepath.Join(dir, "a.md"), []byte("# A\n\nnew\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL="+os.DevNull, "GIT_CONFIG_SYSTEM="+os.DevNull)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	runGit("commit", "-qam", "rewrite a")
	a := &app{workspace: &workspace{treeOpen: true}}
	s, err := openStoreAt(dir)
	if err != nil {
		t.Fatal(err)
	}
	a.store = s
	a.focusFirstNote()
	tt := ui.NewTester(a.view, 1100, 700)
	tt.Frame()
	a.toggleGit()
	for range 3 {
		tt.Frame()
	}
	a.git.ShowHistory()
	for range 3 {
		tt.Frame()
	}
	if a.git.Mode != gitpage.ModeHistory {
		t.Fatalf("mode: %v", a.git.Mode)
	}
	if len(a.git.Commits) != 2 {
		t.Fatalf("commits: %+v", a.git.Commits)
	}
	if got := a.git.Commits[0].Subject; got != "rewrite a" {
		t.Fatalf("newest: %q", got)
	}
	if !strings.Contains(a.git.Diff, "+new") || !strings.Contains(a.git.Diff, "diff --git a/a.md b/a.md") {
		t.Fatalf("patch: %q", a.git.Diff)
	}
	if !tt.HasText("RECENT COMMITS") || !tt.HasText("rewrite a") || !tt.HasText("a.md") {
		t.Fatalf("texts: %q", tt.Texts())
	}
	if got := a.git.Title(); got != "History" {
		t.Fatalf("title: %q", got)
	}

	a.git.ShowChanges()
	for range 3 {
		tt.Frame()
	}
	if a.git.Mode != gitpage.ModeChanges || len(a.git.Changes) != 0 {
		t.Fatalf("working tree: mode %v, changes %+v", a.git.Mode, a.git.Changes)
	}

	// The page's branch chip opens the page's own menu, not the title bar's,
	// and choosing a branch there switches the page and the title bar.
	runGit("branch", "feature")
	a.git.ToggleBranchMenu()
	for range 3 {
		tt.Frame()
	}
	if !a.git.BranchMenu || a.branch.open || !tt.HasText("BRANCHES") {
		t.Fatalf("branch menu: page=%v title=%v texts=%q", a.git.BranchMenu, a.branch.open, tt.Texts())
	}
	if !tt.HasText("feature") {
		t.Fatalf("branch list: %q", tt.Texts())
	}
	if err := tt.Click("feature"); err != nil {
		t.Fatal(err)
	}
	for range 4 {
		tt.Frame()
	}
	if a.git.Branch != "feature" || a.branch.data.Current != "feature" {
		t.Fatalf("switch: page=%q title=%q", a.git.Branch, a.branch.data.Current)
	}
	if a.git.BranchMenu {
		t.Fatal("the menu stayed open after the switch")
	}
}

// A click on another changed file moves the page to it: reading Clicked or
// DoubleClicked inside a list row applies the pending pointer input mid
// build and swallows the row's own click, so the page takes a double click
// from the list's Submitted instead.
func TestGitChangesClick(t *testing.T) {
	registerFonts()
	dir := t.TempDir()
	testutil.WriteVault(t, dir, map[string]string{"a.md": "a\n", "b.md": "b\n"})
	testutil.InitRepo(t, dir)
	if err := os.WriteFile(filepath.Join(dir, "a.md"), []byte("a changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "b.md"), []byte("b changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	a := &app{workspace: &workspace{treeOpen: false}}
	s, err := openStoreAt(dir)
	if err != nil {
		t.Fatal(err)
	}
	a.store = s
	tt := ui.NewTester(a.view, 1100, 700)
	tt.Frame()
	a.toggleGit()
	for range 3 {
		tt.Frame()
	}
	if a.git.Sel != 0 || a.git.DiffPath != "a.md" {
		t.Fatalf("first file: sel %d path %q", a.git.Sel, a.git.DiffPath)
	}
	if err := tt.Click("b.md"); err != nil {
		t.Fatal(err)
	}
	for range 3 {
		tt.Frame()
	}
	if a.git.Sel != 1 || a.git.DiffPath != "b.md" || !strings.Contains(a.git.Diff, "+b changed") {
		t.Fatalf("after click: sel %d path %q diff %q", a.git.Sel, a.git.DiffPath, a.git.Diff)
	}
}
