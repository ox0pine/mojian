package sysstat

import (
	"fmt"
	"strconv"
	"strings"
)

// Options controls which hardware metrics a Sampler collects.
type Options struct {
	CPU    bool `json:"cpu,omitempty"`
	Memory bool `json:"memory,omitempty"`
	GPU    bool `json:"gpu,omitempty"`
}

// Snapshot holds one sampled hardware reading from the local or remote host.
type Snapshot struct {
	CPUPercent    float64 `json:"cpuPercent,omitempty"`
	HasCPU        bool    `json:"hasCPU,omitempty"`
	MemUsedBytes  uint64  `json:"memUsedBytes,omitempty"`
	MemTotalBytes uint64  `json:"memTotalBytes,omitempty"`
	MemPercent    float64 `json:"memPercent,omitempty"`
	HasMem        bool    `json:"hasMem,omitempty"`
	GPUPercent    float64 `json:"gpuPercent,omitempty"`
	HasGPU        bool    `json:"hasGPU,omitempty"`
	GPUVRAMUsed   uint64  `json:"gpuVramUsed,omitempty"`
	GPUVRAMTotal  uint64  `json:"gpuVramTotal,omitempty"`
	HasVRAM       bool    `json:"hasVram,omitempty"`
	GPUName       string  `json:"gpuName,omitempty"`
	Timestamp     int64   `json:"timestamp,omitempty"`
}

// Capabilities describes what telemetry sources are available on this machine.
type Capabilities struct {
	HasCPU  bool
	HasMem  bool
	HasGPU  bool
	GPUName string
	Detail  string
}

// EmptySnapshot returns a zeroed telemetry snapshot.
func EmptySnapshot() Snapshot {
	return Snapshot{}
}

// DetectCapabilities inspects the local machine for supported telemetry sources.
func DetectCapabilities() Capabilities {
	return detectCapabilities()
}

func clampPct(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 100 {
		return 100
	}
	return v
}

// FormatGiB formats a byte count concisely for status bar display (e.g. "14.2G").
func FormatGiB(b uint64) string {
	const gib = 1024 * 1024 * 1024
	const mib = 1024 * 1024
	if b >= gib {
		return fmt.Sprintf("%.1fG", float64(b)/float64(gib))
	}
	return fmt.Sprintf("%dM", b/mib)
}

// ParseProcStat parses the aggregate "cpu ..." line from /proc/stat into total
// and idle jiffies (idle + iowait).
func ParseProcStat(raw string) (total, idle uint64, ok bool) {
	for _, line := range strings.Split(raw, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 5 || fields[0] != "cpu" {
			continue
		}
		var sum uint64
		var idleJ, iowaitJ uint64
		for i, f := range fields[1:] {
			v, err := strconv.ParseUint(f, 10, 64)
			if err != nil {
				return 0, 0, false
			}
			sum += v
			if i == 3 {
				idleJ = v
			} else if i == 4 {
				iowaitJ = v
			}
		}
		return sum, idleJ + iowaitJ, true
	}
	return 0, 0, false
}

// ParseProcMeminfo parses /proc/meminfo for MemTotal and used memory.
// Used memory prefers MemTotal - MemAvailable when MemAvailable is present,
// falling back to MemTotal - (MemFree + Buffers + Cached + SReclaimable).
func ParseProcMeminfo(raw string) (used, total uint64, pct float64, ok bool) {
	var memTotal, memAvail, memFree, buffers, cached, sreclaim uint64
	var hasTotal, hasAvail bool
	for _, line := range strings.Split(raw, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		key := strings.TrimSuffix(fields[0], ":")
		valKB, err := strconv.ParseUint(fields[1], 10, 64)
		if err != nil {
			continue
		}
		valBytes := valKB * 1024
		switch key {
		case "MemTotal":
			memTotal = valBytes
			hasTotal = true
		case "MemAvailable":
			memAvail = valBytes
			hasAvail = true
		case "MemFree":
			memFree = valBytes
		case "Buffers":
			buffers = valBytes
		case "Cached":
			cached = valBytes
		case "SReclaimable":
			sreclaim = valBytes
		}
	}
	if !hasTotal || memTotal == 0 {
		return 0, 0, 0, false
	}
	avail := memAvail
	if !hasAvail {
		avail = memFree + buffers + cached + sreclaim
	}
	if avail > memTotal {
		avail = memTotal
	}
	used = memTotal - avail
	pct = clampPct(float64(used) * 100 / float64(memTotal))
	return used, memTotal, pct, true
}

// ParseNvidiaSMICSV parses a `nvidia-smi --query-gpu=utilization.gpu,memory.used,memory.total,name --format=csv,noheader,nounits` line.
func ParseNvidiaSMICSV(raw string) (util float64, vramUsed, vramTotal uint64, name string, ok bool) {
	for _, line := range strings.Split(strings.TrimSpace(raw), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.Split(line, ",")
		if len(parts) < 3 {
			continue
		}
		u, err := strconv.ParseFloat(strings.TrimSpace(parts[0]), 64)
		if err != nil {
			continue
		}
		var usedMB, totalMB float64
		if mu, err := strconv.ParseFloat(strings.TrimSpace(parts[1]), 64); err == nil {
			usedMB = mu
		}
		if mt, err := strconv.ParseFloat(strings.TrimSpace(parts[2]), 64); err == nil {
			totalMB = mt
		}
		if len(parts) >= 4 {
			name = strings.TrimSpace(parts[3])
		}
		return clampPct(u), uint64(usedMB * 1024 * 1024), uint64(totalMB * 1024 * 1024), name, true
	}
	return 0, 0, 0, "", false
}

// ParseIORegIOAccelerator parses `ioreg -r -c IOAccelerator -d 1` output for GPU utilization and model name.
func ParseIORegIOAccelerator(raw string) (util float64, name string, ok bool) {
	for _, line := range strings.Split(raw, "\n") {
		trim := strings.TrimSpace(line)
		if name == "" && strings.Contains(trim, `"model"`) {
			if idx := strings.Index(trim, "="); idx >= 0 {
				val := strings.TrimSpace(trim[idx+1:])
				val = strings.Trim(val, `"<> `)
				if val != "" {
					name = val
				}
			}
		}
		if !strings.Contains(trim, "Device Utilization %") && !strings.Contains(trim, "GPU Activity(%)") {
			continue
		}
		for _, key := range []string{`"Device Utilization %"=`, `"GPU Activity(%)"=`} {
			idx := strings.Index(trim, key)
			if idx < 0 {
				continue
			}
			rest := trim[idx+len(key):]
			end := 0
			for end < len(rest) && rest[end] >= '0' && rest[end] <= '9' {
				end++
			}
			if end > 0 {
				v, err := strconv.ParseFloat(rest[:end], 64)
				if err == nil {
					return clampPct(v), name, true
				}
			}
		}
	}
	return 0, name, false
}
