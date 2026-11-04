package file_test

import (
	"context"
	"testing"
	"time"

	"github.com/EdwardWhalley/lode/internal/adapter/file"
)

// reported reads a statement out as the UI does and asks for the count before closing.
type reported struct {
	cols, rows int
	n          int64
	known      bool
}

func report(t *testing.T, a *file.Adapter, query string) reported {
	t.Helper()
	rows, err := a.Query(t.Context(), query)
	if err != nil {
		t.Fatalf("%s: %v", query, err)
	}
	defer rows.Close()
	var got reported
	for rows.Next() {
		got.rows++
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("%s: %v", query, err)
	}
	got.cols = len(rows.Columns())
	got.n, got.known = rows.RowsAffected()
	return got
}

func wantAffected(t *testing.T, a *file.Adapter, query string, want int64) {
	t.Helper()
	if got := report(t, a, query); got != (reported{n: want, known: true}) {
		t.Errorf("%s reports %+v, want %d rows affected and no columns or rows", query, got, want)
	}
}

func wantDone(t *testing.T, a *file.Adapter, query string) {
	t.Helper()
	if got := report(t, a, query); got != (reported{}) {
		t.Errorf("%s reports %+v, want no columns, no rows and no count", query, got)
	}
}

func TestWritesToAnInMemoryTableReportWhatTheyChanged(t *testing.T) {
	a := openLimited(t, 0)
	wantDone(t, a, `CREATE TABLE t (i INT)`)
	wantAffected(t, a, `INSERT INTO t VALUES (1), (2), (3)`, 3)
	wantAffected(t, a, `UPDATE t SET i = 9`, 3)
	wantAffected(t, a, `UPDATE t SET i = 8 WHERE i = 0`, 0)
	wantAffected(t, a, `DELETE FROM t WHERE i = 0`, 0)
	wantAffected(t, a, `DELETE FROM t`, 3)
}

func TestStatementsWithNoCountSayDone(t *testing.T) {
	a := openLimited(t, 0)
	for _, query := range []string{
		`CREATE TABLE t (i INT)`,
		`CREATE TABLE u AS SELECT 1 AS i`,
		`CREATE VIEW v AS SELECT 1 AS i`,
		`CREATE MACRO m() AS 1`,
		`ALTER TABLE t RENAME TO t2`,
		`DROP VIEW v`,
		`BEGIN`,
		`COMMIT`,
		`ANALYZE`,
		`VACUUM`,
		`SET VARIABLE x = 1`,
		`USE memory`,
		`CHECKPOINT`,
		`FORCE CHECKPOINT`,
		`PRAGMA disable_profiling`,
	} {
		wantDone(t, a, query)
	}
}

func TestAQueryThatLooksLikeACountIsAQuery(t *testing.T) {
	a := openLimited(t, 0)
	got := report(t, a, `SELECT 3 AS Count`)
	if got.cols != 1 || got.rows != 1 {
		t.Errorf("SELECT 3 AS Count reports %+v, want its one row", got)
	}
}

func TestReturningIsAQuery(t *testing.T) {
	a := openLimited(t, 0)
	wantDone(t, a, `CREATE TABLE t (i BIGINT)`)
	for _, c := range []struct {
		query string
		rows  int
	}{
		{`INSERT INTO t VALUES (1) RETURNING i`, 1},
		{`INSERT INTO t VALUES (2), (3) returning i`, 2},
		{`UPDATE t SET i = 9 RETURNING i`, 3},
		{`INSERT INTO t VALUES (5) RETURNING i AS "Count"`, 1},
		{"INSERT INTO t VALUES (6) -- a note\n RETURNING i", 1},
		{`DELETE FROM t RETURNING i`, 5},
	} {
		got := report(t, a, c.query)
		if got.cols != 1 || got.rows != c.rows {
			t.Errorf("%s reports %+v, want 1 column and %d rows", c.query, got, c.rows)
		}
	}
}

func TestReturningInACommentOrAStringIsNotReturning(t *testing.T) {
	a := openLimited(t, 0)
	wantDone(t, a, `CREATE TABLE t (s VARCHAR)`)
	wantAffected(t, a, `INSERT INTO t VALUES ('returning') -- returning`, 1)
	wantAffected(t, a, `/* returning */ UPDATE t SET s = 'returning'`, 1)
}

