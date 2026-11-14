// Package logfwd reads whether the host sends its logs to a syslog server,
// and builds the settings that make it (docs/engine/screen-logforward.md;
// the survey is docs/engine/gallery-logforward.md §3).
//
// Read tells the logger (rsyslog, busybox syslogd on Alpine, or journald
// alone) and each place it forwards to: rsyslog's omfwd actions and its
// older "@host" and "@@host" lines in /etc/rsyslog.conf and
// /etc/rsyslog.d/*.conf, and busybox syslogd's -R in /etc/conf.d/syslog. It
// only reads, and never runs the logger. What it returns of a file hardn
// did not write is the host's text, as written: the caller makes it safe
// to show (logview.Safe).
//
// DropIn, SetOpts and ClearOpts build what hardn writes: its own file under
// /etc/rsyslog.d, and its words in busybox syslogd's options. Each checks
// the server again (Server.Check), so that nothing but an address or name,
// a port and a path of plain characters reaches the logger's settings,
// whoever calls.
package logfwd

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"regexp"
	"strconv"

	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/alpine"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/packages"
)

// Logger is the host's syslog daemon.
type Logger uint8

// The loggers.
const (
	// LoggerUnknown: not established (Reading.Err says why).
	LoggerUnknown Logger = iota
	// Rsyslog forwards over UDP, TCP or TLS by a file under /etc/rsyslog.d.
	Rsyslog
	// Busybox is busybox syslogd, Alpine's: UDP alone, by its -R option.
	Busybox
	// Journald is systemd-journald with no rsyslog: nothing on the host
	// sends syslog.
	Journald
)

// Proto is how logs are sent.
type Proto uint8

// The protocols.
const (
	UDP Proto = iota
	TCP
	TLS
)

var protoWords = [...]string{"udp", "tcp", "tls"}

func (p Proto) String() string {
	if int(p) < len(protoWords) {
		return protoWords[p]
	}
	return fmt.Sprintf("proto(%d)", uint8(p))
}

// Server is a syslog server. Read's are the host's text, as written; one
// hardn writes passes Check.
type Server struct {
	// Address is its address or name; Port its port, "514".
	Address, Port string
	Proto         Proto
	// CAFile is the file TLS checks the server's certificate against, where
	// the forwarding itself names one; "" otherwise.
	CAFile string
}

// ErrServer: a server hardn was asked to write is not one it writes.
var ErrServer = errors.New("not a server hardn writes")

var (
	// serverAddress: an IPv4 address or a host name, never an option.
	serverAddress = regexp.MustCompile(`^[a-z0-9]([a-z0-9.-]{0,251}[a-z0-9])?$`)
	serverPath    = regexp.MustCompile(`^/[A-Za-z0-9._/+-]+$`)
)

// Check refuses, with ErrServer, a server whose text could end a quoted
// value in rsyslog's file or a word of syslogd's options: the address is
// lower-case letters, digits, dots and hyphens; the port a number from 1 to
// 65535; the CA file, TLS's alone and required there, an absolute path of
// letters, digits and "._+-/".
func (s Server) Check() error {
	port, err := strconv.Atoi(s.Port)
	switch {
	case !serverAddress.MatchString(s.Address):
		return fmt.Errorf("logfwd: address %q: %w", s.Address, ErrServer)
	case err != nil || port < 1 || port > 65535 || strconv.Itoa(port) != s.Port:
		return fmt.Errorf("logfwd: port %q: %w", s.Port, ErrServer)
	case s.Proto > TLS:
		return fmt.Errorf("logfwd: protocol %d: %w", s.Proto, ErrServer)
	case s.Proto == TLS && !serverPath.MatchString(s.CAFile), s.Proto != TLS && s.CAFile != "":
		return fmt.Errorf("logfwd: CA file %q: %w", s.CAFile, ErrServer)
	}
	return nil
}

// Forward is one place the logger sends to.
type Forward struct {
	Server Server
	// File is the file that says so, and Line its line there.
	File string
	Line int
	// ByHardn: the setting is hardn's own: its file under /etc/rsyslog.d,
	// opening with Header, or the words its mark in /etc/conf.d/syslog
	// names.
	ByHardn bool
}

// Reading is what Read found.
type Reading struct {
	Logger Logger
	// Forwards are the places the logger sends to, in the order its files
	// are read.
	Forwards []Forward
	// NoInstall: the logger is Journald, and hardn installs no package on
	// this distribution (packages.ToolFor; Proxmox VE), so not rsyslog.
	NoInstall bool
	// Err is why the logger, or its settings, are not read: a file that
	// cannot be read, or one whose forwarding hardn cannot make out. The
	// Forwards are then not established, and nothing is planned.
	Err error
}

