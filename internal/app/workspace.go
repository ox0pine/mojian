package app

import (
	"path/filepath"
	"strings"

	"mojian/internal/editorconfig"
	gitpage "mojian/internal/git"

	"github.com/egoist/mygo/ui"
)

// workspace is a project that stays loaded while another one is shown,
// so switching back restores its tabs, terminals and remote connection
// instead of reading the project again. The active workspace is embedded
// in app, so its fields are the window's project state and switching does
// not copy them.
type workspace struct {
	key string

	project     string // the last local project, for the server switcher
	store       *store
	serverAlias string
	remotePath  string
	layoutPath  string

	tabs     []*tab
	active   int
	focusReq *pane

	treeOpen   bool
	treeQuery  string
	treeSel    string
	treeReveal string
	openDirs   map[string]bool
	tabScroll  ui.ScrollState

	git                gitpage.Page
	gitMutationPending bool
	gitReloadPending   bool
	gitFileStatus      map[string]string
	gitDirDirty        map[string]bool
	gitStatusSeq       int
	gitStatusLoading   bool
	branch             branchState
	diffBases          map[string]*diffBaseEntry
	diffBaseGen        int
	ecCache            map[string]editorconfig.Props
	ecFiles            map[string][]byte
	closedStack        []closedEntry
	mru                []string
	lsp                *lspManager
	nav                navHistory
	revealTab          bool
}

const maxMRU = 64

// touchMRU records rel as the most recently accessed project file.
func (a *app) touchMRU(rel string) {
	if a.workspace == nil || rel == "" {
		return
	}
	out := make([]string, 0, min(len(a.workspace.mru)+1, maxMRU))
	out = append(out, rel)
	for _, item := range a.workspace.mru {
		if item == rel {
			continue
		}
		out = append(out, item)
		if len(out) >= maxMRU {
			break
		}
	}
	a.workspace.mru = out
}

// mruRank returns rel's 0-based MRU position, or -1 if unrecorded.
func (a *app) mruRank(rel string) int {
	if a.workspace == nil || rel == "" {
		return -1
	}
	for i, item := range a.workspace.mru {
		if item == rel {
			return i
		}
	}
	return -1
}

func workspaceKeyLocal(dir string) string {
	return "local:" + filepath.Clean(dir)
}

func workspaceKeyRemote(host, path string) string {
	return "remote:" + host + ":" + strings.TrimRight(path, "/")
}

// initWorkspaces makes sure the active workspace and the caches exist.
func (a *app) initWorkspaces() {
	if a.workspace == nil {
		a.workspace = &workspace{}
	}
	if a.workspaces == nil {
		a.workspaces = map[string]*workspace{}
	}
}

// activeWorkspaceKey names the project that holds the window now. The
// workspace keeps its key once opened; the derivation is only a fallback
// for an app built without openProject (tests) or a restored layout.
func (a *app) activeWorkspaceKey() string {
	if a.workspace != nil && a.workspace.key != "" {
		return a.workspace.key
	}
	if a.remotePath != "" && a.serverAlias != "" {
		return workspaceKeyRemote(a.serverAlias, a.remotePath)
	}
	if a.store != nil {
		return workspaceKeyLocal(a.store.Dir)
	}
	return ""
}

// GitWorkspaceKey identifies the project a Git action started in. Remote
// keys include the host as well as the path: the same repository path on
// two SSH hosts is still two different workspaces.
func (a *app) GitWorkspaceKey() string { return a.activeWorkspaceKey() }

// DeferGitWorkspaceMutation keeps a completed mutation's note reload and
// cache invalidation with its originating cached workspace. Applying them
// on reactivation avoids both stale A state and side effects on visible B.
func (a *app) DeferGitWorkspaceMutation(workspaceKey string, reloadFiles bool) {
	ws := a.workspaces[workspaceKey]
	if ws == nil {
		return
	}
	ws.gitMutationPending = true
	ws.gitReloadPending = ws.gitReloadPending || reloadFiles
}

// stashActiveWorkspace keeps the project on screen for a later switch.
// The active workspace is the object stored in the cache: there is
// nothing to copy.
func (a *app) stashActiveWorkspace() {
	a.initWorkspaces()
	a.dropPaneOverlays(nil)
	// A Git refresh from this project must not land in the next one.
	a.git.Seq++
	a.git.DiffSeq++
	a.git.HistorySeq++
	a.git.Loading = false
	a.git.DiffLoad = false
	a.git.HistoryLoad = false
	// A branch read from this project must not land in the next one.
	a.branch.seq++
	a.branch.loading = false
	a.branch.queued = false
	a.gitStatusSeq++
	a.gitStatusLoading = false
	if key := a.activeWorkspaceKey(); key != "" {
		a.workspace.key = key
		a.workspaces[key] = a.workspace
	}
}

