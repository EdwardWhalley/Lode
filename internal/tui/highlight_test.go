package tui

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/EdwardWhalley/lode/internal/theme"
)

var (
	keyUp    = tea.KeyMsg{Type: tea.KeyUp}
	keyDown  = tea.KeyMsg{Type: tea.KeyDown}
	keyLeft  = tea.KeyMsg{Type: tea.KeyLeft}
	keyRight = tea.KeyMsg{Type: tea.KeyRight}
	keyPgUp  = tea.KeyMsg{Type: tea.KeyPgUp}
	keyPgDn  = tea.KeyMsg{Type: tea.KeyPgDown}
	keyHome  = tea.KeyMsg{Type: tea.KeyHome}
	keyEnd   = tea.KeyMsg{Type: tea.KeyEnd}
)

// cellGrid is a 100 x 30 terminal, grid focused, on a result whose cells name themselves:
// r0c0, r0c1 and so on.
func cellGrid(t *testing.T, rows, cols int) Model {
	t.Helper()
	withColour(t)
	m := newBare(t, 100, 30)
	m, _ = send(t, m, cellResult(m, rows, cols))
	m, _ = send(t, m, tab)
	return m
}

func cellResult(m Model, rows, cols int) resultMsg {
	names := make([]string, cols)
	for c := range names {
		names[c] = fmt.Sprintf("col%-5d", c)
	}
	cells := make([][]string, rows)
	for r := range cells {
		cells[r] = make([]string, cols)
		for c := range cells[r] {
			cells[r][c] = fmt.Sprintf("r%dc%-*d", r, 8-2-len(fmt.Sprint(r)), c)
		}
	}
	return resultMsg{id: m.query.id, cols: names, rows: cells}
}

// highlighted is every stretch of the view drawn in the highlight style, padding trimmed.
func highlighted(m Model) []string {
	view := m.View()
	sample := highlightStyle.Render("x")
	open, shut, _ := strings.Cut(sample, "x")
	if open == "" {
		return nil
	}
	var out []string
	for {
		_, rest, ok := strings.Cut(view, open)
		if !ok {
			return out
		}
		text, rest, _ := strings.Cut(rest, shut)
		out = append(out, strings.TrimSpace(text))
		view = rest
	}
}

func wantHighlight(t *testing.T, m Model, want string) {
	t.Helper()
	got := highlighted(m)
	if len(got) != 1 || got[0] != want {
		t.Errorf("highlighted %q, want only %q\n%s", got, want, plain(m))
	}
}

func keys(t *testing.T, m Model, msgs ...tea.KeyMsg) Model {
	t.Helper()
	for _, msg := range msgs {
		m, _ = send(t, m, msg)
	}
	return m
}

func repeat(msg tea.KeyMsg, n int) []tea.KeyMsg {
	out := make([]tea.KeyMsg, n)
	for i := range out {
		out[i] = msg
	}
	return out
}

func TestANewResultStartsOnItsFirstCell(t *testing.T) {
	m := cellGrid(t, 100, 30)
	wantHighlight(t, m, "r0c0")

	m = keys(t, m, keyDown, keyRight)
	wantHighlight(t, m, "r1c1")

	m, _ = send(t, m, cellResult(m, 5, 5))
	wantHighlight(t, m, "r0c0")
}

func TestAResultWithNoRowsHasNoHighlight(t *testing.T) {
	m := cellGrid(t, 0, 3)
	if got := highlighted(m); len(got) != 0 {
		t.Errorf("highlighted %q with no rows", got)
	}
	m = keys(t, m, keyDown, keyRight, keyEnd)
	if got := highlighted(m); len(got) != 0 {
		t.Errorf("highlighted %q with no rows, after keys", got)
	}
}

func TestTheSidewaysKeysStillScrollAResultWithNoRows(t *testing.T) {
	m := cellGrid(t, 0, 30)
	if strings.Contains(plain(m), "col20") {
		t.Fatal("setup: col20 is already on screen")
	}
	m = keys(t, m, repeat(keyRight, 20)...)
	if !strings.Contains(plain(m), "col20") {
		t.Errorf("col20 did not come into view:\n%s", plain(m))
	}
}

