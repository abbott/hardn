package firewall_test

import (
	"bytes"
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/abbott/hardn/pkg/config"
	"github.com/abbott/hardn/pkg/engine"
	"github.com/abbott/hardn/pkg/engine/header"
	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/hostfake"
	"github.com/abbott/hardn/pkg/host/pve"
	"github.com/abbott/hardn/pkg/host/ufw"
	"github.com/abbott/hardn/pkg/output"
	fwscreen "github.com/abbott/hardn/pkg/screens/firewall"
	"github.com/abbott/hardn/pkg/ui/component"
	"github.com/abbott/hardn/pkg/ui/nav"
	"github.com/abbott/hardn/pkg/ui/prompt"
	"github.com/abbott/hardn/pkg/ui/term"
	"github.com/abbott/hardn/pkg/ui/uitest"
)

// The hosts the screen's tests run on: host_test.go's fake, set as the gallery's
// Firewall hosts were (gallery-firewall.md §3, fwHosts), and the states its
// listing drew beside them. Synthetic values only: alice, host-01, 192.0.2.0/24,
// 198.51.100.0/24, 2001:db8::/32, port 2230. hardn.yml is the parity table's
// none.yml (sshPort: 2230, which no row reads) or two.yml.

// screenHosts are the hosts by name, in the gallery's order, then the rest.
var screenHosts = []string{
	"debian13", "ubuntu2604", "active22", "hardened", "allow-policy", "lockout", "lockout-at-start", "allowlist", "blocklist", "edit-2230",
	"moved-2230", "denied", "alpine324", "alpine-stock", "alpine-runlevel", "unit-off", "pve9", "pve9-drop", "profiles",
	"pve-stopped", "pve-moved", "pve-blacklist", "pve-ufw-left", "pve-ufw-unread", "pve-unmounted",
	"no-ss", "ss-failed", "garbled", "status-fails", "proxmox-unknown", "ipv6-off", "ipv6-off-active", "one-family",
	"apt-not-found", "apt-removes", "bound-elsewhere-inactive", "active-no-rule", "profiles-inactive",
	"boot-masked", "boot-not-found", "boot-unread",
}

