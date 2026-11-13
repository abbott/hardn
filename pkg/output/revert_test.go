package output_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/netip"
	"testing"
	"time"

	"github.com/abbott/hardn/pkg/host/revert"
	"github.com/abbott/hardn/pkg/output"
)

const revertID = "20261019T120000Z-1a2b3c4d"

// revertJournal is a port change's journal, its rollback stopped after the
// one inverse step: the backups not restored, the reload not run.
func revertJournal() revert.Journal {
	return revert.Journal{ID: revertID, Summary: "SSH port 22 to 2230 on web-01.example.net", Risk: revert.RiskLockout, TimeoutSeconds: 180,
		Backup:   &revert.Backups{Dir: "/var/backups/hardn/" + revertID, Seqs: []int{1}},
		Undo:     []revert.Step{{Op: revert.OpRun, Argv: []string{"ufw", "delete", "allow", "2230/tcp"}}},
		Reload:   []revert.Reload{{Service: revert.ReloadSSH, Keywords: []string{"Port"}, After: []revert.Listener{{Addr: netip.IPv4Unspecified(), Port: 22}}}},
		Rollback: &revert.Progress{Reason: revert.ReasonRequested, UndoDone: 1}}
}

// TestRevertDocuments: the record of each of hardn confirm and hardn
// rollback (<id>, --abandon, --boot), by how it ended: its mode, outcome,
// exit status, the change's id, and its error's code
// (docs/reference/output.md §4.1); the nine fields, as every audit line.
func TestRevertDocuments(t *testing.T) {
	ssh := revert.Session{Remote: true, Conn: "192.0.2.10 50123 192.0.2.20 2230"}
	done := revertJournal()
	done.Rollback = &revert.Progress{Reason: revert.ReasonTimeout, Restored: true, UndoDone: 1, ReloadDone: 1}
	incomplete := fmt.Errorf("revert: rollback %s: %w", revertID, revert.ErrIncomplete)
	for _, tc := range []struct {
		name    string
		run     output.RevertRun
		outcome output.Outcome
		exit    int
		id      string
		code    string
	}{
		{"confirmed", output.RevertRun{Mode: output.ModeConfirm, Confirmed: &revert.ConfirmResult{Journal: revertJournal()}}, output.OutcomeConfirmed, 0, revertID, ""},
		{"confirm: nothing pending", output.RevertRun{Mode: output.ModeConfirm, Err: &revert.NotPendingError{}}, output.OutcomeRefused, 1, "", "NOTHING_PENDING"},
		{"confirm: rolled back by the timer", output.RevertRun{Mode: output.ModeConfirm, Err: &revert.NotPendingError{Reverted: &revert.Outcome{ID: revertID}}},
			output.OutcomeRefused, 1, revertID, "CHANGE_ROLLED_BACK"},
		{"confirm: the same session", output.RevertRun{Mode: output.ModeConfirm, Err: revert.ErrSameSession}, output.OutcomeRefused, 1, "", "CONFIRM_SAME_SESSION"},
		{"rollback", output.RevertRun{Mode: output.ModeRollback, ID: revertID, Rollback: &revert.RollbackResult{Journal: done, Reason: revert.ReasonTimeout}},
			output.OutcomeUndone, 0, revertID, ""},
		{"rollback: nothing", output.RevertRun{Mode: output.ModeRollback, ID: revertID, Rollback: &revert.RollbackResult{Nothing: true, Why: "it was confirmed"}},
			output.OutcomeNothingToDo, 0, revertID, ""},
		{"rollback: not finished", output.RevertRun{Mode: output.ModeRollback, ID: revertID, Rollback: &revert.RollbackResult{Journal: revertJournal()}, Err: incomplete},
			output.OutcomeRollbackIncomplete, 1, revertID, "ROLLBACK_INCOMPLETE"},
		{"rollback: the journal not read", output.RevertRun{Mode: output.ModeRollback, ID: revertID, Rollback: &revert.RollbackResult{}, Err: revert.ErrJournal},
			output.OutcomeFailed, 1, revertID, "JOURNAL_INVALID"},
		{"rollback: not an id", output.RevertRun{Mode: output.ModeRollback, ID: "../etc", Rollback: &revert.RollbackResult{}, Err: revert.ErrID},
			output.OutcomeUsage, 2, "../etc", "CHANGE_ID_INVALID"},
		{"abandoned", output.RevertRun{Mode: output.ModeAbandon, ID: revertID, Abandoned: &revert.AbandonResult{Journal: revertJournal(), Kept: "/var/lib/hardn/abandoned-" + revertID + ".json"}},
			output.OutcomeAbandoned, 0, revertID, ""},
		{"abandon: refused", output.RevertRun{Mode: output.ModeAbandon, ID: revertID, Err: &revert.NoRollbackError{Kind: revert.NoRollbackNotStarted}},
			output.OutcomeRefused, 1, revertID, "ABANDON_NOT_STARTED"},
		{"boot: nothing", output.RevertRun{Mode: output.ModeBoot, Boot: &revert.BootResult{Action: revert.BootNothing, Reason: revert.BootReasonNoChange}},
			output.OutcomeNothingToDo, 0, "", ""},
		{"boot: re-armed", output.RevertRun{Mode: output.ModeBoot, Boot: &revert.BootResult{Action: revert.BootRearmed, Reason: revert.BootReasonInWindow, Change: revertJournal()}},
			output.OutcomeRearmed, 0, revertID, ""},
		{"boot: rolled back", output.RevertRun{Mode: output.ModeBoot, Boot: &revert.BootResult{Action: revert.BootRolledBack, Reason: revert.BootReasonDeadlinePassed,
			Change: done, Rollback: &revert.RollbackResult{Journal: done, Reason: revert.ReasonTimeout}}}, output.OutcomeUndone, 0, revertID, ""},
		{"boot: left", output.RevertRun{Mode: output.ModeBoot, Boot: &revert.BootResult{Action: revert.BootLeft, Reason: revert.BootReasonOtherBinary, Change: revertJournal()}},
			output.OutcomeRefused, 1, revertID, ""},
		{"boot: the rollback not finished", output.RevertRun{Mode: output.ModeBoot, Boot: &revert.BootResult{Action: revert.BootRolledBack, Reason: revert.BootReasonDeadlinePassed,
			Change: revertJournal(), Rollback: &revert.RollbackResult{Journal: revertJournal()}}, Err: incomplete}, output.OutcomeRollbackIncomplete, 1, revertID, "ROLLBACK_INCOMPLETE"},
		{"boot: its own path not read", output.RevertRun{Mode: output.ModeBoot, Err: errors.New("readlink /proc/self/exe")}, output.OutcomeFailed, 1, "", output.CodeUnclassified},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tc.run.User, tc.run.Session = "alice", ssh
			d := output.BuildRevert(tc.run)
			code := ""
			if len(d.Errors) == 1 {
				code = d.Errors[0].Code
			}
			rv := d.Result.Revert
			if d.Mode != tc.run.Mode || d.Result.Outcome != tc.outcome || d.Result.ExitCode != tc.exit || d.Result.ID != tc.id || code != tc.code || len(d.Errors) > 1 {
				t.Errorf("mode %q, outcome %q, exit %d, id %q, errors %+v; want %q, %d, %q, %q", d.Mode, d.Result.Outcome, d.Result.ExitCode, d.Result.ID, d.Errors, tc.outcome, tc.exit, tc.id, tc.code)
			}
			if rv == nil || rv.User != "alice" || rv.Session != output.SessionSSH || rv.Client != "192.0.2.10:50123" || rv.Parts == nil {
				t.Fatalf("result.revert: %+v", rv)
			}
			data, err := json.Marshal(d)
			if err != nil {
				t.Fatal(err)
			}
			var fields map[string]any
			if err := json.Unmarshal(data, &fields); err != nil || len(fields) != 9 {
				t.Errorf("%d top-level fields (%v), want the nine", len(fields), err)
			}
		})
	}
}

