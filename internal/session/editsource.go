// Editing and deleting a Source: the Switcher calls behind the edit form and the delete
// question.

package session

import (
	"errors"
	"fmt"
	"strings"

	"github.com/EdwardWhalley/lode/internal/config"
	"github.com/EdwardWhalley/lode/internal/source"
	"github.com/EdwardWhalley/lode/internal/sourcetext"
	"github.com/EdwardWhalley/lode/internal/tui"
)

// refused turns what the writer refused with into something to show; verb says what was
// being done.
func (s *Session) refused(err error, verb string) error {
	switch {
	case errors.Is(err, config.ErrStale):
		s.readSnapshot()
		return errors.New("sources.toml changed on disk since the Source list was read; it has been read again, so try once more")
	case errors.Is(err, config.ErrExists):
		return tui.ErrNameTaken
	case errors.Is(err, config.ErrNotFound):
		return errors.New("that Source is no longer in sources.toml")
	case errors.Is(err, config.ErrUnsupported):
		return fmt.Errorf("%s this Source in sources.toml by hand: %w", verb, err)
	}
	return err
}

// written notes the file the writer made, and reads the configuration again so that
// Open can find what changed.
func (s *Session) written(snap config.Snapshot) {
	s.snap, s.snapOK = snap, true
	if cfg, err := s.opts.Load(); err == nil {
		s.cfg = cfg
	}
}

// namedSource is the name in the ID of a Source that is in sources.toml.
func namedSource(id string) (string, error) {
	name, ok := strings.CutPrefix(id, namedPrefix)
	if !ok {
		return "", errors.New("this Source was given on the command line; it is not in sources.toml")
	}
	return name, nil
}

// EditForm reads a Source from sources.toml as the form for its Kind shows it, with no
// literal password.
func (s *Session) EditForm(id string) (tui.EditForm, error) {
	name, err := namedSource(id)
	if err != nil {
		return tui.EditForm{}, err
	}
	sec, err := s.storedSection(name)
	if err != nil {
		return tui.EditForm{}, err
	}
	loc := sourcetext.Text(sec.Path, sec.DSN)
	k, ok := source.KindOf(source.Source{Path: sec.Path, DSN: sec.DSN})
	kind := -1
	for i, c := range creatable() {
		if ok && c.Name == k.Name {
			kind = i
		}
	}
	if kind < 0 {
		return tui.EditForm{}, errors.New("no kind of Source lode has a form for claims this one; edit it in sources.toml by hand")
	}

	f := tui.EditForm{Kind: kind, Name: name, Write: sec.Write, MaxRows: sec.MaxRows}
	values, literal, err := k.Parse(loc)
	switch {
	case err != nil && literal:
		f.Placement = tui.Hidden
	case err != nil:
		f.Placement, f.Location = tui.ShownAsText, loc
	case literal:
		f.Placement, f.Values = tui.PlacedKeepingPassword, values
	default:
		f.Values = values
	}
	return f, nil
}

// ReplaceSource writes n over the Source it replaces, renaming it if the name differs.
func (s *Session) ReplaceSource(n tui.NewSource) (Choice, error) {
	if s.opts.ConfigPath == "" {
		return Choice{}, errors.New("there is no sources.toml to save to")
	}
	if n.Replaces == "" {
		return Choice{}, errors.New("no Source to replace")
	}
	r, err := s.resolve(n)
	if err != nil {
		return Choice{}, err
	}
	snap, err := config.ReplaceAs(s.opts.ConfigPath, s.snap, n.Replaces, r.section)
	if err != nil {
		return Choice{}, s.refused(err, "change")
	}
	s.written(snap)
	return s.choice(namedPrefix+n.Name, r.src, false), nil
}

// RemoveSource deletes the Source with the given ID from sources.toml.
func (s *Session) RemoveSource(id string) error {
	if s.opts.ConfigPath == "" {
		return errors.New("there is no sources.toml to change")
	}
	name, err := namedSource(id)
	if err != nil {
		return err
	}
	if !s.snapOK {
		s.readSnapshot()
	}
	snap, err := config.Remove(s.opts.ConfigPath, s.snap, name)
	if err != nil {
		return s.refused(err, "delete")
	}
	s.written(snap)
	return nil
}
