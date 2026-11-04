package tui

import (
	"reflect"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/EdwardWhalley/lode/internal/editor"
)

func TestDraggingTheSeparatorSetsTheSchemaPaneWidth(t *testing.T) {
	m := mouseModel(t, "select 1")
	m = mouse(t, m, press(separatorCol, 10), motion(35, 10), motion(40, 12), release(40, 12))

	l := m.layout
	if l.schemaW != 40 {
		t.Errorf("the schema pane is %d columns, want 40", l.schemaW)
	}
	if want := 100 - 41; l.mainW != want || m.grid.width != want {
		t.Errorf("main area %d and grid %d columns, want %d", l.mainW, m.grid.width, want)
	}
	if m.schema.width != 40 {
		t.Errorf("the tree is %d columns, want 40", m.schema.width)
	}
	fits(t, m)
	if m.paneAt(35, 10) != paneSchema || m.paneAt(40, 10) != paneNone || m.paneAt(41, 3) != paneEditor {
		t.Error("paneAt does not follow the separator")
	}
}

func TestTheSeparatorStopsAtTheLimits(t *testing.T) {
	m := mouseModel(t, "select 1")
	m = mouse(t, m, press(separatorCol, 10), motion(3, 10))
	if m.layout.schemaW != minSchemaWidth {
		t.Errorf("dragged to column 3: the schema pane is %d columns, want the minimum %d", m.layout.schemaW, minSchemaWidth)
	}
	m = mouse(t, m, motion(0, 10))
	if m.layout.schemaW != minSchemaWidth {
		t.Errorf("dragged to column 0: the schema pane is %d columns, want the minimum %d", m.layout.schemaW, minSchemaWidth)
	}
	m = mouse(t, m, motion(99, 10))
	if want := 100 - 1 - minMainWidth; m.layout.schemaW != want || m.layout.mainW != minMainWidth {
		t.Errorf("dragged to the right edge: schema %d main %d, want %d and %d", m.layout.schemaW, m.layout.mainW, want, minMainWidth)
	}
	fits(t, m)
}

func TestPressingTheSeparatorMovesNeitherTheFocusNorTheCursorNorASelection(t *testing.T) {
	m := mouseModel(t, "select 1\nfrom people")
	x0, y0 := edCell(1, 0)
	x1, y1 := edCell(4, 1)
	m = mouse(t, m, press(x0, y0), motion(x1, y1), release(x1, y1))
	selected := m.editor.SelectedText()
	if selected == "" {
		t.Fatal("test needs a Selection")
	}
	r0, c0 := cursorAt(m)

	m = m.toggleFocus()
	m = mouse(t, m, press(separatorCol, 10), motion(45, 10), release(45, 10))
	if m.focus != focusGrid {
		t.Errorf("the drag moved the focus to %v", m.focus)
	}
	if r, c := cursorAt(m); r != r0 || c != c0 {
		t.Errorf("the cursor moved from %d,%d to %d,%d", r0, c0, r, c)
	}
	if got := m.editor.SelectedText(); got != selected {
		t.Errorf("the Selection was %q, now %q", selected, got)
	}
	if m.dragging || m.clicks.count != 0 {
		t.Error("a resize left a drag or a run of clicks behind")
	}
}

func TestAPressOnTheEditorStillPlacesTheCursorAfterAResize(t *testing.T) {
	m := mouseModel(t, "select 1\nfrom people")
	m = mouse(t, m, press(separatorCol, 10), motion(40, 10), release(40, 10))
	x, y := m.layout.mainLeft()+promptCols+5, 2
	m = mouse(t, m, press(x, y), release(x, y))
	if r, c := cursorAt(m); r != 1 || c != 5 {
		t.Errorf("cursor at %d,%d, want 1,5", r, c)
	}
}

