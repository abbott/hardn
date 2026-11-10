package sshchange_test

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"io/fs"
	"net/netip"
	"os"
	"path"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/abbott/hardn/pkg/engine"
	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/accounts"
	"github.com/abbott/hardn/pkg/host/backup"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/hostfake"
	"github.com/abbott/hardn/pkg/host/reload"
	"github.com/abbott/hardn/pkg/host/revert"
	"github.com/abbott/hardn/pkg/host/selinux"
	"github.com/abbott/hardn/pkg/host/sshd"
	"github.com/abbott/hardn/pkg/host/sshd/migrate"
	"github.com/abbott/hardn/pkg/host/sudoers"
	"github.com/abbott/hardn/pkg/host/ufw"
)

// Nothing here runs a real command or reads the machine: every host is
// hostfake's, its commands answered by vm.run, a model written from the
// formats the readers' own tests pin and the VMs' read of 2026-11-03
// (vm-runs/s6/00-inspect-*.log, 01-inspect-b-*.log): sshd -T over the
// fake's files, with -C applying the Match blocks it matches; sshd's
// journal; ss; systemctl; ufw 0.36.2; visudo. Synthetic values only:
// george, alice, web-01, 192.0.2.x, 198.51.100.x, 2230.

const (
	hardnBinary = "/usr/local/bin/hardn"
	sessionPort = "50022"
	applyConn   = "192.0.2.10 " + sessionPort + " 192.0.2.20 22"
	hashSet     = "$y$j9T$Kp3Lm8Nq2Rs5Tv7Wx9Yz1$Bc4Df6Gh8Jk0Lm2Np4Qr6St8Uv0Wx2Yz4Ab6Cd8Ef0"
	sudoPath    = "/usr/bin/sudo"
	// debianMain is the Debian 13 VM's sshd_config as sshd reads it: the
	// stock Include first, then KbdInteractiveAuthentication no,
	// PasswordAuthentication no and UsePAM yes.
	debianMain = sshd.IncludeLine + "\n\nKbdInteractiveAuthentication no\nPasswordAuthentication no\nUsePAM yes\n"
	// cloudInit is cloud-init's rule for the login user, as the VMs have it.
	cloudInit = sudoers.DropInDir + "/90-cloud-init-users"
)

var now = time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC)

// debian13 is detect's view of the Debian 13 VM: ssh.service, sudo.
func debian13() detect.Facts {
	return detect.Facts{
		OS:       detect.OSRelease{ID: "debian", VersionID: "13", VersionCodename: "trixie"},
		Platform: detect.Platform{Distro: detect.DistroDebian, Version: "13", Support: detect.Supported},
		Init:     detect.Init{Kind: detect.Systemd, Running: detect.Yes, Source: detect.SystemdRunDir},
		SSH: detect.SSH{Mode: detect.SSHService, Generator: detect.No,
			Socket:  detect.UnitState{Enabled: "disabled", Active: "inactive"},
			Service: detect.UnitState{Enabled: "enabled", Active: "active"}},
		Proxmox:   detect.Proxmox{Installed: detect.No, ClusterFS: detect.No, EtcPVE: detect.No},
		Container: detect.Container{Detected: detect.No},
		Elevation: detect.Elevation{Sudo: detect.Tool{Present: detect.Yes, Path: sudoPath}, SudoFlavor: detect.SudoOriginal,
			Doas: detect.Tool{Present: detect.No}},
	}
}

// ubuntu2604 is the Ubuntu 26.04 VM: ssh.socket from the generator.
func ubuntu2604() detect.Facts {
	f := debian13()
	f.OS = detect.OSRelease{ID: "ubuntu", VersionID: "26.04", VersionCodename: "resolute"}
	f.Platform = detect.Platform{Distro: detect.DistroUbuntu, Version: "26.04", Support: detect.Supported}
	f.SSH = detect.SSH{Mode: detect.SSHSocket, Generator: detect.Yes, GeneratorPath: detect.SocketGenerator,
		Socket:  detect.UnitState{Enabled: "enabled", Active: "active"},
		Service: detect.UnitState{Enabled: "disabled", Active: "active"}}
	return f
}

// rocky102 is rocky10-test-110: Rocky Linux 10.2, sshd.service (detect
// asked sshd.* where systemd has no ssh.* unit), sudo.
func rocky102() detect.Facts {
	f := debian13()
	f.OS = detect.OSRelease{ID: "rocky", IDLike: []string{"rhel", "centos", "fedora"}, VersionID: "10.2"}
	f.Platform = detect.Platform{Distro: detect.DistroRocky, Version: "10", Support: detect.Supported}
	f.SSH = detect.SSH{Mode: detect.SSHService, Generator: detect.No, SSHDUnits: true, Source: "sshd.service enabled, active",
		Socket:  detect.UnitState{Enabled: "disabled", Active: "inactive"},
		Service: detect.UnitState{Enabled: "enabled", Active: "active"}}
	return f
}

