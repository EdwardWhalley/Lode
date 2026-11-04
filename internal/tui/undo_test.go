package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/EdwardWhalley/lode/internal/clipboard"
	"github.com/EdwardWhalley/lode/internal/history"
)

var (
	ctrlZ = tea.KeyMsg{Type: tea.KeyCtrlZ}
	ctrlY = tea.KeyMsg{Type: tea.KeyCtrlY}
)

// undoModel is an editor that holds nothing, with the clipboard the fake.
func undoModel(t *testing.T, sys *fakeSys) (Model, *countingAdapter) {
	t.Helper()
	a := &countingAdapter{}
	m := New(a, "x", WithClipboard(clipboard.New(sys, nil)))
	m, _ = send(t, m, tea.WindowSizeMsg{Width: 100, Height: 30})
	return m, a
}

func wantQuery(t *testing.T, m Model, want string) {
	t.Helper()
	if got := m.editor.Value(); got != want {
		t.Errorf("editor = %q, want %q", got, want)
	}
}

func TestCtrlZUndoesAndCtrlYRedoesInTheEditor(t *testing.T) {
	m, _ := undoModel(t, &fakeSys{})
	m = typeText(t, m, "SELECT 1")
	m, _ = send(t, m, ctrlZ)
	wantQuery(t, m, "")
	m, _ = send(t, m, ctrlY)
	wantQuery(t, m, "SELECT 1")
}

func TestAPasteIsUndoneInOneCtrlZ(t *testing.T) {
	m, _ := undoModel(t, &fakeSys{text: "FROM t\nWHERE x"})
	m = typeText(t, m, "SELECT 1 ")
	m = sendAndFinish(t, m, tea.KeyMsg{Type: tea.KeyCtrlV})
	wantQuery(t, m, "SELECT 1 FROM t\nWHERE x")

	m, _ = send(t, m, ctrlZ)
	wantQuery(t, m, "SELECT 1 ")
}

func TestACutIsUndoneInOneCtrlZAndTheSelectionComesBack(t *testing.T) {
	m, _ := undoModel(t, &fakeSys{})
	m = typeText(t, m, "select name")
	m = firstWordSelected(t, m)
	m = sendAndFinish(t, m, tea.KeyMsg{Type: tea.KeyCtrlX})
	wantQuery(t, m, " name")

	m, _ = send(t, m, ctrlZ)
	wantQuery(t, m, "select name")
	if got := m.editor.SelectedText(); got != "select" {
		t.Errorf("selected %q after undoing the cut, want select", got)
	}
}

func TestAcceptingACompletionIsOneStepAndUndoLeavesTheDropdownClosed(t *testing.T) {
	m, _ := completeModel(t, schemaFixture(), "", -1)
	m = typeText(t, m, "SELECT * FROM peo")
	m, _ = send(t, m, altSlash)
	if got := labels(m); got != "people" {
		t.Fatalf("suggestions = %s", got)
	}
	m, _ = send(t, m, tea.KeyMsg{Type: tea.KeyTab})
	wantQuery(t, m, `SELECT * FROM "people"`)

	m, _ = send(t, m, ctrlZ)
	wantQuery(t, m, "SELECT * FROM peo")
	if m.complete.active() {
		t.Errorf("the dropdown is open after undoing an accept: %s", labels(m))
	}
	m, _ = send(t, m, ctrlY)
	wantQuery(t, m, `SELECT * FROM "people"`)
	if m.complete.active() {
		t.Errorf("the dropdown is open after redoing an accept: %s", labels(m))
	}
}

func TestUndoAndRedoCloseAnOpenDropdown(t *testing.T) {
	for _, key := range []tea.KeyMsg{ctrlZ, ctrlY} {
		m, _ := completeModel(t, schemaFixture(), "", -1)
		m = typeText(t, m, "SELECT * FROM peo")
		m, _ = send(t, m, altSlash)
		if !m.complete.active() {
			t.Fatal("set up no dropdown")
		}
		m, _ = send(t, m, key)
		if m.complete.active() {
			t.Errorf("%v left the dropdown open: %s", key, labels(m))
		}
	}
}

