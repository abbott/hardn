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

// ReadSource gives the tests the blank guard's read of a real file.
var ReadSource = readSource
