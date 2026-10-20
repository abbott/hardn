package main

import (
	"bytes"
	"context"
	"fmt"
	"net/netip"
	"strings"
	"testing"
	"time"

	"github.com/fatih/color"

	"github.com/abbott/hardn/pkg/engine"
	"github.com/abbott/hardn/pkg/engine/confirm"
	"github.com/abbott/hardn/pkg/engine/steps"
	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/hostfake"
	"github.com/abbott/hardn/pkg/host/revert"
	"github.com/abbott/hardn/pkg/host/sshd"
	"github.com/abbott/hardn/pkg/logging"
	"github.com/abbott/hardn/pkg/ui/term"
)

// hardn confirm's clean-up through cmd/hardn's seams (next/p3-confirm-
// cleanup; P-40): the claim is revert's, on the fake host of
// newRevertSeams; the clean-up is stubbed with the outcomes
// pkg/engine/confirm returns, which internal/tools/p2drive's
// confirm_test.go produces over the fake VM. No binary runs.

const cleanupTestID = "20261018T120200Z-5e6f7a8b"

// heldPort makes the journal the 2230 change's as the engine records it:
// its reload restores sshd's listeners on 22, hardn added ufw's rules for
// 2230 and 22, and a confirmation comes over 2230.
func heldPort(j revert.Journal) revert.Journal {
	j.ConfirmVia = []revert.Listener{{Addr: netip.IPv4Unspecified(), Port: 2230}}
	j.Reload = []revert.Reload{{Service: revert.ReloadSSH, Keywords: []string{"Port"}, After: []revert.Listener{{Addr: netip.IPv4Unspecified(), Port: 22}}}}
	j.Undo = []revert.Step{{Op: revert.OpRun, Argv: []string{"ufw", "delete", "allow", "2230/tcp"}},
		{Op: revert.OpRun, Argv: []string{"ufw", "delete", "allow", "22/tcp"}}}
	return j
}

// cleanupPlan is the clean-up's plan on Debian 13, as the engine makes it.
func cleanupPlan() *engine.Plan {
	cs := []engine.Change{
		{ID: confirm.ControlID + "/firewall", Control: confirm.ControlID, Summary: "Firewall: delete the held ports' rules hardn added", Target: "ufw",
			Commands: []string{"ufw delete allow 22/tcp"}, Risk: engine.RiskPlain, Stage: engine.StageFirewall},
		{ID: confirm.ControlID + "/files", Control: confirm.ControlID, Summary: "Drop port 22 from /etc/ssh/sshd_config.d/00-hardn.conf",
			Target: "/etc/ssh/sshd_config.d/00-hardn.conf", Risk: engine.RiskPlain, Stage: engine.StageFiles},
		{ID: confirm.ControlID + "/reload", Control: confirm.ControlID, Summary: "Reload sshd (ssh.service)", Target: "ssh.service",
			Commands: []string{"systemctl reload ssh.service"}, Risk: engine.RiskServiceRestart, Stage: engine.StageReload},
		{ID: confirm.ControlID + "/verify", Control: confirm.ControlID, Summary: "sshd -T shows port 2230 alone", Target: "sshd -T",
			Risk: engine.RiskPlain, Stage: engine.StageVerify},
	}
	var ids []string
	for _, c := range cs {
		ids = append(ids, c.ID)
	}
	return &engine.Plan{ID: cleanupTestID, Risk: engine.RiskServiceRestart, Changes: cs,
		Facts: detect.Facts{Platform: detect.Platform{Distro: detect.DistroDebian, Version: "13", Support: detect.Supported}, SSH: detect.SSH{Mode: detect.SSHService},
			Elevation: detect.Elevation{Sudo: detect.Tool{Present: detect.Yes}}},
		Items: []engine.Item{{Control: confirm.ControlID, Title: "SSH clean-up after the confirmation", Changes: ids}}}
}

func appliedAll() []engine.Applied {
	var out []engine.Applied
	for _, c := range cleanupPlan().Changes {
		out = append(out, engine.Applied{Change: c.ID, Detail: "done"})
	}
	return out
}

