// Package mainmenu reads and draws the main menu's status on a host
// (refactor plan slice 2, next/s2-menu-live; the maintainer, 2026-10-23:
// "Main menu goes live"): the host block, the risk level and the status
// rows, from the main menu's status controls (controls.Status: Figma's
// seven, then DNS), the header probe and the risk level, mapped by
// pkg/output and drawn by pkg/ui/screen exactly as the gallery's status
// hosts and internal/tools/p4status draw them. Status is the one entry
// point, which cmd/hardn's hook on the main menu calls on every pass
// (menu.MainMenu.SetStatus); docs/engine/menu-live.md says what it reads
// and costs.
//
// It reaches the host only through host.System and the engine's Check,
// which only reads (depguard rule screens-through-host), and renders only
// through pkg/output and pkg/ui.
package mainmenu

import (
	"context"
	"fmt"
	"io"
	"strings"

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
	"github.com/abbott/hardn/pkg/ui/term"
	"github.com/abbott/hardn/pkg/ui/theme"
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
	// Env is the terminal the status is drawn for: its width, its height
	// (which picks the full or the compact form), its colours and glyphs.
	Env term.Env
}

// Status reads the host as the main menu's status, through o, and returns
// its blocks for a main menu whose own menu takes below lines under them,
// heading to prompt (screen.MainMenu.StatusBlocks), followed by the blank
// line before that menu, written through o.Env's writer, ready to print.
//
// The reading is detect's facts, once; Engine.Check over controls.Status
// on them; the header probe on the same facts; and the risk level over
// both. A check only reads, never plans or applies (Engine.Check), and the
// engine's signal hold, which only an apply takes, is never taken. A
// check that fails, or a fact not established, is its row's Unknown, and
// the status is drawn. The error is ctx's alone (Engine.Check's: detect
// fails only with ctx), returned before anything is drawn: a reading cut
// short is never drawn as a whole one.
func Status(ctx context.Context, o Options, below int) (string, error) {
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
		return "", fmt.Errorf("main menu: %w", err)
	}
	h := header.Read(ctx, o.System, facts)
	if err := ctx.Err(); err != nil {
		return "", fmt.Errorf("main menu: header: %w", err)
	}
	vm := output.MainMenuOf(screen.MainMenu{Header: component.Header{Version: o.Version}}, h, reports, risk.Assess(reports, h))
	var b strings.Builder
	blocks := vm.StatusBlocks(component.NewCtx(o.Env), below) + strings.Repeat("\n", theme.SectionGap+1)
	if _, err := io.WriteString(o.Env.Writer(&b), blocks); err != nil {
		return "", fmt.Errorf("main menu: %w", err)
	}
	return b.String(), nil
}
