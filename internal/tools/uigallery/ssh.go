package main

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"math/bits"
	"net/netip"
	"path"
	"slices"
	"strconv"
	"strings"

	"github.com/abbott/hardn/pkg/config"
	"github.com/abbott/hardn/pkg/engine"
	"github.com/abbott/hardn/pkg/engine/controls"
	"github.com/abbott/hardn/pkg/engine/controls/ssh"
	"github.com/abbott/hardn/pkg/engine/controls/users"
	probe "github.com/abbott/hardn/pkg/engine/header"
	"github.com/abbott/hardn/pkg/engine/risk"
	"github.com/abbott/hardn/pkg/engine/steps"
	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/accounts"
	"github.com/abbott/hardn/pkg/host/authkeys"
	"github.com/abbott/hardn/pkg/host/backup"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/pve"
	"github.com/abbott/hardn/pkg/host/reload"
	"github.com/abbott/hardn/pkg/host/revert"
	"github.com/abbott/hardn/pkg/host/sshd"
	"github.com/abbott/hardn/pkg/host/sshd/migrate"
	"github.com/abbott/hardn/pkg/output"
	"github.com/abbott/hardn/pkg/screens/screenkit"
	"github.com/abbott/hardn/pkg/ui/component"
	"github.com/abbott/hardn/pkg/ui/flow"
	"github.com/abbott/hardn/pkg/ui/form"
	"github.com/abbott/hardn/pkg/ui/nav"
	"github.com/abbott/hardn/pkg/ui/screen"
)

// The SSH Login mock (refactor plan slice 6, unit (a), next/s6-gallery;
// docs/engine/gallery-ssh.md): a proposal, on Figma's layouts (02-SSH_LOGIN,
// SSH_LOGIN-TOGGLE_CONFIRMATION), drawn from what real code reads of a
// simulated host (sshhost.go): the SSH Login, SSH Auth and SSH Port rows from
// the real checks as the main menu draws them (Engine.Check, risk.Assess,
// output.StatusViewOf), the SSH Configuration section from sshd -T
// (sshd.ReadEffective) and the files (migrate.Find), the admins from the
// users control's Read, the session from revert.ReadSession. Each change is
// planned by the real steps.PlanSSH or PlanMigration and runs on the real
// engine: lockout-risk, so typed and confirmed or reverted. The lockout
// checks before asking are the gallery's stand-ins (sshLoginPaths,
// sshSessionLogin), labelled, unit (b)'s to build. "uigallery ssh" lists
// the mocks; "play ssh" is the menu of the hosts, "play ssh/<host>" one; the
// main menu's 2 leads to debian13's.

// sshPrefix names the mock's hosts: "ssh" and "ssh/<host>".
const sshPrefix = "ssh"

// sshMainHost is the host the main menu's 2 leads to.
const sshMainHost = sshPrefix + "/debian13"

// sshLinks lead from a menu mock's item to the mock on a host: the main
// menu's 2.
var sshLinks = map[string]string{mainMenuMock + "/2": sshMainHost}

// The screen's words (gallery-ssh.md §6, proposed). Figma's where it has
// them: the section, its rows, View SSH Settings' absence aside.
const (
	sshTitle     = "SSH Login"
	sshSection   = "SSH Configuration"
	sshHeading   = "Select an option"
	sshPrompt    = "Enter your choice"
	sshRowRoot   = "Root SSH Login"
	sshRowPass   = "Password Login"
	sshRowPort   = "SSH Port"
	sshRowUsers  = "Allowed Users"
	sshRowMatch  = "Match Block"
	sshRowYou    = "Logged in as"
	sshRowLegacy = "Old Settings"

	sshDisallow     = "Disallow root login"
	sshDisallowDesc = "Root can't log in over SSH"
	sshKeyOnlyRoot  = "Allow root key-only"
	sshKeyRootDesc  = "Root logs in by key alone"
	sshPVERoot      = "Root key-only"
	sshPVERootDesc  = "Kept for the Proxmox VE cluster"
	sshKeyOnly      = "Key-only login"
	sshKeyOnlyDesc  = "Turn password login off"
	sshPassOn       = "Password login on"
	sshPassOnDesc   = "Let passwords log in again"
	sshPort         = "Change SSH port"
	sshMigrate      = "Move old settings"
	sshMigrateDesc  = "Into 00-hardn.conf"
	sshPortField    = "New SSH port"
)

// sshHost is one simulated host the mock runs on.
type sshHost struct {
	name, label, about, note string
	host                     func() *fakeHost
	// env is hardn's environment: SSH_CONNECTION, the session's
	// connection, and SUDO_USER or DOAS_USER where hardn runs through one.
	env map[string]string
}

// sshHosts are the hosts, in the menu's order.
func sshHosts() []sshHost {
	const (
		deb = hostFQDN + " runs Debian 13 (OpenSSH 10.0p2). "
		ubu = hostFQDN + " runs Ubuntu 26.04 (OpenSSH 10.2p1, ssh.socket). "
		alp = hostFQDN + " runs Alpine 3.23 (OpenSSH 10.2p1, OpenRC, sshd without PAM). "
	)
	sudo := map[string]string{"SSH_CONNECTION": applyConn, "SUDO_USER": "george"}
	doas := map[string]string{"SSH_CONNECTION": applyConn, "DOAS_USER": "george"}
	root := map[string]string{"SSH_CONNECTION": applyConn}
	return []sshHost{
		{name: "debian13", label: "Debian 13", about: "The VM's shape", host: sshDebian13, env: sudo,
			note: deb + "As the VM: the stock Include first, PasswordAuthentication no, UsePAM yes; root \"*\" with george's first key; george \"!\", two keys, sudo by cloud-init; alice a password, no key, in sudo; ufw allows 22 from anywhere; the old hardn.yml. george logged in by key, hardn through sudo."},
		{name: "ubuntu2604", label: "Ubuntu 26.04", about: "ssh.socket", host: sshUbuntu, env: sudo,
			note: ubu + "As the VM: 60-cloudimg-settings.conf refuses passwords, prohibit-password; otherwise as debian13."},
		{name: "alpine323", label: "Alpine 3.23", about: "No PAM, root locked", host: sshAlpine, env: doas,
			note: alp + "As the VM: 50-cloud-init.conf refuses passwords; root \"!\" with a key, so root can't log in at all; george \"*\" in wheel, doas nopass; no ufw."},
		{name: "hardened", label: "Hardened", about: "Figma's state", host: sshHardened, env: map[string]string{"SSH_CONNECTION": confirmConn, "SUDO_USER": "george"},
			note: deb + "00-hardn.conf sets Port 2230, PermitRootLogin no and PasswordAuthentication no; ufw allows 2230. george's session is on 2230."},
		{name: "password-login", label: "Password login", about: "cloud-init allows it", host: sshPasswordLogin, env: sudo,
			note: deb + "cloud-init's 50-cloud-init.conf sets PasswordAuthentication yes, read before sshd_config's no; alice, an admin, has a password and no key."},
		{name: "password-session", label: "Password session", about: "alice by password", host: sshPasswordSession, env: map[string]string{"SSH_CONNECTION": applyConn, "SUDO_USER": "alice"},
			note: deb + "password-login, but alice logged in by password and runs hardn through sudo."},
		{name: "root-session", label: "Root's session", about: "No SUDO_USER", host: sshRootSession, env: root,
			note: deb + "debian13, but hardn runs from root's own SSH login, by key: no SUDO_USER."},
		{name: "legacy-v032", label: "hardn v0.3.2", about: "manage.conf, listen.conf", host: sshLegacyV032, env: sudo,
			note: ubu + "Hardened by v0.3.2: its manage.conf (AuthorizedKeysFile .ssh/authorized_keys .ssh_%u/authorized_keys, george's keys in ~/.ssh_george), and listen.conf for 2230 written with no daemon-reload, so ssh.socket still listens on 22."},
		{name: "legacy-main", label: "origin/main's file", about: "hardn.conf, passwords off", host: sshLegacyMain, env: sudo,
			note: deb + "origin/main's hardn.conf (PermitRootLogin no, AuthenticationMethods publickey, AuthorizedKeysFile .ssh/authorized_keys). 50-cloud-init.conf, read first, sets PasswordAuthentication yes, but AuthenticationMethods publickey keeps passwords out."},
		{name: "include-other", label: "Include not first", about: "Port 22 above it", host: sshIncludeOther, env: sudo,
			note: deb + "sshd_config has Port 22 above the stock Include (P-38)."},
		{name: "match-allows", label: "Match block", about: "Root, passwords from 192.0.2.0/24", host: sshMatchAllows, env: sudo,
			note: deb + "20-site.conf's Match Address 192.0.2.0/24 sets PermitRootLogin yes and PasswordAuthentication yes."},
		{name: "allow-users", label: "AllowUsers", about: "george alone", host: sshAllowUsers, env: sudo,
			note: deb + "10-allow.conf sets AllowUsers george; alice, an admin with a key here, is left out."},
		{name: "pve9", label: "Proxmox VE 9", about: "Root allowed", host: sshPVE, env: root,
			note: hostFQDN + " is a Proxmox VE 9 node as installed: PermitRootLogin yes, passwords on; root alone, by key; no ufw. hardn runs from root's login."},
		{name: "ufw-restricted", label: "ufw, 22 restricted", about: "From 192.0.2.0/24", host: sshUFWRestricted, env: sudo,
			note: deb + "debian13, but ufw allows 22 from 192.0.2.0/24 alone (P-33 (2))."},
		{name: "confirm-window", label: "Confirm pending", about: "22 and 2230 held", host: sshConfirmWindow, env: sudo,
			note: deb + "debian13 after Change SSH port to 2230, applied a minute ago: 22 and 2230 held, hardn confirm pending."},
		{name: "not-read", label: "sshd not read", about: "Before the first connection", host: sshNotRead, env: sudo,
			note: ubu + "ubuntu2604 before its first connection: no /run/sshd, so sshd -T fails."},
		{name: "nopam-locked", label: "Alpine, admin locked", about: "george \"!\", no PAM", host: sshNoPAMLocked, env: sshConsoleEnv,
			note: alp + "alpine323, but george's account is \"!\" and george is the only admin: sshd without PAM refuses george's key [I: not run], so he has no SSH login. hardn runs as root on the console."},
		{name: "rsa-small", label: "RSA key too short", about: "1024 bits, 2048 asked", host: sshRSASmall, env: sshConsoleEnv,
			note: deb + "debian13, but george's only key is RSA 1024 bits and 10-rsa.conf sets RequiredRSASize 2048, so sshd refuses it. hardn runs as root on the console."},
		{name: "admin-no-sudo", label: "Admin without sudo", about: "sudo asks a password", host: sshAdminNoSudo, env: sshConsoleEnv,
			note: deb + "george alone, by key; his only grant is %sudo, which asks a password, and his account is \"!\". hardn runs as root on the console."},
	}
}

