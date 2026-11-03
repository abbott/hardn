package main

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"regexp"
	"slices"
	"strings"

	"github.com/abbott/hardn/pkg/config"
	"github.com/abbott/hardn/pkg/engine"
	"github.com/abbott/hardn/pkg/engine/controls/ssh"
	usersctl "github.com/abbott/hardn/pkg/engine/controls/users"
	"github.com/abbott/hardn/pkg/engine/risk"
	"github.com/abbott/hardn/pkg/engine/steps"
	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/accounts"
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
// USER-MANAGE_USER-*, USER-SSH_KEYS, USER-CREATE_USER), drawn from what real
// code reads of a simulated host (usershost.go): the User row from
// users.Admin.Check through the main menu's mapping, each user's elevation
// from users.ReadElevation and the check's evidence, hardn's own grant from
// sudoers.Read and PolicyOf, the session user from sudoers.Invoker, sshd's
// PAM from ssh.ReadPAM. Each change runs the real engine: a grant through
// sudoers.PlanFor and steps.Sudoers; creating a user and the keys are the
// gallery's stand-ins (usrCreate, usrKeysChange), unit (b)'s to build, their
// commands shadow's useradd and userdel and coreutils' install. "uigallery
// users" lists the mocks; "play users" is the menu of the hosts, "play
// users/<host>" one; the main menu's 1 leads to debian13's.

// usrPrefix names the mock's hosts: "users" and "users/<host>".
const usrPrefix = "users"

// usrMainHost is the host the main menu's 1 leads to.
const usrMainHost = usrPrefix + "/debian13"

// usrLinks lead from a menu mock's item to the mock on a host: the main
// menu's 1.
var usrLinks = map[string]string{mainMenuMock + "/1": usrMainHost}

// The screen's words (gallery-users.md §6, proposed). Figma's where it has
// them.
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

	usrWordsNoPassword  = "%s has no password, so %s couldn't ask for one. Set one with passwd %s, then try again."
	usrWordsNotHardn    = "%s is an admin through %s, which hardn didn't write, so hardn can't remove it. Change it there."
	usrWordsGroup       = "%s is an admin through the %s group, which hardn leaves alone. Take %s out of it with gpasswd -d %s %s."
	usrWordsStill       = "%s would stay an admin through %s, which hardn didn't write. Change it there, then try again."
	usrWordsSession     = "You're running hardn through %s's %s. Removing it would leave no way to become root. Grant another user first."
	usrWordsLastKey     = "This is %s's last key, and you're logged in as %s. Add another key first."
	usrWordsNotRead     = "hardn can't read %s's rules: %s Check them with %s, then try again."
	usrWordsNoTool      = "sudo isn't installed, so hardn can't grant admin rights. Install sudo, then try again."
	usrWordsName        = "Use lower-case letters, digits, - and _, starting with a letter or _, up to 32."
	usrWordsTaken       = "%s already exists."
	usrWordsKey         = "Paste one OpenSSH public key, such as ssh-ed25519 AAAA… name."
	usrWordsDupKey      = "%s already has this key."
	usrWordsNewNoPW     = "A new user has no password, so %s couldn't ask for one."
	usrWordsLocked      = "sshd refuses %s's keys: the account is locked (!) and sshd doesn't use PAM."
	usrWordsNoKeyNotice = "Without a key %s can't log in: there's no password."
	usrWordsNoUser      = "There's no user besides root to manage. Create one first."
	usrWordsEdited      = "%s holds a rule hardn didn't write. Change it there, then try again."
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
			note: alp + "alpine323, with alice, uid 1002, in wheel, her password field \"!\", one key: sshd without PAM refuses her key login [I: not run on the VM]."},
		{name: "yml-missing", label: "hardn.yml's user", about: "deploy isn't on the host", host: usrYMLMissing, env: viaSudo,
			note: deb + "debian13, with hardn.yml naming user deploy, sudoNoPassword and one key: no such user here, and the screen reads the host, not hardn.yml (decision 7)."},
		{name: "not-read", label: "Rules not read", about: "visudo -c rejects them", host: usrNotRead, env: viaSudo,
			note: deb + "debian13, but visudo -c rejects 90-cloud-init-users, so no rule is read: every change refuses."},
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

// usrKey is one line of a user's authorized_keys.
type usrKey struct {
	kind, fp, comment, line string
}

// usrKeyKinds are the key types OpenSSH 9 and 10 take in authorized_keys
// (sshd(8), AUTHORIZED_KEYS FILE FORMAT), DSA gone.
var usrKeyKinds = []string{"ssh-ed25519", "ssh-rsa", "ecdsa-sha2-nistp256", "ecdsa-sha2-nistp384", "ecdsa-sha2-nistp521",
	"sk-ssh-ed25519@openssh.com", "sk-ecdsa-sha2-nistp256@openssh.com"}

// parseKey is a stand-in, unit (b)'s: one OpenSSH public key, its type
// known, its base64 a blob whose first string names the same type, as
// ssh-keygen -l reads one; with options, where a line has them, read past.
func parseKey(line string, options bool) (usrKey, error) {
	f := strings.Fields(line)
	if options && len(f) > 1 && !slices.Contains(usrKeyKinds, f[0]) {
		f = f[1:]
	}
	if len(f) < 2 || !slices.Contains(usrKeyKinds, f[0]) {
		return usrKey{}, errors.New("not a key")
	}
	blob, err := base64.StdEncoding.DecodeString(f[1])
	if err != nil || len(blob) < 4 {
		return usrKey{}, errors.New("not a key")
	}
	n := binary.BigEndian.Uint32(blob)
	if uint64(n)+4 > uint64(len(blob)) || string(blob[4:4+n]) != f[0] {
		return usrKey{}, errors.New("not a key")
	}
	return usrKey{kind: f[0], fp: fingerprint(blob), comment: strings.Join(f[2:], " "), line: strings.TrimSpace(line)}, nil
}

