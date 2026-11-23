package lsp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"testing"
	"time"
)

type pipePair struct {
	r *io.PipeReader
	w *io.PipeWriter
}

func (p *pipePair) Read(b []byte) (int, error)  { return p.r.Read(b) }
func (p *pipePair) Write(b []byte) (int, error) { return p.w.Write(b) }
func (p *pipePair) Close() error {
	_ = p.r.Close()
	return p.w.Close()
}

func TestUTF16PositionConversions(t *testing.T) {
	// 😀 is 4 UTF-8 bytes and 2 UTF-16 code units.
	text := "a😀b\n你好x"
	pos := OffsetToPosition(text, len("a😀"))
	if pos != (Position{Line: 0, Character: 3}) {
		t.Fatalf("OffsetToPosition = %+v, want {0 3}", pos)
	}
	if off := PositionToOffset(text, pos); off != len("a😀") {
		t.Fatalf("PositionToOffset = %d, want %d", off, len("a😀"))
	}
	pos2 := OffsetToPosition(text, len("a😀b\n你"))
	if pos2 != (Position{Line: 1, Character: 1}) {
		t.Fatalf("OffsetToPosition line 1 = %+v, want {1 1}", pos2)
	}
	if off := PositionToOffset(text, pos2); off != len("a😀b\n你") {
		t.Fatalf("PositionToOffset line 1 = %d, want %d", off, len("a😀b\n你"))
	}
}

