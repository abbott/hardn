package header_test

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/abbott/hardn/pkg/engine"
	"github.com/abbott/hardn/pkg/engine/header"
	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/hostfake"
	"github.com/abbott/hardn/pkg/output"
)

// Nothing here runs a real command or reads the machine: every host is
// hostfake's, with the kernel's files and ip's answer as each case says.
// The paths are the kernel's, written out rather than taken from the
// package, so that a wrong path in the code fails here. Synthetic values
// only: web-01.example.net, 192.0.2.0/24, 2001:db8::/32.
const (
	hostnameFile = "/proc/sys/kernel/hostname"
	uptimeFile   = "/proc/uptime"
	routeFile    = "/proc/net/route"
	route6File   = "/proc/net/ipv6_route"
	inet6File    = "/proc/net/if_inet6"
	releaseFile  = "/proc/sys/kernel/osrelease"
	versionFile  = "/proc/sys/kernel/version"
	requiredFile = "/var/run/reboot-required"
)

// dpkgFormat is detect's --showformat for dpkg-query --show.
const dpkgFormat = `--showformat=${Status}\t${Version}\n`

// installed is dpkg-query's answer for a package installed at version.
func installed(version string) hostfake.Response {
	return hostfake.Response{Stdout: "install ok installed\t" + version + "\n"}
}

// figmaUptime is Figma's "182 days, 2 hours, 26 minutes", as
// /proc/uptime prints it ("%lu.%02lu %lu.%02lu\n").
const (
	figmaUptime = "15733560.42 31000000.17\n"
	figmaDur    = 182*24*time.Hour + 2*time.Hour + 26*time.Minute + 420*time.Millisecond
)

// routes is /proc/net/route with lines after its header, each padded to
// 127 characters as the kernel pads them. The addresses are the __be32
// printed with %08X on a little-endian host: 192.0.2.1 is 010200C0.
func routes(lines ...string) string {
	pad := func(s string) string { return s + strings.Repeat(" ", max(0, 127-len(s))) + "\n" }
	out := pad("Iface\tDestination\tGateway \tFlags\tRefCnt\tUse\tMetric\tMask\t\tMTU\tWindow\tIRTT")
	for _, l := range lines {
		out += pad(l)
	}
	return out
}

const (
	defaultEth0  = "eth0\t00000000\t010200C0\t0003\t0\t0\t100\t00000000\t0\t0\t0" // default via 192.0.2.1 dev eth0 metric 100
	connectEth0  = "eth0\t000200C0\t00000000\t0001\t0\t0\t100\t00FFFFFF\t0\t0\t0" // 192.0.2.0/24 dev eth0
	defaultVmbr0 = "vmbr0\t00000000\t010200C0\t0003\t0\t0\t0\t00000000\t0\t0\t0"
	connectVmbr0 = "vmbr0\t000200C0\t00000000\t0001\t0\t0\t0\t00FFFFFF\t0\t0\t0"
)

// ip -4 addr show dev <interface>, as iproute2 and BusyBox print it.
const (
	ipEth0 = `2: eth0: <BROADCAST,MULTICAST,UP,LOWER_UP> mtu 1500 qdisc fq_codel state UP group default qlen 1000
    altname enp0s3
    inet 192.0.2.10/24 brd 192.0.2.255 scope global eth0
       valid_lft forever preferred_lft forever
`
	ipVmbr0 = `4: vmbr0: <BROADCAST,MULTICAST,UP,LOWER_UP> mtu 1500 qdisc noqueue state UP group default qlen 1000
    inet 192.0.2.30/24 scope global vmbr0
       valid_lft forever preferred_lft forever
`
	ipBusyBox = `2: eth0: <BROADCAST,MULTICAST,UP,LOWER_UP> mtu 1500 qdisc pfifo_fast qlen 1000
    inet 192.0.2.20/24 brd 192.0.2.255 scope global eth0
`
)

// /proc/net/ipv6_route lines: a default route learnt from a router
// advertisement (via fe80::1, metric 1024), the kernel's unreachable
// default on lo (rejecting), a prefix route, and the loopback's.
const (
	default6     = "00000000000000000000000000000000 00 00000000000000000000000000000000 00 fe800000000000000000000000000001 00000400 00000001 00000000 00450003     eth0"
	unreachable6 = "00000000000000000000000000000000 00 00000000000000000000000000000000 00 00000000000000000000000000000000 ffffffff 00000001 00000000 00200200       lo"
	prefix6      = "20010db8000100000000000000000000 40 00000000000000000000000000000000 00 00000000000000000000000000000000 00000100 00000001 00000000 00000001     eth0"
	loopback6    = "00000000000000000000000000000001 80 00000000000000000000000000000000 00 00000000000000000000000000000000 00000000 00000002 00000000 80200001       lo"
)

// /proc/net/if_inet6 lines ("%pi6 %02x %02x %02x %02x %8s"): a static
// global address (permanent), a link-local one, the loopback's.
const (
	static6    = "20010db8000100000000000000000010 02 40 00 80     eth0" // 2001:db8:1::10
	linkLocal6 = "fe80000000000000021122fffe334455 02 40 20 80     eth0"
	lo6        = "00000000000000000000000000000001 01 80 10 80       lo"
)

func lines(ls ...string) string { return strings.Join(ls, "\n") + "\n" }

// site is one test's host: files with their content, empty directories,
// ip's answers by interface, dpkg-query's by package, and paths a read of
// fails with a permission error, as for a user other than root.
type site struct {
	facts detect.Facts
	files map[string]string
	dirs  []string
	ip    map[string]hostfake.Response
	dpkg  map[string]hostfake.Response
	deny  []string
	// restarting is needs-restarting -r's answer (Rocky, AlmaLinux); nil
	// leaves it unscripted.
	restarting *hostfake.Response
}

// with returns s with path's content set.
func (s site) with(p, content string) site {
	s.files = maps.Clone(s.files)
	s.files[p] = content
	return s
}

// without returns s with nothing at or under p.
func (s site) without(p string) site {
	s.files = maps.Clone(s.files)
	maps.DeleteFunc(s.files, func(k, _ string) bool { return under(k, p) })
	s.dirs = slices.DeleteFunc(slices.Clone(s.dirs), func(d string) bool { return under(d, p) })
	return s
}

func under(k, p string) bool { return k == p || strings.HasPrefix(k, p+"/") }

func (s site) answer(iface string, r hostfake.Response) site {
	s.ip = maps.Clone(s.ip)
	s.ip[iface] = r
	return s
}

// dpkgAnswer returns s with dpkg-query's answer for pkg set.
func (s site) dpkgAnswer(pkg string, r hostfake.Response) site {
	s.dpkg = maps.Clone(s.dpkg)
	if s.dpkg == nil {
		s.dpkg = map[string]hostfake.Response{}
	}
	s.dpkg[pkg] = r
	return s
}

// restartingAnswer returns s with needs-restarting -r's answer set.
func (s site) restartingAnswer(r hostfake.Response) site {
	s.restarting = &r
	return s
}

func (s site) denying(paths ...string) site {
	s.deny = append(slices.Clone(s.deny), paths...)
	return s
}

func (s site) build() (host.System, *hostfake.System) {
	f := hostfake.New(host.Options{})
	for p, c := range s.files {
		f.AddFile(p, []byte(c), 0o444, 0, 0)
	}
	for _, d := range s.dirs {
		f.AddDir(d, 0o755, 0, 0)
	}
	for iface, r := range s.ip {
		f.Respond(r, "ip", "-4", "addr", "show", "dev", iface)
	}
	for pkg, r := range s.dpkg {
		f.Respond(r, "dpkg-query", "--show", dpkgFormat, pkg)
	}
	if s.restarting != nil {
		f.Respond(*s.restarting, header.NeedsRestarting[0], header.NeedsRestarting[1:]...)
	}
	return denied{System: f, paths: s.deny}, f
}

