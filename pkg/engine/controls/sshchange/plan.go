package sshchange

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"slices"
	"strconv"
	"strings"

	"github.com/abbott/hardn/pkg/config"
	"github.com/abbott/hardn/pkg/engine"
	"github.com/abbott/hardn/pkg/engine/controls/ssh"
	"github.com/abbott/hardn/pkg/engine/controls/users"
	"github.com/abbott/hardn/pkg/engine/steps"
	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/accounts"
	"github.com/abbott/hardn/pkg/host/pve"
	"github.com/abbott/hardn/pkg/host/sshd"
	"github.com/abbott/hardn/pkg/host/sshd/migrate"
)

// The decided words of the refusals (gallery-ssh.md §6; the mock's
// sshWords* constants, decision 10, 2026-11-03), each a *steps.Refusal's
// Why and Fix as screenkit.RefusalWords joins them.
const (
	wordsNotRead     = "hardn can't read sshd's settings: %s. Nothing can change until it can."
	wordsNoRunDir    = "/run/sshd isn't there yet, so sshd -T can't run. Open one SSH connection, then try again."
	wordsRootOwn     = "You're logged in as root over SSH. Turning root login off would close the way you came in. Log in as an admin and run hardn through sudo."
	wordsNoSession   = "hardn can't tell how you logged in: sshd's log has no line for this connection. Nothing that closes a way in can change."
	wordsNoAdmin     = "No admin could log in over SSH afterwards: %s. Fix that first."
	wordsByPassword  = "You logged in with a password. Turning passwords off would close the way you came in. Log in with a key first."
	wordsPassNoKey   = "%s would lose SSH login: %s a password and no key. Add a key in User Management first."
	wordsMatch       = "%s, in %s, lets more in for some connections, and hardn's setting wouldn't change that. Change it there first."
	wordsPVEKeyOnly  = "Root stays key-only on Proxmox VE: the cluster's nodes log in to each other as root by key."
	wordsPVEByPass   = "You logged in as root with a password. Making root key-only would close the way you came in. Log in as root with a key first."
	wordsPVENoKey    = "Root would lose SSH login: %s. Add a key for root first."
	wordsListenConf  = "hardn v0.3.x's listen.conf (port %s for ssh.socket) never took effect: ssh.socket listens on %s. Moving it now couldn't be undone. Let %s through the firewall, run systemctl daemon-reload and systemctl restart ssh.socket, check a new connection to %s works, then try again."
	wordsPVEPort     = "The SSH port stays 22 on Proxmox VE: the cluster and pve-firewall use it."
	wordsRestricted  = "ufw lets port %d in from %s alone. A new port would be open to anyone. Move that rule to the new port by hand first."
	wordsMigrate     = "hardn's old SSH files are still here (%s). Move them into 00-hardn.conf first: item 4."
	wordsIncludeNot  = "sshd_config doesn't start with \"" + sshd.IncludeLine + "\", so hardn's settings wouldn't be read first. Put that line first, then try again."
	wordsNoAdminRoot = "there's no admin besides root"
)

// Errors of the refusals, testable with errors.Is, each the Err of a
// *steps.Refusal in the decided words, wrapping the reader's error where
// there is one. Hardn's old files, an Include not first, Proxmox VE's port
// and the D9 listen.conf are the planner's own sentinels and codes
// (steps.ErrMigrateFirst, steps.ErrNoWriteStrategy, pve.ErrPortChange,
// steps.ErrListenConfNotInEffect), /run/sshd missing steps.ErrNoRunDir's.
var (
	// ErrNotRead: sshd -T, or sshd's files, are not read.
	ErrNotRead = errors.New("sshd's settings are not read, so no SSH change is planned")
	// ErrNoSession: the session's login is not in sshd's log.
	ErrNoSession = errors.New("the session's login is not in sshd's log, so nothing that closes a way in is planned")
	// ErrRootSession: the session is root's own SSH login.
	ErrRootSession = errors.New("the session is root's own SSH login")
	// ErrNoAdmin: no admin could log in over SSH and become root afterwards.
	ErrNoAdmin = errors.New("no admin could log in over SSH and become root afterwards")
	// ErrPasswordSession: the session logged in with a password.
	ErrPasswordSession = errors.New("the session logged in with a password")
	// ErrPasswordOnly: an admin with a password and no key would lose SSH login.
	ErrPasswordOnly = errors.New("an admin with a password and no key would lose SSH login")
	// ErrMatchMore: a Match block lets more in than the change would.
	ErrMatchMore = errors.New("a Match block lets more in than hardn's setting")
	// ErrPVEKeyOnly: root is key-only on Proxmox VE already, and stays so.
	ErrPVEKeyOnly = errors.New("root stays key-only on Proxmox VE")
	// ErrPVEPasswordSession: on Proxmox VE, the session is root's, by password.
	ErrPVEPasswordSession = errors.New("the session is root's own SSH login, by password")
	// ErrPVENoRootKey: on Proxmox VE, root could not log in by key afterwards.
	ErrPVENoRootKey = errors.New("root could not log in by key afterwards")
	// ErrPortRestricted: ufw lets the port sshd listens on in from some
	// sources alone.
	ErrPortRestricted = errors.New("ufw lets the SSH port in from some sources alone")
)

