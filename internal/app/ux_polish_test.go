package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mojian/internal/filekind"
	"mojian/internal/fs"
	"mojian/internal/project"
	"mojian/internal/session"
	"mojian/internal/testutil"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
)

func TestFuzzyScoreAndParseFileLineQuery(t *testing.T) {
	sBoundary, posBoundary, ok1 := fuzzyScore("internal/app/pane_actions.go", "pa")
	sMiddle, _, ok2 := fuzzyScore("internal/app/workspace.go", "pa")
	if !ok1 || !ok2 {
		t.Fatalf("expected both strings to match 'pa'")
	}
	if sBoundary <= sMiddle {
		t.Fatalf("expected boundary match (%d) to outscore mid-word match (%d)", sBoundary, sMiddle)
	}
	if len(posBoundary) != 2 {
		t.Fatalf("expected 2 matched positions, got %v", posBoundary)
	}

	sExact, _, _ := fuzzyScore("app.go", "app")
	sSubseq, _, _ := fuzzyScore("another_purple_page.go", "app")
	if sExact <= sSubseq {
		t.Fatalf("expected prefix match (%d) to outscore scattered match (%d)", sExact, sSubseq)
	}

	cases := []struct {
		raw     string
		wantQ   string
		wantL   int
		wantC   int
		wantHas bool
	}{
		{"app.go", "app.go", 0, 0, false},
		{"app.go:42", "app.go", 42, 0, true},
		{"app.go:42:7", "app.go", 42, 7, true},
		{":15", "", 15, 0, true},
		{":15:3", "", 15, 3, true},
		{"foo:bar", "foo:bar", 0, 0, false},
	}
	for _, tc := range cases {
		q, l, c, has := parseFileLineQuery(tc.raw)
		if q != tc.wantQ || l != tc.wantL || c != tc.wantC || has != tc.wantHas {
			t.Fatalf("parseFileLineQuery(%q) = (%q,%d,%d,%v), want (%q,%d,%d,%v)",
				tc.raw, q, l, c, has, tc.wantQ, tc.wantL, tc.wantC, tc.wantHas)
		}
	}
}

func TestQuickOpenMRUScoringLineAndSplit(t *testing.T) {
	registerFonts()
	dir := t.TempDir()
	testutil.WriteVault(t, dir, map[string]string{
		"alpha.md": "# Alpha\nline2\nline3\nline4\n",
		"beta.md":  "# Beta\nb2\nb3\nb4\nb5\n",
		"gamma.md": "# Gamma\n",
	})
	a := &app{workspace: &workspace{treeOpen: true}}
	s, err := openStoreAt(dir)
	if err != nil {
		t.Fatal(err)
	}
	a.store = s
	a.openNote(s.ByRelPath("alpha.md"))
	a.openNote(s.ByRelPath("gamma.md"))

	choices := a.paneChoices("")
	if len(choices) < 2 || choices[0].rel != "gamma.md" || choices[1].rel != "alpha.md" {
		t.Fatalf("expected MRU order [gamma.md, alpha.md], got %+v", choices)
	}

	tt := ui.NewTester(a.view, 1100, 700)
	tt.Frame()

	// Test Quick Open with :line suffix.
	a.openQuickOpen()
	tt.Frame()
	tt.Type("beta.md:4")
	tt.Frame()
	tt.Key(0, ui.KeyEnter)
	tt.Frame()
	tt.Frame()
	if a.currentNote() == nil || a.currentNote().Rel != "beta.md" {
		t.Fatalf("expected beta.md open, got %v", a.currentNote())
	}
	if got := a.focusPane().state.CursorPos().Line; got != 3 {
		t.Fatalf("expected cursor on 0-based line 3, got %d", got)
	}

	// Test Cmd+Enter split-open from Quick Open.
	a.openQuickOpen()
	tt.Frame()
	tt.Type("alpha")
	tt.Frame()
	tt.Key(ui.Cmd, ui.KeyEnter)
	tt.Frame()
	tt.Frame()
	if panes := len(a.tab().panes()); panes != 2 {
		t.Fatalf("expected 2 split panes after Cmd+Enter, got %d", panes)
	}
	if a.currentNote() == nil || a.currentNote().Rel != "alpha.md" {
		t.Fatalf("expected split focus on alpha.md, got %v", a.currentNote())
	}
}

type uxBlockingRemoteBackend struct {
	root    string
	entries []session.FileEntry
	readCh  chan uxReadResult
	started chan struct{}
}

type uxReadResult struct {
	data []byte
	err  error
}

