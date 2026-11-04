package tui

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/EdwardWhalley/lode/internal/adapter"
	"github.com/EdwardWhalley/lode/internal/history"
)

type fakeHistory struct {
	entries []history.Entry
	failAdd error
	adds    int
}

func (f *fakeHistory) Add(source, sql string) error {
	f.adds++
	if f.failAdd != nil {
		return f.failAdd
	}
	f.entries = append(f.entries, history.Entry{Time: time.Now(), Source: source, SQL: sql})
	return nil
}

func (f *fakeHistory) Entries() []history.Entry { return f.entries }

var (
	ctrlUp   = tea.KeyMsg{Type: tea.KeyCtrlUp}
	ctrlDown = tea.KeyMsg{Type: tea.KeyCtrlDown}
	altUp    = tea.KeyMsg{Type: tea.KeyUp, Alt: true}
	altDown  = tea.KeyMsg{Type: tea.KeyDown, Alt: true}
)

// failingAdapter refuses every query.
type failingAdapter struct{}

func (failingAdapter) Close() error                                    { return nil }
func (failingAdapter) Schema(context.Context) ([]adapter.Table, error) { return nil, nil }
func (failingAdapter) Query(context.Context, string) (adapter.Rows, error) {
	return nil, errors.New("syntax error")
}

func entriesFor(source string, sqls ...string) []history.Entry {
	var out []history.Entry
	for _, q := range sqls {
		out = append(out, history.Entry{Source: source, SQL: q})
	}
	return out
}

func historyModel(t *testing.T, h History, a adapter.Adapter) Model {
	t.Helper()
	m := New(a, "label-a", WithHistory(h), WithSwitcher(newWorld().sw, Choice{ID: "a", Name: "a", Label: "label-a"}))
	m, _ = send(t, m, tea.WindowSizeMsg{Width: 100, Height: 30})
	return m
}

func TestARunIsRecordedUnderTheSourceName(t *testing.T) {
	h := &fakeHistory{}
	m := historyModel(t, h, valuesAdapter{cols: []string{"n"}, rows: [][]any{{int64(1)}}})
	m.SetQuery("  SELECT 1  ")
	m, run := send(t, m, tea.KeyMsg{Type: tea.KeyCtrlR})
	m = drain(t, m, run)

	if len(h.entries) != 1 || h.entries[0].Source != "a" || h.entries[0].SQL != "SELECT 1" {
		t.Errorf("entries = %+v, want SELECT 1 under the Source name a", h.entries)
	}
}

func TestAFailingQueryIsRecordedToo(t *testing.T) {
	h := &fakeHistory{}
	m := historyModel(t, h, failingAdapter{})
	m.SetQuery("SELEKT 1")
	m, run := send(t, m, tea.KeyMsg{Type: tea.KeyCtrlR})
	m = drain(t, m, run)

	if !m.isError {
		t.Fatal("the query should have failed")
	}
	if len(h.entries) != 1 || h.entries[0].SQL != "SELEKT 1" {
		t.Errorf("entries = %+v: you want to fix and re-run a failed query", h.entries)
	}
}

func TestAnAdHocSourceIsRecordedByItsSafeLabel(t *testing.T) {
	h := &fakeHistory{}
	m := New(valuesAdapter{cols: []string{"n"}, rows: [][]any{{int64(1)}}}, "postgres://reader:xxxxx@db.example.com/app", WithHistory(h))
	m, _ = send(t, m, tea.WindowSizeMsg{Width: 100, Height: 30})
	m.SetQuery("SELECT 1")
	m, run := send(t, m, tea.KeyMsg{Type: tea.KeyCtrlR})
	m = drain(t, m, run)

	if len(h.entries) != 1 || h.entries[0].Source != "postgres://reader:xxxxx@db.example.com/app" {
		t.Fatalf("entries = %+v", h.entries)
	}
	if strings.Contains(h.entries[0].Source, "hunter2") {
		t.Error("a password reached the history")
	}
}

func TestCtrlUpAndDownStepThroughTheSourcesHistoryAndRestoreTheDraft(t *testing.T) {
	h := &fakeHistory{entries: append(append(entriesFor("a", "q1", "q2"), entriesFor("b", "other")...), entriesFor("a", "q3")...)}
	m := historyModel(t, h, valuesAdapter{})
	m.SetQuery("my draft")

	want := func(text string) {
		t.Helper()
		if got := m.editor.Value(); got != text {
			t.Fatalf("editor = %q, want %q", got, text)
		}
	}
	m, _ = send(t, m, ctrlUp)
	want("q3")
	m, _ = send(t, m, ctrlUp)
	want("q2")
	m, _ = send(t, m, ctrlUp)
	want("q1")
	m, _ = send(t, m, ctrlUp)
	want("q1")
	m, _ = send(t, m, ctrlDown)
	want("q2")
	m, _ = send(t, m, ctrlDown)
	want("q3")
	m, cmd := send(t, m, ctrlDown)
	want("my draft")
	if cmd != nil || m.query.running() {
		t.Error("stepping must never run a query")
	}
	m, _ = send(t, m, ctrlDown)
	want("my draft")
}

func TestAltArrowsStepToo(t *testing.T) {
	h := &fakeHistory{entries: entriesFor("a", "q1", "q2")}
	m := historyModel(t, h, valuesAdapter{})
	m, _ = send(t, m, altUp)
	m, _ = send(t, m, altUp)
	if m.editor.Value() != "q1" {
		t.Errorf("editor = %q after two alt+up", m.editor.Value())
	}
	m, _ = send(t, m, altDown)
	if m.editor.Value() != "q2" {
		t.Errorf("editor = %q after alt+down", m.editor.Value())
	}
}

