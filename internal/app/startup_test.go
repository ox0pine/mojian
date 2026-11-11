package app

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
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
