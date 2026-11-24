package project

import (
	"context"
	"fmt"
	"time"

	"mojian/internal/session"
)

// LoadNote reads a note's text the first time it opens. A direct project
// is already loaded from its listing; a server project is read here.
func (s *Store) LoadNote(n *Note) error {
	if n == nil || n.Loaded {
		return nil
	}
	data, err := s.FS.Read(n.Rel)
	if err != nil {
		return err
	}
	n.load(string(data))
	return nil
}

// ReadNoteContext reads rel from the backend without mutating any Note state,
// so callers can run it off the UI thread with cancellation.
func (s *Store) ReadNoteContext(ctx context.Context, rel string) (string, error) {
	if s == nil || s.FS == nil {
		return "", fmt.Errorf("no backend")
	}
	var data []byte
	var err error
	if backend, ok := s.FS.(interface {
		ReadContext(context.Context, string) ([]byte, error)
	}); ok {
		data, err = backend.ReadContext(ctx, rel)
	} else {
		data, err = s.FS.Read(rel)
	}
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// CommitLoadedNote installs body read by ReadNoteContext into n on the UI thread.
func (s *Store) CommitLoadedNote(n *Note, body string) {
	if n == nil || n.Loaded || n.Dirty() {
		return
	}
	n.load(body)
}

// MaterializeAsset fetches a remote picture or PDF into the local cache
// its viewer reads, and returns that file.
func (s *Store) MaterializeAsset(f *Asset) (string, error) {
	if f == nil {
		return "", fmt.Errorf("no file")
	}
	if f.IsBinary() {
		// The binary pane needs only metadata; never download a remote file
		// merely to show its name and size.
		return f.Path, nil
	}
	p, err := s.FS.Fetch(f.Rel, f.Size, f.Mod, f.Hash)
	if err != nil {
		return "", err
	}
	if s.IsRemote() {
		f.Local = p
	}
	return p, nil
}

// FetchAssetContext fetches a remote picture or PDF off the UI thread without
// mutating Asset fields.
func (s *Store) FetchAssetContext(ctx context.Context, rel string, size int64, mod time.Time, hash string, isBinary bool, path string) (string, error) {
	if isBinary {
		return path, nil
	}
	if s == nil || s.FS == nil {
		return "", fmt.Errorf("no backend")
	}
	if backend, ok := s.FS.(interface {
		FetchContext(context.Context, string, int64, time.Time, string) (string, error)
	}); ok {
		return backend.FetchContext(ctx, rel, size, mod, hash)
	}
	return s.FS.Fetch(rel, size, mod, hash)
}

// CommitMaterializedAsset records a fetched local cache path on f on the UI thread.
func (s *Store) CommitMaterializedAsset(f *Asset, localPath string) {
	if f == nil || localPath == "" {
		return
	}
	if s != nil && s.IsRemote() {
		f.Local = localPath
	}
}

// ExistsRel reports whether rel names a file or folder in the project.
func (s *Store) ExistsRel(rel string) bool {
	if rel == "" || rel == "." {
		return true
	}
	if s.IsMemory() {
		return s.ByRel[rel] != nil
	}
	if s.Dirs[rel] || s.ByRel[rel] != nil || s.Assets[rel] != nil {
		return true
	}
	_, err := s.FS.Stat(rel)
	return err == nil
}

// DirExists reports whether rel names a folder in the project.
func (s *Store) DirExists(rel string) bool {
	if s == nil || s.IsMemory() {
		return false
	}
	if rel == "" || rel == "." {
		return true
	}
	if s.IsServer() && s.Dirs[rel] {
		return true
	}
	e, err := s.FS.Stat(rel)
	return err == nil && e.Dir
}

// AbsRel resolves a path within the project.
func (s *Store) AbsRel(rel string) (string, error) {
	return s.FS.Abs(rel)
}

// RelOf reports a path's place in the project, if it is inside it.
func (s *Store) RelOf(p string) (string, bool) {
	return s.FS.RelOf(p)
}

// SetClient points a server-backed store at a new control client after
// the project reconnects.
func (s *Store) SetClient(client *session.Client) {
	if sf, ok := s.FS.(interface{ SetClient(*session.Client) }); ok {
		sf.SetClient(client)
	}
}
