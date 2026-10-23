package dns_test

import (
	"bytes"
	"context"
	"io/fs"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/abbott/hardn/pkg/config"
	"github.com/abbott/hardn/pkg/engine"
	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/backup"
	"github.com/abbott/hardn/pkg/host/detect"
	hostdns "github.com/abbott/hardn/pkg/host/dns"
	"github.com/abbott/hardn/pkg/host/hostfake"
	"github.com/abbott/hardn/pkg/host/revert"
	"github.com/abbott/hardn/pkg/output"
	dnsscreen "github.com/abbott/hardn/pkg/screens/dns"
	"github.com/abbott/hardn/pkg/ui/component"
	"github.com/abbott/hardn/pkg/ui/nav"
	"github.com/abbott/hardn/pkg/ui/prompt"
	"github.com/abbott/hardn/pkg/ui/term"
	"github.com/abbott/hardn/pkg/ui/uitest"
)

// The hosts the screen's tests run on, on hostfake, with a model of
// systemd-resolved that loads hardn's drop-in when reloaded, after the DNS
// control's tests (pkg/engine/controls/dns helpers_test.go), which a test
// file of another package cannot share; written from dns.md §3, not
// captured from a host. Synthetic values only: web-01.example.net,
// 192.0.2.0/24, 2001:db8::/32, example.net; the presets keep their public
// resolvers.

const (
	hardnYML = "/etc/hardn/hardn.yml"
	dhcpNS   = "192.0.2.1"
)

// configured is hardn.yml's list on most hosts.
var configured = []string{"192.0.2.54", "2001:db8::54"}

// now is the engine's clock and AfterFlow's.
var now = time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)

// hardnYMLData is hardn.yml: list, under a comment.
func hardnYMLData(list []string) []byte {
	var b strings.Builder
	b.WriteString("# hardn's configuration (web-01.example.net)\nusername: alice\n")
	if len(list) == 0 {
		b.WriteString("nameservers: []\n")
	} else {
		b.WriteString("nameservers:\n")
		for _, ns := range list {
			b.WriteString("  - \"" + ns + "\"\n")
		}
	}
	return []byte(b.String())
}

// fakeHost is one host: its facts, the fake, systemd-resolved's state,
// and writes that fail.
type fakeHost struct {
	*hostfake.System
	facts detect.Facts

	mu     sync.Mutex
	active string   // systemctl is-active's word for systemd-resolved
	global []string // the global servers resolved has loaded
	inert  bool     // a reload exits 0 and loads nothing new
	// links and domains are what resolvectl dns and domain list after the
	// global line: eth0's server and search domain from DHCP.
	links, domains string
	fail           map[string]error
	// lstat are the Lstat calls that fail, by path.
	lstat map[string]error
}

// Lstat fails for a path in lstat.
func (h *fakeHost) Lstat(ctx context.Context, name string) (fs.FileInfo, error) {
	if err := h.lstat[name]; err != nil {
		return nil, &fs.PathError{Op: "lstat", Path: name, Err: err}
	}
	return h.System.Lstat(ctx, name)
}

// WriteFileAtomic fails for a path in fail.
func (h *fakeHost) WriteFileAtomic(ctx context.Context, name string, data []byte, perm fs.FileMode, uid, gid int) error {
	h.mu.Lock()
	err := h.fail[name]
	h.mu.Unlock()
	if err != nil {
		return &fs.PathError{Op: "rename", Path: name, Err: err}
	}
	return h.System.WriteFileAtomic(ctx, name, data, perm, uid, gid)
}

func facts(distro detect.Distro, version string, init detect.InitKind) detect.Facts {
	return detect.Facts{Platform: detect.Platform{Distro: distro, Version: version, Support: detect.Supported},
		Init: detect.Init{Kind: init, Running: detect.Yes}, Container: detect.Container{Detected: detect.No}}
}