// usrPassword is a password field's state, from its first character.
type usrPassword uint8

const (
	pwNotRead usrPassword = iota
	pwSet                 // a hash
	pwNone                // "*", or "!" where sshd uses PAM
	pwLocked              // "!" where sshd doesn't use PAM: key login refused
)

// usrUser is one user as the screen reads it.
type usrUser struct {
	accounts.User
	admin  detect.Tri
	tool   string // the tool that grants it
	by     string // the rule that decides: "hardn", a file's name, "group sudo"
	nopass bool
	pw     usrPassword
	keys   []usrKey
	keyDir bool // ~/.ssh exists
	policy sudoers.Policy
	polErr error
}

// usrReading is the host as the screen reads it.
type usrReading struct {
	f       detect.Facts
	status  component.StatusList
	choice  sudoers.Choice
	notRead []usersctl.NotRead
	pam     detect.Tri
	invoker string
	users   []usrUser
	cfg     *config.Config
}

// usrUIDMax is login.defs' UID_MAX as the VMs have it, not read (a stand-in).
const usrUIDMax = 60000

// readUsers reads the host as the screen does, writing nothing.
func readUsers(ctx context.Context, sys host.System, env map[string]string) (usrReading, error) {
	f, err := detect.Detect(ctx, sys)
	if err != nil {
		return usrReading{}, err
	}
	r := usrReading{f: f, choice: sudoers.Choose(f, sudoers.ToolUnknown)}
	c := usersctl.Admin{}
	st, cerr := c.Check(ctx, sys, f)
	view := output.StatusViewOf([]engine.Report{{Control: c.ID(), Title: c.Title(), Status: st, Err: cerr}}, risk.Level{}).Status
	view.Rows = slices.DeleteFunc(view.Rows, func(row component.StatusRow) bool { return row.Label != c.Title() })
	r.status = view
	if r.cfg, err = statusConfig(ctx, sys); err != nil {
		return usrReading{}, err
	}
	db, err := accounts.Read(ctx, sys)
	if err != nil {
		return r, err
	}
	elev := usersctl.ReadElevation(ctx, sys, f)
	r.notRead = elev.NotRead()
	if p, err := ssh.ReadPAM(ctx, sys, f); err == nil {
		r.pam = p.Uses
	}
	if t := r.choice.Tool; t == sudoers.ToolSudo || t == sudoers.ToolDoas {
		r.invoker, _ = sudoers.Invoker(t, func(k string) string { return env[k] })
	}
	pws := readPasswords(ctx, sys)
	var names []string
	seen := map[string]bool{}
	for _, u := range db.Users {
		if seen[u.Name] || u.UID < usersctl.MinUID || u.UID > usrUIDMax {
			continue
		}
		seen[u.Name] = true
		uu := usrUser{User: u, admin: detect.No}
		for _, g := range elev.GrantsOf(db, u) {
			if g.Grants == detect.Yes || (g.Grants == detect.Unknown && uu.admin == detect.No) {
				uu.admin, uu.tool = g.Grants, g.Tool
			}
		}
		var text string
		uu.by, text = usrDecidedBy(st.Evidence, u.Name)
		uu.nopass = slices.ContainsFunc(st.Findings, func(fd engine.Finding) bool {
			return fd.Code == usersctl.CodeNoPassword && strings.HasPrefix(fd.Detail, u.Name+": ")
		}) || strings.Contains(text, "NOPASSWD") || slices.Contains(strings.Fields(text), "nopass")
		switch p := pws[u.Name]; {
		case p == "":
		case p[0] == '$':
			uu.pw = pwSet
		case p[0] == '!' && r.pam == detect.No:
			uu.pw = pwLocked
		default:
			uu.pw = pwNone
		}
		uu.keys, uu.keyDir = readKeys(ctx, sys, u.Home)
		r.users = append(r.users, uu)
		if sudoers.CheckUser(r.choice.Tool, u.Name) == nil {
			names = append(names, u.Name)
		}
	}
	if t := r.choice.Tool; (t == sudoers.ToolSudo || t == sudoers.ToolDoas) && len(r.notRead) == 0 {
		sst, err := sudoers.Read(ctx, sys, r.choice, names)
		for i := range r.users {
			if err != nil {
				r.users[i].polErr = err
				continue
			}
			r.users[i].policy, r.users[i].polErr = sudoers.PolicyOf(sst, r.users[i].Name)
		}
	}
	return r, nil
}

// readPasswords is a stand-in, unit (b)'s: each name's password field's
// first character in /etc/shadow, which accounts.ReadShadow reads but its
// Lock does not tell apart ("*" and a hash are both NotLocked).
func readPasswords(ctx context.Context, sys host.System) map[string]string {
	out := map[string]string{}
	data, err := sys.ReadFile(ctx, accounts.ShadowFile)
	if err != nil {
		return out
	}
	for _, l := range strings.Split(string(data), "\n") {
		if f := strings.SplitN(l, ":", 3); len(f) == 3 && f[1] != "" {
			if _, ok := out[f[0]]; !ok {
				out[f[0]] = f[1][:1]
			}
		}
	}
	return out
}

// usrKeyFiles are AuthorizedKeysFile as the VMs' sshd -T prints it; a
// stand-in, as sshd.ReadEffective does not expose it (gallery-users.md §7).
var usrKeyFiles = []string{".ssh/authorized_keys", ".ssh/authorized_keys2"}

