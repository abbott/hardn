package main

import (
	"context"
	"fmt"
	"io/fs"
	"slices"
	"strings"

	"github.com/abbott/hardn/pkg/config"
	"github.com/abbott/hardn/pkg/engine"
	usersctl "github.com/abbott/hardn/pkg/engine/controls/users"
	"github.com/abbott/hardn/pkg/engine/risk"
	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/accounts"
	"github.com/abbott/hardn/pkg/host/authkeys"
	"github.com/abbott/hardn/pkg/host/backup"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/revert"
	"github.com/abbott/hardn/pkg/host/sudoers"
	"github.com/abbott/hardn/pkg/output"
	"github.com/abbott/hardn/pkg/screens/screenkit"
	"github.com/abbott/hardn/pkg/ui/component"
	"github.com/abbott/hardn/pkg/ui/flow"
	"github.com/abbott/hardn/pkg/ui/form"
	"github.com/abbott/hardn/pkg/ui/nav"
	"github.com/abbott/hardn/pkg/ui/screen"
)

// The User Management mock (refactor plan slice 5, unit (a), next/s5-gallery;
// docs/engine/gallery-users.md): a proposal, on Figma's layouts (01-USER,
// USER-MANAGE_USER-*, USER-SSH_KEYS, USER-CREATE_USER), drawn from the real
// control on a simulated host (usershost.go): what it draws is users.Read's
// (the User row through the main menu's mapping), each refusal the
// control's (users.Reading's Refusal, CheckName, CheckNew, KeysRefusal,
// ParseKey) in its words (screenkit.RefusalWords), and each change
// users.Admin's plan on the real engine (since next/s5-control, unit (b)).
// What stays here until unit (c) builds the screen: the rows, the layout,
// the forms and their reviews, and the words the control does not say.
// "uigallery users" lists the mocks; "play users" is the menu of the hosts,
// "play users/<host>" one; the main menu's 1 leads to debian13's.

// usrPrefix names the mock's hosts: "users" and "users/<host>".
const usrPrefix = "users"

// usrMainHost is the host the main menu's 1 leads to.
const usrMainHost = usrPrefix + "/debian13"

// usrLinks lead from a menu mock's item to the mock on a host: the main
// menu's 1.
var usrLinks = map[string]string{mainMenuMock + "/1": usrMainHost}

// The screen's words (gallery-users.md §6, decided 2026-11-03). Figma's
// where it has them. The refusals' are the control's; the usrWords* here
// pin them in the tests (TestUsersWords).
const (
	usrTitle        = "User Management"
	usrManageTitle  = "Manage User"
	usrCreateTitle  = "Create User"
	usrKeysSection  = "SSH Keys"
	usrHeading      = "Select an option"
	usrPrompt       = "Enter your choice"
	usrNoOtherUser  = "No user besides root"
	usrKeyPassword  = "Password"
	usrKeyBy        = "Granted by"
	usrKeyKeys      = "SSH Keys"
	usrGrantLabel   = "Grant admin rights"
	usrRemoveLabel  = "Remove admin rights"
	usrRequireLabel = "Ask for password"
	usrStopLabel    = "Don't ask password"
	usrAddKeyLabel  = "Add SSH key"
	usrDelKeyLabel  = "Remove SSH key"

	usrWordsNoPassword = "%s has no password, so %s couldn't ask for one. Set one with passwd %s, then try again."
	usrWordsNotHardn   = "%s is an admin through %s, which hardn didn't write, so hardn can't remove it. Change it there."
	// gpasswd is on all three VMs, Alpine's /usr/bin/gpasswd too
	// (00-inspect-alpine323-vm.log, 2026-11-03).
	usrWordsGroup        = "%s is an admin through the %s group, which hardn leaves alone. Take %s out of it with gpasswd -d %s %s."
	usrWordsStill        = "%s would stay an admin through %s, which hardn didn't write. Change it there, then try again."
	usrWordsStillUnknown = "hardn can't tell whether %s would stay an admin: it can't read %s. Check it, then try again."
	usrWordsSession      = "You're running hardn through %s's %s. Removing it would leave no way to become root. Grant another user first."
	usrWordsLastAdmin    = "hardn can't tell who you're logged in as, and %s is the last admin. Grant another user first."
	usrWordsKeyFiles     = "sshd doesn't read ~/.ssh/authorized_keys here (AuthorizedKeysFile %s), so hardn can't change keys."
	usrWordsKeyFilesRead = "hardn can't read which files sshd takes keys from: %s"
	usrWordsStrict       = "sshd would ignore a key for %s: %s. With StrictModes it takes keys only where the home and ~/.ssh are the user's own and no one else can write to them."
	usrWordsLastKey      = "This is %s's last key, and you're logged in as %s. Add another key first."
	usrWordsNotRead      = "hardn can't read %s's rules: %s Check them with %s, then try again."
	usrWordsNoTool       = "sudo isn't installed, so hardn can't grant admin rights. Install sudo, then try again."
	usrWordsName         = "Use lower-case letters, digits, - and _, starting with a letter or _, up to 32."
	usrWordsTaken        = "%s already exists."
	usrWordsKey          = "Paste one OpenSSH public key, such as ssh-ed25519 AAAA… name."
	usrWordsDupKey       = "%s already has this key."
	usrWordsNewNoPW      = "A new user has no password, so %s couldn't ask for one."
	usrWordsLocked       = "sshd refuses %s's keys: the account is locked (!) and sshd doesn't use PAM."
	usrWordsNoKeyNotice  = "Without a key %s can't log in: there's no password."
	usrWordsEdited       = "%s holds a rule hardn didn't write. Change it there, then try again."
	// usrNoPassToAsk is a NOPASSWD grant's note for an account with no
	// password: a cost, as the main menu's "No password to become root"
	// is (decision 3), not a failure.
	usrNoPassToAsk = "No password to ask for"
	// usrEmpty is the Password row for an empty field (proposed, unit (b)).
	usrEmpty = "Empty"
)

