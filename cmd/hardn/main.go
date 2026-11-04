package main

import (
	"context"
	"fmt"
	"io"
	"os"
	osuser "os/user"

	"github.com/fatih/color"
	"github.com/spf13/cobra"

	"github.com/abbott/hardn/pkg/config"
	"github.com/abbott/hardn/pkg/infrastructure"
	"github.com/abbott/hardn/pkg/interfaces"
	"github.com/abbott/hardn/pkg/logging"
	"github.com/abbott/hardn/pkg/osdetect"
	"github.com/abbott/hardn/pkg/output"
	"github.com/abbott/hardn/pkg/style"
	"github.com/abbott/hardn/pkg/version"
)

// Populated by build flags
var (
	Version   string
	BuildDate string
	GitCommit string
)

var (
	noColor             bool
	configFile          string
	username            string
	dryRun              bool
	createUser          bool
	disableRootSSH      bool
	configureUfw        bool
	configureDns        bool
	runAll              bool
	printLogs           bool
	showVersion         bool
	setupSudoEnv        bool
	debugUpdates        bool
	testUpdateAvailable bool
	testSecurityUpdate  bool
	cfg                 *config.Config
)

// Create provider as a global for dependency injection
var provider = interfaces.NewProvider()

func main() { start("/var/log/hardn.log") }

// start is main, with the log's path for the tests: what hardn says before the
// command goes to stderr, so that stdout holds the command's output alone (defect 28).
func start(logPath string) {
	logging.InitLogging(logPath)

	// Ensure config directory and example config exist
	if err := ensureExampleConfig(); err != nil {
		// Just log a warning, don't exit - the program can still run with defaults
		fmt.Fprintf(os.Stderr, "Warning: Unable to create example configuration file: %v\n", err)
	}

	// All Execute returns is a bad command line, which cobra has said on stderr with
	// the usage: a usage error, as refuse's.
	if err := rootCmd.Execute(); err != nil {
		exit(output.ExitUsage)
	}
}

func init() {
	// Set version for help output
	rootCmd.Version = Version

	// if rootCmd.Version != "" {
	// 	logging.LogInfo("Current version :::: : %s", rootCmd.Version)
	// }

	// Setup color processing before command execution
	cobra.OnInitialize(initializeColor)

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
	rootCmd.PersistentFlags().BoolVar(&noColor, "no-color", false, "Disable colored output")
	rootCmd.PersistentFlags().BoolVar(&debugUpdates, "debug-updates", false, version.UpdateFlagUsage("Enable debugging for update checks"))
	rootCmd.PersistentFlags().BoolVar(&testUpdateAvailable, "test-update", false, version.UpdateFlagUsage("Force update notification for testing"))
	rootCmd.PersistentFlags().BoolVar(&testSecurityUpdate, "test-security-update", false, version.UpdateFlagUsage("Test security update notification"))
}

