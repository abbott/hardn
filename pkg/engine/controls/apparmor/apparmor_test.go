package apparmor_test

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/abbott/hardn/pkg/engine"
	"github.com/abbott/hardn/pkg/engine/controls/apparmor"
	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/alpine"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/hostfake"
	"github.com/abbott/hardn/pkg/output"
)

// Nothing here runs a real command or reads the machine: every host is
// hostfake's, with the kernel's files and systemctl's answer as each case
// says. The paths are the kernel's, written out rather than taken from the
// package, so that a wrong path in the code fails here.
const (
	enabledParam = "/sys/module/apparmor/parameters/enabled"
	modeParam    = "/sys/module/apparmor/parameters/mode"
	profilesFile = "/sys/kernel/security/apparmor/profiles"
	isEnabled    = "systemctl is-enabled apparmor.service"
)

// platform is a supported release as detect sees it.
type platform struct {
	name  string // testdata/<name>/profiles
	facts detect.Facts
}

func debian13() platform {
	return platform{name: "debian13", facts: detect.Facts{
		OS:        detect.OSRelease{ID: "debian", VersionID: "13", VersionCodename: "trixie"},
		Platform:  detect.Platform{Distro: detect.DistroDebian, Version: "13", Support: detect.Supported},
		Init:      detect.Init{Kind: detect.Systemd, Running: detect.Yes, Source: detect.SystemdRunDir},
		Proxmox:   detect.Proxmox{Installed: detect.No, ClusterFS: detect.No, EtcPVE: detect.No},
		Container: detect.Container{Detected: detect.No},
	}}
}

func ubuntu2604() platform {
	p := debian13()
	p.name = "ubuntu2604"
	p.facts.OS = detect.OSRelease{ID: "ubuntu", VersionID: "26.04", VersionCodename: "resolute"}
	p.facts.Platform = detect.Platform{Distro: detect.DistroUbuntu, Version: "26.04", Support: detect.Supported}
	return p
}

func pve9() platform {
	p := debian13()
	p.name = "pve9"
	p.facts.Platform = detect.Platform{Distro: detect.DistroProxmox, Version: "9", Support: detect.Supported}
	p.facts.Proxmox = detect.Proxmox{Installed: detect.Yes, Version: "9.0.10", ClusterFS: detect.Yes, EtcPVE: detect.Yes}
	return p
}

func alpine324() platform {
	p := debian13()
	p.name = "alpine324"
	p.facts.OS = detect.OSRelease{ID: "alpine", VersionID: "3.24.0"}
	p.facts.Platform = detect.Platform{Distro: detect.DistroAlpine, Version: "3.24", Support: detect.Supported}
	p.facts.Init = detect.Init{Kind: detect.OpenRC, Running: detect.Yes, Source: detect.OpenRCRunDir}
	return p
}

// The kernel parameter's states, besides its content.
const (
	noParam  = "no parameter"      // /sys/module without apparmor: a kernel built without it
	noSys    = "no /sys/module"    // nothing under /sys/module at all
	onlyDir  = "module, no param"  // /sys/module/apparmor without its parameter
	noFile   = "no profiles file"  // securityfs not mounted
	noneRead = "no profile loaded" // the profiles file, empty
)

// site is one test's host: a platform with AppArmor on, enforcing its
// stock profiles, loaded at boot, unless a field says otherwise.
type site struct {
	plat     platform
	enabled  string // the parameter's content, or noParam, noSys, onlyDir; "" is "Y\n"
	profiles string // the file's content, or noFile, noneRead; "" is testdata/<plat>/profiles
	mode     string // the global mode as the parameter prints it; "" is "enforce", v6.18's, no newline
	unit     string // what systemctl is-enabled prints; "" is "enabled"
	unitRun  *hostfake.Response
	levels   []string // OpenRC: the runlevels apparmor is in; nil is boot
	cmdline  string
	deny     []string // paths a read, stat or lstat of fails with a permission error
	facts    func(*detect.Facts)
}

