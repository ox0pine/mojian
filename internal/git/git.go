// Package git reads and draws the working tree's status and diff: the
// runner interface that reaches local or server-side Git, the parser for
// its output, the page state, and the view the window shows.
package git

import (
	"errors"
	"io"
	"os"
	"path/filepath"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
)

const headerH = 33

type Change struct {
	Path     string // relative to the repository root, for git
	OldPath  string // where a rename or copy came from, for git
	Display  string // relative to the project, for people
	Status   string // M, A, D, R, U, C
	Staged   bool   // has changes in the index
	Unstaged bool   // has changes in the working tree (or untracked)
}

// Runner runs Git and reads files where the project lives: locally, or
// on an SSH host through the session server.
type Runner interface {
	Run(dir string, args ...string) (stdout string, err error)
	// ReadFile reads at most limit bytes of rel under dir; more than
	// limit means the caller should treat the file as too large. The
	// returned slice may hold limit+1 bytes to say so.
	ReadFile(dir, rel string, limit int) ([]byte, error)
	// RealPath resolves symlinks where the project lives. A remote path
	// must be resolved on its host, not here.
	RealPath(path string) (string, error)
	// Readlink returns the link's target text where the project lives,
	// without opening the target itself.
	Readlink(dir, rel string) (string, error)
}

// LocalRunner runs Git in the local project.
type LocalRunner struct{}

func (LocalRunner) Run(dir string, args ...string) (string, error) { return runGit(dir, args...) }

func (LocalRunner) RunWithStdin(dir, stdin string, args ...string) (string, error) {
	return runGitStdin(dir, stdin, args...)
}

func (LocalRunner) RealPath(path string) (string, error) { return filepath.EvalSymlinks(path) }

func (LocalRunner) Readlink(dir, rel string) (string, error) {
	return os.Readlink(filepath.Join(dir, filepath.FromSlash(rel)))
}

func (LocalRunner) ReadFile(dir, rel string, limit int) ([]byte, error) {
	f, err := os.Open(filepath.Join(dir, filepath.FromSlash(rel)))
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return io.ReadAll(io.LimitReader(f, int64(limit)+1))
}

// Host is what the page needs from the window around it.
type Host interface {
	ProjectDir() (string, bool)
	GitRunner() Runner
	Window() *mygo.Window
	Invalidate()
	CloseGit()
	Fail(error)
	HasPath(string) bool
	OpenPath(string)
	FileMenu(*ui.Menu, string)
	// EditorTheme is the code editor's theme, for the diff pane; its
	// background is the pane's card, so change tints mix against it.
	EditorTheme(*ui.Context) *editor.Theme
	// EditorFont is the code editor's font, line height and tab size.
	EditorFont() (ui.Font, float32, int)
	// SwitchBranch checks out name at root and reloads the notes the new
	// branch holds; it refuses while a note has unsaved edits.
	SwitchBranch(root, name string) error
	// CreateBranch creates name at HEAD in root and checks it out.
	CreateBranch(root, name string) error
	// ReloadFiles reloads clean open notes after a working-tree mutation
	// such as discarding a file's changes.
	ReloadFiles()
	// AfterGitMutation notifies the window that index or HEAD changed
	// (e.g. after staging, discarding, or committing).
	AfterGitMutation()
}

// PageMode is what the page's left list holds.
type PageMode int

const (
	// ModeChanges lists the working tree's changed files.
	ModeChanges PageMode = iota
	// ModeHistory lists the repository's commits.
	ModeHistory
)

// hunkRunRange maps one Git @@ hunk to the viewer's contiguous change runs.
type hunkRunRange struct {
	first int
	count int
}

