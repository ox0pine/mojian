package git

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"mojian/internal/gitenv"
	"mojian/internal/platform"

	"github.com/egoist/mygo/ui/editor"
)

// repoRoot resolves the project's symlinks and finds the repository that
// holds it, with the project's path inside it. ok is false outside one.
func repoRoot(r Runner, project string) (root, prefix string, ok bool) {
	if resolved, err := r.RealPath(project); err == nil && resolved != "" {
		project = resolved
	}
	out, err := r.Run(project, "rev-parse", "--show-toplevel")
	if err != nil {
		return "", "", false
	}
	root = strings.TrimSpace(out)
	if root == "" {
		return "", "", false
	}
	if rel, err := filepath.Rel(root, project); err == nil && rel != "." && rel != "" {
		prefix = filepath.ToSlash(rel)
	}
	return root, prefix, true
}

// ReadStatus reads the repository's branch and changed files under
// the project.
func ReadStatus(r Runner, project string) StatusData {
	if r == nil {
		r = LocalRunner{}
	}
	d := StatusData{Root: project}
	root, prefix, ok := repoRoot(r, project)
	if !ok {
		d.Err = "This folder is not a Git repository."
		return d
	}
	d.Root, d.Prefix = root, prefix
	if branch, err := r.Run(d.Root, "rev-parse", "--abbrev-ref", "HEAD"); err == nil {
		d.Branch = strings.TrimSpace(branch)
	} else if branch, err := r.Run(d.Root, "symbolic-ref", "--short", "HEAD"); err == nil {
		d.Branch = strings.TrimSpace(branch)
	}
	if d.Branch == "" || d.Branch == "HEAD" {
		d.Branch = "detached"
	}
	pathspec := "."
	if d.Prefix != "" {
		pathspec = d.Prefix
	}
	status, err := r.Run(d.Root, "status", "--porcelain=v1", "-z", "--untracked-files=all", "--", pathspec)
	if err != nil {
		d.Err = err.Error()
		return d
	}
	d.Changes = parseStatus(status, d.Prefix)
	return d
}

// parseStatus turns porcelain -z output into changes, scoped to prefix.
func parseStatus(out, prefix string) []Change {
	var changes []Change
	parts := strings.Split(out, "\x00")
	for i := 0; i < len(parts); i++ {
		entry := parts[i]
		if len(entry) < 4 {
			continue
		}
		code := entry[:2]
		path := entry[3:]
		oldPath := ""
		if code[0] == 'R' || code[0] == 'C' {
			// The old path follows: keep it so git can pair the rename
			// instead of reporting the whole file as new.
			if i+1 < len(parts) {
				oldPath = parts[i+1]
			}
			i++
		}
		rootPath := path
		display := path
		if prefix != "" {
			rest, ok := strings.CutPrefix(path, prefix+"/")
			if !ok {
				continue
			}
			display = rest
		}
		status := statusLetter(code)
		changes = append(changes, Change{
			Path:    rootPath,
			OldPath: oldPath,
			Display: display,
			Status:  status,
		})
	}
	sort.SliceStable(changes, func(i, j int) bool {
		if changes[i].Status == "U" && changes[j].Status != "U" {
			return false
		}
		if changes[j].Status == "U" && changes[i].Status != "U" {
			return true
		}
		return strings.ToLower(changes[i].Display) < strings.ToLower(changes[j].Display)
	})
	return changes
}

func statusLetter(code string) string {
	switch {
	case code == "??":
		return "U"
	case code == "AA" || code == "DD" || strings.Contains(code, "U"):
		return "C"
	case strings.Contains(code, "D"):
		return "D"
	case strings.Contains(code, "R"):
		return "R"
	case strings.Contains(code, "A"):
		return "A"
	default:
		return "M"
	}
}

