package widgets

import (
	"mojian/internal/icons"
	"mojian/internal/theme"

	"github.com/egoist/mygo/ui"
)

// SheetHeader is the one search row the palette, quick open and the SSH
// picker start with: the magnifier, the field and the close button.
func SheetHeader(c *ui.Context, k *theme.Colors, query *string, placeholder, label string, open *bool) ui.Element {
	var in ui.Element
	ui.Row(c).Height(48).Padding(0, 16).Gap(10).AlignItems(ui.Center).
		BorderWidth(0, 0, 1, 0).BorderColor(k.PanelBorder).Children(func() {
		ui.Icon(c, icons.Icon("search")).Size(17, 17).TextColor(k.TextFaint)
		in = ui.TextInputBase(c, query).Grow(1).FontSize(c.Theme().Rem(15.0 / 13)).
			Placeholder(placeholder).Label(label).AutoFocus()
		if IconButton(c, k, "x", "Close", IconBtnSm, IconGlyphSm).Clicked() {
			*open = false
		}
	})
	return in
}
