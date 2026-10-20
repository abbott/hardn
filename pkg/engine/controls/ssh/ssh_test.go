package ssh_test

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/abbott/hardn/pkg/engine"
	"github.com/abbott/hardn/pkg/engine/controls/ssh"
	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/hostfake"
	"github.com/abbott/hardn/pkg/host/sshd"
	"github.com/abbott/hardn/pkg/output"
)

// Nothing here runs a real command or reads the machine: every host is
// hostfake's, its sshd -T answered by a model of sshd's reading of the
// fake's files (sshdT). Synthetic values only: alice, 192.0.2.0/24, 2230.

// platform is a supported release's stock SSH host.
type platform struct {
	name   string // testdata/<name>/sshd_config
	facts  detect.Facts
	bin    string // the sshd the checks run
	old    bool   // OpenSSH before 10.1: sshd -T prints without-password
	socket bool   // ssh.socket listens, on a dual-stack [::]
	noSS   bool   // ss is not installed
}

func debian13() platform {
	return platform{name: "debian13", bin: "sshd", old: true, facts: detect.Facts{
		OS:       detect.OSRelease{ID: "debian", VersionID: "13", VersionCodename: "trixie"},
		Platform: detect.Platform{Distro: detect.DistroDebian, Version: "13", Support: detect.Supported},
		Init:     detect.Init{Kind: detect.Systemd, Running: detect.Yes, Source: detect.SystemdRunDir},
		SSH: detect.SSH{Mode: detect.SSHService, Generator: detect.No,
			Socket:  detect.UnitState{Enabled: "disabled", Active: "inactive"},
			Service: detect.UnitState{Enabled: "enabled", Active: "active"}},
		Proxmox:   detect.Proxmox{Installed: detect.No, ClusterFS: detect.No, EtcPVE: detect.No},
		Container: detect.Container{Detected: detect.No},
	}}
}

func ubuntu2604() platform {
	p := debian13()
	p.name, p.old, p.socket = "ubuntu2604", false, true
	p.facts.OS = detect.OSRelease{ID: "ubuntu", VersionID: "26.04", VersionCodename: "resolute"}
	p.facts.Platform = detect.Platform{Distro: detect.DistroUbuntu, Version: "26.04", Support: detect.Supported}
	p.facts.SSH = detect.SSH{Mode: detect.SSHSocket, Generator: detect.Yes, GeneratorPath: detect.SocketGenerator,
		Socket:  detect.UnitState{Enabled: "enabled", Active: "active"},
		Service: detect.UnitState{Enabled: "disabled", Active: "active"}}
	return p
}

func alpine324() platform {
	p := debian13()
	p.name, p.bin, p.old, p.noSS = "alpine324", "/usr/sbin/sshd", false, true
	p.facts.OS = detect.OSRelease{ID: "alpine", VersionID: "3.24.0"}
	p.facts.Platform = detect.Platform{Distro: detect.DistroAlpine, Version: "3.24", Support: detect.Supported}
	p.facts.Init = detect.Init{Kind: detect.OpenRC, Running: detect.Yes}
	p.facts.SSH = detect.SSH{Mode: detect.SSHOpenRC, OpenRC: detect.OpenRCService{Started: detect.Yes, InRunlevel: detect.Yes}}
	return p
}

func pve9() platform {
	p := debian13()
	p.name = "pve9"
	p.facts.Platform = detect.Platform{Distro: detect.DistroProxmox, Version: "9", Support: detect.Supported}
	p.facts.Proxmox = detect.Proxmox{Installed: detect.Yes, Version: "9.0.10", ClusterFS: detect.Yes, EtcPVE: detect.Yes}
	return p
}

// site is one test's host: a platform's stock sshd_config and the drop-ins
// a case adds, with what the commands answer.
type site struct {
	plat   platform
	main   string            // sshd_config; "" is the platform's stock file
	drops  map[string]string // name: content, in sshd_config.d
	noMain bool              // no sshd_config at all
	socket []string          // ssh.socket's Listen entries; nil is the model's
	bound  []string          // what ss shows; nil is what is configured
	edit   func(string) string
	failT  *hostfake.Response // sshd -T fails with this
	ssErr  *hostfake.Response // ss fails with this
	facts  func(*detect.Facts)
}

// hardened are the drop-in lines that make every SSH row Configured off
// Proxmox VE.
const hardened = "Port 2230\nPermitRootLogin no\nPasswordAuthentication no\nKbdInteractiveAuthentication no\nPubkeyAuthentication yes\n"