// The platforms, each with a stock host of it.
func debian13() site {
	return site{
		facts: detect.Facts{
			OS:        detect.OSRelease{ID: "debian", VersionID: "13", VersionCodename: "trixie", PrettyName: "Debian GNU/Linux 13 (trixie)"},
			Platform:  detect.Platform{Distro: detect.DistroDebian, Version: "13", Support: detect.Supported},
			Init:      detect.Init{Kind: detect.Systemd, Running: detect.Yes},
			Proxmox:   detect.Proxmox{Installed: detect.No},
			Container: detect.Container{Detected: detect.No},
		},
		files: map[string]string{
			hostnameFile:                           "web-01.example.net\n",
			uptimeFile:                             figmaUptime,
			routeFile:                              routes(defaultEth0, connectEth0),
			route6File:                             lines(default6, prefix6, unreachable6, loopback6),
			inet6File:                              lines(static6, linkLocal6, lo6),
			releaseFile:                            "6.12.43+deb13-amd64\n",
			versionFile:                            "#1 SMP PREEMPT_DYNAMIC Debian 6.12.43-1 (2025-08-27)\n",
			"/boot/vmlinuz-6.12.41+deb13-amd64":    "",
			"/boot/vmlinuz-6.12.43+deb13-amd64":    "",
			"/boot/initrd.img-6.12.43+deb13-amd64": "",
			"/boot/config-6.12.43+deb13-amd64":     "",
		},
		dirs: []string{"/boot/grub"},
		ip:   map[string]hostfake.Response{"eth0": {Stdout: ipEth0}},
		dpkg: map[string]hostfake.Response{debianPackage: installed("6.12.43-1")},
	}
}

// debianPackage is debian13's running kernel's package.
const debianPackage = "linux-image-6.12.43+deb13-amd64"

// ubuntu2604 has IPv6 alone routed: a SLAAC address, a temporary one and
// a static one; a newer kernel installed, and reboot-required written.
func ubuntu2604() site {
	s := debian13()
	s.facts.OS = detect.OSRelease{ID: "ubuntu", IDLike: []string{"debian"}, VersionID: "26.04", VersionCodename: "resolute", PrettyName: "Ubuntu 26.04 LTS"}
	s.facts.Platform = detect.Platform{Distro: detect.DistroUbuntu, Version: "26.04", Support: detect.Supported}
	s = s.without("/boot").without(routeFile).without(inet6File).without(releaseFile)
	s.files[routeFile] = routes(connectEth0)
	s.files[inet6File] = lines(
		"20010db800010000021122fffe334455 02 40 00 00     eth0", // SLAAC, dynamic
		"20010db8000100005a1b2c3d4e5f6071 02 40 00 01     eth0", // temporary
		"20010db8000200000000000000000020 02 40 00 80     eth0", // static: 2001:db8:2::20
		linkLocal6, lo6)
	s.files[releaseFile] = "6.17.0-14-generic\n"
	s.files[versionFile] = "#14-Ubuntu SMP PREEMPT_DYNAMIC Fri Jan  9 10:00:00 UTC 2026\n"
	s.files["/boot/vmlinuz-6.17.0-14-generic"] = ""
	s.files["/boot/vmlinuz-6.17.0-19-generic"] = ""
	s.files[requiredFile] = "*** System restart required ***\n"
	s.ip, s.dpkg = nil, nil
	return s
}

func pve9() site {
	s := debian13()
	s.facts.Platform = detect.Platform{Distro: detect.DistroProxmox, Version: "9", Support: detect.Supported}
	s.facts.Proxmox = detect.Proxmox{Installed: detect.Yes, Version: "9.0.10", ClusterFS: detect.Yes, EtcPVE: detect.Yes}
	s = s.without("/boot").without(releaseFile).without(route6File).without(inet6File)
	s.files[routeFile] = routes(defaultVmbr0, connectVmbr0)
	s.files[route6File] = lines(unreachable6, loopback6)
	s.files[inet6File] = lines(lo6)
	s.files[releaseFile] = "6.17.2-1-pve\n"
	s.files[versionFile] = "#1 SMP PREEMPT_DYNAMIC PMX 6.17.2-1 (2025-10-21T11:55Z)\n"
	s.files["/boot/vmlinuz-6.14.8-2-pve"] = ""
	s.files["/boot/vmlinuz-6.17.2-1-pve"] = ""
	s.ip, s.dpkg = map[string]hostfake.Response{"vmbr0": {Stdout: ipVmbr0}}, nil
	return s
}

// alpine324 runs IPv6 off (no IPv6 files) and BusyBox's ip.
func alpine324() site {
	s := debian13()
	s.facts.OS = detect.OSRelease{ID: "alpine", VersionID: "3.24.0", PrettyName: "Alpine Linux v3.24"}
	s.facts.Platform = detect.Platform{Distro: detect.DistroAlpine, Version: "3.24", Support: detect.Supported}
	s.facts.Init = detect.Init{Kind: detect.OpenRC, Running: detect.Yes}
	s.facts.Proxmox = detect.Proxmox{}
	s = s.without("/boot").without(releaseFile).without(versionFile).without(route6File).without(inet6File)
	s.files[releaseFile] = "6.12.40-0-lts\n"
	s.files["/boot/vmlinuz-lts"] = ""
	s.dirs = append(s.dirs, "/lib/modules/6.12.40-0-lts/kernel")
	s.ip, s.dpkg = map[string]hostfake.Response{"eth0": {Stdout: ipBusyBox}}, nil
	return s
}

// rebootLines is needs-restarting -r's output when a reboot is needed
// (needs_restarting.py's messages, read on rocky10-test-110).
const rebootLines = "Core libraries or services have been updated since boot-up:\n  * kernel\n\nReboot is required to fully utilize these updates.\n"

// rocky102 is rocky10-test-110: Rocky Linux 10.2, systemd, the kernel the
// probe read, no reboot pending (needs-restarting -r exit 0).
func rocky102() site {
	s := debian13()
	s.facts.OS = detect.OSRelease{ID: "rocky", IDLike: []string{"rhel", "centos", "fedora"}, VersionID: "10.2", PrettyName: "Rocky Linux 10.2 (Red Quartz)"}
	s.facts.Platform = detect.Platform{Distro: detect.DistroRocky, Version: "10", Support: detect.Unlisted}
	s.facts.Proxmox = detect.Proxmox{Installed: detect.No}
	s = s.without("/boot").without(releaseFile).without(versionFile)
	s.files[releaseFile] = "6.12.0-211.16.1.el10_2.0.1.x86_64\n"
	s.files["/boot/vmlinuz-6.12.0-211.16.1.el10_2.0.1.x86_64"] = ""
	s.dpkg = nil
	return s.restartingAnswer(hostfake.Response{Stdout: "No core libraries or services have been updated since boot-up.\nReboot should not be necessary.\n"})
}

// container is Debian 13 in an LXC container: no kernel of its own, no
// iproute2, the uptime LXCFS gives it (one hour).
func container() site {
	s := debian13()
	s.facts.Container = detect.Container{Detected: detect.Yes, Signal: detect.InitEnviron, Manager: "lxc"}
	s = s.without("/boot").without(releaseFile).without(versionFile).with(hostnameFile, "web-01\n").with(uptimeFile, "3600.00 3590.00\n")
	s.ip = map[string]hostfake.Response{"eth0": {Err: fmt.Errorf("host: ip: %w", host.ErrNotFound)}}
	s.dpkg = nil
	return s
}

// denied is a fake on which reads of paths fail with a permission error.
type denied struct {
	*hostfake.System
	paths []string
}

func (d denied) refuse(op, name string) error {
	if slices.Contains(d.paths, name) {
		return &fs.PathError{Op: op, Path: name, Err: fs.ErrPermission}
	}
	return nil
}