// The hosts, by name.
const (
	debian13       = "debian13"       // dhcpcd's file
	configuredHost = "configured"     // Debian 13, the file lists hardn.yml's
	ubuntu2604     = "ubuntu2604"     // systemd-resolved's stub link, eth0 from DHCP
	alpine324      = "alpine324"      // udhcpc's file, its stock udhcpc.conf, ifupdown-ng running udhcpc for eth0
	alpineCloud    = "alpine-cloud"   // Alpine 3.23's cloud image, as the 2026-10-23 VM: dhcpcd installed, which ifupdown-ng runs
	alpineProgram  = "alpine-program" // the cloud image, eth0's dhcp-program udhcpc
	alpineUnknown  = "alpine-unknown" // Alpine 3.24, ifupdown-ng's DHCP executor absent: the client not established
	pve9           = "pve9"           // the installer's file
	networkManager = "networkmanager" // NetworkManager's link
	resolvconf     = "resolvconf"     // resolvconf's link
	container      = "container"      // an LXC container
	unread         = "unread"         // Debian 13 whose container fact is not established
	unreadable     = "unreadable"     // Debian 13 whose /etc/resolv.conf cannot be examined
	resolvedOff    = "resolved-off"   // Ubuntu 26.04, systemd-resolved stopped
	overridden     = "overridden"     // Ubuntu 26.04, a later drop-in setting DNS=
	blocked        = "blocked"        // Ubuntu 26.04, hardn's drop-in path masked
	otherLink      = "other-link"     // a link hardn does not know (connman's)
	resolvedFile   = "resolved-file"  // Debian 13's file, systemd-resolved running beside it
	unsupported    = "unsupported"    // a family hardn does not configure
)

// udhcpcStock is Alpine's udhcpc.conf as installed.
const udhcpcStock = "# Do not overwrite /etc/resolv.conf\n#RESOLV_CONF=\"no\"\n\n#NO_DNS=\"eth1 wlan1\"\n"

// dhcpEth0 is an interfaces file with eth0 on DHCP, as Alpine's installers
// write it.
const dhcpEth0 = "auto lo\niface lo inet loopback\n\nauto eth0\niface eth0 inet dhcp\n"

// ifupdown adds Alpine's ifupdown-ng as installed, with interfaces as
// its configuration: its DHCP executor (dns.DHCPExecutorRule) and busybox's
// udhcpc, so that it runs udhcpc for an interface on DHCP unless dhcpcd or
// dhclient is installed.
func ifupdown(h *hostfake.System, interfaces string) {
	h.AddFile(hostdns.Interfaces, []byte(interfaces), 0o644, 0, 0)
	h.AddFile(hostdns.DHCPExecutor, []byte("#!/bin/sh\n"+hostdns.DHCPExecutorRule), 0o755, 0, 0)
	h.AddFile("/bin/busybox", nil, 0o755, 0, 0)
	h.AddSymlink("/sbin/udhcpc", "/bin/busybox")
}

