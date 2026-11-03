package main

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"io/fs"
	"slices"
	"strconv"
	"strings"

	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/accounts"
	"github.com/abbott/hardn/pkg/host/hostfake"
	"github.com/abbott/hardn/pkg/host/sudoers"
)

// The User Management mock's hosts (refactor plan slice 5, unit (a),
// next/s5-gallery; docs/engine/gallery-users.md): statusOn's hosts with the
// accounts, keys and elevation rules the three VMs showed on 2026-11-03, their
// values synthetic (george the login user, alice a second one). What the
// fake host answers beyond statusOn's is userSim's, an addition: useradd,
// userdel and install -d as shadow's and coreutils' tools do them, and
// visudo -c rejecting the files where a host asks for it. Each answer's
// source is beside it.

// simUser is one account a users host has.
type simUser struct {
	name   string
	uid    int
	groups []string // the supplementary groups /etc/group lists it in
	// pw is its password field in /etc/shadow: "!" (no password, passwd -S
	// L: the Debian and Ubuntu VMs' login user), "*" (none, not locked: the
	// Alpine VM's), or syntheticHash (a password set).
	pw    string
	shell string
	keys  []byte // a synthetic key per seed, in ~/.ssh/authorized_keys
}

// userSim is what the fake host answers for the accounts: the commands the
// mock's stand-in plans run, and visudo -c rejecting the files (rejects).
type userSim struct {
	rejects bool
	home    int // HOME_MODE: login.defs 0700 (Debian 13) or 0750 (Ubuntu 26.04)
}

// answer answers argv when it is the accounts', or reports that it is not.
func (u *userSim) answer(h *fakeHost, argv []string) (hostfake.Response, bool) {
	if u == nil {
		return hostfake.Response{}, false
	}
	ctx := context.Background()
	switch argv[0] {
	case "visudo": // visudo -c over a file it cannot parse: exit 1 and its line (visudo(8), EXIT VALUE)
		if u.rejects && slices.Equal(argv, []string{"visudo", "-c"}) {
			return hostfake.Response{ExitCode: 1, Stderr: "/etc/sudoers.d/90-cloud-init-users:4:27: syntax error\n"}, true
		}
	case "useradd": // useradd(8): --create-home --shell S [--password P] NAME; the user's own group (USERGROUPS_ENAB yes); "!" without --password
		name, shell, pw := argv[len(argv)-1], "/bin/sh", "!"
		for i := 1; i+1 < len(argv); i++ {
			switch argv[i] {
			case "--shell":
				shell = argv[i+1]
			case "--password":
				pw = argv[i+1]
			}
		}
		db, _ := accounts.Read(ctx, h.f)
		uid := 1000
		for _, x := range db.Users {
			if x.UID >= uid && x.UID < 60000 {
				uid = x.UID + 1
			}
		}
		h.appendLine("/etc/passwd", fmt.Sprintf("%s:x:%d:%d::/home/%s:%s", name, uid, uid, name, shell), 0o644, 0)
		h.appendLine("/etc/group", fmt.Sprintf("%s:x:%d:", name, uid), 0o644, 0)
		h.appendLine(accounts.ShadowFile, name+":"+pw+":20000:0:99999:7:::", 0o640, 42)
		h.f.AddUser(host.User{Name: name, UID: uid, GID: uid, HomeDir: "/home/" + name})
		h.f.AddDir("/home/"+name, fsMode(u.home), uid, uid)
		return hostfake.Response{}, true
	case "userdel": // userdel(8) --remove NAME: the lines and the home directory
		name := argv[len(argv)-1]
		for _, f := range []string{"/etc/passwd", "/etc/group", accounts.ShadowFile} {
			h.dropLine(f, name)
		}
		_ = h.f.RemoveAll(ctx, "/home/"+name)
		return hostfake.Response{}, true
	case "install": // install(1) -d -m 700 -o NAME -g NAME DIR
		if len(argv) == 9 && argv[1] == "-d" {
			db, _ := accounts.Read(ctx, h.f)
			for _, x := range db.Users {
				if x.Name == argv[5] {
					m, _ := strconv.ParseUint(argv[3], 8, 32)
					h.f.AddDir(argv[8], fsMode(int(m)), x.UID, x.GID)
					return hostfake.Response{}, true
				}
			}
			return hostfake.Response{ExitCode: 1, Stderr: "install: invalid user '" + argv[5] + "'\n"}, true
		}
	}
	return hostfake.Response{}, false
}

// fsMode is m as a file mode.
func fsMode(m int) fs.FileMode { return fs.FileMode(m) }

// appendLine adds line to the file name.
func (h *fakeHost) appendLine(name, line string, mode, gid int) {
	data, _ := h.f.ReadFile(context.Background(), name)
	h.f.AddFile(name, append(data, []byte(line+"\n")...), fsMode(mode), 0, gid)
}

