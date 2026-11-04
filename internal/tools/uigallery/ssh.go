package main

import (
	"context"
	"errors"
	"fmt"
	"path"
	"slices"
	"strconv"
	"strings"

	"github.com/abbott/hardn/pkg/config"
	"github.com/abbott/hardn/pkg/engine"
	"github.com/abbott/hardn/pkg/engine/controls"
	"github.com/abbott/hardn/pkg/engine/controls/ssh"
	"github.com/abbott/hardn/pkg/engine/controls/sshchange"
	probe "github.com/abbott/hardn/pkg/engine/header"
	"github.com/abbott/hardn/pkg/engine/risk"
	"github.com/abbott/hardn/pkg/engine/steps"
	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/backup"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/pve"
	"github.com/abbott/hardn/pkg/host/reload"
	"github.com/abbott/hardn/pkg/host/revert"
	"github.com/abbott/hardn/pkg/host/sshd"
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
// output.StatusViewOf), the rest from the SSH Login control's reading
// (sshchange.Read: sshd -T, the files, the admins, the session's login).
// Since next/s6-control (unit (b)), each change is the real control
// (sshchange.Root, Auth, Port, Migrate) on the real engine, which refuses
// before asking in the decided words: lockout-risk, so typed and confirmed
// or reverted. Rows, layout and the screen's words stay here until unit
// (c). "uigallery ssh" lists the mocks; "play ssh" is the menu of the hosts,
// "play ssh/<host>" one; the main menu's 2 leads to debian13's.

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
	e := sshEngine(h, applySession, nil)
	p, err := e.Plan(ctx, []engine.Control{sshControl(steps.SSH{Prefix: "ssh.port", Port: 2230})}, config.DefaultConfig())
	if err == nil && len(p.Refused) == 0 {
		_, _ = e.Apply(ctx, p)
	}
	return h
}

