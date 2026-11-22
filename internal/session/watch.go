//go:build darwin || linux || windows

package session

import (
	"encoding/json"
	"errors"
	"hash/fnv"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"mojian/internal/filekind"
)

const (
	watchInterval   = 1500 * time.Millisecond
	watchMaxEntries = 25000
	watchTimeBudget = 150 * time.Millisecond
)

// serveWatch monitors o.Dir with a bounded stat fingerprint and writes a
// "changed\n" line on ms whenever files are created, removed, or modified.
func (s *Server) serveWatch(ms *muxStream, o openRequest) {
	defer ms.Close()
	if o.Dir == "" {
		return
	}
	root := o.Dir
	if resolved, err := filepath.EvalSymlinks(root); err == nil {
		root = resolved
	}
	last, ok := fingerprintDir(root)
	if !ok {
		return
	}
	done := make(chan struct{})
	go func() {
		buf := make([]byte, 32)
		for {
			if _, err := ms.Read(buf); err != nil {
				close(done)
				return
			}
		}
	}()
	ticker := time.NewTicker(watchInterval)
	defer ticker.Stop()
	for {
		select {
		case <-done:
			return
		case <-s.quit:
			return
		case <-ticker.C:
			next, ok := fingerprintDir(root)
			if !ok {
				continue
			}
			if next != last {
				last = next
				s.invalidateTrees()
				if _, err := ms.Write([]byte("changed\n")); err != nil {
					return
				}
			}
		}
	}
}

func fingerprintDir(root string) (uint64, bool) {
	h := fnv.New64a()
	ign := filekind.NewGitIgnore(root)
	deadline := time.Now().Add(watchTimeBudget)
	count := 0
	exceeded := false
	err := filepath.WalkDir(root, func(p string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			if p == root {
				return walkErr
			}
			return nil
		}
		if p == root {
			return nil
		}
		count++
		if count > watchMaxEntries || (count&255 == 0 && time.Now().After(deadline)) {
			exceeded = true
			return filepath.SkipAll
		}
		name := d.Name()
		if d.IsDir() {
			if filekind.SkipDir(name) {
				return filepath.SkipDir
			}
			if rel, err := filepath.Rel(root, p); err == nil && ign.DirIgnored(filepath.ToSlash(rel)) {
				return filepath.SkipDir
			}
			_, _ = h.Write([]byte("D:"))
			_, _ = h.Write([]byte(p))
			_, _ = h.Write([]byte{0})
			return nil
		}
		if len(name) > 0 && name[0] == '.' && containsMojianTemp(name) {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		_, _ = h.Write([]byte("F:"))
		_, _ = h.Write([]byte(p))
		_, _ = h.Write([]byte{':'})
		_, _ = h.Write([]byte(strconv.FormatInt(info.Size(), 10)))
		_, _ = h.Write([]byte{':'})
		_, _ = h.Write([]byte(strconv.FormatInt(info.ModTime().UnixNano(), 10)))
		_, _ = h.Write([]byte{0})
		return nil
	})
	if err != nil || exceeded {
		return 0, false
	}
	return h.Sum64(), true
}

func containsMojianTemp(name string) bool {
	for i := 0; i+8 <= len(name); i++ {
		if name[i:i+8] == ".mojian-" {
			return true
		}
	}
	return false
}

// Watch subscribes to debounced change notifications for dir on the session server.
func (c *Client) Watch(dir string) (io.ReadCloser, error) {
	if c == nil || c.mux == nil {
		return nil, errors.New("session: not connected")
	}
	id := c.mux.newStreamID()
	ms := c.mux.openStream(id)
	b, _ := json.Marshal(openRequest{Kind: "watch", Dir: dir})
	if err := c.mux.writeFrame(frame{typ: frameOpen, stream: id, payload: b}); err != nil {
		ms.finish(err)
		return nil, err
	}
	return ms, nil
}
