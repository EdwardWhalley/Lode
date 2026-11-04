package sqltext

import (
	"slices"
	"strings"
	"testing"
)

// pieces reads Statements as the text of each, with a mark on the blank ones.
func pieces(text string, d Dialect) []string {
	var out []string
	for _, s := range Statements(text, d) {
		piece := text[s.Start:s.End]
		if s.Blank {
			piece = "(blank)" + piece
		}
		out = append(out, piece)
	}
	return out
}

func TestStatementsAreTheTextBetweenSemicolons(t *testing.T) {
	cases := []struct {
		name  string
		text  string
		mysql bool
		want  []string
	}{
		{"one", `SELECT 1`, false, []string{`SELECT 1`}},
		{"the space around the SQL belongs to it", "\n SELECT 1 \n", false, []string{"\n SELECT 1 \n"}},
		{"nothing at all is one blank piece", ``, false, []string{`(blank)`}},
		{"a final semicolon leaves a blank piece", `SELECT 1;`, false, []string{`SELECT 1`, `(blank)`}},
		{"three", `SELECT 1; SELECT 2; SELECT 3`, false, []string{`SELECT 1`, ` SELECT 2`, ` SELECT 3`}},
		{"only space is blank", "SELECT 1;\n  \n;SELECT 2", false, []string{`SELECT 1`, "(blank)\n  \n", `SELECT 2`}},
		{"only comments are blank", "SELECT 1; -- a note\n/* and; another */ ;SELECT 2", false,
			[]string{`SELECT 1`, "(blank) -- a note\n/* and; another */ ", `SELECT 2`}},
		{"a comment beside SQL is part of it", "-- first\nSELECT 1", false, []string{"-- first\nSELECT 1"}},

		{"a semicolon in a string", `SELECT 'a;b'; SELECT 2`, false, []string{`SELECT 'a;b'`, ` SELECT 2`}},
		{"a doubled quote in a string", `SELECT 'it''s;'; SELECT 2`, false, []string{`SELECT 'it''s;'`, ` SELECT 2`}},
		{"a semicolon in a quoted name", `SELECT "a;b"; SELECT 2`, false, []string{`SELECT "a;b"`, ` SELECT 2`}},
		{"a semicolon in a backticked name", "SELECT `a;b`; SELECT 2", false, []string{"SELECT `a;b`", ` SELECT 2`}},
		{"a semicolon in a line comment", "SELECT 1 -- a;b\n; SELECT 2", false, []string{"SELECT 1 -- a;b\n", ` SELECT 2`}},
		{"a semicolon in a block comment", `SELECT 1 /* a;b */; SELECT 2`, false, []string{`SELECT 1 /* a;b */`, ` SELECT 2`}},
		{"a semicolon in dollar quotes", `SELECT $$a;b$$; SELECT 2`, false, []string{`SELECT $$a;b$$`, ` SELECT 2`}},
		{"a semicolon in tagged dollar quotes", `SELECT $q$a;b$q$; SELECT 2`, false, []string{`SELECT $q$a;b$q$`, ` SELECT 2`}},
		{"a string left open runs to the end", `SELECT 'a; SELECT 2`, false, []string{`SELECT 'a; SELECT 2`}},
		{"an empty string is not blank", `''`, false, []string{`''`}},

		{"a backslash is plain text where it escapes nothing", `SELECT 'a\'; SELECT 2`, false, []string{`SELECT 'a\'`, ` SELECT 2`}},
		{"a backslash escapes a quote where the Source says so", `SELECT 'a\';b'; SELECT 2`, true, []string{`SELECT 'a\';b'`, ` SELECT 2`}},
		{"an E string has backslash escapes on any Source", `SELECT E'it\'s; ok'; SELECT 2`, false, []string{`SELECT E'it\'s; ok'`, ` SELECT 2`}},
		{"a lower case e string too", `SELECT e'it\'s; ok'; SELECT 2`, false, []string{`SELECT e'it\'s; ok'`, ` SELECT 2`}},
		{"a word ending in e before a string is not an E string", `SELECT type'a\'; SELECT 2`, false, []string{`SELECT type'a\'`, ` SELECT 2`}},
		{"a double quote escapes where a backslash does", `SELECT "a\"; DELETE FROM t; -- "; SELECT 2`, true,
			[]string{`SELECT "a\"; DELETE FROM t; -- "`, ` SELECT 2`}},
		{"a double quote does not escape elsewhere", `SELECT "a\"; SELECT 2`, false, []string{`SELECT "a\"`, ` SELECT 2`}},
		{"a backtick never escapes", "SELECT `a\\`; SELECT 2", true, []string{"SELECT `a\\`", ` SELECT 2`}},
		{"a # comment where a backslash escapes", "SELECT 1 # a; DELETE FROM t\n; SELECT 2", true,
			[]string{"SELECT 1 # a; DELETE FROM t\n", ` SELECT 2`}},
		{"# is not a comment elsewhere", `SELECT 1 # 2; SELECT 3`, false, []string{`SELECT 1 # 2`, ` SELECT 3`}},
		{"wide characters keep their places", `SELECT 'é'; SELECT '世'`, false, []string{`SELECT 'é'`, ` SELECT '世'`}},
		{"a byte that is not text keeps its place", "SELECT '\xff'; SELECT 2", false, []string{"SELECT '\xff'", ` SELECT 2`}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := pieces(c.text, dialectOf(c.mysql))
			if len(got) != len(c.want) {
				t.Fatalf("Statements(%q) = %q, want %q", c.text, got, c.want)
			}
			for i := range got {
				if got[i] != c.want[i] {
					t.Fatalf("Statements(%q) = %q, want %q", c.text, got, c.want)
				}
			}
		})
	}
}

