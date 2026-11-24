// Package app is Mojian's window in one package: the immediate-mode
// view, its actions, Settings, Explorer, palettes, file operations and
// SSH-driven remote projects. Every piece of window state is a plain
// field on *app, so the view and the actions that mutate it stay
// together.
//
//	go tool mygo dev    the app, rebuilt as the code changes
//	go test ./...       the model, the store and the view, without a window
package app

import (
	"context"
	"fmt"
	"hash/fnv"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"mojian/internal/app/commands"
	"mojian/internal/app/explorer"
	"mojian/internal/app/settingsui"
	"mojian/internal/fs"
	"mojian/internal/lsp"
	"mojian/internal/prefs"
	"mojian/internal/remote/remotehost"
	"mojian/internal/session"
	"mojian/internal/sysstat"
	"mojian/internal/update"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
)

type app struct {
	// workspace is the project on screen. It is also in workspaces, so
	// switching stores and restores the same object instead of copying
	// its fields into the app.
	*workspace
	prefs.Prefs

	// startup is the project and file the command line named. It is
	// consumed by the first open; later windows return to the project on
	// screen.
	startup Startup

	win       *mygo.Window
	tray      *mygo.Tray
	windowSeq int
	// native distinguishes the running desktop app from no-window tests:
	// production drops background callbacks when every window is gone.
	native bool

	dataDir      string
	settingsPath string

	projects       []string
	workspaces     map[string]*workspace
	remoteHosts    *remotehost.Service
	remoteBusy     bool
	remoteBusyMsg  string
	remoteTask     remoteTask
	remoteDirsSeq  int
	remoteDirsBusy bool
	projectOpen    bool
	projectPicker  bool
	remotePickHost string // the host a picker connection was made for
	remoteOpening  bool   // a picked project is about to open
	remoteKeep     map[string]bool
	// remoteRefs counts terminal Create and Kill operations that still
	// need their host's connection. remoteClosing makes their completion
	// release connections left behind by workspace shutdown.
	remoteRefs         map[string]int
	remoteClosing      bool
	localTerminalKills int
	remoteHomes        map[string]string
	// fileClip is the app's own clipboard: absolute local paths, or paths
	// relative to a remote project (fileClipRemote). A cut is a one-shot
	// intent and is cleared when the project changes.
	fileClip              []string
	fileCut               bool
	fileClipRemote        bool
	fileClipHost          string
	fileClipRoot          string
	hostDraftName         string
	hostDraftAddr         string
	sshImportOpen         bool
	sshImportCandidates   []hostConfig
	serverOpen            bool
	remotePicker          bool
	remoteHost            string
	remoteQuery           string
	remoteDirs            []session.FileEntry
	remoteList            ui.ListState
	remoteErr             string
	remoteReconnecting    map[string]bool
	remoteReconnectSeq    map[string]int
	remoteReconnectCancel map[string]context.CancelFunc
	pickerPath            string
	pickerInput           string
	pickerEntries         []pickerEntry
	pickerList            ui.ListState
	pickerSel             int
	pickerErr             string
	pickerHidden          bool

	paneOverlays []*paneOverlay

	sshPick  bool
	sshQuery string
	sshSel   int
	sshList  ui.ListState

	sessionPick   bool
	sessionQuery  string
	sessionSel    int
	sessionList   ui.ListState
	sessionItems  []session.SessionInfo
	sessionBusy   bool
	sessionErr    string
	sessionSeq    int
	sessionServer string

	treeSearch ui.Handle
	treeDraft  *treeDraft
	draftSeq   int

	updateRelaunchFn func()
	updateChecking   bool
	updateInstalling bool
	updateReady      bool
	updateChecked    bool
	pendingUpdate    *update.Release
	updateErr        string
	updateAutoTried  bool
	notifiedUpdate   string
	pendingAbout     bool

	shellStarter       func() (*termTab, error) // tests replace this
	sessionClient      *session.Client
	localServerStopped bool
	localServerErr     string
	localServerProbe   time.Time
	localServerProbeOK bool
	localStopAsk       bool
	// harness marks the headless fixtures of cmd/harness: no native
	// windows or session servers, only the fixture's disposable project.
	harness bool
	// uiPost hands work from a background goroutine to the UI thread.
	// The window sets it through uiPoster; the headless harness queues
	// the work for its next render.
	uiPost func(func())

	paletteOpen  bool
	paletteQuery string
	paletteSel   int
	paletteList  ui.ListState

	quickOpen  bool
	quickQuery string
	quickSel   int
	quickList  ui.ListState

	searchOpen        bool
	searchQuery       string
	searchCase        bool
	searchWholeWord   bool
	searchRegex       bool
	searchInclude     string
	searchExclude     string
	searchFilterOpen  bool
	searchReplace     string
	searchReplaceOpen bool
	searchNotice      string
	searchLastKey     string
	searchBusy        bool
	searchErr         string
	searchResult      session.SearchResult
	searchSel         int
	searchList        ui.ListState
	searchSeq         int
	searchCancel      func()
	searchCollapsed   map[string]bool
	modalSearch       *searchState
	searchHistory     []string
	searchHistIdx     int

	symbolsOpen  bool
	symbolsQuery string
	symbolsSel   int
	symbolsList  ui.ListState
	symbolsItems []lsp.Symbol
	symbolsSeq   int

	wsSymbolsOpen  bool
	wsSymbolsQuery string
	wsSymbolsLastQ string
	wsSymbolsSel   int
	wsSymbolsList  ui.ListState
	wsSymbolsItems []wsSymbolItem
	wsSymbolsBusy  bool
	wsSymbolsSeq   int

	refsOpen  bool
	refsQuery string
	refsSel   int
	refsList  ui.ListState
	refsItems []refItem
	refsBusy  bool
	refsErr   string
	refsSeq   int

	problemsOpen  bool
	problemsQuery string
	problemsSel   int
	problemsList  ui.ListState

	symRenameOpen bool
	symRenameText string
	symRenameErr  string
	symRenameBusy bool
	symRenameSeq  int

	codeActionsOpen  bool
	codeActionsQuery string
	codeActionsSel   int
	codeActionsList  ui.ListState
	codeActionsItems []lsp.CodeAction
	codeActionsBusy  bool
	codeActionsErr   string
	codeActionsSeq   int
	codeActionsNote  *note
	codeActionsWS    *workspace

	refsTitle     string
	lspStatusOpen bool
	lspStatusSel  string
	lspServers    map[string]prefs.LSPServerOverride

	indentMenuOpen bool
	conflict       *saveConflict

	rename      *tab
	renameText  string
	renameFocus bool

	appearanceItems [3]*mygo.MenuItem

	settingsWin     *mygo.Window
	settings        settingsui.UI
	explorer        explorer.UI
	hostEditAddr    string
	hostEditName    string
	hostEditAddress string
	hostRemoveAddr  string
	hostStopAddr    string
	hostSetupAddr   string
	hostSetupBusy   bool
	hostSetupSeq    int
	hostSetupMsg    string
	hostSetupErr    string
	hostStates      map[string]*hostState
	shortcutKeys    map[string]string
	commands        *commands.Registry

	err        string
	title      string
	toastText  string
	toastKind  toastKind
	toastSeq   int
	toastTimer *time.Timer

	layoutDirty    bool
	lastChange     time.Time
	layoutTimer    *time.Timer
	settingsTimer  *time.Timer
	reloadStore    *store
	reloadSeq      int
	watchCancel    context.CancelFunc
	watchDir       string
	watchSeq       int
	watchStarter   func(ctx context.Context, dir string, ignore func(string, bool) bool) (fileWatcher, error)
	reloadMutation uint64
	lastReload     time.Time
	lastReloadFor  *store
	loaded         bool
	quitting       bool
	saveSkipped    bool
	quitReady      bool
	closeReady     bool
	saveTask       saveTask
	saveWaiters    []saveWaiter
	pending        []func()

	edID    string
	edDark  bool
	edTheme *editor.Theme

	sysSampler           *sysstat.Sampler
	sysCancel            context.CancelFunc
	sysSeq               int
	sysKey               string
	sysSnap              sysstat.Snapshot
	sysSnapHost          string
	sysSnapOK            bool
	sysRemoteUnsupported map[*session.Client]bool
	sysGPUCapSet         bool
	sysGPUCap            sysstat.Capabilities
}

