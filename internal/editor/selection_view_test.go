package editor

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

// markedAt is editorAt with the Selection drawn as «text».
func markedAt(t *testing.T, width int, text string, row, col int) Model {
	t.Helper()
	ed := editorAt(t, width, text, row, col)
	marked := lipgloss.NewStyle().Transform(func(s string) string { return "«" + s + "»" })
	ed.FocusedStyle.Selection = marked
	ed.BlurredStyle.Selection = marked
	ed.viewport.Width += 20
	return ed
}

// drawn is the editor's rows as the screen shows them, without trailing padding.
func drawn(ed Model) []string {
	rows := strings.Split(ed.View(), "\n")
	for i := range rows {
		rows[i] = strings.TrimRight(rows[i], " ")
	}
	return rows
}

func wantRows(t *testing.T, ed Model, want ...string) {
	t.Helper()
	got := drawn(ed)
	for i, w := range want {
		if i >= len(got) || got[i] != w {
			t.Errorf("row %d = %q, want %q (all rows: %q)", i, got[i], w, got)
			return
		}
	}
}

func TestASelectionAcrossLinesIsDrawnOnEachLineIncludingTheLineBreak(t *testing.T) {
	ed := markedAt(t, 40, three, 0, 4)
	ed = press(t, ed, "shift+down")
	wantRows(t, ed,
		"┃ one «two »",
		"┃ «thre»e four",
		"┃ five",
	)
}

func TestASelectionOnAWrappedLineIsDrawnOnEachRow(t *testing.T) {
	ed := markedAt(t, 12, "aaaa bbbb cccc dddd", 0, 0)
	ed = press(t, ed, "ctrl+shift+right", "ctrl+shift+right", "ctrl+shift+right")
	wantRows(t, ed,
		"┃ «aaaa bbbb »",
		"┃ «cccc» dddd",
	)
}

func TestSelectingEverythingDrawsLineBreaksAndBlankLines(t *testing.T) {
	ed := markedAt(t, 40, "a\n\nb", 0, 0)
	ed = press(t, ed, "alt+a")
	wantRows(t, ed,
		"┃ «a »",
		"┃ « »",
		"┃ «b»",
	)
}

func TestTheCursorCellAtTheFrontOfASelectionIsStillTheCursor(t *testing.T) {
	ed := markedAt(t, 40, "select", 0, 6)
	ed = press(t, ed, "shift+left", "shift+left")
	wantRows(t, ed, "┃ selec«t»")
}

func TestNothingIsDrawnSelectedWithoutASelection(t *testing.T) {
	ed := markedAt(t, 40, three, 0, 4)
	if view := ed.View(); strings.ContainsAny(view, "«»") {
		t.Errorf("no Selection, but the view draws one: %q", view)
	}
	ed = press(t, ed, "shift+right", "shift+left")
	if view := ed.View(); strings.ContainsAny(view, "«»") {
		t.Errorf("an empty Selection is drawn: %q", view)
	}
}

func TestTheSelectionIsDrawnWhileTheEditorDoesNotHaveTheFocus(t *testing.T) {
	ed := markedAt(t, 40, "select name", 0, 0)
	ed = press(t, ed, "ctrl+shift+right")
	ed.Blur()
	if view := ed.View(); !strings.Contains(view, "«select»") {
		t.Errorf("a blurred editor does not show its Selection: %q", view)
	}
}