// Page is the working-tree diff: its state and its view.
type Page struct {
	Open       bool
	Loading    bool
	Err        string
	Root       string
	Prefix     string
	Branch     string
	Changes    []Change
	Sel        int
	Diff       string
	DiffPath   string
	DiffStaged bool // mixed files show either the index or working-tree patch
	DiffErr    string
	DiffLoad   bool
	Seq        int
	DiffSeq    int
	DiffScroll ui.ScrollState
	list       ui.ListState

	// Mode picks the left list: the working tree's changes, or the
	// repository's commits. Commits and CommitSel carry the history, and
	// patch is the selected commit's diff split per file.
	Mode        PageMode
	Commits     []Commit
	CommitSel   int
	HistoryLoad bool
	HistoryErr  string
	HistorySeq  int
	historyList ui.ListState
	patch       []FileDiff

	// diffGen counts the diffs loaded so far; diffView and patchViews
	// draw the one of viewsGen with MyGo's diff viewer, and are built on
	// the frame that first draws them, where the window's theme is known.
	diffGen    int
	diffAdd    int
	diffDel    int
	diffView   *editor.DiffState
	hunkRuns   []hunkRunRange
	patchViews []*editor.DiffState
	viewsGen   int
	patchCut   bool
	SplitDiff  bool

	// Working-tree write state (stage, unstage, discard, commit, hunk, sync).
	CommitMsg string
	HunkSel   int
	// The last diff layout used to derive HunkSel from scrolling. A
	// button jump keeps its explicit selection for the following layout.
	hunkScrollY     float64
	hunkViewH       float64
	hunkSyncGen     int
	hunkComparing   bool
	hunkJump        bool
	hunkCursor      int
	HunkMenu        bool
	FileActionsMenu bool
	SyncMenu        bool
	ActionBusy      bool
	ActionErr       string
	ActionInfo      string
	ActionSeq       int
	discardPrompt   string
	hunkDiscard     int

	// BranchMenu is the page's own popover under the branch chip; it
	// reads the branches again each time it opens.
	BranchMenu  bool
	Branches    BranchData
	branchShown bool // the menu was drawn open on the last pass
	branchLoad  bool
	branchSeq   int
	branchErr   string
	branchNew   bool
	branchName  string
	branchQuery string
	branchSel   int

	h      Host
	runner Runner
}

// Title names the page for the window.
func (p *Page) Title() string {
	if p.Mode == ModeHistory {
		return "History"
	}
	return "Changes"
}

type StatusData struct {
	Root    string
	Prefix  string
	Branch  string
	Changes []Change
	Err     string
}

func (p *Page) Toggle(h Host) {
	p.h = h
	p.runner = h.GitRunner()
	p.CloseBranchMenu()
	if p.Open {
		p.h.CloseGit()
		h.Invalidate()
		return
	}
	if !hasProject(h) {
		h.Fail(errors.New("the diff needs a project on disk"))
		return
	}
	p.Open = true
	p.Changes = nil
	p.Sel = -1
	p.clearDiff()
	p.Err = ""
	p.Loading = true
	p.Refresh()
	h.Invalidate()
}

func (p *Page) Refresh() {
	h := p.h
	if !hasProject(h) {
		return
	}
	p.runner = h.GitRunner()
	p.Loading = true
	p.Err = ""
	p.Seq++
	seq := p.Seq
	root := mustDir(h)
	if p.Mode == ModeHistory {
		p.loadHistory()
	}
	if h.Window() == nil {
		p.applyStatus(seq, ReadStatus(p.runner, root))
		return
	}
	win := h.Window()
	go func() {
		data := ReadStatus(p.runner, root)
		win.Update(func() {
			if seq == p.Seq {
				p.applyStatus(seq, data)
				h.Invalidate()
			}
		})
	}()
}

func (p *Page) applyStatus(seq int, data StatusData) {
	if seq != p.Seq {
		return
	}
	p.Loading = false
	selectedPath := ""
	if p.Sel >= 0 && p.Sel < len(p.Changes) {
		selectedPath = p.Changes[p.Sel].Path
	}
	p.Root = data.Root
	p.Prefix = data.Prefix
	p.Branch = data.Branch
	p.Changes = data.Changes
	p.Err = data.Err
	if len(data.Changes) == 0 {
		p.Sel = -1
		if p.Mode == ModeHistory {
			// The commit the page shows is not the working tree's to
			// replace.
			return
		}
		p.clearDiff()
		return
	}
	sel := min(max(p.Sel, 0), len(data.Changes)-1)
	if selectedPath != "" {
		for i := range data.Changes {
			if data.Changes[i].Path == selectedPath {
				sel = i
				break
			}
		}
	}
	if p.Mode == ModeHistory {
		p.Sel = sel
		return
	}
	p.Sel = sel
	p.loadDiff(sel)
}

