// Package runall is Run All's planner (refactor plan slice 8, unit (b);
// docs/engine/control-runall.md): what hardn.yml asks of each control, each
// control planned alone and skipped, said, where it refuses, doesn't apply
// or has nothing to change, and the rest in one plan on the engine. The Run
// All screen (main menu 6) and hardn -r both draw from its Reading, so the
// two never differ. It reads the host only through host.System and the
// engine, writes nothing, and renders nothing: its rows are data and words.
//
// The mock (internal/tools/uigallery/runall.go, gallery-runall.md §5 to §7,
// adopted 2026-11-04) is the specification: raAsk is Asked, raReading.pick,
// alone, port and userThere are Read.
package runall

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/abbott/hardn/pkg/config"
	"github.com/abbott/hardn/pkg/engine"
	"github.com/abbott/hardn/pkg/engine/controls"
	"github.com/abbott/hardn/pkg/engine/controls/apparmor"
	backupctl "github.com/abbott/hardn/pkg/engine/controls/backup"
	dnsctl "github.com/abbott/hardn/pkg/engine/controls/dns"
	envctl "github.com/abbott/hardn/pkg/engine/controls/environment"
	fwctl "github.com/abbott/hardn/pkg/engine/controls/firewall"
	"github.com/abbott/hardn/pkg/engine/controls/ssh"
	"github.com/abbott/hardn/pkg/engine/controls/sshchange"
	"github.com/abbott/hardn/pkg/engine/controls/updates"
	"github.com/abbott/hardn/pkg/engine/controls/users"
	"github.com/abbott/hardn/pkg/engine/risk"
	"github.com/abbott/hardn/pkg/engine/steps"
	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/revert"
)

// The words (gallery-runall.md §6, decided 2026-11-04): the screen's title
// and question, and a row's outcome where it is not the change's own.
const (
	Title    = "Run All"
	Question = "Apply this plan?"

	WordsAsked     = "Already as asked"
	WordsNothing   = "Nothing asked"
	WordsStatus    = "Status only"
	WordsUnchanged = "Used, not changed"
	WordsRefused   = "Refused"
	WordsPending   = "Change pending"
	// WordsAfterFirewall is root login off where the firewall is planned
	// with it (decision 5 (a)).
	WordsAfterFirewall = "After the firewall: run Run All again"
	// WordsAfterUpgrade is a row the openssh upgrade defers (afterUpgrade).
	WordsAfterUpgrade = "After the openssh upgrade: run Run All again"
	// PendingWords are a change waiting for hardn confirm's, in full: the
	// SSH Login screen's (pkg/screens/ssh), where the journal holds an
	// applied lockout-risk change (revert.State.Waiting). An unfinished
	// rollback, or a change that did not finish, is revert's own words,
	// with its hardn rollback.
	PendingWords = "A change is waiting for hardn confirm. Confirm it from a new connection, or let it roll back, then try again."

	userMgmt = "User Management's: "
	sshLogin = "SSH Login's: "
	keyOnly  = "Root key-only"
)

// Kind is a row's outcome.
type Kind uint8

// The outcomes.
const (
	// Planned: in the one plan; Words are the change's.
	Planned Kind = iota + 1
	// AsAsked: planned alone, nothing to change; or the port sshd listens
	// on; or the user there, an admin with every asked key.
	AsAsked
	// NothingAsked: hardn.yml asks nothing of it.
	NothingAsked
	// Left: Run All never plans it: AppArmor, status only; Backup, its
	// settings the plan's backups.
	Left
	// NotApplicable: the control's check says it is not for this host;
	// Words are its reason.
	NotApplicable
	// Refused: planned alone, it refuses; Refusal is why, in full.
	Refused
	// Pending: a change is journalled; Refusal is PendingWords where it
	// waits for hardn confirm, else revert's words (words).
	Pending
	// Elsewhere: another screen's: the port SSH Login's, a user there
	// User Management's, with what is missing.
	Elsewhere
	// Later: root login off skipped where the firewall is planned
	// (decision 5 (a)); the next Run All plans it.
	Later
)

