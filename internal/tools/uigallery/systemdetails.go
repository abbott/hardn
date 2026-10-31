package main

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"slices"
	"strconv"
	"strings"

	"github.com/charmbracelet/colorprofile"

	"github.com/abbott/hardn/pkg/engine/details"
	probe "github.com/abbott/hardn/pkg/engine/header"
	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/hostfake"
	"github.com/abbott/hardn/pkg/host/sudoers"
	"github.com/abbott/hardn/pkg/output"
	"github.com/abbott/hardn/pkg/ui/component"
	"github.com/abbott/hardn/pkg/ui/nav"
	"github.com/abbott/hardn/pkg/ui/screen"
	"github.com/abbott/hardn/pkg/ui/term"
	"github.com/abbott/hardn/pkg/ui/theme"
)

// The System Details mocks (refactor plan slice 1, unit (a), next/s1-gallery,
// adopted 2026-10-30): the System Details screen, main menu 9, for unit (c) to
// build, nothing of it wired into hardn. There is no Figma
// frame and it is not in v0.3.2: it is inferred from origin/main
// (pkg/system/display.go:43-129, pkg/menu/system_details_menu.go), with
// P-17's sections, the 2025 prototype's as reference only. Each mock's note
// opens with sdLabel, P-17's label, but those in origin/main's own words.
//
// It only reads, so it has no plan, no y/N and no journal: its sequence is
// the reading, opening, each state, refreshing and leaving.
//
// Real code reads a simulated host: details.Read, on detect's facts, and
// output.Build's document (next/s1-readers). The sections, rows and layout are
// still the gallery's, unit (c)'s to build, as is detailsModel, what the
// simulated host answers the reader, in the forms that ran read-only, as a user,
// on the Debian 13, Ubuntu 26.04 and Alpine 3.23 VMs (vm-runs/s1), but ZFS's, no
// VM having it, and /proc/loadavg, inet6, df's header and /proc/self/status, which
// no VM printed.
//
// "system-details" lists the mocks with no input; "play system-details" is
// the menu of the hosts and "play system-details/<host>" the screen on one.

// sdPrefix names the mocks: "system-details <mock>" in the listing,
// "system-details" and "system-details/<host>" in play.
const sdPrefix = "system-details"

// sdLabel is the source label every mock carries (plan: "inferred from
// origin/main (unreleased)"; P-17).
const sdLabel = "Inferred from origin/main (unreleased)."

// sdMainHost is the host the main menu's 9 leads to.
const sdMainHost = sdPrefix + "/debian13"

// sdLinks lead from a menu mock's item to the System Details screen on a
// host, by the mock's name and the item's key, beside links: the main
// menu's 9, which said the gallery had no screen for it until this unit.
var sdLinks = map[string]string{mainMenuMock + "/9": sdMainHost}

// The screen's words: the main menu's label for the title, Backup's and
// Environment's decided heading and prompt (screen-backup.md §5, P-29 (a));
// P-17's sections; the rest decided (gallery-system-details.md §6).
const (
	sdTitle   = "System Details"
	sdHeading = "Select an option"
	sdPrompt  = "Enter selection"
	secOS     = "OS and Kernel"
	secHost   = "Host and IPs"
	secUsers  = "Users"
	secCPU    = "CPU and Load"
	secDisk   = "Disk"
	secMemory = "Memory"
	secLogin  = "Last Login"
	// Rows.
	rowOS       = "OS"
	rowKernel   = "Kernel"
	rowUptime   = "Uptime"
	rowHostname = "Hostname"
	rowModel    = "Model"
	rowCPUs     = "CPUs"
	rowLoad     = "Load (1, 5, 15 min)"
	rowUsed     = "Used"
	rowPool     = "Pool %s"
	rowRoot     = "/"
	noAddresses = "No addresses"
	noUsers     = "None but root"
	noSudo      = "No sudo"
	noDoas      = "No doas"
	noLogin     = "None recorded"
	cantRead    = "Can't read %s"
	notKnown    = "Not known"
	// rowLogins and noLslogins are Last Login's one row where the host has no
	// lslogins, as Alpine 3.23 has none (vm-runs/s1/01-tools.log).
	rowLogins  = "Logins"
	noLslogins = "Not known: lslogins isn't installed"
	// fromHost and onTTY follow the time of a login, with a remote host and
	// with none.
	fromHost = "%s from %s"
	onTTY    = "%s on %s"
	// notRootRow is said under Users to a user other than root, who cannot
	// read the rules that say who may administer.
	notRootRow = "Not root: %s not read"
	// Items.
	itemRefresh  = "Refresh"
	aboutRefresh = "Read the host again"
	itemOpen     = "Open"
)

// sdHost is one simulated host the System Details mocks run on.
type sdHost struct {
	// name is the host's, after sdPrefix: "debian13".
	name string
	// label and about are its item in the menu of the hosts.
	label, about string
	// note says what the host is.
	note string
	// host returns the simulated host, fresh.
	host func() *fakeHost
}

// The simulated hosts' CPUs, as /proc/cpuinfo names them, each synthetic: a
// VM's (the VMs' own say Common KVM processor, vm-runs/s1/01-tools.log), and on
// the Proxmox VE node a server's (its length real: origin/main shows it whole
// in a 50-column box, display.go:32, which wraps [I]).
const (
	vmCPU     = "QEMU Virtual CPU version 2.5+"
	serverCPU = "Intel(R) Xeon(R) Gold 6338 CPU @ 2.00GHz"
)

