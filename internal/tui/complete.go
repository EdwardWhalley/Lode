// Autocomplete: the dropdown's state, its suggestions and how it is drawn.

package tui

import (
	"slices"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/EdwardWhalley/lode/internal/adapter"
	"github.com/EdwardWhalley/lode/internal/editor"
	"github.com/EdwardWhalley/lode/internal/sqltext"
)

// maxSuggestions bounds the work for a huge schema; typing narrows the list.
const maxSuggestions = 300

// maxVisible is how many suggestions the dropdown shows at once.
const maxVisible = 8

// minAutoChars is how many characters of a word are typed before the dropdown opens by
// itself.
const minAutoChars = 2

// maxBoxWidth bounds the dropdown so it never covers the whole grid.
const maxBoxWidth = 56

// keywords is the fixed list of SQL keywords offered. No dialect is assumed.
var keywords = []string{
	"SELECT", "FROM", "WHERE", "GROUP BY", "ORDER BY", "HAVING", "LIMIT", "OFFSET",
	"JOIN", "INNER JOIN", "LEFT JOIN", "RIGHT JOIN", "FULL JOIN", "CROSS JOIN", "ON", "USING",
	"AS", "DISTINCT", "UNION", "UNION ALL", "INTERSECT", "EXCEPT",
	"INSERT INTO", "VALUES", "UPDATE", "SET", "DELETE FROM",
	"CREATE TABLE", "CREATE VIEW", "CREATE INDEX", "DROP", "ALTER TABLE", "ADD COLUMN",
	"WITH", "CASE", "WHEN", "THEN", "ELSE", "END",
	"AND", "OR", "NOT", "NULL", "IS NULL", "IS NOT NULL", "IN", "BETWEEN", "LIKE", "ILIKE", "EXISTS",
	"ASC", "DESC", "COUNT", "SUM", "AVG", "MIN", "MAX", "CAST", "COALESCE",
}

// suggestion is one candidate: what to show, what to insert, and what it is.
type suggestion struct {
	label  string
	insert string
	detail string
}

// completion is the open dropdown, state of the editor and not an Overlay.
type completion struct {
	items  []suggestion
	cursor int
	moved  bool
	anchor int
}

// wordKey identifies a word in the editor, so a dropdown dismissed with esc stays closed
// for it.
type wordKey struct{ row, col int }

// completer is the autocomplete state of the editor: the open dropdown and the word
// dismissed with esc.
type completer struct {
	*completion
	dismissed     wordKey
	dismissedWord string
	hasDismissed  bool
}

func (c completer) active() bool { return c.completion != nil }

// close shuts the dropdown, keeping the dismissal.
func (c completer) close() completer {
	c.completion = nil
	return c
}

// dismiss records that the dropdown for word, which starts at key, was closed with
// esc.
func (c completer) dismiss(key wordKey, word string) completer {
	c.dismissed, c.dismissedWord, c.hasDismissed = key, strings.ToLower(word), true
	return c
}

// silenced reports that word, at key, is still the dismissed word, and otherwise drops the
// dismissal.
func (c completer) silenced(key wordKey, word string) (completer, bool) {
	if !c.hasDismissed {
		return c, false
	}
	if c.dismissed == key && strings.HasPrefix(strings.ToLower(word), c.dismissedWord) {
		return c, true
	}
	c.hasDismissed = false
	return c, false
}

// wordBeforeCursor finds what is being typed: the word at the cursor, the runes to replace
// and any table before a dot.
func wordBeforeCursor(line []rune, col int) (word string, deleteN int, qualifier string) {
	col = max(0, min(col, len(line)))
	i := col
	for i > 0 && sqltext.IsWordRune(line[i-1]) {
		i--
	}
	word = strings.TrimLeft(string(line[i:col]), "\"`")
	if i > 0 && line[i-1] == '.' {
		k := i - 1
		for k > 0 && sqltext.IsWordRune(line[k-1]) {
			k--
		}
		qualifier = strings.Trim(string(line[k:i-1]), "\"`")
	}
	return word, col - i, qualifier
}

// lastPart is a table's name without its schema: "sales.orders" is "orders".
func lastPart(name string) string {
	if i := strings.LastIndex(name, "."); i >= 0 {
		return name[i+1:]
	}
	return name
}

