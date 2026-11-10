package main

import (
	"path"
	"regexp"
	"slices"
	"strings"
)

// rule is one command p4status runs: argv[0] exactly as the code passes
// it, and the shape of the arguments.
type rule struct {
	name string
	args func(args []string) bool
}

// rules are the commands detect's facts, the Phase 4 checks and the header
// probe run, found in their code, with the shapes of their arguments
// (docs/engine/vmcheck.md §2.3 gives each one's source). Each only reads.
// The words are written out rather than taken from the packages'
// constants, so that the list says what runs; TestRulesMatchTheCode holds
// them to the constants.
var rules = []rule{
	// sshd.ReadEffective: sshd -T over the installed files, and once per
	// Match block with -C; on Alpine with reload.OpenRCFlavour's binary.
	{"sshd", sshdT},
	{"/usr/sbin/sshd", sshdT},
	// reload.OpenRCFlavour: -G of the PAM and Kerberos builds, which the
	// init script asks the same way.
	{"/usr/sbin/sshd.pam", either(sshdT, exactly("-f", "/etc/ssh/sshd_config", "-G"))},
	{"/usr/sbin/sshd.krb5", either(sshdT, exactly("-f", "/etc/ssh/sshd_config", "-G"))},
	{"systemctl", systemctl},
	{"ss", exactly("-Htln")},              // reload.ReadBound
	{"ufw", exactly("status", "verbose")}, // ufw.Read
	// The Auto Updates check (updates.dump).
	{"apt-config", exactly("dump", "APT::Periodic", "Unattended-Upgrade::Origins-Pattern", "Unattended-Upgrade::Allowed-Origins")},
	{"dpkg-query", dpkgQuery},
	{"sudo", exactly("--version")},             // detect.DetectElevation
	{"visudo", exactly("-c")},                  // sudoers.CheckInstalled, sudo
	{"doas", doasCheck},                        // sudoers.CheckInstalled, doas
	{"ip", ipAddr},                             // the header probe's address
	{"apk", exactly("info", "-e", "apk-cron")}, // alpine.ReadUpdates
	{"rc-service", exactly("crond", "status")}, // alpine.ReadUpdates
	// The header probe's reboot source on Rocky Linux and AlmaLinux: this
	// one dnf command, which loads no repository and doesn't wait for
	// another dnf's lock.
	{"dnf", exactly("--setopt=exit_on_lock=True", "needs-restarting", "-r")},
	// The Auto Updates check there (dnfauto.Read).
	{"rpm", exactly("-q", "dnf-automatic")},
	// The Firewall check on Rocky Linux and AlmaLinux (firewalld.Read, and
	// the default zone's target).
	{"firewall-cmd", firewallCmd},
	// The DNS check (dns.Read): systemd-resolved's servers and search
	// domains, as resolvectl lists them.
	{"resolvectl", either(exactly("--no-pager", "dns"), exactly("--no-pager", "domain"))},
}

// allowed reports whether argv is a command p4status runs: a rule's name
// and arguments of its shape.
func allowed(argv []string) bool {
	if len(argv) == 0 {
		return false
	}
	return slices.ContainsFunc(rules, func(r rule) bool { return r.name == argv[0] && r.args(argv[1:]) })
}

// exactly is the shape of these arguments and no others.
func exactly(want ...string) func([]string) bool {
	return func(args []string) bool { return slices.Equal(args, want) }
}

// either is the shapes a and b.
func either(a, b func([]string) bool) func([]string) bool {
	return func(args []string) bool { return a(args) || b(args) }
}

// connSpec is sshd -C's connection as sshd.Conn.Spec writes it: user, host
// and addr, then laddr, lport and rdomain where set, in that order, no
// value holding a comma or white space (sshd.Conn.check).
var connSpec = regexp.MustCompile(`^user=[^,\s]+,host=[^,\s]+,addr=[^,\s]+(,laddr=[^,\s]+)?(,lport=[^,\s]+)?(,rdomain=[^,\s]+)?$`)

// sshdT is sshd -T -f /etc/ssh/sshd_config, alone or with -C and one
// connection: sshd's test mode, which reads its files and prints.
func sshdT(args []string) bool {
	base := []string{"-T", "-f", "/etc/ssh/sshd_config"}
	switch {
	case slices.Equal(args, base):
		return true
	case len(args) == len(base)+2 && slices.Equal(args[:len(base)], base) && args[3] == "-C":
		return connSpec.MatchString(args[4])
	}
	return false
}

