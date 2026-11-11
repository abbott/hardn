package updates_test

import (
	"context"
	"crypto/md5" //nolint:gosec // dpkg's conffile hash
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/abbott/hardn/pkg/config"
	"github.com/abbott/hardn/pkg/engine"
	updatesctl "github.com/abbott/hardn/pkg/engine/controls/updates"
	"github.com/abbott/hardn/pkg/engine/header"
	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/alpine"
	hostbackup "github.com/abbott/hardn/pkg/host/backup"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/dnfauto"
	"github.com/abbott/hardn/pkg/host/hostfake"
	"github.com/abbott/hardn/pkg/host/revert"
)

// The screen's model of a host, copied from the control's
// (pkg/engine/controls/updates/model_test.go) and the gallery's hosts
// (internal/tools/uigallery/updates.go, updateshost.go), in the VMs'
// shapes of 2026-11-03, values synthetic: what apt-config, dpkg-query,
// systemctl, apt-get, apk, rc-service, rc-update, and on Rocky Linux rpm
// and dnf (needs-restarting among its commands) answer of a hostfake's
// files and the state below. apt's files are in apt-config dump's one-line form (`Name
// "value";`, an item `Name:: "value";`); a line in any other form is apt's
// syntax error; a list is one line, `Name { "a"; "b"; };`. A command the
// model does not answer fails the test.

const (
	confDir    = "/etc/apt/apt.conf.d"
	autoUp     = confDir + "/20auto-upgrades"
	uuConf     = confDir + "/50unattended-upgrades"
	hardnFile  = confDir + "/52hardn-auto-upgrades"
	recordPath = "/var/lib/hardn/controls/updates.auto.json"
	uuName     = "unattended-upgrades"
	apkCronVer = "1.0-r3"

	autoOn     = "APT::Periodic::Update-Package-Lists \"1\";\nAPT::Periodic::Unattended-Upgrade \"1\";\n"
	autoOff    = "APT::Periodic::Update-Package-Lists \"0\";\nAPT::Periodic::Unattended-Upgrade \"0\";\n"
	listsStale = "APT::Periodic::Update-Package-Lists \"0\";\nAPT::Periodic::Unattended-Upgrade \"1\";\n"
	origins    = "Unattended-Upgrade::Origins-Pattern { \"origin=Debian,codename=${distro_codename},label=Debian\"; };\n"
	ubuOrigins = "Unattended-Upgrade::Allowed-Origins { \"${distro_id}:${distro_codename}\"; \"${distro_id}:${distro_codename}-security\"; };\n"
	ubuntu10   = "APT::Periodic::Update-Package-Lists \"1\";\nAPT::Periodic::Download-Upgradeable-Packages \"0\";\nAPT::Periodic::AutocleanInterval \"0\";\n"
	apkDaily   = "#!/bin/sh\n\napk upgrade --update --quiet\n"
	hardnHead  = "// Written by hardn (Auto Updates).\n"
	bothLines  = hardnHead + "APT::Periodic::Update-Package-Lists \"1\";\nAPT::Periodic::Unattended-Upgrade \"1\";\n"
	logrotate  = "/etc/logrotate.d/unattended-upgrades"
	logrotConf = "/var/log/unattended-upgrades/*.log {\n  rotate 6\n  monthly\n  compress\n  missingok\n  notifempty\n}\n"
)

// dirDump is apt-config dump Dir's lines aptconf checks.
const dirDump = "Dir \"/\";\nDir::Etc \"etc/apt\";\nDir::Etc::main \"apt.conf\";\nDir::Etc::parts \"apt.conf.d\";\n" +
	"Dir::Ignore-Files-Silently \"\";\nDir::Ignore-Files-Silently:: \"~$\";\nDir::Ignore-Files-Silently:: \"\\.dpkg-[a-z]+$\";\nDir::Ignore-Files-Silently:: \"\\.ucf-[a-z]+$\";\n"

// uptime is /proc/uptime: up 3 days.
const uptime = "259200.00 518400.00\n"

// clock is the engine's, the record's and the screen's.
var clock = func() time.Time { return time.Date(2026, 11, 3, 9, 30, 0, 0, time.UTC) }

// version is the hardn version the records carry.
const version = "v0.4.0"

