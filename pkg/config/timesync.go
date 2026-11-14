package config

import (
	"errors"
	"fmt"
	"slices"
)

// NTPServers is hardn.yml's ntpServers: the time servers Time Sync sets
// (docs/engine/gallery-timesync.md, decision 2 (a)), in the order the
// host's time service is given them. None, the default, hardn changes
// nothing.
type NTPServers []string

// NTPServersMax is how many servers ntpServers takes.
const NTPServersMax = 4

// ErrNTPServers: ntpServers names servers and one is not one hardn writes
// into the time service's settings.
var ErrNTPServers = errors.New("hardn.yml's ntpServers is not valid")

// Resolve checks ntpServers and returns the servers in lower case, in
// order; nil, with no error, where it names none: there is nothing to set.
// More than NTPServersMax, a server named twice, or one that is not exactly
// an IPv4 address or a host name (ResolveHost; an IPv6 address is refused:
// not written yet) is ErrNTPServers, saying which: the names go into the
// time service's settings, so nothing is trimmed, guessed or passed
// through.
func (n NTPServers) Resolve() ([]string, error) {
	if len(n) == 0 {
		return nil, nil
	}
	bad := func(format string, args ...any) ([]string, error) {
		return nil, fmt.Errorf("%w: %s", ErrNTPServers, fmt.Sprintf(format, args...))
	}
	if len(n) > NTPServersMax {
		return bad("more than %d servers", NTPServersMax)
	}
	var out []string
	for i, s := range n {
		host, _, err := ResolveHost(s)
		switch {
		case errors.Is(err, ErrHostIPv6):
			return bad("server %d is an IPv6 address, which hardn does not set yet", i+1)
		case err != nil:
			return bad("server %d is not an IPv4 address or a host name", i+1)
		case slices.Contains(out, host):
			return bad("server %d is named twice", i+1)
		}
		out = append(out, host)
	}
	return out, nil
}
