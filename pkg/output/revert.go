package output

import (
	"errors"
	"time"

	"github.com/abbott/hardn/pkg/engine"
	"github.com/abbott/hardn/pkg/host/revert"
)

// The modes of hardn confirm and hardn rollback (<id>, --abandon, --boot):
// on the audit log's records alone (docs/reference/output.md §4.1); no
// document on stdout has them.
const (
	ModeConfirm  Mode = "confirm"  // hardn confirm: the confirmation itself
	ModeRollback Mode = "rollback" // hardn rollback <id>: a person's, or the revert timer's
	ModeAbandon  Mode = "abandon"  // hardn rollback --abandon <id>
	ModeBoot     Mode = "boot"     // hardn rollback --boot: the boot unit's
)

// The outcomes those modes add, each exit status 0.
const (
	OutcomeConfirmed Outcome = "confirmed" // the change is kept: it will not be rolled back
	OutcomeUndone    Outcome = "undone"    // the change was rolled back, every part of it
	OutcomeAbandoned Outcome = "abandoned" // the unfinished rollback was given up; the host stays as it left it
	OutcomeRearmed   Outcome = "rearmed"   // at boot: the revert timer runs again
)

// The sessions of Revert.Session.
const (
	SessionSSH     = "ssh"     // over SSH
	SessionConsole = "console" // a local console
	SessionUnknown = "unknown" // not established: the revert timer, the boot unit, tmux, run0
)

// Revert is what hardn confirm or hardn rollback (<id>, --abandon, --boot)
// did, and who ran it: Result.Revert, on those modes' records alone.
type Revert struct {
	// User ran the command: the user who ran hardn through sudo or doas,
	// root otherwise (a root login, the revert timer, the boot unit).
	User string `json:"user"`
	// Session is how they reached hardn: ssh, console, unknown; Client the
	// SSH connection's client end, address:port, "" when not read.
	Session string `json:"session"`
	Client  string `json:"client"`
	// Summary is the change's one line; "" when no change was read.
	Summary string `json:"summary"`
	// Reason is why a rollback ran: timeout, requested, failed; "" when
	// none ran.
	Reason string `json:"reason"`
	// Resumed: the rollback took up one that had not finished.
	Resumed bool `json:"resumed"`
	// Parts are the rollback's parts, in the order it runs them, each done
	// or not by its record; [] when none ran or was given up.
	Parts []RevertPart `json:"parts"`
	// Kept is where the record of a rollback given up is kept; "" otherwise.
	Kept string `json:"kept"`
	// BootAction and BootReason are --boot's codes (BootCodes); "" otherwise.
	BootAction string `json:"bootAction"`
	BootReason string `json:"bootReason"`
	// Detail says the rest in words, which may change: why there was
	// nothing to do, what --boot found.
	Detail string `json:"detail"`
}

// RevertPart is one part of a rollback.
type RevertPart struct {
	Kind string `json:"kind"` // restore, undo, reload
	// Index is the part's place among those of its kind, from 1.
	Index int `json:"index"`
	// Done: the rollback's record has it done. A part not done may have
	// run in part.
	Done bool `json:"done"`
	// What it is: the backup run's directory, the inverse step, the service.
	What string `json:"what"`
}

// RevertRun is one run of hardn confirm or hardn rollback (<id>,
// --abandon, --boot): the input of BuildRevert. One of Confirmed,
// Rollback, Abandoned and Boot is set, by Mode, unless Err ended the run
// before it had one.
type RevertRun struct {
	Mode         Mode
	HardnVersion string
	Hostname     string
	// ID is the change asked for (rollback <id>, --abandon <id>); "" takes
	// the result's.
	ID string
	// User ran it; Session is how, and SessionErr why that is not
	// established (revert.ReadSession).
	User       string
	Session    revert.Session
	SessionErr error
	Confirmed  *revert.ConfirmResult
	Rollback   *revert.RollbackResult
	Abandoned  *revert.AbandonResult
	Boot       *revert.BootResult
	Err        error
}

// NewRevertRecord is run r's audit record, at time at (UTC).
func NewRevertRecord(r RevertRun, at time.Time) Record {
	return Record{Time: at.UTC(), Document: BuildRevert(r)}
}

