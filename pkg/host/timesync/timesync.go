// Package timesync reads which service keeps the host's time, whether it
// runs, whether the clock is in sync, and the servers it uses and where
// each is set; and builds the settings that make it use hardn.yml's servers
// in place of its own (docs/engine/screen-timesync.md; the survey is
// docs/engine/gallery-timesync.md §3).
//
// Read tells chrony, systemd-timesyncd or busybox ntpd (Alpine's) from the
// host's packages and services, and reads the service's own files. It runs
// only commands that read: the service manager's status, chronyc tracking,
// timedatectl show and show-timesync, busybox adjtimex. What it returns of
// a file or a command is the host's text, as written: the caller makes it
// safe to show (logview.Safe).
//
// SourcesFile, ChronyOff, ChronyOn, TimesyncdFile, SetPeers and ClearPeers
// build what hardn writes. Each checks the servers again (ErrServer), so
// that nothing but an address or a name reaches a service's settings,
// whoever calls. A file hardn did not write is edited line by line, marked
// so that the edit is recognised and exactly reversed, and refused
// (ErrMalformed) in any form hardn does not write back.
package timesync

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/alpine"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/hostaddr"
	"github.com/abbott/hardn/pkg/host/packages"
)

// Service is the service that keeps the host's time.
type Service uint8

// The services.
const (
	// NoService: none hardn reads. Another daemon (openntpd, ntpsec) is not
	// looked for.
	NoService Service = iota
	// Chrony is chronyd.
	Chrony
	// Timesyncd is systemd-timesyncd.
	Timesyncd
	// Busybox is busybox ntpd, Alpine's, which says nothing of the server
	// it follows.
	Busybox
)

var serviceWords = [...]string{"none", "chrony", "systemd-timesyncd", "busybox ntpd"}

func (s Service) String() string {
	if int(s) < len(serviceWords) {
		return serviceWords[s]
	}
	return fmt.Sprintf("service(%d)", uint8(s))
}

// Clock is whether the clock is in sync.
type Clock uint8

// The clock's states.
const (
	// ClockUnknown: not read (Reading.ClockErr says why).
	ClockUnknown Clock = iota
	InSync
	NotInSync
)

// From is where a server the service uses is set.
type From uint8

// The places.
const (
	// FromFile: the service's main file, or a file the distribution's
	// package installs beside it (DistroSources).
	FromFile From = iota
	// FromHardn: hardn's own setting.
	FromHardn
	// FromOther: a file that is neither, named by Server.File.
	FromOther
	// FromDHCP: handed out with the host's address.
	FromDHCP
	// FromBuiltIn: compiled into systemd-timesyncd.
	FromBuiltIn
)

// Server is one server, or pool of servers, the service uses.
type Server struct {
	// Name is its name or address, as the host has it.
	Name string
	From From
	// File is the file that names it; "" for FromDHCP of systemd-timesyncd
	// and for FromBuiltIn.
	File string
	// NTS: chrony authenticates it (the nts option).
	NTS bool
}

// Reading is what Read found.
type Reading struct {
	// Container: the host is a container, whose clock is its host's. No
	// service is looked for.
	Container bool
	Service   Service
	// Running is whether Service runs; Unknown with NoService.
	Running detect.Tri
	// Unit and RC are the service's names under systemd and OpenRC.
	Unit, RC string
	// Clock is whether the clock is in sync, and ClockErr why that is not
	// read. Synced is the server it follows, as the service names it; ""
	// for none, and for busybox ntpd, which does not say.
	Clock    Clock
	ClockErr error
	Synced   string
	// Servers are the servers the service uses, in the order its files are
	// read.
	Servers []Server
	// File is the file hardn's own servers are set in; "" with none.
	File string
	// Cleared is a systemd-timesyncd drop-in, not hardn's and read after
	// hardn's own, whose empty NTP= takes away the servers before it,
	// hardn's too: servers are set outside hardn. "" for none.
	Cleared string
	// Mine: something of hardn's is in the service's files: its own file,
	// its lines, or lines it switched off.
	Mine bool
	// Restores are the servers hardn's setting took the place of, which
	// taking it out puts back.
	Restores []Server
	// Main is chrony's main file, and DropIn whether it reads SourcesDir,
	// where hardn's own file goes; without, hardn's lines go in Main.
	Main   string
	DropIn bool
	// Err is why the service, or its settings, are not read. The Servers
	// are then not established, and nothing is planned.
	Err error
}

