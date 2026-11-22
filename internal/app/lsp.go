package app

import (
	"context"
	"io"
	"path"
	"path/filepath"
	"strings"
	"time"

	gitpage "mojian/internal/git"
	"mojian/internal/lsp"

	"github.com/egoist/mygo/ui/editor"
)

// lspServerState holds one language server session for a workspace language group.
type lspServerState struct {
	group    string
	name     string
	client   *lsp.Client
	starting bool
	failed   bool
}

type lspDocDiag struct {
	version uint64
	diags   []editor.Diagnostic
}

// lspManager owns lazy per-workspace language servers (local or remote over
// mojian-server) and caches diagnostics per slash-separated relative path.
type lspManager struct {
	servers    map[string]*lspServerState
	opened     map[string]uint64 // rel -> last synced buffer version
	docVersion map[string]int    // rel -> monotonic LSP document version
	diags      map[string]lspDocDiag
	applied    map[*editor.State]uint64 // tracks last applied diag generation
	diagGen    uint64
}

func newLSPManager() *lspManager {
	return &lspManager{
		servers:    map[string]*lspServerState{},
		opened:     map[string]uint64{},
		docVersion: map[string]int{},
		diags:      map[string]lspDocDiag{},
		applied:    map[*editor.State]uint64{},
	}
}

func (m *lspManager) closeAll() {
	if m == nil {
		return
	}
	for _, st := range m.servers {
		if st.client != nil {
			_ = st.client.Close()
		}
	}
	m.servers = map[string]*lspServerState{}
	m.opened = map[string]uint64{}
	m.docVersion = map[string]int{}
	m.diags = map[string]lspDocDiag{}
	m.applied = map[*editor.State]uint64{}
}

func (a *app) closeWorkspaceLSP(ws *workspace) {
	if ws != nil && ws.lsp != nil {
		ws.lsp.closeAll()
		ws.lsp = nil
	}
}

func (a *app) workspaceLSP(ws *workspace) *lspManager {
	if ws == nil {
		return nil
	}
	if ws.lsp == nil {
		ws.lsp = newLSPManager()
	}
	return ws.lsp
}

func (a *app) lspURIForRel(ws *workspace, rel string) string {
	if ws == nil || ws.store == nil || ws.store.Dir == "" {
		return ""
	}
	if ws.store.IsRemote() {
		return lsp.FileURI(path.Join(ws.store.Dir, filepath.ToSlash(rel)))
	}
	return lsp.FileURI(filepath.Join(ws.store.Dir, filepath.FromSlash(rel)))
}

func (a *app) lspRelFromURI(ws *workspace, uri string) (string, bool) {
	if ws == nil || ws.store == nil || ws.store.Dir == "" {
		return "", false
	}
	p, ok := lsp.URIToPath(uri)
	if !ok {
		return "", false
	}
	if ws.store.IsRemote() {
		pSlash := filepath.ToSlash(p)
		root := strings.TrimRight(filepath.ToSlash(ws.store.Dir), "/")
		if pSlash == root {
			return "", false
		}
		if strings.HasPrefix(pSlash, root+"/") {
			return strings.TrimPrefix(pSlash, root+"/"), true
		}
		return "", false
	}
	rel, err := filepath.Rel(ws.store.Dir, p)
	if err != nil || rel == "." || strings.HasPrefix(rel, "..") {
		return "", false
	}
	return filepath.ToSlash(rel), true
}

