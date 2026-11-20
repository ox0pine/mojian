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
