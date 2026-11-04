package editor

import (
	"strings"
	"testing"
)

// editorAt is an editor of the given total width holding text, with the cursor at (row,
// col) and no Selection.
func editorAt(t *testing.T, width int, text string, row, col int) Model {
	t.Helper()
	ed := newEditor(width)
	ed.SetValue(text)
	for ed.Line() > row {
		ed.CursorUp()
	}
	ed.SetCursor(col)
	return ed
}

func press(t *testing.T, ed Model, keys ...string) Model {
	t.Helper()
	for _, k := range keys {
		ed, _ = ed.Update(keyMsg(t, k))
	}
	return ed
}

func cursorOf(ed Model) (row, col int) {
	li := ed.LineInfo()
	return ed.Line(), li.StartColumn + li.ColumnOffset
}

func wantSelection(t *testing.T, ed Model, text string, row, col int) {
	t.Helper()
	if !ed.HasSelection() {
		t.Fatalf("no Selection, want %q with the cursor at %d,%d", text, row, col)
	}
	if got := ed.SelectedText(); got != text {
		t.Errorf("selected %q, want %q", got, text)
	}
	if r, c := cursorOf(ed); r != row || c != col {
		t.Errorf("cursor at %d,%d, want %d,%d", r, c, row, col)
	}
}

func wantNoSelection(t *testing.T, ed Model, row, col int) {
	t.Helper()
	if ed.HasSelection() {
		t.Errorf("selected %q, want no Selection", ed.SelectedText())
	}
	if r, c := cursorOf(ed); r != row || c != col {
		t.Errorf("cursor at %d,%d, want %d,%d", r, c, row, col)
	}
}

const three = "one two\nthree four\nfive"

func TestShiftArrowsExtendTheSelectionByCharacter(t *testing.T) {
	ed := editorAt(t, 40, "select name", 0, 0)
	ed = press(t, ed, "shift+right", "shift+right", "shift+right")
	wantSelection(t, ed, "sel", 0, 3)

	ed = editorAt(t, 40, "select", 0, 6)
	ed = press(t, ed, "shift+left", "shift+left")
	wantSelection(t, ed, "ct", 0, 4)
}

func TestShrinkingTheSelectionBackToItsAnchorLeavesNone(t *testing.T) {
	ed := editorAt(t, 40, "select", 0, 2)
	ed = press(t, ed, "shift+right", "shift+right", "shift+left", "shift+left")
	wantNoSelection(t, ed, 0, 2)
}

func TestTheSelectionCanRunAcrossLines(t *testing.T) {
	ed := editorAt(t, 40, three, 0, 4)
	ed = press(t, ed, "shift+down")
	wantSelection(t, ed, "two\nthre", 1, 4)

	ed = press(t, ed, "shift+down")
	wantSelection(t, ed, "two\nthree four\nfive", 2, 4)

	ed = press(t, ed, "shift+up", "shift+up")
	wantNoSelection(t, ed, 0, 4)

	ed = editorAt(t, 40, three, 2, 2)
	ed = press(t, ed, "shift+up")
	wantSelection(t, ed, "ree four\nfi", 1, 2)
}

func TestShiftRightCarriesOnAcrossALineEnd(t *testing.T) {
	ed := editorAt(t, 40, three, 0, 6)
	ed = press(t, ed, "shift+right")
	wantSelection(t, ed, "o", 0, 7)
	ed = press(t, ed, "shift+right")
	wantSelection(t, ed, "o\n", 1, 0)
	ed = press(t, ed, "shift+right")
	wantSelection(t, ed, "o\nt", 1, 1)
}

func TestShiftHomeAndEndSelectToTheEndsOfTheLine(t *testing.T) {
	ed := editorAt(t, 40, three, 1, 5)
	wantSelection(t, press(t, ed, "shift+end"), " four", 1, 10)
	wantSelection(t, press(t, ed, "shift+home"), "three", 1, 0)
}