// usrHost is one simulated host the mock runs on.
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

// usrPassword is a password's state as the rows draw it.
type usrPassword uint8

const (
	pwNotRead usrPassword = iota
	pwSet                 // a hash
	pwNone                // "*", or "!" where sshd uses PAM
	pwLocked              // "!" where sshd's PAM is not established as on: key login refused
	pwEmpty               // an empty field
)

// usrPW is u's password as the rows draw it.
func usrPW(u usersctl.User) usrPassword {
	switch {
	case u.Locked:
		return pwLocked
	case u.Password == accounts.PasswordSet:
		return pwSet
	case u.Password == accounts.PasswordNone:
		return pwNone
	case u.Password == accounts.PasswordEmpty:
		return pwEmpty
	}
	return pwNotRead
}

// usrBy is the rule that makes a user an admin as the Granted by row says
// it: "hardn", "group sudo", or the file.
func usrBy(r usersctl.Rule) string {
	switch r.Kind {
	case usersctl.RuleHardn:
		return "hardn"
	case usersctl.RuleGroup:
		return "group " + r.Group
	case usersctl.RuleFile:
		return r.File
	}
	return ""
}

// usrReading is the host as the screen reads it: the control's reading
// (users.Read), the User row as the main menu maps it, and hardn.yml.
type usrReading struct {
	usersctl.Reading
	f      detect.Facts
	status component.StatusList
	cfg    *config.Config
	env    map[string]string
}

// readUsers reads the host as the screen does, writing nothing.
func readUsers(ctx context.Context, sys host.System, env map[string]string) (usrReading, error) {
	f, err := detect.Detect(ctx, sys)
	if err != nil {
		return usrReading{}, err
	}
	r := usrReading{f: f, env: env}
	if r.cfg, err = statusConfig(ctx, sys); err != nil {
		return usrReading{}, err
	}
	r.Reading, err = usersctl.Read(ctx, sys, f, r.getenv)
	c := usersctl.Admin{}
	view := output.StatusViewOf([]engine.Report{{Control: c.ID(), Title: c.Title(), Status: r.Status, Err: r.StatusErr}}, risk.Level{}).Status
	view.Rows = slices.DeleteFunc(view.Rows, func(row component.StatusRow) bool { return row.Label != c.Title() })
	r.status = view
	return r, err
}

// getenv is hardn's environment on the host: env's.
func (r usrReading) getenv(k string) string { return r.env[k] }

// admin is the control planning a for u's user, with the host's
// environment.
func (r usrReading) admin(a usersctl.Admin) usersctl.Admin {
	a.Getenv = r.getenv
	return a
}

// user is the user named name.
func (r usrReading) user(name string) (usersctl.User, bool) {
	i := slices.IndexFunc(r.Users, func(u usersctl.User) bool { return u.Name == name })
	if i < 0 {
		return usersctl.User{}, false
	}
	return r.Users[i], true
}

// toolWord is the tool the screen names: doas where Choose picks it, else
// sudo.
func (r usrReading) toolWord() string {
	if r.Choice.Tool == sudoers.ToolDoas {
		return "doas"
	}
	return "sudo"
}

