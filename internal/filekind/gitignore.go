package filekind

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
)

// GitIgnore decides whether a folder is excluded by the .gitignore files
// above it. The Explorer hides a folder Git ignores, so a project's
// generated trees stay out of the way without hiding a tracked folder
// that happens to be named build or dist; the version-control folders and
// the app's trash stay hidden whatever Git says.
//
// Only folders are decided: a file the project ignores still shows, so a
// .env or a log the project keeps out of Git can still be opened.
type GitIgnore struct {
	root string

	mu   sync.Mutex
	dirs map[string]*ignoreRules
}

// NewGitIgnore reads the .gitignore files under root as folders come.
func NewGitIgnore(root string) *GitIgnore {
	return &GitIgnore{root: root, dirs: make(map[string]*ignoreRules)}
}

// ignoreRules are the patterns of one .gitignore, in file order.
type ignoreRules struct {
	patterns []ignorePattern
}

type ignorePattern struct {
	re     *regexp.Regexp
	negate bool
}

// DirIgnored reports whether rel, a slash path relative to the root, is
// a folder Git ignores. The patterns of a deeper .gitignore win over
// those of its parents, and the last pattern in a file that matches
// wins, as Git applies them.
func (g *GitIgnore) DirIgnored(rel string) bool {
	rel = strings.Trim(filepath.ToSlash(rel), "/")
	if rel == "" || rel == "." {
		return false
	}
	ignored := false
	for _, dir := range ignoreAncestors(rel) {
		rules := g.load(dir)
		sub := rel
		if dir != "" {
			sub = strings.TrimPrefix(rel, dir+"/")
		}
		if v, ok := rules.match(sub); ok {
			ignored = v
		}
	}
	return ignored
}

// ignoreAncestors lists the folders whose .gitignore can apply to rel,
// the root first.
func ignoreAncestors(rel string) []string {
	dirs := []string{""}
	parts := strings.Split(rel, "/")
	for i := 1; i < len(parts); i++ {
		dirs = append(dirs, strings.Join(parts[:i], "/"))
	}
	return dirs
}

// load parses the .gitignore of one folder, once. A folder without one
// keeps an empty rule set, so a walk reads each file at most once.
func (g *GitIgnore) load(dir string) *ignoreRules {
	g.mu.Lock()
	defer g.mu.Unlock()
	rules, ok := g.dirs[dir]
	if ok {
		return rules
	}
	rules = &ignoreRules{}
	g.dirs[dir] = rules
	data, err := os.ReadFile(filepath.Join(g.root, filepath.FromSlash(dir), ".gitignore"))
	if err != nil {
		return rules
	}
	for _, line := range strings.Split(string(data), "\n") {
		if p, ok := parseIgnore(line); ok {
			rules.patterns = append(rules.patterns, p)
		}
	}
	return rules
}

// match reports whether rel, a folder under this rule set's folder, is
// ignored, and whether any pattern had an opinion at all.
func (r *ignoreRules) match(rel string) (bool, bool) {
	ignored, found := false, false
	for _, p := range r.patterns {
		if !p.re.MatchString(rel) {
			continue
		}
		ignored, found = !p.negate, true
	}
	return ignored, found
}

// parseIgnore compiles one line of a .gitignore. A trailing slash limits
// a pattern to folders, which is every path a tree decides; a pattern
// with a slash inside is anchored to the .gitignore's folder, and one
// without matches at any depth.
func parseIgnore(line string) (ignorePattern, bool) {
	line = strings.TrimSuffix(line, "\r")
	for strings.HasSuffix(line, " ") {
		backslashes := 0
		for i := len(line) - 2; i >= 0 && line[i] == '\\'; i-- {
			backslashes++
		}
		if backslashes%2 == 1 {
			break
		}
		line = strings.TrimSuffix(line, " ")
	}
	if line == "" || strings.HasPrefix(line, "#") {
		return ignorePattern{}, false
	}
	p := ignorePattern{}
	if strings.HasPrefix(line, `\#`) || strings.HasPrefix(line, `\!`) {
		line = line[1:]
	} else if strings.HasPrefix(line, "!") {
		p.negate = true
		line = strings.TrimPrefix(line, "!")
	}
	line = strings.TrimSuffix(line, "/")
	anchored := strings.Contains(line, "/")
	line = strings.TrimPrefix(line, "/")
	if line == "" {
		return ignorePattern{}, false
	}
	re, err := regexp.Compile("^" + globRegexp(line, anchored) + "$")
	if err != nil {
		return ignorePattern{}, false
	}
	p.re = re
	return p, true
}

// globRegexp turns a gitignore glob into a regular expression. A pattern
// without a slash matches a path component at any depth; a ** path
// component crosses folders, while ordinary stars and ? do not.
func globRegexp(glob string, anchored bool) string {
	var b strings.Builder
	if !anchored {
		b.WriteString(`(?:.*/)?`)
	}
	for i := 0; i < len(glob); i++ {
		switch ch := glob[i]; ch {
		case '*':
			if i+1 < len(glob) && glob[i+1] == '*' {
				end := i + 2
				component := i == 0 || glob[i-1] == '/'
				switch {
				case component && end < len(glob) && glob[end] == '/':
					i = end
					b.WriteString(`(?:.*/)?`)
				case i > 0 && glob[i-1] == '/' && end == len(glob):
					i++
					b.WriteString(`.*`)
				default:
					// Consecutive stars inside an ordinary component have
					// the same directory boundary as a single star.
					i++
					b.WriteString(`[^/]*[^/]*`)
				}
			} else {
				b.WriteString(`[^/]*`)
			}
		case '?':
			b.WriteString(`[^/]`)
		case '[':
			end := i + 1
			if end < len(glob) && (glob[end] == '!' || glob[end] == '^') {
				end++
			}
			if end < len(glob) && glob[end] == ']' {
				end++
			}
			for end < len(glob) {
				if glob[end] == '\\' && end+1 < len(glob) {
					end += 2
					continue
				}
				if glob[end] == '[' && end+1 < len(glob) && glob[end+1] == ':' {
					if close := strings.Index(glob[end+2:], ":]"); close >= 0 {
						end += close + 4
						continue
					}
				}
				if glob[end] == ']' {
					break
				}
				end++
			}
			if end >= len(glob) {
				b.WriteString(`\[`)
				continue
			}
			class := glob[i+1 : end]
			if strings.HasPrefix(class, "!") {
				class = "^" + class[1:]
			}
			b.WriteString("[" + class + "]")
			i = end
		case '\\':
			if i+1 < len(glob) {
				i++
				b.WriteString(regexp.QuoteMeta(string(glob[i])))
			} else {
				b.WriteString(`\\`)
			}
		default:
			b.WriteString(regexp.QuoteMeta(string(ch)))
		}
	}
	return b.String()
}
