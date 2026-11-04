package tui

import (
	"regexp"
	"strconv"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"

	"github.com/EdwardWhalley/lode/internal/theme"
)

var f2 = tea.KeyMsg{Type: tea.KeyF2}

// withColour makes styles emit real colour codes, and puts everything back afterwards.
func withColour(t *testing.T) {
	t.Helper()
	old := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.ANSI256)
	t.Cleanup(func() {
		lipgloss.SetColorProfile(old)
		ApplyTheme(theme.Default)
	})
}

// themed returns a sized Model in the named theme with the grid focused.
func themed(t *testing.T, name string) Model {
	t.Helper()
	m := New(valuesAdapter{}, "x", WithTheme(name))
	m, _ = send(t, m, tea.WindowSizeMsg{Width: 100, Height: 30})
	return m
}

func TestEachThemeDrawsItsOwnColours(t *testing.T) {
	withColour(t)

	m := themed(t, "bubbletea")
	m, _ = send(t, m, tea.KeyMsg{Type: tea.KeyTab})
	view := m.View()
	for _, want := range []string{"38;5;205", "48;5;62"} {
		if !strings.Contains(view, want) {
			t.Errorf("bubbletea is missing the colour code %q", want)
		}
	}

	m = themed(t, "classic")
	m, _ = send(t, m, tea.KeyMsg{Type: tea.KeyTab})
	view = m.View()
	if !strings.Contains(view, "38;5;39") {
		t.Error("classic should focus in blue (39)")
	}
	if strings.Contains(view, "38;5;205") || strings.Contains(view, "48;5;62") {
		t.Error("classic is showing bubbletea colours")
	}

	m = themed(t, "mono")
	m, _ = send(t, m, tea.KeyMsg{Type: tea.KeyTab})
	if codes := colourCodes(m.View()); len(codes) > 0 {
		t.Errorf("mono must not use colour, but the view sets %v", codes)
	}
}

func TestTheHighlightedRowIsVisibleInEveryTheme(t *testing.T) {
	withColour(t)
	for _, name := range theme.Names() {
		m := themed(t, name)
		m, _ = send(t, m, tea.KeyMsg{Type: tea.KeyF1})
		m, _ = send(t, m, tea.KeyMsg{Type: tea.KeyEsc})
		w := newWorld()
		m = New(w.a, "x", WithTheme(name), WithSwitcher(w.sw, w.sw.choices["a"]))
		m, _ = send(t, m, tea.WindowSizeMsg{Width: 100, Height: 30})
		m, _ = send(t, m, ctrlO)
		view := m.View()
		reverse := strings.Contains(view, "\x1b[7m")
		colour := strings.Contains(view, "48;5;57") || strings.Contains(view, "48;5;62")
		if !reverse && !colour {
			t.Errorf("%s: the highlighted row has no highlight at all: %q", name, view)
		}
		if name == "mono" || name == "classic" {
			if !reverse {
				t.Errorf("%s should highlight with reverse video", name)
			}
		}
	}
}

func TestWithNoColourDrawsAttributesAndNoColourWhateverThemeWasAsked(t *testing.T) {
	withColour(t)
	for _, opts := range [][]Option{
		{WithNoColour()},
		{WithTheme("classic"), WithNoColour()},
		{WithNoColour(), WithTheme("classic")},
	} {
		w := newWorld()
		m := New(w.a, "x", append(opts, WithSwitcher(w.sw, w.sw.choices["a"]))...)
		m, _ = send(t, m, tea.WindowSizeMsg{Width: 100, Height: 30})
		if codes := colourCodes(m.View()); len(codes) > 0 {
			t.Errorf("the view sets %v", codes)
		}
		m, _ = send(t, m, ctrlO)
		view := m.View()
		if codes := colourCodes(view); len(codes) > 0 {
			t.Errorf("the selector sets %v", codes)
		}
		if !strings.Contains(view, "\x1b[7m") {
			t.Errorf("the highlighted row has no reverse video: %q", view)
		}
	}
}

