package main

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"regexp"
	"slices"
	"strings"

	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/alpine"
	"github.com/abbott/hardn/pkg/host/hostfake"
)

// The Auto Updates mock's model of a host (updates.go; refactor plan slice
// 7, unit (a); docs/engine/gallery-updates.md): what apt, dpkg, apk and
// OpenRC answer of unattended-upgrades, apt's configuration, apk-cron and
// crond, in the shapes the three VMs showed on 2026-11-03
// (vm-runs/s7/00-inspect-*.log), their values synthetic. It sits on a
// status host (statusOn), which answers the rest of the main menu's checks;
// systemctl's states of apt's timers are the status host's units.
//
// apt's configuration is files under /etc/apt/apt.conf.d, read in name
// order, then /etc/apt/apt.conf, a later scalar winning (apt.conf(5)); the
// model's scalars are written in apt-config dump's own one-line form
// (`Name "value";`), its lists in the packages' block form (`Name {`, a
// `"value";` line each, `};`), which aptconf.Read takes and the dump's
// `Name:: "value";` it refuses; a line in any other form is apt's syntax
// error.

// The files and commands the model answers.
const (
	aptConfDir   = "/etc/apt/apt.conf.d"
	aptConfMain  = "/etc/apt/apt.conf"
	autoUpgrades = aptConfDir + "/20auto-upgrades"
	uuConfig     = aptConfDir + "/50unattended-upgrades"
	periodic10   = aptConfDir + "/10periodic"
	uuPackage    = "unattended-upgrades"
	uuVersion    = "2.12"
	apkCronVer   = "1.0-r3"
)

// What the packages ship, as the model writes it (synthetic, in the dump's
// form): 20auto-upgrades from the postinst by its debconf answer (the
// package's 20auto-upgrades or 20auto-upgrades-disabled), Debian's
// Origins-Pattern or Ubuntu's Allowed-Origins, Ubuntu's 10periodic
// (update-notifier-common) and apk-cron's daily script. Root's stock
// crontab on Alpine is alpine.StockCrontab, the VM's.
const (
	autoOn     = "APT::Periodic::Update-Package-Lists \"1\";\nAPT::Periodic::Unattended-Upgrade \"1\";\n"
	autoOff    = "APT::Periodic::Update-Package-Lists \"0\";\nAPT::Periodic::Unattended-Upgrade \"0\";\n"
	listsStale = "APT::Periodic::Update-Package-Lists \"0\";\nAPT::Periodic::Unattended-Upgrade \"1\";\n"
	ubuntu10   = "APT::Periodic::Update-Package-Lists \"1\";\nAPT::Periodic::Download-Upgradeable-Packages \"0\";\nAPT::Periodic::AutocleanInterval \"0\";\n"
	apkScript  = "#!/bin/sh\n\napk upgrade --update --quiet\n"
)

// uuOrigins is 50unattended-upgrades: Ubuntu's Allowed-Origins or Debian's
// Origins-Pattern.
func uuOrigins(ubuntu bool) string {
	if ubuntu {
		return "Unattended-Upgrade::Allowed-Origins {\n\t\"${distro_id}:${distro_codename}\";\n" +
			"\t\"${distro_id}:${distro_codename}-security\";\n};\n"
	}
	return "Unattended-Upgrade::Origins-Pattern {\n\t\"origin=Debian,codename=${distro_codename},label=Debian\";\n" +
		"\t\"origin=Debian,codename=${distro_codename},label=Debian-Security\";\n};\n"
}

// updSim is the model's state beyond the files: the debconf answer the
// postinst reads, the dependencies apt installs with unattended-upgrades,
// and whether apk-cron is installed and crond runs. The package's files,
// apt's configuration, the daily script and crond's runlevel link are the
// fake's files.
type updSim struct {
	// debconfOff is unattended-upgrades/enable_auto_updates false (a
	// preseeded answer): the postinst writes 20auto-upgrades-disabled.
	// Unset is the template's default, true: installing turns updates on.
	debconfOff bool
	deps       []string // what apt-get install -s lists with unattended-upgrades
	extra      []string // the simulation's other lines: a removal or an upgrade
	offExtra   []string // off's simulations' other lines (apt-get purge/remove -s, apk del --simulate)
	apkCron    bool
	crond      bool // rc-service crond status: started
}