// pve9 is a Proxmox VE 9 node: Debian 13's ssh.service, pmxcfs mounted, no
// sudo.
func pve9() detect.Facts {
	f := debian13()
	f.Platform = detect.Platform{Distro: detect.DistroProxmox, Version: "9", Support: detect.Supported}
	f.Proxmox = detect.Proxmox{Installed: detect.Yes, Version: "9.0.10", ClusterFS: detect.Yes, EtcPVE: detect.Yes}
	f.Elevation.Sudo = detect.Tool{Present: detect.No}
	return f
}

// account is one user of a host: its passwd line's uid, its shadow field,
// and its keys (each a whole authorized_keys line).
type account struct {
	name string
	uid  int
	pw   string
	keys []string
}

// ufwRule is an allow rule of ufw's for a TCP port, from src ("" anywhere),
// on iface ("" any).
type ufwRule struct {
	port       uint16
	src, iface string
}

// vm is a fake host: sshd a model over its files, on 22 through
// ssh.service, or ssh.socket with socket set; ufw active with rules.
type vm struct {
	t     *testing.T
	f     *hostfake.System
	facts detect.Facts
	// session is the applying session; log is sshd's journal.
	session revert.Session
	log     string
	// rules are ufw's allow rules; noUFW: ufw not installed.
	rules []ufwRule
	noUFW bool
	// bound is what ss shows; configured is ssh.socket's Listen as the
	// last daemon-reload set it; socket: ssh.socket holds the listeners.
	bound, configured []string
	socket            bool
	// noPAM: sshd -T prints no usepam line, as a build without PAM;
	// noRSASize no requiredrsasize line, as OpenSSH before 9.1.
	noPAM, noRSASize bool
	// sshdFail is sshd -T's stderr where it fails another way than
	// /run/sshd missing (exit 255); noFollowErr fails ReadFileNoFollow of
	// a path, as an unreadable legacy file.
	sshdFail    string
	noFollowErr map[string]error
	// sshdUnits: systemd has sshd.socket and sshd.service, no ssh.* unit
	// (Rocky Linux, AlmaLinux); reloads records each reload of sshd.service.
	sshdUnits bool
	reloads   int
	// mislabeled: matchpathcon -V says these paths have another label than
	// the policy's (the value); labelled: every path it was asked about.
	mislabeled map[string]string
	labelled   []string
	hardnType  string // stat's type of hardn's binary; "" is bin_t
	// sshPorts are semanage's ssh_port_t entries (nil: no semanage);
	// otherPorts its other exact TCP entries, by type.
	sshPorts   []uint16
	otherPorts map[uint16]string
	// fw is firewalld (nil: not installed).
	fw *fwModel
	// downAt, when set, runs between the apply and the rollback.
	downAt func()
}

// fwModel is firewalld running with one zone, public, the default and
// active one, its services and ports at runtime and in the permanent
// configuration. Stopped, firewall-offline-cmd edits the permanent one,
// and enabled is whether it starts at boot.
type fwModel struct {
	services         []string
	runtime, perm    map[string]bool // "2230/tcp"
	stopped, enabled bool
	added, removed   []string // the --add-port/--remove-port run
}

// offline answers firewall-offline-cmd's port query and removal in public.
func (m *fwModel) offline(argv []string) hostfake.Response {
	switch a := strings.Join(argv[1:], " "); {
	case strings.HasPrefix(a, "--zone=public --query-port="):
		if m.perm[strings.TrimPrefix(a, "--zone=public --query-port=")] {
			return hostfake.Response{Stdout: "yes\n"}
		}
		return hostfake.Response{Stdout: "no\n", ExitCode: 1}
	case strings.HasPrefix(a, "--zone=public --remove-port="):
		delete(m.perm, strings.TrimPrefix(a, "--zone=public --remove-port="))
		m.removed = append(m.removed, strings.Join(argv, " "))
		return hostfake.Response{Stdout: "success\n"}
	}
	return hostfake.Response{Err: hostfake.ErrUnscripted}
}

