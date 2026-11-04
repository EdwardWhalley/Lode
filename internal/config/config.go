// Package config loads named Sources from sources.toml.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"

	"github.com/EdwardWhalley/lode/internal/rowcap"
	"github.com/EdwardWhalley/lode/internal/source"
	"github.com/EdwardWhalley/lode/internal/sourcetext"
	"github.com/EdwardWhalley/lode/internal/theme"
)

// Lookup reports the value of an environment variable.
type Lookup func(name string) (string, bool)

// file is sources.toml as decoded; pointers tell a setting left out from one set to zero or
// false.
type file struct {
	MaxRows *int             `toml:"max_rows"`
	History *bool            `toml:"history"`
	Mouse   *bool            `toml:"mouse"`
	Home    *bool            `toml:"home"`
	Theme   *string          `toml:"theme"`
	Sources map[string]entry `toml:"sources"`
}

type entry struct {
	Path    string `toml:"path"`
	DSN     string `toml:"dsn"`
	Write   bool   `toml:"write"`
	MaxRows *int   `toml:"max_rows"`
}

// Config is the contents of sources.toml.
type Config struct {
	Sources    []source.Source
	MaxRows    int
	HistoryOff bool
	MouseOff   bool
	HomeOff    bool
	Theme      string
}

// DefaultPath is ~/.config/lode/sources.toml, or under $XDG_CONFIG_HOME if set.
func DefaultPath() (string, error) {
	if dir := os.Getenv("XDG_CONFIG_HOME"); dir != "" {
		return filepath.Join(dir, "lode", "sources.toml"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "lode", "sources.toml"), nil
}

// Load reads path, expanding ${NAME} from lookup and resolving relative paths against the
// config file.
func Load(path string, lookup Lookup) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, fmt.Errorf("%s: %w", path, err)
	}
	return parse(path, data, lookup)
}

// parse is Load on contents already read; path only names the file and the base for
// relative paths.
func parse(path string, data []byte, lookup Lookup) (Config, error) {
	var f file
	md, err := toml.Decode(string(data), &f)
	if err != nil {
		return Config{}, fmt.Errorf("%s: %w", path, err)
	}
	if undecoded := md.Undecoded(); len(undecoded) > 0 {
		keys := make([]string, len(undecoded))
		for i, k := range undecoded {
			keys[i] = k.String()
		}
		return Config{}, fmt.Errorf("%s: unknown setting %s", path, strings.Join(keys, ", "))
	}

	cfg := Config{
		HistoryOff: f.History != nil && !*f.History,
		MouseOff:   f.Mouse != nil && !*f.Mouse,
		HomeOff:    f.Home != nil && !*f.Home,
	}
	if f.MaxRows != nil {
		if err := rowcap.Check(*f.MaxRows); err != nil {
			return Config{}, fmt.Errorf("%s: max_rows: %w", path, err)
		}
		cfg.MaxRows = *f.MaxRows
	}
	if f.Theme != nil {
		p, ok := theme.Get(*f.Theme)
		if !ok {
			return Config{}, fmt.Errorf("%s: theme: %w", path, theme.Check(*f.Theme))
		}
		cfg.Theme = p.Name
	}

	names := make([]string, 0, len(f.Sources))
	for name := range f.Sources {
		names = append(names, name)
	}
	sort.Strings(names)

	baseDir := filepath.Dir(path)
	out := make([]source.Source, 0, len(names))
	for _, name := range names {
		e := f.Sources[name]
		if err := sourcetext.CheckOne(e.Path, e.DSN); err != nil {
			return Config{}, fmt.Errorf("%s: source %q %w", path, name, err)
		}

		s := source.Source{Name: name, Write: e.Write}
		if e.MaxRows != nil {
			if err := rowcap.Check(*e.MaxRows); err != nil {
				return Config{}, fmt.Errorf("%s: source %q: max_rows: %w", path, name, err)
			}
			s.MaxRows = *e.MaxRows
		}
		s.Path, s.DSN, err = expandLocation(e.Path, e.DSN, baseDir, lookup)
		if err != nil {
			return Config{}, fmt.Errorf("%s: source %q: %w", path, name, err)
		}
		out = append(out, s)
	}
	cfg.Sources = out
	return cfg, nil
}

// expandLocation is how a stored path or DSN becomes the one a Source opens.
func expandLocation(path, dsn, baseDir string, lookup Lookup) (outPath, outDSN string, err error) {
	if text, isDSN := sourcetext.Of(path, dsn); isDSN {
		outDSN, err = expandEnv(text, lookup)
		return "", outDSN, err
	}
	outPath, err = expandEnv(path, lookup)
	if err != nil {
		return "", "", err
	}
	return resolvePath(outPath, baseDir), "", nil
}

// expandEnv replaces each ${NAME}, naming every unset variable in one error.
func expandEnv(s string, lookup Lookup) (string, error) {
	out, missing := sourcetext.Expand(s, lookup)
	if len(missing) > 0 {
		return "", fmt.Errorf("environment variable %s is not set", strings.Join(missing, ", "))
	}
	return out, nil
}

func resolvePath(p, baseDir string) string {
	if p == "~" || strings.HasPrefix(p, "~/") || strings.HasPrefix(p, `~\`) {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, p[1:])
		}
	}
	if filepath.IsAbs(p) {
		return filepath.Clean(p)
	}
	return filepath.Join(baseDir, p)
}
