package main

import (
	"context"
	"errors"
	"fmt"
	"io/fs"

	"github.com/abbott/hardn/pkg/config"
	"github.com/abbott/hardn/pkg/engine"
	"github.com/abbott/hardn/pkg/engine/header"
	"github.com/abbott/hardn/pkg/engine/risk"
	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/output"
	"github.com/abbott/hardn/pkg/screens/mainmenu"
	"github.com/abbott/hardn/pkg/ui/component"
	"github.com/abbott/hardn/pkg/ui/screen"
	"github.com/abbott/hardn/pkg/ui/term"
)

// reading is what the main menu reads of a host: hardn.yml's path ("" for
// none found), detect's facts, the checks' reports, the header probe's
// header and the risk level over both.
type reading struct {
	config  string
	facts   detect.Facts
	reports []engine.Report
	header  header.Header
	level   risk.Level
}

// read reads the host through g as hardn's main menu does, and as the
// gallery's status hosts do (uigallery's readStatus): hardn.yml, the
// first of configs that exists, as hardn loads it (config.Read; hardn's
// defaults, which list no nameserver, when none does); detect's facts
// once; Engine.Check over controlsOf(hardn.yml), the main menu's status
// controls (controls.Status), on them; the header probe on the same
// facts; and the risk level over both. The engine's signal hold, which a
// check never takes, is g's (a refusal). g records each call as the
// part's that made it (guard.within): hardn.yml's, detect's, a row's
// control's, the header's. The error is hardn.yml's (one that does not
// load, as hardn refuses to start with it), ctx's or detection's alone,
// as Engine.Check's: a check's error is in its report. ctx ended during
// the header probe is an error too, so that a reading cut short is never
// scored and shown as a whole one.
func read(ctx context.Context, g *guard, configs []string, controlsOf func(*config.Config) []engine.Control) (reading, error) {
	var r reading
	g.within(partConfig)
	cfg, err := loadConfig(ctx, g, configs)
	if err != nil {
		return r, err
	}
	r.config = cfg.Path()
	controls := controlsOf(cfg)
	e := engine.New(g, engine.Options{
		Detect: func(ctx context.Context, sys host.System) (detect.Facts, error) {
			g.within(partDetect)
			var err error
			r.facts, err = detect.Detect(ctx, sys)
			return r.facts, err
		},
		Hold: g.hold,
	})
	rows := make([]engine.Control, len(controls))
	for i, c := range controls {
		rows[i] = row{c, g}
	}
	reports, err := e.Check(ctx, rows)
	if err != nil {
		return r, err
	}
	r.reports = reports
	g.within(partHeader)
	r.header = header.Read(ctx, g, r.facts)
	if err := ctx.Err(); err != nil {
		return r, fmt.Errorf("header: %w", err)
	}
	r.level = risk.Assess(r.reports, r.header)
	return r, nil
}

// loadConfig is hardn.yml from the first of paths that exists, read
// through sys as hardn loads it (config.Read), or hardn's defaults when
// none does.
func loadConfig(ctx context.Context, sys host.System, paths []string) (*config.Config, error) {
	for _, p := range paths {
		cfg, err := config.Read(ctx, sys, p)
		switch {
		case errors.Is(err, fs.ErrNotExist):
			continue
		case err != nil:
			return nil, fmt.Errorf("hardn.yml: %w", err)
		}
		return cfg, nil
	}
	return config.DefaultConfig(), nil
}

// row is a status row's control as read checks it: the control's own,
// but that the guard records the calls its Check makes as the row's.
type row struct {
	engine.Control
	g *guard
}

// Check is the control's, its calls recorded as the row's.
func (r row) Check(ctx context.Context, sys host.System, f detect.Facts) (engine.Status, error) {
	r.g.within(r.ID())
	return r.Control.Check(ctx, sys, f)
}

// mainMenu is r's main menu as hardn will draw it, rendered in env:
// output.MainMenuOf over the main menu's mock, as the gallery's status
// hosts draw theirs, with the items hardn's main menu draws on a host of
// r's facts (mainmenu.MenuOf): "Configure firewalld" on Rocky Linux and
// AlmaLinux, "View pve-firewall" on Proxmox VE, and item 5, Auto Updates,
// which the mock has not.
func mainMenu(env term.Env, r reading) string {
	base := screen.MainMenuFixture()
	base.Menu = mainmenu.MenuOf(r.facts)
	return output.MainMenuOf(base, r.header, r.reports, r.level).Render(component.NewCtx(env))
}

// utf8Locale is the locale of a terminal that draws Unicode.
const utf8Locale = "LANG=C.UTF-8"

// profiles are -profile's values, the gallery's, each as the terminal
// term.Resolve makes of it: plain is piped output, the others a terminal
// whose TERM and COLORTERM give the profile, so that p4status needs no
// colour library of its own (depguard's charm rule). The theme is dark.
var profiles = map[string]struct {
	opts    term.Options
	tty     bool
	environ []string
}{
	"plain":     {term.Options{Plain: true}, false, nil},
	"none":      {term.Options{Color: term.ColorNever}, true, []string{"TERM=xterm", utf8Locale}},
	"16":        {term.Options{Color: term.ColorAlways}, true, []string{"TERM=xterm", utf8Locale}},
	"256":       {term.Options{Color: term.ColorAlways}, true, []string{"TERM=xterm-256color", utf8Locale}},
	"truecolor": {term.Options{Color: term.ColorAlways}, true, []string{"TERM=xterm-256color", "COLORTERM=truecolor", utf8Locale}},
}

// renderEnv is the render environment of -profile, -width and -height.
func renderEnv(profile string, width, height int) (term.Env, error) {
	p, ok := profiles[profile]
	switch {
	case !ok:
		return term.Env{}, fmt.Errorf("-profile %q is not plain, none, 16, 256 or truecolor", profile)
	case width < 1 || height < 1:
		return term.Env{}, fmt.Errorf("-width %d and -height %d must be positive", width, height)
	}
	env := term.Resolve(p.opts, term.Facts{StdoutTTY: p.tty, StdinTTY: p.tty, Width: width, Height: height, Environ: p.environ})
	env.Width, env.Height, env.Animate = width, height, false
	return env, nil
}
