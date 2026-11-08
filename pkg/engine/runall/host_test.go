package runall_test

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"io/fs"
	"net/netip"
	"path"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/abbott/hardn/pkg/engine/header"
	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/accounts"
	"github.com/abbott/hardn/pkg/host/alpine"
	"github.com/abbott/hardn/pkg/host/authkeys"
	"github.com/abbott/hardn/pkg/host/detect"
	hostdns "github.com/abbott/hardn/pkg/host/dns"
	"github.com/abbott/hardn/pkg/host/hostfake"
	"github.com/abbott/hardn/pkg/host/reload"
	"github.com/abbott/hardn/pkg/host/revert"
	"github.com/abbott/hardn/pkg/host/sshd"
	"github.com/abbott/hardn/pkg/host/sshd/migrate"
	"github.com/abbott/hardn/pkg/host/sudoers"
	"github.com/abbott/hardn/pkg/host/ufw"
)

// Run All's hosts: hostfake, in the shapes the three VMs showed on
// 2026-11-03 (vm-runs/s6/00-inspect-*.log), as the gallery's Run All hosts
// are (internal/tools/uigallery/runall.go over sshhost.go, fakehost.go and
// usershost.go). Copied from pkg/screens/ssh's host_test.go, the same
// gallery models already copied, as a test cannot import the gallery; ufw
// inactive (the gallery's ufwAsShipped) and the resolver's file added.
// Synthetic values only: george the login user, alice a second one, carol
// a new one, web-01, 192.0.2.x, 2001:db8::/32, 2230. What the fake answers
// beyond its files is a model, each answer's source beside it: sshd -T and
// -T -C over the files (sshd.ReadOrder; the Match blocks applied for -C),
// sshd's log of the session's login, ss, ufw, systemctl and OpenRC as the
// engine runs them.

const (
	hardnBinary = "/usr/local/bin/hardn"
	hostName    = "web-01"
	// syntheticHash is a password set: no hash of anything.
	syntheticHash = "$y$synthetic$not-a-hash"
	// applyConn is george's connection to port 22 that applies a change,
	// and confirmConn one to 2230.
	applyConn   = "192.0.2.10 50022 192.0.2.20 22"
	confirmConn = "192.0.2.10 50123 192.0.2.20 2230"
)

// The sessions: george's over SSH (applyConn), the same on 2230
// (confirmConn).
var (
	applySession   = revert.Session{Remote: true, Conn: applyConn, Source: "SSH_CONNECTION"}
	confirmSession = revert.Session{Remote: true, Conn: confirmConn, Source: "SSH_CONNECTION"}
)

// platform is what a host's release ships, as detect reads it.
type platform struct {
	facts   detect.Facts
	openRC  bool // OpenRC, doas, /bin/ash, sshd built without PAM
	socket  bool // ssh.socket holds the listeners
	noUFW   bool
	sudo    bool
	home    fs.FileMode
	sudoers string // /etc/sudoers, "" for none
	// rootLogin and kbdInteractive are what sshd -T prints for
	// PermitRootLogin and KbdInteractiveAuthentication when the files set
	// none ("" is no), as each VM's sshd -T printed them.
	rootLogin, kbdInteractive string
}

// The platforms: Debian 13, Ubuntu 26.04 (ssh.socket, sudo-rs, HOME_MODE
// 0750), Alpine 3.23 (OpenRC, doas, no PAM, no ufw), Proxmox VE 9 (no sudo,
// no ufw).
func debian13() platform {
	return platform{sudo: true, home: 0o700, rootLogin: "without-password",
		sudoers: "Defaults env_reset\nroot ALL=(ALL:ALL) ALL\n%sudo ALL=(ALL:ALL) ALL\n@includedir /etc/sudoers.d\n",
		facts: detect.Facts{
			Platform:  detect.Platform{Distro: detect.DistroDebian, Version: "13", Support: detect.Supported},
			Init:      detect.Init{Kind: detect.Systemd, Running: detect.Yes, Source: detect.SystemdRunDir},
			SSH:       detect.SSH{Mode: detect.SSHService, Generator: detect.No, Service: detect.UnitState{Enabled: "enabled", Active: "active"}, Socket: detect.UnitState{Enabled: "disabled", Active: "inactive"}},
			Proxmox:   detect.Proxmox{Installed: detect.No, ClusterFS: detect.No, EtcPVE: detect.No},
			Container: detect.Container{Detected: detect.No},
			Elevation: detect.Elevation{Sudo: detect.Tool{Present: detect.Yes, Path: "/usr/bin/sudo"}, SudoFlavor: detect.SudoOriginal, Doas: detect.Tool{Present: detect.No}},
		}}
}

