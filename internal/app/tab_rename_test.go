package app

import (
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestTabRenameWhitespaceRestoresAutomaticName(t *testing.T) {
	registerFonts()
	a := newProjectApp(t)
	tb := a.tab()
	tb.Name = "UniqueTab"
	tt := ui.NewTester(a.view, 1100, 700)
	a.startRename(tb)
	tt.Frame()
	tt.Frame()
	a.renameText = " \t "
	tt.Frame()
	tt.Key(0, ui.KeyEnter)
	tt.Frame()
	if tb.Name != "" {
		t.Fatalf("blank rename left a whitespace-only name: %q", tb.Name)
	}
	if a.rename != nil {
		t.Fatal("submitting the blank name left rename mode active")
	}
}

func TestTabRenameAlignment(t *testing.T) {
	registerFonts()
	a := newProjectApp(t)
	tb := a.tab()
	tb.Name = "UniqueTab"
	tt := ui.NewTester(a.view, 1100, 700)
	tt.Frame()
	label, ok := tt.Find("UniqueTab")
	if !ok {
		t.Fatalf("tab label not found: %q", tt.Texts())
	}
	a.startRename(tb)
	tt.Frame()
	tt.Frame()
	caret, ok := tt.TextCaret()
	if !ok {
		t.Fatalf("rename input is not focused: %q", tt.Texts())
	}
	labelCenter := label.Y + label.H/2
	caretCenter := caret.Y + caret.H/2
	if diff := labelCenter - caretCenter; diff > 1 || diff < -1 {
		t.Fatalf("rename text is %.2f px off the tab label (label %.2f, caret %.2f)", diff, labelCenter, caretCenter)
	}
}