func (d denied) ReadFile(ctx context.Context, name string) ([]byte, error) {
	if err := d.refuse("open", name); err != nil {
		return nil, err
	}
	return d.System.ReadFile(ctx, name)
}

func (d denied) Stat(ctx context.Context, name string) (fs.FileInfo, error) {
	if err := d.refuse("stat", name); err != nil {
		return nil, err
	}
	return d.System.Stat(ctx, name)
}

func (d denied) ReadDir(ctx context.Context, name string) ([]fs.DirEntry, error) {
	if err := d.refuse("open", name); err != nil {
		return nil, err
	}
	return d.System.ReadDir(ctx, name)
}

// readOnly fails the test for any call that is not a read, or a run of
// anything but ip -4 addr show dev <interface> and dpkg-query --show of a
// kernel's package.
func readOnly(t *testing.T, f *hostfake.System) {
	t.Helper()
	for _, c := range f.Calls() {
		switch c.Op {
		case "read", "stat", "readdir":
		case "run":
			ip := len(c.Argv) == 6 && strings.Join(c.Argv[:5], " ") == "ip -4 addr show dev"
			dpkg := len(c.Argv) == 4 && c.Argv[0] == "dpkg-query" && c.Argv[1] == "--show" && c.Argv[2] == dpkgFormat &&
				strings.HasPrefix(c.Argv[3], "linux-image-")
			restarting := slices.Equal(c.Argv, header.NeedsRestarting)
			if !ip && !dpkg && !restarting {
				t.Errorf("ran %q", strings.Join(c.Argv, " "))
			}
		default:
			t.Errorf("a call that is not a read: %v", c)
		}
	}
}

// read probes s and checks that it changed nothing.
func read(t *testing.T, s site) header.Header {
	t.Helper()
	sys, f := s.build()
	h := header.Read(context.Background(), sys, s.facts)
	readOnly(t, f)
	return h
}

// fam renders a Family for comparison: "eth0 192.0.2.10", "eth0 none"
// (a route, no address), "no route", "unknown" (the route not
// established), and the error's code after the rest.
func fam(f header.Family) string {
	var s string
	switch {
	case f.Route == detect.No:
		s = "no route"
	case f.Route == detect.Unknown:
		s = "unknown"
	case f.Addr.IsValid():
		s = f.Iface + " " + f.Addr.String()
	default:
		s = f.Iface + " none"
	}
	if f.Err != nil {
		s += " " + output.CodeOf(f.Err)
	}
	return s
}

// primary renders Primary for comparison: the address, "none", or
// "unknown".
func primary(a header.Address) string {
	p, known := a.Primary()
	switch {
	case !known:
		return "unknown"
	case !p.IsValid():
		return "none"
	}
	return p.String()
}

// consistent fails the test for a Family Read could not return: a route
// not established with no error, or none with an interface or an error.
func consistent(t *testing.T, name string, f header.Family) {
	t.Helper()
	switch {
	case f.Route == detect.Unknown && (f.Err == nil || f.Iface != "" || f.Addr.IsValid()),
		f.Route == detect.No && (f.Err != nil || f.Iface != "" || f.Addr.IsValid()),
		f.Route == detect.Yes && (f.Iface == "" || f.Err != nil && f.Addr.IsValid()):
		t.Errorf("%s: %+v is not a family Read returns", name, f)
	}
}

func codes(fs []engine.Finding) []string {
	var out []string
	for _, f := range fs {
		out = append(out, f.Code)
	}
	return out
}

// TestRead: each platform's stock host, every fact.
func TestRead(t *testing.T) {
	for _, c := range []struct {
		name       string
		site       site
		os         header.OS
		hostname   string
		v4, v6     string
		primary    string
		uptime     time.Duration
		pending    detect.Tri
		running    string
		hostKernel bool
		codes      []string
		evidence   []string
		err        string // the reboot's error's code
	}{
		{name: "debian13", site: debian13(), os: header.OS{Name: "Debian", Version: "13", Codename: "trixie"}, hostname: "web-01.example.net",
			v4: "eth0 192.0.2.10", v6: "eth0 2001:db8:1::10", primary: "192.0.2.10", uptime: figmaDur, pending: detect.No, running: "6.12.43+deb13-amd64",
			evidence: []string{requiredFile + ": absent", releaseFile + ": 6.12.43+deb13-amd64", "/boot: newest vmlinuz-6.12.43+deb13-amd64",
				versionFile + ": Debian 6.12.43-1", "dpkg: " + debianPackage + " 6.12.43-1"}},
		{name: "ubuntu2604", site: ubuntu2604(), os: header.OS{Name: "Ubuntu", Version: "26.04", Codename: "resolute"}, hostname: "web-01.example.net",
			v4: "no route", v6: "eth0 2001:db8:2::20", primary: "2001:db8:2::20", uptime: figmaDur, pending: detect.Yes, running: "6.17.0-14-generic",
			codes:    []string{header.CodeRebootRequired, header.CodeNewerKernel},
			evidence: []string{requiredFile + ": present", releaseFile + ": 6.17.0-14-generic", "/boot: newest vmlinuz-6.17.0-19-generic"}},
		{name: "pve9", site: pve9(), os: header.OS{Name: "Proxmox VE", Version: "9.0.10", Codename: "trixie"}, hostname: "web-01.example.net",
			v4: "vmbr0 192.0.2.30", v6: "no route", primary: "192.0.2.30", uptime: figmaDur, pending: detect.No, running: "6.17.2-1-pve",
			evidence: []string{requiredFile + ": absent", releaseFile + ": 6.17.2-1-pve", "/boot: newest vmlinuz-6.17.2-1-pve"}},
		{name: "alpine324", site: alpine324(), os: header.OS{Name: "Alpine", Version: "3.24.0"}, hostname: "web-01.example.net",
			v4: "eth0 192.0.2.20", v6: "no route", primary: "192.0.2.20", uptime: figmaDur, pending: detect.No, running: "6.12.40-0-lts",
			evidence: []string{releaseFile + ": 6.12.40-0-lts", "/lib/modules/6.12.40-0-lts/kernel: present"}},
		{name: "container", site: container(), os: header.OS{Name: "Debian", Version: "13", Codename: "trixie"}, hostname: "web-01",
			v4: "eth0 none COMMAND_NOT_FOUND", v6: "eth0 2001:db8:1::10", primary: "unknown", uptime: time.Hour, pending: detect.No, hostKernel: true,
			evidence: []string{requiredFile + ": absent", "kernel: the host's (container)"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			h := read(t, c.site)
			if h.OS != c.os {
				t.Errorf("os = %+v, want %+v", h.OS, c.os)
			}
			if h.Hostname.Name != c.hostname || h.Hostname.Err != nil {
				t.Errorf("hostname = %+v, want %q", h.Hostname, c.hostname)
			}
			if got := fam(h.Address.IPv4); got != c.v4 {
				t.Errorf("ipv4 = %s, want %s", got, c.v4)
			}
			if got := fam(h.Address.IPv6); got != c.v6 {
				t.Errorf("ipv6 = %s, want %s", got, c.v6)
			}
			if got := primary(h.Address); got != c.primary {
				t.Errorf("primary = %s, want %s", got, c.primary)
			}
			if h.Uptime.Err != nil || !h.Uptime.Known || h.Uptime.Duration.Round(time.Millisecond) != c.uptime {
				t.Errorf("uptime = %+v, want %v", h.Uptime, c.uptime)
			}
			r := h.Reboot
			if r.HostKernel != c.hostKernel {
				t.Errorf("host kernel = %v, want %v", r.HostKernel, c.hostKernel)
			}
			if r.Pending != c.pending || r.Running != c.running || !slices.Equal(codes(r.Findings), c.codes) || output.CodeOf(r.Err) != c.err {
				t.Errorf("reboot = %v %q %v %v, want %v %q %v %q", r.Pending, r.Running, codes(r.Findings), r.Err, c.pending, c.running, c.codes, c.err)
			}
			if !slices.Equal(r.Evidence, c.evidence) {
				t.Errorf("evidence:\n%s\nwant:\n%s", strings.Join(r.Evidence, "\n"), strings.Join(c.evidence, "\n"))
			}
		})
	}
}

