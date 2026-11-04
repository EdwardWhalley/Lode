package tui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/EdwardWhalley/lode/internal/editor"
)

func TestClickInTheEditorPlacesTheCursorAndFocusesIt(t *testing.T) {
	m := mouseModel(t, "select 1\nfrom people")
	m = m.toggleFocus()
	x, y := edCell(5, 1)
	m = mouse(t, m, press(x, y), release(x, y))

	if m.focus != focusEditor {
		t.Errorf("focus = %v, want the editor", m.focus)
	}
	if r, c := cursorAt(m); r != 1 || c != 5 {
		t.Errorf("cursor at %d,%d, want 1,5", r, c)
	}
	if m.editor.HasSelection() {
		t.Errorf("a click selected %q", m.editor.SelectedText())
	}
}

func TestDragSelectsInTheEditor(t *testing.T) {
	m := mouseModel(t, "select 1\nfrom people")
	x0, y0 := edCell(7, 0)
	x1, y1 := edCell(4, 1)
	m = mouse(t, m, press(x0, y0), motion(x0+1, y0), motion(x1, y1), release(x1, y1))

	if got := m.editor.SelectedText(); got != "1\nfrom" {
		t.Errorf("selected %q, want %q", got, "1\nfrom")
	}
	m = mouse(t, m, motion(editorLeft+promptCols, editorTop))
	if got := m.editor.SelectedText(); got != "1\nfrom" {
		t.Errorf("a motion after the release changed the Selection to %q", got)
	}
}

func TestMotionWithoutAPressInTheEditorDoesNothing(t *testing.T) {
	m := mouseModel(t, "select 1")
	x, y := edCell(3, 0)
	m = mouse(t, m, motion(x, y))
	if m.editor.HasSelection() {
		t.Error("a motion with no press made a Selection")
	}
	m = mouse(t, m, press(editorLeft+2, gridHeader+2), motion(x, y))
	if m.editor.HasSelection() {
		t.Error("a drag begun in the grid made a Selection in the editor")
	}
}

func TestClicksCountAsDoubleAndTripleOnlyWhenQuickAndOnTheSameCell(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	m := mouseModel(t, "select * from people")
	m.now = func() time.Time { return now }
	x, y := edCell(10, 0)
	click := func(m Model, after time.Duration, x, y int) Model {
		now = now.Add(after)
		return mouse(t, m, press(x, y), release(x, y))
	}

	m = click(m, 0, x, y)
	if m.editor.HasSelection() {
		t.Fatal("one click selected")
	}
	m = click(m, 100*time.Millisecond, x, y)
	if got := m.editor.SelectedText(); got != "from" {
		t.Errorf("double-click selected %q, want %q", got, "from")
	}
	m = click(m, 100*time.Millisecond, x, y)
	if got := m.editor.SelectedText(); got != "select * from people" {
		t.Errorf("triple-click selected %q, want the line", got)
	}
	m = click(m, 100*time.Millisecond, x, y)
	if m.editor.HasSelection() {
		t.Errorf("a fourth click selected %q, want a plain click", m.editor.SelectedText())
	}

	m = click(m, 2*time.Second, x, y)
	m = click(m, 2*time.Second, x, y)
	if m.editor.HasSelection() {
		t.Errorf("two slow clicks selected %q", m.editor.SelectedText())
	}
	m = click(m, 50*time.Millisecond, x+1, y)
	m = click(m, 50*time.Millisecond, x+3, y)
	if m.editor.HasSelection() {
		t.Errorf("clicks on different cells selected %q", m.editor.SelectedText())
	}
}

func TestShiftClickExtendsFromTheCursor(t *testing.T) {
	m := mouseModel(t, "select 1\nfrom people")
	x0, y0 := edCell(2, 0)
	x1, y1 := edCell(4, 1)
	m = mouse(t, m, press(x0, y0), release(x0, y0))
	shift := press(x1, y1)
	shift.Shift = true
	m = mouse(t, m, shift, release(x1, y1))

	if got := m.editor.SelectedText(); got != "lect 1\nfrom" {
		t.Errorf("selected %q, want %q", got, "lect 1\nfrom")
	}
}

