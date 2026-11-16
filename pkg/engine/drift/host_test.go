package drift_test

// A model of a host for the drift check's tests, on the recording fake: a
// Debian 13 VM as hardn leaves it (ufw on and denying, 22 let in; root
// login key-only, passwords off; george an admin with one key; automatic
// updates on; chrony on its package's pool), and a Rocky Linux 10 VM on
// firewalld. A test changes the
// model as a person would change the host, and reads it again.

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"io/fs"
	"path"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/abbott/hardn/pkg/engine/drift"
	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/accounts"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/hostfake"
	"github.com/abbott/hardn/pkg/host/sshd"
	"github.com/abbott/hardn/pkg/host/sudoers"
	"github.com/abbott/hardn/pkg/host/timesync"
	"github.com/abbott/hardn/pkg/host/timesync/timefake"
	"github.com/abbott/hardn/pkg/host/ufw"
)

// syntheticHash is a password field that is no one's hash.
const syntheticHash = "$y$synthetic$not-a-hash"

// simUser is one account of the model.
type simUser struct {
	name   string
	uid    int
	groups []string
	pw     string // its field in /etc/shadow
	keys   []byte // a synthetic key for each seed
}

// simHost is the model.
type simHost struct {
	*hostfake.System
	t     *testing.T
	facts detect.Facts

	mu    sync.Mutex
	users []simUser
	// sshd's settings, written to 00-hardn.conf and printed by sshd -T.
	rootLogin, passwords string
	port                 int
	// ufw: its chains loaded, its saved allow rules ("8080/tcp", or
	// "22/tcp 192.0.2.7" from a source), its service at boot.
	ufwOn   bool
	rules   []string
	ufwBoot bool
	// firewalld, on Rocky Linux: the default zone's ports.
	zonePorts []string
	// listen are the sockets ss shows: "0.0.0.0:22", "127.0.0.1:5432".
	listen      []string
	noSS        bool
	nameservers []string
	updates     bool
	noSudoers   bool // /etc/sudoers is not readable
	// forwards are rsyslog's lines in /etc/rsyslog.d/50-remote.conf, each
	// "@@192.0.2.51" or "@192.0.2.52:1514"; noRsyslog: it is not installed.
	forwards  []string
	noRsyslog bool
	// time is the time service, and ntp its files, by name; a file taken
	// out of ntp is removed.
	time       *timefake.Sim
	ntp        map[string]string
	ntpWritten []string
	unexpected []string
}

// synthKey is a synthetic ED25519 public key, its 32 bytes seed.
func synthKey(seed byte, comment string) string {
	blob := binary.BigEndian.AppendUint32(nil, 11)
	blob = append(blob, "ssh-ed25519"...)
	blob = binary.BigEndian.AppendUint32(blob, 32)
	for range 32 {
		blob = append(blob, seed)
	}
	return "ssh-ed25519 " + base64.StdEncoding.EncodeToString(blob) + " " + comment
}

