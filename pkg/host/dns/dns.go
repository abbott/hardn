// Package dns sets the host's DNS resolvers through whatever manages
// /etc/resolv.conf (refactor plan P2, next/p2-dns; V19).
//
// Read establishes the manager and what it holds, with Unknown as a value:
// systemd-resolved (its stub or uplink file linked from /etc/resolv.conf),
// a plain file, Proxmox VE's file, or a manager hardn reports and does not
// write (resolvconf, NetworkManager, an unrecognised link). Validate
// refuses an empty nameserver list and anything that is not an IPv4 or
// IPv6 address. PlanDNS is pure; Apply runs a plan over host.System,
// backing up each file first, checking each step and undoing the steps it
// ran when one fails; Rollback undoes a plan. On systemd-resolved hardn
// writes one late-sorting drop-in and never /etc/resolv.conf, which is a
// symlink there.
//
// docs/safety/dns.md describes the guarantees, the platform facts they rest
// on and what is written but not yet run on a host.
package dns

import (
	"errors"
	"fmt"
	"io/fs"
	"slices"

	"github.com/abbott/hardn/pkg/host/detect"
)

// Paths and names.
const (
	ResolvConf = "/etc/resolv.conf"
	// DropInDir is the administrator's resolved.conf.d; DropIn is hardn's
	// file there. Drop-ins apply in C byte order of their names across all
	// four directories, and "zz-" sorts after every name that starts with a
	// digit, an upper-case letter or a to y.
	DropInDir  = "/etc/systemd/resolved.conf.d"
	DropInName = "zz-hardn.conf"
	DropIn     = DropInDir + "/" + DropInName
	// ResolvedUnit is systemd-resolved's unit.
	ResolvedUnit = "systemd-resolved.service"
	// UDHCPCConf is what Alpine's udhcpc script sources before it rewrites
	// /etc/resolv.conf on a lease.
	UDHCPCConf = "/etc/udhcpc/udhcpc.conf"
	// MaxNameservers is how many nameserver lines glibc and musl use; later
	// ones are ignored, so more are refused for a file.
	MaxNameservers = 3
	// MaxLine is the longest resolv.conf line musl reads, newline included;
	// it skips a longer one whole.
	MaxLine = 255
)

// dropInDirs are the directories systemd-resolved reads drop-ins from, in
// the order in which a name in one hides the same name in those after it.
var dropInDirs = []string{DropInDir, "/run/systemd/resolved.conf.d", "/usr/local/lib/systemd/resolved.conf.d", "/usr/lib/systemd/resolved.conf.d"}

// DropInDirs returns a copy of the directories systemd-resolved reads
// drop-ins from, highest priority first.
func DropInDirs() []string { return slices.Clone(dropInDirs) }

// Errors, testable with errors.Is.
var (
	// ErrUnknown: a fact the answer depends on is not established; the
	// error wraps why.
	ErrUnknown = errors.New("a fact the answer depends on is not established")
	// ErrUnsupported: not a distribution family hardn knows. Whether the
	// release is supported is the engine's gate (detect.Support), not this.
	ErrUnsupported = errors.New("not a distribution family hardn knows")
	// ErrContainer: the container manager provides /etc/resolv.conf.
	ErrContainer = errors.New("in a container, the container manager provides /etc/resolv.conf")
	// ErrManaged: /etc/resolv.conf belongs to a manager hardn reports and
	// does not configure; the error names it.
	ErrManaged = errors.New("/etc/resolv.conf is managed by something hardn does not configure")
	// ErrNotRunning: /etc/resolv.conf is systemd-resolved's link, but
	// systemd-resolved is not running.
	ErrNotRunning = errors.New("systemd-resolved is not running")
	// ErrOverridden: a drop-in sorting after DropInName sets DNS=, or
	// Domains= when the plan sets the search domains, so hardn's would not
	// take effect.
	ErrOverridden = errors.New("a later resolved.conf.d drop-in sets DNS= or Domains=")
	// ErrDropInBlocked: something other than a regular file (a /dev/null
	// mask, a directory) is at DropIn.
	ErrDropInBlocked = errors.New("something other than a regular file is at hardn's drop-in path")
	// ErrInvalid: the requested configuration is refused; the error says
	// why.
	ErrInvalid       = errors.New("invalid DNS configuration")
	ErrNoNameservers = fmt.Errorf("%w: no nameserver", ErrInvalid)
	ErrNotApplicable = fmt.Errorf("%w: option does not apply to this host", ErrInvalid)
	ErrNoBackup      = errors.New("a file the plan writes needs a backup run")
	ErrChanged       = errors.New("the host changed since it was read")
	// ErrNotEffective: a step succeeded but the host does not show its
	// effect.
	ErrNotEffective = errors.New("the step ran, but the host does not show its effect")
	ErrNotRestored  = errors.New("the steps were not all undone")
)