func (b *uxBlockingRemoteBackend) Kind() fs.Kind { return fs.KindServerRemote }
func (b *uxBlockingRemoteBackend) Root() string  { return b.root }
func (b *uxBlockingRemoteBackend) List() ([]session.FileEntry, map[string]string, error) {
	return b.entries, nil, nil
}
func (b *uxBlockingRemoteBackend) Read(rel string) ([]byte, error) {
	return b.ReadContext(context.Background(), rel)
}
func (b *uxBlockingRemoteBackend) ReadContext(ctx context.Context, rel string) ([]byte, error) {
	select {
	case b.started <- struct{}{}:
	default:
	}
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case res := <-b.readCh:
		return res.data, res.err
	}
}
func (b *uxBlockingRemoteBackend) Write(string, []byte) error { return nil }
func (b *uxBlockingRemoteBackend) Stat(string) (session.FileEntry, error) {
	return session.FileEntry{}, nil
}
func (b *uxBlockingRemoteBackend) Mkdir(string) error            { return nil }
func (b *uxBlockingRemoteBackend) Rename(string, string) error   { return nil }
func (b *uxBlockingRemoteBackend) Copy(string, string) error     { return nil }
func (b *uxBlockingRemoteBackend) Remove(string) error           { return nil }
func (b *uxBlockingRemoteBackend) CopyFrom(string, string) error { return nil }
func (b *uxBlockingRemoteBackend) MoveFrom(string, string) error { return nil }
func (b *uxBlockingRemoteBackend) RemoveAbs(string) error        { return nil }
func (b *uxBlockingRemoteBackend) Abs(rel string) (string, error) {
	return filepath.Join(b.root, rel), nil
}
func (b *uxBlockingRemoteBackend) RelOf(string) (string, bool)     { return "", false }
func (b *uxBlockingRemoteBackend) SameFile(string, string) bool    { return false }
func (b *uxBlockingRemoteBackend) LocalPath(string) (string, bool) { return "", false }
func (b *uxBlockingRemoteBackend) Client() *session.Client         { return nil }
func (b *uxBlockingRemoteBackend) CacheDir() string                { return "" }
func (b *uxBlockingRemoteBackend) Fetch(string, int64, time.Time, string) (string, error) {
	return "", errors.New("unsupported")
}
func (b *uxBlockingRemoteBackend) Trash(string) error { return nil }

func TestAsyncRemoteLoadNoteAndCancel(t *testing.T) {
	registerFonts()
	backend := &uxBlockingRemoteBackend{
		root: "/remote/proj",
		entries: []session.FileEntry{
			{Rel: "slow.md", Kind: filekind.Text, Mod: time.Now()},
		},
		readCh:  make(chan uxReadResult, 1),
		started: make(chan struct{}, 2),
	}
	s := project.New(backend)
	if err := s.Refresh(); err != nil {
		t.Fatal(err)
	}
	posted := make(chan func(), 8)
	a := &app{
		workspace: &workspace{store: s, treeOpen: true},
		uiPost: func(fn func()) {
			posted <- fn
		},
	}
	drain := func(tt *ui.Tester) {
		for {
			select {
			case fn := <-posted:
				fn()
			default:
				tt.Frame()
				return
			}
		}
	}

	tt := ui.NewTester(a.view, 1100, 700)
	a.openNoteAt("slow.md", 3, 1, 1)
	p := a.focusPane()
	if p == nil || !p.loading {
		t.Fatalf("expected non-blocking loading pane, got %+v", p)
	}
	select {
	case <-backend.started:
	case <-time.After(2 * time.Second):
		t.Fatal("background read did not start")
	}
	tt.Frame()
	if !tt.HasText("Loading slow…") {
		t.Fatalf("expected loading UI, got %q", tt.Texts())
	}

	// Test Cancel button stops loading without freezing.
	if err := tt.Click("Cancel"); err != nil {
		t.Fatal(err)
	}
	drain(tt)
	drain(tt)
	if p.loading || p.loadErr == "" {
		t.Fatalf("expected canceled load error, loading=%v err=%q", p.loading, p.loadErr)
	}

	// Retry and complete with content -> should load text and apply pending line 3.
	if err := tt.Click("Retry"); err != nil {
		t.Fatal(err)
	}
	drain(tt)
	select {
	case <-backend.started:
	case <-time.After(2 * time.Second):
		t.Fatal("retry read did not start")
	}
	backend.readCh <- uxReadResult{data: []byte("one\ntwo\nthree\nfour\n")}
	select {
	case fn := <-posted:
		fn()
	case <-time.After(2 * time.Second):
		t.Fatal("completion did not post to UI")
	}
	tt.Frame()
	if !p.Note.Loaded || p.Note.Text() != "one\ntwo\nthree\nfour\n" {
		t.Fatalf("expected loaded note text, got loaded=%v text=%q", p.Note.Loaded, p.Note.Text())
	}
	if p.state == nil || p.state.CursorPos().Line != 2 {
		t.Fatalf("expected deferred jump to line 3 (0-based 2), got %+v", p.state)
	}
}

