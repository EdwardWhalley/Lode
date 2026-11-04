// Package editor is the SQL editor: a multi-line text input that wraps long lines and draws
// itself. It began as the Bubbles textarea (MIT, Charm; see LICENSE.bubbles).
package editor

import (
	"strings"
	"time"
	"unicode"

	"github.com/charmbracelet/bubbles/cursor"
	"github.com/charmbracelet/bubbles/runeutil"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	rw "github.com/mattn/go-runewidth"
	"github.com/rivo/uniseg"
)

// MaxHeight is the most rows the editor can be given; SetHeight clamps to it.
const MaxHeight = maxHeight

const (
	minHeight     = 1
	defaultHeight = 6
	defaultWidth  = 40
	maxHeight     = 99
	maxWidth      = 500
	maxLines      = 10000
)

// LineInfo describes where the cursor is in a soft-wrapped line.
type LineInfo struct {
	Width        int
	CharWidth    int
	Height       int
	StartColumn  int
	ColumnOffset int
	RowOffset    int
	CharOffset   int
}

// Style is how the editor is drawn when it has the focus and when it does not.
type Style struct {
	Base        lipgloss.Style
	CursorLine  lipgloss.Style
	EndOfBuffer lipgloss.Style
	Placeholder lipgloss.Style
	Prompt      lipgloss.Style
	Text        lipgloss.Style
	Selection   lipgloss.Style
}

func (s Style) computedCursorLine() lipgloss.Style { return s.CursorLine.Inherit(s.Base).Inline(true) }
func (s Style) computedEndOfBuffer() lipgloss.Style {
	return s.EndOfBuffer.Inherit(s.Base).Inline(true)
}
func (s Style) computedPlaceholder() lipgloss.Style {
	return s.Placeholder.Inherit(s.Base).Inline(true)
}
func (s Style) computedPrompt() lipgloss.Style { return s.Prompt.Inherit(s.Base).Inline(true) }
func (s Style) computedText() lipgloss.Style   { return s.Text.Inherit(s.Base).Inline(true) }
func (s Style) computedSelection() lipgloss.Style {
	return s.Selection.Inherit(s.Base).Inline(true)
}

// DefaultStyles returns the styles for the focused and the blurred editor.
func DefaultStyles() (focused, blurred Style) {
	focused = Style{
		Base:        lipgloss.NewStyle(),
		CursorLine:  lipgloss.NewStyle().Background(lipgloss.AdaptiveColor{Light: "255", Dark: "0"}),
		EndOfBuffer: lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "254", Dark: "0"}),
		Placeholder: lipgloss.NewStyle().Foreground(lipgloss.Color("240")),
		Prompt:      lipgloss.NewStyle().Foreground(lipgloss.Color("7")),
		Text:        lipgloss.NewStyle(),
		Selection:   lipgloss.NewStyle().Foreground(lipgloss.Color("255")).Background(lipgloss.Color("24")),
	}
	blurred = Style{
		Base:        lipgloss.NewStyle(),
		CursorLine:  lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "245", Dark: "7"}),
		EndOfBuffer: lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "254", Dark: "0"}),
		Placeholder: lipgloss.NewStyle().Foreground(lipgloss.Color("240")),
		Prompt:      lipgloss.NewStyle().Foreground(lipgloss.Color("7")),
		Text:        lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "245", Dark: "7"}),
		Selection:   lipgloss.NewStyle().Foreground(lipgloss.Color("255")).Background(lipgloss.Color("24")),
	}
	return focused, blurred
}

// Model is the editor, a value like a Bubble Tea model: Update returns the next one.
type Model struct {
	Prompt       string
	Placeholder  string
	FocusedStyle Style
	BlurredStyle Style
	Cursor       cursor.Model

	promptWidth int
	width       int
	height      int

	value          [][]rune
	focus          bool
	col            int
	row            int
	lastCharOffset int

	anchor    position
	hasAnchor bool

	extras []caret
	addCol wantedCol

	undoStack []state
	redoStack []state
	run       editRun
	grouped   bool
	now       func() time.Time

	detached bool

	viewport *viewport.Model
	rsan     runeutil.Sanitizer
}

