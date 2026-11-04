package session_test

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/EdwardWhalley/lode/internal/adapter"
	"github.com/EdwardWhalley/lode/internal/tui"
)

// editFile has a Source of each shape the edit form has to take.
const editFile = `# my databases

[sources.prod]  # primary
dsn = "postgres://reader:${PASSWORD}@db.example.com:5432/app?sslmode=require"
max_rows = 500

# the shop
[sources.shop]
dsn = "mysql://root:${MYSQLPW}@127.0.0.1:3306/shop?tls=skip-verify"
write = true

[sources.legacy]
dsn = "postgres://admin:hunter2-sekrit@db.example.com/old"

[sources.odd]
dsn = "postgres://db.example.com:notaport/d"

[sources.oddpw]
dsn = "postgres://u:hunter2-sekrit@db.example.com:notaport/d"

[sources.local]
path = "~/db/app.sqlite"
write = false
`

func editRig(t *testing.T) *rig {
	t.Helper()
	r := newRig(t, editFile)
	r.env["PASSWORD"], r.env["MYSQLPW"] = "pw", "pw"
	if _, err := r.s.Sources(); err != nil {
		t.Fatal(err)
	}
	return r
}

// asSaved is what the form sends back when the user changes nothing.
func asSaved(f tui.EditForm) tui.NewSource {
	n := tui.NewSource{
		Kind: f.Kind, Name: f.Name, Values: f.Values, Write: f.Write, MaxRows: f.MaxRows, Replaces: f.Name,
	}
	switch f.Placement {
	case tui.Hidden:
		n.From = tui.FromStored
	case tui.ShownAsText:
		n.From, n.Location = tui.FromText, f.Location
	}
	return n
}

func TestEditingAPostgresSourceShowsItsFieldsAndKeepsTheOptions(t *testing.T) {
	r := editRig(t)
	f, err := r.s.EditForm("named:prod")
	if err != nil {
		t.Fatal(err)
	}
	if f.Kind != r.kind("PostgreSQL") || f.Name != "prod" || f.Placement != tui.Placed || f.Write || f.MaxRows != 500 {
		t.Errorf("form = %+v", f)
	}
	want := adapter.Values{
		"host": "db.example.com", "port": "5432", "database": "app", "user": "reader",
		"password_var": "PASSWORD", "options": "sslmode=require",
	}
	if fmt.Sprint(f.Values) != fmt.Sprint(want) {
		t.Errorf("values = %v, want %v", f.Values, want)
	}
}

func TestEditingAHandWrittenMySQLSourceShowsItsFieldsAndKeepsTheOptions(t *testing.T) {
	r := editRig(t)
	f, err := r.s.EditForm("named:shop")
	if err != nil {
		t.Fatal(err)
	}
	if f.Kind != r.kind("MySQL") || !f.Write || f.Values["options"] != "tls=skip-verify" || f.Values["password_var"] != "MYSQLPW" {
		t.Errorf("form = %+v", f)
	}
}

func TestSavingAnEditFormThatWasNotChangedLeavesTheFileByteIdentical(t *testing.T) {
	for _, name := range []string{"prod", "shop", "legacy", "odd", "oddpw", "local"} {
		t.Run(name, func(t *testing.T) {
			r := editRig(t)
			f, err := r.s.EditForm("named:" + name)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := r.s.ReplaceSource(asSaved(f)); err != nil {
				t.Fatal(err)
			}
			if got := r.text(); got != editFile {
				t.Errorf("the file changed:\n%s", got)
			}
		})
	}
}

func TestALiteralPasswordIsNeverInTheFormAndAnUnchangedSaveKeepsIt(t *testing.T) {
	r := editRig(t)
	f, err := r.s.EditForm("named:legacy")
	if err != nil {
		t.Fatal(err)
	}
	if f.Placement != tui.PlacedKeepingPassword {
		t.Errorf("form = %+v, want a form that holds a password in the location", f)
	}
	if text := fmt.Sprintf("%+v", f); strings.Contains(text, "sekrit") {
		t.Errorf("the form carries the password: %s", text)
	}
	n := asSaved(f)
	n.Write = true
	if _, err := r.s.ReplaceSource(n); err != nil {
		t.Fatal(err)
	}
	want := strings.Replace(editFile, `old"`+"\n", `old"`+"\nwrite = true\n", 1)
	if got := r.text(); got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
}