func TestTheKeysMoveTheHighlightOneCell(t *testing.T) {
	m := cellGrid(t, 100, 30)
	for _, step := range []struct {
		key  tea.KeyMsg
		want string
	}{
		{keyDown, "r1c0"}, {keyRight, "r1c1"}, {keyUp, "r0c1"}, {keyLeft, "r0c0"},
		{keyRunes("j"), "r1c0"}, {keyRunes("l"), "r1c1"}, {keyRunes("k"), "r0c1"}, {keyRunes("h"), "r0c0"},
	} {
		m = keys(t, m, step.key)
		wantHighlight(t, m, step.want)
	}
}

func TestTheHighlightStopsAtTheEdges(t *testing.T) {
	m := cellGrid(t, 3, 3)
	m = keys(t, m, keyUp, keyLeft)
	wantHighlight(t, m, "r0c0")
	m = keys(t, m, repeat(keyDown, 9)...)
	m = keys(t, m, repeat(keyRight, 9)...)
	wantHighlight(t, m, "r2c2")
}

func TestPageKeysMoveAPageAndHomeAndEndStayInTheColumn(t *testing.T) {
	m := cellGrid(t, 100, 30)
	m = keys(t, m, keyRight, keyRight)
	page := m.layout.gridH - 2

	m = keys(t, m, keyPgDn)
	wantHighlight(t, m, fmt.Sprintf("r%dc2", page))
	m = keys(t, m, keyPgDn)
	wantHighlight(t, m, fmt.Sprintf("r%dc2", 2*page))
	m = keys(t, m, keyPgUp)
	wantHighlight(t, m, fmt.Sprintf("r%dc2", page))
	m = keys(t, m, keyEnd)
	wantHighlight(t, m, "r99c2")
	m = keys(t, m, keyPgDn)
	wantHighlight(t, m, "r99c2")
	m = keys(t, m, keyHome)
	wantHighlight(t, m, "r0c2")
	m = keys(t, m, keyPgUp)
	wantHighlight(t, m, "r0c2")
}

func TestTheViewFollowsTheHighlightInBothDirections(t *testing.T) {
	m := cellGrid(t, 100, 30)
	onScreen := func(want string) {
		t.Helper()
		if !strings.Contains(plain(m), want) {
			t.Errorf("%s is not on screen:\n%s", want, plain(m))
		}
		wantHighlight(t, m, want)
	}

	m = keys(t, m, repeat(keyDown, 60)...)
	onScreen("r60c0")
	m = keys(t, m, repeat(keyRight, 25)...)
	onScreen("r60c25")
	m = keys(t, m, repeat(keyUp, 50)...)
	onScreen("r10c25")
	m = keys(t, m, repeat(keyLeft, 20)...)
	onScreen("r10c5")
	m = keys(t, m, keyEnd)
	onScreen("r99c5")
	m = keys(t, m, keyHome)
	onScreen("r0c5")
}

func TestAColumnWiderThanTheGridCanBeReached(t *testing.T) {
	withColour(t)
	m := newBare(t, 40, 30)
	long := strings.Repeat("a", 60)
	m, _ = send(t, m, resultMsg{id: m.query.id, cols: []string{"one", "two", "three"}, rows: [][]string{{"x", long, "z"}}})
	m, _ = send(t, m, tab)
	m = keys(t, m, keyRight)
	if got := highlighted(m); len(got) != 1 || !strings.HasPrefix(got[0], "aaa") {
		t.Errorf("highlighted %q, want the long cell", got)
	}
	m = keys(t, m, keyRight)
	wantHighlight(t, m, "z")
	fits(t, m)
}

// gridCellAt is the screen cell of a cell of cellGrid: each column is 8 wide with a gap of
// 2.
func gridCellAt(column, row int) (x, y int) { return editorLeft + column*10, gridHeader + 1 + row }

