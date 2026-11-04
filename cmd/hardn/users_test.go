package main

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"io/fs"
	"os"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/abbott/hardn/pkg/config"
	"github.com/abbott/hardn/pkg/engine"
	"github.com/abbott/hardn/pkg/engine/hold"
	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/accounts"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/hostfake"
	"github.com/abbott/hardn/pkg/host/sshd"
	"github.com/abbott/hardn/pkg/host/sudoers"
	"github.com/abbott/hardn/pkg/output"
	"github.com/abbott/hardn/pkg/screens/mainmenu"
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

// usrTestUID is the uid, and the gid, useradd gives the user it creates.
const usrTestUID = 1001

// usrTestKey is a synthetic ED25519 public key, its 32 bytes seed, as
// pkg/engine/controls/users' tests make them: no one's.
func usrTestKey(seed byte, comment string) string {
	blob := binary.BigEndian.AppendUint32(nil, 11)
	blob = append(blob, "ssh-ed25519"...)
	blob = binary.BigEndian.AppendUint32(blob, 32)
	for range 32 {
		blob = append(blob, seed)
	}
	return "ssh-ed25519 " + base64.StdEncoding.EncodeToString(blob) + " " + comment
}

// withKeys rewrites h's hardn.yml with username alice and sshKeys keys,
// and returns it as hardn reads it.
func withKeys(t *testing.T, h *usrTestHost, keys ...string) *config.Config {
	t.Helper()
	yml := "username: alice\nsshKeys:"
	if len(keys) == 0 {
		yml += " []"
	}
	yml += "\n"
	for _, k := range keys {
		yml += "  - " + strconv.Quote(k) + "\n"
	}
	h.AddFile(dnsTestYML, []byte(yml), 0o644, 0, 0)
	cfg, err := config.Read(context.Background(), h, dnsTestYML)
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

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
		case argv[0] == "install": // install -d -m 700 -o NAME -g NAME DIR: ~/.ssh, useradd's user's
			h.AddDir(argv[len(argv)-1], 0o700, usrTestUID, usrTestUID)
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
	uid := usrTestUID
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
			out := stripANSI(s.out.String())
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
	set := withUsersScreen(mainmenu.Options{}, cfg)
	if set.Users == nil {
		t.Fatal("withUsersScreen did not set the User Management item on the menu given")
	}
	cfg.DryRun = true
	if err := set.Users(); err != nil {
		t.Fatalf("the User Management item = %v; screen:\n%s", err, s.out.String())
	}
	if got := h.writes(); len(got) > 0 || !strings.Contains(s.out.String(), "No actual changes were made to your system.") || s.applies != 0 {
		t.Errorf("wrote %q, %d applies; screen:\n%s", got, s.applies, s.out.String())
	}
}

// runC runs hardn with args as root (runG) on the User Management host h,
// its configuration cfg as loaded, hardn's environment george's sudo;
// setup, when set, run once the seams are in place.
func runC(t *testing.T, h *usrTestHost, cfg *config.Config, setup func(*dnsSeams), args ...string) (result, *dnsSeams) {
	t.Helper()
	return runG(t, h.dnsHost, cfg, func(s *dnsSeams) {
		engineHost = func() host.System { return h }
		t.Setenv("SUDO_USER", "george")
		if setup != nil {
			setup(s)
		}
	}, args...)
}

