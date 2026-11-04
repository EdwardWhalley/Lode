package mysql_test

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/EdwardWhalley/lode/internal/adapter"
	"github.com/EdwardWhalley/lode/internal/adapter/mysql"
)

func openWritable(t *testing.T, s server) *mysql.Adapter {
	t.Helper()
	dsn := s.newDatabase(t)
	s.seed(t, databaseOf(dsn), peopleFixture, peopleRows)
	a, err := mysql.Open(dsn, mysql.WithWrite())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { a.Close() })
	return a
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
	eachServer(t, func(t *testing.T, s server) {
		a := openWritable(t, s)
		wantCount(t, a, "INSERT INTO people VALUES (4, 'dave'), (5, 'erin'), (6, 'finn')", 3)
		wantCount(t, a, "UPDATE people SET name = CONCAT(name, '!') WHERE id > 3", 3)
		wantCount(t, a, "UPDATE people SET name = 'one' WHERE id = 4", 1)
		wantCount(t, a, "UPDATE people SET name = 'x' WHERE id = -1", 0)
		wantCount(t, a, "INSERT INTO people SELECT id + 10, name FROM people WHERE id < 3", 2)
		wantCount(t, a, "DELETE FROM people WHERE id > 3", 5)
	})
}

func TestTheCountIsOfRowsChanged(t *testing.T) {
	eachServer(t, func(t *testing.T, s server) {
		a := openWritable(t, s)
		wantCount(t, a, "UPDATE people SET name = name", 0)
		wantCount(t, a, "REPLACE INTO people VALUES (1, 'alicia')", 2)
		wantCount(t, a, "INSERT INTO people VALUES (1, 'al') ON DUPLICATE KEY UPDATE name = 'al'", 2)
		wantCount(t, a, "INSERT INTO people VALUES (1, 'al') ON DUPLICATE KEY UPDATE name = 'al'", 0)

		wantNoCount(t, a, "ALTER TABLE people ADD UNIQUE KEY by_name (name)")
		wantCount(t, a, "REPLACE INTO people VALUES (1, 'bob')", 3)
	})
}

func TestTheFirstWordIsReadPastCommentsAndCase(t *testing.T) {
	eachServer(t, func(t *testing.T, s server) {
		a := openWritable(t, s)
		wantCount(t, a, "-- a note\n  update people set id = id + 100", 3)
		wantCount(t, a, "# a note\nUPDATE people SET id = id + 100", 3)
		wantCount(t, a, "/* a note */ UPDATE people SET id = id + 100;", 3)
	})
}

func TestStatementsWithNoCountSaySoStraightAfterAWrite(t *testing.T) {
	eachServer(t, func(t *testing.T, s server) {
		a := openWritable(t, s)
		for _, query := range []string{
			"CREATE TABLE other (i INT)",
			"ALTER TABLE other ADD COLUMN j INT",
			"TRUNCATE TABLE other",
			"DROP TABLE other",
			"CREATE TABLE copied AS SELECT * FROM people",
			"ALTER TABLE copied ALGORITHM=COPY, ADD COLUMN j INT",
			"SELECT 1 INTO @x",
			"WITH c AS (SELECT 1 AS i) SELECT i INTO @x FROM c",
			"SET @a = 1",
			"DO 1",
			"COMMIT",
		} {
			wantCount(t, a, "UPDATE people SET id = id + 100", 3)
			wantNoCount(t, a, query)
		}
	})
}

func TestAQueryOnAWritableSourceIsAQuery(t *testing.T) {
	eachServer(t, func(t *testing.T, s server) {
		a := openWritable(t, s)
		for _, q := range []struct {
			query      string
			cols, rows int
		}{
			{"SELECT id, name FROM people", 2, 3},
			{"SELECT id FROM people WHERE id = -1", 1, 0},
			{"SHOW TABLES", 1, 1},
		} {
			cols, rows, n, known := run(t, a, q.query)
			if cols != q.cols || rows != q.rows || known {
				t.Errorf("%s: %d columns, %d rows, count %d known %v", q.query, cols, rows, n, known)
			}
		}
	})
}

func TestWhatOnlyOneServerAllows(t *testing.T) {
	eachServer(t, func(t *testing.T, s server) {
		a := openWritable(t, s)
		const with = "WITH c AS (SELECT 1 AS i) UPDATE people SET id = id + 100"
		const returning = "INSERT INTO people VALUES (4, 'dave') RETURNING id"
		refused := returning
		if s.name == "MySQL" {
			wantCount(t, a, with, 3)
		} else {
			cols, rows, _, known := run(t, a, returning)
			if cols != 1 || rows != 1 || known {
				t.Errorf("%s: %d columns, %d rows, count known %v; want a query", returning, cols, rows, known)
			}
			refused = with
		}
		if _, err := a.Query(t.Context(), refused); err == nil {
			t.Errorf("%s: expected the server to refuse it", refused)
		}
		if n := mysql.ConnectionsInUse(a); n != 0 {
			t.Errorf("%d connections in use after a refusal", n)
		}
	})
}

