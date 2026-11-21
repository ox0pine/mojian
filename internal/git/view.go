package git

import (
	"fmt"
	"path/filepath"
	"strings"

	"mojian/internal/icons"
	"mojian/internal/motion"
	"mojian/internal/theme"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
)

// View draws the working-tree diff in the editor area: changes on
// the left, the selected file's unified diff on the right.
func (p *Page) View(c *ui.Context, k *theme.Colors, h Host) {
	p.h = h
	card := ui.Column(c).Grow(1).MinWidth(0).MinHeight(0).Radius(theme.Radius).Clip().
		Background(k.CardFocused).Border(1, k.CardBorder).
		Shadow(0, 1, 2, 0, k.Shadow)
	card.Children(func() {
		p.header(c, k)
		ui.Divider(c)
		if p.pageState(c, k) {
			return
		}
		ui.Row(c).Grow(1).MinHeight(0).AlignItems(ui.Stretch).Children(func() {
			p.files(c, k)
			ui.Box(c).Width(1).FillHeight().Background(k.PanelBorder)
			p.diffPane(c, k)
		})
	})
}

// pageState draws the one state a page shows when it has no list to put
// beside the diff: a read error, a clean tree, an empty history, or the
// first read. It is deliberately above the two-column body, so the same
// error cannot appear in both columns.
func (p *Page) pageState(c *ui.Context, k *theme.Colors) bool {
	if p.Mode == ModeHistory {
		switch {
		case p.HistoryErr != "":
			p.state(c, k, "git-branch", k.Danger, "History unavailable", p.HistoryErr)
		case len(p.Commits) > 0:
			return false
		case p.HistoryLoad:
			p.state(c, k, "git-branch", k.TextFaint, "Reading history…", "")
		default:
			p.state(c, k, "git-branch", k.TextFaint, "No commits",
				"This project has no history yet.")
		}
		return true
	}
	switch {
	case p.Err != "" && len(p.Changes) == 0:
		p.state(c, k, "git-branch", k.Danger, "Changes unavailable", p.Err)
	case len(p.Changes) > 0:
		return false
	case p.Loading:
		p.state(c, k, "git-branch", k.TextFaint, "Reading changes…", "")
	default:
		p.state(c, k, "check", k.DiffAdd, "Working tree clean",
			"There are no changes to review.")
	}
	return true
}

func (p *Page) header(c *ui.Context, k *theme.Colors) {
	ui.Row(c).Height(headerH).Padding(0, 8, 0, 12).Gap(10).AlignItems(ui.Center).Children(func() {
		// One track holds the lists and the branch, as the title bar's
		// track holds the machine, project and branch chips.
		ui.Row(c).Height(28).Padding(2).Gap(4).Radius(theme.Radius).
			Background(k.Track).Border(0.5, k.TrackBorder).AlignItems(ui.Center).
			Children(func() {
				p.modeTab(c, k, "Changes", ModeChanges)
				p.modeTab(c, k, "History", ModeHistory)
				if p.Branch != "" {
					p.branchChip(c, k)
				}
			})
		ui.Spacer(c)
		if p.Loading || p.DiffLoad || p.HistoryLoad || p.ActionBusy {
			ui.Text(c, "Reading…").FontSize(c.Theme().Rem(11.0 / 13)).TextColor(k.TextFaint)
		}
		splitLabel, splitGlyph := "Split Diff", "columns-2"
		if p.SplitDiff {
			splitLabel, splitGlyph = "Unified Diff", "rows-2"
		}
		if iconButton(c, k, splitGlyph, splitLabel, 26, 16).
			Tooltip(splitLabel).Clicked() {
			p.SplitDiff = !p.SplitDiff
			p.h.Invalidate()
		}
		if iconButton(c, k, "refresh-cw", "Refresh", 26, 16).
			Tooltip("Refresh").Clicked() {
			p.Refresh()
		}
		if iconButton(c, k, "x", "Close "+p.Title(), 26, 16).
			Tooltip("Close").Clicked() {
			p.h.CloseGit()
			p.h.Invalidate()
		}
	})
}

