// The Source selector Overlay.

package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

// selector is the Source selector Overlay: the Sources that can be switched to, the one in
// use marked.
type selector struct {
	choices   []Choice
	current   string
	cursor    int
	quitOnEsc bool
}

func newSelector(choices []Choice, current string, quitOnEsc bool) selector {
	s := selector{choices: choices, current: current, quitOnEsc: quitOnEsc}
	for i, c := range choices {
		if c.ID == current {
			s.cursor = i
		}
	}
	return s
}

func (selector) scope() scope { return scopeSelector }

func (s selector) update(msg tea.KeyMsg, _ int) (overlay, tea.Cmd) {
	last := len(s.choices) - 1
	switch msg.String() {
	case "up", "k":
		s.cursor = max(s.cursor-1, 0)
	case "down", "j":
		s.cursor = min(s.cursor+1, max(last, 0))
	case "home":
		s.cursor = 0
	case "end":
		s.cursor = max(last, 0)
	case "enter":
		if len(s.choices) == 0 {
			return nil, nil
		}
		c := s.choices[s.cursor]
		return nil, func() tea.Msg { return switchMsg{choice: c} }
	case "n":
		return nil, func() tea.Msg { return newSourceMsg{} }
	case "e", "d":
		if len(s.choices) == 0 {
			return s, nil
		}
		c := s.choices[s.cursor]
		verb := "edit"
		if msg.String() == "d" {
			verb = "delete"
		}
		switch {
		case c.Name == "":
			return s, tell("the Source given on the command line is not in sources.toml, so there is nothing to " + verb)
		case verb == "delete" && s.current != "" && c.ID == s.current:
			return s, tell("the Source that is open cannot be deleted; switch to another one first")
		case verb == "edit":
			return nil, func() tea.Msg { return editSourceMsg{choice: c} }
		}
		return nil, func() tea.Msg { return deleteSourceMsg{choice: c} }
	case "esc", "ctrl+c":
		if s.quitOnEsc {
			return nil, tea.Quit
		}
		return nil, nil
	}
	return s, nil
}

func (s selector) view(width, height int) string {
	title := "Choose a Source"
	if s.current != "" {
		title = "Switch Source"
	}
	lines := []string{titleStyle.Render(title), ""}

	nameWidth := 0
	for _, c := range s.choices {
		nameWidth = max(nameWidth, ansi.StringWidth(sanitize(displayName(c))))
	}

	visible := max(height-len(lines), 1)
	start := max(0, min(s.cursor-visible/2, len(s.choices)-visible))
	end := min(start+visible, len(s.choices))
	if len(s.choices) == 0 {
		lines = append(lines, dimStyle.Render("  no Sources are configured"))
	}
	for i := start; i < end; i++ {
		c := s.choices[i]
		name := sanitize(displayName(c))
		line := "  " + name + strings.Repeat(" ", nameWidth-ansi.StringWidth(name)) + "  " + sanitize(c.Label)
		if c.Write {
			line += "  [write]"
		}
		if c.ID == s.current {
			line += "  (current)"
		}
		line = ansi.Truncate(line, width, "…")
		if i == s.cursor {
			line = highlightStyle.Render(ansi.Strip(line))
		}
		lines = append(lines, line)
	}
	return fit(lines, height)
}

// displayName is what a Source is called in the list: its configured name, or "(ad-hoc)".
func displayName(c Choice) string {
	if c.Name != "" {
		return c.Name
	}
	return "(ad-hoc)"
}

// openSelector re-reads the configuration and shows the Source selector, keeping the last
// list if that fails.
func (m Model) openSelector() Model {
	if m.switcher == nil {
		m.isError = true
		m.status = "this session cannot switch Sources"
		return m
	}
	choices, err := m.switcher.Sources()
	if err != nil {
		m.isError = true
		m.status = sanitize("cannot read the configuration: " + err.Error())
		choices = m.choices
	} else {
		m.isError = false
		m.choices = choices
	}
	if len(choices) == 0 && m.adapter != nil {
		choices = []Choice{m.current}
	}
	current := ""
	if m.adapter != nil {
		current = m.current.ID
	}
	return m.open(newSelector(choices, current, m.adapter == nil))
}

// WithSelectorAtStart opens the Source selector as soon as the Model is created.
func WithSelectorAtStart() Option { return func(m *Model) { m.selectorAtStart = true } }
