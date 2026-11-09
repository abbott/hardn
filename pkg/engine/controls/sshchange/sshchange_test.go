package sshchange_test

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/abbott/hardn/pkg/config"
	"github.com/abbott/hardn/pkg/engine"
	"github.com/abbott/hardn/pkg/engine/controls/sshchange"
	"github.com/abbott/hardn/pkg/engine/steps"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/reload"
	"github.com/abbott/hardn/pkg/host/revert"
	"github.com/abbott/hardn/pkg/host/revert/plans"
	"github.com/abbott/hardn/pkg/host/sshd"
	"github.com/abbott/hardn/pkg/host/sshd/migrate"
	"github.com/abbott/hardn/pkg/output"
	"github.com/abbott/hardn/pkg/screens/screenkit"
)

// read is Read on v, as the screen reads it.
func read(t *testing.T, v *vm, getenv func(string) string) sshchange.Reading {
	t.Helper()
	r, err := sshchange.Read(context.Background(), v.sys(), v.facts, &config.Config{}, v.session, getenv)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

// paths is LoginPaths as "<name> key", "<name>: <why>", with ", password"
// where a password still lets them in.
func paths(t *testing.T, v *vm, keyOnly bool) string {
	t.Helper()
	var got []string
	for _, a := range read(t, v, sudoGeorge).LoginPaths(context.Background(), v.sys(), keyOnly) {
		s := a.Name + " key"
		if !a.Key {
			s = a.Name + ": " + a.Why
		}
		if a.Password {
			s += ", password"
		}
		got = append(got, s)
	}
	return strings.Join(got, "|")
}

// TestReadSysconfig: on Rocky Linux (sshd.service, detect's SSHDUnits) an
// /etc/sysconfig/sshd whose OPTIONS gives sshd arguments leaves sshd's
// effective configuration unread for the screen (EffErr); the stock file
// reads as Debian does.
func TestReadSysconfig(t *testing.T) {
	for _, c := range []struct {
		options string
		want    error
	}{{`""`, nil}, {`"-o PermitRootLogin=yes"`, reload.ErrSysconfig}} {
		v := debianVM(t)
		v.facts.Platform = detect.Platform{Distro: detect.DistroRocky, Version: "10", Support: detect.Unlisted}
		v.facts.SSH.SSHDUnits = true
		v.f.AddFile(reload.SysconfigSSHD, []byte("OPTIONS="+c.options+"\n"), 0o644, 0, 0)
		if r := read(t, v, sudoGeorge); !errors.Is(r.EffErr, c.want) || (c.want == nil) != (r.EffErr == nil) {
			t.Errorf("OPTIONS=%s: EffErr %v, want %v", c.options, r.EffErr, c.want)
		}
	}
}

// TestRead: the reading of the Debian 13 VM, and that Read runs only
// reads: these commands, and no write.
func TestRead(t *testing.T) {
	v := debianVM(t)
	r := read(t, v, sudoGeorge)
	switch {
	case r.EffErr != nil || r.FoundErr != nil || r.UsersErr != nil || r.LoginErr != nil:
		t.Fatalf("errors: %v, %v, %v, %v", r.EffErr, r.FoundErr, r.UsersErr, r.LoginErr)
	case r.Login != sshd.Accepted{User: "george", Method: "publickey", KeyType: "ED25519", Fingerprint: "SHA256:c3ludGhldGljLWtleS1vZi1nZW9yZ2UtbGFwdG9wLTE"}:
		t.Errorf("login %+v", r.Login)
	case r.RootValue() != "prohibit-password" || r.PasswordsOn() || r.PVE || r.RootLocked || r.ListenPort != "" || r.OnlyFrom != nil:
		t.Errorf("root %q, passwords %v, pve %v, locked %v, listen %q, only from %v", r.RootValue(), r.PasswordsOn(), r.PVE, r.RootLocked, r.ListenPort, r.OnlyFrom)
	case r.DecidedBy("passwordauthentication") != "sshd_config" || r.DecidedBy("permitrootlogin") != "":
		t.Errorf("decided by %q, %q", r.DecidedBy("passwordauthentication"), r.DecidedBy("permitrootlogin"))
	case r.Root.Name != "root" || len(r.Users.Users) != 2:
		t.Errorf("root %+v, users %d", r.Root, len(r.Users.Users))
	}
	var cmds []string
	for _, c := range v.f.Calls() {
		switch c.Op {
		case "read", "read-nofollow", "stat", "lstat", "readdir", "readlink", "resolve-symlink", "lookup-user", "lookup-group", "scan-lines":
		case "run":
			if cmd := strings.Join(c.Argv, " "); !slices.Contains(cmds, cmd) {
				cmds = append(cmds, cmd)
			}
		default:
			t.Errorf("Read wrote: %v", c)
		}
	}
	want := []string{
		"sshd -T -f /etc/ssh/sshd_config",
		"visudo -c",
		"journalctl -q --no-pager -b -o short-iso -u ssh -u sshd",
		"ufw status verbose",
	}
	if !slices.Equal(cmds, want) {
		t.Errorf("commands:\n%s\nwant:\n%s", strings.Join(cmds, "\n"), strings.Join(want, "\n"))
	}
	if vc := read(t, v.console(), sudoGeorge); vc.Login != (sshd.Accepted{}) || vc.LoginErr != nil || !vc.Console() {
		t.Errorf("console: login %+v, %v", vc.Login, vc.LoginErr)
	}
}

// TestAdminPaths holds LoginPaths, each counterpart of the gallery's
// TestSSHAdminPaths: sshd's own answer for each admin, then hardn's
// matching, the key, the methods and becoming root.
func TestAdminPaths(t *testing.T) {
	site := func(conf string) func(*testing.T) *vm {
		return func(t *testing.T) *vm { return debianVM(t).file(sshd.DropInDir+"/10-site.conf", conf) }
	}
	for _, tt := range []struct {
		name    string
		host    func(*testing.T) *vm
		keyOnly bool
		want    string
	}{
		{"debian13", debianVM, true, "george key|alice: alice has no key sshd reads"},
		{"allow-users", func(t *testing.T) *vm {
			a := alice
			a.keys = []string{key(3, "alice@laptop-3")}
			return newVM(t, debian13(), true, george, a).file(sshd.DropInDir+"/10-allow.conf", "AllowUsers george\n")
		}, true, "george key|alice: alice isn't in AllowUsers"},
		{"nopam-locked (UsePAM no for Alpine's build without PAM)", func(t *testing.T) *vm {
			v := newVM(t, debian13(), true, george).file(sshd.MainConfig, strings.Replace(debianMain, "UsePAM yes", "UsePAM no", 1)).console()
			return v
		}, true, "george: george's account is locked (!) and sshd has no PAM"},
		{"match-allows", site("Match Address 192.0.2.0/24\n    PermitRootLogin yes\n    PasswordAuthentication yes\n"), false, "george key|alice: alice has no key sshd reads, password"},
		{"allow-users-host", site("AllowUsers george@198.51.100.7\n"), true, "george: AllowUsers lets george in from 198.51.100.7 alone|alice: alice isn't in AllowUsers"},
		{"deny-users", site("DenyUsers george\n"), true, "george: george is in DenyUsers|alice: alice has no key sshd reads"},
		{"pubkey-off", site("PubkeyAuthentication no\n"), true, "george: PubkeyAuthentication is off|alice: PubkeyAuthentication is off"},
		{"methods-password", site("AuthenticationMethods publickey,password\nPasswordAuthentication yes\n"), false, "george: AuthenticationMethods publickey,password asks more than george has|alice: alice has no key sshd reads"},
		{"strict-home", func(t *testing.T) *vm {
			v := debianVM(t)
			v.f.AddDir("/home/george", 0o775, 1000, 1000)
			return v
		}, true, "george: StrictModes ignores george's keys|alice: alice has no key sshd reads"},
		{"key-elsewhere", func(t *testing.T) *vm {
			v := debianVM(t).file(sshd.DropInDir+"/10-site.conf", "AuthorizedKeysFile .ssh/authorized_keys\n")
			e, _ := v.f.Entry("/home/george/.ssh/authorized_keys")
			_ = v.f.Remove(context.Background(), "/home/george/.ssh/authorized_keys")
			v.f.AddFile("/home/george/.ssh/authorized_keys2", e.Data, 0o600, 1000, 1000)
			return v
		}, true, "george: george has no key sshd reads|alice: alice has no key sshd reads"},
		{"algorithms-rsa", site("PubkeyAcceptedAlgorithms rsa-sha2-512,rsa-sha2-256\n"), true, "george: sshd doesn't accept george's key type (ssh-ed25519)|alice: alice has no key sshd reads"},
		{"rsa-small", func(t *testing.T) *vm { return rsaSmall(t, debianVM(t)) }, true,
			"george: george's RSA key is 1024 bits, under RequiredRSASize 2048|alice: alice has no key sshd reads"},
		{"admin-no-sudo", func(t *testing.T) *vm { return newVM(t, debian13(), false, george).console() }, true,
			"george: george can't become root: sudo asks a password george hasn't got"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := paths(t, tt.host(t), tt.keyOnly); got != tt.want {
				t.Errorf("%q\nwant %q", got, tt.want)
			}
		})
	}
}