func Run(startup Startup) {
	registerFonts()
	a := &app{
		workspace: &workspace{},
		Prefs:     prefs.Default(),
		startup:   startup,
		native:    true,
	}
	if !a.takeSingleInstance() {
		// Another Mojian is running: it received this launch's command
		// line and opens it, so this process is done.
		return
	}
	go warmSessionServer()
	update.UseGitHub()
	mygo.App.SetMenu(a.menu())
	mygo.App.WhenReady(a.open)
	mygo.App.OnNotificationClick(func(id string) {
		if id == updateNotificationID {
			a.showAbout()
		}
	})
	mygo.App.OnActivate(func(hasVisibleWindows bool) {
		if !hasVisibleWindows && a.win == nil {
			a.open()
		}
	})
	mygo.App.OnWindowAllClosed(func() {
		if runtime.GOOS != "darwin" {
			mygo.App.Quit()
		}
	})
	mygo.App.OnQuitCanceled(a.quitCanceled)
	mygo.App.OnBeforeQuit(func(e *mygo.QuitEvent) {
		a.beforeQuit(e, mygo.App.Quit, mygo.App.WhenReady)
	})
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}

func (a *app) quitCanceled() {
	a.quitting = false
	a.quitReady = false
}

func (a *app) beforeQuit(e *mygo.QuitEvent, quit func(), post func(func())) {
	a.quitting = true
	if e.SessionEnding {
		// Logout and shutdown cannot be delayed. Do not leave a
		// goroutine or a save-error dialog behind when the process ends.
		a.saveWaiters = nil
		a.cancelRemoteSave()
		if !a.saveSkipped {
			_ = a.saveNow()
		}
		a.finishQuit()
		return
	}
	if a.quitReady || a.saveSkipped {
		a.finishQuit()
		return
	}
	e.PreventDefault()
	a.saveNowAsync(func(err error) {
		if err != nil {
			a.quitting = false
			a.askQuit(err, func() {
				a.saveSkipped = true
				quit()
			})
			return
		}
		post(func() {
			// A local save completes inside OnBeforeQuit. Retry only after
			// MyGo delivers OnQuitCanceled for that prevented attempt, or
			// the cancellation clears quitReady and every retry saves again.
			a.quitReady = true
			quit()
		})
	})
}

