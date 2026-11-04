package tui

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

var (
	ctrlO = tea.KeyMsg{Type: tea.KeyCtrlO}
	enter = tea.KeyMsg{Type: tea.KeyEnter}
	esc   = tea.KeyMsg{Type: tea.KeyEsc}
	down  = tea.KeyMsg{Type: tea.KeyDown}
)

func TestCtrlOOpensTheSelectorFromEveryPane(t *testing.T) {
	for _, panes := range []int{0, 1, 2} {
		w := newWorld()
		m := w.model(t)
		for i := 0; i < panes; i++ {
			m, _ = send(t, m, tea.KeyMsg{Type: tea.KeyTab})
		}
		m, _ = send(t, m, ctrlO)
		if _, ok := m.overlay.(selector); !ok {
			t.Fatalf("ctrl+o did not open the selector with %d tabs", panes)
		}
		view := plain(m)
		for _, want := range []string{"Switch Source", "a", "source-a", "(current)", "b", "source-b", "[write]"} {
			if !strings.Contains(view, want) {
				t.Errorf("selector is missing %q:\n%s", want, view)
			}
		}
		if strings.Count(view, "(current)") != 1 || strings.Count(view, "[write]") != 1 {
			t.Errorf("exactly one Source is current and one write-enabled:\n%s", view)
		}
	}
}

func TestSelectorListsAnAdHocLaunchSourceAsCurrent(t *testing.T) {
	w := newWorld()
	adhoc := Choice{ID: "adhoc", Label: "people.csv"}
	w.sw.sources = func() ([]Choice, error) { return []Choice{adhoc, w.sw.choices["b"]}, nil }
	m := New(w.a, "people.csv", WithSwitcher(w.sw, adhoc))
	m, _ = send(t, m, tea.WindowSizeMsg{Width: 100, Height: 30})
	m, _ = send(t, m, ctrlO)
	view := plain(m)
	if !strings.Contains(view, "(ad-hoc)") || !strings.Contains(view, "people.csv") || !strings.Contains(view, "(current)") {
		t.Errorf("the ad-hoc launch Source should be listed and marked current:\n%s", view)
	}
	if got := m.overlay.(selector).cursor; got != 0 {
		t.Errorf("the cursor starts on the current Source, not %d", got)
	}
}

func TestSelectingASourceSwitchesAndKeepsTheEditorText(t *testing.T) {
	w := newWorld()
	m := w.model(t)
	m.SetQuery("SELECT * FROM t WHERE id = 1")

	m, _ = send(t, m, ctrlO)
	m, _ = send(t, m, down)
	m, cmd := send(t, m, enter)
	if m.overlay != nil {
		t.Error("enter did not close the selector")
	}
	m = drain(t, m, cmd)

	if m.adapter != w.b || m.source != "source-b" {
		t.Fatalf("not switched: %q", m.source)
	}
	if got := m.editor.Value(); got != "SELECT * FROM t WHERE id = 1" {
		t.Errorf("the editor text changed to %q", got)
	}
	if len(m.schema.tables) != 1 || m.schema.tables[0].Name != "from_b" {
		t.Errorf("schema tree = %+v, want the new Source's tables", m.schema.tables)
	}
}

func TestChoosingTheCurrentSourceJustCloses(t *testing.T) {
	w := newWorld()
	m := w.model(t)
	m, _ = send(t, m, ctrlO)
	m, cmd := send(t, m, enter)
	m = drain(t, m, cmd)
	if m.overlay != nil || w.ev.count("open a") != 0 || m.adapter != w.a {
		t.Error("choosing the current Source should change nothing")
	}
}

func TestEscCancelsTheSelector(t *testing.T) {
	w := newWorld()
	m := w.model(t)
	m, _ = send(t, m, ctrlO)
	m, _ = send(t, m, down)
	m, cmd := send(t, m, esc)
	if m.overlay != nil || cmd != nil || m.adapter != w.a {
		t.Error("esc should close the selector and change nothing")
	}
}

