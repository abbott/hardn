package revert

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/netip"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/abbott/hardn/pkg/host/backup"
	"github.com/abbott/hardn/pkg/host/reload"
	"github.com/abbott/hardn/pkg/host/sshd"
)

// Risk is a journalled change's risk class.
type Risk string

const (
	// RiskLockout: an SSH or firewall change (brief 2.4). The timer is
	// armed before the first step, and only a confirmation from a new
	// connection keeps the change.
	RiskLockout Risk = "lockout"
	// RiskOther: no timer; it rolls back on failure only (brief 2.4). It
	// is journalled so that an apply hardn did not finish can be undone
	// (Done removes it once the apply succeeds).
	RiskOther Risk = "other"
)

// Reason is why a rollback ran.
type Reason string

const (
	ReasonTimeout   Reason = "timeout"   // not confirmed by the deadline
	ReasonRequested Reason = "requested" // hardn rollback <id> before the deadline, or of a change hardn did not finish
	ReasonFailed    Reason = "failed"    // the engine: a step failed while the session was open
)

// Listener is an address and port in the journal (sshd.Listener's fields).
// CheckConfirm takes an unspecified address (0.0.0.0, ::) as any address of
// either family: a dual-stack socket (ssh.socket's [::]:2230) takes IPv4
// connections as IPv4-mapped addresses, which ParseConn unmaps.
type Listener struct {
	Addr    netip.Addr `json:"addr"`
	Port    uint16     `json:"port"`
	RDomain string     `json:"rdomain,omitempty"`
}

// ListenersOf converts sshd's listeners (reload.Plan.Adds, for ConfirmVia).
func ListenersOf(ls []sshd.Listener) []Listener {
	out := make([]Listener, len(ls))
	for i, l := range ls {
		out[i] = Listener{Addr: l.Addr, Port: l.Port, RDomain: l.RDomain}
	}
	return out
}

func (l Listener) sshd() sshd.Listener {
	return sshd.Listener{Addr: l.Addr, Port: l.Port, RDomain: l.RDomain}
}

func (l Listener) String() string { return netip.AddrPortFrom(l.Addr, l.Port).String() }

// Backups names the backup entries a rollback restores: the backup run's
// directory (backup.Manifest.Dir) and the Seq of each entry the change took.
type Backups struct {
	Dir  string `json:"dir"`
	Seqs []int  `json:"seqs"`
}

// StepOp is what an inverse Step does.
type StepOp string

const (
	OpRun       StepOp = "run"   // run Argv (ufw's Report.Undo, one command each)
	OpChmod     StepOp = "chmod" // put Mode back on Path (a directory the change chmodded)
	OpChown     StepOp = "chown" // put Owner back on Path
	OpRemoveDir StepOp = "rmdir" // remove the empty directory the change created at Path
	// OpPlan hands Plan, a unit's kept plan, to the rollback Options.Plans
	// registers for Unit: a unit whose own rollback checks what it undoes
	// (sudoers, DNS, Alpine) is journalled this way, not as backups.
	OpPlan StepOp = "plan"
)

// Owner is a uid and gid; -1 leaves one unchanged.
type Owner struct {
	UID int `json:"uid"`
	GID int `json:"gid"`
}

// Step is one inverse step. Steps run in the journal's order, which the
// engine writes as the order to undo in (last change first), after the
// backups are restored. Each is journalled before the step it undoes takes
// effect, so it must be harmless when that step never ran: a chmod or chown
// back to what is there, an rmdir of what is not, ufw's PreUndo.
type Step struct {
	Op    StepOp          `json:"op"`
	Argv  []string        `json:"argv,omitempty"`
	Path  string          `json:"path,omitempty"`
	Mode  *backup.Perm    `json:"mode,omitempty"`
	Owner *Owner          `json:"owner,omitempty"`
	Unit  string          `json:"unit,omitempty"` // OpPlan's
	Plan  json.RawMessage `json:"plan,omitempty"` // OpPlan's
	Why   string          `json:"why,omitempty"`  // one line for the log
}

// ReloadService is a service a rollback reloads.
type ReloadService string

// ReloadSSH: sshd, through p2-reload's PlanFor with Rollback set, on the
// init system detected at rollback time.
const ReloadSSH ReloadService = "ssh"

