package main

// Confirm-or-revert on the command line (refactor plan P2, next/p2-revert;
// brief 2.4, decided in brief §2): `hardn confirm`, `hardn rollback <id>`,
// `hardn rollback --boot` (P3, next/p3-revert-boot), and the notice a run
// as root shows while a change is journalled.
// pkg/host/revert does the work; this file only wires it in, so main.go is
// unchanged.

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/abbott/hardn/pkg/engine/hold"
	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/revert"
	"github.com/abbott/hardn/pkg/host/revert/plans"
	"github.com/abbott/hardn/pkg/ui/component"
)

func init() {
	rollbackCmd.Flags().BoolVar(&rollbackAbandon, "abandon", false,
		"give up the unfinished rollback of change <id>, one that cannot finish, leaving the host as the rollback left it")
	rollbackCmd.Flags().BoolVar(&rollbackBoot, "boot", false,
		"what hardn's boot unit runs, with no <id>: re-arm the revert timer of the change waiting for confirmation, or roll it back once its deadline has passed")
	rootCmd.AddCommand(confirmCmd, rollbackCmd)
	rootCmd.PersistentPreRun = rootPreRun
}

// rootPreRun is every command's first step but confirm's and rollback's
// (noRevertNotices): --no-color's warning (warnNoColor), then the revert
// notices (showRevertNotices).
func rootPreRun(cmd *cobra.Command, args []string) {
	warnNoColor()
	showRevertNotices(cmd, args)
}

// rollbackAbandon is `hardn rollback --abandon <id>` (refactor plan P3,
// next/p3-rollback-abandon; P-35 (a), decided 2026-10-19): give up the
// unfinished rollback of change <id> instead of running it.
var rollbackAbandon bool

// rollbackBoot is `hardn rollback --boot` (refactor plan P3,
// next/p3-revert-boot; P-34 (c), decided 2026-10-19): what the boot unit
// runs at boot, before sshd starts (revert.Boot).
var rollbackBoot bool

// Seams for revert_test.go: the effective uid, the host, the session's
// environment and process id, the exit, the clock, where notices go and
// whether a person sees them, and the signals a rollback holds off.
var (
	revertEUID        = os.Geteuid
	revertHost        = func() host.System { return host.NewReal(host.Options{RunLog: revertLog{}}) }
	revertGetenv      = os.Getenv
	revertPID         = os.Getpid
	revertExit        = os.Exit
	revertNow         = time.Now
	revertNoticeSeen  = stderrIsTerminal
	revertHoldSignals = holdSignals
	revertExecutable  = executable
	revertInit        = detect.DetectInit
)

// executable is this hardn's path, resolved: what the boot unit ran, which
// revert.Boot compares with the binary the journal's timer runs.
func executable() (string, error) {
	p, err := os.Executable()
	if err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(p)
}

// revertPlans are the rollbacks of the units whose kept plans a journal
// carries (revert.OpPlan), by unit name: root's keys on Proxmox VE
// (pve-keys), sudoers, dns and alpine-updates, from pkg/host/revert/plans,
// which decodes each plan strictly and runs the unit's own check before
// its rollback acts (docs/safety/revert-plans.md). The engine passes the
// same registry to revert.Record, which refuses a step of any other unit.
var revertPlans = plans.Registry()

// revertLog writes each line to hardn's log, /var/log/hardn.log, and the
// console, stderr (say): the commands a rollback runs and each of its
// steps. Under the systemd timer the console is the journal; under the
// OpenRC sleeper it is the null device, and the log file is the record.
type revertLog struct{}

func (revertLog) Write(p []byte) (int, error) {
	for _, line := range strings.Split(strings.TrimRight(string(p), "\n"), "\n") {
		sayInfo("%s", line)
	}
	return len(p), nil
}

// noRevertNotices replaces the root command's notice for confirm and
// rollback, which report the journal themselves; --no-color's warning
// stays (warnNoColor).
func noRevertNotices(*cobra.Command, []string) { warnNoColor() }

