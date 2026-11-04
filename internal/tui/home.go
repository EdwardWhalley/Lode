// The home page: the Overlay Lode starts on with no Source, with its animated picture.

package tui

import (
	"strings"
	"time"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// home is the page Lode starts on when given no Source; any key but ctrl+c leaves it for
// the Source selector.
type home struct {
	tick int
}

func (home) scope() scope { return scopeHome }

func (h home) update(msg tea.KeyMsg, _ int) (overlay, tea.Cmd) {
	if msg.String() == "ctrl+c" {
		return nil, tea.Quit
	}
	return nil, func() tea.Msg { return openSelectorMsg{} }
}

const (
	homeName   = "LODE"
	homePrompt = "PRESS ANY KEY"

	homeInterval   = 80 * time.Millisecond
	homeBlinkTicks = 8
)

// homeTickMsg moves the page on one tick; gen says which run of ticks sent it.
type homeTickMsg struct{ gen int }

func homeTick(gen int) tea.Cmd {
	return tea.Tick(homeInterval, func(time.Time) tea.Msg { return homeTickMsg{gen: gen} })
}

// next is the page one tick later.
func (h home) next() home {
	h.tick++
	return h
}

func (h home) promptOn() bool { return (h.tick/homeBlinkTicks)%2 == 0 }

func (h home) view(width, height int) string {
	lines := make([]string, max(height, 0))
	if width < 1 || height < 1 {
		return strings.Join(lines, "\n")
	}
	artW, artH := utf8.RuneCountInString(homeArt[0]), len(homeArt)
	if width >= artW && height >= artH+2 {
		top := (height - (artH + 2)) / 2
		pad := strings.Repeat(" ", (width-artW)/2)
		for i, row := range homeArt {
			lines[top+i] = pad + h.artRow(i, row)
		}
		lines[top+artH+1] = h.promptLine(width)
		return strings.Join(lines, "\n")
	}

	top := max((height-2)/2, 0)
	name := ansi.Truncate(homeName, width, "")
	lines[top] = centred(logoStyle.Render(name), width)
	if top+1 < height {
		lines[top+1] = h.promptLine(width)
	}
	return strings.Join(lines, "\n")
}

// promptLine is the prompt centred, or blanks of the same width while it is off.
func (h home) promptLine(width int) string {
	text := ansi.Truncate(homePrompt, width, "")
	if !h.promptOn() {
		return centred(strings.Repeat(" ", ansi.StringWidth(text)), width)
	}
	return centred(promptStyle.Render(text), width)
}

// centred pads text on the left to sit in the middle of width columns.
func centred(text string, width int) string {
	return strings.Repeat(" ", max((width-ansi.StringWidth(text))/2, 0)) + text
}

// WithHomeAtStart opens the home page as soon as the Model is created.
func WithHomeAtStart() Option { return func(m *Model) { m.homeAtStart = true } }

// openHome shows the home page and returns the command that starts a new chain of ticks.
func (m Model) openHome() (Model, tea.Cmd) {
	if m.overlay != nil {
		return m, nil
	}
	m.homeGen++
	m.overlay = home{}
	return m, homeTick(m.homeGen)
}

// homeTicked answers a homeTickMsg with the next tick, only for the current chain while the
// page is open.
func (m Model) homeTicked(msg homeTickMsg) (Model, tea.Cmd) {
	h, ok := m.overlay.(home)
	if !ok || msg.gen != m.homeGen {
		return m, nil
	}
	m.overlay = h.next()
	return m, homeTick(msg.gen)
}

// homeArt is the picture: LODE over a shaded shadow between two thin rules, every row the
// same width.
var homeArt = [...]string{
	"├──────────────────────────────────────────────┤",
	"                                                ",
	"      ██▌         █████     █████▄     ███████  ",
	"     ██▌        ██▌ ▐██    ██▌ ▐██    ██▌       ",
	"    ██▌        ██▌ ▐██    ██▌ ▐██    █████▌     ",
	"   ██▌        ██▌ ▐██    ██▌ ▐██    ██▌         ",
	"  ███████     █████     █████▀     ███████      ",
	"   ░░░░░░░     ░░░░░     ░░░░░░     ░░░░░░░     ",
	"                                                ",
	"├──────────────────────────────────────────────┤",
}

// The shimmer is a band of light across the letters: homeBand either side of the gleam,
// moving homeStep cells a tick, resting homePause ticks.
const (
	homeBand  = 3
	homeStep  = 2
	homePause = 12
)

var (
	homeSweepTicks = (utf8.RuneCountInString(homeArt[0]) + len(homeArt) + 2*homeBand + homeStep - 1) / homeStep
	homeCycle      = homeSweepTicks + homePause
)

// bandAt is where the middle of the band is on a tick, or a far-off place while it rests.
func bandAt(tick int) int {
	t := tick % homeCycle
	if t >= homeSweepTicks {
		return -1 << 20
	}
	return -homeBand + t*homeStep
}

// cellKind says how a cell of the picture is drawn.
type cellKind int

const (
	cellBlank cellKind = iota
	cellLetter
	cellGlint
	cellGleam
	cellShadow
	cellRule
)

// artRow draws row i of the picture on this tick, one styled run at a time.
func (h home) artRow(i int, row string) string {
	band := bandAt(h.tick)
	var b, run strings.Builder
	kind := cellBlank
	flush := func() {
		if run.Len() > 0 {
			b.WriteString(homeStyle(kind).Render(run.String()))
			run.Reset()
		}
	}
	for col, r := range []rune(row) {
		k := cellKind(cellLetter)
		switch r {
		case ' ':
			k = cellBlank
		case '─', '├', '┤':
			k = cellRule
		case '░':
			k = cellShadow
		default:
			switch d := abs(col + i - band); {
			case d <= 1:
				k = cellGleam
			case d <= homeBand:
				k = cellGlint
			}
		}
		if k != kind {
			flush()
			kind = k
		}
		run.WriteRune(r)
	}
	flush()
	return b.String()
}

func homeStyle(k cellKind) lipgloss.Style {
	switch k {
	case cellLetter:
		return logoStyle
	case cellGlint:
		return logoGlintStyle
	case cellGleam:
		return logoGleamStyle
	case cellShadow:
		return logoShadowStyle
	case cellRule:
		return logoRuleStyle
	}
	return lipgloss.NewStyle()
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}
