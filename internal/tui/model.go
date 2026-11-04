// Package tui is the terminal UI: a SQL editor above a results grid. This file holds the
// Model, its Update and its View.
package tui

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/EdwardWhalley/lode/internal/adapter"
	"github.com/EdwardWhalley/lode/internal/clipboard"
	"github.com/EdwardWhalley/lode/internal/editor"
	"github.com/EdwardWhalley/lode/internal/export"
	"github.com/EdwardWhalley/lode/internal/rowcap"
	"github.com/EdwardWhalley/lode/internal/sourcetext"
	"github.com/EdwardWhalley/lode/internal/theme"
)

const nullText = "NULL"

type focus int

const (
	focusEditor focus = iota
	focusGrid
	focusSchema
)

type Option func(*Model)

// WithMaxRows sets the row cap for a single query.
func WithMaxRows(n int) Option { return func(m *Model) { m.maxRows = n } }

// WithWriteMode marks the Source as write-enabled in the title bar.
func WithWriteMode() Option { return func(m *Model) { m.writeMode = true } }

// Model is the Bubble Tea model for the whole program.
type Model struct {
	adapter adapter.Adapter
	source  string
	maxRows int

	switcher   Switcher
	current    Choice
	switchID   int
	adapterGen int

	writeMode bool

	overlay overlay

	editor editor.Model
	grid   grid
	schema tree
	focus  focus

	width, height int
	layout        layout
	prefs         sizePrefs

	query    queryRun
	position string

	status  string
	isError bool
	held    heldResult

	choices         []Choice
	selectorAtStart bool
	homeAtStart     bool
	homeGen         int
	startCmd        tea.Cmd
	wantFirstQuery  bool

	history  History
	complete completer

	themeName string
	noColour  bool
	clip      *clipboard.Clipboard
	stepping  bool
	histList  []string
	histPos   int
	histDraft string
	notice    string

	dragging bool
	clicks   clickRun
	now      func() time.Time
	resizing border
	pointer  pointerCell
	shapes   *PointerShapes
}

// New returns a Model that queries the given Adapter, or starts without a Source if it is
// nil.
func New(a adapter.Adapter, source string, opts ...Option) Model {
	lay := newLayout(0, 0, sizePrefs{})
	ed := editor.New()
	ed.Placeholder = "SELECT * FROM ...   (ctrl+r to run)"
	ed.SetHeight(lay.editorH)
	ed.Focus()

	m := Model{
		layout:  lay,
		adapter: a, source: source, maxRows: rowcap.Default, editor: ed, wantFirstQuery: true, themeName: theme.Default,
		clip: clipboard.New(nil, nil),
	}
	for _, o := range opts {
		o(&m)
	}
	if m.noColour {
		m.themeName = theme.Mono
	}
	ApplyTheme(m.themeName)
	m.restyleEditor()
	m.status = "ready"
	if m.notice != "" {
		m.status += " — " + m.notice
		m.notice = ""
	}
	switch {
	case m.homeAtStart:
		m, m.startCmd = m.openHome()
	case m.selectorAtStart:
		m = m.openSelector()
	}
	return m
}

// Close closes the Source that is open now, if any, on the final Model the program
// returned.
func (m Model) Close() error {
	if m.adapter == nil {
		return nil
	}
	return m.adapter.Close()
}

func (m *Model) SetQuery(sql string) { m.editor.SetValue(sql) }

func (m Model) Init() tea.Cmd {
	cmds := []tea.Cmd{editor.Blink, m.startCmd}
	if m.adapter != nil {
		cmds = append(cmds, loadSchema(m.adapter, m.adapterGen))
	}
	return tea.Batch(cmds...)
}

type schemaMsg struct {
	gen    int
	tables []adapter.Table
	err    error
}

