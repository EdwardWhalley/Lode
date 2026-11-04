package tui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/EdwardWhalley/lode/internal/history"
)

var ctrlG = tea.KeyMsg{Type: tea.KeyCtrlG}

func searchModel(t *testing.T, entries []history.Entry) (Model, *fakeHistory) {
	t.Helper()
	h := &fakeHistory{entries: entries}
	m := historyModel(t, h, valuesAdapter{cols: []string{"n"}, rows: [][]any{{int64(1)}}})
	return m, h
}

func e(source, sql string) history.Entry {
	return history.Entry{Time: time.Date(2026, 10, 26, 9, 30, 0, 0, time.UTC), Source: source, SQL: sql}
}

func TestCtrlGListsEveryQueryAcrossSourcesNewestFirst(t *testing.T) {
	m, _ := searchModel(t, []history.Entry{e("a", "SELECT oldest"), e("b", "SELECT middle"), e("a", "SELECT newest")})
	m, _ = send(t, m, ctrlG)
	if _, ok := m.overlay.(historySearch); !ok {
		t.Fatal("ctrl+g did not open the history search")
	}
	view := plain(m)
	i, j, k := strings.Index(view, "SELECT newest"), strings.Index(view, "SELECT middle"), strings.Index(view, "SELECT oldest")
	if i < 0 || !(i < j && j < k) {
		t.Errorf("entries are not listed newest first:\n%s", view)
	}
	for _, want := range []string{"Search history", "3 of 3", "a ", "b "} {
		if !strings.Contains(view, want) {
			t.Errorf("view is missing %q:\n%s", want, view)
		}
	}
}

func TestCtrlGWorksFromEveryPane(t *testing.T) {
	for panes := 0; panes < 3; panes++ {
		m, _ := searchModel(t, []history.Entry{e("a", "SELECT 1")})
		for i := 0; i < panes; i++ {
			m, _ = send(t, m, tea.KeyMsg{Type: tea.KeyTab})
		}
		m, _ = send(t, m, ctrlG)
		if _, ok := m.overlay.(historySearch); !ok {
			t.Errorf("ctrl+g did not open the search with %d tabs", panes)
		}
	}
}

func TestTypingFiltersBySQLAndSourceIgnoringCase(t *testing.T) {
	m, _ := searchModel(t, []history.Entry{
		e("shop", "SELECT * FROM Orders"), e("people", "SELECT name FROM people"), e("shop", "DELETE FROM orders"),
	})
	m, _ = send(t, m, ctrlG)

	m = typeText(t, m, "ORDERS")
	view := plain(m)
	if !strings.Contains(view, "2 of 3") || strings.Contains(view, "SELECT name") {
		t.Errorf("filtering on ORDERS:\n%s", view)
	}
	m = typeText(t, m, " delete")
	if view := plain(m); !strings.Contains(view, "1 of 3") || !strings.Contains(view, "DELETE FROM orders") {
		t.Errorf("a second word narrows further:\n%s", view)
	}

	m2, _ := searchModel(t, []history.Entry{e("shop", "SELECT 1"), e("people", "SELECT 2")})
	m2, _ = send(t, m2, ctrlG)
	m2 = typeText(t, m2, "people")
	if view := plain(m2); !strings.Contains(view, "1 of 2") || !strings.Contains(view, "SELECT 2") {
		t.Errorf("the filter also matches the Source name:\n%s", view)
	}
}

func TestEnterLoadsTheFullMultiLineQueryAndDoesNotRunIt(t *testing.T) {
	sql := "SELECT *\nFROM t\nWHERE x = 1"
	m, h := searchModel(t, []history.Entry{e("a", "SELECT 0"), e("a", sql)})
	m.SetQuery("something else")
	m, _ = send(t, m, ctrlG)
	m, cmd := send(t, m, enter)
	if m.overlay != nil {
		t.Error("enter did not close the search")
	}
	m = drain(t, m, cmd)

	if got := m.editor.Value(); got != sql {
		t.Errorf("editor = %q, want the whole multi-line query", got)
	}
	if m.query.running() || m.held.exportable() || h.adds != 0 {
		t.Error("loading a query must not run it")
	}
	if !strings.Contains(m.status, "not run") {
		t.Errorf("status = %q", m.status)
	}
}

func TestEscLeavesTheEditorUntouched(t *testing.T) {
	m, _ := searchModel(t, []history.Entry{e("a", "SELECT 1")})
	m.SetQuery("keep me")
	m, _ = send(t, m, ctrlG)
	m, cmd := send(t, m, esc)
	if m.overlay != nil || cmd != nil || m.editor.Value() != "keep me" {
		t.Errorf("esc: overlay %v, cmd %v, editor %q", m.overlay, cmd != nil, m.editor.Value())
	}
}

