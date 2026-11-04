package adaptertest_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/EdwardWhalley/lode/internal/adapter/adaptertest"
)

// recorder is a test that writes down how it was ended instead of ending.
type recorder struct {
	testing.TB
	skipped, failed string
}

func (r *recorder) Helper()                        {}
func (r *recorder) Skip(args ...any)               { r.skipped = fmt.Sprint(args...) }
func (r *recorder) Fatal(args ...any)              { r.failed = fmt.Sprint(args...) }
func (r *recorder) Fatalf(format string, a ...any) { r.failed = fmt.Sprintf(format, a...) }
func (r *recorder) Skipf(format string, a ...any)  { r.skipped = fmt.Sprintf(format, a...) }

const how = "set it to a mariadb:// DSN of an account that can create databases"

func TestAMissingServerSkipsOnALaptop(t *testing.T) {
	t.Setenv(adaptertest.RequireServers, "")
	r := &recorder{}
	adaptertest.NoServer(r, "LODE_TEST_MARIADB_DSN", how)
	if r.failed != "" {
		t.Fatalf("failed: %s", r.failed)
	}
	if !strings.Contains(r.skipped, "LODE_TEST_MARIADB_DSN") || !strings.Contains(r.skipped, how) {
		t.Errorf("skip message = %q, want the variable and what to do", r.skipped)
	}
}

func TestAMissingServerFailsTheFullSuite(t *testing.T) {
	t.Setenv(adaptertest.RequireServers, "1")
	r := &recorder{}
	adaptertest.NoServer(r, "LODE_TEST_MARIADB_DSN", how)
	if r.skipped != "" {
		t.Fatalf("skipped: %s", r.skipped)
	}
	for _, want := range []string{"LODE_TEST_MARIADB_DSN", how, "LODE_REQUIRE_SERVERS"} {
		if !strings.Contains(r.failed, want) {
			t.Errorf("failure = %q, want it to name %s", r.failed, want)
		}
	}
}
