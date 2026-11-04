package export_test

import (
	"encoding/csv"
	"encoding/json"
	"errors"
	"math"
	"math/big"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/EdwardWhalley/lode/internal/export"
)

func write(t *testing.T, name string, cols []string, rows [][]any) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	target, err := export.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := target.Write(cols, rows); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func readCSV(t *testing.T, s string) [][]string {
	t.Helper()
	recs, err := csv.NewReader(strings.NewReader(s)).ReadAll()
	if err != nil {
		t.Fatalf("not valid CSV: %v\n%s", err, s)
	}
	return recs
}

// bounded: a timestamp literal, not a query.
var when = time.Date(2026, 10, 26, 13, 4, 5, 123456789, time.UTC)

func TestCSVRoundTripKeepsAwkwardTextIntact(t *testing.T) {
	cols := []string{"id", "note"}
	notes := []string{"plain", `has, comma`, `has "quotes"`, "two\nlines", " padded ", "", "ünïcode ✓", "NULL"}
	var rows [][]any
	for i, n := range notes {
		rows = append(rows, []any{int64(i), n})
	}
	recs := readCSV(t, write(t, "r.csv", cols, rows))
	if len(recs) != len(notes)+1 || recs[0][0] != "id" || recs[0][1] != "note" {
		t.Fatalf("records = %q", recs)
	}
	for i, n := range notes {
		if got := recs[i+1][1]; got != n {
			t.Errorf("row %d note = %q, want %q", i, got, n)
		}
	}
}

func TestNullAndTheTextNULLAreDifferentInBothFormats(t *testing.T) {
	cols := []string{"v"}
	rows := [][]any{{nil}, {"NULL"}, {""}}

	recs := readCSV(t, write(t, "n.csv", cols, rows))
	if recs[1][0] != "" || recs[2][0] != "NULL" || recs[3][0] != "" {
		t.Errorf("csv = %q", recs)
	}
	var objs []map[string]any
	if err := json.Unmarshal([]byte(write(t, "n.json", cols, rows)), &objs); err != nil {
		t.Fatal(err)
	}
	if objs[0]["v"] != nil || objs[1]["v"] != "NULL" || objs[2]["v"] != "" {
		t.Errorf("json = %v", objs)
	}
}

func TestJSONKeepsTypesColumnOrderAndRFC3339(t *testing.T) {
	cols := []string{"zeta", "alpha", "mid"}
	rows := [][]any{{
		// bounded: a literal one past 2^53, to check precision; not a query.
		int64(9007199254740993), 1.5, true,
	}, {
		when, []byte("text bytes"), []byte{0x00, 0xff, 0x10},
	}, {
		big.NewInt(0).Lsh(big.NewInt(1), 100), math.NaN(), math.Inf(-1),
	}}
	out := write(t, "t.json", cols, rows)

	if i, j, k := strings.Index(out, `"zeta"`), strings.Index(out, `"alpha"`), strings.Index(out, `"mid"`); !(i < j && j < k) {
		t.Errorf("columns are not in query order:\n%s", out)
	}
	var objs []map[string]any
	dec := json.NewDecoder(strings.NewReader(out))
	dec.UseNumber()
	if err := dec.Decode(&objs); err != nil {
		t.Fatalf("not valid JSON: %v\n%s", err, out)
	}
	// bounded: the same literal as above; not a query.
	if got := objs[0]["zeta"].(json.Number).String(); got != "9007199254740993" {
		t.Errorf("a large integer lost precision: %s", got)
	}
	if objs[0]["alpha"].(json.Number).String() != "1.5" || objs[0]["mid"] != true {
		t.Errorf("row 0 = %v", objs[0])
	}
	if objs[1]["zeta"] != "2026-10-26T13:04:05.123456789Z" {
		t.Errorf("timestamp = %v, want RFC 3339", objs[1]["zeta"])
	}
	if objs[1]["alpha"] != "text bytes" {
		t.Errorf("text delivered as bytes = %v, want the text", objs[1]["alpha"])
	}
	if objs[1]["mid"] != "AP8Q" {
		t.Errorf("binary = %v, want base64", objs[1]["mid"])
	}
	// bounded: 2^100 written out, to check a big integer; not a query.
	if got := objs[2]["zeta"].(json.Number).String(); got != "1267650600228229401496703205376" {
		t.Errorf("big integer = %s", got)
	}
	if objs[2]["alpha"] != "NaN" || objs[2]["mid"] != "-Inf" {
		t.Errorf("NaN and Inf cannot be JSON numbers, want strings: %v", objs[2])
	}
}

