// Package clipboard is lode's copy and paste: the register, the operating system's
// clipboard and OSC 52.
package clipboard

import (
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"

	sysclip "github.com/atotto/clipboard"
)

// maxOSC52 is the most text sent to a terminal, which caps the sequence.
const maxOSC52 = 74000

// System is the operating system's clipboard.
type System interface {
	Read() (string, error)
	Write(text string) error
}

type osClipboard struct{}

func (osClipboard) Read() (string, error) { return sysclip.ReadAll() }
func (osClipboard) Write(text string) error {
	if strings.ContainsRune(text, 0) {
		return errors.New("the system clipboard cannot hold a NUL character")
	}
	return sysclip.WriteAll(text)
}

// OS is the operating system's clipboard, which may fail on any call.
func OS() System { return osClipboard{} }

// osc52 is the escape sequence that sets a terminal's clipboard to text.
func osc52(text string) string {
	return "\x1b]52;c;" + base64.StdEncoding.EncodeToString([]byte(text)) + "\x07"
}

// Clipboard keeps the register and knows where else to put and find text; it is safe for
// several goroutines.
type Clipboard struct {
	system   System
	terminal io.Writer

	writing sync.Mutex

	mu          sync.Mutex
	register    string
	copies      int
	systemHasIt bool
	stale       string
	staleKnown  bool
}

// New returns a Clipboard; either the system clipboard or the terminal may be nil.
func New(system System, terminal io.Writer) *Clipboard {
	return &Clipboard{system: system, terminal: terminal, systemHasIt: true}
}

// Begin puts text in the register and returns the slow half, which writes it to the system
// clipboard or else the terminal.
func (c *Clipboard) Begin(text string) (finish func() error) {
	c.mu.Lock()
	c.copies++
	mine := c.copies
	c.register = text
	c.systemHasIt, c.staleKnown = false, false
	c.mu.Unlock()

	return func() error { return c.finish(mine, text) }
}

// Copy is Begin and its slow half together.
func (c *Clipboard) Copy(text string) error { return c.Begin(text)() }

// finish is the slow half of copy number mine: the system clipboard, else the terminal.
func (c *Clipboard) finish(mine int, text string) error {
	c.writing.Lock()
	defer c.writing.Unlock()
	if c.overtaken(mine) {
		return nil
	}

	var systemErr error
	if c.system != nil {
		if systemErr = c.system.Write(text); systemErr == nil {
			c.settle(mine, true, "", false)
			return nil
		}
	}
	stale, staleKnown := "", false
	if c.system != nil {
		if s, err := c.system.Read(); err == nil {
			stale, staleKnown = s, true
		}
	}
	c.settle(mine, c.system == nil, stale, staleKnown)

	if c.terminal != nil && len(text) <= maxOSC52 {
		if _, err := io.WriteString(c.terminal, osc52(text)); err == nil {
			return nil
		}
	}
	switch {
	case systemErr != nil:
		return fmt.Errorf("the system clipboard did not take it (%v); lode will paste it", systemErr)
	case c.terminal != nil && len(text) > maxOSC52:
		return errors.New("it is too big to send to the terminal; lode will paste it")
	default:
		return errors.New("there is no clipboard to copy to; lode will paste it")
	}
}

func (c *Clipboard) overtaken(mine int) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.copies != mine
}

// settle records what the slow half of copy number mine found, unless a newer copy
// has begun since.
func (c *Clipboard) settle(mine int, systemHasIt bool, stale string, staleKnown bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.copies == mine {
		c.systemHasIt, c.stale, c.staleKnown = systemHasIt, stale, staleKnown
	}
}

// Paste returns the system clipboard's text, unless lode's last copy is newer, and why it
// could not be read.
func (c *Clipboard) Paste() (string, error) {
	c.mu.Lock()
	register, systemHasIt, stale, staleKnown := c.register, c.systemHasIt, c.stale, c.staleKnown
	c.mu.Unlock()

	if c.system == nil {
		return register, nil
	}
	text, err := c.system.Read()
	switch {
	case err != nil:
		return register, fmt.Errorf("the system clipboard could not be read (%v); pasting lode's last copy", err)
	case systemHasIt:
		return text, nil
	case staleKnown && text != stale:
		return text, nil
	default:
		return register, nil
	}
}