// modeTab is one segment of the page's list switch.
func (p *Page) modeTab(c *ui.Context, k *theme.Colors, label string, mode PageMode) {
	active := p.Mode == mode
	b := ui.ButtonBase(c).Height(24).Padding(0, 9).Radius(theme.Radius - 2).
		Cursor(ui.CursorPointer).Label(label).Transition(motion.Colors)
	switch {
	case active:
		b.Background(k.TabActive)
		b.Shadow(0, 1, 2, 0, k.Shadow)
	case b.Hovered():
		b.Background(k.Pressed)
	default:
		b.Background(k.Hover)
	}
	if b.Clicked() {
		if mode == ModeHistory {
			p.ShowHistory()
		} else {
			p.ShowChanges()
		}
	}
	face, weight := k.TextMuted, 500
	if active {
		face, weight = k.Text, 600
	}
	b.Children(func() {
		ui.Text(c, label).FontSize(c.Theme().Rem(12.5 / 13)).FontWeight(weight).TextColor(face)
	})
}

// branchChip names the branch and opens the page's own menu under it: the
// same frame and type as the title bar's chip, so the three controls in
// this header read as one row.
func (p *Page) branchChip(c *ui.Context, k *theme.Colors) {
	m := ui.MenuBase(c, &p.BranchMenu)
	b := m.Trigger.Height(24).Padding(0, 8, 0, 6).Radius(theme.Radius - 2).
		Gap(6).Cursor(ui.CursorPointer).Label("Switch branch: " + p.Branch).
		Tooltip("Switch branch: " + p.Branch).MaxWidth(210).ClipX().
		Transition(motion.Colors)
	switch {
	case p.BranchMenu:
		b.Background(k.TabActive)
		b.Shadow(0, 1, 2, 0, k.Shadow)
	case b.Hovered():
		b.Background(k.Pressed)
	default:
		b.Background(k.Hover)
	}
	if m.Open() && !p.branchShown {
		// Opened by the chip or by the window: read the branches again,
		// since a terminal may have added or removed one.
		p.openBranchMenu()
	}
	p.branchShown = m.Open()
	drop := float32(0)
	if p.BranchMenu {
		drop = 1
	}
	drop = b.AnimateTo("git-branch-menu", drop, motion.PopoverIn)
	b.Children(func() {
		ui.Icon(c, icons.Icon("git-branch")).Size(15, 15).TextColor(k.TextMuted)
		ui.Text(c, p.Branch).FontSize(c.Theme().Rem(12.5 / 13)).FontWeight(600).TextColor(k.Text).
			SingleLine().Ellipsis("…").Shrink(1).MinWidth(0)
		ui.Icon(c, icons.Icon("chevron-down")).Size(12, 12).TextColor(k.TextFaint)
	})
	m.Popup(func(panel ui.Element) {
		panel.Width(branchMenuW).Padding(6).Radius(theme.Radius).
			Background(k.Panel).Border(1, k.PanelBorder).
			Shadow(0, 12, 32, 0, k.ShadowFocused).Gap(1).
			Translate(0, -4*(1-drop)).Opacity(0.45 + 0.55*drop)
		panel.Margin(4, 0, 0, 0)
		p.branchMenuBody(c, k, m)
	})
}