// rsaSmall gives george one RSA key of 1024 bits and sshd RequiredRSASize
// 2048, and hardn a console session.
func rsaSmall(t *testing.T, v *vm) *vm {
	v.f.AddFile("/home/george/.ssh/authorized_keys", []byte(rsaKey(1024, 0x41, "george@laptop-1")+"\n"), 0o600, 1000, 1000)
	return v.file(sshd.DropInDir+"/10-rsa.conf", "RequiredRSASize 2048\n").console()
}

// TestGuards: the gallery's TestSSHGuards' paths both ways, able and not:
// AllowGroups, an AllowUsers CIDR, restricted keys; and the session's
// line found by its address as well as its port.
func TestGuards(t *testing.T) {
	george := func(t *testing.T, v *vm) sshchange.Admin {
		for _, a := range read(t, v, sudoGeorge).LoginPaths(context.Background(), v.sys(), true) {
			if a.Name == "george" {
				return a
			}
		}
		return sshchange.Admin{}
	}
	t.Run("AllowGroups", func(t *testing.T) {
		v := debianVM(t).file(sshd.DropInDir+"/10-site.conf", "AllowGroups sudo\n")
		if a := george(t, v); a.Key || a.Why != "AllowGroups and DenyGroups aren't read" {
			t.Errorf("george: %+v, want not able, groups not read", a)
		}
	})
	t.Run("AllowUsers with a CIDR", func(t *testing.T) {
		for cidr, able := range map[string]bool{"192.0.2.0/24": true, "198.51.100.0/24": false} {
			v := debianVM(t).file(sshd.DropInDir+"/10-site.conf", "AllowUsers george@"+cidr+"\n")
			if a := george(t, v); a.Key != able {
				t.Errorf("AllowUsers george@%s, the session from 192.0.2.10: %+v, want able %v", cidr, a, able)
			}
		}
	})
	t.Run("AllowUsers with a CIDR, from a console", func(t *testing.T) {
		v := debianVM(t).file(sshd.DropInDir+"/10-site.conf", "AllowUsers george@192.0.2.0/24\n").console()
		if a := george(t, v); a.Key || a.Why != "AllowUsers lets george in from 192.0.2.0/24 alone" {
			t.Errorf("from a console: %+v, want not able", a)
		}
	})
	t.Run("DenyUsers with a host, from a console", func(t *testing.T) {
		v := debianVM(t).file(sshd.DropInDir+"/10-site.conf", "DenyUsers george@198.51.100.7\n")
		if a := george(t, v); !a.Key {
			t.Errorf("over SSH from 192.0.2.10: %+v, want able", a)
		}
		if a := george(t, v.console()); a.Key || a.Why != "george is in DenyUsers" {
			t.Errorf("from a console: %+v, want not able", a)
		}
	})
	t.Run("restricted keys", func(t *testing.T) {
		v := debianVM(t)
		v.f.AddFile("/home/george/.ssh/authorized_keys", []byte(`command="/bin/true" `+key(1, "george@laptop-1")+"\n"), 0o600, 1000, 1000)
		if a := george(t, v); a.Key {
			t.Errorf("george with a command= key alone: %+v, want not able", a)
		}
	})
	t.Run("the session's line by address and port", func(t *testing.T) {
		v := debianVM(t)
		v.log += "2026-11-03T11:59:00+00:00 web-01 sshd-session[3901]: Accepted password for alice from 198.51.100.7 port " + sessionPort + " ssh2\n"
		if r := read(t, v, sudoGeorge); r.Login.User != "george" || !r.ByKey() {
			t.Errorf("login %+v, want george by publickey: another address's line on the same port taken", r.Login)
		}
	})
	t.Run("the session's line by its sshd pids", func(t *testing.T) {
		// george's session: its sshd-session 3910 under the [priv] 3900
		// that logs (01-inspect-b-*.log), under the listener 700.
		v := debianVM(t)
		proc := func(pid int, comm string, ppid int, start uint64) {
			v.f.AddFile(fmt.Sprintf("/proc/%d/stat", pid), fmt.Appendf(nil, "%d (%s) S %d %d %d 0 -1 4194560 0 0 0 0 0 0 0 0 20 0 1 0 %d 0 0\n", pid, comm, ppid, pid, pid, start), 0o444, 0, 0)
		}
		proc(700, "sshd", 1, 1200)
		proc(3900, "sshd-session", 700, 390000)
		proc(3910, "sshd-session", 3900, 391000)
		v.session.SSHD = revert.ProcStart{PID: 3910, Start: 391000}
		stale := "2026-11-03T11:59:30+00:00 web-01 sshd-session[3999]: Accepted password for alice from 192.0.2.10 port " + sessionPort + " ssh2\n"
		v.log += stale
		if r := read(t, v, sudoGeorge); r.Login.User != "george" || !r.ByKey() || r.LoginErr != nil {
			t.Errorf("login %+v, %v; want george's, the [priv] pid's, not the later stale line's", r.Login, r.LoginErr)
		}
		v.log = stale
		if r := read(t, v, sudoGeorge); r.Login.User != "" || !errors.Is(r.LoginErr, sshd.ErrNotLogged) {
			t.Errorf("own line missing: %+v, %v; want ErrNotLogged", r.Login, r.LoginErr)
		}
	})
}

