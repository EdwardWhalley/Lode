package tui

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/EdwardWhalley/lode/internal/adapter/mysql"
	"github.com/EdwardWhalley/lode/internal/adapter/postgres"
	"github.com/EdwardWhalley/lode/internal/sqltext"
)

var ctrlR = tea.KeyMsg{Type: tea.KeyCtrlR}

// runner is a Model on a fake Adapter and History, with text in the editor and the cursor
// at the given column of its first line.
func runner(t *testing.T, text string, at int) (Model, *fakeAdapter, *fakeHistory) {
	t.Helper()
	w := newWorld()
	h := &fakeHistory{}
	m := New(w.a, "source-a", WithMaxRows(100), WithSwitcher(w.sw, w.sw.choices["a"]), WithHistory(h))
	m, _ = send(t, m, tea.WindowSizeMsg{Width: 100, Height: 30})
	m.SetQuery(text)
	if at >= 0 {
		for m.editor.Line() > 0 {
			m.editor.CursorUp()
		}
		m.editor.SetCursor(at)
	}
	return m, w.a, h
}

func wantSent(t *testing.T, a *fakeAdapter, want ...string) {
	t.Helper()
	if !reflect.DeepEqual(a.sent, want) {
		t.Errorf("sent %q, want %q", a.sent, want)
	}
}

func TestRunSendsOnlyTheStatementUnderTheCursor(t *testing.T) {
	m, a, _ := runner(t, `SELECT 1; SELECT 2; SELECT 3`, 14)
	m = pressKey(t, m, ctrlR)
	wantSent(t, a, `SELECT 2`)
	if !strings.HasSuffix(m.status, "· statement 2 of 3") {
		t.Errorf("status = %q, want it to end with the position", m.status)
	}
}

func TestRunFindsTheStatementAfterWideCharacters(t *testing.T) {
	m, a, _ := runner(t, `SELECT '世世世世'; SELECT 2; SELECT 3`, 17)
	pressKey(t, m, ctrlR)
	wantSent(t, a, `SELECT 2`)
}

func TestRunWithTheCursorAfterAFinalSemicolonRunsTheStatement(t *testing.T) {
	m, a, _ := runner(t, `SELECT 1;`, -1)
	m = pressKey(t, m, ctrlR)
	wantSent(t, a, `SELECT 1`)
	if strings.Contains(m.status, "statement") {
		t.Errorf("status = %q; one Statement gets no position", m.status)
	}
}

func TestAWriteOnTheNextLineDoesNotRunFromTheEndOfTheLineAbove(t *testing.T) {
	for _, line := range []string{`SELECT 1;`, `SELECT 1; -- note`} {
		m, a, _ := runner(t, line+"\nDELETE FROM t", len(line))
		pressKey(t, m, ctrlR)
		wantSent(t, a, `SELECT 1`)
	}
}

func TestF5RunsAsCtrlRDoes(t *testing.T) {
	m, a, _ := runner(t, `SELECT 1; SELECT 2; SELECT 3`, 14)
	pressKey(t, m, tea.KeyMsg{Type: tea.KeyF5})
	wantSent(t, a, `SELECT 2`)
}

func TestRunFromBlankSpaceFindsTheStatementItMeans(t *testing.T) {
	cases := []struct {
		name, text string
		line       int
		want       string
	}{
		{"a blank line directly above a Statement", "SELECT 1;\n\nSELECT 2", 1, `SELECT 2`},
		{"a comment-only piece after one", "SELECT 1;\n-- a note\n", 2, `SELECT 1`},
		{"a blank piece between two", "SELECT 1;\n  ;\nSELECT 2", 1, `SELECT 1`},
		{"the last of several blank lines", "SELECT 1;\n\n\nSELECT 2", 2, `SELECT 2`},
		{"the first of several blank lines", "SELECT 1;\n\n\nDELETE FROM t", 1, `SELECT 1`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m, a, _ := runner(t, c.text, 0)
			for range c.line {
				m.editor.CursorDown()
			}
			pressKey(t, m, ctrlR)
			wantSent(t, a, c.want)
		})
	}
}

