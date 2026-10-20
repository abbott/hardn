package revert

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestNotices(t *testing.T) {
	j := lockout()
	j.SchemaVersion, j.Created = SchemaVersion, testNow
	j.Timer = &Timer{Kind: TimerSystemd, Unit: UnitPrefix + testID, Argv: systemdRunArgv(testID), Deadline: j.Deadline()}
	applying := j
	j.Applied = &Applied{At: testNow.Add(5 * time.Second)}
	other := Journal{SchemaVersion: SchemaVersion, ID: otherID, Created: testNow, Risk: RiskOther, Summary: "DNS resolvers 192.0.2.53"}
	rolling := j
	rolling.Rollback = &Progress{Started: testNow.Add(3 * time.Minute), Reason: ReasonTimeout, Restored: true, Runner: ProcStart{PID: 6000, Start: 1}}

	tests := []struct {
		name string
		st   State
		now  time.Time
		want []NoticeKind
		says []string
	}{
		{"nothing", State{}, testNow, nil, nil},
		{"waiting", State{Pending: &j}, testNow.Add(time.Minute), []NoticeKind{NoticePending}, []string{
			"is waiting for confirmation: it will be rolled back at 2026-10-18 12:03:00 UTC (in 2m0s)",
			"unless you run `sudo hardn confirm` from a new SSH connection to port 2230",
			"`sudo hardn rollback " + testID + "`",
		}},
		{"still being applied", State{Pending: &applying}, testNow.Add(time.Minute), []NoticeKind{NoticeApplying}, []string{
			"started 2026-10-18 12:00:00 UTC, is being applied, or its apply stopped: unless it finishes and is confirmed, it will be rolled back at 2026-10-18 12:03:00 UTC (in 2m0s)",
			"`sudo hardn rollback " + testID + "`",
		}},
		{"past its deadline, never rolled back", State{Pending: &j}, testNow.Add(time.Hour), []NoticeKind{NoticeOverdue}, []string{
			"was not confirmed by 2026-10-18 12:03:00 UTC, and has not been rolled back", "a reboot stops it",
		}},
		{"past its deadline, never applied nor rolled back", State{Pending: &applying}, testNow.Add(time.Hour), []NoticeKind{NoticeOverdue}, nil},
		{"a rollback running now", State{RollingBack: &rolling, Running: true}, testNow, []NoticeKind{NoticeRollingBack}, []string{
			"is being rolled back (process 6000, started 2026-10-18 12:03:00 UTC, timeout)",
		}},
		{"the timer's record cannot be read", State{OutcomeErr: errors.New("reverted.json: revert: outcome \"" + testID + "\", schemaVersion 2: journal not valid")}, testNow, []NoticeKind{NoticeBadOutcome}, []string{
			"cannot read the record of the timer's last rollback", "schemaVersion 2",
		}},
		{"an apply that did not finish", State{Pending: &other}, testNow, []NoticeKind{NoticeInterrupted}, []string{
			"Change " + otherID + " (DNS resolvers 192.0.2.53), started 2026-10-18 12:00:00 UTC, did not finish",
		}},
		{"an unfinished rollback", State{RollingBack: &rolling}, testNow, []NoticeKind{NoticeUnfinished}, []string{
			"did not finish (started 2026-10-18 12:03:00 UTC, timeout)", "no other change can be applied until then",
			"to give it up, leaving the host as it is, run `sudo hardn rollback --abandon " + testID + "`.",
		}},
		{"rolled back by the timer", State{Reverted: &Outcome{SchemaVersion: SchemaVersion, ID: testID, Summary: j.Summary,
			TimeoutSeconds: 180, Reason: ReasonTimeout, Finished: testNow.Add(3 * time.Minute)}}, testNow, []NoticeKind{NoticeReverted}, []string{
			"Change " + testID + " (SSH port 22 to 2230 on web-01.example.net) was rolled back at 2026-10-18 12:03:00 UTC, because it was not confirmed within 180s.",
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ns := Notices(tt.st, tt.now)
			var kinds []NoticeKind
			var text strings.Builder
			for _, n := range ns {
				kinds = append(kinds, n.Kind)
				text.WriteString(n.Text + "\n")
				if strings.Contains(n.Text, "\n") {
					t.Errorf("notice %q spans lines", n.Text)
				}
			}
			if len(kinds) != len(tt.want) {
				t.Fatalf("kinds %v, want %v", kinds, tt.want)
			}
			for i := range kinds {
				if kinds[i] != tt.want[i] {
					t.Fatalf("kinds %v, want %v", kinds, tt.want)
				}
			}
			for _, s := range tt.says {
				if !strings.Contains(text.String(), s) {
					t.Errorf("notices lack %q:\n%s", s, text.String())
				}
			}
		})
	}
}
