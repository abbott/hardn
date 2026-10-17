//go:build linux || darwin

package prompt

import (
	"os"
	"os/signal"
	"syscall"
	"time"
)

// reraiseWait is how long Reraise waits for SIGINT to end the process
// before it exits with ExitInterrupted instead.
const reraiseWait = 500 * time.Millisecond

// Reraise ends the process as Ctrl-C ends a program that does not catch
// SIGINT. It is for a caller that ends hardn when a read returns
// ErrInterrupted, as the legacy menus (pkg/menu) and the first-run config
// prompts (pkg/config) do: it gives os.Interrupt its default action back,
// undoing every signal.Notify for it in the process (signal.Reset), and
// sends SIGINT to the process, which dies of it. A waiting parent then
// sees a process killed by SIGINT rather than an exit: a shell running
// hardn in a loop or a script stops, as it does for any program Ctrl-C
// ends, where an exit with status 130 would let it go on. If the process
// still runs reraiseWait later, as when SIGINT is ignored (signal.Ignore,
// or SIG_IGN inherited by a background job, which signal.Reset leaves
// ignored), it exits with ExitInterrupted. It never returns.
func Reraise() {
	signal.Reset(os.Interrupt)
	_ = syscall.Kill(syscall.Getpid(), syscall.SIGINT)
	time.Sleep(reraiseWait)
	os.Exit(ExitInterrupted)
}
