package git

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mojian/internal/testutil"
	"mojian/internal/theme"

	"github.com/egoist/mygo/ui"
)

func newMixedStatePage(t *testing.T) (*Page, string) {
	t.Helper()
	theme.RegisterFonts()
	dir := t.TempDir()
	lines := make([]string, 240)
	for i := range lines {
		lines[i] = fmt.Sprintf("line %03d", i)
	}
	testutil.WriteVault(t, dir, map[string]string{"a.md": strings.Join(lines, "\n") + "\n"})
	testutil.InitRepo(t, dir)
	lines[100] = "staged B"
	lines[200] = "staged C"
	if err := os.WriteFile(filepath.Join(dir, "a.md"), []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Stage(LocalRunner{}, dir, "a.md"); err != nil {
		t.Fatal(err)
	}
	lines[10] = "unstaged A"
	if err := os.WriteFile(filepath.Join(dir, "a.md"), []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	h := &pageHost{dir: dir, runner: LocalRunner{}}
	p := &Page{h: h, runner: h.runner}
	p.Refresh()
	if p.DiffStaged || len(SplitHunks(p.Diff)) != 1 || !strings.Contains(p.Diff, "+unstaged A") {
		t.Fatalf("default mixed diff: staged=%v diff=%q", p.DiffStaged, p.Diff)
	}
	return p, dir
}

func TestMixedFileActionUsesVisiblePatch(t *testing.T) {
	p, dir := newMixedStatePage(t)
	p.showStagedDiff(true)
	if !p.DiffStaged || len(SplitHunks(p.Diff)) != 2 ||
		!strings.Contains(p.Diff, "+staged B") || !strings.Contains(p.Diff, "+staged C") || strings.Contains(p.Diff, "unstaged A") {
		t.Fatalf("staged mixed diff: %q", p.Diff)
	}
	// The visible staged list is B,C. Unstaging its first hunk must remove B,
	// never reinterpret index zero in the combined or unstaged diff.
	p.UnstageHunkSelected(0)
	if p.ActionErr != "" {
		t.Fatal(p.ActionErr)
	}
	cached, err := LocalRunner{}.Run(dir, "diff", "--cached", "--", "a.md")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(cached, "+staged B") || !strings.Contains(cached, "+staged C") {
		t.Fatalf("wrong staged hunk removed: B=%v C=%v", strings.Contains(cached, "+staged B"), strings.Contains(cached, "+staged C"))
	}
}

func TestHunkNavigationMapsGitHunksToViewerRuns(t *testing.T) {
	raw := "diff --git a/a.md b/a.md\n--- a/a.md\n+++ b/a.md\n" +
		"@@ -1,5 +1,5 @@\n one\n-old two\n+new two\n three\n-old four\n+new four\n five\n" +
		"@@ -10,3 +10,3 @@\n nine\n-old ten\n+new ten\n eleven\n"
	h := &pageHost{}
	p := &Page{Changes: []Change{{Path: "a.md", Display: "a.md", Status: "M", Unstaged: true}}, Sel: 0,
		DiffPath: "a.md", Diff: raw, diffGen: 1, h: h}
	k := theme.Window(theme.DefaultID, false)
	tt := ui.NewTester(func(c *ui.Context) { p.View(c, k, h) }, 900, 600)
	for range 2 {
		tt.Frame()
	}
	if len(p.hunkRuns) != 2 || len(p.diffView.Hunks()) != 3 {
		t.Fatalf("mappings=%+v runs=%d", p.hunkRuns, len(p.diffView.Hunks()))
	}
	if err := tt.Click("Next Hunk"); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		tt.Frame()
	}
	current, inside := p.diffView.CurrentHunk()
	if p.HunkSel != 1 || current != 2 || !inside || p.gitHunkForRun(current) != 1 || !tt.HasText("Hunk 2/2") {
		t.Fatalf("selected Git hunk=%d viewer run=%d inside=%v", p.HunkSel, current, inside)
	}
}

func TestHunkScrollWinsOverPreviousCaret(t *testing.T) {
	p, tt := newTwoHunkPage(t)
	if err := tt.Click("Next Hunk"); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		tt.Frame()
	}
	if err := tt.Click("Previous Hunk"); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		tt.Frame()
	}
	tt.Scroll(600, 350, 0, 50000)
	for range 3 {
		tt.Frame()
		if p.HunkSel != 1 {
			t.Fatalf("selection returned to old caret: %d", p.HunkSel)
		}
	}
}

func TestNarrowWindowKeepsActionsReachable(t *testing.T) {
	p, _ := newMixedStatePage(t)
	longName := "docs/releases/2026/November/release-notes-for-the-editor-and-terminal-integration.md"
	p.Changes[0].Display = longName
	p.DiffPath = longName
	k := theme.Window(theme.DefaultID, false)
	tt := ui.NewTester(func(c *ui.Context) { p.View(c, k, p.h) }, 760, 460)
	for range 2 {
		tt.Frame()
	}
	if err := tt.Click("File Actions"); err != nil {
		t.Fatal(err)
	}
	if !tt.HasText("Unstage") {
		t.Fatalf("file menu: %q", tt.Texts())
	}
	if err := tt.Click("File Actions"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if err := tt.Click("Hunk Actions"); err != nil {
		t.Fatal(err)
	}
	if !tt.HasText("Stage Hunk") || !tt.HasText("Discard Hunk") {
		t.Fatalf("hunk menu: %q", tt.Texts())
	}
	if err := tt.Click("Stage Hunk"); err != nil {
		t.Fatal(err)
	}
	if p.ActionErr != "" {
		t.Fatal(p.ActionErr)
	}
}

func TestStatusRefreshPreservesSelectedPath(t *testing.T) {
	runner := pageRunner(func(string, ...string) (string, error) { return "", nil })
	h := &pageHost{dir: "/repo", runner: runner}
	p := &Page{
		Changes: []Change{{Path: "a.md", Display: "a.md", Status: "M"}, {Path: "z.txt", Display: "z.txt", Status: "U"}},
		Sel:     1, h: h, runner: runner,
	}
	p.applyStatus(0, StatusData{Root: "/repo", Changes: []Change{
		{Path: "a.md", Display: "a.md", Status: "M"},
		{Path: "m.md", Display: "m.md", Status: "M"},
		{Path: "z.txt", Display: "z.txt", Status: "U"},
	}})
	if p.Sel != 2 || p.DiffPath != "z.txt" {
		t.Fatalf("selection moved to another file: sel=%d path=%q", p.Sel, p.DiffPath)
	}
}
