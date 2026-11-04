package tui

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

// openedValueView has run a query answered by these values and opened the Value view on the
// first of them.
func openedValueView(t *testing.T, sys *fakeSys, values ...any) Model {
	t.Helper()
	m := copyModel(t, oneRowOf(values...), sys)
	m, _ = send(t, m, enter)
	if m.overlay == nil {
		t.Fatal("enter did not open the Value view")
	}
	return m
}

// valueBody is the Overlay's lines below its title, with the padding gone from the end.
func valueBody(t *testing.T, m Model) []string {
	t.Helper()
	rows := lines(plain(m))
	body := rows[2 : 2+m.overlayHeight()-1]
	for len(body) > 0 && strings.TrimSpace(body[len(body)-1]) == "" {
		body = body[:len(body)-1]
	}
	return body
}

func TestEnterOpensTheWholeValueWrapped(t *testing.T) {
	long := strings.Repeat("abcdefghij", 30)
	m := openedValueView(t, &fakeSys{}, long)
	body := valueBody(t, m)
	if got := strings.Join(body, ""); got != long {
		t.Errorf("body holds %q, want the whole value", got)
	}
	if len(body) < 2 {
		t.Errorf("the value was not wrapped: %q", body)
	}
	fits(t, m)
}

func TestTheValueViewShowsLineBreaksWithCRLFAsOne(t *testing.T) {
	m := openedValueView(t, &fakeSys{}, "one\r\ntwo\rthree\nfour")
	want := []string{"one", "two", "three", "four"}
	got := valueBody(t, m)
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("body = %q, want %q", got, want)
	}
}

func TestBytesFromTheDriverShowTheirLineBreaks(t *testing.T) {
	m := openedValueView(t, &fakeSys{}, []byte("one\ntwo"))
	if got := valueBody(t, m); strings.Join(got, "|") != "one|two" {
		t.Errorf("body = %q", got)
	}
}

func TestTabsAreSpacesAndWideCharactersWrapWithoutBeingCut(t *testing.T) {
	m := openedValueView(t, &fakeSys{}, "a\tb")
	if got := valueBody(t, m); len(got) != 1 || strings.Contains(got[0], "\t") || !strings.HasPrefix(got[0], "a ") {
		t.Errorf("body = %q", got)
	}

	wide := strings.Repeat("世", 200)
	m = openedValueView(t, &fakeSys{}, wide)
	m, _ = send(t, m, tea.WindowSizeMsg{Width: 21, Height: 60})
	body := valueBody(t, m)
	if got := strings.Join(body, ""); got != wide {
		t.Errorf("wide characters were cut: %d runes of 200", len([]rune(got)))
	}
	for _, line := range body {
		if ansi.StringWidth(line) > 21 {
			t.Errorf("a line is %d cells wide in a window of 21", ansi.StringWidth(line))
		}
	}
}

func TestAnEscapeIsMadeHarmlessInTheBodyAndTheTitle(t *testing.T) {
	m := copyModel(t, valuesAdapter{cols: []string{"no\x1b[31mtes"}, rows: [][]any{{"a\x1b[31mb\x07c\u009bd"}}}, &fakeSys{})
	m, _ = send(t, m, enter)
	view := m.View()
	if strings.Contains(view, "\x1b[31m") || strings.ContainsAny(view, "\x07\u009b") {
		t.Errorf("a control character from the data reached the terminal:\n%q", view)
	}
	if !strings.Contains(plain(m), "a·[31mb·c·d") {
		t.Errorf("the value is not shown with its control characters made harmless:\n%s", plain(m))
	}
	if !strings.Contains(plain(m), "no·[31mtes") {
		t.Errorf("the column name is not made harmless in the title:\n%s", plain(m))
	}
}