// Manager is what owns /etc/resolv.conf; its String is stable.
type Manager uint8

const (
	ManagerUnknown        Manager = iota
	ManagerFile                   // a regular file, or none: hardn writes it
	ManagerProxmox                // Proxmox VE's file: hardn writes it in PVE's form
	ManagerResolved               // systemd-resolved: hardn writes DropIn and reloads
	ManagerResolvconf             // resolvconf or openresolv: reported, not written
	ManagerNetworkManager         // reported, not written
	// ManagerForeign: a link hardn does not recognise, or a file that a
	// running systemd-resolved reads as another package's: reported.
	ManagerForeign
)

func (m Manager) String() string {
	switch m {
	case ManagerFile:
		return "file"
	case ManagerProxmox:
		return "proxmox"
	case ManagerResolved:
		return "systemd-resolved"
	case ManagerResolvconf:
		return "resolvconf"
	case ManagerNetworkManager:
		return "networkmanager"
	case ManagerForeign:
		return "foreign"
	}
	return "unknown"
}

// FindingKind names a finding; its String is stable.
type FindingKind uint8

const (
	// FindingPerLink: systemd-resolved queries per-link servers (DHCP,
	// netplan, networkd) in parallel to the global ones hardn sets.
	FindingPerLink FindingKind = iota + 1
	// FindingDHCPClient: a DHCP client that may rewrite the file is
	// installed.
	FindingDHCPClient
	// FindingUDHCPC: Alpine's udhcpc rewrites the file on each lease.
	FindingUDHCPC
	// FindingProxmox: Proxmox VE's node DNS settings rewrite the file.
	FindingProxmox
)

func (k FindingKind) String() string {
	switch k {
	case FindingPerLink:
		return "dns-per-link"
	case FindingDHCPClient:
		return "dns-dhcp-client"
	case FindingUDHCPC:
		return "dns-udhcpc"
	case FindingProxmox:
		return "dns-proxmox-node"
	}
	return "unknown"
}

// Finding is something a plan reports; it never stops a plan.
type Finding struct {
	Kind   FindingKind
	Path   string
	Detail string
	// Link is the link or delegate a FindingPerLink is about, with its
	// servers; nil for the other kinds.
	Link *Link `json:",omitempty"`
}

// EntryKind is what a path holds.
type EntryKind uint8

const (
	EntryUnknown EntryKind = iota // not read
	EntryAbsent
	EntryFile
	EntrySymlink
	EntryOther // a directory, a device, ...
)

// Entry is a path as read: a symlink's target, or a regular file's
// content, permission bits and owner.
type Entry struct {
	Kind     EntryKind
	Target   string // EntrySymlink, as stored
	Data     []byte // EntryFile
	Mode     fs.FileMode
	UID, GID int
}

func (e Entry) equal(o Entry) bool {
	return e.Kind == o.Kind && e.Target == o.Target && string(e.Data) == string(o.Data) &&
		e.Mode == o.Mode && e.UID == o.UID && e.GID == o.GID
}

// Host is what Read takes from detect's facts.
type Host struct {
	Distro  detect.Distro
	Systemd detect.Tri // systemd booted the host
}

func errOr(err, def error) error {
	if err != nil {
		return err
	}
	return def
}
