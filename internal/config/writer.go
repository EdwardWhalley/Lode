// Changes sources.toml: adds, replaces and removes one section, checks the result and
// writes through a temporary file.

package config

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"

	"github.com/BurntSushi/toml"

	"github.com/EdwardWhalley/lode/internal/rowcap"
	"github.com/EdwardWhalley/lode/internal/source"
	"github.com/EdwardWhalley/lode/internal/sourcetext"
)

var (
	ErrStale       = errors.New("sources.toml changed since it was read")
	ErrExists      = errors.New("a Source with that name already exists")
	ErrNotFound    = errors.New("no Source with that name")
	ErrBadName     = sourcetext.ErrBadName
	ErrUnsupported = errors.New("the Source is written in a form lode does not edit")
	ErrUnverified  = errors.New("the new sources.toml would not be the old one plus the change")
)

// Section is one [sources.<name>] section as written in the file, with ${NAME} and ~/
// unexpanded.
type Section struct {
	Name    string
	Path    string
	DSN     string
	Write   bool
	MaxRows int
}

// Resolve turns the section into the Source Load would give for it in the file at
// configPath.
func (s Section) Resolve(configPath string, lookup Lookup) (source.Source, error) {
	out := source.Source{Name: s.Name, Write: s.Write, MaxRows: s.MaxRows}
	var err error
	out.Path, out.DSN, err = expandLocation(s.Path, s.DSN, filepath.Dir(configPath), lookup)
	return out, err
}

// Snapshot is what a caller last read of sources.toml; an operation refuses with ErrStale
// if the file has changed.
type Snapshot struct {
	data   []byte
	exists bool
}

// Read takes a Snapshot of path; a missing file is a Snapshot of nothing.
func Read(path string) (Snapshot, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return Snapshot{}, nil
	}
	if err != nil {
		return Snapshot{}, err
	}
	return Snapshot{data: data, exists: true}, nil
}

// Section is the Source called name as stored, or ErrNotFound or ErrUnsupported.
func (s Snapshot) Section(name string) (Section, error) {
	var f file
	if _, err := toml.Decode(string(s.data), &f); err != nil {
		return Section{}, syntaxError("sources.toml", err)
	}
	e, ok := f.Sources[name]
	if !ok {
		return Section{}, fmt.Errorf("%w: %s", ErrNotFound, name)
	}
	if _, found, many := findSection(scan(s.data), name); sourcetext.CheckName(name) != nil || !found || many {
		return Section{}, unsupported(name, "it is not a plain [sources."+name+"] section; edit it in sources.toml by hand")
	}
	out := Section{Name: name, Path: e.Path, DSN: e.DSN, Write: e.Write}
	if e.MaxRows != nil {
		out.MaxRows = *e.MaxRows
	}
	return out, nil
}

func (s Snapshot) same(o Snapshot) bool { return s.exists == o.exists && bytes.Equal(s.data, o.data) }

// Add appends a section for s.Name. It returns the Snapshot of the file it wrote.
func Add(path string, last Snapshot, s Section) (Snapshot, error) {
	return change(path, last, opAdd, s, "")
}

// Replace makes the section for s.Name say s, changing only the lines that differ.
func Replace(path string, last Snapshot, s Section) (Snapshot, error) {
	return change(path, last, opReplace, s, "")
}

// ReplaceAs is Replace for a Source also renamed: the section called from becomes s.Name,
// in one write.
func ReplaceAs(path string, last Snapshot, from string, s Section) (Snapshot, error) {
	if from == s.Name {
		from = ""
	}
	return change(path, last, opReplace, s, from)
}

func Remove(path string, last Snapshot, name string) (Snapshot, error) {
	return change(path, last, opRemove, Section{Name: name}, "")
}

type opKind int

const (
	opAdd opKind = iota
	opReplace
	opRemove
)

// rename puts the finished file over the original; a variable so a test can fail it.
var rename = os.Rename

// buildHook, if set, sees the new contents before they are checked; a test uses it.
var buildHook func([]byte) []byte

// anyValue answers every ${NAME}, so checking a file does not depend on the environment.
func anyValue(string) (string, bool) { return "x", true }