// New returns an empty editor without the focus.
func New() Model {
	vp := viewport.New(0, 0)
	focused, blurred := DefaultStyles()
	m := Model{
		Prompt:       lipgloss.ThickBorder().Left + " ",
		FocusedStyle: focused,
		BlurredStyle: blurred,
		Cursor:       cursor.New(),
		value:        make([][]rune, minHeight, maxLines),
		viewport:     &vp,
	}
	m.SetHeight(defaultHeight)
	m.SetWidth(defaultWidth)
	return m
}

func (m Model) style() *Style {
	if m.focus {
		return &m.FocusedStyle
	}
	return &m.BlurredStyle
}

// Blink is the command that starts the cursor blinking.
func Blink() tea.Msg { return cursor.Blink() }

func (m *Model) Focus() tea.Cmd {
	m.focus = true
	return m.Cursor.Focus()
}

func (m *Model) Blur() {
	m.focus = false
	m.Cursor.Blur()
}

// SetValue replaces the text and puts the cursor at its end.
func (m *Model) SetValue(s string) {
	m.step(func() {
		m.Reset()
		m.InsertString(s)
	})
}

func (m *Model) Reset() { m.step(m.reset) }

func (m *Model) reset() {
	m.value = make([][]rune, minHeight, maxLines)
	m.col = 0
	m.row = 0
	m.hasAnchor = false
	m.extras, m.addCol = nil, wantedCol{}
	m.viewport.GotoTop()
	m.SetCursor(0)
}

// InsertString inserts s at the cursor, making it safe, and ends any other cursors.
func (m *Model) InsertString(s string) {
	m.step(func() {
		m.extras, m.addCol = nil, wantedCol{}
		m.insertRunes([]rune(s))
	})
}

// san is the editor's sanitizer, made on first use.
func (m *Model) san() runeutil.Sanitizer {
	if m.rsan == nil {
		m.rsan = runeutil.NewSanitizer()
	}
	return m.rsan
}

// plainLineEndings turns \r\n and a lone \r into \n.
func plainLineEndings(runes []rune) []rune {
	out := make([]rune, 0, len(runes))
	for i, r := range runes {
		switch {
		case r == '\r' && i+1 < len(runes) && runes[i+1] == '\n':
		case r == '\r':
			out = append(out, '\n')
		default:
			out = append(out, r)
		}
	}
	return out
}

func (m *Model) insertRunes(runes []rune) {
	runes = m.san().Sanitize(plainLineEndings(runes))
	if len(runes) > 0 {
		m.deleteSelection()
	}

	var lines [][]rune
	lstart := 0
	for i := 0; i < len(runes); i++ {
		if runes[i] == '\n' {
			lines = append(lines, runes[lstart:i:i])
			lstart = i + 1
		}
	}
	if lstart <= len(runes) {
		lines = append(lines, runes[lstart:])
	}

	if len(m.value)+len(lines)-1 > maxLines {
		lines = lines[:max(0, maxLines-len(m.value)+1)]
	}
	if len(lines) == 0 {
		return
	}

	tail := make([]rune, len(m.value[m.row][m.col:]))
	copy(tail, m.value[m.row][m.col:])

	m.value[m.row] = append(m.value[m.row][:m.col], lines[0]...)
	m.col += len(lines[0])

	if extra := len(lines) - 1; extra > 0 {
		var grid [][]rune
		if cap(m.value) >= len(m.value)+extra {
			grid = m.value[:len(m.value)+extra]
		} else {
			grid = make([][]rune, len(m.value)+extra)
			copy(grid, m.value[:m.row+1])
		}
		copy(grid[m.row+1+extra:], m.value[m.row+1:])
		m.value = grid
		for _, l := range lines[1:] {
			m.row++
			m.value[m.row] = l
			m.col = len(l)
		}
	}

	m.value[m.row] = append(m.value[m.row], tail...)
	m.SetCursor(m.col)
}

