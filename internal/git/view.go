package git

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"mojian/internal/icons"
	"mojian/internal/motion"
	"mojian/internal/theme"

	"github.com/egoist/mygo/ui"
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
		ui.Row(c).Grow(1).MinHeight(0).AlignItems(ui.Stretch).Children(func() {
			p.files(c, k)
			ui.Box(c).Width(1).FillHeight().Background(k.PanelBorder)
			p.diffPane(c, k)
		})
	})
}

func (p *Page) header(c *ui.Context, k *theme.Colors) {
	ui.Row(c).Height(headerH).Padding(0, 8, 0, 12).Gap(10).AlignItems(ui.Center).Children(func() {
		// One track holds the lists and the branch, as the title bar's
		// track holds the machine, project and branch chips.
		ui.Row(c).Height(28).Gap(4).Radius(theme.Radius).
			Background(k.Track).Border(0.5, k.TrackBorder).AlignItems(ui.Center).
			Children(func() {
				p.modeTab(c, k, "Changes", ModeChanges)
				p.modeTab(c, k, "History", ModeHistory)
				if p.Branch != "" {
					p.branchChip(c, k)
				}
			})
		if p.Mode == ModeHistory {
			if n := len(p.Commits); n > 0 {
				ui.Text(c, commitCount(n)).FontSize(11).TextColor(k.TextFaint)
			}
		} else if n := len(p.Changes); n > 0 {
			ui.Text(c, fileCount(n)).FontSize(11).TextColor(k.TextFaint)
		}
		ui.Spacer(c)
		if p.Loading || p.DiffLoad || p.HistoryLoad {
			ui.Text(c, "Reading…").FontSize(11).TextColor(k.TextFaint)
		}
		if iconButton(c, k, "refresh-cw", "Refresh", 26, 16).
			Tooltip("Refresh").Clicked() {
			p.Refresh()
		}
		if iconButton(c, k, "x", "Close Changes", 26, 16).Clicked() {
			p.Open = false
			p.h.Invalidate()
		}
	})
}

// modeTab is one segment of the page's list switch.
func (p *Page) modeTab(c *ui.Context, k *theme.Colors, label string, mode PageMode) {
	active := p.Mode == mode
	b := ui.ButtonBase(c).Height(28).Padding(0, 9).Radius(theme.Radius).
		Cursor(ui.CursorPointer).Label(label)
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
		ui.Text(c, label).FontSize(12.5).FontWeight(weight).TextColor(face)
	})
}

// branchChip names the branch and opens the page's own menu under it: the
// same frame and type as the title bar's chip, so the three controls in
// this header read as one row.
func (p *Page) branchChip(c *ui.Context, k *theme.Colors) {
	b := ui.ButtonBase(c).Height(28).Padding(0, 8, 0, 6).Radius(theme.Radius).
		Gap(6).Cursor(ui.CursorPointer).Label("Switch branch: " + p.Branch).
		Tooltip("Switch branch: " + p.Branch)
	switch {
	case p.BranchMenu:
		b.Background(k.TabActive)
		b.Shadow(0, 1, 2, 0, k.Shadow)
	case b.Hovered():
		b.Background(k.Pressed)
	default:
		b.Background(k.Hover)
	}
	if b.Clicked() {
		p.ToggleBranchMenu()
	}
	drop := float32(0)
	if p.BranchMenu {
		drop = 1
	}
	drop = b.AnimateTo("git-branch-menu", drop, motion.PopoverIn)
	b.Children(func() {
		ui.Icon(c, icons.Icon("git-branch")).Size(15, 15).TextColor(k.TextMuted)
		ui.Text(c, p.Branch).FontSize(12.5).FontWeight(600).TextColor(k.Text).
			SingleLine().Ellipsis("…").Shrink(1).MinWidth(0)
		ui.Icon(c, icons.Icon("chevron-down")).Size(12, 12).TextColor(k.TextFaint)
	})
	ui.PopoverBase(c, b, &p.BranchMenu, func(panel ui.Element) {
		panel.Width(branchMenuW).Padding(6).Radius(theme.Radius).
			Background(k.Panel).Border(1, k.PanelBorder).
			Shadow(0, 12, 32, 0, k.ShadowFocused).Gap(1).Clip().
			Translate(0, -4*(1-drop)).Opacity(0.45 + 0.55*drop)
		p.branchMenuBody(c, k)
	})
}