// pveHardened is hardened as Proxmox VE's rules leave it (pve.PlanSSH).
const pveHardened = "PermitRootLogin prohibit-password\nPasswordAuthentication no\nKbdInteractiveAuthentication no\nPubkeyAuthentication yes\n"

func (s site) build(t *testing.T) (*hostfake.System, detect.Facts) {
	t.Helper()
	f := hostfake.New(host.Options{})
	main := s.main
	if main == "" {
		data, err := os.ReadFile(filepath.Join("testdata", s.plat.name, "sshd_config"))
		if err != nil {
			t.Fatal(err)
		}
		main = string(data)
	}
	f.AddDir("/etc/ssh", 0o755, 0, 0)
	if !s.noMain {
		f.AddFile(sshd.MainConfig, []byte(main), 0o644, 0, 0)
	}
	f.AddDir(sshd.DropInDir, 0o755, 0, 0)
	var drops []sshd.File
	for name, data := range s.drops {
		f.AddFile(sshd.DropInDir+"/"+name, []byte(data), 0o644, 0, 0)
		drops = append(drops, sshd.File{Path: sshd.DropInDir + "/" + name, Data: []byte(data)})
	}
	f.AddUser(host.User{Name: "root", UID: 0, GID: 0, HomeDir: "/root"})
	f.AddFile("/etc/group", []byte("root:x:0:\nsudo:x:27:alice\n"), 0o644, 0, 0)
	out, ls := sshdT(s.plat.old, []byte(main), drops)
	if s.edit != nil {
		out = s.edit(out)
	}
	socket, bound := s.socket, s.bound
	if socket == nil {
		socket = ls
		if s.plat.socket {
			socket = dualStack(ls)
		}
	}
	if bound == nil {
		bound = ls
		if s.plat.socket {
			bound = socket
		}
	}
	f.Handler = func(argv []string, _ []byte) hostfake.Response {
		cmd := strings.Join(argv, " ")
		switch {
		case argv[0] == s.plat.bin && slices.Contains(argv, "-T") && s.failT != nil:
			return *s.failT
		case argv[0] == s.plat.bin && slices.Contains(argv, "-T"): // and each Match block's probe
			return hostfake.Response{Stdout: out}
		case cmd == "systemctl show -p Listen ssh.socket":
			var b strings.Builder
			for _, l := range socket {
				fmt.Fprintf(&b, "Listen=%s (Stream)\n", l)
			}
			return hostfake.Response{Stdout: b.String()}
		case cmd == "ss -Htln" && s.plat.noSS:
			return hostfake.Response{Err: fmt.Errorf("host: run ss: %w", host.ErrNotFound)}
		case cmd == "ss -Htln" && s.ssErr != nil:
			return *s.ssErr
		case cmd == "ss -Htln":
			return hostfake.Response{Stdout: ss(bound, s.plat.socket)}
		}
		t.Errorf("unexpected command %q", cmd)
		return hostfake.Response{Err: hostfake.ErrUnscripted}
	}
	facts := s.plat.facts
	if s.facts != nil {
		s.facts(&facts)
	}
	return f, facts
}

// defaults are the model's values for what sshd -T always prints, shaped
// like OpenSSH's (sshd_config(5)); not read from a binary.
var defaults = []struct{ k, v string }{
	{"permitrootlogin", "prohibit-password"}, {"pubkeyauthentication", "yes"}, {"passwordauthentication", "yes"},
	{"kbdinteractiveauthentication", "yes"}, {"usepam", "no"}, {"addressfamily", "any"},
	{"authenticationmethods", "any"}, {"authorizedkeysfile", ".ssh/authorized_keys .ssh/authorized_keys2"},
	{"authorizedkeyscommand", "none"}, {"pubkeyacceptedalgorithms", "ssh-ed25519,ecdsa-sha2-nistp256,rsa-sha2-512,rsa-sha2-256"},
}

