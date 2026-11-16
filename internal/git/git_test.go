package git

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestParseDiff(t *testing.T) {
	diff := `diff --git a/x.py b/x.py
index 111..222 100644
--- a/x.py
+++ b/x.py
@@ -1,3 +1,4 @@
 old
-removed
+added
+more
 context
`
	lines := ParseDiff(diff)
	var kinds []byte
	for _, line := range lines {
		kinds = append(kinds, line.kind)
	}
	if string(kinds) != "@ -++ " {
		t.Fatalf("kinds: %q (lines=%+v)", kinds, lines)
	}
	if add, del := DiffStat(diff); add != 2 || del != 1 {
		t.Fatalf("stat: +%d -%d", add, del)
	}
	if lines[2].old != 2 || lines[2].new != 0 {
		t.Fatalf("deleted numbers: %+v", lines[2])
	}
	if lines[3].old != 0 || lines[3].new != 2 {
		t.Fatalf("added numbers: %+v", lines[3])
	}
	if lines[5].old != 3 || lines[5].new != 4 {
		t.Fatalf("context numbers: %+v", lines[5])
	}
}

func TestParseDiffKeepsContentThatLooksLikeHeaders(t *testing.T) {
	diff := `diff --git a/x b/x
--- a/x
+++ b/x
@@ -1,1 +1,1 @@
--- a/removed
+++ b/added
`
	lines := ParseDiff(diff)
	var kinds []byte
	for _, line := range lines {
		kinds = append(kinds, line.kind)
	}
	if string(kinds) != "@-+" {
		t.Fatalf("kinds: %q (lines=%+v)", kinds, lines)
	}
	if lines[1].text != "--- a/removed" || lines[1].old != 1 {
		t.Fatalf("deleted line: %+v", lines[1])
	}
	if lines[2].text != "+++ b/added" || lines[2].new != 1 {
		t.Fatalf("added line: %+v", lines[2])
	}
	if add, del := DiffStat(diff); add != 1 || del != 1 {
		t.Fatalf("stat: +%d -%d", add, del)
	}
}

func TestStatusLetterConflicts(t *testing.T) {
	for _, code := range []string{"UU", "AA", "DD", "AU", "UD", "UA", "DU"} {
		if got := statusLetter(code); got != "C" {
			t.Errorf("statusLetter(%q) = %q, want C", code, got)
		}
	}
}

func TestParseStatusKeepsRenameSource(t *testing.T) {
	changes := parseStatus("R  moved.go\x00pack.go\x00M  other.go\x00", "")
	if len(changes) != 2 {
		t.Fatalf("changes: %+v", changes)
	}
	if changes[0].Path != "moved.go" || changes[0].OldPath != "pack.go" || changes[0].Status != "R" {
		t.Fatalf("rename: %+v", changes[0])
	}
	if changes[1].Path != "other.go" || changes[1].OldPath != "" {
		t.Fatalf("edit: %+v", changes[1])
	}
}

func TestDiffLineTextCapsABinaryLine(t *testing.T) {
	// A binary file Git prints as text arrives as one line of megabytes.
	// Passing it on whole made the layout of one frame take minutes.
	long := strings.Repeat("\xff", 1<<20)
	got := diffLineText(long)
	if n := utf8.RuneCountInString(got); n > diffLineRunes+1 {
		t.Fatalf("rendered line has %d runes", n)
	}
	if !strings.HasPrefix(got, strings.Repeat("\xff", 32)) {
		t.Fatal("the cut line lost its start")
	}
	if !strings.HasSuffix(got, "…") {
		t.Fatalf("the cut line has no ellipsis: %q", got[max(0, len(got)-8):])
	}
	if tabbed := diffLineText("+a\tb"); tabbed != "+a    b" {
		t.Fatalf("tab: %q", tabbed)
	}
	if blank := diffLineText("  "); blank != "\u00a0" {
		t.Fatalf("blank row: %q", blank)
	}
}

func TestSplitDiffReadsEveryFile(t *testing.T) {
	diff := `diff --git a/a.txt b/a.txt
index 111..222 100644
--- a/a.txt
+++ b/a.txt
@@ -1 +1 @@
-one
+two
diff --git a/new.txt b/new.txt
new file mode 100644
index 000..111
--- /dev/null
+++ b/new.txt
@@ -0,0 +1 @@
+hi
diff --git a/gone.txt b/gone.txt
deleted file mode 100644
index 111..000
--- a/gone.txt
+++ /dev/null
@@ -1 +0,0 @@
-bye
diff --git a/old.txt b/renamed.txt
similarity index 100%
rename from old.txt
rename to renamed.txt
diff --git a/blob.bin b/blob.bin
new file mode 100644
index 000..222
Binary files /dev/null and b/blob.bin differ
`
	files := SplitDiff(diff)
	if len(files) != 5 {
		t.Fatalf("files: %+v", files)
	}
	want := []struct {
		path, status string
		add, del     int
	}{
		{"a.txt", "M", 1, 1},
		{"new.txt", "A", 1, 0},
		{"gone.txt", "D", 0, 1},
		{"renamed.txt", "R", 0, 0},
		{"blob.bin", "A", 0, 0},
	}
	for i, w := range want {
		got := files[i]
		if got.Path != w.path || got.Status != w.status || got.Add != w.add || got.Del != w.del {
			t.Errorf("file %d: %+v, want %s %s +%d -%d", i, got, w.path, w.status, w.add, w.del)
		}
	}
	if len(files[0].Lines) == 0 || files[0].Lines[1].text != "-one" {
		t.Fatalf("first file lines: %+v", files[0].Lines)
	}
}

func TestParseHistoryStopsAtTheLastRecord(t *testing.T) {
	out := "h1\x00s1\x00A\x002 days ago\x00first\x00h2\x00s2\x00B\x001 hour ago\x00second\x00"
	commits := parseHistory(out)
	if len(commits) != 2 {
		t.Fatalf("commits: %+v", commits)
	}
	if commits[0] != (Commit{Hash: "h1", Short: "s1", Author: "A", When: "2 days ago", Subject: "first"}) {
		t.Fatalf("first: %+v", commits[0])
	}
	if commits[1].Subject != "second" || commits[1].Hash != "h2" {
		t.Fatalf("second: %+v", commits[1])
	}
	if len(parseHistory("")) != 0 {
		t.Fatal("an empty log has no commits")
	}
}
