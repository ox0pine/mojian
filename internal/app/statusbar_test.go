package app

import (
	"testing"

	"mojian/internal/remote"
	"mojian/internal/session"
	"mojian/internal/sysstat"

	"github.com/egoist/mygo/ui"
)

func TestSettingsStatusBarCollapsibleChildren(t *testing.T) {
	registerFonts()
	a := newProjectApp(t)
	a.settings.Cat = settingsEditor
	tt := ui.NewTester(a.settingsWindowView, 900, 640)

	// Default ShowStatusBar=false: child options remain folded away.
	tt.Frame()
	if tt.HasText("CPU usage") {
		t.Fatalf("CPU usage should be hidden when ShowStatusBar is false: %q", tt.Texts())
	}

	// Enabling ShowStatusBar reveals detailed telemetry controls underneath.
	a.ShowStatusBar = true
	tt.Frame()
	tt.Frame()
	for _, want := range []string{"System status", "CPU usage", "Memory usage", "GPU usage", "Refresh interval"} {
		if !tt.HasText(want) {
			t.Fatalf("missing child setting %q after enabling Status bar: %q", want, tt.Texts())
		}
	}

	// Manual collapse hides children while keeping Status bar toggle visible.
	a.settings.Collapsed = map[string]bool{"show-status-bar": true}
	tt.Frame()
	tt.Frame()
	if tt.HasText("CPU usage") {
		t.Fatalf("CPU usage should fold away when collapsed: %q", tt.Texts())
	}

	// Searching for a child expands the group even when ShowStatusBar is false.
	a.ShowStatusBar = false
	a.settings.Collapsed = nil
	a.settings.Query = "GPU usage"
	tt.Frame()
	tt.Frame()
	if !tt.HasText("GPU usage") {
		t.Fatalf("searching GPU usage should auto-expand child row: %q", tt.Texts())
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

	// Mode 0 (Terminal panes): only terminal panes trigger telemetry.
	a.StatusBarCPU = true
	a.StatusBarSysMode = 0
	if got := a.resolveSysTarget(notePane); got.key != "" {
		t.Fatalf("mode 0 should not sample editor pane, got %+v", got)
	}
	if got := a.resolveSysTarget(localTermPane); got.key != "local" || got.remote {
		t.Fatalf("local terminal target = %+v, want local", got)
	}
	if got := a.resolveSysTarget(remoteTermPane); got.key != "remote:prod" || !got.remote || got.client != remoteClient {
		t.Fatalf("remote terminal target = %+v, want remote:prod", got)
	}

	// Mode 1 (Always): editor panes sample local or active remote workspace host.
	a.StatusBarSysMode = 1
	if got := a.resolveSysTarget(notePane); got.key != "local" {
		t.Fatalf("mode 1 local editor target = %+v, want local", got)
	}
	a.serverAlias = "prod"
	a.setRemoteConn(&remote.Conn{Host: "prod", Client: remoteClient})
	if got := a.resolveSysTarget(notePane); got.key != "remote:prod" || !got.remote {
		t.Fatalf("mode 1 remote editor target = %+v, want remote:prod", got)
	}
}

func TestStatusBarTerminalAndEditorTelemetryRender(t *testing.T) {
	registerFonts()
	a := newProjectApp(t)
	a.ShowStatusBar = true
	a.StatusBarCPU = true
	a.StatusBarMemory = true
	a.StatusBarGPU = true
	a.sysKey = "local|0x0|2|true,true,true"
	a.sysSnapHost = "local"
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

	// Mode 0 on focused editor pane hides telemetry and shows cursor position.
	a.StatusBarSysMode = 0
	tt := ui.NewTester(a.view, 1100, 700)
	tt.Frame()
	if tt.HasText("CPU 42%") {
		t.Fatalf("mode 0 should not render CPU on editor pane: %q", tt.Texts())
	}
	if !tt.HasText("Ln 1, Col 1") {
		t.Fatalf("expected editor cursor on status bar: %q", tt.Texts())
	}

	// Switching focus to a terminal pane hides cursor position and shows host + telemetry.
	tb := a.tab()
	origNotePane := tb.Focus
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

	// Mode 1 (Always) also renders telemetry on focused editor pane.
	a.StatusBarSysMode = 1
	tb.Root = &node{Pane: origNotePane}
	tb.Focus = origNotePane
	a.serverAlias = ""
	a.sysSnapHost = "local"
	a.sysSnapOK = true
	tt.Frame()
	for _, want := range []string{"Ln 1, Col 1", "CPU 42%", "MEM 8.0G/16.0G (50%)", "GPU 25%"} {
		if !tt.HasText(want) {
			t.Fatalf("mode 1 editor status bar missing %q: %q", want, tt.Texts())
		}
	}
}
