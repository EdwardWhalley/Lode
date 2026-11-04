// Package session holds what the program knows about Sources while it runs, and is what the
// TUI switches Sources through.
package session

import (
	"fmt"
	"strings"

	"github.com/EdwardWhalley/lode/internal/config"
	"github.com/EdwardWhalley/lode/internal/rowcap"
	"github.com/EdwardWhalley/lode/internal/source"
	"github.com/EdwardWhalley/lode/internal/sourcetext"
	"github.com/EdwardWhalley/lode/internal/tui"
)

// Choice and Opened are the TUI's own types, so a Session satisfies tui.Switcher.
type (
	Choice = tui.Choice
	Opened = tui.Opened
)

const (
	adHocID     = "adhoc"
	namedPrefix = "named:"
)

// Options configure a Session: Write and MaxRows are the --write and --max-rows flags, and
// Write applies to the launch Source only.
type Options struct {
	Load       func() (config.Config, error)
	Config     config.Config
	Launch     source.Source
	Write      bool
	MaxRows    *int
	ConfigPath string
	Lookup     config.Lookup
}

// Session implements tui.Switcher.
type Session struct {
	opts    Options
	cfg     config.Config
	firstID string
	snap    config.Snapshot
	snapOK  bool
}

var _ tui.Switcher = (*Session)(nil)

func New(opts Options) *Session { return &Session{opts: opts, cfg: opts.Config} }

// Launch returns the Choice for the Source the program was started with; ok is
// false if it was started without one.
func (s *Session) Launch() (c Choice, ok bool) {
	if !s.hasLaunch() {
		return Choice{}, false
	}
	return s.choice(s.launchID(), s.opts.Launch, s.opts.Write), true
}

func (s *Session) hasLaunch() bool {
	return sourcetext.Text(s.opts.Launch.Path, s.opts.Launch.DSN) != ""
}

// launchID is the launch Source's ID: its configured name if it has one.
func (s *Session) launchID() string {
	switch {
	case !s.hasLaunch():
		return s.firstID
	case s.opts.Launch.Name != "":
		return namedPrefix + s.opts.Launch.Name
	}
	return adHocID
}

// choice describes src to the TUI. Its Dialect is its Kind's, and the zero Dialect for a
// Source no Kind claims, which cannot be opened anyway.
func (s *Session) choice(id string, src source.Source, flagWrite bool) Choice {
	k, _ := source.KindOf(src)
	return Choice{
		ID: id, Name: src.Name, Label: src.Label(), Write: src.Write || flagWrite,
		Dialect: k.Dialect,
	}
}

// Sources re-reads the configuration and lists the Sources, the ad-hoc launch Source first.
func (s *Session) Sources() ([]Choice, error) {
	s.readSnapshot()
	cfg, err := s.opts.Load()
	if err != nil {
		return nil, err
	}
	s.cfg = cfg

	var out []Choice
	if l, ok := s.Launch(); ok && l.ID == adHocID {
		out = append(out, l)
	}
	for _, src := range cfg.Sources {
		id := namedPrefix + src.Name
		out = append(out, s.choice(id, src, id == s.launchID() && s.opts.Write))
	}
	return out, nil
}

// Open opens the Source with the given ID, with the row cap that applies to it.
func (s *Session) Open(id string) (Opened, error) {
	src, err := s.find(id)
	if err != nil {
		return Opened{}, err
	}
	standsInForLaunch := !s.hasLaunch() && s.firstID == ""
	flagWrite := s.opts.Write && (id == s.launchID() || standsInForLaunch)
	if flagWrite {
		src.Write = true
	}
	maxRows, err := ResolveMaxRows(s.opts.MaxRows, src, s.cfg.MaxRows)
	if err != nil {
		return Opened{}, err
	}
	a, err := source.Open(src, source.WithRowCap(maxRows))
	if err != nil {
		return Opened{}, err
	}
	if standsInForLaunch {
		s.firstID = id
	}
	return Opened{Adapter: a, Choice: s.choice(id, src, flagWrite), MaxRows: maxRows}, nil
}

func (s *Session) find(id string) (source.Source, error) {
	if id == adHocID {
		return s.opts.Launch, nil
	}
	name, ok := strings.CutPrefix(id, namedPrefix)
	if !ok {
		return source.Source{}, fmt.Errorf("unknown Source %q", id)
	}
	for _, src := range s.cfg.Sources {
		if src.Name == name {
			return src, nil
		}
	}
	if s.opts.Launch.Name == name {
		return s.opts.Launch, nil
	}
	return source.Source{}, fmt.Errorf("Source %q is not in the configuration", name)
}

// ResolveMaxRows picks the row cap: --max-rows, else the Source's max_rows, else the
// top-level one, else the default.
func ResolveMaxRows(override *int, s source.Source, global int) (int, error) {
	switch {
	case override != nil:
		if err := rowcap.Check(*override); err != nil {
			return 0, fmt.Errorf("--max-rows: %w", err)
		}
		return *override, nil
	case s.MaxRows > 0:
		return s.MaxRows, nil
	case global > 0:
		return global, nil
	}
	return rowcap.Default, nil
}
