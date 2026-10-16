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
// x/term's IsTerminal does (term_unix.go:13-16, TCGETS on Linux and
// TIOCGETA on macOS), so the two agree. This package cannot ask term,
// which links Lip Gloss (see the package doc).
func Stdin() *Reader { return stdin() }

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
