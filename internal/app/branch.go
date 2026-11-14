package app

import (
	"errors"
	"fmt"
	"strings"

	gitpage "mojian/internal/git"
	"mojian/internal/icons"
	"mojian/internal/motion"

	"github.com/egoist/mygo/ui"
)

// branchState is the branch a project has checked out and the menu that
// switches it. It lives on the workspace, so every project keeps its own.
type branchState struct {
	data     gitpage.BranchData
	loading  bool
	loaded   bool
	seq      int
	queued   bool
	open     bool
	creating bool
	name     string
	err      string
}

// refreshBranch reads the project's repository and branches in the
// background and keeps the chip in step with them.
func (a *app) refreshBranch() {
	if a.store == nil || a.store.IsMemory() || a.branch.loading {
		return
	}
	dir, ok := a.ProjectDir()
	if !ok {
		return
	}
	runner := a.GitRunner()
	a.branch.seq++
	seq := a.branch.seq
	a.branch.loading = true
	apply := func(d gitpage.BranchData) {
		if seq != a.branch.seq {
			return
		}
		a.branch.loading = false
		a.branch.loaded = true
		a.branch.data = d
		a.invalidate()
	}
	if a.win == nil {
		apply(gitpage.ReadBranches(runner, dir))
		return
	}
	win := a.win
	go func() {
		d := gitpage.ReadBranches(runner, dir)
		win.Update(func() { apply(d) })
	}()
}

// openBranchPicker opens the branch menu under the chip.
func (a *app) openBranchPicker() {
	if a.store == nil || a.store.IsMemory() {
		return
	}
	a.closeModals()
	a.branch.creating = false
	a.branch.name = ""
	a.branch.err = ""
	if a.branch.loaded && a.branch.data.Root == "" {
		// There is no chip to open under; say why the command did
		// nothing instead of leaving the menu invisible.
		msg := a.branch.data.Err
		if msg == "" {
			msg = "This folder is not a Git repository."
		}
		a.branch.err = msg
		a.fail(errors.New(msg))
		return
	}
	a.branch.open = true
	a.refreshBranch()
	a.invalidate()
}

// branchChip is the branch name in the title bar, right of the project,
// and the menu that switches it.
func (a *app) branchChip(c *ui.Context, k *colors) {
	if a.store == nil || a.store.IsMemory() {
		return
	}
	if !a.branch.loaded {
		// Read once per project, after the frame, so the first paint
		// never waits on Git.
		if !a.branch.loading && !a.branch.queued {
			a.branch.queued = true
			a.later(a.refreshBranch)
		}
		return
	}
	d := a.branch.data
	if d.Root == "" {
		// No repository and so no chip: a command that opened the menu
		// says why it stayed empty.
		if a.branch.open {
			a.branch.open = false
			msg := d.Err
			if msg == "" {
				msg = "This folder is not a Git repository."
			}
			a.fail(errors.New(msg))
		}
		return
	}
	name := d.Current
	if d.Detached {
		name = "detached at " + d.Current
	}
	chip := ui.ButtonBase(c).Gap(6).Padding(2, 8, 2, 6).Radius(radius).
		Justify(ui.Start).Cursor(ui.CursorPointer).Label("Switch branch: " + name).
		Tooltip("Branch " + name + "  " + nativeKeys("⌘⇧B")).MaxWidth(220).Shrink(0).ClipX().
		Transition(motion.Colors)
	if chip.Hovered() || a.branch.open {
		chip.Background(k.Hover)
	}
	drop := float32(0)
	if a.branch.open {
		drop = 1
	}
	drop = chip.AnimateTo("branch-menu", drop, motion.PopoverIn)
	if chip.Clicked() {
		if a.branch.open {
			a.branch.open = false
		} else {
			a.openBranchPicker()
		}
	}
	chip.Children(func() {
		ui.Icon(c, icons.Icon("git-branch")).Size(14, 14).TextColor(k.TextMuted)
		ui.Text(c, name).FontSize(12.5).TextColor(k.Text.Alpha(0.9)).
			SingleLine().Ellipsis("…").Shrink(1).MinWidth(0)
		ui.Icon(c, icons.Icon("chevron-down")).Size(12, 12).TextColor(k.TextFaint)
	})
	ui.PopoverBase(c, chip, &a.branch.open, func(panel ui.Element) {
		panel.Width(272).Padding(6).Radius(radius).Background(k.Panel).Border(1, k.PanelBorder).
			Shadow(0, 12, 32, 0, k.ShadowFocused).Gap(1).
			Translate(0, -4*(1-drop)).Opacity(0.45 + 0.55*drop)
		a.branchMenuBody(c, k)
	})
}