// change does one operation. from is the name a Replace renames the Source away from,
// or "".
func change(path string, last Snapshot, o opKind, s Section, from string) (Snapshot, error) {
	if from != "" && sourcetext.CheckName(from) != nil {
		return Snapshot{}, unsupported(from, "its name is not letters, digits, - and _; edit it in sources.toml by hand")
	}
	if sourcetext.CheckName(s.Name) != nil {
		if o != opAdd && definedIn(last, s.Name) {
			return Snapshot{}, unsupported(s.Name, "its name is not letters, digits, - and _; edit it in sources.toml by hand")
		}
		return Snapshot{}, ErrBadName
	}
	if o != opRemove {
		if err := checkSection(s); err != nil {
			return Snapshot{}, err
		}
	}
	if err := checkFresh(path, last); err != nil {
		return Snapshot{}, err
	}

	before := last.data
	var oldRaw file
	if _, err := toml.Decode(string(before), &oldRaw); err != nil {
		return Snapshot{}, syntaxError(path, err)
	}
	if _, err := parse(path, before, anyValue); err != nil {
		return Snapshot{}, fmt.Errorf("sources.toml has to load before it can be changed: %w", err)
	}
	_, exists := oldRaw.Sources[s.Name]
	target := s.Name
	if from != "" {
		if exists {
			return Snapshot{}, fmt.Errorf("%w: %s", ErrExists, s.Name)
		}
		target = from
		_, exists = oldRaw.Sources[from]
	}
	switch {
	case o == opAdd && exists:
		return Snapshot{}, fmt.Errorf("%w: %s", ErrExists, s.Name)
	case o != opAdd && !exists:
		return Snapshot{}, fmt.Errorf("%w: %s", ErrNotFound, target)
	}

	lines := scan(before)
	var after []byte
	switch o {
	case opAdd:
		if hasRootSources(lines) {
			return Snapshot{}, unsupported(s.Name, "sources is written as an inline table or with dotted keys")
		}
		after = addText(before, s)
	default:
		sec, ok, many := findSection(lines, target)
		if !ok || many {
			return Snapshot{}, unsupported(target, "it is not a plain [sources."+target+"] section")
		}
		if o == opRemove {
			after = removeText(before, lines, sec)
		} else {
			text := before
			if from != "" {
				var err error
				if text, err = renameText(before, lines[sec.header], s.Name); err != nil {
					return Snapshot{}, unsupported(from, "its header cannot be renamed in place")
				}
				lines = scan(text)
				if sec, ok, many = findSection(lines, s.Name); !ok || many {
					return Snapshot{}, unsupported(from, "its header cannot be renamed in place")
				}
			}
			var err error
			if after, err = replaceText(text, lines, sec, oldRaw.Sources[target], s); err != nil {
				return Snapshot{}, err
			}
		}
	}

	if buildHook != nil {
		after = buildHook(after)
	}
	if err := verify(path, before, after, o, s, from); err != nil {
		return Snapshot{}, err
	}
	if err := checkFresh(path, last); err != nil {
		return Snapshot{}, err
	}
	if err := writeFile(path, after); err != nil {
		return Snapshot{}, err
	}
	return Snapshot{data: after, exists: true}, nil
}

func unsupported(name, why string) error {
	return fmt.Errorf("%w: %q: %s", ErrUnsupported, name, why)
}

// syntaxError says where a file is not valid TOML, not what is there, which may be a
// password.
func syntaxError(path string, err error) error {
	var pe toml.ParseError
	if errors.As(err, &pe) {
		return fmt.Errorf("%s: line %d is not valid TOML", path, pe.Position.Line)
	}
	return fmt.Errorf("%s: %w", path, err)
}

// definedIn reports whether the file read says there is a Source called name, in
// whatever shape.
func definedIn(snap Snapshot, name string) bool {
	var f file
	if _, err := toml.Decode(string(snap.data), &f); err != nil {
		return false
	}
	_, ok := f.Sources[name]
	return ok
}