// Value is the text, lines joined by "\n".
func (m Model) Value() string {
	if m.value == nil {
		return ""
	}
	var v strings.Builder
	for _, l := range m.value {
		v.WriteString(string(l))
		v.WriteByte('\n')
	}
	return strings.TrimSuffix(v.String(), "\n")
}

// Line is the index of the line the cursor is on.
func (m Model) Line() int { return m.row }

// Offset is where the main cursor is in Value(), in bytes.
func (m Model) Offset() int {
	if m.value == nil {
		return 0
	}
	n := 0
	for _, l := range m.value[:m.row] {
		n += len(string(l)) + 1
	}
	return n + len(string(m.value[m.row][:m.col]))
}

func (m *Model) SetHeight(h int) {
	m.height = clamp(h, minHeight, maxHeight)
	m.viewport.Height = m.height
	m.followCursor()
}

// followCursor brings the cursor's row into view, unless the wheel has scrolled the view
// away.
func (m *Model) followCursor() {
	if !m.detached {
		m.repositionView()
	}
}

// SetWidth sets the total width the editor is drawn in, prompt included.
func (m *Model) SetWidth(w int) {
	m.promptWidth = uniseg.StringWidth(m.Prompt)
	reservedOuter := m.style().Base.GetHorizontalFrameSize()
	inputWidth := min(max(w, m.promptWidth+reservedOuter+1), maxWidth)
	m.viewport.Width = inputWidth - reservedOuter
	m.width = inputWidth - reservedOuter - m.promptWidth
	m.followCursor()
}

// SetCursor moves the cursor along the current line, clamped to its ends.
func (m *Model) SetCursor(col int) {
	m.col = clamp(col, 0, len(m.value[m.row]))
	m.lastCharOffset = 0
}

func (m *Model) CursorStart() { m.SetCursor(0) }

func (m *Model) CursorEnd() { m.SetCursor(len(m.value[m.row])) }

// CursorDown moves the cursor down one wrapped row.
func (m *Model) CursorDown() {
	li := m.LineInfo()
	charOffset := max(m.lastCharOffset, li.CharOffset)
	m.lastCharOffset = charOffset

	if li.RowOffset+1 >= li.Height && m.row < len(m.value)-1 {
		m.row++
		m.col = 0
	} else {
		m.col = li.StartColumn + li.Width
	}

	m.walkToWantedColumn(charOffset)
}

// CursorUp moves the cursor up one wrapped row.
func (m *Model) CursorUp() {
	li := m.LineInfo()
	charOffset := max(m.lastCharOffset, li.CharOffset)
	m.lastCharOffset = charOffset

	if li.RowOffset <= 0 && m.row > 0 {
		m.row--
		m.col = len(m.value[m.row])
	} else {
		m.col = li.StartColumn - 1
	}

	m.walkToWantedColumn(charOffset)
}

// walkToWantedColumn puts the cursor charOffset cells along its row, or at the row's last
// place if nearer.
func (m *Model) walkToWantedColumn(charOffset int) {
	nli := m.LineInfo()
	m.col = nli.StartColumn
	if nli.Width <= 0 {
		return
	}
	last := nli.StartColumn + nli.Width - 1
	offset := 0
	for offset < charOffset && m.col < last && m.col < len(m.value[m.row]) {
		offset += rw.RuneWidth(m.value[m.row][m.col])
		m.col++
	}
}

func (m *Model) moveToBegin() {
	m.row = 0
	m.SetCursor(0)
}

func (m *Model) moveToEnd() {
	m.row = len(m.value) - 1
	m.SetCursor(len(m.value[m.row]))
}

