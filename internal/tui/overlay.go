// Overlays: panes that replace the body and take every key, and the help Overlay.

package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// overlay is a temporary pane drawn over the main view, which takes the keyboard while
// open.
type overlay interface {
	scope() scope
	update(msg tea.KeyMsg, height int) (overlay, tea.Cmd)
	view(width, height int) string
}

// open shows o, unless an overlay is already open.
func (m Model) open(o overlay) Model {
	if m.overlay == nil {
		m.overlay = o
	}
	return m
}

// overlayHeight is how many lines an overlay may use.
func (m Model) overlayHeight() int { return m.layout.bodyH }

// fit pads or cuts lines to exactly height lines.
func fit(lines []string, height int) string {
	if len(lines) > height {
		lines = lines[:height]
	}
	for len(lines) < height {
		lines = append(lines, "")
	}
	return strings.Join(lines, "\n")
}

// help is the F1 overlay: every key, grouped, scrollable on a short terminal.
type help struct{ offset int }

func (help) scope() scope { return scopeHelp }

func (h help) update(msg tea.KeyMsg, height int) (overlay, tea.Cmd) {
	last := max(len(helpLines(1<<30))-height, 0)
	switch msg.String() {
	case "esc", "f1", "ctrl+c":
		return nil, nil
	case "up", "k":
		h.offset--
	case "down", "j":
		h.offset++
	case "pgup":
		h.offset -= height
	case "pgdown":
		h.offset += height
	case "home":
		h.offset = 0
	case "end":
		h.offset = last
	}
	h.offset = max(0, min(h.offset, last))
	return h, nil
}

func (h help) view(width, height int) string {
	lines := helpLines(width)
	start := max(0, min(h.offset, len(lines)-height))
	return fit(lines[start:], height)
}
