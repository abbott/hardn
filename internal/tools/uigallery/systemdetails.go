package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"slices"
	"strconv"
	"strings"

	"github.com/charmbracelet/colorprofile"

	probe "github.com/abbott/hardn/pkg/engine/header"
	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/accounts"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/hostfake"
	"github.com/abbott/hardn/pkg/output"
	"github.com/abbott/hardn/pkg/ui/component"
	"github.com/abbott/hardn/pkg/ui/nav"
	"github.com/abbott/hardn/pkg/ui/screen"
	"github.com/abbott/hardn/pkg/ui/term"
	"github.com/abbott/hardn/pkg/ui/theme"
)

// The System Details mocks (refactor plan slice 1, unit (a), next/s1-gallery):
// a proposal of the System Details screen, main menu 9, for the
// maintainer's review, nothing of it wired into hardn. There is no Figma
// frame and it is not in v0.3.2: it is inferred from origin/main
// (pkg/system/display.go:43-129, pkg/menu/system_details_menu.go), with
// P-17's sections, the 2025 prototype's as reference only.
//
// It only reads, so it has no plan, no y/N and no journal: its sequence is
// the reading, opening, each state, refreshing and leaving.
//
// Real code reads what it can of a simulated host: detect's facts, the
// header probe's OS, hostname and uptime, accounts.Read's users. The rest
// is the gallery's, each a stand-in for unit (b)'s work, labelled so here
// and in the page: readDetails itself, its CPU, load, memory, disk, ZFS,
// address and last-login readers, and detailsModel, what the simulated
// host answers them. Which commands unit (b) reads them with on the
// releases is [unverified]: none has run on a VM.
//
// "system-details" lists the mocks with no input; "play system-details" is
// the menu of the hosts and "play system-details/<host>" the screen on one.

// sdPrefix names the mocks: "system-details <mock>" in the listing,
// "system-details" and "system-details/<host>" in play.
const sdPrefix = "system-details"

// sdMainHost is the host the main menu's 9 leads to.
const sdMainHost = sdPrefix + "/debian13"

// sdLinks lead from a menu mock's item to the System Details screen on a
// host, by the mock's name and the item's key, beside links: the main
// menu's 9, which said the gallery had no screen for it until this unit.
var sdLinks = map[string]string{mainMenuMock + "/9": sdMainHost}

// The screen's words: the main menu's label for the title, Backup's and
// Environment's decided heading and prompt (screen-backup.md §5, P-29 (a));
// P-17's sections; the rest proposed.
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
	rowCores    = "Cores"
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
	// notRootRow is said under Users to a user other than root, who cannot
	// read the rules the group names do not show.
	notRootRow = "Not root: %s not read"
	// Items.
	itemRefresh  = "Refresh"
	aboutRefresh = "Read the host again"
	itemExport   = "Export"
	aboutCommand = "Say how to save it"
	aboutFile    = "Write it to a file"
	itemOpen     = "Open"
	aboutOpen    = "The whole report in %s"
	// Treatment export-command's words, and export-file's.
	saveCommand = "To save it, run: hardn system-details > report.txt"
	askPath     = "Path to write"
	wholePath   = "Use a full path, such as /root/system-details.txt."
	savedTo     = "Written to /root/system-details.txt, 0600."
	planOnlyNo  = "Not available in plan only."
	// Treatment report-pager's section.
	secSummary = "Summary"
)

// The treatments the maintainer chooses between (gallery-system-details.md).
// How the report fits: each host draws report-whole unless it names
// another.
const (
	reportWhole    = "report-whole"    // the report whole, its top scrolling off a shorter terminal, as the main menu's
	reportSections = "report-sections" // one line per section; an item opens its rows
	reportPager    = "report-pager"    // one line per section; an item opens the whole report in the pager
	reportWide     = "report-wide"     // two columns from wideCols up; report-whole below
)

// The treatments of Export: each host offers none unless it names another.
const (
	exportNone    = "export-none"    // no item: hardn system-details > file, which writes nothing itself
	exportCommand = "export-command" // an item that says the command
	exportFile    = "export-file"    // an item that asks a full path and writes the file, a change as root
)

// wideCols is the width report-wide takes two columns from: two content
// boxes and the gap between them (theme.ContentWidth, sideGap).
const wideCols = 2*theme.ContentWidth + sideGap

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
	// env is hardn's environment there: who ran it.
	env map[string]string
	// notRoot: hardn runs as a user other than root, which only
	// hardn system-details can (cmd/hardn/main.go:148, its Run's root check,
	// which a subcommand's RunE does not pass through).
	notRoot bool
	// report and export are the treatments it draws; "" is the proposed.
	report, export string
}

// The simulated hosts' CPUs, as /proc/cpuinfo names them: a VM's, and on
// the Proxmox VE node a server's (synthetic, its length real: origin/main
// shows it whole in a 50-column box, display.go:32, which wraps [I]).
const (
	vmCPU     = "QEMU Virtual CPU version 2.5+"
	serverCPU = "Intel(R) Xeon(R) Gold 6338 CPU @ 2.00GHz"
)