func (m *fwModel) run(argv []string) hostfake.Response {
	if m.stopped {
		return hostfake.Response{Stdout: "not running\n", ExitCode: 252}
	}
	args, perm := argv[1:], false
	if len(args) > 0 && args[0] == "--permanent" {
		args, perm = args[1:], true
	}
	ports := m.runtime
	if perm {
		ports = m.perm
	}
	yesNo := func(ok bool) hostfake.Response {
		if ok {
			return hostfake.Response{Stdout: "yes\n"}
		}
		return hostfake.Response{Stdout: "no\n", ExitCode: 1}
	}
	switch a := strings.Join(args, " "); {
	case a == "--state":
		return hostfake.Response{Stdout: "running\n"}
	case a == "--get-default-zone":
		return hostfake.Response{Stdout: "public\n"}
	case a == "--get-active-zones":
		return hostfake.Response{Stdout: "public (default)\n  interfaces: eth0\n"}
	case a == "--zone=public --get-target":
		return hostfake.Response{Stdout: "default\n"}
	case strings.HasPrefix(a, "--zone=public --query-port="):
		return yesNo(ports[strings.TrimPrefix(a, "--zone=public --query-port=")])
	case a == "--zone=public --list-services":
		return hostfake.Response{Stdout: strings.Join(m.services, " ") + "\n"}
	case strings.HasPrefix(a, "--service=") && strings.HasSuffix(a, " --get-ports"):
		ports := map[string]string{"ssh": "22/tcp", "cockpit": "9090/tcp", "dhcpv6-client": "546/udp"}
		return hostfake.Response{Stdout: ports[strings.TrimSuffix(strings.TrimPrefix(a, "--service="), " --get-ports")] + "\n"}
	case strings.HasPrefix(a, "--zone=public --add-port="):
		ports[strings.TrimPrefix(a, "--zone=public --add-port=")] = true
		m.added = append(m.added, strings.Join(argv, " "))
		return hostfake.Response{Stdout: "success\n"}
	case strings.HasPrefix(a, "--zone=public --remove-port="):
		delete(ports, strings.TrimPrefix(a, "--zone=public --remove-port="))
		m.removed = append(m.removed, strings.Join(argv, " "))
		return hostfake.Response{Stdout: "success\n"}
	}
	return hostfake.Response{Err: hostfake.ErrUnscripted}
}

// semanage answers port -l, -a and -d over v.sshPorts and v.otherPorts.
func (v *vm) semanage(argv []string) hostfake.Response {
	if v.sshPorts == nil {
		return hostfake.Response{Err: host.ErrNotFound}
	}
	switch {
	case len(argv) == 3 && argv[1] == "port" && argv[2] == "-l":
		var b strings.Builder
		b.WriteString("SELinux Port Type              Proto    Port Number\n\n")
		var ps []string
		for _, p := range v.sshPorts {
			ps = append(ps, strconv.Itoa(int(p)))
		}
		fmt.Fprintf(&b, "ssh_port_t                     tcp      %s\n", strings.Join(ps, ", "))
		for p, t := range v.otherPorts {
			fmt.Fprintf(&b, "%-30s tcp      %d\n", t, p)
		}
		b.WriteString("unreserved_port_t              tcp      61000-65535, 1024-32767\n")
		return hostfake.Response{Stdout: b.String()}
	case len(argv) == 8 && argv[1] == "port" && argv[3] == "-t" && argv[4] == "ssh_port_t" && argv[5] == "-p" && argv[6] == "tcp":
		n, _ := strconv.Atoi(argv[7])
		switch argv[2] {
		case "-a":
			v.sshPorts = append(v.sshPorts, uint16(n))
		case "-d":
			v.sshPorts = slices.DeleteFunc(v.sshPorts, func(p uint16) bool { return p == uint16(n) })
		}
		return hostfake.Response{}
	}
	return hostfake.Response{Err: hostfake.ErrUnscripted}
}

// key is a synthetic ssh-ed25519 key line, every byte of its key seed.
func key(seed byte, comment string) string {
	blob := binary.BigEndian.AppendUint32(nil, 11)
	blob = append(blob, "ssh-ed25519"...)
	blob = binary.BigEndian.AppendUint32(blob, 32)
	for range 32 {
		blob = append(blob, seed)
	}
	return "ssh-ed25519 " + base64.StdEncoding.EncodeToString(blob) + " " + comment
}

// rsaKey is a synthetic ssh-rsa key line whose modulus is bits long.
func rsaKey(bits int, seed byte, comment string) string {
	blob := binary.BigEndian.AppendUint32(nil, 7)
	blob = append(blob, "ssh-rsa"...)
	blob = binary.BigEndian.AppendUint32(blob, 3)
	blob = append(blob, 1, 0, 1)
	blob = binary.BigEndian.AppendUint32(blob, uint32(bits/8+1))
	blob = append(blob, 0, 0x80|seed)
	for range bits/8 - 1 {
		blob = append(blob, seed)
	}
	return "ssh-rsa " + base64.StdEncoding.EncodeToString(blob) + " " + comment
}

var (
	// george logs in by key, his password field "!", cloud-init's NOPASSWD
	// rule his; alice has a password and no key; both in sudo.
	george = account{name: "george", uid: 1000, pw: "!", keys: []string{key(1, "george@laptop-1")}}
	alice  = account{name: "alice", uid: 1001, pw: hashSet}
)