// The platforms, as detect establishes them.
func debian13() detect.Facts {
	return detect.Facts{
		OS:        detect.OSRelease{ID: "debian", VersionID: "13", VersionCodename: "trixie"},
		Platform:  detect.Platform{Distro: detect.DistroDebian, Version: "13", Support: detect.Supported},
		Init:      detect.Init{Kind: detect.Systemd, Running: detect.Yes, Source: detect.SystemdRunDir},
		Proxmox:   detect.Proxmox{Installed: detect.No, ClusterFS: detect.No, EtcPVE: detect.No},
		Container: detect.Container{Detected: detect.No},
	}
}

func ubuntu2604() detect.Facts {
	f := debian13()
	f.OS = detect.OSRelease{ID: "ubuntu", VersionID: "26.04", VersionCodename: "resolute"}
	f.Platform = detect.Platform{Distro: detect.DistroUbuntu, Version: "26.04", Support: detect.Supported}
	return f
}

func pve9() detect.Facts {
	f := debian13()
	f.Platform = detect.Platform{Distro: detect.DistroProxmox, Version: "9", Support: detect.Supported}
	f.Proxmox = detect.Proxmox{Installed: detect.Yes, Version: "9.0.10", ClusterFS: detect.Yes, EtcPVE: detect.Yes}
	return f
}

// rocky102 is rocky10-test-110's facts: Rocky Linux 10.2.
func rocky102() detect.Facts {
	f := debian13()
	f.OS = detect.OSRelease{ID: "rocky", IDLike: []string{"rhel", "centos", "fedora"}, VersionID: "10.2"}
	f.Platform = detect.Platform{Distro: detect.DistroRocky, Version: "10", Support: detect.Supported}
	return f
}

func alpine323() detect.Facts {
	f := debian13()
	f.OS = detect.OSRelease{ID: "alpine", VersionID: "3.23.0"}
	f.Platform = detect.Platform{Distro: detect.DistroAlpine, Version: "3.23", Support: detect.Supported}
	f.Init = detect.Init{Kind: detect.OpenRC, Running: detect.Yes, Source: detect.OpenRCRunDir}
	return f
}

// model is one host.
type model struct {
	t     *testing.T
	f     *hostfake.System
	facts detect.Facts
	// apt
	installed  bool
	debconfOff bool     // the postinst writes 20auto-upgrades' "0" lines
	deps       []string // what apt-get install -s lists besides the package
	extra      []string // the install simulation's other lines
	offExtra   []string // off's simulations' other lines (apt-get purge/remove -s, apk del --simulate)
	units      map[string]string
	// Rocky Linux: dnf-automatic installed; dnf has no enabled repository
	// (an unregistered RHEL host)
	dnfAuto, noRepos bool
	// Alpine
	apkCron, crond bool
}

func newModel(t *testing.T, f detect.Facts) *model {
	t.Helper()
	m := &model{t: t, f: hostfake.New(host.Options{}), facts: f, units: map[string]string{}}
	for _, d := range []string{"/etc", "/etc/apt", confDir, "/etc/hardn", "/etc/logrotate.d", "/var", "/var/lib", "/var/backups", "/var/log", "/run"} {
		m.f.AddDir(d, 0o755, 0, 0)
	}
	if f.Platform.Distro == detect.DistroAlpine {
		for _, d := range alpine.PeriodicDirs {
			m.f.AddDir(alpine.PeriodicDir+"/"+d, 0o755, 0, 0)
		}
		m.f.AddDir(alpine.CrontabsDir, 0o755, 0, 0)
		m.f.AddFile(alpine.RootCrontab, []byte(alpine.StockCrontab), 0o600, 0, 0)
		m.f.AddDir("/etc/init.d", 0o755, 0, 0)
		m.f.AddFile(alpine.InitScript, []byte("#!/sbin/openrc-run\n"), 0o755, 0, 0)
		m.f.AddDir("/etc/runlevels/default", 0o755, 0, 0)
	}
	m.f.AddDir("/proc", 0o555, 0, 0)
	m.f.AddFile(header.UptimeFile, []byte(uptime), 0o444, 0, 0) // 3 days: under risk.LongUptime
	m.f.Handler = m.answer
	return m
}

func (m *model) file(name, data string) { m.f.AddFile(name, []byte(data), 0o644, 0, 0) }

