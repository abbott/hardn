package users_test

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"io/fs"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/abbott/hardn/pkg/engine/header"
	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/accounts"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/hostfake"
	"github.com/abbott/hardn/pkg/host/revert"
	"github.com/abbott/hardn/pkg/host/sshd"
	"github.com/abbott/hardn/pkg/host/sudoers"
)

// The screen's hosts: hostfake, with the accounts, keys and elevation rules
// the three VMs showed on 2026-11-03 (s5/00-inspect-*.log), as the
// gallery's users hosts are (internal/tools/uigallery/usershost.go, its
// userSim copied here, as a test cannot import the gallery). Synthetic
// values only: george the login user, alice a second one, carol the one
// Create makes, web-01, 192.0.2.x. sshd -T is a model of sshd's reading of
// its files (sshd.ReadOrder), shaped like OpenSSH's, not read from a binary.

const (
	hardnBinary = "/usr/local/bin/hardn"
	hostName    = "web-01"
	// syntheticHash is a password set: no hash of anything.
	syntheticHash = "$y$synthetic$not-a-hash"
)

// platform is what a host's release ships, as detect reads it.
type platform struct {
	facts   detect.Facts
	alpine  bool // OpenRC, doas, /bin/ash, sshd built without PAM
	sudo    bool // sudo installed, setuid root
	home    fs.FileMode
	sudoers string // /etc/sudoers, "" for none
}

