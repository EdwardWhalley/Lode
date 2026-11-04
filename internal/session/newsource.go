// Creating a Source: the Kinds on offer, composing a location, testing it and saving it.

package session

import (
	"context"
	"errors"
	"net/url"
	"os"
	"strings"
	"unicode"

	"github.com/EdwardWhalley/lode/internal/adapter"
	"github.com/EdwardWhalley/lode/internal/config"
	"github.com/EdwardWhalley/lode/internal/source"
	"github.com/EdwardWhalley/lode/internal/sourcetext"
	"github.com/EdwardWhalley/lode/internal/tui"
)

// creatable lists the Kinds that describe a form; tui.NewSource.Kind indexes this list.
func creatable() []adapter.Kind {
	var out []adapter.Kind
	for _, k := range source.Kinds {
		if len(k.Fields) > 0 {
			out = append(out, k)
		}
	}
	return out
}

// Kinds lists the kinds of Source that can be created, in the order of source.Kinds.
func (s *Session) Kinds() []tui.KindInfo {
	var out []tui.KindInfo
	for _, k := range creatable() {
		out = append(out, tui.KindInfo{Name: k.Name, ReadOnly: k.ReadOnly, Fields: k.Fields})
	}
	return out
}

// lookup is how variables are answered: the given Lookup, else the environment.
func (s *Session) lookup() config.Lookup {
	if s.opts.Lookup != nil {
		return s.opts.Lookup
	}
	return os.LookupEnv
}

// readSnapshot notes what sources.toml holds now, keeping the last note if it cannot be
// read.
func (s *Session) readSnapshot() {
	if s.opts.ConfigPath == "" {
		return
	}
	if snap, err := config.Read(s.opts.ConfigPath); err == nil {
		s.snap, s.snapOK = snap, true
	}
}

// resolved is a Source from the form, in the two shapes it is needed in.
type resolved struct {
	section config.Section
	src     source.Source
	values  []string
	literal bool
}

// storedSection is the Source called name as sources.toml held it when Sources last
// read the file.
func (s *Session) storedSection(name string) (config.Section, error) {
	if !s.snapOK {
		s.readSnapshot()
	}
	sec, err := s.snap.Section(name)
	if err != nil {
		return config.Section{}, s.refused(err, "change")
	}
	return sec, nil
}

// stored is the location sources.toml holds for name, and whether it is stored as a DSN.
func (s *Session) stored(name string) (text string, isDSN bool, err error) {
	if name == "" {
		return "", false, nil
	}
	sec, err := s.storedSection(name)
	if err != nil {
		return "", false, err
	}
	text, isDSN = sourcetext.Of(sec.Path, sec.DSN)
	return text, isDSN, nil
}

// location is the text n stores: typed whole, kept as it is, or made from the Kind's
// fields.
func location(k adapter.Kind, n tui.NewSource, keep string) (string, error) {
	switch n.From {
	case tui.FromStored:
		if keep == "" {
			return "", errors.New("there is no stored location to keep")
		}
		return keep, nil
	case tui.FromText:
		if strings.TrimSpace(n.Location) == "" {
			return "", errors.New("the location is required")
		}
		return n.Location, nil
	}
	return k.Compose(n.Values, keep)
}

// resolve composes the Source n describes; an unset variable is tui.ErrVariableUnset.
func (s *Session) resolve(n tui.NewSource) (resolved, error) {
	kinds := creatable()
	if n.Kind < 0 || n.Kind >= len(kinds) {
		return resolved{}, errors.New("unknown kind of Source")
	}
	keep, keepIsDSN, err := s.stored(n.Replaces)
	if err != nil {
		return resolved{}, err
	}
	loc, err := location(kinds[n.Kind], n, keep)
	if err != nil {
		return resolved{}, err
	}
	sec := config.Section{Name: n.Name, Write: n.Write, MaxRows: n.MaxRows}
	isDSN := sourcetext.LooksLikeDSN(loc)
	if keep != "" && loc == keep {
		isDSN = keepIsDSN
	}
	if isDSN {
		sec.DSN = loc
	} else {
		sec.Path = loc
	}

	var r resolved
	_, r.literal, _ = kinds[n.Kind].Parse(loc)
	unset := false
	lookup := func(name string) (string, bool) {
		v, ok := s.lookup()(name)
		switch {
		case !ok:
			unset = true
		case v != "":
			r.values = append(r.values, v)
		}
		return v, ok
	}
	src, err := sec.Resolve(s.opts.ConfigPath, lookup)
	switch {
	case unset:
		return resolved{}, tui.ErrVariableUnset
	case err != nil:
		return resolved{}, r.scrub(err)
	}
	r.section, r.src = sec, src
	return r, nil
}

// scrub keeps the value of every variable the Source used out of an error's text.
func (r resolved) scrub(err error) error {
	if r.literal {
		return errors.New("the connection failed. The details are hidden because the Source's text holds a password")
	}
	var forms []string
	_, isDSN := sourcetext.Of(r.section.Path, r.section.DSN)
	for _, v := range r.values {
		decoded, derr := url.PathUnescape(v)
		if isDSN && (strings.ContainsAny(v, "@/:?#[]") || strings.IndexFunc(v, unicode.IsSpace) >= 0 || derr != nil) {
			return errors.New("the connection failed. The details are hidden because the value of a variable has characters " +
				"that mean something in a URL (@ / : ? # [ ] % or a space); percent-encode them in the variable, " +
				"for example @ as %40")
		}
		forms = append(forms, v, decoded)
	}
	text := err.Error()
	for _, f := range forms {
		if f != "" {
			text = strings.ReplaceAll(text, f, "xxxxx")
		}
	}
	return errors.New(text)
}

// TestSource opens the Source n describes by the rules Open applies and closes it again.
func (s *Session) TestSource(ctx context.Context, n tui.NewSource) error {
	r, err := s.resolve(n)
	if err != nil {
		return err
	}
	maxRows, err := ResolveMaxRows(s.opts.MaxRows, r.src, s.cfg.MaxRows)
	if err != nil {
		return err
	}
	type result struct {
		a   adapter.Adapter
		err error
	}
	done := make(chan result, 1)
	go func() {
		a, err := source.Open(r.src, source.WithRowCap(maxRows))
		done <- result{a, err}
	}()
	select {
	case <-ctx.Done():
		go func() {
			if res := <-done; res.a != nil {
				res.a.Close()
			}
		}()
		return ctx.Err()
	case res := <-done:
		if res.err != nil {
			return r.scrub(res.err)
		}
		res.a.Close()
		return nil
	}
}

// AddSource writes the Source n describes to sources.toml, if the file is still as Sources
// last read it.
func (s *Session) AddSource(n tui.NewSource) (Choice, error) {
	if s.opts.ConfigPath == "" {
		return Choice{}, errors.New("there is no sources.toml to save to")
	}
	r, err := s.resolve(n)
	if err != nil {
		return Choice{}, err
	}
	if !s.snapOK {
		s.readSnapshot()
	}
	snap, err := config.Add(s.opts.ConfigPath, s.snap, r.section)
	if err != nil {
		return Choice{}, s.refused(err, "add")
	}
	s.written(snap)
	return s.choice(namedPrefix+n.Name, r.src, false), nil
}