// rows are u's rows: Figma's Sudo (doas on Alpine) and SSH Keys, then who
// grants it and its password.
func (r usrReading) rows(u usersctl.User) []component.KeyValue {
	tool := component.KeyValue{Key: r.toolKey(), Value: "Disabled", State: component.ValueNone}
	switch u.Admin {
	case detect.Yes:
		tool.Value, tool.State = "Enabled", component.ValueOK
		if u.NoPassword {
			tool = nopassRow(tool.Key, usrPW(u))
		}
	case detect.Unknown:
		tool.Value, tool.State = "Can't read", component.ValueUnknown
	}
	rows := []component.KeyValue{tool}
	if by := usrBy(u.Decided); u.Admin == detect.Yes && by != "" {
		rows = append(rows, component.KeyValue{Key: usrKeyBy, Value: by})
	}
	keys := component.KeyValue{Key: usrKeyKeys, Value: "None", State: component.ValueWarn}
	switch n := len(u.Keys); {
	case n == 1:
		keys = component.KeyValue{Key: usrKeyKeys, Value: "1 key"}
	case n > 1:
		keys = component.KeyValue{Key: usrKeyKeys, Value: fmt.Sprintf("%d keys", n)}
	}
	pw := component.KeyValue{Key: usrKeyPassword, Value: "Can't read", State: component.ValueUnknown}
	switch usrPW(u) {
	case pwSet:
		pw = component.KeyValue{Key: usrKeyPassword, Value: "Set"}
	case pwNone:
		pw = component.KeyValue{Key: usrKeyPassword, Value: "None"}
	case pwLocked:
		pw = component.KeyValue{Key: usrKeyPassword, Value: "Locked,", State: component.ValueBad, Note: "key login refused"}
	case pwEmpty:
		pw = component.KeyValue{Key: usrKeyPassword, Value: usrEmpty, State: component.ValueBad}
	}
	return append(rows, keys, pw)
}

// nopassRow is the tool's row for a grant that asks no password: a
// failure where the account has a password to ask, a cost where it has
// none (decision 3).
func nopassRow(key string, pw usrPassword) component.KeyValue {
	if pw == pwNone || pw == pwLocked {
		return component.KeyValue{Key: key, Value: "Enabled,", State: component.ValueWarn, Note: usrNoPassToAsk}
	}
	return component.KeyValue{Key: key, Value: "Enabled,", State: component.ValueBad, Note: "No password"}
}

// toolKey is the tool row's key: Figma's Sudo, doas where Choose picks it.
func (r usrReading) toolKey() string {
	if r.toolWord() == "doas" {
		return "doas"
	}
	return "Sudo"
}

// usrExit is the sub-menus' Return.
var usrExit = component.MenuItem{Key: "0", Label: "Return"}

// mainMenu is 01-USER: the User row, a section per user, Manage and Create.
func (r usrReading) mainMenu() screen.SubMenu {
	var sections []screen.Section
	for _, u := range r.Users {
		sections = append(sections, screen.Section{Header: component.SectionHeader{Title: u.Name}, Rows: component.BulletList{Items: r.rows(u)}})
	}
	if len(sections) == 0 {
		sections = []screen.Section{{Header: component.SectionHeader{Title: "Users"}, Rows: component.BulletList{Items: []component.KeyValue{{Key: usrNoOtherUser, State: component.ValueNone}}}}}
	}
	// Manage User only where there is a user to manage; Create keeps 2.
	var items []component.MenuItem
	if len(r.Users) > 0 {
		items = append(items, component.MenuItem{Key: "1", Label: "Manage User", Description: "Configure " + r.toolWord() + ", SSH keys"})
	}
	items = append(items, component.MenuItem{Key: "2", Label: "Create User", Description: "Configure a new user"})
	return screen.SubMenu{Title: component.TitleBar{Title: usrTitle}, Status: r.status, Sections: sections, Heading: usrHeading,
		Menu: component.Menu{Items: items, Exit: usrExit}, Prompt: usrPrompt}
}

// selectMenu is USER-MANAGE_USER-SELECT_USER.
func (r usrReading) selectMenu() screen.SubMenu {
	var items []component.MenuItem
	for i, u := range r.Users {
		items = append(items, component.MenuItem{Key: fmt.Sprint(i + 1), Label: u.Name})
	}
	return screen.SubMenu{Title: component.TitleBar{Title: usrManageTitle, Parents: []string{usrTitle}}, Heading: "Select a user",
		Menu: component.Menu{Items: items, Exit: usrExit}, Prompt: usrPrompt}
}

// manageMenu is USER-MANAGE_USER-MANAGEMENT for u: its rows; admin rights,
// with them the password, then the keys, each named by its result.
func (r usrReading) manageMenu(u usersctl.User) screen.SubMenu {
	grant := component.MenuItem{Key: "1", Label: usrGrantLabel, Description: "Let " + u.Name + " become root"}
	if u.Admin == detect.Yes {
		grant = component.MenuItem{Key: "1", Label: usrRemoveLabel, Description: "Take them away"}
	}
	items := []component.MenuItem{grant}
	if u.Admin == detect.Yes {
		pw := component.MenuItem{Key: "2", Label: usrRequireLabel, Description: "For admin rights"}
		if !u.NoPassword {
			pw = component.MenuItem{Key: "2", Label: usrStopLabel, Description: "For admin rights"}
		}
		items = append(items, pw)
	}
	items = append(items, component.MenuItem{Key: "3", Label: "SSH Keys", Description: "Add or remove SSH keys"})
	return screen.SubMenu{Title: component.TitleBar{Title: usrManageTitle, Parents: []string{usrTitle}},
		Subject:  component.SubjectLine{Label: "Managing", Subject: u.Name},
		Sections: []screen.Section{{Header: component.SectionHeader{Title: "User Configuration"}, Rows: component.BulletList{Items: r.rows(u)}}},
		Heading:  usrHeading, Menu: component.Menu{Items: items, Exit: usrExit}, Prompt: usrPrompt}
}