func TestLayoutPersistsCursorAndScroll(t *testing.T) {
	registerFonts()
	dir := t.TempDir()
	var lines []string
	for i := 1; i <= 150; i++ {
		lines = append(lines, "line content for scroll persistence test")
	}
	testutil.WriteVault(t, dir, map[string]string{
		"long.md": strings.Join(lines, "\n") + "\n",
	})
	layoutPath := filepath.Join(t.TempDir(), "layout.json")
	a := &app{workspace: &workspace{treeOpen: true, layoutPath: layoutPath}}
	s, err := openStoreAt(dir)
	if err != nil {
		t.Fatal(err)
	}
	a.store = s
	a.openNote(s.ByRelPath("long.md"))
	tt := ui.NewTester(a.view, 1100, 700)
	tt.Frame()
	tt.Frame()
	p := a.focusPane()
	p.state.GoTo(editor.Pos{Line: 90, Col: 5})
	tt.Frame()
	p.state.SetScrollY(420)
	tt.Frame()
	wantScroll := p.state.ScrollY()
	if wantScroll <= 0 {
		t.Fatalf("expected positive scroll offset, got %v", wantScroll)
	}
	if err := a.saveLayout(); err != nil {
		t.Fatal(err)
	}

	// Restore into a fresh app and verify cursor & scroll.
	a2 := &app{workspace: &workspace{treeOpen: true, layoutPath: layoutPath}}
	s2, err := openStoreAt(dir)
	if err != nil {
		t.Fatal(err)
	}
	a2.store = s2
	if st := a2.restoreLayoutStatus(); st != layoutRestoreOK {
		t.Fatalf("expected layoutRestoreOK, got %v", st)
	}
	tt2 := ui.NewTester(a2.view, 1100, 700)
	tt2.Frame()
	tt2.Frame()
	tt2.Frame()
	p2 := a2.focusPane()
	if p2 == nil || p2.state == nil {
		t.Fatal("restored pane missing state")
	}
	if got := p2.state.CursorPos(); got.Line != 90 || got.Col != 5 {
		t.Fatalf("restored cursor = %+v, want Line:90 Col:5", got)
	}
	if got := p2.state.ScrollY(); got < wantScroll-2 || got > wantScroll+2 {
		t.Fatalf("restored scrollY = %v, want ~%v", got, wantScroll)
	}
}

func TestSearchSeedsSelectedText(t *testing.T) {
	registerFonts()
	dir := t.TempDir()
	testutil.WriteVault(t, dir, map[string]string{
		"a.go": "package main\nfunc helloWorld() {}\n",
	})
	a := &app{workspace: &workspace{treeOpen: true}}
	s, err := openStoreAt(dir)
	if err != nil {
		t.Fatal(err)
	}
	a.store = s
	a.openNote(s.ByRelPath("a.go"))
	tt := ui.NewTester(a.view, 1100, 700)
	tt.Frame()

	p := a.focusPane()
	text := p.Note.Text()
	idx := strings.Index(text, "helloWorld")
	p.state.Select(idx, idx+len("helloWorld"))
	a.openSearch()
	if a.searchQuery != "helloWorld" {
		t.Fatalf("expected searchQuery seeded with 'helloWorld', got %q", a.searchQuery)
	}

	// Multi-line selection should not overwrite the previous query.
	a.closeSearch()
	p.state.Select(0, len(text))
	a.openSearch()
	if a.searchQuery != "helloWorld" {
		t.Fatalf("multiline selection overwrote searchQuery: %q", a.searchQuery)
	}
}

func TestToastNotifyAndBranchFilter(t *testing.T) {
	registerFonts()
	dir, base := branchRepo(t)
	a, tt := branchTestApp(t, dir)

	// Toast appears when no error banner is active, and hides behind errorBanner.
	a.notifySuccess("Saved a.md")
	tt.Frame()
	if !tt.HasText("Saved a.md") {
		t.Fatalf("expected toast banner, got %q", tt.Texts())
	}
	a.fail(errors.New("disk full"))
	tt.Frame()
	if tt.HasText("Saved a.md") || !tt.HasText("disk full") {
		t.Fatalf("expected error banner to supersede toast, got %q", tt.Texts())
	}
	a.err = ""
	a.stopToast()
	tt.Frame()

	// Create a second branch so filtering distinguishes branches.
	branchTestGit(t, dir, "checkout", "-q", "-b", "release-v2")
	branchTestGit(t, dir, "checkout", "-q", base)
	a.refreshBranch()
	tt.Frame()

	// Open branch menu, type filter query, and press Enter to check out.
	if err := tt.Click("Switch branch: " + base); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	tt.Type("rel")
	tt.Frame()
	if !tt.HasText("release-v2") || tt.HasText("feature") {
		t.Fatalf("expected branch filter to keep release-v2 and hide feature, got %q", tt.Texts())
	}
	tt.Key(0, ui.KeyEnter)
	for range 3 {
		tt.Frame()
	}
	if a.branch.data.Current != "release-v2" {
		t.Fatalf("expected checkout release-v2 via filter+Enter, got %+v", a.branch.data)
	}
	if !tt.HasText("Switched to release-v2") {
		t.Fatalf("expected toast after branch switch, got %q", tt.Texts())
	}
}

// Verify gitignore/os.WriteFile unused check stays compliant.
var _ = os.ErrNotExist
