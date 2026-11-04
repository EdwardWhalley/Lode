package config_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/EdwardWhalley/lode/internal/adapter"
	"github.com/EdwardWhalley/lode/internal/config"
)

func env(vars map[string]string) config.Lookup {
	return func(name string) (string, bool) {
		v, ok := vars[name]
		return v, ok
	}
}

func write(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "sources.toml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadNamedSources(t *testing.T) {
	path := write(t, `
[sources.zebra]
path = "z.csv"

[sources.app]
path = "db/app.sqlite"
write = true

[sources.prod]
dsn = "postgres://reader:${PW}@db.example.com/app"
`)
	cfg, err := config.Load(path, env(map[string]string{"PW": "s3cret"}))
	if err != nil {
		t.Fatal(err)
	}
	got := cfg.Sources

	if len(got) != 3 || got[0].Name != "app" || got[1].Name != "prod" || got[2].Name != "zebra" {
		t.Fatalf("sources not sorted by name: %+v", got)
	}
	dir := filepath.Dir(path)
	if want := filepath.Join(dir, "db", "app.sqlite"); got[0].Path != want {
		t.Errorf("relative path = %q, want it relative to the config file: %q", got[0].Path, want)
	}
	if !got[0].Write || got[2].Write {
		t.Errorf("write flags = %v, %v; want true for app only", got[0].Write, got[2].Write)
	}
	if want := "postgres://reader:s3cret@db.example.com/app"; got[1].DSN != want {
		t.Errorf("dsn = %q, want %q", got[1].DSN, want)
	}
}

func TestLoadExpandsEnvInPaths(t *testing.T) {
	abs := filepath.Join(t.TempDir(), "data")
	path := write(t, "[sources.d]\npath = \"${DATA_DIR}/x.csv\"\n")

	cfg, err := config.Load(path, env(map[string]string{"DATA_DIR": filepath.ToSlash(abs)}))
	if err != nil {
		t.Fatal(err)
	}
	got := cfg.Sources
	if want := filepath.Join(abs, "x.csv"); got[0].Path != want {
		t.Errorf("path = %q, want %q", got[0].Path, want)
	}
}

func TestLoadLeavesUnbracedDollarsAlone(t *testing.T) {
	path := write(t, "[sources.p]\ndsn = \"postgres://u:pa$$word$HOME@h/db\"\n")
	cfg, err := config.Load(path, env(nil))
	if err != nil {
		t.Fatal(err)
	}
	got := cfg.Sources
	if want := "postgres://u:pa$$word$HOME@h/db"; got[0].DSN != want {
		t.Errorf("dsn = %q, want %q", got[0].DSN, want)
	}
}

func TestLoadErrors(t *testing.T) {
	cases := []struct {
		name, toml, wantErr string
	}{
		{"missing env var", "[sources.p]\ndsn = \"postgres://u:${NOPE}@h/d\"\n", "environment variable NOPE is not set"},
		{"names the source", "[sources.p]\ndsn = \"postgres://u:${NOPE}@h/d\"\n", `source "p"`},
		{"no path or dsn", "[sources.p]\nwrite = true\n", `source "p" needs a path or a dsn`},
		{"both path and dsn", "[sources.p]\npath = \"a\"\ndsn = \"b\"\n", "both a path and a dsn"},
		{"unknown key", "[sources.p]\npath = \"a\"\nwirte = true\n", "unknown setting sources.p.wirte"},
		{"malformed toml", "[sources.p\npath = \"a\"\n", "sources.toml"},
		{"wrong type", "[sources.p]\npath = 5\n", "sources.toml"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := config.Load(write(t, tc.toml), env(nil))
			if err == nil {
				t.Fatal("expected an error")
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("error = %q, want it to contain %q", err, tc.wantErr)
			}
		})
	}
}

func TestLoadMaxRows(t *testing.T) {
	path := write(t, `
max_rows = 500

[sources.small]
path = "a.csv"
max_rows = 20

[sources.plain]
path = "b.csv"
`)
	cfg, err := config.Load(path, env(nil))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.MaxRows != 500 {
		t.Errorf("global max_rows = %d, want 500", cfg.MaxRows)
	}
	byName := map[string]int{}
	for _, s := range cfg.Sources {
		byName[s.Name] = s.MaxRows
	}
	if byName["small"] != 20 {
		t.Errorf("small max_rows = %d, want its own 20", byName["small"])
	}
	if byName["plain"] != 0 {
		t.Errorf("plain max_rows = %d, want 0 (unset: the global value applies later)", byName["plain"])
	}
}

func TestLoadWithoutMaxRowsLeavesItUnset(t *testing.T) {
	cfg, err := config.Load(write(t, "[sources.a]\npath = \"a.csv\"\n"), env(nil))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.MaxRows != 0 || cfg.Sources[0].MaxRows != 0 {
		t.Errorf("max_rows = %d / %d, want both unset", cfg.MaxRows, cfg.Sources[0].MaxRows)
	}
}

