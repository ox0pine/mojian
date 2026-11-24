package app

import (
	"mojian/internal/app/commands"
	"mojian/internal/app/explorer"
	"mojian/internal/app/preview"
	"mojian/internal/app/widgets"
	"mojian/internal/icons"
	"mojian/internal/platform"
	"mojian/internal/project"
	"mojian/internal/theme"

	"github.com/egoist/mygo/plugins/terminal"
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
)

// This file keeps the window's concise vocabulary while implementations
// live in lower packages (project, explorer, platform, theme, widgets,
// commands, preview). Architecture guards ensure the rest of internal/app
// reaches theme and platform only through these bridges.

// Project and Explorer aliases.
type store = project.Store
type note = project.Note
type assetFile = project.Asset
type fileNode = project.TreeNode

func isMarkdownPath(path string) bool               { return project.IsMarkdownPath(path) }
func isPDFPath(path string) bool                    { return preview.IsPDFPath(path) }
func wrapFor(path string) bool                      { return project.WrapFor(path) }
func lineNumbersFor(path string) editor.LineNumbers { return project.LineNumbersFor(path) }

type treeDraft = explorer.Draft

const (
	draftFile   = explorer.DraftFile
	draftFolder = explorer.DraftFolder
	draftRename = explorer.DraftRename
)

// Platform bridge.
func terminalsSupported() bool   { return platform.TerminalsSupported() }
func remoteSupported() bool      { return platform.RemoteSupported() }
func nativeKeys(s string) string { return platform.NativeKeys(s) }
func revealLabel() string        { return platform.RevealLabel() }
func thisComputerName() string   { return platform.ThisComputerName() }

// Theme bridge.
const radius = theme.Radius

type colors = theme.Colors

var noteFont = theme.NoteFont

func (a *app) dark(c *ui.Context) bool                          { return theme.Dark(a.Appearance, c.Theme().Dark) }
func (a *app) colorsOf(c *ui.Context) *colors                   { return theme.Window(a.Theme, a.dark(c)) }
func themeName(id string) string                                { return theme.Resolve(id) }
func windowTheme(c *ui.Context, k *colors, dark bool) *ui.Theme { return theme.UI(c, k, dark) }
func codeTheme(t *ui.Theme, id string, dark bool) *editor.Theme {
	return theme.EditorTheme(t, id, dark)
}
func terminalTheme(id string, dark bool) *terminal.Theme  { return theme.Terminal(id, dark) }
func themeBgMid(id string, dark bool) ui.Color            { return theme.Window(id, dark).BgMid }
func paintBackground(p *ui.Painter, r ui.Rect, k *colors) { theme.PaintBackground(p, r, k) }
func registerFonts()                                      { theme.RegisterFonts() }

// brandMark is the Mojian mark, in the window's ink or reversed for dark.
func brandMark(c *ui.Context) *ui.SVG {
	if c.Theme().Dark {
		return icons.Brand("mark-reversed")
	}
	return icons.Brand("mark")
}

// Widgets bridge: shared list, dialog, chrome, sheet and menu helpers.
const (
	rowH      = widgets.RowH
	rowPadX   = widgets.RowPadX
	rowGap    = widgets.RowGap
	rowTitle  = widgets.RowTitle
	rowDetail = widgets.RowDetail

	dotSize = widgets.DotSize

	iconBtnXs, iconGlyphXs = widgets.IconBtnXs, widgets.IconGlyphXs
	iconBtnSm, iconGlyphSm = widgets.IconBtnSm, widgets.IconGlyphSm
	iconBtnMd, iconGlyphMd = widgets.IconBtnMd, widgets.IconGlyphMd
	iconBtnLg, iconGlyphLg = widgets.IconBtnLg, widgets.IconGlyphLg

	menuW     = widgets.MenuW
	menuRowH  = widgets.MenuRowH
	menuMaxH  = widgets.MenuMaxH
	menuInset = widgets.MenuInset
)