// sshConsoleEnv is the environment of hardn run as root on the console: no
// SSH_CONNECTION, no SUDO_USER or DOAS_USER.
var sshConsoleEnv = map[string]string{}

// sshVariants are hosts the tests and the refusals mock read that the
// menu does not list: debian13 and pve9 with one thing changed.
func sshVariants() []sshHost {
	sudo := map[string]string{"SSH_CONNECTION": applyConn, "SUDO_USER": "george"}
	deb := func(name, note, conf string) sshHost {
		return sshHost{name: name, note: note, env: sudo, host: func() *fakeHost {
			return sshDebian13().sshFile(sshd.DropInDir+"/10-site.conf", conf, 0o644)
		}}
	}
	return []sshHost{
		deb("allow-users-host", "AllowUsers george@198.51.100.7; the session is from 192.0.2.10.", "AllowUsers george@198.51.100.7\n"),
		deb("deny-users", "DenyUsers george.", "DenyUsers george\n"),
		deb("pubkey-off", "PubkeyAuthentication no.", "PubkeyAuthentication no\n"),
		deb("methods-password", "AuthenticationMethods publickey,password, passwords on; george has no password.", "AuthenticationMethods publickey,password\nPasswordAuthentication yes\n"),
		deb("algorithms-rsa", "PubkeyAcceptedAlgorithms without ssh-ed25519.", "PubkeyAcceptedAlgorithms rsa-sha2-512,rsa-sha2-256\n"),
		{name: "strict-home", note: "george's home group-writable (0775).", env: sudo, host: func() *fakeHost {
			h := sshDebian13()
			h.f.AddDir("/home/george", 0o775, 1000, 1000)
			return h
		}},
		{name: "key-elsewhere", note: "AuthorizedKeysFile .ssh/authorized_keys; george's keys in authorized_keys2.", env: sudo, host: func() *fakeHost {
			h := sshDebian13().sshFile(sshd.DropInDir+"/10-site.conf", "AuthorizedKeysFile .ssh/authorized_keys\n", 0o644)
			data, _ := h.f.ReadFile(context.Background(), "/home/george/.ssh/authorized_keys")
			_ = h.f.Remove(context.Background(), "/home/george/.ssh/authorized_keys")
			h.f.AddFile("/home/george/.ssh/authorized_keys2", data, 0o600, 1000, 1000)
			return h
		}},
		{name: "no-accepted", note: "sshd's log has no line for the session.", env: sudo, host: func() *fakeHost {
			h := sshDebian13()
			h.ssh.log = ""
			return h
		}},
		{name: "pve9-password", note: "pve9, root logged in by password.", env: map[string]string{"SSH_CONNECTION": applyConn}, host: func() *fakeHost {
			return usersOn(pve9Platform, false).rootLike(syntheticHash).withLog("password", "root", 0)
		}},
	}
}

// findSSHHost returns the host named name, after sshPrefix: the menu's,
// then the variants.
func findSSHHost(name string) (sshHost, bool) {
	all := slices.Concat(sshHosts(), sshVariants())
	i := slices.IndexFunc(all, func(h sshHost) bool { return h.name == name })
	if i < 0 {
		return sshHost{}, false
	}
	return all[i], true
}

// sshConfirmWindow is debian13 after the real port change to 2230 was
// applied and not confirmed: PlanSSH on the real engine, as
// engine/ssh-port-debian13 applies it.
func sshConfirmWindow() *fakeHost {
	h := sshDebian13()
	ctx := context.Background()
	e := sshEngine(h, "", nil)
	p, err := e.Plan(ctx, []engine.Control{sshControl(steps.SSH{Prefix: "ssh.port", Port: 2230})}, config.DefaultConfig())
	if err == nil && len(p.Refused) == 0 {
		_, _ = e.Apply(ctx, p)
	}
	return h
}

// sshEngine is the engine over h, as the kit's: the session's connection
// conn ("" for applyConn's, to port 22), the gallery's clock and hold.
func sshEngine(h *fakeHost, conn string, progress func(engine.Event)) *engine.Engine {
	return engine.New(h.system(), engine.Options{
		Revert:   revert.Options{Now: clock, Hardn: galleryHardn},
		Session:  sshSession(conn),
		Backups:  backup.Settings{Path: backup.DefaultPath, Enabled: true},
		Hold:     sshQuiet.hold,
		PID:      applyPID,
		Progress: progress,
	})
}

// sshQuiet is the session whose hold the mock's engines take outside play
// (the listing, a plan before asking, confirm-window's apply): the
// gallery's hold, which holds nothing.
var sshQuiet session

// sshSession is the applying session with connection conn: applySession's
// process, its connection conn where set.
func sshSession(conn string) revert.Session {
	s := applySession
	if conn != "" {
		s.Conn = conn
	}
	return s
}

// sshLogin is the session's login as sshd's log names it.
type sshLogin struct {
	user, method string // "" where not found
}

// sshAdmin is one admin and whether sshd lets them in from the session's
// address after the change (sshLoginPaths).
type sshAdmin struct {
	name string
	key  bool   // by key alone
	pwd  bool   // not by key alone, but with a password, where passwords stay on
	why  string // why not by key, in a sentence's middle
}

// sshReading is the host as the screen reads it.
type sshReading struct {
	f      detect.Facts
	status component.StatusList
	eff    sshd.Effective
	effErr error
	files  migrate.Found
	users  users.Reading
	usrErr error
	sess   revert.Session
	login  sshLogin
	cfg    *config.Config
	pve    bool
	// root is root's account and password state; rootLocked: sshd has no
	// PAM and every uid-0 account's field starts with "!" (check-ssh.md
	// §9), so no account logs in as root.
	root       accounts.User
	rootPW     accounts.Password
	rootLocked bool
	// listenPort is v0.3.x's listen.conf's port, "" where there is none.
	listenPort string
}