var kindNames = [...]string{"", "planned", "as asked", "nothing asked", "left", "not applicable", "refused", "pending", "elsewhere", "later"}

func (k Kind) String() string {
	if int(k) < len(kindNames) && k != 0 {
		return kindNames[k]
	}
	return fmt.Sprintf("Kind(%d)", k)
}

// Input is what Read reads with: the caller's, as each screen's Options are.
type Input struct {
	System host.System
	Facts  detect.Facts
	// Config is hardn.yml as loaded: what Run All is asked.
	Config *config.Config
	// Session is the session applying the change (revert.ReadSession), as
	// the engine's: the SSH Port row reads sshd with it.
	Session revert.Session
	// Getenv is hardn's environment (os.Getenv in hardn): SUDO_USER and
	// DOAS_USER, for Create's and root login's proofs and Environment's
	// check. nil is an empty environment.
	Getenv func(string) string
	// Revert are the journal's options, as the engine's Options.Revert:
	// the revert state is read with them before any plan, and they are
	// Auto Updates' record's (updates.Auto.Records). Zero in hardn.
	Revert revert.Options
	// Version is hardn's, which Auto Updates' record carries.
	Version string
	// NewEngine returns the engine each plan is made with, as the screens'
	// NewEngine: the caller's options (the session, the backups hardn.yml's,
	// the revert options). Read plans with it and never applies.
	NewEngine func(progress func(engine.Event)) *engine.Engine
}

// getenv is in.Getenv, never nil.
func (in Input) getenv(k string) string {
	if in.Getenv == nil {
		return ""
	}
	return in.Getenv(k)
}

// IDs are the rows' controls, in the screen's order: the main menu's
// (risk.Rows), then Backup and Environment.
func IDs() []string {
	var ids []string
	for _, r := range risk.Rows() {
		ids = append(ids, r.ID)
	}
	return append(ids, backupctl.ID, envctl.ID)
}

// Checked are the controls whose checks the rows show: the main menu's,
// then Backup's and Environment's.
func Checked(cfg *config.Config, getenv func(string) string) []engine.Control {
	return append(controls.Status(cfg), backupctl.Settings{Config: cfg}, envctl.Keep{Getenv: getenv})
}

// Ask is what hardn.yml asks Run All of one row (decision 4 (a)).
type Ask struct {
	// Control is what Run All plans; nil where it plans nothing.
	Control engine.Control
	// Words are the change's, the row's when it is planned.
	Words string
	// Port is the SSH Port row's sshPort, compared with sshd's listeners
	// alone: never planned (decision 3).
	Port uint16
	// Unfixed: asked by the host, not hardn.yml: the openssh upgrade, on
	// Alpine, only where sshd lacks CVE-2026-123158's fix
	// (sshchange.ReadFix); the row is Nothing asked elsewhere.
	Unfixed bool
}

// Asked reports whether hardn.yml asks anything of the row.
func (a Ask) Asked() bool { return a.Control != nil || a.Port != 0 }

