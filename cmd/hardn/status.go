package main

// The main menu's host block, risk level and status rows from the status
// model (refactor plan slice 2, next/s2-menu-live; the maintainer,
// 2026-10-23: "Main menu goes live"): pkg/screens/mainmenu reaches them through the
// hook set here, as it reaches the DNS screen (dns.go), so that pkg/screens/mainmenu
// runs no check and builds no engine; pkg/screens/mainmenu reads them, and
// pkg/screens/mainmenu draws the main menu on its layout with them, on the terminal
// set here (slice 2b, next/s2b-menu). docs/engine/menu-live.md says what
// goes live, docs/engine/menu-layout.md how the main menu is drawn.

import (
	"context"

	"github.com/abbott/hardn/pkg/config"
	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/screens/mainmenu"
	"github.com/abbott/hardn/pkg/ui/screen"
)

// statusHost is the host the main menu's status reads, a seam for the
// tests: host.Real with no run log, unlike engineHost's. The status only
// reads, on every pass of the main menu, and the log would take each
// command and its output every time (sshd -T's whole configuration among
// them), as origin/main's checks logged none; the run log stays with what
// changes the host (-g, the DNS screen).
var statusHost = func() host.System { return host.NewReal(host.Options{}) }

// withStatus returns m with its status read from the host hardn builds,
// against c, the configuration m was given, and hardn's version
// (mainmenu.Options.Status), drawn on stdout's terminal, as stdoutEnv
// detects it on each pass (mainmenu.Options.Terminal). A main menu drawn
// without either panics, so both are set here, on the one main menu
// hardn builds.
func withStatus(m mainmenu.Options, c *config.Config, version string) mainmenu.Options {
	m.Status = func(ctx context.Context) (screen.MainMenu, error) {
		return readStatus(ctx, c, version)
	}
	m.Terminal = stdoutEnv
	return m
}

// readStatus is one pass's status: read through statusHost, the facts
// detected by engineDetect, against c's nameservers as they are now (the
// DNS screen adopts each list it saves into c), the main menu's
// view-model with its header, host block, risk level and rows filled.
// Nothing is registered for Ctrl-C: it ends hardn by Go's default for
// SIGINT, as during origin/main's checks (pkg/screens/mainmenu).
func readStatus(ctx context.Context, c *config.Config, version string) (screen.MainMenu, error) {
	return mainmenu.Status(ctx, mainmenu.StatusOptions{
		System:  statusHost(),
		Detect:  engineDetect,
		Config:  c,
		Version: "v" + version,
	})
}