// aptHost is a Debian-family host: unattended-upgrades installed with
// 20auto-upgrades as auto, or not installed where auto is "".
func aptHost(t *testing.T, f detect.Facts, auto string) *model {
	m := newModel(t, f)
	ubuntu := f.Platform.Distro == detect.DistroUbuntu
	if ubuntu {
		m.file(confDir+"/10periodic", ubuntu10)
	}
	if auto != "" {
		m.installed = true
		m.file(uuConf, map[bool]string{false: origins, true: ubuOrigins}[ubuntu])
		m.file(autoUp, auto)
		m.file(logrotate, logrotConf)
	}
	return m
}

// engine is the engine over m, as cmd/hardn builds it, progress its
// Options.Progress.
func (m *model) engine(progress func(engine.Event)) *engine.Engine {
	return engine.New(m.f, engine.Options{
		Revert:   revert.Options{Now: clock, Hardn: "/usr/local/bin/hardn"},
		Detect:   func(context.Context, host.System) (detect.Facts, error) { return m.facts, nil },
		Hold:     func() func() { return func() {} },
		PID:      5000,
		Backups:  hostbackup.Settings{Path: hostbackup.DefaultPath, Enabled: true},
		Progress: progress,
	})
}

// control is the control in a direction, as the screen builds it.
func control(a updatesctl.AutoAction) updatesctl.Auto {
	return updatesctl.Auto{Action: a, Records: revert.Options{Now: clock}, Version: version}
}

// applied is m after hardn's Enable, planned and applied on the engine.
func (m *model) applied() *model {
	m.t.Helper()
	ctx := context.Background()
	e := m.engine(nil)
	p, err := e.Plan(ctx, []engine.Control{control(updatesctl.AutoOn)}, &config.Config{})
	if err != nil || len(p.Refused) > 0 {
		m.t.Fatalf("Enable: %v, refused %v", err, p.Refused)
	}
	if _, err := e.Apply(ctx, p); err != nil {
		m.t.Fatalf("Enable: %v", err)
	}
	return m
}

// exitOf is systemctl's exit status for state.
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

