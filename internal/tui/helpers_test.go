package tui

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/EdwardWhalley/lode/internal/adapter"
)

// send hands msg to the Model, as Bubble Tea would, and returns the command it gave.
func send(t *testing.T, m Model, msg tea.Msg) (Model, tea.Cmd) {
	t.Helper()
	updated, cmd := m.Update(msg)
	return updated.(Model), cmd
}

// drain runs a command and feeds every message it produces back to the model until nothing
// is left.
func drain(t *testing.T, m Model, cmd tea.Cmd) Model {
	t.Helper()
	if cmd == nil {
		return m
	}
	switch msg := cmd().(type) {
	case nil:
	case tea.BatchMsg:
		for _, c := range msg {
			m = drain(t, m, c)
		}
	default:
		updated, next := m.Update(msg)
		m = updated.(Model)
		m = drain(t, m, next)
	}
	return m
}

// sendAndFinish sends msg, runs the command it returns and feeds the answer back; it fails
// if that quits.
func sendAndFinish(t *testing.T, m Model, msg tea.Msg) Model {
	t.Helper()
	m, cmd := send(t, m, msg)
	if cmd == nil {
		return m
	}
	answer := cmd()
	if _, quit := answer.(tea.QuitMsg); quit {
		t.Fatalf("%v quit the program", msg)
	}
	m, _ = send(t, m, answer)
	return m
}

// pressKey sends a key and runs the commands it returns.
func pressKey(t *testing.T, m Model, k tea.KeyMsg) Model {
	t.Helper()
	m, cmd := send(t, m, k)
	return drain(t, m, cmd)
}

func keyRunes(s string) tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)} }

// runCmd runs a command and drops the message it returns.
func runCmd(cmd tea.Cmd) {
	if cmd != nil {
		cmd()
	}
}

func typeText(t *testing.T, m Model, s string) Model {
	t.Helper()
	for _, r := range s {
		m, _ = send(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	return m
}

// newBare returns a Model with no Adapter, sized like a terminal.
func newBare(t *testing.T, width, height int) Model {
	t.Helper()
	m := New(nil, "test")
	updated, _ := m.Update(tea.WindowSizeMsg{Width: width, Height: height})
	return updated.(Model)
}

func lines(s string) []string { return strings.Split(s, "\n") }

func plain(m Model) string { return ansi.Strip(m.View()) }

func col(name string) adapter.Column {
	return adapter.Column{Name: name, Type: "TEXT", Ref: adapter.QuoteIdent(name)}
}

func schemaFixture() []adapter.Table {
	return []adapter.Table{
		{Name: "orders", Ref: `"orders"`, Columns: []adapter.Column{col("id"), col("customer"), col("order")}},
		{Name: "people", Ref: `"people"`, Columns: []adapter.Column{col("id"), col("name")}},
		{Name: "sales.Order Items", Ref: `"sales"."Order Items"`, Columns: []adapter.Column{col("id"), col("First Name")}},
	}
}

// completeModel is a Model with a loaded schema and text in the editor, the cursor at
// column at (the end if negative).
func completeModel(t *testing.T, tables []adapter.Table, text string, at int) (Model, *countingAdapter) {
	t.Helper()
	a := &countingAdapter{}
	m := New(a, "x")
	m, _ = send(t, m, tea.WindowSizeMsg{Width: 100, Height: 30})
	m, _ = send(t, m, schemaMsg{gen: 0, tables: tables})
	m.SetQuery(text)
	if at >= 0 {
		m.editor.SetCursor(at)
	}
	return m, a
}

func labels(m Model) string {
	if !m.complete.active() {
		return "<closed>"
	}
	var out []string
	for _, s := range m.complete.items {
		out = append(out, s.label)
	}
	return strings.Join(out, ",")
}

func manyTables(n int) []adapter.Table {
	var out []adapter.Table
	for i := 0; i < n; i++ {
		name := fmt.Sprintf("t%02d", i)
		out = append(out, adapter.Table{Name: name, Ref: `"` + name + `"`, Columns: []adapter.Column{col("id")}})
	}
	return out
}

func numberedLines(n int) string {
	lines := make([]string, n)
	for i := range lines {
		lines[i] = fmt.Sprintf("line%02d", i)
	}
	return strings.Join(lines, "\n")
}

// fits says the view is exactly height lines of at most width columns.
func fits(t *testing.T, m Model) {
	t.Helper()
	rows := strings.Split(m.View(), "\n")
	if len(rows) != m.height {
		t.Errorf("the view is %d lines, want %d", len(rows), m.height)
	}
	for i, row := range rows {
		if w := ansi.StringWidth(row); w > m.width {
			t.Errorf("row %d is %d columns wide in a window of %d", i, w, m.width)
		}
	}
}

// The screen of a 100 x 30 terminal, from Model.View.
const (
	editorLeft = 31
	promptCols = 2
	editorTop  = 1
	dividerRow = 6
	gridHeader = 7
	treeTop    = 2
)

// separatorCol is the separator's column in the 100 x 30 mouse model.
const separatorCol = 30

func edCell(col, row int) (x, y int) { return editorLeft + promptCols + col, editorTop + row }

func press(x, y int) tea.MouseMsg {
	return tea.MouseMsg{X: x, Y: y, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft}
}

func motion(x, y int) tea.MouseMsg {
	return tea.MouseMsg{X: x, Y: y, Action: tea.MouseActionMotion, Button: tea.MouseButtonLeft}
}

func release(x, y int) tea.MouseMsg {
	return tea.MouseMsg{X: x, Y: y, Action: tea.MouseActionRelease, Button: tea.MouseButtonNone}
}

func wheel(x, y int, up bool) tea.MouseMsg {
	b := tea.MouseButtonWheelDown
	if up {
		b = tea.MouseButtonWheelUp
	}
	return tea.MouseMsg{X: x, Y: y, Action: tea.MouseActionPress, Button: b}
}

func hover(x, y int) tea.MouseMsg {
	return tea.MouseMsg{X: x, Y: y, Action: tea.MouseActionMotion, Button: tea.MouseButtonNone}
}

// mouseModel is a 100 x 30 terminal with text in the editor, 100 rows in the grid and 40
// tables in the tree.
func mouseModel(t *testing.T, text string) Model {
	t.Helper()
	m, _ := completeModel(t, manyTables(40), text, -1)
	rows := make([][]string, 100)
	for i := range rows {
		rows[i] = []string{fmt.Sprint(i)}
	}
	m, _ = send(t, m, resultMsg{id: m.query.id, cols: []string{"n"}, rows: rows})
	return m
}

func mouse(t *testing.T, m Model, msgs ...tea.MouseMsg) Model {
	t.Helper()
	for _, msg := range msgs {
		m, _ = send(t, m, msg)
	}
	return m
}

func cursorAt(m Model) (row, col int) {
	li := m.editor.LineInfo()
	return m.editor.Line(), li.StartColumn + li.ColumnOffset
}

func firstEditorRow(m Model) string {
	return strings.TrimSpace(strings.SplitN(m.editor.View(), "\n", 2)[0])
}
