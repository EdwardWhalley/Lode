// Command lode is a terminal UI for querying data files and databases with SQL.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"github.com/EdwardWhalley/lode/internal/clipboard"
	"github.com/EdwardWhalley/lode/internal/config"
	"github.com/EdwardWhalley/lode/internal/history"
	"github.com/EdwardWhalley/lode/internal/session"
	"github.com/EdwardWhalley/lode/internal/source"
	"github.com/EdwardWhalley/lode/internal/theme"
	"github.com/EdwardWhalley/lode/internal/tui"
)

// version is set at build time for releases.
var version = "dev"

const usage = `usage: lode [--write] [--max-rows n] [--theme name] [--no-mouse] [--no-home] [--config file] [--version] [source]

source is the name of a Source from sources.toml, or a path to a data file
(csv, tsv, json, ndjson, parquet) or SQLite database (sqlite, sqlite3, db).
With no source, a home page opens first; any key then shows the Source selector,
which lists the Sources from sources.toml.
Press ctrl+o at any time to switch Source, and F1 for every key.

  --write         allow statements that modify the Source (read-only by default)
  --max-rows n    show at most n rows per query (default 10000, at most 1000000);
                  overrides max_rows in sources.toml
  --theme name    colour theme: bubbletea (default), classic, light or mono;
                  overrides theme in sources.toml. F2 cycles themes while running
  --no-mouse      do not capture the mouse, so the terminal's own selection and wheel
                  work; mouse = false in sources.toml does the same for every run
  --no-home       start on the Source selector, without the home page; home = false
                  in sources.toml does the same for every run
  --config file   use this config file instead of ~/.config/lode/sources.toml
  --version       print the version and exit`

var errUsage = errors.New(usage)

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		switch {
		case errors.Is(err, flag.ErrHelp):
		case errors.Is(err, errUsage):
			fmt.Fprintln(os.Stderr, usage)
		default:
			fmt.Fprintln(os.Stderr, "lode:", err)
		}
		os.Exit(1)
	}
}

// run parses the arguments, opens what they name and runs the program until it ends.
func run(args []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("lode", flag.ContinueOnError)
	flags.SetOutput(stderr)
	flags.Usage = func() { fmt.Fprintln(stderr, usage) }
	showVersion := flags.Bool("version", false, "")
	write := flags.Bool("write", false, "")
	configPath := flags.String("config", "", "")
	maxRowsFlag := flags.Int("max-rows", 0, "")
	themeFlag := flags.String("theme", "", "")
	noMouse := flags.Bool("no-mouse", false, "")
	noHome := flags.Bool("no-home", false, "")

	var positional []string
	for {
		if err := flags.Parse(args); err != nil {
			return err
		}
		if flags.NArg() == 0 {
			break
		}
		positional = append(positional, flags.Arg(0))
		args = flags.Args()[1:]
	}
	if *showVersion {
		fmt.Fprintln(stdout, "lode", version)
		return nil
	}
	if len(positional) > 1 {
		return errUsage
	}

	var maxRowsOverride *int
	themeGiven := false
	flags.Visit(func(f *flag.Flag) {
		switch f.Name {
		case "max-rows":
			maxRowsOverride = maxRowsFlag
		case "theme":
			themeGiven = true
		}
	})

	cfg, err := loadConfig(*configPath)
	if err != nil {
		return err
	}
	named := cfg.Sources

	var s source.Source
	if len(positional) == 1 {
		s = source.Resolve(positional[0], named)
		if err := checkExists(s, positional[0], named); err != nil {
			return err
		}
	}
	if _, err := session.ResolveMaxRows(maxRowsOverride, source.Source{}, 0); maxRowsOverride != nil && err != nil {
		return err
	}
	themeName := theme.Default
	switch {
	case themeGiven:
		if err := theme.Check(*themeFlag); err != nil {
			return fmt.Errorf("--theme: %w", err)
		}
		themeName = *themeFlag
	case cfg.Theme != "":
		themeName = cfg.Theme
	}
	sess := session.New(session.Options{
		Load:       func() (config.Config, error) { return loadConfig(*configPath) },
		Config:     cfg,
		Launch:     s,
		Write:      *write,
		MaxRows:    maxRowsOverride,
		ConfigPath: configFilePath(*configPath),
	})

	common := []tui.Option{
		tui.WithTheme(themeName),
		tui.WithClipboard(clipboard.New(clipboard.OS(), os.Stdout)),
	}
	common = append(common, historyOptions(cfg)...)
	if attributesWithoutColour(lipgloss.ColorProfile(), os.Getenv("TERM")) {
		lipgloss.SetColorProfile(termenv.ANSI)
		common = append(common, tui.WithNoColour())
	}
	var shapes *tui.PointerShapes
	if mouseOn(cfg, *noMouse) {
		shapes = tui.NewPointerShapes(os.Stdout)
		common = append(common, tui.WithPointerShapes(shapes))
	}

	m, opened, err := startModel(sess, common, homeSkipped(cfg, *noHome))
	if err != nil {
		return err
	}

	final, err := tea.NewProgram(m, programOptions(cfg, *noMouse)...).Run()
	shapes.Restore()
	if fm, ok := final.(tui.Model); ok {
		fm.Close()
	} else if opened != nil {
		opened.Adapter.Close()
	}
	return err
}

