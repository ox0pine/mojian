package git

import (
	"strings"

	"mojian/internal/icons"
	"mojian/internal/theme"

	"github.com/egoist/mygo/ui"
)

// The page's branch menu: its own popover under its own chip, with its
// own state, instead of the title bar's menu. Switching still runs through
// the window, so the notes it reloads and the title bar agree.
const (
	branchMenuW     = 288
	branchRowH      = 34
	branchMaxH      = 338
	branchMenuInset = 8
)

// ToggleBranchMenu opens the page's branch menu under its chip, or closes
// it. Opening reads the branches again, since a terminal may have added or
// removed one.
func (p *Page) ToggleBranchMenu() {
	if p.h == nil {
		return
	}
	if p.BranchMenu {
		p.CloseBranchMenu()
		return
	}
	p.openBranchMenu()
}

// openBranchMenu shows the page's menu and reads the branches again.
func (p *Page) openBranchMenu() {
	p.BranchMenu = true
	p.branchNew = false
	p.branchName = ""
	p.branchQuery = ""
	p.branchSel = -1
	p.branchErr = ""
	p.loadBranches()
	if p.h != nil {
		p.h.Invalidate()
	}
}

// CloseBranchMenu closes the menu and forgets a name being typed.
func (p *Page) CloseBranchMenu() {
	p.BranchMenu = false
	p.branchNew = false
	p.branchName = ""
	p.branchQuery = ""
	p.branchSel = -1
	p.branchErr = ""
	if p.h != nil {
		p.h.Invalidate()
	}
}

func (p *Page) loadBranches() {
	h := p.h
	if h == nil || !hasProject(h) {
		return
	}
	p.runner = h.GitRunner()
	p.branchLoad = true
	p.branchErr = ""
	p.branchSeq++
	seq := p.branchSeq
	dir := mustDir(h)
	if h.Window() == nil {
		p.applyBranches(seq, ReadBranches(p.runner, dir))
		return
	}
	win := h.Window()
	go func() {
		d := ReadBranches(p.runner, dir)
		win.Update(func() {
			if seq == p.branchSeq {
				p.applyBranches(seq, d)
				h.Invalidate()
			}
		})
	}()
}

func (p *Page) applyBranches(seq int, d BranchData) {
	if seq != p.branchSeq {
		return
	}
	p.branchLoad = false
	p.Branches = d
}

// chooseBranch asks the window to check out name; it closes the menu when
// the window accepted the change.
func (p *Page) chooseBranch(name string) {
	if err := p.h.SwitchBranch(p.Root, name); err != nil {
		p.branchErr = err.Error()
		p.h.Invalidate()
		return
	}
	p.CloseBranchMenu()
}

// makeBranch creates the typed branch at HEAD and checks it out.
func (p *Page) makeBranch() {
	name := strings.TrimSpace(p.branchName)
	if name == "" {
		p.branchErr = "Type a branch name."
		p.h.Invalidate()
		return
	}
	if err := p.h.CreateBranch(p.Root, name); err != nil {
		p.branchErr = err.Error()
		p.h.Invalidate()
		return
	}
	p.CloseBranchMenu()
}

// menuItem is one row of the page's drawn branch menu.
func menuItem(m ui.MenuParts, key any) ui.Element {
	return m.Item().Key(key)
}

func filterPageBranches(branches []Branch, query string) []Branch {
	q := strings.ToLower(strings.TrimSpace(query))
	if q == "" {
		return branches
	}
	want := []rune(q)
	var out []Branch
	for _, b := range branches {
		low := strings.ToLower(b.Name)
		if strings.Contains(low, q) {
			out = append(out, b)
			continue
		}
		qi := 0
		for _, r := range low {
			if qi < len(want) && r == want[qi] {
				qi++
			}
		}
		if qi == len(want) {
			out = append(out, b)
		}
	}
	return out
}