// The refusals' codes, the registry's for the errors they wrap (pkg/output;
// pinned by the tests).
const (
	codeNotRead            = "SSH_SETTINGS_NOT_READ"
	codeNoSession          = "SSH_SESSION_NOT_LOGGED"
	codeRootSession        = "SSH_ROOT_SESSION"
	codeNoAdmin            = "SSH_NO_ADMIN_LOGIN"
	codePasswordSession    = "SSH_PASSWORD_SESSION"
	codePasswordOnly       = "SSH_ADMIN_PASSWORD_ONLY"
	codeMatchMore          = "SSH_MATCH_LETS_MORE"
	codePVEKeyOnly         = "SSH_PVE_ROOT_KEY_ONLY"
	codePVEPasswordSession = "SSH_PVE_ROOT_PASSWORD_SESSION"
	codePVENoRootKey       = "SSH_PVE_NO_ROOT_KEY"
	codePortRestricted     = "SSH_PORT_SOURCE_RESTRICTED"
	codeMigrateFirst       = "SSH_MIGRATE_FIRST"
	codeIncludeNot         = "SSHD_NO_WRITE_STRATEGY"
	codePVEPort            = "PVE_PORT_CHANGE_REFUSED"
)

// MigrateID is Migrate's ID, and its changes' prefix.
const MigrateID = "ssh.migrate"

// refuse is a refusal: its code, its sentinel wrapping cause where there
// is one, and words split at their last sentence into a Why and a Fix,
// which screenkit.RefusalWords joins back into them.
func refuse(code string, sentinel, cause error, words string) error {
	err := sentinel
	if cause != nil {
		err = fmt.Errorf("%w: %w", sentinel, cause)
	}
	why, fix := strings.TrimSuffix(words, "."), ""
	if i := strings.LastIndex(why, ". "); i >= 0 {
		why, fix = why[:i], why[i+2:]
	}
	return &steps.Refusal{Code: code, Why: why, Fix: fix, Err: err}
}

// common is why no change can be planned: sshd -T or the files not read
// (/run/sshd missing in its own words); hardn's old files, but for moving
// them (migrating); an Include not first. nil when none.
func (r Reading) common(migrating bool) error {
	var env *sshd.EnvError
	switch {
	case errors.As(r.EffErr, &env) && env.Kind == sshd.EnvPrivsepDir:
		return refuse(steps.CodeNoRunDir, steps.ErrNoRunDir, r.EffErr, wordsNoRunDir)
	case r.EffErr != nil:
		return refuse(codeNotRead, ErrNotRead, r.EffErr, fmt.Sprintf(wordsNotRead, r.EffErr))
	case r.FoundErr != nil:
		return refuse(codeNotRead, ErrNotRead, r.FoundErr, fmt.Sprintf(wordsNotRead, r.FoundErr))
	case len(r.Found.Artifacts) > 0 && !migrating:
		return refuse(codeMigrateFirst, steps.ErrMigrateFirst, nil, fmt.Sprintf(wordsMigrate, r.legacyNames()))
	case r.Found.Include.Mode == sshd.IncludeOther:
		return refuse(codeIncludeNot, steps.ErrNoWriteStrategy, nil, wordsIncludeNot)
	}
	return nil
}

// legacyNames are hardn's old files' names.
func (r Reading) legacyNames() string {
	var names []string
	for _, a := range r.Found.Artifacts {
		names = append(names, path.Base(a.Path))
	}
	return strings.Join(names, ", ")
}

// session is why a change that closes a way in refuses for the session,
// before the admins are weighed: none from a console; the session's line
// not in sshd's log; root's own login, where root (not Proxmox VE's, which
// stays key-only) is what closes.
func (r Reading) session(rootClosing bool) error {
	switch {
	case r.Console():
	case r.Login.User == "":
		return refuse(codeNoSession, ErrNoSession, r.LoginErr, wordsNoSession)
	case rootClosing && r.RootOwn() && !r.PVE:
		return refuse(codeRootSession, ErrRootSession, nil, wordsRootOwn)
	}
	return nil
}

