// Package ptytest opens pseudo-terminals for tests: a master, where a test
// types and reads what the program writes to the terminal, and a slave,
// which the program reads as its stdin. It is for tests alone.
package ptytest

import (
	"os"
	"testing"
	"time"
)

// Drain reads the master's output, the slave's echo, until it is closed,
// so that a terminal whose output nothing reads never blocks a writer.
func Drain(m *os.File) {
	b := make([]byte, 1024)
	for {
		if _, err := m.Read(b); err != nil {
			return
		}
	}
}

// WaitKeys waits until the terminal f's line editing is off, as a program
// reading keys sets it, so that what the test types next is read as keys
// and not taken by the terminal's line editing first. It fails the test
// after five seconds.
func WaitKeys(t testing.TB, f *os.File) {
	t.Helper()
	for end := time.Now().Add(5 * time.Second); time.Now().Before(end); time.Sleep(5 * time.Millisecond) {
		if on, err := canonical(int(f.Fd())); err != nil {
			t.Fatalf("terminal attributes: %v", err)
		} else if !on {
			return
		}
	}
	t.Fatal("the terminal's line editing is still on")
}

// Canonical reports whether the terminal f's line editing is on, as it is
// while a program reads lines.
func Canonical(t testing.TB, f *os.File) bool {
	t.Helper()
	on, err := canonical(int(f.Fd()))
	if err != nil {
		t.Fatalf("terminal attributes: %v", err)
	}
	return on
}
