// Recording queries to the history, and stepping through it with ctrl+↑ and ctrl+↓.

package tui

import (
	"fmt"

	"github.com/EdwardWhalley/lode/internal/history"
)

// History is where queries are recorded and recalled from; nil means history is off.
type History interface {
	Add(source, sql string) error
	Entries() []history.Entry
}

// WithHistory records every query run and lets ctrl+↑ / ctrl+↓ step through them.
func WithHistory(h History) Option { return func(m *Model) { m.history = h } }

// WithNotice shows a message in the status line at start, such as why history is off.
func WithNotice(s string) Option { return func(m *Model) { m.notice = s } }

// historyKey is how the current Source is recorded: its configured name, else its masked
// label.
func (m Model) historyKey() string {
	if m.current.Name != "" {
		return m.current.Name
	}
	return m.source
}

// record writes a query to the history, turning history off for the session if that fails.
func (m Model) record(query string) Model {
	m.stepping = false
	if m.history == nil {
		return m
	}
	if err := m.history.Add(m.historyKey(), query); err != nil {
		m.history = nil
		m.notice = sanitize("history is off for this session: " + err.Error())
	}
	return m
}

// stepHistory moves through the current Source's history like a shell, loading text and
// never running it.
func (m Model) stepHistory(older bool) Model {
	if !m.stepping {
		if !older {
			return m
		}
		key := m.historyKey()
		var list []string
		for _, e := range m.history.Entries() {
			if e.Source == key {
				list = append(list, e.SQL)
			}
		}
		if len(list) == 0 {
			m.isError = false
			m.status = "no history for " + sanitize(key)
			return m
		}
		m.histList, m.histDraft, m.histPos, m.stepping = list, m.editor.Value(), len(list), true
	}

	if older {
		m.histPos = max(m.histPos-1, 0)
	} else {
		m.histPos++
	}
	m.isError = false
	if m.histPos >= len(m.histList) {
		m.SetQuery(m.histDraft)
		m.stepping = false
		m.status = "ready"
		return m
	}
	m.SetQuery(m.histList[m.histPos])
	m.status = fmt.Sprintf("history %d of %d", m.histPos+1, len(m.histList))
	return m
}
