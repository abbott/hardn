package main

import (
	"context"
	"fmt"
	"io/fs"
	"slices"
	"strings"

	probe "github.com/abbott/hardn/pkg/engine/header"
	"github.com/abbott/hardn/pkg/host/alpine"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/hostfake"
	"github.com/abbott/hardn/pkg/host/pve"
	"github.com/abbott/hardn/pkg/host/sshd"
	"github.com/abbott/hardn/pkg/host/sudoers"
)

// The status hosts' model (next/p4-gallery-status): what the status checks
// (pkg/engine/controls) and the header probe (pkg/engine/header) read of a
// simulated host, beyond what the engine's scenarios read. statusOn adds
// it to simulate's host of a platform, as the platform installs it, and
// each status host changes it as its note says (status.go). The formats
// are the ones the checks' and the probe's own tests pin (their site
// builders and testdata), written again here: a main package cannot share
// them. Nothing is run, written or read on this machine. Synthetic values
// only: alice, web-01 at 192.0.2.20, port 2230.

// readings is what a status host answers the checks and the probe besides
// the scenarios' commands: systemctl's states of the units they ask about,
// dpkg's packages, apt-config's dump, ip's addresses and crond's state. No
// host installs apk-cron: apk info -e finds no package.
type readings struct {
	units    map[string]unit   // systemctl is-active and is-enabled, by unit name
	packages map[string]string // dpkg-query --show: each package installed, and its version
	aptDump  string            // apt-config dump's output: "" sets none of its roots
	addrs    map[string]string // ip -4 addr show dev <interface>'s output, by interface
	crond    bool              // rc-service crond status: started
}

// unit is a systemd unit's states, as systemctl is-active and is-enabled
// print them; "" is not asked about.
type unit struct{ active, enabled string }

// installed is the version of pkg the readings install, "" for none.
func (r *readings) installed(pkg string) string {
	if r == nil {
		return ""
	}
	return r.packages[pkg]
}

// answer answers the checks' and the probe's commands that the scenarios'
// model does not, and reports whether it did: none on a host with no
// readings.
func (r *readings) answer(argv []string) (hostfake.Response, bool) {
	if r == nil {
		return hostfake.Response{}, false
	}
	cmd := strings.Join(argv, " ")
	last := argv[len(argv)-1]
	switch {
	case len(argv) == 3 && argv[0] == "systemctl" && argv[1] == "is-active" && r.units[last].active != "":
		st, code := r.units[last].active, 0
		if st != "active" {
			code = 3 // systemctl(1): is-active exits 0 for active alone
		}
		return hostfake.Response{Stdout: st + "\n", ExitCode: code}, true
	case len(argv) == 3 && argv[0] == "systemctl" && argv[1] == "is-enabled" && r.units[last].enabled != "":
		st := r.units[last].enabled
		return hostfake.Response{Stdout: st + "\n", ExitCode: enabledExit(st)}, true
	case len(argv) > 2 && argv[0] == "apt-config" && argv[1] == "dump":
		return hostfake.Response{Stdout: r.aptDump}, true
	case strings.HasPrefix(cmd, "ip -4 addr show dev ") && len(argv) == 6:
		if out, ok := r.addrs[last]; ok {
			return hostfake.Response{Stdout: out}, true
		}
		return hostfake.Response{Stderr: fmt.Sprintf("Device %q does not exist.\n", last), ExitCode: 1}, true
	case len(argv) == 4 && argv[0] == "apk" && argv[1] == "info" && argv[2] == "-e":
		return hostfake.Response{ExitCode: 1}, true
	case cmd == "rc-service crond status":
		if r.crond {
			return hostfake.Response{Stdout: " * status: started\n"}, true
		}
		return hostfake.Response{Stdout: " * status: stopped\n", ExitCode: 3}, true
	case len(argv) == 3 && argv[0] == "doas" && argv[1] == "-C": // doas.conf's check: every file passes
		return hostfake.Response{}, true
	}
	return hostfake.Response{}, false
}

// enabledExit is systemctl is-enabled's exit status for a state it prints
// (systemctl(1), its table of states).
func enabledExit(state string) int {
	switch state {
	case "enabled", "enabled-runtime", "alias", "static", "indirect", "generated", "transient":
		return 0
	case "not-found":
		return 4
	}
	return 1
}

// statusSys is a status host as the checks read it: hostfake keeps no
// setuid bit (its package doc), so Stat shows one on the elevation tools
// the platform installs setuid root, as the users check's own tests do.
type statusSys struct {
	*hostfake.System
	setuid []string
}

