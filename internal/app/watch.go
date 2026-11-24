package app

import (
	"bufio"
	"context"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"mojian/internal/filekind"
	"mojian/internal/lsp"

	"github.com/egoist/mygo"
)

// fileWatcher is the minimal surface of *mygo.FileWatcher used by the
// project watcher; tests substitute a deterministic watcher.
type fileWatcher interface {
	Next(context.Context) ([]mygo.FileEvent, error)
	Close() error
}

// syncProjectWatcher keeps one live filesystem watcher on the active
// project (local via mygo.WatchFiles, or remote via mojian-server's Watch
// stream) and stops it when switching away or closing the window.
func (a *app) syncProjectWatcher() {
	if a.harness || a.store == nil || a.store.IsMemory() {
		a.stopProjectWatcher()
		return
	}
	if a.store.IsRemote() {
		a.syncRemoteProjectWatcher()
		return
	}
	if a.win == nil && a.watchStarter == nil {
		a.stopProjectWatcher()
		return
	}
	dir := a.store.Dir
	if dir == "" {
		a.stopProjectWatcher()
		return
	}
	if a.watchCancel != nil && a.watchDir == dir {
		return
	}
	a.stopProjectWatcher()
	post := a.uiPoster()
	if post == nil {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	a.watchSeq++
	seq := a.watchSeq
	winSeq := a.windowSeq
	s := a.store
	a.watchCancel = cancel
	a.watchDir = dir

	ign := filekind.NewGitIgnore(dir)
	ignore := func(rel string, isDir bool) bool {
		base := path.Base(rel)
		if isDir {
			if filekind.SkipDir(base) {
				return true
			}
			return ign.DirIgnored(rel)
		}
		return strings.HasPrefix(base, ".") && strings.Contains(base, ".mojian-")
	}

	starter := a.watchStarter
	if starter == nil {
		starter = func(ctx context.Context, root string, ignFn func(string, bool) bool) (fileWatcher, error) {
			return mygo.WatchFiles(ctx, root, mygo.FileWatchOptions{
				Recursive: true,
				HintsOnly: true,
				Debounce:  250 * time.Millisecond,
				MaxDelay:  750 * time.Millisecond,
				Ignore:    ignFn,
			})
		}
	}

	go func() {
		w, err := starter(ctx, dir, ignore)
		if err != nil {
			return
		}
		defer w.Close()
		for {
			events, err := w.Next(ctx)
			if err != nil || ctx.Err() != nil {
				return
			}
			if len(events) == 0 {
				continue
			}
			eventsCopy := append([]mygo.FileEvent(nil), events...)
			post(func() {
				if a.windowSeq != winSeq || a.watchSeq != seq || a.store != s {
					return
				}
				a.handleWatchEvents(eventsCopy)
			})
		}
	}()
}

// syncRemoteProjectWatcher subscribes to debounced change notifications from
// the remote mojian-server so external edits in a remote terminal refresh
// clean open files, the Explorer tree, and Git state automatically.
func (a *app) syncRemoteProjectWatcher() {
	s := a.store
	if s == nil || !s.IsRemote() || s.Dir == "" || s.Client() == nil {
		a.stopProjectWatcher()
		return
	}
	post := a.uiPoster()
	if post == nil {
		a.stopProjectWatcher()
		return
	}
	key := "remote:" + a.serverAlias + ":" + s.Dir
	if a.watchCancel != nil && a.watchDir == key {
		return
	}
	a.stopProjectWatcher()
	ctx, cancel := context.WithCancel(context.Background())
	a.watchSeq++
	seq := a.watchSeq
	winSeq := a.windowSeq
	a.watchCancel = cancel
	a.watchDir = key
	client := s.Client()
	dir := s.Dir

	go func() {
		rc, err := client.Watch(dir)
		if err != nil {
			return
		}
		go func() {
			<-ctx.Done()
			_ = rc.Close()
		}()
		sc := bufio.NewScanner(rc)
		for sc.Scan() {
			if ctx.Err() != nil {
				return
			}
			post(func() {
				if a.windowSeq != winSeq || a.watchSeq != seq || a.store != s {
					return
				}
				a.handleRemoteWatchEvents()
			})
		}
	}()
}

// stopProjectWatcher cancels the active project's filesystem watch.
func (a *app) stopProjectWatcher() {
	a.watchSeq++
	a.watchDir = ""
	if a.watchCancel != nil {
		a.watchCancel()
		a.watchCancel = nil
	}
}

// handleWatchEvents reconciles external edits to the local project:
// reloads clean open notes, refreshes the Explorer tree, notifies LSP
// servers of watched file changes, and updates Git state when open.
func (a *app) handleWatchEvents(events ...[]mygo.FileEvent) {
	s := a.store
	if s == nil || s.IsMemory() || s.IsRemote() {
		return
	}
	beforePaths, beforeVers := snapshotStoreState(s)
	if err := s.Reload(); err == nil {
		a.invalidateEditorConfig()
		a.explorerUI().RefreshApplied()
		var flat []mygo.FileEvent
		for _, batch := range events {
			flat = append(flat, batch...)
		}
		changes := a.diffStoreLSPChanges(a.workspace, beforePaths, beforeVers, flat)
		a.notifyLSPWatchedFiles(a.workspace, changes)
		a.invalidateDiffBases()
		a.refreshGitStatus()
		if a.git.Open && !a.NoGitAutoRefresh {
			a.refreshGit()
		}
		a.invalidate()
	}
}

func snapshotStoreState(s *store) (paths map[string]bool, vers map[string]uint64) {
	paths = map[string]bool{}
	vers = map[string]uint64{}
	if s == nil {
		return paths, vers
	}
	for _, n := range s.Notes {
		rel := filepath.ToSlash(n.Rel)
		paths[rel] = true
		if n.Loaded {
			vers[rel] = n.Buffer().Version()
		}
	}
	for rel := range s.Assets {
		paths[filepath.ToSlash(rel)] = true
	}
	return paths, vers
}

func (a *app) diffStoreLSPChanges(ws *workspace, beforePaths map[string]bool, beforeVers map[string]uint64, events []mygo.FileEvent) []lsp.FileEvent {
	if ws == nil || ws.store == nil {
		return nil
	}
	afterPaths, afterVers := snapshotStoreState(ws.store)
	byURI := map[string]int{}
	set := func(rel string, typ int) {
		rel = strings.TrimPrefix(filepath.ToSlash(rel), "./")
		if rel == "" || rel == "." {
			return
		}
		uri := a.lspURIForRel(ws, rel)
		if uri == "" {
			return
		}
		if prev, ok := byURI[uri]; ok && (prev == lsp.FileCreated || prev == lsp.FileDeleted) && typ == lsp.FileChanged {
			return
		}
		byURI[uri] = typ
	}
	for rel := range afterPaths {
		if !beforePaths[rel] {
			set(rel, lsp.FileCreated)
		} else if bVer, ok := beforeVers[rel]; ok && afterVers[rel] != bVer {
			set(rel, lsp.FileChanged)
		}
	}
	for rel := range beforePaths {
		if !afterPaths[rel] {
			set(rel, lsp.FileDeleted)
		}
	}
	for _, ev := range events {
		if ev.IsDir {
			continue
		}
		switch ev.Op {
		case mygo.FileCreate:
			set(ev.Path, lsp.FileCreated)
		case mygo.FileRemove:
			set(ev.Path, lsp.FileDeleted)
		case mygo.FileRename:
			if ev.OldPath != "" && ev.OldPath != "." {
				set(ev.OldPath, lsp.FileDeleted)
			}
			set(ev.Path, lsp.FileCreated)
		default:
			set(ev.Path, lsp.FileChanged)
		}
	}
	if len(byURI) == 0 {
		return nil
	}
	out := make([]lsp.FileEvent, 0, len(byURI))
	for uri, typ := range byURI {
		out = append(out, lsp.FileEvent{URI: uri, Type: typ})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].URI < out[j].URI })
	return out
}

// handleRemoteWatchEvents refreshes the remote project listing and reloads
// clean open notes off the UI thread after a remote server watch signal.
func (a *app) handleRemoteWatchEvents() {
	s := a.store
	if s == nil || !s.IsRemote() {
		return
	}
	a.invalidateEditorConfig()
	a.reloadProject(true)
	a.invalidateDiffBases()
	a.refreshGitStatus()
	if a.git.Open && !a.NoGitAutoRefresh {
		a.refreshGit()
	}
}
