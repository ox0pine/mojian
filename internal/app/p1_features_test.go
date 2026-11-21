package app

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestSSHConfigImportInSettings(t *testing.T) {
	registerFonts()
	if !remoteSupported() {
		t.Skip("remote hosts unsupported on this platform")
	}
	cfg := filepath.Join(t.TempDir(), "config")
	if err := os.WriteFile(cfg, []byte("Host *\n  User root\nHost alpha\n  HostName 10.0.0.1\nHost beta\n  HostName 10.0.0.2\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	orig := sshConfigPathFn
	sshConfigPathFn = func() string { return cfg }
	defer func() { sshConfigPathFn = orig }()

	a := newProjectApp(t)
	a.settingsPath = filepath.Join(t.TempDir(), "settings.json")
	a.settings.Cat = settingsHosts
	a.addHost("existing", "alpha")

	a.openSSHConfigImport()
	if !a.sshImportOpen || len(a.sshImportCandidates) != 1 || a.sshImportCandidates[0].Address != "beta" {
		t.Fatalf("candidates = %+v, open=%v", a.sshImportCandidates, a.sshImportOpen)
	}

	tt := ui.NewTester(a.settingsWindowView, 900, 640)
	tt.Frame()
	if !tt.HasText("Hosts from ~/.ssh/config") || !tt.HasText("beta") {
		t.Fatalf("expected import panel in Settings > Hosts, texts=%q", tt.Texts())
	}

	a.importAllSSHHosts()
	if a.sshImportOpen || len(a.hosts()) != 2 {
		t.Fatalf("after import: open=%v hosts=%+v", a.sshImportOpen, a.hosts())
	}
}

func TestRemoteMarkdownLoadsRelativeImages(t *testing.T) {
	svg := []byte(`<svg xmlns="http://www.w3.org/2000/svg" width="16" height="16"><rect width="16" height="16"/></svg>`)
	var gotRel string
	load := markdownImages("/srv/repo/docs", false, "docs/guide.md", func(rel string) ([]byte, error) {
		gotRel = rel
		return svg, nil
	})
	src, err := load("./img/diagram.svg?v=1")
	if err != nil {
		t.Fatalf("remote image load failed: %v", err)
	}
	if src == nil || gotRel != "docs/img/diagram.svg" {
		t.Fatalf("src=%v rel=%q, want docs/img/diagram.svg", src, gotRel)
	}

	// Escaping the project root must be refused.
	if _, err := load("../../../etc/passwd"); err == nil {
		t.Fatal("expected path traversal image to be rejected")
	}
}
