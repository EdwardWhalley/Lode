package adaptertest_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/EdwardWhalley/lode/internal/adapter"
	"github.com/EdwardWhalley/lode/internal/adapter/adaptertest"
	"github.com/EdwardWhalley/lode/internal/sqltext"
)

// The fake's statements, each of which it knows by its text.
const (
	slowQuery  = "SLOW"
	hugeQuery  = "HUGE"
	typesQuery = "TYPES"
	writeSQL   = "INSERT INTO people VALUES (4, 'dave')"
	noCountSQL = "CREATE TABLE no_count (i INT)"
	secret     = "hunter2"
	writer     = "fake://writer"
)

// engineDialect is how the fake engine reads text.
var engineDialect = sqltext.Dialect{NestedComments: true}

// broken names the rule the fake breaks; the zero value breaks none.
type broken string

const (
	writeAfterSet   broken = "lets a write through after a SET"
	passwordInError broken = "repeats the password in an error"
	transactionOpen broken = "leaves a transaction open"
	unlistedType    broken = "returns an unlisted type"
	blankField      broken = "leaves a Target field blank with no reason"
	otherDialect    broken = "has a Kind whose Dialect reads a probe differently from its engine"
)

// person is a row of the people table.
type person struct {
	id   int64
	name string
}

// store is a Source's rows, and the copy a transaction left open writes to.
type store struct {
	mu      sync.Mutex
	rows    []person
	pending []person
}

func newStore() *store {
	return &store{rows: []person{{1, "alice"}, {2, "bob"}, {3, "carol"}}}
}

// fake is an Adapter over a store.
type fake struct {
	s       *store
	write   bool
	breaks  broken
	setSeen bool
}

func (f *fake) Close() error { return nil }

func (f *fake) Schema(context.Context) ([]adapter.Table, error) {
	return []adapter.Table{{Name: "people", Ref: `"people"`, Columns: []adapter.Column{
		{Name: "id", Type: "INTEGER", Ref: `"id"`},
		{Name: "name", Type: "TEXT", Ref: `"name"`},
	}}}, nil
}

var update = regexp.MustCompile(`^UPDATE people SET id = (?:id \+ 100|1)(?: WHERE id = (-?\d+))?$`)

func (f *fake) Query(ctx context.Context, query string) (adapter.Rows, error) {
	f.s.mu.Lock()
	defer f.s.mu.Unlock()
	visible := f.s.rows
	if f.write && f.s.pending != nil {
		visible = f.s.pending
	}
	switch query {
	case slowQuery:
		f.s.mu.Unlock()
		<-ctx.Done()
		f.s.mu.Lock()
		return nil, ctx.Err()
	case hugeQuery:
		return &hugeRows{}, nil
	case typesQuery:
		row := []any{int64(1), "x", []byte{0xff}, time.Date(2026, 11, 9, 0, 0, 0, 0, time.UTC), nil}
		if f.breaks == unlistedType {
			row = append(row, int32(1))
		}
		return table(nil, row), nil
	case "SELECT id, name FROM people ORDER BY id":
		var out [][]any
		for _, p := range visible {
			out = append(out, []any{p.id, p.name})
		}
		return table([]string{"id", "name"}, out...), nil
	case "SELECT id FROM people ORDER BY id", `SELECT "id" FROM "people"`:
		var out [][]any
		for _, p := range visible {
			out = append(out, []any{p.id})
		}
		return table([]string{"id"}, out...), nil
	case `SELECT "name" FROM "people"`:
		return table([]string{"name"}, []any{"alice"}), nil
	case "SELECT count(*) FROM people", `SELECT count(*) FROM "people"`:
		return table([]string{"count"}, []any{int64(len(visible))}), nil
	case writeSQL:
		if !f.write && !(f.breaks == writeAfterSet && f.setSeen) {
			return nil, errors.New("read-only")
		}
		f.s.rows = append(f.s.rows, person{4, "dave"})
		return adapter.NoRows(1, true, nil), nil
	case "SET TRANSACTION READ WRITE":
		f.setSeen = true
		return adapter.NoRows(0, false, nil), nil
	case "BEGIN":
		if !f.write || f.breaks != transactionOpen {
			return nil, errors.New("a transaction cannot outlast a run")
		}
		f.s.pending = slices.Clone(f.s.rows)
		return adapter.NoRows(0, false, nil), nil
	case noCountSQL:
		if !f.write {
			return nil, errors.New("read-only")
		}
		return adapter.NoRows(0, false, nil), nil
	}
	if m := update.FindStringSubmatch(query); m != nil {
		if !f.write {
			return nil, errors.New("read-only")
		}
		rows := f.s.rows
		if f.s.pending != nil {
			rows = f.s.pending
		}
		var n int64
		for i := range rows {
			if m[1] == "" || fmt.Sprint(rows[i].id) == m[1] {
				rows[i].id += 100
				n++
			}
		}
		return adapter.NoRows(n, true, nil), nil
	}
	return engineReads(query)
}

