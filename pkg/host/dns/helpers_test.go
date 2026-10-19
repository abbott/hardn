package dns_test

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/backup"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/dns"
	"github.com/abbott/hardn/pkg/host/hostfake"
)

// Synthetic values only (192.0.2.0/24, 2001:db8::/32, example.net).
const (
	ns1, ns2, ns6 = "192.0.2.53", "192.0.2.54", "2001:db8::53"
	dhcpNS        = "192.0.2.1"
	runID         = "20261017T143005Z-1a2b3c4d"
)

// facts are detect's facts for a host from detect's os-release fixture
// (debian-13, ubuntu-26.04, alpine-3.24, ...), booted by systemd or OpenRC,
// with pve-manager installed for "proxmox" (Debian 13's os-release); no
// container.
func facts(t *testing.T, fixture string) detect.Facts {
	t.Helper()
	pve := detect.Proxmox{Installed: detect.No}
	if fixture == "proxmox" {
		fixture, pve = "debian-13", detect.Proxmox{Installed: detect.Yes, Version: "9.0.3"}
	}
	data, err := os.ReadFile(filepath.Join("..", "detect", "testdata", "os-release", fixture))
	if err != nil {
		t.Fatal(err)
	}
	h := hostfake.New(host.Options{})
	h.AddFile(detect.EtcOSRelease, data, 0o644, 0, 0)
	f := detect.Facts{OS: detect.ReadOSRelease(context.Background(), h), Container: detect.Container{Detected: detect.No}}
	f.Platform = detect.Classify(f.OS, pve)
	f.Init = detect.Init{Kind: detect.Systemd, Running: detect.Yes}
	if f.Platform.Distro == detect.DistroAlpine {
		f.Init.Kind = detect.OpenRC
	}
	if f.Platform.Support != detect.Supported {
		t.Fatalf("facts for %s: %+v", fixture, f.Platform)
	}
	return f
}

// world is a modelled host. systemctl and resolvectl act on the fake as
// systemd-resolved would for these tests: a reload re-reads the main
// resolved.conf and the drop-ins (DNS= and Domains= lists collect, an
// empty assignment resets them, names sort in C byte order across the four
// directories, /etc's hiding the same name elsewhere, key and value
// stripped of blanks), and resolvectl prints what was last loaded in its
// own form (resolvectl.c status_print_strv_ifindex). This is the tests'
// model of resolved, not resolved: nothing here was captured from a host.
type world struct {
	h       *hostfake.System
	f       detect.Facts
	active  string // systemctl is-active's word for systemd-resolved
	loaded  []string
	domains []string
	links   []dns.Link
	// reload is how try-reload-or-restart answers: "" reloads, "fail"
	// exits 1, "down" exits 1 and leaves the unit failed, "inert" exits 0
	// without re-reading.
	reload string
	// restart is how reload-or-restart answers: "" (re)starts the unit,
	// which then reads its files, "fail" exits 1, "inert" exits 0 without
	// re-reading.
	restart string
	// rejectDomains: the parser ignores every Domains= line, as resolved
	// ignores one it cannot parse, with a warning.
	rejectDomains bool
	hook          func(cmd string)
}

