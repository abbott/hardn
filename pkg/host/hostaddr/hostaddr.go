// Package hostaddr is the check a server's address or name passes before
// hardn writes it into a service's settings: the second check, by whoever
// writes the file, after hardn.yml's own (config.ResolveHost;
// docs/engine/screen-logforward.md §2).
package hostaddr

import "regexp"

// name: an IPv4 address or a host name, never an option.
var name = regexp.MustCompile(`^[a-z0-9]([a-z0-9.-]{0,251}[a-z0-9])?$`)

// Valid reports whether s is a name hardn writes: none whose text could
// end a quoted value or a word in a service's settings. It is lower-case
// letters, digits, dots and hyphens, neither first nor last a dot or a
// hyphen, 253 characters at most. The error of one that is not is the
// caller's (logfwd.ErrServer).
func Valid(s string) bool { return name.MatchString(s) }