func TestATextFieldsPlaceholderIsInTheTheme(t *testing.T) {
	withColour(t)
	ApplyTheme(theme.Mono)
	search := newHistorySearch(nil)
	if !strings.Contains(ansi.Strip(search.filter.View()), "type to filter") {
		t.Fatalf("no placeholder in %q", search.filter.View())
	}
	field := newInput("")
	field.Placeholder = "a placeholder"
	export := newTextInput()
	export.Placeholder = "a placeholder"
	for _, view := range []string{search.filter.View(), field.View(), export.View()} {
		if codes := colourCodes(view); len(codes) > 0 {
			t.Errorf("%q sets %v", view, codes)
		}
	}
}

func TestTheGridsHighlightCanBeSeenWithNoColour(t *testing.T) {
	withColour(t)
	m := New(nil, "x", WithTheme("classic"), WithNoColour())
	m, _ = send(t, m, tea.WindowSizeMsg{Width: 100, Height: 30})
	m, _ = send(t, m, resultMsg{id: m.query.id, cols: []string{"a", "b"}, rows: [][]string{{"first", "second"}}})
	m, _ = send(t, m, tea.KeyMsg{Type: tea.KeyTab})
	wantHighlight(t, m, "first")
	if codes := colourCodes(m.View()); len(codes) > 0 {
		t.Errorf("the view sets %v", codes)
	}
}

func TestF2StaysOnMonoWithNoColour(t *testing.T) {
	withColour(t)
	m := New(valuesAdapter{}, "x", WithNoColour())
	m, _ = send(t, m, tea.WindowSizeMsg{Width: 100, Height: 30})
	m, _ = send(t, m, f2)
	if m.themeName != "mono" {
		t.Errorf("theme = %q, want mono", m.themeName)
	}
	if !strings.Contains(m.status, "no colour") {
		t.Errorf("status = %q, want it to say why", m.status)
	}
	if codes := colourCodes(m.View()); len(codes) > 0 {
		t.Errorf("the view sets %v after F2", codes)
	}
}

func TestTheTitleIsABarOnlyWhereTheThemeGivesItABackground(t *testing.T) {
	withColour(t)
	first := func(name string) string { return strings.Split(themed(t, name).View(), "\n")[0] }

	if !strings.Contains(first("bubbletea"), "48;5;62") {
		t.Error("bubbletea's title should be a purple bar")
	}
	if got := ansi.Strip(first("bubbletea")); !strings.HasPrefix(got, " lode ") {
		t.Errorf("a title bar is padded: %q", got)
	}
	if got := ansi.Strip(first("classic")); !strings.HasPrefix(got, "lode") {
		t.Errorf("classic keeps the plain title: %q", got)
	}
}

func TestEveryThemeFitsTheTerminalExactly(t *testing.T) {
	for _, name := range theme.Names() {
		m := New(valuesAdapter{}, "x", WithTheme(name))
		m, _ = send(t, m, tea.WindowSizeMsg{Width: 80, Height: 24})
		lines := strings.Split(m.View(), "\n")
		if len(lines) != 24 {
			t.Errorf("%s: %d lines, want 24", name, len(lines))
		}
		for i, l := range lines {
			if w := ansi.StringWidth(l); w > 80 {
				t.Errorf("%s: line %d is %d columns wide", name, i, w)
			}
		}
	}
}

func TestF2CyclesThroughTheThemesAndBack(t *testing.T) {
	withColour(t)
	m := themed(t, theme.Default)
	seen := []string{m.themeName}
	for range theme.Names() {
		m, _ = send(t, m, f2)
		seen = append(seen, m.themeName)
		if !strings.Contains(m.status, "theme: "+m.themeName) {
			t.Errorf("status = %q, want it to name the theme", m.status)
		}
	}
	if seen[0] != seen[len(seen)-1] || len(seen) != len(theme.Names())+1 {
		t.Errorf("themes seen = %v, want a full cycle ending where it began", seen)
	}
	for i, a := range seen[:len(seen)-1] {
		for _, b := range seen[:i] {
			if a == b {
				t.Errorf("the cycle repeats %q before finishing: %v", a, seen)
			}
		}
	}
}

