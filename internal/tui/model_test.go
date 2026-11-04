package tui_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/EdwardWhalley/lode/internal/adapter/file"
	"github.com/EdwardWhalley/lode/internal/tui"
)

// harness drives the top-level model with messages, as Bubble Tea would.
type harness struct {
	t *testing.T
	m tea.Model
}

func newHarness(t *testing.T, csv string, opts ...tui.Option) *harness {
	t.Helper()
	path := filepath.Join(t.TempDir(), "people.csv")
	if err := os.WriteFile(path, []byte(csv), 0o600); err != nil {
		t.Fatal(err)
	}
	a, err := file.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { a.Close() })

	m := tui.New(a, "people.csv", opts...)
	h := &harness{t: t, m: m}
	h.send(tea.WindowSizeMsg{Width: 80, Height: 24})
	return h
}

func (h *harness) send(msg tea.Msg) tea.Cmd {
	var cmd tea.Cmd
	h.m, cmd = h.m.Update(msg)
	return cmd
}

func (h *harness) typeQuery(sql string) {
	m := h.m.(tui.Model)
	m.SetQuery(sql)
	h.m = m
}

func (h *harness) view() string { return h.m.View() }

// start runs the commands Init returns, abandoning any that never finish, such as cursor
// blink timers.
func (h *harness) start() {
	h.t.Helper()
	cmd := h.m.Init()
	if cmd == nil {
		return
	}
	cmds := []tea.Cmd{cmd}
	if batch, ok := cmd().(tea.BatchMsg); ok {
		cmds = batch
	}
	for _, c := range cmds {
		if c == nil {
			continue
		}
		done := make(chan tea.Msg, 1)
		go func() { done <- c() }()
		select {
		case msg := <-done:
			h.send(msg)
		case <-time.After(time.Second):
		}
	}
}

func (h *harness) press(keys ...tea.KeyType) {
	for _, k := range keys {
		h.send(tea.KeyMsg{Type: k})
	}
}

// runToCompletion presses ctrl+r and feeds the query result back to the model.
func (h *harness) runToCompletion() {
	h.t.Helper()
	cmd := h.send(tea.KeyMsg{Type: tea.KeyCtrlR})
	if cmd == nil {
		h.t.Fatal("ctrl+r did not start a query")
	}
	h.send(cmd())
}

func TestRunQueryShowsResults(t *testing.T) {
	h := newHarness(t, "id,name\n1,alice\n2,bob\n")
	h.typeQuery("SELECT * FROM people ORDER BY id")
	h.runToCompletion()

	view := h.view()
	for _, want := range []string{"id", "name", "alice", "bob", "2 rows"} {
		if !strings.Contains(view, want) {
			t.Errorf("view missing %q:\n%s", want, view)
		}
	}
}

func TestQueryErrorShownInUI(t *testing.T) {
	h := newHarness(t, "id,name\n1,alice\n")
	h.typeQuery("SELECT * FROM does_not_exist")
	h.runToCompletion()

	if view := h.view(); !strings.Contains(view, "does_not_exist") {
		t.Errorf("expected the error in the view:\n%s", view)
	}
}

func TestResultsTruncatedAtCap(t *testing.T) {
	var csv strings.Builder
	csv.WriteString("n\n")
	for i := 0; i < 10; i++ {
		fmt.Fprintf(&csv, "%d\n", i)
	}
	h := newHarness(t, csv.String(), tui.WithMaxRows(3))
	h.typeQuery("SELECT * FROM people ORDER BY n")
	h.runToCompletion()

	view := h.view()
	if !strings.Contains(view, "truncated at 3 rows") {
		t.Errorf("expected a truncation indicator:\n%s", view)
	}
	if !strings.Contains(view, "3 rows") {
		t.Errorf("expected 3 rows reported:\n%s", view)
	}
}

func TestNoTruncationIndicatorWhenUnderCap(t *testing.T) {
	h := newHarness(t, "n\n1\n2\n", tui.WithMaxRows(2))
	h.typeQuery("SELECT * FROM people")
	h.runToCompletion()

	if view := h.view(); strings.Contains(view, "truncated") {
		t.Errorf("a result exactly at the cap must not be marked truncated:\n%s", view)
	}
}