// keysMenu is USER-SSH_KEYS for u: its keys by fingerprint; Add, and with
// any, Remove.
func (r usrReading) keysMenu(u usersctl.User) screen.SubMenu {
	var rows []component.KeyValue
	for _, k := range u.Keys {
		rows = append(rows, component.KeyValue{Key: k.Fingerprint, Value: k.Comment})
	}
	if len(rows) == 0 {
		rows = []component.KeyValue{{Key: "No SSH keys", State: component.ValueWarn}}
	}
	items := []component.MenuItem{{Key: "1", Label: usrAddKeyLabel}}
	if len(u.Keys) > 0 {
		items = append(items, component.MenuItem{Key: "2", Label: usrDelKeyLabel})
	}
	return screen.SubMenu{Title: component.TitleBar{Title: usrManageTitle, Parents: []string{usrTitle}},
		Subject:  component.SubjectLine{Label: "Managing SSH keys for", Subject: u.Name},
		Sections: []screen.Section{{Header: component.SectionHeader{Title: usrKeysSection}, Rows: component.BulletList{Items: rows}}},
		Heading:  usrHeading, Menu: component.Menu{Items: items, Exit: usrExit}, Prompt: usrPrompt}
}

// refusal is the control's refusal of a before asking anything
// (users.Reading.Refusal), in its words; "" for none.
func (r usrReading) refusal(ctx context.Context, sys host.System, a usersctl.Admin) string {
	return usrWords(r.Refusal(ctx, sys, r.admin(a)))
}

// usrWords is err in the screen's words (screenkit.RefusalWords), "" for
// nil.
func usrWords(err error) string {
	if err == nil {
		return ""
	}
	return screenkit.RefusalWords(err)
}

// usrRefuse is a form's refusal of err, nil for none.
func usrRefuse(err error) error {
	if err == nil {
		return nil
	}
	return nav.Refuse(screenkit.RefusalWords(err))
}

// usrRunner plays the mock on one host.
type usrRunner struct {
	s     *session
	uh    usrHost
	h     *fakeHost
	title string
}

// openUsers returns the action that opens what name names: the hosts' menu
// for usrPrefix alone, the mock on one host for "users/<host>".
func (s *session) openUsers(name string) func() error {
	if uh, ok := findUsrHost(strings.TrimPrefix(name, usrPrefix+"/")); ok && name != usrPrefix {
		return func() error { return s.usersScreen(context.Background(), uh, uh.host()) }
	}
	return func() error {
		all := usrHosts()
		vm := screen.SubMenu{Title: component.TitleBar{Title: "User Management hosts"}, Heading: "Select a host", Prompt: usrPrompt, Menu: component.Menu{Exit: usrExit}}
		menu := nav.Menu{Exit: vm.Menu.Exit}
		for i, uh := range all {
			it := component.MenuItem{Key: fmt.Sprint(i + 1), Label: uh.label, Description: uh.about}
			vm.Menu.Items = append(vm.Menu.Items, it)
			menu.Options = append(menu.Options, nav.Option{Item: it, Action: func() error { return s.usersScreen(context.Background(), uh, uh.host()) }})
		}
		return s.p.Run(func() nav.Screen { return vm.Screen(s.p.Ctx, menu) })
	}
}

// usersScreen runs the mock on h, uh's host, until the user goes back: its
// note above it, then each round the host read again.
func (s *session) usersScreen(ctx context.Context, uh usrHost, h *fakeHost) error {
	e := &usrRunner{s: s, uh: uh, h: h, title: usrPrefix + "/" + uh.name}
	if err := s.writeNote(e.title, uh.note); err != nil {
		return err
	}
	return s.p.Run(func() nav.Screen { return e.draw(ctx) })
}

// read reads the host as the screen does.
func (e *usrRunner) read(ctx context.Context) (usrReading, error) {
	return readUsers(ctx, e.h.sys(), e.uh.env)
}

// bind is vm's menu with actions by key.
func usrBind(vm screen.SubMenu, actions map[string]func() error) nav.Menu {
	menu := nav.Menu{Exit: vm.Menu.Exit}
	for _, it := range vm.Menu.Items {
		menu.Options = append(menu.Options, nav.Option{Item: it, Action: actions[it.Key]})
	}
	return menu
}

// failed is the screen when the host is not read: Return alone.
func (e *usrRunner) failed(title string) nav.Screen {
	return screen.SubMenu{Title: component.TitleBar{Title: title}, Menu: component.Menu{Exit: usrExit}, Prompt: usrPrompt}.Screen(e.s.p.Ctx, nav.Menu{Exit: usrExit})
}