// newHost is the model of distro, "debian" or "rocky".
func newHost(t *testing.T, distro string) *simHost {
	t.Helper()
	h := &simHost{System: hostfake.New(host.Options{}), t: t,
		users:     []simUser{{name: "george", uid: 1000, groups: []string{"sudo"}, pw: "!", keys: []byte{1}}, {name: "alice", uid: 1001, pw: syntheticHash}},
		rootLogin: "prohibit-password", passwords: "no", port: 22,
		ufwOn: true, rules: []string{"22/tcp"}, ufwBoot: true,
		zonePorts: []string{"8443/tcp"},
		listen:    []string{"0.0.0.0:22", "[::]:22", "127.0.0.1:5432"}, nameservers: []string{"192.0.2.53", "192.0.2.54"}, updates: true,
	}
	h.facts = detect.Facts{
		OS:        detect.OSRelease{ID: "debian", VersionID: "13", VersionCodename: "trixie"},
		Platform:  detect.Platform{Distro: detect.DistroDebian, Version: "13", Support: detect.Supported},
		Init:      detect.Init{Kind: detect.Systemd, Running: detect.Yes, Source: detect.SystemdRunDir},
		SSH:       detect.SSH{Mode: detect.SSHService, Generator: detect.No, Service: detect.UnitState{Enabled: "enabled", Active: "active"}, Socket: detect.UnitState{Enabled: "disabled", Active: "inactive"}},
		Proxmox:   detect.Proxmox{Installed: detect.No, ClusterFS: detect.No, EtcPVE: detect.No},
		Container: detect.Container{Detected: detect.No},
		Elevation: detect.Elevation{Sudo: detect.Tool{Present: detect.Yes, Path: "/usr/bin/sudo"}, SudoFlavor: detect.SudoOriginal, Doas: detect.Tool{Present: detect.No}},
	}
	h.time = &timefake.Sim{Service: "chrony", Synced: "2.debian.pool.ntp.org", Packages: map[string]bool{"chrony": true}}
	h.ntp = map[string]string{timesync.ChronyConfDebian: timefake.DebianConf}
	if distro == "rocky" {
		h.facts.OS = detect.OSRelease{ID: "rocky", IDLike: []string{"rhel", "centos", "fedora"}, VersionID: "10.2"}
		h.facts.Platform = detect.Platform{Distro: detect.DistroRocky, Version: "10", Support: detect.Supported}
		h.users[0].groups = []string{"wheel"}
		h.time.Service, h.time.Synced = "chronyd", "2.rocky.pool.ntp.org"
		h.ntp = map[string]string{timesync.ChronyConfEL: timefake.ELConf}
	}
	h.time.Read = func(name string) ([]byte, error) { return h.ReadFile(context.Background(), name) }
	for _, d := range []string{"/etc", "/bin", "/usr/bin", "/usr/sbin", "/home", "/var/lib", "/run", "/run/sshd", "/etc/ssh", sshd.DropInDir, "/etc/ufw", "/etc/default"} {
		h.AddDir(d, 0o755, 0, 0)
	}
	for _, sh := range []string{"/bin/bash", "/usr/sbin/nologin"} {
		h.AddFile(sh, []byte("ELF"), 0o755, 0, 0)
	}
	h.AddFile("/usr/bin/sudo", []byte("ELF"), 0o755, 0, 0)
	h.AddDir(sudoers.DropInDir, 0o755, 0, 0)
	h.AddDir("/root/.ssh", 0o700, 0, 0)
	h.AddFile("/root/.ssh/authorized_keys", []byte(synthKey(0x72, "root@admin-01")+"\n"), 0o600, 0, 0)
	h.AddDir(ufw.KernelIPv6, 0o555, 0, 0)
	h.sync()
	h.Handler = h.answer
	t.Cleanup(func() {
		if len(h.unexpected) > 0 {
			t.Errorf("commands the model does not answer: %q", h.unexpected)
		}
	})
	return h
}

// Stat is the fake's, with the setuid bit on sudo: hostfake keeps none.
func (h *simHost) Stat(ctx context.Context, name string) (fs.FileInfo, error) {
	fi, err := h.System.Stat(ctx, name)
	if err == nil && name == "/usr/bin/sudo" {
		return setuidInfo{fi}, nil
	}
	return fi, err
}

type setuidInfo struct{ fs.FileInfo }

func (i setuidInfo) Mode() fs.FileMode { return i.FileInfo.Mode() | fs.ModeSetuid }

