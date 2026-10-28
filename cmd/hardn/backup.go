package main

// Backup on the plan/apply engine (refactor plan slice 3, Backup, unit
// (d), next/s3-replace; plan §3.14: the legacy path replaced, not gated):
// the main menu's Backup item runs the Backup screen (pkg/screens/backup),
// which pkg/menu reaches through the hook set here, as it reaches the DNS
// screen (dns.go), so that pkg/menu builds no engine. The engine's options
// are engine.go's. No flag routes to Backup.

import (
	"context"

	"github.com/abbott/hardn/pkg/config"
	"github.com/abbott/hardn/pkg/menu"
	"github.com/abbott/hardn/pkg/output"
	backupscreen "github.com/abbott/hardn/pkg/screens/backup"
	"github.com/abbott/hardn/pkg/ui/component"
	"github.com/abbott/hardn/pkg/ui/nav"
)

// backupMenu is what withBackupScreen sets the Backup item of: pkg/menu's
// main menu, and the tests' record of what was set.
type backupMenu interface{ SetBackup(run func() error) }

// The main menu is a backupMenu.
var _ backupMenu = (*menu.MainMenu)(nil)

// withBackupScreen returns m with its Backup item set to the Backup screen
// on c, the configuration m was given (menu.MainMenu.SetBackup).
func withBackupScreen[M backupMenu](m M, c *config.Config) M {
	m.SetBackup(func() error { return runBackupScreen(context.Background(), c) })
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
// screen reads none. No audit record is written: the audit log's first
// writer is next/p3-startup's (Options.Ran left nil). Each change's apply,
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
	})
}
