package main

import (
	"context"
	"fmt"
	"net/netip"
	"path"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/hostfake"
	"github.com/abbott/hardn/pkg/host/revert"
	"github.com/abbott/hardn/pkg/host/sshd"
	"github.com/abbott/hardn/pkg/host/sshd/migrate"
	"github.com/abbott/hardn/pkg/host/ufw"
)

// The simulated host of the engine's scenarios (next/p3-gallery-engine):
// hostfake's recording host.System, whose commands a model of the host
// answers. Nothing is run, written or read on this machine: the fake's
// files are in memory, and a command is a call of fakeHost.run. The model
// is ported from internal/tools/p2drive's fake VM (fake_test.go), which a
// main package's test file cannot share; its formats are the ones the
// units' own tests pin (sshd -T, ss -Htln, systemctl show -p Listen, ufw
// 0.36.2's status). Synthetic values only: alice, web-01.example.net at
// 192.0.2.20, the runner at 192.0.2.10, port 2230.

const (
	// galleryHardn is where the simulated host's hardn binary is, which
	// the revert timer runs.
	galleryHardn = "/usr/local/bin/hardn"
	// hostFQDN is the simulated host's name, and hostName its short
	// name, as hostname(1) prints it: the phrase a lockout-risk change is
	// confirmed by typing.
	hostFQDN = "web-01.example.net"
	hostName = "web-01"
	// applyConn is the session that applies a change: alice, from the
	// runner, on port 22 (SSH_CONNECTION's form).
	applyConn = "192.0.2.10 50022 192.0.2.20 22"
	// confirmConn is the new SSH connection to 2230 that confirms it.
	confirmConn = "192.0.2.10 50123 192.0.2.20 2230"
)

// galleryNow is the simulated host's clock: every scenario runs at this
// moment, so that its change id, its deadline and its countdown are the
// same on every run.
var galleryNow = time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)

// stockMain is sshd_config as the simulated hosts ship it: the stock
// Include first (sshd.IncludeLine), so 00-hardn.conf can be written.
const stockMain = sshd.IncludeLine + "\n\nUsePAM yes\n"

// fakeHost is one simulated host: the fake, and the state the model keeps
// of what the host runs. sshd listens on 22 through ssh.service, or
// through ssh.socket when socket is set; ufw is active and allows 22
// unless set otherwise; sudo has its stock includedir.
type fakeHost struct {
	f          *hostfake.System
	allowed    []uint16 // ufw's allow rules, both families
	inactive   bool     // ufw stopped, ENABLED=no
	noUFW      bool     // ufw not installed
	bound      []string // what ss shows of sshd's (or ssh.socket's) sockets
	socket     bool     // ssh.socket holds the listeners (Ubuntu 26.04)
	configured []string // ssh.socket's Listen, as the last daemon-reload set it
	// failReloads is how many reloads of ssh.service to refuse, as
	// systemctl does when sshd -t fails in ExecReload.
	failReloads int

	// mu guards timers and unexpected, which a test reads once play has
	// ended; the rest is the model's, changed only by run, which the
	// engine calls one command at a time.
	mu         sync.Mutex
	timers     map[string]time.Time // each systemd-run timer unit, and when it fires
	unexpected []string             // the commands the model does not answer
}

