// Package lsp implements a lightweight, zero-configuration Language Server
// Protocol client over JSON-RPC 2.0 stdio streams (local or remote).
package lsp

import (
	"encoding/json"
	"net/url"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/egoist/mygo/ui/editor"
)

// Position is a 0-based line and UTF-16 code-unit character offset.
type Position struct {
	Line      int `json:"line"`
	Character int `json:"character"`
}

// Range is a half-open span [Start, End).
type Range struct {
	Start Position `json:"start"`
	End   Position `json:"end"`
}

// Location is a file URI and range returned by definition requests.
type Location struct {
	URI   string `json:"uri"`
	Range Range  `json:"range"`
}

// LocationLink is the extended definition target some servers return.
type LocationLink struct {
	TargetURI            string `json:"targetUri"`
	TargetRange          Range  `json:"targetRange"`
	TargetSelectionRange Range  `json:"targetSelectionRange"`
}

// Diagnostic is an LSP textDocument/publishDiagnostics entry.
type Diagnostic struct {
	Range    Range  `json:"range"`
	Severity int    `json:"severity,omitempty"`
	Source   string `json:"source,omitempty"`
	Message  string `json:"message"`
}

// PublishDiagnosticsParams is the payload of textDocument/publishDiagnostics.
type PublishDiagnosticsParams struct {
	URI         string       `json:"uri"`
	Version     *int         `json:"version,omitempty"`
	Diagnostics []Diagnostic `json:"diagnostics"`
}

// Symbol is a flattened document symbol for Go to Symbol (⇧⌘O) and breadcrumbs.
type Symbol struct {
	Name      string
	Detail    string
	Container string
	Kind      int
	Line      int // 0-based start line
	EndLine   int // 0-based end line (0 if unknown)
	Col       int // 0-based byte offset within line
}

// InlayHintLabelPart is one segment of a structured LSP InlayHint label.
type InlayHintLabelPart struct {
	Value string `json:"value"`
}

// InlayHint is a type or parameter hint returned by textDocument/inlayHint.
type InlayHint struct {
	Position     Position        `json:"position"`
	Label        json.RawMessage `json:"label"`
	Kind         int             `json:"kind,omitempty"` // 1=Type, 2=Parameter
	PaddingLeft  bool            `json:"paddingLeft,omitempty"`
	PaddingRight bool            `json:"paddingRight,omitempty"`
}

// LabelText flattens an InlayHint's string or label-part array into plain text.
func (h InlayHint) LabelText() string {
	if len(h.Label) == 0 {
		return ""
	}
	var s string
	if json.Unmarshal(h.Label, &s) == nil {
		return s
	}
	var parts []InlayHintLabelPart
	if json.Unmarshal(h.Label, &parts) == nil {
		var b strings.Builder
		for _, p := range parts {
			b.WriteString(p.Value)
		}
		return b.String()
	}
	return ""
}

// ParameterInformation is one parameter inside a SignatureInformation.
type ParameterInformation struct {
	Label         json.RawMessage `json:"label"`
	Documentation json.RawMessage `json:"documentation,omitempty"`
}

// DocText returns the parameter's documentation as plain text.
func (p ParameterInformation) DocText() string {
	return extractMarkup(p.Documentation)
}

// SignatureInformation describes one callable overload.
type SignatureInformation struct {
	Label           string                 `json:"label"`
	Documentation   json.RawMessage        `json:"documentation,omitempty"`
	Parameters      []ParameterInformation `json:"parameters,omitempty"`
	ActiveParameter *int                   `json:"activeParameter,omitempty"`
}

// DocText returns the signature's documentation as plain text.
func (s SignatureInformation) DocText() string {
	return extractMarkup(s.Documentation)
}

