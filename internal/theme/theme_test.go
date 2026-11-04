package theme_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/EdwardWhalley/lode/internal/theme"
)

func TestEveryNamedThemeResolvesAndTheDefaultIsOneOfThem(t *testing.T) {
	names := theme.Names()
	if len(names) < 3 {
		t.Fatalf("names = %v, want several themes", names)
	}
	seen := map[string]bool{}
	for _, n := range names {
		p, ok := theme.Get(n)
		if !ok || p.Name != n {
			t.Errorf("Get(%q) = %+v, %v", n, p, ok)
		}
		if seen[n] {
			t.Errorf("duplicate theme name %q", n)
		}
		seen[n] = true
		if n != strings.ToLower(n) || strings.ContainsAny(n, " \t") {
			t.Errorf("theme name %q should be lower case with no spaces: it is typed in a config file", n)
		}
	}
	if !seen[theme.Default] {
		t.Errorf("the default %q is not a theme", theme.Default)
	}
}

func TestLookupIgnoresCaseAndReportsUnknownNames(t *testing.T) {
	if p, ok := theme.Get("BubbleTea"); !ok || p.Name != "bubbletea" {
		t.Errorf("Get(BubbleTea) = %+v, %v", p, ok)
	}
	if _, ok := theme.Get("nope"); ok {
		t.Error("an unknown theme was found")
	}
	if err := theme.Check("bubbletea"); err != nil {
		t.Errorf("Check(bubbletea) = %v", err)
	}
	err := theme.Check("nope")
	if err == nil || !strings.Contains(err.Error(), `"nope"`) {
		t.Fatalf("Check(nope) = %v, want the bad name", err)
	}
	for _, n := range theme.Names() {
		if !strings.Contains(err.Error(), n) {
			t.Errorf("the error %q should list the available theme %q", err, n)
		}
	}
}

func TestNextCyclesThroughEveryThemeAndComesBack(t *testing.T) {
	start := theme.Default
	name, visited := start, map[string]bool{start: true}
	for i := 0; i < len(theme.Names()); i++ {
		name = theme.Next(name)
		visited[name] = true
	}
	if name != start {
		t.Errorf("after one full cycle the theme is %q, want %q", name, start)
	}
	if len(visited) != len(theme.Names()) {
		t.Errorf("visited %v, want every theme", visited)
	}
	if got := theme.Next("not-a-theme"); got != theme.Names()[0] {
		t.Errorf("Next of an unknown name = %q, want the first theme", got)
	}
}

func TestThemesDifferFromEachOther(t *testing.T) {
	for i, a := range theme.Names() {
		for _, b := range theme.Names()[i+1:] {
			pa, _ := theme.Get(a)
			pb, _ := theme.Get(b)
			pa.Name, pb.Name = "", ""
			if pa == pb {
				t.Errorf("themes %q and %q are identical", a, b)
			}
		}
	}
}

func TestMonoUsesNoColour(t *testing.T) {
	p, _ := theme.Get("mono")
	p.Name = ""
	p.Editor = theme.EditorDefault
	if p != (theme.Palette{}) {
		t.Errorf("mono = %+v, want no colours at all", p)
	}
}

func TestReadmeNamesEveryTheme(t *testing.T) {
	dir, _ := os.Getwd()
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			break
		}
		if parent := filepath.Dir(dir); parent != dir {
			dir = parent
			continue
		}
		t.Fatal("go.mod not found")
	}
	raw, err := os.ReadFile(filepath.Join(dir, "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range theme.Names() {
		if !strings.Contains(string(raw), "`"+n+"`") {
			t.Errorf("README does not mention the theme `%s`", n)
		}
	}
}

func TestEveryColourThemeColoursTheSelection(t *testing.T) {
	for _, name := range theme.Names() {
		p, _ := theme.Get(name)
		if p.Editor == theme.EditorPlain {
			continue
		}
		if p.SelectionFg == "" && p.SelectionBg == "" {
			t.Errorf("theme %q has colour but no SelectionFg or SelectionBg", name)
		}
	}
}

func TestEveryThemeSetsTheHomePageColoursOrIsMono(t *testing.T) {
	for _, name := range theme.Names() {
		p, _ := theme.Get(name)
		colours := map[string]string{
			"LogoFg": p.LogoFg, "LogoGlint": p.LogoGlint, "LogoGleam": p.LogoGleam,
			"LogoShadow": p.LogoShadow, "LogoRule": p.LogoRule, "PromptFg": p.PromptFg,
		}
		for field, colour := range colours {
			if name == "mono" && colour != "" {
				t.Errorf("mono sets %s = %q; it has no colour", field, colour)
			}
			if name != "mono" && colour == "" {
				t.Errorf("theme %s leaves %s empty", name, field)
			}
		}
	}
}