func TestLoadingFromAnotherSourceDoesNotSwitchAndSaysWhereItCameFrom(t *testing.T) {
	m, _ := searchModel(t, []history.Entry{e("elsewhere", "SELECT from_elsewhere")})
	m, _ = send(t, m, ctrlG)
	m, cmd := send(t, m, enter)
	m = drain(t, m, cmd)
	if m.editor.Value() != "SELECT from_elsewhere" {
		t.Errorf("editor = %q", m.editor.Value())
	}
	if m.current.ID != "a" || m.source != "label-a" {
		t.Error("loading a query switched Source")
	}
	if !strings.Contains(m.status, "elsewhere") || !strings.Contains(m.status, "Source unchanged") {
		t.Errorf("status = %q, want where the query came from", m.status)
	}
}

func TestLoadingFocusesTheEditor(t *testing.T) {
	m, _ := searchModel(t, []history.Entry{e("a", "SELECT 1")})
	m, _ = send(t, m, tea.KeyMsg{Type: tea.KeyTab})
	m, _ = send(t, m, ctrlG)
	m, cmd := send(t, m, enter)
	m = drain(t, m, cmd)
	if m.focus != focusEditor {
		t.Errorf("focus = %v, want the editor", m.focus)
	}
}

func TestHistoryTextCannotDriveTheTerminal(t *testing.T) {
	m, _ := searchModel(t, []history.Entry{e("so\x1b[2Jurce", "SELECT '\x1b[2J'")})
	m, _ = send(t, m, ctrlG)
	if strings.Contains(m.View(), "\x1b[2J") {
		t.Errorf("a history entry reached the terminal unsanitized: %q", m.View())
	}
}

func TestAThousandEntriesScrollAndTheCursorStaysVisible(t *testing.T) {
	var entries []history.Entry
	for i := 0; i < history.Max; i++ {
		entries = append(entries, e("a", fmt.Sprintf("SELECT %04d", i)))
	}
	m, _ := searchModel(t, entries)
	m, _ = send(t, m, tea.WindowSizeMsg{Width: 80, Height: 16})
	m, _ = send(t, m, ctrlG)
	for i := 0; i < 500; i++ {
		m, _ = send(t, m, tea.KeyMsg{Type: tea.KeyDown})
	}
	if view := plain(m); !strings.Contains(view, "SELECT 0499") || strings.Contains(view, "SELECT 0999") {
		t.Errorf("the cursor row must be visible and the list scrolled:\n%s", view)
	}
	m, _ = send(t, m, tea.KeyMsg{Type: tea.KeyEnd})
	if view := plain(m); !strings.Contains(view, "SELECT 0000") {
		t.Errorf("end should reach the oldest:\n%s", view)
	}
	m, _ = send(t, m, tea.KeyMsg{Type: tea.KeyHome})
	if view := plain(m); !strings.Contains(view, "SELECT 0999") {
		t.Errorf("home should reach the newest:\n%s", view)
	}
	if n := len(lines(m.View())); n != 16 {
		t.Errorf("view is %d lines, want 16", n)
	}
}

func TestEmptyAndNoMatchStatesRender(t *testing.T) {
	m, _ := searchModel(t, nil)
	m, _ = send(t, m, ctrlG)
	if view := plain(m); !strings.Contains(view, "no history yet") || !strings.Contains(view, "0 of 0") {
		t.Errorf("empty history:\n%s", view)
	}
	if m2, cmd := send(t, m, enter); m2.overlay == nil || cmd != nil {
		t.Error("enter on an empty list should do nothing")
	}

	m, _ = searchModel(t, []history.Entry{e("a", "SELECT 1")})
	m, _ = send(t, m, ctrlG)
	m = typeText(t, m, "zzz")
	if view := plain(m); !strings.Contains(view, "nothing matches") {
		t.Errorf("no match:\n%s", view)
	}
}

func TestCtrlGIsIgnoredWhileAnotherOverlayIsOpen(t *testing.T) {
	m, _ := searchModel(t, []history.Entry{e("a", "SELECT 1")})
	m, _ = send(t, m, f1)
	m, _ = send(t, m, ctrlG)
	if _, ok := m.overlay.(help); !ok {
		t.Errorf("ctrl+g replaced the help overlay with %T", m.overlay)
	}
}

func TestCtrlGWithHistoryOffSaysSo(t *testing.T) {
	m := New(valuesAdapter{}, "x")
	m, _ = send(t, m, tea.WindowSizeMsg{Width: 100, Height: 30})
	m, _ = send(t, m, ctrlG)
	if m.overlay != nil || !m.isError || !strings.Contains(m.status, "history is off") {
		t.Errorf("status = %q, overlay %v", m.status, m.overlay)
	}
}
