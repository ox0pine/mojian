package explorer

import (
	"fmt"
	"strings"

	"mojian/internal/icons"
	"mojian/internal/motion"

	"github.com/egoist/mygo/ui"
)

// Sidebar draws the project tree beside the panes.
func (u *UI) Sidebar(c *ui.Context, k *colors) {
	if u.store() == nil {
		return
	}
	card := ui.Column(c.Key("tree:"+u.activeWorkspaceKey())).Width(treeW).Shrink(0).MinHeight(0).Radius(radius).Clip().
		Background(k.Card).Border(1, k.CardBorder).
		Shadow(0, 1, 2, 0, k.Shadow).Shadow(0, 6, 22, -2, k.Shadow).
		Transition(motion.Sidebar)
	card.Children(func() {
		u.sidebarHeader(c, k)
		u.sidebarSearch(c, k)
		if u.emptyTreeVisible() {
			u.emptyTree(c, k)
			return
		}
		scroll := ui.Scroll(c).Grow(1).MinHeight(0).Padding(2, 6)
		scroll.PopoverMenu(func(m *ui.Menu) { u.NodeMenu(m, "", true) })
		scroll.Children(func() {
			if strings.TrimSpace(u.TreeQuery()) != "" {
				u.searchResults(c, k)
				return
			}
			tree := ui.TreeBase(c)
			tree.Root.Children(func() {
				var walk func(n *fileNode, depth int)
				walk = func(n *fileNode, depth int) {
					if n == nil {
						return
					}
					if d := u.TreeDraft(); d != nil && d.Kind != DraftRename && d.Parent == n.Rel {
						u.treeDraftRow(c, k, depth, n.Dir)
					}
					for _, child := range n.Children {
						if child.Dir {
							open := u.dirOpen(child.Rel)
							c.Key("dir:" + child.Rel)
							item := tree.Item(&open)
							if open != u.dirOpen(child.Rel) {
								u.toggleDir(child.Rel)
							}
							d := u.TreeDraft()
							renaming := d != nil && d.Kind == DraftRename && d.Target == child.Rel
							u.treeFolderRow(c, k, child, depth, item.Row, renaming)
							item.Group(func() { walk(child, depth+1) })
							continue
						}
						if child.Note == nil && child.Asset == nil {
							continue
						}
						c.Key("file:" + child.Rel)
						item := tree.Item(nil)
						d := u.TreeDraft()
						renaming := d != nil && d.Kind == DraftRename && d.Target == child.Rel
						u.treeFileRow(c, k, child, depth, item.Row, renaming)
					}
				}
				walk(u.store().Tree, 0)
			})
		})
	})
	if u.TreeReveal() != "" {
		u.SetTreeReveal("")
	}
}

// emptyTreeVisible reports whether the Explorer shows its empty prompt
// instead of the scrollable tree.
func (u *UI) emptyTreeVisible() bool {
	if strings.TrimSpace(u.TreeQuery()) != "" {
		return false
	}
	tree := u.store().Tree
	return tree != nil && len(tree.Children) == 0 && u.TreeDraft() == nil
}

func (u *UI) sidebarHeader(c *ui.Context, k *colors) {
	ui.Row(c).Height(headerH).Padding(0, 7, 0, 11).Gap(6).AlignItems(ui.Center).
		PopoverMenu(func(m *ui.Menu) { u.NodeMenu(m, "", true) }).Children(func() {
		ui.Icon(c, icons.FolderIcon()).Size(16, 16).TextColor(k.TextMuted)
		ui.Text(c, "Explorer").FontSize(12.5).FontWeight(600).TextColor(k.Text)
		ui.Spacer(c)
		ui.Toolbar(c, func() {
			if iconButton(c, k, "file-plus", "New File", 22, 13).
				Tooltip(u.tip("New File", "new-file")).Clicked() {
				u.later(func() { u.StartNewFile() })
			}
			if iconButton(c, k, "folder-plus", "New Folder", 22, 13).
				Tooltip(u.tip("New Folder", "new-folder")).Clicked() {
				u.later(func() { u.StartNewFolder() })
			}
			if u.refreshing() {
				ui.Spinner(c).Size(14, 14).Label("Refreshing project")
			} else if iconButton(c, k, "refresh-cw", "Refresh Project", 22, 13).
				Tooltip("Refresh Project").Clicked() {
				u.later(func() { u.RefreshProject() })
			}
		}).Label("Explorer actions").Padding(0).Gap(1)
	})
}

func (u *UI) sidebarSearch(c *ui.Context, k *colors) {
	field := ui.SearchField(c.Key("tree-search"), u.TreeQueryPtr()).
		Height(32).Margin(0, 9, 6, 9).Padding(0, 9).Gap(6).Radius(radius).
		Background(k.Hover).Border(0, ui.Transparent).FontSize(13).
		Placeholder("Search files…").Label("Search files").Bind(u.TreeSearch())
	if field.Submitted() {
		if notes := u.filteredNotes(); len(notes) > 0 {
			u.SetTreeSel(notes[0].Rel)
			u.openNote(notes[0])
		}
	}
}

