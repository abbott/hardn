package main

import (
	"context"
	"errors"
	"io/fs"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/abbott/hardn/pkg/config"
	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/accounts"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/hostfake"
	"github.com/abbott/hardn/pkg/host/sshd"
	"github.com/abbott/hardn/pkg/host/sudoers"
	"github.com/abbott/hardn/pkg/style"
	"github.com/abbott/hardn/pkg/ui/nav"
	"github.com/abbott/hardn/pkg/ui/prompt"
)

// The User Management host of the User Management item's tests: Debian 13
// as the DNS host has it (newDNSHost, so that -g runs there too), with the
// accounts, sudo and sshd of pkg/screens/users' debian13 host
// (host_test.go, which a test of another package cannot share), cut to
// what Create reads and runs: george, the login user, uid 1000, in sudo,
// no password, cloud-init's NOPASSWD rule; sudo installed setuid root;
// sshd's PAM on. Synthetic values only: george, alice, carol, web-01.

// usrTestHost is the host: the DNS host's fake and facts, sudo's setuid
// bit, and the commands it was not scripted for.
type usrTestHost struct {
	*dnsHost
	t *testing.T

	mu  sync.Mutex
	ran []string // the commands run, as host.FormatArgv gives them
}

// usrTestSSHD is sshd -T on the host: the keywords sshd always prints, its
// PAM on, as pkg/screens/users' model prints them for Debian 13.
const usrTestSSHD = "port 22\nlistenaddress [::]:22\nlistenaddress 0.0.0.0:22\npermitrootlogin prohibit-password\npubkeyauthentication yes\n" +
	"passwordauthentication yes\nkbdinteractiveauthentication no\npermitemptypasswords no\nusepam yes\nhostbasedauthentication no\n" +
	"gssapiauthentication no\nkerberosauthentication no\nauthorizedkeysfile .ssh/authorized_keys .ssh/authorized_keys2\n" +
	"authenticationmethods any\nstrictmodes yes\nauthorizedkeyscommand none\nauthorizedkeyscommanduser none\n" +
	"authorizedprincipalsfile none\nauthorizedprincipalscommand none\nauthorizedprincipalscommanduser none\ntrustedusercakeys none\n" +
	"revokedkeys none\npubkeyauthoptions none\nmaxauthtries 6\nforcecommand none\nchrootdirectory none\n"

