package app

import (
	"math"
	"testing"

	"mojian/internal/remote"
	"mojian/internal/session"
	"mojian/internal/sysstat"

	"github.com/egoist/mygo/ui"
)

func TestSettingsStatusBarFlatRowsAligned(t *testing.T) {
	registerFonts()
	a := newProjectApp(t)
	tt := ui.NewTester(a.settingsWindowView, 900, 1400)

	// Editor keeps only the top-level Status bar toggle under WRITING.
	a.settings.Cat = settingsEditor
	tt.Frame()
	if !tt.HasText("Status bar") {
		t.Fatalf("missing Status bar on Editor page: %q", tt.Texts())
	}
	if tt.HasText("CPU usage") || tt.HasText("System status") {
		t.Fatalf("terminal telemetry settings leaked into Editor page: %q", tt.Texts())
	}

	// Terminal holds flat CPU / Memory / GPU / Refresh interval rows by default.
	a.settings.Cat = settingsTerminal
	tt.Frame()
	for _, want := range []string{"CPU usage", "Memory usage", "GPU usage", "Refresh interval"} {
		if !tt.HasText(want) {
			t.Fatalf("missing %q on Terminal page: %q", want, tt.Texts())
		}
	}
	if tt.HasText("System status") {
		t.Fatalf("removed setting System status still rendered: %q", tt.Texts())
	}

	ref, ok := tt.Find("Notify on a bell toggle")
	if !ok {
		t.Fatalf("missing reference Terminal toggle: %q", tt.Texts())
	}
	refRight := ref.X + ref.W
	for _, label := range []string{"CPU usage toggle", "Memory usage toggle", "GPU usage toggle"} {
		got, ok := tt.Find(label)
		if !ok {
			t.Fatalf("missing switch %q", label)
		}
		right := got.X + got.W
		if got.X < 0 || right > 900 {
			t.Fatalf("%s out of window bounds: %+v", label, got)
		}
		if math.Abs(float64(right-refRight)) > 1 {
			t.Fatalf("%s right edge %.1f misaligned vs reference %.1f", label, right, refRight)
		}
	}
}

func TestResolveSysTargetModesAndHosts(t *testing.T) {
	a := &app{workspace: &workspace{}}
	notePane := &pane{ID: "note-1"}
	localTermPane := &pane{ID: "term-1", Term: &termTab{Spec: termSpec{Server: localServerID}}}
	remoteClient := &session.Client{}
	remoteTermPane := &pane{ID: "term-2", Term: &termTab{Spec: termSpec{Server: "prod"}, Client: remoteClient}}

	// Off when ShowStatusBar or all metrics are disabled.
	if got := a.resolveSysTarget(localTermPane); got.key != "" {
		t.Fatalf("expected empty target when status bar is off, got %+v", got)
	}
	a.ShowStatusBar = true
	if got := a.resolveSysTarget(localTermPane); got.key != "" {
		t.Fatalf("expected empty target when all metrics default to false, got %+v", got)
	}

	// File panes never request telemetry (local or remote workspace).
	a.StatusBarCPU = true
	if got := a.resolveSysTarget(notePane); got.key != "" {
		t.Fatalf("editor pane must not request system telemetry, got %+v", got)
	}
	a.serverAlias = "prod"
	a.setRemoteConn(&remote.Conn{Host: "prod", Client: remoteClient})
	if got := a.resolveSysTarget(notePane); got.key != "" {
		t.Fatalf("remote editor pane must not request system telemetry, got %+v", got)
	}

	// Terminal panes resolve local host or remote mojian-server host.
	if got := a.resolveSysTarget(localTermPane); got.key != "local" || got.remote {
		t.Fatalf("local terminal target = %+v, want local", got)
	}
	if got := a.resolveSysTarget(remoteTermPane); got.key != "remote:prod" || !got.remote || got.client != remoteClient {
		t.Fatalf("remote terminal target = %+v, want remote:prod", got)
	}
}

func TestStatusBarTerminalAndEditorTelemetryRender(t *testing.T) {
	registerFonts()
	a := newProjectApp(t)
	a.ShowStatusBar = true
	a.StatusBarCPU = true
	a.StatusBarMemory = true
	a.StatusBarGPU = true

	// Editor pane stays strictly editor-focused and never renders hardware telemetry.
	a.sysSnapOK = true
	a.sysSnapHost = "local"
	a.sysSnap = sysstat.Snapshot{
		CPUPercent:    42,
		HasCPU:        true,
		MemUsedBytes:  8 * 1024 * 1024 * 1024,
		MemTotalBytes: 16 * 1024 * 1024 * 1024,
		MemPercent:    50,
		HasMem:        true,
		GPUPercent:    25,
		HasGPU:        true,
		GPUName:       "Test GPU",
	}
	tt := ui.NewTester(a.view, 1100, 700)
	tt.Frame()
	if tt.HasText("CPU 42%") || tt.HasText("GPU 25%") {
		t.Fatalf("editor pane status bar must not render CPU/GPU telemetry: %q", tt.Texts())
	}
	if !tt.HasText("Ln 1, Col 1") {
		t.Fatalf("expected editor cursor on status bar: %q", tt.Texts())
	}

	// Terminal pane hides cursor position and shows host badge + hardware telemetry.
	tb := a.tab()
	termPane := newTermPane(&termTab{Spec: termSpec{Server: "prod"}, Client: &session.Client{}, startErr: "disconnected"})
	tb.Root = &node{Pane: termPane}
	tb.Focus = termPane
	a.sysSnapHost = "remote:prod"
	a.sysSnapOK = true
	a.sysSnap = sysstat.Snapshot{
		CPUPercent:    42,
		HasCPU:        true,
		MemUsedBytes:  8 * 1024 * 1024 * 1024,
		MemTotalBytes: 16 * 1024 * 1024 * 1024,
		MemPercent:    50,
		HasMem:        true,
		GPUPercent:    25,
		HasGPU:        true,
		GPUName:       "Test GPU",
	}
	tt.Frame()
	if tt.HasText("Ln 1, Col 1") {
		t.Fatalf("terminal pane status bar should not show Ln/Col: %q", tt.Texts())
	}
	for _, want := range []string{"ssh · prod", "CPU 42%", "MEM 8.0G/16.0G (50%)", "GPU 25%"} {
		if !tt.HasText(want) {
			t.Fatalf("missing terminal status bar item %q: %q", want, tt.Texts())
		}
	}
}

func TestLSPNotFoundHidesStatusBarError(t *testing.T) {
	registerFonts()
	a := newProjectApp(t)
	a.ShowStatusBar = true
	ws := a.workspace
	ws.lsp = newLSPManager()
	ws.lsp.servers["markdown"] = &lspServerState{
		group:    "markdown",
		name:     "marksman",
		notFound: true,
	}

	tt := ui.NewTester(a.view, 1100, 700)
	tt.Frame()
	if tt.HasText("marksman !") {
		t.Fatalf("missing optional LSP server should not show error badge: %q", tt.Texts())
	}

	a.openLSPStatus()
	tt.Frame()
	if !tt.HasText("Not installed") || tt.HasText("Failed") {
		t.Fatalf("expected Not installed status without Failed: %q", tt.Texts())
	}
}
