package app

import (
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
