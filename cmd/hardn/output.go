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
//     (docs/safety/vmtest.md §4.9). Since decision 5 (a) of
//     docs/engine/legacy-left.md their lines are console lines (say),
//     rendered for stderr, which is plain off a colour terminal.

import (
	"fmt"
	"io"
	"os"

	"github.com/abbott/hardn/pkg/config"
	"github.com/abbott/hardn/pkg/engine/logview"
	"github.com/abbott/hardn/pkg/host/hangup"
	"github.com/abbott/hardn/pkg/ui/component"
	"github.com/abbott/hardn/pkg/ui/term"
)

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
// pipe, a terminal or the null device goes through it.
func terminalWriter(f *os.File) io.Writer {
	if fi, err := f.Stat(); err == nil && fi.Mode().IsRegular() {
		return f
	}
	return hangup.Writer(f)
}

// warnDeprecations prints the configuration's values read in a deprecated
// form (§3.17: a bool permitRootLogin) to stderr, as warnings through
// pkg/ui, with the file they were read from. The root command calls it
// once, right after the configuration loads; it is the one warning of
// them (pkg/output's documents carry them as findings, and its Emit does
// not warn again). A logFile naming another path than the one hardn writes
// is one too (decision 6 (a); gallery-system-details.md §6's words), and
// so is sudoNoPassword: false (slice 5, decision 7 (a)), and each key nothing
// reads set to a value (slices 4, 6 and 8, decisions 8, 9 (a) and 4 (a),
// and the legacy-left unit: notUsed).
func warnDeprecations(c *config.Config) {
	if w := loadWarnings(c); w != "" {
		_, _ = io.WriteString(stderrEnv().Writer(stderr), w)
	}
}

// loadWarnings is what warnDeprecations says for c, rendered for stderr; "" when
// nothing.
func loadWarnings(c *config.Config) string {
	var retired []string
	if noColor {
		retired = append(retired, noColorDeprecated) // §3.11: an alias of --color=never
	}
	if p := logview.Elsewhere(c.LogFile); p != "" {
		retired = append(retired, fmt.Sprintf(logFileNotUsed, p))
	}
	if !c.SudoNoPassword {
		retired = append(retired, sudoNoPasswordNotUsed)
	}
	for _, key := range notUsed(c) {
		retired = append(retired, fmt.Sprintf(keyNotUsed, key))
	}
	return deprecationWarnings(stderrEnv(), c.Path(), c.Deprecations(), retired...)
}

// logFileNotUsed is decision 6 (a)'s warning, and the Logs screen's row.
const logFileNotUsed = "hardn.yml's logFile, %s, isn't used"

// sudoNoPasswordNotUsed is refactor plan slice 5's decision 7 (a)
// (gallery-users.md §5, adopted 2026-11-03), and since slice 8 Run All's
// too (gallery-runall.md §5, decision 4 (a), adopted 2026-11-04):
// hardn.yml's username, sudoNoPassword and sshKeys are Create's defaults
// alone, and nothing reads sudoNoPassword: a new user has no password to
// ask, so Create's grant, the screen's, -c's and Run All's, asks none
// whatever it says. Only false warns: the defaults have true
// (config.DefaultConfig), which is what Create does, so true, set or not,
// is hardn's, not a value unused. username and sshKeys never warn: User
// Management, -c and Run All read both.
const sudoNoPasswordNotUsed = "hardn.yml's sudoNoPassword: false isn't used"

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
// package, sources and localization code read, and logsconfig. A value equal to the
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