// attributesWithoutColour says the terminal must show no colour but can still show bold and
// reverse video.
func attributesWithoutColour(profile termenv.Profile, term string) bool {
	return profile == termenv.Ascii && term != "dumb"
}

// startModel is the Model the program starts with, on the Source given or else on the home
// page or selector. The caller closes opened, nil if there is none, if the program never
// gets to.
func startModel(sess *session.Session, common []tui.Option, homeOff bool) (m tui.Model, opened *tui.Opened, err error) {
	opts := slices.Clone(common)
	launch, ok := sess.Launch()
	if !ok {
		opts = append(opts, tui.WithSwitcher(sess, tui.Choice{}))
		if homeOff {
			opts = append(opts, tui.WithSelectorAtStart())
		} else {
			opts = append(opts, tui.WithHomeAtStart())
		}
		return tui.New(nil, "", opts...), nil, nil
	}
	o, err := sess.Open(launch.ID)
	if err != nil {
		return tui.Model{}, nil, err
	}
	opts = append(opts, tui.WithMaxRows(o.MaxRows), tui.WithSwitcher(sess, o.Choice))
	if o.Choice.Write {
		opts = append(opts, tui.WithWriteMode())
	}
	return tui.New(o.Adapter, o.Choice.Label, opts...), &o, nil
}

// homeSkipped says to start on the Source selector, because of home = false or --no-home.
func homeSkipped(cfg config.Config, noHome bool) bool { return cfg.HomeOff || noHome }

// mouseOn says the mouse is captured: unless mouse = false or --no-mouse.
func mouseOn(cfg config.Config, noMouse bool) bool { return !cfg.MouseOff && !noMouse }

func programOptions(cfg config.Config, noMouse bool) []tea.ProgramOption {
	opts := []tea.ProgramOption{tea.WithAltScreen()}
	if mouseOn(cfg, noMouse) {
		opts = append(opts, tea.WithMouseAllMotion())
	}
	return opts
}

// checkExists catches a mistyped Source name: neither a configured name nor an existing
// file.
func checkExists(s source.Source, arg string, named []source.Source) error {
	if s.Name != "" || s.Path == "" || len(named) == 0 {
		return nil
	}
	if _, err := os.Stat(s.Path); err == nil {
		return nil
	}
	names := make([]string, len(named))
	for i, n := range named {
		names[i] = n.Name
	}
	return fmt.Errorf("%q is neither a configured Source nor an existing file (configured: %s)", arg, strings.Join(names, ", "))
}

// configFilePath is the sources.toml Sources are saved to: --config, else the default, else
// empty with no home directory.
func configFilePath(explicit string) string {
	if explicit != "" {
		return explicit
	}
	path, err := config.DefaultPath()
	if err != nil {
		return ""
	}
	return path
}

// loadConfig reads sources.toml; only a missing file the user asked for is an error.
func loadConfig(explicit string) (config.Config, error) {
	path := explicit
	if path == "" {
		var err error
		if path, err = config.DefaultPath(); err != nil {
			return config.Config{}, nil
		}
	}
	cfg, err := config.Load(path, os.LookupEnv)
	if errors.Is(err, os.ErrNotExist) && explicit == "" {
		return config.Config{}, nil
	}
	return cfg, err
}

// historyOptions opens the query history unless sources.toml turns it off; if it cannot be
// opened it stays off and the status line says so.
func historyOptions(cfg config.Config) []tui.Option {
	if cfg.HistoryOff {
		return nil
	}
	path, err := history.DefaultPath()
	if err == nil {
		var store *history.Store
		if store, err = history.Open(path); err == nil {
			return []tui.Option{tui.WithHistory(store)}
		}
	}
	return []tui.Option{tui.WithNotice("history is off for this session: " + err.Error())}
}
