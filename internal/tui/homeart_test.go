package tui

import (
	"strings"
	"testing"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/EdwardWhalley/lode/internal/theme"
)

func TestTheHomeArtIsOneWidthAndMadeOfCharactersOneCellWide(t *testing.T) {
	const allowed = " █▀▄▌▐░▒▓─│╱╲├┤"
	if len(homeArt) == 0 || len(homeArt) > 14 {
		t.Fatalf("the picture is %d rows; it must fit 14", len(homeArt))
	}
	width := utf8.RuneCountInString(homeArt[0])
	if width > 60 {
		t.Errorf("the picture is %d columns wide; it must fit 60", width)
	}
	for i, line := range homeArt {
		if got := utf8.RuneCountInString(line); got != width {
			t.Errorf("row %d is %d runes, row 0 is %d", i, got, width)
		}
		if got := ansi.StringWidth(line); got != width {
			t.Errorf("row %d is %d cells wide, but %d runes: some rune is not one cell", i, got, width)
		}
		for _, r := range line {
			if !strings.ContainsRune(allowed, r) {
				t.Errorf("row %d has %q, which is not a block or box-drawing character from the list", i, r)
			}
		}
	}
}

func TestAt80x24ThePictureAndThePromptAreShownAndNoTagline(t *testing.T) {
	view := ansi.Strip(home{}.view(80, 24))
	for i, row := range homeArt {
		if !strings.Contains(view, strings.TrimRight(row, " ")) {
			t.Errorf("row %d of the picture is missing:\n%s", i, view)
		}
	}
	if !strings.Contains(view, "PRESS ANY KEY") {
		t.Errorf("the prompt is missing:\n%s", view)
	}
	if strings.Contains(view, "TACTICAL") {
		t.Error("the picture has no tagline")
	}
}

func TestATerminalTooSmallForThePictureGetsThePlainName(t *testing.T) {
	artW, artH := utf8.RuneCountInString(homeArt[0]), len(homeArt)
	for _, size := range [][2]int{{40, 10}, {artW - 1, 40}, {200, artH}, {20, 3}, {14, 2}} {
		view := ansi.Strip(home{}.view(size[0], size[1]))
		if strings.ContainsAny(view, "█▌▐░") {
			t.Errorf("%dx%d: a cut-off picture was drawn:\n%s", size[0], size[1], view)
		}
		if !strings.Contains(view, "LODE") || !strings.Contains(view, "PRESS ANY KEY") {
			t.Errorf("%dx%d: want the plain name and the prompt:\n%s", size[0], size[1], view)
		}
	}
	if view := (home{}).view(artW, artH+2); !strings.Contains(view, "█") {
		t.Errorf("%dx%d is big enough for the picture:\n%s", artW, artH+2, view)
	}
}

func TestHomeViewFitsEverySizeWithBlinkOnAndOffAndEveryTheme(t *testing.T) {
	withColour(t)
	for _, name := range theme.Names() {
		ApplyTheme(name)
		for _, tick := range []int{0, homeBlinkTicks, homeSweepTicks / 2} {
			for w := 1; w <= 70; w += 3 {
				for h := 1; h <= 30; h += 2 {
					lines := strings.Split(home{tick: tick}.view(w, h), "\n")
					if len(lines) != h {
						t.Fatalf("%s %dx%d tick %d: %d lines", name, w, h, tick, len(lines))
					}
					for i, l := range lines {
						if got := ansi.StringWidth(l); got > w {
							t.Fatalf("%s %dx%d tick %d: line %d is %d columns", name, w, h, tick, i, got)
						}
					}
				}
			}
		}
	}
}

// tickHome sends the Model n ticks of the page it is showing.
func tickHome(t *testing.T, m Model, n int) Model {
	t.Helper()
	for i := 0; i < n; i++ {
		var cmd tea.Cmd
		m, cmd = send(t, m, homeTickMsg{gen: m.homeGen})
		if cmd == nil {
			t.Fatalf("tick %d did not ask for the next one", i)
		}
	}
	return m
}

func TestThePromptBlinksWithoutMovingAnything(t *testing.T) {
	m := homeModel(t, newWorld())
	on := plain(m)
	if !strings.Contains(on, "PRESS ANY KEY") {
		t.Fatalf("the prompt starts on:\n%s", on)
	}
	m = tickHome(t, m, homeBlinkTicks-1)
	if !strings.Contains(plain(m), "PRESS ANY KEY") {
		t.Error("the prompt went off early")
	}
	m = tickHome(t, m, 1)
	off := plain(m)
	if strings.Contains(off, "PRESS ANY KEY") {
		t.Errorf("the prompt is still on after %d ticks:\n%s", homeBlinkTicks, off)
	}
	onLines, offLines := strings.Split(on, "\n"), strings.Split(off, "\n")
	for i := range onLines {
		if a, b := ansi.StringWidth(onLines[i]), ansi.StringWidth(offLines[i]); a != b {
			t.Errorf("line %d is %d columns with the prompt on and %d with it off", i, a, b)
		}
	}
	m = tickHome(t, m, homeBlinkTicks)
	if !strings.Contains(plain(m), "PRESS ANY KEY") {
		t.Error("the prompt did not come back on")
	}
}

func TestATickAfterThePageClosedDoesNothingAndDoesNotRearm(t *testing.T) {
	m := homeModel(t, newWorld())
	gen := m.homeGen
	m, _ = send(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("x")})
	if m.overlay != nil {
		t.Fatal("the page is still open")
	}
	before := m.View()
	m, cmd := send(t, m, homeTickMsg{gen: gen})
	if cmd != nil {
		t.Error("a late tick scheduled another")
	}
	if m.View() != before {
		t.Error("a late tick changed the screen")
	}
	m, _ = send(t, m, tea.KeyMsg{Type: tea.KeyF1})
	if _, cmd := send(t, m, homeTickMsg{gen: gen}); cmd != nil {
		t.Error("a tick scheduled another while the help was open")
	}
}