func (p *Page) files(c *ui.Context, k *theme.Colors) {
	width := float32(248)
	if w := c.Root().Bounds().W; w > 0 && w < 900 {
		width = 190
	}
	ui.Column(c).Width(width).Shrink(0).FillHeight().Children(func() {
		ui.Row(c).Height(headerH).Padding(0, 10, 0, 12).Gap(6).AlignItems(ui.Center).Children(func() {
			label, n := "WORKING TREE", len(p.Changes)
			if p.Mode == ModeHistory {
				label, n = "RECENT COMMITS", len(p.Commits)
			}
			ui.Text(c, label).FontSize(c.Theme().Rem(10.0 / 13)).FontWeight(650).TextColor(k.TextFaint)
			ui.Spacer(c)
			if p.Mode == ModeChanges && len(p.Changes) > 0 {
				if p.StagedCount() < len(p.Changes) {
					if iconButton(c, k, "plus", "Stage All", 22, 13).
						Tooltip("Stage All").Disabled(p.ActionBusy).Clicked() {
						p.StageAll()
					}
				}
				if p.StagedCount() > 0 {
					if iconButton(c, k, "x", "Unstage All", 22, 13).
						Tooltip("Unstage All").Disabled(p.ActionBusy).Clicked() {
						p.UnstageAll()
					}
				}
			}
			if n > 0 {
				ui.Text(c, fmt.Sprintf("%d", n)).FontSize(c.Theme().Rem(10.0/13)).TextColor(k.TextFaint).
					Padding(2, 5).Radius(theme.Radius).Background(k.Hover)
			}
		})
		ui.Divider(c)
		if p.Mode == ModeHistory {
			p.commits(c, k)
			return
		}
		p.list.Selected = &p.Sel
		p.list.Key = func(i int) any { return p.Changes[i].Display }
		p.list.MoveAnimation = motion.ListMove
		list := ui.List(c, &p.list, len(p.Changes), func(i int) {
			ch := p.Changes[i]
			row := ui.Row(c.Key("git:"+ch.Display)).Height(34).Padding(0, 10).
				Gap(10).Radius(theme.Radius).AlignItems(ui.Center).Cursor(ui.CursorPointer).
				Label(ch.Display).Tooltip(ch.Display + " — " + statusLabel(ch.Status)).
				PopoverMenu(func(m *ui.Menu) { p.h.FileMenu(m, ch.Display) })
			fg, muted, icon := k.Text, k.TextFaint, k.TextMuted
			if i == p.Sel {
				fg = c.Theme().AccentText
				muted, icon = fg.Alpha(0.9), fg.Alpha(0.9)
			} else if row.Hovered() {
				row.Background(k.Hover)
			}

			row.Children(func() {
				ui.Icon(c, icons.FileIcon(ch.Path)).Size(15, 15).TextColor(icon)
				ui.Column(c).Grow(1).MinWidth(0).Gap(1).Children(func() {
					ui.Text(c, filepath.Base(ch.Display)).FontSize(c.Theme().Rem(1)).FontWeight(500).
						TextColor(fg).SingleLine().Ellipsis("…")
					if dir := filepath.Dir(ch.Display); dir != "." {
						// The folder nearest the file is the useful half
						// of a path too long for the list.
						ui.Text(c, dir).FontSize(c.Theme().Rem(12.0 / 13)).TextColor(muted).
							SingleLine().TruncateStart()
					}
				})
				if ch.Staged {
					col := k.DiffAdd
					if i == p.Sel {
						col = fg
					}
					ui.Icon(c, icons.Icon("check")).Size(12, 12).TextColor(col).Tooltip("Staged")
				}
				p.statusBadge(c, k, ch.Status, i == p.Sel)
			})
		}).Grow(1).MinHeight(0).Padding(6, 7).Gap(6)
		if list.Changed() {
			p.discardPrompt = ""
			p.Select(p.Sel)
		}
		if list.Submitted() && p.Sel >= 0 && p.Sel < len(p.Changes) {
			if path := p.Changes[p.Sel].Display; p.h.HasPath(path) {
				p.h.OpenPath(path)
			}
		}
		if len(p.Changes) > 0 {
			p.commitBox(c, k)
		}
	})
}

