// Package vim implements an optional modal Vim keybinding layer on top of
// MyGo's editor.State, including automatic IME suppression in Normal/Visual
// modes so non-Latin input methods stay out of the way.
package vim

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
)

// Mode is the current modal editing state.
type Mode uint8

const (
	Normal Mode = iota
	Insert
	Visual
	VisualLine
)

func (m Mode) String() string {
	switch m {
	case Insert:
		return "INSERT"
	case Visual:
		return "VISUAL"
	case VisualLine:
		return "V-LINE"
	default:
		return "NORMAL"
	}
}

// Actions lets ex-commands (:w, :q) notify the hosting pane/window.
type Actions struct {
	Save  func()
	Close func()
}

// State holds per-pane Vim editing state.
type State struct {
	Mode        Mode
	Operator    rune // 'd', 'c', 'y', or 0
	Pending     rune // 'g', 'f', 'F', 't', 'T', 'r', 'i', 'a', or 0
	Count       int
	Register    string
	RegLinewise bool
	LastFindCh  rune
	LastFindDir int
	LastFindTil bool

	CmdActive bool
	CmdBuf    string
}

// Label returns the compact mode indicator shown in the status bar.
func (s *State) Label() string {
	if s == nil {
		return ""
	}
	if s.CmdActive {
		return ":" + s.CmdBuf
	}
	base := s.Mode.String()
	if s.Operator != 0 {
		return base + " " + string(s.Operator)
	}
	if s.Pending != 0 {
		return base + " " + string(s.Pending)
	}
	return base
}

// Configure updates st's cursor shape, IME suppression and line numbering to
// match the current Vim mode.
func (s *State) Configure(st *editor.State, hideLineNumbers bool) {
	if s == nil || st == nil {
		return
	}
	st.NoInputMethod = s.Mode != Insert
	switch {
	case s.Mode == Insert:
		st.CursorShape = editor.CursorBar
	case s.Operator != 0 || s.Pending != 0:
		st.CursorShape = editor.CursorUnderline
	default:
		st.CursorShape = editor.CursorBlock
	}
	if !hideLineNumbers && s.Mode != Insert {
		st.LineNumbers = editor.LineNumbersRelative
	}
}

// Handle intercepts editor input events when Vim mode is active. Returning true
// consumes the event; returning false lets the underlying editor handle it.
func (s *State) Handle(st *editor.State, ev ui.InputEvent, act Actions) bool {
	if s == nil || st == nil {
		return false
	}
	if s.Mode == Insert {
		if ev.Kind == ui.InputKeyDown && (ev.Key == ui.KeyEscape || (ev.Mods == ui.Ctrl && ev.Key == ui.KeyBracketLeft)) {
			s.leaveInsert(st)
			return true
		}
		return false
	}

	// Command-line mode (:w, :q, :wq).
	if s.CmdActive {
		return s.handleCmdLine(st, ev, act)
	}

	if ev.Kind == ui.InputKeyDown {
		// Allow Cmd/Super shortcuts (⌘S, ⌘P, ⌘C, etc.) through to the app/editor.
		if ev.Mods&ui.Super != 0 {
			return false
		}
		switch {
		case ev.Key == ui.KeyEscape || (ev.Mods == ui.Ctrl && ev.Key == ui.KeyBracketLeft):
			s.resetPending()
			if s.Mode != Normal {
				s.Mode = Normal
				cur := st.Cursor()
				st.Select(cur, cur)
			}
			return true
		case ev.Mods == ui.Ctrl && ev.Key == ui.KeyR:
			s.resetPending()
			st.Run(editor.Redo)
			return true
		case ev.Mods == ui.Ctrl && ev.Key == ui.KeyD:
			s.resetPending()
			s.moveVisualOrNormal(st, editor.MovePageDown, editor.SelectPageDown)
			return true
		case ev.Mods == ui.Ctrl && ev.Key == ui.KeyU:
			s.resetPending()
			s.moveVisualOrNormal(st, editor.MovePageUp, editor.SelectPageUp)
			return true
		case ev.Mods == 0 && (ev.Key == ui.KeyLeft || ev.Key == ui.KeyRight || ev.Key == ui.KeyUp || ev.Key == ui.KeyDown):
			// Arrow keys work naturally in Normal/Visual too.
			s.resetPending()
			return false
		case ev.Mods == 0 && (ev.Key == ui.KeyBackspace || ev.Key == ui.KeyDelete || ev.Key == ui.KeyEnter || ev.Key == ui.KeyTab):
			if ev.Key == ui.KeyEnter {
				s.moveVisualOrNormal(st, editor.MoveDown, editor.SelectDown)
			} else if ev.Key == ui.KeyBackspace {
				s.moveVisualOrNormal(st, editor.MoveLeft, editor.SelectLeft)
			}
			s.resetPending()
			return true
		}
		// Let other non-text keydowns fall through to InputText or be ignored if plain letters.
		if ev.Mods&(ui.Ctrl|ui.Alt) != 0 {
			return false
		}
		return false
	}

	if ev.Kind != ui.InputText || ev.Text == "" {
		return false
	}
	for _, r := range ev.Text {
		s.handleRune(st, r)
	}
	return true
}