// planOn plans c on v through the engine, and fails for any call that
// changed something outside hardn's run directory.
func planOn(t *testing.T, v *vm, c engine.Control) (*engine.Engine, *engine.Plan) {
	t.Helper()
	e := v.engine()
	n := len(v.f.Calls())
	p, err := e.Plan(context.Background(), []engine.Control{c}, &config.Config{})
	if err != nil {
		t.Fatal(err)
	}
	for _, call := range v.f.Calls()[n:] {
		switch call.Op {
		case "read", "read-nofollow", "stat", "lstat", "readdir", "run", "readlink", "resolve-symlink", "lookup-user", "lookup-group", "scan-lines":
		default:
			if !strings.HasPrefix(call.Path, "/run/hardn") {
				t.Errorf("planning wrote: %v", call)
			}
		}
	}
	return e, p
}

// refusalOf is the plan's one refusal, a *steps.Refusal.
func refusalOf(t *testing.T, p *engine.Plan) *steps.Refusal {
	t.Helper()
	var rf *steps.Refusal
	if !p.Empty() || len(p.Items) != 1 || !errors.As(p.Items[0].Err, &rf) {
		t.Fatalf("plan: %d changes, items %+v", len(p.Changes), p.Items)
	}
	return rf
}

var (
	disallow  = sshchange.Root{Want: "no", Getenv: sudoGeorge}
	keyOnly   = sshchange.Auth{Action: sshchange.AuthKeyOnly, Getenv: sudoGeorge}
	passOn    = sshchange.Auth{Action: sshchange.AuthPasswordsOn, Getenv: sudoGeorge}
	port2230  = sshchange.Port{To: 2230}
	allowRoot = sshchange.Root{Want: "prohibit-password", Getenv: sudoGeorge}
)

