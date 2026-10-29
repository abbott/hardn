package main

// The plan/apply engine in cmd/hardn (refactor plan slice 2, the DNS
// pilot, unit (d), next/s2-replace): its options, assembled in one place
// for every path that changes the host through it, -g and the main menu's
// DNS screen now, the next slices' flags and screens after them, from what
// hardn confirm and hardn rollback already build (revert.go, confirm.go).
// docs/engine/cmd-engine.md says where each option comes from.

import (
	"context"
	"io"
	"os"
	"os/signal"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/abbott/hardn/pkg/config"
	"github.com/abbott/hardn/pkg/engine"
	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/hangup"
	"github.com/abbott/hardn/pkg/host/revert"
	"github.com/abbott/hardn/pkg/logging"
	"github.com/abbott/hardn/pkg/ui/prompt"
)

// engineSignalSet are the signals that cancel an apply: Ctrl-C, SIGTERM
// and a hangup, which hardn treats as Ctrl-C (engine.md §2.14).
var engineSignalSet = []os.Signal{os.Interrupt, syscall.SIGTERM, syscall.SIGHUP}

// Seams for the tests: the host the engine changes and the screens read,
// with its run log; detection, the engine's and the screens'; the hold
// around an in-process rollback (nil: the engine's own); the context an
// apply runs under, which engineSignalSet cancels, registered for that
// apply alone (engineSignals); the line reader and the stdout the screens
// and a flag's report use.
var (
	engineHost    = func() host.System { return host.NewReal(host.Options{RunLog: revertFileLog{}}) }
	engineDetect  = detect.Detect
	engineHold    func() (release func())
	engineSignals = func(ctx context.Context) (context.Context, context.CancelFunc) {
		return signal.NotifyContext(ctx, engineSignalSet...)
	}
	engineInput            = prompt.Stdin
	engineStdout io.Writer = termStdout
)

// terminalGone records a hangup: hardn's terminal is gone, and what hardn
// writes to it is dropped from then on (hangup.Seen, through termStdout
// and termStderr); a seam for the tests.
var terminalGone = hangup.Seen

// A hangupWatch catches SIGHUP from watchHangup until its stop, and marks
// hardn's terminal gone (terminalGone) as soon as one comes, not at stop:
// under sudo-rs a write to the terminal blocks for good once the session
// has dropped and the pty's buffer is full, and the apply or the rollback
// that wrote it, so that write is given up on and the work goes on (since
// next/s3-hangup; the maintainer, 2026-10-28: "Make hardn stop writing to
// its terminal once a hangup is caught"; cmd-engine.md §2.6). It is
// around every window in which cmd/hardn catches SIGHUP: the screens' and
// -g's apply, hardn confirm's clean-up, and hardn rollback's and --boot's
// hold, the hold's own mark (pkg/engine/hold) coming after any line it is
// still logging. Registered first, before the signals that cancel the
// work, so that no hangup they catch goes unmarked.
type hangupWatch struct {
	c       chan os.Signal
	stopped chan struct{} // closed once c is read to its end
	hup     atomic.Bool
	once    sync.Once // the stop's
}

// watchHangup starts a hangupWatch.
func watchHangup() *hangupWatch {
	w := &hangupWatch{c: make(chan os.Signal, 1), stopped: make(chan struct{})}
	signal.Notify(w.c, syscall.SIGHUP)
	go func() {
		defer close(w.stopped)
		for range w.c {
			w.hup.Store(true)
			terminalGone()
		}
	}()
	return w
}

// seen reports whether a hangup has come since watchHangup.
func (w *hangupWatch) seen() bool { return w.hup.Load() }

// stop ends the watch, SIGHUP back to the disposition it had (signal.Stop,
// after which os/signal sends c nothing more, so c is closed), and
// reports whether a hangup came, one c still held included; a second stop
// ends nothing more and reports the same.
func (w *hangupWatch) stop() bool {
	w.once.Do(func() {
		signal.Stop(w.c)
		close(w.c)
		<-w.stopped
	})
	return w.seen()
}

// endHangup ends hardn after a hangup during a screen's apply
// (screenApplyContext): endByHangup, a seam for the tests.
var endHangup = endByHangup

// hangupWait is how long endByHangup waits for SIGHUP to end hardn
// before it exits with prompt.ExitInterrupted instead.
const hangupWait = 500 * time.Millisecond