func TestEachDialectFlagAloneChangesTheCut(t *testing.T) {
	escapes := Dialect{BackslashEscapes: true}
	hashes := Dialect{HashComments: true}
	cases := []struct {
		name    string
		text    string
		dialect Dialect
		want    []string
	}{
		{"a backslash escapes a quote with BackslashEscapes", `SELECT 'a\';b'; SELECT 2`, escapes,
			[]string{`SELECT 'a\';b'`, ` SELECT 2`}},
		{"and not with HashComments", `SELECT 'a\';b'; SELECT 2`, hashes,
			[]string{`SELECT 'a\'`, `b'; SELECT 2`}},
		{"a backslash escapes a double quote with BackslashEscapes", `SELECT "a\";b"; SELECT 2`, escapes,
			[]string{`SELECT "a\";b"`, ` SELECT 2`}},
		{"and not with HashComments", `SELECT "a\";b"; SELECT 2`, hashes,
			[]string{`SELECT "a\"`, `b"; SELECT 2`}},
		{"# starts a comment with HashComments", "SELECT 1 # a;b\n; SELECT 2", hashes,
			[]string{"SELECT 1 # a;b\n", ` SELECT 2`}},
		{"and not with BackslashEscapes", "SELECT 1 # a;b\n; SELECT 2", escapes,
			[]string{`SELECT 1 # a`, "b\n", ` SELECT 2`}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := pieces(c.text, c.dialect)
			if len(got) != len(c.want) {
				t.Fatalf("Statements(%q, %+v) = %q, want %q", c.text, c.dialect, got, c.want)
			}
			for i := range got {
				if got[i] != c.want[i] {
					t.Fatalf("Statements(%q, %+v) = %q, want %q", c.text, c.dialect, got, c.want)
				}
			}
		})
	}
}

// fault is the text that ran a commented-out DELETE on PostgreSQL before the scanner knew
// nested comments: to PostgreSQL it is one comment and then SELECT 1.
const fault = "/* outer /* inner */ ;\nDELETE FROM x;\n*/ SELECT 1;"