func (p *Page) commitBox(c *ui.Context, k *theme.Colors) {
	staged := p.StagedCount()
	canCommit := staged > 0 && strings.TrimSpace(p.CommitMsg) != "" && !p.ActionBusy
	ui.Divider(c)
	ui.Column(c).Padding(8, 10).Gap(6).Children(func() {
		if p.ActionErr != "" {
			ui.Text(c, p.ActionErr).FontSize(c.Theme().Rem(11.0 / 13)).TextColor(k.Danger)
		}
		in := ui.TextInputBase(c.Key("git-commit-msg"), &p.CommitMsg).
			Height(30).Padding(0, 8).Radius(theme.Radius).
			Background(k.Hover).Border(1, k.PanelBorder).
			FontSize(c.Theme().Rem(12.0 / 13)).
			Placeholder("Commit message…").Label("Commit message")
		if in.Submitted() && canCommit {
			p.CommitStaged()
		}
		ui.Row(c).AlignItems(ui.Center).Gap(6).Children(func() {
			note := "Stage changes to commit"
			if staged == 1 {
				note = "1 staged"
			} else if staged > 1 {
				note = fmt.Sprintf("%d staged", staged)
			}
			ui.Text(c, note).FontSize(c.Theme().Rem(11.0 / 13)).TextColor(k.TextFaint).
				SingleLine().Ellipsis("…").Grow(1).MinWidth(0)
			btn := ui.ButtonBase(c).Height(26).Padding(0, 10).Radius(theme.Radius).
				Cursor(ui.CursorPointer).Label("Commit").Disabled(!canCommit).
				Transition(motion.Colors)
			if canCommit {
				btn.Background(c.Theme().Accent)
			} else {
				btn.Background(k.Hover)
			}
			if btn.Clicked() {
				p.CommitStaged()
			}
			fg := k.TextFaint
			if canCommit {
				fg = c.Theme().AccentText
			}
			btn.Children(func() {
				ui.Text(c, "Commit").FontSize(c.Theme().Rem(11.5 / 13)).FontWeight(600).TextColor(fg)
			})
		})
	})
}

func (p *Page) commits(c *ui.Context, k *theme.Colors) {
	p.historyList.Selected = &p.CommitSel
	p.historyList.Key = func(i int) any { return p.Commits[i].Hash }
	p.historyList.MoveAnimation = motion.ListMove
	list := ui.List(c, &p.historyList, len(p.Commits), func(i int) {
		commit := p.Commits[i]
		row := ui.Row(c.Key("git-commit:"+commit.Hash)).Height(34).Padding(0, 10).
			Gap(10).Radius(theme.Radius).AlignItems(ui.Center).Cursor(ui.CursorPointer).
			Label(commit.Subject).
			Tooltip(commit.Short + " · " + commit.Author + " · " + commit.When)
		fg, muted, icon := k.Text, k.TextFaint, k.TextMuted
		if i == p.CommitSel {
			fg = c.Theme().AccentText
			muted, icon = fg.Alpha(0.9), fg.Alpha(0.9)
		} else if row.Hovered() {
			row.Background(k.Hover)
		}
		row.Children(func() {
			ui.Icon(c, icons.Icon("git-commit")).Size(15, 15).TextColor(icon)
			ui.Column(c).Grow(1).MinWidth(0).Gap(1).Children(func() {
				ui.Text(c, commit.Subject).FontSize(c.Theme().Rem(1)).FontWeight(500).TextColor(fg).
					SingleLine().Ellipsis("…")
				ui.Text(c, commitMeta(commit)).FontSize(c.Theme().Rem(12.0 / 13)).TextColor(muted).
					SingleLine().Ellipsis("…")
			})
		})
	}).Grow(1).MinHeight(0).Padding(6, 7).Gap(6)
	if list.Changed() {
		p.selectCommit(p.CommitSel)
	}
}

// commitMeta is the quiet second line of a commit row: the hash people
// quote and when the commit landed.
func commitMeta(commit Commit) string {
	return commit.Short + " · " + commit.When
}

func statusLabel(status string) string {
	switch status {
	case "A":
		return "Added"
	case "D":
		return "Deleted"
	case "R":
		return "Renamed"
	case "C":
		return "Conflict"
	case "U":
		return "Untracked"
	default:
		return "Modified"
	}
}

func statusColor(status string, k *theme.Colors) ui.Color {
	switch status {
	case "A", "U":
		return k.DiffAdd
	case "D":
		return k.DiffDel
	case "R", "C":
		return k.DiffHunk
	default:
		return k.Attention
	}
}

func (p *Page) changeStatus() string {
	if p.Sel >= 0 && p.Sel < len(p.Changes) {
		return p.Changes[p.Sel].Status
	}
	return ""
}

func (p *Page) statusBadge(c *ui.Context, k *theme.Colors, status string, selected bool) {
	col := statusColor(status, k)
	fg, bg := col, col.Alpha(0.11)
	if selected {
		// The accent fill replaces the page's background, so a status
		// tint would disappear into it. Take the row's own text color and
		// a translucent chip instead.
		fg = c.Theme().AccentText
		bg = fg.Alpha(0.16)
	}
	ui.Text(c, statusLabel(status)).FontSize(c.Theme().Rem(10.0/13)).FontWeight(700).
		TextColor(fg).Padding(2, 5).Radius(theme.Radius).Background(bg).
		Shrink(0).Tooltip(statusLabel(status))
}