func TestCSVTimestampsAndBinary(t *testing.T) {
	recs := readCSV(t, write(t, "t.csv", []string{"ts", "bin", "txt", "f"},
		[][]any{{when, []byte{0x00, 0xff, 0x10}, []byte("line1\nline2"), 2.5}}))
	if recs[1][0] != "2026-10-26T13:04:05.123456789Z" {
		t.Errorf("timestamp = %q", recs[1][0])
	}
	if recs[1][1] != `\x00ff10` {
		t.Errorf("binary = %q, want hex", recs[1][1])
	}
	if recs[1][2] != "line1\nline2" {
		t.Errorf("text delivered as bytes = %q, want the text with its newline", recs[1][2])
	}
	if recs[1][3] != "2.5" {
		t.Errorf("float = %q", recs[1][3])
	}
}

func TestBytesWithControlCharactersAreHexAndTextStaysText(t *testing.T) {
	cases := []struct {
		name  string
		value []byte
		want  string
	}{
		{"a NUL", []byte{0x00, 0x01}, `\x0001`},
		{"an escape sequence", []byte("\x1b[31m"), `\x1b5b33316d`},
		{"line breaks and a tab", []byte("a\tb\r\nc\nd"), "a\tb\r\nc\nd"},
		{"wide characters", []byte("世界"), "世界"},
		{"nothing at all", []byte{}, ""},
	}
	for _, c := range cases {
		if got := export.CellText(c.value); got != c.want {
			t.Errorf("%s: CellText = %q, want %q", c.name, got, c.want)
		}
	}
}

func TestJSONCallsTheSameBytesBinary(t *testing.T) {
	got := write(t, "t.json", []string{"blob", "text"}, [][]any{{[]byte{0x00, 0x01}, []byte("a\nb")}})
	if want := `"blob": "AAE="`; !strings.Contains(got, want) {
		t.Errorf("%s: want the blob as base64, %s", got, want)
	}
	if want := `"text": "a\nb"`; !strings.Contains(got, want) {
		t.Errorf("%s: want the text as text, %s", got, want)
	}
}

// fakeDecimal has the methods of the driver's decimal, which this package cannot import.
type fakeDecimal string

func (d fakeDecimal) String() string   { return string(d) }
func (d fakeDecimal) Float64() float64 { return 0 }

func TestAListOrStructIsJSONAsText(t *testing.T) {
	for _, tt := range []struct {
		value any
		want  string
	}{
		{[]any{int32(1), nil, int32(3)}, `[1,null,3]`},
		{map[string]any{"x": int32(1), "y": "two"}, `{"x":1,"y":"two"}`},
		{[]any{fakeDecimal("2.50")}, `[2.50]`},
		{[]any{"a<b>&c"}, `["a<b>&c"]`},
		{[]any{}, `[]`},
		{[]any{[]byte("abc")}, `["abc"]`},
		{[]any{[]byte{0x00, 0xff}}, `["AP8="]`},
		{[]any{time.Date(-43, 3, 15, 0, 0, 0, 0, time.UTC)}, `["-0043-03-15T00:00:00Z"]`},
		{[]any{time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)}, `["2026-01-02T03:04:05Z"]`},
	} {
		if got := export.CellText(tt.value); got != tt.want {
			t.Errorf("CellText(%#v) = %s, want %s", tt.value, got, tt.want)
		}
	}
}

func TestJSONKeepsNestedNumbersAsNumbers(t *testing.T) {
	got := write(t, "t.json", []string{"top", "list", "object", "odd"}, [][]any{{
		fakeDecimal("12.5"),
		[]any{fakeDecimal("1.5"), nil, int32(2)},
		map[string]any{"cost": fakeDecimal("2.5"), "deep": []any{fakeDecimal("3.5")}},
		[]any{math.NaN(), 1.5, math.Inf(1)},
	}})
	for _, want := range []string{
		`"top": 12.5`,
		`"list": [1.5,null,2]`,
		`"object": {"cost":2.5,"deep":[3.5]}`,
		`"odd": ["NaN",1.5,"+Inf"]`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("want %s in:\n%s", want, got)
		}
	}
}

