package main

// The wirings of refactor plan P3, next/p3-output, that cmd/hardn owes
// (since next/p3-confirm-cleanup, hardn confirm's clean-up links pkg/output
// and the engine, confirm.go, and since slice 2's unit (d), -g and the DNS
// screen, engine.go and dns.go; since next/p3-startup, the audit log,
// audit below):
//   - §3.17's deprecation warning (approved 2026-10-17) for a bool
//     permitRootLogin, printed to stderr once, when the configuration loads;
//   - no colour codes from hardn confirm and hardn rollback (<id>,
//     --abandon, --boot) when their output is not a terminal: the Debian 13
//     and Ubuntu 26.04 VM runs found escape codes in the journal
//     (docs/safety/vmtest.md §4.9). Since decision 5 (a) of
//     docs/engine/legacy-left.md their lines are console lines (say),
//     rendered for stderr, which is plain off a colour terminal.

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/abbott/hardn/pkg/config"
	"github.com/abbott/hardn/pkg/engine/header"
	"github.com/abbott/hardn/pkg/engine/logview"
	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/hangup"
	"github.com/abbott/hardn/pkg/output"
	"github.com/abbott/hardn/pkg/ui/component"
	"github.com/abbott/hardn/pkg/ui/term"
)

// auditPath is the audit log's path (output.AuditPath), and auditHost the
// host it is written on, given the run's: that host itself. Seams for the
// tests.
var (
	auditPath = output.AuditPath
	auditHost = func(run host.System) host.System { return run }
)

// audit appends run's record to the audit log (docs/reference/output.md
// §4) through sys, the host the run planned and applied on: one line for
// each engine run, a flag's or a screen's change, whatever its outcome (a
// plan alone, a refusal, an apply, its rollback, one declined or
// interrupted), once it is reported and before hardn may end on a signal
// (endControls). Only the operations reach it, so never --help,
// --version, a usage error or the non-root refusal. The run's version and
// hostname, and c's deprecations, are filled in where the run has none,
// so that the line holds what a JSON document of the run would. A record
// not written is said as a warning on stderr, with its code; it never
// fails the run nor changes its exit status. output.Append runs without
// the run's cancellation, so an interrupted run is recorded too.
func audit(sys host.System, c *config.Config, run output.Run) {
	ctx, sys := context.Background(), auditHost(sys)
	if run.HardnVersion == "" {
		run.HardnVersion = Version
	}
	if run.Hostname == "" {
		if data, err := sys.ReadFile(ctx, header.HostnameFile); err == nil {
			run.Hostname = strings.TrimSpace(string(data))
		}
	}
	if c != nil && run.Deprecations == nil {
		run.Deprecations, run.ConfigPath = c.Deprecations(), c.Path()
	}
	if err := output.Append(ctx, sys, auditPath, output.NewRecord(run, revertNow())); err != nil {
		sayWarning("This run is not in the audit log (%s): %v", output.CodeOf(err), err)
	}
}

// auditRan is a screen's Options.Ran: each change's run, once its flow has
// ended, appended to the audit log (audit).
func auditRan(sys host.System, c *config.Config) func(output.Run) {
	return func(run output.Run) { audit(sys, c, run) }
}

// stderrEnv is the render environment of stderr: a seam for the tests.
var stderrEnv = func() term.Env { return streamEnv(os.Stderr) }

// termStdout and termStderr are hardn's stdout and stderr as cmd/hardn
// writes to them, its console lines (say) among them: through
// hangup.Writer (terminalWriter), one each, so that once a
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

// terminalWriter is f through hangup.Writer, unless f is a regular file
// (hardn -g > report.txt): a write to a file never waits on a reader, and
// what hardn writes there after a hangup is still read, so it is kept. A
// pipe, a terminal or the null device goes through it. A terminal is inside
// the output frame (frame.go) too.
func terminalWriter(f *os.File) io.Writer {
	var w io.Writer = f
	if fi, err := f.Stat(); err != nil || !fi.Mode().IsRegular() {
		w = hangup.Writer(f)
	}
	if frameTerminal(f) {
		return framed{outFrame, w}
	}
	return w
}

// warnDeprecations prints the configuration's values read in a deprecated
// form (§3.17: a bool permitRootLogin) to stderr, as warnings through
// pkg/ui, with the file they were read from. The root command calls it
// once, right after the configuration loads; it is the one warning of
// them (pkg/output's documents carry them as findings, and its Emit does
// not warn again). A logFile naming another path than the one hardn writes
// is one too (decision 6 (a); gallery-system-details.md §6's words), and
// so is each key nothing
// reads set to a value (slices 4, 6 and 8, decisions 8, 9 (a) and 4 (a),
// and the legacy-left unit: notUsed). sudoNoPassword is one of those since
// adminNoPassword replaced it (the maintainer, 2026-11-11): false warns,
// and true, the old default every saved file holds, does not.
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
	for _, key := range notUsed(c) {
		retired = append(retired, fmt.Sprintf(keyNotUsed, key))
	}
	return deprecationWarnings(stderrEnv(), c.Path(), c.Deprecations(), retired...)
}

// logFileNotUsed is decision 6 (a)'s warning, and the Logs screen's row.
const logFileNotUsed = "hardn.yml's logFile, %s, isn't used"

// keyNotUsed is said once for each key of notUsed: refactor plan slice 6's
// decision 9 (a) (gallery-ssh.md §5, adopted 2026-11-03) for the SSH
// settings, and slice 8's decision 4 (a) (gallery-runall.md §5, adopted
// 2026-11-04) for ufwAllowedPorts, enableAppArmor and enableLynis, and
// slice 4's decision 8 (gallery-firewall.md §5, adopted 2026-11-01) for
// the two ufw policies. Since
// slice 8 nothing reads them, Run All included, so the words name no
// screen (slice 6's were "isn't used by SSH Login or -d").
const keyNotUsed = "hardn.yml's %s isn't used"

// notUsed are the keys of c that nothing in hardn reads and that hold a
// value other than config.DefaultConfig's: permitRootLogin (as RootLogin
// reads it, so that the bool of §3.17, which loadWarnings already says is
// deprecated, warns here only where it is true), then the keys Config has
// no field for (config.Retired, read from the file's own keys):
// sshAllowedUsers, sshListenAddress, sshKeyPath, the ufw policies,
// ufwAllowedPorts, enableAppArmor, enableLynis, the keys the legacy
// package, sources and localization code read, logsconfig, and
// sudoNoPassword. A value equal to the
// default never warns: origin/main's SaveConfig writes the defaults into
// every file it saves, so it is hardn's, not the file's. disableRootSSH is read:
// true, Run All turns root login off (decision 4 (a)), so it warns no more.
// sshPort and sshConfigFile, SSH Login's port form's default and Move old
// settings' old file, and sshPort Run All's too, never warn.
func notUsed(c *config.Config) []string {
	d := config.DefaultConfig()
	var keys []string
	for _, k := range []struct {
		key string
		set bool
	}{
		{"permitRootLogin", c.RootLogin() != d.RootLogin()},
	} {
		if k.set {
			keys = append(keys, k.key)
		}
	}
	return append(keys, c.Retired()...)
}

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
