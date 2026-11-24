//go:build windows

package sysstat

import (
	"context"
	"os/exec"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
	"mojian/internal/platform"
)

var (
	kernel32               = windows.NewLazySystemDLL("kernel32.dll")
	procGetSystemTimes     = kernel32.NewProc("GetSystemTimes")
	procGlobalMemoryStatus = kernel32.NewProc("GlobalMemoryStatusEx")

	pdhMod          = windows.NewLazySystemDLL("pdh.dll")
	procPdhOpenQ    = pdhMod.NewProc("PdhOpenQueryW")
	procPdhAddEC    = pdhMod.NewProc("PdhAddEnglishCounterW")
	procPdhCollectQ = pdhMod.NewProc("PdhCollectQueryData")
	procPdhGetFmt   = pdhMod.NewProc("PdhGetFormattedCounterValue")
	procPdhCloseQ   = pdhMod.NewProc("PdhCloseQuery")
)

type memoryStatusEx struct {
	Length               uint32
	MemoryLoad           uint32
	TotalPhys            uint64
	AvailPhys            uint64
	TotalPageFile        uint64
	AvailPageFile        uint64
	TotalVirtual         uint64
	AvailVirtual         uint64
	AvailExtendedVirtual uint64
}

type pdhFmtCounterValue struct {
	CStatus uint32
	_       uint32
	Double  float64
}

const (
	pdhFmtDouble = 0x00000200
)

// Sampler samples Windows CPU, memory and GPU metrics without cgo.
type Sampler struct {
	mu       sync.Mutex
	hasPrev  bool
	prevIdle uint64
	prevKern uint64
	prevUser uint64

	pdhMu       sync.Mutex
	pdhInit     bool
	pdhOK       bool
	pdhQuery    uintptr
	pdhCounter  uintptr
	pdhHasFirst bool
}

// NewSampler returns a stateful hardware sampler.
func NewSampler() *Sampler {
	return &Sampler{}
}

