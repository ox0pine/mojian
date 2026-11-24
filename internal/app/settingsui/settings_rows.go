package settingsui

import (
	"math"
	"reflect"
	"runtime"

	"mojian/internal/icons"
	"mojian/internal/motion"
	"mojian/internal/settings"

	"github.com/egoist/mygo/ui"
)

// settingsRowAvailable is the one platform filter used by both the page
// and its search index, so search never reveals a row that is not drawn.
func settingsRowAvailable(row settings.Row, goos string) bool {
	if goos == "js" && row.Parent == "show-status-bar" {
		return false
	}
	switch row.ID {
	case "option-alt":
		return goos == "darwin"
	case "shell-command":
		return shellCommandAvailable(goos)
	default:
		return true
	}
}

func shellCommandAvailable(goos string) bool { return goos != "windows" }

// settingsGenericPage renders a page whose rows are declared in the
// settings table: sections and rows come from the same list the search
// index uses.
func (u *UI) settingsGenericPage(c *ui.Context, k *colors, cat int) {
	section := ""
	var rows []settings.Row
	flush := func() {
		if len(rows) == 0 {
			return
		}
		u.settingsSection(c, k, section, func() {
			for i := 0; i < len(rows); i++ {
				row := rows[i]
				if row.Parent != "" {
					u.settingsRowSpec(c, k, row)
					continue
				}
				var children []settings.Row
				for j := i + 1; j < len(rows) && rows[j].Parent == row.ID; j++ {
					children = append(children, rows[j])
				}
				if len(children) > 0 {
					u.settingsCollapsibleGroup(c, k, row, children)
					i += len(children)
					continue
				}
				u.settingsRowSpec(c, k, row)
			}
		})
	}
	for _, row := range settings.Rows {
		if row.Cat != cat || !settingsRowAvailable(row, runtime.GOOS) {
			continue
		}
		if row.Section != section {
			flush()
			section, rows = row.Section, nil
		}
		rows = append(rows, row)
	}
	flush()
}

func (u *UI) settingsCollapsibleGroup(c *ui.Context, k *colors, parent settings.Row, children []settings.Row) {
	parentOn := u.prefBool(parent.Pref)
	if parent.Invert {
		parentOn = !parentOn
	}
	childHit := false
	for _, ch := range children {
		if u.Hits[ch.Title] {
			childHit = true
			break
		}
	}
	expanded := parentOn && (u.Collapsed == nil || !u.Collapsed[parent.ID])
	if childHit {
		expanded = true
	}

	u.settingRow(c, k, parent.Title, parent.Detail, func() {
		if parentOn {
			iconName := "chevron-right"
			tip := "Expand status bar options"
			if expanded {
				iconName = "chevron-down"
				tip = "Collapse status bar options"
			}
			chev := ui.ButtonBase(c.Key("collapse-"+parent.ID)).Size(24, 24).Radius(6).
				AlignItems(ui.Center).Justify(ui.Center).
				Cursor(ui.CursorPointer).Tooltip(tip).Label(tip)
			if chev.Hovered() {
				chev.Background(k.Hover)
			}
			if chev.Clicked() {
				if u.Collapsed == nil {
					u.Collapsed = map[string]bool{}
				}
				u.Collapsed[parent.ID] = expanded
				u.invalidate()
			}
			chev.Children(func() {
				ui.Icon(c, icons.Icon(iconName)).Size(14, 14).TextColor(k.TextMuted)
			})
		}
		on := parentOn
		sw := ui.Switch(c, &on).Label(parent.Title + " toggle")
		if sw.Changed() {
			val := on
			if parent.Invert {
				val = !val
			}
			u.setPrefBool(parent.Pref, val)
			if on && u.Collapsed != nil {
				delete(u.Collapsed, parent.ID)
			}
			u.saveSettings()
			u.invalidate()
		}
	})

	if !expanded {
		return
	}
	ui.Divider(c).Margin(0, 16)
	box := ui.Column(c.Key("children-"+parent.ID)).FillWidth().
		Margin(4, 16, 10, 28).Padding(2, 0, 2, 12).
		BorderWidth(0, 0, 0, 2).BorderColor(k.PanelBorder).
		Clip().Transition(motion.Tree(c.Preferences().ReduceMotion))
	if !parentOn {
		box.Disabled(true).Opacity(0.55)
	}
	box.Children(func() {
		savedRows := u.Rows
		u.Rows = 0
		for _, ch := range children {
			u.settingsRowSpec(c, k, ch)
		}
		u.Rows = savedRows
	})
}