func TestOpeningTheSelectorReReadsTheConfiguration(t *testing.T) {
	w := newWorld()
	c := Choice{ID: "c", Name: "c", Label: "source-c"}
	reads := 0
	w.sw.sources = func() ([]Choice, error) {
		reads++
		list := []Choice{w.sw.choices["a"], w.sw.choices["b"]}
		if reads >= 2 {
			list = append(list, c)
		}
		return list, nil
	}
	m := w.model(t)

	m, _ = send(t, m, ctrlO)
	if strings.Contains(plain(m), "source-c") {
		t.Error("c is not in the config yet")
	}
	m, _ = send(t, m, esc)
	m, _ = send(t, m, ctrlO)
	if !strings.Contains(plain(m), "source-c") {
		t.Errorf("a Source added to the config did not appear:\n%s", plain(m))
	}
}

func TestABrokenConfigKeepsTheOldListAndShowsTheError(t *testing.T) {
	w := newWorld()
	fail := false
	w.sw.sources = func() ([]Choice, error) {
		if fail {
			return nil, errors.New("sources.toml: unset variable ${PW}\x1b[2J")
		}
		return []Choice{w.sw.choices["a"], w.sw.choices["b"]}, nil
	}
	m := w.model(t)
	m, _ = send(t, m, ctrlO)
	m, _ = send(t, m, esc)

	fail = true
	m, _ = send(t, m, ctrlO)
	view := m.View()
	if !strings.Contains(plain(m), "source-b") {
		t.Errorf("the previous list should stay:\n%s", plain(m))
	}
	if !m.isError || !strings.Contains(m.status, "unset variable") {
		t.Errorf("status = %q (error %v), want the config error", m.status, m.isError)
	}
	if strings.Contains(view, "\x1b[2J") {
		t.Error("the error carried an escape sequence onto the screen")
	}
}

func TestStartingWithoutASourceOpensTheSelectorAndEscQuits(t *testing.T) {
	w := newWorld()
	m := New(nil, "", WithSwitcher(w.sw, Choice{}), WithSelectorAtStart())
	m, _ = send(t, m, tea.WindowSizeMsg{Width: 100, Height: 30})

	sel, ok := m.overlay.(selector)
	if !ok {
		t.Fatal("the selector is not open at start")
	}
	if !strings.Contains(plain(m), "Choose a Source") || strings.Contains(plain(m), "(current)") {
		t.Errorf("with no Source open nothing is current:\n%s", plain(m))
	}
	if !sel.quitOnEsc {
		t.Fatal("esc must quit when there is no Source to go back to")
	}
	_, cmd := send(t, m, esc)
	if cmd == nil {
		t.Fatal("esc returned no command")
	}
	if _, quit := cmd().(tea.QuitMsg); !quit {
		t.Error("esc with no Source open should quit")
	}
}

func TestTheFirstSourceChosenGetsAStartingQueryAndLaterOnesDoNot(t *testing.T) {
	w := newWorld()
	m := New(nil, "", WithSwitcher(w.sw, Choice{}), WithSelectorAtStart())
	m, _ = send(t, m, tea.WindowSizeMsg{Width: 100, Height: 30})

	m, cmd := send(t, m, enter)
	m = drain(t, m, cmd)
	if got, want := m.editor.Value(), `SELECT * FROM "from_a" LIMIT 100`; got != want {
		t.Fatalf("starting query = %q, want %q", got, want)
	}

	m.SetQuery("SELECT 42")
	m, _ = send(t, m, ctrlO)
	m, _ = send(t, m, down)
	m, cmd = send(t, m, enter)
	m = drain(t, m, cmd)
	if got := m.editor.Value(); got != "SELECT 42" {
		t.Errorf("a switch changed the editor text to %q", got)
	}
}

