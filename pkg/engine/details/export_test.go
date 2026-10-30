package details

import (
	"context"
	"time"

	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/detect"
)

// Bounds gives the tests the bounds themselves.
var Bounds = struct{ Command, Lslogins, Grace time.Duration }{defaultBounds.command, defaultBounds.lslogins, defaultBounds.grace}

// ReadBounded gives the tests Read under short bounds.
func ReadBounded(ctx context.Context, sys host.System, f detect.Facts, command, lslogins, grace time.Duration) Report {
	return read(ctx, sys, f, bounds{command: command, lslogins: lslogins, grace: grace})
}

// RunBounded gives the tests the bound on a real child.
func RunBounded(ctx context.Context, sys host.System, command, grace time.Duration, name string, args ...string) (host.Result, error) {
	return boundedSystem{System: sys, bounds: bounds{command: command, grace: grace}}.Run(ctx, name, args...)
}

// ReadSource gives the tests the blank guard's read of a real file.
var ReadSource = readSource
