package main

import (
	"context"
	"fmt"

	"github.com/abbott/hardn/pkg/engine"
	"github.com/abbott/hardn/pkg/engine/controls/apparmor"
	"github.com/abbott/hardn/pkg/engine/controls/firewall"
	"github.com/abbott/hardn/pkg/engine/controls/ssh"
	"github.com/abbott/hardn/pkg/engine/controls/updates"
	"github.com/abbott/hardn/pkg/engine/controls/users"
	"github.com/abbott/hardn/pkg/engine/header"
	"github.com/abbott/hardn/pkg/engine/risk"
	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/output"
	"github.com/abbott/hardn/pkg/ui/component"
	"github.com/abbott/hardn/pkg/ui/screen"
	"github.com/abbott/hardn/pkg/ui/term"
)

// statusControls are the main menu's status rows: the five areas'
// Controls(), in Figma's order (risk.Rows), as the gallery's status hosts
// check them (uigallery's statusControls, which this copies until slice 2
// exports one list; TestStatusControls holds it to risk.Rows).
func statusControls() []engine.Control {
	var out []engine.Control
	for _, cs := range [][]engine.Control{users.Controls(), firewall.Controls(), ssh.Controls(), apparmor.Controls(), updates.Controls()} {
		out = append(out, cs...)
	}
	return out
}

// reading is what the main menu reads of a host: detect's facts, the
// checks' reports, the header probe's header and the risk level over both.
type reading struct {
	facts   detect.Facts
	reports []engine.Report
	header  header.Header
	level   risk.Level
}

// read reads sys as hardn's main menu will, and as the gallery's status
// hosts do (uigallery's readStatus): detect's facts once, Engine.Check over
// controls on them, the header probe on the same facts, and the risk level
// over both. hold is the engine's signal hold, which a check never takes.
// The error is ctx's or detection's alone, as Engine.Check's: a check's
// error is in its report.
func read(ctx context.Context, sys host.System, controls []engine.Control, hold func() func()) (reading, error) {
	var r reading
	e := engine.New(sys, engine.Options{
		Detect: func(ctx context.Context, sys host.System) (detect.Facts, error) {
			var err error
			r.facts, err = detect.Detect(ctx, sys)
			return r.facts, err
		},
		Hold: hold,
	})
	reports, err := e.Check(ctx, controls)
	if err != nil {
		return r, err
	}
	r.reports = reports
	r.header = header.Read(ctx, sys, r.facts)
	r.level = risk.Assess(r.reports, r.header)
	return r, nil
}

// mainMenu is r's main menu as hardn will draw it, rendered in env:
// output.MainMenuOf over the main menu's mock, as the gallery's status
// hosts draw theirs.
func mainMenu(env term.Env, r reading) string {
	return output.MainMenuOf(screen.MainMenuFixture(), r.header, r.reports, r.level).Render(component.NewCtx(env))
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
