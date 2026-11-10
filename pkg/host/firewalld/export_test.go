package firewalld

import (
	"context"
	"time"
)

// NoPause makes the wait after a start poll without sleeping, and give up
// after d, for tests.
func NoPause(d time.Duration) func() {
	old, oldT := pause, readyTimeout
	pause = func(ctx context.Context, _ time.Duration) error { return ctx.Err() }
	readyTimeout = d
	return func() { pause, readyTimeout = old, oldT }
}
