package tui

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"testing"
)

// writesTo is a Model on a fake Adapter whose Query answers as a statement with no columns
// does, with the given count.
func writesTo(t *testing.T, text string, affected int64, known bool) (Model, *fakeAdapter) {
	t.Helper()
	m, a, _ := runner(t, text, 0)
	a.write = &writeAnswer{affected: affected, known: known}
	return m, a
}

func TestAWriteSaysWhatItDid(t *testing.T) {
	cases := []struct {
		name     string
		affected int64
		known    bool
		want     string
	}{
		{"several rows", 3, true, `^3 rows affected in \d+`},
		{"one row reads properly", 1, true, `^1 row affected in \d+`},
		{"none, so a WHERE that matched nothing shows", 0, true, `^0 rows affected in \d+`},
		{"no count, so no number is invented", 0, false, `^done in \d+`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m, _ := writesTo(t, `UPDATE t SET a = 1`, c.affected, c.known)
			m = pressKey(t, m, ctrlR)
			if !regexp.MustCompile(c.want).MatchString(m.status) {
				t.Errorf("status = %q, want %s", m.status, c.want)
			}
		})
	}
}

func TestAWriteClearsTheGridAndTheHeldResultAsARunDoes(t *testing.T) {
	m, a, _ := runner(t, `SELECT 1`, 0)
	m = pressKey(t, m, ctrlR)
	if !strings.Contains(plain(m), "src") {
		t.Fatalf("setup: the result is not on screen:\n%s", plain(m))
	}
	a.write = &writeAnswer{affected: 3, known: true}
	m = pressKey(t, m, ctrlR)
	if strings.Contains(plain(m), "src") {
		t.Errorf("the rows are still on screen after a write:\n%s", plain(m))
	}
	m, _ = send(t, m, tab)
	if m, _ = send(t, m, keyC); m.status != "no rows to copy" {
		t.Errorf("status = %q; the held result should have been replaced by the write's", m.status)
	}
}

func TestAResultWithColumnsIsAQueryWhatever(t *testing.T) {
	m, a, _ := runner(t, `INSERT INTO t VALUES (1) RETURNING id`, 0)
	a.answerKnown, a.answerCount = true, 1
	m = pressKey(t, m, ctrlR)
	if !regexp.MustCompile(`^1 rows in \d+`).MatchString(m.status) {
		t.Errorf("status = %q; the message for a read does not change", m.status)
	}
	if !strings.Contains(plain(m), "src") {
		t.Errorf("its rows are not on screen:\n%s", plain(m))
	}
}

func TestAWriteKeepsThePosition(t *testing.T) {
	m, _ := writesTo(t, `UPDATE t SET a = 1; SELECT 2`, 3, true)
	m = pressKey(t, m, ctrlR)
	if !regexp.MustCompile(`^3 rows affected in \d+\S* · statement 1 of 2$`).MatchString(m.status) {
		t.Errorf("status = %q", m.status)
	}
	m, _ = writesTo(t, `CREATE TABLE t (i INT); SELECT 2`, 0, false)
	m = pressKey(t, m, ctrlR)
	if !regexp.MustCompile(`^done in \d+\S* · statement 1 of 2$`).MatchString(m.status) {
		t.Errorf("status = %q", m.status)
	}
}

func TestAFailedOrCancelledWriteSaysWhatItSaidBefore(t *testing.T) {
	m, a := writesTo(t, `UPDATE t SET a = 1`, 3, true)
	a.err = errors.New("no such table")
	if m = pressKey(t, m, ctrlR); m.status != "no such table" || !m.isError {
		t.Errorf("status = %q, error %v", m.status, m.isError)
	}
	m, a = writesTo(t, `UPDATE t SET a = 1`, 3, true)
	a.err = context.Canceled
	if m = pressKey(t, m, ctrlR); m.status != "cancelled" || m.isError {
		t.Errorf("status = %q, error %v", m.status, m.isError)
	}
}

func TestAWriteThatFailsWhileBeingReadReportsTheFailure(t *testing.T) {
	m, a := writesTo(t, `UPDATE t SET a = 1`, 3, true)
	a.write.err = errors.New("disk full")
	if m = pressKey(t, m, ctrlR); !strings.HasPrefix(m.status, "disk full") || !m.isError {
		t.Errorf("status = %q, error %v", m.status, m.isError)
	}
}

func TestAReadStillSaysItsRows(t *testing.T) {
	m, _, _ := runner(t, `SELECT 1`, 0)
	m = pressKey(t, m, ctrlR)
	if !regexp.MustCompile(`^1 rows in \d+`).MatchString(m.status) {
		t.Errorf("status = %q", m.status)
	}
}
