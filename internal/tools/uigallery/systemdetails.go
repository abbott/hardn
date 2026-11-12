package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"slices"
	"strconv"
	"strings"

	"github.com/abbott/hardn/pkg/engine/details"
	probe "github.com/abbott/hardn/pkg/engine/header"
	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/hostfake"
	"github.com/abbott/hardn/pkg/host/sudoers"
	sdscreen "github.com/abbott/hardn/pkg/screens/systemdetails"
	"github.com/abbott/hardn/pkg/ui/component"
	"github.com/abbott/hardn/pkg/ui/nav"
	"github.com/abbott/hardn/pkg/ui/screen"
	"github.com/abbott/hardn/pkg/ui/term"
	"github.com/abbott/hardn/pkg/ui/theme"
)

// The System Details screen on the simulated hosts (refactor plan slice 1, unit (c),
// next/s1-screen): hardn's System Details screen itself (pkg/screens/systemdetails),
// drawn from what details.Read reads of each host, on the hosts the mocks were drawn
// for, which it replaces (docs/engine/screen-system-details.md). There is no Figma
// frame and it is not in v0.3.2: it is inferred from origin/main, with P-17's
// sections. "play system-details" is the menu of the hosts and "play
// system-details/<host>" the screen on one; the main menu's 9 leads to it on Debian
// 13, under plan only too (-sequence dry-run), as unit (d) wires hardn's own.
//
// detailsModel is what a simulated host answers the reader, in the forms that ran
// read-only, as a user, on the Debian 13, Ubuntu 26.04 and Alpine 3.23 VMs
// (vm-runs/s1), but ZFS's, no VM having it, and inet6, none having a global address.
//
// "uigallery system-details" lists the non-interactive forms: hardn system-details'
// through the real Print (cli-terminal, cli-pipe, cli-pipe-cut, cli-not-root). The
// listing's other mocks were retired with the screen and, cli-json, cli-json-unreadable
// and cli-flags, once unit (d) built them: git keeps them (next/s1-parity, next/s1-screen),
// and gallery-system-details.md records what each showed.

// sdPrefix names the forms and hosts: "system-details <mock>" in the listing,
// "system-details" and "system-details/<host>" in play.
const sdPrefix = "system-details"

// sdLabel is the source label every form in the listing carries (plan: "inferred
// from origin/main (unreleased)"; P-17).
const sdLabel = "Inferred from origin/main (unreleased)."

// sdMainHost is the host the main menu's 9 leads to.
const sdMainHost = sdPrefix + "/debian13"

// sdLinks lead from a menu mock's item to the System Details screen on a
// host, by the mock's name and the item's key, beside links: the main
// menu's 9, which said the gallery had no screen for it until unit (a).
var sdLinks = map[string]string{mainMenuMock + "/8": sdMainHost}

// sdHost is one simulated host the System Details screen runs on.
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
// written from their manuals, no VM having ZFS, and inet6, none having a global
// address.
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
// command will. It changes nothing, and what it cannot read the sections say.
func readDetails(ctx context.Context, sys host.System) detailsReading {
	facts, _ := detect.Detect(ctx, details.Bounded(sys)) // what detect does not establish, the report says not read
	return detailsReading{Report: details.Read(ctx, sys, facts), facts: facts}
}

// sdMoving is a System Details host as play reads it: each read of the uptime after
// the first finds the host a minute on, so that a Refresh draws a change. The screen
// has no Refresh of its own to hook: what moves is the gallery's fake's, put there
// unrecorded.
type sdMoving struct {
	statusSys
	h            *fakeHost
	reads, ticks int
}

