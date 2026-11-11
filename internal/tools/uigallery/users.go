package main

import (
	"context"
	"fmt"
	"io/fs"
	"slices"
	"strings"

	"github.com/abbott/hardn/pkg/engine"
	"github.com/abbott/hardn/pkg/host/backup"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/revert"
	"github.com/abbott/hardn/pkg/output"
	usrscreen "github.com/abbott/hardn/pkg/screens/users"
	"github.com/abbott/hardn/pkg/ui/component"
	"github.com/abbott/hardn/pkg/ui/nav"
	"github.com/abbott/hardn/pkg/ui/screen"
)

// The User Management screen on the simulated hosts (refactor plan slice 5,
// unit (c), next/s5-screen): hardn's User Management screen itself
// (pkg/screens/users), drawn from what the User control reads of each host
// (usershost.go), each of its changes on the real engine, on the hosts the
// User Management mock was drawn for, which it replaces
// (docs/engine/screen-users.md). The main menu's 1 leads to it on Debian 13;
// "play users" is the menu of the hosts and "play users/<host>" one. The
// listing of the mock, "uigallery users", was retired with it: git keeps it
// (users.go, next/s5-control), and gallery-users.md records what each showed.

// usrPrefix names the User Management screen's hosts in play: "users" is the
// menu of them and "users/<host>" one.
const usrPrefix = "users"

// usrMainHost is the host the main menu's 1 leads to.
const usrMainHost = usrPrefix + "/debian13"

// usrLinks lead from a menu mock's item to the User Management screen on a
// host, beside links: the main menu's 1.
var usrLinks = map[string]string{mainMenuMock + "/1": usrMainHost}

// usrHost is one simulated host the User Management screen runs on.
type usrHost struct {
	name, label, about, note string
	host                     func() *fakeHost
	// env is hardn's environment there: SUDO_USER or DOAS_USER, the
	// session user (sudoers.Invoker); none at a root login.
	env map[string]string
}