func ubuntu2604() platform {
	p := debian13()
	p.socket, p.home, p.rootLogin = true, 0o750, "prohibit-password"
	p.sudoers = "Defaults env_reset\nroot ALL=(ALL:ALL) ALL\n%admin ALL=(ALL) ALL\n%sudo ALL=(ALL:ALL) ALL\n@includedir /etc/sudoers.d\n"
	p.facts.Platform = detect.Platform{Distro: detect.DistroUbuntu, Version: "26.04", Support: detect.Supported}
	p.facts.SSH = detect.SSH{Mode: detect.SSHSocket, Generator: detect.Yes, GeneratorPath: detect.SocketGenerator,
		Service: detect.UnitState{Enabled: "disabled", Active: "active"}, Socket: detect.UnitState{Enabled: "enabled", Active: "active"}}
	p.facts.Elevation.SudoFlavor = detect.SudoRS
	return p
}

func alpine323() platform {
	return platform{openRC: true, noUFW: true, home: 0o755, rootLogin: "prohibit-password", kbdInteractive: "yes",
		sudoers: "root ALL=(ALL:ALL) ALL\n#includedir /etc/sudoers.d\n",
		facts: detect.Facts{
			Platform:  detect.Platform{Distro: detect.DistroAlpine, Version: "3.23", Support: detect.Supported},
			Init:      detect.Init{Kind: detect.OpenRC, Running: detect.Yes},
			SSH:       detect.SSH{Mode: detect.SSHOpenRC, OpenRC: detect.OpenRCService{Started: detect.Yes, InRunlevel: detect.Yes}},
			Proxmox:   detect.Proxmox{Installed: detect.No, ClusterFS: detect.No, EtcPVE: detect.No},
			Container: detect.Container{Detected: detect.No},
			Elevation: detect.Elevation{Sudo: detect.Tool{Present: detect.No}, Doas: detect.Tool{Present: detect.Yes, Path: "/usr/bin/doas"}},
		}}
}

func pve9() platform {
	p := debian13()
	p.sudo, p.sudoers, p.noUFW = false, "", true
	p.facts.Platform = detect.Platform{Distro: detect.DistroProxmox, Version: "9", Support: detect.Supported}
	p.facts.Proxmox = detect.Proxmox{Installed: detect.Yes, Version: "9.0.10", ClusterFS: detect.Yes, EtcPVE: detect.Yes}
	p.facts.Elevation.Sudo = detect.Tool{Present: detect.No}
	return p
}

// simUser is one account a host has.
type simUser struct {
	name   string
	uid    int
	groups []string // the supplementary groups /etc/group lists it in
	// pw is its password field in /etc/shadow: "!" (the Debian and Ubuntu
	// VMs' login user), "*" (the Alpine VM's), or syntheticHash.
	pw    string
	shell string
	keys  []byte // a synthetic key per seed, in ~/.ssh/authorized_keys
}

// george is the VMs' login user: uid 1000 (1001 on Alpine), in sudo and
// adm (wheel and adm), no password, two keys.
func george(p platform) simUser {
	if p.openRC {
		return simUser{name: "george", uid: 1001, groups: []string{"wheel", "adm"}, pw: "*", shell: "/bin/ash", keys: []byte{1, 2}}
	}
	return simUser{name: "george", uid: 1000, groups: []string{"sudo", "adm"}, pw: "!", shell: "/bin/bash", keys: []byte{1, 2}}
}

// alice is the VMs' second user: uid 1001, a password, in sudo, no key.
func alice() simUser {
	return simUser{name: "alice", uid: 1001, groups: []string{"sudo"}, pw: syntheticHash, shell: "/bin/bash"}
}

// rule is one of ufw's allow rules: a port, from src ("" anywhere).
type rule struct {
	port uint16
	src  string
}

// sshHost is a host as a test runs the screen on it: the fake, its facts,
// hardn's environment and session, and the model's state of what it runs.
type sshHost struct {
	*hostfake.System
	plat platform
	env  map[string]string // hardn's environment: SUDO_USER or DOAS_USER, none at a root login
	sess revert.Session
	// log is sshd's log of the session's login: journalctl -u ssh's on
	// systemd; on OpenRC /var/log/messages holds it.
	log string

	mu         sync.Mutex
	rules      []rule   // ufw's allow rules, both families; nil with ufw active and none
	inactive   bool     // ufw installed and off, as shipped (the gallery's ufwAsShipped)
	aptBroken  bool     // apt's files have a syntax error, which every apt-config run prints
	bound      []string // what ss shows of sshd's (or ssh.socket's) sockets
	configured []string // ssh.socket's Listen, as the last daemon-reload set it
	unexpected []string // the commands the fake does not answer
	// openssh is the version of every openssh package in apk's installed
	// database, as on the Alpine VM, and offer the repository's; "" with
	// no database (unfixedSSH sets them).
	openssh, offer string
	pid            int // sshd's listener, in /run/sshd.pid
}

// opensshPackages are the Alpine VM's openssh packages
// (vm-runs/cve/run-alpine323.log).
var opensshPackages = []string{"openssh", "openssh-client-common", "openssh-client-default", "openssh-keygen", "openssh-server",
	"openssh-server-common", "openssh-server-common-openrc", "openssh-server-pam", "openssh-sftp-server"}

