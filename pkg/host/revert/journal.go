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
// An unspecified address (0.0.0.0, ::) is any address of its family.
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
)

// Owner is a uid and gid; -1 leaves one unchanged.
type Owner struct {
	UID int `json:"uid"`
	GID int `json:"gid"`
}

// Step is one inverse step. Steps run in the journal's order, which the
// engine writes as the order to undo in (last change first), after the
// backups are restored.
type Step struct {
	Op    StepOp       `json:"op"`
	Argv  []string     `json:"argv,omitempty"`
	Path  string       `json:"path,omitempty"`
	Mode  *backup.Perm `json:"mode,omitempty"`
	Owner *Owner       `json:"owner,omitempty"`
	Why   string       `json:"why,omitempty"` // one line for the log
}

// ReloadService is a service a rollback reloads.
type ReloadService string

// ReloadSSH: sshd, through p2-reload's PlanFor with Rollback set, on the
// init system detected at rollback time.
const ReloadSSH ReloadService = "ssh"

// Reload is a service to reload once the backups are restored: for ssh,
// reload.Change's Keywords and After (what the restored files configure).
// Journal one only once the forward reload's gated step ran (the unit was
// up), since a rollback reload starts a unit that is down (reload.Change
// Rollback); a unit that was down reads the restored files when it starts.
type Reload struct {
	Service  ReloadService `json:"service"`
	Keywords []string      `json:"keywords"`
	After    []Listener    `json:"after,omitempty"`
}

// Progress is a started rollback's record, in rollback.json: a rollback
// that stops part-way resumes after what it had done.
type Progress struct {
	Started    time.Time `json:"started"`
	Reason     Reason    `json:"reason"`
	Restored   bool      `json:"restored"`
	UndoDone   int       `json:"undoDone"`
	ReloadDone int       `json:"reloadDone"`
}

// Journal is one change, as the engine records it for a rollback in another
// process. The engine fills ID, Risk, TimeoutSeconds, Summary, Session and
// ConfirmVia, then Backup, Undo and Reload as its steps complete (Record);
// Arm fills SchemaVersion, Created and Timer.
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
	Rollback   *Progress  `json:"rollback,omitempty"` // rollback.json only
}

// Timeout is the confirm window.
func (j Journal) Timeout() time.Duration { return time.Duration(j.TimeoutSeconds) * time.Second }

// Deadline is when the timer rolls the change back.
func (j Journal) Deadline() time.Time { return j.Created.Add(j.Timeout()) }

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
	}
	if p := j.Rollback; p != nil {
		switch {
		case p.Started.IsZero(), p.Reason != ReasonTimeout && p.Reason != ReasonRequested && p.Reason != ReasonFailed:
			return bad("rollback progress %+v", *p)
		case p.UndoDone < 0 || p.UndoDone > len(j.Undo), p.ReloadDone < 0 || p.ReloadDone > len(j.Reload):
			return bad("rollback progress past the steps")
		}
	}
	return nil
}

func clean(p string) bool { return filepath.IsAbs(p) && filepath.Clean(p) == p }

func (s Step) check() error {
	switch s.Op {
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
// until a run has said so (Notices, Acknowledge).
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

func decodeOutcome(data []byte) (Outcome, error) {
	var o Outcome
	if err := json.Unmarshal(data, &o); err != nil {
		return Outcome{}, fmt.Errorf("revert: outcome: %w: %w", ErrJournal, err)
	}
	if o.SchemaVersion != SchemaVersion || !ValidID(o.ID) {
		return Outcome{}, fmt.Errorf("revert: outcome %q, schemaVersion %d: %w", o.ID, o.SchemaVersion, ErrJournal)
	}
	return o, nil
}