// ShowHistory switches the left list to the repository's commits.
func (p *Page) ShowHistory() {
	if p.Mode == ModeHistory || p.h == nil {
		return
	}
	p.Mode = ModeHistory
	p.clearDiff()
	p.loadHistory()
	p.h.Invalidate()
}

// ShowChanges switches the left list back to the working tree.
func (p *Page) ShowChanges() {
	if p.Mode == ModeChanges || p.h == nil {
		return
	}
	p.Mode = ModeChanges
	p.HistorySeq++
	p.HistoryLoad = false
	p.clearDiff()
	p.Refresh()
	p.h.Invalidate()
}

// clearDiff forgets the shown patch and drops its read.
func (p *Page) clearDiff() {
	p.Diff = ""
	p.DiffPath = ""
	p.DiffErr = ""
	p.DiffLoad = false
	p.diffAdd, p.diffDel = 0, 0
	p.DiffSeq++
	p.patch = nil
	p.hunkRuns = nil
	p.diffGen++
	p.diffView, p.patchViews = nil, nil
}

func (p *Page) loadHistory() {
	h := p.h
	if h == nil || !hasProject(h) {
		return
	}
	p.runner = h.GitRunner()
	p.HistoryLoad = true
	p.HistoryErr = ""
	p.HistorySeq++
	seq := p.HistorySeq
	dir := mustDir(h)
	if h.Window() == nil {
		p.applyHistory(seq, ReadHistory(p.runner, dir))
		return
	}
	win := h.Window()
	go func() {
		data := ReadHistory(p.runner, dir)
		win.Update(func() {
			if seq == p.HistorySeq {
				p.applyHistory(seq, data)
				h.Invalidate()
			}
		})
	}()
}

func (p *Page) applyHistory(seq int, data HistoryData) {
	if seq != p.HistorySeq || p.Mode != ModeHistory {
		return
	}
	p.HistoryLoad = false
	p.HistoryErr = data.Err
	p.Root, p.Prefix = data.Root, data.Prefix
	shown := ""
	if p.CommitSel >= 0 && p.CommitSel < len(p.Commits) {
		shown = p.Commits[p.CommitSel].Hash
	}
	p.Commits = data.Commits
	if len(p.Commits) == 0 {
		p.CommitSel = -1
		p.clearDiff()
		return
	}
	sel := 0
	if shown != "" {
		for i := range p.Commits {
			if p.Commits[i].Hash == shown {
				sel = i
				break
			}
		}
	}
	// A refresh that leaves the same commit selected keeps its patch.
	if shown == p.Commits[sel].Hash && p.Diff != "" && p.DiffErr == "" && !p.DiffLoad {
		p.CommitSel = sel
		return
	}
	p.selectCommit(sel)
}

// selectCommit shows one commit's patch in the diff pane.
func (p *Page) selectCommit(i int) {
	h := p.h
	if h == nil || i < 0 || i >= len(p.Commits) {
		return
	}
	p.CommitSel = i
	commit := p.Commits[i]
	p.clearDiff()
	p.DiffLoad = true
	p.DiffScroll = ui.ScrollState{}
	seq := p.DiffSeq
	root, prefix := p.Root, p.Prefix
	if h.Window() == nil {
		diff, err := ReadCommitDiff(p.runner, root, prefix, commit.Hash)
		p.applyDiff(seq, diff, err)
		return
	}
	win := h.Window()
	go func() {
		diff, err := ReadCommitDiff(p.runner, root, prefix, commit.Hash)
		win.Update(func() {
			if seq == p.DiffSeq {
				p.applyDiff(seq, diff, err)
				h.Invalidate()
			}
		})
	}()
}

