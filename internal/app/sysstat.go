package app

import (
	"context"
	"fmt"
	"os"
	"runtime"
	"strings"
	"time"

	"mojian/internal/session"
	"mojian/internal/sysstat"
)

type sysTarget struct {
	key       string
	label     string
	remote    bool
	hostAlias string
	client    *session.Client
}

func localHostLabel() string {
	h, err := os.Hostname()
	if err != nil || strings.TrimSpace(h) == "" {
		return "local"
	}
	h = strings.TrimSpace(h)
	h = strings.TrimSuffix(h, ".local")
	return h
}

func (a *app) localGPUCapability() sysstat.Capabilities {
	if !a.sysGPUCapSet {
		a.sysGPUCap = sysstat.DetectCapabilities()
		a.sysGPUCapSet = true
	}
	return a.sysGPUCap
}

func (a *app) gpuCapabilityDetail() string {
	caps := a.localGPUCapability()
	switch {
	case caps.HasGPU && caps.GPUName != "":
		return caps.GPUName
	case !caps.HasGPU && caps.Detail != "":
		return "Unavailable locally: " + caps.Detail
	default:
		return caps.Detail
	}
}

func (a *app) resolveSysTarget(p *pane) sysTarget {
	if !a.ShowStatusBar || runtime.GOOS == "js" {
		return sysTarget{}
	}
	if !a.StatusBarCPU && !a.StatusBarMemory && !a.StatusBarGPU {
		return sysTarget{}
	}
	if p == nil || p.Term == nil {
		return sysTarget{}
	}
	if p.Term.Spec.Server != "" && p.Term.Spec.Server != localServerID {
		alias := p.Term.Spec.Server
		client := p.Term.Client
		if client == nil {
			if rc := a.remoteConn(alias); rc != nil && rc.Healthy() {
				client = rc.Client
			}
		}
		if client == nil {
			return sysTarget{}
		}
		if a.sysRemoteUnsupported != nil && a.sysRemoteUnsupported[client] {
			return sysTarget{}
		}
		return sysTarget{
			key:       "remote:" + alias,
			label:     a.hostLabel(alias),
			remote:    true,
			hostAlias: alias,
			client:    client,
		}
	}
	return sysTarget{
		key:   "local",
		label: localHostLabel(),
	}
}

func (a *app) stopSysStats() {
	a.sysSeq++
	if a.sysCancel != nil {
		a.sysCancel()
		a.sysCancel = nil
	}
	a.sysKey = ""
}

func (a *app) syncSysStats(p *pane) {
	target := a.resolveSysTarget(p)
	opts := sysstat.Options{
		CPU:    a.StatusBarCPU,
		Memory: a.StatusBarMemory,
		GPU:    a.StatusBarGPU,
	}
	interval := a.statusBarInterval()
	cfgKey := ""
	if target.key != "" {
		cfgKey = fmt.Sprintf("%s|%p|%d|%v,%v,%v", target.key, target.client, interval/time.Second, opts.CPU, opts.Memory, opts.GPU)
	}
	if cfgKey == a.sysKey {
		return
	}
	a.stopSysStats()
	if target.key != a.sysSnapHost {
		a.sysSnap = sysstat.EmptySnapshot()
		a.sysSnapOK = false
		a.sysSnapHost = target.key
	}
	a.sysKey = cfgKey
	if cfgKey == "" {
		return
	}
	post := a.uiPoster()
	if post == nil {
		return
	}
	if !target.remote && a.sysSampler == nil {
		a.sysSampler = sysstat.NewSampler()
	}
	ctx, cancel := context.WithCancel(context.Background())
	a.sysCancel = cancel
	seq := a.sysSeq
	go a.runSysStatsLoop(ctx, seq, target, opts, interval, a.sysSampler, post)
}

func (a *app) runSysStatsLoop(
	ctx context.Context,
	seq int,
	target sysTarget,
	opts sysstat.Options,
	interval time.Duration,
	sampler *sysstat.Sampler,
	post func(func()),
) {
	sampleOnce := func() bool {
		if target.remote {
			cctx, cancel := context.WithTimeout(ctx, 2*time.Second)
			snap, err := target.client.SysStatsContext(cctx, opts)
			cancel()
			if ctx.Err() != nil {
				return false
			}
			if err != nil {
				if strings.Contains(strings.ToLower(err.Error()), "unknown") {
					post(func() {
						if a.sysRemoteUnsupported == nil {
							a.sysRemoteUnsupported = map[*session.Client]bool{}
						}
						a.sysRemoteUnsupported[target.client] = true
						if seq == a.sysSeq {
							a.stopSysStats()
							a.invalidate()
						}
					})
					return false
				}
				return true
			}
			post(func() {
				if seq != a.sysSeq {
					return
				}
				a.sysSnap = snap
				a.sysSnapOK = true
				a.invalidate()
			})
			return true
		}

		snap := sampler.Sample(ctx, opts)
		if ctx.Err() != nil {
			return false
		}
		post(func() {
			if seq != a.sysSeq {
				return
			}
			a.sysSnap = snap
			a.sysSnapOK = true
			a.invalidate()
		})
		return true
	}

	if !sampleOnce() {
		return
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if !sampleOnce() {
				return
			}
		}
	}
}