// ActiveParamSpan resolves the byte [start, end) range of paramIdx inside s.Label.
func (s SignatureInformation) ActiveParamSpan(paramIdx int) (start, end int, ok bool) {
	if paramIdx < 0 || paramIdx >= len(s.Parameters) {
		return 0, 0, false
	}
	raw := s.Parameters[paramIdx].Label
	if len(raw) == 0 {
		return 0, 0, false
	}
	var offsets [2]int
	if json.Unmarshal(raw, &offsets) == nil && offsets[1] > offsets[0] {
		st := utf16OffsetInLine(s.Label, offsets[0])
		en := utf16OffsetInLine(s.Label, offsets[1])
		if en > st && en <= len(s.Label) {
			return st, en, true
		}
	}
	var sub string
	if json.Unmarshal(raw, &sub) == nil && sub != "" {
		if idx := strings.Index(s.Label, sub); idx >= 0 {
			return idx, idx + len(sub), true
		}
	}
	return 0, 0, false
}

func utf16OffsetInLine(s string, want int) int {
	if want <= 0 {
		return 0
	}
	cols := 0
	i := 0
	for i < len(s) && cols < want {
		r, sz := utf8.DecodeRuneInString(s[i:])
		cols += utf16.RuneLen(r)
		i += sz
	}
	return i
}

// SignatureHelp is the response of textDocument/signatureHelp.
type SignatureHelp struct {
	Signatures      []SignatureInformation `json:"signatures"`
	ActiveSignature int                    `json:"activeSignature"`
	ActiveParameter int                    `json:"activeParameter"`
}

// Active returns the currently active signature and its effective active parameter index.
func (sh *SignatureHelp) Active() (SignatureInformation, int, bool) {
	if sh == nil || len(sh.Signatures) == 0 {
		return SignatureInformation{}, 0, false
	}
	idx := sh.ActiveSignature
	if idx < 0 || idx >= len(sh.Signatures) {
		idx = 0
	}
	sig := sh.Signatures[idx]
	param := sh.ActiveParameter
	if sig.ActiveParameter != nil {
		param = *sig.ActiveParameter
	}
	return sig, param, true
}

// WorkspaceSymbol is a symbol returned by workspace/symbol.
type WorkspaceSymbol struct {
	Name   string
	Detail string
	Kind   int
	URI    string
	Line   int // 0-based
	Col    int // 0-based UTF-16 character offset
}

// TextEdit is a single range replacement returned by rename or formatting.
type TextEdit struct {
	Range   Range  `json:"range"`
	NewText string `json:"newText"`
}

// WorkspaceEdit describes edits across one or more documents.
type WorkspaceEdit struct {
	Changes         map[string][]TextEdit `json:"changes,omitempty"`
	DocumentChanges []json.RawMessage     `json:"documentChanges,omitempty"`
}

// Command is an LSP command attached to a CodeAction.
type Command struct {
	Title     string            `json:"title"`
	Command   string            `json:"command"`
	Arguments []json.RawMessage `json:"arguments,omitempty"`
}

// CodeAction is a quick fix or refactoring returned by textDocument/codeAction.
type CodeAction struct {
	Title       string         `json:"title"`
	Kind        string         `json:"kind,omitempty"`
	IsPreferred bool           `json:"isPreferred,omitempty"`
	Edit        *WorkspaceEdit `json:"edit,omitempty"`
	Command     *Command       `json:"command,omitempty"`
}

// DocumentHighlight is a same-symbol range returned by textDocument/documentHighlight.
type DocumentHighlight struct {
	Range Range `json:"range"`
	Kind  int   `json:"kind,omitempty"`
}

// LSP FileChangeType values for workspace/didChangeWatchedFiles.
const (
	FileCreated = 1
	FileChanged = 2
	FileDeleted = 3
)

// FileEvent describes a file creation, modification, or deletion for LSP.
type FileEvent struct {
	URI  string `json:"uri"`
	Type int    `json:"type"`
}

// CompletionResult pairs a converted editor completion with any LSP
// additionalTextEdits and raw payload needed for completionItem/resolve.
type CompletionResult struct {
	Item                editor.Completion
	AdditionalTextEdits []TextEdit
	Raw                 json.RawMessage
	HasData             bool
}