// screenHost returns the host name names, as its start leaves it, hardn.yml
// on it and as read from it.
func screenHost(t *testing.T, name string) (*fwHost, *config.Config) {
	t.Helper()
	distro, st, fixture := "debian", start{sshd: 22}, "none.yml"
	var set func(h *fwHost)
	switch name {
	case "debian13":
		st.missing = true
	case "ubuntu2604":
		distro = "ubuntu"
	case "active22", "moved-2230", "unit-off", "boot-masked", "boot-not-found", "boot-unread", "profiles":
		st.active, st.ruleOn22 = true, true
	case "garbled":
		st.active, st.ruleOn22 = true, true
		set = func(h *fwHost) { h.statusOut = "Status: active\ngarbage\n" }
	case "hardened":
		st.active, st.sshd = true, 2230
		set = func(h *fwHost) { h.rules = []fwRule{{port: "2230"}} }
	case "allow-policy":
		st.active = true
		set = func(h *fwHost) { h.incoming = "allow" }
	case "lockout":
		st.active, st.sshd = true, 2230
		set = func(h *fwHost) { h.rules = []fwRule{{port: "443"}} }
	case "lockout-at-start":
		st.sshd = 2230
		set = func(h *fwHost) { h.stale, h.rules = true, []fwRule{{port: "443"}} }
	case "allowlist":
		st.active = true
		set = func(h *fwHost) {
			h.rules = []fwRule{{port: "22", src: "192.0.2.0/24"}, {port: "22", src: "2001:db8::/32"}}
		}
	case "one-family":
		st.active = true
		set = func(h *fwHost) { h.rules = []fwRule{{port: "22", src: "192.0.2.0/24"}} }
	case "blocklist":
		set = func(h *fwHost) {
			h.rules = []fwRule{{port: "22", action: "deny", src: "198.51.100.0/24"}, {port: "22"}}
		}
	case "edit-2230":
		st.sshd = 2230
		set = func(h *fwHost) { h.bound = []uint16{22} }
	case "bound-elsewhere-inactive":
		set = func(h *fwHost) { h.bound = []uint16{2230} }
	case "denied":
		st.active = true
		set = func(h *fwHost) { h.rules = []fwRule{{port: "22", action: "deny"}} }
	case "alpine324":
		distro, st.missing = "alpine", true
	case "alpine-stock":
		distro, st.missing, st.noSS, st.noIndex = "alpine", true, true, true
	case "alpine-runlevel":
		distro, st.active, st.ruleOn22 = "alpine", true, true
	case "pve9", "pve9-drop", "pve-stopped", "pve-moved", "pve-blacklist", "pve-ufw-unread", "pve-unmounted":
		distro, st.missing = "proxmox", true
	case "pve-ufw-left":
		distro, st.active, st.ruleOn22 = "proxmox", true, true
	case "no-ss":
		st.noSS = true
	case "ss-failed":
		set = func(h *fwHost) { h.ssFails = true }
	case "status-fails":
		st.active, st.statusFails = true, true
	case "proxmox-unknown":
		st.active, st.ruleOn22 = true, true
		set = func(h *fwHost) {
			h.facts.Proxmox = detect.Proxmox{Installed: detect.Unknown, Err: errors.New("dpkg-query: error: cannot access the dpkg database")}
		}
	case "ipv6-off":
		set = func(h *fwHost) { h.noIPv6 = true }
	case "ipv6-off-active":
		st.active, st.ruleOn22 = true, true
		set = func(h *fwHost) { h.noIPv6 = true }
	case "apt-not-found":
		st.missing = true
		set = func(h *fwHost) {
			h.sim = &hostfake.Response{Stdout: "Reading package lists...\n", Stderr: "E: Unable to locate package ufw\n", ExitCode: 100}
		}
	case "apt-removes":
		st.missing = true
		set = func(h *fwHost) {
			h.sim = &hostfake.Response{Stdout: "Remv nftables [1.1.3-1]\nInst ufw (0.36.2-9 Debian:13.7/stable [all])\nConf ufw (0.36.2-9 Debian:13.7/stable [all])\n"}
		}
	case "active-no-rule":
		st.active = true
	case "profiles-inactive":
		st.ruleOn22 = true
	default:
		t.Fatalf("no host %q", name)
	}
	if name == "profiles" || name == "profiles-inactive" {
		fixture = "two.yml"
	}
	h := newHost(t, distro, st)
	switch name {
	case "boot-masked":
		h.unit = "masked"
	case "boot-not-found":
		h.unit = "not-found"
	case "boot-unread":
		h.unit = "fails"
	case "unit-off":
		h.unit = "disabled"
	case "moved-2230":
		h.bound = []uint16{2230}
	case "pve9-drop", "pve-stopped", "pve-moved", "pve-ufw-unread":
		h.AddFile(pve.ClusterFW, []byte("[OPTIONS]\nenable: 1\npolicy_in: DROP\n\n[IPSET management]\n192.0.2.0/24\n"), 0o640, 0, 33)
	case "pve-blacklist":
		h.AddFile(pve.ClusterFW, []byte("[OPTIONS]\nenable: 1\npolicy_in: DROP\n\n[IPSET management]\n192.0.2.0/24\n\n[IPSET blacklist]\n198.51.100.7\n"), 0o640, 0, 33)
	case "pve-unmounted":
		h.facts.Proxmox.ClusterFS = detect.No
	}
	if name == "pve-stopped" {
		h.pveDown = true
	}
	if name == "pve-moved" {
		h.ports = []uint16{2230}
		h.AddFile("/etc/ssh/sshd_config.d/00-hardn.conf", []byte("Port 2230\n"), 0o644, 0, 0)
	}
	if set != nil {
		set(h)
	}
	if !h.missing {
		h.syncFiles()
	}
	h.AddFile(header.HostnameFile, []byte(hostName+"\n"), 0o444, 0, 0)
	data, err := os.ReadFile(filepath.Join(fixtures, fixture))
	if err != nil {
		t.Fatal(err)
	}
	h.AddFile(yml, data, 0o600, 0, 0)
	cfg, err := config.Read(context.Background(), h, yml)
	if err != nil {
		t.Fatal(err)
	}
	return h, cfg
}