// sshdT is sshd -T's output by a model of sshd's reading (sshd.ReadOrder):
// the first value of each keyword outside Match blocks, every value of an
// accumulating one, defaults for the rest; and the listeners: a
// ListenAddress with a port as it is, one without on every Port, with none
// every Port on [::] and 0.0.0.0 (sshd_config(5) ListenAddress).
func sshdT(old bool, main []byte, drops []sshd.File) (out string, listeners []string) {
	first, acc := map[string]string{}, map[string][]string{}
	for _, l := range sshd.ReadOrder(main, drops) {
		switch {
		case l.Match != nil:
		case sshd.Accumulates(l.Keyword):
			acc[l.Keyword] = append(acc[l.Keyword], l.Args...)
		default:
			if _, ok := first[l.Keyword]; !ok {
				first[l.Keyword] = strings.Join(l.Args, " ")
			}
		}
	}
	var b strings.Builder
	ports := acc["port"]
	if len(ports) == 0 {
		ports = []string{"22"}
	}
	slices.Sort(ports)
	ports = slices.Compact(ports)
	for _, p := range ports {
		b.WriteString("port " + p + "\n")
	}
	on := func(a netip.Addr, p string) {
		n, _ := strconv.Atoi(p)
		listeners = append(listeners, netip.AddrPortFrom(a, uint16(n)).String())
	}
	if las := acc["listenaddress"]; len(las) > 0 {
		for _, la := range las {
			if ap, err := netip.ParseAddrPort(la); err == nil {
				listeners = append(listeners, ap.String())
				continue
			}
			for _, p := range ports {
				on(netip.MustParseAddr(strings.Trim(la, "[]")), p)
			}
		}
	} else {
		for _, p := range ports {
			on(netip.IPv6Unspecified(), p)
			on(netip.IPv4Unspecified(), p)
		}
	}
	for _, l := range listeners {
		b.WriteString("listenaddress " + l + "\n")
	}
	for _, d := range defaults {
		v, ok := first[d.k]
		if !ok {
			v = d.v
		}
		if d.k != "authorizedkeysfile" {
			v = strings.ToLower(v)
		}
		if d.k == "permitrootlogin" && old && v == "prohibit-password" {
			v = "without-password"
		}
		b.WriteString(d.k + " " + v + "\n")
	}
	for _, k := range []string{"allowusers", "allowgroups", "denyusers", "denygroups", "acceptenv"} {
		for _, v := range acc[k] {
			b.WriteString(k + " " + v + "\n")
		}
	}
	return b.String(), listeners
}

// dualStack is systemd's socket for ls: one dual-stack [::] per port.
func dualStack(ls []string) []string {
	var out []string
	for _, l := range ls {
		ap := netip.MustParseAddrPort(l)
		if s := netip.AddrPortFrom(netip.IPv6Unspecified(), ap.Port()).String(); !slices.Contains(out, s) {
			out = append(out, s)
		}
	}
	return out
}

// ss is ss -Htln's output for the sockets given; a socket host's [::] is
// dual-stack, which ss prints as "*".
func ss(socks []string, dual bool) string {
	var b strings.Builder
	for _, s := range socks {
		peer := "0.0.0.0:*"
		if strings.HasPrefix(s, "[") {
			peer = "[::]:*"
			if dual && strings.HasPrefix(s, "[::]:") {
				s, peer = "*:"+strings.TrimPrefix(s, "[::]:"), "*:*"
			}
		}
		fmt.Fprintf(&b, "LISTEN 0      4096   %20s   %s\n", s, peer)
	}
	return b.String()
}

// replace is an edit of sshd -T's output: the line of keyword k says v.
func replace(k, v string) func(string) string {
	return func(out string) string {
		var ls []string
		for _, l := range strings.Split(out, "\n") {
			if kw, _, _ := strings.Cut(l, " "); kw == k {
				l = k + " " + v
			}
			ls = append(ls, l)
		}
		return strings.Join(ls, "\n")
	}
}

// row is one status a control shows on a host.
type row struct {
	name  string
	ctl   engine.Control
	site  site
	state engine.State
	want  string   // the detail
	codes []string // the findings' codes, in order
	err   string   // the error's code (pkg/output), "" for none
}

func run(t *testing.T, r row) engine.Status {
	t.Helper()
	f, facts := r.site.build(t)
	st, err := r.ctl.Check(context.Background(), f, facts)
	if code := codeOf(err); code != r.err {
		t.Errorf("error %v: code %q, want %q", err, code, r.err)
	}
	if err != nil {
		st.State = engine.Unknown // as Engine.Check reports it
	}
	if st.State != r.state || st.Detail != r.want {
		t.Errorf("%s %q, want %s %q (evidence %q, findings %v)", st.State, st.Detail, r.state, r.want, st.Evidence, st.Findings)
	}
	var codes []string
	for _, fd := range st.Findings {
		codes = append(codes, fd.Code)
		if e, ok := output.Lookup(fd.Code); !ok || e.Kind != output.KindFinding {
			t.Errorf("finding %s: not a registered finding code", fd.Code)
		}
	}
	if !slices.Equal(codes, r.codes) {
		t.Errorf("findings %v, want codes %v", st.Findings, r.codes)
	}
	readOnly(t, f, r.site.plat.bin)
	return st
}

