package editor

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// clock is a hand-wound time source, so a pause costs no sleeping.
type clock struct{ now time.Time }

func (c *clock) Now() time.Time { return c.now }

func (c *clock) advance(d time.Duration) { c.now = c.now.Add(d) }

// undoEditor is an empty editor on a clock the test winds.
func undoEditor(t *testing.T) (Model, *clock) {
	t.Helper()
	c := &clock{now: time.Date(2026, 10, 28, 9, 0, 0, 0, time.UTC)}
	ed := newEditor(40)
	ed.SetClock(c.Now)
	return ed, c
}

// typed presses each letter of s as its own key.
func typed(t *testing.T, ed Model, s string) Model {
	t.Helper()
	for _, r := range s {
		ed = press(t, ed, "type:"+string(r))
	}
	return ed
}

func wantText(t *testing.T, ed Model, want string) {
	t.Helper()
	if got := ed.Value(); got != want {
		t.Errorf("text = %q, want %q", got, want)
	}
}

func TestTypingAWordIsOneStep(t *testing.T) {
	ed, _ := undoEditor(t)
	ed = typed(t, ed, "select")
	ed = press(t, ed, "ctrl+z")
	wantText(t, ed, "")
}

func TestAPauseOfASecondStartsANewStep(t *testing.T) {
	ed, c := undoEditor(t)
	ed = typed(t, ed, "ab")
	c.advance(time.Second)
	ed = typed(t, ed, "cd")

	ed = press(t, ed, "ctrl+z")
	wantText(t, ed, "ab")
	ed = press(t, ed, "ctrl+z")
	wantText(t, ed, "")
}

func TestAShorterPauseKeepsTheRun(t *testing.T) {
	ed, c := undoEditor(t)
	ed = typed(t, ed, "ab")
	c.advance(time.Second - time.Millisecond)
	ed = typed(t, ed, "cd")

	ed = press(t, ed, "ctrl+z")
	wantText(t, ed, "")
}

func TestThePauseIsTimedFromTheLatestKeyNotTheFirst(t *testing.T) {
	ed, c := undoEditor(t)
	for _, r := range "abcde" {
		ed = press(t, ed, "type:"+string(r))
		c.advance(600 * time.Millisecond)
	}
	ed = press(t, ed, "ctrl+z")
	wantText(t, ed, "")
}

func TestSwitchingToDeletingStartsANewStep(t *testing.T) {
	ed, _ := undoEditor(t)
	ed = typed(t, ed, "select")
	ed = press(t, ed, "backspace", "backspace")
	wantText(t, ed, "sele")

	ed = press(t, ed, "ctrl+z")
	wantText(t, ed, "select")
	ed = press(t, ed, "ctrl+z")
	wantText(t, ed, "")
}

func TestSwitchingBackToTypingStartsANewStep(t *testing.T) {
	ed, _ := undoEditor(t)
	ed = typed(t, ed, "ab")
	ed = press(t, ed, "backspace", "backspace")
	ed = typed(t, ed, "xy")

	for _, want := range []string{"", "ab", ""} {
		ed = press(t, ed, "ctrl+z")
		wantText(t, ed, want)
	}
}

func TestADeleteRunIsOneStepForBothDeleteKeys(t *testing.T) {
	ed, _ := undoEditor(t)
	ed = typed(t, ed, "abcdef")
	ed = press(t, ed, "left", "left", "backspace", "delete", "ctrl+d", "ctrl+h")
	wantText(t, ed, "ab")
	ed = press(t, ed, "ctrl+z")
	wantText(t, ed, "abcdef")
}

func TestACursorMoveStartsANewStep(t *testing.T) {
	ed, _ := undoEditor(t)
	ed = typed(t, ed, "ab")
	ed = press(t, ed, "left")
	ed = typed(t, ed, "x")
	wantText(t, ed, "axb")

	ed = press(t, ed, "ctrl+z")
	wantText(t, ed, "ab")
	ed = press(t, ed, "ctrl+z")
	wantText(t, ed, "")
}

func TestMovingAwayAndBackToTheSamePlaceStillStartsANewStep(t *testing.T) {
	ed, _ := undoEditor(t)
	ed = typed(t, ed, "ab")
	ed = press(t, ed, "left", "right")
	ed = typed(t, ed, "c")

	ed = press(t, ed, "ctrl+z")
	wantText(t, ed, "ab")
}