// matchMore refuses for the first Match line that lets in more than
// global for keyword.
func (r Reading) matchMore(keyword, global string) error {
	ms := r.MatchMore(keyword, global)
	if len(ms) == 0 {
		return nil
	}
	return refuse(codeMatchMore, ErrMatchMore, nil, fmt.Sprintf(wordsMatch, strings.TrimSpace(ms[0].Match.Text), path.Base(ms[0].File)))
}

// noAdmin refuses where no admin could log in afterwards, by key alone
// where keyOnly.
func noAdmin(admins []Admin, keyOnly bool) error {
	var whys []string
	for _, a := range admins {
		if a.Key || (!keyOnly && a.Password) {
			return nil
		}
		whys = append(whys, a.Why)
	}
	why := wordsNoAdminRoot
	if len(whys) > 0 {
		why = strings.Join(whys, "; ")
	}
	return refuse(codeNoAdmin, ErrNoAdmin, nil, fmt.Sprintf(wordsNoAdmin, why))
}

// rootRefusal is why setting PermitRootLogin to want refuses, in the
// mock's order (gallery-ssh.md §5 decisions 2, 3): the common refusals;
// on Proxmox VE root key-only already; then, but where root's login only
// opens (from no) and no admin is weighed, the session, a Match block
// letting root in more, and on Proxmox VE root's own key, elsewhere an
// admin able to log in and become root.
func (r Reading) rootRefusal(ctx context.Context, sys host.System) error {
	if err := r.common(false); err != nil {
		return err
	}
	switch {
	case r.PVE && r.RootValue() == pve.RootLogin:
		return refuse(codePVEKeyOnly, ErrPVEKeyOnly, nil, wordsPVEKeyOnly)
	case r.RootValue() == "no": // root key-only opens a way in: no check of the admins
		return nil
	}
	if err := r.session(true); err != nil {
		return err
	}
	if err := r.matchMore("permitrootlogin", "no"); err != nil {
		return err
	}
	if r.PVE { // root key-only: root's own key is the way in kept
		if r.RootOwn() && !r.ByKey() {
			return refuse(codePVEPasswordSession, ErrPVEPasswordSession, nil, wordsPVEByPass)
		}
		if a := r.RootPath(ctx, sys); !a.Key {
			return refuse(codePVENoRootKey, ErrPVENoRootKey, nil, fmt.Sprintf(wordsPVENoKey, a.Why))
		}
		return nil
	}
	keyOnly := !r.PasswordsOn()
	return noAdmin(r.LoginPaths(ctx, sys, keyOnly), keyOnly)
}

// passwordRefusal is why Key-only login refuses (gallery-ssh.md §5
// decision 4): the common refusals; where passwords log in, the session
// (its line, then a password session), a Match block letting passwords in;
// on Proxmox VE (where root by key, which pve.PlanSSH keeps, is the way
// in) root able by key (RootPath), elsewhere an admin with a password and
// no key, then an admin able by key.
func (r Reading) passwordRefusal(ctx context.Context, sys host.System) error {
	if err := r.common(false); err != nil {
		return err
	}
	if !r.PasswordsOn() {
		return nil
	}
	if err := r.session(false); err != nil {
		return err
	}
	if !r.Console() && !r.ByKey() {
		return refuse(codePasswordSession, ErrPasswordSession, nil, wordsByPassword)
	}
	if err := r.matchMore("passwordauthentication", "no"); err != nil {
		return err
	}
	if r.PVE { // root by key is the way in kept, as rootRefusal weighs it
		if a := r.RootPath(ctx, sys); !a.Key {
			return refuse(codePVENoRootKey, ErrPVENoRootKey, nil, fmt.Sprintf(wordsPVENoKey, a.Why))
		}
		return nil
	}
	admins := r.LoginPaths(ctx, sys, true)
	for _, a := range admins {
		i := slices.IndexFunc(r.Users.Users, func(u users.User) bool { return u.Name == a.Name })
		if !a.Key && i >= 0 && r.Users.Users[i].Password == accounts.PasswordSet && len(r.Users.Users[i].Keys) == 0 {
			return refuse(codePasswordOnly, ErrPasswordOnly, nil, fmt.Sprintf(wordsPassNoKey, a.Name, a.Name+" has"))
		}
	}
	return noAdmin(admins, true)
}