func TestAClickGivesTheGridTheFocusAndMovesTheHighlight(t *testing.T) {
	m := cellGrid(t, 100, 30)
	m, _ = send(t, m, tab)
	m, _ = send(t, m, tab)
	if m.focus != focusEditor {
		t.Fatalf("focus = %v", m.focus)
	}
	x, y := gridCellAt(2, 3)
	m = mouse(t, m, press(x, y), release(x, y))
	if m.focus != focusGrid {
		t.Errorf("focus = %v, want the grid", m.focus)
	}
	wantHighlight(t, m, "r3c2")
}

func TestAClickLandsOnTheRightCellWithTheWindowScrolledBothWays(t *testing.T) {
	m := cellGrid(t, 100, 30)
	m = keys(t, m, repeat(keyDown, 60)...)
	m = keys(t, m, repeat(keyRight, 25)...)

	row, col := -1, -1
	for y, line := range lines(plain(m)) {
		if at := strings.Index(line, "r60c24"); at >= 0 {
			row, col = y, ansi.StringWidth(line[:at])
		}
	}
	if row < 0 {
		t.Fatalf("r60c24 is not on screen:\n%s", plain(m))
	}
	m = mouse(t, m, press(col, row))
	wantHighlight(t, m, "r60c24")
}

func TestTheGapBetweenTwoColumnsBelongsToTheOneOnItsLeft(t *testing.T) {
	m := cellGrid(t, 5, 5)
	x, y := gridCellAt(1, 0)
	for _, c := range []struct {
		x    int
		want string
	}{
		{x, "r0c1"},
		{x + 7, "r0c1"},
		{x + 8, "r0c1"},
		{x + 9, "r0c1"},
		{x + 10, "r0c2"},
	} {
		m = mouse(t, m, press(c.x, y))
		wantHighlight(t, m, c.want)
	}
}

func TestAClickCountsCellsOnWideCharactersAndACutValue(t *testing.T) {
	withColour(t)
	m := newBare(t, 200, 30)
	wide := "世世世世"
	long := strings.Repeat("a", 60)
	m, _ = send(t, m, resultMsg{id: m.query.id, cols: []string{"w", "l", "after"}, rows: [][]string{{wide, long, "z"}}})
	m, _ = send(t, m, tab)

	left := m.layout.mainLeft()
	y := gridHeader + 1
	m = mouse(t, m, press(left+7, y))
	wantHighlight(t, m, wide)
	m = mouse(t, m, press(left+10+maxColWidth-1, y))
	if got := highlighted(m); len(got) != 1 || !strings.HasSuffix(got[0], "…") {
		t.Errorf("highlighted %q, want the cut value", got)
	}
	m = mouse(t, m, press(left+10+maxColWidth+2, y))
	wantHighlight(t, m, "z")
}

func TestAClickOnTheHeaderOrBelowTheLastRowOnlyTakesTheFocus(t *testing.T) {
	m := cellGrid(t, 3, 3)
	m = keys(t, m, keyDown, keyRight)
	for _, c := range []struct {
		name string
		x, y int
	}{
		{"the header", editorLeft + 20, gridHeader},
		{"below the last row", editorLeft + 20, gridHeader + 1 + 3},
		{"past the last column", editorLeft + 35, gridHeader + 1},
	} {
		t.Run(c.name, func(t *testing.T) {
			before, _ := send(t, m, tab)
			before, _ = send(t, before, tab)
			after := mouse(t, before, press(c.x, c.y))
			if after.focus != focusGrid {
				t.Errorf("focus = %v, want the grid", after.focus)
			}
			wantHighlight(t, after, "r1c1")
		})
	}
}