// dropLine removes user's line from the file name.
func (h *fakeHost) dropLine(name, user string) {
	data, err := h.f.ReadFile(context.Background(), name)
	if err != nil {
		return
	}
	var keep []string
	for _, l := range strings.SplitAfter(string(data), "\n") {
		if !strings.HasPrefix(l, user+":") {
			keep = append(keep, l)
		}
	}
	fi, _ := h.f.Stat(context.Background(), name)
	h.f.AddFile(name, []byte(strings.Join(keep, "")), fi.Mode().Perm(), 0, 0)
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

// fingerprint is OpenSSH's SHA256 fingerprint of a key's blob (ssh-keygen -l).
func fingerprint(blob []byte) string {
	sum := sha256.Sum256(blob)
	return "SHA256:" + base64.RawStdEncoding.EncodeToString(sum[:])
}

// usersOn is a users host: statusOn's for platform p, its accounts root and
// us, written as the VMs' are (passwd, group, shadow; the keys 0600 in .ssh
// 0700), root with one key, and cloud-init's sudoers rule for the first of
// us where cloud is set (/etc/sudoers.d/90-cloud-init-users, 0440, the VMs'
// "<user> ALL=(ALL) NOPASSWD:ALL"). Alpine's cloud-init writes that file
// too, where no sudo reads it.
func usersOn(p platform, cloud bool, us ...simUser) *fakeHost {
	h := statusOn(p)
	h.users = &userSim{home: 0o700}
	if p.socket {
		h.users.home = 0o750
	}
	nologin, rootShell := "/usr/sbin/nologin", "/bin/bash"
	tools := []string{"sudo:x:27:", "adm:x:4:"}
	if p.openRC {
		nologin, rootShell = "/sbin/nologin", "/bin/ash"
		tools = []string{"wheel:x:10:root", "adm:x:4:root"}
		h.f.AddFile("/bin/ash", []byte("ELF"), 0o755, 0, 0)
	}
	passwd := "root:x:0:0:root:/root:" + rootShell + "\nsshd:x:100:65534::/run/sshd:" + nologin + "\nnobody:x:65534:65534:nobody:/nonexistent:" + nologin + "\n"
	rootPW := syntheticHash
	if p.openRC {
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
	h.f.AddDir("/root/.ssh", 0o700, 0, 0)
	h.f.AddFile("/root/.ssh/authorized_keys", []byte(synthKey(0x72, "root@admin-01")+"\n"), 0o600, 0, 0)
	for _, u := range us {
		passwd += fmt.Sprintf("%s:x:%d:%d::/home/%s:%s\n", u.name, u.uid, u.uid, u.name, u.shell)
		group += fmt.Sprintf("%s:x:%d:\n", u.name, u.uid)
		shadow += u.name + ":" + u.pw + ":20000:0:99999:7:::\n"
		h.f.AddUser(host.User{Name: u.name, UID: u.uid, GID: u.uid, HomeDir: "/home/" + u.name})
		h.f.AddDir("/home/"+u.name, fsMode(h.users.home), u.uid, u.uid)
		if len(u.keys) > 0 {
			h.f.AddDir("/home/"+u.name+"/.ssh", 0o700, u.uid, u.uid)
			var lines string
			for i, s := range u.keys {
				lines += synthKey(s, fmt.Sprintf("%s@laptop-%d", u.name, i+1)) + "\n"
			}
			h.f.AddFile("/home/"+u.name+"/.ssh/authorized_keys", []byte(lines), 0o600, u.uid, u.uid)
		}
	}
	h.f.AddFile("/etc/passwd", []byte(passwd), 0o644, 0, 0)
	h.f.AddFile("/etc/group", []byte(group), 0o644, 0, 0)
	h.f.AddFile(accounts.ShadowFile, []byte(shadow), 0o640, 0, 42)
	if cloud && len(us) > 0 {
		h.f.AddDir(sudoers.DropInDir, 0o755, 0, 0)
		h.f.AddFile(sudoers.DropInDir+"/90-cloud-init-users", []byte("# Created by cloud-init v. 25.1\n\n# User rules for "+us[0].name+"\n"+us[0].name+" ALL=(ALL) NOPASSWD:ALL\n"), 0o440, 0, 0)
	}
	return h
}

// george is the VMs' login user: uid 1000 (Debian, Ubuntu) or 1001
// (Alpine), in sudo and adm (wheel and adm), no password, two keys.
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

// hardnGrant writes hardn's own sudo grant for user, as sudoers.PlanFor
// writes it (sudoers.md §2.6), or doas's rule on a doas host.
func (h *fakeHost) hardnGrant(user string, g sudoers.Grant) *fakeHost {
	tool := sudoers.ToolSudo
	if h.doas {
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
	mode := fsMode(0o440)
	if tool == sudoers.ToolDoas {
		mode = 0o640
	}
	h.f.AddFile(n.Grant, []byte(sudoers.Header+line+"\n"), mode, 0, 0)
	return h
}

// The users hosts' builders, each as its note in usrHosts says.

func usrDebian13() *fakeHost {
	return usersOn(debian13Platform, true, george(debian13Platform), alice())
}

func usrUbuntu() *fakeHost {
	h := usersOn(ubuntu2604Platform, true, george(ubuntu2604Platform), alice())
	h.f.AddFile("/etc/sudoers", []byte("Defaults env_reset\nroot ALL=(ALL:ALL) ALL\n%admin ALL=(ALL) ALL\n%sudo ALL=(ALL:ALL) ALL\n@includedir /etc/sudoers.d\n"), 0o440, 0, 0)
	return h
}

func usrAlpine() *fakeHost {
	p := alpine323CloudPlatform
	h := usersOn(p, true, simUser{name: "alpine", uid: 1000, groups: []string{"wheel"}, pw: "*", shell: "/bin/ash"}, george(p))
	h.f.AddFile(sudoers.DropInDir+"/90-cloud-init-users", []byte("# Created by cloud-init v. 25.1\n\n# User rules for george\ngeorge ALL=(ALL) NOPASSWD:ALL\n"), 0o440, 0, 0)
	h.f.AddFile("/etc/sudoers", []byte("root ALL=(ALL:ALL) ALL\n#includedir /etc/sudoers.d\n"), 0o440, 0, 0)
	h.f.AddFile(sudoers.DoasConfig, []byte("# See doas.conf(5) for configuration details.\n# Uncomment to allow group \"wheel\" to become root.\n"), 0o640, 0, 0)
	_ = h.f.Remove(context.Background(), sudoers.DoasDir+"/20-wheel.conf")
	h.f.AddFile(sudoers.DoasDir+"/wheel.conf", []byte("permit nopass :wheel\n"), 0o640, 0, 0)
	return h
}

func usrLegacy() *fakeHost {
	g := george(debian13Platform)
	g.groups, g.pw = []string{"adm"}, syntheticHash
	h := usersOn(debian13Platform, false, g)
	h.f.AddFile(sudoers.DropInDir+"/george", []byte("george ALL=(ALL) NOPASSWD: ALL\nDefaults:george env_keep += \"HARDN_CONFIG\"\n"), 0o440, 0, 0)
	return h
}

func usrHardn() *fakeHost {
	g := george(debian13Platform)
	g.groups = []string{"adm"}
	return usersOn(debian13Platform, false, g).hardnGrant("george", sudoers.GrantNoPassword)
}

func usrSecondAdmin() *fakeHost {
	a := alice()
	a.groups, a.keys = nil, []byte{3}
	return usersOn(debian13Platform, true, george(debian13Platform), a).hardnGrant("alice", sudoers.GrantNoPassword)
}

func usrNoKeys() *fakeHost {
	g := george(debian13Platform)
	g.keys = []byte{1}
	return usersOn(debian13Platform, true, g, alice())
}

func usrSudoPassword() *fakeHost {
	g := george(debian13Platform)
	g.groups, g.pw = []string{"adm"}, syntheticHash
	return usersOn(debian13Platform, false, g).hardnGrant("george", sudoers.GrantPassword)
}

func usrAlpineNoRule() *fakeHost {
	g := george(alpine323CloudPlatform)
	g.groups = []string{"adm"}
	h := usersOn(alpine323CloudPlatform, false, g)
	h.f.AddFile(sudoers.DoasConfig, []byte("# See doas.conf(5) for configuration details.\n"), 0o640, 0, 0)
	_ = h.f.Remove(context.Background(), sudoers.DoasDir+"/20-wheel.conf")
	h.f.AddFile(sudoers.DoasDir+"/wheel.conf", []byte("permit nopass :wheel\n"), 0o640, 0, 0)
	return h
}

func usrAlpineLocked() *fakeHost {
	h := usrAlpine()
	h.users = &userSim{home: 0o700}
	a := simUser{name: "alice", uid: 1002, groups: []string{"wheel"}, pw: "!", shell: "/bin/ash", keys: []byte{4}}
	h.appendLine("/etc/passwd", "alice:x:1002:1002::/home/alice:/bin/ash", 0o644, 0)
	h.appendLine(accounts.ShadowFile, "alice:!:20000:0:99999:7:::", 0o640, 42)
	data, _ := h.f.ReadFile(context.Background(), "/etc/group")
	group := strings.Replace(string(data), "wheel:x:10:root,alpine,george", "wheel:x:10:root,alpine,george,alice", 1) + "alice:x:1002:\n"
	h.f.AddFile("/etc/group", []byte(group), 0o644, 0, 0)
	h.f.AddDir("/home/alice/.ssh", 0o700, a.uid, a.uid)
	h.f.AddFile("/home/alice/.ssh/authorized_keys", []byte(synthKey(4, "alice@laptop-1")+"\n"), 0o600, a.uid, a.uid)
	return h
}

// usrYML is hardn.yml naming a user the host does not have.
const usrYML = "username: deploy\nsudoNoPassword: true\nsshKeys:\n  - ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIAEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEB deploy@ci\n"

func usrYMLMissing() *fakeHost { return usrDebian13().withHardnYML(usrYML) }

func usrNotRead() *fakeHost {
	h := usrDebian13()
	h.users.rejects = true
	return h
}

func usrPVE() *fakeHost { return usersOn(pve9Platform, false) }

func usrNoSudo() *fakeHost {
	p := debian13Platform
	p.sudo = ""
	g := george(p)
	g.groups = []string{"adm"}
	return usersOn(p, false, g)
}