func TestTheDraggedWidthSurvivesTheWindowChangingSize(t *testing.T) {
	m := mouseModel(t, "select 1")
	m = mouse(t, m, press(separatorCol, 10), motion(40, 10), release(40, 10))

	m, _ = send(t, m, tea.WindowSizeMsg{Width: 50, Height: 30})
	if l := m.layout; l.schemaW < minSchemaWidth || l.mainW < minMainWidth || l.schemaW+1+l.mainW != 50 {
		t.Errorf("in 50 columns: schema %d main %d", l.schemaW, l.mainW)
	}
	m, _ = send(t, m, tea.WindowSizeMsg{Width: 100, Height: 30})
	if m.layout.schemaW != 40 {
		t.Errorf("back at 100 columns the schema pane is %d, want the dragged 40", m.layout.schemaW)
	}
	fits(t, m)
}

func TestMotionAfterTheReleaseMovesNothing(t *testing.T) {
	m := mouseModel(t, "select 1")
	m = mouse(t, m, press(separatorCol, 10), motion(40, 10), release(40, 10), motion(60, 10), motion(20, 10))
	if m.layout.schemaW != 40 {
		t.Errorf("the schema pane is %d columns, want 40", m.layout.schemaW)
	}
}

func TestMotionWithNoButtonEndsAResize(t *testing.T) {
	m := mouseModel(t, "select 1")
	m = mouse(t, m, press(separatorCol, 10), motion(40, 10))
	m = mouse(t, m, tea.MouseMsg{X: 50, Y: 10, Action: tea.MouseActionMotion, Button: tea.MouseButtonNone})
	m = mouse(t, m, motion(60, 10))
	if m.layout.schemaW != 40 {
		t.Errorf("the schema pane is %d columns, want 40", m.layout.schemaW)
	}
}

func TestAPressElsewhereEndsAResize(t *testing.T) {
	m := mouseModel(t, "select 1")
	m = mouse(t, m, press(separatorCol, 10), motion(40, 10))
	m = mouse(t, m, press(5, treeTop+2), motion(60, 10))
	if m.layout.schemaW != 40 {
		t.Errorf("the schema pane is %d columns, want 40", m.layout.schemaW)
	}
}

func TestAPressOnTheSeparatorOutsideTheBodyStartsNothing(t *testing.T) {
	m := mouseModel(t, "select 1")
	for _, y := range []int{0, 28, 29, dividerRow} {
		m = mouse(t, m, press(separatorCol, y), motion(50, y), release(50, y))
		if m.layout.schemaW != 30 {
			t.Errorf("a press on the separator column at row %d resized the schema pane to %d", y, m.layout.schemaW)
		}
	}
}