// Reload is a service to reload once the backups are restored: for ssh,
// reload.Change's Keywords and After (what the restored files configure).
// It is journalled in two steps around the forward reload. Before it, with
// IfUp: the forward reload may run or not, so the rollback reloads only a
// unit that is up then, and never starts one, which may have been down
// before the change. Once the forward reload's gated step ran
// (reload.Report.Steps), again without IfUp: the unit was up, so the
// rollback starts it if the change left it down (reload.Change Rollback).
// A forward step that found the unit down (NotUp) leaves IfUp: the unit
// reads the restored files when it next starts.
type Reload struct {
	Service  ReloadService `json:"service"`
	Keywords []string      `json:"keywords"`
	After    []Listener    `json:"after,omitempty"`
	IfUp     bool          `json:"ifUp,omitempty"`
	// SocketDropIns are ssh.socket drop-ins the change removed and its
	// backups put back (v0.3.x's listen.conf, which a migration removes):
	// their ListenStream lines decide the socket's listeners again, so the
	// reload expects them (reload.Change.RestoredDropIns), and After is
	// then the listeners the socket had before the change. A listener
	// reload's alone, and only with Backup: the rollback refuses one that
	// is not the path of a journalled backup of a file, before the reload.
	SocketDropIns []string `json:"socketDropIns,omitempty"`
}

// ProcStart names one process for as long as it runs: its pid and its
// start time in clock ticks after boot (/proc/<pid>/stat fields 1 and 22).
// Two processes that ran at different times under one pid started at
// different times. Boot, when set, is the boot's id
// (/proc/sys/kernel/random/boot_id), for a record that may outlive a
// reboot.
type ProcStart struct {
	PID   int    `json:"pid"`
	Start uint64 `json:"start"`
	Boot  string `json:"boot,omitempty"`
}

func (p ProcStart) known() bool { return p.PID > 0 }

// same reports whether p and q name one process, ignoring Boot.
func (p ProcStart) same(q ProcStart) bool { return p.PID == q.PID && p.Start == q.Start }

// Applied is the end of an apply (MarkApplied): when, and the sshd
// processes then running, the server ends of every SSH connection made
// before the change took effect. A connection whose sshd process is one of
// them cannot confirm the change (CheckConfirm).
type Applied struct {
	At   time.Time   `json:"at"`
	SSHD []ProcStart `json:"sshd"`
}

// Progress is a started rollback's record, in rollback.json: a rollback
// that stops part-way resumes after what it had done. Runner is the process
// running it: another rollback does not take it up while that process
// lives.
type Progress struct {
	Started    time.Time `json:"started"`
	Reason     Reason    `json:"reason"`
	Restored   bool      `json:"restored"`
	UndoDone   int       `json:"undoDone"`
	ReloadDone int       `json:"reloadDone"`
	Runner     ProcStart `json:"runner,omitzero"`
}

// Journal is one change, as the engine records it for a rollback in another
// process. The engine fills ID, Risk, TimeoutSeconds, Summary, Session and
// ConfirmVia; Arm fills SchemaVersion, Created and Timer. Then the engine
// adds Backup, Undo and Reload with Record, write-ahead: each entry before
// the step it covers takes effect (a backup's Seq once the backup is taken
// and before the file is written; an inverse step before its step; a
// Reload with IfUp before the forward reload), so a rollback that runs at
// any moment, or after hardn died, covers every step that may have run.
// MarkApplied sets Applied after the last step.
type Journal struct {
	SchemaVersion int       `json:"schemaVersion"`
	ID            string    `json:"id"`
	Created       time.Time `json:"created"`
	// TimeoutSeconds is the confirm window (ResolveTimeout), at least
	// MinTimeout for RiskLockout; 0 for RiskOther.
	TimeoutSeconds int    `json:"timeoutSeconds"`
	Risk           Risk   `json:"risk"`
	Summary        string `json:"summary"` // one line: what the change does
	// Session is the session that applied the change (ReadSession).
	Session Session `json:"session"`
	// ConfirmVia, when set, is the listeners a confirming SSH connection
	// must reach: those the change adds (reload.Plan.Adds). A connection
	// over a held port proves nothing about the new one, and the clean-up
	// after the confirmation drops the port that worked (reload.md §6).
	ConfirmVia []Listener `json:"confirmVia,omitempty"`
	Timer      *Timer     `json:"timer,omitempty"`
	Backup     *Backups   `json:"backup,omitempty"`
	Undo       []Step     `json:"undo,omitempty"`
	Reload     []Reload   `json:"reload,omitempty"`
	// Applied is set by MarkApplied once the apply has finished; until
	// then the change cannot be confirmed.
	Applied  *Applied  `json:"applied,omitempty"`
	Rollback *Progress `json:"rollback,omitempty"` // rollback.json only
}

// Timeout is the confirm window.
func (j Journal) Timeout() time.Duration { return time.Duration(j.TimeoutSeconds) * time.Second }

// Deadline is when the timer rolls the change back. With a timer, it is
// the timer's deadline: Created plus the window, as Arm sets it, which a
// re-arm at boot keeps, or moves to the window from the re-arm when the
// clock then read before Created (Boot). Without one, Created plus the
// window.
func (j Journal) Deadline() time.Time {
	if t := j.Timer; t != nil && !t.Deadline.IsZero() {
		return t.Deadline
	}
	return j.Created.Add(j.Timeout())
}

