package engine

import (
	"context"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/backup"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/revert"
)

// RiskClass is what a change risks. A plan's is its highest change's.
type RiskClass uint8

// The risk classes, lowest first.
const (
	// RiskPlain: none of the below (a resolver drop-in, a cron file).
	RiskPlain RiskClass = iota
	// RiskPackageInstall: installs or removes a package.
	RiskPackageInstall
	// RiskServiceRestart: restarts or reloads a service.
	RiskServiceRestart
	// RiskLockout: could lock the user out: SSH, the firewall, sudo
	// (brief 2.4). A plan with one is applied under confirm-or-revert:
	// the revert timer is armed first, and only `hardn confirm` from a new
	// connection keeps it. Every other class is journalled without a timer
	// and rolled back on failure only (revert.RiskOther).
	RiskLockout
)

var riskWords = [...]string{"plain", "package-install", "service-restart", "lockout"}

func (r RiskClass) String() string {
	if int(r) < len(riskWords) {
		return riskWords[r]
	}
	return fmt.Sprintf("risk(%d)", uint8(r))
}

// MarshalText writes the class's word.
func (r RiskClass) MarshalText() ([]byte, error) {
	if int(r) >= len(riskWords) {
		return nil, fmt.Errorf("engine: risk class %d", uint8(r))
	}
	return []byte(riskWords[r]), nil
}

// Stage is where a change runs in an apply: brief 2.3's ordering rule as
// stages. Every change of a stage runs before any change of a later one;
// within a stage, Change.After orders them, then the plan's order. Around
// the stages: everything that validates and checks runs at plan time,
// before anything changes; the revert timer is armed before the first
// stage; MarkApplied (or Done) is the apply's last write.
type Stage uint8

// The stages, in the order they run.
const (
	// StagePackages: packages a later change needs (ufw, apk-cron).
	StagePackages Stage = iota + 1
	// StageAccounts: the user and the key in place, ~/.ssh 0700 and
	// authorized_keys 0600 (brief 2.3 step 1).
	StageAccounts
	// StageFirewall: the new port allowed, the old one kept, before sshd
	// moves (brief 2.3 step 2).
	StageFirewall
	// StageFiles: configuration writes, each after the engine has backed
	// up the change's files (brief 2.3 step 3; §3.16).
	StageFiles
	// StageReload: reloads that make the written files take effect, never
	// a restart where a reload does (brief 2.3 steps 3 and 4).
	StageReload
	// StageVerify: checks of the effective values after the reloads (sshd
	// -T, the listeners); a check that fails rolls the change back.
	StageVerify
)

var stageWords = [...]string{"", "packages", "accounts", "firewall", "files", "reload", "verify"}

func (s Stage) String() string {
	if s >= StagePackages && int(s) < len(stageWords) {
		return stageWords[s]
	}
	return fmt.Sprintf("stage(%d)", uint8(s))
}

// MarshalText writes the stage's word.
func (s Stage) MarshalText() ([]byte, error) {
	if s < StagePackages || int(s) >= len(stageWords) {
		return nil, fmt.Errorf("engine: stage %d", uint8(s))
	}
	return []byte(stageWords[s]), nil
}

// Undo is a change's undo as the write-ahead journal holds it
// (revert.Journal), in revert's own terms, so that a rollback in another
// process (the timer's `hardn rollback <id>`) does exactly what the
// in-process one does: both are revert.Rollback over the journal.
type Undo struct {
	// Steps are inverse steps, in the order to run them: ufw's undo
	// commands (revert.OpRun), a mode, owner or created directory put
	// back (OpChmod, OpChown, OpRemoveDir), or a unit's kept plan
	// (OpPlan, built with the unit's step function in package plans,
	// which checks the plan first). Each is harmless when the step it
	// undoes never ran: it is journalled before.
	Steps []revert.Step
	// Reloads are services a rollback reloads once the files are back:
	// ssh with the change's keywords, IfUp until the forward reload's
	// gated step has run.
	Reloads []revert.Reload
}