// newHost returns the host name names, with hardn.yml listing list, and
// hardn.yml as read from it.
func newHost(t *testing.T, name string, list []string) (*fakeHost, *config.Config) {
	t.Helper()
	h := &fakeHost{System: hostfake.New(host.Options{}), active: "inactive", fail: map[string]error{}, lstat: map[string]error{},
		facts: facts(detect.DistroDebian, "13", detect.Systemd),
		links: "Link 2 (eth0): " + dhcpNS + "\n", domains: "Link 2 (eth0): example.net\n"}
	for _, d := range []string{"/etc", "/etc/hardn", "/var/lib", "/var/backups", "/run", "/var/log"} {
		h.AddDir(d, 0o755, 0, 0)
	}
	file := func(s string) { h.AddFile(hostdns.ResolvConf, []byte(s), 0o644, 0, 0) }
	resolved := func(active string) {
		h.facts = facts(detect.DistroUbuntu, "26.04", detect.Systemd)
		h.active = active
		h.AddSymlink(hostdns.ResolvConf, "../run/systemd/resolve/stub-resolv.conf")
		h.AddFile("/etc/systemd/resolved.conf", []byte("[Resolve]\n#DNS=\n#Domains=\n"), 0o644, 0, 0)
	}
	const dhcpcd = "# Generated by dhcpcd from eth0.dhcp\nsearch example.net\n"
	switch name {
	case debian13, unread, unreadable, resolvedFile, unsupported:
		file(dhcpcd + "nameserver " + dhcpNS + "\n")
		h.AddFile("/usr/sbin/dhcpcd", nil, 0o755, 0, 0)
		switch name {
		case unread:
			h.facts.Container = detect.Container{Detected: detect.Unknown}
		case unreadable:
			h.lstat[hostdns.ResolvConf] = fs.ErrPermission
		case resolvedFile:
			h.active = "active"
		case unsupported:
			h.facts.Platform = detect.Platform{Distro: detect.DistroOther, Support: detect.Unlisted}
		}
	case configuredHost:
		file(dhcpcd + "nameserver 192.0.2.54\nnameserver 2001:db8::54\n")
		h.AddFile("/usr/sbin/dhcpcd", nil, 0o755, 0, 0)
	case ubuntu2604:
		resolved("active")
	case resolvedOff:
		resolved("inactive")
	case overridden:
		resolved("active")
		h.AddFile("/etc/systemd/resolved.conf.d/zzz-site.conf", []byte("[Resolve]\nDNS=192.0.2.7\n"), 0o644, 0, 0)
	case blocked:
		resolved("active")
		h.AddSymlink(hostdns.DropIn, "/dev/null")
	case otherLink:
		h.AddSymlink(hostdns.ResolvConf, "/run/connman/resolv.conf")
	case alpine324, alpineUnknown:
		h.facts = facts(detect.DistroAlpine, "3.24", detect.OpenRC)
		file("search example.net\nnameserver " + dhcpNS + "\n")
		h.AddFile(hostdns.UDHCPCConf, []byte(udhcpcStock), 0o644, 0, 0)
		if name == alpine324 {
			ifupdown(h.System, dhcpEth0)
		} else {
			h.AddFile(hostdns.Interfaces, []byte(dhcpEth0), 0o644, 0, 0)
		}
	case alpineCloud, alpineProgram:
		h.facts = facts(detect.DistroAlpine, "3.23", detect.OpenRC)
		file(dhcpcd + "nameserver " + dhcpNS + "\n")
		h.AddFile(hostdns.UDHCPCConf, []byte(udhcpcStock), 0o644, 0, 0)
		h.AddFile("/sbin/dhcpcd", nil, 0o555, 0, 0)
		if name == alpineCloud {
			ifupdown(h.System, dhcpEth0)
		} else {
			ifupdown(h.System, dhcpEth0+"\tdhcp-program udhcpc\n")
		}
	case pve9:
		h.facts = facts(detect.DistroProxmox, "9", detect.Systemd)
		file("search example.net\nnameserver " + dhcpNS + "\n")
	case networkManager:
		h.AddSymlink(hostdns.ResolvConf, "/run/NetworkManager/resolv.conf")
	case resolvconf:
		h.AddSymlink(hostdns.ResolvConf, "../run/resolvconf/resolv.conf")
	case container:
		h.facts.Container = detect.Container{Detected: detect.Yes, Signal: "/proc/1/environ", Manager: "lxc"}
		file("nameserver " + dhcpNS + "\n")
	default:
		t.Fatalf("no host %q", name)
	}
	h.AddFile(hardnYML, hardnYMLData(list), 0o600, 0, 0)
	h.Handler = h.answer
	cfg, err := config.Read(context.Background(), h, hardnYML)
	if err != nil {
		t.Fatal(err)
	}
	return h, cfg
}

// answer is the host's commands: systemctl and resolvectl, as
// systemd-resolved answers them in the model.
func (h *fakeHost) answer(argv []string, _ []byte) hostfake.Response {
	h.mu.Lock()
	defer h.mu.Unlock()
	switch cmd := strings.Join(argv, " "); cmd {
	case "systemctl is-active " + hostdns.ResolvedUnit:
		if h.active == "active" {
			return hostfake.Response{Stdout: "active\n"}
		}
		return hostfake.Response{Stdout: h.active + "\n", ExitCode: 3}
	case "resolvectl --no-pager dns", "resolvectl --no-pager domain":
		if h.active != "active" {
			return hostfake.Response{Stderr: "Failed to get global data: Unit dbus-org.freedesktop.resolve1.service not found.\n", ExitCode: 1}
		}
		if strings.HasSuffix(cmd, "dns") {
			return hostfake.Response{Stdout: "Global:" + spaced(h.global) + "\n" + h.links}
		}
		return hostfake.Response{Stdout: "Global:\n" + h.domains}
	case "systemctl try-reload-or-restart " + hostdns.ResolvedUnit:
		if !h.inert {
			h.load()
		}
		return hostfake.Response{}
	case "systemctl reload-or-restart " + hostdns.ResolvedUnit:
		h.active = "active"
		h.load()
		return hostfake.Response{}
	}
	return hostfake.Response{Err: hostfake.ErrUnscripted}
}