func TestUndoNeverOpensTheDropdown(t *testing.T) {
	m, _ := completeModel(t, schemaFixture(), "", -1)
	m = typeText(t, m, "SELECT * FROM peo")
	m, _ = send(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	m, _ = send(t, m, tea.KeyMsg{Type: tea.KeyLeft})
	m, _ = send(t, m, tea.KeyMsg{Type: tea.KeyBackspace})
	m, _ = send(t, m, ctrlZ)
	wantQuery(t, m, "SELECT * FROM peo")
	if m.complete.active() {
		t.Errorf("undo opened the dropdown: %s", labels(m))
	}
}

func TestUndoAndRedoNeverRunAQuery(t *testing.T) {
	m, a := undoModel(t, &fakeSys{})
	m = typeText(t, m, "SELECT 1")
	for _, key := range []tea.KeyMsg{ctrlZ, ctrlY, ctrlZ, ctrlY} {
		var cmd tea.Cmd
		m, cmd = send(t, m, key)
		if m.query.running() || strings.Contains(m.status, "running") {
			t.Fatalf("%v started a query (status %q)", key, m.status)
		}
		_ = cmd
	}
	if a.queries != 0 {
		t.Errorf("%d queries ran, want none", a.queries)
	}
}

func TestCtrlZStraightAfterStartLeavesTheGeneratedFirstQuery(t *testing.T) {
	m, _ := undoModel(t, &fakeSys{})
	m, _ = send(t, m, schemaMsg{gen: 0, tables: schemaFixture()})
	const first = `SELECT * FROM "orders" LIMIT 100`
	wantQuery(t, m, first)

	m, _ = send(t, m, ctrlZ)
	wantQuery(t, m, first)
	m, _ = send(t, m, ctrlY)
	wantQuery(t, m, first)

	m = typeText(t, m, "!")
	m, _ = send(t, m, ctrlZ)
	wantQuery(t, m, first)
	m, _ = send(t, m, ctrlZ)
	wantQuery(t, m, first)
}

func TestAHistoryLoadIsUndoneInOneCtrlZ(t *testing.T) {
	m, _ := searchModel(t, entriesFor("a", "SELECT 9"))
	m = typeText(t, m, "SELECT 1")
	m, _ = send(t, m, loadQueryMsg{entry: history.Entry{Source: "a", SQL: "SELECT 9\nFROM t"}})
	wantQuery(t, m, "SELECT 9\nFROM t")

	m, _ = send(t, m, ctrlZ)
	wantQuery(t, m, "SELECT 1")
}

func TestSteppingThroughHistoryUndoesOneStepAndSteppingGoesOn(t *testing.T) {
	m, _ := searchModel(t, entriesFor("a", "q1", "q2", "q3"))
	m = typeText(t, m, "draft")
	m, _ = send(t, m, ctrlUp)
	m, _ = send(t, m, ctrlUp)
	wantQuery(t, m, "q2")

	m, _ = send(t, m, ctrlZ)
	wantQuery(t, m, "q3")

	m, _ = send(t, m, ctrlUp)
	wantQuery(t, m, "q1")
	m, _ = send(t, m, ctrlDown)
	wantQuery(t, m, "q2")
	m, _ = send(t, m, ctrlDown)
	wantQuery(t, m, "q3")
	m, _ = send(t, m, ctrlDown)
	wantQuery(t, m, "draft")
}

func TestTheUndoStackSurvivesASourceSwitch(t *testing.T) {
	w := newWorld()
	m := w.model(t)
	m = typeText(t, m, "ab")
	m, _ = send(t, m, pasteMsg{text: "cd"})
	wantQuery(t, m, "abcd")

	m, cmd := send(t, m, switchMsg{w.sw.choices["b"]})
	m = drain(t, m, cmd)
	if m.adapter != w.b {
		t.Fatal("the switch did not happen")
	}
	wantQuery(t, m, "abcd")

	m, _ = send(t, m, ctrlZ)
	wantQuery(t, m, "ab")
	m, _ = send(t, m, ctrlZ)
	wantQuery(t, m, "")
	m, _ = send(t, m, ctrlY)
	m, _ = send(t, m, ctrlY)
	wantQuery(t, m, "abcd")
}

func TestUndoOnlyActsWhileTheEditorHasTheFocus(t *testing.T) {
	m, _ := undoModel(t, &fakeSys{})
	m = typeText(t, m, "SELECT 1")
	m, _ = send(t, m, tea.KeyMsg{Type: tea.KeyTab})
	m, _ = send(t, m, ctrlZ)
	wantQuery(t, m, "SELECT 1")
}

func TestTheUndoKeysAreInTheLegend(t *testing.T) {
	help := strings.Join(helpLines(140), "\n")
	for _, want := range []string{"ctrl+z", "ctrl+y"} {
		if !strings.Contains(help, want) {
			t.Errorf("F1 help does not mention %q", want)
		}
	}
	if row := hintRow(scopeEditor, 300); !strings.Contains(row, "ctrl+z") {
		t.Errorf("hint row %q does not show ctrl+z", row)
	}
}

func TestTheStartingQueryIsTheFloorEvenAfterEarlyEdits(t *testing.T) {
	m, _ := undoModel(t, &fakeSys{})
	m = typeText(t, m, "abc")
	for i := 0; i < 3; i++ {
		m, _ = send(t, m, tea.KeyMsg{Type: tea.KeyBackspace})
	}
	m, _ = send(t, m, schemaMsg{gen: 0, tables: schemaFixture()})
	const first = `SELECT * FROM "orders" LIMIT 100`
	wantQuery(t, m, first)

	m, _ = send(t, m, ctrlZ)
	wantQuery(t, m, first)
}
