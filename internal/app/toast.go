package app

import (
	"time"

	"mojian/internal/motion"

	"github.com/egoist/mygo/ui"
)

type toastKind uint8

const (
	toastInfo toastKind = iota
	toastSuccess
)

const toastDuration = 2200 * time.Millisecond

// notify shows a brief, non-intrusive toast at the bottom of the window.
func (a *app) notify(text string) {
	a.showToast(text, toastInfo)
}

// notifySuccess shows a brief confirmation toast at the bottom of the window.
func (a *app) notifySuccess(text string) {
	a.showToast(text, toastSuccess)
}

func (a *app) showToast(text string, kind toastKind) {
	if text == "" {
		return
	}
	a.toastText = text
	a.toastKind = kind
	a.toastSeq++
	seq := a.toastSeq
	if a.toastTimer != nil {
		a.toastTimer.Stop()
		a.toastTimer = nil
	}
	if a.win != nil {
		win := a.win
		winSeq := a.windowSeq
		a.toastTimer = time.AfterFunc(toastDuration, func() {
			win.Update(func() {
				if a.windowSeq != winSeq || a.toastSeq != seq {
					return
				}
				a.toastText = ""
				a.toastTimer = nil
				a.invalidate()
			})
		})
	}
	a.invalidate()
}

func (a *app) stopToast() {
	a.toastSeq++
	a.toastText = ""
	if a.toastTimer != nil {
		a.toastTimer.Stop()
		a.toastTimer = nil
	}
}

func (a *app) toastBanner(c *ui.Context, k *colors) {
	if a.toastText == "" || a.err != "" {
		return
	}
	bottom := float32(18)
	if a.ShowStatusBar {
		bottom = statusBarH + 14
	}
	dot := c.Theme().Accent
	if a.toastKind == toastSuccess {
		dot = k.DiffAdd
	}
	ui.Overlay(c, func() {
		ui.Row(c).Absolute().Bottom(bottom).Left(0).Right(0).Justify(ui.Center).
			PassThrough().IgnoreInput(true).Children(func() {
			a.surface(ui.Row(c.Key("toast-banner")).Gap(8).Padding(7, 13).Radius(radius), k.Panel).
				Border(1, k.PanelBorder).Shadow(0, 8, 24, 0, k.ShadowFocused).
				AlignItems(ui.Center).Transition(motion.Banner).Children(func() {
				ui.Box(c).Size(7, 7).Radius(3.5).Background(dot)
				ui.Text(c, a.toastText).FontSize(c.Theme().Rem(12.0 / 13)).TextColor(k.Text).
					MaxWidth(480).SingleLine().Ellipsis("…")
			})
		})
	})
}
