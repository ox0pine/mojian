package app

import (
	gitpage "mojian/internal/git"

	"github.com/egoist/mygo/ui/editor"
)

// diffBaseEntry caches a file's HEAD buffer for editor gutter diff bars.
type diffBaseEntry struct {
	gen     int
	loading bool
	buf     *editor.Buffer
}

// noteDiffBase returns the cached HEAD buffer for n's gutter diff bars,
// queueing an asynchronous read on first access or after invalidation.
func (a *app) noteDiffBase(n *note) *editor.Buffer {
	if n == nil || a.store == nil || a.store.IsMemory() {
		return nil
	}
	ws := a.workspace
	if ws == nil {
		return nil
	}
	if ws.diffBases == nil {
		ws.diffBases = map[string]*diffBaseEntry{}
	}
	e := ws.diffBases[n.Rel]
	if e != nil && e.gen == ws.diffBaseGen {
		return e.buf
	}
	if e == nil {
		e = &diffBaseEntry{}
		ws.diffBases[n.Rel] = e
	}
	if !e.loading {
		e.loading = true
		gen := ws.diffBaseGen
		rel := n.Rel
		a.later(func() { a.loadNoteDiffBase(ws, rel, gen) })
	}
	return e.buf
}

func (a *app) loadNoteDiffBase(ws *workspace, rel string, gen int) {
	if ws == nil || ws.store == nil || ws.store.IsMemory() || ws.diffBaseGen != gen {
		return
	}
	dir := ws.store.Dir
	if dir == "" {
		return
	}
	runner := gitpage.Runner(gitpage.LocalRunner{})
	if ws.store.IsServer() {
		runner = serverGitRunner{client: ws.store.Client()}
	}
	apply := func(text string, ok bool) {
		if ws.diffBaseGen != gen {
			return
		}
		if ws.diffBases == nil {
			ws.diffBases = map[string]*diffBaseEntry{}
		}
		var buf *editor.Buffer
		if ok {
			buf = editor.NewBuffer(text)
		}
		ws.diffBases[rel] = &diffBaseEntry{gen: gen, buf: buf}
		if a.workspace == ws {
			a.invalidate()
		}
	}
	post := a.uiPoster()
	if post == nil {
		text, ok, _ := gitpage.ReadHeadFile(runner, dir, rel)
		apply(text, ok)
		return
	}
	winSeq := a.windowSeq
	go func() {
		text, ok, _ := gitpage.ReadHeadFile(runner, dir, rel)
		post(func() {
			if a.windowSeq != winSeq {
				return
			}
			apply(text, ok)
		})
	}()
}

// invalidateDiffBases drops cached HEAD buffers for the active workspace
// so open editors re-read their gutter diff bases on the next frame.
func (a *app) invalidateDiffBases() {
	if a.workspace == nil {
		return
	}
	a.workspace.diffBaseGen++
	a.workspace.diffBases = nil
	a.invalidate()
}
