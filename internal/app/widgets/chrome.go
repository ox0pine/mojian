package widgets

import (
	"strings"

	"mojian/internal/icons"
	"mojian/internal/motion"
	"mojian/internal/theme"

	"github.com/egoist/mygo/ui"
)

// Shared chrome metrics for badges and icon buttons across the window.
const (
	DotSize = 6 // the unsaved dot, on a tab or in a header

	IconBtnXs, IconGlyphXs = 18, 12 // inside a tab
	IconBtnSm, IconGlyphSm = 22, 13 // in a header of a narrow pane or rail
	IconBtnMd, IconGlyphMd = 26, 16 // a sheet header, a pane header, a tab strip
	IconBtnLg, IconGlyphLg = 30, 17 // the title bar's tool buttons
)

// SectionLabel is the one caption a group of rows carries, in the title
// bar menus, the pickers, the Git page and the welcome page.
func SectionLabel(c *ui.Context, k *theme.Colors, title string) ui.Element {
	return ui.Text(c, strings.ToUpper(title)).FontSize(c.Theme().Rem(10.0 / 13)).FontWeight(650).
		TextColor(k.TextFaint)
}

// EmptyLine is the quiet line a list shows when it has no rows: the same
// size and place in every picker.
func EmptyLine(c *ui.Context, k *theme.Colors, text string) {
	ui.Text(c, text).FontSize(c.Theme().Rem(12.5/13)).TextColor(k.TextFaint).Padding(10, 8)
}

// EmptyState is the centered block a whole surface shows when it has
// nothing to draw: one glyph on a tint of its color, a title and a
// detail line, then whatever action is offered.
func EmptyState(c *ui.Context, k *theme.Colors, glyph string, col ui.Color, title, detail string, action func()) {
	ui.Column(c).Fill().Center().Padding(20).Gap(8).Children(func() {
		ui.Box(c).Size(40, 40).Radius(theme.Radius).Background(col.Alpha(0.10)).Center().
			Children(func() {
				ui.Icon(c, icons.Icon(glyph)).Size(20, 20).TextColor(col)
			})
		ui.Text(c, title).FontSize(c.Theme().Rem(1)).FontWeight(600).TextColor(k.Text).
			TextAlign(ui.Center)
		if detail != "" {
			ui.Text(c, detail).FontSize(RowDetail).TextColor(k.TextFaint).
				TextAlign(ui.Center).MaxWidth(460)
		}
		if action != nil {
			action()
		}
	})
}

// IconButton is a borderless button of an icon, with a face on hover;
// label names it for screen readers and its tooltip.
func IconButton(c *ui.Context, k *theme.Colors, name, label string, size, iconSize float32) ui.Element {
	// ButtonBase carries the focus, Enter and Space, and the button role;
	// only the look is ours.
	b := ui.ButtonBase(c).Size(size, size).Radius(theme.Radius).
		Cursor(ui.CursorPointer).Label(label)
	if b.Pressed() {
		b.Background(k.Pressed)
	} else if b.Hovered() {
		b.Background(k.Hover)
	}
	b.Transition(motion.Colors)
	col := k.IconMuted
	if b.Hovered() {
		col = k.Text
	}
	b.Children(func() {
		ui.Icon(c, icons.Icon(name)).Size(iconSize, iconSize).TextColor(col)
	})
	return b
}