func TestLoadRejectsBadMaxRows(t *testing.T) {
	cases := []struct{ name, toml, wantErr string }{
		{"global zero", "max_rows = 0\n[sources.a]\npath = \"a\"\n", "max_rows: max rows must be between 1 and"},
		{"global negative", "max_rows = -5\n[sources.a]\npath = \"a\"\n", "max_rows:"},
		{"global too large", "max_rows = 99999999\n[sources.a]\npath = \"a\"\n", "max_rows:"},
		{"source zero", "[sources.a]\npath = \"a\"\nmax_rows = 0\n", `source "a": max_rows:`},
		{"source too large", "[sources.a]\npath = \"a\"\nmax_rows = 2000000\n", `source "a": max_rows:`},
		{"not a number", "[sources.a]\npath = \"a\"\nmax_rows = \"lots\"\n", "sources.toml"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := config.Load(write(t, tc.toml), env(nil))
			if err == nil {
				t.Fatal("expected an error")
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("error = %q, want it to contain %q", err, tc.wantErr)
			}
		})
	}
}

func TestLoadMissingFile(t *testing.T) {
	_, err := config.Load(filepath.Join(t.TempDir(), "nope.toml"), env(nil))
	if !errors.Is(err, os.ErrNotExist) {
		t.Errorf("err = %v, want one satisfying os.ErrNotExist", err)
	}
}

func TestDefaultPath(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", filepath.Join("x", "cfg"))
	got, err := config.DefaultPath()
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join("x", "cfg", "lode", "sources.toml"); got != want {
		t.Errorf("DefaultPath = %q, want %q", got, want)
	}
}

func TestHistoryIsOnUnlessTheFileTurnsItOff(t *testing.T) {
	for _, tc := range []struct {
		name, toml string
		wantOff    bool
	}{
		{"not mentioned", "", false},
		{"history = true", "history = true\n", false},
		{"history = false", "history = false\n", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg, err := config.Load(write(t, tc.toml+"[sources.p]\npath = \"a.csv\"\n"), env(nil))
			if err != nil {
				t.Fatal(err)
			}
			if cfg.HistoryOff != tc.wantOff {
				t.Errorf("HistoryOff = %v, want %v", cfg.HistoryOff, tc.wantOff)
			}
		})
	}
}

func TestHistorySettingMustBeABoolean(t *testing.T) {
	if _, err := config.Load(write(t, "history = \"no\"\n"), env(nil)); err == nil {
		t.Error("history = \"no\" should be an error, not silently on or off")
	}
	if _, err := config.Load(write(t, "histroy = false\n"), env(nil)); err == nil || !strings.Contains(err.Error(), "unknown setting histroy") {
		t.Errorf("a misspelt setting must be an error, got %v", err)
	}
}

func TestAV1ConfigWithEverySettingStillLoads(t *testing.T) {
	const v1 = `
max_rows = 5000

[sources.people]
path = "data/people.csv"

[sources.app]
path = "~/db/app.sqlite"
write = true
max_rows = 200

[sources.prod]
dsn = "postgres://reader:${PROD_PASSWORD}@db.example.com/app"

[sources.shop]
dsn = "mysql://reader:${SHOP_PASSWORD}@db.example.com:3306/shop?tls=skip-verify"
max_rows = 50
`
	cfg, err := config.Load(write(t, v1), env(map[string]string{"PROD_PASSWORD": "p1", "SHOP_PASSWORD": "p2"}))
	if err != nil {
		t.Fatalf("a v1 config no longer loads: %v", err)
	}
	if cfg.MaxRows != 5000 || cfg.HistoryOff || len(cfg.Sources) != 4 {
		t.Fatalf("cfg = %+v", cfg)
	}
	byName := map[string]int{}
	for i, s := range cfg.Sources {
		byName[s.Name] = i
	}
	if app := cfg.Sources[byName["app"]]; !app.Write || app.MaxRows != 200 {
		t.Errorf("app = %+v", app)
	}
	if prod := cfg.Sources[byName["prod"]]; !strings.Contains(prod.DSN, ":p1@") {
		t.Errorf("prod DSN = %q, want ${PROD_PASSWORD} expanded", prod.DSN)
	}
	if shop := cfg.Sources[byName["shop"]]; shop.MaxRows != 50 || !strings.Contains(shop.DSN, "tls=skip-verify") {
		t.Errorf("shop = %+v", shop)
	}
}