// sdHosts are the hosts, in the menu's order: the four releases, then the
// states.
func sdHosts() []sdHost {
	const (
		deb   = hostFQDN + " runs Debian 13, as the VM of 2026-10-21 (vmtest.md §8.5): kernel 6.12.113+deb13-cloud-amd64, sudo. "
		other = "Debian 13, as debian13. "
	)
	return []sdHost{
		{name: "debian13", label: "Debian 13", about: "ext4, sudo", host: vmDetails(debian13Platform),
			note: deb + "The report, Refresh and Return. Last Login is lslogins' newest login for root and each user (decision 3 (a)), the same answer as a user and as root, an account with none answering blanks (vm-runs/s1/02-login.log)."},
		{name: "ubuntu2604", label: "Ubuntu 26.04", about: "ext4, sudo-rs", host: vmDetails(ubuntu2604Platform),
			note: hostFQDN + " runs Ubuntu 26.04, as the VM of 2026-10-21 (vmtest.md §8.5): kernel 7.0.0-38-generic, sudo-rs 0.2.13. lslogins is in util-linux-extra there, and answers as on Debian 13 (vm-runs/s1/02-login.log)."},
		{name: "alpine323", label: "Alpine 3.23", about: "ext4, doas, BusyBox", host: vmDetails(alpine323Platform),
			note: hostFQDN + " runs Alpine 3.23.6, as the 2026-10-21 VM (vmtest.md §8.6): kernel 6.18.53-0-virt, doas. It has no lslogins or lastlog, /var/log/wtmp and /run/utmp stay at 0 bytes after a terminal login, and BusyBox's last -n 1 alice prints its usage and fails; sshd's own lines go to /var/log/messages, 0640 root wheel (vm-runs/s1/01-tools.log, 08-lslogins.log, 09-lslogins-blank.log). So one row says so."},
		{name: "pve9", label: "Proxmox VE 9", about: "ZFS, root alone", host: pveDetails,
			note: hostFQDN + " is a Proxmox VE 9 node on ZFS [I, no VM]: root's dataset read from /proc/self/mounts, rpool/ROOT/pve-1, where origin/main asks zfs for its hard-coded zroot/ROOT/os (details.go:85; menu/system_details_zfs_other_dataset.golden). No user but root. lslogins by inference from Debian 13, its base [I, no VM]. The CPU's name is cut at its row."},
		{name: "zfs-degraded", label: "ZFS degraded", about: "Pool not ONLINE", host: pveDegraded,
			note: "As pve9, its pool DEGRADED. origin/main's zpool status -x reads \"is healthy\" alone, and otherwise falls through to the root partition's figures, ZFS not mentioned (collectors.go:384-432; menu/system_details_zfs_all_healthy.golden)."},
		{name: "unreadable", label: "One section unreadable", about: "Memory", host: unreadableDetails,
			note: other + "/proc/meminfo cannot be read. origin/main's GenerateSystemStatus stops at the first collector's error and the whole report is lost (details.go:91-117); here the rest stays and the section says which."},
		{name: "no-logins", label: "No logins", about: "Nothing recorded", host: noLoginDetails,
			note: other + "lslogins answers blanks for root and alice, its form for an account with no login (vm-runs/s1/02-login.log)."},
		{name: "login-console", label: "A console login", about: "No remote host", host: consoleDetails,
			note: other + "alice logged in at a console, so lslogins' line has no host [I: the VMs ran it with a host]."},
		{name: "many", label: "Many users and addresses", about: "14 users, 9 addresses", host: manyDetails,
			note: other + "14 users and 9 addresses, IPv6 among them: the origin/main report lists IPv4 alone (os_host_info_repository.go:161, collectors.go:186). Last Login takes a row for root and one for each user."},
	}
}

// findSDHost returns the host named name, after sdPrefix.
func findSDHost(name string) (sdHost, bool) {
	i := slices.IndexFunc(sdHosts(), func(h sdHost) bool { return h.name == name })
	if i < 0 {
		return sdHost{}, false
	}
	return sdHosts()[i], true
}

// detailsModel is what a System Details host answers beyond statusOn's
// model, each command's output as details.Read reads it. The forms are those the
// VMs printed (vm-runs/s1/01-tools.log, 02-login.log), but zpool's and zfs's,
// written from their manuals, no VM having ZFS, and /proc/loadavg, inet6 and df's
// header, which no VM printed.
type detailsModel struct {
	addrs      string            // ip -o addr show scope global
	df         string            // df -kP /
	pool       string            // zpool list -H -o health <pool>, for a pool
	zfs        string            // zfs get -Hp -o value used,available <dataset>
	dataset    string            // the dataset zfs is asked about
	logins     map[string]string // lslogins' line for a name with a login; any other it knows answers noLoginLine
	accounts   []string          // the names lslogins knows, withDetails' from /etc/passwd
	noLslogins bool              // no lslogins, as host.Real says it
	memInfo    string            // /proc/meminfo; "" is memInfo
	noMemInfo  bool              // /proc/meminfo cannot be read
	noWtmp     bool              // /var/log/wtmp is not there, so no source holds records
}

// lsloginsArgs is how the reader asks lslogins about names, which follow -l,
// joined by commas: a raw line each, each blank field empty, no line for a name
// it does not know, exit 0 (vm-runs/s1/02-login.log, 08-lslogins.log).
var lsloginsArgs = []string{"--time-format=iso", "--noheadings", "--raw", "--output=USER,LAST-LOGIN,LAST-TTY,LAST-HOSTNAME", "-l"}

// answer answers the reader's commands.
func (m detailsModel) answer(argv []string) (hostfake.Response, bool) {
	switch cmd := strings.Join(argv, " "); {
	case cmd == "ip -o addr show scope global":
		return hostfake.Response{Stdout: m.addrs}, true
	case cmd == "df -kP /":
		return hostfake.Response{Stdout: m.df}, true
	case len(argv) == 6 && argv[0] == "zpool" && m.pool != "":
		return hostfake.Response{Stdout: m.pool + "\n"}, true
	case m.dataset != "" && cmd == "zfs get -Hp -o value used,available "+m.dataset:
		return hostfake.Response{Stdout: m.zfs}, true
	case argv[0] == "lslogins" && m.noLslogins:
		return hostfake.Response{Err: fmt.Errorf("host: %q: not in %s: %w", "lslogins", host.SearchPath, host.ErrNotFound)}, true
	case len(argv) == len(lsloginsArgs)+2 && argv[0] == "lslogins" && slices.Equal(argv[1:len(argv)-1], lsloginsArgs):
		var b strings.Builder
		for _, name := range strings.Split(argv[len(argv)-1], ",") {
			if line, ok := m.logins[name]; ok {
				b.WriteString(line + "\n")
			} else if slices.Contains(m.accounts, name) {
				b.WriteString(noLoginLine(name) + "\n")
			}
		}
		return hostfake.Response{Stdout: b.String()}, true
	}
	return hostfake.Response{}, false
}

// noLoginLine is lslogins' line for an account with no login: its name and
// three spaces (vm-runs/s1/02-login.log, root and nobody).
func noLoginLine(name string) string { return name + "   " }

// sessionLine is lslogins' line for name's login session (the form of
// vm-runs/s1/02-login.log; the time and address synthetic).
func sessionLine(name string) string {
	return name + " 2026-10-20T11:42:05+00:00 pts/0 192.0.2.10"
}

// ipLine is one line of ip -o addr show, iproute2's form as the Debian 13 and
// Ubuntu 26.04 VMs print it (vm-runs/s1/01-tools.log). inet6's, with no brd or
// label, is [I]: no VM has a global IPv6 address.
func ipLine(index int, iface, family, addr string) string {
	if family == "inet6" {
		return fmt.Sprintf("%d: %s    inet6 %s scope global \\       valid_lft 7104sec preferred_lft 7104sec\n", index, iface, addr)
	}
	return fmt.Sprintf("%d: %s    %s %s metric 100 brd %s scope global dynamic %s\\       valid_lft 7104sec preferred_lft 7104sec\n", index, iface, family, addr, broadcast(addr), iface)
}

// alpineIPLine is the same as the Alpine 3.23 VM prints it, noprefixroute and
// no metric (vm-runs/s1/01-tools.log).
func alpineIPLine(index int, iface, addr string) string {
	return fmt.Sprintf("%d: %s    inet %s brd %s scope global dynamic noprefixroute %s\\       valid_lft 5235sec preferred_lft 4411sec\n", index, iface, addr, broadcast(addr), iface)
}

// broadcast is the .255 of an IPv4 address, which is all a fixture needs.
func broadcast(addr string) string {
	return addr[:strings.LastIndexByte(addr, '.')] + ".255"
}