// readSSH reads the host as the screen does, writing nothing.
func readSSH(ctx context.Context, sys host.System, env map[string]string) (sshReading, error) {
	var r sshReading
	cfg, err := statusConfig(ctx, sys)
	if err != nil {
		return r, err
	}
	r.cfg = cfg
	e := engine.New(sys, engine.Options{
		Detect: func(ctx context.Context, sys host.System) (detect.Facts, error) {
			var err error
			r.f, err = detect.Detect(ctx, sys)
			return r.f, err
		},
		Hold: sshQuiet.hold,
	})
	reports, err := e.Check(ctx, controls.Status(cfg))
	if err != nil {
		return r, err
	}
	view := output.StatusViewOf(reports, risk.Assess(reports, probe.Read(ctx, sys, r.f))).Status
	keep := []string{ssh.Login{}.Title(), ssh.Auth{}.Title(), ssh.Port{}.Title()}
	view.Rows = slices.DeleteFunc(view.Rows, func(row component.StatusRow) bool { return !slices.Contains(keep, row.Label) })
	r.status = view
	var bin string
	if r.f.Init.Kind == detect.OpenRC {
		bin, _ = reload.OpenRCFlavour(ctx, sys)
	}
	r.eff, _, r.effErr = sshd.ReadEffective(ctx, sys, sshd.EffectiveRequest{SSH: r.f.SSH, Binary: bin})
	r.files, _ = migrate.Find(ctx, sys, migrate.FindRequest{ConfigFile: cfg.SshConfigFile})
	r.users, r.usrErr = users.Read(ctx, sys, r.f, func(k string) string { return env[k] })
	r.sess, _ = revert.ReadSession(ctx, sys, func(k string) string { return env[k] }, applyPID)
	r.login = sshSessionLogin(ctx, sys, r.f, r.sess)
	r.pve, _ = pve.Applies(r.f.Proxmox)
	r.readRoot(ctx, sys)
	if data, err := sys.ReadFile(ctx, migrate.ListenConf); err == nil {
		for _, l := range strings.Split(string(data), "\n") {
			if v, ok := strings.CutPrefix(strings.TrimSpace(l), "ListenStream="); ok && v != "" {
				r.listenPort = v
			}
		}
	}
	return r, nil
}

// readRoot reads root's account, its password state, and whether every
// uid-0 account is locked where sshd has no PAM: the first line of each
// name in /etc/passwd, as check-ssh.md §9 reads them.
func (r *sshReading) readRoot(ctx context.Context, sys host.System) {
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
			r.root = u
			if shErr == nil {
				r.rootPW = sh.PasswordOf(u)
			}
		}
		if u.UID == 0 {
			any0 = true
			if l, _ := sh.LockOf(u); shErr != nil || l != accounts.Locked {
				locked = false
			}
		}
	}
	r.rootLocked = r.effErr == nil && r.eff.UsePAM.Value != "yes" && any0 && locked
}

// console reports whether hardn runs from a console, not over SSH: closing
// an SSH login path cannot close the way in.
func (r sshReading) console() bool { return !r.sess.Remote }

// sshSessionLogin is a stand-in, unit (b)'s: the session's login as sshd
// logs it ("Accepted <method> for <user> from <addr> port <port> ssh2"),
// the line whose address and port are the session's connection's:
// journalctl -u ssh on systemd, /var/log/messages on OpenRC, as the three
// VMs log it. Nothing where the connection or the line is not found.
func sshSessionLogin(ctx context.Context, sys host.System, f detect.Facts, s revert.Session) sshLogin {
	c := strings.Fields(s.Conn)
	if len(c) != 4 {
		return sshLogin{}
	}
	var out string
	if f.Init.Kind == detect.OpenRC {
		data, err := sys.ReadFile(ctx, "/var/log/messages")
		if err != nil {
			return sshLogin{}
		}
		out = string(data)
	} else {
		res, err := sys.Run(ctx, "journalctl", "--no-pager", "-o", "short-iso", "-u", "ssh")
		if err != nil {
			return sshLogin{}
		}
		out = string(res.Stdout)
	}
	var l sshLogin
	for _, line := range strings.Split(out, "\n") {
		_, rest, ok := strings.Cut(line, ": Accepted ")
		f := strings.Fields(rest)
		if !ok || len(f) < 7 || f[1] != "for" || f[3] != "from" || f[4] != c[0] || f[5] != "port" || f[6] != c[1] {
			continue
		}
		l = sshLogin{user: f[2], method: f[0]}
	}
	return l
}

// rootOwn reports whether the session is root's own SSH login.
func (r sshReading) rootOwn() bool { return r.login.user == "root" }

// byKey reports whether the session logged in by key.
func (r sshReading) byKey() bool { return r.login.method == "publickey" }

// sshdSays is a stand-in, unit (b)'s: sshd's own answer for user from the
// session's address, sshd -T -C user=,host=,addr= over the installed files
// (the binary OpenRC runs on Alpine), so Match blocks are applied by sshd.
func sshdSays(ctx context.Context, sys host.System, f detect.Facts, user, addr string) (map[string][]string, error) {
	bin := sshd.DefaultBinary
	if f.Init.Kind == detect.OpenRC {
		if b, err := reload.OpenRCFlavour(ctx, sys); err == nil {
			bin = b
		}
	}
	res, err := sys.Run(ctx, bin, "-T", "-f", sshd.MainConfig, "-C", fmt.Sprintf("user=%s,host=%s,addr=%s", user, addr, addr))
	if err != nil {
		return nil, err
	}
	kw := map[string][]string{}
	for _, l := range strings.Split(string(res.Stdout), "\n") {
		if k, v, ok := strings.Cut(strings.TrimSpace(l), " "); ok {
			kw[k] = append(kw[k], v)
		}
	}
	return kw, nil
}

// sshLoginPaths is a stand-in, unit (b)'s: each admin besides root, and
// whether they could log in from the session's address once passwords are
// off (keyOnly) or as they are, and then become root (sshLoginPath): an
// admin who cannot do both is not shown able.
func (r sshReading) sshLoginPaths(ctx context.Context, sys host.System, keyOnly bool) []sshAdmin {
	var out []sshAdmin
	for _, u := range r.users.Users {
		if u.Admin != detect.Yes || u.UID == 0 {
			continue
		}
		a := r.sshLoginPath(ctx, sys, u.User, u.Password, u.Locked, keyOnly)
		if (a.key || a.pwd) && !u.NoPassword && u.Password != accounts.PasswordSet {
			tool := u.Tool
			if tool == "" {
				tool = "sudo"
			}
			a.key, a.pwd = false, false
			a.why = u.Name + " can't become root: " + tool + " asks a password " + u.Name + " hasn't got"
		}
		out = append(out, a)
	}
	return out
}

// sshAddr is the session's address, 192.0.2.10 where it is not read.
func (r sshReading) sshAddr() string {
	if c := strings.Fields(r.sess.Conn); len(c) == 4 {
		return c[0]
	}
	return "192.0.2.10"
}

