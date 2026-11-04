package tui

import (
	"context"
	"encoding/csv"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/EdwardWhalley/lode/internal/adapter"
	"github.com/EdwardWhalley/lode/internal/clipboard"
	"github.com/EdwardWhalley/lode/internal/export"
)

// copyModel has run a query answered by these values, and the grid has the focus.
func copyModel(t *testing.T, a adapter.Adapter, sys *fakeSys) Model {
	t.Helper()
	m := New(a, "x", WithClipboard(clipboard.New(sys, nil)))
	m, _ = send(t, m, tea.WindowSizeMsg{Width: 100, Height: 30})
	m.SetQuery("SELECT 1")
	m = pressKey(t, m, ctrlR)
	m, _ = send(t, m, tab)
	if m.focus != focusGrid {
		t.Fatalf("focus = %v", m.focus)
	}
	return m
}

func oneRowOf(values ...any) valuesAdapter {
	cols := make([]string, len(values))
	for i := range cols {
		cols[i] = "c" + itoa(i)
	}
	return valuesAdapter{cols: cols, rows: [][]any{values}}
}

// copied presses the key, as Bubble Tea would, and says what reached the clipboard.
func copied(t *testing.T, m Model, sys *fakeSys, key tea.KeyMsg) (Model, string) {
	t.Helper()
	m = sendAndFinish(t, m, key)
	return m, sys.text
}

var (
	keyC = keyRunes("c")
	keyR = keyRunes("r")
)

func TestACellCopiesTheDatabasesValue(t *testing.T) {
	long := strings.Repeat("x", 300)
	stamp := time.Date(2026, 11, 1, 18, 30, 0, 500, time.UTC)
	cases := []struct {
		name   string
		value  any
		want   string
		status string
	}{
		{"past the 40 columns the grid shows", long, long, "copied 300 characters"},
		{"a line break is kept", "one\ntwo", "one\ntwo", "copied 7 characters"},
		{"a tab is kept", "one\ttwo", "one\ttwo", "copied 7 characters"},
		{"bytes from the driver are text", []byte("jsonb text"), "jsonb text", "copied 10 characters"},
		{"one character reads properly", "é", "é", "copied 1 character"},
		{"an empty string", "", "", "copied 0 characters"},
		{"NULL is empty and says so", nil, "", "copied NULL (empty)"},
		{"the text NULL is that text", "NULL", "NULL", "copied 4 characters"},
		{"a timestamp is RFC 3339", stamp, "2026-11-01T18:30:00.0000005Z", "copied 28 characters"},
		{"binary is hex", []byte{0xff, 0x00, 0x10}, `\xff0010`, "copied 8 characters"},
		{"binary that is valid text is still hex", []byte{0x00, 0x01}, `\x0001`, "copied 6 characters"},
		{"a number", int64(42), "42", "copied 2 characters"},
		{"a float keeps its digits", 1.5, "1.5", "copied 3 characters"},
	}
	for _, key := range []tea.KeyMsg{keyC, ctrlC} {
		for _, c := range cases {
			t.Run(key.String()+"/"+c.name, func(t *testing.T) {
				sys := &fakeSys{text: "before"}
				m := copyModel(t, oneRowOf(c.value), sys)
				m, got := copied(t, m, sys, key)
				if got != c.want {
					t.Errorf("clipboard holds %q, want %q", got, c.want)
				}
				if m.status != c.status {
					t.Errorf("status = %q, want %q", m.status, c.status)
				}
			})
		}
	}
}

func TestACopyIsTheHighlightedCell(t *testing.T) {
	sys := &fakeSys{}
	a := valuesAdapter{cols: []string{"a", "b"}, rows: [][]any{{"r0a", "r0b"}, {"r1a", "r1b"}}}
	m := copyModel(t, a, sys)
	m = keys(t, m, keyDown, keyRight)
	if _, got := copied(t, m, sys, keyC); got != "r1b" {
		t.Errorf("clipboard holds %q, want the highlighted cell", got)
	}
}

