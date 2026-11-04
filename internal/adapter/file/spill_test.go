package file

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"
)

func TestASortTooBigForMemoryStillFinishesUnderTheLock(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	path := filepath.Join(dir, "a.csv")
	if err := os.WriteFile(path, []byte("id\n1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("duckdb", "")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(`SET memory_limit = '60MB'`); err != nil {
		t.Fatal(err)
	}
	if err := lock(db, path); err != nil {
		t.Fatal(err)
	}

	var n int64
	err = db.QueryRow(`SELECT count(*) FROM (SELECT i, random() AS r FROM range(5000000) t(i) ORDER BY r OFFSET 1)`).Scan(&n)
	if err != nil {
		t.Fatalf("the sort failed under the lock: %v", err)
	}
	if n != 4999999 {
		t.Errorf("count = %d, want 4999999", n)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if entries, err := os.ReadDir(dir); err != nil || len(entries) != 1 {
		t.Errorf("left in the folder after closing: %v (%v), want only the file", entries, err)
	}
}
