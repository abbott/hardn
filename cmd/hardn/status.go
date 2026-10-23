package main

// The main menu's host block, risk level and status rows from the status
// model (refactor plan slice 2, next/s2-menu-live; the maintainer,
// 2026-10-23: "Main menu goes live"): pkg/menu reaches them through the
// hook set here, as it reaches the DNS screen (dns.go), so that pkg/menu
// runs no check and builds no engine; pkg/screens/mainmenu reads and
// draws them. docs/engine/menu-live.md says what goes live.

import (
	"context"

	"github.com/abbott/hardn/pkg/config"
	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/menu"
	"github.com/abbott/hardn/pkg/screens/mainmenu"
)

// statusHost is the host the main menu's status reads, a seam for the
// tests: host.Real with no run log, unlike engineHost's. The status only
// reads, on every pass of the main menu, and the log would take each
// command and its output every time (sshd -T's whole configuration among
// them), as origin/main's checks logged none; the run log stays with what
// changes the host (-g, the DNS screen).
var statusHost = func() host.System { return host.NewReal(host.Options{}) }

// withStatus returns m with its status drawn from the host hardn builds,
// against c, the configuration m was given, and hardn's version
// (menu.MainMenu.SetStatus).
func withStatus(m *menu.MainMenu, c *config.Config, version string) *menu.MainMenu {
	m.SetStatus(func(ctx context.Context, below int) (string, error) {
		return drawStatus(ctx, c, version, below)
	})
	return m
}

// drawStatus is one pass's status: read through statusHost, the facts
// detected by engineDetect, against c's nameservers as they are now (the
// DNS screen adopts each list it saves into c), drawn for stdout's
// terminal (stdoutEnv), whose height picks the full or the compact form,
// above a menu of below lines. Nothing is registered for Ctrl-C: it ends
// hardn by Go's default for SIGINT, as during origin/main's checks
// (pkg/menu).
func drawStatus(ctx context.Context, c *config.Config, version string, below int) (string, error) {
	return mainmenu.Status(ctx, mainmenu.Options{
		System:  statusHost(),
		Detect:  engineDetect,
		Config:  c,
		Version: "v" + version,
		Env:     stdoutEnv(),
	}, below)
}
