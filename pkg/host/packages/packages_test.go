package packages_test

import (
	"context"
	"errors"
	"io/fs"
	"slices"
	"strings"
	"testing"

	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/hostfake"
	"github.com/abbott/hardn/pkg/host/packages"
)

// The apt-get and apk failure answers are the forms read from the lab's VMs on
// 2026-11-02 (docs/safety/packages.md). apkOK and aptUpgrade are not: see each.
const (
	aptPreamble = "Reading package lists...\nBuilding dependency tree...\nReading state information...\n"

	aptLynis = aptPreamble +
		"Inst lynis (3.1.4-1 Debian:13.7/stable [all])\n" +
		"Inst menu (2.1.51 Debian:13.7/stable [amd64])\n" +
		"Conf lynis (3.1.4-1 Debian:13.7/stable [all])\n" +
		"Conf menu (2.1.51 Debian:13.7/stable [amd64])\n"
	aptPostfix = aptPreamble +
		"Inst libnsl2 (1.3.0-3build4 Ubuntu:26.04/resolute [amd64])\n" +
		"Inst postfix (3.10.6-4ubuntu2.2 Ubuntu:26.04/resolute-updates [amd64])\n" +
		"Conf libnsl2 (1.3.0-3build4 Ubuntu:26.04/resolute [amd64])\n" +
		"Conf postfix (3.10.6-4ubuntu2.2 Ubuntu:26.04/resolute-updates [amd64])\n"
	aptNewest = aptPreamble +
		"ufw is already the newest version (0.36.2-9build1).\nufw set to manually installed.\nSolving dependencies...\n" +
		"0 upgraded, 0 newly installed, 0 to remove and 32 not upgraded.\n"
	aptRemoves = aptPreamble +
		"Remv ufw [0.36.2-9]\nInst nftables (1.1.3-1 Debian:13.7/stable [amd64])\nConf nftables (1.1.3-1 Debian:13.7/stable [amd64])\n"
	// [I] apt's documented simulate form, not in the logs.
	aptUpgrade = aptPreamble +
		"Inst libc6 [2.41-11] (2.41-12 Debian:13.7/stable [amd64])\nInst nftables (1.1.3-1 Debian:13.7/stable [amd64])\n"
	aptMissing = "E: Unable to locate package hardn-no-such-package\n"

	apkEmptyIndex = "WARNING: opening from cache https://dl-cdn.alpinelinux.org/alpine/v3.23/main/x86_64/APKINDEX.tar.gz: No such file or directory\n" +
		"ERROR: unable to select packages:\n  ufw (no such package):\n    required by: world[ufw]\n"
	apkNoDNS = "WARNING: fetching https://dl-cdn.alpinelinux.org/alpine/v3.23/main/x86_64/APKINDEX.tar.gz: DNS: transient error (try again later)\n" +
		"ERROR: unable to select packages:\n  ufw (no such package):\n    required by: world[ufw]\n"
	// [I] pkg/host/alpine's tests' form: no VM has shown it for ufw.
	apkOK = "(1/2) Installing python3 (3.12.13-r0)\n(2/2) Installing ufw (0.36.2-r3)\nOK: 9 MiB in 26 packages\n"
)

