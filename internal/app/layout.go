package app

import (
	"encoding/json"
	"log"
	"os"

	"mojian/internal/fs"
)

// layoutFile is what the app keeps of its tabs between launches.
type layoutFile struct {
	Tabs   []layoutTab `json:"tabs"`
	Active int         `json:"active"`
}

type layoutTab struct {
	Name  string      `json:"name,omitempty"`
	Root  *layoutNode `json:"root,omitempty"`
	Focus string      `json:"focus,omitempty"`
	Zoom  string      `json:"zoom,omitempty"`
	// Columns and Panes are the grid formats written just before the
	// split tree came back; they are read so their panes survive.
	Columns [][]layoutPane `json:"columns,omitempty"`
	Panes   []layoutPane   `json:"panes,omitempty"`
}

type layoutNode struct {
	Vertical bool        `json:"vertical,omitempty"`
	Share    float32     `json:"share,omitempty"`
	A        *layoutNode `json:"a,omitempty"`
	B        *layoutNode `json:"b,omitempty"`
	Pane     *layoutPane `json:"pane,omitempty"`
}

type layoutPane struct {
	ID       string          `json:"id"`
	Note     string          `json:"note,omitempty"`
	Asset    string          `json:"asset,omitempty"`
	Preview  bool            `json:"preview,omitempty"`
	Line     int             `json:"line,omitempty"`
	Col      int             `json:"col,omitempty"`
	ScrollY  float64         `json:"scrollY,omitempty"`
	Terminal *layoutTerminal `json:"terminal,omitempty"`
}

type layoutTerminal struct {
	SID    string `json:"sid,omitempty"`
	Host   string `json:"host,omitempty"`
	Server string `json:"server,omitempty"`
}

func layoutOf(t *tab) layoutTab {
	lt := layoutTab{Name: t.Name, Root: layoutNodeOf(t.Root)}
	if t.Focus != nil && !t.Focus.closing {
		lt.Focus = t.Focus.ID
	}
	if t.Zoom != nil && !t.Zoom.closing {
		lt.Zoom = t.Zoom.ID
	}
	return lt
}

func layoutNodeOf(n *node) *layoutNode {
	if n == nil {
		return nil
	}
	if n.Pane != nil {
		lp := layoutPaneOf(n.Pane)
		if lp == nil {
			return nil
		}
		return &layoutNode{Pane: lp}
	}
	return &layoutNode{
		Vertical: n.Vertical,
		Share:    n.Share,
		A:        layoutNodeOf(n.A),
		B:        layoutNodeOf(n.B),
	}
}

func layoutPaneOf(p *pane) *layoutPane {
	if p == nil || p.closing {
		return nil
	}
	lp := &layoutPane{ID: p.ID, Preview: p.Preview}
	switch {
	case p.Term != nil:
		lp.Terminal = &layoutTerminal{SID: p.Term.SID, Host: p.Term.Spec.Host, Server: p.Term.Spec.Server}
	case p.Note != nil:
		lp.Note = p.Note.Rel
		if p.state != nil {
			pos := p.state.CursorPos()
			lp.Line = pos.Line
			lp.Col = pos.Col
			lp.ScrollY = p.state.ScrollY()
		} else if p.hasPendingPos || p.hasPendingScroll {
			if p.hasPendingPos {
				lp.Line = max(p.pendingLine-1, 0)
				lp.Col = max(p.pendingCol, 0)
			}
			lp.ScrollY = p.pendingScroll
		}
	case p.Asset != nil:
		lp.Asset = p.Asset.Rel
	default:
		return nil
	}
	return lp
}

func (a *app) snapshotLayout() layoutFile {
	f := layoutFile{Active: a.active}
	for _, t := range a.tabs {
		f.Tabs = append(f.Tabs, layoutOf(t))
	}
	return f
}

type layoutRestoreStatus uint8

const (
	layoutRestoreMissing layoutRestoreStatus = iota
	layoutRestoreInvalid
	layoutRestoreEmpty
	layoutRestoreOK
)

func (s layoutRestoreStatus) needsFallback() bool {
	return s == layoutRestoreMissing || s == layoutRestoreEmpty
}

// restoreLayout is the boolean form used by older callers. New open paths
// use restoreLayoutStatus so an invalid file is not mistaken for a missing one.
func (a *app) restoreLayout() bool {
	return a.restoreLayoutStatus() == layoutRestoreOK
}

