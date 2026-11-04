package sqlite_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/EdwardWhalley/lode/internal/adapter"
	"github.com/EdwardWhalley/lode/internal/adapter/sqlite"
)

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
	a := openWritable(t)
	defer a.Close()
	wantCount(t, a, "INSERT INTO animals VALUES ('ant'), ('bee'), ('cat')", 3)
	wantCount(t, a, "UPDATE animals SET kind = kind || '!'", 3)
	wantCount(t, a, "UPDATE animals SET kind = 'one' WHERE kind = 'ant!'", 1)
	wantCount(t, a, "UPDATE animals SET kind = 'x' WHERE kind = 'nothing like this'", 0)
	wantCount(t, a, "DELETE FROM animals", 3)
	wantCount(t, a, "REPLACE INTO animals VALUES ('dog')", 1)
}

func TestTheCountIsWhatTheEngineSays(t *testing.T) {
	a := openWritable(t)
	defer a.Close()
	wantNoCount(t, a, "CREATE TABLE log (who TEXT)")
	wantNoCount(t, a, "CREATE TRIGGER noted AFTER UPDATE ON people BEGIN INSERT INTO log VALUES (new.name); END")
	wantCount(t, a, "UPDATE people SET name = name", 3)
}

func TestTheFirstWordIsReadPastCommentsAndCase(t *testing.T) {
	a := openWritable(t)
	defer a.Close()
	wantCount(t, a, "-- a note\n  update people set name = name", 3)
	wantCount(t, a, "/* a note */ UPDATE people SET name = name;", 3)
	wantCount(t, a, "WITH c AS (SELECT 1) UPDATE people SET name = name", 3)
}

func TestAStatementWithNoCountSaysSoStraightAfterAWrite(t *testing.T) {
	a := openWritable(t)
	defer a.Close()
	wantCount(t, a, "UPDATE people SET name = name", 3)
	wantNoCount(t, a, "CREATE TABLE fresh (i INTEGER)")
	wantNoCount(t, a, "DROP TABLE fresh")
	wantNoCount(t, a, "CREATE INDEX i ON people (name)")
	wantNoCount(t, a, "PRAGMA user_version = 1")
	wantNoCount(t, a, "BEGIN; COMMIT")
}

// The contract checks each statement that opens a transaction is refused and holds nothing;
// this checks what a refusal says, and that a refused script changes nothing.
func TestARefusedTransactionSaysWhyAndChangesNothing(t *testing.T) {
	path := makeDB(t)
	a, err := sqlite.Open(path, sqlite.WithWrite())
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	other, err := sqlite.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	changed := func(name string) int {
		t.Helper()
		_, rows, _, _ := run(t, other, "SELECT id FROM people WHERE name = '"+name+"'")
		return rows
	}

	for _, tt := range []struct {
		query, says, name string
	}{
		{"BEGIN", "from one run to the next", ""},
		{"BEGIN; UPDATE people SET name = 'lost'", "from one run to the next", "lost"},
		{"SAVEPOINT s; UPDATE people SET name = 'undone'", "from one run to the next", "undone"},
		{"BEGIN; UPDATE no_such_table SET a = 1; COMMIT", "no such table", ""},
	} {
		_, err := a.Query(t.Context(), tt.query)
		if err == nil || !strings.Contains(err.Error(), tt.says) {
			t.Errorf("%s: got %v, want %q", tt.query, err, tt.says)
		}
		if tt.name != "" {
			if n := changed(tt.name); n != 0 {
				t.Errorf("%s: %d rows kept", tt.query, n)
			}
		}
	}

	wantNoCount(t, a, "BEGIN; UPDATE people SET name = 'script'; COMMIT")
	if n := changed("script"); n != 3 {
		t.Errorf("a whole script changed %d rows, want 3", n)
	}
}

func TestAReadOnlySourceDoesNotRefuseATransaction(t *testing.T) {
	a, err := sqlite.Open(makeDB(t))
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	if _, _, n, known := run(t, a, "BEGIN"); known {
		t.Errorf("BEGIN reports %d rows affected", n)
	}
}

