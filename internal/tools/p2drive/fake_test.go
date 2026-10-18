package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/netip"
	"os"
	"path"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/hostfake"
	"github.com/abbott/hardn/pkg/host/revert"
	"github.com/abbott/hardn/pkg/host/sshd"
	"github.com/abbott/hardn/pkg/host/sshd/migrate"
	"github.com/abbott/hardn/pkg/host/ufw"
)

// Nothing here runs a real command or touches the machine: every host is
// hostfake's, and its commands are answered by vm.run below, written from
// the formats the units' own tests pin (sshd -T, ss, systemctl show -p
// Listen, ufw 0.36.2). Synthetic values only: alice, web-01.example.net,
// 192.0.2.0/24, 2230.

const (
	testHardn = "/usr/local/bin/hardn"
	applyConn = "192.0.2.10 50022 192.0.2.20 22"
)

var testNow = time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)

// debian13 is detect's view of a stock Debian 13 host: ssh.service, sudo.
var debian13 = detect.Facts{
	OS:       detect.OSRelease{ID: "debian", VersionID: "13", VersionCodename: "trixie"},
	Platform: detect.Platform{Distro: detect.DistroDebian, Version: "13", Support: detect.Supported},
	Init:     detect.Init{Kind: detect.Systemd, Running: detect.Yes, Source: detect.SystemdRunDir},
	SSH: detect.SSH{Mode: detect.SSHService, Generator: detect.No,
		Socket:  detect.UnitState{Enabled: "disabled", Active: "inactive"},
		Service: detect.UnitState{Enabled: "enabled", Active: "active"}},
	Proxmox:   detect.Proxmox{Installed: detect.No, ClusterFS: detect.No, EtcPVE: detect.No},
	Container: detect.Container{Detected: detect.No},
	Elevation: detect.Elevation{Sudo: detect.Tool{Present: detect.Yes, Path: "/usr/bin/sudo"}, SudoFlavor: detect.SudoOriginal,
		Doas: detect.Tool{Present: detect.No}},
}

// ubuntu2604 is a stock Ubuntu 26.04: ssh.socket from the generator.
func ubuntu2604() detect.Facts {
	f := debian13
	f.OS = detect.OSRelease{ID: "ubuntu", VersionID: "26.04"}
	f.Platform = detect.Platform{Distro: detect.DistroUbuntu, Version: "26.04", Support: detect.Supported}
	f.SSH = detect.SSH{Mode: detect.SSHSocket, Generator: detect.Yes, GeneratorPath: detect.SocketGenerator,
		Socket:  detect.UnitState{Enabled: "enabled", Active: "active"},
		Service: detect.UnitState{Enabled: "disabled", Active: "active"}}
	return f
}

// proxmox9 is a Proxmox VE 9 node: Debian 13's ssh.service, pmxcfs mounted.
func proxmox9() detect.Facts {
	f := debian13
	f.Platform = detect.Platform{Distro: detect.DistroProxmox, Version: "9", Support: detect.Supported}
	f.Proxmox = detect.Proxmox{Installed: detect.Yes, Version: "9.0.10", ClusterFS: detect.Yes, EtcPVE: detect.Yes}
	return f
}

// alpine324 is a stock Alpine 3.24: OpenRC's sshd, doas, no ufw.
func alpine324() detect.Facts {
	f := debian13
	f.OS = detect.OSRelease{ID: "alpine", VersionID: "3.24.0"}
	f.Platform = detect.Platform{Distro: detect.DistroAlpine, Version: "3.24", Support: detect.Supported}
	f.Init = detect.Init{Kind: detect.OpenRC, Running: detect.Yes}
	f.SSH = detect.SSH{Mode: detect.SSHOpenRC, OpenRC: detect.OpenRCService{Started: detect.Yes}}
	f.Elevation = detect.Elevation{Sudo: detect.Tool{Present: detect.No}, Doas: detect.Tool{Present: detect.Yes, Path: "/usr/bin/doas"}}
	return f
}

// stockMain is sshd_config as the tests' hosts ship it.
const stockMain = sshd.IncludeLine + "\n\nUsePAM yes\n"