func fileCount(n int) string {
	if n == 1 {
		return "1 file"
	}
	return fmt.Sprintf("%d files", n)
}

func commitCount(n int) string {
	if n == 1 {
		return "1 commit"
	}
	return fmt.Sprintf("%d commits", n)
}

func (p *Page) files(c *ui.Context, k *theme.Colors) {
	ui.Column(c).Width(248).Shrink(0).FillHeight().Children(func() {
		ui.Row(c).Height(headerH).Padding(0, 10, 0, 12).Gap(6).AlignItems(ui.Center).Children(func() {
			label, n := "WORKING TREE", len(p.Changes)
			if p.Mode == ModeHistory {
				label, n = "RECENT COMMITS", len(p.Commits)
			}
			ui.Text(c, label).FontSize(10).FontWeight(650).TextColor(k.TextFaint)
			ui.Spacer(c)
			if n > 0 {
				ui.Text(c, fmt.Sprintf("%d", n)).FontSize(10).TextColor(k.TextFaint).
					Padding(2, 5).Radius(theme.Radius).Background(k.Hover)
			}
		})
		ui.Divider(c)
		if p.Mode == ModeHistory {
			p.commits(c, k)
			return
		}
		if p.Err != "" {
			ui.Column(c).Fill().Center().Padding(16).Gap(8).Children(func() {
				ui.Icon(c, icons.Icon("git-branch")).Size(20, 20).TextColor(k.TextFaint)
				ui.Text(c, "Changes unavailable").FontSize(13).FontWeight(600).
					TextColor(k.Text).TextAlign(ui.Center)
				ui.Text(c, p.Err).FontSize(12).TextColor(k.TextFaint).
					TextAlign(ui.Center).MaxWidth(210)
			})
			return
		}
		if len(p.Changes) == 0 {
			ui.Column(c).Fill().Center().Padding(16).Gap(8).Children(func() {
				ui.Icon(c, icons.Icon("check")).Size(20, 20).TextColor(k.DiffAdd)
				ui.Text(c, "No changes").FontSize(13).FontWeight(600).TextColor(k.Text)
				if p.Loading {
					ui.Text(c, "Reading…").FontSize(12).TextColor(k.TextFaint)
				}
			})
			return
		}
		p.list.Selected = &p.Sel
		p.list.Key = func(i int) any { return p.Changes[i].Display }
		list := ui.List(c, &p.list, len(p.Changes), func(i int) {
			ch := p.Changes[i]
			row := ui.Row(c.Key(fmt.Sprintf("git:%d", i))).Height(34).Padding(0, 10).
				Gap(10).Radius(theme.Radius).Cursor(ui.CursorPointer).
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
					ui.Text(c, filepath.Base(ch.Display)).FontSize(13).FontWeight(500).
						TextColor(fg).SingleLine().Ellipsis("…")
					if dir := filepath.Dir(ch.Display); dir != "." {
						// The folder nearest the file is the useful half
						// of a path too long for the list.
						ui.Text(c, dir).FontSize(12).TextColor(muted).
							SingleLine().TruncateStart()
					}
				})
				p.statusBadge(c, k, ch.Status)
			})
		}).Grow(1).MinHeight(0).Padding(6, 7).Gap(4)
		if list.Changed() {
			p.Select(p.Sel)
		}
		if list.Submitted() && p.Sel >= 0 && p.Sel < len(p.Changes) {
			if path := p.Changes[p.Sel].Display; p.h.HasPath(path) {
				p.h.OpenPath(path)
			}
		}
	})
}

