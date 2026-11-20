package app

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/egoist/mygo/ui"
)

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
