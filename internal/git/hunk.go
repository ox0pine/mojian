package git

import (
	"errors"
	"fmt"
	"strings"
)

// StdinRunner is an optional Runner extension that delivers stdin to Git
// (used by hunk-level git apply).
type StdinRunner interface {
	RunWithStdin(dir, stdin string, args ...string) (stdout string, err error)
}

// HunkPatch is one selectable hunk from a single-file unified diff along with
// a complete unified patch that `git apply` accepts on stdin.
type HunkPatch struct {
	Header string // e.g. "@@ -10,4 +10,6 @@ func foo()"
	Patch  string // file header + single hunk, ending with '\n'
}

// SplitHunks extracts individual hunks from a single-file unified diff.
func SplitHunks(rawDiff string) []HunkPatch {
	if rawDiff == "" {
		return nil
	}
	lines := strings.Split(strings.ReplaceAll(rawDiff, "\r\n", "\n"), "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	var fileHeader []string
	var hunks [][]string
	inHunk := false
	for _, line := range lines {
		if strings.HasPrefix(line, "@@ ") {
			inHunk = true
			hunks = append(hunks, []string{line})
			continue
		}
		if !inHunk {
			fileHeader = append(fileHeader, line)
		} else {
			hunks[len(hunks)-1] = append(hunks[len(hunks)-1], line)
		}
	}
	if len(hunks) == 0 || len(fileHeader) == 0 {
		return nil
	}
	headerPrefix := strings.Join(fileHeader, "\n") + "\n"
	out := make([]HunkPatch, 0, len(hunks))
	for _, h := range hunks {
		patch := headerPrefix + strings.Join(h, "\n") + "\n"
		out = append(out, HunkPatch{
			Header: h[0],
			Patch:  patch,
		})
	}
	return out
}

func runWithStdin(r Runner, dir, stdin string, args ...string) (string, error) {
	if r == nil {
		r = LocalRunner{}
	}
	sr, ok := r.(StdinRunner)
	if !ok {
		return "", errors.New("hunk actions are unavailable on this runner")
	}
	return sr.RunWithStdin(dir, stdin, args...)
}

// StageHunk stages hunkIndex (0-based) of c's unstaged diff into the index.
func StageHunk(r Runner, root string, c Change, hunkIndex int) error {
	if r == nil {
		r = LocalRunner{}
	}
	if root == "" {
		return errors.New("not a Git repository")
	}
	if c.Status == "U" {
		return Stage(r, root, c.Path)
	}
	raw, err := r.Run(root, "--literal-pathspecs", "diff", "--no-color", "--", c.Path)
	if err != nil {
		return err
	}
	hunks := SplitHunks(raw)
	if hunkIndex < 0 || hunkIndex >= len(hunks) {
		return fmt.Errorf("hunk %d is no longer available", hunkIndex+1)
	}
	_, err = runWithStdin(r, root, hunks[hunkIndex].Patch, "apply", "--cached", "--unidiff-zero", "-")
	return err
}

// UnstageHunk removes hunkIndex (0-based) of c's staged diff from the index.
func UnstageHunk(r Runner, root string, c Change, hunkIndex int) error {
	if r == nil {
		r = LocalRunner{}
	}
	if root == "" {
		return errors.New("not a Git repository")
	}
	raw, err := r.Run(root, "--literal-pathspecs", "diff", "--cached", "--no-color", "--", c.Path)
	if err != nil {
		return err
	}
	hunks := SplitHunks(raw)
	if hunkIndex < 0 || hunkIndex >= len(hunks) {
		return fmt.Errorf("staged hunk %d is no longer available", hunkIndex+1)
	}
	_, err = runWithStdin(r, root, hunks[hunkIndex].Patch, "apply", "--cached", "-R", "--unidiff-zero", "-")
	return err
}

// DiscardHunk reverts hunkIndex (0-based) of c's unstaged diff in the working tree.
func DiscardHunk(r Runner, root string, c Change, hunkIndex int) error {
	if r == nil {
		r = LocalRunner{}
	}
	if root == "" {
		return errors.New("not a Git repository")
	}
	raw, err := r.Run(root, "--literal-pathspecs", "diff", "--no-color", "--", c.Path)
	if err != nil {
		return err
	}
	hunks := SplitHunks(raw)
	if hunkIndex < 0 || hunkIndex >= len(hunks) {
		return fmt.Errorf("hunk %d is no longer available", hunkIndex+1)
	}
	_, err = runWithStdin(r, root, hunks[hunkIndex].Patch, "apply", "-R", "--unidiff-zero", "-")
	return err
}