func (p *Page) diffPane(c *ui.Context, k *theme.Colors) {
	ui.Column(c).Grow(1).MinWidth(0).FillHeight().Children(func() {
		p.diffHeader(c, k)
		ui.Divider(c)
		p.diffBody(c, k)
	})
}

func (p *Page) diffHeader(c *ui.Context, k *theme.Colors) {
	ui.Row(c).Height(headerH).Padding(0, 10, 0, 12).Gap(7).AlignItems(ui.Center).Children(func() {
		if p.Mode == ModeHistory {
			p.commitHeader(c, k)
			return
		}
		path := p.DiffPath
		if path == "" {
			ui.Text(c, "No file selected").FontSize(c.Theme().Rem(12.0 / 13)).TextColor(k.TextMuted)
			ui.Spacer(c)
			return
		}
		ui.Icon(c, icons.FileIcon(path)).Size(15, 15).TextColor(k.TextMuted)
		ui.Text(c, filepath.Base(path)).FontSize(c.Theme().Rem(1)).FontWeight(500).TextColor(k.Text).
			SingleLine().Ellipsis("…").Shrink(0.3).MinWidth(0)
		if dir := filepath.Dir(path); dir != "." {
			ui.Text(c, dir).FontSize(c.Theme().Rem(12.0 / 13)).TextColor(k.TextFaint).
				SingleLine().Ellipsis("…").Shrink(1).MinWidth(0)
		}
		ui.Spacer(c)
		if status := p.changeStatus(); status != "" {
			p.statusBadge(c, k, status, false)
		}
		if p.diffAdd > 0 || p.diffDel > 0 {
			ui.Text(c, fmt.Sprintf("+%d", p.diffAdd)).FontSize(c.Theme().Rem(11.0 / 13)).FontWeight(600).
				TextColor(k.DiffAdd).Shrink(0)
			ui.Text(c, fmt.Sprintf("−%d", p.diffDel)).FontSize(c.Theme().Rem(11.0 / 13)).FontWeight(600).
				TextColor(k.DiffDel).Shrink(0)
		}
		if ch, ok := p.selectedChange(); ok {
			if !ch.Staged || ch.Unstaged {
				if headerActionButton(c, k, "Stage", false, p.ActionBusy).Clicked() {
					p.StageSelected()
				}
			}
			if ch.Staged {
				if headerActionButton(c, k, "Unstage", false, p.ActionBusy).Clicked() {
					p.UnstageSelected()
				}
			}
			if p.discardPrompt == ch.Path {
				if headerActionButton(c, k, "Confirm Discard", true, p.ActionBusy).Clicked() {
					p.DiscardSelected()
				}
				if headerActionButton(c, k, "Cancel", false, p.ActionBusy).Clicked() {
					p.discardPrompt = ""
				}
			} else {
				if headerActionButton(c, k, "Discard", false, p.ActionBusy).Clicked() {
					p.discardPrompt = ch.Path
				}
			}
		}
		if p.h.HasPath(path) {
			if headerActionButton(c, k, "Open", false, false).Label("Open File").Clicked() {
				p.h.OpenPath(path)
			}
		}
	})
}

func headerActionButton(c *ui.Context, k *theme.Colors, text string, danger, disabled bool) ui.Element {
	b := ui.ButtonBase(c).Padding(4, 9).Radius(theme.Radius).
		Cursor(ui.CursorPointer).Label(text).Disabled(disabled).
		Transition(motion.Colors)
	fg := k.Text
	if danger {
		fg = k.Danger
		b.Background(k.Danger.Alpha(0.12))
	}
	if b.Hovered() {
		if danger {
			b.Background(k.Danger.Alpha(0.2))
		} else {
			b.Background(k.Hover)
		}
	}
	b.Children(func() {
		ui.Text(c, text).FontSize(c.Theme().Rem(12.0 / 13)).FontWeight(500).TextColor(fg)
	})
	return b
}