// vm is a fake disposable VM: sshd a model over its files (sshdT), on 22
// through ssh.service, or through ssh.socket with socket set; an active
// ufw allowing 22; sudo with its stock includedir.
type vm struct {
	t           *testing.T
	f           *hostfake.System
	allowed     []uint16 // ufw's allow rules, both families
	inactive    bool     // ufw stopped, ENABLED=no
	bound       []string // what ss shows of sshd's (or ssh.socket's) sockets
	other       []string // what ss shows of another service's sockets
	socket      bool     // ssh.socket holds the listeners (Ubuntu)
	configured  []string // ssh.socket's Listen, as the last daemon-reload set it
	down        bool     // ssh.service stopped
	failReloads int      // forward reloads to refuse, as sshd -t would
	// stale: sshd -T over the installed files leaves 00-hardn.conf out, as
	// an sshd that did not reread it would; for the values check.
	stale bool
	// hook answers a command first, when it returns true.
	hook func(cmd string) (hostfake.Response, bool)
	// held and released are the fake's call counts when the rollback's
	// signal hold began and ended; -1 when it did not.
	held, released int
}

func newVM(t *testing.T) *vm {
	v := &vm{t: t, f: hostfake.New(host.Options{}), allowed: []uint16{22}, held: -1, released: -1}
	f := v.f
	f.AddFile(sshd.MainConfig, []byte(stockMain), 0o644, 0, 0)
	f.AddDir(sshd.DropInDir, 0o755, 0, 0)
	f.AddDir("/run/sshd", 0o755, 0, 0)
	f.AddDir(detect.SystemdRunDir, 0o755, 0, 0)
	f.AddDir("/var/lib", 0o755, 0, 0)
	f.AddDir("/var/backups", 0o755, 0, 0)
	f.AddFile(testHardn, []byte("ELF"), 0o755, 0, 0)
	f.AddFile("/proc/self/stat", []byte("5000 (p2drive) S 1 5000 5000 34819 -1 4194560 0 0 0 0 0 0 0 0 20 0 1 0 500000 0 0\n"), 0o444, 0, 0)
	f.AddFile("/proc/sys/kernel/random/boot_id", []byte("8e2a3c4d-1b2c-4d3e-8f40-0123456789ab\n"), 0o444, 0, 0)
	f.AddDir(ufw.KernelIPv6, 0o555, 0, 0)
	f.AddFile(ufw.DefaultsFile, []byte("IPV6=yes\nDEFAULT_INPUT_POLICY=\"DROP\"\nDEFAULT_OUTPUT_POLICY=\"ACCEPT\"\n"), 0o644, 0, 0)
	f.AddFile("/etc/sudoers", []byte("Defaults env_reset\nroot ALL=(ALL:ALL) ALL\n%sudo ALL=(ALL:ALL) ALL\n@includedir /etc/sudoers.d\n"), 0o440, 0, 0)
	f.AddDir("/etc/sudoers.d", 0o755, 0, 0)
	f.AddUser(host.User{Name: "root", UID: 0, GID: 0, HomeDir: "/root"})
	f.AddGroup(host.Group{Name: "root", GID: 0})
	f.AddFile("/etc/group", []byte("root:x:0:\n"), 0o644, 0, 0)
	v.saveRules()
	v.boot()
	f.Handler = v.run
	return v
}

// socketVM is newVM with ssh.socket holding the listeners (Ubuntu).
func socketVM(t *testing.T) *vm {
	v := newVM(t)
	v.socket = true
	v.boot()
	return v
}

// boot makes the files on disk the running configuration, as a reboot
// does: sshd's (or the socket's) listeners from the model.
func (v *vm) boot() {
	if v.socket {
		v.bound, v.configured = v.socketListens(), v.socketListens()
		return
	}
	v.bound, v.configured = v.listening(), v.listening()
}

