package theme

import (
	"github.com/egoist/mygo/plugins/glass"
	"github.com/egoist/mygo/ui"
)

// Surface fills a floating dialog, popover, banner, or bar with MyGo's
// Liquid Glass material, tinted gently with tint so all ten Mojian themes stay
// recognizable and legible. Under OS Reduce Transparency, MyGo automatically
// falls back to an opaque surface.
func Surface(e ui.Element, tint ui.Color) ui.Element {
	return e.Material(glass.Glass{
		Style: glass.Regular,
		Tint:  tint.Alpha(0.56),
	})
}

// TrackSurface fills a title-bar context track, tab strip track, or segmented
// mode track with translucent Liquid Glass tinted by tint.
func TrackSurface(e ui.Element, tint ui.Color) ui.Element {
	return e.Material(glass.Glass{
		Style: glass.Regular,
		Tint:  tint.Alpha(0.42),
	})
}
