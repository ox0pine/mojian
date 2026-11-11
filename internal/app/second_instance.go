package app

import (
	"errors"

	"github.com/egoist/mygo"
)

// takeSingleInstance makes this process the app's one window: a later
// launch, from the Dock or `mojian path`, hands it the command line over a
// per-app socket and exits. It reports false when a running instance
// already holds the lock, meaning this launch belongs to that window.
func (a *app) takeSingleInstance() bool {
	if !mygo.App.RequestSingleInstanceLock() {
		return false
	}
	mygo.App.OnSecondInstance(a.secondInstance)
	return true
}

// secondInstance opens what a later launch named, in this window: its
// project first, then its file, and brings the window forward. A launch
// that names nothing, as the Dock icon does, only comes to the front.
func (a *app) secondInstance(args []string, wd string) {
	s, err := ParseStartup(args, wd)
	if err != nil {
		if !errors.Is(err, ErrHelp) {
			a.fail(err)
		}
		a.showMainWindow()
		return
	}
	if !a.loaded {
		// The window is still opening with the first launch's path, and
		// the newer launch replaces it.
		a.startup = s
		return
	}
	a.showMainWindow()
	a.applyStartup(s)
}

// applyStartup makes the running window open what a launch named: its
// project first, then its file. The caller has already brought the window
// forward.
func (a *app) applyStartup(s Startup) {
	if s.Dir == "" {
		return
	}
	a.openProject(s.Dir)
	// Show the file only when the project actually switched, so a failed
	// open cannot resolve the path against another project.
	if s.File == "" || a.store == nil || a.store.Dir != resolveProjectRoot(s.Dir) {
		return
	}
	a.startup = s
	a.openStartupFile()
	a.startup = Startup{}
}
