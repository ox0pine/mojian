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
	"github.com/egoist/mygo/ui/editor"
)

// openSearch shows the project-wide full-text search & replace sheet over the window.
func (a *app) openSearch() {
	a.closeModals()
	a.searchOpen = true
	a.searchSel = 0
	a.searchList = ui.ListState{}
	a.searchLastKey = ""
	a.searchNotice = ""
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

func (a *app) searchOptions() session.SearchOptions {
	return session.SearchOptions{
		Query:         a.searchQuery,
		CaseSensitive: a.searchCase,
		Regex:         a.searchRegex,
		Include:       a.searchInclude,
		Exclude:       a.searchExclude,
	}
}

func (a *app) searchKey() string {
	return fmt.Sprintf("%t|%t|%s|%s|%s", a.searchCase, a.searchRegex, a.searchInclude, a.searchExclude, a.searchQuery)
}

func (a *app) startProjectSearch() {
	a.cancelProjectSearch()
	a.searchLastKey = a.searchKey()
	a.searchErr = ""
	a.searchNotice = ""
	a.searchSel = 0
	if a.searchQuery == "" || a.store == nil {
		a.searchResult = session.SearchResult{}
		return
	}
	opts := a.searchOptions()
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

// replaceAllMatches applies searchReplace across every matched file in memory,
// keeping Mojian's explicit ⌘S save and undo semantics intact.
func (a *app) replaceAllMatches() {
	if a.store == nil || a.searchQuery == "" || len(a.searchResult.Matches) == 0 {
		return
	}
	opts := a.searchOptions()
	seen := map[string]bool{}
	var rels []string
	for _, m := range a.searchResult.Matches {
		if !seen[m.Rel] {
			seen[m.Rel] = true
			rels = append(rels, m.Rel)
		}
	}
	totalCount := 0
	totalFiles := 0
	for _, rel := range rels {
		n := a.store.ByRelPath(filepath.FromSlash(rel))
		if n == nil {
			continue
		}
		if !a.loadNote(n) {
			continue
		}
		cur := n.Text()
		updated, count, err := session.ReplaceAllText(cur, a.searchReplace, opts)
		if err != nil {
			a.searchErr = err.Error()
			a.invalidate()
			return
		}
		if count == 0 || updated == cur {
			continue
		}
		buf := n.Buffer()
		buf.Edit(editor.Edit{Start: 0, End: len(cur), Text: updated})
		totalCount += count
		totalFiles++
	}
	a.startProjectSearch()
	if totalCount > 0 {
		a.searchNotice = fmt.Sprintf("Replaced %d in %d %s · %s to save",
			totalCount, totalFiles, pluralWord(totalFiles, "file", "files"), a.commandKeys(cmdSave))
	} else {
		a.searchNotice = "No replacements applied"
	}
	a.invalidate()
}

func pluralWord(n int, singular, plural string) string {
	if n == 1 {
		return singular
	}
	return plural
}

// searchModal draws the project-wide text search & replace sheet over the window.
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
	a.commandDialog(c, k, &a.searchOpen, func() {
		var in ui.Element
		ui.Row(c).Height(48).Padding(0, 16).Gap(6).AlignItems(ui.Center).
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
			if searchToggleChip(c, k, "Files", "Filter files by glob", a.searchFilterOpen || a.searchInclude != "" || a.searchExclude != "").Clicked() {
				a.searchFilterOpen = !a.searchFilterOpen
			}
			if searchToggleChip(c, k, "Replace", "Toggle Replace", a.searchReplaceOpen).Clicked() {
				a.searchReplaceOpen = !a.searchReplaceOpen
			}
			if iconButton(c, k, "x", "Close", iconBtnSm, iconGlyphSm).Clicked() {
				a.searchOpen = false
			}
		})
		extraChrome := float32(28)
		if a.searchFilterOpen {
			extraChrome += 36
			ui.Row(c).Height(36).Padding(0, 16).Gap(8).AlignItems(ui.Center).
				BorderWidth(0, 0, 1, 0).BorderColor(k.PanelBorder).Children(func() {
				ui.TextInputBase(c.Key("search-inc"), &a.searchInclude).Grow(1).MinWidth(0).
					FontSize(c.Theme().Rem(12.0 / 13)).Placeholder("Include (e.g. *.go, internal/**)").
					Label("Files to include")
				ui.Box(c).Width(1).Height(16).Background(k.PanelBorder)
				ui.TextInputBase(c.Key("search-exc"), &a.searchExclude).Grow(1).MinWidth(0).
					FontSize(c.Theme().Rem(12.0 / 13)).Placeholder("Exclude (e.g. *_test.go)").
					Label("Files to exclude")
			})
		}
		if a.searchReplaceOpen {
			extraChrome += 38
			ui.Row(c).Height(38).Padding(0, 16).Gap(8).AlignItems(ui.Center).
				BorderWidth(0, 0, 1, 0).BorderColor(k.PanelBorder).Children(func() {
				ui.TextInputBase(c.Key("search-replace"), &a.searchReplace).Grow(1).MinWidth(0).
					FontSize(c.Theme().Rem(13.0 / 13)).Placeholder("Replace with…").
					Label("Replace with")
				btn := ui.ButtonBase(c.Key("replace-all")).Height(24).Padding(0, 10).Radius(radius - 2).
					Cursor(ui.CursorPointer).Label("Replace All").
					Disabled(a.searchBusy || len(matches) == 0).Transition(motion.Colors)
				if btn.Hovered() {
					btn.Background(k.Pressed)
				} else {
					btn.Background(k.Hover)
				}
				btn.Children(func() {
					ui.Text(c, "Replace All").FontSize(c.Theme().Rem(11.5 / 13)).FontWeight(550).TextColor(k.Text)
				})
				if btn.Clicked() {
					a.later(func() { a.replaceAllMatches() })
				}
			})
		}
		if listNav(in, &a.searchSel, len(matches), &reveal) {
			chosen = a.searchSel
		}
		if a.searchQuery != "" || a.searchNotice != "" {
			ui.Row(c).Height(28).Padding(0, 16).Gap(8).AlignItems(ui.Center).
				BorderWidth(0, 0, 1, 0).BorderColor(k.PanelBorder).Children(func() {
				switch {
				case a.searchBusy:
					ui.Spinner(c).Size(12, 12).Label("Searching")
					ui.Text(c, "Searching…").FontSize(rowDetail).TextColor(k.TextFaint)
				case a.searchErr != "":
					ui.Text(c, a.searchErr).FontSize(rowDetail).TextColor(k.Danger).
						SingleLine().Ellipsis("…").Shrink(1).MinWidth(0)
				case a.searchNotice != "":
					ui.Text(c, a.searchNotice).FontSize(rowDetail).TextColor(k.TextMuted).
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
		listH := max(sheetListHeight(c)-extraChrome, 100)
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
		a.later(func() {
			a.recordNav()
			a.openNoteAt(filepath.FromSlash(m.Rel), m.Line, m.Col, m.End)
		})
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
	matchWord := pluralWord(n, "match", "matches")
	fileWord := pluralWord(r.Files, "file", "files")
	if r.Truncated {
		return fmt.Sprintf("First %d %s in %d %s", n, matchWord, r.Files, fileWord)
	}
	return fmt.Sprintf("%d %s in %d %s", n, matchWord, r.Files, fileWord)
}
