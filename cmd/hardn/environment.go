package main

// Environment on the plan/apply engine (refactor plan slice 9,
// Environment, unit (d), next/s9-replace; plan §3.14: the legacy path
// replaced, not gated): the main menu's Environment item runs the
// Environment screen (pkg/screens/environment), which pkg/menu reaches
// through the hook set here, as it reaches the DNS and Backup screens
// (dns.go, backup.go), so that pkg/menu builds no engine; and -e and
// setup-sudo-env plan and apply the Environment control as -g runs the
// DNS control (runControls), refusing a lockout-risk plan. The engine's
// options are engine.go's.

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/abbott/hardn/pkg/config"
	"github.com/abbott/hardn/pkg/engine"
	envctl "github.com/abbott/hardn/pkg/engine/controls/environment"
	"github.com/abbott/hardn/pkg/engine/steps"
	"github.com/abbott/hardn/pkg/logging"
	"github.com/abbott/hardn/pkg/menu"
	"github.com/abbott/hardn/pkg/output"
	envscreen "github.com/abbott/hardn/pkg/screens/environment"
	"github.com/abbott/hardn/pkg/ui/component"
	"github.com/abbott/hardn/pkg/ui/nav"
)

// environmentGetenv is hardn's environment as the Environment screen and
// -e read it: who ran hardn through sudo or doas (SUDO_USER, DOAS_USER)
// and HARDN_CONFIG as the tool passed it on. A seam for the tests.
var environmentGetenv = os.Getenv

// keepEnvironment is -e and setup-sudo-env: the Environment control
// keeping HARDN_CONFIG for the user who ran hardn through sudo or doas
// (envctl.Controls, Keep with Stop false) planned and applied as
// runControls runs a flag's controls, without a question, as -g's
// (gallery-environment.md §5, Made here, adopted 2026-10-29); its report
// output.Human's
// on stdout; under -n, or hardn.yml's dryRun: true, the plan alone,
// nothing written. A host the control refuses (no tool, no user, a file
// of hardn's edited, doas with no rule of hardn's for the user, ...) is
// reported with its code and how to go on, and not applied. A
// lockout-risk plan (on doas, hardn's password rule rewritten) is refused
// too, naming main menu 8, whose screen confirms it by the hostname
// typed: a flag that does not ask never arms the revert timer
// (refuseLockoutRisk). It returns the exit status: 0 applied or nothing
// to change, 3 a plan with changes to make, 1 refused or failed.
func keepEnvironment(c *config.Config) int {
	return runControls(c, envctl.Controls(environmentGetenv), refuseLockoutRisk)
}

// errLockoutRisk is -e's and setup-sudo-env's refusal of a lockout-risk
// plan (proposed words, refactor plan slice 9, unit (d)): its code is the
// registry's PLAN_REFUSED (engine.ErrRefused, a refusal with no code of
// its own), exit status 1.
var errLockoutRisk = &steps.Refusal{
	Code: "PLAN_REFUSED",
	Why:  "Keeping HARDN_CONFIG here risks a lockout",
	Fix:  "keep it from main menu 8, Environment, which asks you to confirm",
	Err:  fmt.Errorf("%w: a lockout-risk plan, which -e and setup-sudo-env do not confirm", engine.ErrRefused),
}

// refuseLockoutRisk refuses p when it is lockout-risk (engine.RiskLockout):
// such a plan is confirmed by the hostname typed and armed with the
// revert timer, which a flag that applies without asking must not arm
// unasked (gallery-environment.md §5, Made here, adopted 2026-10-29). Any
// other plan it leaves to the engine.
func refuseLockoutRisk(p *engine.Plan) error {
	if p.Risk == engine.RiskLockout {
		return errLockoutRisk
	}
	return nil
}

// errNotRoot is what checkRoot reports for a user other than root.
var errNotRoot = errors.New("not root")

// checkRoot is setup-sudo-env's root check, the root command's
// (main.go:141-153, kept there as it is, as the characterization goldens
// cite main.go's lines): the current user looked up, and hardn ended with
// exit status 1 for a failed lookup or a user other than root, in the
// root command's words. It returns errNotRoot after exit, which the tests
// replace.
func checkRoot() error {
	currentUser, err := lookupCurrentUser()
	if err != nil {
		logging.LogError("Failed to get current user: %v", err)
		exit(1)
		return err
	}
	if currentUser.Uid != "0" {
		logging.LogError("This script needs to be run as root.")
		fmt.Println("For Ubuntu/Debian run: `sudo hardn` or switch to root `sudo -i`")
		fmt.Println("For Alpine run: `sudo hardn` or switch to root `su`")
		exit(1)
		return errNotRoot
	}
	return nil
}

// environmentMenu is what withEnvironmentScreen sets the Environment item
// of: pkg/menu's main menu, and the tests' record of what was set.
type environmentMenu interface{ SetEnvironment(run func() error) }

// The main menu is an environmentMenu.
var _ environmentMenu = (*menu.MainMenu)(nil)

// withEnvironmentScreen returns m with its Environment item set to the
// Environment screen on c, the configuration m was given
// (menu.MainMenu.SetEnvironment).
func withEnvironmentScreen[M environmentMenu](m M, c *config.Config) M {
	m.SetEnvironment(func() error { return runEnvironmentScreen(context.Background(), c) })
	return m
}

// runEnvironmentScreen is the main menu's Environment item: the
// Environment screen until the user goes back (nil) or quits
// (nav.ErrQuit), or the input ends (prompt.ErrAborted), as runDNSScreen
// runs the DNS screen: on the host hardn builds, with its run log in
// hardn's log, the facts detected once per visit, hardn's environment
// (environmentGetenv), through the one line reader every prompt shares
// and stdout's render environment; each change on an engine with
// engineOptions, assembled for this visit from c as it is now (its backup
// settings, the session a lockout-risk change's journal names, hardn's
// path for the revert timer); a dry run when c says so as the item is
// chosen, item 1 then stopping at its plan. No Config: the screen reads
// and saves no setting of hardn.yml. No audit record is written: the
// audit log's first writer is next/p3-startup's (Options.Ran left nil).
// Each change's apply, and nothing else, runs under engineSignals,
// hardn ending after a hangup (screenApplyContext; runDNSScreen says
// why).
func runEnvironmentScreen(ctx context.Context, c *config.Config) error {
	sys := engineHost()
	facts, err := engineDetect(ctx, sys)
	if err != nil {
		return err
	}
	env := stdoutEnv()
	return envscreen.Run(ctx, envscreen.Options{
		Prompter:     nav.Prompter{Ctx: component.NewCtx(env), In: engineInput(), Out: env.Writer(engineStdout)},
		System:       sys,
		Facts:        facts,
		Getenv:       environmentGetenv,
		NewEngine:    newEngine(sys, engineOptions(ctx, sys, c)),
		ApplyContext: screenApplyContext,
		DryRun:       c.DryRun,
		LogPath:      output.LegacyLogPath,
		Now:          revertNow,
	})
}