// passwordLogin is Debian 13 installed from the ISO: cloud-init's
// 50-cloud-init.conf allowing passwords, read before sshd_config's no.
func passwordLogin(t *testing.T) *vm {
	return debianVM(t).file(sshd.DropInDir+"/50-cloud-init.conf", "PasswordAuthentication yes\n")
}

// pveVM is a Proxmox VE 9 node: root alone, its key george's, its session
// root's by key.
func pveVM(t *testing.T) *vm {
	v := newVM(t, pve9(), false)
	v.f.AddFile(sshd.MainConfig, []byte(sshd.IncludeLine+"\n\nPermitRootLogin yes\nUsePAM yes\n"), 0o644, 0, 0)
	v.log = ""
	v.logIn("publickey", "root", "192.0.2.10")
	v.boot()
	return v
}

// legacyMain is Debian 13 with origin/main's hardn.conf (side-effects.md
// D7), beaten for PasswordAuthentication by 50-cloud-init.conf.
const legacyMain = "# SSH configuration managed by Hardn\n\nProtocol 2\nStrictModes yes\n\nPort 22\n\n" +
	"AuthenticationMethods publickey\nPubkeyAuthentication yes\n\nPermitRootLogin no\n\n" +
	"PasswordAuthentication no\nPermitEmptyPasswords no\n\nAuthorizedKeysFile .ssh/authorized_keys\n"

// d9 is Ubuntu 26.04 hardened by v0.3.2 with listen.conf never read: the
// socket on 22, listen.conf naming 2230 (P-39).
func d9(t *testing.T) *vm {
	v := newVM(t, ubuntu2604(), true, george, alice)
	v.socket = true
	v.f.AddFile("/usr/lib/systemd/system-generators/sshd-socket-generator", []byte("ELF"), 0o755, 0, 0)
	v.f.AddDir("/etc/systemd/system/ssh.socket.d", 0o755, 0, 0)
	v.f.AddFile(migrate.ManageConf, fixture(t, "v032-manage.conf"), 0o644, 0, 0)
	v.f.AddFile(migrate.ListenConf, fixture(t, "v032-listen.conf"), 0o644, 0, 0)
	v.configured = []string{"[::]:22", "0.0.0.0:22"}
	v.bound = slices.Clone(v.configured)
	return v
}