// dnf's transaction table, as rocky10-test-110 printed it (probe 4,
// 2026-11-09, LC_ALL=C): dnfProbe is that output; the others are it edited
// [I], each said.
const (
	dnfHead = "Last metadata expiration check: 1:54:47 ago on Mon Nov  9 23:03:35 2026.\nDependencies resolved.\n" +
		"================================================================================\n Package                 Arch      Version                   Repository    Size\n================================================================================\n"
	dnfInstalling = "Installing:\n firewalld               noarch    2.4.3-4.el10_2            baseos       471 k\n"
	dnfKernel     = " kernel-core             x86_64    6.12.0-211.74.1.el10_2    baseos        19 M\n"
	dnfDeps       = "Installing dependencies:\n" +
		" firewalld-filesystem    noarch    2.4.3-4.el10_2            baseos       7.7 k\n" +
		" ipset-libs              x86_64    7.22-13.el10_2            baseos        69 k\n"
	dnfKernelDeps = " kernel-modules          x86_64    6.12.0-211.74.1.el10_2    baseos        42 M\n" +
		" kernel-modules-core     x86_64    6.12.0-211.74.1.el10_2    baseos        30 M\n" +
		" kernel-modules-extra    x86_64    6.12.0-211.74.1.el10_2    baseos       3.2 M\n"
	dnfMoreDeps = " libnftnl                x86_64    1.3.0-3.el10              baseos        87 k\n" +
		" nftables                x86_64    1:1.1.5-6.el10_2          baseos       447 k\n" +
		" python3-firewall        noarch    2.4.3-4.el10_2            baseos       478 k\n" +
		" python3-nftables        x86_64    1:1.1.5-6.el10_2          baseos        25 k\n"
	dnfWeak = "Installing weak dependencies:\n" +
		" ipset                   x86_64    7.22-13.el10_2            baseos        44 k\n" +
		" iptables-nft            x86_64    1.8.11-13.el10            appstream    190 k\n" +
		" libcap-ng-python3       x86_64    0.8.4-6.el10              appstream     30 k\n"
	dnfTail = "\nTransaction Summary\n================================================================================\nInstall  14 Packages\n\nTotal download size: 96 M\nInstalled size: 145 M\n"
	// dnfProbe is probe 4's transaction: the newest kernel comes with
	// firewalld's kernel-modules-extra.
	dnfProbe = dnfHead + dnfInstalling + dnfKernel + dnfDeps + dnfKernelDeps + dnfMoreDeps + dnfWeak + dnfTail
	// [I] the same on a host that runs the newest kernel: its modules come,
	// for the running kernel.
	dnfRunningKernel = dnfHead + dnfInstalling + dnfDeps + " kernel-modules-extra    x86_64    6.12.0-211.74.1.el10_2    baseos       3.2 M\n" + dnfMoreDeps + dnfWeak + dnfTail
	// [I] fmtColumns: a kernel row wrapped, its version on the line below.
	dnfWrappedKernel = dnfHead + dnfInstalling + " kernel-core\n                         x86_64    6.12.0-211.74.1.el10_2    baseos        19 M\n" + dnfTail
	// [I] userland headers of another release: no kernel.
	dnfHeaders = dnfHead + dnfInstalling + "Installing dependencies:\n kernel-headers          x86_64    6.12.0-211.74.1.el10_2    baseos       2.6 M\n" + dnfTail
	// The same without weak dependencies, as hardn simulates it
	// (rocky10-test-110, probe 3b-2, 2026-11-10: 6 packages, no kernel).
	dnfNoKernel = dnfHead + dnfInstalling + "Installing dependencies:\n" +
		" firewalld-filesystem      noarch      2.4.3-4.el10_2         baseos      7.7 k\n" + dnfMoreDeps + dnfTail
	dnfAborted = "Operation aborted.\n"
	// [I] "Upgrading:" is probe 4's, for sudo; the rest is the table's.
	dnfUpgrade = dnfHead + dnfInstalling + "Upgrading:\n sudo                  x86_64    1.9.17-10.p2.el10_2.7       baseos       1.3 M\n" + dnfTail
	// [I] dnf's output.py: an obsoleting package's "replacing" line.
	dnfReplacing = dnfHead + dnfInstalling + "     replacing  firewalld-old.noarch 0.9\n" + dnfTail
	// [I] fmtColumns: a name wider than its column, the rest below it.
	dnfWrapped = dnfHead + "Installing:\n firewalld-with-a-name-wider-than-its-column\n                         noarch    2.4.3-4.el10_2            baseos       471 k\n" + dnfTail
	// rocky10-test-110, probe 3b (2026-11-10): no package of that name, the
	// first line on stdout, the second on stderr, exit 1.
	dnfMissingOut = "Last metadata expiration check: 3:34:12 ago on Mon Nov  9 23:03:35 2026.\nNo match for argument: hardn-no-such-package\n"
	dnfMissing    = "Error: Unable to find a match: hardn-no-such-package\n"
	// rhel10-test-114 and rhel9-test-115, probe 3 (2026-11-10): an unregistered
	// RHEL host, no enabled repository: the subscription plugin's notices on
	// stdout, dnf's error on stderr, exit 1.
	dnfUnregisteredOut = "Updating Subscription Management repositories.\nUnable to read consumer identity\n\nThis system is not registered with an entitlement server. You can use \"rhc\" or \"subscription-manager\" to register.\n\n"
	dnfNoRepos         = "Error: There are no enabled repositories in \"/etc/yum.repos.d\", \"/etc/yum/repos.d\", \"/etc/distro.repos.d\".\n"
	// rhel9-test-115 (probe 4) and rhel10-test-114 (probe 5), registered
	// (2026-11-10): the subscription plugin's line and, on RHEL 9, the
	// metadata downloads above the table; stderr "Operation aborted.", exit 1.
	dnfRHEL9Firewalld  = "Updating Subscription Management repositories.\nRed Hat Enterprise Linux 9 for x86_64 - BaseOS  9.2 MB/s | 168 MB     00:18    \nRed Hat Enterprise Linux 9 for x86_64 - AppStre 9.6 MB/s | 122 MB     00:12    \nLast metadata expiration check: 0:00:01 ago on Tue Nov 10 19:38:10 2026.\nDependencies resolved.\n================================================================================\n Package             Arch   Version         Repository                     Size\n================================================================================\nInstalling:\n firewalld           noarch 1.3.4-20.el9_8  rhel-9-for-x86_64-baseos-rpms 542 k\nInstalling dependencies:\n firewalld-filesystem\n                     noarch 1.3.4-20.el9_8  rhel-9-for-x86_64-baseos-rpms  15 k\n ipset               x86_64 7.11-11.el9_5   rhel-9-for-x86_64-baseos-rpms  45 k\n ipset-libs          x86_64 7.11-11.el9_5   rhel-9-for-x86_64-baseos-rpms  71 k\n iptables-nft        x86_64 1.8.10-11.el9_5 rhel-9-for-x86_64-baseos-rpms 209 k\n libnftnl            x86_64 1.2.6-4.el9_4   rhel-9-for-x86_64-baseos-rpms  89 k\n nftables            x86_64 1:1.0.9-9.el9_8 rhel-9-for-x86_64-baseos-rpms 462 k\n python3-firewall    noarch 1.3.4-20.el9_8  rhel-9-for-x86_64-baseos-rpms 400 k\n python3-nftables    x86_64 1:1.0.9-9.el9_8 rhel-9-for-x86_64-baseos-rpms  49 k\n\nTransaction Summary\n================================================================================\nInstall  9 Packages\n\nTotal download size: 1.8 M\nInstalled size: 6.3 M\n"
	dnfRHEL9Automatic  = "Updating Subscription Management repositories.\nLast metadata expiration check: 0:00:26 ago on Tue Nov 10 19:38:10 2026.\nDependencies resolved.\n================================================================================\n Package         Arch     Version         Repository                       Size\n================================================================================\nInstalling:\n dnf-automatic   noarch   4.14.0-37.el9   rhel-9-for-x86_64-baseos-rpms    39 k\n\nTransaction Summary\n================================================================================\nInstall  1 Package\n\nTotal download size: 39 k\nInstalled size: 58 k\n"
	dnfRHEL10Firewalld = "Updating Subscription Management repositories.\nLast metadata expiration check: 0:01:51 ago on Wed Nov 11 00:37:41 2026.\nDependencies resolved.\n======================================================================================\n Package                Arch    Version          Repository                       Size\n======================================================================================\nInstalling:\n firewalld              noarch  2.4.3-4.el10_2   rhel-10-for-x86_64-baseos-rpms  573 k\nInstalling dependencies:\n firewalld-filesystem   noarch  2.4.3-4.el10_2   rhel-10-for-x86_64-baseos-rpms   14 k\n python3-firewall       noarch  2.4.3-4.el10_2   rhel-10-for-x86_64-baseos-rpms  522 k\n python3-nftables       x86_64  1:1.1.5-6.el10_2 rhel-10-for-x86_64-baseos-rpms   32 k\n\nTransaction Summary\n======================================================================================\nInstall  4 Packages\n\nTotal download size: 1.1 M\nInstalled size: 5.1 M\n"
	dnfRHEL10Automatic = "Updating Subscription Management repositories.\nLast metadata expiration check: 0:01:53 ago on Wed Nov 11 00:37:41 2026.\nDependencies resolved.\n================================================================================\n Package        Arch    Version           Repository                       Size\n================================================================================\nInstalling:\n dnf-automatic  noarch  4.20.0-28.el10    rhel-10-for-x86_64-baseos-rpms   46 k\n\nTransaction Summary\n================================================================================\nInstall  1 Package\n\nTotal download size: 46 k\nInstalled size: 82 k\n"
)

