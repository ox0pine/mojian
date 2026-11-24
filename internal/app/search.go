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

type searchState struct {
	query       string
	caseSens    bool
	wholeWord   bool
	regex       bool
	include     string
	exclude     string
	filterOpen  bool
	replace     string
	replaceOpen bool
	notice      string
	lastKey     string
	busy        bool
	err         string
	result      session.SearchResult
	sel         int
	list        ui.ListState
	seq         int
	cancel      func()
	collapsed   map[string]bool
	isModal     bool
}

type searchPane struct {
	state      *searchState
	focusInput bool
}

type searchRow struct {
	isHeader  bool
	rel       string
	count     int
	collapsed bool
	match     session.SearchMatch
}

func (s *searchState) options() session.SearchOptions {
	return session.SearchOptions{
		Query:         s.query,
		CaseSensitive: s.caseSens,
		WholeWord:     s.wholeWord,
		Regex:         s.regex,
		Include:       s.include,
		Exclude:       s.exclude,
	}
}

func (s *searchState) key() string {
	return fmt.Sprintf("%t|%t|%t|%s|%s|%s", s.caseSens, s.wholeWord, s.regex, s.include, s.exclude, s.query)
}

func (s *searchState) cloneForPane() *searchState {
	if s == nil {
		return &searchState{collapsed: map[string]bool{}}
	}
	cp := *s
	cp.cancel = nil
	cp.busy = false
	cp.isModal = false
	cp.list = ui.ListState{}
	cp.collapsed = map[string]bool{}
	for k, v := range s.collapsed {
		cp.collapsed[k] = v
	}
	cp.result.Matches = append([]session.SearchMatch(nil), s.result.Matches...)
	return &cp
}

func (a *app) ensureModalSearch() *searchState {
	if a.modalSearch == nil {
		a.modalSearch = &searchState{isModal: true, collapsed: map[string]bool{}}
	}
	return a.modalSearch
}

func (a *app) syncModalSearchIn() *searchState {
	st := a.ensureModalSearch()
	st.query = a.searchQuery
	st.caseSens = a.searchCase
	st.wholeWord = a.searchWholeWord
	st.regex = a.searchRegex
	st.include = a.searchInclude
	st.exclude = a.searchExclude
	st.filterOpen = a.searchFilterOpen
	st.replace = a.searchReplace
	st.replaceOpen = a.searchReplaceOpen
	st.notice = a.searchNotice
	st.lastKey = a.searchLastKey
	st.busy = a.searchBusy
	st.err = a.searchErr
	st.result = a.searchResult
	st.sel = a.searchSel
	st.list = a.searchList
	st.seq = a.searchSeq
	st.cancel = a.searchCancel
	if a.searchCollapsed != nil {
		st.collapsed = a.searchCollapsed
	} else if st.collapsed == nil {
		st.collapsed = map[string]bool{}
	}
	return st
}

func (a *app) syncModalSearchOut() {
	if a.modalSearch == nil {
		return
	}
	st := a.modalSearch
	a.searchQuery = st.query
	a.searchCase = st.caseSens
	a.searchWholeWord = st.wholeWord
	a.searchRegex = st.regex
	a.searchInclude = st.include
	a.searchExclude = st.exclude
	a.searchFilterOpen = st.filterOpen
	a.searchReplace = st.replace
	a.searchReplaceOpen = st.replaceOpen
	a.searchNotice = st.notice
	a.searchLastKey = st.lastKey
	a.searchBusy = st.busy
	a.searchErr = st.err
	a.searchResult = st.result
	a.searchSel = st.sel
	a.searchList = st.list
	a.searchSeq = st.seq
	a.searchCancel = st.cancel
	a.searchCollapsed = st.collapsed
}

// selectedSearchSeed returns the single-line selection of the focused editor
// pane when suitable for seeding a project search.
func (a *app) selectedSearchSeed() string {
	p := a.focusPane()
	if p == nil || p.Note == nil || p.Preview || p.state == nil {
		return ""
	}
	sel := strings.TrimRight(p.state.SelectedText(), "\r")
	if sel == "" || len(sel) > 200 || strings.ContainsAny(sel, "\n\r") {
		return ""
	}
	return sel
}