// Check refuses a journal a rollback could not act on safely, with
// ErrJournal: every field is checked, since a rollback runs it as root.
func (j Journal) Check() error {
	bad := func(format string, args ...any) error {
		return fmt.Errorf("revert: journal %q: %s: %w", j.ID, fmt.Sprintf(format, args...), ErrJournal)
	}
	switch {
	case j.SchemaVersion != SchemaVersion:
		return bad("schemaVersion %d, want %d", j.SchemaVersion, SchemaVersion)
	case !ValidID(j.ID):
		return bad("id: %v", ErrID)
	case j.Created.IsZero():
		return bad("no created time")
	case j.Risk == RiskLockout && j.Timeout() < MinTimeout:
		return bad("timeout %s below %s", j.Timeout(), MinTimeout)
	case j.Risk == RiskOther && j.TimeoutSeconds != 0:
		return bad("a timeout for a change with no timer")
	case j.Risk != RiskLockout && j.Risk != RiskOther:
		return bad("risk %q", j.Risk)
	case j.Summary == "" || strings.ContainsAny(j.Summary, "\r\n"):
		return bad("summary must be one line")
	}
	if j.Risk == RiskLockout && !j.Session.Remote && j.Session.Source == "" {
		return bad("session not established (ReadSession)")
	}
	if j.Session.Conn != "" {
		if _, err := ParseConn(j.Session.Conn); err != nil {
			return bad("session: %v", err)
		}
	}
	for _, l := range j.ConfirmVia {
		if !l.Addr.IsValid() || l.Port == 0 {
			return bad("confirmVia %v", l)
		}
	}
	switch {
	case j.Risk == RiskLockout && j.Timer == nil:
		return bad("a lockout-risk change with no timer")
	case j.Risk == RiskOther && j.Timer != nil:
		return bad("a timer for a change that has none")
	case j.Timer != nil:
		if err := j.Timer.check(j); err != nil {
			return bad("timer: %v", err)
		}
	}
	if b := j.Backup; b != nil {
		if !clean(b.Dir) || len(b.Seqs) == 0 {
			return bad("backup %+v", *b)
		}
		for i, s := range b.Seqs {
			if s < 1 || i > 0 && s <= b.Seqs[i-1] {
				return bad("backup seqs %v: ascending, from 1", b.Seqs)
			}
		}
	}
	for i, s := range j.Undo {
		if err := s.check(); err != nil {
			return bad("undo step %d: %v", i+1, err)
		}
	}
	for i, r := range j.Reload {
		if err := r.check(); err != nil {
			return bad("reload %d: %v", i+1, err)
		}
		if len(r.SocketDropIns) > 0 && j.Backup == nil {
			return bad("reload %d: socketDropIns with no backups to restore them", i+1)
		}
	}
	if a := j.Applied; a != nil {
		switch {
		case j.Risk != RiskLockout:
			return bad("applied: a change with no timer ends with Done")
		case a.At.IsZero():
			return bad("applied: no time")
		case slices.ContainsFunc(a.SSHD, func(p ProcStart) bool { return !p.known() }):
			return bad("applied: sshd %v", a.SSHD)
		}
	}
	if p := j.Rollback; p != nil {
		switch {
		case p.Started.IsZero(), p.Reason != ReasonTimeout && p.Reason != ReasonRequested && p.Reason != ReasonFailed:
			return bad("rollback progress %+v", *p)
		case p.UndoDone < 0 || p.UndoDone > len(j.Undo), p.ReloadDone < 0 || p.ReloadDone > len(j.Reload):
			return bad("rollback progress past the steps")
		case p.Runner.PID < 0:
			return bad("rollback runner %+v", p.Runner)
		}
	}
	return nil
}

func clean(p string) bool { return filepath.IsAbs(p) && filepath.Clean(p) == p }