// lspClientForNote returns the ready LSP client for n, lazily starting one
// in the background if a server is available on the local or remote machine.
func (a *app) lspClientForNote(ws *workspace, n *note) *lsp.Client {
	if a.harness || ws == nil || ws.store == nil || ws.store.IsMemory() || n == nil || ws.store.Dir == "" {
		return nil
	}
	lang := lsp.LanguageForPath(n.Rel)
	if lang == "" {
		return nil
	}
	group := lsp.ServerGroup(lang)
	mgr := a.workspaceLSP(ws)
	st := mgr.servers[group]
	if st != nil {
		if st.client != nil {
			select {
			case <-st.client.Closed():
				st.client = nil
				st.failed = true
			default:
				if st.client.Ready() {
					return st.client
				}
				return nil
			}
		}
		if st.starting || st.failed {
			return nil
		}
	}
	spec, ok := lsp.SpecForLanguage(lang)
	if !ok {
		return nil
	}
	post := a.uiPoster()
	if post == nil {
		return nil
	}
	st = &lspServerState{group: group, name: spec.Name, starting: true}
	mgr.servers[group] = st

	dir := ws.store.Dir
	remote := ws.store.IsRemote()
	remoteClient := ws.store.Client()
	winSeq := a.windowSeq
	rootURI := lsp.FileURI(dir)

	go func() {
		var rwc io.ReadWriteCloser
		var err error
		if remote {
			if remoteClient == nil {
				err = io.EOF
			} else {
				for _, argv := range spec.Commands {
					rwc, err = remoteClient.StartLSP(dir, argv)
					if err == nil {
						break
					}
				}
			}
		} else {
			argv, _, found := lsp.LookupLocal(lang)
			if !found {
				err = io.EOF
			} else {
				rwc, err = lsp.StartLocal(dir, argv)
			}
		}
		if err != nil || rwc == nil {
			post(func() {
				if a.windowSeq != winSeq || ws.lsp != mgr {
					return
				}
				st.starting = false
				st.failed = true
			})
			return
		}
		cl := lsp.NewClient(rwc, func(params lsp.PublishDiagnosticsParams) {
			post(func() {
				if a.windowSeq != winSeq || ws.lsp != mgr {
					return
				}
				a.applyLSPDiagnostics(ws, params)
			})
		})
		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		initErr := cl.Initialize(ctx, rootURI)
		cancel()
		if initErr != nil {
			_ = cl.Close()
			post(func() {
				if a.windowSeq != winSeq || ws.lsp != mgr {
					return
				}
				st.starting = false
				st.failed = true
			})
			return
		}
		post(func() {
			if a.windowSeq != winSeq || ws.lsp != mgr {
				_ = cl.Close()
				return
			}
			st.starting = false
			st.client = cl
			a.invalidate()
		})
	}()
	return nil
}

func (a *app) applyLSPDiagnostics(ws *workspace, params lsp.PublishDiagnosticsParams) {
	if ws == nil || ws.lsp == nil || ws.store == nil {
		return
	}
	rel, ok := a.lspRelFromURI(ws, params.URI)
	if !ok {
		return
	}
	n := ws.store.ByRelPath(filepath.FromSlash(rel))
	if n == nil {
		return
	}
	buf := n.Buffer()
	edDiags := lsp.ToEditorDiagnostics(n.Text(), params.Diagnostics)
	ws.lsp.diagGen++
	ws.lsp.diags[rel] = lspDocDiag{version: buf.Version(), diags: edDiags}
	if a.workspace == ws {
		a.invalidate()
	}
}

