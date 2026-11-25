//go:build darwin

package sysstat

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
	"unsafe"

	"github.com/ebitengine/purego"
	"golang.org/x/sys/unix"
)

const (
	machProcessorCPULoadInfo int32  = 2
	machHostCPULoadInfo      int32  = 3
	machHostVMInfo64         int32  = 4
	machHostCPULoadInfoCount uint32 = 4
	kIOMainPortDefault       uint32 = 0
	kCFStringEncodingUTF8    uint32 = 0x08000100
	kCFNumberSInt64Type      int32  = 4
	kCFNumberFloat64Type     int32  = 13
)

type cfRange struct {
	location int64
	length   int64
}

type vmStatistics64 struct {
	FreeCount                           uint32
	ActiveCount                         uint32
	InactiveCount                       uint32
	WireCount                           uint32
	ZeroFillCount                       uint64
	Reactivations                       uint64
	Pageins                             uint64
	Pageouts                            uint64
	Faults                              uint64
	CowFaults                           uint64
	Lookups                             uint64
	Hits                                uint64
	Purges                              uint64
	PurgeableCount                      uint32
	SpeculativeCount                    uint32
	Decompressions                      uint64
	Compressions                        uint64
	Swapins                             uint64
	Swapouts                            uint64
	CompressorPageCount                 uint32
	ThrottledCount                      uint32
	ExternalPageCount                   uint32
	InternalPageCount                   uint32
	TotalUncompressedPagesInCompressor  uint64
}

type darwinAPIs struct {
	initOnce sync.Once
	machOK   bool
	iokitOK  bool

	machHostSelf      func() uint32
	taskSelfTrap      func() uint32
	hostStatistics    func(host uint32, flavor int32, info unsafe.Pointer, count *uint32) int32
	hostStatistics64  func(host uint32, flavor int32, info unsafe.Pointer, count *uint32) int32
	hostProcessorInfo func(host uint32, flavor int32, outCPUCount *uint32, outInfo unsafe.Pointer, outInfoCnt *uint32) int32
	vmDeallocate      func(task uint32, address uintptr, size uintptr) int32

	ioServiceMatching               func(name string) uintptr
	ioServiceGetMatchingServices    func(mainPort uint32, matching uintptr, existing *uint32) int32
	ioIteratorNext                  func(iterator uint32) uint32
	ioRegistryEntryCreateCFProps    func(entry uint32, properties *uintptr, allocator uintptr, options uint32) int32
	ioObjectRelease                 func(object uint32) int32

	cfStringCreateWithCString func(alloc uintptr, cStr string, encoding uint32) uintptr
	cfDictionaryGetValue      func(theDict, key uintptr) uintptr
	cfNumberGetValue          func(number uintptr, theType int32, valuePtr unsafe.Pointer) bool
	cfStringGetCString        func(theString uintptr, buffer *byte, bufferSize int, encoding uint32) bool
	cfDataGetLength           func(theData uintptr) int
	cfDataGetBytes            func(theData uintptr, r cfRange, buffer *byte)
	cfGetTypeID               func(cf uintptr) uintptr
	cfStringGetTypeID         func() uintptr
	cfDataGetTypeID           func() uintptr
	cfNumberGetTypeID         func() uintptr
	cfDictionaryGetTypeID     func() uintptr
	cfRelease                 func(cf uintptr)
}

var darwinLib darwinAPIs