// draw is 01-USER, its items bound.
func (e *usrRunner) draw(ctx context.Context) nav.Screen {
	r, err := e.read(ctx)
	if err != nil {
		return e.failed(usrTitle)
	}
	vm := r.mainMenu()
	return vm.Screen(e.s.p.Ctx, usrBind(vm, map[string]func() error{
		"1": func() error { return e.s.p.Run(func() nav.Screen { return e.drawSelect(ctx) }) },
		"2": func() error { return e.create(ctx, r) },
	}))
}

// drawSelect is the users to manage.
func (e *usrRunner) drawSelect(ctx context.Context) nav.Screen {
	r, err := e.read(ctx)
	if err != nil {
		return e.failed(usrManageTitle)
	}
	vm := r.selectMenu()
	actions := map[string]func() error{}
	for i, u := range r.Users {
		name := u.Name
		actions[fmt.Sprint(i+1)] = func() error { return e.s.p.Run(func() nav.Screen { return e.drawManage(ctx, name) }) }
	}
	return vm.Screen(e.s.p.Ctx, usrBind(vm, actions))
}

// drawManage is one user's screen.
func (e *usrRunner) drawManage(ctx context.Context, name string) nav.Screen {
	r, err := e.read(ctx)
	u, ok := r.user(name)
	if err != nil || !ok {
		return e.failed(usrManageTitle)
	}
	vm := r.manageMenu(u)
	return vm.Screen(e.s.p.Ctx, usrBind(vm, map[string]func() error{
		"1": func() error { return e.admin(ctx, r, u) },
		"2": func() error { return e.password(ctx, r, u) },
		"3": func() error { return e.s.p.Run(func() nav.Screen { return e.drawKeys(ctx, name) }) },
	}))
}

// drawKeys is one user's keys.
func (e *usrRunner) drawKeys(ctx context.Context, name string) nav.Screen {
	r, err := e.read(ctx)
	u, ok := r.user(name)
	if err != nil || !ok {
		return e.failed(usrManageTitle)
	}
	vm := r.keysMenu(u)
	return vm.Screen(e.s.p.Ctx, usrBind(vm, map[string]func() error{
		"1": func() error { return e.addKey(ctx, r, u) },
		"2": func() error { return e.removeKey(ctx, r, u) },
	}))
}

// refuse says words before asking anything.
func (e *usrRunner) refuse(r usrReading, words string) error {
	return e.kit(r).Say(screenkit.Warnings(words))()
}

// usrConfirm is the question: y/N, or the hostname typed for a lockout-risk
// plan, as Firewall's Enable asks it.
func usrConfirm(question string) func(flow.Plan) flow.Confirm {
	return func(p flow.Plan) flow.Confirm {
		if p.Typed() {
			return flow.Confirm{Question: question, Label: typeTheHostname, Phrase: hostName}
		}
		return flow.Confirm{Question: question}
	}
}

// change runs one change through the flow: its review, the control's plan
// of a, the question.
func (e *usrRunner) change(ctx context.Context, r usrReading, title, question string, review flow.Review, a usersctl.Admin) error {
	return e.kit(r).Change(ctx, screenkit.Change{
		Step:     screen.WorkflowStep{Title: component.TitleBar{Title: title, Parents: []string{usrTitle}}},
		Flow:     flow.Flow{Review: func() flow.Review { return review }, Confirm: usrConfirm(question)},
		Controls: func(*config.Config) []engine.Control { return []engine.Control{r.admin(a)} },
	})
}

// admin is Grant or Remove admin rights for u.
func (e *usrRunner) admin(ctx context.Context, r usrReading, u usersctl.User) error {
	if u.Admin == detect.Yes {
		a := usersctl.Admin{Action: usersctl.ActionRemove, User: u.Name}
		if words := r.refusal(ctx, e.h.sys(), a); words != "" {
			return e.refuse(r, words)
		}
		return e.change(ctx, r, usrRemoveLabel, "Remove "+u.Name+"'s admin rights?",
			flow.Review{Title: "User Configuration", Items: []component.KeyValue{{Key: r.toolKey(), Value: "Disabled"}}}, a)
	}
	a := usersctl.Admin{Action: usersctl.ActionGrant, User: u.Name}
	if words := r.refusal(ctx, e.h.sys(), a); words != "" {
		return e.refuse(r, words)
	}
	row := nopassRow(r.toolKey(), usrPW(u))
	if usrPW(u) == pwSet {
		row = component.KeyValue{Key: r.toolKey(), Value: "Enabled", State: component.ValueOK}
	}
	return e.change(ctx, r, usrGrantLabel, "Grant "+u.Name+" admin rights?", flow.Review{Title: "User Configuration", Items: []component.KeyValue{row}}, a)
}

