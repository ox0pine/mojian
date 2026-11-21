package session

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"

	"mojian/internal/filekind"
)

const (
	defaultSearchLimit = 500
	maxSearchLimit     = 1000
	maxSearchFileBytes = 2 << 20
	maxSearchExcerpt   = 180
)

// searchMatcher finds the first match span [start, end) in a line.
type searchMatcher struct {
	re            *regexp.Regexp
	query         string
	lower         string
	caseSensitive bool
}

// CompileSearch validates opts and builds a reusable line matcher.
func compileSearch(opts SearchOptions) (*searchMatcher, error) {
	if opts.Query == "" {
		return nil, nil
	}
	if opts.Regex {
		pat := opts.Query
		if !opts.CaseSensitive {
			pat = "(?i)" + pat
		}
		re, err := regexp.Compile(pat)
		if err != nil {
			return nil, fmt.Errorf("invalid regular expression: %w", err)
		}
		return &searchMatcher{re: re, query: opts.Query, caseSensitive: opts.CaseSensitive}, nil
	}
	return &searchMatcher{
		query:         opts.Query,
		lower:         strings.ToLower(opts.Query),
		caseSensitive: opts.CaseSensitive,
	}, nil
}

func (m *searchMatcher) find(line string) (start, end int, ok bool) {
	if m.re != nil {
		loc := m.re.FindStringIndex(line)
		if loc == nil {
			return 0, 0, false
		}
		return loc[0], loc[1], true
	}
	if m.caseSensitive {
		i := strings.Index(line, m.query)
		if i < 0 {
			return 0, 0, false
		}
		return i, i + len(m.query), true
	}
	// Map byte offsets safely when lowercasing changes UTF-8 byte lengths.
	low := strings.ToLower(line)
	i := strings.Index(low, m.lower)
	if i < 0 {
		return 0, 0, false
	}
	if len(low) == len(line) {
		return i, i + len(m.lower), true
	}
	// Fallback: match by rune index to keep byte offsets valid on line.
	rStart := utf8.RuneCountInString(low[:i])
	rLen := utf8.RuneCountInString(m.lower)
	byteStart, byteEnd := 0, len(line)
	rIdx := 0
	for off := range line {
		if rIdx == rStart {
			byteStart = off
		}
		if rIdx == rStart+rLen {
			byteEnd = off
			break
		}
		rIdx++
	}
	return byteStart, byteEnd, true
}

func normalizeSearchLimit(limit int) int {
	if limit <= 0 {
		return defaultSearchLimit
	}
	if limit > maxSearchLimit {
		return maxSearchLimit
	}
	return limit
}

// SearchText searches text for matches of opts, returning at most limit matches
// and whether additional matches were truncated.
func SearchText(rel, text string, opts SearchOptions, limit int) ([]SearchMatch, bool, error) {
	m, err := compileSearch(opts)
	if err != nil || m == nil {
		return nil, false, err
	}
	return searchTextWithMatcher(rel, text, m, normalizeSearchLimit(limit))
}

func searchTextWithMatcher(rel, text string, m *searchMatcher, limit int) ([]SearchMatch, bool, error) {
	var out []SearchMatch
	rel = filepath.ToSlash(rel)
	lineNo := 0
	for len(text) > 0 {
		lineNo++
		var line string
		if nl := strings.IndexByte(text, '\n'); nl >= 0 {
			line = text[:nl]
			text = text[nl+1:]
		} else {
			line = text
			text = ""
		}
		line = strings.TrimSuffix(line, "\r")
		start, end, ok := m.find(line)
		if !ok {
			continue
		}
		if len(out) >= limit {
			return out, true, nil
		}
		out = append(out, SearchMatch{
			Rel:  rel,
			Line: lineNo,
			Col:  start,
			End:  end,
			Text: searchExcerpt(line, start, end),
		})
	}
	return out, false, nil
}

// searchExcerpt trims outer whitespace and clips very long lines around
// [start, end) so minified files remain fast to render.
func searchExcerpt(line string, start, end int) string {
	trimmed := strings.TrimSpace(line)
	if utf8.RuneCountInString(trimmed) <= maxSearchExcerpt {
		return trimmed
	}
	// Compute a rune window centered around the match start.
	runes := []rune(line)
	rStart := utf8.RuneCountInString(line[:min(start, len(line))])
	left := max(rStart-40, 0)
	right := min(left+maxSearchExcerpt, len(runes))
	if right-left < maxSearchExcerpt && left > 0 {
		left = max(right-maxSearchExcerpt, 0)
	}
	prefix, suffix := "", ""
	if left > 0 {
		prefix = "…"
	}
	if right < len(runes) {
		suffix = "…"
	}
	return prefix + strings.TrimSpace(string(runes[left:right])) + suffix
}