func (s *State) handleCmdLine(st *editor.State, ev ui.InputEvent, act Actions) bool {
	if ev.Kind == ui.InputKeyDown {
		switch ev.Key {
		case ui.KeyEscape:
			s.CmdActive = false
			s.CmdBuf = ""
			return true
		case ui.KeyBackspace:
			if len(s.CmdBuf) > 0 {
				_, sz := utf8.DecodeLastRuneInString(s.CmdBuf)
				s.CmdBuf = s.CmdBuf[:len(s.CmdBuf)-sz]
			} else {
				s.CmdActive = false
			}
			return true
		case ui.KeyEnter:
			cmd := strings.TrimSpace(s.CmdBuf)
			s.CmdActive = false
			s.CmdBuf = ""
			switch cmd {
			case "w":
				if act.Save != nil {
					act.Save()
				}
			case "q", "q!":
				if act.Close != nil {
					act.Close()
				}
			case "wq", "x":
				if act.Save != nil {
					act.Save()
				}
				if act.Close != nil {
					act.Close()
				}
			}
			return true
		}
		return true
	}
	if ev.Kind == ui.InputText {
		s.CmdBuf += ev.Text
		return true
	}
	return true
}

func (s *State) leaveInsert(st *editor.State) {
	s.Mode = Normal
	s.resetPending()
	buf := st.Buffer
	cur := st.Cursor()
	pos := buf.Pos(cur)
	if pos.Col > 0 {
		st.Run(editor.MoveLeft)
	}
}

func (s *State) resetPending() {
	s.Operator = 0
	s.Pending = 0
	s.Count = 0
}

func (s *State) effectiveCount() int {
	if s.Count <= 0 {
		return 1
	}
	return min(s.Count, 1000)
}