// NormalizeWorkspaceEdit extracts all per-URI text edits from changes and
// documentChanges into a single map.
func NormalizeWorkspaceEdit(we WorkspaceEdit) map[string][]TextEdit {
	out := map[string][]TextEdit{}
	for u, edits := range we.Changes {
		if len(edits) > 0 {
			out[u] = append(out[u], edits...)
		}
	}
	for _, dc := range we.DocumentChanges {
		var tde textDocumentEdit
		if json.Unmarshal(dc, &tde) == nil && tde.TextDocument.URI != "" && len(tde.Edits) > 0 {
			out[tde.TextDocument.URI] = append(out[tde.TextDocument.URI], tde.Edits...)
		}
	}
	return out
}

type textDocumentEdit struct {
	TextDocument struct {
		URI string `json:"uri"`
	} `json:"textDocument"`
	Edits []TextEdit `json:"edits"`
}

// ApplyTextEdits applies a set of LSP TextEdits to text and returns the updated content.
func ApplyTextEdits(text string, edits []TextEdit) string {
	if len(edits) == 0 {
		return text
	}
	type indexedEdit struct {
		idx        int
		start, end int
		newText    string
	}
	resolved := make([]indexedEdit, 0, len(edits))
	for i, e := range edits {
		s := PositionToOffset(text, e.Range.Start)
		end := PositionToOffset(text, e.Range.End)
		if end < s {
			end = s
		}
		resolved = append(resolved, indexedEdit{idx: i, start: s, end: end, newText: e.NewText})
	}
	sort.Slice(resolved, func(i, j int) bool {
		if resolved[i].start != resolved[j].start {
			return resolved[i].start > resolved[j].start
		}
		if resolved[i].end != resolved[j].end {
			return resolved[i].end > resolved[j].end
		}
		return resolved[i].idx > resolved[j].idx
	})
	out := text
	lastStart := len(out) + 1
	for _, e := range resolved {
		if e.end > lastStart {
			// Skip overlapping edit to keep offsets valid.
			continue
		}
		out = out[:e.start] + e.newText + out[e.end:]
		lastStart = e.start
	}
	return out
}

// SymbolKindName returns a short label for an LSP SymbolKind.
func SymbolKindName(kind int) string {
	switch kind {
	case 1:
		return "file"
	case 2:
		return "module"
	case 3:
		return "namespace"
	case 5:
		return "class"
	case 6:
		return "method"
	case 7:
		return "property"
	case 8:
		return "field"
	case 9:
		return "constructor"
	case 10:
		return "enum"
	case 11:
		return "interface"
	case 12:
		return "func"
	case 13:
		return "var"
	case 14:
		return "const"
	case 15:
		return "heading"
	case 23:
		return "struct"
	case 26:
		return "type"
	default:
		return "symbol"
	}
}

type completionItem struct {
	Label               string          `json:"label"`
	Kind                int             `json:"kind,omitempty"`
	Detail              string          `json:"detail,omitempty"`
	Documentation       json.RawMessage `json:"documentation,omitempty"`
	FilterText          string          `json:"filterText,omitempty"`
	InsertText          string          `json:"insertText,omitempty"`
	InsertTextFormat    int             `json:"insertTextFormat,omitempty"`
	AdditionalTextEdits []TextEdit      `json:"additionalTextEdits,omitempty"`
	Data                json.RawMessage `json:"data,omitempty"`
	TextEdit            *struct {
		NewText string `json:"newText"`
	} `json:"textEdit,omitempty"`
}

type completionList struct {
	Items []json.RawMessage `json:"items"`
}

type hoverResult struct {
	Contents json.RawMessage `json:"contents"`
	Range    *Range          `json:"range,omitempty"`
}

type documentSymbol struct {
	Name           string           `json:"name"`
	Detail         string           `json:"detail,omitempty"`
	Kind           int              `json:"kind"`
	Range          Range            `json:"range"`
	SelectionRange Range            `json:"selectionRange"`
	Children       []documentSymbol `json:"children,omitempty"`
}

type symbolInformation struct {
	Name          string   `json:"name"`
	Kind          int      `json:"kind"`
	ContainerName string   `json:"containerName,omitempty"`
	Location      Location `json:"location"`
}

// FileURI turns an absolute POSIX or Windows path into a file:// URI.
func FileURI(p string) string {
	p = filepath.ToSlash(p)
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	u := url.URL{Scheme: "file", Path: p}
	return u.String()
}

