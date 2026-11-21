package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"mojian/internal/fs"
	"mojian/internal/project"

	"github.com/egoist/mygo/ui"
)

func TestSaveCommandShowsExternalConflict(t *testing.T) {
	a := newProjectApp(t)
	n := a.currentNote()
	n.Buffer().Set("local edit\n")
	if err := os.WriteFile(n.Path, []byte("external edit\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !a.commandRegistry().Run("save") {
		t.Fatal("the save command was not registered")
	}
	if !strings.Contains(a.err, "file changed outside mojian") || !strings.Contains(a.err, n.Rel) {
		t.Fatalf("save did not identify the conflict: %q", a.err)
	}
	tt := ui.NewTester(a.view, 1100, 700)
	tt.Frame()
	if !tt.HasText(a.err) {
		t.Fatalf("conflict banner was not rendered: %q", tt.Texts())
	}
	data, err := os.ReadFile(n.Path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "external edit\n" || !n.Dirty() || n.Text() != "local edit\n" {
		t.Fatal("the save command lost external or unsaved local edits")
	}
}

type blockingRemoteBackend struct {
	*fs.Direct
	started chan struct{}
	unblock chan struct{}
	once    sync.Once
}

func newBlockingRemoteBackend(root string) *blockingRemoteBackend {
	return &blockingRemoteBackend{
		Direct:  fs.NewDirect(root),
		started: make(chan struct{}),
		unblock: make(chan struct{}),
	}
}

func (b *blockingRemoteBackend) Kind() fs.Kind { return fs.KindServerRemote }

func (b *blockingRemoteBackend) ReadContext(ctx context.Context, rel string) ([]byte, error) {
	data, err := b.Direct.Read(rel)
	if err != nil {
		return nil, err
	}
	b.once.Do(func() { close(b.started) })
	select {
	case <-b.unblock:
		return data, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (b *blockingRemoteBackend) WriteContext(ctx context.Context, rel string, data []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return b.Direct.Write(rel, data)
}

func (b *blockingRemoteBackend) Trash(rel string) error { return b.Direct.Remove(rel) }

func newBlockingSaveStore(t *testing.T, body string) (*store, *blockingRemoteBackend, *project.Note) {
	t.Helper()
	root := t.TempDir()
	name := filepath.Join(root, "note.md")
	if err := os.WriteFile(name, []byte("original\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	backend := newBlockingRemoteBackend(root)
	s := project.New(backend)
	n := &project.Note{Rel: "note.md", Path: name, Title: "note", Saved: "original\n", Loaded: true}
	s.Add(n)
	n.Buffer().Set(body)
	return s, backend, n
}

func waitSaveSignal(t *testing.T, ch <-chan struct{}) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for remote save")
	}
}

func waitSavePost(t *testing.T, ch <-chan func()) func() {
	t.Helper()
	select {
	case fn := <-ch:
		return fn
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for save completion")
		return nil
	}
}

func TestRemoteSaveQueueStoresSerially(t *testing.T) {
	first, firstBackend, _ := newBlockingSaveStore(t, "first saved\n")
	second, secondBackend, _ := newBlockingSaveStore(t, "second saved\n")
	posts := make(chan func(), 4)
	a := &app{
		workspace: &workspace{store: first},
		native:    true,
		uiPost:    func(fn func()) { posts <- fn },
	}
	firstDone, secondDone := false, false
	a.saveNowAsync(func(err error) {
		if err != nil {
			t.Error(err)
		}
		firstDone = true
	})
	waitSaveSignal(t, firstBackend.started)
	a.workspace = &workspace{store: second}
	a.saveNowAsync(func(err error) {
		if err != nil {
			t.Error(err)
		}
		secondDone = true
	})
	select {
	case <-secondBackend.started:
		t.Fatal("the second store started before the first finished")
	default:
	}
	close(firstBackend.unblock)
	waitSavePost(t, posts)()
	if !firstDone || secondDone {
		t.Fatalf("first completion released the wrong waiters: first=%v second=%v", firstDone, secondDone)
	}
	waitSaveSignal(t, secondBackend.started)
	close(secondBackend.unblock)
	waitSavePost(t, posts)()
	if !secondDone || a.saveTask.active || len(a.saveWaiters) != 0 {
		t.Fatalf("second store was not saved: done=%v active=%v waiters=%d", secondDone, a.saveTask.active, len(a.saveWaiters))
	}
	for name, want := range map[string]string{
		filepath.Join(first.Dir, "note.md"):  "first saved\n",
		filepath.Join(second.Dir, "note.md"): "second saved\n",
	} {
		data, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		if string(data) != want {
			t.Fatalf("%s = %q, want %q", name, data, want)
		}
	}
}

func TestRemoteSaveDoesNotOverwriteRecreatedPath(t *testing.T) {
	s, backend, original := newBlockingSaveStore(t, "changed\n")
	posts := make(chan func(), 4)
	a := &app{
		workspace: &workspace{store: s},
		native:    true,
		uiPost:    func(fn func()) { posts <- fn },
	}
	done := false
	a.saveNowAsync(func(err error) {
		if err != nil {
			t.Error(err)
		}
		done = true
	})
	waitSaveSignal(t, backend.started)
	newRel, _, err := s.Rename("note.md", "renamed.md")
	if err != nil {
		t.Fatal(err)
	}
	replacement, err := s.NewFileIn("", "note.md", "replacement\n")
	if err != nil {
		t.Fatal(err)
	}
	close(backend.unblock)
	waitSavePost(t, posts)()
	waitSavePost(t, posts)()
	if !done || a.saveTask.active {
		t.Fatal("the current-path save did not finish")
	}
	if original.Rel != newRel || original.Dirty() {
		t.Fatalf("renamed note was not saved at %q: dirty=%v", original.Rel, original.Dirty())
	}
	for rel, want := range map[string]string{
		"note.md":    "replacement\n",
		"renamed.md": "changed\n",
	} {
		data, err := os.ReadFile(filepath.Join(s.Dir, rel))
		if err != nil {
			t.Fatal(err)
		}
		if string(data) != want {
			t.Fatalf("%s = %q, want %q", rel, data, want)
		}
	}
	if replacement.Dirty() || s.ByRelPath("note.md") != replacement {
		t.Fatal("the replacement note was changed by the old snapshot")
	}
}

func TestRemoteSaveDoesNotRecreateTrashedPath(t *testing.T) {
	s, backend, _ := newBlockingSaveStore(t, "changed\n")
	posts := make(chan func(), 2)
	a := &app{
		workspace: &workspace{store: s},
		native:    true,
		uiPost:    func(fn func()) { posts <- fn },
	}
	done := false
	a.saveNowAsync(func(err error) {
		if err != nil {
			t.Error(err)
		}
		done = true
	})
	waitSaveSignal(t, backend.started)
	if err := s.Trash("note.md"); err != nil {
		t.Fatal(err)
	}
	close(backend.unblock)
	waitSavePost(t, posts)()
	if !done || a.saveTask.active {
		t.Fatal("the save did not finish after the trash")
	}
	if _, err := os.Stat(filepath.Join(s.Dir, "note.md")); !os.IsNotExist(err) {
		t.Fatalf("the old snapshot recreated the trashed file: %v", err)
	}
}