// socketListens is ssh.socket's Listen as a daemon-reload or a boot sets
// it: the generator's, the listeners the sshd files configure, unless
// v0.3.x's listen.conf ("ListenStream=", then "ListenStream=<port>") is in
// ssh.socket.d, whose empty assignment resets them to its port alone, on
// a dual-stack [::] (docs/safety/reload.md §3).
func (v *vm) socketListens() []string {
	if e, ok := v.f.Entry(migrate.ListenConf); ok {
		for _, l := range strings.Split(string(e.Data), "\n") {
			if p, ok := strings.CutPrefix(l, "ListenStream="); ok && p != "" {
				return []string{"[::]:" + p}
			}
		}
	}
	return v.listening()
}

// fixture reads one of migrate's verbatim v0.3.2 files.
func fixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(path.Join("..", "..", "..", "pkg", "host", "sshd", "migrate", "testdata", "legacy", name))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// env is production's with the fake and the facts given.
func (v *vm) env(facts detect.Facts, out, errOut io.Writer) env {
	return env{
		goos: "linux", euid: func() int { return 0 }, now: func() time.Time { return testNow },
		getenv: func(k string) string {
			if k == gateEnv {
				return gateValue
			}
			return ""
		},
		stdout: out, stderr: errOut,
		system: func(io.Writer) host.System { return v.f },
		detect: func(context.Context, host.System) (detect.Facts, error) { return facts, nil },
		session: func(context.Context, host.System) (revert.Session, error) {
			return revert.Session{Remote: true, Conn: applyConn, Source: "SSH_CONNECTION"}, nil
		},
		hold: func(io.Writer) func() {
			v.held = len(v.f.Calls())
			return func() { v.released = len(v.f.Calls()) }
		},
	}
}

// do runs p2drive with args on v as a host with facts, gated.
func (v *vm) do(ctx context.Context, facts detect.Facts, args ...string) (code int, stdout, stderr string) {
	var out, errOut bytes.Buffer
	code = run(ctx, append([]string{"--" + gateFlag}, args...), v.env(facts, &out, &errOut))
	return code, out.String(), errOut.String()
}

// files is the configuration sshd -f conf reads: conf (sshd_config, or
// sshd.Validate's copy with its Include mapped back) and the drop-ins
// beside it, by their real paths; without hardn, 00-hardn.conf left out.
func (v *vm) files(conf string, withoutHardn bool) ([]byte, []sshd.File) {
	work := path.Dir(conf)
	e, _ := v.f.Entry(conf)
	main := strings.ReplaceAll(string(e.Data), "Include "+work+"/sshd_config.d/*.conf", sshd.IncludeLine)
	entries, _ := v.f.ReadDir(context.Background(), work+"/sshd_config.d")
	var drops []sshd.File
	for _, de := range entries {
		d, ok := v.f.Entry(work + "/sshd_config.d/" + de.Name())
		if ok && d.Mode.IsRegular() && !(withoutHardn && de.Name() == path.Base(sshd.HardnDropIn)) {
			drops = append(drops, sshd.File{Path: sshd.DropInDir + "/" + de.Name(), Data: d.Data})
		}
	}
	return []byte(main), drops
}

// modelDefaults are the model's values for the keywords sshd -T always
// prints, shaped like OpenSSH's (sshd_config(5)), as migrate's tests model
// them; not read from a binary.
var modelDefaults = []struct{ k, v string }{
	{"permitrootlogin", "prohibit-password"}, {"pubkeyauthentication", "yes"}, {"passwordauthentication", "yes"},
	{"kbdinteractiveauthentication", "no"}, {"permitemptypasswords", "no"}, {"usepam", "no"},
	{"hostbasedauthentication", "no"}, {"gssapiauthentication", "no"}, {"kerberosauthentication", "no"},
	{"authorizedkeysfile", ".ssh/authorized_keys .ssh/authorized_keys2"}, {"authenticationmethods", "any"},
	{"ciphers", "chacha20-poly1305@openssh.com,aes256-gcm@openssh.com"}, {"macs", "hmac-sha2-256-etm@openssh.com,hmac-sha2-512-etm@openssh.com"},
	{"kexalgorithms", "mlkem768x25519-sha256,curve25519-sha256"}, {"hostkeyalgorithms", "ssh-ed25519,ecdsa-sha2-nistp256,rsa-sha2-512"},
	{"pubkeyacceptedalgorithms", "ssh-ed25519,ecdsa-sha2-nistp256,rsa-sha2-512,rsa-sha2-256"}, {"casignaturealgorithms", "ssh-ed25519,ecdsa-sha2-nistp256,rsa-sha2-512"},
	{"authorizedkeyscommand", "none"}, {"authorizedkeyscommanduser", "none"}, {"authorizedprincipalsfile", "none"},
	{"authorizedprincipalscommand", "none"}, {"authorizedprincipalscommanduser", "none"}, {"trustedusercakeys", "none"},
	{"revokedkeys", "none"}, {"pubkeyauthoptions", "none"}, {"maxauthtries", "6"}, {"forcecommand", "none"}, {"chrootdirectory", "none"},
	{"allowtcpforwarding", "yes"}, {"gatewayports", "no"}, {"x11forwarding", "no"}, {"addressfamily", "any"},
}