// readKeys reads the keys in home's authorized key files, and whether
// ~/.ssh exists.
func readKeys(ctx context.Context, sys host.System, home string) ([]usrKey, bool) {
	var keys []usrKey
	for _, name := range usrKeyFiles {
		data, err := sys.ReadFile(ctx, path.Join(home, name))
		if err != nil {
			continue
		}
		for _, l := range strings.Split(string(data), "\n") {
			if k, err := parseKey(l, true); err == nil {
				keys = append(keys, k)
			}
		}
	}
	_, err := sys.Stat(ctx, path.Join(home, ".ssh"))
	return keys, err == nil
}

// usrDecidedBy is a stand-in, unit (b)'s: the rule that makes name an
// admin, from the check's evidence ("sudo: george by FILE:LINE: TEXT"), as
// users.Grant says neither the rule nor the password: "hardn", "group
// sudo" or the file; and the rule's text, whose NOPASSWD or nopass says
// the password where the check, which leaves out a locked account, has no
// finding.
func usrDecidedBy(evidence []string, name string) (by, text string) {
	for _, ev := range evidence {
		tool, rest, ok := strings.Cut(ev, ": ")
		if !ok || !strings.HasPrefix(rest, name+" by ") {
			continue
		}
		at, text, _ := strings.Cut(strings.TrimPrefix(rest, name+" by "), ": ")
		file := at
		if i := strings.LastIndexByte(at, ':'); i > 0 {
			file = at[:i]
		}
		for _, w := range strings.Fields(text) {
			if g, ok := strings.CutPrefix(w, "%"); ok && tool == "sudo" {
				return "group " + g, text
			}
			if g, ok := strings.CutPrefix(w, ":"); ok && tool == "doas" {
				return "group " + g, text
			}
		}
		if strings.HasPrefix(path.Base(file), "hardn-") {
			return "hardn", text
		}
		return file, text
	}
	return "", ""
}

// user is the user named name.
func (r usrReading) user(name string) (usrUser, bool) {
	i := slices.IndexFunc(r.users, func(u usrUser) bool { return u.Name == name })
	if i < 0 {
		return usrUser{}, false
	}
	return r.users[i], true
}

// toolWord is the tool the screen names: doas where Choose picks it, else
// sudo.
func (r usrReading) toolWord() string {
	if r.choice.Tool == sudoers.ToolDoas {
		return "doas"
	}
	return "sudo"
}

// rows are u's rows: Figma's Sudo (doas on Alpine) and SSH Keys, then who
// grants it and its password.
func (r usrReading) rows(u usrUser) []component.KeyValue {
	tool := component.KeyValue{Key: "Sudo", Value: "Disabled", State: component.ValueNone}
	if r.toolWord() == "doas" {
		tool.Key = "doas"
	}
	switch u.admin {
	case detect.Yes:
		tool.Value, tool.State = "Enabled", component.ValueOK
		if u.nopass {
			tool.Value, tool.State, tool.Note = "Enabled,", component.ValueBad, "No password"
		}
	case detect.Unknown:
		tool.Value, tool.State = "Can't read", component.ValueUnknown
	}
	rows := []component.KeyValue{tool}
	if u.admin == detect.Yes && u.by != "" {
		rows = append(rows, component.KeyValue{Key: usrKeyBy, Value: u.by})
	}
	keys := component.KeyValue{Key: usrKeyKeys, Value: "None", State: component.ValueWarn}
	switch n := len(u.keys); {
	case n == 1:
		keys = component.KeyValue{Key: usrKeyKeys, Value: "1 key"}
	case n > 1:
		keys = component.KeyValue{Key: usrKeyKeys, Value: fmt.Sprintf("%d keys", n)}
	}
	pw := component.KeyValue{Key: usrKeyPassword, Value: "Can't read", State: component.ValueUnknown}
	switch u.pw {
	case pwSet:
		pw = component.KeyValue{Key: usrKeyPassword, Value: "Set"}
	case pwNone:
		pw = component.KeyValue{Key: usrKeyPassword, Value: "None"}
	case pwLocked:
		pw = component.KeyValue{Key: usrKeyPassword, Value: "Locked,", State: component.ValueBad, Note: "key login refused"}
	}
	return append(rows, keys, pw)
}

// usrExit is the sub-menus' Return.
var usrExit = component.MenuItem{Key: "0", Label: "Return"}

// mainMenu is 01-USER: the User row, a section per user, Manage and Create.
func (r usrReading) mainMenu() screen.SubMenu {
	var sections []screen.Section
	for _, u := range r.users {
		sections = append(sections, screen.Section{Header: component.SectionHeader{Title: u.Name}, Rows: component.BulletList{Items: r.rows(u)}})
	}
	if len(sections) == 0 {
		sections = []screen.Section{{Header: component.SectionHeader{Title: "Users"}, Rows: component.BulletList{Items: []component.KeyValue{{Key: usrNoOtherUser, State: component.ValueNone}}}}}
	}
	items := []component.MenuItem{{Key: "1", Label: "Manage User", Description: "Configure sudo, SSH keys"}, {Key: "2", Label: "Create User", Description: "Configure a new user"}}
	return screen.SubMenu{Title: component.TitleBar{Title: usrTitle}, Status: r.status, Sections: sections, Heading: usrHeading,
		Menu: component.Menu{Items: items, Exit: usrExit}, Prompt: usrPrompt}
}

// selectMenu is USER-MANAGE_USER-SELECT_USER.
func (r usrReading) selectMenu() screen.SubMenu {
	var items []component.MenuItem
	for i, u := range r.users {
		items = append(items, component.MenuItem{Key: fmt.Sprint(i + 1), Label: u.Name})
	}
	return screen.SubMenu{Title: component.TitleBar{Title: usrManageTitle, Parents: []string{usrTitle}}, Heading: "Select a user",
		Menu: component.Menu{Items: items, Exit: usrExit}, Prompt: usrPrompt}
}

