package editor

import (
	"math/rand"
	"slices"
	"strings"
	"testing"
)

// heads is where the cursors are, in text order, as row,column pairs.
func heads(ed Model) [][2]int {
	var out [][2]int
	for _, p := range ed.cursors() {
		out = append(out, [2]int{p.row, p.col})
	}
	return out
}

func wantHeads(t *testing.T, ed Model, want ...[2]int) {
	t.Helper()
	if got := heads(ed); !slices.Equal(got, want) {
		t.Errorf("cursors at %v, want %v", got, want)
	}
}

// withCursorsDown is editorAt with n-1 more cursors added below the first.
func withCursorsDown(t *testing.T, width int, text string, row, col, n int) Model {
	t.Helper()
	ed := editorAt(t, width, text, row, col)
	for i := 1; i < n; i++ {
		ed = press(t, ed, "ctrl+alt+down")
	}
	if ed.CursorCount() != n {
		t.Fatalf("set up %d cursors, have %d", n, ed.CursorCount())
	}
	return ed
}

func TestACursorIsAddedBelowAtTheSameColumn(t *testing.T) {
	ed := editorAt(t, 40, three, 0, 4)
	ed = press(t, ed, "ctrl+alt+down")
	wantHeads(t, ed, [2]int{0, 4}, [2]int{1, 4})
	if ed.HasSelection() {
		t.Errorf("a new cursor selects %q, want nothing", ed.SelectedText())
	}
	if r, c := cursorOf(ed); r != 1 || c != 4 {
		t.Errorf("main cursor at %d,%d, want 1,4", r, c)
	}

	ed = press(t, ed, "ctrl+alt+down")
	wantHeads(t, ed, [2]int{0, 4}, [2]int{1, 4}, [2]int{2, 4})
}

func TestACursorIsAddedAboveTheHighestOne(t *testing.T) {
	ed := editorAt(t, 40, three, 2, 2)
	ed = press(t, ed, "ctrl+alt+up")
	wantHeads(t, ed, [2]int{1, 2}, [2]int{2, 2})
	ed = press(t, ed, "ctrl+alt+up")
	wantHeads(t, ed, [2]int{0, 2}, [2]int{1, 2}, [2]int{2, 2})
}

func TestACursorOnAShortLineGoesToItsEndAndTheColumnIsRemembered(t *testing.T) {
	ed := editorAt(t, 40, "abcdef\nab\nabcdef", 0, 5)
	ed = press(t, ed, "ctrl+alt+down")
	wantHeads(t, ed, [2]int{0, 5}, [2]int{1, 2})
	ed = press(t, ed, "ctrl+alt+down")
	wantHeads(t, ed, [2]int{0, 5}, [2]int{1, 2}, [2]int{2, 5})
}

func TestNoCursorIsAddedPastTheFirstOrLastLine(t *testing.T) {
	ed := editorAt(t, 40, three, 2, 1)
	wantHeads(t, press(t, ed, "ctrl+alt+down"), [2]int{2, 1})
	ed = editorAt(t, 40, three, 0, 1)
	wantHeads(t, press(t, ed, "ctrl+alt+up"), [2]int{0, 1})
}

func TestTypingGoesInAtEveryCursor(t *testing.T) {
	ed := withCursorsDown(t, 40, three, 0, 1, 3)
	ed = press(t, ed, "type:X")
	if got, want := ed.Value(), "oXne two\ntXhree four\nfXive"; got != want {
		t.Errorf("text = %q, want %q", got, want)
	}
	wantHeads(t, ed, [2]int{0, 2}, [2]int{1, 2}, [2]int{2, 2})
}

func TestEnterSplitsEveryLine(t *testing.T) {
	ed := withCursorsDown(t, 40, "ab\ncd\nef", 0, 1, 3)
	ed = press(t, ed, "enter")
	if got, want := ed.Value(), "a\nb\nc\nd\ne\nf"; got != want {
		t.Errorf("text = %q, want %q", got, want)
	}
	wantHeads(t, ed, [2]int{1, 0}, [2]int{3, 0}, [2]int{5, 0})
}