// newVM is the Debian 13 VM (facts) with the accounts given, all in group
// sudo, cloud-init's NOPASSWD rule for the first where cloud; root "*"
// with george's key; ufw active, 22 from anywhere; george's session by key
// from 192.0.2.10.
func newVM(t *testing.T, facts detect.Facts, cloud bool, as ...account) *vm {
	v := &vm{t: t, f: hostfake.New(host.Options{}), facts: facts, rules: []ufwRule{{port: 22}},
		session: revert.Session{Remote: true, Conn: applyConn, Source: "SSH_CONNECTION"}}
	f := v.f
	for _, d := range []string{"/etc", "/bin", "/usr/bin", "/usr/sbin", "/home", "/var/lib", "/var/backups", "/var/log", "/run", "/usr/local/bin",
		"/usr/lib/systemd/system", "/etc/systemd/system", "/etc/init.d", "/etc/runlevels/default", "/etc/ssh", sshd.DropInDir, "/run/sshd",
		detect.SystemdRunDir, revert.SystemdUnitDir, revert.OpenRCInitDir, revert.OpenRCDefault} {
		f.AddDir(d, 0o755, 0, 0)
	}
	f.AddDir(revert.ProcDir, 0o555, 0, 0) // no sshd process: the session is the journal's alone
	f.AddDir("/root", 0o700, 0, 0)
	f.AddFile(hardnBinary, []byte("ELF"), 0o755, 0, 0)
	f.AddFile("/bin/bash", nil, 0o755, 0, 0)
	f.AddFile(sudoPath, nil, 0o755, 0, 0)
	f.AddFile(sshd.MainConfig, []byte(debianMain), 0o644, 0, 0)
	passwd := "root:x:0:0:root:/root:/bin/bash\n"
	shadow := "root:*:20000:0:99999:7:::\n"
	group := "root:x:0:\n"
	var members []string
	for _, a := range as {
		passwd += fmt.Sprintf("%s:x:%d:%d::/home/%s:/bin/bash\n", a.name, a.uid, a.uid, a.name)
		shadow += a.name + ":" + a.pw + ":20000:0:99999:7:::\n"
		group += fmt.Sprintf("%s:x:%d:\n", a.name, a.uid)
		members = append(members, a.name)
		home := "/home/" + a.name
		f.AddDir(home, 0o755, a.uid, a.uid)
		if len(a.keys) > 0 {
			f.AddDir(home+"/.ssh", 0o700, a.uid, a.uid)
			f.AddFile(home+"/.ssh/authorized_keys", []byte(strings.Join(a.keys, "\n")+"\n"), 0o600, a.uid, a.uid)
		}
		f.AddUser(host.User{Name: a.name, UID: a.uid, GID: a.uid, HomeDir: home})
	}
	group += "sudo:x:27:" + strings.Join(members, ",") + "\n"
	f.AddFile(accounts.PasswdFile, []byte(passwd), 0o644, 0, 0)
	f.AddFile(accounts.ShadowFile, []byte(shadow), 0o640, 0, 42)
	f.AddFile("/etc/group", []byte(group), 0o644, 0, 0)
	f.AddUser(host.User{Name: "root", UID: 0, GID: 0, HomeDir: "/root"})
	f.AddGroup(host.Group{Name: "root", GID: 0})
	f.AddDir("/root/.ssh", 0o700, 0, 0)
	f.AddFile("/root/.ssh/authorized_keys", []byte(george.keys[0]+"\n"), 0o600, 0, 0)
	if facts.Elevation.Sudo.Present == detect.Yes {
		f.AddFile(sudoers.MainConfig, []byte("Defaults\tenv_reset\nroot\tALL=(ALL:ALL) ALL\n%sudo\tALL=(ALL:ALL) ALL\n@includedir /etc/sudoers.d\n"), 0o440, 0, 0)
		f.AddDir(sudoers.DropInDir, 0o750, 0, 0)
		if cloud && len(as) > 0 {
			f.AddFile(cloudInit, []byte(as[0].name+" ALL=(ALL) NOPASSWD:ALL\n"), 0o440, 0, 0)
		}
	}
	f.AddDir(ufw.KernelIPv6, 0o555, 0, 0)
	f.AddFile(ufw.DefaultsFile, []byte("IPV6=yes\nDEFAULT_INPUT_POLICY=\"DROP\"\nDEFAULT_OUTPUT_POLICY=\"ACCEPT\"\n"), 0o644, 0, 0)
	v.saveRules()
	v.logIn("publickey", "george", "192.0.2.10")
	v.boot()
	f.Handler = v.run
	return v
}

// debianVM is the Debian 13 VM: george and alice, cloud-init's rule
// george's.
func debianVM(t *testing.T) *vm { return newVM(t, debian13(), true, george, alice) }

// elPorts is rockyVM with SELinux enforcing, semanage's stock ssh_port_t
// (22) and firewalld running with public's stock services (ssh, cockpit,
// dhcpv6-client), as rocky10-test-110 would have it.
func elPorts(t *testing.T) *vm {
	v := rockyVM(t).enforcing()
	v.sshPorts = []uint16{22}
	v.fw = &fwModel{services: []string{"cockpit", "dhcpv6-client", "ssh"}, runtime: map[string]bool{}, perm: map[string]bool{}}
	return v
}

