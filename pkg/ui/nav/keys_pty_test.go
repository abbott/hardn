//go:build linux || darwin

package nav

import (
	"bytes"
	"testing"
	"time"

	"github.com/charmbracelet/colorprofile"

	"github.com/abbott/hardn/internal/ptytest"
	"github.com/abbott/hardn/pkg/ui/component"
	"github.com/abbott/hardn/pkg/ui/prompt"
	"github.com/abbott/hardn/pkg/ui/term"
)

// TestChooseKeysOnATerminal: where stdin and stdout are a terminal, a menu
// choice is read key by key (prompt.Reader.AskChoice), so an arrow typed
// before the answer is no part of it; a question stays a line, its escape
// bytes and all.
func TestChooseKeysOnATerminal(t *testing.T) {
	m, s := ptytest.Open(t)
	go ptytest.Drain(m)
	var out bytes.Buffer
	p := Prompter{Ctx: component.NewCtx(term.Fixed(80, colorprofile.ANSI, false, true)), In: prompt.New(s, true), Out: &out}

	type chosen struct {
		key string
		err error
	}
	done := make(chan chosen, 1)
	go func() {
		o, err := p.Choose(sshMenu(new([]string)), choice)
		done <- chosen{o.Item.Key, err}
	}()
	ptytest.WaitKeys(t, s)
	if _, err := m.WriteString("\x1b[A2\r"); err != nil {
		t.Fatal(err)
	}
	select {
	case c := <-done:
		if c.err != nil || c.key != "2" {
			t.Errorf("Choose = %q, %v; want 2", c.key, c.err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Choose did not return")
	}

	// stdout not a terminal: the choice is a line, so the terminal keeps its
	// line editing while the prompt waits.
	notTTY := p
	env := term.Fixed(80, colorprofile.ANSI, false, true)
	env.TTY = false
	notTTY.Ctx = component.NewCtx(env)
	go func() {
		o, err := notTTY.Choose(sshMenu(new([]string)), choice)
		done <- chosen{o.Item.Key, err}
	}()
	for end := time.Now().Add(300 * time.Millisecond); time.Now().Before(end); time.Sleep(10 * time.Millisecond) {
		if !ptytest.Canonical(t, s) {
			t.Fatal("stdout not a terminal: the terminal was set for keys")
		}
	}
	for timeout, tick := time.After(5*time.Second), time.NewTicker(200*time.Millisecond); ; { // typed again until read
		if _, err := m.WriteString("2\n"); err != nil {
			t.Fatal(err)
		}
		select {
		case c := <-done:
			tick.Stop()
			if c.err != nil || c.key != "2" {
				t.Errorf("stdout not a terminal: Choose = %q, %v; want 2", c.key, c.err)
			}
			goto asked
		case <-tick.C:
		case <-timeout:
			t.Fatal("Choose did not return")
		}
	}
asked:
	asked := make(chan string, 1)
	go func() {
		_ = p.Ask("Username:", "Invalid", func(line string) error { asked <- line; return nil })
	}()
	timeout := time.After(5 * time.Second)
	for tick := time.NewTicker(200 * time.Millisecond); ; { // typed again until read: the question's flush may take the first
		if _, err := m.WriteString("\x1b[Ab\n"); err != nil {
			t.Fatal(err)
		}
		select {
		case line := <-asked:
			tick.Stop()
			if line != "\x1b[Ab" {
				t.Errorf("a question's line is %q, want %q", line, "\x1b[Ab")
			}
			return
		case <-tick.C:
		case <-timeout:
			t.Fatal("Ask did not return")
		}
	}
}
