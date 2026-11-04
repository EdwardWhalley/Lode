package editor

import (
	"math/rand"
	"testing"
)

func TestDownFromALineAsWideAsTheTextReachesTheNextLine(t *testing.T) {
	const text = "aaaaaaaaaa\nbbbbbbbbbb\nz"
	for _, key := range []string{"down", "shift+down"} {
		ed := editorAt(t, 12, text, 0, 0)
		for i := 0; i < 6 && ed.Line() < 2; i++ {
			ed = press(t, ed, key)
		}
		if ed.Line() != 2 {
			t.Errorf("%s repeatedly: cursor on line %d, want 2", key, ed.Line())
		}
	}
}

func TestUpOntoARowEndingInZeroWidthCharactersMovesEveryTime(t *testing.T) {
	const text = "a本eb\u0301 🙂\u200b\u0301\u0301🙂"
	for _, key := range []string{"up", "shift+up"} {
		ed := editorAt(t, 7, text, 0, 7)
		for want := ed.cursorLineNumber() - 1; want >= 0; want-- {
			ed = press(t, ed, key)
			if got := ed.cursorLineNumber(); got != want {
				t.Fatalf("%s: cursor on drawn row %d, want %d", key, got, want)
			}
		}
	}
}

func TestUpAndDownFromTheTicketsWideCharacterText(t *testing.T) {
	const text = "日本語日本語 abc def\n\nxy日z\né..."
	ed := editorAt(t, 6, text, 3, 0)
	for ed.cursorLineNumber() > 0 {
		before := ed.cursorLineNumber()
		ed = press(t, ed, "up")
		if got := ed.cursorLineNumber(); got != before-1 {
			t.Fatalf("up from drawn row %d reached %d", before, got)
		}
	}
	for i := 0; i < 30 && ed.Line() < 3; i++ {
		before := ed.cursorLineNumber()
		ed = press(t, ed, "down")
		if got := ed.cursorLineNumber(); got != before+1 {
			t.Fatalf("down from drawn row %d reached %d", before, got)
		}
	}
}

func TestSeveralCursorsMoveDownFromAFullWidthLine(t *testing.T) {
	ed := editorAt(t, 12, "aaaaaaaaaa\nbbbbbbbbbb\nz", 0, 0)
	ed = press(t, ed, "ctrl+alt+down")
	ed = press(t, ed, "down")
	if got := len(ed.extras); got != 1 {
		t.Fatalf("%d extra cursors, want 1", got)
	}
	if row, col := cursorOf(ed); row != 1 || col != 10 {
		t.Errorf("main cursor at %d,%d, want 1,10", row, col)
	}
	if head := ed.extras[0].head; head != 10 {
		t.Errorf("the other cursor is at offset %d, want 10", head)
	}
}

func TestSeveralCursorsMoveUpOntoARowEndingInZeroWidthCharacters(t *testing.T) {
	const text = "a本eb́ 🙂​́́🙂\nx"
	ed := editorAt(t, 7, text, 0, 7)
	ed = press(t, ed, "ctrl+alt+down")
	row0 := ed.extras[0].head
	ed = press(t, ed, "up")
	if got := len(ed.extras); got != 1 {
		t.Fatalf("%d extra cursors, want 1", got)
	}
	if r := ed.cursorLineNumber(); r != 2 {
		t.Errorf("main cursor on drawn row %d, want 2", r)
	}
	other := ed
	other.row, other.col = 0, ed.extras[0].head
	if r := other.cursorLineNumber(); r != 1 || ed.extras[0].head == row0 {
		t.Errorf("the other cursor is on drawn row %d (offset %d, was %d), want one row up", r, ed.extras[0].head, row0)
	}
}

func TestUpAndDownMoveExactlyOneDrawnRow(t *testing.T) {
	alpha := []rune("ab 日本e\u0301\u200b\n")
	rng := rand.New(rand.NewSource(12))
	for n := 0; n < 3000; n++ {
		rs := make([]rune, 1+rng.Intn(14))
		for i := range rs {
			rs[i] = alpha[rng.Intn(len(alpha))]
		}
		text, width := string(rs), 5+rng.Intn(9)
		ed := newEditor(width)
		ed.SetValue(text)
		ed.row = rng.Intn(len(ed.value))
		ed.SetCursor(rng.Intn(len(ed.value[ed.row]) + 1))
		for _, key := range []string{"up", "down"} {
			before := ed
			li := before.LineInfo()
			top := before.Line() == 0 && li.RowOffset == 0
			bottom := before.Line() == len(before.value)-1 && li.RowOffset+1 >= li.Height
			if key == "up" && top || key == "down" && bottom {
				continue
			}
			step := 1
			if key == "up" {
				step = -1
			}
			want := before.cursorLineNumber() + step
			after := press(t, before, key)
			if got := after.cursorLineNumber(); got != want {
				t.Fatalf("%s in %q at width %d from %d,%d: drawn row %d, want %d",
					key, text, width, before.row, before.col, got, want)
			}
		}
	}
}