func codeOf(err error) string {
	if err == nil {
		return ""
	}
	return output.CodeOf(err)
}

// readOnly fails for any call that changes the fake, and any command but
// the reads the checks make.
func readOnly(t *testing.T, f *hostfake.System, bin string) {
	t.Helper()
	for _, c := range f.Calls() {
		switch c.Op {
		case "read", "read-nofollow", "stat", "lstat", "readdir", "readlink", "resolve-symlink", "lookup-user", "lookup-group":
		case "run":
			cmd := strings.Join(c.Argv, " ")
			if !strings.HasPrefix(cmd, bin+" -T -f "+sshd.MainConfig) && cmd != "systemctl show -p Listen ssh.socket" && cmd != "ss -Htln" {
				t.Errorf("ran %q", cmd)
			}
		default:
			t.Errorf("a call that is not a read: %v", c)
		}
	}
}

func drops(kv ...string) map[string]string {
	m := map[string]string{}
	for i := 0; i+1 < len(kv); i += 2 {
		m[kv[i]] = kv[i+1]
	}
	return m
}

var (
	login = ssh.Login{}
	auth  = ssh.Auth{}
	port  = ssh.Port{}
)

const (
	shadowed   = "SSHD_SHADOWED"
	overridden = "SSHD_OVERRIDDEN"
	unknownF   = "SSHD_EFFECTIVE_UNKNOWN"
	disagreeF  = "SSHD_SOURCES_DISAGREE"
)

// TestStock: each platform as installed, and hardened as slice 6 would
// leave it, through Engine.Check over the three controls in Figma's order.
func TestStock(t *testing.T) {
	type line struct {
		state  engine.State
		detail string
	}
	for _, c := range []struct {
		name  string
		site  site
		want  [3]line
		codes [3][]string // findings, by row
	}{
		{"debian13", site{plat: debian13()}, [3]line{{engine.Partial, "root key-only"}, {engine.NotConfigured, "password auth on"}, {engine.NotConfigured, "default (22)"}}, [3][]string{}},
		{"ubuntu2604", site{plat: ubuntu2604()}, [3]line{{engine.Partial, "root key-only"}, {engine.NotConfigured, "password auth on"}, {engine.NotConfigured, "default (22)"}}, [3][]string{}},
		{"alpine324", site{plat: alpine324()}, [3]line{{engine.Partial, "root key-only"}, {engine.NotConfigured, "password auth on"}, {engine.NotConfigured, "default (22)"}}, [3][]string{}},
		{"pve9", site{plat: pve9()}, [3]line{{engine.NotConfigured, "root allowed"}, {engine.NotConfigured, "password auth on"}, {engine.Configured, "22, as Proxmox needs"}},
			[3][]string{{"PVE_ROOT_NOT_KEY_ONLY"}}},
		{"debian13 hardened", site{plat: debian13(), drops: drops("00-hardn.conf", hardened)},
			[3]line{{engine.Configured, "root disallowed"}, {engine.Configured, "key-only auth"}, {engine.Configured, "non-default (2230)"}}, [3][]string{}},
		{"ubuntu2604 hardened", site{plat: ubuntu2604(), drops: drops("00-hardn.conf", hardened)},
			[3]line{{engine.Configured, "root disallowed"}, {engine.Configured, "key-only auth"}, {engine.Configured, "non-default (2230)"}}, [3][]string{}},
		{"alpine324 hardened", site{plat: alpine324(), drops: drops("00-hardn.conf", hardened)},
			[3]line{{engine.Configured, "root disallowed"}, {engine.Configured, "key-only auth"}, {engine.Configured, "non-default (2230)"}}, [3][]string{}},
		{"pve9 hardened", site{plat: pve9(), drops: drops("00-hardn.conf", pveHardened)},
			[3]line{{engine.Configured, "root key-only (Proxmox)"}, {engine.Configured, "key-only auth"}, {engine.Configured, "22, as Proxmox needs"}},
			[3][]string{{"PVE_PROXMOX_ROOT_LOGIN"}}},
	} {
		t.Run(c.name, func(t *testing.T) {
			f, facts := c.site.build(t)
			e := engine.New(f, engine.Options{Detect: func(context.Context, host.System) (detect.Facts, error) { return facts, nil }})
			reps, err := e.Check(context.Background(), ssh.Controls())
			if err != nil {
				t.Fatal(err)
			}
			if len(reps) != 3 {
				t.Fatalf("%d reports", len(reps))
			}
			for i, rep := range reps {
				id, title := []string{"ssh.login", "ssh.auth", "ssh.port"}[i], []string{"SSH Login", "SSH Auth", "SSH Port"}[i]
				if rep.Control != id || rep.Title != title || rep.Err != nil {
					t.Errorf("report %d: %s %q, error %v", i, rep.Control, rep.Title, rep.Err)
				}
				if got := (line{rep.Status.State, rep.Status.Detail}); got != c.want[i] {
					t.Errorf("%s: %s %q, want %s %q", id, got.state, got.detail, c.want[i].state, c.want[i].detail)
				}
				if len(rep.Status.Evidence) == 0 {
					t.Errorf("%s: no evidence", id)
				}
				var codes []string
				for _, fd := range rep.Status.Findings {
					codes = append(codes, fd.Code)
				}
				if !slices.Equal(codes, c.codes[i]) {
					t.Errorf("%s: findings %v, want codes %v", id, rep.Status.Findings, c.codes[i])
				}
			}
			readOnly(t, f, c.site.plat.bin)
		})
	}
}

