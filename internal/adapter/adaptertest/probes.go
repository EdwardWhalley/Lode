// The probe texts every Adapter's engine reads, and the check that sqltext reads them the
// same way under the Kind's Dialect.

package adaptertest

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/EdwardWhalley/lode/internal/adapter"
	"github.com/EdwardWhalley/lode/internal/sqltext"
)

// probe is a text built so that the engine's answer shows how it read it. cursorAt is the
// text a cursor stands at the start of for StatementToRun, the end of the text if empty.
// notSQLWhere names a reading under which the text is no SQL at all, so that an engine
// that reads it so refuses it, and that refusal counts as agreeing.
type probe struct {
	text        string
	cursorAt    string
	notSQLWhere string
}

const (
	noEStrings    = "E before a string is no prefix, so the string ends at the quote after the backslash"
	noDollars     = "$ starts no string"
	noBacktickIDs = "a backtick quotes no name"
)

// probes reads the same on every engine whose Dialect is right: nested comments, comments
// and strings left open, backslash escapes, # comments, E strings, dollar quotes, doubled
// quotes and quoted names, each with a ; inside. No probe names a real table, so a wrong cut
// on a writable Source changes nothing.
var probes = []probe{
	{text: `/* /* */ SELECT 1`},
	{text: "/* outer /* inner */ ;\nDELETE FROM x;\n*/ SELECT 1;", cursorAt: "FROM x"},
	{text: `/* a /* b */ ; c */ SELECT 1`, cursorAt: " c "},
	{text: `/* 1 /* 2 /* 3 */ ; */ ; */ SELECT 1`, cursorAt: " ; */ ;"},
	{text: `/* 1 /* 2 /* 3 */ ; */ ; */ SELECT 1; SELECT 2`},
	{text: `SELECT 1 /* /* */ ; SELECT 2 -- */`},
	{text: "/* /* */ -- */ SELECT 1;\nSELECT 2"},
	{text: `/* /* ' */ ; SELECT 1; ' */ SELECT 2`},
	{text: `/* a */ SELECT 1; /* b; */ SELECT 2`},
	{text: "SELECT 1 -- /* \n; SELECT 2 -- */"},
	{text: `SELECT '/*'; SELECT 2 -- */`},

	{text: `SELECT 1 /* ; SELECT 2`},
	{text: `SELECT 1 /* /* */ ; SELECT 2`},
	{text: `SELECT 1 /* /* */ AS x`},
	{text: `/* a /* b */ ; SELECT 1`},
	{text: `SELECT 'a; SELECT 2`},

	{text: `SELECT 'a\'; DELETE FROM t; -- '`},
	{text: `SELECT "a\"; DELETE FROM t; -- "`},
	{text: `SELECT 'a\';b'; SELECT 2`},
	{text: `SELECT 'a\'; SELECT 2`},
	{text: `SELECT "a\";b"; SELECT 2`},

	{text: "SELECT 1 # ; DELETE FROM t"},
	{text: `SELECT 1 # 2; SELECT 3`},
	{text: "SELECT 1 # a;b\n; SELECT 2"},

	{text: `SELECT E'it\'s; ok'`, notSQLWhere: noEStrings},
	{text: `SELECT e'it\'s; ok'`, notSQLWhere: noEStrings},
	{text: `SELECT E'it\'s; ok'; SELECT 2`},
	{text: `SELECT E'\''; SELECT 2 --'`},

	{text: `SELECT $$a;b$$`, notSQLWhere: noDollars},
	{text: `SELECT $q$a;b$q$`, notSQLWhere: noDollars},
	{text: `SELECT $a$x; $b$; $a$`, notSQLWhere: noDollars},
	{text: `SELECT 1 AS a$b; SELECT 2 -- $b$`},

	{text: `SELECT 'it''s; ok'`},
	{text: `SELECT 1 AS "a"";b"`},
	{text: "SELECT 1 AS `a;b`", notSQLWhere: noBacktickIDs},
}

// answer is what an engine gave for a text: its rows, and whether it had columns.
type answer struct {
	columns bool
	rows    [][]any
}

// ask runs text on a and reads all of it.
func ask(a adapter.Adapter, text string) (answer, error) {
	rows, err := a.Query(context.Background(), text)
	if err != nil {
		return answer{}, err
	}
	defer rows.Close()
	got := answer{columns: len(rows.Columns()) > 0}
	for rows.Next() {
		got.rows = append(got.rows, append([]any(nil), rows.Values()...))
	}
	return got, rows.Err()
}

// readsAsTheEngineDoes checks one probe: the engine refuses what sqltext reads as nothing to
// run, left open, or several Statements (an engine may refuse more than one in a run), and
// otherwise gives for the whole text what it gives for the Statement StatementToRun chooses.
func readsAsTheEngineDoes(t *testing.T, a adapter.Adapter, d sqltext.Dialect, p probe) {
	t.Helper()
	cursor := len(p.text)
	if p.cursorAt != "" {
		if cursor = strings.Index(p.text, p.cursorAt); cursor < 0 {
			t.Fatalf("probe %q has no %q to put a cursor at", p.text, p.cursorAt)
		}
	}
	whole, wholeErr := ask(a, p.text)
	pieces := sqltext.Statements(p.text, d)

	if sqltext.IsBlank(p.text, d) {
		if wholeErr == nil && whole.columns {
			t.Errorf("%q: sqltext reads nothing to run, and the engine answered %v", p.text, whole.rows)
		}
		return
	}
	if wholeErr != nil {
		switch {
		case pieces[len(pieces)-1].Open, len(pieces) > 1:
		case p.notSQLWhere != "":
			t.Logf("%q: refused, which counts as the reading where %s: %v", p.text, p.notSQLWhere, wholeErr)
		default:
			t.Errorf("%q: sqltext reads one Statement, closed, and the engine refused it: %v", p.text, wholeErr)
		}
		return
	}
	chosen, ok := sqltext.StatementToRun(p.text, cursor, d)
	if !ok {
		t.Errorf("%q: the engine answered %v, and sqltext chooses nothing to run", p.text, whole.rows)
		return
	}
	got, err := ask(a, chosen.Text)
	if err != nil || !reflect.DeepEqual(got, whole) {
		t.Errorf("%q: the engine answered %v for the whole text, and %v, %v for %q, the Statement sqltext chooses",
			p.text, whole.rows, got.rows, err, chosen.Text)
	}
}