func (m *Model) characterRight() {
	if m.col < len(m.value[m.row]) {
		m.SetCursor(m.col + 1)
	} else if m.row < len(m.value)-1 {
		m.row++
		m.CursorStart()
	}
}

// characterLeft moves one character left; with insideLine, it stops at the last character
// of the line above.
func (m *Model) characterLeft(insideLine bool) {
	if m.col == 0 && m.row != 0 {
		m.row--
		m.CursorEnd()
		if !insideLine {
			return
		}
	}
	if m.col > 0 {
		m.SetCursor(m.col - 1)
	}
}

func (m *Model) wordLeft() {
	for {
		row, col := m.row, m.col
		m.characterLeft(true)
		if m.col < len(m.value[m.row]) && !unicode.IsSpace(m.value[m.row][m.col]) {
			break
		}
		if m.row == row && m.col == col {
			return
		}
	}
	for m.col > 0 {
		if unicode.IsSpace(m.value[m.row][m.col-1]) {
			break
		}
		m.SetCursor(m.col - 1)
	}
}

func (m *Model) wordRight() {
	m.doWordRight(func(int, int) {})
}

// doWordRight moves right past one word, calling fn for each character of it.
func (m *Model) doWordRight(fn func(charIdx int, pos int)) {
	for m.col >= len(m.value[m.row]) || unicode.IsSpace(m.value[m.row][m.col]) {
		if m.row == len(m.value)-1 && m.col == len(m.value[m.row]) {
			break
		}
		m.characterRight()
	}
	charIdx := 0
	for m.col < len(m.value[m.row]) {
		if unicode.IsSpace(m.value[m.row][m.col]) {
			break
		}
		fn(charIdx, m.col)
		m.SetCursor(m.col + 1)
		charIdx++
	}
}

func (m *Model) uppercaseRight() {
	m.doWordRight(func(_ int, i int) { m.value[m.row][i] = unicode.ToUpper(m.value[m.row][i]) })
}

func (m *Model) lowercaseRight() {
	m.doWordRight(func(_ int, i int) { m.value[m.row][i] = unicode.ToLower(m.value[m.row][i]) })
}

func (m *Model) capitalizeRight() {
	m.doWordRight(func(charIdx int, i int) {
		if charIdx == 0 {
			m.value[m.row][i] = unicode.ToTitle(m.value[m.row][i])
		}
	})
}

func (m *Model) deleteBeforeCursor() {
	m.value[m.row] = m.value[m.row][m.col:]
	m.SetCursor(0)
}

func (m *Model) deleteAfterCursor() {
	m.value[m.row] = m.value[m.row][:m.col]
	m.SetCursor(len(m.value[m.row]))
}

// transposeLeft swaps the characters either side of the cursor, as ctrl+t does in
// a shell.
func (m *Model) transposeLeft() {
	if m.col == 0 || len(m.value[m.row]) < 2 {
		return
	}
	if m.col >= len(m.value[m.row]) {
		m.SetCursor(m.col - 1)
	}
	m.value[m.row][m.col-1], m.value[m.row][m.col] = m.value[m.row][m.col], m.value[m.row][m.col-1]
	if m.col < len(m.value[m.row]) {
		m.SetCursor(m.col + 1)
	}
}

func (m *Model) deleteWordLeft() {
	if m.col == 0 || len(m.value[m.row]) == 0 {
		return
	}
	oldCol := m.col

	m.SetCursor(m.col - 1)
	for unicode.IsSpace(m.value[m.row][m.col]) {
		if m.col <= 0 {
			break
		}
		m.SetCursor(m.col - 1)
	}
	for m.col > 0 {
		if !unicode.IsSpace(m.value[m.row][m.col]) {
			m.SetCursor(m.col - 1)
		} else {
			if m.col > 0 {
				m.SetCursor(m.col + 1)
			}
			break
		}
	}

	if oldCol > len(m.value[m.row]) {
		m.value[m.row] = m.value[m.row][:m.col]
	} else {
		m.value[m.row] = append(m.value[m.row][:m.col], m.value[m.row][oldCol:]...)
	}
}