func spaced(l []string) string {
	if len(l) == 0 {
		return ""
	}
	return " " + strings.Join(l, " ")
}

// load is systemd-resolved reading hardn's drop-in, the one file that sets
// DNS= on these hosts: its DNS= lines collect, an empty one resets them.
func (h *fakeHost) load() {
	h.global = nil
	data, ok := h.Entry(hostdns.DropIn)
	if !ok {
		return
	}
	for _, l := range strings.Split(string(data.Data), "\n") {
		if v, ok := strings.CutPrefix(strings.TrimSpace(l), "DNS="); ok {
			if v == "" {
				h.global = nil
			}
			h.global = append(h.global, strings.Fields(v)...)
		}
	}
}

// content is name's content on h, or "<absent>".
func content(h *fakeHost, name string) string {
	e, ok := h.Entry(name)
	if !ok {
		return "<absent>"
	}
	return string(e.Data)
}

// writes are the calls that changed something outside /run/hardn, the
// engine's run directory, from the nth on.
func writes(h *fakeHost, n int) []string {
	var out []string
	for _, c := range h.Calls()[n:] {
		switch c.Op {
		case "write-atomic", "write-in-place", "remove", "removeall", "rename", "mkdir", "chmod", "chown", "append":
			if !strings.HasPrefix(c.Path, "/run/hardn") {
				out = append(out, c.Op+" "+c.Path)
			}
		}
	}
	return out
}

// played is one run of the screen: what it wrote, with the answers echoed,
// what each change did (Options.Ran), how many engines it made, and what
// Run returned.
type played struct {
	out     string
	runs    []output.Run
	engines int
	err     error
	unread  int
}

// play runs the DNS screen on h with cfg, plain at width columns (not a
// terminal), the user typing lines; a dry run with dryRun.
func play(h *fakeHost, cfg *config.Config, width int, dryRun bool, lines ...string) played {
	return playWith(h, cfg, width, dryRun, nil, lines...)
}

// playWith is play with the options set changes, when not nil.
func playWith(h *fakeHost, cfg *config.Config, width int, dryRun bool, set func(*dnsscreen.Options), lines ...string) played {
	var out bytes.Buffer
	env := term.Resolve(term.Options{}, term.Facts{Width: width})
	ty := uitest.NewTypist(&out, lines...)
	var p played
	o := dnsscreen.Options{
		Prompter: nav.Prompter{Ctx: component.NewCtx(env), In: prompt.New(ty, false), Out: env.Writer(&out)},
		System:   h, Facts: h.facts, Config: cfg, DryRun: dryRun, LogPath: output.LegacyLogPath,
		Now: func() time.Time { return now },
		NewEngine: func(progress func(engine.Event)) *engine.Engine {
			p.engines++
			return engine.New(h, engine.Options{
				Revert:   revert.Options{Now: func() time.Time { return now }, Hardn: "/usr/local/bin/hardn"},
				Detect:   func(context.Context, host.System) (detect.Facts, error) { return h.facts, nil },
				Hold:     func() func() { return func() {} },
				PID:      5000,
				Backups:  backup.Settings{Path: backup.DefaultPath, Enabled: true},
				Progress: progress,
			})
		},
		Ran: func(r output.Run) { p.runs = append(p.runs, r) },
	}
	if set != nil {
		set(&o)
	}
	p.err = dnsscreen.Run(context.Background(), o)
	p.out, p.unread = out.String(), ty.Unread()
	return p
}

// screens are the DNS screens out shows, each from its title bar to its
// prompt: one per round.
func screens(out string) []string {
	var got []string
	for _, part := range strings.Split(out, "# DNS Configuration #")[1:] {
		end := strings.Index(part, "> Enter selection")
		if end < 0 {
			end = len(part)
		}
		got = append(got, part[:end])
	}
	return got
}

// row is the value of the screen's row key in s, "" when it has none.
func row(s, key string) string {
	for _, l := range strings.Split(s, "\n") {
		if t := strings.TrimSpace(l); strings.Contains(t, key+"  ") {
			_, v, _ := strings.Cut(t, key)
			return strings.TrimSpace(v)
		}
	}
	return ""
}