func TestOpeningThePageAgainNeverRunsTwoChainsOfTicks(t *testing.T) {
	m := homeModel(t, newWorld())
	first := m.homeGen
	m, _ = send(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("x")})
	m, _ = m.openHome()
	if m.homeGen == first {
		t.Fatal("a reopened page keeps the old chain's number")
	}
	if _, cmd := send(t, m, homeTickMsg{gen: first}); cmd != nil {
		t.Error("the old chain's tick was answered, so two chains would run")
	}
	if _, cmd := send(t, m, homeTickMsg{gen: m.homeGen}); cmd == nil {
		t.Error("the new chain's tick was dropped")
	}
}

func TestTheFirstTickIsStartedWithTheProgram(t *testing.T) {
	m := homeModel(t, newWorld())
	cmd := m.Init()
	if cmd == nil {
		t.Fatal("Init returned no command")
	}
	batch, ok := cmd().(tea.BatchMsg)
	if !ok {
		t.Fatalf("Init returned %T, want a batch", cmd())
	}
	var sawTick bool
	for _, sub := range batch {
		if sub == nil {
			continue
		}
		if tick, isTick := sub().(homeTickMsg); isTick && tick.gen == m.homeGen {
			sawTick = true
		}
	}
	if !sawTick {
		t.Error("Init does not start the home page's ticks")
	}
}

func TestADifferentStartHasNoHomeTicks(t *testing.T) {
	w := newWorld()
	m := New(nil, "", WithSwitcher(w.sw, Choice{}), WithSelectorAtStart())
	if _, cmd := send(t, m, homeTickMsg{gen: m.homeGen}); cmd != nil {
		t.Error("a tick was answered with no home page open")
	}
}

func TestEachColourThemeDrawsThePictureInItsBlues(t *testing.T) {
	withColour(t)
	for _, name := range []string{"bubbletea", "classic", "light"} {
		p, _ := theme.Get(name)
		m := New(nil, "", WithSwitcher(newWorld().sw, Choice{}), WithHomeAtStart(), WithTheme(name))
		m, _ = send(t, m, tea.WindowSizeMsg{Width: 100, Height: 30})
		view := m.View()
		for field, colour := range map[string]string{
			"LogoFg": p.LogoFg, "LogoShadow": p.LogoShadow, "LogoRule": p.LogoRule, "PromptFg": p.PromptFg,
		} {
			if !strings.Contains(view, "38;5;"+colour) {
				t.Errorf("%s: %s (%s) is not used", name, field, colour)
			}
		}
	}
}

func TestMonoDrawsThePictureBoldAndTheRulesFaintWithNoColour(t *testing.T) {
	withColour(t)
	ApplyTheme("mono")
	for _, tick := range []int{0, 4, homeSweepTicks / 2, homeBlinkTicks} {
		view := home{tick: tick}.view(100, 20)
		if codes := colourCodes(view); len(codes) > 0 {
			t.Fatalf("tick %d: mono sets colour %v", tick, codes)
		}
		for _, want := range []string{"\x1b[1m", "\x1b[2m"} {
			if !strings.Contains(view, want) {
				t.Errorf("tick %d: mono is missing %q (bold letters, faint rules)", tick, want)
			}
		}
	}
}

func TestTheShimmerSweepsAcrossTheLettersThenRests(t *testing.T) {
	withColour(t)
	p, _ := theme.Get("bubbletea")
	ApplyTheme("bubbletea")
	gleam, glint := "38;5;"+p.LogoGleam, "38;5;"+p.LogoGlint

	var gleamTicks int
	for tick := 0; tick < homeCycle; tick++ {
		view := home{tick: tick}.view(100, 20)
		if strings.Contains(view, gleam) {
			gleamTicks++
		}
		if tick >= homeSweepTicks && (strings.Contains(view, gleam) || strings.Contains(view, glint)) {
			t.Fatalf("tick %d is in the pause but the letters are still shining", tick)
		}
	}
	if gleamTicks < homeSweepTicks/2 {
		t.Errorf("the gleam showed on %d of %d sweep ticks", gleamTicks, homeSweepTicks)
	}
	mid := homeSweepTicks / 2
	if (home{tick: mid}).view(100, 20) == (home{tick: mid + 1}).view(100, 20) {
		t.Error("the shimmer did not move between two ticks")
	}
	later := 3 + 2*homeBlinkTicks*homeCycle
	if (home{tick: 3}).view(100, 20) != (home{tick: later}).view(100, 20) {
		t.Error("the cycle does not repeat")
	}
}

func TestOnlyLettersShimmerNotTheRulesOrTheShadow(t *testing.T) {
	withColour(t)
	p, _ := theme.Get("bubbletea")
	ApplyTheme("bubbletea")
	for tick := 0; tick < homeCycle; tick++ {
		for _, line := range strings.Split(home{tick: tick}.view(100, 20), "\n") {
			plainLine := ansi.Strip(line)
			isRuleOrShadow := strings.ContainsAny(plainLine, "─░") && !strings.ContainsAny(plainLine, "█▌▐▄▀")
			if isRuleOrShadow && (strings.Contains(line, "38;5;"+p.LogoGleam) || strings.Contains(line, "38;5;"+p.LogoGlint)) {
				t.Fatalf("tick %d: a rule or shadow row shimmers: %q", tick, line)
			}
		}
	}
}
