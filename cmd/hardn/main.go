package main

import (
	"context"
	"fmt"
	"io"
	"os"
	osuser "os/user"

	"github.com/spf13/cobra"

	"github.com/abbott/hardn/pkg/config"
	"github.com/abbott/hardn/pkg/output"
	"github.com/abbott/hardn/pkg/version"
)

// Populated by build flags
var (
	Version   string
	BuildDate string
	GitCommit string
)

var (
	noColor        bool
	configFile     string
	username       string
	dryRun         bool
	createUser     bool
	disableRootSSH bool
	configureUfw   bool
	configureDns   bool
	runAll         bool
	printLogs      bool
	showVersion    bool
	setupSudoEnv   bool
	assumeYes      bool
	cfg            *config.Config
)

func main() { start(output.LegacyLogPath) }

// start is main, with the log's path for the tests. It writes nothing before the
// flags are parsed: --help, --version, a usage error, the root check's refusal and
// a plan write neither the log nor the example hardn.yml (refactor plan §3.23,
// next/p3-startup). The commands that write the log open it in their run, once
// every refusal before their operation is past (liveWrites, openStartLog). What
// hardn says before the command goes to stderr, so that stdout holds the
// command's output alone (defect 28).
func start(logPath string) {
	startLogPath, logOpened = logPath, false

	// All Execute returns is a bad command line, which cobra has said on stderr with
	// the usage: a usage error, as refuse's.
	if err := rootCmd.Execute(); err != nil {
		exit(output.ExitUsage)
	}
}

// startLogPath is the log's path start was given, opened by openStartLog; ""
// until start runs, as when cmd/hardn's tests run rootCmd without it: nothing is
// then opened or written.
var startLogPath string

// logOpened is set once openStartLog has opened the log.
var logOpened bool

// openStartLog opens hardn's log (openLog), once, for the commands that write it:
// the main menu, the operation flags and setup-sudo-env through liveWrites, and
// confirm, rollback, rollback --abandon and rollback --boot, once their own
// refusals (revertPreflight: -n, not root) are past, whose steps it records under
// the revert timer and the boot unit (revertLog). Those that only read it, the
// Logs screen and -p, leave it unopened: a missing log is "no log yet" there.
func openStartLog() {
	if startLogPath == "" || logOpened {
		return
	}
	logOpened = true
	openLog(startLogPath)
}

// liveWrites are start-up's writes for a run that may change the host: hardn's
// log opened (openStartLog), and the example hardn.yml written where absent
// (ensureExampleConfig), a warning and not an exit where it cannot be. They are
// made after the root check, hardn.yml's load and every refusal before the
// operation, and not in plan only (c.DryRun, -n or hardn.yml's dryRun), which
// writes neither.
func liveWrites(c *config.Config) {
	if startLogPath == "" || c.DryRun {
		return
	}
	openStartLog()
	if err := ensureExampleConfig(); err != nil {
		sayWarning("Unable to create example configuration file: %v", err)
	}
}

