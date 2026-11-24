package fs

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"mojian/internal/session"
)

// Server is a project's files reached through a session server. The app
// reads and writes them directly: there is no local copy of the project.
// A local project and a remote one use the same path; local says whether
// the server runs on this machine.
type Server struct {
	mu     sync.RWMutex
	client *session.Client
	root   string
	cache  string // where pictures and PDFs are fetched for their viewers
	local  bool   // the server runs on this machine
}

// NewServer returns a backend for a folder served by client. cache is
// where remote pictures and PDFs are fetched for their viewers.
func NewServer(client *session.Client, root, cache string, local bool) *Server {
	return &Server{client: client, root: root, cache: cache, local: local}
}

// SetClient points the backend at a new control client after the project
// reconnects.
func (r *Server) SetClient(client *session.Client) {
	r.mu.Lock()
	r.client = client
	r.mu.Unlock()
}

// ctl returns the current control client, safe while a background listing
// runs and the project reconnects.
func (r *Server) ctl() *session.Client {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.client
}

func (r *Server) Kind() Kind {
	if r.local {
		return KindServerLocal
	}
	return KindServerRemote
}
func (r *Server) Root() string            { return r.root }
func (r *Server) Client() *session.Client { return r.ctl() }
func (r *Server) CacheDir() string        { return r.cache }

func (r *Server) Abs(rel string) (string, error) {
	clean, err := cleanRel(rel)
	if err != nil {
		return "", err
	}
	if clean == "" {
		return r.root, nil
	}
	return path.Join(r.root, filepath.ToSlash(clean)), nil
}

func (r *Server) RelOf(abs string) (string, bool) {
	if !r.local {
		return "", false
	}
	return relOfRoot(r.root, abs)
}

func (r *Server) SameFile(oldRel, newRel string) bool {
	if !r.local {
		return false
	}
	oldAbs, err := r.Abs(oldRel)
	if err != nil {
		return false
	}
	newAbs, err := r.Abs(newRel)
	if err != nil {
		return false
	}
	oldInfo, err := os.Lstat(oldAbs)
	if err != nil {
		return false
	}
	newInfo, err := os.Lstat(newAbs)
	return err == nil && os.SameFile(oldInfo, newInfo)
}

func (r *Server) LocalPath(rel string) (string, bool) {
	if !r.local {
		return "", false
	}
	p, err := r.Abs(rel)
	return p, err == nil
}

func (r *Server) List() ([]session.FileEntry, map[string]string, error) {
	return r.ListContext(context.Background())
}

// ListContext lists through the caller's context so cancellation clears a
// pending request even when the server continues its walk.
func (r *Server) ListContext(ctx context.Context) ([]session.FileEntry, map[string]string, error) {
	entries, err := r.ctl().ListTreeContext(ctx, r.root)
	if err != nil {
		return nil, nil, fmt.Errorf("listing the server project: %w", err)
	}
	return entries, nil, nil
}

// ListFresh asks the server to re-walk the project instead of reusing
// its short-lived listing cache.
func (r *Server) ListFresh() ([]session.FileEntry, map[string]string, error) {
	return r.ListFreshContext(context.Background())
}

// ListFreshContext is ListFresh with caller cancellation.
func (r *Server) ListFreshContext(ctx context.Context) ([]session.FileEntry, map[string]string, error) {
	entries, err := r.ctl().ListTreeFreshContext(ctx, r.root)
	if err != nil {
		return nil, nil, fmt.Errorf("listing the server project: %w", err)
	}
	return entries, nil, nil
}

func (r *Server) Read(rel string) ([]byte, error) {
	return r.ReadContext(context.Background(), rel)
}

// ReadContext is Read with cancellation of the caller's pending request.
func (r *Server) ReadContext(ctx context.Context, rel string) ([]byte, error) {
	var data []byte
	err := at(r, rel, func(p string) error {
		var err error
		data, err = r.ctl().ReadFileContext(ctx, p)
		return err
	})
	return data, err
}

func (r *Server) Write(rel string, data []byte) error {
	return r.WriteContext(context.Background(), rel, data)
}

// WriteContext is Write with cancellation of the caller's pending request.
func (r *Server) WriteContext(ctx context.Context, rel string, data []byte) error {
	return at(r, rel, func(p string) error { return r.ctl().WriteFileContext(ctx, p, data) })
}

func (r *Server) Stat(rel string) (session.FileEntry, error) {
	p, err := r.Abs(rel)
	if err != nil {
		return session.FileEntry{}, err
	}
	e, err := r.ctl().Stat(p)
	if err != nil {
		return session.FileEntry{}, err
	}
	e.Rel = rel
	return e, nil
}

func (r *Server) Mkdir(rel string) error {
	return at(r, rel, r.ctl().Mkdir)
}

