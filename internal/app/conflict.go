package app

import (
	"context"
	"errors"
	"path/filepath"

	"mojian/internal/icons"
	"mojian/internal/motion"
	"mojian/internal/project"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
)

// saveConflict holds the state of an external-modification conflict dialog
// comparing the file currently on disk against the user's unsaved buffer.
type saveConflict struct {
	note      *note
	rel       string
	diskText  string
	localText string
	diff      *editor.DiffState
	split     bool
	busy      bool
	err       string
}

// handleSaveError reports a save error and opens the visual conflict resolver
// when a file was modified outside Mojian.
func (a *app) handleSaveError(err error) {
	if err == nil {
		return
	}
	a.fail(err)
	var ce *project.FileChangedError
	if errors.As(err, &ce) && a.store != nil {
		if n := a.store.ByRelPath(ce.Rel); n != nil {
			a.openSaveConflict(n, ce.Disk)
		}
	}
}

func (a *app) openSaveConflict(n *note, diskText string) {
	if n == nil {
		return
	}
	localText := n.Text()
	oldBuf := editor.NewBuffer(diskText)
	newBuf := editor.NewBuffer(localText)
	lang := editor.LanguageFor(n.Path)
	oldBuf.SetLanguage(lang)
	newBuf.SetLanguage(lang)
	a.conflict = &saveConflict{
		note:      n,
		rel:       n.Rel,
		diskText:  diskText,
		localText: localText,
		split:     true,
		diff: &editor.DiffState{
			Old:        oldBuf,
			New:        newBuf,
			Layout:     editor.DiffSplit,
			Inline:     editor.DiffInlineWordAlt,
			Indicators: editor.DiffBars,
			Wrap:       true,
		},
	}
	a.invalidate()
}

func (a *app) closeSaveConflict() {
	a.conflict = nil
	a.restoreModalFocus()
	a.invalidate()
}

func (a *app) resolveConflictReload() {
	cf := a.conflict
	if cf == nil || cf.busy || a.store == nil || cf.note == nil {
		return
	}
	a.store.ReloadNote(cf.note, cf.diskText)
	if isEditorConfigPath(cf.rel) {
		a.invalidateEditorConfig()
	}
	a.conflict = nil
	a.err = ""
	a.invalidateDiffBases()
	a.refreshGitStatus()
	a.restoreModalFocus()
	a.invalidate()
}

func (a *app) resolveConflictOverwrite() {
	cf := a.conflict
	if cf == nil || cf.busy || a.store == nil || cf.note == nil {
		return
	}
	s := a.store
	n := cf.note
	if !s.IsRemote() || (a.win == nil && a.uiPost == nil) {
		if err := s.ForceSave(n); err != nil {
			cf.err = err.Error()
			a.invalidate()
			return
		}
		if isEditorConfigPath(n.Rel) {
			a.invalidateEditorConfig()
		}
		a.notifyLSPSaved(n)
		a.conflict = nil
		a.err = ""
		a.refreshGitStatus()
		if a.git.Open && !a.NoGitAutoRefresh {
			a.refreshGit()
		}
		a.restoreModalFocus()
		a.invalidate()
		return
	}
	snapshot, changed, err := s.SnapshotSave(n)
	if err != nil {
		cf.err = err.Error()
		a.invalidate()
		return
	}
	if !changed {
		a.conflict = nil
		a.err = ""
		a.invalidate()
		return
	}
	cf.busy = true
	cf.err = ""
	post := a.uiPoster()
	winSeq := a.windowSeq
	go func() {
		werr := s.WriteSnapshotForce(context.Background(), snapshot)
		post(func() {
			if a.windowSeq != winSeq || a.conflict != cf {
				return
			}
			cf.busy = false
			if werr != nil {
				cf.err = werr.Error()
				a.invalidate()
				return
			}
			s.CommitSnapshot(snapshot)
			if isEditorConfigPath(n.Rel) {
				a.invalidateEditorConfig()
			}
			a.notifyLSPSaved(n)
			a.conflict = nil
			a.err = ""
			a.refreshGitStatus()
			if a.git.Open && !a.NoGitAutoRefresh {
				a.refreshGit()
			}
			a.restoreModalFocus()
			a.invalidate()
		})
	}()
	a.invalidate()
}

