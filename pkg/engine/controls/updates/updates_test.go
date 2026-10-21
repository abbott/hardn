package updates_test

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
	"github.com/abbott/hardn/pkg/engine/controls/updates"
	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/alpine"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/hostfake"
	"github.com/abbott/hardn/pkg/output"
)

// Nothing here runs a real command or reads the machine: every host is
// hostfake's, with apt-config's, dpkg-query's, systemctl's, apk's and
// rc-service's answers as each case says. The commands and paths are
// written out rather than taken from the package, so that a wrong one in
// the code fails here.
const (
	dumpCmd     = "apt-config dump APT::Periodic Unattended-Upgrade::Origins-Pattern Unattended-Upgrade::Allowed-Origins"
	dpkgCmd     = "dpkg-query --show --showformat=${Status}\\t${Version}\\n unattended-upgrades"
	upActive    = "systemctl is-active apt-daily-upgrade.timer"
	upEnabled   = "systemctl is-enabled apt-daily-upgrade.timer"
	listActive  = "systemctl is-active apt-daily.timer"
	listEnabled = "systemctl is-enabled apt-daily.timer"
	upJob       = "systemctl is-enabled apt-daily-upgrade.service"
	listJob     = "systemctl is-enabled apt-daily.service"
	apkInfo     = "apk info -e apk-cron"
	crondStatus = "rc-service crond status"

	apkScript    = "/etc/periodic/daily/apk"
	crondInit    = "/etc/init.d/crond"
	crondLink    = "/etc/runlevels/default/crond"
	legacyScript = "/etc/periodic/daily/apk-upgrade"
)

// platform is a supported release as detect sees it.
type platform struct {
	name  string // testdata/<name>/apt-config-dump
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

// Values of a site's fields besides a command's output.
const (
	empty   = "empty"   // apt-config dump prints nothing: none of the subtrees set
	gone    = "gone"    // dpkg-query: no such package (exit 1)
	absent  = "absent"  // Alpine: no file there
	plain   = "plain"   // Alpine: apk-cron's script without its execute bit
	missing = "missing" // Alpine: no /etc/init.d/crond
)

// site is one test's host: a platform with automatic updates on, as its
// stock install turns them on (apt: unattended-upgrades installed, both
// timers enabled and active, their services static; Alpine: apk-cron installed, crond started and
// in the default runlevel), unless a field says otherwise.
type site struct {
	plat    platform
	dump    string            // apt-config dump's output; "" is testdata/<plat>/apt-config-dump, empty none
	pkg     string            // dpkg-query's ${Status}; "" is "install ok installed", gone exit 1
	units   map[string]string // systemctl's state by command; absent: active, enabled, static
	runs    map[string]*hostfake.Response
	apk     string   // Alpine: apk-cron "" installed, gone not installed
	script  string   // Alpine: "" executable, absent, plain
	crond   string   // Alpine: "" started, stopped, crashed, missing, or an exit code as "exit N"
	inLevel *bool    // Alpine: crond in the default runlevel; nil is yes
	legacy  string   // Alpine: "" none, "hardn" origin/main's file, "other"
	deny    []string // paths a read, stat or lstat of fails with a permission error
	facts   func(*detect.Facts)
}

func no() *bool { b := false; return &b }

func (s site) build(t *testing.T) (host.System, *hostfake.System, detect.Facts) {
	t.Helper()
	f := hostfake.New(host.Options{})
	if s.plat.facts.Platform.Distro == detect.DistroAlpine {
		f.AddDir("/etc/periodic/daily", 0o755, 0, 0)
		f.AddDir("/etc/runlevels/default", 0o755, 0, 0)
		switch s.script {
		case "":
			f.AddFile(apkScript, []byte("#!/bin/sh\n\nset -eu\n\nsleep $(expr $RANDOM % 7200)\nexec apk -U upgrade\n"), 0o755, 0, 0)
		case plain:
			f.AddFile(apkScript, []byte("#!/bin/sh\nexec apk -U upgrade\n"), 0o644, 0, 0)
		}
		if s.crond != missing {
			f.AddFile(crondInit, []byte("#!/sbin/openrc-run\n"), 0o755, 0, 0)
			if s.inLevel == nil || *s.inLevel {
				f.AddSymlink(crondLink, crondInit)
			}
		}
		switch s.legacy {
		case "hardn":
			f.AddFile(legacyScript, []byte(alpine.LegacyContent), 0o755, 0, 0)
		case "other":
			f.AddFile(legacyScript, []byte("#!/bin/sh\napk upgrade\n"), 0o755, 0, 0)
		}
	}
	dump := s.dump
	switch dump {
	case "":
		if s.plat.facts.Platform.Distro != detect.DistroAlpine {
			data, err := os.ReadFile(filepath.Join("testdata", s.plat.name, "apt-config-dump"))
			if err != nil {
				t.Fatal(err)
			}
			dump = string(data)
		}
	case empty:
		dump = ""
	}
	f.Handler = func(argv []string, _ []byte) hostfake.Response {
		cmd := strings.Join(argv, " ")
		if r := s.runs[cmd]; r != nil {
			return *r
		}
		switch cmd {
		case dumpCmd:
			return hostfake.Response{Stdout: dump}
		case dpkgCmd:
			switch s.pkg {
			case "":
				return hostfake.Response{Stdout: "install ok installed\t2.12\n"}
			case gone:
				return hostfake.Response{Stderr: "dpkg-query: no packages found matching unattended-upgrades\n", ExitCode: 1}
			}
			return hostfake.Response{Stdout: s.pkg + "\t2.12\n"}
		case upActive, listActive, upEnabled, listEnabled, upJob, listJob:
			st, ok := s.units[cmd]
			switch {
			case ok:
			case strings.Contains(cmd, "is-active"):
				st = "active"
			case strings.HasSuffix(cmd, ".service"):
				st = "static"
			default:
				st = "enabled"
			}
			return hostfake.Response{Stdout: st + "\n", ExitCode: exitOf(st)}
		case apkInfo:
			if s.apk == gone {
				return hostfake.Response{ExitCode: 1}
			}
			return hostfake.Response{Stdout: "apk-cron\n"}
		case crondStatus:
			var code int
			switch s.crond {
			case "", missing:
			case "stopped":
				code = 3
			case "crashed":
				code = 32
			default:
				if _, err := fmt.Sscanf(s.crond, "exit %d", &code); err != nil {
					t.Fatalf("crond %q", s.crond)
				}
			}
			return hostfake.Response{ExitCode: code}
		}
		t.Errorf("unexpected command %q", cmd)
		return hostfake.Response{Err: hostfake.ErrUnscripted}
	}
	facts := s.plat.facts
	if s.facts != nil {
		s.facts(&facts)
	}
	return denied{System: f, paths: s.deny}, f, facts
}

// exitOf is systemctl's exit status for a state it prints (systemctl(1)):
// is-active exits 0 for active only; is-enabled 0 for enabled,
// enabled-runtime, alias, static, indirect, generated and transient, 4
// for not-found.
func exitOf(state string) int {
	switch state {
	case "active", "enabled", "enabled-runtime", "alias", "static", "indirect", "generated", "transient":
		return 0
	case "not-found":
		return 4
	case "inactive", "failed", "activating", "deactivating", "maintenance", "reloading", "refreshing":
		return 3
	}
	return 1
}

// denied is a fake on which reads of paths fail as for a user other than
// root.
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
	name     string
	site     site
	state    engine.State
	detail   string
	err      string   // the error's code (pkg/output), "" for none
	findings []string // the findings' codes, in order
}