func TestAPressOnABorderStillResizesAndDoesNotMoveTheHighlight(t *testing.T) {
	m := cellGrid(t, 100, 30)
	m = keys(t, m, keyDown, keyRight)
	before := m.layout.editorH
	m = mouse(t, m, press(editorLeft+10, dividerRow), motion(editorLeft+10, dividerRow+2), release(editorLeft+10, dividerRow+2))
	if m.layout.editorH != before+2 {
		t.Errorf("editor height = %d, want %d", m.layout.editorH, before+2)
	}
	wantHighlight(t, m, "r1c1")
}

func TestADoubleClickOpensNothing(t *testing.T) {
	m := cellGrid(t, 3, 3)
	x, y := gridCellAt(1, 1)
	m = mouse(t, m, press(x, y), release(x, y), press(x, y), release(x, y))
	if m.overlay != nil {
		t.Errorf("a double-click opened %T", m.overlay)
	}
	wantHighlight(t, m, "r1c1")
}

func TestTheWheelLeavesTheHighlightAndTheNextKeyBringsItBack(t *testing.T) {
	m := cellGrid(t, 100, 3)
	x, y := gridCellAt(0, 0)
	for range 10 {
		m = mouse(t, m, wheel(x, y, false))
	}
	if got := highlighted(m); len(got) != 0 {
		t.Errorf("highlighted %q with the Highlight scrolled off", got)
	}
	if strings.Contains(plain(m), "r0c0") {
		t.Fatalf("the wheel did not scroll:\n%s", plain(m))
	}

	m = keys(t, m, keyRunes("x"))
	if strings.Contains(plain(m), "r0c0") {
		t.Errorf("a key that moves nothing brought the Highlight back")
	}

	m = keys(t, m, keyDown)
	wantHighlight(t, m, "r1c0")
	if !strings.Contains(plain(m), "r1c0") {
		t.Errorf("the next key did not bring the Highlight into view:\n%s", plain(m))
	}
}

func TestAClickAfterTheWheelLandsOnTheCellUnderThePointer(t *testing.T) {
	m := cellGrid(t, 100, 3)
	x, y := gridCellAt(1, 0)
	m = mouse(t, m, wheel(x, y, false), wheel(x, y, false))
	m = mouse(t, m, press(x, y))
	wantHighlight(t, m, "r6c1")
}

func TestTheHighlightIsDrawnOnlyWhileTheGridHasTheFocus(t *testing.T) {
	m := cellGrid(t, 3, 3)
	wantHighlight(t, m, "r0c0")
	m, _ = send(t, m, tab)
	if got := highlighted(m); len(got) != 0 {
		t.Errorf("highlighted %q with the tree focused", got)
	}
	m, _ = send(t, m, tab)
	if got := highlighted(m); len(got) != 0 {
		t.Errorf("highlighted %q with the editor focused", got)
	}
	m, _ = send(t, m, tab)
	wantHighlight(t, m, "r0c0")
}

func TestTheHighlightCoversANullAndDrawsInEveryTheme(t *testing.T) {
	withColour(t)
	for _, name := range theme.Names() {
		t.Run(name, func(t *testing.T) {
			m := New(nil, "x", WithTheme(name))
			m, _ = send(t, m, tea.WindowSizeMsg{Width: 100, Height: 30})
			m, _ = send(t, m, resultMsg{id: m.query.id, cols: []string{"a", "b"}, rows: [][]string{{nullText, "x"}}})
			m, _ = send(t, m, tab)
			wantHighlight(t, m, nullText)
			m = keys(t, m, keyRight)
			wantHighlight(t, m, "x")
		})
	}
}

func TestTheHighlightCoversTheCellsPaddingSoAnEmptyValueShows(t *testing.T) {
	withColour(t)
	m := newBare(t, 100, 30)
	m, _ = send(t, m, resultMsg{id: m.query.id, cols: []string{"name"}, rows: [][]string{{""}}})
	m, _ = send(t, m, tab)
	got := highlighted(m)
	if len(got) != 1 {
		t.Fatalf("highlighted %q, want one cell", got)
	}
	sample := highlightStyle.Render("    ")
	if !strings.Contains(m.View(), sample) {
		t.Errorf("the four cells of the column are not highlighted:\n%q", m.View())
	}
}