// ErrMalformed: a file's forwarding cannot be made out.
var ErrMalformed = errors.New("forwarding hardn can't make out")

// ErrNoLogger: neither rsyslog, busybox syslogd nor journald alone.
var ErrNoLogger = errors.New("no logger hardn reads")

// Now is the forwarding the screen shows: hardn's own where there is one,
// else the first; nil for none.
func (r Reading) Now() *Forward {
	for i := range r.Forwards {
		if r.Forwards[i].ByHardn {
			return &r.Forwards[i]
		}
	}
	if len(r.Forwards) > 0 {
		return &r.Forwards[0]
	}
	return nil
}

// Own is hardn's own forwarding; nil for none.
func (r Reading) Own() *Forward {
	if f := r.Now(); f != nil && f.ByHardn {
		return f
	}
	return nil
}

// Foreign is the first forwarding that is not hardn's; nil for none.
func (r Reading) Foreign() *Forward {
	for i := range r.Forwards {
		if !r.Forwards[i].ByHardn {
			return &r.Forwards[i]
		}
	}
	return nil
}

// ErrTwoLoggers: an Alpine host has rsyslog installed beside busybox
// syslogd, and hardn does not tell which of them logs.
var ErrTwoLoggers = errors.New("rsyslog is installed beside busybox syslogd, so which one logs is not established")

// rsyslogPackage is the package whose presence makes rsyslog the logger.
const rsyslogPackage = "rsyslog"

// Read tells the host's logger and where it forwards, writing nothing. On
// Alpine the logger is busybox syslogd, read from /etc/conf.d/syslog, where
// rsyslog, which Alpine also packages, is not installed (apk's database,
// a file); with
// it installed, which of the two logs is not established (ErrTwoLoggers),
// and nothing is read or planned. Whether syslogd's service is in a
// runlevel is not looked at. On
// the others it is rsyslog where its package is installed (dpkg's or rpm's
// word), read from its files; without it, on a host systemd booted, it is
// journald alone. Anything else, another distribution or a package state
// that does not say, is LoggerUnknown with Err. Another syslog daemon
// (syslog-ng) is not looked for.
func Read(ctx context.Context, sys host.System, f detect.Facts) Reading {
	var inst detect.Tri
	var err error
	switch d := f.Platform.Distro; {
	case d == detect.DistroAlpine:
		db, err := alpine.ReadInstalled(ctx, sys)
		if err != nil {
			return Reading{Err: fmt.Errorf("logfwd: whether rsyslog is installed: %w", err)}
		}
		if _, two := db[rsyslogPackage]; two {
			return Reading{Err: fmt.Errorf("logfwd: %w", ErrTwoLoggers)}
		}
		fw, err := readBusybox(ctx, sys)
		return Reading{Logger: Busybox, Forwards: fw, Err: err}
	case d.EL():
		inst, err = packages.Installed(ctx, sys, packages.Dnf, rsyslogPackage)
	case d == detect.DistroDebian, d == detect.DistroUbuntu, d == detect.DistroProxmox:
		inst, err = detect.DpkgInstalled(ctx, sys, rsyslogPackage)
	default:
		return Reading{Err: fmt.Errorf("logfwd: distribution %q: %w", d, ErrNoLogger)}
	}
	switch {
	case inst == detect.Yes:
		fw, err := readRsyslog(ctx, sys)
		return Reading{Logger: Rsyslog, Forwards: fw, Err: err}
	case inst == detect.No && f.Init.Kind == detect.Systemd && f.Init.Running == detect.Yes:
		_, noTool := packages.ToolFor(f)
		return Reading{Logger: Journald, NoInstall: noTool != nil}
	case inst == detect.No:
		return Reading{Err: fmt.Errorf("logfwd: no rsyslog, and systemd did not boot the host: %w", ErrNoLogger)}
	}
	if err == nil {
		err = detect.ErrNoSignal
	}
	return Reading{Err: fmt.Errorf("logfwd: whether rsyslog is installed: %w", err)}
}

// readFile is name's bytes; ok is false, with no error, where it is not
// there.
func readFile(ctx context.Context, sys host.System, name string) (data []byte, ok bool, err error) {
	data, err = sys.ReadFile(ctx, name)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return nil, false, nil
	case err != nil:
		return nil, false, fmt.Errorf("logfwd: %w", err)
	}
	return data, true, nil
}
