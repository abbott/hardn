package main

// DNS on the plan/apply engine (refactor plan slice 2, the DNS pilot,
// unit (d), next/s2-replace; the maintainer, 2026-10-22: the legacy path
// replaced, not gated): the main menu's DNS item runs the DNS screen
// (pkg/screens/dns), which pkg/menu reaches through the hook set here, so
// that pkg/menu builds no engine. The engine's options are engine.go's.

import (
	"context"

	"github.com/abbott/hardn/pkg/config"
	"github.com/abbott/hardn/pkg/menu"
	"github.com/abbott/hardn/pkg/output"
	dnsscreen "github.com/abbott/hardn/pkg/screens/dns"
	"github.com/abbott/hardn/pkg/ui/component"
	"github.com/abbott/hardn/pkg/ui/nav"
)

// withDNSScreen returns m with its DNS item set to the DNS screen on c,
// the configuration m was given (menu.MainMenu.SetDNS).
func withDNSScreen(m *menu.MainMenu, c *config.Config) *menu.MainMenu {
	m.SetDNS(func() error { return runDNSScreen(context.Background(), c) })
	return m
}

// runDNSScreen is the main menu's DNS item: the DNS screen until the user
// goes back (nil) or quits (nav.ErrQuit), or the input ends
// (prompt.ErrAborted). It runs on the host hardn builds, with its run log
// in hardn's log, the facts detected once, through the one line reader
// every prompt shares and stdout's render environment; each change on an
// engine with engineOptions, a dry run when c says so as the item is
// chosen (the dry-run setting as the legacy menus read it). The screen
// adopts hardn.yml as each change saved it into c (dnsscreen.Options), the
// configuration the main menu holds. No audit record is written: the
// audit log's first writer is next/p3-startup's (Options.Ran left nil).
func runDNSScreen(ctx context.Context, c *config.Config) error {
	sys := engineHost()
	facts, err := engineDetect(ctx, sys)
	if err != nil {
		return err
	}
	env := stdoutEnv()
	return dnsscreen.Run(ctx, dnsscreen.Options{
		Prompter:  nav.Prompter{Ctx: component.NewCtx(env), In: engineInput(), Out: env.Writer(engineStdout)},
		System:    sys,
		Facts:     facts,
		Config:    c,
		NewEngine: newEngine(sys, engineOptions(ctx, sys, c)),
		DryRun:    c.DryRun,
		LogPath:   output.LegacyLogPath,
		Now:       revertNow,
	})
}