// answer is the model's commands.
func (m *model) answer(argv []string, _ []byte) hostfake.Response {
	ctx := context.Background()
	cmd := strings.Join(argv, " ")
	switch {
	case argv[0] == "apt-config" && len(argv) > 2 && argv[1] == "dump":
		out, err := m.dump(ctx, argv[2:])
		if err != nil {
			return hostfake.Response{Stderr: "E: " + err.Error() + "\n", ExitCode: 100}
		}
		return hostfake.Response{Stdout: out}
	case cmd == "dpkg-query --show --showformat=${Status}\\t${Version}\\n "+uuName:
		if m.installed {
			return hostfake.Response{Stdout: "install ok installed\t2.12\n"}
		}
		return hostfake.Response{Stderr: "dpkg-query: no packages found matching unattended-upgrades\n", ExitCode: 1}
	case argv[0] == "systemctl" && len(argv) == 3 && (argv[1] == "is-active" || argv[1] == "is-enabled") && m.unitGone(argv[1], argv[2]):
		return hostfake.Response{Stdout: map[string]string{"is-active": "inactive", "is-enabled": "not-found"}[argv[1]] + "\n", ExitCode: 4}
	case argv[0] == "systemctl" && len(argv) == 3 && (argv[1] == "is-active" || argv[1] == "is-enabled"):
		st, ok := m.units[argv[1]+" "+argv[2]]
		switch {
		case ok:
		case argv[1] == "is-active":
			st = "active"
		case strings.HasSuffix(argv[2], ".service"):
			st = "static"
		default:
			st = "enabled"
		}
		return hostfake.Response{Stdout: st + "\n", ExitCode: exitOf(st)}
	case cmd == "dnf --setopt=exit_on_lock=True needs-restarting -r": // no core package updated since boot (header's reboot on Rocky Linux)
		return hostfake.Response{Stdout: "No core libraries or services have been updated since boot-up.\nReboot should not be necessary.\n"}
	case argv[0] == "rpm" || argv[0] == "dnf" || argv[0] == "systemctl" && len(argv) == 3:
		return m.dnf(ctx, argv)
	case cmd == "dpkg-query -W -f=${Conffiles}\\n "+uuName:
		if !m.installed {
			return hostfake.Response{Stderr: "dpkg-query: no packages found matching unattended-upgrades\n", ExitCode: 1}
		}
		sum := md5.Sum([]byte(logrotConf)) //nolint:gosec // dpkg's conffile hash
		return hostfake.Response{Stdout: " " + logrotate + " " + hex.EncodeToString(sum[:]) + "\n\n"}
	case cmd == "apt-get install -s -y "+uuName:
		var out strings.Builder
		out.WriteString("NOTE: This is only a simulation!\n")
		for _, p := range append(slices.Clone(m.deps), uuName) {
			fmt.Fprintf(&out, "Inst %s (2.12 Debian:13.1/stable [all])\n", p)
		}
		out.WriteString(lines(m.extra))
		return hostfake.Response{Stdout: out.String()}
	case cmd == "apt-get install -y "+uuName:
		m.install(ctx)
		return hostfake.Response{}
	case cmd == "apt-get purge -s -y "+uuName:
		return hostfake.Response{Stdout: "NOTE: This is only a simulation!\nPurg " + uuName + " [2.12]\n" + lines(m.offExtra)}
	case cmd == "apt-get purge -y "+uuName: // the postrm: rm -f both files, edited or not
		m.installed = false
		_ = m.f.Remove(ctx, autoUp)
		_ = m.f.Remove(ctx, uuConf)
		_ = m.f.Remove(ctx, logrotate)
		return hostfake.Response{}
	case cmd == "apt-get remove -s -y "+uuName:
		return hostfake.Response{Stdout: "NOTE: This is only a simulation!\nRemv " + uuName + " [2.12]\n" + lines(m.offExtra)}
	case cmd == "apt-get remove -y "+uuName: // ucf's file and the conffile stay
		m.installed = false
		return hostfake.Response{}
	case cmd == "apk info -e "+alpine.Package:
		if m.apkCron {
			return hostfake.Response{Stdout: alpine.Package + "\n"}
		}
		return hostfake.Response{ExitCode: 1}
	case cmd == "apk add --simulate "+alpine.Package:
		return hostfake.Response{Stdout: "(1/1) Installing " + alpine.Package + " (" + apkCronVer + ")\nOK: 9 MiB in 42 packages\n"}
	case cmd == "apk add "+alpine.Package:
		m.apkCron = true
		m.f.AddFile(alpine.Script, []byte(apkDaily), 0o755, 0, 0)
		return hostfake.Response{Stdout: "(1/1) Installing " + alpine.Package + " (" + apkCronVer + ")\nOK: 9 MiB in 42 packages\n"}
	case cmd == "apk del --simulate "+alpine.Package:
		return hostfake.Response{Stdout: "(1/1) Purging " + alpine.Package + " (" + apkCronVer + ")\n" + lines(m.offExtra) + "OK: 9 MiB in 41 packages\n"}
	case cmd == "apk del "+alpine.Package: // an edited file under /etc stays
		m.apkCron = false
		if data, err := m.f.ReadFile(ctx, alpine.Script); err == nil && string(data) == apkDaily {
			_ = m.f.Remove(ctx, alpine.Script)
		}
		return hostfake.Response{Stdout: "(1/1) Purging " + alpine.Package + " (" + apkCronVer + ")\nOK: 9 MiB in 41 packages\n"}
	case cmd == "rc-service crond status":
		if m.crond {
			return hostfake.Response{Stdout: " * status: started\n"}
		}
		return hostfake.Response{Stdout: " * status: stopped\n", ExitCode: 3}
	case cmd == "rc-service crond start":
		m.crond = true
		return hostfake.Response{}
	case cmd == "rc-service crond stop":
		m.crond = false
		return hostfake.Response{}
	case cmd == "rc-update add crond default":
		m.f.AddSymlink(alpine.RunlevelLink, alpine.InitScript)
		return hostfake.Response{}
	case cmd == "rc-update del crond default":
		_ = m.f.Remove(ctx, alpine.RunlevelLink)
		return hostfake.Response{}
	}
	m.t.Errorf("unexpected command %q", cmd)
	return hostfake.Response{Err: hostfake.ErrUnscripted}
}

