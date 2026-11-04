package tui

import (
	"errors"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

func TestSwitchOpensTheNewSourceThenClosesTheOldOne(t *testing.T) {
	w := newWorld()
	m := w.model(t)

	m, cmd := send(t, m, switchMsg{w.sw.choices["b"]})
	if got := m.status; !strings.Contains(got, "opening source-b") {
		t.Errorf("status while opening = %q", got)
	}
	if m.adapter != w.a {
		t.Fatal("the current Source must stay in place until the new one has opened")
	}
	m = drain(t, m, cmd)

	if m.adapter != w.b || m.source != "source-b" || !m.writeMode || m.maxRows != 7 {
		t.Errorf("after the switch: adapter %v, title %q, write %v, cap %d; want b / source-b / write / 7",
			m.adapter == w.b, m.source, m.writeMode, m.maxRows)
	}
	if got, want := strings.Join(w.ev.all(), ","), "open b,close a"; got != want {
		t.Errorf("events = %s, want %s: the old Source closes only after the new one opened", got, want)
	}
	if len(m.schema.tables) != 1 || m.schema.tables[0].Name != "from_b" {
		t.Errorf("schema tree = %+v, want the new Source's tables", m.schema.tables)
	}
	if !strings.Contains(m.View(), "source-b") || !strings.Contains(m.View(), "[write enabled]") {
		t.Errorf("title does not show the new Source:\n%s", m.View())
	}
}

func TestSwitchingCancelsARunningQueryBeforeAnythingIsOpened(t *testing.T) {
	w := newWorld()
	w.a.block = true
	m := w.model(t)
	m.SetQuery("SELECT 1")

	m, run := send(t, m, tea.KeyMsg{Type: tea.KeyCtrlR})
	if !m.query.running() {
		t.Fatal("the query did not start")
	}
	finished := make(chan tea.Msg, 1)
	go func() { finished <- run() }()

	m, open := send(t, m, switchMsg{w.sw.choices["b"]})
	if m.query.running() {
		t.Error("the running query was not cancelled by the switch request")
	}
	if w.ev.count("open b") != 0 {
		t.Error("the new Source was opened before the cancel")
	}
	m = drain(t, m, open)

	select {
	case msg := <-finished:
		m, _ = send(t, m, msg)
	case <-time.After(5 * time.Second):
		t.Fatal("the cancelled query did not stop")
	}
	if len(m.grid.rows) != 0 {
		t.Errorf("a result from the old Source leaked in: %d rows", len(m.grid.rows))
	}
}

func TestAResultFromTheOldSourceIsIgnoredAfterTheSwitch(t *testing.T) {
	w := newWorld()
	m := w.model(t)
	m.SetQuery("SELECT 1")

	m, run := send(t, m, tea.KeyMsg{Type: tea.KeyCtrlR})
	late := run()

	m, open := send(t, m, switchMsg{w.sw.choices["b"]})
	m = drain(t, m, open)
	m, _ = send(t, m, late)

	if len(m.grid.rows) != 0 {
		t.Errorf("rows from the old Source appeared after the switch: %d", len(m.grid.rows))
	}
}

func TestAQueryStartedWhileTheNewSourceOpensIsDropped(t *testing.T) {
	w := newWorld()
	m := w.model(t)

	m, open := send(t, m, switchMsg{w.sw.choices["b"]})
	m.SetQuery("SELECT 1")
	m, run := send(t, m, tea.KeyMsg{Type: tea.KeyCtrlR})
	if !m.query.running() {
		t.Fatal("the query did not start on the old Source")
	}
	late := run()

	m = drain(t, m, open)
	if m.query.running() {
		t.Error("a query on the Source that was left is still marked as running")
	}
	m, _ = send(t, m, late)
	if len(m.grid.rows) != 0 {
		t.Errorf("rows from the old Source appeared after the switch: %d", len(m.grid.rows))
	}
}

func TestAFailedOpenKeepsTheCurrentSource(t *testing.T) {
	w := newWorld()
	w.sw.errs["b"] = errors.New("connection refused\x1b[2J")
	m := w.model(t)

	m, cmd := send(t, m, switchMsg{w.sw.choices["b"]})
	m = drain(t, m, cmd)

	if m.adapter != w.a || m.source != "source-a" || m.writeMode || m.maxRows != 100 {
		t.Error("a failed open changed the current Source")
	}
	if !m.isError || !strings.Contains(m.status, "connection refused") || !strings.Contains(m.status, "source-b") {
		t.Errorf("status = %q (error %v), want the failure and the Source it was for", m.status, m.isError)
	}
	if strings.Contains(m.status, "\x1b") {
		t.Errorf("the error carried an escape sequence onto the screen: %q", m.status)
	}
	if w.ev.count("close a") != 0 {
		t.Error("the current Source was closed although the switch failed")
	}
	m.SetQuery("SELECT 1")
	m, run := send(t, m, tea.KeyMsg{Type: tea.KeyCtrlR})
	m = drain(t, m, run)
	if len(m.grid.rows) != 1 {
		t.Errorf("the current Source stopped working: %d rows, status %q", len(m.grid.rows), m.status)
	}
}

func TestASupersededSwitchLeaksNoAdapter(t *testing.T) {
	w := newWorld()
	c := &fakeAdapter{name: "c", ev: w.ev}
	w.sw.adapters["c"] = c
	w.sw.choices["c"] = Choice{ID: "c", Label: "source-c"}
	w.sw.maxRows["c"] = 50
	m := w.model(t)

	m, first := send(t, m, switchMsg{w.sw.choices["b"]})
	m, second := send(t, m, switchMsg{w.sw.choices["c"]})
	m = drain(t, m, second)
	m = drain(t, m, first)

	if m.adapter != c {
		t.Fatalf("the later request must win, but the current Source is %v", m.source)
	}
	opened, closed := 0, 0
	for _, e := range w.ev.all() {
		switch {
		case strings.HasPrefix(e, "open "):
			opened++
		case strings.HasPrefix(e, "close "):
			closed++
		}
	}
	if opened != 2 || closed != 2 {
		t.Errorf("events %v: want 2 opens and 2 closes (a and b closed, c left open)", w.ev.all())
	}
	if w.ev.count("close b") != 1 || w.ev.count("close a") != 1 || w.ev.count("close c") != 0 {
		t.Errorf("events = %v", w.ev.all())
	}
}

func TestASchemaLoadedForTheOldSourceIsIgnored(t *testing.T) {
	w := newWorld()
	m := w.model(t)
	oldGen := m.adapterGen

	m, cmd := send(t, m, switchMsg{w.sw.choices["b"]})
	m = drain(t, m, cmd)
	m, _ = send(t, m, schemaMsg{gen: oldGen, tables: w.a.tables})

	if len(m.schema.tables) != 1 || m.schema.tables[0].Name != "from_b" {
		t.Errorf("schema tree = %+v, want only the new Source's tables", m.schema.tables)
	}
}

func TestSwitchingToTheCurrentSourceDoesNothing(t *testing.T) {
	w := newWorld()
	m := w.model(t)
	m, cmd := send(t, m, switchMsg{w.sw.choices["a"]})
	if cmd != nil || w.ev.count("open a") != 0 {
		t.Error("switching to the current Source opened it again")
	}
}

func TestACanStartWithoutASource(t *testing.T) {
	w := newWorld()
	m := New(nil, "", WithSwitcher(w.sw, Choice{}))
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m = updated.(Model)

	if m.Init() == nil {
		t.Fatal("Init returned nothing")
	}
	if !strings.Contains(m.View(), "no Source open") {
		t.Errorf("title does not say there is no Source:\n%s", m.View())
	}
	m.SetQuery("SELECT 1")
	m, cmd := send(t, m, tea.KeyMsg{Type: tea.KeyCtrlR})
	if cmd != nil || !strings.Contains(m.status, "no Source is open") {
		t.Errorf("running without a Source: cmd %v, status %q", cmd != nil, m.status)
	}

	m, cmd = send(t, m, switchMsg{w.sw.choices["a"]})
	m = drain(t, m, cmd)
	if m.adapter != w.a || len(m.schema.tables) != 1 {
		t.Error("the first Source did not open from the no-Source state")
	}
}