var confirmCmd = &cobra.Command{
	Use:   "confirm",
	Short: "Keep the change that is waiting for confirmation",
	Long: `Keep the SSH or firewall change hardn applied, which is otherwise rolled back
when its confirm window ends. Run it from a new SSH connection, to the new port
if the change moved it: a confirmation typed into the session that applied the
change proves nothing. Away from SSH (a local console), that session may confirm.

Once a port change is confirmed from a new SSH connection, it also cleans up:
the old port, kept open during the confirm window, is dropped from
/etc/ssh/sshd_config.d/00-hardn.conf, sshd is reloaded, and the ufw rule hardn
added for that port is deleted. A console confirms and leaves that to a new
SSH connection.

Example (doas in place of sudo on a doas host):
  sudo hardn confirm`,
	Args:             cobra.NoArgs,
	PersistentPreRun: noRevertNotices,
	Run:              runConfirm,
}

var rollbackCmd = &cobra.Command{
	Use:   "rollback [--abandon] <id> | --boot",
	Short: "Undo the journalled change",
	Long: `Undo change <id>: restore the files it backed up, run its inverse steps and
reload the services it changed. The revert timer runs this when a change is not
confirmed in time; run it to undo a change before then, or to finish a rollback
that did not finish. With nothing to undo, it says so and succeeds.

A rollback that did not finish blocks every new change until it does. With
--abandon, a rollback that cannot ever finish (its backups are gone, an undo
command was removed) is given up instead: the host stays as the rollback left
it, hardn lists what the rollback did and did not undo, and keeps its record.

With --boot, and no <id>, it is what hardn's boot unit runs at boot, before sshd
starts: a reboot stops the revert timer, so it re-arms it for what remains of
the confirm window, or rolls the change back once its deadline has passed. It
acts only on a journal this hardn binary wrote.

Examples (doas in place of sudo on a doas host):
  sudo hardn rollback 20261018T120000Z-1a2b3c4d
  sudo hardn rollback --abandon 20261018T120000Z-1a2b3c4d`,
	Args:             rollbackArgs,
	PersistentPreRun: noRevertNotices,
	Run:              runRollback,
}

// rollbackArgs is <id>, exactly one, or none with --boot, which does not
// go with --abandon.
func rollbackArgs(cmd *cobra.Command, args []string) error {
	if rollbackBoot && rollbackAbandon {
		return errors.New("hardn rollback: --boot and --abandon do not go together")
	}
	if rollbackBoot && len(args) > 0 {
		return fmt.Errorf("hardn rollback --boot takes no <id>: it acts on the change the journal holds; got %q", args)
	}
	if rollbackBoot {
		return nil
	}
	return cobra.ExactArgs(1)(cmd, args)
}

// revertElevation is this host's elevation tools.
func revertElevation() detect.Elevation {
	return detect.FindElevation(context.Background(), revertHost())
}

// revertElevate is the tool that runs a command as root on this host:
// sudo, doas, or revert.NoElevation where neither is installed
// (revert.Elevate).
func revertElevate() string { return revert.Elevate(revertElevation()) }

// revertAsRoot is cmd run as root on this host: after its tool, bare where
// neither sudo nor doas is installed.
func revertAsRoot(cmd string) string {
	if e := revertElevate(); e != revert.NoElevation {
		return e + " " + cmd
	}
	return cmd
}

// revertPreflight refuses -n, which only previews, and a user other than
// root, who cannot read the journal.
func revertPreflight(name string) bool {
	if dryRun {
		sayError("-n only previews, and hardn %s changes the host: run it without -n", name)
		revertExit(2)
		return false
	}
	if revertEUID() != 0 {
		sayError("hardn %s must be run as root: %s %s", name, revert.Hardn(revertElevation()), name)
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
		sayWarning("%v", err)
	}
	if code := confirmAndCleanUp(ctx, sys, s, opts); code != 0 {
		revertExit(code)
	}
}

func runRollback(_ *cobra.Command, args []string) {
	if rollbackBoot {
		runBoot()
		return
	}
	if rollbackAbandon {
		runAbandon(args[0])
		return
	}
	if !revertPreflight("rollback") {
		return
	}
	defer watchHangup().stop() // a hangup's mark, whatever the hold is busy logging
	defer revertHoldSignals()()
	ctx := context.Background()
	sys := revertHost()
	opts := revert.Options{Log: revertLog{}, Now: revertNow, Plans: revertPlans}
	res, err := revert.Rollback(ctx, sys, args[0], opts)
	switch {
	case errors.Is(err, revert.ErrID):
		sayError("%v", err)
		revertExit(2)
	case err != nil:
		sayError("%v", err)
		revertExit(1)
	case res.Nothing:
		sayInfo("Nothing to roll back for %s: %s.", args[0], res.Why)
		if res.Reverted != nil {
			acknowledge(ctx, sys, opts)
		}
	default:
		saySuccess("Change %s (%s) rolled back.", res.Journal.ID, res.Journal.Summary)
	}
}

