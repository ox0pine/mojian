// Package widgets holds the list and dialog pieces the command palette,
// quick open, file pickers and the SSH picker share.
package widgets

import (
	"mojian/internal/icons"
	"mojian/internal/motion"
	"mojian/internal/theme"

	"github.com/egoist/mygo/ui"
)

// ListNavSplit applies list navigation keys and distinguishes Enter from Cmd+Enter.
func ListNavSplit(in ui.Element, sel *int, n int, reveal *bool) (submit, split bool) {
	if in.Changed() {
		*sel = 0
		*reveal = true
		return false, false
	}
	if n == 0 {
		return false, false
	}
	switch {
	case in.Shortcut(0, ui.KeyDown):
		*sel = min(*sel+1, n-1)
		*reveal = true
	case in.Shortcut(0, ui.KeyUp):
		*sel = max(*sel-1, 0)
		*reveal = true
	}
	if in.Shortcut(ui.Cmd, ui.KeyEnter) {
		return false, true
	}
	return in.Submitted(), false
}

// ListNav applies the keys every filtered list shares: typing resets the
// selection, the arrows move it, and Enter chooses when a row exists.
func ListNav(in ui.Element, sel *int, n int, reveal *bool) bool {
	submit, _ := ListNavSplit(in, sel, n, reveal)
	return submit
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
	return FileRowMatch(c, k, key, title, rel, nil, nil, "", selected)
}

// FileRowMatch is FileRow with highlighted fuzzy-match spans and an optional trailing badge.
func FileRowMatch(c *ui.Context, k *theme.Colors, key any, title, rel string, titlePos, relPos []int, badge string, selected bool) (ui.Element, bool) {
	row := ui.Row(c.Key(key)).Height(RowH).Padding(0, RowPadX).Gap(RowGap).
		Radius(theme.Radius).AlignItems(ui.Center).Cursor(ui.CursorPointer).
		Role(ui.RoleButton).Label(title)
	fg, muted, icon := k.Text, k.TextFaint, k.TextMuted
	hiColor, hiWeight, underline := c.Theme().Accent, 700, false
	if selected {
		// List paints the accent row; its text has to follow that face.
		fg = c.Theme().AccentText
		muted, icon = fg.Alpha(0.9), fg.Alpha(0.9)
		hiColor, hiWeight, underline = fg, 750, true
	} else if row.Hovered() {
		row.Background(k.Hover)
	}
	row.Children(func() {
		ui.Icon(c, icons.FileIcon(rel)).Size(15, 15).TextColor(icon)
		HighlightText(c, title, titlePos, fg, hiColor, 500, hiWeight, RowTitle, underline).
			SingleLine().Ellipsis("…").Shrink(0.3).MinWidth(0)
		HighlightText(c, rel, relPos, muted, hiColor, 400, hiWeight, RowDetail, underline).
			SingleLine().Ellipsis("…").Shrink(1).MinWidth(0)
		if badge != "" {
			ui.Spacer(c)
			chipFg, chipBg := k.TextFaint, k.Hover
			if selected {
				chipFg, chipBg = fg, fg.Alpha(0.16)
			}
			ui.Text(c, badge).FontSize(c.Theme().Rem(11.5/13)).TextColor(chipFg).
				Padding(2, 7).Radius(theme.Radius).Background(chipBg).Shrink(0)
		}
	})
	return row, row.Clicked()
}

// HighlightText draws text with matched rune indices styled in hiColor/hiWeight.
func HighlightText(c *ui.Context, text string, positions []int, baseColor, hiColor ui.Color, baseWeight, hiWeight int, fontSize float32, underline bool) ui.Element {
	if len(positions) == 0 || text == "" {
		return ui.Text(c, text).FontSize(fontSize).FontWeight(baseWeight).TextColor(baseColor)
	}
	tr := []rune(text)
	matched := make([]bool, len(tr))
	anyMatched := false
	for _, idx := range positions {
		if idx >= 0 && idx < len(tr) {
			matched[idx] = true
			anyMatched = true
		}
	}
	if !anyMatched {
		return ui.Text(c, text).FontSize(fontSize).FontWeight(baseWeight).TextColor(baseColor)
	}
	var spans []ui.Span
	start := 0
	cur := matched[0]
	for i := 1; i <= len(tr); i++ {
		if i < len(tr) && matched[i] == cur {
			continue
		}
		seg := string(tr[start:i])
		if cur {
			spans = append(spans, ui.Span{
				Text:      seg,
				Color:     hiColor,
				Weight:    hiWeight,
				Underline: underline,
			})
		} else {
			spans = append(spans, ui.Span{
				Text: seg,
			})
		}
		if i < len(tr) {
			start = i
			cur = matched[i]
		}
	}
	return ui.RichText(c, spans...).FontSize(fontSize).FontWeight(baseWeight).TextColor(baseColor)
}