func (s site) build(t *testing.T) (host.System, *hostfake.System, detect.Facts) {
	t.Helper()
	f := hostfake.New(host.Options{})
	switch s.enabled {
	case "":
		f.AddFile(enabledParam, []byte("Y\n"), 0o444, 0, 0)
	case noParam:
		f.AddDir("/sys/module/kernel", 0o755, 0, 0)
	case noSys:
	case onlyDir:
		f.AddDir("/sys/module/apparmor", 0o755, 0, 0)
	default:
		f.AddFile(enabledParam, []byte(s.enabled), 0o444, 0, 0)
	}
	if s.enabled == "" || s.enabled == "Y\n" {
		mode := s.mode
		if mode == "" {
			mode = "enforce"
		}
		f.AddFile(modeParam, []byte(mode), 0o600, 0, 0)
	}
	switch s.profiles {
	case "":
		data, err := os.ReadFile(filepath.Join("testdata", s.plat.name, "profiles"))
		if err != nil {
			t.Fatal(err)
		}
		f.AddFile(profilesFile, data, 0o444, 0, 0)
	case noFile:
		f.AddDir("/sys/kernel/security", 0o755, 0, 0)
	case noneRead:
		f.AddFile(profilesFile, nil, 0o444, 0, 0)
	default:
		f.AddFile(profilesFile, []byte(s.profiles), 0o444, 0, 0)
	}
	cmd := s.cmdline
	if cmd == "" {
		cmd = "BOOT_IMAGE=/boot/vmlinuz root=/dev/vda1 ro quiet"
	}
	f.AddFile("/proc/cmdline", []byte(cmd+"\n"), 0o444, 0, 0)
	if s.plat.facts.Init.Kind == detect.OpenRC {
		f.AddFile("/etc/init.d/apparmor", []byte("#!/sbin/openrc-run\n"), 0o755, 0, 0)
		for _, l := range []string{"boot", "default", "nonetwork", "shutdown", "sysinit"} {
			f.AddDir(detect.OpenRCRunlevels+"/"+l, 0o755, 0, 0)
		}
		levels := s.levels
		if levels == nil {
			levels = []string{"boot"}
		}
		for _, l := range levels {
			f.AddSymlink(detect.OpenRCRunlevels+"/"+l+"/apparmor", "/etc/init.d/apparmor")
		}
	}
	f.Handler = func(argv []string, _ []byte) hostfake.Response {
		if cmd := strings.Join(argv, " "); cmd != isEnabled {
			t.Errorf("unexpected command %q", cmd)
			return hostfake.Response{Err: hostfake.ErrUnscripted}
		}
		if s.unitRun != nil {
			return *s.unitRun
		}
		st := s.unit
		if st == "" {
			st = "enabled"
		}
		code := 0
		if st != "enabled" {
			code = 1
		}
		return hostfake.Response{Stdout: st + "\n", ExitCode: code}
	}
	facts := s.plat.facts
	if s.facts != nil {
		s.facts(&facts)
	}
	return denied{System: f, paths: s.deny}, f, facts
}

// denied is a fake on which reads of paths fail as for a user other than
// root: the profiles list (EACCES) and the mode parameter (0600).
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

func (d denied) Lstat(ctx context.Context, name string) (fs.FileInfo, error) {
	if err := d.refuse("lstat", name); err != nil {
		return nil, err
	}
	return d.System.Lstat(ctx, name)
}

// row is one line the control shows on a host.
type row struct {
	name   string
	site   site
	state  engine.State
	detail string
	err    string // the error's code (pkg/output), "" for none
}

func run(t *testing.T, r row) engine.Status {
	t.Helper()
	sys, f, facts := r.site.build(t)
	st, err := apparmor.Enforce{}.Check(context.Background(), sys, facts)
	if code := codeOf(err); code != r.err {
		t.Errorf("error %v: code %q, want %q", err, code, r.err)
	}
	if err != nil && (st.State != engine.Unknown || st.Detail != "can't read") {
		t.Errorf("an error with %s %q: a failed read is Unknown, \"can't read\"", st.State, st.Detail)
	}
	if st.State != r.state || st.Detail != r.detail {
		t.Errorf("%s %q, want %s %q (evidence %q, error %v)", st.State, st.Detail, r.state, r.detail, st.Evidence, err)
	}
	if len(st.Findings) != 0 {
		t.Errorf("findings %v: this control has none", st.Findings)
	}
	readOnly(t, f)
	return st
}

func codeOf(err error) string {
	if err == nil {
		return ""
	}
	return output.CodeOf(err)
}

// readOnly fails for any call that changes the fake, and any command but
// systemctl is-enabled apparmor.service.
func readOnly(t *testing.T, f *hostfake.System) {
	t.Helper()
	for _, c := range f.Calls() {
		switch c.Op {
		case "read", "stat", "lstat", "readdir":
		case "run":
			if cmd := strings.Join(c.Argv, " "); cmd != isEnabled {
				t.Errorf("ran %q", cmd)
			}
		default:
			t.Errorf("a call that is not a read: %v", c)
		}
	}
}

