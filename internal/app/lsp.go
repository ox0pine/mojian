package app

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	gitpage "mojian/internal/git"
	"mojian/internal/lsp"
	"mojian/internal/motion"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
)

const maxLSPLogLines = 200

// lspServerState holds one language server session for a workspace language group.
type lspServerState struct {
	group     string
	name      string
	command   []string
	client    *lsp.Client
	starting  bool
	failed    bool
	notFound  bool
	disabled  bool
	errMsg    string
	startedAt time.Time
	logs      []string
}

type lspDocDiag struct {
	version uint64
	diags   []editor.Diagnostic
	raw     []lsp.Diagnostic
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
		if st.starting || st.failed || st.notFound {
			return nil
		}
	}
	spec, initOpts, disabled, ok := lsp.ResolveServer(lang, a.lspServers)
	if !ok {
		return nil
	}
	if disabled {
		mgr.servers[group] = &lspServerState{
			group:    group,
			name:     spec.Name,
			disabled: true,
		}
		return nil
	}
	post := a.uiPoster()
	if post == nil {
		return nil
	}
	st = &lspServerState{
		group:     group,
		name:      spec.Name,
		starting:  true,
		startedAt: time.Now(),
	}
	mgr.servers[group] = st

	dir := ws.store.Dir
	remote := ws.store.IsRemote()
	remoteClient := ws.store.Client()
	winSeq := a.windowSeq
	rootURI := lsp.FileURI(dir)

	go func() {
		var rwc io.ReadWriteCloser
		var chosen []string
		var notFound bool
		var err error
		if remote {
			if remoteClient == nil {
				err = io.EOF
			} else {
				for _, argv := range spec.Commands {
					rwc, err = remoteClient.StartLSP(dir, argv)
					if err == nil {
						chosen = append([]string(nil), argv...)
						break
					}
				}
			}
		} else {
			argv, found := lsp.LookupLocalCommands(spec.Commands)
			if !found {
				notFound = true
			} else {
				chosen = append([]string(nil), argv...)
				rwc, err = lsp.StartLocalWithLog(dir, argv, func(line string) {
					post(func() {
						if a.windowSeq != winSeq || ws.lsp != mgr {
							return
						}
						a.appendLSPLog(ws, st, "[stderr] "+line)
					})
				})
			}
		}
		if notFound {
			post(func() {
				if a.windowSeq != winSeq || ws.lsp != mgr {
					return
				}
				st.starting = false
				st.notFound = true
				st.failed = false
				st.errMsg = ""
				a.appendLSPLog(ws, st, "[info] "+spec.Name+" not found in PATH or standard tool directories")
				a.invalidate()
			})
			return
		}
		if err != nil || rwc == nil {
			msg := "failed to start"
			if err != nil {
				msg = err.Error()
			}
			post(func() {
				if a.windowSeq != winSeq || ws.lsp != mgr {
					return
				}
				st.starting = false
				st.failed = true
				st.command = chosen
				st.errMsg = msg
				a.appendLSPLog(ws, st, "[error] "+msg)
				a.invalidate()
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
		cl.SetLogHandler(func(level int, msg string) {
			post(func() {
				if a.windowSeq != winSeq || ws.lsp != mgr {
					return
				}
				a.appendLSPLog(ws, st, fmt.Sprintf("[lsp:%d] %s", level, msg))
			})
		})
		cl.SetApplyEditHandler(func(we lsp.WorkspaceEdit) bool {
			done := make(chan bool, 1)
			post(func() {
				if a.windowSeq != winSeq || a.workspace != ws {
					done <- false
					return
				}
				a.applyLSPWorkspaceEdit(ws, lsp.NormalizeWorkspaceEdit(we))
				a.invalidate()
				done <- true
			})
			select {
			case ok := <-done:
				return ok
			case <-time.After(2 * time.Second):
				return false
			}
		})
		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		initErr := cl.Initialize(ctx, rootURI, initOpts)
		cancel()
		if initErr != nil {
			_ = cl.Close()
			msg := initErr.Error()
			post(func() {
				if a.windowSeq != winSeq || ws.lsp != mgr {
					return
				}
				st.starting = false
				st.failed = true
				st.command = chosen
				st.errMsg = msg
				a.appendLSPLog(ws, st, "[init error] "+msg)
				a.invalidate()
			})
			return
		}
		post(func() {
			if a.windowSeq != winSeq || ws.lsp != mgr {
				_ = cl.Close()
				return
			}
			st.starting = false
			st.failed = false
			st.errMsg = ""
			st.command = chosen
			st.client = cl
			a.appendLSPLog(ws, st, "[ready] "+strings.Join(chosen, " "))
			a.invalidate()
		})
	}()
	return nil
}

func (a *app) appendLSPLog(ws *workspace, st *lspServerState, line string) {
	if st == nil || line == "" {
		return
	}
	st.logs = append(st.logs, line)
	if len(st.logs) > maxLSPLogLines {
		st.logs = append([]string(nil), st.logs[len(st.logs)-maxLSPLogLines:]...)
	}
	if a.workspace == ws && a.lspStatusOpen {
		a.invalidate()
	}
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
	ws.lsp.diags[rel] = lspDocDiag{version: buf.Version(), diags: edDiags, raw: append([]lsp.Diagnostic(nil), params.Diagnostics...)}
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
			detailed, err := cl.CompletionDetailed(ctx, uri, pos)
			cancel()
			if err != nil || len(detailed) == 0 {
				return
			}
			items := make([]editor.Completion, len(detailed))
			for i, d := range detailed {
				items[i] = d.Item
			}
			post(func() {
				if a.windowSeq != winSeq || p.state != st || p.Note != n {
					return
				}
				if n.Buffer().Version() != req.Version {
					return
				}
				p.compSeq++
				p.compResults = detailed
				p.compResolved = nil
				p.compInFlight = nil
				p.compStartVer = req.Version
				p.compSelectedIdx = 0
				p.compAcceptHint = false
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

// notifyLSPWatchedFiles broadcasts workspace/didChangeWatchedFiles to every
// ready language server in ws.
func (a *app) notifyLSPWatchedFiles(ws *workspace, changes []lsp.FileEvent) {
	if ws == nil || ws.lsp == nil || len(changes) == 0 {
		return
	}
	var clients []*lsp.Client
	for _, st := range ws.lsp.servers {
		if st != nil && st.client != nil && st.client.Ready() {
			clients = append(clients, st.client)
		}
	}
	if len(clients) == 0 {
		return
	}
	evCopy := append([]lsp.FileEvent(nil), changes...)
	go func() {
		for _, cl := range clients {
			_ = cl.DidChangeWatchedFiles(evCopy)
		}
	}()
}

// notifyLSPFileChange notifies active workspace language servers of a single
// relative path change (typ: lsp.FileCreated, FileChanged, or FileDeleted).
func (a *app) notifyLSPFileChange(rel string, typ int) {
	ws := a.workspace
	if ws == nil || ws.store == nil || rel == "" {
		return
	}
	uri := a.lspURIForRel(ws, filepath.ToSlash(rel))
	if uri == "" {
		return
	}
	a.notifyLSPWatchedFiles(ws, []lsp.FileEvent{{URI: uri, Type: typ}})
}

func (a *app) resolveCompletionItemAsync(p *pane, origIdx int, raw json.RawMessage) {
	if p == nil || p.Note == nil || a.workspace == nil || len(raw) == 0 {
		return
	}
	ws := a.workspace
	n := p.Note
	cl := a.lspClientForNote(ws, n)
	if cl == nil {
		return
	}
	post := a.uiPoster()
	if post == nil {
		return
	}
	if p.compInFlight == nil {
		p.compInFlight = map[int]bool{}
	}
	if p.compInFlight[origIdx] {
		return
	}
	p.compInFlight[origIdx] = true
	seq := p.compSeq
	winSeq := a.windowSeq
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 1200*time.Millisecond)
		res, err := cl.ResolveCompletion(ctx, raw)
		cancel()
		post(func() {
			if a.windowSeq != winSeq || p.compSeq != seq || p.Note != n {
				return
			}
			delete(p.compInFlight, origIdx)
			if err != nil {
				return
			}
			if p.compResolved == nil {
				p.compResolved = map[int]lsp.CompletionResult{}
			}
			p.compResolved[origIdx] = res
			a.invalidate()
		})
	}()
}

// finishCompletionFrame detects when a completion menu just closed due to
// acceptance and applies any auto-import additionalTextEdits in memory.
func (a *app) finishCompletionFrame(p *pane, st *editor.State) {
	if p == nil || st == nil || p.Note == nil {
		return
	}
	wasOpen := p.compPrevOpen
	isOpen := p.compOpen
	p.compPrevOpen = isOpen
	p.compOpen = false

	if !wasOpen || isOpen {
		return
	}
	accepted := p.compAcceptHint
	p.compAcceptHint = false
	if !accepted || p.Note.Buffer().Version() == p.compStartVer {
		return
	}
	idx := p.compSelectedIdx
	if idx < 0 || idx >= len(p.compResults) {
		return
	}
	if res, ok := p.compResolved[idx]; ok && len(res.AdditionalTextEdits) > 0 {
		applyTargetedLSPEdits(p.Note, res.AdditionalTextEdits)
		a.invalidate()
		return
	}
	base := p.compResults[idx]
	if len(base.AdditionalTextEdits) > 0 {
		applyTargetedLSPEdits(p.Note, base.AdditionalTextEdits)
		a.invalidate()
		return
	}
	if !base.HasData || len(base.Raw) == 0 || a.workspace == nil {
		return
	}
	ws := a.workspace
	n := p.Note
	cl := a.lspClientForNote(ws, n)
	if cl == nil {
		return
	}
	post := a.uiPoster()
	if post == nil {
		return
	}
	verAfterAccept := n.Buffer().Version()
	winSeq := a.windowSeq
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 800*time.Millisecond)
		res, err := cl.ResolveCompletion(ctx, base.Raw)
		cancel()
		if err != nil || len(res.AdditionalTextEdits) == 0 {
			return
		}
		post(func() {
			if a.windowSeq != winSeq || p.Note != n || n.Buffer().Version() != verAfterAccept {
				return
			}
			applyTargetedLSPEdits(n, res.AdditionalTextEdits)
			a.invalidate()
		})
	}()
}

// applyTargetedLSPEdits applies targeted LSP TextEdits (such as auto-imports)
// atomically via buf.Edit without replacing the whole document, preserving the
// user's cursor position and ⌘Z undo history.
func applyTargetedLSPEdits(n *note, edits []lsp.TextEdit) {
	if n == nil || len(edits) == 0 {
		return
	}
	text := n.Text()
	edEdits := make([]editor.Edit, 0, len(edits))
	for _, e := range edits {
		s := lsp.PositionToOffset(text, e.Range.Start)
		end := lsp.PositionToOffset(text, e.Range.End)
		if end < s {
			end = s
		}
		edEdits = append(edEdits, editor.Edit{Start: s, End: end, Text: e.NewText})
	}
	sort.SliceStable(edEdits, func(i, j int) bool {
		if edEdits[i].Start != edEdits[j].Start {
			return edEdits[i].Start < edEdits[j].Start
		}
		return edEdits[i].End < edEdits[j].End
	})
	filtered := edEdits[:0]
	lastEnd := 0
	for _, e := range edEdits {
		if e.Start < lastEnd {
			continue
		}
		filtered = append(filtered, e)
		lastEnd = e.End
	}
	if len(filtered) > 0 {
		n.Buffer().Edit(filtered...)
	}
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
	st := a.lspServerStateFor(rel)
	if st == nil || st.client == nil || !st.client.Ready() {
		return ""
	}
	return st.name
}

func (a *app) lspServerStateFor(rel string) *lspServerState {
	if a.workspace == nil || a.workspace.lsp == nil || rel == "" {
		return nil
	}
	lang := lsp.LanguageForPath(rel)
	if lang == "" {
		return nil
	}
	return a.workspace.lsp.servers[lsp.ServerGroup(lang)]
}

// syncEditorHighlights queries LSP textDocument/documentHighlight for the
// symbol at the focused editor's caret and renders subtle tinted spans via
// st.TextBackgrounds.
func (a *app) syncEditorHighlights(p *pane, st *editor.State, focused bool, k *colors) {
	if p == nil || p.Note == nil || st == nil {
		return
	}
	ver := p.Note.Buffer().Version()
	if !focused || a.workspace == nil || a.store == nil || a.store.IsMemory() {
		if len(p.hlRanges) > 0 {
			p.hlRanges = nil
			st.TextBackgrounds = nil
		}
		return
	}
	if p.hlVer != ver {
		p.hlRanges = nil
		st.TextBackgrounds = nil
	}
	cl := a.lspClientForNote(a.workspace, p.Note)
	if cl == nil {
		if len(p.hlRanges) > 0 {
			p.hlRanges = nil
			st.TextBackgrounds = nil
		}
		return
	}
	off := st.Cursor()
	if p.hlOffset == off && p.hlVer == ver {
		st.TextBackgrounds = p.hlRanges
		return
	}
	p.hlOffset = off
	p.hlVer = ver
	p.hlSeq++
	seq := p.hlSeq
	text := p.Note.Text()
	if wordAtOffset(text, off) == "" {
		p.hlRanges = nil
		st.TextBackgrounds = nil
		return
	}
	post := a.uiPoster()
	if post == nil {
		return
	}
	ws := a.workspace
	n := p.Note
	uri := a.lspURIForRel(ws, filepath.ToSlash(n.Rel))
	pos := lsp.OffsetToPosition(text, off)
	winSeq := a.windowSeq
	hlColor := k.Attention.Alpha(0.14)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 1500*time.Millisecond)
		hls, err := cl.DocumentHighlight(ctx, uri, pos)
		cancel()
		if err != nil {
			return
		}
		var bgs []editor.TextBackground
		for _, h := range hls {
			s := lsp.PositionToOffset(text, h.Range.Start)
			e := lsp.PositionToOffset(text, h.Range.End)
			if e > s {
				bgs = append(bgs, editor.TextBackground{Start: s, End: e, Color: hlColor})
			}
		}
		post(func() {
			if a.windowSeq != winSeq || p.hlSeq != seq || p.Note != n || p.state != st || n.Buffer().Version() != ver {
				return
			}
			p.hlRanges = bgs
			st.TextBackgrounds = bgs
			a.invalidate()
		})
	}()
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