// Own are hardn's own servers, in order.
func (r Reading) Own() []string {
	var out []string
	for _, s := range r.Servers {
		if s.From == FromHardn {
			out = append(out, s.Name)
		}
	}
	return out
}

// Outside is the first server set by a file that is neither the service's
// own nor hardn's; nil for none.
func (r Reading) Outside() *Server {
	for i := range r.Servers {
		if r.Servers[i].From == FromOther {
			return &r.Servers[i]
		}
	}
	return nil
}

// NTS reports whether a server of the service's own files is authenticated.
func (r Reading) NTS() bool {
	for _, s := range r.Servers {
		if s.From == FromFile && s.NTS {
			return true
		}
	}
	return false
}

// The errors.
var (
	// ErrMalformed: a file's servers cannot be made out, or it is in a form
	// hardn does not write back.
	ErrMalformed = errors.New("time servers hardn can't make out")
	// ErrServer: servers hardn was asked to write are not ones it writes.
	ErrServer = errors.New("not time servers hardn writes")
	// ErrNotOwn: nothing in the file is hardn's to take out.
	ErrNotOwn = errors.New("no time servers of hardn's own")
)

// Header opens a file hardn writes: its own, whole.
const Header = "# Written by hardn (NTP Settings).\n"

// MaxServers is how many servers hardn writes.
const MaxServers = 4

// check refuses, with ErrServer, servers hardn does not write: none, more
// than MaxServers, or a name that is not lower-case letters, digits, dots
// and hyphens (hostaddr.Valid), which could end a word or a line of a
// service's settings.
func check(servers []string) error {
	if len(servers) == 0 || len(servers) > MaxServers {
		return fmt.Errorf("timesync: %d servers: %w", len(servers), ErrServer)
	}
	for _, s := range servers {
		if !hostaddr.Valid(s) {
			return fmt.Errorf("timesync: server %q: %w", s, ErrServer)
		}
	}
	return nil
}

// chronyPackage and timesyncdPackage are the packages whose presence tells
// the service.
const (
	chronyPackage    = "chrony"
	timesyncdPackage = "systemd-timesyncd"
)

// Read tells the host's time service and reads it, writing nothing. In a
// container no service is looked for: the clock is its host's, and only the
// kernel's word on it is read. Elsewhere the service is chrony where its
// package is installed (dpkg's, rpm's or apk's word); without it,
// systemd-timesyncd on Debian, Ubuntu and Proxmox VE where that package is
// installed, and on Alpine busybox ntpd where its service is started or in
// a runlevel; else none. A package state that does not say, or a service
// whose state is not read, is Err.
func Read(ctx context.Context, sys host.System, f detect.Facts) Reading {
	if f.Container.Detected == detect.Yes {
		r := Reading{Container: true}
		r.Clock, r.ClockErr = kernelClock(ctx, sys, f)
		return r
	}
	r, err := service(ctx, sys, f)
	if err != nil {
		r.Err = fmt.Errorf("timesync: %w", err)
		return r
	}
	switch r.Service {
	case Chrony:
		r.Err = r.readChrony(ctx, sys, f)
	case Timesyncd:
		r.Err = r.readTimesyncd(ctx, sys)
	case Busybox:
		r.Err = r.readBusybox(ctx, sys)
	}
	if r.Service == Chrony && r.Running == detect.Yes {
		r.Clock, r.Synced, r.ClockErr = chronyClock(ctx, sys)
	} else {
		r.Clock, r.ClockErr = kernelClock(ctx, sys, f)
	}
	return r
}