// cpuinfo is /proc/cpuinfo for n logical CPUs on sockets sockets, the
// lines the reader reads.
func cpuinfo(model string, n, sockets int) string {
	var b strings.Builder
	for i := range n {
		fmt.Fprintf(&b, "processor\t: %d\nmodel name\t: %s\nphysical id\t: %d\n\n", i, model, i%sockets)
	}
	return b.String()
}

// The reader's files and the standard outputs, as the kernel and df
// print them; the figures are synthetic. A wtmp record is 384 bytes on amd64
// (vm-runs/s1/05-wtmp-*.tail), which the reader does not parse: it asks only that
// the file hold a byte other than NUL, so this is a record's start. As root, the
// status file's Uid line says 0 (proc_pid_status(5); vm-runs/s1/10-forms.log).
const (
	wtmpRecord = "\x06\x00\x00\x00\x10\x02\x00\x00LOGIN\x00"
	rootStatus = "Name:\thardn\nUid:\t0\t0\t0\t0\nGid:\t0\t0\t0\t0\n"
	userStatus = "Name:\thardn\nUid:\t1000\t1000\t1000\t1000\nGid:\t1000\t1000\t1000\t1000\n"
	loadAvg    = "0.12 0.08 0.03 1/123 4567\n"
	memInfo    = "MemTotal:        4030544 kB\nMemFree:          812344 kB\nMemAvailable:    2801234 kB\nBuffers:           91234 kB\n"
	dfRoot     = "Filesystem     1024-blocks    Used Available Capacity Mounted on\n/dev/vda1         40470732 4412345  36058387      11% /\n"
)

// The kernels of the VM runs: vmtest.md §8.5 and §8.6.
const (
	debianVMKernel = "6.12.113+deb13-cloud-amd64"
	ubuntuVMKernel = "7.0.0-38-generic"
	alpineVMKernel = "6.18.53-0-virt"
)

// withDetails adds to h, a status host, what the reader reads of it: the
// files and m's commands, its Handler answering them first. Where lslogins is
// there, wtmp holds records, as on the VMs (vm-runs/s1/01-tools.log:30, :96).
func withDetails(h *fakeHost, m detailsModel, cpu string, cores, sockets int) *fakeHost {
	h.f.AddFile(details.CPUInfoFile, []byte(cpuinfo(cpu, cores, sockets)), 0o444, 0, 0)
	h.f.AddFile(details.LoadAvgFile, []byte(loadAvg), 0o444, 0, 0)
	h.f.AddFile(details.SelfStatusFile, []byte(rootStatus), 0o444, 0, 0)
	if !m.noMemInfo {
		mem := memInfo
		if m.memInfo != "" {
			mem = m.memInfo
		}
		h.f.AddFile(details.MemInfoFile, []byte(mem), 0o444, 0, 0)
	}
	if !m.noLslogins && !m.noWtmp {
		h.f.AddFile(details.WtmpFile, []byte(wtmpRecord), 0o664, 0, 43)
	}
	for _, line := range strings.Split(fileOn(h, "/etc/passwd"), "\n") {
		if name, _, ok := strings.Cut(line, ":"); ok {
			m.accounts = append(m.accounts, name)
		}
	}
	run := h.f.Handler
	h.f.Handler = func(argv []string, stdin []byte) hostfake.Response {
		if r, ok := m.answer(argv); ok {
			return r
		}
		return run(argv, stdin)
	}
	return h
}

// vmModel is a VM's model: eth0 at 192.0.2.20, the root file system on
// /dev/vda1 (simulate's mounts; synthetic, the VMs' being /dev/sda1 and
// /dev/sda), alice's session from the runner and no login for root.
func vmModel() detailsModel {
	return detailsModel{addrs: ipLine(2, "eth0", "inet", "192.0.2.20/24"), df: dfRoot, logins: map[string]string{"alice": sessionLine("alice")}}
}

// vmDetails returns a function making a host of platform p as the VM runs
// show it: its kernel, vmModel, on Alpine its ip's form and no lslogins, and
// 4 CPUs (synthetic: the VMs have 2).
func vmDetails(p platform) func() *fakeHost {
	return func() *fakeHost {
		m := vmModel()
		if p.openRC {
			m.noLslogins = true
			m.addrs = alpineIPLine(2, "eth0", "192.0.2.20/24")
		}
		return withDetails(onVMKernel(p), m, vmCPU, 4, 1)
	}
}

// consoleDetails is Debian 13 with alice's login at a console.
func consoleDetails() *fakeHost {
	m := vmModel()
	m.logins["alice"] = "alice 2026-10-20T11:42:05+00:00 tty1 "
	return withDetails(onVMKernel(debian13Platform), m, vmCPU, 4, 1)
}

// onVMKernel is a status host of platform p on the kernel the VM run of its
// release found.
func onVMKernel(p platform) *fakeHost {
	h := statusOn(p)
	kernel := debianVMKernel
	switch {
	case p.openRC:
		kernel = alpineVMKernel
	case p.socket:
		kernel = ubuntuVMKernel
	}
	h.f.AddFile(probe.KernelReleaseFile, []byte(kernel+"\n"), 0o444, 0, 0)
	return h
}

// pveDataset is the dataset a Proxmox VE install on ZFS mounts on /
// [I: the installer's name, not run here]; origin/main hard-codes
// zroot/ROOT/os (details.go:85).
const pveDataset = "rpool/ROOT/pve-1"

// pveDetails is a Proxmox VE 9 node with root on ZFS, its pool ONLINE, up
// 41 days.
func pveDetails() *fakeHost { return pveOn("ONLINE") }

// pveDegraded is pveDetails with its pool DEGRADED.
func pveDegraded() *fakeHost { return pveOn("DEGRADED") }

// pveOn is a Proxmox VE 9 node with root on ZFS, its pool health as given:
// 48 CPUs on 2 sockets, vmbr0, root's session from the runner.
func pveOn(health string) *fakeHost {
	h := pveNode()
	h.f.AddFile(detect.SelfMounts, []byte("proc /proc proc rw,nosuid,nodev,noexec,relatime 0 0\n"+pveDataset+" / zfs rw,relatime,xattr,noacl 0 0\n"+
		"/dev/fuse /etc/pve fuse rw,nosuid,nodev,relatime,user_id=0,group_id=0,default_permissions,allow_other 0 0\n"), 0o444, 0, 0)
	return withDetails(h, detailsModel{addrs: ipLine(4, "vmbr0", "inet", "192.0.2.20/24"), pool: health, dataset: pveDataset,
		zfs: "53687091200\n429496729600\n", memInfo: "MemTotal:       131072000 kB\nMemFree:        81234000 kB\nMemAvailable:   96123000 kB\n", logins: map[string]string{"root": sessionLine("root")}}, serverCPU, 48, 2)
}

// unreadableDetails is Debian 13 whose /proc/meminfo cannot be read.
func unreadableDetails() *fakeHost {
	m := vmModel()
	m.noMemInfo = true
	return withDetails(onVMKernel(debian13Platform), m, vmCPU, 4, 1)
}