// conflictModal draws a centered diff dialog when an explicit save detects
// that the file changed on disk outside Mojian.
func (a *app) conflictModal(c *ui.Context, k *colors) {
	cf := a.conflict
	if cf == nil {
		return
	}
	cf.diff.Theme = a.EditorTheme(c)
	cf.diff.Font = a.editorFont()
	cf.diff.LineHeight = float32(a.lineHeight())
	cf.diff.TabSize = a.tabSize()
	if cf.split {
		cf.diff.Layout = editor.DiffSplit
	} else {
		cf.diff.Layout = editor.DiffUnified
	}

	ui.Overlay(c, func() {
		back := ui.Box(c.Key("conflict-backdrop")).Fill().Background(k.Backdrop).Center().Padding(24).
			Transition(motion.Backdrop)
		if back.Shortcut(0, ui.KeyEscape) && !cf.busy {
			a.later(func() { a.closeSaveConflict() })
		}
		back.Children(func() {
			vw, vh := back.Bounds().W, back.Bounds().H
			if vw <= 0 {
				vw = 960
			}
			if vh <= 0 {
				vh = 640
			}
			w := min(max(vw-64, 480), 820)
			h := min(max(vh-84, 340), 560)
			card := a.surface(ui.Column(c.Key("conflict-card")).Width(w).Height(h).Radius(radius+2).Clip(), k.Panel).
				Border(1, k.PanelBorder).
				Shadow(0, 14, 36, 0, k.ShadowDialog).Transition(motion.Dialog)
			card.Children(func() {
				// Header
				ui.Row(c).Height(44).Padding(0, 12, 0, 14).Gap(8).AlignItems(ui.Center).
					BorderWidth(0, 0, 1, 0).BorderColor(k.PanelBorder).Children(func() {
					ui.Icon(c, icons.FileIcon(cf.rel)).Size(16, 16).TextColor(k.TextMuted)
					ui.Text(c, filepath.Base(cf.rel)+" changed on disk").
						FontSize(c.Theme().Rem(13.0 / 13)).FontWeight(600).TextColor(k.Text).
						SingleLine().Ellipsis("…").Shrink(1).MinWidth(0)
					ui.Text(c, filepath.ToSlash(cf.rel)).
						FontSize(c.Theme().Rem(11.5 / 13)).TextColor(k.TextFaint).
						SingleLine().TruncateStart().Shrink(1).MinWidth(0)
					ui.Spacer(c)
					modeLabel := "Split"
					if !cf.split {
						modeLabel = "Unified"
					}
					if ui.Button(c, modeLabel).Clicked() {
						cf.split = !cf.split
					}
					if iconButton(c, k, "x", "Close Conflict Dialog", iconBtnMd, iconGlyphMd).
						Disabled(cf.busy).Clicked() {
						a.later(func() { a.closeSaveConflict() })
					}
				})

				// Explanation bar
				ui.Row(c).Padding(8, 14).Gap(8).AlignItems(ui.Center).
					Background(k.Hover).BorderWidth(0, 0, 1, 0).BorderColor(k.PanelBorder).Children(func() {
					ui.Text(c, "Disk version (left) vs. your unsaved edits (right). Choose how to resolve:").
						FontSize(c.Theme().Rem(11.5 / 13)).TextColor(k.TextMuted)
				})

				// Diff viewer
				ui.Box(c).Grow(1).MinHeight(0).Children(func() {
					editor.DiffView(c, cf.diff).Fill()
				})

				// Error line if any
				if cf.err != "" {
					ui.Row(c).Padding(6, 14).Background(k.Danger.Alpha(0.1)).Children(func() {
						ui.Text(c, cf.err).FontSize(c.Theme().Rem(11.5 / 13)).TextColor(k.Danger)
					})
				}

				// Footer buttons
				ui.Row(c).Padding(10, 14).Gap(8).AlignItems(ui.Center).
					BorderWidth(1, 0, 0, 0).BorderColor(k.PanelBorder).Children(func() {
					if ui.Button(c, "Keep Editing").Disabled(cf.busy).Clicked() {
						a.later(func() { a.closeSaveConflict() })
					}
					ui.Spacer(c)
					if ui.Button(c, "Reload from Disk").Disabled(cf.busy).Clicked() {
						a.later(func() { a.resolveConflictReload() })
					}
					overwriteLabel := "Overwrite Disk"
					if cf.busy {
						overwriteLabel = "Saving…"
					}
					if ui.Button(c, overwriteLabel).Disabled(cf.busy).Clicked() {
						a.later(func() { a.resolveConflictOverwrite() })
					}
				})
			})
		})
	})
}