func rpmQ(name string) []string { return []string{"rpm", "-q", name} }

func rpmAbsent(name string) hostfake.Response {
	return hostfake.Response{Stdout: "package " + name + " is not installed\n", ExitCode: 1}
}

// dpkg is detect's read of a package.
func dpkg(name string) []string {
	return []string{"dpkg-query", "--show", `--showformat=${Status}\t${Version}\n`, name}
}

func apkInfo(name string) []string { return []string{"apk", "info", "-e", name} }

var (
	dpkgAbsent = hostfake.Response{Stderr: "dpkg-query: no packages found matching x\n", ExitCode: 1}
	dpkgThere  = hostfake.Response{Stdout: "install ok installed\t0.36.2-9\n"}
	apkAbsent  = hostfake.Response{ExitCode: 1}
)

func respond(h *hostfake.System, r hostfake.Response, argv []string) {
	h.Respond(r, argv[0], argv[1:]...)
}

// absent is a fake on which name reads not installed.
func absent(t packages.Tool, name string) *hostfake.System {
	h := hostfake.New(host.Options{})
	switch t {
	case packages.Apt:
		respond(h, dpkgAbsent, dpkg(name))
	case packages.Dnf:
		respond(h, rpmAbsent(name), rpmQ(name))
	default:
		respond(h, apkAbsent, apkInfo(name))
	}
	return h
}

func simulate(t packages.Tool, name string) []string {
	switch t {
	case packages.Apt:
		return []string{"apt-get", "install", "-s", "-y", name}
	case packages.Dnf:
		return []string{"dnf", "install", "--assumeno", "--setopt=install_weak_deps=False", "--setopt=exit_on_lock=True", name}
	}
	return []string{"apk", "add", "--simulate", name}
}

func runs(h *hostfake.System) []string {
	var out []string
	for _, r := range h.Runs() {
		out = append(out, strings.Join(r, " "))
	}
	return out
}

func TestToolFor(t *testing.T) {
	for _, c := range []struct {
		distro detect.Distro
		like   detect.Distro
		want   packages.Tool
		err    error
	}{
		{distro: detect.DistroDebian, want: packages.Apt},
		{distro: detect.DistroUbuntu, want: packages.Apt},
		{distro: detect.DistroAlpine, want: packages.Apk},
		{distro: detect.DistroRocky, want: packages.Dnf},
		{distro: detect.DistroAlma, want: packages.Dnf},
		{distro: detect.DistroRHEL, want: packages.Dnf},
		{distro: detect.DistroProxmox, err: packages.ErrNoTool},
		{distro: detect.DistroOther, like: detect.DistroDebian, err: packages.ErrNoTool},
		{distro: detect.DistroUnknown, err: packages.ErrNoTool},
	} {
		got, err := packages.ToolFor(detect.Facts{Platform: detect.Platform{Distro: c.distro, Like: c.like}})
		if got != c.want || !errors.Is(err, c.err) {
			t.Errorf("%q: got %d, %v; want %d, %v", c.distro, got, err, c.want, c.err)
		}
	}
}

func TestInstalled(t *testing.T) {
	for _, c := range []struct {
		name string
		tool packages.Tool
		r    hostfake.Response
		want detect.Tri
		err  bool
	}{
		{"dpkg installed", packages.Apt, dpkgThere, detect.Yes, false},
		{"dpkg none", packages.Apt, dpkgAbsent, detect.No, false},
		{"dpkg config-files", packages.Apt, hostfake.Response{Stdout: "deinstall ok config-files\t1\n"}, detect.No, false},
		{"dpkg half", packages.Apt, hostfake.Response{Stdout: "install ok half-installed\t1\n"}, detect.Unknown, true},
		{"apk name", packages.Apk, hostfake.Response{Stdout: "ufw\n"}, detect.Yes, false},
		{"apk version", packages.Apk, hostfake.Response{Stdout: "ufw-0.36.2-r3\n"}, detect.Yes, false},
		{"apk quiet", packages.Apk, hostfake.Response{}, detect.Yes, false},
		{"apk none", packages.Apk, apkAbsent, detect.No, false},
		{"apk other name", packages.Apk, hostfake.Response{Stdout: "ufw-extra\n"}, detect.Unknown, true},
		{"apk exit 1 said why", packages.Apk, hostfake.Response{Stderr: "ERROR: database locked\n", ExitCode: 1}, detect.Unknown, true},
		{"apk exit 2", packages.Apk, hostfake.Response{ExitCode: 2}, detect.Unknown, true},
		{"rpm installed", packages.Dnf, hostfake.Response{Stdout: "ufw-0.36.2-1.el10.noarch\n"}, detect.Yes, false},
		{"rpm two versions", packages.Dnf, hostfake.Response{Stdout: "ufw-0.36.2-1.el10.noarch\nufw-0.36.3-1.el10.noarch\n"}, detect.Yes, false},
		{"rpm none", packages.Dnf, rpmAbsent("ufw"), detect.No, false},
		{"rpm other name", packages.Dnf, hostfake.Response{Stdout: "ufwx-1-1.noarch\n"}, detect.Unknown, true},
		{"rpm exit 1 said otherwise", packages.Dnf, hostfake.Response{Stderr: "error: rpmdb open failed\n", ExitCode: 1}, detect.Unknown, true},
		{"rpm exit 0 silent", packages.Dnf, hostfake.Response{}, detect.Unknown, true},
	} {
		t.Run(c.name, func(t *testing.T) {
			h := hostfake.New(host.Options{})
			switch c.tool {
			case packages.Apt:
				respond(h, c.r, dpkg("ufw"))
			case packages.Dnf:
				respond(h, c.r, rpmQ("ufw"))
			default:
				respond(h, c.r, apkInfo("ufw"))
			}
			got, err := packages.Installed(context.Background(), h, c.tool, "ufw")
			if got != c.want || (err != nil) != c.err {
				t.Errorf("got %v, %v; want %v, error %t", got, err, c.want, c.err)
			}
		})
	}
}

