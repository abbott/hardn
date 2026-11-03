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
	if t == packages.Apt {
		respond(h, dpkgAbsent, dpkg(name))
	} else {
		respond(h, apkAbsent, apkInfo(name))
	}
	return h
}

func simulate(t packages.Tool, name string) []string {
	if t == packages.Apt {
		return []string{"apt-get", "install", "-s", "-y", name}
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
	} {
		t.Run(c.name, func(t *testing.T) {
			h := hostfake.New(host.Options{})
			if c.tool == packages.Apt {
				respond(h, c.r, dpkg("ufw"))
			} else {
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
			if c.tool == packages.Apt {
				respond(h, c.after, dpkg("ufw"))
			} else {
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
