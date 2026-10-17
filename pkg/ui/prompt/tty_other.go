//go:build !linux && !darwin

package prompt

// terminalFd reports no terminal on systems other than Linux, which hardn
// runs on, and macOS, where it is developed. Stdin is never a terminal
// Reader there: it flushes nothing, as flush_other.go cannot, and Ask and
// Answer end each prompt's line themselves, so a terminal there shows a
// blank line after its echoed Enter.
func terminalFd(int) bool { return false }