// cleanupSeams are newRevertSeams with the change holding port 22 (and
// 00-hardn.conf with it, as the change wrote it), the clean-up stubbed with
// out (recording what it was given), stdout's environment plain and stdout
// and the legacy logger in one buffer.
type cleanupSeams struct {
	*revertSeams
	out      bytes.Buffer
	calls    int
	held     confirm.Held
	session  revert.Session
	signals  int
	canceled bool
}

func newCleanupSeams(t *testing.T, outcome func(h confirm.Held) confirm.Outcome) *cleanupSeams {
	t.Helper()
	s := &cleanupSeams{revertSeams: newRevertSeamsWith(t, 0, heldPort)}
	s.dropIn("Port 22", "Port 2230")
	cleanupWas, stdoutWas, envWas, signalsWas, outputWas := confirmCleanup, confirmStdout, stdoutEnv, confirmSignals, color.Output
	confirmCleanup = func(ctx context.Context, sys host.System, h confirm.Held, se revert.Session, o confirm.Options) confirm.Outcome {
		s.calls++
		s.held, s.session = h, se
		if sys != host.System(s.f) || o.Hold == nil || o.Revert.Log == nil || o.PID != 4000 {
			t.Errorf("Cleanup given %T, options %+v: want confirmHost's, the hold, the log and hardn's pid", sys, o)
		}
		s.canceled = ctx.Err() != nil
		return outcome(h)
	}
	confirmSignals = func(ctx context.Context) (context.Context, context.CancelFunc) {
		s.signals++
		return context.WithCancel(ctx)
	}
	confirmStdout, color.Output = &s.out, &s.out
	stdoutEnv = func() term.Env { return term.Resolve(term.Options{}, term.Facts{}) } // not a terminal: plain
	logging.SetSilentMode(false)
	t.Cleanup(func() {
		confirmCleanup, confirmStdout, stdoutEnv, confirmSignals, color.Output = cleanupWas, stdoutWas, envWas, signalsWas, outputWas
		logging.SetSilentMode(true)
	})
	return s
}

// dropIn writes 00-hardn.conf with lines, as hardn writes it.
func (s *cleanupSeams) dropIn(lines ...string) {
	s.f.AddFile(sshd.HardnDropIn, []byte("# Managed by hardn.\n"+strings.Join(lines, "\n")+"\n"), 0o644, 0, 0)
}

