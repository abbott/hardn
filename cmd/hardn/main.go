package main

import (
	"fmt"
	"os"
	osuser "os/user"

	"github.com/fatih/color"
	"github.com/spf13/cobra"

	"github.com/abbott/hardn/pkg/cmd"
	"github.com/abbott/hardn/pkg/config"
	"github.com/abbott/hardn/pkg/domain/model"
	"github.com/abbott/hardn/pkg/infrastructure"
	"github.com/abbott/hardn/pkg/interfaces"
	"github.com/abbott/hardn/pkg/logging"
	"github.com/abbott/hardn/pkg/osdetect"
	"github.com/abbott/hardn/pkg/version"
)

// Version information - populated by build flags
var (
	Version   string // Semantic version
	BuildDate string // Build date in ISO format
	GitCommit string // Git commit hash
)

var (
	configFile          string
	username            string
	dryRun              bool
	createUser          bool
	disableRoot         bool
	installLinux        bool
	installPython       bool
	installAll          bool
	configureUfw        bool
	configureDns        bool
	runAll              bool
	updateSources       bool
	printLogs           bool
	showVersion         bool // Flag to display version information
	setupSudoEnv        bool
	debugUpdates        bool
	testUpdateAvailable bool
	cfg                 *config.Config
)

// Create provider as a global for dependency injection
var provider = interfaces.NewProvider()

func main() {
	// Setup colors
	color.NoColor = false

	// Init utils
	logging.InitLogging("/var/log/hardn.log")

	// Ensure config directory and example config exist
	if err := config.EnsureExampleConfigExists(); err != nil {
		// Just log a warning, don't exit - the program can still run with defaults
		fmt.Printf("Warning: Unable to create example configuration file: %v\n", err)
	}

	// Execute command
	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}

func init() {
	// Set version for help output
	rootCmd.Version = Version

	if rootCmd.Version != "" {
		logging.LogInfo("Current version :::: : %s", rootCmd.Version)
	}

	rootCmd.PersistentFlags().StringVarP(&configFile, "config", "f", "",
		"Specify configuration file path")

	rootCmd.AddCommand(setupSudoEnvCmd)
	rootCmd.AddCommand(cmd.HostInfoCmd())

	rootCmd.PersistentFlags().StringVarP(&username, "username", "u", "", "Specify username to create")
	rootCmd.PersistentFlags().BoolVarP(&createUser, "create-user", "c", false, "Create non-root user with sudo access")
	rootCmd.PersistentFlags().BoolVarP(&disableRoot, "disable-root", "d", false, "Disable root SSH access")
	rootCmd.PersistentFlags().BoolVarP(&installLinux, "install-linux", "l", false, "Install Linux packages")
	rootCmd.PersistentFlags().BoolVarP(&installPython, "install-python", "i", false, "Install Python packages")
	rootCmd.PersistentFlags().BoolVarP(&installAll, "install-all", "a", false, "Install all packages")
	rootCmd.PersistentFlags().BoolVarP(&configureDns, "configure-dns", "g", false, "Configure DNS resolvers")
	rootCmd.PersistentFlags().BoolVarP(&configureUfw, "configure-ufw", "w", false, "Configure UFW")
	rootCmd.PersistentFlags().BoolVarP(&updateSources, "configure-sources", "s", false, "Update package sources")
	rootCmd.PersistentFlags().BoolVarP(&runAll, "run-all", "r", false, "Run all hardening steps")
	rootCmd.PersistentFlags().BoolVarP(&dryRun, "dry-run", "n", false, "Dry run mode (preview changes without applying)")
	rootCmd.PersistentFlags().BoolVarP(&printLogs, "print-logs", "p", false, "Print logs")
	rootCmd.PersistentFlags().BoolVarP(&showVersion, "version", "v", false, "Show version information")
	rootCmd.PersistentFlags().BoolVarP(&setupSudoEnv, "setup-sudo-env", "e", false,
		"Configure sudoers to preserve HARDN_CONFIG environment variable")
	rootCmd.PersistentFlags().BoolVar(&debugUpdates, "debug-updates", false, "Enable debugging for update checks")
	rootCmd.PersistentFlags().BoolVar(&testUpdateAvailable, "test-update", false, "Force update notification for testing")
}

