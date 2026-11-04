// What the form and the Switcher share for creating a Source, and the Kind picker Overlay.

package tui

import (
	"errors"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/EdwardWhalley/lode/internal/adapter"
)

// KindInfo is what the Source form needs to know about a Kind: its name and fields.
type KindInfo struct {
	Name     string
	ReadOnly bool
	Fields   []adapter.Field
}

// NewSource is a Source entered in the form; Values holds the Kind's own fields, by
// Field.Key.
type NewSource struct {
	Kind    int
	Name    string
	Values  adapter.Values
	Write   bool
	MaxRows int

	Replaces string
	From     LocationFrom
	Location string
}

// LocationFrom says where the location of a NewSource comes from: the fields, text typed,
// or what is stored.
type LocationFrom int

const (
	FromFields LocationFrom = iota
	FromText
	FromStored
)

// Placement says how the location of a stored Source reaches the form: in fields, in fields
// keeping a password, as text, or hidden.
type Placement int

const (
	Placed Placement = iota
	PlacedKeepingPassword
	ShownAsText
	Hidden
)

// EditForm is a stored Source as the form shows it.
type EditForm struct {
	Kind      int
	Name      string
	Values    adapter.Values
	Write     bool
	MaxRows   int
	Placement Placement
	Location  string
}

// ErrVariableUnset must not name the variable, which may be a password typed into the wrong
// box.
var (
	ErrVariableUnset = errors.New("the password variable is not set in the environment lode runs in")
	ErrNameTaken     = errors.New("a Source with that name already exists")
)

// newSourceMsg asks the Model to open the type picker.
type newSourceMsg struct{}

// openSelectorMsg asks the Model to show the Source selector again.
type openSelectorMsg struct{}

// kindPicker is the Overlay that asks which kind of Source to create.
type kindPicker struct {
	sw     Switcher
	kinds  []KindInfo
	taken  []string
	cursor int
}

func (kindPicker) scope() scope { return scopePicker }

func (p kindPicker) update(msg tea.KeyMsg, _ int) (overlay, tea.Cmd) {
	last := max(len(p.kinds)-1, 0)
	switch msg.String() {
	case "up", "k":
		p.cursor = max(p.cursor-1, 0)
	case "down", "j":
		p.cursor = min(p.cursor+1, last)
	case "home":
		p.cursor = 0
	case "end":
		p.cursor = last
	case "enter":
		if len(p.kinds) == 0 {
			return p, nil
		}
		return newSourceForm(p.sw, p.kinds, p.cursor, p.taken), nil
	case "esc", "ctrl+c":
		return nil, func() tea.Msg { return openSelectorMsg{} }
	}
	return p, nil
}

func (p kindPicker) view(width, height int) string {
	lines := []string{titleStyle.Render("New Source: what kind?"), ""}
	if len(p.kinds) == 0 {
		lines = append(lines, dimStyle.Render("  no kinds of Source are available"))
	}
	visible := max(height-len(lines), 1)
	start := max(0, min(p.cursor-visible/2, len(p.kinds)-visible))
	end := min(start+visible, len(p.kinds))
	for i := start; i < end; i++ {
		line := ansi.Truncate("  "+sanitize(p.kinds[i].Name), width, "…")
		if i == p.cursor {
			line = highlightStyle.Render(ansi.Strip(line))
		}
		lines = append(lines, line)
	}
	return fit(lines, height)
}

func (m Model) openPicker() Model {
	if m.switcher == nil {
		return m
	}
	var taken []string
	for _, c := range m.choices {
		if c.Name != "" {
			taken = append(taken, c.Name)
		}
	}
	return m.open(kindPicker{sw: m.switcher, kinds: m.switcher.Kinds(), taken: taken})
}

// added takes the answer of a save.
func (m Model) added(msg addedMsg) Model {
	if msg.err == nil && m.switcher != nil {
		if choices, err := m.switcher.Sources(); err == nil {
			m.choices = choices
		}
	}
	if f, ok := m.overlay.(sourceForm); ok {
		m.overlay = f.added(msg)
		if msg.err == nil && f.editsOpen() {
			m.isError = false
			m.status = "saved. The open connection is unchanged; the change applies when this Source is next switched to or opened"
		}
	}
	return m
}
