package project

import (
	"context"
	"path/filepath"

	"mojian/internal/fs"
	"mojian/internal/session"
)

// SearchOverrides snapshots in-memory texts that must override disk content
// during a background project search: every note in a memory store, or
// every dirty note in a disk/server store. It must run on the UI thread.
func (s *Store) SearchOverrides() map[string]string {
	if s == nil {
		return nil
	}
	if s.IsMemory() {
		out := make(map[string]string, len(s.Notes))
		for _, n := range s.Notes {
			out[filepath.ToSlash(n.Rel)] = n.Text()
		}
		return out
	}
	var out map[string]string
	for _, n := range s.Notes {
		if !n.Dirty() {
			continue
		}
		if out == nil {
			out = map[string]string{}
		}
		out[filepath.ToSlash(n.Rel)] = n.Text()
	}
	return out
}

// Search searches backend for opts, merging overrides from dirty buffers
// or a memory-backed store. It is safe to run off the UI thread.
func Search(ctx context.Context, backend fs.Backend, overrides map[string]string, opts session.SearchOptions) (session.SearchResult, error) {
	if backend == nil {
		return session.SearchResult{}, nil
	}
	if opts.Query == "" {
		return session.SearchResult{}, nil
	}
	switch backend.Kind() {
	case fs.KindMemory:
		return session.SearchDirWithOverrides(ctx, "", overrides, opts)
	case fs.KindDirect:
		return session.SearchDirWithOverrides(ctx, backend.Root(), overrides, opts)
	default:
		ctl := backend.Client()
		if ctl == nil {
			return session.SearchDirWithOverrides(ctx, "", overrides, opts)
		}
		diskRes, err := ctl.SearchContext(ctx, backend.Root(), opts)
		if err != nil {
			return session.SearchResult{}, err
		}
		if len(overrides) == 0 {
			return diskRes, nil
		}
		// Replace disk matches for overridden dirty notes with in-memory matches.
		norm := make(map[string]string, len(overrides))
		for k, v := range overrides {
			norm[filepath.ToSlash(k)] = v
		}
		limit := opts.Limit
		if limit <= 0 || limit > 1000 {
			limit = 500
		}
		overRes, err := session.SearchDirWithOverrides(ctx, "", norm, opts)
		if err != nil {
			return session.SearchResult{}, err
		}
		merged := session.SearchResult{Truncated: diskRes.Truncated || overRes.Truncated}
		files := map[string]bool{}
		appendMatch := func(m session.SearchMatch) {
			if len(merged.Matches) >= limit {
				merged.Truncated = true
				return
			}
			merged.Matches = append(merged.Matches, m)
			if !files[m.Rel] {
				files[m.Rel] = true
				merged.Files++
			}
		}
		for _, m := range overRes.Matches {
			appendMatch(m)
		}
		for _, m := range diskRes.Matches {
			if _, ok := norm[m.Rel]; ok {
				continue
			}
			appendMatch(m)
		}
		session.SortSearchMatches(merged.Matches)
		return merged, nil
	}
}