func TestAnOverlayIgnoresAPressOnTheSeparator(t *testing.T) {
	m := mouseModel(t, "select 1")
	m, _ = send(t, m, tea.KeyMsg{Type: tea.KeyF1})
	m = mouse(t, m, press(separatorCol, 10), motion(50, 10), release(50, 10))
	m, _ = send(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	if m.layout.schemaW != 30 {
		t.Errorf("the schema pane is %d columns after a drag under an overlay", m.layout.schemaW)
	}
	m = mouse(t, m, motion(50, 10))
	if m.layout.schemaW != 30 {
		t.Errorf("a motion after the overlay closed resized the schema pane to %d", m.layout.schemaW)
	}
}

func TestStartingAResizeClosesTheDropdown(t *testing.T) {
	m, _ := completeModel(t, schemaFixture(), "", -1)
	m = typeText(t, m, "SELECT * FROM ord")
	if !m.complete.active() {
		t.Fatal("the dropdown did not open")
	}
	m = mouse(t, m, press(separatorCol, 10))
	if m.complete.active() {
		t.Error("the dropdown stayed open when a resize started")
	}
	if m.focus != focusEditor {
		t.Errorf("focus = %v", m.focus)
	}
}

func TestTheEditorKeepsItsTextCursorAndSelectionThroughAResize(t *testing.T) {
	m := mouseModel(t, "select 1\nfrom people")
	x0, y0 := edCell(1, 0)
	x1, y1 := edCell(4, 1)
	m = mouse(t, m, press(x0, y0), motion(x1, y1), release(x1, y1))
	text, selected := m.editor.Value(), m.editor.SelectedText()
	r0, c0 := cursorAt(m)

	m = mouse(t, m, press(separatorCol, 10), motion(60, 10), motion(12, 10), release(12, 10))
	if m.editor.Value() != text || m.editor.SelectedText() != selected {
		t.Errorf("text %q selected %q after the resize; was %q and %q", m.editor.Value(), m.editor.SelectedText(), text, selected)
	}
	if r, c := cursorAt(m); r != r0 || c != c0 {
		t.Errorf("cursor at %d,%d, was %d,%d", r, c, r0, c0)
	}
}

func TestTheSchemaTreeKeepsItsCursorInViewWhenItIsResized(t *testing.T) {
	m := mouseModel(t, "select 1")
	m = m.toggleFocus().toggleFocus()
	for range 30 {
		m, _ = send(t, m, tea.KeyMsg{Type: tea.KeyDown})
	}
	m = mouse(t, m, press(separatorCol, 10), motion(15, 10), release(15, 10))
	s := m.schema
	if s.cursor < s.off || s.cursor >= s.off+s.height {
		t.Errorf("the tree cursor %d left the window starting at %d of %d rows", s.cursor, s.off, s.height)
	}
}

func TestTheGridKeepsItsPositionInRangeWhenItIsResized(t *testing.T) {
	m := mouseModel(t, "select 1")
	m = mouse(t, m, wheel(editorLeft+3, gridHeader+3, false), wheel(editorLeft+3, gridHeader+3, false))
	rowOff := m.grid.rowOff
	m = mouse(t, m, press(separatorCol, 10), motion(50, 10), release(50, 10))
	if m.grid.rowOff != rowOff {
		t.Errorf("the grid moved from row %d to %d", rowOff, m.grid.rowOff)
	}
}

func TestWithNothingDraggedTheSizesAreTheDefaults(t *testing.T) {
	m := mouseModel(t, "select 1")
	if m.prefs != (sizePrefs{}) || m.layout != newLayout(100, 30, sizePrefs{}) {
		t.Errorf("prefs %+v layout %+v", m.prefs, m.layout)
	}
}

func TestTheLegendHasADragABorderRow(t *testing.T) {
	_, byGroup := groupedBindings()
	found := false
	for _, b := range byGroup["Mouse"] {
		found = found || b.keys == "drag a border"
	}
	if !found {
		t.Error("the Mouse group has no `drag a border` row")
	}
}

// dividerAtRow is the row the divider is on when the editor is rows tall.
func dividerAtRow(rows int) int { return 1 + rows }

func TestDraggingTheDividerSetsTheEditorHeight(t *testing.T) {
	m := mouseModel(t, "select 1")
	m = mouse(t, m, press(50, dividerRow), motion(50, 9), motion(50, dividerAtRow(12)), release(50, dividerAtRow(12)))

	l := m.layout
	if l.editorH != 12 {
		t.Errorf("the editor is %d rows, want 12", l.editorH)
	}
	if want := 27 - 12 - 1; l.gridH != want || m.grid.height != want {
		t.Errorf("the grid is %d rows (layout) and %d (pane), want %d", l.gridH, m.grid.height, want)
	}
	if m.schema.height != 26 {
		t.Errorf("the tree is %d rows, want 26: the body less its heading, unchanged by this drag", m.schema.height)
	}
	if got := len(strings.Split(m.editor.View(), "\n")); got != 12 {
		t.Errorf("the editor draws %d rows, want 12", got)
	}
	fits(t, m)
	if l.schemaW != 30 {
		t.Errorf("the divider drag changed the schema width to %d", l.schemaW)
	}
	if m.paneAt(50, 12) != paneEditor || m.paneAt(50, 13) != paneNone || m.paneAt(50, 14) != paneGrid {
		t.Error("paneAt does not follow the divider")
	}
}

func TestTheDividerStopsAtTheLimits(t *testing.T) {
	m := mouseModel(t, "select 1")
	m = mouse(t, m, press(50, dividerRow), motion(50, 0))
	if m.layout.editorH != minEditorHeight {
		t.Errorf("dragged to the title row: the editor is %d rows, want the minimum %d", m.layout.editorH, minEditorHeight)
	}
	m = mouse(t, m, motion(50, 29))
	if want := 27 - 1 - minGridHeight; m.layout.editorH != want || m.layout.gridH != minGridHeight {
		t.Errorf("dragged to the bottom: editor %d grid %d, want %d and %d", m.layout.editorH, m.layout.gridH, want, minGridHeight)
	}
	fits(t, m)
}

func TestPressingTheDividerMovesNeitherTheFocusNorTheCursorNorASelection(t *testing.T) {
	m := mouseModel(t, "select 1\nfrom people")
	x0, y0 := edCell(1, 0)
	x1, y1 := edCell(4, 1)
	m = mouse(t, m, press(x0, y0), motion(x1, y1), release(x1, y1))
	selected := m.editor.SelectedText()
	r0, c0 := cursorAt(m)

	m = m.toggleFocus()
	m = mouse(t, m, press(50, dividerRow), motion(50, 10), release(50, 10))
	if m.focus != focusGrid {
		t.Errorf("the drag moved the focus to %v", m.focus)
	}
	if r, c := cursorAt(m); r != r0 || c != c0 || m.editor.SelectedText() != selected {
		t.Errorf("cursor %d,%d selected %q; was %d,%d and %q", r, c, m.editor.SelectedText(), r0, c0, selected)
	}
	if m.dragging {
		t.Error("a resize left a drag behind")
	}
}

func TestTheCursorStaysInViewWhenTheEditorIsMadeShorter(t *testing.T) {
	m := mouseModel(t, numberedLines(20))
	_ = m.View()
	m, _ = send(t, m, editor.Blink())
	if !strings.Contains(m.editor.View(), "line19") {
		t.Fatalf("test needs the cursor in view to begin with:\n%s", m.editor.View())
	}
	m = mouse(t, m, press(50, dividerRow), motion(50, dividerAtRow(12)), release(50, dividerAtRow(12)))
	if !strings.Contains(m.editor.View(), "line19") {
		t.Fatalf("the cursor's line is not in view in 12 rows:\n%s", m.editor.View())
	}
	m = mouse(t, m, press(50, dividerAtRow(12)), motion(50, dividerAtRow(3)), release(50, dividerAtRow(3)))
	if got := len(strings.Split(m.editor.View(), "\n")); got != 3 {
		t.Errorf("the editor draws %d rows, want 3", got)
	}
	if !strings.Contains(m.View(), "line19") {
		t.Errorf("the cursor's line is not in view in 3 rows:\n%s", m.View())
	}
}

func TestTheGridKeepsItsPositionWhenTheEditorGrowsAndShrinks(t *testing.T) {
	m := mouseModel(t, "select 1")
	cols := []string{"a", "b", "c", "d", "e"}
	rows := make([][]string, 100)
	for i := range rows {
		rows[i] = []string{"1", "2", "3", "4", "5"}
	}
	m, _ = send(t, m, resultMsg{id: m.query.id, cols: cols, rows: rows})
	m.grid.rowOff, m.grid.colOff = 10, 2
	m.held.truncated, m.held.truncatedAt = true, 100
	view := func(m Model) string { return ansi.Strip(strings.Split(m.View(), "\n")[m.layout.dividerY()]) }
	before, held := view(m), m.held

	m = mouse(t, m, press(50, dividerRow), motion(50, dividerAtRow(15)), release(50, dividerAtRow(15)))
	if m.grid.rowOff != 10 || m.grid.colOff != 2 {
		t.Errorf("after growing the editor the grid is at row %d column %d, want 10 and 2", m.grid.rowOff, m.grid.colOff)
	}
	m = mouse(t, m, press(50, dividerAtRow(15)), motion(50, dividerAtRow(3)), release(50, dividerAtRow(3)), hover(50, 20))
	if m.grid.rowOff != 10 || m.grid.colOff != 2 {
		t.Errorf("after shrinking it the grid is at row %d column %d, want 10 and 2", m.grid.rowOff, m.grid.colOff)
	}
	if !m.held.truncated || !strings.Contains(view(m), "truncated at 100 rows") || !strings.Contains(before, "truncated at 100 rows") {
		t.Errorf("the divider lost its note: %q", view(m))
	}
	if !reflect.DeepEqual(m.held, held) {
		t.Errorf("the held result changed: %+v, was %+v", m.held, held)
	}
}

func TestDraggingTheDividerAndThenTheSeparatorLeavesEachOthersSizeAlone(t *testing.T) {
	m := mouseModel(t, "select 1")
	m = mouse(t, m, press(50, dividerRow), motion(50, dividerAtRow(10)), release(50, dividerAtRow(10)))
	m = mouse(t, m, press(separatorCol, 3), motion(42, 3), release(42, 3))
	if m.layout.schemaW != 42 || m.layout.editorH != 10 {
		t.Errorf("schema %d editor %d, want 42 and 10", m.layout.schemaW, m.layout.editorH)
	}
	m = mouse(t, m, press(60, dividerAtRow(10)), motion(60, dividerAtRow(6)), release(60, dividerAtRow(6)))
	if m.layout.schemaW != 42 || m.layout.editorH != 6 {
		t.Errorf("schema %d editor %d, want 42 and 6", m.layout.schemaW, m.layout.editorH)
	}
	fits(t, m)
}

func TestTheCornerAndTheRowsBelowTheBodyStartNoResize(t *testing.T) {
	m := mouseModel(t, "select 1")
	for _, at := range [][2]int{{separatorCol, dividerRow}, {50, 28}, {50, 29}, {50, 0}, {5, dividerRow}} {
		m = mouse(t, m, press(at[0], at[1]), motion(at[0]+5, at[1]+3), release(at[0]+5, at[1]+3))
		if m.layout.editorH != 5 || m.layout.schemaW != 30 {
			t.Errorf("a press at %v resized: schema %d editor %d", at, m.layout.schemaW, m.layout.editorH)
		}
	}
}

func TestTheDraggedHeightSurvivesTheWindowChangingSize(t *testing.T) {
	m := mouseModel(t, "select 1")
	m = mouse(t, m, press(50, dividerRow), motion(50, dividerAtRow(12)), release(50, dividerAtRow(12)))

	m, _ = send(t, m, tea.WindowSizeMsg{Width: 100, Height: 14})
	if l := m.layout; l.gridH < minGridHeight || l.editorH < minEditorHeight || 1+l.bodyH+2 != 14 {
		t.Errorf("in 14 rows: editor %d grid %d body %d", l.editorH, l.gridH, l.bodyH)
	}
	fits(t, m)
	m, _ = send(t, m, tea.WindowSizeMsg{Width: 100, Height: 30})
	if m.layout.editorH != 12 {
		t.Errorf("back at 30 rows the editor is %d, want the dragged 12", m.layout.editorH)
	}
}

func TestTheDropdownIsDrawnOverTheGridInATallerEditor(t *testing.T) {
	m, _ := completeModel(t, schemaFixture(), "", -1)
	m = mouse(t, m, press(50, dividerRow), motion(50, dividerAtRow(9)), release(50, dividerAtRow(9)))
	if m.layout.editorH != 9 {
		t.Fatalf("the editor is %d rows, want 9", m.layout.editorH)
	}
	m = typeText(t, m, "SELECT * FROM or")
	rows := strings.Split(ansi.Strip(m.View()), "\n")
	gridTop := m.layout.dividerY() + 1
	first := rows[gridTop]
	if !strings.Contains(first, "orders") || !strings.Contains(first, "table") {
		t.Fatalf("the first suggestion is not on the first grid row (row %d):\n%s", gridTop, strings.Join(rows, "\n"))
	}
	if len(rows) != 30 {
		t.Errorf("the view is %d lines, want 30", len(rows))
	}
}