func TestTheHighlightWorksWithTheDropdownOverTheGrid(t *testing.T) {
	withColour(t)
	m, _ := completeModel(t, schemaFixture(), "SELECT * FROM ", -1)
	m, _ = send(t, m, cellResult(m, 100, 30))
	m = typeText(t, m, "or")
	if !m.complete.active() {
		t.Fatal("setup: the dropdown did not open")
	}
	fits(t, m)

	x, y := gridCellAt(2, 0)
	m = mouse(t, m, press(x, y))
	if m.complete.active() {
		t.Error("the dropdown stayed open")
	}
	if m.focus != focusGrid {
		t.Errorf("focus = %v, want the grid", m.focus)
	}
	wantHighlight(t, m, "r0c2")
}

func TestTheHighlightWorksOnAResultCutAtTheCap(t *testing.T) {
	m := cellGrid(t, 0, 3)
	msg := cellResult(m, 5, 3)
	msg.truncated, msg.maxRows = true, 5
	m, _ = send(t, m, msg)
	m = keys(t, m, keyEnd)
	wantHighlight(t, m, "r4c0")
	if !strings.Contains(plain(m), "truncated at 5 rows") {
		t.Errorf("the result does not say it was cut:\n%s", plain(m))
	}
}

func TestTheHighlightStaysThroughASourceSwitch(t *testing.T) {
	withColour(t)
	w := newWorld()
	m := w.model(t)
	m, _ = send(t, m, cellResult(m, 5, 3))
	m, _ = send(t, m, tab)
	m = keys(t, m, keyDown, keyRight)
	m = sendAndFinish(t, m, switchMsg{choice: w.sw.choices["b"]})
	if m.source != "source-b" {
		t.Fatalf("source = %q", m.source)
	}
	wantHighlight(t, m, "r1c1")
}

func TestAWindowResizeKeepsTheHighlightInView(t *testing.T) {
	m := cellGrid(t, 100, 30)
	m = keys(t, m, repeat(keyDown, 15)...)
	m = keys(t, m, repeat(keyRight, 6)...)
	wantHighlight(t, m, "r15c6")
	m, _ = send(t, m, tea.WindowSizeMsg{Width: 60, Height: 14})
	if !strings.Contains(plain(m), "r15c6") {
		t.Errorf("the Highlight left the screen on a resize:\n%s", plain(m))
	}
	fits(t, m)
}

func TestDraggingTheDividerKeepsTheHighlightInView(t *testing.T) {
	m := cellGrid(t, 100, 3)
	m = keys(t, m, repeat(keyDown, 15)...)
	m = mouse(t, m, press(editorLeft+10, dividerRow), motion(editorLeft+10, dividerRow+10), release(editorLeft+10, dividerRow+10))
	wantHighlight(t, m, "r15c0")
}

func TestAWindowResizeLeavesAHighlightTheWheelScrolledOff(t *testing.T) {
	m := cellGrid(t, 100, 3)
	x, y := gridCellAt(0, 0)
	for range 10 {
		m = mouse(t, m, wheel(x, y, false))
	}
	m, _ = send(t, m, tea.WindowSizeMsg{Width: 90, Height: 26})
	if got := highlighted(m); len(got) != 0 {
		t.Errorf("highlighted %q: the resize brought it back", got)
	}
}

func TestThePositionStillDescribesTheWindow(t *testing.T) {
	m := cellGrid(t, 100, 3)
	if !strings.Contains(plain(m), "rows 1-") {
		t.Errorf("position:\n%s", plain(m))
	}
	m = keys(t, m, keyDown)
	if !strings.Contains(plain(m), "rows 1-") {
		t.Errorf("one row down, on screen, moved the window:\n%s", plain(m))
	}
	m = keys(t, m, keyEnd)
	if !strings.Contains(plain(m), "of 100") || strings.Contains(plain(m), "rows 1-") {
		t.Errorf("position after end:\n%s", plain(m))
	}
}