// newUsersHost returns the host, and hardn.yml as hardn reads it there
// (newDNSHost's: username alice, Create's default).
func newUsersHost(t *testing.T) (*usrTestHost, *config.Config) {
	t.Helper()
	d, cfg := newDNSHost(t, "debian13", dnsTestList)
	h := &usrTestHost{dnsHost: d, t: t}
	h.facts.SSH = detect.SSH{Mode: detect.SSHService, Generator: detect.No,
		Service: detect.UnitState{Enabled: "enabled", Active: "active"}, Socket: detect.UnitState{Enabled: "disabled", Active: "inactive"}}
	h.facts.Proxmox = detect.Proxmox{Installed: detect.No, ClusterFS: detect.No, EtcPVE: detect.No}
	h.facts.Elevation = detect.Elevation{Sudo: detect.Tool{Present: detect.Yes, Path: "/usr/bin/sudo"}, SudoFlavor: detect.SudoOriginal, Doas: detect.Tool{Present: detect.No}}
	for _, dir := range []string{"/bin", "/usr", "/usr/bin", "/usr/sbin", "/home", "/etc/ssh", sshd.DropInDir, sudoers.DropInDir} {
		h.AddDir(dir, 0o755, 0, 0)
	}
	for _, sh := range []string{"/bin/bash", "/bin/sh", "/usr/sbin/nologin", "/usr/bin/sudo"} {
		h.AddFile(sh, []byte("ELF"), 0o755, 0, 0)
	}
	h.AddFile(sshd.MainConfig, []byte(sshd.IncludeLine+"\n\nUsePAM yes\n"), 0o644, 0, 0)
	h.AddFile(sudoers.MainConfig, []byte(envTestSudoers), 0o440, 0, 0)
	h.AddFile(sudoers.DropInDir+"/90-cloud-init-users", []byte("# Created by cloud-init v. 25.1\n\n# User rules for george\ngeorge ALL=(ALL) NOPASSWD:ALL\n"), 0o440, 0, 0)
	h.AddFile(accounts.PasswdFile, []byte("root:x:0:0:root:/root:/bin/bash\nnobody:x:65534:65534:nobody:/nonexistent:/usr/sbin/nologin\ngeorge:x:1000:1000::/home/george:/bin/bash\n"), 0o644, 0, 0)
	h.AddFile(accounts.GroupFile, []byte("root:x:0:\nsudo:x:27:george\ngeorge:x:1000:\n"), 0o644, 0, 0)
	h.AddFile(accounts.ShadowFile, []byte("root:$y$synthetic$not-a-hash:20000:0:99999:7:::\nnobody:*:20000:0:99999:7:::\ngeorge:!:20000:0:99999:7:::\n"), 0o640, 0, 42)
	h.AddUser(host.User{Name: "root", UID: 0, GID: 0, HomeDir: "/root"})
	h.AddUser(host.User{Name: "george", UID: 1000, GID: 1000, HomeDir: "/home/george"})
	h.AddDir("/home/george", 0o700, 1000, 1000)
	dns := h.Handler
	h.Handler = func(argv []string, in []byte) hostfake.Response {
		h.mu.Lock()
		h.ran = append(h.ran, host.FormatArgv(argv))
		h.mu.Unlock()
		switch cmd := strings.Join(argv, " "); {
		case cmd == "visudo -c":
			return hostfake.Response{Stdout: sudoers.MainConfig + ": parsed OK\n"}
		case len(argv) == 3 && argv[0] == "visudo" && argv[1] == "-cf":
			return hostfake.Response{Stdout: argv[2] + ": parsed OK\n"}
		case (argv[0] == "sshd" || argv[0] == "/usr/sbin/sshd") && slices.Contains(argv, "-T"):
			return hostfake.Response{Stdout: usrTestSSHD}
		case argv[0] == "useradd":
			return h.useradd(argv)
		case argv[0] == "userdel":
			return hostfake.Response{}
		}
		return dns(argv, in)
	}
	return h, cfg
}

// useradd is useradd(8) as Create runs it, [--home-dir D] [--create-home]
// --shell S [--password P] NAME, as pkg/screens/users' model does it: the
// user's own group, the next uid from 1000, "!" without --password, the
// home 0700 with --create-home.
func (h *usrTestHost) useradd(argv []string) hostfake.Response {
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
	uid := 1001
	add := func(file, line string, mode fs.FileMode, gid int) {
		data, _ := h.ReadFile(context.Background(), file)
		h.AddFile(file, append(data, line+"\n"...), mode, 0, gid)
	}
	id := strconv.Itoa(uid)
	add(accounts.PasswdFile, name+":x:"+id+":"+id+"::"+home+":"+shell, 0o644, 0)
	add(accounts.GroupFile, name+":x:"+id+":", 0o644, 0)
	add(accounts.ShadowFile, name+":"+pw+":20000:0:99999:7:::", 0o640, 42)
	h.AddUser(host.User{Name: name, UID: uid, GID: uid, HomeDir: home})
	if mkHome {
		h.AddDir(home, 0o700, uid, uid)
	}
	return hostfake.Response{}
}

// Stat is the fake's, with sudo's setuid bit: hostfake keeps none.
func (h *usrTestHost) Stat(ctx context.Context, name string) (fs.FileInfo, error) {
	fi, err := h.System.Stat(ctx, name)
	if err == nil && name == "/usr/bin/sudo" {
		fi = setuidInfo{fi}
	}
	return fi, err
}