func (m *Model) deleteWordRight() {
	if m.col >= len(m.value[m.row]) || len(m.value[m.row]) == 0 {
		return
	}
	oldCol := m.col

	for m.col < len(m.value[m.row]) && unicode.IsSpace(m.value[m.row][m.col]) {
		m.SetCursor(m.col + 1)
	}
	for m.col < len(m.value[m.row]) {
		if !unicode.IsSpace(m.value[m.row][m.col]) {
			m.SetCursor(m.col + 1)
		} else {
			break
		}
	}

	if m.col > len(m.value[m.row]) {
		m.value[m.row] = m.value[m.row][:oldCol]
	} else {
		m.value[m.row] = append(m.value[m.row][:oldCol], m.value[m.row][m.col:]...)
	}
	m.SetCursor(oldCol)
}

func (m *Model) mergeLineBelow(row int) {
	if row >= len(m.value)-1 {
		return
	}
	m.value[row] = append(m.value[row], m.value[row+1]...)
	for i := row + 1; i < len(m.value)-1; i++ {
		m.value[i] = m.value[i+1]
	}
	if len(m.value) > 0 {
		m.value = m.value[:len(m.value)-1]
	}
}

func (m *Model) mergeLineAbove(row int) {
	if row <= 0 {
		return
	}
	m.col = len(m.value[row-1])
	m.row--

	m.value[row-1] = append(m.value[row-1], m.value[row]...)
	for i := row; i < len(m.value)-1; i++ {
		m.value[i] = m.value[i+1]
	}
	if len(m.value) > 0 {
		m.value = m.value[:len(m.value)-1]
	}
}

func (m *Model) splitLine(row, col int) {
	head, tailSrc := m.value[row][:col], m.value[row][col:]
	tail := make([]rune, len(tailSrc))
	copy(tail, tailSrc)

	m.value = append(m.value[:row+1], m.value[row:]...)
	m.value[row] = head
	m.value[row+1] = tail

	m.col = 0
	m.row++
}

// LineInfo says where the cursor is in the wrapped rows of its line.
func (m Model) LineInfo() LineInfo {
	grid := wrap(m.value[m.row], m.width)

	var counter int
	for i, line := range grid {
		if counter+len(line) == m.col && i+1 < len(grid) {
			return LineInfo{
				CharOffset:   0,
				ColumnOffset: 0,
				Height:       len(grid),
				RowOffset:    i + 1,
				StartColumn:  m.col,
				Width:        len(grid[i+1]),
				CharWidth:    uniseg.StringWidth(string(line)),
			}
		}
		if counter+len(line) >= m.col {
			return LineInfo{
				CharOffset:   uniseg.StringWidth(string(line[:max(0, m.col-counter)])),
				ColumnOffset: m.col - counter,
				Height:       len(grid),
				RowOffset:    i,
				StartColumn:  counter,
				Width:        len(line),
				CharWidth:    uniseg.StringWidth(string(line)),
			}
		}
		counter += len(line)
	}
	return LineInfo{}
}

// cursorLineNumber is the drawn row the cursor is on, counting wrapped rows.
func (m Model) cursorLineNumber() int {
	line := 0
	for i := 0; i < m.row; i++ {
		line += len(wrap(m.value[i], m.width))
	}
	return line + m.LineInfo().RowOffset
}

// repositionView scrolls so the cursor's row is drawn.
func (m *Model) repositionView() {
	minimum := m.viewport.YOffset
	maximum := minimum + m.viewport.Height - 1
	if row := m.cursorLineNumber(); row < minimum {
		m.viewport.ScrollUp(minimum - row)
	} else if row > maximum {
		m.viewport.ScrollDown(row - maximum)
	}
}

// PasteMsg is text for Update to insert at the cursor, as a key would be typed.
type PasteMsg string

