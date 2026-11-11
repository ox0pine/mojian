package app

import (
	"context"
	"fmt"
	"time"

	"mojian/internal/update"

	"github.com/egoist/mygo"
)

// updateNotificationID names the launch notification about a release, so a
// click reaches the About page.
const updateNotificationID = "mojian.update"

// afterOpen runs once a launch, or a window reopened from the Dock, has a
// window: it opens the About page a notification click asked for, and
// starts the quiet update check the preference allows.
func (a *app) afterOpen() {
	if a.win == nil {
		return
	}
	if a.pendingAbout {
		a.pendingAbout = false
		a.openSettingsWindow()
	}
	a.autoCheckUpdates()
}

// showAbout shows Settings on the About page, or waits for the window a
// launch is about to create.
func (a *app) showAbout() {
	a.settings.Cat = settingsAbout
	if a.win == nil {
		a.pendingAbout = true
		return
	}
	a.openSettingsWindow()
}

// autoCheckUpdates asks GitHub once a launch when the user asked to be
// told, and notifies about a release this machine has not seen yet. A
// failed check stays quiet: a launch must not accuse the network, and
// About checks on demand.
func (a *app) autoCheckUpdates() {
	if a.updateAutoTried || a.harness || a.win == nil || a.NoUpdateNotifications {
		return
	}
	a.updateAutoTried = true
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		defer cancel()
		up, err := update.Check(ctx)
		if err != nil || up == nil {
			return
		}
		a.onUpdateUI(func() {
			a.pendingUpdate = up
			if up.Version != "" && a.notifiedUpdate != up.Version {
				a.notifiedUpdate = up.Version
				a.saveSettings()
				_ = mygo.NewNotification(mygo.NotificationOptions{
					ID:    updateNotificationID,
					Title: "Mojian " + up.Version + " is available",
					Body:  "Open Settings › About to install it.",
				}).Show()
			}
			a.invalidate()
		})
	}()
}

// markUpdateSeen records a release the Settings window already showed, so
// the next launch does not notify about it again.
func (a *app) markUpdateSeen(up *update.Release) {
	if up == nil || up.Version == "" || a.notifiedUpdate == up.Version {
		return
	}
	a.notifiedUpdate = up.Version
	a.saveSettings()
}

// checkForUpdates checks the feed in the background, so the settings window
// never waits on the network. The buttons and the state change on the UI
// thread.
func (a *app) checkForUpdates() {
	if a.updateChecking || a.updateInstalling {
		return
	}
	a.updateChecking = true
	a.updateChecked = false
	a.updateErr = ""
	a.pendingUpdate = nil
	a.updateReady = false
	a.invalidate()
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		defer cancel()
		up, err := update.Check(ctx)
		a.onUpdateUI(func() {
			a.applyUpdateCheck(up, err)
			a.markUpdateSeen(up)
			a.invalidate()
		})
	}()
}

// installAvailableUpdate downloads the release found by the last check and
// stages it. On macOS the new bundle replaces the app as it quits; on
// Linux the files are replaced in place and the running process keeps
// the old image until the next relaunch.
func (a *app) installAvailableUpdate() {
	up := a.pendingUpdate
	if up == nil || a.updateInstalling || a.updateReady {
		return
	}
	a.updateInstalling = true
	a.updateErr = ""
	a.invalidate()
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()
		err := update.Install(ctx, up)
		a.onUpdateUI(func() {
			a.applyUpdateInstall(err)
			a.invalidate()
		})
	}()
}

// applyUpdateCheck lands a check result: a failed check is not a claim
// that the app is current.
func (a *app) applyUpdateCheck(up *update.Release, err error) {
	a.updateChecking = false
	a.updateChecked = err == nil
	if err != nil {
		a.updateErr = update.ErrorMessage(err)
		return
	}
	a.pendingUpdate = up
}

// applyUpdateInstall lands an install result.
func (a *app) applyUpdateInstall(err error) {
	a.updateInstalling = false
	if err != nil {
		a.updateErr = update.ErrorMessage(err)
		return
	}
	a.updateErr = ""
	a.updateReady = true
}

// restartForUpdate relaunches the app after an update was staged. The quit
// sequence saves open files and detaches terminals first.
func (a *app) restartForUpdate() {
	if !a.updateReady {
		return
	}
	relaunch := a.updateRelaunchFn
	if relaunch == nil {
		relaunch = func() { mygo.App.Relaunch() }
	}
	relaunch()
}

// updateSummary is the line under the update buttons.
func (a *app) updateSummary() string {
	switch {
	case a.updateChecking:
		return "Checking GitHub for a signed release…"
	case a.updateInstalling:
		if a.pendingUpdate != nil && a.pendingUpdate.Version != "" {
			return fmt.Sprintf("Downloading and installing Mojian %s…", a.pendingUpdate.Version)
		}
		return "Downloading and installing the update…"
	case a.updateReady:
		if a.pendingUpdate != nil && a.pendingUpdate.Version != "" {
			return fmt.Sprintf("Mojian %s is ready. Restart to apply it.", a.pendingUpdate.Version)
		}
		return "The update is ready. Restart to apply it."
	case a.pendingUpdate != nil:
		if a.pendingUpdate.Version != "" {
			return fmt.Sprintf("Mojian %s is available.", a.pendingUpdate.Version)
		}
		return "A new version is available."
	case a.updateErr != "":
		return ""
	case a.updateChecked:
		return "You are running the latest version."
	}
	return ""
}

// onUpdateUI runs fn where the app's view is built. A test has no window
// and runs it directly.
func (a *app) onUpdateUI(fn func()) {
	switch {
	case a.win != nil:
		a.win.Update(fn)
	case a.settingsWin != nil:
		a.settingsWin.Update(fn)
	default:
		fn()
	}
}