// Editor code folding commands (exposed to Command Palette, menus & shortcuts).
func (a *app) editorFold() {
	if p := a.focusPane(); p != nil && p.Note != nil && p.state != nil {
		p.state.Run(editor.Fold)
		a.invalidate()
	}
}

func (a *app) editorUnfold() {
	if p := a.focusPane(); p != nil && p.Note != nil && p.state != nil {
		p.state.Run(editor.Unfold)
		a.invalidate()
	}
}

func (a *app) editorToggleFold() {
	if p := a.focusPane(); p != nil && p.Note != nil && p.state != nil {
		p.state.Run(editor.ToggleFold)
		a.invalidate()
	}
}

func (a *app) editorFoldAll() {
	if p := a.focusPane(); p != nil && p.Note != nil && p.state != nil {
		p.state.Run(editor.FoldAll)
		a.invalidate()
	}
}

func (a *app) editorUnfoldAll() {
	if p := a.focusPane(); p != nil && p.Note != nil && p.state != nil {
		p.state.Run(editor.UnfoldAll)
		a.invalidate()
	}
}

func (a *app) editorFoldImports() {
	if p := a.focusPane(); p != nil && p.Note != nil && p.state != nil {
		p.state.FoldImports()
		a.invalidate()
	}
}

// toggleInlayHints flips LSP inlay hints and persists the preference.
func (a *app) toggleInlayHints() {
	a.NoInlayHints = !a.NoInlayHints
	a.saveSettings()
	if t := a.tab(); t != nil {
		for _, p := range t.panes() {
			if p.state != nil {
				if a.NoInlayHints {
					p.inlayHints = nil
					p.inlayLoaded = false
					applyPaneInlays(p, p.state)
				} else {
					p.inlayLoaded = false
				}
			}
		}
	}
	a.invalidate()
}