// TestRefusals: every refusal, before anything is written, in the decided
// words (gallery-ssh.md §6) as the screen says them
// (screenkit.RefusalWords), with its code, the registry's; each
// counterpart of the gallery's TestSSHRefusals and TestSSHGuards.
func TestRefusals(t *testing.T) {
	const noAdminWhy = "No admin could log in over SSH afterwards: "
	for _, tt := range []struct {
		name  string
		host  func(*testing.T) *vm
		c     engine.Control
		code  string
		is    error
		words string // the whole, or with prefix its start
		pre   bool
	}{
		{"root's own SSH login", func(t *testing.T) *vm {
			v := debianVM(t)
			v.log = ""
			v.logIn("publickey", "root", "192.0.2.10")
			return v
		}, disallow, "SSH_ROOT_SESSION", sshchange.ErrRootSession,
			"You're logged in as root over SSH. Turning root login off would close the way you came in. Log in as an admin and run hardn through sudo.", false},
		{"no admin: george locked without PAM", func(t *testing.T) *vm {
			return newVM(t, debian13(), true, george).file(sshd.MainConfig, strings.Replace(debianMain, "UsePAM yes", "UsePAM no", 1)).console()
		}, disallow, "SSH_NO_ADMIN_LOGIN", sshchange.ErrNoAdmin,
			noAdminWhy + "george's account is locked (!) and sshd has no PAM. Fix that first.", false},
		{"no admin: an RSA key under RequiredRSASize", func(t *testing.T) *vm { return rsaSmall(t, newVM(t, debian13(), true, george)) },
			disallow, "SSH_NO_ADMIN_LOGIN", sshchange.ErrNoAdmin,
			noAdminWhy + "george's RSA key is 1024 bits, under RequiredRSASize 2048. Fix that first.", false},
		{"no admin: sudo asks a password george hasn't got", func(t *testing.T) *vm { return newVM(t, debian13(), false, george).console() },
			disallow, "SSH_NO_ADMIN_LOGIN", sshchange.ErrNoAdmin,
			noAdminWhy + "george can't become root: sudo asks a password george hasn't got. Fix that first.", false},
		{"no admin: AllowUsers george@198.51.100.7", func(t *testing.T) *vm {
			return debianVM(t).file(sshd.DropInDir+"/10-site.conf", "AllowUsers george@198.51.100.7\n")
		}, disallow, "SSH_NO_ADMIN_LOGIN", sshchange.ErrNoAdmin,
			noAdminWhy + "AllowUsers lets george in from 198.51.100.7 alone; alice isn't in AllowUsers. Fix that first.", false},
		{"no admin besides root", func(t *testing.T) *vm { return newVM(t, debian13(), false).console() },
			disallow, "SSH_NO_ADMIN_LOGIN", sshchange.ErrNoAdmin, noAdminWhy + "there's no admin besides root. Fix that first.", false},
		{"no Accepted line for the session", func(t *testing.T) *vm {
			v := debianVM(t)
			v.log = ""
			return v
		}, disallow, "SSH_SESSION_NOT_LOGGED", sshchange.ErrNoSession,
			"hardn can't tell how you logged in: sshd's log has no line for this connection. Nothing that closes a way in can change.", false},
		{"a Match block lets root in", func(t *testing.T) *vm {
			return debianVM(t).file(sshd.DropInDir+"/20-site.conf", "Match Address 192.0.2.0/24\n    PermitRootLogin yes\n    PasswordAuthentication yes\n")
		}, disallow, "SSH_MATCH_LETS_MORE", sshchange.ErrMatchMore,
			"Match Address 192.0.2.0/24, in 20-site.conf, lets more in for some connections, and hardn's setting wouldn't change that. Change it there first.", false},
		{"sshd -T fails", func(t *testing.T) *vm {
			v := debianVM(t)
			_ = v.f.Remove(context.Background(), "/run/sshd")
			return v
		}, disallow, steps.CodeNoRunDir, steps.ErrNoRunDir,
			"/run/sshd isn't there yet, so sshd -T can't run. Open one SSH connection, then try again.", false},
		{"origin/main's hardn.conf", func(t *testing.T) *vm {
			return passwordLogin(t).file(sshd.DropInDir+"/hardn.conf", legacyMain)
		}, disallow, "SSH_MIGRATE_FIRST", steps.ErrMigrateFirst,
			"hardn's old SSH files are still here (hardn.conf). Move them into 00-hardn.conf first: item 4.", false},
		{"an Include not first (P-38)", func(t *testing.T) *vm { return debianVM(t).file(sshd.MainConfig, "Port 22\n"+debianMain) },
			keyOnly, "SSHD_NO_WRITE_STRATEGY", steps.ErrNoWriteStrategy,
			`sshd_config doesn't start with "Include /etc/ssh/sshd_config.d/*.conf", so hardn's settings wouldn't be read first. Put that line first, then try again.`, false},
		{"Key-only: alice has a password and no key", passwordLogin, keyOnly, "SSH_ADMIN_PASSWORD_ONLY", sshchange.ErrPasswordOnly,
			"alice would lose SSH login: alice has a password and no key. Add a key in User Management first.", false},
		{"Key-only: a password session", func(t *testing.T) *vm {
			v := passwordLogin(t)
			v.log = ""
			v.logIn("password", "alice", "192.0.2.10")
			return v
		}, keyOnly, "SSH_PASSWORD_SESSION", sshchange.ErrPasswordSession,
			"You logged in with a password. Turning passwords off would close the way you came in. Log in with a key first.", false},
		{"Key-only: no line in sshd's log", func(t *testing.T) *vm {
			v := passwordLogin(t)
			v.log = ""
			return v
		}, keyOnly, "SSH_SESSION_NOT_LOGGED", sshchange.ErrNoSession,
			"hardn can't tell how you logged in: sshd's log has no line for this connection. Nothing that closes a way in can change.", false},
		{"Key-only: a Match block letting passwords in", func(t *testing.T) *vm {
			return passwordLogin(t).file(sshd.DropInDir+"/20-site.conf", "Match Address 198.51.100.0/24\n    PasswordAuthentication yes\n")
		}, keyOnly, "SSH_MATCH_LETS_MORE", sshchange.ErrMatchMore, "Match Address 198.51.100.0/24, in 20-site.conf", true},
		{"Key-only: no admin able by key", func(t *testing.T) *vm {
			return rsaSmall(t, newVM(t, debian13(), true, george)).file(sshd.DropInDir+"/50-cloud-init.conf", "PasswordAuthentication yes\n")
		}, keyOnly, "SSH_NO_ADMIN_LOGIN", sshchange.ErrNoAdmin, noAdminWhy + "george's RSA key is 1024 bits", true},
		{"Proxmox VE: root key-only already", func(t *testing.T) *vm {
			return pveVM(t).file(sshd.DropInDir+"/00-hardn.conf", "PermitRootLogin prohibit-password\n")
		}, allowRoot, "SSH_PVE_ROOT_KEY_ONLY", sshchange.ErrPVEKeyOnly,
			"Root stays key-only on Proxmox VE: the cluster's nodes log in to each other as root by key.", false},
		{"Proxmox VE: root's own login by password", func(t *testing.T) *vm {
			v := pveVM(t).shadowOf("root", hashSet)
			v.log = ""
			v.logIn("password", "root", "192.0.2.10")
			return v
		}, allowRoot, "SSH_PVE_ROOT_PASSWORD_SESSION", sshchange.ErrPVEPasswordSession,
			"You logged in as root with a password. Making root key-only would close the way you came in. Log in as root with a key first.", false},
		{"Proxmox VE: root without a key", func(t *testing.T) *vm {
			v := pveVM(t).console()
			_ = v.f.Remove(context.Background(), "/root/.ssh/authorized_keys")
			return v
		}, allowRoot, "SSH_PVE_NO_ROOT_KEY", sshchange.ErrPVENoRootKey,
			"Root would lose SSH login: root has no key sshd reads. Add a key for root first.", false},
		{"Proxmox VE: Key-only, root without a key, from a console", func(t *testing.T) *vm {
			v := pveVM(t).file(sshd.DropInDir+"/50-cloud-init.conf", "PasswordAuthentication yes\n").console()
			_ = v.f.Remove(context.Background(), "/root/.ssh/authorized_keys")
			return v
		}, keyOnly, "SSH_PVE_NO_ROOT_KEY", sshchange.ErrPVENoRootKey,
			"Root would lose SSH login: root has no key sshd reads. Add a key for root first.", false},
		{"the port on Proxmox VE", pveVM, port2230, "PVE_PORT_CHANGE_REFUSED", nil,
			"The SSH port stays 22 on Proxmox VE: the cluster and pve-firewall use it.", false},
		{"ufw lets 22 in from 192.0.2.0/24 alone", func(t *testing.T) *vm {
			v := debianVM(t)
			v.rules = []ufwRule{{port: 22, src: "192.0.2.0/24"}}
			v.saveRules()
			return v
		}, port2230, "SSH_PORT_SOURCE_RESTRICTED", sshchange.ErrPortRestricted,
			"ufw lets port 22 in from 192.0.2.0/24 alone. A new port would be open to anyone. Move that rule to the new port by hand first.", false},
		{"ufw lets 2230, where sshd listens now, in from 192.0.2.0/24 alone", func(t *testing.T) *vm {
			v := debianVM(t).file(sshd.HardnDropIn, "Port 2230\n")
			v.rules = []ufwRule{{port: 22}, {port: 2230, src: "192.0.2.0/24"}}
			v.saveRules()
			return v
		}, sshchange.Port{To: 2231}, "SSH_PORT_SOURCE_RESTRICTED", sshchange.ErrPortRestricted,
			"ufw lets port 2230 in from 192.0.2.0/24 alone. A new port would be open to anyone. Move that rule to the new port by hand first.", false},
		{"ufw lets 22 in on eth0 alone", func(t *testing.T) *vm {
			v := debianVM(t)
			v.rules = []ufwRule{{port: 22, iface: "eth0"}}
			v.saveRules()
			return v
		}, port2230, "SSH_PORT_SOURCE_RESTRICTED", sshchange.ErrPortRestricted,
			"ufw lets port 22 in from anywhere on eth0 alone. A new port would be open to anyone. Move that rule to the new port by hand first.", false},
		{"the port with v0.3.2's files", d9, port2230, "SSH_MIGRATE_FIRST", steps.ErrMigrateFirst,
			"hardn's old SSH files are still here (manage.conf, listen.conf). Move them into 00-hardn.conf first: item 4.", false},
		{"listen.conf never in effect (P-39)", d9, sshchange.Migrate{}, steps.CodeListenConfNotInEffect, steps.ErrListenConfNotInEffect,
			"hardn v0.3.x's listen.conf (port 2230 for ssh.socket) never took effect: ssh.socket listens on 22. Moving it now couldn't be undone. Let 2230 through the firewall, run systemctl daemon-reload and systemctl restart ssh.socket, check a new connection to 2230 works, then try again.", false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			v := tt.host(t)
			_, p := planOn(t, v, tt.c)
			rf := refusalOf(t, p)
			words := screenkit.RefusalWords(rf)
			if tt.pre && !strings.HasPrefix(words, tt.words) || !tt.pre && words != tt.words {
				t.Errorf("words %q\nwant %q", words, tt.words)
			}
			if rf.Code != tt.code || output.CodeOf(p.Items[0].Err) != tt.code {
				t.Errorf("code %s, registry's %s, want %s", rf.Code, output.CodeOf(p.Items[0].Err), tt.code)
			}
			if tt.is != nil && !errors.Is(rf, tt.is) {
				t.Errorf("%v: not %v", rf, tt.is)
			}
			for _, c := range v.f.Calls() {
				if c.Op == "run" && (c.Argv[0] == "systemctl" && c.Argv[1] == "reload" || c.Argv[0] == "ufw" && c.Argv[1] != "status") {
					t.Errorf("ran %v before refusing", c.Argv)
				}
			}
		})
	}
}