// syncEditorLSP hooks LSP completion, hover, definition and diagnostics onto st.
func (a *app) syncEditorLSP(p *pane, st *editor.State) {
	if p == nil || p.Note == nil || st == nil {
		return
	}
	ws := a.workspace
	n := p.Note
	rel := filepath.ToSlash(n.Rel)
	cl := a.lspClientForNote(ws, n)
	if cl != nil && ws != nil && ws.lsp != nil {
		buf := n.Buffer()
		ver := buf.Version()
		uri := a.lspURIForRel(ws, rel)
		if lastVer, opened := ws.lsp.opened[rel]; !opened {
			ws.lsp.opened[rel] = ver
			ws.lsp.docVersion[rel] = 1
			text := n.Text()
			lang := lsp.LanguageForPath(rel)
			go func() { _ = cl.DidOpen(uri, lang, 1, text) }()
		} else if lastVer != ver {
			ws.lsp.opened[rel] = ver
			ws.lsp.docVersion[rel]++
			docVer := ws.lsp.docVersion[rel]
			text := n.Text()
			go func() { _ = cl.DidChange(uri, docVer, text) }()
		}
		if ws.lsp.applied[st] != ws.lsp.diagGen {
			ws.lsp.applied[st] = ws.lsp.diagGen
			if d, ok := ws.lsp.diags[rel]; ok {
				st.SetDiagnostics(d.version, d.diags)
			}
		}
	}

	st.OnCompletion = func(req editor.CompletionRequest) {
		cl := a.lspClientForNote(ws, n)
		if cl == nil {
			return
		}
		post := a.uiPoster()
		if post == nil {
			return
		}
		text := n.Text()
		pos := lsp.OffsetToPosition(text, req.Offset)
		uri := a.lspURIForRel(ws, rel)
		winSeq := a.windowSeq
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			items, err := cl.Completion(ctx, uri, pos)
			cancel()
			if err != nil || len(items) == 0 {
				return
			}
			post(func() {
				if a.windowSeq != winSeq || p.state != st || p.Note != n {
					return
				}
				if n.Buffer().Version() != req.Version {
					return
				}
				st.ShowCompletions(req.Offset-len(req.Prefix), items)
				a.invalidate()
			})
		}()
	}

	st.OnHover = func(req editor.HoverRequest) {
		cl := a.lspClientForNote(ws, n)
		if cl == nil {
			return
		}
		post := a.uiPoster()
		if post == nil {
			return
		}
		text := n.Text()
		pos := lsp.OffsetToPosition(text, req.Offset)
		uri := a.lspURIForRel(ws, rel)
		winSeq := a.windowSeq
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			body, rng, err := cl.Hover(ctx, uri, pos)
			cancel()
			if err != nil || body == "" {
				return
			}
			post(func() {
				if a.windowSeq != winSeq || p.state != st || p.Note != n {
					return
				}
				if n.Buffer().Version() != req.Version {
					return
				}
				start, end := req.Offset, req.Offset
				if rng != nil {
					start = lsp.PositionToOffset(text, rng.Start)
					end = lsp.PositionToOffset(text, rng.End)
				}
				st.ShowHover(start, end, body, nil)
				a.invalidate()
			})
		}()
	}

	st.OnDefinition = func(offset int) {
		cl := a.lspClientForNote(ws, n)
		if cl == nil {
			return
		}
		post := a.uiPoster()
		if post == nil {
			return
		}
		text := n.Text()
		pos := lsp.OffsetToPosition(text, offset)
		uri := a.lspURIForRel(ws, rel)
		winSeq := a.windowSeq
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			loc, ok, err := cl.Definition(ctx, uri, pos)
			cancel()
			if err != nil || !ok {
				return
			}
			post(func() {
				if a.windowSeq != winSeq || a.workspace != ws {
					return
				}
				targetRel, ok := a.lspRelFromURI(ws, loc.URI)
				if !ok {
					return
				}
				a.recordNav()
				a.openNoteAt(filepath.FromSlash(targetRel), loc.Range.Start.Line+1, loc.Range.Start.Character, loc.Range.Start.Character)
			})
		}()
	}
}

// notifyLSPSaved sends textDocument/didSave for any open LSP document in ws.
func (a *app) notifyLSPSaved(n *note) {
	ws := a.workspace
	if ws == nil || ws.lsp == nil || n == nil {
		return
	}
	rel := filepath.ToSlash(n.Rel)
	if _, ok := ws.lsp.opened[rel]; !ok {
		return
	}
	cl := a.lspClientForNote(ws, n)
	if cl == nil {
		return
	}
	uri := a.lspURIForRel(ws, rel)
	go func() { _ = cl.DidSave(uri) }()
}