func TestDraggingBelowTheEditorScrollsItAndKeepsExtending(t *testing.T) {
	m := mouseModel(t, numberedLines(20))
	m.editor.ScrollView(-100)
	x, y := edCell(0, 0)
	m = mouse(t, m, press(x, y), motion(x, dividerRow), motion(x, dividerRow))
	if first := firstEditorRow(m); !strings.HasSuffix(first, "line02") {
		t.Errorf("first drawn line = %q, want line02 (scrolled one row an event)", first)
	}
	if got := m.editor.SelectedText(); got != "line00\nline01\nline02\nline03\nline04\nline05\n" {
		t.Errorf("selected %q, want line00 up to the start of line06, the bottom drawn row", got)
	}
}

func TestTheWheelScrollsTheEditorViewWithoutMovingTheCursor(t *testing.T) {
	m := mouseModel(t, numberedLines(20))
	m.editor.ScrollView(-100)
	r0, c0 := cursorAt(m)
	x, y := edCell(2, 2)
	m = mouse(t, m, wheel(x, y, false))
	if first := firstEditorRow(m); !strings.HasSuffix(first, "line03") {
		t.Errorf("first drawn line = %q, want line03 (3 rows a notch)", first)
	}
	if r, c := cursorAt(m); r != r0 || c != c0 {
		t.Errorf("the wheel moved the cursor from %d,%d to %d,%d", r0, c0, r, c)
	}
	if m.focus != focusEditor {
		t.Errorf("focus = %v", m.focus)
	}
}

func TestTheWheelScrollsThePaneUnderThePointerNotTheFocusedOne(t *testing.T) {
	m := mouseModel(t, "select 1")

	m = mouse(t, m, wheel(editorLeft+3, gridHeader+3, false))
	if m.grid.rowOff != 3 {
		t.Errorf("grid rowOff = %d after one notch down, want 3", m.grid.rowOff)
	}
	m = mouse(t, m, wheel(editorLeft+3, gridHeader+3, false), wheel(editorLeft+3, gridHeader+3, true))
	if m.grid.rowOff != 3 {
		t.Errorf("grid rowOff = %d after down, down, up, want 3", m.grid.rowOff)
	}
	if m.focus != focusEditor {
		t.Errorf("scrolling the grid moved the focus to %v", m.focus)
	}

	m = mouse(t, m, wheel(5, treeTop+4, false))
	if m.schema.off != 3 {
		t.Errorf("tree off = %d after one notch down, want 3", m.schema.off)
	}
	if m.schema.cursor < m.schema.off || m.schema.cursor >= m.schema.off+m.schema.height {
		t.Errorf("tree cursor %d left the window starting at %d", m.schema.cursor, m.schema.off)
	}
	if m.grid.rowOff != 3 {
		t.Errorf("the wheel over the tree scrolled the grid to %d", m.grid.rowOff)
	}

	m = m.toggleFocus().toggleFocus()
	m = mouse(t, m, wheel(editorLeft+3, gridHeader+3, false))
	if m.grid.rowOff != 6 {
		t.Errorf("grid rowOff = %d, want 6 while the tree has the focus", m.grid.rowOff)
	}
}

func TestTheWheelStopsAtTheEndsOfTheGridAndTree(t *testing.T) {
	m := mouseModel(t, "select 1")
	for range 100 {
		m = mouse(t, m, wheel(editorLeft+3, gridHeader+3, false), wheel(5, treeTop+3, false))
	}
	if want := len(m.grid.rows) - m.grid.bodyHeight(); m.grid.rowOff != want {
		t.Errorf("grid rowOff = %d, want %d", m.grid.rowOff, want)
	}
	if want := len(m.schema.items()) - m.schema.height; m.schema.off != want {
		t.Errorf("tree off = %d, want %d", m.schema.off, want)
	}
	for range 100 {
		m = mouse(t, m, wheel(editorLeft+3, gridHeader+3, true), wheel(5, treeTop+3, true))
	}
	if m.grid.rowOff != 0 || m.schema.off != 0 {
		t.Errorf("after scrolling back, grid rowOff = %d and tree off = %d", m.grid.rowOff, m.schema.off)
	}
}