// TestPlans: what each change plans on the gallery's hosts, every change
// of lockout risk (PlanSSH's), and nothing where it is in effect already.
func TestPlans(t *testing.T) {
	for _, tt := range []struct {
		name    string
		host    func(*testing.T) *vm
		c       engine.Control
		changes bool
	}{
		{"Disallow on debian13", debianVM, disallow, true},
		{"Disallow from a console, george able", func(t *testing.T) *vm { return debianVM(t).console() }, disallow, true},
		{"Disallow, AllowUsers george", func(t *testing.T) *vm {
			return debianVM(t).file(sshd.DropInDir+"/10-allow.conf", "AllowUsers george\n")
		}, disallow, true},
		{"Disallow where root is disallowed already", func(t *testing.T) *vm {
			return debianVM(t).file(sshd.HardnDropIn, "PermitRootLogin no\n")
		}, disallow, false},
		{"Allow root key-only where disallowed: no admin weighed", func(t *testing.T) *vm {
			return newVM(t, debian13(), false).file(sshd.HardnDropIn, "PermitRootLogin no\n")
		}, allowRoot, true},
		{"Key-only on Proxmox VE, root by key", func(t *testing.T) *vm {
			return pveVM(t).file(sshd.DropInDir+"/50-cloud-init.conf", "PasswordAuthentication yes\n")
		}, keyOnly, true},
		{"Key-only on Proxmox VE from a console, root's key", func(t *testing.T) *vm {
			return pveVM(t).file(sshd.DropInDir+"/50-cloud-init.conf", "PasswordAuthentication yes\n").console()
		}, keyOnly, true},
		{"Root key-only on Proxmox VE, root's key", pveVM, allowRoot, true},
		{"Root key-only on Proxmox VE asked as Disallow", pveVM, disallow, true},
		{"Key-only where passwords are off", debianVM, keyOnly, false},
		{"Key-only where AuthenticationMethods keeps passwords off", func(t *testing.T) *vm {
			return passwordLogin(t).file(sshd.DropInDir+"/10-site.conf", "AuthenticationMethods publickey\n")
		}, keyOnly, false},
		{"Key-only, alice with a key too", func(t *testing.T) *vm {
			a := alice
			a.keys = []string{key(3, "alice@laptop-3")}
			return newVM(t, debian13(), true, george, a).file(sshd.DropInDir+"/50-cloud-init.conf", "PasswordAuthentication yes\n")
		}, keyOnly, true},
		{"Password login on", debianVM, passOn, true},
		{"Password login on where on", passwordLogin, passOn, false},
		{"the port", debianVM, port2230, true},
		{"the port, ufw not installed", func(t *testing.T) *vm {
			v := debianVM(t)
			v.noUFW = true
			return v
		}, port2230, true},
		{"origin/main's hardn.conf moved", func(t *testing.T) *vm {
			return passwordLogin(t).file(sshd.DropInDir+"/hardn.conf", legacyMain)
		}, sshchange.Migrate{}, true},
		{"nothing to move", debianVM, sshchange.Migrate{}, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			_, p := planOn(t, tt.host(t), tt.c)
			if err := p.Err(); err != nil {
				t.Fatalf("plan: %v", err)
			}
			if tt.changes == p.Empty() {
				t.Fatalf("%d changes, want changes %v", len(p.Changes), tt.changes)
			}
			for _, c := range p.Changes {
				if c.Risk != engine.RiskLockout && !strings.HasPrefix(c.ID, "engine.") && !strings.Contains(c.ID, "boot") {
					t.Errorf("change %s of risk %v, want lockout", c.ID, c.Risk)
				}
				if !strings.HasPrefix(c.ID, tt.c.ID()+"/") && !strings.HasPrefix(c.ID, "engine.") && !strings.Contains(c.ID, "boot") {
					t.Errorf("change %s, not under %s", c.ID, tt.c.ID())
				}
			}
		})
	}
}

