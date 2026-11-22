package git

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// BlameInfo describes the commit that last modified a single line.
type BlameInfo struct {
	Hash    string
	Author  string
	Time    time.Time
	Summary string
	Unborn  bool
}

// InlineText formats BlameInfo as a quiet single-line trailing inlay.
func (b BlameInfo) InlineText() string {
	if b.Unborn || b.Author == "" {
		return "Uncommitted"
	}
	age := relativeAge(time.Since(b.Time))
	text := b.Author
	if age != "" {
		text += ", " + age
	}
	if b.Summary != "" {
		text += " · " + b.Summary
	}
	return text
}

func relativeAge(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	case d < 30*24*time.Hour:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	case d < 365*24*time.Hour:
		return fmt.Sprintf("%dmo ago", int(d.Hours()/(24*30)))
	default:
		return fmt.Sprintf("%dy ago", int(d.Hours()/(24*365)))
	}
}

// ReadLineBlame reads commit attribution for 1-based line1 of rel inside projectDir.
func ReadLineBlame(r Runner, projectDir, rel string, line1 int) (BlameInfo, bool) {
	if r == nil {
		r = LocalRunner{}
	}
	if projectDir == "" || rel == "" || line1 <= 0 {
		return BlameInfo{}, false
	}
	root, prefix, inRepo := repoRoot(r, projectDir)
	if !inRepo {
		return BlameInfo{}, false
	}
	repoRel := filepath.ToSlash(filepath.Clean(rel))
	if prefix != "" {
		repoRel = prefix + "/" + repoRel
	}
	spec := fmt.Sprintf("%d,%d", line1, line1)
	out, err := r.Run(root, "--literal-pathspecs", "blame", "-L", spec, "--porcelain", "--", repoRel)
	if err != nil || strings.TrimSpace(out) == "" {
		return BlameInfo{}, false
	}
	return parseBlamePorcelain(out)
}

func parseBlamePorcelain(out string) (BlameInfo, bool) {
	lines := strings.Split(out, "\n")
	if len(lines) == 0 {
		return BlameInfo{}, false
	}
	fields := strings.Fields(lines[0])
	if len(fields) == 0 {
		return BlameInfo{}, false
	}
	info := BlameInfo{Hash: fields[0]}
	if strings.Trim(info.Hash, "0") == "" {
		info.Unborn = true
		return info, true
	}
	for _, line := range lines[1:] {
		switch {
		case strings.HasPrefix(line, "author "):
			info.Author = strings.TrimPrefix(line, "author ")
		case strings.HasPrefix(line, "author-time "):
			if sec, err := strconv.ParseInt(strings.TrimSpace(strings.TrimPrefix(line, "author-time ")), 10, 64); err == nil {
				info.Time = time.Unix(sec, 0)
			}
		case strings.HasPrefix(line, "summary "):
			info.Summary = strings.TrimSpace(strings.TrimPrefix(line, "summary "))
		}
	}
	if info.Author == "" && info.Summary == "" {
		return BlameInfo{}, false
	}
	return info, true
}

// ReadHeadFile returns the HEAD version of rel (relative to projectDir)
// for gutter diff markers. ok is false outside a repository, on an unborn
// HEAD, or when the file is not tracked at HEAD.
func ReadHeadFile(r Runner, projectDir, rel string) (text string, ok bool, err error) {
	if r == nil {
		r = LocalRunner{}
	}
	if projectDir == "" || rel == "" {
		return "", false, nil
	}
	root, prefix, inRepo := repoRoot(r, projectDir)
	if !inRepo {
		return "", false, nil
	}
	repoRel := filepath.ToSlash(filepath.Clean(rel))
	if prefix != "" {
		repoRel = prefix + "/" + repoRel
	}
	out, runErr := r.Run(root, "show", "HEAD:"+repoRel)
	if runErr != nil {
		// Untracked file, deleted from HEAD, or unborn HEAD.
		return "", false, nil
	}
	return out, true, nil
}