func run(t *testing.T, r row) engine.Status {
	t.Helper()
	sys, f, facts := r.site.build(t)
	st, err := updates.Auto{}.Check(context.Background(), sys, facts)
	if code := codeOf(err); code != r.err {
		t.Errorf("error %v: code %q, want %q", err, code, r.err)
	}
	if err != nil && (st.State != engine.Unknown || st.Detail != "can't read") {
		t.Errorf("an error with %s %q: a failed read is Unknown, \"can't read\"", st.State, st.Detail)
	}
	if st.State != r.state || st.Detail != r.detail {
		t.Errorf("%s %q, want %s %q (evidence %q, error %v)", st.State, st.Detail, r.state, r.detail, st.Evidence, err)
	}
	var codes []string
	for _, fd := range st.Findings {
		codes = append(codes, fd.Code)
		if e, ok := output.Lookup(fd.Code); !ok || e.Kind != output.KindFinding {
			t.Errorf("finding %s: not a registered finding code", fd.Code)
		}
	}
	if !slices.Equal(codes, r.findings) {
		t.Errorf("findings %v, want %v", st.Findings, r.findings)
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
// the queries the control makes.
func readOnly(t *testing.T, f *hostfake.System) {
	t.Helper()
	for _, c := range f.Calls() {
		switch c.Op {
		case "read", "read-nofollow", "stat", "lstat", "readdir":
		case "run":
			switch cmd := strings.Join(c.Argv, " "); cmd {
			case dumpCmd, dpkgCmd, upActive, upEnabled, upJob, listActive, listEnabled, listJob, apkInfo, crondStatus:
			default:
				t.Errorf("ran %q", cmd)
			}
		default:
			t.Errorf("a call that is not a read: %v", c)
		}
	}
}

// The details, as the tests expect them (check-updates.md §5).
const (
	unattended = "unattended-upgrades"
	apkCron    = "apk-cron"
	stale      = "lists not refreshed"
	offRestart = "off after restart"
	noPkg      = "no unattended-upgrades"
	upOff      = "upgrades off"
	timerOff   = "upgrade timer off"
	jobMasked  = "upgrade job masked"
	noApkCron  = "no apk-cron"
	notRun     = "daily script not run"
	crondOff   = "crond not running"
	cantRead   = "can't read"
)

// Finding codes.
const (
	valueUnknown    = "UPDATES_APT_VALUE_UNKNOWN"
	scheduleUnknown = "UPDATES_SCHEDULE_UNKNOWN"
	proxmox         = "UPDATES_PROXMOX_NOT_UPGRADED"
)

// with is a dump with lines replaced or added: each "Name value" pair
// sets Name's line, "" as the value drops it.
func with(plat platform, sets ...string) string {
	data, err := os.ReadFile(filepath.Join("testdata", plat.name, "apt-config-dump"))
	if err != nil {
		panic(err)
	}
	lines := strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
	for i := 0; i+1 < len(sets); i += 2 {
		name, value := sets[i], sets[i+1]
		j := slices.IndexFunc(lines, func(l string) bool { return strings.HasPrefix(l, name+` "`) })
		switch {
		case value == "" && j >= 0:
			lines = slices.Delete(lines, j, j+1)
		case value == "":
		case j >= 0:
			lines[j] = name + " " + value + ";"
		default:
			lines = slices.Insert(lines, 1, name+" "+value+";")
		}
	}
	return strings.Join(lines, "\n") + "\n"
}

func units(kv ...string) map[string]string {
	m := map[string]string{}
	for i := 0; i+1 < len(kv); i += 2 {
		m[kv[i]] = kv[i+1]
	}
	return m
}

func fails(r hostfake.Response, cmds ...string) map[string]*hostfake.Response {
	m := map[string]*hostfake.Response{}
	for _, c := range cmds {
		m[c] = &r
	}
	return m
}

var (
	notFound = hostfake.Response{Err: fmt.Errorf("host: run: %w", host.ErrNotFound)}
	stock    = site{dump: empty, pkg: gone} // apt: no unattended-upgrades, nothing set, the timers on
)

func stockOn(p platform) site { s := stock; s.plat = p; return s }

const (
	enable  = "APT::Periodic::Enable"
	upgrade = "APT::Periodic::Unattended-Upgrade"
	lists   = "APT::Periodic::Update-Package-Lists"
)

// TestLines: every line of the page's table (check-updates.md §5), on the
// platforms where it differs.
func TestLines(t *testing.T) {
	for _, r := range []row{
		// Configured: unattended-upgrades runs, the lists refreshed.
		{name: "debian13", site: site{plat: debian13()}, state: engine.Configured, detail: unattended},
		{name: "ubuntu2604", site: site{plat: ubuntu2604()}, state: engine.Configured, detail: unattended},
		{name: "pve9, Debian's origins", site: site{plat: pve9()}, state: engine.Configured, detail: unattended, findings: []string{proxmox}},
		{name: "pve9, an origin naming Proxmox", site: site{plat: pve9(), dump: with(pve9(), "Unattended-Upgrade::Origins-Pattern::", "") + "Unattended-Upgrade::Origins-Pattern:: \"origin=Proxmox,codename=${distro_codename}\";\n"},
			state: engine.Configured, detail: unattended},
		{name: "weekly upgrades, lists always", site: site{plat: debian13(), dump: with(debian13(), upgrade, `"7"`, lists, `"always"`)}, state: engine.Configured, detail: unattended},
		{name: "intervals with units", site: site{plat: debian13(), dump: with(debian13(), upgrade, `"12h"`, lists, `"0d"`)}, state: engine.Configured, detail: unattended},
		{name: "00 is not 0 for an interval", site: site{plat: debian13(), dump: with(debian13(), lists, `"00"`)}, state: engine.Configured, detail: unattended},
		{name: "Enable 1", site: site{plat: debian13(), dump: with(debian13(), enable, `"1"`)}, state: engine.Configured, detail: unattended},
		{name: "names in another case", site: site{plat: debian13(), dump: "apt::periodic \"\";\napt::periodic::update-package-lists \"1\";\napt::Periodic::UNATTENDED-UPGRADE \"1\";\n"},
			state: engine.Configured, detail: unattended},
		{name: "a Debian derivative", site: site{plat: debian13(), facts: func(f *detect.Facts) {
			f.OS = detect.OSRelease{ID: "raspbian", IDLike: []string{"debian"}}
			f.Platform = detect.Platform{Distro: detect.DistroOther, Like: detect.DistroDebian, Support: detect.Unlisted}
		}}, state: engine.Configured, detail: unattended},
		{name: "Debian whose Proxmox VE is not established", site: site{plat: debian13(), facts: notPVE}, state: engine.Configured, detail: unattended},
		{name: "in a container", site: site{plat: debian13(), facts: inContainer}, state: engine.Configured, detail: unattended},

		// Partial: upgrades run, the lists not refreshed or a timer off after restart.
		{name: "Update-Package-Lists 0", site: site{plat: debian13(), dump: with(debian13(), lists, `"0"`)}, state: engine.Partial, detail: stale},
		{name: "Update-Package-Lists unset", site: site{plat: ubuntu2604(), dump: with(ubuntu2604(), lists, "")}, state: engine.Partial, detail: stale},
		{name: "apt-daily.timer stopped", site: site{plat: debian13(), units: units(listActive, "inactive")}, state: engine.Partial, detail: stale},
		{name: "apt-daily.timer masked", site: site{plat: ubuntu2604(), units: units(listActive, "inactive", listEnabled, "masked")}, state: engine.Partial, detail: stale},
		{name: "apt-daily.service masked", site: site{plat: debian13(), units: units(listJob, "masked")}, state: engine.Partial, detail: stale},
		{name: "apt-daily.service masked for this boot", site: site{plat: debian13(), units: units(listJob, "masked-runtime")}, state: engine.Partial, detail: stale},
		{name: "apt-daily-upgrade.timer disabled, running", site: site{plat: debian13(), units: units(upEnabled, "disabled")}, state: engine.Partial, detail: offRestart},
		{name: "apt-daily.timer disabled, running", site: site{plat: ubuntu2604(), units: units(listEnabled, "disabled")}, state: engine.Partial, detail: offRestart},
		{name: "lists before off after restart", site: site{plat: debian13(), dump: with(debian13(), lists, `"0"`), units: units(upEnabled, "disabled")}, state: engine.Partial, detail: stale},
		{name: "pve9, lists not refreshed", site: site{plat: pve9(), dump: with(pve9(), lists, `"0"`)}, state: engine.Partial, detail: stale, findings: []string{proxmox}},
		{name: "a lists value not ranked, a timer off after restart", site: site{plat: debian13(), dump: with(debian13(), lists, `"weekly"`), units: units(upEnabled, "disabled")},
			state: engine.Partial, detail: offRestart},

		// Not Configured: no upgrade runs.
		{name: "debian13 stock", site: stockOn(debian13()), state: engine.NotConfigured, detail: noPkg},
		{name: "pve9 stock", site: stockOn(pve9()), state: engine.NotConfigured, detail: noPkg},
		{name: "removed, its files kept", site: site{plat: debian13(), pkg: "deinstall ok config-files"}, state: engine.NotConfigured, detail: noPkg},
		{name: "no dpkg-query", site: site{plat: debian13(), runs: fails(notFound, dpkgCmd)}, state: engine.NotConfigured, detail: noPkg},
		{name: "Unattended-Upgrade 0", site: site{plat: ubuntu2604(), dump: with(ubuntu2604(), upgrade, `"0"`)}, state: engine.NotConfigured, detail: upOff},
		{name: "Unattended-Upgrade unset", site: site{plat: debian13(), dump: with(debian13(), upgrade, "")}, state: engine.NotConfigured, detail: upOff},
		{name: "Enable 0", site: site{plat: debian13(), dump: with(debian13(), enable, `"0"`)}, state: engine.NotConfigured, detail: upOff},
		{name: "Enable 00", site: site{plat: pve9(), dump: with(pve9(), enable, `"00"`)}, state: engine.NotConfigured, detail: upOff},
		{name: "apt-daily-upgrade.timer off", site: site{plat: debian13(), units: units(upActive, "inactive", upEnabled, "disabled")}, state: engine.NotConfigured, detail: timerOff},
		{name: "apt-daily-upgrade.timer stopped, enabled", site: site{plat: ubuntu2604(), units: units(upActive, "inactive")}, state: engine.NotConfigured, detail: timerOff},
		{name: "apt-daily-upgrade.timer failed", site: site{plat: debian13(), units: units(upActive, "failed")}, state: engine.NotConfigured, detail: timerOff},
		{name: "apt-daily-upgrade.timer masked", site: site{plat: pve9(), units: units(upActive, "inactive", upEnabled, "masked")}, state: engine.NotConfigured, detail: timerOff},
		{name: "apt-daily-upgrade.service masked, its timer running", site: site{plat: ubuntu2604(), units: units(upJob, "masked")}, state: engine.NotConfigured, detail: jobMasked},
		{name: "both masked, the timer first", site: site{plat: ubuntu2604(), units: units(upActive, "inactive", upEnabled, "masked", upJob, "masked")}, state: engine.NotConfigured, detail: timerOff},
		{name: "apt-daily-upgrade.service not found", site: site{plat: debian13(), units: units(upJob, "not-found")}, state: engine.NotConfigured, detail: jobMasked},
		{name: "the package before the switches", site: site{plat: debian13(), pkg: gone, dump: with(debian13(), upgrade, `"0"`)}, state: engine.NotConfigured, detail: noPkg},
		{name: "not installed, apt-config failing", site: site{plat: debian13(), pkg: gone, runs: fails(hostfake.Response{ExitCode: 100}, dumpCmd)}, state: engine.NotConfigured, detail: noPkg},
		{name: "not installed, a timer state not ranked", site: site{plat: debian13(), pkg: gone, units: units(upEnabled, "enabled-runtime")}, state: engine.NotConfigured, detail: noPkg},
		{name: "Enable 0, dpkg-query failing", site: site{plat: debian13(), dump: with(debian13(), enable, `"0"`), runs: fails(hostfake.Response{ExitCode: 2}, dpkgCmd)},
			state: engine.NotConfigured, detail: upOff},
		{name: "not installed, no systemd", site: site{plat: debian13(), pkg: gone, facts: openRC}, state: engine.NotConfigured, detail: noPkg},

		// Unknown, can't read: an error, or a finding.
		{name: "apt-config not installed", site: site{plat: debian13(), runs: fails(notFound, dumpCmd)}, state: engine.Unknown, detail: cantRead, err: "COMMAND_NOT_FOUND"},
		{name: "apt-config failing", site: site{plat: ubuntu2604(), runs: fails(hostfake.Response{Stderr: "E: Syntax error /etc/apt/apt.conf.d/99local:1: Extra junk at end of file\n", ExitCode: 100}, dumpCmd)},
			state: engine.Unknown, detail: cantRead, err: "COMMAND_FAILED"},
		{name: "a dump line in another form", site: site{plat: debian13(), dump: with(debian13()) + "APT::Periodic::Verbose 1;\n"}, state: engine.Unknown, detail: cantRead, err: "DETECT_UNRECOGNISED"},
		{name: "an item printed twice", site: site{plat: debian13(), dump: with(debian13()) + "APT::Periodic::Unattended-Upgrade \"0\";\n"}, state: engine.Unknown, detail: cantRead, err: "DETECT_UNRECOGNISED"},
		{name: "dpkg-query: half-installed", site: site{plat: debian13(), pkg: "install reinstreq half-installed"}, state: engine.Unknown, detail: cantRead, err: "DETECT_UNRECOGNISED"},
		{name: "dpkg-query failing", site: site{plat: pve9(), runs: fails(hostfake.Response{Stderr: "dpkg-query: error: ...\n", ExitCode: 2}, dpkgCmd)},
			state: engine.Unknown, detail: cantRead, err: "COMMAND_FAILED"},
		{name: "Enable yes", site: site{plat: debian13(), dump: with(debian13(), enable, `"yes"`)}, state: engine.Unknown, detail: cantRead, findings: []string{valueUnknown}},
		{name: "Unattended-Upgrade 1w", site: site{plat: ubuntu2604(), dump: with(ubuntu2604(), upgrade, `"1w"`)}, state: engine.Unknown, detail: cantRead, findings: []string{valueUnknown}},
		{name: "Unattended-Upgrade empty", site: site{plat: debian13(), dump: with(debian13(), upgrade, `""`)}, state: engine.Unknown, detail: cantRead, findings: []string{valueUnknown}},
		{name: "Update-Package-Lists weekly", site: site{plat: debian13(), dump: with(debian13(), lists, `"weekly"`)}, state: engine.Unknown, detail: cantRead, findings: []string{valueUnknown}},
		{name: "two values not ranked", site: site{plat: debian13(), dump: with(debian13(), enable, `"true"`, upgrade, `"daily"`)}, state: engine.Unknown, detail: cantRead,
			findings: []string{valueUnknown, valueUnknown}},
		{name: "apt-daily-upgrade.timer enabled for this boot only", site: site{plat: debian13(), units: units(upEnabled, "enabled-runtime")}, state: engine.Unknown, detail: cantRead,
			findings: []string{scheduleUnknown}},
		{name: "apt-daily-upgrade.timer activating", site: site{plat: ubuntu2604(), units: units(upActive, "activating")}, state: engine.Unknown, detail: cantRead,
			findings: []string{scheduleUnknown}},
		{name: "apt-daily.timer static", site: site{plat: debian13(), units: units(listEnabled, "static")}, state: engine.Unknown, detail: cantRead, findings: []string{scheduleUnknown}},
		{name: "apt-daily-upgrade.service bad", site: site{plat: debian13(), units: units(upJob, "bad")}, state: engine.Unknown, detail: cantRead, findings: []string{scheduleUnknown}},
		{name: "systemctl prints a state hardn does not know", site: site{plat: debian13(), units: units(upEnabled, "frobnicated")}, state: engine.Unknown, detail: cantRead, err: "DETECT_UNRECOGNISED"},
		{name: "systemctl not installed", site: site{plat: debian13(), runs: fails(notFound, upActive, upEnabled, upJob, listActive, listEnabled, listJob)},
			state: engine.Unknown, detail: cantRead, err: "COMMAND_NOT_FOUND"},
		{name: "no systemd", site: site{plat: debian13(), facts: openRC}, state: engine.Unknown, detail: cantRead, findings: []string{scheduleUnknown}},
		{name: "the init system not established", site: site{plat: debian13(), facts: func(f *detect.Facts) {
			f.Init = detect.Init{Err: fmt.Errorf("detect: init: %w", detect.ErrConflict)}
		}}, state: engine.Unknown, detail: cantRead, err: "DETECT_SIGNALS_DISAGREE"},
		{name: "the distribution not established", site: site{plat: debian13(), facts: func(f *detect.Facts) {
			f.OS, f.Platform = detect.OSRelease{Err: detect.ErrNoSignal}, detect.Platform{Err: fmt.Errorf("detect: platform: %w", detect.ErrNoSignal)}
		}}, state: engine.Unknown, detail: cantRead, err: "DETECT_NO_SIGNAL"},
		{name: "a distribution hardn does not read", site: site{plat: debian13(), facts: func(f *detect.Facts) {
			f.OS = detect.OSRelease{ID: "fedora", IDLike: nil}
			f.Platform = detect.Platform{Distro: detect.DistroOther, Support: detect.Unlisted}
		}}, state: engine.Unknown, detail: cantRead, err: "DETECT_UNRECOGNISED"},

		// Alpine.
		{name: "alpine324", site: site{plat: alpine324()}, state: engine.Configured, detail: apkCron},
		{name: "alpine324, origin/main's script beside", site: site{plat: alpine324(), legacy: "hardn"}, state: engine.Configured, detail: apkCron, findings: []string{"ALPINE_LEGACY_SCRIPT"}},
		{name: "alpine324, crond out of the default runlevel", site: site{plat: alpine324(), inLevel: no()}, state: engine.Partial, detail: offRestart},
		{name: "alpine324 stock", site: site{plat: alpine324(), apk: gone, script: absent}, state: engine.NotConfigured, detail: noApkCron},
		{name: "alpine324 stock, origin/main's script", site: site{plat: alpine324(), apk: gone, script: absent, legacy: "hardn"}, state: engine.NotConfigured, detail: noApkCron,
			findings: []string{"ALPINE_LEGACY_SCRIPT"}},
		{name: "alpine324, another apk-upgrade", site: site{plat: alpine324(), apk: gone, script: absent, legacy: "other"}, state: engine.NotConfigured, detail: noApkCron,
			findings: []string{"ALPINE_FOREIGN_SCRIPT"}},
		{name: "alpine324, the daily script removed", site: site{plat: alpine324(), script: absent}, state: engine.NotConfigured, detail: notRun},
		{name: "alpine324, the daily script not executable", site: site{plat: alpine324(), script: plain}, state: engine.NotConfigured, detail: notRun},
		{name: "alpine324, crond stopped", site: site{plat: alpine324(), crond: "stopped"}, state: engine.NotConfigured, detail: crondOff},
		{name: "alpine324, crond crashed", site: site{plat: alpine324(), crond: "crashed"}, state: engine.NotConfigured, detail: crondOff},
		{name: "alpine324, crond stopped, out of the runlevel", site: site{plat: alpine324(), crond: "stopped", inLevel: no()}, state: engine.NotConfigured, detail: crondOff},
		{name: "alpine324, no crond", site: site{plat: alpine324(), crond: missing}, state: engine.NotConfigured, detail: crondOff},
		{name: "alpine324, not installed, crond's state not established", site: site{plat: alpine324(), apk: gone, script: absent, crond: "exit 8"}, state: engine.NotConfigured, detail: noApkCron},
		{name: "alpine324, crond starting", site: site{plat: alpine324(), crond: "exit 8"}, state: engine.Unknown, detail: cantRead, err: "DETECT_UNRECOGNISED"},
		{name: "alpine324, apk failing", site: site{plat: alpine324(), runs: fails(hostfake.Response{ExitCode: 99}, apkInfo)}, state: engine.Unknown, detail: cantRead, err: "COMMAND_FAILED"},
		{name: "alpine324, the runlevel unreadable", site: site{plat: alpine324(), deny: []string{crondLink}}, state: engine.Unknown, detail: cantRead, err: "PERMISSION_DENIED"},
		{name: "alpine324 container", site: site{plat: alpine324(), facts: func(f *detect.Facts) {
			f.Init = detect.Init{Kind: detect.OpenRC, Running: detect.No, Source: detect.OpenRCRun}
			inContainer(f)
		}}, state: engine.Unknown, detail: cantRead, err: "ALPINE_NO_OPENRC"},
		{name: "alpine324, the init system not established", site: site{plat: alpine324(), facts: func(f *detect.Facts) { f.Init = detect.Init{Err: detect.ErrNoSignal} }},
			state: engine.Unknown, detail: cantRead, err: "ALPINE_UNKNOWN"},
	} {
		t.Run(r.name, func(t *testing.T) { run(t, r) })
	}
}

var inContainer = func(f *detect.Facts) {
	f.Container = detect.Container{Detected: detect.Yes, Signal: detect.SystemdContainerFile, Manager: "lxc"}
}

var openRC = func(f *detect.Facts) {
	f.Init = detect.Init{Kind: detect.OpenRC, Running: detect.Yes, Source: detect.OpenRCRunDir}
}

var notPVE = func(f *detect.Facts) {
	f.Platform = detect.Platform{Err: fmt.Errorf("detect: platform: Debian or Proxmox VE: %w", detect.ErrNoSignal)}
}

// TestStock: each platform through Engine.Check over Controls(), as the
// main menu will run it.
func TestStock(t *testing.T) {
	for _, c := range []struct {
		name   string
		site   site
		state  engine.State
		detail string
	}{
		{"debian13", stockOn(debian13()), engine.NotConfigured, noPkg},
		{"debian13 with unattended-upgrades", site{plat: debian13()}, engine.Configured, unattended},
		{"ubuntu2604", site{plat: ubuntu2604()}, engine.Configured, unattended},
		{"pve9", stockOn(pve9()), engine.NotConfigured, noPkg},
		{"pve9 with unattended-upgrades", site{plat: pve9()}, engine.Configured, unattended},
		{"alpine324", site{plat: alpine324(), apk: gone, script: absent}, engine.NotConfigured, noApkCron},
		{"alpine324 with apk-cron", site{plat: alpine324()}, engine.Configured, apkCron},
	} {
		t.Run(c.name, func(t *testing.T) {
			sys, f, facts := c.site.build(t)
			e := engine.New(sys, engine.Options{Detect: func(context.Context, host.System) (detect.Facts, error) { return facts, nil }})
			reps, err := e.Check(context.Background(), updates.Controls())
			if err != nil {
				t.Fatal(err)
			}
			if len(reps) != 1 {
				t.Fatalf("%d reports", len(reps))
			}
			rep := reps[0]
			if rep.Control != "updates.auto" || rep.Title != "Auto Updates" || rep.Err != nil {
				t.Errorf("report: %s %q, error %v", rep.Control, rep.Title, rep.Err)
			}
			if rep.Status.State != c.state || rep.Status.Detail != c.detail {
				t.Errorf("%s %q, want %s %q", rep.Status.State, rep.Status.Detail, c.state, c.detail)
			}
			if len(rep.Status.Evidence) == 0 {
				t.Error("no evidence")
			}
			readOnly(t, f)
		})
	}
}

// TestEvidence: what each line was read from, as the commands printed it.
func TestEvidence(t *testing.T) {
	timersOn := []string{upActive + ": active", upEnabled + ": enabled", upJob + ": static", listActive + ": active", listEnabled + ": enabled", listJob + ": static"}
	for _, c := range []struct {
		r    row
		want []string
	}{
		{row{site: site{plat: debian13()}, state: engine.Configured, detail: unattended}, append([]string{
			"apt-config dump: APT::Periodic::Enable unset",
			`apt-config dump: APT::Periodic::Unattended-Upgrade "1"`,
			`apt-config dump: APT::Periodic::Update-Package-Lists "1"`,
			"dpkg-query --show unattended-upgrades: installed"}, timersOn...)},
		{row{site: stockOn(debian13()), state: engine.NotConfigured, detail: noPkg}, append([]string{
			"apt-config dump: APT::Periodic::Enable unset",
			"apt-config dump: APT::Periodic::Unattended-Upgrade unset",
			"apt-config dump: APT::Periodic::Update-Package-Lists unset",
			"dpkg-query --show unattended-upgrades: not installed"}, timersOn...)},
		{row{site: site{plat: pve9(), units: units(upEnabled, "disabled")}, state: engine.Partial, detail: offRestart, findings: []string{proxmox}}, []string{
			"apt-config dump: APT::Periodic::Enable unset",
			`apt-config dump: APT::Periodic::Unattended-Upgrade "1"`,
			`apt-config dump: APT::Periodic::Update-Package-Lists "1"`,
			"dpkg-query --show unattended-upgrades: installed",
			upActive + ": active", upEnabled + ": disabled", upJob + ": static", listActive + ": active", listEnabled + ": enabled", listJob + ": static",
			"apt-config dump: 3 origins, none naming Proxmox"}},
		{row{site: site{plat: debian13(), facts: notPVE}, state: engine.Configured, detail: unattended}, append(append([]string{
			"apt-config dump: APT::Periodic::Enable unset",
			`apt-config dump: APT::Periodic::Unattended-Upgrade "1"`,
			`apt-config dump: APT::Periodic::Update-Package-Lists "1"`,
			"dpkg-query --show unattended-upgrades: installed"}, timersOn...), "Proxmox VE: not established, origins not compared")},
		{row{site: site{plat: debian13(), pkg: gone, facts: openRC}, state: engine.NotConfigured, detail: noPkg}, []string{
			"apt-config dump: APT::Periodic::Enable unset",
			`apt-config dump: APT::Periodic::Unattended-Upgrade "1"`,
			`apt-config dump: APT::Periodic::Update-Package-Lists "1"`,
			"dpkg-query --show unattended-upgrades: not installed",
			"init: openrc, running yes: no systemd timers"}},
		{row{site: site{plat: alpine324(), inLevel: no()}, state: engine.Partial, detail: offRestart}, []string{
			"apk info -e apk-cron: installed yes",
			"/etc/periodic/daily/apk: executable file",
			"rc-service crond status: started",
			"/etc/runlevels/default/crond: in runlevel no",
			"/etc/periodic/daily/apk-upgrade: absent"}},
	} {
		t.Run(c.r.detail, func(t *testing.T) {
			if st := run(t, c.r); !slices.Equal(st.Evidence, c.want) {
				t.Errorf("evidence\n%q\nwant\n%q", st.Evidence, c.want)
			}
		})
	}
}

// TestFindings: what each finding says, and where it was read.
func TestFindings(t *testing.T) {
	for _, c := range []struct {
		r    row
		want []engine.Finding
	}{
		{row{site: site{plat: debian13(), dump: with(debian13(), upgrade, `"1w"`)}, state: engine.Unknown, detail: cantRead, findings: []string{valueUnknown}},
			[]engine.Finding{{Code: updates.CodeAptValueUnknown, Detail: `Unattended-Upgrade "1w"`, Source: "apt-config dump"}}},
		{row{site: site{plat: debian13(), units: units(upEnabled, "enabled-runtime")}, state: engine.Unknown, detail: cantRead, findings: []string{scheduleUnknown}},
			[]engine.Finding{{Code: updates.CodeScheduleUnknown, Detail: "apt-daily-upgrade.timer enabled-runtime", Source: upEnabled}}},
		{row{site: site{plat: debian13(), facts: openRC}, state: engine.Unknown, detail: cantRead, findings: []string{scheduleUnknown}},
			[]engine.Finding{{Code: updates.CodeScheduleUnknown, Detail: "no systemd timers", Source: detect.OpenRCRunDir}}},
		{row{site: site{plat: pve9()}, state: engine.Configured, detail: unattended, findings: []string{proxmox}},
			[]engine.Finding{{Code: updates.CodeProxmoxNotUpgraded, Detail: "Proxmox packages not upgraded", Source: "apt-config dump"}}},
		{row{site: site{plat: alpine324(), legacy: "hardn"}, state: engine.Configured, detail: apkCron, findings: []string{"ALPINE_LEGACY_SCRIPT"}},
			[]engine.Finding{{Code: "ALPINE_LEGACY_SCRIPT", Detail: "old apk-upgrade script", Source: legacyScript}}},
		{row{site: site{plat: alpine324(), legacy: "other"}, state: engine.Configured, detail: apkCron, findings: []string{"ALPINE_FOREIGN_SCRIPT"}},
			[]engine.Finding{{Code: "ALPINE_FOREIGN_SCRIPT", Detail: "other apk-upgrade file", Source: legacyScript}}},
	} {
		t.Run(c.want[0].Detail, func(t *testing.T) {
			if st := run(t, c.r); !slices.Equal(st.Findings, c.want) {
				t.Errorf("findings %v, want %v", st.Findings, c.want)
			}
		})
	}
}

// TestNoKinderThanAlpine: on Alpine the line is never more favourable than
// next/p2-alpine's own Status: Configured only where it is, Partial only
// where it is, and its NotConfigured stays NotConfigured.
func TestNoKinderThanAlpine(t *testing.T) {
	for _, apk := range []string{"", gone} {
		for _, script := range []string{"", absent, plain} {
			for _, crond := range []string{"", "stopped", "crashed", missing, "exit 8"} {
				for _, in := range []*bool{nil, no()} {
					s := site{plat: alpine324(), apk: apk, script: script, crond: crond, inLevel: in}
					sys, f, facts := s.build(t)
					st, _ := updates.Auto{}.Check(context.Background(), sys, facts)
					u, err := alpine.ReadUpdates(context.Background(), sys, facts)
					if err != nil {
						t.Fatal(err)
					}
					want := u.Status().State
					ok := map[engine.State]bool{
						engine.Configured:    want == alpine.StateConfigured,
						engine.Partial:       want == alpine.StatePartial,
						engine.NotConfigured: want != alpine.StateConfigured,
						engine.Unknown:       want != alpine.StateConfigured && want != alpine.StateNotConfigured,
					}[st.State]
					if !ok {
						t.Errorf("apk-cron %q, script %q, crond %q, in runlevel %v: %s %q, alpine.Status %s", apk, script, crond, in == nil, st.State, st.Detail, want)
					}
					readOnly(t, f)
				}
			}
		}
	}
}

// TestDetails: the exported details and ID are the page's wording, and
// each fits the room beside its state at 64 columns (checks.md §1.9).
func TestDetails(t *testing.T) {
	room := map[engine.State]int{engine.Configured: 26, engine.NotConfigured: 22, engine.Partial: 29, engine.Unknown: 29}
	for _, c := range []struct {
		got, want string
		state     engine.State
	}{
		{updates.DetailUnattended, unattended, engine.Configured},
		{updates.DetailApkCron, apkCron, engine.Configured},
		{updates.DetailListsStale, stale, engine.Partial},
		{updates.DetailOffAfterRestart, offRestart, engine.Partial},
		{updates.DetailNoUnattended, noPkg, engine.NotConfigured},
		{updates.DetailUpgradesOff, upOff, engine.NotConfigured},
		{updates.DetailTimerOff, timerOff, engine.NotConfigured},
		{updates.DetailJobMasked, jobMasked, engine.NotConfigured},
		{updates.DetailNoApkCron, noApkCron, engine.NotConfigured},
		{updates.DetailScriptNotRun, notRun, engine.NotConfigured},
		{updates.DetailCrondOff, crondOff, engine.NotConfigured},
		{updates.DetailCantRead, cantRead, engine.Unknown},
	} {
		if c.got != c.want {
			t.Errorf("%q, want %q", c.got, c.want)
		}
		if len(c.got) > room[c.state] {
			t.Errorf("%q: %d cells beside %s, room for %d", c.got, len(c.got), c.state, room[c.state])
		}
	}
	if (updates.Auto{}).ID() != updates.ID || updates.ID != "updates.auto" {
		t.Errorf("Auto.ID() %q, ID %q", updates.Auto{}.ID(), updates.ID)
	}
}

// TestCheckOnly: Plan is ErrNotMigrated, and a plan with the control is
// refused with it (CONTROL_NOT_MIGRATED), changing nothing.
func TestCheckOnly(t *testing.T) {
	sys, f, facts := site{plat: debian13()}.build(t)
	cs, err := updates.Auto{}.Plan(context.Background(), sys, engine.Input{Facts: facts})
	if !errors.Is(err, engine.ErrNotMigrated) || cs != nil {
		t.Errorf("Plan %v, %v", cs, err)
	}
	f.AddDir("/run", 0o755, 0, 0)
	e := engine.New(sys, engine.Options{Detect: func(context.Context, host.System) (detect.Facts, error) { return facts, nil }})
	p, err := e.Plan(context.Background(), updates.Controls(), nil)
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