func (a *app) finishQuit() {
	a.closeWorkspaces()
	if a.sessionClient != nil && a.localTerminalKills == 0 {
		_ = a.sessionClient.Close()
		a.sessionClient = nil
	}
	if a.settingsWin != nil {
		a.settingsWin.Close()
	}
}

// open opens the window, with the tabs of the last launch.
func (a *app) open() {
	a.setDockIcon()
	a.installTray()
	defer a.afterOpen()
	if !a.loaded {
		a.loaded = true
		if dir, err := mygo.App.Path(mygo.PathUserData); err == nil {
			a.dataDir = dir
			a.settingsPath = filepath.Join(dir, "settings.json")
		}
		a.loadSettings()
		if len(a.shortcutKeys) > 0 {
			mygo.App.SetMenu(a.menu())
		}
		if a.startup.Dir != "" {
			// The command line chose a local project: the remote project
			// of the last launch must not take its place.
			a.serverAlias, a.remotePath = "", ""
		}
		if a.serverAlias != "" && a.remotePath != "" {
			// Create the window first and connect behind the loading card,
			// so an unreachable host cannot delay the launch.
			a.treeOpen = true
			a.win = a.createWindow("main")
			a.checkHosts()
			a.updateTitle()
			a.openRemoteProjectAsync(a.serverAlias, a.remotePath)
			return
		}
		firstProject := a.project == ""
		s, err := a.initialStore()
		if err != nil {
			log.Print(err)
			mygo.Dialog.Message(mygo.MessageOptions{
				Type:    mygo.MessageError,
				Title:   "Mojian could not open the project",
				Message: err.Error(),
			})
			mygo.App.Quit()
			return
		}
		a.store = s
		a.treeOpen = true
		a.layoutPath = a.projectLayoutPath(s.Dir)
		if firstProject && !s.IsMemory() {
			a.migrateLayout(s.Dir)
		}
		a.rememberProject(s.Dir)
		a.saveSettings()
		restore := layoutRestoreMissing
		if !a.NoRestoreLayout {
			restore = a.restoreLayoutStatus()
		}
		if restore.needsFallback() {
			a.focusFirstNote()
		} else if restore == layoutRestoreOK {
			if t := a.tab(); t != nil {
				a.focusReq = t.editorPane()
			}
		}
		a.openStartupFile()
		a.startup = Startup{}
		if n := a.currentNote(); n != nil {
			a.revealInTree(n)
		}
		a.revealTab = true
		if restore != layoutRestoreInvalid {
			a.saveLayout()
		}
	}
	a.quitting = false
	a.saveSkipped = false
	a.quitReady = false
	a.closeReady = false
	if a.loaded && a.serverAlias != "" && a.remotePath != "" {
		// Reopening a window on a remote project reconnects behind the
		// loading card instead of holding the window back.
		a.win = a.createWindow("main")
		a.checkHosts()
		a.registerActiveWorkspace()
		a.refreshBranch()
		a.updateTitle()
		a.reconnectActiveRemoteAsync()
		return
	}
	a.win = a.createWindow("main")
	a.checkHosts()
	a.registerActiveWorkspace()
	a.refreshBranch()
	a.refreshGitStatus()
	a.syncProjectWatcher()
	a.updateTitle()
}