// install is apt-get install's effect: 50unattended-upgrades, and the
// postinst's 20auto-upgrades by the debconf answer, through ucf, which
// keeps a file already there.
func (m *model) install(ctx context.Context) {
	m.installed = true
	m.deps = nil
	m.file(logrotate, logrotConf)
	if _, err := m.f.Lstat(ctx, uuConf); errors.Is(err, fs.ErrNotExist) {
		m.file(uuConf, origins)
	}
	if _, err := m.f.Lstat(ctx, autoUp); errors.Is(err, fs.ErrNotExist) {
		m.file(autoUp, map[bool]string{false: autoOn, true: autoOff}[m.debconfOff])
	}
}

// entry is one item of apt's configuration as a file sets it.
type entry struct {
	name, value string
	item        bool
}

// entries reads apt.conf.d in name order, skipping the names
// Dir::Ignore-Files-Silently matches here, then apt.conf.
func (m *model) entries(ctx context.Context) ([]entry, error) {
	des, err := m.f.ReadDir(ctx, confDir)
	if err != nil {
		return nil, err
	}
	var files []string
	for _, de := range des {
		n := de.Name()
		if !strings.HasSuffix(n, "~") && !strings.Contains(n, ".ucf-") && !strings.Contains(n, ".dpkg-") {
			files = append(files, path.Join(confDir, n))
		}
	}
	slices.Sort(files)
	files = append(files, "/etc/apt/apt.conf")
	var out []entry
	for _, f := range files {
		data, err := m.f.ReadFile(ctx, f)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		for i, l := range strings.Split(string(data), "\n") {
			l = strings.TrimSpace(l)
			if l == "" || strings.HasPrefix(l, "//") {
				continue
			}
			if name, list, ok := strings.Cut(l, " { "); ok && strings.HasSuffix(list, " };") {
				for _, v := range strings.Split(strings.TrimSuffix(list, " };"), "; ") {
					out = append(out, entry{name: name, value: strings.Trim(strings.TrimSuffix(v, ";"), `"`), item: true})
				}
				continue
			}
			name, rest, ok := strings.Cut(l, ` "`)
			value, closed := strings.CutSuffix(rest, `";`)
			if !ok || !closed || name == "" || strings.ContainsAny(name, " \t\"") || strings.Contains(value, `"`) {
				return nil, fmt.Errorf("Syntax error %s:%d: Extra junk at end of file", f, i+1)
			}
			n, item := strings.CutSuffix(name, "::")
			out = append(out, entry{name: n, value: value, item: item})
		}
	}
	return out, nil
}

// node is one item of apt's tree, as apt-pkg's Configuration builds it.
type node struct {
	tag, value string
	children   []*node
}

func (n *node) find(tag string) *node {
	for _, c := range n.children {
		if c.tag != "" && strings.EqualFold(c.tag, tag) {
			return c
		}
	}
	return nil
}

// dump is apt-config dump of roots: each root's subtree depth first, a
// list item named after its parent with "::"; Dir's lines for "Dir".
func (m *model) dump(ctx context.Context, roots []string) (string, error) {
	es, err := m.entries(ctx)
	if err != nil {
		return "", err
	}
	if len(roots) == 1 && roots[0] == "Dir" {
		return dirDump, nil
	}
	tree := &node{}
	for _, e := range es {
		n := tree
		for _, s := range strings.Split(e.name, "::") {
			c := n.find(s)
			if c == nil {
				c = &node{tag: s}
				n.children = append(n.children, c)
			}
			n = c
		}
		if e.item {
			n.children = append(n.children, &node{value: e.value})
		} else {
			n.value = e.value
		}
	}
	var b strings.Builder
	var walk func(n *node, full string)
	walk = func(n *node, full string) {
		fmt.Fprintf(&b, "%s \"%s\";\n", full, n.value)
		for _, c := range n.children {
			walk(c, full+"::"+c.tag)
		}
	}
	for _, r := range roots {
		n, full := tree, ""
		for _, s := range strings.Split(r, "::") {
			if n = n.find(s); n == nil {
				break
			}
			if full != "" {
				full += "::"
			}
			full += n.tag
		}
		if n != nil {
			walk(n, full)
		}
	}
	return b.String(), nil
}

func lines(ls []string) string {
	var b strings.Builder
	for _, l := range ls {
		b.WriteString(l + "\n")
	}
	return b.String()
}