func TestEnterIsAStepOfItsOwn(t *testing.T) {
	ed, _ := undoEditor(t)
	ed = typed(t, ed, "ab")
	ed = press(t, ed, "enter")
	ed = typed(t, ed, "cd")
	wantText(t, ed, "ab\ncd")

	for _, want := range []string{"ab\n", "ab", ""} {
		ed = press(t, ed, "ctrl+z")
		wantText(t, ed, want)
	}
}

func TestRedoReplaysTheStep(t *testing.T) {
	ed, c := undoEditor(t)
	ed = typed(t, ed, "ab")
	c.advance(time.Second)
	ed = typed(t, ed, "cd")

	ed = press(t, ed, "ctrl+z", "ctrl+z")
	wantText(t, ed, "")
	ed = press(t, ed, "ctrl+y")
	wantText(t, ed, "ab")
	wantNoSelection(t, ed, 0, 2)
	ed = press(t, ed, "ctrl+y")
	wantText(t, ed, "abcd")
	wantNoSelection(t, ed, 0, 4)

	ed = press(t, ed, "ctrl+y")
	wantText(t, ed, "abcd")
}

func TestANewEditAfterAnUndoClearsRedo(t *testing.T) {
	ed, _ := undoEditor(t)
	ed = typed(t, ed, "abc")
	ed = press(t, ed, "ctrl+z")
	ed = typed(t, ed, "x")
	ed = press(t, ed, "ctrl+y")
	wantText(t, ed, "x")
}

func TestUndoAndRedoWithNothingToDoChangeNothing(t *testing.T) {
	ed, _ := undoEditor(t)
	ed = press(t, ed, "ctrl+z", "ctrl+y")
	wantText(t, ed, "")
	ed = typed(t, ed, "a")
	ed = press(t, ed, "ctrl+z", "ctrl+z", "ctrl+z")
	wantText(t, ed, "")
}

func TestUndoPutsTheSelectionAndCursorBack(t *testing.T) {
	ed, _ := undoEditor(t)
	ed = typed(t, ed, "hello world")
	ed = press(t, ed, "shift+left", "shift+left", "shift+left", "shift+left", "shift+left")
	wantSelection(t, ed, "world", 0, 6)
	ed = typed(t, ed, "X")
	wantText(t, ed, "hello X")

	ed = press(t, ed, "ctrl+z")
	wantText(t, ed, "hello world")
	wantSelection(t, ed, "world", 0, 6)

	ed = press(t, ed, "ctrl+y")
	wantText(t, ed, "hello X")
	wantNoSelection(t, ed, 0, 7)
}

func TestUndoPutsTheCursorBackWhereTheEditBegan(t *testing.T) {
	ed, _ := undoEditor(t)
	ed = typed(t, ed, "abc")
	ed = press(t, ed, "left", "left", "backspace")
	wantText(t, ed, "bc")
	wantNoSelection(t, ed, 0, 0)

	ed = press(t, ed, "ctrl+z")
	wantText(t, ed, "abc")
	wantNoSelection(t, ed, 0, 1)
}

func TestUndoRestoresEveryCursorAndSelection(t *testing.T) {
	ed, _ := undoEditor(t)
	ed.SetValue(three)
	ed = press(t, ed, "ctrl+home")
	ed = press(t, ed, "ctrl+alt+down", "ctrl+alt+down")
	ed = press(t, ed, "shift+right", "shift+right")
	if ed.CursorCount() != 3 || ed.SelectedText() != "on\nth\nfi" {
		t.Fatalf("set up %d cursors selecting %q", ed.CursorCount(), ed.SelectedText())
	}
	ed = typed(t, ed, "X")
	wantText(t, ed, "Xe two\nXree four\nXve")

	ed = press(t, ed, "ctrl+z")
	wantText(t, ed, three)
	if ed.CursorCount() != 3 || ed.SelectedText() != "on\nth\nfi" {
		t.Errorf("after undo: %d cursors selecting %q, want 3 selecting %q", ed.CursorCount(), ed.SelectedText(), "on\nth\nfi")
	}

	ed = press(t, ed, "ctrl+y")
	wantText(t, ed, "Xe two\nXree four\nXve")
	wantHeads(t, ed, [2]int{0, 1}, [2]int{1, 1}, [2]int{2, 1})
}

func TestAnEditAtSeveralCursorsIsOneStep(t *testing.T) {
	ed, _ := undoEditor(t)
	ed.SetValue(three)
	ed = press(t, ed, "ctrl+home", "ctrl+alt+down", "ctrl+alt+down")
	ed = press(t, ed, "ctrl+k")
	wantText(t, ed, "\n\n")

	ed = press(t, ed, "ctrl+z")
	wantText(t, ed, three)
	wantHeads(t, ed, [2]int{0, 0}, [2]int{1, 0}, [2]int{2, 0})
}