// SearchDir walks root (following the project's .gitignore and text
// classification rules) and returns bounded matches for opts.
func SearchDir(ctx context.Context, root string, opts SearchOptions) (SearchResult, error) {
	return SearchDirWithOverrides(ctx, root, nil, opts)
}

// SearchDirWithOverrides is SearchDir with in-memory overrides keyed by
// slash-separated relative path: overridden files are searched from the
// map instead of disk, so unsaved edits are reflected accurately.
func SearchDirWithOverrides(ctx context.Context, root string, overrides map[string]string, opts SearchOptions) (SearchResult, error) {
	m, err := compileSearch(opts)
	if err != nil {
		return SearchResult{}, err
	}
	if m == nil {
		return SearchResult{}, nil
	}
	limit := normalizeSearchLimit(opts.Limit)
	var out SearchResult
	seenFiles := map[string]bool{}

	addMatches := func(rel string, ms []SearchMatch, trunc bool) {
		if len(ms) > 0 {
			if !seenFiles[rel] {
				seenFiles[rel] = true
				out.Files++
			}
			out.Matches = append(out.Matches, ms...)
		}
		if trunc {
			out.Truncated = true
		}
	}

	// Search in-memory overrides first in deterministic path order.
	if len(overrides) > 0 {
		keys := make([]string, 0, len(overrides))
		for k := range overrides {
			keys = append(keys, filepath.ToSlash(k))
		}
		sort.Strings(keys)
		normOverrides := make(map[string]string, len(overrides))
		for k, v := range overrides {
			normOverrides[filepath.ToSlash(k)] = v
		}
		overrides = normOverrides
		for _, rel := range keys {
			if err := ctx.Err(); err != nil {
				return SearchResult{}, err
			}
			rem := limit - len(out.Matches)
			if rem <= 0 {
				// Probe whether there are more matches to set Truncated.
				ms, _, _ := searchTextWithMatcher(rel, overrides[rel], m, 1)
				if len(ms) > 0 {
					out.Truncated = true
					break
				}
				continue
			}
			ms, trunc, _ := searchTextWithMatcher(rel, overrides[rel], m, rem)
			addMatches(rel, ms, trunc)
		}
	}

	if root == "" {
		return out, nil
	}
	resolved, err := filepath.EvalSymlinks(root)
	if err != nil {
		return SearchResult{}, err
	}
	ign := filekind.NewGitIgnore(resolved)
	errStop := fmt.Errorf("search limit reached")
	err = filepath.WalkDir(resolved, func(p string, d os.DirEntry, walkErr error) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if walkErr != nil {
			if p == resolved {
				return walkErr
			}
			return nil
		}
		if p == resolved {
			return nil
		}
		if d.IsDir() {
			if filekind.SkipDir(d.Name()) {
				return filepath.SkipDir
			}
			rel, relErr := filepath.Rel(resolved, p)
			if relErr != nil {
				return nil
			}
			if ign.DirIgnored(filepath.ToSlash(rel)) {
				return filepath.SkipDir
			}
			return nil
		}
		rel, relErr := filepath.Rel(resolved, p)
		if relErr != nil {
			return nil
		}
		slashRel := filepath.ToSlash(rel)
		if _, overridden := overrides[slashRel]; overridden {
			return nil
		}
		info, infoErr := d.Info()
		if infoErr != nil || info.Size() > maxSearchFileBytes {
			return nil
		}
		kind, kindErr := filekind.ClassifyFile(p)
		if kindErr != nil || kind != filekind.Text {
			return nil
		}
		data, readErr := os.ReadFile(p)
		if readErr != nil {
			return nil
		}
		rem := limit - len(out.Matches)
		if rem <= 0 {
			ms, _, _ := searchTextWithMatcher(slashRel, string(data), m, 1)
			if len(ms) > 0 {
				out.Truncated = true
				return errStop
			}
			return nil
		}
		ms, trunc, _ := searchTextWithMatcher(slashRel, string(data), m, rem)
		addMatches(slashRel, ms, trunc)
		if out.Truncated {
			return errStop
		}
		return nil
	})
	if err != nil && err != errStop {
		return SearchResult{}, err
	}
	SortSearchMatches(out.Matches)
	return out, nil
}

// SortSearchMatches orders matches deterministically by path, line and column.
func SortSearchMatches(ms []SearchMatch) {
	sort.SliceStable(ms, func(i, j int) bool {
		if ms[i].Rel != ms[j].Rel {
			return ms[i].Rel < ms[j].Rel
		}
		if ms[i].Line != ms[j].Line {
			return ms[i].Line < ms[j].Line
		}
		return ms[i].Col < ms[j].Col
	})
}
