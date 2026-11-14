package config

import (
	"errors"
	"net/netip"
	"regexp"
	"strings"
)

// The rule for a server typed in hardn.yml (logForwarding's server): an
// IPv4 address or a host name, written into a service's settings, so
// nothing is trimmed, guessed or passed through.
var (
	// ErrHostIPv6: the server is an IPv6 address, which hardn does not set
	// yet.
	ErrHostIPv6 = errors.New("an IPv6 address, which hardn does not set yet")
	// ErrHostName: the server is not an IPv4 address or a host name.
	ErrHostName = errors.New("not an IPv4 address or a host name")
)

// hostLabel is one label of a host name: letters, digits and hyphens,
// neither first nor last a hyphen.
var hostLabel = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)

// ResolveHost checks a server as hardn.yml names it and returns it in lower
// case; isAddr says it is an IPv4 address, not a name. An IPv6 address is
// ErrHostIPv6, and anything that is neither an address nor a host name
// (validHostName) ErrHostName.
func ResolveHost(s string) (host string, isAddr bool, err error) {
	host = strings.ToLower(s)
	_, addrErr := netip.ParseAddr(host)
	isAddr = addrErr == nil
	switch {
	case isAddr && strings.Contains(host, ":"):
		return "", false, ErrHostIPv6
	case !isAddr && !validHostName(host):
		return "", false, ErrHostName
	}
	return host, isAddr, nil
}

// validHostName reports whether name is a host name: labels of letters,
// digits and hyphens, 253 characters at most, and not all digits and dots
// (a mistyped address).
func validHostName(name string) bool {
	if len(name) > 253 || strings.Trim(name, "0123456789.") == "" {
		return false
	}
	for _, label := range strings.Split(name, ".") {
		if !hostLabel.MatchString(label) {
			return false
		}
	}
	return true
}
