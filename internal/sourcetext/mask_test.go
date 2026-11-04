package sourcetext_test

import (
	"strings"
	"testing"

	"github.com/EdwardWhalley/lode/internal/sourcetext"
)

func TestMaskDSN(t *testing.T) {
	const unparseable = "(unparseable DSN)"
	for _, tc := range []struct{ name, dsn, want string }{
		{"a password", "postgres://reader:hunter2@db.example.com/app", "postgres://reader:xxxxx@db.example.com/app"},
		{"the port stays", "mysql://u:hunter2@h:3306/shop", "mysql://u:xxxxx@h:3306/shop"},
		{"nothing to hide", "postgres://u@h/db", "postgres://u@h/db"},
		{"a harmless query goes too", "postgres://u@h/db?sslmode=disable", "postgres://u@h/db?…"},
		{"a password in the options", "postgres://127.0.0.1:1/db?user=u&password=hunter2", "postgres://127.0.0.1:1/db?…"},
		{"an option no list would name", "postgres://u@h/db?sslpassword=hunter2", "postgres://u@h/db?…"},
		{"a query with no name", "postgres://u@h/db?hunter2", "postgres://u@h/db?…"},
		{"both kinds of password", "postgres://u:hunter2@h/db?password=hunter2", "postgres://u:xxxxx@h/db?…"},
		{"a fragment is dropped", "postgres://u@h/db#hunter2", "postgres://u@h/db"},
		{"a query and a fragment", "postgres://u@h/db?a=b#hunter2", "postgres://u@h/db?…"},
		{"an empty query hides nothing", "postgres://u@h/db?", "postgres://u@h/db"},
		{"only a scheme", "postgres://", "postgres://"},
		{"only a scheme and a query", "postgres://?password=hunter2", "postgres://?…"},
		{"only a scheme and a fragment", "postgres://#hunter2", "postgres://"},
		{"an opaque DSN", "mysql:u:hunter2@tcp(127.0.0.1)/db", unparseable},
		{"one slash", "postgres:/hunter2", unparseable},
		{"no scheme at all", "hunter2@h/db", unparseable},
		{"no scheme, and :// only in the query", "hunter2@h?a://b", unparseable},
		{"a port that is no number", "postgres://u:hunter2@h:notaport/d", unparseable},
		{"a slash left in a password", "postgres://u:12345/hunter2@h/db", unparseable},
		{"an escaped @ in the path", "postgres://u@h/a%40hunter2", unparseable},
		{"a ; in the path", "postgres://u@h/a;password=hunter2", unparseable},
		{"an escaped ; in the path", "postgres://u@h/a%3Bpassword=hunter2", unparseable},
		{"a ; in the host", "sqlserver://h;password=hunter2", unparseable},
		{"a ; in the user", "sqlserver://u;password=hunter2@h/db", unparseable},
		{"an escaped ; in the user", "sqlserver://u%3Bpassword=hunter2@h/db", unparseable},
		{"an escaped ? in the path", "postgres://u@h/db%3Fpassword%3Dhunter2", unparseable},
		{"a ; in a password is masked with it", "postgres://u:pa;ss@h/db", "postgres://u:xxxxx@h/db"},
		{"a ; in a query is hidden with it", "postgres://u@h/db?a=1;b=2", "postgres://u@h/db?…"},
		{"a ; in a fragment is dropped with it", "postgres://u@h/db#a;b", "postgres://u@h/db"},
		{"a ; in the path and no =", "postgres://u@h/a;hunter2", unparseable},
		{"an escaped ; in the path and no =", "postgres://u@h/a%3Bhunter2", unparseable},
		{"an escaped ? in the path and no =", "postgres://u@h/db%3Fhunter2", unparseable},
		{"an option after an & in the path", "postgres://u@h/db&password=hunter2", unparseable},
		{"an escaped option in the path", "postgres://u@h/a%26password%3Dhunter2", unparseable},
		{"an option after an escaped # in the path", "postgres://u@h/db%23password%3Dhunter2", unparseable},
		{"an option after a comma in the path", "postgres://h/db,password=hunter2", unparseable},
		{"a = in a password is masked with it", "postgres://u:a=b@h/db", "postgres://u:xxxxx@h/db"},
		{"a = in a query is hidden with it", "postgres://u@h/db?a=b", "postgres://u@h/db?…"},
		{"a slash and a ? left in a password", "postgres://u:12345/hunter2?x@h/db", unparseable},
		{"a slash and a # left in a password", "postgres://u:12345/hunter2#x@h/db", unparseable},
		{"a ? left in a password", "postgres://u:12345?hunter2@h/db", unparseable},
		{"a # left in a password", "postgres://u:12345#hunter2@h/db", unparseable},
		{"an @ in an option cannot be told from those", "postgres://u@h/db?user=a@b", unparseable},
		{"an escaped @ in an option can", "postgres://u@h/db?user=a%40b", "postgres://u@h/db?…"},
		{"an escaped @ in a fragment can", "postgres://u@h/db#a%40b", "postgres://u@h/db"},
	} {
		if got := sourcetext.MaskDSN(tc.dsn); got != tc.want {
			t.Errorf("%s: MaskDSN(%q) = %q, want %q", tc.name, tc.dsn, got, tc.want)
		}
		if strings.Contains(sourcetext.MaskDSN(tc.dsn), "hunter2") {
			t.Errorf("%s: MaskDSN(%q) shows the secret", tc.name, tc.dsn)
		}
	}
}

// History masks what it loads every time, so what it wrote last time must come back the same,
// or a file would be rewritten on every start.
func TestMaskingAMaskedDSNChangesNothing(t *testing.T) {
	for _, dsn := range []string{
		"postgres://reader:hunter2@db.example.com/app",
		"postgres://u@h/db?sslmode=disable",
		"postgres://u:hunter2@h:5432/a%20b?password=hunter2#hunter2",
		"mysql://u%40x:p%40ss@h/db?a=b",
		"postgres://h",
		"file:///data/x",
		"postgres://",
		"postgres://?password=hunter2",
		"mysql:u:hunter2@tcp(127.0.0.1)/db",
	} {
		once := sourcetext.MaskDSN(dsn)
		if twice := sourcetext.MaskDSN(once); twice != once {
			t.Errorf("MaskDSN(%q) = %q, and masked again %q", dsn, once, twice)
		}
	}
}