// writes are the calls after the first n that could change the host
// outside /run/hardn, the engine's run directory: every call but a read,
// every command but those that only read.
func writes(m *model, n int) []string {
	var out []string
	for _, c := range m.f.Calls()[n:] {
		switch c.Op {
		case "read", "read-nofollow", "stat", "lstat", "readdir", "readlink", "resolve-symlink", "lookup-user", "lookup-group":
		case "run":
			cmd := strings.Join(c.Argv, " ")
			if strings.HasPrefix(cmd, "apt-config dump ") || strings.HasPrefix(cmd, "dpkg-query ") || strings.HasPrefix(cmd, "systemctl is-") ||
				strings.Contains(cmd, " -s -y ") || strings.Contains(cmd, "--simulate") || cmd == "apk info -e apk-cron" || cmd == "rc-service crond status" ||
				strings.HasPrefix(cmd, "rpm -q ") || strings.Contains(cmd, " --assumeno ") || cmd == "dnf --setopt=exit_on_lock=True needs-restarting -r" {
				continue
			}
			out = append(out, cmd)
		default:
			if !strings.HasPrefix(c.Path, "/run/hardn") {
				out = append(out, c.Op+" "+c.Path)
			}
		}
	}
	return out
}

// The hosts of the gallery's menu (gallery-updates.md §2), in its order,
// then Rocky Linux's (el-updates.md §1.4); then the gallery's variants and
// the refusals' hosts.
var hosts = []string{"debian13", "ubuntu2604", "alpine323", "debian-installer", "debian-off", "lists-stale", "timer-off", "by-hardn-debian",
	"by-hardn-alpine", "edited", "crond-shared", "dormant-jobs", "config-files", "legacy-alpine", "pve9", "not-read",
	"rocky102", "rocky-timers-off", "by-hardn-rocky", "rocky-all-updates", "rocky-masked", "rocky-plain-timer", "rocky-reboot"}

var variants = []string{"edited-alpine", "enable-off", "own-file", "masked", "missing", "no-origin", "extra-packages", "later-file", "no-record",
	"corrupt-record", "purge-extra", "apk-del-extra"}