func TestThemeSetting(t *testing.T) {
	cfg, err := config.Load(write(t, "theme = \"Light\"\n[sources.p]\npath = \"a.csv\"\n"), env(nil))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Theme != "light" {
		t.Errorf("Theme = %q, want the canonical name light", cfg.Theme)
	}

	cfg, err = config.Load(write(t, "[sources.p]\npath = \"a.csv\"\n"), env(nil))
	if err != nil || cfg.Theme != "" {
		t.Errorf("without the setting: Theme = %q, %v; want unset", cfg.Theme, err)
	}

	_, err = config.Load(write(t, "theme = \"neon\"\n"), env(nil))
	if err == nil || !strings.Contains(err.Error(), `unknown theme "neon"`) || !strings.Contains(err.Error(), "bubbletea") {
		t.Errorf("err = %v, want the bad name and the available themes", err)
	}
	if _, err := config.Load(write(t, "theme = 3\n"), env(nil)); err == nil {
		t.Error("a theme that is not text should be an error")
	}
}

func TestMouseIsOnUnlessTheFileTurnsItOff(t *testing.T) {
	for _, tc := range []struct {
		name, toml string
		wantOff    bool
	}{
		{"not mentioned", "", false},
		{"mouse = true", "mouse = true\n", false},
		{"mouse = false", "mouse = false\n", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg, err := config.Load(write(t, tc.toml+"[sources.p]\npath = \"a.csv\"\n"), env(nil))
			if err != nil {
				t.Fatal(err)
			}
			if cfg.MouseOff != tc.wantOff {
				t.Errorf("MouseOff = %v, want %v", cfg.MouseOff, tc.wantOff)
			}
		})
	}
}

func TestMouseSettingMustBeABooleanAndATypoIsStillAnError(t *testing.T) {
	if _, err := config.Load(write(t, "mouse = \"no\"\n"), env(nil)); err == nil {
		t.Error("mouse = \"no\" should be an error, not silently on or off")
	}
	if _, err := config.Load(write(t, "mous = false\n"), env(nil)); err == nil || !strings.Contains(err.Error(), "unknown setting mous") {
		t.Errorf("a misspelt setting must be an error, got %v", err)
	}
}

func TestTheHomePageIsOnUnlessTheFileTurnsItOff(t *testing.T) {
	for _, tc := range []struct {
		name, toml string
		wantOff    bool
	}{
		{"not mentioned", "", false},
		{"home = true", "home = true\n", false},
		{"home = false", "home = false\n", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg, err := config.Load(write(t, tc.toml+"[sources.p]\npath = \"a.csv\"\n"), env(nil))
			if err != nil {
				t.Fatal(err)
			}
			if cfg.HomeOff != tc.wantOff {
				t.Errorf("HomeOff = %v, want %v", cfg.HomeOff, tc.wantOff)
			}
		})
	}
}

func TestHomeSettingMustBeABooleanAndATypoIsStillAnError(t *testing.T) {
	if _, err := config.Load(write(t, "home = \"no\"\n"), env(nil)); err == nil {
		t.Error("home = \"no\" should be an error, not silently on or off")
	}
	if _, err := config.Load(write(t, "hom = false\n"), env(nil)); err == nil || !strings.Contains(err.Error(), "unknown setting hom") {
		t.Errorf("a misspelt setting must be an error, got %v", err)
	}
}

func TestPasswordVariableNamesAreTheOnesConfigExpands(t *testing.T) {
	form := adapter.ServerForm(5432, []string{"postgres"})

	for _, name := range []string{
		"PW", "pw", "_", "_x", "a1", "A_B_9", "Pw_2_x",
		"1a", "9", "a-b", "a.b", "a b", "é", "a$", "${PW}", "PW}",
	} {
		var asked []string
		lookup := func(n string) (string, bool) {
			asked = append(asked, n)
			return "v", true
		}
		path := write(t, "[sources.p]\ndsn = \"postgres://u:${"+name+"}@h\"\n")
		_, loadErr := config.Load(path, lookup)
		expands := loadErr == nil && len(asked) == 1 && asked[0] == name

		_, composeErr := form.Compose(adapter.Values{"host": "h", "user": "u", "password_var": name}, "")
		accepts := composeErr == nil

		if expands != accepts {
			t.Errorf("name %q: config expands it = %v, the form accepts it = %v", name, expands, accepts)
		}
	}
}

func TestTextThatIsNotAReferenceIsNotExpanded(t *testing.T) {
	for _, dsn := range []string{"postgres://u:${1A}@h", "postgres://u:${}@h", "postgres://u:${A@h", "postgres://u:$A@h", "postgres://u:p${${A}@h"} {
		var asked []string
		lookup := func(n string) (string, bool) {
			asked = append(asked, n)
			return "v", true
		}
		cfg, err := config.Load(write(t, "[sources.p]\ndsn = \""+dsn+"\"\n"), lookup)
		if err != nil {
			t.Errorf("%s: %v", dsn, err)
			continue
		}
		want := dsn
		if strings.Contains(dsn, "${${A}") {
			want = "postgres://u:p${v@h"
		}
		if got := cfg.Sources[0].DSN; got != want {
			t.Errorf("DSN %q loaded as %q, want %q", dsn, got, want)
		}
		if strings.Contains(dsn, "${${A}") != (len(asked) == 1) {
			t.Errorf("DSN %q: lookup was asked %v", dsn, asked)
		}
	}
}