// applyPaneInlays merges LSP inlay hints and the optional trailing Git blame
// inlay into a single SetInlays call so both features coexist cleanly.
func applyPaneInlays(p *pane, st *editor.State) {
	if p == nil || st == nil {
		return
	}
	n := len(p.inlayHints)
	if p.blameInlay != nil {
		n++
	}
	if n == 0 {
		st.SetInlays(nil)
		return
	}
	out := make([]editor.Inlay, 0, n)
	out = append(out, p.inlayHints...)
	if p.blameInlay != nil {
		out = append(out, *p.blameInlay)
	}
	st.SetInlays(out)
}

// syncEditorInlays asynchronously queries textDocument/inlayHint for the open
// note and merges them with any active inline Git blame inlay.
func (a *app) syncEditorInlays(p *pane, st *editor.State, focused bool) {
	if p == nil || p.Note == nil || st == nil {
		return
	}
	if a.NoInlayHints || a.workspace == nil || a.store == nil || a.store.IsMemory() {
		if len(p.inlayHints) > 0 || p.inlayLoaded {
			p.inlayHints = nil
			p.inlayLoaded = false
			applyPaneInlays(p, st)
		}
		return
	}
	cl := a.lspClientForNote(a.workspace, p.Note)
	if cl == nil {
		if len(p.inlayHints) > 0 {
			p.inlayHints = nil
			p.inlayLoaded = false
			applyPaneInlays(p, st)
		}
		return
	}
	ver := p.Note.Buffer().Version()
	if p.inlayLoaded && p.inlayNote == p.Note && p.inlayVer == ver {
		return
	}
	if !focused && p.inlayLoaded && p.inlayNote == p.Note {
		return
	}
	p.inlayNote = p.Note
	p.inlayVer = ver
	p.inlayLoaded = true
	p.inlaySeq++
	seq := p.inlaySeq

	post := a.uiPoster()
	if post == nil {
		return
	}
	ws := a.workspace
	n := p.Note
	text := n.Text()
	lineCount := max(n.Buffer().LineCount(), 1)
	uri := a.lspURIForRel(ws, filepath.ToSlash(n.Rel))
	rng := lsp.Range{
		Start: lsp.Position{Line: 0, Character: 0},
		End:   lsp.Position{Line: lineCount, Character: 0},
	}
	winSeq := a.windowSeq
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		hints, err := cl.InlayHints(ctx, uri, rng)
		cancel()
		if err != nil {
			return
		}
		inlays := lspInlayHintsToEditor(text, hints)
		post(func() {
			if a.windowSeq != winSeq || p.inlaySeq != seq || p.Note != n || p.state != st {
				return
			}
			if a.NoInlayHints || n.Buffer().Version() != ver {
				return
			}
			p.inlayHints = inlays
			applyPaneInlays(p, st)
			a.invalidate()
		})
	}()
}