// commitHeader names the commit whose patch the diff pane shows.
func (p *Page) commitHeader(c *ui.Context, k *theme.Colors) {
	commit, ok := p.selectedCommit()
	if !ok {
		ui.Text(c, "No commit selected").FontSize(c.Theme().Rem(12.0 / 13)).TextColor(k.TextMuted)
		ui.Spacer(c)
		return
	}
	ui.Text(c, commit.Short).Font(theme.NoteFont.Family).FontSize(c.Theme().Rem(11.0/13)).
		TextColor(k.TextMuted).Padding(2, 6).Radius(theme.Radius).Background(k.Hover)
	ui.Text(c, commit.Subject).FontSize(c.Theme().Rem(1)).FontWeight(500).TextColor(k.Text).
		SingleLine().Ellipsis("…").Shrink(1).MinWidth(0)
	ui.Spacer(c)
	if p.diffAdd > 0 || p.diffDel > 0 {
		ui.Text(c, fmt.Sprintf("+%d", p.diffAdd)).FontSize(c.Theme().Rem(11.0 / 13)).FontWeight(600).
			TextColor(k.DiffAdd).Shrink(0)
		ui.Text(c, fmt.Sprintf("−%d", p.diffDel)).FontSize(c.Theme().Rem(11.0 / 13)).FontWeight(600).
			TextColor(k.DiffDel).Shrink(0)
	}
}

func (p *Page) diffBody(c *ui.Context, k *theme.Colors) {
	key := "diff:" + p.DiffPath
	if p.Mode == ModeHistory {
		key = "diff:commit"
		if commit, ok := p.selectedCommit(); ok {
			key += ":" + commit.Hash
		}
	}
	ui.Box(c.Key(key)).Grow(1).MinWidth(0).MinHeight(0).Clip().
		Transition(motion.Page).Children(func() { p.diffState(c, k) })
}

func (p *Page) diffState(c *ui.Context, k *theme.Colors) {
	if p.Mode == ModeHistory {
		p.commitState(c, k)
		return
	}
	switch {
	case p.Err != "":
		p.state(c, k, "git-branch", k.Danger, "Changes unavailable", p.Err)
	case len(p.Changes) == 0 && !p.Loading:
		p.state(c, k, "check", k.DiffAdd, "Working tree clean",
			"There are no changes to review.")
	case p.DiffLoad:
		p.state(c, k, "git-branch", k.TextFaint, "Reading diff…", "")
	case p.DiffPath == "":
		p.state(c, k, "git-branch", k.TextFaint, "Select a change",
			"Pick a changed file on the left to see its diff.")
	case p.DiffErr != "":
		p.state(c, k, "git-branch", k.Danger, "Could not read this diff", p.DiffErr)
	case strings.TrimSpace(p.Diff) == "":
		p.state(c, k, "file-text", k.TextFaint, "No text changes",
			"This file has no textual diff.")
	default:
		p.buildDiffViews(c)
		if p.diffView == nil {
			detail := strings.TrimSpace(p.Diff)
			if detail == "" {
				detail = "This file has no textual diff."
			}
			p.state(c, k, "file-text", k.TextFaint, "No text changes", detail)
			return
		}
		p.styleDiffView(c, p.diffView)
		editor.DiffView(c, p.diffView).Grow(1).FillWidth()
	}
}

func (p *Page) commitState(c *ui.Context, k *theme.Colors) {
	switch {
	case p.HistoryErr != "":
		p.state(c, k, "git-branch", k.Danger, "History unavailable", p.HistoryErr)
	case p.HistoryLoad && len(p.Commits) == 0:
		p.state(c, k, "git-branch", k.TextFaint, "Reading history…", "")
	case p.CommitSel < 0 || p.CommitSel >= len(p.Commits):
		p.state(c, k, "git-branch", k.TextFaint, "Select a commit",
			"Pick a commit on the left to see what it changed.")
	case p.DiffLoad:
		p.state(c, k, "git-branch", k.TextFaint, "Reading diff…", "")
	case p.DiffErr != "":
		p.state(c, k, "git-branch", k.Danger, "Could not read this commit", p.DiffErr)
	case len(p.patch) == 0:
		p.state(c, k, "file-text", k.TextFaint, "Nothing to show",
			"A merge commit, or one that changed no files, leaves no patch.")
	default:
		p.diffPatch(c, k)
	}
}

