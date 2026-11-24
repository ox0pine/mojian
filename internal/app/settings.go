package app

import (
	"encoding/json"
	"errors"
	"log"
	"os"
	"runtime"
	"strings"
	"time"

	"mojian/internal/app/commands"
	"mojian/internal/app/settingsui"
	"mojian/internal/fs"
	"mojian/internal/prefs"
	"mojian/internal/remote"
	"mojian/internal/settings"
	"mojian/internal/shellcmd"
	"mojian/internal/theme"

	"github.com/egoist/mygo/ui"
)

// Settings page indexes, shared by the window, the menu and the palette.
const (
	settingsGeneral    = settings.General
	settingsAppearance = settings.Appearance
	settingsEditor     = settings.Editor
	settingsTerminal   = settings.Terminal
	settingsKeyboard   = settings.Keyboard
	settingsHosts      = settings.Hosts
	settingsAdvanced   = settings.Advanced
	settingsAbout      = settings.About
)

// commandAvailable hides the commands the platform does not have, as the
// palette does.
func commandAvailable(c *commands.Command) bool {
	if !terminalsSupported() && (c == cmdTerminal || c == cmdAttachSession) {
		return false
	}
	if !remoteSupported() && c == cmdSSH {
		return false
	}
	if runtime.GOOS == "js" && (c == cmdTerminal || c == cmdAttachSession || c == cmdOpenProject || c == cmdNewFolder || c == cmdBranch) {
		return false
	}
	return true
}

// settingsFile is what the app keeps between launches.
type settingsFile struct {
	prefs.Prefs
	Shortcuts      map[string]string `json:"shortcuts,omitempty"`
	Project        string            `json:"project,omitempty"`
	Projects       []string          `json:"projects,omitempty"`
	NotifiedUpdate string            `json:"notifiedUpdate,omitempty"`
	Server         string            `json:"server,omitempty"`
	RemotePath     string            `json:"remotePath,omitempty"`
	RemoteProjects []remoteProject              `json:"remoteProjects,omitempty"`
	Hosts          []hostConfig                 `json:"hosts,omitempty"`
	LSPServers     map[string]prefs.LSPServerOverride `json:"lspServers,omitempty"`
}

// loadSettings reads the preferences and the project the app was left in.
// Unmarshalling starts from the defaults, so a file written by an older
// version keeps sane values for fields it did not know.
func (a *app) loadSettings() {
	if a.settingsPath == "" {
		return
	}
	data, err := os.ReadFile(a.settingsPath)
	if err != nil {
		return
	}
	s := settingsFile{Prefs: prefs.Default()}
	if json.Unmarshal(data, &s) != nil {
		return
	}
	if !remoteSupported() {
		s.Server, s.RemotePath = "", ""
		s.RemoteProjects, s.Hosts = nil, nil
	}
	a.Prefs = s.Prefs
	a.Prefs.Sanitize()
	a.Theme = themeName(a.Theme)
	a.syncAppearance()
	a.shortcutKeys = s.Shortcuts
	a.settings.IndexCache = nil
	a.settings.IndexDirty = true
	a.project = s.Project
	a.projects = s.Projects
	a.notifiedUpdate = s.NotifiedUpdate
	a.serverAlias = s.Server
	a.remotePath = s.RemotePath
	a.hostService().SetProjects(s.RemoteProjects)
	a.hostService().SetHosts(s.Hosts)
	a.lspServers = s.LSPServers
}

// saveSettingsSoon debounces a write while a slider is dragged: the
// value changes on every pointer frame, but the file needs to land once
// the drag settles.
func (a *app) saveSettingsSoon() {
	if a.win == nil && a.settingsWin == nil {
		a.saveSettings()
		return
	}
	if a.settingsTimer == nil {
		a.settingsTimer = time.AfterFunc(400*time.Millisecond, func() {
			a.onUpdateUI(func() {
				a.saveSettings()
				a.invalidate()
			})
		})
		return
	}
	a.settingsTimer.Reset(400 * time.Millisecond)
}