// newHost returns a simulated Debian-like host on hostfake: sshd_config
// with the stock Include, sshd on 22 by ssh.service, ufw active allowing
// 22, sudo with /etc/sudoers.d, the directories revert and the boot unit
// use, and root and alice's accounts.
func newHost() *fakeHost {
	h := &fakeHost{f: hostfake.New(host.Options{}), allowed: []uint16{22}, timers: map[string]time.Time{}}
	f := h.f
	f.AddFile(sshd.MainConfig, []byte(stockMain), 0o644, 0, 0)
	f.AddDir(sshd.DropInDir, 0o755, 0, 0)
	f.AddDir("/run/sshd", 0o755, 0, 0)
	f.AddDir(detect.SystemdRunDir, 0o755, 0, 0)
	f.AddDir("/var/lib", 0o755, 0, 0)
	f.AddDir("/var/backups", 0o755, 0, 0)
	f.AddFile(galleryHardn, []byte("ELF"), 0o755, 0, 0)
	f.AddFile("/proc/self/stat", []byte("5000 (hardn) S 1 5000 5000 34819 -1 4194560 0 0 0 0 0 0 0 0 20 0 1 0 500000 0 0\n"), 0o444, 0, 0)
	f.AddFile("/proc/sys/kernel/random/boot_id", []byte("8e2a3c4d-1b2c-4d3e-8f40-0123456789ab\n"), 0o444, 0, 0)
	f.AddDir(ufw.KernelIPv6, 0o555, 0, 0)
	f.AddFile(ufw.DefaultsFile, []byte("IPV6=yes\nDEFAULT_INPUT_POLICY=\"DROP\"\nDEFAULT_OUTPUT_POLICY=\"ACCEPT\"\n"), 0o644, 0, 0)
	f.AddFile("/etc/sudoers", []byte("Defaults env_reset\nroot ALL=(ALL:ALL) ALL\n%sudo ALL=(ALL:ALL) ALL\n@includedir /etc/sudoers.d\n"), 0o440, 0, 0)
	f.AddDir("/etc/sudoers.d", 0o755, 0, 0)
	f.AddDir(revert.SystemdUnitDir, 0o755, 0, 0)
	f.AddDir(revert.OpenRCInitDir, 0o755, 0, 0)
	f.AddDir(revert.OpenRCDefault, 0o755, 0, 0)
	f.AddUser(host.User{Name: "root", UID: 0, GID: 0, HomeDir: "/root"})
	f.AddUser(host.User{Name: "alice", UID: 1000, GID: 1000, HomeDir: "/home/alice"})
	f.AddGroup(host.Group{Name: "root", GID: 0})
	f.AddGroup(host.Group{Name: "alice", GID: 1000})
	f.AddFile("/etc/group", []byte("root:x:0:\nalice:x:1000:\n"), 0o644, 0, 0)
	h.saveRules()
	h.boot()
	f.Handler = h.run
	return h
}

// socketHost is newHost with ssh.socket holding the listeners, from
// sshd-socket-generator (Ubuntu 26.04).
func socketHost() *fakeHost {
	h := newHost()
	h.socket = true
	h.f.AddFile(detect.SocketGenerator, []byte("ELF"), 0o755, 0, 0)
	h.boot()
	return h
}

// openRCHost is newHost as Alpine runs it: OpenRC's sshd started, and no
// ufw installed.
func openRCHost() *fakeHost {
	h := newHost()
	h.noUFW = true
	h.f.AddFile(detect.OpenRCStarted+"/sshd", nil, 0o644, 0, 0)
	return h
}

// boot makes the files on disk the running configuration, as a reboot
// does: sshd's (or the socket's) listeners from the model.
func (h *fakeHost) boot() {
	if h.socket {
		h.bound, h.configured = h.socketListens(), h.socketListens()
		return
	}
	h.bound, h.configured = h.listening(), h.listening()
}

// socketListens is ssh.socket's Listen as a daemon-reload or a boot sets
// it: the generator's, the listeners the sshd files configure, unless
// v0.3.x's listen.conf ("ListenStream=", then "ListenStream=<port>") is in
// ssh.socket.d, whose empty assignment resets them to its port alone, on
// a dual-stack [::] (docs/safety/reload.md §3).
func (h *fakeHost) socketListens() []string {
	if e, ok := h.f.Entry(migrate.ListenConf); ok {
		for _, l := range strings.Split(string(e.Data), "\n") {
			if p, ok := strings.CutPrefix(l, "ListenStream="); ok && p != "" {
				return []string{"[::]:" + p}
			}
		}
	}
	return h.listening()
}

// files is the configuration sshd -f conf reads: conf (sshd_config, or
// sshd.Validate's copy with its Include mapped back) and the drop-ins
// beside it, by their real paths.
func (h *fakeHost) files(conf string) ([]byte, []sshd.File) {
	work := path.Dir(conf)
	e, _ := h.f.Entry(conf)
	main := strings.ReplaceAll(string(e.Data), "Include "+work+"/sshd_config.d/*.conf", sshd.IncludeLine)
	entries, _ := h.f.ReadDir(context.Background(), work+"/sshd_config.d")
	var drops []sshd.File
	for _, de := range entries {
		if d, ok := h.f.Entry(work + "/sshd_config.d/" + de.Name()); ok && d.Mode.IsRegular() {
			drops = append(drops, sshd.File{Path: sshd.DropInDir + "/" + de.Name(), Data: d.Data})
		}
	}
	return []byte(main), drops
}

// modelDefaults are the model's values for the keywords sshd -T always
// prints, shaped like OpenSSH's (sshd_config(5)), as migrate's tests and
// p2drive's fake model them; not read from a binary.
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

// listening is the listeners the installed files configure, by the model.
func (h *fakeHost) listening() []string {
	_, ls := sshdT(h.files(sshd.MainConfig))
	return ls
}