// TestEvidence: what each row was read from, as the commands printed it.
func TestEvidence(t *testing.T) {
	st := run(t, row{ctl: login, site: site{plat: debian13()}, state: engine.Partial, want: "root key-only"})
	if want := []string{"sshd -T: permitrootlogin without-password"}; !slices.Equal(st.Evidence, want) {
		t.Errorf("login evidence %q, want %q", st.Evidence, want)
	}
	st = run(t, row{ctl: port, site: site{plat: ubuntu2604(), drops: drops("00-hardn.conf", hardened)}, state: engine.Configured, want: "non-default (2230)"})
	want := []string{"sshd -T: listenaddress [::]:2230", "sshd -T: listenaddress 0.0.0.0:2230",
		"systemctl show -p Listen ssh.socket: [::]:2230", "ss -Htln: *:2230"}
	if !slices.Equal(st.Evidence, want) {
		t.Errorf("port evidence %q, want %q", st.Evidence, want)
	}
	st = run(t, row{ctl: port, site: site{plat: alpine324()}, state: engine.NotConfigured, want: "default (22)"})
	if !slices.Contains(st.Evidence, "ss: not installed, so the bound sockets are not read") {
		t.Errorf("port evidence %q: no word of ss", st.Evidence)
	}
}

func TestLogin(t *testing.T) {
	match := "Match Address 192.0.2.0/24\n\tPermitRootLogin yes\n"
	for _, r := range []row{
		{name: "no", site: site{plat: debian13(), drops: drops("10-site.conf", "PermitRootLogin no\n")}, state: engine.Configured, want: "root disallowed"},
		{name: "prohibit-password", site: site{plat: ubuntu2604(), drops: drops("10-site.conf", "PermitRootLogin prohibit-password\n")}, state: engine.Partial, want: "root key-only"},
		{name: "forced-commands-only", site: site{plat: debian13(), drops: drops("10-site.conf", "PermitRootLogin forced-commands-only\n")}, state: engine.Partial, want: "root commands only"},
		{name: "yes", site: site{plat: alpine324(), drops: drops("10-site.conf", "PermitRootLogin yes\n")}, state: engine.NotConfigured, want: "root allowed"},
		{name: "no, and a Match block allows root", site: site{plat: debian13(), drops: drops("10-site.conf", "PermitRootLogin no\n"+match)},
			state: engine.Partial, want: "Match allows root", codes: []string{ssh.CodeMatchAllowsMore}},
		{name: "no, and a Match block keeps it out", site: site{plat: debian13(), drops: drops("10-site.conf", "PermitRootLogin no\nMatch User alice\n\tPermitRootLogin no\n")},
			state: engine.Configured, want: "root disallowed"},
		{name: "hardn's no read after another file's yes", site: site{plat: debian13(), drops: drops("00-hardn.conf", "PermitRootLogin no\n", "00-early.conf", "PermitRootLogin yes\n")},
			state: engine.NotConfigured, want: "root allowed", codes: []string{overridden}},
		{name: "a value sshd -T prints that hardn does not know", site: site{plat: debian13(), edit: replace("permitrootlogin", "maybe")},
			state: engine.Unknown, want: "can't read", codes: []string{unknownF}},
		{name: "the files and sshd -T disagree", site: site{plat: debian13(), drops: drops("00-hardn.conf", "PermitRootLogin no\n"), edit: replace("permitrootlogin", "yes")},
			state: engine.Unknown, want: "can't read", codes: []string{disagreeF}},
		{name: "another keyword's finding is not this row's", site: site{plat: debian13(),
			edit: func(out string) string {
				return replace("passwordauthentication", "maybe")(replace("permitrootlogin", "maybe")(out))
			}},
			state: engine.Unknown, want: "can't read", codes: []string{unknownF}},
		// Proxmox VE: prohibit-password with root reachable by key.
		{name: "pve: no", site: site{plat: pve9(), drops: drops("00-hardn.conf", "PermitRootLogin no\n")},
			state: engine.Partial, want: "root disallowed", codes: []string{"PVE_ROOT_NOT_KEY_ONLY", "PVE_ROOT_UNREACHABLE", "PVE_PROXMOX_ROOT_LOGIN"}},
		{name: "pve: root denied", site: site{plat: pve9(), drops: drops("00-hardn.conf", pveHardened+"DenyUsers root\n")},
			state: engine.Partial, want: "root blocked for keys", codes: []string{"PVE_ROOT_UNREACHABLE", "PVE_PROXMOX_ROOT_LOGIN"}},
		{name: "pve: a Match block keeps root out", site: site{plat: pve9(), drops: drops("00-hardn.conf", pveHardened+"Match User alice\n\tPermitRootLogin no\n")},
			state: engine.Partial, want: "Match blocks root", codes: []string{"PVE_ROOT_NOT_KEY_ONLY", "PVE_ROOT_UNREACHABLE", "PVE_PROXMOX_ROOT_LOGIN"}},
		{name: "pve: a Match block allows root", site: site{plat: pve9(), drops: drops("00-hardn.conf", pveHardened+match)},
			state: engine.Partial, want: "Match allows root", codes: []string{ssh.CodeMatchAllowsMore, "PVE_ROOT_NOT_KEY_ONLY", "PVE_PROXMOX_ROOT_LOGIN"}},
		{name: "pve: root by key not established", site: site{plat: pve9(), drops: drops("00-hardn.conf", pveHardened+"ListenAddress 192.0.2.10\n")},
			state: engine.Unknown, want: "root key login unclear", codes: []string{"PVE_ROOT_UNREACHABLE", "PVE_PROXMOX_ROOT_LOGIN"}},
		{name: "pve: a Match block for root asks for more than a key", site: site{plat: pve9(), drops: drops("00-hardn.conf", pveHardened+"Match User root\n\tAuthenticationMethods publickey,password\n")},
			state: engine.Unknown, want: "root key login unclear", codes: []string{"PVE_ROOT_UNREACHABLE", "PVE_PROXMOX_ROOT_LOGIN"}},
		{name: "proxmox not established", site: site{plat: debian13(), facts: func(f *detect.Facts) { f.Proxmox = detect.Proxmox{Err: detect.ErrNoSignal} }},
			state: engine.Unknown, want: "can't tell if Proxmox", err: "PVE_UNKNOWN"},
	} {
		r.ctl = login
		t.Run(r.name, func(t *testing.T) { run(t, r) })
	}
}