// mentionedTables returns the tables whose names appear in the text.
func mentionedTables(tables []adapter.Table, text string) []adapter.Table {
	lower := strings.ToLower(text)
	tokens := map[string]bool{}
	for _, tok := range strings.FieldsFunc(lower, func(r rune) bool { return !sqltext.IsWordRune(r) }) {
		tokens[strings.Trim(tok, "\"`")] = true
	}
	var out []adapter.Table
	for _, tb := range tables {
		name := strings.ToLower(lastPart(tb.Name))
		if tokens[name] || (strings.ContainsAny(name, " \"`") && strings.Contains(lower, name)) {
			out = append(out, tb)
		}
	}
	return out
}

// suggestionsFor lists what could complete word: a qualifier's columns, or tables, the
// tables' columns, then keywords.
func suggestionsFor(tables []adapter.Table, text, word, qualifier string, aliases map[string][]string) []suggestion {
	prefix := strings.ToLower(word)
	matches := func(label string) bool { return strings.HasPrefix(strings.ToLower(label), prefix) }
	var out []suggestion
	add := func(s suggestion) bool {
		out = append(out, s)
		return len(out) >= maxSuggestions
	}

	if qualifier != "" {
		named := func(tb adapter.Table, name string) bool {
			return strings.EqualFold(lastPart(tb.Name), name) || strings.EqualFold(tb.Name, name)
		}
		stands, isAlias := aliases[strings.ToLower(qualifier)]
		for _, tb := range tables {
			if isAlias {
				if !slices.ContainsFunc(stands, func(name string) bool { return named(tb, name) }) {
					continue
				}
			} else if !named(tb, qualifier) {
				continue
			}
			for _, c := range tb.Columns {
				if matches(c.Name) && add(suggestion{label: c.Name, insert: c.Ref, detail: "column of " + tb.Name}) {
					return out
				}
			}
		}
		return out
	}

	for _, tb := range tables {
		if matches(tb.Name) && add(suggestion{label: tb.Name, insert: tb.Ref, detail: "table"}) {
			return out
		}
	}
	for _, tb := range mentionedTables(tables, text) {
		for _, c := range tb.Columns {
			if matches(c.Name) && add(suggestion{label: c.Name, insert: c.Ref, detail: "column of " + tb.Name}) {
				return out
			}
		}
	}
	for _, kw := range keywords {
		if matches(kw) && add(suggestion{label: kw, insert: kw, detail: "keyword"}) {
			return out
		}
	}
	return out
}

// contextBefore and contextAfter are how far from the cursor autocomplete looks, in bytes.
const (
	contextBefore = 8000
	contextAfter  = 4000
)

// cursorContext is what autocomplete knows about the cursor, worked out once per
// keystroke.
type cursorContext struct {
	line   []rune
	col    int
	before string
	around string

	cursor int
}

// aliases are the table aliases of the statement the cursor is in, within the window.
func (c cursorContext) aliases(d sqltext.Dialect) map[string][]string {
	return sqltext.Aliases(sqltext.StatementAround(c.around, c.cursor, d), d)
}

// cursorContextOf works out what autocomplete knows about the cursor in ed.
func cursorContextOf(ed editor.Model) cursorContext {
	value := ed.Value()

	start := 0
	for i := 0; i < ed.Line(); i++ {
		j := strings.IndexByte(value[start:], '\n')
		if j < 0 {
			break
		}
		start += j + 1
	}
	end := len(value)
	if j := strings.IndexByte(value[start:], '\n'); j >= 0 {
		end = start + j
	}
	line := []rune(value[start:end])

	info := ed.LineInfo()
	col := max(0, min(info.StartColumn+info.ColumnOffset, len(line)))
	cursor := start + len(string(line[:col]))

	from := max(0, start-contextBefore)
	if from > 0 {
		if j := strings.IndexByte(value[from:], '\n'); j >= 0 && from+j+1 <= start {
			from += j + 1
		} else {
			from = start
		}
	}
	to := min(len(value), cursor+contextAfter)
	if to < len(value) {
		if j := strings.LastIndexByte(value[cursor:to], '\n'); j >= 0 {
			to = cursor + j
		} else {
			to = cursor
		}
	}
	return cursorContext{line: line, col: col, before: value[from:cursor], around: value[from:to], cursor: cursor - from}
}