// units are the units the checks ask systemctl about, by verb: detect's
// (ssh.socket and ssh.service, and sshd.socket and sshd.service where
// systemd has neither ssh.* unit), the AppArmor check's (apparmor.service), the
// Auto Updates check's (apt's timers and their services; dnf-automatic's
// install and plain timers on Rocky Linux and AlmaLinux), the Firewall
// row's on Rocky Linux and AlmaLinux (firewalld.service), Proxmox VE's
// firewall's (pve.ReadFirewall) and the DNS check's (systemd-resolved).
var units = map[string][]string{
	"is-enabled": {"ssh.socket", "ssh.service", "sshd.socket", "sshd.service", "ufw.service", "firewalld.service", "apparmor.service",
		"dnf-automatic-install.timer", "dnf-automatic.timer",
		"apt-daily-upgrade.timer", "apt-daily-upgrade.service", "apt-daily.timer", "apt-daily.service"},
	"is-active": {"ssh.socket", "ssh.service", "sshd.socket", "sshd.service", "apt-daily-upgrade.timer", "apt-daily.timer",
		"pve-firewall.service", "proxmox-firewall.service", "systemd-resolved.service", "firewalld.service",
		"dnf-automatic-install.timer", "dnf-automatic.timer"},
}

// systemctl is is-enabled or is-active of one of units, or ssh.socket's
// listeners (sshd.ReadEffective): queries, which change no unit.
func systemctl(args []string) bool {
	if slices.Equal(args, []string{"show", "-p", "Listen", "ssh.socket"}) {
		return true
	}
	return len(args) == 2 && slices.Contains(units[args[0]], args[1])
}

// kernelPackage is the package of a kernel release, as the header probe
// names it (header.KernelPackagePrefix and the release, which holds no
// slash or white space): a release's characters only, so no pattern of
// dpkg-query's.
var kernelPackage = regexp.MustCompile(`^linux-image-[0-9A-Za-z][0-9A-Za-z.+~_-]*$`)

// dpkgQuery is dpkg-query --show of one package in detect's format:
// pve-manager (detect.DetectProxmox), unattended-upgrades (the Auto
// Updates check), the SSH server packages (sshd.NoServer: the SSH rows,
// the Firewall row and Enable, on a host without sshd), or the running
// kernel's (the header probe).
func dpkgQuery(args []string) bool {
	if len(args) != 3 || args[0] != "--show" || args[1] != `--showformat=${Status}\t${Version}\n` {
		return false
	}
	switch args[2] {
	case "pve-manager", "unattended-upgrades", "openssh-server", "dropbear", "dropbear-bin", "tinysshd":
		return true
	}
	return kernelPackage.MatchString(args[2])
}

// doasCheck is doas -C of /etc/doas.conf or of a *.conf in /etc/doas.d:
// doas's check of a file, which parses it and exits.
func doasCheck(args []string) bool {
	if len(args) != 2 || args[0] != "-C" {
		return false
	}
	p := args[1]
	if p == "/etc/doas.conf" {
		return true
	}
	return path.Clean(p) == p && path.Dir(p) == "/etc/doas.d" && strings.HasSuffix(p, ".conf")
}

// ipAddr is ip -4 addr show dev of one interface, whose name the kernel
// would accept (dev_valid_name: 1 to 15 bytes, not "." or "..", no slash,
// colon or white space).
func ipAddr(args []string) bool {
	if len(args) != 5 || !slices.Equal(args[:4], []string{"-4", "addr", "show", "dev"}) {
		return false
	}
	dev := args[4]
	return dev != "" && len(dev) < 16 && dev != "." && dev != ".." && !strings.ContainsAny(dev, "/: \t\n\v\f\r")
}

// firewallCmd is the Firewall check's firewall-cmd on Rocky Linux and
// AlmaLinux: --state, --get-default-zone, --get-active-zones, or a zone's
// permanent target (a zone name of firewalld's letters).
func firewallCmd(args []string) bool {
	switch {
	case len(args) == 1:
		return args[0] == "--state" || args[0] == "--get-default-zone" || args[0] == "--get-active-zones"
	case len(args) == 3 && args[0] == "--permanent" && args[2] == "--get-target":
		z, ok := strings.CutPrefix(args[1], "--zone=")
		return ok && z != "" && strings.Trim(z, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_-") == ""
	}
	return false
}
