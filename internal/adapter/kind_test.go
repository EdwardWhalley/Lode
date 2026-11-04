package adapter_test

import (
	"strings"
	"testing"

	"github.com/EdwardWhalley/lode/internal/adapter"
)

func open(adapter.Request) (adapter.Adapter, error) { return nil, nil }

func TestCheckKinds(t *testing.T) {
	good := adapter.Kind{Name: "A", Schemes: []string{"a"}, Extensions: []string{"x"}, Open: open}

	if err := adapter.CheckKinds([]adapter.Kind{good, {Name: "B", Schemes: []string{"b"}, Extensions: []string{"y"}, Open: open}}); err != nil {
		t.Errorf("a consistent list was rejected: %v", err)
	}

	cases := []struct {
		name  string
		kinds []adapter.Kind
		want  string
	}{
		{"duplicate scheme", []adapter.Kind{good, {Name: "B", Schemes: []string{"a"}, Open: open}}, `both claim scheme "a"`},
		{"duplicate extension", []adapter.Kind{good, {Name: "B", Extensions: []string{"x"}, Open: open}}, `both claim extension "x"`},
		{"no name", []adapter.Kind{{Schemes: []string{"a"}, Open: open}}, "has no name"},
		{"no open", []adapter.Kind{{Name: "A", Schemes: []string{"a"}}}, "has no open function"},
		{"upper-case scheme", []adapter.Kind{{Name: "A", Schemes: []string{"Postgres"}, Open: open}}, "not lower case"},
		{"upper-case extension", []adapter.Kind{{Name: "A", Extensions: []string{"CSV"}, Open: open}}, "not lower case"},
		{"dotted extension", []adapter.Kind{{Name: "A", Extensions: []string{".csv"}, Open: open}}, "without dots"},
		{"empty claim", []adapter.Kind{{Name: "A", Extensions: []string{""}, Open: open}}, "empty extension"},
	}
	for _, c := range cases {
		err := adapter.CheckKinds(c.kinds)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: err = %v, want one containing %q", c.name, err, c.want)
		}
	}
}

func TestCheckKindsRejectsAnInconsistentForm(t *testing.T) {
	compose := func(adapter.Values, string) (string, error) { return "", nil }
	parse := func(string) (adapter.Values, bool, error) { return nil, false, nil }
	field := func(key string) adapter.Field { return adapter.Field{Key: key, Label: key} }
	kind := func(f adapter.Form) []adapter.Kind {
		return []adapter.Kind{{Name: "A", Schemes: []string{"a"}, Form: f, Open: open}}
	}
	whole := adapter.Form{Fields: []adapter.Field{field("host"), field("port")}, Compose: compose, Parse: parse}

	if err := adapter.CheckKinds(kind(whole)); err != nil {
		t.Errorf("a complete form was rejected: %v", err)
	}
	if err := adapter.CheckKinds(kind(adapter.Form{})); err != nil {
		t.Errorf("a Kind with no form at all was rejected: %v", err)
	}

	for _, c := range []struct {
		name string
		form adapter.Form
		want string
	}{
		{"empty key", adapter.Form{Fields: []adapter.Field{field("host"), field("")}, Compose: compose, Parse: parse}, "empty key"},
		{"duplicate key", adapter.Form{Fields: []adapter.Field{field("host"), field("host")}, Compose: compose, Parse: parse}, `field "host" twice`},
		{"fields without compose", adapter.Form{Fields: whole.Fields, Parse: parse}, "no compose function"},
		{"fields without parse", adapter.Form{Fields: whole.Fields, Compose: compose}, "no parse function"},
		{"fields with neither", adapter.Form{Fields: whole.Fields}, "no compose function"},
		{"compose without fields", adapter.Form{Compose: compose, Parse: parse}, "no fields"},
		{"parse without fields", adapter.Form{Parse: parse}, "no fields"},
	} {
		err := adapter.CheckKinds(kind(c.form))
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: err = %v, want one containing %q", c.name, err, c.want)
		}
	}
}
