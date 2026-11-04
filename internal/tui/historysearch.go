// The history search Overlay (ctrl+g).

package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/EdwardWhalley/lode/internal/history"
)

// historySearch is the ctrl+g Overlay: every query ever run, narrowed by what you type.
type historySearch struct {
	all    []history.Entry
	shown  []history.Entry
	filter textinput.Model
	cursor int
}

func newHistorySearch(entries []history.Entry) historySearch {
	all := make([]history.Entry, len(entries))
	for i, e := range entries {
		all[len(entries)-1-i] = e
	}
	in := newTextInput()
	in.Prompt = ""
	in.Placeholder = "type to filter by query or Source"
	in.Focus()
	return historySearch{all: all, shown: all, filter: in}
}

func (historySearch) scope() scope { return scopeHistory }

// loadQueryMsg asks the Model to put a query from the history into the editor.
type loadQueryMsg struct {
	entry history.Entry
}

func (s historySearch) update(msg tea.KeyMsg, height int) (overlay, tea.Cmd) {
	page := max(height-4, 1)
	last := max(len(s.shown)-1, 0)
	switch msg.String() {
	case "esc", "ctrl+c":
		return nil, nil
	case "enter":
		if len(s.shown) == 0 {
			return s, nil
		}
		e := s.shown[s.cursor]
		return nil, func() tea.Msg { return loadQueryMsg{entry: e} }
	case "up":
		s.cursor = max(s.cursor-1, 0)
	case "down":
		s.cursor = min(s.cursor+1, last)
	case "pgup":
		s.cursor = max(s.cursor-page, 0)
	case "pgdown":
		s.cursor = min(s.cursor+page, last)
	case "home":
		s.cursor = 0
	case "end":
		s.cursor = last
	default:
		before := s.filter.Value()
		var cmd tea.Cmd
		s.filter, cmd = s.filter.Update(msg)
		if s.filter.Value() != before {
			s.shown = matching(s.all, s.filter.Value())
			s.cursor = 0
		}
		return s, cmd
	}
	return s, nil
}

// matching keeps the entries whose SQL or Source contains every word of filter,
// ignoring case.
func matching(all []history.Entry, filter string) []history.Entry {
	words := strings.Fields(strings.ToLower(filter))
	if len(words) == 0 {
		return all
	}
	var out []history.Entry
	for _, e := range all {
		text := strings.ToLower(e.SQL + " " + e.Source)
		ok := true
		for _, w := range words {
			ok = ok && strings.Contains(text, w)
		}
		if ok {
			out = append(out, e)
		}
	}
	return out
}

// oneLine collapses a query to a single printable line.
func oneLine(sql string) string { return sanitize(strings.Join(strings.Fields(sql), " ")) }

func (s historySearch) view(width, height int) string {
	in := s.filter
	in.Width = max(width-12, 10)
	lines := []string{
		titleStyle.Render("Search history") + dimStyle.Render(fmt.Sprintf("  %d of %d", len(s.shown), len(s.all))),
		"  Filter: " + in.View(),
		"",
	}

	switch {
	case len(s.all) == 0:
		lines = append(lines, dimStyle.Render("  no history yet"))
	case len(s.shown) == 0:
		lines = append(lines, dimStyle.Render("  nothing matches"))
	}

	sourceWidth := 0
	for _, e := range s.shown {
		sourceWidth = max(sourceWidth, ansi.StringWidth(sanitize(e.Source)))
	}
	sourceWidth = min(sourceWidth, 24)

	visible := max(height-len(lines), 1)
	start := max(0, min(s.cursor-visible/2, len(s.shown)-visible))
	end := min(start+visible, len(s.shown))
	for i := start; i < end; i++ {
		e := s.shown[i]
		source := ansi.Truncate(sanitize(e.Source), sourceWidth, "…")
		source += strings.Repeat(" ", sourceWidth-ansi.StringWidth(source))
		line := "  " + e.Time.Local().Format("2006-01-02 15:04") + "  " + source + "  " + oneLine(e.SQL)
		line = ansi.Truncate(line, width, "…")
		if i == s.cursor {
			line = highlightStyle.Render(ansi.Strip(line))
		}
		lines = append(lines, line)
	}
	return fit(lines, height)
}

// openHistorySearch shows the history search, or says why it cannot.
func (m Model) openHistorySearch() Model {
	if m.history == nil {
		m.isError = true
		m.status = "history is off"
		return m
	}
	return m.open(newHistorySearch(m.history.Entries()))
}

// loadQuery puts a query from the history into the editor, without running it.
func (m Model) loadQuery(e history.Entry) Model {
	m.SetQuery(e.SQL)
	m.stepping = false
	m.focus = focusEditor
	m.editor.Focus()
	m.isError = false
	if e.Source != m.historyKey() {
		m.status = "loaded from the history of " + sanitize(e.Source) + " (not run, Source unchanged)"
	} else {
		m.status = "loaded from history (not run)"
	}
	return m
}
