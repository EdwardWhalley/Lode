package postgres_test

import (
	"context"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/EdwardWhalley/lode/internal/adapter"
	"github.com/EdwardWhalley/lode/internal/adapter/adaptertest"
	"github.com/EdwardWhalley/lode/internal/adapter/postgres"
	"github.com/EdwardWhalley/lode/internal/export"
)

// writable is openWritable for a test that does not hand the Adapter to the contract.
func writable(t *testing.T) *postgres.Adapter {
	t.Helper()
	a := openWritable(t)
	t.Cleanup(func() { a.Close() })
	return a
}

func TestContractOnAWritableSource(t *testing.T) {
	adaptertest.Run(t, func(t *testing.T) adaptertest.Target {
		return adaptertest.Target{
			Kind:    postgres.Kind,
			Adapter: writable(t),
			Table:   "people",
			// bounded: an aggregate, so the result is one row; the contract test cancels it after 200ms.
			SlowQuery: "SELECT count(*) FROM generate_series(1, 100000000000) a, generate_series(1, 100000000000) b",
			// bounded: streamed one row at a time, and the test cancels it after reading three.
			HugeQuery:  "SELECT generate_series(1, 100000000000)",
			NoWriteSQL: "the Adapter under test can write",
			Secret:     secret(t),
			TypesQuery: typesQuery,
			NoWritable: "the Adapter under test is itself writable; TestContract has a read-only one beside a writable one",
		}
	})
}

// run reads a query out as the UI does and asks for the count before closing.
func run(t *testing.T, a adapter.Adapter, query string) (cols, rowsRead int, n int64, known bool) {
	t.Helper()
	rows, err := a.Query(t.Context(), query)
	if err != nil {
		t.Fatalf("%s: %v", query, err)
	}
	defer rows.Close()
	for rows.Next() {
		rowsRead++
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("%s: %v", query, err)
	}
	n, known = rows.RowsAffected()
	return len(rows.Columns()), rowsRead, n, known
}

func wantCount(t *testing.T, a adapter.Adapter, query string, want int64) {
	t.Helper()
	cols, _, n, known := run(t, a, query)
	if cols != 0 || !known || n != want {
		t.Errorf("%s: %d columns, count %d known %v; want no columns and %d", query, cols, n, known, want)
	}
}

func wantNoCount(t *testing.T, a adapter.Adapter, query string) {
	t.Helper()
	if cols, _, n, known := run(t, a, query); cols != 0 || known {
		t.Errorf("%s: %d columns, count %d known %v; want no columns and no count", query, cols, n, known)
	}
}

func TestWritesReportWhatTheyChanged(t *testing.T) {
	a := writable(t)
	wantNoCount(t, a, "CREATE TABLE animals (kind text)")
	wantCount(t, a, "INSERT INTO animals VALUES ('ant'), ('bee'), ('cat')", 3)
	wantCount(t, a, "UPDATE animals SET kind = kind || '!'", 3)
	wantCount(t, a, "UPDATE animals SET kind = 'one' WHERE kind = 'ant!'", 1)
	wantCount(t, a, "UPDATE animals SET kind = 'x' WHERE kind = 'nothing like this'", 0)
	wantCount(t, a, "DELETE FROM animals", 3)
	wantCount(t, a, "WITH c AS (SELECT 1) UPDATE people SET name = name", 3)
	wantCount(t, a, "-- a note\nUPDATE people SET name = name;", 3)
	wantCount(t, a, "INSERT INTO people VALUES (1, 'taken') ON CONFLICT DO NOTHING", 0)
	wantCount(t, a, "MERGE INTO people p USING (VALUES (1), (2)) v(id) ON p.id = v.id WHEN MATCHED THEN UPDATE SET name = p.name", 2)
}

func TestAStatementRunsOnce(t *testing.T) {
	a := writable(t)
	wantNoCount(t, a, "CREATE SEQUENCE s")
	if _, rows := readAll(t, a, "SELECT nextval('s')"); len(rows) != 1 || rows[0][0] != int64(1) {
		t.Errorf("the first nextval gave %v, want 1", rows)
	}
	wantNoCount(t, a, "DO $$ BEGIN PERFORM nextval('s'); END $$")
	if _, rows := readAll(t, a, "SELECT last_value FROM s"); len(rows) != 1 || rows[0][0] != int64(2) {
		t.Errorf("after one more use the sequence is at %v, want 2", rows)
	}
}