func (a *app) recordSearchHistory(q string) {
	q = strings.TrimSpace(q)
	if q == "" {
		return
	}
	out := make([]string, 0, len(a.searchHistory)+1)
	for _, h := range a.searchHistory {
		if h != q {
			out = append(out, h)
		}
	}
	out = append(out, q)
	if len(out) > 20 {
		out = out[len(out)-20:]
	}
	a.searchHistory = out
	a.searchHistIdx = -1
}

// openSearch shows the project-wide full-text search & replace sheet over the window.
func (a *app) openSearch() {
	seed := a.selectedSearchSeed()
	a.closeModals()
	a.searchOpen = true
	a.searchSel = 0
	a.searchList = ui.ListState{}
	a.searchLastKey = ""
	a.searchNotice = ""
	a.searchHistIdx = -1
	if seed != "" {
		a.searchQuery = seed
	}
	st := a.syncModalSearchIn()
	if strings.TrimSpace(st.query) != "" {
		a.startSearchFor(st)
		a.syncModalSearchOut()
	}
	a.invalidate()
}

// closeSearch hides the search sheet and cancels any in-flight search.
func (a *app) closeSearch() {
	a.searchOpen = false
	a.cancelProjectSearch()
}

func (a *app) cancelProjectSearch() {
	st := a.syncModalSearchIn()
	cancelSearchState(st)
	a.syncModalSearchOut()
}

func cancelSearchState(st *searchState) {
	if st == nil {
		return
	}
	st.seq++
	if st.cancel != nil {
		st.cancel()
		st.cancel = nil
	}
	st.busy = false
}

func (a *app) searchOptions() session.SearchOptions {
	return session.SearchOptions{
		Query:         a.searchQuery,
		CaseSensitive: a.searchCase,
		WholeWord:     a.searchWholeWord,
		Regex:         a.searchRegex,
		Include:       a.searchInclude,
		Exclude:       a.searchExclude,
	}
}

func (a *app) searchKey() string {
	return a.syncModalSearchIn().key()
}

func (a *app) startProjectSearch() {
	st := a.syncModalSearchIn()
	a.startSearchFor(st)
	a.syncModalSearchOut()
}