// ssOutput is ss -Htln's output for the sockets given ("0.0.0.0:22").
func ssOutput(socks []string) string {
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
func (h *fakeHost) ufwStatus() string {
	if h.inactive {
		return "Status: inactive\n"
	}
	var b strings.Builder
	b.WriteString("Status: active\nLogging: on (low)\nDefault: deny (incoming), allow (outgoing), disabled (routed)\nNew profiles: skip\n")
	fmt.Fprintf(&b, "\n%-26s %-12s%s\n%-26s %-12s%s\n", "To", "Action", "From", "--", "------", "----")
	for _, sfx := range []string{"", " (v6)"} {
		for _, p := range h.allowed {
			fmt.Fprintf(&b, "%-26s %-12s%-26s\n", fmt.Sprintf("%d/tcp%s", p, sfx), "ALLOW IN", "Anywhere"+sfx)
		}
	}
	return b.String()
}

// saveRules writes ufw's configuration and rule files as ufw keeps them.
func (h *fakeHost) saveRules() {
	enabled := "yes"
	if h.inactive {
		enabled = "no"
	}
	h.f.AddFile(ufw.ConfFile, []byte("ENABLED="+enabled+"\nLOGLEVEL=low\n"), 0o644, 0, 0)
	for _, r := range []struct{ path, any string }{{ufw.UserRules, "0.0.0.0/0"}, {ufw.User6Rules, "::/0"}} {
		var b strings.Builder
		b.WriteString("*filter\n:ufw-user-input - [0:0]\n### RULES ###\n")
		for _, p := range h.allowed {
			fmt.Fprintf(&b, "\n### tuple ### allow tcp %d %s any %s in\n-A ufw-user-input -p tcp --dport %d -j ACCEPT\n", p, r.any, r.any, p)
		}
		b.WriteString("\n### END RULES ###\nCOMMIT\n")
		h.f.AddFile(r.path, []byte(b.String()), 0o640, 0, 0)
	}
}

// run answers the commands the engine and the primitives run, changing
// the model as the host would change.
func (h *fakeHost) run(argv []string, _ []byte) hostfake.Response {
	cmd := strings.Join(argv, " ")
	switch path.Base(argv[0]) {
	case "sshd":
		return h.sshd(argv)
	case "rc-service":
		if cmd == "rc-service sshd reload" {
			h.bound = slices.Clone(h.listening())
			return hostfake.Response{}
		}
	case "sh": // OpenRC's timer, the sleeper revert starts detached; sh -n, the boot script's check
		return hostfake.Response{}
	case "systemd-analyze": // the boot unit's check, verify on its copy
		return hostfake.Response{}
	case "rc-update":
		if cmd == "rc-update add "+revert.BootUnitName+" default" {
			h.f.AddSymlink(revert.OpenRCDefault+"/"+revert.BootUnitName, revert.OpenRCInitDir+"/"+revert.BootUnitName)
			return hostfake.Response{}
		}
	case "ss":
		return hostfake.Response{Stdout: ssOutput(h.bound)}
	case "systemd-run":
		h.arm(argv)
		return hostfake.Response{}
	case "visudo":
		return hostfake.Response{Stdout: "/etc/sudoers: parsed OK\n"}
	case "ufw":
		if h.noUFW {
			return hostfake.Response{Err: host.ErrNotFound, ExitCode: -1}
		}
		if r, ok := h.ufw(argv); ok {
			return r
		}
	case "systemctl":
		if r, ok := h.systemctl(argv); ok {
			return r
		}
	}
	h.mu.Lock()
	h.unexpected = append(h.unexpected, cmd)
	h.mu.Unlock()
	return hostfake.Response{Err: fmt.Errorf("the gallery's host does not model %q: %w", cmd, hostfake.ErrUnscripted), ExitCode: -1}
}

// unanswered returns the commands the model did not answer, which a
// scenario's test fails on.
func (h *fakeHost) unanswered() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return slices.Clone(h.unexpected)
}