func TestPlanInstall(t *testing.T) {
	for _, c := range []struct {
		name, pkg  string
		tool       packages.Tool
		out        string
		argv, undo []string
		adds       []string
	}{
		{"deb13 lynis", "lynis", packages.Apt, aptLynis,
			[]string{"apt-get", "install", "-y", "lynis"}, []string{"apt-get", "remove", "-y", "lynis"}, []string{"lynis", "menu"}},
		{"ubuntu26 postfix", "postfix", packages.Apt, aptPostfix,
			[]string{"apt-get", "install", "-y", "postfix"}, []string{"apt-get", "remove", "-y", "postfix"}, []string{"libnsl2", "postfix"}},
		{"alpine ufw [I]", "ufw", packages.Apk, apkOK,
			[]string{"apk", "add", "ufw"}, []string{"apk", "del", "ufw"}, []string{"python3", "ufw"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			h := absent(c.tool, c.pkg)
			sim := simulate(c.tool, c.pkg)
			respond(h, hostfake.Response{Stdout: c.out}, sim)
			p, err := packages.PlanInstall(context.Background(), h, c.tool, c.pkg)
			if err != nil {
				t.Fatal(err)
			}
			if p.Empty() || p.Tool != c.tool || p.Name != c.pkg || !slices.Equal(p.Argv, c.argv) || !slices.Equal(p.Undo, c.undo) || !slices.Equal(p.Adds, c.adds) {
				t.Errorf("plan %+v", p)
			}
			// Reads only: the read and the simulation, nothing else.
			if got := h.Runs(); len(got) != 2 || !slices.Equal(got[1], sim) {
				t.Errorf("runs %q", runs(h))
			}
		})
	}
}

// TestPlanInstallDnf: dnf's table read section by section. Only installs
// are accepted; a kernel among them is a *KernelError (probe 4's own
// transaction); an upgrade or a replacement is an *OtherPackagesError; no
// such package is ErrNotInRepos. Each reads only: rpm -q, then the
// simulation.
func TestPlanInstallDnf(t *testing.T) {
	aborted := func(out string) hostfake.Response {
		return hostfake.Response{Stdout: out, Stderr: dnfAborted, ExitCode: 1}
	}
	ctx := context.Background()
	for _, c := range []struct {
		name    string
		r       hostfake.Response
		adds    []string
		kernels []string
		changes []string
		uname   string // uname -r, where a kernel row is listed
		repos   bool
		noRepos bool
		locked  bool
		other   bool // any other error
	}{
		{name: "no weak dependencies, no kernel", r: aborted(dnfNoKernel), adds: []string{"firewalld", "firewalld-filesystem", "libnftnl", "nftables", "python3-firewall", "python3-nftables"}},
		{name: "probe 4: a new kernel comes too", r: aborted(dnfProbe), uname: "6.12.0-211.16.1.el10_2.0.1.x86_64", kernels: []string{"kernel-core", "kernel-modules", "kernel-modules-core", "kernel-modules-extra"}},
		{name: "modules for the running kernel [I]", r: aborted(dnfRunningKernel), uname: "6.12.0-211.74.1.el10_2.x86_64",
			adds: []string{"firewalld", "firewalld-filesystem", "ipset-libs", "kernel-modules-extra", "libnftnl", "nftables", "python3-firewall", "python3-nftables", "ipset", "iptables-nft", "libcap-ng-python3"}},
		{name: "modules for another kernel [I]", r: aborted(dnfRunningKernel), uname: "6.12.0-211.16.1.el10_2.0.1.x86_64", kernels: []string{"kernel-modules-extra"}},
		{name: "kernel-headers of another release, no kernel [I]", r: aborted(dnfHeaders), adds: []string{"firewalld", "kernel-headers"}},
		{name: "a wrapped kernel row: another release [I]", r: aborted(dnfWrappedKernel), uname: "6.12.0-211.74.1.el10_2.x86_64", kernels: []string{"kernel-core"}},
		{name: "another dnf holds the lock [I]", r: hostfake.Response{Stderr: "Error: [Errno 11] Resource temporarily unavailable\n", ExitCode: 200}, locked: true},
		{name: "upgrading [I]", r: aborted(dnfUpgrade), changes: []string{"Upgrading sudo"}},
		{name: "replacing [I]", r: aborted(dnfReplacing), changes: []string{"Replacing firewalld-old.noarch"}},
		{name: "a wrapped row [I]", r: aborted(dnfWrapped), adds: []string{"firewalld-with-a-name-wider-than-its-column"}},
		{name: "no such package", r: hostfake.Response{Stdout: dnfMissingOut, Stderr: dnfMissing, ExitCode: 1}, repos: true},
		{name: "registered RHEL 9: firewalld, a wrapped row", r: aborted(dnfRHEL9Firewalld), adds: []string{"firewalld", "firewalld-filesystem", "ipset", "ipset-libs", "iptables-nft", "libnftnl", "nftables", "python3-firewall", "python3-nftables"}},
		{name: "registered RHEL 9: dnf-automatic", r: aborted(dnfRHEL9Automatic), adds: []string{"dnf-automatic"}},
		{name: "registered RHEL 10: firewalld", r: aborted(dnfRHEL10Firewalld), adds: []string{"firewalld", "firewalld-filesystem", "python3-firewall", "python3-nftables"}},
		{name: "registered RHEL 10: dnf-automatic", r: aborted(dnfRHEL10Automatic), adds: []string{"dnf-automatic"}},
		{name: "no enabled repository (an unregistered RHEL host)", r: hostfake.Response{Stdout: dnfUnregisteredOut, Stderr: dnfNoRepos, ExitCode: 1}, noRepos: true},
		{name: "the no-repositories line, exit 0 [I]", r: hostfake.Response{Stdout: dnfUnregisteredOut, Stderr: dnfNoRepos}, other: true},
		{name: "the no-repositories line on stdout alone [I]", r: hostfake.Response{Stdout: dnfUnregisteredOut + dnfNoRepos, ExitCode: 1}, other: true},
		{name: "aborted without a table", r: hostfake.Response{Stderr: dnfAborted, ExitCode: 1}, other: true},
		{name: "a repository unreachable [I]", r: hostfake.Response{Stderr: "Error: Failed to download metadata for repo 'baseos'\n", ExitCode: 1}, other: true},
		{name: "nothing to do [I]", r: hostfake.Response{Stdout: "Dependencies resolved.\nNothing to do.\n"}, other: true},
	} {
		t.Run(c.name, func(t *testing.T) {
			name := "firewalld"
			if c.repos {
				name = "hardn-no-such-package"
			}
			if len(c.adds) == 1 {
				name = c.adds[0]
			}
			h := absent(packages.Dnf, name)
			respond(h, c.r, simulate(packages.Dnf, name))
			if c.uname != "" {
				respond(h, hostfake.Response{Stdout: c.uname + "\n"}, []string{"uname", "-r"})
			}
			p, err := packages.PlanInstall(ctx, h, packages.Dnf, name)
			var kernel *packages.KernelError
			var other *packages.OtherPackagesError
			switch {
			case c.adds != nil:
				if err != nil || !slices.Equal(p.Adds, c.adds) || !slices.Equal(p.Argv, []string{"dnf", "install", "-y", "--setopt=install_weak_deps=False", "--setopt=exit_on_lock=True", name}) ||
					!slices.Equal(p.Undo, []string{"dnf", "remove", "-y", "--noautoremove", "--setopt=exit_on_lock=True", name}) {
					t.Errorf("plan %+v, %v", p, err)
				}
			case c.kernels != nil:
				if !errors.Is(err, packages.ErrKernel) || !errors.As(err, &kernel) || !slices.Equal(kernel.Packages, c.kernels) || !p.Empty() {
					t.Errorf("plan %+v, %v; want the kernels %q", p, err, c.kernels)
				}
			case c.changes != nil:
				if !errors.As(err, &other) || !slices.Equal(other.Changes, c.changes) {
					t.Errorf("plan %+v, %v; want the changes %q", p, err, c.changes)
				}
			case c.repos:
				if !errors.Is(err, packages.ErrNotInRepos) {
					t.Errorf("%v, want ErrNotInRepos", err)
				}
			case c.noRepos:
				if !errors.Is(err, packages.ErrNoRepos) || errors.Is(err, packages.ErrNotInRepos) {
					t.Errorf("%v, want ErrNoRepos", err)
				}
			case c.locked:
				if !errors.Is(err, packages.ErrLocked) {
					t.Errorf("%v, want ErrLocked", err)
				}
			case c.other:
				if err == nil || errors.Is(err, packages.ErrNotInRepos) || errors.Is(err, packages.ErrNoRepos) || errors.Is(err, packages.ErrKernel) || errors.Is(err, packages.ErrOtherPackages) {
					t.Errorf("plan %+v, %v; want another error", p, err)
				}
			}
			want := 2
			if c.uname != "" {
				want = 3 // and uname -r
			}
			if got := h.Runs(); len(got) != want || !slices.Equal(got[0], rpmQ(name)) || !slices.Equal(got[1], simulate(packages.Dnf, name)) {
				t.Errorf("runs %q", runs(h))
			}
		})
	}
}