// newHost is the host named name.
func newHost(t *testing.T, name string) *model {
	t.Helper()
	switch name {
	case "debian13":
		return aptHost(t, debian13(), autoOn)
	case "ubuntu2604":
		return aptHost(t, ubuntu2604(), autoOn)
	case "alpine323":
		return newModel(t, alpine323())
	case "debian-installer":
		m := aptHost(t, debian13(), "")
		m.deps = []string{"python3-dbus", "python3-distro-info"}
		return m
	case "debian-off":
		m := aptHost(t, debian13(), autoOff)
		m.debconfOff = true
		return m
	case "lists-stale":
		return aptHost(t, debian13(), listsStale)
	case "timer-off":
		m := aptHost(t, debian13(), autoOn)
		m.units["is-active apt-daily-upgrade.timer"], m.units["is-enabled apt-daily-upgrade.timer"] = "inactive", "disabled"
		return m
	case "by-hardn-debian":
		return newHost(t, "debian-installer").applied()
	case "by-hardn-alpine":
		return newHost(t, "alpine323").applied()
	case "edited":
		m := newHost(t, "by-hardn-debian")
		m.file(hardnFile, bothLines+"APT::Periodic::Enable \"1\";\n")
		return m
	case "crond-shared":
		m := newHost(t, "dormant-jobs")
		m.crond = true
		m.f.AddSymlink(alpine.RunlevelLink, alpine.InitScript)
		return m.applied()
	case "dormant-jobs":
		m := newModel(t, alpine323())
		m.f.AddFile("/etc/periodic/daily/logrotate", []byte("#!/bin/sh\n/usr/sbin/logrotate /etc/logrotate.conf\n"), 0o755, 0, 0)
		return m
	case "config-files":
		m := aptHost(t, debian13(), "")
		m.file(uuConf, origins+"Unattended-Upgrade::Mail \"root\";\n")
		return m
	case "legacy-alpine":
		m := newModel(t, alpine323())
		m.f.AddFile(alpine.LegacyScript, []byte(alpine.LegacyContent), 0o755, 0, 0)
		return m
	case "pve9":
		return aptHost(t, pve9(), "")
	case "not-read":
		m := aptHost(t, debian13(), autoOn)
		m.file(confDir+"/99site", "APT::Periodic::Verbose 1\n")
		return m
	case "rocky102":
		return dnfHost(t, false)
	case "rocky-timers-off":
		return dnfHost(t, true)
	case "by-hardn-rocky":
		return newHost(t, "rocky102").applied()
	case "rocky-all-updates": // on outside hardn: the install timer on, upgrade_type default
		m := dnfHost(t, true)
		m.units["is-active "+dnfauto.InstallTimer], m.units["is-enabled "+dnfauto.InstallTimer] = "active", "enabled"
		return m
	case "rocky-plain-timer": // on outside hardn: dnf-automatic.timer with apply_updates yes, the install timer off
		m := dnfHost(t, true)
		m.units["is-active "+dnfauto.PlainTimer], m.units["is-enabled "+dnfauto.PlainTimer] = "active", "enabled"
		m.file(dnfauto.Conf, strings.Replace(dnfConf, "apply_updates = no", "apply_updates = yes", 1))
		return m
	case "rocky-masked":
		m := dnfHost(t, true)
		m.units["is-enabled "+dnfauto.InstallTimer] = "masked"
		return m
	case "rocky-reboot": // automatic.conf restarts the host where an update needs it
		m := dnfHost(t, true)
		m.file(dnfauto.Conf, strings.Replace(dnfConf, "reboot = never", "reboot = when-needed", 1))
		return m
	case "edited-alpine":
		m := newHost(t, "by-hardn-alpine")
		m.f.AddFile(alpine.Script, []byte(apkDaily+"echo done\n"), 0o755, 0, 0)
		return m
	case "enable-off":
		m := newHost(t, "debian-off")
		m.file(confDir+"/99site", "APT::Periodic::Enable \"0\";\n")
		return m
	case "own-file":
		m := newHost(t, "debian-off")
		m.file(hardnFile, bothLines)
		return m
	case "masked":
		m := newHost(t, "debian-off")
		m.units["is-enabled apt-daily-upgrade.service"] = "masked"
		return m
	case "missing":
		m := newHost(t, "debian-off")
		m.units["is-enabled apt-daily-upgrade.service"] = "not-found"
		return m
	case "no-origin":
		m := newHost(t, "debian-off")
		m.file(uuConf, "// No origins.\n")
		return m
	case "extra-packages":
		m := newHost(t, "debian-installer")
		m.extra = []string{"Inst libc6 [2.41-11] (2.41-12 Debian:13.1/stable [arm64])"}
		return m
	case "later-file":
		m := newHost(t, "debian-off")
		m.file(confDir+"/60site", "APT::Periodic::Unattended-Upgrade \"0\";\n")
		return m
	case "no-record":
		m := newHost(t, "by-hardn-debian")
		_ = m.f.Remove(context.Background(), recordPath)
		return m
	case "corrupt-record":
		m := newHost(t, "by-hardn-alpine")
		m.f.AddFile(recordPath, []byte("{\"schemaVersion\": 1, \"entries\": [\n"), 0o600, 0, 0)
		return m
	case "purge-extra":
		m := newHost(t, "by-hardn-debian")
		m.offExtra = []string{"Remv python3-apt [3.0.0]"}
		return m
	case "apk-del-extra":
		m := newHost(t, "by-hardn-alpine")
		m.offExtra = []string{"(2/2) Purging busybox-extras (1.37.0-r0)"}
		return m
	}
	t.Fatalf("no host %q", name)
	return nil
}

// dnfConf is dnf-automatic's stock automatic.conf, cut to what the control
// reads, as the control's model has it (pkg/host/dnfauto's testdata holds
// the whole of it).
const dnfConf = "[commands]\n#  What kind of upgrade to perform:\nupgrade_type = default\nrandom_sleep = 0\ndownload_updates = yes\napply_updates = no\nreboot = never\n\n[emitters]\nemit_via = stdio\n"

// dnfHost is Rocky Linux 10.2 with dnf-automatic installed, its timers off
// as its preset leaves them, its stock file; or not installed, its timers
// not found.
func dnfHost(t *testing.T, installed bool) *model {
	m := newModel(t, rocky102())
	m.f.AddDir("/etc/dnf", 0o755, 0, 0)
	if installed {
		m.timersOff()
		m.dnfAuto = true
		m.file(dnfauto.Conf, dnfConf)
	}
	return m
}

