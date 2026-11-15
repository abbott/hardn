package config

import (
	"errors"
	"fmt"
	"path"
	"regexp"
)

// SyslogServer is one block of hardn.yml's syslogServers: a syslog server
// Syslog Settings sets (docs/engine/design-syslog-servers.md §2.1).
type SyslogServer struct {
	// Server is the server's IPv4 address or name.
	Server string `yaml:"server,omitempty"`
	// Port is its port; 0 is the protocol's default, 514, or 6514 for tls.
	Port int `yaml:"port,omitempty"`
	// Protocol is udp, tcp or tls; "" is tcp.
	Protocol string `yaml:"protocol,omitempty"`
	// CAFile is the file the server's certificate is checked against: tls's
	// alone, and tls needs it.
	CAFile string `yaml:"caFile,omitempty"`
}

// SyslogServers is hardn.yml's syslogServers: the syslog servers the host's
// logs go to, each sent every message, one to four. None, the default,
// hardn changes nothing.
type SyslogServers []SyslogServer

// SyslogServersMax is how many servers syslogServers takes.
const SyslogServersMax = 4

// The protocols a server's protocol takes, and the ports' defaults.
const (
	LogUDP = "udp"
	LogTCP = "tcp"
	LogTLS = "tls"

	LogPort    = 514
	LogPortTLS = 6514
)

// LogServer is one server of syslogServers as checked
// (SyslogServers.Resolve): every field set, and none holding a character
// that could end a quoted value or a word in the logger's settings.
type LogServer struct {
	Address  string // an IPv4 address or a host name, lower case
	Port     int    // 1 to 65535
	Protocol string // LogUDP, LogTCP or LogTLS
	CAFile   string // LogTLS's: an absolute, clean path; "" otherwise
}

// ErrSyslogServers: syslogServers names servers and one of its values is
// not one hardn writes into the logger's settings.
var ErrSyslogServers = errors.New("hardn.yml's syslogServers is not valid")

// logPath are the characters a caFile may hold: no space, quote, backslash,
// "$" or control character reaches the logger's settings.
var logPath = regexp.MustCompile(`^/[A-Za-z0-9._/+-]*$`)

// Resolve checks syslogServers and fills each server's defaults, in the
// list's order; nil, with no error, where it names none: there is nothing
// to set. More than SyslogServersMax, a block with no server, the same
// address and port twice, whatever the protocol, or a value that is not
// exactly what hardn writes is ErrSyslogServers, saying which entry
// ("server 2: port is not between 1 and 65535"): the values go into the
// logger's settings, so nothing is trimmed, guessed or passed through, and
// no part of a list with one bad entry is returned.
func (l SyslogServers) Resolve() ([]LogServer, error) {
	if len(l) == 0 {
		return nil, nil
	}
	bad := func(format string, args ...any) ([]LogServer, error) {
		return nil, fmt.Errorf("%w: %s", ErrSyslogServers, fmt.Sprintf(format, args...))
	}
	if len(l) > SyslogServersMax {
		return bad("more than %d servers", SyslogServersMax)
	}
	var out []LogServer
	for i, b := range l {
		s, why := b.resolve()
		if why != "" {
			return bad("server %d: %s", i+1, why)
		}
		for j, o := range out {
			if o.Address == s.Address && o.Port == s.Port {
				return bad("server %d: the same address and port as server %d", i+1, j+1)
			}
		}
		out = append(out, s)
	}
	return out, nil
}

// resolve checks one block; why says what is not valid, "" for nothing.
// The server is an IPv4 address or a host name (ResolveHost; an IPv6
// address is refused: not written yet); tls needs the server's name, as
// its certificate has it, and caFile.
func (l SyslogServer) resolve() (s LogServer, why string) {
	if l.Server == "" {
		return s, "no server"
	}
	address, isAddr, err := ResolveHost(l.Server)
	switch {
	case errors.Is(err, ErrHostIPv6):
		return s, "server is an IPv6 address, which hardn does not set yet"
	case err != nil:
		return s, "server is not an IPv4 address or a host name"
	}
	s.Address = address
	switch l.Protocol {
	case "":
		s.Protocol = LogTCP
	case LogUDP, LogTCP, LogTLS:
		s.Protocol = l.Protocol
	default:
		return s, "protocol is not udp, tcp or tls"
	}
	switch {
	case l.Port == 0 && s.Protocol == LogTLS:
		s.Port = LogPortTLS
	case l.Port == 0:
		s.Port = LogPort
	case l.Port < 1 || l.Port > 65535:
		return s, "port is not between 1 and 65535"
	default:
		s.Port = l.Port
	}
	switch {
	case s.Protocol != LogTLS && l.CAFile != "":
		return s, "caFile is for protocol tls"
	case s.Protocol != LogTLS:
		return s, ""
	case isAddr:
		return s, "tls needs the server's name, as its certificate has it"
	case l.CAFile == "":
		return s, "tls needs caFile"
	case !logPath.MatchString(l.CAFile) || path.Clean(l.CAFile) != l.CAFile || l.CAFile == "/":
		return s, "caFile is not an absolute path of letters, digits and . _ + - /"
	}
	s.CAFile = l.CAFile
	return s, ""
}