func TestAuth(t *testing.T) {
	for _, r := range []row{
		{name: "key-only", site: site{plat: alpine324(), drops: drops("10-site.conf", "PasswordAuthentication no\nKbdInteractiveAuthentication no\n")}, state: engine.Configured, want: "key-only auth"},
		{name: "password on", site: site{plat: debian13()}, state: engine.NotConfigured, want: "password auth on"},
		{name: "keyboard-interactive on", site: site{plat: debian13(), drops: drops("10-site.conf", "PasswordAuthentication no\nKbdInteractiveAuthentication yes\n")},
			state: engine.NotConfigured, want: "kbd-interactive on"},
		{name: "keys off too", site: site{plat: debian13(), drops: drops("10-site.conf", "PasswordAuthentication no\nPubkeyAuthentication no\n")},
			state: engine.Partial, want: "key auth off"},
		{name: "a Match block allows passwords", site: site{plat: ubuntu2604(), drops: drops("10-site.conf", "PasswordAuthentication no\nMatch Address 192.0.2.0/24\n\tPasswordAuthentication yes\n")},
			state: engine.Partial, want: "Match allows passwords", codes: []string{ssh.CodeMatchAllowsMore}},
		{name: "cloud-init's yes shadowed by hardn's no", site: site{plat: ubuntu2604(), drops: drops("00-hardn.conf", "PasswordAuthentication no\n", "50-cloud-init.conf", "PasswordAuthentication yes\n")},
			state: engine.Configured, want: "key-only auth", codes: []string{shadowed}},
		{name: "an alias in a Match block", site: site{plat: alpine324(), drops: drops("10-site.conf",
			"PasswordAuthentication no\nKbdInteractiveAuthentication no\nMatch User alice\n\tChallengeResponseAuthentication yes\n")},
			state: engine.Partial, want: "Match allows passwords", codes: []string{ssh.CodeMatchAllowsMore}},
		{name: "a value printed twice", site: site{plat: debian13(), drops: drops("10-site.conf", "PasswordAuthentication no\n"),
			edit: func(out string) string { return out + "passwordauthentication yes\n" }},
			state: engine.Unknown, want: "can't read", codes: []string{unknownF}},
		{name: "the files and sshd -T disagree, password off", site: site{plat: debian13(), drops: drops("00-hardn.conf", "PasswordAuthentication yes\n"), edit: replace("passwordauthentication", "no")},
			state: engine.Unknown, want: "can't read", codes: []string{disagreeF}},
		{name: "password on, keyboard-interactive not read", site: site{plat: debian13(), edit: replace("kbdinteractiveauthentication", "maybe")},
			state: engine.NotConfigured, want: "password auth on"},
	} {
		r.ctl = auth
		t.Run(r.name, func(t *testing.T) { run(t, r) })
	}
}