func loadSchema(a adapter.Adapter, gen int) tea.Cmd {
	return func() tea.Msg {
		tables, err := a.Schema(context.Background())
		return schemaMsg{gen: gen, tables: tables, err: err}
	}
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m.relayout(), nil

	case tea.MouseMsg:
		return m.mouse(msg)

	case switchMsg:
		return m.switchTo(msg.choice)

	case openedMsg:
		return m.opened(msg)

	case newSourceMsg:
		return m.openPicker(), nil

	case openSelectorMsg:
		return m.openSelector(), nil

	case homeTickMsg:
		return m.homeTicked(msg)

	case editSourceMsg:
		return m.openEdit(msg.choice), nil

	case deleteSourceMsg:
		if m.switcher == nil {
			return m, nil
		}
		return m.open(confirmDelete{sw: m.switcher, choice: msg.choice}), nil

	case noticeMsg:
		m.isError, m.status = true, sanitize(msg.text)
		return m, nil

	case removedMsg:
		return m.removed(msg), nil

	case testedMsg:
		if f, ok := m.overlay.(sourceForm); ok {
			var cmd tea.Cmd
			m.overlay, cmd = f.tested(msg)
			return m, cmd
		}
		return m, nil

	case addedMsg:
		return m.added(msg), nil

	case exportedMsg:
		return m.exported(msg), nil

	case loadQueryMsg:
		return m.loadQuery(msg.entry), nil

	case pasteMsg:
		var cmd tea.Cmd
		if m.focus == focusEditor && m.overlay == nil {
			m.editor, cmd = m.editor.Update(editor.PasteMsg(msg.text))
			if msg.err != nil {
				m.isError = false
				m.status = sanitize(msg.err.Error())
			}
		}
		return m, cmd

	case copyValueMsg:
		return m.copyValue(msg.value)

	case copiedMsg:
		if msg.err != nil {
			m.isError = false
			failure := msg.what + ", but " + sanitize(msg.err.Error())
			if m.status == msg.what {
				m.status = failure
			} else {
				m.status += "; " + failure
			}
		}
		return m, nil

	case schemaMsg:
		if msg.gen != m.adapterGen {
			return m, nil
		}
		firstSource := m.wantFirstQuery
		m.wantFirstQuery = false
		if msg.err != nil {
			m.schema.loadErr = sanitize(msg.err.Error())
			return m, nil
		}
		m.schema.setTables(msg.tables)
		m.schema.clamp()
		if firstSource && len(msg.tables) > 0 && strings.TrimSpace(m.editor.Value()) == "" {
			m.editor.SetInitialValue("SELECT * FROM " + msg.tables[0].Ref + " LIMIT 100")
		}
		return m, nil

	case resultMsg:
		var fresh bool
		if m.query, fresh = m.query.finish(msg); !fresh {
			return m, nil
		}
		m.isError = false
		switch {
		case errors.Is(msg.err, context.Canceled):
			m.status = "cancelled"
		case msg.err != nil:
			m.isError = true
			m.status = sanitize(msg.err.Error())
		default:
			m.grid.setData(msg.cols, msg.rows)
			m.grid.clamp()
			m.held = newHeldResult(msg, m.source, m.historyKey())
			m.status = msg.outcome()
		}
		if m.notice != "" {
			m.status += " — " + m.notice
			m.notice = ""
		}
		if !errors.Is(msg.err, context.Canceled) {
			m.status += m.positionSuffix()
		}
		return m, nil

	case tea.KeyMsg:
		if msg.String() == "ctrl+q" {
			return m, tea.Quit
		}
		if m.overlay != nil {
			var cmd tea.Cmd
			m.overlay, cmd = m.overlay.update(msg, m.overlayHeight())
			return m, cmd
		}
		if m.complete.active() {
			var cmd tea.Cmd
			var handled bool
			m.complete, m.editor, cmd, handled = m.complete.key(msg, m.editor, m.schema.tables, m.current.Dialect)
			if handled {
				return m, cmd
			}
		}
		switch msg.String() {
		case "alt+/":
			if m.focus == focusEditor {
				return m.openCompletion(), nil
			}
		case "f1":
			m.complete = m.complete.close()
			return m.open(help{}), nil
		case "ctrl+o":
			return m.openSelector(), nil
		case "ctrl+s":
			return m.openExport(), nil
		case "f2":
			return m.nextTheme()
		case "ctrl+g":
			return m.openHistorySearch(), nil
		case "ctrl+up", "alt+up", "ctrl+down", "alt+down":
			if m.focus == focusEditor && m.history != nil {
				return m.stepHistory(strings.HasSuffix(msg.String(), "up")), nil
			}
		case "ctrl+c", "esc":
			if msg.String() == "esc" && m.focus == focusEditor && m.editor.CollapseCursors() {
				return m, nil
			}
			if msg.String() == "ctrl+c" && m.focus == focusEditor && m.editor.HasSelection() {
				return m.copySelection(false)
			}
			if next, wasRunning := m.query.abandon(); wasRunning {
				m.query = next
				m.isError = false
				m.status = "cancelled"
				return m, nil
			}
			if msg.String() == "ctrl+c" {
				if m.focus == focusGrid && m.grid.hasHighlight() {
					return m.copyCell()
				}
				return m, tea.Quit
			}
			if m.editor.CollapseCursors() {
				return m, nil
			}
		case "ctrl+r", "f5":
			return m.run()
		case "ctrl+x":
			if m.focus == focusEditor && m.editor.HasSelection() {
				return m.copySelection(true)
			}
		case "ctrl+v":
			if m.focus == focusEditor {
				return m, pasteFromClipboard(m.clip)
			}
		case "tab":
			return m.toggleFocus(), nil
		}
		if m.focus == focusSchema {
			m.schema.update(msg)
			return m, nil
		}
		if m.focus == focusGrid {
			switch msg.String() {
			case "c":
				return m.copyCell()
			case "r":
				return m.copyRow()
			case "enter":
				return m.openValueView(), nil
			}
			m.grid.update(msg)
			return m, nil
		}
	}

	var cmd tea.Cmd
	if m.focus == focusEditor {
		m.editor, cmd = m.editor.Update(msg)
		if key, ok := msg.(tea.KeyMsg); ok {
			m = m.autoComplete(key)
		}
	}
	return m, cmd
}