// sync writes the model's files: the accounts and their keys, sudoers,
// sshd's files, ufw's, the time service's, resolv.conf.
func (h *simHost) sync() {
	admin := "sudo"
	if h.facts.Platform.Distro.EL() {
		admin = "wheel"
	}
	passwd := "root:x:0:0:root:/root:/bin/bash\nsshd:x:100:65534::/run/sshd:/usr/sbin/nologin\n"
	shadow := "root:" + syntheticHash + ":20000:0:99999:7:::\nsshd:*:20000:0:99999:7:::\n"
	group := "root:x:0:\n"
	var members []string
	for _, u := range h.users {
		passwd += fmt.Sprintf("%s:x:%d:%d::/home/%s:/bin/bash\n", u.name, u.uid, u.uid, u.name)
		shadow += u.name + ":" + u.pw + ":20000:0:99999:7:::\n"
		if u.uid != 0 {
			group += fmt.Sprintf("%s:x:%d:\n", u.name, u.uid)
		}
		if slices.Contains(u.groups, admin) {
			members = append(members, u.name)
		}
		h.AddDir("/home/"+u.name, 0o700, u.uid, u.uid)
		h.AddDir("/home/"+u.name+"/.ssh", 0o700, u.uid, u.uid)
		var lines string
		for i, s := range u.keys {
			lines += synthKey(s, fmt.Sprintf("%s@laptop-%d", u.name, i+1)) + "\n"
		}
		h.AddFile("/home/"+u.name+"/.ssh/authorized_keys", []byte(lines), 0o600, u.uid, u.uid)
	}
	group += admin + ":x:27:" + strings.Join(members, ",") + "\n"
	h.AddFile(accounts.PasswdFile, []byte(passwd), 0o644, 0, 0)
	h.AddFile(accounts.GroupFile, []byte(group), 0o644, 0, 0)
	h.AddFile(accounts.ShadowFile, []byte(shadow), 0o640, 0, 42)
	if h.noSudoers {
		_ = h.Remove(context.Background(), sudoers.MainConfig)
	} else {
		h.AddFile(sudoers.MainConfig, []byte("Defaults env_reset\nroot ALL=(ALL:ALL) ALL\n%"+admin+" ALL=(ALL:ALL) ALL\n@includedir /etc/sudoers.d\n"), 0o440, 0, 0)
	}

	h.AddFile(sshd.MainConfig, []byte(sshd.IncludeLine+"\n\nUsePAM yes\n"), 0o644, 0, 0)
	h.AddFile(sshd.HardnDropIn, []byte(fmt.Sprintf("Port %d\nPermitRootLogin %s\nPasswordAuthentication %s\n", h.port, h.rootLogin, h.passwords)), 0o644, 0, 0)

	enabled := map[bool]string{true: "yes", false: "no"}[h.ufwOn]
	h.AddFile(ufw.DefaultsFile, []byte("IPV6=yes\nDEFAULT_INPUT_POLICY=\"DROP\"\nDEFAULT_OUTPUT_POLICY=\"ACCEPT\"\n"), 0o644, 0, 0)
	h.AddFile(ufw.ConfFile, []byte("ENABLED="+enabled+"\nLOGLEVEL=low\n"), 0o644, 0, 0)
	for _, f := range []struct {
		path, any string
		v6        bool
	}{{ufw.UserRules, "0.0.0.0/0", false}, {ufw.User6Rules, "::/0", true}} {
		var b strings.Builder
		b.WriteString("*filter\n### RULES ###\n")
		for _, r := range h.rules {
			spec, src, _ := strings.Cut(r, " ")
			port, proto, _ := strings.Cut(spec, "/")
			switch {
			case src != "" && f.v6:
				continue
			case src == "":
				src = f.any
			}
			fmt.Fprintf(&b, "\n### tuple ### allow %s %s %s any %s in\n", proto, port, f.any, src)
		}
		b.WriteString("\n### END RULES ###\nCOMMIT\n")
		h.AddFile(f.path, []byte(b.String()), 0o640, 0, 0)
	}

	var fw strings.Builder
	for _, f := range h.forwards {
		fw.WriteString("*.* " + f + "\n")
	}
	h.AddDir("/etc/rsyslog.d", 0o755, 0, 0)
	h.AddFile("/etc/rsyslog.d/50-remote.conf", []byte(fw.String()), 0o644, 0, 0)

	for _, name := range h.ntpWritten {
		if _, ok := h.ntp[name]; !ok {
			_ = h.Remove(context.Background(), name)
		}
	}
	h.ntpWritten = nil
	for name, text := range h.ntp {
		h.AddFile(name, []byte(text), 0o644, 0, 0)
		h.ntpWritten = append(h.ntpWritten, name)
	}

	var rc strings.Builder
	for _, ns := range h.nameservers {
		rc.WriteString("nameserver " + ns + "\n")
	}
	h.AddFile("/etc/resolv.conf", []byte(rc.String()), 0o644, 0, 0)
}

