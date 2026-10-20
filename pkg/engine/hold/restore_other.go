//go:build !linux && !darwin

package hold

import (
	"errors"
	"syscall"
)

// errNoRestore: this platform has no restore here.
var errNoRestore = errors.New("no restore of the default action on this platform")

// restoreDefault has no implementation here: the signal stays ignored.
func restoreDefault(syscall.Signal) error { return errNoRestore }
