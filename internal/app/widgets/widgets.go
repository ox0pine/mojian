// Package widgets holds the list and dialog pieces the command palette,
// quick open, file pickers and the SSH picker share.
package widgets

import (
	"mojian/internal/icons"
	"mojian/internal/motion"
	"mojian/internal/theme"

	"github.com/egoist/mygo/ui"
)

// ListNav applies the keys every filtered list shares: typing resets the
// selection, the arrows move it, and Enter chooses when a row exists.
func ListNav(in ui.Element, sel *int, n int, reveal *bool) bool {
	if in.Changed() {
		*sel = 0
		*reveal = true
		return false
	}
	if n == 0 {
		return false
	}
	switch {
	case in.Shortcut(0, ui.KeyDown):
		*sel = min(*sel+1, n-1)
		*reveal = true
	case in.Shortcut(0, ui.KeyUp):
		*sel = max(*sel-1, 0)
		*reveal = true
	}
	return in.Submitted()
}

const (
	commandDialogTop        = 78
	commandDialogBottom     = 24
	commandDialogMaxH       = 440
	commandDialogListMaxH   = 384
	commandDialogListChrome = commandDialogMaxH - commandDialogListMaxH
)

// commandDialogHeight is the tallest panel that leaves room below its top
// edge, capped at the size the sheet has in a large window.
func commandDialogHeight(c *ui.Context) float32 {
	_, height := c.Size()
	return min(commandDialogMaxH, max(height-commandDialogTop-commandDialogBottom, 0))
}

// CommandListHeight is the tallest list that fits CommandDialog in this
// frame. The chrome allowance keeps its default maximum 56 DIPs below the
// panel's, as the search header and existing spacing require.
func CommandListHeight(c *ui.Context) float32 {
	return max(commandDialogHeight(c)-commandDialogListChrome, 0)
}

// CommandDialog is the search sheet the palette, quick open and the SSH
// picker share: one width, one top edge and one shadow.
func CommandDialog(c *ui.Context, k *theme.Colors, open *bool, body func()) {
	ui.DialogBase(c, open, func(backdrop, panel ui.Element) {
		backdrop.Background(k.Backdrop).Justify(ui.Start).Padding(commandDialogTop, 0, 0, 0).
			Transition(motion.Backdrop)
		theme.Surface(panel.Width(560).MaxHeight(commandDialogHeight(c)).Radius(theme.Radius), k.Panel).
			Border(1, k.PanelBorder).
			Shadow(0, 24, 60, -8, k.ShadowDialog).Clip().
			Transition(motion.Sheet)
		body()
	})
}

// The one row the file lists share: quick open, a blank pane's picker,
// the SSH hosts and the Settings font picker all read at this size.
const (
	RowH      = 34 // one row
	RowPadX   = 10 // its left and right padding
	RowGap    = 10 // between its glyph, title and detail
	RowTitle  = 13 // its title
	RowDetail = 12 // its quieter second line
)

// FileRow is one file in a picker: its icon, title and project path, with
// the shared selected and hover faces.
func FileRow(c *ui.Context, k *theme.Colors, key any, title, rel string, selected bool) (ui.Element, bool) {
	row := ui.Row(c.Key(key)).Height(RowH).Padding(0, RowPadX).Gap(RowGap).
		Radius(theme.Radius).AlignItems(ui.Center).Cursor(ui.CursorPointer).
		Role(ui.RoleButton).Label(title)
	fg, muted, icon := k.Text, k.TextFaint, k.TextMuted
	if selected {
		// List paints the accent row; its text has to follow that face.
		fg = c.Theme().AccentText
		muted, icon = fg.Alpha(0.9), fg.Alpha(0.9)
	} else if row.Hovered() {
		row.Background(k.Hover)
	}
	row.Children(func() {
		ui.Icon(c, icons.FileIcon(rel)).Size(15, 15).TextColor(icon)
		ui.Text(c, title).FontSize(RowTitle).FontWeight(500).TextColor(fg).
			SingleLine().Ellipsis("…").Shrink(0.3).MinWidth(0)
		ui.Text(c, rel).FontSize(RowDetail).TextColor(muted).
			SingleLine().Ellipsis("…").Shrink(1).MinWidth(0)
	})
	return row, row.Clicked()
}