// password is Ask or Don't ask the password for u's admin rights: hardn's
// own grant, written to win over the rules before it.
func (e *usrRunner) password(ctx context.Context, r usrReading, u usersctl.User) error {
	if u.Admin != detect.Yes {
		return nil
	}
	a := usersctl.Admin{Action: usersctl.ActionNoPassword, User: u.Name}
	label, question, row := usrStopLabel, "Stop asking "+u.Name+"'s password for admin rights?", nopassRow(r.toolKey(), usrPW(u))
	if u.NoPassword {
		a.Action = usersctl.ActionAskPassword
		label, question, row = usrRequireLabel, "Ask "+u.Name+"'s password for admin rights?", component.KeyValue{Key: r.toolKey(), Value: "Enabled", State: component.ValueOK}
	}
	if words := r.refusal(ctx, e.h.sys(), a); words != "" {
		return e.refuse(r, words)
	}
	return e.change(ctx, r, label, question, flow.Review{Title: "User Configuration", Items: []component.KeyValue{row}}, a)
}

// keyField is the form's key question, validated by the control
// (users.ParseKey); a key u has already warns.
func keyField(value *string, have []authkeys.Key, name string, skip func() bool) *form.Text {
	return &form.Text{Title: "Paste SSH public key", Value: value, Skip: skip, Validate: func(s string) error {
		k, err := usersctl.ParseKey(s)
		if err != nil {
			return usrRefuse(err)
		}
		if slices.ContainsFunc(have, k.Same) {
			return nav.Warn(fmt.Sprintf(usrWordsDupKey, name))
		}
		return nil
	}}
}

// addKey is Add SSH key: one key, then the control's plan.
func (e *usrRunner) addKey(ctx context.Context, r usrReading, u usersctl.User) error {
	if words := r.refusal(ctx, e.h.sys(), usersctl.Admin{Action: usersctl.ActionAddKey, User: u.Name}); words != "" {
		return e.refuse(r, words)
	}
	var line string
	review := flow.Review{Title: usrKeysSection, Items: []component.KeyValue{{Key: usrKeyKeys, Value: fmt.Sprintf("%d keys", len(u.Keys)+1)}}}
	if len(u.Keys) == 0 {
		review.Items[0].Value = "1 key"
	}
	if u.Locked {
		review.Notice = &flow.Notice{Callout: component.Callout{Kind: component.MessageWarn, Body: []string{fmt.Sprintf(usrWordsLocked, u.Name)}}}
	}
	return e.kit(r).Change(ctx, screenkit.Change{
		Step: screen.WorkflowStep{Title: component.TitleBar{Title: usrAddKeyLabel, Parents: []string{usrTitle}}},
		Flow: flow.Flow{
			Form:    form.Form{Fields: []form.Field{keyField(&line, u.Keys, u.Name, nil)}},
			Review:  func() flow.Review { return review },
			Confirm: usrConfirm("Add the key for " + u.Name + "?"),
		},
		Controls: func(*config.Config) []engine.Control {
			return []engine.Control{r.admin(usersctl.Admin{Action: usersctl.ActionAddKey, User: u.Name, Key: line})}
		},
	})
}

// removeKey is Remove SSH key: which, by fingerprint, then the control's
// plan, a lockout risk where it says.
func (e *usrRunner) removeKey(ctx context.Context, r usrReading, u usersctl.User) error {
	if len(u.Keys) == 0 {
		return nil
	}
	if words := r.refusal(ctx, e.h.sys(), usersctl.Admin{Action: usersctl.ActionRemoveKey, User: u.Name}); words != "" {
		return e.refuse(r, words)
	}
	gone := 0
	opts := make([]form.Option[int], len(u.Keys))
	for i, k := range u.Keys {
		opts[i] = form.Option[int]{Label: k.Fingerprint + " " + k.Comment, Value: i}
	}
	n := len(u.Keys) - 1
	review := flow.Review{Title: usrKeysSection, Items: []component.KeyValue{{Key: usrKeyKeys, Value: fmt.Sprintf("%d keys", n)}}}
	switch n {
	case 0:
		review.Items[0] = component.KeyValue{Key: usrKeyKeys, Value: "None", State: component.ValueWarn}
	case 1:
		review.Items[0].Value = "1 key"
	}
	return e.kit(r).Change(ctx, screenkit.Change{
		Step: screen.WorkflowStep{Title: component.TitleBar{Title: usrDelKeyLabel, Parents: []string{usrTitle}}},
		Flow: flow.Flow{
			Form:    form.Form{Fields: []form.Field{&form.Select[int]{Title: "Remove which key?", Options: opts, Value: &gone}}},
			Review:  func() flow.Review { return review },
			Confirm: usrConfirm("Remove the key from " + u.Name + "?"),
		},
		Controls: func(*config.Config) []engine.Control {
			return []engine.Control{r.admin(usersctl.Admin{Action: usersctl.ActionRemoveKey, User: u.Name, Fingerprint: u.Keys[gone].Fingerprint})}
		},
	})
}