// noLoginDetails is Debian 13 where lslogins finds nothing for anyone.
func noLoginDetails() *fakeHost {
	m := vmModel()
	m.logins = nil
	return withDetails(onVMKernel(debian13Platform), m, vmCPU, 4, 1)
}

// manyDetails is Debian 13 with 14 users and 9 addresses, in documentation
// ranges, two of the users having logged in.
func manyDetails() *fakeHost {
	h := onVMKernel(debian13Platform)
	passwd, group := fileOn(h, "/etc/passwd"), fileOn(h, "/etc/group")
	var sudo []string
	for i, u := range []string{"bob", "carol", "dave", "erin", "frank", "grace", "heidi", "ivan", "judy", "mallory", "niaj", "olivia", "peggy"} {
		uid := 1001 + i
		passwd += fmt.Sprintf("%s:x:%d:%d:%s:/home/%s:/bin/bash\n", u, uid, uid, u, u)
		group += fmt.Sprintf("%s:x:%d:\n", u, uid)
		if i < 4 {
			sudo = append(sudo, u)
		}
	}
	group = strings.Replace(group, "sudo:x:27:alice", "sudo:x:27:alice,"+strings.Join(sudo, ","), 1)
	h.f.AddFile("/etc/passwd", []byte(passwd), 0o644, 0, 0)
	h.f.AddFile("/etc/group", []byte(group), 0o644, 0, 0)
	m := vmModel()
	m.logins["bob"] = "bob 2026-10-19T08:01:22+00:00 pts/1 192.0.2.11"
	m.addrs = ipLine(2, "eth0", "inet", "192.0.2.20/24") + ipLine(2, "eth0", "inet", "192.0.2.21/24") + ipLine(2, "eth0", "inet6", "2001:db8:0:1::20/64") +
		ipLine(3, "eth1", "inet", "198.51.100.20/24") + ipLine(3, "eth1", "inet6", "2001:db8:0:2::20/64") + ipLine(4, "bond0", "inet", "203.0.113.20/24") +
		ipLine(5, "wg0", "inet", "198.18.0.20/32") + ipLine(5, "wg0", "inet6", "2001:db8:0:3::20/128") + ipLine(6, "docker0", "inet", "198.19.0.1/16")
	return withDetails(h, m, vmCPU, 4, 1)
}

// fileOn is the file at name on h, "" when absent.
func fileOn(h *fakeHost, name string) string {
	e, _ := h.f.Entry(name)
	return string(e.Data)
}

// detailsReading is what details.Read read, and the facts it read it on, which
// the document's host object takes.
type detailsReading struct {
	details.Report
	facts detect.Facts
}

// readDetails reads sys with the real reader, on detect's facts, as unit (d)'s
// command and unit (c)'s screen will. It changes nothing, and what it cannot read
// the sections say.
func readDetails(ctx context.Context, sys host.System) detailsReading {
	facts, _ := detect.Detect(ctx, details.Bounded(sys)) // what detect does not establish, the report says not read
	return detailsReading{Report: details.Read(ctx, sys, facts), facts: facts}
}

// loginRow is a login's row in the section. When is the year in place of the
// clock, as ls does, where it was not this year, galleryNow's on these hosts.
func loginRow(l details.Login) component.KeyValue {
	when := l.At.Format("Jan 2 15:04")
	if l.At.Year() != galleryNow.Year() {
		when = l.At.Format("Jan 2 2006")
	}
	switch {
	case l.State == details.LoginUnknown:
		return component.KeyValue{Key: l.User, Value: notKnown, State: component.ValueUnknown}
	case l.State == details.LoginNone:
		return component.KeyValue{Key: l.User, Value: noLogin, State: component.ValueNone}
	case l.From == "":
		return component.KeyValue{Key: l.User, Value: fmt.Sprintf(onTTY, when, l.TTY)}
	}
	return component.KeyValue{Key: l.User, Value: fmt.Sprintf(fromHost, when, l.From)}
}

// gib is n bytes in GiB, one decimal, as origin/main's memory says GiB and
// its disk, in the same unit, says GB (collectors.go:370, :430).
func gib(n uint64) string {
	return strconv.FormatFloat(float64(n)/(1<<30), 'f', 1, 64)
}

// usedOf says how much of total is used: "4.2 of 38.6 GiB (11%)".
func usedOf(used, total uint64) string {
	return fmt.Sprintf("%s of %s GiB (%s)", gib(used), gib(total), percent(used, total))
}

// percent is used of total as a whole percentage.
func percent(used, total uint64) string {
	if total == 0 {
		return "0%"
	}
	return fmt.Sprintf("%d%%", (used*100+total/2)/total)
}

// notKnownRow is a row whose fact is not established, though read.
func notKnownRow(key string) component.KeyValue {
	return component.KeyValue{Key: key, Value: notKnown, State: component.ValueUnknown}
}

// cantReadRows are the rows that say what of a section was not read.
func cantReadRows(unread []details.Unread) []component.KeyValue {
	var rows []component.KeyValue
	for _, u := range unread {
		rows = append(rows, component.KeyValue{Key: fmt.Sprintf(cantRead, u.Source), State: component.ValueWarn})
	}
	return rows
}

// sectionRows are section's rows: each fact read, then a row for each thing that
// could not be read. A fact keeps its section's others.
func (r detailsReading) sectionRows(section string) []component.KeyValue {
	switch section {
	case secOS:
		return r.osRows()
	case secHost:
		return r.hostRows()
	case secUsers:
		return r.userRows()
	case secCPU:
		return r.cpuRows()
	case secDisk:
		return r.diskRows()
	case secMemory:
		return r.memoryRows()
	case secLogin:
		return r.loginRows()
	}
	return nil
}

// osWords is the distribution as the header's bar names it: "Debian 13 (Trixie)".
func (r detailsReading) osWords() string {
	head := output.HeaderViewOf("", probe.Header{OS: r.OS.OS}).Context
	name := strings.TrimSpace(head.Name + " " + head.Version)
	if head.Codename != "" {
		name += " (" + head.Codename + ")"
	}
	return name
}

func (r detailsReading) osRows() []component.KeyValue {
	var rows []component.KeyValue
	if name := r.osWords(); name != "" {
		rows = append(rows, component.KeyValue{Key: rowOS, Value: name})
	}
	if r.OS.Kernel != "" {
		rows = append(rows, component.KeyValue{Key: rowKernel, Value: r.OS.Kernel})
	}
	if r.OS.Uptime.Known {
		rows = append(rows, component.KeyValue{Key: rowUptime, Value: output.FormatUptime(r.OS.Uptime.Duration)})
	}
	return append(rows, cantReadRows(r.OS.Unread)...)
}

func (r detailsReading) hostRows() []component.KeyValue {
	var rows []component.KeyValue
	if r.Host.Hostname != "" {
		rows = append(rows, component.KeyValue{Key: rowHostname, Value: r.Host.Hostname})
	}
	for _, a := range r.Host.Addresses {
		rows = append(rows, component.KeyValue{Key: a.Interface, Value: a.Address})
	}
	if r.Host.Addresses != nil && len(r.Host.Addresses) == 0 {
		rows = append(rows, component.KeyValue{Key: noAddresses, State: component.ValueNone})
	}
	return append(rows, cantReadRows(r.Host.Unread)...)
}