func TestBackspaceAtEveryCursor(t *testing.T) {
	ed := withCursorsDown(t, 40, "ab\ncd\nef", 0, 1, 3)
	ed = press(t, ed, "backspace")
	if got, want := ed.Value(), "b\nd\nf"; got != want {
		t.Errorf("text = %q, want %q", got, want)
	}
	wantHeads(t, ed, [2]int{0, 0}, [2]int{1, 0}, [2]int{2, 0})
}

func TestBackspaceAtTheStartOfSeveralLinesJoinsThemAndKeepsEveryCursorInPlace(t *testing.T) {
	ed := withCursorsDown(t, 40, "ab\ncd\nef", 1, 0, 2)
	ed = press(t, ed, "backspace")
	if got, want := ed.Value(), "abcdef"; got != want {
		t.Errorf("text = %q, want %q", got, want)
	}
	wantHeads(t, ed, [2]int{0, 2}, [2]int{0, 4})
}

func TestDeleteAtTheEndOfSeveralLinesJoinsThemToo(t *testing.T) {
	ed := withCursorsDown(t, 40, "a\nb\nc", 0, 1, 2)
	ed = press(t, ed, "delete")
	if got, want := ed.Value(), "abc"; got != want {
		t.Errorf("text = %q, want %q", got, want)
	}
	wantHeads(t, ed, [2]int{0, 1}, [2]int{0, 2})
}

func TestTheLineEditingKeysActAtEveryCursor(t *testing.T) {
	cases := []struct {
		key, text, want string
		heads           [][2]int
	}{
		{"ctrl+w", "foo bar\nbaz qux", "foo \nbaz ", [][2]int{{0, 4}, {1, 4}}},
		{"ctrl+k", "abcd\nefgh\nijkl", "ab\nef\nij", [][2]int{{0, 2}, {1, 2}, {2, 2}}},
		{"ctrl+u", "abcd\nefgh\nijkl", "cd\ngh\nkl", [][2]int{{0, 0}, {1, 0}, {2, 0}}},
		{"alt+d", "ab cd\nef gh\nij kl", "ab\nef\nij", [][2]int{{0, 2}, {1, 2}, {2, 2}}},
	}
	for _, tc := range cases {
		n := strings.Count(tc.text, "\n") + 1
		col := 2
		if tc.key == "ctrl+w" {
			col = 7
		}
		if tc.key == "alt+d" {
			col = 2
		}
		ed := withCursorsDown(t, 40, tc.text, 0, col, n)
		ed = press(t, ed, tc.key)
		if got := ed.Value(); got != tc.want {
			t.Errorf("%s: text = %q, want %q", tc.key, got, tc.want)
		}
		wantHeads(t, ed, tc.heads...)
	}
}

func TestSelectionKeysGrowASelectionAtEveryCursor(t *testing.T) {
	ed := withCursorsDown(t, 40, three, 0, 0, 3)
	ed = press(t, ed, "ctrl+shift+right")
	if got, want := ed.SelectedText(), "one\nthree\nfive"; got != want {
		t.Errorf("selected %q, want %q", got, want)
	}
	if !ed.HasSelection() {
		t.Error("HasSelection is false")
	}
	wantHeads(t, ed, [2]int{0, 3}, [2]int{1, 5}, [2]int{2, 4})
}

func TestTypingReplacesEverySelectionAndBackspaceRemovesThem(t *testing.T) {
	ed := withCursorsDown(t, 40, three, 0, 0, 3)
	ed = press(t, ed, "ctrl+shift+right", "type:X")
	if got, want := ed.Value(), "X two\nX four\nX"; got != want {
		t.Errorf("after typing text = %q, want %q", got, want)
	}
	wantHeads(t, ed, [2]int{0, 1}, [2]int{1, 1}, [2]int{2, 1})

	ed = withCursorsDown(t, 40, three, 0, 0, 3)
	ed = press(t, ed, "ctrl+shift+right", "backspace")
	if got, want := ed.Value(), " two\n four\n"; got != want {
		t.Errorf("after backspace text = %q, want %q", got, want)
	}
}

