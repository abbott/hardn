// Package sshchange holds the SSH Login screen's changes (refactor plan
// slice 6, unit (b), next/s6-control; docs/engine/gallery-ssh.md): root
// login (Root: Disallow, Allow root key-only, on Proxmox VE Root key-only),
// password login (Auth: Key-only login, Password login on), the port (Port)
// and moving hardn's old files (Migrate). Each embeds the check control of
// pkg/engine/controls/ssh it changes (ssh.Login, ssh.Auth, ssh.Port), whose
// ID, Title, Applies and Check it keeps, and gives it a Plan: steps.PlanSSH
// or steps.PlanMigration, every change of lockout risk, so typed and
// confirmed or reverted. The status list (controls.Status) keeps the
// check-only controls; each control here plans nothing at its zero value
// (engine.ErrNotMigrated), so a plan over a list of them refuses too. It
// lives apart from ssh because the proof needs users.Read, and
// pkg/engine/controls/users imports ssh.
//
// Read gives the screen what it draws, as facts, no words: sshd -T, the
// files, the admins (users.Read), the session's login from sshd's log,
// root's account, v0.3.x's listen.conf, ufw's sources for port 22 and why
// they could not be carried to a new port.
// Plan reads the host again the same way and refuses, before anything is
// written, in the order of the mock (gallery-ssh.md §5 decision 2, the
// proof): sshd not read, hardn's old files and an Include not first; then
// each change's own (root.go, auth.go, port.go). Every refusal is a
// *steps.Refusal with a code (pkg/output) in the decided words of
// gallery-ssh.md §6. Which admin could still log in from the session's
// address is LoginPaths (paths.go): sshd's own answer for each admin
// (sshd.ReadUser), then what hardn matches itself.
package sshchange

import (
	"context"
	"path"
	"strings"

	"github.com/abbott/hardn/pkg/config"
	"github.com/abbott/hardn/pkg/engine/controls/ssh"
	"github.com/abbott/hardn/pkg/engine/controls/users"
	"github.com/abbott/hardn/pkg/engine/steps"
	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/accounts"
	"github.com/abbott/hardn/pkg/host/alpine"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/pve"
	"github.com/abbott/hardn/pkg/host/reload"
	"github.com/abbott/hardn/pkg/host/revert"
	"github.com/abbott/hardn/pkg/host/sshd"
	"github.com/abbott/hardn/pkg/host/sshd/migrate"
	"github.com/abbott/hardn/pkg/host/ufw"
)

// Reading is the host as the SSH Login screen draws it, as facts, no
// words.
type Reading struct {
	Facts detect.Facts
	// Binary is the sshd OpenRC runs (reload.OpenRCFlavour), "" elsewhere
	// (sshd.DefaultBinary).
	Binary string
	// Effective and EffErr are sshd -T over the installed files
	// (sshd.ReadEffective).
	Effective sshd.Effective
	EffErr    error
	// ServerWorld is, on Alpine where sshd is off without its host keys,
	// /etc/apk/world's entries that keep sshd installed
	// (steps.ServerWorld), which the host-key refusal names; nil elsewhere.
	ServerWorld []string
	// Found are sshd's files and hardn's old ones (migrate.Find, with
	// hardn.yml's sshConfigFile); FoundErr why they are not read.
	Found    migrate.Found
	FoundErr error
	// Users and UsersErr are the User Management screen's reading
	// (users.Read): the admins, their passwords and keys.
	Users    users.Reading
	UsersErr error
	// Session is the applying session (revert.ReadSession's in hardn,
	// engine.Input.Session in Plan). Login is its line in sshd's log
	// (sshd.ReadAccepted, logged by its sshd processes where /proc shows
	// them: Session.LogPIDs); LoginErr why it is not found. A console session
	// (Session.Remote false) reads no log.
	Session  revert.Session
	Login    sshd.Accepted
	LoginErr error
	// PVE: Proxmox VE's rules apply (pve.Applies).
	PVE bool
	// Root is root's account and RootPassword its password's state, the
	// first line of each name; RootLocked: sshd has no PAM and every uid-0
	// account's password field starts with "!" (check-ssh.md §9), so no
	// account logs in as root.
	Root         accounts.User
	RootPassword accounts.Password
	RootLocked   bool
	// ListenPort is v0.3.x's listen.conf's port for ssh.socket, "" where
	// there is none.
	ListenPort string
	// OnlyFromPort is the first port sshd listens on now (22 where its
	// listeners are not read) that an active ufw lets in from some
	// sources alone, and OnlyFrom those sources, an interface rule's
	// "<src> on <iface>" (ufw.OnlyFrom): a new port is given the same
	// rules (design-ssh-port-sources.md, which superseded P-33 (2) (a)).
	// Zero and nil where ufw is not installed, not read, or lets each such
	// port in from anywhere on any interface.
	OnlyFromPort uint16
	OnlyFrom     []string
	// CarryErr is why those rules could not be carried to a new port,
	// whatever the port (ufw.CheckSession): a *ufw.CopyError for a rule
	// with a source port or a source application, a *ufw.SessionError
	// where the rules as they would be do not let the session's address
	// in, or a remote session's address is not read. nil where nothing is
	// carried, and where ufw's state is not read whole (the planner
	// refuses that).
	CarryErr error
	// Fix is, on Alpine, whether the openssh-server package sshd runs (the
	// binary OpenRC's script runs) has CVE-2026-123158's fix
	// (alpine.OpenSSHFix), Unknown where which sshd runs is not read; the
	// zero Fix elsewhere.
	Fix alpine.Fix
}