// Stat is the fake's, with the setuid bit on the tools.
func (s statusSys) Stat(ctx context.Context, name string) (fs.FileInfo, error) {
	fi, err := s.System.Stat(ctx, name)
	if err == nil && slices.Contains(s.setuid, name) {
		fi = setuidInfo{fi}
	}
	return fi, err
}

// setuidInfo is a file's information with its setuid bit set.
type setuidInfo struct{ fs.FileInfo }

func (i setuidInfo) Mode() fs.FileMode { return i.FileInfo.Mode() | fs.ModeSetuid }

// sys returns the host as the checks read it.
func (h *fakeHost) sys() statusSys {
	s := statusSys{System: h.f}
	if h.sudo != "" {
		s.setuid = append(s.setuid, "/usr/bin/sudo")
	}
	if h.doas {
		s.setuid = append(s.setuid, "/usr/bin/doas")
	}
	return s
}

// Each platform's kernel, as the header probe reads it: its release
// (uname -r), its build (/proc/sys/kernel/version, whose "Debian" names a
// kernel Debian built, and its package's version), as the probe's own
// tests write them; Alpine's build is not read.
type kernel struct{ release, build, version string }

var (
	debianKernel = kernel{"6.12.43+deb13-amd64", "#1 SMP PREEMPT_DYNAMIC Debian 6.12.43-1 (2025-08-27)", "6.12.43-1"}
	ubuntuKernel = kernel{"6.17.0-14-generic", "#14-Ubuntu SMP PREEMPT_DYNAMIC Fri Jan  9 10:00:00 UTC 2026", ""}
	pveKernel    = kernel{"6.17.2-1-pve", "#1 SMP PREEMPT_DYNAMIC PMX 6.17.2-1 (2025-10-21T11:55Z)", ""}
	alpineKernel = kernel{release: "6.12.40-0-lts"}
)

// The header probe's network: a default route via 192.0.2.1 from the
// host's one interface, 192.0.2.20 on it, and IPv6 with no default route
// (the kernel's unreachable default and the loopback's), in the kernel's
// and ip's forms (the probe's tests: /proc/net/route padded to 127
// characters, little-endian addresses).
const (
	route6 = "00000000000000000000000000000000 00 00000000000000000000000000000000 00 00000000000000000000000000000000 ffffffff 00000001 00000000 00200200       lo\n" +
		"00000000000000000000000000000001 80 00000000000000000000000000000000 00 00000000000000000000000000000000 00000000 00000002 00000000 80200001       lo\n"
	inet6 = "00000000000000000000000000000001 01 80 10 80       lo\n"
)

// routes is /proc/net/route for the default route and the connected
// 192.0.2.0/24 on iface.
func routes(iface string) string {
	pad := func(s string) string { return s + strings.Repeat(" ", max(0, 127-len(s))) + "\n" }
	return pad("Iface\tDestination\tGateway \tFlags\tRefCnt\tUse\tMetric\tMask\t\tMTU\tWindow\tIRTT") +
		pad(iface+"\t00000000\t010200C0\t0003\t0\t0\t100\t00000000\t0\t0\t0") +
		pad(iface+"\t000200C0\t00000000\t0001\t0\t0\t100\t00FFFFFF\t0\t0\t0")
}

// ipAddr is ip -4 addr show dev iface: iproute2's form, or BusyBox's on
// Alpine.
func ipAddr(iface string, busybox bool) string {
	if busybox {
		return "2: " + iface + ": <BROADCAST,MULTICAST,UP,LOWER_UP> mtu 1500 qdisc pfifo_fast qlen 1000\n" +
			"    inet 192.0.2.20/24 brd 192.0.2.255 scope global " + iface + "\n"
	}
	return "2: " + iface + ": <BROADCAST,MULTICAST,UP,LOWER_UP> mtu 1500 qdisc noqueue state UP group default qlen 1000\n" +
		"    inet 192.0.2.20/24 brd 192.0.2.255 scope global " + iface + "\n" +
		"       valid_lft forever preferred_lft forever\n"
}

// Uptimes, as /proc/uptime prints them ("%lu.%02lu %lu.%02lu\n"): 3 days,
// 5 hours and 12 minutes; Figma's 182 days, 2 hours and 26 minutes; 41
// days and 1 hour.
const (
	upDays    = "277920.37 553000.12\n"
	upFigma   = "15733560.42 31000000.17\n"
	upWeeks   = "3546000.00 7000000.00\n"
	upLXCHour = "3600.00 3590.00\n"
)