// cursorLineOf returns the cursor's line and its column in runes from the start of the
// logical line.
func cursorLineOf(ed editor.Model) ([]rune, int) {
	c := cursorContextOf(ed)
	return c.line, c.col
}

// How a refresh was triggered.
const (
	byRequest = iota
	byTyping
	whileOpen
)

// refresh recomputes the dropdown from ed; noMatch says a request (byRequest) found nothing
// to offer.
func (c completer) refresh(how int, ed editor.Model, tables []adapter.Table, d sqltext.Dialect) (_ completer, noMatch bool) {
	if ed.CursorCount() > 1 {
		return c.close(), false
	}
	ctx := cursorContextOf(ed)
	line, col := ctx.line, ctx.col
	word, deleteN, qualifier := wordBeforeCursor(line, col)
	key := wordKey{row: ed.Line(), col: col - deleteN}

	switch how {
	case byTyping:
		if qualifier == "" && len([]rune(word)) < minAutoChars {
			return c.close(), false
		}
		var silenced bool
		if c, silenced = c.silenced(key, word); silenced {
			return c.close(), false
		}
		if sqltext.InLiteralOrComment(ctx.before, d) {
			return c.close(), false
		}
	case whileOpen:
		if word == "" && qualifier == "" {
			return c.close(), false
		}
		if sqltext.InLiteralOrComment(ctx.before, d) {
			return c.close(), false
		}
	}

	var aliases map[string][]string
	if qualifier != "" {
		aliases = ctx.aliases(d)
	}
	items := suggestionsFor(tables, ctx.around, word, qualifier, aliases)
	if how != byRequest && len(items) == 1 && strings.EqualFold(strings.Trim(items[0].label, "\"`"), word) {
		items = nil
	}
	if len(items) == 0 {
		return c.close(), how == byRequest
	}

	next := &completion{items: items}
	if c.active() && how == whileOpen {
		next.cursor, next.moved = min(c.cursor, len(items)-1), c.moved
	}
	info := ed.LineInfo()
	onThisRow := line[max(col-deleteN, info.StartColumn):col]
	start := max(info.CharOffset-ansi.StringWidth(string(onThisRow)), 0)
	next.anchor = ansi.StringWidth(ed.Prompt) + min(start, info.CharOffset)
	c.completion = next
	return c, false
}

// openCompletion is alt+/: open the dropdown now, even with nothing typed.
func (m Model) openCompletion() Model {
	if m.editor.CursorCount() > 1 {
		return m
	}
	m.editor.ClearSelection()
	if len(m.schema.tables) == 0 && !m.hasSchema() {
		m.isError = true
		m.status = "no schema loaded: it may still be loading, or it failed (see the schema pane); keywords still complete"
	}
	var noMatch bool
	m.complete, noMatch = m.complete.refresh(byRequest, m.editor, m.schema.tables, m.current.Dialect)
	if noMatch {
		m.isError = false
		m.status = "no suggestions"
	}
	return m
}

// hasSchema is whether a schema has loaded, even an empty one.
func (m Model) hasSchema() bool { return m.schema.tables != nil }

// autoComplete opens the dropdown after a key that edited the text, if a word is being
// typed.
func (m Model) autoComplete(msg tea.KeyMsg) Model {
	edited := (msg.Type == tea.KeyRunes && !msg.Alt && !msg.Paste) || msg.Type == tea.KeyBackspace || msg.Type == tea.KeyDelete
	if !edited || m.focus != focusEditor {
		return m
	}
	m.complete, _ = m.complete.refresh(byTyping, m.editor, m.schema.tables, m.current.Dialect)
	return m
}

// accept replaces the word before the cursor of ed with the chosen suggestion and
// closes the dropdown.
func (c completer) accept(ed editor.Model) (completer, editor.Model) {
	if !c.active() {
		return c, ed
	}
	chosen := c.items[c.cursor]
	c = c.close()
	line, col := cursorLineOf(ed)
	_, deleteN, _ := wordBeforeCursor(line, col)
	ed.Batch(func(e *editor.Model) {
		for i := 0; i < deleteN; i++ {
			*e, _ = e.Update(tea.KeyMsg{Type: tea.KeyBackspace})
		}
		e.InsertString(chosen.insert)
	})
	return c, ed
}

