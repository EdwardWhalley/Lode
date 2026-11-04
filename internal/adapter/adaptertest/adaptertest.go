// Package adaptertest is the contract suite every Adapter must pass. It holds the rules an
// Adapter could break without its author noticing, and each engine's own awkward statements
// are data its Target supplies. A field the suite needs fails the suite if it is blank,
// unless the field named No... beside it says why the Target has none.
package adaptertest

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/EdwardWhalley/lode/internal/adapter"
	"github.com/EdwardWhalley/lode/internal/sourcetext"
)

// Target is an Adapter under test, opened through Kind, on a Source with the rows (1,
// alice), (2, bob), (3, carol) in columns id, name of Table.
//
// SlowQuery runs until cancelled; HugeQuery streams far more rows than anyone reads.
// WriteSQL is a write the read-only Adapter must refuse, and Escapes are statements that try
// to get past read-only (setting the transaction read-write, a COMMIT then a write, several
// statements in one run, a function with side effects, the engine's own), after each of
// which WriteSQL must still be refused. Secret is what no error may hold. TypesQuery returns
// one column of each common type of the engine, with a row of NULLs.
type Target struct {
	Kind        adapter.Kind
	Adapter     adapter.Adapter
	Table       string
	SlowQuery   string
	HugeQuery   string
	NoHugeQuery string
	WriteSQL    string
	NoWriteSQL  string
	Escapes     []string
	Secret      Secret
	TypesQuery  string
	Writable    *Writable
	NoWritable  string
}

// Secret is a password and Locations that hold it and fail to open: a wrong password, a bad
// host, a bad option and, where the DSN takes one, the password in its options. None says
// why the Kind has no password, in place of the rest.
type Secret struct {
	Password  string
	Locations []string
	None      string
}

// Writable is an Adapter under test that can write, on its own copy of those rows, at
// Location. OpensTransaction are the statements that open a transaction on this engine, each
// of which it must refuse; NoOpenTransaction says why none can outlast a run, in place of
// them and Location. ConnectionsInUse, if set, counts the connections the Adapter has not
// given back.
type Writable struct {
	Adapter           adapter.Adapter
	Table             string
	NoCountSQL        string
	Location          string
	OpensTransaction  []string
	NoOpenTransaction string
	ConnectionsInUse  func() int
}

// need fails the test if the Target lacks what it is about, and skips it with the reason if
// the Target says why it has none.
func need(t *testing.T, has bool, field, reason string) {
	t.Helper()
	switch {
	case has:
	case reason == "":
		t.Fatalf("the Target has no %s, and no reason why", field)
	default:
		t.Skip(reason)
	}
}

