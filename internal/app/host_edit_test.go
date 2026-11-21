package app

import (
	"os"
	"path/filepath"
	"testing"

	"mojian/internal/remote"
	"mojian/internal/session"
)

func TestHostEditMovesTerminalsToNewAddress(t *testing.T) {
	p := newTermPane(newTermPaneSpec(termSpec{Server: "old-box"}, "session-1"))
	p.Term.Client = &session.Client{}
	p.Term.startErr = "disconnected"
	tb := &tab{ID: nextID("t"), Root: &node{Pane: p}, Focus: p}
	key := workspaceKeyRemote("old-box", "/srv/app")
	ws := &workspace{key: key, serverAlias: "old-box", remotePath: "/srv/app", tabs: []*tab{tb}}
	a := &app{
		workspace:    ws,
		workspaces:   map[string]*workspace{key: ws},
		settingsPath: filepath.Join(t.TempDir(), "settings.json"),
		remoteKeep:   map[string]bool{"old-box": true},
		remoteHomes:  map[string]string{"old-box": "/home/me"},
		hostEditAddr: "old-box",
	}
	a.hostService().AddHost("Old", "old-box")
	a.hostService().RememberProject(remoteProject{Host: "old-box", Path: "/srv/app"})
	a.hostEditAddress, a.hostEditName = "new-box", "New"

	a.saveHostEdit("old-box")

	if p.Term.Spec.Server != "new-box" || p.Term.Client != nil {
		t.Fatalf("terminal stayed on the old address: server=%q client=%p", p.Term.Spec.Server, p.Term.Client)
	}
	if p.Term.startErr != "" || p.Term.connecting {
		t.Fatalf("terminal did not reset for reconnect: err=%q connecting=%v", p.Term.startErr, p.Term.connecting)
	}
	if ws.serverAlias != "new-box" || a.workspaces[workspaceKeyRemote("new-box", "/srv/app")] != ws {
		t.Fatalf("workspace was not rekeyed: alias=%q key=%q old=%p new=%p ws=%p", ws.serverAlias, ws.key, a.workspaces[key], a.workspaces[workspaceKeyRemote("new-box", "/srv/app")], ws)
	}
	newClient := &session.Client{}
	a.installRemoteConn(&remote.Conn{Host: "new-box", Client: newClient})
	if p.Term.Client != newClient {
		t.Fatal("the terminal did not receive the new connection")
	}
}

func TestHostEditMovesLayoutToNewAddress(t *testing.T) {
	fixture := newProjectApp(t)
	tb := fixture.tab()
	dataDir := t.TempDir()
	key := workspaceKeyRemote("old-box", "/srv/app")
	ws := &workspace{
		key: key, store: fixture.store, serverAlias: "old-box", remotePath: "/srv/app",
		tabs: []*tab{tb}, active: 0,
	}
	a := &app{
		workspace: ws, workspaces: map[string]*workspace{key: ws}, dataDir: dataDir,
		settingsPath: filepath.Join(dataDir, "settings.json"), hostEditAddr: "old-box",
	}
	ws.layoutPath = a.projectLayoutPath("remote:old-box:/srv/app")
	a.hostService().AddHost("Old", "old-box")
	a.saveLayout()
	oldLayout := ws.layoutPath
	newLayout := a.projectLayoutPath("remote:new-box:/srv/app")
	if err := os.MkdirAll(filepath.Dir(newLayout), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(newLayout, []byte(`{"tabs":[],"active":0}`), 0o644); err != nil {
		t.Fatal(err)
	}
	a.hostEditAddress, a.hostEditName = "new-box", "New"

	a.saveHostEdit("old-box")

	if ws.layoutPath != newLayout {
		t.Fatalf("layout path = %q, want new host path %q", ws.layoutPath, newLayout)
	}
	if _, err := os.Stat(oldLayout); !os.IsNotExist(err) {
		t.Fatalf("old layout still exists: %v", err)
	}
	b := &app{workspace: &workspace{
		store: ws.store, serverAlias: "new-box", remotePath: "/srv/app", layoutPath: newLayout,
	}}
	if !b.restoreLayout() {
		t.Fatal("the renamed host could not restore its layout")
	}
	if len(b.tabs) != 1 || len(b.tabs[0].panes()) != 1 {
		t.Fatalf("restored layout has unexpected tabs: %+v", b.tabs)
	}
	if got, want := b.tabs[0].panes()[0].Note.Rel, tb.Focus.Note.Rel; got != want {
		t.Fatalf("restored note = %q, want %q", got, want)
	}
}