// Asked is the mapping, hardn.yml's keys to the controls, key by key
// (decision 4 (a)): username and sshKeys, Create with admin rights and the
// keys, its grant asking no password, as -c's; disableRootSSH true, root
// login off, key-only on Proxmox VE, never loosened (sshchange.Disallow,
// as -d's); sshPort, compared only;
// enableUfwSshPolicy true, Enable; configureDns true with nameservers, the
// nameservers; enableUnattendedUpgrades true, on (never off). SSH Auth asks
// no key: on Alpine, where sshd lacks CVE-2026-123158's fix, it is the
// openssh upgrade (sshchange.Upgrade; the maintainer, 2026-11-08). Every
// other row asks nothing: AppArmor, Backup and Environment.
func Asked(in Input, id string) Ask {
	cfg := in.Config
	switch id {
	case users.ID:
		if cfg.Username == "" {
			return Ask{}
		}
		return Ask{Control: users.Admin{Action: users.ActionCreate, New: users.New{Name: cfg.Username, Admin: true, Keys: cfg.SshKeys}, Getenv: in.getenv},
			Words: fmt.Sprintf("Create %s, %s", cfg.Username, keys(len(cfg.SshKeys)))}
	case ssh.Login{}.ID():
		if !cfg.DisableRootSSH {
			return Ask{}
		}
		words := "Root login off"
		if in.Facts.Platform.Distro == detect.DistroProxmox {
			words = keyOnly
		}
		return Ask{Control: sshchange.Disallow{Root: sshchange.Root{Want: "no", Getenv: in.getenv}}, Words: words}
	case ssh.Auth{}.ID():
		if in.Facts.Platform.Distro != detect.DistroAlpine {
			return Ask{}
		}
		return Ask{Control: sshchange.Upgrade{}, Words: sshchange.UpgradeTitle, Unfixed: true}
	case ssh.Port{}.ID():
		if cfg.SshPort <= 0 || cfg.SshPort > 65535 {
			return Ask{}
		}
		return Ask{Port: uint16(cfg.SshPort)}
	case fwctl.ID:
		if !cfg.EnableUfwSshPolicy {
			return Ask{}
		}
		return Ask{Control: fwctl.Incoming{Action: fwctl.ActionEnable}, Words: wordsEnableSSH}
	case dnsctl.ID:
		if !cfg.ConfigureDns || len(cfg.Nameservers) == 0 {
			return Ask{}
		}
		return Ask{Control: dnsctl.Nameservers{Config: cfg}, Words: fmt.Sprintf("Set %d nameservers", len(cfg.Nameservers))}
	case updates.ID:
		if !cfg.EnableUnattendedUpgrades {
			return Ask{}
		}
		return Ask{Control: updates.Auto{Action: updates.AutoOn, Records: in.Revert, Version: in.Version}, Words: "Turn on"}
	}
	return Ask{}
}

// left are the rows Run All never plans, and their words (decision 1 (a)).
var left = map[string]string{
	apparmor.ID:  WordsStatus,
	backupctl.ID: WordsUnchanged,
}

// keys is "1 key" or "n keys".
func keys(n int) string {
	if n == 1 {
		return "1 key"
	}
	return fmt.Sprintf("%d keys", n)
}

// Row is one control's row: its check, and what Run All does with it.
type Row struct {
	ID     string
	Title  string        // the control's (its check's)
	Report engine.Report // the control's check: the state the main menu maps
	Kind   Kind
	// Words are the row's outcome: the change, or why none.
	Words string
	// Refusal is a Refused or Pending row's why, in full: the control's
	// refusal (as the screens word one), the users read's error, or a
	// change journalled's (words). Err is the refusal itself.
	Refusal string
	Err     error
	// Control is a Planned row's control, as the plan has it.
	Control engine.Control
}

// Reading is the host read for Run All.
type Reading struct {
	Config *config.Config
	Facts  detect.Facts
	// Reports are each row's check, in Rows' order.
	Reports []engine.Report
	Rows    []Row
	// Busy: a change waits for hardn confirm, or its rollback has not
	// finished (revert.State.Busy), read once before any plan.
	Busy bool
	// Plan is the Planned rows' controls in one plan; nil where none is
	// planned or a change is pending.
	Plan *engine.Plan
	// Refusal is what refuses Apply all before asking, in full: a change
	// journalled (words), or the one plan's refusal. Err is the refusal
	// itself.
	Refusal string
	Err     error

	state revert.State // the revert state Busy was read from
}

// Planned are the rows in the plan.
func (r Reading) Planned() []Row { return r.of(Planned) }

// Refused are the rows whose control refused alone: each is said before
// the review ("<row> skipped: <Refusal>").
func (r Reading) Refused() []Row { return r.of(Refused) }

// Pending reports whether a change waits for hardn confirm.
func (r Reading) Pending() bool { return len(r.of(Pending)) > 0 }

// Lockout reports whether the plan is lockout-risk: confirmed or reverted
// from a new connection. The hostname is typed whatever the risk
// (decision 6 (a)).
func (r Reading) Lockout() bool { return r.Plan != nil && r.Plan.Risk >= engine.RiskLockout }

