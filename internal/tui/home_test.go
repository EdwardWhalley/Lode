package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

// homeModel is a Model started the way lode with no argument starts: on the home page, with
// no Adapter.
func homeModel(t *testing.T, w *world) Model {
	t.Helper()
	m := New(nil, "", WithSwitcher(w.sw, Choice{}), WithHomeAtStart())
	m, _ = send(t, m, tea.WindowSizeMsg{Width: 100, Height: 30})
	return m
}

func TestStartingWithTheHomePageShowsItAndNotTheSelector(t *testing.T) {
	m := homeModel(t, newWorld())
	if _, ok := m.overlay.(home); !ok {
		t.Fatalf("overlay = %T, want the home page", m.overlay)
	}
	view := plain(m)
	if !strings.Contains(view, strings.TrimSpace(homeArt[3])) || !strings.Contains(view, "PRESS ANY KEY") {
		t.Errorf("the home page shows the name and the prompt:\n%s", view)
	}
	if strings.Contains(view, "Choose a Source") {
		t.Errorf("the selector must wait for a key:\n%s", view)
	}
}

func TestAnyKeyOpensTheSelectorAndIsNotAlsoHandledElsewhere(t *testing.T) {
	keys := map[string]tea.KeyMsg{
		"a letter": {Type: tea.KeyRunes, Runes: []rune("x")},
		"enter":    {Type: tea.KeyEnter},
		"space":    {Type: tea.KeySpace, Runes: []rune(" ")},
		"esc":      {Type: tea.KeyEsc},
		"an arrow": {Type: tea.KeyDown},
		"F2":       {Type: tea.KeyF2},
		"F5":       {Type: tea.KeyF5},
		"ctrl+r":   {Type: tea.KeyCtrlR},
		"tab":      {Type: tea.KeyTab},
	}
	for name, k := range keys {
		w := newWorld()
		m := homeModel(t, w)
		theme := m.themeName
		m, cmd := send(t, m, k)
		if m.overlay != nil {
			t.Errorf("%s: the home page is still open", name)
		}
		if m.editor.Value() != "" {
			t.Errorf("%s: the key reached the editor: %q", name, m.editor.Value())
			continue
		}
		if m.themeName != theme {
			t.Errorf("%s: the key changed the theme to %q", name, m.themeName)
		}
		if m.query.running() || m.status == "no Source is open" {
			t.Errorf("%s: the key tried to run a query", name)
		}
		if cmd == nil {
			t.Errorf("%s: no command, so the selector cannot open", name)
			continue
		}
		if _, quit := cmd().(tea.QuitMsg); quit {
			t.Errorf("%s: the key quits; only ctrl+c and ctrl+q do", name)
			continue
		}
		m = drain(t, m, cmd)
		sel, ok := m.overlay.(selector)
		if !ok {
			t.Errorf("%s: overlay = %T, want the selector", name, m.overlay)
			continue
		}
		if !sel.quitOnEsc {
			t.Errorf("%s: with no Source open esc must quit from the selector", name)
		}
	}
}

func TestTheSelectorAfterTheHomePageIsReadAgain(t *testing.T) {
	w := newWorld()
	reads := 0
	w.sw.sources = func() ([]Choice, error) { reads++; return []Choice{w.sw.choices["a"]}, nil }
	m := homeModel(t, w)
	if reads != 0 {
		t.Fatalf("the home page read the configuration (%d reads)", reads)
	}
	m, cmd := send(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("x")})
	m = drain(t, m, cmd)
	if reads != 1 {
		t.Errorf("the selector read the configuration %d times, want 1", reads)
	}
	if !strings.Contains(plain(m), "source-a") {
		t.Errorf("the selector lists the Sources read:\n%s", plain(m))
	}
}

func TestWithNoSourcesTheHomePageLeadsToASelectorThatOffersNew(t *testing.T) {
	w := newWorld()
	w.sw.sources = func() ([]Choice, error) { return nil, nil }
	w.sw.kinds = []KindInfo{{Name: "file"}}
	m := homeModel(t, w)
	m, cmd := send(t, m, enter)
	m = drain(t, m, cmd)
	if !strings.Contains(plain(m), "no Sources are configured") {
		t.Fatalf("the selector says there are no Sources:\n%s", plain(m))
	}
	m, cmd = send(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("n")})
	m = drain(t, m, cmd)
	if _, ok := m.overlay.(kindPicker); !ok {
		t.Errorf("n opened %T, want the Source type picker", m.overlay)
	}
}

func TestCtrlCAndCtrlQQuitFromTheHomePage(t *testing.T) {
	for _, k := range []tea.KeyMsg{{Type: tea.KeyCtrlC}, {Type: tea.KeyCtrlQ}} {
		m := homeModel(t, newWorld())
		_, cmd := send(t, m, k)
		if cmd == nil {
			t.Fatalf("%v returned no command", k)
		}
		if _, quit := cmd().(tea.QuitMsg); !quit {
			t.Errorf("%v does not quit", k)
		}
	}
}

func TestHomeViewIsExactlyTheSizeGiven(t *testing.T) {
	for _, size := range [][2]int{{1, 1}, {20, 5}, {80, 24}, {200, 60}, {3, 1}, {1, 30}} {
		w, h := size[0], size[1]
		view := home{}.view(w, h)
		lines := strings.Split(view, "\n")
		if len(lines) != h {
			t.Errorf("%dx%d: %d lines, want %d", w, h, len(lines), h)
		}
		for i, l := range lines {
			if got := ansi.StringWidth(l); got > w {
				t.Errorf("%dx%d: line %d is %d columns: %q", w, h, i, got, l)
			}
		}
	}
}

func TestAMousePressOnTheHomePageChangesNothing(t *testing.T) {
	m := homeModel(t, newWorld())
	before := m.View()
	for _, ev := range []tea.MouseMsg{
		{X: 10, Y: 5, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress},
		{X: 3, Y: 2, Button: tea.MouseButtonWheelDown, Action: tea.MouseActionPress},
	} {
		var cmd tea.Cmd
		m, cmd = send(t, m, ev)
		if cmd != nil {
			t.Errorf("%v returned a command", ev)
		}
	}
	if _, ok := m.overlay.(home); !ok {
		t.Errorf("a mouse event closed the home page: %T", m.overlay)
	}
	if m.View() != before {
		t.Error("a mouse event changed what is drawn")
	}
}