func TestPlanInstallAlreadyInstalled(t *testing.T) {
	h := hostfake.New(host.Options{})
	respond(h, dpkgThere, dpkg("ufw"))
	p, err := packages.PlanInstall(context.Background(), h, packages.Apt, "ufw")
	if err != nil || !p.Empty() || p.Name != "ufw" || p.Undo != nil {
		t.Fatalf("plan %+v, %v", p, err)
	}
	if got := h.Runs(); len(got) != 1 {
		t.Errorf("runs %q: want only the read", runs(h))
	}
}

func TestPlanInstallRefusals(t *testing.T) {
	apk := func(line string) string {
		return "(1/2) " + line + "\n(2/2) Installing ufw (0.36.2-r3)\nOK: 9 MiB in 26 packages\n"
	}
	for _, c := range []struct {
		name, pkg string
		tool      packages.Tool
		out       string
		changes   []string
	}{
		{"removal", "nftables", packages.Apt, aptRemoves, []string{"Remv ufw [0.36.2-9]"}},
		{"upgrade [I]", "nftables", packages.Apt, aptUpgrade, []string{"Inst libc6 [2.41-11] (2.41-12 Debian:13.7/stable [amd64])"}},
		{"purge [I]", "nftables", packages.Apt, "Purg ufw [0.36.2-9]\nInst nftables (1.1.3-1 x [amd64])\n", []string{"Purg ufw [0.36.2-9]"}},
		{"a trailing - removes [I]", "ufw-", packages.Apt, "Remv ufw [0.36.2-9]\n", []string{"Remv ufw [0.36.2-9]"}},
		{"another package instead [I]", "mail-transport-agent", packages.Apt, aptPostfix, []string{"libnsl2", "postfix"}},
		{"apk upgrading", "ufw", packages.Apk, apk("Upgrading musl (1.2.5-r1 -> 1.2.5-r2)"), []string{"Upgrading musl (1.2.5-r1 -> 1.2.5-r2)"}},
		{"apk purging", "ufw", packages.Apk, apk("Purging openssh-server (10.2_p1-r0)"), []string{"Purging openssh-server (10.2_p1-r0)"}},
		{"apk replacing", "ufw", packages.Apk, apk("Replacing iptables (1.8.11-r1)"), []string{"Replacing iptables (1.8.11-r1)"}},
		{"apk downgrading", "ufw", packages.Apk, apk("Downgrading musl (1.2.5-r2 -> 1.2.5-r1)"), []string{"Downgrading musl (1.2.5-r2 -> 1.2.5-r1)"}},
		{"apk reinstalling", "ufw", packages.Apk, apk("Reinstalling musl (1.2.5-r1)"), []string{"Reinstalling musl (1.2.5-r1)"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			h := absent(c.tool, c.pkg)
			respond(h, hostfake.Response{Stdout: c.out}, simulate(c.tool, c.pkg))
			p, err := packages.PlanInstall(context.Background(), h, c.tool, c.pkg)
			var other *packages.OtherPackagesError
			if !errors.Is(err, packages.ErrOtherPackages) || !errors.As(err, &other) {
				t.Fatalf("plan %+v, err %v", p, err)
			}
			if !slices.Equal(other.Changes, c.changes) {
				t.Errorf("changes %q, want %q", other.Changes, c.changes)
			}
			if !p.Empty() {
				t.Errorf("a refusal planned %+v", p)
			}
			for _, change := range c.changes {
				if !strings.Contains(err.Error(), change) {
					t.Errorf("%q does not name %q", err, change)
				}
			}
		})
	}
}

