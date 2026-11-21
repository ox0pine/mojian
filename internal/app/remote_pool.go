package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"mojian/internal/fs"
	"mojian/internal/remote"
	"mojian/internal/remote/remotehost"
)

// The app reaches SSH connections and saved host lists through
// internal/remote and internal/remote/remotehost.

type remoteConn = remote.Conn
type hostConfig = remotehost.Host
type remoteProject = remotehost.Project

func dialRemoteContext(ctx context.Context, host string) (*remoteConn, error) {
	return remote.DialContext(ctx, host)
}

func closeRemoteConn(rc *remoteConn) { remote.Close(rc) }

func (a *app) hostService() *remotehost.Service {
	if a.remoteHosts == nil {
		a.remoteHosts = remotehost.New()
	}
	return a.remoteHosts
}

func (a *app) hosts() []hostConfig                       { return a.hostService().Hosts() }
func (a *app) remoteProjects() []remoteProject           { return a.hostService().Projects() }
func (a *app) remoteConn(host string) *remote.Conn       { return a.hostService().Conn(host) }
func (a *app) setRemoteConn(c *remote.Conn) bool         { return a.hostService().SetConn(c) }
func (a *app) deleteRemoteConn(host string) *remote.Conn { return a.hostService().DeleteConn(host) }

// connectRemote prepares a connection to host without replacing the
// current server, so a failed or cancelled switch leaves the open project
// talking to the machine it came from.
func (a *app) connectRemote(host string) (*remote.Conn, error) {
	if host == "" {
		return nil, fmt.Errorf("no server selected")
	}
	if rc := a.remoteForHost(host); rc != nil {
		return rc, nil
	}
	rc, err := remote.Dial(host)
	if err != nil {
		return nil, err
	}
	a.installRemoteConn(rc)
	return rc, nil
}

// remoteForHost returns the live connection the pool owns for host,
// dropping it first when it has gone stale.
func (a *app) remoteForHost(host string) *remote.Conn {
	a.initWorkspaces()
	rc := a.remoteConn(host)
	if rc == nil {
		return nil
	}
	if rc.Healthy() {
		return rc
	}
	a.deleteRemoteConn(host)
	delete(a.remoteKeep, host)
	remote.Close(rc)
	return nil
}

// activeRemote is the connection of the project on screen, if it is a
// remote one. The pool owns it; nothing else keeps a copy.
func (a *app) activeRemote() *remote.Conn {
	if a.serverAlias == "" {
		return nil
	}
	return a.remoteConn(a.serverAlias)
}

// serverAssetCache is where pictures and PDFs of a server project are
// fetched for their viewers.
func (a *app) serverAssetCache(serverID, root string) string {
	base := a.dataDir
	if base == "" {
		base, _ = os.UserCacheDir()
	}
	return filepath.Join(base, "server-assets", fs.ShortHash(serverID+"\x00"+root))
}

func (a *app) detachRemoteConn(rc *remote.Conn) {
	if rc == nil {
		return
	}
	if a.remoteConn(rc.Host) == rc {
		a.deleteRemoteConn(rc.Host)
	}
	remote.Close(rc)
}

// hostUsed reports whether a connection to host still has a user: a
// loaded project, a terminal pane, or the open project picker.
func (a *app) hostUsed(host string) bool {
	if host == "" {
		return false
	}
	if a.remotePickHost == host {
		return true
	}
	used := func(ws *workspace) bool {
		if ws == nil {
			return false
		}
		if ws.serverAlias == host && ws.remotePath != "" {
			return true
		}
		for _, t := range ws.tabs {
			for _, p := range t.panes() {
				if p.Term != nil && p.Term.Spec.Server == host {
					return true
				}
			}
		}
		return false
	}
	if used(a.workspace) {
		return true
	}
	for _, ws := range a.workspaces {
		if ws != a.workspace && used(ws) {
			return true
		}
	}
	return false
}

// keepRemote marks a host's connection as owned by setup or a loaded
// project: closing the picker and pane reaping do not release it.
func (a *app) keepRemote(host string) {
	if host == "" {
		return
	}
	if a.remoteKeep == nil {
		a.remoteKeep = map[string]bool{}
	}
	a.remoteKeep[host] = true
}

// retainRemote holds one independent lease on host's connection until a
// terminal Create or Kill has finished. The returned release is idempotent
// and must run on the UI thread; the last lease reaps an unused connection.
func (a *app) retainRemote(host string) func() {
	if host == "" {
		return func() {}
	}
	if a.remoteRefs == nil {
		a.remoteRefs = map[string]int{}
	}
	a.remoteRefs[host]++
	released := false
	return func() {
		if released {
			return
		}
		released = true
		if n := a.remoteRefs[host]; n <= 1 {
			delete(a.remoteRefs, host)
		} else {
			a.remoteRefs[host] = n - 1
		}
		a.reapRemotes()
	}
}

// releaseRemote closes host's pooled connection when nothing uses it any
// more. The server and its sessions keep running on the host.
func (a *app) releaseRemote(host string) {
	if host == "" || a.remoteRefs[host] > 0 {
		return
	}
	if !a.remoteClosing && (a.remoteBusy || a.remoteDirsBusy || a.hostUsed(host) || a.remoteKeep[host]) {
		return
	}
	rc := a.remoteConn(host)
	if rc == nil {
		return
	}
	delete(a.remoteKeep, host)
	a.deleteRemoteConn(host)
	remote.Close(rc)
}

// reapRemotes drops every pooled connection whose last project or
// terminal has gone.
func (a *app) reapRemotes() {
	if !a.remoteClosing && (a.remoteBusy || a.remoteDirsBusy) {
		return
	}
	for host := range a.hostService().Conns() {
		a.releaseRemote(host)
	}
}