// userRows are the Users section's rows: each user and what lets them administer,
// as the users check reads it. As a user other than root the rules refused are one
// row, naming each: where they are not read a user's row says Not known, never a
// group's guess.
func (r detailsReading) userRows() []component.KeyValue {
	if r.Users.List == nil {
		return cantReadRows(r.Users.Unread)
	}
	noTool := noSudo
	if r.Users.Tool == sudoers.ToolDoas.String() {
		noTool = noDoas
	}
	var rows []component.KeyValue
	for _, u := range r.Users.List {
		switch tool, t := u.Admin(); t {
		case detect.Yes:
			rows = append(rows, component.KeyValue{Key: u.Name, Value: tool})
		case detect.No:
			rows = append(rows, component.KeyValue{Key: u.Name, Value: noTool})
		default:
			rows = append(rows, notKnownRow(u.Name))
		}
	}
	if len(rows) == 0 {
		rows = []component.KeyValue{{Key: noUsers, State: component.ValueNone}}
	}
	unread := r.Users.Unread
	if refused := r.Users.NotRead(); r.Users.Root == detect.No && len(refused) > 0 {
		rows = append(rows, component.KeyValue{Key: fmt.Sprintf(notRootRow, strings.Join(refused, ", ")), State: component.ValueWarn})
		unread = slices.DeleteFunc(slices.Clone(unread), func(u details.Unread) bool { return slices.Contains(refused, u.Source) })
	}
	return append(rows, cantReadRows(unread)...)
}

// cpuRows are the CPU and Load section's rows: Model says Not known where a
// /proc/cpuinfo read has no model name, as on arm64 [I, not run].
func (r detailsReading) cpuRows() []component.KeyValue {
	var rows []component.KeyValue
	if c := r.CPU; c.CPUs > 0 {
		model := component.KeyValue{Key: rowModel, Value: c.Model}
		if c.Model == "" {
			model = notKnownRow(rowModel)
		}
		rows = append(rows, model, component.KeyValue{Key: rowCPUs, Value: cpusWords(c)})
	}
	if r.CPU.Load[0] != "" {
		rows = append(rows, component.KeyValue{Key: rowLoad, Value: strings.Join(r.CPU.Load[:], ", ")})
	}
	return append(rows, cantReadRows(r.CPU.Unread)...)
}

func (r detailsReading) diskRows() []component.KeyValue {
	var rows []component.KeyValue
	d := r.Disk
	if d.Source != "" {
		rows = append(rows, component.KeyValue{Key: rowRoot, Value: d.Source + " " + d.Type})
	}
	if d.Known {
		rows = append(rows, component.KeyValue{Key: rowUsed, Value: usedOf(d.Used, d.Total)})
	}
	if d.Pool != "" {
		pool := component.KeyValue{Key: fmt.Sprintf(rowPool, d.Pool), Value: d.Health, State: component.ValueWarn}
		switch d.Health {
		case "":
			pool = notKnownRow(pool.Key)
		case "ONLINE":
			pool.State = component.ValueOK
		}
		rows = append(rows, pool)
	}
	return append(rows, cantReadRows(d.Unread)...)
}

func (r detailsReading) memoryRows() []component.KeyValue {
	var rows []component.KeyValue
	if r.Memory.Total > 0 {
		if used, ok := r.Memory.Used(); ok {
			rows = append(rows, component.KeyValue{Key: rowUsed, Value: usedOf(used, r.Memory.Total)})
		} else {
			rows = append(rows, notKnownRow(rowUsed))
		}
	}
	return append(rows, cantReadRows(r.Memory.Unread)...)
}

// loginRows are the Last Login section's rows: one where no answer holds any, its
// words by why.
func (r detailsReading) loginRows() []component.KeyValue {
	switch r.Logins.State {
	case details.LoginsNoTool:
		return []component.KeyValue{{Key: rowLogins, Value: noLslogins, State: component.ValueUnknown}}
	case details.LoginsUnread:
		return cantReadRows(r.Logins.Unread)
	}
	var rows []component.KeyValue
	for _, l := range r.Logins.List {
		rows = append(rows, loginRow(l))
	}
	return rows
}

// cpusWords is "4, 1 socket", the CPUs alone where the sockets are not known.
func cpusWords(c details.CPU) string {
	switch c.Sockets {
	case 0:
		return strconv.Itoa(c.CPUs)
	case 1:
		return strconv.Itoa(c.CPUs) + ", 1 socket"
	}
	return fmt.Sprintf("%d, %d sockets", c.CPUs, c.Sockets)
}

// sdSections are the sections, in P-17's order.
var sdSections = []string{secOS, secHost, secUsers, secCPU, secDisk, secMemory, secLogin}

// sections are the report's sections.
func (r detailsReading) sections() []screen.Section {
	var out []screen.Section
	for _, s := range sdSections {
		out = append(out, r.section(s))
	}
	return out
}

// section is one section of the report.
func (r detailsReading) section(title string) screen.Section {
	return screen.Section{Header: component.SectionHeader{Title: title}, Rows: component.BulletList{Items: r.sectionRows(title)}}
}

// items are the screen's items: Refresh.
func (r detailsReading) items() []component.MenuItem {
	return []component.MenuItem{{Key: "1", Label: itemRefresh, Description: aboutRefresh}}
}

// subMenu is the screen.
func (r detailsReading) subMenu() screen.SubMenu {
	return screen.SubMenu{
		Title:    component.TitleBar{Title: sdTitle},
		Sections: r.sections(),
		Heading:  sdHeading,
		Menu:     component.Menu{Items: r.items(), Exit: component.MenuItem{Key: "0", Label: "Return"}},
		Prompt:   sdPrompt,
	}
}

// report is the report with no menu and no prompt: what hardn system-details
// prints.
func (r detailsReading) report(c component.Ctx) string {
	blocks := []string{component.TitleBar{Title: sdTitle}.Render(c)}
	for _, s := range r.sections() {
		blocks = append(blocks, s.Render(c))
	}
	return apart(blocks...)
}

// offTerminal is the render environment of output that is not a terminal:
// plain mode at the default width, whatever the listing's own cell.
func offTerminalCtx() component.Ctx {
	return component.NewCtx(term.Fixed(term.DefaultWidth, colorprofile.NoTTY, false, true))
}

// sdRun is one run of the System Details screen on a simulated host.
type sdRun struct {
	s     *session
	h     *fakeHost
	ticks int
}

