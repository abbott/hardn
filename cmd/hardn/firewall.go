package main

// Firewall on the plan/apply engine (refactor plan slice 4, Firewall,
// unit (d), next/s4-replace; plan §3.14: the legacy path replaced, not
// gated): the main menu's Firewall item runs the Firewall screen
// (pkg/screens/firewall), which pkg/menu reaches through the hook set
// here, as it reaches the Environment screen (environment.go), so that
// pkg/menu builds no engine; and -w plans and applies the Firewall
// control's Enable through runControls, a lockout-risk plan confirmed by
// the hostname typed on a terminal and refused off one
// (gallery-firewall.md §5, decision 4, adopted 2026-11-01). The engine's
// options are engine.go's.

import (
	"context"
	"fmt"
	"sync"

	"github.com/abbott/hardn/pkg/config"
	"github.com/abbott/hardn/pkg/engine"
	firewallctl "github.com/abbott/hardn/pkg/engine/controls/firewall"
	"github.com/abbott/hardn/pkg/engine/steps"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/menu"
	"github.com/abbott/hardn/pkg/output"
	fwscreen "github.com/abbott/hardn/pkg/screens/firewall"
	"github.com/abbott/hardn/pkg/ui/component"
	"github.com/abbott/hardn/pkg/ui/nav"
)

// configureFirewall is -w: the Firewall control's Enable (let SSH through,
// deny by default, start ufw, start it at boot) planned and applied as
// runControls runs a flag's controls, its report output.Human's on stdout
// off a terminal, the canonical flow's on one. A lockout-risk plan, which
// Enable's is wherever it starts ufw or sets the deny policy, is confirmed
// as main menu 4 confirms it, by the hostname typed, on a terminal, the
// revert timer then armed as the engine arms it; off a terminal it is
// refused, nothing armed (enableFirewall). On Proxmox VE the control
// refuses (UFW_NOT_APPLICABLE). Under -n, or hardn.yml's dryRun: true,
// the plan alone, no question, nothing written. It returns the exit
// status: 0 applied or nothing to change, 3 a plan with changes to make
// (-n) or declined (0, q), 1 refused or failed.
func configureFirewall(c *config.Config) int {
	return runControls(c, []engine.Control{firewallctl.Incoming{Action: firewallctl.ActionEnable}}, nil, &enableFirewall)
}

// enableFirewall is -w's confirmation of a lockout-risk plan: the
// question its screen's Enable asks (gallery-firewall.md §6, decided), and
// the refusal off a terminal (proposed words, refactor plan slice 4, unit
// (d), on -e's decided pattern): its code the registry's PLAN_REFUSED
// (engine.ErrRefused, a refusal with no code of its own), exit status 1.
var enableFirewall = typedConfirm{
	question: "Enable the firewall?",
	offTerminal: &steps.Refusal{
		Code: "PLAN_REFUSED",
		Why:  "Enabling the firewall here risks a lockout",
		Fix:  "run hardn -w from a terminal, which asks you to confirm",
		Err:  fmt.Errorf("%w: a lockout-risk plan, which -w confirms on a terminal alone", engine.ErrRefused),
	},
}

// firewallMenu is what withFirewallScreen sets the Firewall item of:
// pkg/menu's main menu, and the tests' record of what was set.
type firewallMenu interface {
	SetFirewall(run func() error, onPVE func() bool)
}

// The main menu is a firewallMenu.
var _ firewallMenu = (*menu.MainMenu)(nil)

// withFirewallScreen returns m with its Firewall item set to the Firewall
// screen on c, the configuration m was given, and its description to
// Proxmox VE's where the host is Proxmox VE, as the screen reads it
// (menu.MainMenu.SetFirewall): detect's Proxmox VE installed, read through
// engineDetect once, as the main menu is first drawn, a fact that does not
// change while hardn runs.
func withFirewallScreen[M firewallMenu](m M, c *config.Config) M {
	onPVE := sync.OnceValue(func() bool {
		facts, err := engineDetect(context.Background(), statusHost())
		return err == nil && facts.Proxmox.Installed == detect.Yes
	})
	m.SetFirewall(func() error { return runFirewallScreen(context.Background(), c) }, onPVE)
	return m
}

// runFirewallScreen is the main menu's Firewall item: the Firewall screen
// until the user goes back (nil) or quits (nav.ErrQuit), or the input ends
// (prompt.ErrAborted), as runEnvironmentScreen runs the Environment
// screen: on the host hardn builds, with its run log in hardn's log, the
// facts detected once per visit, through the one line reader every prompt
// shares and stdout's render environment; each change on an engine with
// engineOptions, assembled for this visit from c as it is now, and the
// session the screen plans item 1 with the one those options hold, so
// that the screen and the engine never disagree on the port Enable keeps;
// c, the configuration the main menu holds, whose application profiles the
// screen shows and adopts as each change saved them; a dry run when c says
// so as the item is chosen. No audit record is written: the audit log's
// first writer is next/p3-startup's (Options.Ran left nil). Each change's
// apply, and nothing else, runs under engineSignals, hardn ending after a
// hangup (screenApplyContext; runDNSScreen says why).
func runFirewallScreen(ctx context.Context, c *config.Config) error {
	sys := engineHost()
	facts, err := engineDetect(ctx, sys)
	if err != nil {
		return err
	}
	o := engineOptions(ctx, sys, c)
	env := stdoutEnv()
	return fwscreen.Run(ctx, fwscreen.Options{
		Prompter:     nav.Prompter{Ctx: component.NewCtx(env), In: engineInput(), Out: env.Writer(engineStdout)},
		System:       sys,
		Facts:        facts,
		Session:      o.Session,
		Config:       c,
		NewEngine:    newEngine(sys, o),
		ApplyContext: screenApplyContext,
		DryRun:       c.DryRun,
		LogPath:      output.LegacyLogPath,
		Now:          revertNow,
	})
}