// sdHosts are the hosts, in the menu's order: the four releases, the
// states, then the treatments on Debian 13.
func sdHosts() []sdHost {
	const (
		deb   = hostFQDN + " runs Debian 13, as the VM of 2026-10-21 (vmtest.md §8.5): kernel 6.12.113+deb13-cloud-amd64, sudo. alice ran sudo hardn. "
		other = "Debian 13, as debian13. "
	)
	return []sdHost{
		{name: "debian13", label: "Debian 13", about: "ext4, sudo", host: details(debian13Platform), env: sudoAlice,
			note: deb + "Treatment report-whole, export-none: the report, Refresh and Return."},
		{name: "ubuntu2604", label: "Ubuntu 26.04", about: "ext4, sudo-rs", host: details(ubuntu2604Platform), env: sudoAlice,
			note: hostFQDN + " runs Ubuntu 26.04, as the VM of 2026-10-21 (vmtest.md §8.5): kernel 7.0.0-38-generic, sudo-rs 0.2.13. alice ran sudo hardn."},
		{name: "alpine323", label: "Alpine 3.23", about: "ext4, doas, BusyBox", host: details(alpine323Platform), env: doasAlice,
			note: hostFQDN + " runs Alpine 3.23.6, as the 2026-10-21 VM (vmtest.md §8.6): kernel 6.18.53-0-virt, doas, BusyBox's ip, df and last, whose forms are [unverified]. alice ran doas hardn."},
		{name: "pve9", label: "Proxmox VE 9", about: "ZFS, root alone", host: pveDetails, env: rootLogin,
			note: hostFQDN + " is a Proxmox VE 9 node on ZFS [I, no VM]: root's dataset read from /proc/self/mounts, rpool/ROOT/pve-1, where origin/main asks zfs for its hard-coded zroot/ROOT/os (details.go:85). No user but root; root logged in. The CPU's name is cut at its row."},
		{name: "zfs-degraded", label: "ZFS degraded", about: "Pool not ONLINE", host: pveDegraded, env: rootLogin,
			note: "As pve9, its pool DEGRADED. origin/main's zpool status -x reads \"is healthy\" alone, and otherwise falls through to the root partition's figures, ZFS not mentioned (collectors.go:384-432)."},
		{name: "unreadable", label: "One section unreadable", about: "Memory", host: unreadableDetails, env: sudoAlice,
			note: other + "/proc/meminfo cannot be read. origin/main's GenerateSystemStatus stops at the first collector's error and the whole report is lost (details.go:91-117); here the rest stays and the section says which."},
		{name: "no-last-login", label: "No last login", about: "Nothing recorded", host: noLoginDetails, env: sudoAlice,
			note: other + "last finds no login for alice."},
		{name: "many", label: "Many users and addresses", about: "14 users, 9 addresses", host: manyDetails, env: sudoAlice,
			note: other + "14 users and 9 addresses, IPv6 among them: the origin/main report lists IPv4 alone (os_host_info_repository.go:161, collectors.go:186)."},
		{name: reportSections, label: "Treatment: sections", about: "A line each, items open them", host: details(debian13Platform), env: sudoAlice, report: reportSections,
			note: other + "Treatment report-sections: one line per section, an item opening its rows. It fits 80x24."},
		{name: reportPager, label: "Treatment: pager", about: "A line each, the rest in less", host: details(debian13Platform), env: sudoAlice, report: reportPager,
			note: other + "Treatment report-pager: one line per section, an item opening the whole report in the pager. It fits 80x24. The gallery runs no pager: the item says what it would run."},
		{name: exportCommand, label: "Treatment: Export says how", about: "No file written", host: details(debian13Platform), env: sudoAlice, export: exportCommand,
			note: other + "Treatment export-command: Export says the command."},
		{name: exportFile, label: "Treatment: Export writes", about: "A file, as root", host: details(debian13Platform), env: sudoAlice, export: exportFile,
			note: other + "Treatment export-file: Export asks a full path and writes the file, as root: a change on the host, so not read only. The gallery writes nothing."},
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
// model, each command's output as the stand-in reads it: gallery-local, as
// readDetails is. The forms are written from iproute2's, coreutils', zfs's
// and util-linux's; on Alpine BusyBox's are [unverified], and so is every
// command's presence on a release (unit (b), on the VMs).
type detailsModel struct {
	addrs      string // ip -o addr show scope global
	df         string // df -kP /
	pool       string // zpool list -H -o health <pool>, for a pool
	zfs        string // zfs get -Hp -o value used,available <dataset>
	dataset    string // the dataset zfs is asked about
	lastOutput string // last -n 1 <user>
	memInfo    string // /proc/meminfo; "" is memInfo
	noMemInfo  bool   // /proc/meminfo cannot be read
}

// answer answers the stand-in's commands.
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
	case argv[0] == "last":
		return hostfake.Response{Stdout: m.lastOutput}, true
	}
	return hostfake.Response{}, false
}

// ipLine is one line of ip -o addr show: iproute2's form.
func ipLine(index int, iface, family, addr string) string {
	return fmt.Sprintf("%d: %s    %s %s scope global %s\\       valid_lft forever preferred_lft forever\n", index, iface, family, addr, iface)
}

// cpuinfo is /proc/cpuinfo for n logical CPUs on sockets sockets, the
// lines the stand-in reads.
func cpuinfo(model string, n, sockets int) string {
	var b strings.Builder
	for i := range n {
		fmt.Fprintf(&b, "processor\t: %d\nmodel name\t: %s\nphysical id\t: %d\n\n", i, model, i%sockets)
	}
	return b.String()
}

// The stand-in's files and the standard outputs, as the kernel and df
// print them; the figures are synthetic.
const (
	loadAvg     = "0.12 0.08 0.03 1/123 4567\n"
	memInfo     = "MemTotal:        4030544 kB\nMemFree:          812344 kB\nMemAvailable:    2801234 kB\nBuffers:           91234 kB\n"
	dfRoot      = "Filesystem     1024-blocks    Used Available Capacity Mounted on\n/dev/vda1         40470732 4412345  36058387      11% /\n"
	lastSession = "alice    pts/0        192.0.2.10       Tue Oct 20 11:42   still logged in\n\nwtmp begins Mon Oct 19 08:01:22 2026\n"
)

// The kernels of the VM runs: vmtest.md §8.5 and §8.6.
const (
	debianVMKernel = "6.12.113+deb13-cloud-amd64"
	ubuntuVMKernel = "7.0.0-38-generic"
	alpineVMKernel = "6.18.53-0-virt"
)

