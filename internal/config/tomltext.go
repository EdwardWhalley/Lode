// Edits sources.toml as text, reading just enough TOML to find each [sources.<name>]
// section and leaving every other byte as it was.

package config

import (
	"bytes"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/EdwardWhalley/lode/internal/sourcetext"
)

type lineKind int

const (
	blankLine lineKind = iota
	commentLine
	headerLine
	keyLine
	contLine
)

// line is one line of the file: where it is and, for a header or key line, where its parts
// are.
type line struct {
	kind             lineKind
	start, end, next int
	array            bool
	key              []string
	keyStart, keyEnd int
	valStart, valEnd int
	multi            bool
}

// scan splits data into lines and says what each is.
func scan(data []byte) []line {
	var lines []line
	var open byte
	for pos := leadingBOM(data); pos < len(data); {
		end := pos
		for end < len(data) && data[end] != '\n' {
			end++
		}
		next := end
		if end < len(data) {
			next = end + 1
			if end > pos && data[end-1] == '\r' {
				end--
			}
		}
		l := line{start: pos, end: end, next: next}
		switch s := skipSpace(data, pos, end); {
		case open != 0:
			l.kind = contLine
			if closeMulti(data, pos, end, open) >= 0 {
				open = 0
			}
		case s == end:
			l.kind = blankLine
		case data[s] == '#':
			l.kind = commentLine
		case data[s] == '[':
			l.kind = headerLine
			scanHeader(data, s, end, &l)
		default:
			l.kind = keyLine
			open = scanKeyLine(data, s, end, &l)
		}
		lines = append(lines, l)
		pos = next
	}
	return lines
}

// leadingBOM is how many bytes of a UTF-8 byte order mark the file starts with.
func leadingBOM(data []byte) int {
	if bytes.HasPrefix(data, []byte("\xef\xbb\xbf")) {
		return 3
	}
	return 0
}

func skipSpace(b []byte, i, end int) int {
	for i < end && (b[i] == ' ' || b[i] == '\t') {
		i++
	}
	return i
}

// skipQuoted returns the index just past the quoted key or string at i, or end if it is not
// closed on the line.
func skipQuoted(b []byte, i, end int) int {
	q := b[i]
	for i++; i < end; i++ {
		if q == '"' && b[i] == '\\' {
			i++
			continue
		}
		if b[i] == q {
			return i + 1
		}
	}
	return end
}

// closeMulti returns the index just past the delimiter closing a multi-line string of quote
// q, or -1.
func closeMulti(b []byte, i, end int, q byte) int {
	for ; i < end; i++ {
		if q == '"' && b[i] == '\\' {
			i++
			continue
		}
		if i+3 <= end && b[i] == q && b[i+1] == q && b[i+2] == q {
			j := i + 3
			for j < end && j < i+5 && b[j] == q {
				j++
			}
			return j
		}
	}
	return -1
}

func scanHeader(b []byte, s, end int, l *line) {
	i := s + 1
	if i < end && b[i] == '[' {
		l.array = true
		i++
	}
	j := i
	for j < end && b[j] != ']' {
		if b[j] == '"' || b[j] == '\'' {
			j = skipQuoted(b, j, end)
			continue
		}
		j++
	}
	if j < end {
		l.key, _ = parseKey(b[i:j])
	}
}

// scanKeyLine reads `key = value`, returning the quote of a multi-line string it leaves
// open, or 0.
func scanKeyLine(b []byte, s, end int, l *line) byte {
	eq := s
	for eq < end && b[eq] != '=' {
		if b[eq] == '"' || b[eq] == '\'' {
			eq = skipQuoted(b, eq, end)
			continue
		}
		eq++
	}
	if eq >= end {
		return 0
	}
	keyEnd := eq
	for keyEnd > s && (b[keyEnd-1] == ' ' || b[keyEnd-1] == '\t') {
		keyEnd--
	}
	l.key, _ = parseKey(b[s:keyEnd])
	l.keyStart, l.keyEnd = s, keyEnd

	v := skipSpace(b, eq+1, end)
	l.valStart, l.valEnd = v, v
	if v >= end {
		return 0
	}
	switch {
	case bytes.HasPrefix(b[v:end], []byte(`"""`)), bytes.HasPrefix(b[v:end], []byte(`'''`)):
		q := b[v]
		if e := closeMulti(b, v+3, end, q); e >= 0 {
			l.valEnd = e
			return 0
		}
		l.multi, l.valEnd = true, end
		return q
	case b[v] == '"' || b[v] == '\'':
		l.valEnd = skipQuoted(b, v, end)
	default:
		e := v
		for e < end && b[e] != '#' {
			e++
		}
		for e > v && (b[e-1] == ' ' || b[e-1] == '\t') {
			e--
		}
		l.valEnd = e
	}
	return 0
}