func lspInlayHintsToEditor(text string, hints []lsp.InlayHint) []editor.Inlay {
	if len(hints) == 0 {
		return nil
	}
	const maxHints = 300
	limit := min(len(hints), maxHints)
	out := make([]editor.Inlay, 0, limit)
	for _, h := range hints[:limit] {
		label := strings.TrimSpace(h.LabelText())
		if label == "" {
			continue
		}
		var b strings.Builder
		if h.PaddingLeft || h.Kind == 1 {
			b.WriteByte(' ')
		}
		b.WriteString(label)
		if h.PaddingRight || h.Kind == 2 {
			b.WriteByte(' ')
		}
		off := lsp.PositionToOffset(text, h.Position)
		out = append(out, editor.Inlay{
			Offset: off,
			Text:   b.String(),
		})
	}
	return out
}

// triggerSignatureHelp explicitly queries textDocument/signatureHelp at the
// focused editor's cursor (⇧⌘Space).
func (a *app) triggerSignatureHelp() {
	p := a.focusPane()
	if p == nil || p.Note == nil || p.state == nil || a.workspace == nil {
		return
	}
	p.sigDismissedVer = 0
	p.sigDismissedOff = -1
	a.requestSignatureHelp(p, p.state, true)
}

func (a *app) dismissSignatureHelp(p *pane) {
	if p == nil || p.Note == nil || p.state == nil {
		return
	}
	p.sigHelp = nil
	p.sigDismissedVer = p.Note.Buffer().Version()
	p.sigDismissedOff = p.state.Cursor()
	a.invalidate()
}

