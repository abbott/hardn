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
	"github.com/abbott/hardn/pkg/engine/controls/updates"
	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/alpine"
	hostbackup "github.com/abbott/hardn/pkg/host/backup"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/hostfake"
	"github.com/abbott/hardn/pkg/host/revert"
)

// The control's model of a host (control_test.go), as the gallery's
// (internal/tools/uigallery/updateshost.go) in the VMs' shapes of
// 2026-11-03, values synthetic: what apt-config, dpkg-query, systemctl,
// apt-get, apk, rc-service and rc-update answer of a hostfake's files and
// the state below. apt's files are in apt-config dump's one-line form
// (`Name "value";`, an item `Name:: "value";`); a line in any other form
// is apt's syntax error, which every apt-config run then prints; a list is
// one line, `Name { "a"; "b"; };`.

const (
	confDir    = "/etc/apt/apt.conf.d"
	autoUp     = confDir + "/20auto-upgrades"
	uuConf     = confDir + "/50unattended-upgrades"
	hardnFile  = confDir + "/52hardn-auto-upgrades"
	recordPath = "/var/lib/hardn/controls/updates.auto.json"
	hardnYML   = "/etc/hardn/hardn.yml"
	uuName     = "unattended-upgrades"
	apkCronVer = "1.0-r3"

	autoOn     = "APT::Periodic::Update-Package-Lists \"1\";\nAPT::Periodic::Unattended-Upgrade \"1\";\n"
	autoOff    = "APT::Periodic::Update-Package-Lists \"0\";\nAPT::Periodic::Unattended-Upgrade \"0\";\n"
	listsStale = "APT::Periodic::Update-Package-Lists \"0\";\nAPT::Periodic::Unattended-Upgrade \"1\";\n"
	origins    = "Unattended-Upgrade::Origins-Pattern { \"origin=Debian,codename=${distro_codename},label=Debian\"; };\n"
	apkDaily   = "#!/bin/sh\n\napk upgrade --update --quiet\n"
	hardnHead  = "// Written by hardn (Auto Updates).\n"
	// logrotate is the package's one dpkg conffile here (of the five on the
	// VM: 00-inspect-deb13-vm.log, "conffiles"), as shipped.
	logrotate     = "/etc/logrotate.d/unattended-upgrades"
	logrotateConf = "/var/log/unattended-upgrades/*.log {\n  rotate 6\n  monthly\n  compress\n  missingok\n  notifempty\n}\n"
	bothLines     = hardnHead + "APT::Periodic::Update-Package-Lists \"1\";\nAPT::Periodic::Unattended-Upgrade \"1\";\n"
)

// dirDump is apt-config dump Dir's lines aptconf checks.
const dirDump = "Dir \"/\";\nDir::Etc \"etc/apt\";\nDir::Etc::main \"apt.conf\";\nDir::Etc::parts \"apt.conf.d\";\n" +
	"Dir::Ignore-Files-Silently \"\";\nDir::Ignore-Files-Silently:: \"~$\";\nDir::Ignore-Files-Silently:: \"\\.dpkg-[a-z]+$\";\nDir::Ignore-Files-Silently:: \"\\.ucf-[a-z]+$\";\n"

// clock is the engine's and the record's.
var clock = time.Date(2026, 11, 3, 9, 30, 0, 0, time.UTC)

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
	notInRepos bool
	confTail   string            // after the conffile's MD5 in dpkg-query's Conffiles, " obsolete" say
	units      map[string]string // systemctl's state by "<verb> <unit>"
	// Alpine
	apkCron, crond bool
	crashed        bool // rc-service crond status: crashed (exit 32)
	apkKept        bool // apk del leaves apk-cron installed
}

func newModel(t *testing.T, p platform) *model {
	t.Helper()
	m := &model{t: t, f: hostfake.New(host.Options{}), facts: p.facts, units: map[string]string{}}
	for _, d := range []string{"/etc", "/etc/apt", confDir, "/etc/hardn", "/etc/logrotate.d", "/var", "/var/lib", "/var/backups", "/var/log", "/run"} {
		m.f.AddDir(d, 0o755, 0, 0)
	}
	if p.facts.Platform.Distro == detect.DistroAlpine {
		for _, d := range alpine.PeriodicDirs {
			m.f.AddDir(alpine.PeriodicDir+"/"+d, 0o755, 0, 0)
		}
		m.f.AddDir(alpine.CrontabsDir, 0o755, 0, 0)
		m.f.AddFile(alpine.RootCrontab, []byte(alpine.StockCrontab), 0o600, 0, 0)
		m.f.AddDir("/etc/init.d", 0o755, 0, 0)
		m.f.AddFile(alpine.InitScript, []byte("#!/sbin/openrc-run\n"), 0o755, 0, 0)
		m.f.AddDir("/etc/runlevels/default", 0o755, 0, 0)
	}
	m.f.Handler = m.answer
	return m
}

// aptHost is a Debian-family host: unattended-upgrades installed with
// 20auto-upgrades as auto, or not installed where auto is "".
func aptHost(t *testing.T, p platform, auto string) *model {
	m := newModel(t, p)
	if auto != "" {
		m.installed = true
		m.file(uuConf, origins)
		m.file(autoUp, auto)
		m.file(logrotate, logrotateConf)
	}
	return m
}

func (m *model) file(name, data string) { m.f.AddFile(name, []byte(data), 0o644, 0, 0) }

