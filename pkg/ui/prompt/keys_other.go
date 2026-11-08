//go:build !linux && !darwin

package prompt

import "time"

// keyMode is never made here: menu choices are lines.
type keyMode struct{}

// startKeys reads no keys on systems other than Linux and macOS.
func (r *Reader) startKeys() *keyMode { return nil }

func (km *keyMode) restore() {}

func (km *keyMode) ended() bool { return false }

func (km *keyMode) wait(*interrupt, time.Duration) (bool, error) { return true, nil }
