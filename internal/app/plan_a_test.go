package app

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mojian/internal/app/commands"
	"mojian/internal/app/explorer"
	"mojian/internal/fs"
	gitpage "mojian/internal/git"
	"mojian/internal/lsp"
	"mojian/internal/project"
	"mojian/internal/testutil"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
)

func TestSaveConflictModalReloadAndOverwrite(t *testing.T) {
	// 1) Reload from Disk
	a := newProjectApp(t)
	n := a.currentNote()
	n.Buffer().Set("local draft\n")
	if err := os.WriteFile(n.Path, []byte("disk version\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !a.commandRegistry().Run("save") {
		t.Fatal("save command not registered")
	}
	if a.conflict == nil {
		t.Fatal("expected visual conflict dialog to open on external file change")
	}
	tt := ui.NewTester(a.view, 1100, 720)
	tt.Frame()
	if !tt.HasText("Reload from Disk") || !tt.HasText("Overwrite Disk") {
		t.Fatalf("conflict modal buttons missing: %v", tt.Texts())
	}
	a.resolveConflictReload()
	if a.conflict != nil || a.err != "" {
		t.Fatalf("expected conflict and error cleared after reload, got conflict=%v err=%q", a.conflict, a.err)
	}
	if n.Dirty() || n.Text() != "disk version\n" {
		t.Fatalf("expected clean disk version, got dirty=%v text=%q", n.Dirty(), n.Text())
	}

	// 2) Overwrite Disk
	n.Buffer().Set("my override\n")
	if err := os.WriteFile(n.Path, []byte("other external edit\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	a.commandRegistry().Run("save")
	if a.conflict == nil {
		t.Fatal("expected conflict dialog for second conflict")
	}
	a.resolveConflictOverwrite()
	if a.conflict != nil || a.err != "" {
		t.Fatalf("expected conflict cleared after overwrite, got conflict=%v err=%q", a.conflict, a.err)
	}
	data, err := os.ReadFile(n.Path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "my override\n" || n.Dirty() {
		t.Fatalf("expected disk overwritten and note clean, got disk=%q dirty=%v", string(data), n.Dirty())
	}
}

func TestPaneSubtitleDisambiguatesDuplicateNames(t *testing.T) {
	root := t.TempDir()
	for rel, body := range map[string]string{
		"pkg/a/view.go": "package a\n",
		"pkg/b/view.go": "package b\n",
		"unique.go":     "package main\n",
	} {
		full := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	s := newDirectStore(t, root)
	nA := s.ByRelPath(filepath.FromSlash("pkg/a/view.go"))
	nB := s.ByRelPath(filepath.FromSlash("pkg/b/view.go"))
	nU := s.ByRelPath("unique.go")
	pA, pB, pU := newPane(nA), newPane(nB), newPane(nU)
	tab := &tab{
		ID:    "t1",
		Focus: pA,
		Root: &node{
			A: &node{Pane: pA},
			B: &node{
				A: &node{Pane: pB},
				B: &node{Pane: pU},
			},
		},
	}
	if got := paneSubtitle(tab, pA); got != "a" {
		t.Fatalf("paneSubtitle(pA) = %q, want %q", got, "a")
	}
	if got := paneSubtitle(tab, pB); got != "b" {
		t.Fatalf("paneSubtitle(pB) = %q, want %q", got, "b")
	}
	if got := paneSubtitle(tab, pU); got != "" {
		t.Fatalf("paneSubtitle(pU) = %q, want empty", got)
	}
}

func TestExplorerGitStatusBadges(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "src", "app.go"), []byte("package src\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("# Hi\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	testutil.InitRepo(t, root)
	if err := os.WriteFile(filepath.Join(root, "src", "app.go"), []byte("package src\n// mod\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "notes.txt"), []byte("untracked\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	a := &app{workspace: &workspace{store: newDirectStore(t, root), treeOpen: true}}
	a.openDirs = map[string]bool{"src": true}
	a.refreshGitStatus()

	if got := a.gitStatusFor("src/app.go"); got != "M" {
		t.Fatalf("gitStatusFor(src/app.go) = %q, want M", got)
	}
	if got := a.gitStatusFor("notes.txt"); got != "U" {
		t.Fatalf("gitStatusFor(notes.txt) = %q, want U", got)
	}
	if !a.gitDirDirtyFor("src") {
		t.Fatal("expected src directory to be marked dirty")
	}
	if a.gitDirDirtyFor("other") {
		t.Fatal("did not expect other directory to be dirty")
	}
	tt := ui.NewTester(a.view, 1100, 700)
	tt.Frame()
	if !tt.HasText("M") || !tt.HasText("U") {
		t.Fatalf("expected M and U git badges in Explorer, got texts: %v", tt.Texts())
	}

	// Verify applyGitChanges clears when empty
	applyGitChanges(a.workspace, []gitpage.Change{})
	if a.gitStatusFor("src/app.go") != "" || a.gitDirDirtyFor("src") {
		t.Fatal("expected git status cleared on empty changes")
	}
}

func TestEditorConfigAndInteractiveStatusBar(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, ".editorconfig"), []byte(`root = true
[*.ts]
indent_style = space
indent_size = 2
trim_trailing_whitespace = true
insert_final_newline = true
end_of_line = lf
`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "main.ts"), []byte("const x = 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	a := &app{workspace: &workspace{store: newDirectStore(t, root)}}
	a.ShowStatusBar = true
	a.EditorTabSize = 4
	n := a.store.ByRelPath("main.ts")
	a.openNote(n)
	p := a.focusPane()

	style, size := a.effectiveIndent(p, n)
	if style != editor.IndentSpaces || size != 2 {
		t.Fatalf("effectiveIndent = (%v, %d), want (Spaces, 2)", style, size)
	}

	// Dirty edit with trailing spaces and missing trailing newline; explicit save applies EditorConfig transforms.
	n.Buffer().Set("const x = 2   \nconst y = 3  ")
	if err := a.saveNow(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(n.Path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "const x = 2\nconst y = 3\n" {
		t.Fatalf("saved text = %q, want trimmed with final newline", string(data))
	}

	// Interactive status bar: override indent and toggle EOL.
	tt := ui.NewTester(a.view, 1100, 700)
	tt.Frame()
	if !tt.HasText("Spaces: 2") || !tt.HasText("LF") {
		t.Fatalf("expected status bar to display Spaces: 2 and LF, got %v", tt.Texts())
	}
	p.indentOverride = editor.IndentTabs
	p.tabSizeOverride = 8
	n.Buffer().SetLineEnding("\r\n")
	tt.Frame()
	if !tt.HasText("Tabs: 8") || !tt.HasText("CRLF") {
		t.Fatalf("expected status bar to reflect overrides Tabs: 8 and CRLF, got %v", tt.Texts())
	}
}

func TestMarkdownPreviewSideScrollSyncAndTaskToggle(t *testing.T) {
	a := newProjectApp(t)
	n := a.currentNote()
	n.Buffer().Set("# Tasks\n\n- [ ] First task\n- [x] Done task\n")
	a.focusPane().Preview = false

	if !a.commandRegistry().Run("markdown-preview-side") {
		t.Fatal("markdown-preview-side command not registered")
	}
	panes := a.tab().panes()
	if len(panes) != 2 {
		t.Fatalf("expected 2 panes after opening preview to side, got %d", len(panes))
	}
	if panes[0].Preview || !panes[1].Preview || panes[0].Note != panes[1].Note {
		t.Fatalf("expected source + preview panes sharing same note")
	}

	// Toggle task checkbox in preview updates shared buffer in memory (undoable, dirty).
	toggleMarkdownTask(n, 3, true)
	if !strings.Contains(n.Text(), "- [x] First task") {
		t.Fatalf("expected task line 3 checked, got %q", n.Text())
	}
	if !n.Dirty() {
		t.Fatal("expected toggling task checkbox to leave buffer dirty until explicit save")
	}
	toggleMarkdownTask(n, 4, false)
	if !strings.Contains(n.Text(), "- [ ] Done task") {
		t.Fatalf("expected task line 4 unchecked, got %q", n.Text())
	}
}

func TestTabReorderReopenClosedAndFileDrop(t *testing.T) {
	root := t.TempDir()
	for name, body := range map[string]string{
		"a.md": "# A\n",
		"b.md": "# B\n",
		"c.md": "# C\n",
	} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	a := &app{workspace: &workspace{store: newDirectStore(t, root)}}
	a.openNoteInTab(a.store.ByRelPath("a.md"))
	a.openNoteInTab(a.store.ByRelPath("b.md"))
	a.openNoteInTab(a.store.ByRelPath("c.md"))

	// Reorder tab 0 (a.md) to index 2 while active is index 2 (c.md).
	tabA := a.tabs[0]
	tabC := a.tabs[2]
	a.moveTab(tabA, 2)
	if a.tabs[2] != tabA {
		t.Fatalf("expected tabA at index 2")
	}
	if a.tab() != tabC {
		t.Fatalf("expected active tab to remain tabC after reorder")
	}

	// Close tabA and reopen it via reopen-closed.
	tabA.Name = "Pinned A"
	a.closeTab(tabA)
	if len(a.tabs) != 2 {
		t.Fatalf("expected 2 tabs after close, got %d", len(a.tabs))
	}
	if !a.commandRegistry().Run("reopen-closed") {
		t.Fatal("reopen-closed command not registered")
	}
	if len(a.tabs) != 3 || a.tab().Name != "Pinned A" || a.currentNote().Rel != "a.md" {
		t.Fatalf("reopen-closed did not restore tabA: tabs=%d name=%q rel=%q",
			len(a.tabs), a.tab().Name, a.currentNote().Rel)
	}

	// Explorer drag-drop onto pane splits right.
	before := len(a.tab().panes())
	a.openDroppedExplorerFile(a.tab(), a.focusPane(), "b.md", dropRight)
	if len(a.tab().panes()) != before+1 || a.focusPane().Note.Rel != "b.md" {
		t.Fatalf("expected explorer drop to split right with b.md")
	}

	// Explorer MoveEntry moves file into subfolder.
	if err := os.MkdirAll(filepath.Join(root, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	_ = a.store.Refresh()
	a.explorerUI().MoveEntry("c.md", "sub")
	if a.store.ByRelPath(filepath.Join("sub", "c.md")) == nil {
		t.Fatal("expected c.md moved into sub/")
	}
	_ = explorer.DragItem{Rel: "sub/c.md"}

	// OS FileDrop of a file inside the current project opens it directly.
	newFile := filepath.Join(root, "dropped.txt")
	if err := os.WriteFile(newFile, []byte("from finder\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	a.handleFileDrop([]string{newFile})
	if a.currentNote() == nil || a.currentNote().Rel != "dropped.txt" {
		t.Fatalf("expected dropped.txt opened, got %v", a.currentNote())
	}
}

func TestCodeActionsAndFormatOnSaveWithFakeLSP(t *testing.T) {
	root := t.TempDir()
	goPath := filepath.Join(root, "main.go")
	if err := os.WriteFile(goPath, []byte("package main\nfunc main(){x:=1}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	a := &app{workspace: &workspace{store: newDirectStore(t, root)}}
	n := a.store.ByRelPath("main.go")
	a.openNote(n)
	p := a.focusPane()
	p.state = n.NewState()

	// Spin up an in-memory fake LSP server handling initialize, codeAction and formatting.
	c2sR, c2sW := io.Pipe()
	s2cR, s2cW := io.Pipe()
	rwc := struct {
		io.Reader
		io.Writer
		io.Closer
	}{s2cR, c2sW, closerFunc(func() error { _ = c2sW.Close(); _ = s2cW.Close(); return nil })}

	go serveFakeLSP(c2sR, s2cW, lsp.FileURI(goPath))

	cl := lsp.NewClient(rwc, nil)
	defer cl.Close()
	if err := cl.Initialize(t.Context(), lsp.FileURI(root)); err != nil {
		t.Fatalf("fake LSP initialize failed: %v", err)
	}
	mgr := a.workspaceLSP(a.workspace)
	mgr.servers[lsp.ServerGroup("go")] = &lspServerState{group: "go", name: "fake-gopls", client: cl}

	// 1) Test Format on Save
	a.EditorFormatOnSave = true
	n.Buffer().Set("package main\nfunc main(){x:=2}\n")
	var saveErr error
	a.saveNowAsync(func(err error) { saveErr = err })
	if saveErr != nil {
		t.Fatalf("saveNowAsync failed: %v", saveErr)
	}
	got, err := os.ReadFile(goPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "package main\n\nfunc main() { x := 2 }\n" {
		t.Fatalf("format on save did not format file: %q", string(got))
	}

	// 2) Test CodeAction apply in memory
	actions, err := cl.CodeActions(t.Context(), lsp.FileURI(goPath), lsp.Range{}, nil)
	if err != nil || len(actions) != 1 {
		t.Fatalf("CodeActions = (%v, %v), want 1 action", actions, err)
	}
	a.codeActionsWS = a.workspace
	a.codeActionsNote = n
	a.applyCodeAction(actions[0])
	if !strings.Contains(n.Text(), "// fixed") || !n.Dirty() {
		t.Fatalf("applyCodeAction did not apply edit in memory: text=%q dirty=%v", n.Text(), n.Dirty())
	}

	// Verify new commands exist in static palette.
	for _, id := range []string{"code-actions", "reopen-closed", "markdown-preview-side"} {
		if commands.ByID(id) == nil {
			t.Fatalf("command %q missing from Palette", id)
		}
	}
}

func TestLiquidGlassRendersFloatingChrome(t *testing.T) {
	a := newProjectApp(t)
	a.openPalette()
	tt := ui.NewTester(a.view, 1100, 700)
	tt.Frame()
	if !tt.HasText("Open Project…") {
		t.Fatalf("expected palette rendered with Liquid Glass, got %v", tt.Texts())
	}
}

type closerFunc func() error

func (f closerFunc) Close() error { return f() }

func serveFakeLSP(r io.Reader, w io.Writer, targetURI string) {
	for {
		var header bytes.Buffer
		buf := make([]byte, 1)
		for {
			if _, err := r.Read(buf); err != nil {
				return
			}
			header.Write(buf)
			if bytes.HasSuffix(header.Bytes(), []byte("\r\n\r\n")) {
				break
			}
		}
		var contentLen int
		for _, line := range strings.Split(header.String(), "\r\n") {
			if k, v, ok := strings.Cut(line, ":"); ok && strings.EqualFold(strings.TrimSpace(k), "Content-Length") {
				fmt.Sscanf(strings.TrimSpace(v), "%d", &contentLen)
			}
		}
		body := make([]byte, contentLen)
		if _, err := io.ReadFull(r, body); err != nil {
			return
		}
		var req struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
		}
		if json.Unmarshal(body, &req) != nil || len(req.ID) == 0 {
			continue
		}
		var result any
		switch req.Method {
		case "initialize":
			result = map[string]any{"capabilities": map[string]any{}}
		case "textDocument/formatting":
			result = []lsp.TextEdit{{
				Range: lsp.Range{
					Start: lsp.Position{Line: 0, Character: 0},
					End:   lsp.Position{Line: 10, Character: 0},
				},
				NewText: "package main\n\nfunc main() { x := 2 }\n",
			}}
		case "textDocument/codeAction":
			result = []lsp.CodeAction{{
				Title: "Add fix comment",
				Kind:  "quickfix",
				Edit: &lsp.WorkspaceEdit{
					Changes: map[string][]lsp.TextEdit{
						targetURI: {{
							Range:   lsp.Range{Start: lsp.Position{Line: 0, Character: 0}, End: lsp.Position{Line: 0, Character: 0}},
							NewText: "// fixed\n",
						}},
					},
				},
			}}
		default:
			result = nil
		}
		resp, _ := json.Marshal(map[string]any{
			"jsonrpc": "2.0",
			"id":      req.ID,
			"result":  result,
		})
		fmt.Fprintf(w, "Content-Length: %d\r\n\r\n%s", len(resp), resp)
	}
}

func newDirectStore(t *testing.T, root string) *store {
	t.Helper()
	s := project.New(fs.NewDirect(root))
	if err := s.Refresh(); err != nil {
		t.Fatal(err)
	}
	return s
}
