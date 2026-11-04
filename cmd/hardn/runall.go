package main

// Run All on its planner (refactor plan slice 8, Run All, unit (d)): the
// main menu's Run All item (7) runs the Run All screen
// (pkg/screens/runall), which pkg/screens/mainmenu reaches through the hook set here,
// as it reaches the SSH Login screen (ssh.go), so that pkg/screens/mainmenu builds no
// engine (gallery-runall.md §5, decision 7 (a): the item's label and
// description kept); and -r plans with the same planner
// (pkg/engine/runall) and applies its one plan through runControls, the
// hostname typed on a terminal whatever the plan's risk, any plan with
// changes refused off one (decision 8 (a)). The engine's options are
// engine.go's.

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/abbott/hardn/pkg/config"
	"github.com/abbott/hardn/pkg/engine"
	planner "github.com/abbott/hardn/pkg/engine/runall"
	"github.com/abbott/hardn/pkg/engine/steps"
	"github.com/abbott/hardn/pkg/logging"
	"github.com/abbott/hardn/pkg/output"
	"github.com/abbott/hardn/pkg/screens/mainmenu"
	runallscreen "github.com/abbott/hardn/pkg/screens/runall"
	"github.com/abbott/hardn/pkg/ui/component"
	"github.com/abbott/hardn/pkg/ui/nav"
)

// withRunAllScreen returns m with its Run All item set to the Run All
// screen on c, the configuration m was given, and hardn's version
// (mainmenu.Options.RunAll).
func withRunAllScreen(m mainmenu.Options, c *config.Config, version string) mainmenu.Options {
	m.RunAll = func() error { return runRunAllScreen(context.Background(), c, version) }
	return m
}

// runAllRun is the Run All screen's Run, a seam for the tests, which
// record the options runRunAllScreen gives it.
var runAllRun = runallscreen.Run

// runRunAllScreen is the main menu's Run All item: the Run All screen until
// the user goes back (nil) or quits (nav.ErrQuit), or the input ends
// (prompt.ErrAborted), as runSSHScreen runs the SSH Login screen: on the
// host hardn builds, with its run log in hardn's log, the facts detected
// once per visit, hardn's environment (os.Getenv), from which the
// controls take the session user, through the one line reader every
// prompt shares and stdout's render environment; the planner's reads and
// each change on an engine with engineOptions, assembled for this visit
// from c as it is now (hardn.yml's backups), and the session the planner
// reads the SSH Port row with the one those options hold, so that the
// planner and the engine never disagree on how hardn's user logged in; c,
// the configuration the main menu holds, what Run All is asked, which no
// change saves; hardn's version, in Auto Updates' record; a dry run when c
// says so as the item is chosen, Apply all then stopping at its plan.
// Revert is zero: the journal's options are hardn's own. No audit record
// is written (Options.Ran left nil, as the other screens'). Each change's
// apply, and nothing else, runs under engineSignals, hardn ending after a
// hangup (screenApplyContext; runDNSScreen says why).
func runRunAllScreen(ctx context.Context, c *config.Config, version string) error {
	sys := engineHost()
	facts, err := engineDetect(ctx, sys)
	if err != nil {
		return err
	}
	o := engineOptions(ctx, sys, c)
	env := stdoutEnv()
	return runAllRun(ctx, runallscreen.Options{
		Prompter:     nav.Prompter{Ctx: component.NewCtx(env), In: engineInput(), Out: env.Writer(engineStdout)},
		System:       sys,
		Facts:        facts,
		Session:      o.Session,
		Getenv:       os.Getenv,
		Config:       c,
		NewEngine:    newEngine(sys, o),
		ApplyContext: screenApplyContext,
		DryRun:       c.DryRun,
		LogPath:      output.LegacyLogPath,
		Now:          revertNow,
		Version:      version,
	})
}

// runAllFlag is -r: Run All's planner (planner.Read) on the host hardn
// builds, as the Run All screen reads it, with engineOptions' session and
// engines, hardn's environment (os.Getenv) and hardn's version, writing
// nothing; each row it skips, refused, said on stderr, "<Row> skipped:
// <the control's refusal>" (gallery-runall.md §6); then, where nothing
// refuses Apply all, the planned rows' controls in one plan through
// runControls, its report output.Human's on stdout: on a terminal (stdin
// and stdout) the plan, "Apply this plan?" above the hostname typed
// whatever the plan's risk, then the apply, the revert timer armed and
// sudo hardn confirm said where the engine finds it lockout-risk, as -w
// and -d (runAllConfirm); off a terminal any plan with changes refused,
// PLAN_REFUSED, nothing run; under -n, or hardn.yml's dryRun: true, the
// plan alone. A change pending, or the one plan's refusal, is said on
// stderr in the planner's words and refuses -r, nothing planned. No plan
// saves hardn.yml. It returns the exit status, runControls': 0 applied or
// nothing to change, 3 a plan with changes to make (-n) or declined, 1
// refused or failed; 1 too where the host can't be read.
func runAllFlag(c *config.Config, version string) int {
	ctx := context.Background()
	sys := engineHost()
	facts, err := engineDetect(ctx, sys)
	if err != nil {
		logging.LogError("%v", err)
		return 1
	}
	o := engineOptions(ctx, sys, c)
	r, err := runAllRead(ctx, planner.Input{System: sys, Facts: facts, Config: c, Session: o.Session,
		Getenv: os.Getenv, Version: version, NewEngine: newEngine(sys, o)})
	if err != nil {
		logging.LogError("%v", err)
		return 1
	}
	for _, rw := range r.Refused() {
		fmt.Fprintln(stderr, rw.Title+runAllSkipped+rw.Refusal)
	}
	if r.Refusal != "" {
		fmt.Fprintln(stderr, r.Refusal)
		return 1
	}
	return runControls(c, r.Controls(), nil, &runAllConfirm)
}

// runAllRead is -r's planner.Read, a seam for the tests, which record the
// input runAllFlag gives it (hardn's version among it).
var runAllRead = planner.Read

// runAllSkipped follows a refused row's label on stderr: "<Row> skipped:
// <the control's refusal>" (gallery-runall.md §6, the screen's words).
const runAllSkipped = " skipped: "

// runAllConfirm is -r's confirmation: the question the Run All screen asks
// above the hostname typed (planner.Question, "Apply this plan?"), of any
// plan with changes (always: decision 6 (a)), and the refusal off a
// terminal in gallery-runall.md §6's words, its code the registry's
// PLAN_REFUSED (engine.ErrRefused), exit status 1.
var runAllConfirm = typedConfirm{
	question: planner.Question,
	always:   true,
	offTerminal: &steps.Refusal{
		Code: "PLAN_REFUSED",
		Why:  "hardn -r asks you to type the hostname, and this isn't a terminal",
		Fix:  "run hardn -r from a terminal",
		Err:  fmt.Errorf("%w: a plan with changes, which -r confirms on a terminal alone", engine.ErrRefused),
	},
}

// errRunAllAlone refuses -r with -c, -d, -w, -g or -e: -r runs alone
// (gallery-runall.md §5, decision 8 (a); slice 9's defect 10, where -r -e
// ran -r alone), a usage error, nothing run. -p is not refused: it
// prints the log after -r's run.
var errRunAllAlone = errors.New("hardn -r runs alone: it can't be combined with -c, -d, -w, -g or -e.")

// runAllAlone is errRunAllAlone where -r is given with -c, -d, -w, -g or
// -e; nil otherwise.
func runAllAlone() error {
	if runAll && (createUser || disableRootSSH || configureUfw || configureDns || setupSudoEnv) {
		return errRunAllAlone
	}
	return nil
}