func (r *Server) Rename(oldRel, newRel string) error {
	if oldRel != newRel && strings.EqualFold(filepath.Clean(oldRel), filepath.Clean(newRel)) {
		return pair(r, oldRel, newRel, r.renameCase)
	}
	return pair(r, oldRel, newRel, r.ctl().Rename)
}

func serverPathExists(ctl *session.Client, name string) (bool, error) {
	_, err := ctl.Stat(name)
	if err == nil {
		return true, nil
	}
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	return false, err
}

// renameCase moves the source aside before naming it. On a case-insensitive
// filesystem the destination then vanishes with the source; on a
// case-sensitive filesystem a distinct destination remains and is protected.
func (r *Server) renameCase(oldAbs, newAbs string) error {
	ctl := r.ctl()
	tmp := oldAbs + ".mojian-rename"
	for i := 2; ; i++ {
		exists, err := serverPathExists(ctl, tmp)
		if err != nil {
			return err
		}
		if !exists {
			break
		}
		tmp = fmt.Sprintf("%s.mojian-rename-%d", oldAbs, i)
	}
	if err := ctl.Rename(oldAbs, tmp); err != nil {
		return err
	}
	exists, err := serverPathExists(ctl, newAbs)
	if err != nil || exists {
		if restoreErr := ctl.Rename(tmp, oldAbs); restoreErr != nil {
			return fmt.Errorf("checking %s; restoring the original name: %w", filepath.Base(newAbs), restoreErr)
		}
		if err != nil {
			return err
		}
		return fmt.Errorf("%s already exists", filepath.Base(newAbs))
	}
	if err := ctl.Rename(tmp, newAbs); err != nil {
		_ = ctl.Rename(tmp, oldAbs)
		return err
	}
	return nil
}

func (r *Server) Copy(oldRel, newRel string) error {
	return pair(r, oldRel, newRel, r.ctl().Copy)
}

func (r *Server) Remove(rel string) error {
	return at(r, rel, r.ctl().Remove)
}

func (r *Server) CopyFrom(srcAbs, destRel string) error {
	if !r.local {
		return fmt.Errorf("a remote project cannot paste Finder files")
	}
	return at(r, destRel, func(dest string) error { return r.ctl().Copy(srcAbs, dest) })
}

func (r *Server) MoveFrom(srcAbs, destRel string) error {
	if !r.local {
		return fmt.Errorf("a remote project cannot move Finder files")
	}
	return at(r, destRel, func(dest string) error {
		if err := r.ctl().Rename(srcAbs, dest); err == nil {
			return nil
		}
		if err := r.ctl().Copy(srcAbs, dest); err != nil {
			return err
		}
		return r.ctl().Remove(srcAbs)
	})
}

func (r *Server) RemoveAbs(abs string) error { return r.ctl().Remove(abs) }

func (r *Server) Fetch(rel string, size int64, mod time.Time, hash string) (string, error) {
	return r.FetchContext(context.Background(), rel, size, mod, hash)
}

// FetchContext downloads a remote picture or PDF into the local cache with cancellation.
func (r *Server) FetchContext(ctx context.Context, rel string, size int64, mod time.Time, hash string) (string, error) {
	if r.local {
		return r.Abs(rel)
	}
	if hash != "" {
		local := filepath.Join(r.cache, hash+strings.ToLower(filepath.Ext(rel)))
		if info, err := os.Stat(local); err == nil && info.Size() == size {
			return local, nil
		}
		data, err := r.ReadContext(ctx, rel)
		if err != nil {
			return "", err
		}
		if err := os.MkdirAll(r.cache, 0o755); err != nil {
			return "", err
		}
		if err := os.WriteFile(local, data, 0o644); err != nil {
			return "", err
		}
		return local, nil
	}
	dir := filepath.Join(r.cache, fmt.Sprintf("%d-%d", mod.Unix(), size))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	local := filepath.Join(dir, ShortHash(rel)+strings.ToLower(filepath.Ext(rel)))
	if info, err := os.Stat(local); err == nil && info.Size() == size {
		return local, nil
	}
	data, err := r.ReadContext(ctx, rel)
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(local, data, 0o644); err != nil {
		return "", err
	}
	return local, nil
}

func (r *Server) Trash(rel string) error {
	p, err := r.Abs(rel)
	if err != nil {
		return err
	}
	if r.local {
		return TrashPath(p)
	}
	dir := path.Join(".mojian-trash", time.Now().Format("20060102-150405"))
	if err := r.ctl().Mkdir(path.Join(r.root, dir)); err != nil {
		return err
	}
	dest := path.Join(dir, path.Base(filepath.ToSlash(rel)))
	for i := 2; ; i++ {
		if _, err := r.ctl().Stat(path.Join(r.root, dest)); err != nil {
			break
		}
		stem, ext := SplitExt(path.Base(filepath.ToSlash(rel)))
		dest = path.Join(dir, fmt.Sprintf("%s %d%s", stem, i, ext))
	}
	return r.ctl().Rename(p, path.Join(r.root, dest))
}