// The hosts of the gallery's menu and variants (gallery-updates.md §2).
func installer(t *testing.T) *model {
	m := aptHost(t, debian13(), "")
	m.deps = []string{"python3-dbus", "python3-distro-info"}
	return m
}

func debianOff(t *testing.T) *model {
	m := aptHost(t, debian13(), autoOff)
	m.debconfOff = true
	return m
}

func alpineHost(t *testing.T) *model { return newModel(t, alpine324()) }

func (m *model) logrotate() *model {
	m.f.AddFile("/etc/periodic/daily/logrotate", []byte("#!/bin/sh\n/usr/sbin/logrotate /etc/logrotate.conf\n"), 0o755, 0, 0)
	return m
}

// engine is the engine over m, as cmd/hardn builds it.
func (m *model) engine() *engine.Engine {
	return engine.New(m.f, engine.Options{
		Revert:  revert.Options{Now: func() time.Time { return clock }, Hardn: "/usr/local/bin/hardn"},
		Detect:  func(context.Context, host.System) (detect.Facts, error) { return m.facts, nil },
		Hold:    func() func() { return func() {} },
		PID:     5000,
		Backups: hostbackup.Settings{Path: hostbackup.DefaultPath, Enabled: true},
	})
}

// control is the control in a direction, with the test's clock.
func control(a updates.AutoAction) updates.Auto {
	return updates.Auto{Action: a, Records: revert.Options{Now: func() time.Time { return clock }}, Version: "v0.4.0"}
}

// apply plans and applies a on m, failing the test on a refusal; it
// returns the plan's change ids and the apply's error.
func (m *model) apply(a updates.AutoAction, cfg *config.Config) ([]string, error) {
	m.t.Helper()
	ctx := context.Background()
	e := m.engine()
	p, err := e.Plan(ctx, []engine.Control{control(a)}, cfg)
	if err != nil {
		m.t.Fatal(err)
	}
	if len(p.Refused) > 0 {
		m.t.Fatalf("refused: %v", p.Refused)
	}
	var ids []string
	for _, c := range p.Changes {
		ids = append(ids, c.ID)
	}
	_, err = e.Apply(ctx, p)
	return ids, err
}

// applied is m after an apply of a that must succeed.
func (m *model) applied(a updates.AutoAction) *model {
	m.t.Helper()
	if _, err := m.apply(a, nil); err != nil {
		m.t.Fatal(err)
	}
	return m
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
	case argv[0] == "systemctl" && len(argv) == 3:
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
	case cmd == "dpkg-query -W -f=${Conffiles}\\n "+uuName:
		if !m.installed {
			return hostfake.Response{Stderr: "dpkg-query: no packages found matching unattended-upgrades\n", ExitCode: 1}
		}
		sum := md5.Sum([]byte(logrotateConf)) //nolint:gosec // dpkg's conffile hash
		return hostfake.Response{Stdout: " " + logrotate + " " + hex.EncodeToString(sum[:]) + m.confTail + "\n\n"}
	case cmd == "apt-get install -s -y "+uuName:
		if m.notInRepos {
			return hostfake.Response{Stdout: "Reading package lists...\n", Stderr: "E: Unable to locate package unattended-upgrades\n", ExitCode: 100}
		}
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
	case cmd == "apk del "+alpine.Package && m.apkKept:
		return hostfake.Response{}
	case cmd == "apk del "+alpine.Package: // an edited file under /etc stays
		m.apkCron = false
		if data, err := m.f.ReadFile(ctx, alpine.Script); err == nil && string(data) == apkDaily {
			_ = m.f.Remove(ctx, alpine.Script)
		}
		return hostfake.Response{Stdout: "(1/1) Purging " + alpine.Package + " (" + apkCronVer + ")\nOK: 9 MiB in 41 packages\n"}
	case cmd == "rc-service crond status":
		if m.crashed {
			return hostfake.Response{Stdout: " * status: crashed\n", ExitCode: 32}
		}
		if m.crond {
			return hostfake.Response{Stdout: " * status: started\n"}
		}
		return hostfake.Response{Stdout: " * status: stopped\n", ExitCode: 3}
	case cmd == "rc-service crond start":
		m.crond = true
		return hostfake.Response{}
	case cmd == "rc-service crond restart":
		m.crond, m.crashed = true, false
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
	m.file(logrotate, logrotateConf)
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
// Dir::Ignore-Files-Silently matches here (a "~" or ".ucf-" or ".dpkg-"
// suffix), then apt.conf.
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

// writes are the calls that changed the fake, and the commands that are
// not reads, after the first n calls.
func writes(f *hostfake.System, n int) []string {
	var out []string
	for _, c := range f.Calls()[n:] {
		switch c.Op {
		case "read", "read-nofollow", "stat", "lstat", "readdir":
		case "run":
			cmd := strings.Join(c.Argv, " ")
			if strings.HasPrefix(cmd, "apt-config dump ") || strings.HasPrefix(cmd, "dpkg-query ") || strings.HasPrefix(cmd, "systemctl is-") ||
				strings.Contains(cmd, " -s -y ") || strings.Contains(cmd, "--simulate") || cmd == "apk info -e apk-cron" || cmd == "rc-service crond status" {
				continue
			}
			out = append(out, cmd)
		default:
			out = append(out, c.String())
		}
	}
	return out
}
