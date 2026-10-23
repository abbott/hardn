package main

// hardn confirm's clean-up (refactor plan P3, next/p3-confirm-cleanup;
// P-40, decided by the maintainer on 2026-10-19): once the change is
// confirmed from a new connection, the port it held during its confirm
// window is dropped, sshd reloaded and the ufw rule the change added for it
// deleted, through the engine (pkg/engine/confirm), and reported through
// pkg/output's renderer. The confirmation stands either way.

import (
	"context"
	"errors"
	"io"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"

	"github.com/abbott/hardn/pkg/engine/confirm"
	"github.com/abbott/hardn/pkg/engine/hold"
	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/revert"
	"github.com/abbott/hardn/pkg/host/sshd"
	"github.com/abbott/hardn/pkg/logging"
	"github.com/abbott/hardn/pkg/output"
	"github.com/abbott/hardn/pkg/ui/component"
	"github.com/abbott/hardn/pkg/ui/term"
	"github.com/abbott/hardn/pkg/ui/theme"
)

// Seams for the tests: the claim and the clean-up, the clean-up's host,
// stdout and its render environment, the signals that cancel the
// clean-up, and the hold around its rollback.
var (
	confirmClaim             = confirm.Claim
	confirmCleanup           = confirm.Cleanup
	confirmHost              = func() host.System { return host.NewReal(host.Options{RunLog: revertFileLog{}}) }
	confirmStdout  io.Writer = os.Stdout
	stdoutEnv                = func() term.Env { return streamEnv(os.Stdout) }
	confirmSignals           = func(ctx context.Context) (context.Context, context.CancelFunc) {
		return signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)
	}
	confirmHold = func() func() { return hold.SignalsAs("hardn confirm", revertWarnLog{}) }
)

// confirmAndCleanUp is hardn confirm from session s: the claim, then the
// clean-up of what the change held, and the exit status. Nothing confirmed
// is said first, as before, with the timer's rollback of the change meant
// (acknowledged once said), whatever clean-up follows, and exits 1 (130
// when the clean-up was interrupted): `confirm` exits 1 when it does not
// confirm (revert.md §2.21). A change confirmed exits 1 when its clean-up
// ran and did not finish (refused, or failed and rolled back here): the
// confirmation stands, but the host is not as asked yet, so a script is not
// told it is (output.md's exit statuses: an apply that failed, or a
// refusal, is 1); 130 when it was interrupted; 0 once nothing it held is
// left, and when the clean-up was not run from this session (a console, by
// design, or a session that could not have confirmed the change before
// it), whose report says what stays open.
func confirmAndCleanUp(ctx context.Context, sys host.System, s revert.Session, opts revert.Options) int {
	h, err := confirmClaim(ctx, sys, s, opts)
	code := 0
	if err != nil {
		logging.LogError("%v", err)
		acknowledgeRolledBack(ctx, sys, opts, err)
		code = 1
	} else {
		if h.Confirmed.Local {
			logging.LogWarning("Confirmed from a session that is not over SSH: this does not show that a new SSH connection works.")
		}
		logging.LogSuccess("Change %s confirmed (%s): it will not be rolled back.", h.Confirmed.Journal.ID, h.Confirmed.Journal.Summary)
	}
	if !h.Run() {
		showCleanup(h, confirm.Outcome{}, false)
		return code
	}
	if h.Finishing || h.Carried {
		which := "the last one confirmed"
		if h.Carried {
			which = "confirmed before change " + h.Confirmed.Journal.ID
		}
		logging.LogInfo("Change %s (%s), %s, still has port %s in %s, which it held during its confirm window: its clean-up, from this connection.",
			h.Change.ID, h.Change.Summary, which, portWords(h.Cleanup.Drop), sshd.HardnDropIn)
	}
	cctx, stop := confirmSignals(ctx)
	o := confirmCleanup(cctx, confirmHost(), h, s, confirm.Options{
		Revert: revert.Options{Log: revertFileLog{}, Now: revertNow}, Hold: confirmHold, PID: revertPID()})
	stop()
	run := output.Run{Mode: output.ModeApply, Plan: o.Plan, Result: o.Result, Err: o.Err}
	reported := !o.Done() || !o.Result.Nothing
	if reported {
		if err := output.Emit(output.Streams{Stdout: confirmStdout, Out: stdoutEnv()}, output.FormatHuman, run, revertNow()); err != nil {
			logging.LogWarning("hardn confirm: the clean-up's report: %v", err)
		}
	}
	showCleanup(h, o, reported)
	return max(code, output.ExitCode(run))
}

// acknowledgeRolledBack clears the timer's rollback notice once a
// confirmation has said that the change was rolled back (revert.md §2.22).
func acknowledgeRolledBack(ctx context.Context, sys host.System, opts revert.Options, err error) {
	var np *revert.NotPendingError
	if errors.As(err, &np) && np.RolledBack() {
		acknowledge(ctx, sys, opts)
	}
}

// showCleanup writes what the clean-up of h did and what is left of it,
// and how to finish it (confirm.Held.Report), on stdout through pkg/ui, with
// no colour off a terminal: one run of messages (component.Messages), so
// what was done is one "Operation Successful" callout, what is left one
// "Warning", and how to finish it prose. After the clean-up's report
// (afterReport), whose last block is a callout too, the run starts
// theme.SectionGap blank lines down, as pkg/output separates its blocks,
// so that its first callout is not read as the report's continued.
func showCleanup(h confirm.Held, o confirm.Outcome, afterReport bool) {
	r := h.Report(o)
	env := stdoutEnv()
	var run component.Messages
	for _, d := range r.Done {
		run = append(run, component.Message{Kind: component.MessageOK, Text: d})
	}
	for _, l := range r.Left {
		run = append(run, component.Message{Kind: component.MessageWarn, Text: l})
	}
	run = append(run, component.Message{Text: r.Finish})
	out := run.Render(component.NewCtx(env))
	if out == "" {
		return
	}
	if afterReport {
		out = strings.Repeat("\n", theme.SectionGap) + out
	}
	_, _ = io.WriteString(env.Writer(confirmStdout), out+"\n")
}

// portWords is ports in words: "22", "22 and 2222".
func portWords(ps []uint16) string {
	var s []string
	for _, p := range ps {
		s = append(s, strconv.Itoa(int(p)))
	}
	return strings.Join(s, " and ")
}

// revertFileLog writes each line to hardn's log, /var/log/hardn.log, alone,
// as revertLog writes it to the log and the console: the clean-up's run log
// (every command and its output) and the engine's and revert's lines
// during the clean-up, which its report sums up on the console. The
// claim's lines go through revertLog, to both, as before.
type revertFileLog struct{}

func (revertFileLog) Write(p []byte) (int, error) {
	for _, line := range strings.Split(strings.TrimRight(string(p), "\n"), "\n") {
		logging.LogFileOnly("%s", line)
	}
	return len(p), nil
}
