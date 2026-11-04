package main

// Backup on the plan/apply engine (refactor plan slice 3, Backup, unit
// (d), next/s3-replace; plan §3.14: the legacy path replaced, not gated):
// the main menu's Backup item runs the Backup screen (pkg/screens/backup),
// which pkg/screens/mainmenu reaches through the hook set here, as it reaches the DNS
// screen (dns.go), so that pkg/screens/mainmenu builds no engine. The engine's options
// are engine.go's. No flag routes to Backup.

import (
	"context"

	"github.com/abbott/hardn/pkg/config"
	"github.com/abbott/hardn/pkg/output"
	backupscreen "github.com/abbott/hardn/pkg/screens/backup"
	"github.com/abbott/hardn/pkg/screens/mainmenu"
	"github.com/abbott/hardn/pkg/ui/component"
	"github.com/abbott/hardn/pkg/ui/nav"
)

// withBackupScreen returns m with its Backup item set to the Backup screen
// on c, the configuration m was given (mainmenu.Options.Backup).
func withBackupScreen(m mainmenu.Options, c *config.Config) mainmenu.Options {
	m.Backup = func() error { return runBackupScreen(context.Background(), c) }
	return m
}

// runBackupScreen is the main menu's Backup item: the Backup screen until
// the user goes back (nil) or quits (nav.ErrQuit), or the input ends
// (prompt.ErrAborted), as runDNSScreen runs the DNS screen: on the host
// hardn builds, with its run log in hardn's log, through the one line
// reader every prompt shares and stdout's render environment; each change
// on an engine with engineOptions, assembled for this visit from c as it
// is now, so that a change elsewhere after a path change backs up under
// the new path (Backup's own changes are planned under the settings they
// save, decision 7 (a)); a dry run when c says so as the item is chosen.
// The screen adopts hardn.yml as each change saved it into c, the
// configuration the main menu, and the DNS screen, hold. No Facts: the
// screen reads none. Each change's run is a record of the audit log
// (Options.Ran, auditRan). Each change's apply,
// and nothing else, runs under engineSignals, hardn ending after a hangup
// (screenApplyContext; runDNSScreen says why).
func runBackupScreen(ctx context.Context, c *config.Config) error {
	sys := engineHost()
	env := stdoutEnv()
	return backupscreen.Run(ctx, backupscreen.Options{
		Prompter:     nav.Prompter{Ctx: component.NewCtx(env), In: engineInput(), Out: env.Writer(engineStdout)},
		System:       sys,
		Config:       c,
		NewEngine:    newEngine(sys, engineOptions(ctx, sys, c)),
		ApplyContext: screenApplyContext,
		DryRun:       c.DryRun,
		LogPath:      output.LegacyLogPath,
		Now:          revertNow,
		Ran:          auditRan(sys, c),
	})
}