// enforcing turns SELinux on, enforcing, as on rocky10-test-110.
func (v *vm) enforcing() *vm {
	v.f.AddDir("/sys/fs/selinux", 0o755, 0, 0)
	v.f.AddFile(selinux.EnforceFile, []byte("1"), 0o644, 0, 0)
	return v
}

// rockyVM is rocky10-test-110 as far as the SSH changes read it: Debian
// 13's files and accounts, sshd.service in place of ssh.service, the stock
// /etc/sysconfig/sshd, no ufw.
func rockyVM(t *testing.T) *vm {
	v := newVM(t, rocky102(), true, george, alice)
	v.sshdUnits, v.noUFW = true, true
	v.f.AddDir("/etc/sysconfig", 0o755, 0, 0)
	v.f.AddFile(reload.SysconfigSSHD, []byte("# Configuration file for the sshd service.\nOPTIONS=\"\"\n"), 0o644, 0, 0)
	return v
}

// logIn makes sshd's journal hold the session's login: method, user, from
// addr on the session's port, as journalctl -o short-iso prints it.
func (v *vm) logIn(method, user, addr string) {
	what := ""
	if method == "publickey" {
		what = ": ED25519 SHA256:c3ludGhldGljLWtleS1vZi1nZW9yZ2UtbGFwdG9wLTE"
	}
	v.log += fmt.Sprintf("2026-11-03T11:58:00+00:00 web-01 sshd-session[3900]: Accepted %s for %s from %s port %s ssh2%s\n", method, user, addr, sessionPort, what)
}

// console makes the session root's on the console, as ReadSession finds
// it under login(1): no SSH connection.
func (v *vm) console() *vm {
	v.session = revert.Session{Source: "a console login (/proc/4990 is login)"}
	v.log = ""
	return v
}

// file writes name and boots, so that sshd runs with it.
func (v *vm) file(name, data string) *vm {
	v.f.AddFile(name, []byte(data), 0o644, 0, 0)
	v.boot()
	return v
}

// shadowOf sets name's password field.
func (v *vm) shadowOf(name, pw string) *vm {
	e, _ := v.f.Entry(accounts.ShadowFile)
	lines := strings.Split(string(e.Data), "\n")
	for i, l := range lines {
		if f := strings.SplitN(l, ":", 3); len(f) == 3 && f[0] == name {
			lines[i] = name + ":" + pw + ":" + f[2]
		}
	}
	v.f.AddFile(accounts.ShadowFile, []byte(strings.Join(lines, "\n")), 0o640, 0, 42)
	return v
}

// boot makes the files on disk the running configuration, as a reboot
// does.
func (v *vm) boot() {
	if v.socket {
		v.bound, v.configured = v.socketListens(), v.socketListens()
		return
	}
	v.bound, v.configured = v.listening(), v.listening()
}

// socketListens is ssh.socket's Listen as a daemon-reload sets it: the
// sshd files' listeners, unless v0.3.x's listen.conf resets them to its
// port, on a dual-stack [::].
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
	data, err := os.ReadFile(path.Join("..", "..", "..", "host", "sshd", "migrate", "testdata", "legacy", name))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// files is the configuration sshd -f conf reads, by the real paths.
func (v *vm) files(conf string) ([]byte, []sshd.File) {
	work := path.Dir(conf)
	e, _ := v.f.Entry(conf)
	main := strings.ReplaceAll(string(e.Data), "Include "+work+"/sshd_config.d/*.conf", sshd.IncludeLine)
	entries, _ := v.f.ReadDir(context.Background(), work+"/sshd_config.d")
	var drops []sshd.File
	for _, de := range entries {
		d, ok := v.f.Entry(work + "/sshd_config.d/" + de.Name())
		if ok && d.Mode.IsRegular() && strings.HasSuffix(de.Name(), ".conf") {
			drops = append(drops, sshd.File{Path: sshd.DropInDir + "/" + de.Name(), Data: d.Data})
		}
	}
	return []byte(main), drops
}

// modelDefaults are the model's values for the keywords sshd -T always
// prints, shaped like OpenSSH's (sshd_config(5)); not read from a binary.
var modelDefaults = []struct{ k, v string }{
	{"permitrootlogin", "prohibit-password"}, {"pubkeyauthentication", "yes"}, {"passwordauthentication", "yes"},
	{"kbdinteractiveauthentication", "no"}, {"permitemptypasswords", "no"}, {"usepam", "no"}, {"strictmodes", "yes"},
	{"authorizedkeysfile", ".ssh/authorized_keys .ssh/authorized_keys2"}, {"authenticationmethods", "any"},
	{"pubkeyacceptedalgorithms", "ssh-ed25519,ecdsa-sha2-nistp256,rsa-sha2-512,rsa-sha2-256"}, {"requiredrsasize", "1024"},
	{"authorizedkeyscommand", "none"}, {"addressfamily", "any"},
}