// AppArmor's profiles, as the kernel lists them (name (mode)); the names
// are written from the vendors' packages and the counts are illustrative,
// as the check's own fixtures are.
const (
	debianProfiles = "/usr/bin/man (enforce)\nlsb_release (enforce)\nman_filter (enforce)\nman_groff (enforce)\n" +
		"nvidia_modprobe (enforce)\nnvidia_modprobe//kmod (enforce)\nunix-chkpwd (enforce)\n"
	ubuntuProfiles = "/usr/bin/man (enforce)\n/usr/lib/snapd/snap-confine (enforce)\nbusybox (unconfined)\nlsb_release (enforce)\n" +
		"man_filter (enforce)\nman_groff (enforce)\nrsyslogd (enforce)\nsbuild (complain)\ntcpdump (enforce)\nunix-chkpwd (enforce)\n" +
		"unprivileged_userns (enforce)\n"
	pveProfiles = "/usr/bin/lxc-start (enforce)\n/usr/bin/man (enforce)\nlsb_release (enforce)\nlxc-101_</var/lib/lxc> (enforce)\n" +
		"lxc-container-default (enforce)\nlxc-container-default-cgns (enforce)\nlxc-container-default-with-mounting (enforce)\n" +
		"lxc-container-default-with-nesting (enforce)\nman_filter (enforce)\nman_groff (enforce)\nunix-chkpwd (enforce)\n"
)

// The paths the AppArmor check reads (the kernel's).
const (
	apparmorEnabled  = "/sys/module/apparmor/parameters/enabled"
	apparmorMode     = "/sys/module/apparmor/parameters/mode"
	apparmorProfiles = "/sys/kernel/security/apparmor/profiles"
)

// unattended is apt-config dump's output with unattended-upgrades on:
// Debian's Origins-Pattern, or Ubuntu's Allowed-Origins, as the Auto
// Updates check's fixtures write them.
func unattended(ubuntu bool) string {
	out := "APT::Periodic \"\";\nAPT::Periodic::Update-Package-Lists \"1\";\nAPT::Periodic::Unattended-Upgrade \"1\";\n"
	if ubuntu {
		return out + "Unattended-Upgrade::Allowed-Origins \"\";\n" +
			"Unattended-Upgrade::Allowed-Origins:: \"${distro_id}:${distro_codename}\";\n" +
			"Unattended-Upgrade::Allowed-Origins:: \"${distro_id}:${distro_codename}-security\";\n"
	}
	return out + "Unattended-Upgrade::Origins-Pattern \"\";\n" +
		"Unattended-Upgrade::Origins-Pattern:: \"origin=Debian,codename=${distro_codename},label=Debian\";\n" +
		"Unattended-Upgrade::Origins-Pattern:: \"origin=Debian,codename=${distro_codename},label=Debian-Security\";\n" +
		"Unattended-Upgrade::Origins-Pattern:: \"origin=Debian,codename=${distro_codename}-security,label=Debian-Security\";\n"
}

// apparmorState is the state of AppArmor's module a status host is built
// in (statusWith), off Alpine, whose kernel has none.
type apparmorState uint8

// The states of AppArmor's module.
const (
	apparmorStock     apparmorState = iota // on, enforcing the platform's profiles
	apparmorComplain                       // on, every profile in complain mode
	apparmorNone                           // on, no profile loaded
	apparmorKernelOff                      // off: apparmor=0 on the kernel's command line
	apparmorNoFS                           // on, securityfs not mounted
)

// statusOn returns a simulated host of platform p as the status checks and
// the header probe read it, as the platform installs it, but for what
// simulate already sets (sshd on 22, ufw active allowing 22 where it is
// installed): alice, uid 1000 with a login shell, in sudo (doas's wheel on
// Alpine), but on Proxmox VE, which has root alone; AppArmor enforcing the
// platform's profiles, loaded at boot (Alpine: none, its kernel without
// AppArmor); apt's daily timers, with unattended-upgrades installed and on
// on Ubuntu alone (Alpine: crond started, no apk-cron); on Proxmox VE
// Proxmox VE's PermitRootLogin yes and pve-firewall running, disabled;
// web-01 at 192.0.2.20, up 3 days, and the running kernel the newest
// installed.
func statusOn(p platform) *fakeHost { return statusWith(p, apparmorStock) }