func TestRunReadsTheStatementAsTheOpenSourceReadsStrings(t *testing.T) {
	const text = `SELECT 'a\'; SELECT 2; -- '`
	for _, c := range []struct {
		dialect sqltext.Dialect
		want    string
	}{
		{sqltext.Dialect{}, `SELECT 'a\'`},
		{mysql.Kind.Dialect, `SELECT 'a\'; SELECT 2; -- '`},
	} {
		m, a, _ := runner(t, text, 0)
		m.current.Dialect = c.dialect
		pressKey(t, m, ctrlR)
		wantSent(t, a, c.want)
	}
}

func TestRunOnPostgreSQLSendsNoStatementFromInsideANestedComment(t *testing.T) {
	const text = "/* outer /* inner */ ;\nDELETE FROM x;\n*/ SELECT 1;"
	m, a, _ := runner(t, text, 0)
	m.current.Dialect = postgres.Kind.Dialect
	m.editor.CursorDown()
	m.editor.SetCursor(len("DELETE "))
	pressKey(t, m, ctrlR)
	wantSent(t, a, strings.TrimSuffix(text, ";"))
}

func TestRunSendsNothingWhenThereIsNothingToRun(t *testing.T) {
	cases := []struct {
		name, text string
		selectAll  bool
	}{
		{"an empty editor", ``, false},
		{"only space", "  \n ", false},
		{"only a comment", `-- a note`, false},
		{"a Selection of only space", "SELECT 1;\n   \n", true},
		{"a Selection of only a comment", "SELECT 1;\n-- a note", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m, a, h := runner(t, c.text, -1)
			if c.selectAll {
				m, _ = send(t, m, tea.KeyMsg{Type: tea.KeyShiftUp})
			}
			m = pressKey(t, m, ctrlR)
			wantSent(t, a)
			if m.status != "nothing to run" {
				t.Errorf("status = %q, want it to say nothing to run", m.status)
			}
			if m.query.running() {
				t.Error("a query is running")
			}
			if len(h.entries) != 0 {
				t.Errorf("history holds %v", h.entries)
			}
		})
	}
}

func TestRunWithNoSourceAndNothingToRunSaysNothingToRun(t *testing.T) {
	m := pressKey(t, newBare(t, 100, 30), ctrlR)
	if m.status != "nothing to run" {
		t.Errorf("status = %q", m.status)
	}
}

func TestRunWithNoSourceSaysSo(t *testing.T) {
	m := newBare(t, 100, 30)
	m.SetQuery(`SELECT 1`)
	if m = pressKey(t, m, ctrlR); m.status != "no Source is open" {
		t.Errorf("status = %q", m.status)
	}
}

func TestASelectionIsSentAsSelectedWhereverTheCursorIs(t *testing.T) {
	m, a, h := runner(t, `SELECT 1; SELECT 2`, 0)
	for range 8 {
		m, _ = send(t, m, tea.KeyMsg{Type: tea.KeyShiftRight})
	}
	m = pressKey(t, m, ctrlR)
	wantSent(t, a, `SELECT 1`)
	if strings.Contains(m.status, "statement") {
		t.Errorf("status = %q; a Selection gets no position", m.status)
	}
	if got := h.entries; len(got) != 1 || got[0].SQL != `SELECT 1` {
		t.Errorf("history holds %v, want what was sent", got)
	}
}

func TestASelectionIsTrimmedAndItsStatementsReachTheAdapterAsOneText(t *testing.T) {
	const text = "  BEGIN; SELECT 1; COMMIT  \n"
	m, a, _ := runner(t, text, 0)
	m, _ = send(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}, Alt: true})
	pressKey(t, m, ctrlR)
	wantSent(t, a, `BEGIN; SELECT 1; COMMIT`)
}