func initDarwinAPIs() *darwinAPIs {
	darwinLib.initOnce.Do(func() {
		if sysLib, err := purego.Dlopen("/usr/lib/libSystem.B.dylib", purego.RTLD_LAZY); err == nil {
			purego.RegisterLibFunc(&darwinLib.machHostSelf, sysLib, "mach_host_self")
			purego.RegisterLibFunc(&darwinLib.taskSelfTrap, sysLib, "task_self_trap")
			purego.RegisterLibFunc(&darwinLib.hostStatistics, sysLib, "host_statistics")
			purego.RegisterLibFunc(&darwinLib.hostStatistics64, sysLib, "host_statistics64")
			purego.RegisterLibFunc(&darwinLib.hostProcessorInfo, sysLib, "host_processor_info")
			purego.RegisterLibFunc(&darwinLib.vmDeallocate, sysLib, "vm_deallocate")
			darwinLib.machOK = true
		}

		ioLib, errIO := purego.Dlopen("/System/Library/Frameworks/IOKit.framework/IOKit", purego.RTLD_LAZY)
		cfLib, errCF := purego.Dlopen("/System/Library/Frameworks/CoreFoundation.framework/CoreFoundation", purego.RTLD_LAZY)
		if errIO == nil && errCF == nil {
			purego.RegisterLibFunc(&darwinLib.ioServiceMatching, ioLib, "IOServiceMatching")
			purego.RegisterLibFunc(&darwinLib.ioServiceGetMatchingServices, ioLib, "IOServiceGetMatchingServices")
			purego.RegisterLibFunc(&darwinLib.ioIteratorNext, ioLib, "IOIteratorNext")
			purego.RegisterLibFunc(&darwinLib.ioRegistryEntryCreateCFProps, ioLib, "IORegistryEntryCreateCFProperties")
			purego.RegisterLibFunc(&darwinLib.ioObjectRelease, ioLib, "IOObjectRelease")

			purego.RegisterLibFunc(&darwinLib.cfStringCreateWithCString, cfLib, "CFStringCreateWithCString")
			purego.RegisterLibFunc(&darwinLib.cfDictionaryGetValue, cfLib, "CFDictionaryGetValue")
			purego.RegisterLibFunc(&darwinLib.cfNumberGetValue, cfLib, "CFNumberGetValue")
			purego.RegisterLibFunc(&darwinLib.cfStringGetCString, cfLib, "CFStringGetCString")
			purego.RegisterLibFunc(&darwinLib.cfDataGetLength, cfLib, "CFDataGetLength")
			purego.RegisterLibFunc(&darwinLib.cfDataGetBytes, cfLib, "CFDataGetBytes")
			purego.RegisterLibFunc(&darwinLib.cfGetTypeID, cfLib, "CFGetTypeID")
			purego.RegisterLibFunc(&darwinLib.cfStringGetTypeID, cfLib, "CFStringGetTypeID")
			purego.RegisterLibFunc(&darwinLib.cfDataGetTypeID, cfLib, "CFDataGetTypeID")
			purego.RegisterLibFunc(&darwinLib.cfNumberGetTypeID, cfLib, "CFNumberGetTypeID")
			purego.RegisterLibFunc(&darwinLib.cfDictionaryGetTypeID, cfLib, "CFDictionaryGetTypeID")
			purego.RegisterLibFunc(&darwinLib.cfRelease, cfLib, "CFRelease")
			darwinLib.iokitOK = true
		}
	})
	return &darwinLib
}

// Sampler samples macOS CPU, memory and GPU metrics without cgo.
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

// Sample reads the requested metrics on macOS.
func (s *Sampler) Sample(ctx context.Context, opts Options) Snapshot {
	snap := Snapshot{Timestamp: time.Now().UnixMilli()}
	if opts.CPU {
		if pct, ok := s.sampleCPU(ctx); ok {
			snap.CPUPercent = pct
			snap.HasCPU = true
		}
	}
	if opts.Memory {
		if used, total, pct, ok := sampleDarwinMemory(); ok {
			snap.MemUsedBytes = used
			snap.MemTotalBytes = total
			snap.MemPercent = pct
			snap.HasMem = true
		}
	}
	if opts.GPU {
		if pct, name, ok := sampleDarwinGPU(ctx); ok {
			snap.GPUPercent = pct
			snap.HasGPU = true
			snap.GPUName = name
		}
	}
	return snap
}

