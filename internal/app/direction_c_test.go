package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"mojian/internal/lsp"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
)

func TestInlayHintsAndGitBlameCoexist(t *testing.T) {
	registerFonts()
	a := newProjectApp(t)
	py := a.store.ByRelPath(filepath.Join("scripts", "tasks.py"))
	a.openNote(py)
	p := a.focusPane()
	p.state = py.NewState()

	// Seed both LSP inlay hints and an active trailing Git blame inlay.
	hints := []lsp.InlayHint{
		{Position: lsp.Position{Line: 0, Character: 5}, Label: json.RawMessage(`"count:"`), Kind: 2, PaddingRight: true},
		{Position: lsp.Position{Line: 0, Character: 10}, Label: json.RawMessage(`[{"value":": "},{"value":"int"}]`), Kind: 1, PaddingLeft: true},
	}
	p.inlayHints = lspInlayHintsToEditor(py.Text(), hints)
	blame := editor.Inlay{Offset: lineEndOffset(py.Text(), 0), Text: "   alice · fix", Trailing: true, Ellipsis: "…"}
	p.blameInlay = &blame
	p.blameText = "alice · fix"
	applyPaneInlays(p, p.state)

	// Toggle inlay hints off: LSP hints clear while blame inlay survives.
	a.toggleInlayHints()
	if !a.NoInlayHints {
		t.Fatal("expected NoInlayHints=true after toggle")
	}
	if len(p.inlayHints) != 0 {
		t.Fatalf("expected inlayHints cleared, got %d", len(p.inlayHints))
	}
	if p.blameInlay == nil || p.blameText != "alice · fix" {
		t.Fatal("expected Git blame inlay to survive toggling LSP inlay hints")
	}
}

func TestSignatureHelpContextAndOverlay(t *testing.T) {
	if !inCallContext("fmt.Printf(\"%s\", ", len("fmt.Printf(\"%s\", ")) {
		t.Fatal("expected inCallContext true inside open call")
	}
	if inCallContext("fmt.Printf(\"ok\")", len("fmt.Printf(\"ok\")")) {
		t.Fatal("expected inCallContext false after closed call")
	}
	if !inCallContext("fn(inner(1), ", len("fn(inner(1), ")) {
		t.Fatal("expected inCallContext true after nested call")
	}
	if !inCallContext("fn(\")\", ", len("fn(\")\", ")) {
		t.Fatal("expected inCallContext true when ')' is inside string")
	}

	registerFonts()
	a := newProjectApp(t)
	py := a.store.ByRelPath(filepath.Join("scripts", "tasks.py"))
	a.openNote(py)
	tt := ui.NewTester(a.view, 1100, 700)
	tt.Frame()

	p := a.focusPane()
	p.sigHelp = &lsp.SignatureHelp{
		Signatures: []lsp.SignatureInformation{{
			Label:         "render(name string, count int) error",
			Documentation: json.RawMessage(`"Render formats a template."`),
			Parameters: []lsp.ParameterInformation{
				{Label: json.RawMessage(`"name string"`), Documentation: json.RawMessage(`"Template name"`)},
				{Label: json.RawMessage(`"count int"`), Documentation: json.RawMessage(`"Repeat count"`)},
			},
		}},
		ActiveSignature: 0,
		ActiveParameter: 1,
	}
	tt.Frame()
	if !tt.HasText("Repeat count") {
		t.Fatalf("expected signature help parameter doc rendered, got texts: %q", tt.Texts())
	}
	a.dismissSignatureHelp(p)
	tt.Frame()
	if tt.HasText("Repeat count") {
		t.Fatal("expected signature help overlay dismissed")
	}
}

func TestCodeFoldingCommandsAndBreadcrumbs(t *testing.T) {
	registerFonts()
	dir := t.TempDir()
	goCode := `package demo

import (
	"fmt"
	"strings"
)

type Server struct {
	Host string
}

func (s *Server) Start() {
	fmt.Println(strings.ToUpper(s.Host))
}

func Helper() {
	fmt.Println("hi")
}
`
	if err := os.WriteFile(filepath.Join(dir, "server.go"), []byte(goCode), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := openStoreAt(dir)
	if err != nil {
		t.Fatal(err)
	}
	a := &app{workspace: &workspace{store: s, treeOpen: true}}
	a.focusFirstNote()

	tt := ui.NewTester(a.view, 1100, 700)
	tt.Frame()

	p := a.focusPane()
	if p == nil || p.state == nil {
		t.Fatal("expected focused editor pane")
	}

	// Move caret inside Start() and verify pane header breadcrumb updates live.
	p.state.GoTo(editor.Pos{Line: 12, Col: 2})
	tt.Frame()
	if p.symCrumb != "Start" {
		t.Fatalf("breadcrumb at line 12 = %q, want %q", p.symCrumb, "Start")
	}
	if !tt.HasText("Start") {
		t.Fatalf("expected breadcrumb 'Start' rendered in pane header")
	}

	// Verify hierarchical LSP EndLine enclosing match ("Server › Start").
	hier := []lsp.Symbol{
		{Name: "Server", Kind: 23, Line: 7, EndLine: 14},
		{Name: "Start", Container: "Server", Kind: 6, Line: 11, EndLine: 13},
	}
	if got := enclosingSymbolLabel(hier, 12); got != "Server › Start" {
		t.Fatalf("enclosingSymbolLabel = %q, want %q", got, "Server › Start")
	}

	// Test FoldImports, FoldAll, UnfoldAll via app commands.
	a.editorFoldImports()
	tt.Frame()
	if len(p.state.Folded()) == 0 {
		t.Fatal("expected FoldImports to fold the import block")
	}
	a.editorUnfoldAll()
	tt.Frame()
	if len(p.state.Folded()) != 0 {
		t.Fatalf("expected UnfoldAll to clear folds, got %v", p.state.Folded())
	}
	a.editorFoldAll()
	tt.Frame()
	if len(p.state.Folded()) == 0 {
		t.Fatal("expected FoldAll to fold code blocks")
	}
	a.editorUnfoldAll()
	tt.Frame()
	if len(p.state.Folded()) != 0 {
		t.Fatalf("expected UnfoldAll to unfold all blocks, got %v", p.state.Folded())
	}
}
