package lsp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/egoist/mygo/ui/editor"
)

type rpcMessage struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (e *rpcError) Error() string { return e.Message }

type pendingCall struct {
	ch chan rpcResult
}

type rpcResult struct {
	raw json.RawMessage
	err error
}

// Client speaks LSP JSON-RPC 2.0 over a stdio-like ReadWriteCloser.
type Client struct {
	rwc     io.ReadWriteCloser
	writeMu sync.Mutex

	mu      sync.Mutex
	nextID  int64
	pending map[int64]*pendingCall
	closed  chan struct{}
	closeFn sync.Once

	onDiags     func(PublishDiagnosticsParams)
	onLog       func(level int, msg string)
	onApplyEdit func(we WorkspaceEdit) bool
	initOpts    map[string]any
	ready       atomic.Bool
}

// NewClient wraps rwc as an LSP client and starts its reader goroutine.
func NewClient(rwc io.ReadWriteCloser, onDiags func(PublishDiagnosticsParams)) *Client {
	c := &Client{
		rwc:     rwc,
		pending: map[int64]*pendingCall{},
		closed:  make(chan struct{}),
		onDiags: onDiags,
	}
	go c.readLoop()
	return c
}

// SetLogHandler sets a callback invoked on window/logMessage and window/showMessage.
func (c *Client) SetLogHandler(fn func(level int, msg string)) {
	c.mu.Lock()
	c.onLog = fn
	c.mu.Unlock()
}

// SetApplyEditHandler sets a callback invoked when the server requests workspace/applyEdit.
func (c *Client) SetApplyEditHandler(fn func(we WorkspaceEdit) bool) {
	c.mu.Lock()
	c.onApplyEdit = fn
	c.mu.Unlock()
}

// Closed is closed when the underlying transport ends.
func (c *Client) Closed() <-chan struct{} { return c.closed }

// Ready reports whether Initialize has completed.
func (c *Client) Ready() bool { return c.ready.Load() }

// Close terminates the transport and wakes every pending call.
func (c *Client) Close() error {
	var err error
	c.closeFn.Do(func() {
		err = c.rwc.Close()
		c.failPending(io.EOF)
		close(c.closed)
	})
	return err
}

func (c *Client) failPending(err error) {
	c.mu.Lock()
	pending := c.pending
	c.pending = map[int64]*pendingCall{}
	c.mu.Unlock()
	for _, p := range pending {
		p.ch <- rpcResult{err: err}
	}
}

func (c *Client) writeMsg(v any) error {
	body, err := json.Marshal(v)
	if err != nil {
		return err
	}
	var buf bytes.Buffer
	fmt.Fprintf(&buf, "Content-Length: %d\r\n\r\n", len(body))
	buf.Write(body)
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	_, err = c.rwc.Write(buf.Bytes())
	return err
}

func (c *Client) call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	c.mu.Lock()
	select {
	case <-c.closed:
		c.mu.Unlock()
		return nil, errors.New("lsp: client closed")
	default:
	}
	c.nextID++
	id := c.nextID
	p := &pendingCall{ch: make(chan rpcResult, 1)}
	c.pending[id] = p
	c.mu.Unlock()

	rawID := json.RawMessage(strconv.FormatInt(id, 10))
	var rawParams json.RawMessage
	if params != nil {
		b, err := json.Marshal(params)
		if err != nil {
			c.deletePending(id)
			return nil, err
		}
		rawParams = b
	}
	if err := c.writeMsg(rpcMessage{JSONRPC: "2.0", ID: rawID, Method: method, Params: rawParams}); err != nil {
		c.deletePending(id)
		return nil, err
	}
	select {
	case res := <-p.ch:
		return res.raw, res.err
	case <-ctx.Done():
		c.deletePending(id)
		_ = c.notify("$/cancelRequest", map[string]any{"id": id})
		return nil, ctx.Err()
	case <-c.closed:
		c.deletePending(id)
		return nil, errors.New("lsp: client closed")
	}
}

func (c *Client) deletePending(id int64) {
	c.mu.Lock()
	delete(c.pending, id)
	c.mu.Unlock()
}

func (c *Client) notify(method string, params any) error {
	var rawParams json.RawMessage
	if params != nil {
		b, err := json.Marshal(params)
		if err != nil {
			return err
		}
		rawParams = b
	}
	return c.writeMsg(rpcMessage{JSONRPC: "2.0", Method: method, Params: rawParams})
}

