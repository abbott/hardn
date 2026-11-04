package main

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/abbott/hardn/pkg/config"
	"github.com/abbott/hardn/pkg/engine"
	"github.com/abbott/hardn/pkg/engine/steps"
	"github.com/abbott/hardn/pkg/host/backup"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/revert"
	"github.com/abbott/hardn/pkg/host/sshd"
	"github.com/abbott/hardn/pkg/output"
	sshscreen "github.com/abbott/hardn/pkg/screens/ssh"
	"github.com/abbott/hardn/pkg/ui/component"
	"github.com/abbott/hardn/pkg/ui/nav"
	"github.com/abbott/hardn/pkg/ui/screen"
)

// The SSH Login screen on the simulated hosts (refactor plan slice 6, unit
// (c), next/s6-screen): hardn's SSH Login screen itself (pkg/screens/ssh),
// drawn from what the SSH Login control and the SSH checks read of each host
// (sshhost.go), each of its changes on the real engine, on the hosts the SSH
// Login mock was drawn for, which it replaces (docs/engine/screen-ssh.md).
// The main menu's 2 leads to it on Debian 13; "play ssh" is the menu of the
// hosts and "play ssh/<host>" one. The listing of the mock, "uigallery ssh",
// was retired with it: git keeps it (ssh.go, next/s6-control), and
// gallery-ssh.md records what each showed.

// sshPrefix names the SSH Login screen's hosts in play: "ssh" is the menu of
// them and "ssh/<host>" one.
const sshPrefix = "ssh"

// sshMainHost is the host the main menu's 2 leads to.
const sshMainHost = sshPrefix + "/debian13"

// sshLinks lead from a menu mock's item to the SSH Login screen on a host,
// beside links: the main menu's 2.
var sshLinks = map[string]string{mainMenuMock + "/2": sshMainHost}

// sshHost is one simulated host the SSH Login screen runs on.
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
			note: alp + "alpine323, but george's account is \"!\" and george is the only admin: sshd without PAM refuses george's key [I: not run], so george has no SSH login. hardn runs as root on the console."},
		{name: "rsa-small", label: "RSA key too short", about: "1024 bits, 2048 asked", host: sshRSASmall, env: sshConsoleEnv,
			note: deb + "debian13, but george's only key is RSA 1024 bits and 10-rsa.conf sets RequiredRSASize 2048, so sshd refuses it. hardn runs as root on the console."},
		{name: "admin-no-sudo", label: "Admin without sudo", about: "sudo asks a password", host: sshAdminNoSudo, env: sshConsoleEnv,
			note: deb + "george alone, by key; george's only grant is %sudo, which asks a password, and george's account is \"!\". hardn runs as root on the console."},
	}
}

// sshConsoleEnv is the environment of hardn run as root on the console: no
// SSH_CONNECTION, no SUDO_USER or DOAS_USER.
var sshConsoleEnv = map[string]string{}

// sshVariants are hosts play opens by name ("ssh/<host>") that the menu
// does not list: debian13 and pve9 with one thing changed, the refusals'
// hosts of gallery-ssh.md §6.
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

// sshQuiet is the session whose hold the engines outside play take (the
// tests' plans, confirm-window's apply): the gallery's hold, which holds
// nothing.
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

// getenv is hardn's environment on the host: env's.
func (sh sshHost) getenv(k string) string { return sh.env[k] }

// sshHostMenu is the menu of the SSH Login screen's hosts: the sub-menu
// layout, an item per host the menu lists.
func sshHostMenu() screen.SubMenu {
	var items []component.MenuItem
	for i, sh := range sshHosts() {
		items = append(items, component.MenuItem{Key: fmt.Sprint(i + 1), Label: sh.label, Description: sh.about})
	}
	return screen.SubMenu{
		Title:   component.TitleBar{Title: "SSH Login hosts"},
		Heading: "Select a host",
		Menu:    component.Menu{Items: items, Exit: component.MenuItem{Key: "0", Label: "Return"}},
		Prompt:  "Enter your choice",
	}
}

// openSSH returns the action that opens what name names: the hosts' menu
// for sshPrefix alone, the screen on one host for "ssh/<host>".
func (s *session) openSSH(name string) func() error {
	if sh, ok := findSSHHost(strings.TrimPrefix(name, sshPrefix+"/")); ok && name != sshPrefix {
		return func() error { return s.sshScreen(context.Background(), sh, sh.host()) }
	}
	return func() error {
		vm, all := sshHostMenu(), sshHosts()
		menu := nav.Menu{Exit: vm.Menu.Exit}
		for i, it := range vm.Menu.Items {
			sh := all[i]
			menu.Options = append(menu.Options, nav.Option{Item: it, Action: func() error { return s.sshScreen(context.Background(), sh, sh.host()) }})
		}
		return s.p.Run(func() nav.Screen { return vm.Screen(s.p.Ctx, menu) })
	}
}

// sshScreen runs the SSH Login screen on h, sh's host, until the user goes
// back from it: its note above it; the facts detect's, as the engine
// establishes them on the host; the session the one revert.ReadSession finds
// there; hardn's environment the host's (sshHost.env); hardn.yml the host's,
// or the defaults where it has none, as hardn's main menu holds them
// (statusConfig); the change's engine the gallery's over the host, on the
// applying session (sshEngineSession), its backups the defaults, with the
// gallery's hold, its apply recorded for the end of play: a change's
// engine alone, kept once its flow has ended (Ran), not the one an item's
// plan before asking makes, which Ran never ends. A dry run when play plays
// the dry-run sequence.
func (s *session) sshScreen(ctx context.Context, sh sshHost, h *fakeHost) error {
	sys, title := h.sys(), sshPrefix+"/"+sh.name
	facts, err := detect.Detect(ctx, sys)
	if err != nil {
		return fmt.Errorf("%s: %w", title, err)
	}
	cfg, err := statusConfig(ctx, sys)
	if err != nil {
		return fmt.Errorf("%s: %w", title, err)
	}
	sess, _ := revert.ReadSession(ctx, sys, sh.getenv, applyPID)
	if err := s.writeNote(title, sh.note); err != nil {
		return err
	}
	var rec *recorder
	return sshscreen.Run(ctx, sshscreen.Options{
		Prompter: s.p, System: sys, Facts: facts, Session: sess, Getenv: sh.getenv, Config: cfg, DryRun: s.sequence == "dry-run",
		LogPath: output.LegacyLogPath, Now: clock,
		NewEngine: func(progress func(engine.Event)) *engine.Engine {
			r := &recorder{host: h, f: h.f, title: title}
			rec = r
			r.begin()
			return engine.New(h.system(), engine.Options{
				Revert:   revert.Options{Now: clock, Hardn: galleryHardn},
				Session:  sshEngineSession(sess),
				Backups:  backup.Settings{Path: backup.DefaultPath, Enabled: true},
				Hold:     s.hold,
				PID:      applyPID,
				Progress: func(ev engine.Event) { r.event(ev); progress(ev) },
			})
		},
		Ran: func(run output.Run) {
			rec.end()
			rec.setPlan(run.Plan)
			rec.failedWith(run.Err)
			s.mu.Lock()
			s.engineRuns = append(s.engineRuns, rec)
			s.mu.Unlock()
		},
	})
}

// sshEngineSession is the engine's session for the session sess the screen
// reads: applySession's process with sess's connection over SSH, sess
// itself on a console, so the control's Plan (engine.Input.Session) reads
// what the screen read.
func sshEngineSession(sess revert.Session) revert.Session {
	if sess.Remote {
		return sshSession(sess.Conn)
	}
	return sess
}

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