func initializeColor() {
	if noColor {
		color.NoColor = true
		style.UseColors = false
	} else {
		color.NoColor = false
		style.UseColors = true
	}
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

		// Create version service
		versionService := version.NewService(Version, BuildDate, GitCommit)

		// Check if version flag is set and display version info
		if showVersion {
			versionService.PrintVersionInfo()
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
			logging.LogError("Failed to get current user: %v", err)
			exit(1)
		}

		if currentUser.Uid != "0" {
			logging.LogError("This script needs to be run as root.")
			fmt.Println(rootHint())
			exit(1)
		}

		// Load configuration (will check both command-line flag and environment variable)
		cfg, err = loadConfig(configFile)
		if err != nil {
			logging.LogError("Failed to load configuration: %v", err)
			exit(1)
		}

		// The session's mode: -n/--dry-run when given, true or false, else
		// the dryRun of the file loaded, as loaded (§3.19; decision 2).
		cfg.ResolveDryRun(dryRunGiven(cmd), dryRun)

		// If username is provided, override config: -c's name, and the
		// default of the User Management screen's Create and Run All's user
		// (users.go, runall.go)
		if username != "" {
			cfg.Username = username
		}

		// Check if we need to create a user and no username is provided
		// (-r without one asks Run All for no user: runall.Asked)
		if createUser && cfg.Username == "" {
			logging.LogError("Please specify a username with -u flag or in the configuration file.")
			exit(1)
		}

		// Detect OS
		osInfo, err := detectOS()
		if err != nil {
			logging.LogError("Failed to detect OS: %v", err)
			exit(1)
		}

		// After detectOS, which clears the terminal: said before it, these are gone
		// before anyone can read them.
		warnDeprecations(cfg)

		// Create service factory
		serviceFactory := infrastructure.NewServiceFactory(provider, osInfo)
		serviceFactory.SetConfig(cfg)

		// If no specific flags provided, show the interactive menu
		if !createUser && !disableRootSSH && !configureUfw && !configureDns &&
			!runAll && !printLogs && !setupSudoEnv {

			// Create menu factory and main menu; its screens' items and its status: hooks.go
			menuFactory := infrastructure.NewMenuFactory(serviceFactory, cfg, osInfo)
			mainMenu := withHooks(menuFactory.CreateMainMenu(versionService), cfg, Version)

			// Handle test flags
			if testUpdateAvailable {
				// Force the update notification to appear with a hard-coded newer version
				mainMenu.SetTestUpdateAvailable("99.0.0")
			}

			// Test security update notification if requested
			if testSecurityUpdate {
				// Force security update notification to appear
				mainMenu.SetTestSecurityUpdate("Critical security vulnerability CVE-2023-1234: Privilege escalation in SSH component")
			}

			if os.Getenv("HARDN_FORCE_UPDATE") != "" {
				// Force the update notification to appear with a hard-coded newer version
				mainMenu.SetTestUpdateAvailable("99.0.0")
			} else if os.Getenv("HARDN_FORCE_SECURITY") != "" {
				// Test mode for security updates - use the mainMenu method instead of direct field access
				mainMenu.SetTestSecurityUpdate("CVE-2023-1234: Security update")
			} else {
				mainMenu.CheckForUpdates()
			}

			// Show main menu with version info
			mainMenu.ShowMainMenu(Version, BuildDate, GitCommit)
			return
		}

		// Run All: the planner's one plan through the engine, the hostname
		// typed on a terminal, any plan with changes refused off one
		// (runall.go). It runs alone (runAllAlone), -p aside: the log printed
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
	// What -e does (keepEnvironment), after the root command's root check
	// and hardn.yml's load (loadExisting: none found is the defaults, with
	// no offer to create one, which -n would let write), whose backup
	// settings the engine takes and whose dryRun, or -n, makes it the plan
	// alone (refactor plan slice 9,
	// unit (d); gallery-environment.md §5, Made here, adopted 2026-10-29).
	Run: func(cmd *cobra.Command, args []string) {
		if checkRoot() != nil {
			return
		}
		var err error
		if cfg, err = loadExisting(configFile); err != nil {
			logging.LogError("Failed to load configuration: %v", err)
			exit(1)
			return
		}
		warnDeprecations(cfg)
		cfg.ResolveDryRun(dryRunGiven(cmd), dryRun)
		if code := keepEnvironment(cfg); code != 0 {
			exit(code)
		}
	},
}

// The steps of the commands' Run functions that reach the host outside the
// provider, and the process exit. cmd/hardn's tests replace them, so that a
// test never reads this machine's user, config or OS, and never exits.
// loadExisting is setup-sudo-env's load: hardn.yml where one is found, the
// defaults where none is, never the offer to create one (config.LoadExisting;
// refactor plan slice 9, unit (d)).
var (
	lookupCurrentUser             = osuser.Current
	loadConfig                    = config.LoadConfig
	loadExisting                  = config.LoadExisting
	ensureExampleConfig           = config.EnsureExampleConfigExists
	detectOS                      = osdetect.DetectOS
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
