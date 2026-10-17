//go:build !linux && !darwin

package prompt

import "os"

// Reraise ends the process with ExitInterrupted. Elsewhere than Linux and
// macOS ReadLine does not relay Ctrl-C (wait_other.go), so it never
// returns ErrInterrupted and SIGINT keeps its default action; Reraise is
// here so that its callers build.
func Reraise() { os.Exit(ExitInterrupted) }
