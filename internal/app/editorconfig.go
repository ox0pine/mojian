package app

import (
	"path"
	"path/filepath"
	"strings"

	"mojian/internal/editorconfig"

	"github.com/egoist/mygo/ui/editor"
)

// ecPropsFor resolves .editorconfig properties for n, caching per workspace.
func (a *app) ecPropsFor(n *note) editorconfig.Props {
	if a.workspace == nil || a.store == nil || a.store.IsMemory() || n == nil || n.Rel == "" {
		return editorconfig.Props{}
	}
	rel := filepath.ToSlash(n.Rel)
	if a.ecCache == nil {
		a.ecCache = map[string]editorconfig.Props{}
	}
	if props, ok := a.ecCache[rel]; ok {
		return props
	}
	if a.ecFiles == nil {
		a.ecFiles = map[string][]byte{}
	}
	read := func(ecRel string) ([]byte, bool) {
		if data, ok := a.ecFiles[ecRel]; ok {
			return data, data != nil
		}
		osRel := filepath.FromSlash(ecRel)
		if ecNote := a.store.ByRelPath(osRel); ecNote != nil {
			if ecNote.Loaded || !a.store.IsRemote() {
				data := []byte(ecNote.Text())
				a.ecFiles[ecRel] = data
				return data, true
			}
		}
		// Only read from disk directly for local projects to keep UI frames non-blocking.
		if !a.store.IsRemote() {
			if data, err := a.store.FS.Read(osRel); err == nil {
				a.ecFiles[ecRel] = data
				return data, true
			}
		}
		a.ecFiles[ecRel] = nil
		return nil, false
	}
	props := editorconfig.Resolve(rel, read)
	a.ecCache[rel] = props
	return props
}

// invalidateEditorConfig drops cached .editorconfig resolutions for the active workspace.
func (a *app) invalidateEditorConfig() {
	if a.workspace == nil {
		return
	}
	a.ecCache = nil
	a.ecFiles = nil
}

// effectiveIndent resolves indentation style and tab width for a pane/note:
// explicit per-pane override > .editorconfig > global settings.
func (a *app) effectiveIndent(p *pane, n *note) (editor.IndentStyle, int) {
	style := editor.IndentAuto
	size := a.tabSize()
	if n != nil {
		ec := a.ecPropsFor(n)
		switch ec.IndentStyle {
		case "space":
			style = editor.IndentSpaces
		case "tab":
			style = editor.IndentTabs
		}
		if ec.TabWidth > 0 {
			size = ec.TabWidth
		} else if ec.IndentSize > 0 {
			size = ec.IndentSize
		}
	}
	if p != nil {
		if p.indentOverride != 0 {
			style = p.indentOverride
		}
		if p.tabSizeOverride > 0 {
			size = p.tabSizeOverride
		}
	}
	return style, size
}

// insertSpacesFor reports whether formatting/editing n in p uses spaces.
func (a *app) insertSpacesFor(p *pane, n *note) bool {
	style, _ := a.effectiveIndent(p, n)
	switch style {
	case editor.IndentTabs:
		return false
	case editor.IndentSpaces:
		return true
	default:
		if n != nil && strings.EqualFold(filepath.Ext(n.Path), ".go") {
			return false
		}
		return true
	}
}

// applyEditorConfigSaveTransforms applies trim_trailing_whitespace,
// insert_final_newline, and end_of_line (when configured by .editorconfig)
// to a dirty note right before an explicit save.
func (a *app) applyEditorConfigSaveTransforms(n *note) {
	if n == nil || !n.Dirty() {
		return
	}
	ec := a.ecPropsFor(n)
	if ec.EndOfLine == "lf" {
		n.Buffer().SetLineEnding("\n")
	} else if ec.EndOfLine == "crlf" {
		n.Buffer().SetLineEnding("\r\n")
	}
	cur := n.Text()
	updated := editorconfig.ApplySaveTransforms(cur, ec)
	if updated != cur {
		n.Buffer().Edit(editor.Edit{Start: 0, End: len(cur), Text: updated})
	}
}

func isEditorConfigPath(rel string) bool {
	return path.Base(filepath.ToSlash(rel)) == ".editorconfig"
}
