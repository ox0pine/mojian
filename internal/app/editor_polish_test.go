package app

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"mojian/internal/lsp"
	"mojian/internal/prefs"
	"mojian/internal/session"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
)

func TestEditorGlassHooksWhitespaceWrapGuidesAndMultiCursor(t *testing.T) {
	registerFonts()
	a := newProjectApp(t)
	if err := os.WriteFile(filepath.Join(a.store.Dir, "sample.go"), []byte("package main\n\nfunc main() {\n\tfoo := 1\n\tbar := foo + foo\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := a.store.Refresh(); err != nil {
		t.Fatal(err)
	}
	a.EditorWhitespace = 4 // WhitespaceAll
	a.EditorWrapGuides = "80, 100"
	a.openRel("sample.go")

	tt := ui.NewTester(a.view, 1100, 700)
	tt.Frame()

	p := a.focusPane()
	if p == nil || p.state == nil {
		t.Fatal("expected focused editor pane")
	}
	st := p.state
	if st.RenderFindBar == nil || st.RenderGoToLine == nil || st.RenderCompletions == nil || st.RenderHover == nil {
		t.Fatalf("expected all 4 Liquid Glass render hooks wired, got find=%v goto=%v comp=%v hover=%v",
			st.RenderFindBar != nil, st.RenderGoToLine != nil, st.RenderCompletions != nil, st.RenderHover != nil)
	}
	if st.Whitespace != editor.WhitespaceAll {
		t.Fatalf("st.Whitespace = %v, want WhitespaceAll", st.Whitespace)
	}
	if !slices.Equal(st.WrapGuides, []int{80, 100}) {
		t.Fatalf("st.WrapGuides = %v, want [80 100]", st.WrapGuides)
	}

	// Select first "foo" and invoke select-next-match via command registry.
	text := p.Note.Text()
	idx := len("package main\n\nfunc main() {\n\t")
	st.Select(idx, idx+3)
	if text[idx:idx+3] != "foo" {
		t.Fatalf("unexpected slice %q", text[idx:idx+3])
	}
	if !a.commandRegistry().Run("select-next-match") {
		t.Fatal("select-next-match not bound")
	}
	if !a.commandRegistry().Run("select-all-matches") {
		t.Fatal("select-all-matches not bound")
	}
	for _, id := range []string{"add-cursor-above", "add-cursor-below", "split-selection-lines", "expand-selection", "shrink-selection"} {
		if !a.commandRegistry().Run(id) {
			t.Fatalf("command %q not bound", id)
		}
	}
}

func TestApplyTargetedLSPEditsAutoImportUndoable(t *testing.T) {
	registerFonts()
	a := newProjectApp(t)
	src := "package main\n\nfunc main() {\n\tprintln(Now())\n}\n"
	if err := os.WriteFile(filepath.Join(a.store.Dir, "imp.go"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := a.store.Refresh(); err != nil {
		t.Fatal(err)
	}
	n := a.store.ByRelPath("imp.go")
	a.openNote(n)
	tt := ui.NewTester(a.view, 1100, 700)
	tt.Frame()

	p := a.focusPane()
	p.state.GoTo(editor.Pos{Line: 3, Col: 14})
	beforeCursor := p.state.Cursor()

	applyTargetedLSPEdits(n, []lsp.TextEdit{
		{
			Range:   lsp.Range{Start: lsp.Position{Line: 1, Character: 0}, End: lsp.Position{Line: 1, Character: 0}},
			NewText: "\nimport \"time\"\n",
		},
	})
	want := "package main\n\nimport \"time\"\n\nfunc main() {\n\tprintln(Now())\n}\n"
	if got := n.Text(); got != want {
		t.Fatalf("after auto-import = %q, want %q", got, want)
	}
	if !n.Buffer().CanUndo() {
		t.Fatal("expected auto-import edit to be undoable")
	}
	if gotCursor := p.state.Cursor(); gotCursor <= beforeCursor {
		t.Fatalf("expected cursor shifted forward past inserted import: before=%d after=%d", beforeCursor, gotCursor)
	}
}

func TestLSPServersSettingsRoundTripAndStatusModal(t *testing.T) {
	registerFonts()
	a := newProjectApp(t)
	a.settingsPath = filepath.Join(t.TempDir(), "settings.json")
	a.lspServers = map[string]prefs.LSPServerOverride{
		"go": {
			Command:               []string{"custom-gopls"},
			InitializationOptions: map[string]any{"staticcheck": true},
		},
		"python": {Disabled: true},
	}
	a.saveSettings()

	b := &app{workspace: &workspace{}, settingsPath: a.settingsPath, Prefs: prefs.Default()}
	b.loadSettings()
	if len(b.lspServers) != 2 || !b.lspServers["python"].Disabled {
		t.Fatalf("loaded lspServers = %+v", b.lspServers)
	}
	if !slices.Equal(b.lspServers["go"].Command, []string{"custom-gopls"}) {
		t.Fatalf("loaded go command = %v", b.lspServers["go"].Command)
	}

	// Verify LSP Status & Logs modal renders server status and captured logs.
	mgr := a.workspaceLSP(a.workspace)
	mgr.servers["go"] = &lspServerState{
		group:   "go",
		name:    "gopls",
		command: []string{"gopls"},
		failed:  true,
		errMsg:  "exit status 1",
		logs:    []string{"[stderr] missing go.mod"},
	}
	tt := ui.NewTester(a.view, 1100, 700)
	a.openLSPStatus()
	tt.Frame()

	if !tt.HasText("Language Servers") || !tt.HasText("gopls") || !tt.HasText("[stderr] missing go.mod") {
		t.Fatalf("expected LSP status and logs in modal, texts=%v", tt.Texts())
	}
}

func TestSearchWholeWordGroupedSingleReplaceAndPane(t *testing.T) {
	registerFonts()
	a := newProjectApp(t)
	if err := os.WriteFile(filepath.Join(a.store.Dir, "one.go"), []byte("package p\n\nvar foo = foobar + foo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(a.store.Dir, "two.go"), []byte("package p\n\nvar foo = 2\nvar x = foo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := a.store.Refresh(); err != nil {
		t.Fatal(err)
	}

	tt := ui.NewTester(a.view, 1100, 700)
	a.openSearch()
	a.searchQuery = "foo"
	a.searchWholeWord = true
	a.searchReplace = "qux"
	a.searchReplaceOpen = true
	tt.Frame()
	tt.Frame()

	// Whole-word finds 1 matching line in one.go and 2 matching lines in two.go.
	if len(a.searchResult.Matches) != 3 {
		t.Fatalf("wholeWord matches = %+v, want 3", a.searchResult.Matches)
	}

	// Grouped rows include 2 headers + 3 matches = 5 rows; collapsing one.go hides its match.
	rows := buildSearchRows(a.searchResult.Matches, nil)
	if len(rows) != 5 || !rows[0].isHeader || rows[0].rel != "one.go" || rows[0].count != 1 {
		t.Fatalf("grouped rows = %+v", rows)
	}
	collapsedRows := buildSearchRows(a.searchResult.Matches, map[string]bool{"one.go": true})
	if len(collapsedRows) != 4 {
		t.Fatalf("collapsed rows len = %d, want 4", len(collapsedRows))
	}

	// Single-match replace edits only the first match in two.go in memory.
	st := a.syncModalSearchIn()
	var twoLine3 session.SearchMatch
	for _, m := range st.result.Matches {
		if m.Rel == "two.go" && m.Line == 3 {
			twoLine3 = m
			break
		}
	}
	a.replaceSingleMatchInState(st, twoLine3)
	a.syncModalSearchOut()
	two := a.store.ByRelPath("two.go")
	if got := two.Text(); got != "package p\n\nvar qux = 2\nvar x = foo\n" {
		t.Fatalf("two.go after single replace = %q", got)
	}
	if !two.Dirty() {
		t.Fatal("expected two.go dirty in memory")
	}

	// Per-file replace replaces remaining match in two.go without touching one.go.
	a.replaceFileInState(st, "two.go")
	a.syncModalSearchOut()
	if got := two.Text(); got != "package p\n\nvar qux = 2\nvar x = qux\n" {
		t.Fatalf("two.go after file replace = %q", got)
	}
	if a.store.ByRelPath("one.go").Dirty() {
		t.Fatal("one.go should remain clean after Replace File on two.go")
	}

	// Open in Pane creates a Search pane that is omitted from snapshotLayout().
	a.openSearchInPaneFromModal()
	tt.Frame()
	if a.searchOpen {
		t.Fatal("expected search modal closed after Open in Pane")
	}
	var sp *pane
	for _, p := range a.tab().panes() {
		if p.Search != nil {
			sp = p
			break
		}
	}
	if sp == nil {
		t.Fatal("expected Search pane in active tab")
	}
	snap := a.snapshotLayout()
	for _, lt := range snap.Tabs {
		if lt.Root != nil && lt.Root.Pane != nil && lt.Root.Pane.ID == sp.ID {
			t.Fatal("expected Search pane omitted from snapshotLayout")
		}
	}

	// Clicking/jumping a match from the Search pane keeps the Search pane open and focuses sibling editor.
	a.openSearchMatchFromPane(sp, noteMatch(a.store.ByRelPath("one.go"), 3, 4, 7))
	tt.Frame()
	if a.focusPane() == sp {
		t.Fatal("expected sibling editor pane focused after opening match from Search pane")
	}
	if found := a.tab().contains(sp); !found {
		t.Fatal("expected Search pane to remain open in tab")
	}
}

func noteMatch(n *note, line, col, end int) session.SearchMatch {
	return session.SearchMatch{Rel: filepath.ToSlash(n.Rel), Line: line, Col: col, End: end}
}