// unfixedSSH gives h, the Alpine VM, its openssh packages at 10.2_p1-r0,
// before CVE-2026-123158's fix (10.2_p1-r1, which the repository offers),
// sshd started, and its listener on its file.
func (h *sshHost) unfixedSSH() *sshHost {
	h.offer, h.pid = "10.2_p1-r1", 900
	h.plat.facts.OS = detect.OSRelease{ID: "alpine", VersionID: "3.23.6"}
	h.AddDir(detect.OpenRCStarted, 0o755, 0, 0)
	h.AddSymlink(detect.OpenRCStarted+"/sshd", detect.OpenRCSSHD)
	h.AddDir("/lib/apk/db", 0o755, 0, 0)
	h.writeDB("10.2_p1-r0")
	h.listener(false)
	return h
}

func (h *sshHost) writeDB(v string) {
	var b strings.Builder
	b.WriteString("P:musl\nV:1.2.5-r21\n\n")
	for _, n := range opensshPackages {
		b.WriteString("P:" + n + "\nV:" + v + "\n\n")
	}
	h.AddFile(alpine.InstalledDB, []byte(b.String()), 0o644, 0, 0)
	h.openssh = v
}

// listener writes sshd's pid file and its /proc/<pid>/exe, marked deleted
// where stale (the 2026-11-08 probe).
func (h *sshHost) listener(stale bool) {
	exe := reload.BinarySSHD
	if stale {
		exe += " (deleted)"
	}
	dir := "/proc/" + strconv.Itoa(h.pid)
	_ = h.RemoveAll(context.Background(), dir)
	h.AddDir(dir, 0o555, 0, 0)
	h.AddSymlink(dir+"/exe", exe)
	h.AddFile(alpine.SSHDPidFile, []byte(strconv.Itoa(h.pid)+"\n"), 0o644, 0, 0)
}

// opensshApk answers apk update and the upgrade of every openssh package,
// as apk-tools 3.0.8 did on the Alpine VM; nothing where the host has no
// openssh packages modelled.
func (h *sshHost) opensshApk(cmd string) (hostfake.Response, bool) {
	if h.openssh == "" {
		return hostfake.Response{}, false
	}
	names := strings.Join(opensshPackages, " ")
	listing := func() string {
		var b strings.Builder
		for i, n := range opensshPackages {
			fmt.Fprintf(&b, "(%d/%d) Upgrading %s (%s -> %s)\n", i+1, len(opensshPackages), n, h.openssh, h.offer)
		}
		return b.String() + "OK: 136.9 MiB in 207 packages\n"
	}
	switch cmd {
	case "apk update":
		return hostfake.Response{Stdout: "OK: 26371 distinct packages available\n"}, true
	case "apk upgrade --simulate " + names:
		return hostfake.Response{Stdout: listing()}, true
	case "apk upgrade " + names:
		out := listing()
		h.writeDB(h.offer)
		h.listener(true)
		return hostfake.Response{Stdout: out}, true
	}
	return hostfake.Response{}, false
}

// facts are detect's facts of the host.
func (h *sshHost) facts() detect.Facts { return h.plat.facts }

// getenv is hardn's environment on the host.
func (h *sshHost) getenv(k string) string { return h.env[k] }

// synthKey is a synthetic ED25519 public key, its 32 bytes seed: the
// OpenSSH wire form (RFC 8709 §4) of a key that is no one's.
func synthKey(seed byte, comment string) string {
	blob := binary.BigEndian.AppendUint32(nil, 11)
	blob = append(blob, "ssh-ed25519"...)
	blob = binary.BigEndian.AppendUint32(blob, 32)
	for range 32 {
		blob = append(blob, seed)
	}
	return "ssh-ed25519 " + base64.StdEncoding.EncodeToString(blob) + " " + comment
}