func TestStatementsWithNoCountSaySoStraightAfterAWrite(t *testing.T) {
	a := writable(t)
	wantCount(t, a, "UPDATE people SET name = name", 3)
	for _, query := range []string{
		"CREATE TABLE fresh (i integer)",
		"CREATE TABLE made AS SELECT 1 AS i",
		"ALTER TABLE fresh ADD COLUMN j integer",
		"CREATE INDEX i ON people (name)",
		"DROP TABLE fresh",
		"SET search_path = public",
		"COMMIT",
		"TRUNCATE made",
		"DO $$ BEGIN PERFORM 1; END $$",
	} {
		wantNoCount(t, a, query)
	}
}

func TestReturningIsAQuery(t *testing.T) {
	a := writable(t)
	for _, c := range []struct {
		query string
		rows  int
	}{
		{"UPDATE people SET name = name || '!' RETURNING id, name", 3},
		{"INSERT INTO people VALUES (4, 'dave') RETURNING id, name", 1},
		{"WITH u AS (UPDATE people SET name = name RETURNING id, name) SELECT * FROM u", 4},
	} {
		if cols, rows, _, _ := run(t, a, c.query); cols != 2 || rows != c.rows {
			t.Errorf("%s: %d columns, %d rows; want 2 and %d", c.query, cols, rows, c.rows)
		}
	}
}

func TestAQueryOnAWritableSourceIsAQuery(t *testing.T) {
	a := writable(t)
	wantCount(t, a, "UPDATE people SET name = name", 3)
	if cols, rows, _, _ := run(t, a, "SELECT id, name FROM people"); cols != 2 || rows != 3 {
		t.Errorf("%d columns, %d rows", cols, rows)
	}
	if cols, rows, _, _ := run(t, a, "EXPLAIN SELECT id FROM people"); cols == 0 || rows == 0 {
		t.Errorf("EXPLAIN: %d columns, %d rows", cols, rows)
	}
}

// readAll runs query and returns its columns and rows.
func readAll(t *testing.T, a adapter.Adapter, query string) ([]adapter.Column, [][]any) {
	t.Helper()
	rows, err := a.Query(t.Context(), query)
	if err != nil {
		t.Fatalf("%s: %v", query, err)
	}
	defer rows.Close()
	var out [][]any
	for rows.Next() {
		out = append(out, append([]any(nil), rows.Values()...))
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("%s: %v", query, err)
	}
	return rows.Columns(), out
}

// The all_types table holds a row of every common type and a row of NULLs.
const (
	allTypesTable = `CREATE TABLE all_types (
		i integer, big bigint, n numeric(20,6), r real, d double precision,
		t text, v varchar(20), b boolean,
		ts timestamp, tz timestamptz, day date, tod time,
		j json, jb jsonb, raw bytea, u uuid, a integer[], ta text[], iv interval
	)`
	allTypesRow = `INSERT INTO all_types VALUES (
		1, 9007199254740993, 12345.678900, 0.1, 0.1, -- bounded: these are values in one row
		'text', 'var', true,
		'2026-11-01 18:30:05.5', '2026-11-01 18:30:05.5+01', '2026-11-01', '18:30:05',
		'{"z": 1.0000000000000001, "a": [1, 2]}', '{"z": 1, "a": [1, 2]}', '\xff0010', -- bounded: as above
		'a0eebc99-9c0b-4ef8-bb6d-6bb9bd380a11', '{1,2,NULL}', '{"a","b c"}', '1 day 2 hours'
	)`
	allTypesNulls = `INSERT INTO all_types (i) VALUES (NULL)`
)

func TestEveryCommonTypeReadsTheSameOnAWritableSource(t *testing.T) {
	roDSN := newDatabase(t)
	seed(t, roDSN, allTypesTable, allTypesRow, allTypesNulls)
	readOnly, err := postgres.Open(roDSN)
	if err != nil {
		t.Fatal(err)
	}
	defer readOnly.Close()

	wDSN := newDatabase(t)
	seed(t, wDSN, allTypesTable, allTypesRow, allTypesNulls)
	write, err := postgres.Open(wDSN, postgres.WithWrite())
	if err != nil {
		t.Fatal(err)
	}
	defer write.Close()

	const query = "SELECT * FROM all_types ORDER BY i NULLS LAST"
	wantCols, want := readAll(t, readOnly, query)
	gotCols, got := readAll(t, write, query)
	if !reflect.DeepEqual(gotCols, wantCols) {
		t.Errorf("columns = %+v, want %+v", gotCols, wantCols)
	}
	if len(got) != 2 || len(want) != 2 {
		t.Fatalf("%d and %d rows, want 2", len(got), len(want))
	}
	for r := range want {
		for c := range want[r] {
			name := wantCols[c].Name
			if !reflect.DeepEqual(got[r][c], want[r][c]) {
				t.Errorf("row %d %s: %#v, want %#v", r, name, got[r][c], want[r][c])
			}
			if g, w := export.CellText(got[r][c]), export.CellText(want[r][c]); g != w {
				t.Errorf("row %d %s: text %q, want %q", r, name, g, w)
			}
		}
	}
}

