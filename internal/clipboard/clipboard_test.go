package clipboard

import (
	"bytes"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type fakeSystem struct {
	text                 string
	readErr, writeErr    error
	reads, writesAttempt int
}

func (f *fakeSystem) Read() (string, error) {
	f.reads++
	if f.readErr != nil {
		return "", f.readErr
	}
	return f.text, nil
}

func (f *fakeSystem) Write(text string) error {
	f.writesAttempt++
	if f.writeErr != nil {
		return f.writeErr
	}
	f.text = text
	return nil
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("the terminal is gone") }

func TestCopyPutsTheTextOnTheSystemClipboard(t *testing.T) {
	sys := &fakeSystem{}
	var osc bytes.Buffer
	c := New(sys, &osc)

	if err := c.Copy("select 1"); err != nil {
		t.Fatalf("Copy = %v, want nil", err)
	}
	if sys.text != "select 1" {
		t.Errorf("system clipboard holds %q", sys.text)
	}
	if osc.Len() != 0 {
		t.Errorf("OSC 52 was written (%q) although the system clipboard worked", osc.String())
	}
}

func TestCopyFallsBackToOSC52WhenTheSystemClipboardFails(t *testing.T) {
	sys := &fakeSystem{writeErr: errors.New("no xclip")}
	var osc bytes.Buffer
	c := New(sys, &osc)

	if err := c.Copy("hello"); err != nil {
		t.Fatalf("Copy = %v, want nil: the terminal took it", err)
	}
	if want := "\x1b]52;c;aGVsbG8=\x07"; osc.String() != want {
		t.Errorf("OSC 52 = %q, want %q", osc.String(), want)
	}
}

func TestCopyReportsWhenNothingOutsideLodeTookTheText(t *testing.T) {
	for name, c := range map[string]*Clipboard{
		"no outlets at all":        New(nil, nil),
		"system fails, no OSC 52":  New(&fakeSystem{writeErr: errors.New("no xclip")}, nil),
		"system and terminal fail": New(&fakeSystem{writeErr: errors.New("no xclip")}, failingWriter{}),
	} {
		err := c.Copy("kept")
		if err == nil {
			t.Errorf("%s: Copy = nil, want an error saying only lode has it", name)
		}
		if got, _ := c.Paste(); got != "kept" {
			t.Errorf("%s: the register holds %q, want %q whatever happened", name, got, "kept")
		}
	}
}

func TestPasteReadsTheSystemClipboard(t *testing.T) {
	sys := &fakeSystem{}
	c := New(sys, nil)
	if err := c.Copy("mine"); err != nil {
		t.Fatal(err)
	}
	sys.text = "from another program"

	got, err := c.Paste()
	if err != nil || got != "from another program" {
		t.Errorf("Paste = %q, %v; want the system clipboard's text", got, err)
	}
}

func TestPasteFallsBackToTheRegisterAndSaysWhy(t *testing.T) {
	sys := &fakeSystem{}
	c := New(sys, nil)
	if err := c.Copy("mine"); err != nil {
		t.Fatal(err)
	}
	sys.readErr = errors.New("no xclip")

	got, err := c.Paste()
	if got != "mine" {
		t.Errorf("Paste = %q, want the register's %q", got, "mine")
	}
	if err == nil {
		t.Error("Paste said nothing about the system clipboard failing")
	}
}

func TestPasteWithNoSystemClipboardUsesTheRegisterWithoutComplaint(t *testing.T) {
	c := New(nil, nil)
	_ = c.Copy("mine")
	if got, err := c.Paste(); got != "mine" || err != nil {
		t.Errorf("Paste = %q, %v; want %q and no error", got, err, "mine")
	}
}

func TestPasteAfterACopyThatMissedTheSystemClipboardUsesTheRegister(t *testing.T) {
	sys := &fakeSystem{text: "old", writeErr: errors.New("no xclip")}
	var osc bytes.Buffer
	c := New(sys, &osc)

	if err := c.Copy("new"); err != nil {
		t.Fatal(err)
	}
	if got, _ := c.Paste(); got != "new" {
		t.Errorf("Paste = %q, want %q (not the stale system clipboard)", got, "new")
	}
}

func TestANULIsRefusedBeforeItReachesTheSystemClipboard(t *testing.T) {
	if err := OS().Write("a\x00b"); err == nil {
		t.Error("expected a text holding a NUL to be refused")
	}
}

func TestOSC52IsTheSequenceTerminalsKnow(t *testing.T) {
	if got, want := osc52("héllo"), "\x1b]52;c;aMOpbGxv\x07"; got != want {
		t.Errorf("OSC52 = %q, want %q", got, want)
	}
	got := osc52("a\x07\x1b[2Jb")
	if inner := strings.TrimSuffix(strings.TrimPrefix(got, "\x1b]52;c;"), "\x07"); strings.ContainsAny(inner, "\x07\x1b") {
		t.Errorf("OSC52 let a control character through: %q", got)
	}
}

func TestAPasteWhileACopyIsStillInFlightGivesTheNewText(t *testing.T) {
	sys := &fakeSystem{text: "OLD"}
	c := New(sys, nil)

	finish := c.Begin("new")
	if got, _ := c.Paste(); got != "new" {
		t.Errorf("Paste before the copy finished = %q, want %q (not the old system clipboard)", got, "new")
	}

	if err := finish(); err != nil {
		t.Fatal(err)
	}
	if sys.text != "new" {
		t.Errorf("system clipboard holds %q once the copy finished", sys.text)
	}
	if got, _ := c.Paste(); got != "new" {
		t.Errorf("Paste after the copy finished = %q", got)
	}
}

func TestALateOlderCopyDoesNotReplaceANewerOne(t *testing.T) {
	sys := &fakeSystem{}
	c := New(sys, nil)

	older := c.Begin("A")
	newer := c.Begin("B")
	if err := newer(); err != nil {
		t.Fatal(err)
	}
	if err := older(); err != nil {
		t.Fatal(err)
	}

	if sys.text != "B" {
		t.Errorf("system clipboard holds %q, want the newer %q", sys.text, "B")
	}
	if got, _ := c.Paste(); got != "B" {
		t.Errorf("Paste = %q, want %q", got, "B")
	}
}

func TestAfterAMissedCopyAnotherProgramsCopyIsPastedNotLodes(t *testing.T) {
	sys := &fakeSystem{text: "old", writeErr: errors.New("no permission")}
	var osc bytes.Buffer
	c := New(sys, &osc)
	if err := c.Copy("mine"); err != nil {
		t.Fatal(err)
	}
	if got, _ := c.Paste(); got != "mine" {
		t.Fatalf("Paste straight after = %q, want %q", got, "mine")
	}

	sys.text = "copied somewhere else"

	if got, err := c.Paste(); got != "copied somewhere else" || err != nil {
		t.Errorf("Paste = %q, %v; want the other program's copy", got, err)
	}
}

func TestATextTooBigForTheTerminalIsNotSentToIt(t *testing.T) {
	sys := &fakeSystem{writeErr: errors.New("no xclip")}
	var osc bytes.Buffer
	c := New(sys, &osc)

	err := c.Copy(strings.Repeat("x", 80000))

	if err == nil {
		t.Error("Copy = nil, want an error: nothing outside lode took it")
	}
	if osc.Len() != 0 {
		t.Errorf("%d bytes of OSC 52 were sent for a text over the limit", osc.Len())
	}
	if got, _ := c.Paste(); len(got) != 80000 {
		t.Errorf("the register holds %d characters, want all 80000", len(got))
	}
}

// slowSystem's first Write blocks until release is closed, like a slow clipboard helper.
type slowSystem struct {
	mu      sync.Mutex
	text    string
	slowed  atomic.Bool
	started chan struct{}
	release chan struct{}
}

func (s *slowSystem) Read() (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.text, nil
}

func (s *slowSystem) Write(text string) error {
	if s.slowed.CompareAndSwap(false, true) {
		close(s.started)
		<-s.release
	}
	s.mu.Lock()
	s.text = text
	s.mu.Unlock()
	return nil
}

func TestASlowOlderCopyCannotLandAfterANewerOne(t *testing.T) {
	sys := &slowSystem{started: make(chan struct{}), release: make(chan struct{})}
	c := New(sys, nil)

	older := c.Begin("OLD")
	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); older() }()
	<-sys.started

	newer := c.Begin("NEW")
	newerDone := make(chan struct{})
	go func() { defer wg.Done(); newer(); close(newerDone) }()
	select {
	case <-newerDone:
	case <-time.After(100 * time.Millisecond):
	}
	close(sys.release)
	wg.Wait()

	if got, _ := sys.Read(); got != "NEW" {
		t.Errorf("the system clipboard holds %q, want the newer copy %q", got, "NEW")
	}
	if got, err := c.Paste(); got != "NEW" || err != nil {
		t.Errorf("Paste = %q, %v; want NEW", got, err)
	}
}