// usersOn is a host of platform p with the accounts root and us, written as
// the VMs' are (passwd, group, shadow; the keys 0600 in .ssh 0700), root's
// password syntheticHash ("!" on Alpine) with one key, cloud-init's sudoers
// rule for the first of us where cloud is set; sshd_config with the stock
// Include; ufw active allowing 22 where it is installed; what the engine
// needs to apply a plan; the hostname; george's session over SSH.
func usersOn(p platform, cloud bool, us ...simUser) *sshHost {
	h := &sshHost{System: hostfake.New(host.Options{}), plat: p, sess: applySession, rules: []rule{{port: 22}}}
	for _, d := range []string{"/etc", "/bin", "/usr/bin", "/usr/sbin", "/sbin", "/home", "/var/lib", "/var/backups", "/run", "/run/sshd", "/var/log", "/usr/local/bin",
		"/usr/lib/systemd/system", revert.SystemdUnitDir, revert.OpenRCInitDir, revert.OpenRCDefault, sshd.DropInDir} {
		h.AddDir(d, 0o755, 0, 0)
	}
	h.AddDir(revert.ProcDir, 0o555, 0, 0)
	h.AddFile(hardnBinary, []byte("ELF"), 0o755, 0, 0)
	h.AddFile(header.HostnameFile, []byte(hostName+"\n"), 0o444, 0, 0)
	for _, sh := range []string{"/bin/bash", "/bin/sh", "/usr/sbin/nologin", "/sbin/nologin", "/bin/ash"} {
		h.AddFile(sh, []byte("ELF"), 0o755, 0, 0)
	}
	main := sshd.IncludeLine + "\n\nUsePAM yes\n"
	nologin, rootShell, rootPW := "/usr/sbin/nologin", "/bin/bash", syntheticHash
	tools := []string{"sudo:x:27:", "adm:x:4:"}
	if p.openRC {
		nologin, rootShell, rootPW = "/sbin/nologin", "/bin/ash", "!"
		tools = []string{"wheel:x:10:root", "adm:x:4:root"}
		// The Alpine VM's sshd -T: authorizedkeysfile .ssh/authorized_keys
		// alone (00-inspect-alpine323-vm.log), as its sshd_config sets it.
		main += "AuthorizedKeysFile .ssh/authorized_keys\n"
		h.AddFile(sudoers.DoasConfig, []byte("# See doas.conf(5) for configuration details.\n# Uncomment to allow group \"wheel\" to become root.\n"), 0o640, 0, 0)
		h.AddDir(sudoers.DoasDir, 0o750, 0, 0)
		h.AddFile(sudoers.DoasDir+"/wheel.conf", []byte("permit nopass :wheel\n"), 0o640, 0, 0)
		h.AddFile("/usr/bin/doas", []byte("ELF"), 0o755, 0, 0)
		h.AddFile(detect.OpenRCSSHD, []byte("#!/sbin/openrc-run\n"), 0o755, 0, 0)
	}
	h.AddFile(sshd.MainConfig, []byte(main), 0o644, 0, 0)
	if p.sudoers != "" {
		h.AddFile(sudoers.MainConfig, []byte(p.sudoers), 0o440, 0, 0)
		h.AddDir(sudoers.DropInDir, 0o755, 0, 0)
	}
	if p.sudo {
		h.AddFile("/usr/bin/sudo", []byte("ELF"), 0o755, 0, 0)
	}
	if p.noUFW {
		h.rules = nil
	}
	passwd := "root:x:0:0:root:/root:" + rootShell + "\nsshd:x:100:65534::/run/sshd:" + nologin + "\nnobody:x:65534:65534:nobody:/nonexistent:" + nologin + "\n"
	shadow := "root:" + rootPW + ":20000:0:99999:7:::\nsshd:*:20000:0:99999:7:::\nnobody:*:20000:0:99999:7:::\n"
	group := "root:x:0:\n"
	for _, t := range tools {
		name := t[:strings.IndexByte(t, ':')]
		var members []string
		if strings.HasSuffix(t, ":root") {
			members = append(members, "root")
			t = strings.TrimSuffix(t, "root")
		}
		for _, u := range us {
			if slices.Contains(u.groups, name) {
				members = append(members, u.name)
			}
		}
		group += t + strings.Join(members, ",") + "\n"
	}
	h.AddDir("/root/.ssh", 0o700, 0, 0)
	h.AddFile("/root/.ssh/authorized_keys", []byte(synthKey(0x72, "root@admin-01")+"\n"), 0o600, 0, 0)
	h.AddUser(host.User{Name: "root", UID: 0, GID: 0, HomeDir: "/root"})
	for _, u := range us {
		passwd += fmt.Sprintf("%s:x:%d:%d::/home/%s:%s\n", u.name, u.uid, u.uid, u.name, u.shell)
		group += fmt.Sprintf("%s:x:%d:\n", u.name, u.uid)
		shadow += u.name + ":" + u.pw + ":20000:0:99999:7:::\n"
		h.AddUser(host.User{Name: u.name, UID: u.uid, GID: u.uid, HomeDir: "/home/" + u.name})
		h.AddDir("/home/"+u.name, p.home, u.uid, u.uid)
		if len(u.keys) > 0 {
			h.AddDir("/home/"+u.name+"/.ssh", 0o700, u.uid, u.uid)
			var lines string
			for i, s := range u.keys {
				lines += synthKey(s, fmt.Sprintf("%s@laptop-%d", u.name, i+1)) + "\n"
			}
			h.AddFile("/home/"+u.name+"/.ssh/authorized_keys", []byte(lines), 0o600, u.uid, u.uid)
		}
	}
	h.AddFile("/etc/passwd", []byte(passwd), 0o644, 0, 0)
	h.AddFile("/etc/group", []byte(group), 0o644, 0, 0)
	h.AddFile(accounts.ShadowFile, []byte(shadow), 0o640, 0, 42)
	if cloud && len(us) > 0 {
		h.AddFile(sudoers.DropInDir+"/90-cloud-init-users", []byte("# Created by cloud-init v. 25.1\n\n# User rules for "+us[0].name+"\n"+us[0].name+" ALL=(ALL) NOPASSWD:ALL\n"), 0o440, 0, 0)
	}
	h.saveRules()
	h.boot()
	h.Handler = h.answer
	return h
}