func TestMovementMovesEveryCursor(t *testing.T) {
	ed := withCursorsDown(t, 40, three, 0, 2, 3)
	ed = press(t, ed, "end")
	wantHeads(t, ed, [2]int{0, 7}, [2]int{1, 10}, [2]int{2, 4})
	ed = press(t, ed, "home")
	wantHeads(t, ed, [2]int{0, 0}, [2]int{1, 0}, [2]int{2, 0})
}

func TestCursorsThatMeetBecomeOne(t *testing.T) {
	ed := withCursorsDown(t, 40, three, 0, 1, 3)
	ed = press(t, ed, "ctrl+end")
	if ed.CursorCount() != 1 {
		t.Errorf("%d cursors after they all went to the end, want 1", ed.CursorCount())
	}
	wantHeads(t, ed, [2]int{2, 4})
}

func TestSelectionsThatOverlapBecomeOne(t *testing.T) {
	ed := withCursorsDown(t, 40, "ab\ncd\nef", 0, 0, 3)
	ed = press(t, ed, "ctrl+shift+end")
	if ed.CursorCount() != 1 {
		t.Errorf("%d cursors, want the overlapping Selections merged into 1", ed.CursorCount())
	}
	if got, want := ed.SelectedText(), "ab\ncd\nef"; got != want {
		t.Errorf("selected %q, want %q", got, want)
	}
}

func TestAPasteGoesInAtEveryCursor(t *testing.T) {
	ed := withCursorsDown(t, 40, three, 0, 1, 3)
	ed, _ = ed.Update(PasteMsg("Z"))
	if got, want := ed.Value(), "oZne two\ntZhree four\nfZive"; got != want {
		t.Errorf("text = %q, want %q", got, want)
	}
}

func TestSelectAllEndsTheExtraCursors(t *testing.T) {
	ed := withCursorsDown(t, 40, three, 0, 1, 3)
	ed = press(t, ed, "alt+a")
	if ed.CursorCount() != 1 {
		t.Errorf("%d cursors, want 1", ed.CursorCount())
	}
	if got := ed.SelectedText(); got != three {
		t.Errorf("selected %q, want everything", got)
	}
}

func TestCollapsingKeepsTheMainCursorOnly(t *testing.T) {
	ed := withCursorsDown(t, 40, three, 0, 1, 3)
	if !ed.CollapseCursors() {
		t.Error("CollapseCursors said there was nothing to do")
	}
	wantHeads(t, ed, [2]int{2, 1})
	if ed.CollapseCursors() {
		t.Error("CollapseCursors said it did something with one cursor")
	}
}

func TestReplacingTheTextEndsTheExtraCursors(t *testing.T) {
	ed := withCursorsDown(t, 40, three, 0, 1, 3)
	ed.SetValue("new text")
	if ed.CursorCount() != 1 {
		t.Errorf("%d cursors after SetValue, want 1", ed.CursorCount())
	}
}

func TestEnterMakesNoNewLinesIfTheyWouldPassTheLimit(t *testing.T) {
	text := strings.Repeat("x\n", 96) + "x"
	ed := editorAt(t, 40, text, 96, 1)
	for range 2 {
		ed = press(t, ed, "ctrl+alt+up")
	}
	ed = press(t, ed, "enter")
	if got := ed.Value(); got != text {
		t.Errorf("enter changed the text (%d lines), want it left alone", strings.Count(got, "\n")+1)
	}

	ed = editorAt(t, 40, text, 96, 1)
	ed = press(t, ed, "ctrl+alt+up", "enter")
	if got := strings.Count(ed.Value(), "\n") + 1; got != 99 {
		t.Errorf("%d lines, want 99", got)
	}
}

