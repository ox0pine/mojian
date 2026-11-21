package app

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"strings"

	"mojian/internal/session"
)

// These seams keep SSH and RPC blocking in tests without real network
// waits. Production workers own their results until onWindow hands them back.
var dialTerminalRemoteFn = dialRemoteContext
var createTerminalSessionFn = func(client *session.Client, opts session.CreateOptions) (session.SessionInfo, error) {
	return client.Create(opts)
}
var killTerminalSessionFn = func(client *session.Client, sid string) error {
	return client.Kill(sid)
}

// terminalCreateOptions is what a new session needs, read on the UI
// thread before the request goes out.
func (a *app) terminalCreateOptions(spec termSpec) session.CreateOptions {
	var cmd []string
	dir := a.terminalDir()
	if spec.Host != "" {
		cmd = sshArgs(spec.Host)
		dir = ""
	} else if spec.Server != localServerID {
		dir = a.remotePath
	} else if strings.TrimSpace(a.ShellPath) != "" {
		cmd = []string{a.ShellPath}
		if runtime.GOOS != "windows" {
			cmd = append(cmd, "-l")
		}
	}
	return session.CreateOptions{Command: cmd, Dir: dir, Cols: 80, Rows: 24}
}

// prepareTerminalSession reuses a saved session when it still exists, or
// creates a new one. It may run away from the UI thread.
func prepareTerminalSession(client *session.Client, sid string, opts session.CreateOptions) (string, bool, error) {
	if sid != "" {
		if list, err := client.List(); err == nil {
			for _, info := range list {
				if info.ID == sid && !info.Exited {
					return sid, false, nil
				}
			}
		}
	}
	info, err := createTerminalSessionFn(client, opts)
	if err != nil {
		return "", false, err
	}
	return info.ID, true, nil
}

type remoteTerminalResult struct {
	conn    *remoteConn
	host    string
	seq     uint64
	sid     string
	created bool
	dialed  bool
	err     error
}

// startRemoteTerminal opens a session for a pane already in the layout.
// The worker owns every connection it dials until the UI callback chooses
// the pooled connection or closes the duplicate. Create is deliberately
// not cancelled: once it has begun, waiting for its SID is the only way to
// reliably kill a session whose pane went away.
func (a *app) startRemoteTerminal(p *pane) {
	if p == nil || p.Term == nil || p.Term.Term != nil || p.Term.connecting {
		return
	}
	t := p.Term
	host := t.Spec.Server
	if t.Spec.Host != "" || host == "" || host == localServerID {
		return
	}
	opts := a.terminalCreateOptions(t.Spec)
	existing := a.remoteForHost(host)
	temporaryKeep := !a.remoteKeep[host]
	if temporaryKeep {
		a.keepRemote(host)
	}
	seq := t.beginConnect()
	t.startErr = ""
	a.invalidate()
	win := a.win
	go func() {
		result := remoteTerminalResult{conn: existing, host: host, seq: seq, dialed: existing == nil}
		if result.conn == nil {
			result.conn, result.err = dialTerminalRemoteFn(context.Background(), host)
		}
		if result.err == nil {
			if result.conn == nil {
				result.err = errors.New("remote server returned no connection")
			} else {
				result.sid, result.created, result.err = prepareTerminalSession(result.conn.Client, "", opts)
				if result.err != nil {
					result.err = fmt.Errorf("session: %w", result.err)
				}
			}
		}
		onWindow(win, func() {
			release := func() {
				if temporaryKeep {
					delete(a.remoteKeep, host)
				}
				a.reapRemotes()
			}
			a.finishRemoteTerminal(p, t, result, a.win == win, release, func() { onWindow(win, release) })
		})
	}()
}

// finishRemoteTerminal applies a worker's result on the UI thread. A
// connection dialed by this start is installed only if the pane is still
// live; a pooled connection that appeared during Dial wins over it.
func (a *app) finishRemoteTerminal(p *pane, t *termTab, result remoteTerminalResult, windowCurrent bool, release, releaseAsync func()) {
	host := result.host
	current := windowCurrent && p.Term == t && !p.closing && a.tabOf(p) != nil &&
		t.connectSeq == result.seq && t.Spec.Server == host
	if p.Term == t && t.connectSeq == result.seq && !current {
		t.connecting = false
	}
	chosen := result.conn
	var closeConn *remoteConn
	if result.dialed && result.conn != nil {
		if existing := a.remoteForHost(host); existing != nil && existing != result.conn {
			chosen = existing
			closeConn = result.conn
		}
	}
	if current && chosen != nil && result.dialed {
		a.installRemoteConn(chosen)
	}
	if !current && closeConn == nil && result.dialed && chosen != nil && a.remoteConn(host) != chosen {
		closeConn = chosen
	}
	closeDuplicate := func() {
		if closeConn != nil && current {
			go closeRemoteConn(closeConn)
			closeConn = nil
		}
	}
	if !current {
		var client *session.Client
		if result.conn != nil {
			client = result.conn.Client
		} else if chosen != nil {
			client = chosen.Client
		}
		sid := ""
		if result.created {
			sid = result.sid
		}
		cleanupRemoteTerminal(client, sid, closeConn, releaseAsync)
		return
	}
	if result.err == nil && (chosen == nil || chosen.Client == nil) {
		result.err = errors.New("remote server returned no client")
	}
	if result.err != nil {
		t.connecting = false
		t.startErr = result.err.Error()
		if chosen != nil {
			t.Client = chosen.Client
		}
		closeDuplicate()
		release()
		a.invalidate()
		return
	}
	tt, err := a.attachTerminal(chosen.Client, result.sid, t.Spec)
	if err != nil {
		t.connecting = false
		t.startErr = err.Error()
		closeDuplicate()
		if result.created {
			cleanupRemoteTerminal(chosen.Client, result.sid, nil, releaseAsync)
		} else {
			release()
		}
		a.invalidate()
		return
	}
	t.Term, t.SID, t.stream, t.Client = tt.Term, tt.SID, tt.stream, tt.Client
	t.connecting = false
	t.startErr = ""
	closeDuplicate()
	release()
	a.changed()
	a.invalidate()
}