// ReadDiff reads one file's diff against HEAD, including staged and
// unstaged work. Untracked files are shown as additions.
func ReadDiff(r Runner, root string, c Change) (string, error) {
	if r == nil {
		r = LocalRunner{}
	}
	path := c.Path
	if c.Status == "U" {
		return untrackedDiff(r, root, path, c.Display)
	}
	paths := []string{path}
	if c.OldPath != "" && c.OldPath != path {
		paths = append([]string{c.OldPath}, paths...)
	}
	out, err := r.Run(root, append([]string{"-c", "core.quotePath=false", "diff", "--no-color", "--no-ext-diff", "HEAD", "--"}, paths...)...)
	if err != nil && (strings.Contains(err.Error(), "unknown revision") ||
		strings.Contains(err.Error(), "bad revision") ||
		strings.Contains(err.Error(), "ambiguous argument")) {
		out, err = r.Run(root, append([]string{"-c", "core.quotePath=false", "diff", "--no-color", "--no-ext-diff", "--cached", "--"}, paths...)...)
	}
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(out) == "" {
		out, err = r.Run(root, append([]string{"-c", "core.quotePath=false", "diff", "--no-color", "--no-ext-diff", "--cached", "--"}, paths...)...)
		if err != nil {
			return "", err
		}
	}
	return out, nil
}

// untrackedDiff makes a new-file diff for a file Git does not track.
func untrackedDiff(r Runner, root, path, display string) (string, error) {
	data, err := r.ReadFile(root, path, 512*1024)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "is a directory") {
			return "This is a folder Git does not track.\n", nil
		}
		return "", err
	}
	if len(data) > 512*1024 {
		return "File is too large to show here.\n", nil
	}
	if bytes.IndexByte(data, 0) >= 0 {
		return "Binary file not shown.\n", nil
	}
	text := strings.ReplaceAll(string(data), "\r\n", "\n")
	lines := strings.Split(text, "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	if len(lines) == 0 {
		return "Empty file.\n", nil
	}
	var b strings.Builder
	fmt.Fprintf(&b, "diff --git a/%s b/%s\nnew file mode 100644\n--- /dev/null\n+++ b/%s\n", display, display, display)
	fmt.Fprintf(&b, "@@ -0,0 +1,%d @@\n", len(lines))
	for _, line := range lines {
		b.WriteByte('+')
		b.WriteString(line)
		b.WriteByte('\n')
	}
	return b.String(), nil
}

// runGit runs Git in a folder, with a deadline and a cap on its output.
func runGit(dir string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...)
	platform.HideConsole(cmd)
	cmd.Env = append(gitEnv(), "GIT_PAGER=cat", "GIT_TERMINAL_PROMPT=0", "LC_ALL=C")
	var out, errb cappedBuffer
	out.limit = 2 << 20
	errb.limit = 64 << 10
	cmd.Stdout = &out
	cmd.Stderr = &errb
	err := cmd.Run()
	if ctx.Err() != nil {
		return "", errors.New("git timed out")
	}
	if err != nil {
		msg := strings.TrimSpace(errb.String())
		if msg == "" {
			msg = err.Error()
		}
		return out.String(), errors.New(msg)
	}
	return out.String(), nil
}

// gitEnv is the inherited environment without the variables a shell
// could set to point Git at another repository.
func gitEnv() []string { return gitenv.Env() }

type cappedBuffer struct {
	bytes.Buffer
	limit int
}

func (b *cappedBuffer) Write(p []byte) (int, error) {
	if b.Len() < b.limit {
		n := min(len(p), b.limit-b.Len())
		_, _ = b.Buffer.Write(p[:n])
	}
	return len(p), nil
}

// FileDiff is one file's part of a patch that touched several.
type FileDiff struct {
	Path   string // the file's path in the repository
	Status string // M, A, D or R
	Add    int
	Del    int
	Lines  []gitDiffLine
}

