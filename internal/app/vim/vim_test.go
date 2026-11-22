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