// answer answers the commands of the updates' model, and reports whether
// it did: none on a host with no model.
func (u *updSim) answer(h *fakeHost, argv []string) (hostfake.Response, bool) {
	if u == nil {
		return hostfake.Response{}, false
	}
	ctx := context.Background()
	cmd := strings.Join(argv, " ")
	switch {
	case cmd == "apt-config dump Dir": // the model's files set nothing under Dir
		return hostfake.Response{Stdout: aptDirDump()}, true
	case argv[0] == "apt-config" && len(argv) > 2 && argv[1] == "dump":
		out, err := aptDump(ctx, h.f, argv[2:])
		if err != nil {
			return hostfake.Response{Stderr: "E: " + err.Error() + "\n", ExitCode: 100}, true
		}
		return hostfake.Response{Stdout: out}, true
	case cmd == "apt-get install -s -y "+uuPackage:
		var out strings.Builder
		out.WriteString("NOTE: This is only a simulation!\n")
		for _, p := range append(slices.Clone(u.deps), uuPackage) {
			fmt.Fprintf(&out, "Inst %s (%s Debian:13.1/stable [all])\n", p, uuVersion)
		}
		for _, l := range u.extra {
			out.WriteString(l + "\n")
		}
		return hostfake.Response{Stdout: out.String()}, true
	case cmd == "apt-get install -y "+uuPackage:
		u.install(h)
		return hostfake.Response{}, true
	case cmd == "apt-get purge -s -y "+uuPackage:
		return hostfake.Response{Stdout: "NOTE: This is only a simulation!\nPurg " + uuPackage + " [" + uuVersion + "]\n" + lines(u.offExtra)}, true
	case cmd == "apt-get purge -y "+uuPackage: // the postrm: rm -f both files, edited or not
		delete(h.status.packages, uuPackage)
		_ = h.f.Remove(ctx, autoUpgrades)
		_ = h.f.Remove(ctx, uuConfig)
		u.debconfOff = false
		return hostfake.Response{}, true
	case cmd == "apt-get remove -s -y "+uuPackage:
		return hostfake.Response{Stdout: "NOTE: This is only a simulation!\nRemv " + uuPackage + " [" + uuVersion + "]\n" + lines(u.offExtra)}, true
	case cmd == "apt-get remove -y "+uuPackage: // ucf's and the conffile stay
		delete(h.status.packages, uuPackage)
		return hostfake.Response{}, true
	case cmd == "apk info -e "+alpine.Package:
		if u.apkCron {
			return hostfake.Response{Stdout: alpine.Package + "\n"}, true
		}
		return hostfake.Response{ExitCode: 1}, true
	case cmd == "apk add --simulate "+alpine.Package:
		return hostfake.Response{Stdout: "(1/1) Installing " + alpine.Package + " (" + apkCronVer + ")\nOK: 9 MiB in 42 packages\n"}, true
	case cmd == "apk add "+alpine.Package:
		u.apkCron = true
		h.f.AddFile(alpine.Script, []byte(apkScript), 0o755, 0, 0)
		return hostfake.Response{Stdout: "(1/1) Installing " + alpine.Package + " (" + apkCronVer + ")\nOK: 9 MiB in 42 packages\n"}, true
	case cmd == "apk del --simulate "+alpine.Package:
		return hostfake.Response{Stdout: "(1/1) Purging " + alpine.Package + " (" + apkCronVer + ")\n" + lines(u.offExtra) + "OK: 9 MiB in 41 packages\n"}, true
	case cmd == "apk del "+alpine.Package: // an edited file under /etc stays (apk_db_purge_pkg)
		u.apkCron = false
		if data, err := h.f.ReadFile(ctx, alpine.Script); err == nil && string(data) == apkScript {
			_ = h.f.Remove(ctx, alpine.Script)
		}
		return hostfake.Response{Stdout: "(1/1) Purging " + alpine.Package + " (" + apkCronVer + ")\nOK: 9 MiB in 41 packages\n"}, true
	case cmd == "rc-service crond status":
		if u.crond {
			return hostfake.Response{Stdout: " * status: started\n"}, true
		}
		return hostfake.Response{Stdout: " * status: stopped\n", ExitCode: 3}, true
	case cmd == "rc-service crond start":
		u.crond = true
		return hostfake.Response{}, true
	case cmd == "rc-service crond stop":
		u.crond = false
		return hostfake.Response{}, true
	case cmd == "rc-update add crond default":
		h.f.AddSymlink(alpine.RunlevelLink, alpine.InitScript)
		return hostfake.Response{}, true
	case cmd == "rc-update del crond default":
		_ = h.f.Remove(ctx, alpine.RunlevelLink)
		return hostfake.Response{}, true
	}
	return hostfake.Response{}, false
}

