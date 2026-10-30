package revert

import (
	"fmt"
	"strings"
	"time"

	"github.com/abbott/hardn/pkg/host"
)

// NoticeKind is what a next-run notice is about.
type NoticeKind string

const (
	NoticeApplying    NoticeKind = "applying"            // a lockout-risk change is being applied (not yet MarkApplied)
	NoticePending     NoticeKind = "pending"             // a lockout-risk change waits for confirmation
	NoticeOverdue     NoticeKind = "overdue"             // its deadline passed and it was not rolled back
	NoticeInterrupted NoticeKind = "interrupted"         // a change without a timer did not finish
	NoticeRollingBack NoticeKind = "rolling-back"        // a rollback is running now
	NoticeUnfinished  NoticeKind = "rollback-unfinished" // a rollback started and did not finish
	NoticeReverted    NoticeKind = "reverted"            // the timer undid the last change
	NoticeBadOutcome  NoticeKind = "outcome-unreadable"  // reverted.json cannot be read
)

// Notice is one thing the next run of hardn says about the journal, as plain
// text: rendering it is the caller's.
type Notice struct {
	Kind NoticeKind
	ID   string
	Text string
}

// Notices is what a run of hardn says about st at now (brief 2.4: the next
// time hardn runs, it says plainly that the last change was undone because
// it was not confirmed). After showing a NoticeReverted or a
// NoticeBadOutcome, call Acknowledge.
func Notices(st State, now time.Time) []Notice {
	var ns []Notice
	hardn := st.hardn()
	if j := st.RollingBack; j != nil && st.Running {
		ns = append(ns, Notice{NoticeRollingBack, j.ID, fmt.Sprintf(
			"Change %s (%s) is being rolled back (process %d, started %s, %s).",
			j.ID, j.Summary, j.Rollback.Runner.PID, stamp(j.Rollback.Started), j.Rollback.Reason)})
	} else if j != nil {
		why := "it stopped before it recorded its progress"
		if j.Rollback != nil {
			why = "started " + stamp(j.Rollback.Started) + ", " + string(j.Rollback.Reason)
		}
		ns = append(ns, Notice{NoticeUnfinished, j.ID, fmt.Sprintf(
			"The rollback of change %s (%s) did not finish (%s). Run `%s rollback %s` to finish it; no other change can be applied until then. If it cannot finish, fix what it names in /var/log/hardn.log; to give it up, leaving the host as it is, run `%s rollback --abandon %s`.",
			j.ID, j.Summary, why, hardn, j.ID, hardn, j.ID)})
	}
	if j := st.Pending; j != nil {
		switch {
		case j.Risk != RiskLockout:
			ns = append(ns, Notice{NoticeInterrupted, j.ID, fmt.Sprintf(
				"Change %s (%s), started %s, did not finish. Undo what it did with `%s rollback %s`.",
				j.ID, j.Summary, stamp(j.Created), hardn, j.ID)})
		case j.Applied == nil && now.Before(j.Deadline()):
			ns = append(ns, Notice{NoticeApplying, j.ID, fmt.Sprintf(
				"Change %s (%s), started %s, is being applied, or its apply stopped: unless it finishes and is confirmed, it will be rolled back at %s (in %s). To undo it now, run `%s rollback %s`.",
				j.ID, j.Summary, stamp(j.Created), stamp(j.Deadline()), j.Deadline().Sub(now).Round(time.Second), hardn, j.ID)})
		case now.Before(j.Deadline()):
			ns = append(ns, Notice{NoticePending, j.ID, fmt.Sprintf(
				"Change %s (%s) is waiting for confirmation: it will be rolled back at %s (in %s) unless you run `%s confirm` from %s. To undo it now, run `%s rollback %s`.",
				j.ID, j.Summary, stamp(j.Deadline()), j.Deadline().Sub(now).Round(time.Second), hardn, confirmWhere(*j), hardn, j.ID)})
		default:
			ns = append(ns, Notice{NoticeOverdue, j.ID, fmt.Sprintf(
				"Change %s (%s) was not confirmed by %s, and has not been rolled back: its timer did not run (a reboot stops it). Undo it with `%s rollback %s`, or keep it with `%s confirm` from a new SSH session.",
				j.ID, j.Summary, stamp(j.Deadline()), hardn, j.ID, hardn)})
		}
	}
	if o := st.Reverted; o != nil {
		text := fmt.Sprintf("Change %s (%s) was rolled back at %s, because it was not confirmed within %ds.",
			o.ID, o.Summary, stamp(o.Finished), o.TimeoutSeconds)
		if len(o.Errors) > 0 {
			text += " The rollback did not finish: " + strings.Join(o.Errors, "; ") + "."
		}
		ns = append(ns, Notice{NoticeReverted, o.ID, text})
	}
	if st.OutcomeErr != nil {
		ns = append(ns, Notice{NoticeBadOutcome, "", fmt.Sprintf(
			"hardn cannot read the record of the timer's last rollback, so cannot say what it did: %v.", st.OutcomeErr)})
	}
	return ns
}

// stamp is how times read in messages: UTC, to the second.
func stamp(t time.Time) string { return t.UTC().Format("2006-01-02 15:04:05 UTC") }

func reasonWords(j Journal) string {
	switch j.Rollback.Reason {
	case ReasonTimeout:
		return fmt.Sprintf("not confirmed within %ds", j.TimeoutSeconds)
	case ReasonFailed:
		return "a step of the change failed"
	}
	return "rollback requested"
}

func stepWords(s Step) string {
	switch s.Op {
	case OpRun:
		return host.FormatArgv(s.Argv)
	case OpChmod:
		return fmt.Sprintf("chmod %04o %s", uint32(*s.Mode), s.Path)
	case OpChown:
		return fmt.Sprintf("chown %d:%d %s", s.Owner.UID, s.Owner.GID, s.Path)
	case OpPlan:
		return "roll back the " + s.Unit + " plan"
	}
	return string(s.Op) + " " + s.Path
}

func errWords(err error) string {
	if err == nil {
		return ""
	}
	return ": error: " + err.Error()
}

func noteWords(note string) string {
	if note == "" {
		return ""
	}
	return " (" + note + ")"
}