// TestRevertSession: the session of a record is ssh, console, or unknown
// when it was not established (the revert timer, the boot unit), whatever
// ReadSession returned with its error; the client is the connection's, ""
// when not read.
func TestRevertSession(t *testing.T) {
	for _, tc := range []struct {
		s               revert.Session
		err             error
		session, client string
	}{
		{revert.Session{Remote: true, Conn: "2001:db8::10 50123 2001:db8::20 22"}, nil, output.SessionSSH, "[2001:db8::10]:50123"},
		{revert.Session{Remote: true}, nil, output.SessionSSH, ""},
		{revert.Session{}, nil, output.SessionConsole, ""},
		{revert.Session{Remote: true}, revert.ErrSessionUnknown, output.SessionUnknown, ""},
	} {
		rv := output.BuildRevert(output.RevertRun{Mode: output.ModeConfirm, Session: tc.s, SessionErr: tc.err, Err: revert.ErrNothingPending}).Result.Revert
		if rv.Session != tc.session || rv.Client != tc.client {
			t.Errorf("%+v, %v: session %q, client %q; want %q, %q", tc.s, tc.err, rv.Session, rv.Client, tc.session, tc.client)
		}
	}
}

// TestRevertRecord pins one record whole: a rollback that did not finish,
// as the audit log holds it (docs/reference/output.md §4.1 shows its
// result).
func TestRevertRecord(t *testing.T) {
	run := output.RevertRun{Mode: output.ModeRollback, HardnVersion: "0.9.0", Hostname: "web-01", ID: revertID, User: "alice",
		Session:  revert.Session{Remote: true, Conn: "192.0.2.10 50123 192.0.2.20 2230"},
		Rollback: &revert.RollbackResult{Journal: revertJournal(), Reason: revert.ReasonRequested},
		Err:      fmt.Errorf("revert: rollback %s: %w", revertID, revert.ErrIncomplete)}
	data, err := json.MarshalIndent(output.NewRevertRecord(run, time.Date(2026, 10, 19, 12, 1, 0, 0, time.UTC)), "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	golden(t, "json/audit-rollback.json", append(bytes.TrimSpace(data), '\n'))
}