// service tells the service and whether it runs.
func service(ctx context.Context, sys host.System, f detect.Facts) (Reading, error) {
	installed := func(tri detect.Tri, err error, pkg string) (bool, error) {
		if tri == detect.Unknown {
			if err == nil {
				err = detect.ErrNoSignal
			}
			return false, fmt.Errorf("whether %s is installed: %w", pkg, err)
		}
		return tri == detect.Yes, nil
	}
	switch d := f.Platform.Distro; {
	case d == detect.DistroAlpine:
		db, err := alpine.ReadInstalled(ctx, sys)
		if err != nil {
			return Reading{}, fmt.Errorf("whether chrony is installed: %w", err)
		}
		if _, ok := db[chronyPackage]; ok {
			return running(ctx, sys, f, Reading{Service: Chrony, RC: "chronyd"})
		}
		r, err := running(ctx, sys, f, Reading{Service: Busybox, RC: BusyboxService})
		if err != nil || r.Running == detect.Yes {
			return r, err
		}
		// Every Alpine has busybox's ntpd; it is the host's time service
		// only where it was added to a runlevel.
		switch _, in, err := detect.ServiceRunlevels(ctx, sys, BusyboxService); {
		case in == detect.Unknown:
			return r, fmt.Errorf("whether ntpd is in a runlevel: %w", err)
		case in == detect.No:
			return Reading{}, nil
		}
		return r, nil
	case d.EL():
		tri, err := packages.Installed(ctx, sys, packages.Dnf, chronyPackage)
		if ok, err := installed(tri, err, chronyPackage); err != nil || !ok {
			return Reading{}, err
		}
		return running(ctx, sys, f, Reading{Service: Chrony, Unit: "chronyd"})
	case d == detect.DistroDebian, d == detect.DistroUbuntu, d == detect.DistroProxmox:
		tri, err := detect.DpkgInstalled(ctx, sys, chronyPackage)
		switch ok, err := installed(tri, err, chronyPackage); {
		case err != nil:
			return Reading{}, err
		case ok:
			return running(ctx, sys, f, Reading{Service: Chrony, Unit: "chrony"})
		}
		tri, err = detect.DpkgInstalled(ctx, sys, timesyncdPackage)
		if ok, err := installed(tri, err, timesyncdPackage); err != nil || !ok {
			return Reading{}, err
		}
		return running(ctx, sys, f, Reading{Service: Timesyncd, Unit: timesyncdPackage})
	}
	return Reading{}, fmt.Errorf("distribution %q: %w", f.Platform.Distro, detect.ErrNoSignal)
}

// running fills r.Running by the init system that booted the host:
// systemctl is-active for r.Unit, rc-service status for r.RC (0 started, 3
// stopped). Another init, a service with no name under it, or an answer
// that does not say is an error: Running is then not established.
func running(ctx context.Context, sys host.System, f detect.Facts, r Reading) (Reading, error) {
	fail := func(err error) (Reading, error) {
		return r, fmt.Errorf("whether %s runs: %w", r.Service, err)
	}
	if f.Init.Running != detect.Yes {
		return fail(detect.ErrNoSignal)
	}
	switch {
	case f.Init.Kind == detect.Systemd && r.Unit != "":
		state, err := detect.SystemctlIsActive(ctx, sys, r.Unit)
		if err != nil {
			return fail(err)
		}
		if r.Running = (detect.UnitState{Active: state}).IsActive(); r.Running == detect.Unknown {
			return fail(fmt.Errorf("systemctl is-active %s: %s: %w", r.Unit, state, detect.ErrUnrecognised))
		}
		return r, nil
	case f.Init.Kind == detect.OpenRC && r.RC != "":
		res, err := sys.Run(ctx, "rc-service", r.RC, "status")
		var exit *host.ExitError
		switch {
		case err == nil:
			r.Running = detect.Yes
		case errors.As(err, &exit) && exit.ExitCode == 3:
			r.Running = detect.No
		case errors.As(err, &exit):
			return fail(fmt.Errorf("rc-service %s status: exit %d: %w", r.RC, res.ExitCode, detect.ErrUnrecognised))
		default:
			return fail(err)
		}
		return r, nil
	}
	return fail(detect.ErrNoSignal)
}