func TestFillingTheVariableFieldReplacesALiteralPasswordWithTheVariable(t *testing.T) {
	r := editRig(t)
	f, _ := r.s.EditForm("named:legacy")
	n := asSaved(f)
	n.Values["password_var"] = "NEWPW"

	if _, err := r.s.ReplaceSource(n); !errors.Is(err, tui.ErrVariableUnset) || strings.Contains(err.Error(), "NEWPW") {
		t.Errorf("unset variable: err = %v, want ErrVariableUnset without the name", err)
	}
	if err := r.s.TestSource(t.Context(), n); !errors.Is(err, tui.ErrVariableUnset) {
		t.Errorf("TestSource with an unset variable = %v", err)
	}
	if r.text() != editFile {
		t.Error("a refused save wrote anyway")
	}

	r.env["NEWPW"] = "x"
	if _, err := r.s.ReplaceSource(n); err != nil {
		t.Fatal(err)
	}
	if got := r.text(); strings.Contains(got, "admin:hunter2") || !strings.Contains(got, `postgres://admin:${NEWPW}@db.example.com/old`) {
		t.Errorf("the literal password should be gone, the variable in its place:\n%s", got)
	}
}

func TestALocationTheKindCannotPlaceIsOneRawFieldAndSavesFaithfully(t *testing.T) {
	r := editRig(t)
	f, err := r.s.EditForm("named:odd")
	if err != nil {
		t.Fatal(err)
	}
	if f.Placement != tui.ShownAsText || f.Location != "postgres://db.example.com:notaport/d" || f.Kind != r.kind("PostgreSQL") {
		t.Errorf("form = %+v", f)
	}
	n := asSaved(f)
	n.Location = "postgres://db.example.com:5432/d"
	if _, err := r.s.ReplaceSource(n); err != nil {
		t.Fatal(err)
	}
	want := strings.Replace(editFile, "db.example.com:notaport/d", "db.example.com:5432/d", 1)
	if got := r.text(); got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
}

func TestARawLocationWithAPasswordIsNotShownAndIsKept(t *testing.T) {
	r := editRig(t)
	f, err := r.s.EditForm("named:oddpw")
	if err != nil {
		t.Fatal(err)
	}
	if f.Placement != tui.Hidden || f.Location != "" {
		t.Errorf("form = %+v, want a raw form that does not show the location", f)
	}
	if text := fmt.Sprintf("%+v", f); strings.Contains(text, "sekrit") {
		t.Errorf("the form carries the password: %s", text)
	}
}

// pathWithAScheme is a path, by its key, that has a DSN's look.
const pathWithAScheme = "[sources.odd]\npath = \"x://data.sqlite\"\n"

func TestAStoredPathThatLooksLikeADSNStaysAPathWhenItsLocationIsNotChanged(t *testing.T) {
	r := newRig(t, pathWithAScheme)
	if _, err := r.s.Sources(); err != nil {
		t.Fatal(err)
	}
	f, err := r.s.EditForm("named:odd")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.s.ReplaceSource(asSaved(f)); err != nil {
		t.Fatal(err)
	}
	if got := r.text(); got != pathWithAScheme {
		t.Errorf("saved with nothing changed:\n%s", got)
	}

	n := asSaved(f)
	n.MaxRows = 50
	if _, err := r.s.ReplaceSource(n); err != nil {
		t.Fatal(err)
	}
	if got, want := r.text(), pathWithAScheme+"max_rows = 50\n"; got != want {
		t.Errorf("saved with Max rows changed:\n%s", got)
	}
}

func TestALocationChangedInTheFormIsClassifiedByWhatItHolds(t *testing.T) {
	r := newRig(t, pathWithAScheme)
	if _, err := r.s.Sources(); err != nil {
		t.Fatal(err)
	}
	f, err := r.s.EditForm("named:odd")
	if err != nil {
		t.Fatal(err)
	}
	n := asSaved(f)
	n.Values = adapter.Values{"path": "plain/other.sqlite"}
	if _, err := r.s.ReplaceSource(n); err != nil {
		t.Fatal(err)
	}
	if got, want := r.text(), "[sources.odd]\npath = \"plain/other.sqlite\"\n"; got != want {
		t.Errorf("a new path:\n%s", got)
	}
	n.Values = adapter.Values{"path": "y://other.sqlite"}
	if _, err := r.s.ReplaceSource(n); err != nil {
		t.Fatal(err)
	}
	if got, want := r.text(), "[sources.odd]\ndsn = \"y://other.sqlite\"\n"; got != want {
		t.Errorf("a changed location that looks like a DSN:\n%s", got)
	}
}

