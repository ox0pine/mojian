package sysstat

import (
	"context"
	"runtime"
	"testing"
)

func TestParseProcStat(t *testing.T) {
	raw := "cpu  1000 100 500 8000 200 0 50 0 0 0\ncpu0 500 50 250 4000 100 0 25 0 0 0\n"
	tot, idle, ok := ParseProcStat(raw)
	if !ok {
		t.Fatal("expected ParseProcStat ok")
	}
	if tot != 9850 {
		t.Fatalf("total = %d, want 9850", tot)
	}
	if idle != 8200 {
		t.Fatalf("idle = %d, want 8200", idle)
	}
	if _, _, ok := ParseProcStat("intr 1 2 3\n"); ok {
		t.Fatal("expected missing cpu line to return false")
	}
}

func TestParseProcMeminfo(t *testing.T) {
	raw := "MemTotal:       16384000 kB\nMemFree:         2048000 kB\nMemAvailable:    8192000 kB\nBuffers:          512000 kB\n"
	used, total, pct, ok := ParseProcMeminfo(raw)
	if !ok {
		t.Fatal("expected ParseProcMeminfo ok")
	}
	if total != 16384000*1024 {
		t.Fatalf("total = %d, want %d", total, 16384000*1024)
	}
	if used != 8192000*1024 {
		t.Fatalf("used = %d, want %d", used, 8192000*1024)
	}
	if pct < 49.9 || pct > 50.1 {
		t.Fatalf("pct = %.2f, want ~50.0", pct)
	}

	// Fallback when MemAvailable is absent.
	rawNoAvail := "MemTotal:       1000 kB\nMemFree:         200 kB\nBuffers:         100 kB\nCached:          150 kB\nSReclaimable:     50 kB\n"
	used2, total2, pct2, ok2 := ParseProcMeminfo(rawNoAvail)
	if !ok2 || total2 != 1000*1024 || used2 != 500*1024 || pct2 < 49.9 || pct2 > 50.1 {
		t.Fatalf("unexpected fallback result: used=%d total=%d pct=%.2f ok=%v", used2, total2, pct2, ok2)
	}
}

func TestParseNvidiaSMICSV(t *testing.T) {
	raw := "37, 2048, 8192, NVIDIA GeForce RTX 3070\n"
	u, mu, mt, name, ok := ParseNvidiaSMICSV(raw)
	if !ok {
		t.Fatal("expected ParseNvidiaSMICSV ok")
	}
	if u != 37 || mu != 2048*1024*1024 || mt != 8192*1024*1024 || name != "NVIDIA GeForce RTX 3070" {
		t.Fatalf("unexpected parsed result: u=%.1f mu=%d mt=%d name=%q", u, mu, mt, name)
	}
}

func TestParseIORegIOAccelerator(t *testing.T) {
	raw := `
  |   "model" = <"Apple M3 Max">
  |   "PerformanceStatistics" = {"Device Utilization %"=23,"Renderer Utilization %"=18}
`
	u, name, ok := ParseIORegIOAccelerator(raw)
	if !ok {
		t.Fatal("expected ParseIORegIOAccelerator ok")
	}
	if u != 23 || name != "Apple M3 Max" {
		t.Fatalf("unexpected ioreg parse: u=%.1f name=%q", u, name)
	}
}

func TestFormatGiB(t *testing.T) {
	if got := FormatGiB(14 * 1024 * 1024 * 1024); got != "14.0G" {
		t.Fatalf("FormatGiB = %q, want 14.0G", got)
	}
	if got := FormatGiB(512 * 1024 * 1024); got != "512M" {
		t.Fatalf("FormatGiB = %q, want 512M", got)
	}
}

func TestSamplerLocal(t *testing.T) {
	s := NewSampler()
	snap := s.Sample(context.Background(), Options{CPU: true, Memory: true, GPU: true})
	if runtime.GOOS == "darwin" || runtime.GOOS == "linux" || runtime.GOOS == "windows" {
		if !snap.HasCPU {
			t.Fatalf("expected HasCPU on %s", runtime.GOOS)
		}
		if !snap.HasMem || snap.MemTotalBytes == 0 {
			t.Fatalf("expected HasMem with non-zero total on %s, got %+v", runtime.GOOS, snap)
		}
	}
}

func TestSamplerConsecutiveCPU(t *testing.T) {
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" && runtime.GOOS != "windows" {
		return
	}
	for i := range 8 {
		snap := NewSampler().Sample(context.Background(), Options{CPU: true})
		if !snap.HasCPU {
			t.Fatalf("iteration %d: expected HasCPU on %s", i, runtime.GOOS)
		}
	}
}
