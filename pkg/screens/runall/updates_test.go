package runall_test

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

// Auto Updates' model: the minimal parts of the gallery's
// (internal/tools/uigallery/updateshost.go, withApt and withApk, its
// apt-config dump over apt's files), copied as a test cannot import the
// gallery, so that the hosts ask for Auto Updates where the gallery's do.
// apt's configuration is files under /etc/apt/apt.conf.d, read in name
// order, then /etc/apt/apt.conf, a later scalar winning (apt.conf(5)),
// scalars in the dump's one-line form, lists in the packages' block form;
// a line in any other form is apt's syntax error. Synthetic values only.

// The files and versions the model answers.
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

// What the packages ship, as the gallery's model writes it: 20auto-upgrades
// on, Ubuntu's 10periodic, apk-cron's daily script.
const (
	autoOn    = "APT::Periodic::Update-Package-Lists \"1\";\nAPT::Periodic::Unattended-Upgrade \"1\";\n"
	ubuntu10  = "APT::Periodic::Update-Package-Lists \"1\";\nAPT::Periodic::Download-Upgradeable-Packages \"0\";\nAPT::Periodic::AutocleanInterval \"0\";\n"
	apkScript = "#!/bin/sh\n\napk upgrade --update --quiet\n"
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

// updSim is the model's state beyond the files: unattended-upgrades
// installed, apk-cron installed, crond started.
type updSim struct {
	uu      bool
	apkCron bool
	crond   bool
}

// withApt gives h, a Debian-family host, the gallery's apt model:
// unattended-upgrades installed and on where on is set, not installed
// otherwise.
func (h *sshHost) withApt(on bool) *sshHost {
	h.upd = &updSim{uu: on}
	h.AddDir("/etc/apt", 0o755, 0, 0)
	h.AddDir(aptConfDir, 0o755, 0, 0)
	if h.plat.socket {
		h.AddFile(periodic10, []byte(ubuntu10), 0o644, 0, 0)
	}
	if on {
		h.AddFile(uuConfig, []byte(uuOrigins(h.plat.socket)), 0o644, 0, 0)
		h.AddFile(autoUpgrades, []byte(autoOn), 0o644, 0, 0)
	}
	return h
}

// withApk gives h, the Alpine host, the gallery's apk model: apk-cron not
// installed, /etc/periodic's directories empty, root's stock crontab, crond
// in no runlevel and started, as on the Alpine VM.
func (h *sshHost) withApk() *sshHost {
	h.upd = &updSim{crond: true}
	for _, d := range []string{"/etc/periodic", "/etc/periodic/15min", "/etc/periodic/hourly", alpine.DailyDir, "/etc/periodic/weekly", "/etc/periodic/monthly", "/etc/crontabs", "/etc/init.d", "/etc/runlevels", "/etc/runlevels/default"} {
		h.AddDir(d, 0o755, 0, 0)
	}
	h.AddFile(alpine.RootCrontab, []byte(alpine.StockCrontab), 0o600, 0, 0)
	h.AddFile(alpine.InitScript, []byte("#!/sbin/openrc-run\n"), 0o755, 0, 0)
	return h
}

// updates answers Auto Updates' commands by the model; with aptBroken,
// apt's syntax error, every apt-config run fails, as an administrator's
// typo makes it. A host with no model answers none.
func (h *sshHost) updates(argv []string) (hostfake.Response, bool) {
	u := h.upd
	if argv[0] == "apt-config" && h.aptBroken {
		return hostfake.Response{Stderr: "E: Syntax error /etc/apt/apt.conf.d/99local:1: Extra junk at end of file\n", ExitCode: 100}, true
	}
	if u == nil {
		return hostfake.Response{}, false
	}
	ctx := context.Background()
	cmd := strings.Join(argv, " ")
	switch {
	case cmd == "apt-config dump Dir":
		return hostfake.Response{Stdout: aptDirDump()}, true
	case argv[0] == "apt-config" && len(argv) > 2 && argv[1] == "dump":
		out, err := aptDump(ctx, h.System, argv[2:])
		if err != nil {
			return hostfake.Response{Stderr: "E: " + err.Error() + "\n", ExitCode: 100}, true
		}
		return hostfake.Response{Stdout: out}, true
	case argv[0] == "dpkg-query" && argv[len(argv)-1] == uuPackage:
		if !u.uu {
			return hostfake.Response{Stderr: "dpkg-query: no packages found matching " + uuPackage + "\n", ExitCode: 1}, true
		}
		return hostfake.Response{Stdout: "install ok installed\t" + uuVersion + "\n"}, true
	case cmd == "apt-get install -s -y "+uuPackage:
		return hostfake.Response{Stdout: "NOTE: This is only a simulation!\nInst " + uuPackage + " (" + uuVersion + " Debian:13.1/stable [all])\n"}, true
	case cmd == "apt-get install -y "+uuPackage: // the package, 50unattended-upgrades and the postinst's 20auto-upgrades, kept where there
		u.uu = true
		for f, data := range map[string]string{uuConfig: uuOrigins(h.plat.socket), autoUpgrades: autoOn} {
			if _, err := h.Lstat(ctx, f); errors.Is(err, fs.ErrNotExist) {
				h.AddFile(f, []byte(data), 0o644, 0, 0)
			}
		}
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
		h.AddFile(alpine.Script, []byte(apkScript), 0o755, 0, 0)
		return hostfake.Response{Stdout: "(1/1) Installing " + alpine.Package + " (" + apkCronVer + ")\nOK: 9 MiB in 42 packages\n"}, true
	case cmd == "apk del "+alpine.Package:
		u.apkCron = false
		if data, err := h.ReadFile(ctx, alpine.Script); err == nil && string(data) == apkScript {
			_ = h.Remove(ctx, alpine.Script)
		}
		return hostfake.Response{}, true
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
		h.AddSymlink(alpine.RunlevelLink, alpine.InitScript)
		return hostfake.Response{}, true
	case cmd == "rc-update del crond default":
		_ = h.Remove(ctx, alpine.RunlevelLink)
		return hostfake.Response{}, true
	}
	return hostfake.Response{}, false
}

// aptEntry is one item of apt's configuration as a file sets it.
type aptEntry struct {
	name, value string
	item        bool // a list item
}

// aptIgnored are the suffixes apt skips in apt.conf.d
// (Dir::Ignore-Files-Silently's defaults).
var aptIgnored = []string{"~", ".disabled", ".bak", ".save", ".orig", ".distUpgrade", ".ucf-dist", ".ucf-old", ".ucf-new", ".dpkg-old", ".dpkg-dist", ".dpkg-new"}

// aptDirDump is apt-config dump Dir as the VMs print it, the model's
// aptIgnored as Dir::Ignore-Files-Silently's patterns.
func aptDirDump() string {
	var b strings.Builder
	b.WriteString("Dir \"/\";\nDir::Etc \"etc/apt\";\nDir::Etc::main \"apt.conf\";\nDir::Etc::parts \"apt.conf.d\";\nDir::Ignore-Files-Silently \"\";\n")
	for _, s := range aptIgnored {
		fmt.Fprintf(&b, "Dir::Ignore-Files-Silently:: \"%s$\";\n", regexp.QuoteMeta(s))
	}
	return b.String()
}

// aptConfEntries reads apt's configuration as the model writes it: each
// file of apt.conf.d in name order, then apt.conf.
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
		list := ""
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
					return nil, fmt.Errorf("syntax error %s:%d: extra junk at end of file", f, i+1)
				}
				out = append(out, aptEntry{name: list, value: v, item: true})
				continue
			}
			name, rest, ok := strings.Cut(l, ` "`)
			value, closed := strings.CutSuffix(rest, `";`)
			if !ok || !closed || name == "" || strings.ContainsAny(name, " \t\"") || strings.Contains(value, `"`) {
				return nil, fmt.Errorf("syntax error %s:%d: extra junk at end of file", f, i+1)
			}
			n, item := strings.CutSuffix(name, "::")
			out = append(out, aptEntry{name: n, value: value, item: item})
		}
	}
	return out, nil
}

// aptDump is apt-config dump of roots: each root's node, then the items
// under it in the order first set, a scalar's value the last set, a list's
// items in order.
func aptDump(ctx context.Context, sys host.System, roots []string) (string, error) {
	es, err := aptConfEntries(ctx, sys)
	if err != nil {
		return "", err
	}
	type node struct {
		name, value string
		items       []string
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