func TestSeveralCursorsDoWhatEachWouldDoAlone(t *testing.T) {
	edits := []string{
		"type:x", "type:é", "backspace", "ctrl+h", "delete", "ctrl+d", "ctrl+w", "alt+backspace",
		"alt+d", "alt+delete", "ctrl+k", "ctrl+u", "enter", "ctrl+t", "alt+u", "alt+l", "alt+c",
	}
	moves := []string{
		"left", "right", "home", "end", "ctrl+a", "ctrl+e", "alt+left", "alt+right", "ctrl+left", "ctrl+right",
		"shift+left", "shift+right", "shift+home", "shift+end", "ctrl+shift+left", "ctrl+shift+right",
	}
	words := []string{"select", "from", "people", "where", "id", "name", "city", "order", "count"}
	rng := rand.New(rand.NewSource(7))

	trials := 0
	for trial := 0; trial < 600; trial++ {
		n := 2 + rng.Intn(4)
		lines := make([]string, n)
		minLen := 1 << 30
		for i := range lines {
			lines[i] = words[rng.Intn(len(words))] + " " + words[rng.Intn(len(words))]
			minLen = min(minLen, len(lines[i]))
		}
		col := 1 + rng.Intn(minLen-1)
		keys := []string{moves[rng.Intn(len(moves))], edits[rng.Intn(len(edits))]}
		if trial%3 == 0 {
			keys = keys[1:]
		}

		var wantText []string
		var expectHeads [][2]int
		var wantSelected []string
		row, crossed := 0, false
		for _, line := range lines {
			one := editorAt(t, 40, line, 0, col)
			for i, k := range keys {
				if _, c := cursorOf(one); (k == "delete" || k == "ctrl+d") && c == len(one.Value())-1 {
					crossed = true
				}
				one = press(t, one, k)
				if _, c := cursorOf(one); i < len(keys)-1 && (c == 0 || c == len(line)) {
					crossed = true
				}
			}
			r, c := cursorOf(one)
			expectHeads = append(expectHeads, [2]int{row + r, c})
			wantText = append(wantText, one.Value())
			if one.HasSelection() {
				wantSelected = append(wantSelected, one.SelectedText())
			}
			row += strings.Count(one.Value(), "\n") + 1
		}
		if crossed {
			continue
		}
		trials++

		ed := withCursorsDown(t, 40, strings.Join(lines, "\n"), 0, col, n)
		ed = press(t, ed, keys...)

		desc := strings.Join(lines, "|") + " at " + string(rune('0'+col)) + " keys " + strings.Join(keys, ",")
		if got, want := ed.Value(), strings.Join(wantText, "\n"); got != want {
			t.Errorf("%s: text = %q, want %q", desc, got, want)
			continue
		}
		if got := heads(ed); !slices.Equal(got, expectHeads) {
			t.Errorf("%s: cursors at %v, want %v", desc, got, expectHeads)
		}
		if got, want := ed.SelectedText(), strings.Join(wantSelected, "\n"); got != want {
			t.Errorf("%s: selected %q, want %q", desc, got, want)
		}
	}
	if trials < 200 {
		t.Fatalf("only %d fair comparisons ran; the generator is wrong", trials)
	}
}

// withExtraCursors adds cursors at the given (row, column) places; ed's own cursor stays
// the main one.
func withExtraCursors(ed Model, at ...[2]int) Model {
	for _, p := range at {
		ed.extras = append(ed.extras, caret{head: ed.offset(position{p[0], p[1]})})
	}
	return ed
}