func TestCtrlShiftArrowsExtendByWord(t *testing.T) {
	ed := editorAt(t, 40, "select name from", 0, 0)
	ed = press(t, ed, "ctrl+shift+right")
	wantSelection(t, ed, "select", 0, 6)
	ed = press(t, ed, "ctrl+shift+right")
	wantSelection(t, ed, "select name", 0, 11)

	ed = editorAt(t, 40, "select name from", 0, 16)
	ed = press(t, ed, "ctrl+shift+left")
	wantSelection(t, ed, "from", 0, 12)
	ed = press(t, ed, "ctrl+shift+left")
	wantSelection(t, ed, "name from", 0, 7)
}

func TestCtrlShiftHomeAndEndReachTheEndsOfTheText(t *testing.T) {
	ed := editorAt(t, 40, three, 1, 3)
	wantSelection(t, press(t, ed, "ctrl+shift+home"), "one two\nthr", 0, 0)
	wantSelection(t, press(t, ed, "ctrl+shift+end"), "ee four\nfive", 2, 4)
}

func TestSelectAll(t *testing.T) {
	ed := editorAt(t, 40, three, 1, 3)
	ed = press(t, ed, "alt+a")
	wantSelection(t, ed, three, 2, 4)
}

func TestSelectAllThenBackspaceEmptiesTheEditor(t *testing.T) {
	ed := editorAt(t, 40, three, 1, 3)
	ed = press(t, ed, "alt+a", "backspace")
	if got := ed.Value(); got != "" {
		t.Errorf("text = %q, want it empty", got)
	}
	wantNoSelection(t, ed, 0, 0)
}

func TestSelectAllOfNothingSelectsNothing(t *testing.T) {
	ed := editorAt(t, 40, "", 0, 0)
	ed = press(t, ed, "alt+a")
	wantNoSelection(t, ed, 0, 0)
}

func TestTypingReplacesTheSelection(t *testing.T) {
	ed := editorAt(t, 40, "select name", 0, 0)
	ed = press(t, ed, "shift+right", "shift+right", "shift+right", "type:X")
	if got := ed.Value(); got != "Xect name" {
		t.Errorf("text = %q, want %q", got, "Xect name")
	}
	wantNoSelection(t, ed, 0, 1)
}

func TestEnterAndPasteReplaceTheSelectionToo(t *testing.T) {
	ed := editorAt(t, 40, "select name", 0, 0)
	ed = press(t, ed, "ctrl+shift+right", "enter")
	if got := ed.Value(); got != "\n name" {
		t.Errorf("after enter text = %q, want %q", got, "\n name")
	}

	ed = editorAt(t, 40, "select name", 0, 7)
	ed = press(t, ed, "shift+end")
	ed, _ = ed.Update(PasteMsg("people"))
	if got := ed.Value(); got != "select people" {
		t.Errorf("after paste text = %q, want %q", got, "select people")
	}
}

func TestEveryDeleteKeyRemovesTheSelectionAndNothingElse(t *testing.T) {
	for _, k := range []string{"backspace", "ctrl+h", "delete", "ctrl+d", "alt+backspace", "ctrl+w", "alt+delete", "alt+d", "ctrl+k", "ctrl+u"} {
		ed := editorAt(t, 40, "select name from", 0, 7)
		ed = press(t, ed, "ctrl+shift+right", k)
		if got := ed.Value(); got != "select  from" {
			t.Errorf("%s: text = %q, want %q", k, got, "select  from")
		}
		wantNoSelection(t, ed, 0, 7)
	}
}

func TestDeletingASelectionAcrossLinesJoinsTheEnds(t *testing.T) {
	ed := editorAt(t, 40, three, 0, 4)
	ed = press(t, ed, "shift+down", "shift+down", "backspace")
	if got := ed.Value(); got != "one " {
		t.Errorf("text = %q, want %q", got, "one ")
	}
	wantNoSelection(t, ed, 0, 4)

	ed = editorAt(t, 40, three, 0, 4)
	ed = press(t, ed, "shift+down", "delete")
	if got := ed.Value(); got != "one e four\nfive" {
		t.Errorf("text = %q, want %q", got, "one e four\nfive")
	}
}