// sshdT is sshd -T's output by a model of sshd's reading (sshd.ReadOrder):
// the first value of each keyword outside Match blocks, every value of an
// accumulating one, modelDefaults for the rest; and the listeners: a
// ListenAddress with a port as it is, one without on every Port, with none
// every Port on [::] and 0.0.0.0 (sshd_config(5) ListenAddress).
func sshdT(main []byte, drops []sshd.File) (out string, listeners []string) {
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
	for _, d := range modelDefaults {
		v, ok := first[d.k]
		if !ok {
			v = d.v
		}
		if d.k != "authorizedkeysfile" {
			v = strings.ToLower(v)
		}
		b.WriteString(d.k + " " + v + "\n")
	}
	for _, k := range []string{"allowusers", "allowgroups", "denyusers", "denygroups"} {
		for _, v := range acc[k] {
			b.WriteString(k + " " + v + "\n")
		}
	}
	return b.String(), listeners
}

// ss is ss -Htln's output for the sockets given ("0.0.0.0:22", "[::]:22").
func ss(socks []string) string {
	var b strings.Builder
	for _, s := range socks {
		peer := "0.0.0.0:*"
		if strings.HasPrefix(s, "[") {
			peer = "[::]:*"
		}
		fmt.Fprintf(&b, "LISTEN 0      4096   %20s   %s\n", s, peer)
	}
	return b.String()
}

// ufwStatus is ufw 0.36.2's verbose status for the allow rules.
func (v *vm) ufwStatus() string {
	if v.inactive {
		return "Status: inactive\n"
	}
	var b strings.Builder
	b.WriteString("Status: active\nLogging: on (low)\nDefault: deny (incoming), allow (outgoing), disabled (routed)\nNew profiles: skip\n")
	fmt.Fprintf(&b, "\n%-26s %-12s%s\n%-26s %-12s%s\n", "To", "Action", "From", "--", "------", "----")
	for _, sfx := range []string{"", " (v6)"} {
		for _, p := range v.allowed {
			fmt.Fprintf(&b, "%-26s %-12s%-26s\n", fmt.Sprintf("%d/tcp%s", p, sfx), "ALLOW IN", "Anywhere"+sfx)
		}
	}
	return b.String()
}

func (v *vm) saveRules() {
	enabled := "yes"
	if v.inactive {
		enabled = "no"
	}
	v.f.AddFile(ufw.ConfFile, []byte("ENABLED="+enabled+"\nLOGLEVEL=low\n"), 0o644, 0, 0)
	for _, f := range []struct{ path, any string }{{ufw.UserRules, "0.0.0.0/0"}, {ufw.User6Rules, "::/0"}} {
		var b strings.Builder
		b.WriteString("*filter\n:ufw-user-input - [0:0]\n### RULES ###\n")
		for _, p := range v.allowed {
			fmt.Fprintf(&b, "\n### tuple ### allow tcp %d %s any %s in\n-A ufw-user-input -p tcp --dport %d -j ACCEPT\n", p, f.any, f.any, p)
		}
		b.WriteString("\n### END RULES ###\nCOMMIT\n")
		v.f.AddFile(f.path, []byte(b.String()), 0o640, 0, 0)
	}
}

