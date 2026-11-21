package app

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mojian/internal/platform"

	"github.com/egoist/mygo/ui"
)

func TestEveryShortcutHasABrowserSpelling(t *testing.T) {
	a := &app{workspace: &workspace{}}
	for _, c := range paletteCommands {
		got := platform.NativeKeysFor("js", a.commandKeys(c))
		if strings.ContainsAny(got, "⌘⇧⌥⌃") {
			t.Errorf("%s: %q was not translated", c.Title, got)
		}
	}
}

func TestTabRenameWhitespaceRestoresAutomaticName(t *testing.T) {
	registerFonts()
	a := newProjectApp(t)
	tb := a.tab()
	tb.Name = "UniqueTab"
	tt := ui.NewTester(a.view, 1100, 700)
	a.startRename(tb)
	tt.Frame()
	tt.Frame()
	a.renameText = " \t "
	tt.Frame()
	tt.Key(0, ui.KeyEnter)
	tt.Frame()
	if tb.Name != "" {
		t.Fatalf("blank rename left a whitespace-only name: %q", tb.Name)
	}
	if a.rename != nil {
		t.Fatal("submitting the blank name left rename mode active")
	}
}

func TestTabRenameAlignment(t *testing.T) {
	registerFonts()
	a := newProjectApp(t)
	tb := a.tab()
	tb.Name = "UniqueTab"
	tt := ui.NewTester(a.view, 1100, 700)
	tt.Frame()
	label, ok := tt.Find("UniqueTab")
	if !ok {
		t.Fatalf("tab label not found: %q", tt.Texts())
	}
	a.startRename(tb)
	tt.Frame()
	tt.Frame()
	caret, ok := tt.TextCaret()
	if !ok {
		t.Fatalf("rename input is not focused: %q", tt.Texts())
	}
	labelCenter := label.Y + label.H/2
	caretCenter := caret.Y + caret.H/2
	if diff := labelCenter - caretCenter; diff > 1 || diff < -1 {
		t.Fatalf("rename text is %.2f px off the tab label (label %.2f, caret %.2f)", diff, labelCenter, caretCenter)
	}
}

func TestActiveTabCloseStaysVisibleAtMinimumWidths(t *testing.T) {
	registerFonts()
	for _, width := range []int{800, 760} {
		a := newProjectApp(t)
		a.workspace.project = "/tmp/Client research — product & editorial workspace"
		a.serverAlias = "build-server-with-a-long-name"
		a.branch.loaded = true
		a.branch.data.Root = "/tmp/repository"
		a.branch.data.Current = "feature/a-very-long-branch-name"
		tt := ui.NewTester(a.view, width, 460)
		tt.Frame()
		tt.Frame()
		closeTab, ok := tt.Find("Close Tab")
		if !ok {
			t.Fatalf("width %d: close tab control is missing", width)
		}
		if closeTab.W < 17 || closeTab.H < 17 || closeTab.X < 0 || closeTab.X+closeTab.W > float32(width) {
			t.Fatalf("width %d: close tab is clipped: %+v", width, closeTab)
		}
	}
}

func TestTitleBarCompactsBranchBeforeContext(t *testing.T) {
	registerFonts()
	a := newProjectApp(t)
	a.store.Dir = "/tmp/Client research"
	a.serverAlias = "build-server-with-a-long-name"
	const branch = "feature/research-editorial-review-november-2026"
	a.branch.loaded = true
	a.branch.data.Root = "/tmp/repository"
	a.branch.data.Current = branch
	tt := ui.NewTester(a.view, 760, 460)
	tt.Frame()
	tt.Frame()
	chip, ok := tt.Find("Switch branch: " + branch)
	if !ok || chip.W > 56 {
		t.Fatalf("long branch did not yield to the project context: %+v", chip)
	}
	server, ok := tt.Find(a.serverAlias)
	if !ok || server.W < 24 {
		t.Fatalf("server name remained squeezed out: %+v", server)
	}
	project, ok := tt.Find("Client research")
	if !ok || project.W < 24 {
		t.Fatalf("project name remained squeezed out: %+v", project)
	}
	a.git.Open = true
	tt.Frame()
	if tt.HasText("Switch branch: " + branch) {
		t.Fatal("title bar duplicated the Git page's branch control")
	}
}

