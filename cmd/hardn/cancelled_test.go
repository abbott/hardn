package main

import "context"

// cancelled returns once ctx's cancellation, already seen on its Done, has
// reached every context derived from it, the apply's own among them. A
// cancelCtx closes its Done before it cancels its children, holding its
// lock until all of them are, and Err is an atomic load (Go 1.27's
// context.go, cancelCtx.cancel and Err): a handler that returns on Done
// alone can let the step go on under a child not yet cancelled, which then
// reports the step done and fails the plan at the journal's next write
// (TestDNSScreenSignals, seen 12 in 9000 runs on two CPUs, none on one).
// Cause takes that lock, so it waits the cancellation out.
func cancelled(ctx context.Context) { _ = context.Cause(ctx) }
