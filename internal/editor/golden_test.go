package editor

import (
	"compress/gzip"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"sort"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// goldenState and goldenCase are what testdata/textarea-golden.json.gz holds: what the
// Bubbles textarea did, recorded once.
type goldenState struct {
	V    string `json:"v"`
	Row  int    `json:"row"`
	Col  int    `json:"col"`
	LI   [7]int `json:"li"`
	View string `json:"view"`
}

type goldenCase struct {
	Text   string        `json:"text"`
	Width  int           `json:"width"`
	Row    int           `json:"row"`
	Col    int           `json:"col"`
	Keys   []string      `json:"keys"`
	States []goldenState `json:"states"`
}

func loadGolden(t *testing.T) []goldenCase {
	t.Helper()
	f, err := os.Open("testdata/textarea-golden.json.gz")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	zr, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	var cases []goldenCase
	if err := json.NewDecoder(zr).Decode(&cases); err != nil {
		t.Fatal(err)
	}
	if len(cases) < 10000 {
		t.Fatalf("only %d golden cases; the file is damaged", len(cases))
	}
	return cases
}

// keyMsg turns a key as the golden file names it into the message Bubble Tea sends.
func keyMsg(t *testing.T, spec string) tea.KeyMsg {
	t.Helper()
	if s, ok := strings.CutPrefix(spec, "type:"); ok {
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
	}
	if s, ok := strings.CutPrefix(spec, "paste:"); ok {
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s), Paste: true}
	}
	alt := func(k tea.KeyType) tea.KeyMsg { return tea.KeyMsg{Type: k, Alt: true} }
	altRune := func(r rune) tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}, Alt: true} }
	plain := map[string]tea.KeyType{
		"left": tea.KeyLeft, "right": tea.KeyRight, "up": tea.KeyUp, "down": tea.KeyDown,
		"ctrl+b": tea.KeyCtrlB, "ctrl+f": tea.KeyCtrlF, "ctrl+p": tea.KeyCtrlP, "ctrl+n": tea.KeyCtrlN,
		"home": tea.KeyHome, "end": tea.KeyEnd, "ctrl+a": tea.KeyCtrlA, "ctrl+e": tea.KeyCtrlE,
		"ctrl+home": tea.KeyCtrlHome, "ctrl+end": tea.KeyCtrlEnd,
		"backspace": tea.KeyBackspace, "ctrl+h": tea.KeyCtrlH, "delete": tea.KeyDelete, "ctrl+d": tea.KeyCtrlD,
		"ctrl+w": tea.KeyCtrlW, "ctrl+k": tea.KeyCtrlK, "ctrl+u": tea.KeyCtrlU,
		"enter": tea.KeyEnter, "ctrl+m": tea.KeyCtrlM, "ctrl+t": tea.KeyCtrlT, "ctrl+v": tea.KeyCtrlV,
		"tab": tea.KeyTab, "esc": tea.KeyEsc, "pgup": tea.KeyPgUp, "f1": tea.KeyF1,
		"shift+left": tea.KeyShiftLeft, "ctrl+left": tea.KeyCtrlLeft, "ctrl+right": tea.KeyCtrlRight, "ctrl+z": tea.KeyCtrlZ, "ctrl+y": tea.KeyCtrlY,
		"shift+right": tea.KeyShiftRight, "shift+up": tea.KeyShiftUp, "shift+down": tea.KeyShiftDown,
		"shift+home": tea.KeyShiftHome, "shift+end": tea.KeyShiftEnd,
		"ctrl+shift+left": tea.KeyCtrlShiftLeft, "ctrl+shift+right": tea.KeyCtrlShiftRight,
		"ctrl+shift+home": tea.KeyCtrlShiftHome, "ctrl+shift+end": tea.KeyCtrlShiftEnd,
	}
	if k, ok := plain[spec]; ok {
		return tea.KeyMsg{Type: k}
	}
	switch spec {
	case "ctrl+alt+up":
		return alt(tea.KeyCtrlUp)
	case "ctrl+alt+down":
		return alt(tea.KeyCtrlDown)
	case "alt+left":
		return alt(tea.KeyLeft)
	case "alt+right":
		return alt(tea.KeyRight)
	case "alt+backspace":
		return alt(tea.KeyBackspace)
	case "alt+delete":
		return alt(tea.KeyDelete)
	case "alt+b", "alt+f", "alt+d", "alt+c", "alt+l", "alt+u", "alt+a", "alt+<", "alt+>":
		return altRune([]rune(spec)[4])
	}
	t.Fatalf("golden file names a key this test does not know: %q", spec)
	return tea.KeyMsg{}
}

// newEditor is how lode sets the editor up, at a given total width.
func newEditor(width int) Model {
	ed := New()
	ed.Placeholder = "SELECT * FROM ...   (ctrl+r to run)"
	ed.SetHeight(5)
	ed.Focus()
	ed.SetWidth(width)
	return ed
}

func stateOf(ed Model) goldenState {
	li := ed.LineInfo()
	sum := sha256.Sum256([]byte(ed.View()))
	return goldenState{
		V:    ed.Value(),
		Row:  ed.Line(),
		Col:  li.StartColumn + li.ColumnOffset,
		LI:   [7]int{li.Width, li.CharWidth, li.Height, li.StartColumn, li.ColumnOffset, li.RowOffset, li.CharOffset},
		View: fmt.Sprintf("%x", sum[:8]),
	}
}

