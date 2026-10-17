package prompt

import "golang.org/x/sys/unix"

// fread is FREAD from <sys/fcntl.h> (0x00000001), which x/sys does not
// define: TIOCFLUSH's argument selecting the input queue.
const fread = 0x1

// flushInput discards the terminal fd's input queue: what was typed and
// not yet read. TIOCFLUSH is _IOW('t', 16, int) (<sys/ttycom.h>), so it
// takes a pointer to an int holding FREAD; FWRITE would flush output, and
// 0 both.
func flushInput(fd int) error {
	return unix.IoctlSetPointerInt(fd, unix.TIOCFLUSH, fread)
}