func init() {
	// rootCmd.Version is not set: cobra would then answer --version itself,
	// with "hardn version <x>" alone, ahead of Run's build info (refactor
	// plan §3.11: --version's build info restored).

	rootCmd.AddCommand(setupSudoEnvCmd)
	rootCmd.AddCommand(systemDetailsCmd(runSystemDetails))

	rootCmd.PersistentFlags().StringVarP(&configFile, "config", "f", "", "Specify configuration file path")
	rootCmd.PersistentFlags().StringVarP(&username, "username", "u", "", "Specify username to create")
	rootCmd.PersistentFlags().BoolVarP(&createUser, "create-user", "c", false, "Create non-root user with sudo access")
	rootCmd.PersistentFlags().BoolVarP(&disableRootSSH, "disable-root", "d", false, "Disable root SSH access")
	rootCmd.PersistentFlags().BoolVarP(&configureDns, "configure-dns", "g", false, "Configure DNS resolvers")
	rootCmd.PersistentFlags().BoolVarP(&configureUfw, "configure-ufw", "w", false, "Configure UFW")
	rootCmd.PersistentFlags().BoolVarP(&runAll, "run-all", "r", false, "Run all hardening steps")
	rootCmd.PersistentFlags().BoolVarP(&dryRun, "dry-run", "n", false, "Dry run mode (preview changes without applying)")
	rootCmd.PersistentFlags().BoolVarP(&printLogs, "print-logs", "p", false, "Print logs")
	rootCmd.PersistentFlags().BoolVarP(&showVersion, "version", "v", false, "Show version information")
	rootCmd.PersistentFlags().BoolVarP(&setupSudoEnv, "setup-sudo-env", "e", false, "Keep HARDN_CONFIG under sudo or doas")
	rootCmd.PersistentFlags().BoolVar(&assumeYes, "yes", false, "Apply a plan without a terminal to confirm it on; a lockout-risk plan still needs one")
	addSurfaceFlags(rootCmd.PersistentFlags())
	addTimeoutFlag(rootCmd.PersistentFlags())
}

// startMenu runs the main menu (runMenu), a seam for the tests, which
// record the options the root command gives it.
var startMenu = runMenu

// mainMenuRun reports whether the root command opens the main menu: no
// operation flag given.
func mainMenuRun() bool {
	return !createUser && !disableRootSSH && !configureUfw && !configureDns &&
		!runAll && !printLogs && !setupSudoEnv
}

// sayFound says which hardn.yml FindConfigFile found, once, on stderr:
// an error when -f or HARDN_CONFIG names a file that is not there.
func sayFound(f config.Found) {
	if f.By != config.FoundNone && !f.Exists {
		sayError("%s", f)
		return
	}
	sayInfo("%s", f)
}