func (a *app) startSearchFor(st *searchState) {
	if st == nil {
		return
	}
	cancelSearchState(st)
	st.lastKey = st.key()
	st.err = ""
	st.notice = ""
	st.sel = 0
	if st.query == "" || a.store == nil {
		st.result = session.SearchResult{}
		if st.isModal {
			a.syncModalSearchOut()
		}
		return
	}
	opts := st.options()
	overrides := a.store.SearchOverrides()
	backend := a.store.FS
	post := a.uiPoster()
	if post == nil {
		res, err := project.Search(context.Background(), backend, overrides, opts)
		if err != nil {
			st.err = err.Error()
			st.result = session.SearchResult{}
		} else {
			st.result = res
		}
		if st.isModal {
			a.syncModalSearchOut()
		}
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	st.cancel = cancel
	st.busy = true
	seq := st.seq
	winSeq := a.windowSeq
	store := a.store
	go func() {
		res, err := project.Search(ctx, backend, overrides, opts)
		if ctx.Err() != nil {
			return
		}
		post(func() {
			if a.windowSeq != winSeq || st.seq != seq || a.store != store {
				return
			}
			st.busy = false
			st.cancel = nil
			if err != nil {
				st.err = err.Error()
				st.result = session.SearchResult{}
			} else {
				st.err = ""
				st.result = res
			}
			if st.isModal {
				a.syncModalSearchOut()
			}
			a.invalidate()
		})
	}()
}

// replaceAllMatches applies searchReplace across every matched file in memory,
// keeping Mojian's explicit ⌘S save and undo semantics intact.
func (a *app) replaceAllMatches() {
	st := a.syncModalSearchIn()
	a.replaceAllInState(st)
	a.syncModalSearchOut()
}

func (a *app) replaceAllInState(st *searchState) {
	if st == nil || a.store == nil || st.query == "" || len(st.result.Matches) == 0 {
		return
	}
	seen := map[string]bool{}
	var rels []string
	for _, m := range st.result.Matches {
		if !seen[m.Rel] {
			seen[m.Rel] = true
			rels = append(rels, m.Rel)
		}
	}
	a.replaceFilesInState(st, rels)
}

func (a *app) replaceFileInState(st *searchState, rel string) {
	if st == nil || rel == "" {
		return
	}
	a.replaceFilesInState(st, []string{rel})
}

func (a *app) replaceFilesInState(st *searchState, rels []string) {
	if st == nil || a.store == nil || st.query == "" || len(rels) == 0 {
		return
	}
	opts := st.options()
	totalCount := 0
	totalFiles := 0
	for _, rel := range rels {
		n := a.store.ByRelPath(filepath.FromSlash(rel))
		if n == nil || !a.loadNote(n) {
			continue
		}
		cur := n.Text()
		updated, count, err := session.ReplaceAllText(cur, st.replace, opts)
		if err != nil {
			st.err = err.Error()
			if st.isModal {
				a.syncModalSearchOut()
			}
			a.invalidate()
			return
		}
		if count == 0 || updated == cur {
			continue
		}
		editNoteTextMinimal(n, cur, updated)
		totalCount += count
		totalFiles++
	}
	a.startSearchFor(st)
	if totalCount > 0 {
		st.notice = fmt.Sprintf("Replaced %d in %d %s · %s to save",
			totalCount, totalFiles, pluralWord(totalFiles, "file", "files"), a.commandKeys(cmdSave))
	} else {
		st.notice = "No replacements applied"
	}
	if st.isModal {
		a.syncModalSearchOut()
	}
	a.invalidate()
}

func (a *app) replaceSingleMatchInState(st *searchState, m session.SearchMatch) {
	if st == nil || a.store == nil || st.query == "" {
		return
	}
	n := a.store.ByRelPath(filepath.FromSlash(m.Rel))
	if n == nil || !a.loadNote(n) {
		return
	}
	cur := n.Text()
	updated, replaced, err := session.ReplaceOneInText(cur, m, st.replace, st.options())
	if err != nil {
		st.err = err.Error()
		if st.isModal {
			a.syncModalSearchOut()
		}
		a.invalidate()
		return
	}
	if !replaced || updated == cur {
		st.notice = "Match is no longer current"
		if st.isModal {
			a.syncModalSearchOut()
		}
		a.invalidate()
		return
	}
	editNoteTextMinimal(n, cur, updated)
	a.startSearchFor(st)
	st.notice = fmt.Sprintf("Replaced 1 in %s · %s to save", filepath.Base(m.Rel), a.commandKeys(cmdSave))
	if st.isModal {
		a.syncModalSearchOut()
	}
	a.invalidate()
}

func editNoteTextMinimal(n *note, cur, updated string) {
	if n == nil || cur == updated {
		return
	}
	prefix := 0
	minLen := min(len(cur), len(updated))
	for prefix < minLen && cur[prefix] == updated[prefix] {
		prefix++
	}
	curEnd := len(cur)
	updEnd := len(updated)
	for curEnd > prefix && updEnd > prefix && cur[curEnd-1] == updated[updEnd-1] {
		curEnd--
		updEnd--
	}
	n.Buffer().Edit(editor.Edit{
		Start: prefix,
		End:   curEnd,
		Text:  updated[prefix:updEnd],
	})
}

func buildSearchRows(matches []session.SearchMatch, collapsed map[string]bool) []searchRow {
	if len(matches) == 0 {
		return nil
	}
	var order []string
	grouped := map[string][]session.SearchMatch{}
	for _, m := range matches {
		if _, exists := grouped[m.Rel]; !exists {
			order = append(order, m.Rel)
		}
		grouped[m.Rel] = append(grouped[m.Rel], m)
	}
	rows := make([]searchRow, 0, len(matches)+len(order))
	for _, rel := range order {
		group := grouped[rel]
		isCol := collapsed != nil && collapsed[rel]
		rows = append(rows, searchRow{
			isHeader:  true,
			rel:       rel,
			count:     len(group),
			collapsed: isCol,
		})
		if !isCol {
			for _, m := range group {
				rows = append(rows, searchRow{
					rel:   rel,
					match: m,
				})
			}
		}
	}
	return rows
}

func pluralWord(n int, singular, plural string) string {
	if n == 1 {
		return singular
	}
	return plural
}

func (a *app) openSearchInPaneFromModal() {
	st := a.syncModalSearchIn().cloneForPane()
	a.closeSearch()
	a.openSearchInPane(st)
}

func (a *app) openSearchInPane(initial *searchState) {
	st := initial
	if st == nil {
		st = &searchState{query: a.selectedSearchSeed(), collapsed: map[string]bool{}}
	}
	if st.collapsed == nil {
		st.collapsed = map[string]bool{}
	}
	a.git.Open = false
	t := a.tab()
	if t != nil {
		for _, p := range t.panes() {
			if p.Search != nil {
				p.Search.state = st
				p.Search.focusInput = true
				t.Focus = p
				a.focusReq = p
				if strings.TrimSpace(st.query) != "" && st.lastKey != st.key() {
					a.startSearchFor(st)
				}
				a.invalidate()
				return
			}
		}
		if p := t.Focus; p != nil && p.Note == nil && p.Asset == nil && p.Term == nil && p.Search == nil {
			p.Search = &searchPane{state: st, focusInput: true}
			t.Focus = p
			a.focusReq = p
			if strings.TrimSpace(st.query) != "" && st.lastKey != st.key() {
				a.startSearchFor(st)
			}
			a.changed()
			a.invalidate()
			return
		}
	}
	sp := newSearchPane(st)
	if t == nil || t.Root == nil {
		nt := &tab{ID: nextID("t"), Root: &node{Pane: sp}, Focus: sp}
		a.tabs = append(a.tabs, nt)
		a.active = len(a.tabs) - 1
		a.revealTab = true
	} else {
		target := t.Focus
		if target == nil {
			target = firstPane(t.Root)
		}
		if target == nil || !t.splitSide(target, sp, splitRight) {
			nt := &tab{ID: nextID("t"), Root: &node{Pane: sp}, Focus: sp}
			a.tabs = append(a.tabs, nt)
			a.active = len(a.tabs) - 1
			a.revealTab = true
		}
	}
	a.focusReq = sp
	if strings.TrimSpace(st.query) != "" && st.lastKey != st.key() {
		a.startSearchFor(st)
	}
	a.changed()
	a.invalidate()
}

func (a *app) openSearchMatchFromPane(searchPaneRef *pane, m session.SearchMatch) {
	a.openSearchMatchFromPaneMode(searchPaneRef, m, false)
}

func (a *app) openSearchMatchFromPaneMode(searchPaneRef *pane, m session.SearchMatch, split bool) {
	if a.store == nil {
		return
	}
	n := a.store.ByRelPath(filepath.FromSlash(m.Rel))
	if n == nil {
		return
	}
	if !a.isAsyncRemote() && !a.loadNote(n) {
		return
	}
	a.recordNav()
	if split {
		a.openFileInSplit(n, nil, m.Line, m.Col)
		if p := a.focusPane(); p != nil && p.Note == n && m.End > m.Col {
			p.setPendingPos(m.Line, m.Col, m.End, 0)
			if n.Loaded {
				a.applyPendingPanePos(p)
			}
		}
		return
	}
	t := a.tabOf(searchPaneRef)
	if t == nil {
		t = a.tab()
	}
	if t != nil {
		if ep := t.firstEditorPane(); ep != nil {
			t.Focus = ep
		} else if searchPaneRef != nil {
			np := newPane(n)
			if t.splitSide(searchPaneRef, np, splitRight) {
				t.Focus = np
			}
		}
	}
	a.openNoteAt(filepath.FromSlash(m.Rel), m.Line, m.Col, m.End)
}

// searchModal draws the project-wide text search & replace sheet over the window.
func (a *app) searchModal(c *ui.Context, k *colors) {
	if !a.searchOpen {
		return
	}
	st := a.syncModalSearchIn()
	was := a.searchOpen
	chosenMatch, chosenHeader, chosenSplit := a.renderSearchUI(c, k, st, true, nil, true)
	a.syncModalSearchOut()

	if chosenHeader != "" {
		if st.collapsed == nil {
			st.collapsed = map[string]bool{}
		}
		st.collapsed[chosenHeader] = !st.collapsed[chosenHeader]
		a.syncModalSearchOut()
		a.invalidate()
	}
	if chosenMatch != nil {
		m := *chosenMatch
		a.recordSearchHistory(st.query)
		a.closeSearch()
		a.later(func() {
			a.recordNav()
			if chosenSplit {
				if n := a.store.ByRelPath(filepath.FromSlash(m.Rel)); n != nil {
					a.openFileInSplit(n, nil, m.Line, m.Col)
					if p := a.focusPane(); p != nil && p.Note == n && m.End > m.Col {
						p.setPendingPos(m.Line, m.Col, m.End, 0)
						if n.Loaded {
							a.applyPendingPanePos(p)
						}
					}
				}
				return
			}
			a.openNoteAt(filepath.FromSlash(m.Rel), m.Line, m.Col, m.End)
		})
	}
	if was && !a.searchOpen {
		a.cancelProjectSearch()
		a.restoreModalFocus()
	}
}

func (a *app) searchPaneBody(c *ui.Context, k *colors, t *tab, p *pane, overlay bool) {
	if p == nil || p.Search == nil {
		return
	}
	if p.Search.state == nil {
		p.Search.state = &searchState{collapsed: map[string]bool{}}
	}
	st := p.Search.state
	focusInput := !overlay && (p.Search.focusInput || a.focusReq == p)
	if !overlay && p.Search.focusInput {
		p.Search.focusInput = false
	}
	if !overlay && a.focusReq == p {
		a.focusReq = nil
	}
	ui.Column(c.Key("search-pane:" + p.ID)).Fill().Children(func() {
		chosenMatch, chosenHeader, chosenSplit := a.renderSearchUI(c, k, st, false, p, focusInput)
		if chosenHeader != "" {
			if st.collapsed == nil {
				st.collapsed = map[string]bool{}
			}
			st.collapsed[chosenHeader] = !st.collapsed[chosenHeader]
			a.invalidate()
		}
		if chosenMatch != nil && !overlay {
			m := *chosenMatch
			a.recordSearchHistory(st.query)
			a.later(func() { a.openSearchMatchFromPaneMode(p, m, chosenSplit) })
		}
	})
}

func (a *app) renderSearchUI(c *ui.Context, k *colors, st *searchState, isModal bool, paneRef *pane, focusInput bool) (chosenMatch *session.SearchMatch, chosenHeader string, chosenSplit bool) {
	if st.lastKey != st.key() {
		a.startSearchFor(st)
	}
	rows := buildSearchRows(st.result.Matches, st.collapsed)
	st.sel = min(max(st.sel, 0), max(len(rows)-1, 0))
	reveal := false
	chosenIdx := -1
	splitTrigger := false

	renderInner := func() {
		keyPrefix := "modal"
		if !isModal && paneRef != nil {
			keyPrefix = "pane-" + paneRef.ID
		}
		var in ui.Element
		ui.Row(c).Height(44).Padding(0, 14).Gap(6).AlignItems(ui.Center).
			BorderWidth(0, 0, 1, 0).BorderColor(k.PanelBorder).Children(func() {
			ui.Icon(c, icons.Icon("search")).Size(16, 16).TextColor(k.TextFaint)
			in = ui.TextInputBase(c.Key("search-query-"+keyPrefix), &st.query).Grow(1).MinWidth(0).
				FontSize(c.Theme().Rem(14.0 / 13)).Placeholder("Search in project…").
				Label("Search in project")
			if focusInput {
				in.AutoFocus()
			}
			if searchToggleChip(c, k, "Aa", "Match Case", st.caseSens).Clicked() {
				st.caseSens = !st.caseSens
				a.startSearchFor(st)
			}
			if searchToggleChip(c, k, "ab", "Match Whole Word", st.wholeWord).Clicked() {
				st.wholeWord = !st.wholeWord
				a.startSearchFor(st)
			}
			if searchToggleChip(c, k, ".*", "Use Regular Expression", st.regex).Clicked() {
				st.regex = !st.regex
				a.startSearchFor(st)
			}
			if searchToggleChip(c, k, "Files", "Filter files by glob", st.filterOpen || st.include != "" || st.exclude != "").Clicked() {
				st.filterOpen = !st.filterOpen
			}
			if searchToggleChip(c, k, "Replace", "Toggle Replace", st.replaceOpen).Clicked() {
				st.replaceOpen = !st.replaceOpen
			}
			if isModal {
				if iconButton(c, k, "columns-2", "Open in Pane", iconBtnSm, iconGlyphSm).
					Tooltip("Open search in a persistent pane").Clicked() {
					a.later(func() { a.openSearchInPaneFromModal() })
				}
				if iconButton(c, k, "x", "Close", iconBtnSm, iconGlyphSm).Clicked() {
					a.searchOpen = false
				}
			}
		})

		extraChrome := float32(28)
		if st.filterOpen {
			extraChrome += 36
			ui.Row(c).Height(36).Padding(0, 14).Gap(8).AlignItems(ui.Center).
				BorderWidth(0, 0, 1, 0).BorderColor(k.PanelBorder).Children(func() {
				ui.TextInputBase(c.Key("search-inc-"+keyPrefix), &st.include).Grow(1).MinWidth(0).
					FontSize(c.Theme().Rem(12.0 / 13)).Placeholder("Include (e.g. *.go, internal/**)").
					Label("Files to include")
				ui.Box(c).Width(1).Height(16).Background(k.PanelBorder)
				ui.TextInputBase(c.Key("search-exc-"+keyPrefix), &st.exclude).Grow(1).MinWidth(0).
					FontSize(c.Theme().Rem(12.0 / 13)).Placeholder("Exclude (e.g. *_test.go)").
					Label("Files to exclude")
			})
		}
		if st.replaceOpen {
			extraChrome += 38
			ui.Row(c).Height(38).Padding(0, 14).Gap(8).AlignItems(ui.Center).
				BorderWidth(0, 0, 1, 0).BorderColor(k.PanelBorder).Children(func() {
				ui.TextInputBase(c.Key("search-replace-"+keyPrefix), &st.replace).Grow(1).MinWidth(0).
					FontSize(c.Theme().Rem(13.0 / 13)).Placeholder("Replace with…").
					Label("Replace with")
				btn := ui.ButtonBase(c.Key("replace-all-"+keyPrefix)).Height(24).Padding(0, 10).Radius(radius - 2).
					Cursor(ui.CursorPointer).Label("Replace All").
					Disabled(st.busy || len(st.result.Matches) == 0).Transition(motion.Colors)
				if btn.Hovered() {
					btn.Background(k.Pressed)
				} else {
					btn.Background(k.Hover)
				}
				btn.Children(func() {
					ui.Text(c, "Replace All").FontSize(c.Theme().Rem(11.5 / 13)).FontWeight(550).TextColor(k.Text)
				})
				if btn.Clicked() {
					a.later(func() { a.replaceAllInState(st) })
				}
			})
		}
		if len(a.searchHistory) > 0 {
			switch {
			case in.Shortcut(ui.Alt, ui.KeyUp):
				if a.searchHistIdx < 0 {
					a.searchHistIdx = len(a.searchHistory) - 1
				} else if a.searchHistIdx > 0 {
					a.searchHistIdx--
				}
				st.query = a.searchHistory[a.searchHistIdx]
				a.startSearchFor(st)
			case in.Shortcut(ui.Alt, ui.KeyDown):
				if a.searchHistIdx >= 0 {
					a.searchHistIdx++
					if a.searchHistIdx >= len(a.searchHistory) {
						a.searchHistIdx = -1
						st.query = ""
					} else {
						st.query = a.searchHistory[a.searchHistIdx]
					}
					a.startSearchFor(st)
				}
			}
		}
		submit, split := listNavSplit(in, &st.sel, len(rows), &reveal)
		if split && len(rows) > 0 {
			chosenIdx = st.sel
			splitTrigger = true
		} else if submit && len(rows) > 0 {
			chosenIdx = st.sel
		}
		if st.query != "" || st.notice != "" {
			ui.Row(c).Height(28).Padding(0, 14).Gap(8).AlignItems(ui.Center).
				BorderWidth(0, 0, 1, 0).BorderColor(k.PanelBorder).Children(func() {
				switch {
				case st.busy:
					ui.Spinner(c).Size(12, 12).Label("Searching")
					ui.Text(c, "Searching…").FontSize(rowDetail).TextColor(k.TextFaint)
				case st.err != "":
					ui.Text(c, st.err).FontSize(rowDetail).TextColor(k.Danger).
						SingleLine().Ellipsis("…").Shrink(1).MinWidth(0)
				case st.notice != "":
					ui.Text(c, st.notice).FontSize(rowDetail).TextColor(k.TextMuted).
						SingleLine().Ellipsis("…").Shrink(1).MinWidth(0)
				default:
					ui.Text(c, searchSummary(st.result)).FontSize(rowDetail).TextColor(k.TextFaint)
				}
			})
		}

		st.list.Selected = &st.sel
		st.list.Key = func(i int) any {
			r := rows[i]
			if r.isHeader {
				return "hdr:" + r.rel
			}
			return fmt.Sprintf("m:%s:%d:%d", r.match.Rel, r.match.Line, r.match.Col)
		}

		opts := st.options()
		list := ui.List(c, &st.list, len(rows), func(i int) {
			r := rows[i]
			selected := i == st.sel
			if r.isHeader {
				hRow := ui.Row(c.Key("hdr:"+keyPrefix+":"+r.rel)).
					Height(30).Padding(0, rowPadX).Gap(6).AlignItems(ui.Center).
					Radius(radius).Cursor(ui.CursorPointer)
				fg, muted := k.Text, k.TextFaint
				if selected {
					fg = c.Theme().AccentText
					muted = fg.Alpha(0.88)
				} else if hRow.Hovered() {
					hRow.Background(k.Hover)
				}
				hRow.Children(func() {
					chev := "chevron-down"
					if r.collapsed {
						chev = "chevron-right"
					}
					ui.Icon(c, icons.Icon(chev)).Size(12, 12).TextColor(muted)
					ui.Icon(c, icons.FileIcon(r.rel)).Size(14, 14).TextColor(muted)
					ui.Text(c, filepath.Base(r.rel)).FontSize(rowDetail).FontWeight(600).TextColor(fg).
						SingleLine().Ellipsis("…").Shrink(0.3).MinWidth(0)
					if dir := filepath.Dir(filepath.FromSlash(r.rel)); dir != "." {
						ui.Text(c, filepath.ToSlash(dir)).FontSize(c.Theme().Rem(11.0 / 13)).TextColor(muted).
							SingleLine().TruncateStart().Shrink(1).MinWidth(0)
					}
					ui.Spacer(c)
					ui.Text(c, strconv.Itoa(r.count)).FontSize(c.Theme().Rem(10.5/13)).
						TextColor(muted).Padding(1, 6).Radius(8).Background(k.Hover).Shrink(0)
					if st.replaceOpen {
						relCopy := r.rel
						fbtn := ui.ButtonBase(c.Key("rep-file:"+keyPrefix+":"+relCopy)).Height(20).Padding(0, 7).
							Radius(radius - 2).Cursor(ui.CursorPointer).Label("Replace File " + filepath.Base(relCopy)).
							Transition(motion.Colors)
						if fbtn.Hovered() {
							fbtn.Background(k.Pressed)
						} else {
							fbtn.Background(k.Hover)
						}
						fbtn.Children(func() {
							ui.Text(c, "Replace File").FontSize(c.Theme().Rem(10.5 / 13)).TextColor(k.Text)
						})
						if fbtn.Clicked() {
							a.later(func() { a.replaceFileInState(st, relCopy) })
						}
					}
				})
				if hRow.Clicked() {
					chosenIdx = i
				}
				return
			}

			m := r.match
			mKey := fmt.Sprintf("match:%s:%s:%d:%d", keyPrefix, m.Rel, m.Line, m.Col)
			mRow := ui.Row(c.Key(mKey)).MinHeight(28).Padding(3, rowPadX, 3, rowPadX+18).Gap(8).
				AlignItems(ui.Center).Radius(radius).Cursor(ui.CursorPointer)
			fg, muted := k.Text, k.TextMuted
			if selected {
				fg = c.Theme().AccentText
				muted = fg.Alpha(0.9)
			} else if mRow.Hovered() {
				mRow.Background(k.Hover)
			}
			mRow.Children(func() {
				ui.Text(c, strconv.Itoa(m.Line)).Font(noteFont.Family).FontSize(c.Theme().Rem(11.0 / 13)).
					TextColor(muted).Width(32).Shrink(0)
				if st.replaceOpen {
					if before, oldText, newText, after, ok := session.PreviewMatchReplacement(m, st.replace, opts); ok {
						sz := c.Theme().Rem(11.5 / 13)
						ui.RichText(c,
							ui.Span{Text: before, Size: sz, Color: muted},
							ui.Span{Text: oldText, Size: sz, Weight: 600, Color: k.Danger},
							ui.Span{Text: " → ", Size: sz, Color: k.TextFaint},
							ui.Span{Text: newText, Size: sz, Weight: 600, Color: c.Theme().Accent},
							ui.Span{Text: after, Size: sz, Color: muted},
						).Font(noteFont.Family).Grow(1).MinWidth(0)
					} else {
						ui.Text(c, m.Text).Font(noteFont.Family).FontSize(rowDetail).TextColor(fg).
							SingleLine().Ellipsis("…").Grow(1).MinWidth(0)
					}
					matchCopy := m
					rbtn := ui.ButtonBase(c.Key("rep-one:"+mKey)).Height(20).Padding(0, 7).
						Radius(radius - 2).Cursor(ui.CursorPointer).Label("Replace Match").
						Transition(motion.Colors)
					if rbtn.Hovered() {
						rbtn.Background(k.Pressed)
					} else {
						rbtn.Background(k.Hover)
					}
					rbtn.Children(func() {
						ui.Text(c, "Replace").FontSize(c.Theme().Rem(10.5 / 13)).TextColor(k.Text)
					})
					if rbtn.Clicked() {
						a.later(func() { a.replaceSingleMatchInState(st, matchCopy) })
					}
				} else {
					ui.Text(c, m.Text).Font(noteFont.Family).FontSize(rowDetail).TextColor(fg).
						SingleLine().Ellipsis("…").Grow(1).MinWidth(0)
				}
			})
			if mRow.Clicked() {
				chosenIdx = i
			}
		}).Padding(6)
		if isModal {
			listH := max(sheetListHeight(c)-extraChrome, 100)
			list.MaxHeight(listH)
		} else {
			list.Grow(1).MinHeight(0)
		}
		if len(rows) == 0 && !st.busy && st.err == "" {
			msg := "Type to search across files"
			if st.query != "" {
				msg = "No matches"
			}
			list.Children(func() { emptyLine(c, k, msg) })
		}
		if reveal {
			st.list.ScrollIntoView(st.sel)
		}
	}

	if isModal {
		a.commandDialog(c, k, &a.searchOpen, renderInner)
	} else {
		renderInner()
	}

	if chosenIdx >= 0 && chosenIdx < len(rows) {
		r := rows[chosenIdx]
		if r.isHeader {
			chosenHeader = r.rel
		} else {
			m := r.match
			chosenMatch = &m
			chosenSplit = splitTrigger
		}
	}
	return chosenMatch, chosenHeader, chosenSplit
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
