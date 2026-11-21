package git

import (
	"strings"
	"testing"
	"unicode/utf8"

	"mojian/internal/theme"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
)

type pageRunner func(dir string, args ...string) (string, error)

func (r pageRunner) Run(dir string, args ...string) (string, error) { return r(dir, args...) }
func (pageRunner) ReadFile(string, string, int) ([]byte, error)     { return nil, nil }
func (pageRunner) RealPath(path string) (string, error)             { return path, nil }
func (pageRunner) Readlink(string, string) (string, error)          { return "", nil }

type pageHost struct {
	dir    string
	runner Runner
}

func (h *pageHost) ProjectDir() (string, bool) { return h.dir, true }
func (h *pageHost) GitRunner() Runner          { return h.runner }
func (h *pageHost) Window() *mygo.Window       { return nil }
func (h *pageHost) Invalidate()                {}
func (h *pageHost) CloseGit()                  {}
func (h *pageHost) Fail(error)                 {}
func (h *pageHost) HasPath(string) bool        { return false }
func (h *pageHost) OpenPath(string)            {}
func (h *pageHost) FileMenu(*ui.Menu, string)  {}
func (h *pageHost) EditorTheme(*ui.Context) *editor.Theme {
	return &editor.Theme{}
}
func (h *pageHost) EditorFont() (ui.Font, float32, int) { return ui.Font{}, 16, 4 }
func (h *pageHost) SwitchBranch(string, string) error   { return nil }
func (h *pageHost) CreateBranch(string, string) error   { return nil }
func (h *pageHost) ReloadFiles()                        {}
func (h *pageHost) AfterGitMutation()                   {}

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

func TestToFilePatchClipsAHugeLine(t *testing.T) {
	// A binary file Git prints as text arrives as one line of megabytes.
	// Passing it on whole made the layout of one frame take minutes.
	long := strings.Repeat("\xff", 1<<20)
	diff := "diff --git a/blob.bin b/blob.bin\n--- a/blob.bin\n+++ b/blob.bin\n@@ -1 +1 @@\n-" + long + "\n+" + long + "\n"
	files := SplitDiff(diff)
	if len(files) != 1 {
		t.Fatalf("files: %d", len(files))
	}
	p := toFilePatch(files[0])
	if len(p.Hunks) != 1 || len(p.Hunks[0].Lines) != 2 {
		t.Fatalf("hunks: %+v", p.Hunks)
	}
	for _, line := range p.Hunks[0].Lines {
		if n := utf8.RuneCountInString(line); n > diffLineRunes+2 {
			t.Fatalf("patch line has %d runes", n)
		}
		if line[0] != '+' && line[0] != '-' {
			t.Fatalf("the cut line lost its prefix: %q", line[:1])
		}
		if !strings.HasSuffix(line, "…") {
			t.Fatalf("the cut line has no ellipsis: %q", line[max(0, len(line)-8):])
		}
	}
	if p.OldName != "blob.bin" || p.NewName != "blob.bin" {
		t.Fatalf("names: %q %q", p.OldName, p.NewName)
	}
}

func TestToFilePatchKeepsPrefixes(t *testing.T) {
	diff := "diff --git a/a.txt b/a.txt\nnew file mode 100644\n--- /dev/null\n+++ b/a.txt\n" +
		"@@ -0,0 +1,2 @@ section\n+one\n+\n\\ No newline at end of file\n"
	files := SplitDiff(diff)
	if len(files) != 1 {
		t.Fatalf("files: %d", len(files))
	}
	p := toFilePatch(files[0])
	if p.OldName != "" || p.NewName != "a.txt" {
		t.Fatalf("names: %q %q", p.OldName, p.NewName)
	}
	if len(p.Hunks) != 1 || p.Hunks[0].Section != "section" {
		t.Fatalf("hunks: %+v", p.Hunks)
	}
	got := p.Hunks[0].Lines
	want := []string{"+one", "+", "\\ No newline at end of file"}
	if len(got) != len(want) {
		t.Fatalf("lines: %q", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("line %d: %q, want %q", i, got[i], want[i])
		}
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

func TestShowChangesDropsLateHistory(t *testing.T) {
	const working = "diff --git a/a.md b/a.md\n--- a/a.md\n+++ b/a.md\n@@ -1 +1 @@\n-old\n+working\n"
	runner := pageRunner(func(dir string, args ...string) (string, error) {
		command := strings.Join(args, " ")
		switch {
		case command == "rev-parse --show-toplevel":
			return dir + "\n", nil
		case strings.Contains(command, "rev-parse --abbrev-ref HEAD"):
			return "main\n", nil
		case strings.Contains(command, "status --porcelain=v1"):
			return " M a.md\x00", nil
		case strings.Contains(command, "diff") && strings.Contains(command, "HEAD"):
			return working, nil
		case strings.Contains(command, "show"):
			return "diff --git a/a.md b/a.md\n--- a/a.md\n+++ b/a.md\n@@ -1 +1 @@\n-old\n+commit\n", nil
		default:
			return "", nil
		}
	})
	h := &pageHost{dir: "/repo", runner: runner}
	p := Page{Mode: ModeHistory, HistorySeq: 1, HistoryLoad: true, h: h, runner: runner}
	historySeq := p.HistorySeq
	p.ShowChanges()
	if p.DiffPath != "a.md" || p.Diff != working {
		t.Fatalf("working diff before late history: path %q diff %q", p.DiffPath, p.Diff)
	}
	p.applyHistory(historySeq, HistoryData{
		Root: "/repo", Commits: []Commit{{Hash: "abc", Short: "abc"}},
	})
	if p.DiffPath != "a.md" || p.Diff != working || p.DiffLoad {
		t.Fatalf("late history replaced working diff: path %q diff %q loading %v", p.DiffPath, p.Diff, p.DiffLoad)
	}
}

func TestNonTextDiffUsesExplanationInsteadOfBlankViewer(t *testing.T) {
	cases := []string{
		"diff --git a/image.png b/image.png\nindex 111..222 100644\nBinary files a/image.png and b/image.png differ\n",
		"diff --git a/run.sh b/run.sh\nold mode 100644\nnew mode 100755\n",
	}
	for _, diff := range cases {
		h := &pageHost{}
		p := Page{
			Changes:  []Change{{Path: "file", Display: "file", Status: "M"}},
			DiffPath: "file", Diff: diff, diffGen: 1, h: h,
		}
		k := theme.Window(theme.DefaultID, false)
		tt := ui.NewTester(func(c *ui.Context) { p.View(c, k, h) }, 900, 600)
		tt.Frame()
		if p.diffView != nil {
			t.Fatalf("non-text diff created an empty viewer: %+v", p.diffView)
		}
		if !tt.HasText("No text changes") || !tt.HasText(strings.TrimSpace(diff)) {
			t.Fatalf("non-text explanation missing %q: %q", strings.TrimSpace(diff), tt.Texts())
		}
	}
}

func TestSplitDiffPathContainingNewPrefix(t *testing.T) {
	diff := "diff --git a/foo b/bar b/foo b/bar\n" +
		"index 111..222 100644\n" +
		"Binary files a/foo b/bar and b/foo b/bar differ\n"
	files := SplitDiff(diff)
	if len(files) != 1 || files[0].Path != "foo b/bar" {
		t.Fatalf("files: %+v", files)
	}
	if !strings.Contains(files[0].Note, "Binary files a/foo b/bar and b/foo b/bar differ") {
		t.Fatalf("binary explanation: %q", files[0].Note)
	}
}