// key handles a key while the dropdown is open; handled is false when it should be treated
// as if the list were closed.
func (c completer) key(msg tea.KeyMsg, ed editor.Model, tables []adapter.Table, d sqltext.Dialect) (_ completer, _ editor.Model, _ tea.Cmd, handled bool) {
	last := len(c.items) - 1
	move := func(to int) (completer, editor.Model, tea.Cmd, bool) {
		moved := *c.completion
		moved.cursor, moved.moved = max(0, min(to, last)), true
		c.completion = &moved
		return c, ed, nil, true
	}
	switch msg.String() {
	case "up":
		return move(c.cursor - 1)
	case "down":
		return move(c.cursor + 1)
	case "pgup":
		return move(c.cursor - maxVisible)
	case "pgdown":
		return move(c.cursor + maxVisible)
	case "tab":
		c, ed = c.accept(ed)
		return c, ed, nil, true
	case "enter":
		if c.moved {
			c, ed = c.accept(ed)
			return c, ed, nil, true
		}
		return c.close(), ed, nil, false
	case "esc":
		line, col := cursorLineOf(ed)
		word, deleteN, _ := wordBeforeCursor(line, col)
		return c.dismiss(wordKey{row: ed.Line(), col: col - deleteN}, word).close(), ed, nil, true
	case "backspace":
		var cmd tea.Cmd
		ed, cmd = ed.Update(msg)
		c, _ = c.refresh(whileOpen, ed, tables, d)
		return c, ed, cmd, true
	}
	if msg.Type == tea.KeyRunes && !msg.Alt && !msg.Paste {
		var cmd tea.Cmd
		ed, cmd = ed.Update(msg)
		if len(msg.Runes) == 1 && (sqltext.IsWordRune(msg.Runes[0]) || msg.Runes[0] == '.') {
			c, _ = c.refresh(whileOpen, ed, tables, d)
			return c, ed, cmd, true
		}
		return c.close(), ed, cmd, true
	}
	return c.close(), ed, nil, false
}

// box renders at most maxVisible rows, no more than height, around the highlighted one; it
// returns them and their width.
func (c *completion) box(avail, height int) ([]string, int) {
	visible := min(len(c.items), maxVisible, max(height, 0))
	if visible == 0 {
		return nil, 0
	}
	start := max(0, min(c.cursor-visible/2, len(c.items)-visible))

	labelW, detailW := 0, 0
	for _, s := range c.items[start : start+visible] {
		labelW = max(labelW, ansi.StringWidth(sanitize(s.label)))
		detailW = max(detailW, ansi.StringWidth(sanitize(s.detail)))
	}
	width := min(labelW+detailW+5, maxBoxWidth, avail)
	width = max(width, 1)

	rows := make([]string, 0, visible)
	for i := start; i < start+visible; i++ {
		s := c.items[i]
		label, detail := sanitize(s.label), sanitize(s.detail)
		text := " " + label + strings.Repeat(" ", labelW-ansi.StringWidth(label)) + "  "
		room := width - ansi.StringWidth(text) - 1
		detail = ansi.Truncate(detail, max(room, 0), "…")
		pad := strings.Repeat(" ", max(width-ansi.StringWidth(text)-ansi.StringWidth(detail), 0))
		line := ansi.Truncate(text+detail+pad, width, "")
		if i == c.cursor {
			rows = append(rows, boxPickStyle.Render(line))
			continue
		}
		head := ansi.Truncate(text, width, "")
		tail := ansi.TruncateLeft(line, ansi.StringWidth(head), "")
		rows = append(rows, boxStyle.Render(head)+boxDimStyle.Render(tail))
	}
	return rows, width
}

// overlayAt draws box over base at column x, keeping the colour of what is either side.
func overlayAt(base string, x int, box string, boxWidth int) string {
	left := ansi.Truncate(base, x, "")
	if pad := x - ansi.StringWidth(left); pad > 0 {
		left += strings.Repeat(" ", pad)
	}
	return left + box + ansi.TruncateLeft(base, x+boxWidth, "")
}

// draw floats the dropdown over the first rows of body, lined up with the word being
// completed.
func (c completer) draw(body string, mainWidth int) string {
	if !c.active() {
		return body
	}
	lines := strings.Split(body, "\n")
	rows, width := c.box(mainWidth, len(lines))
	x := max(0, min(c.anchor, mainWidth-width))
	for i, row := range rows {
		lines[i] = overlayAt(lines[i], x, row, width)
	}
	return strings.Join(lines, "\n")
}