// run answers the commands the driver and the primitives run.
func (v *vm) run(argv []string, _ []byte) hostfake.Response {
	cmd := strings.Join(argv, " ")
	if v.hook != nil {
		if r, ok := v.hook(cmd); ok {
			return r
		}
	}
	switch path.Base(argv[0]) {
	case "sshd":
		return v.sshd(argv)
	case "rc-service":
		if cmd == "rc-service sshd reload" {
			v.bound = slices.Clone(v.listening())
			return hostfake.Response{}
		}
	case "sh": // OpenRC's timer, the sleeper revert starts detached
		return hostfake.Response{}
	case "ss":
		return hostfake.Response{Stdout: ss(append(slices.Clone(v.bound), v.other...))}
	case "systemd-run":
		return hostfake.Response{}
	case "visudo":
		return hostfake.Response{Stdout: "/etc/sudoers: parsed OK\n"}
	case "ufw":
		return v.ufw(argv)
	case "systemctl":
		if r, ok := v.systemctl(argv); ok {
			return r
		}
	}
	v.t.Errorf("unexpected command %q", cmd)
	return hostfake.Response{Err: hostfake.ErrUnscripted, ExitCode: -1}
}

// sshd answers sshd -t (a PermitRootLogin maybe is refused, as sshd does,
// "unsupported option") and sshd -T over the files -f names.
func (v *vm) sshd(argv []string) hostfake.Response {
	conf := argv[slices.Index(argv, "-f")+1]
	main, drops := v.files(conf, v.stale && conf == sshd.MainConfig)
	if slices.Contains(argv, "-t") {
		for _, d := range drops {
			for i, l := range strings.Split(string(d.Data), "\n") {
				if strings.HasSuffix(l, " maybe") {
					return hostfake.Response{Stderr: fmt.Sprintf("%s line %d: unsupported option \"maybe\".\r\n",
						path.Join(path.Dir(conf), "sshd_config.d", path.Base(d.Path)), i+1), ExitCode: 255}
				}
			}
		}
		return hostfake.Response{}
	}
	out, _ := sshdT(main, drops)
	return hostfake.Response{Stdout: out}
}

// listening is the listeners the installed files configure, by the model.
func (v *vm) listening() []string {
	_, ls := sshdT(v.files(sshd.MainConfig, false))
	return ls
}

func (v *vm) systemctl(argv []string) (hostfake.Response, bool) {
	state := func(on bool, yes, no string, code int) hostfake.Response {
		if on {
			return hostfake.Response{Stdout: yes + "\n"}
		}
		return hostfake.Response{Stdout: no + "\n", ExitCode: code}
	}
	installed := v.listening
	switch cmd := strings.Join(argv, " "); {
	case cmd == "systemctl is-active ssh.service":
		return state(!v.down, "active", "inactive", 3), true
	case cmd == "systemctl is-enabled ssh.service":
		return state(!v.socket, "enabled", "disabled", 1), true
	case cmd == "systemctl is-active ssh.socket":
		return state(v.socket, "active", "inactive", 3), true
	case cmd == "systemctl is-enabled ssh.socket":
		return state(v.socket, "enabled", "disabled", 1), true
	case cmd == "systemctl reload ssh.service" && v.failReloads > 0:
		v.failReloads--
		return hostfake.Response{Stderr: "Job for ssh.service failed.\n", ExitCode: 1}, true
	case cmd == "systemctl reload ssh.service":
		if !v.socket {
			v.bound = installed()
		}
		return hostfake.Response{}, true
	case cmd == "systemctl start ssh.service":
		v.down = false
		if !v.socket {
			v.bound = installed()
		}
		return hostfake.Response{}, true
	case cmd == "systemctl daemon-reload":
		v.configured = v.socketListens()
		return hostfake.Response{}, true
	case cmd == "systemctl show -p Listen ssh.socket":
		var b strings.Builder
		for _, l := range v.configured {
			fmt.Fprintf(&b, "Listen=%s (Stream)\n", l)
		}
		return hostfake.Response{Stdout: b.String()}, true
	case cmd == "systemctl restart ssh.socket":
		v.bound = slices.Clone(v.configured)
		return hostfake.Response{}, true
	case len(argv) == 3 && argv[1] == "stop" && strings.HasPrefix(argv[2], revert.UnitPrefix):
		return hostfake.Response{}, true
	}
	return hostfake.Response{}, false
}

