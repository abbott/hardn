package config

import (
	"errors"
	"fmt"
	"net/netip"
	"path"
	"regexp"
	"strings"
)

// LogForwarding is hardn.yml's logForwarding: the syslog server Log
// Forwarding sets (docs/engine/gallery-logforward.md, decision 1 (a)).
// Server alone turns it on; nothing is set by default.
type LogForwarding struct {
	// Server is the server's IPv4 address or name; "" names none.
	Server string `yaml:"server,omitempty"`
	// Port is its port; 0 is the protocol's default, 514, or 6514 for tls.
	Port int `yaml:"port,omitempty"`
	// Protocol is udp, tcp or tls; "" is tcp.
	Protocol string `yaml:"protocol,omitempty"`
	// CAFile is the file the server's certificate is checked against: tls's
	// alone, and tls needs it.
	CAFile string `yaml:"caFile,omitempty"`
}

// The protocols logForwarding's protocol takes, and the ports' defaults.
const (
	LogUDP = "udp"
	LogTCP = "tcp"
	LogTLS = "tls"

	LogPort    = 514
	LogPortTLS = 6514
)

// LogServer is logForwarding as checked (LogForwarding.Resolve): every
// field set, and none holding a character that could end a quoted value or
// a word in the logger's settings.
type LogServer struct {
	Address  string // an IPv4 address or a host name, lower case
	Port     int    // 1 to 65535
	Protocol string // LogUDP, LogTCP or LogTLS
	CAFile   string // LogTLS's: an absolute, clean path; "" otherwise
}

// ErrLogForwarding: logForwarding names a server and one of its values is
// not one hardn writes into the logger's settings.
var ErrLogForwarding = errors.New("hardn.yml's logForwarding is not valid")

var (
	// logName is one label of a host name: letters, digits and hyphens,
	// neither first nor last a hyphen.
	logName = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)
	// logPath are the characters a caFile may hold: no space, quote,
	// backslash, "$" or control character reaches the logger's settings.
	logPath = regexp.MustCompile(`^/[A-Za-z0-9._/+-]*$`)
)

// Resolve checks logForwarding and fills its defaults. ok is false, with no
// error, where it names no server: there is nothing to set. Any other value
// that is not exactly what hardn writes is ErrLogForwarding, saying which:
// the values go into the logger's settings, so nothing is trimmed, guessed
// or passed through. The server is an IPv4 address or a host name (an IPv6
// address is refused: not written yet); tls needs the server's name, as its
// certificate has it, and caFile.
func (l LogForwarding) Resolve() (s LogServer, ok bool, err error) {
	if l.Server == "" {
		return LogServer{}, false, nil
	}
	bad := func(format string, args ...any) (LogServer, bool, error) {
		return LogServer{}, false, fmt.Errorf("%w: %s", ErrLogForwarding, fmt.Sprintf(format, args...))
	}
	s.Address = strings.ToLower(l.Server)
	_, addrErr := netip.ParseAddr(s.Address)
	isAddr := addrErr == nil
	switch {
	case isAddr && strings.Contains(s.Address, ":"):
		return bad("server is an IPv6 address, which hardn does not set yet")
	case !isAddr && !validLogName(s.Address):
		return bad("server is not an IPv4 address or a host name")
	}
	switch l.Protocol {
	case "":
		s.Protocol = LogTCP
	case LogUDP, LogTCP, LogTLS:
		s.Protocol = l.Protocol
	default:
		return bad("protocol is not udp, tcp or tls")
	}
	switch {
	case l.Port == 0 && s.Protocol == LogTLS:
		s.Port = LogPortTLS
	case l.Port == 0:
		s.Port = LogPort
	case l.Port < 1 || l.Port > 65535:
		return bad("port is not between 1 and 65535")
	default:
		s.Port = l.Port
	}
	switch {
	case s.Protocol != LogTLS && l.CAFile != "":
		return bad("caFile is for protocol tls")
	case s.Protocol != LogTLS:
		return s, true, nil
	case isAddr:
		return bad("tls needs the server's name, as its certificate has it")
	case l.CAFile == "":
		return bad("tls needs caFile")
	case !logPath.MatchString(l.CAFile) || path.Clean(l.CAFile) != l.CAFile || l.CAFile == "/":
		return bad("caFile is not an absolute path of letters, digits and . _ + - /")
	}
	s.CAFile = l.CAFile
	return s, true, nil
}

// validLogName reports whether name is a host name: labels of letters,
// digits and hyphens, 253 characters at most, and not all digits and dots
// (a mistyped address).
func validLogName(name string) bool {
	if len(name) > 253 || strings.Trim(name, "0123456789.") == "" {
		return false
	}
	for _, label := range strings.Split(name, ".") {
		if !logName.MatchString(label) {
			return false
		}
	}
	return true
}
