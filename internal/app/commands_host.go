package app

import (
	"strings"

	"mojian/internal/app/commands"

	"github.com/egoist/mygo"
)

// openSettingsCommand opens Settings, assigned after the view is built:
// a direct call from cmdSettings' initializer would make the command
// table depend on the window that lists it.
var openSettingsCommand func(*app)

func init() {
	openSettingsCommand = func(a *app) { a.openSettingsWindow() }
}

// initCommands binds the static command table to this window.
func (a *app) initCommands() {
	r := commands.NewRegistry()
	r.Bind("open-project", func() { a.openProjectPicker() })
	r.Bind("new-file", func() { a.startNewFile() })
	r.Bind("new-folder", func() { a.startNewFolder() })
	r.Bind("rename", func() { a.renameSelection() })
	r.Bind("trash", func() { a.trashSelection() })
	r.Bind("new-tab", func() { a.newTab() })
	r.Bind("save", func() {
		a.saveNowAsync(func(err error) {
			if err != nil {
				a.fail(err)
			}
		})
	})
	r.Bind("focus-left", func() { a.focusPaneDirection(paneLeft) })
	r.Bind("focus-right", func() { a.focusPaneDirection(paneRight) })
	r.Bind("focus-up", func() { a.focusPaneDirection(paneUp) })
	r.Bind("focus-down", func() { a.focusPaneDirection(paneDown) })
	r.Bind("move-pane-left", func() { a.movePaneDirection(paneLeft) })
	r.Bind("move-pane-right", func() { a.movePaneDirection(paneRight) })
	r.Bind("move-pane-up", func() { a.movePaneDirection(paneUp) })
	r.Bind("move-pane-down", func() { a.movePaneDirection(paneDown) })
	r.Bind("split-left", func() { a.splitFocusedSide(splitLeft) })
	r.Bind("split-right", func() { a.splitFocusedSide(splitRight) })
	r.Bind("split-up", func() { a.splitFocusedSide(splitUp) })
	r.Bind("split-down", func() { a.splitFocusedSide(splitDown) })
	r.Bind("zoom", func() { a.toggleZoom() })
	r.Bind("close-pane", func() { a.closePane(a.focusPane()) })
	r.Bind("close-tab", func() { a.closeTab(a.tab()) })
	r.Bind("rename-tab", func() { a.startRename(a.tab()) })
	r.Bind("next-tab", func() { a.cycleTab(1) })
	r.Bind("prev-tab", func() { a.cycleTab(-1) })
	r.Bind("palette", func() { a.openPalette() })
	r.Bind("find-in-files", func() { a.openSearch() })
	r.Bind("goto-symbol", func() { a.openSymbols() })
	r.Bind("next-hunk", func() { a.editorNextHunk() })
	r.Bind("prev-hunk", func() { a.editorPrevHunk() })
	r.Bind("revert-hunk", func() { a.editorRevertHunk() })
	r.Bind("open-file", func() { a.openQuickOpen() })
	r.Bind("toggle-tree", func() { a.toggleTree() })
	r.Bind("toggle-terminal", func() { a.toggleTerminal() })
	r.Bind("attach-terminal", func() { a.openSessionPicker() })
	r.Bind("jump-attention", func() { a.jumpToAttention() })
	r.Bind("git", func() { a.toggleGit() })
	r.Bind("switch-branch", func() { a.openBranchPicker() })
	r.Bind("settings", func() { openSettingsCommand(a) })
	r.Bind("ssh", func() { a.openSSHPicker() })
	a.commands = r
}

func (a *app) commandRegistry() *commands.Registry {
	if a.commands == nil {
		a.initCommands()
	}
	return a.commands
}

// commandAccel is the accelerator the menus give a command: its own,
// unless the user rebound it in Settings.
func (a *app) commandAccel(c *commands.Command) string {
	if c == nil {
		return ""
	}
	if v, ok := a.shortcutKeys[c.ID]; ok {
		return v
	}
	return c.Default()
}

// commandKeys is the keys a command shows beside its name, in the
// platform's notation.
func (a *app) commandKeys(c *commands.Command) string {
	if c == nil {
		return ""
	}
	if v, ok := a.shortcutKeys[c.ID]; ok {
		return shortcutDisplay(v)
	}
	if c.Display != "" {
		return nativeKeys(c.Display)
	}
	return shortcutDisplay(a.commandAccel(c))
}

// menuAccel gives a menu item its keys, none while a shortcut is being
// typed: macOS takes a key bound to a menu before the focused control
// sees it, which would split a pane instead of recording its keys.
func (a *app) menuAccel(accel string) string {
	if a.settings.Recording != "" {
		return ""
	}
	return accel
}

// setShortcutValue binds a command, taking the keys from any other that had
// them; the default's own keys remove the binding. The returned notice is
// shown by the Settings window.
func (a *app) setShortcutValue(id, accel string) string {
	if a.shortcutKeys == nil {
		a.shortcutKeys = map[string]string{}
	}
	sc := make(map[string]string, len(a.shortcutKeys))
	for k, v := range a.shortcutKeys {
		sc[k] = v
	}
	notice := ""
	if accel != "" {
		for _, other := range paletteCommands {
			if other.ID == id {
				continue
			}
			if sameAccel(a.commandAccel(other), accel) {
				sc[other.ID] = ""
				notice = shortcutDisplay(accel) + " was used by " + strings.TrimSuffix(other.Title, "…") + "; it is now unbound."
			}
		}
	}
	c := commandByID(id)
	if c != nil && sameAccel(accel, c.Default()) {
		delete(sc, id)
	} else {
		sc[id] = accel
	}
	if len(sc) == 0 {
		sc = nil
	}
	a.shortcutKeys = sc
	a.settings.IndexDirty = true
	a.saveSettings()
	a.refreshMenu()
	a.invalidate()
	return notice
}

// refreshMenu gives the menu bar the accelerators the settings now say.
func (a *app) refreshMenu() {
	if a.win != nil || a.settingsWin != nil {
		mygo.App.SetMenu(a.menu())
	}
}
