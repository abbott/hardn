package prompt

import (
	"os"
	"sync"
	"syscall"
)

// stdin builds the process-wide Reader on its first call (Stdin).
var stdin = sync.OnceValue(func() *Reader {
	return New(os.Stdin, isTerminal(os.Stdin))
})

// Stdin returns the process-wide Reader over os.Stdin, built on the first
// call and the same at every call after it. Every prompt in hardn reads
// through it: the legacy menus (pkg/menu), the first-run config prompts
// (pkg/config) and the new screens (nav.Prompter.In) alike. stdin then
// has one buffer, so no line one reader took ahead is lost to another
// (refactor plan, risk R17).
//
// It is a terminal Reader when stdin is a terminal, asked as term.Detect
// asks it for term.Env.InputTTY: by reading the terminal's attributes, as
// IsTerminal does in github.com/charmbracelet/x/term v0.2.2
// (term_unix.go:14-17; TCGETS on Linux, term_unix_other.go:9, and
// TIOCGETA on macOS, term_unix_bsd.go:9). On Linux and macOS the two
// therefore agree; elsewhere Stdin is never a terminal Reader
// (tty_other.go). This package cannot ask term, which links Lip Gloss
// (see the package doc).
func Stdin() *Reader { return stdin() }

// IsTerminal reports whether f is a terminal, asked as Stdin asks it of
// os.Stdin (isTerminal): the first-run question (pkg/config) asks it of
// stdin and stdout, as it is asked only when both are one.
func IsTerminal(f *os.File) bool { return isTerminal(f) }

// isTerminal reports whether in is a file whose descriptor is a terminal.
// The descriptor is taken with SyscallConn, which leaves the file as it
// is (File.Fd would set it to blocking mode).
func isTerminal(in any) bool {
	c, ok := in.(syscall.Conn)
	if !ok {
		return false
	}
	rc, err := c.SyscallConn()
	if err != nil {
		return false
	}
	var tty bool
	if err := rc.Control(func(fd uintptr) { tty = terminalFd(int(fd)) }); err != nil {
		return false
	}
	return tty
}
