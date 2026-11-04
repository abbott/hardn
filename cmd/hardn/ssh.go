package main

// SSH Login on the plan/apply engine (refactor plan slice 6, SSH Login,
// unit (d), next/s6-replace; the maintainer, 2026-11-03: a hard cutover,
// the legacy SSH Login deleted, not frozen): the main menu's SSH Login item
// runs the SSH Login screen (pkg/screens/ssh), which pkg/screens/mainmenu reaches
// through the hook set here, as it reaches the User Management screen
// (users.go), so that pkg/screens/mainmenu builds no engine; and -d plans and applies
// the SSH Login control's Disallow root login through runControls, a
// lockout-risk plan confirmed by the hostname typed on a terminal and
// refused off one, as -w's (gallery-ssh.md §5, decision 8 (a), adopted
// 2026-11-03). The engine's options are engine.go's.

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/abbott/hardn/pkg/config"
	"github.com/abbott/hardn/pkg/engine"
	"github.com/abbott/hardn/pkg/engine/controls/sshchange"
	"github.com/abbott/hardn/pkg/engine/steps"
	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/output"
	"github.com/abbott/hardn/pkg/screens/mainmenu"
	sshscreen "github.com/abbott/hardn/pkg/screens/ssh"
	"github.com/abbott/hardn/pkg/ui/component"
	"github.com/abbott/hardn/pkg/ui/nav"
)

// disallowRoot is -d: the SSH Login control's Root with Want "no",
// Disallow root login (PermitRootLogin no), planned and applied as
// runControls runs a flag's controls, its report output.Human's on stdout.
// hardn's environment (os.Getenv) feeds users.Read's Invoker, which the
// SSH Login control does not weigh: the session's login is sshd's log's
// (sshchange.Reading.Login). The control refuses before anything is
// written, in the decided words, what the screen's Disallow refuses
// (gallery-ssh.md §5, decisions 2 and 3 (a)): sshd's settings not read,
// hardn's old files not yet moved, an Include not first, root's own SSH
// session, a session sshd's log has no line for, a Match block that lets
// root in more, no admin able to log in and become root afterwards. Its
// plan is lockout-risk, so it is confirmed as the screen confirms it, by
// the hostname typed, on a terminal, the revert timer then armed; off a
// terminal it is refused, nothing armed (disallowRootConfirm). -d never
// loosens root login: root login already off (sshd -T's PermitRootLogin
// no) is nothing to change, on Proxmox VE as elsewhere, though there the
// control would plan root key-only (prohibit-password), which the
// screen's Root key-only item offers; root key-only already there is
// nothing to change for -d too (disallowRootControl), a flag being
// idempotent. Under -n, or hardn.yml's dryRun: true, the plan alone, no
// question, nothing written. hardn.yml's SSH settings are not read
// (loadWarnings says so of those set). It returns the exit status: 0
// applied or nothing to change, 3 a plan with changes to make (-n) or
// declined (0, q), 1 refused or failed.
func disallowRoot(c *config.Config) int {
	root := disallowRootControl{sshchange.Root{Want: "no", Getenv: os.Getenv}}
	return runControls(c, []engine.Control{root}, nil, &disallowRootConfirm)
}

// disallowRootControl is -d's control: the SSH Login control's Root, but
// never loosening root login: on Proxmox VE, where the control plans root
// key-only (prohibit-password), root login already off (sshd -T's
// PermitRootLogin no, the reading the control makes) is nothing to
// change, as it is elsewhere; and the control's refusal where root is
// key-only already (sshchange.ErrPVEKeyOnly, SSH_PVE_ROOT_KEY_ONLY: the
// screen's item then read-only) is nothing to change too.
type disallowRootControl struct{ sshchange.Root }

// Plan is Root's, with Proxmox VE's root key-only already, and a plan
// there that would open root's login from no, planned as nothing to
// change.
func (c disallowRootControl) Plan(ctx context.Context, sys host.System, in engine.Input) ([]engine.Change, error) {
	cs, err := c.Root.Plan(ctx, sys, in)
	if errors.Is(err, sshchange.ErrPVEKeyOnly) {
		return nil, nil
	}
	if err != nil || len(cs) == 0 {
		return cs, err
	}
	r, err := sshchange.Read(ctx, sys, in.Facts, in.Config, in.Session, c.Getenv)
	if err != nil {
		return nil, err
	}
	if r.PVE && r.RootValue() == "no" { // Root key-only would loosen it: the screen's item
		return nil, nil
	}
	return cs, nil
}

// disallowRootConfirm is -d's confirmation of its lockout-risk plan: the
// question the screen's Disallow root login asks (gallery-ssh.md §6,
// decided), and the refusal off a terminal (proposed words, refactor plan
// slice 6, unit (d), on -w's pattern): its code the registry's
// PLAN_REFUSED (engine.ErrRefused, a refusal with no code of its own),
// exit status 1.
var disallowRootConfirm = typedConfirm{
	question: "Disallow root login?",
	offTerminal: &steps.Refusal{
		Code: "PLAN_REFUSED",
		Why:  "Turning root login off here risks a lockout",
		Fix:  "run hardn -d from a terminal, which asks you to confirm",
		Err:  fmt.Errorf("%w: a lockout-risk plan, which -d confirms on a terminal alone", engine.ErrRefused),
	},
}

// withSSHScreen returns m with its SSH Login item set to the SSH Login
// screen on c, the configuration m was given (mainmenu.Options.SSH).
func withSSHScreen(m mainmenu.Options, c *config.Config) mainmenu.Options {
	m.SSH = func() error { return runSSHScreen(context.Background(), c) }
	return m
}

// runSSHScreen is the main menu's SSH Login item: the SSH Login screen
// until the user goes back (nil) or quits (nav.ErrQuit), or the input ends
// (prompt.ErrAborted), as runUsersScreen runs the User Management screen:
// on the host hardn builds, with its run log in hardn's log, the facts
// detected once per visit, hardn's environment (os.Getenv), which feeds
// users.Read's Invoker, which the SSH Login control does not weigh (the
// session's login is sshd's log's, sshchange.Reading.Login), through the
// one line reader every prompt shares and stdout's
// render environment; each change on an engine with engineOptions,
// assembled for this visit from c as it is now, and the session the
// screen reads (its line in sshd's log, or the console) the one those
// options hold, as runFirewallScreen gives it, so that the screen and the
// engine never disagree on how hardn's user logged in; c, the
// configuration the main menu holds, whose sshPort is the port form's
// default and sshConfigFile Move old settings' file alone, and which no
// change saves (gallery-ssh.md §5, decision 9 (a)); a dry run when c says
// so as the item is chosen, every change then stopping at its plan. Each
// change's run is a record of the audit log (Options.Ran, auditRan). Each
// change's apply, and nothing else, runs under engineSignals, hardn
// ending after a hangup (screenApplyContext; runDNSScreen says why).
func runSSHScreen(ctx context.Context, c *config.Config) error {
	sys := engineHost()
	facts, err := engineDetect(ctx, sys)
	if err != nil {
		return err
	}
	o := engineOptions(ctx, sys, c)
	env := stdoutEnv()
	return sshscreen.Run(ctx, sshscreen.Options{
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
		Ran:          auditRan(sys, c),
	})
}
