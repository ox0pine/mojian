package app

import (
	"errors"
	"path"
	"strings"
	"time"

	"mojian/internal/session"

	gitpage "mojian/internal/git"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
)

// toggleGit shows the working-tree page or returns to the editor.
func (a *app) toggleGit() {
	a.git.Toggle(a)
	a.invalidate()
}

// CloseGit returns to the active workspace and restores its pane focus.
func (a *app) CloseGit() {
	a.git.Open = false
	if t := a.tab(); t != nil {
		a.focusReq = t.Focus
		if a.focusReq == nil {
			a.focusReq = t.editorPane()
		}
	}
	a.invalidate()
}

// refreshGit rereads the working tree for the open page.
func (a *app) refreshGit() { a.git.Refresh() }

// gitPage draws the page; the page owns its state and its view.
func (a *app) gitPage(c *ui.Context, k *colors) {
	a.git.View(c, k, a)
}

// EditorTheme is the code editor's theme for the page's diff, with the
// card as its background: the editor's own theme is transparent, and the
// diff viewer mixes change tints against the background it is given.
func (a *app) EditorTheme(c *ui.Context) *editor.Theme {
	th := *a.editorTheme(c)
	th.Background = a.colorsOf(c).CardFocused
	return &th
}

// EditorFont is the code editor's font, its line height and its tab
// size, so the diff reads as the pane beside it.
func (a *app) EditorFont() (ui.Font, float32, int) {
	return a.editorFont(), float32(a.lineHeight()), a.tabSize()
}

// The Host methods give the Git page the project, a callback into the
// window, and the file menu.
func (a *app) ProjectDir() (string, bool) {
	if a.store == nil || a.store.IsMemory() {
		return "", false
	}
	return a.store.Dir, true
}

// GitRunner runs Git where the project lives: locally, or on a server.
func (a *app) GitRunner() gitpage.Runner {
	if a.store != nil && a.store.IsServer() {
		return serverGitRunner{client: a.store.Client()}
	}
	return gitpage.LocalRunner{}
}

type serverGitRunner struct{ client *session.Client }

func (r serverGitRunner) Run(dir string, args ...string) (string, error) {
	return r.RunWithStdin(dir, "", args...)
}

func (r serverGitRunner) RunWithStdin(dir, stdin string, args ...string) (string, error) {
	cmd := append([]string{"git", "-C", dir}, args...)
	env := []string{"GIT_PAGER=cat", "GIT_TERMINAL_PROMPT=0", "LC_ALL=C"}
	res, err := r.client.ExecStdin(dir, cmd, env, stdin, 20*time.Second)
	if err != nil {
		return res.Stdout, err
	}
	if res.Code != 0 {
		msg := strings.TrimSpace(res.Stderr)
		if msg == "" {
			msg = "git failed"
		}
		return res.Stdout, errors.New(msg)
	}
	return res.Stdout, nil
}

func (r serverGitRunner) RealPath(path string) (string, error) {
	res, err := r.client.Exec("", []string{"sh", "-c", `cd "$1" && pwd -P`, "sh", path}, nil, 5*time.Second)
	if err != nil {
		return "", err
	}
	if res.Code != 0 {
		msg := strings.TrimSpace(res.Stderr)
		if msg == "" {
			msg = "pwd failed"
		}
		return "", errors.New(msg)
	}
	out := strings.TrimSuffix(res.Stdout, "\n")
	out = strings.TrimSuffix(out, "\r")
	return out, nil
}

func (r serverGitRunner) Readlink(dir, rel string) (string, error) {
	res, err := r.client.Exec(dir, []string{"readlink", "--", rel}, nil, 5*time.Second)
	if err != nil {
		return "", err
	}
	if res.Code != 0 {
		return "", errors.New(strings.TrimSpace(res.Stderr))
	}
	return strings.TrimSuffix(res.Stdout, "\n"), nil
}

func (r serverGitRunner) ReadFile(dir, rel string, limit int) ([]byte, error) {
	data, err := r.client.ReadFile(path.Join(dir, rel))
	if err != nil {
		return nil, err
	}
	if len(data) > limit {
		return data[:limit+1], nil
	}
	return data, nil
}

func (a *app) Window() *mygo.Window { return a.win }

func (a *app) Invalidate() { a.invalidate() }

func (a *app) Fail(err error) { a.fail(err) }

func (a *app) HasPath(rel string) bool {
	return a.store != nil && (a.store.ByRelPath(rel) != nil || a.store.AssetByRel(rel) != nil)
}

func (a *app) OpenPath(rel string) { a.openRel(rel) }