// snapshot is every file under the paths the SSH changes write (sshd's
// files, ufw's rules), with mode and owner.
func snapshot(v *vm) map[string]string {
	out := map[string]string{}
	var walk func(dir string)
	walk = func(dir string) {
		entries, _ := v.f.ReadDir(context.Background(), dir)
		for _, de := range entries {
			p := dir + "/" + de.Name()
			e, _ := v.f.Entry(p)
			if de.IsDir() {
				walk(p)
				continue
			}
			out[p] = e.Mode.String() + " " + string(e.Data)
		}
	}
	walk("/etc/ssh")
	walk("/etc/ufw")
	return out
}

// TestApplyRollBack: a Disallow, a Key-only and a port change applied on
// the engine and rolled back by the timer's rollback, sshd's files and
// ufw's rules byte for byte as before, and sshd on what it listened on.
func TestApplyRollBack(t *testing.T) {
	ctx := context.Background()
	for _, tt := range []struct {
		name string
		host func(*testing.T) *vm
		c    engine.Control
		want string // what sshd -T shows applied
	}{
		{"Disallow", debianVM, disallow, "permitrootlogin no"},
		{"Key-only", func(t *testing.T) *vm {
			a := alice
			a.keys = []string{key(3, "alice@laptop-3")}
			return newVM(t, debian13(), true, george, a).file(sshd.DropInDir+"/50-cloud-init.conf", "PasswordAuthentication yes\n")
		}, keyOnly, "passwordauthentication no"},
		{"the port", debianVM, port2230, "port 2230"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			v := tt.host(t)
			before, bound := snapshot(v), slices.Clone(v.bound)
			e, p := planOn(t, v, tt.c)
			if err := p.Err(); err != nil || p.Empty() || p.Risk != engine.RiskLockout {
				t.Fatalf("plan: %v, %d changes, risk %v", err, len(p.Changes), p.Risk)
			}
			res, err := e.Apply(ctx, p)
			if err != nil {
				t.Fatal(err)
			}
			if out := v.sshd([]string{"sshd", "-T", "-f", sshd.MainConfig}).Stdout; !strings.Contains(out, tt.want+"\n") {
				t.Errorf("applied: sshd -T shows\n%s\nwant %q", out, tt.want)
			}
			opts := revert.Options{Now: func() time.Time { return now.Add(time.Minute) }, Hardn: hardnBinary, Reason: revert.ReasonTimeout, Plans: plans.Registry()}
			if _, err := revert.Rollback(ctx, v.sys(), res.ID, opts); err != nil {
				t.Fatal(err)
			}
			after := snapshot(v)
			for p, b := range before {
				if after[p] != b {
					t.Errorf("%s rolled back:\n%q\nwas\n%q", p, after[p], b)
				}
			}
			for p := range after {
				if _, ok := before[p]; !ok {
					t.Errorf("%s left behind", p)
				}
			}
			if !slices.Equal(v.bound, bound) {
				t.Errorf("sshd on %v, was %v", v.bound, bound)
			}
		})
	}
}

// TestNotMigrated: each control's zero value, and the Auth action none,
// plan nothing and read nothing: engine.ErrNotMigrated.
func TestNotMigrated(t *testing.T) {
	for _, c := range []engine.Control{sshchange.Root{}, sshchange.Auth{}, sshchange.Port{}, sshchange.Auth{Action: sshchange.AuthNone, Getenv: sudoGeorge}} {
		v := debianVM(t)
		n := len(v.f.Calls())
		cs, err := c.Plan(context.Background(), v.sys(), engine.Input{Facts: v.facts, Session: v.session})
		if !errors.Is(err, engine.ErrNotMigrated) || cs != nil || len(v.f.Calls()) != n {
			t.Errorf("%s: %v, %v, %d calls", c.ID(), cs, err, len(v.f.Calls())-n)
		}
	}
	if (sshchange.Root{}).ID() != "ssh.login" || (sshchange.Auth{}).ID() != "ssh.auth" || (sshchange.Port{}).ID() != "ssh.port" || (sshchange.Migrate{}).ID() != sshchange.MigrateID {
		t.Error("ids")
	}
}