// restoreLayoutStatus builds the tabs of the saved layout, dropping panes whose
// note is no longer in the vault. Empty tabs are real tabs; a nonempty tab whose
// panes all disappeared is skipped and the active index follows its saved tab.
func (a *app) restoreLayoutStatus() layoutRestoreStatus {
	if a.layoutPath == "" {
		return layoutRestoreMissing
	}
	data, err := os.ReadFile(a.layoutPath)
	if err != nil {
		if os.IsNotExist(err) {
			return layoutRestoreMissing
		}
		return layoutRestoreInvalid
	}
	var f layoutFile
	if err := json.Unmarshal(data, &f); err != nil {
		return layoutRestoreInvalid
	}
	byID := map[string]*pane{}
	active := -1
	afterActive := -1
	beforeActive := -1
	for i, lt := range f.Tabs {
		t := &tab{ID: nextID("t"), Name: lt.Name}
		hasContent := lt.Root != nil || len(lt.Columns) > 0 || len(lt.Panes) > 0
		switch {
		case lt.Root != nil:
			t.Root = a.restoreNode(lt.Root, byID)
		case len(lt.Columns) > 0:
			t.Root = a.restoreColumns(lt.Columns, byID)
		case len(lt.Panes) > 0:
			// The old one-row format: every pane was a column.
			var cols [][]layoutPane
			for _, lp := range lt.Panes {
				cols = append(cols, []layoutPane{lp})
			}
			t.Root = a.restoreColumns(cols, byID)
		}
		if hasContent && t.Root == nil {
			continue
		}
		if p := byID[lt.Focus]; p != nil && t.contains(p) {
			t.Focus = p
		} else {
			t.Focus = firstPane(t.Root)
		}
		if p := byID[lt.Zoom]; p != nil && t.contains(p) {
			t.Zoom = p
		}
		if i == f.Active {
			active = len(a.tabs)
		} else if i > f.Active && afterActive < 0 {
			afterActive = len(a.tabs)
		} else if i < f.Active {
			beforeActive = len(a.tabs)
		}
		a.tabs = append(a.tabs, t)
	}
	if len(a.tabs) == 0 {
		return layoutRestoreEmpty
	}
	switch {
	case active >= 0:
		a.active = active
	case afterActive >= 0:
		a.active = afterActive
	case beforeActive >= 0:
		a.active = beforeActive
	default:
		a.active = 0
	}
	return layoutRestoreOK
}

func (a *app) restoreNode(ln *layoutNode, byID map[string]*pane) *node {
	if ln == nil {
		return nil
	}
	if ln.Pane != nil {
		p := a.restorePane(*ln.Pane, byID)
		if p == nil {
			return nil
		}
		return &node{Pane: p}
	}
	left := a.restoreNode(ln.A, byID)
	right := a.restoreNode(ln.B, byID)
	switch {
	case left == nil && right == nil:
		return nil
	case left == nil:
		return right
	case right == nil:
		return left
	}
	return &node{Vertical: ln.Vertical, Share: layoutShare(ln.Share), A: left, B: right}
}

// layoutShare reads a saved split share, giving layouts from before the
// dividers half.
func layoutShare(share float32) float32 {
	if share <= 0 || share >= 100 {
		return 50
	}
	return share
}

// restoreColumns turns the old grid into a split tree: columns side by
// side, each column's panes stacked.
func (a *app) restoreColumns(cols [][]layoutPane, byID map[string]*pane) *node {
	var columns []*node
	for _, lc := range cols {
		var panes []*node
		for _, lp := range lc {
			if p := a.restorePane(lp, byID); p != nil {
				panes = append(panes, &node{Pane: p})
			}
		}
		if len(panes) > 0 {
			columns = append(columns, joinNodes(panes, true))
		}
	}
	return joinNodes(columns, false)
}

func joinNodes(ns []*node, vertical bool) *node {
	if len(ns) == 0 {
		return nil
	}
	root := ns[0]
	for _, n := range ns[1:] {
		root = &node{Vertical: vertical, Share: 50, A: root, B: n}
	}
	return root
}

func (a *app) restorePane(lp layoutPane, byID map[string]*pane) *pane {
	p := &pane{ID: nextID("p"), Preview: lp.Preview}
	switch {
	case lp.Terminal != nil:
		if !terminalsSupported() {
			return nil
		}
		if !remoteSupported() && (lp.Terminal.Host != "" || (lp.Terminal.Server != "" && lp.Terminal.Server != localServerID)) {
			return nil
		}
		p.Term = newTermPaneSpec(termSpec{Host: lp.Terminal.Host, Server: lp.Terminal.Server}, lp.Terminal.SID)
	case lp.Note != "":
		n := a.store.ByRelPath(lp.Note)
		if n == nil {
			return nil
		}
		if !a.isAsyncRemote() && !a.loadNote(n) {
			return nil
		}
		p.Note = n
		if lp.Line > 0 || lp.Col > 0 || lp.ScrollY > 0 {
			p.setPendingPos(lp.Line+1, lp.Col, 0, lp.ScrollY)
		}
		if a.isAsyncRemote() && !n.Loaded {
			a.startLoadNote(p, n)
		}
	case lp.Asset != "":
		f := a.store.AssetByRel(lp.Asset)
		if f == nil {
			return nil
		}
		if !a.isAsyncRemote() && !a.loadAsset(f) {
			return nil
		}
		p.Asset = f
		p.assetView = &assetView{}
		p.Preview = false
		if a.isAsyncRemote() && !f.IsBinary() && f.Local == "" {
			a.startLoadAsset(p, f)
		}
	default:
		return nil
	}
	byID[lp.ID] = p
	return p
}

// saveLayout writes the tabs to the app's data directory. Callers that
// clear layoutDirty or leave the current project must handle the error.
func (a *app) saveLayout() error {
	if a.layoutPath == "" {
		return nil
	}
	data, err := json.MarshalIndent(a.snapshotLayout(), "", "  ")
	if err != nil {
		log.Print(err)
		return err
	}
	if err := fs.WriteFileAtomic(a.layoutPath, data); err != nil {
		log.Print(err)
		return err
	}
	return nil
}
