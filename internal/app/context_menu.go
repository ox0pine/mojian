package app

import (
	"github.com/egoist/mygo/ui"

	"mojian/internal/icons"
)

const (
	menuW     = 288 // the width every title bar menu opens with
	menuRowH  = 36  // one choice in it
	menuMaxH  = 324 // nine choices, then the list scrolls
	menuInset = 8   // the left and right padding of a row
)

// menuSurface styles the popover the machine, project and branch menus
// share, so the three open at one size, corner and shadow.
func menuSurface(panel ui.Element, k *colors, drop float32) {
	panel.Width(menuW).Padding(6).Radius(radius).Background(k.Panel).
		Border(1, k.PanelBorder).Shadow(0, 12, 32, 0, k.ShadowFocused).Gap(1).
		Translate(0, -4*(1-drop)).Opacity(0.45 + 0.55*drop)
}

// menuHeader is the caption at the top of a menu, with the keys that run
// the same command at its right end.
func menuHeader(c *ui.Context, k *colors, title, keys string) {
	ui.Row(c).Padding(6, menuInset, 4, menuInset).AlignItems(ui.Center).Children(func() {
		sectionLabel(c, k, title)
		ui.Spacer(c)
		if keys != "" {
			ui.Text(c, keys).FontSize(10).TextColor(k.TextFaint)
		}
	})
}

// menuChoice is one single-line choice: its icon, its label, and the
// check the current one shows in place of the icon, on the selected fill.
func menuChoice(c *ui.Context, k *colors, key any, icon, label string, current bool) ui.Element {
	row := ui.ButtonBase(c.Key(key)).Height(menuRowH).Padding(0, menuInset).Gap(8).
		Radius(radius).Justify(ui.Start).Cursor(ui.CursorPointer).Label(label)
	if current {
		row.Background(k.PanelSel)
	} else if row.Hovered() {
		row.Background(k.Hover)
	}
	weight := 500
	if current {
		weight = 600
	}
	row.Children(func() {
		if current {
			ui.Icon(c, icons.Icon("check")).Size(14, 14).TextColor(k.Text)
		} else {
			ui.Icon(c, icons.Icon(icon)).Size(15, 15).TextColor(k.TextMuted)
		}
		ui.Text(c, label).FontSize(13).FontWeight(weight).TextColor(k.Text).
			SingleLine().Ellipsis("…").Grow(1).MinWidth(0)
	})
	return row
}

// menuAction is a row that does something rather than choose a value:
// the foot of a menu, above its divider.
func menuAction(c *ui.Context, k *colors, key any, icon, label string) ui.Element {
	row := ui.ButtonBase(c.Key(key)).Height(menuRowH).Padding(0, menuInset).Gap(8).
		Radius(radius).Justify(ui.Start).Cursor(ui.CursorPointer).Label(label)
	if row.Hovered() {
		row.Background(k.Hover)
	}
	row.Children(func() {
		ui.Icon(c, icons.Icon(icon)).Size(15, 15).TextColor(k.TextMuted)
		ui.Text(c, label).FontSize(13).TextColor(k.Text)
	})
	return row
}

// menuNote is a menu's empty, loading or error line, in the same place
// and size whether it explains a missing host, project or branch.
func menuNote(c *ui.Context, k *colors, text string, danger bool) {
	face := k.TextFaint
	if danger {
		face = k.Danger
	}
	ui.Text(c, text).FontSize(12.5).TextColor(face).Padding(6, menuInset)
}

// menuList scrolls a menu's choices once they would run past the window.
func menuList(c *ui.Context, body func()) {
	ui.Scroll(c).MaxHeight(menuMaxH).Children(body)
}