// unitGone: one of dnf-automatic's timers with the package not installed
// and no state of its own (a mask): systemctl answers not-found and
// inactive, exit 4 (rocky10-test-110, 2026-11-10).
func (m *model) unitGone(verb, unit string) bool {
	_, ok := m.units[verb+" "+unit]
	return !ok && !m.dnfAuto && (unit == dnfauto.InstallTimer || unit == dnfauto.PlainTimer)
}

// timersOff turns dnf-automatic's two timers off, inactive and disabled.
func (m *model) timersOff() {
	for _, tm := range []string{dnfauto.InstallTimer, dnfauto.PlainTimer} {
		m.units["is-active "+tm], m.units["is-enabled "+tm] = "inactive", "disabled"
	}
}

// dnf is rpm's, dnf's and systemctl's answers on Rocky Linux, as the
// control's model has them: the package, its install (a file there before
// stays, the package's beside it as .rpmnew where they differ) and remove
// (the file goes with a remove where it is the stock one, else stays as
// .rpmsave; the timers go), and the timers.
func (m *model) dnf(ctx context.Context, argv []string) hostfake.Response {
	cmd := strings.Join(argv, " ")
	table := func(heading, repo, summary string) hostfake.Response {
		return hostfake.Response{Stdout: "Dependencies resolved.\n" + strings.Repeat("=", 80) + "\n Package  Arch  Version  Repository  Size\n" + strings.Repeat("=", 80) +
			"\n" + heading + ":\n dnf-automatic     noarch     4.20.0-22.el10_2.rocky.0.1       " + repo + "      37 k\n\nTransaction Summary\n" + strings.Repeat("=", 80) + "\n" + summary + "\n",
			Stderr: "Operation aborted.\n", ExitCode: 1}
	}
	switch cmd {
	case "rpm -q " + dnfauto.Package:
		if m.dnfAuto {
			return hostfake.Response{Stdout: "dnf-automatic-4.20.0-22.el10_2.rocky.0.1.noarch\n"}
		}
		return hostfake.Response{Stdout: "package dnf-automatic is not installed\n", ExitCode: 1}
	case "dnf install --assumeno --setopt=install_weak_deps=False --setopt=exit_on_lock=True dnf-automatic":
		if m.noRepos { // dnf's line on rhel10-test-114 (el-rhel.md §5)
			return hostfake.Response{Stderr: "Error: There are no enabled repositories in \"/etc/yum.repos.d\", \"/etc/yum/repos.d\", \"/etc/distro.repos.d\".\n", ExitCode: 1}
		}
		return table("Installing", "baseos", "Install  1 Package")
	case "dnf install -y --setopt=install_weak_deps=False --setopt=exit_on_lock=True dnf-automatic":
		m.dnfAuto = true
		switch data, err := m.f.ReadFile(ctx, dnfauto.Conf); {
		case err != nil:
			m.file(dnfauto.Conf, dnfConf)
		case string(data) != dnfConf:
			m.file(dnfauto.Conf+".rpmnew", dnfConf)
		}
		m.timersOff()
		return hostfake.Response{}
	case "dnf remove --assumeno --noautoremove --setopt=exit_on_lock=True dnf-automatic":
		return table("Removing", "@baseos", "Remove  1 Package")
	case "dnf remove -y --noautoremove --setopt=exit_on_lock=True dnf-automatic":
		m.dnfAuto = false
		if data, err := m.f.ReadFile(ctx, dnfauto.Conf); err == nil {
			if string(data) != dnfConf {
				m.file(dnfauto.Conf+".rpmsave", string(data))
			}
			_ = m.f.Remove(ctx, dnfauto.Conf)
		}
		for _, tm := range []string{dnfauto.InstallTimer, dnfauto.PlainTimer} {
			delete(m.units, "is-active "+tm)
			delete(m.units, "is-enabled "+tm)
		}
		return hostfake.Response{}
	}
	if argv[0] == "systemctl" {
		switch argv[1] {
		case "enable":
			m.units["is-enabled "+argv[2]] = "enabled"
		case "disable":
			m.units["is-enabled "+argv[2]] = "disabled"
		case "start":
			m.units["is-active "+argv[2]] = "active"
		case "stop":
			m.units["is-active "+argv[2]] = "inactive"
		}
		return hostfake.Response{}
	}
	m.t.Errorf("unexpected command %q", cmd)
	return hostfake.Response{Err: hostfake.ErrUnscripted}
}
