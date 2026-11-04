package runall_test

import (
	"context"
	"fmt"
	"io/fs"
	"strconv"
	"strings"

	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/accounts"
	"github.com/abbott/hardn/pkg/host/hostfake"
)

// account answers useradd, userdel and install -d as shadow's and
// coreutils' tools do them, so that Apply all creates carol: copied from
// pkg/screens/users' host_test.go (the gallery's userSim, usershost.go),
// the caller holding h.mu.
func (h *sshHost) account(argv []string) (hostfake.Response, bool) {
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
func (h *sshHost) appendLine(name, line string, mode fs.FileMode, gid int) {
	data, _ := h.ReadFile(context.Background(), name)
	h.AddFile(name, append(data, []byte(line+"\n")...), mode, 0, gid)
}

// dropLine removes user's line from the file name.
func (h *sshHost) dropLine(name, user string) {
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