// TestOS: the name, version and codename from detect's facts alone.
func TestOS(t *testing.T) {
	notRead := detect.OSRelease{Err: fmt.Errorf("detect: os-release: %w", fs.ErrPermission)}
	for _, c := range []struct {
		name string
		f    detect.Facts
		want header.OS
		code string
	}{
		{name: "os-release not read", f: detect.Facts{OS: notRead}, code: "PERMISSION_DENIED"},
		{name: "no facts", f: detect.Facts{}, code: "DETECT_NO_SIGNAL"},
		{name: "Debian, Proxmox VE not established", f: detect.Facts{OS: debian13().facts.OS, Proxmox: detect.Proxmox{Err: detect.ErrNoSignal}},
			want: header.OS{Name: "Debian", Version: "13", Codename: "trixie"}},
		{name: "Proxmox VE, version not read", f: detect.Facts{OS: debian13().facts.OS, Proxmox: detect.Proxmox{Installed: detect.Yes}},
			want: header.OS{Name: "Proxmox VE", Codename: "trixie"}},
		{name: "Debian testing", f: detect.Facts{OS: detect.OSRelease{ID: "debian", VersionCodename: "forky"}}, want: header.OS{Name: "Debian", Codename: "forky"}},
		{name: "a derivative", f: detect.Facts{OS: detect.OSRelease{ID: "linuxmint", IDLike: []string{"ubuntu"}, VersionID: "22.1", VersionCodename: "xia", PrettyName: "Linux Mint 22.1"}},
			want: header.OS{Name: "Linux Mint 22.1"}},
		{name: "no PRETTY_NAME", f: detect.Facts{OS: detect.OSRelease{ID: "fedora", VersionID: "43"}}, want: header.OS{Name: "fedora"}},
		{name: "Rocky Linux 10.2", f: rocky102().facts, want: header.OS{Name: "Rocky Linux", Version: "10.2"}},
		{name: "AlmaLinux 10.1", f: detect.Facts{OS: detect.OSRelease{ID: "almalinux", VersionID: "10.1", PrettyName: "AlmaLinux 10.1"}},
			want: header.OS{Name: "AlmaLinux", Version: "10.1"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			s := debian13()
			s.facts = c.f
			got := read(t, s).OS
			if output.CodeOf(got.Err) != c.code {
				t.Errorf("err = %v, want %s", got.Err, c.code)
			}
			got.Err = nil
			if got != c.want {
				t.Errorf("os = %+v, want %+v", got, c.want)
			}
		})
	}
}

// TestHostnameAndUptime: what is read, and every reason either is not
// established.
func TestHostnameAndUptime(t *testing.T) {
	for _, c := range []struct {
		name     string
		site     site
		hostname string
		hostErr  string
		uptime   time.Duration
		upErr    string
	}{
		{name: "stock", site: debian13(), hostname: "web-01.example.net", uptime: figmaDur},
		{name: "just booted", site: debian13().with(uptimeFile, "0.00 0.00\n"), hostname: "web-01.example.net"},
		{name: "a whole number", site: debian13().with(uptimeFile, "60 59\n"), hostname: "web-01.example.net", uptime: time.Minute},
		{name: "unreadable", site: debian13().denying(hostnameFile, uptimeFile), hostErr: "PERMISSION_DENIED", upErr: "PERMISSION_DENIED"},
		{name: "absent", site: debian13().without(hostnameFile).without(uptimeFile), hostErr: "FILE_NOT_FOUND", upErr: "FILE_NOT_FOUND"},
		{name: "empty", site: debian13().with(hostnameFile, "\n").with(uptimeFile, ""), hostErr: "DETECT_UNRECOGNISED", upErr: "DETECT_UNRECOGNISED"},
		{name: "one number", site: debian13().with(uptimeFile, "350735.47\n"), hostname: "web-01.example.net", upErr: "DETECT_UNRECOGNISED"},
		{name: "negative", site: debian13().with(uptimeFile, "-1.00 2.00\n"), hostname: "web-01.example.net", upErr: "DETECT_UNRECOGNISED"},
		{name: "exponent", site: debian13().with(uptimeFile, "1e5 2.00\n"), hostname: "web-01.example.net", upErr: "DETECT_UNRECOGNISED"},
		{name: "NaN", site: debian13().with(uptimeFile, "NaN 2.00\n"), hostname: "web-01.example.net", upErr: "DETECT_UNRECOGNISED"},
		{name: "a point alone", site: debian13().with(uptimeFile, "12. 2.00\n"), hostname: "web-01.example.net", upErr: "DETECT_UNRECOGNISED"},
		{name: "past a Duration", site: debian13().with(uptimeFile, "9300000000.00 2.00\n"), hostname: "web-01.example.net", upErr: "DETECT_UNRECOGNISED"},
	} {
		t.Run(c.name, func(t *testing.T) {
			h := read(t, c.site)
			if h.Hostname.Name != c.hostname || output.CodeOf(h.Hostname.Err) != c.hostErr {
				t.Errorf("hostname = %+v, want %q %s", h.Hostname, c.hostname, c.hostErr)
			}
			if h.Uptime.Duration.Round(time.Millisecond) != c.uptime || output.CodeOf(h.Uptime.Err) != c.upErr || h.Uptime.Known != (c.upErr == "") {
				t.Errorf("uptime = %+v, want %v %s", h.Uptime, c.uptime, c.upErr)
			}
		})
	}
}

