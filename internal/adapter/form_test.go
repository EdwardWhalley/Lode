package adapter_test

import (
	"math/rand"
	"reflect"
	"strings"
	"testing"

	"github.com/EdwardWhalley/lode/internal/adapter"
)

var (
	pg    = adapter.ServerForm(5432, []string{"postgres", "postgresql"})
	mysql = adapter.ServerForm(3306, []string{"mysql", "mariadb"})
)

func TestComposeWritesTheDSN(t *testing.T) {
	for _, c := range []struct {
		name   string
		form   adapter.Form
		values adapter.Values
		want   string
	}{
		{"everything", pg,
			adapter.Values{"host": "db.example.com", "port": "5432", "database": "app", "user": "reader", "password_var": "PROD_PASSWORD", "options": "sslmode=require&opt=1"},
			"postgres://reader:${PROD_PASSWORD}@db.example.com:5432/app?sslmode=require&opt=1"},
		{"MySQL writes mysql://", mysql,
			adapter.Values{"host": "db.example.com", "port": "3306", "database": "shop", "user": "reader", "password_var": "SHOP_PASSWORD", "options": "tls=skip-verify"},
			"mysql://reader:${SHOP_PASSWORD}@db.example.com:3306/shop?tls=skip-verify"},
		{"host only", pg, adapter.Values{"host": "localhost"}, "postgres://localhost"},
		{"no password", pg, adapter.Values{"host": "h", "user": "u", "database": "d"}, "postgres://u@h/d"},
		{"no user", pg, adapter.Values{"host": "h", "password_var": "PW"}, "postgres://:${PW}@h"},
		{"user is escaped", pg, adapter.Values{"host": "h", "user": "me@work:1/x y", "password_var": "PW"}, "postgres://me%40work%3A1%2Fx%20y:${PW}@h"},
		{"user keeps ${VAR}", pg, adapter.Values{"host": "h", "user": "${DB_USER}", "password_var": "PW"}, "postgres://${DB_USER}:${PW}@h"},
		{"host keeps ${VAR}", pg, adapter.Values{"host": "${DB_HOST}", "user": "u"}, "postgres://u@${DB_HOST}"},
		{"IPv6 host", pg, adapter.Values{"host": "[::1]", "port": "5432", "database": "d"}, "postgres://[::1]:5432/d"},
		{"database is escaped", pg, adapter.Values{"host": "h", "database": "my db"}, "postgres://h/my%20db"},
		{"options verbatim", pg, adapter.Values{"host": "h", "options": "a=b%20c&d=${E}"}, "postgres://h?a=b%20c&d=${E}"},
	} {
		got, err := c.form.Compose(c.values, "")
		if err != nil || got != c.want {
			t.Errorf("%s: Compose = %q, %v; want %q", c.name, got, err, c.want)
		}
	}
}