// sshEngine is the engine over h, as the kit's: the session sess, the
// gallery's clock and hold.
func sshEngine(h *fakeHost, sess revert.Session, progress func(engine.Event)) *engine.Engine {
	return engine.New(h.system(), engine.Options{
		Revert:   revert.Options{Now: clock, Hardn: galleryHardn},
		Session:  sess,
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

// sshReading is the host as the screen reads it: the control's reading
// (sshchange.Read), the three SSH rows as the main menu draws them, and
// hardn.yml.
type sshReading struct {
	sshchange.Reading
	status component.StatusList
	cfg    *config.Config
	// getenv is hardn's environment, as the control's Plan looks it up
	// (sshchange.Root's and Auth's Getenv).
	getenv func(string) string
}

// readSSH reads the host as the screen does, writing nothing: the SSH rows
// from the real checks, then sshchange.Read with the session
// revert.ReadSession finds.
func readSSH(ctx context.Context, sys host.System, env map[string]string) (sshReading, error) {
	r := sshReading{getenv: func(k string) string { return env[k] }}
	cfg, err := statusConfig(ctx, sys)
	if err != nil {
		return r, err
	}
	r.cfg = cfg
	var f detect.Facts
	e := engine.New(sys, engine.Options{
		Detect: func(ctx context.Context, sys host.System) (detect.Facts, error) {
			var err error
			f, err = detect.Detect(ctx, sys)
			return f, err
		},
		Hold: sshQuiet.hold,
	})
	reports, err := e.Check(ctx, controls.Status(cfg))
	if err != nil {
		return r, err
	}
	view := output.StatusViewOf(reports, risk.Assess(reports, probe.Read(ctx, sys, f))).Status
	keep := []string{ssh.Login{}.Title(), ssh.Auth{}.Title(), ssh.Port{}.Title()}
	view.Rows = slices.DeleteFunc(view.Rows, func(row component.StatusRow) bool { return !slices.Contains(keep, row.Label) })
	r.status = view
	sess, _ := revert.ReadSession(ctx, sys, r.getenv, applyPID)
	r.Reading, err = sshchange.Read(ctx, sys, f, cfg, sess, r.getenv)
	return r, err
}

// session is the engine's session: applySession's process with the read
// session's connection over SSH, the read session itself on a console, so
// the control's Plan (engine.Input.Session) reads what the screen read.
func (r sshReading) session() revert.Session {
	if r.Session.Remote {
		return sshSession(r.Session.Conn)
	}
	return r.Session
}

// byNote is the deciding file as a row's note: "" for hardn's own.
func (r sshReading) byNote(k string) string {
	switch f := r.DecidedBy(k); f {
	case path.Base(sshd.HardnDropIn):
		return ""
	case "":
		return "sshd's default"
	default:
		return "by " + f
	}
}

// passwordMethods are the password methods sshd offers: password where
// PasswordAuthentication is yes, keyboard-interactive where it is yes and
// sshd uses PAM.
func (r sshReading) passwordMethods() map[string]bool {
	return map[string]bool{"password": r.Effective.PasswordAuthentication.Value == "yes",
		"keyboard-interactive": r.Effective.KbdInteractiveAuthentication.Value == "yes" && r.Effective.UsePAM.Value == "yes"}
}

// rows are the SSH Configuration section: what sshd runs with, from sshd
// -T and the files, never hardn.yml.
func (r sshReading) rows() []component.KeyValue {
	if r.EffErr != nil {
		return []component.KeyValue{{Key: sshRowRoot, Value: "Can't read", State: component.ValueUnknown},
			{Key: sshRowPass, Value: "Can't read", State: component.ValueUnknown},
			{Key: sshRowPort, Value: "Can't read", State: component.ValueUnknown}}
	}
	var rows []component.KeyValue
	switch v := r.RootValue(); {
	case v != "no" && r.RootLocked:
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
	case !r.PasswordsOn() && sshd.MethodsMet(nil, pm): // offered, but no list is met by them alone
		// "in <file>" does not fit the row at 80 columns: the file is the
		// Old Settings row's, or 00-hardn.conf.
		rows = append(rows, component.KeyValue{Key: sshRowPass, Value: "Off", Note: "by AuthenticationMethods"})
	case !r.PasswordsOn():
		rows = append(rows, component.KeyValue{Key: sshRowPass, Value: "Off", Note: r.byNote("passwordauthentication")})
	case sshd.MethodsMet(r.Methods(), map[string]bool{"password": pm["password"]}) || !pm["keyboard-interactive"]:
		rows = append(rows, component.KeyValue{Key: sshRowPass, Value: "On", State: component.ValueWarn, Note: r.byNote("passwordauthentication")})
	default:
		rows = append(rows, component.KeyValue{Key: sshRowPass, Value: "On, keyboard-interactive", State: component.ValueWarn, Note: r.byNote("kbdinteractiveauthentication")})
	}
	var ports []string
	for _, p := range r.Effective.Listeners.Ports() {
		ports = append(ports, strconv.Itoa(int(p)))
	}
	port := component.KeyValue{Key: sshRowPort, Value: strings.Join(ports, ", ")}
	if len(ports) == 0 {
		port.Value, port.State = "Can't read", component.ValueUnknown
	}
	if r.Facts.SSH.Mode == detect.SSHSocket {
		port.Note = "ssh.socket"
	}
	rows = append(rows, port)
	if us := r.Effective.AllowUsers.Values; len(us) > 0 {
		rows = append(rows, component.KeyValue{Key: sshRowUsers, Value: strings.Join(us, " "), Note: r.byNote("allowusers")})
	} else {
		rows = append(rows, component.KeyValue{Key: sshRowUsers, Value: "Anyone"})
	}
	ms := slices.Concat(r.MatchMore("permitrootlogin", r.RootValue()), r.MatchMore("passwordauthentication", r.Effective.PasswordAuthentication.Value))
	if r.Effective.UsePAM.Value == "yes" {
		ms = append(ms, r.MatchMore("kbdinteractiveauthentication", r.Effective.KbdInteractiveAuthentication.Value)...)
	}
	if len(ms) > 0 {
		rows = append(rows, component.KeyValue{Key: sshRowMatch, Value: "Lets more in", State: component.ValueWarn,
			Note: path.Base(ms[0].File)})
	}
	if len(r.Found.Artifacts) > 0 {
		var names []string
		for _, a := range r.Found.Artifacts {
			names = append(names, path.Base(a.Path))
		}
		rows = append(rows, component.KeyValue{Key: sshRowLegacy, Value: strings.Join(names, ", "), State: component.ValueWarn})
	}
	you := component.KeyValue{Key: sshRowYou, Value: "Not known", State: component.ValueUnknown}
	switch m := r.Login.Method; {
	case r.Login.User == "":
	case m == "publickey":
		you = component.KeyValue{Key: sshRowYou, Value: r.Login.User + ", by key"}
	default:
		you = component.KeyValue{Key: sshRowYou, Value: r.Login.User + ", by password", State: component.ValueWarn}
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
	case r.PVE:
		root.Label, root.Description = sshPVERoot, sshPVERootDesc
	case r.EffErr == nil && r.RootValue() == "no":
		root.Label, root.Description = sshKeyOnlyRoot, sshKeyRootDesc
	}
	pass := component.MenuItem{Key: "2", Label: sshKeyOnly, Description: sshKeyOnlyDesc}
	if r.EffErr == nil && !r.PasswordsOn() {
		pass.Label, pass.Description = sshPassOn, sshPassOnDesc
	}
	port := component.MenuItem{Key: "3", Label: sshPort, Description: "Now " + r.portWords()}
	items := []component.MenuItem{root, pass, port}
	if len(r.Found.Artifacts) > 0 {
		items = append(items, component.MenuItem{Key: "4", Label: sshMigrate, Description: sshMigrateDesc})
	}
	if r.EffErr != nil { // sshd -T unread: each item's result not known
		for i := range items {
			items[i].Description = "Can't read"
		}
	}
	return items
}

// portWords are the ports sshd listens on, "22" or "22 and 2230".
func (r sshReading) portWords() string {
	var ps []string
	for _, p := range r.Effective.Listeners.Ports() {
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

// The screen's own refusal words (gallery-ssh.md §6, decided); the control's
// refusals are pkg/engine/controls/sshchange's, in the same section's words.
const (
	sshWordsPending     = "A change is waiting for hardn confirm. Confirm it from a new connection, or let it roll back, then try again."
	sshWordsInUse       = "Port %d is in use. Pick another."
	sshWordsPortSame    = "sshd listens on %d already."
	sshWordsPortBad     = "Type a port from 1 to 65535."
	sshWordsNothingLeft = "Nothing to move."
)

// planWords are a refusal of the engine's plan in the screen's words: a
// change pending, a port in use; the control's refusals as they are
// (screenkit.RefusalWords).
func planWords(err error) string {
	switch {
	case errors.Is(err, revert.ErrPending):
		return sshWordsPending
	case errors.Is(err, reload.ErrInUse):
		return "A port the change adds is in use. Pick another."
	}
	return screenkit.RefusalWords(err)
}

// legacyNames are hardn's old files' names.
func (r sshReading) legacyNames() string {
	var names []string
	for _, a := range r.Found.Artifacts {
		names = append(names, path.Base(a.Path))
	}
	return strings.Join(names, ", ")
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
// refusal in the screen's words: the control's own and the engine's,
// before asking.
func (e *sshRunner) preplan(ctx context.Context, r sshReading, c engine.Control) string {
	p, err := sshEngine(e.h, r.session(), nil).Plan(ctx, []engine.Control{c}, r.cfg)
	switch {
	case err != nil:
		return planWords(err)
	case len(p.Refused) > 0:
		return planWords(p.Refused[0])
	}
	return ""
}

// change runs c through the flow: its review, its plan, the question;
// refused before asking where the engine refuses its plan (an item with a
// field, the port, refuses before its field itself).
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

// root is item 1: Disallow root login, or Allow root key-only; on Proxmox
// VE, Root key-only (sshchange.Root, which refuses).
func (e *sshRunner) root(ctx context.Context, r sshReading) error {
	want, label, question, value := "no", sshDisallow, "Disallow root login?", "Disallowed"
	switch {
	case r.PVE:
		want, label, question, value = pve.RootLogin, sshPVERoot, "Make root key-only?", "Key only"
	case r.EffErr == nil && r.RootValue() == "no":
		want, label, question, value = "prohibit-password", sshKeyOnlyRoot, "Allow root to log in by key?", "Key only"
	}
	review := flow.Review{Title: sshSection, Items: []component.KeyValue{{Key: sshRowRoot, Value: value}}}
	return e.change(ctx, r, label, question, review, nil, func() engine.Control {
		return sshchange.Root{Want: want, Getenv: r.getenv}
	})
}

// password is item 2: Key-only login, or Password login on (sshchange.Auth).
func (e *sshRunner) password(ctx context.Context, r sshReading) error {
	action, label, question, value := sshchange.AuthKeyOnly, sshKeyOnly, "Turn password login off?", "Off"
	if r.EffErr == nil && !r.PasswordsOn() {
		action, label, question, value = sshchange.AuthPasswordsOn, sshPassOn, "Turn password login on?", "On"
	}
	review := flow.Review{Title: sshSection, Items: []component.KeyValue{{Key: sshRowPass, Value: value}}}
	return e.change(ctx, r, label, question, review, nil, func() engine.Control {
		return sshchange.Auth{Action: action, Getenv: r.getenv}
	})
}

// port is item 3: refused before its field as sshchange.Port refuses (a
// plan for a port sshd has already, which plans nothing else), then the
// new port and its plan, 22 held until confirmed.
func (e *sshRunner) port(ctx context.Context, r sshReading) error {
	have := r.Effective.Listeners.Ports()
	now := uint16(22)
	if len(have) > 0 {
		now = have[0]
	}
	if words := e.preplan(ctx, r, sshchange.Port{To: now}); words != "" {
		return e.refuse(r, words)
	}
	var value string
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
		return sshchange.Port{To: uint16(n)}
	})
}

// migrate is item 4: hardn's old files moved into 00-hardn.conf
// (sshchange.Migrate).
func (e *sshRunner) migrate(ctx context.Context, r sshReading) error {
	review := flow.Review{Title: sshSection, Items: []component.KeyValue{{Key: sshRowLegacy, Value: r.legacyNames() + ", into 00-hardn.conf"}}}
	return e.change(ctx, r, sshMigrate, "Move hardn's old settings?", review, nil, func() engine.Control {
		return sshchange.Migrate{ConfigFile: r.cfg.SshConfigFile}
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
				Session:  r.session(),
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

// refusalOf is rf's words on its host, "" for none: the engine's plan of
// the item's control, which refuses before asking.
func refusalOf(ctx context.Context, rf sshRefusal) (string, error) {
	r, h, err := sshReadNamed(ctx, rf.host)
	if err != nil {
		return "", err
	}
	var c engine.Control
	switch rf.item {
	case "root":
		c = sshchange.Root{Want: "no", Getenv: r.getenv}
	case "password":
		c = sshchange.Auth{Action: sshchange.AuthKeyOnly, Getenv: r.getenv}
	case "migrate":
		c = sshchange.Migrate{ConfigFile: r.cfg.SshConfigFile}
	default:
		c = sshchange.Port{To: 2230}
	}
	return (&sshRunner{h: h}).preplan(ctx, r, c), nil
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
	"The lockout checks before asking are the real SSH Login control's (sshchange). Nothing on this machine changes: every command and file is the fake's."