// relayout works the layout out again and gives each pane its size.
func (m Model) relayout() Model {
	m.layout = newLayout(m.width, m.height, m.prefs)
	m.editor.SetWidth(m.layout.mainW)
	m.editor.SetHeight(m.layout.editorH)
	m.grid.setSize(m.layout.mainW, m.layout.gridH)
	m.schema.setSize(m.layout.schemaW, m.layout.treeH())
	if v, ok := m.overlay.(valueView); ok {
		m.overlay = v.resized(m.width)
	}
	return m
}

// toggleFocus cycles editor → results → schema → editor.
func (m Model) toggleFocus() Model {
	m.focus = (m.focus + 1) % 3
	if m.focus == focusEditor {
		m.editor.Focus()
	} else {
		m.editor.Blur()
	}
	return m
}

// run starts what ctrl+r means: the text whatToRun gives, or the reason it refuses.
func (m Model) run() (tea.Model, tea.Cmd) {
	run := whatToRun(m.editor, m.current.Dialect)
	if run.refusal != "" {
		m.isError = false
		m.status = run.refusal
		return m, nil
	}
	if m.adapter == nil {
		m.isError = true
		m.status = "no Source is open"
		return m, nil
	}
	m = m.record(run.query)
	m.position = run.position
	var cmd tea.Cmd
	m.query, cmd = m.query.begin(m.adapter, run.query, m.maxRows)
	m.isError = false
	m.status = "running… (esc to cancel)"
	return m, cmd
}

// positionSuffix ends a result's status with which Statement ran, when there was a choice.
func (m Model) positionSuffix() string {
	if m.position == "" {
		return ""
	}
	return " · " + m.position
}

// formatValue renders one cell as a single line that is safe to print.
func formatValue(v any) string {
	switch v := v.(type) {
	case nil:
		return nullText
	case []byte:
		if utf8.Valid(v) && !hasControl(string(v)) {
			return string(v)
		}
		return fmt.Sprintf(`\x%x`, v)
	case time.Time:
		if v.Hour() == 0 && v.Minute() == 0 && v.Second() == 0 && v.Nanosecond() == 0 {
			return v.Format("2006-01-02")
		}
		return v.Format("2006-01-02 15:04:05.999999")
	case []any, map[string]any:
		return sanitize(export.CellText(v))
	default:
		return sanitize(fmt.Sprint(v))
	}
}

func hasControl(s string) bool {
	for _, r := range s {
		if unicode.IsControl(r) {
			return true
		}
	}
	return false
}

