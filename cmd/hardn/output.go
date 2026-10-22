package main

// The two wirings of refactor plan P3, next/p3-output, that cmd/hardn owes
// now (and nothing of pkg/output: §3.11 is not approved, so no --output,
// --yes or --confirm-timeout flag, and the audit log comes with
// next/p3-startup; since next/p3-confirm-cleanup, hardn confirm's clean-up
// links pkg/output and the engine, confirm.go):
//   - §3.17's deprecation warning (approved 2026-10-17) for a bool
//     permitRootLogin, printed to stderr once, when the configuration loads;
//   - no colour codes from hardn confirm and hardn rollback (<id>,
//     --abandon, --boot) when their output is not a terminal: the Debian 13
//     and Ubuntu 26.04 VM runs found escape codes in the journal
//     (docs/safety/vmtest.md §4.9).

import (
	"io"
	"os"

	"github.com/fatih/color"

	"github.com/abbott/hardn/pkg/config"
	"github.com/abbott/hardn/pkg/ui/component"
	"github.com/abbott/hardn/pkg/ui/term"
)

// Seams for the tests: the render environment of stderr, and whether
// confirm and rollback write colour.
var (
	stderrEnv    = func() term.Env { return streamEnv(os.Stderr) }
	revertColour = func() bool { return streamEnv(os.Stdout).HasColor() && streamEnv(os.Stderr).HasColor() }
)

// streamEnv is pkg/ui/term's detection for the stream f, as it detects
// stdout: f a terminal, TERM, NO_COLOR, HARDN_PLAIN, and --no-color as
// --color=never. The theme is fixed to dark, so that nothing queries the
// terminal's background (HARDN_THEME=auto would): only the profile is
// needed here.
func streamEnv(f *os.File) term.Env {
	opts := term.Options{Theme: term.ThemeDark}
	if noColor {
		opts.Color = term.ColorNever
	}
	return term.Detect(opts, os.Stdin, f)
}

// warnDeprecations prints the configuration's values read in a deprecated
// form (§3.17: a bool permitRootLogin) to stderr, as warnings through
// pkg/ui, with the file they were read from. The root command calls it
// once, right after the configuration loads; it is the one warning of
// them (pkg/output's documents carry them as findings, and its Emit does
// not warn again).
func warnDeprecations(c *config.Config) {
	env := stderrEnv()
	if w := deprecationWarnings(env, c.Path(), c.Deprecations()); w != "" {
		_, _ = io.WriteString(env.Writer(stderr), w)
	}
}

// deprecationWarnings renders deps as one Warning callout, a line for each
// after the file path it was read from (component.Messages: a run of
// warnings, its title once), for stderr; "" when there are none.
func deprecationWarnings(env term.Env, path string, deps []config.Deprecation) string {
	var run component.Messages
	for _, d := range deps {
		text := d.String()
		if path != "" {
			text = path + ": " + text
		}
		run = append(run, component.Message{Kind: component.MessageWarn, Text: text})
	}
	out := run.Render(component.NewCtx(env))
	if out == "" {
		return ""
	}
	return out + "\n"
}

// setRevertColour turns the legacy logger's colour (fatih/color, which the
// root command's initializeColor turns on unless --no-color) off for hardn
// confirm and hardn rollback unless both stdout, where the logger writes,
// and stderr, where the notices go, are colour terminals. Under the systemd
// timer both are the journal, under the OpenRC sleeper the null device.
func setRevertColour() { color.NoColor = !revertColour() }
