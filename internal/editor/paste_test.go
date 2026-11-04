package editor

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestPastedTextIsMadeSafeAndGivenPlainLineEndings(t *testing.T) {
	cases := []struct{ name, in, want string }{
		{"an escape sequence loses its escape", "x\x1b[31my", "x[31my"},
		{"NUL is dropped", "a\x00b", "ab"},
		{"other control characters are dropped", "a\x07\x08b", "ab"},
		{"CRLF is one newline", "a\r\nb", "a\nb"},
		{"CR alone is a newline", "a\rb", "a\nb"},
		{"a blank line stays one", "a\r\n\r\nb", "a\n\nb"},
		{"LF is left alone", "a\nb", "a\nb"},
		{"a tab becomes spaces", "a\tb", "a    b"},
	}
	for _, tc := range cases {
		ed := newEditor(40)
		ed, _ = ed.Update(PasteMsg(tc.in))
		if got := ed.Value(); got != tc.want {
			t.Errorf("%s (PasteMsg): text = %q, want %q", tc.name, got, tc.want)
		}

		ed = newEditor(40)
		ed, _ = ed.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(tc.in), Paste: true})
		if got := ed.Value(); got != tc.want {
			t.Errorf("%s (bracketed paste): text = %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestACRLFPasteAtSeveralCursorsGivesEachOneNewline(t *testing.T) {
	ed := withCursorsDown(t, 40, three, 0, 1, 3)
	ed, _ = ed.Update(PasteMsg("Z\r\nY"))
	if got, want := ed.Value(), "oZ\nYne two\ntZ\nYhree four\nfZ\nYive"; got != want {
		t.Errorf("text = %q, want %q", got, want)
	}
}

func TestPastingOverASelectionReplacesIt(t *testing.T) {
	ed := editorAt(t, 40, "select name", 0, 7)
	ed = press(t, ed, "shift+end")
	ed, _ = ed.Update(PasteMsg("people\r\ntable"))
	if got, want := ed.Value(), "select people\ntable"; got != want {
		t.Errorf("text = %q, want %q", got, want)
	}
}