// TestCreateUserFlag: -c plans and applies the User control's Create
// through the engine, without a question, as -g applies DNS's (refactor
// plan slice 5, unit (d); gallery-users.md §5, decision 6 (a), adopted
// 2026-11-03): the user -u names, else hardn.yml's username (decision 7
// (a)), with admin rights asking no password and no key, the plan's
// USERS_NO_KEY finding said; useradd run, hardn's grant written, exit
// status 0. -n -c the plan alone, nothing run or written, 3. A name taken,
// one useradd would refuse, or one whose home is there: the control's
// refusal, its code, nothing run or written, 1, under -n too. sshd's
// configuration is not touched (origin/main's -c rewrote it: slice 6's
// now), hardn.yml is never saved, nothing goes through the legacy
// provider, and the signals are registered for an apply alone.
func TestCreateUserFlag(t *testing.T) {
	for _, tc := range []struct {
		name    string
		args    []string
		home    bool // /home/carol already there
		code    int  // the exit status; 0: hardn did not exit
		said    []string
		applied string // the user created; "" none
	}{
		{name: "-c -u carol", args: []string{"-c", "-u", "carol"}, applied: "carol",
			said: []string{"Create user carol", "Let carol use sudo", "Operation Successful", "Without a key carol can't log in", "USERS_NO_KEY"}},
		{name: "-c: hardn.yml's username", args: []string{"-c"}, applied: "alice",
			said: []string{"Create user alice", "Let alice use sudo", "Operation Successful"}},
		{name: "-n -c -u carol", args: []string{"-n", "-c", "-u", "carol"}, code: 3,
			said: []string{"Create user carol", "Let carol use sudo", "No actual changes were made to your system."}},
		{name: "a name taken", args: []string{"-c", "-u", "george"}, code: 1, said: []string{"george already exists", "USERS_NAME_TAKEN"}},
		{name: "a name taken, -n", args: []string{"-n", "-c", "-u", "george"}, code: 1, said: []string{"USERS_NAME_TAKEN"}},
		{name: "a name useradd refuses", args: []string{"-c", "-u", "Carol"}, code: 1, said: []string{"USERS_NAME_FORM"}},
		{name: "its home there", args: []string{"-c", "-u", "carol"}, home: true, code: 1, said: []string{"/home/carol is already there", "USERS_HOME_THERE"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h, cfg := newUsersHost(t)
			if tc.home {
				h.AddDir("/home/carol", 0o755, 0, 0)
			}
			yml, sshdConfig := h.content(dnsTestYML), h.content(sshd.MainConfig)
			r, s := runC(t, h, cfg, nil, tc.args...)
			report := stripANSI(s.out.String())
			if got := map[bool]int{true: r.code}[r.ended]; got != tc.code {
				t.Fatalf("exit status %d (ended %v), want %d; report:\n%s\nstdout:\n%s\nstderr:\n%s", r.code, r.ended, tc.code, report, r.stdout, r.stderr)
			}
			if w := inOrder(report, tc.said...); w != "" {
				t.Errorf("the report lacks %q after the text before it:\n%s", w, report)
			}
			for _, w := range []string{"y/N", "Type the hostname", "[SUCCESS]", "[ERROR]", "Script completed", "created successfully"} {
				if strings.Contains(report+r.stdout, w) {
					t.Errorf("printed %q:\n%s%s", w, report, r.stdout)
				}
			}
			ran := h.commands("useradd")
			if tc.applied == "" {
				if got := h.writes(); len(got) > 0 || len(ran) > 0 {
					t.Errorf("wrote %q, ran %q", got, ran)
				}
			} else {
				want := "useradd --home-dir /home/" + tc.applied + " --create-home --shell /bin/bash " + tc.applied
				grant := sudoers.DropInDir + "/hardn-grant-" + tc.applied
				if !slices.Equal(ran, []string{want}) || !strings.Contains(h.content(grant), tc.applied+" ALL=(ALL) NOPASSWD: ALL") {
					t.Errorf("ran %q, %s:\n%s\nwant %q and hardn's grant asking no password", ran, grant, h.content(grant), want)
				}
				if _, ok := h.Entry("/home/" + tc.applied + "/.ssh"); ok {
					t.Error("-c added a key")
				}
			}
			if h.content(sshd.MainConfig) != sshdConfig || slices.ContainsFunc(h.writes(), func(w string) bool { return strings.Contains(w, " /etc/ssh/") }) {
				t.Errorf("sshd's configuration written: %q", h.writes())
			}
			if h.content(dnsTestYML) != yml {
				t.Errorf("hardn.yml changed:\n%s", h.content(dnsTestYML))
			}
			if want := map[bool]int{true: 1}[tc.applied != ""]; s.applies != want || s.live != 0 {
				t.Errorf("signals registered %d times, want %d; %d left", s.applies, want, s.live)
			}
		})
	}
}