// TestAddress: the default route in each family, and the primary
// address of its interface.
func TestAddress(t *testing.T) {
	eth1 := "eth1\t00000000\t016433C6\t0003\t0\t0\t50\t00000000\t0\t0\t0"  // default via 198.51.100.1 dev eth1 metric 50
	tie := "eth1\t00000000\t016433C6\t0003\t0\t0\t100\t00000000\t0\t0\t0"  // the same metric as eth0's
	same := "eth0\t00000000\t020200C0\t0003\t0\t0\t100\t00000000\t0\t0\t0" // a second gateway, 192.0.2.2, on eth0
	reject := "eth0\t00000000\t00000000\t0201\t0\t0\t0\t00000000\t0\t0\t0" // unreachable default
	blackhole := "*\t00000000\t00000000\t0001\t0\t0\t0\t00000000\t0\t0\t0"
	unreachable := "*\t00000000\t00000000\t0201\t0\t0\t%d\t00000000\t0\t0\t0" // unreachable default metric %d, as the kernel lists it
	noGateway := "ppp0\t00000000\t00000000\t0001\t0\t0\t0\t00000000\t0\t0\t0" // default dev ppp0
	ip := func(body string) hostfake.Response {
		return hostfake.Response{Stdout: "2: eth0: <BROADCAST,MULTICAST,UP,LOWER_UP> mtu 1500\n" + body}
	}
	slaac6 := "20010db800010000021122fffe334455 02 40 00 00     eth0"
	tentative6 := "20010db8000100000000000000000005 02 40 00 c0     eth0"
	deprecated6 := "20010db8000100000000000000000006 02 40 00 a0     eth0"
	ula6 := "fd000000000000000000000000000010 02 40 00 80     eth0" // fd00::10, static
	for _, c := range []struct {
		name         string
		site         site
		v4, v6, prim string // prim: the primary address, "none", or "unknown"
	}{
		{name: "stock", site: debian13(), v4: "eth0 192.0.2.10", v6: "eth0 2001:db8:1::10", prim: "192.0.2.10"},
		{name: "the lower metric wins", site: debian13().with(routeFile, routes(defaultEth0, eth1)).
			answer("eth1", hostfake.Response{Stdout: "    inet 198.51.100.7/24 brd 198.51.100.255 scope global eth1\n"}),
			v4: "eth1 198.51.100.7", v6: "eth0 2001:db8:1::10", prim: "198.51.100.7"},
		{name: "two interfaces at one metric", site: debian13().with(routeFile, routes(defaultEth0, tie)),
			v4: "unknown DETECT_SIGNALS_DISAGREE", v6: "eth0 2001:db8:1::10", prim: "unknown"},
		{name: "two gateways on one interface", site: debian13().with(routeFile, routes(defaultEth0, same)), v4: "eth0 192.0.2.10", v6: "eth0 2001:db8:1::10", prim: "192.0.2.10"},
		{name: "the address in the gateway's network", site: debian13().answer("eth0", ip(
			"    inet 198.51.100.5/24 scope global eth0\n    inet 192.0.2.10/24 brd 192.0.2.255 scope global eth0\n    inet 192.0.2.11/24 scope global secondary eth0\n")),
			v4: "eth0 192.0.2.10", v6: "eth0 2001:db8:1::10", prim: "192.0.2.10"},
		{name: "secondary, link scope, tentative skipped", site: debian13().answer("eth0", ip(
			"    inet 192.0.2.11/24 scope global secondary eth0\n    inet 169.254.1.1/16 scope link eth0\n    inet 192.0.2.12/24 scope global tentative eth0\n    inet 198.51.100.9/24 scope global dynamic noprefixroute eth0\n")),
			v4: "eth0 198.51.100.9", v6: "eth0 2001:db8:1::10", prim: "198.51.100.9"},
		{name: "a /32 with an on-link gateway", site: debian13().answer("eth0", ip("    inet 192.0.2.50/32 scope global eth0\n")),
			v4: "eth0 192.0.2.50", v6: "eth0 2001:db8:1::10", prim: "192.0.2.50"},
		{name: "a point-to-point link", site: debian13().with(routeFile, routes(noGateway)).
			answer("ppp0", hostfake.Response{Stdout: "    inet 192.0.2.60 peer 192.0.2.61/32 scope global ppp0\n"}),
			v4: "ppp0 192.0.2.60", v6: "eth0 2001:db8:1::10", prim: "192.0.2.60"},
		{name: "no global IPv4 address", site: debian13().answer("eth0", ip("    inet 169.254.1.1/16 scope link eth0\n")),
			v4: "eth0 none", v6: "eth0 2001:db8:1::10", prim: "2001:db8:1::10"},
		{name: "ip exits 1", site: debian13().answer("eth0", hostfake.Response{Stderr: "Device \"eth0\" does not exist.\n", ExitCode: 1}),
			v4: "eth0 none COMMAND_FAILED", v6: "eth0 2001:db8:1::10", prim: "unknown"},
		{name: "ip's line in no form known", site: debian13().answer("eth0", ip("    inet 192.0.2.10/24 brd 192.0.2.255\n")),
			v4: "eth0 none DETECT_UNRECOGNISED", v6: "eth0 2001:db8:1::10", prim: "unknown"},
		{name: "ip's address in no form known", site: debian13().answer("eth0", ip("    inet 2001:db8::1/64 scope global eth0\n")),
			v4: "eth0 none DETECT_UNRECOGNISED", v6: "eth0 2001:db8:1::10", prim: "unknown"},
		{name: "ip's peer in no form known", site: debian13().with(routeFile, routes(noGateway)).
			answer("ppp0", hostfake.Response{Stdout: "    inet 192.0.2.60 peer bad/32 scope global ppp0\n"}),
			v4: "ppp0 none DETECT_UNRECOGNISED", v6: "eth0 2001:db8:1::10", prim: "unknown"},
		{name: "rejecting and blackhole defaults", site: debian13().with(routeFile, routes(reject, blackhole, connectEth0)),
			v4: "no route", v6: "eth0 2001:db8:1::10", prim: "2001:db8:1::10"},
		{name: "a rejecting default at a lower metric", site: debian13().with(routeFile, routes(fmt.Sprintf(unreachable, 0), defaultEth0, connectEth0)),
			v4: "no route", v6: "eth0 2001:db8:1::10", prim: "2001:db8:1::10"},
		{name: "a rejecting default at a higher metric", site: debian13().with(routeFile, routes(defaultEth0, fmt.Sprintf(unreachable, 200), connectEth0)),
			v4: "eth0 192.0.2.10", v6: "eth0 2001:db8:1::10", prim: "192.0.2.10"},
		{name: "a rejecting default at the same metric", site: debian13().with(routeFile, routes(defaultEth0, fmt.Sprintf(unreachable, 100))),
			v4: "unknown DETECT_SIGNALS_DISAGREE", v6: "eth0 2001:db8:1::10", prim: "unknown"},
		{name: "the route table unreadable", site: debian13().denying(routeFile), v4: "unknown PERMISSION_DENIED", v6: "eth0 2001:db8:1::10", prim: "unknown"},
		{name: "the route table in no form known", site: debian13().with(routeFile, routes("eth0\t00000000\tzz\t0003")), v4: "unknown DETECT_UNRECOGNISED", v6: "eth0 2001:db8:1::10", prim: "unknown"},
		{name: "a route's field in no form known", site: debian13().with(routeFile, routes("eth0\t00000000\t010200C0\t0003\t0\t0\tmany\t00000000\t0\t0\t0")),
			v4: "unknown DETECT_UNRECOGNISED", v6: "eth0 2001:db8:1::10", prim: "unknown"},
		{name: "the route table with no header", site: debian13().with(routeFile, ""), v4: "unknown DETECT_UNRECOGNISED", v6: "eth0 2001:db8:1::10", prim: "unknown"},
		{name: "IPv6 off", site: debian13().without(route6File).without(inet6File), v4: "eth0 192.0.2.10", v6: "no route", prim: "192.0.2.10"},
		{name: "no /proc/net", site: debian13().without("/proc/net"), v4: "unknown FILE_NOT_FOUND", v6: "unknown FILE_NOT_FOUND", prim: "unknown"},
		{name: "no default route", site: debian13().with(routeFile, routes(connectEth0)).with(route6File, lines(prefix6, unreachable6)), v4: "no route", v6: "no route", prim: "none"},
		{name: "IPv6: link-local alone", site: debian13().with(routeFile, routes()).with(inet6File, lines(linkLocal6, lo6)), v4: "no route", v6: "eth0 none", prim: "none"},
		{name: "IPv6: permanent before SLAAC, not tentative or deprecated", site: debian13().with(routeFile, routes()).
			with(inet6File, lines(tentative6, deprecated6, slaac6, "20010db8000900000000000000000001 02 40 00 80     eth0")),
			v4: "no route", v6: "eth0 2001:db8:9::1", prim: "2001:db8:9::1"},
		{name: "IPv6: the lowest of two dynamic", site: debian13().with(routeFile, routes()).
			with(inet6File, lines(slaac6, "20010db8000100000000000000000abc 02 40 00 00     eth0")),
			v4: "no route", v6: "eth0 2001:db8:1::abc", prim: "2001:db8:1::abc"},
		{name: "IPv6: a unique local address after a global one", site: debian13().with(routeFile, routes()).with(inet6File, lines(ula6, slaac6)),
			v4: "no route", v6: "eth0 2001:db8:1:0:211:22ff:fe33:4455", prim: "2001:db8:1:0:211:22ff:fe33:4455"},
		{name: "IPv6: a unique local address alone", site: debian13().with(routeFile, routes()).with(inet6File, lines(ula6, linkLocal6)),
			v4: "no route", v6: "eth0 fd00::10", prim: "fd00::10"},
		{name: "IPv6: two interfaces at one metric", site: debian13().with(route6File, lines(default6, strings.Replace(default6, "    eth0", "    eth1", 1))),
			v4: "eth0 192.0.2.10", v6: "unknown DETECT_SIGNALS_DISAGREE", prim: "192.0.2.10"},
		{name: "IPv6: the routes in no form known", site: debian13().with(route6File, "0000 00\n"), v4: "eth0 192.0.2.10", v6: "unknown DETECT_UNRECOGNISED", prim: "192.0.2.10"},
		{name: "IPv6: a default route with no device", site: debian13().with(route6File, lines(strings.Replace(default6, "     eth0", "", 1), default6)),
			v4: "eth0 192.0.2.10", v6: "eth0 2001:db8:1::10", prim: "192.0.2.10"},
		{name: "IPv6: a route's metric in no form known", site: debian13().with(route6File, lines(strings.Replace(default6, " 00000400 ", " 0000040z ", 1))),
			v4: "eth0 192.0.2.10", v6: "unknown DETECT_UNRECOGNISED", prim: "192.0.2.10"},
		{name: "IPv6: addresses unreadable", site: debian13().with(routeFile, routes()).denying(inet6File), v4: "no route", v6: "eth0 none PERMISSION_DENIED", prim: "unknown"},
		{name: "IPv6: addresses in no form known", site: debian13().with(routeFile, routes()).with(inet6File, "2001 02 40 00 80 eth0\n"), v4: "no route", v6: "eth0 none DETECT_UNRECOGNISED", prim: "unknown"},
	} {
		t.Run(c.name, func(t *testing.T) {
			a := read(t, c.site).Address
			if got := fam(a.IPv4); got != c.v4 {
				t.Errorf("ipv4 = %s, want %s", got, c.v4)
			}
			if got := fam(a.IPv6); got != c.v6 {
				t.Errorf("ipv6 = %s, want %s", got, c.v6)
			}
			consistent(t, "ipv4", a.IPv4)
			consistent(t, "ipv6", a.IPv6)
			if got := primary(a); got != c.prim {
				t.Errorf("primary = %s, want %s", got, c.prim)
			}
		})
	}
}