func TestAClickOnAPaneFocusesItAndMovesNoWindow(t *testing.T) {
	m := mouseModel(t, "select 1")
	m = mouse(t, m, wheel(editorLeft+3, gridHeader+3, false), wheel(5, treeTop+4, false))
	rowOff, treeOff, treeCursor := m.grid.rowOff, m.schema.off, m.schema.cursor

	m = mouse(t, m, press(editorLeft+3, gridHeader+5), release(editorLeft+3, gridHeader+5))
	if m.focus != focusGrid {
		t.Errorf("focus = %v after a click on the grid", m.focus)
	}
	m = mouse(t, m, press(5, treeTop+6), release(5, treeTop+6))
	if m.focus != focusSchema {
		t.Errorf("focus = %v after a click on the tree", m.focus)
	}
	if m.grid.rowOff != rowOff || m.schema.off != treeOff || m.schema.cursor != treeCursor {
		t.Errorf("a click moved the panes: grid %d, tree %d/%d; was %d, %d/%d", m.grid.rowOff, m.schema.off, m.schema.cursor, rowOff, treeOff, treeCursor)
	}
	if len(m.schema.expanded) != 0 {
		t.Errorf("a click expanded a table: %v", m.schema.expanded)
	}
	x, y := edCell(2, 0)
	m = mouse(t, m, press(x, y))
	if m.focus != focusEditor {
		t.Errorf("focus = %v after a click on the editor", m.focus)
	}
}

func TestClicksOutsideThePanesChangeNothing(t *testing.T) {
	m := mouseModel(t, "select 1")
	before := m.View()
	for _, at := range [][2]int{{10, 0}, {30, 10}, {editorLeft + 5, dividerRow}, {10, 28}, {10, 29}} {
		m = mouse(t, m, press(at[0], at[1]), release(at[0], at[1]))
		if m.focus != focusEditor {
			t.Errorf("a click at %v moved the focus to %v", at, m.focus)
		}
	}
	if m.View() != before {
		t.Error("a click outside the panes changed the screen")
	}
}

func TestOnlyTheLeftButtonDoesAnything(t *testing.T) {
	m := mouseModel(t, "select 1")
	x, y := edCell(3, 0)
	for _, b := range []tea.MouseButton{tea.MouseButtonRight, tea.MouseButtonMiddle} {
		m = mouse(t, m, tea.MouseMsg{X: x, Y: y, Action: tea.MouseActionPress, Button: b})
	}
	if _, c := cursorAt(m); c != len("select 1") {
		t.Errorf("a right or middle click moved the cursor to column %d", c)
	}
}

func TestWithAnOverlayOpenTheMouseChangesNothing(t *testing.T) {
	for name, open := range map[string]tea.KeyMsg{
		"help":     {Type: tea.KeyF1},
		"selector": {Type: tea.KeyCtrlO},
		"export":   {Type: tea.KeyCtrlS},
	} {
		t.Run(name, func(t *testing.T) {
			m := mouseModel(t, "select 1\nfrom people")
			m.switcher = newWorld().sw
			m, _ = send(t, m, open)
			if m.overlay == nil {
				t.Fatal("the overlay did not open")
			}
			before := m.View()
			rowOff, off := m.grid.rowOff, m.schema.off
			x, y := edCell(2, 0)
			m = mouse(t, m, press(x, y), motion(x+3, y), release(x+3, y),
				wheel(editorLeft+3, gridHeader+3, false), wheel(5, treeTop+3, false),
				press(editorLeft+3, gridHeader+5), press(5, treeTop+6))
			if m.overlay == nil {
				t.Error("the mouse closed the overlay")
			}
			if m.focus != focusEditor || m.editor.HasSelection() || m.grid.rowOff != rowOff || m.schema.off != off {
				t.Errorf("the mouse reached behind the overlay: focus %v, selected %q, grid %d, tree %d", m.focus, m.editor.SelectedText(), m.grid.rowOff, m.schema.off)
			}
			if m.View() != before {
				t.Error("the mouse changed what the overlay shows")
			}
			m, _ = send(t, m, tea.KeyMsg{Type: tea.KeyEsc})
			m = mouse(t, m, motion(x+5, y))
			if m.editor.HasSelection() {
				t.Error("a motion after the overlay closed extended a Selection that began under it")
			}
		})
	}
}

