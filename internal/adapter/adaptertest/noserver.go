// What a server-backed test does when its server is not configured: skip on a laptop, fail
// in the full suite.

package adaptertest

import (
	"fmt"
	"os"
	"testing"
)

// RequireServers names the variable that makes a missing server a failure. The full suite
// (scripts/test-all.*) sets it, so an Adapter whose server never ran cannot pass there.
const RequireServers = "LODE_REQUIRE_SERVERS"

// NoServer ends a test whose server is not configured, or is not one it can use. envVar is
// the variable that names the server and how says what to do about it. The test is skipped
// with that message, or fails with it if LODE_REQUIRE_SERVERS is set. Every server-backed
// test skips through here, never with t.Skip, and internal/guard holds that.
func NoServer(t testing.TB, envVar, how string) {
	t.Helper()
	msg := fmt.Sprintf("needs %s: %s", envVar, how)
	if os.Getenv(RequireServers) != "" {
		t.Fatalf("%s (%s is set, so this fails instead of skipping)", msg, RequireServers)
		return
	}
	t.Skip(msg)
}