func (c *Client) readLoop() {
	defer func() {
		_ = c.Close()
	}()
	r := bufio.NewReaderSize(c.rwc, 64<<10)
	for {
		body, err := readFrame(r)
		if err != nil {
			return
		}
		var msg rpcMessage
		if json.Unmarshal(body, &msg) != nil {
			continue
		}
		if msg.Method != "" {
			if len(msg.ID) > 0 && string(msg.ID) != "null" {
				c.handleServerRequest(msg)
			} else {
				c.handleNotification(msg)
			}
			continue
		}
		if len(msg.ID) == 0 {
			continue
		}
		id, err := strconv.ParseInt(strings.TrimSpace(string(msg.ID)), 10, 64)
		if err != nil {
			continue
		}
		c.mu.Lock()
		p := c.pending[id]
		delete(c.pending, id)
		c.mu.Unlock()
		if p != nil {
			if msg.Error != nil {
				p.ch <- rpcResult{err: msg.Error}
			} else {
				p.ch <- rpcResult{raw: msg.Result}
			}
		}
	}
}

func (c *Client) handleServerRequest(msg rpcMessage) {
	// Answer common capability/config/edit requests so servers never block.
	var result any
	switch msg.Method {
	case "workspace/configuration":
		var params struct {
			Items []struct {
				Section string `json:"section"`
			} `json:"items"`
		}
		_ = json.Unmarshal(msg.Params, &params)
		c.mu.Lock()
		opts := c.initOpts
		c.mu.Unlock()
		res := make([]any, len(params.Items))
		for i, item := range params.Items {
			if len(opts) == 0 {
				continue
			}
			if item.Section == "" {
				res[i] = opts
			} else if v, ok := opts[item.Section]; ok {
				res[i] = v
			}
		}
		result = res
	case "workspace/applyEdit":
		var params struct {
			Edit WorkspaceEdit `json:"edit"`
		}
		applied := false
		if json.Unmarshal(msg.Params, &params) == nil {
			c.mu.Lock()
			fn := c.onApplyEdit
			c.mu.Unlock()
			if fn != nil {
				applied = fn(params.Edit)
			}
		}
		result = map[string]any{"applied": applied}
	default:
		result = nil
	}
	raw, _ := json.Marshal(result)
	_ = c.writeMsg(rpcMessage{JSONRPC: "2.0", ID: msg.ID, Result: raw})
}

func (c *Client) handleNotification(msg rpcMessage) {
	switch msg.Method {
	case "textDocument/publishDiagnostics":
		if c.onDiags == nil {
			return
		}
		var params PublishDiagnosticsParams
		if json.Unmarshal(msg.Params, &params) == nil {
			c.onDiags(params)
		}
	case "window/logMessage", "window/showMessage":
		c.mu.Lock()
		fn := c.onLog
		c.mu.Unlock()
		if fn == nil {
			return
		}
		var params struct {
			Type    int    `json:"type"`
			Message string `json:"message"`
		}
		if json.Unmarshal(msg.Params, &params) == nil && params.Message != "" {
			fn(params.Type, params.Message)
		}
	}
}

func readFrame(r *bufio.Reader) ([]byte, error) {
	contentLen := -1
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return nil, err
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			break
		}
		k, v, ok := strings.Cut(line, ":")
		if ok && strings.EqualFold(strings.TrimSpace(k), "Content-Length") {
			if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil && n >= 0 {
				contentLen = n
			}
		}
	}
	if contentLen < 0 || contentLen > 32<<20 {
		return nil, errors.New("lsp: invalid Content-Length")
	}
	buf := make([]byte, contentLen)
	if _, err := io.ReadFull(r, buf); err != nil {
		return nil, err
	}
	return buf, nil
}