// Run executes the contract suite. newTarget is called once per subtest.
func Run(t *testing.T, newTarget func(t *testing.T) Target) {
	t.Helper()

	t.Run("streams rows with column metadata", func(t *testing.T) {
		tg := open(t, newTarget)
		rows, err := tg.Adapter.Query(context.Background(), "SELECT id, name FROM "+tg.Table+" ORDER BY id")
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()

		cols := rows.Columns()
		if len(cols) != 2 || cols[0].Name != "id" || cols[1].Name != "name" {
			t.Fatalf("columns = %+v, want id, name", cols)
		}
		var names []string
		for rows.Next() {
			vals := rows.Values()
			if len(vals) != 2 {
				t.Fatalf("row has %d values, want 2", len(vals))
			}
			names = append(names, text(t, vals[1]))
		}
		if err := rows.Err(); err != nil {
			t.Fatal(err)
		}
		want := []string{"alice", "bob", "carol"}
		if len(names) != len(want) {
			t.Fatalf("names = %v, want %v", names, want)
		}
		for i := range want {
			if names[i] != want[i] {
				t.Fatalf("names = %v, want %v", names, want)
			}
		}
	})

	t.Run("describes its schema", func(t *testing.T) {
		tg := open(t, newTarget)
		tables, err := tg.Adapter.Schema(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		var found *adapter.Table
		for i := range tables {
			if tables[i].Name == tg.Table {
				found = &tables[i]
			}
		}
		if found == nil {
			t.Fatalf("schema %+v has no table %q", tables, tg.Table)
		}
		if len(found.Columns) != 2 || found.Columns[0].Name != "id" || found.Columns[1].Name != "name" {
			t.Fatalf("columns = %+v, want id, name in order", found.Columns)
		}
		for _, c := range found.Columns {
			if c.Type == "" {
				t.Errorf("column %q has no type", c.Name)
			}
		}
		for _, tb := range tables {
			if tb.Ref == "" {
				t.Errorf("table %q has no Ref", tb.Name)
			}
			for _, c := range tb.Columns {
				if c.Ref == "" {
					t.Errorf("column %q of table %q has no Ref", c.Name, tb.Name)
				}
			}
		}
		for _, c := range found.Columns {
			rows, err := tg.Adapter.Query(context.Background(), "SELECT "+c.Ref+" FROM "+found.Ref)
			if err != nil {
				t.Errorf("column Ref %q is not usable in SQL: %v", c.Ref, err)
				continue
			}
			rows.Close()
		}
		rows, err := tg.Adapter.Query(context.Background(), "SELECT count(*) FROM "+found.Ref)
		if err != nil {
			t.Fatalf("Ref %q is not usable in SQL: %v", found.Ref, err)
		}
		defer rows.Close()
		if !rows.Next() || fmt.Sprint(rows.Values()[0]) != "3" {
			t.Errorf("SELECT count(*) FROM %s = %v, want 3", found.Ref, rows.Values())
		}
		for i := 1; i < len(tables); i++ {
			if tables[i-1].Name > tables[i].Name {
				t.Errorf("tables not sorted by name: %q before %q", tables[i-1].Name, tables[i].Name)
			}
		}
	})

	t.Run("rejects writes", func(t *testing.T) {
		tg := open(t, newTarget)
		need(t, tg.WriteSQL != "", "WriteSQL", tg.NoWriteSQL)
		if err := refused(tg.Adapter, tg.WriteSQL); err != nil {
			t.Fatal(err)
		}
		wantTheFixture(t, tg.Adapter, tg.Table)
	})

	t.Run("read-only cannot be switched off by a query", func(t *testing.T) {
		tg := open(t, newTarget)
		need(t, tg.WriteSQL != "", "WriteSQL", tg.NoWriteSQL)
		if len(tg.Escapes) == 0 {
			t.Fatal("the Target lists no Escapes: statements that try to get past read-only")
		}
		before, err := tg.Adapter.Schema(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		for _, escape := range tg.Escapes {
			if rows, err := tg.Adapter.Query(context.Background(), escape); err == nil {
				for rows.Next() {
				}
				rows.Close()
			}
			if err := refused(tg.Adapter, tg.WriteSQL); err != nil {
				t.Errorf("after %q: %v", escape, err)
			}
		}
		wantTheFixture(t, tg.Adapter, tg.Table)
		after, err := tg.Adapter.Schema(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(after, before) {
			t.Errorf("the schema changed:\n\tbefore %+v\n\tafter  %+v", before, after)
		}
	})

	t.Run("no error holds the password", func(t *testing.T) {
		tg := open(t, newTarget)
		need(t, tg.Secret.Password != "", "Secret", tg.Secret.None)
		if len(tg.Secret.Locations) == 0 {
			t.Fatal("the Target's Secret has no Locations that hold it")
		}
		password := tg.Secret.Password
		for _, location := range tg.Secret.Locations {
			if !strings.Contains(location, password) {
				t.Errorf("%s does not hold the Secret", location)
			}
			if sourcetext.LooksLikeDSN(location) && strings.Contains(sourcetext.MaskDSN(location), password) {
				t.Errorf("the Label of a location holds the password: %s", sourcetext.MaskDSN(location))
			}
			for _, write := range []bool{false, true} {
				if write && tg.Kind.ReadOnly {
					continue
				}
				a, err := tg.Kind.Open(adapter.Request{Location: location, Write: write})
				switch {
				case err == nil:
					a.Close()
					t.Errorf("a location that holds the Secret opened, write %v; each must fail to open", write)
				case strings.Contains(err.Error(), password):
					t.Errorf("opening with write %v: the error holds the password: %s", write,
						strings.ReplaceAll(err.Error(), password, "<the password>"))
				}
			}
		}
	})

	t.Run("every value is one Rows.Values may hold", func(t *testing.T) {
		tg := open(t, newTarget)
		if tg.TypesQuery == "" {
			t.Fatal("the Target has no TypesQuery: one column of each common type of the engine")
		}
		cols, rows := readAll(t, tg.Adapter, tg.TypesQuery)
		if len(rows) == 0 {
			t.Fatalf("%s returned no rows", tg.TypesQuery)
		}
		for _, row := range rows {
			for i, v := range row {
				if err := adapter.CheckValue(v); err != nil {
					t.Errorf("column %s (%s): %v", cols[i].Name, cols[i].Type, err)
				}
			}
		}
	})

	t.Run("sqltext reads text as the engine does", func(t *testing.T) {
		tg := open(t, newTarget)
		for _, p := range probes {
			readsAsTheEngineDoes(t, tg.Adapter, tg.Kind.Dialect, p)
		}
	})

	writable := func(t *testing.T) (Target, *Writable) {
		t.Helper()
		tg := open(t, newTarget)
		need(t, tg.Writable != nil, "Writable", tg.NoWritable)
		return tg, tg.Writable
	}

	t.Run("a write says how many rows it affected", func(t *testing.T) {
		_, w := writable(t)
		got := runWrite(t, w.Adapter, "UPDATE "+w.Table+" SET id = id + 100")
		if got != (written{n: 3, known: true}) {
			t.Errorf("an update of 3 rows reports %+v", got)
		}
		got = runWrite(t, w.Adapter, "UPDATE "+w.Table+" SET id = id + 100 WHERE id = 101")
		if got != (written{n: 1, known: true}) {
			t.Errorf("an update of 1 row reports %+v", got)
		}
	})

	t.Run("a write that matches nothing says zero", func(t *testing.T) {
		_, w := writable(t)
		runWrite(t, w.Adapter, "UPDATE "+w.Table+" SET id = id + 100")
		got := runWrite(t, w.Adapter, "UPDATE "+w.Table+" SET id = 1 WHERE id = -1")
		if got != (written{n: 0, known: true}) {
			t.Errorf("an update matching nothing reports %+v", got)
		}
	})

	t.Run("a statement with no count does not invent one", func(t *testing.T) {
		_, w := writable(t)
		runWrite(t, w.Adapter, "UPDATE "+w.Table+" SET id = id + 100")
		if got := runWrite(t, w.Adapter, w.NoCountSQL); got.known {
			t.Errorf("%s reports %+v, want no count", w.NoCountSQL, got)
		}
	})

	t.Run("a query on a writable Source gives what it gives on a read-only one", func(t *testing.T) {
		tg, w := writable(t)
		wantCols, want := readAll(t, tg.Adapter, "SELECT id, name FROM "+tg.Table+" ORDER BY id")
		gotCols, got := readAll(t, w.Adapter, "SELECT id, name FROM "+w.Table+" ORDER BY id")
		if !reflect.DeepEqual(gotCols, wantCols) {
			t.Errorf("columns = %+v, want %+v", gotCols, wantCols)
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("rows = %#v, want %#v", got, want)
		}
	})

	t.Run("a long run of writes keeps working", func(t *testing.T) {
		_, w := writable(t)
		for i := range 200 {
			got := runWrite(t, w.Adapter, "UPDATE "+w.Table+" SET id = id + 100")
			if got != (written{n: 3, known: true}) {
				t.Fatalf("write %d reports %+v", i, got)
			}
		}
	})

	t.Run("a transaction cannot outlast a run", func(t *testing.T) {
		tg, w := writable(t)
		need(t, len(w.OpensTransaction) > 0, "OpensTransaction", w.NoOpenTransaction)
		if w.Location == "" {
			t.Fatal("the Writable has no Location for a second Adapter to open")
		}
		other, err := tg.Kind.Open(adapter.Request{Location: w.Location})
		if err != nil {
			t.Fatalf("a second Adapter on the writable Source: %v", err)
		}
		defer other.Close()
		ids := "SELECT id FROM " + w.Table + " ORDER BY id"
		for _, statement := range w.OpensTransaction {
			if err := refused(w.Adapter, statement); err != nil {
				t.Errorf("%v: it opens a transaction", err)
			}
			if w.ConnectionsInUse != nil {
				if n := w.ConnectionsInUse(); n != 0 {
					t.Errorf("%s: %d connections in use after it", statement, n)
				}
			}
			if got := runWrite(t, w.Adapter, "UPDATE "+w.Table+" SET id = id + 100"); got != (written{n: 3, known: true}) {
				t.Errorf("after %s an update of 3 rows reports %+v", statement, got)
			}
			_, mine := readAll(t, w.Adapter, ids)
			_, seen := readAll(t, other, ids)
			if !reflect.DeepEqual(seen, mine) {
				t.Errorf("after %s a second Adapter sees %v, and the writer %v: something held the write", statement, seen, mine)
			}
		}
	})

	t.Run("returns an error for bad SQL", func(t *testing.T) {
		tg := open(t, newTarget)
		rows, err := tg.Adapter.Query(context.Background(), "SELEKT nonsense")
		if err == nil {
			rows.Close()
			t.Fatal("expected an error for invalid SQL")
		}
	})

	t.Run("closing a cancelled result set returns promptly", func(t *testing.T) {
		tg := open(t, newTarget)
		need(t, tg.HugeQuery != "", "HugeQuery", tg.NoHugeQuery)
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		rows, err := tg.Adapter.Query(ctx, tg.HugeQuery)
		if err != nil {
			t.Fatal(err)
		}
		for i := 0; i < 3; i++ {
			if !rows.Next() {
				t.Fatalf("row %d: result ended early: %v", i+1, rows.Err())
			}
		}
		cancel()

		closed := make(chan struct{})
		go func() {
			rows.Close()
			close(closed)
		}()
		select {
		case <-closed:
		case <-time.After(15 * time.Second):
			t.Fatal("Close did not return: it is reading out the rest of the result")
		}
	})

	t.Run("cancelling the context stops a running query", func(t *testing.T) {
		tg := open(t, newTarget)
		if tg.SlowQuery == "" {
			t.Fatal("the Target has no SlowQuery")
		}
		ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
		defer cancel()

		done := make(chan error, 1)
		go func() {
			rows, err := tg.Adapter.Query(ctx, tg.SlowQuery)
			if err == nil {
				for rows.Next() {
				}
				err = rows.Err()
				rows.Close()
			}
			done <- err
		}()

		select {
		case err := <-done:
			if err == nil {
				t.Fatal("expected the cancelled query to fail")
			}
		case <-time.After(10 * time.Second):
			t.Fatal("query was not stopped by context cancellation")
		}
	})
}

// refused says why the write was not refused, or nil if it was.
func refused(a adapter.Adapter, query string) error {
	rows, err := a.Query(context.Background(), query)
	if err == nil {
		for rows.Next() {
		}
		err = rows.Err()
		rows.Close()
	}
	if err == nil {
		return fmt.Errorf("%s was not refused", query)
	}
	return nil
}

// wantTheFixture checks table still holds the three rows of the fixture.
func wantTheFixture(t *testing.T, a adapter.Adapter, table string) {
	t.Helper()
	_, rows := readAll(t, a, "SELECT id, name FROM "+table+" ORDER BY id")
	var names []string
	for _, row := range rows {
		names = append(names, text(t, row[1]))
	}
	if want := []string{"alice", "bob", "carol"}; !reflect.DeepEqual(names, want) {
		t.Errorf("the rows changed: names are %v, want %v", names, want)
	}
}

// written is what a statement with no columns reports.
type written struct {
	n     int64
	known bool
}

// runWrite runs query as the UI does, asks for the count before closing, and fails if it
// returned columns.
func runWrite(t *testing.T, a adapter.Adapter, query string) written {
	t.Helper()
	rows, err := a.Query(context.Background(), query)
	if err != nil {
		t.Fatalf("%s: %v", query, err)
	}
	defer rows.Close()
	for rows.Next() {
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("%s: %v", query, err)
	}
	if cols := rows.Columns(); len(cols) != 0 {
		t.Fatalf("%s returned columns %+v, want none", query, cols)
	}
	n, known := rows.RowsAffected()
	return written{n: n, known: known}
}

// readAll runs query and returns its columns' names and types, and its rows.
func readAll(t *testing.T, a adapter.Adapter, query string) (cols []adapter.Column, out [][]any) {
	t.Helper()
	rows, err := a.Query(context.Background(), query)
	if err != nil {
		t.Fatalf("%s: %v", query, err)
	}
	defer rows.Close()
	cols = rows.Columns()
	for rows.Next() {
		out = append(out, append([]any(nil), rows.Values()...))
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("%s: %v", query, err)
	}
	return cols, out
}

// text returns a text value, which an Adapter may deliver as string or []byte.
func text(t *testing.T, v any) string {
	t.Helper()
	switch v := v.(type) {
	case string:
		return v
	case []byte:
		return string(v)
	}
	t.Fatalf("text value is %T, want string or []byte", v)
	return ""
}

// open makes a Target that is closed when the test ends.
func open(t *testing.T, newTarget func(t *testing.T) Target) Target {
	t.Helper()
	tg := newTarget(t)
	t.Cleanup(func() {
		tg.Adapter.Close()
		if tg.Writable != nil {
			tg.Writable.Adapter.Close()
		}
	})
	if tg.Kind.Open == nil {
		t.Fatal("the Target has no Kind")
	}
	return tg
}