func (a *app) saveSettings() {
	if a.settingsPath == "" {
		return
	}
	project := a.project
	if a.remotePath == "" && a.store != nil && !a.store.IsMemory() {
		project = a.store.Dir
	}
	p := a.Prefs
	p.Sanitize()
	server, remotePath := a.serverAlias, a.remotePath
	remoteProjects, hosts := a.remoteProjects(), a.hosts()
	if !remoteSupported() {
		server, remotePath, remoteProjects, hosts = "", "", nil, nil
	}
	data, _ := json.MarshalIndent(settingsFile{
		Prefs:          p,
		Shortcuts:      a.shortcutKeys,
		Project:        project,
		Projects:       a.projects,
		NotifiedUpdate: a.notifiedUpdate,
		Server:         server,
		RemotePath:     remotePath,
		RemoteProjects: remoteProjects,
		Hosts:          hosts,
		LSPServers:     a.lspServers,
	}, "", "  ")
	a.settings.IndexDirty = true
	if err := fs.WriteFileAtomic(a.settingsPath, data); err != nil {
		log.Print(err)
	}
}

// fontSize and the other getters are the effective values: what the
// editor and terminal use when a preference is unset or zero.
func (a *app) fontSize() float64 {
	if a.EditorFontSize <= 0 {
		return prefs.Default().EditorFontSize
	}
	return a.EditorFontSize
}

func (a *app) termFontSize() float64 {
	if a.TerminalFontSize <= 0 {
		return prefs.Default().TerminalFontSize
	}
	return a.TerminalFontSize
}

func (a *app) lineHeight() float64 {
	if a.EditorLineHeight <= 0 {
		return prefs.Default().EditorLineHeight
	}
	return a.EditorLineHeight
}

func (a *app) tabSize() int {
	if a.EditorTabSize <= 0 {
		return prefs.Default().EditorTabSize
	}
	return a.EditorTabSize
}

func (a *app) termLineHeight() float64 {
	if a.TerminalLineHeight <= 0 {
		return prefs.Default().TerminalLineHeight
	}
	return a.TerminalLineHeight
}

func (a *app) termScrollback() int {
	if a.TerminalScrollback <= 0 {
		return prefs.Default().TerminalScrollback
	}
	return a.TerminalScrollback
}

// applyAppearance applies an appearance without saving it: the native
// chrome follows it, the window's background follows it, and the menu's
// checks follow it. Its callers decide whether the file changes.
func (a *app) applyAppearance(v int) {
	if v < 0 || v > 2 {
		return
	}
	a.Appearance = v
	a.syncAppearance()
	a.applyAppearanceSource()
	a.updateWindowBackground()
}

func (a *app) setAppearance(v int) {
	if v < 0 || v > 2 {
		return
	}
	a.applyAppearance(v)
	a.saveSettings()
	a.invalidate()
}

// setTheme applies a theme without saving it: every window, pane, terminal
// and open note takes its colors from it.
func (a *app) setTheme(id string) {
	id = themeName(id)
	a.Theme = id
	a.edTheme = nil
	a.rethemeTerminals()
	a.updateWindowBackground()
}

func (a *app) syncAppearance() {
	for i, it := range a.appearanceItems {
		if it != nil {
			it.SetChecked(i == a.Appearance)
		}
	}
}

// settingsHost adapts the window to settingsui.Host. It is the only place
// the Settings package reaches app state.
type settingsHost struct{ a *app }

func (h settingsHost) Invalidate()                { h.a.invalidate() }
func (h settingsHost) ApplyTheme(c *ui.Context)   { h.a.applyTheme(c) }
func (h settingsHost) RunPending()                { h.a.runPending() }
func (h settingsHost) SaveSettings()              { h.a.saveSettings() }
func (h settingsHost) SaveSettingsSoon()          { h.a.saveSettingsSoon() }
func (h settingsHost) Later(fn func())            { h.a.later(fn) }
func (h settingsHost) Fail(err error)             { h.a.fail(err) }
func (h settingsHost) RefreshMenu()               { h.a.refreshMenu() }
func (h settingsHost) SetAppearance(n int)        { h.a.applyAppearance(n) }
func (h settingsHost) SetTheme(id string)         { h.a.setTheme(id) }
func (h settingsHost) ProjectPath() string        { return h.a.projectPath() }
func (h settingsHost) RemoteName() string         { return h.a.remoteName() }
func (h settingsHost) SettingsPath() string       { return h.a.settingsPath }
func (h settingsHost) RemotePath() string         { return h.a.remotePath }
func (h settingsHost) ServerAlias() string        { return h.a.serverAlias }
func (h settingsHost) Store() *store              { return h.a.store }
func (h settingsHost) ActiveRemote() *remote.Conn { return h.a.activeRemote() }
func (h settingsHost) ShellCommandExecutable() (string, error) {
	if h.a.harness {
		return "", errors.New("the UI harness does not install shell commands")
	}
	return shellcmd.Executable()
}
func (h settingsHost) Shortcuts() map[string]string { return h.a.shortcutKeys }
func (h settingsHost) ShowTabKeys() string          { return showTabKeys() }
func (h settingsHost) FontSize() float64            { return h.a.fontSize() }
func (h settingsHost) CheckForUpdates()             { h.a.checkForUpdates() }
func (h settingsHost) InstallAvailableUpdate()      { h.a.installAvailableUpdate() }
func (h settingsHost) RestartForUpdate()            { h.a.restartForUpdate() }