// Initialize performs the LSP initialize + initialized handshake for rootURI.
func (c *Client) Initialize(ctx context.Context, rootURI string, initOpts ...map[string]any) error {
	var opts map[string]any
	if len(initOpts) > 0 && len(initOpts[0]) > 0 {
		opts = initOpts[0]
		c.mu.Lock()
		c.initOpts = opts
		c.mu.Unlock()
	}
	params := map[string]any{
		"processId": nil,
		"rootUri":   rootURI,
		"workspaceFolders": []map[string]any{
			{"uri": rootURI, "name": "workspace"},
		},
		"capabilities": map[string]any{
			"textDocument": map[string]any{
				"synchronization": map[string]any{
					"didSave": true,
				},
				"completion": map[string]any{
					"completionItem": map[string]any{
						"snippetSupport":      true,
						"documentationFormat": []string{"plaintext", "markdown"},
						"resolveSupport": map[string]any{
							"properties": []string{"documentation", "detail", "additionalTextEdits"},
						},
					},
				},
				"hover": map[string]any{
					"contentFormat": []string{"plaintext", "markdown"},
				},
				"definition": map[string]any{
					"linkSupport": true,
				},
				"implementation": map[string]any{
					"linkSupport": true,
				},
				"typeDefinition": map[string]any{
					"linkSupport": true,
				},
				"references": map[string]any{},
				"rename":     map[string]any{},
				"formatting": map[string]any{},
				"codeAction": map[string]any{
					"codeActionLiteralSupport": map[string]any{
						"codeActionKind": map[string]any{
							"valueSet": []string{
								"", "quickfix", "refactor", "refactor.extract",
								"refactor.inline", "refactor.rewrite", "source", "source.organizeImports",
							},
						},
					},
				},
				"documentHighlight": map[string]any{},
				"inlayHint":         map[string]any{},
				"signatureHelp": map[string]any{
					"signatureInformation": map[string]any{
						"documentationFormat": []string{"plaintext", "markdown"},
						"parameterInformation": map[string]any{
							"labelOffsetSupport": true,
						},
						"activeParameterSupport": true,
					},
				},
				"documentSymbol": map[string]any{
					"hierarchicalDocumentSymbolSupport": true,
				},
				"publishDiagnostics": map[string]any{},
			},
			"workspace": map[string]any{
				"applyEdit": true,
				"symbol":    map[string]any{},
				"didChangeWatchedFiles": map[string]any{
					"dynamicRegistration": false,
				},
			},
		},
	}
	if len(opts) > 0 {
		params["initializationOptions"] = opts
	}
	if _, err := c.call(ctx, "initialize", params); err != nil {
		return err
	}
	if err := c.notify("initialized", map[string]any{}); err != nil {
		return err
	}
	c.ready.Store(true)
	return nil
}

// DidChangeWatchedFiles notifies the server of external/workspace file changes.
func (c *Client) DidChangeWatchedFiles(changes []FileEvent) error {
	if len(changes) == 0 {
		return nil
	}
	return c.notify("workspace/didChangeWatchedFiles", map[string]any{
		"changes": changes,
	})
}

func (c *Client) DidOpen(uri, languageID string, version int, text string) error {
	return c.notify("textDocument/didOpen", map[string]any{
		"textDocument": map[string]any{
			"uri":        uri,
			"languageId": languageID,
			"version":    version,
			"text":       text,
		},
	})
}

func (c *Client) DidChange(uri string, version int, text string) error {
	return c.notify("textDocument/didChange", map[string]any{
		"textDocument": map[string]any{
			"uri":     uri,
			"version": version,
		},
		"contentChanges": []map[string]any{
			{"text": text},
		},
	})
}

func (c *Client) DidSave(uri string) error {
	return c.notify("textDocument/didSave", map[string]any{
		"textDocument": map[string]any{"uri": uri},
	})
}

func (c *Client) DidClose(uri string) error {
	return c.notify("textDocument/didClose", map[string]any{
		"textDocument": map[string]any{"uri": uri},
	})
}

// Completion queries textDocument/completion and converts items to editor.Completion.
func (c *Client) Completion(ctx context.Context, uri string, pos Position) ([]editor.Completion, error) {
	detailed, err := c.CompletionDetailed(ctx, uri, pos)
	if err != nil || len(detailed) == 0 {
		return nil, err
	}
	out := make([]editor.Completion, len(detailed))
	for i, d := range detailed {
		out[i] = d.Item
	}
	return out, nil
}