// Stat is the fake's, with the setuid bit on the tools the platform
// installs setuid root: hostfake keeps none (its package doc).
func (h *sshHost) Stat(ctx context.Context, name string) (fs.FileInfo, error) {
	fi, err := h.System.Stat(ctx, name)
	if err == nil && (name == "/usr/bin/sudo" && h.plat.sudo || name == "/usr/bin/doas" && h.plat.openRC) {
		fi = setuidInfo{fi}
	}
	return fi, err
}

// setuidInfo is a file's information with its setuid bit set.
type setuidInfo struct{ fs.FileInfo }

func (i setuidInfo) Mode() fs.FileMode { return i.FileInfo.Mode() | fs.ModeSetuid }

// rootLike gives root the shadow field pw and, as on the three VMs, the
// login user's first key (seed 1) as its one key.
func (h *sshHost) rootLike(pw string) *sshHost {
	data, _ := h.ReadFile(context.Background(), accounts.ShadowFile)
	lines := strings.Split(string(data), "\n")
	for i, l := range lines {
		if f := strings.SplitN(l, ":", 3); len(f) == 3 && f[0] == "root" {
			lines[i] = "root:" + pw + ":" + f[2]
		}
	}
	h.AddFile(accounts.ShadowFile, []byte(strings.Join(lines, "\n")), 0o640, 0, 42)
	h.AddFile("/root/.ssh/authorized_keys", []byte(synthKey(1, "george@laptop-1")+"\n"), 0o600, 0, 0)
	return h
}

// sshFile writes a drop-in or sshd_config and boots, so sshd runs with it.
func (h *sshHost) sshFile(name, data string, mode fs.FileMode) *sshHost {
	h.AddFile(name, []byte(data), mode, 0, 0)
	h.boot()
	return h
}

// withLog logs the session's login by method as user with the key of
// seed: sshd's line (auth2.c, "Accepted %s for %s from %s port %d ssh2: %s
// %s"), its port the session's.
func (h *sshHost) withLog(method, user string, seed byte) *sshHost {
	what := ""
	if method == "publickey" {
		blob, _ := base64.StdEncoding.DecodeString(strings.Fields(synthKey(seed, ""))[1])
		sum := sha256.Sum256(blob)
		what = ": ED25519 SHA256:" + base64.RawStdEncoding.EncodeToString(sum[:])
	}
	port := strings.Fields(h.sess.Conn)[1]
	line := fmt.Sprintf("sshd-session[3900]: Accepted %s for %s from 192.0.2.10 port %s ssh2%s", method, user, port, what)
	if h.plat.openRC {
		h.log = "Oct 20 11:58:00 " + hostName + " auth.info " + line + "\n"
		h.AddFile("/var/log/messages", []byte(h.log), 0o640, 0, 0)
		return h
	}
	h.log = "2026-10-20T11:58:00+00:00 " + hostName + " " + line + "\n"
	return h
}

// boot makes the files on disk the running configuration, as a reboot
// does: sshd's (or the socket's) listeners.
func (h *sshHost) boot() {
	if h.plat.socket {
		h.bound, h.configured = h.socketListens(), h.socketListens()
		return
	}
	h.bound, h.configured = h.listening(), h.listening()
}

// socketListens is ssh.socket's Listen as a daemon-reload or a boot sets
// it: the generator's, the listeners the sshd files configure, unless
// v0.3.x's listen.conf ("ListenStream=", then "ListenStream=<port>") is in
// ssh.socket.d, which resets them to its port alone, on a dual-stack [::]
// (docs/safety/reload.md §3).
func (h *sshHost) socketListens() []string {
	if e, ok := h.Entry(migrate.ListenConf); ok {
		for _, l := range strings.Split(string(e.Data), "\n") {
			if p, ok := strings.CutPrefix(l, "ListenStream="); ok && p != "" {
				return []string{"[::]:" + p}
			}
		}
	}
	return h.listening()
}

// listening is the listeners the installed files configure, by the model.
func (h *sshHost) listening() []string {
	_, ls := h.sshdT(h.files(sshd.MainConfig))
	return ls
}

// files is the configuration sshd -f conf reads: conf (sshd_config, or
// sshd.Validate's copy with its Include mapped back) and the drop-ins
// beside it, by their real paths.
func (h *sshHost) files(conf string) ([]byte, []sshd.File) {
	work := path.Dir(conf)
	e, _ := h.Entry(conf)
	main := strings.ReplaceAll(string(e.Data), "Include "+work+"/sshd_config.d/*.conf", sshd.IncludeLine)
	entries, _ := h.ReadDir(context.Background(), work+"/sshd_config.d")
	var drops []sshd.File
	for _, de := range entries {
		if d, ok := h.Entry(work + "/sshd_config.d/" + de.Name()); ok && d.Mode.IsRegular() {
			drops = append(drops, sshd.File{Path: sshd.DropInDir + "/" + de.Name(), Data: d.Data})
		}
	}
	return []byte(main), drops
}