func TestKeepingTheStoredLocationNeedsASourceThatIsStored(t *testing.T) {
	r := editRig(t)
	n := tui.NewSource{Kind: r.kind("PostgreSQL"), Name: "fresh", From: tui.FromStored}
	err := r.s.TestSource(t.Context(), n)
	if err == nil || err.Error() != "there is no stored location to keep" {
		t.Errorf("a new Source asked to keep a stored location: err = %v", err)
	}
	n.Replaces = "gone"
	if err := r.s.TestSource(t.Context(), n); err == nil || err.Error() != "that Source is no longer in sources.toml" {
		t.Errorf("a stored location of a Source that is not in the file: err = %v", err)
	}
}

func TestARawLocationMustNotBeEmpty(t *testing.T) {
	r := editRig(t)
	f, _ := r.s.EditForm("named:odd")
	n := asSaved(f)
	n.Location = "  "
	if _, err := r.s.ReplaceSource(n); err == nil {
		t.Error("an empty location was saved")
	}
	if r.text() != editFile {
		t.Error("the refused save wrote anyway")
	}
}

func TestRenamingASourceMovesOnlyItsHeader(t *testing.T) {
	r := editRig(t)
	f, _ := r.s.EditForm("named:prod")
	n := asSaved(f)
	n.Name = "live"
	c, err := r.s.ReplaceSource(n)
	if err != nil {
		t.Fatal(err)
	}
	if c.ID != "named:live" || c.Name != "live" {
		t.Errorf("choice = %+v", c)
	}
	want := strings.Replace(editFile, "[sources.prod]", "[sources.live]", 1)
	if got := r.text(); got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
	list, err := r.s.Sources()
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, c := range list {
		names = append(names, c.Name)
	}
	if strings.Contains(strings.Join(names, " "), "prod") || !strings.Contains(strings.Join(names, " "), "live") {
		t.Errorf("Sources after the rename = %v", names)
	}
}

func TestRenamingOverAnotherSourceIsRefused(t *testing.T) {
	r := editRig(t)
	f, _ := r.s.EditForm("named:prod")
	n := asSaved(f)
	n.Name = "shop"
	if _, err := r.s.ReplaceSource(n); !errors.Is(err, tui.ErrNameTaken) {
		t.Errorf("err = %v, want ErrNameTaken", err)
	}
	if r.text() != editFile {
		t.Error("the refused rename wrote anyway")
	}
}

func TestTestingAnEditedSourceUsesTheStoredPasswordAndNeverRepeatsIt(t *testing.T) {
	r := newRig(t, "[sources.pg]\ndsn = \"postgres://u:hunter2-sekrit@127.0.0.1:1/d?sslmode=disable\"\n")
	if _, err := r.s.Sources(); err != nil {
		t.Fatal(err)
	}
	f, err := r.s.EditForm("named:pg")
	if err != nil {
		t.Fatal(err)
	}
	err = r.s.TestSource(t.Context(), asSaved(f))
	if err == nil {
		t.Fatal("a Source nothing listens for opened")
	}
	if strings.Contains(err.Error(), "sekrit") {
		t.Errorf("the error repeats the password: %v", err)
	}
}

func TestAnEditedFileSourceCanBeTestedAndSaved(t *testing.T) {
	db := makeSQLite(t)
	r := newRig(t, "[sources.mine]\npath = \"x.db\"\n")
	if _, err := r.s.Sources(); err != nil {
		t.Fatal(err)
	}
	f, _ := r.s.EditForm("named:mine")
	n := asSaved(f)
	n.Values = adapter.Values{"path": db}
	if err := r.s.TestSource(t.Context(), n); err != nil {
		t.Fatalf("TestSource = %v", err)
	}
	if _, err := r.s.ReplaceSource(n); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(r.text(), "path = ") || strings.Contains(r.text(), "x.db") {
		t.Errorf("file:\n%s", r.text())
	}
}

func TestAnEditOfAFileChangedSinceItWasReadIsRefusedAndTheRetryWorks(t *testing.T) {
	r := editRig(t)
	f, _ := r.s.EditForm("named:prod")
	other := editFile + "\n[sources.new]\npath = \"y.csv\"\n"
	r.write(other)
	n := asSaved(f)
	n.Write = true
	_, err := r.s.ReplaceSource(n)
	if err == nil || !strings.Contains(err.Error(), "changed") {
		t.Fatalf("err = %v, want a refusal saying the file changed", err)
	}
	if r.text() != other {
		t.Error("a refused save wrote anyway")
	}
	if _, err := r.s.ReplaceSource(n); err != nil {
		t.Errorf("retry = %v", err)
	}
	if got := r.text(); !strings.Contains(got, "[sources.new]") || !strings.Contains(got, "write = true") {
		t.Errorf("the other edit and ours should both be there:\n%s", got)
	}
}

