package main

import (
	"fmt"
	"os"
	osuser "os/user"

	"github.com/fatih/color"
	"github.com/spf13/cobra"

	"github.com/abbott/hardn/pkg/config"
	"github.com/abbott/hardn/pkg/infrastructure"
	"github.com/abbott/hardn/pkg/interfaces"
	"github.com/abbott/hardn/pkg/logging"
	"github.com/abbott/hardn/pkg/menu"
	"github.com/abbott/hardn/pkg/osdetect"
	"github.com/abbott/hardn/pkg/packages"
	"github.com/abbott/hardn/pkg/security"
	"github.com/abbott/hardn/pkg/ssh"
	"github.com/abbott/hardn/pkg/updates"
	"github.com/abbott/hardn/pkg/user"
	"github.com/abbott/hardn/pkg/utils"
)

// Version information - populated by build flags
var (
	Version   string // Semantic version
	BuildDate string // Build date in ISO format
	GitCommit string // Git commit hash
)

var (
	configFile    string
	username      string
	dryRun        bool
	createUser    bool
	disableRoot   bool
	installLinux  bool
	installPython bool
	installAll    bool
	configureUfw  bool
	configureDns  bool
	runAll        bool
	updateSources bool
	printLogs     bool
	showVersion   bool // Flag to display version information
	setupSudoEnv  bool
	cfg           *config.Config
)

var provider = interfaces.NewProvider()

var useNewArchitecture bool

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

	rootCmd.PersistentFlags().BoolVar(&useNewArchitecture, "use-new-arch", false,
		"Use new architecture implementation")

	rootCmd.PersistentFlags().StringVarP(&configFile, "config", "f", "",
		"Specify configuration file path")

	rootCmd.AddCommand(setupSudoEnvCmd)
	// "Specify configuration file path (optionally set HARDN_CONFIG as variable)")
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
}