// sshdT is sshd -T's output by a model of sshd's reading (sshd.ReadOrder):
// the first value of each keyword outside Match blocks, every value of an
// accumulating one (one per line, as sshd prints AllowUsers), the
// defaults for the rest; and the listeners: every Port on [::] and
// 0.0.0.0, a ListenAddress with a port as it is.
func sshdT(main []byte, drops []sshd.File, noPAM, noRSASize bool) (out string, listeners []string) {
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
	for _, d := range modelDefaults {
		if d.k == "usepam" && noPAM || d.k == "requiredrsasize" && noRSASize {
			continue
		}
		v, ok := first[d.k]
		if !ok {
			v = d.v
		}
		if d.k != "authorizedkeysfile" && d.k != "authenticationmethods" {
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

// matched is main with the directives of the Match blocks the connection
// spec ("user=george,host=192.0.2.10,addr=192.0.2.10") matches put first,
// so that sshdT prints them: sshd_config(5), the first matching block's
// value winning. The criteria modelled are All, User, Address and Host
// (the address); Group never matches; a negated pattern refuses.
func matched(main []byte, drops []sshd.File, spec string) []byte {
	conn := map[string]string{}
	for _, kv := range strings.Split(spec, ",") {
		if k, val, ok := strings.Cut(kv, "="); ok {
			conn[k] = val
		}
	}
	var first []string
	for _, l := range sshd.ReadOrder(main, drops) {
		if l.Match != nil && matches(l.Match.Text, conn) {
			first = append(first, l.Keyword+" "+strings.Join(l.Args, " "))
		}
	}
	if len(first) == 0 {
		return main
	}
	return []byte(strings.Join(first, "\n") + "\n" + string(main))
}

// matches reports whether a Match line's criteria all match conn.
func matches(line string, conn map[string]string) bool {
	f := strings.Fields(line)
	for i := 1; i < len(f); i++ {
		crit := strings.ToLower(f[i])
		if crit == "all" {
			continue
		}
		if i+1 >= len(f) {
			return false
		}
		list := f[i+1]
		i++
		var s string
		var match func(s, p string) bool
		switch crit {
		case "user":
			s, match = conn["user"], func(s, p string) bool { ok, _ := path.Match(p, s); return ok }
		case "address", "host":
			s, match = conn["addr"], addrMatch
		default:
			return false
		}
		found := false
		for _, p := range strings.Split(list, ",") {
			neg := strings.HasPrefix(p, "!")
			if match(s, strings.TrimPrefix(p, "!")) {
				if neg {
					return false
				}
				found = true
			}
		}
		if !found {
			return false
		}
	}
	return len(f) > 1
}

// addrMatch matches an address against a CIDR or a wildcard pattern.
func addrMatch(s, p string) bool {
	if pr, err := netip.ParsePrefix(p); err == nil {
		a, err := netip.ParseAddr(s)
		return err == nil && pr.Contains(a)
	}
	ok, _ := path.Match(p, s)
	return ok
}

// ss is ss -Htln's output for the sockets given.
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
	var b strings.Builder
	b.WriteString("Status: active\nLogging: on (low)\nDefault: deny (incoming), allow (outgoing), disabled (routed)\nNew profiles: skip\n")
	fmt.Fprintf(&b, "\n%-26s %-12s%s\n%-26s %-12s%s\n", "To", "Action", "From", "--", "------", "----")
	for _, sfx := range []string{"", " (v6)"} {
		for _, r := range v.rules {
			from := "Anywhere" + sfx
			if r.src != "" {
				if sfx != "" {
					continue
				}
				from = r.src
			}
			to := fmt.Sprintf("%d/tcp%s", r.port, sfx)
			if r.iface != "" {
				to = fmt.Sprintf("%d/tcp on %s%s", r.port, r.iface, sfx)
			}
			fmt.Fprintf(&b, "%-26s %-12s%-26s\n", to, "ALLOW IN", from)
		}
	}
	return b.String()
}

// saveRules writes ufw's files for the rules.
func (v *vm) saveRules() {
	v.f.AddFile(ufw.ConfFile, []byte("ENABLED=yes\nLOGLEVEL=low\n"), 0o644, 0, 0)
	for _, f := range []struct{ path, any string }{{ufw.UserRules, "0.0.0.0/0"}, {ufw.User6Rules, "::/0"}} {
		var b strings.Builder
		b.WriteString("*filter\n:ufw-user-input - [0:0]\n### RULES ###\n")
		for _, r := range v.rules {
			src := f.any
			if r.src != "" {
				if f.any == "::/0" {
					continue
				}
				src = r.src
			}
			dir := "in"
			if r.iface != "" {
				dir += "_" + r.iface
			}
			fmt.Fprintf(&b, "\n### tuple ### allow tcp %d %s any %s %s\n-A ufw-user-input -p tcp --dport %d -j ACCEPT\n", r.port, f.any, src, dir, r.port)
		}
		b.WriteString("\n### END RULES ###\nCOMMIT\n")
		v.f.AddFile(f.path, []byte(b.String()), 0o640, 0, 0)
	}
}

// run answers the commands the readers, the planner, the engine and the
// rollback run.
func (v *vm) run(argv []string, _ []byte) hostfake.Response {
	cmd := strings.Join(argv, " ")
	switch path.Base(argv[0]) {
	case "sshd":
		return v.sshd(argv)
	case "journalctl":
		if cmd == "journalctl -q --no-pager -b -o short-iso -u ssh -u sshd" {
			return hostfake.Response{Stdout: v.log}
		}
	case "ss":
		return hostfake.Response{Stdout: ss(v.bound)}
	case "stat": // -c %C -- <path>: hardn's binary, bin_t unless v.hardnType
		t := v.hardnType
		if t == "" {
			t = "bin_t"
		}
		return hostfake.Response{Stdout: "system_u:object_r:" + t + ":s0\n"}
	case "semanage":
		return v.semanage(argv)
	case "firewall-cmd":
		if v.fw == nil {
			return hostfake.Response{Err: host.ErrNotFound}
		}
		return v.fw.run(argv)
	case "firewall-offline-cmd":
		if v.fw == nil {
			return hostfake.Response{Err: host.ErrNotFound}
		}
		return v.fw.offline(argv)
	case "matchpathcon": // -V: each path verified, but those in v.mislabeled
		var b strings.Builder
		for _, p := range argv[2:] {
			v.labelled = append(v.labelled, p)
			if want, ok := v.mislabeled[p]; ok {
				fmt.Fprintf(&b, "%s has context unconfined_u:object_r:admin_home_t:s0, should be %s\n", p, want)
				continue
			}
			fmt.Fprintf(&b, "%s verified.\n", p)
		}
		return hostfake.Response{Stdout: b.String()}
	case "visudo":
		return hostfake.Response{Stdout: sudoers.MainConfig + ": parsed OK\n"}
	case "systemd-run", "systemd-analyze", "sh":
		return hostfake.Response{}
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

// sshd answers sshd -t (every file accepted) and sshd -T, with -C over
// the Match blocks it matches; sshd.c's fatal() where /run/sshd is not
// there (reload.md §2.4).
func (v *vm) sshd(argv []string) hostfake.Response {
	if v.sshdFail != "" {
		return hostfake.Response{Stderr: v.sshdFail, ExitCode: 255}
	}
	if _, ok := v.f.Entry("/run/sshd"); !ok {
		return hostfake.Response{Stderr: "Missing privilege separation directory: /run/sshd\r\n", ExitCode: 255}
	}
	main, drops := v.files(argv[slices.Index(argv, "-f")+1])
	if slices.Contains(argv, "-t") {
		return hostfake.Response{}
	}
	if i := slices.Index(argv, "-C"); i >= 0 {
		main = matched(main, drops, argv[i+1])
	}
	out, _ := sshdT(main, drops, v.noPAM, v.noRSASize)
	return hostfake.Response{Stdout: out}
}

// listening is the listeners the installed files configure.
func (v *vm) listening() []string {
	main, drops := v.files(sshd.MainConfig)
	_, ls := sshdT(main, drops, v.noPAM, v.noRSASize)
	return ls
}

func (v *vm) systemctl(argv []string) (hostfake.Response, bool) {
	state := func(on bool, yes, no string, code int) hostfake.Response {
		if on {
			return hostfake.Response{Stdout: yes + "\n"}
		}
		return hostfake.Response{Stdout: no + "\n", ExitCode: code}
	}
	switch cmd := strings.Join(argv, " "); {
	case v.fw == nil && cmd == "systemctl is-enabled firewalld.service":
		return hostfake.Response{Stdout: "not-found\n", ExitCode: 4}, true
	case v.fw == nil && cmd == "systemctl is-active firewalld.service":
		return hostfake.Response{Stdout: "inactive\n", ExitCode: 3}, true
	case cmd == "systemctl is-enabled firewalld.service":
		return state(v.fw.enabled || !v.fw.stopped, "enabled", "disabled", 1), true
	case cmd == "systemctl is-active firewalld.service":
		return state(!v.fw.stopped, "active", "inactive", 3), true
	case v.sshdUnits && (cmd == "systemctl is-enabled ssh.service" || cmd == "systemctl is-enabled ssh.socket"):
		return hostfake.Response{Stdout: "not-found\n", ExitCode: 4}, true
	case v.sshdUnits && (cmd == "systemctl is-active ssh.service" || cmd == "systemctl is-active ssh.socket"):
		return hostfake.Response{Stdout: "inactive\n", ExitCode: 4}, true
	case v.sshdUnits && cmd == "systemctl is-active sshd.service":
		return state(true, "active", "inactive", 3), true
	case v.sshdUnits && cmd == "systemctl is-enabled sshd.service":
		return state(true, "enabled", "disabled", 1), true
	case v.sshdUnits && cmd == "systemctl is-active sshd.socket":
		return hostfake.Response{Stdout: "inactive\n", ExitCode: 3}, true
	case v.sshdUnits && cmd == "systemctl is-enabled sshd.socket":
		return hostfake.Response{Stdout: "disabled\n", ExitCode: 1}, true
	case v.sshdUnits && (cmd == "systemctl reload sshd.service" || cmd == "systemctl start sshd.service"):
		v.reloads++
		v.bound = v.listening()
		return hostfake.Response{}, true
	case v.sshdUnits && strings.HasSuffix(cmd, " ssh.service"):
		v.t.Errorf("ran %q on a host whose unit is sshd.service", cmd)
		return hostfake.Response{ExitCode: 5, Stderr: "Unit ssh.service not found.\n"}, true
	case cmd == "systemctl is-active ssh.service":
		return state(true, "active", "inactive", 3), true
	case cmd == "systemctl is-enabled ssh.service":
		return state(!v.socket, "enabled", "disabled", 1), true
	case cmd == "systemctl is-active ssh.socket":
		return state(v.socket, "active", "inactive", 3), true
	case cmd == "systemctl is-enabled ssh.socket":
		return state(v.socket, "enabled", "disabled", 1), true
	case cmd == "systemctl reload ssh.service", cmd == "systemctl restart ssh.service":
		if !v.socket {
			v.bound = v.listening()
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
	case cmd == "systemctl --no-reload enable "+revert.BootUnitName+".service":
		v.f.AddSymlink(revert.SystemdWants+"/"+revert.BootUnitName+".service", revert.SystemdUnitDir+"/"+revert.BootUnitName+".service")
		return hostfake.Response{}, true
	case strings.Contains(cmd, "hardn"):
		return hostfake.Response{}, true
	}
	return hostfake.Response{}, false
}

func (v *vm) ufw(argv []string) hostfake.Response {
	cmd := strings.Join(argv, " ")
	if v.noUFW {
		return hostfake.Response{Err: fmt.Errorf("host: run ufw: %w", host.ErrNotFound), ExitCode: -1}
	}
	if cmd == "ufw status verbose" {
		return hostfake.Response{Stdout: v.ufwStatus()}
	}
	spec := argv[len(argv)-1]
	n, err := strconv.Atoi(strings.TrimSuffix(spec, "/tcp"))
	switch {
	case err != nil:
	case len(argv) == 3 && argv[1] == "allow":
		v.rules = append(v.rules, ufwRule{port: uint16(n)})
		v.saveRules()
		return hostfake.Response{Stdout: "Rule added\nRule added (v6)\n"}
	case len(argv) == 4 && argv[1] == "delete" && argv[2] == "allow":
		v.rules = slices.DeleteFunc(v.rules, func(r ufwRule) bool { return r.port == uint16(n) && r.src == "" })
		v.saveRules()
		return hostfake.Response{Stdout: "Rule deleted\nRule deleted (v6)\n"}
	}
	v.t.Errorf("unexpected command %q", cmd)
	return hostfake.Response{Err: hostfake.ErrUnscripted, ExitCode: -1}
}

// sys is the fake with sudo setuid root, which hostfake has no bit for.
func (v *vm) sys() host.System { return setuidSys{v.f, v} }

// setuidSys shows sudo's setuid bit, and fails v's noFollowErr reads.
type setuidSys struct {
	*hostfake.System
	v *vm
}

func (s setuidSys) ReadFileNoFollow(ctx context.Context, name string) ([]byte, fs.FileInfo, error) {
	if err := s.v.noFollowErr[name]; err != nil {
		return nil, nil, err
	}
	return s.System.ReadFileNoFollow(ctx, name)
}

func (s setuidSys) Stat(ctx context.Context, name string) (fs.FileInfo, error) {
	fi, err := s.System.Stat(ctx, name)
	if err == nil && name == sudoPath {
		fi = setuidInfo{fi}
	}
	return fi, err
}

type setuidInfo struct{ fs.FileInfo }

func (i setuidInfo) Mode() fs.FileMode { return i.FileInfo.Mode() | fs.ModeSetuid }

// engine is the engine on v as cmd/hardn builds it: its clock, the facts,
// the backups, the session.
func (v *vm) engine() *engine.Engine {
	return engine.New(v.sys(), engine.Options{
		Revert:  revert.Options{Now: func() time.Time { return now }, Hardn: hardnBinary},
		Detect:  func(context.Context, host.System) (detect.Facts, error) { return v.facts, nil },
		Hold:    func() func() { return func() {} },
		PID:     5000,
		Session: v.session,
		Backups: backup.Settings{Path: backup.DefaultPath, Enabled: true},
	})
}

// env is getenv over vars.
func env(vars map[string]string) func(string) string { return func(k string) string { return vars[k] } }

// sudoGeorge is hardn run through sudo by george.
var sudoGeorge = env(map[string]string{"SUDO_USER": "george", "USER": "root"})