func TestClientInitializeRequestsAndDiagnostics(t *testing.T) {
	c2sR, c2sW := io.Pipe()
	s2cR, s2cW := io.Pipe()
	clientSide := &pipePair{r: s2cR, w: c2sW}
	serverSide := &pipePair{r: c2sR, w: s2cW}
	defer serverSide.Close()

	writeServer := func(v any) {
		b, _ := json.Marshal(v)
		fmt.Fprintf(serverSide, "Content-Length: %d\r\n\r\n%s", len(b), b)
	}

	go func() {
		br := bufio.NewReader(serverSide)
		for {
			body, err := readFrame(br)
			if err != nil {
				return
			}
			var msg rpcMessage
			if json.Unmarshal(body, &msg) != nil {
				continue
			}
			switch msg.Method {
			case "initialize":
				writeServer(rpcMessage{JSONRPC: "2.0", ID: msg.ID, Result: json.RawMessage(`{"capabilities":{}}`)})
			case "textDocument/didOpen":
				writeServer(rpcMessage{
					JSONRPC: "2.0",
					Method:  "textDocument/publishDiagnostics",
					Params:  json.RawMessage(`{"uri":"file:///proj/main.go","diagnostics":[{"range":{"start":{"line":0,"character":0},"end":{"line":0,"character":3}},"severity":1,"source":"gopls","message":"undeclared name"}]}`),
				})
			case "textDocument/completion":
				writeServer(rpcMessage{
					JSONRPC: "2.0",
					ID:      msg.ID,
					Result:  json.RawMessage(`{"items":[{"label":"Println","kind":3,"detail":"func(a ...any)"}]}`),
				})
			case "textDocument/hover":
				writeServer(rpcMessage{
					JSONRPC: "2.0",
					ID:      msg.ID,
					Result:  json.RawMessage(`{"contents":{"kind":"markdown","value":"func Println(a ...any)"}}`),
				})
			case "textDocument/definition":
				writeServer(rpcMessage{
					JSONRPC: "2.0",
					ID:      msg.ID,
					Result:  json.RawMessage(`[{"uri":"file:///proj/util.go","range":{"start":{"line":4,"character":5},"end":{"line":4,"character":12}}}]`),
				})
			case "textDocument/documentSymbol":
				writeServer(rpcMessage{
					JSONRPC: "2.0",
					ID:      msg.ID,
					Result:  json.RawMessage(`[{"name":"main","kind":12,"range":{"start":{"line":2,"character":0},"end":{"line":4,"character":1}},"selectionRange":{"start":{"line":2,"character":5},"end":{"line":2,"character":9}}}]`),
				})
			case "textDocument/references":
				writeServer(rpcMessage{
					JSONRPC: "2.0",
					ID:      msg.ID,
					Result:  json.RawMessage(`[{"uri":"file:///proj/main.go","range":{"start":{"line":2,"character":5},"end":{"line":2,"character":9}}},{"uri":"file:///proj/util.go","range":{"start":{"line":1,"character":2},"end":{"line":1,"character":6}}}]`),
				})
			case "workspace/symbol":
				writeServer(rpcMessage{
					JSONRPC: "2.0",
					ID:      msg.ID,
					Result:  json.RawMessage(`[{"name":"Helper","kind":12,"containerName":"pkg","location":{"uri":"file:///proj/util.go","range":{"start":{"line":1,"character":0},"end":{"line":1,"character":10}}}}]`),
				})
			case "textDocument/rename":
				writeServer(rpcMessage{
					JSONRPC: "2.0",
					ID:      msg.ID,
					Result:  json.RawMessage(`{"changes":{"file:///proj/main.go":[{"range":{"start":{"line":2,"character":5},"end":{"line":2,"character":9}},"newText":"run"}]}}`),
				})
			case "textDocument/formatting":
				writeServer(rpcMessage{
					JSONRPC: "2.0",
					ID:      msg.ID,
					Result:  json.RawMessage(`[{"range":{"start":{"line":0,"character":0},"end":{"line":0,"character":7}},"newText":"package"}]`),
				})
			case "textDocument/codeAction":
				writeServer(rpcMessage{
					JSONRPC: "2.0",
					ID:      msg.ID,
					Result:  json.RawMessage(`[{"title":"Organize Imports","kind":"source.organizeImports","edit":{"changes":{"file:///proj/main.go":[{"range":{"start":{"line":0,"character":0},"end":{"line":0,"character":0}},"newText":"// ok\n"}]}}},{"title":"Run fix","command":"gopls.fix","arguments":["arg1"]}]`),
				})
			case "workspace/executeCommand":
				writeServer(rpcMessage{
					JSONRPC: "2.0",
					ID:      msg.ID,
					Result:  json.RawMessage(`null`),
				})
			case "textDocument/documentHighlight":
				writeServer(rpcMessage{
					JSONRPC: "2.0",
					ID:      msg.ID,
					Result:  json.RawMessage(`[{"range":{"start":{"line":2,"character":5},"end":{"line":2,"character":9}},"kind":1}]`),
				})
			}
		}
	}()

	diagsCh := make(chan PublishDiagnosticsParams, 1)
	cl := NewClient(clientSide, func(p PublishDiagnosticsParams) {
		diagsCh <- p
	})
	defer cl.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	if err := cl.Initialize(ctx, FileURI("/proj")); err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	if !cl.Ready() {
		t.Fatal("expected Ready after Initialize")
	}
	uri := FileURI("/proj/main.go")
	if err := cl.DidOpen(uri, "go", 1, "package main\n\nfunc main() {}\n"); err != nil {
		t.Fatalf("DidOpen: %v", err)
	}

	select {
	case d := <-diagsCh:
		if len(d.Diagnostics) != 1 || d.Diagnostics[0].Message != "undeclared name" {
			t.Fatalf("unexpected diagnostics: %+v", d)
		}
	case <-ctx.Done():
		t.Fatal("timed out waiting for publishDiagnostics")
	}

	comps, err := cl.Completion(ctx, uri, Position{Line: 2, Character: 4})
	if err != nil || len(comps) != 1 || comps[0].Label != "Println" {
		t.Fatalf("Completion = %+v, err=%v", comps, err)
	}
	hover, _, err := cl.Hover(ctx, uri, Position{Line: 2, Character: 4})
	if err != nil || hover != "func Println(a ...any)" {
		t.Fatalf("Hover = %q, err=%v", hover, err)
	}
	loc, ok, err := cl.Definition(ctx, uri, Position{Line: 2, Character: 4})
	if err != nil || !ok || loc.Range.Start.Line != 4 {
		t.Fatalf("Definition = %+v ok=%v err=%v", loc, ok, err)
	}
	syms, err := cl.DocumentSymbols(ctx, uri, "package main\n\nfunc main() {}\n")
	if err != nil || len(syms) != 1 || syms[0].Name != "main" || syms[0].Line != 2 {
		t.Fatalf("DocumentSymbols = %+v err=%v", syms, err)
	}
	refs, err := cl.References(ctx, uri, Position{Line: 2, Character: 5})
	if err != nil || len(refs) != 2 {
		t.Fatalf("References = %+v err=%v", refs, err)
	}
	wsSyms, err := cl.WorkspaceSymbols(ctx, "Help")
	if err != nil || len(wsSyms) != 1 || wsSyms[0].Name != "Helper" {
		t.Fatalf("WorkspaceSymbols = %+v err=%v", wsSyms, err)
	}
	changes, err := cl.Rename(ctx, uri, Position{Line: 2, Character: 5}, "run")
	if err != nil || len(changes[uri]) != 1 || changes[uri][0].NewText != "run" {
		t.Fatalf("Rename = %+v err=%v", changes, err)
	}
	fmtEdits, err := cl.Formatting(ctx, uri, 4, false)
	if err != nil || len(fmtEdits) != 1 {
		t.Fatalf("Formatting = %+v err=%v", fmtEdits, err)
	}
	actions, err := cl.CodeActions(ctx, uri, Range{}, nil)
	if err != nil || len(actions) != 2 || actions[0].Title != "Organize Imports" || actions[1].Command == nil || actions[1].Command.Command != "gopls.fix" {
		t.Fatalf("CodeActions = %+v err=%v", actions, err)
	}
	if err := cl.ExecuteCommand(ctx, actions[1].Command.Command, actions[1].Command.Arguments); err != nil {
		t.Fatalf("ExecuteCommand: %v", err)
	}
	hls, err := cl.DocumentHighlight(ctx, uri, Position{Line: 2, Character: 5})
	if err != nil || len(hls) != 1 || hls[0].Range.Start.Line != 2 {
		t.Fatalf("DocumentHighlight = %+v err=%v", hls, err)
	}
}