// The platforms: Debian 13, Ubuntu 26.04 (sudo-rs, HOME_MODE 0750),
// Alpine 3.23 (doas, a home 0755 [I: login.defs has no HOME_MODE; the VM's
// login home is 0755]), Proxmox VE 9 (no sudo).
func debian13() platform {
	return platform{sudo: true, home: 0o700, sudoers: "Defaults env_reset\nroot ALL=(ALL:ALL) ALL\n%sudo ALL=(ALL:ALL) ALL\n@includedir /etc/sudoers.d\n",
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
	p.home = 0o750
	p.sudoers = "Defaults env_reset\nroot ALL=(ALL:ALL) ALL\n%admin ALL=(ALL) ALL\n%sudo ALL=(ALL:ALL) ALL\n@includedir /etc/sudoers.d\n"
	p.facts.Platform = detect.Platform{Distro: detect.DistroUbuntu, Version: "26.04", Support: detect.Supported}
	p.facts.Elevation.SudoFlavor = detect.SudoRS
	return p
}

func alpine323() platform {
	return platform{alpine: true, home: 0o755, sudoers: "root ALL=(ALL:ALL) ALL\n#includedir /etc/sudoers.d\n",
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
	p.sudo, p.sudoers = false, ""
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
	// pw is its password field in /etc/shadow: "!" (no password: the Debian
	// and Ubuntu VMs' login user), "*" (none, not locked: the Alpine VM's),
	// or syntheticHash.
	pw    string
	shell string
	keys  []byte // a synthetic key per seed, in ~/.ssh/authorized_keys
}

// usrHost is a host as a test runs the screen on it: the fake, its facts,
// and what the fake answers beyond its files.
type usrHost struct {
	*hostfake.System
	facts detect.Facts
	plat  platform
	env   map[string]string // hardn's environment: SUDO_USER or DOAS_USER, none at a root login
	// rejects: visudo -c rejects cloud-init's file.
	rejects bool

	mu         sync.Mutex
	unexpected []string // the commands the fake does not answer
}

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
// the VMs' are (passwd, group, shadow; the keys 0600 in .ssh 0700), root
// with one key, and cloud-init's sudoers rule for the first of us where
// cloud is set (/etc/sudoers.d/90-cloud-init-users, 0440, the VMs'
// "<user> ALL=(ALL) NOPASSWD:ALL"); what the engine needs to apply a plan;
// the hostname.
func usersOn(p platform, cloud bool, us ...simUser) *usrHost {
	h := &usrHost{System: hostfake.New(host.Options{}), facts: p.facts, plat: p}
	for _, d := range []string{"/etc", "/bin", "/usr/bin", "/usr/sbin", "/sbin", "/home", "/var/lib", "/var/backups", "/run", "/var/log", "/usr/local/bin",
		"/usr/lib/systemd/system", "/etc/systemd/system", "/etc/init.d", "/etc/runlevels/default", sshd.DropInDir} {
		h.AddDir(d, 0o755, 0, 0)
	}
	h.AddDir(revert.ProcDir, 0o555, 0, 0) // no sshd process: the session is the journal's alone
	h.AddFile(hardnBinary, []byte("ELF"), 0o755, 0, 0)
	h.AddFile(header.HostnameFile, []byte(hostName+"\n"), 0o444, 0, 0)
	for _, sh := range []string{"/bin/bash", "/bin/sh", "/usr/sbin/nologin", "/sbin/nologin", "/bin/ash"} {
		h.AddFile(sh, []byte("ELF"), 0o755, 0, 0)
	}
	main := sshd.IncludeLine + "\n\nUsePAM yes\n"
	nologin, rootShell := "/usr/sbin/nologin", "/bin/bash"
	tools := []string{"sudo:x:27:", "adm:x:4:"}
	if p.alpine {
		nologin, rootShell = "/sbin/nologin", "/bin/ash"
		tools = []string{"wheel:x:10:root", "adm:x:4:root"}
		// The Alpine VM's sshd -T: authorizedkeysfile .ssh/authorized_keys
		// alone (00-inspect-alpine323-vm.log), as its sshd_config sets it.
		main += "AuthorizedKeysFile .ssh/authorized_keys\n"
		h.AddFile(sudoers.DoasConfig, []byte("# See doas.conf(5) for configuration details.\n# Uncomment to allow group \"wheel\" to become root.\n"), 0o640, 0, 0)
		h.AddDir(sudoers.DoasDir, 0o750, 0, 0)
		h.AddFile(sudoers.DoasDir+"/wheel.conf", []byte("permit nopass :wheel\n"), 0o640, 0, 0)
		h.AddFile("/usr/bin/doas", []byte("ELF"), 0o755, 0, 0)
		h.AddFile("/usr/sbin/useradd", []byte("ELF"), 0o755, 0, 0) // shadow's, which Create runs
	}
	h.AddFile(sshd.MainConfig, []byte(main), 0o644, 0, 0)
	if p.sudoers != "" {
		h.AddFile(sudoers.MainConfig, []byte(p.sudoers), 0o440, 0, 0)
		h.AddDir(sudoers.DropInDir, 0o755, 0, 0)
	}
	if p.sudo {
		h.AddFile("/usr/bin/sudo", []byte("ELF"), 0o755, 0, 0)
	}
	passwd := "root:x:0:0:root:/root:" + rootShell + "\nsshd:x:100:65534::/run/sshd:" + nologin + "\nnobody:x:65534:65534:nobody:/nonexistent:" + nologin + "\n"
	rootPW := syntheticHash
	if p.alpine {
		rootPW = "!"
	}
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
	h.Handler = h.answer
	return h
}

// Stat is the fake's, with the setuid bit on the tools the platform
// installs setuid root: hostfake keeps none (its package doc).
func (h *usrHost) Stat(ctx context.Context, name string) (fs.FileInfo, error) {
	fi, err := h.System.Stat(ctx, name)
	if err == nil && (name == "/usr/bin/sudo" && h.plat.sudo || name == "/usr/bin/doas" && h.plat.alpine) {
		fi = setuidInfo{fi}
	}
	return fi, err
}

// setuidInfo is a file's information with its setuid bit set.
type setuidInfo struct{ fs.FileInfo }

func (i setuidInfo) Mode() fs.FileMode { return i.FileInfo.Mode() | fs.ModeSetuid }

// getenv is hardn's environment on the host.
func (h *usrHost) getenv(k string) string { return h.env[k] }

// answer is the host's answer to argv: the tools' checks, sshd -T, the
// accounts' commands, and the revert timer's and the boot unit's. A command
// it does not know is recorded (unanswered), which the tests fail on.
func (h *usrHost) answer(argv []string, _ []byte) hostfake.Response {
	cmd := strings.Join(argv, " ")
	switch argv[0] {
	case "visudo":
		switch {
		case cmd == "visudo -c" && h.rejects: // visudo(8), EXIT VALUE: a file it cannot parse
			return hostfake.Response{ExitCode: 1, Stderr: "/etc/sudoers.d/90-cloud-init-users:4:27: syntax error\n"}
		case cmd == "visudo -c":
			return hostfake.Response{Stdout: sudoers.MainConfig + ": parsed OK\n"}
		case len(argv) == 3 && argv[1] == "-cf": // a plan's copy (sudoers.Apply)
			return hostfake.Response{Stdout: argv[2] + ": parsed OK\n"}
		}
	case "doas":
		if len(argv) == 3 && argv[1] == "-C" { // the parse check pkg/host/sudoers runs: every file passes
			return hostfake.Response{}
		}
	case "sshd", "/usr/sbin/sshd":
		if slices.Contains(argv, "-T") {
			return h.sshdT()
		}
	case "useradd", "userdel", "install":
		if r, ok := h.account(argv); ok {
			return r
		}
	case "systemd-run", "systemd-analyze", "sh", "/bin/sh", "setsid", "rc-service":
		return hostfake.Response{}
	case "rc-update":
		if cmd == "rc-update add hardn-boot default" {
			h.AddSymlink(detect.OpenRCRunlevels+"/default/hardn-boot", "/etc/init.d/hardn-boot")
		}
		return hostfake.Response{}
	case "systemctl":
		switch {
		case cmd == "systemctl --no-reload enable hardn-boot.service":
			h.AddSymlink("/etc/systemd/system/multi-user.target.wants/hardn-boot.service", "/etc/systemd/system/hardn-boot.service")
			return hostfake.Response{}
		case strings.Contains(cmd, "hardn"), cmd == "systemctl daemon-reload":
			return hostfake.Response{}
		}
	}
	h.mu.Lock()
	h.unexpected = append(h.unexpected, cmd)
	h.mu.Unlock()
	return hostfake.Response{Err: hostfake.ErrUnscripted, ExitCode: -1}
}

// unanswered returns the commands the fake did not answer.
func (h *usrHost) unanswered() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return slices.Clone(h.unexpected)
}

// sshdKeywords are the values the model prints for the keywords sshd -T
// always prints, where the files set none, as the gallery's model has them
// (modelDefaults), shaped like OpenSSH's (sshd_config(5)).
var sshdKeywords = []struct{ k, v string }{
	{"permitrootlogin", "prohibit-password"}, {"pubkeyauthentication", "yes"}, {"passwordauthentication", "yes"},
	{"kbdinteractiveauthentication", "no"}, {"permitemptypasswords", "no"}, {"usepam", "no"},
	{"hostbasedauthentication", "no"}, {"gssapiauthentication", "no"}, {"kerberosauthentication", "no"},
	{"authorizedkeysfile", ".ssh/authorized_keys .ssh/authorized_keys2"}, {"authenticationmethods", "any"}, {"strictmodes", "yes"},
	{"authorizedkeyscommand", "none"}, {"authorizedkeyscommanduser", "none"}, {"authorizedprincipalsfile", "none"},
	{"authorizedprincipalscommand", "none"}, {"authorizedprincipalscommanduser", "none"}, {"trustedusercakeys", "none"},
	{"revokedkeys", "none"}, {"pubkeyauthoptions", "none"}, {"maxauthtries", "6"}, {"forcecommand", "none"}, {"chrootdirectory", "none"},
}

// sshdT is sshd -T's output from the files, by sshd's reading order: the
// first value of each keyword outside Match blocks, the model's for the
// rest; no usepam line from Alpine's build, without PAM; port 22 on [::]
// and 0.0.0.0. With no sshd_config, sshd's own error, exit 255.
func (h *usrHost) sshdT() hostfake.Response {
	ctx := context.Background()
	main, err := h.ReadFile(ctx, sshd.MainConfig)
	if err != nil {
		return hostfake.Response{Stderr: sshd.MainConfig + ": No such file or directory\n", ExitCode: 255}
	}
	entries, _ := h.ReadDir(ctx, sshd.DropInDir)
	var drops []sshd.File
	for _, de := range entries {
		if data, err := h.ReadFile(ctx, sshd.DropInDir+"/"+de.Name()); err == nil && strings.HasSuffix(de.Name(), ".conf") {
			drops = append(drops, sshd.File{Path: sshd.DropInDir + "/" + de.Name(), Data: data})
		}
	}
	first := map[string]string{}
	for _, l := range sshd.ReadOrder(main, drops) {
		if _, ok := first[l.Keyword]; !ok && l.Match == nil && !sshd.Accumulates(l.Keyword) {
			first[l.Keyword] = strings.Join(l.Args, " ")
		}
	}
	var b strings.Builder
	b.WriteString("port 22\nlistenaddress [::]:22\nlistenaddress 0.0.0.0:22\n")
	for _, d := range sshdKeywords {
		v, ok := first[d.k]
		switch {
		case d.k == "usepam" && h.plat.alpine:
			continue
		case !ok:
			v = d.v
		}
		if d.k != "authorizedkeysfile" {
			v = strings.ToLower(v)
		}
		b.WriteString(d.k + " " + v + "\n")
	}
	return hostfake.Response{Stdout: b.String()}
}

// account answers useradd, userdel and install -d as shadow's and
// coreutils' tools do them: userSim's (usershost.go).
func (h *usrHost) account(argv []string) (hostfake.Response, bool) {
	ctx := context.Background()
	switch argv[0] {
	case "useradd": // useradd(8): [--home-dir D] [--create-home] --shell S [--password P] NAME; the user's own group (USERGROUPS_ENAB yes, on all three VMs); "!" without --password; the home, HOME_MODE's, only with --create-home
		name := argv[len(argv)-1]
		shell, pw, home, mkHome := "/bin/sh", "!", "/home/"+name, false
		for i := 1; i < len(argv)-1; i++ {
			switch argv[i] {
			case "--shell":
				i++
				shell = argv[i]
			case "--password":
				i++
				pw = argv[i]
			case "--home-dir":
				i++
				home = argv[i]
			case "--create-home":
				mkHome = true
			}
		}
		db, _ := accounts.Read(ctx, h.System)
		uid := 1000
		for _, x := range db.Users {
			if x.UID >= uid && x.UID < 60000 {
				uid = x.UID + 1
			}
		}
		h.appendLine("/etc/passwd", fmt.Sprintf("%s:x:%d:%d::%s:%s", name, uid, uid, home, shell), 0o644, 0)
		h.appendLine("/etc/group", fmt.Sprintf("%s:x:%d:", name, uid), 0o644, 0)
		h.appendLine(accounts.ShadowFile, name+":"+pw+":20000:0:99999:7:::", 0o640, 42)
		h.AddUser(host.User{Name: name, UID: uid, GID: uid, HomeDir: home})
		if mkHome {
			h.AddDir(home, h.plat.home, uid, uid)
		}
		return hostfake.Response{}, true
	case "userdel": // userdel(8) --remove NAME: the lines and the home directory
		name := argv[len(argv)-1]
		for _, f := range []string{"/etc/passwd", "/etc/group", accounts.ShadowFile} {
			h.dropLine(f, name)
		}
		_ = h.RemoveAll(ctx, "/home/"+name)
		return hostfake.Response{}, true
	case "install": // install(1) -d -m 700 -o USER -g GROUP DIR, the user by name or uid, its group taken as its own
		if len(argv) == 9 && argv[1] == "-d" {
			db, _ := accounts.Read(ctx, h.System)
			for _, x := range db.Users {
				if x.Name == argv[5] || strconv.Itoa(x.UID) == argv[5] {
					m, _ := strconv.ParseUint(argv[3], 8, 32)
					h.AddDir(argv[8], fs.FileMode(m), x.UID, x.GID)
					return hostfake.Response{}, true
				}
			}
			return hostfake.Response{ExitCode: 1, Stderr: "install: invalid user '" + argv[5] + "'\n"}, true
		}
	}
	return hostfake.Response{}, false
}

// appendLine adds line to the file name.
func (h *usrHost) appendLine(name, line string, mode fs.FileMode, gid int) {
	data, _ := h.ReadFile(context.Background(), name)
	h.AddFile(name, append(data, []byte(line+"\n")...), mode, 0, gid)
}

// dropLine removes user's line from the file name.
func (h *usrHost) dropLine(name, user string) {
	e, ok := h.Entry(name)
	if !ok {
		return
	}
	var keep []string
	for _, l := range strings.SplitAfter(string(e.Data), "\n") {
		if !strings.HasPrefix(l, user+":") {
			keep = append(keep, l)
		}
	}
	h.AddFile(name, []byte(strings.Join(keep, "")), e.Mode.Perm(), e.UID, e.GID)
}

// george is the VMs' login user: uid 1000 (Debian, Ubuntu) or 1001
// (Alpine), in sudo and adm (wheel and adm), no password, two keys.
func george(p platform) simUser {
	if p.alpine {
		return simUser{name: "george", uid: 1001, groups: []string{"wheel", "adm"}, pw: "*", shell: "/bin/ash", keys: []byte{1, 2}}
	}
	return simUser{name: "george", uid: 1000, groups: []string{"sudo", "adm"}, pw: "!", shell: "/bin/bash", keys: []byte{1, 2}}
}

// alice is the VMs' second user: uid 1001, a password, in sudo, no key.
func alice() simUser {
	return simUser{name: "alice", uid: 1001, groups: []string{"sudo"}, pw: syntheticHash, shell: "/bin/bash"}
}

// hardnGrant writes hardn's own grant for user, as sudoers.PlanFor writes
// it (sudoers.md §2.6), or doas's rule on a doas host.
func (h *usrHost) hardnGrant(user string, g sudoers.Grant) *usrHost {
	tool := sudoers.ToolSudo
	if h.plat.alpine {
		tool = sudoers.ToolDoas
	}
	n, _ := sudoers.FileNames(tool, user)
	var line string
	switch {
	case tool == sudoers.ToolDoas && g == sudoers.GrantNoPassword:
		line = "permit nopass " + user
	case tool == sudoers.ToolDoas:
		line = "permit persist " + user
	case g == sudoers.GrantNoPassword:
		line = user + " ALL=(ALL) NOPASSWD: ALL"
	default:
		line = user + " ALL=(ALL) ALL"
	}
	mode := fs.FileMode(0o440)
	if tool == sudoers.ToolDoas {
		mode = 0o640
	}
	h.AddFile(n.Grant, []byte(sudoers.Header+line+"\n"), mode, 0, 0)
	return h
}

// usrHosts are the gallery's users hosts (usrHosts), in its order, by name.
var usrHosts = []string{"debian13", "ubuntu2604", "alpine323", "legacy", "hardn", "second-admin", "one-key", "sudo-password", "alpine-no-rule",
	"alpine-locked", "yml-missing", "not-read", "root-login", "debian-nopam", "alpine-stray", "group-home", "pve9", "no-sudo"}

// usrYML is hardn.yml naming a user the host does not have.
const usrYML = "username: deploy\nsudoNoPassword: true\nsshKeys:\n  - ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIAEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEB deploy@ci\n"

// newHost is the host name names, as the gallery's note for it says, with
// hardn's environment there: run through george's sudo, or doas.
func newHost(t *testing.T, name string) *usrHost {
	t.Helper()
	viaSudo := map[string]string{"SUDO_USER": "george"}
	viaDoas := map[string]string{"DOAS_USER": "george"}
	deb, alp := debian13(), alpine323()
	var h *usrHost
	env := viaSudo
	switch name {
	case "debian13", "yml-missing":
		h = usersOn(deb, true, george(deb), alice())
	case "ubuntu2604":
		u := ubuntu2604()
		h = usersOn(u, true, george(u), alice())
	case "alpine323":
		h, env = alpineVM(), viaDoas
	case "legacy":
		g := george(deb)
		g.groups, g.pw = []string{"adm"}, syntheticHash
		h = usersOn(deb, false, g)
		h.AddFile(sudoers.DropInDir+"/george", []byte("george ALL=(ALL) NOPASSWD: ALL\nDefaults:george env_keep += \"HARDN_CONFIG\"\n"), 0o440, 0, 0)
	case "hardn", "root-login":
		g := george(deb)
		g.groups = []string{"adm"}
		h = usersOn(deb, false, g).hardnGrant("george", sudoers.GrantNoPassword)
		if name == "root-login" {
			env = nil
		}
	case "second-admin":
		a := alice()
		a.groups, a.keys = nil, []byte{3}
		h = usersOn(deb, true, george(deb), a).hardnGrant("alice", sudoers.GrantNoPassword)
	case "one-key":
		g := george(deb)
		g.keys = []byte{1}
		h = usersOn(deb, true, g, alice())
	case "sudo-password":
		g := george(deb)
		g.groups, g.pw = []string{"adm"}, syntheticHash
		h = usersOn(deb, false, g).hardnGrant("george", sudoers.GrantPassword)
	case "alpine-no-rule":
		g := george(alp)
		g.groups = []string{"adm"}
		h, env = usersOn(alp, false, g), viaDoas
	case "alpine-locked":
		h, env = alpineVM(simUser{name: "alice", uid: 1002, groups: []string{"wheel"}, pw: "!", shell: "/bin/ash", keys: []byte{4}}), viaDoas
	case "not-read":
		h = usersOn(deb, true, george(deb), alice())
		h.rejects = true
	case "debian-nopam":
		h = usersOn(deb, true, george(deb), alice())
		h.AddFile(sshd.DropInDir+"/50-usepam.conf", []byte("UsePAM no\n"), 0o644, 0, 0)
	case "alpine-stray":
		h, env = alpineVM(), viaDoas
		h.AddFile("/home/george/.ssh/authorized_keys", []byte(synthKey(1, "george@laptop-1")+"\n"), 0o600, 1001, 1001)
		h.AddFile("/home/george/.ssh/authorized_keys2", []byte(synthKey(6, "george@old-laptop")+"\n"), 0o600, 1001, 1001)
	case "group-home":
		h = usersOn(deb, true, george(deb), alice())
		h.AddDir("/home/alice", 0o775, 1001, 1001)
	case "pve9":
		h, env = usersOn(pve9(), false), nil
	case "no-sudo":
		p := deb
		p.sudo = false
		p.facts.Elevation.Sudo = detect.Tool{Present: detect.No}
		g := george(p)
		g.groups = []string{"adm"}
		h = usersOn(p, false, g)
	default:
		t.Fatalf("no host %q", name)
	}
	h.env = env
	return h
}

// alpineVM is the Alpine VM's accounts, alpine and george, then more, and
// cloud-init's sudoers file, where no sudo reads it.
func alpineVM(more ...simUser) *usrHost {
	p := alpine323()
	h := usersOn(p, true, append([]simUser{{name: "alpine", uid: 1000, groups: []string{"wheel"}, pw: "*", shell: "/bin/ash"}, george(p)}, more...)...)
	h.AddFile(sudoers.DropInDir+"/90-cloud-init-users", []byte("# Created by cloud-init v. 25.1\n\n# User rules for george\ngeorge ALL=(ALL) NOPASSWD:ALL\n"), 0o440, 0, 0)
	return h
}