func readMachCPULoad() (total, idle uint64, ok bool) {
	apis := initDarwinAPIs()
	if !apis.machOK {
		return 0, 0, false
	}
	host := apis.machHostSelf()
	if host == 0 {
		return 0, 0, false
	}

	// Prefer host_processor_info over host_statistics/host_statistics64 because
	// XNU rate-limits host_statistics(HOST_CPU_LOAD_INFO) and returns stale ticks.
	var cpuCount uint32
	var info *uint32
	var infoCount uint32
	if kr := apis.hostProcessorInfo(host, machProcessorCPULoadInfo, &cpuCount, unsafe.Pointer(&info), &infoCount); kr == 0 && info != nil {
		if task := apis.taskSelfTrap(); task != 0 && infoCount > 0 {
			defer apis.vmDeallocate(task, uintptr(unsafe.Pointer(info)), uintptr(infoCount)*unsafe.Sizeof(uint32(0)))
		}
		needed := int(cpuCount) * int(machHostCPULoadInfoCount)
		if cpuCount > 0 && int(infoCount) >= needed {
			slice := unsafe.Slice(info, int(infoCount))
			for i := 0; i < needed; i += int(machHostCPULoadInfoCount) {
				user := uint64(slice[i])
				sys := uint64(slice[i+1])
				idleT := uint64(slice[i+2])
				nice := uint64(slice[i+3])
				total += user + sys + idleT + nice
				idle += idleT
			}
			return total, idle, true
		}
	}

	var ticks [4]uint32
	count := machHostCPULoadInfoCount
	if kr := apis.hostStatistics(host, machHostCPULoadInfo, unsafe.Pointer(&ticks[0]), &count); kr != 0 {
		return 0, 0, false
	}
	user := uint64(ticks[0])
	sys := uint64(ticks[1])
	idleT := uint64(ticks[2])
	nice := uint64(ticks[3])
	return user + sys + idleT + nice, idleT, true
}

