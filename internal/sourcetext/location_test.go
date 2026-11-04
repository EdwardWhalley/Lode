package sourcetext_test

import (
	"errors"
	"testing"

	"github.com/EdwardWhalley/lode/internal/sourcetext"
)

func TestWhichOfPathAndDSNIsSet(t *testing.T) {
	for _, tc := range []struct {
		name, path, dsn string
		text            string
		isDSN           bool
	}{
		{"a path", "data/x.csv", "", "data/x.csv", false},
		{"a DSN", "", "postgres://u@h/d", "postgres://u@h/d", true},
		{"neither", "", "", "", false},
		{"both: the DSN wins, as it always has", "x.csv", "postgres://u@h/d", "postgres://u@h/d", true},
		{"a path that holds ://", "a://b", "", "a://b", false},
		{"a Windows path", `C:\data\x.csv`, "", `C:\data\x.csv`, false},
	} {
		text, isDSN := sourcetext.Of(tc.path, tc.dsn)
		if text != tc.text || isDSN != tc.isDSN {
			t.Errorf("%s: Of(%q, %q) = %q, %v; want %q, %v", tc.name, tc.path, tc.dsn, text, isDSN, tc.text, tc.isDSN)
		}
		if got := sourcetext.Text(tc.path, tc.dsn); got != tc.text {
			t.Errorf("%s: Text(%q, %q) = %q, want %q", tc.name, tc.path, tc.dsn, got, tc.text)
		}
	}
}

func TestExactlyOneOfPathAndDSN(t *testing.T) {
	for _, tc := range []struct {
		name, path, dsn string
		want            error
	}{
		{"a path", "x.csv", "", nil},
		{"a DSN", "", "postgres://u@h", nil},
		{"neither", "", "", sourcetext.ErrNoLocation},
		{"both", "x.csv", "postgres://u@h", sourcetext.ErrBothLocations},
	} {
		if err := sourcetext.CheckOne(tc.path, tc.dsn); !errors.Is(err, tc.want) || (tc.want == nil) != (err == nil) {
			t.Errorf("%s: CheckOne = %v, want %v", tc.name, err, tc.want)
		}
	}
	if sourcetext.ErrNoLocation.Error() != "needs a path or a dsn" || sourcetext.ErrBothLocations.Error() != "has both a path and a dsn; use one" {
		t.Errorf("messages are %q and %q", sourcetext.ErrNoLocation, sourcetext.ErrBothLocations)
	}
}

func TestTypedTextIsADSNIfItHasAScheme(t *testing.T) {
	for _, tc := range []struct {
		typed string
		isDSN bool
	}{
		{"postgres://u:${PW}@h/d", true},
		{"mysql://h", true},
		{"data/x.csv", false},
		{`C:\data\x.csv`, false},
		{"C:/data/x.csv", false},
		{"", false},
		{"postgres:", false},
		{"postgres:/h", false},
		{"a://b", true},
		{"://h", true},
	} {
		if got := sourcetext.LooksLikeDSN(tc.typed); got != tc.isDSN {
			t.Errorf("LooksLikeDSN(%q) = %v, want %v", tc.typed, got, tc.isDSN)
		}
	}
}

func TestSplitScheme(t *testing.T) {
	for _, tc := range []struct {
		in           string
		scheme, rest string
		ok           bool
	}{
		{"postgres://u:${PW}@h/d?a=b", "postgres", "u:${PW}@h/d?a=b", true},
		{"MySQL://h", "MySQL", "h", true},
		{"://h", "", "h", true},
		{"a://b://c", "a", "b://c", true},
		{"data/x.csv", "", "", false},
		{"postgres:/h", "", "", false},
		{"", "", "", false},
	} {
		scheme, rest, ok := sourcetext.SplitScheme(tc.in)
		if scheme != tc.scheme || rest != tc.rest || ok != tc.ok {
			t.Errorf("SplitScheme(%q) = %q, %q, %v; want %q, %q, %v", tc.in, scheme, rest, ok, tc.scheme, tc.rest, tc.ok)
		}
	}
}