func TestSeveralStatementsAreRefusedAndRunNothing(t *testing.T) {
	eachServer(t, func(t *testing.T, s server) {
		a := openWritable(t, s)
		_, err := a.Query(t.Context(), "UPDATE people SET name = 'changed'; UPDATE people SET name = 'again'")
		if err == nil || !strings.Contains(err.Error(), "1064") {
			t.Errorf("got %v, want the server's syntax error", err)
		}
		if n := mysql.ConnectionsInUse(a); n != 0 {
			t.Errorf("%d connections in use after the refusal", n)
		}
		if _, rows, _, _ := run(t, a, "SELECT name FROM people WHERE name IN ('changed', 'again')"); rows != 0 {
			t.Errorf("%d rows were changed by a refused text", rows)
		}
	})
}

// The contract checks each statement that would leave a transaction open is refused and
// holds nothing; this checks what a refusal says, what is not refused, and what runs and
// is then cleared.
func TestATransactionLeftOpenIsRefusedOrCleared(t *testing.T) {
	eachServer(t, func(t *testing.T, s server) {
		dsn := s.newDatabase(t)
		db := databaseOf(dsn)
		s.seed(t, db, peopleFixture, peopleRows)
		a, err := mysql.Open(dsn, mysql.WithWrite())
		if err != nil {
			t.Fatal(err)
		}
		defer a.Close()
		for _, query := range opensTransaction {
			_, err := a.Query(t.Context(), query)
			if err == nil || !strings.Contains(err.Error(), "from one run to the next") {
				t.Errorf("%s: got %v, want it refused saying why", query, err)
			}
		}

		for _, query := range []string{
			"SET autocommit = 1",
			"SET autocommit = ON",
			"SET autocommit = 'ON'",
			"SET autocommit = DEFAULT",
			"SET autocommit = (1)",
			"SET @autocommit = 0",
			"SET @saved = @@autocommit",
			"SET @a = 'autocommit = 0'",
			"COMMIT",
			"ROLLBACK",
		} {
			wantNoCount(t, a, query)
			wantSeenAtOnce(t, s, a, db)
		}

		s.seed(t, db, "CREATE PROCEDURE xa_open() BEGIN XA START 'lode_test'; END")
		leaves := []string{
			"/*! BEGIN */",
			"/*!50000 SET autocommit = 0 */",
			"/*!SET autocommit = 0, @y = (*/SELECT 1/*!)*/",
			"LOCK TABLES people WRITE",
			"CALL xa_open()",
			"/*! XA START 'lode_test' */",
		}
		if s.name == "MariaDB" {
			leaves = append(leaves, "EXECUTE IMMEDIATE 'BEGIN'", "BEGIN NOT ATOMIC START TRANSACTION; SELECT 1 INTO @x; END")

			if cols, rows, _, _ := run(t, a, "SET STATEMENT max_statement_time = 100 FOR SELECT id FROM people"); cols != 1 || rows != 3 {
				t.Errorf("SET STATEMENT ... FOR SELECT: %d columns, %d rows; want its rows", cols, rows)
			}
		}
		for _, query := range leaves {
			run(t, a, query)
			wantSeenAtOnce(t, s, a, db)
		}

		s.seed(t, db,
			"CREATE PROCEDURE leaves_open() BEGIN START TRANSACTION; UPDATE people SET name = 'undone'; END",
			"CREATE PROCEDURE leaves_autocommit_off() BEGIN SET autocommit = 0; UPDATE people SET name = 'undone'; END")
		for _, query := range []string{"CALL leaves_open()", "CALL leaves_autocommit_off()"} {
			run(t, a, query)
			var undone int
			if err := s.admin(t, db).QueryRow("SELECT count(*) FROM people WHERE name = 'undone'").Scan(&undone); err != nil {
				t.Fatal(err)
			}
			if undone != 0 {
				t.Errorf("%s: %d rows kept from what it left open, want them undone", query, undone)
			}
			wantSeenAtOnce(t, s, a, db)
		}
	})
}

func TestAConnectionWithAutocommitOffDoesNotOpenForWriting(t *testing.T) {
	eachServer(t, func(t *testing.T, s server) {
		dsn := s.newDatabase(t) + "?autocommit=0"
		if a, err := mysql.Open(dsn, mysql.WithWrite()); err == nil {
			a.Close()
			t.Error("it opened for writing")
		} else if !strings.Contains(err.Error(), "autocommit") {
			t.Errorf("got %v, want it to say why", err)
		}
		a, err := mysql.Open(dsn)
		if err != nil {
			t.Fatalf("it should still open read-only: %v", err)
		}
		a.Close()
	})
}

// wantSeenAtOnce checks a run left nothing behind: another connection sees the write at
// once.
func wantSeenAtOnce(t *testing.T, s server, a *mysql.Adapter, db string) {
	t.Helper()
	seenWrites++
	name := fmt.Sprintf("seen%d", seenWrites)
	wantCount(t, a, "UPDATE people SET name = '"+name+"'", 3)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	var n int
	if err := s.admin(t, db).QueryRowContext(ctx, "SELECT count(*) FROM people WHERE name = ?", name).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 3 {
		t.Errorf("another connection sees %d changed rows, want 3", n)
	}
}