func TestTheStackHoldsTwoHundredSteps(t *testing.T) {
	ed, c := undoEditor(t)
	for i := 0; i < 250; i++ {
		ed = press(t, ed, "type:x")
		c.advance(time.Second)
	}
	for i := 0; i < 200; i++ {
		ed = press(t, ed, "ctrl+z")
	}
	wantText(t, ed, strings.Repeat("x", 50))

	ed = press(t, ed, "ctrl+z")
	wantText(t, ed, strings.Repeat("x", 50))

	for i := 0; i < 200; i++ {
		ed = press(t, ed, "ctrl+y")
	}
	wantText(t, ed, strings.Repeat("x", 250))
}

func TestAVeryLongSessionKeepsTheStackBounded(t *testing.T) {
	ed, c := undoEditor(t)
	for i := 0; i < 1000; i++ {
		ed = press(t, ed, "type:x")
		c.advance(time.Second)
	}
	if n := len(ed.undoStack); n != 200 {
		t.Errorf("%d steps held after 1000, want 200", n)
	}
}

func TestAPasteIsOneStepAndNeverJoinsTyping(t *testing.T) {
	ed, _ := undoEditor(t)
	ed = typed(t, ed, "ab")
	ed, _ = ed.Update(PasteMsg("one\ntwo"))
	ed, _ = ed.Update(PasteMsg("!"))
	ed = typed(t, ed, "cd")
	wantText(t, ed, "abone\ntwo!cd")

	for _, want := range []string{"abone\ntwo!", "abone\ntwo", "ab", ""} {
		ed = press(t, ed, "ctrl+z")
		wantText(t, ed, want)
	}
}

func TestATerminalsOwnPasteIsOneStep(t *testing.T) {
	ed, _ := undoEditor(t)
	ed = typed(t, ed, "ab")
	ed = press(t, ed, "paste:cdef")
	ed = press(t, ed, "ctrl+z")
	wantText(t, ed, "ab")
}

func TestAPasteOverASelectionIsUndoneInOneStep(t *testing.T) {
	ed, _ := undoEditor(t)
	ed = typed(t, ed, "hello world")
	ed = press(t, ed, "alt+a")
	ed, _ = ed.Update(PasteMsg("bye"))
	wantText(t, ed, "bye")

	ed = press(t, ed, "ctrl+z")
	wantText(t, ed, "hello world")
	wantSelection(t, ed, "hello world", 0, 11)
}

func TestACutIsOneStep(t *testing.T) {
	ed, _ := undoEditor(t)
	ed = typed(t, ed, "one two")
	ed = press(t, ed, "shift+left", "shift+left", "shift+left")
	ed.DeleteSelection()
	wantText(t, ed, "one ")

	ed = press(t, ed, "ctrl+z")
	wantText(t, ed, "one two")
	wantSelection(t, ed, "two", 0, 4)
}

func TestACutAtSeveralCursorsIsOneStep(t *testing.T) {
	ed, _ := undoEditor(t)
	ed.SetValue(three)
	ed = press(t, ed, "ctrl+home", "ctrl+alt+down", "ctrl+alt+down", "shift+right", "shift+right")
	ed.DeleteSelection()
	wantText(t, ed, "e two\nree four\nve")

	ed = press(t, ed, "ctrl+z")
	wantText(t, ed, three)
	if ed.CursorCount() != 3 {
		t.Errorf("%d cursors after undo, want 3", ed.CursorCount())
	}
}

func TestSetValueIsOneStep(t *testing.T) {
	ed, _ := undoEditor(t)
	ed = typed(t, ed, "ab")
	ed.SetValue("select 1\nfrom t")

	ed = press(t, ed, "ctrl+z")
	wantText(t, ed, "ab")
	wantNoSelection(t, ed, 0, 2)
	ed = press(t, ed, "ctrl+y")
	wantText(t, ed, "select 1\nfrom t")
	wantNoSelection(t, ed, 1, 6)
}

func TestSetValueEndsTheRunBeforeIt(t *testing.T) {
	ed, _ := undoEditor(t)
	ed = typed(t, ed, "ab")
	ed.SetValue("x")
	ed = typed(t, ed, "c")

	ed = press(t, ed, "ctrl+z")
	wantText(t, ed, "x")
	ed = press(t, ed, "ctrl+z")
	wantText(t, ed, "ab")
}