var rootCmd = &cobra.Command{
	Use:   "hardn",
	Short: "Linux hardening tool",
	Long:  `A simple hardening tool for Debian, Ubuntu, Proxmox and Alpine Linux.`,
	Run: func(cmd *cobra.Command, args []string) {
		// Create version service
		versionService := version.NewService(Version, BuildDate, GitCommit)

		// Check if version flag is set and display version info
		if showVersion {
			versionService.PrintVersionInfo()
			return
		}

		// Check if running as root
		currentUser, err := osuser.Current()
		if err != nil {
			logging.LogError("Failed to get current user: %v", err)
			os.Exit(1)
		}

		if currentUser.Uid != "0" {
			logging.LogError("This script needs to be run as root.")
			fmt.Println("For Ubuntu/Debian run: `sudo hardn` or switch to root `sudo -i`")
			fmt.Println("For Alpine run: `sudo hardn` or switch to root `su`")
			os.Exit(1)
		}

		// Load configuration (will check both command-line flag and environment variable)
		cfg, err = config.LoadConfig(configFile)
		if err != nil {
			logging.LogError("Failed to load configuration: %v", err)
			os.Exit(1)
		}

		// Set dry run mode from flag
		cfg.DryRun = dryRun

		// If username is provided, override config
		if username != "" {
			cfg.Username = username
		}

		// Check if we need to create a user and no username is provided
		if (createUser || runAll) && cfg.Username == "" {
			logging.LogError("Please specify a username with -u flag or in the configuration file.")
			os.Exit(1)
		}

		// Detect OS
		osInfo, err := osdetect.DetectOS()
		if err != nil {
			logging.LogError("Failed to detect OS: %v", err)
			os.Exit(1)
		}

		// Create service factory
		serviceFactory := infrastructure.NewServiceFactory(provider, osInfo)
		serviceFactory.SetConfig(cfg)

		// If no specific flags provided, show the interactive menu
		if !createUser && !disableRoot && !installLinux && !installPython &&
			!installAll && !configureUfw && !configureDns && !runAll &&
			!updateSources && !printLogs && !setupSudoEnv {

			// Create menu factory and main menu with version service
			menuFactory := infrastructure.NewMenuFactory(serviceFactory, cfg, osInfo)
			mainMenu := menuFactory.CreateMainMenu(versionService)

			if testUpdateAvailable {
				// Force the update notification to appear with a hard-coded newer version
				mainMenu.SetTestUpdateAvailable("99.0.0")
			}

			// Show main menu with version info
			mainMenu.ShowMainMenu(Version, BuildDate, GitCommit)
			return
		}

		// Process command line options using the new architecture

		// Get required managers
		sshManager := serviceFactory.CreateSSHManager()
		firewallManager := serviceFactory.CreateFirewallManager()
		dnsManager := serviceFactory.CreateDNSManager()
		packageManager := serviceFactory.CreatePackageManager()
		userManager := serviceFactory.CreateUserManager()
		menuManager := serviceFactory.CreateMenuManager()
		environmentManager := serviceFactory.CreateEnvironmentManager()

		// Handle a complete system hardening request
		if runAll {
			logging.LogInfo("Running complete system hardening...")

			// Create a comprehensive hardening configuration
			hardeningConfig := &model.HardeningConfig{
				CreateUser:               cfg.Username != "",
				Username:                 cfg.Username,
				SudoNoPassword:           cfg.SudoNoPassword,
				SshKeys:                  cfg.SshKeys,
				SshPort:                  cfg.SshPort,
				SshListenAddresses:       []string{cfg.SshListenAddress},
				SshAllowedUsers:          cfg.SshAllowedUsers,
				EnableFirewall:           cfg.EnableUfwSshPolicy,
				AllowedPorts:             []int{},
				FirewallProfiles:         []model.FirewallProfile{},
				ConfigureDns:             cfg.ConfigureDns,
				Nameservers:              cfg.Nameservers,
				EnableAppArmor:           cfg.EnableAppArmor,
				EnableLynis:              cfg.EnableLynis,
				EnableUnattendedUpgrades: cfg.EnableUnattendedUpgrades,
			}

			// Run all hardening steps
			if err := menuManager.HardenSystem(hardeningConfig); err != nil {
				logging.LogError("Failed to complete system hardening: %v", err)
			} else {
				logging.LogSuccess("System hardening completed successfully!")
				fmt.Printf("Check the log file at %s for details.\n", cfg.LogFile)
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

		// Disable root SSH access
		if disableRoot {
			if err := sshManager.DisableRootAccess(); err != nil {
				logging.LogError("Failed to disable root SSH access: %v", err)
			} else {
				logging.LogSuccess("Root SSH access disabled")
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
		// Here's the update for installPython code path
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

		// Create user
		if createUser {
			if err := userManager.CreateUser(cfg.Username, true, cfg.SudoNoPassword, cfg.SshKeys); err != nil {
				logging.LogError("Failed to create user: %v", err)
			} else {
				logging.LogSuccess("User '%s' created successfully", cfg.Username)
			}

			// Configure SSH after user creation
			// TODO: This might need to be refactored to avoid duplicating the SSH configuration
			if err := sshManager.ConfigureSSH(
				cfg.SshPort,
				[]string{cfg.SshListenAddress},
				cfg.PermitRootLogin,
				cfg.SshAllowedUsers,
				[]string{cfg.SshKeyPath},
			); err != nil {
				logging.LogError("Failed to configure SSH: %v", err)
			}
		}

		// Configure firewall
		if configureUfw {
			if err := firewallManager.ConfigureSecureFirewall(cfg.SshPort, []int{}, []model.FirewallProfile{}); err != nil {
				logging.LogError("Failed to configure firewall: %v", err)
			} else {
				logging.LogSuccess("Firewall configured successfully")
			}
		}

		// Configure DNS
		if configureDns {
			if err := dnsManager.ConfigureDNS(cfg.Nameservers, "lan"); err != nil {
				logging.LogError("Failed to configure DNS: %v", err)
			} else {
				logging.LogSuccess("DNS configured successfully")
			}
		}

		// Print logs
		if printLogs {
			logging.PrintLogs(cfg.LogFile)
		}

		// Setting up sudo environment preservation
		if setupSudoEnv {
			if err := environmentManager.SetupSudoPreservation(); err != nil {
				logging.LogError("Failed to configure sudoers: %v", err)
				os.Exit(1)
			}
			logging.LogSuccess("Sudo environment configured to preserve HARDN_CONFIG")
			return
		}

		// Output completion message for operations other than the all-in-one run
		if createUser || disableRoot || installLinux || installPython ||
			installAll || configureUfw || configureDns || updateSources {
			logging.LogSuccess("Script completed selected hardening operations.")
		}
	},
}

var setupSudoEnvCmd = &cobra.Command{
	Use:   "setup-sudo-env",
	Short: "Configure sudo to preserve HARDN_CONFIG environment variable",
	Long: `This command adds a configuration to /etc/sudoers.d/ to ensure that 
the HARDN_CONFIG environment variable is preserved when using sudo with hardn.
This allows you to consistently use a custom configuration file location across
all commands, even when requiring elevated privileges.

This command must be run with sudo privileges.

Example:
  sudo hardn setup-sudo-env`,
	Run: func(cmd *cobra.Command, args []string) {
		// Detect OS
		osInfo, err := osdetect.DetectOS()
		if err != nil {
			logging.LogError("Failed to detect OS: %v", err)
			os.Exit(1)
		}

		// Create service factory
		serviceFactory := infrastructure.NewServiceFactory(provider, osInfo)
		environmentManager := serviceFactory.CreateEnvironmentManager()

		if err := environmentManager.SetupSudoPreservation(); err != nil {
			logging.LogError("Failed to configure sudoers: %v", err)
			os.Exit(1)
		}
		logging.LogSuccess("Sudo environment configured to preserve HARDN_CONFIG")
	},
}