// parseKey splits a TOML key (a.b."c d") into its parts, and says no rather than guess at
// escapes.
func parseKey(b []byte) ([]string, bool) {
	var parts []string
	i := 0
	for {
		i = skipSpace(b, i, len(b))
		if i >= len(b) {
			return nil, false
		}
		var part string
		if c := b[i]; c == '"' || c == '\'' {
			j := i + 1
			for j < len(b) && b[j] != c {
				if c == '"' && b[j] == '\\' {
					return nil, false
				}
				j++
			}
			if j >= len(b) {
				return nil, false
			}
			part, i = string(b[i+1:j]), j+1
		} else {
			j := i
			for j < len(b) && isBare(b[j]) {
				j++
			}
			if j == i {
				return nil, false
			}
			part, i = string(b[i:j]), j
		}
		parts = append(parts, part)
		i = skipSpace(b, i, len(b))
		if i == len(b) {
			return parts, true
		}
		if b[i] != '.' {
			return nil, false
		}
		i++
	}
}

func isBare(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' || c == '-'
}

// lineEnding is CRLF if most of the file's line breaks are, otherwise LF.
func lineEnding(data []byte) string {
	crlf := bytes.Count(data, []byte("\r\n"))
	if crlf > bytes.Count(data, []byte("\n"))-crlf {
		return "\r\n"
	}
	return "\n"
}

