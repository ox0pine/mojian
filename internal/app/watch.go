package app

import (
	"bufio"
	"context"
	"path"
	"strings"
	"time"

	"mojian/internal/filekind"

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
			post(func() {
				if a.windowSeq != winSeq || a.watchSeq != seq || a.store != s {
					return
				}
				a.handleWatchEvents()
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
// reloads clean open notes, refreshes the Explorer tree, and updates
// Git state when open.
func (a *app) handleWatchEvents() {
	s := a.store
	if s == nil || s.IsMemory() || s.IsRemote() {
		return
	}
	if err := s.Reload(); err == nil {
		a.invalidateEditorConfig()
		a.explorerUI().RefreshApplied()
		a.invalidateDiffBases()
		a.refreshGitStatus()
		if a.git.Open && !a.NoGitAutoRefresh {
			a.refreshGit()
		}
		a.invalidate()
	}
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