// Controls are the Planned rows' controls, in the rows' order: the plan's.
func (r Reading) Controls() []engine.Control {
	var out []engine.Control
	for _, rw := range r.Planned() {
		out = append(out, rw.Control)
	}
	return out
}

// Row is the row of control id.
func (r Reading) Row(id string) (Row, bool) {
	i := slices.IndexFunc(r.Rows, func(rw Row) bool { return rw.ID == id })
	if i < 0 {
		return Row{}, false
	}
	return r.Rows[i], true
}

func (r Reading) of(k Kind) []Row {
	var out []Row
	for _, rw := range r.Rows {
		if rw.Kind == k {
			out = append(out, rw)
		}
	}
	return out
}

// Read reads the host for Run All, writing nothing: the revert state first,
// then each row's check; each row hardn.yml asks of (Asked) planned alone,
// in the screen's order; root login off skipped where the firewall is
// planned too (decision 5 (a)); then the planned rows' controls in one
// plan, whose refusal refuses Apply all. Where a change is pending, every
// row asked of but the port and those Not Applicable is Pending and
// nothing is planned. The error is ctx's, the revert state's or the
// checks' (detection's): a control's refusal is its row's.
func Read(ctx context.Context, in Input) (Reading, error) {
	if in.System == nil || in.Config == nil || in.NewEngine == nil {
		return Reading{}, fmt.Errorf("runall: %w: Read needs a System, a Config and NewEngine", fs.ErrInvalid)
	}
	r := Reading{Config: in.Config, Facts: in.Facts}
	st, err := revert.ReadState(ctx, in.System, in.Revert)
	if err != nil {
		return Reading{}, fmt.Errorf("runall: the change journal: %w", err)
	}
	r.Busy, r.state = st.Busy(), st
	reports, err := newEngine(in).Check(ctx, Checked(in.Config, in.getenv))
	if err != nil {
		return Reading{}, fmt.Errorf("runall: %w", err)
	}
	for _, id := range IDs() {
		rep, _ := report(reports, id)
		r.Reports = append(r.Reports, rep)
		r.Rows = append(r.Rows, r.pick(ctx, in, id, rep))
	}
	r.afterUpgrade()
	r.afterFirewall()
	r.noSSHWords()
	for _, rw := range r.Rows {
		if rw.Kind == Pending {
			r.Refusal, r.Err = rw.Refusal, rw.Err
			return r, nil
		}
	}
	if err := r.combine(ctx, in); err != nil {
		return Reading{}, err
	}
	return r, nil
}

// combine plans the Planned rows' controls in one plan, kept in r.Plan;
// its first refusal refuses Apply all (r.Err, r.Refusal). Decision 5 (a)
// leaves the engine nothing it refuses on the gallery's hosts; the refusal
// stays, a guard for a conflict the rule does not cover. The error is the
// engine's.
func (r *Reading) combine(ctx context.Context, in Input) error {
	planned := r.Controls()
	if len(planned) == 0 {
		return nil
	}
	p, err := newEngine(in).Plan(ctx, planned, in.Config)
	if err != nil {
		return fmt.Errorf("runall: %w", err)
	}
	r.Plan = p
	if len(p.Refused) > 0 {
		r.Err = p.Refused[0]
		r.Refusal = words(r.Err)
	}
	return nil
}

// newEngine is the caller's engine, its progress unused: Read never
// applies.
func newEngine(in Input) *engine.Engine { return in.NewEngine(func(engine.Event) {}) }

// report is the report of control id.
func report(reports []engine.Report, id string) (engine.Report, bool) {
	i := slices.IndexFunc(reports, func(r engine.Report) bool { return r.Control == id })
	if i < 0 {
		return engine.Report{Control: id}, false
	}
	return reports[i], true
}