func TestF2ChangesWhatIsDrawnImmediately(t *testing.T) {
	withColour(t)
	m := themed(t, "bubbletea")
	m, _ = send(t, m, tea.KeyMsg{Type: tea.KeyTab})
	if !strings.Contains(m.View(), "38;5;205") {
		t.Fatal("expected bubbletea's pink first")
	}
	for m.themeName != "classic" {
		m, _ = send(t, m, f2)
	}
	if view := m.View(); strings.Contains(view, "38;5;205") || !strings.Contains(view, "38;5;39") {
		t.Errorf("after F2 to classic the view still has the old colours")
	}
}

func TestWithThemeIgnoresAnUnknownNameAndNewModelsStartClean(t *testing.T) {
	withColour(t)
	m := New(valuesAdapter{}, "x", WithTheme("neon"))
	if m.themeName != theme.Default {
		t.Errorf("themeName = %q, want the default for an unknown theme", m.themeName)
	}
	New(valuesAdapter{}, "x", WithTheme("mono"))
	m = New(valuesAdapter{}, "x")
	m, _ = send(t, m, tea.WindowSizeMsg{Width: 100, Height: 30})
	m, _ = send(t, m, tea.KeyMsg{Type: tea.KeyTab})
	if !strings.Contains(m.View(), "38;5;205") {
		t.Error("a new Model did not apply its own theme")
	}
}

func TestApplyThemeRejectsUnknownNames(t *testing.T) {
	if err := ApplyTheme("neon"); err == nil || !strings.Contains(err.Error(), "neon") {
		t.Errorf("err = %v", err)
	}
	if err := ApplyTheme("Mono"); err != nil {
		t.Errorf("names are case-insensitive: %v", err)
	}
	ApplyTheme(theme.Default)
}

func TestF2IsInTheLegend(t *testing.T) {
	m := themed(t, theme.Default)
	m, _ = send(t, m, tea.WindowSizeMsg{Width: 200, Height: 30})
	parts := strings.Split(m.View(), "\n")
	if got := ansi.Strip(parts[len(parts)-1]); !strings.Contains(got, "F2 theme") {
		t.Errorf("hint row = %q", got)
	}
}

var sgr = regexp.MustCompile(`\x1b\[([0-9;]*)m`)

// colourCodes lists every colour a view sets, ignoring bold, reverse and resets.
func colourCodes(view string) []string {
	var found []string
	for _, m := range sgr.FindAllStringSubmatch(view, -1) {
		for _, p := range strings.Split(m[1], ";") {
			n, err := strconv.Atoi(p)
			if err != nil {
				continue
			}
			if (n >= 30 && n <= 38) || (n >= 40 && n <= 48) || (n >= 90 && n <= 97) || (n >= 100 && n <= 107) {
				found = append(found, m[1])
				break
			}
		}
	}
	return found
}

func TestTheEditorFollowsTheTheme(t *testing.T) {
	withColour(t)
	editorLines := func(m Model) string { return strings.Join(strings.Split(m.View(), "\n")[1:1+m.layout.editorH], "\n") }

	pink := themed(t, "bubbletea")
	if !strings.Contains(editorLines(pink), "38;5;205") {
		t.Errorf("bubbletea's editor prompt should be pink: %q", editorLines(pink))
	}
	pink, _ = send(t, pink, tea.KeyMsg{Type: tea.KeyTab})
	if strings.Contains(editorLines(pink), "38;5;205") {
		t.Errorf("the prompt of an editor without the focus should not be pink: %q", editorLines(pink))
	}
	pink, _ = send(t, pink, tea.KeyMsg{Type: tea.KeyTab})
	pink, _ = send(t, pink, tea.KeyMsg{Type: tea.KeyTab})
	if !strings.Contains(editorLines(pink), "38;5;205") {
		t.Errorf("the prompt should be pink again once the editor has the focus: %q", editorLines(pink))
	}

	if codes := colourCodes(editorLines(themed(t, "mono"))); len(codes) > 0 {
		t.Errorf("mono's editor must have no colour, has %v", codes)
	}

	m := themed(t, "bubbletea")
	for m.themeName != "mono" {
		m, _ = send(t, m, f2)
	}
	if codes := colourCodes(editorLines(m)); len(codes) > 0 {
		t.Errorf("after F2 to mono the editor still has colour %v", codes)
	}
	for m.themeName != "bubbletea" {
		m, _ = send(t, m, f2)
	}
	if !strings.Contains(editorLines(m), "38;5;205") {
		t.Errorf("after cycling back the prompt should be pink again: %q", editorLines(m))
	}
}