// sshLoginPath is a stand-in, unit (b)'s: whether sshd lets u in from the
// session's address by key alone, or else with a password where passwords
// stay on (not keyOnly). It reads sshd's own answer for u (sshdSays, Match
// blocks applied by sshd), then matches AllowUsers and DenyUsers itself
// against the address (sshListed); AllowGroups and DenyGroups are not read,
// so a user under either is not shown able. The methods: publickey is a key
// in the files AuthorizedKeysFile names (authkeys.Read, a restricted key not
// counted) whose type PubkeyAcceptedAlgorithms accepts and, for RSA, whose
// modulus is at least RequiredRSASize bits, StrictModes passed
// (authkeys.CheckStrict), PubkeyAuthentication yes; password is
// PasswordAuthentication yes and a password set; keyboard-interactive is
// KbdInteractiveAuthentication yes, PAM in use and a password set. One of
// AuthenticationMethods' lists must be made of those alone (sshMethodsMet).
// The account's lock counts where sshd has no PAM (locked).
func (r sshReading) sshLoginPath(ctx context.Context, sys host.System, u accounts.User, pw accounts.Password, locked, keyOnly bool) sshAdmin {
	a := sshAdmin{name: u.Name}
	addr := r.sshAddr()
	kw, err := sshdSays(ctx, sys, r.f, u.Name, addr)
	one := func(k string) string { return strings.Join(kw[k], " ") }
	if err != nil {
		a.why = "sshd's answer for " + u.Name + " not read"
		return a
	}
	if ok, _, _ := sshListed(kw["denyusers"], u.Name, addr); ok {
		a.why = u.Name + " is in DenyUsers"
		return a
	}
	if allow := kw["allowusers"]; len(allow) > 0 {
		if ok, host, named := sshListed(allow, u.Name, addr); !ok {
			switch {
			case named:
				a.why = "AllowUsers names " + u.Name + "@" + host + ", a host name hardn doesn't match"
			case host != "":
				a.why = "AllowUsers lets " + u.Name + " in from " + host + " alone"
			default:
				a.why = u.Name + " isn't in AllowUsers"
			}
			return a
		}
	}
	switch {
	case len(kw["allowgroups"]) > 0 || len(kw["denygroups"]) > 0:
		a.why = "AllowGroups and DenyGroups aren't read"
		return a
	case locked:
		a.why = u.Name + "'s account is locked (!) and sshd has no PAM"
		return a
	}
	files := authkeys.Read(ctx, sys, strings.Fields(one("authorizedkeysfile")), u)
	keys := slices.DeleteFunc(authkeys.Keys(files), func(k authkeys.Key) bool { return k.Restricted() })
	algos := strings.Split(one("pubkeyacceptedalgorithms"), ",")
	minRSA := authkeys.MinRSABits
	if n, err := strconv.Atoi(one("requiredrsasize")); err == nil {
		minRSA = n
	}
	var typed, sized []authkeys.Key
	for _, k := range keys {
		if !sshTypeAccepted(k.Type, algos, len(kw["pubkeyacceptedalgorithms"]) > 0) {
			continue
		}
		typed = append(typed, k)
		if k.Type != "ssh-rsa" || sshRSABits(k.Blob) >= minRSA {
			sized = append(sized, k)
		}
	}
	var keyWhy string
	switch {
	case one("pubkeyauthentication") != "yes":
		keyWhy = "PubkeyAuthentication is off"
	case len(keys) == 0:
		keyWhy = u.Name + " has no key sshd reads"
	case len(typed) == 0:
		var ts []string
		for _, k := range keys {
			ts = append(ts, k.Type)
		}
		keyWhy = "sshd doesn't accept " + u.Name + "'s key type (" + strings.Join(slices.Compact(ts), ", ") + ")"
	case len(sized) == 0:
		keyWhy = fmt.Sprintf("%s's RSA key is %d bits, under RequiredRSASize %d", u.Name, sshRSABits(typed[0].Blob), minRSA)
	case authkeys.CheckStrict(ctx, sys, u) != nil:
		keyWhy = "StrictModes ignores " + u.Name + "'s keys"
	}
	pam := one("usepam") == "yes"
	have := map[string]bool{
		"publickey":            keyWhy == "",
		"password":             one("passwordauthentication") == "yes" && pw == accounts.PasswordSet,
		"keyboard-interactive": one("kbdinteractiveauthentication") == "yes" && pam && pw == accounts.PasswordSet,
	}
	am := one("authenticationmethods")
	a.key = sshMethodsMet(am, map[string]bool{"publickey": have["publickey"]})
	a.pwd = !a.key && !keyOnly && sshMethodsMet(am, have)
	switch {
	case a.key:
	case keyWhy != "":
		a.why = keyWhy
	default:
		a.why = "AuthenticationMethods " + am + " asks more than " + u.Name + " has"
	}
	return a
}

// sshMethodsMet reports whether one of AuthenticationMethods' lists (am as
// sshd -T prints it: lists apart by spaces, each a comma-separated chain, a
// method's ":submethod" ignored; "any", or none, any single method) is made
// only of methods have holds true.
func sshMethodsMet(am string, have map[string]bool) bool {
	lists := strings.Fields(am)
	if len(lists) == 0 {
		lists = []string{"any"}
	}
	for _, l := range lists {
		if l == "any" {
			for _, ok := range have {
				if ok {
					return true
				}
			}
			continue
		}
		all := true
		for _, m := range strings.Split(l, ",") {
			m, _, _ = strings.Cut(m, ":")
			all = all && have[m]
		}
		if all {
			return true
		}
	}
	return false
}

// sshTypeAccepted reports whether PubkeyAcceptedAlgorithms (algos, read
// where printed) accepts a key of type t: ssh-ed25519, ecdsa-sha2-nistp*
// and sk-* by their own name, ssh-rsa by any of ssh-rsa, rsa-sha2-256 and
// rsa-sha2-512.
func sshTypeAccepted(t string, algos []string, read bool) bool {
	if !read {
		return true
	}
	if t == "ssh-rsa" {
		return slices.ContainsFunc([]string{"ssh-rsa", "rsa-sha2-256", "rsa-sha2-512"}, func(a string) bool { return slices.Contains(algos, a) })
	}
	return slices.Contains(algos, t)
}

// sshRSABits is a stand-in: an ssh-rsa blob's modulus length in bits,
// parsed here as authkeys does not expose it (string type, mpint e, mpint
// n; RFC 4253 §6.6); 0 where it does not parse.
func sshRSABits(b64 string) int {
	blob, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return 0
	}
	var n []byte
	for range 3 {
		if len(blob) < 4 || int(binary.BigEndian.Uint32(blob)) > len(blob)-4 {
			return 0
		}
		l := int(binary.BigEndian.Uint32(blob))
		n, blob = blob[4:4+l], blob[4+l:]
	}
	for len(n) > 0 && n[0] == 0 {
		n = n[1:]
	}
	if len(n) == 0 {
		return 0
	}
	return (len(n)-1)*8 + bits.Len8(n[0])
}

// sshListed matches name, connecting from addr, against AllowUsers' or
// DenyUsers' patterns (USER or USER@HOST, sshd's match_pattern, * and ?):
// HOST an address, a CIDR, or a pattern on the address. A HOST that is a
// host name never matches: the client's name is not read. It returns
// whether one matches, and, where none does but USER did, the first such
// HOST and whether it is a host name.
func sshListed(list []string, name, addr string) (ok bool, host string, named bool) {
	for _, p := range strings.Fields(strings.Join(list, " ")) {
		up, hp, at := strings.Cut(p, "@")
		if !sshPattern(name, up) {
			continue
		}
		if !at {
			return true, "", false
		}
		isAddr := strings.Trim(hp, "0123456789.:*?/") == ""
		if pr, err := netip.ParsePrefix(hp); err == nil {
			if a, err := netip.ParseAddr(addr); err == nil && pr.Contains(a) {
				return true, "", false
			}
		} else if isAddr && sshPattern(addr, hp) {
			return true, "", false
		}
		if host == "" {
			host, named = hp, !isAddr
		}
	}
	return false, host, named
}

// sshPattern is sshd's match_pattern: * any run, ? any one character.
func sshPattern(s, p string) bool {
	for len(p) > 0 {
		switch p[0] {
		case '*':
			for i := len(s); i >= 0; i-- {
				if sshPattern(s[i:], p[1:]) {
					return true
				}
			}
			return false
		case '?':
			if len(s) == 0 {
				return false
			}
		default:
			if len(s) == 0 || s[0] != p[0] {
				return false
			}
		}
		s, p = s[1:], p[1:]
	}
	return len(s) == 0
}

// decided is the file whose line sshd reads first for keyword k, outside
// Match blocks: "" where none sets it (sshd's default).
func (r sshReading) decided(k string) string {
	for _, l := range sshd.ReadOrder(r.files.Main, r.files.DropIns) {
		if l.Match == nil && l.Keyword == k {
			return path.Base(l.File)
		}
	}
	return ""
}

// byNote is the deciding file as a row's note: "" for hardn's own.
func (r sshReading) byNote(k string) string {
	switch f := r.decided(k); f {
	case path.Base(sshd.HardnDropIn):
		return ""
	case "":
		return "sshd's default"
	default:
		return "by " + f
	}
}

// matchMore are the Match lines that let in more than the global value of
// k, as the SSH checks read them (every block counts).
func (r sshReading) matchMore(k, global string) []sshd.Line {
	rank := map[string]int{"no": 0, "forced-commands-only": 1, "prohibit-password": 2, "without-password": 2, "yes": 3}
	var out []sshd.Line
	for _, l := range sshd.ReadOrder(r.files.Main, r.files.DropIns) {
		if l.Match != nil && l.Keyword == k && len(l.Args) > 0 && rank[strings.ToLower(l.Args[0])] > rank[global] {
			out = append(out, l)
		}
	}
	return out
}

// rootValue is PermitRootLogin as sshd -T prints it, without-password read
// as prohibit-password.
func (r sshReading) rootValue() string {
	if v := r.eff.PermitRootLogin.Value; v != "without-password" {
		return v
	}
	return "prohibit-password"
}