// openSD returns the action that opens what name names: the hosts' menu
// for sdPrefix alone, the screen on one host for "system-details/<host>".
func (s *session) openSD(name string) func() error {
	if sh, ok := findSDHost(strings.TrimPrefix(name, sdPrefix+"/")); ok && name != sdPrefix {
		return func() error { return s.sdScreen(context.Background(), sh, sh.host()) }
	}
	return func() error {
		m := hostsMenu("System Details hosts", func(add func(label, about string, action func() error)) {
			for _, sh := range sdHosts() {
				add(sh.label, sh.about, func() error { return s.sdScreen(context.Background(), sh, sh.host()) })
			}
		})
		return s.p.Run(func() nav.Screen { return m.vm.Screen(s.p.Ctx, m.menu) })
	}
}

// boundHosts is a menu of hosts, drawn and bound.
type boundHosts struct {
	vm   screen.SubMenu
	menu nav.Menu
}

// hostsMenu is the sub-menu layout, an item per host fill adds, bound to
// its action, as the menus of the Environment and Backup hosts are.
func hostsMenu(title string, fill func(add func(label, about string, action func() error))) boundHosts {
	m := boundHosts{vm: screen.SubMenu{Title: component.TitleBar{Title: title}, Heading: "Select a host", Prompt: "Enter your choice"}}
	exit := component.MenuItem{Key: "0", Label: "Return"}
	m.menu.Exit = exit
	var items []component.MenuItem
	fill(func(label, about string, action func() error) {
		it := component.MenuItem{Key: strconv.Itoa(len(items) + 1), Label: label, Description: about}
		items = append(items, it)
		m.menu.Options = append(m.menu.Options, nav.Option{Item: it, Action: action})
	})
	m.vm.Menu = component.Menu{Items: items, Exit: exit}
	return m
}

// sdScreen runs the screen on h, sh's host, until the user goes back from
// it: its note above it, then each round the screen read again from the
// host, which is all that Refresh does. Nothing is written; the plan-only
// sequence draws the same screen, as Backup and Environment are under plan
// only.
func (s *session) sdScreen(ctx context.Context, sh sdHost, h *fakeHost) error {
	run := &sdRun{s: s, h: h}
	if err := s.writeNote(sdPrefix+"/"+sh.name, sh.note); err != nil {
		return err
	}
	return s.p.Run(func() nav.Screen { return run.draw(ctx) })
}

// draw reads the host and returns the screen, its one item bound.
func (run *sdRun) draw(ctx context.Context) nav.Screen {
	r := readDetails(ctx, run.h.sys())
	vm := r.subMenu()
	menu := nav.Menu{Exit: vm.Menu.Exit}
	for _, it := range r.items() {
		menu.Options = append(menu.Options, nav.Option{Item: it, Action: func() error { run.tick(); return nil }})
	}
	return vm.Screen(run.s.p.Ctx, menu)
}

// tick moves the simulated host on, as if a Refresh a minute later found it
// so: its uptime and load, so that the screen drawn again shows a change.
func (run *sdRun) tick() {
	run.ticks++
	f := run.h.f
	up := fmt.Sprintf("%d.37 553000.12\n", 277920+60*run.ticks)
	if e, ok := f.Entry(probe.UptimeFile); ok && string(e.Data) == upWeeks {
		up = fmt.Sprintf("%d.00 7000000.00\n", 3546000+60*run.ticks)
	}
	f.AddFile(probe.UptimeFile, []byte(up), 0o444, 0, 0)
	f.AddFile(details.LoadAvgFile, []byte(fmt.Sprintf("0.%02d 0.09 0.03 1/123 4567\n", 12+run.ticks*10)), 0o444, 0, 0)
}

// main()'s two lines as a user other than root: logging.go:40's and
// cmd/hardn/main.go:62's, on stdout today, the reasons the system's words
// [I, not run as a user other than root].
const (
	mainOpenLine    = "Failed to open log file /var/log/hardn.log: open /var/log/hardn.log: permission denied"
	mainExampleLine = "Warning: Unable to create example configuration file: failed to create directory for example config: mkdir /etc/hardn: permission denied"
)

// sdStart returns the hosts' menu, or the host, that args name
// ("system-details", "system-details/<host>"), and whether they name one;
// an error for a host that is not one.
func sdStart(args []string) (string, bool, error) {
	if len(args) != 1 || (args[0] != sdPrefix && !strings.HasPrefix(args[0], sdPrefix+"/")) {
		return "", false, nil
	}
	if name := strings.TrimPrefix(args[0], sdPrefix+"/"); args[0] != sdPrefix {
		if _, ok := findSDHost(name); !ok {
			var names []string
			for _, h := range sdHosts() {
				names = append(names, sdPrefix+"/"+h.name)
			}
			return "", false, fmt.Errorf("no system-details host %q; the hosts are %s", args[0], strings.Join(names, ", "))
		}
	}
	return args[0], true, nil
}

// sdNote is play's note for the System Details mocks.
const sdNote = "Play mode, mocks: the System Details screen of slice 1, inferred from origin/main (unreleased), read from a simulated host by the real reader (pkg/engine/details), " +
	"its rows and layout unit (c)'s to build. " +
	"It only reads: no plan, no y/N, no journal. Nothing on this machine changes: every command and file is the fake's."

// readSDHost reads the host named name, fresh, as the screen does.
func readSDHost(ctx context.Context, name string) (detailsReading, error) {
	sh, ok := findSDHost(name)
	if !ok {
		return detailsReading{}, fmt.Errorf("no system-details host %q", name)
	}
	return readDetails(ctx, sh.host().sys()), nil
}

// asUserSys is a host as a user other than root reads it, which only hardn
// system-details can (cmd/hardn/main.go:148, its Run's root check, which a
// subcommand's RunE does not pass through): the rules refuse them, /etc/sudoers.d
// and /etc/doas.d being drwxr-x--- and /etc/doas.conf 0640 on the VMs
// (vm-runs/s1/01-tools.log:65, :130, :191-193), and /etc/sudoers 0440
// (vm-runs/s1/10-forms.log).
type asUserSys struct{ host.System }

// refusesUser reports whether a user other than root is refused name.
func refusesUser(name string) bool {
	return slices.Contains([]string{sudoers.MainConfig, sudoers.DropInDir, sudoers.DoasConfig, sudoers.DoasDir}, name)
}

func (s asUserSys) refusal(name string) error {
	if refusesUser(name) {
		return &fs.PathError{Op: "open", Path: name, Err: fs.ErrPermission}
	}
	return nil
}

// ReadFile is the host's, but for what a user is refused.
func (s asUserSys) ReadFile(ctx context.Context, name string) ([]byte, error) {
	if err := s.refusal(name); err != nil {
		return nil, err
	}
	return s.System.ReadFile(ctx, name)
}

// ReadFileNoFollow is the host's, but for what a user is refused.
func (s asUserSys) ReadFileNoFollow(ctx context.Context, name string) ([]byte, fs.FileInfo, error) {
	if err := s.refusal(name); err != nil {
		return nil, nil, err
	}
	return s.System.ReadFileNoFollow(ctx, name)
}

// ReadDir is the host's, but for what a user is refused.
func (s asUserSys) ReadDir(ctx context.Context, name string) ([]fs.DirEntry, error) {
	if err := s.refusal(name); err != nil {
		return nil, err
	}
	return s.System.ReadDir(ctx, name)
}