func TestPlanInstallNoChangeListed(t *testing.T) {
	h := absent(packages.Apt, "ufw")
	respond(h, hostfake.Response{Stdout: aptNewest}, simulate(packages.Apt, "ufw"))
	if p, err := packages.PlanInstall(context.Background(), h, packages.Apt, "ufw"); !errors.Is(err, detect.ErrUnrecognised) {
		t.Errorf("plan %+v, err %v", p, err)
	}
}

func TestPlanInstallFailures(t *testing.T) {
	for _, c := range []struct {
		name  string
		tool  packages.Tool
		pkg   string
		r     hostfake.Response
		repos bool
	}{
		{"apt missing", packages.Apt, "hardn-no-such-package", hostfake.Response{Stdout: aptPreamble, Stderr: aptMissing, ExitCode: 100}, true},
		{"apk empty index", packages.Apk, "ufw", hostfake.Response{Stderr: apkEmptyIndex, ExitCode: 1}, true},
		{"apk no DNS", packages.Apk, "ufw", hostfake.Response{Stderr: apkNoDNS, ExitCode: 1}, true},
		{"apt other failure", packages.Apt, "ufw", hostfake.Response{Stderr: "E: Could not get lock\n", ExitCode: 100}, false},
		{"apt same words, other exit", packages.Apt, "hardn-no-such-package", hostfake.Response{Stderr: aptMissing, ExitCode: 1}, false},
		{"apk conflict", packages.Apk, "ufw", hostfake.Response{Stderr: "ERROR: unable to select packages:\n  ufw-0.36.2-r3:\n    breaks: world[ufw]\n", ExitCode: 1}, false},
		{"apk other failure", packages.Apk, "ufw", hostfake.Response{Stderr: "ERROR: Unable to lock database\n", ExitCode: 2}, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			h := absent(c.tool, c.pkg)
			respond(h, c.r, simulate(c.tool, c.pkg))
			_, err := packages.PlanInstall(context.Background(), h, c.tool, c.pkg)
			if got := errors.Is(err, packages.ErrNotInRepos); got != c.repos {
				t.Errorf("ErrNotInRepos %t, want %t: %v", got, c.repos, err)
			}
			var exit *host.ExitError
			if !c.repos && !errors.As(err, &exit) {
				t.Errorf("the failure is not wrapped: %v", err)
			}
		})
	}
}

func TestBadNameRunsNothing(t *testing.T) {
	ctx := context.Background()
	for _, name := range []string{"", "-y", "UFW", "a b", "ufw\n", "a;b", "cmd:ss", "ufw=1", ".x", "+x", "../x"} {
		h := hostfake.New(host.Options{})
		if _, err := packages.Installed(ctx, h, packages.Apk, name); !errors.Is(err, fs.ErrInvalid) {
			t.Errorf("Installed %q: %v", name, err)
		}
		if _, err := packages.PlanInstall(ctx, h, packages.Apt, name); !errors.Is(err, fs.ErrInvalid) {
			t.Errorf("PlanInstall %q: %v", name, err)
		}
		if _, err := packages.Execute(ctx, h, packages.Plan{Tool: packages.Apt, Name: name}); !errors.Is(err, fs.ErrInvalid) {
			t.Errorf("Execute %q: %v", name, err)
		}
		if got := runs(h); len(got) != 0 {
			t.Errorf("%q ran %q", name, got)
		}
	}
	if _, err := packages.Installed(ctx, hostfake.New(host.Options{}), 0, "ufw"); !errors.Is(err, fs.ErrInvalid) {
		t.Errorf("no tool: %v", err)
	}
}

func planFor(t packages.Tool) packages.Plan {
	if t == packages.Dnf {
		return packages.Plan{Tool: t, Name: "ufw", Argv: []string{"dnf", "install", "-y", "--setopt=install_weak_deps=False", "--setopt=exit_on_lock=True", "ufw"}, Undo: []string{"dnf", "remove", "-y", "--noautoremove", "--setopt=exit_on_lock=True", "ufw"}, Adds: []string{"ufw"}}
	}
	if t == packages.Apt {
		return packages.Plan{Tool: t, Name: "ufw", Argv: []string{"apt-get", "install", "-y", "ufw"}, Undo: []string{"apt-get", "remove", "-y", "ufw"}, Adds: []string{"ufw"}}
	}
	return packages.Plan{Tool: t, Name: "ufw", Argv: []string{"apk", "add", "ufw"}, Undo: []string{"apk", "del", "ufw"}, Adds: []string{"ufw"}}
}

