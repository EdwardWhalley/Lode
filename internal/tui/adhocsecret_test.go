package tui_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/EdwardWhalley/lode/internal/history"
	"github.com/EdwardWhalley/lode/internal/source"
	"github.com/EdwardWhalley/lode/internal/tui"
)

// The DSN of `lode "postgres://u@db.example.com/app?password=hunter2"`. It is never opened:
// these tests are about what the program does with its Label.
var adHocWithASecret = source.Source{DSN: "postgres://u@db.example.com/app?password=hunter2"}

func adHocModel(t *testing.T, store *history.Store) *harness {
	t.Helper()
	m := tui.New(&fakeAdapter{total: 1}, adHocWithASecret.Label(), tui.WithHistory(store))
	h := &harness{t: t, m: m}
	h.send(tea.WindowSizeMsg{Width: 100, Height: 30})
	return h
}

func openHistory(t *testing.T, path string) *history.Store {
	t.Helper()
	store, err := history.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	return store
}

func TestAQueryOnAnAdHocSourceDoesNotWriteItsPasswordToTheHistoryFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history")
	h := adHocModel(t, openHistory(t, path))

	h.typeQuery("SELECT 1")
	h.runToCompletion()

	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "SELECT 1") {
		t.Fatalf("the query was not recorded:\n%s", b)
	}
	if strings.Contains(string(b), "hunter2") {
		t.Errorf("the history file holds the password:\n%s", b)
	}
	if strings.Contains(h.view(), "hunter2") {
		t.Errorf("the screen shows the password:\n%s", h.view())
	}
}

// An entry recorded before Labels hid the options is still that Source's history.
func TestAnOldEntryWithAPasswordStepsOnItsSourceAfterLoading(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history")
	old := `{"time":"2026-01-02T03:04:05Z","source":"postgres://u@db.example.com/app?password=hunter2","sql":"SELECT 'from before'"}` + "\n"
	if err := os.WriteFile(path, []byte(old), 0o600); err != nil {
		t.Fatal(err)
	}

	h := adHocModel(t, openHistory(t, path))
	h.send(tea.KeyMsg{Type: tea.KeyCtrlUp})

	if view := h.view(); !strings.Contains(view, "SELECT 'from before'") {
		t.Errorf("the old entry did not step:\n%s", view)
	}
}