// newWorld is a stock host for fixture: "ubuntu-26.04" with
// systemd-resolved's stub link and a DHCP link; "debian-12-cloud" with the
// uplink file; "debian-13" with a regular file and dhcpcd; "alpine-3.24"
// with udhcpc's file and its stock udhcpc.conf; "proxmox" with the
// installer's file.
func newWorld(t *testing.T, fixture string) *world {
	t.Helper()
	w := &world{h: hostfake.New(host.Options{}), active: "inactive"}
	w.h.AddDir("/etc", 0o755, 0, 0)
	w.h.AddDir("/var/backups", 0o755, 0, 0)
	switch fixture {
	case "ubuntu-26.04", "debian-12-cloud":
		w.active = "active"
		w.h.AddFile("/etc/systemd/resolved.conf", []byte("[Resolve]\n#DNS=\nDNS=192.0.2.99\nDomains=example.net\n"), 0o644, 0, 0)
		w.h.AddFile("/run/systemd/resolve/stub-resolv.conf", []byte("nameserver 127.0.0.53\noptions edns0 trust-ad\nsearch .\n"), 0o644, 0, 0)
		w.h.AddFile("/run/systemd/resolve/resolv.conf", []byte("nameserver 192.0.2.99\nnameserver "+dhcpNS+"\n"), 0o644, 0, 0)
		w.h.AddSymlink(dns.ResolvConf, "../run/systemd/resolve/stub-resolv.conf")
		if fixture == "debian-12-cloud" {
			fixture = "debian-12"
			w.h.AddSymlink(dns.ResolvConf, "/run/systemd/resolve/resolv.conf") // replaces the stub link
		}
		w.links = []dns.Link{{Index: 2, Name: "eth0", Servers: []string{dhcpNS}}}
		w.load()
	case "debian-13":
		w.h.AddFile(dns.ResolvConf, []byte("# installer\ndomain example.net\nsearch example.net\nnameserver "+dhcpNS+"\noptions rotate\n"), 0o644, 0, 0)
		w.h.AddFile("/usr/sbin/dhcpcd", nil, 0o755, 0, 0)
	case "alpine-3.24":
		w.h.AddFile(dns.ResolvConf, []byte("search example.net\nnameserver "+dhcpNS+"\n"), 0o644, 0, 0)
		w.h.AddFile(dns.UDHCPCConf, []byte("# Do not overwrite /etc/resolv.conf\n#RESOLV_CONF=\"no\"\n\n#NO_DNS=\"eth1 wlan1\"\n"), 0o644, 0, 0)
	case "proxmox":
		w.h.AddFile(dns.ResolvConf, []byte("search example.net\nnameserver "+dhcpNS+"\n"), 0o644, 0, 0)
	}
	w.f = facts(t, fixture)
	w.h.Handler = w.answer
	return w
}

func (w *world) answer(argv []string, _ []byte) hostfake.Response {
	cmd := strings.Join(argv, " ")
	if w.hook != nil {
		w.hook(cmd)
	}
	switch cmd {
	case "systemctl is-active " + dns.ResolvedUnit:
		if w.active == "active" {
			return hostfake.Response{Stdout: "active\n"}
		}
		return hostfake.Response{Stdout: w.active + "\n", ExitCode: 3}
	case "systemctl try-reload-or-restart " + dns.ResolvedUnit:
		switch w.reload {
		case "down":
			w.active = "failed"
			fallthrough
		case "fail":
			return hostfake.Response{Stderr: "Job for systemd-resolved.service failed.\n", ExitCode: 1}
		case "":
			w.load()
		}
		return hostfake.Response{}
	case "systemctl reload-or-restart " + dns.ResolvedUnit:
		switch w.restart {
		case "fail":
			return hostfake.Response{Stderr: "Job for systemd-resolved.service failed.\n", ExitCode: 1}
		case "":
			w.active = "active"
			w.load()
		}
		return hostfake.Response{}
	case "resolvectl --no-pager dns", "resolvectl --no-pager domain":
		if w.active != "active" {
			return hostfake.Response{Stderr: "Failed to get global data: Unit dbus-org.freedesktop.resolve1.service not found.\n", ExitCode: 1}
		}
		global, perLink := w.loaded, func(l dns.Link) []string { return l.Servers }
		if strings.HasSuffix(cmd, "domain") {
			global, perLink = w.domains, func(dns.Link) []string { return nil }
		}
		out := "Global:" + list(global) + "\n"
		for _, l := range w.links {
			out += "Link " + strconv.Itoa(l.Index) + " (" + l.Name + "):" + list(perLink(l)) + "\n"
		}
		return hostfake.Response{Stdout: out}
	}
	return hostfake.Response{Err: hostfake.ErrUnscripted}
}

func list(ss []string) string {
	out := ""
	for _, s := range ss {
		out += " " + s
	}
	return out
}