// BuildRevert maps run r to its document: the nine fields, the host its
// hostname alone, result.id the change's and result.revert the rest. It is
// pure.
func BuildRevert(r RevertRun) Document {
	d := Build(Run{Mode: r.Mode, HardnVersion: r.HardnVersion, Hostname: r.Hostname})
	rv := &Revert{User: r.User, Session: SessionUnknown, Parts: []RevertPart{}}
	if r.SessionErr == nil {
		rv.Session = SessionConsole
		if r.Session.Remote {
			rv.Session = SessionSSH
		}
	}
	if c, err := revert.ParseConn(r.Session.Conn); err == nil {
		rv.Client = c.Client.String()
	}
	id, step := r.ID, ""
	of := func(j revert.Journal) {
		if j.ID != "" {
			id, rv.Summary = j.ID, j.Summary
		}
	}
	rolledBack := func(rb revert.RollbackResult) {
		of(rb.Journal)
		rv.Reason, rv.Resumed, rv.Parts = string(rb.Reason), rb.Resumed, partsOf(rb.Journal)
		if rb.Nothing {
			rv.Detail = rb.Why
		}
	}
	o := OutcomeFailed
	switch {
	case r.Mode == ModeConfirm && r.Confirmed != nil:
		of(r.Confirmed.Journal)
		o = OutcomeConfirmed
	case r.Mode == ModeConfirm:
		var np *revert.NotPendingError
		if errors.As(r.Err, &np) && np.Reverted != nil {
			id, rv.Summary = np.Reverted.ID, np.Reverted.Summary
		}
		o = OutcomeRefused
	case r.Mode == ModeRollback && r.Rollback != nil:
		rolledBack(*r.Rollback)
		step = "rollback"
		o = OutcomeUndone
		if r.Rollback.Nothing {
			o = OutcomeNothingToDo
		}
	case r.Mode == ModeAbandon && r.Abandoned != nil:
		of(r.Abandoned.Journal)
		rv.Kept, rv.Parts = r.Abandoned.Kept, partsOf(r.Abandoned.Journal)
		if p := r.Abandoned.Journal.Rollback; p != nil {
			rv.Reason = string(p.Reason)
		}
		o = OutcomeAbandoned
	case r.Mode == ModeAbandon:
		o = OutcomeRefused
	case r.Mode == ModeBoot && r.Boot != nil:
		b := *r.Boot
		of(b.Change)
		rv.BootAction, rv.BootReason = BootCodes(b.Action, b.Reason)
		rv.Detail = b.Why
		if b.Rollback != nil {
			rolledBack(*b.Rollback)
			rv.Detail, step = b.Why, "rollback"
		}
		switch b.Action {
		case revert.BootNothing:
			o = OutcomeNothingToDo
		case revert.BootRearmed:
			o = OutcomeRearmed
		case revert.BootRolledBack:
			o = OutcomeUndone
		case revert.BootLeft:
			o = OutcomeRefused
		}
	}
	if err := r.Err; err != nil {
		e := errorOf(err, "", "")
		e.Step = step
		d.Errors = append(d.Errors, e)
		switch {
		case exitOf(e.Code) == ExitUsage:
			o = OutcomeUsage
		case errors.Is(err, revert.ErrIncomplete):
			o = OutcomeRollbackIncomplete
		case r.Mode == ModeRollback || r.Mode == ModeBoot:
			o = OutcomeFailed
		}
	}
	d.Result = Result{Outcome: o, ExitCode: o.Exit(), ID: id, Revert: rv}
	return d
}

// partsOf are the parts of j's rollback, done or not by its record
// (j.Rollback), as revert.Unfinished lists them: the backups' restore,
// each inverse step, each reload. With no record, none is done.
func partsOf(j revert.Journal) []RevertPart {
	p := revert.Progress{}
	if j.Rollback != nil {
		p = *j.Rollback
	}
	out := []RevertPart{}
	if b := j.Backup; b != nil {
		out = append(out, RevertPart{Kind: "restore", Index: 1, Done: p.Restored, What: b.Dir})
	}
	for i, s := range j.Undo {
		out = append(out, RevertPart{Kind: "undo", Index: i + 1, Done: i < p.UndoDone, What: undoWords(engine.Undo{Steps: []revert.Step{s}})[0]})
	}
	for i, rl := range j.Reload {
		out = append(out, RevertPart{Kind: "reload", Index: i + 1, Done: i < p.ReloadDone, What: string(rl.Service)})
	}
	return out
}