// sanitize keeps a cell on one line and stops data from driving the terminal.
func sanitize(s string) string {
	s = strings.NewReplacer("\r\n", "↵", "\n", "↵", "\r", "↵", "\t", " ").Replace(s)
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return '·'
		}
		return r
	}, s)
}

// title says where the open Source is, or its name if its Label had to hide that.
func (m Model) title() string {
	switch {
	case m.adapter == nil:
		return "no Source open"
	case m.source == sourcetext.UnparseableDSN && m.current.Name != "":
		return sanitize(m.current.Name)
	}
	return sanitize(m.source)
}

func (m Model) View() string {
	ApplyTheme(m.themeName)

	var b strings.Builder
	mainWidth := m.layout.mainW

	divider := "─ Results "
	if m.focus == focusGrid {
		divider = activeStyle.Render(divider)
	}
	if m.held.fromElsewhere(m.source, m.historyKey()) {
		divider += warnStyle.Render("results from " + sanitize(m.held.from) + " ")
	}
	if m.held.truncated {
		divider += warnStyle.Render(fmt.Sprintf("(truncated at %d rows) ", m.held.truncatedAt))
	}
	lit, arrowX, arrowY := m.litBorder()
	if pad := mainWidth - ansi.StringWidth(divider); pad > 0 {
		padStyle := dimStyle
		if lit == borderDivider {
			padStyle = activeStyle
		}
		divider += padStyle.Render(strings.Repeat("─", pad))
	}
	if lit == borderDivider {
		divider = overlayAt(divider, arrowX-m.layout.mainLeft(), activeStyle.Render("↕"), 1)
	}
	body := m.complete.draw(m.grid.view(m.focus == focusGrid), mainWidth)
	main := strings.Join([]string{m.editor.View(), divider, body}, "\n")

	heading := " Schema"
	if m.focus == focusSchema {
		heading = activeStyle.Render(heading)
	} else {
		heading = dimStyle.Render(heading)
	}
	sidebar := heading + "\n" + m.schema.view(m.focus == focusSchema)
	bodyHeight := m.layout.bodyH
	separator := dimStyle.Render(strings.TrimSuffix(strings.Repeat("│\n", bodyHeight), "\n"))
	if lit == borderSeparator {
		rows := make([]string, bodyHeight)
		for i := range rows {
			cell := "│"
			if bodyTop+i == arrowY {
				cell = "↔"
			}
			rows[i] = activeStyle.Render(cell)
		}
		separator = strings.Join(rows, "\n")
	}

	b.WriteString(titleStyle.Render("lode") + dimStyle.Render("  "+m.title()))
	if m.writeMode {
		b.WriteString("  " + warnStyle.Render("[write enabled]"))
	}
	b.WriteString("\n")
	if m.overlay != nil {
		b.WriteString(m.overlay.view(m.width, bodyHeight))
	} else {
		b.WriteString(lipgloss.JoinHorizontal(lipgloss.Top, sidebar, separator, main))
	}
	b.WriteString("\n")

	b.WriteString(m.statusLine())
	if m.focus == focusGrid && m.overlay == nil {
		b.WriteString(dimStyle.Render("  " + m.grid.position()))
	}
	b.WriteString("\n" + hintRow(m.currentScope(), m.width))
	return b.String()
}

// statusLine is the status cut to the window, keeping any position at the end of an error.
func (m Model) statusLine() string {
	status, position := m.status, m.positionSuffix()
	if position != "" && strings.HasSuffix(status, position) {
		status = strings.TrimSuffix(status, position)
		status = ansi.Truncate(status, max(0, m.width-ansi.StringWidth(position)), "…") + position
	}
	if m.isError {
		status = errStyle.Render(status)
	}
	return ansi.Truncate(status, m.width, "…")
}

// currentScope is where key hints come from: the open overlay, else the focused pane.
func (m Model) currentScope() scope {
	if m.overlay != nil {
		return m.overlay.scope()
	}
	if m.complete.active() {
		return scopeComplete
	}
	switch m.focus {
	case focusGrid:
		return scopeGrid
	case focusSchema:
		return scopeSchema
	}
	return scopeEditor
}

func itoa(n int) string { return strconv.Itoa(n) }