func (p *Page) commits(c *ui.Context, k *theme.Colors) {
	if p.HistoryErr != "" {
		ui.Column(c).Fill().Center().Padding(16).Gap(8).Children(func() {
			ui.Icon(c, icons.Icon("git-branch")).Size(20, 20).TextColor(k.TextFaint)
			ui.Text(c, "History unavailable").FontSize(13).FontWeight(600).
				TextColor(k.Text).TextAlign(ui.Center)
			ui.Text(c, p.HistoryErr).FontSize(12).TextColor(k.TextFaint).
				TextAlign(ui.Center).MaxWidth(210)
		})
		return
	}
	if len(p.Commits) == 0 {
		ui.Column(c).Fill().Center().Padding(16).Gap(8).Children(func() {
			ui.Icon(c, icons.Icon("git-branch")).Size(20, 20).TextColor(k.TextFaint)
			ui.Text(c, "No commits").FontSize(13).FontWeight(600).TextColor(k.Text)
			detail := "This project has no history yet."
			if p.HistoryLoad {
				detail = "Reading…"
			}
			ui.Text(c, detail).FontSize(12).TextColor(k.TextFaint).
				TextAlign(ui.Center).MaxWidth(210)
		})
		return
	}
	p.historyList.Selected = &p.CommitSel
	p.historyList.Key = func(i int) any { return p.Commits[i].Hash }
	list := ui.List(c, &p.historyList, len(p.Commits), func(i int) {
		commit := p.Commits[i]
		row := ui.Row(c.Key(fmt.Sprintf("git-commit:%d", i))).Height(34).Padding(0, 10).
			Gap(10).Radius(theme.Radius).Cursor(ui.CursorPointer).
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
			ui.Column(c).Grow(1).MinWidth(0).Gap(2).Children(func() {
				ui.Text(c, commit.Subject).FontSize(13).FontWeight(500).TextColor(fg).
					SingleLine().Ellipsis("…")
				ui.Text(c, commitMeta(commit)).FontSize(12).TextColor(muted).
					SingleLine().Ellipsis("…")
			})
		})
	}).Grow(1).MinHeight(0).Padding(6, 7).Gap(4)
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

func (p *Page) statusBadge(c *ui.Context, k *theme.Colors, status string) {
	col := statusColor(status, k)
	ui.Text(c, statusLabel(status)).FontSize(10).FontWeight(700).
		TextColor(col).Padding(2, 5).Radius(theme.Radius).Background(col.Alpha(0.11)).
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
			ui.Text(c, "No file selected").FontSize(12).TextColor(k.TextMuted)
			ui.Spacer(c)
			return
		}
		ui.Icon(c, icons.FileIcon(path)).Size(15, 15).TextColor(k.TextMuted)
		ui.Text(c, filepath.Base(path)).FontSize(13).FontWeight(500).TextColor(k.Text).
			SingleLine().Shrink(0)
		if dir := filepath.Dir(path); dir != "." {
			ui.Text(c, dir).FontSize(12).TextColor(k.TextFaint).
				SingleLine().Ellipsis("…").Shrink(1).MinWidth(0)
		}
		ui.Spacer(c)
		if status := p.changeStatus(); status != "" {
			p.statusBadge(c, k, status)
		}
		if add, del := DiffStat(p.Diff); add > 0 || del > 0 {
			ui.Text(c, fmt.Sprintf("+%d", add)).FontSize(11).FontWeight(600).
				TextColor(k.DiffAdd).Shrink(0)
			ui.Text(c, fmt.Sprintf("−%d", del)).FontSize(11).FontWeight(600).
				TextColor(k.DiffDel).Shrink(0)
		}
		if p.h.HasPath(path) {
			open := ui.ButtonBase(c).Padding(4, 9).Radius(theme.Radius).
				Cursor(ui.CursorPointer).Label("Open File")
			if open.Hovered() {
				open.Background(k.Hover)
			}
			if open.Clicked() {
				p.h.OpenPath(path)
			}
			open.Children(func() {
				ui.Text(c, "Open").FontSize(12).FontWeight(500).TextColor(k.Text)
			})
		}
	})
}

