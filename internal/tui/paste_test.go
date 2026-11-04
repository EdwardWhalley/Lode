package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestCtrlVInTheEditorAsksForTheClipboardAndChangesNothingYet(t *testing.T) {
	m, _ := completeModel(t, schemaFixture(), "select  from x", 7)

	m, cmd := send(t, m, tea.KeyMsg{Type: tea.KeyCtrlV})

	if cmd == nil {
		t.Fatal("ctrl+v in the editor started nothing: the clipboard is never read")
	}
	if got := m.editor.Value(); got != "select  from x" {
		t.Errorf("editor text = %q before the clipboard answered, want it unchanged", got)
	}
}

func TestPastedTextGoesInAtTheCursorWithoutRunningAnything(t *testing.T) {
	m, a := completeModel(t, schemaFixture(), "select  from x", 7)

	m, _ = send(t, m, pasteMsg{text: "a\tb"})

	if got, want := m.editor.Value(), "select a    b from x"; got != want {
		t.Errorf("editor text = %q, want %q (a tab becomes spaces)", got, want)
	}
	if a.queries != 0 {
		t.Errorf("pasting ran %d queries, want none", a.queries)
	}
	if m.complete.active() {
		t.Errorf("pasting opened the dropdown (%s); pasted text never does", labels(m))
	}
}

func TestPasteGoesNowhereWhenTheEditorDoesNotHaveTheFocus(t *testing.T) {
	m, _ := completeModel(t, schemaFixture(), "select 1", -1)
	m, _ = send(t, m, tea.KeyMsg{Type: tea.KeyTab})

	_, cmd := send(t, m, tea.KeyMsg{Type: tea.KeyCtrlV})
	if cmd != nil {
		t.Error("ctrl+v with the grid focused started a clipboard read")
	}
	m, _ = send(t, m, pasteMsg{text: "DROP TABLE x"})
	if got := m.editor.Value(); got != "select 1" {
		t.Errorf("a paste reached the editor while the grid had the focus: %q", got)
	}
}

func TestAfterPastingMoreLinesThanTheEditorShowsTheNextKeyBringsTheCursorIntoView(t *testing.T) {
	m, _ := completeModel(t, schemaFixture(), "", -1)

	m, _ = send(t, m, pasteMsg{text: "line01\nline02\nline03\nline04\nline05\nline06\nline07\nline08\nline09\nline10"})
	_ = m.View()
	m, _ = send(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})

	if view := m.editor.View(); !strings.Contains(view, "line10x") {
		t.Errorf("after pasting 10 lines and typing x the editor shows %q, want the last line (where the cursor is) in view", view)
	}
}

func TestAPasteThatArrivesUnderAnOverlayIsLeftOut(t *testing.T) {
	m, _ := completeModel(t, schemaFixture(), "select  from x", 7)
	m, _ = send(t, m, f1)
	if m.overlay == nil {
		t.Fatal("F1 opened no Overlay")
	}

	m, _ = send(t, m, pasteMsg{text: "PASTED"})

	if got := m.editor.Value(); got != "select  from x" {
		t.Errorf("editor text = %q, want it unchanged: the paste arrived under an Overlay", got)
	}
}