// settingsRowSpec draws one declared row with the control its Kind names.
func (u *UI) settingsRowSpec(c *ui.Context, k *colors, row settings.Row) {
	detail := row.Detail
	if row.ID == "status-bar-gpu" {
		if extra := u.gpuDetail(); extra != "" {
			detail += " (" + extra + ")"
		}
	}
	switch row.Kind {
	case settings.Toggle:
		u.settingsSwitchRow(c, k, row.Title, detail,
			func() bool {
				v := u.prefBool(row.Pref)
				if row.Invert {
					return !v
				}
				return v
			},
			func(v bool) {
				if row.Invert {
					v = !v
				}
				u.setPrefBool(row.Pref, v)
			})
	case settings.Choice:
		u.settingsChoiceRow(c, k, row.ID, row.Title, detail,
			func() int { return u.prefChoice(row) },
			func(i int) { u.setPrefChoice(row, i) }, row.Options)
	case settings.Slider:
		u.settingsSliderRow(c, k, row.Title, detail,
			func() float64 { return u.prefFloat(row.Pref) },
			func(v float64) { u.setPrefFloat(row.Pref, v) },
			row.Min, row.Max, row.Format)
	case settings.Text:
		ptr := u.prefStringPtr(row.Pref)
		if ptr == nil {
			return
		}
		u.settingRow(c, k, row.Title, row.Detail, func() {
			in := ui.TextInputBase(c, ptr).Width(240).Height(30).Padding(0, 9).
				Radius(8).Background(k.Hover).FontSize(c.Theme().Rem(12.0 / 13)).
				Placeholder(row.Placeholder).Label(row.Title)
			if in.Changed() {
				u.saveSettingsSoon()
				u.invalidate()
			}
		})
	case settings.Font:
		u.fontRow(c, k, row.Title, row.Detail, row.FontWhich, u.prefString(row.Pref))
	case settings.Info:
		u.settingsInfoRow(c, k, row.Title, u.settingsInfoValue(row.ID))
	}
}

// settingsInfoValue is the value a declared Info row shows.
func (u *UI) settingsInfoValue(id string) string {
	switch id {
	case "current-project":
		if path := u.projectPath(); path != "" {
			return path
		}
		return "No project"
	case "server":
		return u.remoteName()
	}
	return ""
}

// prefField reaches the prefs struct by its exported field name, which
// is what a settings.Row's Pref names.
func (u *UI) prefField(name string) reflect.Value {
	v := reflect.ValueOf(u.Prefs).Elem().FieldByName(name)
	if !v.IsValid() {
		panic("settings: unknown preference " + name)
	}
	return v
}

func (u *UI) prefBool(name string) bool { return u.prefField(name).Bool() }
func (u *UI) prefInt(name string) int {
	v := u.prefField(name)
	if v.Kind() == reflect.Int {
		return int(v.Int())
	}
	return 0
}
func (u *UI) prefFloat(name string) float64 {
	v := u.prefField(name)
	if v.Kind() == reflect.Float64 {
		return v.Float()
	}
	return float64(v.Int())
}
func (u *UI) prefString(name string) string { return u.prefField(name).String() }
func (u *UI) prefStringPtr(name string) *string {
	v := u.prefField(name)
	if v.Kind() != reflect.String {
		return nil
	}
	return v.Addr().Interface().(*string)
}
func (u *UI) setPrefBool(name string, value bool) { u.prefField(name).SetBool(value) }
func (u *UI) setPrefInt(name string, value int) {
	v := u.prefField(name)
	if v.Kind() == reflect.Int {
		v.SetInt(int64(value))
	}
}
func (u *UI) setPrefFloat(name string, value float64) {
	v := u.prefField(name)
	if v.Kind() == reflect.Float64 {
		v.SetFloat(value)
		return
	}
	v.SetInt(int64(math.Round(value)))
}

func (u *UI) prefChoice(row settings.Row) int {
	v := u.prefInt(row.Pref)
	if len(row.Values) > 0 {
		for i, want := range row.Values {
			if v == want {
				return i
			}
		}
		return 0
	}
	return v
}

func (u *UI) setPrefChoice(row settings.Row, i int) {
	value := i
	if len(row.Values) > 0 && i >= 0 && i < len(row.Values) {
		value = row.Values[i]
	}
	u.setPrefInt(row.Pref, value)
}