// Change is one step of a plan. A control makes it; the engine orders it,
// backs up its files, journals its undo and runs Apply.
//
// The journal is not optional. Before Apply runs, the engine journals what
// Rollback says (with the backups of Backups), so a rollback at any moment
// (this process after a failure or Ctrl-C, or the timer's after hardn died)
// covers the step. After Apply returns, error or not, the engine journals
// what Ran.Rollback says the step that ran needs instead (ufw's Report.Undo
// for its PreUndo; the ssh reload without IfUp once its gated step ran),
// under a context without the apply's cancellation and with a bound
// (Options.JournalTimeout). There is no in-process undo besides the
// journal's: revert.Rollback runs it, here and from the timer alike.
type Change struct {
	// ID is unique in the plan: lower case letters, digits and ".-_/"
	// ("ssh.port/firewall"). After names changes by it.
	ID string
	// Control is the control that planned it; the engine sets it.
	Control string
	// Summary is one line: what the change does ("Set Port 2230 in
	// /etc/ssh/sshd_config.d/00-hardn.conf").
	Summary string
	// Target is what the change acts on, for the plan's view: the file
	// Diff changes, or a service or tool ("ufw", "ssh.service"); "" for
	// none.
	Target string
	// Diff is the unified diff for a file (FileDiff), before and after for
	// a command (CommandDiff); "" when there is nothing to show.
	Diff string
	// StateBefore and StateAfter are a setting's or service's state before
	// and after a change that has no file to diff ("inactive", "active"),
	// as the plan shows them (component.PlanChange's Before and After); ""
	// for none.
	StateBefore, StateAfter string
	// Commands are the commands the change runs, in order, as the plan
	// shows them ("systemctl reload ssh").
	Commands []string
	Risk     RiskClass
	Stage    Stage
	// After are the changes that must run before this one, in its stage
	// or an earlier one; one in a later stage, or one no control planned,
	// is ErrOrder at plan time.
	After []string
	// Backups are the files the change writes or removes: the engine
	// backs each up (backup.RollbackRequired) and journals it before
	// Apply runs, so the rollback restores it. Two changes of one plan
	// that back up the same file are ErrConflict.
	Backups []string
	// Resources name what the change's plan read of the host, and what
	// its apply changes, that another control's change in the same plan
	// could change under it: "sshd" (sshd's files and listeners), "ufw"
	// (its rules, policies and state), a unit's name. Two controls' changes
	// in one plan that name the same resource are ErrConflict, at plan
	// time: the controls must feed one planner, or go in separate plans.
	// The changes of one control may share them (next/p3-steps).
	Resources []string
	// Rollback is the undo the journal holds before Apply runs.
	Rollback Undo
	// Findings are what the planner found that the user should know about
	// the change, which do not refuse it: a value Proxmox VE's rules
	// rewrote, a line another file sets first, a listener ufw would not
	// let through once it starts. p3-output renders them, as it renders a
	// check's (Status.Findings); their codes are in its registry
	// (next/p3-steps).
	Findings []Finding
	// ConfirmVia, for a lockout-risk change that adds listeners
	// (reload.Plan.Adds), is them: a confirming connection must reach one
	// (revert.Journal.ConfirmVia).
	ConfirmVia []revert.Listener
	// Apply makes the change. An error is a failed step: the engine rolls
	// back every change it started, this one included. In a lockout-risk
	// plan, ctx ends Options.Margin before the revert timer fires
	// (ErrWindow), so that the rollback here claims the change first.
	Apply func(ctx context.Context, sys host.System, run Run) (Ran, error) `json:"-"`
}

// Run is what Apply is given.
type Run struct {
	ID    string // the plan's id
	Dir   string // the plan's run directory (Input.RunDir)
	Facts detect.Facts
	// Backups are this change's backups, by path, as the engine took them
	// (a write can check that the file is still as the backup shows it).
	Backups map[string]backup.Entry
	// Store is the plan's backup run, <backupPath>/<id>/, whose entries the
	// engine takes for Backups and the journal names. A unit that takes
	// backups of its own (dns.Apply, alpine.Apply, migrate.Apply and
	// pve.ApplyKeys take a *backup.Run) takes them here, so that one run
	// directory holds the change's backups; what a unit backs up itself is
	// not journalled: its undo is its plan step or the engine's backup of
	// the same file, as p2drive does it (vmtest.md §2).
	Store *backup.Run
}

// Ran is what an Apply that ran says about its undo.
type Ran struct {
	// Rollback, when set, replaces the change's undo in the journal: what
	// the step that ran needs undone, which may differ from what was
	// journalled before it (ufw's Report.Undo, the reload's IfUp
	// cleared). An empty Undo removes it: an apply refused before it
	// wrote anything, or a unit that undid its own writes, whose
	// rollback must not run (revert-plans.md §6). Set it whether or not
	// Apply returns an error.
	Rollback *Undo
	// Detail is one line for the result ("firewall: verified").
	Detail string
}

// validName reports whether s can name a control or a change.
func validName(s string) bool {
	if s == "" || len(s) > 128 || !isAlnum(rune(s[0])) {
		return false
	}
	for _, c := range s {
		if !isAlnum(c) && !strings.ContainsRune(".-_/", c) {
			return false
		}
	}
	return true
}

func isAlnum(c rune) bool { return c >= 'a' && c <= 'z' || c >= '0' && c <= '9' }

