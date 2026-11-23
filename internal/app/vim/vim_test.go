package vim

import (
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
)

func typeKeys(v *State, st *editor.State, s string, act Actions) {
	for _, r := range s {
		v.Handle(st, ui.InputEvent{Kind: ui.InputText, Text: string(r)}, act)
	}
}

func pressKey(v *State, st *editor.State, mods ui.Modifiers, key ui.Key, act Actions) {
	v.Handle(st, ui.InputEvent{Kind: ui.InputKeyDown, Mods: mods, Key: key}, act)
}

func TestVimConfigureSuppressesIMEInNormalMode(t *testing.T) {
	v := &State{}
	st := &editor.State{Buffer: editor.NewBuffer("hello\nworld\n")}
	v.Configure(st, false)
	if !st.NoInputMethod || st.CursorShape != editor.CursorBlock || st.LineNumbers != editor.LineNumbersRelative {
		t.Fatalf("normal mode config = NoIME:%v shape:%v ln:%v", st.NoInputMethod, st.CursorShape, st.LineNumbers)
	}
	typeKeys(v, st, "i", Actions{})
	v.Configure(st, false)
	if st.NoInputMethod || st.CursorShape != editor.CursorBar {
		t.Fatalf("insert mode config = NoIME:%v shape:%v", st.NoInputMethod, st.CursorShape)
	}
	pressKey(v, st, 0, ui.KeyEscape, Actions{})
	v.Configure(st, false)
	if !st.NoInputMethod || v.Mode != Normal {
		t.Fatalf("after Escape: mode=%v NoIME=%v", v.Mode, st.NoInputMethod)
	}
}

func TestVimMotionsOperatorsTextObjectsAndEx(t *testing.T) {
	v := &State{}
	st := &editor.State{Buffer: editor.NewBuffer("alpha beta\ngamma \"delta\" epsilon\nzeta\n")}
	saved := false
	act := Actions{Save: func() { saved = true }}

	// dw deletes "alpha "
	typeKeys(v, st, "dw", act)
	if got := st.Buffer.String(); got != "beta\ngamma \"delta\" epsilon\nzeta\n" {
		t.Fatalf("after dw: %q", got)
	}

	// j moves to line 1, f" finds quote, ci" deletes inside quotes and enters Insert.
	typeKeys(v, st, "jf\"lci\"", act)
	if v.Mode != Insert {
		t.Fatalf("expected Insert after ci\", got %v", v.Mode)
	}
	st.Insert("REPLACED")
	pressKey(v, st, 0, ui.KeyEscape, act)
	if got := st.Buffer.String(); got != "beta\ngamma \"REPLACED\" epsilon\nzeta\n" {
		t.Fatalf("after ci\": %q", got)
	}

	// dd deletes current line, p puts it below next line.
	typeKeys(v, st, "ddp", act)
	if got := st.Buffer.String(); got != "beta\nzeta\ngamma \"REPLACED\" epsilon\n" {
		t.Fatalf("after ddp: %q", got)
	}

	// :w triggers Save action.
	typeKeys(v, st, ":w", act)
	pressKey(v, st, 0, ui.KeyEnter, act)
	if !saved {
		t.Fatal("expected :w to invoke Save action")
	}
}

