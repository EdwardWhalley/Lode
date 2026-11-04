package sqltext

import (
	"math/rand"
	"reflect"
	"strings"
	"testing"
)

type aliasMap = map[string][]string

func TestAliasesIn(t *testing.T) {
	cases := []struct {
		name  string
		sql   string
		mysql bool
		want  aliasMap
	}{
		{"plain alias", `SELECT * FROM orders o`, false, aliasMap{"o": {"orders"}}},
		{"AS", `SELECT * FROM orders AS o`, false, aliasMap{"o": {"orders"}}},
		{"schema-qualified", `SELECT * FROM sales.orders o`, false, aliasMap{"o": {"orders"}}},
		{"double-quoted name", `SELECT * FROM "my orders" m`, false, aliasMap{"m": {"my orders"}}},
		{"backticked name", "SELECT * FROM `my orders` m", false, aliasMap{"m": {"my orders"}}},
		{"quoted schema and name", `SELECT * FROM "Sales"."My Orders" m`, false, aliasMap{"m": {"My Orders"}}},
		{"a doubled quote in a name", `SELECT * FROM "a""b" x`, false, aliasMap{"x": {`a"b`}}},
		{"quoted alias", `SELECT * FROM orders "O"`, false, aliasMap{"o": {"orders"}}},
		{"join", `SELECT * FROM orders o JOIN customers c ON c.id = o.customer_id`, false,
			aliasMap{"o": {"orders"}, "c": {"customers"}}},
		{"left outer join", `SELECT * FROM orders o LEFT OUTER JOIN customers c ON 1=1`, false,
			aliasMap{"o": {"orders"}, "c": {"customers"}}},
		{"comma list", `SELECT * FROM a x, b y WHERE 1=1`, false, aliasMap{"x": {"a"}, "y": {"b"}}},
		{"comma list, one without an alias", `SELECT * FROM a, b y`, false, aliasMap{"y": {"b"}}},
		{"update", `UPDATE orders o SET total = 1`, false, aliasMap{"o": {"orders"}}},
		{"delete", `DELETE FROM orders o WHERE o.id = 1`, false, aliasMap{"o": {"orders"}}},
		{"lower case keywords", `select * from orders o join customers c on 1=1`, false,
			aliasMap{"o": {"orders"}, "c": {"customers"}}},
		{"mixed case keywords", `Select * From orders o Inner Join customers c On 1=1`, false,
			aliasMap{"o": {"orders"}, "c": {"customers"}}},
		{"the alias is kept in lower case", `SELECT * FROM orders Ord`, false, aliasMap{"ord": {"orders"}}},
		{"the table is kept as written", `SELECT * FROM Orders o`, false, aliasMap{"o": {"Orders"}}},
		{"over lines", "SELECT *\nFROM orders\n  o\nJOIN customers\n  AS c", false,
			aliasMap{"o": {"orders"}, "c": {"customers"}}},
		{"alias after the cursor's word", `SELECT o. FROM orders o`, false, aliasMap{"o": {"orders"}}},
		{"nested selects share", `SELECT * FROM (SELECT * FROM orders o) s JOIN t x ON 1=1`, false,
			aliasMap{"o": {"orders"}, "x": {"t"}}},
		{"a comment between the name and the alias", `SELECT * FROM orders /* the big one */ o`, false, aliasMap{"o": {"orders"}}},
		{"an underscore alias", `SELECT * FROM orders _o1`, false, aliasMap{"_o1": {"orders"}}},

		{"WHERE", `SELECT * FROM orders WHERE x = 1`, false, aliasMap{}},
		{"ORDER BY", `SELECT * FROM orders ORDER BY id`, false, aliasMap{}},
		{"LEFT JOIN", `SELECT * FROM orders LEFT JOIN customers ON 1=1`, false, aliasMap{}},
		{"semicolon", `SELECT * FROM orders;`, false, aliasMap{}},
		{"end of the text", `SELECT * FROM orders`, false, aliasMap{}},
		{"GROUP BY", `SELECT * FROM orders GROUP BY id`, false, aliasMap{}},
		{"UNION", `SELECT * FROM orders UNION SELECT * FROM customers`, false, aliasMap{}},
		{"USING", `SELECT * FROM a JOIN b USING (id)`, false, aliasMap{}},
		{"ON after a join", `SELECT * FROM a JOIN b ON a.id = b.id`, false, aliasMap{}},
		{"UPDATE SET", `UPDATE orders SET total = 1`, false, aliasMap{}},
		{"AS with a clause word", `SELECT * FROM orders AS WHERE`, false, aliasMap{}},
		{"AS at the end", `SELECT * FROM orders AS`, false, aliasMap{}},
		{"a function is not a table", `SELECT * FROM generate_series(1, 3) g`, false, aliasMap{}},
		{"EXTRACT FROM", `SELECT EXTRACT(year FROM created_at) y FROM orders`, false, aliasMap{}},
		{"a number is not a name", `SELECT SUBSTRING(s FROM 2 FOR 3) FROM orders`, false, aliasMap{}},
		{"FOR UPDATE OF", `SELECT * FROM orders o FOR UPDATE OF o`, false, aliasMap{"o": {"orders"}}},
		{"FOR UPDATE SKIP LOCKED", `SELECT * FROM orders FOR UPDATE SKIP LOCKED`, false, aliasMap{}},
		{"FOR NO KEY UPDATE", `SELECT * FROM orders FOR NO KEY UPDATE NOWAIT`, false, aliasMap{}},
		{"ON CONFLICT DO UPDATE", `INSERT INTO orders VALUES (1) ON CONFLICT DO UPDATE SET a = 1`, false, aliasMap{}},
		{"a subquery's alias", `SELECT * FROM (SELECT 1) s`, false, aliasMap{}},
		{"a subquery's alias, with AS", `SELECT * FROM (SELECT 1 FROM orders) AS s`, false, aliasMap{}},
		{"a subquery in a list", `SELECT * FROM a x, (SELECT 1) s`, false, aliasMap{"x": {"a"}}},
		{"nothing", ``, false, aliasMap{}},
		{"FROM at the end", `SELECT * FROM`, false, aliasMap{}},
		{"an unterminated quoted name", `SELECT * FROM "orders o`, false, aliasMap{}},
		{"a quote that only looks closed", `SELECT * FROM "a"" x`, false, aliasMap{}},

		{"in a string", `SELECT 'FROM orders o'`, false, aliasMap{}},
		{"in a dollar quote", `SELECT $$ FROM orders o $$`, false, aliasMap{}},
		{"in a tagged dollar quote", `SELECT $t$ FROM orders o $t$`, false, aliasMap{}},
		{"in a line comment", "SELECT 1 -- FROM orders o\n", false, aliasMap{}},
		{"in a block comment", `SELECT 1 /* FROM orders o */`, false, aliasMap{}},
		{"after a comment", "SELECT 1 -- FROM x y\nFROM orders o", false, aliasMap{"o": {"orders"}}},
		{"an alias in a quoted name", `SELECT * FROM "a FROM b c" d`, false, aliasMap{"d": {"a FROM b c"}}},
		{"backslash: one string", `SELECT 'it\'s FROM orders o' FROM t x`, true, aliasMap{"x": {"t"}}},
		{"no backslash: the string ends at the escaped quote", `SELECT 'it\'s FROM orders o' FROM t x`, false, aliasMap{"o": {"orders"}}},
		{"a string between name and alias breaks the pair", `SELECT * FROM orders 'x' o`, false, aliasMap{}},

		{"one alias, two tables", `SELECT * FROM (SELECT * FROM a x) p, (SELECT * FROM b x) q`, false, aliasMap{"x": {"a", "b"}}},
		{"one alias twice for one table", `SELECT * FROM orders o JOIN orders o ON 1=1`, false, aliasMap{"o": {"orders"}}},
		{"a table under two aliases", `SELECT * FROM orders a JOIN orders b ON 1=1`, false, aliasMap{"a": {"orders"}, "b": {"orders"}}},
	}
	for _, tc := range cases {
		got := Aliases(tc.sql, dialectOf(tc.mysql))
		if got == nil {
			t.Errorf("%s: Aliases returned a nil map", tc.name)
		}
		if !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%s: Aliases(%q, mysql=%v) = %v, want %v", tc.name, tc.sql, tc.mysql, got, tc.want)
		}
	}
}