func TestSeveralCursorsRefuseUntilEscTakesThemAway(t *testing.T) {
	m, a, h := runner(t, "SELECT 1;\nSELECT 2", 0)
	m, _ = send(t, m, tea.KeyMsg{Type: tea.KeyCtrlDown, Alt: true})
	if m.editor.CursorCount() != 2 {
		t.Fatalf("%d cursors, want 2", m.editor.CursorCount())
	}
	m = pressKey(t, m, ctrlR)
	wantSent(t, a)
	if m.status != "several cursors: press esc first" {
		t.Errorf("status = %q", m.status)
	}
	if len(h.entries) != 0 || m.query.running() {
		t.Errorf("history %v, running %v; a refusal records and starts nothing", h.entries, m.query.running())
	}

	m, _ = send(t, m, esc)
	pressKey(t, m, ctrlR)
	if len(a.sent) != 1 {
		t.Errorf("sent %q after esc, want one Statement", a.sent)
	}
}

func TestEscTakesSeveralCursorsAwayWhicheverPaneHasTheFocus(t *testing.T) {
	for _, presses := range []int{1, 2} {
		m, a, _ := runner(t, "SELECT 1;\nSELECT 2", 0)
		m, _ = send(t, m, ctrlAltDown)
		for range presses {
			m, _ = send(t, m, tab)
		}
		m = pressKey(t, m, ctrlR)
		wantSent(t, a)
		m, _ = send(t, m, esc)
		pressKey(t, m, ctrlR)
		if len(a.sent) != 1 {
			t.Errorf("sent %q after esc, want one Statement", a.sent)
		}
	}
}

func TestEscOutsideTheEditorCancelsARunningQueryBeforeItTakesCursorsAway(t *testing.T) {
	m, a, _ := runner(t, "SELECT 1;\nSELECT 2", 0)
	a.block = true
	m, cmd := send(t, m, ctrlR)
	defer runCmd(cmd)
	m, _ = send(t, m, ctrlAltDown)
	m, _ = send(t, m, tab)
	m, _ = send(t, m, esc)
	if m.status != "cancelled" || m.editor.CursorCount() != 2 {
		t.Errorf("status %q, %d cursors; want the query cancelled and the cursors left", m.status, m.editor.CursorCount())
	}
}

func TestRunUsesTheEditorsCursorWhicheverPaneHasTheFocus(t *testing.T) {
	for _, presses := range []int{1, 2} {
		m, a, _ := runner(t, `SELECT 1; SELECT 2; SELECT 3`, 14)
		for range presses {
			m, _ = send(t, m, tab)
		}
		m = pressKey(t, m, ctrlR)
		wantSent(t, a, `SELECT 2`)
		if !strings.HasSuffix(m.status, "· statement 2 of 3") {
			t.Errorf("status = %q", m.status)
		}
	}
}

func TestRunUsesTheEditorsSelectionWhicheverPaneHasTheFocus(t *testing.T) {
	m, a, _ := runner(t, `SELECT 1; SELECT 2`, 0)
	for range 8 {
		m, _ = send(t, m, tea.KeyMsg{Type: tea.KeyShiftRight})
	}
	m, _ = send(t, m, tab)
	pressKey(t, m, ctrlR)
	wantSent(t, a, `SELECT 1`)
}

func TestHistoryHoldsOnlyWhatRanAndSteppingBackLoadsIt(t *testing.T) {
	m, a, h := runner(t, `SELECT 1; SELECT 2; SELECT 3`, 14)
	m = pressKey(t, m, ctrlR)
	wantSent(t, a, `SELECT 2`)
	if len(h.entries) != 1 || h.entries[0].SQL != `SELECT 2` {
		t.Fatalf("history holds %v, want only what ran", h.entries)
	}
	m, _ = send(t, m, tea.KeyMsg{Type: tea.KeyCtrlUp})
	if got := m.editor.Value(); got != `SELECT 2` {
		t.Errorf("stepping back loaded %q", got)
	}
}

