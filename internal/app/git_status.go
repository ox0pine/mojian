package app

import (
	"maps"
	"path"
	"path/filepath"

	gitpage "mojian/internal/git"
)

// refreshGitStatus asynchronously reads working-tree file statuses for the
// Explorer tree badges and directory indicators.
func (a *app) refreshGitStatus() {
	if a.workspace == nil || a.store == nil || a.store.IsMemory() {
		return
	}
	if a.store.IsServer() && a.store.Client() == nil {
		return
	}
	dir, ok := a.ProjectDir()
	if !ok {
		return
	}
	ws := a.workspace
	if ws.gitStatusLoading {
		return
	}
	runner := a.GitRunner()
	ws.gitStatusSeq++
	seq := ws.gitStatusSeq
	ws.gitStatusLoading = true
	apply := func(st gitpage.StatusData) {
		a.applyGitStatusData(ws, seq, st)
	}
	post := a.uiPoster()
	if post == nil {
		apply(gitpage.ReadStatus(runner, dir))
		return
	}
	go func() {
		st := gitpage.ReadStatus(runner, dir)
		post(func() { apply(st) })
	}()
}

func (a *app) applyGitStatusData(ws *workspace, seq int, st gitpage.StatusData) {
	if ws == nil || seq != ws.gitStatusSeq {
		return
	}
	ws.gitStatusLoading = false
	prevFiles := ws.gitFileStatus
	prevDirs := ws.gitDirDirty
	if st.Root == "" || st.Err != "" {
		ws.gitFileStatus = nil
		ws.gitDirDirty = nil
	} else {
		applyGitChanges(ws, st.Changes)
	}
	if a.workspace == ws && (!maps.Equal(prevFiles, ws.gitFileStatus) || !maps.Equal(prevDirs, ws.gitDirDirty)) {
		a.invalidate()
	}
}

// applyGitChanges indexes working-tree changes by slash-separated project-relative
// path and marks every ancestor directory dirty for collapsed-folder indicators.
func applyGitChanges(ws *workspace, changes []gitpage.Change) {
	if ws == nil {
		return
	}
	if len(changes) == 0 {
		ws.gitFileStatus = nil
		ws.gitDirDirty = nil
		return
	}
	files := make(map[string]string, len(changes))
	dirs := make(map[string]bool)
	for _, ch := range changes {
		rel := filepath.ToSlash(ch.Display)
		if rel == "" {
			rel = filepath.ToSlash(ch.Path)
		}
		files[rel] = ch.Status
		for d := path.Dir(rel); d != "." && d != "" && d != "/"; d = path.Dir(d) {
			dirs[d] = true
		}
	}
	ws.gitFileStatus = files
	ws.gitDirDirty = dirs
}

func (a *app) gitStatusFor(rel string) string {
	if a.workspace == nil || len(a.gitFileStatus) == 0 || rel == "" {
		return ""
	}
	return a.gitFileStatus[filepath.ToSlash(rel)]
}

func (a *app) gitDirDirtyFor(rel string) bool {
	if a.workspace == nil || len(a.gitDirDirty) == 0 || rel == "" {
		return false
	}
	return a.gitDirDirty[filepath.ToSlash(rel)]
}