// passwordsOn reports whether a password logs in: PasswordAuthentication,
// or keyboard-interactive where sshd uses PAM.
func (r sshReading) passwordsOn() bool {
	return sshMethodsMet(r.methods(), r.passwordMethods())
}

// methods is AuthenticationMethods as sshd -T prints it.
func (r sshReading) methods() string {
	return strings.Join(r.eff.Keywords["authenticationmethods"], " ")
}

// passwordMethods are the password methods sshd offers: password where
// PasswordAuthentication is yes, keyboard-interactive where it is yes and
// sshd uses PAM.
func (r sshReading) passwordMethods() map[string]bool {
	return map[string]bool{"password": r.eff.PasswordAuthentication.Value == "yes",
		"keyboard-interactive": r.eff.KbdInteractiveAuthentication.Value == "yes" && r.eff.UsePAM.Value == "yes"}
}

// rows are the SSH Configuration section: what sshd runs with, from sshd
// -T and the files, never hardn.yml.
func (r sshReading) rows() []component.KeyValue {
	if r.effErr != nil {
		return []component.KeyValue{{Key: sshRowRoot, Value: "Can't read", State: component.ValueUnknown},
			{Key: sshRowPass, Value: "Can't read", State: component.ValueUnknown},
			{Key: sshRowPort, Value: "Can't read", State: component.ValueUnknown}}
	}
	var rows []component.KeyValue
	switch v := r.rootValue(); {
	case v != "no" && r.rootLocked:
		rows = append(rows, component.KeyValue{Key: sshRowRoot, Value: "Locked", Note: "root's account, no PAM"})
	case v == "no":
		rows = append(rows, component.KeyValue{Key: sshRowRoot, Value: "Disallowed", Note: r.byNote("permitrootlogin")})
	case v == "yes":
		rows = append(rows, component.KeyValue{Key: sshRowRoot, Value: "Allowed", State: component.ValueBad, Note: r.byNote("permitrootlogin")})
	case v == "forced-commands-only":
		rows = append(rows, component.KeyValue{Key: sshRowRoot, Value: "Commands only", Note: r.byNote("permitrootlogin")})
	default:
		rows = append(rows, component.KeyValue{Key: sshRowRoot, Value: "Key only", Note: r.byNote("permitrootlogin")})
	}
	pm := r.passwordMethods()
	switch {
	case !r.passwordsOn() && sshMethodsMet("any", pm): // offered, but no list is met by them alone
		// "in <file>" does not fit the row at 80 columns: the file is the
		// Old Settings row's, or 00-hardn.conf.
		rows = append(rows, component.KeyValue{Key: sshRowPass, Value: "Off", Note: "by AuthenticationMethods"})
	case !r.passwordsOn():
		rows = append(rows, component.KeyValue{Key: sshRowPass, Value: "Off", Note: r.byNote("passwordauthentication")})
	case sshMethodsMet(r.methods(), map[string]bool{"password": pm["password"]}) || !pm["keyboard-interactive"]:
		rows = append(rows, component.KeyValue{Key: sshRowPass, Value: "On", State: component.ValueWarn, Note: r.byNote("passwordauthentication")})
	default:
		rows = append(rows, component.KeyValue{Key: sshRowPass, Value: "On, keyboard-interactive", State: component.ValueWarn, Note: r.byNote("kbdinteractiveauthentication")})
	}
	var ports []string
	for _, p := range r.eff.Listeners.Ports() {
		ports = append(ports, strconv.Itoa(int(p)))
	}
	port := component.KeyValue{Key: sshRowPort, Value: strings.Join(ports, ", ")}
	if len(ports) == 0 {
		port.Value, port.State = "Can't read", component.ValueUnknown
	}
	if r.f.SSH.Mode == detect.SSHSocket {
		port.Note = "ssh.socket"
	}
	rows = append(rows, port)
	if us := r.eff.AllowUsers.Values; len(us) > 0 {
		rows = append(rows, component.KeyValue{Key: sshRowUsers, Value: strings.Join(us, " "), Note: r.byNote("allowusers")})
	} else {
		rows = append(rows, component.KeyValue{Key: sshRowUsers, Value: "Anyone"})
	}
	ms := slices.Concat(r.matchMore("permitrootlogin", r.rootValue()), r.matchMore("passwordauthentication", r.eff.PasswordAuthentication.Value))
	if r.eff.UsePAM.Value == "yes" {
		ms = append(ms, r.matchMore("kbdinteractiveauthentication", r.eff.KbdInteractiveAuthentication.Value)...)
	}
	if len(ms) > 0 {
		rows = append(rows, component.KeyValue{Key: sshRowMatch, Value: "Lets more in", State: component.ValueWarn,
			Note: path.Base(ms[0].File)})
	}
	if len(r.files.Artifacts) > 0 {
		var names []string
		for _, a := range r.files.Artifacts {
			names = append(names, path.Base(a.Path))
		}
		rows = append(rows, component.KeyValue{Key: sshRowLegacy, Value: strings.Join(names, ", "), State: component.ValueWarn})
	}
	you := component.KeyValue{Key: sshRowYou, Value: "Not known", State: component.ValueUnknown}
	switch m := r.login.method; {
	case r.login.user == "":
	case m == "publickey":
		you = component.KeyValue{Key: sshRowYou, Value: r.login.user + ", by key"}
	default:
		you = component.KeyValue{Key: sshRowYou, Value: r.login.user + ", by password", State: component.ValueWarn}
	}
	return append(rows, you)
}

// sshExit is the sub-menu's Return.
var sshExit = component.MenuItem{Key: "0", Label: "Return"}

// items are the menu's items, each named by its result (P-12): root login,
// password login, the port; moving hardn's old files where there are any.
func (r sshReading) items() []component.MenuItem {
	root := component.MenuItem{Key: "1", Label: sshDisallow, Description: sshDisallowDesc}
	switch {
	case r.pve:
		root.Label, root.Description = sshPVERoot, sshPVERootDesc
	case r.effErr == nil && r.rootValue() == "no":
		root.Label, root.Description = sshKeyOnlyRoot, sshKeyRootDesc
	}
	pass := component.MenuItem{Key: "2", Label: sshKeyOnly, Description: sshKeyOnlyDesc}
	if r.effErr == nil && !r.passwordsOn() {
		pass.Label, pass.Description = sshPassOn, sshPassOnDesc
	}
	port := component.MenuItem{Key: "3", Label: sshPort, Description: "Now " + r.portWords()}
	items := []component.MenuItem{root, pass, port}
	if len(r.files.Artifacts) > 0 {
		items = append(items, component.MenuItem{Key: "4", Label: sshMigrate, Description: sshMigrateDesc})
	}
	if r.effErr != nil { // sshd -T unread: each item's result not known
		for i := range items {
			items[i].Description = "Can't read"
		}
	}
	return items
}

// portWords are the ports sshd listens on, "22" or "22 and 2230".
func (r sshReading) portWords() string {
	var ps []string
	for _, p := range r.eff.Listeners.Ports() {
		ps = append(ps, strconv.Itoa(int(p)))
	}
	if len(ps) == 0 {
		return "not read"
	}
	return strings.Join(ps, " and ")
}

// screen is 02-SSH_LOGIN: the three rows, the section, the items.
func (r sshReading) screen() screen.SubMenu {
	return screen.SubMenu{
		Title:    component.TitleBar{Title: sshTitle},
		Status:   r.status,
		Sections: []screen.Section{{Header: component.SectionHeader{Title: sshSection}, Rows: component.BulletList{Items: r.rows()}}},
		Heading:  sshHeading,
		Menu:     component.Menu{Items: r.items(), Exit: sshExit},
		Prompt:   sshPrompt,
	}
}

