package main

// The two wirings of refactor plan P3, next/p3-output, that cmd/hardn owes
// now (and nothing of pkg/output: §3.11 is not approved, but for hardn
// system-details' --output json, readers.go, so no other --output, no
// --yes or --confirm-timeout flag, and the audit log comes with
// next/p3-startup; since next/p3-confirm-cleanup, hardn confirm's clean-up
// links pkg/output and the engine, confirm.go, and since slice 2's unit
// (d), -g and the DNS screen, engine.go and dns.go):
//   - §3.17's deprecation warning (approved 2026-10-17) for a bool
//     permitRootLogin, printed to stderr once, when the configuration loads;
//   - no colour codes from hardn confirm and hardn rollback (<id>,
//     --abandon, --boot) when their output is not a terminal: the Debian 13
//     and Ubuntu 26.04 VM runs found escape codes in the journal
//     (docs/safety/vmtest.md §4.9).

import (
	"fmt"
	"io"
	"os"

	"github.com/fatih/color"

	"github.com/abbott/hardn/pkg/config"
	"github.com/abbott/hardn/pkg/engine/logview"
	"github.com/abbott/hardn/pkg/host/hangup"
	"github.com/abbott/hardn/pkg/logging"
	"github.com/abbott/hardn/pkg/ui/component"
	"github.com/abbott/hardn/pkg/ui/term"
)

// Seams for the tests: the render environment of stderr, and whether
// confirm and rollback write colour.
var (
	stderrEnv    = func() term.Env { return streamEnv(os.Stderr) }
	revertColour = func() bool { return streamEnv(os.Stdout).HasColor() && streamEnv(os.Stderr).HasColor() }
)

// termStdout and termStderr are hardn's stdout and stderr as cmd/hardn
// writes to them, the legacy logger's console lines (fatih/color's Output
// and Error) and its fallback when hardn's log cannot be opened
// (logging.FallbackOutput) among them, set by init before main opens the
// log: through hangup.Writer (terminalWriter), one each, so that once a
// hangup is caught (hangup.Seen) what hardn writes there is dropped, and
// a write blocked on a terminal nothing reads any more is given up on
// (since next/s3-hangup; the maintainer, 2026-10-28: "Make hardn stop
// writing to its terminal once a hangup is caught"; cmd-engine.md §2.6).
// The legacy menus' own fmt.Print calls write to os.Stdout itself; no
// hangup is caught while they run.
var (
	termStdout = terminalWriter(os.Stdout)
	termStderr = terminalWriter(os.Stderr)
)

func init() {
	color.Output, color.Error = termStdout, termStderr
	logging.FallbackOutput = termStderr
}

// terminalWriter is f through hangup.Writer, unless f is a regular file
// (hardn -g > report.txt): a write to a file never waits on a reader, and
// what hardn writes there after a hangup is still read, so it is kept. A
// pipe, a terminal or the null device goes through it.
func terminalWriter(f *os.File) io.Writer {
	if fi, err := f.Stat(); err == nil && fi.Mode().IsRegular() {
		return f
	}
	return hangup.Writer(f)
}

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
// not warn again). A logFile naming another path than the one hardn writes
// is one too (decision 6 (a); gallery-system-details.md §6's words), and
// so is either ufw policy the file sets (slice 4, decision 8), and
// sudoNoPassword: false (slice 5, decision 7 (a)).
func warnDeprecations(c *config.Config) {
	if w := loadWarnings(c); w != "" {
		_, _ = io.WriteString(stderrEnv().Writer(stderr), w)
	}
}

// loadWarnings is what warnDeprecations says for c, rendered for stderr; "" when
// nothing.
func loadWarnings(c *config.Config) string {
	var retired []string
	if p := logview.Elsewhere(c.LogFile); p != "" {
		retired = append(retired, fmt.Sprintf(logFileNotUsed, p))
	}
	if c.UfwDefaultIncomingPolicy != "" {
		retired = append(retired, ufwIncomingNotUsed)
	}
	if c.UfwDefaultOutgoingPolicy != "" {
		retired = append(retired, ufwOutgoingNotUsed)
	}
	if !c.SudoNoPassword {
		retired = append(retired, sudoNoPasswordNotUsed)
	}
	return deprecationWarnings(stderrEnv(), c.Path(), c.Deprecations(), retired...)
}

// logFileNotUsed is decision 6 (a)'s warning, and the Logs screen's row.
const logFileNotUsed = "hardn.yml's logFile, %s, isn't used"

// ufwIncomingNotUsed and ufwOutgoingNotUsed are refactor plan slice 4's
// decision 8 (gallery-firewall.md §5, adopted 2026-11-01): nothing reads
// the two policies, so a file that sets one is told so, and still loads.
// Only a value warns: the defaults leave both "" (config.DefaultConfig),
// and origin/main's SaveConfig writes them as "" into every file it saves,
// so "" is hardn's, not the file's. ufwAllowedPorts, which Run All reads,
// waits for slice 8.
const (
	ufwIncomingNotUsed = "hardn.yml's ufwDefaultIncomingPolicy isn't used"
	ufwOutgoingNotUsed = "hardn.yml's ufwDefaultOutgoingPolicy isn't used"
)

// sudoNoPasswordNotUsed is refactor plan slice 5's decision 7 (a)
// (gallery-users.md §5, adopted 2026-11-03; proposed words, unit (d)):
// hardn.yml's username, sudoNoPassword and sshKeys are Create's defaults
// alone, and nothing on the User control reads sudoNoPassword: a new user
// has no password to ask (decision 3 (a)), so Create's grant, the screen's
// and -c's, asks none whatever it says. Only false warns: the defaults have
// true (config.DefaultConfig), which is what Create does, so true, set or
// not, is hardn's, not a value unused. username and sshKeys never warn:
// the screen's Create offers both, and -c takes both (with no key, its
// plan's own finding, USERS_NO_KEY, says so). Run All (-r, main menu 7)
// still reads sudoNoPassword, through origin/main's UserManager, until
// slice 8.
const sudoNoPasswordNotUsed = "hardn.yml's sudoNoPassword: false isn't used by User Management or -c"

// deprecationWarnings renders deps as one Warning callout, a line for each
// after the file path it was read from (component.Messages: a run of
// warnings, its title once), then each of retired, a line naming hardn.yml
// itself, for stderr; "" when there are none.
func deprecationWarnings(env term.Env, path string, deps []config.Deprecation, retired ...string) string {
	var run component.Messages
	for _, d := range deps {
		text := d.String()
		if path != "" {
			text = path + ": " + text
		}
		run = append(run, component.Message{Kind: component.MessageWarn, Text: text})
	}
	for _, text := range retired {
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