// treeFolderRow styles the native tree item of a folder and gives it its
// contents, or the field renaming it.
func (u *UI) treeFolderRow(c *ui.Context, k *colors, n *fileNode, depth int, row ui.Element, renaming bool) {
	row.Height(rowH).PaddingStart(5 + float32(depth)*13).PaddingEnd(6).Gap(5).Radius(radius).
		AlignItems(ui.Center).Cursor(ui.CursorPointer).Label(n.Name).ClipX().
		PopoverMenu(func(m *ui.Menu) { u.NodeMenu(m, n.Rel, true) }).
		Transition(motion.Row)
	if renaming {
		row.Background(k.PanelSel)
		u.treeRenameInput(c, k, true)
		return
	}
	if n.Rel == u.TreeSel() {
		row.Background(k.PanelSel)
	} else if row.Hovered() {
		row.Background(k.Hover)
	}
	if row.Clicked() {
		u.toggleDir(n.Rel)
	}
	u.treeRowKeys(row, n.Rel)
	row.Children(func() {
		arrow := "chevron-right"
		if u.dirOpen(n.Rel) {
			arrow = "chevron-down"
		}
		ui.Icon(c, icons.Icon(arrow)).Size(12, 12).TextColor(k.TextFaint)
		ui.Icon(c, icons.FolderIcon()).Size(15, 15).TextColor(k.TextMuted)
		ui.Text(c, n.Name).FontSize(12.5).FontWeight(600).TextColor(k.Text).
			SingleLine().Ellipsis("…").Shrink(1).MinWidth(0)
	})
}

// treeFileRow styles the native tree item of one text file, or the field
// renaming it.
func (u *UI) treeFileRow(c *ui.Context, k *colors, n *fileNode, depth int, row ui.Element, renaming bool) {
	path, title := n.Name, n.Name
	if n.Note != nil {
		path, title = n.Note.Path, n.Note.Title
	} else {
		path = n.Asset.Path
	}
	row.Height(rowH).PaddingStart(5 + float32(depth)*13).PaddingEnd(6).Gap(5).Radius(radius).
		AlignItems(ui.Center).Cursor(ui.CursorPointer).Label(title).
		Tooltip(title + " — " + n.Rel).ClipX().
		PopoverMenu(func(m *ui.Menu) { u.NodeMenu(m, n.Rel, false) }).
		Transition(motion.Row)
	if renaming {
		row.Background(k.PanelSel)
		u.treeRenameInput(c, k, false)
		return
	}
	if n.Rel == u.TreeSel() {
		row.Background(k.PanelSel)
	} else if n.Note != nil && u.currentNote() == n.Note {
		row.Background(k.Hover)
	} else if row.Hovered() {
		row.Background(k.Hover)
	}
	if row.Clicked() {
		u.SetTreeSel(n.Rel)
		u.SetTreeDraft(nil)
		u.openRel(n.Rel)
	}
	u.treeRowKeys(row, n.Rel)
	if n.Rel == u.TreeReveal() {
		row.ScrollIntoView()
	}
	row.Children(func() {
		ui.Box(c).Size(12, 12).Shrink(0)
		ui.Icon(c, icons.FileIcon(path)).Size(15, 15).TextColor(k.TextMuted)
		ui.Text(c, n.Name).FontSize(12.5).FontWeight(500).TextColor(k.Text).
			SingleLine().Ellipsis("…").Shrink(1).MinWidth(0)
		ui.Spacer(c)
		if n.Note != nil && n.Note.Dirty() {
			ui.Box(c).Size(6, 6).Radius(3).Background(k.Attention).Shrink(0)
		}
	})
}

// treeRenameInput fills a tree item with the field renaming its entry.
func (u *UI) treeRenameInput(c *ui.Context, k *colors, dir bool) {
	d := u.TreeDraft()
	if d == nil {
		return
	}
	ui.Box(c).Size(12, 12).Shrink(0)
	ic := icons.FileIcon(d.Name)
	if dir {
		ic = icons.FolderIcon()
	}
	ui.Icon(c, ic).Size(15, 15).TextColor(k.TextMuted)
	u.treeDraftInput(c, d, "New name")
}

// treeRowKeys gives a focused row the keyboard commands of the Explorer.
func (u *UI) treeRowKeys(row ui.Element, rel string) {
	if row.Shortcut(0, ui.KeyF2) {
		u.later(func() { u.StartRenameNode(rel) })
	}
	if row.Shortcut(ui.Cmd, ui.KeyC) {
		u.CopySelectionTo(rel, false)
	}
	if row.Shortcut(ui.Cmd, ui.KeyX) {
		u.CopySelectionTo(rel, true)
	}
	if row.Shortcut(ui.Cmd, ui.KeyV) {
		u.later(func() { u.PasteInto(rel) })
	}
	if row.Shortcut(ui.Cmd, ui.KeyBackspace) {
		u.later(func() { u.Trash(rel) })
	}
}