// URIToPath turns a file:// URI back into an OS path.
func URIToPath(raw string) (string, bool) {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "file" {
		return "", false
	}
	p := u.Path
	if runtime.GOOS == "windows" && len(p) >= 3 && p[0] == '/' && p[2] == ':' {
		p = p[1:]
	}
	return filepath.FromSlash(p), true
}

// OffsetToPosition converts a UTF-8 byte offset in text to an LSP Position
// (0-based line, UTF-16 code units).
func OffsetToPosition(text string, offset int) Position {
	if offset < 0 {
		offset = 0
	}
	if offset > len(text) {
		offset = len(text)
	}
	line := 0
	lineStart := 0
	for i := 0; i < offset; i++ {
		if text[i] == '\n' {
			line++
			lineStart = i + 1
		}
	}
	cols := 0
	for _, r := range text[lineStart:offset] {
		cols += utf16.RuneLen(r)
	}
	return Position{Line: line, Character: cols}
}

// PositionToOffset converts an LSP Position (0-based line, UTF-16 code units)
// to a UTF-8 byte offset within text.
func PositionToOffset(text string, pos Position) int {
	if pos.Line < 0 {
		return 0
	}
	line := 0
	off := 0
	for line < pos.Line && off < len(text) {
		nl := strings.IndexByte(text[off:], '\n')
		if nl < 0 {
			return len(text)
		}
		off += nl + 1
		line++
	}
	end := len(text)
	if nl := strings.IndexByte(text[off:], '\n'); nl >= 0 {
		end = off + nl
	}
	// Trim CR if present.
	if end > off && text[end-1] == '\r' {
		end--
	}
	want := max(pos.Character, 0)
	cols := 0
	i := off
	for i < end && cols < want {
		r, sz := utf8.DecodeRuneInString(text[i:end])
		cols += utf16.RuneLen(r)
		i += sz
	}
	return i
}

// ToEditorDiagnostics converts LSP diagnostics into MyGo editor diagnostics.
func ToEditorDiagnostics(text string, ds []Diagnostic) []editor.Diagnostic {
	if len(ds) == 0 {
		return nil
	}
	out := make([]editor.Diagnostic, 0, len(ds))
	for _, d := range ds {
		start := PositionToOffset(text, d.Range.Start)
		end := PositionToOffset(text, d.Range.End)
		if end <= start && start < len(text) {
			_, sz := utf8.DecodeRuneInString(text[start:])
			end = min(start+sz, len(text))
		}
		sev := editor.SeverityError
		switch d.Severity {
		case 2:
			sev = editor.SeverityWarning
		case 3:
			sev = editor.SeverityInfo
		case 4:
			sev = editor.SeverityHint
		}
		out = append(out, editor.Diagnostic{
			Start:    start,
			End:      end,
			Severity: sev,
			Message:  d.Message,
			Source:   d.Source,
		})
	}
	return out
}

func mapCompletionKind(k int) editor.CompletionKind {
	switch k {
	case 2:
		return editor.CompletionMethod
	case 3, 4:
		return editor.CompletionFunction
	case 5:
		return editor.CompletionField
	case 6:
		return editor.CompletionVariable
	case 7, 8, 22, 23:
		return editor.CompletionType
	case 9:
		return editor.CompletionModule
	case 14:
		return editor.CompletionKeyword
	case 15:
		return editor.CompletionSnippet
	case 21:
		return editor.CompletionConstant
	default:
		return editor.CompletionText
	}
}

func extractMarkup(raw json.RawMessage) string {
	raw = []byte(strings.TrimSpace(string(raw)))
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return strings.TrimSpace(s)
	}
	var obj struct {
		Value string `json:"value"`
	}
	if json.Unmarshal(raw, &obj) == nil && obj.Value != "" {
		return strings.TrimSpace(obj.Value)
	}
	var arr []json.RawMessage
	if json.Unmarshal(raw, &arr) == nil {
		var parts []string
		for _, item := range arr {
			if p := extractMarkup(item); p != "" {
				parts = append(parts, p)
			}
		}
		return strings.Join(parts, "\n\n")
	}
	return ""
}