func (p *Page) state(c *ui.Context, k *theme.Colors, glyph string, col ui.Color, title, detail string) {
	ui.Column(c).Fill().Center().Padding(20).Gap(8).Children(func() {
		ui.Box(c).Size(40, 40).Radius(theme.Radius).Background(col.Alpha(0.10)).Center().Children(func() {
			ui.Icon(c, icons.Icon(glyph)).Size(20, 20).TextColor(col)
		})
		ui.Text(c, title).FontSize(c.Theme().Rem(1)).FontWeight(600).TextColor(k.Text).
			TextAlign(ui.Center)
		if detail != "" {
			ui.Text(c, detail).FontSize(c.Theme().Rem(12.0 / 13)).TextColor(k.TextFaint).
				TextAlign(ui.Center).MaxWidth(460)
		}
	})
}

// buildDiffViews makes the viewer states of the loaded diff, once per
// diff: the frame that first draws one has the window's theme and font.
func (p *Page) buildDiffViews(c *ui.Context) {
	if p.viewsGen == p.diffGen {
		return
	}
	p.viewsGen = p.diffGen
	p.diffView, p.patchViews = nil, nil
	p.patchCut = false
	if p.Mode == ModeHistory {
		p.buildPatchViews(c)
		return
	}
	if files := SplitDiff(p.Diff); len(files) > 0 {
		p.diffView = p.newDiffView(c, files[0], false)
	}
}

// buildPatchViews makes one viewer per file a commit touched, stopping
// at the line budget on a hunk boundary.
func (p *Page) buildPatchViews(c *ui.Context) {
	const maxLines = 2500
	left := maxLines
	for _, file := range p.patch {
		if left <= 0 {
			p.patchCut = true
			break
		}
		if len(file.Lines) > left {
			file = cutFileDiff(file, left)
			p.patchCut = true
		}
		left -= len(file.Lines)
		p.patchViews = append(p.patchViews, p.newDiffView(c, file, true))
	}
}

// cutFileDiff keeps the whole hunks of f that fit in n lines.
func cutFileDiff(f FileDiff, n int) FileDiff {
	used := 0
	out := f
	out.Lines = nil
	for i := 0; i < len(f.Lines); {
		j := i + 1
		for j < len(f.Lines) && f.Lines[j].kind != '@' {
			j++
		}
		if used > 0 && used+j-i > n {
			break
		}
		out.Lines = append(out.Lines, f.Lines[i:j]...)
		used += j - i
		i = j
	}
	return out
}

// newDiffView configures one viewer with the page's editor look.
func (p *Page) newDiffView(c *ui.Context, f FileDiff, fit bool) *editor.DiffState {
	patch := toFilePatch(f)
	if len(patch.Hunks) == 0 {
		return nil
	}
	font, lineHeight, tabSize := p.h.EditorFont()
	layout := editor.DiffUnified
	if p.SplitDiff {
		layout = editor.DiffSplit
	}
	d := &editor.DiffState{
		Layout:         layout,
		Wrap:           true,
		Inline:         editor.DiffInlineWordAlt,
		Indicators:     editor.DiffClassic,
		KeepScrollbars: true,
		FitContent:     fit,
		Font:           font,
		LineHeight:     lineHeight,
		TabSize:        tabSize,
	}
	d.SetPatch(patch)
	return d
}

// styleDiffView follows the window's theme and font, so a theme switch
// recolors a diff already built.
func (p *Page) styleDiffView(c *ui.Context, d *editor.DiffState) {
	font, lineHeight, tabSize := p.h.EditorFont()
	d.Theme = p.h.EditorTheme(c)
	d.Font, d.LineHeight, d.TabSize = font, lineHeight, tabSize
	if p.SplitDiff {
		d.Layout = editor.DiffSplit
	} else {
		d.Layout = editor.DiffUnified
	}
}

