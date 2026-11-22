package git

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mojian/internal/testutil"
)

func TestSplitAndStageUnstageDiscardHunk(t *testing.T) {
	dir := t.TempDir()
	testutil.WriteVault(t, dir, map[string]string{
		"main.txt": "line1\nline2\nline3\nline4\nline5\nline6\nline7\nline8\nline9\nline10\n",
	})
	testutil.InitRepo(t, dir)
	// Modify two separated lines so Git produces two distinct hunks.
	edited := "LINE1\nline2\nline3\nline4\nline5\nline6\nline7\nline8\nline9\nLINE10\n"
	if err := os.WriteFile(filepath.Join(dir, "main.txt"), []byte(edited), 0o644); err != nil {
		t.Fatal(err)
	}

	st := ReadStatus(LocalRunner{}, dir)
	if len(st.Changes) != 1 {
		t.Fatalf("changes = %+v", st.Changes)
	}
	ch := st.Changes[0]
	raw, err := ReadDiff(LocalRunner{}, dir, ch)
	if err != nil {
		t.Fatal(err)
	}
	hunks := SplitHunks(raw)
	if len(hunks) != 2 {
		t.Fatalf("expected 2 hunks, got %d:\n%s", len(hunks), raw)
	}

	// Stage only hunk 0.
	if err := StageHunk(LocalRunner{}, dir, ch, 0); err != nil {
		t.Fatalf("StageHunk: %v", err)
	}
	stagedDiff, _ := LocalRunner{}.Run(dir, "diff", "--cached", "--", "main.txt")
	if !strings.Contains(stagedDiff, "+LINE1") || strings.Contains(stagedDiff, "+LINE10") {
		t.Fatalf("expected only hunk 0 staged, got:\n%s", stagedDiff)
	}

	// Unstage hunk 0.
	st = ReadStatus(LocalRunner{}, dir)
	if err := UnstageHunk(LocalRunner{}, dir, st.Changes[0], 0); err != nil {
		t.Fatalf("UnstageHunk: %v", err)
	}
	stagedDiff, _ = LocalRunner{}.Run(dir, "diff", "--cached", "--", "main.txt")
	if strings.TrimSpace(stagedDiff) != "" {
		t.Fatalf("expected empty staged diff after unstage, got:\n%s", stagedDiff)
	}

	// Discard hunk 1 (LINE10 -> line10), keeping hunk 0 (LINE1).
	st = ReadStatus(LocalRunner{}, dir)
	if err := DiscardHunk(LocalRunner{}, dir, st.Changes[0], 1); err != nil {
		t.Fatalf("DiscardHunk: %v", err)
	}
	got, _ := os.ReadFile(filepath.Join(dir, "main.txt"))
	if !strings.Contains(string(got), "LINE1\n") || strings.Contains(string(got), "LINE10") {
		t.Fatalf("after DiscardHunk(1), content = %q", string(got))
	}
}

func TestStashAmendAndLineBlame(t *testing.T) {
	dir := t.TempDir()
	testutil.WriteVault(t, dir, map[string]string{
		"hello.txt": "alpha\nbeta\n",
	})
	testutil.InitRepo(t, dir)

	// Line blame on committed line 1.
	info, ok := ReadLineBlame(LocalRunner{}, dir, "hello.txt", 1)
	if !ok || info.Author == "" || info.Summary == "" {
		t.Fatalf("ReadLineBlame = %+v ok=%v", info, ok)
	}
	if !strings.Contains(info.InlineText(), info.Author) {
		t.Fatalf("InlineText = %q", info.InlineText())
	}

	// Stash & Pop.
	if err := os.WriteFile(filepath.Join(dir, "hello.txt"), []byte("alpha\ngamma\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Stash(LocalRunner{}, dir); err != nil {
		t.Fatalf("Stash: %v", err)
	}
	if st := ReadStatus(LocalRunner{}, dir); len(st.Changes) != 0 {
		t.Fatalf("expected clean tree after Stash, got %+v", st.Changes)
	}
	if err := StashPop(LocalRunner{}, dir); err != nil {
		t.Fatalf("StashPop: %v", err)
	}
	if st := ReadStatus(LocalRunner{}, dir); len(st.Changes) != 1 {
		t.Fatalf("expected restored change after StashPop, got %+v", st.Changes)
	}

	// Amend commit.
	if err := Stage(LocalRunner{}, dir, "hello.txt"); err != nil {
		t.Fatal(err)
	}
	if err := CreateCommitAmend(LocalRunner{}, dir, "amended message", true); err != nil {
		t.Fatalf("CreateCommitAmend: %v", err)
	}
	if msg := LastCommitMessage(LocalRunner{}, dir); msg != "amended message" {
		t.Fatalf("LastCommitMessage = %q, want %q", msg, "amended message")
	}
}