// SplitDiff splits a patch into one part per file. A working tree diff
// holds one file; a commit's patch holds every file it touched, which is
// what the page's history shows.
func SplitDiff(diff string) []FileDiff {
	var files []FileDiff
	var section []string
	flush := func() {
		if len(section) > 0 {
			files = append(files, buildFileDiff(section))
			section = nil
		}
	}
	for _, line := range strings.Split(strings.TrimRight(diff, "\n"), "\n") {
		if strings.HasPrefix(line, "diff --git ") {
			flush()
		} else if section == nil {
			// Text before the first file header belongs to no file.
			continue
		}
		section = append(section, line)
	}
	flush()
	return files
}

// buildFileDiff reads one file's section: its path, what happened to it,
// and the lines the diff pane draws.
func buildFileDiff(section []string) FileDiff {
	file := FileDiff{Status: "M"}
	oldPath, newPath := "", ""
	for _, line := range section {
		if strings.HasPrefix(line, "@@") {
			break
		}
		switch {
		case strings.HasPrefix(line, "+++ "):
			newPath = diffPath(line[4:], "b/")
		case strings.HasPrefix(line, "--- "):
			oldPath = diffPath(line[4:], "a/")
		case strings.HasPrefix(line, "rename to "):
			newPath, file.Status = unquotePath(line[len("rename to "):]), "R"
		case strings.HasPrefix(line, "copy to "):
			newPath, file.Status = unquotePath(line[len("copy to "):]), "A"
		case strings.HasPrefix(line, "rename from "):
			file.Status = "R"
		case strings.HasPrefix(line, "copy from "):
			file.Status = "A"
		case strings.HasPrefix(line, "new file mode "):
			file.Status = "A"
		case strings.HasPrefix(line, "deleted file mode "):
			file.Status = "D"
		}
	}
	file.Path = newPath
	if file.Path == "" {
		file.Path = oldPath
	}
	if file.Path == "" {
		// A binary file or a mode change carries no +++ header.
		file.Path = diffGitPath(section[0])
	}
	file.Lines = ParseDiff(strings.Join(section, "\n"))
	file.Add, file.Del = DiffStat(strings.Join(section, "\n"))
	return file
}

// gitHunkCounts reads a hunk header's ranges and its section text.
var gitHunkCounts = regexp.MustCompile(`^@@ -(\d+)(?:,(\d+))? \+(\d+)(?:,(\d+))? @@ ?(.*)$`)

// toFilePatch turns a parsed file's diff into the patch MyGo's diff
// viewer draws. Lines are clipped as the page clipped them: a file Git
// reads as text but is really binary arrives as one line of megabytes,
// which costs the editor minutes to shape.
func toFilePatch(f FileDiff) editor.FilePatch {
	p := editor.FilePatch{}
	switch f.Status {
	case "A":
		p.NewName = f.Path
	case "D":
		p.OldName = f.Path
	default:
		p.OldName, p.NewName = f.Path, f.Path
	}
	var h *editor.PatchHunk
	for _, line := range f.Lines {
		if line.kind == '@' {
			m := gitHunkCounts.FindStringSubmatch(line.text)
			if m == nil {
				h = nil
				continue
			}
			p.Hunks = append(p.Hunks, editor.PatchHunk{
				OldStart: hunkCount(m[1]), OldLines: hunkCount(m[2]),
				NewStart: hunkCount(m[3]), NewLines: hunkCount(m[4]),
				Section: strings.TrimSpace(m[5]),
			})
			h = &p.Hunks[len(p.Hunks)-1]
			continue
		}
		if h == nil {
			continue
		}
		text := line.text
		if text == "" {
			// ParseDiff's blank context line carries no prefix.
			text = " "
		}
		if line.kind != '\\' {
			text = clipDiffLine(text)
		}
		h.Lines = append(h.Lines, text)
	}
	return p
}

// clipDiffLine keeps a patch line's prefix and cuts its text.
func clipDiffLine(line string) string {
	if line == "" {
		return line
	}
	return line[:1] + clipRunes(line[1:], diffLineRunes)
}

// hunkCount reads a hunk header's line count, where Git omits a count of
// one.
func hunkCount(s string) int {
	if s == "" {
		return 1
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0
	}
	return n
}