// activateWorkspace puts a loaded project on screen without reading it
// again: its tabs, terminals and git page are the objects it kept.
func (a *app) activateWorkspace(ws *workspace) {
	if ws == nil {
		return
	}
	a.initWorkspaces()
	a.workspace = ws
	a.reloadStore = nil
	a.reloadSeq++
	if ws.key != "" {
		a.workspaces[ws.key] = ws
	}
	if ws.openDirs == nil {
		ws.openDirs = map[string]bool{}
	}
	// A refresh from the project that was on screen must not land here.
	a.git.Seq++
	a.git.DiffSeq++
	a.git.HistorySeq++
	a.git.Loading = false
	a.git.DiffLoad = false
	a.git.HistoryLoad = false
	a.closeTransientUI()
	if ws.gitMutationPending {
		reloadFiles := ws.gitReloadPending
		ws.gitMutationPending = false
		ws.gitReloadPending = false
		if reloadFiles {
			a.ReloadFiles()
		}
		// The activation below always reads the branch, completing the same
		// follow-up as AfterGitMutation without starting a duplicate read.
		a.invalidateDiffBases()
	}
	if a.git.Open {
		// A mutation that completed while this page was cached deliberately
		// skipped its refresh. Read this workspace now; its ActionSeq remains
		// intact so the action's own completion state is not discarded.
		a.git.Refresh()
	}
	// A cut is a pending one-shot action for the project it was made in.
	a.fileCut = false
	// The branch may have moved while this project was off screen.
	a.branch.seq++
	a.branch.loading = false
	a.branch.queued = false
	a.branch.open = false
	a.branch.creating = false
	a.branch.name = ""
	a.branch.query = ""
	a.branch.sel = -1
	a.branch.err = ""
	a.gitStatusSeq++
	a.gitStatusLoading = false
	// Focus requests are consumed by the editor. Request it again every
	// time a cached project becomes visible, not only when it is first read.
	a.focusReq = nil
	if t := a.tab(); t != nil {
		a.focusReq = t.editorPane()
	}
	a.refreshBranch()
	a.refreshGitStatus()
	a.syncProjectWatcher()
	a.updateTitle()
	a.invalidate()
}

// abandonActiveWorkspace closes the terminals of the project on screen
// without keeping it as the active workspace. The cached entry, if any,
// stays for a later reconnect.
func (a *app) abandonActiveWorkspace() {
	a.stopProjectWatcher()
	a.stopSysStats()
	if a.workspace == nil {
		return
	}
	a.closeWorkspaceLSP(a.workspace)
	closeWorkspaceTerminals(a.workspace)
	a.workspace = &workspace{}
}

// closeModals closes every dialog and popover that belongs to the
// window, so opening one cannot leave another one drawn underneath it.
func (a *app) closeModals() {
	a.dropPickerRemote()
	a.projectOpen = false
	a.serverOpen = false
	a.projectPicker = false
	a.paletteOpen = false
	a.quickOpen = false
	a.symbolsOpen = false
	a.wsSymbolsOpen = false
	a.refsOpen = false
	a.problemsOpen = false
	a.symRenameOpen = false
	a.codeActionsOpen = false
	a.lspStatusOpen = false
	a.indentMenuOpen = false
	a.conflict = nil
	a.closeSearch()
	a.sessionPick = false
	a.sshPick = false
	a.remotePicker = false
	a.branch.open = false
	a.branch.creating = false
	a.branch.name = ""
	a.branch.query = ""
	a.branch.sel = -1
	a.branch.err = ""
	a.git.CloseBranchMenu()
}

// closeTransientUI drops the pickers and edits that belong to the
// project that was on screen, not to the one coming next.
func (a *app) closeTransientUI() {
	a.closeModals()
	a.rename = nil
	a.treeDraft = nil
	a.treeSearch.CancelFocus()
	a.err = ""
	a.stopToast()
}

// switchWorkspace saves the project on screen and shows another one
// already loaded.
func (a *app) switchWorkspace(ws *workspace) {
	a.switchWorkspaceAndThen(ws, nil)
}

func (a *app) switchWorkspaceAndThen(ws *workspace, done func()) {
	if ws == nil {
		return
	}
	if ws == a.workspace {
		a.invalidate()
		if done != nil {
			done()
		}
		return
	}
	a.saveNowAsync(func(err error) {
		if err != nil {
			a.fail(err)
			return
		}
		if err := a.saveLayout(); err != nil {
			a.fail(err)
			return
		}
		a.layoutDirty = false
		a.dropPickerRemote()
		a.stashActiveWorkspace()
		a.activateWorkspace(ws)
		a.saveSettings()
		a.updateTitle()
		a.invalidate()
		if done != nil {
			done()
		}
	})
}

// registerActiveWorkspace records the project loaded at launch or after
// a first open, so leaving and returning to it is a switch, not a read.
func (a *app) registerActiveWorkspace() {
	a.initWorkspaces()
	if key := a.activeWorkspaceKey(); key != "" {
		a.workspace.key = key
		a.workspaces[key] = a.workspace
	}
}

// closeWorkspaces detaches every loaded project's terminals and remote
// connection, as quitting the app does. The servers keep running.
func (a *app) closeWorkspaces() {
	a.stopProjectWatcher()
	a.stopSysStats()
	a.cancelRemoteReconnect("")
	a.initWorkspaces()
	a.remoteClosing = true
	// A closing pane has already left its tab's split tree. Finish those
	// overlays first so their terminals receive Kill too; their leases
	// keep the control clients alive until the requests complete.
	a.dropPaneOverlays(nil)
	if key := a.activeWorkspaceKey(); key != "" {
		a.workspace.key = key
		a.workspaces[key] = a.workspace
	}
	for _, ws := range a.workspaces {
		a.closeWorkspaceLSP(ws)
		closeWorkspaceTerminals(ws)
	}
	for host, rc := range a.hostService().Conns() {
		if a.remoteRefs[host] > 0 {
			continue
		}
		a.detachRemoteConn(rc)
	}
	a.remotePickHost = ""
	a.remoteOpening = false
}

func closeWorkspaceTerminals(ws *workspace) {
	if ws == nil {
		return
	}
	for _, t := range ws.tabs {
		for _, p := range t.panes() {
			p.cancelLoad()
			detachTerminalView(p)
		}
	}
}