var seenWrites int

func TestAReadOnlySourceDoesNotRefuseATransaction(t *testing.T) {
	eachServer(t, func(t *testing.T, s server) {
		dsn := s.newDatabase(t)
		s.seed(t, databaseOf(dsn), peopleFixture, peopleRows)
		a, err := mysql.Open(dsn)
		if err != nil {
			t.Fatal(err)
		}
		defer a.Close()
		if _, _, n, known := run(t, a, "BEGIN"); known {
			t.Errorf("BEGIN reports %d rows affected", n)
		}
		if got := count(t, a, "people"); got != "3" {
			t.Errorf("count = %s, want 3", got)
		}
	})
}

// wantCancelled runs a statement the context ends while it runs.
func wantCancelled(t *testing.T, a *mysql.Adapter, query string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 300*time.Millisecond)
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
	case <-time.After(15 * time.Second):
		t.Fatalf("%s was not stopped by cancelling", query)
	}
}

func TestCancellingAWriteKillsItOnTheServerAndGivesBackTheConnection(t *testing.T) {
	eachServer(t, func(t *testing.T, s server) {
		a := openWritable(t, s)
		const write = "UPDATE people SET name = SLEEP(120)"
		wantCancelled(t, a, write)
		if n := mysql.ConnectionsInUse(a); n != 0 {
			t.Errorf("%d connections in use after a cancelled write", n)
		}

		admin := s.admin(t, "")
		deadline := time.Now().Add(10 * time.Second)
		for {
			var n int
			if err := admin.QueryRow(`SELECT count(*) FROM information_schema.processlist WHERE info = ?`, write).Scan(&n); err != nil {
				t.Fatal(err)
			}
			if n == 0 {
				break
			}
			if time.Now().After(deadline) {
				t.Fatal("the cancelled write is still running on the server")
			}
			time.Sleep(200 * time.Millisecond)
		}

		if _, rows, _, _ := run(t, a, "SELECT id FROM people WHERE name IN ('alice', 'bob', 'carol')"); rows != 3 {
			t.Errorf("%d of 3 rows left as they were", rows)
		}
		wantCount(t, a, "UPDATE people SET id = id + 100", 3)
	})
}

func TestAReadOnAWritableSourceCanBeCancelled(t *testing.T) {
	eachServer(t, func(t *testing.T, s server) {
		a := openWritable(t, s)
		wantCancelled(t, a, "SELECT SLEEP(120)")
		if n := mysql.ConnectionsInUse(a); n != 0 {
			t.Errorf("%d connections in use after a cancelled read", n)
		}

		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		rows, err := a.Query(ctx, hugeQuery)
		if err != nil {
			t.Fatal(err)
		}
		for i := range 3 {
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
		if n := mysql.ConnectionsInUse(a); n != 0 {
			t.Errorf("%d connections in use after closing", n)
		}
	})
}

var failingWrites = []string{
	"UPDATE no_such_table SET a = 1",
	"INSERT INTO people VALUES (1, 'taken')",
	"SELEKT nonsense",
}

func TestAFailedWriteIsAnErrorAndLeavesTheNextOneRight(t *testing.T) {
	eachServer(t, func(t *testing.T, s server) {
		a := openWritable(t, s)
		for _, query := range failingWrites {
			if _, err := a.Query(t.Context(), query); err == nil {
				t.Errorf("%s: expected an error", query)
			}
			if n := mysql.ConnectionsInUse(a); n != 0 {
				t.Errorf("%s: %d connections still in use", query, n)
			}
		}
		wantCount(t, a, "UPDATE people SET id = id + 100", 3)
	})
}

func TestNoErrorContainsThePassword(t *testing.T) {
	eachServer(t, func(t *testing.T, s server) {
		a := openWritable(t, s)
		u, err := url.Parse(s.adminDSN)
		if err != nil {
			t.Fatal(err)
		}
		password, ok := u.User.Password()
		if !ok || password == "" {
			t.Fatalf("%s has no password to look for", s.envVar)
		}
		for _, query := range failingWrites {
			if _, err := a.Query(t.Context(), query); err == nil {
				t.Errorf("%s: expected an error", query)
			} else if strings.Contains(err.Error(), password) {
				t.Errorf("%s: the error holds the password", query)
			}
		}
	})
}

func TestWritesDoNotLeakConnections(t *testing.T) {
	eachServer(t, func(t *testing.T, s server) {
		a := openWritable(t, s)
		for range 100 {
			wantCount(t, a, "UPDATE people SET id = id + 100", 3)
			run(t, a, "SELECT * FROM people")
			wantNoCount(t, a, "SET @a = 1")
		}
		if n := mysql.ConnectionsInUse(a); n != 0 {
			t.Errorf("%d connections still in use", n)
		}
		if n := mysql.OpenConnections(a); n > 2 {
			t.Errorf("%d connections open after a long run, want them reused", n)
		}
	})
}
