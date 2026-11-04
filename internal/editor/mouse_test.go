package editor

import (
	"strings"
	"testing"
)

// promptCols is the prompt's width; the mouse methods take cells from the editor's top left
// corner, prompt included.
const promptCols = 2

func clicked(t *testing.T, width int, text string, x, y int) (row, col int) {
	t.Helper()
	ed := newEditor(width)
	ed.SetValue(text)
	ed.ClickAt(x, y)
	if ed.HasSelection() {
		t.Errorf("a click left a Selection: %q", ed.SelectedText())
	}
	return cursorOf(ed)
}

func TestClickMapsACellToAPositionOnWrappedRows(t *testing.T) {
	const text = "hello world foo"
	for _, tc := range []struct {
		name     string
		x, y     int
		row, col int
	}{
		{"inside the first row", promptCols + 3, 0, 0, 3},
		{"the first cell of the first row", promptCols, 0, 0, 0},
		{"in the prompt is the start of the row", 0, 0, 0, 0},
		{"first cell of the second row", promptCols, 1, 0, 6},
		{"inside the second row", promptCols + 4, 1, 0, 10},
		{"past the text of a wrapped row is before its trailing space", promptCols + 8, 0, 0, 5},
		{"past the end of the last row is the end of the line", promptCols + 9, 1, 0, 15},
		{"far past the end of the last row", 11, 1, 0, 15},
		{"below the text is the end of the text", promptCols + 1, 4, 0, 15},
	} {
		t.Run(tc.name, func(t *testing.T) {
			row, col := clicked(t, 12, text, tc.x, tc.y)
			if row != tc.row || col != tc.col {
				t.Errorf("click at (%d,%d) put the cursor at %d,%d, want %d,%d", tc.x, tc.y, row, col, tc.row, tc.col)
			}
		})
	}
}

func TestClickPicksTheLineUnderThePointer(t *testing.T) {
	for _, tc := range []struct {
		name     string
		x, y     int
		row, col int
	}{
		{"second line, middle", promptCols + 1, 1, 1, 1},
		{"second line, past its end", promptCols + 7, 1, 1, 2},
		{"first line, past its end", promptCols + 7, 0, 0, 2},
		{"below the text goes to the end of the last line", 0, 3, 2, 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			row, col := clicked(t, 12, "ab\ncd\nefg", tc.x, tc.y)
			if row != tc.row || col != tc.col {
				t.Errorf("click at (%d,%d) put the cursor at %d,%d, want %d,%d", tc.x, tc.y, row, col, tc.row, tc.col)
			}
		})
	}
}

func TestClickOnAWideCharacterIsThatCharacter(t *testing.T) {
	for _, tc := range []struct {
		name string
		x    int
		col  int
	}{
		{"first cell of the first", promptCols, 0},
		{"second cell of the first", promptCols + 1, 0},
		{"first cell of the second", promptCols + 2, 1},
		{"second cell of the second", promptCols + 3, 1},
		{"after the wide ones", promptCols + 6, 3},
		{"past the end", promptCols + 9, 6},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, col := clicked(t, 12, "日本語abc", tc.x, 0); col != tc.col {
				t.Errorf("click at x=%d put the cursor at column %d, want %d", tc.x, col, tc.col)
			}
		})
	}
}

func TestClickOnTheCellOfEveryCursorPositionComesBackToIt(t *testing.T) {
	texts := []string{
		"select * from people where name = 'x' and age > 3",
		"日本語の文字 mixed with ascii 文字列 and more words 日本語",
		"averyveryverylongwordwithnospacesatalltoseehowitbreaks and then some",
		"a\n\nb  c   d\n    indented line that is long enough to wrap around",
		"trailing spaces   \nend ",
		"",
	}
	for _, text := range texts {
		for width := 6; width <= 31; width += 5 {
			ed := newEditor(width)
			ed.SetHeight(99)
			ed.SetValue(text)
			lines := strings.Split(text, "\n")
			for row := range lines {
				for col := 0; col <= len([]rune(lines[row])); col++ {
					at := editorAt(t, width, text, row, col)
					at.SetHeight(99)
					li := at.LineInfo()
					y := li.RowOffset
					for i := 0; i < row; i++ {
						y += len(wrap(at.value[i], at.width))
					}
					x := promptCols + li.CharOffset
					at.ClickAt(0, 0)
					at.ClickAt(x, y)
					if r, c := cursorOf(at); r != row || c != col {
						t.Errorf("width %d, %q: the cursor drawn at (%d,%d) is %d,%d, but a click there gives %d,%d", width, text, x, y, row, col, r, c)
					}
				}
			}
		}
	}
}

func TestAClickLeavesNoSelectionAndNoExtraCursors(t *testing.T) {
	ed := editorAt(t, 40, "one two three", 0, 4)
	ed = press(t, ed, "shift+right", "shift+right")
	ed, _ = ed.Update(keyMsg(t, "ctrl+alt+down"))
	ed.ClickAt(promptCols+9, 0)
	wantNoSelection(t, ed, 0, 9)
	if ed.CollapseCursors() {
		t.Error("a click left extra cursors")
	}
}