func TestExecute(t *testing.T) {
	notRun := errors.Join(errors.New("fixture"), host.ErrNotFound)
	killed := errors.New("signal: killed")
	for _, c := range []struct {
		name      string
		tool      packages.Tool
		install   hostfake.Response
		after     hostfake.Response
		installed bool
		undo      bool
		verify    bool
		runs      int
	}{
		{name: "apt", tool: packages.Apt, after: dpkgThere, installed: true, undo: true, runs: 2},
		{name: "apk", tool: packages.Apk, after: hostfake.Response{Stdout: "ufw\n"}, installed: true, undo: true, runs: 2},
		{name: "dnf", tool: packages.Dnf, after: hostfake.Response{Stdout: "ufw-0.36.2-1.el10.noarch\n"}, installed: true, undo: true, runs: 2},
		{name: "dnf, not installed after", tool: packages.Dnf, after: rpmAbsent("ufw"), undo: true, verify: true, runs: 2},
		{name: "apt, not installed after", tool: packages.Apt, after: dpkgAbsent, undo: true, verify: true, runs: 2},
		{name: "apk, not installed after", tool: packages.Apk, after: apkAbsent, undo: true, verify: true, runs: 2},
		{name: "unread after", tool: packages.Apt, after: hostfake.Response{Stdout: "install ok half-installed\t1\n"}, undo: true, verify: true, runs: 2},
		{name: "install fails", tool: packages.Apt, install: hostfake.Response{Stderr: "E: Sub-process /usr/bin/dpkg returned an error code (1)\n", ExitCode: 100}, undo: true, runs: 1},
		{name: "install killed", tool: packages.Apk, install: hostfake.Response{Err: killed}, undo: true, runs: 1},
		{name: "not found", tool: packages.Apt, install: hostfake.Response{Err: notRun}, runs: 1},
	} {
		t.Run(c.name, func(t *testing.T) {
			h := hostfake.New(host.Options{})
			p := planFor(c.tool)
			respond(h, c.install, p.Argv)
			switch c.tool {
			case packages.Apt:
				respond(h, c.after, dpkg("ufw"))
			case packages.Dnf:
				respond(h, c.after, rpmQ("ufw"))
			default:
				respond(h, c.after, apkInfo("ufw"))
			}
			rep, err := packages.Execute(context.Background(), h, p)
			if rep.Installed != c.installed || (rep.Undo != nil) != c.undo || (err != nil) == c.installed {
				t.Errorf("report %+v, err %v", rep, err)
			}
			if c.undo && !slices.Equal(rep.Undo, p.Undo) {
				t.Errorf("undo %q", rep.Undo)
			}
			if got := errors.Is(err, packages.ErrVerify); got != c.verify {
				t.Errorf("ErrVerify %t, want %t: %v", got, c.verify, err)
			}
			if got := h.Runs(); len(got) != c.runs || !slices.Equal(got[0], p.Argv) {
				t.Errorf("runs %q", runs(h))
			}
		})
	}
}

func TestExecuteEmptyPlanOnlyReads(t *testing.T) {
	h := hostfake.New(host.Options{})
	respond(h, dpkgThere, dpkg("ufw"))
	rep, err := packages.Execute(context.Background(), h, packages.Plan{Tool: packages.Apt, Name: "ufw"})
	if err != nil || !rep.Installed || rep.Undo != nil {
		t.Errorf("report %+v, err %v", rep, err)
	}
	if got := h.Runs(); len(got) != 1 {
		t.Errorf("runs %q", runs(h))
	}
}

// cancelling is a host whose install, once it runs, cancels its caller's
// context, as a Ctrl-C or a dropped session would, and records whether the
// install's own context saw it.
type cancelling struct {
	*hostfake.System
	cancel  context.CancelFunc
	sawDone bool
}

func (c *cancelling) Run(ctx context.Context, name string, args ...string) (host.Result, error) {
	if name == "dnf" && len(args) > 1 && args[0] == "install" && args[1] == "-y" {
		c.cancel()
		c.sawDone = ctx.Err() != nil
	}
	return c.System.Run(ctx, name, args...)
}

// TestExecuteRunsToItsEnd: a context cancelled while the install runs
// doesn't reach it (InstallTimeout's own context), and the read after it
// still runs: a killed rpm transaction leaves the database half done.
func TestExecuteRunsToItsEnd(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	h := &cancelling{System: hostfake.New(host.Options{}), cancel: cancel}
	p := planFor(packages.Dnf)
	respond(h.System, hostfake.Response{}, p.Argv)
	respond(h.System, hostfake.Response{Stdout: "ufw-0.36.2-1.el10.noarch\n"}, rpmQ("ufw"))
	if _, err := packages.Execute(ctx, h, p); h.sawDone || ctx.Err() == nil {
		t.Errorf("the install saw the cancel %v (ctx %v): %v", h.sawDone, ctx.Err(), err)
	}
}