// syncEditorSignatureHelp automatically queries signature help while the
// focused caret is inside a function call's parentheses.
func (a *app) syncEditorSignatureHelp(p *pane, st *editor.State, focused bool) {
	if p == nil || p.Note == nil || st == nil {
		return
	}
	if !focused || a.workspace == nil || a.store == nil || a.store.IsMemory() {
		if p.sigHelp != nil {
			p.sigHelp = nil
		}
		return
	}
	off := st.Cursor()
	ver := p.Note.Buffer().Version()
	if p.sigDismissedVer == ver && p.sigDismissedOff == off {
		return
	}
	if p.sigOffset == off && p.sigVer == ver {
		return
	}
	text := p.Note.Text()
	if !inCallContext(text, off) {
		p.sigOffset = off
		p.sigVer = ver
		p.sigHelp = nil
		return
	}
	a.requestSignatureHelp(p, st, false)
}

func (a *app) requestSignatureHelp(p *pane, st *editor.State, explicit bool) {
	cl := a.lspClientForNote(a.workspace, p.Note)
	if cl == nil {
		p.sigHelp = nil
		return
	}
	off := st.Cursor()
	ver := p.Note.Buffer().Version()
	p.sigOffset = off
	p.sigVer = ver
	p.sigSeq++
	seq := p.sigSeq

	post := a.uiPoster()
	if post == nil {
		return
	}
	ws := a.workspace
	n := p.Note
	text := n.Text()
	uri := a.lspURIForRel(ws, filepath.ToSlash(n.Rel))
	pos := lsp.OffsetToPosition(text, off)
	winSeq := a.windowSeq
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 1500*time.Millisecond)
		sh, err := cl.SignatureHelp(ctx, uri, pos)
		cancel()
		if err != nil {
			if explicit {
				post(func() {
					if a.windowSeq == winSeq && p.sigSeq == seq {
						p.sigHelp = nil
						a.invalidate()
					}
				})
			}
			return
		}
		post(func() {
			if a.windowSeq != winSeq || p.sigSeq != seq || p.Note != n || p.state != st || n.Buffer().Version() != ver {
				return
			}
			p.sigHelp = sh
			a.invalidate()
		})
	}()
}