// Update handles a key, a PasteMsg and the cursor's blink; ctrl+v is left to the caller.
func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	if !m.focus {
		m.Cursor.Blur()
		return m, nil
	}

	oldRow, oldCol := m.cursorLineNumber(), m.col

	var cmds []tea.Cmd

	if m.value[m.row] == nil {
		m.value[m.row] = make([]rune, 0)
	}

	if undo, redo := undoKey(msg); undo {
		m.undo()
	} else if redo {
		m.redo()
	} else if m.grouped {
		if m.edit(msg) {
			return m, nil
		}
	} else {
		before := m.capture()
		skipTail := m.edit(msg)
		m.note(before, editKindOf(msg, before))
		if skipTail {
			return m, nil
		}
	}

	newRow, newCol := m.cursorLineNumber(), m.col
	var cmd tea.Cmd
	m.Cursor, cmd = m.Cursor.Update(msg)
	if (newRow != oldRow || newCol != oldCol) && m.Cursor.Mode() == cursor.CursorBlink {
		m.Cursor.Blink = false
		cmd = m.Cursor.BlinkCmd()
	}
	cmds = append(cmds, cmd)

	if scrollsToCursor(msg) {
		m.detached = false
	}
	if !m.detached {
		m.repositionView()
	}

	return m, tea.Batch(cmds...)
}

// editOne does what msg asks at the main cursor; skipTail says Update has nothing more to
// do.
func (m *Model) editOne(msg tea.Msg) (skipTail bool) {
	if _, ok := msg.(deleteSelectionMsg); ok {
		m.deleteSelection()
		return false
	}
	if paste, ok := msg.(PasteMsg); ok {
		m.insertRunes([]rune(paste))
	}

	if key, ok := msg.(tea.KeyMsg); ok && !m.selectionKey(key.String()) {
		switch key.String() {
		case "ctrl+k":
			m.col = clamp(m.col, 0, len(m.value[m.row]))
			if m.col >= len(m.value[m.row]) {
				m.mergeLineBelow(m.row)
				break
			}
			m.deleteAfterCursor()
		case "ctrl+u":
			m.col = clamp(m.col, 0, len(m.value[m.row]))
			if m.col <= 0 {
				m.mergeLineAbove(m.row)
				break
			}
			m.deleteBeforeCursor()
		case "backspace", "ctrl+h":
			m.col = clamp(m.col, 0, len(m.value[m.row]))
			if m.col <= 0 {
				m.mergeLineAbove(m.row)
				break
			}
			if len(m.value[m.row]) > 0 {
				m.value[m.row] = append(m.value[m.row][:max(0, m.col-1)], m.value[m.row][m.col:]...)
				if m.col > 0 {
					m.SetCursor(m.col - 1)
				}
			}
		case "delete", "ctrl+d":
			if len(m.value[m.row]) > 0 && m.col < len(m.value[m.row]) {
				m.value[m.row] = append(m.value[m.row][:m.col], m.value[m.row][m.col+1:]...)
			}
			if m.col >= len(m.value[m.row]) {
				m.mergeLineBelow(m.row)
				break
			}
		case "alt+backspace", "ctrl+w":
			if m.col <= 0 {
				m.mergeLineAbove(m.row)
				break
			}
			m.deleteWordLeft()
		case "alt+delete", "alt+d":
			m.col = clamp(m.col, 0, len(m.value[m.row]))
			if m.col >= len(m.value[m.row]) {
				m.mergeLineBelow(m.row)
				break
			}
			m.deleteWordRight()
		case "enter", "ctrl+m":
			if len(m.value) >= maxHeight {
				return true
			}
			m.col = clamp(m.col, 0, len(m.value[m.row]))
			m.splitLine(m.row, m.col)
		case "end", "ctrl+e":
			m.CursorEnd()
		case "home", "ctrl+a":
			m.CursorStart()
		case "right", "ctrl+f":
			m.characterRight()
		case "down", "ctrl+n":
			m.CursorDown()
		case "alt+right", "alt+f", "ctrl+right":
			m.wordRight()
		case "ctrl+v":
			return true
		case "left", "ctrl+b":
			m.characterLeft(false)
		case "up", "ctrl+p":
			m.CursorUp()
		case "alt+left", "alt+b", "ctrl+left":
			m.wordLeft()
		case "alt+<", "ctrl+home":
			m.moveToBegin()
		case "alt+>", "ctrl+end":
			m.moveToEnd()
		case "alt+l":
			m.lowercaseRight()
		case "alt+u":
			m.uppercaseRight()
		case "alt+c":
			m.capitalizeRight()
		case "ctrl+t":
			m.transposeLeft()
		default:
			m.insertRunes(key.Runes)
		}
	}
	return false
}