// The lines agreed in P-43 (3), as the tests expect them.
const (
	logging    = "logging only"
	offRestart = "off after restart"
	noProfiles = "no profiles loaded"
	offKernel  = "off in kernel"
	byHost     = "set by the host"
	alpineDef  = "Alpine default"
	cantRead   = "can't read"
)

// TestLines: every line of the page's table (check-apparmor.md §5), on
// the platforms where it differs.
func TestLines(t *testing.T) {
	for _, r := range []row{
		// Configured: one profile or more enforcing, loaded at boot.
		{name: "debian13", site: site{plat: debian13()}, state: engine.Configured, detail: "7 profiles enforced"},
		{name: "ubuntu2604, complain and unconfined profiles beside", site: site{plat: ubuntu2604()}, state: engine.Configured, detail: "15 profiles enforced"},
		{name: "pve9 with LXC's profiles", site: site{plat: pve9()}, state: engine.Configured, detail: "16 profiles enforced"},
		{name: "alpine324 with AppArmor, apparmor in boot", site: site{plat: alpine324()}, state: engine.Configured, detail: "2 profiles enforced"},
		{name: "alpine324 with AppArmor, apparmor in default", site: site{plat: alpine324(), levels: []string{"default"}}, state: engine.Configured, detail: "2 profiles enforced"},
		{name: "one profile", site: site{plat: debian13(), profiles: "/usr/bin/man (enforce)\n"}, state: engine.Configured, detail: "1 profile enforced"},
		{name: "kill mode counts as enforcing", site: site{plat: debian13(), profiles: "/usr/bin/man (kill)\nlsb_release (complain)\n"}, state: engine.Configured, detail: "1 profile enforced"},
		{name: "a mode hardn does not know beside enforcing ones", site: site{plat: ubuntu2604(), profiles: "/usr/bin/man (enforce)\nsnap.app (prompt)\n"}, state: engine.Configured, detail: "1 profile enforced"},
		{name: "the mode with a newline", site: site{plat: debian13(), mode: "enforce\n"}, state: engine.Configured, detail: "7 profiles enforced"},
		{name: "distribution not established, LSM on", site: site{plat: debian13(), facts: func(f *detect.Facts) { f.Platform = detect.Platform{Err: detect.ErrNoSignal} }},
			state: engine.Configured, detail: "7 profiles enforced"},

		// Partial, logging only: none enforcing, or the global mode complain.
		{name: "every profile in complain mode", site: site{plat: debian13(), profiles: "/usr/bin/man (complain)\nlsb_release (complain)\n"}, state: engine.Partial, detail: logging},
		{name: "unconfined and user modes only", site: site{plat: ubuntu2604(), profiles: "busybox (unconfined)\nchrome (unconfined)\nfoo (user)\n"}, state: engine.Partial, detail: logging},
		{name: "global mode complain", site: site{plat: ubuntu2604(), mode: "complain"}, state: engine.Partial, detail: logging},
		{name: "global mode complain, pve9", site: site{plat: pve9(), mode: "complain\n"}, state: engine.Partial, detail: logging},
		{name: "logging only before off after restart", site: site{plat: debian13(), mode: "complain", unit: "disabled"}, state: engine.Partial, detail: logging},
		{name: "alpine324, complain only", site: site{plat: alpine324(), profiles: "ping (complain)\n"}, state: engine.Partial, detail: logging},

		// Partial, off after restart: profiles loaded, the service not loading them at boot.
		{name: "debian13, apparmor.service disabled", site: site{plat: debian13(), unit: "disabled"}, state: engine.Partial, detail: offRestart},
		{name: "ubuntu2604, apparmor.service masked", site: site{plat: ubuntu2604(), unit: "masked"}, state: engine.Partial, detail: offRestart},
		{name: "pve9, apparmor.service disabled", site: site{plat: pve9(), unit: "disabled"}, state: engine.Partial, detail: offRestart},
		{name: "debian13, no apparmor.service", site: site{plat: debian13(), unit: "not-found"}, state: engine.Partial, detail: offRestart},
		{name: "alpine324, apparmor in no runlevel", site: site{plat: alpine324(), levels: []string{}}, state: engine.Partial, detail: offRestart},

		// Not Configured, no profiles loaded: the LSM on, nothing loaded.
		{name: "debian13 without the apparmor package", site: site{plat: debian13(), profiles: noneRead, unit: "not-found"}, state: engine.NotConfigured, detail: noProfiles},
		{name: "ubuntu2604, none loaded", site: site{plat: ubuntu2604(), profiles: noneRead}, state: engine.NotConfigured, detail: noProfiles},
		{name: "none loaded, the mode unreadable", site: site{plat: debian13(), profiles: noneRead, deny: []string{modeParam}}, state: engine.NotConfigured, detail: noProfiles},
		{name: "alpine324 with AppArmor, none loaded", site: site{plat: alpine324(), profiles: noneRead}, state: engine.NotConfigured, detail: noProfiles},

		// Not Configured, off in kernel.
		{name: "debian13, apparmor=0", site: site{plat: debian13(), enabled: "N\n", cmdline: "root=/dev/vda1 ro apparmor=0"}, state: engine.NotConfigured, detail: offKernel},
		{name: "ubuntu2604, an lsm= without it", site: site{plat: ubuntu2604(), enabled: "N\n", cmdline: "root=/dev/vda1 lsm=landlock,lockdown,yama,integrity"}, state: engine.NotConfigured, detail: offKernel},
		{name: "pve9, apparmor=0", site: site{plat: pve9(), enabled: "N\n", cmdline: "root=/dev/mapper/pve-root ro apparmor=0"}, state: engine.NotConfigured, detail: offKernel},
		{name: "a kernel built without it", site: site{plat: debian13(), enabled: noParam}, state: engine.NotConfigured, detail: offKernel},
		{name: "off, the command line unreadable", site: site{plat: debian13(), enabled: "N\n", deny: []string{"/proc/cmdline"}}, state: engine.NotConfigured, detail: offKernel},
		{name: "Debian whose Proxmox VE is not established, off", site: site{plat: debian13(), enabled: "N\n", facts: func(f *detect.Facts) {
			f.Platform = detect.Platform{Err: fmt.Errorf("detect: platform: Debian or Proxmox VE: %w", detect.ErrNoSignal)}
		}}, state: engine.NotConfigured, detail: offKernel},

		// Not Applicable, Alpine default.
		{name: "alpine324 stock", site: site{plat: alpine324(), enabled: "N\n"}, state: engine.NotApplicable, detail: alpineDef},
		{name: "alpine324, a kernel built without it", site: site{plat: alpine324(), enabled: noParam}, state: engine.NotApplicable, detail: alpineDef},

		// Not Applicable, set by the host: Check alike when called without Applies.
		{name: "a container", site: site{plat: debian13(), facts: inContainer}, state: engine.NotApplicable, detail: byHost},

		// Unknown, can't read.
		{name: "the profiles unreadable (not root)", site: site{plat: debian13(), deny: []string{profilesFile}}, state: engine.Unknown, detail: cantRead, err: "PERMISSION_DENIED"},
		{name: "the mode unreadable (not root)", site: site{plat: ubuntu2604(), deny: []string{modeParam}}, state: engine.Unknown, detail: cantRead, err: "PERMISSION_DENIED"},
		{name: "the parameter unreadable", site: site{plat: pve9(), deny: []string{enabledParam}}, state: engine.Unknown, detail: cantRead, err: "PERMISSION_DENIED"},
		{name: "alpine324, the parameter unreadable", site: site{plat: alpine324(), deny: []string{enabledParam}}, state: engine.Unknown, detail: cantRead, err: "PERMISSION_DENIED"},
		{name: "securityfs not mounted", site: site{plat: debian13(), profiles: noFile}, state: engine.Unknown, detail: cantRead, err: "FILE_NOT_FOUND"},
		{name: "the parameter unrecognised", site: site{plat: debian13(), enabled: "1\n"}, state: engine.Unknown, detail: cantRead, err: "DETECT_UNRECOGNISED"},
		{name: "alpine324, the parameter unrecognised", site: site{plat: alpine324(), enabled: "1\n"}, state: engine.Unknown, detail: cantRead, err: "DETECT_UNRECOGNISED"},
		{name: "the module without its parameter", site: site{plat: debian13(), enabled: onlyDir}, state: engine.Unknown, detail: cantRead, err: "DETECT_UNRECOGNISED"},
		{name: "no /sys/module", site: site{plat: ubuntu2604(), enabled: noSys}, state: engine.Unknown, detail: cantRead, err: "FILE_NOT_FOUND"},
		{name: "alpine324, no /sys/module", site: site{plat: alpine324(), enabled: noSys}, state: engine.Unknown, detail: cantRead, err: "FILE_NOT_FOUND"},
		{name: "a global mode hardn does not rank", site: site{plat: debian13(), mode: "kill"}, state: engine.Unknown, detail: cantRead, err: "DETECT_UNRECOGNISED"},
		{name: "modes hardn does not know, none enforcing", site: site{plat: ubuntu2604(), profiles: "snap.app (prompt)\nchrome (unconfined)\n"}, state: engine.Unknown, detail: cantRead, err: "DETECT_UNRECOGNISED"},
		{name: "a profiles line in another form", site: site{plat: debian13(), profiles: "/usr/bin/man (enforce)\n/usr/bin/foo\n"}, state: engine.Unknown, detail: cantRead, err: "DETECT_UNRECOGNISED"},
		{name: "a blank profiles line", site: site{plat: debian13(), profiles: "/usr/bin/man (enforce)\n\nlsb_release (enforce)\n"}, state: engine.Unknown, detail: cantRead, err: "DETECT_UNRECOGNISED"},
		{name: "apparmor.service static", site: site{plat: debian13(), unit: "static"}, state: engine.Unknown, detail: cantRead, err: "DETECT_UNRECOGNISED"},
		{name: "systemctl prints a state hardn does not know", site: site{plat: debian13(), unit: "frobnicated"}, state: engine.Unknown, detail: cantRead, err: "DETECT_UNRECOGNISED"},
		{name: "systemctl not installed", site: site{plat: debian13(), unitRun: &hostfake.Response{Err: fmt.Errorf("host: run systemctl: %w", host.ErrNotFound)}},
			state: engine.Unknown, detail: cantRead, err: "COMMAND_NOT_FOUND"},
		{name: "the init system not established", site: site{plat: debian13(), facts: func(f *detect.Facts) {
			f.Init = detect.Init{Err: fmt.Errorf("detect: init: %w", detect.ErrConflict)}
		}}, state: engine.Unknown, detail: cantRead, err: "DETECT_SIGNALS_DISAGREE"},
		{name: "alpine324, apparmor only in a runlevel of its own", site: site{plat: alpine324(), levels: []string{"nonetwork"}}, state: engine.Unknown, detail: cantRead, err: "DETECT_UNRECOGNISED"},
		{name: "alpine324, a runlevel unreadable", site: site{plat: alpine324(), deny: []string{detect.OpenRCRunlevels + "/boot/apparmor"}}, state: engine.Unknown, detail: cantRead, err: "PERMISSION_DENIED"},
		{name: "the container check not established", site: site{plat: debian13(), facts: func(f *detect.Facts) {
			f.Container = detect.Container{Err: fmt.Errorf("detect: container: %w", &fs.PathError{Op: "open", Path: detect.InitEnviron, Err: fs.ErrPermission})}
		}}, state: engine.Unknown, detail: cantRead, err: "PERMISSION_DENIED"},
		{name: "the distribution not established, LSM off", site: site{plat: debian13(), enabled: "N\n", facts: func(f *detect.Facts) {
			f.OS, f.Platform = detect.OSRelease{Err: detect.ErrNoSignal}, detect.Platform{Err: fmt.Errorf("detect: platform: %w", detect.ErrNoSignal)}
		}}, state: engine.Unknown, detail: cantRead, err: "DETECT_NO_SIGNAL"},
	} {
		t.Run(r.name, func(t *testing.T) { run(t, r) })
	}
}

