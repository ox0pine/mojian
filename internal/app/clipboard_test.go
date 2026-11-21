package app

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/transfer"
)

// useClipboardFiles makes the next clipboard reads see files, and
// restores the real clipboard when the test ends.
func useClipboardFiles(t *testing.T, files ...string) {
	t.Helper()
	oldRead, oldWrite := readClipboardFiles, writeClipboardFiles
	readClipboardFiles = func() ([]string, error) { return files, nil }
	writeClipboardFiles = func(...string) error { return nil }
	t.Cleanup(func() { readClipboardFiles, writeClipboardFiles = oldRead, oldWrite })
}

func useClipboardReadError(t *testing.T, err error) {
	t.Helper()
	oldRead, oldWrite := readClipboardFiles, writeClipboardFiles
	readClipboardFiles = func() ([]string, error) { return nil, err }
	writeClipboardFiles = func(...string) error { return nil }
	t.Cleanup(func() { readClipboardFiles, writeClipboardFiles = oldRead, oldWrite })
}

func TestPasteUsesSystemClipboardWhenCutIsStale(t *testing.T) {
	a := newProjectApp(t)
	// A window makes the app read the system clipboard, as it does in the
	// real window; the test seams supply its contents.
	a.win = &mygo.Window{}
	py := a.store.ByRelPath(filepath.Join("scripts", "tasks.py"))
	js := a.store.ByRelPath(filepath.Join("scripts", "app.js"))
	jsAbs, err := a.store.AbsRel(js.Rel)
	if err != nil {
		t.Fatal(err)
	}
	// The user copied another file in Finder after the cut. The old cut
	// must not move the Python file; the Finder file is pasted instead.
	useClipboardFiles(t, jsAbs)
	a.copySelectionTo(py.Rel, true)
	if !a.fileCut {
		t.Fatal("copy did not mark a cut")
	}
	a.pasteInto("Ideas")

	if _, err := os.Stat(filepath.Join(a.store.Dir, "scripts", "tasks.py")); err != nil {
		t.Fatalf("the stale cut removed the source: %v", err)
	}
	if a.store.ByRelPath(filepath.Join("scripts", "tasks.py")) != py {
		t.Fatal("the stale cut moved the source file")
	}
	if a.store.ByRelPath(filepath.Join("Ideas", "app copy.js")) == nil {
		t.Fatalf("the clipboard file was not pasted (err=%q)", a.err)
	}
	if a.fileCut {
		t.Fatal("a copy paste cleared the cut marker incorrectly")
	}
}

func TestCutMovesWhenTheClipboardStillHoldsIt(t *testing.T) {
	a := newProjectApp(t)
	a.win = &mygo.Window{}
	py := a.store.ByRelPath(filepath.Join("scripts", "tasks.py"))
	abs, err := a.store.AbsRel(py.Rel)
	if err != nil {
		t.Fatal(err)
	}
	useClipboardFiles(t, abs)
	a.copySelectionTo(py.Rel, true)
	a.pasteInto("Ideas")

	if a.store.ByRelPath(filepath.Join("scripts", "tasks.py")) != nil {
		t.Fatal("the cut source is still in the project")
	}
	if a.store.ByRelPath(filepath.Join("Ideas", "tasks.py")) == nil {
		t.Fatalf("the cut was not pasted (err=%q)", a.err)
	}
	if a.fileCut {
		t.Fatal("a move left the cut marker set")
	}
}

func TestPasteWithTextOnlyClipboardDoesNotMoveCut(t *testing.T) {
	a := newProjectApp(t)
	a.win = &mygo.Window{}
	py := a.store.ByRelPath(filepath.Join("scripts", "tasks.py"))
	useClipboardReadError(t, transfer.ErrFormat)
	a.copySelectionTo(py.Rel, true)
	a.pasteInto("Ideas")

	if a.err != "the clipboard has no files" {
		t.Fatalf("paste error = %q, want the no-files error", a.err)
	}
	if _, err := os.Stat(filepath.Join(a.store.Dir, "scripts", "tasks.py")); err != nil {
		t.Fatalf("the text-only clipboard moved the cut source: %v", err)
	}
	if a.store.ByRelPath(filepath.Join("Ideas", "tasks.py")) != nil {
		t.Fatal("the old cut was pasted after the system clipboard became text")
	}
}

func TestPasteFallsBackToCopyWhenClipboardReadFails(t *testing.T) {
	a := newProjectApp(t)
	a.win = &mygo.Window{}
	useClipboardReadError(t, errors.New("clipboard unavailable"))

	py := a.store.ByRelPath(filepath.Join("scripts", "tasks.py"))
	a.copySelectionTo(py.Rel, true)
	a.pasteInto("Ideas")

	if a.err != "" {
		t.Fatalf("the internal clipboard fallback failed: %q", a.err)
	}
	if _, err := os.Stat(filepath.Join(a.store.Dir, "scripts", "tasks.py")); err != nil {
		t.Fatalf("an unverified cut moved the source: %v", err)
	}
	if a.store.ByRelPath(filepath.Join("Ideas", "tasks copy.py")) == nil {
		t.Fatal("the internal clipboard fallback did not copy the file")
	}
	if a.fileCut {
		t.Fatal("the copy fallback left the cut marker set")
	}
}

func TestSwitchingProjectClearsTheCut(t *testing.T) {
	a := newProjectApp(t)
	py := a.store.ByRelPath(filepath.Join("scripts", "tasks.py"))
	a.copySelectionTo(py.Rel, true)
	store := a.store
	a.activateWorkspace(&workspace{key: "other", store: store, openDirs: map[string]bool{}})
	if a.fileCut {
		t.Fatal("a cut survived a project switch")
	}
	if a.store != store {
		t.Fatal("activating a workspace lost its store")
	}
}