// TestCreateUserKeys: -c adds every key in hardn.yml's sshKeys to the user
// it creates (the maintainer, 2026-11-03: "(a) Add sshKeys"), in one
// change, the file 0600 and the user's, and no USERS_NO_KEY; -n -c shows
// them, nothing run or written, 3. A key that is not one, or an empty
// entry, is the control's refusal, naming which, before anything is
// written: no useradd, 1.
func TestCreateUserKeys(t *testing.T) {
	k1, k2 := usrTestKey(5, "carol@laptop"), usrTestKey(6, "carol@phone")
	for _, tc := range []struct {
		name string
		args []string
		keys []string
		code int // the exit status; 0: hardn did not exit
		said []string
	}{
		{"-c -u carol", []string{"-c", "-u", "carol"}, []string{k1, k2}, 0, []string{"Create user carol", "Let carol use sudo", "Add carol's keys", "Operation Successful"}},
		{"-n -c -u carol", []string{"-n", "-c", "-u", "carol"}, []string{k1, k2}, 3, []string{"Create user carol", "Add carol's keys", "carol@laptop", "carol@phone", "No actual changes were made to your system."}},
		{"a bad second key", []string{"-c", "-u", "carol"}, []string{k1, "ssh-rsa AAAA carol@old"}, 1, []string{"Key 2 isn't an OpenSSH public key", "AUTHKEYS_NOT_KEY"}},
		{"an empty entry", []string{"-c", "-u", "carol"}, []string{k1, ""}, 1, []string{"Key 2 isn't an OpenSSH public key", "AUTHKEYS_NOT_KEY"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h, _ := newUsersHost(t)
			cfg := withKeys(t, h, tc.keys...)
			r, s := runC(t, h, cfg, nil, tc.args...)
			report := stripANSI(s.out.String())
			if got := map[bool]int{true: r.code}[r.ended]; got != tc.code {
				t.Fatalf("exit status %d (ended %v), want %d; report:\n%s\nstderr:\n%s", r.code, r.ended, tc.code, report, r.stderr)
			}
			if w := inOrder(report, tc.said...); w != "" {
				t.Errorf("the report lacks %q after the text before it:\n%s", w, report)
			}
			if strings.Contains(report, "USERS_NO_KEY") {
				t.Errorf("USERS_NO_KEY with keys:\n%s", report)
			}
			if tc.code != 0 {
				if got := h.writes(); len(got) > 0 || len(h.commands("useradd")) > 0 {
					t.Errorf("wrote %q, ran %q", got, h.commands("useradd"))
				}
				return
			}
			k, _ := h.Entry("/home/carol/.ssh/authorized_keys")
			if want := k1 + "\n" + k2 + "\n"; string(k.Data) != want || k.Mode.Perm() != 0o600 || k.UID != usrTestUID || k.GID != usrTestUID {
				t.Errorf("keys %q %v %d:%d, want %q 0600 %d:%d", k.Data, k.Mode, k.UID, k.GID, want, usrTestUID, usrTestUID)
			}
		})
	}
}

// TestCreateUserPlanOnlyFromFile: hardn.yml's dryRun: true makes -c the
// plan alone, as -n does: nothing run or written, 3; --dry-run=false over
// it applies as live, 0.
func TestCreateUserPlanOnlyFromFile(t *testing.T) {
	for _, tc := range []struct {
		args []string
		code int
	}{
		{[]string{"-c", "-u", "carol"}, 3},
		{[]string{"--dry-run=false", "-c", "-u", "carol"}, 0},
	} {
		t.Run(strings.Join(tc.args, " "), func(t *testing.T) {
			h, _ := newUsersHost(t)
			h.AddFile(dnsTestYML, []byte("username: alice\ndryRun: true\n"), 0o644, 0, 0)
			cfg, err := config.Read(context.Background(), h, dnsTestYML)
			if err != nil {
				t.Fatal(err)
			}
			r, s := runC(t, h, cfg, nil, tc.args...)
			report := stripANSI(s.out.String())
			if got := map[bool]int{true: r.code}[r.ended]; got != tc.code {
				t.Fatalf("exit status %d (ended %v), want %d; report:\n%s\nstderr:\n%s", r.code, r.ended, tc.code, report, r.stderr)
			}
			live := tc.code == 0
			if strings.Contains(report, "No actual changes were made to your system.") == live || (len(h.commands("useradd")) > 0) != live {
				t.Errorf("useradd ran %q, want applied %t; report:\n%s", h.commands("useradd"), live, report)
			}
		})
	}
}

// TestCreateUserNoName: -c with no -u and no username in hardn.yml is
// refused as it was, in its words, on stderr (say), exit status 1, before the engine's host
// is reached (runHardnWith fails the test if it is).
func TestCreateUserNoName(t *testing.T) {
	for _, args := range [][]string{{"-c"}, {"-n", "-c"}} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			r := runHardnWith(t, asRoot, func() {
				loadConfig = func(string) (*config.Config, error) { return config.DefaultConfig(), nil }
			}, args...)
			if !r.ended || r.code != 1 || !strings.HasSuffix(flat(r.stderr), "Operation Failed Please specify a username with -u flag or in the configuration file.") || r.stdout != "" {
				t.Errorf("ended %v with %d; stdout:\n%s\nstderr:\n%s", r.ended, r.code, r.stdout, r.stderr)
			}
		})
	}
}