var inContainer = func(f *detect.Facts) {
	f.Container = detect.Container{Detected: detect.Yes, Signal: detect.SystemdContainerFile, Manager: "lxc"}
}

// TestStock: each platform through Engine.Check over Controls(), as the
// main menu will run it; in a container nothing is read.
func TestStock(t *testing.T) {
	for _, c := range []struct {
		name   string
		site   site
		state  engine.State
		detail string
	}{
		{"debian13", site{plat: debian13()}, engine.Configured, "7 profiles enforced"},
		{"ubuntu2604", site{plat: ubuntu2604()}, engine.Configured, "15 profiles enforced"},
		{"pve9", site{plat: pve9()}, engine.Configured, "16 profiles enforced"},
		{"alpine324", site{plat: alpine324(), enabled: "N\n"}, engine.NotApplicable, alpineDef},
		{"alpine324 with AppArmor", site{plat: alpine324()}, engine.Configured, "2 profiles enforced"},
		{"debian13 in a container", site{plat: debian13(), facts: inContainer}, engine.NotApplicable, byHost},
		{"alpine324 in a container", site{plat: alpine324(), enabled: "N\n", facts: inContainer}, engine.NotApplicable, byHost},
	} {
		t.Run(c.name, func(t *testing.T) {
			sys, f, facts := c.site.build(t)
			e := engine.New(sys, engine.Options{Detect: func(context.Context, host.System) (detect.Facts, error) { return facts, nil }})
			reps, err := e.Check(context.Background(), apparmor.Controls())
			if err != nil {
				t.Fatal(err)
			}
			if len(reps) != 1 {
				t.Fatalf("%d reports", len(reps))
			}
			rep := reps[0]
			if rep.Control != "apparmor.enforce" || rep.Title != "AppArmor" || rep.Err != nil {
				t.Errorf("report: %s %q, error %v", rep.Control, rep.Title, rep.Err)
			}
			if rep.Status.State != c.state || rep.Status.Detail != c.detail {
				t.Errorf("%s %q, want %s %q", rep.Status.State, rep.Status.Detail, c.state, c.detail)
			}
			if c.state != engine.NotApplicable && len(rep.Status.Evidence) == 0 {
				t.Error("no evidence")
			}
			readOnly(t, f)
			if c.detail == byHost && len(f.Calls()) != 0 {
				t.Errorf("in a container, read %v", f.Calls())
			}
		})
	}
}