// TestConfirmCleanupCommand: hardn confirm, the change confirmed from a
// new connection to 2230 and holding 22, runs the clean-up from that
// session, with the signals that cancel it registered, and reports it
// through pkg/output's renderer and what is left of it, plain off a
// terminal; the exit status is output.ExitCode's over the clean-up: 0 once
// applied, 1 when it failed and was rolled back or was refused, 130 when
// interrupted. The confirmation stands in every case.
func TestConfirmCleanupCommand(t *testing.T) {
	reloadErr := fmt.Errorf("reload: %w", &host.ExitError{Argv: []string{"systemctl", "reload", "ssh.service"}, ExitCode: 1,
		Tail: []string{"Job for ssh.service failed because the control process exited with error code."}})
	failedAt := func(step string, err error) confirm.Outcome {
		ae := &engine.ApplyError{ID: cleanupTestID, Change: confirm.ControlID + "/reload", Step: step, Err: err}
		return confirm.Outcome{Plan: cleanupPlan(), Result: &engine.Result{ID: cleanupTestID, Applied: appliedAll()[:2], Failed: ae}, Err: ae}
	}
	refusal := fmt.Errorf("%s: port [22]: still configured outside /etc/ssh/sshd_config.d/00-hardn.conf ([22 2230]), which hardn does not edit: %w",
		confirm.ControlID, steps.ErrNoWriteStrategy)
	for _, tc := range []struct {
		name    string
		outcome confirm.Outcome
		exit    int // -1: none
		want    []string
	}{
		{"applied", confirm.Outcome{Plan: cleanupPlan(), Result: &engine.Result{ID: cleanupTestID, Applied: appliedAll()}}, -1, []string{
			"[SUCCESS] Change " + revertTestID + " confirmed",
			"Drop port 22 from", "Operation Successful",
			"Port 22, which change " + revertTestID + " held during its confirm window, is dropped",
			"ufw's rule for 22/tcp, which hardn added, is deleted."}},
		{"the reload failed: rolled back", failedAt(engine.StepApply, reloadErr), 1, []string{
			"[SUCCESS] Change " + revertTestID + " confirmed", "COMMAND_FAILED", "Job for ssh.service failed",
			"Port 22, which change " + revertTestID + " held during its confirm window, is still open",
			"or by hand: delete the `Port 22` line"}},
		{"interrupted", failedAt(engine.StepStart, context.Canceled), 130, []string{
			"[SUCCESS] Change " + revertTestID + " confirmed", "is still open"}},
		{"refused", confirm.Outcome{Plan: &engine.Plan{ID: cleanupTestID, Facts: cleanupPlan().Facts, Refused: []error{refusal},
			Items: []engine.Item{{Control: confirm.ControlID, Title: "SSH clean-up after the confirmation", Err: refusal}}},
			Err: fmt.Errorf("%w: %w", engine.ErrRefused, refusal)}, 1, []string{
			"[SUCCESS] Change " + revertTestID + " confirmed", "still configured outside", "SSHD_NO_WRITE_STRATEGY", "is still open"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := newCleanupSeams(t, func(confirm.Held) confirm.Outcome { return tc.outcome })
			executeHardn(t, "confirm")
			out, flat := s.out.String(), squash(s.out.String())
			if s.exit != tc.exit || s.calls != 1 || s.signals != 1 {
				t.Fatalf("exit %d, want %d; clean-up called %d times, signals %d:\n%s", s.exit, tc.exit, s.calls, s.signals, out)
			}
			if !s.has(revert.DefaultDir+"/"+revert.ConfirmedName) || s.has(revert.DefaultDir+"/"+revert.PendingName) {
				t.Error("the confirmation does not stand")
			}
			if s.held.Finishing || !s.held.Run() || s.held.Change.ID != revertTestID || s.session.Conn != "192.0.2.10 50123 192.0.2.20 2230" {
				t.Errorf("Cleanup given %+v from %+v", s.held, s.session)
			}
			at := 0
			for _, w := range tc.want {
				i := strings.Index(flat[at:], squash(w))
				if i < 0 {
					t.Fatalf("no %q (in order) in:\n%s", w, out)
				}
				at += i
			}
			if strings.Contains(out, "\x1b[") {
				t.Errorf("escape codes off a terminal:\n%q", out)
			}
		})
	}
}