// statusWith is statusOn with AppArmor's module in state aa.
func statusWith(p platform, aa apparmorState) *fakeHost {
	h := simulate(p)
	f := h.f
	r := &readings{units: map[string]unit{}, packages: map[string]string{}, addrs: map[string]string{}}
	h.status = r
	iface, k := "eth0", debianKernel
	switch {
	case p.openRC:
		k = alpineKernel
	case p.proxmox != "":
		iface, k = "vmbr0", pveKernel
	case p.socket:
		k = ubuntuKernel
	}
	// The header probe's files.
	f.AddFile(probe.HostnameFile, []byte(hostName+"\n"), 0o644, 0, 0)
	f.AddFile(probe.UptimeFile, []byte(upDays), 0o444, 0, 0)
	f.AddFile(probe.RouteFile, []byte(routes(iface)), 0o444, 0, 0)
	f.AddFile(probe.Route6File, []byte(route6), 0o444, 0, 0)
	f.AddFile(probe.Inet6File, []byte(inet6), 0o444, 0, 0)
	r.addrs[iface] = ipAddr(iface, p.openRC)
	h.installKernel(k)
	f.AddFile(probe.KernelReleaseFile, []byte(k.release+"\n"), 0o444, 0, 0)
	if k.build != "" {
		f.AddFile(probe.KernelVersionFile, []byte(k.build+"\n"), 0o444, 0, 0)
	}
	// The users check's accounts and rules: simulate's sudoers, or doas's
	// rule for wheel, as setup-alpine leaves it.
	for _, sh := range []string{"/bin/bash", "/bin/sh"} {
		f.AddFile(sh, []byte("ELF"), 0o755, 0, 0)
	}
	h.accounts(p.proxmox == "")
	if p.doas {
		f.AddFile(sudoers.DoasConfig, []byte("# Uncomment to allow group \"wheel\" to become root.\n# permit persist :wheel\n"), 0o440, 0, 0)
		f.AddDir(sudoers.DoasDir, 0o750, 0, 0)
		f.AddFile(sudoers.DoasDir+"/20-wheel.conf", []byte("permit persist :wheel\n"), 0o440, 0, 0)
	}
	// AppArmor and the updates.
	if p.openRC {
		f.AddDir("/sys/module/kernel", 0o755, 0, 0)
		f.AddDir(alpine.DailyDir, 0o755, 0, 0)
		f.AddFile(alpine.InitScript, []byte("#!/sbin/openrc-run\n"), 0o755, 0, 0)
		f.AddSymlink(alpine.RunlevelLink, alpine.InitScript)
		r.crond = true
		return h
	}
	profiles, cmdline := debianProfiles, "BOOT_IMAGE=/boot/vmlinuz-"+k.release+" root=/dev/vda1 ro quiet"
	switch {
	case p.proxmox != "":
		profiles = pveProfiles
	case p.socket:
		profiles = ubuntuProfiles
	}
	switch aa {
	case apparmorComplain:
		profiles = strings.ReplaceAll(profiles, "(enforce)", "(complain)")
	case apparmorNone:
		profiles = ""
	case apparmorKernelOff:
		cmdline += " apparmor=0"
	}
	f.AddFile("/proc/cmdline", []byte(cmdline+"\n"), 0o444, 0, 0)
	switch aa {
	case apparmorKernelOff:
		f.AddFile(apparmorEnabled, []byte("N\n"), 0o444, 0, 0)
	case apparmorNoFS:
		f.AddFile(apparmorEnabled, []byte("Y\n"), 0o444, 0, 0)
		f.AddFile(apparmorMode, []byte("enforce"), 0o600, 0, 0)
		f.AddDir("/sys/kernel/security", 0o755, 0, 0)
	default:
		f.AddFile(apparmorEnabled, []byte("Y\n"), 0o444, 0, 0)
		f.AddFile(apparmorMode, []byte("enforce"), 0o600, 0, 0)
		f.AddFile(apparmorProfiles, []byte(profiles), 0o444, 0, 0)
	}
	r.units["apparmor.service"] = unit{enabled: "enabled"}
	for _, t := range []string{"apt-daily.timer", "apt-daily-upgrade.timer"} {
		r.units[t] = unit{active: "active", enabled: "enabled"}
	}
	for _, s := range []string{"apt-daily.service", "apt-daily-upgrade.service"} {
		r.units[s] = unit{enabled: "static"}
	}
	if p.socket {
		h.unattendedUpgrades()
	}
	if p.proxmox != "" {
		f.AddFile(sshd.MainConfig, []byte(stockMain+"PermitRootLogin yes\n"), 0o644, 0, 0)
		r.units[pve.FirewallUnit] = unit{active: "active"}
		h.boot()
	}
	return h
}