// diffPath reads one side of a header: /dev/null is no path, and the
// a/ or b/ prefix is Git's, not the file's.
func diffPath(s, dir string) string {
	s = unquotePath(s)
	if s == "/dev/null" {
		return ""
	}
	return strings.TrimPrefix(s, dir)
}

// unquotePath undoes Git's C-style quoting, which it applies to a path
// carrying a quote, a backslash or a control character.
func unquotePath(s string) string {
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		if u, err := strconv.Unquote(s); err == nil {
			return u
		}
	}
	return s
}

// diffGitPath reads the new path off a "diff --git a/x b/y" line, for a
// section that has no +++ header to name the file.
func diffGitPath(line string) string {
	rest := strings.TrimPrefix(line, "diff --git ")
	for _, cut := range []string{` "b/`, " b/"} {
		if i := strings.LastIndex(rest, cut); i >= 0 {
			return strings.TrimPrefix(unquotePath(rest[i+1:]), "b/")
		}
	}
	return ""
}

var gitHunkPattern = regexp.MustCompile(`^@@ -(\d+)(?:,\d+)? \+(\d+)(?:,\d+)? @@`)

// gitDiffLine is one parsed line of a unified diff, with the old and new
// line numbers its hunk gives it.
type gitDiffLine struct {
	text string
	old  int
	new  int
	kind byte // ' ', '+', '-', '@', 'm' or '\\'
}

func ParseDiff(diff string) []gitDiffLine {
	var out []gitDiffLine
	oldNo, newNo := 0, 0
	inHunk := false
	for _, line := range strings.Split(strings.TrimRight(diff, "\n"), "\n") {
		if strings.HasPrefix(line, "diff --git ") {
			inHunk = false
			continue
		}
		// File headers only appear before a hunk. Inside one, "--- a/x"
		// is a deleted line whose text starts with "--", not metadata.
		if !inHunk && diffMeta(line) {
			continue
		}
		if strings.HasPrefix(line, "\\") {
			out = append(out, gitDiffLine{text: line, kind: '\\'})
			continue
		}
		if strings.HasPrefix(line, "@@") {
			inHunk = true
			if m := gitHunkPattern.FindStringSubmatch(line); len(m) == 3 {
				oldNo, _ = strconv.Atoi(m[1])
				newNo, _ = strconv.Atoi(m[2])
			}
			out = append(out, gitDiffLine{text: line, kind: '@'})
			continue
		}
		if line == "" {
			out = append(out, gitDiffLine{old: oldNo, new: newNo})
			oldNo++
			newNo++
			continue
		}
		kind := line[0]
		dl := gitDiffLine{text: line, kind: kind}
		switch kind {
		case '+':
			dl.new = newNo
			newNo++
		case '-':
			dl.old = oldNo
			oldNo++
		default:
			dl.old, dl.new = oldNo, newNo
			oldNo++
			newNo++
		}
		out = append(out, dl)
	}
	return out
}

func diffMeta(line string) bool {
	for _, prefix := range []string{
		"diff --git ", "index ", "new file mode ", "deleted file mode ",
		"similarity index ", "rename from ", "rename to ", "old mode ",
		"new mode ", "--- a/", "+++ b/", "--- /dev/null", "+++ /dev/null",
	} {
		if strings.HasPrefix(line, prefix) {
			return true
		}
	}
	return false
}

func DiffStat(diff string) (add, del int) {
	inHunk := false
	for _, line := range strings.Split(diff, "\n") {
		if strings.HasPrefix(line, "diff --git ") {
			inHunk = false
			continue
		}
		if !inHunk {
			if strings.HasPrefix(line, "@@") {
				inHunk = true
			}
			continue
		}
		if strings.HasPrefix(line, "\\") {
			continue
		}
		switch {
		case strings.HasPrefix(line, "+"):
			add++
		case strings.HasPrefix(line, "-"):
			del++
		}
	}
	return add, del
}