// View draws the editor in Height rows.
func (m Model) View() string {
	if m.Value() == "" && m.row == 0 && m.col == 0 && m.Placeholder != "" {
		return m.placeholderView()
	}
	st := m.style()
	m.Cursor.TextStyle = st.computedCursorLine()

	var (
		s        strings.Builder
		rowStyle lipgloss.Style
		lineInfo = m.LineInfo()
		multi    = len(m.extras) > 0
		heads    []orderedCaret
		spans    []selRange
	)
	if multi {
		heads, spans = m.allCarets(), m.selections()
	}

	for l, line := range m.value {
		wrappedLines := wrap(line, m.width)

		if m.row == l {
			rowStyle = st.computedCursorLine()
		} else {
			rowStyle = st.computedText()
		}

		lo, hi, anySelected := m.selectedColumns(l, len(line))
		rowStart := 0
		for wl, wrappedLine := range wrappedLines {
			s.WriteString(rowStyle.Render(st.computedPrompt().Render(m.Prompt)))

			strwidth := uniseg.StringWidth(string(wrappedLine))
			padding := m.width - strwidth
			from := rowStart
			rowStart += len(wrappedLine)
			rowLen := len(wrappedLine)
			if strwidth > m.width {
				wrappedLine = []rune(strings.TrimSuffix(string(wrappedLine), " "))
				padding -= m.width - strwidth
			}
			onCursorRow := m.row == l && lineInfo.RowOffset == wl
			pastEnd := onCursorRow && m.col >= len(line) && lineInfo.CharOffset >= m.width
			if multi {
				ranges, cursors, past := m.marksOnRow(rowGeom{l, len(line), from, rowLen, len(wrappedLine)}, pastEnd, heads, spans)
				if len(ranges) == 0 && len(cursors) == 0 && !past {
					s.WriteString(rowStyle.Render(string(wrappedLine)))
				} else {
					s.WriteString(m.selectedRow(wrappedLine, ranges, cursors, past, rowStyle))
				}
			} else if a, b := max(lo-from, 0), min(hi-from, len(wrappedLine)); anySelected && a < b {
				var cursors []int
				if onCursorRow && !pastEnd {
					cursors = []int{lineInfo.ColumnOffset}
				}
				s.WriteString(m.selectedRow(wrappedLine, [][2]int{{a, b}}, cursors, pastEnd, rowStyle))
			} else if onCursorRow {
				s.WriteString(rowStyle.Render(string(wrappedLine[:lineInfo.ColumnOffset])))
				if m.col >= len(line) && lineInfo.CharOffset >= m.width {
					m.Cursor.SetChar(" ")
					s.WriteString(m.Cursor.View())
				} else {
					m.Cursor.SetChar(string(wrappedLine[lineInfo.ColumnOffset]))
					s.WriteString(rowStyle.Render(m.Cursor.View()))
					s.WriteString(rowStyle.Render(string(wrappedLine[lineInfo.ColumnOffset+1:])))
				}
			} else {
				s.WriteString(rowStyle.Render(string(wrappedLine)))
			}
			s.WriteString(rowStyle.Render(strings.Repeat(" ", max(0, padding))))
			s.WriteRune('\n')
		}
	}

	for i := 0; i < m.height; i++ {
		s.WriteString(st.computedPrompt().Render(m.Prompt))
		gutter := " "
		gap := strings.Repeat(" ", max(0, m.width-lipgloss.Width(gutter)))
		s.WriteString(st.computedEndOfBuffer().Render(gutter + gap))
		s.WriteRune('\n')
	}

	m.viewport.SetContent(s.String())
	return st.Base.Render(m.viewport.View())
}