func TestKeysThatReachOverAnotherCursorDoWhatBothWouldDoTogether(t *testing.T) {
	cases := []struct {
		name, key, text string
		main            [2]int
		other           [2]int
		want            string
		heads           [][2]int
	}{
		{"ctrl+u over the other", "ctrl+u", "prev\nabcdef", [2]int{1, 4}, [2]int{1, 3}, "prev\nef", [][2]int{{1, 0}}},
		{"ctrl+u the other way round", "ctrl+u", "prev\nabcdef", [2]int{1, 3}, [2]int{1, 4}, "prev\nef", [][2]int{{1, 0}}},
		{"ctrl+u nearer the start", "ctrl+u", "prev\nabcdef", [2]int{1, 2}, [2]int{1, 1}, "prev\ncdef", [][2]int{{1, 0}}},
		{"ctrl+k, one at the end of the line", "ctrl+k", "abcd\nnext", [2]int{0, 2}, [2]int{0, 4}, "abnext", [][2]int{{0, 2}}},
		{"ctrl+k, one inside what the other kills", "ctrl+k", "abcd\nnext", [2]int{0, 2}, [2]int{0, 3}, "ab\nnext", [][2]int{{0, 2}}},
		{"alt+d", "alt+d", "abc def\nnext", [2]int{0, 5}, [2]int{0, 7}, "abc dnext", [][2]int{{0, 5}}},
		{"ctrl+w", "ctrl+w", "foo bar baz", [2]int{0, 7}, [2]int{0, 11}, "foo  ", [][2]int{{0, 4}, {0, 5}}},
	}
	for _, tc := range cases {
		ed := editorAt(t, 40, tc.text, tc.main[0], tc.main[1])
		ed = withExtraCursors(ed, tc.other)
		ed = press(t, ed, tc.key)
		if got := ed.Value(); got != tc.want {
			t.Errorf("%s: text = %q, want %q", tc.name, got, tc.want)
		}
		wantHeads(t, ed, tc.heads...)
	}
}

func TestCursorsStayValidWhateverTheKeys(t *testing.T) {
	keys := []string{
		"type:x", "type:世", "paste:a\nb", "backspace", "delete", "ctrl+w", "alt+d", "ctrl+k", "ctrl+u", "enter",
		"left", "right", "up", "down", "home", "end", "alt+left", "alt+right", "ctrl+t", "alt+u",
		"shift+left", "shift+right", "shift+up", "shift+down", "ctrl+shift+left", "ctrl+shift+right",
		"shift+home", "shift+end", "ctrl+alt+up", "ctrl+alt+down", "alt+a",
	}
	words := []string{"ab", "世界", "foo bar", "", "x y z", "select name"}
	rng := rand.New(rand.NewSource(11))

	for trial := 0; trial < 3000; trial++ {
		n := 1 + rng.Intn(4)
		lines := make([]string, n)
		for i := range lines {
			lines[i] = words[rng.Intn(len(words))]
		}
		text := strings.Join(lines, "\n")
		ed := editorAt(t, 12+rng.Intn(30), text, 0, 0)
		for k := rng.Intn(4); k > 0; k-- {
			c := caret{head: rng.Intn(ed.total() + 1)}
			if rng.Intn(2) == 0 {
				c.anchor, c.hasAnchor = rng.Intn(ed.total()+1), true
			}
			ed.extras = append(ed.extras, c)
		}
		ed.settle(ed.allCarets())

		var done []string
		for step := 0; step < 6; step++ {
			k := keys[rng.Intn(len(keys))]
			done = append(done, k)
			ed = press(t, ed, k)
			_ = ed.View()

			total := ed.total()
			var spans [][2]int
			for _, c := range ed.allCarets() {
				lo, hi := c.extent()
				if c.head < 0 || c.head > total || c.anchor < 0 || c.anchor > total {
					t.Fatalf("keys %v on %q: cursor out of range (head %d anchor %d, text length %d)", done, text, c.head, c.anchor, total)
				}
				if c.hasAnchor && c.anchor == c.head {
					t.Fatalf("keys %v on %q: a Selection of nothing at %d", done, text, c.head)
				}
				spans = append(spans, [2]int{lo, hi})
			}
			for i := 1; i < len(spans); i++ {
				if spans[i][0] <= spans[i-1][1] {
					t.Fatalf("keys %v on %q: cursors %v overlap or touch", done, text, spans)
				}
			}
		}
	}
}