// create is Create User: Figma's questions, the password one in Figma's
// polarity (P-11), refused at yes (decision 3), each answer checked by the
// control (CheckName, ToolRefusal, CheckNew, KeysRefusal, ParseKey); one
// plan for the account, the grant and the key, the control's.
func (e *usrRunner) create(ctx context.Context, r usrReading) error {
	sys := e.h.sys()
	var name, key string
	var admin, password, addKey bool
	fields := []form.Field{
		&form.Text{Title: "Username", Value: &name, Validate: func(s string) error { return usrRefuse(r.CheckName(ctx, sys, s)) }},
		&form.YesNo{Title: "Grant admin rights?", Value: &admin, Validate: func(yes bool) error {
			if yes {
				return usrRefuse(r.ToolRefusal())
			}
			return nil
		}},
		&form.YesNo{Title: "Require password for admin rights?", Value: &password, Skip: func() bool { return !admin }, Validate: func(yes bool) error {
			return usrRefuse(r.CheckNew(ctx, sys, usersctl.New{Name: name, Admin: admin, Password: yes}))
		}},
		&form.YesNo{Title: "Add SSH key?", Value: &addKey, Validate: func(yes bool) error {
			if yes {
				return usrRefuse(r.KeysRefusal())
			}
			return nil
		}},
		keyField(&key, nil, "", func() bool { return !addKey }),
	}
	return e.kit(r).Change(ctx, screenkit.Change{
		Step: screen.WorkflowStep{Title: component.TitleBar{Title: usrCreateTitle, Parents: []string{usrTitle}}},
		Flow: flow.Flow{
			Form: form.Form{Fields: fields},
			Review: func() flow.Review {
				sudoRow := component.KeyValue{Key: r.toolKey(), Value: "Disabled"}
				if admin {
					sudoRow = nopassRow(r.toolKey(), pwNone)
				}
				keyRow := component.KeyValue{Key: usrKeyKeys, Value: "None", State: component.ValueWarn}
				rv := flow.Review{Title: "User Configuration", Items: []component.KeyValue{sudoRow, keyRow, {Key: usrKeyPassword, Value: "None"}}}
				if addKey { // with none, the plan says the user can't log in (users.CodeNoKey)
					rv.Items[1] = component.KeyValue{Key: usrKeyKeys, Value: "1 key"}
				}
				return rv
			},
			Confirm: func(flow.Plan) flow.Confirm { return flow.Confirm{Question: fmt.Sprintf("Create user '%s'?", name)} },
		},
		Controls: func(*config.Config) []engine.Control {
			n := usersctl.New{Name: name, Admin: admin}
			if addKey {
				n.Key = key
			}
			return []engine.Control{r.admin(usersctl.Admin{Action: usersctl.ActionCreate, New: n})}
		},
	})
}

// usrSys is the host as the engine reaches it in the mock: engineSys, with
// the tools' setuid bit as the checks read it (statusSys), since the
// control's plan reads the host as Read does.
type usrSys struct {
	engineSys
	setuid []string
}

// Stat is the fake's, with the setuid bit on the tools.
func (s usrSys) Stat(ctx context.Context, name string) (fs.FileInfo, error) {
	return statusSys{System: s.engineSys.System, setuid: s.setuid}.Stat(ctx, name)
}

// system is the host as the mock's engine reaches it.
func (e *usrRunner) system() usrSys { return usrSys{engineSys: e.h.system(), setuid: e.h.sys().setuid} }

