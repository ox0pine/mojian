package app

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"mojian/internal/prefs"
	"mojian/internal/update"

	"github.com/egoist/mygo/ui"
)

func TestSettingsUpdateActions(t *testing.T) {
	registerFonts()
	a := newProjectApp(t)
	a.settings.Cat = settingsAbout
	tt := ui.NewTester(a.settingsWindowView, 900, 640)
	tt.Frame()
	if !tt.HasText("Check for Updates") {
		t.Fatalf("update settings: %q", tt.Texts())
	}
	if !tt.HasText("Restart to Update") {
		t.Fatalf("update settings: %q", tt.Texts())
	}
	if !tt.HasText("Notify me about updates") {
		t.Fatalf("update notification setting: %q", tt.Texts())
	}
	if !tt.HasText("github.com/ox0pine/mojian") {
		t.Fatalf("the About page has no GitHub link: %q", tt.Texts())
	}

	a.pendingUpdate = &update.Release{Version: "0.1.2"}
	tt.Frame()
	if !tt.HasText("Install Update") {
		t.Fatalf("available update settings: %q", tt.Texts())
	}
	if !tt.HasText("Mojian 0.1.2 is available.") {
		t.Fatalf("available update status: %q", tt.Texts())
	}

	if err := tt.Click("Notify me about updates toggle"); err != nil {
		t.Fatal(err)
	}
	if !a.NoUpdateNotifications {
		t.Fatal("the switch did not turn the launch check off")
	}

	a.updateReady = true
	tt.Frame()
	if !tt.HasText("Mojian 0.1.2 is ready. Restart to apply it.") {
		t.Fatalf("ready update status: %q", tt.Texts())
	}
	if tt.HasText("Install Update") {
		t.Fatalf("the install button survived a staged update: %q", tt.Texts())
	}

	relaunched := false
	a.updateRelaunchFn = func() { relaunched = true }
	if err := tt.Click("Restart to Update"); err != nil {
		t.Fatal(err)
	}
	if !relaunched {
		t.Fatal("Restart to Update did not relaunch the app")
	}
}

func TestFailedUpdateCheckDoesNotClaimTheLatestVersion(t *testing.T) {
	a := &app{workspace: &workspace{}}
	a.updateChecking = true
	a.updateChecked = true
	a.applyUpdateCheck(nil, errors.New("offline"))
	if a.updateChecked {
		t.Fatal("a failed check marked the app as current")
	}
	if a.updateErr == "" {
		t.Fatal("a failed check did not keep its error")
	}
	if got := a.updateSummary(); got != "" {
		t.Fatalf("failed check summary: %q", got)
	}
}

func TestUpdateStateLandsOnTheThread(t *testing.T) {
	a := &app{workspace: &workspace{}}
	a.applyUpdateCheck(&update.Release{Version: "0.3.0"}, nil)
	if !a.updateChecked || a.updateErr != "" || a.pendingUpdate == nil {
		t.Fatalf("check state: %+v", a)
	}
	if got := a.updateSummary(); !strings.Contains(got, "0.3.0") {
		t.Fatalf("available summary: %q", got)
	}
	a.updateInstalling = true
	a.applyUpdateInstall(errors.New("signature"))
	if a.updateInstalling || a.updateErr != "signature" || a.updateReady {
		t.Fatalf("install error state: %+v", a)
	}
	a.applyUpdateInstall(nil)
	if !a.updateReady || a.updateErr != "" {
		t.Fatalf("install ready state: %+v", a)
	}
}

func TestUpdateNotificationPreferencePersists(t *testing.T) {
	registerFonts()
	a := newProjectApp(t)
	a.settingsPath = filepath.Join(t.TempDir(), "settings.json")
	a.NoUpdateNotifications = true
	a.saveSettings()
	loaded := &app{workspace: &workspace{}, settingsPath: a.settingsPath}
	loaded.loadSettings()
	if !loaded.NoUpdateNotifications {
		t.Fatal("the update notification preference did not persist")
	}
	// The inverse default of a new install is the check being on.
	var fresh app
	fresh.Prefs = prefs.Default()
	if fresh.NoUpdateNotifications {
		t.Fatal("a new install starts with update notifications off")
	}
}

func TestNotificationClickWaitsForTheWindow(t *testing.T) {
	a := &app{workspace: &workspace{}}
	a.showAbout()
	if a.settings.Cat != settingsAbout {
		t.Fatalf("settings page after a notification click: %d", a.settings.Cat)
	}
	if !a.pendingAbout {
		t.Fatal("a notification click before the window was forgotten")
	}
	a.afterOpen()
	if !a.pendingAbout {
		t.Fatal("the pending About page opened without a window")
	}
}

func TestSeenUpdateIsNotNotifiedAgain(t *testing.T) {
	a := &app{workspace: &workspace{}, settingsPath: filepath.Join(t.TempDir(), "settings.json")}
	a.markUpdateSeen(&update.Release{Version: "0.4.0"})
	if a.notifiedUpdate != "0.4.0" {
		t.Fatalf("notified version: %q", a.notifiedUpdate)
	}
	loaded := &app{workspace: &workspace{}, settingsPath: a.settingsPath}
	loaded.loadSettings()
	if loaded.notifiedUpdate != "0.4.0" {
		t.Fatalf("notified version after a launch: %q", loaded.notifiedUpdate)
	}
}
