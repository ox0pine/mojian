// Package editorconfig parses and resolves .editorconfig files for a project
// without external dependencies. It reads files through a caller-supplied
// Reader so both local and remote SSH projects share one implementation.
package editorconfig

import (
	"path"
	"strconv"
	"strings"
)

// Props is the resolved EditorConfig properties for one project file.
type Props struct {
	IndentStyle            string // "", "space", or "tab"
	IndentSize             int    // 0 if unset
	TabWidth               int    // 0 if unset
	EndOfLine              string // "", "lf", or "crlf"
	TrimTrailingWhitespace *bool
	InsertFinalNewline     *bool
}

// Reader reads a project-relative slash-separated file path (e.g. ".editorconfig"
// or "src/.editorconfig") and returns its contents and whether it exists.
type Reader func(rel string) ([]byte, bool)

// Resolve resolves EditorConfig properties for rel (project-relative path,
// slash or OS separated) by walking upward from rel's directory to the project
// root and stopping at the first .editorconfig with root = true.
func Resolve(rel string, read Reader) Props {
	if read == nil || rel == "" {
		return Props{}
	}
	rel = strings.TrimPrefix(path.Clean(strings.ReplaceAll(rel, "\\", "/")), "./")
	if rel == "." || rel == "" || strings.HasPrefix(rel, "..") {
		return Props{}
	}

	type layer struct {
		dir  string
		data []byte
	}
	var layers []layer
	dir := path.Dir(rel)
	if dir == "." {
		dir = ""
	}
	for {
		ecRel := ".editorconfig"
		if dir != "" {
			ecRel = dir + "/.editorconfig"
		}
		if data, ok := read(ecRel); ok {
			layers = append(layers, layer{dir: dir, data: data})
			if isRootConfig(data) {
				break
			}
		}
		if dir == "" {
			break
		}
		parent := path.Dir(dir)
		if parent == "." || parent == dir {
			dir = ""
		} else {
			dir = parent
		}
	}
	if len(layers) == 0 {
		return Props{}
	}

	var out Props
	// Apply from shallowest (outermost) to deepest so deeper files override.
	for i := len(layers) - 1; i >= 0; i-- {
		l := layers[i]
		subRel := rel
		if l.dir != "" {
			subRel = strings.TrimPrefix(rel, l.dir+"/")
		}
		applyFile(&out, l.data, subRel)
	}
	// If indent_size was "tab" (represented as -1), inherit tab_width.
	if out.IndentSize < 0 {
		out.IndentSize = out.TabWidth
	}
	if out.TabWidth == 0 && out.IndentSize > 0 {
		out.TabWidth = out.IndentSize
	}
	return out
}

// ApplySaveTransforms applies trim_trailing_whitespace and insert_final_newline
// to LF-normalized text when those properties are explicitly configured.
func ApplySaveTransforms(text string, p Props) string {
	if p.TrimTrailingWhitespace == nil && p.InsertFinalNewline == nil {
		return text
	}
	out := text
	if p.TrimTrailingWhitespace != nil && *p.TrimTrailingWhitespace {
		lines := strings.Split(out, "\n")
		for i, line := range lines {
			lines[i] = strings.TrimRight(line, " \t")
		}
		out = strings.Join(lines, "\n")
	}
	if p.InsertFinalNewline != nil {
		if *p.InsertFinalNewline {
			if len(out) > 0 && !strings.HasSuffix(out, "\n") {
				out += "\n"
			}
		} else {
			out = strings.TrimRight(out, "\n")
		}
	}
	return out
}

func isRootConfig(data []byte) bool {
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}
		if strings.HasPrefix(line, "[") {
			return false
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(k), "root") && strings.EqualFold(strings.TrimSpace(v), "true") {
			return true
		}
	}
	return false
}

func applyFile(out *Props, data []byte, rel string) {
	matched := false
	for _, rawLine := range strings.Split(string(data), "\n") {
		line := strings.TrimSpace(rawLine)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			pat := line[1 : len(line)-1]
			matched = matchGlob(pat, rel)
			continue
		}
		if !matched {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key := strings.ToLower(strings.TrimSpace(k))
		val := strings.ToLower(strings.TrimSpace(v))
		switch key {
		case "indent_style":
			switch val {
			case "space", "tab":
				out.IndentStyle = val
			case "unset":
				out.IndentStyle = ""
			}
		case "indent_size":
			switch val {
			case "tab":
				out.IndentSize = -1
			case "unset":
				out.IndentSize = 0
			default:
				if n, err := strconv.Atoi(val); err == nil && n >= 1 && n <= 8 {
					out.IndentSize = n
				}
			}
		case "tab_width":
			if val == "unset" {
				out.TabWidth = 0
			} else if n, err := strconv.Atoi(val); err == nil && n >= 1 && n <= 8 {
				out.TabWidth = n
			}
		case "end_of_line":
			switch val {
			case "lf", "crlf":
				out.EndOfLine = val
			case "unset":
				out.EndOfLine = ""
			}
		case "trim_trailing_whitespace":
			switch val {
			case "true":
				b := true
				out.TrimTrailingWhitespace = &b
			case "false":
				b := false
				out.TrimTrailingWhitespace = &b
			case "unset":
				out.TrimTrailingWhitespace = nil
			}
		case "insert_final_newline":
			switch val {
			case "true":
				b := true
				out.InsertFinalNewline = &b
			case "false":
				b := false
				out.InsertFinalNewline = &b
			case "unset":
				out.InsertFinalNewline = nil
			}
		}
	}
}