// pick is the row of control id: left alone; nothing asked (the openssh
// upgrade where sshd has the fix, or where that is not established); Not
// Applicable by its check; the port compared; pending where a change
// waits; else its control planned alone.
func (r Reading) pick(ctx context.Context, in Input, id string, rep engine.Report) Row {
	rw := Row{ID: id, Title: rep.Title, Report: rep}
	a := Asked(in, id)
	switch {
	case left[id] != "":
		rw.Kind, rw.Words = Left, left[id]
	case !a.Asked():
		rw.Kind, rw.Words = NothingAsked, WordsNothing
	case rep.Status.State == engine.NotApplicable:
		rw.Kind, rw.Words = NotApplicable, rep.Status.Detail
	case a.Unfixed && sshchange.ReadFix(ctx, in.System, in.Facts).Fixed != detect.No:
		rw.Kind, rw.Words = NothingAsked, WordsNothing
	case a.Control == nil:
		r.port(ctx, in, a.Port, &rw)
	case r.Busy:
		pending(&rw, &revert.PendingError{State: r.state})
	default:
		r.alone(ctx, in, a, &rw)
	}
	return rw
}

// pending makes rw a change journalled's.
func pending(rw *Row, err error) {
	rw.Kind, rw.Words, rw.Refusal, rw.Err = Pending, WordsPending, words(err), err
}

// refused makes rw refused by err.
func refused(rw *Row, err error) {
	rw.Kind, rw.Words, rw.Refusal, rw.Err = Refused, WordsRefused, words(err), err
}

// port is the SSH Port row: as asked where sshd listens on sshPort, else
// SSH Login's to change (decision 3); refused where sshd cannot be read
// (sshd -T's error, the reading's EffErr).
func (r Reading) port(ctx context.Context, in Input, want uint16, rw *Row) {
	sr, err := sshchange.Read(ctx, in.System, in.Facts, in.Config, in.Session, in.Getenv)
	if err == nil {
		err = sr.EffErr
	}
	switch {
	case err != nil:
		refused(rw, err)
	case slices.Contains(sr.Effective.Listeners.Ports(), want):
		rw.Kind, rw.Words = AsAsked, WordsAsked
	default:
		rw.Kind, rw.Words = Elsewhere, fmt.Sprintf("%s%d", sshLogin, want)
	}
}

// alone plans a's control alone, writing nothing: a user there
// (userThere), a refusal, nothing to change, or planned. A change pending
// is Busy, read first (pick); one journalled since is the engine's
// refusal, Refused in its words.
func (r Reading) alone(ctx context.Context, in Input, a Ask, rw *Row) {
	p, err := newEngine(in).Plan(ctx, []engine.Control{a.Control}, in.Config)
	if err == nil && len(p.Refused) > 0 {
		err = p.Refused[0]
	}
	switch {
	case errors.Is(err, users.ErrNameTaken):
		r.userThere(ctx, in, rw)
	case err != nil:
		refused(rw, err)
	case len(p.Changes) == 0:
		rw.Kind, rw.Words = AsAsked, WordsAsked
	default:
		rw.Kind, rw.Words, rw.Control = Planned, a.Words, a.Control
	}
}

// userThere is the User row where hardn.yml's username is taken: as asked
// where the users read (users.Read) has it an admin with every key
// hardn.yml asks for, else User Management's, with what is missing: Run
// All does not change a user that is there. Refused, the read's error its
// why, where the users cannot be read; a key that does not parse is
// missing.
func (r Reading) userThere(ctx context.Context, in Input, rw *Row) {
	rd, err := users.Read(ctx, in.System, in.Facts, in.Getenv)
	if err != nil {
		refused(rw, err)
		return
	}
	rw.Kind = Elsewhere
	i := slices.IndexFunc(rd.Users, func(u users.User) bool { return u.Name == in.Config.Username })
	if i < 0 {
		rw.Words = userMgmt + "name taken"
		return
	}
	u := rd.Users[i]
	var missing []string
	if u.Admin != detect.Yes {
		missing = append(missing, "not an admin")
	}
	n := 0
	for _, s := range in.Config.SshKeys {
		if k, err := users.ParseKey(s); err != nil || !slices.ContainsFunc(u.Keys, k.Same) {
			n++
		}
	}
	if n > 0 {
		missing = append(missing, keys(n)+" missing")
	}
	if len(missing) == 0 {
		rw.Kind, rw.Words = AsAsked, WordsAsked
		return
	}
	rw.Words = userMgmt + strings.Join(missing, ", ")
}