// install is apt-get install's effect: the package and the dependencies
// it lists, 50unattended-upgrades, and the postinst's 20auto-upgrades by
// the debconf answer, through ucf, which keeps a file already there.
func (u *updSim) install(h *fakeHost) {
	h.status.packages[uuPackage] = uuVersion
	for _, d := range u.deps {
		h.status.packages[d] = "1.0"
	}
	u.deps = nil
	ctx := context.Background()
	if _, err := h.f.Lstat(ctx, uuConfig); errors.Is(err, fs.ErrNotExist) {
		h.f.AddFile(uuConfig, []byte(uuOrigins(h.socket)), 0o644, 0, 0)
	}
	if _, err := h.f.Lstat(ctx, autoUpgrades); errors.Is(err, fs.ErrNotExist) {
		data := autoOn
		if u.debconfOff {
			data = autoOff
		}
		h.f.AddFile(autoUpgrades, []byte(data), 0o644, 0, 0)
	}
}

// aptEntry is one item of apt's configuration as a file sets it.
type aptEntry struct {
	file, name, value string
	item              bool // a list item, `Name:: "value";`
}

// aptIgnored are the suffixes apt skips in apt.conf.d
// (Dir::Ignore-Files-Silently's defaults).
var aptIgnored = []string{"~", ".disabled", ".bak", ".save", ".orig", ".distUpgrade", ".ucf-dist", ".ucf-old", ".ucf-new", ".dpkg-old", ".dpkg-dist", ".dpkg-new"}

// aptDirDump is apt-config dump Dir as aptconf.Read reads it: Dir,
// Dir::Etc, main and parts as the VMs print them (aptconf's dirDump,
// 02-inspect-c-*.log), then Dir::Ignore-Files-Silently's items, the
// model's aptIgnored as patterns, so that aptconf skips what the model
// skips. The VMs' own items are patterns of the same suffixes ("~$",
// "\.dpkg-[a-z]+$", "\.ucf-[a-z]+$" among them); the model's are literal.
func aptDirDump() string {
	var b strings.Builder
	b.WriteString("Dir \"/\";\nDir::Etc \"etc/apt\";\nDir::Etc::main \"apt.conf\";\nDir::Etc::parts \"apt.conf.d\";\nDir::Ignore-Files-Silently \"\";\n")
	for _, s := range aptIgnored {
		fmt.Fprintf(&b, "Dir::Ignore-Files-Silently:: \"%s$\";\n", regexp.QuoteMeta(s))
	}
	return b.String()
}

// aptConfEntries reads apt's configuration as the model writes it: each
// file of apt.conf.d in name order, then apt.conf; a line not in the
// dump's form is apt's syntax error, naming the file and line.
func aptConfEntries(ctx context.Context, sys host.System) ([]aptEntry, error) {
	des, err := sys.ReadDir(ctx, aptConfDir)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}
	var files []string
	for _, de := range des {
		if !slices.ContainsFunc(aptIgnored, func(s string) bool { return strings.HasSuffix(de.Name(), s) }) {
			files = append(files, path.Join(aptConfDir, de.Name()))
		}
	}
	slices.Sort(files)
	files = append(files, aptConfMain)
	var out []aptEntry
	for _, f := range files {
		data, err := sys.ReadFile(ctx, f)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		list := "" // the name of the list block open, `Name {` to `};`
		for i, l := range strings.Split(string(data), "\n") {
			l = strings.TrimSpace(l)
			if l == "" || strings.HasPrefix(l, "//") {
				continue
			}
			if name, ok := strings.CutSuffix(l, " {"); ok && list == "" && name != "" && !strings.ContainsAny(name, " \t\"") {
				list = name
				continue
			}
			if list != "" {
				if l == "};" {
					list = ""
					continue
				}
				v, ok := strings.CutPrefix(l, `"`)
				v, closed := strings.CutSuffix(v, `";`)
				if !ok || !closed || strings.Contains(v, `"`) {
					return nil, fmt.Errorf("Syntax error %s:%d: Extra junk at end of file", f, i+1)
				}
				out = append(out, aptEntry{file: f, name: list, value: v, item: true})
				continue
			}
			name, rest, ok := strings.Cut(l, ` "`)
			value, closed := strings.CutSuffix(rest, `";`)
			if !ok || !closed || name == "" || strings.ContainsAny(name, " \t\"") || strings.Contains(value, `"`) {
				return nil, fmt.Errorf("Syntax error %s:%d: Extra junk at end of file", f, i+1)
			}
			n, item := strings.CutSuffix(name, "::")
			out = append(out, aptEntry{file: f, name: n, value: value, item: item})
		}
	}
	return out, nil
}

