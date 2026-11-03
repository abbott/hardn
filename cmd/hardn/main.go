package main

import (
	"context"
	"fmt"
	"io"
	"os"
	osuser "os/user"

	"github.com/fatih/color"
	"github.com/spf13/cobra"

	hardncmd "github.com/abbott/hardn/pkg/cmd"
	"github.com/abbott/hardn/pkg/config"
	"github.com/abbott/hardn/pkg/domain/model"
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
	installLinux        bool
	installPython       bool
	installAll          bool
	configureUfw        bool
	configureDns        bool
	runAll              bool
	updateSources       bool
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
		exit(hardncmd.ExitUsage)
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
	rootCmd.AddCommand(hardncmd.SystemDetailsCmd(runSystemDetails))

	rootCmd.PersistentFlags().StringVarP(&configFile, "config", "f", "", "Specify configuration file path")
	rootCmd.PersistentFlags().StringVarP(&username, "username", "u", "", "Specify username to create")
	rootCmd.PersistentFlags().BoolVarP(&createUser, "create-user", "c", false, "Create non-root user with sudo access")
	rootCmd.PersistentFlags().BoolVarP(&disableRootSSH, "disable-root", "d", false, "Disable root SSH access")
	// rootCmd.PersistentFlags().BoolVarP(&installLinux, "install-linux", "l", false, "Install Linux packages")
	// rootCmd.PersistentFlags().BoolVarP(&installPython, "install-python", "i", false, "Install Python packages")
	// rootCmd.PersistentFlags().BoolVarP(&installAll, "install-all", "a", false, "Install all packages")
	rootCmd.PersistentFlags().BoolVarP(&configureDns, "configure-dns", "g", false, "Configure DNS resolvers")
	rootCmd.PersistentFlags().BoolVarP(&configureUfw, "configure-ufw", "w", false, "Configure UFW")
	// rootCmd.PersistentFlags().BoolVarP(&updateSources, "configure-sources", "s", false, "Update package sources")
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
		// -c with -d: refused outright, whatever hardn.yml holds, before
		// anything runs (refactor plan slice 5, unit (d); the maintainer,
		// 2026-11-03): -d turns off root's SSH login, and a key -c adds is
		// not proof that its user can log in (hardncmd.RefuseCreateDisableRoot).
		if refuse(hardncmd.RefuseCreateDisableRoot(createUser, disableRootSSH)) {
			return
		}
		// -n only previews: refuse operation flags before anything runs (V1).
		if refuseDryRunOperations(operationFlags()) {
			return
		}
		// hardn.yml's dryRun: true only previews too, -n/--dry-run not given
		// (decision 9).
		if refuseConfigDryRunOperations(cmd, operationFlags()) {
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
		// the dryRun of the file loaded, as loaded (§3.19; decision 2). The
		// file is the one read above, so plan only from it is refused here
		// too, should it say so now where it did not before the root check.
		cfg.ResolveDryRun(dryRunGiven(cmd), dryRun)
		if cfg.DryRunSource() == config.DryRunFile &&
			refuse(hardncmd.RefuseConfigDryRunOperations(cfg.DryRun, cfg.Path(), operationFlags())) {
			return
		}

		// If username is provided, override config: -c's name, and the
		// default of the User Management screen's Create and Run All's user
		// (users.go)
		if username != "" {
			cfg.Username = username
		}

		// Check if we need to create a user and no username is provided
		if (createUser || runAll) && cfg.Username == "" {
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
		if !createUser && !disableRootSSH && !installLinux && !installPython &&
			!installAll && !configureUfw && !configureDns && !runAll &&
			!updateSources && !printLogs && !setupSudoEnv {

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

		// Process command line options using the new architecture

		// Get required managers
		// (-d needs no manager: it plans and applies the SSH Login control's Root, ssh.go)
		// (-w needs no manager: it plans and applies the Firewall control, firewall.go)
		// (-g needs no manager: it plans and applies the DNS control, dns.go)
		packageManager := serviceFactory.CreatePackageManager()
		// (-c needs no manager: it plans and applies the User control's Create, users.go)
		menuManager := serviceFactory.CreateMenuManager()
		// (-e needs no manager: it plans and applies the Environment control, environment.go)

		// Handle a complete system hardening request
		if runAll {
			logging.LogInfo("Running complete system hardening...")

			// Create a comprehensive hardening configuration
			hardeningConfig := &model.HardeningConfig{
				CreateUser:         cfg.Username != "",
				Username:           cfg.Username,
				SudoNoPassword:     cfg.SudoNoPassword,
				SshKeys:            cfg.SshKeys,
				SshPort:            cfg.SshPort,
				SshListenAddresses: []string{cfg.SshListenAddress},
				SshAllowedUsers:    cfg.SshAllowedUsers,
				EnableFirewall:     cfg.EnableUfwSshPolicy,
				AllowedPorts:       []int{},
				FirewallProfiles:   []model.FirewallProfile{},
				ConfigureDns:       cfg.ConfigureDns,
				Nameservers:        cfg.Nameservers,
				EnableAppArmor:     cfg.EnableAppArmor,
				EnableLynis:        cfg.EnableLynis,
				// EnableUnattendedUpgrades: cfg.EnableUnattendedUpgrades,
			}

			// Run all hardening steps
			if err := menuManager.HardenSystem(hardeningConfig); err != nil {
				logging.LogError("Failed to complete system hardening: %v", err)
			} else {
				logging.LogSuccess("System hardening completed successfully!")
				fmt.Printf("Check the log file at %s for details.\n", output.LegacyLogPath)
			}
			return
		}

		// Handle individual operations based on flags

		// Update package sources
		if updateSources {
			if err := packageManager.UpdatePackageSources(); err != nil {
				logging.LogError("Failed to update package sources: %v", err)
			} else {
				logging.LogSuccess("Package sources updated")
			}

			// Handle Proxmox-specific sources
			if osInfo.OsType != "alpine" && osInfo.IsProxmox {
				if err := packageManager.UpdateProxmoxSources(); err != nil {
					logging.LogError("Failed to update Proxmox sources: %v", err)
				} else {
					logging.LogSuccess("Proxmox sources updated")
				}
			}
		}

		// Install Linux packages
		if installLinux || installAll {
			logging.LogInfo("Installing Linux packages...")

			if installAll {
				// Use the enhanced method that handles all package types appropriately
				if err := packageManager.InstallAllLinuxPackages(); err != nil {
					logging.LogError("Failed to install Linux packages: %v", err)
				} else {
					logging.LogSuccess("All Linux packages installed successfully")
				}
			} else {
				// Just install core packages when specifically requested
				if osInfo.OsType == "alpine" && len(cfg.AlpineCorePackages) > 0 {
					if err := packageManager.InstallLinuxPackages(cfg.AlpineCorePackages, "core"); err != nil {
						logging.LogError("Failed to install Alpine core packages: %v", err)
					} else {
						logging.LogSuccess("Alpine core packages installed successfully")
					}
				} else if len(cfg.LinuxCorePackages) > 0 {
					if err := packageManager.InstallLinuxPackages(cfg.LinuxCorePackages, "core"); err != nil {
						logging.LogError("Failed to install Linux core packages: %v", err)
					} else {
						logging.LogSuccess("Linux core packages installed successfully")
					}
				}
			}
		}

		// Install Python packages
		if installPython || installAll {
			logging.LogInfo("Installing Python packages...")

			if installAll {
				// Use the enhanced method for all Python packages
				if err := packageManager.InstallAllPythonPackages(cfg.UseUvPackageManager); err != nil {
					logging.LogError("Failed to install Python packages: %v", err)
				} else {
					logging.LogSuccess("All Python packages installed successfully")
				}
			} else {
				// Handle specific Python package installation
				if osInfo.OsType == "alpine" && len(cfg.AlpinePythonPackages) > 0 {
					if err := packageManager.InstallPythonPackages(
						cfg.AlpinePythonPackages,
						cfg.PythonPipPackages,
						cfg.UseUvPackageManager,
					); err != nil {
						logging.LogError("Failed to install Alpine Python packages: %v", err)
					} else {
						logging.LogSuccess("Alpine Python packages installed successfully")
					}
				} else {
					// For Debian/Ubuntu
					pythonPackages := cfg.PythonPackages
					// Add non-WSL packages if not in WSL
					if os.Getenv("WSL") == "" && len(cfg.NonWslPythonPackages) > 0 {
						pythonPackages = append(pythonPackages, cfg.NonWslPythonPackages...)
					}

					if err := packageManager.InstallPythonPackages(
						pythonPackages,
						cfg.PythonPipPackages,
						cfg.UseUvPackageManager,
					); err != nil {
						logging.LogError("Failed to install Python packages: %v", err)
					} else {
						logging.LogSuccess("Python packages installed successfully")
					}
				}
			}
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
		// After -c, so that its proof sees the user -c created. Its status is
		// hardn's as -c's is, composed by runStatus, once the flags after it
		// have run.
		if disableRootSSH {
			status = runStatus(status, disallowRoot(cfg))
		}

		// Configure firewall: the Firewall control through the engine, the hostname
		// typed on a terminal, refused off one (firewall.go). Its status is hardn's
		// as -g's is, composed by runStatus, once the flags after it have run.
		if configureUfw {
			status = runStatus(status, configureFirewall(cfg))
		}
		// (-w no longer goes through FirewallManager, which Run All and -r keep)

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

		// Output completion message for the legacy operations other than the all-in-one run
		if installLinux || installPython || installAll || updateSources {
			logging.LogSuccess("Script completed selected hardening operations.")
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
	fileDryRun                    = config.FileDryRun
	ensureExampleConfig           = config.EnsureExampleConfigExists
	detectOS                      = osdetect.DetectOS
	exit                          = os.Exit
	stderr              io.Writer = termStderr
)

// operationFlags names the operation flags set on the command line that
// -n refuses: the flags that change the host (pkg/cmd lists them), but -g,
// -e, -w, -c and -d, whose dry runs are the DNS, Environment, Firewall,
// User and SSH Login controls' plans, shown with nothing written (refactor
// plan slice 2, unit (d); the maintainer, 2026-10-23; slice 9, unit (d),
// gallery-environment.md §5, Made here, adopted 2026-10-29; slice 4, unit
// (d), parity-firewall.md §6, adopted 2026-11-02; slice 5, unit (d),
// gallery-users.md §5, decision 6 (a), adopted 2026-11-03; slice 6, unit
// (d), gallery-ssh.md §5, decision 8 (a), adopted 2026-11-03).
func operationFlags() []string {
	var names []string
	for _, f := range []struct {
		set  bool
		name string
	}{
		{runAll, hardncmd.FlagRunAll},
		{installLinux, hardncmd.FlagInstallLinux},
		{installPython, hardncmd.FlagInstallPython},
		{installAll, hardncmd.FlagInstallAll},
		{updateSources, hardncmd.FlagConfigureSources},
	} {
		if f.set {
			names = append(names, f.name)
		}
	}
	return names
}

// refuseDryRunOperations reports whether -n was combined with the named
// operations; if so it has printed why to stderr and ended hardn with exit
// status 2 (pkg/cmd's dryRunFlagApplies gates this; V1).
func refuseDryRunOperations(operations []string) bool {
	return refuse(hardncmd.RefuseDryRunOperations(dryRun, operations))
}

// refuseConfigDryRunOperations reports whether hardn.yml's dryRun: true,
// -n/--dry-run not given, was combined with the named operations; if so it
// has printed why, naming the file and --dry-run=false, and ended hardn
// with exit status 2, as for -n (refactor plan slice 2b, decision 9). It
// reads the file the loader finds, quietly (config.FileDryRun), so the
// refusal comes before the root check, as -n's does; a file that does not
// load is the loader's to report. The root command refuses again on the
// file as loaded (its Run).
func refuseConfigDryRunOperations(cmd *cobra.Command, operations []string) bool {
	if len(operations) == 0 || dryRunGiven(cmd) {
		return false
	}
	path, on, err := fileDryRun(configFile)
	if err != nil {
		return false
	}
	return refuse(hardncmd.RefuseConfigDryRunOperations(on, path, operations))
}

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
	exit(hardncmd.ExitUsage)
	return true
}
