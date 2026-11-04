package editor

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

// cursorsDrawn is withCursorsDown with the Selection drawn as «text» and every cursor
// cell as ⟦c⟧.
func cursorsDrawn(t *testing.T, width int, text string, row, col, n int) Model {
	t.Helper()
	ed := withCursorsDown(t, width, text, row, col, n)
	marked := lipgloss.NewStyle().Transform(func(s string) string { return "«" + s + "»" })
	ed.FocusedStyle.Selection = marked
	ed.BlurredStyle.Selection = marked
	ed.Cursor.Style = lipgloss.NewStyle().Transform(func(s string) string { return "⟦" + s + "⟧" })
	ed.viewport.Width += 40
	return ed
}

func TestEveryCursorIsDrawn(t *testing.T) {
	ed := cursorsDrawn(t, 40, "one\ntwo\nsix", 0, 1, 3)
	wantRows(t, ed,
		"┃ o⟦n⟧e",
		"┃ t⟦w⟧o",
		"┃ s⟦i⟧x",
	)
}

func TestACursorAtTheEndOfALineIsDrawnOnTheCellAfterTheText(t *testing.T) {
	ed := cursorsDrawn(t, 40, "ab\ncd", 0, 2, 2)
	wantRows(t, ed,
		"┃ ab⟦ ⟧",
		"┃ cd⟦ ⟧",
	)
}

func TestASelectionAtEveryCursorIsDrawn(t *testing.T) {
	ed := cursorsDrawn(t, 40, "one two\nthree four", 0, 0, 2)
	ed = press(t, ed, "ctrl+shift+right")
	wantRows(t, ed,
		"┃ «one»⟦ ⟧two",
		"┃ «three»⟦ ⟧four",
	)
}

func TestCursorsOnWrappedRowsAreDrawnOnTheRightRow(t *testing.T) {
	ed := cursorsDrawn(t, 12, "aaaa bbbb cccc dddd\nxxxxxxxxxxxx", 0, 12, 2)
	rows := drawn(ed)
	if want := "┃ cc⟦c⟧c dddd"; rows[1] != want {
		t.Errorf("row 1 = %q, want %q (all rows: %q)", rows[1], want, rows)
	}
	if !strings.Contains(strings.Join(rows[2:], "\n"), "⟦") {
		t.Errorf("the cursor on the second line is not drawn: %q", rows)
	}
}

func TestOneCursorIsDrawnExactlyAsBefore(t *testing.T) {
	ed := cursorsDrawn(t, 40, "one\ntwo", 0, 1, 2)
	ed.CollapseCursors()
	wantRows(t, ed,
		"┃ one",
		"┃ t⟦w⟧o",
	)
}