// engineReads is the fake engine's answer to any other text: one SELECT, read with
// engineDialect, answers with its own code, and anything else is refused.
func engineReads(text string) (adapter.Rows, error) {
	pieces := sqltext.Statements(text, engineDialect)
	var code []string
	for _, s := range pieces {
		if !s.Blank {
			code = append(code, text[s.Code:s.CodeEnd])
		}
	}
	switch {
	case pieces[len(pieces)-1].Open:
		return nil, errors.New("left open")
	case len(code) != 1:
		return nil, fmt.Errorf("%d statements", len(code))
	}
	words := sqltext.Words(code[0], engineDialect)
	if len(words) == 0 || !strings.EqualFold(words[0].Text, "SELECT") {
		return nil, errors.New("syntax error")
	}
	return table([]string{"?column?"}, []any{code[0]}), nil
}

// tableRows is a result held whole.
type tableRows struct {
	cols []adapter.Column
	rows [][]any
	at   int
}

func table(names []string, rows ...[]any) *tableRows {
	r := &tableRows{rows: rows, at: -1}
	if names == nil && len(rows) > 0 {
		for i := range rows[0] {
			names = append(names, fmt.Sprintf("c%d", i))
		}
	}
	for _, n := range names {
		r.cols = append(r.cols, adapter.Column{Name: n, Type: "ANY"})
	}
	return r
}

func (r *tableRows) Columns() []adapter.Column   { return r.cols }
func (r *tableRows) Next() bool                  { r.at++; return r.at < len(r.rows) }
func (r *tableRows) Values() []any               { return r.rows[r.at] }
func (r *tableRows) Err() error                  { return nil }
func (r *tableRows) RowsAffected() (int64, bool) { return 0, false }
func (r *tableRows) Close() error                { return nil }

// hugeRows never ends.
type hugeRows struct{ n int64 }

func (r *hugeRows) Columns() []adapter.Column   { return []adapter.Column{{Name: "n", Type: "INTEGER"}} }
func (r *hugeRows) Next() bool                  { r.n++; return true }
func (r *hugeRows) Values() []any               { return []any{r.n} }
func (r *hugeRows) Err() error                  { return nil }
func (r *hugeRows) RowsAffected() (int64, bool) { return 0, false }
func (r *hugeRows) Close() error                { return nil }

// fakeTarget is a Target over the fake, breaking the rule b names.
func fakeTarget(b broken) func(t *testing.T) adaptertest.Target {
	return func(t *testing.T) adaptertest.Target {
		writable := newStore()
		kind := adapter.Kind{
			Name:    "fake",
			Schemes: []string{"fake"},
			Dialect: engineDialect,
			Open: func(r adapter.Request) (adapter.Adapter, error) {
				switch {
				case r.Location == writer:
					return &fake{s: writable, write: r.Write, breaks: b}, nil
				case b == passwordInError:
					return nil, fmt.Errorf("cannot reach %s", r.Location)
				}
				return nil, errors.New("cannot reach the server")
			},
		}
		if b == otherDialect {
			kind.Dialect = sqltext.Dialect{}
		}
		tg := adaptertest.Target{
			Kind:       kind,
			Adapter:    &fake{s: newStore(), breaks: b},
			Table:      "people",
			SlowQuery:  slowQuery,
			HugeQuery:  hugeQuery,
			WriteSQL:   writeSQL,
			Escapes:    []string{"SET TRANSACTION READ WRITE"},
			Secret:     adaptertest.Secret{Password: secret, Locations: []string{"fake://reader:" + secret + "@nowhere/db"}},
			TypesQuery: typesQuery,
			Writable: &adaptertest.Writable{
				Adapter:          &fake{s: writable, write: true, breaks: b},
				Table:            "people",
				NoCountSQL:       noCountSQL,
				Location:         writer,
				OpensTransaction: []string{"BEGIN"},
			},
		}
		if b == blankField {
			tg.HugeQuery = ""
		}
		return tg
	}
}

// breakEnv names the rule a child run's fake breaks.
const breakEnv = "LODE_TEST_FAKE_BREAKS"

func TestTheSuiteOnAFake(t *testing.T) {
	adaptertest.Run(t, fakeTarget(broken(os.Getenv(breakEnv))))
}

func TestTheSuiteFailsAFakeThatBreaksARule(t *testing.T) {
	if os.Getenv(breakEnv) != "" {
		t.Skip("this is a child run")
	}
	failed := regexp.MustCompile(`--- FAIL: TestTheSuiteOnAFake/(\S+)`)
	for _, c := range []struct {
		breaks broken
		rule   string
	}{
		{writeAfterSet, "read-only cannot be switched off by a query"},
		{passwordInError, "no error holds the password"},
		{transactionOpen, "a transaction cannot outlast a run"},
		{unlistedType, "every value is one Rows.Values may hold"},
		{blankField, "closing a cancelled result set returns promptly"},
		{otherDialect, "sqltext reads text as the engine does"},
	} {
		t.Run(string(c.breaks), func(t *testing.T) {
			child := exec.Command(os.Args[0], "-test.run=^TestTheSuiteOnAFake$", "-test.v")
			child.Env = append(os.Environ(), breakEnv+"="+string(c.breaks))
			out, err := child.CombinedOutput()
			if err == nil {
				t.Fatalf("the suite passed a fake that %s", c.breaks)
			}
			var rules []string
			for _, m := range failed.FindAllStringSubmatch(string(out), -1) {
				rules = append(rules, m[1])
			}
			if want := []string{strings.ReplaceAll(c.rule, " ", "_")}; !slices.Equal(rules, want) {
				t.Errorf("the rules that failed are %q, want only %q:\n%s", rules, want, out)
			}
		})
	}
}
