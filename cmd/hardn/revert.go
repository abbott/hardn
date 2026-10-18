package main

// Confirm-or-revert on the command line (refactor plan P2, next/p2-revert;
// brief 2.4, decided in brief §2): `hardn confirm`, `hardn rollback <id>`,
// and the notice a run as root shows while a change is journalled.
// pkg/host/revert does the work; this file only wires it in, so main.go is
// unchanged.

import (
	"context"
	"errors"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/fatih/color"
	"github.com/spf13/cobra"

	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/revert"
	"github.com/abbott/hardn/pkg/logging"
)

func init() {
	rootCmd.AddCommand(confirmCmd, rollbackCmd)
	rootCmd.PersistentPreRun = showRevertNotices
}

// Seams for revert_test.go: the effective uid, the host, the session's
// environment and process id, the exit, the clock, where notices go and
// whether a person sees them, and the signals a rollback holds off.
var (
	revertEUID                  = os.Geteuid
	revertHost                  = func() host.System { return host.NewReal(host.Options{RunLog: revertLog{}}) }
	revertGetenv                = os.Getenv
	revertPID                   = os.Getpid
	revertExit                  = os.Exit
	revertNow                   = time.Now
	revertStderr      io.Writer = os.Stderr
	revertNoticeSeen            = stderrIsTerminal
	revertHoldSignals           = holdSignals
)

// revertPlans are the rollbacks of the units whose kept plans a journal
// carries (revert.OpPlan), by unit name. None of those units is in this
// stack yet; each registers its checked Rollback here when it lands
// (docs/safety/revert.md §7).
var revertPlans = map[string]revert.PlanRollback{}

// revertLog writes each line to hardn's log, /var/log/hardn.log, and the
// console, through the legacy logging package: the commands a rollback runs
// and each of its steps. Under the systemd timer the console is the
// journal; under the OpenRC sleeper it is the null device, and the log file
// is the record.
type revertLog struct{}

func (revertLog) Write(p []byte) (int, error) {
	for _, line := range strings.Split(strings.TrimRight(string(p), "\n"), "\n") {
		logging.LogInfo("%s", line)
	}
	return len(p), nil
}

// noRevertNotices replaces the root command's notice for confirm and
// rollback, which report the journal themselves.
func noRevertNotices(*cobra.Command, []string) {}

var confirmCmd = &cobra.Command{
	Use:   "confirm",
	Short: "Keep the change that is waiting for confirmation",
	Long: `Keep the SSH or firewall change hardn applied, which is otherwise rolled back
when its confirm window ends. Run it from a new SSH connection, to the new port
if the change moved it: a confirmation typed into the session that applied the
change proves nothing. Away from SSH (a local console), that session may confirm.

Example:
  sudo hardn confirm`,
	Args:             cobra.NoArgs,
	PersistentPreRun: noRevertNotices,
	Run:              runConfirm,
}

var rollbackCmd = &cobra.Command{
	Use:   "rollback <id>",
	Short: "Undo the journalled change",
	Long: `Undo change <id>: restore the files it backed up, run its inverse steps and
reload the services it changed. The revert timer runs this when a change is not
confirmed in time; run it to undo a change before then, or to finish a rollback
that did not finish. With nothing to undo, it says so and succeeds.

Example:
  sudo hardn rollback 20261018T120000Z-1a2b3c4d`,
	Args:             cobra.ExactArgs(1),
	PersistentPreRun: noRevertNotices,
	Run:              runRollback,
}

// revertPreflight refuses -n, which only previews, and a user other than
// root, who cannot read the journal.
func revertPreflight(name string) bool {
	if dryRun {
		logging.LogError("-n only previews, and hardn %s changes the host: run it without -n", name)
		revertExit(2)
		return false
	}
	if revertEUID() != 0 {
		logging.LogError("hardn %s must be run as root: sudo hardn %s", name, name)
		revertExit(1)
		return false
	}
	return true
}

func runConfirm(*cobra.Command, []string) {
	if !revertPreflight("confirm") {
		return
	}
	ctx := context.Background()
	sys := revertHost()
	opts := revert.Options{Log: revertLog{}, Now: revertNow}
	s, err := revert.ReadSession(ctx, sys, revertGetenv, revertPID())
	if err != nil {
		logging.LogWarning("%v", err)
	}
	res, err := revert.Confirm(ctx, sys, s, opts)
	if err != nil {
		logging.LogError("%v", err)
		var np *revert.NotPendingError
		if errors.As(err, &np) && np.Reverted != nil {
			acknowledge(ctx, sys, opts)
		}
		revertExit(1)
		return
	}
	if res.Local {
		logging.LogWarning("Confirmed from a session that is not over SSH: this does not show that a new SSH connection works.")
	}
	logging.LogSuccess("Change %s confirmed (%s): it will not be rolled back.", res.Journal.ID, res.Journal.Summary)
}