func TestAnUnshiftedLeftOrRightCollapsesTheSelectionToItsEnds(t *testing.T) {
	for _, k := range []string{"left", "ctrl+b"} {
		ed := editorAt(t, 40, "select name", 0, 2)
		ed = press(t, ed, "shift+right", "shift+right", "shift+right", k)
		wantNoSelection(t, ed, 0, 2)
	}
	for _, k := range []string{"right", "ctrl+f"} {
		ed := editorAt(t, 40, "select name", 0, 2)
		ed = press(t, ed, "shift+right", "shift+right", "shift+right", k)
		wantNoSelection(t, ed, 0, 5)
	}
	ed := editorAt(t, 40, "select name", 0, 5)
	wantNoSelection(t, press(t, ed, "shift+left", "shift+left", "shift+left", "left"), 0, 2)
	wantNoSelection(t, press(t, ed, "shift+left", "shift+left", "shift+left", "right"), 0, 5)
}

func TestOtherMovementKeysDropTheSelectionAndMoveFromTheCursor(t *testing.T) {
	cases := []struct {
		key      string
		row, col int
	}{
		{"home", 1, 0}, {"ctrl+a", 1, 0}, {"end", 1, 10}, {"ctrl+e", 1, 10},
		{"up", 0, 3}, {"down", 2, 3}, {"alt+left", 1, 0}, {"alt+right", 1, 5},
		{"ctrl+home", 0, 0}, {"ctrl+end", 2, 4},
	}
	for _, tc := range cases {
		ed := editorAt(t, 40, three, 1, 0)
		ed = press(t, ed, "shift+right", "shift+right", "shift+right", tc.key)
		if ed.HasSelection() {
			t.Errorf("%s left %q selected", tc.key, ed.SelectedText())
		}
		if r, c := cursorOf(ed); r != tc.row || c != tc.col {
			t.Errorf("%s: cursor at %d,%d, want %d,%d", tc.key, r, c, tc.row, tc.col)
		}
	}
}

func TestCtrlLeftAndRightMoveByWordLikeAlt(t *testing.T) {
	ed := editorAt(t, 40, "select name", 0, 0)
	wantNoSelection(t, press(t, ed, "ctrl+right"), 0, 6)
	wantNoSelection(t, press(t, ed, "ctrl+right", "ctrl+right"), 0, 11)
	ed = editorAt(t, 40, "select name", 0, 11)
	wantNoSelection(t, press(t, ed, "ctrl+left"), 0, 7)
	wantNoSelection(t, press(t, ed, "ctrl+left", "ctrl+left"), 0, 0)
}

func TestSelectingByWordFromInsideLeadingSpaceGoesToTheStart(t *testing.T) {
	ed := editorAt(t, 40, "  abc", 0, 2)
	wantSelection(t, press(t, ed, "ctrl+shift+left"), "  ", 0, 0)
}

func TestWrappedRowsAreSelectedRowByRow(t *testing.T) {
	ed := editorAt(t, 12, "aaaa bbbb cccc dddd", 0, 0)
	ed = press(t, ed, "shift+down")
	wantSelection(t, ed, "aaaa bbbb ", 0, 10)
	if li := ed.LineInfo(); li.RowOffset != 1 {
		t.Errorf("cursor on wrapped row %d, want 1", li.RowOffset)
	}
}

func TestWideCharactersAreSelectedWhole(t *testing.T) {
	ed := editorAt(t, 40, "名前 と 住所", 0, 0)
	ed = press(t, ed, "shift+right", "shift+right")
	wantSelection(t, ed, "名前", 0, 2)
	ed = press(t, ed, "ctrl+shift+right")
	wantSelection(t, ed, "名前 と", 0, 4)
}