// The refusals' words (gallery-ssh.md §6, proposed).
const (
	sshWordsNotRead     = "hardn can't read sshd's settings: %s. Nothing can change until it can."
	sshWordsNoRunDir    = "/run/sshd isn't there yet, so sshd -T can't run. Open one SSH connection, then try again."
	sshWordsRootOwn     = "You're logged in as root over SSH. Turning root login off would close the way you came in. Log in as an admin and run hardn through sudo."
	sshWordsNoSession   = "hardn can't tell how you logged in: sshd's log has no line for this connection. Nothing that closes a way in can change."
	sshWordsNoAdmin     = "No admin could log in over SSH afterwards: %s. Fix that first."
	sshWordsByPassword  = "You logged in with a password. Turning passwords off would close the way you came in. Log in with a key first."
	sshWordsPassNoKey   = "%s would lose SSH login: %s a password and no key. Add a key in User Management first."
	sshWordsMatch       = "%s, in %s, lets more in for some connections, and hardn's setting wouldn't change that. Change it there first."
	sshWordsPVEKeyOnly  = "Root stays key-only on Proxmox VE: the cluster's nodes log in to each other as root by key."
	sshWordsPVEByPass   = "You logged in as root with a password. Making root key-only would close the way you came in. Log in as root with a key first."
	sshWordsPVENoKey    = "Root would lose SSH login: %s. Add a key for root first."
	sshWordsListenConf  = "hardn v0.3.x's listen.conf (port %s for ssh.socket) never took effect: ssh.socket listens on %s. Moving it now couldn't be undone. Let %s through the firewall, run systemctl daemon-reload and systemctl restart ssh.socket, check a new connection to %s works, then try again."
	sshWordsPVEPort     = "The SSH port stays 22 on Proxmox VE: the cluster and pve-firewall use it."
	sshWordsRestricted  = "ufw lets port 22 in from %s alone. A new port would be open to anyone. Move that rule to the new port by hand first."
	sshWordsMigrate     = "hardn's old SSH files are still here (%s). Move them into 00-hardn.conf first: item 4."
	sshWordsIncludeNot  = "sshd_config doesn't start with \"" + sshd.IncludeLine + "\", so hardn's settings wouldn't be read first. Put that line first, then try again."
	sshWordsPending     = "A change is waiting for hardn confirm. Confirm it from a new connection, or let it roll back, then try again."
	sshWordsInUse       = "Port %d is in use. Pick another."
	sshWordsPortSame    = "sshd listens on %d already."
	sshWordsPortBad     = "Type a port from 1 to 65535."
	sshWordsNothingLeft = "Nothing to move."
)

// notReadWords are the words for sshd not read.
func notReadWords(err error) string {
	var env *sshd.EnvError
	if errors.As(err, &env) && env.Kind == sshd.EnvPrivsepDir {
		return sshWordsNoRunDir
	}
	return fmt.Sprintf(sshWordsNotRead, err)
}

// planWords are a refusal of the engine's plan in the screen's words.
func (r sshReading) planWords(err error) string {
	var words string
	switch {
	case errors.Is(err, steps.ErrMigrateFirst):
		words = fmt.Sprintf(sshWordsMigrate, r.legacyNames())
	case errors.Is(err, steps.ErrNoWriteStrategy):
		words = sshWordsIncludeNot
	case errors.Is(err, pve.ErrPortChange):
		words = sshWordsPVEPort
	case errors.Is(err, revert.ErrPending):
		words = sshWordsPending
	case errors.Is(err, reload.ErrInUse):
		words = "A port the change adds is in use. Pick another."
	case errors.Is(err, steps.ErrListenConfNotInEffect):
		var ps []string
		for _, l := range r.eff.Listeners.Socket {
			ps = append(ps, strconv.Itoa(int(l.Port)))
		}
		slices.Sort(ps)
		p := r.listenPort
		words = fmt.Sprintf(sshWordsListenConf, p, strings.Join(slices.Compact(ps), " and "), p, p)
	default:
		words = screenkit.RefusalWords(err)
	}
	return words
}

// legacyNames are hardn's old files' names.
func (r sshReading) legacyNames() string {
	var names []string
	for _, a := range r.files.Artifacts {
		names = append(names, path.Base(a.Path))
	}
	return strings.Join(names, ", ")
}

// common is why no change can be planned, before anything is asked: sshd
// not read; hardn's old files (but for moving them); an Include not first.
func (r sshReading) common(migrating bool) string {
	switch {
	case r.effErr != nil:
		return notReadWords(r.effErr)
	case len(r.files.Artifacts) > 0 && !migrating:
		return fmt.Sprintf(sshWordsMigrate, r.legacyNames())
	case r.files.Include.Mode == sshd.IncludeOther:
		return sshWordsIncludeNot
	}
	return ""
}

// noAdmin is why no admin could log in afterwards, "" when one can.
func noAdmin(admins []sshAdmin, keyOnly bool) string {
	var whys []string
	for _, a := range admins {
		if a.key || (!keyOnly && a.pwd) {
			return ""
		}
		whys = append(whys, a.why)
	}
	if len(whys) == 0 {
		return fmt.Sprintf(sshWordsNoAdmin, "there's no admin besides root")
	}
	return fmt.Sprintf(sshWordsNoAdmin, strings.Join(whys, "; "))
}

// rootRefusal is why the root item refuses, before asking.
func (r sshReading) rootRefusal(ctx context.Context, sys host.System) string {
	if w := r.common(false); w != "" {
		return w
	}
	switch {
	case r.pve && r.rootValue() == pve.RootLogin:
		return sshWordsPVEKeyOnly
	case r.rootValue() == "no": // Allow root key-only opens a way in: no check of the admins
		return ""
	case r.console(): // from a console: no SSH session to keep, the admins' paths alone
	case r.login.user == "":
		return sshWordsNoSession
	case r.rootOwn() && !r.pve:
		return sshWordsRootOwn
	}
	if ms := r.matchMore("permitrootlogin", "no"); len(ms) > 0 {
		return fmt.Sprintf(sshWordsMatch, strings.TrimSpace(ms[0].Match.Text), path.Base(ms[0].File))
	}
	if r.pve { // root key-only: root's own key is the way in kept
		if r.rootOwn() && !r.byKey() {
			return sshWordsPVEByPass
		}
		if a := r.sshLoginPath(ctx, sys, r.root, r.rootPW, r.rootLocked, true); !a.key {
			return fmt.Sprintf(sshWordsPVENoKey, a.why)
		}
		return ""
	}
	return noAdmin(r.sshLoginPaths(ctx, sys, !r.passwordsOn()), !r.passwordsOn())
}

// passwordRefusal is why Key-only login refuses, before asking; Password
// login on refuses only where nothing can change.
func (r sshReading) passwordRefusal(ctx context.Context, sys host.System) string {
	if w := r.common(false); w != "" {
		return w
	}
	if !r.passwordsOn() {
		return ""
	}
	switch {
	case r.console():
	case r.login.user == "":
		return sshWordsNoSession
	case !r.byKey():
		return sshWordsByPassword
	}
	if ms := r.matchMore("passwordauthentication", "no"); len(ms) > 0 {
		return fmt.Sprintf(sshWordsMatch, strings.TrimSpace(ms[0].Match.Text), path.Base(ms[0].File))
	}
	if r.pve { // root by key, which pve.PlanSSH keeps: no admin besides root
		return ""
	}
	admins := r.sshLoginPaths(ctx, sys, true)
	for _, a := range admins {
		u := r.user(a.name)
		if !a.key && u.Password == accounts.PasswordSet && len(u.Keys) == 0 {
			return fmt.Sprintf(sshWordsPassNoKey, a.name, a.name+" has")
		}
	}
	return noAdmin(admins, true)
}

// user is the users reading's user named name.
func (r sshReading) user(name string) users.User {
	i := slices.IndexFunc(r.users.Users, func(u users.User) bool { return u.Name == name })
	if i < 0 {
		return users.User{}
	}
	return r.users.Users[i]
}

// portRefusal is why Change SSH port refuses, before asking.
func (r sshReading) portRefusal(ctx context.Context, sys host.System) string {
	if w := r.common(false); w != "" {
		return w
	}
	if r.pve {
		return sshWordsPVEPort
	}
	if src := restricted22(ctx, sys); src != "" {
		return fmt.Sprintf(sshWordsRestricted, src)
	}
	return ""
}

// restricted22 is a stand-in, unit (b)'s: the source of an active ufw's
// rule allowing port 22 from less than anywhere, from ufw status verbose;
// "" when none.
func restricted22(ctx context.Context, sys host.System) string {
	res, err := sys.Run(ctx, "ufw", "status", "verbose")
	if err != nil || !strings.Contains(string(res.Stdout), "Status: active") {
		return ""
	}
	for _, l := range strings.Split(string(res.Stdout), "\n") {
		f := strings.Fields(l)
		if len(f) >= 4 && (f[0] == "22" || f[0] == "22/tcp") && f[1] == "ALLOW" && !strings.HasPrefix(f[len(f)-1], "Anywhere") {
			return f[len(f)-1]
		}
	}
	return ""
}