func TestReturningGivesWhatASelectGives(t *testing.T) {
	dsn := newDatabase(t)
	seed(t, dsn, allTypesTable, allTypesRow)
	a, err := postgres.Open(dsn, postgres.WithWrite())
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()

	_, selected := readAll(t, a, "SELECT * FROM all_types")
	_, returned := readAll(t, a, "UPDATE all_types SET i = i RETURNING *")
	if !reflect.DeepEqual(returned, selected) {
		t.Errorf("RETURNING gave %#v\nSELECT gave %#v", returned, selected)
	}
}

func TestCopyToTheClientEndsAndLeavesTheSourceWorking(t *testing.T) {
	a := writable(t)
	done := make(chan struct{})
	go func() {
		defer close(done)
		if rows, err := a.Query(t.Context(), "COPY people TO STDOUT"); err == nil {
			for rows.Next() {
			}
			rows.Close()
		}
	}()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("COPY to the client did not end")
	}
	wantCount(t, a, "UPDATE people SET name = name", 3)
}

func TestCopyFromTheClientEndsWhenCancelled(t *testing.T) {
	a := writable(t)
	wantCancelled(t, a, "COPY people FROM STDIN")
	if n := postgres.ConnectionsInUse(a); n != 0 {
		t.Errorf("%d connections in use after it", n)
	}
	wantCount(t, a, "UPDATE people SET name = name", 3)
}

func TestSeveralStatementsAreStillRefused(t *testing.T) {
	a := writable(t)
	_, err := a.Query(t.Context(), "UPDATE people SET name = name; UPDATE people SET name = name")
	if err == nil || !strings.Contains(err.Error(), "cannot insert multiple commands") {
		t.Errorf("got %v, want the server's refusal", err)
	}
	if n := postgres.ConnectionsInUse(a); n != 0 {
		t.Errorf("%d connections in use after the refusal", n)
	}
	wantCount(t, a, "UPDATE people SET name = name", 3)
}

// wantCancelled runs a statement the context ends while it runs.
func wantCancelled(t *testing.T, a *postgres.Adapter, query string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 200*time.Millisecond)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		rows, err := a.Query(ctx, query)
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
			t.Errorf("%s: expected the cancelled statement to fail", query)
		}
	case <-time.After(10 * time.Second):
		t.Fatalf("%s was not stopped by cancelling", query)
	}
}

func TestAWriteAndAReadCanBeCancelled(t *testing.T) {
	a := writable(t)
	wantCancelled(t, a, "UPDATE people SET name = pg_sleep(30)::text")
	if n := postgres.ConnectionsInUse(a); n != 0 {
		t.Errorf("%d connections in use after a cancelled write", n)
	}
	wantCancelled(t, a, "SELECT pg_sleep(30)")
	if n := postgres.ConnectionsInUse(a); n != 0 {
		t.Errorf("%d connections in use after a cancelled read", n)
	}
	wantCount(t, a, "UPDATE people SET name = name", 3)
	if cols, rows, _, _ := run(t, a, "SELECT name FROM people WHERE name <> ''"); cols != 1 || rows != 3 {
		t.Errorf("after cancelling: %d columns, %d rows", cols, rows)
	}
}

func TestAFailedWriteIsAnErrorAndLeavesTheNextOneRight(t *testing.T) {
	a := writable(t)
	for _, query := range []string{
		"UPDATE no_such_table SET a = 1",
		"INSERT INTO people VALUES (1, 'taken')",
	} {
		rows, err := a.Query(t.Context(), query)
		if err == nil {
			for rows.Next() {
			}
			err = rows.Err()
			rows.Close()
		}
		if err == nil {
			t.Errorf("%s: expected an error", query)
		}
		if n := postgres.ConnectionsInUse(a); n != 0 {
			t.Errorf("%s: %d connections still in use", query, n)
		}
	}
	wantCount(t, a, "UPDATE people SET name = name", 3)
}