// runAbandon is `hardn rollback --abandon <id>` (P-35 (a)): revert.Abandon,
// then what the rollback did and did not undo, from its record. As `hardn
// rollback`: root only (exit 1), -n refused (exit 2), an id that is not one
// exit 2; refused, saying what to do instead, while the rollback runs and
// for anything but an unfinished rollback of <id> (exit 1). It runs no
// command and makes one rename, so it holds no signals.
func runAbandon(id string) {
	if !revertPreflight("rollback --abandon") {
		return
	}
	res, err := revert.Abandon(context.Background(), revertHost(), id, revert.Options{Log: revertLog{}, Now: revertNow})
	switch {
	case errors.Is(err, revert.ErrID):
		sayError("%v", err)
		revertExit(2)
		return
	case err != nil:
		sayError("%v", err)
		rb := filepath.Join(revert.DefaultDir, revert.RollbackName)
		switch {
		case errors.Is(err, host.ErrLocked):
			sayInfo("Run it again once that hardn has finished: one stopped inside the lock (Ctrl-Z) keeps it until it goes on or ends.")
		case errors.Is(err, revert.ErrNoLock):
			sayInfo("The journal lock %s could not be taken, so nothing was given up: free space on its file system, or put right what the error names, and run it again. Moving %s aside by hand, as root, gives its rollback up the same way, without this report, and leaves the host as it is.",
				filepath.Join(revert.DefaultDir, revert.JournalLockName), rb)
		case errors.Is(err, revert.ErrJournal) || errors.Is(err, revert.ErrUnsafe):
			sayInfo("If the file it names is %s and this hardn cannot act on it, moving that file aside by hand, as root, gives its rollback up the same way, without this report, and leaves the host as it is.", rb)
		}
		revertExit(1)
		return
	}
	j := res.Journal
	done, left := revert.Unfinished(j)
	ok := func(text string) component.Message { return component.Message{Kind: component.MessageOK, Text: text} }
	warn := func(text string) component.Message { return component.Message{Kind: component.MessageWarn, Text: text} }
	info := func(text string) component.Message { return component.Message{Text: text} }
	run := []component.Message{
		ok(fmt.Sprintf("Gave up the rollback of change %s (%s).", j.ID, j.Summary)),
		warn("The host stays as the rollback left it: hardn will not undo what the rollback did not."),
	}
	if j.Rollback == nil {
		run = append(run, warn("The rollback stopped before it recorded its progress, so it may have done more than its record shows."))
	}
	if res.RunnerUnknown {
		run = append(run, warn(fmt.Sprintf("Its record does not name the process that ran it, so hardn cannot tell whether a `hardn rollback %s` is still running it: check with `ps`. One that still runs writes its record again as it goes, and the next run of hardn reports it.", j.ID)))
	}
	run = append(run, info("Undone by the rollback, by its record:"))
	for _, line := range orNothing(done) {
		run = append(run, info("  - "+line))
	}
	run = append(run, warn("Not undone, or not finished:"))
	for _, line := range orNothing(left) {
		run = append(run, warn("  - "+line))
	}
	if res.SSHUnchecked {
		run = append(run, warn(fmt.Sprintf("The backups were not all restored, so sshd's files on disk may be a mix of the change's and the ones before it, which no check has passed, and sshd reads them the next time it starts (a restart, a package upgrade, a reboot). Check them now with `%s` and fix what it reports: the rollback's restore lines in /var/log/hardn.log say which files came back.", revertAsRoot("sshd -t"))))
	}
	run = append(run, info(fmt.Sprintf("The rollback's record is kept in %s, and /var/log/hardn.log has what each of its steps did. New changes can be applied again.", res.Kept)))
	sayRun(run...)
}