func TestShiftClickAndDragExtendFromTheCursor(t *testing.T) {
	ed := editorAt(t, 40, "select * from people", 0, 0)
	ed.ClickAt(promptCols+9, 0)
	ed.ExtendTo(promptCols+13, 0)
	wantSelection(t, ed, "from", 0, 13)

	ed.ExtendTo(promptCols+2, 0)
	wantSelection(t, ed, "lect * ", 0, 2)

	ed.ExtendTo(promptCols+9, 0)
	wantNoSelection(t, ed, 0, 9)
}

func TestShiftClickKeepsTheAnchorOfASelectionFromTheKeyboard(t *testing.T) {
	ed := editorAt(t, 40, "select * from people", 0, 2)
	ed = press(t, ed, "shift+right", "shift+right", "shift+right")
	ed.ExtendTo(promptCols+14, 0)
	wantSelection(t, ed, "lect * from ", 0, 14)
}

func TestShiftClickAcrossLines(t *testing.T) {
	ed := editorAt(t, 40, "one\ntwo\nthree", 0, 1)
	ed.ExtendTo(promptCols+2, 2)
	wantSelection(t, ed, "ne\ntwo\nth", 2, 2)
}

func TestDoubleClickSelectsTheWordUnderThePointer(t *testing.T) {
	const text = "select * from people_2 where x=1"
	for _, tc := range []struct {
		name     string
		x        int
		selected string
		end      int
	}{
		{"first letter", promptCols + 14, "people_2", 22},
		{"last letter", promptCols + 21, "people_2", 22},
		{"the underscore is part of a word", promptCols + 20, "people_2", 22},
		{"punctuation is a word of its own", promptCols + 7, "*", 8},
		{"spaces between words", promptCols + 6, " ", 7},
		{"the cell after the end of the line takes the last word", promptCols + 40, "1", 32},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ed := editorAt(t, 60, text, 0, 0)
			ed.SelectWordAt(tc.x, 0)
			wantSelection(t, ed, tc.selected, 0, tc.end)
		})
	}
}

func TestDoubleClickOnAWordThatWraps(t *testing.T) {
	ed := editorAt(t, 12, "ab cdefghijklmnop qr", 0, 0)
	ed.SelectWordAt(promptCols+1, 1)
	wantSelection(t, ed, "cdefghijklmnop", 0, 17)
}

func TestDoubleClickOnAnEmptyLineSelectsNothing(t *testing.T) {
	ed := editorAt(t, 40, "a\n\nb", 0, 0)
	ed.SelectWordAt(promptCols, 1)
	wantNoSelection(t, ed, 1, 0)
}

func TestDoubleClickOnWideCharacters(t *testing.T) {
	ed := editorAt(t, 40, "x 日本語 y", 0, 0)
	ed.SelectWordAt(promptCols+3, 0)
	wantSelection(t, ed, "日本語", 0, 5)
}

func TestTripleClickSelectsTheWholeLineWithoutItsLineBreak(t *testing.T) {
	ed := editorAt(t, 12, "first\nhello world foo\nlast", 0, 0)
	ed.SelectLineAt(promptCols+3, 2)
	wantSelection(t, ed, "hello world foo", 1, 15)

	ed.SelectLineAt(promptCols, 0)
	wantSelection(t, ed, "first", 0, 5)
}

func TestTripleClickOnAnEmptyLineSelectsNothing(t *testing.T) {
	ed := editorAt(t, 40, "a\n\nb", 0, 0)
	ed.SelectLineAt(promptCols, 1)
	wantNoSelection(t, ed, 1, 0)
}

func TestDoubleClickDropsExtraCursors(t *testing.T) {
	ed := editorAt(t, 40, "one two\nthree", 0, 1)
	ed, _ = ed.Update(keyMsg(t, "ctrl+alt+down"))
	ed.SelectWordAt(promptCols+5, 0)
	wantSelection(t, ed, "two", 0, 7)
	if ed.CollapseCursors() {
		t.Error("a double-click left extra cursors")
	}
}

// twentyText is twenty short lines, l00 to l19, in an editor five rows high.
func twentyText() string {
	lines := make([]string, 20)
	for i := range lines {
		lines[i] = "l" + string(rune('0'+i/10)) + string(rune('0'+i%10))
	}
	return strings.Join(lines, "\n")
}

func twenty(t *testing.T) Model {
	t.Helper()
	ed := editorAt(t, 40, twentyText(), 19, 0)
	_ = ed.View()
	return ed
}

func firstDrawnLine(ed Model) string {
	return strings.TrimSpace(strings.SplitN(ed.View(), "\n", 2)[0])
}