// usrHosts are the hosts, in the menu's order.
func usrHosts() []usrHost {
	const (
		deb = hostFQDN + " runs Debian 13. "
		alp = hostFQDN + " runs Alpine 3.23. "
	)
	viaSudo := map[string]string{"SUDO_USER": "george"}
	viaDoas := map[string]string{"DOAS_USER": "george"}
	return []usrHost{
		{name: "debian13", label: "Debian 13 cloud", about: "cloud-init's rule, a second user", host: usrDebian13, env: viaSudo,
			note: deb + "As the VM: george, uid 1000, no password (!), in sudo and adm, two keys, NOPASSWD by cloud-init's 90-cloud-init-users; alice, uid 1001, a password, in sudo, no key. hardn runs through george's sudo."},
		{name: "ubuntu2604", label: "Ubuntu 26.04", about: "sudo-rs, %admin too", host: usrUbuntu, env: viaSudo,
			note: hostFQDN + " runs Ubuntu 26.04 with sudo-rs 0.2.13, as the VM: debian13's accounts, /etc/sudoers with %admin as well as %sudo, HOME_MODE 0750."},
		{name: "alpine323", label: "Alpine 3.23", about: "doas through wheel", host: usrAlpine, env: viaDoas,
			note: alp + "As the VM: doas with wheel.conf's permit nopass :wheel; george, uid 1001, and alpine, uid 1000, in wheel, \"*\", /bin/ash; cloud-init's sudoers file written where no sudo reads it; sshd without PAM."},
		{name: "legacy", label: "origin/main's file", about: "/etc/sudoers.d/george", host: usrLegacy, env: viaSudo,
			note: deb + "george has a password and sudo by origin/main's /etc/sudoers.d/george (NOPASSWD, and its env_keep line), in no sudo group."},
		{name: "hardn", label: "hardn's grant", about: "hardn-grant-george", host: usrHardn, env: viaSudo,
			note: deb + "george, no password, has sudo by hardn's own hardn-grant-george alone (NOPASSWD), as sudoers.PlanFor writes it."},
		{name: "second-admin", label: "Second admin", about: "alice, a password", host: usrSecondAdmin, env: viaSudo,
			note: deb + "debian13, but alice has a password, one key and sudo by hardn-grant-alice (NOPASSWD), in no sudo group."},
		{name: "one-key", label: "One key, no keys", about: "george's last key", host: usrNoKeys, env: viaSudo,
			note: deb + "debian13, but george has one key: removing it is refused. alice has none, and no .ssh."},
		{name: "sudo-password", label: "Password asked", about: "george has one", host: usrSudoPassword, env: viaSudo,
			note: deb + "george has a password and sudo by hardn-grant-george asking for it."},
		{name: "alpine-no-rule", label: "Alpine, no rule", about: "george not in wheel", host: usrAlpineNoRule, env: viaDoas,
			note: alp + "george is in no doas rule: not in wheel, no file of hardn's. Grant writes hardn's doas file."},
		{name: "alpine-locked", label: "Alpine, locked", about: "alice's account is \"!\"", host: usrAlpineLocked, env: viaDoas,
			note: alp + "alpine323, with alice, uid 1002, in wheel, password field \"!\", one key: sshd without PAM refuses alice's key login [I: not run on the VM]."},
		{name: "yml-missing", label: "hardn.yml's user", about: "deploy isn't on the host", host: usrYMLMissing, env: viaSudo,
			note: deb + "debian13, with hardn.yml naming user deploy, sudoNoPassword and one key: no such user here, and the screen reads the host, not hardn.yml (decision 7)."},
		{name: "not-read", label: "Rules not read", about: "visudo -c rejects them", host: usrNotRead, env: viaSudo,
			note: deb + "debian13, but visudo -c rejects 90-cloud-init-users, so no rule is read: every change refuses."},
		{name: "root-login", label: "Root login", about: "no SUDO_USER", host: usrHardn,
			note: deb + "hardn's host, but hardn runs from a root login (su -, root's key): no SUDO_USER, so the session user isn't known. Removing george's grant is refused, as george is the last admin; every key removal is typed."},
		{name: "debian-nopam", label: "Debian, no PAM", about: "UsePAM no", host: usrDebianNoPAM, env: viaSudo,
			note: deb + "debian13, but sshd_config.d/50-usepam.conf sets UsePAM no: george's \"!\" refuses george's key login, and Create plans --password '*'."},
		{name: "alpine-stray", label: "Alpine, stray key", about: "authorized_keys2 unread", host: usrAlpineStray, env: viaDoas,
			note: alp + "alpine323, but george has one key in authorized_keys and one in authorized_keys2, which this sshd doesn't read (AuthorizedKeysFile .ssh/authorized_keys, as the VM): one key counted, and removing it is refused."},
		{name: "group-home", label: "Group-writable home", about: "alice's home 0775", host: usrGroupHome, env: viaSudo,
			note: deb + "debian13, but alice's home is 0775: sshd's StrictModes would ignore alice's keys, so Add SSH key refuses."},
		{name: "pve9", label: "Proxmox VE 9", about: "root alone, no sudo", host: usrPVE,
			note: hostFQDN + " is a Proxmox VE 9 node: root alone, by key (brief 2.5), no sudo. hardn runs as root."},
		{name: "no-sudo", label: "No sudo", about: "Debian without sudo", host: usrNoSudo, env: viaSudo,
			note: deb + "sudo isn't installed; george has two keys, no password, no elevation. Grant refuses."},
	}
}

// findUsrHost returns the host named name, after usrPrefix.
func findUsrHost(name string) (usrHost, bool) {
	all := usrHosts()
	i := slices.IndexFunc(all, func(h usrHost) bool { return h.name == name })
	if i < 0 {
		return usrHost{}, false
	}
	return all[i], true
}

// getenv is hardn's environment on the host: env's.
func (uh usrHost) getenv(k string) string { return uh.env[k] }

// usrHostMenu is the menu of the User Management screen's hosts: the
// sub-menu layout, an item per host.
func usrHostMenu() screen.SubMenu {
	var items []component.MenuItem
	for i, uh := range usrHosts() {
		items = append(items, component.MenuItem{Key: fmt.Sprint(i + 1), Label: uh.label, Description: uh.about})
	}
	return screen.SubMenu{
		Title:   component.TitleBar{Title: "User Management hosts"},
		Heading: "Select an option",
		Menu:    component.Menu{Items: items, Exit: component.MenuItem{Key: "0", Label: "Return"}},
		Prompt:  "Option",
	}
}

