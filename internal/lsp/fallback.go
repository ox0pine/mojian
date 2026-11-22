package lsp

import (
	"path/filepath"
	"regexp"
	"strings"
)

var (
	reGoFunc      = regexp.MustCompile(`^func\s+(?:\(([^)]+)\)\s+)?([A-Za-z_][A-Za-z0-9_]*)\s*\(`)
	reGoType      = regexp.MustCompile(`^type\s+([A-Za-z_][A-Za-z0-9_]*)\s+(struct|interface|[^\s{]+)`)
	rePyDef       = regexp.MustCompile(`^\s*(?:async\s+)?def\s+([A-Za-z_][A-Za-z0-9_]*)\s*\(`)
	rePyClass     = regexp.MustCompile(`^\s*class\s+([A-Za-z_][A-Za-z0-9_]*)`)
	reJSFunc      = regexp.MustCompile(`^\s*(?:export\s+)?(?:default\s+)?(?:async\s+)?function\s+([A-Za-z_$][A-Za-z0-9_$]*)`)
	reJSClass     = regexp.MustCompile(`^\s*(?:export\s+)?(?:default\s+)?(?:abstract\s+)?class\s+([A-Za-z_$][A-Za-z0-9_$]*)`)
	reTSInterface = regexp.MustCompile(`^\s*(?:export\s+)?(interface|type|enum)\s+([A-Za-z_$][A-Za-z0-9_$]*)`)
	reRustItem    = regexp.MustCompile(`^\s*(?:pub(?:\([^)]*\))?\s+)?(?:async\s+)?(fn|struct|enum|trait|impl)\s+([A-Za-z_][A-Za-z0-9_:]*)`)
	reMdHeading   = regexp.MustCompile(`^(#{1,6})\s+(.+?)\s*#*$`)
)

// FallbackSymbols extracts top-level symbols from text without an LSP server,
// so Go to Symbol (⇧⌘O) works immediately for Markdown, Go, JS/TS, Python and Rust.
func FallbackSymbols(rel, text string) []Symbol {
	ext := strings.ToLower(filepath.Ext(rel))
	lines := strings.Split(text, "\n")
	var out []Symbol
	for i, raw := range lines {
		line := strings.TrimSuffix(raw, "\r")
		switch ext {
		case ".md", ".markdown", ".mdx":
			if m := reMdHeading.FindStringSubmatch(line); len(m) == 3 {
				out = append(out, Symbol{
					Name:   strings.TrimSpace(m[2]),
					Detail: m[1],
					Kind:   15,
					Line:   i,
				})
			}
		case ".go":
			if m := reGoFunc.FindStringSubmatch(line); len(m) == 3 {
				kind := 12
				detail := ""
				if m[1] != "" {
					kind = 6
					detail = "(" + strings.TrimSpace(m[1]) + ")"
				}
				out = append(out, Symbol{Name: m[2], Detail: detail, Kind: kind, Line: i})
			} else if m := reGoType.FindStringSubmatch(line); len(m) == 3 {
				kind := 26
				switch m[2] {
				case "struct":
					kind = 23
				case "interface":
					kind = 11
				}
				out = append(out, Symbol{Name: m[1], Detail: m[2], Kind: kind, Line: i})
			}
		case ".py":
			if m := rePyClass.FindStringSubmatch(line); len(m) == 2 {
				out = append(out, Symbol{Name: m[1], Kind: 5, Line: i})
			} else if m := rePyDef.FindStringSubmatch(line); len(m) == 2 {
				out = append(out, Symbol{Name: m[1], Kind: 12, Line: i})
			}
		case ".ts", ".tsx", ".mts", ".cts", ".js", ".jsx", ".mjs", ".cjs":
			if m := reJSClass.FindStringSubmatch(line); len(m) == 2 {
				out = append(out, Symbol{Name: m[1], Kind: 5, Line: i})
			} else if m := reJSFunc.FindStringSubmatch(line); len(m) == 2 {
				out = append(out, Symbol{Name: m[1], Kind: 12, Line: i})
			} else if m := reTSInterface.FindStringSubmatch(line); len(m) == 3 {
				kind := 26
				if m[1] == "interface" {
					kind = 11
				} else if m[1] == "enum" {
					kind = 10
				}
				out = append(out, Symbol{Name: m[2], Detail: m[1], Kind: kind, Line: i})
			}
		case ".rs":
			if m := reRustItem.FindStringSubmatch(line); len(m) == 3 {
				kind := 12
				switch m[1] {
				case "struct":
					kind = 23
				case "enum":
					kind = 10
				case "trait":
					kind = 11
				case "impl":
					kind = 5
				}
				out = append(out, Symbol{Name: m[2], Detail: m[1], Kind: kind, Line: i})
			}
		}
	}
	return out
}
