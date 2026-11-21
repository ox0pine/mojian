package commands

import (
	"runtime"
	"strconv"
	"strings"

	"github.com/egoist/mygo/ui"
)

// ShowTabAccel is the accelerator of the nth tab, from 1 to 9: Command
// and the digit on a Mac, Option elsewhere, as Ghostty binds them.
func ShowTabAccel(n int) string {
	if runtime.GOOS == "darwin" {
		return "CmdOrCtrl+" + strconv.Itoa(n)
	}
	return "Alt+" + strconv.Itoa(n)
}

// ShowTabKeys writes the tab digits for the platform, as Settings shows
// them: ⌘1…⌘9 on a Mac, Alt+1…Alt+9 elsewhere.
func ShowTabKeys() string {
	return Display(ShowTabAccel(1)) + "…" + Display(ShowTabAccel(9))
}

// Display writes an accelerator in the keys of the platform, as command
// keys show them: ⌥⇧⌘→ on a Mac, Ctrl+Alt+Shift+Right elsewhere.
func Display(accel string) string {
	if accel == "" {
		return "None"
	}
	parts := strings.Split(accel, "+")
	if runtime.GOOS != "darwin" {
		out := make([]string, 0, len(parts))
		for _, p := range parts {
			switch p {
			case "CmdOrCtrl":
				out = append(out, "Ctrl")
			default:
				out = append(out, p)
			}
		}
		return strings.Join(out, "+")
	}
	var ctrl, alt, shift, cmd bool
	key := ""
	for _, p := range parts {
		switch p {
		case "CmdOrCtrl", "Super":
			cmd = true
		case "Ctrl":
			ctrl = true
		case "Alt":
			alt = true
		case "Shift":
			shift = true
		default:
			key = shortcutGlyph(p)
		}
	}
	var b strings.Builder
	if ctrl {
		b.WriteString("⌃")
	}
	if alt {
		b.WriteString("⌥")
	}
	if shift {
		b.WriteString("⇧")
	}
	if cmd {
		b.WriteString("⌘")
	}
	b.WriteString(key)
	return b.String()
}

func shortcutGlyph(key string) string {
	switch key {
	case "Left":
		return "←"
	case "Right":
		return "→"
	case "Up":
		return "↑"
	case "Down":
		return "↓"
	case "Enter":
		return "↩"
	case "Escape":
		return "⎋"
	case "Backspace":
		return "⌫"
	case "Delete":
		return "⌦"
	case "Tab":
		return "⇥"
	case "Space":
		return "Space"
	}
	return key
}

// ParseAccel reads an accelerator into the modifiers and key a browser
// build binds, where there is no menu bar to take it.
func ParseAccel(accel string) (ui.Modifiers, ui.Key, bool) {
	var mods ui.Modifiers
	key := ui.KeyUnknown
	for _, p := range strings.Split(accel, "+") {
		switch p {
		case "CmdOrCtrl":
			if runtime.GOOS == "darwin" {
				mods |= ui.Super
			} else {
				mods |= ui.Ctrl
			}
		case "Super":
			mods |= ui.Super
		case "Ctrl":
			mods |= ui.Ctrl
		case "Alt":
			mods |= ui.Alt
		case "Shift":
			mods |= ui.Shift
		default:
			k, ok := KeyByName(p)
			if !ok {
				return 0, 0, false
			}
			key = k
		}
	}
	return mods, key, key != ui.KeyUnknown
}

// KeyByName is the key an accelerator's last part names.
func KeyByName(name string) (ui.Key, bool) {
	if k, ok := shortcutKeys[name]; ok {
		return k, true
	}
	return 0, false
}

// shortcutKeys are the keys a shortcut may bind, by accelerator name.
var shortcutKeys = func() map[string]ui.Key {
	m := map[string]ui.Key{
		"Enter": ui.KeyEnter, "Tab": ui.KeyTab, "Space": ui.KeySpace,
		"Backspace": ui.KeyBackspace, "Delete": ui.KeyDelete, "Escape": ui.KeyEscape,
		"Home": ui.KeyHome, "End": ui.KeyEnd, "PageUp": ui.KeyPageUp, "PageDown": ui.KeyPageDown,
		"Left": ui.KeyLeft, "Right": ui.KeyRight, "Up": ui.KeyUp, "Down": ui.KeyDown,
		"-": ui.KeyMinus, "=": ui.KeyEqual, ",": ui.KeyComma, ".": ui.KeyPeriod, "/": ui.KeySlash,
		";": ui.KeySemicolon, "'": ui.KeyQuote, "[": ui.KeyBracketLeft, "]": ui.KeyBracketRight,
		"\\": ui.KeyBackslash, "`": ui.KeyBackquote,
	}
	for i, k := range []ui.Key{ui.KeyF1, ui.KeyF2, ui.KeyF3, ui.KeyF4, ui.KeyF5, ui.KeyF6, ui.KeyF7, ui.KeyF8, ui.KeyF9, ui.KeyF10, ui.KeyF11, ui.KeyF12} {
		m["F"+strconv.Itoa(i+1)] = k
	}
	for i, k := range []ui.Key{ui.Key0, ui.Key1, ui.Key2, ui.Key3, ui.Key4, ui.Key5, ui.Key6, ui.Key7, ui.Key8, ui.Key9} {
		m[strconv.Itoa(i)] = k
	}
	letters := []ui.Key{ui.KeyA, ui.KeyB, ui.KeyC, ui.KeyD, ui.KeyE, ui.KeyF, ui.KeyG, ui.KeyH, ui.KeyI, ui.KeyJ,
		ui.KeyK, ui.KeyL, ui.KeyM, ui.KeyN, ui.KeyO, ui.KeyP, ui.KeyQ, ui.KeyR, ui.KeyS, ui.KeyT,
		ui.KeyU, ui.KeyV, ui.KeyW, ui.KeyX, ui.KeyY, ui.KeyZ}
	for i, k := range letters {
		m[string(rune('A'+i))] = k
	}
	return m
}()

// KeyName is the accelerator name of a key.
func KeyName(key ui.Key) (string, bool) {
	for name, k := range shortcutKeys {
		if k == key {
			return name, true
		}
	}
	return "", false
}

// SameAccel reports whether two accelerators bind the same keys on this
// platform, treating CmdOrCtrl and Ctrl as equivalent outside macOS.
func SameAccel(a, b string) bool {
	if strings.EqualFold(a, b) {
		return true
	}
	if a == "" || b == "" {
		return false
	}
	ma, ka, oka := ParseAccel(a)
	mb, kb, okb := ParseAccel(b)
	return oka && okb && ma == mb && ka == kb
}