func TestNestedCommentsChangeTheCutOnlyWhereACommentIsNested(t *testing.T) {
	cases := []struct {
		name         string
		text         string
		nested, flat []string
	}{
		{"the fault", fault,
			[]string{"/* outer /* inner */ ;\nDELETE FROM x;\n*/ SELECT 1", "(blank)"},
			[]string{"(blank)/* outer /* inner */ ", "\nDELETE FROM x", "\n*/ SELECT 1", "(blank)"}},
		{"depth two", `/* a /* b */ ; c */ SELECT 1`,
			[]string{`/* a /* b */ ; c */ SELECT 1`},
			[]string{`(blank)/* a /* b */ `, ` c */ SELECT 1`}},
		{"depth three", `/* 1 /* 2 /* 3 */ ; */ ; */ SELECT 1; SELECT 2`,
			[]string{`/* 1 /* 2 /* 3 */ ; */ ; */ SELECT 1`, ` SELECT 2`},
			[]string{`(blank)/* 1 /* 2 /* 3 */ `, ` */ `, ` */ SELECT 1`, ` SELECT 2`}},
		{"an unnested comment reads the same", `/* a */ SELECT 1; /* b; */ SELECT 2`,
			[]string{`/* a */ SELECT 1`, ` /* b; */ SELECT 2`},
			[]string{`/* a */ SELECT 1`, ` /* b; */ SELECT 2`}},
		{"*/ in what looks like a string in a comment closes it", `/* /* ' */ ; SELECT 1; ' */ SELECT 2`,
			[]string{`/* /* ' */ ; SELECT 1; ' */ SELECT 2`},
			[]string{`(blank)/* /* ' */ `, ` SELECT 1`, ` ' */ SELECT 2`}},
		{"/* in a line comment opens nothing", "SELECT 1 -- /* \n; SELECT 2 */",
			[]string{"SELECT 1 -- /* \n", ` SELECT 2 */`},
			[]string{"SELECT 1 -- /* \n", ` SELECT 2 */`}},
		{"a line comment in a nested comment ends nothing", "/* /* */ -- */ SELECT 1;\nSELECT 2",
			[]string{`/* /* */ -- */ SELECT 1`, "\nSELECT 2"},
			[]string{"/* /* */ -- */ SELECT 1;\nSELECT 2"}},
		{"/* in a string opens nothing", `SELECT '/*'; SELECT 2 */`,
			[]string{`SELECT '/*'`, ` SELECT 2 */`},
			[]string{`SELECT '/*'`, ` SELECT 2 */`}},
		{"a comment left open at any depth runs to the end", `/* a /* b */ ; SELECT 1`,
			[]string{`(blank)/* a /* b */ ; SELECT 1`},
			[]string{`(blank)/* a /* b */ `, ` SELECT 1`}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := pieces(c.text, Dialect{NestedComments: true}); !slices.Equal(got, c.nested) {
				t.Errorf("Statements(%q) with NestedComments = %q, want %q", c.text, got, c.nested)
			}
			if got := pieces(c.text, Dialect{}); !slices.Equal(got, c.flat) {
				t.Errorf("Statements(%q) without = %q, want %q", c.text, got, c.flat)
			}
		})
	}
}

func TestAStatementSaysWhereItsSQLEndsAndWhetherItIsLeftOpen(t *testing.T) {
	cases := []struct {
		text string
		sql  []string
		open bool
	}{
		{"SELECT 1 ; ", []string{"SELECT 1", ""}, false},
		{"SELECT 1 -- note", []string{"SELECT 1"}, false},
		{"SELECT 1 /* note */ \n", []string{"SELECT 1"}, false},
		{"SELECT 'a' /* /* */ x -- */", []string{"SELECT 'a'"}, false},
		{`SELECT "a b"`, []string{`SELECT "a b"`}, false},
		{"-- only\n/* comments */", []string{""}, false},
		{"SELECT 'open", []string{"SELECT 'open"}, true},
		{`SELECT "open`, []string{`SELECT "open`}, true},
		{"SELECT $$open", []string{"SELECT $$open"}, true},
		{"SELECT 1 /* open", []string{"SELECT 1"}, true},
		{"SELECT 1 /* /* */ open", []string{"SELECT 1"}, true},
		{"SELECT 1; /* open", []string{"SELECT 1", ""}, true},
		{"SELECT 1 /* c */; 'a' -- d\n; SELECT 2", []string{"SELECT 1", "'a'", "SELECT 2"}, false},
	}
	for _, c := range cases {
		got := Statements(c.text, Dialect{NestedComments: true})
		var sql []string
		for _, s := range got {
			sql = append(sql, c.text[s.Code:s.CodeEnd])
		}
		if !slices.Equal(sql, c.sql) {
			t.Errorf("Statements(%q): SQL %q, want %q", c.text, sql, c.sql)
		}
		for i, s := range got {
			if want := c.open && i == len(got)-1; s.Open != want {
				t.Errorf("Statements(%q)[%d].Open = %v, want %v", c.text, i, s.Open, want)
			}
		}
	}
}