// portRefusal is why a port change refuses (gallery-ssh.md §5 decision
// 5): the common refusals; Proxmox VE; ufw letting 22 in from some
// sources alone (P-33 (2) (a)), the sources joined by ", ".
func (r Reading) portRefusal() error {
	if err := r.common(false); err != nil {
		return err
	}
	switch {
	case r.PVE:
		return refuse(codePVEPort, pve.ErrPortChange, nil, wordsPVEPort)
	case len(r.OnlyFrom) > 0:
		return refuse(codePortRestricted, ErrPortRestricted, nil, fmt.Sprintf(wordsRestricted, r.OnlyFromPort, strings.Join(r.OnlyFrom, ", ")))
	}
	return nil
}

// planned is the planner's refusal in the screen's words where it has
// them: v0.3.x's listen.conf never in effect (D9), its error kept
// wrapped. Any other is returned as it is.
func (r Reading) planned(cs []engine.Change, err error) ([]engine.Change, error) {
	if err == nil || !errors.Is(err, steps.ErrListenConfNotInEffect) {
		return cs, err
	}
	var ps []string
	for _, l := range r.Effective.Listeners.Socket {
		ps = append(ps, strconv.Itoa(int(l.Port)))
	}
	slices.Sort(ps)
	p := r.ListenPort
	return nil, refuse(steps.CodeListenConfNotInEffect, err, nil, fmt.Sprintf(wordsListenConf, p, strings.Join(slices.Compact(ps), " and "), p, p))
}

// planSSH plans s under the control's id, reading nothing more.
func (r Reading) planSSH(ctx context.Context, sys host.System, in engine.Input, prefix string, s steps.SSH) ([]engine.Change, error) {
	s.Prefix = prefix
	return r.planned(steps.PlanSSH(ctx, sys, in, s))
}

// directive is one sshd directive.
func directive(k, v string) migrate.Directive {
	return migrate.Directive{Keyword: k, Args: []string{v}}
}

// Root sets PermitRootLogin: Want "no" is Disallow root login,
// "prohibit-password" Allow root key-only; on Proxmox VE either is Root
// key-only, prohibit-password (pve.RootLogin), which root keeps. Getenv
// looks up hardn's environment for the session user (users.Read). Its zero
// value (Want "") plans nothing: engine.ErrNotMigrated.
type Root struct {
	ssh.Login
	Want   string
	Getenv func(string) string
}

// Plan reads the host (Read, with in.Session), refuses as rootRefusal
// does, and plans PermitRootLogin through steps.PlanSSH; nothing where
// sshd -T shows it already (off Proxmox VE).
func (c Root) Plan(ctx context.Context, sys host.System, in engine.Input) ([]engine.Change, error) {
	switch c.Want {
	case "":
		return nil, engine.ErrNotMigrated
	case "no", pve.RootLogin:
	default:
		return nil, fmt.Errorf("sshchange: PermitRootLogin %q: %w", c.Want, fs.ErrInvalid)
	}
	r, err := Read(ctx, sys, in.Facts, in.Config, in.Session, c.Getenv)
	if err != nil {
		return nil, err
	}
	want := c.Want
	if r.PVE {
		want = pve.RootLogin
	} else if r.common(false) == nil && r.RootValue() == want {
		return nil, nil
	}
	if err := r.rootRefusal(ctx, sys); err != nil {
		return nil, err
	}
	return r.planSSH(ctx, sys, in, c.ID(), steps.SSH{Set: []migrate.Directive{directive("PermitRootLogin", want)}})
}

// Disallow is Disallow root login as -d and Run All plan it: Root, with
// Want "no", never loosening root's login. Root login already off (sshd
// -T's PermitRootLogin no) is nothing to change, on Proxmox VE as
// elsewhere, though there Root would plan root key-only
// (prohibit-password), which only the screen's Root key-only item offers;
// root key-only already there (ErrPVEKeyOnly, SSH_PVE_ROOT_KEY_ONLY) is
// nothing to change too, as a flag and Run All are idempotent.
type Disallow struct{ Root }

// Plan is Root's, with Proxmox VE's root key-only already, and root's
// login already off there, planned as nothing to change. Root login off
// is read first, where sshd's settings read as Root's own check reads
// them (common), so that a key-only plan it would never make, which Proxmox
// VE's check of the candidate may refuse (an AllowUsers leaving root out),
// is not planned at all.
func (c Disallow) Plan(ctx context.Context, sys host.System, in engine.Input) ([]engine.Change, error) {
	r, err := Read(ctx, sys, in.Facts, in.Config, in.Session, c.Getenv)
	if err != nil {
		return nil, err
	}
	if r.PVE && r.common(false) == nil && r.RootValue() == "no" { // root key-only would loosen it: the screen's item
		return nil, nil
	}
	cs, err := c.Root.Plan(ctx, sys, in)
	if errors.Is(err, ErrPVEKeyOnly) {
		return nil, nil
	}
	return cs, err
}

