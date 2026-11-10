package main

// Firewall on the plan/apply engine (refactor plan slice 4, Firewall,
// unit (d), next/s4-replace; plan §3.14: the legacy path replaced, not
// gated): the main menu's Firewall item runs the Firewall screen
// (pkg/screens/firewall), which pkg/screens/mainmenu reaches through the hook set
// here, as it reaches the Environment screen (environment.go), so that
// pkg/screens/mainmenu builds no engine; and -w plans and applies the Firewall
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
	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/output"
	fwscreen "github.com/abbott/hardn/pkg/screens/firewall"
	"github.com/abbott/hardn/pkg/screens/mainmenu"
	"github.com/abbott/hardn/pkg/ui/component"
	"github.com/abbott/hardn/pkg/ui/nav"
)

// configureFirewall is -w: the Firewall control's Enable (let SSH through,
// deny by default, start ufw, start it at boot) planned and applied as
// runControls runs a flag's controls, its report output.Human's on stdout
// off a terminal, the canonical flow's on one. A lockout-risk plan, which
// Enable's is wherever it starts ufw or sets the deny policy, is confirmed
// as main menu 3 confirms it, by the hostname typed, on a terminal, the
// revert timer then armed as the engine arms it; off a terminal it is
// refused, nothing armed (enableFirewall). A plain plan ("Start ufw at
// boot" alone, ufw already running) is applied unasked, on a terminal or
// off, as any flag's. On Proxmox VE the control
// refuses (UFW_NOT_APPLICABLE). On Rocky Linux and AlmaLinux it is refused
// before anything is planned, whatever hardn.yml says (FIREWALL_EL_READ_ONLY,
// firewallEL): the firewall is the Firewall screen's there
// (el-firewalld.md §1.7). Under -n, or hardn.yml's dryRun: true,
// the plan alone, no question, nothing written. It returns the exit
// status: 0 applied or nothing to change, 3 a plan with changes to make
// (-n) or declined (0, q), 1 refused or failed.
func configureFirewall(c *config.Config) int {
	ctx := context.Background()
	sys := engineHost()
	if facts, err := engineDetect(ctx, sys); err == nil && facts.Platform.Distro.EL() {
		return refuseControls(sys, c, firewallEL)
	}
	return runControls(c, []engine.Control{firewallctl.Incoming{Action: firewallctl.ActionEnable}}, nil, &enableFirewall)
}

// firewallEL is -w's refusal on Rocky Linux and AlmaLinux (el-firewalld.md
// §1.7; proposed words).
var firewallEL = &steps.Refusal{
	Code: "FIREWALL_EL_READ_ONLY",
	Why:  "hardn doesn't configure the firewall from -w on Rocky Linux or AlmaLinux",
	Fix:  "use the Firewall screen",
	Err:  steps.ErrELFirewall,
}

// refuseControls is a flag's refusal before its plan, err, reported and
// audited as runControls reports and audits the engine's refusal, nothing
// planned or applied; it returns the exit status, 1.
func refuseControls(sys host.System, c *config.Config, err error) int {
	run := output.Run{Mode: output.ModeApply, Err: err}
	if c.DryRun {
		run.Mode = output.ModePlan
	}
	if err := output.Emit(output.Streams{Stdout: engineStdout, Out: stdoutEnv()}, output.FormatHuman, run, revertNow()); err != nil {
		sayError("%v", err)
	}
	audit(sys, c, run)
	return endControls(run, func() bool { return false })
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

// withFirewallScreen returns m with its Firewall item set to the Firewall
// screen on c, the configuration m was given, and its description to
// Proxmox VE's where the host is Proxmox VE, and firewalld's where it is
// Rocky Linux or AlmaLinux, as the screen reads them
// (mainmenu.Options.Firewall): detect's Proxmox VE installed and its
// distribution, read through engineDetect once, as the main menu is first
// drawn, facts that do not change while hardn runs.
func withFirewallScreen(m mainmenu.Options, c *config.Config) mainmenu.Options {
	type kind struct{ pve, el bool }
	read := sync.OnceValue(func() kind {
		facts, err := engineDetect(context.Background(), statusHost())
		if err != nil {
			return kind{}
		}
		pve, el := mainmenu.FirewallOf(facts)
		return kind{pve: pve, el: el}
	})
	m.Firewall = func() error { return runFirewallScreen(context.Background(), c) }
	m.FirewallPVE, m.FirewallEL = func() bool { return read().pve }, func() bool { return read().el }
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
// so as the item is chosen. Each change's run is a record of the audit
// log (Options.Ran, auditRan). Each change's apply, and nothing else,
// runs under engineSignals, hardn ending after a hangup
// (screenApplyContext; runDNSScreen says why).
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
		Ran:          auditRan(sys, c),
	})
}