func TestACopiedCellMatchesWhatCSVExportWrites(t *testing.T) {
	values := []any{
		nil, "NULL", "text", "a,b", "line\nbreak",
		[]byte("bytes"), []byte{0xff, 0x00}, []byte{0x00, 0x01}, []byte("a\tb\nc"), int64(7), 2.5, float32(0.1), true,
		time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 11, 1, 18, 30, 5, 0, time.FixedZone("x", 3600)),
	}
	path := filepath.Join(t.TempDir(), "out.csv")
	target, err := export.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	cols := make([]string, len(values))
	for i := range cols {
		cols[i] = "c" + itoa(i)
	}
	if err := target.Write(cols, [][]any{values}); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	records, err := csv.NewReader(f).ReadAll()
	if err != nil {
		t.Fatal(err)
	}

	for i, v := range values {
		sys := &fakeSys{text: "before"}
		m := copyModel(t, oneRowOf(v), sys)
		if _, got := copied(t, m, sys, keyC); got != records[1][i] {
			t.Errorf("value %#v copied as %q, and exported as %q", v, got, records[1][i])
		}
	}
}

func TestARowCopiesAsTabSeparatedCells(t *testing.T) {
	sys := &fakeSys{}
	a := valuesAdapter{
		cols: []string{"a", "b", "c", "d", "e"},
		rows: [][]any{{int64(1), "two", nil, "", []byte{0xff}}},
	}
	m := copyModel(t, a, sys)
	m, got := copied(t, m, sys, keyR)
	if want := "1\ttwo\t\t\t\\xff"; got != want {
		t.Errorf("clipboard holds %q, want %q", got, want)
	}
	if m.status != "copied a row of 5 columns" {
		t.Errorf("status = %q", m.status)
	}
}

func TestARowOfOneColumnReadsProperly(t *testing.T) {
	sys := &fakeSys{}
	m := copyModel(t, oneRowOf("x"), sys)
	m, _ = copied(t, m, sys, keyR)
	if m.status != "copied a row of 1 column" {
		t.Errorf("status = %q", m.status)
	}
}

func TestARowQuotesWhatWouldShiftTheColumnsAndACellDoesNot(t *testing.T) {
	cases := []struct {
		name, value, inRow string
	}{
		{"a tab", "a\tb", `"a	b"`},
		{"a line feed", "a\nb", "\"a\nb\""},
		{"a carriage return", "a\rb", "\"a\rb\""},
		{"a double quote", `say "hi"`, `"say ""hi"""`},
		{"plain text is left alone", "plain, text's", "plain, text's"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			sys := &fakeSys{}
			m := copyModel(t, oneRowOf("first", c.value), sys)
			m, got := copied(t, m, sys, keyR)
			if want := "first\t" + c.inRow; got != want {
				t.Errorf("row is %q, want %q", got, want)
			}
			m = keys(t, m, keyRight)
			if _, got := copied(t, m, sys, keyC); got != c.value {
				t.Errorf("cell is %q, want it as it is: %q", got, c.value)
			}
		})
	}
}

func TestCtrlCInTheGridCancelsARunningQueryAndCopiesNothing(t *testing.T) {
	w := newWorld()
	w.a.block = true
	sys := &fakeSys{text: "before"}
	m := New(w.a, "x", WithClipboard(clipboard.New(sys, nil)))
	m, _ = send(t, m, tea.WindowSizeMsg{Width: 100, Height: 30})
	m, _ = send(t, m, resultMsg{id: m.query.id, cols: []string{"a"}, rows: [][]string{{"shown"}}, raw: [][]any{{"shown"}}, rawCols: []string{"a"}})
	m.SetQuery("SELECT 1")
	m, run := send(t, m, ctrlR)
	defer runCmd(run)
	m, _ = send(t, m, tab)

	m, cmd := send(t, m, ctrlC)
	if cmd != nil {
		t.Errorf("ctrl+c gave a command while cancelling")
	}
	if m.status != "cancelled" || m.query.running() {
		t.Errorf("status %q, running %v; want the query cancelled", m.status, m.query.running())
	}
	if sys.text != "before" {
		t.Errorf("clipboard holds %q; nothing should be copied", sys.text)
	}
}

func TestCtrlCInTheGridWithRowsDoesNotQuit(t *testing.T) {
	sys := &fakeSys{}
	m := copyModel(t, oneRowOf("x"), sys)
	sendAndFinish(t, m, ctrlC)
	if sys.text != "x" {
		t.Errorf("clipboard holds %q", sys.text)
	}
}

