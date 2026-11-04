// Editing and deleting a Source: opening the form and the delete question.

package tui

import (
	"errors"
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

// editSourceMsg asks the Model to open the form for a Source in sources.toml.
type editSourceMsg struct{ choice Choice }

// deleteSourceMsg asks the Model to confirm deleting a Source.
type deleteSourceMsg struct{ choice Choice }

// noticeMsg puts an error in the status line without closing the overlay that sent it.
type noticeMsg struct{ text string }

// tell is a command that sends a noticeMsg.
func tell(text string) tea.Cmd { return func() tea.Msg { return noticeMsg{text: text} } }

// removedMsg is the answer to Switcher.RemoveSource.
type removedMsg struct {
	choice Choice
	err    error
}

// confirmDelete is the Overlay that asks before a Source is removed from sources.toml.
type confirmDelete struct {
	sw       Switcher
	choice   Choice
	removing bool
	notice   string
}

func (confirmDelete) scope() scope { return scopeConfirm }

func (c confirmDelete) update(msg tea.KeyMsg, _ int) (overlay, tea.Cmd) {
	if c.removing {
		return c, nil
	}
	switch msg.String() {
	case "enter":
		c.removing, c.notice = true, ""
		sw, choice := c.sw, c.choice
		return c, func() tea.Msg { return removedMsg{choice: choice, err: sw.RemoveSource(choice.ID)} }
	case "esc", "ctrl+c":
		return nil, func() tea.Msg { return openSelectorMsg{} }
	}
	return c, nil
}

// refused takes the answer of a removal that did not happen.
func (c confirmDelete) refused(err error) confirmDelete {
	c.removing, c.notice = false, sanitize("not deleted: "+err.Error())
	return c
}

func (c confirmDelete) view(width, height int) string {
	lines := []string{
		titleStyle.Render("Delete Source"),
		"",
		ansi.Truncate(fmt.Sprintf("  Delete the Source %q?", sanitize(c.choice.Name)), width, "…"),
		dimStyle.Render(ansi.Truncate("  "+sanitize(c.choice.Label), width, "…")),
		"",
		dimStyle.Render("  This removes its section from sources.toml and changes nothing else."),
	}
	switch {
	case c.removing:
		lines = append(lines, dimStyle.Render("  deleting…"))
	case c.notice != "":
		lines = append(lines, errorLines(c.notice, width)...)
	}
	return fit(lines, height)
}

// openEdit shows the form for the Source c, or the selector with the reason if it cannot be
// read.
func (m Model) openEdit(c Choice) Model {
	if m.switcher == nil {
		return m
	}
	ef, err := m.switcher.EditForm(c.ID)
	kinds := m.switcher.Kinds()
	if err == nil && (ef.Kind < 0 || ef.Kind >= len(kinds)) {
		err = errors.New("no form for this kind of Source")
	}
	if err != nil {
		m = m.openSelector()
		m.isError = true
		m.status = sanitize(err.Error())
		return m
	}
	var taken []string
	for _, other := range m.choices {
		if other.Name != "" && other.Name != ef.Name {
			taken = append(taken, other.Name)
		}
	}
	open := m.adapter != nil && c.ID == m.current.ID
	return m.open(newEditForm(m.switcher, kinds, ef, taken, open))
}

// removed takes the answer of a removal.
func (m Model) removed(msg removedMsg) Model {
	c, ok := m.overlay.(confirmDelete)
	if !ok || !c.removing || c.choice.ID != msg.choice.ID {
		return m
	}
	if msg.err != nil {
		m.overlay = c.refused(msg.err)
		return m
	}
	m.overlay = nil
	m = m.openSelector()
	if !m.isError {
		m.status = fmt.Sprintf("deleted the Source %q", sanitize(msg.choice.Name))
	}
	return m
}