// initialStore opens the project the command line named, else
// $MOJIAN_VAULT, else the project of the last launch, else the default
// folder. The app's own project gets a welcome note; a project the user
// chose does not.
func (a *app) initialStore() (*store, error) {
	if a.startup.Dir != "" {
		return a.openLocalStore(a.startup.Dir)
	}
	if os.Getenv("MOJIAN_VAULT") != "" {
		return a.openDefaultStore()
	}
	if a.project != "" {
		if info, err := os.Stat(a.project); err == nil && info.IsDir() {
			return a.openLocalStore(a.project)
		}
	}
	return a.openDefaultStore()
}

// rememberProject moves dir to the front of the recent projects.
func (a *app) rememberProject(dir string) {
	if dir == "" {
		return
	}
	dir = filepath.Clean(dir)
	a.project = dir
	out := []string{dir}
	for _, p := range a.projects {
		p = filepath.Clean(p)
		if p != dir && p != "." && p != "" {
			out = append(out, p)
		}
	}
	if len(out) > 8 {
		out = out[:8]
	}
	a.projects = out
}

// projectLayoutPath keeps a layout for each project, apart from the
// others.
func (a *app) projectLayoutPath(dir string) string {
	if a.dataDir == "" || dir == "" {
		return ""
	}
	h := fnv.New64a()
	_, _ = h.Write([]byte(dir))
	return filepath.Join(a.dataDir, "layouts", fmt.Sprintf("%016x.json", h.Sum64()))
}

// migrateLayout copies the one layout of older versions to the first
// project, so its tabs survive the upgrade.
func (a *app) migrateLayout(dir string) {
	if a.layoutPath == "" {
		return
	}
	if _, err := os.Stat(a.layoutPath); err == nil {
		return
	}
	old := filepath.Join(a.dataDir, "layout.json")
	data, err := os.ReadFile(old)
	if err != nil {
		return
	}
	if err := fs.WriteFileAtomic(a.layoutPath, data); err != nil {
		log.Print(err)
	}
}