// inCallContext reports whether byte offset off sits inside an unclosed call
// parenthesis '(' on the nearby preceding text.
func inCallContext(text string, off int) bool {
	if off <= 0 || off > len(text) {
		return false
	}
	start := max(0, off-400)
	depth := 0
	sq, dq, bq := false, false, false
	for i := off - 1; i >= start; i-- {
		ch := text[i]
		if sq {
			if ch == '\'' && (i == 0 || text[i-1] != '\\') {
				sq = false
			}
			continue
		}
		if dq {
			if ch == '"' && (i == 0 || text[i-1] != '\\') {
				dq = false
			}
			continue
		}
		if bq {
			if ch == '`' {
				bq = false
			}
			continue
		}
		switch ch {
		case '\'':
			sq = true
		case '"':
			dq = true
		case '`':
			bq = true
		case ')':
			depth++
		case '(':
			if depth == 0 {
				return true
			}
			depth--
		case ';', '{', '}':
			if depth == 0 {
				return false
			}
		}
	}
	return false
}

// signatureHelpOverlay draws a quiet Liquid Glass floating signature hint card
// inside the focused editor pane.
func (a *app) signatureHelpOverlay(c *ui.Context, k *colors, p *pane) {
	if p == nil || p.sigHelp == nil {
		return
	}
	sig, activeParam, ok := p.sigHelp.Active()
	if !ok || strings.TrimSpace(sig.Label) == "" {
		return
	}
	card := a.surface(
		ui.Column(c.Key("sig-help:"+p.ID)).Absolute().Top(8).Right(14).
			MaxWidth(460).Padding(7, 10).Gap(3).Radius(radius),
		k.Panel,
	).Border(1, k.PanelBorder).
		Shadow(0, 4, 16, -2, k.ShadowDialog).
		Transition(motion.Dialog)
	card.Children(func() {
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			spans := signatureSpans(c, k, sig, activeParam)
			ui.RichText(c, spans...).Font(noteFont.Family).FontSize(c.Theme().Rem(11.5 / 13)).
				Grow(1).MinWidth(0)
			if len(p.sigHelp.Signatures) > 1 {
				idx := p.sigHelp.ActiveSignature
				if idx < 0 || idx >= len(p.sigHelp.Signatures) {
					idx = 0
				}
				ui.Text(c, fmt.Sprintf("%d/%d", idx+1, len(p.sigHelp.Signatures))).
					FontSize(c.Theme().Rem(10.5 / 13)).TextColor(k.TextFaint).Shrink(0)
			}
			if iconButton(c, k, "x", "Dismiss Signature Help", iconBtnXs, iconGlyphXs).Clicked() {
				a.later(func() { a.dismissSignatureHelp(p) })
			}
		})
		doc := ""
		if activeParam >= 0 && activeParam < len(sig.Parameters) {
			doc = firstNonEmptyLine(sig.Parameters[activeParam].DocText())
		}
		if doc == "" {
			doc = firstNonEmptyLine(sig.DocText())
		}
		if doc != "" {
			ui.Text(c, doc).FontSize(c.Theme().Rem(11.0 / 13)).TextColor(k.TextMuted).
				SingleLine().Ellipsis("…")
		}
	})
}