// selectedCommit is the commit whose patch the diff pane shows.
func (p *Page) selectedCommit() (Commit, bool) {
	if p.CommitSel < 0 || p.CommitSel >= len(p.Commits) {
		return Commit{}, false
	}
	return p.Commits[p.CommitSel], true
}

// Reset drops the page's in-flight reads, for a project switch.
func (p *Page) Reset() {
	p.Loading = false
	p.DiffLoad = false
	p.Err = ""
	p.DiffErr = ""
	p.HistoryLoad = false
	p.HistoryErr = ""
	p.BranchMenu = false
	p.branchLoad = false
	p.branchNew = false
	p.branchName = ""
	p.branchErr = ""
	p.ActionBusy = false
	p.ActionErr = ""
	p.discardPrompt = ""
	p.Seq++
	p.DiffSeq++
	p.HistorySeq++
	p.branchSeq++
	p.ActionSeq++
}

func (p *Page) Select(i int) {
	h := p.h
	if i < 0 || i >= len(p.Changes) {
		return
	}
	p.Sel = i
	p.loadDiff(i)
	h.Invalidate()
}

func (p *Page) loadDiff(i int) {
	h := p.h
	if h == nil || p.Mode == ModeHistory || i < 0 || i >= len(p.Changes) {
		return
	}
	change := p.Changes[i]
	newPath := p.DiffPath != change.Display
	source := diffCombined
	if change.Status != "U" {
		switch {
		case change.Staged && change.Unstaged:
			if newPath {
				p.DiffStaged = false
			}
			if p.DiffStaged {
				source = diffStaged
			} else {
				source = diffUnstaged
			}
		case change.Staged:
			p.DiffStaged = true
			source = diffStaged
		default:
			p.DiffStaged = false
		}
	}
	if newPath {
		p.HunkSel = 0
		p.hunkDiscard = -1
	}
	p.DiffPath = change.Display
	p.Diff = ""
	p.DiffErr = ""
	p.patch = nil
	p.hunkRuns = nil
	p.HunkMenu = false
	p.FileActionsMenu = false
	p.DiffLoad = true
	p.DiffScroll = ui.ScrollState{}
	p.diffGen++
	p.diffView, p.patchViews = nil, nil
	p.DiffSeq++
	seq := p.DiffSeq
	root := p.Root
	if h.Window() == nil {
		diff, err := readDiffSource(p.runner, root, change, source)
		p.applyDiff(seq, diff, err)
		return
	}
	win := h.Window()
	go func() {
		diff, err := readDiffSource(p.runner, root, change, source)
		win.Update(func() {
			if seq == p.DiffSeq {
				p.applyDiff(seq, diff, err)
				h.Invalidate()
			}
		})
	}()
}

// showStagedDiff switches a mixed file between its two independent patch
// lists, keeping navigation and mutation indices in the same coordinate system.
func (p *Page) showStagedDiff(staged bool) {
	ch, ok := p.selectedChange()
	if !ok || !ch.Staged || !ch.Unstaged || p.DiffStaged == staged {
		return
	}
	p.DiffStaged = staged
	p.HunkSel = 0
	p.hunkDiscard = -1
	p.loadDiff(p.Sel)
	p.h.Invalidate()
}

func (p *Page) applyDiff(seq int, diff string, err error) {
	if seq != p.DiffSeq {
		return
	}
	p.DiffLoad = false
	p.Diff = diff
	p.diffAdd, p.diffDel = DiffStat(diff)
	p.DiffErr = ""
	p.patch = nil
	p.hunkRuns = nil
	p.diffGen++
	p.diffView, p.patchViews = nil, nil
	if err != nil {
		p.DiffErr = err.Error()
		p.diffAdd, p.diffDel = 0, 0
		return
	}
	if p.Mode == ModeHistory {
		p.patch = SplitDiff(diff)
	}
}
