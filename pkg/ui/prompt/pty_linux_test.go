package prompt

import (
	"os"
	"testing"

	"github.com/abbott/hardn/internal/ptytest"
)

// openPTY opens a pseudo-terminal in its default, cooked mode
// (ptytest.Open) and returns its master, where the test types, and its
// slave, which hardn would read as stdin; the master's echo is drained.
func openPTY(t *testing.T) (master, slave *os.File) {
	t.Helper()
	m, s := ptytest.Open(t)
	go ptytest.Drain(m)
	return m, s
}