func (s *State) handleRune(st *editor.State, r rune) {
	// Pending single-char arguments: r, f, F, t, T, g, i, a.
	if s.Pending != 0 {
		p := s.Pending
		s.Pending = 0
		switch p {
		case 'r':
			s.replaceChar(st, r)
			s.resetPending()
			return
		case 'f', 'F', 't', 'T':
			dir := 1
			if p == 'F' || p == 'T' {
				dir = -1
			}
			til := p == 't' || p == 'T'
			s.LastFindCh, s.LastFindDir, s.LastFindTil = r, dir, til
			s.applyMotionTo(st, s.findInLine(st, r, dir, til, s.effectiveCount()), dir > 0)
			return
		case 'g':
			if r == 'g' {
				line := max(s.Count-1, 0)
				if s.Operator != 0 {
					s.applyLineRangeOp(st, st.CursorPos().Line, line)
				} else {
					st.GoTo(editor.Pos{Line: line, Col: 0})
				}
			}
			s.resetPending()
			return
		case 'i', 'a':
			if s.Operator != 0 || s.Mode == Visual {
				start, end, ok := textObjectRange(st.Buffer.String(), st.Cursor(), p == 'a', r)
				if ok {
					if s.Mode == Visual {
						st.Select(start, end)
					} else {
						s.applyOpRange(st, start, end, false)
					}
				}
			}
			s.resetPending()
			return
		}
	}

	// Count prefix (1-9, or 0 when count already started).
	if r >= '1' && r <= '9' || (r == '0' && s.Count > 0) {
		s.Count = min(s.Count*10+int(r-'0'), 1000)
		return
	}

	// Doubled operator (dd, cc, yy).
	if s.Operator != 0 && r == s.Operator {
		n := s.effectiveCount()
		curLine := st.CursorPos().Line
		s.applyLineRangeOp(st, curLine, curLine+n-1)
		s.resetPending()
		return
	}

	// Text object prefixes when an operator or visual mode is active.
	if (s.Operator != 0 || s.Mode == Visual) && (r == 'i' || r == 'a') {
		s.Pending = r
		return
	}

	n := s.effectiveCount()
	switch r {
	// Motions
	case 'h':
		s.applyRepeatMotion(st, n, editor.MoveLeft, editor.SelectLeft)
	case 'l':
		s.applyRepeatMotion(st, n, editor.MoveRight, editor.SelectRight)
	case 'j':
		if s.Operator != 0 {
			cur := st.CursorPos().Line
			s.applyLineRangeOp(st, cur, cur+n)
			s.resetPending()
			return
		}
		s.applyRepeatMotion(st, n, editor.MoveDown, editor.SelectDown)
		if s.Mode == VisualLine {
			s.expandVisualLine(st)
		}
	case 'k':
		if s.Operator != 0 {
			cur := st.CursorPos().Line
			s.applyLineRangeOp(st, max(cur-n, 0), cur)
			s.resetPending()
			return
		}
		s.applyRepeatMotion(st, n, editor.MoveUp, editor.SelectUp)
		if s.Mode == VisualLine {
			s.expandVisualLine(st)
		}
	case 'w':
		s.applyMotionTo(st, nextWordStart(st.Buffer.String(), st.Cursor(), n), false)
	case 'b':
		s.applyMotionTo(st, prevWordStart(st.Buffer.String(), st.Cursor(), n), false)
	case 'e':
		s.applyMotionTo(st, nextWordEnd(st.Buffer.String(), st.Cursor(), n), true)
	case '0':
		s.applyMotionTo(st, lineStartOff(st.Buffer.String(), st.Cursor()), false)
	case '^':
		s.applyMotionTo(st, firstNonBlankOff(st.Buffer.String(), st.Cursor()), false)
	case '$':
		s.applyMotionTo(st, lineEndOff(st.Buffer.String(), st.Cursor()), true)
	case 'G':
		target := st.Buffer.LineCount() - 1
		if s.Count > 0 {
			target = min(max(s.Count-1, 0), target)
		}
		if s.Operator != 0 {
			s.applyLineRangeOp(st, st.CursorPos().Line, target)
			s.resetPending()
			return
		}
		st.GoTo(editor.Pos{Line: target, Col: 0})
		s.resetPending()
	case '{':
		s.applyRepeatMotion(st, n, editor.MoveParagraphUp, editor.SelectParagraphUp)
	case '}':
		s.applyRepeatMotion(st, n, editor.MoveParagraphDown, editor.SelectParagraphDown)
	case '%':
		s.moveVisualOrNormal(st, editor.MoveToBracket, editor.SelectToBracket)
		s.resetPending()
	case 'g', 'f', 'F', 't', 'T', 'r':
		s.Pending = r
	case ';':
		if s.LastFindCh != 0 {
			s.applyMotionTo(st, s.findInLine(st, s.LastFindCh, s.LastFindDir, s.LastFindTil, n), s.LastFindDir > 0)
		}
	case ',':
		if s.LastFindCh != 0 {
			s.applyMotionTo(st, s.findInLine(st, s.LastFindCh, -s.LastFindDir, s.LastFindTil, n), -s.LastFindDir > 0)
		}

	// Visual & Insert entry
	case 'v':
		s.resetPending()
		if s.Mode == Visual {
			s.Mode = Normal
			cur := st.Cursor()
			st.Select(cur, cur)
		} else {
			s.Mode = Visual
			cur := st.Cursor()
			st.Select(cur, cur)
		}
	case 'V':
		s.resetPending()
		if s.Mode == VisualLine {
			s.Mode = Normal
			cur := st.Cursor()
			st.Select(cur, cur)
		} else {
			s.Mode = VisualLine
			s.expandVisualLine(st)
		}
	case 'i':
		s.Mode = Insert
		s.resetPending()
	case 'I':
		off := firstNonBlankOff(st.Buffer.String(), st.Cursor())
		st.Select(off, off)
		s.Mode = Insert
		s.resetPending()
	case 'a':
		text := st.Buffer.String()
		cur := st.Cursor()
		if cur < len(text) && text[cur] != '\n' {
			_, sz := utf8.DecodeRuneInString(text[cur:])
			st.Select(cur+sz, cur+sz)
		}
		s.Mode = Insert
		s.resetPending()
	case 'A':
		off := lineEndOff(st.Buffer.String(), st.Cursor())
		st.Select(off, off)
		s.Mode = Insert
		s.resetPending()
	case 'o':
		off := lineEndOff(st.Buffer.String(), st.Cursor())
		st.Buffer.Edit(editor.Edit{Start: off, End: off, Text: "\n"})
		st.Select(off+1, off+1)
		s.Mode = Insert
		s.resetPending()
	case 'O':
		off := lineStartOff(st.Buffer.String(), st.Cursor())
		st.Buffer.Edit(editor.Edit{Start: off, End: off, Text: "\n"})
		st.Select(off, off)
		s.Mode = Insert
		s.resetPending()

	// Operators
	case 'd', 'c', 'y':
		if s.Mode == Visual || s.Mode == VisualLine {
			sel := st.Selection()
			a, b := sel.Anchor, sel.Head
			if a > b {
				a, b = b, a
			}
			linewise := s.Mode == VisualLine
			if !linewise && b < st.Buffer.Len() {
				_, sz := utf8.DecodeRuneInString(st.Buffer.String()[b:])
				if st.Buffer.String()[b] != '\n' {
					b += sz
				}
			}
			s.Operator = r
			s.Mode = Normal
			s.applyOpRange(st, a, b, linewise)
			s.resetPending()
			return
		}
		s.Operator = r
	case 'D':
		s.Operator = 'd'
		s.applyOpRange(st, st.Cursor(), lineEndOff(st.Buffer.String(), st.Cursor()), false)
		s.resetPending()
	case 'C':
		s.Operator = 'c'
		s.applyOpRange(st, st.Cursor(), lineEndOff(st.Buffer.String(), st.Cursor()), false)
		s.resetPending()
	case 'Y':
		s.Operator = 'y'
		cur := st.CursorPos().Line
		s.applyLineRangeOp(st, cur, cur+n-1)
		s.resetPending()
	case 'x':
		text := st.Buffer.String()
		cur := st.Cursor()
		end := cur
		for i := 0; i < n && end < len(text) && text[end] != '\n'; i++ {
			_, sz := utf8.DecodeRuneInString(text[end:])
			end += sz
		}
		if end > cur {
			s.Register = text[cur:end]
			s.RegLinewise = false
			st.Buffer.Edit(editor.Edit{Start: cur, End: end, Text: ""})
		}
		s.resetPending()
	case 'X':
		text := st.Buffer.String()
		cur := st.Cursor()
		start := cur
		lineStart := lineStartOff(text, cur)
		for i := 0; i < n && start > lineStart; i++ {
			_, sz := utf8.DecodeLastRuneInString(text[:start])
			start -= sz
		}
		if start < cur {
			s.Register = text[start:cur]
			s.RegLinewise = false
			st.Buffer.Edit(editor.Edit{Start: start, End: cur, Text: ""})
			st.Select(start, start)
		}
		s.resetPending()
	case 'p':
		s.put(st, true)
		s.resetPending()
	case 'P':
		s.put(st, false)
		s.resetPending()
	case 'u':
		for i := 0; i < n; i++ {
			st.Run(editor.Undo)
		}
		s.resetPending()
	case 'J':
		s.joinLine(st)
		s.resetPending()
	case '/':
		s.resetPending()
		st.Run(editor.Find)
	case 'n':
		s.resetPending()
		st.Run(editor.FindNext)
	case 'N':
		s.resetPending()
		st.Run(editor.FindPrevious)
	case ':':
		s.resetPending()
		s.CmdActive = true
		s.CmdBuf = ""
	default:
		s.resetPending()
	}
}

