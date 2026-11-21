package fs

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"syscall"
	"time"
)

// WriteFileAtomic replaces path with data, so a crash leaves the old
// file or the new one, never half of each.
func WriteFileAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	mode := os.FileMode(0o644)
	if info, err := os.Stat(path); err == nil {
		mode = info.Mode().Perm()
	}
	f, err := os.CreateTemp(dir, filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	name := f.Name()
	defer os.Remove(name)
	if err := f.Chmod(mode); err != nil {
		_ = f.Close()
		return err
	}
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return renameRetry(name, path)
}

// MoveFileAtomic moves path within a data directory without a partial copy.
// A missing source is already the requested end state. An existing destination
// is replaced with the source's complete contents rather than left stale.
func MoveFileAtomic(path, newPath string) error {
	if filepath.Clean(path) == filepath.Clean(newPath) {
		return nil
	}
	if _, err := os.Stat(newPath); err == nil {
		data, err := os.ReadFile(path)
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		if err := WriteFileAtomic(newPath, data); err != nil {
			return err
		}
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(newPath), 0o755); err != nil {
		return err
	}
	if err := renameRetry(path, newPath); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	return nil
}

// renameRetry retries the replace on Windows, where another process
// holding the destination open makes os.Rename fail with a sharing or
// access violation.
func renameRetry(old, new string) error {
	for attempt := 0; ; attempt++ {
		err := os.Rename(old, new)
		if err == nil {
			return nil
		}
		if runtime.GOOS != "windows" || attempt >= 9 {
			return err
		}
		if !errors.Is(err, syscall.Errno(32)) && !errors.Is(err, syscall.Errno(5)) {
			return err
		}
		time.Sleep(50 * time.Millisecond)
	}
}