// check refuses, with ErrChange, a change the engine could not order,
// back up or journal, before anything changes: every plan step's unit must
// have a rollback in plans. Its undo is checked with the changes before it,
// once the plan is in order (checkJournals).
func (c Change) check(plans map[string]revert.PlanRollback) error {
	bad := func(format string, args ...any) error {
		return fmt.Errorf("engine: change %q: %s: %w", c.ID, fmt.Sprintf(format, args...), ErrChange)
	}
	switch {
	case !validName(c.ID):
		return bad("id")
	case c.Summary == "" || strings.ContainsAny(c.Summary, "\r\n"):
		return bad("summary must be one line")
	case c.Risk > RiskLockout:
		return bad("risk %v", c.Risk)
	case c.Stage < StagePackages || c.Stage > StageVerify:
		return bad("stage %v", c.Stage)
	case c.Apply == nil:
		return bad("no Apply")
	case len(c.ConfirmVia) > 0 && c.Risk != RiskLockout:
		return bad("confirmVia for a change with no lockout risk")
	}
	for i, a := range c.After {
		if a == c.ID || !validName(a) || slices.Contains(c.After[:i], a) {
			return bad("after %q: another change, once", a)
		}
	}
	for i, p := range c.Backups {
		if !filepath.IsAbs(p) || filepath.Clean(p) != p || slices.Contains(c.Backups[:i], p) {
			return bad("backup %q: absolute, clean, once", p)
		}
	}
	for i, r := range c.Resources {
		if r == "" || strings.ContainsAny(r, " \t\r\n") || slices.Contains(c.Resources[:i], r) {
			return bad("resource %q: a word, once", r)
		}
	}
	for _, s := range c.Rollback.Steps {
		if s.Op == revert.OpPlan && plans[s.Unit] == nil {
			return fmt.Errorf("engine: change %q: plan step of unit %q: %w: %w", c.ID, s.Unit, revert.ErrNoPlanRollback, ErrChange)
		}
	}
	return nil
}

// checkJournals refuses, with ErrChange, a change whose undo revert.Record
// would refuse when the engine journals it: the journal as record writes it
// before that change, with every earlier change's undo and backups (a
// reload's socketDropIns are restored from an earlier change's backup, for
// one), under the plan's id. It returns the change refused, or -1.
func checkJournals(id string, cs []Change) (int, error) {
	var undos []Undo
	seqs := 0
	for i, c := range cs {
		undos = append(undos, c.Rollback)
		seqs += len(c.Backups)
		j := revert.Journal{SchemaVersion: revert.SchemaVersion, ID: id, Created: time.Unix(1, 0).UTC(),
			Risk: revert.RiskOther, Summary: c.Summary}
		if seqs > 0 {
			j.Backup = &revert.Backups{Dir: "/", Seqs: make([]int, seqs)}
			for k := range j.Backup.Seqs {
				j.Backup.Seqs[k] = k + 1
			}
		}
		j.Undo, j.Reload = journalUndo(undos)
		if err := j.Check(); err != nil {
			return i, fmt.Errorf("engine: change %q: its undo, with the changes before it: %w: %w", c.ID, err, ErrChange)
		}
	}
	return -1, nil
}

// journalUndo is the journal's undo for the changes started, given in the
// order they started: the last change's steps first, each change's in its
// own order, and their reloads the same way, except that a reload an entry
// before it already names (the same service, keywords, listeners and
// socket drop-ins) is journalled once, IfUp only when every one of them
// says IfUp. A change that writes sshd's files journals the ssh reload
// with IfUp before its write, so that a rollback after the write reloads
// an sshd that started in between (revert.md §7; p2drive journals it so,
// vmtest.md §2.6), and the reload change that follows journals the same
// reload, without IfUp once its gated step ran: the rollback reloads once,
// and starts the unit when the forward reload ran (next/p3-steps).
func journalUndo(undos []Undo) (steps []revert.Step, reloads []revert.Reload) {
	for i := len(undos) - 1; i >= 0; i-- {
		steps = append(steps, undos[i].Steps...)
		for _, r := range undos[i].Reloads {
			if k := slices.IndexFunc(reloads, func(x revert.Reload) bool { return sameReload(x, r) }); k >= 0 {
				reloads[k].IfUp = reloads[k].IfUp && r.IfUp
				continue
			}
			reloads = append(reloads, r)
		}
	}
	return steps, reloads
}

// sameReload reports whether a and b reload the same thing, IfUp aside.
func sameReload(a, b revert.Reload) bool {
	return a.Service == b.Service && slices.Equal(a.Keywords, b.Keywords) && slices.Equal(a.After, b.After) &&
		slices.Equal(a.SocketDropIns, b.SocketDropIns)
}
