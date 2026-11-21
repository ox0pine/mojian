package app

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"mojian/internal/fs"
	"mojian/internal/project"
)

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
	b.once.Do(func() { close(b.started) })
	select {
	case <-b.unblock:
		return b.Direct.Read(rel)
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
