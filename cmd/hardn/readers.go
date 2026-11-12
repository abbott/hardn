package main

// System Details and Logs (refactor plan slice 1, unit (d), next/s1-replace;
// docs/engine/screen-system-details.md §7; plan §3.14: the legacy path replaced, not
// gated): main menu 9 and 10 run the two screens (pkg/screens/systemdetails,
// pkg/screens/logs), which pkg/screens/mainmenu reaches through the hooks set here,
// so that pkg/screens/mainmenu builds no reader; main menu 8 runs the Configuration
// screen (pkg/screens/config; docs/engine/screen-config.md), which reads no host; hardn
// system-details, hardn config and -p print through the same packages. All of this only reads, under
// plan only as live, on a host with no run log; neither opens hardn's log nor writes
// /etc/hardn/hardn.yml.example (main.go's openStartLog and liveWrites).

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"

	"github.com/abbott/hardn/pkg/config"
	"github.com/abbott/hardn/pkg/engine/details"
	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/revert"
	"github.com/abbott/hardn/pkg/output"
	configscreen "github.com/abbott/hardn/pkg/screens/config"
	"github.com/abbott/hardn/pkg/screens/logs"
	"github.com/abbott/hardn/pkg/screens/mainmenu"
	"github.com/abbott/hardn/pkg/screens/systemdetails"
	"github.com/abbott/hardn/pkg/ui/component"
	"github.com/abbott/hardn/pkg/ui/nav"
)

// readerHost is the host these read, a seam for the tests: host.Real with no run log,
// as statusHost's (status.go): a draw's reads and less --version are not hardn's
// changes, and -p writes nothing to the log it prints.
var readerHost = func() host.System { return host.NewReal(host.Options{}) }

// withReaderScreens returns m with its Configuration, System Details and Logs items
// set to their screens on c, the configuration m was given, and found, how its file
// was looked for when it loaded.
func withReaderScreens(m mainmenu.Options, c *config.Config, found config.Found) mainmenu.Options {
	m.Configuration = func() error { return runConfigScreen(context.Background(), c, found) }
	m.SystemDetails = func() error { return runSystemDetailsScreen(context.Background()) }
	m.Logs = func() error { return runLogsScreen(context.Background(), c) }
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

// loadedFound is how c's file was found when it loaded, for the Configuration screen:
// found, main's findConfig ahead of the load, but for a file the first run's offer
// created since (c has a path where found saw no file), which is from the search path
// where its path is on it, and from nowhere the screen names where it is not.
func loadedFound(c *config.Config, found config.Found) config.Found {
	path := c.Path()
	if path == "" || found.Exists {
		return found
	}
	if config.OnSearchPath(path) {
		return config.Found{Path: path, By: config.FoundSearch, Exists: true}
	}
	return config.Found{}
}

// runConfigScreen is main menu 8: c, hardn.yml as the main menu holds it, and found,
// how its file was found when it loaded (main.go's loadedFound), not asked again: the screen
// names the file whose values it shows. It reads no host and writes nothing, under
// plan only as live.
func runConfigScreen(ctx context.Context, c *config.Config, found config.Found) error {
	return configscreen.Run(ctx, configscreen.Options{Prompter: readerPrompter(), Config: c, Found: found})
}

// runSystemDetailsScreen is main menu 9: the screen until the user goes back (nil) or
// quits (nav.ErrQuit), or the input ends (prompt.ErrAborted),
// pkg/screens/mainmenu's showScreen mapping what it returns; the facts detected
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
	if form.JSON {
		outFrame.disarm() // stdout holds one document (frame.go)
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

// runConfig is hardn config: what main menu 8 shows, on stdout, with no menu and no
// prompt. No root check, as system-details; refused's words on stderr, exit status 2,
// nothing read. It writes nothing: no log, no example, and no offer to create
// hardn.yml (loadExisting). A file -f or HARDN_CONFIG names that is not there is the
// screen's "not found", exit status 0; one that does not load is said on stderr, exit
// status 1, as is a failed write. The load's warnings are not said, as system-details
// says none: "Not used" lists the keys.
func runConfig(refused error) {
	if refuse(refused) {
		return
	}
	found := findConfig(configFile)
	// A file -f or HARDN_CONFIG names that can't be looked at (a directory
	// this user can't enter) is not a file that isn't there: say why, as
	// for one that can't be read.
	if !found.Exists && found.By != config.FoundNone {
		if _, err := os.Stat(found.Path); err != nil && !errors.Is(err, fs.ErrNotExist) {
			sayError("Failed to load configuration: %v", err)
			exit(1)
			return
		}
	}
	c := config.DefaultConfig()
	if found.Exists || found.By == config.FoundNone {
		var err error
		if c, err = loadExisting(configFile); err != nil {
			sayError("Failed to load configuration: %v", err)
			exit(1)
			return
		}
	}
	err := configscreen.Print(configscreen.PrintOptions{Out: engineStdout, Env: stdoutEnv(), Config: c, Found: loadedFound(c, found)})
	if err != nil {
		fmt.Fprintf(stderr, "hardn: %v\n", err)
		exit(1)
	}
}

// printOnly reports whether -p is the only operation flag, -n being none: it then
// runs ahead of the root check (decisions 9 (a), 10 (a)).
func printOnly() bool {
	return printLogs && !createUser && !disableRootSSH && !configureUfw && !configureDns &&
		!runAll
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