func TestAScriptReportsItsLastStatement(t *testing.T) {
	a := openWritable(t)
	defer a.Close()
	wantCount(t, a, "CREATE TABLE s (i INTEGER); INSERT INTO s VALUES (1), (2)", 2)
	wantNoCount(t, a, "UPDATE s SET i = 3; CREATE TABLE s2 (i INTEGER)")
	wantCount(t, a, "UPDATE s SET i = 4 -- a note\n; -- and one after\n", 2)
}

func TestReturningIsAQuery(t *testing.T) {
	a := openWritable(t)
	defer a.Close()
	cols, rows, _, known := run(t, a, "UPDATE people SET name = name || '!' RETURNING id, name")
	if cols != 2 || rows != 3 {
		t.Errorf("%d columns, %d rows; want 2 and 3", cols, rows)
	}
	if known {
		t.Error("a statement that returns rows is a query, and reports no count")
	}
}

func TestAQueryOnAWritableSourceIsAQuery(t *testing.T) {
	a := openWritable(t)
	defer a.Close()
	wantCount(t, a, "UPDATE people SET name = name", 3)
	cols, rows, _, known := run(t, a, "SELECT id, name FROM people")
	if cols != 2 || rows != 3 || known {
		t.Errorf("%d columns, %d rows, known %v", cols, rows, known)
	}
}

func TestAFailedWriteIsAnErrorAndLeavesTheNextOneRight(t *testing.T) {
	a := openWritable(t)
	defer a.Close()
	if rows, err := a.Query(t.Context(), "UPDATE no_such_table SET a = 1"); err == nil {
		for rows.Next() {
		}
		err = rows.Err()
		rows.Close()
		if err == nil {
			t.Error("expected an error")
		}
	}
	wantCount(t, a, "UPDATE people SET name = name", 3)
	if n := sqlite.ConnectionsInUse(a); n != 0 {
		t.Errorf("%d connections still in use after a failed write", n)
	}
}

func TestACancelledWriteIsAnErrorAndLeavesNoConnectionBehind(t *testing.T) {
	a := openWritable(t)
	defer a.Close()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	timer := time.AfterFunc(100*time.Millisecond, cancel)
	defer timer.Stop()
	rows, err := a.Query(ctx, "WITH RECURSIVE c(x) AS (SELECT 1 UNION ALL SELECT x + 1 FROM c) DELETE FROM people WHERE id IN (SELECT x FROM c)")
	if err == nil {
		for rows.Next() {
		}
		err = rows.Err()
		rows.Close()
	}
	if !errors.Is(err, context.Canceled) && (err == nil || !strings.Contains(err.Error(), "interrupted")) {
		t.Fatalf("got %v, want the cancellation", err)
	}
	if n := sqlite.ConnectionsInUse(a); n != 0 {
		t.Errorf("%d connections still in use after a cancelled write", n)
	}
	wantCount(t, a, "UPDATE people SET name = name", 3)
}

func TestWritesDoNotLeakConnections(t *testing.T) {
	a := openWritable(t)
	defer a.Close()
	for range 500 {
		wantCount(t, a, "UPDATE people SET name = name", 3)
		run(t, a, "SELECT * FROM people")
		wantNoCount(t, a, "PRAGMA user_version = 1")
	}
	if n := sqlite.ConnectionsInUse(a); n != 0 {
		t.Errorf("%d connections still in use", n)
	}
	if n := sqlite.OpenConnections(a); n > 2 {
		t.Errorf("%d connections open after a long run, want them reused", n)
	}
}

func TestClosingRowsTwiceIsSafe(t *testing.T) {
	a := openWritable(t)
	defer a.Close()
	rows, err := a.Query(t.Context(), "SELECT * FROM people")
	if err != nil {
		t.Fatal(err)
	}
	if n := sqlite.ConnectionsInUse(a); n != 1 {
		t.Errorf("%d connections in use while rows are open, want 1", n)
	}
	rows.Close()
	rows.Close()
	if n := sqlite.ConnectionsInUse(a); n != 0 {
		t.Errorf("%d connections in use after closing", n)
	}
}

func TestAReadOnlySourceStillRefusesAndReportsNoCount(t *testing.T) {
	a, err := sqlite.Open(makeDB(t))
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
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
	if _, _, n, known := run(t, a, "SELECT * FROM people"); known {
		t.Errorf("a read reports %d rows affected", n)
	}
}