// readAsUser reads a host of platform p as alice, not through sudo.
func readAsUser(ctx context.Context, p platform) detailsReading {
	h := vmDetails(p)()
	h.f.AddFile(details.SelfStatusFile, []byte(userStatus), 0o444, 0, 0)
	return readDetails(ctx, asUserSys{h.sys()})
}

// onSDHost returns a drawing of the host named name's reading.
func onSDHost(name string, view func(detailsReading, component.Ctx) string) func(context.Context, component.Ctx) (string, error) {
	return func(ctx context.Context, c component.Ctx) (string, error) {
		r, err := readSDHost(ctx, name)
		if err != nil {
			return "", err
		}
		return view(r, c), nil
	}
}

// sdMocks are the listing's mocks, in order: the screen on each host, plan
// only, the non-interactive forms, and origin/main's report in its own words.
func sdMocks() []listMock {
	var mocks []listMock
	for _, sh := range sdHosts() {
		mocks = append(mocks, listMock{name: "screen-" + sh.name, note: "The System Details screen. " + sh.note,
			draw: onSDHost(sh.name, func(r detailsReading, c component.Ctx) string { return r.subMenu().Render(c) })})
	}
	return append(mocks,
		listMock{name: "plan-only", note: "Plan only, as the other sub-menus show it: they show nothing. No sub-menu shows the mode (SubMenu.Heading, sub.go:58: \"with no mode beside it\"; play/backup-plan-only, play/environment-main-plan-only). The screen is the same under plan only, since it changes nothing, and the main menu says the mode.",
			draw: onSDHost("debian13", func(r detailsReading, c component.Ctx) string { return r.subMenu().Render(c) })},
		listMock{name: "cli-terminal", note: "hardn system-details to a terminal: the report, no menu and no prompt. It reads the same as the screen.",
			draw: onSDHost("debian13", func(r detailsReading, c component.Ctx) string { return r.report(c) })},
		listMock{name: "cli-pipe", wide: theme.ContentWidth, note: "hardn system-details not to a terminal: plain mode, no colour, ASCII, whatever the terminal around it (term.Env's Plain). Its values are cut at the 64-cell content width like any row's today; unit (c) must not cut them (the page, Made here; cli-pipe-cut). As a user other than root, see cli-not-root.",
			draw: onSDHost("debian13", func(r detailsReading, _ component.Ctx) string { return r.report(offTerminalCtx()) })},
		listMock{name: "cli-pipe-cut", wide: theme.ContentWidth, note: "The same on pve9: the CPU's name is 40 cells, its row holds 37, so the pipe loses its end, as a row cuts on a terminal. Unit (c) must not cut off a terminal: nothing in pkg/ui lays out uncapped today, so a new path through the components is a deviation to log.",
			draw: onSDHost("pve9", func(r detailsReading, _ component.Ctx) string { return r.report(offTerminalCtx()) })},
		listMock{name: "cli-not-root", note: "hardn system-details as alice, not through sudo: reachable, as the subcommand has no root check (cmd/hardn/main.go:84; the check at :148 is in the root command's Run). The main menu and -p are not. What it cannot read it says: /etc/sudoers.d is drwxr-x--- root root, so a user cannot list it; on a doas host /etc/doas.conf, 0640 root (vm-runs/s1/01-tools.log); /etc/sudoers is 0440 (vm-runs/s1/10-forms.log). So a user's row reads Not known, never a group's guess. Last Login reads the same, run (vm-runs/s1/02-login.log). The two lines above the report are main()'s, unrelated, on stdout today, so hardn system-details > file holds them (cmd/hardn/main.go:57-62; logging.go:40; read, not run); unit (d) moves them to stderr.",
			draw: func(ctx context.Context, c component.Ctx) (string, error) {
				return apart(strings.Join(codeRows(c, []string{mainOpenLine, mainExampleLine}, c.Theme.Output.Render), "\n"), readAsUser(ctx, debian13Platform).report(c)), nil
			}},
		listMock{name: "cli-json", wide: theme.ContentWidth, note: "Decision 1 (a): hardn system-details --output json, whose flag is unit (d)'s; the document is output.Build's. On pkg/output's document, not beside it: --output json is one flag with one contract (docs/reference/output.md), mode \"check\" already means \"read, change nothing\", host already holds the name and OS, and wire.go's SchemaVersion comment lets a field be added without a new version. So the nine fields, and one more, details, on this form alone; result nothing-to-do, exit 0. logins holds each account's answer. As a user other than root main()'s two lines (cli-not-root) reach stdout first until unit (d) moves them, and the output would not parse.",
			draw: onSDHost("debian13", jsonDrawing)},
		listMock{name: "cli-json-unreadable", wide: theme.ContentWidth, note: "As cli-json: a section not read is null in details and one finding, DETAILS_UNREADABLE, which is not an error: the rest was read, so the exit status stays 0.",
			draw: onSDHost("unreadable", jsonDrawing)},
		listMock{name: "cli-flags", note: "The five flags origin/main parses and ignores, -E/--export, -S/--section, -O/--format, --json, --yaml (pkg/cmd/system_details_cmd.go:38-42, the code using them commented out at :95-121). What they become is decision 1 (a): §3.11's system-details change alone is approved, narrowed; unit (d) builds it.",
			draw: func(_ context.Context, c component.Ctx) (string, error) { return flagsDrawing(c), nil }},
		listMock{name: "origin", own: true, note: "origin/main's report, its words as they are (display.go:43-129; pkg/characterization's menu/system_details_open.golden), on the design system, on the debian13 host: its rows, its items (system_details_menu.go:70-80), its heading and its prompt. Last Login is blank, as lastlog is not there (menu/system_details_no_lastlog.golden). Its 50-column box, its screen clear, its blank lines where sections would be, and its bar graphs are left out.",
			draw: onSDHost("debian13", func(r detailsReading, c component.Ctx) string { return r.originScreen(false).Render(c) })},
		listMock{name: "origin-error", own: true, note: "origin/main's report when any collector fails (system_details_menu.go:42-62; menu/system_details_error.golden): the error, twice, above the box and in it, the box saying to check permissions whatever it was, the report gone, Export not offered. The word \"Error:\" before each title here is the component's, not origin/main's.",
			draw: onSDHost("unreadable", func(r detailsReading, c component.Ctx) string {
				const why = "failed to collect memory info: failed to get memory info: exec: \"free\": executable file not found in $PATH"
				return apart(component.Message{Kind: component.MessageBad, Text: "Error retrieving system status: " + why}.Render(c),
					component.Callout{Kind: component.MessageBad, Title: "System Details Error", Body: []string{
						"Failed to retrieve system status information.", "Error: " + why,
						"Please check system permissions and try again.",
					}}.Render(c), r.originScreen(true).Render(c))
			})},
	)
}

// document is the reading as the JSON form decision 1 (a) adopted: pkg/output's
// document, built by output.Build.
func (r detailsReading) document() output.Document {
	facts, report := r.facts, r.Report
	return output.Build(output.Run{Mode: output.ModeCheck, HardnVersion: "0.4.0", Hostname: r.Host.Hostname, Facts: &facts, Details: &report})
}

