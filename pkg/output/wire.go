package output

import "time"

// SchemaVersion is the version of the JSON document's schema
// (docs/reference/output.md). A field may be added, and a new value given
// to a field documented as open, without changing it; anything that would
// break a script reading version 1 (a field removed or renamed, its type
// or meaning changed, a code changed or removed, an exit status's meaning
// changed) is a new version.
const SchemaVersion = 1

// Document is the JSON document of one run, written to stdout as one
// value (brief Phase 3, the JSON contract), and the body of each line of
// the audit log (Record). Arrays are never null: an empty one is [].
type Document struct {
	SchemaVersion int       `json:"schemaVersion"`
	HardnVersion  string    `json:"hardnVersion"`
	Host          Host      `json:"host"`
	Mode          Mode      `json:"mode"`
	Result        Result    `json:"result"`
	Changes       []Change  `json:"changes"`
	Status        []Status  `json:"status"`
	Findings      []Finding `json:"findings"`
	Errors        []Error   `json:"errors"`
}

// Mode is what the run was asked to do.
type Mode string

// The modes.
const (
	ModeCheck Mode = "check" // read each control's state; change nothing
	ModePlan  Mode = "plan"  // plan the changes; change nothing (the dry run)
	ModeApply Mode = "apply" // plan, then apply
)

// Host is the host the run read, in this schema's own words: detect's
// facts are mapped, never written as they are (detect.md §6). A fact not
// established is "unknown".
type Host struct {
	Hostname  string    `json:"hostname"` // "" when not read
	OS        OS        `json:"os"`
	Platform  Platform  `json:"platform"`
	Init      string    `json:"init"` // systemd, openrc, unknown
	SSH       string    `json:"ssh"`  // how sshd is started: ssh.socket, ssh.service, openrc, unknown
	Proxmox   Proxmox   `json:"proxmox"`
	Container Container `json:"container"`
	Elevation Elevation `json:"elevation"`
}

// OS is /etc/os-release's fields, as written there.
type OS struct {
	ID         string `json:"id"`
	VersionID  string `json:"versionId"`
	Codename   string `json:"codename"`
	PrettyName string `json:"prettyName"`
}

// Platform is the support matrix's view of the host.
type Platform struct {
	Distro  string `json:"distro"`  // debian, ubuntu, alpine, proxmox, other, unknown
	Version string `json:"version"` // the matrix's key, such as 13 or 3.24; "" when not established
	Support string `json:"support"` // supported, best-effort, out-of-scope, unlisted, unknown
}

// Proxmox is whether the host runs Proxmox VE.
type Proxmox struct {
	Installed string `json:"installed"` // yes, no, unknown
	Version   string `json:"version"`
}

// Container is whether hardn runs in a container.
type Container struct {
	Detected string `json:"detected"` // yes, no, unknown
	Manager  string `json:"manager"`
}

// Elevation is the elevation tools installed.
type Elevation struct {
	Sudo       string `json:"sudo"`       // yes, no, unknown
	SudoFlavor string `json:"sudoFlavor"` // sudo, sudo-rs, unknown
	Doas       string `json:"doas"`       // yes, no, unknown
}

// Result is how the run ended.
type Result struct {
	Outcome Outcome `json:"outcome"`
	// ExitCode is the run's exit status (ExitCode), so that a reader of
	// the audit log sees it too.
	ExitCode int `json:"exitCode"`
	// ID is the change's id (its journal's, its backups' and its timer's)
	// once a plan has one; "" otherwise.
	ID string `json:"id"`
	// Risk is the plan's risk class: plain, package-install,
	// service-restart, lockout; "" with no plan.
	Risk string `json:"risk"`
	// Confirm is set while a lockout-risk change waits for confirmation.
	Confirm *Confirm `json:"confirm,omitempty"`
	// Rollback is set when an apply stopped after the revert timer was
	// armed: what rolling it back did.
	Rollback *Rollback `json:"rollback,omitempty"`
	// Boot is set when the apply installed hardn's boot unit, before the
	// change was armed: it is never rolled back.
	Boot *Boot `json:"boot,omitempty"`
}

// Outcome is a run's result in one word.
type Outcome string

// The outcomes, each with its exit status (ExitCode).
const (
	OutcomeNothingToDo        Outcome = "nothing-to-do"         // 0: everything as asked; a plan with no changes
	OutcomeApplied            Outcome = "applied"               // 0: every change applied, none waiting
	OutcomeAwaiting           Outcome = "awaiting-confirmation" // 0: applied, and rolled back at the deadline unless confirmed
	OutcomeChangesNeeded      Outcome = "changes-needed"        // 3: a check or plan found changes to make, or the user declined them
	OutcomeRefused            Outcome = "refused"               // 1: the plan was refused; nothing changed
	OutcomeFailed             Outcome = "failed"                // 1: an error before anything was armed; nothing journalled
	OutcomeRolledBack         Outcome = "rolled-back"           // 1: the apply failed and was rolled back
	OutcomeRollbackIncomplete Outcome = "rollback-incomplete"   // 1: the apply failed, and its rollback did not finish here
	OutcomeUsage              Outcome = "usage-error"           // 2: bad flags or configuration
	OutcomeCancelled          Outcome = "cancelled"             // 130: interrupted; what was applied was rolled back
)