// TestEvidence: what each line was read from, as the kernel and systemctl
// printed it.
func TestEvidence(t *testing.T) {
	for _, c := range []struct {
		r    row
		want []string
	}{
		{row{site: site{plat: debian13()}, state: engine.Configured, detail: "7 profiles enforced"}, []string{
			enabledParam + `: "Y\n"`, profilesFile + ": 7 enforce", modeParam + `: "enforce"`, isEnabled + ": enabled"}},
		{row{site: site{plat: ubuntu2604()}, state: engine.Configured, detail: "15 profiles enforced"}, []string{
			enabledParam + `: "Y\n"`, profilesFile + ": 15 enforce, 1 complain, 4 unconfined", modeParam + `: "enforce"`, isEnabled + ": enabled"}},
		{row{site: site{plat: debian13(), profiles: "a (enforce)\nb (prompt)\nc (kill)\n"}, state: engine.Configured, detail: "2 profiles enforced"}, []string{
			enabledParam + `: "Y\n"`, profilesFile + ": 1 enforce, 1 kill, 1 prompt", modeParam + `: "enforce"`, isEnabled + ": enabled"}},
		{row{site: site{plat: alpine324(), levels: []string{}}, state: engine.Partial, detail: offRestart}, []string{
			enabledParam + `: "Y\n"`, profilesFile + ": 2 enforce, 1 complain", modeParam + `: "enforce"`, "/etc/runlevels: apparmor in none"}},
		{row{site: site{plat: debian13(), profiles: noneRead}, state: engine.NotConfigured, detail: noProfiles}, []string{
			enabledParam + `: "Y\n"`, profilesFile + ": none"}},
		{row{site: site{plat: debian13(), enabled: "N\n", cmdline: "root=/dev/vda1 ro apparmor=0"}, state: engine.NotConfigured, detail: offKernel}, []string{
			enabledParam + `: "N\n"`, "/proc/cmdline: apparmor=0"}},
		{row{site: site{plat: debian13(), enabled: "N\n"}, state: engine.NotConfigured, detail: offKernel}, []string{
			enabledParam + `: "N\n"`, "/proc/cmdline: no apparmor=, lsm= or security="}},
		{row{site: site{plat: debian13(), enabled: noParam, deny: []string{"/proc/cmdline"}}, state: engine.NotConfigured, detail: offKernel}, []string{
			"/sys/module/apparmor: absent", "/proc/cmdline: not read"}},
		{row{site: site{plat: alpine324(), enabled: "N\n"}, state: engine.NotApplicable, detail: alpineDef}, []string{enabledParam + `: "N\n"`}},
		{row{site: site{plat: alpine324(), enabled: noParam}, state: engine.NotApplicable, detail: alpineDef}, []string{"/sys/module/apparmor: absent"}},
	} {
		t.Run(c.r.detail, func(t *testing.T) {
			if st := run(t, c.r); !slices.Equal(st.Evidence, c.want) {
				t.Errorf("evidence %q, want %q", st.Evidence, c.want)
			}
		})
	}
}