func (a *app) FileMenu(m *ui.Menu, rel string) { a.nodeMenu(m, rel, false) }

// ReloadFiles reloads clean open notes after the Git page mutates the
// working tree (for example discarding changes).
func (a *app) ReloadFiles() {
	if a.store == nil || a.store.IsMemory() {
		return
	}
	if err := a.store.Reload(); err != nil {
		a.fail(err)
	}
}

// AfterGitMutation refreshes branch state and editor gutter diff bases
// after staging, discarding, or committing from the Git page.
func (a *app) AfterGitMutation() {
	a.invalidateDiffBases()
	a.refreshBranch()
}

// SwitchBranch checks out a branch chosen in the Git page's own menu, then
// reloads every clean note from the files the branch holds.
func (a *app) SwitchBranch(root, name string) error {
	if err := a.branchReady(root); err != nil {
		return err
	}
	a.runBranchChange("switching to "+name, func(r gitpage.Runner) error {
		return gitpage.SwitchBranch(r, root, name)
	})
	return nil
}

// CreateBranch creates a branch at HEAD from the Git page's menu and
// checks it out.
func (a *app) CreateBranch(root, name string) error {
	if err := a.branchReady(root); err != nil {
		return err
	}
	a.runBranchChange("creating "+name, func(r gitpage.Runner) error {
		return gitpage.CreateBranch(r, root, name)
	})
	return nil
}

// branchReady refuses a checkout while a note holds unsaved edits, since
// the branch would overwrite the file those edits belong to.
func (a *app) branchReady(root string) error {
	if a.branch.loading {
		return errors.New("A branch change is already in progress.")
	}
	if root == "" {
		return errors.New("This folder is not a Git repository.")
	}
	if a.dirtyNotes() > 0 {
		return errors.New("Save or discard your edits before switching branches.")
	}
	return nil
}

// diffBaseEntry caches a file's HEAD buffer for editor gutter diff bars.
type diffBaseEntry struct {
	gen     int
	loading bool
	buf     *editor.Buffer
}

// noteDiffBase returns the cached HEAD buffer for n's gutter diff bars,
// queueing an asynchronous read on first access or after invalidation.
func (a *app) noteDiffBase(n *note) *editor.Buffer {
	if n == nil || a.store == nil || a.store.IsMemory() {
		return nil
	}
	ws := a.workspace
	if ws == nil {
		return nil
	}
	if ws.diffBases == nil {
		ws.diffBases = map[string]*diffBaseEntry{}
	}
	e := ws.diffBases[n.Rel]
	if e != nil && e.gen == ws.diffBaseGen {
		return e.buf
	}
	if e == nil {
		e = &diffBaseEntry{}
		ws.diffBases[n.Rel] = e
	}
	if !e.loading {
		e.loading = true
		gen := ws.diffBaseGen
		rel := n.Rel
		a.later(func() { a.loadNoteDiffBase(ws, rel, gen) })
	}
	return e.buf
}

func (a *app) loadNoteDiffBase(ws *workspace, rel string, gen int) {
	if ws == nil || ws.store == nil || ws.store.IsMemory() || ws.diffBaseGen != gen {
		return
	}
	dir := ws.store.Dir
	if dir == "" {
		return
	}
	runner := gitpage.Runner(gitpage.LocalRunner{})
	if ws.store.IsServer() {
		runner = serverGitRunner{client: ws.store.Client()}
	}
	apply := func(text string, ok bool) {
		if ws.diffBaseGen != gen {
			return
		}
		if ws.diffBases == nil {
			ws.diffBases = map[string]*diffBaseEntry{}
		}
		var buf *editor.Buffer
		if ok {
			buf = editor.NewBuffer(text)
		}
		ws.diffBases[rel] = &diffBaseEntry{gen: gen, buf: buf}
		if a.workspace == ws {
			a.invalidate()
		}
	}
	post := a.uiPoster()
	if post == nil {
		text, ok, _ := gitpage.ReadHeadFile(runner, dir, rel)
		apply(text, ok)
		return
	}
	winSeq := a.windowSeq
	go func() {
		text, ok, _ := gitpage.ReadHeadFile(runner, dir, rel)
		post(func() {
			if a.windowSeq != winSeq {
				return
			}
			apply(text, ok)
		})
	}()
}

// invalidateDiffBases drops cached HEAD buffers for the active workspace
// so open editors re-read their gutter diff bases on the next frame.
func (a *app) invalidateDiffBases() {
	if a.workspace == nil {
		return
	}
	a.workspace.diffBaseGen++
	a.workspace.diffBases = nil
	a.invalidate()
}
