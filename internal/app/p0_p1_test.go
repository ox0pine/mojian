package app

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
)

func TestParseTerminalLink(t *testing.T) {
	cases := []struct {
		raw    string
		path   string
		line   int
		col    int
		isFile bool
	}{
		{"https://github.com/ox0pine/mojian", "", 0, 0, false},
		{"mailto:hi@example.com", "", 0, 0, false},
		{"internal/app/lsp.go:42:10", "internal/app/lsp.go", 42, 10, true},
		{"internal/app/lsp.go:42:", "internal/app/lsp.go", 42, 0, true},
		{"src/App.tsx(12,4)", "src/App.tsx", 12, 4, true},
		{"README.md#L8C2", "README.md", 8, 2, true},
		{"file:///tmp/demo/main.go#L15", "/tmp/demo/main.go", 15, 0, true},
		{"./notes.md", "./notes.md", 0, 0, true},
	}
	for _, tc := range cases {
		p, l, c, ok := parseTerminalLink(tc.raw)
		if ok != tc.isFile || p != tc.path || l != tc.line || c != tc.col {
			t.Errorf("parseTerminalLink(%q) = (%q, %d, %d, %v), want (%q, %d, %d, %v)",
				tc.raw, p, l, c, ok, tc.path, tc.line, tc.col, tc.isFile)
		}
	}
}

