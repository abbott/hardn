package revert

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/hostfake"
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

// doasHost is a fake host where doas is the elevation tool: sudo is not
// installed, as on Alpine.
func doasHost(t *testing.T) *hostfake.System {
	t.Helper()
	f := newHost(t)
	asDoas(t, f)
	return f
}

// asDoas makes f a doas host: sudo removed, doas installed.
func asDoas(t *testing.T, f *hostfake.System) {
	t.Helper()
	if err := f.Remove(context.Background(), "/usr/bin/sudo"); err != nil {
		t.Fatal(err)
	}
	f.AddFile("/usr/bin/doas", []byte("ELF"), 0o755, 0, 0)
}

// TestHostsToolInTheWords: every line that says to run hardn says doas on a
// doas host (found on the Alpine 3.23 VM, 2026-10-30), and sudo where sudo is
// installed, doas or not; "hardn" alone where neither is (Proxmox VE).
func TestHostsToolInTheWords(t *testing.T) {
	ctx := context.Background()
	check := func(t *testing.T, what, got string, want ...string) {
		t.Helper()
		if strings.Contains(got, "sudo") {
			t.Errorf("%s names sudo: %s", what, got)
		}
		for _, w := range want {
			if !strings.Contains(got, w) {
				t.Errorf("%s = %s\nwant %q", what, got, w)
			}
		}
	}
	second := lockout()
	second.ID = otherID

	t.Run("while a change is applied, and waits", func(t *testing.T) {
		f := doasHost(t)
		c := &clock{testNow}
		armed(t, f, c)
		_, err := Arm(ctx, f, second, systemd, testOptions(c, nil))
		check(t, "the refusal while it is applied", err.Error(), "undo it now with `doas hardn rollback "+testID+"`")
		st, err := ReadState(ctx, f, testOptions(c, nil))
		if err != nil || st.Elevate != "doas" {
			t.Fatalf("ReadState: Elevate %q, %v", st.Elevate, err)
		}
		check(t, "the notice while it is applied", Notices(st, c.t)[0].Text, "`doas hardn rollback "+testID+"`")

		markApplied(t, f, c)
		_, err = Arm(ctx, f, second, systemd, testOptions(c, nil))
		check(t, "the refusal while it waits", err.Error(), "confirm it with `doas hardn confirm` from a new SSH connection to port 2230, or undo it with `doas hardn rollback "+testID+"`")
		_, err = Confirm(ctx, f, remote(applyConn, applySSHD), testOptions(c, nil))
		check(t, "hardn confirm's refusal", err.Error(), "run `doas hardn confirm` from a new SSH connection to port 2230, made from another machine, before")
		_, err = Confirm(ctx, f, Session{Remote: true, Source: "/proc/11025/stat"}, testOptions(c, nil))
		check(t, "hardn confirm's refusal, the connection not known", err.Error(), "log in over a new SSH connection to port 2230, made from another machine, and run `doas hardn confirm` at the prompt")
		if st, err = ReadState(ctx, f, testOptions(c, nil)); err != nil {
			t.Fatal(err)
		}
		check(t, "the notice while it waits", Notices(st, c.t)[0].Text, "unless you run `doas hardn confirm` from a new SSH connection to port 2230", "`doas hardn rollback "+testID+"`")
		check(t, "the notice past its deadline", Notices(st, st.Pending.Deadline().Add(time.Hour))[0].Text, "`doas hardn rollback "+testID+"`", "`doas hardn confirm`")
		_, err = Abandon(ctx, f, testID, testOptions(c, nil))
		check(t, "a refused abandon", err.Error(), "to undo it, run `doas hardn rollback "+testID+"`")
	})

	t.Run("a change that did not finish, at hardn confirm", func(t *testing.T) {
		f := doasHost(t)
		c := &clock{testNow}
		other := Journal{ID: otherID, Risk: RiskOther, Summary: "DNS resolvers 192.0.2.53"}
		if _, err := Arm(ctx, f, other, systemd, testOptions(c, nil)); err != nil {
			t.Fatal(err)
		}
		_, err := Confirm(ctx, f, newSession, testOptions(c, nil))
		check(t, "the refusal", err.Error(), "undo it with `doas hardn rollback "+otherID+"`")
	})

	t.Run("a rollback that stops part-way", func(t *testing.T) {
		ch := newChange(t)
		asDoas(t, ch.f)
		ch.f.Respond(hostfake.Response{ExitCode: 1, Stderr: "ERROR: Could not delete non-existent rule\n"}, "ufw", "delete", "allow", "2230/tcp")
		ch.c.t = ch.j.Deadline()
		_, err := Rollback(ctx, ch.f, testID, ch.opts)
		check(t, "the error", err.Error(), "run `doas hardn rollback "+testID+"` to take it up again", "with `doas hardn rollback --abandon "+testID+"`")
	})

	t.Run("at boot", func(t *testing.T) {
		ch := rebooted(t, 100*time.Second+500*time.Millisecond)
		respond(ch.f, hostfake.Response{}, rearmArgv("80s"))
		asDoas(t, ch.f)
		res, err := Boot(ctx, ch.f, systemd, ch.opts)
		if err != nil {
			t.Fatal(err)
		}
		check(t, "the re-arm", res.Why, "unless confirmed with `doas hardn confirm` from a new SSH connection to port 2230")

		ch = newChangeBare(t)
		asDoas(t, ch.f)
		markApplied(t, ch.f, ch.c)
		timerState(ch.f, "inactive")
		ch.f.AddDir(DefaultRunDir, 0o700, 0, 0)
		if _, err := ch.f.Lock(ctx, DefaultRunDir+"/"+RunLockName, []byte("pid 6100, hardn rollback --boot, since 2026-10-18T12:01:00Z\n")); err != nil {
			t.Fatal(err)
		}
		res, err = Boot(ctx, ch.f, systemd, ch.opts)
		if err != nil {
			t.Fatal(err)
		}
		check(t, "the run lock held", res.Why, "run `doas hardn rollback --boot` again", "confirm it with `doas hardn confirm`", "undo it with `doas hardn rollback "+testID+"`")
	})

	t.Run("a change with no timer that did not finish", func(t *testing.T) {
		st := State{Elevate: "doas", Pending: &Journal{SchemaVersion: SchemaVersion, ID: otherID, Created: testNow, Risk: RiskOther, Summary: "DNS"}}
		check(t, "the notice", Notices(st, testNow)[0].Text, "`doas hardn rollback "+otherID+"`")
		check(t, "the refusal", (&PendingError{State: st}).Error(), "undo it with `doas hardn rollback "+otherID+"`")
	})

	t.Run("an unfinished rollback", func(t *testing.T) {
		j := lockout()
		j.Rollback = &Progress{Started: testNow, Reason: ReasonTimeout}
		st := State{Elevate: "doas", RollingBack: &j}
		check(t, "the notice", Notices(st, testNow)[0].Text, "Run `doas hardn rollback "+testID+"` to finish it", "run `doas hardn rollback --abandon "+testID+"`")
		check(t, "the refusal", (&PendingError{State: st}).Error(), "run `doas hardn rollback "+testID+"` to finish it")
		err := abandonable(ctx, doasHost(t), st, otherID, "/kept")
		check(t, "abandoning another change", err.Error(), "finish it with `doas hardn rollback "+testID+"`", "give it up with `doas hardn rollback --abandon "+testID+"`")
		j.Rollback.Runner = ProcStart{PID: 6000, Start: 1}
		err = rollingBackError(testID, *j.Rollback, st, "is rolling it back now")
		check(t, "abandoning a rollback that runs", err.Error(), "run `doas hardn rollback "+testID+"` once more")
	})

	t.Run("neither sudo nor doas: root runs hardn itself", func(t *testing.T) {
		f := bareHost(t)
		c := &clock{testNow}
		armed(t, f, c)
		st, err := ReadState(ctx, f, testOptions(c, nil))
		if err != nil || st.Elevate != NoElevation {
			t.Fatalf("ReadState: Elevate %q, %v", st.Elevate, err)
		}
		markApplied(t, f, c)
		_, err = Arm(ctx, f, second, systemd, testOptions(c, nil))
		check(t, "the refusal while it waits", err.Error(), "confirm it with `hardn confirm` from a new SSH connection to port 2230, or undo it with `hardn rollback "+testID+"`")
		if st, err = ReadState(ctx, f, testOptions(c, nil)); err != nil {
			t.Fatal(err)
		}
		check(t, "the notice while it waits", Notices(st, c.t)[0].Text, "unless you run `hardn confirm` from a new SSH connection to port 2230", "`hardn rollback "+testID+"`")
		if strings.Contains(Notices(st, c.t)[0].Text, "doas") {
			t.Errorf("the notice names doas: %s", Notices(st, c.t)[0].Text)
		}
	})

	t.Run("Hardn: the words for each host", func(t *testing.T) {
		tool := func(p detect.Tri) detect.Tool { return detect.Tool{Present: p} }
		for _, tc := range []struct {
			sudo, doas detect.Tri
			want       string
		}{
			{detect.No, detect.No, "hardn"},
			{detect.Yes, detect.No, "sudo hardn"},
			{detect.No, detect.Yes, "doas hardn"},
			{detect.Yes, detect.Yes, "sudo hardn"},
			{detect.Unknown, detect.No, "sudo hardn"},
			{detect.No, detect.Unknown, "sudo hardn"},
		} {
			if got := Hardn(detect.Elevation{Sudo: tool(tc.sudo), Doas: tool(tc.doas)}); got != tc.want {
				t.Errorf("Hardn(sudo %v, doas %v) = %q, want %q", tc.sudo, tc.doas, got, tc.want)
			}
		}
	})

	t.Run("sudo installed too, or sudo alone", func(t *testing.T) {
		f := doasHost(t)
		f.AddFile("/usr/bin/sudo", []byte("ELF"), 0o755, 0, 0)
		c := &clock{testNow}
		armed(t, f, c)
		if st, err := ReadState(ctx, f, testOptions(c, nil)); err != nil || st.Elevate != "sudo" {
			t.Errorf("with both: Elevate %q, %v", st.Elevate, err)
		}
		f = newHost(t)
		armed(t, f, c)
		if st, err := ReadState(ctx, f, testOptions(c, nil)); err != nil || st.Elevate != "sudo" {
			t.Errorf("with sudo: Elevate %q, %v", st.Elevate, err)
		}
		if st, err := ReadState(ctx, doasHost(t), testOptions(c, nil)); err != nil || st.Elevate != "" {
			t.Errorf("nothing journalled: Elevate %q, %v", st.Elevate, err)
		}
	})
}