func TestACursorIsAddedBeyondTheEndOfASelectionNotInsideIt(t *testing.T) {
	ed := editorAt(t, 40, "abc\ndef\nghi", 2, 2)
	ed = press(t, ed, "shift+up", "shift+left", "ctrl+alt+down")
	if ed.CursorCount() != 1 {
		t.Errorf("%d cursors, want the one with the Selection", ed.CursorCount())
	}
	ed = press(t, ed, "backspace")
	if got, want := ed.Value(), "abc\ndi"; got != want {
		t.Errorf("text = %q, want %q (only the Selection goes)", got, want)
	}

	ed = editorAt(t, 40, "abc\ndef\nghi\njkl", 2, 2)
	ed = press(t, ed, "shift+up", "ctrl+alt+down")
	if ed.CursorCount() != 2 {
		t.Fatalf("%d cursors, want 2", ed.CursorCount())
	}
	if got := heads(ed); got[1][0] != 3 {
		t.Errorf("new cursor on line %d, want line 3", got[1][0])
	}
}

func TestAnAddedCursorKeepsTheWantedColumnForUpAndDown(t *testing.T) {
	text := "abcdefgh\nab\nabcdefgh\nab\nabcdefgh"
	ed := editorAt(t, 40, text, 0, 6)
	ed = press(t, ed, "ctrl+alt+down")
	wantHeads(t, ed, [2]int{0, 6}, [2]int{1, 2})
	ed = press(t, ed, "down")
	wantHeads(t, ed, [2]int{1, 2}, [2]int{2, 6})
}

func TestDeleteSelectionRemovesEverySelectionAndNothingElse(t *testing.T) {
	ed := withCursorsDown(t, 40, three, 0, 0, 3)
	ed = press(t, ed, "ctrl+shift+right")
	ed.DeleteSelection()
	if got, want := ed.Value(), " two\n four\n"; got != want {
		t.Errorf("text = %q, want %q", got, want)
	}
	if ed.HasSelection() {
		t.Errorf("%q is still selected", ed.SelectedText())
	}

	ed = editorAt(t, 40, "one two\nthree", 0, 0)
	ed = press(t, ed, "shift+right", "shift+right", "shift+right")
	ed = withExtraCursors(ed, [2]int{1, 2})
	ed.DeleteSelection()
	if got, want := ed.Value(), " two\nthree"; got != want {
		t.Errorf("text = %q, want %q (a cursor with nothing selected deletes nothing)", got, want)
	}
	wantHeads(t, ed, [2]int{0, 0}, [2]int{1, 2})
}

func TestEnterAtSeveralCursorsCountsTheLinesASelectionGivesBack(t *testing.T) {
	text := strings.Repeat("x\n", 97) + "x"
	ed := editorAt(t, 40, text, 0, 0)
	ed = press(t, ed, "shift+down", "shift+down", "shift+down")
	ed = withExtraCursors(ed, [2]int{97, 1})

	ed = press(t, ed, "enter")

	if got := strings.Count(ed.Value(), "\n") + 1; got != 97 {
		t.Errorf("%d lines, want 97: enter ran at both cursors", got)
	}
}

func TestEnterAtSeveralCursorsStillRefusesWhenAnyCursorHasNoRoom(t *testing.T) {
	text := strings.Repeat("x\n", 97) + "x"
	ed := withExtraCursors(editorAt(t, 40, text, 0, 1), [2]int{50, 1})
	ed = press(t, ed, "enter")

	if got := ed.Value(); got != text {
		t.Errorf("enter changed the text (%d lines), want it left alone", strings.Count(got, "\n")+1)
	}
}

func TestInsertStringEndsTheExtraCursors(t *testing.T) {
	ed := editorAt(t, 40, "abc\ndef\nghi", 2, 1)
	ed = press(t, ed, "ctrl+alt+up")
	if ed.CursorCount() != 2 {
		t.Fatalf("set up %d cursors, want 2", ed.CursorCount())
	}

	ed.InsertString("XY")

	if got, want := ed.Value(), "abc\ndXYef\nghi"; got != want {
		t.Errorf("text = %q, want %q", got, want)
	}
	if ed.CursorCount() != 1 {
		t.Errorf("%d cursors after InsertString, want 1", ed.CursorCount())
	}
	ed = typed(t, ed, "Z")
	if got, want := ed.Value(), "abc\ndXYZef\nghi"; got != want {
		t.Errorf("typing after InsertString gave %q, want %q", got, want)
	}
}