func signatureSpans(c *ui.Context, k *colors, sig lsp.SignatureInformation, activeParam int) []ui.Span {
	sz := c.Theme().Rem(11.5 / 13)
	s, e, ok := sig.ActiveParamSpan(activeParam)
	if !ok {
		return []ui.Span{{Text: sig.Label, Size: sz, Color: k.Text}}
	}
	var out []ui.Span
	if s > 0 {
		out = append(out, ui.Span{Text: sig.Label[:s], Size: sz, Color: k.TextMuted})
	}
	out = append(out, ui.Span{Text: sig.Label[s:e], Size: sz, Weight: 600, Color: c.Theme().Accent})
	if e < len(sig.Label) {
		out = append(out, ui.Span{Text: sig.Label[e:], Size: sz, Color: k.TextMuted})
	}
	return out
}

func firstNonEmptyLine(s string) string {
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			return line
		}
	}
	return ""
}

// syncEditorBlame asynchronously fetches commit attribution for the focused
// editor's active line and displays it as a subtle trailing inlay.
func (a *app) syncEditorBlame(p *pane, st *editor.State, focused bool) {
	if p == nil || p.Note == nil || st == nil {
		return
	}
	if !a.EditorGitBlame || !focused || a.store == nil || a.store.IsMemory() || a.store.Dir == "" {
		if p.blameText != "" || p.blameInlay != nil {
			p.blameText = ""
			p.blameLine = -1
			p.blameInlay = nil
			applyPaneInlays(p, st)
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
	p.blameInlay = nil
	applyPaneInlays(p, st)

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
			inlay := editor.Inlay{
				Offset:   off,
				Text:     "   " + text,
				Trailing: true,
				Ellipsis: "…",
			}
			p.blameInlay = &inlay
			applyPaneInlays(p, st)
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