// escapedE is the six characters of a JSON escape, not the letter it stands for.
const escapedE = `\` + `u00e9`

func TestJSONIsIndentedAndKeepsWhatWasStored(t *testing.T) {
	cases := []struct {
		name, value string
		want        []string
	}{
		{
			"an object keeps its key order, its number text and its string escapes",
			// bounded: these digits are the text of a JSON value, and no query asks for them.
			`{"z":1.0000000000000001,"a":[1,2],"big":12345678901234567890,"e":1E5,"s":"a` + escapedE + `"}`,
			[]string{
				`{`,
				// bounded: the same number, as text.
				`  "z": 1.0000000000000001,`,
				`  "a": [`,
				`    1,`,
				`    2`,
				`  ],`,
				// bounded: the same number, as text.
				`  "big": 12345678901234567890,`,
				`  "e": 1E5,`,
				`  "s": "a` + escapedE + `"`,
				`}`,
			},
		},
		{
			"an array, and space around it",
			"  [true, null]\n",
			[]string{`[`, `  true,`, `  null`, `]`},
		},
		{"a scalar is shown as it is", `123`, []string{`123`}},
		{"a string scalar is shown as it is", `"text"`, []string{`"text"`}},
		{"text that only looks like JSON", `{not json}`, []string{`{not json}`}},
		{"an array left open", `[1, 2`, []string{`[1, 2`}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m := openedValueView(t, &fakeSys{}, c.value)
			got := valueBody(t, m)
			if strings.Join(got, "\n") != strings.Join(c.want, "\n") {
				t.Errorf("body:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(c.want, "\n"))
			}
		})
	}
}

func TestBytesHoldingJSONAreIndentedToo(t *testing.T) {
	m := openedValueView(t, &fakeSys{}, []byte(`{"a":1}`))
	if got := valueBody(t, m); strings.Join(got, "|") != `{|  "a": 1|}` {
		t.Errorf("body = %q", got)
	}
}

func TestBinaryNullAndTimeAreInTheGridsForms(t *testing.T) {
	stamp := time.Date(2026, 11, 1, 18, 30, 5, 0, time.UTC)
	for _, c := range []struct {
		value any
		want  string
	}{
		{[]byte{0xff, 0x00}, `\xff00`},
		{[]byte{0x00, 0x01}, `\x0001`},
		{nil, nullText},
		{stamp, "2026-11-01 18:30:05"},
		{int64(42), "42"},
	} {
		m := openedValueView(t, &fakeSys{}, c.value)
		if got := valueBody(t, m); len(got) != 1 || got[0] != c.want {
			t.Errorf("value %#v shows %q, want %q", c.value, got, c.want)
		}
		grid := formatValue(c.value)
		if grid != c.want {
			t.Errorf("value %#v: the grid shows %q and the Value view %q", c.value, grid, c.want)
		}
	}
}

func TestTheTitleNamesTheColumnAndTheRow(t *testing.T) {
	rows := make([][]any, 300)
	for i := range rows {
		rows[i] = []any{i, "x"}
	}
	m := copyModel(t, valuesAdapter{cols: []string{"id", "notes"}, rows: rows}, &fakeSys{})
	m = keys(t, m, keyRight)
	m = keys(t, m, repeat(keyDown, 11)...)
	m, _ = send(t, m, enter)
	if !strings.Contains(plain(m), "notes, row 12 of 300") {
		t.Errorf("title:\n%s", plain(m))
	}
}

func TestTheValueViewScrollsWhenTallerThanTheOverlay(t *testing.T) {
	var b strings.Builder
	for i := range 200 {
		fmt.Fprintf(&b, "line%03d\n", i)
	}
	m := openedValueView(t, &fakeSys{}, strings.TrimSuffix(b.String(), "\n"))
	first := func() string { return valueBody(t, m)[0] }
	height := m.overlayHeight() - 1

	if got := first(); got != "line000" {
		t.Fatalf("starts at %q", got)
	}
	for _, step := range []struct {
		key  tea.KeyMsg
		want string
	}{
		{keyUp, "line000"},
		{keyDown, "line001"},
		{keyUp, "line000"},
		{keyPgDn, fmt.Sprintf("line%03d", height)},
		{keyPgUp, "line000"},
		{keyEnd, fmt.Sprintf("line%03d", 200-height)},
		{keyDown, fmt.Sprintf("line%03d", 200-height)},
		{keyPgDn, fmt.Sprintf("line%03d", 200-height)},
		{keyHome, "line000"},
	} {
		m, _ = send(t, m, step.key)
		if got := first(); got != step.want {
			t.Errorf("after %s the first line is %q, want %q", step.key, got, step.want)
		}
		fits(t, m)
	}
}

func TestEscAndEnterCloseAndLeaveTheHighlightWhereItWas(t *testing.T) {
	for _, key := range []tea.KeyMsg{esc, enter} {
		withColour(t)
		a := valuesAdapter{cols: []string{"a", "b"}, rows: [][]any{{"r0a", "r0b"}, {"r1a", "r1b"}}}
		m := copyModel(t, a, &fakeSys{})
		m = keys(t, m, keyDown, keyRight)
		m, _ = send(t, m, enter)
		m, _ = send(t, m, key)
		if m.overlay != nil {
			t.Errorf("%s did not close it", key)
		}
		wantHighlight(t, m, "r1b")
	}
}

func TestCopyingGivesTheOriginalAndNotTheIndentedText(t *testing.T) {
	// bounded: these digits are the text of a JSON value, and no query asks for them.
	const stored = `{"z":1.0000000000000001,"a":[1,2]}`
	for _, key := range []tea.KeyMsg{keyC, ctrlC} {
		sys := &fakeSys{}
		m := openedValueView(t, sys, stored)
		m, cmd := send(t, m, key)
		m = drain(t, m, cmd)
		if sys.text != stored {
			t.Errorf("%s copied %q, want %q", key, sys.text, stored)
		}
		if m.overlay == nil {
			t.Errorf("%s closed the Value view", key)
		}
		if !strings.Contains(m.status, "copied 34 characters") {
			t.Errorf("status = %q", m.status)
		}
	}
}

func TestCopyingNullFromTheValueViewIsEmpty(t *testing.T) {
	sys := &fakeSys{text: "before"}
	m := openedValueView(t, sys, nil)
	m, cmd := send(t, m, keyC)
	m = drain(t, m, cmd)
	if sys.text != "" {
		t.Errorf("clipboard holds %q", sys.text)
	}
	if m.status != "copied NULL (empty)" {
		t.Errorf("status = %q", m.status)
	}
}

func TestTheValueViewKeepsItsValueWhenAQueryFinishes(t *testing.T) {
	sys := &fakeSys{}
	m := openedValueView(t, sys, "the first value")
	m, _ = send(t, m, resultMsg{id: m.query.id, cols: []string{"other"}, rows: [][]string{{"new"}}, raw: [][]any{{"new"}}, rawCols: []string{"other"}})
	if !strings.Contains(plain(m), "the first value") || strings.Contains(plain(m), "new") {
		t.Errorf("the view changed:\n%s", plain(m))
	}
	m, cmd := send(t, m, keyC)
	drain(t, m, cmd)
	if sys.text != "the first value" {
		t.Errorf("copied %q, want what is on screen", sys.text)
	}
}

func TestEnterWithNoRowsDoesNothing(t *testing.T) {
	m := copyModel(t, valuesAdapter{cols: []string{"a"}}, &fakeSys{})
	m, _ = send(t, m, enter)
	if m.overlay != nil {
		t.Errorf("enter opened %T", m.overlay)
	}
}

func TestEnterWithTheTreeFocusedStillExpandsATable(t *testing.T) {
	m, _ := completeModel(t, schemaFixture(), "", -1)
	m, _ = send(t, m, cellResult(m, 3, 3))
	m, _ = send(t, m, tab)
	m, _ = send(t, m, tab)
	m, _ = send(t, m, enter)
	if m.overlay != nil {
		t.Errorf("enter opened %T", m.overlay)
	}
	if len(m.schema.expanded) != 1 {
		t.Errorf("expanded = %v, want the table", m.schema.expanded)
	}
}

func TestEnterInTheEditorIsStillANewLine(t *testing.T) {
	m := copyModel(t, oneRowOf("x"), &fakeSys{})
	m, _ = send(t, m, tab)
	m, _ = send(t, m, tab)
	m.SetQuery("a")
	m, _ = send(t, m, enter)
	if m.overlay != nil || m.editor.Value() != "a\n" {
		t.Errorf("overlay %T, editor %q", m.overlay, m.editor.Value())
	}
}

func TestTheValueViewFitsVerySmallWindowsAndAResizeReWrapsIt(t *testing.T) {
	long := strings.Repeat("abcdefghij ", 200)
	m := openedValueView(t, &fakeSys{}, long)
	for _, size := range [][2]int{
		{9, minWindowHeight},
		{10, minWindowHeight + 1},
		{60, 12},
		{200, 50},
	} {
		m, _ = send(t, m, tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		fits(t, m)
		m, _ = send(t, m, keyEnd)
		fits(t, m)
		if got := valueBody(t, m); len(got) == 0 {
			t.Errorf("%dx%d: nothing of the value shows", size[0], size[1])
		}
	}

	for _, size := range [][2]int{{minWindowWidth, minWindowHeight}, {1, 1}, {0, 0}, {1, 40}} {
		m, _ = send(t, m, tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		m.View()
		m, _ = send(t, m, keyDown)
		m.View()
	}

	m, _ = send(t, m, tea.WindowSizeMsg{Width: 200, Height: 50})
	wide := len(valueBody(t, m)[0])
	m, _ = send(t, m, keyHome)
	m, _ = send(t, m, tea.WindowSizeMsg{Width: 40, Height: 50})
	if narrow := len(valueBody(t, m)[0]); narrow >= wide || narrow > 40 {
		t.Errorf("a line was %d cells wide at 200 and is %d at 40", wide, narrow)
	}
}

func TestAKeyAfterAResizeMovesFromWhatIsOnScreen(t *testing.T) {
	var b strings.Builder
	for i := range 200 {
		fmt.Fprintf(&b, "line%03d\n", i)
	}
	m := openedValueView(t, &fakeSys{}, strings.TrimSuffix(b.String(), "\n"))
	m, _ = send(t, m, keyEnd)
	m, _ = send(t, m, tea.WindowSizeMsg{Width: 100, Height: 60})
	before := valueBody(t, m)
	if before[len(before)-1] != "line199" {
		t.Fatalf("the last line is %q, want the end of the text", before[len(before)-1])
	}
	m, _ = send(t, m, keyUp)
	after := valueBody(t, m)
	if after[0] >= before[0] {
		t.Errorf("up left the first line at %q, from %q", after[0], before[0])
	}
}

func TestOnlyJSONsOwnWhiteSpaceIsLookedPast(t *testing.T) {
	for _, value := range []string{" [1,2] ", " {\"a\":1}"} {
		m := openedValueView(t, &fakeSys{}, value)
		got := strings.Join(valueBody(t, m), "\n")
		if got != value {
			t.Errorf("%q shows as %q", value, got)
		}
	}
}

func TestJSONNestedTooDeeplyIsShownAsItIsAndQuickly(t *testing.T) {
	deep := strings.Repeat("[", 10000) + strings.Repeat("]", 10000)
	done := make(chan Model, 1)
	go func() { done <- openedValueView(t, &fakeSys{}, deep) }()
	var m Model
	select {
	case m = <-done:
	case <-time.After(time.Second):
		t.Fatal("opening it took over a second, so indenting it blew up")
	}
	if got := strings.Join(valueBody(t, m), ""); !strings.HasPrefix(got, "[[[[") {
		t.Errorf("body starts %.20q, want it as stored", got)
	}

	inString := `{"a":"` + strings.Repeat("[", 500) + `"}`
	m = openedValueView(t, &fakeSys{}, inString)
	if got := valueBody(t, m); got[0] != "{" {
		t.Errorf("brackets in a string counted as depth: %.40q", got[0])
	}
}

func TestAListOrStructIsIndentedAndIsJSONInTheGrid(t *testing.T) {
	value := map[string]any{"list": []any{int32(1), nil}, "name": "a\x1b[31mb"}
	if got := formatValue(value); strings.Contains(got, "\x1b") || !strings.Contains(got, `"list":[1,null]`) {
		t.Errorf("the grid shows %q", got)
	}
	m := copyModel(t, oneRowOf(value), &fakeSys{})
	m, _ = send(t, m, enter)
	view := m.View()
	if strings.Contains(view, "\x1b[31m") {
		t.Error("an escape reached the view")
	}
	for _, want := range []string{`"list": [`, `null`} {
		if !strings.Contains(view, want) {
			t.Errorf("the Value view does not indent it: no %q in %q", want, view)
		}
	}
}

func TestAValueOfAnyOtherTypeIsMadeHarmless(t *testing.T) {
	m := copyModel(t, oneRowOf(errors.New("a\x1b[31mb")), &fakeSys{})
	m, _ = send(t, m, enter)
	if view := m.View(); strings.Contains(view, "\x1b[31m") {
		t.Errorf("an escape reached the terminal:\n%q", view)
	}
	if !strings.Contains(plain(m), "a·[31mb") {
		t.Errorf("view:\n%s", plain(m))
	}
}

func TestAWrapDoesNotSplitAJoinedCharacter(t *testing.T) {
	const family = "\U0001F469‍\U0001F469‍\U0001F467"
	m := openedValueView(t, &fakeSys{}, strings.Repeat(family, 100))
	m, _ = send(t, m, tea.WindowSizeMsg{Width: 21, Height: 60})
	body := valueBody(t, m)
	if len(body) < 2 {
		t.Fatalf("the value did not wrap: %q", body)
	}
	for _, line := range body {
		if strings.ReplaceAll(line, family, "") != "" {
			t.Errorf("a line holds part of a joined character: %q", line)
		}
	}
}

func TestALineTooWideForTheWindowIsCutWhenDrawn(t *testing.T) {
	v := newValueView("世世", "c", 0, 1, 1)
	for _, line := range strings.Split(v.view(1, 5), "\n") {
		if w := ansi.StringWidth(ansi.Strip(line)); w > 1 {
			t.Errorf("a line is %d cells wide in a window of 1: %q", w, line)
		}
	}
}

func TestTheValueViewUsesTheTitleStyleOfTheOtherOverlays(t *testing.T) {
	withColour(t)
	m := openedValueView(t, &fakeSys{}, "x")
	if want := titleStyle.Render("c0, row 1 of 1"); !strings.Contains(m.View(), want) {
		t.Errorf("the title is not drawn in the title style:\n%q", m.View())
	}
}

func TestTheMouseDoesNothingWhileItIsOpen(t *testing.T) {
	m := openedValueView(t, &fakeSys{}, "value")
	before := plain(m)
	x, y := gridCellAt(0, 0)
	m = mouse(t, m, press(x, y), release(x, y), wheel(x, y, false))
	if m.overlay == nil || plain(m) != before {
		t.Errorf("the mouse reached the Value view:\n%s", plain(m))
	}
}

func TestCtrlQQuitsAheadOfTheValueView(t *testing.T) {
	m := openedValueView(t, &fakeSys{}, "value")
	_, cmd := send(t, m, tea.KeyMsg{Type: tea.KeyCtrlQ})
	if cmd == nil {
		t.Fatal("ctrl+q did nothing")
	}
	if _, quit := cmd().(tea.QuitMsg); !quit {
		t.Error("ctrl+q did not quit")
	}
}

func TestTheValueViewsKeysAreInTheHintRow(t *testing.T) {
	m := copyModel(t, oneRowOf("value"), &fakeSys{})
	rows := lines(plain(m))
	if hint := rows[len(rows)-1]; !strings.Contains(hint, "enter view") {
		t.Errorf("grid hints = %q", hint)
	}
	m, _ = send(t, m, enter)
	rows = lines(plain(m))
	hint := rows[len(rows)-1]
	for _, want := range []string{"↑↓ scroll", "c copy", "esc close"} {
		if !strings.Contains(hint, want) {
			t.Errorf("Value view hints = %q, want %q", hint, want)
		}
	}
}