// unreadable is a host whose ReadFile of one file fails.
type unreadable struct {
	*fwHost
	name string
}

// ReadFile fails for u.name.
func (u unreadable) ReadFile(ctx context.Context, name string) ([]byte, error) {
	if name == u.name {
		return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrPermission}
	}
	return u.fwHost.ReadFile(ctx, name)
}

// system is the host the screen runs on: the fake, with ufw.conf unreadable on
// pve-ufw-unread.
func system(name string, h *fwHost) host.System {
	if name == "pve-ufw-unread" {
		return unreadable{h, ufw.ConfFile}
	}
	return h
}

// played is one run of the screen: what it wrote, with the answers echoed, what
// each change did (Options.Ran), how many engines it made, what Run returned and
// how many lines were left unread.
type played struct {
	out     string
	runs    []output.Run
	engines int
	err     error
	unread  int
}

// play runs the screen on the host name names, plain at 120 columns, the user
// typing lines; a dry run with dryRun.
func play(sys host.System, h *fwHost, cfg *config.Config, dryRun bool, lines ...string) played {
	return playWith(sys, h, cfg, dryRun, nil, lines...)
}

// playWith is play with the options set changes, when not nil, in env when set
// changes Prompter. Each change's engine is built as parity_test.go's newEngine
// builds it, on the session sshd is bound on.
func playWith(sys host.System, h *fwHost, cfg *config.Config, dryRun bool, set func(*fwscreen.Options), lines ...string) played {
	var out bytes.Buffer
	env := term.Resolve(term.Options{}, term.Facts{Width: 120})
	ty := uitest.NewTypist(&out, lines...)
	var p played
	build := newEngine(h)
	o := fwscreen.Options{
		Prompter: nav.Prompter{Ctx: component.NewCtx(env), In: prompt.New(ty, false), Out: env.Writer(&out)},
		System:   sys, Facts: h.facts, Session: h.session(), Config: cfg, DryRun: dryRun, LogPath: output.LegacyLogPath, Now: clock,
		NewEngine: func(progress func(engine.Event)) *engine.Engine {
			p.engines++
			return build(progress)
		},
		Ran: func(r output.Run) { p.runs = append(p.runs, r) },
	}
	if set != nil {
		set(&o)
	}
	p.err = fwscreen.Run(context.Background(), o)
	p.out, p.unread = out.String(), ty.Unread()
	return p
}

// writes are the calls on h, from the nth on, that could change the host outside
// /run/hardn, the engine's run directory: every call but a read or a lookup, every
// command but those that only read (isRead).
func writes(h *fwHost, n int) []string {
	var out []string
	for _, c := range h.Calls()[n:] {
		switch c.Op {
		case "read", "read-nofollow", "stat", "lstat", "readdir", "readlink", "resolve-symlink", "lookup-user", "lookup-group":
		case "run", "start-detached":
			if f := host.FormatArgv(c.Argv); !isRead(f) {
				out = append(out, c.Op+" "+f)
			}
		default:
			if !strings.HasPrefix(c.Path, "/run/hardn") {
				out = append(out, c.Op+" "+c.Path)
			}
		}
	}
	return out
}

// ufwCalls are the ufw commands run on h from the nth call on.
func ufwCalls(h *fwHost, n int) []string {
	var out []string
	for _, c := range h.Calls()[n:] {
		if c.Op == "run" && filepath.Base(c.Argv[0]) == "ufw" {
			out = append(out, host.FormatArgv(c.Argv))
		}
	}
	return out
}

