package app

import (
	"mojian/internal/icons"

	"github.com/egoist/mygo/ui"
)

// sheetListH is the tallest list a search sheet shows before it scrolls:
// the panel's 440 less the header and its border.
const sheetListH = 384

// sheetHeader is the one search row the palette, quick open and the SSH
// picker start with: the magnifier, the field and the close button.
func sheetHeader(c *ui.Context, k *colors, query *string, placeholder, label string, open *bool) ui.Element {
	var in ui.Element
	ui.Row(c).Height(48).Padding(0, 16).Gap(10).AlignItems(ui.Center).
		BorderWidth(0, 0, 1, 0).BorderColor(k.PanelBorder).Children(func() {
		ui.Icon(c, icons.Icon("search")).Size(17, 17).TextColor(k.TextFaint)
		in = ui.TextInputBase(c, query).Grow(1).FontSize(15).
			Placeholder(placeholder).Label(label).AutoFocus()
		if iconButton(c, k, "x", "Close", iconBtnSm, iconGlyphSm).Clicked() {
			*open = false
		}
	})
	return in
}