func TestPort(t *testing.T) {
	notActive := func(f *detect.Facts) { f.SSH.Socket.Active = "failed" }
	for _, r := range []row{
		{name: "2230", site: site{plat: debian13(), drops: drops("10-site.conf", "Port 2230\n")}, state: engine.Configured, want: "non-default (2230)"},
		{name: "a ListenAddress with its own port", site: site{plat: debian13(), drops: drops("10-site.conf", "ListenAddress 0.0.0.0:2230\n")},
			state: engine.Configured, want: "non-default (2230)"},
		{name: "22 kept beside 2230, as in a confirm window", site: site{plat: ubuntu2604(), drops: drops("00-hardn.conf", "Port 22\nPort 2230\n")},
			state: engine.Partial, want: "22 and 2230"},
		{name: "22", site: site{plat: debian13()}, state: engine.NotConfigured, want: "default (22)"},
		{name: "another file adds 22", site: site{plat: debian13(), main: "Include /etc/ssh/sshd_config.d/*.conf\nPort 22\n", drops: drops("00-hardn.conf", "Port 2230\n")},
			state: engine.Partial, want: "22 and 2230", codes: []string{"SSHD_ACCUMULATES"}},
		{name: "configured, not bound: not reloaded", site: site{plat: debian13(), drops: drops("10-site.conf", "Port 2230\n"), bound: []string{"[::]:22", "0.0.0.0:22"}},
			state: engine.Unknown, want: "not listening on 2230", codes: []string{ssh.CodeNotInEffect}},
		{name: "socket not restarted after daemon-reload", site: site{plat: ubuntu2604(), drops: drops("10-site.conf", "Port 2230\n"), bound: []string{"[::]:22"}},
			state: engine.Unknown, want: "not listening on 2230", codes: []string{ssh.CodeNotInEffect}},
		{name: "socket and sshd differ: daemon-reload not run", site: site{plat: ubuntu2604(), drops: drops("10-site.conf", "Port 2230\n"), socket: []string{"[::]:22"}},
			state: engine.Unknown, want: "socket and sshd differ", codes: []string{disagreeF}},
		{name: "the files and sshd -T differ on the port", site: site{plat: debian13(), drops: drops("00-hardn.conf", "Port 2230\n"),
			edit: func(out string) string { return strings.ReplaceAll(out, "2230", "22") }},
			state: engine.Unknown, want: "files and sshd differ", codes: []string{disagreeF}},
		{name: "ssh.socket failed", site: site{plat: ubuntu2604(), facts: notActive}, state: engine.Unknown, want: "ssh.socket not active", codes: []string{unknownF}},
		{name: "OpenRC has not started sshd", site: site{plat: alpine324(), facts: func(f *detect.Facts) { f.SSH.OpenRC.Started = detect.No }},
			state: engine.Unknown, want: "sshd not running", codes: []string{unknownF}},
		{name: "how sshd starts not established", site: site{plat: debian13(), facts: func(f *detect.Facts) {
			f.SSH = detect.SSH{Socket: detect.UnitState{Enabled: "enabled", Active: "inactive"}, Service: detect.UnitState{Enabled: "enabled", Active: "active"}, Err: detect.ErrConflict}
		}}, state: engine.Unknown, want: "can't tell what listens", codes: []string{unknownF}},
		{name: "ss without ss", site: site{plat: alpine324(), drops: drops("10-site.conf", "Port 2230\n")}, state: engine.Configured, want: "non-default (2230)"},
		{name: "ss fails", site: site{plat: debian13(), ssErr: &hostfake.Response{Stderr: "ss: bad\n", ExitCode: 1}}, state: engine.Unknown, want: "can't read", err: "COMMAND_FAILED"},
		// Proxmox VE: 22 alone.
		{name: "pve: 2230", site: site{plat: pve9(), drops: drops("00-hardn.conf", "Port 2230\n")},
			state: engine.NotConfigured, want: "Proxmox needs 22 only", codes: []string{"PVE_PORT_NOT_22"}},
		{name: "pve: 22 and 2230", site: site{plat: pve9(), drops: drops("00-hardn.conf", "Port 22\nPort 2230\n")},
			state: engine.NotConfigured, want: "Proxmox needs 22 only", codes: []string{"PVE_PORT_NOT_22"}},
		{name: "proxmox not established", site: site{plat: debian13(), facts: func(f *detect.Facts) { f.Proxmox = detect.Proxmox{Err: detect.ErrNoSignal} }},
			state: engine.Unknown, want: "can't tell if Proxmox", err: "PVE_UNKNOWN"},
	} {
		r.ctl = port
		t.Run(r.name, func(t *testing.T) { run(t, r) })
	}
}