// setuidInfo is a file's information with its setuid bit set.
type setuidInfo struct{ fs.FileInfo }

func (i setuidInfo) Mode() fs.FileMode { return i.FileInfo.Mode() | fs.ModeSetuid }

// commands are the commands run whose name is one of names.
func (h *usrTestHost) commands(names ...string) []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	var out []string
	for _, c := range h.ran {
		if slices.Contains(names, strings.Fields(c)[0]) {
			out = append(out, c)
		}
	}
	return out
}

// useUsersHost points the seams at h as useDNSHost does, the user typing
// lines, the engine's host h itself (its Stat sudo's), and hardn's
// environment george's sudo.
func useUsersHost(t *testing.T, h *usrTestHost, lines ...string) *dnsSeams {
	t.Helper()
	s := useDNSHost(t, h.dnsHost, lines...)
	engineHost = func() host.System { return h }
	t.Setenv("SUDO_USER", "george")
	return s
}

// TestUsersScreen: the main menu's User Management item runs the User
// Management screen on the host hardn builds, through the engine with
// engineOptions, hardn's environment read for the session user (os.Getenv:
// george, through sudo): the screen draws george's row, and Create of
// carol, hardn.yml's username alice offered as its default, under plan
// only stops at its plan, nothing run or written; 0 goes back (nil), q
// quits (nav.ErrQuit), the end of the input aborts (prompt.ErrAborted).
// No change saves hardn.yml.
func TestUsersScreen(t *testing.T) {
	for _, tc := range []struct {
		name   string
		dryRun bool
		lines  []string
		want   error
		said   []string
	}{
		{"back", false, []string{"0"}, nil, []string{"User Management", "george"}},
		{"Create under plan only", true, []string{"2", "carol", "y", "n", "n", "0"}, nil,
			[]string{"Default: alice", "Create user carol", "No actual changes were made to your system."}},
		{"quit", false, []string{"q"}, nav.ErrQuit, nil},
		{"the end of the input", false, nil, prompt.ErrAborted, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h, cfg := newUsersHost(t)
			yml := h.content(dnsTestYML)
			s := useUsersHost(t, h, tc.lines...)
			cfg.DryRun = tc.dryRun
			err := runUsersScreen(context.Background(), cfg)
			if tc.want == nil && err != nil || tc.want != nil && !errors.Is(err, tc.want) {
				t.Fatalf("runUsersScreen = %v, want %v; screen:\n%s", err, tc.want, s.out.String())
			}
			out := style.StripAnsi(s.out.String())
			if w := inOrder(out, tc.said...); w != "" {
				t.Errorf("the screen lacks %q:\n%s", w, out)
			}
			if got := h.writes(); len(got) > 0 || len(h.commands("useradd")) > 0 || s.applies != 0 {
				t.Errorf("wrote %q, ran %q, %d applies", got, h.commands("useradd"), s.applies)
			}
			if h.content(dnsTestYML) != yml {
				t.Errorf("hardn.yml changed:\n%s", h.content(dnsTestYML))
			}
		})
	}
}

// TestUsersHook: withUsersScreen sets the User Management item's screen on
// the configuration main gives it, plan only set after the hook stopping
// Create at its plan.
func TestUsersHook(t *testing.T) {
	h, cfg := newUsersHost(t)
	s := useUsersHost(t, h, "2", "carol", "y", "n", "n", "0")
	set := &hookSet{}
	if got := withUsersScreen(set, cfg); got != set || set.users == nil {
		t.Fatal("withUsersScreen did not set the User Management item on the menu given")
	}
	cfg.DryRun = true
	if err := set.users(); err != nil {
		t.Fatalf("the User Management item = %v; screen:\n%s", err, s.out.String())
	}
	if got := h.writes(); len(got) > 0 || !strings.Contains(s.out.String(), "No actual changes were made to your system.") || s.applies != 0 {
		t.Errorf("wrote %q, %d applies; screen:\n%s", got, s.applies, s.out.String())
	}
}
