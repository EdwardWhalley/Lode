// The export Overlay: asks for a path and writes the held result there.

package tui

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/EdwardWhalley/lode/internal/export"
)

// exportPrompt is the one-line path prompt for ctrl+s, holding the result as it was when it
// opened.
type exportPrompt struct {
	input     textinput.Model
	err       string
	cols      []string
	rows      [][]any
	truncated bool
	cap       int
}

func newExportPrompt(cols []string, rows [][]any, truncated bool, cap int) exportPrompt {
	in := newTextInput()
	in.Prompt = ""
	in.SetValue("results-" + time.Now().Format("20060102-150405") + ".csv")
	in.CursorEnd()
	in.Focus()
	return exportPrompt{input: in, cols: cols, rows: rows, truncated: truncated, cap: cap}
}

func (exportPrompt) scope() scope { return scopeExport }

func (p exportPrompt) update(msg tea.KeyMsg, _ int) (overlay, tea.Cmd) {
	switch msg.String() {
	case "esc", "ctrl+c":
		return nil, nil
	case "enter":
		path := expandHome(strings.TrimSpace(p.input.Value()))
		if path == "" {
			p.err = "enter a file name ending in .csv or .json"
			return p, nil
		}
		target, err := export.Create(path)
		switch {
		case errors.Is(err, export.ErrExists):
			p.err = "that file already exists; nothing is ever overwritten"
			return p, nil
		case err != nil:
			p.err = sanitize(err.Error())
			return p, nil
		}
		return nil, writeExport(target, path, p.cols, p.rows, p.truncated, p.cap)
	}
	p.err = ""
	var cmd tea.Cmd
	p.input, cmd = p.input.Update(msg)
	return p, cmd
}

func (p exportPrompt) view(width, height int) string {
	in := p.input
	in.Width = max(width-10, 10)
	rows := fmt.Sprintf("%d rows", len(p.rows))
	if p.truncated {
		rows += fmt.Sprintf(" (the result was truncated at %d rows)", p.cap)
	}
	lines := []string{
		titleStyle.Render("Export results"),
		"",
		"  " + rows,
		"  Path: " + in.View(),
	}
	if p.err != "" {
		lines = append(lines, "  "+errStyle.Render(p.err))
	} else {
		lines = append(lines, "")
	}
	lines = append(lines, "",
		dimStyle.Render("  The format follows the extension: .csv or .json."),
		dimStyle.Render("  The file is created readable by you only. An existing file is never overwritten."),
	)
	return fit(lines, height)
}

// exportedMsg reports a finished export.
type exportedMsg struct {
	path      string
	rows      int
	truncated bool
	cap       int
	err       error
}

// writeExport writes the file off the UI goroutine: a result can be a million rows.
func writeExport(t *export.Target, path string, cols []string, rows [][]any, truncated bool, cap int) tea.Cmd {
	return func() tea.Msg {
		err := t.Write(cols, rows)
		return exportedMsg{path: path, rows: len(rows), truncated: truncated, cap: cap, err: err}
	}
}

// exported takes the answer of an export.
func (m Model) exported(msg exportedMsg) Model {
	if msg.err != nil {
		m.isError = true
		m.status = sanitize("export failed: " + msg.err.Error())
		return m
	}
	m.isError = false
	m.status = fmt.Sprintf("exported %d rows to %s", msg.rows, sanitize(msg.path))
	if msg.truncated {
		m.status += fmt.Sprintf("; result was truncated at %d rows", msg.cap)
	}
	return m
}

// openExport shows the export prompt for the result held now.
func (m Model) openExport() Model {
	if !m.held.exportable() {
		m.isError = true
		m.status = "nothing to export: run a query first"
		return m
	}
	return m.open(newExportPrompt(m.held.rawCols, m.held.raw, m.held.truncated, m.held.truncatedAt))
}

// expandHome turns a leading ~/ into the home directory, as the config does.
func expandHome(path string) string {
	if rest, ok := strings.CutPrefix(path, "~/"); ok {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, rest)
		}
	}
	return path
}
