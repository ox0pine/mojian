package git

import (
	"errors"
	"strings"
)

// Stage adds paths (relative to the repository root) to the index.
func Stage(r Runner, root string, paths ...string) error {
	if r == nil {
		r = LocalRunner{}
	}
	if root == "" {
		return errors.New("not a Git repository")
	}
	args := []string{"--literal-pathspecs", "add", "--"}
	if len(paths) == 0 {
		args = []string{"--literal-pathspecs", "add", "-A", "--", "."}
	} else {
		args = append(args, paths...)
	}
	_, err := r.Run(root, args...)
	return err
}

// Unstage removes paths from the index while keeping working-tree edits.
func Unstage(r Runner, root string, paths ...string) error {
	if r == nil {
		r = LocalRunner{}
	}
	if root == "" {
		return errors.New("not a Git repository")
	}
	args := []string{"--literal-pathspecs", "reset", "-q", "HEAD", "--"}
	if len(paths) == 0 {
		args = append(args, ".")
	} else {
		args = append(args, paths...)
	}
	_, err := r.Run(root, args...)
	if err != nil && isUnbornHeadErr(err) {
		rmArgs := []string{"--literal-pathspecs", "rm", "--cached", "-r", "--quiet", "--"}
		if len(paths) == 0 {
			rmArgs = append(rmArgs, ".")
		} else {
			rmArgs = append(rmArgs, paths...)
		}
		_, err = r.Run(root, rmArgs...)
	}
	return err
}

// Discard restores a single changed file to HEAD (or removes an untracked file).
func Discard(r Runner, root string, c Change) error {
	if r == nil {
		r = LocalRunner{}
	}
	if root == "" {
		return errors.New("not a Git repository")
	}
	if c.Status == "U" {
		_, err := r.Run(root, "--literal-pathspecs", "clean", "-fd", "--", c.Path)
		return err
	}
	paths := changePaths(c)
	if c.Staged {
		_ = Unstage(r, root, paths...)
	}
	if c.Status == "A" {
		// A newly added file does not exist in HEAD; remove it from the working tree.
		_, err := r.Run(root, "--literal-pathspecs", "clean", "-fd", "--", c.Path)
		return err
	}
	_, err := r.Run(root, append([]string{"--literal-pathspecs", "checkout", "HEAD", "--"}, paths...)...)
	if err != nil && isUnbornHeadErr(err) {
		_, err = r.Run(root, append([]string{"--literal-pathspecs", "checkout", "--"}, paths...)...)
	}
	return err
}

// CreateCommit creates a commit from the staged index with message.
func CreateCommit(r Runner, root, message string) error {
	if r == nil {
		r = LocalRunner{}
	}
	if root == "" {
		return errors.New("not a Git repository")
	}
	msg := strings.TrimSpace(message)
	if msg == "" {
		return errors.New("enter a commit message")
	}
	_, err := r.Run(root, "commit", "-m", msg)
	return err
}

func changePaths(c Change) []string {
	if c.OldPath != "" && c.OldPath != c.Path {
		return []string{c.OldPath, c.Path}
	}
	return []string{c.Path}
}

func isUnbornHeadErr(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "ambiguous argument 'head'") ||
		strings.Contains(msg, "unknown revision") ||
		strings.Contains(msg, "failed to resolve 'head'") ||
		strings.Contains(msg, "bad revision")
}

// StagedCount returns how many changes in the working tree have staged index entries.
func (p *Page) StagedCount() int {
	n := 0
	for _, ch := range p.Changes {
		if ch.Staged {
			n++
		}
	}
	return n
}

func (p *Page) selectedChange() (Change, bool) {
	if p.Sel < 0 || p.Sel >= len(p.Changes) {
		return Change{}, false
	}
	return p.Changes[p.Sel], true
}

func (p *Page) runAction(reloadFiles bool, fn func(r Runner, root string) error, onSuccess func()) {
	h := p.h
	if h == nil || p.ActionBusy || p.Root == "" {
		return
	}
	p.runner = h.GitRunner()
	p.ActionBusy = true
	p.ActionErr = ""
	p.discardPrompt = ""
	p.ActionSeq++
	seq := p.ActionSeq
	root := p.Root
	runner := p.runner
	finish := func(err error) {
		if seq != p.ActionSeq {
			return
		}
		p.ActionBusy = false
		if err != nil {
			p.ActionErr = err.Error()
			h.Invalidate()
			return
		}
		if onSuccess != nil {
			onSuccess()
		}
		if reloadFiles {
			h.ReloadFiles()
		}
		h.AfterGitMutation()
		p.Refresh()
		h.Invalidate()
	}
	if h.Window() == nil {
		finish(fn(runner, root))
		return
	}
	win := h.Window()
	go func() {
		err := fn(runner, root)
		win.Update(func() { finish(err) })
	}()
}

// StageSelected stages the currently selected working-tree change.
func (p *Page) StageSelected() {
	ch, ok := p.selectedChange()
	if !ok {
		return
	}
	paths := changePaths(ch)
	p.runAction(false, func(r Runner, root string) error {
		return Stage(r, root, paths...)
	}, nil)
}

// UnstageSelected unstages the currently selected working-tree change.
func (p *Page) UnstageSelected() {
	ch, ok := p.selectedChange()
	if !ok {
		return
	}
	paths := changePaths(ch)
	p.runAction(false, func(r Runner, root string) error {
		return Unstage(r, root, paths...)
	}, nil)
}

// StageAll stages every change under the open project.
func (p *Page) StageAll() {
	pathspec := "."
	if p.Prefix != "" {
		pathspec = p.Prefix
	}
	p.runAction(false, func(r Runner, root string) error {
		return Stage(r, root, pathspec)
	}, nil)
}

// UnstageAll unstages every staged change under the open project.
func (p *Page) UnstageAll() {
	pathspec := "."
	if p.Prefix != "" {
		pathspec = p.Prefix
	}
	p.runAction(false, func(r Runner, root string) error {
		return Unstage(r, root, pathspec)
	}, nil)
}

// DiscardSelected discards working-tree changes for the selected file.
func (p *Page) DiscardSelected() {
	ch, ok := p.selectedChange()
	if !ok {
		return
	}
	p.runAction(true, func(r Runner, root string) error {
		return Discard(r, root, ch)
	}, nil)
}

// CommitStaged commits staged changes with p.CommitMsg.
func (p *Page) CommitStaged() {
	msg := strings.TrimSpace(p.CommitMsg)
	if msg == "" || p.StagedCount() == 0 {
		return
	}
	p.runAction(false, func(r Runner, root string) error {
		return CreateCommit(r, root, msg)
	}, func() {
		p.CommitMsg = ""
	})
}