// TestConfirmFinishCommand: with nothing pending, hardn confirm says so
// first, as before, and finishes the clean-up the last confirmed change
// still has in 00-hardn.conf (a crash after its claim), from a session that
// could have confirmed it; it exits 1 all the same, since it confirmed
// nothing. A later change rolled back by its timer is said first, and the
// notice acknowledged, before that clean-up. With nothing left in
// 00-hardn.conf, nothing runs (exit 1, as before); from a session that may
// not finish it (the one that applied the change), it says what is left
// and how to finish it, and runs nothing (exit 1).
func TestConfirmFinishCommand(t *testing.T) {
	crash := func(t *testing.T, s *cleanupSeams) {
		t.Helper()
		if _, err := revert.Confirm(context.Background(), s.f, revert.Session{Remote: true, Conn: "192.0.2.10 50123 192.0.2.20 2230",
			SSHD: revert.ProcStart{PID: 4100, Start: 410000}, Source: "SSH_CONNECTION"}, revert.Options{Now: revertNow}); err != nil {
			t.Fatal(err)
		}
	}
	applied := func(confirm.Held) confirm.Outcome {
		return confirm.Outcome{Plan: cleanupPlan(), Result: &engine.Result{ID: cleanupTestID, Applied: appliedAll()}}
	}
	inOrder := func(t *testing.T, out string, want ...string) {
		t.Helper()
		at := 0
		for _, w := range want {
			i := strings.Index(squash(out)[at:], squash(w))
			if i < 0 {
				t.Fatalf("no %q (in order) in:\n%s", w, out)
			}
			at += i
		}
	}
	t.Run("finished", func(t *testing.T) {
		s := newCleanupSeams(t, applied)
		crash(t, s)
		executeHardn(t, "confirm")
		out := s.out.String()
		if s.exit != 1 || s.calls != 1 || !s.held.Finishing || s.held.Change.ID != revertTestID {
			t.Fatalf("exit %d, clean-up called %d times (%+v):\n%s", s.exit, s.calls, s.held, out)
		}
		inOrder(t, out, "[ERROR] revert: confirm: no change is waiting for confirmation",
			"Change "+revertTestID+" (SSH port 22 to 2230 on web-01.example.net), the last one confirmed, still has port 22 in /etc/ssh/sshd_config.d/00-hardn.conf",
			"Operation Successful", "the last one confirmed, held during its confirm window, is dropped")
	})
	t.Run("a later change rolled back by its timer", func(t *testing.T) {
		s := newCleanupSeams(t, applied)
		crash(t, s)
		const later = "20261018T130000Z-9a8b7c6d"
		laterNow := revertTestNow.Add(time.Hour)
		s.arm(t, later, laterNow)
		if _, err := revert.Rollback(context.Background(), s.f, later, revert.Options{Now: func() time.Time { return laterNow.Add(181 * time.Second) }}); err != nil {
			t.Fatal(err)
		}
		executeHardn(t, "confirm")
		out := s.out.String()
		if s.exit != 1 || s.calls != 1 || s.has(revert.DefaultDir+"/"+revert.RevertedName) {
			t.Fatalf("exit %d, clean-up called %d times, reverted.json kept %v:\n%s", s.exit, s.calls, s.has(revert.DefaultDir+"/"+revert.RevertedName), out)
		}
		inOrder(t, out, "[ERROR] revert: confirm: no change is waiting for confirmation: change "+later, "was rolled back at",
			"Change "+revertTestID, "still has port 22", "Operation Successful")
	})
	t.Run("nothing left", func(t *testing.T) {
		s := newCleanupSeams(t, func(confirm.Held) confirm.Outcome {
			t.Error("the clean-up ran")
			return confirm.Outcome{}
		})
		crash(t, s)
		s.dropIn("Port 2230")
		executeHardn(t, "confirm")
		out := s.out.String()
		if s.exit != 1 || !strings.Contains(out, "[ERROR] revert: confirm: no change is waiting for confirmation") ||
			strings.Contains(out, "Port 22") || strings.Contains(out, "Nothing needs changing") {
			t.Errorf("exit %d:\n%s", s.exit, out)
		}
	})
	t.Run("from the session that applied the change", func(t *testing.T) {
		s := newCleanupSeams(t, func(confirm.Held) confirm.Outcome {
			t.Error("the clean-up ran")
			return confirm.Outcome{}
		})
		crash(t, s)
		revertProc(s.f, 3980, 3970, "bash", "SSH_CONNECTION=192.0.2.10 50022 192.0.2.20 22") // under the applying sshd-session
		executeHardn(t, "confirm")
		out := s.out.String()
		if s.exit != 1 || !strings.Contains(out, "no change is waiting for confirmation") ||
			!strings.Contains(squash(out), squash("this session cannot finish its clean-up")) || !strings.Contains(squash(out), squash("from a new SSH connection to port 2230")) {
			t.Errorf("exit %d:\n%s", s.exit, out)
		}
	})
}

// arm arms change id, which holds no port (as keyonly), at now, and marks
// it applied, as the engine does; then alice connects again on 2230
// (sshd-session 4300) and runs sudo hardn, process 4000, from there.
func (s *cleanupSeams) arm(t *testing.T, id string, now time.Time) {
	t.Helper()
	argv := []string{"systemd-run", "--unit=hardn-revert-" + id, "--description=hardn: roll back change " + id + " unless confirmed",
		"--on-active=180s", "--timer-property=AccuracySec=1s", revertTestHardn, "rollback", id}
	s.f.Respond(hostfake.Response{}, argv[0], argv[1:]...)
	s.f.Respond(hostfake.Response{}, "systemctl", "stop", "hardn-revert-"+id+".timer")
	clock := func() time.Time { return now }
	j := revert.Journal{ID: id, Risk: revert.RiskLockout, TimeoutSeconds: 180, Summary: "SSH: key-only login on web-01.example.net",
		Session: revert.Session{Remote: true, Conn: "192.0.2.10 50300 192.0.2.20 2230"}}
	if _, err := revert.Arm(context.Background(), s.f, j, detect.Init{Kind: detect.Systemd, Running: detect.Yes},
		revert.Options{Now: clock, Hardn: revertTestHardn}); err != nil {
		t.Fatal(err)
	}
	if _, err := revert.MarkApplied(context.Background(), s.f, id, revert.Options{Now: clock}); err != nil {
		t.Fatal(err)
	}
	revertProc(s.f, 4300, 900, "sshd-session")
	revertProc(s.f, 4310, 4300, "bash", "SSH_CONNECTION=192.0.2.10 50400 192.0.2.20 2230")
	revertProc(s.f, 4320, 4310, "sudo")
	revertProc(s.f, 4000, 4320, "hardn")
}