// placeholderView draws the prompt and the placeholder, with the cursor on its first
// character.
func (m Model) placeholderView() string {
	var (
		s     strings.Builder
		st    = m.style()
		style = st.computedPlaceholder()
	)

	wordwrapped := ansi.Wordwrap(m.Placeholder, m.width, "")
	hardwrapped := ansi.Hardwrap(wordwrapped, m.width, true)
	plines := strings.Split(strings.TrimSpace(hardwrapped), "\n")

	for i := 0; i < m.height; i++ {
		lineStyle := st.computedPlaceholder()
		if len(plines) > i {
			lineStyle = st.computedCursorLine()
		}

		s.WriteString(lineStyle.Render(st.computedPrompt().Render(m.Prompt)))

		switch {
		case i == 0:
			m.Cursor.TextStyle = st.computedPlaceholder()
			ch, rest, _, _ := uniseg.FirstGraphemeClusterInString(plines[0], 0)
			m.Cursor.SetChar(ch)
			s.WriteString(lineStyle.Render(m.Cursor.View()))
			s.WriteString(lineStyle.Render(style.Render(rest)))
		case len(plines) > i:
			s.WriteString(lineStyle.Render(style.Render(plines[i] + strings.Repeat(" ", max(0, m.width-uniseg.StringWidth(plines[i]))))))
		default:
			s.WriteString(st.computedEndOfBuffer().Render(" "))
		}
		s.WriteRune('\n')
	}

	m.viewport.SetContent(s.String())
	return st.Base.Render(m.viewport.View())
}

// wrap breaks runes into rows of at most width columns, at spaces where it can.
func wrap(runes []rune, width int) [][]rune {
	var (
		lines  = [][]rune{{}}
		word   = []rune{}
		row    int
		spaces int
	)

	for _, r := range runes {
		if unicode.IsSpace(r) {
			spaces++
		} else {
			word = append(word, r)
		}

		if spaces > 0 {
			if uniseg.StringWidth(string(lines[row]))+uniseg.StringWidth(string(word))+spaces > width {
				row++
				lines = append(lines, []rune{})
				lines[row] = append(lines[row], word...)
				lines[row] = append(lines[row], repeatSpaces(spaces)...)
				spaces = 0
				word = nil
			} else {
				lines[row] = append(lines[row], word...)
				lines[row] = append(lines[row], repeatSpaces(spaces)...)
				spaces = 0
				word = nil
			}
		} else {
			lastCharLen := rw.RuneWidth(word[len(word)-1])
			if uniseg.StringWidth(string(word))+lastCharLen > width {
				if len(lines[row]) > 0 {
					row++
					lines = append(lines, []rune{})
				}
				lines[row] = append(lines[row], word...)
				word = nil
			}
		}
	}

	if uniseg.StringWidth(string(lines[row]))+uniseg.StringWidth(string(word))+spaces >= width {
		lines = append(lines, []rune{})
		lines[row+1] = append(lines[row+1], word...)
		spaces++
		lines[row+1] = append(lines[row+1], repeatSpaces(spaces)...)
	} else {
		lines[row] = append(lines[row], word...)
		spaces++
		lines[row] = append(lines[row], repeatSpaces(spaces)...)
	}

	return lines
}

func repeatSpaces(n int) []rune {
	return []rune(strings.Repeat(" ", n))
}

func clamp(v, low, high int) int {
	if high < low {
		low, high = high, low
	}
	return min(high, max(low, v))
}