// load is systemd-resolved reading its configuration.
func (w *world) load() {
	ctx := context.Background()
	var files []string
	if data, err := w.h.ReadFile(ctx, "/etc/systemd/resolved.conf"); err == nil {
		files = append(files, string(data))
	}
	byName := map[string]string{}
	for _, dir := range slices.Backward(dns.DropInDirs()) {
		des, _ := w.h.ReadDir(ctx, dir)
		for _, de := range des {
			if strings.HasSuffix(de.Name(), ".conf") {
				byName[de.Name()] = dir + "/" + de.Name() // /etc, listed first, is read last and wins
			}
		}
	}
	names := make([]string, 0, len(byName))
	for n := range byName {
		names = append(names, n)
	}
	slices.Sort(names)
	for _, n := range names {
		data, _ := w.h.ReadFile(ctx, byName[n])
		files = append(files, string(data))
	}
	w.loaded, w.domains = nil, nil
	for _, f := range files {
		for _, l := range strings.Split(f, "\n") {
			k, v, ok := strings.Cut(strings.TrimSpace(l), "=")
			list := map[string]*[]string{"DNS": &w.loaded, "Domains": &w.domains}[strings.TrimSpace(k)]
			if !ok || list == nil || list == &w.domains && w.rejectDomains {
				continue
			}
			if v = strings.TrimSpace(v); v == "" {
				*list = nil
			}
			*list = append(*list, strings.Fields(v)...)
		}
	}
}

func (w *world) read(t *testing.T) dns.State {
	t.Helper()
	s, err := dns.Read(context.Background(), w.h, w.f)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func (w *world) plan(t *testing.T, c dns.Config) dns.Plan {
	t.Helper()
	p, err := dns.PlanDNS(w.read(t), c)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func backups(t *testing.T, sys host.System) *backup.Run {
	t.Helper()
	r, err := backup.Open(sys, backup.Settings{Path: backup.DefaultPath, Enabled: true}, runID)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func content(t *testing.T, w *world, name string) string {
	t.Helper()
	e, ok := w.h.Entry(name)
	if !ok {
		return "<absent>"
	}
	return string(e.Data)
}

func steps(p dns.Plan) []string {
	var out []string
	for _, c := range p.Changes {
		out = append(out, c.Step.String())
	}
	return out
}

func kinds(p dns.Plan) []string {
	var out []string
	for _, f := range p.Findings {
		out = append(out, f.Kind.String())
	}
	return out
}

// writes are the calls that changed something outside the backup run.
func writes(h *hostfake.System) []string {
	var out []string
	for _, c := range h.Calls() {
		switch c.Op {
		case "write-atomic", "write-in-place", "remove", "removeall", "rename", "mkdir", "chmod", "chown":
			if !strings.HasPrefix(c.Path, backup.DefaultPath) {
				out = append(out, c.Op+" "+c.Path)
			}
		}
	}
	return out
}

// faulty is a fake on which the listed operations fail: fail["write-atomic
// /etc/resolv.conf"] is the error WriteFileAtomic of that path returns.
type faulty struct {
	*hostfake.System
	fail map[string]error
}

func (f faulty) err(op, name string) error {
	if err := f.fail[op+" "+name]; err != nil {
		return &fs.PathError{Op: op, Path: name, Err: err}
	}
	return nil
}

func (f faulty) WriteFileAtomic(ctx context.Context, name string, data []byte, perm fs.FileMode, uid, gid int) error {
	if err := f.err("write-atomic", name); err != nil {
		return err
	}
	return f.System.WriteFileAtomic(ctx, name, data, perm, uid, gid)
}

func (f faulty) MkdirAll(ctx context.Context, name string, perm fs.FileMode) error {
	if err := f.err("mkdir", name); err != nil {
		return err
	}
	return f.System.MkdirAll(ctx, name, perm)
}

func (f faulty) Stat(ctx context.Context, name string) (fs.FileInfo, error) {
	if err := f.err("stat", name); err != nil {
		return nil, err
	}
	return f.System.Stat(ctx, name)
}

func (f faulty) ReadFileNoFollow(ctx context.Context, name string) ([]byte, fs.FileInfo, error) {
	if err := f.err("read-nofollow", name); err != nil {
		return nil, nil, err
	}
	return f.System.ReadFileNoFollow(ctx, name)
}

func (f faulty) ReadDir(ctx context.Context, name string) ([]fs.DirEntry, error) {
	if err := f.err("readdir", name); err != nil {
		return nil, err
	}
	return f.System.ReadDir(ctx, name)
}

var errDisk = errors.New("no space left on device")
