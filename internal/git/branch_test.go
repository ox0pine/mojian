package git_test

import (
	"os"
	"os/exec"
	"strings"
	"testing"

	"mojian/internal/git"
	"mojian/internal/testutil"
)

// branchGit runs one Git command in dir, the way a person would, with the
// environment the harness uses.
func branchGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL="+os.DevNull, "GIT_CONFIG_SYSTEM="+os.DevNull)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func TestReadBranches(t *testing.T) {
	dir := t.TempDir()
	testutil.WriteVault(t, dir, map[string]string{"a.md": "# A\n"})
	testutil.InitRepo(t, dir)
	base := branchGit(t, dir, "symbolic-ref", "--short", "HEAD")
	branchGit(t, dir, "branch", "feature")
	d := git.ReadBranches(git.LocalRunner{}, dir)
	if d.Err != "" || d.Root == "" || d.Detached {
		t.Fatalf("branches: %+v", d)
	}
	if d.Current != base {
		t.Fatalf("current = %q, want %q", d.Current, base)
	}
	current := map[string]bool{}
	for _, b := range d.Branches {
		current[b.Name] = b.Current
	}
	if cur, ok := current[base]; !ok || !cur {
		t.Fatalf("base branch flag: %+v", d.Branches)
	}
	if cur, ok := current["feature"]; !ok || cur {
		t.Fatalf("feature branch flag: %+v", d.Branches)
	}
}

func TestSwitchAndCreateBranch(t *testing.T) {
	dir := t.TempDir()
	testutil.WriteVault(t, dir, map[string]string{"a.md": "# A\n"})
	testutil.InitRepo(t, dir)
	branchGit(t, dir, "symbolic-ref", "--short", "HEAD")
	branchGit(t, dir, "branch", "feature")

	if err := git.SwitchBranch(git.LocalRunner{}, dir, "feature"); err != nil {
		t.Fatal(err)
	}
	if d := git.ReadBranches(git.LocalRunner{}, dir); d.Current != "feature" {
		t.Fatalf("after switch: %+v", d)
	}
	if err := git.CreateBranch(git.LocalRunner{}, dir, "topic"); err != nil {
		t.Fatal(err)
	}
	d := git.ReadBranches(git.LocalRunner{}, dir)
	if d.Current != "topic" {
		t.Fatalf("after create: %+v", d)
	}
	if len(d.Branches) != 3 {
		t.Fatalf("branches: %+v", d.Branches)
	}
	if err := git.CreateBranch(git.LocalRunner{}, dir, "bad name"); err == nil {
		t.Fatal("a name with a space was accepted")
	}
	if err := git.SwitchBranch(git.LocalRunner{}, dir, "missing"); err == nil {
		t.Fatal("a missing branch was accepted")
	}
	if err := git.SwitchBranch(git.LocalRunner{}, dir, ""); err == nil {
		t.Fatal("an empty branch was accepted")
	}
	if d := git.ReadBranches(git.LocalRunner{}, dir); d.Current != "topic" {
		t.Fatalf("a rejected switch moved HEAD: %+v", d)
	}
}

func TestReadBranchesDetachedAndOutsideRepo(t *testing.T) {
	plain := t.TempDir()
	if d := git.ReadBranches(git.LocalRunner{}, plain); d.Root != "" || d.Err == "" {
		t.Fatalf("outside a repository: %+v", d)
	}

	dir := t.TempDir()
	testutil.WriteVault(t, dir, map[string]string{"a.md": "# A\n"})
	testutil.InitRepo(t, dir)
	branchGit(t, dir, "checkout", "-q", "--detach")
	d := git.ReadBranches(git.LocalRunner{}, dir)
	if !d.Detached || d.Current == "" || len(d.Branches) == 0 {
		t.Fatalf("detached: %+v", d)
	}
	for _, b := range d.Branches {
		if b.Current {
			t.Fatalf("detached HEAD marked %q current: %+v", b.Name, d.Branches)
		}
	}
	// A detached HEAD can still move onto a branch.
	base := branchGit(t, dir, "for-each-ref", "--format=%(refname:short)", "refs/heads")
	if err := git.SwitchBranch(git.LocalRunner{}, dir, base); err != nil {
		t.Fatal(err)
	}
	if d := git.ReadBranches(git.LocalRunner{}, dir); d.Detached || d.Current != base {
		t.Fatalf("after leaving detached: %+v", d)
	}
}
