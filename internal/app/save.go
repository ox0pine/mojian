package app

import (
	"context"
	"errors"
	"log"

	"mojian/internal/project"
)

// saveTask is the remote save currently writing immutable buffer snapshots.
// Its sequence rejects a completion canceled by an explicit abandon.
type saveTask struct {
	seq    int
	cancel context.CancelFunc
	active bool
	store  *store
}

type saveWaiter struct {
	store *store
	done  func(error)
}

// saveNowAsync is saveNow for an action that must not wait for a remote RPC.
// Local projects and no-window tests keep the original synchronous behavior.
// The callback runs on the UI thread after every snapshot and any edit made
// while those snapshots were writing has been saved, or after the first error.
func (a *app) saveNowAsync(done func(error)) {
	if done == nil {
		done = func(error) {}
	}
	if a.store == nil || !a.store.IsRemote() || (a.win == nil && a.uiPost == nil) {
		done(a.saveNow())
		return
	}
	if a.layoutDirty {
		if err := a.saveLayout(); err != nil {
			done(err)
			return
		}
		a.layoutDirty = false
	}
	a.saveSettings()
	s := a.store
	a.saveWaiters = append(a.saveWaiters, saveWaiter{store: s, done: done})
	if !a.saveTask.active {
		a.startNextRemoteSave()
	}
}

// startNextRemoteSave keeps actions from different workspaces serialized:
// a waiter is released only after the store it belonged to has finished.
func (a *app) startNextRemoteSave() {
	if a.saveTask.active || len(a.saveWaiters) == 0 {
		return
	}
	a.startRemoteSave(a.saveWaiters[0].store)
}

func (a *app) startRemoteSave(s *store) {
	a.saveTask.store = s
	snapshots, snapshotErr := s.DirtySnapshots()
	if len(snapshots) == 0 {
		a.saveTask.store = nil
		a.finishRemoteSaveWaiters(s, snapshotErr)
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	a.saveTask.seq++
	seq := a.saveTask.seq
	a.saveTask.cancel = cancel
	a.saveTask.active = true

	go func() {
		written := make([]bool, len(snapshots))
		var writeErr error
		for i, snapshot := range snapshots {
			// A rename, recreate, or trash invalidates the old destination.
			// Skip that write without failing the action; any surviving dirty
			// note is snapshotted at its current path in the next pass.
			if err := s.WriteSnapshot(ctx, snapshot); err != nil {
				if !errors.Is(err, project.ErrSaveSnapshotStale) && writeErr == nil {
					writeErr = err
				}
				continue
			}
			written[i] = true
		}
		a.onUpdateUI(func() {
			if !a.saveTask.active || a.saveTask.seq != seq || a.saveTask.store != s {
				return
			}
			for i, ok := range written {
				if ok {
					s.CommitSnapshot(snapshots[i])
				}
			}
			a.saveTask.active = false
			a.saveTask.cancel = nil
			a.saveTask.store = nil
			cancel()
			first := snapshotErr
			if first == nil {
				first = writeErr
			}
			if first == nil {
				// A buffer edited while its snapshot was in flight is still
				// dirty. Write another snapshot before releasing the action
				// waiting to switch projects or close the window.
				if next, err := s.DirtySnapshots(); len(next) > 0 || err != nil {
					a.startRemoteSave(s)
					return
				}
			}
			a.finishRemoteSaveWaiters(s, first)
		})
	}()
}

func (a *app) finishRemoteSaveWaiters(s *store, err error) {
	var done []func(error)
	kept := a.saveWaiters[:0]
	for _, waiter := range a.saveWaiters {
		if waiter.store == s {
			done = append(done, waiter.done)
		} else {
			kept = append(kept, waiter)
		}
	}
	a.saveWaiters = kept
	if err != nil {
		log.Print(err)
	}
	for _, fn := range done {
		fn(err)
	}
	if !a.saveTask.active {
		a.startNextRemoteSave()
	}
}

// cancelRemoteSave abandons a save only when the user has explicitly
// discarded its edits. Normal close instead waits for saveNowAsync.
func (a *app) cancelRemoteSave() {
	if !a.saveTask.active {
		return
	}
	s := a.saveTask.store
	a.saveTask.seq++
	a.saveTask.active = false
	a.saveTask.store = nil
	if a.saveTask.cancel != nil {
		a.saveTask.cancel()
		a.saveTask.cancel = nil
	}
	a.finishRemoteSaveWaiters(s, context.Canceled)
}