func TestDuplicateColumnNamesAreSuffixed(t *testing.T) {
	recs := readCSV(t, write(t, "d.csv", []string{"a", "a", "a", "b"}, [][]any{{1, 2, 3, 4}}))
	if got := strings.Join(recs[0], ","); got != "a,a_1,a_2,b" {
		t.Errorf("header = %s, want a,a_1,a_2,b", got)
	}
	recs = readCSV(t, write(t, "d2.csv", []string{"a", "a", "a_1"}, [][]any{{1, 2, 3}}))
	if got := strings.Join(recs[0], ","); got != "a,a_2,a_1" {
		t.Errorf("header = %s, want a,a_2,a_1", got)
	}
	var objs []map[string]any
	if err := json.Unmarshal([]byte(write(t, "d.json", []string{"a", "a"}, [][]any{{1, 2}})), &objs); err != nil {
		t.Fatal(err)
	}
	if objs[0]["a"] == nil || objs[0]["a_1"] == nil || len(objs[0]) != 2 {
		t.Errorf("json keys = %v, want a and a_1", objs[0])
	}
}

func TestEmptyResultsStillExport(t *testing.T) {
	if recs := readCSV(t, write(t, "e.csv", []string{"a", "b"}, nil)); len(recs) != 1 {
		t.Errorf("csv = %q, want just the header", recs)
	}
	var objs []any
	out := write(t, "e.json", []string{"a"}, nil)
	if err := json.Unmarshal([]byte(out), &objs); err != nil || len(objs) != 0 {
		t.Errorf("json = %q, %v; want []", out, err)
	}
}

func TestTheExtensionDecidesTheFormatAndUnknownOnesAreRefused(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"x.CSV", "x.Json"} {
		tg, err := export.Create(filepath.Join(dir, name))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if err := tg.Write([]string{"a"}, [][]any{{1}}); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{"x.txt", "x", "x.parquet"} {
		path := filepath.Join(dir, name)
		if _, err := export.Create(path); err == nil || !strings.Contains(err.Error(), ".csv or .json") {
			t.Errorf("%s: err = %v, want a refusal naming the supported formats", name, err)
		}
		if _, err := os.Stat(path); err == nil {
			t.Errorf("%s: a file was created for an unsupported format", name)
		}
	}
}

func TestAnExistingFileIsNeverOverwritten(t *testing.T) {
	path := filepath.Join(t.TempDir(), "keep.csv")
	if err := os.WriteFile(path, []byte("precious"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := export.Create(path)
	if !errors.Is(err, export.ErrExists) {
		t.Fatalf("err = %v, want ErrExists", err)
	}
	if b, _ := os.ReadFile(path); string(b) != "precious" {
		t.Errorf("the existing file was changed: %q", b)
	}
}

func TestExportedFilesAreOwnerOnly(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows has no Unix permission bits")
	}
	path := filepath.Join(t.TempDir(), "p.csv")
	tg, err := export.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := tg.Write([]string{"a"}, nil); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("mode = %o, want 600: results can hold sensitive data", perm)
	}
}

func TestAMissingDirectoryIsAnError(t *testing.T) {
	if _, err := export.Create(filepath.Join(t.TempDir(), "no", "such", "dir", "x.csv")); err == nil {
		t.Error("expected an error")
	}
	if _, err := export.Create(""); err == nil {
		t.Error("an empty path should be an error")
	}
}

func TestAResultAtTheDefaultCapExports(t *testing.T) {
	const n = 10000
	rows := make([][]any, n)
	for i := range rows {
		rows[i] = []any{int64(i), "row"}
	}
	recs := readCSV(t, write(t, "big.csv", []string{"id", "s"}, rows))
	if len(recs) != n+1 {
		t.Errorf("csv has %d records, want %d", len(recs), n+1)
	}
	var objs []map[string]any
	if err := json.Unmarshal([]byte(write(t, "big.json", []string{"id", "s"}, rows)), &objs); err != nil || len(objs) != n {
		t.Errorf("json has %d objects, %v; want %d", len(objs), err, n)
	}
}