func TestExecuteRefuses(t *testing.T) {
	other := planFor(packages.Apt)
	other.Argv = []string{"sh", "-c", "true"}
	noUndo := planFor(packages.Apt)
	noUndo.Undo = nil
	undoOnly := packages.Plan{Tool: packages.Apt, Name: "ufw", Undo: []string{"apt-get", "remove", "-y", "ufw"}}
	for _, p := range []packages.Plan{other, noUndo, undoOnly, {}} {
		h := hostfake.New(host.Options{})
		if _, err := packages.Execute(context.Background(), h, p); !errors.Is(err, fs.ErrInvalid) {
			t.Errorf("%+v: %v", p, err)
		}
		if got := runs(h); len(got) != 0 {
			t.Errorf("%+v ran %q", p, got)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	h := hostfake.New(host.Options{})
	if _, err := packages.Execute(ctx, h, planFor(packages.Apt)); !errors.Is(err, context.Canceled) {
		t.Errorf("done ctx: %v", err)
	}
	if got := runs(h); len(got) != 0 {
		t.Errorf("done ctx ran %q", got)
	}
}

// dnfRemoveTable is dnf remove --assumeno's table [I: the install
// simulation's form, probe 4, with dnf's "Removing:" heading].
func dnfRemoveTable(sections string) hostfake.Response {
	return hostfake.Response{Stdout: dnfHead + sections + "\nTransaction Summary\n" + strings.Repeat("=", 80) + "\nRemove  1 Package\n", Stderr: dnfAborted, ExitCode: 1}
}

// TestPlanRemove: dnf alone; not installed plans nothing; the package
// alone under "Removing:" plans its remove and, as undo, its install; a
// dependent package removed with it, or another section, is refused; the
// lock is ErrLocked; a simulation listing nothing ("Nothing to do.") is
// ErrUnrecognised, and rpm -q unread plans nothing more. Each reads only:
// rpm -q, then the simulation.
func TestPlanRemove(t *testing.T) {
	ctx := context.Background()
	sim := []string{"dnf", "remove", "--assumeno", "--noautoremove", "--setopt=exit_on_lock=True", "dnf-automatic"}
	if _, err := packages.PlanRemove(ctx, hostfake.New(host.Options{}), packages.Apt, "dnf-automatic"); !errors.Is(err, fs.ErrInvalid) {
		t.Errorf("apt: %v", err)
	}
	h := absent(packages.Dnf, "dnf-automatic")
	if r, err := packages.PlanRemove(ctx, h, packages.Dnf, "dnf-automatic"); err != nil || !r.Empty() || len(h.Runs()) != 1 {
		t.Errorf("not installed: %+v, %v", r, err)
	}
	h = hostfake.New(host.Options{})
	respond(h, hostfake.Response{Stderr: "error: rpmdb: damaged header\n", ExitCode: 1}, rpmQ("dnf-automatic"))
	respond(h, dnfRemoveTable("Removing:\n dnf-automatic     noarch     4.20.0-22.el10_2.rocky.0.1       @baseos     79 k\n"), sim)
	if r, err := packages.PlanRemove(ctx, h, packages.Dnf, "dnf-automatic"); !errors.Is(err, detect.ErrUnrecognised) || len(h.Runs()) != 1 {
		t.Errorf("rpm -q unread: %+v, %v, ran %q", r, err, runs(h))
	}
	row := " dnf-automatic     noarch     4.20.0-22.el10_2.rocky.0.1       @baseos     79 k\n"
	for _, c := range []struct {
		name         string
		r            hostfake.Response
		changes      []string
		locked       bool
		unrecognised bool
	}{
		{name: "alone", r: dnfRemoveTable("Removing:\n" + row)},
		{name: "a dependent package too", r: dnfRemoveTable("Removing:\n" + row + "Removing dependent packages:\n cockpit-pkg       noarch     1-1                              @appstream  10 k\n"), changes: []string{"Removing cockpit-pkg"}},
		{name: "another package under Removing", r: dnfRemoveTable("Removing:\n" + row + " other             noarch     1-1                              @baseos     1 k\n"), changes: []string{"dnf-automatic", "other"}},
		{name: "the lock", r: hostfake.Response{Stderr: "Error: [Errno 11] Resource temporarily unavailable\n", ExitCode: 200}, locked: true},
		{name: "nothing listed", r: hostfake.Response{Stdout: "Dependencies resolved.\nNothing to do.\nComplete!\n"}, unrecognised: true},
	} {
		h := hostfake.New(host.Options{})
		respond(h, hostfake.Response{Stdout: "dnf-automatic-4.20.0-22.el10_2.rocky.0.1.noarch\n"}, rpmQ("dnf-automatic"))
		respond(h, c.r, sim)
		r, err := packages.PlanRemove(ctx, h, packages.Dnf, "dnf-automatic")
		var other *packages.OtherPackagesError
		switch {
		case c.locked:
			if !errors.Is(err, packages.ErrLocked) {
				t.Errorf("%s: %v", c.name, err)
			}
		case c.unrecognised:
			if !errors.Is(err, detect.ErrUnrecognised) {
				t.Errorf("%s: %+v, %v", c.name, r, err)
			}
		case c.changes != nil:
			if !errors.As(err, &other) || !slices.Equal(other.Changes, c.changes) {
				t.Errorf("%s: %+v, %v", c.name, r, err)
			}
		default:
			if err != nil || r.Name != "dnf-automatic" || !slices.Equal(r.Argv, []string{"dnf", "remove", "-y", "--noautoremove", "--setopt=exit_on_lock=True", "dnf-automatic"}) ||
				!slices.Equal(r.Undo, []string{"dnf", "install", "-y", "--setopt=install_weak_deps=False", "--setopt=exit_on_lock=True", "dnf-automatic"}) {
				t.Errorf("%s: %+v, %v", c.name, r, err)
			}
		}
		if got := h.Runs(); len(got) != 2 || !slices.Equal(got[1], sim) {
			t.Errorf("%s: runs %q", c.name, runs(h))
		}
	}
}

// TestExecuteRemove: the remove runs, then rpm -q must say not installed
// (unread is ErrVerify, wrapping why); a removal not PlanRemove's, or a
// cancelled ctx, runs nothing.
func TestExecuteRemove(t *testing.T) {
	ctx := context.Background()
	r := packages.Removal{Name: "dnf-automatic", Argv: []string{"dnf", "remove", "-y", "--noautoremove", "--setopt=exit_on_lock=True", "dnf-automatic"},
		Undo: []string{"dnf", "install", "-y", "--setopt=install_weak_deps=False", "--setopt=exit_on_lock=True", "dnf-automatic"}}
	for _, after := range []hostfake.Response{rpmAbsent("dnf-automatic"), {Stdout: "dnf-automatic-4.20.0-22.el10_2.rocky.0.1.noarch\n"}} {
		h := hostfake.New(host.Options{})
		respond(h, hostfake.Response{}, r.Argv)
		respond(h, after, rpmQ("dnf-automatic"))
		err := packages.ExecuteRemove(ctx, h, r)
		if gone := after.ExitCode == 1; (err == nil) != gone || !gone && !errors.Is(err, packages.ErrVerify) {
			t.Errorf("after %q: %v", after.Stdout, err)
		}
	}
	h := hostfake.New(host.Options{})
	respond(h, hostfake.Response{}, r.Argv)
	respond(h, hostfake.Response{Stderr: "error: rpmdb: damaged header\n", ExitCode: 1}, rpmQ("dnf-automatic"))
	if err := packages.ExecuteRemove(ctx, h, r); !errors.Is(err, packages.ErrVerify) || !errors.Is(err, detect.ErrUnrecognised) {
		t.Errorf("rpm -q unread after: %v", err)
	}
	for name, edit := range map[string]func(*packages.Removal){
		"other remove": func(b *packages.Removal) { b.Argv = []string{"dnf", "remove", "-y", "dnf-automatic"} },
		"other undo":   func(b *packages.Removal) { b.Undo = []string{"dnf", "install", "-y", "dnf-automatic"} },
	} {
		bad := r
		edit(&bad)
		h := hostfake.New(host.Options{})
		if err := packages.ExecuteRemove(ctx, h, bad); !errors.Is(err, fs.ErrInvalid) || len(h.Runs()) != 0 {
			t.Errorf("%s: %v, ran %q", name, err, runs(h))
		}
	}
	cctx, cancel := context.WithCancel(ctx)
	cancel()
	h = hostfake.New(host.Options{})
	respond(h, hostfake.Response{}, r.Argv)
	respond(h, rpmAbsent("dnf-automatic"), rpmQ("dnf-automatic"))
	if err := packages.ExecuteRemove(cctx, h, r); !errors.Is(err, context.Canceled) || len(h.Runs()) != 0 {
		t.Errorf("cancelled: %v, ran %q", err, runs(h))
	}
}