func (s *State) moveVisualOrNormal(st *editor.State, normalCmd, selectCmd editor.Command) {
	if s.Mode == Visual || s.Mode == VisualLine {
		st.Run(selectCmd)
	} else {
		st.Run(normalCmd)
	}
}

func (s *State) applyRepeatMotion(st *editor.State, n int, normalCmd, selectCmd editor.Command) {
	if s.Operator != 0 {
		start := st.Cursor()
		for i := 0; i < n; i++ {
			st.Run(normalCmd)
		}
		end := st.Cursor()
		s.applyOpRange(st, min(start, end), max(start, end), false)
		s.resetPending()
		return
	}
	for i := 0; i < n; i++ {
		s.moveVisualOrNormal(st, normalCmd, selectCmd)
	}
	s.resetPending()
}

func (s *State) applyMotionTo(st *editor.State, target int, inclusive bool) {
	cur := st.Cursor()
	if s.Operator != 0 {
		a, b := cur, target
		if a > b {
			a, b = b, a
		}
		if inclusive && b < st.Buffer.Len() {
			text := st.Buffer.String()
			if text[b] != '\n' {
				_, sz := utf8.DecodeRuneInString(text[b:])
				b += sz
			}
		}
		s.applyOpRange(st, a, b, false)
		s.resetPending()
		return
	}
	if s.Mode == Visual {
		anchor := st.Selection().Anchor
		st.Select(anchor, target)
	} else if s.Mode == VisualLine {
		anchor := st.Selection().Anchor
		st.Select(anchor, target)
		s.expandVisualLine(st)
	} else {
		st.Select(target, target)
	}
	s.resetPending()
}