// matchGlob matches rel (slash-separated path relative to the .editorconfig
// directory) against an EditorConfig section pattern.
func matchGlob(pattern, rel string) bool {
	if pattern == "" {
		return false
	}
	if !strings.Contains(pattern, "/") {
		// Patterns without a slash match the basename at any depth.
		return matchExpanded(pattern, path.Base(rel))
	}
	pattern = strings.TrimPrefix(pattern, "/")
	return matchExpanded(pattern, rel)
}

func matchExpanded(pattern, target string) bool {
	for _, p := range expandBraces(pattern, 0) {
		if matchWildcard(p, target) {
			return true
		}
	}
	return false
}

// expandBraces expands {a,b,c} and {1..5} brace expressions with bounded depth.
func expandBraces(p string, depth int) []string {
	if depth > 4 {
		return []string{p}
	}
	start := -1
	level := 0
	end := -1
	for i := 0; i < len(p); i++ {
		if p[i] == '\\' && i+1 < len(p) {
			i++
			continue
		}
		if p[i] == '{' {
			if level == 0 {
				start = i
			}
			level++
		} else if p[i] == '}' && level > 0 {
			level--
			if level == 0 {
				end = i
				break
			}
		}
	}
	if start < 0 || end < 0 {
		return []string{p}
	}
	prefix := p[:start]
	body := p[start+1 : end]
	suffix := p[end+1:]

	var alts []string
	if a, b, ok := strings.Cut(body, ".."); ok && !strings.Contains(body, ",") {
		lo, err1 := strconv.Atoi(a)
		hi, err2 := strconv.Atoi(b)
		if err1 == nil && err2 == nil {
			step := 1
			if lo > hi {
				step = -1
			}
			for n := lo; len(alts) < 64; n += step {
				alts = append(alts, strconv.Itoa(n))
				if n == hi {
					break
				}
			}
		}
	}
	if len(alts) == 0 {
		// Split on top-level commas only.
		partStart := 0
		nested := 0
		hasComma := false
		for i := 0; i < len(body); i++ {
			if body[i] == '\\' && i+1 < len(body) {
				i++
				continue
			}
			switch body[i] {
			case '{':
				nested++
			case '}':
				if nested > 0 {
					nested--
				}
			case ',':
				if nested == 0 {
					hasComma = true
					alts = append(alts, body[partStart:i])
					partStart = i + 1
				}
			}
		}
		if !hasComma {
			// Literal braces without comma or range.
			return []string{p}
		}
		alts = append(alts, body[partStart:])
	}

	var out []string
	for _, alt := range alts {
		expanded := expandBraces(prefix+alt+suffix, depth+1)
		out = append(out, expanded...)
		if len(out) >= 128 {
			return out[:128]
		}
	}
	return out
}

// matchWildcard matches an already brace-expanded EditorConfig pattern against s.
// '**' matches any characters including '/', '*' matches any except '/', '?' matches
// any single char except '/', and '[...]' matches a character class.
func matchWildcard(p, s string) bool {
	return matchAt(p, s)
}

func matchAt(p, s string) bool {
	for len(p) > 0 {
		if strings.HasPrefix(p, "**") {
			rest := p[2:]
			for strings.HasPrefix(rest, "**") {
				rest = rest[2:]
			}
			if len(rest) == 0 {
				return true
			}
			// '**/foo' should also match 'foo' at root.
			if rest[0] == '/' && matchAt(rest[1:], s) {
				return true
			}
			for i := 0; i <= len(s); i++ {
				if matchAt(rest, s[i:]) {
					return true
				}
			}
			return false
		}
		switch p[0] {
		case '*':
			rest := p[1:]
			for i := 0; i <= len(s); i++ {
				if i > 0 && s[i-1] == '/' {
					break
				}
				if matchAt(rest, s[i:]) {
					return true
				}
			}
			return false
		case '?':
			if len(s) == 0 || s[0] == '/' {
				return false
			}
			p, s = p[1:], s[1:]
		case '[':
			if len(s) == 0 || s[0] == '/' {
				return false
			}
			closeIdx := strings.IndexByte(p[1:], ']')
			if closeIdx < 0 {
				if s[0] != '[' {
					return false
				}
				p, s = p[1:], s[1:]
				continue
			}
			cls := p[1 : 1+closeIdx]
			neg := false
			if len(cls) > 0 && (cls[0] == '!' || cls[0] == '^') {
				neg = true
				cls = cls[1:]
			}
			hit := matchClass(cls, s[0])
			if neg {
				hit = !hit
			}
			if !hit {
				return false
			}
			p, s = p[2+closeIdx:], s[1:]
		case '\\':
			if len(p) > 1 {
				p = p[1:]
			}
			if len(s) == 0 || p[0] != s[0] {
				return false
			}
			p, s = p[1:], s[1:]
		default:
			if len(s) == 0 || p[0] != s[0] {
				return false
			}
			p, s = p[1:], s[1:]
		}
	}
	return len(s) == 0
}

func matchClass(cls string, ch byte) bool {
	for i := 0; i < len(cls); i++ {
		if i+2 < len(cls) && cls[i+1] == '-' {
			if ch >= cls[i] && ch <= cls[i+2] {
				return true
			}
			i += 2
			continue
		}
		if ch == cls[i] {
			return true
		}
	}
	return false
}