func TestAStartingQueryNeverOverwritesWhatWasTyped(t *testing.T) {
	w := newWorld()
	m := New(nil, "", WithSwitcher(w.sw, Choice{}), WithSelectorAtStart())
	m, _ = send(t, m, tea.WindowSizeMsg{Width: 100, Height: 30})
	m.SetQuery("SELECT mine")
	m, cmd := send(t, m, enter)
	m = drain(t, m, cmd)
	if got := m.editor.Value(); got != "SELECT mine" {
		t.Errorf("editor = %q, want the typed text kept", got)
	}
}

func TestResultsFromTheOldSourceAreLabelledUntilTheNextRun(t *testing.T) {
	w := newWorld()
	m := w.model(t)
	m.SetQuery("SELECT 1")
	m, run := send(t, m, tea.KeyMsg{Type: tea.KeyCtrlR})
	m = drain(t, m, run)
	if len(m.grid.rows) != 1 || strings.Contains(plain(m), "results from") {
		t.Fatalf("a fresh result needs no label:\n%s", plain(m))
	}

	m, cmd := send(t, m, switchMsg{w.sw.choices["b"]})
	m = drain(t, m, cmd)
	if len(m.grid.rows) != 1 {
		t.Errorf("the grid was cleared by the switch: %d rows", len(m.grid.rows))
	}
	if !strings.Contains(plain(m), "results from source-a") {
		t.Errorf("the divider should name the Source the rows came from:\n%s", plain(m))
	}

	m, run = send(t, m, tea.KeyMsg{Type: tea.KeyCtrlR})
	m = drain(t, m, run)
	if strings.Contains(plain(m), "results from") {
		t.Errorf("running a query should replace the label:\n%s", plain(m))
	}
}

// A Label hides options, so two Sources can differ only there and still be two Sources.
func TestResultsFromANamedSourceWithTheSameLabelAreStillNoted(t *testing.T) {
	const same = "postgres://u@h/db?…"
	w := newWorld()
	a, b := w.sw.choices["a"], w.sw.choices["b"]
	a.Label, b.Label = same, same
	w.sw.choices["a"], w.sw.choices["b"] = a, b
	m := New(w.a, same, WithMaxRows(100), WithSwitcher(w.sw, a))
	m, _ = send(t, m, tea.WindowSizeMsg{Width: 100, Height: 30})
	m.SetQuery("SELECT 1")
	m, run := send(t, m, tea.KeyMsg{Type: tea.KeyCtrlR})
	m = drain(t, m, run)

	m, cmd := send(t, m, switchMsg{b})
	m = drain(t, m, cmd)

	if !strings.Contains(plain(m), "results from "+same) {
		t.Errorf("the divider should say the rows came from another Source:\n%s", plain(m))
	}
}

// The name is the same, but the rows came from where the Source used to point.
func TestResultsFromBeforeAnEditOfTheLocationAreStillNoted(t *testing.T) {
	w := newWorld()
	m := w.model(t)
	m.SetQuery("SELECT 1")
	m, run := send(t, m, tea.KeyMsg{Type: tea.KeyCtrlR})
	m = drain(t, m, run)

	m, cmd := send(t, m, switchMsg{w.sw.choices["b"]})
	m = drain(t, m, cmd)
	edited := w.sw.choices["a"]
	edited.Label = "source-a-moved"
	w.sw.choices["a"] = edited
	m, cmd = send(t, m, switchMsg{edited})
	m = drain(t, m, cmd)

	if !strings.Contains(plain(m), "results from source-a ") {
		t.Errorf("the divider should say the rows came from the old location:\n%s", plain(m))
	}
}

