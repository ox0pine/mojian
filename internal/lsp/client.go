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

	onDiags func(PublishDiagnosticsParams)
	ready   atomic.Bool
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
	// Answer common capability/config requests so servers never block.
	var result any
	switch msg.Method {
	case "workspace/configuration":
		var params struct {
			Items []json.RawMessage `json:"items"`
		}
		_ = json.Unmarshal(msg.Params, &params)
		res := make([]any, len(params.Items))
		result = res
	default:
		result = nil
	}
	raw, _ := json.Marshal(result)
	_ = c.writeMsg(rpcMessage{JSONRPC: "2.0", ID: msg.ID, Result: raw})
}

func (c *Client) handleNotification(msg rpcMessage) {
	if msg.Method != "textDocument/publishDiagnostics" || c.onDiags == nil {
		return
	}
	var params PublishDiagnosticsParams
	if json.Unmarshal(msg.Params, &params) == nil {
		c.onDiags(params)
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
func (c *Client) Initialize(ctx context.Context, rootURI string) error {
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
					},
				},
				"hover": map[string]any{
					"contentFormat": []string{"plaintext", "markdown"},
				},
				"definition": map[string]any{
					"linkSupport": true,
				},
				"documentSymbol": map[string]any{
					"hierarchicalDocumentSymbolSupport": true,
				},
				"publishDiagnostics": map[string]any{},
			},
		},
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
	raw, err := c.call(ctx, "textDocument/completion", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     pos,
	})
	if err != nil || len(raw) == 0 || string(raw) == "null" {
		return nil, err
	}
	var items []completionItem
	var list completionList
	if json.Unmarshal(raw, &list) == nil && len(list.Items) > 0 {
		items = list.Items
	} else {
		_ = json.Unmarshal(raw, &items)
	}
	if len(items) > 120 {
		items = items[:120]
	}
	out := make([]editor.Completion, 0, len(items))
	for _, it := range items {
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
		out = append(out, comp)
	}
	return out, nil
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

// Definition queries textDocument/definition and returns the first target location.
func (c *Client) Definition(ctx context.Context, uri string, pos Position) (Location, bool, error) {
	raw, err := c.call(ctx, "textDocument/definition", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     pos,
	})
	if err != nil || len(raw) == 0 || string(raw) == "null" {
		return Location{}, false, err
	}
	var single Location
	if json.Unmarshal(raw, &single) == nil && single.URI != "" {
		return single, true, nil
	}
	var locs []Location
	if json.Unmarshal(raw, &locs) == nil && len(locs) > 0 && locs[0].URI != "" {
		return locs[0], true, nil
	}
	var links []LocationLink
	if json.Unmarshal(raw, &links) == nil && len(links) > 0 && links[0].TargetURI != "" {
		r := links[0].TargetSelectionRange
		if r == (Range{}) {
			r = links[0].TargetRange
		}
		return Location{URI: links[0].TargetURI, Range: r}, true, nil
	}
	return Location{}, false, nil
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
					Name:   s.Name,
					Detail: detail,
					Kind:   s.Kind,
					Line:   r.Start.Line,
					Col:    col,
				})
				if len(s.Children) > 0 {
					walk(s.Name, s.Children)
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