func TestSeveralStatementsReportTheLast(t *testing.T) {
	a := openLimited(t, 0)
	wantDone(t, a, `CREATE TABLE t (i INT)`)
	wantAffected(t, a, `INSERT INTO t VALUES (1); INSERT INTO t VALUES (2), (3);`, 2)
	wantDone(t, a, `UPDATE t SET i = 1; CREATE TABLE u (i INT)`)
	if got := report(t, a, `UPDATE t SET i = 1; SELECT i FROM t`); got.cols != 1 || got.rows != 3 {
		t.Errorf("a script ending in a query reports %+v", got)
	}
	wantAffected(t, a, `INSERT INTO t VALUES (9) RETURNING i; UPDATE t SET i = 2`, 4)
}

func TestRowsAreNotLostBehindANestedComment(t *testing.T) {
	a := openLimited(t, 0)
	wantDone(t, a, `CREATE TABLE c ("Count" BIGINT, b BIGINT)`)
	wantAffected(t, a, `INSERT INTO c VALUES (1, 100), (2, 100), (3, 100)`, 3)

	got := report(t, a, `/* /* */ ' */ UPDATE c SET "Count" = "Count" + 1 RETURNING b AS "Count"`)
	if got.cols != 1 || got.rows != 3 {
		t.Errorf("a hidden RETURNING reports %+v, want its 3 rows", got)
	}
	got = report(t, a, `/* /* */ ' */ UPDATE c SET b = 1 RETURNING "Count"`)
	if got.cols != 1 || got.rows != 3 {
		t.Errorf("a hidden RETURNING reports %+v, want its 3 rows", got)
	}
	wantAffected(t, a, `/* /* */ */ UPDATE c SET b = 2`, 3)
}

func TestReturningAsANameIsNotTheClause(t *testing.T) {
	a := openLimited(t, 0)
	wantDone(t, a, `CREATE TABLE r2 ("returning" INT, i INT)`)
	wantAffected(t, a, `INSERT INTO r2 VALUES (1, 1), (2, 2)`, 2)
	wantAffected(t, a, `UPDATE r2 SET i = r2.returning`, 2)
	wantAffected(t, a, `DELETE FROM r2 WHERE r2.returning = 2`, 1)
	wantAffected(t, a, `INSERT INTO r2 SELECT 1 AS returning, 2`, 1)
	wantAffected(t, a, `UPDATE r2 SET "returning" = 5 WHERE i = 2`, 1)
}

func TestAConstraintFailureIsAnErrorAndTheNextQueryRuns(t *testing.T) {
	a := openLimited(t, 0)
	wantDone(t, a, `CREATE TABLE p (i INT PRIMARY KEY)`)
	wantAffected(t, a, `INSERT INTO p VALUES (1)`, 1)
	if err := run(t, a, `INSERT INTO p VALUES (1)`); err == nil {
		t.Error("expected the repeated key to fail")
	}
	if got := report(t, a, `SELECT * FROM p`); got.rows != 1 {
		t.Errorf("after the failure: %+v", got)
	}
}

func TestACancelledWriteIsAnErrorAndTheNextQueryRuns(t *testing.T) {
	a := openLimited(t, 0)
	wantDone(t, a, `CREATE TABLE b (i BIGINT)`)
	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()
	// bounded: an aggregate of a cross join, so the only row it holds is the one it inserts.
	_, err := a.Query(ctx, `INSERT INTO b SELECT count(*) FROM range(1000000000) x, range(1000000000) y`)
	if err == nil {
		t.Fatal("expected the cancelled write to fail")
	}
	if got := report(t, a, `SELECT * FROM b`); got.rows != 0 {
		t.Errorf("after the cancelled write: %+v, want nothing written", got)
	}
}

func TestPragmaAndCallStayQueries(t *testing.T) {
	a := openLimited(t, 0)
	if got := report(t, a, `CALL pragma_version()`); got.cols == 0 || got.rows != 1 {
		t.Errorf("CALL pragma_version() reports %+v", got)
	}
}

func TestAFailedWriteIsAnError(t *testing.T) {
	a := openLimited(t, 0)
	if err := run(t, a, `UPDATE no_such_table SET i = 1`); err == nil {
		t.Error("expected an error")
	}
}

func TestTheOpenedFileCannotBeUpdated(t *testing.T) {
	dir, path := folder(t)
	before := disk(t, dir)
	a, err := file.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	if err := run(t, a, `UPDATE a SET id = 9`); err == nil {
		t.Error("expected an error updating the view")
	}
	if got := disk(t, dir); len(got) != len(before) || got["a.csv"] != before["a.csv"] {
		t.Errorf("the disk changed: %q", got)
	}
}