func TestVimDotRepeatClipboardWordSearchAndExCommands(t *testing.T) {
	// 1. Dot repeat for Normal edits (dw, rx) and Insert edits (ci" + typed text).
	v := &State{}
	st := &editor.State{Buffer: editor.NewBuffer("one two three\n\"aaa\" \"bbb\"\n")}
	typeKeys(v, st, "dw.", Actions{})
	if got := st.Buffer.String(); got != "three\n\"aaa\" \"bbb\"\n" {
		t.Fatalf("after dw.: %q", got)
	}

	// Move to second line inside "aaa", run ci", type "XXX" via InputText + Buffer.Edit, Esc, move to "bbb", press '.'
	typeKeys(v, st, "j0lci\"", Actions{})
	if v.Mode != Insert {
		t.Fatalf("expected Insert after ci\", got %v", v.Mode)
	}
	for _, ch := range "XXX" {
		v.Handle(st, ui.InputEvent{Kind: ui.InputText, Text: string(ch)}, Actions{})
		st.Insert(string(ch))
	}
	pressKey(v, st, 0, ui.KeyEscape, Actions{})
	// Cursor is on last 'X' of "XXX": 2f" jumps past closing quote to opening quote of "bbb", l steps inside.
	typeKeys(v, st, "2f\"l.", Actions{})
	if got := st.Buffer.String(); got != "three\n\"XXX\" \"XXX\"\n" {
		t.Fatalf("after ci\" + .: %q", got)
	}

	// 2. System clipboard registers ("+yy and "+p).
	clipboard := ""
	act := Actions{
		ReadClipboard:  func() string { return clipboard },
		WriteClipboard: func(s string) { clipboard = s },
	}
	st2 := &editor.State{Buffer: editor.NewBuffer("first\nsecond\n")}
	v2 := &State{}
	typeKeys(v2, st2, "\"+yy", act)
	if clipboard != "first\n" {
		t.Fatalf("clipboard after \"+yy = %q, want %q", clipboard, "first\n")
	}
	clipboard = "from-system\n"
	typeKeys(v2, st2, "j\"+p", act)
	if got := st2.Buffer.String(); got != "first\nsecond\nfrom-system\n" {
		t.Fatalf("after \"+p: %q", got)
	}

	// 3. * and # whole-word search + n / N.
	st3 := &editor.State{Buffer: editor.NewBuffer("foo foobar foo bar foo")}
	v3 := &State{}
	typeKeys(v3, st3, "*", Actions{})
	if st3.Cursor() != 11 {
		t.Fatalf("after *: cursor = %d, want 11 (skipping foobar)", st3.Cursor())
	}
	typeKeys(v3, st3, "n", Actions{})
	if st3.Cursor() != 19 {
		t.Fatalf("after n: cursor = %d, want 19", st3.Cursor())
	}
	typeKeys(v3, st3, "#", Actions{})
	if st3.Cursor() != 11 {
		t.Fatalf("after #: cursor = %d, want 11", st3.Cursor())
	}
	typeKeys(v3, st3, "N", Actions{})
	if st3.Cursor() != 19 {
		t.Fatalf("after N: cursor = %d, want 19", st3.Cursor())
	}

	// 4. Ex commands (:sp, :vs, :e, :tabnew, :tabc, :<line>).
	var splitDown, splitRight, newTab, closeTab int
	var openedFile string
	exAct := Actions{
		SplitDown:  func() { splitDown++ },
		SplitRight: func() { splitRight++ },
		NewTab:     func() { newTab++ },
		CloseTab:   func() { closeTab++ },
		OpenFile:   func(name string) { openedFile = name },
	}
	st4 := &editor.State{Buffer: editor.NewBuffer("l1\nl2\nl3\nl4\n")}
	v4 := &State{}
	runEx := func(cmd string) {
		typeKeys(v4, st4, ":"+cmd, exAct)
		pressKey(v4, st4, 0, ui.KeyEnter, exAct)
	}
	runEx("sp")
	runEx("vs main.go")
	runEx("tabnew")
	runEx("tabc")
	runEx("3")
	if splitDown != 1 || splitRight != 1 || newTab != 1 || closeTab != 1 || openedFile != "main.go" {
		t.Fatalf("ex actions = sp:%d vs:%d tabnew:%d tabc:%d open:%q", splitDown, splitRight, newTab, closeTab, openedFile)
	}
	if st4.CursorPos().Line != 2 {
		t.Fatalf("after :3 line = %d, want 2", st4.CursorPos().Line)
	}
}
