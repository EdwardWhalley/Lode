// Finds the table aliases a statement defines, for autocomplete.

package sqltext

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// clauseWords end a table reference, so the word after a table that is one of these is not
// an alias.
var clauseWords = func() map[string]bool {
	m := map[string]bool{}
	for _, w := range strings.Fields(`where on using group order having limit offset union intersect
		except set left right inner outer full cross natural join window fetch for returning
		lateral values select with`) {
		m[w] = true
	}
	return m
}()

type tokenKind int

const (
	tokWord tokenKind = iota
	tokQuoted
	tokPunct
	tokOpaque
)

type token struct {
	kind tokenKind
	text string
}

// tokens reads statement as the code in this package needs it: words, quoted names and
// single punctuation characters.
func tokens(statement string, d Dialect) []token {
	rs, runs := lex(statement, d)
	var out []token
	for _, run := range runs {
		switch run.class {
		case lexComment:
		case lexString:
			out = append(out, token{kind: tokOpaque})
		case lexIdent:
			if !run.closed {
				out = append(out, token{kind: tokOpaque})
				continue
			}
			q := rs[run.lo]
			name := strings.ReplaceAll(string(rs[run.lo+1:run.hi-1]), string([]rune{q, q}), string(q))
			out = append(out, token{kind: tokQuoted, text: name})
		default:
			start := -1
			flush := func(end int) {
				if start >= 0 {
					out = append(out, token{kind: tokWord, text: string(rs[start:end])})
					start = -1
				}
			}
			for i := run.lo; i < run.hi; i++ {
				switch r := rs[i]; {
				case r == '_' || r == '$' || unicode.IsLetter(r) || unicode.IsDigit(r):
					if start < 0 {
						start = i
					}
				case unicode.IsSpace(r):
					flush(i)
				default:
					flush(i)
					out = append(out, token{kind: tokPunct, text: string(r)})
				}
			}
			flush(run.hi)
		}
	}
	return out
}

func (t token) isWord(w string) bool { return t.kind == tokWord && strings.EqualFold(t.text, w) }

func (t token) isPunct(p string) bool { return t.kind == tokPunct && t.text == p }

// isName says t can be the name of a table.
func (t token) isName() bool {
	switch t.kind {
	case tokQuoted:
		return true
	case tokWord:
		r, _ := utf8.DecodeRuneInString(t.text)
		return (r == '_' || unicode.IsLetter(r)) && !clauseWords[strings.ToLower(t.text)]
	}
	return false
}

// Aliases finds the table aliases a statement defines: the lower-cased alias to the tables
// it stands for.
func Aliases(statement string, d Dialect) map[string][]string {
	out := map[string][]string{}
	toks := tokens(statement, d)
	for i, t := range toks {
		switch {
		case t.isWord("from"), t.isWord("join"):
		case t.isWord("update"):
			if i > 0 && (toks[i-1].isWord("for") || toks[i-1].isWord("key")) {
				continue
			}
		default:
			continue
		}
		referencesFrom(toks, i+1, out)
	}
	return out
}

// referencesFrom reads a list of table references starting at toks[i] and adds
// their aliases to out.
func referencesFrom(toks []token, i int, out map[string][]string) {
	for {
		if i >= len(toks) || !toks[i].isName() {
			return
		}
		table := toks[i].text
		i++
		for i+1 < len(toks) && toks[i].kind == tokPunct && toks[i].text == "." && toks[i+1].isName() {
			table = toks[i+1].text
			i += 2
		}
		withAs := i < len(toks) && toks[i].isWord("as")
		if withAs {
			i++
		}
		if i < len(toks) && toks[i].isName() && !toks[i].isWord("as") {
			addAlias(out, strings.ToLower(toks[i].text), table)
			i++
		}
		if i >= len(toks) || toks[i].kind != tokPunct || toks[i].text != "," {
			return
		}
		i++
	}
}

func addAlias(out map[string][]string, alias, table string) {
	if clauseWords[alias] {
		return
	}
	for _, have := range out[alias] {
		if have == table {
			return
		}
	}
	out[alias] = append(out[alias], table)
}

// StatementAround returns the statement the byte offset cursor is in, between the
// semicolons either side.
func StatementAround(text string, cursor int, d Dialect) string {
	pieces := Statements(text, d)
	for _, s := range pieces {
		if cursor <= s.End {
			return text[s.Start:s.End]
		}
	}
	last := pieces[len(pieces)-1]
	return text[last.Start:last.End]
}