// manageMenu is USER-MANAGE_USER-MANAGEMENT for u: its rows; admin rights,
// with them the password, then the keys, each named by its result.
func (r usrReading) manageMenu(u usrUser) screen.SubMenu {
	grant := component.MenuItem{Key: "1", Label: usrGrantLabel, Description: "Let " + u.Name + " use " + r.toolWord()}
	if u.admin == detect.Yes {
		grant = component.MenuItem{Key: "1", Label: usrRemoveLabel, Description: "Take " + r.toolWord() + " away"}
	}
	items := []component.MenuItem{grant}
	if u.admin == detect.Yes {
		pw := component.MenuItem{Key: "2", Label: usrRequireLabel, Description: "For " + r.toolWord()}
		if !u.nopass {
			pw = component.MenuItem{Key: "2", Label: usrStopLabel, Description: "For " + r.toolWord()}
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
func (r usrReading) keysMenu(u usrUser) screen.SubMenu {
	var rows []component.KeyValue
	for _, k := range u.keys {
		rows = append(rows, component.KeyValue{Key: k.fp, Value: k.comment})
	}
	if len(rows) == 0 {
		rows = []component.KeyValue{{Key: "No SSH keys", State: component.ValueWarn}}
	}
	items := []component.MenuItem{{Key: "1", Label: usrAddKeyLabel}}
	if len(u.keys) > 0 {
		items = append(items, component.MenuItem{Key: "2", Label: usrDelKeyLabel})
	}
	return screen.SubMenu{Title: component.TitleBar{Title: usrManageTitle, Parents: []string{usrTitle}},
		Subject:  component.SubjectLine{Label: "Managing SSH keys for", Subject: u.Name},
		Sections: []screen.Section{{Header: component.SectionHeader{Title: usrKeysSection}, Rows: component.BulletList{Items: rows}}},
		Heading:  usrHeading, Menu: component.Menu{Items: items, Exit: usrExit}, Prompt: usrPrompt}
}

// toolRefusal is why no grant can be planned on the host, before anything
// is asked: no tool (sudoers.ErrNoTool), its rules not read
// (users.Elevation.NotRead); "" when one can.
func (r usrReading) toolRefusal() string {
	if len(r.notRead) > 0 {
		nr := r.notRead[0]
		check := "visudo -c"
		if nr.Tool == "doas" {
			check = "doas -C"
		}
		why := "its files don't check."
		if nr.Err != nil && !errors.Is(nr.Err, sudoers.ErrRejected) {
			why = screenkit.FullStop(nr.Err.Error())
		}
		return fmt.Sprintf(usrWordsNotRead, nr.Tool, why, check)
	}
	switch {
	case errors.Is(r.choice.Err, sudoers.ErrNoTool):
		return usrWordsNoTool
	case r.choice.Err != nil:
		return screenkit.FullStop(r.choice.Err.Error())
	}
	return ""
}

// removeRefusal is why Remove admin rights refuses for u, before asking:
// the tool's; the grant not hardn's; another rule granting it once hardn's
// is gone (stillBy, read with hardn's file hidden); the session user's own.
func (r usrReading) removeRefusal(u usrUser, stillBy string) string {
	switch {
	case r.toolRefusal() != "":
		return r.toolRefusal()
	case errors.Is(u.polErr, sudoers.ErrEdited):
		n, _ := sudoers.FileNames(r.choice.Tool, u.Name)
		return fmt.Sprintf(usrWordsEdited, n.Grant)
	case u.polErr != nil:
		return screenkit.FullStop(u.polErr.Error())
	case u.policy.Grant == sudoers.GrantNone && strings.HasPrefix(u.by, "group "):
		g := strings.TrimPrefix(u.by, "group ")
		return fmt.Sprintf(usrWordsGroup, u.Name, g, u.Name, u.Name, g)
	case u.policy.Grant == sudoers.GrantNone:
		return fmt.Sprintf(usrWordsNotHardn, u.Name, r.byWords(u.by))
	case stillBy != "":
		return fmt.Sprintf(usrWordsStill, u.Name, r.byWords(stillBy))
	case u.Name == r.invoker:
		return fmt.Sprintf(usrWordsSession, u.Name, r.toolWord())
	}
	return ""
}

// byWords is a rule's source as a sentence names it.
func (r usrReading) byWords(by string) string {
	switch {
	case strings.HasPrefix(by, "group "):
		return "the " + strings.TrimPrefix(by, "group ") + " group"
	case by == "":
		return "a rule hardn can't read"
	}
	return by
}

// passwordRefusal is why Require a password refuses for u: the tool's;
// no password to ask.
func (r usrReading) passwordRefusal(u usrUser) string {
	switch {
	case r.toolRefusal() != "":
		return r.toolRefusal()
	case u.nopass && u.pw != pwSet:
		return fmt.Sprintf(usrWordsNoPassword, u.Name, r.toolWord(), u.Name)
	}
	return ""
}

// keyRefusal is why Remove SSH key refuses for u before asking: the
// session user's last key.
func (r usrReading) keyRefusal(u usrUser) string {
	if len(u.keys) == 1 && u.Name == r.invoker {
		return fmt.Sprintf(usrWordsLastKey, u.Name, u.Name)
	}
	return ""
}

// keyRisk is whether removing one of u's keys is a lockout risk: the
// session user's (sshd's log, which says which key it logged in with, is
// not read), or anyone's last key, with no password to log in by.
func (r usrReading) keyRisk(u usrUser) bool {
	return u.Name == r.invoker || len(u.keys) == 1
}

// hiddenSys is sys with one file hidden: the host as a plan that removes it
// leaves it, for the reader. A stand-in, unit (b)'s.
type hiddenSys struct {
	host.System
	hide string
}

func (h hiddenSys) ReadFile(ctx context.Context, name string) ([]byte, error) {
	if name == h.hide {
		return nil, fs.ErrNotExist
	}
	return h.System.ReadFile(ctx, name)
}

func (h hiddenSys) ReadFileNoFollow(ctx context.Context, name string) ([]byte, fs.FileInfo, error) {
	if name == h.hide {
		return nil, nil, fs.ErrNotExist
	}
	return h.System.ReadFileNoFollow(ctx, name)
}

func (h hiddenSys) ScanLines(ctx context.Context, name string, yield func(host.Line) bool) (fs.FileInfo, error) {
	if name == h.hide {
		return nil, fs.ErrNotExist
	}
	return h.System.ScanLines(ctx, name, yield)
}

func (h hiddenSys) Stat(ctx context.Context, name string) (fs.FileInfo, error) {
	if name == h.hide {
		return nil, fs.ErrNotExist
	}
	return h.System.Stat(ctx, name)
}

func (h hiddenSys) Lstat(ctx context.Context, name string) (fs.FileInfo, error) {
	if name == h.hide {
		return nil, fs.ErrNotExist
	}
	return h.System.Lstat(ctx, name)
}

func (h hiddenSys) ReadDir(ctx context.Context, name string) ([]fs.DirEntry, error) {
	es, err := h.System.ReadDir(ctx, name)
	return slices.DeleteFunc(es, func(e fs.DirEntry) bool { return path.Join(name, e.Name()) == h.hide }), err
}

// stillAdmin is the rule that still makes u an admin once hardn's grant is
// gone, read by the real reader with the file hidden; "" for none.
func stillAdmin(ctx context.Context, sys host.System, r usrReading, u usrUser) string {
	n, err := sudoers.FileNames(r.choice.Tool, u.Name)
	if err != nil {
		return ""
	}
	hs := hiddenSys{System: sys, hide: n.Grant}
	db, err := accounts.Read(ctx, hs)
	if err != nil {
		return ""
	}
	i := slices.IndexFunc(db.Users, func(x accounts.User) bool { return x.Name == u.Name })
	if i < 0 {
		return ""
	}
	for _, g := range usersctl.ReadElevation(ctx, hs, r.f).GrantsOf(db, db.Users[i]) {
		if g.Grants == detect.Yes {
			st, _ := usersctl.Admin{}.Check(ctx, hs, r.f)
			if by, _ := usrDecidedBy(st.Evidence, u.Name); by != "" {
				return by
			}
			return g.Tool
		}
	}
	return ""
}

// usrNameForm is useradd's default NAME_REGEX on Debian (^[a-z][-a-z0-9_]*\$?$),
// with shadow's leading "_" (is_valid_user_name), at most 32 bytes.
var usrNameForm = regexp.MustCompile(`^[a-z_][-a-z0-9_]*\$?$`)

// checkName refuses a name useradd would, or one the host has.
func (r usrReading) checkName(db accounts.DB, name string) error {
	switch {
	case len(name) > 32 || !usrNameForm.MatchString(name) || sudoers.CheckUser(sudoers.ToolDoas, name) != nil:
		return nav.Refuse(usrWordsName)
	case slices.ContainsFunc(db.Users, func(u accounts.User) bool { return u.Name == name }),
		slices.ContainsFunc(db.Groups, func(g accounts.Group) bool { return g.Name == name }):
		return nav.Refuse(fmt.Sprintf(usrWordsTaken, name))
	}
	return nil
}

// usrControl is the gallery-local control, unit (b)'s: users.Admin's id,
// title and check, its Plan the change's.
type usrControl struct {
	usersctl.Admin
	plan func(ctx context.Context, sys host.System, in engine.Input) ([]engine.Change, error)
}

// Plan plans the change, reading the host where the engine plans.
func (c usrControl) Plan(ctx context.Context, sys host.System, in engine.Input) ([]engine.Change, error) {
	return c.plan(ctx, sys, in)
}

// The change ids.
const (
	usrIDGrant  = "users.admin/grant"
	usrIDCreate = "users.admin/create"
	usrIDKeys   = "users.admin/keys"
)

// grantChange is user's grant set to g through the real planner: the
// files Read, hardn's Policy read back and its variables carried, PlanFor
// and steps.Sudoers (sudoers.md §6, slice 5). nil when nothing changes.
func grantChange(ctx context.Context, sys host.System, f detect.Facts, user string, g sudoers.Grant, summary string, after ...string) (*engine.Change, error) {
	choice := sudoers.Choose(f, sudoers.ToolUnknown)
	if choice.Err != nil {
		return nil, choice.Err
	}
	st, err := sudoers.Read(ctx, sys, choice, []string{user})
	if err != nil {
		return nil, err
	}
	pol, err := sudoers.PolicyOf(st, user)
	if err != nil {
		return nil, err
	}
	pol.Grant = g
	p, err := sudoers.PlanFor(st, []sudoers.Policy{pol})
	if err != nil || len(p.Changes) == 0 {
		return nil, err
	}
	c, err := steps.Sudoers(usrIDGrant, p, summary)
	if err != nil {
		return nil, err
	}
	c.After = after
	return &c, nil
}

// usrCommand is a stand-in change, unit (b)'s, that runs argv, undone by
// undo.
func usrCommand(id, summary, target string, stage engine.Stage, argv, undo []string) engine.Change {
	return engine.Change{ID: id, Summary: summary, Title: summary, Target: target, Commands: []string{host.FormatArgv(argv)}, Stage: stage, Resources: []string{"accounts"},
		Rollback: engine.Undo{Steps: []revert.Step{{Op: revert.OpRun, Argv: undo, Why: "accounts"}}},
		Apply: func(ctx context.Context, sys host.System, _ engine.Run) (engine.Ran, error) {
			_, err := sys.Run(ctx, argv[0], argv[1:]...)
			return engine.Ran{Detail: host.FormatArgv(argv)}, err
		}}
}

// usrCreate is the stand-in that creates name: useradd with a home, the
// release's shell and, on Alpine, whose sshd has no PAM, the password field
// "*" in place of useradd's "!", which would refuse a key login (decision 3).
func usrCreate(f detect.Facts, name string) engine.Change {
	argv := []string{"useradd", "--create-home", "--shell", "/bin/bash"}
	if f.Platform.Distro == detect.DistroAlpine {
		argv = []string{"useradd", "--create-home", "--shell", "/bin/ash", "--password", "*"}
	}
	c := usrCommand(usrIDCreate, "Create user "+name, accounts.PasswdFile, engine.StageAccounts, append(argv, name), []string{"userdel", "--remove", name})
	c.StateBefore, c.StateAfter = "absent", "no password, key login"
	return c
}

// usrKeysChange is the stand-in that writes name's authorized_keys as
// lines, 0600 and the user's own, creating ~/.ssh 0700 first where it is
// not there; risky marks it a lockout risk. home is the user's.
func usrKeysChange(name, home string, before *string, lines []string, mkdir, risky bool, summary string, after ...string) engine.Change {
	dir := path.Join(home, ".ssh")
	file := path.Join(home, usrKeyFiles[0])
	data := strings.Join(lines, "\n")
	if data != "" {
		data += "\n"
	}
	mk := []string{"install", "-d", "-m", "700", "-o", name, "-g", name, dir}
	c := engine.Change{ID: usrIDKeys, Summary: summary, Target: file, Stage: engine.StageFiles, Resources: []string{"accounts"}, After: after,
		Diff: engine.FileDiff(file, before, &data)}
	if before != nil {
		c.Backups = []string{file}
	}
	if mkdir {
		c.Commands = []string{host.FormatArgv(mk)}
		c.Rollback = engine.Undo{Steps: []revert.Step{{Op: revert.OpRemoveDir, Path: dir, Why: "accounts"}}}
	}
	if risky {
		c.Risk = engine.RiskLockout
	}
	c.Apply = func(ctx context.Context, sys host.System, _ engine.Run) (engine.Ran, error) {
		if mkdir {
			if _, err := sys.Run(ctx, mk[0], mk[1:]...); err != nil {
				return engine.Ran{}, err
			}
		}
		users, err := accounts.ReadPasswd(ctx, sys)
		if err != nil {
			return engine.Ran{}, err
		}
		i := slices.IndexFunc(users, func(u accounts.User) bool { return u.Name == name })
		if i < 0 {
			return engine.Ran{}, fmt.Errorf("no user %s: %w", name, fs.ErrNotExist)
		}
		return engine.Ran{Detail: file}, sys.WriteFileAtomic(ctx, file, []byte(data), 0o600, users[i].UID, users[i].GID)
	}
	return c
}

// keyFile is u's authorized_keys as it is, nil when it is not there.
func keyFile(ctx context.Context, sys host.System, home string) *string {
	data, err := sys.ReadFile(ctx, path.Join(home, usrKeyFiles[0]))
	if err != nil {
		return nil
	}
	s := string(data)
	return &s
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
		"1": func() error {
			if len(r.users) == 0 {
				return e.refuse(r, usrWordsNoUser)
			}
			return e.s.p.Run(func() nav.Screen { return e.drawSelect(ctx) })
		},
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
	for i, u := range r.users {
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

// change runs one change through the flow: its review, its plan, the
// question.
func (e *usrRunner) change(ctx context.Context, r usrReading, title, question string, review flow.Review, plan func(ctx context.Context, sys host.System, in engine.Input) ([]engine.Change, error)) error {
	return e.kit(r).Change(ctx, screenkit.Change{
		Step: screen.WorkflowStep{Title: component.TitleBar{Title: title, Parents: []string{usrTitle}}},
		Flow: flow.Flow{Review: func() flow.Review { return review }, Confirm: usrConfirm(question)},
		Controls: func(*config.Config) []engine.Control {
			return []engine.Control{usrControl{plan: plan}}
		},
	})
}

// one is a plan of the change c builds, none when it is nil.
func one(c func(ctx context.Context, sys host.System, in engine.Input) (*engine.Change, error)) func(context.Context, host.System, engine.Input) ([]engine.Change, error) {
	return func(ctx context.Context, sys host.System, in engine.Input) ([]engine.Change, error) {
		ch, err := c(ctx, sys, in)
		if err != nil || ch == nil {
			return nil, err
		}
		return []engine.Change{*ch}, nil
	}
}

// admin is Grant or Remove admin rights for u.
func (e *usrRunner) admin(ctx context.Context, r usrReading, u usrUser) error {
	if u.admin == detect.Yes {
		if words := r.removeRefusal(u, stillAdmin(ctx, e.h.sys(), r, u)); words != "" {
			return e.refuse(r, words)
		}
		return e.change(ctx, r, usrRemoveLabel, "Remove "+u.Name+"'s admin rights?",
			flow.Review{Title: "User Configuration", Items: []component.KeyValue{{Key: "Sudo", Value: "Disabled"}}},
			one(func(ctx context.Context, sys host.System, in engine.Input) (*engine.Change, error) {
				return grantChange(ctx, sys, in.Facts, u.Name, sudoers.GrantNone, "Remove "+u.Name+"'s "+r.toolWord())
			}))
	}
	if words := r.toolRefusal(); words != "" {
		return e.refuse(r, words)
	}
	g, row := sudoers.GrantNoPassword, component.KeyValue{Key: "Sudo", Value: "Enabled,", State: component.ValueBad, Note: "No password"}
	if u.pw == pwSet {
		g, row = sudoers.GrantPassword, component.KeyValue{Key: "Sudo", Value: "Enabled", State: component.ValueOK}
	}
	return e.change(ctx, r, usrGrantLabel, "Let "+u.Name+" use "+r.toolWord()+"?",
		flow.Review{Title: "User Configuration", Items: []component.KeyValue{row}},
		one(func(ctx context.Context, sys host.System, in engine.Input) (*engine.Change, error) {
			return grantChange(ctx, sys, in.Facts, u.Name, g, "Let "+u.Name+" use "+r.toolWord())
		}))
}

// password is Require or Stop requiring a password for u's elevation:
// hardn's own grant, written to win over the rules before it.
func (e *usrRunner) password(ctx context.Context, r usrReading, u usrUser) error {
	if u.admin != detect.Yes {
		return nil
	}
	if u.nopass {
		if words := r.passwordRefusal(u); words != "" {
			return e.refuse(r, words)
		}
		return e.change(ctx, r, usrRequireLabel, "Require "+u.Name+"'s password for "+r.toolWord()+"?",
			flow.Review{Title: "User Configuration", Items: []component.KeyValue{{Key: "Sudo", Value: "Enabled", State: component.ValueOK}}},
			one(func(ctx context.Context, sys host.System, in engine.Input) (*engine.Change, error) {
				return grantChange(ctx, sys, in.Facts, u.Name, sudoers.GrantPassword, "Ask "+u.Name+"'s password")
			}))
	}
	if words := r.toolRefusal(); words != "" {
		return e.refuse(r, words)
	}
	return e.change(ctx, r, usrStopLabel, "Stop asking "+u.Name+"'s password?",
		flow.Review{Title: "User Configuration", Items: []component.KeyValue{{Key: "Sudo", Value: "Enabled,", State: component.ValueBad, Note: "No password"}}},
		one(func(ctx context.Context, sys host.System, in engine.Input) (*engine.Change, error) {
			return grantChange(ctx, sys, in.Facts, u.Name, sudoers.GrantNoPassword, "Stop asking "+u.Name+"'s password")
		}))
}

// keyField is the form's key question, validated.
func keyField(value *string, have []usrKey, name string, skip func() bool) *form.Text {
	return &form.Text{Title: "Paste SSH public key", Value: value, Skip: skip, Validate: func(s string) error {
		k, err := parseKey(s, false)
		if err != nil {
			return nav.Refuse(usrWordsKey)
		}
		if slices.ContainsFunc(have, func(h usrKey) bool { return h.fp == k.fp }) {
			return nav.Warn(fmt.Sprintf(usrWordsDupKey, name))
		}
		return nil
	}}
}

// addKey is Add SSH key: one key, then the plan.
func (e *usrRunner) addKey(ctx context.Context, r usrReading, u usrUser) error {
	var line string
	review := flow.Review{Title: usrKeysSection, Items: []component.KeyValue{{Key: usrKeyKeys, Value: fmt.Sprintf("%d keys", len(u.keys)+1)}}}
	if len(u.keys) == 0 {
		review.Items[0].Value = "1 key"
	}
	if u.pw == pwLocked {
		review.Notice = &flow.Notice{Callout: component.Callout{Kind: component.MessageWarn, Body: []string{fmt.Sprintf(usrWordsLocked, u.Name)}}}
	}
	return e.kit(r).Change(ctx, screenkit.Change{
		Step: screen.WorkflowStep{Title: component.TitleBar{Title: usrAddKeyLabel, Parents: []string{usrTitle}}},
		Flow: flow.Flow{
			Form:    form.Form{Fields: []form.Field{keyField(&line, u.keys, u.Name, nil)}},
			Review:  func() flow.Review { return review },
			Confirm: usrConfirm("Add the key for " + u.Name + "?"),
		},
		Controls: func(*config.Config) []engine.Control {
			return []engine.Control{usrControl{plan: func(ctx context.Context, sys host.System, in engine.Input) ([]engine.Change, error) {
				before := keyFile(ctx, sys, u.Home)
				var lines []string
				if before != nil {
					lines = strings.Split(strings.TrimSuffix(*before, "\n"), "\n")
				}
				k, _ := parseKey(line, false)
				_, dirErr := sys.Stat(ctx, path.Join(u.Home, ".ssh"))
				return []engine.Change{usrKeysChange(u.Name, u.Home, before, append(lines, k.line), dirErr != nil, false, "Add a key for "+u.Name)}, nil
			}}}
		},
	})
}

// removeKey is Remove SSH key: which, by fingerprint, then the plan, the
// line really removed; a lockout risk where keyRisk says.
func (e *usrRunner) removeKey(ctx context.Context, r usrReading, u usrUser) error {
	if len(u.keys) == 0 {
		return nil
	}
	if words := r.keyRefusal(u); words != "" {
		return e.refuse(r, words)
	}
	gone := 0
	opts := make([]form.Option[int], len(u.keys))
	for i, k := range u.keys {
		opts[i] = form.Option[int]{Label: k.fp + " " + k.comment, Value: i}
	}
	n := len(u.keys) - 1
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
			return []engine.Control{usrControl{plan: func(ctx context.Context, sys host.System, in engine.Input) ([]engine.Change, error) {
				before := keyFile(ctx, sys, u.Home)
				if before == nil {
					return nil, nil // the key is in authorized_keys2: not planned here (§7)
				}
				var keep []string
				for _, l := range strings.Split(strings.TrimSuffix(*before, "\n"), "\n") {
					if k, err := parseKey(l, true); err == nil && k.fp == u.keys[gone].fp {
						continue
					}
					keep = append(keep, l)
				}
				return []engine.Change{usrKeysChange(u.Name, u.Home, before, keep, false, r.keyRisk(u), "Remove a key from "+u.Name)}, nil
			}}}
		},
	})
}

// create is Create User: Figma's questions, the password one in Figma's
// polarity (P-11), refused at yes (decision 3); one plan for the account,
// the grant and the key.
func (e *usrRunner) create(ctx context.Context, r usrReading) error {
	db, err := accounts.Read(ctx, e.h.system())
	if err != nil {
		return e.refuse(r, screenkit.FullStop(err.Error()))
	}
	var name, key string
	var admin, password, addKey bool
	tool := r.toolWord()
	fields := []form.Field{
		&form.Text{Title: "Username", Value: &name, Validate: func(s string) error { return r.checkName(db, s) }},
		&form.YesNo{Title: "Grant " + tool + " privilege?", Value: &admin, Validate: func(yes bool) error {
			if words := r.toolRefusal(); yes && words != "" {
				return nav.Refuse(words)
			}
			return nil
		}},
		&form.YesNo{Title: "Require " + tool + " password?", Value: &password, Skip: func() bool { return !admin }, Validate: func(yes bool) error {
			if yes {
				return nav.Refuse(fmt.Sprintf(usrWordsNewNoPW, tool))
			}
			return nil
		}},
		&form.YesNo{Title: "Add SSH key?", Value: &addKey},
		keyField(&key, nil, "", func() bool { return !addKey }),
	}
	return e.kit(r).Change(ctx, screenkit.Change{
		Step: screen.WorkflowStep{Title: component.TitleBar{Title: usrCreateTitle, Parents: []string{usrTitle}}},
		Flow: flow.Flow{
			Form: form.Form{Fields: fields},
			Review: func() flow.Review {
				sudoRow := component.KeyValue{Key: "Sudo", Value: "Disabled"}
				if tool == "doas" {
					sudoRow.Key = "doas"
				}
				if admin {
					sudoRow.Value, sudoRow.State, sudoRow.Note = "Enabled,", component.ValueBad, "No password"
				}
				keyRow := component.KeyValue{Key: usrKeyKeys, Value: "None", State: component.ValueWarn}
				rv := flow.Review{Title: "User Configuration", Items: []component.KeyValue{sudoRow, keyRow, {Key: usrKeyPassword, Value: "None"}}}
				if addKey {
					rv.Items[1] = component.KeyValue{Key: usrKeyKeys, Value: "1 key"}
				} else {
					rv.Notice = &flow.Notice{Callout: component.Callout{Kind: component.MessageWarn, Body: []string{fmt.Sprintf(usrWordsNoKeyNotice, name)}}}
				}
				return rv
			},
			Confirm: func(flow.Plan) flow.Confirm { return flow.Confirm{Question: fmt.Sprintf("Create user '%s'?", name)} },
		},
		Controls: func(*config.Config) []engine.Control {
			return []engine.Control{usrControl{plan: func(ctx context.Context, sys host.System, in engine.Input) ([]engine.Change, error) {
				out := []engine.Change{usrCreate(in.Facts, name)}
				if admin {
					g, err := grantChange(ctx, sys, in.Facts, name, sudoers.GrantNoPassword, "Let "+name+" use "+tool, usrIDCreate)
					if err != nil {
						return nil, err
					}
					if g != nil {
						out = append(out, *g)
					}
				}
				if addKey {
					k, _ := parseKey(key, false)
					out = append(out, usrKeysChange(name, "/home/"+name, nil, []string{k.line}, true, false, "Add "+name+"'s key", usrIDCreate))
				}
				return out, nil
			}}}
		},
	})
}

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
		Name: "users mock", Prompter: s.p, System: h.system(), Config: cfg,
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
		{"remove", "hardn", "george"},       // the session user's own grant
		{"remove", "debian13", "george"},    // cloud-init's file
		{"remove", "debian13", "alice"},     // the sudo group
		{"remove", "legacy", "george"},      // origin/main's file
		{"remove", "alpine323", "george"},   // wheel
		{"remove", "not-read", "george"},    // rules not read
		{"require", "debian13", "george"},   // no password
		{"require", "alpine323", "george"},  // no password, doas
		{"remove-key", "one-key", "george"}, // the session user's last key
		{"grant", "no-sudo", "george"},      // no sudo
		{"grant", "not-read", "alice"},      // rules not read
	}
}

// refusalOf is rf's words on its host, "" for none.
func refusalOf(ctx context.Context, rf usrRefusal) (string, error) {
	r, h, err := usrReadNamed(ctx, rf.host)
	if err != nil {
		return "", err
	}
	u, ok := r.user(rf.user)
	if !ok {
		return "", fmt.Errorf("%s: no user %s", rf.host, rf.user)
	}
	switch rf.item {
	case "remove":
		return r.removeRefusal(u, stillAdmin(ctx, h.sys(), r, u)), nil
	case "require":
		return r.passwordRefusal(u), nil
	case "remove-key":
		return r.keyRefusal(u), nil
	}
	return r.toolRefusal(), nil
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
const usrNote = "Play mode, mocks: the User Management screen proposed for slice 5, drawn from what real code reads of a simulated host " +
	"(users.Admin.Check, users.ReadElevation, sudoers.Read, PolicyOf, Invoker, ssh.ReadPAM), each grant the real sudoers.PlanFor on the real plan/apply engine. " +
	"Creating a user and the keys are the gallery's stand-ins, unit (b)'s to build. Nothing on this machine changes: every command and file is the fake's."
