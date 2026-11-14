package app

import (
	"strings"

	"mojian/internal/app/widgets"
	"mojian/internal/icons"

	"github.com/egoist/mygo/ui"
)

// The metrics every list and sheet in the window shares. A row is one
// file, one choice or one folder wherever it appears; the pickers and the
// panes differ in where they put it, not in how tall or how wide it is.
const (
	rowH      = widgets.RowH
	rowPadX   = widgets.RowPadX
	rowGap    = widgets.RowGap
	rowTitle  = widgets.RowTitle
	rowDetail = widgets.RowDetail

	dotSize = 6 // the unsaved dot, on a tab or in a header

	iconBtnXs, iconGlyphXs = 18, 12 // inside a tab
	iconBtnSm, iconGlyphSm = 22, 13 // in a header of a narrow pane or rail
	iconBtnMd, iconGlyphMd = 26, 16 // a sheet header, a pane header, a tab strip
	iconBtnLg, iconGlyphLg = 30, 17 // the title bar's tool buttons
)

// sectionLabel is the one caption a group of rows carries, in the title
// bar menus, the pickers, the Git page and the welcome page.
func sectionLabel(c *ui.Context, k *colors, title string) ui.Element {
	return ui.Text(c, strings.ToUpper(title)).FontSize(10).FontWeight(650).
		TextColor(k.TextFaint)
}

// emptyLine is the quiet line a list shows when it has no rows: the same
// size and place in every picker.
func emptyLine(c *ui.Context, k *colors, text string) {
	ui.Text(c, text).FontSize(12.5).TextColor(k.TextFaint).Padding(10, 8)
}

// emptyState is the centered block a whole surface shows when it has
// nothing to draw: one glyph on a tint of its color, a title and a
// detail line, then whatever action is offered.
func emptyState(c *ui.Context, k *colors, glyph string, col ui.Color, title, detail string, action func()) {
	ui.Column(c).Fill().Center().Padding(20).Gap(8).Children(func() {
		ui.Box(c).Size(40, 40).Radius(radius).Background(col.Alpha(0.10)).Center().
			Children(func() {
				ui.Icon(c, icons.Icon(glyph)).Size(20, 20).TextColor(col)
			})
		ui.Text(c, title).FontSize(13).FontWeight(600).TextColor(k.Text).
			TextAlign(ui.Center)
		if detail != "" {
			ui.Text(c, detail).FontSize(rowDetail).TextColor(k.TextFaint).
				TextAlign(ui.Center).MaxWidth(460)
		}
		if action != nil {
			action()
		}
	})
}