func waitSamplerDelay(ctx context.Context, d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func (s *Sampler) sampleCPU(ctx context.Context) (float64, bool) {
	s.mu.Lock()
	hasPrev := s.hasPrev
	prevTot, prevIdle := s.prevTot, s.prevIdle
	s.mu.Unlock()

	if !hasPrev {
		t0, i0, ok := readMachCPULoad()
		if !ok {
			return 0, false
		}
		if !waitSamplerDelay(ctx, 80*time.Millisecond) {
			return 0, false
		}
		prevTot, prevIdle = t0, i0
	}

	t1, i1, ok := readMachCPULoad()
	if !ok {
		return 0, false
	}

	if !hasPrev {
		for attempt := 0; attempt < 2 && t1 <= prevTot; attempt++ {
			if !waitSamplerDelay(ctx, 80*time.Millisecond) {
				return 0, false
			}
			t1, i1, ok = readMachCPULoad()
			if !ok {
				return 0, false
			}
		}
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

func sampleDarwinMemory() (used, total uint64, pct float64, ok bool) {
	total, err := unix.SysctlUint64("hw.memsize")
	if err != nil || total == 0 {
		return 0, 0, 0, false
	}
	apis := initDarwinAPIs()
	if !apis.machOK {
		return 0, 0, 0, false
	}
	host := apis.machHostSelf()
	if host == 0 {
		return 0, 0, 0, false
	}
	var vm vmStatistics64
	count := uint32(unsafe.Sizeof(vm) / 4)
	if kr := apis.hostStatistics64(host, machHostVMInfo64, unsafe.Pointer(&vm), &count); kr != 0 {
		return 0, 0, 0, false
	}
	pageSize := uint64(os.Getpagesize())
	if pageSize == 0 {
		pageSize = 4096
	}
	var appPages uint64
	if vm.InternalPageCount > vm.PurgeableCount {
		appPages = uint64(vm.InternalPageCount - vm.PurgeableCount)
	}
	usedPages := appPages + uint64(vm.WireCount) + uint64(vm.CompressorPageCount)
	used = usedPages * pageSize
	if used > total {
		used = total
	}
	pct = clampPct(float64(used) * 100 / float64(total))
	return used, total, pct, true
}

func (a *darwinAPIs) cfStr(s string) uintptr {
	return a.cfStringCreateWithCString(0, s, kCFStringEncodingUTF8)
}

func (a *darwinAPIs) readCFNumber(ref uintptr) (float64, bool) {
	if ref == 0 || a.cfGetTypeID(ref) != a.cfNumberGetTypeID() {
		return 0, false
	}
	var fv float64
	if a.cfNumberGetValue(ref, kCFNumberFloat64Type, unsafe.Pointer(&fv)) {
		return fv, true
	}
	var iv int64
	if a.cfNumberGetValue(ref, kCFNumberSInt64Type, unsafe.Pointer(&iv)) {
		return float64(iv), true
	}
	return 0, false
}

func (a *darwinAPIs) readCFText(ref uintptr) string {
	if ref == 0 {
		return ""
	}
	tid := a.cfGetTypeID(ref)
	switch tid {
	case a.cfStringGetTypeID():
		var buf [256]byte
		if a.cfStringGetCString(ref, &buf[0], len(buf), kCFStringEncodingUTF8) {
			return strings.TrimRight(string(buf[:]), "\x00")
		}
	case a.cfDataGetTypeID():
		n := a.cfDataGetLength(ref)
		if n > 0 {
			var buf [256]byte
			if n > len(buf) {
				n = len(buf)
			}
			a.cfDataGetBytes(ref, cfRange{location: 0, length: int64(n)}, &buf[0])
			return strings.TrimSpace(strings.TrimRight(string(buf[:n]), "\x00"))
		}
	}
	return ""
}

func sampleDarwinGPUIOKit() (util float64, name string, ok bool) {
	apis := initDarwinAPIs()
	if !apis.iokitOK {
		return 0, "", false
	}
	matching := apis.ioServiceMatching("IOAccelerator")
	if matching == 0 {
		return 0, "", false
	}
	var iter uint32
	if kr := apis.ioServiceGetMatchingServices(kIOMainPortDefault, matching, &iter); kr != 0 || iter == 0 {
		return 0, "", false
	}
	defer apis.ioObjectRelease(iter)

	keyPerf := apis.cfStr("PerformanceStatistics")
	keyUtil1 := apis.cfStr("Device Utilization %")
	keyUtil2 := apis.cfStr("GPU Activity(%)")
	keyModel := apis.cfStr("model")
	defer func() {
		if keyPerf != 0 {
			apis.cfRelease(keyPerf)
		}
		if keyUtil1 != 0 {
			apis.cfRelease(keyUtil1)
		}
		if keyUtil2 != 0 {
			apis.cfRelease(keyUtil2)
		}
		if keyModel != 0 {
			apis.cfRelease(keyModel)
		}
	}()

	for {
		svc := apis.ioIteratorNext(iter)
		if svc == 0 {
			break
		}
		var props uintptr
		kr := apis.ioRegistryEntryCreateCFProps(svc, &props, 0, 0)
		apis.ioObjectRelease(svc)
		if kr != 0 || props == 0 {
			continue
		}

		if modelName := apis.readCFText(apis.cfDictionaryGetValue(props, keyModel)); modelName != "" && name == "" {
			name = modelName
		}
		perfRef := apis.cfDictionaryGetValue(props, keyPerf)
		if perfRef != 0 && apis.cfGetTypeID(perfRef) == apis.cfDictionaryGetTypeID() {
			if v, found := apis.readCFNumber(apis.cfDictionaryGetValue(perfRef, keyUtil1)); found {
				apis.cfRelease(props)
				return clampPct(v), name, true
			}
			if v, found := apis.readCFNumber(apis.cfDictionaryGetValue(perfRef, keyUtil2)); found {
				apis.cfRelease(props)
				return clampPct(v), name, true
			}
		}
		apis.cfRelease(props)
	}
	return 0, name, false
}

func sampleDarwinGPU(ctx context.Context) (util float64, name string, ok bool) {
	if u, n, found := sampleDarwinGPUIOKit(); found {
		return u, n, true
	}
	if _, err := exec.LookPath("ioreg"); err == nil {
		cctx, cancel := context.WithTimeout(ctx, 1200*time.Millisecond)
		defer cancel()
		out, err := exec.CommandContext(cctx, "ioreg", "-r", "-c", "IOAccelerator", "-d", "1").Output()
		if err == nil {
			if u, n, parsed := ParseIORegIOAccelerator(string(out)); parsed {
				return u, n, true
			}
		}
	}
	return 0, "", false
}

func detectCapabilities() Capabilities {
	apis := initDarwinAPIs()
	caps := Capabilities{HasCPU: apis.machOK}
	if total, err := unix.SysctlUint64("hw.memsize"); err == nil && total > 0 && apis.machOK {
		caps.HasMem = true
	}
	if _, name, ok := sampleDarwinGPUIOKit(); ok {
		caps.HasGPU = true
		caps.GPUName = name
		caps.Detail = "macOS IOKit IOAccelerator telemetry"
		return caps
	}
	if _, name, ok := sampleDarwinGPU(context.Background()); ok {
		caps.HasGPU = true
		caps.GPUName = name
		caps.Detail = "macOS IOKit / ioreg GPU telemetry"
		return caps
	}
	caps.Detail = "macOS Mach / IOKit telemetry"
	return caps
}