func TestSteppingWithNoHistoryForTheSourceSaysSo(t *testing.T) {
	h := &fakeHistory{entries: entriesFor("b", "other")}
	m := historyModel(t, h, valuesAdapter{})
	m.SetQuery("typed")
	m, _ = send(t, m, ctrlUp)
	if m.editor.Value() != "typed" || !strings.Contains(m.status, "no history for a") {
		t.Errorf("editor %q, status %q", m.editor.Value(), m.status)
	}
}

func TestRunningAQueryEndsSteppingSoTheNewOneIsFirst(t *testing.T) {
	h := &fakeHistory{entries: entriesFor("a", "old")}
	m := historyModel(t, h, valuesAdapter{cols: []string{"n"}, rows: [][]any{{int64(1)}}})
	m, _ = send(t, m, ctrlUp)
	m.SetQuery("SELECT 'new'")
	m, run := send(t, m, tea.KeyMsg{Type: tea.KeyCtrlR})
	m = drain(t, m, run)
	m.SetQuery("")
	m, _ = send(t, m, ctrlUp)
	if m.editor.Value() != "SELECT 'new'" {
		t.Errorf("editor = %q, want the query just run", m.editor.Value())
	}
}

func TestStepsOnlyInTheEditor(t *testing.T) {
	h := &fakeHistory{entries: entriesFor("a", "q1")}
	m := historyModel(t, h, valuesAdapter{})
	m.SetQuery("typed")
	m, _ = send(t, m, tea.KeyMsg{Type: tea.KeyTab})
	m, _ = send(t, m, ctrlUp)
	if m.editor.Value() != "typed" {
		t.Errorf("ctrl+up in the grid changed the editor to %q", m.editor.Value())
	}
}

func TestWithoutHistoryTheKeysDoNothingAndNothingBreaks(t *testing.T) {
	m := New(valuesAdapter{cols: []string{"n"}, rows: [][]any{{int64(1)}}}, "x")
	m, _ = send(t, m, tea.WindowSizeMsg{Width: 100, Height: 30})
	m.SetQuery("typed")
	m, _ = send(t, m, ctrlUp)
	if m.editor.Value() != "typed" {
		t.Errorf("editor = %q", m.editor.Value())
	}
	m, run := send(t, m, tea.KeyMsg{Type: tea.KeyCtrlR})
	m = drain(t, m, run)
	if m.isError || len(m.grid.rows) != 1 {
		t.Errorf("a query failed with history off: %q", m.status)
	}
}

func TestAHistoryFailureTurnsItOffForTheSessionAndQueriesCarryOn(t *testing.T) {
	h := &fakeHistory{failAdd: errors.New("disk full\x1b[2J")}
	m := historyModel(t, h, valuesAdapter{cols: []string{"n"}, rows: [][]any{{int64(1)}}})

	m.SetQuery("SELECT 1")
	m, run := send(t, m, tea.KeyMsg{Type: tea.KeyCtrlR})
	m = drain(t, m, run)
	if len(m.grid.rows) != 1 {
		t.Fatalf("the query did not run: %q", m.status)
	}
	if !strings.Contains(m.status, "history is off for this session") || !strings.Contains(m.status, "disk full") {
		t.Errorf("status = %q, want one message saying history is off and why", m.status)
	}
	if strings.Contains(m.View(), "\x1b[2J") {
		t.Error("the error carried an escape sequence onto the screen")
	}

	m.SetQuery("SELECT 2")
	m, run = send(t, m, tea.KeyMsg{Type: tea.KeyCtrlR})
	m = drain(t, m, run)
	if h.adds != 1 {
		t.Errorf("history was written to %d times, want just the one that failed", h.adds)
	}
	if strings.Contains(m.status, "history is off") {
		t.Errorf("the message should be shown once, not on every query: %q", m.status)
	}
	if m.history != nil {
		t.Error("history should stay off for the rest of the session")
	}
}

func TestANoticeIsShownAtStart(t *testing.T) {
	m := New(valuesAdapter{}, "x", WithNotice("history is off for this session: no state directory"))
	if !strings.Contains(m.status, "history is off") {
		t.Errorf("status = %q", m.status)
	}
}

func TestSwitchingSourceChangesWhichHistoryIsStepped(t *testing.T) {
	w := newWorld()
	h := &fakeHistory{entries: append(entriesFor("a", "from a"), entriesFor("b", "from b")...)}
	m := New(w.a, "source-a", WithSwitcher(w.sw, w.sw.choices["a"]), WithHistory(h))
	m, _ = send(t, m, tea.WindowSizeMsg{Width: 100, Height: 30})

	m, _ = send(t, m, ctrlUp)
	if m.editor.Value() != "from a" {
		t.Fatalf("editor = %q", m.editor.Value())
	}
	m, cmd := send(t, m, switchMsg{w.sw.choices["b"]})
	m = drain(t, m, cmd)
	m.SetQuery("")
	m, _ = send(t, m, ctrlUp)
	if m.editor.Value() != "from b" {
		t.Errorf("after switching to b the editor = %q, want b's history", m.editor.Value())
	}
}

func TestHistoryKeysAreInTheLegend(t *testing.T) {
	m := historyModel(t, &fakeHistory{}, valuesAdapter{})
	parts := strings.Split(m.View(), "\n")
	if got := ansi.Strip(parts[len(parts)-1]); !strings.Contains(got, "history") {
		t.Errorf("hint row = %q, want the history keys", got)
	}
}