// TestUnread: a reading that fails is the check's error, its code
// pkg/output's, and Unknown with what the error shows of the host.
func TestUnread(t *testing.T) {
	noRunDir := &hostfake.Response{Stderr: "Missing privilege separation directory: /run/sshd\r\n", ExitCode: 255}
	invalid := &hostfake.Response{Stderr: "/etc/ssh/sshd_config.d/10-site.conf line 1: Bad configuration option: Bogus\r\n" +
		"/etc/ssh/sshd_config.d/10-site.conf: terminating, 1 bad configuration options\r\n", ExitCode: 255}
	missing := &hostfake.Response{Err: fmt.Errorf("host: run sshd: %w", host.ErrNotFound)}
	for _, ctl := range ssh.Controls() {
		for _, r := range []row{
			{name: "ubuntu before its first connection", site: site{plat: ubuntu2604(), failT: noRunDir}, state: engine.Unknown, want: "sshd not started yet", err: "SSHD_ENV_NO_RUNDIR"},
			{name: "debian with ssh.service stopped", site: site{plat: debian13(), failT: noRunDir}, state: engine.Unknown, want: "sshd not running", err: "SSHD_ENV_NO_RUNDIR"},
			{name: "alpine without its privilege separation directory", site: site{plat: alpine324(), failT: noRunDir}, state: engine.Unknown, want: "can't read", err: "SSHD_ENV_NO_RUNDIR"},
			{name: "a configuration sshd refuses", site: site{plat: debian13(), failT: invalid}, state: engine.Unknown, want: "sshd config invalid", err: "SSHD_CONFIG_INVALID"},
			{name: "no sshd", site: site{plat: debian13(), failT: missing}, state: engine.Unknown, want: "sshd not installed", err: "COMMAND_NOT_FOUND"},
			{name: "no sshd_config", site: site{plat: debian13(), noMain: true}, state: engine.Unknown, want: "no sshd_config", err: "FILE_NOT_FOUND"},
		} {
			r.ctl = ctl
			t.Run(ctl.ID()+"/"+r.name, func(t *testing.T) { run(t, r) })
		}
	}
}

// TestCheckOnly: Plan is ErrNotMigrated, and a plan with the SSH controls
// is refused with it (CONTROL_NOT_MIGRATED), changing nothing.
func TestCheckOnly(t *testing.T) {
	f, facts := site{plat: debian13()}.build(t)
	for _, c := range ssh.Controls() {
		cs, err := c.Plan(context.Background(), f, engine.Input{Facts: facts})
		if !errors.Is(err, engine.ErrNotMigrated) || cs != nil {
			t.Errorf("%s: Plan %v, %v", c.ID(), cs, err)
		}
	}
	f.AddDir("/run", 0o755, 0, 0)
	e := engine.New(f, engine.Options{Detect: func(context.Context, host.System) (detect.Facts, error) { return facts, nil }})
	p, err := e.Plan(context.Background(), ssh.Controls(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if !p.Empty() || !errors.Is(p.Err(), engine.ErrRefused) || !errors.Is(p.Err(), engine.ErrNotMigrated) || len(p.Refused) != 3 {
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