// TestConfirmCarriedCommand: change A's clean-up was left (a crash after
// its claim); change B, which holds no port, is confirmed from a new
// connection to 2230. The claim replaces A's record, so hardn confirm
// cleans up after A from that connection: B is confirmed (its id in the
// success line), then A's clean-up runs and is reported (exit 0).
func TestConfirmCarriedCommand(t *testing.T) {
	s := newCleanupSeams(t, func(confirm.Held) confirm.Outcome {
		return confirm.Outcome{Plan: cleanupPlan(), Result: &engine.Result{ID: cleanupTestID, Applied: appliedAll()}}
	})
	if _, err := revert.Confirm(context.Background(), s.f, revert.Session{Remote: true, Conn: "192.0.2.10 50123 192.0.2.20 2230",
		SSHD: revert.ProcStart{PID: 4100, Start: 410000}, Source: "SSH_CONNECTION"}, revert.Options{Now: revertNow}); err != nil {
		t.Fatal(err)
	}
	const later = "20261018T130000Z-9a8b7c6d"
	s.arm(t, later, revertTestNow.Add(time.Hour))
	executeHardn(t, "confirm")
	out := s.out.String()
	if s.exit != -1 || s.calls != 1 || !s.held.Carried || s.held.Change.ID != revertTestID || s.held.Confirmed.Journal.ID != later {
		t.Fatalf("exit %d, clean-up called %d times (%+v):\n%s", s.exit, s.calls, s.held, out)
	}
	if c, err := revert.ReadConfirmed(context.Background(), s.f, revert.Options{}); err != nil || c.ID != later {
		t.Errorf("confirmed.json %+v, %v; want %s", c, err, later)
	}
	at := 0
	for _, w := range []string{"[SUCCESS] Change " + later + " confirmed",
		"Change " + revertTestID + " (SSH port 22 to 2230 on web-01.example.net), confirmed before change " + later + ", still has port 22",
		"Operation Successful", "Port 22, which change " + revertTestID + ", confirmed before change " + later + ", held during its confirm window, is dropped"} {
		i := strings.Index(squash(out)[at:], squash(w))
		if i < 0 {
			t.Fatalf("no %q (in order) in:\n%s", w, out)
		}
		at += i
	}
}

// TestConfirmConsoleCommand: a console confirms, as before, and cleans
// nothing up: it shows nothing about SSH, so port 22 stays open, and the
// report says how to drop it (exit 0).
func TestConfirmConsoleCommand(t *testing.T) {
	s := newCleanupSeams(t, func(confirm.Held) confirm.Outcome {
		t.Error("the clean-up ran from a console")
		return confirm.Outcome{}
	})
	// hardn under bash under login on tty1: a console.
	revertProc(s.f, 3000, 1, "login")
	revertProc(s.f, 3980, 3000, "bash")
	executeHardn(t, "confirm")
	out := s.out.String()
	if s.exit != -1 || !strings.Contains(out, "Confirmed from a session that is not over SSH") ||
		!strings.Contains(squash(out), squash("stays open: hardn drops it only from a new SSH connection to port 2230")) {
		t.Errorf("exit %d:\n%s", s.exit, out)
	}
}

// squash is s with no white space, so that a sentence matches however the
// report wraps it.
func squash(s string) string { return strings.Join(strings.Fields(s), "") }

// TestConfirmSeamsAreReal: the clean-up's host is host.Real, whose run log
// goes to hardn's log alone.
func TestConfirmSeamsAreReal(t *testing.T) {
	if _, ok := confirmHost().(*host.Real); !ok {
		t.Error("confirmHost is not host.Real")
	}
	if n, err := (revertFileLog{}).Write([]byte("run 1: $ sshd -T\n")); n != 17 || err != nil {
		t.Errorf("revertFileLog.Write = %d, %v", n, err)
	}
}