func TestApplyTextEditsAndLanguages(t *testing.T) {
	src := "alpha beta gamma\n"
	edits := []TextEdit{
		{Range: Range{Start: Position{Line: 0, Character: 0}, End: Position{Line: 0, Character: 5}}, NewText: "ALPHA"},
		{Range: Range{Start: Position{Line: 0, Character: 11}, End: Position{Line: 0, Character: 16}}, NewText: "GAMMA"},
	}
	if got := ApplyTextEdits(src, edits); got != "ALPHA beta GAMMA\n" {
		t.Fatalf("ApplyTextEdits = %q", got)
	}
	for ext, want := range map[string]string{
		"main.zig": "zig", "init.lua": "lua", "ci.yml": "yaml", "app.swift": "swift", "run.sh": "shellscript",
	} {
		if got := LanguageForPath(ext); got != want {
			t.Fatalf("LanguageForPath(%q) = %q, want %q", ext, got, want)
		}
	}
}

func TestFallbackSymbols(t *testing.T) {
	goSrc := "package p\n\ntype Store struct{}\nfunc (s *Store) Open() {}\nfunc Helper() {}\n"
	syms := FallbackSymbols("store.go", goSrc)
	if len(syms) != 3 || syms[0].Name != "Store" || syms[1].Name != "Open" || syms[2].Name != "Helper" {
		t.Fatalf("go symbols = %+v", syms)
	}
	mdSrc := "# Title\n## Section\nBody\n"
	mdSyms := FallbackSymbols("README.md", mdSrc)
	if len(mdSyms) != 2 || mdSyms[0].Name != "Title" || mdSyms[1].Name != "Section" {
		t.Fatalf("md symbols = %+v", mdSyms)
	}
}
