package file

import (
	"strconv"
	"strings"
	"testing"
)

const wrapPrefix = "SELECT * FROM (\n"

func TestLimitQueryWrapsPlainQueries(t *testing.T) {
	cases := []struct{ name, in, body string }{
		{"select", "SELECT 1", "SELECT 1"},
		{"lower case", "select 1", "select 1"},
		{"mixed case", "SeLeCt 1", "SeLeCt 1"},
		{"trailing semicolon dropped", "SELECT 1;", "SELECT 1"},
		{"several trailing semicolons", "SELECT 1 ; ;", "SELECT 1"},
		{"surrounding whitespace", "  \n SELECT 1 \n ", "  \n SELECT 1"},
		{"trailing line comment dropped", "SELECT 1 -- note", "SELECT 1"},
		{"trailing block comment dropped", "SELECT 1 /* note */", "SELECT 1"},
		{"leading line comment kept", "-- hi\nSELECT 1", "-- hi\nSELECT 1"},
		{"leading block comment kept", "/* hi */ SELECT 1", "/* hi */ SELECT 1"},
		{"semicolon inside a string", "SELECT 'a;b'", "SELECT 'a;b'"},
		{"escaped quote in a string", "SELECT 'it''s; fine'", "SELECT 'it''s; fine'"},
		{"semicolon inside a quoted identifier", `SELECT "we;ird" FROM t`, `SELECT "we;ird" FROM t`},
		{"semicolon inside a line comment", "SELECT 1 -- a;b\n, 2", "SELECT 1 -- a;b\n, 2"},
		{"semicolon inside a block comment", "SELECT 1 /* ; */, 2", "SELECT 1 /* ; */, 2"},
		{"semicolon inside dollar quotes", "SELECT $$a;b$$", "SELECT $$a;b$$"},
		{"tagged dollar quotes", "SELECT $q$a;b$q$", "SELECT $q$a;b$q$"},
		{"positional parameter is not a quote", "SELECT $1", "SELECT $1"},
		{"leading FROM", "FROM t", "FROM t"},
		{"VALUES", "VALUES (1), (2)", "VALUES (1), (2)"},
		{"TABLE", "TABLE t", "TABLE t"},
		{"parenthesised query", "(SELECT 1) UNION ALL (SELECT 2)", "(SELECT 1) UNION ALL (SELECT 2)"},
		{"WITH select", "WITH x AS (SELECT 1) SELECT * FROM x", "WITH x AS (SELECT 1) SELECT * FROM x"},
		{"WITH several CTEs", "with a as (select 1), b as (select 2) select * from a, b", "with a as (select 1), b as (select 2) select * from a, b"},
		{"WITH RECURSIVE", "WITH RECURSIVE r(n) AS (SELECT 1 UNION ALL SELECT n+1 FROM r WHERE n < 5) SELECT * FROM r", "WITH RECURSIVE r(n) AS (SELECT 1 UNION ALL SELECT n+1 FROM r WHERE n < 5) SELECT * FROM r"},
		{"WITH a nested INSERT-looking word in a CTE body", "WITH x AS (SELECT 'insert' AS w) SELECT * FROM x", "WITH x AS (SELECT 'insert' AS w) SELECT * FROM x"},
		{"a blank piece before it dropped", "/* c */ ; SELECT 1", " SELECT 1"},
		{"a name with dollars in it is no dollar quote", "SELECT a$b$c FROM t", "SELECT a$b$c FROM t"},
		{"a nested comment after it dropped", "SELECT 1 /* /* */ x -- */", "SELECT 1"},
		{"semicolon inside a nested comment", "SELECT 1 /* /* */ ; */, 2", "SELECT 1 /* /* */ ; */, 2"},
		{"semicolon inside an E string", `SELECT E'it\'s; fine'`, `SELECT E'it\'s; fine'`},
		{"user LIMIT and ORDER BY", "SELECT i FROM t ORDER BY i DESC LIMIT 5 OFFSET 2", "SELECT i FROM t ORDER BY i DESC LIMIT 5 OFFSET 2"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			want := wrapPrefix + tc.body + "\n) LIMIT 7"
			if got := limitQuery(tc.in, 7); got != want {
				t.Errorf("limitQuery(%q, 7)\n got  %q\n want %q", tc.in, got, want)
			}
		})
	}
}

func TestLimitQueryLeavesEverythingElseAlone(t *testing.T) {
	for _, in := range []string{
		"DESCRIBE t", "SHOW TABLES", "PRAGMA version", "SET memory_limit='1GB'", "USE x",
		"CREATE TABLE a AS SELECT 1", "INSERT INTO t VALUES (1)", "UPDATE t SET i = 1",
		"DELETE FROM t", "DROP TABLE t", "EXPLAIN SELECT 1", "COPY (SELECT 1) TO 'x.csv'",
		"CALL pragma_version()",
		"WITH x AS (SELECT 1) INSERT INTO t SELECT * FROM x",
		"with x as (select 1) delete from t where i in (select * from x)",
		"SELECT 1; SELECT 2",
		"SELECT 1;\nDROP TABLE t",
		"SELECT 1; -- then a comment\nSELECT 2",
		"", "   ", "-- just a comment", "/* nothing */", ";",
		"SELECT 'unterminated", `SELECT "unterminated`, "SELECT 1 /* unterminated", "SELECT $$unterminated",
		"SELECT 1 /* /* */ still a comment", "SELECT 1; /* /* */ */ SELECT 2",
		"SELECTED 1", "FROMAGE", "tables", "withdraw",
	} {
		if got := limitQuery(in, 7); got != in {
			t.Errorf("limitQuery(%q, 7) = %q, want it unchanged", in, got)
		}
	}
}

func TestLimitQueryKeepsMultibyteText(t *testing.T) {
	got := limitQuery("SELECT 'héllo; wörld 🙂' AS é -- ünï", 3)
	want := wrapPrefix + "SELECT 'héllo; wörld 🙂' AS é\n) LIMIT 3"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestLimitQueryUsesTheGivenLimit(t *testing.T) {
	for _, n := range []int{1, 10, 10001, 1000001} {
		got := limitQuery("SELECT 1", n)
		if !strings.HasSuffix(got, "LIMIT "+strconv.Itoa(n)) {
			t.Errorf("limit %d: got %q", n, got)
		}
	}
}