// branchMenuBody draws the menu: the branches, or the new branch field.
func (a *app) branchMenuBody(c *ui.Context, k *colors) {
	d := a.branch.data
	if a.branch.creating {
		submit := false
		ui.Row(c).Padding(2, 6).Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Icon(c, icons.Icon("plus")).Size(15, 15).TextColor(k.TextMuted)
			in := ui.TextInputBase(c, &a.branch.name).Grow(1).FontSize(13).
				Placeholder("New branch name").AutoFocus()
			submit = in.Submitted()
		})
		hint := "Creates a branch from " + d.Current
		if d.Detached {
			hint = "Creates a branch at " + d.Current
		}
		face := k.TextFaint
		if a.branch.err != "" {
			hint, face = a.branch.err, k.Danger
		}
		ui.Text(c, hint).FontSize(11.5).TextColor(face).Padding(0, 8, 6, 8)
		if submit {
			a.createBranchAt(a.branch.name)
		}
		return
	}
	ui.Row(c).Padding(4, 8, 5, 8).AlignItems(ui.Center).Children(func() {
		ui.Text(c, "Branches").FontSize(11).FontWeight(600).TextColor(k.TextFaint)
		ui.Spacer(c)
		ui.Text(c, nativeKeys("⌘⇧B")).FontSize(11).TextColor(k.TextFaint)
	})
	switch {
	case a.branch.loading && len(d.Branches) == 0:
		ui.Text(c, "Reading branches…").FontSize(12.5).TextColor(k.TextFaint).Padding(4, 8)
	case len(d.Branches) == 0:
		msg := "No branches"
		if d.Err != "" {
			msg = d.Err
		}
		ui.Text(c, msg).FontSize(12.5).TextColor(k.TextFaint).Padding(4, 8)
	default:
		ui.Scroll(c).MaxHeight(300).Children(func() {
			for _, b := range d.Branches {
				a.branchMenuRow(c, k, b)
			}
		})
	}
	if a.branch.err != "" {
		ui.Text(c, a.branch.err).FontSize(11.5).TextColor(k.Danger).Padding(4, 8)
	}
	ui.Divider(c).Margin(6, 0)
	row := ui.ButtonBase(c).Height(32).Padding(0, 8).Gap(8).Radius(radius).
		Justify(ui.Start).Cursor(ui.CursorPointer).Label("New Branch…")
	if row.Hovered() {
		row.Background(k.Hover)
	}
	if row.Clicked() {
		a.branch.creating = true
		a.branch.name = ""
		a.branch.err = ""
	}
	row.Children(func() {
		ui.Icon(c, icons.Icon("plus")).Size(15, 15).TextColor(k.TextMuted)
		ui.Text(c, "New Branch…").FontSize(12.5).TextColor(k.Text)
	})
}

// branchMenuRow is one branch in the menu, checked when it is current.
func (a *app) branchMenuRow(c *ui.Context, k *colors, b gitpage.Branch) {
	row := ui.ButtonBase(c).Height(30).Padding(0, 8).Gap(8).Radius(radius).
		Justify(ui.Start).Cursor(ui.CursorPointer).Label(b.Name)
	if row.Hovered() {
		row.Background(k.Hover)
	}
	if row.Clicked() {
		a.branch.open = false
		a.branch.creating = false
		a.checkoutBranch(b.Name)
	}
	row.Children(func() {
		if b.Current {
			ui.Icon(c, icons.Icon("check")).Size(14, 14).TextColor(k.TextMuted)
		} else {
			ui.Icon(c, icons.Icon("git-branch")).Size(14, 14).TextColor(k.TextFaint)
		}
		weight := 400
		if b.Current {
			weight = 600
		}
		ui.Text(c, b.Name).FontSize(12.5).FontWeight(weight).TextColor(k.Text).
			SingleLine().Ellipsis("…").Grow(1).MinWidth(0)
	})
}

// checkoutBranch switches to an existing branch, then reloads every clean
// pane from the files the branch holds.
func (a *app) checkoutBranch(name string) {
	root := a.branchRoot()
	if root == "" {
		return
	}
	a.runBranchChange("switching to "+name, func(r gitpage.Runner) error {
		return gitpage.SwitchBranch(r, root, name)
	})
}

// createBranchAt creates the typed branch at HEAD and switches to it.
func (a *app) createBranchAt(name string) {
	name = strings.TrimSpace(name)
	if name == "" {
		a.branch.err = "Type a branch name."
		return
	}
	root := a.branchRoot()
	if root == "" {
		return
	}
	a.runBranchChange("creating "+name, func(r gitpage.Runner) error {
		return gitpage.CreateBranch(r, root, name)
	})
}

// branchRoot refuses a change while notes hold unsaved edits, since
// checkout would overwrite files those edits belong to.
func (a *app) branchRoot() string {
	if root := a.branch.data.Root; root == "" {
		err := errors.New("This folder is not a Git repository.")
		a.branch.err = err.Error()
		a.fail(err)
		return ""
	}
	if n := a.dirtyNotes(); n > 0 {
		err := errors.New("Save or discard your edits before switching branches.")
		a.branch.err = err.Error()
		a.fail(err)
		return ""
	}
	a.branch.err = ""
	return a.branch.data.Root
}

// runBranchChange runs a checkout off the UI thread and applies the result.
func (a *app) runBranchChange(what string, fn func(gitpage.Runner) error) {
	runner := a.GitRunner()
	store := a.store
	a.branch.seq++
	seq := a.branch.seq
	a.branch.loading = true
	a.branch.open = false
	a.branch.creating = false
	a.branch.name = ""
	a.invalidate()
	apply := func(err error) {
		if seq != a.branch.seq {
			return
		}
		a.branch.loading = false
		if err != nil {
			a.fail(fmt.Errorf("%s: %w", what, err))
			a.invalidate()
			return
		}
		if err := store.Reload(); err != nil {
			a.fail(err)
		}
		if a.git.Open {
			a.refreshGit()
		}
		a.refreshBranch()
		a.invalidate()
	}
	if a.win == nil {
		apply(fn(runner))
		return
	}
	win := a.win
	go func() {
		err := fn(runner)
		win.Update(func() { apply(err) })
	}()
}

// dirtyNotes counts the notes whose text only exists in memory.
func (a *app) dirtyNotes() int {
	if a.store == nil {
		return 0
	}
	n := 0
	for _, note := range a.store.Notes {
		if note.Dirty() {
			n++
		}
	}
	return n
}