func TestEditorBehavesAsTheTextareaDid(t *testing.T) {
	cases := loadGolden(t)

	type miss struct {
		count   int
		example string
	}
	misses := map[string]*miss{}
	note := func(group, field, detail string) {
		k := group + " / " + field
		if misses[k] == nil {
			misses[k] = &miss{example: detail}
		}
		misses[k].count++
	}

	changed := map[string]bool{
		"shift+left": true, "ctrl+left": true, "ctrl+right": true, "ctrl+z": true,
		"paste:a\tb\r\nc\n": true,
	}

	replayed, stuck := 0, 0
	for _, c := range cases {
		if slices.ContainsFunc(c.Keys, func(k string) bool { return changed[k] }) {
			continue
		}
		if stuckInTheTextarea(c) {
			stuck++
			continue
		}
		replayed++
		group := "sequence"
		if len(c.Keys) == 1 {
			group = c.Keys[0]
		}
		ed := newEditor(c.Width)
		ed.SetValue(c.Text)
		for ed.Line() > c.Row {
			ed.CursorUp()
		}
		ed.SetCursor(c.Col)

		where := fmt.Sprintf("text %q width %d at %d,%d keys %v", c.Text, c.Width, c.Row, c.Col, c.Keys)
		for i, want := range c.States {
			if i > 0 {
				ed, _ = ed.Update(keyMsg(t, c.Keys[i-1]))
			}
			got := stateOf(ed)
			step := fmt.Sprintf("%s, after %d keys", where, i)
			switch {
			case got.V != want.V:
				note(group, "text", fmt.Sprintf("%s: got %q want %q", step, got.V, want.V))
			case got.Row != want.Row || got.Col != want.Col:
				note(group, "cursor", fmt.Sprintf("%s: got %d,%d want %d,%d", step, got.Row, got.Col, want.Row, want.Col))
			case got.LI != want.LI:
				note(group, "lineinfo", fmt.Sprintf("%s: got %v want %v", step, got.LI, want.LI))
			case got.View != want.View:
				note(group, "view", step)
			default:
				continue
			}
			break
		}
	}

	if replayed < 14000 {
		t.Errorf("only %d of %d recorded cases were replayed; the skip list has grown too far", replayed, len(cases))
	}
	if stuck != 26 {
		t.Errorf("%d recorded cases are left out as the textarea's stuck cursor, want the 26 there were when this was written", stuck)
	}
	if len(misses) == 0 {
		return
	}
	keys := make([]string, 0, len(misses))
	for k := range misses {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	for _, k := range keys {
		fmt.Fprintf(&b, "\n%s: %d cases, e.g. %s", k, misses[k].count, misses[k].example)
	}
	t.Errorf("%d groups differ from the textarea:%s", len(misses), b.String())
}

// stuckInTheTextarea says a recorded run had up, down, ctrl+p or ctrl+n take the textarea's
// cursor the wrong way.
func stuckInTheTextarea(c goldenCase) bool {
	for i, k := range c.Keys {
		up := k == "up" || k == "ctrl+p"
		if !up && k != "down" && k != "ctrl+n" {
			continue
		}
		before, after := c.States[i], c.States[i+1]
		earlier := func(a, b goldenState) bool {
			return a.Row < b.Row || a.Row == b.Row && a.LI[5] < b.LI[5]
		}
		same := before.Row == after.Row && before.LI[5] == after.LI[5]
		lastLine := strings.Count(before.V, "\n")
		atEdge := up && before.Row == 0 && before.LI[5] == 0 ||
			!up && before.Row == lastLine && before.LI[5]+1 >= before.LI[2]
		switch {
		case up && earlier(before, after), !up && earlier(after, before):
			return true
		case same && !atEdge:
			return true
		}
	}
	return false
}

func TestWordLeftWithOnlySpaceBeforeTheCursorDoesNotHang(t *testing.T) {
	for _, tc := range []struct{ text string }{{""}, {"   abc"}, {"\n\n  x"}} {
		for _, k := range []string{"alt+left", "alt+b"} {
			done := make(chan struct{})
			go func() {
				defer close(done)
				ed := newEditor(30)
				ed.SetValue(tc.text)
				ed.SetCursor(0)
				for ed.Line() > 0 {
					ed.CursorUp()
				}
				ed.SetCursor(0)
				ed, _ = ed.Update(keyMsg(t, k))
				if ed.Value() != tc.text {
					t.Errorf("%q: %s changed the text to %q", tc.text, k, ed.Value())
				}
			}()
			select {
			case <-done:
			case <-time.After(2 * time.Second):
				t.Fatalf("%q: %s did not return", tc.text, k)
			}
		}
	}
}

func TestWordLeftFromInsideLeadingSpaceGoesToTheStart(t *testing.T) {
	done := make(chan Model, 1)
	go func() {
		ed := newEditor(30)
		ed.SetValue("   abc")
		ed.SetCursor(2)
		ed, _ = ed.Update(keyMsg(t, "alt+left"))
		done <- ed
	}()
	select {
	case ed := <-done:
		if li := ed.LineInfo(); li.StartColumn+li.ColumnOffset != 0 {
			t.Errorf("cursor at column %d, want 0", li.StartColumn+li.ColumnOffset)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("alt+left did not return")
	}
}