// branchMenuBody draws the popover: the branches, the new branch field, or
// the note explaining why the list is empty.
func (p *Page) branchMenuBody(c *ui.Context, k *theme.Colors, m ui.MenuParts) {
	if p.branchNew {
		p.newBranchBody(c, k)
		return
	}
	ui.Row(c).Padding(6, branchMenuInset, 4, branchMenuInset).AlignItems(ui.Center).Children(func() {
		ui.Text(c, "BRANCHES").FontSize(c.Theme().Rem(10.0 / 13)).FontWeight(650).TextColor(k.TextFaint)
	})
	d := p.Branches
	filtered := filterPageBranches(d.Branches, p.branchQuery)
	if p.branchSel >= len(filtered) {
		p.branchSel = len(filtered) - 1
	}
	reveal := false
	switch {
	case p.branchLoad && len(d.Branches) == 0:
		p.branchNote(c, k, "Reading branches…", false)
	case len(d.Branches) == 0:
		msg := "No branches"
		if d.Err != "" {
			msg = d.Err
		}
		p.branchNote(c, k, msg, d.Err != "")
	default:
		ui.Box(c).Padding(2, branchMenuInset, 6, branchMenuInset).Children(func() {
			in := ui.SearchField(c.Key("git-branch-filter"), &p.branchQuery).FillWidth().
				Height(30).Padding(0, 8).Gap(6).Radius(theme.Radius-2).Background(k.Hover).
				Border(0, ui.Transparent).FontSize(c.Theme().Rem(12.0 / 13)).
				Placeholder("Filter branches…").Label("Filter branches").AutoFocus()
			if in.Changed() {
				if len(filtered) > 0 {
					p.branchSel = 0
				} else {
					p.branchSel = -1
				}
				reveal = true
			}
			switch {
			case in.Shortcut(0, ui.KeyDown) && len(filtered) > 0:
				if p.branchSel < 0 {
					p.branchSel = 0
				} else {
					p.branchSel = min(p.branchSel+1, len(filtered)-1)
				}
				reveal = true
			case in.Shortcut(0, ui.KeyUp) && len(filtered) > 0:
				if p.branchSel > 0 {
					p.branchSel--
				} else {
					p.branchSel = 0
				}
				reveal = true
			}
			if in.Submitted() && len(filtered) > 0 {
				idx := p.branchSel
				if idx < 0 || idx >= len(filtered) {
					idx = 0
				}
				p.chooseBranch(filtered[idx].Name)
			}
		})
		if len(filtered) == 0 {
			p.branchNote(c, k, "No matching branches", false)
		} else {
			ui.Scroll(c).MaxHeight(branchMaxH).Gap(4).Children(func() {
				for i, b := range filtered {
					p.branchMenuRow(c, k, m, b, i == p.branchSel, reveal)
				}
			})
		}
	}
	if p.branchErr != "" {
		p.branchNote(c, k, p.branchErr, true)
	}
	ui.Divider(c).Margin(6, 0)
	row := menuItem(m, "menu:new-branch").Height(branchRowH).Padding(0, branchMenuInset).Gap(8).
		Radius(theme.Radius).Justify(ui.Start).Cursor(ui.CursorPointer).
		Label("New Branch…").KeepOpen()
	if row.Highlighted() {
		row.Background(k.Hover)
	}
	if row.Clicked() {
		p.branchNew = true
		p.branchName = ""
		p.branchErr = ""
	}
	row.Children(func() {
		ui.Box(c).Size(16, 16).Center().Shrink(0).Children(func() {
			ui.Icon(c, icons.Icon("plus")).Size(15, 15).TextColor(k.TextMuted)
		})
		ui.Text(c, "New Branch…").FontSize(c.Theme().Rem(1)).TextColor(k.Text)
	})
}

// branchMenuRow is one branch, checked when it is the current one.
func (p *Page) branchMenuRow(c *ui.Context, k *theme.Colors, m ui.MenuParts, b Branch, selected, reveal bool) {
	row := menuItem(m, "branch:"+b.Name).Height(branchRowH).Padding(0, branchMenuInset).Gap(8).
		Radius(theme.Radius).Justify(ui.Start).Cursor(ui.CursorPointer).Label(b.Name).KeepOpen()
	if b.Current {
		row.Background(k.PanelSel)
	} else if selected || row.Highlighted() {
		row.Background(k.Hover)
	}
	if selected && reveal {
		row.ScrollIntoView()
	}
	if row.Clicked() {
		p.chooseBranch(b.Name)
	}
	weight := 500
	if b.Current {
		weight = 600
	}
	row.Children(func() {
		ui.Box(c).Size(16, 16).Center().Shrink(0).Children(func() {
			if b.Current {
				ui.Icon(c, icons.Icon("check")).Size(14, 14).TextColor(k.Text)
			} else {
				ui.Icon(c, icons.Icon("git-branch")).Size(15, 15).TextColor(k.TextMuted)
			}
		})
		ui.Text(c, b.Name).FontSize(c.Theme().Rem(1)).FontWeight(weight).TextColor(k.Text).
			SingleLine().Ellipsis("…").Grow(1).MinWidth(0)
	})
}

// newBranchBody is the name field the New Branch row swaps in.
func (p *Page) newBranchBody(c *ui.Context, k *theme.Colors) {
	ui.Row(c).Padding(2, branchMenuInset).Gap(8).AlignItems(ui.Center).Children(func() {
		ui.Icon(c, icons.Icon("plus")).Size(15, 15).TextColor(k.TextMuted)
		in := ui.TextInputBase(c, &p.branchName).Grow(1).FontSize(c.Theme().Rem(12.5 / 13)).
			Placeholder("New branch name").AutoFocus()
		if in.Submitted() {
			p.makeBranch()
		}
	})
	if p.branchErr != "" {
		p.branchNote(c, k, p.branchErr, true)
		return
	}
	if p.Branches.Current == "" {
		return
	}
	hint := "Creates a branch from " + p.Branches.Current
	if p.Branches.Detached {
		hint = "Creates a branch at " + p.Branches.Current
	}
	p.branchNote(c, k, hint, false)
}

// branchNote is the menu's loading, empty or failure line.
func (p *Page) branchNote(c *ui.Context, k *theme.Colors, text string, danger bool) {
	face := k.TextFaint
	if danger {
		face = k.Danger
	}
	ui.Text(c, text).FontSize(c.Theme().Rem(12.5/13)).TextColor(face).Padding(6, branchMenuInset)
}