// TestNoIPWithoutARoute: ip runs only for an IPv4 default route's
// interface.
func TestNoIPWithoutARoute(t *testing.T) {
	s := debian13().with(routeFile, routes(connectEth0))
	sys, f := s.build()
	header.Read(context.Background(), sys, s.facts)
	for _, argv := range f.Runs() {
		if argv[0] == "ip" {
			t.Errorf("ran %v", argv)
		}
	}
}

// TestZeroFamily: a Family or an Address Read did not fill establishes
// nothing.
func TestZeroFamily(t *testing.T) {
	if (header.Family{}).Known() {
		t.Error("the zero Family is known")
	}
	if _, known := (header.Address{}).Primary(); known {
		t.Error("the zero Address's primary is known")
	}
}

// TestReboot: each source on each platform, and every reason the state
// is not established.
func TestReboot(t *testing.T) {
	deb := debian13()
	alp := alpine324()
	rocky := rocky102()
	rockyContainer := rocky.without("/boot").without(releaseFile)
	rockyContainer.facts.Container = detect.Container{Detected: detect.Yes, Signal: detect.DockerEnvFile, Manager: "docker"}
	alma := rocky
	alma.facts.OS = detect.OSRelease{ID: "almalinux", IDLike: []string{"rhel", "centos", "fedora"}, VersionID: "10.1"}
	alma.facts.Platform = detect.Platform{Distro: detect.DistroAlma, Version: "10", Support: detect.Unlisted}
	other := deb
	other.facts.OS = detect.OSRelease{ID: "fedora", VersionID: "43"}
	other.facts.Platform = detect.Platform{Distro: detect.DistroOther, Support: detect.Unlisted}
	noOS := deb
	noOS.facts.OS = detect.OSRelease{Err: fmt.Errorf("detect: os-release: %w", fs.ErrPermission)}
	noOS.facts.Platform = detect.Platform{Err: noOS.facts.OS.Err}
	derivative := other
	derivative.facts.OS = detect.OSRelease{ID: "linuxmint", IDLike: []string{"ubuntu", "debian"}}
	derivative.facts.Platform = detect.Platform{Distro: detect.DistroOther, Like: detect.DistroUbuntu, Support: detect.Unlisted}
	alpContainer := alp
	alpContainer.facts.Container = detect.Container{Detected: detect.Yes, Signal: detect.DockerEnvFile, Manager: "docker"}
	unknownContainer := deb
	unknownContainer.facts.Container = detect.Container{Err: detect.ErrNoSignal}
	newer := "/boot/vmlinuz-6.12.48+deb13-amd64"
	rebuilt := "dpkg-query --show " + debianPackage
	modules := func(dirs ...string) site { // alp with /lib/modules holding these directories alone
		s := alp.without("/lib/modules")
		for _, d := range dirs {
			s = s.with("/lib/modules/"+d+"/x.ko", "")
		}
		return s
	}
	for _, c := range []struct {
		name    string
		site    site
		pending detect.Tri
		codes   []string
		sources []string // the findings' sources
		err     string
	}{
		{name: "debian13: the newest running", site: deb, pending: detect.No},
		{name: "debian13: a newer kernel", site: deb.with(newer, ""), pending: detect.Yes, codes: []string{header.CodeNewerKernel}, sources: []string{newer}},
		{name: "debian13: reboot-required", site: deb.with(requiredFile, ""), pending: detect.Yes, codes: []string{header.CodeRebootRequired}, sources: []string{requiredFile}},
		{name: "debian13: both", site: deb.with(requiredFile, "").with(newer, ""), pending: detect.Yes,
			codes: []string{header.CodeRebootRequired, header.CodeNewerKernel}, sources: []string{requiredFile, newer}},
		{name: "debian13: the running kernel not in /boot, an older one there", site: deb.without("/boot").with("/boot/vmlinuz-6.12.41+deb13-amd64", ""),
			pending: detect.Unknown, err: "DETECT_NO_SIGNAL"},
		{name: "debian13: the running kernel not in /boot, a newer one there", site: deb.without("/boot").with(newer, ""), pending: detect.Yes,
			codes: []string{header.CodeNewerKernel}, sources: []string{newer}},
		{name: "debian13: a name that is no release", site: deb.without("/boot").with("/boot/vmlinuz-custom", ""), pending: detect.Unknown, err: "DETECT_NO_SIGNAL"},
		{name: "debian13: the running release rebuilt", site: deb.dpkgAnswer(debianPackage, installed("6.12.43-2")), pending: detect.Yes,
			codes: []string{header.CodeKernelReplaced}, sources: []string{rebuilt}},
		{name: "debian13: the running release's package older", site: deb.dpkgAnswer(debianPackage, installed("6.12.43-1~exp1")), pending: detect.Yes,
			codes: []string{header.CodeKernelReplaced}, sources: []string{rebuilt}},
		{name: "debian13: rebuilt, and a newer kernel", site: deb.with(newer, "").dpkgAnswer(debianPackage, installed("6.12.43-2")), pending: detect.Yes,
			codes: []string{header.CodeNewerKernel, header.CodeKernelReplaced}, sources: []string{newer, rebuilt}},
		{name: "debian13: the running kernel's package not installed", site: deb.dpkgAnswer(debianPackage, hostfake.Response{ExitCode: 1}),
			pending: detect.Unknown, err: "DETECT_NO_SIGNAL"},
		{name: "debian13: no dpkg-query", site: deb.dpkgAnswer(debianPackage, hostfake.Response{Err: fmt.Errorf("host: dpkg-query: %w", host.ErrNotFound)}),
			pending: detect.Unknown, err: "DETECT_NO_SIGNAL"},
		{name: "debian13: dpkg-query fails", site: deb.dpkgAnswer(debianPackage, hostfake.Response{ExitCode: 2}), pending: detect.Unknown, err: "COMMAND_FAILED"},
		{name: "debian13: the package half installed", site: deb.dpkgAnswer(debianPackage, hostfake.Response{Stdout: "install ok half-installed\t6.12.43-2\n"}),
			pending: detect.Unknown, err: "DETECT_UNRECOGNISED"},
		{name: "debian13: the build unreadable", site: deb.denying(versionFile), pending: detect.Unknown, err: "PERMISSION_DENIED"},
		{name: "debian13: the build's version in no form known", site: deb.with(versionFile, "#1 SMP Debian (2025-08-27)\n"), pending: detect.Unknown, err: "DETECT_UNRECOGNISED"},
		{name: "debian13: a kernel built otherwise", site: deb.with(versionFile, "#1 SMP PREEMPT_DYNAMIC Tue Oct 21 10:00:00 UTC 2026\n"), pending: detect.No},
		{name: "debian13: another flavour of the same version", site: deb.with("/boot/vmlinuz-6.12.43+deb13-cloud-amd64", ""), pending: detect.No},
		{name: "debian13: a newer kernel of another flavour", site: deb.with("/boot/vmlinuz-6.12.48+deb13-cloud-amd64", ""), pending: detect.Yes,
			codes: []string{header.CodeNewerKernel}, sources: []string{"/boot/vmlinuz-6.12.48+deb13-cloud-amd64"}},
		{name: "debian13: no /boot", site: deb.without("/boot"), pending: detect.Unknown, err: "DETECT_NO_SIGNAL"},
		{name: "debian13: no kernel in /boot", site: deb.without("/boot").with("/boot/grub/grub.cfg", "").with("/boot/vmlinuz", ""), pending: detect.Unknown, err: "DETECT_NO_SIGNAL"},
		{name: "debian13: no /boot, reboot-required", site: deb.without("/boot").with(requiredFile, ""), pending: detect.Yes,
			codes: []string{header.CodeRebootRequired}, sources: []string{requiredFile}, err: "DETECT_NO_SIGNAL"},
		{name: "debian13: /boot unreadable", site: deb.denying("/boot"), pending: detect.Unknown, err: "PERMISSION_DENIED"},
		{name: "debian13: the release unreadable", site: deb.denying(releaseFile), pending: detect.Unknown, err: "PERMISSION_DENIED"},
		{name: "debian13: the release empty", site: deb.with(releaseFile, "\n"), pending: detect.Unknown, err: "DETECT_UNRECOGNISED"},
		{name: "debian13: the release no version", site: deb.with(releaseFile, "custom-amd64\n").with("/boot/vmlinuz-custom-amd64", ""),
			pending: detect.Unknown, err: "DETECT_UNRECOGNISED"},
		{name: "debian13: reboot-required unreadable", site: deb.denying(requiredFile), pending: detect.Unknown, err: "PERMISSION_DENIED"},
		{name: "debian13: reboot-required unreadable, a newer kernel", site: deb.denying(requiredFile).with(newer, ""), pending: detect.Yes,
			codes: []string{header.CodeNewerKernel}, sources: []string{newer}, err: "PERMISSION_DENIED"},
		{name: "debian13: container not established", site: unknownContainer.with(newer, ""), pending: detect.Yes, codes: []string{header.CodeNewerKernel}, sources: []string{newer}},
		{name: "ubuntu2604", site: ubuntu2604(), pending: detect.Yes, codes: []string{header.CodeRebootRequired, header.CodeNewerKernel},
			sources: []string{requiredFile, "/boot/vmlinuz-6.17.0-19-generic"}},
		{name: "pve9", site: pve9(), pending: detect.No},
		{name: "a derivative of Ubuntu", site: derivative, pending: detect.No},
		{name: "alpine324: modules present", site: alp, pending: detect.No},
		{name: "alpine324: the running kernel's modules gone", site: modules("6.12.45-0-lts/kernel"), pending: detect.Yes,
			codes: []string{header.CodeModulesMissing}, sources: []string{"/lib/modules/6.12.40-0-lts"}},
		{name: "alpine324: an older kernel's modules alone", site: modules("6.12.30-0-lts/kernel"), pending: detect.Yes,
			codes: []string{header.CodeModulesMissing}, sources: []string{"/lib/modules/6.12.40-0-lts"}},
		{name: "alpine324: the running release's directory left behind", site: modules("6.12.40-0-lts/extra", "6.12.45-0-lts/kernel"), pending: detect.Yes,
			codes: []string{header.CodeModulesMissing}, sources: []string{"/lib/modules/6.12.40-0-lts"}},
		{name: "alpine324: directories left behind alone", site: modules("6.12.40-0-lts/extra", "6.12.45-0-lts"), pending: detect.Unknown, err: "DETECT_NO_SIGNAL"},
		{name: "alpine324: kernel/ a file", site: alp.without("/lib/modules").with("/lib/modules/6.12.40-0-lts/kernel", ""), pending: detect.Unknown, err: "DETECT_UNRECOGNISED"},
		{name: "alpine324: reboot-required is not Alpine's", site: alp.with(requiredFile, ""), pending: detect.No},
		{name: "alpine324: no /lib/modules", site: alp.without("/lib/modules"), pending: detect.Unknown, err: "DETECT_NO_SIGNAL"},
		{name: "alpine324: no kernel's modules", site: alp.without("/lib/modules").with("/lib/modules/README", ""), pending: detect.Unknown, err: "DETECT_NO_SIGNAL"},
		{name: "alpine324: the modules a file", site: alp.without("/lib/modules").with("/lib/modules/6.12.40-0-lts", ""), pending: detect.Unknown, err: "DETECT_UNRECOGNISED"},
		{name: "alpine324: modules unreadable", site: alp.denying("/lib/modules/6.12.40-0-lts"), pending: detect.Unknown, err: "PERMISSION_DENIED"},
		{name: "alpine324: the list unreadable", site: modules("6.12.45-0-lts/kernel").denying("/lib/modules"), pending: detect.Unknown, err: "PERMISSION_DENIED"},
		{name: "alpine324: another kernel's unreadable", site: modules("6.12.45-0-lts/kernel").denying("/lib/modules/6.12.45-0-lts"), pending: detect.Unknown, err: "PERMISSION_DENIED"},
		{name: "alpine324: one kernel's unreadable, another's there", site: modules("6.12.30-0-lts/kernel", "6.12.45-0-lts/kernel").denying("/lib/modules/6.12.30-0-lts"),
			pending: detect.Yes, codes: []string{header.CodeModulesMissing}, sources: []string{"/lib/modules/6.12.40-0-lts"}},
		{name: "container", site: container(), pending: detect.No},
		{name: "container: reboot-required", site: container().with(requiredFile, ""), pending: detect.Yes, codes: []string{header.CodeRebootRequired}, sources: []string{requiredFile}},
		{name: "container: Alpine", site: alpContainer.without("/lib/modules"), pending: detect.Unknown, err: "DETECT_NO_SIGNAL"},
		{name: "rocky102: needs-restarting exit 0", site: rocky, pending: detect.No},
		{name: "rocky102: needs-restarting exit 1", site: rocky.restartingAnswer(hostfake.Response{Stdout: rebootLines, ExitCode: 1}),
			pending: detect.Yes, codes: []string{header.CodeRebootRequired}, sources: []string{"dnf needs-restarting -r"}},
		{name: "rocky102: exit 1 without the reboot line is dnf's error", site: rocky.restartingAnswer(hostfake.Response{Stderr: "Error: rpmdb open failed\n", ExitCode: 1}),
			pending: detect.Unknown, err: "DETECT_UNRECOGNISED"},
		{name: "rocky102: needs-restarting fails to start", site: rocky.restartingAnswer(hostfake.Response{Err: errors.New("fork/exec: resource temporarily unavailable")}),
			pending: detect.Unknown, err: "UNCLASSIFIED_ERROR"},
		{name: "rocky102 in a container: needs-restarting alone", site: rockyContainer.restartingAnswer(hostfake.Response{Stdout: rebootLines, ExitCode: 1}),
			pending: detect.Yes, codes: []string{header.CodeRebootRequired}, sources: []string{"dnf needs-restarting -r"}},
		{name: "rocky102 in a container, no reboot", site: rockyContainer, pending: detect.No},
		{name: "rocky102: needs-restarting exit 2", site: rocky.restartingAnswer(hostfake.Response{ExitCode: 2}), pending: detect.Unknown, err: "DETECT_UNRECOGNISED"},
		{name: "rocky102: another dnf holds the lock", site: rocky.restartingAnswer(hostfake.Response{ExitCode: 200}), pending: detect.Unknown, err: "DETECT_UNRECOGNISED"},
		{name: "rocky102: no dnf", site: rocky.restartingAnswer(hostfake.Response{Err: fmt.Errorf("host: dnf: %w", host.ErrNotFound)}),
			pending: detect.Unknown, err: "DETECT_NO_SIGNAL"},
		// dnf 4.14's and 4.20's words with the plugin disabled, read on
		// alma9-test-111 and alma10-test-112.
		{name: "rocky102: dnf without needs-restarting", site: rocky.restartingAnswer(hostfake.Response{ExitCode: 1,
			Stderr: "No such command: needs-restarting. Please use /usr/bin/dnf --help\nIt could be a DNF plugin command, try: \"dnf install 'dnf-command(needs-restarting)'\"\n"}),
			pending: detect.Unknown, err: "DETECT_NO_SIGNAL"},
		{name: "rocky102: reboot-required is not Rocky's", site: rocky.with(requiredFile, ""), pending: detect.No},
		{name: "rocky102: a newer kernel in /boot is needs-restarting's to say", site: rocky.with("/boot/vmlinuz-6.12.0-230.1.1.el10_2.x86_64", ""), pending: detect.No},
		{name: "AlmaLinux: needs-restarting exit 1", site: alma.restartingAnswer(hostfake.Response{Stdout: rebootLines, ExitCode: 1}), pending: detect.Yes,
			codes: []string{header.CodeRebootRequired}, sources: []string{"dnf needs-restarting -r"}},
		{name: "a distribution not known", site: other, pending: detect.Unknown, err: "DETECT_NO_SIGNAL"},
		{name: "os-release not read", site: noOS, pending: detect.Unknown, err: "PERMISSION_DENIED"},
	} {
		t.Run(c.name, func(t *testing.T) {
			r := read(t, c.site).Reboot
			var sources []string
			for _, f := range r.Findings {
				sources = append(sources, f.Source)
			}
			if r.Pending != c.pending || !slices.Equal(codes(r.Findings), c.codes) || !slices.Equal(sources, c.sources) || output.CodeOf(r.Err) != c.err {
				t.Errorf("reboot = %v %v %v %v, want %v %v %v %s", r.Pending, codes(r.Findings), sources, r.Err, c.pending, c.codes, c.sources, c.err)
			}
			if (r.Pending == detect.Unknown) != (r.Err != nil && r.Pending != detect.Yes) {
				t.Errorf("Unknown without an error, or an error with No: %v %v", r.Pending, r.Err)
			}
			if container := c.site.facts.Container.Detected == detect.Yes; r.HostKernel != container || container && r.Running != "" {
				t.Errorf("host kernel %v, running %q, in a container %v", r.HostKernel, r.Running, container)
			}
		})
	}
}

