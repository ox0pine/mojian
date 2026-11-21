package app

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"mojian/internal/testutil"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

func TestEditorDiffBaseLoadsAndInvalidates(t *testing.T) {
	registerFonts()
	dir := t.TempDir()
	testutil.WriteVault(t, dir, map[string]string{"note.go": "package x\n\nvar a = 1\n"})
	testutil.InitRepo(t, dir)

	a := &app{workspace: &workspace{treeOpen: false}}
	s, err := openStoreAt(dir)
	if err != nil {
		t.Fatal(err)
	}
	a.store = s
	a.focusFirstNote()
	tt := ui.NewTester(a.view, 1100, 700)
	// First frame queues the HEAD read via a.later; second frame runs it and binds DiffBase.
	tt.Frame()
	tt.Frame()
	tt.Frame()

	p := a.focusPane()
	if p == nil || p.state == nil || p.state.DiffBase == nil {
		t.Fatalf("expected DiffBase loaded for tracked file, got pane=%+v", p)
	}
	if got := p.state.DiffBase.String(); got != "package x\n\nvar a = 1\n" {
		t.Fatalf("DiffBase = %q, want HEAD content", got)
	}

	// Untracked file gets a nil DiffBase without errors.
	if err := os.WriteFile(filepath.Join(dir, "untracked.go"), []byte("package u\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := a.store.Refresh(); err != nil {
		t.Fatal(err)
	}
	a.openRel("untracked.go")
	tt.Frame()
	tt.Frame()
	tt.Frame()
	if a.focusPane().state.DiffBase != nil {
		t.Fatalf("expected nil DiffBase for untracked file")
	}
}

type stubWatcher struct {
	ch     chan []mygo.FileEvent
	closed chan struct{}
}

func (w *stubWatcher) Next(ctx context.Context) ([]mygo.FileEvent, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-w.closed:
		return nil, io.EOF
	case evs := <-w.ch:
		return evs, nil
	}
}

func (w *stubWatcher) Close() error {
	select {
	case <-w.closed:
	default:
		close(w.closed)
	}
	return nil
}

func TestProjectWatcherReloadsCleanAndKeepsDirty(t *testing.T) {
	registerFonts()
	a := newProjectApp(t)
	if err := os.WriteFile(filepath.Join(a.store.Dir, "clean.md"), []byte("v1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(a.store.Dir, "dirty.md"), []byte("disk1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := a.store.Refresh(); err != nil {
		t.Fatal(err)
	}
	clean := a.store.ByRelPath("clean.md")
	dirty := a.store.ByRelPath("dirty.md")
	a.openNote(clean)
	dirty.Buffer().Set("unsaved local\n")

	w := &stubWatcher{ch: make(chan []mygo.FileEvent, 1), closed: make(chan struct{})}
	posted := make(chan func(), 4)
	a.uiPost = func(fn func()) { posted <- fn }
	a.watchStarter = func(context.Context, string, func(string, bool) bool) (fileWatcher, error) {
		return w, nil
	}
	a.syncProjectWatcher()
	defer a.stopProjectWatcher()

	if err := os.WriteFile(filepath.Join(a.store.Dir, "clean.md"), []byte("v2 external\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(a.store.Dir, "dirty.md"), []byte("disk2 external\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(a.store.Dir, "added.md"), []byte("brand new\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	w.ch <- []mygo.FileEvent{{Path: "."}}

	select {
	case fn := <-posted:
		fn()
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for watcher callback")
	}

	if got := clean.Text(); got != "v2 external\n" {
		t.Fatalf("clean note = %q, want external update", got)
	}
	if got := dirty.Text(); got != "unsaved local\n" {
		t.Fatalf("dirty note overwritten: %q", got)
	}
	if a.store.ByRelPath("added.md") == nil {
		t.Fatal("newly created file was not picked up by watcher")
	}
}