func TestCtrlCInTheGridWithNoRowsQuits(t *testing.T) {
	m := copyModel(t, valuesAdapter{cols: []string{"a"}}, &fakeSys{})
	_, cmd := send(t, m, ctrlC)
	if cmd == nil {
		t.Fatal("ctrl+c with no rows did nothing")
	}
	if _, quit := cmd().(tea.QuitMsg); !quit {
		t.Error("ctrl+c with no rows did not quit")
	}
}

func TestASelectionLeftInTheEditorMakesNoDifferenceInTheGrid(t *testing.T) {
	sys := &fakeSys{}
	m := copyModel(t, oneRowOf("cell"), sys)
	m, _ = send(t, m, tab)
	m, _ = send(t, m, tab)
	m, _ = send(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}, Alt: true})
	if !m.editor.HasSelection() {
		t.Fatal("setup: no Selection")
	}
	m, _ = send(t, m, tab)
	if _, got := copied(t, m, sys, ctrlC); got != "cell" {
		t.Errorf("clipboard holds %q, want the cell", got)
	}
}

func TestCtrlCInTheEditorAndTreeIsUnchanged(t *testing.T) {
	for _, presses := range []int{0, 2} {
		sys := &fakeSys{text: "before"}
		m := copyModel(t, oneRowOf("x"), sys)
		for range presses + 2 {
			m, _ = send(t, m, tab)
		}
		_, cmd := send(t, m, ctrlC)
		if cmd == nil {
			t.Fatalf("focus %v: ctrl+c did nothing", m.focus)
		}
		if _, quit := cmd().(tea.QuitMsg); !quit {
			t.Errorf("focus %v: ctrl+c did not quit", m.focus)
		}
		if sys.text != "before" {
			t.Errorf("focus %v: clipboard holds %q", m.focus, sys.text)
		}
	}
}

func TestCAndRTypedInTheEditorAreText(t *testing.T) {
	sys := &fakeSys{text: "before"}
	m := copyModel(t, oneRowOf("x"), sys)
	m, _ = send(t, m, tab)
	m, _ = send(t, m, tab)
	m.SetQuery("")
	m = typeText(t, m, "cr")
	if got := m.editor.Value(); got != "cr" {
		t.Errorf("editor holds %q", got)
	}
	if sys.text != "before" {
		t.Errorf("clipboard holds %q", sys.text)
	}
}

func TestCAndRDoNothingInTheTree(t *testing.T) {
	sys := &fakeSys{text: "before"}
	m := copyModel(t, oneRowOf("x"), sys)
	m, _ = send(t, m, tab)
	if m = sendAndFinish(t, m, keyC); sys.text != "before" {
		t.Errorf("clipboard holds %q", sys.text)
	}
	if m = sendAndFinish(t, m, keyR); sys.text != "before" {
		t.Errorf("clipboard holds %q", sys.text)
	}
	if strings.Contains(m.status, "copied") || strings.Contains(m.status, "no rows") {
		t.Errorf("status = %q", m.status)
	}
}

func TestACopyThenAPasteInTheEditorGivesTheValueWithNoSystemClipboard(t *testing.T) {
	a := oneRowOf("a value")
	m := New(a, "x")
	m, _ = send(t, m, tea.WindowSizeMsg{Width: 100, Height: 30})
	m.SetQuery("SELECT 1")
	m = pressKey(t, m, ctrlR)
	m, _ = send(t, m, tab)
	m = sendAndFinish(t, m, keyC)
	m, _ = send(t, m, tab)
	m, _ = send(t, m, tab)
	m.SetQuery("")
	m = sendAndFinish(t, m, ctrlV)
	if got := m.editor.Value(); got != "a value" {
		t.Errorf("pasted %q", got)
	}
}

func TestAFailingClipboardSaysSo(t *testing.T) {
	for _, key := range []tea.KeyMsg{keyC, keyR} {
		sys := &fakeSys{writeErr: errors.New("no xclip")}
		m := copyModel(t, oneRowOf("x"), sys)
		m = sendAndFinish(t, m, key)
		if !strings.Contains(m.status, "copied") || !strings.Contains(m.status, ", but ") {
			t.Errorf("status = %q, want it to say the copy did not reach a clipboard", m.status)
		}
	}
}