// diffPatch draws every file a commit touched, each under its own
// heading, so a commit that changed several files stays readable.
func (p *Page) diffPatch(c *ui.Context, k *theme.Colors) {
	p.buildDiffViews(c)
	scroll := ui.Scroll(c).Grow(1).MinHeight(0).Padding(0, 0, 4, 0).
		TrackScroll(&p.DiffScroll)
	scroll.Children(func() {
		for i, file := range p.patch {
			if i >= len(p.patchViews) {
				break
			}
			p.fileHeading(c, k, file)
			if p.patchViews[i] == nil {
				note := file.Note
				if note == "" {
					note = "This file has no textual diff."
				}
				ui.Text(c, note).FontSize(c.Theme().Rem(12.0/13)).TextColor(k.TextFaint).Padding(6, 10)
				continue
			}
			p.styleDiffView(c, p.patchViews[i])
			editor.DiffView(c, p.patchViews[i]).FillWidth()
		}
		if p.patchCut {
			ui.Text(c, "… diff truncated").FontSize(c.Theme().Rem(11.0/13)).TextColor(k.TextFaint).Padding(6, 10)
		}
	})
}

// fileHeading names one file inside a commit's patch.
func (p *Page) fileHeading(c *ui.Context, k *theme.Colors, file FileDiff) {
	ui.Row(c).FillWidth().Padding(6, 10).Gap(7).AlignItems(ui.Center).
		Background(k.Hover).Children(func() {
		ui.Icon(c, icons.FileIcon(file.Path)).Size(13, 13).TextColor(k.TextMuted)
		ui.Text(c, filepath.Base(file.Path)).FontSize(c.Theme().Rem(12.0 / 13)).FontWeight(600).
			TextColor(k.Text).SingleLine().Ellipsis("…").Shrink(0.3).MinWidth(0)
		if dir := filepath.Dir(file.Path); dir != "." {
			ui.Text(c, dir).FontSize(c.Theme().Rem(11.5 / 13)).TextColor(k.TextFaint).
				SingleLine().Ellipsis("…").Shrink(1).MinWidth(0)
		}
		ui.Spacer(c)
		if file.Add > 0 || file.Del > 0 {
			ui.Text(c, fmt.Sprintf("+%d", file.Add)).FontSize(c.Theme().Rem(10.5 / 13)).FontWeight(600).
				TextColor(k.DiffAdd).Shrink(0)
			ui.Text(c, fmt.Sprintf("−%d", file.Del)).FontSize(c.Theme().Rem(10.5 / 13)).FontWeight(600).
				TextColor(k.DiffDel).Shrink(0)
		}
		if file.Status != "M" {
			p.statusBadge(c, k, file.Status, false)
		}
	})
}

// diffLineRunes bounds one patch line. A file Git reads as text but is
// really binary, such as an archive or a packed blob whose first
// kilobytes held no NUL, arrives as one line of megabytes; shaping it
// costs the square of its length, which stalled the window for seconds.
// The adapter cuts it once, when it builds the patch.
const diffLineRunes = 2000

// clipRunes keeps at most max runes of s, ending the cut with an ellipsis.
func clipRunes(s string, max int) string {
	n := 0
	for i := range s {
		if n == max {
			return s[:i] + "…"
		}
		n++
	}
	return s
}

// hasProject reports whether the host has a project on disk.
func hasProject(h Host) bool {
	_, ok := h.ProjectDir()
	return ok
}

// mustDir returns the project directory, or "".
func mustDir(h Host) string {
	dir, _ := h.ProjectDir()
	return dir
}

// iconButton is a borderless icon button of the page header.
func iconButton(c *ui.Context, k *theme.Colors, name, label string, size, iconSize float32) ui.Element {
	b := ui.ButtonBase(c).Size(size, size).Radius(theme.Radius).
		Cursor(ui.CursorPointer).Label(label).Transition(motion.Colors)
	if b.Pressed() {
		b.Background(k.Pressed)
	} else if b.Hovered() {
		b.Background(k.Hover)
	}
	b.Children(func() {
		col := k.IconMuted
		if b.Hovered() {
			col = k.Text
		}
		ui.Icon(c, icons.Icon(name)).Size(iconSize, iconSize).TextColor(col)
	})
	return b
}
