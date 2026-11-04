package export

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("disk full") }

func TestAFailedWriteLeavesNoPartialFile(t *testing.T) {
	for _, name := range []string{"fail.csv", "fail.json"} {
		path := filepath.Join(t.TempDir(), name)
		target, err := Create(path)
		if err != nil {
			t.Fatal(err)
		}
		if err := target.writeTo(failingWriter{}, []string{"a"}, [][]any{{1}, {2}}); err == nil {
			t.Fatalf("%s: expected the write to fail", name)
		}
		if _, err := os.Stat(path); err == nil {
			t.Errorf("%s: a failed export left a partial file behind", name)
		}
	}
}
