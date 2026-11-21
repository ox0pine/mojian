package git_test

import (
	"os"
	"path/filepath"
	"testing"

	"mojian/internal/git"
	"mojian/internal/testutil"
)

func TestGitStageUnstageDiscardCommit(t *testing.T) {
	dir := t.TempDir()
	testutil.WriteVault(t, dir, map[string]string{"a.md": "one\n", "b.md": "two\n"})
	testutil.InitRepo(t, dir)

	// Modify a.md and add untracked c.md.
	if err := os.WriteFile(filepath.Join(dir, "a.md"), []byte("one changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "c.md"), []byte("new file\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	r := git.LocalRunner{}
	st := git.ReadStatus(r, dir)
	if len(st.Changes) != 2 {
		t.Fatalf("changes = %+v, want 2", st.Changes)
	}
	if st.Changes[0].Staged || !st.Changes[0].Unstaged {
		t.Fatalf("expected unstaged change: %+v", st.Changes[0])
	}

	// Stage a.md.
	if err := git.Stage(r, st.Root, "a.md"); err != nil {
		t.Fatal(err)
	}
	st = git.ReadStatus(r, dir)
	if !st.Changes[0].Staged || st.Changes[0].Unstaged {
		t.Fatalf("expected a.md staged: %+v", st.Changes[0])
	}

	// Unstage a.md.
	if err := git.Unstage(r, st.Root, "a.md"); err != nil {
		t.Fatal(err)
	}
	st = git.ReadStatus(r, dir)
	if st.Changes[0].Staged || !st.Changes[0].Unstaged {
		t.Fatalf("expected a.md unstaged again: %+v", st.Changes[0])
	}

	// Discard untracked c.md.
	var cChange git.Change
	for _, ch := range st.Changes {
		if ch.Path == "c.md" {
			cChange = ch
		}
	}
	if err := git.Discard(r, st.Root, cChange); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "c.md")); !os.IsNotExist(err) {
		t.Fatalf("expected c.md removed, err=%v", err)
	}

	// Stage and commit a.md.
	if err := git.Stage(r, st.Root, "a.md"); err != nil {
		t.Fatal(err)
	}
	if err := git.CreateCommit(r, st.Root, "update a"); err != nil {
		t.Fatal(err)
	}
	st = git.ReadStatus(r, dir)
	if len(st.Changes) != 0 {
		t.Fatalf("expected clean tree after commit, got %+v", st.Changes)
	}
	hist := git.ReadHistory(r, dir)
	if len(hist.Commits) < 2 || hist.Commits[0].Subject != "update a" {
		t.Fatalf("history = %+v", hist.Commits)
	}

	// Modify b.md and discard it back to HEAD.
	if err := os.WriteFile(filepath.Join(dir, "b.md"), []byte("dirty\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	st = git.ReadStatus(r, dir)
	if len(st.Changes) != 1 {
		t.Fatalf("expected b.md change, got %+v", st.Changes)
	}
	if err := git.Discard(r, st.Root, st.Changes[0]); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "b.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "two\n" {
		t.Fatalf("b.md after discard = %q, want %q", string(data), "two\n")
	}
}
