// Package header is the main menu's header probe (refactor plan Phase 4,
// next/p4-header): what the header shows of the host, read cheaply through
// host.System and returned as data, with nothing rendered. Read takes
// detect's facts, so the platform is not detected again, and gives:
//
//   - the distribution's name, version and codename, from detect's
//     os-release and Proxmox VE facts (OS);
//   - the hostname, as the kernel has it (/proc/sys/kernel/hostname);
//   - the primary address in each family: the address of the interface
//     the default route leaves by (Address);
//   - the uptime (/proc/uptime);
//   - whether a reboot is pending (Reboot): /var/run/reboot-required and
//     the running kernel against the newest in /boot on Debian, Ubuntu and
//     Proxmox VE, the running kernel's modules on Alpine.
//
// A fact that cannot be read, or is read in a form the probe does not
// know, is not established, with the reason in its Err: nothing is
// guessed. Read reads files and runs one command, `ip -4 addr show dev
// <interface>`, and changes nothing. pkg/output maps a Header to the main
// menu's Header, ContextBar and HostBlock. docs/engine/header.md has each
// fact's source and every line the blocks can show.
package header

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"strconv"
	"strings"
	"time"

	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/detect"
)

// The kernel's files the probe reads for the hostname and the uptime
// (proc_sys_kernel(5), proc_uptime(5)).
const (
	HostnameFile = "/proc/sys/kernel/hostname"
	UptimeFile   = "/proc/uptime"
)

// Header is what the probe read of the host. Each fact but Reboot is
// established when its Err is nil; whether Reboot is, its Pending says.
type Header struct {
	OS       OS
	Hostname Hostname
	Address  Address
	Uptime   Uptime
	Reboot   Reboot
}

// OS is the distribution, as the header names it.
type OS struct {
	// Name is Debian, Ubuntu, Alpine or Proxmox VE, or, for a
	// distribution hardn does not name, os-release's PRETTY_NAME (its ID
	// when it has none); "" when os-release was not read.
	Name string
	// Version is os-release's VERSION_ID, or on Proxmox VE pve-manager's
	// version; "" when there is none.
	Version string
	// Codename is os-release's VERSION_CODENAME as written ("trixie");
	// "" when there is none (Alpine sets none).
	Codename string
	Err      error
}

// Hostname is the host's name as the kernel has it: the name set, short
// or full, not /etc/hostname.
type Hostname struct {
	Name string
	Err  error
}

// Uptime is the time since the kernel booted, time in suspend included.
type Uptime struct {
	Duration time.Duration
	Err      error
}

// Read probes the host. f is detect's facts for it. A done ctx leaves
// every fact not yet read with ctx's error.
func Read(ctx context.Context, sys host.System, f detect.Facts) Header {
	return Header{
		OS:       osOf(f),
		Hostname: readHostname(ctx, sys),
		Address:  readAddress(ctx, sys),
		Uptime:   readUptime(ctx, sys),
		Reboot:   readReboot(ctx, sys, f),
	}
}

// names are the distributions hardn names, by os-release ID.
var names = map[string]string{"debian": "Debian", "ubuntu": "Ubuntu", "alpine": "Alpine"}

// osOf names the distribution from detect's facts: Proxmox VE when it is
// installed (its os-release is Debian's), else by os-release's ID.
func osOf(f detect.Facts) OS {
	r := f.OS
	switch {
	case !r.Known():
		return OS{Err: fmt.Errorf("header: os: %w", errOr(r.Err, detect.ErrNoSignal))}
	case f.Proxmox.Installed == detect.Yes:
		return OS{Name: "Proxmox VE", Version: f.Proxmox.Version, Codename: r.VersionCodename}
	}
	if name, ok := names[r.ID]; ok {
		return OS{Name: name, Version: r.VersionID, Codename: r.VersionCodename}
	}
	if r.PrettyName != "" {
		return OS{Name: r.PrettyName}
	}
	return OS{Name: r.ID}
}

// readHostname reads the kernel's hostname.
func readHostname(ctx context.Context, sys host.System) Hostname {
	data, err := sys.ReadFile(ctx, HostnameFile)
	if err != nil {
		return Hostname{Err: fmt.Errorf("header: hostname: %w", err)}
	}
	name := strings.TrimSpace(string(data))
	if name == "" {
		return Hostname{Err: fmt.Errorf("header: hostname: %s is empty: %w", HostnameFile, detect.ErrUnrecognised)}
	}
	return Hostname{Name: name}
}

// maxUptime is the longest uptime a time.Duration holds, in whole seconds.
const maxUptime = 9e9

// readUptime reads /proc/uptime's first number: two decimal numbers of
// seconds, the uptime and the idle time.
func readUptime(ctx context.Context, sys host.System) Uptime {
	data, err := sys.ReadFile(ctx, UptimeFile)
	if err != nil {
		return Uptime{Err: fmt.Errorf("header: uptime: %w", err)}
	}
	fields := strings.Fields(string(data))
	if len(fields) != 2 || !decimal(fields[0]) || !decimal(fields[1]) {
		return Uptime{Err: fmt.Errorf("header: uptime: %s: %q: %w", UptimeFile, firstLine(data), detect.ErrUnrecognised)}
	}
	secs, err := strconv.ParseFloat(fields[0], 64)
	if err != nil || secs >= maxUptime {
		return Uptime{Err: fmt.Errorf("header: uptime: %s: %q: %w", UptimeFile, fields[0], detect.ErrUnrecognised)}
	}
	return Uptime{Duration: time.Duration(secs * float64(time.Second))}
}

// decimal reports whether s is digits, with at most one point between
// digits, as the kernel prints seconds ("350735.47").
func decimal(s string) bool {
	whole, frac, _ := strings.Cut(s, ".")
	return digits(whole) && (frac == "" && !strings.HasSuffix(s, ".") || digits(frac))
}

func digits(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

// missing reports an error that means the path is not there.
func missing(err error) bool { return errors.Is(err, fs.ErrNotExist) }

func firstLine(data []byte) string {
	line, _, _ := strings.Cut(string(data), "\n")
	return strings.TrimSpace(line)
}

func errOr(err, def error) error {
	if err != nil {
		return err
	}
	return def
}
