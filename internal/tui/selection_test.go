package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/EdwardWhalley/lode/internal/theme"
)

var (
	shiftLeft  = tea.KeyMsg{Type: tea.KeyShiftLeft}
	shiftRight = tea.KeyMsg{Type: tea.KeyShiftRight}
	altA       = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}, Alt: true}
)

func TestSelectingInTheEditorClosesTheDropdown(t *testing.T) {
	m, _ := completeModel(t, schemaFixture(), "", -1)
	m = typeText(t, m, "SELECT * FROM ord")
	if !m.complete.active() {
		t.Fatal("the dropdown did not open while typing, so there is nothing to close")
	}

	m, _ = send(t, m, shiftLeft)

	if m.complete.active() {
		t.Errorf("the dropdown is still open (%s) after a Selection was made", labels(m))
	}
	if got := m.editor.SelectedText(); got != "d" {
		t.Errorf("selected %q, want %q", got, "d")
	}
}

func TestOpeningSuggestionsWithASelectionDropsItSoAcceptingDoesNotEatIt(t *testing.T) {
	m, _ := completeModel(t, schemaFixture(), "SELECT * FROM ord", 14)
	for range 3 {
		m, _ = send(t, m, shiftRight)
	}
	if got := m.editor.SelectedText(); got != "ord" {
		t.Fatalf("setup selected %q, want %q", got, "ord")
	}

	m, _ = send(t, m, altSlash)
	if m.editor.HasSelection() {
		t.Errorf("alt+/ left %q selected", m.editor.SelectedText())
	}
	if !m.complete.active() {
		t.Fatal("alt+/ did not open the dropdown")
	}
	m, _ = send(t, m, tea.KeyMsg{Type: tea.KeyTab})

	if got, want := m.editor.Value(), `SELECT * FROM "orders"`; got != want {
		t.Errorf("after accepting, text = %q, want %q", got, want)
	}
}

func TestSelectKeysNeverRunAQuery(t *testing.T) {
	m, a := completeModel(t, schemaFixture(), "select 1", -1)

	for _, k := range []tea.KeyMsg{shiftLeft, shiftLeft, altA, {Type: tea.KeyRunes, Runes: []rune{'x'}}, {Type: tea.KeyBackspace}} {
		m, _ = send(t, m, k)
	}

	if a.queries != 0 {
		t.Errorf("editing with a Selection ran %d queries, want none", a.queries)
	}
}

func TestSelectKeysActOnTheEditorOnlyWhileItHasTheFocus(t *testing.T) {
	m, _ := completeModel(t, schemaFixture(), "select 1", -1)
	m, _ = send(t, m, tea.KeyMsg{Type: tea.KeyTab})

	m, _ = send(t, m, shiftLeft)
	m, _ = send(t, m, altA)

	if m.editor.HasSelection() {
		t.Errorf("a Selection was made in the editor while the grid had the focus: %q", m.editor.SelectedText())
	}
}

func TestTheSelectionIsStillThereWhenTheFocusComesBack(t *testing.T) {
	m, _ := completeModel(t, schemaFixture(), "select 1", -1)
	m, _ = send(t, m, shiftLeft)
	m, _ = send(t, m, shiftLeft)
	tab := tea.KeyMsg{Type: tea.KeyTab}
	m, _ = send(t, m, tab)
	m, _ = send(t, m, tab)
	m, _ = send(t, m, tab)

	if got := m.editor.SelectedText(); got != " 1" {
		t.Errorf("selected %q after the focus went round, want %q", got, " 1")
	}
}

func TestSelectAllSelectsTheWholeQuery(t *testing.T) {
	m, _ := completeModel(t, schemaFixture(), "select 1\nfrom x", 3)
	m, _ = send(t, m, altA)
	if got := m.editor.SelectedText(); got != "select 1\nfrom x" {
		t.Errorf("selected %q, want everything", got)
	}
}

func TestTheSelectionKeysAreInTheLegend(t *testing.T) {
	help := strings.Join(helpLines(120), "\n")
	for _, want := range []string{"shift+arrows", "ctrl+shift+←/→", "ctrl+shift+home/end", "ctrl+←/→", "alt+a"} {
		if !strings.Contains(help, want) {
			t.Errorf("F1 help does not list %q", want)
		}
	}
}

func TestTheSelectionIsVisibleInEveryThemeAndNotTheSameAsTheHighlight(t *testing.T) {
	withColour(t)
	for _, name := range theme.Names() {
		m := themed(t, name)
		m.SetQuery("select name from people")
		plain := m.View()

		m, _ = send(t, m, tea.KeyMsg{Type: tea.KeyCtrlA})
		m, _ = send(t, m, tea.KeyMsg{Type: tea.KeyCtrlShiftRight})
		if !m.editor.HasSelection() {
			t.Fatalf("%s: ctrl+shift+→ selected nothing", name)
		}
		if m.View() == plain {
			t.Errorf("%s: a Selection is not drawn at all", name)
		}

		ApplyTheme(name)
		selection, highlight := selectionStyle.Render("x"), highlightStyle.Render("x")
		if selection == "x" {
			t.Errorf("%s: the Selection style adds nothing to the text", name)
		}
		if selection == highlight {
			t.Errorf("%s: the Selection looks the same as the Highlight (%q)", name, selection)
		}
	}
}

func TestMonoDrawsTheSelectionWithoutColour(t *testing.T) {
	withColour(t)
	m := themed(t, "mono")
	m.SetQuery("select name from people")
	m, _ = send(t, m, tea.KeyMsg{Type: tea.KeyCtrlA})
	m, _ = send(t, m, tea.KeyMsg{Type: tea.KeyCtrlShiftRight})

	view := m.View()
	if codes := colourCodes(view); len(codes) > 0 {
		t.Errorf("mono must not use colour, but the view sets %v", codes)
	}
	if !strings.Contains(view, "\x1b[4") {
		t.Errorf("mono does not underline the Selection: %q", view)
	}
}

func TestTheEditorsOwnSelectionStyleFollowsTheTheme(t *testing.T) {
	withColour(t)
	for _, name := range theme.Names() {
		m := themed(t, name)
		want := lipgloss.NewStyle().Inherit(selectionStyle).Render("x")
		got := m.editor.FocusedStyle.Selection.Render("x")
		if got != want {
			t.Errorf("%s: the editor draws its Selection as %q, the theme says %q", name, got, want)
		}
	}
}