// kernelClock is the kernel's word on the clock (adjtimex(2): unsynchronized
// while STA_UNSYNC is set), as a command of the host reports it: under
// systemd "timedatectl show", whose NTPSynchronized is that flag; under
// OpenRC "busybox adjtimex", whose return value is 5, TIME_ERROR, while the
// clock is not synchronized. Any other answer is ClockUnknown with the
// error.
func kernelClock(ctx context.Context, sys host.System, f detect.Facts) (Clock, error) {
	switch {
	case f.Init.Running == detect.Yes && f.Init.Kind == detect.Systemd:
		res, err := sys.Run(ctx, "timedatectl", "show")
		if err != nil {
			return ClockUnknown, fmt.Errorf("timesync: timedatectl show: %w", err)
		}
		switch properties(res.Stdout)["NTPSynchronized"] {
		case "yes":
			return InSync, nil
		case "no":
			return NotInSync, nil
		}
		return ClockUnknown, fmt.Errorf("timesync: timedatectl show: no NTPSynchronized: %w", detect.ErrUnrecognised)
	case f.Init.Running == detect.Yes && f.Init.Kind == detect.OpenRC:
		res, err := sys.Run(ctx, "busybox", "adjtimex")
		if err != nil {
			return ClockUnknown, fmt.Errorf("timesync: busybox adjtimex: %w", err)
		}
		for _, line := range strings.Split(string(res.Stdout), "\n") {
			rest, ok := strings.CutPrefix(strings.TrimSpace(line), "return value:")
			if !ok {
				continue
			}
			word, _, _ := strings.Cut(strings.TrimSpace(rest), " ")
			switch n, err := strconv.Atoi(word); {
			case err != nil:
			case n == 5:
				return NotInSync, nil
			case n >= 0 && n < 5:
				return InSync, nil
			}
		}
		return ClockUnknown, fmt.Errorf("timesync: busybox adjtimex: no return value: %w", detect.ErrUnrecognised)
	}
	return ClockUnknown, fmt.Errorf("timesync: the clock: %w", detect.ErrNoSignal)
}

// properties are the "Key=Value" lines of out, as timedatectl show prints
// them; a line with no "=" is skipped, and a key printed twice keeps its
// first value.
func properties(out []byte) map[string]string {
	m := map[string]string{}
	for _, line := range strings.Split(string(out), "\n") {
		if k, v, ok := strings.Cut(strings.TrimRight(line, "\r"), "="); ok {
			if _, dup := m[k]; !dup {
				m[k] = v
			}
		}
	}
	return m
}

// readFile is name's bytes; ok is false, with no error, where it is not
// there (host.ReadFileIfThere).
func readFile(ctx context.Context, sys host.System, name string) (data []byte, ok bool, err error) {
	if data, ok, err = host.ReadFileIfThere(ctx, sys, name); err != nil {
		return nil, false, fmt.Errorf("timesync: %w", err)
	}
	return data, ok, nil
}

// bad is an ErrMalformed naming file and, above 0, its line.
func bad(file string, line int, what string) error {
	if line > 0 {
		return fmt.Errorf("timesync: %s:%d: %s: %w", file, line, what, ErrMalformed)
	}
	return fmt.Errorf("timesync: %s: %s: %w", file, what, ErrMalformed)
}