func TestMonoMarksTheFocusedPaneWithoutColour(t *testing.T) {
	withColour(t)
	rowOf := func(m Model, find string) string {
		for _, l := range strings.Split(m.View(), "\n") {
			if strings.Contains(ansi.Strip(l), find) {
				return l
			}
		}
		t.Fatalf("no line contains %q:\n%s", find, m.View())
		return ""
	}
	panes := []struct {
		name    string
		find    string
		presses int
	}{
		{"results divider", "Results", 1},
		{"schema heading", "Schema", 2},
		{"editor prompt", "┃", 0},
	}
	for _, p := range panes {
		m := themed(t, "mono")
		unfocused := m
		if p.presses == 0 {
			unfocused, _ = send(t, m, tea.KeyMsg{Type: tea.KeyTab})
		}
		focused := m
		for i := 0; i < p.presses; i++ {
			focused, _ = send(t, focused, tea.KeyMsg{Type: tea.KeyTab})
		}
		if p.presses > 0 {
			unfocused = m
		}
		a, b := rowOf(focused, p.find), rowOf(unfocused, p.find)
		if a == b {
			t.Errorf("mono: the %s looks the same with and without the focus: %q", p.name, a)
		}
		if codes := colourCodes(a + b); len(codes) > 0 {
			t.Errorf("mono: the %s used colour %v to mark the focus", p.name, codes)
		}
	}
}

func TestMonoMarksErrorsAndSecondaryTextWithoutColour(t *testing.T) {
	withColour(t)
	last2 := func(m Model) (status, hint string) {
		l := strings.Split(m.View(), "\n")
		return l[len(l)-2], l[len(l)-1]
	}
	m := themed(t, "mono")
	okStatus, hint := last2(m)
	m.isError, m.status = true, "ready"
	errStatus, _ := last2(m)
	if okStatus == errStatus {
		t.Errorf("mono: an error looks the same as normal status: %q", errStatus)
	}
	if ansi.Strip(hint) == hint {
		t.Errorf("mono: the hint row is plain text, indistinguishable from the status: %q", hint)
	}
	if codes := colourCodes(errStatus + hint); len(codes) > 0 {
		t.Errorf("mono must not use colour, but sets %v", codes)
	}
}

func TestTwoModelsInDifferentThemesDrawTheirOwnColours(t *testing.T) {
	withColour(t)
	a := themed(t, "bubbletea")
	b := themed(t, "classic")
	a, _ = send(t, a, tea.KeyMsg{Type: tea.KeyTab})
	b, _ = send(t, b, tea.KeyMsg{Type: tea.KeyTab})

	for round := 0; round < 2; round++ {
		viewA := a.View()
		if !strings.Contains(viewA, "38;5;205") || !strings.Contains(viewA, "48;5;62") || strings.Contains(viewA, "38;5;39") {
			t.Fatalf("round %d: the bubbletea Model is not drawn in bubbletea colours", round)
		}
		viewB := b.View()
		if !strings.Contains(viewB, "38;5;39") || strings.Contains(viewB, "38;5;205") || strings.Contains(viewB, "48;5;62") {
			t.Fatalf("round %d: the classic Model is not drawn in classic colours", round)
		}
	}
	if a.themeName != "bubbletea" || b.themeName != "classic" {
		t.Errorf("theme names changed: %q, %q", a.themeName, b.themeName)
	}
}

func TestDrawingOneModelDoesNotChangeAnotherInMono(t *testing.T) {
	withColour(t)
	pink := themed(t, "bubbletea")
	mono := themed(t, "mono")
	if codes := colourCodes(mono.View()); len(codes) > 0 {
		t.Errorf("mono set colours %v", codes)
	}
	if !strings.Contains(pink.View(), "48;5;62") {
		t.Error("drawing the mono Model must not strip the other Model's colours")
	}
}