// CompletionDetailed queries textDocument/completion and preserves raw items and additionalTextEdits.
func (c *Client) CompletionDetailed(ctx context.Context, uri string, pos Position) ([]CompletionResult, error) {
	raw, err := c.call(ctx, "textDocument/completion", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     pos,
	})
	if err != nil || len(raw) == 0 || string(raw) == "null" {
		return nil, err
	}
	var rawItems []json.RawMessage
	var list completionList
	if json.Unmarshal(raw, &list) == nil && len(list.Items) > 0 {
		rawItems = list.Items
	} else {
		_ = json.Unmarshal(raw, &rawItems)
	}
	if len(rawItems) > 120 {
		rawItems = rawItems[:120]
	}
	out := make([]CompletionResult, 0, len(rawItems))
	for _, itemRaw := range rawItems {
		res, ok := parseCompletionResult(itemRaw)
		if ok {
			out = append(out, res)
		}
	}
	return out, nil
}

// ResolveCompletion resolves a completion item via completionItem/resolve.
func (c *Client) ResolveCompletion(ctx context.Context, rawItem json.RawMessage) (CompletionResult, error) {
	if len(rawItem) == 0 {
		return CompletionResult{}, errors.New("lsp: empty completion item")
	}
	raw, err := c.call(ctx, "completionItem/resolve", rawItem)
	if err != nil {
		return CompletionResult{}, err
	}
	if len(raw) == 0 || string(raw) == "null" {
		return CompletionResult{}, nil
	}
	res, _ := parseCompletionResult(raw)
	return res, nil
}

func parseCompletionResult(raw json.RawMessage) (CompletionResult, bool) {
	var it completionItem
	if json.Unmarshal(raw, &it) != nil {
		return CompletionResult{}, false
	}
	ins := it.InsertText
	if it.TextEdit != nil && it.TextEdit.NewText != "" {
		ins = it.TextEdit.NewText
	}
	comp := editor.Completion{
		Label:         it.Label,
		Detail:        it.Detail,
		Kind:          mapCompletionKind(it.Kind),
		Insert:        ins,
		FilterText:    it.FilterText,
		Documentation: extractMarkup(it.Documentation),
	}
	if it.InsertTextFormat == 2 && ins != "" {
		if sn, err := editor.ParseSnippet(ins); err == nil && sn != nil {
			comp.Snippet = sn
		}
	}
	hasData := len(it.Data) > 0 && string(it.Data) != "null"
	return CompletionResult{
		Item:                comp,
		AdditionalTextEdits: it.AdditionalTextEdits,
		Raw:                 raw,
		HasData:             hasData,
	}, true
}

// Hover queries textDocument/hover and returns the rendered text and optional range.
func (c *Client) Hover(ctx context.Context, uri string, pos Position) (string, *Range, error) {
	raw, err := c.call(ctx, "textDocument/hover", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     pos,
	})
	if err != nil || len(raw) == 0 || string(raw) == "null" {
		return "", nil, err
	}
	var res hoverResult
	if err := json.Unmarshal(raw, &res); err != nil {
		return "", nil, nil
	}
	return extractMarkup(res.Contents), res.Range, nil
}

func (c *Client) queryLocations(ctx context.Context, method, uri string, pos Position) ([]Location, error) {
	raw, err := c.call(ctx, method, map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     pos,
	})
	if err != nil || len(raw) == 0 || string(raw) == "null" {
		return nil, err
	}
	var single Location
	if json.Unmarshal(raw, &single) == nil && single.URI != "" {
		return []Location{single}, nil
	}
	var locs []Location
	if json.Unmarshal(raw, &locs) == nil && len(locs) > 0 {
		out := make([]Location, 0, len(locs))
		for _, loc := range locs {
			if loc.URI != "" {
				out = append(out, loc)
			}
		}
		if len(out) > 0 {
			return out, nil
		}
	}
	var links []LocationLink
	if json.Unmarshal(raw, &links) == nil && len(links) > 0 {
		out := make([]Location, 0, len(links))
		for _, link := range links {
			if link.TargetURI == "" {
				continue
			}
			r := link.TargetSelectionRange
			if r == (Range{}) {
				r = link.TargetRange
			}
			out = append(out, Location{URI: link.TargetURI, Range: r})
		}
		if len(out) > 0 {
			return out, nil
		}
	}
	return nil, nil
}

// Definition queries textDocument/definition and returns the first target location.
func (c *Client) Definition(ctx context.Context, uri string, pos Position) (Location, bool, error) {
	locs, err := c.queryLocations(ctx, "textDocument/definition", uri, pos)
	if err != nil || len(locs) == 0 {
		return Location{}, false, err
	}
	return locs[0], true, nil
}

