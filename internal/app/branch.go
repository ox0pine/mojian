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
	shown    bool // the menu was drawn open on the last pass
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
func (a *app) branchChip(c *ui.Context, k *colors, compact bool) {
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
		a.branch.shown = false
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
	m := ui.MenuBase(c, &a.branch.open)
	maxWidth := float32(210)
	if compact {
		maxWidth = 56
	}
	chip := titleChip(m.Trigger, k, a.branch.open).Label("Switch branch: " + name).
		Tooltip(a.tip("Branch "+name, cmdBranch)).MaxWidth(maxWidth)
	if m.Open() && !a.branch.shown {
		// Opened by the chip or a command: forget a name being typed
		// and read the branches again.
		a.beginBranchMenu()
	}
	a.branch.shown = m.Open()
	drop := float32(0)
	if a.branch.open {
		drop = 1
	}
	drop = chip.AnimateTo("branch-menu", drop, motion.PopoverIn)
	chip.Children(func() {
		ui.Icon(c, icons.Icon("git-branch")).Size(15, 15).TextColor(k.TextMuted)
		if !compact {
			ui.Text(c, name).FontSize(c.Theme().Rem(12.5 / 13)).FontWeight(600).TextColor(k.Text).
				SingleLine().Ellipsis("…").Shrink(1).MinWidth(0)
		}
		ui.Icon(c, icons.Icon("chevron-down")).Size(12, 12).TextColor(k.TextFaint)
	})
	m.Popup(func(panel ui.Element) {
		menuSurface(panel, k, drop)
		panel.Margin(4, 0, 0, 0)
		a.branchMenuBody(c, k, m)
	})
}

// beginBranchMenu prepares the menu the framework just opened: no name
// half typed, and the branches as they are now.
func (a *app) beginBranchMenu() {
	a.branch.creating = false
	a.branch.name = ""
	a.branch.err = ""
	a.refreshBranch()
}

// branchMenuBody draws the menu: the branches, or the new branch field.
func (a *app) branchMenuBody(c *ui.Context, k *colors, m ui.MenuParts) {
	d := a.branch.data
	if a.branch.creating {
		submit := false
		ui.Row(c).Padding(2, menuInset).Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Icon(c, icons.Icon("plus")).Size(15, 15).TextColor(k.TextMuted)
			in := ui.TextInputBase(c, &a.branch.name).Grow(1).FontSize(c.Theme().Rem(12.5 / 13)).
				Placeholder("New branch name").AutoFocus()
			submit = in.Submitted()
		})
		hint := "Creates a branch from " + d.Current
		if d.Detached {
			hint = "Creates a branch at " + d.Current
		}
		if a.branch.err != "" {
			menuNote(c, k, a.branch.err, true)
		} else {
			menuNote(c, k, hint, false)
		}
		if submit {
			a.createBranchAt(a.branch.name)
		}
		return
	}
	keys := a.commandKeys(cmdBranch)
	if keys == "None" {
		keys = ""
	}
	menuHeader(c, k, "Branches", keys)
	switch {
	case a.branch.loading && len(d.Branches) == 0:
		menuNote(c, k, "Reading branches…", false)
	case len(d.Branches) == 0:
		msg := "No branches"
		if d.Err != "" {
			msg = d.Err
		}
		menuNote(c, k, msg, d.Err != "")
	default:
		menuList(c, func() {
			for _, b := range d.Branches {
				a.branchMenuRow(c, k, m, b)
			}
		})
	}
	if a.branch.err != "" {
		menuNote(c, k, a.branch.err, true)
	}
	ui.Divider(c).Margin(6, 0)
	row := menuAction(c, k, m, "menu:new-branch", "plus", "New Branch…").KeepOpen()
	if row.Clicked() {
		a.branch.creating = true
		a.branch.name = ""
		a.branch.err = ""
	}
}

// branchMenuRow is one branch in the menu, checked when it is current.
func (a *app) branchMenuRow(c *ui.Context, k *colors, m ui.MenuParts, b gitpage.Branch) {
	row := menuChoice(c, k, m, "branch:"+b.Name, "git-branch", b.Name, b.Current).KeepOpen()
	if row.Clicked() {
		a.checkoutBranch(b.Name)
	}
}

// checkoutBranch switches to an existing branch, then reloads every clean
// pane from the files the branch holds.
func (a *app) checkoutBranch(name string) {
	if a.branch.loading {
		return
	}
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
	if a.branch.loading {
		return
	}
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