func TestSchemaTreeListsTablesAndExpandsToColumns(t *testing.T) {
	h := newHarness(t, "id,name\n1,alice\n")
	h.start()

	view := h.view()
	if !strings.Contains(view, "Schema") || !strings.Contains(view, "people") {
		t.Fatalf("expected the schema pane to list the table:\n%s", view)
	}
	if strings.Contains(view, "varchar") {
		t.Fatalf("columns should be hidden until the table is expanded:\n%s", view)
	}

	h.press(tea.KeyTab, tea.KeyTab)
	h.press(tea.KeyEnter)
	view = h.view()
	for _, want := range []string{"id", "name", "varchar"} {
		if !strings.Contains(view, want) {
			t.Errorf("expanded view missing %q:\n%s", want, view)
		}
	}

	h.press(tea.KeyEnter)
	if view := h.view(); strings.Contains(view, "varchar") {
		t.Errorf("table did not collapse:\n%s", view)
	}
}

func TestTabCyclesBackToEditor(t *testing.T) {
	h := newHarness(t, "id\n1\n")
	h.typeQuery("SELECT * FROM people")
	h.press(tea.KeyTab, tea.KeyTab, tea.KeyTab)

	h.send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("x")})
	if view := h.view(); !strings.Contains(view, "SELECT * FROM peoplex") {
		t.Errorf("typing after cycling focus should reach the editor:\n%s", view)
	}
}

// clearScreen is an escape sequence a value from the database might hold.
const clearScreen = "\x1b[2J"

func TestDatabaseTextCannotDriveTheTerminal(t *testing.T) {
	h := newHarness(t, "id,ev"+clearScreen+"il\n1,x"+clearScreen+"y\n")
	h.start()

	check := func(what string) {
		t.Helper()
		if view := h.view(); strings.Contains(view, clearScreen) {
			t.Errorf("%s carried an escape sequence onto the screen:\n%q", what, view)
		}
	}

	h.typeQuery("SELECT * FROM people")
	h.runToCompletion()
	check("a column name or value in the results")

	h.typeQuery("SELECT CAST('a' || chr(27) || '[2J' AS INTEGER)")
	h.runToCompletion()
	if view := h.view(); !strings.Contains(view, "Could not convert") {
		t.Fatalf("expected a conversion error that echoes the value:\n%s", view)
	}
	check("a database error message")

	h.press(tea.KeyTab, tea.KeyTab, tea.KeyEnter)
	check("a table or column name in the schema tree")
}

func TestEscCancelsRunningQuery(t *testing.T) {
	h := newHarness(t, "id\n1\n")
	// bounded: an aggregate, so the result is one row; the test presses esc before it can finish.
	h.typeQuery("SELECT count(*) FROM range(1000000000) a, range(1000000000) b")

	cmd := h.send(tea.KeyMsg{Type: tea.KeyCtrlR})
	if cmd == nil {
		t.Fatal("ctrl+r did not start a query")
	}
	done := make(chan tea.Msg, 1)
	go func() { done <- cmd() }()

	time.Sleep(100 * time.Millisecond)
	h.send(tea.KeyMsg{Type: tea.KeyEsc})
	if view := h.view(); !strings.Contains(view, "cancelled") {
		t.Errorf("expected cancelled status:\n%s", view)
	}

	select {
	case msg := <-done:
		h.send(msg)
	case <-time.After(10 * time.Second):
		t.Fatal("cancelled query did not stop")
	}
	if view := h.view(); !strings.Contains(view, "cancelled") {
		t.Errorf("late result overwrote the cancelled status:\n%s", view)
	}
}

func TestWriteModeIsShownInTitle(t *testing.T) {
	if view := newHarness(t, "n\n1\n", tui.WithWriteMode()).view(); !strings.Contains(view, "write enabled") {
		t.Errorf("expected a write-mode indicator:\n%s", view)
	}
	if view := newHarness(t, "n\n1\n").view(); strings.Contains(view, "write enabled") {
		t.Errorf("read-only Sources must not show the indicator:\n%s", view)
	}
}
