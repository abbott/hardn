package host

import (
	"context"
	"fmt"
	"time"
)

// RunBounded runs name through sys under limit, and returns at limit plus grace
// whether or not the command has ended, the error then wrapping ctx's. For a
// read a person waits for. host.Real kills the command when ctx is done, but its
// Wait returns only once the process has gone, and one stuck in the kernel on a
// hung pool or mount cannot go (os/exec's Cmd.Wait), so past grace the command
// is left behind, a goroutine waiting on it.
func RunBounded(ctx context.Context, sys System, limit, grace time.Duration, name string, args ...string) (Result, error) {
	ctx, cancel := context.WithTimeout(ctx, limit)
	defer cancel()
	type answer struct {
		res Result
		err error
	}
	done := make(chan answer, 1)
	go func() {
		res, err := sys.Run(ctx, name, args...)
		done <- answer{res, err}
	}()
	select {
	case a := <-done:
		return a.res, a.err
	case <-ctx.Done():
	}
	timer := time.NewTimer(grace)
	defer timer.Stop()
	select {
	case a := <-done:
		return a.res, a.err
	case <-timer.C:
		argv := append([]string{name}, args...)
		return Result{Argv: argv, ExitCode: -1}, fmt.Errorf("host: %s did not end: %w", FormatArgv(argv), ctx.Err())
	}
}