// Read reads the host as the screen draws it, writing nothing: sshd -T
// (OpenRC's binary on OpenRC), the files (migrate.Find with cfg's
// sshConfigFile; cfg nil is none), users.Read with getenv (os.Getenv in
// hardn), the session's line in sshd's log where sess is remote, root's
// account, on Alpine apk's installed database (Fix) and, where sshd is off
// without its host keys, its world file (ServerWorld), listen.conf and ufw.
// A read that fails is kept beside what it would have given; Read itself
// returns no error but ctx's.
func Read(ctx context.Context, sys host.System, f detect.Facts, cfg *config.Config, sess revert.Session, getenv func(string) string) (Reading, error) {
	r := Reading{Facts: f, Session: sess}
	if err := ctx.Err(); err != nil {
		return r, err
	}
	if f.Init.Kind == detect.OpenRC {
		r.Binary, _ = reload.OpenRCFlavour(ctx, sys) // its error is sshd -T's below, run with the default binary
	}
	r.Effective, _, r.EffErr = sshd.ReadEffective(ctx, sys, sshd.EffectiveRequest{SSH: f.SSH, Binary: r.Binary})
	if f.SSH.SSHDUnits && r.EffErr == nil {
		r.EffErr = reload.CheckSysconfig(ctx, sys) // sshd's command line, which sshd -T does not see
	}
	r.ServerWorld = steps.ServerWorld(ctx, sys, f, r.EffErr)
	var file string
	if cfg != nil {
		file = cfg.SshConfigFile
	}
	r.Found, r.FoundErr = migrate.Find(ctx, sys, migrate.FindRequest{ConfigFile: file})
	r.Users, r.UsersErr = users.Read(ctx, sys, f, getenv)
	if sess.Remote {
		r.Login, r.LoginErr = sshd.ReadAccepted(ctx, sys, f.Init.Kind == detect.OpenRC, sess.Conn, sess.LogPIDs(ctx, sys))
	}
	r.PVE, _ = pve.Applies(f.Proxmox)
	r.readRoot(ctx, sys)
	if f.Platform.Distro == detect.DistroAlpine {
		r.Fix = fixUnread(f)
		if f.Init.Kind != detect.OpenRC || r.Binary != "" {
			r.Fix = alpine.OpenSSHFix(ctx, sys, ssh.ServerPackage(r.Binary), f.OS.VersionID)
		}
	}
	if data, err := sys.ReadFile(ctx, migrate.ListenConf); err == nil {
		for _, l := range strings.Split(string(data), "\n") {
			if v, ok := strings.CutPrefix(strings.TrimSpace(l), "ListenStream="); ok && v != "" {
				r.ListenPort = v
			}
		}
	}
	if !r.PVE {
		if st, err := ufw.Read(ctx, sys, f.Proxmox); err == nil && st.Installed {
			ports := r.Effective.Listeners.Ports()
			if len(ports) == 0 {
				ports = []uint16{22}
			}
			for _, p := range ports {
				if srcs := ufw.OnlyFrom(st, p); len(srcs) > 0 {
					r.OnlyFromPort, r.OnlyFrom = p, srcs
					break
				}
			}
			if st.Known() {
				r.CarryErr = ufw.CheckSession(st, ports, steps.UFWSession(ctx, sys, sess, st, ports))
			}
		}
	}
	return r, ctx.Err()
}