func TestWordsAreReadWithTheirBracketDepth(t *testing.T) {
	got := Words(`WITH x AS (SELECT 'from' /* table */ FROM "values") select (1)`, Dialect{})
	want := []Word{{"WITH", 0}, {"x", 0}, {"AS", 0}, {"SELECT", 1}, {"FROM", 1}, {"select", 0}, {"1", 1}}
	if !slices.Equal(got, want) {
		t.Errorf("Words = %v, want %v", got, want)
	}
}

func TestTheFaultRunsTheSelectAndNotTheCommentedOutDelete(t *testing.T) {
	cursor := strings.Index(fault, "FROM x")
	got, ok := StatementToRun(fault, cursor, Dialect{NestedComments: true})
	want := "/* outer /* inner */ ;\nDELETE FROM x;\n*/ SELECT 1"
	if !ok || got.Text != want || got.Position != 1 || got.Of != 1 {
		t.Errorf("StatementToRun = %+v, %v; want %q, 1 of 1", got, ok, want)
	}
	if got, _ := StatementToRun(fault, cursor, Dialect{}); got.Text != "DELETE FROM x" {
		t.Errorf("without NestedComments StatementToRun = %+v; the fault was DELETE FROM x", got)
	}
}

func TestStatementToRunIsTheOneTheCursorMeans(t *testing.T) {
	const three = `SELECT 1; SELECT 2; SELECT 3`
	cases := []struct {
		name     string
		text     string
		cursor   int
		want     string
		position int
		of       int
	}{
		{"the first", three, 3, `SELECT 1`, 1, 3},
		{"the second", three, 14, `SELECT 2`, 2, 3},
		{"the third", three, 25, `SELECT 3`, 3, 3},
		{"just before a semicolon is the one that ends there", three, 8, `SELECT 1`, 1, 3},
		{"just after a semicolon is the one that ended there", three, 9, `SELECT 1`, 1, 3},
		{"at the start of the next one is that one", three, 10, `SELECT 2`, 2, 3},
		{"touching the next one is that one", `SELECT 1;SELECT 2`, 9, `SELECT 2`, 2, 2},
		{"a final semicolon, cursor after it", `SELECT 1;`, 9, `SELECT 1`, 1, 1},

		{"after a semicolon, with a Statement on the next line", "SELECT 1;\nDELETE FROM t", 9, `SELECT 1`, 1, 2},
		{"after a comment on the semicolon's line", "SELECT 1; -- note\nDELETE FROM t", 17, `SELECT 1`, 1, 2},
		{"in a comment on the semicolon's line", "SELECT 1; -- note\nDELETE FROM t", 12, `SELECT 1`, 1, 2},
		{"a blank line with another between it and the next", "SELECT 1;\n\n\n\nDELETE FROM t", 10, `SELECT 1`, 1, 2},
		{"the second of three blank lines", "SELECT 1;\n\n\n\nDELETE FROM t", 11, `SELECT 1`, 1, 2},
		{"a comment with a blank line between it and the next", "SELECT 1;\n-- why\n\nSELECT 2", 12, `SELECT 1`, 1, 2},

		{"the blank line directly above", "SELECT 1;\n\n\n\nDELETE FROM t", 12, `DELETE FROM t`, 2, 2},
		{"a comment directly above", "SELECT 1;\n-- why\nSELECT 2", 12, "-- why\nSELECT 2", 2, 2},
		{"a blank line above a comment directly above", "SELECT 1;\n\n-- why\nSELECT 2", 10, "-- why\nSELECT 2", 2, 2},
		{"indented, on the line above", "SELECT 1;\n\n    SELECT 2", 10, `SELECT 2`, 2, 2},
		{"space before the first Statement", "\n\n  SELECT 1", 0, `SELECT 1`, 1, 1},
		{"blank space after a Statement", "SELECT 1;\n\n  ", 12, `SELECT 1`, 1, 1},
		{"a comment-only piece after a Statement", "SELECT 1;\n-- a note", 15, `SELECT 1`, 1, 1},
		{"a blank piece between two runs the one before", "SELECT 1;\n;SELECT 2", 10, `SELECT 1`, 1, 2},
		{"a blank line directly above a Statement is in it", "SELECT 1;\n\nSELECT 2", 10, `SELECT 2`, 2, 2},
		{"space around the SQL is trimmed", "  SELECT 1  ", 0, `SELECT 1`, 1, 1},
		{"a comment before the SQL goes with it", "-- why\nSELECT 1", 0, "-- why\nSELECT 1", 1, 1},
		{"a cursor past the end is at the end", `SELECT 1`, 99, `SELECT 1`, 1, 1},
		{"a cursor before the start is at the start", `SELECT 1; SELECT 2`, -1, `SELECT 1`, 1, 2},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, ok := StatementToRun(c.text, c.cursor, Dialect{})
			if !ok {
				t.Fatalf("StatementToRun(%q, %d) found nothing, want %q", c.text, c.cursor, c.want)
			}
			if got.Text != c.want || got.Position != c.position || got.Of != c.of {
				t.Errorf("StatementToRun(%q, %d) = %+v, want %q, %d of %d", c.text, c.cursor, got, c.want, c.position, c.of)
			}
		})
	}
}