// The welcome page's Open File row opens a picker over the project.
// Focusing the Explorer's search field looked like nothing happened when
// the tree was already open.
func TestWelcomeOpenFileShowsAPicker(t *testing.T) {
	registerFonts()
	a := newProjectApp(t)
	a.treeOpen = false
	tt := ui.NewTester(a.view, 1100, 700)
	tt.Frame()
	if err := tt.Click("New Tab"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("Open File…"); err != nil {
		t.Fatal(err)
	}
	if !a.quickOpen {
		t.Fatal("Open File did not show the picker")
	}
	if !tt.Focused("Open a file") {
		t.Fatal("the picker's field did not take focus")
	}
	tt.Type("tasks")
	tt.Frame()
	tt.Key(0, ui.KeyEnter)
	tt.Frame()
	tt.Frame()
	if a.quickOpen {
		t.Fatal("the picker stayed open after Enter")
	}
	n := a.currentNote()
	if n == nil || n.Rel != filepath.Join("scripts", "tasks.py") {
		t.Fatalf("opened note: %+v", n)
	}
	if name, _ := a.tab().label(); name != "tasks.py" {
		t.Fatalf("active tab: %q", name)
	}
	// The other tab still shows Welcome.md, so check the welcome page's
	// own subtitle rather than its heading.
	if tt.HasText("A quiet editor for the files in your project.") {
		t.Fatal("the welcome page stayed after opening a file")
	}
}

func TestQuickOpenEscapeCloses(t *testing.T) {
	registerFonts()
	a := newProjectApp(t)
	tt := ui.NewTester(a.view, 1100, 700)
	tt.Frame()
	before := a.currentNote()
	a.openQuickOpen()
	tt.Frame()
	if !a.quickOpen {
		t.Fatal("openQuickOpen did not show the picker")
	}
	tt.Key(0, ui.KeyEscape)
	tt.Frame()
	if a.quickOpen {
		t.Fatal("Escape did not close the picker")
	}
	if a.currentNote() != before {
		t.Fatal("Escape changed the open file")
	}
}

var commandSheetTestSizes = []struct {
	width  int
	height int
}{
	{width: 800, height: 500},
	{width: 760, height: 460},
}

func newCommandSheetApp(t *testing.T) *app {
	t.Helper()
	a := newProjectApp(t)
	for i := range 24 {
		name := fmt.Sprintf("sheet-%02d.md", i)
		if err := os.WriteFile(filepath.Join(a.store.Dir, name), []byte("# "+name+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := a.store.Refresh(); err != nil {
		t.Fatal(err)
	}
	return a
}

func commandSheetRow(tt *ui.Tester, name string, dialog ui.Rect) (ui.AccessInfo, bool) {
	var best ui.AccessInfo
	found := false
	for _, candidate := range tt.AccessAll() {
		if candidate.Name != name {
			continue
		}
		b := candidate.Bounds
		if b.W <= 0 || b.H <= 0 || b.Y+b.H < dialog.Y || b.Y > dialog.Y+dialog.H {
			continue
		}
		if !found || b.H > best.Bounds.H {
			best = candidate
			found = true
		}
	}
	return best, found
}

func assertCommandSheetRow(t *testing.T, tt *ui.Tester, name string, height int) ui.Rect {
	t.Helper()
	dialog, ok := tt.AccessByRole(ui.RoleDialog, "")
	if !ok {
		t.Fatal("no command sheet dialog")
	}
	panelBottom := dialog.Bounds.Y + dialog.Bounds.H
	if bottomLimit := float32(height - 24); panelBottom > bottomLimit+0.5 {
		t.Fatalf("dialog bottom %.1f exceeds %.1f", panelBottom, bottomLimit)
	}
	row, ok := commandSheetRow(tt, name, dialog.Bounds)
	if !ok {
		t.Fatalf("no visible row %q in dialog %+v", name, dialog.Bounds)
	}
	if row.Bounds.Y < dialog.Bounds.Y || row.Bounds.Y+row.Bounds.H > panelBottom+0.5 {
		t.Fatalf("row %q at %+v escapes dialog %+v", name, row.Bounds, dialog.Bounds)
	}
	if row.Role == ui.RoleButton && (row.Bounds.H < rowH-0.5 || row.Bounds.H > rowH+0.5) {
		t.Fatalf("row %q is clipped: height %.1f, want %d", name, row.Bounds.H, rowH)
	}
	return row.Bounds
}

func TestPaletteFitsSmallWindows(t *testing.T) {
	registerFonts()
	for _, size := range commandSheetTestSizes {
		a := newCommandSheetApp(t)
		a.openPalette()
		items := a.paletteItems()
		if len(items) < 12 {
			t.Fatalf("only %d palette items", len(items))
		}
		tt := ui.NewTester(func(c *ui.Context) {
			a.palette(c, a.colorsOf(c))
		}, size.width, size.height)
		tt.Frame()
		for range len(items) - 1 {
			tt.Key(0, ui.KeyDown)
		}
		if a.paletteSel != len(items)-1 {
			t.Fatalf("palette selection %d at %dx%d, want %d", a.paletteSel, size.width, size.height, len(items)-1)
		}
		last := items[len(items)-1]
		row := assertCommandSheetRow(t, tt, last.title, size.height)
		tt.ClickAt(row.X+row.W/2, row.Y+row.H/2)
		if a.paletteOpen {
			t.Fatalf("clicking the revealed palette row did not choose it at %dx%d", size.width, size.height)
		}
	}
}

func TestQuickOpenFitsSmallWindows(t *testing.T) {
	registerFonts()
	for _, size := range commandSheetTestSizes {
		a := newCommandSheetApp(t)
		a.openQuickOpen()
		choices := a.paneChoices(a.quickQuery)
		if len(choices) < 12 {
			t.Fatalf("only %d quick open choices", len(choices))
		}
		tt := ui.NewTester(func(c *ui.Context) {
			a.quickOpenModal(c, a.colorsOf(c))
		}, size.width, size.height)
		tt.Frame()
		for range len(choices) - 1 {
			tt.Key(0, ui.KeyDown)
		}
		if a.quickSel != len(choices)-1 {
			t.Fatalf("quick open selection %d at %dx%d, want %d", a.quickSel, size.width, size.height, len(choices)-1)
		}
		last := choices[len(choices)-1]
		row := assertCommandSheetRow(t, tt, last.title, size.height)
		tt.ClickAt(row.X+row.W/2, row.Y+row.H/2)
		if a.quickOpen {
			t.Fatalf("clicking the revealed quick open row did not choose it at %dx%d", size.width, size.height)
		}
	}
}

func TestSSHPickerFitsSmallWindows(t *testing.T) {
	if !remoteSupported() {
		t.Skip("SSH is unavailable")
	}
	registerFonts()
	for _, size := range commandSheetTestSizes {
		a := newCommandSheetApp(t)
		a.settingsPath = ""
		hosts := make([]hostConfig, 24)
		for i := range hosts {
			hosts[i] = hostConfig{Name: fmt.Sprintf("Host %02d", i), Address: fmt.Sprintf("host-%02d", i)}
		}
		a.hostService().SetHosts(hosts)
		a.openSSHPicker()
		tt := ui.NewTester(func(c *ui.Context) {
			a.sshPicker(c, a.colorsOf(c))
		}, size.width, size.height)
		tt.Frame()
		for range len(hosts) - 1 {
			tt.Key(0, ui.KeyDown)
		}
		if a.sshSel != len(hosts)-1 {
			t.Fatalf("SSH selection %d at %dx%d, want %d", a.sshSel, size.width, size.height, len(hosts)-1)
		}
		last := hosts[len(hosts)-1]
		assertCommandSheetRow(t, tt, last.Name+" "+last.Address, size.height)
	}
}
