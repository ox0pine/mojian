package settingsui

import (
	"strings"
	"testing"

	"mojian/internal/settings"
)

func TestSettingsRowAvailabilityMatchesPlatform(t *testing.T) {
	tests := []struct {
		name string
		id   string
		goos string
		want bool
	}{
		{name: "option on Darwin", id: "option-alt", goos: "darwin", want: true},
		{name: "option on Windows", id: "option-alt", goos: "windows", want: false},
		{name: "option on Linux", id: "option-alt", goos: "linux", want: false},
		{name: "shell command on Darwin", id: "shell-command", goos: "darwin", want: true},
		{name: "shell command on Linux", id: "shell-command", goos: "linux", want: true},
		{name: "shell command on Windows", id: "shell-command", goos: "windows", want: false},
		{name: "unrestricted row", id: "copy-select", goos: "windows", want: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			row := settings.Row{ID: test.id}
			if got := settingsRowAvailable(row, test.goos); got != test.want {
				t.Fatalf("settingsRowAvailable(%q, %q) = %v, want %v", test.id, test.goos, got, test.want)
			}
		})
	}
}

func TestStatusBarRowsHiddenOnWeb(t *testing.T) {
	for _, id := range []string{"status-bar-cpu", "status-bar-memory", "status-bar-gpu", "status-bar-interval"} {
		row := settings.Row{ID: id}
		for _, goos := range []string{"darwin", "linux", "windows"} {
			if !settingsRowAvailable(row, goos) {
				t.Fatalf("expected %s available on %s", row.ID, goos)
			}
		}
		if settingsRowAvailable(row, "js") {
			t.Fatalf("expected %s hidden on js build", row.ID)
		}
	}
}

func TestSettingsCategoryMetadataMatchesPlatform(t *testing.T) {
	for _, goos := range []string{"darwin", "linux"} {
		if got := settingsCategory(settingsAdvanced, goos); got != settings.Categories[settingsAdvanced] {
			t.Fatalf("Advanced metadata changed on %s: %+v", goos, got)
		}
	}
	if got := settingsCategory(settingsTerminal, "darwin"); got != settings.Categories[settingsTerminal] {
		t.Fatalf("Terminal metadata changed on Darwin: %+v", got)
	}

	advanced := settingsCategory(settingsAdvanced, "windows")
	if advanced.Detail != "Data, settings file and reset" {
		t.Fatalf("Windows Advanced detail: %q", advanced.Detail)
	}
	assertSearchWordsAbsent(t, advanced.Words, "shell", "command", "zsh", "bash", "alias")
	for _, word := range []string{"data", "settings", "file", "reset"} {
		if !hasSearchWord(advanced.Words, word) {
			t.Errorf("Windows Advanced lost search word %q: %q", word, advanced.Words)
		}
	}
	for _, goos := range []string{"windows", "linux"} {
		terminal := settingsCategory(settingsTerminal, goos)
		assertSearchWordsAbsent(t, terminal.Words, "option", "alt")
	}
}

func assertSearchWordsAbsent(t *testing.T, words string, unwanted ...string) {
	t.Helper()
	for _, word := range unwanted {
		if hasSearchWord(words, word) {
			t.Errorf("search metadata %q still contains %q", words, word)
		}
	}
}

func hasSearchWord(words, want string) bool {
	for _, word := range strings.Fields(words) {
		if word == want {
			return true
		}
	}
	return false
}

func TestInstallUpdateActionStates(t *testing.T) {
	tests := []struct {
		name     string
		state    UpdateState
		detail   string
		disabled bool
	}{
		{
			name:     "needs check",
			detail:   "Check for updates before installing.",
			disabled: true,
		},
		{
			name:     "installable",
			state:    UpdateState{PendingVersion: "1.2.3"},
			detail:   "Download and install Mojian 1.2.3.",
			disabled: false,
		},
		{
			name:     "ready to restart",
			state:    UpdateState{PendingVersion: "1.2.3", Ready: true},
			detail:   "Mojian 1.2.3 is installed and ready to restart.",
			disabled: true,
		},
		{
			name:     "checking",
			state:    UpdateState{Checking: true},
			detail:   "Checking for a newer release…",
			disabled: true,
		},
		{
			name:     "installing",
			state:    UpdateState{PendingVersion: "1.2.3", Installing: true},
			detail:   "Installing Mojian 1.2.3…",
			disabled: true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			detail, disabled := installUpdateAction(test.state)
			if detail != test.detail || disabled != test.disabled {
				t.Fatalf("installUpdateAction(%+v) = (%q, %v), want (%q, %v)", test.state, detail, disabled, test.detail, test.disabled)
			}
		})
	}
}