func listNav(in ui.Element, sel *int, n int, reveal *bool) bool {
	return widgets.ListNav(in, sel, n, reveal)
}

func listNavSplit(in ui.Element, sel *int, n int, reveal *bool) (submit, split bool) {
	return widgets.ListNavSplit(in, sel, n, reveal)
}

func highlightText(c *ui.Context, text string, positions []int, baseColor, hiColor ui.Color, baseWeight, hiWeight int, fontSize float32, underline bool) ui.Element {
	return widgets.HighlightText(c, text, positions, baseColor, hiColor, baseWeight, hiWeight, fontSize, underline)
}

func (a *app) surface(e ui.Element, tint ui.Color) ui.Element {
	return theme.Surface(e, tint)
}

func (a *app) trackSurface(e ui.Element, tint ui.Color) ui.Element {
	return theme.TrackSurface(e, tint)
}

func (a *app) commandDialog(c *ui.Context, k *colors, open *bool, body func()) {
	widgets.CommandDialog(c, k, open, body)
}

func sheetListHeight(c *ui.Context) float32 { return widgets.CommandListHeight(c) }

func fileChoiceRow(c *ui.Context, k *colors, key any, ch fileChoice, selected bool) (ui.Element, bool) {
	return widgets.FileRow(c, k, key, ch.title, ch.rel, selected)
}

func fileChoiceRowMatch(c *ui.Context, k *colors, key any, ch fileChoice, badge string, selected bool) (ui.Element, bool) {
	return widgets.FileRowMatch(c, k, key, ch.title, ch.rel, ch.titlePos, ch.relPos, badge, selected)
}

func sectionLabel(c *ui.Context, k *colors, title string) ui.Element {
	return widgets.SectionLabel(c, k, title)
}

func emptyLine(c *ui.Context, k *colors, text string) { widgets.EmptyLine(c, k, text) }

func emptyState(c *ui.Context, k *colors, glyph string, col ui.Color, title, detail string, action func()) {
	widgets.EmptyState(c, k, glyph, col, title, detail, action)
}

func iconButton(c *ui.Context, k *colors, name, label string, size, iconSize float32) ui.Element {
	return widgets.IconButton(c, k, name, label, size, iconSize)
}

func sheetHeader(c *ui.Context, k *colors, query *string, placeholder, label string, open *bool) ui.Element {
	return widgets.SheetHeader(c, k, query, placeholder, label, open)
}

func (a *app) menuSurface(panel ui.Element, k *colors, drop float32) {
	widgets.MenuSurface(panel, k, drop)
}

func menuHeader(c *ui.Context, k *colors, title, keys string) { widgets.MenuHeader(c, k, title, keys) }

func menuItem(m ui.MenuParts, key any) ui.Element { return widgets.MenuItem(m, key) }

func menuChoice(c *ui.Context, k *colors, m ui.MenuParts, key any, icon, label string, current bool) ui.Element {
	return widgets.MenuChoice(c, k, m, key, icon, label, current)
}

func menuAction(c *ui.Context, k *colors, m ui.MenuParts, key any, icon, label string) ui.Element {
	return widgets.MenuAction(c, k, m, key, icon, label)
}

func menuNote(c *ui.Context, k *colors, text string, danger bool) {
	widgets.MenuNote(c, k, text, danger)
}

func menuList(c *ui.Context, body func()) { widgets.MenuList(c, body) }

// Command accelerator bridge.
func showTabAccel(n int) string                            { return commands.ShowTabAccel(n) }
func showTabKeys() string                                  { return commands.ShowTabKeys() }
func shortcutDisplay(accel string) string                  { return commands.Display(accel) }
func parseAccel(accel string) (ui.Modifiers, ui.Key, bool) { return commands.ParseAccel(accel) }
func sameAccel(a, b string) bool                           { return commands.SameAccel(a, b) }
func commandByID(id string) *commands.Command              { return commands.ByID(id) }