// sshControl2 is the gallery-local control, unit (b)'s: a real SSH
// control's id, title and check (ssh.Login, Auth or Port, CheckOnly until
// this slice), its Plan the change's: steps.PlanSSH or PlanMigration.
type sshControl2 struct {
	engine.Control
	plan func(ctx context.Context, sys host.System, in engine.Input) ([]engine.Change, error)
}

// Plan plans the change, reading the host where the engine plans.
func (c sshControl2) Plan(ctx context.Context, sys host.System, in engine.Input) ([]engine.Change, error) {
	return c.plan(ctx, sys, in)
}

// sshSet is a control that plans s through PlanSSH, under control c.
func sshSet(c engine.Control, s steps.SSH) engine.Control {
	s.Prefix = c.ID()
	return sshControl2{Control: c, plan: func(ctx context.Context, sys host.System, in engine.Input) ([]engine.Change, error) {
		return steps.PlanSSH(ctx, sys, in, s)
	}}
}

// sshMigration is a control that plans moving hardn's old files.
func sshMigration(configFile string) engine.Control {
	return sshControl2{Control: ssh.Login{}, plan: func(ctx context.Context, sys host.System, in engine.Input) ([]engine.Change, error) {
		return steps.PlanMigration(ctx, sys, in, "ssh.migrate", configFile)
	}}
}

// directive is one sshd directive.
func directive(k, v string) migrate.Directive {
	return migrate.Directive{Keyword: k, Args: []string{v}}
}

// sshRunner runs the mock on one host.
type sshRunner struct {
	s     *session
	sh    sshHost
	h     *fakeHost
	title string
}

// openSSH returns the action that opens what name names: the hosts' menu
// for sshPrefix alone, the mock on one host for "ssh/<host>".
func (s *session) openSSH(name string) func() error {
	if sh, ok := findSSHHost(strings.TrimPrefix(name, sshPrefix+"/")); ok && name != sshPrefix {
		return func() error { return s.sshScreen(context.Background(), sh, sh.host()) }
	}
	return func() error {
		all := sshHosts()
		vm := screen.SubMenu{Title: component.TitleBar{Title: "SSH Login hosts"}, Heading: "Select a host", Prompt: sshPrompt, Menu: component.Menu{Exit: sshExit}}
		menu := nav.Menu{Exit: vm.Menu.Exit}
		for i, sh := range all {
			it := component.MenuItem{Key: fmt.Sprint(i + 1), Label: sh.label, Description: sh.about}
			vm.Menu.Items = append(vm.Menu.Items, it)
			menu.Options = append(menu.Options, nav.Option{Item: it, Action: func() error { return s.sshScreen(context.Background(), sh, sh.host()) }})
		}
		return s.p.Run(func() nav.Screen { return vm.Screen(s.p.Ctx, menu) })
	}
}

// sshScreen runs the mock on h until the user goes back: its note, then
// each round the host read again.
func (s *session) sshScreen(ctx context.Context, sh sshHost, h *fakeHost) error {
	e := &sshRunner{s: s, sh: sh, h: h, title: sshPrefix + "/" + sh.name}
	if err := s.writeNote(e.title, sh.note); err != nil {
		return err
	}
	return s.p.Run(func() nav.Screen { return e.draw(ctx) })
}

// draw is 02-SSH_LOGIN, its items bound.
func (e *sshRunner) draw(ctx context.Context) nav.Screen {
	r, err := readSSH(ctx, e.h.sys(), e.sh.env)
	if err != nil {
		vm := screen.SubMenu{Title: component.TitleBar{Title: sshTitle}, Menu: component.Menu{Exit: sshExit}, Prompt: sshPrompt}
		return vm.Screen(e.s.p.Ctx, nav.Menu{Exit: sshExit})
	}
	vm := r.screen()
	actions := map[string]func() error{
		"1": func() error { return e.root(ctx, r) },
		"2": func() error { return e.password(ctx, r) },
		"3": func() error { return e.port(ctx, r) },
		"4": func() error { return e.migrate(ctx, r) },
	}
	menu := nav.Menu{Exit: vm.Menu.Exit}
	for _, it := range vm.Menu.Items {
		menu.Options = append(menu.Options, nav.Option{Item: it, Action: actions[it.Key]})
	}
	return vm.Screen(e.s.p.Ctx, menu)
}

// refuse says words before asking anything.
func (e *sshRunner) refuse(r sshReading, words string) error {
	return e.kit(r).Say(screenkit.Warnings(words))()
}

// sshConfirm is the question: the hostname typed for a lockout-risk plan,
// as Firewall's Enable asks it; y/N otherwise.
func sshConfirm(question string) func(flow.Plan) flow.Confirm {
	return func(p flow.Plan) flow.Confirm {
		if p.Typed() {
			return flow.Confirm{Question: question, Label: typeTheHostname, Phrase: hostName}
		}
		return flow.Confirm{Question: question}
	}
}

// preplan plans c on the host, writing nothing, and returns the first
// refusal in the screen's words: the engine's own, before asking.
func (e *sshRunner) preplan(ctx context.Context, r sshReading, c engine.Control) string {
	p, err := sshEngine(e.h, r.sess.Conn, nil).Plan(ctx, []engine.Control{c}, r.cfg)
	switch {
	case err != nil:
		return r.planWords(err)
	case len(p.Refused) > 0:
		return r.planWords(p.Refused[0])
	}
	return ""
}

// change runs c through the flow: its review, its plan, the question;
// refused in the screen's words where the engine refuses its plan.
func (e *sshRunner) change(ctx context.Context, r sshReading, title, question string, review flow.Review, fields []form.Field, c func() engine.Control) error {
	if len(fields) == 0 {
		if words := e.preplan(ctx, r, c()); words != "" {
			return e.refuse(r, words)
		}
	}
	return e.kit(r).Change(ctx, screenkit.Change{
		Step: screen.WorkflowStep{Title: component.TitleBar{Title: title, Parents: []string{sshTitle}}},
		Flow: flow.Flow{Form: form.Form{Fields: fields}, Review: func() flow.Review { return review }, Confirm: sshConfirm(question)},
		Controls: func(*config.Config) []engine.Control {
			return []engine.Control{c()}
		},
	})
}

// root is item 1: Disallow root login, or Allow root key-only.
func (e *sshRunner) root(ctx context.Context, r sshReading) error {
	sys := e.h.sys()
	if words := r.rootRefusal(ctx, sys); words != "" {
		return e.refuse(r, words)
	}
	want, label, question, value := "no", sshDisallow, "Disallow root login?", "Disallowed"
	switch {
	case r.pve:
		want, label, question, value = pve.RootLogin, sshPVERoot, "Make root key-only?", "Key only"
	case r.rootValue() == "no":
		want, label, question, value = "prohibit-password", sshKeyOnlyRoot, "Allow root to log in by key?", "Key only"
	}
	review := flow.Review{Title: sshSection, Items: []component.KeyValue{{Key: sshRowRoot, Value: value}}}
	return e.change(ctx, r, label, question, review, nil, func() engine.Control {
		return sshSet(ssh.Login{}, steps.SSH{Set: []migrate.Directive{directive("PermitRootLogin", want)}})
	})
}

// password is item 2: Key-only login, or Password login on.
func (e *sshRunner) password(ctx context.Context, r sshReading) error {
	sys := e.h.sys()
	if words := r.passwordRefusal(ctx, sys); words != "" {
		return e.refuse(r, words)
	}
	set := []migrate.Directive{directive("PasswordAuthentication", "no")}
	label, question, value := sshKeyOnly, "Turn password login off?", "Off"
	switch {
	case !r.passwordsOn():
		set = []migrate.Directive{directive("PasswordAuthentication", "yes")}
		label, question, value = sshPassOn, "Turn password login on?", "On"
	case r.eff.KbdInteractiveAuthentication.Value == "yes" && r.eff.UsePAM.Value == "yes":
		set = append(set, directive("KbdInteractiveAuthentication", "no"))
	}
	review := flow.Review{Title: sshSection, Items: []component.KeyValue{{Key: sshRowPass, Value: value}}}
	return e.change(ctx, r, label, question, review, nil, func() engine.Control {
		return sshSet(ssh.Auth{}, steps.SSH{Set: set})
	})
}

