// Package motion holds the interruptible spring transitions for pages,
// tabs, rows, panes, sheets, dialogs, popovers and colors, so a change
// mid-flight reverses instead of restarting.
package motion

import (
	"time"

	"github.com/egoist/mygo/ui"
)

// Mojian's motion grammar pairs critically damped springs for entrances and
// spatial moves (interruptible, zero overshoot) with faster ease-out exits
// so dismissing a sheet, tab, or sidebar never lingers.
const (
	pageResponse     = 190 * time.Millisecond
	tabResponse      = 180 * time.Millisecond
	treeResponse     = 190 * time.Millisecond
	paneResponse     = 200 * time.Millisecond
	sheetResponse    = 185 * time.Millisecond
	dialogResponse   = 200 * time.Millisecond
	popoverResponse  = 165 * time.Millisecond
	bannerResponse   = 190 * time.Millisecond
	floatingResponse = 160 * time.Millisecond
	listMoveResponse = 170 * time.Millisecond
	colorDuration    = 100 * time.Millisecond
	exitFast         = 95 * time.Millisecond
	exitMedium       = 110 * time.Millisecond
	exitSlow         = 120 * time.Millisecond
)

// easeOut is a crisp deceleration curve for fades, exits and color states.
var easeOut = ui.CubicBezier(0.22, 1, 0.36, 1)

// spring is a critically damped spring that settles in about d.
func spring(d time.Duration) *ui.Animation {
	return &ui.Animation{Spring: ui.SpringOptions{Duration: d}}
}

// The tree's rows and its unfolding children share one spring, so they
// move in step as a folder opens or closes.
var treeSpring = spring(treeResponse)

// paneSpring carries a pane the layout moves, and a new pane sliding in,
// at the same pace.
var paneSpring = spring(paneResponse)

var (
	// Page swaps a tab's content, the editor and Git, and a project: subtle
	// vertical glide on enter and a brisk fade-out on exit.
	Page = ui.ElementTransition{
		Animation:    spring(pageResponse),
		ExitDuration: exitMedium,
		ExitEase:     easeOut,
		Enter:        &ui.Motion{Y: 3, Opacity: 0},
		Exit:         &ui.Motion{Y: -2, Opacity: 0},
	}

	// Tab moves tabs smoothly as they are added, closed, or dragged to reorder.
	Tab = ui.ElementTransition{
		Animation:    spring(tabResponse),
		ExitDuration: exitFast,
		ExitEase:     easeOut,
		Position:     true,
		Colors:       true,
		Enter:        &ui.Motion{Y: 2, Opacity: 0},
		Exit:         &ui.Motion{Y: -2, Opacity: 0},
	}

	// Row glides tree rows into place as folders above them unfold or collapse.
	Row = ui.ElementTransition{
		Animation: treeSpring,
		Position:  true,
	}

	// Sidebar slides the Explorer in from the window edge and tucks it back briskly.
	Sidebar = ui.ElementTransition{
		Animation:    treeSpring,
		ExitDuration: exitSlow,
		ExitEase:     easeOut,
		Enter:        &ui.Motion{X: -10, Opacity: 0},
		Exit:         &ui.Motion{X: -8, Opacity: 0},
	}

	// Sheet transitions top-pinned CommandDialog sheets (palette, quick open,
	// project search, symbols, references, code actions, SSH/session pickers).
	Sheet = ui.ElementTransition{
		Animation:    spring(sheetResponse),
		ExitDuration: exitFast,
		ExitEase:     easeOut,
		Enter:        &ui.Motion{Y: -8, Opacity: 0},
		Exit:         &ui.Motion{Y: -4, Opacity: 0},
	}

	// Dialog transitions centered modal cards (folder pickers, conflict diff,
	// font picker, remote connection status) with a subtle depth scale.
	Dialog = ui.ElementTransition{
		Animation:    spring(dialogResponse),
		ExitDuration: exitMedium,
		ExitEase:     easeOut,
		Enter:        &ui.Motion{Y: 8, Scale: 0.965, Opacity: 0},
		Exit:         &ui.Motion{Y: 4, Scale: 0.98, Opacity: 0},
	}

	// Backdrop fades dialog scrims smoothly in and out.
	Backdrop = ui.ElementTransition{
		Duration:     140 * time.Millisecond,
		Ease:         easeOut,
		ExitDuration: exitFast,
		ExitEase:     easeOut,
		Enter:        &ui.Motion{Opacity: 0},
		Exit:         &ui.Motion{Opacity: 0},
	}

	// Banner lifts bottom floating error/notice toasts gently into view.
	Banner = ui.ElementTransition{
		Animation:    spring(bannerResponse),
		ExitDuration: exitMedium,
		ExitEase:     easeOut,
		Enter:        &ui.Motion{Y: 12, Scale: 0.97, Opacity: 0},
		Exit:         &ui.Motion{Y: 8, Opacity: 0},
	}

	// FloatingBar transitions compact floating overlays such as the terminal
	// scrollback find strip.
	FloatingBar = ui.ElementTransition{
		Animation:    spring(floatingResponse),
		ExitDuration: exitFast,
		ExitEase:     easeOut,
		Enter:        &ui.Motion{Y: -6, Scale: 0.97, Opacity: 0},
		Exit:         &ui.Motion{Y: -4, Opacity: 0},
	}

	// PopoverIn drives menus dropping out of title-bar, status-bar and Git chips.
	PopoverIn = ui.Animation{Spring: ui.SpringOptions{Duration: popoverResponse}}

	// ListMove carries a list's rows to their places as items come, go, or reorder.
	ListMove = spring(listMoveResponse)

	// Colors makes hover and active fills respond cleanly to pointer movement.
	Colors = ui.ElementTransition{
		Duration: colorDuration,
		Ease:     easeOut,
		Colors:   true,
	}

	// Pane moves a pane card into a new split, across a drag, or through a zoom.
	Pane = ui.ElementTransition{
		Animation: paneSpring,
		Colors:    true,
	}

	// PaneSlide slides a new pane card from the edge of its split into place.
	PaneSlide = *paneSpring

	// FadeIn brings a new pane card in smoothly as its split opens.
	FadeIn = ui.Animation{Duration: 135 * time.Millisecond, Ease: easeOut}

	// Close fades and tucks a closing pane overlay away briskly.
	Close = ui.Animation{Duration: 115 * time.Millisecond, Ease: easeOut}
)

// Tree is the transition of a folder's children: they unfold while the
// rows below glide with Row. When Reduce Motion is enabled, collapse is
// omitted so children only crossfade in place.
func Tree(reduceMotion bool) ui.ElementTransition {
	if reduceMotion {
		return ui.ElementTransition{
			Animation:    treeSpring,
			ExitDuration: exitMedium,
			ExitEase:     easeOut,
			Enter:        &ui.Motion{Opacity: 0},
			Exit:         &ui.Motion{Opacity: 0},
		}
	}
	return ui.ElementTransition{
		Animation:    treeSpring,
		ExitDuration: exitMedium,
		ExitEase:     easeOut,
		Enter:        &ui.Motion{Opacity: 0, Collapse: true},
		Exit:         &ui.Motion{Opacity: 0, Collapse: true},
	}
}