func (v *vm) ufw(argv []string) hostfake.Response {
	cmd := strings.Join(argv, " ")
	switch cmd {
	case "ufw status verbose":
		return hostfake.Response{Stdout: v.ufwStatus()}
	case "ufw --force enable", "ufw disable":
		v.inactive = cmd == "ufw disable"
		v.saveRules()
		return hostfake.Response{Stdout: "Firewall is active and enabled on system startup\n"}
	}
	word := "Rule added"
	if v.inactive {
		word = "Rules updated"
	}
	spec := argv[len(argv)-1]
	n, err := strconv.Atoi(strings.TrimSuffix(spec, "/tcp"))
	switch {
	case err != nil:
	case len(argv) == 3 && argv[1] == "allow":
		v.allowed = append(v.allowed, uint16(n))
		slices.Sort(v.allowed)
		v.saveRules()
		return hostfake.Response{Stdout: word + "\n" + word + " (v6)\n"}
	case len(argv) == 4 && argv[1] == "delete" && argv[2] == "allow":
		v.allowed = slices.DeleteFunc(v.allowed, func(p uint16) bool { return p == uint16(n) })
		v.saveRules()
		return hostfake.Response{Stdout: "Rule deleted\nRule deleted (v6)\n"}
	}
	v.t.Errorf("unexpected command %q", cmd)
	return hostfake.Response{Err: hostfake.ErrUnscripted, ExitCode: -1}
}

// index is the position in the fake's record of the first call match
// accepts, or -1.
func (v *vm) index(match func(hostfake.Call) bool) int {
	return slices.IndexFunc(v.f.Calls(), match)
}

func runIs(cmd string) func(hostfake.Call) bool {
	return func(c hostfake.Call) bool { return c.Op == "run" && strings.Join(c.Argv, " ") == cmd }
}

func writeOf(p string) func(hostfake.Call) bool {
	return func(c hostfake.Call) bool { return c.Op == "write-atomic" && c.Path == p && c.Err == nil }
}

func armed(c hostfake.Call) bool { return c.Op == "run" && c.Argv[0] == "systemd-run" }

// changes lists the calls that changed the host outside the run
// directories: a file written, removed, renamed or chmod-ed, a directory
// made, or a command that is not a read.
func (v *vm) changes() []string {
	var out []string
	for _, c := range v.f.Calls() {
		switch c.Op {
		case "write-atomic", "write-in-place", "remove", "removeall", "rename", "chmod", "chown", "mkdir":
			if !strings.HasPrefix(c.Path, runRoot+"/") {
				out = append(out, c.String())
			}
		case "run":
			cmd := strings.Join(c.Argv, " ")
			if c.Argv[0] == "sshd" || c.Argv[0] == "ss" || cmd == "ufw status verbose" ||
				c.Argv[0] == "systemctl" && (c.Argv[1] == "is-active" || c.Argv[1] == "is-enabled" || c.Argv[1] == "show") {
				continue
			}
			out = append(out, c.String())
		}
	}
	return out
}

// journal decodes the change journal file name, or fails.
func (v *vm) journal(name string) revert.Journal {
	v.t.Helper()
	e, ok := v.f.Entry(path.Join(revert.DefaultDir, name))
	if !ok {
		v.t.Fatalf("%s: missing", name)
	}
	var j revert.Journal
	if err := json.Unmarshal(e.Data, &j); err != nil {
		v.t.Fatalf("%s: %v", name, err)
	}
	return j
}

// cleared fails unless the journal holds nothing: rolled back and done.
func (v *vm) cleared() {
	v.t.Helper()
	for _, name := range []string{revert.PendingName, revert.RollbackName, revert.RevertedName} {
		if _, ok := v.f.Entry(path.Join(revert.DefaultDir, name)); ok {
			v.t.Errorf("%s left behind", name)
		}
	}
}

// dropIn is 00-hardn.conf's content, "" when absent.
func (v *vm) dropIn() string {
	e, _ := v.f.Entry(sshd.HardnDropIn)
	return string(e.Data)
}
