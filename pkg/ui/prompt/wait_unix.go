//go:build linux || darwin

package prompt

import (
	"errors"
	"fmt"
	"os"
	"syscall"

	"golang.org/x/sys/unix"
)

// interruptible reports whether ReadLine can wait on a file here, so that
// Ctrl-C wakes it.
const interruptible = true

// wait blocks until c has something to read, returning nil, or until wake
// has, returning errWoken. c's descriptor is taken with SyscallConn, which
// leaves the file as it is (File.Fd would set it to blocking mode).
func wait(c syscall.Conn, wake *os.File) error {
	rc, err := c.SyscallConn()
	if err != nil {
		return fmt.Errorf("prompt: %w", err)
	}
	w := int(wake.Fd())
	var werr error
	if err := rc.Control(func(fd uintptr) { werr = selectRead(int(fd), w) }); err != nil {
		return fmt.Errorf("prompt: %w", err)
	}
	return werr
}

// selectRead waits with select(2) until fd or wake is readable. When both
// are, wake wins, so an interrupt reads nothing. Readable means a read
// will not block: a terminal has a whole line or an end of input, a pipe
// data or its end, and a regular file always.
func selectRead(fd, wake int) error {
	if fd >= unix.FD_SETSIZE || wake >= unix.FD_SETSIZE {
		return fmt.Errorf("prompt: descriptor %d is beyond select's limit of %d", max(fd, wake), unix.FD_SETSIZE)
	}
	for {
		var set unix.FdSet
		set.Set(fd)
		set.Set(wake)
		_, err := unix.Select(max(fd, wake)+1, &set, nil, nil, nil)
		switch {
		case errors.Is(err, unix.EINTR):
			continue
		case err != nil:
			return fmt.Errorf("prompt: select: %w", err)
		case set.IsSet(wake):
			return errWoken
		}
		return nil
	}
}