// TestFindings: each finding's code is registered, and its detail is the
// package's.
func TestFindings(t *testing.T) {
	want := map[string]string{
		header.CodeRebootRequired: header.DetailRebootRequired,
		header.CodeNewerKernel:    header.DetailNewerKernel,
		header.CodeModulesMissing: header.DetailModulesMissing,
		header.CodeKernelReplaced: header.DetailKernelReplaced,
	}
	alp := alpine324().without("/lib/modules").with("/lib/modules/6.12.45-0-lts/kernel/x.ko", "")
	deb := debian13().dpkgAnswer(debianPackage, installed("6.12.43-2"))
	got := slices.Concat(read(t, ubuntu2604()).Reboot.Findings, read(t, alp).Reboot.Findings, read(t, deb).Reboot.Findings)
	if len(got) != len(want) {
		t.Fatalf("findings %v, want one of each of %v", got, want)
	}
	for _, f := range got {
		if f.Detail != want[f.Code] || f.Source == "" {
			t.Errorf("finding %+v, want detail %q and a source", f, want[f.Code])
		}
		if e, ok := output.Lookup(f.Code); !ok || e.Kind != output.KindFinding {
			t.Errorf("%s: not registered as a finding", f.Code)
		}
	}
}

// TestCompareKernels: releases compare by their versions, as dpkg
// compares versions, whatever their flavours.
func TestCompareKernels(t *testing.T) {
	for _, c := range []struct {
		a, b string
		want int // the sign
	}{
		{"6.12.48+deb13-amd64", "6.12.43+deb13-amd64", 1},
		{"6.12.43+deb13-amd64", "6.12.43+deb13-amd64", 0},
		{"6.12.43+deb13-cloud-amd64", "6.12.43+deb13-amd64", 0},
		{"6.17.0-19-generic", "6.17.0-14-generic", 1},
		{"6.17.0-9-generic", "6.17.0-14-generic", -1},
		{"6.17.0-1012-aws", "6.17.0-14-generic", 1},
		{"6.14.8-2-pve", "6.17.2-1-pve", -1},
		{"6.1.0-40-amd64", "6.1.0-37-amd64", 1},
		{"6.12.12+bpo-amd64", "6.12.43+deb13-amd64", -1},
		{"6.12.0", "6.12.0-1-amd64", -1},
		{"6.12.0~rc1", "6.12.0", -1},
		{"6.8.0-31-generic.efi.signed", "6.8.0-31-generic", 0},
		{"6.10.0", "6.9.12", 1},
		{"6.12.007", "6.12.7", 0},
	} {
		got := header.CompareKernels(c.a, c.b)
		if sign(got) != c.want {
			t.Errorf("compare(%s, %s) = %d, want sign %d", c.a, c.b, got, c.want)
		}
		if sign(header.CompareKernels(c.b, c.a)) != -c.want {
			t.Errorf("compare(%s, %s) is not the opposite", c.b, c.a)
		}
	}
}

func sign(n int) int {
	switch {
	case n > 0:
		return 1
	case n < 0:
		return -1
	}
	return 0
}

// TestCancelled: a done context leaves every fact not established, with
// its error, and runs nothing.
func TestCancelled(t *testing.T) {
	s := debian13()
	sys, f := s.build()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	h := header.Read(ctx, sys, s.facts)
	for name, err := range map[string]error{"hostname": h.Hostname.Err, "uptime": h.Uptime.Err, "ipv4": h.Address.IPv4.Err, "ipv6": h.Address.IPv6.Err, "reboot": h.Reboot.Err} {
		if output.CodeOf(err) != "CANCELLED" {
			t.Errorf("%s: %v, want CANCELLED", name, err)
		}
	}
	if _, known := h.Address.Primary(); known || h.Reboot.Pending != detect.Unknown {
		t.Errorf("primary known %v, reboot %v", known, h.Reboot.Pending)
	}
	if runs := f.Runs(); len(runs) != 0 {
		t.Errorf("ran %v", runs)
	}
}