func TestAFailingStatementKeepsThePosition(t *testing.T) {
	m, a, _ := runner(t, `SELECT 1; SELECT 2`, 0)
	a.err = errors.New("no such table")
	m = pressKey(t, m, ctrlR)
	if m.status != "no such table · statement 1 of 2" {
		t.Errorf("status = %q", m.status)
	}
}

func TestASingleStatementThatFailsAddsNothing(t *testing.T) {
	m, a, _ := runner(t, `SELECT 1;`, 0)
	a.err = errors.New("no such table")
	if m = pressKey(t, m, ctrlR); m.status != "no such table" {
		t.Errorf("status = %q", m.status)
	}
}

func TestACancelledStatementAddsNothing(t *testing.T) {
	m, a, _ := runner(t, `SELECT 1; SELECT 2`, 0)
	a.block = true
	m, cmd := send(t, m, ctrlR)
	if cmd == nil {
		t.Fatal("no command")
	}
	m, _ = send(t, m, esc)
	m, _ = send(t, m, cmd())
	if m.status != "cancelled" {
		t.Errorf("status = %q", m.status)
	}
}

func TestALongErrorKeepsThePositionOnScreen(t *testing.T) {
	m, a, _ := runner(t, `SELECT 1; SELECT 2`, 0)
	a.err = errors.New(strings.Repeat("no such table ", 40))
	m = pressKey(t, m, ctrlR)
	if view := plain(m); !strings.Contains(view, "… · statement 1 of 2") {
		t.Errorf("the position is not on screen:\n%s", view)
	}
	fits(t, m)
}

func TestALongStatusFromElsewhereIsCutAsBefore(t *testing.T) {
	m, _, _ := runner(t, `SELECT 1; SELECT 2`, 0)
	m = pressKey(t, m, ctrlR)
	m.status = strings.Repeat("something else ", 40)
	if view := plain(m); strings.Contains(view, "statement 1 of 2") || !strings.Contains(view, "something else") {
		t.Errorf("view:\n%s", view)
	}
}

func TestThePositionStaysLastAfterANotice(t *testing.T) {
	m, _, _ := runner(t, `SELECT 1; SELECT 2`, 0)
	m.notice = "history is off"
	m = pressKey(t, m, ctrlR)
	if !strings.Contains(m.status, "history is off") || !strings.HasSuffix(m.status, "· statement 1 of 2") {
		t.Errorf("status = %q", m.status)
	}
}

func TestThePositionFitsAVeryNarrowWindow(t *testing.T) {
	m, a, _ := runner(t, `SELECT 1; SELECT 2`, 0)
	a.err = errors.New("no such table")
	m = pressKey(t, m, ctrlR)
	m, _ = send(t, m, tea.WindowSizeMsg{Width: 10, Height: 30})
	for _, row := range lines(plain(m)) {
		if w := ansi.StringWidth(row); w > 10 && strings.Contains(row, "statement") {
			t.Errorf("status row is %d columns wide in a window of 10: %q", w, row)
		}
	}
}

func TestACancelledResultThatArrivesAddsNothing(t *testing.T) {
	m, a, _ := runner(t, `SELECT 1; SELECT 2`, 0)
	a.err = context.Canceled
	if m = pressKey(t, m, ctrlR); m.status != "cancelled" {
		t.Errorf("status = %q", m.status)
	}
}

func TestAPositionFromAnEarlierRunDoesNotFollowALaterOne(t *testing.T) {
	m, _, _ := runner(t, `SELECT 1; SELECT 2`, 0)
	m = pressKey(t, m, ctrlR)
	if !strings.Contains(m.status, "statement 1 of 2") {
		t.Fatalf("status = %q", m.status)
	}
	m.SetQuery(`SELECT 3`)
	if m = pressKey(t, m, ctrlR); strings.Contains(m.status, "statement") {
		t.Errorf("status = %q", m.status)
	}
}