// openUsers returns the action that opens what name names: the hosts' menu
// for usrPrefix alone, the screen on one host for "users/<host>".
func (s *session) openUsers(name string) func() error {
	if uh, ok := findUsrHost(strings.TrimPrefix(name, usrPrefix+"/")); ok && name != usrPrefix {
		return func() error { return s.usersScreen(context.Background(), uh, uh.host()) }
	}
	return func() error {
		vm, all := usrHostMenu(), usrHosts()
		menu := nav.Menu{Exit: vm.Menu.Exit}
		for i, it := range vm.Menu.Items {
			uh := all[i]
			menu.Options = append(menu.Options, nav.Option{Item: it, Action: func() error { return s.usersScreen(context.Background(), uh, uh.host()) }})
		}
		return s.p.Run(func() nav.Screen { return vm.Screen(s.p.Ctx, menu) })
	}
}

// usersScreen runs the User Management screen on h, uh's host, until the
// user goes back from it: its note above it; the facts detect's, as the
// engine establishes them on the host; hardn's environment the host's
// (usrHost.env); hardn.yml the host's, or the defaults where it has none,
// as hardn's main menu holds them (statusConfig); the change's engine the
// gallery's, over the host as the engine reaches it with the tools' setuid
// bit (usrSys), its backups the defaults, with the gallery's hold, its apply
// recorded for the end of play. A dry run when play plays the dry-run
// sequence.
func (s *session) usersScreen(ctx context.Context, uh usrHost, h *fakeHost) error {
	sys, title := usrSystem(h), usrPrefix+"/"+uh.name
	facts, err := detect.Detect(ctx, sys)
	if err != nil {
		return fmt.Errorf("%s: %w", title, err)
	}
	cfg, err := statusConfig(ctx, sys)
	if err != nil {
		return fmt.Errorf("%s: %w", title, err)
	}
	if err := s.writeNote(title, uh.note); err != nil {
		return err
	}
	var rec *recorder
	return usrscreen.Run(ctx, usrscreen.Options{
		Prompter: s.p, System: sys, Facts: facts, Getenv: uh.getenv, Config: cfg, DryRun: s.sequence == "dry-run",
		LogPath: output.LegacyLogPath, Now: clock,
		NewEngine: func(progress func(engine.Event)) *engine.Engine {
			r := &recorder{host: h, f: h.f, title: title}
			rec = r
			s.mu.Lock()
			s.engineRuns = append(s.engineRuns, r)
			s.mu.Unlock()
			r.begin()
			return engine.New(sys, engine.Options{
				Revert:   revert.Options{Now: clock, Hardn: galleryHardn},
				Session:  applySession,
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
		},
	})
}

// usrSys is a users host as the screen reads it and the engine reaches it:
// engineSys, with the tools' setuid bit as the checks read it (statusSys),
// since the control's plan reads the host as Read does.
type usrSys struct {
	engineSys
	setuid []string
}

// Stat is the fake's, with the setuid bit on the tools.
func (s usrSys) Stat(ctx context.Context, name string) (fs.FileInfo, error) {
	return statusSys{System: s.engineSys.System, setuid: s.setuid}.Stat(ctx, name)
}

// usrSystem is h as the screen and its engine reach it.
func usrSystem(h *fakeHost) usrSys { return usrSys{engineSys: h.system(), setuid: h.sys().setuid} }

// usrStart returns the hosts' menu, or the host, that args name ("users",
// "users/<host>"), and whether they name one; an error for a host that is
// not one.
func usrStart(args []string) (string, bool, error) {
	if len(args) != 1 || (args[0] != usrPrefix && !strings.HasPrefix(args[0], usrPrefix+"/")) {
		return "", false, nil
	}
	if name := strings.TrimPrefix(args[0], usrPrefix+"/"); args[0] != usrPrefix {
		if _, ok := findUsrHost(name); !ok {
			var names []string
			for _, h := range usrHosts() {
				names = append(names, usrPrefix+"/"+h.name)
			}
			return "", false, fmt.Errorf("no users host %q; the hosts are %s", args[0], strings.Join(names, ", "))
		}
	}
	return args[0], true, nil
}
