package project

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mojian/internal/fs"
)

func newSaveStore(t *testing.T, body string) (*Store, *Note) {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "note.txt"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	s := New(fs.NewDirect(root))
	if err := s.Refresh(); err != nil {
		t.Fatal(err)
	}
	n := s.ByRelPath("note.txt")
	if n == nil {
		t.Fatal("the fixture note was not listed")
	}
	return s, n
}

func writeSaveFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func assertSaveFile(t *testing.T, path, want string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != want {
		t.Fatalf("file = %q, want %q", data, want)
	}
}

func TestSaveRejectsExternalChange(t *testing.T) {
	s, n := newSaveStore(t, "original\n")
	n.Buffer().Set("local edit\n")
	writeSaveFile(t, n.Path, "external edit\n")
	err := s.SaveAll()
	if !errors.Is(err, ErrFileChanged) || !strings.Contains(err.Error(), "note.txt") {
		t.Fatalf("expected a conflict identifying the file, got %v", err)
	}
	assertSaveFile(t, n.Path, "external edit\n")
	if !n.Dirty() || n.Text() != "local edit\n" || n.Saved != "original\n" {
		t.Fatalf("conflict lost the local edit or its baseline: %+v", n)
	}
	// Restoring the baseline lets a later explicit save succeed.
	writeSaveFile(t, n.Path, "original\n")
	if err := s.SaveAll(); err != nil {
		t.Fatal(err)
	}
	assertSaveFile(t, n.Path, "local edit\n")
	if n.Dirty() {
		t.Fatal("successful retry left the note dirty")
	}
}

func TestRefreshKeepsDirtySaveBaseline(t *testing.T) {
	s, n := newSaveStore(t, "original\n")
	n.Buffer().Set("local edit\n")
	info, err := os.Stat(n.Path)
	if err != nil {
		t.Fatal(err)
	}
	// Same-length content and unchanged mtime still constitute a conflict.
	writeSaveFile(t, n.Path, "external\n")
	if err := os.Chtimes(n.Path, info.ModTime(), info.ModTime()); err != nil {
		t.Fatal(err)
	}
	if err := s.Refresh(); err != nil {
		t.Fatal(err)
	}
	if n.Saved != "original\n" || n.Text() != "local edit\n" {
		t.Fatal("refresh replaced a dirty buffer or its saved baseline")
	}
	if err := s.SaveAll(); err == nil {
		t.Fatal("refresh allowed overwriting an external edit")
	}
	assertSaveFile(t, n.Path, "external\n")
}

func TestRefreshReloadsCleanSharedBuffer(t *testing.T) {
	s, n := newSaveStore(t, "original\n")
	buf := n.Buffer()
	writeSaveFile(t, n.Path, "external\r\n")
	if err := s.Refresh(); err != nil {
		t.Fatal(err)
	}
	if s.ByRelPath(n.Rel) != n || n.Buf != buf {
		t.Fatal("refresh replaced the shared note or buffer")
	}
	if n.Text() != "external\n" || n.Saved != "external\r\n" || n.Dirty() {
		t.Fatalf("clean note was not reloaded: text=%q saved=%q dirty=%v", n.Text(), n.Saved, n.Dirty())
	}
	if buf.LineEnding() != "\r\n" {
		t.Fatal("reload lost the external file's line ending")
	}
	buf.Set("local edit\n")
	if err := s.SaveAll(); err != nil {
		t.Fatal(err)
	}
	assertSaveFile(t, n.Path, "local edit\r\n")
}

func TestSavePreservesCRLFAcrossRepeatedEdits(t *testing.T) {
	s, n := newSaveStore(t, "first\r\nsecond\r\n")
	buf := n.Buffer()
	buf.Set("edited first\nsecond\n")
	if err := s.SaveAll(); err != nil {
		t.Fatal(err)
	}
	assertSaveFile(t, n.Path, "edited first\r\nsecond\r\n")
	if n.Saved != "edited first\r\nsecond\r\n" || n.Dirty() {
		t.Fatal("save did not update the on-disk baseline")
	}
	if err := s.Refresh(); err != nil {
		t.Fatal(err)
	}
	buf.Set("edited twice\nsecond\n")
	if err := s.SaveAll(); err != nil {
		t.Fatal(err)
	}
	assertSaveFile(t, n.Path, "edited twice\r\nsecond\r\n")
	if n.Dirty() {
		t.Fatal("second save left the note dirty")
	}
}

func TestSavePreservesLFAndAcceptsUnchangedContent(t *testing.T) {
	s, n := newSaveStore(t, "first\nsecond\n")
	n.Buffer().Set("edited\nsecond\n")
	// A rewrite of identical bytes must not count as a conflict.
	writeSaveFile(t, n.Path, "first\nsecond\n")
	if err := s.SaveAll(); err != nil {
		t.Fatal(err)
	}
	assertSaveFile(t, n.Path, "edited\nsecond\n")
	if n.Dirty() {
		t.Fatal("LF note stayed dirty")
	}
}

func TestSaveDoesNotRecreateExternallyDeletedFile(t *testing.T) {
	s, n := newSaveStore(t, "original\n")
	n.Buffer().Set("local edit\n")
	if err := os.Remove(n.Path); err != nil {
		t.Fatal(err)
	}
	if err := s.Refresh(); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveAll(); err == nil {
		t.Fatal("save silently recreated an externally deleted file")
	}
	if _, err := os.Stat(n.Path); !os.IsNotExist(err) {
		t.Fatalf("deleted file was recreated: %v", err)
	}
	if s.ByRelPath(n.Rel) != n || !n.Dirty() || n.Text() != "local edit\n" {
		t.Fatal("the deleted file's unsaved edit was lost")
	}
}

