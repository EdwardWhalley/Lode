// Copy, cut and paste: from the editor and the grid, through the clipboard package.

package tui

import (
	"fmt"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/EdwardWhalley/lode/internal/clipboard"
	"github.com/EdwardWhalley/lode/internal/export"
)

// WithClipboard sets where copies go and pastes come from. Without it copies stay
// inside lode.
func WithClipboard(c *clipboard.Clipboard) Option { return func(m *Model) { m.clip = c } }

// pasteMsg carries the text read from the clipboard by ctrl+v; err says why the system
// clipboard was not its source.
type pasteMsg struct {
	text string
	err  error
}

// pasteFromClipboard reads the clipboard off the UI goroutine.
func pasteFromClipboard(c *clipboard.Clipboard) tea.Cmd {
	return func() tea.Msg {
		text, err := c.Paste()
		return pasteMsg{text: text, err: err}
	}
}

// copiedMsg is the answer to a copy or cut: what the status line said was done, and why it
// reached no outside clipboard.
type copiedMsg struct {
	what string
	err  error
}

// copySelection copies the text of every Selection to the clipboard and, for a cut, removes
// it.
func (m Model) copySelection(cut bool) (tea.Model, tea.Cmd) {
	text := m.editor.SelectedText()
	if cut {
		m.editor.DeleteSelection()
		m.complete = m.complete.close()
		return m.toClipboard(text, "cut "+characters(text))
	}
	return m.toClipboard(text, "copied "+characters(text))
}

// toClipboard copies text and says what in the status line.
func (m Model) toClipboard(text, what string) (tea.Model, tea.Cmd) {
	finish := m.clip.Begin(text)
	m.isError = false
	m.status = what
	return m, func() tea.Msg { return copiedMsg{what: what, err: finish()} }
}

// characters is how much text is, in words for the status line.
func characters(text string) string {
	n := utf8.RuneCountInString(text)
	if n == 1 {
		return "1 character"
	}
	return fmt.Sprintf("%d characters", n)
}

// highlightedRow is the held result's row under the Highlight, as the database gave it.
func (m Model) highlightedRow() ([]any, bool) {
	if !m.grid.hasHighlight() || m.grid.hlRow >= len(m.held.raw) {
		return nil, false
	}
	return m.held.raw[m.grid.hlRow], true
}

// copyCell copies the highlighted cell as the database's value, not the grid's shortened
// text.
func (m Model) copyCell() (tea.Model, tea.Cmd) {
	value, ok := m.highlightedValue()
	if !ok {
		return m.noRowsToCopy(), nil
	}
	return m.copyValue(value)
}

// highlightedValue is the held result's value under the Highlight.
func (m Model) highlightedValue() (any, bool) {
	row, ok := m.highlightedRow()
	if !ok || m.grid.hlCol >= len(row) {
		return nil, false
	}
	return row[m.grid.hlCol], true
}

// copyValue copies one value from a result, from the grid or the Value view.
func (m Model) copyValue(value any) (tea.Model, tea.Cmd) {
	text := export.CellText(value)
	if value == nil {
		return m.toClipboard(text, "copied NULL (empty)")
	}
	return m.toClipboard(text, "copied "+characters(text))
}

// copyRow copies the highlighted row as tab-separated text.
func (m Model) copyRow() (tea.Model, tea.Cmd) {
	row, ok := m.highlightedRow()
	if !ok {
		return m.noRowsToCopy(), nil
	}
	what := fmt.Sprintf("copied a row of %d columns", len(row))
	if len(row) == 1 {
		what = "copied a row of 1 column"
	}
	return m.toClipboard(export.RowText(row), what)
}

func (m Model) noRowsToCopy() Model {
	m.isError = false
	m.status = "no rows to copy"
	return m
}
