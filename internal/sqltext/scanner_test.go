package sqltext

import "testing"

// dialectOf is the Dialect mysql.Kind sets when mysql is true, and the zero Dialect when it
// is not, for the tables written when the choice was one bool.
func dialectOf(mysql bool) Dialect {
	if mysql {
		return Dialect{BackslashEscapes: true, HashComments: true}
	}
	return Dialect{}
}

func TestInLiteralOrComment(t *testing.T) {
	cases := []struct {
		name   string
		before string
		mysql  bool
		want   bool
	}{
		{"open string", `SELECT 'abc`, false, true},
		{"closed string", `SELECT 'abc'`, false, false},
		{"doubled quote, closed", `SELECT 'it''s'`, false, false},
		{"doubled quote, still open", `SELECT 'it''s`, false, true},
		{"after a closed string", `SELECT 'x' || ta`, false, false},

		{"apostrophe in a double-quoted identifier", `SELECT "o'brien", ta`, false, false},
		{"apostrophe in a backticked identifier", "SELECT `o'brien`, ta", false, false},
		{"typing inside a quoted identifier is allowed", `SELECT "Ord`, false, false},
		{"doubled double quote", `SELECT "a""b", ta`, false, false},

		{"dollar quote closed", `SELECT $$it's$$, ta`, false, false},
		{"dollar quote open", `SELECT $$it's, ta`, false, true},
		{"tagged dollar quote closed", `SELECT $tag$ 'x $tag$ ta`, false, false},
		{"tagged dollar quote open", `SELECT $tag$ x`, false, true},
		{"a different tag does not close it", `SELECT $a$ x $b$ y`, false, true},
		{"a positional parameter is not a dollar quote", `SELECT $1, ta`, false, false},
		{"a dollar inside an identifier is not a dollar quote", `SELECT a$b$, ta`, false, false},

		{"line comment", `SELECT 1 -- note`, false, true},
		{"line comment ended", "SELECT 1 -- note\nta", false, false},
		{"block comment open", `SELECT 1 /* c`, false, true},
		{"block comment closed", `SELECT 1 /* c */ ta`, false, false},
		{"a quote inside a comment is not a string", "SELECT 1 -- it's\nta", false, false},
		{"a comment marker inside a string is not a comment", `SELECT '--', ta`, false, false},

		{"mysql escaped quote keeps the string open", `SELECT 'it\'s'`, true, false},
		{"mysql escaped quote, string not closed", `SELECT 'it\'`, true, true},
		{"mysql escaped backslash then close", `SELECT 'C:\\', ta`, true, false},
		{"mysql lone backslash escapes the closing quote", `SELECT 'C:\', ta`, true, true},
		{"standard SQL: a backslash ends nothing", `SELECT 'C:\', ta`, false, false},
		{"standard SQL: so backslash-quote closes the string", `SELECT 'it\'s', ta`, false, true},
	}
	for _, tc := range cases {
		if got := InLiteralOrComment(tc.before, dialectOf(tc.mysql)); got != tc.want {
			t.Errorf("%s: InLiteralOrComment(%q, mysql=%v) = %v, want %v", tc.name, tc.before, tc.mysql, got, tc.want)
		}
	}
}

func TestInLiteralOrCommentAtEachDepth(t *testing.T) {
	nested := Dialect{NestedComments: true}
	cases := []struct {
		name         string
		before       string
		nested, flat bool
	}{
		{"depth one, open", `SELECT /* a`, true, true},
		{"depth one, closed", `SELECT /* a */ ta`, false, false},
		{"depth two, both open", `SELECT /* a /* b`, true, true},
		{"depth two, inner closed", `SELECT /* a /* b */ ta`, true, false},
		{"depth two, both closed", `SELECT /* a /* b */ c */ ta`, false, false},
		{"depth three, all open", `SELECT /* a /* b /* c`, true, true},
		{"depth three, one closed", `SELECT /* a /* b /* c */ ta`, true, false},
		{"depth three, two closed", `SELECT /* a /* b /* c */ */ ta`, true, false},
		{"depth three, all closed", `SELECT /* a /* b /* c */ */ */ ta`, false, false},
		{"a quote in a comment opens no string, so */ closes it", `SELECT /* ' */ ta`, false, false},
		{"and the quote after it opens one", `SELECT /* /* ' */ */ ' ta`, true, true},
		{"/* in a line comment opens nothing", "SELECT 1 -- /* \nta", false, false},
		{"/* in a string opens nothing", `SELECT '/*', ta`, false, false},
		{"*/ before /* closes, so */* is no new comment", `SELECT /* a */* ta`, false, false},
		{"/*/ opens one comment and closes none", `SELECT /* /*/ */ ta`, true, false},
	}
	for _, tc := range cases {
		if got := InLiteralOrComment(tc.before, nested); got != tc.nested {
			t.Errorf("%s: InLiteralOrComment(%q) with NestedComments = %v, want %v", tc.name, tc.before, got, tc.nested)
		}
		if got := InLiteralOrComment(tc.before, Dialect{}); got != tc.flat {
			t.Errorf("%s: InLiteralOrComment(%q) without = %v, want %v", tc.name, tc.before, got, tc.flat)
		}
	}
}