// Confirm is how a change waiting for confirmation is kept or undone.
type Confirm struct {
	// Deadline is when the revert timer fires, in UTC (RFC 3339).
	Deadline time.Time `json:"deadline"`
	// Approximate: the deadline is the journal's, and the timer fires a
	// few seconds after it (its own trigger time could not be read).
	Approximate bool `json:"approximate"`
	// Via are the listeners a confirming connection must reach, as
	// address:port; empty when the change adds none.
	Via []string `json:"via"`
	// Command keeps the change, run from a new SSH connection; Undo undoes
	// it now.
	Command string `json:"command"`
	Undo    string `json:"undo"`
}

// Rollback is what rolling back a failed apply did.
type Rollback struct {
	// Finished: the rollback in this run finished and undid every change
	// started. False when it did not finish (Command finishes it), when
	// another process has it, or when a change ran that the journal did
	// not cover (Unjournalled).
	Finished bool `json:"finished"`
	// ByAnotherProcess: another process's rollback (the timer's, a
	// `hardn rollback <id>`) had taken the change; this run undid nothing.
	ByAnotherProcess bool `json:"byAnotherProcess"`
	// Step and Change are where the apply stopped: start, window, backup,
	// journal, apply, journal-after, mark-applied, done; and the change
	// that failed, "" for the apply's last write.
	Step   string `json:"step"`
	Change string `json:"change"`
	// Restored are the files the rollback put back, Undone its inverse
	// steps, in the order run.
	Restored []string `json:"restored"`
	Undone   []string `json:"undone"`
	// Unjournalled names a change that ran whose undo the journal did not
	// hold, with what would undo it; "" when none.
	Unjournalled string `json:"unjournalled"`
	// Command finishes an unfinished rollback; "" when there is none to
	// finish.
	Command string `json:"command"`
}

// Boot is what the install of hardn's boot unit did.
type Boot struct {
	Installed bool   `json:"installed"` // the install finished
	Detail    string `json:"detail"`
}

// Change is one change of the plan, in the order the apply runs them.
type Change struct {
	ID          string   `json:"id"`
	Control     string   `json:"control"` // "" for hardn's own (the boot unit's)
	Summary     string   `json:"summary"`
	Target      string   `json:"target"`
	Risk        string   `json:"risk"`  // plain, package-install, service-restart, lockout
	Stage       string   `json:"stage"` // boot, packages, accounts, firewall, files, reload, verify
	After       []string `json:"after"` // the changes it runs after
	Diff        string   `json:"diff"`  // a unified diff; "" for none
	StateBefore string   `json:"stateBefore"`
	StateAfter  string   `json:"stateAfter"`
	Commands    []string `json:"commands"`
	Backups     []string `json:"backups"` // the files backed up before it runs
	Status      string   `json:"status"`  // planned, applied, failed, rolled-back, not-run, unjournalled
	Detail      string   `json:"detail"`  // what it did, or why it failed
}

// The change statuses.
const (
	StatusPlanned      = "planned"      // a check's or plan's: not applied
	StatusApplied      = "applied"      // applied, and not rolled back
	StatusFailed       = "failed"       // the change that failed (Result.Rollback says what was undone)
	StatusRolledBack   = "rolled-back"  // applied, then undone by the rollback in this run
	StatusNotRun       = "not-run"      // the apply stopped before it
	StatusUnjournalled = "unjournalled" // ran, and the journal did not hold its undo: what it did may be left
)

// Status is one control's state, as a check read it.
type Status struct {
	Control  string   `json:"control"`
	Title    string   `json:"title"`
	State    string   `json:"state"` // configured, not-configured, partial, not-applicable, unknown
	Detail   string   `json:"detail"`
	Evidence []string `json:"evidence"`
}

// Finding is something the user should know that does not stop anything.
type Finding struct {
	Code    string `json:"code"`
	Detail  string `json:"detail"`
	Source  string `json:"source"`  // a file and line, a command; "" when not known
	Control string `json:"control"` // the control whose check or plan found it
	Change  string `json:"change"`  // the change it rides on; "" for a check's
}

// Error is one error of the run.
type Error struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Control string `json:"control"` // the control it is about; "" for none
	Change  string `json:"change"`  // the change it is about; "" for none
	Step    string `json:"step"`    // the apply's step, as Rollback.Step; "rollback" for a rollback's error; "" otherwise
	// Why and Fix are a refusal's report: why the host is refused, and
	// what makes it one hardn can change (steps.Refusal); "" otherwise.
	Why string `json:"why"`
	Fix string `json:"fix"`
}

// Record is one line of the audit log: when the run ended, and its
// document.
type Record struct {
	Time time.Time `json:"time"`
	Document
}