var rootCmd = &cobra.Command{
	Use:   "hardn",
	Short: "Linux hardening tool",
	Long:  `A simple hardening tool for Debian, Ubuntu, Proxmox and Alpine Linux.`,
	Run: func(cmd *cobra.Command, args []string) {
		// -r runs alone: with -c, -d, -w, -g or -e a usage error before
		// anything runs (runall.go; slice 9's defect 10).
		if refuse(runAllAlone()) {
			return
		}
		// A bad --confirm-timeout: a usage error before anything runs
		// (hardn.yml's confirmTimeout once it loads, below).
		if refuseConfirmTimeout(nil) {
			return
		}

		// Create version service
		versionService := version.NewService(Version, BuildDate, GitCommit)

		// Check if version flag is set and display version info
		if showVersion {
			if err := versionService.PrintVersionInfo(engineStdout); err != nil {
				sayError("%v", err)
				exit(1)
			}
			return
		}

		// -p alone only reads what any user can cat: ahead of the root check, the
		// config load and detectOS, so that its pipe form holds the log alone
		// (decisions 9 (a), 10 (a)).
		if printOnly() {
			if code := printLog(context.Background(), ""); code != 0 {
				exit(code)
			}
			return
		}

		// Check if running as root
		currentUser, err := lookupCurrentUser()
		if err != nil {
			sayError("Failed to get current user: %v", err)
			exit(1)
		}

		if currentUser.Uid != "0" {
			sayNotRoot()
			exit(1)
		}

		// An OS other than Debian, Ubuntu or Alpine, or none os-release
		// names, is refused (hostOS); nothing is said of one that is not.
		// Before hardn.yml's load, whose first-run question may write one:
		// a host hardn refuses is never offered it.
		if err := detectOS(); err != nil {
			sayError("Failed to detect OS: %v", err)
			exit(1)
		}

		// Load configuration (-f, then HARDN_CONFIG, then the search path).
		// Which hardn.yml is said once, on stderr, for the flags, and
		// nothing before the main menu (decision 4 (a) of
		// docs/engine/legacy-left.md, adopted 2026-11-04). With -n, plan
		// only, none found is the defaults, with no offer to create one
		// (loadExisting, as setup-sudo-env's), so that -n writes nothing.
		// A yes to the offer on a live run writes hardn.yml, asked for,
		// whatever refusal follows it. None found is not said here: the
		// offer, NoneLoaded below, or both say it (a third line said it
		// again, seen on an Alpine LXC, 2026-11-07).
		found := findConfig(configFile)
		if !mainMenuRun() && found.By != config.FoundNone {
			sayFound(found)
		}
		load := loadConfig
		if dryRun {
			load = loadExisting
		}
		cfg, err = load(configFile)
		if err != nil {
			sayError("Failed to load configuration: %v", err)
			exit(1)
		}
		if !mainMenuRun() && !found.Exists && cfg.Path() == "" {
			sayInfo("%s", config.NoneLoaded) // the offer to create one declined
		}

		// The session's mode: -n/--dry-run when given, true or false, else
		// the dryRun of the file loaded, as loaded (§3.19; decision 2).
		cfg.ResolveDryRun(dryRunGiven(cmd), dryRun)

		// hardn.yml's confirmTimeout, read as the engine reads it
		// (engineOptions): a bad value a usage error before anything runs.
		if refuseConfirmTimeout(cfg) {
			return
		}

		// If username is provided, override config: -c's name, and the
		// default of the User Management screen's Create and Run All's user
		// (users.go, runall.go)
		if username != "" {
			cfg.Username = username
		}

		// Check if we need to create a user and no username is provided
		// (-r without one asks Run All for no user: runall.Asked)
		if createUser && cfg.Username == "" {
			sayError("Please specify a username with -u flag or in the configuration file.")
			exit(1)
		}

		// Every refusal before the operation is past (the OS's is ahead of
		// the load): the log opened and the example written, unless plan
		// only.
		liveWrites(cfg)

		warnDeprecations(cfg)

		// If no specific flags provided, show the interactive menu
		if mainMenuRun() {

			// The main menu; its screens' items and its status: hooks.go
			startMenu(context.Background(), menuOptions(cfg, Version))
			return
		}

		// Run All: the planner's one plan through the engine, the hostname
		// typed on a terminal; off one a lockout-risk plan refused, a plain
		// one applied with --yes alone (runall.go). It runs alone (runAllAlone), -p aside: the log printed
		// after -r's run, as after -c, -d, -w and -g, its status composed by
		// runStatus.
		if runAll {
			code := runAllFlag(cfg, Version)
			if printLogs {
				code = runStatus(code, printLog(context.Background(), cfg.LogFile))
			}
			if code != 0 {
				exit(code)
			}
			return
		}

		// Create user: the User control's Create through the engine, with no
		// question, as -g's (users.go). Its status is hardn's as -g's is,
		// composed by runStatus, once the flags after it have run. It no
		// longer configures sshd after the user (SSH is slice 6's).
		status := 0 // -c's, -d's, -w's, -g's and -p's, what -e's yields to (runStatus)
		if createUser {
			status = createAdmin(cfg)
		}

		// Disallow root login: the SSH Login control's Root through the
		// engine, the hostname typed on a terminal, refused off one (ssh.go).
		// After -c, so that its proof, whether an admin could still log in,
		// sees the user -c created: -c -d is no longer refused (slice 6,
		// decision 8 (a)), its guard -d's own. Its status is hardn's as -c's
		// is, composed by runStatus, once the flags after it have run.
		if disableRootSSH {
			status = runStatus(status, disallowRoot(cfg))
		}

		// Configure firewall: the Firewall control through the engine, the hostname
		// typed on a terminal, refused off one (firewall.go). Its status is hardn's
		// as -g's is, composed by runStatus, once the flags after it have run.
		if configureUfw {
			status = runStatus(status, configureFirewall(cfg))
		}
		// (-w no longer goes through FirewallManager)

		// Configure DNS: the DNS control through the engine, with no question (dns.go).
		// A refusal or a failure is hardn's exit status, once the flags after it have run (V25).
		defer func() {
			if status != 0 {
				exit(status)
			}
		}()
		if configureDns {
			status = runStatus(status, configureDNS(cfg))
		}

		// Print the log, after the others: their lines stay on stdout (with no other
		// operation flag it ran ahead of the root check, printOnly).
		if printLogs {
			status = runStatus(status, printLog(context.Background(), cfg.LogFile))
		}

		// Keep HARDN_CONFIG: the Environment control through the engine,
		// with no question (environment.go). A refusal, a lockout-risk
		// plan's among them, or a failure is hardn's exit status at once;
		// a plan with changes to make (3) yields to -g's refusal or failure.
		if setupSudoEnv {
			status = runStatus(status, keepEnvironment(cfg))
			return
		}
	},
}