// ReadFile is the host's, a minute on from the second read of the uptime.
func (m *sdMoving) ReadFile(ctx context.Context, name string) ([]byte, error) {
	if name == probe.UptimeFile {
		if m.reads++; m.reads > 1 {
			m.tick()
		}
	}
	return m.statusSys.ReadFile(ctx, name)
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
	m := boundHosts{vm: screen.SubMenu{Title: component.TitleBar{Title: title}, Heading: "Select an option", Prompt: "Option"}}
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

// sdScreen runs hardn's System Details screen on h, sh's host, until the user goes
// back from it: its note above it; the facts detect's through details.Bounded, as
// hardn system-details has them. Nothing is written; the plan-only sequence draws the
// same screen, as Options has no mode.
func (s *session) sdScreen(ctx context.Context, sh sdHost, h *fakeHost) error {
	if err := s.writeNote(sdPrefix+"/"+sh.name, sh.note); err != nil {
		return err
	}
	facts, _ := detect.Detect(ctx, details.Bounded(h.sys())) // what detect does not establish, the report says not read
	return sdscreen.Run(ctx, sdscreen.Options{Prompter: s.p, System: &sdMoving{statusSys: h.sys(), h: h}, Facts: facts, Now: clock})
}

// tick moves the simulated host on, as if a Refresh a minute later found it
// so: its uptime and load, so that the screen drawn again shows a change.
func (m *sdMoving) tick() {
	m.ticks++
	f := m.h.f
	up := fmt.Sprintf("%d.37 553000.12\n", 277920+60*m.ticks)
	if e, ok := f.Entry(probe.UptimeFile); ok && string(e.Data) == upWeeks {
		up = fmt.Sprintf("%d.00 7000000.00\n", 3546000+60*m.ticks)
	}
	f.AddFile(probe.UptimeFile, []byte(up), 0o444, 0, 0)
	f.AddFile(details.LoadAvgFile, []byte(fmt.Sprintf("0.%02d 0.09 0.03 1/123 4567\n", 12+m.ticks*10)), 0o444, 0, 0)
}

// main()'s two lines as a user other than root: logging.go's and
// cmd/hardn/main.go's, on stderr since unit (d), the reasons the system's words
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

// sdNote is play's note for the System Details screen.
const sdNote = "Play mode: hardn's System Details screen (pkg/screens/systemdetails), inferred from origin/main (unreleased), read from a simulated host by the real reader (pkg/engine/details). " +
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

// userHost is a host of platform p as alice reads it, not through sudo.
func userHost(p platform) host.System {
	h := vmDetails(p)()
	h.f.AddFile(details.SelfStatusFile, []byte(userStatus), 0o444, 0, 0)
	return asUserSys{h.sys()}
}

// printDetails is what hardn system-details writes on sys in env: the real Print.
func printDetails(ctx context.Context, sys host.System, env term.Env) (string, error) {
	facts, _ := detect.Detect(ctx, details.Bounded(sys)) // what detect does not establish, the report says not read
	var out strings.Builder
	err := sdscreen.Print(ctx, sdscreen.PrintOptions{Out: &out, Env: env, System: sys, Facts: facts, Now: clock})
	return strings.TrimSuffix(out.String(), "\n"), err
}

// sdPrinted returns a drawing of what hardn system-details writes on the host named
// name, to a terminal of c's size or off one.
func sdPrinted(name string, terminal bool) func(context.Context, component.Ctx) (string, error) {
	return func(ctx context.Context, c component.Ctx) (string, error) {
		sh, ok := findSDHost(name)
		if !ok {
			return "", fmt.Errorf("no system-details host %q", name)
		}
		return printDetails(ctx, sh.host().sys(), printEnv(c, terminal))
	}
}

// printEnv is c's environment as hardn would find it at a terminal, or with its
// output off one, which is all Print asks of it there.
func printEnv(c component.Ctx, terminal bool) term.Env {
	env := c.Env
	env.TTY = terminal
	return env
}

// sdMocks are the listing's forms, in order: hardn system-details through the real
// Print.
func sdMocks() []listMock {
	return []listMock{
		{name: "cli-terminal", note: "hardn system-details to a terminal: the report, no menu and no prompt. It reads the same as the screen.",
			draw: sdPrinted("debian13", true)},
		{name: "cli-pipe", wide: theme.ContentWidth, note: "hardn system-details not to a terminal: plain mode, no colour, ASCII, whatever the terminal around it. As a user other than root, see cli-not-root.",
			draw: sdPrinted("debian13", false)},
		{name: "cli-pipe-cut", wide: theme.ContentWidth, note: "The same on pve9: the CPU's name is 40 cells, its row holds 37, and off a terminal it stays whole (the page, Made here; DEV-067).",
			draw: sdPrinted("pve9", false)},
		{name: "cli-not-root", note: "hardn system-details as alice, not through sudo: reachable, as the subcommand has no root check (cmd/hardn/main.go:84; the check at :148 is in the root command's Run). The main menu and -p are not. What it cannot read it says: /etc/sudoers.d is drwxr-x--- root root, so a user cannot list it; on a doas host /etc/doas.conf, 0640 root (vm-runs/s1/01-tools.log); /etc/sudoers is 0440 (vm-runs/s1/10-forms.log). So a user's row reads Not known, never a group's guess. Last Login reads the same, run (vm-runs/s1/02-login.log). The two lines above the report are main()'s, unrelated, on stderr, so hardn system-details > file holds the report alone (cmd/hardn/main.go, logging.go; read, not run).",
			draw: func(ctx context.Context, c component.Ctx) (string, error) {
				report, err := printDetails(ctx, userHost(debian13Platform), printEnv(c, true))
				return apart(component.CodeBlock{Lines: []component.CodeLine{{Text: mainOpenLine}, {Text: mainExampleLine}}}.Render(c), report), err
			}},
	}
}

// listMock is one mock a listing shows: its name, its note, its drawing,
// and the width it needs, if more than any terminal's: its off-terminal
// forms do not draw to the listing's width.
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
	// label is the source label a mock's note opens with.
	label string
	mocks func() []listMock
}

// note is what m's note says, under the listing's label.
func (l listing) note(m listMock) string { return l.label + " " + m.note }

// run is the listing's mode.
func (l listing) run(args []string, size sizer, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("uigallery "+l.prefix, flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() {
		fmt.Fprintln(stderr, l.usage)
		fs.PrintDefaults()
	}
	list := fs.Bool("list", false, "print the names of the forms it would list, and exit")
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
			return nil, fmt.Errorf("flag %s comes after a form's name; flags go before them", a)
		case !found:
			return nil, fmt.Errorf("no form %q; -list shows them", a)
		}
	}
	return slices.DeleteFunc(all, func(m listMock) bool { return !slices.Contains(args, m.name) }), nil
}

// sdUsage is the listing's usage line.
const sdUsage = "usage: go run ./internal/tools/uigallery system-details [flags] [form | all]..."

// sdListing is the System Details listing.
var sdListing = listing{prefix: sdPrefix, usage: sdUsage, label: sdLabel, mocks: sdMocks}
