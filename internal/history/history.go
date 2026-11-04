// Package history records the queries that were run, across Sources, between sessions, in
// an owner-only file.
package history

import (
	"bufio"
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/EdwardWhalley/lode/internal/sourcetext"
)

// Max is how many entries are kept. The oldest are dropped.
const Max = 1000

// Entry is one recorded query.
type Entry struct {
	Time   time.Time `json:"time"`
	Source string    `json:"source"`
	SQL    string    `json:"sql"`
}

// DefaultPath is $XDG_STATE_HOME/lode/history, else ~/.local/state/lode/history, on Windows
// too.
func DefaultPath() (string, error) {
	if dir := os.Getenv("XDG_STATE_HOME"); dir != "" {
		return filepath.Join(dir, "lode", "history"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".local", "state", "lode", "history"), nil
}

// Store is the history file and the entries loaded from it.
type Store struct {
	path    string
	mu      sync.Mutex
	entries []Entry
}

// Open loads the history at path, trimming it to the newest Max entries and masking any DSN
// recorded as a Source, and rewrites the file if that changed it.
func Open(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	s := &Store{path: path}

	entries, changed, err := load(path)
	if err != nil {
		return nil, err
	}
	s.entries = entries
	if changed {
		if err := s.rewrite(); err != nil {
			return nil, err
		}
	}
	return s, nil
}

// load reads the entries at path, the newest Max, and says whether they differ from what the
// file holds.
func load(path string) (entries []Entry, changed bool, err error) {
	f, err := os.Open(path)
	switch {
	case os.IsNotExist(err):
		return nil, false, nil
	case err != nil:
		return nil, false, err
	}
	defer f.Close()

	lines := 0
	masked := false
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	for sc.Scan() {
		lines++
		var e Entry
		if json.Unmarshal(sc.Bytes(), &e) != nil || e.SQL == "" {
			continue
		}
		if safe := maskSource(e.Source); safe != e.Source {
			e.Source, masked = safe, true
		}
		entries = appendEntry(entries, e)
	}
	if err := sc.Err(); err != nil {
		return nil, false, err
	}
	if len(entries) > Max {
		entries = entries[len(entries)-Max:]
	}
	return entries, masked || lines != len(entries), nil
}

// maskSource is what an entry's Source should say: an ad-hoc Source is recorded by its Label,
// which once showed a DSN's options, so one recorded then would hold them on disk and no longer
// match. A name the form or the writer accepts can hold no ":", so it is never touched.
func maskSource(source string) string {
	if sourcetext.LooksLikeDSN(source) {
		return sourcetext.MaskDSN(source)
	}
	if lostItsSlashes(source) {
		return sourcetext.MaskDSN(strings.Replace(source, ":", "://", 1))
	}
	return source
}

// lostItsSlashes reports a Label such as "postgres:?password=x": net/url printed a DSN with no
// host, path or user without its "//".
func lostItsSlashes(source string) bool {
	u, err := url.Parse(source)
	return err == nil && u.Scheme != "" && u.Opaque == "" && u.Host == "" && u.Path == "" && u.User == nil
}

// appendEntry adds e unless it repeats the last entry.
func appendEntry(es []Entry, e Entry) []Entry {
	if n := len(es); n > 0 && es[n-1].Source == e.Source && es[n-1].SQL == e.SQL {
		return es
	}
	return append(es, e)
}

// rewrite replaces the file with the entries held, through a temporary file.
func (s *Store) rewrite() error {
	tmp, err := os.CreateTemp(filepath.Dir(s.path), "history-*.tmp")
	if err != nil {
		return err
	}
	name := tmp.Name()
	w := bufio.NewWriter(tmp)
	for _, e := range s.entries {
		b, _ := json.Marshal(e)
		w.Write(append(b, '\n'))
	}
	err = w.Flush()
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Chmod(name, 0o600)
	}
	if err == nil {
		err = os.Rename(name, s.path)
	}
	if err != nil {
		os.Remove(name)
	}
	return err
}

// Add records a query, unless it repeats the one before it for the same Source.
func (s *Store) Add(source, sql string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	e := Entry{Time: time.Now().UTC(), Source: source, SQL: sql}
	if n := len(s.entries); n > 0 && s.entries[n-1].Source == source && s.entries[n-1].SQL == sql {
		return nil
	}
	b, err := json.Marshal(e)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(s.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	_, err = f.Write(append(b, '\n'))
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return err
	}
	s.entries = append(s.entries, e)
	if len(s.entries) > Max {
		s.entries = s.entries[len(s.entries)-Max:]
	}
	return nil
}

// Entries returns every entry, oldest first.
func (s *Store) Entries() []Entry {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Entry(nil), s.entries...)
}

// ForSource returns the entries recorded for one Source, oldest first.
func (s *Store) ForSource(source string) []Entry {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []Entry
	for _, e := range s.entries {
		if e.Source == source {
			out = append(out, e)
		}
	}
	return out
}