// arm records the timer systemd-run starts (--on-active=<N>s
// --unit=<unit>), firing a second after the window, as the Ubuntu 26.04
// run found the timer firing 1 to 2 s after the deadline hardn prints
// (docs/safety/vmtest.md §4.9).
func (h *fakeHost) arm(argv []string) {
	var unit string
	var after time.Duration
	for _, a := range argv[1:] {
		if v, ok := strings.CutPrefix(a, "--unit="); ok {
			unit = v
		}
		if v, ok := strings.CutPrefix(a, "--on-active="); ok {
			after, _ = time.ParseDuration(v)
		}
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.timers[unit+".timer"] = galleryNow.Add(after + time.Second)
}

// sshd answers sshd -t and sshd -T over the files -f names.
func (h *fakeHost) sshd(argv []string) hostfake.Response {
	main, drops := h.files(argv[slices.Index(argv, "-f")+1])
	if slices.Contains(argv, "-t") {
		return hostfake.Response{}
	}
	out, _ := sshdT(main, drops)
	return hostfake.Response{Stdout: out}
}

func (h *fakeHost) systemctl(argv []string) (hostfake.Response, bool) {
	state := func(on bool, yes, no string, code int) hostfake.Response {
		if on {
			return hostfake.Response{Stdout: yes + "\n"}
		}
		return hostfake.Response{Stdout: no + "\n", ExitCode: code}
	}
	switch cmd := strings.Join(argv, " "); {
	case cmd == "systemctl is-active ssh.service": // up on every simulated host
		return hostfake.Response{Stdout: "active\n"}, true
	case cmd == "systemctl is-enabled ssh.service":
		return state(!h.socket, "enabled", "disabled", 1), true
	case cmd == "systemctl is-active ssh.socket":
		return state(h.socket, "active", "inactive", 3), true
	case cmd == "systemctl is-enabled ssh.socket":
		return state(h.socket, "enabled", "disabled", 1), true
	case cmd == "systemctl reload ssh.service" && h.failReloads > 0:
		h.failReloads--
		return hostfake.Response{Stderr: "Job for ssh.service failed because the control process exited with error code.\n", ExitCode: 1}, true
	case cmd == "systemctl reload ssh.service":
		if !h.socket {
			h.bound = h.listening()
		}
		return hostfake.Response{}, true
	case cmd == "systemctl daemon-reload":
		h.configured = h.socketListens()
		return hostfake.Response{}, true
	case cmd == "systemctl show -p Listen ssh.socket":
		var b strings.Builder
		for _, l := range h.configured {
			fmt.Fprintf(&b, "Listen=%s (Stream)\n", l)
		}
		return hostfake.Response{Stdout: b.String()}, true
	case cmd == "systemctl restart ssh.socket":
		h.bound = slices.Clone(h.configured)
		return hostfake.Response{}, true
	case len(argv) == 3 && argv[1] == "stop" && strings.HasPrefix(argv[2], revert.UnitPrefix):
		return hostfake.Response{}, true
	case cmd == "systemctl --no-reload enable "+revert.BootUnitName+".service":
		h.f.AddSymlink(revert.SystemdWants+"/"+revert.BootUnitName+".service", revert.SystemdUnitDir+"/"+revert.BootUnitName+".service")
		return hostfake.Response{}, true
	case len(argv) == 6 && argv[1] == "list-timers":
		return h.listTimers(argv[5]), true
	}
	return hostfake.Response{}, false
}

// listTimers is systemctl list-timers --output=json for unit: its next
// trigger time in microseconds since the epoch, as systemd v257 prints it
// (pkg/engine's timerTrigger reads it).
func (h *fakeHost) listTimers(unit string) hostfake.Response {
	h.mu.Lock()
	at, ok := h.timers[unit]
	h.mu.Unlock()
	if !ok {
		return hostfake.Response{Stdout: "[]\n"}
	}
	return hostfake.Response{Stdout: fmt.Sprintf("[{\"next\":%d,\"left\":0,\"last\":null,\"passed\":null,\"unit\":%q,\"activates\":%q}]\n",
		at.UnixMicro(), unit, strings.TrimSuffix(unit, ".timer")+".service")}
}

// ufw answers ufw 0.36.2's commands: its status, enable, and a port's
// allow rule added or deleted, in both families.
func (h *fakeHost) ufw(argv []string) (hostfake.Response, bool) {
	cmd := strings.Join(argv, " ")
	switch cmd {
	case "ufw status verbose":
		return hostfake.Response{Stdout: h.ufwStatus()}, true
	case "ufw --force enable":
		h.inactive = false
		h.saveRules()
		return hostfake.Response{Stdout: "Firewall is active and enabled on system startup\n"}, true
	}
	word := "Rule added"
	if h.inactive {
		word = "Rules updated"
	}
	n, err := strconv.Atoi(strings.TrimSuffix(argv[len(argv)-1], "/tcp"))
	switch {
	case err != nil:
	case len(argv) == 3 && argv[1] == "allow":
		h.allowed = append(h.allowed, uint16(n))
		slices.Sort(h.allowed)
		h.allowed = slices.Compact(h.allowed)
		h.saveRules()
		return hostfake.Response{Stdout: word + "\n" + word + " (v6)\n"}, true
	case len(argv) == 4 && argv[1] == "delete" && argv[2] == "allow":
		h.allowed = slices.DeleteFunc(h.allowed, func(p uint16) bool { return p == uint16(n) })
		h.saveRules()
		return hostfake.Response{Stdout: "Rule deleted\nRule deleted (v6)\n"}, true
	}
	return hostfake.Response{}, false
}