// aptDump is apt-config dump of roots: each root's node, then the items
// under it in the order first set, a scalar's value the last set, a list's
// items in order, each name as its first writer spelt it.
func aptDump(ctx context.Context, sys host.System, roots []string) (string, error) {
	es, err := aptConfEntries(ctx, sys)
	if err != nil {
		return "", err
	}
	type node struct {
		name  string
		value string
		items []string
	}
	var order []string
	nodes := map[string]*node{}
	for _, e := range es {
		k := strings.ToLower(e.name)
		n, ok := nodes[k]
		if !ok {
			n = &node{name: e.name}
			nodes[k] = n
			order = append(order, k)
		}
		if e.item {
			n.items = append(n.items, e.value)
		} else {
			n.value = e.value
		}
	}
	var b strings.Builder
	for _, root := range roots {
		r := strings.ToLower(root)
		var under []string
		for _, k := range order {
			if k == r || strings.HasPrefix(k, r+"::") {
				under = append(under, k)
			}
		}
		if len(under) == 0 {
			continue
		}
		fmt.Fprintf(&b, "%s \"\";\n", root)
		for _, k := range under {
			n := nodes[k]
			if k == r {
				for _, it := range n.items {
					fmt.Fprintf(&b, "%s:: %q;\n", n.name, it)
				}
				continue
			}
			if n.items == nil || n.value != "" {
				fmt.Fprintf(&b, "%s %q;\n", n.name, n.value)
			}
			for _, it := range n.items {
				fmt.Fprintf(&b, "%s:: %q;\n", n.name, it)
			}
		}
	}
	return b.String(), nil
}

// The hosts' builders: a status host of the platform with the updates'
// model, in the VMs' shapes and the departures each host names.

// updApt returns a Debian-family host: unattended-upgrades installed with
// 20auto-upgrades as auto, or not installed where auto is "".
func updApt(p platform, auto string) *fakeHost {
	h := statusOn(p)
	h.upd = &updSim{}
	h.f.AddDir(aptConfDir, 0o755, 0, 0)
	if p.socket {
		h.f.AddFile(periodic10, []byte(ubuntu10), 0o644, 0, 0)
	}
	delete(h.status.packages, uuPackage)
	if auto != "" {
		h.status.packages[uuPackage] = uuVersion
		h.f.AddFile(uuConfig, []byte(uuOrigins(p.socket)), 0o644, 0, 0)
		h.f.AddFile(autoUpgrades, []byte(auto), 0o644, 0, 0)
	}
	return h
}

// updAlpine returns Alpine 3.23 as the VM: apk-cron not installed,
// /etc/periodic's directories empty, root's stock crontab, crond in no
// runlevel and stopped.
func updAlpine() *fakeHost {
	h := statusOn(alpine323Platform)
	h.upd = &updSim{}
	for _, d := range []string{"15min", "hourly", "daily", "weekly", "monthly"} {
		h.f.AddDir("/etc/periodic/"+d, 0o755, 0, 0)
	}
	h.f.AddDir("/etc/crontabs", 0o755, 0, 0)
	h.f.AddFile(alpine.RootCrontab, []byte(alpine.StockCrontab), 0o600, 0, 0) // the VM's 283 bytes (alpine.StockCrontab)
	_ = h.f.Remove(context.Background(), alpine.RunlevelLink)
	return h
}

// lines joins ls, each ended by a newline.
func lines(ls []string) string {
	var b strings.Builder
	for _, l := range ls {
		b.WriteString(l + "\n")
	}
	return b.String()
}