// screenApplyContext is the screens' ApplyContext: engineSignals around
// the apply, and after a hangup during it hardn ends once the apply has
// returned, the change rolled back, or finished when the hangup came as
// it finished (the maintainer, 2026-10-28: "Yes, end on hangup"), rather
// than at the screen's next prompt, which under sudo-rs never ends, its
// input kept open (screen-backup.md §4.2). Ctrl-C and SIGTERM during the
// apply are unchanged: rolled back, the screen goes on.
//
// The hangup is caught on a channel of its own (watchHangup), registered
// before engineSignals' registration: a hangup between the two reaches
// this channel alone, so the apply, not cancelled, finishes and hardn
// then ends; registered after, such a hangup would cancel the apply
// unseen here, and the screen would go on. The watch marks hardn's
// terminal gone as the hangup comes (terminalGone), so that what the
// screen writes after it, the steps' redraws, the report, is dropped and
// cannot block the apply (since next/s3-hangup; the maintainer,
// 2026-10-28: "Make hardn stop writing to its terminal once a hangup is
// caught"). The engine's hold around a
// rollback (hold.Signals) catches SIGHUP and leaves this registration be
// (since next/s3-hangup; the maintainer, 2026-10-28: "Yes, catch it"), so
// a hangup during the rollback, or after it while the engine finishes,
// reaches this channel too, and hardn ends once the apply has returned;
// the commands the rollback runs are in sessions of their own
// (host.OwnSessions), out of the hangup's reach. A hangup after stop, at
// the screen's prompt, ends hardn by Go's default, as before.
func screenApplyContext(ctx context.Context) (context.Context, context.CancelFunc) {
	hup := watchHangup()
	actx, stop := engineSignals(ctx)
	var once sync.Once
	return actx, func() {
		stop()
		once.Do(func() {
			if hup.stop() {
				logging.LogFileOnly("hangup during the apply: hardn ends")
				endHangup()
			}
		})
	}
}

// endByHangup ends hardn as a hangup ends a program that does not catch
// SIGHUP, as prompt.Reraise ends it for Ctrl-C: SIGHUP's default action
// back (signal.Reset, which drops any registration of SIGHUP still live,
// none today), then SIGHUP sent to hardn, which dies of it, so that a
// waiting parent sees a process killed by SIGHUP. If hardn still runs
// hangupWait later, SIGHUP being ignored (SIG_IGN inherited, as under
// nohup, which signal.Reset leaves), it exits with
// prompt.ExitInterrupted (130). It never returns.
func endByHangup() {
	signal.Reset(syscall.SIGHUP)
	_ = syscall.Kill(syscall.Getpid(), syscall.SIGHUP)
	time.Sleep(hangupWait)
	os.Exit(prompt.ExitInterrupted)
}

// engineOptions are the engine's options for the changes cmd/hardn makes
// on sys with the configuration c, all but Progress, which is the caller's:
//
//   - Revert as hardn rollback builds it (Now, Plans) and hardn rollback
//     --boot (Hardn, this hardn's path, resolved), with Log the log file
//     alone (revertFileLog), as hardn confirm's clean-up has it: the
//     engine's and revert's lines are the log's, and what a person reads
//     is the report (output.Human, or a screen's flow). Plans is needed:
//     a DNS change journals its undo as the dns unit's kept plan
//     (revert.OpPlan), which revert.Record refuses without its rollback,
//     and it is the registry a later hardn rollback <id> undoes the
//     journal with. Hardn is not, for DNS: the engine reads it only to
//     arm a lockout-risk plan's revert timer, which a DNS change never
//     arms (brief 2.4); it is set for the next slices' changes, and left
//     empty when this hardn's path cannot be read, so that the engine
//     refuses such a plan ("the revert timer") rather than arm a timer
//     that runs nothing.
//   - Session as hardn confirm reads it (revert.ReadSession, with hardn's
//     environment and pid): journalled with the change, and required of a
//     lockout-risk plan, which the engine refuses when it was not
//     established. Its error goes to the log file.
//   - Backups from hardn.yml (backupPath, enableBackups; §3.16: the
//     engine's backups are taken whatever enableBackups says).
//   - PID, hardn's, for the run lock's holder line.
//   - Detect and Hold the engine's own: detect.Detect, and hold.Signals
//     over Log around an in-process rollback.
func engineOptions(ctx context.Context, sys host.System, c *config.Config) engine.Options {
	o := engine.Options{
		Revert:  revert.Options{Log: revertFileLog{}, Now: revertNow, Plans: revertPlans},
		Backups: c.BackupSettings(),
		Detect:  engineDetect,
		Hold:    engineHold,
		PID:     revertPID(),
	}
	if self, err := revertExecutable(); err == nil {
		o.Revert.Hardn = self
	}
	s, err := revert.ReadSession(ctx, sys, revertGetenv, revertPID())
	if err != nil {
		logging.LogFileOnly("%v", err)
	}
	o.Session = s
	return o
}

// newEngine returns a screen's NewEngine: an engine over sys with o, its
// Progress the flow's, one per change.
func newEngine(sys host.System, o engine.Options) func(progress func(engine.Event)) *engine.Engine {
	return func(progress func(engine.Event)) *engine.Engine {
		o := o
		o.Progress = progress
		return engine.New(sys, o)
	}
}