func TestStatementToRunFindsNothing(t *testing.T) {
	cases := []struct {
		name   string
		text   string
		cursor int
	}{
		{"an empty text", ``, 0},
		{"only space", " \n ", 1},
		{"only a comment", `-- a note`, 3},
		{"only comments and semicolons", `/* a */; -- b`, 11},
		{"blank before the first Statement, with a semicolon between", `; SELECT 1`, 0},
		{"just after a semicolon with nothing before it", `; SELECT 1`, 1},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got, ok := StatementToRun(c.text, c.cursor, Dialect{}); ok {
				t.Errorf("StatementToRun(%q, %d) = %+v, want nothing", c.text, c.cursor, got)
			}
		})
	}
}

func TestMayReturnRows(t *testing.T) {
	for _, c := range []struct {
		statement string
		want      bool
	}{
		{`INSERT INTO t VALUES (1) RETURNING i`, true},
		{`insert into t values (1) returning i`, true},
		{"INSERT INTO t VALUES (1)\nReturning\ni", true},
		{`DELETE FROM t RETURNING t.i`, true},
		{`INSERT INTO t VALUES (1) RETURNING *`, true},
		{`INSERT INTO t VALUES (1)`, false},

		{`INSERT INTO t VALUES ('returning')`, false},
		{`INSERT INTO t VALUES (1) -- returning`, false},
		{`/* returning */ INSERT INTO t VALUES (1)`, false},
		{`INSERT INTO "returning" VALUES (1)`, false},
		{`INSERT INTO returning_things VALUES (1)`, false},
		{`INSERT INTO not_returning VALUES (1)`, false},
		{`UPDATE r2 SET i = r2.returning`, false},
		{`DELETE FROM r2 WHERE r2.returning = 2`, false},
		{`INSERT INTO r2 SELECT 1 AS returning, 2`, false},
		{`UPDATE r2 SET returning.i = 1`, false},

		{`/* /* */ ' */ UPDATE c SET a = 1 RETURNING b AS "Count"`, true},
		{`/* /* */ */ UPDATE c SET a = 1`, false},
		{`/* /* returning */ */ UPDATE c SET a = 1`, false},
		{`/* a note */ UPDATE c SET a = 1`, false},
		{`/* one *//* two */ UPDATE c SET a = 1`, false},
	} {
		if got := MayReturnRows(c.statement, Dialect{NestedComments: true}); got != c.want {
			t.Errorf("MayReturnRows(%q) = %v, want %v", c.statement, got, c.want)
		}
	}
}

func TestIsBlank(t *testing.T) {
	for text, want := range map[string]bool{
		``:                 true,
		" \n\t":            true,
		`-- a`:             true,
		`/* a */ -- b`:     true,
		`;`:                true,
		` ; -- a`:          true,
		`; SELECT 1`:       false,
		`SELECT 1`:         false,
		`''`:               false,
		`/* a */ SELECT 1`: false,
	} {
		if got := IsBlank(text, Dialect{}); got != want {
			t.Errorf("IsBlank(%q) = %v, want %v", text, got, want)
		}
	}
}

