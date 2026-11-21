package git_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"mojian/internal/git"
	"mojian/internal/testutil"
)

func TestWorkingTreeStatusAndDiff(t *testing.T) {
	dir := t.TempDir()
	testutil.WriteVault(t, dir, map[string]string{
		"a.md": "# A\n\nold\n", "b.js": "const b = 1;\n",
		"sub/d.txt": "d\n",
	})
	testutil.InitRepo(t, dir)
	if err := os.WriteFile(filepath.Join(dir, "a.md"), []byte("# A\n\nnew line\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "new.py"), []byte("print('new')\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	data := git.ReadStatus(git.LocalRunner{}, dir)
	if data.Err != "" || data.Branch == "" || data.Branch == "detached" {
		t.Fatalf("status: %+v", data)
	}
	if len(data.Changes) != 2 {
		t.Fatalf("changes: %+v", data.Changes)
	}
	var modified, untracked *git.Change
	for i := range data.Changes {
		switch data.Changes[i].Display {
		case "a.md":
			modified = &data.Changes[i]
		case "new.py":
			untracked = &data.Changes[i]
		}
	}
	if modified == nil || modified.Status != "M" || untracked == nil || untracked.Status != "U" {
		t.Fatalf("changes: %+v", data.Changes)
	}
	diff, err := git.ReadDiff(git.LocalRunner{}, data.Root, *modified)
	if err != nil || !strings.Contains(diff, "+new line") || !strings.Contains(diff, "-old") {
		t.Fatalf("diff: %q %v", diff, err)
	}
	diff, err = git.ReadDiff(git.LocalRunner{}, data.Root, *untracked)
	if err != nil || !strings.Contains(diff, "new file") || !strings.Contains(diff, "+print('new')") {
		t.Fatalf("untracked diff: %q %v", diff, err)
	}
}

func TestNestedProjectPaths(t *testing.T) {
	root := t.TempDir()
	sub := filepath.Join(root, "sub")
	testutil.WriteVault(t, sub, map[string]string{"a.md": "# A\n\nold\n"})
	testutil.InitRepo(t, root)
	if err := os.WriteFile(filepath.Join(sub, "a.md"), []byte("# A\n\nnew\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	data := git.ReadStatus(git.LocalRunner{}, sub)
	if data.Err != "" || len(data.Changes) != 1 {
		t.Fatalf("status: %+v", data)
	}
	if data.Changes[0].Display != "a.md" || filepath.ToSlash(data.Changes[0].Path) != "sub/a.md" {
		t.Fatalf("paths: %+v", data.Changes[0])
	}
}

func TestRenameDiffKeepsTheRename(t *testing.T) {
	dir := t.TempDir()
	var body strings.Builder
	for i := 1; i <= 40; i++ {
		body.WriteString("line ")
		body.WriteString(strings.Repeat("x", i%3))
		body.WriteString("\n")
	}
	testutil.WriteVault(t, dir, map[string]string{"pack.go": body.String()})
	testutil.InitRepo(t, dir)
	if err := os.Rename(filepath.Join(dir, "pack.go"), filepath.Join(dir, "moved.go")); err != nil {
		t.Fatal(err)
	}
	edited := strings.Replace(body.String(), "line 1\n", "line one\n", 1)
	if err := os.WriteFile(filepath.Join(dir, "moved.go"), []byte(edited), 0o644); err != nil {
		t.Fatal(err)
	}
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL="+os.DevNull, "GIT_CONFIG_SYSTEM="+os.DevNull)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("add", "-A")
	data := git.ReadStatus(git.LocalRunner{}, dir)
	var renamed *git.Change
	for i := range data.Changes {
		if data.Changes[i].Status == "R" {
			renamed = &data.Changes[i]
		}
	}
	if renamed == nil {
		t.Fatalf("status: %+v", data.Changes)
	}
	if renamed.OldPath != "pack.go" {
		t.Fatalf("rename source: %q", renamed.OldPath)
	}
	diff, err := git.ReadDiff(git.LocalRunner{}, data.Root, *renamed)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(diff, "rename from pack.go") || !strings.Contains(diff, "rename to moved.go") {
		t.Fatalf("rename diff: %q", diff)
	}
}

func TestHistoryReadsCommitsAndPatches(t *testing.T) {
	dir := t.TempDir()
	testutil.WriteVault(t, dir, map[string]string{
		"a.md":    "# A\n\nold\n",
		"中文 名.md": "x\n",
	})
	testutil.InitRepo(t, dir)
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL="+os.DevNull, "GIT_CONFIG_SYSTEM="+os.DevNull)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "a.md"), []byte("# A\n\nnew\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(filepath.Join(dir, "中文 名.md"), filepath.Join(dir, "改名.md")); err != nil {
		t.Fatal(err)
	}
	run("add", "-A")
	run("commit", "-qm", "second 中文")

	data := git.ReadHistory(git.LocalRunner{}, dir)
	if data.Err != "" || data.Root == "" {
		t.Fatalf("history: %+v", data)
	}
	if len(data.Commits) != 2 {
		t.Fatalf("commits: %+v", data.Commits)
	}
	newest := data.Commits[0]
	if newest.Subject != "second 中文" || newest.Short == "" || newest.Hash == "" ||
		newest.Author != "Mojian Test" || newest.When == "" {
		t.Fatalf("newest: %+v", newest)
	}
	if data.Commits[1].Subject != "initial" {
		t.Fatalf("oldest: %+v", data.Commits[1])
	}

	diff, err := git.ReadCommitDiff(git.LocalRunner{}, data.Root, data.Prefix, newest.Hash)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(diff, "+new") || !strings.Contains(diff, "rename to 改名.md") {
		t.Fatalf("patch: %q", diff)
	}
	files := git.SplitDiff(diff)
	status := map[string]string{}
	for _, f := range files {
		status[f.Path] = f.Status
	}
	if len(files) != 2 || status["a.md"] != "M" || status["改名.md"] != "R" {
		t.Fatalf("files: %+v", files)
	}
}

func TestHistoryOfAnEmptyRepository(t *testing.T) {
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL="+os.DevNull, "GIT_CONFIG_SYSTEM="+os.DevNull)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-q")
	data := git.ReadHistory(git.LocalRunner{}, dir)
	if data.Err != "" || len(data.Commits) != 0 {
		t.Fatalf("history: %+v", data)
	}
}

func TestUnbornHeadDiffIncludesStagedAndUnstagedEdits(t *testing.T) {
	dir := t.TempDir()
	branchGit(t, dir, "init", "-q")
	path := filepath.Join(dir, "a.md")
	if err := os.WriteFile(path, []byte("one\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	branchGit(t, dir, "add", "--", "a.md")
	if err := os.WriteFile(path, []byte("one\ntwo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	data := git.ReadStatus(git.LocalRunner{}, dir)
	if data.Err != "" || len(data.Changes) != 1 || data.Changes[0].Path != "a.md" {
		t.Fatalf("status: %+v", data)
	}
	diff, err := git.ReadDiff(git.LocalRunner{}, data.Root, data.Changes[0])
	if err != nil || !strings.Contains(diff, "+one") || !strings.Contains(diff, "+two") {
		t.Fatalf("unborn HEAD diff: %q %v", diff, err)
	}
}

func TestLiteralFilePathspecs(t *testing.T) {
	dir := t.TempDir()
	testutil.WriteVault(t, dir, map[string]string{
		"a[1].txt": "bracket old\n",
		"a1.txt":   "plain old\n",
	})
	testutil.InitRepo(t, dir)
	if err := os.WriteFile(filepath.Join(dir, "a[1].txt"), []byte("bracket new\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "a1.txt"), []byte("plain new\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	data := git.ReadStatus(git.LocalRunner{}, dir)
	changes := map[string]git.Change{}
	for _, change := range data.Changes {
		changes[change.Display] = change
	}
	bracket, ok := changes["a[1].txt"]
	if !ok {
		t.Fatalf("bracket path missing: %+v", data)
	}
	diff, err := git.ReadDiff(git.LocalRunner{}, data.Root, bracket)
	if err != nil || !strings.Contains(diff, "+bracket new") || strings.Contains(diff, "+plain new") {
		t.Fatalf("bracket path diff: %q %v", diff, err)
	}
	plain, ok := changes["a1.txt"]
	if !ok {
		t.Fatalf("plain path missing: %+v", data)
	}
	diff, err = git.ReadDiff(git.LocalRunner{}, data.Root, plain)
	if err != nil || !strings.Contains(diff, "+plain new") || strings.Contains(diff, "+bracket new") {
		t.Fatalf("plain path diff: %q %v", diff, err)
	}
}

func TestLiteralNestedProjectPathspecs(t *testing.T) {
	root := t.TempDir()
	project := filepath.Join(root, "sub[1]")
	testutil.WriteVault(t, project, map[string]string{"a.md": "old\n"})
	testutil.InitRepo(t, root)
	if err := os.WriteFile(filepath.Join(project, "a.md"), []byte("new\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	status := git.ReadStatus(git.LocalRunner{}, project)
	if status.Err != "" || len(status.Changes) != 1 || status.Changes[0].Display != "a.md" {
		t.Fatalf("nested status: %+v", status)
	}
	history := git.ReadHistory(git.LocalRunner{}, project)
	if history.Err != "" || len(history.Commits) != 1 {
		t.Fatalf("nested history: %+v", history)
	}
	patch, err := git.ReadCommitDiff(git.LocalRunner{}, history.Root, history.Prefix, history.Commits[0].Hash)
	if err != nil || !strings.Contains(patch, "sub[1]/a.md") || !strings.Contains(patch, "+old") {
		t.Fatalf("nested commit patch: %q %v", patch, err)
	}
}

func TestUntrackedSymlinkDiffDoesNotReadTarget(t *testing.T) {
	dir := t.TempDir()
	testutil.WriteVault(t, dir, map[string]string{"tracked.md": "tracked\n"})
	testutil.InitRepo(t, dir)
	target := filepath.Join(t.TempDir(), "outside.txt")
	if err := os.WriteFile(target, []byte("outside file contents\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(dir, "link")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	data := git.ReadStatus(git.LocalRunner{}, dir)
	var link *git.Change
	for i := range data.Changes {
		if data.Changes[i].Display == "link" {
			link = &data.Changes[i]
		}
	}
	if link == nil || link.Status != "U" {
		t.Fatalf("symlink status: %+v", data)
	}
	diff, err := git.ReadDiff(git.LocalRunner{}, data.Root, *link)
	if err != nil || !strings.Contains(diff, "new file mode 120000") ||
		!strings.Contains(diff, "+"+target) || strings.Contains(diff, "outside file contents") {
		t.Fatalf("symlink diff: %q %v", diff, err)
	}
}

func TestRepoRootPreservesTrailingSpace(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows cannot address a directory whose name ends in a space")
	}
	dir := filepath.Join(t.TempDir(), "repo ")
	testutil.WriteVault(t, dir, map[string]string{"a.md": "old\n"})
	testutil.InitRepo(t, dir)
	if err := os.WriteFile(filepath.Join(dir, "a.md"), []byte("new\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	status := git.ReadStatus(git.LocalRunner{}, dir)
	if status.Err != "" || !strings.HasSuffix(status.Root, "repo ") || len(status.Changes) != 1 {
		t.Fatalf("status from spaced root: %+v", status)
	}
	branches := git.ReadBranches(git.LocalRunner{}, dir)
	if branches.Err != "" || branches.Root != status.Root || !strings.HasSuffix(branches.Root, "repo ") {
		t.Fatalf("branches from spaced root: %+v", branches)
	}
}