// runBoot is `hardn rollback --boot` (P-34 (c)): revert.Boot, with this
// hardn's own path and the init system detected now, and the plan
// rollbacks `hardn rollback` passes. As `hardn rollback`: root only (exit
// 1), -n refused (exit 2), signals held, since it may roll back. Exit 0 when
// it had nothing to do, re-armed the timer or rolled the change back; 1 when
// it left a change or a rollback to the person (revert.BootLeft) or its
// rollback did not finish, so that the boot unit shows as failed. sshd is
// only ordered after the unit, so it starts either way.
func runBoot() {
	if !revertPreflight("rollback --boot") {
		return
	}
	defer watchHangup().stop() // a hangup's mark, whatever the hold is busy logging
	defer revertHoldSignals()()
	ctx := context.Background()
	sys := revertHost()
	self, err := revertExecutable()
	if err != nil {
		sayError("hardn rollback --boot: this hardn's own path cannot be read, so whether it wrote the change journal cannot be told, and hardn does not act on it: %v", err)
		revertExit(1)
		return
	}
	opts := revert.Options{Log: revertLog{}, Now: revertNow, Hardn: self, Plans: revertPlans}
	res, err := revert.Boot(ctx, sys, revertInit(ctx, sys), opts)
	what := "At boot"
	if j := res.Change; j.ID != "" {
		what = "At boot, change " + j.ID + " (" + j.Summary + ")"
	}
	var run []component.Message
	switch {
	case err != nil:
		run = append(run, component.Message{Kind: component.MessageBad, Text: fmt.Sprintf("%s: %s: %v", what, res.Why, err)})
	case res.Action == revert.BootLeft:
		run = append(run, component.Message{Kind: component.MessageWarn, Text: fmt.Sprintf("%s: %s.", what, res.Why)})
	case res.Action == revert.BootNothing:
		run = append(run, component.Message{Text: fmt.Sprintf("%s: nothing to do: %s.", what, res.Why)})
	default:
		run = append(run, component.Message{Kind: component.MessageOK, Text: fmt.Sprintf("%s: %s.", what, res.Why)})
	}
	if res.Removed {
		run = append(run, component.Message{Text: "No change is journalled, so hardn's boot unit is removed; hardn installs it again before the next change that could lock you out."})
	}
	if res.RemoveErr != nil {
		run = append(run, component.Message{Kind: component.MessageWarn, Text: fmt.Sprintf("hardn's boot unit was not removed: %v", res.RemoveErr)})
	}
	sayRun(run...)
	if err != nil || res.Action == revert.BootLeft {
		revertExit(1)
	}
}

// orNothing is lines, or a line saying there are none.
func orNothing(lines []string) []string {
	if len(lines) == 0 {
		return []string{"nothing"}
	}
	return lines
}

// holdSignals keeps a hangup, Ctrl-C or SIGTERM from stopping `hardn
// rollback` (and its --boot) part-way, until the returned function is
// called: Go's default is to exit on each (os/signal), and a rollback half
// done is worse than one finished (RollbackTimeout bounds it). It is
// pkg/engine/hold's (next/p3-engine-boot), the engine's own: SIGINT is
// ignored, which the commands the rollback runs inherit, and they run in
// sessions of their own (host.OwnSessions), so the terminal's Ctrl-C and
// hangup, sent to the whole foreground process group, stop neither hardn
// nor the ufw or systemctl it is running; SIGTERM and SIGHUP are caught
// and logged as warnings, "hardn rollback: SIGTERM received; the rollback
// finishes first", as before, and the same for SIGHUP (since
// next/s3-hangup); and the release gives the three back their disposition
// before the hold. SIGKILL cannot be held off: the timer, or the next
// `hardn rollback <id>`, takes the rollback up after it.
func holdSignals() (release func()) { return hold.SignalsAs("hardn rollback", revertWarnLog{}) }

// revertWarnLog writes each line as a warning to hardn's log and the
// console, as revertLog writes its lines as information.
type revertWarnLog struct{}

func (revertWarnLog) Write(p []byte) (int, error) {
	for _, line := range strings.Split(strings.TrimRight(string(p), "\n"), "\n") {
		sayWarning("%s", line)
	}
	return len(p), nil
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

// revertNotice writes one notice line to stderr, a warning, and to stderr
// only, not hardn's log: every root run shows it (PersistentPreRun), so
// logging it would append it to the log on each run, and -p would write
// into the log it prints.
func revertNotice(format string, args ...any) {
	writeConsole(component.Messages{{Kind: component.MessageWarn, Text: fmt.Sprintf(format, args...)}})
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
		sayWarning("hardn could not clear the rollback notice: %v", err)
	}
}
