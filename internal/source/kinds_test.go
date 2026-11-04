package source

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/EdwardWhalley/lode/internal/adapter"
)

func TestKindsAreConsistent(t *testing.T) {
	if err := adapter.CheckKinds(Kinds); err != nil {
		t.Fatal(err)
	}
}

func kindNamed(t *testing.T, name string) adapter.Kind {
	t.Helper()
	for _, k := range Kinds {
		if k.Name == name {
			return k
		}
	}
	t.Fatalf("no Kind named %q", name)
	return adapter.Kind{}
}

func TestEachKindDescribesItsFormFields(t *testing.T) {
	type field struct {
		key      string
		typ      adapter.FieldType
		required bool
		def      string
	}
	server := func(port string) []field {
		return []field{
			{"host", adapter.Text, true, ""},
			{"port", adapter.Number, false, port},
			{"database", adapter.Text, false, ""},
			{"user", adapter.Text, false, ""},
			{"password_var", adapter.EnvVar, false, ""},
			{"options", adapter.Text, false, ""},
		}
	}
	path := []field{{"path", adapter.Text, true, ""}}

	for name, want := range map[string][]field{
		"file":       path,
		"SQLite":     path,
		"PostgreSQL": server("5432"),
		"MySQL":      server("3306"),
	} {
		k := kindNamed(t, name)
		var got []field
		for _, f := range k.Fields {
			got = append(got, field{f.Key, f.Type, f.Required, f.Default})
			if f.Label == "" || f.Help == "" {
				t.Errorf("%s: field %q needs a label and a help line", name, f.Key)
			}
			if f.Type == adapter.Number && (f.Min != 1 || f.Max != 65535) {
				t.Errorf("%s: field %q range = %d..%d, want 1..65535", name, f.Key, f.Min, f.Max)
			}
		}
		if fmt.Sprint(got) != fmt.Sprint(want) {
			t.Errorf("%s: fields = %v, want %v", name, got, want)
		}
	}
}

func TestTheFileKindClaimsParquetInAnyCase(t *testing.T) {
	for _, path := range []string{"data.parquet", "data.PARQUET", "data.Parquet"} {
		if k, ok := KindOf(Source{Path: path}); !ok || k.Name != "file" {
			t.Errorf("%s: kind %q, ok %v; want the file Kind", path, k.Name, ok)
		}
	}
}

func TestUnclaimedSourcesGetOneErrorListingWhatIsSupported(t *testing.T) {
	for _, s := range []Source{
		{DSN: "sqlserver://u:hunter2@h/d"},
		{DSN: "not a url hunter2"},
		{DSN: "postgres://u:hunter2@h:notaport/d"},
		{Path: "data.xlsx"},
		{Path: "noextension"},
	} {
		_, err := Open(s)
		if err == nil {
			t.Errorf("%+v: opened, want an error", s)
			continue
		}
		msg := err.Error()
		for _, want := range []string{"no Adapter for", "postgres", "mysql", "sqlite", "csv", "parquet"} {
			if !strings.Contains(msg, want) {
				t.Errorf("%+v: %q does not mention %q", s, msg, want)
			}
		}
		if strings.Contains(msg, "hunter2") {
			t.Errorf("%+v: error leaks the password: %q", s, msg)
		}
	}
}

func TestSchemesAndExtensionsMatchWhateverTheCase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "A.CSV")
	if err := os.WriteFile(path, []byte("n\n1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	a, err := Open(Source{Path: path})
	if err != nil {
		t.Fatalf("upper-case extension: %v", err)
	}
	a.Close()

	_, err = Open(Source{DSN: "MySQL://u:hunter2@127.0.0.1:1/d"})
	if err == nil || !strings.Contains(err.Error(), "MySQL") {
		t.Errorf("upper-case scheme: err = %v, want a MySQL connection error", err)
	}
}

type fake struct{ adapter.Adapter }

func TestAnAdapterOnlyInTheListOpensThroughTheNormalPath(t *testing.T) {
	var got adapter.Request
	kinds := []adapter.Kind{
		{
			Name:       "fake",
			Schemes:    []string{"fake"},
			Extensions: []string{"fk"},
			Open: func(r adapter.Request) (adapter.Adapter, error) {
				got = r
				return fake{}, nil
			},
		},
	}
	if err := adapter.CheckKinds(kinds); err != nil {
		t.Fatal(err)
	}

	for _, s := range []Source{{DSN: "FAKE://host/db", Write: true}, {Path: "x.FK", Write: true}} {
		a, err := open(kinds, s, WithRowCap(7))
		if err != nil {
			t.Fatalf("%+v: %v", s, err)
		}
		if _, ok := a.(fake); !ok {
			t.Errorf("%+v: opened %T, want the fake", s, a)
		}
		want := adapter.Request{Location: s.DSN + s.Path, Write: true, RowCap: 7}
		if got != want {
			t.Errorf("%+v: request = %+v, want %+v", s, got, want)
		}
	}

	if _, err := open(kinds, Source{Path: "x.csv"}); err == nil {
		t.Error("a path with an extension nobody claims should not open")
	}
}

func TestAnAdapterOnlyInTheListHasItsOwnFormFields(t *testing.T) {
	var opened adapter.Request
	kinds := []adapter.Kind{{
		Name:    "fake",
		Schemes: []string{"fake"},
		Form: adapter.Form{
			Fields: []adapter.Field{
				{Key: "account", Label: "Account", Type: adapter.Text, Required: true},
				{Key: "region", Label: "Region", Type: adapter.Text, Default: "eu"},
			},
			Compose: func(v adapter.Values, _ string) (string, error) {
				return "fake://" + v["account"] + "@" + v["region"], nil
			},
			Parse: func(location string) (adapter.Values, bool, error) {
				rest, ok := strings.CutPrefix(location, "fake://")
				account, region, found := strings.Cut(rest, "@")
				if !ok || !found {
					return nil, false, fmt.Errorf("not a fake location")
				}
				return adapter.Values{"account": account, "region": region}, false, nil
			},
		},
		Open: func(r adapter.Request) (adapter.Adapter, error) {
			opened = r
			return fake{}, nil
		},
	}}
	if err := adapter.CheckKinds(append(kinds, Kinds...)); err != nil {
		t.Fatal(err)
	}

	k := kinds[0]
	if k.Fields[1].Default != "eu" {
		t.Errorf("default = %q", k.Fields[1].Default)
	}
	location, err := k.Compose(adapter.Values{"account": "acme", "region": "us"}, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := open(kinds, Source{DSN: location}); err != nil || opened.Location != "fake://acme@us" {
		t.Fatalf("opened %+v, err %v; want the composed location", opened, err)
	}
	values, literal, err := k.Parse(opened.Location)
	if err != nil || literal || values["account"] != "acme" || values["region"] != "us" {
		t.Errorf("Parse = %v, %v, %v", values, literal, err)
	}
	if _, _, err := k.Parse("other://x"); err == nil {
		t.Error("a location the Kind cannot parse should say so")
	}
}