// Implementation queries textDocument/implementation and returns target locations.
func (c *Client) Implementation(ctx context.Context, uri string, pos Position) ([]Location, error) {
	return c.queryLocations(ctx, "textDocument/implementation", uri, pos)
}

// TypeDefinition queries textDocument/typeDefinition and returns target locations.
func (c *Client) TypeDefinition(ctx context.Context, uri string, pos Position) ([]Location, error) {
	return c.queryLocations(ctx, "textDocument/typeDefinition", uri, pos)
}

// DocumentSymbols queries textDocument/documentSymbol and returns flattened symbols.
func (c *Client) DocumentSymbols(ctx context.Context, uri, text string) ([]Symbol, error) {
	raw, err := c.call(ctx, "textDocument/documentSymbol", map[string]any{
		"textDocument": map[string]any{"uri": uri},
	})
	if err != nil || len(raw) == 0 || string(raw) == "null" {
		return nil, err
	}
	var docSyms []documentSymbol
	if json.Unmarshal(raw, &docSyms) == nil && len(docSyms) > 0 && docSyms[0].Name != "" && docSyms[0].SelectionRange != (Range{}) {
		var out []Symbol
		var walk func(prefix string, list []documentSymbol)
		walk = func(prefix string, list []documentSymbol) {
			for _, s := range list {
				detail := s.Detail
				if prefix != "" && detail == "" {
					detail = prefix
				}
				r := s.SelectionRange
				if r == (Range{}) {
					r = s.Range
				}
				off := PositionToOffset(text, r.Start)
				col := colInLine(text, off)
				out = append(out, Symbol{
					Name:      s.Name,
					Detail:    detail,
					Container: prefix,
					Kind:      s.Kind,
					Line:      r.Start.Line,
					EndLine:   s.Range.End.Line,
					Col:       col,
				})
				if len(s.Children) > 0 {
					nextPrefix := s.Name
					if prefix != "" {
						nextPrefix = prefix + " › " + s.Name
					}
					walk(nextPrefix, s.Children)
				}
			}
		}
		walk("", docSyms)
		return out, nil
	}
	var infos []symbolInformation
	if json.Unmarshal(raw, &infos) == nil && len(infos) > 0 {
		out := make([]Symbol, 0, len(infos))
		for _, info := range infos {
			off := PositionToOffset(text, info.Location.Range.Start)
			out = append(out, Symbol{
				Name:   info.Name,
				Detail: info.ContainerName,
				Kind:   info.Kind,
				Line:   info.Location.Range.Start.Line,
				Col:    colInLine(text, off),
			})
		}
		return out, nil
	}
	return nil, nil
}

func colInLine(text string, off int) int {
	if off < 0 || off > len(text) {
		return 0
	}
	nl := strings.LastIndexByte(text[:off], '\n')
	if nl < 0 {
		return off
	}
	return off - (nl + 1)
}

// References queries textDocument/references for the symbol at pos.
func (c *Client) References(ctx context.Context, uri string, pos Position) ([]Location, error) {
	raw, err := c.call(ctx, "textDocument/references", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     pos,
		"context":      map[string]any{"includeDeclaration": true},
	})
	if err != nil || len(raw) == 0 || string(raw) == "null" {
		return nil, err
	}
	var locs []Location
	if err := json.Unmarshal(raw, &locs); err != nil {
		return nil, nil
	}
	return locs, nil
}

// WorkspaceSymbols queries workspace/symbol across the project.
func (c *Client) WorkspaceSymbols(ctx context.Context, query string) ([]WorkspaceSymbol, error) {
	raw, err := c.call(ctx, "workspace/symbol", map[string]any{
		"query": query,
	})
	if err != nil || len(raw) == 0 || string(raw) == "null" {
		return nil, err
	}
	var infos []symbolInformation
	if err := json.Unmarshal(raw, &infos); err != nil {
		return nil, nil
	}
	out := make([]WorkspaceSymbol, 0, len(infos))
	for _, info := range infos {
		if info.Name == "" || info.Location.URI == "" {
			continue
		}
		out = append(out, WorkspaceSymbol{
			Name:   info.Name,
			Detail: info.ContainerName,
			Kind:   info.Kind,
			URI:    info.Location.URI,
			Line:   info.Location.Range.Start.Line,
			Col:    info.Location.Range.Start.Character,
		})
	}
	return out, nil
}