// TestCreateUserFlagWithOthers: -c's exit status combines with -g's as
// -w's does (runStatus): -g runs after -c's refusal, and the refusal is
// hardn's exit status once it has; -c applied and -g applied, 0; under
// plan only both plans shown, nothing written, 3, and -c's refusal, 1,
// over -g's plan.
func TestCreateUserFlagWithOthers(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
		code int // the exit status; 0: hardn did not exit
		said []string
		dns  bool // -g's file written
	}{
		{"-c refused, then -g", []string{"-c", "-u", "george", "-g"}, 1, []string{"USERS_NAME_TAKEN", "Set the nameservers  Done"}, true},
		{"-c applied, then -g", []string{"-c", "-u", "carol", "-g"}, 0, []string{"Create user carol", "Set the nameservers  Done"}, true},
		{"-n: -c's plan, then -g's", []string{"-n", "-c", "-u", "carol", "-g"}, 3, []string{"Create user carol", "+nameserver 192.0.2.53"}, false},
		{"-n: -c refused, then -g's plan", []string{"-n", "-c", "-u", "george", "-g"}, 1, []string{"USERS_NAME_TAKEN", "+nameserver 192.0.2.53"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h, cfg := newUsersHost(t)
			r, s := runC(t, h, cfg, nil, tc.args...)
			report := stripANSI(s.out.String())
			if got := map[bool]int{true: r.code}[r.ended]; got != tc.code {
				t.Fatalf("exit status %d (ended %v), want %d; report:\n%s\nstderr:\n%s", r.code, r.ended, tc.code, report, r.stderr)
			}
			if w := inOrder(report, tc.said...); w != "" {
				t.Errorf("the report lacks %q after the text before it:\n%s", w, report)
			}
			if written := slices.Contains(h.writes(), "write-atomic /etc/resolv.conf"); written != tc.dns {
				t.Errorf("-g's file written %t, want %t: %q", written, tc.dns, h.writes())
			}
		})
	}
}

// TestCreateUserRefused: -c hands its guard (createRefuse) to runControls:
// a plan it refuses, Create's here taken as lockout-risk, is shown and
// refused, PLAN_REFUSED, exit status 1, nothing run or written. The guard
// is refuseCreateLockoutRisk.
func TestCreateUserRefused(t *testing.T) {
	if reflect.ValueOf(createRefuse).Pointer() != reflect.ValueOf(refuseCreateLockoutRisk).Pointer() {
		t.Fatal("-c's guard is not refuseCreateLockoutRisk")
	}
	h, cfg := newUsersHost(t)
	was := createRefuse
	t.Cleanup(func() { createRefuse = was })
	r, s := runC(t, h, cfg, func(*dnsSeams) {
		createRefuse = func(*engine.Plan) error { return refuseCreateLockoutRisk(&engine.Plan{Risk: engine.RiskLockout}) }
	}, "-c", "-u", "carol")
	report := stripANSI(s.out.String())
	if !r.ended || r.code != 1 {
		t.Fatalf("exit status %d (ended %v), want 1; report:\n%s\nstderr:\n%s", r.code, r.ended, report, r.stderr)
	}
	if w := inOrder(report, "Create user carol", "PLAN_REFUSED"); w != "" {
		t.Errorf("the report lacks %q after the text before it:\n%s", w, report)
	}
	if got := h.writes(); len(got) > 0 || len(h.commands("useradd")) > 0 || s.applies != 0 {
		t.Errorf("wrote %q, ran %q, %d applies", got, h.commands("useradd"), s.applies)
	}
}

// TestRefuseCreateLockoutRisk: -c's guard refuses a lockout-risk plan,
// PLAN_REFUSED, and leaves a plain one, Create's, to the engine.
func TestRefuseCreateLockoutRisk(t *testing.T) {
	if err := refuseCreateLockoutRisk(&engine.Plan{Risk: engine.RiskLockout}); !errors.Is(err, engine.ErrRefused) {
		t.Errorf("a lockout-risk plan: %v, want a refusal wrapping engine.ErrRefused", err)
	}
	if err := refuseCreateLockoutRisk(&engine.Plan{Risk: engine.RiskPlain}); err != nil {
		t.Errorf("a plain plan refused: %v", err)
	}
}