func (s Step) check() error {
	if s.Op != OpPlan && (s.Unit != "" || len(s.Plan) > 0) {
		return fmt.Errorf("%s: unit or plan given", s.Op)
	}
	switch s.Op {
	case OpPlan:
		switch {
		case !ValidID(s.Unit):
			return fmt.Errorf("plan: unit %q", s.Unit)
		case len(s.Plan) == 0 || !json.Valid(s.Plan):
			return fmt.Errorf("plan: not JSON")
		case len(s.Argv) > 0 || s.Path != "" || s.Mode != nil || s.Owner != nil:
			return fmt.Errorf("plan: argv, path, mode or owner given")
		}
		return nil
	case OpRun:
		switch {
		case len(s.Argv) == 0 || s.Argv[0] == "":
			return fmt.Errorf("run: no command")
		case strings.Contains(s.Argv[0], "/") && !clean(s.Argv[0]):
			return fmt.Errorf("run: %q: a command is a name or an absolute path", s.Argv[0])
		case s.Path != "" || s.Mode != nil || s.Owner != nil:
			return fmt.Errorf("run: path, mode or owner given")
		}
		return nil
	case OpChmod, OpChown, OpRemoveDir:
		switch {
		case !clean(s.Path) || s.Path == "/":
			return fmt.Errorf("%s: path %q", s.Op, s.Path)
		case len(s.Argv) > 0:
			return fmt.Errorf("%s: argv given", s.Op)
		case (s.Op == OpChmod) != (s.Mode != nil), (s.Op == OpChown) != (s.Owner != nil):
			return fmt.Errorf("%s: mode is chmod's alone, owner chown's alone", s.Op)
		case s.Mode != nil && uint32(*s.Mode) > 0o7777:
			return fmt.Errorf("chmod: mode %o", uint32(*s.Mode))
		case s.Owner != nil && (s.Owner.UID < -1 || s.Owner.GID < -1):
			return fmt.Errorf("chown: owner %+v", *s.Owner)
		}
		return nil
	}
	return fmt.Errorf("op %q", s.Op)
}

// listenKeywords are reload's keywords that move listeners (reload.go's
// listenKeywords): a reload of them needs After.
var listenKeywords = []string{"port", "listenaddress", "addressfamily"}

func movesListeners(kws []string) bool {
	return slices.ContainsFunc(kws, func(k string) bool { return slices.Contains(listenKeywords, strings.ToLower(k)) })
}

func (r Reload) check() error {
	switch {
	case r.Service != ReloadSSH:
		return fmt.Errorf("service %q", r.Service)
	case len(r.Keywords) == 0:
		return fmt.Errorf("ssh: no keywords")
	case movesListeners(r.Keywords) && len(r.After) == 0:
		return fmt.Errorf("ssh: %v move listeners: after is needed", r.Keywords)
	}
	for _, l := range r.After {
		if !l.Addr.IsValid() || l.Port == 0 {
			return fmt.Errorf("ssh: after %v", l)
		}
	}
	for i, d := range r.SocketDropIns {
		switch {
		case !movesListeners(r.Keywords):
			return fmt.Errorf("ssh: socketDropIns for %v, which move no listener", r.Keywords)
		case !clean(d) || filepath.Ext(d) != ".conf" || filepath.Base(filepath.Dir(d)) != reload.SocketUnit+".d":
			return fmt.Errorf("ssh: socketDropIns %q: not a %s drop-in", d, reload.SocketUnit)
		case slices.Contains(r.SocketDropIns[:i], d):
			return fmt.Errorf("ssh: socketDropIns %q twice", d)
		}
	}
	return nil
}

// encode is the journal's file content: indented JSON and a newline.
func encode(v any) ([]byte, error) {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}

// decodeJournal reads and checks a journal. Unknown fields are refused: a
// field this version does not know may be one a rollback must act on.
func decodeJournal(data []byte) (Journal, error) {
	var j Journal
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&j); err != nil {
		return Journal{}, fmt.Errorf("revert: journal: %w: %w", ErrJournal, err)
	}
	if dec.More() {
		return Journal{}, fmt.Errorf("revert: journal: data after the document: %w", ErrJournal)
	}
	return j, j.Check()
}

// Outcome is what the last rollback by the timer did, kept in reverted.json
// until a run has said so (Notices, Acknowledge). Its SchemaVersion is
// OutcomeVersion.
type Outcome struct {
	SchemaVersion  int       `json:"schemaVersion"`
	ID             string    `json:"id"`
	Summary        string    `json:"summary"`
	Created        time.Time `json:"created"`
	TimeoutSeconds int       `json:"timeoutSeconds"`
	Reason         Reason    `json:"reason"`
	Finished       time.Time `json:"finished"`
	Errors         []string  `json:"errors,omitempty"`
}

// decodeOutcome reads an outcome. It is lenient (unknown fields are
// ignored): it is a notice, which no rollback acts on.
func decodeOutcome(data []byte) (Outcome, error) {
	var o Outcome
	if err := json.Unmarshal(data, &o); err != nil {
		return Outcome{}, fmt.Errorf("revert: outcome: %w: %w", ErrJournal, err)
	}
	if o.SchemaVersion != OutcomeVersion || !ValidID(o.ID) {
		return Outcome{}, fmt.Errorf("revert: outcome %q, schemaVersion %d: %w", o.ID, o.SchemaVersion, ErrJournal)
	}
	return o, nil
}