var setupSudoEnvCmd = &cobra.Command{
	Use:   "setup-sudo-env",
	Short: "Keep HARDN_CONFIG under sudo or doas",
	Long: `Keeps HARDN_CONFIG for the user who runs sudo hardn (or doas hardn), in hardn's own rule.
This allows you to consistently use a custom configuration file location across
all commands, even when requiring elevated privileges.

This command must be run with sudo privileges.

Example:
  sudo hardn setup-sudo-env`,
	// What -e does (keepEnvironment), after the root command's root check,
	// its OS check (detectOS: hardn changes nothing on an OS it refuses,
	// macOS included, its log and example configuration neither), and
	// hardn.yml's load (loadExisting: none found is the defaults, with
	// no offer to create one, which -n would let write), whose backup
	// settings the engine takes and whose dryRun, or -n, makes it the plan
	// alone (refactor plan slice 9,
	// unit (d); gallery-environment.md §5, Made here, adopted 2026-10-29).
	Run: func(cmd *cobra.Command, args []string) {
		if checkRoot() != nil {
			return
		}
		if err := detectOS(); err != nil {
			sayError("Failed to detect OS: %v", err)
			exit(1)
			return
		}
		sayFound(findConfig(configFile))
		var err error
		if cfg, err = loadExisting(configFile); err != nil {
			sayError("Failed to load configuration: %v", err)
			exit(1)
			return
		}
		warnDeprecations(cfg)
		cfg.ResolveDryRun(dryRunGiven(cmd), dryRun)
		if refuseConfirmTimeout(cfg) {
			return
		}
		liveWrites(cfg)
		if code := keepEnvironment(cfg); code != 0 {
			exit(code)
		}
	},
}

// The steps of the commands' Run functions that reach the host outside the
// engine, and the process exit. cmd/hardn's tests replace them, so that a
// test never reads this machine's user, config or OS, and never exits.
// loadExisting is setup-sudo-env's load: hardn.yml where one is found, the
// defaults where none is, never the offer to create one (config.LoadExisting;
// refactor plan slice 9, unit (d)).
var (
	lookupCurrentUser             = osuser.Current
	loadConfig                    = config.LoadConfig
	loadExisting                  = config.LoadExisting
	ensureExampleConfig           = config.EnsureExampleConfigExists
	findConfig                    = config.FindConfigFile
	detectOS                      = hostOS
	exit                          = os.Exit
	stderr              io.Writer = termStderr
)

// dryRunGiven reports whether -n/--dry-run was given on cmd's command line,
// true or false: the flag given wins over hardn.yml (§3.19).
func dryRunGiven(cmd *cobra.Command) bool { return cmd.Flags().Changed("dry-run") }

// refuse prints err, a refusal of the command line, to stderr and ends
// hardn with exit status 2; nil refuses nothing (false).
func refuse(err error) bool {
	if err == nil {
		return false
	}
	fmt.Fprintln(stderr, err)
	exit(output.ExitUsage)
	return true
}