func TestStatementAround(t *testing.T) {
	cases := []struct {
		name  string
		text  string
		mysql bool
		want  string
	}{
		{"one statement", `SELECT o.| FROM orders o`, false, `SELECT o. FROM orders o`},
		{"the statement before is cut off", `SELECT 1 FROM a x; SELECT o.| FROM orders o`, false, ` SELECT o. FROM orders o`},
		{"the statement after is cut off", `SELECT o.| FROM orders o; SELECT 1 FROM a x`, false, `SELECT o. FROM orders o`},
		{"both", `SELECT 1; SELECT o.| FROM orders o; SELECT 2`, false, ` SELECT o. FROM orders o`},
		{"a cursor just after a semicolon is in the next statement", `SELECT 1;| SELECT 2`, false, ` SELECT 2`},
		{"a cursor just before a semicolon is in this one", `SELECT 1|; SELECT 2`, false, `SELECT 1`},
		{"a semicolon in a string does not cut", `SELECT ';' FROM orders o WHERE o.|`, false, `SELECT ';' FROM orders o WHERE o.`},
		{"a semicolon in a dollar quote does not cut", `SELECT $$;$$ FROM orders o WHERE o.|`, false, `SELECT $$;$$ FROM orders o WHERE o.`},
		{"a semicolon in a comment does not cut", "SELECT 1 -- ;\nFROM orders o WHERE o.|", false, "SELECT 1 -- ;\nFROM orders o WHERE o."},
		{"a semicolon in a block comment does not cut", `SELECT /* ; */ 1 FROM orders o WHERE o.|`, false, `SELECT /* ; */ 1 FROM orders o WHERE o.`},
		{"a semicolon in a quoted name does not cut", `SELECT 1 FROM "a;b" x WHERE x.|`, false, `SELECT 1 FROM "a;b" x WHERE x.`},
		{"backslash: an escaped quote keeps the string open", `SELECT 'a\';' FROM t x WHERE x.|`, true, `SELECT 'a\';' FROM t x WHERE x.`},
		{"no backslash: the string ended, so the semicolon cuts", `SELECT 'a\';| FROM t x`, false, ` FROM t x`},
		{"empty", `|`, false, ``},
		{"the cursor at the very end", `SELECT 1; SELECT 2|`, false, ` SELECT 2`},
		{"text with a wide character", `SELECT 'é;' FROM ünï u WHERE u.|; SELECT 2`, false, `SELECT 'é;' FROM ünï u WHERE u.`},
	}
	for _, tc := range cases {
		cursor := strings.Index(tc.text, "|")
		text := strings.Replace(tc.text, "|", "", 1)
		if got := StatementAround(text, cursor, dialectOf(tc.mysql)); got != tc.want {
			t.Errorf("%s: StatementAround(%q, %d, mysql=%v) = %q, want %q", tc.name, text, cursor, tc.mysql, got, tc.want)
		}
	}
}