// treeDraftRow is the field naming a new file or folder, or renaming an
// entry in place.
func (u *UI) treeDraftRow(c *ui.Context, k *colors, depth int, dir bool) {
	d := u.TreeDraft()
	if d == nil {
		return
	}
	row := ui.Row(c.Key(fmt.Sprintf("draft:%d", d.Gen))).Height(rowH).
		PaddingStart(5 + float32(depth)*13).PaddingEnd(6).Gap(5).Radius(radius).
		AlignItems(ui.Center).Background(k.PanelSel)
	row.Children(func() {
		ui.Box(c).Size(12, 12).Shrink(0)
		ic := icons.FileIcon(d.Name)
		if dir {
			ic = icons.FolderIcon()
		}
		ui.Icon(c, ic).Size(15, 15).TextColor(k.TextMuted)
		placeholder := "Folder name"
		switch d.Kind {
		case DraftFile:
			placeholder = "file name, e.g. app.py"
		case DraftRename:
			placeholder = "New name"
		}
		u.treeDraftInput(c, d, placeholder)
	})
}

// treeDraftInput is the field naming a draft entry, shared by a new file
// or folder row and the row renaming an entry.
func (u *UI) treeDraftInput(c *ui.Context, d *Draft, placeholder string) {
	in := ui.TextInputBase(c, &d.Name).Grow(1).MinWidth(0).FontSize(12.5).
		Placeholder(placeholder).AutoFocus()
	if in.Focused() {
		d.Focused = true
	}
	switch {
	case in.Shortcut(0, ui.KeyEscape):
		u.CancelTreeDraft()
	case in.Submitted():
		u.later(func() { u.SubmitTreeDraft() })
	case d.Focused && !in.Focused():
		if d.Kind == DraftRename {
			u.later(func() { u.SubmitTreeDraft() })
		} else {
			u.CancelTreeDraft()
		}
	}
}

// emptyTree is shown for a project or folder with no text files yet. It
// fills the space below the header so its prompt sits in the middle.
func (u *UI) emptyTree(c *ui.Context, k *colors) {
	ui.Column(c).Grow(1).MinHeight(0).Padding(18, 8).Gap(8).Center().
		PopoverMenu(func(m *ui.Menu) { u.NodeMenu(m, "", true) }).
		Children(func() {
			ui.Box(c).Size(40, 40).Radius(radius).Background(k.Hover).Center().
				Children(func() {
					ui.Icon(c, icons.Icon("file-plus")).Size(20, 20).TextColor(k.TextFaint)
				})
			ui.Text(c, "No files here").FontSize(13).FontWeight(600).TextColor(k.Text)
			if ui.Button(c, "New File").Clicked() {
				u.later(func() { u.StartNewFile() })
			}
		})
}

// searchResults lists the files matching the search field.
func (u *UI) searchResults(c *ui.Context, k *colors) {
	notes := u.filteredNotes()
	if len(notes) == 0 {
		ui.Text(c, "No files match").FontSize(12.5).TextColor(k.TextFaint).Padding(10, 8)
		return
	}
	for _, n := range notes {
		n := n
		row := ui.Row(c.Key("result:"+n.Rel)).Height(34).Padding(0, 8).Gap(10).Radius(radius).
			AlignItems(ui.Center).Cursor(ui.CursorPointer).Role(ui.RoleTreeItem).
			Focusable().FocusRing(false).Label(n.Title).
			PopoverMenu(func(m *ui.Menu) { u.NodeMenu(m, n.Rel, false) })
		if n.Rel == u.TreeSel() {
			row.Background(k.PanelSel)
		} else if row.Hovered() {
			row.Background(k.Hover)
		}
		if row.Clicked() {
			u.SetTreeSel(n.Rel)
			u.openNote(n)
		}
		u.treeRowKeys(row, n.Rel)
		row.Children(func() {
			ui.Icon(c, icons.FileIcon(n.Path)).Size(15, 15).TextColor(k.TextMuted)
			ui.Column(c).Grow(1).MinWidth(0).Children(func() {
				ui.Text(c, n.Title).FontSize(13).FontWeight(500).TextColor(k.Text).
					SingleLine().Ellipsis("…")
				ui.Text(c, n.Rel).FontSize(12).TextColor(k.TextFaint).
					SingleLine().Ellipsis("…")
			})
			if n.Dirty() {
				ui.Box(c).Size(6, 6).Radius(3).Background(k.Attention).Shrink(0)
			}
		})
	}
}
