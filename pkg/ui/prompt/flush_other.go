//go:build !linux && !darwin

package prompt

// flushInput does nothing here: only Linux and macOS flush a terminal's
// input queue. Flush still discards the Reader's own buffer.
func flushInput(int) error { return nil }
