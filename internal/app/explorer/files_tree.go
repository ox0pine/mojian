package explorer

import (
	"errors"
	"path/filepath"
	"strings"
)

// startTreeDraft opens a name field in the tree.
func (u *UI) startTreeDraft(kind DraftKind, parent, target, name string) {
	if u.store() == nil {
		return
	}
	if u.store().IsMemory() && kind != DraftRename {
		u.fail(errors.New("files need the desktop app"))
		return
	}
	u.SetTreeOpen(true)
	u.SetTreeQuery("")
	u.TreeSearch().CancelFocus()
	if kind == DraftRename {
		parent = filepath.Dir(target)
		if parent == "." {
			parent = ""
		}
	} else {
		u.SetTreeSel(parent)
	}
	u.OpenPath(parent)
	seq := u.NextDraftSeq()
	u.SetTreeDraft(&Draft{Kind: kind, Parent: parent, Target: target, Name: name, Gen: seq})
	u.SetTreeReveal(target)
	u.invalidate()
}

func (u *UI) StartNewFile() {
	u.startTreeDraft(DraftFile, u.selectedDir(), "", "")
}

func (u *UI) StartNewFileIn(dir string) {
	u.startTreeDraft(DraftFile, dir, "", "")
}

func (u *UI) StartNewFolder() {
	u.startTreeDraft(DraftFolder, u.selectedDir(), "", "")
}

func (u *UI) StartRenameNode(rel string) {
	if rel == "" || u.store() == nil {
		return
	}
	abs, err := u.store().AbsRel(rel)
	if err != nil {
		u.fail(err)
		return
	}
	u.startTreeDraft(DraftRename, "", rel, filepath.Base(abs))
}

func (u *UI) CancelTreeDraft() {
	u.SetTreeDraft(nil)
	u.invalidate()
}

// SubmitTreeDraft creates the named file or folder, or renames the target.
func (u *UI) SubmitTreeDraft() {
	u.submitTreeDraft(u.TreeDraft())
}

// submitTreeDraft commits the draft an input captured, even if a click has
// already cleared the global draft before its deferred blur action runs.
func (u *UI) submitTreeDraft(d *Draft) {
	if d == nil || u.store() == nil {
		return
	}
	name := strings.TrimSpace(d.Name)
	if d.Kind == DraftRename && name == "" {
		if u.TreeDraft() == d {
			u.SetTreeDraft(nil)
		}
		return
	}
	var (
		rel     string
		n       *note
		err     error
		changed []*note
	)
	before := map[*note]string{}
	for _, n := range u.store().Notes {
		before[n] = editorShape(n.Path)
	}
	switch d.Kind {
	case DraftFile:
		if name == "" {
			name = "untitled.txt"
		}
		n, err = u.store().NewFileIn(d.Parent, name, "")
		if n != nil {
			rel = n.Rel
		}
	case DraftFolder:
		if name == "" {
			name = "New Folder"
		}
		rel, err = u.store().NewFolder(d.Parent, name)
	case DraftRename:
		rel, changed, err = u.store().Rename(d.Target, name)
	}
	if err != nil {
		u.fail(err)
		return
	}
	active := u.TreeDraft() == d
	if active {
		u.SetTreeDraft(nil)
	}
	if d.Kind == DraftFolder {
		if u.OpenDirs() == nil {
			u.SetOpenDirs(map[string]bool{})
		}
		u.OpenDirs()[rel] = true
	}
	if d.Kind == DraftRename {
		u.remapOpenDirs(d.Target, rel)
	}
	if active {
		// A newer draft keeps its input and focus, and a row clicked
		// during blur keeps its selection instead of the old draft's.
		u.SetTreeSel(rel)
		u.OpenPath(filepath.Dir(rel))
		u.SetTreeReveal(rel)
		if d.Kind == DraftFile && n != nil {
			u.openNote(n)
		}
	}
	if d.Kind == DraftRename {
		u.resetRenamedStates(changed, before)
		u.NotifyFileChange(d.Target, 3)
		u.NotifyFileChange(rel, 1)
	} else if d.Kind == DraftFile && rel != "" {
		u.NotifyFileChange(rel, 1)
	}
	u.changed()
	u.invalidate()
}

// selectedDir is the folder new files and folders go into: the selected
// folder, the parent of the selected file, or the project root.