// commitHeader names the commit whose patch the diff pane shows.
func (p *Page) commitHeader(c *ui.Context, k *theme.Colors) {
	commit, ok := p.selectedCommit()
	if !ok {
		ui.Text(c, "No commit selected").FontSize(12).TextColor(k.TextMuted)
		ui.Spacer(c)
		return
	}
	ui.Text(c, commit.Short).Font(theme.NoteFont.Family).FontSize(11).
		TextColor(k.TextMuted).Padding(2, 6).Radius(theme.Radius).Background(k.Hover)
	ui.Text(c, commit.Subject).FontSize(13).FontWeight(500).TextColor(k.Text).
		SingleLine().Ellipsis("…").Shrink(1).MinWidth(0)
	ui.Spacer(c)
	if add, del := DiffStat(p.Diff); add > 0 || del > 0 {
		ui.Text(c, fmt.Sprintf("+%d", add)).FontSize(11).FontWeight(600).
			TextColor(k.DiffAdd).Shrink(0)
		ui.Text(c, fmt.Sprintf("−%d", del)).FontSize(11).FontWeight(600).
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
		lines := ParseDiff(p.Diff)
		if len(lines) == 0 {
			p.state(c, k, "file-text", k.TextFaint, "No text changes",
				"This file has no textual diff.")
			return
		}
		p.diffLines(c, k, lines)
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
		ui.Text(c, title).FontSize(13).FontWeight(600).TextColor(k.Text).
			TextAlign(ui.Center)
		if detail != "" {
			ui.Text(c, detail).FontSize(12).TextColor(k.TextFaint).
				TextAlign(ui.Center).MaxWidth(460)
		}
	})
}

// diffLines draws one file's diff, the working tree's view of it.
func (p *Page) diffLines(c *ui.Context, k *theme.Colors, lines []gitDiffLine) {
	const maxLines = 2500
	truncated := len(lines) > maxLines
	if truncated {
		lines = lines[:maxLines]
	}
	scroll := ui.Scroll(c).Grow(1).MinHeight(0).Padding(0, 0, 4, 0).
		TrackScroll(&p.DiffScroll)
	scroll.Children(func() {
		diffRows(c, k, lines)
		if truncated {
			ui.Text(c, "… diff truncated").FontSize(11).TextColor(k.TextFaint).Padding(6, 10)
		}
	})
}

// diffPatch draws every file a commit touched, each under its own
// heading, so a commit that changed several files stays readable.
func (p *Page) diffPatch(c *ui.Context, k *theme.Colors) {
	const maxLines = 2500
	left := maxLines
	scroll := ui.Scroll(c).Grow(1).MinHeight(0).Padding(0, 0, 4, 0).
		TrackScroll(&p.DiffScroll)
	scroll.Children(func() {
		for _, file := range p.patch {
			if left <= 0 {
				break
			}
			p.fileHeading(c, k, file)
			lines := file.Lines
			if len(lines) > left {
				lines = lines[:left]
			}
			diffRows(c, k, lines)
			left -= len(lines)
		}
		if left <= 0 {
			ui.Text(c, "… diff truncated").FontSize(11).TextColor(k.TextFaint).Padding(6, 10)
		}
	})
}