func (a *app) askQuit(err error, proceed func()) {
	parent := a.win
	go func() {
		res, _ := mygo.Dialog.Message(mygo.MessageOptions{
			Parent: parent, Type: mygo.MessageError,
			Title:   "Mojian could not save",
			Message: err.Error(),
			Detail:  "Your edits are still in memory. Quit without saving?",
			Buttons: []string{"Keep Editing", "Quit Without Saving"},
		})
		if res.Button == 1 {
			proceed()
		}
	}()
}

// saveNow writes every unsaved note, the layout and the settings. It
// returns the first write error, so a project switch or quit can stop
// before dropping unsaved work.
func (a *app) saveNow() error {
	var first error
	if a.store != nil {
		ecTouched := false
		var savedNotes []*note
		for _, n := range a.store.Notes {
			if n.Dirty() {
				a.applyEditorConfigSaveTransforms(n)
				if isEditorConfigPath(n.Rel) {
					ecTouched = true
				}
				savedNotes = append(savedNotes, n)
			}
		}
		if err := a.store.SaveAll(); err != nil {
			log.Print(err)
			first = err
		}
		if ecTouched {
			a.invalidateEditorConfig()
		}
		for _, n := range savedNotes {
			if !n.Dirty() {
				a.notifyLSPSaved(n)
			}
		}
		if len(savedNotes) > 0 {
			a.refreshGitStatus()
		}
	}
	if a.layoutDirty {
		if err := a.saveLayout(); err != nil {
			if first == nil {
				first = err
			}
		} else {
			a.layoutDirty = false
		}
	}
	a.saveSettings()
	return first
}

// fail shows an error at the bottom of the window.
func (a *app) fail(err error) {
	if err == nil {
		return
	}
	a.err = err.Error()
	a.invalidate()
}

// onCleanupUI posts operation cleanup to the UI thread even after the
// last window has closed. Unlike onUpdateUI, it does not drop the work:
// stale-result guards still reject state changes, while connections and
// leases are always released.
func (a *app) onCleanupUI(fn func()) {
	switch {
	case a.win != nil:
		a.win.Update(fn)
	case a.settingsWin != nil:
		a.settingsWin.Update(fn)
	case a.uiPost != nil:
		a.uiPost(fn)
	case onWindowPostFn != nil:
		onWindowPostFn(fn)
	case a.native:
		mygo.App.WhenReady(fn)
	default:
		fn()
	}
}

// later runs fn after the frame that asked for it, so a button does not
// change the tree it is part of while the tree builds.
func (a *app) later(fn func()) {
	a.pending = append(a.pending, fn)
	switch {
	case a.win != nil:
		a.win.Update(func() {})
	case a.settingsWin != nil:
		a.settingsWin.Update(func() {})
	}
}

func (a *app) runPending() {
	if len(a.pending) == 0 {
		return
	}
	fns := a.pending
	a.pending = nil
	for _, fn := range fns {
		fn()
	}
}

// changed marks the layout for saving and schedules the frame that
// persists it after the pointer or keyboard settles.
func (a *app) changed() {
	a.layoutDirty = true
	a.lastChange = time.Now()
	a.scheduleLayoutSave()
}

func (a *app) scheduleLayoutSave() {
	win := a.win
	if win == nil {
		return
	}
	// The timer outlives a single frame. Recreate it for a reopened
	// window rather than waking one that is gone.
	if a.layoutTimer == nil {
		a.layoutTimer = time.AfterFunc(700*time.Millisecond, func() {
			win.Update(func() {})
		})
		return
	}
	a.layoutTimer.Reset(700 * time.Millisecond)
}

// persistLayout writes the window layout after a change settles. File
// contents are written only by an explicit save.
func (a *app) persistLayout(c *ui.Context) {
	if !a.layoutDirty {
		return
	}
	if time.Since(a.lastChange) < 550*time.Millisecond {
		return
	}
	if err := a.saveLayout(); err == nil {
		a.layoutDirty = false
	}
}

func (a *app) uiPoster() func(func()) {
	if a.uiPost != nil {
		return a.uiPost
	}
	if a.win != nil {
		win := a.win
		return func(fn func()) { win.Update(fn) }
	}
	return nil
}
