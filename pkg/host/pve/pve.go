// Package pve holds the rules hardn follows on a Proxmox VE host (refactor
// plan P2, next/p2-pve; brief §2 "Proxmox hosts: no UFW, and root SSH stays
// key-only", brief 2.5; decision §3.18, approved 2026-10-17: no SSH port
// change on Proxmox VE). V-numbers: V8 (the port), V10 (UFW) and V11
// (PermitRootLogin no) on Proxmox VE.
//
// Every entry point that decides whether a rule applies takes
// detect.Detect's Proxmox fact and applies the rules only when it is Yes; a
// fact that is not established is ErrProxmoxUnknown, never "not Proxmox
// VE". PlanKeys works on what ReadKeys read, and RootGroups is not
// Proxmox VE's.
//
//   - SSH (ssh.go): PlanSSH rewrites the settings hardn wants in
//     00-hardn.conf: PermitRootLogin prohibit-password, Port 22 only, root
//     kept reachable by key. CheckSSH reads sshd.ReadEffective's values and
//     the files' Match blocks the same way, and tells Proxmox VE's own
//     "PermitRootLogin yes" in sshd_config from drift.
//   - Root's keys (keys.go): /root/.ssh/authorized_keys is a link to the
//     cluster-wide /etc/pve/priv/authorized_keys. ReadKeys, PlanKeys,
//     ApplyKeys and UndoKeys edit through it, never over it; RollbackKeys
//     is UndoKeys for p2-revert's journal.
//   - Root's password (CheckAccount): never locked, removed or expired.
//   - The firewall (firewall.go): ReadFirewall reports pve-firewall's state,
//     read-only. UFW is never used (ufw.Applies).
//
// docs/safety/pve.md describes the guarantees, the Proxmox VE facts they
// rest on and the manual checklist for a Proxmox VE 9 host (written, not
// run).
package pve

import (
	"errors"
	"fmt"

	"github.com/abbott/hardn/pkg/host/detect"
)

// The ports Proxmox VE uses for SSH between nodes and for its web UI and API.
const (
	SSHPort = 22
	WebPort = 8006
)

// Errors the package returns, testable with errors.Is.
var (
	ErrNotApplicable  = errors.New("not a Proxmox VE host")
	ErrProxmoxUnknown = errors.New("whether the host runs Proxmox VE is not established")
	// ErrPortChange: decision §3.18.
	ErrPortChange = errors.New("the SSH port stays 22 on Proxmox VE: cluster SSH and pve-firewall's management rule use port 22")
	// ErrRootExcluded: the change would stop root logging in by key, which
	// Proxmox VE's nodes do to each other.
	ErrRootExcluded = errors.New("root must stay reachable by key on Proxmox VE: cluster nodes log in to each other as root")
	// ErrRootPassword: the web UI's root@pam login and the cluster join use
	// root's password.
	ErrRootPassword = errors.New("root's password stays usable on Proxmox VE: the web UI's root@pam login and the cluster join use it")
	// ErrClusterFSDown: pmxcfs is not mounted at /etc/pve, so the
	// cluster-wide files are not there.
	ErrClusterFSDown = errors.New("pmxcfs is not mounted at /etc/pve")
	// ErrKeysLink: /root/.ssh/authorized_keys is not the link to
	// /etc/pve/priv/authorized_keys that Proxmox VE keeps.
	ErrKeysLink = errors.New("root's authorized_keys is not Proxmox VE's link to /etc/pve/priv/authorized_keys")
	// ErrKeysChanged: the cluster-wide keys file changed between the plan
	// and the write (another node, or pvecm updatecerts, wrote it).
	ErrKeysChanged = errors.New("the cluster-wide authorized_keys changed since the plan")
	// ErrBadKey: not one authorized_keys line hardn can read.
	ErrBadKey = errors.New("not an authorized_keys key line")
	// ErrClusterKey: the key is a cluster node's root key, or the edit would
	// leave no key at all.
	ErrClusterKey = errors.New("the edit would remove a cluster node's root key, or every key")
	// ErrNoBackup: ApplyKeys was given no backup run.
	ErrNoBackup = errors.New("a backup run is required")
	// ErrPlanMismatch: a KeysPlan's New is not what its Added and Removed
	// make of the keys it was planned from.
	ErrPlanMismatch = errors.New("the keys plan's new content does not follow from its edit")
)

// Applies reports whether the Proxmox VE rules apply: true on Proxmox VE,
// false on another host, and ErrProxmoxUnknown (wrapping detect's reason)
// when that is not established.
func Applies(p detect.Proxmox) (bool, error) {
	switch p.Installed {
	case detect.Yes:
		return true, nil
	case detect.No:
		return false, nil
	}
	if p.Err != nil {
		return false, fmt.Errorf("pve: %w: %w", ErrProxmoxUnknown, p.Err)
	}
	return false, fmt.Errorf("pve: %w", ErrProxmoxUnknown)
}

// FindingKind is what a Finding reports. Its String is a stable code.
type FindingKind uint8

const (
	// FindingRootKeyOnly: PermitRootLogin was set to prohibit-password in
	// place of another value ("root: key-only (required by Proxmox
	// clustering)").
	FindingRootKeyOnly FindingKind = iota + 1
	// FindingRootAdded: root was added to AllowUsers so that it stays
	// reachable by key.
	FindingRootAdded
	// FindingProxmoxRootLogin: Proxmox VE's own "PermitRootLogin yes" in
	// sshd_config, which hardn's drop-in shadows. Expected; not drift.
	FindingProxmoxRootLogin
	// FindingPortNot22: sshd is configured to listen on a port other than
	// 22, or not on 22.
	FindingPortNot22
	// FindingRootUnreachable: a setting keeps root from logging in by key,
	// or may (the Finding says which).
	FindingRootUnreachable
	// FindingClusterWide: the keys edit applies to every node of the
	// cluster.
	FindingClusterWide
	// FindingRuleMayBlock: a pve-firewall rule may drop the port before
	// pve-firewall's built-in management rule accepts it.
	FindingRuleMayBlock
	// FindingBlacklist: the blacklist IP set has entries; traffic from them
	// is dropped.
	FindingBlacklist
	// FindingNotModelled: proxmox-firewall (nftables, tech preview) filters
	// the host; its rules are not read.
	FindingNotModelled
	// FindingNotKeyOnly: PermitRootLogin is not prohibit-password, globally
	// or in a Match block (the Finding says where).
	FindingNotKeyOnly

	findingEnd // one past the last kind; keep it last
)

var findingCodes = map[FindingKind]string{
	FindingRootKeyOnly:      "root-key-only",
	FindingRootAdded:        "root-allowed",
	FindingProxmoxRootLogin: "proxmox-root-login",
	FindingPortNot22:        "port-not-22",
	FindingRootUnreachable:  "root-unreachable",
	FindingClusterWide:      "cluster-wide-keys",
	FindingRuleMayBlock:     "rule-may-block",
	FindingBlacklist:        "blacklist",
	FindingNotModelled:      "firewall-not-modelled",
	FindingNotKeyOnly:       "root-not-key-only",
}

// String is the stable code (TestFindingCodes pins them).
func (k FindingKind) String() string {
	if c, ok := findingCodes[k]; ok {
		return c
	}
	return fmt.Sprintf("FindingKind(%d)", k)
}

// Finding is something a plan or a status report tells the user.
type Finding struct {
	Kind   FindingKind
	File   string // the file it is about; "" for none
	Line   int    // 1-based; 0 for none
	Detail string
}