// quote writes s as a TOML basic string, escaping control characters.
func quote(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch {
		case r == '\\' || r == '"':
			b.WriteByte('\\')
			b.WriteRune(r)
		case r == '\b':
			b.WriteString(`\b`)
		case r == '\t':
			b.WriteString(`\t`)
		case r == '\n':
			b.WriteString(`\n`)
		case r == '\f':
			b.WriteString(`\f`)
		case r == '\r':
			b.WriteString(`\r`)
		case r < 0x20 || r == 0x7f:
			fmt.Fprintf(&b, `\u%04X`, r)
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}

func locationKey(s Section) (key, value string) {
	if text, isDSN := sourcetext.Of(s.Path, s.DSN); isDSN {
		return "dsn", text
	}
	return "path", s.Path
}

// terminator is the line break that ends l, or "" if it has none.
func terminator(data []byte, l line) string { return string(data[l.end:l.next]) }

// valueLines are the key lines of a section, in the order they are written.
func valueLines(s Section) []string {
	k, v := locationKey(s)
	out := []string{k + " = " + quote(v)}
	if s.Write {
		out = append(out, writeLine)
	}
	if s.MaxRows > 0 {
		out = append(out, maxRowsLine(s.MaxRows))
	}
	return out
}

const writeLine = "write = true"

func maxRowsLine(n int) string { return "max_rows = " + strconv.Itoa(n) }

// addText appends the section, with a blank line before it if one is needed.
func addText(data []byte, s Section) []byte {
	eol := lineEnding(data)
	out := append([]byte(nil), data...)
	if len(out) > 0 && out[len(out)-1] != '\n' {
		out = append(out, eol...)
	}
	if lines := scan(out); len(lines) > 0 && lines[len(lines)-1].kind != blankLine {
		out = append(out, eol...)
	}
	out = append(out, "[sources."+s.Name+"]"+eol...)
	for _, l := range valueLines(s) {
		out = append(out, l+eol...)
	}
	return out
}

// hasRootSources reports a top-level `sources = ...` or `sources.x... = ...`.
func hasRootSources(lines []line) bool {
	for _, l := range lines {
		if l.kind == headerLine {
			return false
		}
		if l.kind == keyLine && len(l.key) > 0 && l.key[0] == "sources" {
			return true
		}
	}
	return false
}

// renameText writes name in place of the last part of the header on l, keeping the rest of
// the line.
func renameText(data []byte, l line, name string) ([]byte, error) {
	i := skipSpace(data, l.start, l.end) + 1
	dot, closing := -1, -1
	for j := i; j < l.end && closing < 0; {
		switch data[j] {
		case '"', '\'':
			j = skipQuoted(data, j, l.end)
			continue
		case '.':
			dot = j
		case ']':
			closing = j
		}
		j++
	}
	if dot < 0 || closing < 0 {
		return nil, fmt.Errorf("%w: header", ErrUnsupported)
	}
	from, to := skipSpace(data, dot+1, closing), closing
	for to > from && (data[to-1] == ' ' || data[to-1] == '\t') {
		to--
	}
	if from == to {
		return nil, fmt.Errorf("%w: header", ErrUnsupported)
	}
	return applyEdits(data, []edit{{from, to, name}}), nil
}

// section is where one [sources.<name>] lives in the lines: its header, the next header and
// its last key.
type section struct {
	header int
	end    int
	last   int
}

// findSection returns the section for name; ok says one exists and many that more than one
// does.
func findSection(lines []line, name string) (sec section, ok, many bool) {
	for i, l := range lines {
		if l.kind != headerLine || l.array || len(l.key) != 2 || l.key[0] != "sources" || l.key[1] != name {
			continue
		}
		if ok {
			return sec, true, true
		}
		sec, ok = section{header: i, last: i}, true
		sec.end = len(lines)
		for j := i + 1; j < len(lines); j++ {
			if lines[j].kind == headerLine {
				sec.end = j
				break
			}
			if lines[j].kind == keyLine || lines[j].kind == contLine {
				sec.last = j
			}
		}
	}
	return sec, ok, false
}

type edit struct {
	from, to int
	text     string
}

func applyEdits(data []byte, edits []edit) []byte {
	sort.SliceStable(edits, func(i, j int) bool { return edits[i].from > edits[j].from })
	out := append([]byte(nil), data...)
	for _, e := range edits {
		out = append(out[:e.from], append([]byte(e.text), out[e.to:]...)...)
	}
	return out
}

// removeText drops the section: its header, its lines to the last key, its own comment
// block and the blank lines after.
func removeText(data []byte, lines []line, sec section) []byte {
	first := sec.header
	j := sec.header - 1
	for j >= 0 && lines[j].kind == commentLine {
		j--
	}
	if j+1 < sec.header && j >= 0 && lines[j].kind == blankLine {
		first = j + 1
	}
	k := sec.last + 1
	for k < sec.end && lines[k].kind == blankLine {
		k++
	}
	if k == len(lines) {
		for first > 0 && lines[first-1].kind == blankLine {
			first--
		}
	}
	return applyEdits(data, []edit{{lines[first].start, lines[k-1].next, ""}})
}

// replaceText makes the section hold s by changing only the key lines that differ.
func replaceText(data []byte, lines []line, sec section, old entry, s Section) ([]byte, error) {
	eol := terminator(data, lines[sec.last])
	if eol == "" {
		eol = lineEnding(data)
	}
	keys := map[string]line{}
	for i := sec.header + 1; i < sec.end; i++ {
		if l := lines[i]; l.kind == keyLine && len(l.key) == 1 {
			if _, seen := keys[l.key[0]]; !seen {
				keys[l.key[0]] = l
			}
		}
	}

	var edits []edit
	var insert []string
	rewrite := func(l line, key, value string) error {
		if l.multi {
			return unsupported(s.Name, "a value that runs over several lines would have to be rewritten")
		}
		if len(l.key) == 1 && l.key[0] == key {
			edits = append(edits, edit{l.valStart, l.valEnd, value})
		} else {
			edits = append(edits, edit{l.keyStart, l.valEnd, key + string(data[l.keyEnd:l.valStart]) + value})
		}
		return nil
	}
	droppedLast := false
	drop := func(l line) {
		edits = append(edits, edit{l.start, l.next, ""})
		droppedLast = droppedLast || l.start == lines[sec.last].start
	}

	lk, lv := locationKey(s)
	cur, ok := keys["path"]
	curKey, curVal := "path", old.Path
	if !ok {
		cur, ok = keys["dsn"]
		curKey, curVal = "dsn", old.DSN
	}
	if !ok {
		return nil, unsupported(s.Name, "it has no path or dsn line")
	}
	if curKey != lk || curVal != lv {
		if err := rewrite(cur, lk, quote(lv)); err != nil {
			return nil, err
		}
	}

	if l, ok := keys["write"]; ok {
		switch {
		case old.Write == s.Write:
		case s.Write:
			if err := rewrite(l, "write", "true"); err != nil {
				return nil, err
			}
		default:
			drop(l)
		}
	} else if s.Write {
		insert = append(insert, writeLine)
	}

	if l, ok := keys["max_rows"]; ok {
		switch {
		case s.MaxRows == 0:
			drop(l)
		case old.MaxRows == nil || *old.MaxRows != s.MaxRows:
			if err := rewrite(l, "max_rows", strconv.Itoa(s.MaxRows)); err != nil {
				return nil, err
			}
		}
	} else if s.MaxRows > 0 {
		insert = append(insert, maxRowsLine(s.MaxRows))
	}

	if len(insert) > 0 {
		last := lines[sec.last]
		indent := ""
		if last.kind == keyLine {
			indent = string(data[last.start:last.keyStart])
		}
		text := ""
		for _, l := range insert {
			text += indent + l + eol
		}
		if len(data) > 0 && data[len(data)-1] != '\n' && last.next == len(data) && !droppedLast {
			text = eol + text
		}
		edits = append(edits, edit{last.next, last.next, text})
	}
	return applyEdits(data, edits), nil
}
