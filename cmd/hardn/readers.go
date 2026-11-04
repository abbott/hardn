package main

// System Details and Logs (refactor plan slice 1, unit (d), next/s1-replace;
// docs/engine/screen-system-details.md §7; plan §3.14: the legacy path replaced, not
// gated): main menu 9 and 10 run the two screens (pkg/screens/systemdetails,
// pkg/screens/logs), which pkg/menu reaches through the hooks set here, as it reaches
// the Environment screen (environment.go), so that pkg/menu builds no reader; hardn
// system-details and -p print through the same packages. All of this only reads, under
// plan only as live, on a host with no run log; main()'s start-up, ahead of it, does
// not: as root it creates the log, and /etc/hardn/hardn.yml.example where absent.

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/abbott/hardn/pkg/config"
	"github.com/abbott/hardn/pkg/engine/details"
	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/revert"
	"github.com/abbott/hardn/pkg/menu"
	"github.com/abbott/hardn/pkg/output"
	"github.com/abbott/hardn/pkg/screens/logs"
	"github.com/abbott/hardn/pkg/screens/systemdetails"
	"github.com/abbott/hardn/pkg/ui/component"
	"github.com/abbott/hardn/pkg/ui/nav"
)

// readerHost is the host these read, a seam for the tests: host.Real with no run log,
// as statusHost's (status.go): a draw's reads and less --version are not hardn's
// changes, and -p writes nothing to the log it prints.
var readerHost = func() host.System { return host.NewReal(host.Options{}) }

// readerMenu is what withReaderScreens sets the System Details and Logs items of:
// pkg/menu's main menu, and the tests' record of what was set.
type readerMenu interface {
	SetSystemDetails(run func() error)
	SetLogs(run func() error)
}

// The main menu is a readerMenu.
var _ readerMenu = (*menu.MainMenu)(nil)

// withReaderScreens returns m with its System Details and Logs items set to their
// screens on c, the configuration m was given.
func withReaderScreens[M readerMenu](m M, c *config.Config) M {
	m.SetSystemDetails(func() error { return runSystemDetailsScreen(context.Background()) })
	m.SetLogs(func() error { return runLogsScreen(context.Background(), c) })
	return m
}

// readerPrompter is the screens' terminal: the one line reader every prompt shares
// and stdout's render environment, as on each visit.
func readerPrompter() nav.Prompter {
	env := stdoutEnv()
	return nav.Prompter{Ctx: component.NewCtx(env), In: engineInput(), Out: env.Writer(engineStdout)}
}

// readerFacts are the facts for one reading, each command under details.Read's bounds.
// What detect does not establish, the report says not read, so its error is no error
// here.
func readerFacts(ctx context.Context, sys host.System) detect.Facts {
	facts, _ := engineDetect(ctx, details.Bounded(sys))
	return facts
}

// runSystemDetailsScreen is main menu 9: the screen until the user goes back (nil) or
// quits (nav.ErrQuit), or the input ends (prompt.ErrAborted), as runEnvironmentScreen
// runs Environment's, pkg/menu's showScreen mapping what it returns; the facts detected
// once per visit.
func runSystemDetailsScreen(ctx context.Context) error {
	sys := readerHost()
	return systemdetails.Run(ctx, systemdetails.Options{Prompter: readerPrompter(), System: sys, Facts: readerFacts(ctx, sys)})
}

// runLogsScreen is main menu 10, as runSystemDetailsScreen is 9: the log hardn writes,
// c's logFile said where it names another path, TERM for the pager.
func runLogsScreen(ctx context.Context, c *config.Config) error {
	return logs.Run(ctx, logs.Options{Prompter: readerPrompter(), System: readerHost(), LogFile: c.LogFile, Term: os.Getenv("TERM")})
}

// runSystemDetails is hardn system-details, no root check, as ever, and no hardn.yml:
// form's refusal on stderr, exit status 2, nothing read; its warnings on stderr; the
// report, or under --output json output.Build's document, on stdout. A failed write
// is said on stderr, exit status 1.
func runSystemDetails(form SystemDetailsForm, refused error) {
	if refuse(refused) {
		return
	}
	var words component.Messages
	for _, w := range form.Warnings {
		words = append(words, component.Message{Kind: component.MessageWarn, Text: w})
	}
	env := stderrEnv()
	if out := words.Render(component.NewCtx(env)); out != "" {
		_, _ = io.WriteString(env.Writer(stderr), out+"\n")
	}
	ctx := context.Background()
	sys := readerHost()
	facts := readerFacts(ctx, sys)
	var err error
	if form.JSON {
		report := details.Read(ctx, sys, facts)
		err = output.WriteJSON(engineStdout, output.Build(output.Run{
			Mode: output.ModeCheck, HardnVersion: Version, Hostname: report.Host.Hostname, Facts: &facts, Details: &report,
		}))
	} else {
		err = systemdetails.Print(ctx, systemdetails.PrintOptions{Out: engineStdout, Env: stdoutEnv(), System: sys, Facts: facts})
	}
	if err != nil {
		fmt.Fprintf(stderr, "hardn: %v\n", err)
		exit(1)
	}
}

// printOnly reports whether -p is the only operation flag, -n being none: it then
// runs ahead of the root check (decisions 9 (a), 10 (a)).
func printOnly() bool {
	return printLogs && !createUser && !disableRootSSH && !configureUfw && !configureDns &&
		!runAll && !setupSudoEnv
}

// printLog is -p: logs.Print on stdout, logFile said in a row to a terminal where it
// names another path ("" where nothing has loaded hardn.yml), Printed.Stderr on stderr
// and the exit status, which it returns: Printed.ExitCode's, or 1 for a failed write.
// It writes nothing to the log.
func printLog(ctx context.Context, logFile string) int {
	sys := readerHost()
	printed, err := logs.Print(ctx, logs.PrintOptions{Out: engineStdout, Env: stdoutEnv(), System: sys, LogFile: logFile})
	if err != nil {
		fmt.Fprintf(stderr, "hardn: %v\n", err)
		return 1
	}
	code := printed.ExitCode()
	tool := "" // asked only where a read failed: -p reads nothing else
	if code != 0 {
		tool = elevationHint(ctx, sys)
	}
	for _, l := range printed.Stderr(tool) {
		fmt.Fprintln(stderr, l)
	}
	return code
}

// elevationHint is the tool -p's refusal names: detect's elevation tool, or sudo; ""
// as root, whom no tool helps (a root-squashed NFS path among what refuses root), and
// where neither sudo nor doas is installed (revert.Elevate). It runs nothing
// (detect.FindElevation).
func elevationHint(ctx context.Context, sys host.System) string {
	if u, err := lookupCurrentUser(); err == nil && u.Uid == "0" {
		return ""
	}
	if e := revert.Elevate(detect.FindElevation(ctx, sys)); e != revert.NoElevation {
		return e
	}
	return ""
}
