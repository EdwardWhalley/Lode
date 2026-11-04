package file

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func openForValues(t *testing.T) *Adapter {
	t.Helper()
	path := filepath.Join(t.TempDir(), "a.csv")
	if err := os.WriteFile(path, []byte("id\n1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	a, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { a.Close() })
	return a
}

// oneValue runs a query of one row and one column.
func oneValue(t *testing.T, a *Adapter, query string) any {
	t.Helper()
	rows, err := a.Query(context.Background(), query)
	if err != nil {
		t.Fatalf("%s: %v", query, err)
	}
	defer rows.Close()
	if !rows.Next() {
		t.Fatalf("%s: no row: %v", query, rows.Err())
	}
	return rows.Values()[0]
}

func TestAValueReadsAsDuckDBWritesIt(t *testing.T) {
	a := openForValues(t)
	for _, expr := range []string{
		`'6ba7b810-9dad-11d1-80b4-00c04fd430c8'::UUID`,
		`'00000000-0000-0000-0000-000000000000'::UUID`, // bounded: a literal, not a count of rows
		`'ffffffff-ffff-ffff-ffff-ffffffffffff'::UUID`,
		`100.00::DECIMAL(10,2)`,
		`12.50::DECIMAL(10,2)`,
		`(-0.05)::DECIMAL(10,3)`,
		`0::DECIMAL(10,2)`,
		`0.001::DECIMAL(10,3)`,
		`7::DECIMAL(10,0)`,
		`(-7)::DECIMAL(10,0)`,
		`123456789012345678901234567890.12::DECIMAL(38,2)`, // bounded: a literal, not a count of rows
		`TIME '18:30:05'`,
		`TIME '18:30:05.25'`,
		`TIME '00:00:00'`,
		`to_microseconds(-9223372036854775808)`, // bounded: a literal, not a count of rows
		`INTERVAL '14 months 2 days 3 hours'`,
		`INTERVAL '1 year'`,
		`INTERVAL '1 month'`,
		`INTERVAL '1 day'`,
		`INTERVAL '-3 hours'`,
		`INTERVAL '90 minutes 1.5 seconds'`,
		`INTERVAL '100 hours'`,
		`INTERVAL '0 seconds'`,
		`INTERVAL '1 microsecond'`,
		`INTERVAL '-14 months'`,
	} {
		want, ok := oneValue(t, a, "SELECT ("+expr+")::VARCHAR").(string)
		if !ok {
			t.Fatalf("%s: DuckDB's cast is not text", expr)
		}
		if got := fmt.Sprint(oneValue(t, a, "SELECT "+expr)); got != want {
			t.Errorf("%s reads %q, and DuckDB writes %q", expr, got, want)
		}
	}
}

func TestSixteenBytesOfABlobAreStillBinary(t *testing.T) {
	a := openForValues(t)
	const blob = `'\x6B\xA7\xB8\x10\x9D\xAD\x11\xD1\x80\xB4\x00\xC0\x4F\xD4\x30\xC8'::BLOB`
	for _, query := range []string{
		"SELECT " + blob,
		"SELECT [" + blob + "][1]",
	} {
		v, ok := oneValue(t, a, query).([]byte)
		if !ok || len(v) != 16 {
			t.Errorf("%s: got %#v, want its 16 bytes", query, v)
		}
	}
	both := oneValue(t, a, "SELECT {'b': "+blob+", 'u': '6ba7b810-9dad-11d1-80b4-00c04fd430c8'::UUID}")
	if _, ok := both.(map[string]any)["u"].([]byte); !ok {
		t.Errorf("got %#v, want the bytes left as they are", both)
	}
}

func TestATimestampInTheYearOneStaysATimestamp(t *testing.T) {
	a := openForValues(t)
	if got := fmt.Sprint(oneValue(t, a, "SELECT TIMESTAMP '0001-01-01 18:30:05'")); !strings.HasPrefix(got, "0001-01-01") {
		t.Errorf("reads %q", got)
	}
}

func TestATimeWithAZoneReadsAsTheUTCTheDriverKept(t *testing.T) {
	a := openForValues(t)
	for expr, want := range map[string]string{
		`TIMETZ '18:30:05+02'`: "16:30:05+00",
		`TIMETZ '01:00:00+05'`: "20:00:00+00",
		`TIMETZ '23:00:00-05'`: "04:00:00+00",
	} {
		if got := oneValue(t, a, "SELECT "+expr); got != want {
			t.Errorf("%s reads %q, want %q", expr, got, want)
		}
	}
}

func TestADecimalWithAllItsDigitsAfterThePointHasALeadingZero(t *testing.T) {
	a := openForValues(t)
	if got := fmt.Sprint(oneValue(t, a, "SELECT 0.1234::DECIMAL(4,4)")); got != "0.1234" {
		t.Errorf("reads %q", got)
	}
}

func TestAStructFieldNamedLikeATypeIsNotThatType(t *testing.T) {
	a := openForValues(t)
	const id = "6ba7b810-9dad-11d1-80b4-00c04fd430c8"
	for _, tt := range []struct {
		query, field, want string
	}{
		{"SELECT {'BLOB': '" + id + "'::UUID}", "BLOB", id},
		{"SELECT {'TIMETZ': TIME '01:02:03'}", "TIMETZ", "01:02:03"},
	} {
		s, ok := oneValue(t, a, tt.query).(map[string]any)
		if !ok {
			t.Fatalf("%s: not a struct", tt.query)
		}
		if got := fmt.Sprint(s[tt.field]); got != tt.want {
			t.Errorf("%s reads %q, want %q", tt.query, got, tt.want)
		}
	}
	s := oneValue(t, a, "SELECT {'TIME': DATE '0001-01-01'}").(map[string]any)
	if got := fmt.Sprint(s["TIME"]); !strings.HasPrefix(got, "0001-01-01") {
		t.Errorf("a date in a field called TIME reads %q", got)
	}
}

func TestAUnionReadsAsWhatItHolds(t *testing.T) {
	a := openForValues(t)
	if got := fmt.Sprint(oneValue(t, a, "SELECT 2.50::DECIMAL(5,2)::UNION(a DECIMAL(5,2), b VARCHAR)")); got != "2.50" {
		t.Errorf("reads %q", got)
	}
}

func TestAMapsKeysReadAsWhatTheyHold(t *testing.T) {
	a := openForValues(t)
	m, ok := oneValue(t, a, "SELECT MAP {DATE '2026-01-01': 1}").(map[string]any)
	if !ok {
		t.Fatal("not a map")
	}
	if _, ok := m["2026-01-01T00:00:00Z"]; !ok {
		t.Errorf("keys are %v", m)
	}
}

func TestWhatIsNestedReadsTheSameAsOnItsOwn(t *testing.T) {
	a := openForValues(t)
	v := oneValue(t, a, `SELECT [{
		'u': '6ba7b810-9dad-11d1-80b4-00c04fd430c8'::UUID,
		'd': 2.50::DECIMAL(5,2),
		't': TIME '01:02:03',
		'i': INTERVAL '1 day',
		'm': MAP {'k': 1.50::DECIMAL(5,2)}
	}]`)
	list, ok := v.([]any)
	if !ok || len(list) != 1 {
		t.Fatalf("got %#v", v)
	}
	s, ok := list[0].(map[string]any)
	if !ok {
		t.Fatalf("got %#v", list[0])
	}
	for field, want := range map[string]string{
		"u": "6ba7b810-9dad-11d1-80b4-00c04fd430c8",
		"d": "2.50",
		"t": "01:02:03",
		"i": "1 day",
		"m": "map[k:1.50]",
	} {
		if got := fmt.Sprint(s[field]); got != want {
			t.Errorf("%s reads %q, want %q", field, got, want)
		}
	}
}

func TestAResultWithNoColumnsIsLeftAlone(t *testing.T) {
	a := openForValues(t)
	for _, query := range []string{"CREATE TABLE t (i INT)", "INSERT INTO t VALUES (1), (2)"} {
		rows, err := a.Query(context.Background(), query)
		if err != nil {
			t.Fatalf("%s: %v", query, err)
		}
		if _, ok := rows.(plainRows); ok {
			t.Errorf("%s: a result with no columns was wrapped", query)
		}
		rows.Close()
	}
}