// installKernel installs kernel k as the newest: its image in /boot, and
// for a kernel Debian built its package at its version (Debian, Ubuntu
// and Proxmox VE), or its modules (Alpine).
func (h *fakeHost) installKernel(k kernel) {
	if h.openRC {
		h.f.AddDir(probe.ModulesDir+"/"+k.release+"/kernel", 0o755, 0, 0)
		return
	}
	h.f.AddFile(probe.BootDir+"/vmlinuz-"+k.release, nil, 0o644, 0, 0)
	if k.version != "" {
		h.status.packages[probe.KernelPackagePrefix+k.release] = k.version
	}
}

// accounts writes /etc/passwd and /etc/group: root and the system
// accounts, and with admin alice, uid 1000 with a login shell, in sudo, or
// on a host with doas in wheel.
func (h *fakeHost) accounts(admin bool) {
	shell, nologin, tool := "/bin/bash", "/usr/sbin/nologin", "sudo:x:27:"
	if h.openRC {
		shell, nologin = "/bin/sh", "/sbin/nologin"
	}
	if h.doas {
		tool = "wheel:x:10:root"
	}
	passwd := "root:x:0:0:root:/root:" + shell + "\n" +
		"daemon:x:1:1:daemon:/usr/sbin:" + nologin + "\n" +
		"sshd:x:100:65534::/run/sshd:" + nologin + "\n" +
		"nobody:x:65534:65534:nobody:/nonexistent:" + nologin + "\n"
	group := "root:x:0:\n" + tool
	switch {
	case admin && h.doas:
		group += ",alice"
	case admin:
		group += "alice"
	}
	group += "\nnogroup:x:65534:\n"
	if admin {
		passwd += "alice:x:1000:1000:alice:/home/alice:" + shell + "\n"
		group += "alice:x:1000:\n"
	}
	h.f.AddFile("/etc/passwd", []byte(passwd), 0o644, 0, 0)
	h.f.AddFile("/etc/group", []byte(group), 0o644, 0, 0)
}

// unattendedUpgrades installs unattended-upgrades, on: its package, and
// apt-config's items (Ubuntu's or Debian's).
func (h *fakeHost) unattendedUpgrades() {
	h.status.packages["unattended-upgrades"] = "2.12"
	h.status.aptDump = unattended(h.socket)
}

// harden makes the host what hardn's baseline wants (checks.md §1.3), as
// hardn's 2230 change, key-only login and root's login refused leave it
// once confirmed: 00-hardn.conf with Port 2230, PermitRootLogin no,
// PasswordAuthentication no and KbdInteractiveAuthentication no, sshd
// reloaded onto 2230, ufw allowing 2230 alone, and unattended-upgrades on.
func (h *fakeHost) harden() {
	h.f.AddFile(sshd.DropInDir+"/00-hardn.conf", []byte("Port 2230\nPermitRootLogin no\nPasswordAuthentication no\nKbdInteractiveAuthentication no\n"), 0o644, 0, 0)
	h.boot()
	h.allowed = []uint16{2230}
	h.saveRules()
	h.unattendedUpgrades()
}

// rebootPending is a newer kernel installed than the one running, and
// /var/run/reboot-required written, as an upgrade of the kernel leaves
// Ubuntu.
func (h *fakeHost) rebootPending() {
	h.f.AddFile(probe.BootDir+"/vmlinuz-6.17.0-19-generic", nil, 0o644, 0, 0)
	h.f.AddFile(probe.RebootRequiredFile, []byte("*** System restart required ***\n"), 0o644, 0, 0)
}

// inContainer is the host in an LXC container, as systemd as its PID 1
// says (/run/systemd/container): no kernel of its own in /boot, and the
// uptime LXCFS gives it.
func (h *fakeHost) inContainer() {
	h.f.AddFile(detect.SystemdContainerFile, []byte("lxc\n"), 0o444, 0, 0)
	h.f.AddFile(detect.InitEnviron, []byte("container=lxc\x00HOME=/\x00TERM=linux\x00"), 0o400, 0, 0)
	h.f.AddFile(probe.UptimeFile, []byte(upLXCHour), 0o444, 0, 0)
}
