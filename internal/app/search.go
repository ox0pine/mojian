package app

import (
	"context"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"mojian/internal/icons"
	"mojian/internal/motion"
	"mojian/internal/project"
	"mojian/internal/session"

	"github.com/egoist/mygo/ui"
)

// openSearch shows the project-wide full-text search sheet over the window.
func (a *app) openSearch() {
	a.closeModals()
	a.searchOpen = true
	a.searchSel = 0
	a.searchList = ui.ListState{}
	a.searchLastKey = ""
	if strings.TrimSpace(a.searchQuery) != "" {
		a.startProjectSearch()
	}
	a.invalidate()
}

// closeSearch hides the search sheet and cancels any in-flight search.
func (a *app) closeSearch() {
	a.searchOpen = false
	a.cancelProjectSearch()
}

func (a *app) cancelProjectSearch() {
	a.searchSeq++
	if a.searchCancel != nil {
		a.searchCancel()
		a.searchCancel = nil
	}
	a.searchBusy = false
}

func (a *app) searchKey() string {
	return fmt.Sprintf("%t|%t|%s", a.searchCase, a.searchRegex, a.searchQuery)
}

func (a *app) startProjectSearch() {
	a.cancelProjectSearch()
	a.searchLastKey = a.searchKey()
	a.searchErr = ""
	a.searchSel = 0
	if a.searchQuery == "" || a.store == nil {
		a.searchResult = session.SearchResult{}
		return
	}
	opts := session.SearchOptions{
		Query:         a.searchQuery,
		CaseSensitive: a.searchCase,
		Regex:         a.searchRegex,
	}
	overrides := a.store.SearchOverrides()
	backend := a.store.FS
	post := a.uiPoster()
	if post == nil {
		res, err := project.Search(context.Background(), backend, overrides, opts)
		if err != nil {
			a.searchErr = err.Error()
			a.searchResult = session.SearchResult{}
			return
		}
		a.searchResult = res
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	a.searchCancel = cancel
	a.searchBusy = true
	seq := a.searchSeq
	winSeq := a.windowSeq
	store := a.store
	go func() {
		res, err := project.Search(ctx, backend, overrides, opts)
		if ctx.Err() != nil {
			return
		}
		post(func() {
			if a.windowSeq != winSeq || a.searchSeq != seq || a.store != store {
				return
			}
			a.searchBusy = false
			a.searchCancel = nil
			if err != nil {
				a.searchErr = err.Error()
				a.searchResult = session.SearchResult{}
			} else {
				a.searchErr = ""
				a.searchResult = res
				a.searchSel = min(max(a.searchSel, 0), max(len(res.Matches)-1, 0))
			}
			a.invalidate()
		})
	}()
}

// searchModal draws the project-wide text search sheet over the window.
func (a *app) searchModal(c *ui.Context, k *colors) {
	if !a.searchOpen {
		return
	}
	if a.searchLastKey != a.searchKey() {
		a.startProjectSearch()
	}
	matches := a.searchResult.Matches
	a.searchSel = min(max(a.searchSel, 0), max(len(matches)-1, 0))
	was := a.searchOpen
	reveal := false
	chosen := -1
	commandDialog(c, k, &a.searchOpen, func() {
		var in ui.Element
		ui.Row(c).Height(48).Padding(0, 16).Gap(8).AlignItems(ui.Center).
			BorderWidth(0, 0, 1, 0).BorderColor(k.PanelBorder).Children(func() {
			ui.Icon(c, icons.Icon("search")).Size(17, 17).TextColor(k.TextFaint)
			in = ui.TextInputBase(c, &a.searchQuery).Grow(1).FontSize(c.Theme().Rem(15.0 / 13)).
				Placeholder("Search in project…").Label("Search in project").AutoFocus()
			if searchToggleChip(c, k, "Aa", "Match Case", a.searchCase).Clicked() {
				a.searchCase = !a.searchCase
				a.startProjectSearch()
			}
			if searchToggleChip(c, k, ".*", "Use Regular Expression", a.searchRegex).Clicked() {
				a.searchRegex = !a.searchRegex
				a.startProjectSearch()
			}
			if iconButton(c, k, "x", "Close", iconBtnSm, iconGlyphSm).Clicked() {
				a.searchOpen = false
			}
		})
		if listNav(in, &a.searchSel, len(matches), &reveal) {
			chosen = a.searchSel
		}
		if a.searchQuery != "" {
			ui.Row(c).Height(28).Padding(0, 16).Gap(8).AlignItems(ui.Center).
				BorderWidth(0, 0, 1, 0).BorderColor(k.PanelBorder).Children(func() {
				switch {
				case a.searchBusy:
					ui.Spinner(c).Size(12, 12).Label("Searching")
					ui.Text(c, "Searching…").FontSize(rowDetail).TextColor(k.TextFaint)
				case a.searchErr != "":
					ui.Text(c, a.searchErr).FontSize(rowDetail).TextColor(k.Danger).
						SingleLine().Ellipsis("…").Shrink(1).MinWidth(0)
				default:
					ui.Text(c, searchSummary(a.searchResult)).FontSize(rowDetail).TextColor(k.TextFaint)
				}
			})
		}
		a.searchList.Selected = &a.searchSel
		a.searchList.Key = func(i int) any {
			m := matches[i]
			return m.Rel + ":" + strconv.Itoa(m.Line) + ":" + strconv.Itoa(m.Col)
		}
		listH := max(sheetListHeight(c)-28, 120)
		list := ui.List(c, &a.searchList, len(matches), func(i int) {
			m := matches[i]
			selected := i == a.searchSel
			row := ui.Column(c.Key("match:"+m.Rel+":"+strconv.Itoa(m.Line)+":"+strconv.Itoa(m.Col))).
				Height(44).Padding(4, rowPadX).Gap(2).Justify(ui.Center).
				Radius(radius).Cursor(ui.CursorPointer)
			fg, muted, icon := k.Text, k.TextFaint, k.TextMuted
			if selected {
				fg = c.Theme().AccentText
				muted, icon = fg.Alpha(0.9), fg.Alpha(0.9)
			} else if row.Hovered() {
				row.Background(k.Hover)
			}
			if row.Clicked() {
				chosen = i
			}
			row.Children(func() {
				ui.Row(c).Gap(6).AlignItems(ui.Center).MinWidth(0).Children(func() {
					ui.Icon(c, icons.FileIcon(m.Rel)).Size(14, 14).TextColor(icon)
					ui.Text(c, filepath.Base(m.Rel)).FontSize(rowTitle).FontWeight(500).TextColor(fg).
						SingleLine().Ellipsis("…").Shrink(0.3).MinWidth(0)
					ui.Text(c, ":"+strconv.Itoa(m.Line)).Font(noteFont.Family).FontSize(rowDetail).
						TextColor(muted).Shrink(0)
					if dir := filepath.Dir(filepath.FromSlash(m.Rel)); dir != "." {
						ui.Text(c, filepath.ToSlash(dir)).FontSize(rowDetail).TextColor(muted).
							SingleLine().TruncateStart().Shrink(1).MinWidth(0)
					}
				})
				ui.Text(c, m.Text).Font(noteFont.Family).FontSize(rowDetail).TextColor(muted).
					SingleLine().Ellipsis("…").MinWidth(0)
			})
		}).MaxHeight(listH).Padding(6)
		if len(matches) == 0 && !a.searchBusy && a.searchErr == "" {
			msg := "Type to search across files"
			if a.searchQuery != "" {
				msg = "No matches"
			}
			list.Children(func() { emptyLine(c, k, msg) })
		}
		if reveal {
			a.searchList.ScrollIntoView(a.searchSel)
		}
	})
	if chosen >= 0 && chosen < len(matches) {
		m := matches[chosen]
		a.closeSearch()
		a.later(func() { a.openNoteAt(filepath.FromSlash(m.Rel), m.Line, m.Col, m.End) })
	}
	if was && !a.searchOpen {
		a.cancelProjectSearch()
		if t := a.tab(); t != nil && a.focusReq == nil {
			if t.Focus != nil {
				a.focusReq = t.Focus
			} else {
				a.focusReq = t.editorPane()
			}
		}
	}
}

func searchToggleChip(c *ui.Context, k *colors, text, label string, active bool) ui.Element {
	b := ui.ButtonBase(c).Height(24).Padding(0, 7).Radius(radius - 2).
		Cursor(ui.CursorPointer).Label(label).Tooltip(label).Transition(motion.Colors)
	fg := k.TextMuted
	switch {
	case active:
		b.Background(c.Theme().Accent)
		fg = c.Theme().AccentText
	case b.Hovered():
		b.Background(k.Hover)
		fg = k.Text
	default:
		b.Background(ui.Transparent)
	}
	b.Children(func() {
		ui.Text(c, text).Font(noteFont.Family).FontSize(c.Theme().Rem(11.5 / 13)).
			FontWeight(600).TextColor(fg)
	})
	return b
}

func searchSummary(r session.SearchResult) string {
	n := len(r.Matches)
	if n == 0 {
		return "No matches"
	}
	matchWord := "matches"
	if n == 1 {
		matchWord = "match"
	}
	fileWord := "files"
	if r.Files == 1 {
		fileWord = "file"
	}
	if r.Truncated {
		return fmt.Sprintf("First %d %s in %d %s", n, matchWord, r.Files, fileWord)
	}
	return fmt.Sprintf("%d %s in %d %s", n, matchWord, r.Files, fileWord)
}
