//go:build unix

package filekind

import (
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func TestClassifyFileFIFOsDoNotBlock(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"pipe.txt", "pipe.unknown"} {
		path := filepath.Join(dir, name)
		if err := syscall.Mkfifo(path, 0o600); err != nil {
			t.Fatal(err)
		}
		type result struct {
			kind Kind
			err  error
		}
		done := make(chan result, 1)
		go func() {
			kind, err := ClassifyFile(path)
			done <- result{kind: kind, err: err}
		}()
		select {
		case got := <-done:
			if got.err != nil || got.kind != Other {
				t.Errorf("ClassifyFile(%q) = %v, %v; want %v, nil", name, got.kind, got.err, Other)
			}
		case <-time.After(2 * time.Second):
			t.Fatalf("ClassifyFile(%q) blocked on a FIFO", name)
		}
	}
}