func (s *State) applyLineRangeOp(st *editor.State, fromLine, toLine int) {
	if fromLine > toLine {
		fromLine, toLine = toLine, fromLine
	}
	text := st.Buffer.String()
	lineCount := st.Buffer.LineCount()
	fromLine = max(0, min(fromLine, lineCount-1))
	toLine = max(0, min(toLine, lineCount-1))
	start := st.Buffer.Offset(editor.Pos{Line: fromLine, Col: 0})
	var end int
	if toLine+1 < lineCount {
		end = st.Buffer.Offset(editor.Pos{Line: toLine + 1, Col: 0})
	} else {
		end = len(text)
		if start > 0 && s.Operator == 'd' {
			start-- // remove preceding newline when deleting last line
		}
	}
	s.applyOpRange(st, start, end, true)
}

func (s *State) applyOpRange(st *editor.State, start, end int, linewise bool) {
	text := st.Buffer.String()
	start = max(0, min(start, len(text)))
	end = max(start, min(end, len(text)))
	if start == end && s.Operator != 'c' {
		return
	}
	s.Register = text[start:end]
	s.RegLinewise = linewise
	switch s.Operator {
	case 'y':
		st.Select(start, start)
	case 'd':
		st.Buffer.Edit(editor.Edit{Start: start, End: end, Text: ""})
		cur := min(start, st.Buffer.Len())
		st.Select(cur, cur)
	case 'c':
		if linewise && strings.HasSuffix(s.Register, "\n") && end > start {
			end--
		}
		st.Buffer.Edit(editor.Edit{Start: start, End: end, Text: ""})
		st.Select(start, start)
		s.Mode = Insert
	}
}

func (s *State) expandVisualLine(st *editor.State) {
	sel := st.Selection()
	text := st.Buffer.String()
	a, b := sel.Anchor, sel.Head
	if a <= b {
		a = lineStartOff(text, a)
		b = lineEndOff(text, b)
		if b < len(text) && text[b] == '\n' {
			b++
		}
	} else {
		a = lineEndOff(text, a)
		if a < len(text) && text[a] == '\n' {
			a++
		}
		b = lineStartOff(text, b)
	}
	st.Select(a, b)
}