// Rename queries textDocument/rename and returns normalized per-URI text edits.
func (c *Client) Rename(ctx context.Context, uri string, pos Position, newName string) (map[string][]TextEdit, error) {
	raw, err := c.call(ctx, "textDocument/rename", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     pos,
		"newName":      newName,
	})
	if err != nil || len(raw) == 0 || string(raw) == "null" {
		return nil, err
	}
	var we WorkspaceEdit
	if err := json.Unmarshal(raw, &we); err != nil {
		return nil, err
	}
	return NormalizeWorkspaceEdit(we), nil
}

// Formatting queries textDocument/formatting for the whole document.
func (c *Client) Formatting(ctx context.Context, uri string, tabSize int, insertSpaces bool) ([]TextEdit, error) {
	if tabSize <= 0 {
		tabSize = 4
	}
	raw, err := c.call(ctx, "textDocument/formatting", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"options": map[string]any{
			"tabSize":      tabSize,
			"insertSpaces": insertSpaces,
		},
	})
	if err != nil || len(raw) == 0 || string(raw) == "null" {
		return nil, err
	}
	var edits []TextEdit
	if err := json.Unmarshal(raw, &edits); err != nil {
		return nil, nil
	}
	return edits, nil
}

// CodeActions queries textDocument/codeAction for rng and the overlapping diagnostics.
func (c *Client) CodeActions(ctx context.Context, uri string, rng Range, diags []Diagnostic) ([]CodeAction, error) {
	if diags == nil {
		diags = []Diagnostic{}
	}
	raw, err := c.call(ctx, "textDocument/codeAction", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"range":        rng,
		"context": map[string]any{
			"diagnostics": diags,
		},
	})
	if err != nil || len(raw) == 0 || string(raw) == "null" {
		return nil, err
	}
	var items []json.RawMessage
	if err := json.Unmarshal(raw, &items); err != nil {
		return nil, nil
	}
	out := make([]CodeAction, 0, len(items))
	for _, item := range items {
		var ca CodeAction
		if json.Unmarshal(item, &ca) == nil && ca.Title != "" && (ca.Edit != nil || ca.Command != nil || ca.Kind != "") {
			out = append(out, ca)
			continue
		}
		var cmd Command
		if json.Unmarshal(item, &cmd) == nil && cmd.Title != "" && cmd.Command != "" {
			out = append(out, CodeAction{Title: cmd.Title, Command: &cmd})
		}
	}
	return out, nil
}

// ExecuteCommand runs workspace/executeCommand for a command-backed CodeAction.
func (c *Client) ExecuteCommand(ctx context.Context, command string, args []json.RawMessage) error {
	params := map[string]any{"command": command}
	if len(args) > 0 {
		params["arguments"] = args
	}
	_, err := c.call(ctx, "workspace/executeCommand", params)
	return err
}

// DocumentHighlight queries textDocument/documentHighlight at pos.
func (c *Client) DocumentHighlight(ctx context.Context, uri string, pos Position) ([]DocumentHighlight, error) {
	raw, err := c.call(ctx, "textDocument/documentHighlight", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     pos,
	})
	if err != nil || len(raw) == 0 || string(raw) == "null" {
		return nil, err
	}
	var out []DocumentHighlight
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, nil
	}
	return out, nil
}

// InlayHints queries textDocument/inlayHint for rng.
func (c *Client) InlayHints(ctx context.Context, uri string, rng Range) ([]InlayHint, error) {
	raw, err := c.call(ctx, "textDocument/inlayHint", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"range":        rng,
	})
	if err != nil || len(raw) == 0 || string(raw) == "null" {
		return nil, err
	}
	var out []InlayHint
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, nil
	}
	return out, nil
}

// SignatureHelp queries textDocument/signatureHelp at pos.
func (c *Client) SignatureHelp(ctx context.Context, uri string, pos Position) (*SignatureHelp, error) {
	raw, err := c.call(ctx, "textDocument/signatureHelp", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     pos,
	})
	if err != nil || len(raw) == 0 || string(raw) == "null" {
		return nil, err
	}
	var out SignatureHelp
	if err := json.Unmarshal(raw, &out); err != nil || len(out.Signatures) == 0 {
		return nil, nil
	}
	return &out, nil
}