func (h settingsHost) RemoteBusyView(c *ui.Context, k *theme.Colors) {
	h.a.remoteBusyView(c, k)
}

func (h settingsHost) SetShortcut(id, value string) string {
	return h.a.setShortcutValue(id, value)
}

func (h settingsHost) ResetShortcut(id string) {
	if c := commandByID(id); c != nil {
		if notice := h.a.setShortcutValue(id, c.Default()); notice != "" {
			h.a.settings.Notice = notice
		}
		return
	}
	delete(h.a.shortcutKeys, id)
	h.a.settings.IndexDirty = true
}

func (h settingsHost) ResetShortcuts() {
	h.a.shortcutKeys = nil
	h.a.settings.IndexDirty = true
}

func (h settingsHost) Commands() []settingsui.Command {
	out := make([]settingsui.Command, 0, len(paletteCommands))
	for _, c := range paletteCommands {
		out = append(out, settingsui.Command{
			ID:          c.ID,
			Title:       c.Title,
			Accel:       c.Default(),
			Keys:        h.a.commandKeys(c),
			DefaultKeys: shortcutDisplay(c.Default()),
			Available:   commandAvailable(c),
		})
	}
	return out
}

func (h settingsHost) UpdateState() settingsui.UpdateState {
	st := settingsui.UpdateState{
		Checking:   h.a.updateChecking,
		Installing: h.a.updateInstalling,
		Ready:      h.a.updateReady,
		Checked:    h.a.updateChecked,
		Err:        h.a.updateErr,
		Summary:    h.a.updateSummary(),
	}
	if h.a.pendingUpdate != nil {
		st.PendingVersion = h.a.pendingUpdate.Version
	}
	return st
}

func (h settingsHost) GPUDetail() string {
	return h.a.gpuCapabilityDetail()
}

func (h settingsHost) SettingsHostsPage(c *ui.Context, k *theme.Colors) {
	h.a.settingsHostsPage(c, k)
}

func (a *app) statusBarInterval() time.Duration {
	switch a.StatusBarInterval {
	case 5:
		return 5 * time.Second
	case 10:
		return 10 * time.Second
	default:
		return 2 * time.Second
	}
}

// settingsWindowView renders Settings with the app as its host.
func (a *app) settingsWindowView(c *ui.Context) {
	a.initSettings()
	a.settings.WindowView(c)
}

// editorFont is the effective editor face, shared by the pane and the
// font row.
func (a *app) editorFont() ui.Font {
	f := noteFont
	if fam := strings.TrimSpace(a.EditorFontFamily); fam != "" {
		f.Family = fam + ", " + f.Family
	}
	f.Size = float32(a.fontSize())
	return f
}

func settingsButton(c *ui.Context, label string, primary bool) ui.Element {
	if primary {
		return ui.PrimaryButton(c, label)
	}
	return ui.Button(c, label)
}

// setShortcut and resetPreferences are the app-facing names tests and
// menus use; the Settings package owns the implementation.
func (a *app) setShortcut(id, accel string) {
	a.settings.Notice = a.setShortcutValue(id, accel)
}

func (a *app) initSettings() {
	a.settings.Host = settingsHost{a}
	a.settings.Prefs = &a.Prefs
}

func (a *app) resetPreferences() {
	a.initSettings()
	a.settings.ResetPreferences()
}
