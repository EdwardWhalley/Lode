// Turns Bubble Tea's mouse events into calls on the editor, the panes and the borders.

package tui

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

const (
	multiClickGap = 500 * time.Millisecond
	wheelRows     = 3
)

// pane is what is under the pointer.
type pane int

const (
	paneNone pane = iota
	paneEditor
	paneGrid
	paneSchema
)

// border is the border between panes that a drag is moving, if any.
type border int

const (
	borderNone border = iota
	borderSeparator
	borderDivider
)

// borderAt says which border the screen cell (x, y) is on.
func (m Model) borderAt(x, y int) border {
	l := m.layout
	switch {
	case x == l.separatorX() && y >= bodyTop && y < bodyTop+l.bodyH && y != l.dividerY():
		return borderSeparator
	case y == l.dividerY() && x >= l.mainLeft() && x < l.mainLeft()+l.mainW:
		return borderDivider
	}
	return borderNone
}

// pointerCell is where the mouse pointer was at the last mouse event; known is false until
// one.
type pointerCell struct {
	x, y  int
	known bool
}

// shapeFor is the pointer to ask the terminal for over each border.
var shapeFor = map[border]shape{
	borderNone:      shapeDefault,
	borderSeparator: shapeColumnResize,
	borderDivider:   shapeRowResize,
}

// litBorder is the border the View draws lit, and the cell of the arrow on it.
func (m Model) litBorder() (b border, x, y int) {
	if m.overlay != nil || !m.pointer.known {
		return borderNone, 0, 0
	}
	l := m.layout
	b = m.resizing
	if b == borderNone {
		b = m.borderAt(m.pointer.x, m.pointer.y)
	}
	switch b {
	case borderSeparator:
		return b, l.separatorX(), min(max(m.pointer.y, bodyTop), bodyTop+l.bodyH-1)
	case borderDivider:
		return b, min(max(m.pointer.x, l.mainLeft()), l.mainLeft()+l.mainW-1), l.dividerY()
	}
	return borderNone, 0, 0
}

// clickRun is the last click, for counting double and triple clicks.
type clickRun struct {
	at    time.Time
	x, y  int
	count int
}

// paneAt says which pane the screen cell (x, y) is in.
func (m Model) paneAt(x, y int) pane {
	l := m.layout
	switch {
	case y < bodyTop || y >= bodyTop+l.bodyH:
		return paneNone
	case x < l.separatorX():
		return paneSchema
	case x < l.mainLeft():
		return paneNone
	case x >= l.mainLeft()+l.mainW:
		return paneNone
	case y < l.dividerY():
		return paneEditor
	case y == l.dividerY():
		return paneNone
	}
	return paneGrid
}

// clock is the injected time, else time.Now.
func (m Model) clock() time.Time {
	if m.now != nil {
		return m.now()
	}
	return time.Now()
}

// mouse handles a mouse event, then asks the terminal for the pointer that suits the border
// under it.
func (m Model) mouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	m.pointer = pointerCell{x: msg.X, y: msg.Y, known: true}
	if m.overlay == nil {
		m = m.mouseEvent(msg)
	}
	lit, _, _ := m.litBorder()
	return m, m.shapes.want(shapeFor[lit])
}

// mouseEvent does what a mouse event asks of the panes and the borders.
func (m Model) mouseEvent(msg tea.MouseMsg) Model {
	ex, ey := msg.X-m.layout.mainLeft(), msg.Y-bodyTop
	under := m.paneAt(msg.X, msg.Y)

	switch {
	case msg.Button == tea.MouseButtonWheelUp || msg.Button == tea.MouseButtonWheelDown:
		rows := wheelRows
		if msg.Button == tea.MouseButtonWheelUp {
			rows = -rows
		}
		switch under {
		case paneEditor:
			m.editor.ScrollView(rows)
		case paneGrid:
			m.grid.scroll(rows)
		case paneSchema:
			m.schema.scroll(rows)
		}

	case msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonLeft:
		m.dragging = false
		m.resizing = borderNone
		if b := m.borderAt(msg.X, msg.Y); b != borderNone {
			m.resizing = b
			m.clicks = clickRun{}
			m.complete = m.complete.close()
			return m
		}
		if under != paneEditor {
			m.clicks = clickRun{}
		}
		if under != paneNone {
			m.complete = m.complete.close()
		}
		switch under {
		case paneEditor:
			m = m.focusPane(focusEditor)
			m.dragging = true
			switch {
			case msg.Shift:
				m.clicks = clickRun{}
				m.editor.ExtendTo(ex, ey)
			default:
				switch m.countClick(msg.X, msg.Y) {
				case 2:
					m.editor.SelectWordAt(ex, ey)
				case 3:
					m.editor.SelectLineAt(ex, ey)
				default:
					m.editor.ClickAt(ex, ey)
				}
			}
		case paneGrid:
			m = m.focusPane(focusGrid)
			m.grid.click(ex, msg.Y-m.layout.gridTop())
		case paneSchema:
			m = m.focusPane(focusSchema)
		}

	case msg.Action == tea.MouseActionMotion && msg.Button == tea.MouseButtonLeft:
		switch {
		case m.resizing == borderSeparator:
			m = m.dragSeparatorTo(msg.X)
		case m.resizing == borderDivider:
			m = m.dragDividerTo(msg.Y)
		case m.dragging:
			m.editor.DragTo(ex, ey)
		}

	case msg.Action == tea.MouseActionRelease, msg.Action == tea.MouseActionMotion && msg.Button == tea.MouseButtonNone:
		m.dragging = false
		m.resizing = borderNone
	}
	return m
}

// dragSeparatorTo makes the schema pane x columns wide, as far as the layout allows.
func (m Model) dragSeparatorTo(x int) Model {
	m.prefs.schemaW = max(x, 1)
	m = m.relayout()
	m.prefs.schemaW = m.layout.schemaW
	return m
}

// dragDividerTo makes the editor reach the pointer's row, as far as the layout allows.
func (m Model) dragDividerTo(y int) Model {
	m.prefs.editorH = max(y-bodyTop, 1)
	m = m.relayout()
	m.prefs.editorH = m.layout.editorH
	return m
}

// countClick says whether a click at (x, y) now is the first, second or third of a run on
// one cell.
func (m *Model) countClick(x, y int) int {
	now := m.clock()
	c := m.clicks
	if c.count > 0 && c.x == x && c.y == y && now.Sub(c.at) <= multiClickGap {
		c.count = c.count%3 + 1
	} else {
		c.count = 1
	}
	c.at, c.x, c.y = now, x, y
	m.clicks = c
	return c.count
}

// focusPane gives the focus to p, as tab does.
func (m Model) focusPane(f focus) Model {
	if m.focus == f {
		return m
	}
	m.focus = f
	if f == focusEditor {
		m.editor.Focus()
	} else {
		m.editor.Blur()
	}
	return m
}