func checkFresh(path string, last Snapshot) error {
	now, err := Read(path)
	if err != nil {
		return err
	}
	if !now.same(last) {
		return ErrStale
	}
	return nil
}

// checkSection refuses what Load would refuse, repeating no value.
func checkSection(s Section) error {
	if err := sourcetext.CheckOne(s.Path, s.DSN); err != nil {
		return fmt.Errorf("%s: %w", s.Name, err)
	}
	if s.MaxRows != 0 {
		if err := rowcap.Check(s.MaxRows); err != nil {
			return fmt.Errorf("%s: max_rows: %w", s.Name, err)
		}
	}
	return nil
}

// verify loads both versions and checks the new one is the old with exactly the intended
// change.
func verify(path string, before, after []byte, o opKind, s Section, from string) error {
	fail := func(why string) error { return fmt.Errorf("%w: %s", ErrUnverified, why) }

	var oldRaw, newRaw file
	if _, err := toml.Decode(string(before), &oldRaw); err != nil {
		return fail("the old file does not decode")
	}
	if _, err := toml.Decode(string(after), &newRaw); err != nil {
		return fail("the new file does not decode")
	}
	want := map[string]entry{}
	for k, v := range oldRaw.Sources {
		want[k] = v
	}
	if from != "" {
		delete(want, from)
	}
	if o == opRemove {
		delete(want, s.Name)
	} else {
		e := entry{Path: s.Path, DSN: s.DSN, Write: s.Write}
		if s.MaxRows > 0 {
			n := s.MaxRows
			e.MaxRows = &n
		}
		want[s.Name] = e
	}
	got := newRaw.Sources
	oldRaw.Sources, newRaw.Sources = nil, nil
	if !reflect.DeepEqual(oldRaw, newRaw) {
		return fail("a setting outside the Sources would change")
	}
	if len(got) != len(want) {
		return fail("the number of Sources would be wrong")
	}
	for k, v := range want {
		if !reflect.DeepEqual(got[k], v) {
			return fail(fmt.Sprintf("Source %q would not be as intended", k))
		}
	}

	oldCfg, err := parse(path, before, anyValue)
	if err != nil {
		return fail("the old file does not load")
	}
	newCfg, err := parse(path, after, anyValue)
	if err != nil {
		return fail("the new file does not load")
	}
	if oldCfg.MaxRows != newCfg.MaxRows || oldCfg.HistoryOff != newCfg.HistoryOff ||
		oldCfg.MouseOff != newCfg.MouseOff || oldCfg.HomeOff != newCfg.HomeOff || oldCfg.Theme != newCfg.Theme {
		return fail("a setting outside the Sources would change when loaded")
	}
	var oldOthers, newOthers []source.Source
	for _, src := range oldCfg.Sources {
		if src.Name != s.Name && src.Name != from {
			oldOthers = append(oldOthers, src)
		}
	}
	copies := 0
	for _, src := range newCfg.Sources {
		if src.Name != s.Name {
			newOthers = append(newOthers, src)
			continue
		}
		copies++
		if src.Write != s.Write || src.MaxRows != s.MaxRows {
			return fail(fmt.Sprintf("Source %q would not be as intended when loaded", s.Name))
		}
	}
	if !reflect.DeepEqual(oldOthers, newOthers) {
		return fail("another Source would change when loaded")
	}
	wantCopies := 1
	if o == opRemove {
		wantCopies = 0
	}
	if copies != wantCopies {
		return fail(fmt.Sprintf("Source %q would be missing or repeated", s.Name))
	}
	return nil
}

// writeFile replaces path with data through a temporary file in the same directory.
func writeFile(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	mode := os.FileMode(0o600)
	if fi, err := os.Stat(path); err == nil {
		mode = fi.Mode().Perm()
	}
	tmp, err := os.CreateTemp(dir, "sources-*.tmp")
	if err != nil {
		return err
	}
	name := tmp.Name()
	_, err = tmp.Write(data)
	if err == nil {
		err = tmp.Sync()
	}
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Chmod(name, mode)
	}
	if err == nil {
		err = rename(name, path)
	}
	if err != nil {
		os.Remove(name)
	}
	return err
}