// fileHeading names one file inside a commit's patch.
func (p *Page) fileHeading(c *ui.Context, k *theme.Colors, file FileDiff) {
	ui.Row(c).FillWidth().Padding(6, 10).Gap(7).AlignItems(ui.Center).
		Background(k.Hover).Children(func() {
		ui.Icon(c, icons.FileIcon(file.Path)).Size(13, 13).TextColor(k.TextMuted)
		ui.Text(c, filepath.Base(file.Path)).FontSize(12).FontWeight(600).
			TextColor(k.Text).SingleLine().Shrink(0)
		if dir := filepath.Dir(file.Path); dir != "." {
			ui.Text(c, dir).FontSize(11.5).TextColor(k.TextFaint).
				SingleLine().Ellipsis("…").Shrink(1).MinWidth(0)
		}
		ui.Spacer(c)
		if file.Add > 0 || file.Del > 0 {
			ui.Text(c, fmt.Sprintf("+%d", file.Add)).FontSize(10.5).FontWeight(600).
				TextColor(k.DiffAdd).Shrink(0)
			ui.Text(c, fmt.Sprintf("−%d", file.Del)).FontSize(10.5).FontWeight(600).
				TextColor(k.DiffDel).Shrink(0)
		}
		if file.Status != "M" {
			p.statusBadge(c, k, file.Status)
		}
	})
}

func diffRows(c *ui.Context, k *theme.Colors, lines []gitDiffLine) {
	for _, line := range lines {
		switch line.kind {
		case '@':
			ui.Row(c).FillWidth().Padding(3, 10).AlignItems(ui.Start).
				Background(k.DiffHunk.Alpha(0.08)).Children(func() {
				ui.Text(c, diffLineText(line.text)).Font(theme.NoteFont.Family).FontSize(11).
					TextColor(k.DiffHunk).LineHeight(1.5).Shrink(1).MinWidth(0)
			})
		case 'm', '\\':
			ui.Row(c).FillWidth().Padding(1, 10).AlignItems(ui.Start).Children(func() {
				ui.Text(c, diffLineText(line.text)).Font(theme.NoteFont.Family).FontSize(11).
					TextColor(k.TextFaint).LineHeight(1.5).Shrink(1).MinWidth(0)
			})
		default:
			row := ui.Row(c).FillWidth().ClipX().AlignItems(ui.Start)
			switch line.kind {
			case '+':
				row.Background(k.DiffAdd.Alpha(0.10))
			case '-':
				row.Background(k.DiffDel.Alpha(0.09))
			}
			row.Children(func() {
				gitGutter(c, k, line.old)
				gitGutter(c, k, line.new)
				ui.Text(c, diffLineText(line.text)).Font(theme.NoteFont.Family).FontSize(11.5).
					TextColor(diffLineKindColor(line.kind, k)).LineHeight(1.5).
					Shrink(1).MinWidth(0).Padding(1, 0, 1, 8)
			})
		}
	}
}

func gitGutter(c *ui.Context, k *theme.Colors, n int) {
	text := ""
	if n > 0 {
		text = strconv.Itoa(n)
	}
	ui.Text(c, text).Font(theme.NoteFont.Family).FontSize(10).TextColor(k.TextFaint).
		Width(38).TextAlign(ui.End).Shrink(0).Padding(0, 5)
}

func diffLineKindColor(kind byte, k *theme.Colors) ui.Color {
	switch kind {
	case '+':
		return k.DiffAdd
	case '-':
		return k.DiffDel
	default:
		return k.TextMuted
	}
}

// diffLineRunes bounds one drawn diff line. A file Git reads as text but
// is really binary, such as an archive or a packed blob whose first
// kilobytes held no NUL, arrives as one line of megabytes; shaping it
// costs the square of its length, which stalled the window for seconds.
// A row wraps, so it draws the line up to this many characters and marks
// the cut.
const diffLineRunes = 2000

func diffLineText(line string) string {
	line = clipRunes(line, diffLineRunes)
	line = strings.ReplaceAll(line, "\t", "    ")
	if strings.TrimSpace(line) == "" {
		// A blank row still takes a line of height, which shaping an empty
		// string does not.
		return "\u00a0"
	}
	return line
}

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
		Cursor(ui.CursorPointer).Label(label)
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
