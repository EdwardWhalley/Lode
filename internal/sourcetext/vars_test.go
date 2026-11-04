package sourcetext_test

import (
	"reflect"
	"testing"

	"github.com/EdwardWhalley/lode/internal/sourcetext"
)

func TestVariableNames(t *testing.T) {
	for _, tc := range []struct {
		name string
		ok   bool
	}{
		{"PW", true},
		{"pw", true},
		{"_", true},
		{"_x", true},
		{"a1", true},
		{"A_B_9", true},
		{"1a", false},
		{"9", false},
		{"a-b", false},
		{"a.b", false},
		{"a b", false},
		{"é", false},
		{"a$", false},
		{"${PW}", false},
		{"PW}", false},
		{"", false},
		{"A\n", false},
	} {
		if got := sourcetext.ValidVarName(tc.name); got != tc.ok {
			t.Errorf("ValidVarName(%q) = %v, want %v", tc.name, got, tc.ok)
		}
	}
}

func TestReferencesInAText(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   string
		want []sourcetext.Ref
	}{
		{"none", "postgres://u@h", nil},
		{"empty text", "", nil},
		{"one", "${A}", []sourcetext.Ref{{Start: 0, End: 4, Name: "A"}}},
		{"text around", "x${A}y", []sourcetext.Ref{{Start: 1, End: 5, Name: "A"}}},
		{"two in a row", "${A}${B}", []sourcetext.Ref{{Start: 0, End: 4, Name: "A"}, {Start: 4, End: 8, Name: "B"}}},
		{"two apart", "${A}-${B_2}", []sourcetext.Ref{{Start: 0, End: 4, Name: "A"}, {Start: 5, End: 11, Name: "B_2"}}},
		{"name starting with a digit", "${1A}", nil},
		{"empty braces", "${}", nil},
		{"unclosed", "${A", nil},
		{"unclosed before a reference", "${A ${B}", []sourcetext.Ref{{Start: 4, End: 8, Name: "B"}}},
		{"nested finds the inner one", "${${A}}", []sourcetext.Ref{{Start: 2, End: 6, Name: "A"}}},
		{"a literal $NAME", "pa$A$$w{rd}", nil},
		{"a $ before a reference", "$${A}", []sourcetext.Ref{{Start: 1, End: 5, Name: "A"}}},
	} {
		if got := sourcetext.References(tc.in); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%s: References(%q) = %+v, want %+v", tc.name, tc.in, got, tc.want)
		}
	}
}

func TestExactlyOneReference(t *testing.T) {
	for _, tc := range []struct {
		in   string
		name string
		ok   bool
	}{
		{"${A}", "A", true},
		{"${PROD_PASSWORD}", "PROD_PASSWORD", true},
		{"${A}x", "", false},
		{"x${A}", "", false},
		{" ${A}", "", false},
		{"${A}${B}", "", false},
		{"${${A}}", "", false},
		{"${1A}", "", false},
		{"${}", "", false},
		{"${A", "", false},
		{"$A", "", false},
		{"pa$$w{rd}", "", false},
		{"", "", false},
	} {
		name, ok := sourcetext.SoleReference(tc.in)
		if name != tc.name || ok != tc.ok {
			t.Errorf("SoleReference(%q) = %q, %v; want %q, %v", tc.in, name, ok, tc.name, tc.ok)
		}
	}
}

func TestMapAroundLeavesReferencesAsWritten(t *testing.T) {
	wrap := func(s string) string { return "[" + s + "]" }
	for _, tc := range []struct{ in, want string }{
		{"", ""},
		{"abc", "[abc]"},
		{"${A}", "${A}"},
		{"a${A}b", "[a]${A}[b]"},
		{"${A}${B}", "${A}${B}"},
		{"a${A}${B}", "[a]${A}${B}"},
		{"${A}b${B}", "${A}[b]${B}"},
		{"${1A}", "[${1A}]"},
		{"${A", "[${A]"},
		{"${}", "[${}]"},
		{"${${A}}", "[${]${A}[}]"},
		{"$A", "[$A]"},
	} {
		if got := sourcetext.MapAround(tc.in, wrap); got != tc.want {
			t.Errorf("MapAround(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestExpandAnswersEachReferenceFromTheLookup(t *testing.T) {
	values := map[string]string{"A": "1", "B": "${A}", "C": "p$w"}
	lookup := func(name string) (string, bool) { v, ok := values[name]; return v, ok }

	for _, tc := range []struct {
		in      string
		want    string
		missing []string
	}{
		{"", "", nil},
		{"plain", "plain", nil},
		{"${A}", "1", nil},
		{"u:${A}@${C}/d", "u:1@p$w/d", nil},
		{"${A}${A}", "11", nil},
		{"${B}", "${A}", nil},
		{"$A and ${1A} and ${A and ${}", "$A and ${1A} and ${A and ${}", nil},
		{"${${A}}", "${1}", nil},
		{"${X}", "", []string{"X"}},
		{"a${A}${X}b${Y}", "a1b", []string{"X", "Y"}},
		{"${X}${X}", "", []string{"X", "X"}},
	} {
		got, missing := sourcetext.Expand(tc.in, lookup)
		if got != tc.want {
			t.Errorf("Expand(%q) = %q, want %q", tc.in, got, tc.want)
		}
		if !reflect.DeepEqual(missing, tc.missing) {
			t.Errorf("Expand(%q) missing = %v, want %v", tc.in, missing, tc.missing)
		}
	}
}

func TestReferenceWritesWhatTheReadersFind(t *testing.T) {
	for _, name := range []string{"A", "PROD_PASSWORD", "_x9"} {
		text := sourcetext.Reference(name)
		if text != "${"+name+"}" {
			t.Errorf("Reference(%q) = %q", name, text)
		}
		if got, ok := sourcetext.SoleReference(text); !ok || got != name {
			t.Errorf("SoleReference(%q) = %q, %v; want %q", text, got, ok, name)
		}
	}
}