func TestStatementAroundClampsTheCursor(t *testing.T) {
	if got := StatementAround("SELECT 1; SELECT 2", 1000, Dialect{}); got != " SELECT 2" {
		t.Errorf("past the end: %q", got)
	}
	if got := StatementAround("SELECT 1; SELECT 2", -5, Dialect{}); got != "SELECT 1" {
		t.Errorf("before the start: %q", got)
	}
}

// aliasPieces are the fragments the property test builds text from.
var aliasPieces = []string{
	"SELECT", "FROM", "JOIN", "UPDATE", "LEFT", "OUTER", "ON", "WHERE", "AS", "SET", "USING", "GROUP", "BY",
	"LATERAL", "VALUES", "WITH", "FOR", "KEY", "orders", "o", "c", "x", "sales.orders", "t1",
	",", ";", "(", ")", ".", "=", " ", " ", " ", "\n",
	"'", "''", `'a b'`, `'it\'s'`, `"`, `""`, `"my orders"`, "`", "`q r`", "$$", "$t$", "$1", "$",
	"--", "-- c\n", "/*", "*/", "/* c */", `\`, "é", "世界",
}

func TestAliasesInNeverPanicsAndNeverReturnsAClauseWord(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	for n := 0; n < 20000; n++ {
		var b strings.Builder
		for k := rng.Intn(14); k >= 0; k-- {
			b.WriteString(aliasPieces[rng.Intn(len(aliasPieces))])
			if rng.Intn(3) > 0 {
				b.WriteByte(' ')
			}
		}
		text := b.String()
		d := dialectOf(rng.Intn(2) == 0)
		cursor := rng.Intn(len(text) + 2)

		for _, stmt := range []string{text, StatementAround(text, cursor, d)} {
			for alias, tables := range Aliases(stmt, d) {
				if clauseWords[alias] {
					t.Fatalf("Aliases(%q, %v) has the clause word %q", stmt, d, alias)
				}
				if alias != strings.ToLower(alias) || len(tables) == 0 {
					t.Fatalf("Aliases(%q, %v): bad entry %q: %v", stmt, d, alias, tables)
				}
				seen := map[string]bool{}
				for _, tb := range tables {
					if seen[tb] {
						t.Fatalf("Aliases(%q, %v): %q lists %q twice", stmt, d, alias, tb)
					}
					seen[tb] = true
				}
			}
		}
	}
}
