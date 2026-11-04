package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

var (
	ctrlAltDown = tea.KeyMsg{Type: tea.KeyCtrlDown, Alt: true}
	ctrlAltUp   = tea.KeyMsg{Type: tea.KeyCtrlUp, Alt: true}
	escKey      = tea.KeyMsg{Type: tea.KeyEsc}
)

func TestCtrlAltDownAddsACursorInTheEditorOnly(t *testing.T) {
	m, _ := completeModel(t, schemaFixture(), "a1\nb2\nc3", -1)
	m, _ = send(t, m, tea.KeyMsg{Type: tea.KeyCtrlHome})

	m, _ = send(t, m, ctrlAltDown)
	if got := m.editor.CursorCount(); got != 2 {
		t.Errorf("%d cursors after ctrl+alt+↓, want 2", got)
	}

	m, _ = send(t, m, tea.KeyMsg{Type: tea.KeyTab})
	m, _ = send(t, m, ctrlAltDown)
	if got := m.editor.CursorCount(); got != 2 {
		t.Errorf("a cursor was added while the grid had the focus: %d cursors", got)
	}
}

func TestTypingAtSeveralCursorsEditsEachLineAndRunsNothing(t *testing.T) {
	m, a := completeModel(t, schemaFixture(), "ab\ncd", -1)
	m, _ = send(t, m, ctrlAltUp)
	m = typeText(t, m, "X")

	if got, want := m.editor.Value(), "abX\ncdX"; got != want {
		t.Errorf("text = %q, want %q", got, want)
	}
	if a.queries != 0 {
		t.Errorf("editing at several cursors ran %d queries", a.queries)
	}
}

func TestEscRemovesTheExtraCursorsBeforeItCancelsAQuery(t *testing.T) {
	m, _ := completeModel(t, schemaFixture(), "ab\ncd", -1)
	m, _ = send(t, m, ctrlAltUp)
	cancelled := 0
	m.query.cancel = func() { cancelled++ }

	m, _ = send(t, m, escKey)
	if got := m.editor.CursorCount(); got != 1 {
		t.Errorf("%d cursors after esc, want 1", got)
	}
	if cancelled != 0 {
		t.Error("esc cancelled the query while it still had cursors to remove")
	}

	m, _ = send(t, m, escKey)
	if cancelled != 1 {
		t.Errorf("a second esc cancelled the query %d times, want once", cancelled)
	}
}

func TestTheDropdownStaysClosedWithSeveralCursors(t *testing.T) {
	m, _ := completeModel(t, schemaFixture(), "SELECT * FROM or\nSELECT * FROM or", -1)
	m, _ = send(t, m, ctrlAltUp)
	m = typeText(t, m, "d")

	if got, want := m.editor.Value(), "SELECT * FROM ord\nSELECT * FROM ord"; got != want {
		t.Fatalf("text = %q, want %q", got, want)
	}
	if m.complete.active() {
		t.Errorf("the dropdown opened with several cursors (%s)", labels(m))
	}
	m, _ = send(t, m, altSlash)
	if m.complete.active() {
		t.Errorf("alt+/ opened the dropdown with several cursors (%s)", labels(m))
	}
}

func TestLoadingAQueryEndsTheExtraCursors(t *testing.T) {
	m, _ := completeModel(t, schemaFixture(), "ab\ncd", -1)
	m, _ = send(t, m, ctrlAltUp)
	m.SetQuery("select 1")
	if got := m.editor.CursorCount(); got != 1 {
		t.Errorf("%d cursors after the query was replaced, want 1", got)
	}
}

func TestTheCursorKeysAreInTheLegend(t *testing.T) {
	help := strings.Join(helpLines(120), "\n")
	if !strings.Contains(help, "ctrl+alt+↑/↓") {
		t.Errorf("F1 help does not list ctrl+alt+↑/↓")
	}
}

func TestAltSlashWithSeveralCursorsChangesNothing(t *testing.T) {
	m, _ := completeModel(t, schemaFixture(), "ab cd\nef gh", -1)
	m, _ = send(t, m, ctrlAltUp)
	m, _ = send(t, m, tea.KeyMsg{Type: tea.KeyShiftLeft})
	want := m.editor.SelectedText()
	if want == "" {
		t.Fatal("set up no Selection")
	}

	m, _ = send(t, m, altSlash)

	if got := m.editor.SelectedText(); got != want {
		t.Errorf("alt+/ changed the Selection from %q to %q", want, got)
	}
}