// ufwStatus is ufw status verbose's output for the model.
func (h *simHost) ufwStatus() string {
	if !h.ufwOn {
		return "Status: inactive\n"
	}
	var b strings.Builder
	b.WriteString("Status: active\nLogging: on (low)\nDefault: deny (incoming), allow (outgoing), disabled (routed)\nNew profiles: skip\n")
	fmt.Fprintf(&b, "\n%-26s %-12s%s\n%-26s %-12s%s\n", "To", "Action", "From", "--", "------", "----")
	for _, v6 := range []bool{false, true} {
		for _, r := range h.rules {
			to, from, _ := strings.Cut(r, " ")
			switch {
			case from != "" && v6:
				continue
			case from == "" && v6:
				to, from = to+" (v6)", "Anywhere (v6)"
			case from == "":
				from = "Anywhere"
			}
			fmt.Fprintf(&b, "%-26s %-12s%s\n", to, "ALLOW IN", from)
		}
	}
	return b.String()
}

// answer is the model's commands. One it does not know fails the test.
func (h *simHost) answer(argv []string, _ []byte) hostfake.Response {
	h.mu.Lock()
	defer h.mu.Unlock()
	cmd := strings.Join(argv, " ")
	el := h.facts.Platform.Distro.EL()
	notFound := hostfake.Response{Err: host.ErrNotFound, ExitCode: -1}
	state := func(on bool, yes, no string, code int) hostfake.Response {
		if on {
			return hostfake.Response{Stdout: yes + "\n"}
		}
		return hostfake.Response{Stdout: no + "\n", ExitCode: code}
	}
	switch name := path.Base(argv[0]); {
	case name == "sshd" && slices.Contains(argv, "-T"):
		return hostfake.Response{Stdout: fmt.Sprintf("port %d\nlistenaddress [::]:%d\nlistenaddress 0.0.0.0:%d\npermitrootlogin %s\npubkeyauthentication yes\n"+
			"passwordauthentication %s\nkbdinteractiveauthentication no\nusepam yes\nstrictmodes yes\nauthorizedkeysfile .ssh/authorized_keys .ssh/authorized_keys2\n",
			h.port, h.port, h.port, h.rootLogin, h.passwords)}
	case name == "sshd":
		return hostfake.Response{}
	case name == "visudo":
		return hostfake.Response{Stdout: sudoers.MainConfig + ": parsed OK\n"}
	case name == "ss" && h.noSS:
		return notFound
	case cmd == "ss -Htlnp":
		var b strings.Builder
		for _, s := range h.listen {
			peer := "0.0.0.0:*"
			if strings.HasPrefix(s, "[") {
				peer = "[::]:*"
			}
			fmt.Fprintf(&b, "LISTEN 0      4096   %20s   %s\n", s, peer)
		}
		return hostfake.Response{Stdout: b.String()}
	case cmd == "ss -Hulnp":
		return hostfake.Response{}
	case name == "ufw" && el:
		return notFound
	case cmd == "ufw status verbose":
		return hostfake.Response{Stdout: h.ufwStatus()}
	case cmd == "systemctl is-enabled ufw.service":
		return state(h.ufwBoot, "enabled", "disabled", 1)
	case cmd == "systemctl is-active firewalld.service":
		return state(el, "active", "inactive", 3)
	case cmd == "systemctl is-enabled firewalld.service":
		return state(el, "enabled", "not-found", 4)
	case cmd == "firewall-cmd --state":
		return hostfake.Response{Stdout: "running\n"}
	case cmd == "firewall-cmd --get-default-zone":
		return hostfake.Response{Stdout: "public\n"}
	case cmd == "firewall-cmd --get-active-zones":
		return hostfake.Response{Stdout: "public (default)\n  interfaces: eth0\n"}
	case cmd == "firewall-cmd --list-all-zones":
		return hostfake.Response{Stdout: "public (default, active)\n  target: default\n  interfaces: eth0\n  sources: \n  services: ssh\n  ports: " +
			strings.Join(h.zonePorts, " ") + "\n  rich rules: \n"}
	case cmd == "firewall-cmd --permanent --service=ssh --get-ports":
		return hostfake.Response{Stdout: "22/tcp\n"}
	case cmd == "systemctl is-active systemd-resolved.service":
		return hostfake.Response{Stdout: "inactive\n", ExitCode: 3}
	case cmd == "rpm -q dnf-automatic": // not installed, as on the Rocky VM
		return hostfake.Response{Stdout: "package dnf-automatic is not installed\n", ExitCode: 1}
	case name == "systemctl" && len(argv) == 3 && strings.HasPrefix(argv[2], "dnf-automatic"):
		if argv[1] == "is-active" {
			return hostfake.Response{Stdout: "inactive\n", ExitCode: 3}
		}
		return hostfake.Response{Stdout: "disabled\n", ExitCode: 1}
	case strings.HasPrefix(cmd, "apt-config dump"):
		on := map[bool]string{true: "1", false: "0"}[h.updates]
		return hostfake.Response{Stdout: "APT::Periodic \"\";\nAPT::Periodic::Update-Package-Lists \"" + on + "\";\nAPT::Periodic::Unattended-Upgrade \"" + on + "\";\n" +
			"Unattended-Upgrade::Origins-Pattern \"\";\nUnattended-Upgrade::Origins-Pattern:: \"origin=Debian,codename=${distro_codename},label=Debian-Security\";\n"}
	case name == "dpkg-query" && argv[len(argv)-1] == "rsyslog" && h.noRsyslog:
		return hostfake.Response{Stderr: "dpkg-query: no packages found matching rsyslog\n", ExitCode: 1}
	case name == "dpkg-query" && argv[len(argv)-1] == "rsyslog":
		return hostfake.Response{Stdout: "install ok installed\t8.2504.0\n"}
	case cmd == "rpm -q rsyslog":
		return hostfake.Response{Stdout: "rsyslog-8.2412.0-1.el10.x86_64\n"}
	case name == "dpkg-query" && argv[len(argv)-1] == "unattended-upgrades":
		return hostfake.Response{Stdout: "install ok installed\t2.12\n"}
	case name == "systemctl" && len(argv) == 3 && strings.HasPrefix(argv[2], "apt-daily"):
		if argv[1] == "is-active" {
			return hostfake.Response{Stdout: "active\n"}
		}
		if strings.HasSuffix(argv[2], ".service") {
			return hostfake.Response{Stdout: "static\n"}
		}
		return hostfake.Response{Stdout: "enabled\n"}
	}
	if r, ok := h.time.Answer(argv); ok {
		return hostfake.Response{Stdout: r.Stdout, Stderr: r.Stderr, ExitCode: r.ExitCode}
	}
	h.unexpected = append(h.unexpected, cmd)
	return hostfake.Response{Err: hostfake.ErrUnscripted, ExitCode: -1}
}

// change changes the model under its lock and writes its files again.
func (h *simHost) change(f func(*simHost)) {
	h.mu.Lock()
	f(h)
	h.mu.Unlock()
	h.sync()
}

// read is drift.Read of the model.
func (h *simHost) read() drift.Facts {
	h.t.Helper()
	facts, err := drift.Read(context.Background(), h, h.facts)
	if err != nil {
		h.t.Fatal(err)
	}
	return facts
}
