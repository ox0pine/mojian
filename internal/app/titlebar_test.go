package app

import (
	"testing"

	"github.com/egoist/mygo/ui"
)

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
