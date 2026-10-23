// Package mainmenu reads the main menu's status on a host (refactor plan
// slice 2, next/s2-menu-live; the maintainer, 2026-10-23: "Main menu goes
// live"): the header, the host block, the risk level and the status rows,
// from the main menu's status controls (controls.Status: Figma's seven,
// then DNS), the header probe and the risk level, mapped by pkg/output
// into pkg/ui/screen's main-menu layout exactly as the gallery's status
// hosts and internal/tools/p4status map them. Status is the one entry
// point, which cmd/hardn's hook on the main menu calls on every pass
// (menu.MainMenu.SetStatus); pkg/menu fills in the menu heading, the mode
// indicator, the items and the prompt and draws the whole layout (slice
// 2b, next/s2b-menu). docs/engine/menu-live.md says what it reads and
// costs.
//
// It reaches the host only through host.System and the engine's Check,
// which only reads (depguard rule screens-through-host), and maps only
// through pkg/output into pkg/ui's view-models; it draws nothing.
package mainmenu

import (
	"context"
	"fmt"

	"github.com/abbott/hardn/pkg/config"
	"github.com/abbott/hardn/pkg/engine"
	"github.com/abbott/hardn/pkg/engine/controls"
	"github.com/abbott/hardn/pkg/engine/header"
	"github.com/abbott/hardn/pkg/engine/risk"
	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/output"
	"github.com/abbott/hardn/pkg/ui/component"
	"github.com/abbott/hardn/pkg/ui/screen"
)

// Options is what the main menu's status is read and drawn with: what
// cmd/hardn has when it draws its main menu.
type Options struct {
	// System is the host the checks and the header probe read.
	System host.System
	// Detect establishes the host's facts, once a pass (detect.Detect);
	// the checks and the probe read them.
	Detect func(ctx context.Context, sys host.System) (detect.Facts, error)
	// Config is hardn.yml as loaded: the DNS row's list.
	Config *config.Config
	// Version is hardn's, as the header shows it ("v0.4.0").
	Version string
}

// Status reads the host as the main menu's status, through o, and returns
// the main menu's view-model with the blocks the status model fills: the
// Header (o.Version), the ContextBar, the HostBlock, the RiskSummary and
// the StatusList (output.MainMenuOf). Its Heading, Mode, Menu and Prompt
// are left for the caller, pkg/menu, which draws the layout.
//
// The reading is detect's facts, once; Engine.Check over controls.Status
// on them; the header probe on the same facts; and the risk level over
// both. A check only reads, never plans or applies (Engine.Check), and the
// engine's signal hold, which only an apply takes, is never taken. A
// check that fails, or a fact not established, is its row's Unknown, and
// the status is returned. The error is ctx's alone (Engine.Check's:
// detect fails only with ctx), with the zero view-model: a reading cut
// short is never returned as a whole one.
func Status(ctx context.Context, o Options) (screen.MainMenu, error) {
	var facts detect.Facts
	e := engine.New(o.System, engine.Options{
		Detect: func(ctx context.Context, sys host.System) (detect.Facts, error) {
			var err error
			facts, err = o.Detect(ctx, sys)
			return facts, err
		},
	})
	reports, err := e.Check(ctx, controls.Status(o.Config))
	if err != nil {
		return screen.MainMenu{}, fmt.Errorf("main menu: %w", err)
	}
	h := header.Read(ctx, o.System, facts)
	if err := ctx.Err(); err != nil {
		return screen.MainMenu{}, fmt.Errorf("main menu: header: %w", err)
	}
	return output.MainMenuOf(screen.MainMenu{Header: component.Header{Version: o.Version}}, h, reports, risk.Assess(reports, h)), nil
}