// cleanupRemoteTerminal kills an unattached session before releasing its
// connection and pool keep. done returns to the window thread.
func cleanupRemoteTerminal(client *session.Client, sid string, closeConn *remoteConn, done func()) {
	go func() {
		if sid != "" {
			_ = killTerminalSessionFn(client, sid)
		}
		if closeConn != nil {
			closeRemoteConn(closeConn)
		}
		done()
	}()
}

// startRestoredTerminal attaches a terminal restored by the layout. The
// session lookup and creation run away from the UI thread; the view is
// built once the answer is on the window's thread. A test has no window
// and runs the whole path synchronously.
func (a *app) startRestoredTerminal(p *pane) {
	if p == nil || p.Term == nil || p.Term.Term != nil || p.Term.connecting || p.Term.startErr != "" {
		return
	}
	t := p.Term
	if t.Spec.Server == "" {
		// Layouts written before terminals carried a server keep working:
		// a direct SSH pane lives locally; a plain shell follows the
		// project that was open when the layout was written.
		switch {
		case t.Spec.Host != "":
			t.Spec.Server = localServerID
		case a.remotePath != "" && a.serverAlias != "":
			t.Spec.Server = a.serverAlias
		default:
			t.Spec.Server = localServerID
		}
	}
	if t.SID == "" && t.Spec.Host == "" {
		if t.Spec.Server != localServerID && a.shellStarter == nil {
			a.startRemoteTerminal(p)
		}
		return
	}
	// The terminal belongs to the server that already owns the project.
	// A view never dials SSH itself, but a remote terminal on the project
	// on screen may wait for the project's connection to come back after
	// a window reopen; attach then instead of showing a dead end.
	client, err := a.terminalClientExisting(t.Spec)
	if err != nil {
		if a.win != nil && t.Spec.Server != "" && t.Spec.Server != localServerID &&
			t.Spec.Server == a.serverAlias {
			t.beginConnect()
			if !a.remoteBusy {
				a.reconnectActiveRemoteAsync()
			}
			a.invalidate()
			return
		}
		t.startErr = err.Error()
		a.invalidate()
		return
	}
	opts := a.terminalCreateOptions(t.Spec)
	sid := t.SID
	seq := t.beginConnect()
	a.invalidate()
	win := a.win
	finish := func(sid string, created bool, err error) {
		stale := p.Term != t || a.win != win || (win != nil && a.tabOf(p) == nil) || t.connectSeq != seq
		if p.Term == t && t.connectSeq == seq {
			t.connecting = false
		}
		if stale {
			// Never attach an old result to a replacement pane. A newly
			// created session still needs its SID reaped.
			if created {
				go func() { _ = killTerminalSessionFn(client, sid) }()
			}
			return
		}
		if err != nil {
			t.startErr = err.Error()
			a.invalidate()
			return
		}
		tt, err := a.attachTerminal(client, sid, t.Spec)
		if err != nil {
			if created {
				go func() { _ = killTerminalSessionFn(client, sid) }()
			}
			t.startErr = err.Error()
			a.invalidate()
			return
		}
		t.Term, t.SID, t.stream, t.Client = tt.Term, tt.SID, tt.stream, tt.Client
		t.startErr = ""
		a.changed()
		a.invalidate()
	}
	if a.win == nil {
		// Tests and a headless build have no window thread.
		sid, created, err := prepareTerminalSession(client, sid, opts)
		finish(sid, created, err)
		return
	}
	go func() {
		sid, created, err := prepareTerminalSession(client, sid, opts)
		onWindow(win, func() { finish(sid, created, err) })
	}()
}

// detachTerminalView closes the local terminal and stream without
// ending the session on the server.
func detachTerminalView(p *pane) {
	if p == nil || p.Term == nil {
		return
	}
	if p.Term.Term != nil {
		_ = p.Term.Term.Close()
		p.Term.Term = nil
	}
	if p.Term.stream != nil {
		_ = p.Term.stream.Close()
		p.Term.stream = nil
	}
	p.Term.invalidateConnect()
}

// endTerminalPane closes a pane's terminal view, ending its session on
// the server when kill is set.
func (a *app) endTerminalPane(p *pane, kill bool) {
	if p == nil || p.Term == nil {
		return
	}
	if kill {
		a.endTerminal(p.Term)
	}
	detachTerminalView(p)
}

// endTerminal ends a session when its pane is closed by the user.
func (a *app) endTerminal(t *termTab) {
	if t == nil || t.SID == "" {
		return
	}
	client := t.Client
	if client == nil {
		if t.Spec.Server == "" || t.Spec.Server == localServerID || t.Spec.Host != "" {
			client = a.sessionClient
		} else if rc := a.remoteForHost(t.Spec.Server); rc != nil {
			client = rc.Client
		}
	}
	if client == nil {
		return
	}
	sid := t.SID
	go func() { _ = killTerminalSessionFn(client, sid) }()
}
