// Package sqltext reads SQL as text, knowing no Source and no schema. This file is the
// scanner: where a string, a quoted name or a comment begins and ends.
package sqltext

import "unicode"

// Dialect is the lexical rules an engine has beyond those the scanner applies to every
// engine: with BackslashEscapes a backslash escapes the next character in '...' and "...",
// with HashComments # starts a comment that runs to the end of the line, and with
// NestedComments a /* inside a block comment opens another, so the comment ends only at the
// */ that closes the first. Its zero value is none of them, which is what SQLite needs. An
// Adapter's Kind sets the ones its engine has.
type Dialect struct {
	BackslashEscapes bool
	HashComments     bool
	NestedComments   bool
}

// lexClass says what a stretch of SQL text is, for the code that reads it.
type lexClass int

const (
	lexCode lexClass = iota
	lexString
	lexIdent
	lexComment
)

type scanState int

const (
	inCode scanState = iota
	inString
	inIdent
	inLine
	inBlock
	inDollar
)

// scanner walks SQL text one lexical unit at a time, the one place that knows where
// strings, quoted names and comments begin and end.
type scanner struct {
	rs      []rune
	i       int
	dialect Dialect
	state   scanState
	escaped bool
	quote   rune
	tag     string
	depth   int
}

func newScanner(text string, d Dialect) *scanner {
	return &scanner{rs: []rune(text), dialect: d}
}

func (s *scanner) more() bool { return s.i < len(s.rs) }

// open reports that the text read so far ends inside a string, a comment or a dollar quote.
func (s *scanner) open() bool {
	return s.state == inString || s.state == inLine || s.state == inBlock || s.state == inDollar
}

// unterminated reports that the text read so far ends inside something not closed, a quoted
// identifier included; a line comment is closed by the end of the text.
func (s *scanner) unterminated() bool { return s.state != inCode && s.state != inLine }

// has reports that the text at i starts with want.
func (s *scanner) has(i int, want string) bool {
	for _, w := range want {
		if i >= len(s.rs) || s.rs[i] != w {
			return false
		}
		i++
	}
	return true
}

// next reads one unit, from lo to hi in runes, and says what it is.
func (s *scanner) next() (lo, hi int, class lexClass) {
	lo = s.i
	r := s.rs[lo]
	s.i++

	closing := func(q rune) bool {
		if s.i < len(s.rs) && s.rs[s.i] == q {
			s.i++
			return false
		}
		return true
	}

	switch s.state {
	case inLine:
		if r == '\n' {
			s.state = inCode
		}
		return lo, s.i, lexComment
	case inBlock:
		switch {
		case r == '*' && s.has(s.i, "/"):
			s.i++
			if s.depth--; s.depth == 0 {
				s.state = inCode
			}
		case r == '/' && s.has(s.i, "*") && s.dialect.NestedComments:
			s.i++
			s.depth++
		}
		return lo, s.i, lexComment
	case inString:
		switch {
		case (s.dialect.BackslashEscapes || s.escaped) && r == '\\':
			s.i = min(s.i+1, len(s.rs))
		case r == '\'':
			if closing('\'') {
				s.state = inCode
			}
		}
		return lo, s.i, lexString
	case inIdent:
		switch {
		case s.dialect.BackslashEscapes && s.quote == '"' && r == '\\':
			s.i = min(s.i+1, len(s.rs))
		case r == s.quote && closing(s.quote):
			s.state = inCode
		}
		return lo, s.i, lexIdent
	case inDollar:
		if r == '$' && s.has(lo, "$"+s.tag+"$") {
			s.i = lo + len([]rune(s.tag)) + 2
			s.state = inCode
		}
		return lo, s.i, lexString
	}

	switch {
	case r == '\'':
		s.state, s.escaped = inString, s.afterE(lo)
		return lo, s.i, lexString
	case r == '"' || r == '`':
		s.state, s.quote = inIdent, r
		return lo, s.i, lexIdent
	case r == '-' && s.has(s.i, "-"):
		s.state = inLine
		s.i++
		return lo, s.i, lexComment
	case r == '#' && s.dialect.HashComments:
		s.state = inLine
		return lo, s.i, lexComment
	case r == '/' && s.has(s.i, "*"):
		s.state, s.depth = inBlock, 1
		s.i++
		return lo, s.i, lexComment
	case r == '$' && (lo == 0 || !IsWordRune(s.rs[lo-1])):
		j := s.i
		for j < len(s.rs) && (s.rs[j] == '_' || unicode.IsLetter(s.rs[j]) || (j > s.i && unicode.IsDigit(s.rs[j]))) {
			j++
		}
		if j < len(s.rs) && s.rs[j] == '$' {
			s.state, s.tag = inDollar, string(s.rs[s.i:j])
			s.i = j + 1
			return lo, s.i, lexString
		}
	}
	return lo, s.i, lexCode
}

// afterE reports that the quote at i makes an E'...' string.
func (s *scanner) afterE(i int) bool {
	if i == 0 || (s.rs[i-1] != 'E' && s.rs[i-1] != 'e') {
		return false
	}
	return i == 1 || !IsWordRune(s.rs[i-2])
}

// lexRun is a stretch of one class: a string, a comment, or the code between them.
type lexRun struct {
	class  lexClass
	lo, hi int
	closed bool
}

// lex splits text into runs, returned with the runes they index.
func lex(text string, d Dialect) ([]rune, []lexRun) {
	s := newScanner(text, d)
	var runs []lexRun
	for s.more() {
		lo, hi, class := s.next()
		if n := len(runs); n > 0 && runs[n-1].class == class {
			runs[n-1].hi = hi
		} else {
			runs = append(runs, lexRun{class: class, lo: lo, hi: hi, closed: true})
		}
	}
	if n := len(runs); n > 0 && runs[n-1].class != lexCode {
		runs[n-1].closed = !s.unterminated()
	}
	return s.rs, runs
}

// InLiteralOrComment reports whether the end of before is inside a string or comment, where
// a table name is not wanted.
func InLiteralOrComment(before string, d Dialect) bool {
	s := newScanner(before, d)
	for s.more() {
		s.next()
	}
	return s.open()
}

// IsWordRune says r can be part of a word being typed: a letter, digit, underscore or name
// quote.
func IsWordRune(r rune) bool {
	return r == '_' || r == '"' || r == '`' || unicode.IsLetter(r) || unicode.IsDigit(r)
}