// modelDefaults are the model's values for the keywords sshd -T always
// prints, shaped like OpenSSH's (sshd_config(5)), as the gallery's model
// has them; not read from a binary. permitrootlogin's and
// kbdinteractiveauthentication's are the platform's where it sets them.
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
// accumulating one, modelDefaults and the platform's for the rest, no
// usepam line from a build without PAM; and the listeners: a ListenAddress
// with a port as it is, one without on every Port, with none every Port on
// [::] and 0.0.0.0 (sshd_config(5) ListenAddress).
func (h *sshHost) sshdT(main []byte, drops []sshd.File) (out string, listeners []string) {
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
		switch {
		case d.k == "usepam" && h.plat.openRC:
			continue
		case ok:
		case d.k == "permitrootlogin":
			v = h.plat.rootLogin
		case d.k == "kbdinteractiveauthentication" && h.plat.kbdInteractive != "":
			v = h.plat.kbdInteractive
		default:
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

// matched is main with the directives of the Match blocks spec's
// connection matches ("user=george,host=192.0.2.10,addr=192.0.2.10") put
// first, so that sshdT, which reads the first value outside Match blocks,
// prints them: sshd_config(5), the first matching block's value winning.
// The criteria modelled are All, User, Group (not matched: no group is
// read), Address and Host (the address, UseDNS no); a negated pattern is
// honoured.
func matched(main []byte, drops []sshd.File, spec string) []byte {
	conn := map[string]string{}
	for _, kv := range strings.Split(spec, ",") {
		if k, v, ok := strings.Cut(kv, "="); ok {
			conn[k] = v
		}
	}
	var first []string
	for _, l := range sshd.ReadOrder(main, drops) {
		if l.Match == nil || !matches(l.Match.Text, conn) {
			continue
		}
		first = append(first, l.Keyword+" "+strings.Join(l.Args, " "))
	}
	if len(first) == 0 {
		return main
	}
	return []byte(strings.Join(first, "\n") + "\n" + string(main))
}

// matches reports whether a Match line's criteria all match conn.
func matches(line string, conn map[string]string) bool {
	f := strings.Fields(line)
	if len(f) < 2 {
		return false
	}
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
		switch crit {
		case "user":
			if !inList(conn["user"], list, func(s, p string) bool { ok, _ := path.Match(p, s); return ok }) {
				return false
			}
		case "address", "host":
			if !inList(conn["addr"], list, addrMatch) {
				return false
			}
		default:
			return false
		}
	}
	return true
}

// inList is match_pattern_list's answer: a negated pattern that matches
// refuses, else any pattern that matches.
func inList(s, list string, match func(s, p string) bool) bool {
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
	return found
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

// sshd answers sshd -t, -T and -T -C over the files -f names: sshd.c's
// fatal() where /run/sshd is missing (reload.md §2.4); -C with the Match
// blocks applied and requiredrsasize, the first value, OpenSSH 9.1's
// default 1024 where none is set.
func (h *sshHost) sshd(argv []string) hostfake.Response {
	if _, err := h.System.Stat(context.Background(), "/run/sshd"); err != nil {
		return hostfake.Response{Stderr: "Missing privilege separation directory: /run/sshd\r\n", ExitCode: 255}
	}
	conf := sshd.MainConfig
	if i := slices.Index(argv, "-f"); i >= 0 && i+1 < len(argv) {
		conf = argv[i+1]
	}
	main, drops := h.files(conf)
	if slices.Contains(argv, "-t") {
		return hostfake.Response{}
	}
	if i := slices.Index(argv, "-C"); i >= 0 && i+1 < len(argv) {
		m := matched(main, drops, argv[i+1])
		out, _ := h.sshdT(m, drops)
		size := strconv.Itoa(authkeys.MinRSABits)
		for _, l := range sshd.ReadOrder(m, drops) {
			if l.Match == nil && l.Keyword == "requiredrsasize" && len(l.Args) > 0 {
				size = l.Args[0]
				break
			}
		}
		return hostfake.Response{Stdout: out + "requiredrsasize " + size + "\n"}
	}
	out, _ := h.sshdT(main, drops)
	return hostfake.Response{Stdout: out}
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
func (h *sshHost) ufwStatus() string {
	if h.inactive {
		return "Status: inactive\n"
	}
	var b strings.Builder
	b.WriteString("Status: active\nLogging: on (low)\nDefault: deny (incoming), allow (outgoing), disabled (routed)\nNew profiles: skip\n")
	fmt.Fprintf(&b, "\n%-26s %-12s%s\n%-26s %-12s%s\n", "To", "Action", "From", "--", "------", "----")
	for _, v6 := range []bool{false, true} {
		for _, r := range h.rules {
			to, from := fmt.Sprintf("%d/tcp", r.port), r.src
			switch {
			case r.src != "" && v6:
				continue
			case r.src == "" && v6:
				to, from = to+" (v6)", "Anywhere (v6)"
			case r.src == "":
				from = "Anywhere"
			}
			fmt.Fprintf(&b, "%-26s %-12s%s\n", to, "ALLOW IN", from)
		}
	}
	return b.String()
}

// saveRules writes ufw's defaults, configuration and rule files as ufw
// keeps them; nothing where ufw is not installed.
func (h *sshHost) saveRules() {
	if h.plat.noUFW {
		return
	}
	h.AddDir(ufw.KernelIPv6, 0o555, 0, 0)
	h.AddFile(ufw.DefaultsFile, []byte("IPV6=yes\nDEFAULT_INPUT_POLICY=\"DROP\"\nDEFAULT_OUTPUT_POLICY=\"ACCEPT\"\n"), 0o644, 0, 0)
	enabled := "yes"
	if h.inactive {
		enabled = "no"
	}
	h.AddFile(ufw.ConfFile, []byte("ENABLED="+enabled+"\nLOGLEVEL=low\n"), 0o644, 0, 0)
	for _, f := range []struct {
		path, any string
		v6        bool
	}{{ufw.UserRules, "0.0.0.0/0", false}, {ufw.User6Rules, "::/0", true}} {
		var b strings.Builder
		b.WriteString("*filter\n:ufw-user-input - [0:0]\n### RULES ###\n")
		for _, r := range h.rules {
			src := r.src
			switch {
			case src != "" && f.v6:
				continue
			case src == "":
				src = f.any
			}
			fmt.Fprintf(&b, "\n### tuple ### allow tcp %d %s any %s in\n-A ufw-user-input -p tcp --dport %d -j ACCEPT\n", r.port, f.any, src, r.port)
		}
		b.WriteString("\n### END RULES ###\nCOMMIT\n")
		h.AddFile(f.path, []byte(b.String()), 0o640, 0, 0)
	}
}

// installed reports whether the platform ships the command name: an init
// system's own tools on that init system alone, ufw unless it is not
// installed, sudo's and doas where installed. A command that is not is not
// found, as on the host.
func (h *sshHost) installed(name string) bool {
	switch name {
	case "systemctl", "systemd-run", "systemd-analyze", "journalctl":
		return !h.plat.openRC
	case "rc-service", "rc-update":
		return h.plat.openRC
	case "ufw":
		return !h.plat.noUFW
	case "sudo", "visudo":
		return h.plat.sudo
	case "doas":
		return h.plat.openRC
	case "ss": // the Alpine VM has no ss, busybox's netstat alone (00-inspect-alpine323.log)
		return !h.plat.openRC
	}
	return true
}

// answer is the host's answer to argv, changing the model as the host
// would change. A command it does not know is recorded (unanswered), which
// the tests fail on.
func (h *sshHost) answer(argv []string, _ []byte) hostfake.Response {
	h.mu.Lock()
	defer h.mu.Unlock()
	cmd := strings.Join(argv, " ")
	if !h.installed(path.Base(argv[0])) {
		return hostfake.Response{Err: host.ErrNotFound, ExitCode: -1}
	}
	switch path.Base(argv[0]) {
	case "sshd":
		return h.sshd(argv)
	case "journalctl": // journalctl -u ssh -u sshd: sshd's lines, as the Debian and Ubuntu VMs showed them
		return hostfake.Response{Stdout: h.log}
	case "ss":
		return hostfake.Response{Stdout: ssOutput(h.bound)}
	case "visudo":
		return hostfake.Response{Stdout: sudoers.MainConfig + ": parsed OK\n"}
	case "doas": // doas -C <file>, the parse check pkg/host/sudoers runs: every file passes
		if len(argv) == 3 && argv[1] == "-C" {
			return hostfake.Response{}
		}
	case "systemd-run", "systemd-analyze", "sh": // the revert timer, the boot unit's check, OpenRC's sleeper
		return hostfake.Response{}
	case "rc-update":
		if cmd == "rc-update add "+revert.BootUnitName+" default" {
			h.AddSymlink(revert.OpenRCDefault+"/"+revert.BootUnitName, revert.OpenRCInitDir+"/"+revert.BootUnitName)
			return hostfake.Response{}
		}
	case "rc-service":
		if cmd == "rc-service sshd reload" {
			h.bound = slices.Clone(h.listening())
			if h.pid != 0 {
				h.pid++
				h.listener(false)
			}
			return hostfake.Response{}
		}
		if r, ok := h.updates(argv); ok {
			return r
		}
	case "ufw":
		if r, ok := h.ufw(argv); ok {
			return r
		}
	case "apt-config", "dpkg-query", "apk":
		if r, ok := h.opensshApk(cmd); ok {
			return r
		}
		if r, ok := h.updates(argv); ok {
			return r
		}
	case "systemctl":
		if r, ok := h.systemctl(argv); ok {
			return r
		}
	}
	h.unexpected = append(h.unexpected, cmd)
	return hostfake.Response{Err: hostfake.ErrUnscripted, ExitCode: -1}
}

// systemctl answers systemctl as the engine and the readers run it.
func (h *sshHost) systemctl(argv []string) (hostfake.Response, bool) {
	state := func(on bool, yes, no string, code int) hostfake.Response {
		if on {
			return hostfake.Response{Stdout: yes + "\n"}
		}
		return hostfake.Response{Stdout: no + "\n", ExitCode: code}
	}
	switch cmd := strings.Join(argv, " "); {
	case cmd == "systemctl is-active ssh.service": // running on every systemd host, by itself or socket-activated
		return hostfake.Response{Stdout: "active\n"}, true
	case cmd == "systemctl is-enabled ssh.service":
		return state(!h.plat.socket, "enabled", "disabled", 1), true
	case cmd == "systemctl is-active ssh.socket":
		return state(h.plat.socket, "active", "inactive", 3), true
	case cmd == "systemctl is-enabled ssh.socket":
		return state(h.plat.socket, "enabled", "disabled", 1), true
	case cmd == "systemctl is-enabled ufw.service":
		return state(!h.inactive, "enabled", "disabled", 1), true
	case cmd == "systemctl is-active "+hostdns.ResolvedUnit: // no systemd-resolved on the Debian VM: dhcpcd writes the file
		return state(false, "active", "inactive", 3), true
	case cmd == "systemctl reload ssh.service":
		if !h.plat.socket {
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
		h.AddSymlink(revert.SystemdWants+"/"+revert.BootUnitName+".service", revert.SystemdUnitDir+"/"+revert.BootUnitName+".service")
		return hostfake.Response{}, true
	case cmd == "systemctl is-active pve-firewall.service": // Proxmox VE's, running as installed
		return hostfake.Response{Stdout: "active\n"}, true
	case strings.HasPrefix(cmd, "systemctl is-active apt-daily"): // apt's daily jobs, on as the package leaves them
		return hostfake.Response{Stdout: "active\n"}, true
	case strings.HasPrefix(cmd, "systemctl is-enabled apt-daily"):
		return hostfake.Response{Stdout: "enabled\n"}, true
	case len(argv) == 6 && argv[1] == "list-timers":
		return hostfake.Response{Stdout: "[]\n"}, true
	}
	return hostfake.Response{}, false
}

// ufw answers ufw 0.36.2's status, and a port's allow rule added or
// deleted, in both families.
func (h *sshHost) ufw(argv []string) (hostfake.Response, bool) {
	if strings.Join(argv, " ") == "ufw status verbose" {
		return hostfake.Response{Stdout: h.ufwStatus()}, true
	}
	n, err := strconv.Atoi(strings.TrimSuffix(argv[len(argv)-1], "/tcp"))
	switch {
	case err != nil:
	case len(argv) == 3 && argv[1] == "allow":
		h.rules = append(h.rules, rule{port: uint16(n)})
		h.saveRules()
		return hostfake.Response{Stdout: "Rule added\nRule added (v6)\n"}, true
	case len(argv) == 4 && argv[1] == "delete" && argv[2] == "allow":
		h.rules = slices.DeleteFunc(h.rules, func(r rule) bool { return r.port == uint16(n) && r.src == "" })
		h.saveRules()
		return hostfake.Response{Stdout: "Rule deleted\nRule deleted (v6)\n"}, true
	}
	return hostfake.Response{}, false
}

// updates answers Auto Updates' check on the VMs as the gallery's model
// leaves them (updateshost.go, autoOn): unattended-upgrades installed, apt's
// daily jobs on, Debian's Origins-Pattern; apk-cron not installed on
// Alpine. Its plan is not modelled here: with aptBroken, apt's syntax
// error, every apt-config run fails, as an administrator's typo makes it.
func (h *sshHost) updates(argv []string) (hostfake.Response, bool) {
	cmd := strings.Join(argv, " ")
	switch {
	case argv[0] == "apt-config" && h.aptBroken:
		return hostfake.Response{Stderr: "E: Syntax error /etc/apt/apt.conf.d/99local:1: Extra junk at end of file\n", ExitCode: 100}, true
	case strings.HasPrefix(cmd, "apt-config dump APT::Periodic"):
		return hostfake.Response{Stdout: "APT::Periodic \"\";\nAPT::Periodic::Update-Package-Lists \"1\";\nAPT::Periodic::Unattended-Upgrade \"1\";\n" +
			"Unattended-Upgrade::Origins-Pattern \"\";\nUnattended-Upgrade::Origins-Pattern:: \"origin=Debian,codename=${distro_codename},label=Debian-Security\";\n"}, true
	case argv[0] == "dpkg-query" && argv[len(argv)-1] == "unattended-upgrades":
		return hostfake.Response{Stdout: "install ok installed\t2.12\n"}, true
	case cmd == "apk info -e apk-cron":
		return hostfake.Response{ExitCode: 1}, true
	case cmd == "apk add --simulate apk-cron": // the Alpine VM's (updateshost.go)
		return hostfake.Response{Stdout: "(1/1) Installing apk-cron (1.0-r3)\nOK: 9 MiB in 42 packages\n"}, true
	case cmd == "rc-service crond status": // crond started, as on the Alpine VM
		return hostfake.Response{Stdout: " * status: started\n"}, true
	}
	return hostfake.Response{}, false
}

// unanswered returns the commands the fake did not answer.
func (h *sshHost) unanswered() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return slices.Clone(h.unexpected)
}

// checkHost fails t for a command h did not answer.
func checkHost(t *testing.T, name string, h *sshHost) {
	t.Helper()
	if got := h.unanswered(); len(got) > 0 {
		t.Errorf("%s: commands the fake does not answer: %q", name, got)
	}
}