func TestOpenTerminalLinkSplitsAndJumps(t *testing.T) {
	registerFonts()
	a := newProjectApp(t)
	if err := os.MkdirAll(filepath.Join(a.store.Dir, "pkg"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(a.store.Dir, "pkg", "engine.go"),
		[]byte("package pkg\n\nfunc Run() {\n\tprintln(1)\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := a.store.Refresh(); err != nil {
		t.Fatal(err)
	}

	// Put a single terminal pane in the active tab.
	tt := &termTab{Spec: termSpec{Server: localServerID}}
	tp := newTermPane(tt)
	tb := &tab{ID: nextID("t"), Root: &node{Pane: tp}, Focus: tp}
	a.tabs = []*tab{tb}
	a.active = 0

	if !a.openTerminalLink(tt, "pkg/engine.go:4:2") {
		t.Fatal("openTerminalLink returned false for valid project file")
	}
	if got := len(tb.panes()); got != 2 {
		t.Fatalf("expected terminal + split editor pane (2 panes), got %d", got)
	}
	p := a.focusPane()
	if p == nil || p.Note == nil || filepath.ToSlash(p.Note.Rel) != "pkg/engine.go" {
		t.Fatalf("expected focused editor on pkg/engine.go, got %+v", p)
	}
	if got := p.state.CursorPos().Line; got != 3 {
		t.Fatalf("cursor line = %d, want 3 (0-based for line 4)", got)
	}
	// Unique basename fallback also resolves cleanly.
	if !a.openTerminalLink(tt, "engine.go:3") {
		t.Fatal("expected unique basename link to resolve")
	}
	if got := a.focusPane().state.CursorPos().Line; got != 2 {
		t.Fatalf("cursor line after basename jump = %d, want 2", got)
	}
}

func TestFormatEditorReference(t *testing.T) {
	registerFonts()
	a := newProjectApp(t)
	src := "line1\nline2\nline3\nline4\n"
	if err := os.WriteFile(filepath.Join(a.store.Dir, "demo.go"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := a.store.Refresh(); err != nil {
		t.Fatal(err)
	}
	a.openRel("demo.go")
	p := a.focusPane()
	p.state = p.Note.NewState()
	p.state.GoTo(editor.Pos{Line: 1, Col: 0})
	ref, ok := a.formatEditorReference()
	if !ok || ref != "@demo.go:2 " {
		t.Fatalf("cursor ref = %q ok=%v, want %q", ref, ok, "@demo.go:2 ")
	}
	// Multi-line selection from line 2 to line 4.
	p.state.Select(len("line1\n"), len("line1\nline2\nline3\nline4"))
	ref, ok = a.formatEditorReference()
	if !ok || ref != "@demo.go:2-4 " {
		t.Fatalf("selection ref = %q ok=%v, want %q", ref, ok, "@demo.go:2-4 ")
	}
}

func TestNavHistoryBackAndForward(t *testing.T) {
	registerFonts()
	a := newProjectApp(t)
	if err := os.WriteFile(filepath.Join(a.store.Dir, "a.go"), []byte("package x\n\nfunc A() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(a.store.Dir, "b.go"), []byte("package x\n\n\n\nfunc B() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := a.store.Refresh(); err != nil {
		t.Fatal(err)
	}
	a.openNoteAt("a.go", 3, 0, 0)
	a.recordNav()
	a.openNoteAt("b.go", 5, 0, 0)

	if got := a.focusPane().Note.Rel; got != "b.go" {
		t.Fatalf("current file = %q, want b.go", got)
	}
	a.navBack()
	if got := a.focusPane().Note.Rel; got != "a.go" {
		t.Fatalf("after navBack file = %q, want a.go", got)
	}
	if got := a.focusPane().state.CursorPos().Line; got != 2 {
		t.Fatalf("after navBack line = %d, want 2", got)
	}
	a.navForward()
	if got := a.focusPane().Note.Rel; got != "b.go" {
		t.Fatalf("after navForward file = %q, want b.go", got)
	}
	if got := a.focusPane().state.CursorPos().Line; got != 4 {
		t.Fatalf("after navForward line = %d, want 4", got)
	}
}

func TestProblemsModalAndJumpDiagnostic(t *testing.T) {
	registerFonts()
	a := newProjectApp(t)
	src := "package main\n\nfunc main() {\n\tbad1()\n\tbad2()\n}\n"
	if err := os.WriteFile(filepath.Join(a.store.Dir, "main.go"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := a.store.Refresh(); err != nil {
		t.Fatal(err)
	}
	a.openRel("main.go")
	mgr := a.workspaceLSP(a.workspace)
	mgr.diags["main.go"] = lspDocDiag{
		diags: []editor.Diagnostic{
			{Start: len("package main\n\nfunc main() {\n\t"), End: len("package main\n\nfunc main() {\n\tbad1"), Severity: editor.SeverityWarning, Message: "warn one"},
			{Start: len("package main\n\nfunc main() {\n\tbad1()\n\t"), End: len("package main\n\nfunc main() {\n\tbad1()\n\tbad2"), Severity: editor.SeverityError, Message: "err two"},
		},
	}

	probs := a.collectWorkspaceProblems()
	if len(probs) != 2 {
		t.Fatalf("expected 2 problems, got %+v", probs)
	}
	if probs[0].Severity != editor.SeverityError || probs[0].Message != "err two" {
		t.Fatalf("expected error sorted first, got %+v", probs)
	}

	tt := ui.NewTester(a.view, 1100, 700)
	a.openProblems()
	tt.Frame()
	if !tt.HasText("err two") || !tt.HasText("warn one") {
		t.Fatalf("expected diagnostics in problems modal, texts=%v", tt.Texts())
	}
	a.problemsOpen = false
	tt.Frame()

	// Next / Prev diagnostic jumps to lines 4 and 5 (0-based 3 and 4).
	a.jumpDiagnostic(1)
	if got := a.focusPane().state.CursorPos().Line; got != 3 {
		t.Fatalf("first jumpDiagnostic(1) line = %d, want 3", got)
	}
	a.jumpDiagnostic(1)
	if got := a.focusPane().state.CursorPos().Line; got != 4 {
		t.Fatalf("second jumpDiagnostic(1) line = %d, want 4", got)
	}
	a.jumpDiagnostic(-1)
	if got := a.focusPane().state.CursorPos().Line; got != 3 {
		t.Fatalf("jumpDiagnostic(-1) line = %d, want 3", got)
	}
}

func TestWorkspaceSymbolsFallback(t *testing.T) {
	registerFonts()
	a := newProjectApp(t)
	if err := os.WriteFile(filepath.Join(a.store.Dir, "alpha.go"), []byte("package demo\n\nfunc AlphaHelper() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(a.store.Dir, "beta.go"), []byte("package demo\n\ntype BetaService struct{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := a.store.Refresh(); err != nil {
		t.Fatal(err)
	}
	tt := ui.NewTester(a.view, 1100, 700)
	a.openWorkspaceSymbols()
	a.wsSymbolsQuery = "Beta"
	tt.Frame()
	tt.Frame()
	if len(a.wsSymbolsItems) != 1 || a.wsSymbolsItems[0].Name != "BetaService" {
		t.Fatalf("workspace symbols = %+v, want BetaService", a.wsSymbolsItems)
	}
}