func (s *State) replaceChar(st *editor.State, r rune) {
	text := st.Buffer.String()
	cur := st.Cursor()
	if cur >= len(text) || text[cur] == '\n' {
		return
	}
	_, sz := utf8.DecodeRuneInString(text[cur:])
	st.Buffer.Edit(editor.Edit{Start: cur, End: cur + sz, Text: string(r)})
	st.Select(cur, cur)
}

func (s *State) put(st *editor.State, after bool) {
	if s.Register == "" {
		return
	}
	text := st.Buffer.String()
	cur := st.Cursor()
	if s.RegLinewise {
		reg := s.Register
		if !strings.HasSuffix(reg, "\n") {
			reg += "\n"
		}
		if after {
			eol := lineEndOff(text, cur)
			if eol < len(text) && text[eol] == '\n' {
				ins := eol + 1
				st.Buffer.Edit(editor.Edit{Start: ins, End: ins, Text: reg})
				st.Select(ins, ins)
			} else {
				st.Buffer.Edit(editor.Edit{Start: eol, End: eol, Text: "\n" + strings.TrimSuffix(reg, "\n")})
				st.Select(eol+1, eol+1)
			}
		} else {
			sol := lineStartOff(text, cur)
			st.Buffer.Edit(editor.Edit{Start: sol, End: sol, Text: reg})
			st.Select(sol, sol)
		}
		return
	}
	ins := cur
	if after && cur < len(text) && text[cur] != '\n' {
		_, sz := utf8.DecodeRuneInString(text[cur:])
		ins = cur + sz
	}
	st.Buffer.Edit(editor.Edit{Start: ins, End: ins, Text: s.Register})
	st.Select(ins+len(s.Register)-1, ins+len(s.Register)-1)
}

func (s *State) joinLine(st *editor.State) {
	text := st.Buffer.String()
	eol := lineEndOff(text, st.Cursor())
	if eol >= len(text) || text[eol] != '\n' {
		return
	}
	next := eol + 1
	for next < len(text) && (text[next] == ' ' || text[next] == '\t') {
		next++
	}
	st.Buffer.Edit(editor.Edit{Start: eol, End: next, Text: " "})
	st.Select(eol, eol)
}

func (s *State) findInLine(st *editor.State, target rune, dir int, til bool, count int) int {
	text := st.Buffer.String()
	cur := st.Cursor()
	sol := lineStartOff(text, cur)
	eol := lineEndOff(text, cur)
	found := cur
	if dir > 0 {
		off := cur
		if off < eol {
			_, sz := utf8.DecodeRuneInString(text[off:])
			off += sz
		}
		hits := 0
		for off < eol {
			r, sz := utf8.DecodeRuneInString(text[off:eol])
			if r == target {
				hits++
				if hits == count {
					if til {
						_, psz := utf8.DecodeLastRuneInString(text[sol:off])
						return max(sol, off-psz)
					}
					return off
				}
			}
			off += sz
		}
	} else {
		off := cur
		hits := 0
		for off > sol {
			r, sz := utf8.DecodeLastRuneInString(text[sol:off])
			off -= sz
			if r == target {
				hits++
				if hits == count {
					if til {
						return min(eol, off+sz)
					}
					return off
				}
			}
		}
	}
	return found
}

func lineStartOff(text string, off int) int {
	off = max(0, min(off, len(text)))
	if nl := strings.LastIndexByte(text[:off], '\n'); nl >= 0 {
		return nl + 1
	}
	return 0
}

func lineEndOff(text string, off int) int {
	off = max(0, min(off, len(text)))
	if nl := strings.IndexByte(text[off:], '\n'); nl >= 0 {
		return off + nl
	}
	return len(text)
}

func firstNonBlankOff(text string, off int) int {
	sol := lineStartOff(text, off)
	eol := lineEndOff(text, off)
	i := sol
	for i < eol && (text[i] == ' ' || text[i] == '\t') {
		i++
	}
	return i
}

func charClass(r rune) int {
	switch {
	case unicode.IsSpace(r):
		return 0
	case unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_':
		return 1
	default:
		return 2
	}
}