var rootCmd = &cobra.Command{
	Use:   "hardn",
	Short: "Linux hardening tool",
	Long:  `A simple hardening tool for Debian, Ubuntu, Proxmox and Alpine Linux.`,
	Run: func(cmd *cobra.Command, args []string) {
		// Check if version flag is set and display version info
		if showVersion {
			fmt.Println("hardn - Linux hardening tool")
			fmt.Printf("Version:    %s\n", Version)
			if BuildDate != "" {
				fmt.Printf("Build Date: %s\n", BuildDate)
			}
			if GitCommit != "" {
				fmt.Printf("Git Commit: %s\n", GitCommit)
			}
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

		if useNewArchitecture {
			
			// Create service factory
			// provider := interfaces.NewProvider()


			logging.LogSuccess("The value of useNewArchitecture is %t", useNewArchitecture)
			
			// Create service factory
			serviceFactory := infrastructure.NewServiceFactory(provider, osInfo)
			
			// Create menu factory
			menuFactory := infrastructure.NewMenuFactory(serviceFactory, cfg, osInfo)
			
			// Create and show main menu
			mainMenu := menuFactory.CreateMainMenu()
			mainMenu.ShowMainMenu()

			factory := infrastructure.NewServiceFactory(provider, osInfo)

			// Handle SSH operations with new architecture
			if disableRoot {
				sshManager := factory.CreateSSHManager()
				if err := sshManager.DisableRootAccess(); err != nil {
					logging.LogError("Failed to disable root SSH access: %v", err)
				} else {
					logging.LogSuccess("Disabled root SSH access")
				}
			}

			// Handle firewall operations with new architecture
			if configureUfw {
				firewallManager := factory.CreateFirewallManager()
				if err := firewallManager.ConfigureSecureFirewall(cfg.SshPort, []int{}); err != nil {
					logging.LogError("Failed to configure firewall: %v", err)
				} else {
					logging.LogSuccess("Firewall configured successfully")
				}
			}

			// Handle DNS operations with new architecture
			if configureDns {
				dnsManager := factory.CreateDNSManager()
				if err := dnsManager.ConfigureDNS(cfg.Nameservers, "lan"); err != nil {
					logging.LogError("Failed to configure DNS: %v", err)
				} else {
					logging.LogSuccess("DNS configured successfully")
				}
			}
			
		} else {

			logging.LogSuccess("The value of useNewArchitecture is %t", useNewArchitecture)

			// If no flags provided, show menu
			if !createUser && !disableRoot && !installLinux && !installPython &&
				!installAll && !configureUfw && !configureDns && !runAll &&
				!updateSources && !printLogs {
				menu.ShowMainMenu(cfg, osInfo)
				return
			}

			// Process command line flags
			if runAll {
				runAllHardening(cfg, osInfo)
				return
			}

			// Handle individual operations
			if updateSources || installLinux || installPython || installAll || createUser || runAll {
				packages.WriteSources(cfg, osInfo)
				if osInfo.OsType != "alpine" && osInfo.IsProxmox {
					packages.WriteProxmoxRepos(cfg, osInfo)
				}
			}

			if disableRoot {
				err := ssh.DisableRootSSHAccess(cfg, osInfo)
				if err != nil {
					logging.LogError("Failed to disable root SSH access: %v", err)
				} else {
					logging.LogSuccess("Disabled root SSH access")
				}
			}

			if installPython || installAll {
				packages.InstallPythonPackages(cfg, osInfo)
			}

			if installLinux || installAll || runAll {
				installLinuxPackages(cfg, osInfo)
			}

			if createUser || runAll {
				// Install sudo if needed
				if osInfo.OsType == "alpine" {
					if !packages.IsPackageInstalled("sudo") {
						packages.InstallPackages([]string{"sudo"}, osInfo, cfg)
					}
				} else {
					if !packages.IsPackageInstalled("sudo") {
						packages.InstallPackages([]string{"sudo"}, osInfo, cfg)
					}
				}

				err := user.CreateUser(cfg.Username, cfg, osInfo)
				if err != nil {
					logging.LogError("Failed to create user: %v", err)
				}
				ssh.WriteSSHConfig(cfg, osInfo)
			}

			if runAll && cfg.EnableAppArmor {
				security.SetupAppArmor(cfg, osInfo)
			}

			if runAll && cfg.EnableLynis {
				security.SetupLynis(cfg, osInfo)
			}

			if runAll && cfg.EnableUnattendedUpgrades {
				updates.SetupUnattendedUpgrades(cfg, osInfo)
			}

			if printLogs {
				logging.PrintLogs(cfg.LogFile)
			}

			// Output completion message
			if runAll {
				logging.LogSuccess("Script completed all hardening operations.")
			} else if createUser || disableRoot || installLinux || installPython ||
				installAll || configureUfw || configureDns || updateSources {
				logging.LogSuccess("Script completed selected hardening operations.")
			}

			if setupSudoEnv {
				if err := utils.SetupSudoEnvPreservation(); err != nil {
					logging.LogError("Failed to configure sudoers: %v", err)
					os.Exit(1)
				}
				return
			}
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
		if err := utils.SetupSudoEnvPreservation(); err != nil {
			logging.LogError("Failed to configure sudoers: %v", err)
			os.Exit(1)
		}
	},
}

// Run all hardening operations
func runAllHardening(cfg *config.Config, osInfo *osdetect.OSInfo) {
	utils.PrintLogo()
	logging.LogInfo("Running complete system hardening...")

	// Setup hushlogin
	utils.SetupHushlogin(cfg)

	// Update package repositories
	packages.WriteSources(cfg, osInfo)
	if osInfo.OsType != "alpine" && osInfo.IsProxmox {
		packages.WriteProxmoxRepos(cfg, osInfo)
	}

	// Install packages
	installLinuxPackages(cfg, osInfo)

	// Create user
	if cfg.Username != "" {
		err := user.CreateUser(cfg.Username, cfg, osInfo)
		if err != nil {
			logging.LogError("Failed to create user: %v", err)
		}
	}

	// Configure SSH
	ssh.WriteSSHConfig(cfg, osInfo)

	// Disable root SSH access if requested
	if cfg.DisableRoot {
		ssh.DisableRootSSHAccess(cfg, osInfo)
	}

	// Setup AppArmor if enabled
	if cfg.EnableAppArmor {
		security.SetupAppArmor(cfg, osInfo)
	}

	// Setup Lynis if enabled
	if cfg.EnableLynis {
		security.SetupLynis(cfg, osInfo)
	}

	// Setup unattended upgrades if enabled
	if cfg.EnableUnattendedUpgrades {
		updates.SetupUnattendedUpgrades(cfg, osInfo)
	}

	logging.LogSuccess("System hardening completed successfully!")
	fmt.Printf("Check the log file at %s for details.\n", cfg.LogFile)
}

// Install Linux packages based on OS type
func installLinuxPackages(cfg *config.Config, osInfo *osdetect.OSInfo) {
	if osInfo.OsType == "alpine" {
		fmt.Println("\nInstalling Alpine Linux packages...")

		// Install core Alpine packages first
		if len(cfg.AlpineCorePackages) > 0 {
			logging.LogInfo("Installing Alpine core packages...")
			packages.InstallPackages(cfg.AlpineCorePackages, osInfo, cfg)
		}

		// Check subnet to determine which package sets to install
		isDmz, _ := utils.CheckSubnet(cfg.DmzSubnet, provider.Network)
		// isDmz, _ := utils.CheckSubnet(cfg.DmzSubnet)
		if isDmz {
			if len(cfg.AlpineDmzPackages) > 0 {
				logging.LogInfo("Installing Alpine DMZ packages...")
				packages.InstallPackages(cfg.AlpineDmzPackages, osInfo, cfg)
			}
		} else {
			// Install both
			if len(cfg.AlpineDmzPackages) > 0 {
				logging.LogInfo("Installing Alpine DMZ packages...")
				packages.InstallPackages(cfg.AlpineDmzPackages, osInfo, cfg)
			}
			if len(cfg.AlpineLabPackages) > 0 {
				logging.LogInfo("Installing Alpine LAB packages...")
				packages.InstallPackages(cfg.AlpineLabPackages, osInfo, cfg)
			}
		}
	} else {
		// Install core Linux packages first
		if len(cfg.LinuxCorePackages) > 0 {
			logging.LogInfo("Installing Linux core packages...")
			packages.InstallPackages(cfg.LinuxCorePackages, osInfo, cfg)
		}

		// Check subnet to determine which package sets to install
		isDmz, _ := utils.CheckSubnet(cfg.DmzSubnet, provider.Network)
		// isDmz, _ := utils.CheckSubnet(cfg.DmzSubnet)
		if isDmz {
			if len(cfg.LinuxDmzPackages) > 0 {
				logging.LogInfo("Installing Debian DMZ packages...")
				packages.InstallPackages(cfg.LinuxDmzPackages, osInfo, cfg)
			}
		} else {
			// Install both
			if len(cfg.LinuxDmzPackages) > 0 {
				logging.LogInfo("Installing Debian DMZ packages...")
				packages.InstallPackages(cfg.LinuxDmzPackages, osInfo, cfg)
			}
			if len(cfg.LinuxLabPackages) > 0 {
				logging.LogInfo("Installing Debian Lab packages...")
				packages.InstallPackages(cfg.LinuxLabPackages, osInfo, cfg)
			}
		}
	}
}