func TestParseSplitsTheDSNIntoFields(t *testing.T) {
	for _, c := range []struct {
		name string
		form adapter.Form
		dsn  string
		want adapter.Values
	}{
		{"everything", pg, "postgres://reader:${PROD_PASSWORD}@db.example.com:5432/app?sslmode=require&opt=1",
			adapter.Values{"host": "db.example.com", "port": "5432", "database": "app", "user": "reader", "password_var": "PROD_PASSWORD", "options": "sslmode=require&opt=1"}},
		{"README MySQL", mysql, "mysql://reader:${SHOP_PASSWORD}@db.example.com:3306/shop?tls=skip-verify",
			adapter.Values{"host": "db.example.com", "port": "3306", "database": "shop", "user": "reader", "password_var": "SHOP_PASSWORD", "options": "tls=skip-verify"}},
		{"no port is not the default port", pg, "postgres://reader:${PW}@db.example.com/app",
			adapter.Values{"host": "db.example.com", "database": "app", "user": "reader", "password_var": "PW"}},
		{"mariadb scheme", mysql, "mariadb://u@h", adapter.Values{"host": "h", "user": "u"}},
		{"user is unescaped", pg, "postgres://me%40work%3A1%2Fx%20y:${PW}@h", adapter.Values{"host": "h", "user": "me@work:1/x y", "password_var": "PW"}},
		{"${VAR} in user and host", pg, "postgres://${DB_USER}:${PW}@${DB_HOST}:5432", adapter.Values{"host": "${DB_HOST}", "port": "5432", "user": "${DB_USER}", "password_var": "PW"}},
		{"IPv6 host", pg, "postgres://[::1]:5432/d", adapter.Values{"host": "[::1]", "port": "5432", "database": "d"}},
		{"IPv6 host without port", pg, "postgres://[2001:db8::7]/d", adapter.Values{"host": "[2001:db8::7]", "database": "d"}},
		{"database is unescaped", pg, "postgres://h/my%20db", adapter.Values{"host": "h", "database": "my db"}},
		{"any query lives in options", mysql, "mysql://h?parseTime=true&sql_mode=ANSI&tls=custom", adapter.Values{"host": "h", "options": "parseTime=true&sql_mode=ANSI&tls=custom"}},
	} {
		got, literal, err := c.form.Parse(c.dsn)
		if err != nil || literal {
			t.Errorf("%s: Parse = %v, literal %v, err %v", c.name, got, literal, err)
			continue
		}
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: values = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestALiteralPasswordIsReportedNeverReturnedAndKeptOnEdit(t *testing.T) {
	for _, c := range []struct{ dsn, password string }{
		{"postgres://reader:hunter2@db.example.com:5432/app?sslmode=require", "hunter2"},
		{"postgres://reader:p%40ss%2Fw%3Frd@h", "p%40ss%2Fw%3Frd"},
		{"postgres://reader:pa$$w{rd}@h", "pa$$w{rd}"},
		{"postgres://reader:${PW}x@h", "${PW}x"},
		{"postgresql://reader:hunter2@h", "hunter2"},
	} {
		dsn := c.dsn
		values, literal, err := pg.Parse(dsn)
		if err != nil || !literal {
			t.Errorf("Parse(%q) = %v, literal %v, err %v; want a literal password", dsn, values, literal, err)
			continue
		}
		if _, has := values["password_var"]; has {
			t.Errorf("Parse(%q): a literal password was returned as a password variable", dsn)
		}
		for k, v := range values {
			if strings.Contains(v, c.password) {
				t.Errorf("Parse(%q): the password came back in %s = %q", dsn, k, v)
			}
		}
		if got, err := pg.Compose(values, dsn); err != nil || got != dsn {
			t.Errorf("Compose(Parse(%q), keep) = %q, %v", dsn, got, err)
		}
	}

	const dsn = "postgres://reader:hunter2@h/app"
	values, _, _ := pg.Parse(dsn)

	values["user"] = "writer"
	if got, err := pg.Compose(values, dsn); err != nil || got != "postgres://writer:hunter2@h/app" {
		t.Errorf("a changed user: Compose = %q, %v; want the password kept", got, err)
	}

	values["password_var"] = "NEW_PW"
	if got, err := pg.Compose(values, dsn); err != nil || got != "postgres://writer:${NEW_PW}@h/app" {
		t.Errorf("a password variable replaces the literal: Compose = %q, %v", got, err)
	}

	if got, err := pg.Compose(adapter.Values{"host": "h", "user": "u"}, ""); err != nil || got != "postgres://u@h" {
		t.Errorf("nothing to keep: Compose = %q, %v", got, err)
	}

	if got, err := pg.Compose(adapter.Values{"host": "h", "user": "u"}, "mysql://x:hunter2@y"); err != nil || strings.Contains(got, "hunter2") {
		t.Errorf("a foreign keep: Compose = %q, %v", got, err)
	}
}

func TestParseThenComposeGivesBackTheLocation(t *testing.T) {
	for _, c := range []struct {
		form adapter.Form
		dsn  string
	}{
		{pg, "postgres://reader:${PROD_PASSWORD}@db.example.com/app"},
		{mysql, "mysql://reader:${SHOP_PASSWORD}@db.example.com:3306/shop?tls=skip-verify"},
		{mysql, "mariadb://reader:${SHOP_PASSWORD}@db.example.com:3306/shop"},
		{pg, "postgresql://u@h"},
		{pg, "POSTGRES://u@h"},
		{pg, "postgres://localhost"},
		{pg, "postgres://u:${PW}@[::1]:5432/d?a=1&b=2"},
		{pg, "postgres://me%40work%3A1%2Fx%20y:${PW}@h/my%20db"},
		{pg, "postgres://${DB_USER}:${PW}@${DB_HOST}"},
		{pg, "postgres://u@h?options=-c%20search_path%3Dapp&application_name=lode"},
		{pg, "postgres://u@h?x=a?b#c"},
		{pg, "postgres://:${PW}@h"},
		{pg, "postgres://reader:hunter2@h:5432/app?sslmode=require"},
	} {
		values, _, err := c.form.Parse(c.dsn)
		if err != nil {
			t.Errorf("Parse(%q): %v", c.dsn, err)
			continue
		}
		if got, err := c.form.Compose(values, c.dsn); err != nil || got != c.dsn {
			t.Errorf("Compose(Parse(%q)) = %q, %v", c.dsn, got, err)
		}
	}
}

func TestComposedLocationsParseBackToTheSameFields(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	pick := func(from ...string) string { return from[rng.Intn(len(from))] }
	text := func(alphabet []string, max int) string {
		var b strings.Builder
		for i, n := 0, rng.Intn(max+1); i < n; i++ {
			b.WriteString(pick(alphabet...))
		}
		return b.String()
	}
	odd := []string{"a", "Z", "7", " ", "@", ":", "/", "?", "#", "%", "%41", "+", "&", "=", "é", "世", "$", "{", "}", "${V}", "[", "]", "."}
	hostChars := []string{"a", "b", "1", "-", ".", "_", "${H}"}
	optionChars := []string{"a", "=", "&", "%20", "?", "#", "${E}", "-", "1", "/", ":", "@"}
	envChars := []string{"A", "z", "_", "9"}

	for _, f := range []adapter.Form{pg, mysql} {
		for i := 0; i < 2000; i++ {
			values := adapter.Values{
				"host":     pick("[::1]", "[2001:db8::7]", "h"+text(hostChars, 8)),
				"user":     text(odd, 6),
				"database": text(odd, 6),
				"options":  text(optionChars, 8),
			}
			if rng.Intn(2) == 0 {
				values["port"] = pick("1", "5432", "65535")
			}
			if rng.Intn(2) == 0 {
				values["password_var"] = "V" + text(envChars, 6)
			}
			for k, v := range values {
				if v == "" {
					delete(values, k)
				}
			}

			dsn, err := f.Compose(values, "")
			if err != nil {
				t.Fatalf("Compose(%v): %v", values, err)
			}
			got, literal, err := f.Parse(dsn)
			if err != nil || literal || !reflect.DeepEqual(got, values) {
				t.Fatalf("Parse(%q) = %v, literal %v, err %v; want %v", dsn, got, literal, err, values)
			}
			if again, err := f.Compose(got, dsn); err != nil || again != dsn {
				t.Fatalf("Compose(Parse(%q)) = %q, %v", dsn, again, err)
			}
		}
	}
}

func TestParseSaysWhenALocationItCannotPlaceMayHoldAPassword(t *testing.T) {
	for _, c := range []struct {
		dsn     string
		mayHold bool
	}{
		{"postgres://u:hunter2@h:notaport/d", true},
		{"postgres://u:p/ss@h/db", true},
		{"postgres://ü:pä@hôst/dé", true},
		{"postgres://u:hunter2@h/a/b", true},
		{"sqlserver://u:hunter2@h/d", true},
		{"postgres://h/a/b", false},
		{"not a url", false},
	} {
		if _, literal, err := pg.Parse(c.dsn); err == nil || literal != c.mayHold {
			t.Errorf("Parse(%q): literal %v, err %v; want an error and literal %v", c.dsn, literal, err, c.mayHold)
		}
	}
}

func TestParseReportsALocationItCannotPlaceRatherThanGuessing(t *testing.T) {
	for _, dsn := range []string{
		"sqlserver://u@h/d",
		"mysql://u@h/d",
		"not a url",
		"",
		"postgres://",
		"postgres://u:hunter2@h:notaport/d",
		"postgres://u:hunter2@h:/d",
		"postgres://u:hunter2@h:99999/d",
		"postgres://u:hunter2@h/a/b",
		"postgres://u:hunter2@h/",
		"postgres://u:hunter2@h?",
		"postgres://u:hunter2@h/%zz",
		"postgres://u%zz:hunter2@h",
		"postgres://u:hunter2@[::1/d",
		"postgres://u:hunter2@[::1]x/d",
		"postgres://u:hunter2@::1/d",
	} {
		values, _, err := pg.Parse(dsn)
		if err == nil {
			t.Errorf("Parse(%q) = %v, want an error", dsn, values)
		} else if strings.Contains(err.Error(), "hunter2") {
			t.Errorf("Parse(%q): error repeats the password: %v", dsn, err)
		}
	}
}

func TestComposeRejectsWhatCannotBeAField(t *testing.T) {
	const secret = "hunter2"
	for _, c := range []struct {
		name   string
		values adapter.Values
	}{
		{"no host", adapter.Values{"user": "u"}},
		{"password with a symbol", adapter.Values{"host": "h", "password_var": "p@ss-" + secret}},
		{"variable starting with a digit", adapter.Values{"host": "h", "password_var": "1" + secret}},
		{"variable in braces", adapter.Values{"host": "h", "password_var": "${PW}"}},
		{"port not a number", adapter.Values{"host": "h", "port": "54x32"}},
		{"port too big", adapter.Values{"host": "h", "port": "65536"}},
		{"port zero", adapter.Values{"host": "h", "port": "0"}},
		{"DSN pasted in the host", adapter.Values{"host": "postgres://u:" + secret + "@h/d"}},
		{"port with a sign", adapter.Values{"host": "h", "port": "+5432"}},
		{"port with a space", adapter.Values{"host": "h", "port": "54 32"}},
		{"port of too many digits for a number", adapter.Values{"host": "h", "port": strings.Repeat("9", 40)}},
		{"port with a minus sign", adapter.Values{"host": "h", "port": "-5432"}},
		{"options with a line break", adapter.Values{"host": "h", "options": "a=b\nc=d"}},
		{"options with a control character", adapter.Values{"host": "h", "options": "a=b\x1b[2J"}},
		{"stray bracket after an IPv6 host", adapter.Values{"host": "[::1]x]"}},
		{"two bracketed hosts", adapter.Values{"host": "[::1][::2]"}},
		{"IPv6 host without brackets", adapter.Values{"host": "::1"}},
		{"unclosed bracket", adapter.Values{"host": "[::1"}},
		{"host with a space", adapter.Values{"host": "my " + secret}},
	} {
		got, err := pg.Compose(c.values, "")
		if err == nil {
			t.Errorf("%s: composed %q, want an error", c.name, got)
			continue
		}
		if strings.Contains(err.Error(), secret) {
			t.Errorf("%s: error repeats the value: %v", c.name, err)
		}
	}
}

func TestPathFormRoundTripsAPathWhateverItHolds(t *testing.T) {
	form := adapter.PathForm()

	if len(form.Fields) != 1 || form.Fields[0].Key != "path" || !form.Fields[0].Required || form.Fields[0].Type != adapter.Text {
		t.Fatalf("fields = %+v, want one required text field %q", form.Fields, "path")
	}

	for _, path := range []string{
		"data.csv",
		"~/notes/my data.csv",
		`C:\Users\me\My Documents\shop.db`,
		"${DATA_DIR}/x.csv",
		" edge spaces.csv ",
	} {
		values, literal, err := form.Parse(path)
		if err != nil || literal {
			t.Errorf("Parse(%q) = %v, literal %v, err %v", path, values, literal, err)
			continue
		}
		if values["path"] != path {
			t.Errorf("Parse(%q): path = %q", path, values["path"])
		}
		got, err := form.Compose(values, "")
		if err != nil || got != path {
			t.Errorf("Compose(Parse(%q)) = %q, %v", path, got, err)
		}
	}

	if got, err := form.Compose(adapter.Values{"path": "  "}, ""); err == nil {
		t.Errorf("a blank path composed to %q, want an error", got)
	}
}

func TestPathFormParseRefusesWhatComposeWouldRefuse(t *testing.T) {
	form := adapter.PathForm()
	for _, location := range []string{"", " ", "\t "} {
		if values, _, err := form.Parse(location); err == nil {
			t.Errorf("Parse(%q) = %v, want an error: it cannot be composed back", location, values)
		}
	}
}

func TestComposeRefusesControlCharactersInTheHost(t *testing.T) {
	for _, host := range []string{"h\x1bx", "h\x00", "h\x7f", "\x1b[31mh", "h\u0085x"} {
		if got, err := pg.Compose(adapter.Values{"host": host}, ""); err == nil {
			t.Errorf("host %q composed to %q, want an error", host, got)
		}
	}
}

func TestAnEmptyPasswordIsNotReportedAsALiteralPassword(t *testing.T) {
	const location = "mysql://u:@h"
	values, literal, err := mysql.Parse(location)
	if err != nil {
		t.Fatal(err)
	}
	if literal {
		t.Errorf("Parse(%q) reports a literal password, but there is none", location)
	}
	if got, err := mysql.Compose(values, location); err != nil || got != location {
		t.Errorf("Compose(Parse(%q)) = %q, %v; the round trip must still hold", location, got, err)
	}
	if _, literal, _ := mysql.Parse("mysql://u:real@h"); !literal {
		t.Error("a real literal password is no longer reported")
	}
}

func TestAPasswordInTheOptionsIsNotShownInAField(t *testing.T) {
	const secret = "hunter2"
	for _, location := range []string{
		"postgres://u:${P}@h/d?password=" + secret,
		"postgres://u@h/d?sslmode=disable&Password=" + secret,
		"postgres://u@h/d?passwd=" + secret,
		"mysql://u@h/d?tls=skip-verify;pwd=" + secret,
		"postgres://u@h/d?pass%77ord=" + secret,
	} {
		form := pg
		if strings.HasPrefix(location, "mysql") {
			form = mysql
		}
		values, literal, err := form.Parse(location)
		if err == nil {
			t.Errorf("Parse(%q) placed it in fields: %v", location, values)
		}
		if !literal {
			t.Errorf("Parse(%q): literalPassword is false, so the raw text would not be masked", location)
		}
		if err != nil && strings.Contains(err.Error(), secret) {
			t.Errorf("error repeats the password: %v", err)
		}
	}
	for _, location := range []string{"postgres://u@h/d?sslmode=disable", "postgres://u@h/d?application_name=password_check"} {
		if _, _, err := pg.Parse(location); err != nil {
			t.Errorf("Parse(%q) = %v, want it placed", location, err)
		}
	}
}

func TestComposeRefusesAPasswordInTheOptionsWithoutRepeatingIt(t *testing.T) {
	for _, forms := range []adapter.Form{pg, mysql} {
		for _, options := range []string{"password=hunter2", "sslmode=disable&PWD=hunter2", "x=1;passwd=hunter2", "%70assword=hunter2"} {
			_, err := forms.Compose(adapter.Values{"host": "h", "options": options}, "")
			if err == nil || strings.Contains(err.Error(), "hunter2") {
				t.Errorf("options %q: err = %v, want a refusal without the value", options, err)
			}
		}
	}
	if _, err := pg.Compose(adapter.Values{"host": "h", "options": "sslmode=disable"}, ""); err != nil {
		t.Errorf("ordinary options were refused: %v", err)
	}
}