// readRoot reads root's account, its password state, and whether every
// uid-0 account is locked where sshd has no PAM: the first line of each
// name in /etc/passwd, as check-ssh.md §9 reads them.
func (r *Reading) readRoot(ctx context.Context, sys host.System) {
	db, err := accounts.Read(ctx, sys)
	if err != nil {
		return
	}
	sh, shErr := accounts.ReadShadow(ctx, sys)
	seen, locked, any0 := map[string]bool{}, true, false
	for _, u := range db.Users {
		if seen[u.Name] {
			continue
		}
		seen[u.Name] = true
		if u.Name == "root" {
			r.Root = u
			if shErr == nil {
				r.RootPassword = sh.PasswordOf(u)
			}
		}
		if u.UID == 0 {
			any0 = true
			if l, _ := sh.LockOf(u); shErr != nil || l != accounts.Locked {
				locked = false
			}
		}
	}
	r.RootLocked = r.EffErr == nil && r.Effective.UsePAM.Value != "yes" && any0 && locked
}

// Console reports whether hardn runs from a console, not over SSH:
// closing an SSH login path cannot close the way in.
func (r Reading) Console() bool { return !r.Session.Remote }

// RootOwn reports whether the session is root's own SSH login.
func (r Reading) RootOwn() bool { return r.Login.User == "root" }

// ByKey reports whether the session logged in by key.
func (r Reading) ByKey() bool { return r.Login.Method == "publickey" }

// RootValue is PermitRootLogin as sshd -T prints it, without-password read
// as prohibit-password.
func (r Reading) RootValue() string {
	if v := r.Effective.PermitRootLogin.Value; v != "without-password" {
		return v
	}
	return pve.RootLogin
}

// Methods are AuthenticationMethods' lists as sshd -T prints them.
func (r Reading) Methods() []string {
	return strings.Fields(strings.Join(r.Effective.Keywords["authenticationmethods"], " "))
}

// PasswordsOn reports whether a password logs in: one of
// AuthenticationMethods' lists met by password, where
// PasswordAuthentication is yes, or keyboard-interactive, where it is yes
// and sshd uses PAM (sshd.MethodsMet).
func (r Reading) PasswordsOn() bool {
	return sshd.MethodsMet(r.Methods(), map[string]bool{
		"password":             r.Effective.PasswordAuthentication.Value == "yes",
		"keyboard-interactive": r.Effective.KbdInteractiveAuthentication.Value == "yes" && r.Effective.UsePAM.Value == "yes",
	})
}

// MatchMore are the Match lines that let in more than global for keyword
// (permitrootlogin, passwordauthentication), as the SSH checks read them:
// every block counts.
func (r Reading) MatchMore(keyword, global string) []sshd.Line {
	rank := map[string]int{"no": 0, "forced-commands-only": 1, "prohibit-password": 2, "without-password": 2, "yes": 3}
	var out []sshd.Line
	for _, l := range sshd.ReadOrder(r.Found.Main, r.Found.DropIns) {
		if l.Match != nil && l.Keyword == keyword && len(l.Args) > 0 && rank[strings.ToLower(l.Args[0])] > rank[global] {
			out = append(out, l)
		}
	}
	return out
}

// DecidedBy is the base name of the file whose line sshd reads first for
// keyword, outside Match blocks: "" where none sets it (sshd's default).
func (r Reading) DecidedBy(keyword string) string {
	for _, l := range sshd.ReadOrder(r.Found.Main, r.Found.DropIns) {
		if l.Match == nil && l.Keyword == keyword {
			return path.Base(l.File)
		}
	}
	return ""
}

// PenaltiesBy is the base name of the file whose line decides whether
// PerSourcePenalties is on, as servconf.c reads it: off, the first line
// that says "no"; on, the first line that sets a penalty, which turns them
// on whatever a "yes" or "no" says, else the first that says "yes". ""
// where no line read does: on, that is sshd's default.
func (r Reading) PenaltiesBy() string {
	var yes, no, sets string
	for _, l := range sshd.ReadOrder(r.Found.Main, r.Found.DropIns) {
		if l.Match != nil || l.Keyword != "persourcepenalties" {
			continue
		}
		for _, a := range l.Args {
			switch f := path.Base(l.File); {
			case strings.EqualFold(a, "no"):
				no = first(no, f)
			case strings.EqualFold(a, "yes"):
				yes = first(yes, f)
			default:
				sets = first(sets, f)
			}
		}
	}
	switch {
	case r.Effective.Penalties == sshd.PenaltiesOff:
		return no
	case sets != "":
		return sets
	}
	return yes
}

// first is a, or b where a is empty.
func first(a, b string) string {
	if a != "" {
		return a
	}
	return b
}