// The Firewall row's words when Enable is planned: SSH let through, or,
// where the SSH rows say the host has no SSH server, Enable alone.
const (
	wordsEnableSSH   = "Enable, SSH allowed"
	wordsEnableNoSSH = "Enable"
)

// noSSHWords words a planned Enable wordsEnableNoSSH where SSH Login's
// report is Not Applicable for want of an SSH server (ssh.DetailNoServer):
// its plan lets no SSH through (steps.PlanUFWEnable).
func (r *Reading) noSSHWords() {
	login, ok := report(r.Reports, ssh.Login{}.ID())
	if !ok || login.Status.State != engine.NotApplicable || login.Status.Detail != ssh.DetailNoServer {
		return
	}
	for i, rw := range r.Rows {
		if rw.ID == fwctl.ID && rw.Kind == Planned && rw.Words == wordsEnableSSH {
			r.Rows[i].Words = wordsEnableNoSSH
		}
	}
}

// afterUpgrade is decision 5 (a)'s rule for the openssh upgrade (the
// maintainer, 2026-11-08): it claims sshd, as Enable and root login off
// do, so where it is planned they are skipped, said on their rows, and so
// is a Create refused because sshd lacks the fix (users.ErrPasswordBypass),
// which the upgrade lifts; the next Run All plans them on the host as the
// upgrade left it.
func (r *Reading) afterUpgrade() {
	if !slices.ContainsFunc(r.Rows, func(rw Row) bool { return rw.ID == (ssh.Auth{}).ID() && rw.Kind == Planned }) {
		return
	}
	for i, rw := range r.Rows {
		switch {
		case rw.Kind == Planned && (rw.ID == fwctl.ID || rw.ID == (ssh.Login{}).ID()),
			rw.Kind == Refused && rw.ID == users.ID && errors.Is(rw.Err, users.ErrPasswordBypass):
			r.Rows[i].Kind, r.Rows[i].Words, r.Rows[i].Control, r.Rows[i].Refusal, r.Rows[i].Err = Later, WordsAfterUpgrade, nil, "", nil
		}
	}
}

// afterFirewall is decision 5 (a): where Enable and root login off both
// plan changes, the engine refuses them as one plan (engine.ErrConflict:
// each plans sshd and ufw from the host as it is now), so the firewall is
// planned and root login off skipped, said on its row; the next Run All
// plans it on the host as the firewall left it.
func (r *Reading) afterFirewall() {
	fw := slices.IndexFunc(r.Rows, func(rw Row) bool { return rw.ID == fwctl.ID && rw.Kind == Planned })
	login := slices.IndexFunc(r.Rows, func(rw Row) bool { return rw.ID == (ssh.Login{}).ID() && rw.Kind == Planned })
	if fw < 0 || login < 0 {
		return
	}
	rw := &r.Rows[login]
	rw.Kind, rw.Words, rw.Control = Later, WordsAfterFirewall, nil
}

// words are a refusal in the screens' words: PendingWords where a change
// waits for hardn confirm (revert.State.Waiting); revert's own for any
// other change journalled (an unfinished rollback, a change that did not
// finish), with its hardn rollback; else a control's refusal as screenkit.RefusalWords has
// it, its why and its fix each a sentence, or the error's own (the test
// holds the two the same).
func words(err error) string {
	var pe *revert.PendingError
	if errors.As(err, &pe) && pe.State.Waiting() {
		return PendingWords
	}
	var rf *steps.Refusal
	if !errors.As(err, &rf) || rf.Why == "" {
		return fullStop(err.Error())
	}
	if rf.Fix == "" {
		return fullStop(rf.Why)
	}
	first, n := utf8.DecodeRuneInString(rf.Fix)
	return fullStop(rf.Why) + " " + fullStop(string(unicode.ToUpper(first))+rf.Fix[n:])
}

// fullStop is s ending in one full stop, as screenkit.FullStop.
func fullStop(s string) string {
	return strings.TrimSuffix(strings.TrimSpace(s), ".") + "."
}
