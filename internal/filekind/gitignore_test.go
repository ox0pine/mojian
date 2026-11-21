package filekind

import (
	"os"
	"path/filepath"
	"testing"
)

// writeIgnore puts a .gitignore in dir, making it.
func writeIgnore(t *testing.T, dir, body string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".gitignore"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestGitIgnorePatterns checks the shapes a project's .gitignore uses:
// anchored folders, names at any depth, globs and a negation.
func TestGitIgnorePatterns(t *testing.T) {
	root := t.TempDir()
	writeIgnore(t, root, "# generated\n/build/\nnode_modules\n*.cache/\nlogs\n!logs/keep\n\\#notes\n")
	g := NewGitIgnore(root)
	cases := []struct {
		rel  string
		want bool
	}{
		{"build", true},
		{"sub/build", false},
		{"node_modules", true},
		{"app/node_modules", true},
		{"tmp.cache", true},
		{"a/tmp.cache", true},
		{"logs", true},
		{"logs/keep", false},
		{"#notes", true},
		{"src", false},
		{"", false},
	}
	for _, c := range cases {
		if got := g.DirIgnored(c.rel); got != c.want {
			t.Errorf("DirIgnored(%q) = %v, want %v", c.rel, got, c.want)
		}
	}
}

func TestGitIgnorePreservesDirectorySpace(t *testing.T) {
	root := t.TempDir()
	writeIgnore(t, root, "build/\n")
	g := NewGitIgnore(root)
	for rel, want := range map[string]bool{
		"build":  true,
		" build": false,
		"build ": false,
	} {
		if got := g.DirIgnored(rel); got != want {
			t.Errorf("DirIgnored(%q) = %v, want %v", rel, got, want)
		}
	}
}

// TestGitIgnoreNestedWins checks that a deeper .gitignore overrides its
// parent, as Git applies them.
func TestGitIgnoreNestedWins(t *testing.T) {
	root := t.TempDir()
	writeIgnore(t, root, "cache/\n")
	writeIgnore(t, filepath.Join(root, "vendor"), "!cache/\ncache/keep\n")
	g := NewGitIgnore(root)
	if !g.DirIgnored("cache") {
		t.Error("the root rule did not hide cache")
	}
	if g.DirIgnored("vendor/cache") {
		t.Error("the nested rule did not bring cache back")
	}
	if !g.DirIgnored("vendor/cache/keep") {
		t.Error("the last matching rule did not win")
	}
}

// TestGitIgnoreCrossesFolders checks ** against *.
func TestGitIgnoreCrossesFolders(t *testing.T) {
	root := t.TempDir()
	writeIgnore(t, root, "**/generated/\ndocs/*/out/\n")
	g := NewGitIgnore(root)
	for rel, want := range map[string]bool{
		"generated":     true,
		"a/b/generated": true,
		"docs/x/out":    true,
		"docs/x/y/out":  false,
		"docs/out":      false,
	} {
		if got := g.DirIgnored(rel); got != want {
			t.Errorf("DirIgnored(%q) = %v, want %v", rel, got, want)
		}
	}
}

func TestGitIgnoreDoubleStarInsideComponent(t *testing.T) {
	root := t.TempDir()
	writeIgnore(t, root, "a**b/\n")
	g := NewGitIgnore(root)
	for rel, want := range map[string]bool{
		"aXXb":        true,
		"a/b":         false,
		"a/x/b":       false,
		"nested/aXXb": true,
		"nested/a/b":  false,
	} {
		if got := g.DirIgnored(rel); got != want {
			t.Errorf("DirIgnored(%q) = %v, want %v", rel, got, want)
		}
	}
}

func TestGitIgnorePreservesEscapedTrailingSpace(t *testing.T) {
	root := t.TempDir()
	writeIgnore(t, root, "cache\\ \nplain \n")
	g := NewGitIgnore(root)
	for rel, want := range map[string]bool{
		"cache ":        true,
		"cache":         false,
		"nested/cache ": true,
		"plain":         true,
		"plain ":        false,
	} {
		if got := g.DirIgnored(rel); got != want {
			t.Errorf("DirIgnored(%q) = %v, want %v", rel, got, want)
		}
	}
}

func TestGitIgnorePOSIXAndEscapedRightBracket(t *testing.T) {
	root := t.TempDir()
	writeIgnore(t, root, "[[:digit:]]/\n[a\\]]/\n")
	g := NewGitIgnore(root)
	for rel, want := range map[string]bool{
		"5":  true,
		"5]": false,
		"a":  true,
		"]":  true,
		"a]": false,
		"x":  false,
	} {
		if got := g.DirIgnored(rel); got != want {
			t.Errorf("DirIgnored(%q) = %v, want %v", rel, got, want)
		}
	}
}
