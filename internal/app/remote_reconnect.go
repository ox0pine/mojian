package app

import (
	"context"
	"time"
)

// watchRemoteConn monitors rc for unexpected disconnects (such as laptop
// sleep/wake or transient SSH drops) and automatically reconnects with
// bounded backoff while any loaded project or terminal still uses host.
func (a *app) watchRemoteConn(rc *remoteConn) {
	if rc == nil || rc.Client == nil || a.harness {
		return
	}
	post := a.uiPoster()
	if post == nil {
		return
	}
	host := rc.Host
	winSeq := a.windowSeq
	go func() {
		<-rc.Client.Closed()
		post(func() {
			if a.windowSeq != winSeq || a.quitting || a.remoteClosing {
				return
			}
			// Only reconnect if this exact connection was still the active pooled owner.
			if a.remoteConn(host) != rc {
				return
			}
			a.deleteRemoteConn(host)
			if !a.hostUsed(host) {
				return
			}
			a.startRemoteReconnect(host)
		})
	}()
}

// startRemoteReconnect dials host with exponential backoff until reconnected
// or cancelled.
func (a *app) startRemoteReconnect(host string) {
	if host == "" || a.harness || a.quitting || a.remoteClosing {
		return
	}
	if a.remoteReconnecting == nil {
		a.remoteReconnecting = map[string]bool{}
	}
	if a.remoteReconnecting[host] {
		return
	}
	if a.remoteReconnectSeq == nil {
		a.remoteReconnectSeq = map[string]int{}
	}
	if a.remoteReconnectCancel == nil {
		a.remoteReconnectCancel = map[string]context.CancelFunc{}
	}
	post := a.uiPoster()
	if post == nil {
		return
	}
	a.remoteReconnectSeq[host]++
	seq := a.remoteReconnectSeq[host]
	ctx, cancel := context.WithCancel(context.Background())
	a.remoteReconnectCancel[host] = cancel
	a.remoteReconnecting[host] = true
	winSeq := a.windowSeq
	a.invalidate()

	go func() {
		defer cancel()
		backoffs := []time.Duration{
			1 * time.Second,
			2 * time.Second,
			4 * time.Second,
			8 * time.Second,
			15 * time.Second,
			15 * time.Second,
		}
		for _, delay := range backoffs {
			select {
			case <-ctx.Done():
				return
			case <-time.After(delay):
			}
			dialCtx, dialCancel := context.WithTimeout(ctx, 20*time.Second)
			rc, err := dialRemoteContext(dialCtx, host)
			dialCancel()
			if err != nil {
				if ctx.Err() != nil {
					return
				}
				continue
			}
			post(func() {
				if a.windowSeq != winSeq || a.remoteReconnectSeq[host] != seq || a.quitting || a.remoteClosing || !a.hostUsed(host) {
					closeRemoteConn(rc)
					return
				}
				delete(a.remoteReconnecting, host)
				delete(a.remoteReconnectCancel, host)
				a.installRemoteConn(rc)
				a.syncProjectWatcher()
				a.invalidate()
			})
			return
		}
		post(func() {
			if a.windowSeq != winSeq || a.remoteReconnectSeq[host] != seq {
				return
			}
			delete(a.remoteReconnecting, host)
			delete(a.remoteReconnectCancel, host)
			a.invalidate()
		})
	}()
}

// cancelRemoteReconnect stops any background reconnect loop for host (or all
// hosts when host == "").
func (a *app) cancelRemoteReconnect(host string) {
	if host == "" {
		for h, fn := range a.remoteReconnectCancel {
			a.remoteReconnectSeq[h]++
			fn()
			delete(a.remoteReconnectCancel, h)
			delete(a.remoteReconnecting, h)
		}
		return
	}
	if a.remoteReconnectSeq != nil {
		a.remoteReconnectSeq[host]++
	}
	if fn, ok := a.remoteReconnectCancel[host]; ok {
		fn()
		delete(a.remoteReconnectCancel, host)
	}
	delete(a.remoteReconnecting, host)
}