func TestCAndRWithNoRowsSaySo(t *testing.T) {
	for _, key := range []tea.KeyMsg{keyC, keyR} {
		sys := &fakeSys{text: "before"}
		m := copyModel(t, valuesAdapter{cols: []string{"a"}}, sys)
		m, cmd := send(t, m, key)
		if cmd != nil {
			t.Error("a command with no rows to copy")
		}
		if m.status != "no rows to copy" {
			t.Errorf("status = %q", m.status)
		}
		if sys.text != "before" {
			t.Errorf("clipboard holds %q", sys.text)
		}
	}
}

// blockingAfterFirst answers its first query with rows and then waits to be cancelled.
type blockingAfterFirst struct {
	valuesAdapter
	queries *int
}

func (a blockingAfterFirst) Query(ctx context.Context, sql string) (adapter.Rows, error) {
	*a.queries++
	if *a.queries > 1 {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	return a.valuesAdapter.Query(ctx, sql)
}

func TestACopyWhileAQueryRunsCopiesTheRowsOnScreen(t *testing.T) {
	sys := &fakeSys{}
	n := 0
	m := copyModel(t, blockingAfterFirst{oneRowOf("on screen"), &n}, sys)
	m, run := send(t, m, ctrlR)
	if !m.query.running() {
		t.Fatal("setup: no query running")
	}
	cancel := m.query.cancel
	defer func() { cancel(); runCmd(run) }()

	m = sendAndFinish(t, m, keyC)
	if sys.text != "on screen" {
		t.Errorf("clipboard holds %q", sys.text)
	}
	if !m.query.running() {
		t.Error("c cancelled the query")
	}
}

// failsAfterFirst answers its first query with rows and then fails.
type failsAfterFirst struct {
	valuesAdapter
	queries *int
}

func (a failsAfterFirst) Query(ctx context.Context, sql string) (adapter.Rows, error) {
	*a.queries++
	if *a.queries > 1 {
		return nil, errors.New("no such table")
	}
	return a.valuesAdapter.Query(ctx, sql)
}

func TestAfterAFailedQueryACopyStillGivesTheRowsOnScreen(t *testing.T) {
	sys := &fakeSys{}
	n := 0
	m := copyModel(t, failsAfterFirst{oneRowOf("kept"), &n}, sys)
	m = pressKey(t, m, ctrlR)
	if !m.isError {
		t.Fatalf("setup: status %q", m.status)
	}
	if _, got := copied(t, m, sys, keyC); got != "kept" {
		t.Errorf("clipboard holds %q", got)
	}
}

func TestAfterASourceSwitchACopyStillGivesTheRowsOnScreen(t *testing.T) {
	w := newWorld()
	sys := &fakeSys{}
	m := New(w.a, "source-a", WithMaxRows(100), WithSwitcher(w.sw, w.sw.choices["a"]), WithClipboard(clipboard.New(sys, nil)))
	m, _ = send(t, m, tea.WindowSizeMsg{Width: 100, Height: 30})
	m.SetQuery("SELECT 1")
	m = pressKey(t, m, ctrlR)
	m, _ = send(t, m, tab)
	m = sendAndFinish(t, m, switchMsg{choice: w.sw.choices["b"]})
	if m.source != "source-b" {
		t.Fatalf("source = %q", m.source)
	}
	if _, got := copied(t, m, sys, keyC); got != "a" {
		t.Errorf("clipboard holds %q", got)
	}
}

func TestTheCopyKeysAreInTheHintRowAndHelp(t *testing.T) {
	m := copyModel(t, oneRowOf("x"), &fakeSys{})
	m, _ = send(t, m, tea.WindowSizeMsg{Width: 80, Height: 30})
	rows := lines(plain(m))
	if hint := rows[len(rows)-1]; !strings.Contains(hint, "c copy") || !strings.Contains(hint, "r row") {
		t.Errorf("grid hints at 80 columns = %q", hint)
	}
	help := strings.Join(helpLines(140), "\n")
	for _, want := range []string{"c / r", "copy"} {
		if !strings.Contains(help, want) {
			t.Errorf("F1 help does not mention %q", want)
		}
	}
}