// kit is the mock's screenkit.Session: the gallery's engine over the host,
// its backups the defaults, the gallery's hold, its apply recorded for the
// end of play; a dry run when play plays the dry-run sequence.
func (e *usrRunner) kit(r usrReading) screenkit.Session {
	s, h := e.s, e.h
	var rec *recorder
	cfg := r.cfg
	if cfg == nil {
		cfg = &config.Config{}
	}
	return screenkit.Session{
		Name: "users mock", Prompter: s.p, System: e.system(), Config: cfg,
		DryRun: s.sequence == "dry-run", LogPath: output.LegacyLogPath, Now: clock,
		NewEngine: func(progress func(engine.Event)) *engine.Engine {
			rc := &recorder{host: h, f: h.f, title: e.title}
			rec = rc
			s.mu.Lock()
			s.engineRuns = append(s.engineRuns, rc)
			s.mu.Unlock()
			rc.begin()
			return engine.New(e.system(), engine.Options{
				Revert:   revert.Options{Now: clock, Hardn: galleryHardn},
				Session:  applySession,
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

// usrReadNamed reads the host named name, fresh.
func usrReadNamed(ctx context.Context, name string) (usrReading, *fakeHost, error) {
	uh, ok := findUsrHost(name)
	if !ok {
		return usrReading{}, nil, fmt.Errorf("no users host %q", name)
	}
	h := uh.host()
	r, err := readUsers(ctx, h.sys(), uh.env)
	return r, h, err
}

// usrOnHost returns a drawing of the host named name's reading.
func usrOnHost(name string, view func(usrReading, component.Ctx) string) func(context.Context, component.Ctx) (string, error) {
	return func(ctx context.Context, c component.Ctx) (string, error) {
		r, _, err := usrReadNamed(ctx, name)
		if err != nil {
			return "", err
		}
		return view(r, c), nil
	}
}

// usrMocks are the listing's mocks: the screen on each host; the select,
// manage and keys sub-screens; the refusals.
func usrMocks() []listMock {
	var mocks []listMock
	for _, uh := range usrHosts() {
		mocks = append(mocks, listMock{name: "screen-" + uh.name, note: "User Management. " + uh.note,
			draw: usrOnHost(uh.name, func(r usrReading, c component.Ctx) string { return r.mainMenu().Render(c) })})
	}
	return append(mocks,
		listMock{name: "select", note: "Manage User's Select a user, on debian13.",
			draw: usrOnHost("debian13", func(r usrReading, c component.Ctx) string { return r.selectMenu().Render(c) })},
		listMock{name: "manage", note: "Manage User for george on debian13: Remove admin rights, Require a password, SSH Keys.",
			draw: usrOnHost("debian13", func(r usrReading, c component.Ctx) string { u, _ := r.user("george"); return r.manageMenu(u).Render(c) })},
		listMock{name: "manage-alice", note: "Manage User for alice on debian13: in sudo by group, a password asked.",
			draw: usrOnHost("debian13", func(r usrReading, c component.Ctx) string { u, _ := r.user("alice"); return r.manageMenu(u).Render(c) })},
		listMock{name: "keys", note: "SSH Keys for george on debian13, by fingerprint.",
			draw: usrOnHost("debian13", func(r usrReading, c component.Ctx) string { u, _ := r.user("george"); return r.keysMenu(u).Render(c) })},
		listMock{name: "refusals", note: "The refusals before asking, each from the real reading: the item, the host, the user.", draw: usrRefusalsMock},
	)
}

// usrRefusal is one refusal the refusals mock draws, and the tests hold.
type usrRefusal struct{ item, host, user string }

// usrRefusals are the lockout paths and refusals, by item, host and user.
func usrRefusals() []usrRefusal {
	return []usrRefusal{
		{"remove", "hardn", "george"},            // the session user's own grant
		{"remove", "debian13", "george"},         // cloud-init's file
		{"remove", "debian13", "alice"},          // the sudo group
		{"remove", "legacy", "george"},           // origin/main's file
		{"remove", "alpine323", "george"},        // wheel
		{"remove", "not-read", "george"},         // rules not read
		{"require", "debian13", "george"},        // no password
		{"require", "alpine323", "george"},       // no password, doas
		{"remove-key", "one-key", "george"},      // the session user's last key
		{"grant", "no-sudo", "george"},           // no sudo
		{"grant", "not-read", "alice"},           // rules not read
		{"remove", "root-login", "george"},       // the last admin, the session user not known
		{"remove-key", "alpine-stray", "george"}, // the last key sshd reads
		{"add-key", "group-home", "alice"},       // StrictModes
	}
}

// usrItemActions are the refusals' items as the control's actions.
var usrItemActions = map[string]usersctl.Action{"grant": usersctl.ActionGrant, "remove": usersctl.ActionRemove, "require": usersctl.ActionAskPassword,
	"remove-key": usersctl.ActionRemoveKey, "add-key": usersctl.ActionAddKey}

// refusalOf is rf's words on its host, the control's, "" for none.
func refusalOf(ctx context.Context, rf usrRefusal) (string, error) {
	r, h, err := usrReadNamed(ctx, rf.host)
	if err != nil {
		return "", err
	}
	if _, ok := r.user(rf.user); !ok {
		return "", fmt.Errorf("%s: no user %s", rf.host, rf.user)
	}
	return r.refusal(ctx, h.sys(), usersctl.Admin{Action: usrItemActions[rf.item], User: rf.user}), nil
}

// usrRefusalsMock draws each refusal under a header naming it.
func usrRefusalsMock(ctx context.Context, c component.Ctx) (string, error) {
	var out []string
	for _, rf := range usrRefusals() {
		words, err := refusalOf(ctx, rf)
		if err != nil {
			return "", err
		}
		if words == "" {
			return "", fmt.Errorf("%v: no refusal", rf)
		}
		out = append(out, header(c, rf.item+", "+rf.host+", "+rf.user), screenkit.Warnings(words).Render(c))
	}
	return apart(out...), nil
}

// usrUsage is the listing's usage line.
const usrUsage = "usage: go run ./internal/tools/uigallery users [flags] [mock | all]..."

// usrListing is the User Management listing.
var usrListing = listing{prefix: usrPrefix, usage: usrUsage, label: "Mock:", mocks: usrMocks}

// usrStart returns the hosts' menu, or the host, that args name, and
// whether they name one; an error for a host that is not one.
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

// usrNote is play's note for the mock.
const usrNote = "Play mode, mocks: the User Management screen proposed for slice 5, drawn from the real control on a simulated host " +
	"(users.Read), each refusal the control's and each change users.Admin's plan on the real plan/apply engine. " +
	"Nothing on this machine changes: every command and file is the fake's."