// sectionHeader is a section header as plain mode draws it
// (component.SectionHeader): the title from column 2, a space, then a run
// of the rule's "-".
var sectionHeader = regexp.MustCompile(`(?m)^  \S.*? -+$`)

// screens are the screens out shows of the title, each from its title bar to its
// prompt: one per round.
func screens(out, title string) []string {
	var got []string
	for _, part := range strings.Split(out, "# "+title+" #")[1:] {
		end := strings.Index(part, "> Enter selection")
		if end < 0 {
			end = len(part)
		}
		got = append(got, part[:end])
	}
	return got
}

// mainScreens are ufw's screens out shows.
func mainScreens(out string) []string { return screens(out, "UFW Firewall Configuration") }

// rowOf is the value of the screen's row key in s, its marker dropped, a value
// wrapped under a long key read from the next line; "" when it has none.
func rowOf(s, key string) string {
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		t := unmarked(l)
		switch {
		case t == key && i+1 < len(lines):
			return strings.Join(strings.Fields(lines[i+1]), " ")
		case strings.HasPrefix(t, key+"  "):
			return strings.Join(strings.Fields(strings.TrimPrefix(t, key)), " ")
		}
	}
	return ""
}

// unmarked is a row's line without its marker.
func unmarked(l string) string {
	t := strings.TrimSpace(l)
	if i := strings.Index(t, "] "); strings.HasPrefix(t, "[") && i > 0 {
		return strings.TrimSpace(t[i+2:])
	}
	return strings.TrimSpace(strings.TrimPrefix(t, "- "))
}

// marker is the marker of the screen's row key in s, "" when it has none.
func marker(s, key string) string {
	for _, l := range strings.Split(s, "\n") {
		t := strings.TrimSpace(l)
		if u := unmarked(l); strings.HasPrefix(t, "[") && (u == key || strings.HasPrefix(u, key+"  ")) {
			return t[:strings.Index(t, "]")+1]
		}
	}
	return ""
}

// items are the screen's items in s, each "<key> <label>", joined by ", ": the
// labels end at the column the descriptions start in, which the longest label
// meets with one space.
func items(s string) string {
	type item struct{ key, rest string }
	var its []item
	col := -1
	for _, l := range strings.Split(s, "\n") {
		key, rest, ok := strings.Cut(strings.TrimSpace(l), ") ")
		if !ok || len(key) != 1 || key[0] < '1' || key[0] > '9' {
			continue
		}
		its = append(its, item{key, rest})
		if i := strings.Index(rest, "  "); i > 0 {
			start := i + len(rest[i:]) - len(strings.TrimLeft(rest[i:], " "))
			if col < 0 || start < col {
				col = start
			}
		}
	}
	var out []string
	for _, it := range its {
		label := it.rest
		if col > 0 && len(label) > col {
			label = label[:col]
		}
		out = append(out, it.key+" "+strings.TrimSpace(label))
	}
	return strings.Join(out, ", ")
}

// flat is out with each callout's continued lines joined ("\n# "), a word
// broken after its hyphen made whole again, its white space as single spaces, so
// that a wrapped sentence reads whole.
func flat(out string) string {
	out = strings.ReplaceAll(out, "-\n# ", "-")
	return strings.Join(strings.Fields(strings.ReplaceAll(out, "\n# ", " ")), " ")
}

// inOrder returns the first of want that out does not show after what came
// before it, each compared flat; "" when it shows them all, in order.
func inOrder(out string, want ...string) string {
	f, at := flat(out), 0
	for _, w := range want {
		i := strings.Index(f[at:], strings.Join(strings.Fields(w), " "))
		if i < 0 {
			return w
		}
		at += i + 1
	}
	return ""
}

// hasRule reports whether ufw on h holds a rule on port.
func hasRule(h *fwHost, port string) bool {
	return slices.ContainsFunc(h.rules, func(r fwRule) bool { return r.port == port && r.verb() == "allow" })
}
