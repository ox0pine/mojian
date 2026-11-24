// Package prefs is the preference schema Mojian keeps between launches.
// The Settings window edits these fields, and settings.json stores them.
package prefs

import (
	"sort"
	"strconv"
	"strings"
)

// LSPServerOverride customizes or disables a language server in settings.json.
type LSPServerOverride struct {
	Command               []string       `json:"command,omitempty"`
	Disabled              bool           `json:"disabled,omitempty"`
	InitializationOptions map[string]any `json:"initializationOptions,omitempty"`
}

// Prefs is the set of preferences the Settings window edits. The app and
// its settings file embed it, so loading, saving, resetting and the JSON
// keys all come from this one declaration.
type Prefs struct {
	Appearance       int     `json:"appearance"`
	Theme            string  `json:"theme,omitempty"`
	EditorFontFamily string  `json:"editorFontFamily,omitempty"`
	EditorFontSize   float64 `json:"editorFontSize,omitempty"`
	EditorLineHeight float64 `json:"editorLineHeight,omitempty"`
	EditorTabSize    int     `json:"editorTabSize,omitempty"`
	EditorSoftWrap   bool    `json:"editorSoftWrap,omitempty"`
	HideLineNumbers  bool    `json:"hideLineNumbers,omitempty"`
	NoActiveLine     bool    `json:"noActiveLine,omitempty"`
	NoIndentGuides   bool    `json:"noIndentGuides,omitempty"`
	NoAutoClose      bool    `json:"noAutoClose,omitempty"`
	NoScrollPastEnd  bool    `json:"noScrollPastEnd,omitempty"`
	// EditorScrollMargin is how many lines the editor keeps between the
	// caret and the edge of the pane; -1 keeps none, as Vim's scrolloff.
	EditorScrollMargin  int     `json:"editorScrollMargin,omitempty"`
	EditorWhitespace    int     `json:"editorWhitespace,omitempty"`
	EditorWrapGuides    string  `json:"editorWrapGuides,omitempty"`
	EditorGitBlame      bool    `json:"editorGitBlame,omitempty"`
	NoInlayHints       bool `json:"noInlayHints,omitempty"`
	EditorVimMode       bool `json:"editorVimMode,omitempty"`
	EditorFormatOnSave  bool `json:"editorFormatOnSave,omitempty"`
	ShowStatusBar       bool `json:"showStatusBar,omitempty"`
	StatusBarCPU        bool `json:"statusBarCPU,omitempty"`      // default false
	StatusBarMemory     bool `json:"statusBarMemory,omitempty"`   // default false
	StatusBarGPU        bool `json:"statusBarGPU,omitempty"`      // default false
	StatusBarInterval   int  `json:"statusBarInterval,omitempty"` // 2, 5, 10 seconds; default 2
	TerminalFontFamily  string  `json:"terminalFontFamily,omitempty"`
	TerminalFontSize    float64 `json:"terminalFontSize,omitempty"`
	ShellPath           string  `json:"shellPath,omitempty"`
	TerminalCursor      int     `json:"terminalCursor,omitempty"`
	NotifyBell          bool    `json:"notifyBell"`
	NotifyExit          bool    `json:"notifyExit"`
	TerminalLineHeight  float64 `json:"terminalLineHeight,omitempty"`
	TerminalOptionAsAlt bool    `json:"terminalOptionAsAlt,omitempty"`
	TerminalCopySelect  bool    `json:"terminalCopySelect,omitempty"`
	NoCursorBlink       bool    `json:"noCursorBlink,omitempty"`
	TerminalScrollback  int     `json:"terminalScrollback,omitempty"`
	NoRestoreLayout     bool    `json:"noRestoreLayout,omitempty"`
	NoGitAutoRefresh    bool    `json:"noGitAutoRefresh,omitempty"`
	// NoUpdateNotifications keeps the launch check quiet; the About page
	// still checks on demand.
	NoUpdateNotifications bool   `json:"noUpdateNotifications,omitempty"`
	ShellCommand          string `json:"shellCommand,omitempty"`
}

// defaults is the settings of a new install. Default and the effective
// value getters of the app both read it, so a default lives in one place.
var defaults = Prefs{
	Theme:          "petal",
	EditorFontSize: 12.5, EditorLineHeight: 1.55, EditorTabSize: 4,
	EditorScrollMargin: 3,
	StatusBarInterval:  2,
	TerminalFontSize:   12.5, TerminalLineHeight: 1, TerminalScrollback: 10,
	NotifyBell: true, NotifyExit: true,
}

// Default is the settings of a new install.
func Default() Prefs { return defaults }

// clamp keeps a hand-edited number inside the range the Settings window
// can show, and restores def when it is unset.
func clamp(v, lo, hi, def float64) float64 {
	if v <= 0 {
		return def
	}
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// Sanitize keeps a hand-edited settings.json in the ranges the Settings
// window can show.
func (p *Prefs) Sanitize() {
	if p.Appearance < 0 || p.Appearance > 2 {
		p.Appearance = 0
	}
	// Unknown ids are resolved by theme.Resolve, so only an unset one
	// needs a value here.
	if p.Theme == "" {
		p.Theme = "petal"
	}
	p.EditorFontSize = clamp(p.EditorFontSize, 9, 28, defaults.EditorFontSize)
	p.EditorLineHeight = clamp(p.EditorLineHeight, 1.1, 2.2, defaults.EditorLineHeight)
	p.EditorTabSize = int(clamp(float64(p.EditorTabSize), 1, 8, float64(defaults.EditorTabSize)))
	// The settings window offers a fixed set, and 0 would mean MyGo's own
	// default rather than a choice, so only the offered values survive.
	switch p.EditorScrollMargin {
	case -1, 1, 3, 5, 8:
	default:
		p.EditorScrollMargin = defaults.EditorScrollMargin
	}
	p.TerminalFontSize = clamp(p.TerminalFontSize, 9, 28, defaults.TerminalFontSize)
	p.TerminalLineHeight = clamp(p.TerminalLineHeight, 0.8, 2, defaults.TerminalLineHeight)
	if p.TerminalCursor < 0 || p.TerminalCursor > 2 {
		p.TerminalCursor = 0
	}
	p.TerminalScrollback = int(clamp(float64(p.TerminalScrollback), 1, 100, float64(defaults.TerminalScrollback)))
	switch p.ShellCommand {
	case "", "zsh", "bash":
	default:
		p.ShellCommand = ""
	}
	if p.EditorWhitespace < 0 || p.EditorWhitespace > 4 {
		p.EditorWhitespace = 0
	}
	p.EditorWrapGuides = strings.TrimSpace(p.EditorWrapGuides)
	switch p.StatusBarInterval {
	case 2, 5, 10:
	default:
		p.StatusBarInterval = 2
	}
}

// ParseWrapGuides parses comma- or space-separated column numbers in [20, 400],
// returning sorted, deduplicated ruler columns.
func ParseWrapGuides(raw string) []int {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	fields := strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == ';' || r == ' ' || r == '\t'
	})
	seen := map[int]bool{}
	var out []int
	for _, f := range fields {
		n, err := strconv.Atoi(strings.TrimSpace(f))
		if err != nil || n < 20 || n > 400 || seen[n] {
			continue
		}
		seen[n] = true
		out = append(out, n)
	}
	sort.Ints(out)
	return out
}
