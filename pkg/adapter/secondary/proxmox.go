package secondary

import (
	"errors"
	"os"
	"strings"

	"github.com/abbott/hardn/pkg/interfaces"
)

// proxmoxTreatedAsDebian: DISABLED 2026-10-17. Flip to true to restore.
//
// With it true, a Proxmox VE host gets Debian's steps unchanged: UFW is set
// to default deny and enabled, which blocks the web UI on 8006, corosync
// and migration and competes with pve-firewall (V10), and PermitRootLogin
// no is written, which breaks the root key login that cluster nodes use
// for migration and replication (V11; Phase 0 side-effects §4.3, harness
// A12).
//
// With it false, brief §2's rules apply on a Proxmox host (ProxmoxHost):
// hardn never configures or enables UFW there (errUFWOnProxmox, before any
// ufw command), and wherever it would write PermitRootLogin no it writes
// PermitRootLogin prohibit-password, key-only root login, instead.
const proxmoxTreatedAsDebian = false

// errUFWOnProxmox is the refusal of a UFW change on a Proxmox host.
var errUFWOnProxmox = errors.New("UFW is not used on Proxmox VE hosts: pve-firewall manages the packet filter, so hardn neither configures nor enables UFW")

// pveRootLogin is the PermitRootLogin value hardn writes on Proxmox in
// place of "no".
const pveRootLogin = "prohibit-password"

// ProxmoxHost says whether the host is Proxmox VE, finding out once,
// through the file system and commander seams, when first asked.
type ProxmoxHost struct {
	fs        interfaces.FileSystem
	commander interfaces.Commander
	osType    string
	detected  bool // osdetect already found /etc/pve
	known     bool
	proxmox   bool
}

// NewProxmoxHost returns the check for a host whose osdetect.OSInfo says
// osType, and IsProxmox as detected. The ServiceFactory shares one between
// the repositories it builds.
func NewProxmoxHost(fs interfaces.FileSystem, commander interfaces.Commander, osType string, detected bool) *ProxmoxHost {
	return &ProxmoxHost{fs: fs, commander: commander, osType: osType, detected: detected}
}

// IsProxmox reports whether the host is Proxmox VE: osdetect said so, or
// /etc/pve (the cluster file system's mount point) exists, or, outside
// Alpine, dpkg reports the pve-manager package installed. A /etc/pve that
// cannot be checked counts as present, as in osdetect: wrongly taking a
// host for Proxmox leaves UFW off and root key login on, while the reverse
// can cut a cluster off. While proxmoxTreatedAsDebian is set it is always
// false and checks nothing.
func (h *ProxmoxHost) IsProxmox() bool {
	if proxmoxTreatedAsDebian {
		return false
	}
	if !h.known {
		h.proxmox, h.known = h.detect(), true
	}
	return h.proxmox
}

func (h *ProxmoxHost) detect() bool {
	if h.detected {
		return true
	}
	if _, err := h.fs.Stat("/etc/pve"); !os.IsNotExist(err) {
		return true
	}
	if h.osType == "alpine" {
		return false
	}
	out, err := h.commander.Execute("dpkg-query", "-W", "--showformat=${Status}", "pve-manager")
	return err == nil && strings.Contains(string(out), "install ok installed")
}

// proxmoxOrProbe returns h, or when h is nil a check of its own on fs and
// commander, so a repository built without one still finds a Proxmox host.
func proxmoxOrProbe(h *ProxmoxHost, fs interfaces.FileSystem, commander interfaces.Commander, osType string) *ProxmoxHost {
	if h != nil {
		return h
	}
	return NewProxmoxHost(fs, commander, osType, false)
}