// jsonDrawing is the JSON form as it prints: output.WriteJSON's.
func jsonDrawing(r detailsReading, _ component.Ctx) string {
	var b bytes.Buffer
	if err := output.WriteJSON(&b, r.document()); err != nil {
		return err.Error()
	}
	return strings.TrimRight(b.String(), "\n")
}

// flagsDrawing is the five flags, today and as decision 1 (a) has them.
func flagsDrawing(c component.Ctx) string {
	const warn = "--output json, with a warning"
	return apart(
		component.SectionHeader{Title: "Today"}.Render(c),
		component.BulletList{Items: []component.KeyValue{{Key: "All five", Value: "Parsed and ignored"}}}.Render(c),
		component.SectionHeader{Title: "Proposed, if 3.11 is approved"}.Render(c),
		component.BulletList{Items: []component.KeyValue{
			{Key: "--json, -O json", Value: warn},
			{Key: "--yaml, -O yaml", Value: "Refused, exit 2"},
			{Key: "-O text", Value: "Accepted, with a warning"},
			{Key: "-S, --section", Value: "Refused, exit 2"},
			{Key: "-E, --export", Value: "Refused: use > file"},
		}}.Render(c),
	)
}

// originScreen is origin/main's report in its words, on the design system.
// failed draws its items alone, as after a collector's error, Export not
// offered.
func (r detailsReading) originScreen(failed bool) screen.SubMenu {
	items := []component.MenuItem{{Key: "1", Label: "Refresh Information", Description: "Reload system status from system"}}
	vm := screen.SubMenu{Title: component.TitleBar{Title: sdTitle}, Heading: sdHeading, Prompt: sdPrompt}
	if !failed {
		items = append(items, component.MenuItem{Key: "2", Label: "Export Information", Description: "Export system status to file"})
		rows := []component.KeyValue{
			{Key: "OS", Value: "Debian 13 Trixie"}, {Key: "Kernel", Value: "Linux " + r.OS.Kernel}, {Key: "Hostname", Value: r.Host.Hostname},
			{Key: "IP Addresses", Value: "192.0.2.20"}, {Key: "Client IP", Value: "Not connected"}, {Key: "DNS IP 1", Value: "192.0.2.53"}, {Key: "User", Value: "root"},
			{Key: "Non-System Users", Value: "alice (sudo)"}, {Key: "Processor", Value: r.CPU.Model}, {Key: "Cores", Value: "4 vCPU(s) / 1 Socket(s)"},
			{Key: "Hypervisor", Value: "KVM"}, {Key: "CPU Freq", Value: "0.00 GHz"}, {Key: "Load 1m", Value: "(0.12)"}, {Key: "Load 5m", Value: "(0.08)"}, {Key: "Load 15m", Value: "(0.03)"},
			{Key: "Volume", Value: "6.17/38.60 GB [15.99%]"}, {Key: "Memory", Value: "3.07/3.84 GiB [79.84%]"},
			{Key: "Last Login"}, {Key: "Uptime", Value: output.FormatUptime(r.OS.Uptime.Duration)},
		}
		vm.Sections = []screen.Section{{Rows: component.BulletList{Items: rows}}}
	}
	vm.Menu = component.Menu{Items: items, Exit: component.MenuItem{Key: "0", Label: "Return"}}
	return vm
}

// listMock is one mock a listing shows: its name, its note, its drawing,
// and the width it needs, if more than any terminal's: its off-terminal
// forms do not draw to the listing's width.
type listMock struct {
	name, note string
	draw       func(ctx context.Context, c component.Ctx) (string, error)
	wide       int
	// own: its note names its source itself, as origin/main's own words do,
	// and takes no listing label.
	own bool
}

// listing is a mode that lists mocks with no input, as "status" and
// Environment's did: each under the gallery's header, "<prefix> <mock>",
// its note, then its drawing, a write each; -list prints their names. It
// sizes itself as play does (look.sizeTo).
type listing struct {
	prefix, usage string
	// label is the source label a mock's note opens with.
	label string
	mocks func() []listMock
}

// note is what m's note says, under the listing's label.
func (l listing) note(m listMock) string {
	if m.own {
		return m.note
	}
	return l.label + " " + m.note
}

// run is the listing's mode.
func (l listing) run(args []string, size sizer, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("uigallery "+l.prefix, flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() {
		fmt.Fprintln(stderr, l.usage)
		fs.PrintDefaults()
	}
	list := fs.Bool("list", false, "print the names of the mocks it would list, and exit")
	look := lookFlags(fs, true)
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return exitOK
		}
		return exitUsage
	}
	mocks, err := selectListMocks(l.mocks(), fs.Args())
	if err != nil {
		fmt.Fprintf(stderr, "uigallery: %s: %v\n", l.prefix, err)
		return exitUsage
	}
	if *list {
		for _, m := range mocks {
			fmt.Fprintln(stdout, m.name)
		}
		return exitOK
	}
	look.sizeTo(fs, size)
	c, err := look.cell()
	if err != nil {
		fmt.Fprintf(stderr, "uigallery: %s: %v\n", l.prefix, err)
		return exitUsage
	}
	g, ok := look.gallery(stderr)
	if !ok {
		return exitError
	}
	rc, err := g.ctx(c.env)
	if err == nil {
		w := c.env.Writer(stdout)
		for _, m := range mocks {
			var out string
			if out, err = m.draw(context.Background(), rc); err == nil {
				_, err = io.WriteString(w, apart(header(rc, l.prefix+" "+m.name),
					component.Callout{Kind: component.MessageInfo, Body: []string{l.note(m)}}.Render(rc), out)+sectionBreak)
			}
			if err != nil {
				err = fmt.Errorf("%s: %w", m.name, err)
				break
			}
		}
	}
	if err != nil {
		fmt.Fprintf(stderr, "uigallery: %s: %v\n", l.prefix, err)
		return exitError
	}
	return exitOK
}

// selectListMocks returns the mocks args name, in order: every mock for
// none or "all". An argument that starts with "-" is a flag the flag
// package left unparsed, after a name.
func selectListMocks(all []listMock, args []string) ([]listMock, error) {
	if len(args) == 0 || slices.Contains(args, "all") {
		return all, nil
	}
	for _, a := range args {
		switch found := slices.ContainsFunc(all, func(m listMock) bool { return m.name == a }); {
		case !found && strings.HasPrefix(a, "-"):
			return nil, fmt.Errorf("flag %s comes after a mock's name; flags go before them", a)
		case !found:
			return nil, fmt.Errorf("no mock %q; -list shows them", a)
		}
	}
	return slices.DeleteFunc(all, func(m listMock) bool { return !slices.Contains(args, m.name) }), nil
}

// sdUsage is the listing's usage line.
const sdUsage = "usage: go run ./internal/tools/uigallery system-details [flags] [mock | all]..."

// sdListing is the System Details listing.
var sdListing = listing{prefix: sdPrefix, usage: sdUsage, label: sdLabel, mocks: sdMocks}
