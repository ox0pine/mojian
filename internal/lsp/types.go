// Package lsp implements a lightweight, zero-configuration Language Server
// Protocol client over JSON-RPC 2.0 stdio streams (local or remote).
package lsp

import (
	"encoding/json"
	"net/url"
	"path/filepath"
	"runtime"
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

// Symbol is a flattened document symbol for Go to Symbol (⇧⌘O).
type Symbol struct {
	Name   string
	Detail string
	Kind   int
	Line   int // 0-based
	Col    int // 0-based byte offset within line
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
	Label            string          `json:"label"`
	Kind             int             `json:"kind,omitempty"`
	Detail           string          `json:"detail,omitempty"`
	Documentation    json.RawMessage `json:"documentation,omitempty"`
	FilterText       string          `json:"filterText,omitempty"`
	InsertText       string          `json:"insertText,omitempty"`
	InsertTextFormat int             `json:"insertTextFormat,omitempty"`
	TextEdit         *struct {
		NewText string `json:"newText"`
	} `json:"textEdit,omitempty"`
}

type completionList struct {
	Items []completionItem `json:"items"`
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