func TestLeavesTransactionOpen(t *testing.T) {
	for _, tt := range []struct {
		text  string
		mysql bool
		want  bool
	}{
		{`BEGIN`, false, true},
		{`begin;`, false, true},
		{"-- a note\nBEGIN", false, true},
		{`BEGIN WORK`, true, true},
		{`BEGIN IMMEDIATE`, false, true},
		{`BEGIN ISOLATION LEVEL SERIALIZABLE`, false, true},
		{`START TRANSACTION`, false, true},
		{`start transaction read only`, true, true},
		{`COMMIT AND CHAIN`, false, true},
		{`ROLLBACK AND CHAIN`, false, true},
		{`COMMIT AND CHAIN NO RELEASE`, true, true},
		{`ROLLBACK AND CHAIN NO RELEASE`, true, true},
		{`XA START 'x'`, true, true},
		{`XA BEGIN 'x'`, true, true},
		{`BEGIN; UPDATE t SET a = 1`, false, true},
		{`BEGIN; COMMIT; BEGIN`, false, true},

		{``, false, false},
		{`SELECT 'BEGIN'`, false, false},
		{`-- BEGIN`, false, false},
		{`COMMIT`, false, false},
		{`ROLLBACK`, false, false},
		{`COMMIT AND NO CHAIN`, false, false},
		{`COMMIT AND NO CHAIN NO RELEASE`, true, false},
		{`XA RECOVER`, true, false},
		{`BEGIN; UPDATE t SET a = 1; COMMIT`, false, false},
		{`BEGIN; UPDATE t SET a = 1; END;`, false, false},
		{`BEGIN; UPDATE t SET a = 1; ROLLBACK`, false, false},
		{`BEGIN; UPDATE t SET a = 1; ABORT`, false, false},
		{`START REPLICA`, true, false},
		{`CREATE TRIGGER x AFTER UPDATE ON t BEGIN UPDATE u SET a = 1; END`, false, false},

		{`BEGIN NOT ATOMIC SELECT 1; END`, true, false},
		{`BEGIN; ROLLBACK TO s`, false, true},
		{`BEGIN; ROLLBACK WORK TO SAVEPOINT s`, true, true},
	} {
		if got := LeavesTransactionOpen(tt.text, dialectOf(tt.mysql)); got != tt.want {
			t.Errorf("LeavesTransactionOpen(%q, %v) = %v, want %v", tt.text, tt.mysql, got, tt.want)
		}
	}
}

func TestLastStatementVerb(t *testing.T) {
	for _, tt := range []struct {
		text  string
		mysql bool
		want  string
	}{
		{``, false, ""},
		{`-- only a note`, false, ""},
		{`update t set a = 1`, false, "UPDATE"},
		{"  /* note */ -- another\n Delete FROM t;", false, "DELETE"},
		{`UPDATE t SET a = 1; CREATE TABLE u (i INT)`, false, "CREATE"},
		{`CREATE TABLE u (i INT); INSERT INTO u VALUES (1); -- done`, false, "INSERT"},
		{`(SELECT 1)`, false, ""},
		{"# note\nUPDATE t SET a = 1", true, "UPDATE"},
		{"# note\nUPDATE t SET a = 1", false, ""},
		{`update_log`, false, "UPDATE_LOG"},
		{`WITH c AS (SELECT 1) UPDATE t SET a = 1`, false, "UPDATE"},
		{`with recursive c (i) as (select 1 union all select i + 1 from c) delete from t`, false, "DELETE"},
		{`WITH a AS (SELECT 1), b AS (SELECT 2) INSERT INTO t SELECT * FROM a`, false, "INSERT"},
		{`WITH c AS (SELECT 1 AS i) SELECT i INTO @a FROM c`, true, "SELECT"},
		{`WITH "update" AS (SELECT 1) SELECT * FROM "update"`, false, "SELECT"},
		{`WITH c AS (SELECT 1)`, false, ""},
		{`WITH`, false, ""},
	} {
		if got := LastStatementVerb(tt.text, dialectOf(tt.mysql)); got != tt.want {
			t.Errorf("LastStatementVerb(%q, %v) = %q, want %q", tt.text, tt.mysql, got, tt.want)
		}
	}
}