func nextWordStart(text string, off, count int) int {
	for c := 0; c < count && off < len(text); c++ {
		r, sz := utf8.DecodeRuneInString(text[off:])
		cls := charClass(r)
		off += sz
		for off < len(text) {
			r, sz = utf8.DecodeRuneInString(text[off:])
			if charClass(r) != cls {
				break
			}
			off += sz
		}
		for off < len(text) {
			r, sz = utf8.DecodeRuneInString(text[off:])
			if charClass(r) != 0 {
				break
			}
			off += sz
		}
	}
	return off
}

func prevWordStart(text string, off, count int) int {
	for c := 0; c < count && off > 0; c++ {
		for off > 0 {
			r, sz := utf8.DecodeLastRuneInString(text[:off])
			off -= sz
			if charClass(r) != 0 {
				cls := charClass(r)
				for off > 0 {
					pr, psz := utf8.DecodeLastRuneInString(text[:off])
					if charClass(pr) != cls {
						break
					}
					off -= psz
				}
				break
			}
		}
	}
	return off
}

func nextWordEnd(text string, off, count int) int {
	for c := 0; c < count && off < len(text); c++ {
		if off < len(text) {
			_, sz := utf8.DecodeRuneInString(text[off:])
			off += sz
		}
		for off < len(text) {
			r, sz := utf8.DecodeRuneInString(text[off:])
			if charClass(r) != 0 {
				cls := charClass(r)
				last := off
				off += sz
				for off < len(text) {
					nr, nsz := utf8.DecodeRuneInString(text[off:])
					if charClass(nr) != cls {
						break
					}
					last = off
					off += nsz
				}
				off = last
				break
			}
			off += sz
		}
	}
	return min(off, len(text))
}

func textObjectRange(text string, cur int, around bool, obj rune) (int, int, bool) {
	if cur < 0 || cur >= len(text) {
		return 0, 0, false
	}
	switch obj {
	case 'w':
		r, _ := utf8.DecodeRuneInString(text[cur:])
		cls := charClass(r)
		a, b := cur, cur
		for a > 0 {
			pr, psz := utf8.DecodeLastRuneInString(text[:a])
			if charClass(pr) != cls {
				break
			}
			a -= psz
		}
		for b < len(text) {
			nr, nsz := utf8.DecodeRuneInString(text[b:])
			if charClass(nr) != cls {
				break
			}
			b += nsz
		}
		if around {
			for b < len(text) && (text[b] == ' ' || text[b] == '\t') {
				b++
			}
		}
		return a, b, b > a
	case '"', '\'', '`':
		sol, eol := lineStartOff(text, cur), lineEndOff(text, cur)
		left := strings.LastIndexByte(text[sol:cur+1], byte(obj))
		if left < 0 {
			return 0, 0, false
		}
		left += sol
		right := strings.IndexByte(text[left+1:eol], byte(obj))
		if right < 0 {
			return 0, 0, false
		}
		right += left + 1
		if around {
			return left, right + 1, true
		}
		return left + 1, right, true
	case '(', ')', 'b':
		return pairedRange(text, cur, '(', ')', around)
	case '{', '}', 'B':
		return pairedRange(text, cur, '{', '}', around)
	case '[', ']':
		return pairedRange(text, cur, '[', ']', around)
	}
	return 0, 0, false
}

func pairedRange(text string, cur int, open, close byte, around bool) (int, int, bool) {
	depth := 0
	left := -1
	for i := cur; i >= 0; i-- {
		if text[i] == close && i != cur {
			depth++
		} else if text[i] == open {
			if depth == 0 {
				left = i
				break
			}
			depth--
		}
	}
	if left < 0 {
		return 0, 0, false
	}
	depth = 0
	right := -1
	for i := left + 1; i < len(text); i++ {
		if text[i] == open {
			depth++
		} else if text[i] == close {
			if depth == 0 {
				right = i
				break
			}
			depth--
		}
	}
	if right < 0 {
		return 0, 0, false
	}
	if around {
		return left, right + 1, true
	}
	return left + 1, right, true
}