// lspDiagCounts returns error and warning counts for rel (or whole workspace if rel == "").
func (a *app) lspDiagCounts(rel string) (errs, warns int) {
	if a.workspace == nil || a.workspace.lsp == nil {
		return 0, 0
	}
	count := func(ds []editor.Diagnostic) {
		for _, d := range ds {
			switch d.Severity {
			case editor.SeverityError:
				errs++
			case editor.SeverityWarning:
				warns++
			}
		}
	}
	if rel != "" {
		if d, ok := a.workspace.lsp.diags[filepath.ToSlash(rel)]; ok {
			count(d.diags)
		}
		return errs, warns
	}
	for _, d := range a.workspace.lsp.diags {
		count(d.diags)
	}
	return errs, warns
}

// lspServerName returns the active LSP server name for rel, or "".
func (a *app) lspServerName(rel string) string {
	if a.workspace == nil || a.workspace.lsp == nil || rel == "" {
		return ""
	}
	lang := lsp.LanguageForPath(rel)
	if lang == "" {
		return ""
	}
	st := a.workspace.lsp.servers[lsp.ServerGroup(lang)]
	if st == nil || st.client == nil || !st.client.Ready() {
		return ""
	}
	return st.name
}

// Editor Git hunk navigation and revert.
func (a *app) editorNextHunk() {
	if p := a.focusPane(); p != nil && p.Note != nil && p.state != nil {
		p.state.Run(editor.NextHunk)
		a.invalidate()
	}
}

func (a *app) editorPrevHunk() {
	if p := a.focusPane(); p != nil && p.Note != nil && p.state != nil {
		p.state.Run(editor.PreviousHunk)
		a.invalidate()
	}
}

func (a *app) editorRevertHunk() {
	if p := a.focusPane(); p != nil && p.Note != nil && p.state != nil {
		p.state.Run(editor.RevertHunk)
		a.invalidate()
	}
}

// syncEditorBlame asynchronously fetches commit attribution for the focused
// editor's active line and displays it as a subtle trailing inlay.
func (a *app) syncEditorBlame(p *pane, st *editor.State, focused bool) {
	if p == nil || p.Note == nil || st == nil {
		return
	}
	if !a.EditorGitBlame || !focused || a.store == nil || a.store.IsMemory() || a.store.Dir == "" {
		if p.blameText != "" {
			p.blameText = ""
			p.blameLine = -1
			st.SetInlays(nil)
		}
		return
	}
	line := st.CursorPos().Line
	ver := p.Note.Buffer().Version()
	if p.blameLine == line && p.blameVer == ver {
		return
	}
	p.blameLine = line
	p.blameVer = ver
	p.blameSeq++
	seq := p.blameSeq
	p.blameText = ""
	st.SetInlays(nil)

	post := a.uiPoster()
	if post == nil {
		return
	}
	runner := a.GitRunner()
	dir := a.store.Dir
	rel := p.Note.Rel
	winSeq := a.windowSeq
	n := p.Note
	go func() {
		info, ok := gitpage.ReadLineBlame(runner, dir, rel, line+1)
		if !ok {
			return
		}
		text := info.InlineText()
		post(func() {
			if a.windowSeq != winSeq || p.blameSeq != seq || p.Note != n || p.state != st {
				return
			}
			if !a.EditorGitBlame || p.Note.Buffer().Version() != ver {
				return
			}
			p.blameText = text
			off := lineEndOffset(n.Text(), line)
			st.SetInlays([]editor.Inlay{{
				Offset:   off,
				Text:     "   " + text,
				Trailing: true,
				Ellipsis: "…",
			}})
			a.invalidate()
		})
	}()
}

func lineEndOffset(text string, line int) int {
	cur := 0
	off := 0
	for cur < line && off < len(text) {
		nl := strings.IndexByte(text[off:], '\n')
		if nl < 0 {
			return len(text)
		}
		off += nl + 1
		cur++
	}
	if nl := strings.IndexByte(text[off:], '\n'); nl >= 0 {
		end := off + nl
		if end > off && text[end-1] == '\r' {
			end--
		}
		return end
	}
	return len(text)
}
