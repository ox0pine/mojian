//go:build darwin

package app

import (
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/golden"
)

// TestSettingsWindowGolden keeps the Settings window's first page as a
// picture beside the test, so a change no text assertion catches — a row's
// spacing, a color, a radius — fails review as a diff. Another platform's
// fonts and metrics differ, so it runs on macOS only; the release workflow
// runs on Linux and skips it.
//
// Regenerate after an intended change:
//
//	MYGO_UPDATE_GOLDEN=1 go test ./internal/app -run TestSettingsWindowGolden
func TestSettingsWindowGolden(t *testing.T) {
	registerFonts()
	a := newProjectApp(t)
	// Keep the displayed project path out of the temp directory's random
	// suffix; otherwise every runner paints a different golden.
	a.store.Dir = "/tmp/project"
	a.settingsPath = t.TempDir() + "/settings.json"
	tt := ui.NewTester(a.settingsWindowView, 900, 640)
	tt.Advance(time.Second) // every spring the page starts has settled
	// A caret's blink and sub-pixel anti-aliasing move a few hundred pixels
	// from run to run; a layout or color change moves far more.
	golden.Match(t, tt, "testdata/settings-window.png", golden.Tolerance{Channel: 2, PerMille: 6})
}