// TestUsersScreenSignals: Ctrl-C, SIGTERM and a hangup, each sent to this
// test process as useradd runs in Create of carol on the User Management
// screen, as the kernel delivers it, cancel that apply: the change is
// undone under the engine's own hold, userdel --remove carol run and
// hardn's grant for carol never written, the report saying the apply was
// cancelled and Create rolled back, naming hardn's log; the user then
// goes back (nil). After the hangup hardn ends (endHangup, recorded here),
// once, as the apply returns; never after the others.
func TestUsersScreenSignals(t *testing.T) {
	grant := sudoers.DropInDir + "/hardn-grant-carol"
	for _, sig := range []syscall.Signal{syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP} {
		t.Run(sig.String(), func(t *testing.T) {
			signals := engineSignals // the real one: useDNSHost replaces it
			h, cfg := newUsersHost(t)
			s := useUsersHost(t, h, "2", "carol", "y", "n", "n", "y", "0")
			engineHold = func() func() { s.held++; return hold.Signals(nil) } // the engine's own, counted
			var applying context.Context
			stopped := 0
			engineSignals = func(ctx context.Context) (context.Context, context.CancelFunc) {
				s.applies++
				c, stop := signals(ctx)
				applying = c
				return c, func() { stopped++; stop() }
			}
			type end struct {
				stopped, held int
				grant         string
			}
			var ended []end
			endHangup = func() { ended = append(ended, end{stopped, s.held, h.content(grant)}) }
			answer := h.Handler
			h.Handler = func(argv []string, in []byte) hostfake.Response {
				r := answer(argv, in)
				if argv[0] != "useradd" {
					return r
				}
				if err := syscall.Kill(os.Getpid(), sig); err != nil {
					t.Error(err)
				}
				select {
				case <-applying.Done():
				case <-time.After(5 * time.Second):
					t.Errorf("%v did not cancel the apply", sig)
				}
				return r // useradd itself succeeds
			}
			if err := runUsersScreen(context.Background(), cfg); err != nil {
				t.Fatalf("runUsersScreen = %v; screen:\n%s", err, s.out.String())
			}
			out := stripANSI(s.out.String())
			if s.applies != 1 || s.held != 1 || h.content(grant) != "<absent>" || !slices.Equal(h.commands("userdel"), []string{"userdel --remove carol"}) {
				t.Errorf("%d applies, %d holds, ran %q; %s:\n%s\nscreen:\n%s", s.applies, s.held, h.commands("useradd", "userdel"), grant, h.content(grant), out)
			}
			if sig == syscall.SIGHUP {
				if want := (end{1, 1, "<absent>"}); len(ended) != 1 || ended[0] != want {
					t.Errorf("hardn ended %+v, want %+v", ended, want)
				}
				return
			}
			if len(ended) != 0 {
				t.Errorf("%v ended hardn (%d)", sig, len(ended))
			}
			if w := inOrder(out, "(CANCELLED)", "Create user carol    Rolled back", "Check the log file at "+output.LegacyLogPath); w != "" {
				t.Errorf("the report does not say the change was cancelled, naming %s:\n%s", output.LegacyLogPath, out)
			}
		})
	}
}

// TestUsersScreenSessionUser: the screen reads hardn's environment for the
// session user (os.Getenv): with george's sudo (SUDO_USER=george), george
// an admin through hardn's grant alone, Remove admin rights on george is
// refused before asking in the session-user words, not the root login's
// (hardn can't tell who you're logged in as), nothing run or written.
func TestUsersScreenSessionUser(t *testing.T) {
	h, cfg := newUsersHost(t)
	h.AddFile(accounts.GroupFile, []byte("root:x:0:\nsudo:x:27:\ngeorge:x:1000:\n"), 0o644, 0, 0)
	h.AddFile(sudoers.DropInDir+"/90-cloud-init-users", []byte("# Created by cloud-init v. 25.1\n"), 0o440, 0, 0)
	h.AddFile(sudoers.DropInDir+"/hardn-grant-george", []byte(sudoers.Header+"george ALL=(ALL) NOPASSWD: ALL\n"), 0o440, 0, 0)
	s := useUsersHost(t, h, "1", "1", "1", "0", "0", "0")
	if err := runUsersScreen(context.Background(), cfg); err != nil {
		t.Fatalf("runUsersScreen = %v; screen:\n%s", err, s.out.String())
	}
	out := strings.Join(strings.Fields(strings.ReplaceAll(stripANSI(s.out.String()), "#", "")), " ") // the warning's lines joined
	if !strings.Contains(out, "You're running hardn through george's sudo. Removing it would leave no way to become root") || strings.Contains(out, "hardn can't tell who you're logged in as") {
		t.Errorf("the screen does not say the session-user refusal:\n%s", out)
	}
	if got := h.writes(); len(got) > 0 || s.applies != 0 || strings.Contains(out, "y/N") {
		t.Errorf("wrote %q, %d applies; screen:\n%s", got, s.applies, out)
	}
}