func TestEditFormRefusesWhatCannotBeEdited(t *testing.T) {
	r := editRig(t)
	if _, err := r.s.EditForm("adhoc"); err == nil {
		t.Error("the ad-hoc Source has a form")
	}
	if _, err := r.s.EditForm("named:nope"); err == nil {
		t.Error("a Source that is not there has a form")
	}
	h := newRig(t, "sources = { a = { path = \"x.csv\" } }\n")
	if _, err := h.s.Sources(); err != nil {
		t.Fatal(err)
	}
	if _, err := h.s.EditForm("named:a"); err == nil || !strings.Contains(err.Error(), "by hand") {
		t.Errorf("an inline table: err = %v, want advice to edit by hand", err)
	}
	u := newRig(t, "[sources.strange]\npath = \"x.unheardof\"\n")
	if _, err := u.s.Sources(); err != nil {
		t.Fatal(err)
	}
	if _, err := u.s.EditForm("named:strange"); err == nil || !strings.Contains(err.Error(), "by hand") {
		t.Errorf("a Source no Kind claims: err = %v, want advice to edit by hand", err)
	}
}

func TestRemovingASourceRemovesOnlyItsSection(t *testing.T) {
	r := editRig(t)
	if err := r.s.RemoveSource("named:shop"); err != nil {
		t.Fatal(err)
	}
	want := strings.Replace(editFile, "# the shop\n[sources.shop]\ndsn = \"mysql://root:${MYSQLPW}@127.0.0.1:3306/shop?tls=skip-verify\"\nwrite = true\n\n", "", 1)
	if want == editFile {
		t.Fatal("the test's expectation did not remove anything")
	}
	if got := r.text(); got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
	list, err := r.s.Sources()
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range list {
		if c.Name == "shop" {
			t.Error("the removed Source is still listed")
		}
	}
}

func TestRemovingRefusesTheAdHocSourceAndOneThatIsNotThere(t *testing.T) {
	r := editRig(t)
	for _, id := range []string{"adhoc", "named:nope", "weird"} {
		if err := r.s.RemoveSource(id); err == nil {
			t.Errorf("RemoveSource(%q) succeeded", id)
		}
	}
	if r.text() != editFile {
		t.Error("a refused remove wrote anyway")
	}
}

func TestRemovingFromAFileChangedSinceItWasReadIsRefusedAndTheRetryWorks(t *testing.T) {
	r := editRig(t)
	other := editFile + "\n[sources.new]\npath = \"y.csv\"\n"
	r.write(other)
	err := r.s.RemoveSource("named:shop")
	if err == nil || !strings.Contains(err.Error(), "changed") {
		t.Fatalf("err = %v, want a refusal saying the file changed", err)
	}
	if r.text() != other {
		t.Error("a refused remove wrote anyway")
	}
	if err := r.s.RemoveSource("named:shop"); err != nil {
		t.Errorf("retry = %v", err)
	}
	if got := r.text(); strings.Contains(got, "shop") || !strings.Contains(got, "[sources.new]") {
		t.Errorf("file:\n%s", got)
	}
}

func TestRemovingASourceWrittenByHandAsAnInlineTableSaysToEditItByHand(t *testing.T) {
	r := newRig(t, "sources = { a = { path = \"x.csv\" } }\n")
	if _, err := r.s.Sources(); err != nil {
		t.Fatal(err)
	}
	if err := r.s.RemoveSource("named:a"); err == nil || !strings.Contains(err.Error(), "by hand") {
		t.Errorf("err = %v, want advice to edit by hand", err)
	}
}

func TestNoErrorOnTheEditAndRemovePathsRepeatsAPassword(t *testing.T) {
	r := editRig(t)
	f, _ := r.s.EditForm("named:legacy")
	n := asSaved(f)
	n.Name = "bad name"
	_, err1 := r.s.ReplaceSource(n)
	n.Name = "shop"
	_, err2 := r.s.ReplaceSource(n)
	n.Name, n.Replaces = "legacy2", "gone"
	_, err3 := r.s.ReplaceSource(n)
	for i, err := range []error{err1, err2, err3, r.s.RemoveSource("named:gone")} {
		if err == nil {
			t.Errorf("case %d: no error", i)
		} else if strings.Contains(err.Error(), "sekrit") {
			t.Errorf("case %d repeats the password: %v", i, err)
		}
	}
}