// port is item 3: the new port, then its plan, 22 held until confirmed.
func (e *sshRunner) port(ctx context.Context, r sshReading) error {
	sys := e.h.sys()
	if words := r.portRefusal(ctx, sys); words != "" {
		return e.refuse(r, words)
	}
	var value string
	have := r.eff.Listeners.Ports()
	field := &form.Text{Title: sshPortField, Value: &value, Validate: func(s string) error {
		n, err := strconv.Atoi(strings.TrimSpace(s))
		switch {
		case err != nil || n < 1 || n > 65535:
			return nav.Refuse(sshWordsPortBad)
		case slices.Contains(have, uint16(n)):
			return nav.Warn(fmt.Sprintf(sshWordsPortSame, n))
		}
		return nil
	}}
	review := flow.Review{Title: sshSection, Items: []component.KeyValue{{Key: sshRowPort, Value: "22 held until confirmed"}}}
	return e.change(ctx, r, sshPort, "Change the SSH port?", review, []form.Field{field}, func() engine.Control {
		n, _ := strconv.Atoi(strings.TrimSpace(value))
		return sshSet(ssh.Port{}, steps.SSH{Port: uint16(n)})
	})
}

// migrate is item 4: hardn's old files moved into 00-hardn.conf.
func (e *sshRunner) migrate(ctx context.Context, r sshReading) error {
	if words := r.common(true); words != "" {
		return e.refuse(r, words)
	}
	if words := e.preplan(ctx, r, sshMigration(r.cfg.SshConfigFile)); words != "" {
		return e.refuse(r, words)
	}
	review := flow.Review{Title: sshSection, Items: []component.KeyValue{{Key: sshRowLegacy, Value: r.legacyNames() + ", into 00-hardn.conf"}}}
	return e.change(ctx, r, sshMigrate, "Move hardn's old settings?", review, nil, func() engine.Control {
		return sshMigration(r.cfg.SshConfigFile)
	})
}

// kit is the screen kit's session on the host, the engine's runs recorded
// for the end of play.
func (e *sshRunner) kit(r sshReading) screenkit.Session {
	s, h := e.s, e.h
	var rec *recorder
	cfg := r.cfg
	if cfg == nil {
		cfg = config.DefaultConfig()
	}
	return screenkit.Session{
		Name: "ssh mock", Prompter: s.p, System: h.system(), Config: cfg,
		DryRun: s.sequence == "dry-run", LogPath: output.LegacyLogPath, Now: clock,
		NewEngine: func(progress func(engine.Event)) *engine.Engine {
			rc := &recorder{host: h, f: h.f, title: e.title}
			rec = rc
			s.mu.Lock()
			s.engineRuns = append(s.engineRuns, rc)
			s.mu.Unlock()
			rc.begin()
			return engine.New(h.system(), engine.Options{
				Revert:   revert.Options{Now: clock, Hardn: galleryHardn},
				Session:  sshSession(r.sess.Conn),
				Backups:  backup.Settings{Path: backup.DefaultPath, Enabled: true},
				Hold:     s.hold,
				PID:      applyPID,
				Progress: func(ev engine.Event) { rc.event(ev); progress(ev) },
			})
		},
		Ran: func(run output.Run) {
			rec.end()
			rec.setPlan(run.Plan)
			rec.failedWith(run.Err)
		},
	}
}

// sshReadNamed reads the host named name, fresh.
func sshReadNamed(ctx context.Context, name string) (sshReading, *fakeHost, error) {
	sh, ok := findSSHHost(name)
	if !ok {
		return sshReading{}, nil, fmt.Errorf("no ssh host %q", name)
	}
	h := sh.host()
	r, err := readSSH(ctx, h.sys(), sh.env)
	return r, h, err
}

// sshMocks are the listing's mocks: the screen on each host, then the
// refusals.
func sshMocks() []listMock {
	var mocks []listMock
	for _, sh := range sshHosts() {
		mocks = append(mocks, listMock{name: "screen-" + sh.name, note: "SSH Login. " + sh.note,
			draw: func(ctx context.Context, c component.Ctx) (string, error) {
				r, _, err := sshReadNamed(ctx, sh.name)
				if err != nil {
					return "", err
				}
				return r.screen().Render(c), nil
			}})
	}
	return append(mocks, listMock{name: "refusals", note: "The refusals before asking, each from the real reading: the item, the host.", draw: sshRefusalsMock})
}

// sshRefusal is one refusal the refusals mock draws and the tests hold.
type sshRefusal struct{ item, host string }

// sshRefusals are the lockout paths and refusals, by item and host.
func sshRefusals() []sshRefusal {
	return []sshRefusal{
		{"root", "root-session"},       // root's own SSH login
		{"root", "nopam-locked"},       // no admin able: "!" without PAM
		{"root", "rsa-small"},          // no admin able: an RSA key under RequiredRSASize
		{"root", "admin-no-sudo"},      // no admin able: sudo asks a password george hasn't got
		{"root", "allow-users-host"},   // no admin able: AllowUsers george@198.51.100.7
		{"root", "no-accepted"},        // the session not known: no Accepted line
		{"root", "match-allows"},       // a Match block lets root in
		{"root", "not-read"},           // sshd -T fails
		{"root", "legacy-main"},        // origin/main's hardn.conf
		{"root", "include-other"},      // P-38
		{"root", "confirm-window"},     // a change pending
		{"password", "password-login"}, // alice: a password, no key
		{"password", "password-session"},
		{"root", "pve9-password"}, // root's own login by password
		{"port", "pve9"},
		{"port", "ufw-restricted"},
		{"port", "legacy-v032"},
		{"migrate", "legacy-v032"}, // listen.conf never in effect (P-39)
	}
}

// refusalOf is rf's words on its host, "" for none: the screen's own
// checks, then the engine's plan.
func refusalOf(ctx context.Context, rf sshRefusal) (string, error) {
	r, h, err := sshReadNamed(ctx, rf.host)
	if err != nil {
		return "", err
	}
	sys := h.sys()
	e := &sshRunner{h: h}
	switch rf.item {
	case "root":
		if w := r.rootRefusal(ctx, sys); w != "" {
			return w, nil
		}
		return e.preplan(ctx, r, sshSet(ssh.Login{}, steps.SSH{Set: []migrate.Directive{directive("PermitRootLogin", "no")}})), nil
	case "migrate":
		if w := r.common(true); w != "" {
			return w, nil
		}
		return e.preplan(ctx, r, sshMigration(r.cfg.SshConfigFile)), nil
	case "password":
		if w := r.passwordRefusal(ctx, sys); w != "" {
			return w, nil
		}
		return e.preplan(ctx, r, sshSet(ssh.Auth{}, steps.SSH{Set: []migrate.Directive{directive("PasswordAuthentication", "no")}})), nil
	}
	return r.portRefusal(ctx, sys), nil
}

// sshRefusalsMock draws each refusal under a header naming it.
func sshRefusalsMock(ctx context.Context, c component.Ctx) (string, error) {
	var out []string
	for _, rf := range sshRefusals() {
		words, err := refusalOf(ctx, rf)
		if err != nil {
			return "", err
		}
		if words == "" {
			return "", fmt.Errorf("%v: no refusal", rf)
		}
		out = append(out, header(c, rf.item+", "+rf.host), screenkit.Warnings(words).Render(c))
	}
	return apart(out...), nil
}

// sshUsage is the listing's usage line.
const sshUsage = "usage: go run ./internal/tools/uigallery ssh [flags] [mock | all]..."

// sshListing is the SSH Login listing.
var sshListing = listing{prefix: sshPrefix, usage: sshUsage, label: "Mock:", mocks: sshMocks}

// sshStart returns the hosts' menu, or the host, that args name, and
// whether they name one; an error for a host that is not one.
func sshStart(args []string) (string, bool, error) {
	if len(args) != 1 || (args[0] != sshPrefix && !strings.HasPrefix(args[0], sshPrefix+"/")) {
		return "", false, nil
	}
	if name := strings.TrimPrefix(args[0], sshPrefix+"/"); args[0] != sshPrefix {
		if _, ok := findSSHHost(name); !ok {
			var names []string
			for _, h := range sshHosts() {
				names = append(names, sshPrefix+"/"+h.name)
			}
			return "", false, fmt.Errorf("no ssh host %q; the hosts are %s", args[0], strings.Join(names, ", "))
		}
	}
	return args[0], true, nil
}

// sshNote is play's note for the mock.
const sshNote = "Play mode, mocks: the SSH Login screen proposed for slice 6, drawn from what real code reads of a simulated host " +
	"(the SSH checks, sshd.ReadEffective, migrate.Find, users.Read, revert.ReadSession), each change the real steps.PlanSSH or PlanMigration on the real plan/apply engine. " +
	"The lockout checks before asking are the gallery's stand-ins, unit (b)'s to build. Nothing on this machine changes: every command and file is the fake's."