func TestAClickInTheEditorOrOnAPaneClosesTheDropdown(t *testing.T) {
	open := func() Model {
		m, _ := completeModel(t, schemaFixture(), "", -1)
		m = typeText(t, m, "SELECT * FROM ord")
		if !m.complete.active() {
			t.Fatal("the dropdown did not open")
		}
		return m
	}
	x, y := edCell(2, 0)

	if m := mouse(t, open(), press(x, y)); m.complete.active() {
		t.Errorf("a click in the editor left the dropdown open (%s)", labels(m))
	}
	if m := mouse(t, open(), press(editorLeft+3, gridHeader+3)); m.complete.active() {
		t.Errorf("a click on the grid left the dropdown open (%s)", labels(m))
	}
	if m := mouse(t, open(), press(x, y), motion(x+2, y)); m.complete.active() {
		t.Errorf("a drag left the dropdown open (%s)", labels(m))
	}
}

func TestTheLegendHasAMouseGroup(t *testing.T) {
	names, byGroup := groupedBindings()
	found := false
	for _, n := range names {
		found = found || n == "Mouse"
	}
	if !found {
		t.Fatalf("groups %v have no Mouse", names)
	}
	var keys []string
	for _, b := range byGroup["Mouse"] {
		keys = append(keys, b.keys)
	}
	all := strings.Join(keys, "|")
	for _, gesture := range []string{"click", "drag", "double-click", "triple-click", "shift+click", "wheel"} {
		if !strings.Contains(all, gesture) {
			t.Errorf("the Mouse group has no %q (it has %v)", gesture, keys)
		}
	}
	help := strings.Join(helpLines(200), "\n")
	if !strings.Contains(help, "Mouse") || !strings.Contains(help, "triple-click") {
		t.Errorf("F1 does not show the Mouse group:\n%s", help)
	}
}

func TestTheReadmeSaysHowToTurnTheMouseOffAndAboutNativeSelection(t *testing.T) {
	readme := readmeText(t)
	for _, want := range []string{"mouse = false", "--no-mouse", "on by default", "Shift+drag", "Windows Terminal"} {
		if !strings.Contains(readme, want) {
			t.Errorf("README does not mention %q", want)
		}
	}
}

func TestAClickOnAnotherPaneBreaksADoubleClick(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	m := mouseModel(t, "select one two")
	m.now = func() time.Time { return now }
	x, y := edCell(2, 0)

	m = mouse(t, m, press(x, y), release(x, y))
	m = mouse(t, m, press(3, treeTop), release(3, treeTop))
	m = mouse(t, m, press(x, y), release(x, y))
	if m.editor.HasSelection() {
		t.Errorf("selected %q: the click on the tree between did not break the run", m.editor.SelectedText())
	}
}

func TestSettingTheQueryKeepsTheCursorInViewAfterABlink(t *testing.T) {
	m, _ := completeModel(t, schemaFixture(), "", -1)
	m.SetQuery(numberedLines(12))
	_ = m.View()
	m, _ = send(t, m, editor.Blink())
	if first := firstEditorRow(m); !strings.HasSuffix(first, "line07") {
		t.Errorf("first drawn line = %q, want line07 (the cursor on line11 in view)", first)
	}
}