func TestCommitSnapshotKeepsNewerEditDirty(t *testing.T) {
	s, n := newSaveStore(t, "original\n")
	n.Buffer().Set("first edit\n")
	snapshot, changed, err := s.SnapshotSave(n)
	if err != nil || !changed {
		t.Fatalf("snapshot: changed=%v err=%v", changed, err)
	}
	if err := s.WriteSnapshot(t.Context(), snapshot); err != nil {
		t.Fatal(err)
	}
	n.Buffer().Set("second edit\n")
	s.CommitSnapshot(snapshot)
	if n.Saved != "first edit\n" || n.Text() != "second edit\n" || !n.Dirty() {
		t.Fatalf("new edit was marked saved: text=%q saved=%q dirty=%v", n.Text(), n.Saved, n.Dirty())
	}
	assertSaveFile(t, n.Path, "first edit\n")
	if err := s.Save(n); err != nil {
		t.Fatal(err)
	}
	assertSaveFile(t, n.Path, "second edit\n")
	if n.Dirty() {
		t.Fatal("the follow-up save left the note dirty")
	}
}

func TestMoveRelInvalidatesSaveSnapshot(t *testing.T) {
	s, n := newSaveStore(t, "original\n")
	if _, err := s.NewFolder("", "folder"); err != nil {
		t.Fatal(err)
	}
	n.Buffer().Set("local edit\n")
	snapshot, changed, err := s.SnapshotSave(n)
	if err != nil || !changed {
		t.Fatalf("snapshot: changed=%v err=%v", changed, err)
	}
	if _, err := s.MoveRel("note.txt", "folder"); err != nil {
		t.Fatal(err)
	}
	if err := s.WriteSnapshot(t.Context(), snapshot); !errors.Is(err, ErrSaveSnapshotStale) {
		t.Fatalf("the pre-move snapshot returned %v, want ErrSaveSnapshotStale", err)
	}
	if _, err := os.Stat(filepath.Join(s.Dir, "note.txt")); !os.IsNotExist(err) {
		t.Fatalf("the stale save recreated the old path: %v", err)
	}
	assertSaveFile(t, filepath.Join(s.Dir, "folder", "note.txt"), "original\n")
}

type blockingWriteBackend struct {
	*fs.Direct
	writeStarted chan struct{}
	allowWrite   chan struct{}
	recreated    chan struct{}
}

func (b *blockingWriteBackend) WriteContext(ctx context.Context, rel string, data []byte) error {
	close(b.writeStarted)
	select {
	case <-b.allowWrite:
		return b.Direct.Write(rel, data)
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (b *blockingWriteBackend) Write(rel string, data []byte) error {
	err := b.Direct.Write(rel, data)
	if err == nil && rel == "note.txt" {
		select {
		case b.recreated <- struct{}{}:
		default:
		}
	}
	return err
}

func TestSaveCannotOverwriteRecreatedPath(t *testing.T) {
	s, n := newSaveStore(t, "original\n")
	backend := &blockingWriteBackend{
		Direct: s.FS.(*fs.Direct), writeStarted: make(chan struct{}),
		allowWrite: make(chan struct{}), recreated: make(chan struct{}, 1),
	}
	s.FS = backend
	n.Buffer().Set("old edit\n")
	snapshot, changed, err := s.SnapshotSave(n)
	if err != nil || !changed {
		t.Fatalf("snapshot: changed=%v err=%v", changed, err)
	}
	saveDone := make(chan error, 1)
	go func() { saveDone <- s.WriteSnapshot(t.Context(), snapshot) }()
	select {
	case <-backend.writeStarted:
	case <-time.After(3 * time.Second):
		t.Fatal("the snapshot did not reach its write")
	}
	mutationDone := make(chan error, 1)
	go func() {
		_, _, err := s.Rename("note.txt", "renamed.txt")
		if err == nil {
			_, err = s.NewFileIn("", "note.txt", "new file\n")
		}
		mutationDone <- err
	}()
	mutationFinished := false
	select {
	case <-backend.recreated:
		if err := <-mutationDone; err != nil {
			t.Fatal(err)
		}
		mutationFinished = true
	case <-time.After(100 * time.Millisecond):
		// A serialized path change is still waiting for the old write.
	}
	close(backend.allowWrite)
	if err := <-saveDone; err != nil {
		t.Fatal(err)
	}
	if !mutationFinished {
		if err := <-mutationDone; err != nil {
			t.Fatal(err)
		}
	}
	assertSaveFile(t, filepath.Join(s.Dir, "note.txt"), "new file\n")
	assertSaveFile(t, filepath.Join(s.Dir, "renamed.txt"), "old edit\n")
}

func TestMemorySaveKeepsWorking(t *testing.T) {
	s := New(fs.NewMemory())
	n, err := s.NewMemoryFile("note.txt", "original\r\n")
	if err != nil {
		t.Fatal(err)
	}
	n.Buffer().Set("edited\n")
	if err := s.SaveAll(); err != nil {
		t.Fatal(err)
	}
	if n.Dirty() {
		t.Fatal("memory save left the note dirty")
	}
}