func runRollback(_ *cobra.Command, args []string) {
	if !revertPreflight("rollback") {
		return
	}
	defer revertHoldSignals()()
	ctx := context.Background()
	sys := revertHost()
	opts := revert.Options{Log: revertLog{}, Now: revertNow, Plans: revertPlans}
	res, err := revert.Rollback(ctx, sys, args[0], opts)
	switch {
	case errors.Is(err, revert.ErrID):
		logging.LogError("%v", err)
		revertExit(2)
	case err != nil:
		logging.LogError("%v", err)
		revertExit(1)
	case res.Nothing:
		logging.LogInfo("Nothing to roll back for %s: %s.", args[0], res.Why)
		if res.Reverted != nil {
			acknowledge(ctx, sys, opts)
		}
	default:
		logging.LogSuccess("Change %s (%s) rolled back.", res.Journal.ID, res.Journal.Summary)
	}
}

// holdSignals keeps a hangup, Ctrl-C or SIGTERM from stopping `hardn
// rollback` part-way, until the returned function is called: Go's default
// is to exit on each (os/signal), and a rollback half done is worse than
// one finished (RollbackTimeout bounds it). SIGHUP and SIGINT are ignored,
// which the commands the rollback runs inherit (an ignored signal stays
// ignored across exec, POSIX exec; Go's runtime resets only the handlers
// it installed, runtime/signal_unix.go clearSignalHandlers), so the
// terminal's Ctrl-C and hangup, sent to the whole foreground process group,
// stop neither hardn nor the ufw or systemctl it is running. SIGTERM is
// caught and logged. SIGKILL cannot be held off: the timer, or the next
// `hardn rollback <id>`, takes the rollback up after it.
func holdSignals() (release func()) {
	signal.Ignore(syscall.SIGHUP, syscall.SIGINT)
	term := make(chan os.Signal, 1)
	signal.Notify(term, syscall.SIGTERM)
	done, stopped := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(stopped)
		for {
			select {
			case <-term:
				logging.LogWarning("hardn rollback: SIGTERM received; the rollback finishes first")
			case <-done:
				return
			}
		}
	}()
	return func() {
		signal.Stop(term)
		close(done)
		<-stopped
		// Notify then Stop puts Go's own handler back, ending the Ignore.
		c := make(chan os.Signal, 1)
		signal.Notify(c, syscall.SIGHUP, syscall.SIGINT)
		signal.Stop(c)
	}
}

// showRevertNotices is the next-run notice: what the journal holds, for any
// run as root (the journal is root's). It goes to stderr, so a subcommand's
// stdout (system-details --json) stays its own. A "rolled back by the
// timer" notice is cleared once a person has seen it: after a subcommand
// whose stderr is a terminal. The root command may go on to the legacy main
// menu, which clears the screen, so there it is shown again on later runs;
// a stderr that is a pipe, a file or the null device keeps it too.
func showRevertNotices(cmd *cobra.Command, _ []string) {
	if revertEUID() != 0 {
		return
	}
	ctx := context.Background()
	sys := revertHost()
	opts := revert.Options{}
	st, err := revert.ReadState(ctx, sys, opts)
	if err != nil {
		revertNotice("hardn cannot read its change journal: %v", err)
		return
	}
	reverted := false
	for _, n := range revert.Notices(st, revertNow()) {
		revertNotice("%s", n.Text)
		reverted = reverted || n.Kind == revert.NoticeReverted || n.Kind == revert.NoticeBadOutcome
	}
	if reverted && cmd != rootCmd && revertNoticeSeen() {
		acknowledge(ctx, sys, opts)
	}
}

// revertNotice writes one notice line to stderr, as the legacy warnings
// look.
func revertNotice(format string, args ...any) {
	_, _ = color.New(color.FgYellow).Fprintf(revertStderr, "[WARNING] "+format+"\n", args...)
}

// stderrIsTerminal reports whether stderr is a character device other than
// the null device: a terminal, where a person reads the notice.
func stderrIsTerminal() bool {
	fi, err := os.Stderr.Stat()
	if err != nil || fi.Mode()&os.ModeCharDevice == 0 {
		return false
	}
	null, err := os.Stat(os.DevNull)
	return err != nil || !os.SameFile(fi, null)
}

func acknowledge(ctx context.Context, sys host.System, opts revert.Options) {
	if err := revert.Acknowledge(ctx, sys, opts); err != nil {
		logging.LogWarning("hardn could not clear the rollback notice: %v", err)
	}
}
