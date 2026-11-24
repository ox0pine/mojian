//go:build linux

package sysstat

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Sampler samples Linux CPU, memory and GPU metrics without cgo.
type Sampler struct {
	mu       sync.Mutex
	hasPrev  bool
	prevTot  uint64
	prevIdle uint64
}

// NewSampler returns a stateful hardware sampler.
func NewSampler() *Sampler {
	return &Sampler{}
}

// Sample reads the requested metrics, respecting ctx cancellation.
func (s *Sampler) Sample(ctx context.Context, opts Options) Snapshot {
	snap := Snapshot{Timestamp: time.Now().UnixMilli()}
	if opts.CPU {
		if pct, ok := s.sampleCPU(ctx); ok {
			snap.CPUPercent = pct
			snap.HasCPU = true
		}
	}
	if opts.Memory {
		if raw, err := os.ReadFile("/proc/meminfo"); err == nil {
			if used, total, pct, ok := ParseProcMeminfo(string(raw)); ok {
				snap.MemUsedBytes = used
				snap.MemTotalBytes = total
				snap.MemPercent = pct
				snap.HasMem = true
			}
		}
	}
	if opts.GPU {
		if util, vUsed, vTotal, name, hasVRAM, ok := sampleLinuxGPU(ctx); ok {
			snap.GPUPercent = util
			snap.HasGPU = true
			snap.GPUName = name
			if hasVRAM && vTotal > 0 {
				snap.GPUVRAMUsed = vUsed
				snap.GPUVRAMTotal = vTotal
				snap.HasVRAM = true
			}
		}
	}
	return snap
}

func (s *Sampler) sampleCPU(ctx context.Context) (float64, bool) {
	readStat := func() (uint64, uint64, bool) {
		raw, err := os.ReadFile("/proc/stat")
		if err != nil {
			return 0, 0, false
		}
		return ParseProcStat(string(raw))
	}

	s.mu.Lock()
	hasPrev := s.hasPrev
	prevTot, prevIdle := s.prevTot, s.prevIdle
	s.mu.Unlock()

	if !hasPrev {
		t0, i0, ok := readStat()
		if !ok {
			return 0, false
		}
		timer := time.NewTimer(80 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return 0, false
		case <-timer.C:
		}
		prevTot, prevIdle = t0, i0
	}

	t1, i1, ok := readStat()
	if !ok {
		return 0, false
	}

	s.mu.Lock()
	s.prevTot, s.prevIdle = t1, i1
	s.hasPrev = true
	s.mu.Unlock()

	if t1 <= prevTot {
		return 0, false
	}
	dTot := t1 - prevTot
	var dIdle uint64
	if i1 > prevIdle {
		dIdle = i1 - prevIdle
	}
	if dIdle > dTot {
		dIdle = dTot
	}
	pct := float64(dTot-dIdle) * 100 / float64(dTot)
	return clampPct(pct), true
}

func sampleLinuxGPU(ctx context.Context) (util float64, vramUsed, vramTotal uint64, name string, hasVRAM, ok bool) {
	if matches, _ := filepath.Glob("/sys/class/drm/card*/device/gpu_busy_percent"); len(matches) > 0 {
		for _, p := range matches {
			raw, err := os.ReadFile(p)
			if err != nil {
				continue
			}
			v, err := strconv.ParseFloat(strings.TrimSpace(string(raw)), 64)
			if err != nil {
				continue
			}
			devDir := filepath.Dir(p)
			var used, total uint64
			if ub, err := os.ReadFile(filepath.Join(devDir, "mem_info_vram_used")); err == nil {
				used, _ = strconv.ParseUint(strings.TrimSpace(string(ub)), 10, 64)
			}
			if tb, err := os.ReadFile(filepath.Join(devDir, "mem_info_vram_total")); err == nil {
				total, _ = strconv.ParseUint(strings.TrimSpace(string(tb)), 10, 64)
			}
			return clampPct(v), used, total, "DRM GPU", total > 0, true
		}
	}

	if path, err := exec.LookPath("nvidia-smi"); err == nil {
		cctx, cancel := context.WithTimeout(ctx, 1500*time.Millisecond)
		defer cancel()
		cmd := exec.CommandContext(cctx, path,
			"--query-gpu=utilization.gpu,memory.used,memory.total,name",
			"--format=csv,noheader,nounits",
		)
		out, err := cmd.Output()
		if err == nil {
			if u, mu, mt, n, parsed := ParseNvidiaSMICSV(string(out)); parsed {
				return u, mu, mt, n, mt > 0, true
			}
		}
	}
	return 0, 0, 0, "", false, false
}

func detectCapabilities() Capabilities {
	caps := Capabilities{}
	if _, err := os.Stat("/proc/stat"); err == nil {
		caps.HasCPU = true
	}
	if _, err := os.Stat("/proc/meminfo"); err == nil {
		caps.HasMem = true
	}
	if matches, _ := filepath.Glob("/sys/class/drm/card*/device/gpu_busy_percent"); len(matches) > 0 {
		caps.HasGPU = true
		caps.GPUName = "DRM GPU"
		caps.Detail = "sysfs DRM GPU telemetry"
		return caps
	}
	if _, err := exec.LookPath("nvidia-smi"); err == nil {
		caps.HasGPU = true
		caps.GPUName = "NVIDIA GPU"
		caps.Detail = "nvidia-smi GPU telemetry"
		return caps
	}
	caps.Detail = "No Linux DRM or NVIDIA GPU telemetry source found"
	return caps
}