// Sample reads the requested metrics on Windows.
func (s *Sampler) Sample(ctx context.Context, opts Options) Snapshot {
	snap := Snapshot{Timestamp: time.Now().UnixMilli()}
	if opts.CPU {
		if pct, ok := s.sampleCPU(ctx); ok {
			snap.CPUPercent = pct
			snap.HasCPU = true
		}
	}
	if opts.Memory {
		if used, total, pct, ok := sampleWindowsMemory(); ok {
			snap.MemUsedBytes = used
			snap.MemTotalBytes = total
			snap.MemPercent = pct
			snap.HasMem = true
		}
	}
	if opts.GPU {
		if pct, vUsed, vTotal, name, hasVRAM, ok := s.sampleGPU(ctx); ok {
			snap.GPUPercent = pct
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

func filetimeToUint64(ft windows.Filetime) uint64 {
	return (uint64(ft.HighDateTime) << 32) | uint64(ft.LowDateTime)
}

func readWindowsSystemTimes() (idle, kernel, user uint64, ok bool) {
	if procGetSystemTimes.Find() != nil {
		return 0, 0, 0, false
	}
	var ftIdle, ftKern, ftUser windows.Filetime
	r1, _, _ := procGetSystemTimes.Call(
		uintptr(unsafe.Pointer(&ftIdle)),
		uintptr(unsafe.Pointer(&ftKern)),
		uintptr(unsafe.Pointer(&ftUser)),
	)
	if r1 == 0 {
		return 0, 0, 0, false
	}
	return filetimeToUint64(ftIdle), filetimeToUint64(ftKern), filetimeToUint64(ftUser), true
}

func (s *Sampler) sampleCPU(ctx context.Context) (float64, bool) {
	s.mu.Lock()
	hasPrev := s.hasPrev
	pIdle, pKern, pUser := s.prevIdle, s.prevKern, s.prevUser
	s.mu.Unlock()

	if !hasPrev {
		i0, k0, u0, ok := readWindowsSystemTimes()
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
		pIdle, pKern, pUser = i0, k0, u0
	}

	i1, k1, u1, ok := readWindowsSystemTimes()
	if !ok {
		return 0, false
	}

	s.mu.Lock()
	s.prevIdle, s.prevKern, s.prevUser = i1, k1, u1
	s.hasPrev = true
	s.mu.Unlock()

	dIdle := i1 - pIdle
	dKern := k1 - pKern
	dUser := u1 - pUser
	dSys := dKern + dUser
	if dSys == 0 || dIdle > dSys {
		return 0, dSys > 0
	}
	pct := float64(dSys-dIdle) * 100 / float64(dSys)
	return clampPct(pct), true
}

func sampleWindowsMemory() (used, total uint64, pct float64, ok bool) {
	if procGlobalMemoryStatus.Find() != nil {
		return 0, 0, 0, false
	}
	var ms memoryStatusEx
	ms.Length = uint32(unsafe.Sizeof(ms))
	r1, _, _ := procGlobalMemoryStatus.Call(uintptr(unsafe.Pointer(&ms)))
	if r1 == 0 || ms.TotalPhys == 0 {
		return 0, 0, 0, false
	}
	used = ms.TotalPhys - ms.AvailPhys
	pct = clampPct(float64(used) * 100 / float64(ms.TotalPhys))
	return used, ms.TotalPhys, pct, true
}

func pdhSupported() bool {
	return procPdhOpenQ.Find() == nil &&
		procPdhAddEC.Find() == nil &&
		procPdhCollectQ.Find() == nil &&
		procPdhGetFmt.Find() == nil
}

func (s *Sampler) initPDH() bool {
	s.pdhMu.Lock()
	defer s.pdhMu.Unlock()
	if s.pdhInit {
		return s.pdhOK
	}
	s.pdhInit = true
	if !pdhSupported() {
		return false
	}
	var q uintptr
	r1, _, _ := procPdhOpenQ.Call(0, 0, uintptr(unsafe.Pointer(&q)))
	if r1 != 0 || q == 0 {
		return false
	}
	pathPtr, err := syscall.UTF16PtrFromString(`\GPU Engine(*engtype_3D)\Utilization Percentage`)
	if err != nil {
		_, _, _ = procPdhCloseQ.Call(q)
		return false
	}
	var counter uintptr
	r1, _, _ = procPdhAddEC.Call(q, uintptr(unsafe.Pointer(pathPtr)), 0, uintptr(unsafe.Pointer(&counter)))
	if r1 != 0 || counter == 0 {
		_, _, _ = procPdhCloseQ.Call(q)
		return false
	}
	_, _, _ = procPdhCollectQ.Call(q)
	s.pdhQuery = q
	s.pdhCounter = counter
	s.pdhHasFirst = true
	s.pdhOK = true
	return true
}

func (s *Sampler) samplePDHGPU() (float64, bool) {
	if !s.initPDH() {
		return 0, false
	}
	s.pdhMu.Lock()
	defer s.pdhMu.Unlock()
	if !s.pdhOK || s.pdhQuery == 0 {
		return 0, false
	}
	r1, _, _ := procPdhCollectQ.Call(s.pdhQuery)
	if r1 != 0 {
		return 0, false
	}
	var val pdhFmtCounterValue
	r1, _, _ = procPdhGetFmt.Call(s.pdhCounter, pdhFmtDouble, 0, uintptr(unsafe.Pointer(&val)))
	if r1 != 0 || val.CStatus != 0 {
		return 0, false
	}
	return clampPct(val.Double), true
}

func (s *Sampler) sampleGPU(ctx context.Context) (util float64, vramUsed, vramTotal uint64, name string, hasVRAM, ok bool) {
	if path, err := exec.LookPath("nvidia-smi"); err == nil {
		cctx, cancel := context.WithTimeout(ctx, 1500*time.Millisecond)
		defer cancel()
		cmd := exec.CommandContext(cctx, path,
			"--query-gpu=utilization.gpu,memory.used,memory.total,name",
			"--format=csv,noheader,nounits",
		)
		platform.HideConsole(cmd)
		if out, err := cmd.Output(); err == nil {
			if u, mu, mt, n, parsed := ParseNvidiaSMICSV(string(out)); parsed {
				return u, mu, mt, n, mt > 0, true
			}
		}
	}
	if u, ok := s.samplePDHGPU(); ok {
		return u, 0, 0, "WDDM GPU", false, true
	}
	return 0, 0, 0, "", false, false
}

func detectCapabilities() Capabilities {
	caps := Capabilities{
		HasCPU: procGetSystemTimes.Find() == nil,
		HasMem: procGlobalMemoryStatus.Find() == nil,
	}
	if _, err := exec.LookPath("nvidia-smi"); err == nil {
		caps.HasGPU = true
		caps.GPUName = "NVIDIA GPU"
		caps.Detail = "NVIDIA / WDDM GPU telemetry"
		return caps
	}
	if pdhSupported() {
		caps.HasGPU = true
		caps.GPUName = "WDDM GPU"
		caps.Detail = "Windows PDH WDDM GPU telemetry"
		return caps
	}
	caps.Detail = "Windows PDH GPU counter unavailable"
	return caps
}