// TestSameAsAlpine: off Alpine the control reads the kernel's parameter by
// alpine.CheckAppArmor's rules, so the same file gives the same answer
// and the same code on every platform.
func TestSameAsAlpine(t *testing.T) {
	for _, enabled := range []string{"Y\n", "N\n", noParam, noSys, onlyDir, "1\n", "Y"} {
		t.Run(strings.TrimSpace(enabled), func(t *testing.T) {
			s := site{plat: debian13(), enabled: enabled}
			sys, _, facts := s.build(t)
			st, err := apparmor.Enforce{}.Check(context.Background(), sys, facts)
			a, aerr := alpine.CheckAppArmor(context.Background(), sys, alpine324().facts)
			if aerr != nil {
				t.Fatal(aerr)
			}
			want := map[detect.Tri]engine.State{detect.Yes: engine.Configured, detect.No: engine.NotConfigured, detect.Unknown: engine.Unknown}[a.Enabled]
			if st.State != want || codeOf(err) != codeOf(a.Err) {
				t.Errorf("Check: %s (%v); alpine.CheckAppArmor: Enabled %s (%v)", st.State, err, a.Enabled, a.Err)
			}
		})
	}
}

// TestEnforced: the count a Configured status carries, for the risk level;
// nothing for another status.
func TestEnforced(t *testing.T) {
	for _, c := range []struct {
		st engine.Status
		n  int
		ok bool
	}{
		{engine.Status{State: engine.Configured, Detail: "34 profiles enforced"}, 34, true},
		{engine.Status{State: engine.Configured, Detail: "1 profile enforced"}, 1, true},
		{engine.Status{State: engine.Configured, Detail: "1 profiles enforced"}, 0, false},
		{engine.Status{State: engine.Configured, Detail: "034 profiles enforced"}, 0, false},
		{engine.Status{State: engine.Partial, Detail: "34 profiles enforced"}, 0, false},
		{engine.Status{State: engine.Partial, Detail: logging}, 0, false},
		{engine.Status{State: engine.Unknown, Detail: cantRead}, 0, false},
	} {
		if n, ok := apparmor.Enforced(c.st); n != c.n || ok != c.ok {
			t.Errorf("Enforced(%s %q) = %d, %t; want %d, %t", c.st.State, c.st.Detail, n, ok, c.n, c.ok)
		}
	}
	st := run(t, row{site: site{plat: pve9()}, state: engine.Configured, detail: "16 profiles enforced"})
	if n, ok := apparmor.Enforced(st); n != 16 || !ok {
		t.Errorf("Enforced of the check's own status: %d, %t", n, ok)
	}
}