func TestSetValueToTheTextItAlreadyHasIsNotAStep(t *testing.T) {
	ed, _ := undoEditor(t)
	ed = typed(t, ed, "ab")
	ed.SetValue("cd")
	ed.SetValue("cd")

	ed = press(t, ed, "ctrl+z")
	wantText(t, ed, "ab")
}

func TestInsertStringIsOneStep(t *testing.T) {
	ed, _ := undoEditor(t)
	ed = typed(t, ed, "ab")
	ed.InsertString("cd\nef")
	ed = press(t, ed, "ctrl+z")
	wantText(t, ed, "ab")
}

func TestBatchMakesSeveralChangesOneStep(t *testing.T) {
	ed, _ := undoEditor(t)
	ed = typed(t, ed, "hello")
	ed.Batch(func(e *Model) {
		*e, _ = e.Update(keyMsg(t, "backspace"))
		*e, _ = e.Update(keyMsg(t, "backspace"))
		e.InsertString("p!")
	})
	wantText(t, ed, "help!")

	ed = press(t, ed, "ctrl+z")
	wantText(t, ed, "hello")
	ed = press(t, ed, "ctrl+z")
	wantText(t, ed, "")
}

func TestSetInitialValueIsNotAStep(t *testing.T) {
	ed, _ := undoEditor(t)
	ed.SetInitialValue("SELECT * FROM t LIMIT 100")
	ed = press(t, ed, "ctrl+z")
	wantText(t, ed, "SELECT * FROM t LIMIT 100")

	ed = typed(t, ed, "x")
	ed = press(t, ed, "ctrl+z")
	wantText(t, ed, "SELECT * FROM t LIMIT 100")
	ed = press(t, ed, "ctrl+z")
	wantText(t, ed, "SELECT * FROM t LIMIT 100")
}

func TestSetInitialValueClearsRedo(t *testing.T) {
	ed, _ := undoEditor(t)
	ed = typed(t, ed, "ab")
	ed = press(t, ed, "ctrl+z")
	ed.SetInitialValue("SELECT 1")
	ed = press(t, ed, "ctrl+y")
	wantText(t, ed, "SELECT 1")
}

func TestTypingOverASelectionIsAStepOfItsOwn(t *testing.T) {
	ed, _ := undoEditor(t)
	ed = typed(t, ed, "hello world")
	ed = press(t, ed, "shift+left", "shift+left", "shift+left", "shift+left", "shift+left")
	ed = typed(t, ed, "XY")
	wantText(t, ed, "hello XY")

	ed = press(t, ed, "ctrl+z")
	wantText(t, ed, "hello X")
	ed = press(t, ed, "ctrl+z")
	wantText(t, ed, "hello world")
	wantSelection(t, ed, "world", 0, 6)
}

func TestAMoveMadeOutsideUpdateStillEndsTheRun(t *testing.T) {
	ed, _ := undoEditor(t)
	ed = typed(t, ed, "ab")
	ed.SetCursor(0)
	ed = typed(t, ed, "X")
	wantText(t, ed, "Xab")

	ed = press(t, ed, "ctrl+z")
	wantText(t, ed, "ab")
}

func TestASpaceDoesNotEndTheRunOnAnyPlatform(t *testing.T) {
	ed, _ := undoEditor(t)
	for _, r := range "SELECT 1 FROM" {
		k := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}}
		if r == ' ' {
			k.Type = tea.KeySpace
		}
		ed, _ = ed.Update(k)
	}
	wantText(t, ed, "SELECT 1 FROM")
	ed = press(t, ed, "ctrl+z")
	wantText(t, ed, "")
}

func TestSeveralRunesInOneKeyAreAPasteNotTyping(t *testing.T) {
	ed, _ := undoEditor(t)
	ed = typed(t, ed, "ab")
	ed = press(t, ed, "type:cdef")
	ed = typed(t, ed, "g")
	wantText(t, ed, "abcdefg")

	for _, want := range []string{"abcdef", "ab", ""} {
		ed = press(t, ed, "ctrl+z")
		wantText(t, ed, want)
	}
}

func TestSetInitialValueIsTheFloorEvenAfterEarlierEdits(t *testing.T) {
	ed, _ := undoEditor(t)
	ed = typed(t, ed, "abc")
	ed = press(t, ed, "backspace", "backspace", "backspace")
	ed.SetInitialValue("SELECT 1")

	ed = press(t, ed, "ctrl+z")
	wantText(t, ed, "SELECT 1")
}