func TestTheWheelScrollsTheViewAndLeavesTheCursor(t *testing.T) {
	ed := press(t, twenty(t), "alt+>")
	if got := firstDrawnLine(ed); !strings.HasSuffix(got, "l15") {
		t.Fatalf("first drawn line = %q, want l15", got)
	}
	ed.ScrollView(-3)
	if got := firstDrawnLine(ed); !strings.HasSuffix(got, "l12") {
		t.Errorf("after scrolling up three, first drawn line = %q, want l12", got)
	}
	if r, c := cursorOf(ed); r != 19 || c != 3 {
		t.Errorf("the cursor moved to %d,%d", r, c)
	}
	ed.ClickAt(promptCols, 0)
	if r, _ := cursorOf(ed); r != 12 {
		t.Errorf("a click on the first drawn row went to line %d, want 12", r)
	}
}

func TestTheWheelStopsAtTheEndsOfTheText(t *testing.T) {
	ed := twenty(t)
	ed.ScrollView(-100)
	if got := firstDrawnLine(ed); !strings.HasSuffix(got, "l00") {
		t.Errorf("first drawn line = %q, want l00", got)
	}
	ed.ScrollView(100)
	if got := firstDrawnLine(ed); !strings.HasSuffix(got, "l15") {
		t.Errorf("first drawn line = %q, want l15 (the last page)", got)
	}
}

func TestTheViewStaysScrolledUntilAKeyBringsTheCursorBack(t *testing.T) {
	ed := press(t, twenty(t), "alt+>")
	ed.ScrollView(-10)
	ed, _ = ed.Update(Blink())
	if got := firstDrawnLine(ed); !strings.HasSuffix(got, "l05") {
		t.Errorf("a blink moved the view: first drawn line = %q, want l05", got)
	}
	ed = press(t, ed, "left")
	if got := firstDrawnLine(ed); !strings.HasSuffix(got, "l15") {
		t.Errorf("after a key the first drawn line = %q, want l15 (the cursor in view)", got)
	}
}

func TestDraggingPastTheBottomScrollsAndKeepsExtending(t *testing.T) {
	ed := twenty(t)
	ed.ScrollView(-100)
	ed.ClickAt(promptCols+1, 1)
	ed.DragTo(promptCols+3, 5)
	if got := firstDrawnLine(ed); !strings.HasSuffix(got, "l01") {
		t.Errorf("first drawn line = %q, want l01 (scrolled one row)", got)
	}
	wantSelection(t, ed, "01\nl02\nl03\nl04\nl05", 5, 3)
}

func TestDraggingPastTheTopScrollsUp(t *testing.T) {
	ed := twenty(t)
	ed.ScrollView(-100)
	ed.ScrollView(10)
	ed.ClickAt(promptCols+1, 2)
	ed.DragTo(promptCols+1, -1)
	if got := firstDrawnLine(ed); !strings.HasSuffix(got, "l09") {
		t.Errorf("first drawn line = %q, want l09", got)
	}
	wantSelection(t, ed, "09\nl10\nl11\nl", 9, 1)
}

func TestDraggingFarPastAnEdgeScrollsFasterButNotWithoutLimit(t *testing.T) {
	ed := twenty(t)
	ed.ScrollView(-100)
	ed.ClickAt(promptCols, 0)
	ed.DragTo(promptCols, 5+50)
	if got := firstDrawnLine(ed); !strings.HasSuffix(got, "l03") {
		t.Errorf("first drawn line = %q, want l03 (at most three rows an event)", got)
	}
}

func TestDraggingPastTheBottomOfShortTextSelectsToTheEnd(t *testing.T) {
	ed := editorAt(t, 40, "one\ntwo", 0, 0)
	ed.ClickAt(promptCols, 0)
	ed.DragTo(promptCols, 5+2)
	wantSelection(t, ed, "one\ntwo", 1, 3)
}

func TestAClickEndsARunOfTypingForUndo(t *testing.T) {
	ed := newEditor(40)
	ed = press(t, ed, "type:a", "type:b", "type:c")
	ed.ClickAt(promptCols+1, 0)
	ed = press(t, ed, "type:X")
	ed = press(t, ed, "ctrl+z")
	if got := ed.Value(); got != "abc" {
		t.Errorf("after undo the text is %q, want %q (the X alone undone)", got, "abc")
	}
}

func TestAChangeFromCodeStillBringsTheCursorIntoViewOnTheNextBlink(t *testing.T) {
	ed := newEditor(40)
	ed.SetValue(twentyText())
	_ = ed.View()
	ed, _ = ed.Update(Blink())
	if got := firstDrawnLine(ed); !strings.HasSuffix(got, "l15") {
		t.Errorf("first drawn line = %q, want l15 (the cursor on l19 in view)", got)
	}

	ed.ScrollView(-100)
	ed.SetValue(twentyText())
	_ = ed.View()
	ed, _ = ed.Update(Blink())
	if got := firstDrawnLine(ed); !strings.HasSuffix(got, "l15") {
		t.Errorf("after the wheel and a new text, first drawn line = %q, want l15", got)
	}
}