func TestNoErrorContainsThePassword(t *testing.T) {
	const password = "hunter2"
	role := fmt.Sprintf("writer_%d", os.Getpid())
	t.Cleanup(func() { seed(t, adminDSN, "DROP ROLE IF EXISTS "+role) })
	dsn := newDatabase(t)
	seed(t, dsn, peopleFixture, peopleRows,
		"CREATE ROLE "+role+" LOGIN PASSWORD '"+password+"'",
		"GRANT SELECT, INSERT ON people TO "+role)
	own := strings.Replace(dsn, "postgres:postgres@", role+":"+password+"@", 1)
	if !strings.Contains(own, password) {
		adaptertest.NoServer(t, "LODE_TEST_POSTGRES_DSN", needsDefaultCredentials)
	}
	a, err := postgres.Open(own, postgres.WithWrite())
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()

	want := func(query string) {
		t.Helper()
		rows, err := a.Query(t.Context(), query)
		if err == nil {
			for rows.Next() {
			}
			err = rows.Err()
			rows.Close()
		}
		if err == nil {
			t.Errorf("%s: expected an error", query)
		} else if strings.Contains(err.Error(), password) {
			t.Errorf("%s: the error holds the password: %v", query, err)
		}
	}
	want("UPDATE no_such_table SET a = 1")
	want("UPDATE people SET name = 'denied'")
	want("INSERT INTO people VALUES (1, 'taken')")
	want("SELEKT nonsense")

	seed(t, dsn, "ALTER ROLE "+role+" NOLOGIN",
		"SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE usename = '"+role+"'")
	want("UPDATE people SET name = name")
}

func TestWritesDoNotLeakConnections(t *testing.T) {
	a := writable(t)
	for range 300 {
		wantCount(t, a, "UPDATE people SET name = name", 3)
		run(t, a, "SELECT * FROM people")
		wantNoCount(t, a, "SET search_path = public")
	}
	if n := postgres.ConnectionsInUse(a); n != 0 {
		t.Errorf("%d connections still in use", n)
	}
	if n := postgres.OpenConnections(a); n > 2 {
		t.Errorf("%d connections open after a long run, want them reused", n)
	}
}

// The contract checks each statement that opens a transaction is refused and holds nothing;
// this checks what a refusal says, and what the connection keeps through one.
func TestARefusedTransactionSaysWhyAndKeepsTheSession(t *testing.T) {
	a := writable(t)
	for _, query := range opensTransaction {
		if _, err := a.Query(t.Context(), query); err == nil || !strings.Contains(err.Error(), "from one run to the next") {
			t.Errorf("%s: got %v, want it refused saying why", query, err)
		}
	}

	wantNoCount(t, a, "COMMIT")
	wantNoCount(t, a, "ROLLBACK")
	if cols, rows, _, _ := run(t, a, "/* /* */ BEGIN */ SELECT 1"); cols != 1 || rows != 1 {
		t.Errorf("a BEGIN in a nested comment: %d columns, %d rows; want the SELECT", cols, rows)
	}

	wantNoCount(t, a, "CREATE TEMPORARY TABLE kept (i integer)")
	if _, err := a.Query(t.Context(), "BEGIN"); err == nil {
		t.Fatal("BEGIN was not refused")
	}
	if cols, _, _, _ := run(t, a, "SELECT i FROM kept"); cols != 1 {
		t.Errorf("%d columns from the temporary table, want it still there", cols)
	}
}

func TestAReadOnlySourceDoesNotRefuseATransaction(t *testing.T) {
	dsn := newDatabase(t)
	seed(t, dsn, peopleFixture, peopleRows)
	a, err := postgres.Open(dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	if _, _, n, known := run(t, a, "BEGIN"); known {
		t.Errorf("BEGIN reports %d rows affected", n)
	}
}

func TestAReadOnlySourceReportsNoCountAndStillRefuses(t *testing.T) {
	dsn := newDatabase(t)
	seed(t, dsn, peopleFixture, peopleRows)
	a, err := postgres.Open(dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	if _, _, n, known := run(t, a, "SELECT * FROM people"); known {
		t.Errorf("a read reports %d rows affected", n)
	}
	rows, err := a.Query(t.Context(), "UPDATE people SET name = 'x'")
	if err == nil {
		for rows.Next() {
		}
		err = rows.Err()
		rows.Close()
	}
	if err == nil {
		t.Error("a read-only Source took a write")
	}
}