func TestKeysThatDoNothingLeaveTheSelectionAlone(t *testing.T) {
	ed := editorAt(t, 40, "select name", 0, 0)
	ed = press(t, ed, "shift+right", "shift+right", "tab", "f1", "pgup", "ctrl+v")
	wantSelection(t, ed, "se", 0, 2)
}

func TestTheSelectionSurvivesTheEditorLosingAndRegainingTheFocus(t *testing.T) {
	ed := editorAt(t, 40, "select name", 0, 0)
	ed = press(t, ed, "shift+right", "shift+right")
	ed.Blur()
	ed.Focus()
	wantSelection(t, ed, "se", 0, 2)
}

func TestReplacingTheTextDropsTheSelection(t *testing.T) {
	ed := editorAt(t, 40, "select name", 0, 0)
	ed = press(t, ed, "shift+right", "shift+right")
	ed.SetValue("select name")
	if ed.HasSelection() {
		t.Errorf("SetValue left %q selected", ed.SelectedText())
	}
}

func TestShiftUpOnTheFirstRowSelectsToTheStartAndShiftDownOnTheLastToTheEnd(t *testing.T) {
	ed := editorAt(t, 40, "select name", 0, 4)
	wantSelection(t, press(t, ed, "shift+up"), "sele", 0, 0)

	ed = editorAt(t, 40, three, 0, 4)
	wantSelection(t, press(t, ed, "shift+up"), "one ", 0, 0)

	ed = editorAt(t, 40, three, 2, 1)
	wantSelection(t, press(t, ed, "shift+down"), "ive", 2, 4)

	ed = editorAt(t, 40, three, 1, 2)
	wantSelection(t, press(t, ed, "shift+up"), "e two\nth", 0, 2)
}

func TestEnterAtTheLineLimitDoesNothingEvenWithASelection(t *testing.T) {
	text := strings.Repeat("x\n", 98) + "abc"
	ed := editorAt(t, 40, text, 98, 3)
	ed = press(t, ed, "shift+left")
	ed = press(t, ed, "enter")
	if got := ed.Value(); got != text {
		t.Errorf("enter at the limit changed the text to end %q", got[len(got)-6:])
	}
	wantSelection(t, ed, "c", 98, 2)
}

func TestEveryKeyEitherEndsTheSelectionOrDeliberatelyLeavesIt(t *testing.T) {
	leaves := map[string]bool{
		"tab": true, "esc": true, "pgup": true, "f1": true, "ctrl+z": true, "ctrl+v": true,
		"shift+left": true, "shift+right": true, "shift+up": true, "shift+down": true,
		"shift+home": true, "shift+end": true, "ctrl+shift+left": true, "ctrl+shift+right": true,
		"ctrl+shift+home": true, "ctrl+shift+end": true, "alt+a": true,
	}
	seen := map[string]bool{}
	for _, c := range loadGolden(t) {
		for _, k := range c.Keys {
			seen[k] = true
		}
	}
	for k := range seen {
		if leaves[k] {
			continue
		}
		ed := editorAt(t, 40, three, 1, 0)
		ed = press(t, ed, "shift+right", "shift+right", "shift+right", k)
		if ed.HasSelection() {
			t.Errorf("%s moved or edited the text but left %q selected", k, ed.SelectedText())
		}
	}
	if len(seen) < 40 {
		t.Fatalf("only %d keys found in the golden file", len(seen))
	}
}

func TestTypingOverASelectionOfEverythingLeavesJustTheTyping(t *testing.T) {
	ed := editorAt(t, 40, three, 0, 0)
	ed = press(t, ed, "alt+a", "type:z")
	if got := ed.Value(); got != "z" {
		t.Errorf("text = %q, want %q", got, "z")
	}
	wantNoSelection(t, ed, 0, 1)
}