func TestTheTitleNamesASourceWhoseLabelIsHidden(t *testing.T) {
	const hidden = "(unparseable DSN)"
	for _, tc := range []struct {
		name          string
		choice        Choice
		want, notWant string
	}{
		{"named and hidden", Choice{ID: "p", Name: "prod", Label: hidden}, "prod", hidden},
		{"named and shown", Choice{ID: "p", Name: "prod", Label: "postgres://u@h/db"}, "postgres://u@h/db", "prod"},
		{"ad-hoc and hidden", Choice{ID: "adhoc", Label: hidden}, hidden, ""},
		{"a path that could drive the terminal", Choice{ID: "adhoc", Label: "a\x1b[2Jb.csv"}, "a·[2Jb.csv", "\x1b[2J"},
		{"a name that could drive the terminal", Choice{ID: "p", Name: "pr\x1b[2Jod", Label: hidden}, "pr·[2Jod", "\x1b[2J"},
	} {
		w := newWorld()
		m := New(w.a, tc.choice.Label, WithSwitcher(w.sw, tc.choice))
		m, _ = send(t, m, tea.WindowSizeMsg{Width: 100, Height: 30})

		title := strings.SplitN(m.View(), "\n", 2)[0]
		if plainTitle := strings.SplitN(plain(m), "\n", 2)[0]; !strings.Contains(plainTitle, tc.want) {
			t.Errorf("%s: title = %q, want %q in it", tc.name, plainTitle, tc.want)
		}
		if tc.notWant != "" && strings.Contains(title, tc.notWant) {
			t.Errorf("%s: title = %q, want no %q in it", tc.name, title, tc.notWant)
		}
	}
}

func TestTruncationNoticeKeepsTheCapTheResultWasCutAt(t *testing.T) {
	w := newWorld()
	m := w.model(t)
	m.held.truncated, m.held.truncatedAt, m.held.from = true, 100, "source-a"

	m, cmd := send(t, m, switchMsg{w.sw.choices["b"]})
	m = drain(t, m, cmd)
	view := plain(m)
	if !strings.Contains(view, "truncated at 100 rows") || strings.Contains(view, "truncated at 7 rows") {
		t.Errorf("the notice should still say 100:\n%s", view)
	}
}

func TestSelectorScrollsAndKeepsTheCursorVisible(t *testing.T) {
	w := newWorld()
	var many []Choice
	for i := 0; i < 50; i++ {
		many = append(many, Choice{ID: fmt.Sprint(i), Name: fmt.Sprintf("src%02d", i), Label: "x.csv"})
	}
	w.sw.sources = func() ([]Choice, error) { return many, nil }
	m := w.model(t)
	m, _ = send(t, m, tea.WindowSizeMsg{Width: 60, Height: 14})
	m, _ = send(t, m, ctrlO)
	for i := 0; i < 40; i++ {
		m, _ = send(t, m, down)
	}
	view := plain(m)
	if !strings.Contains(view, "src40") {
		t.Errorf("the selected Source must be visible:\n%s", view)
	}
	if strings.Contains(view, "src00") {
		t.Errorf("the list did not scroll:\n%s", view)
	}
	if n := len(lines(m.View())); n != 14 {
		t.Errorf("view is %d lines, want 14", n)
	}
}

func TestCtrlOIsIgnoredWhileAnotherOverlayIsOpen(t *testing.T) {
	w := newWorld()
	m := w.model(t)
	m, _ = send(t, m, f1)
	m, _ = send(t, m, ctrlO)
	if _, ok := m.overlay.(help); !ok {
		t.Errorf("ctrl+o replaced the help overlay with %T", m.overlay)
	}
}

func TestSelectorTextCannotDriveTheTerminal(t *testing.T) {
	w := newWorld()
	w.sw.sources = func() ([]Choice, error) {
		return []Choice{{ID: "x", Name: "ev\x1b[2Jil", Label: "la\x1b[2Jbel"}}, nil
	}
	m := w.model(t)
	m, _ = send(t, m, ctrlO)
	if strings.Contains(m.View(), "\x1b[2J") {
		t.Errorf("a name or label reached the terminal unsanitized: %q", m.View())
	}
}

func TestACopyOfTheModelWithoutASwitcherSaysSo(t *testing.T) {
	m := New(nil, "")
	m, _ = send(t, m, ctrlO)
	if m.overlay != nil || !m.isError || !strings.Contains(m.status, "cannot switch") {
		t.Errorf("status = %q, overlay %v", m.status, m.overlay)
	}
}
