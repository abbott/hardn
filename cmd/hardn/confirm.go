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
	"github.com/abbott/hardn/pkg/logging"
	"github.com/abbott/hardn/pkg/output"
	"github.com/abbott/hardn/pkg/ui/component"
	"github.com/abbott/hardn/pkg/ui/term"
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
// clean-up of what the change held, and the exit status. Exit 1 when
// nothing was confirmed (as before), and when the clean-up did not finish
// (refused, or failed and rolled back here): the confirmation stands, but
// the host is not as asked yet, so a script is not told it is (output.md's
// exit statuses: an apply that failed, or a refusal, is 1); 130 when it was
// interrupted; 0 once the change is confirmed and nothing it held is left,
// or a console confirmed it (the clean-up is then left for SSH, by design).
func confirmAndCleanUp(ctx context.Context, sys host.System, s revert.Session, opts revert.Options) int {
	h, err := confirmClaim(ctx, sys, s, opts)
	if err != nil && !h.Run() {
		logging.LogError("%v", err)
		acknowledgeRolledBack(ctx, sys, opts, err)
		showCleanup(h, confirm.Outcome{})
		return 1
	}
	if err == nil {
		if h.Confirmed.Local {
			logging.LogWarning("Confirmed from a session that is not over SSH: this does not show that a new SSH connection works.")
		}
		logging.LogSuccess("Change %s confirmed (%s): it will not be rolled back.", h.Change.ID, h.Change.Summary)
	}
	if !h.Run() {
		showCleanup(h, confirm.Outcome{})
		return 0
	}
	cctx, stop := confirmSignals(ctx)
	o := confirmCleanup(cctx, confirmHost(), h, s, confirm.Options{
		Revert: revert.Options{Log: revertFileLog{}, Now: revertNow}, Hold: confirmHold, PID: revertPID()})
	stop()
	if h.Finishing {
		if o.Done() && o.Result.Nothing {
			// Nothing pending, and nothing left of the last change's
			// clean-up: hardn confirm as before.
			logging.LogError("%v", err)
			acknowledgeRolledBack(ctx, sys, opts, err)
			return 1
		}
		logging.LogInfo("No change is waiting for confirmation. Change %s (%s), the last one confirmed, still held port %s: its clean-up, from this connection.",
			h.Change.ID, h.Change.Summary, portWords(h.Cleanup.Drop))
	}
	run := output.Run{Mode: output.ModeApply, Plan: o.Plan, Result: o.Result, Err: o.Err}
	if err := output.Emit(output.Streams{Stdout: confirmStdout, Out: stdoutEnv()}, output.FormatHuman, run, revertNow()); err != nil {
		logging.LogWarning("hardn confirm: the clean-up's report: %v", err)
	}
	showCleanup(h, o)
	return output.ExitCode(run)
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
// no colour off a terminal.
func showCleanup(h confirm.Held, o confirm.Outcome) {
	r := h.Report(o)
	env := stdoutEnv()
	c := component.NewCtx(env)
	var lines []string
	for _, d := range r.Done {
		lines = append(lines, component.Message{Kind: component.MessageOK, Text: d}.Render(c))
	}
	for _, l := range r.Left {
		lines = append(lines, component.Message{Kind: component.MessageWarn, Text: l}.Render(c))
	}
	if r.Finish != "" {
		lines = append(lines, component.Message{Text: r.Finish}.Render(c))
	}
	if len(lines) > 0 {
		_, _ = io.WriteString(env.Writer(confirmStdout), strings.Join(lines, "\n")+"\n")
	}
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
// (every command and its output) and the engine's and revert's lines,
// which the clean-up's report sums up on the console.
type revertFileLog struct{}

func (revertFileLog) Write(p []byte) (int, error) {
	for _, line := range strings.Split(strings.TrimRight(string(p), "\n"), "\n") {
		logging.LogFileOnly("%s", line)
	}
	return len(p), nil
}