// TestCheckOnly: Plan is ErrNotMigrated, and a plan with the control is
// refused with it (CONTROL_NOT_MIGRATED), changing nothing.
func TestCheckOnly(t *testing.T) {
	sys, f, facts := site{plat: debian13()}.build(t)
	cs, err := apparmor.Enforce{}.Plan(context.Background(), sys, engine.Input{Facts: facts})
	if !errors.Is(err, engine.ErrNotMigrated) || cs != nil {
		t.Errorf("Plan %v, %v", cs, err)
	}
	f.AddDir("/run", 0o755, 0, 0)
	e := engine.New(sys, engine.Options{Detect: func(context.Context, host.System) (detect.Facts, error) { return facts, nil }})
	p, err := e.Plan(context.Background(), apparmor.Controls(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if !p.Empty() || !errors.Is(p.Err(), engine.ErrRefused) || !errors.Is(p.Err(), engine.ErrNotMigrated) || len(p.Refused) != 1 {
		t.Fatalf("plan: %d changes, refused %v", len(p.Changes), p.Refused)
	}
	for _, it := range p.Items {
		if code := output.CodeOf(it.Err); code != "CONTROL_NOT_MIGRATED" {
			t.Errorf("%s: %v, code %s", it.Control, it.Err, code)
		}
	}
	for _, c := range f.Calls() {
		if c.Op != "mkdir" && c.Op != "removeall" && c.Op != "lstat" && c.Op != "stat" {
			t.Errorf("plan: %v", c)
		}
	}
}
