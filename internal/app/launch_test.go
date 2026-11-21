package app

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"mojian/internal/testutil"
)

func TestParseStartup(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "proj")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(dir, "note.md")
	if err := os.WriteFile(file, []byte("# hi\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if s, err := ParseStartup(nil, ""); err != nil || s.Dir != "" || s.File != "" {
		t.Fatalf("no path: %+v %v", s, err)
	}
	s, err := ParseStartup([]string{"-psn_0_1234"}, "")
	if err != nil || s.Dir != "" {
		t.Fatalf("LaunchServices argument: %+v %v", s, err)
	}
	s, err = ParseStartup([]string{dir}, "")
	if err != nil || s.Dir != dir || s.File != "" {
		t.Fatalf("folder: %+v %v", s, err)
	}
	s, err = ParseStartup([]string{file}, "")
	if err != nil || s.Dir != dir || s.File != file {
		t.Fatalf("file: %+v %v", s, err)
	}

	// A handed-over launch keeps the directory it ran in, which is how a
	// relative path from another shell still resolves.
	s, err = ParseStartup([]string{"proj"}, root)
	if err != nil || s.Dir != dir {
		t.Fatalf("relative folder: %+v %v", s, err)
	}
	s, err = ParseStartup([]string{filepath.Join("proj", "note.md")}, root)
	if err != nil || s.Dir != dir || s.File != file {
		t.Fatalf("relative file: %+v %v", s, err)
	}

	// A quoted ~ reaches the binary before the shell expands it. Windows
	// reads USERPROFILE where the other systems read HOME.
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	proj := filepath.Join(home, "abc")
	if err := os.MkdirAll(proj, 0o755); err != nil {
		t.Fatal(err)
	}
	s, err = ParseStartup([]string{"~/abc"}, "")
	if err != nil || s.Dir != proj {
		t.Fatalf("tilde: %+v %v", s, err)
	}

	if _, err := ParseStartup([]string{"-x"}, ""); err == nil {
		t.Fatal("an unknown option was accepted")
	}
	if _, err := ParseStartup([]string{dir, dir}, ""); err == nil {
		t.Fatal("two paths were accepted")
	}
	if _, err := ParseStartup([]string{filepath.Join(root, "missing")}, ""); err == nil {
		t.Fatal("a missing path was accepted")
	}
	if _, err := ParseStartup([]string{"-h"}, ""); !errors.Is(err, ErrHelp) {
		t.Fatalf("help: %v", err)
	}
}

func TestSecondInstanceWithoutPathKeepsPendingStartup(t *testing.T) {
	queued := Startup{Dir: filepath.Join(t.TempDir(), "First"), File: "first.md"}
	a := &app{startup: queued}

	a.secondInstance(nil, t.TempDir())

	if a.startup != queued {
		t.Fatalf("a pathless second launch replaced the queued startup: got %+v, want %+v", a.startup, queued)
	}
}

func TestSecondInstanceOpensFileAfterRemoteProjectSave(t *testing.T) {
	startLocalServerForTest(t)
	remote, backend, _ := newBlockingSaveStore(t, "saved before switching\n")
	posts := make(chan func(), 1)
	a := &app{
		workspace: &workspace{store: remote, treeOpen: true},
		dataDir:   t.TempDir(),
		uiPost:    func(fn func()) { posts <- fn },
	}
	target := filepath.Join(t.TempDir(), "Target")
	testutil.WriteVault(t, target, map[string]string{"b.md": "# B\n"})
	startup := Startup{Dir: target, File: filepath.Join(target, "b.md")}

	a.applyStartup(startup)
	if a.store != remote {
		t.Fatal("the project switched before the remote save completed")
	}
	if n := a.currentNote(); n != nil {
		t.Fatalf("the startup file opened in the old remote project: %+v", n)
	}

	waitSaveSignal(t, backend.started)
	close(backend.unblock)
	waitSavePost(t, posts)()

	if a.store == nil || a.store.Dir != resolveProjectRoot(target) {
		t.Fatalf("the saved project did not switch: %+v", a.store)
	}
	if n := a.currentNote(); n == nil || n.Rel != "b.md" {
		t.Fatalf("the startup file did not open after switching: %+v", n)
	}
	if a.startup != (Startup{}) {
		t.Fatalf("the completed startup was not consumed: %+v", a.startup)
	}
}

func TestSecondInstanceCancelsRemoteTaskAndOpensFile(t *testing.T) {
	startLocalServerForTest(t)
	root := t.TempDir()
	active := filepath.Join(root, "Active")
	other := filepath.Join(root, "Other")
	testutil.WriteVault(t, active, map[string]string{"a.md": "# A\n"})
	testutil.WriteVault(t, other, map[string]string{"b.md": "# B\n"})
	a := &app{workspace: &workspace{treeOpen: true}, dataDir: t.TempDir()}
	s, err := openStoreAt(active)
	if err != nil {
		t.Fatal(err)
	}
	a.store = s
	a.layoutPath = a.projectLayoutPath(s.Dir)
	a.project = active
	a.focusFirstNote()
	a.registerActiveWorkspace()
	seq, _, ctx := a.beginRemoteTask("box", "Opening an obsolete remote project")

	a.applyStartup(Startup{Dir: other, File: filepath.Join(other, "b.md")})

	select {
	case <-ctx.Done():
	default:
		t.Fatal("the second launch did not cancel the obsolete remote task")
	}
	if a.endRemoteTask(seq) {
		t.Fatal("the canceled remote task could still publish over the second launch")
	}
	if a.store == nil || a.store.Dir != resolveProjectRoot(other) {
		t.Fatalf("the second launch did not open its local project: %+v", a.store)
	}
	if n := a.currentNote(); n == nil || n.Rel != "b.md" {
		t.Fatalf("the second launch lost its file continuation: %+v", n)
	}
}

func TestSecondInstanceOpensProjectAndFile(t *testing.T) {
	startLocalServerForTest(t)
	root := t.TempDir()
	active := filepath.Join(root, "Active")
	other := filepath.Join(root, "Other")
	testutil.WriteVault(t, active, map[string]string{"a.md": "# A\n"})
	testutil.WriteVault(t, other, map[string]string{"b.md": "# B\n"})
	a := &app{workspace: &workspace{treeOpen: true}, dataDir: t.TempDir()}
	s, err := openStoreAt(active)
	if err != nil {
		t.Fatal(err)
	}
	a.store = s
	a.layoutPath = a.projectLayoutPath(s.Dir)
	a.project = active
	a.focusFirstNote()
	a.registerActiveWorkspace()

	// A later launch naming a file in another project switches the window
	// to that project and shows the file.
	a.applyStartup(Startup{Dir: other, File: filepath.Join(other, "b.md")})
	if a.store == nil || a.store.Dir != resolveProjectRoot(other) {
		t.Fatalf("the second launch did not switch project: %+v", a.store)
	}
	if n := a.currentNote(); n == nil || n.Rel != "b.md" {
		t.Fatalf("the second launch did not open its file: %+v", n)
	}

	// Naming only a project switches without touching the open tab.
	a.applyStartup(Startup{Dir: active})
	if a.store == nil || a.store.Dir != resolveProjectRoot(active) {
		t.Fatalf("the second launch did not switch back: %+v", a.store)
	}
	if n := a.currentNote(); n != nil && n.Rel != "a.md" {
		t.Fatalf("a project-only launch changed the tab: %+v", n)
	}
}