// withDetails adds to h, a status host, what the stand-in reads of it: the
// files and m's commands, its Handler answering them first.
func withDetails(h *fakeHost, m detailsModel, cpu string, cores, sockets int) *fakeHost {
	h.f.AddFile("/proc/cpuinfo", []byte(cpuinfo(cpu, cores, sockets)), 0o444, 0, 0)
	h.f.AddFile("/proc/loadavg", []byte(loadAvg), 0o444, 0, 0)
	if !m.noMemInfo {
		mem := memInfo
		if m.memInfo != "" {
			mem = m.memInfo
		}
		h.f.AddFile("/proc/meminfo", []byte(mem), 0o444, 0, 0)
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
// /dev/vda1 (simulate's mounts), alice's session from the runner.
func vmModel() detailsModel {
	return detailsModel{addrs: ipLine(2, "eth0", "inet", "192.0.2.20/24"), df: dfRoot, lastOutput: lastSession}
}

// details returns a function making a host of platform p as the VM runs
// show it: its kernel, 4 cores, vmModel.
func details(p platform) func() *fakeHost {
	return func() *fakeHost { return withDetails(onVMKernel(p), vmModel(), vmCPU, 4, 1) }
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
// 48 cores on 2 sockets, vmbr0, root's session.
func pveOn(health string) *fakeHost {
	h := pveNode()
	h.f.AddFile(detect.SelfMounts, []byte("proc /proc proc rw,nosuid,nodev,noexec,relatime 0 0\n"+pveDataset+" / zfs rw,relatime,xattr,noacl 0 0\n"+
		"/dev/fuse /etc/pve fuse rw,nosuid,nodev,relatime,user_id=0,group_id=0,default_permissions,allow_other 0 0\n"), 0o444, 0, 0)
	return withDetails(h, detailsModel{addrs: ipLine(4, "vmbr0", "inet", "192.0.2.20/24"), pool: health, dataset: pveDataset,
		zfs: "53687091200\n429496729600\n", memInfo: "MemTotal:       131072000 kB\nMemFree:        81234000 kB\nMemAvailable:   96123000 kB\n", lastOutput: "root     pts/0        192.0.2.10       Tue Oct 20 09:14   still logged in\n\nwtmp begins Thu Sep 10 10:00:00 2026\n"}, serverCPU, 48, 2)
}

// unreadableDetails is Debian 13 whose /proc/meminfo cannot be read.
func unreadableDetails() *fakeHost {
	m := vmModel()
	m.noMemInfo = true
	return withDetails(onVMKernel(debian13Platform), m, vmCPU, 4, 1)
}

// noLoginDetails is Debian 13 where last finds nothing for alice.
func noLoginDetails() *fakeHost {
	h := onVMKernel(debian13Platform)
	m := vmModel()
	m.lastOutput = "\nwtmp begins Mon Oct 19 08:01:22 2026\n"
	return withDetails(h, m, vmCPU, 4, 1)
}

// manyDetails is Debian 13 with 14 users and 9 addresses, in documentation
// ranges.
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

// detailAddr is one address the stand-in read.
type detailAddr struct {
	Iface  string `json:"interface"`
	Family string `json:"family"`
	Addr   string `json:"address"`
}

// detailUser is one user other than root and the system's, as origin/main
// chooses them (UID 1000 and up, not nobody, user_info.go:43-61) and the
// tool they are in the group of, "" for none. origin/main also asks sudo
// -l -U (user_info.go:89); the stand-in reads the groups alone, unit (b)'s
// to read as the users check reads them (pkg/engine/controls).
type detailUser struct {
	Name string `json:"name"`
	Tool string `json:"tool"`
}

// detailCPU is /proc/cpuinfo's and /proc/loadavg's.
type detailCPU struct {
	Model   string    `json:"model"`
	Cores   int       `json:"cores"`
	Sockets int       `json:"sockets"`
	Load    [3]string `json:"load"`
}

// detailDisk is the root file system: /proc/self/mounts's device and type,
// then df's figures, or on ZFS the dataset's and its pool's health.
type detailDisk struct {
	Source string `json:"source"`
	Type   string `json:"type"`
	Used   uint64 `json:"usedBytes"`
	Total  uint64 `json:"totalBytes"`
	Pool   string `json:"pool,omitempty"`
	Health string `json:"health,omitempty"`
}

// detailMemory is /proc/meminfo's: used is what MemAvailable leaves.
// origin/main's used is total less MemFree (collectors.go:329), which
// counts the kernel's caches as used.
type detailMemory struct {
	Used  uint64 `json:"usedBytes"`
	Total uint64 `json:"totalBytes"`
}

// detailLogin is who ran hardn and last's line for them. origin/main asks
// for the current user, root under sudo (collectors.go:498).
type detailLogin struct {
	User string `json:"user"`
	When string `json:"when"`
	From string `json:"from"`
}

// detailsReading is what readDetails read: the stand-in for what unit (b)
// builds. A section not read is in unread, under its title, its source
// saying what could not be read, and the rest stays.
type detailsReading struct {
	sh       sdHost
	facts    detect.Facts
	os       string
	kernel   string
	uptime   string
	hostname string
	addrs    []detailAddr
	users    []detailUser
	cpu      detailCPU
	disk     detailDisk
	memory   detailMemory
	login    detailLogin
	unread   map[string]unreadFact
}

// unreadFact says what a section could not read.
type unreadFact struct {
	source string
	err    error
}

// readDetails reads sys, hardn's environment being sh's: real code where
// there is some (detect, the header probe, accounts), the gallery's
// stand-ins for the rest. It changes nothing, and a section it cannot read
// is said where it would be.
func readDetails(ctx context.Context, sys host.System, sh sdHost) detailsReading {
	r := detailsReading{sh: sh, unread: map[string]unreadFact{}}
	fail := func(section, source string, err error) { r.unread[section] = unreadFact{source, err} }
	facts, err := detect.Detect(ctx, sys)
	if err != nil {
		fail(secOS, detect.EtcOSRelease, err)
	}
	r.facts = facts
	hd := probe.Read(ctx, sys, facts)
	head := output.HeaderViewOf("", hd).Context
	r.os = strings.TrimSpace(head.Name + " " + head.Version)
	if head.Codename != "" {
		r.os += " (" + head.Codename + ")"
	}
	if hd.Uptime.Known {
		r.uptime = output.FormatUptime(hd.Uptime.Duration)
	}
	if data, err := sys.ReadFile(ctx, probe.KernelReleaseFile); err != nil {
		fail(secOS, probe.KernelReleaseFile, err)
	} else {
		r.kernel = strings.TrimSpace(string(data))
	}
	if r.hostname = hd.Hostname.Name; hd.Hostname.Err != nil {
		fail(secHost, probe.HostnameFile, hd.Hostname.Err)
	}
	r.readAddrs(ctx, sys, fail)
	r.readUsers(ctx, sys, fail)
	r.readCPU(ctx, sys, fail)
	r.readDisk(ctx, sys, fail)
	r.readMemory(ctx, sys, fail)
	r.readLogin(ctx, sys, fail)
	return r
}

// readAddrs is ip -o addr show scope global's addresses: IPv4 and IPv6.
func (r *detailsReading) readAddrs(ctx context.Context, sys host.System, fail func(string, string, error)) {
	res, err := sys.Run(ctx, "ip", "-o", "addr", "show", "scope", "global")
	if err != nil {
		fail(secHost, "ip addr", err)
		return
	}
	for _, line := range strings.Split(string(res.Stdout), "\n") {
		if f := strings.Fields(line); len(f) >= 4 && (f[2] == "inet" || f[2] == "inet6") {
			r.addrs = append(r.addrs, detailAddr{Iface: f[1], Family: f[2], Addr: f[3]})
		}
	}
}

// readUsers is accounts.Read's users, UID 1000 and up but nobody, and the
// tool each is in the group of.
func (r *detailsReading) readUsers(ctx context.Context, sys host.System, fail func(string, string, error)) {
	db, err := accounts.Read(ctx, sys)
	if err != nil {
		fail(secUsers, accounts.PasswdFile, err)
		return
	}
	for _, u := range db.Users {
		if u.UID < 1000 || u.Name == "nobody" || u.Name == "nfsnobody" {
			continue
		}
		du := detailUser{Name: u.Name}
		switch {
		case r.facts.Elevation.Doas.Present == detect.Yes && db.Member(u, "wheel"):
			du.Tool = "doas"
		case db.Member(u, "sudo"), db.Member(u, "admin"), db.Member(u, "wheel"):
			du.Tool = "sudo"
		}
		r.users = append(r.users, du)
	}
}

// readCPU is /proc/cpuinfo's model, count and sockets, and /proc/loadavg's
// three figures.
func (r *detailsReading) readCPU(ctx context.Context, sys host.System, fail func(string, string, error)) {
	data, err := sys.ReadFile(ctx, "/proc/cpuinfo")
	if err != nil {
		fail(secCPU, "/proc/cpuinfo", err)
		return
	}
	sockets := map[string]bool{}
	for _, line := range strings.Split(string(data), "\n") {
		k, v, ok := strings.Cut(line, ":")
		k, v = strings.TrimSpace(k), strings.TrimSpace(v)
		switch {
		case !ok:
		case k == "processor":
			r.cpu.Cores++
		case k == "model name" && r.cpu.Model == "":
			r.cpu.Model = v
		case k == "physical id":
			sockets[v] = true
		}
	}
	r.cpu.Sockets = max(1, len(sockets))
	load, err := sys.ReadFile(ctx, "/proc/loadavg")
	if err != nil {
		fail(secCPU, "/proc/loadavg", err)
		return
	}
	if f := strings.Fields(string(load)); len(f) >= 3 {
		r.cpu.Load = [3]string{f[0], f[1], f[2]}
	}
}

// readDisk is the root file system's: from /proc/self/mounts its source
// and type, then df -kP / or, on ZFS, the dataset mounted there and its
// pool, never a dataset of the gallery's own.
func (r *detailsReading) readDisk(ctx context.Context, sys host.System, fail func(string, string, error)) {
	data, err := sys.ReadFile(ctx, detect.SelfMounts)
	if err != nil {
		fail(secDisk, detect.SelfMounts, err)
		return
	}
	for _, line := range strings.Split(string(data), "\n") {
		if f := strings.Fields(line); len(f) >= 3 && f[1] == "/" {
			r.disk.Source, r.disk.Type = f[0], f[2]
		}
	}
	if r.disk.Type == "zfs" {
		r.disk.Pool, _, _ = strings.Cut(r.disk.Source, "/")
		pool, err := sys.Run(ctx, "zpool", "list", "-H", "-o", "health", r.disk.Pool)
		if err != nil {
			fail(secDisk, "zpool list", err)
			return
		}
		r.disk.Health = strings.TrimSpace(string(pool.Stdout))
		res, err := sys.Run(ctx, "zfs", "get", "-Hp", "-o", "value", "used,available", r.disk.Source)
		if err != nil {
			fail(secDisk, "zfs get", err)
			return
		}
		if f := strings.Fields(string(res.Stdout)); len(f) == 2 {
			used, _ := strconv.ParseUint(f[0], 10, 64)
			avail, _ := strconv.ParseUint(f[1], 10, 64)
			r.disk.Used, r.disk.Total = used, used+avail
		}
		return
	}
	res, err := sys.Run(ctx, "df", "-kP", "/")
	if err != nil {
		fail(secDisk, "df", err)
		return
	}
	lines := strings.Split(strings.TrimSpace(string(res.Stdout)), "\n")
	if f := strings.Fields(lines[len(lines)-1]); len(f) >= 6 {
		total, _ := strconv.ParseUint(f[1], 10, 64)
		used, _ := strconv.ParseUint(f[2], 10, 64)
		r.disk.Total, r.disk.Used = total*1024, used*1024
	}
}

// readMemory is /proc/meminfo's total, less what MemAvailable leaves.
func (r *detailsReading) readMemory(ctx context.Context, sys host.System, fail func(string, string, error)) {
	data, err := sys.ReadFile(ctx, "/proc/meminfo")
	if err != nil {
		fail(secMemory, "/proc/meminfo", err)
		return
	}
	var total, avail uint64
	for _, line := range strings.Split(string(data), "\n") {
		if f := strings.Fields(line); len(f) >= 2 {
			n, _ := strconv.ParseUint(f[1], 10, 64)
			switch f[0] {
			case "MemTotal:":
				total = n * 1024
			case "MemAvailable:":
				avail = n * 1024
			}
		}
	}
	r.memory = detailMemory{Used: total - avail, Total: total}
}

// who is the user who ran hardn: SUDO_USER or DOAS_USER, as the Environment
// control reads them, else the current user, USER here.
func (sh sdHost) who() string {
	for _, k := range []string{"SUDO_USER", "DOAS_USER", "USER"} {
		if v := sh.env[k]; v != "" {
			return v
		}
	}
	return "root"
}

// readLogin is last -n 1's line for who ran hardn. Which source each
// release keeps logins in is [unverified] (wtmp; origin/main's lastlog -u,
// lastlog_command_adapter.go:36), and last's most recent line is commonly
// this very session [I]: unit (b)'s, on the VMs.
func (r *detailsReading) readLogin(ctx context.Context, sys host.System, fail func(string, string, error)) {
	r.login.User = r.sh.who()
	res, err := sys.Run(ctx, "last", "-n", "1", r.login.User)
	if err != nil {
		fail(secLogin, "last", err)
		return
	}
	line, _, _ := strings.Cut(string(res.Stdout), "\n")
	if f := strings.Fields(line); len(f) >= 7 && f[0] == r.login.User {
		r.login.From, r.login.When = f[2], f[4]+" "+f[5]+" "+f[6]
	}
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

// sectionRows are section's rows: what it read, or the one row that says
// what it could not.
func (r detailsReading) sectionRows(section string) []component.KeyValue {
	if u, ok := r.unread[section]; ok {
		return []component.KeyValue{{Key: section, Note: fmt.Sprintf(cantRead, u.source), State: component.ValueUnknown}}
	}
	switch section {
	case secOS:
		return []component.KeyValue{{Key: rowOS, Value: r.os}, {Key: rowKernel, Value: r.kernel}, {Key: rowUptime, Value: r.uptime}}
	case secHost:
		rows := []component.KeyValue{{Key: rowHostname, Value: r.hostname}}
		for _, a := range r.addrs {
			rows = append(rows, component.KeyValue{Key: a.Iface, Value: a.Addr})
		}
		if len(r.addrs) == 0 {
			rows = append(rows, component.KeyValue{Key: noAddresses, State: component.ValueNone})
		}
		return rows
	case secUsers:
		return r.userRows()
	case secCPU:
		return []component.KeyValue{{Key: rowModel, Value: r.cpu.Model}, {Key: rowCores, Value: coresWords(r.cpu)},
			{Key: rowLoad, Value: strings.Join(r.cpu.Load[:], ", ")}}
	case secDisk:
		rows := []component.KeyValue{{Key: rowRoot, Value: r.disk.Source + " " + r.disk.Type}, {Key: rowUsed, Value: usedOf(r.disk.Used, r.disk.Total)}}
		if r.disk.Pool != "" {
			state := component.ValueWarn
			if r.disk.Health == "ONLINE" {
				state = component.ValueOK
			}
			rows = append(rows, component.KeyValue{Key: fmt.Sprintf(rowPool, r.disk.Pool), Value: r.disk.Health, State: state})
		}
		return rows
	case secMemory:
		return []component.KeyValue{{Key: rowUsed, Value: usedOf(r.memory.Used, r.memory.Total)}}
	case secLogin:
		if r.login.When == "" {
			return []component.KeyValue{{Key: r.login.User, Value: noLogin, State: component.ValueNone}}
		}
		return []component.KeyValue{{Key: r.login.User, Value: r.login.When + " from " + r.login.From}}
	}
	return nil
}

// userRows are the Users section's rows.
func (r detailsReading) userRows() []component.KeyValue {
	var rows []component.KeyValue
	noTool := noSudo
	if r.facts.Elevation.Doas.Present == detect.Yes {
		noTool = noDoas
	}
	for _, u := range r.users {
		tool := u.Tool
		if tool == "" {
			tool = noTool
		}
		rows = append(rows, component.KeyValue{Key: u.Name, Value: tool})
	}
	if len(rows) == 0 {
		rows = []component.KeyValue{{Key: noUsers, State: component.ValueNone}}
	}
	if r.sh.notRoot {
		rows = append(rows, component.KeyValue{Key: fmt.Sprintf(notRootRow, "/etc/sudoers.d"), State: component.ValueWarn})
	}
	return rows
}

// coresWords is "4 cores, 1 socket".
func coresWords(c detailCPU) string {
	plural := func(n int, one string) string {
		if n == 1 {
			return "1 " + one
		}
		return fmt.Sprintf("%d %ss", n, one)
	}
	return plural(c.Cores, "core") + ", " + plural(c.Sockets, "socket")
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

// summary is section's one line, for treatments report-sections and
// report-pager.
func (r detailsReading) summary(section string) (string, component.ValueState) {
	if _, ok := r.unread[section]; ok {
		return "", component.ValueUnknown
	}
	switch section {
	case secOS:
		return r.os, component.ValuePlain
	case secHost:
		s := r.hostname
		if len(r.addrs) > 0 {
			s += ", " + r.addrs[0].Addr
		}
		if n := len(r.addrs) - 1; n > 0 {
			s += fmt.Sprintf(" (+%d)", n)
		}
		return s, component.ValuePlain
	case secUsers:
		switch n := len(r.users); n {
		case 0:
			return noUsers, component.ValuePlain
		case 1:
			return r.users[0].Name, component.ValuePlain
		default:
			return fmt.Sprintf("%d users", n), component.ValuePlain
		}
	case secCPU:
		return strconv.Itoa(r.cpu.Cores) + " cores, load " + r.cpu.Load[0], component.ValuePlain
	case secDisk:
		return percent(r.disk.Used, r.disk.Total) + " of " + gib(r.disk.Total) + " GiB", component.ValuePlain
	case secMemory:
		return percent(r.memory.Used, r.memory.Total) + " of " + gib(r.memory.Total) + " GiB", component.ValuePlain
	case secLogin:
		if r.login.When == "" {
			return noLogin, component.ValuePlain
		}
		return r.login.User + ", " + r.login.When, component.ValuePlain
	}
	return "", component.ValuePlain
}

// sdItem is one item of the screen, and which action it runs.
type sdItem struct {
	item    component.MenuItem
	section string // report-sections: the section it opens
	action  string // itemRefresh, itemExport, itemOpen
}

// items are the screen's items: under report-sections one per section,
// under report-pager Open; then Refresh; then Export under its treatments.
// Under export-file, as origin/main's (system_details_menu.go:75-80), it is
// not offered with a section unread, the file being part of a report;
// export-command's command prints what can be read.
func (r detailsReading) items() []sdItem {
	var out []sdItem
	add := func(label, about, section, action string) {
		out = append(out, sdItem{item: component.MenuItem{Key: strconv.Itoa(len(out) + 1), Label: label, Description: about}, section: section, action: action})
	}
	switch r.sh.report {
	case reportSections:
		for _, s := range sdSections {
			about, _ := r.summary(s)
			if _, unread := r.unread[s]; unread {
				about = "Unknown"
			}
			add(s, about, s, "")
		}
	case reportPager:
		add(itemOpen, fmt.Sprintf(aboutOpen, "less"), "", itemOpen)
	}
	add(itemRefresh, aboutRefresh, "", itemRefresh)
	switch r.sh.export {
	case exportCommand:
		add(itemExport, aboutCommand, "", itemExport)
	case exportFile:
		if len(r.unread) == 0 {
			add(itemExport, aboutFile, "", itemExport)
		}
	}
	return out
}

// subMenu is the screen, in the host's treatments.
func (r detailsReading) subMenu() screen.SubMenu {
	var items []component.MenuItem
	for _, it := range r.items() {
		items = append(items, it.item)
	}
	vm := screen.SubMenu{
		Title:   component.TitleBar{Title: sdTitle},
		Heading: sdHeading,
		Menu:    component.Menu{Items: items, Exit: component.MenuItem{Key: "0", Label: "Return"}},
		Prompt:  sdPrompt,
	}
	switch r.sh.report {
	case reportSections:
	case reportPager:
		vm.Sections = []screen.Section{{Header: component.SectionHeader{Title: secSummary}, Rows: component.BulletList{Items: r.summaryRows()}}}
	default:
		vm.Sections = r.sections()
	}
	return vm
}

// summaryRows are report-pager's rows: a section and its one line.
func (r detailsReading) summaryRows() []component.KeyValue {
	var rows []component.KeyValue
	for _, s := range sdSections {
		v, state := r.summary(s)
		rows = append(rows, component.KeyValue{Key: s, Value: v, State: state})
	}
	return rows
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
	sh    sdHost
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
// only, and refuses treatment export-file's write.
func (s *session) sdScreen(ctx context.Context, sh sdHost, h *fakeHost) error {
	run := &sdRun{s: s, sh: sh, h: h}
	if err := s.writeNote(sdPrefix+"/"+sh.name, sh.note); err != nil {
		return err
	}
	return s.p.Run(func() nav.Screen { return run.draw(ctx) })
}

// draw reads the host and returns the screen, its items bound.
func (run *sdRun) draw(ctx context.Context) nav.Screen {
	s := run.s
	r := readDetails(ctx, run.h.sys(), run.sh)
	vm := r.subMenu()
	menu := nav.Menu{Exit: vm.Menu.Exit}
	for _, it := range r.items() {
		menu.Options = append(menu.Options, nav.Option{Item: it.item, Action: run.action(r, it)})
	}
	return vm.Screen(s.p.Ctx, menu)
}

// action is what item it does on the screen.
func (run *sdRun) action(r detailsReading, it sdItem) func() error {
	s := run.s
	write := func(text string) func() error {
		return func() error {
			_, err := io.WriteString(s.p.Out, strings.Repeat("\n", theme.SectionGap)+text+"\n")
			return err
		}
	}
	c := s.p.Ctx
	switch {
	case it.section != "":
		return write(r.section(it.section).Render(c))
	case it.action == itemRefresh:
		return func() error { run.tick(); return nil }
	case it.action == itemOpen:
		return write(pagerNote(c, "the report on its standard input"))
	case it.action == itemExport && run.sh.export == exportCommand:
		return s.say(component.Message{Text: saveCommand})
	case it.action == itemExport && s.sequence == "dry-run":
		return s.say(component.Message{Text: planOnlyNo})
	case it.action == itemExport:
		return write(exportFileWords(c))
	}
	return func() error { return nil }
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
	f.AddFile("/proc/loadavg", []byte(fmt.Sprintf("0.%02d 0.09 0.03 1/123 4567\n", 12+run.ticks*10)), 0o444, 0, 0)
}

// pagerNote is the gallery's note where hardn would run a pager: no golden
// can run one, and the fake models none.
func pagerNote(c component.Ctx, what string) string {
	return component.Callout{Kind: component.MessageInfo, Body: []string{
		"The gallery runs no pager. hardn would run LESSSECURE=1 less, " + what + ", then draw this screen again.",
	}}.Render(c)
}

// exportFileWords are treatment export-file's words: the question, a
// relative path refused, the file written. The gallery writes nothing.
func exportFileWords(c component.Ctx) string {
	return apart(
		component.PromptLine{Label: askPath}.Render(c),
		component.Message{Kind: component.MessageWarn, Text: wholePath}.Render(c),
		component.Message{Kind: component.MessageOK, Text: savedTo}.Render(c),
	)
}

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
const sdNote = "Play mode, mocks: the System Details screen proposed for slice 1, read from a simulated host, " +
	"by real code where there is some (detect, the header probe, accounts) and by the gallery's stand-ins elsewhere, unit (b)'s to build. " +
	"It only reads: no plan, no y/N, no journal. Nothing on this machine changes: every command and file is the fake's."

// readSDHost reads the host named name, fresh, as the screen does.
func readSDHost(ctx context.Context, name string) (detailsReading, error) {
	sh, ok := findSDHost(name)
	if !ok {
		return detailsReading{}, fmt.Errorf("no system-details host %q", name)
	}
	return readDetails(ctx, sh.host().sys(), sh), nil
}

// notRootHost is Debian 13, hardn run by alice, not through sudo.
func notRootHost() sdHost {
	return sdHost{name: "not-root", host: details(debian13Platform), env: map[string]string{"USER": "alice"}, notRoot: true}
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

// sdMocks are the listing's mocks, in order: the screen on each host, each
// treatment's item opened, the wide form, plan only, the non-interactive
// forms, and origin/main's report in its own words.
func sdMocks() []listMock {
	var mocks []listMock
	for _, sh := range sdHosts() {
		mocks = append(mocks, listMock{name: "screen-" + sh.name, note: "Mock: the System Details screen. " + sh.note,
			draw: onSDHost(sh.name, func(r detailsReading, c component.Ctx) string { return r.subMenu().Render(c) })})
	}
	return append(mocks,
		listMock{name: reportSections + "-open", note: "Treatment report-sections: item 5, Disk, opened, under the prompt, on the screen drawn again.",
			draw: onSDHost(reportSections, func(r detailsReading, c component.Ctx) string {
				return apart(r.subMenu().Render(c), r.section(secDisk).Render(c), r.subMenu().Render(c))
			})},
		listMock{name: reportPager + "-open", note: "Treatment report-pager: item 1, Open. A golden cannot run a pager: this is what the screen says and the command hardn would run.",
			draw: onSDHost(reportPager, func(r detailsReading, c component.Ctx) string {
				return apart(r.subMenu().Render(c), pagerNote(c, "the report on its standard input"), r.subMenu().Render(c))
			})},
		listMock{name: reportWide, wide: wideCols, note: fmt.Sprintf("Treatment report-wide: two columns from %d cells up (run with -width %d), a second 64-cell box beside the first, past the content cap of §7 and so a new layout; below that width it is report-whole.", wideCols, wideCols),
			draw: onSDHost("debian13", wideDrawing)},
		listMock{name: exportCommand + "-open", note: "Treatment export-command: item 2 says the command.",
			draw: onSDHost(exportCommand, func(r detailsReading, c component.Ctx) string {
				return apart(r.subMenu().Render(c), component.Message{Text: saveCommand}.Render(c))
			})},
		listMock{name: exportFile + "-open", note: "Treatment export-file: item 2's question, a relative path refused (origin/main takes it, system_details_menu.go:135, and writes under the current directory as root), and the file written. Under plan only it says " + planOnlyNo,
			draw: onSDHost(exportFile, func(r detailsReading, c component.Ctx) string {
				return apart(r.subMenu().Render(c), exportFileWords(c))
			})},
		listMock{name: "plan-only", note: "Plan only, as the other sub-menus show it: they show nothing. No sub-menu shows the mode (SubMenu.Heading, sub.go:58: \"with no mode beside it\"; play/backup-plan-only, play/environment-main-plan-only). The screen is the same under plan only, since it changes nothing, and the main menu says the mode.",
			draw: onSDHost("debian13", func(r detailsReading, c component.Ctx) string { return r.subMenu().Render(c) })},
		listMock{name: "plan-only-indicator", note: "Treatment mode-beside-heading, not proposed: the mode beside this screen's heading, as the main menu has it (\"Mode Plan only (-n)\"). No sub-menu does this today, and the layout has no field for it.",
			draw: onSDHost("debian13", planOnlyIndicator)},
		listMock{name: "cli-terminal", note: "hardn system-details to a terminal: the report, no menu and no prompt. It reads the same as the screen.",
			draw: onSDHost("debian13", func(r detailsReading, c component.Ctx) string { return r.report(c) })},
		listMock{name: "cli-pipe", wide: theme.ContentWidth, note: "hardn system-details not to a terminal: plain mode, no colour, ASCII, whatever the terminal around it (term.Env's Plain). Its values are cut at the 64-cell content width like any row's; see cli-pipe-cut.",
			draw: onSDHost("debian13", func(r detailsReading, _ component.Ctx) string { return r.report(offTerminalCtx()) })},
		listMock{name: "cli-pipe-cut", wide: theme.ContentWidth, note: "The same on pve9: the CPU's name is 40 cells, its row holds 37, so the pipe loses its end, as a row cuts on a terminal. Unit (b) must not cut off a terminal: nothing in pkg/ui lays out uncapped today, so it needs a decision.",
			draw: onSDHost("pve9", func(r detailsReading, _ component.Ctx) string { return r.report(offTerminalCtx()) })},
		listMock{name: "cli-not-root", note: "hardn system-details as alice, not through sudo: reachable, as the subcommand has no root check (cmd/hardn/main.go:84; the check at :148 is in the root command's Run). The main menu and -p are not. What it cannot read it says; /etc/sudoers.d's files are 0440, root's [I]. origin/main's main() also prints two lines of its own first, unrelated: the log it cannot open and the example configuration it cannot write (cmd/hardn/main.go:57-62; logging.go:39).",
			draw: func(ctx context.Context, c component.Ctx) (string, error) {
				sh := notRootHost()
				return readDetails(ctx, sh.host().sys(), sh).report(c), nil
			}},
		listMock{name: "cli-json", wide: theme.ContentWidth, note: "Proposal, depends on §3.11 (not approved: cmd/hardn/output.go:4): hardn system-details --output json. On pkg/output's document, not beside it: --output json is one flag with one contract (docs/reference/output.md), mode \"check\" already means \"read, change nothing\", host already holds the name and OS, and wire.go's SchemaVersion comment lets a field be added without a new version. So the nine fields, and one more, details, on this form alone; result nothing-to-do, exit 0.",
			draw: onSDHost("debian13", jsonDrawing)},
		listMock{name: "cli-json-unreadable", wide: theme.ContentWidth, note: "Proposal, as cli-json: a section not read is null in details and one finding, DETAILS_UNREADABLE (proposed code), which is not an error: the rest was read, so the exit status stays 0.",
			draw: onSDHost("unreadable", jsonDrawing)},
		listMock{name: "cli-flags", note: "The five flags origin/main parses and ignores, -E/--export, -S/--section, -O/--format, --json, --yaml (pkg/cmd/system_details_cmd.go:38-42, the code using them commented out at :95-121). What they become is §3.11's: not approved, so this is a proposal.",
			draw: func(_ context.Context, c component.Ctx) (string, error) { return flagsDrawing(c), nil }},
		listMock{name: "origin", note: "origin/main's report, its words as they are (display.go:43-129), on the design system, on the debian13 host: its rows, its items (system_details_menu.go:70-80), its heading and its prompt. Its 50-column box, its screen clear and its bar graphs are left out. Read, not run: it has no seam (pkg/characterization/host_info_test.go:14-27).",
			draw: onSDHost("debian13", func(r detailsReading, c component.Ctx) string { return r.originScreen(false).Render(c) })},
		listMock{name: "origin-error", note: "origin/main's report when any collector fails (system_details_menu.go:42-62): the box's words, the report gone, Export not offered. Read, not run.",
			draw: onSDHost("unreadable", func(r detailsReading, c component.Ctx) string {
				return apart(component.Callout{Kind: component.MessageBad, Title: "System Details Error", Body: []string{
					"Failed to retrieve system status information.", "Error: failed to collect memory info: failed to get memory info: exec: \"free\": executable file not found",
					"Please check system permissions and try again.",
				}}.Render(c), r.originScreen(true).Render(c))
			})},
	)
}

// wideDrawing is treatment report-wide: at wideCols and up, the sections
// in two columns, then the menu; narrower, report-whole.
func wideDrawing(r detailsReading, c component.Ctx) string {
	if c.Env.Width < wideCols {
		return r.subMenu().Render(c)
	}
	col := func(titles ...string) string {
		var blocks []string
		for _, t := range titles {
			blocks = append(blocks, r.section(t).Render(c))
		}
		return apart(blocks...)
	}
	left := col(secOS, secHost, secUsers, secCPU)
	right := col(secDisk, secMemory, secLogin)
	vm := r.subMenu()
	vm.Sections, vm.Title = nil, component.TitleBar{}
	menu := strings.TrimLeft(vm.Render(c), "\n")
	_, menu, _ = strings.Cut(menu, "\n\n") // the empty title bar
	return apart(component.TitleBar{Title: sdTitle}.Render(c), beside(left, right, theme.ContentWidth, sideGap), menu)
}

// planOnlyIndicator is the screen with the mode beside its heading, each
// from its component.
func planOnlyIndicator(r detailsReading, c component.Ctx) string {
	mode := component.ModeIndicator{DryRun: true, Source: component.ModeSourceFlag}
	heading := component.MenuHeading{Title: sdHeading}.Render(c)
	return strings.Replace(r.subMenu().Render(c), heading, component.MenuHeading{Title: sdHeading, Mode: &mode}.Render(c), 1)
}

// detailsDocument is the proposed JSON form: pkg/output's document, built
// by output.Build, and one field more. Gallery-local: the field is unit
// (b)'s, should §3.11 be approved.
type detailsDocument struct {
	output.Document
	Details detailsJSON `json:"details"`
}

// detailsJSON is the report's facts; a section not read is null.
type detailsJSON struct {
	Kernel    *string       `json:"kernel"`
	Uptime    *string       `json:"uptime"`
	Addresses []detailAddr  `json:"addresses"`
	Users     []detailUser  `json:"users"`
	CPU       *detailCPU    `json:"cpu"`
	Disk      *detailDisk   `json:"disk"`
	Memory    *detailMemory `json:"memory"`
	LastLogin *detailLogin  `json:"lastLogin"`
}

// document is the reading as the proposed JSON form.
func (r detailsReading) document() detailsDocument {
	facts := r.facts
	d := detailsDocument{Document: output.Build(output.Run{Mode: output.ModeCheck, HardnVersion: "0.4.0", Hostname: r.hostname, Facts: &facts})}
	read := func(section string) bool { _, unread := r.unread[section]; return !unread }
	if read(secOS) {
		d.Details.Kernel, d.Details.Uptime = &r.kernel, &r.uptime
	}
	d.Details.Addresses, d.Details.Users = []detailAddr{}, []detailUser{}
	if read(secHost) && r.addrs != nil {
		d.Details.Addresses = r.addrs
	}
	if read(secUsers) && r.users != nil {
		d.Details.Users = r.users
	}
	if read(secCPU) {
		d.Details.CPU = &r.cpu
	}
	if read(secDisk) {
		d.Details.Disk = &r.disk
	}
	if read(secMemory) {
		d.Details.Memory = &r.memory
	}
	if read(secLogin) {
		d.Details.LastLogin = &r.login
	}
	for _, s := range sdSections {
		if u, ok := r.unread[s]; ok {
			d.Findings = append(d.Findings, output.Finding{Code: "DETAILS_UNREADABLE", Detail: strings.ToLower(s) + " not read", Source: u.source})
		}
	}
	return d
}

// jsonDrawing is the proposed JSON form, as it would print, in plain text.
func jsonDrawing(r detailsReading, _ component.Ctx) string {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(r.document()); err != nil {
		return err.Error()
	}
	return strings.TrimRight(b.String(), "\n")
}

// flagsDrawing is the five flags, today and as proposed under §3.11.
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
		component.SectionHeader{Title: "Until 3.11 is decided"}.Render(c),
		component.BulletList{Items: []component.KeyValue{{Key: "All five", Value: "Parsed and ignored, as today"}}}.Render(c),
	)
}

// originScreen is origin/main's report in its words, on the design system.
// failed draws its items alone, as after a collector's error, Export not
// offered.
func (r detailsReading) originScreen(failed bool) screen.SubMenu {
	items := []component.MenuItem{{Key: "1", Label: "Refresh Information", Description: "Reload system status from system"}}
	vm := screen.SubMenu{Title: component.TitleBar{Title: sdTitle}, Heading: sdHeading, Prompt: "Enter your choice"}
	if !failed {
		items = append(items, component.MenuItem{Key: "2", Label: "Export Information", Description: "Export system status to file"})
		rows := []component.KeyValue{
			{Key: "OS", Value: "Debian 13 Trixie"}, {Key: "Kernel", Value: "Linux " + r.kernel}, {Key: "Hostname", Value: r.hostname},
			{Key: "IP Addresses", Value: "192.0.2.20"}, {Key: "Client IP", Value: "Not connected"}, {Key: "DNS IP 1", Value: "192.0.2.53"}, {Key: "User", Value: "root"},
			{Key: "Non-System Users", Value: "alice (sudo)"}, {Key: "Processor", Value: r.cpu.Model}, {Key: "Cores", Value: "4 vCPU(s) / 1 Socket(s)"},
			{Key: "Hypervisor", Value: "KVM"}, {Key: "CPU Freq", Value: "2.00 GHz"}, {Key: "Load 1m", Value: "(0.12)"}, {Key: "Load 5m", Value: "(0.08)"}, {Key: "Load 15m", Value: "(0.03)"},
			{Key: "Volume", Value: "6.17/38.60 GB [15.99%]"}, {Key: "Memory", Value: "3.07/3.84 GiB [79.84%]"},
			{Key: "Last Login", Value: "Never logged in"}, {Key: "Uptime", Value: r.uptime},
		}
		vm.Sections = []screen.Section{{Rows: component.BulletList{Items: rows}}}
	}
	vm.Menu = component.Menu{Items: items, Exit: component.MenuItem{Key: "0", Label: "Return"}}
	return vm
}

// listMock is one mock a listing shows: its name, its note, its drawing,
// and the cells it needs, if more than any terminal's: its off-terminal
// and wide forms do not draw to the listing's width.
type listMock struct {
	name, note string
	draw       func(ctx context.Context, c component.Ctx) (string, error)
	wide       int
}

// listing is a mode that lists mocks with no input, as "status" and
// Environment's did: each under the gallery's header, "<prefix> <mock>",
// its note, then its drawing, a write each; -list prints their names. It
// sizes itself as play does (look.sizeTo).
type listing struct {
	prefix, usage string
	mocks         func() []listMock
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
					component.Callout{Kind: component.MessageInfo, Body: []string{m.note}}.Render(rc), out)+sectionBreak)
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
var sdListing = listing{prefix: sdPrefix, usage: sdUsage, mocks: sdMocks}
