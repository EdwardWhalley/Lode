// Changing Source while running.

package tui

import (
	"context"
	"fmt"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/EdwardWhalley/lode/internal/adapter"
	"github.com/EdwardWhalley/lode/internal/sqltext"
)

// Choice is a Source the program can switch to; only the Switcher knows how to open its
// opaque ID.
type Choice struct {
	ID      string
	Name    string
	Label   string
	Write   bool
	Dialect sqltext.Dialect
}

// Opened is a Source that has been opened.
type Opened struct {
	Adapter adapter.Adapter
	Choice  Choice
	MaxRows int
}

// Switcher lists the Sources that can be switched to and opens them, so the TUI knows
// nothing of configuration or Adapters.
type Switcher interface {
	Sources() ([]Choice, error)
	Open(id string) (Opened, error)
	Kinds() []KindInfo
	TestSource(ctx context.Context, n NewSource) error
	AddSource(n NewSource) (Choice, error)
	EditForm(id string) (EditForm, error)
	ReplaceSource(n NewSource) (Choice, error)
	RemoveSource(id string) error
}

// WithSwitcher lets the Model change Source while running; current is the Source New's
// Adapter belongs to.
func WithSwitcher(sw Switcher, current Choice) Option {
	return func(m *Model) { m.switcher, m.current = sw, current }
}

// switchMsg asks the Model to switch to a Source.
type switchMsg struct{ choice Choice }

// openedMsg reports the outcome of opening a Source for a switch.
type openedMsg struct {
	id     int
	choice Choice
	opened Opened
	err    error
}

// openSource opens a Source off the UI goroutine.
func openSource(sw Switcher, c Choice, id int) tea.Cmd {
	return func() tea.Msg {
		o, err := sw.Open(c.ID)
		return openedMsg{id: id, choice: c, opened: o, err: err}
	}
}

// closeAdapter closes an Adapter off the UI goroutine.
func closeAdapter(a adapter.Adapter) tea.Cmd {
	if a == nil {
		return nil
	}
	return func() tea.Msg {
		a.Close()
		return nil
	}
}

// switchTo cancels any running query, then starts switching to c.
func (m Model) switchTo(c Choice) (Model, tea.Cmd) {
	if m.switcher == nil || (m.adapter != nil && c.ID == m.current.ID) {
		return m, nil
	}
	m.query, _ = m.query.abandon()
	m.switchID++
	m.isError = false
	m.status = fmt.Sprintf("opening %s…", sanitize(c.Label))
	return m, openSource(m.switcher, c, m.switchID)
}

// opened finishes a switch, closing the Adapter of a superseded request and keeping the
// current Source on failure.
func (m Model) opened(msg openedMsg) (Model, tea.Cmd) {
	if msg.id != m.switchID {
		if msg.err == nil {
			return m, closeAdapter(msg.opened.Adapter)
		}
		return m, nil
	}
	if msg.err != nil {
		m.isError = true
		m.status = sanitize(fmt.Sprintf("%s: %v", msg.choice.Label, msg.err))
		return m, nil
	}

	m.query, _ = m.query.abandon()

	old := m.adapter
	m.adapter = msg.opened.Adapter
	m.current = msg.opened.Choice
	m.source = msg.opened.Choice.Label
	m.writeMode = msg.opened.Choice.Write
	m.maxRows = msg.opened.MaxRows
	m.adapterGen++
	m.stepping = false
	m.complete = m.complete.close()
	m.schema.setTables(nil)
	m.isError = false
	m.status = "ready"
	return m, tea.Batch(loadSchema(m.adapter, m.adapterGen), closeAdapter(old))
}
