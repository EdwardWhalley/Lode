// Cuts text into Statements at semicolons, and answers questions about them.

package sqltext

import (
	"strings"
	"unicode"
)

// Statement is one piece of a text between two semicolons outside strings, quoted names and
// comments. Code and CodeEnd are where its SQL begins and ends, past the space and comments
// around it, and Open says the text ends inside a string, a quoted name or a comment that
// began in this piece.
type Statement struct {
	Start, End    int
	Code, CodeEnd int
	Blank, Open   bool
}

// Statements cuts text into its pieces, in order, blank ones included, read with the rules
// of d.
func Statements(text string, d Dialect) []Statement {
	rs, runs := lex(text, d)
	offsets := make([]int, 0, len(rs)+1)
	for b := range text {
		offsets = append(offsets, b)
	}
	offsets = append(offsets, len(text))

	var out []Statement
	start, code, codeEnd := 0, -1, -1
	end := func(at int) {
		s := Statement{Start: start, End: at, Code: code, CodeEnd: codeEnd, Blank: code < 0}
		if s.Blank {
			s.Code, s.CodeEnd = at, at
		}
		out = append(out, s)
	}
	for _, run := range runs {
		switch run.class {
		case lexComment:
		case lexCode:
			for i := run.lo; i < run.hi; i++ {
				switch {
				case rs[i] == ';':
					end(offsets[i])
					start, code, codeEnd = offsets[i+1], -1, -1
				case !unicode.IsSpace(rs[i]):
					if code < 0 {
						code = offsets[i]
					}
					codeEnd = offsets[i+1]
				}
			}
		default:
			if code < 0 {
				code = offsets[run.lo]
			}
			codeEnd = offsets[run.hi]
		}
	}
	end(len(text))
	if n := len(runs); n > 0 && !runs[n-1].closed {
		out[len(out)-1].Open = true
	}
	return out
}

// IsBlank says every piece of text is blank, so there is nothing in it to run.
func IsBlank(text string, d Dialect) bool {
	for _, s := range Statements(text, d) {
		if !s.Blank {
			return false
		}
	}
	return true
}

// LeavesTransactionOpen says text ends with a transaction it opened still open, for saying
// why on an engine that cannot be asked.
func LeavesTransactionOpen(text string, d Dialect) bool {
	open := false
	for _, s := range Statements(text, d) {
		toks := tokens(text[s.Code:s.End], d)
		if len(toks) == 0 {
			continue
		}
		has := func(word string) bool {
			for _, t := range toks[1:] {
				if t.isWord(word) {
					return true
				}
			}
			return false
		}
		switch first := toks[0]; {
		case first.isWord("begin"):
			open = true
		case first.isWord("start"):
			open = open || has("transaction")
		case first.isWord("xa"):
			open = open || has("start") || has("begin")
		case first.isWord("commit") || first.isWord("end") || first.isWord("rollback") || first.isWord("abort"):
			if !has("to") {
				open = chains(toks)
			}
		}
	}
	return open
}

// chains says a COMMIT or ROLLBACK starts the next transaction: AND CHAIN and not AND NO
// CHAIN.
func chains(toks []token) bool {
	for i, t := range toks {
		if t.isWord("chain") {
			return !toks[i-1].isWord("no")
		}
	}
	return false
}

// LastStatementVerb is the upper-case word for what the last non-blank Statement does, past
// any WITH, or "".
func LastStatementVerb(text string, d Dialect) string {
	last := ""
	for _, s := range Statements(text, d) {
		if !s.Blank {
			last = text[s.Code:s.End]
		}
	}
	toks := tokens(last, d)
	if len(toks) == 0 || toks[0].kind != tokWord {
		return ""
	}
	if !toks[0].isWord("with") {
		return strings.ToUpper(toks[0].text)
	}
	depth := 0
	for _, t := range toks[1:] {
		switch {
		case t.isPunct("("):
			depth++
		case t.isPunct(")"):
			depth--
		case depth == 0 && (t.isWord("select") || t.isWord("insert") || t.isWord("update") || t.isWord("delete") || t.isWord("replace")):
			return strings.ToUpper(t.text)
		}
	}
	return ""
}

// Word is a word of a statement outside its strings, quoted names and comments, with the
// number of brackets it is inside.
type Word struct {
	Text  string
	Depth int
}

// Words reads the words of statement in order, with the rules of d.
func Words(statement string, d Dialect) []Word {
	var out []Word
	depth := 0
	for _, t := range tokens(statement, d) {
		switch {
		case t.isPunct("("):
			depth++
		case t.isPunct(")"):
			depth--
		case t.kind == tokWord:
			out = append(out, Word{Text: t.text, Depth: depth})
		}
	}
	return out
}

// MayReturnRows says a statement that writes could also return rows: it has a RETURNING
// clause.
func MayReturnRows(statement string, d Dialect) bool {
	toks := tokens(statement, d)
	for i, t := range toks {
		if !t.isWord("returning") {
			continue
		}
		named := i > 0 && (toks[i-1].isWord("as") || toks[i-1].isPunct("."))
		qualifies := i+1 < len(toks) && toks[i+1].isPunct(".")
		if !named && !qualifies {
			return true
		}
	}
	return false
}

// Chosen is the Statement that running with no Selection means.
type Chosen struct {
	Text         string
	Position, Of int
}

// StatementToRun finds the Statement the byte offset cursor means: the one it is in, or the
// nearest non-blank one before.
func StatementToRun(text string, cursor int, d Dialect) (Chosen, bool) {
	pieces := Statements(text, d)
	at := len(pieces) - 1
	for i, s := range pieces {
		if cursor <= s.End {
			at = i
			break
		}
	}
	if !pieces[at].Blank && !leadsInto(text, pieces[at], cursor) {
		at--
	}

	var chosen Chosen
	found := false
	for i, s := range pieces {
		if s.Blank {
			continue
		}
		chosen.Of++
		if i <= at {
			chosen.Text = strings.TrimSpace(text[s.Start:s.End])
			chosen.Position = chosen.Of
			found = true
		}
	}
	return chosen, found
}

// leadsInto says a cursor in the piece s means s and not the Statement before it.
func leadsInto(text string, s Statement, cursor int) bool {
	if cursor >= s.Code || s.Start == 0 {
		return true
	}
	cursor = max(cursor, s.Start)
	if !strings.Contains(text[s.Start:cursor], "\n") {
		return false
	}
	between := strings.Split(text[cursor:s.Code], "\n")
	for _, line := range between[1:max(1, len(between)-1)] {
		if strings.TrimSpace(line) == "" {
			return false
		}
	}
	return true
}