// AuthAction is what Auth's Plan does.
type AuthAction uint8

// The actions.
const (
	// AuthNone is the status list's: Plan is engine.ErrNotMigrated.
	AuthNone AuthAction = iota
	// AuthKeyOnly is Key-only login: PasswordAuthentication no, and
	// KbdInteractiveAuthentication no where it is yes and sshd uses PAM.
	AuthKeyOnly
	// AuthPasswordsOn is Password login on: PasswordAuthentication yes.
	AuthPasswordsOn
)

// Auth turns password login off or back on. Getenv looks up hardn's
// environment for the session user (users.Read).
type Auth struct {
	ssh.Auth
	Action AuthAction
	Getenv func(string) string
}

// Plan reads the host (Read, with in.Session), refuses as passwordRefusal
// does (Password login on: the common refusals alone), and plans through
// steps.PlanSSH; nothing where passwords are already off (Key-only) or on
// (Password login on), as PasswordsOn reads them.
func (c Auth) Plan(ctx context.Context, sys host.System, in engine.Input) ([]engine.Change, error) {
	switch c.Action {
	case AuthNone:
		return nil, engine.ErrNotMigrated
	case AuthKeyOnly, AuthPasswordsOn:
	default:
		return nil, fmt.Errorf("sshchange: auth action %d: %w", c.Action, fs.ErrInvalid)
	}
	r, err := Read(ctx, sys, in.Facts, in.Config, in.Session, c.Getenv)
	if err != nil {
		return nil, err
	}
	if c.Action == AuthPasswordsOn {
		if err := r.common(false); err != nil || r.PasswordsOn() {
			return nil, err
		}
		return r.planSSH(ctx, sys, in, c.ID(), steps.SSH{Set: []migrate.Directive{directive("PasswordAuthentication", "yes")}})
	}
	if err := r.passwordRefusal(ctx, sys); err != nil || !r.PasswordsOn() {
		return nil, err
	}
	set := []migrate.Directive{directive("PasswordAuthentication", "no")}
	if r.Effective.KbdInteractiveAuthentication.Value == "yes" && r.Effective.UsePAM.Value == "yes" {
		set = append(set, directive("KbdInteractiveAuthentication", "no"))
	}
	return r.planSSH(ctx, sys, in, c.ID(), steps.SSH{Set: set})
}

// Port adds port To, 22 held until the change is confirmed (steps.SSH's
// Port). Its zero value (To 0) plans nothing: engine.ErrNotMigrated.
type Port struct {
	ssh.Port
	To uint16
}

// Plan reads the host (Read, with in.Session), refuses as portRefusal
// does, and plans through steps.PlanSSH, which refuses a port in use and
// plans nothing for one sshd has in effect already.
func (c Port) Plan(ctx context.Context, sys host.System, in engine.Input) ([]engine.Change, error) {
	if c.To == 0 {
		return nil, engine.ErrNotMigrated
	}
	r, err := Read(ctx, sys, in.Facts, in.Config, in.Session, nil)
	if err != nil {
		return nil, err
	}
	if err := r.portRefusal(); err != nil {
		return nil, err
	}
	return r.planSSH(ctx, sys, in, c.ID(), steps.SSH{Port: c.To})
}

// Migrate moves hardn's old SSH files into 00-hardn.conf
// (steps.PlanMigration), ConfigFile hardn.yml's sshConfigFile ("" for
// none; in.Config's is not read). Its check is SSH Login's.
type Migrate struct {
	ssh.Login
	ConfigFile string
}

// ID is MigrateID.
func (Migrate) ID() string { return MigrateID }

// Title is the item's name (proposed).
func (Migrate) Title() string { return "Move old settings" }

// Plan reads the host (Read, with in.Session), refuses for sshd not read
// or an Include not first, and plans the migration; nothing where there is
// no old file. The D9 listen.conf is refused in the screen's words.
func (c Migrate) Plan(ctx context.Context, sys host.System, in engine.Input) ([]engine.Change, error) {
	r, err := Read(ctx, sys, in.Facts, &config.Config{SshConfigFile: c.ConfigFile}, in.Session, nil)
	if err != nil {
		return nil, err
	}
	if err := r.common(true); err != nil {
		return nil, err
	}
	return r.planned(steps.PlanMigration(ctx, sys, in, MigrateID, c.ConfigFile))
}
