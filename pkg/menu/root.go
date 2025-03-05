// pkg/menu/root.go

package menu

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/abbott/hardn/pkg/config"
	"github.com/abbott/hardn/pkg/osdetect"
	"github.com/abbott/hardn/pkg/ssh"
	"github.com/abbott/hardn/pkg/style"
	"github.com/abbott/hardn/pkg/utils"
)

// DisableRootMenu handles disabling root SSH access
func DisableRootMenu(cfg *config.Config, osInfo *osdetect.OSInfo) {
	utils.PrintHeader()
	fmt.Println(style.Bolded("Disable Root SSH Access", style.Blue))

	// Check current status of root SSH access
	rootAccessEnabled := CheckRootLoginEnabled(osInfo)
	
	fmt.Println()
	if rootAccessEnabled {
		fmt.Printf("%s %s Root SSH access is currently %s\n", 
			style.Colored(style.Yellow, style.SymWarning),
			style.Bolded("WARNING:"),
			style.Bolded("ENABLED", style.Red))
	} else {
		fmt.Printf("%s Root SSH access is already %s\n", 
			style.Colored(style.Green, style.SymCheckMark),
			style.Bolded("DISABLED", style.Green))
		
		fmt.Printf("\n%s Nothing to do. Press any key to return to the main menu...", style.BulletItem)
		ReadKey()
		return
	}
	
	// Security warning
	fmt.Println(style.Colored(style.Yellow, "\nBefore proceeding, ensure that:"))
	fmt.Printf("%s You have created at least one non-root user with sudo privileges\n", style.BulletItem)
	fmt.Printf("%s You have tested SSH access with this non-root user\n", style.BulletItem)
	fmt.Printf("%s You have a backup method to access this system if SSH fails\n", style.BulletItem)

	// Create menu options
	menuOptions := []style.MenuOption{
		{Number: 1, Title: "Disable root SSH access", Description: "Modify SSH config to prevent root login"},
	}
	
	// Create menu
	menu := style.NewMenu("Select an option", menuOptions)
	menu.SetExitOption(style.MenuOption{
		Number:      0,
		Title:       "Return to main menu",
		Description: "Keep root SSH access enabled",
	})
	
	// Display menu
	menu.Print()
	
	choice := ReadInput()
	
	switch choice {
	case "1":
		fmt.Println("\nDisabling root SSH access...")
		err := ssh.DisableRootSSHAccess(cfg, osInfo)
		if err == nil {
			fmt.Printf("\n%s Root SSH access has been disabled\n", 
				style.Colored(style.Green, style.SymCheckMark))
			
			// Restart SSH service
			fmt.Println(style.Dimmed("Restarting SSH service..."))
			if osInfo.OsType == "alpine" {
				exec.Command("rc-service", "sshd", "restart").Run()
			} else {
				exec.Command("systemctl", "restart", "ssh").Run()
			}
		} else {
			fmt.Printf("\n%s Failed to disable root SSH access: %v\n", 
				style.Colored(style.Red, style.SymCrossMark), err)
		}
	case "0":
		fmt.Println("\nOperation cancelled. Root SSH access remains enabled.")
	default:
		fmt.Printf("\n%s Invalid option. No changes were made.\n", 
			style.Colored(style.Yellow, style.SymWarning))
	}
	
	fmt.Printf("\n%s Press any key to return to the main menu...", style.BulletItem)
	ReadKey()
}

// CheckRootLoginEnabled checks if SSH root login is enabled
func CheckRootLoginEnabled(osInfo *osdetect.OSInfo) bool {
	var sshConfigPath string
	if osInfo.OsType == "alpine" {
		sshConfigPath = "/etc/ssh/sshd_config"
	} else {
		// For Debian/Ubuntu, check both main config and config.d
		sshConfigPath = "/etc/ssh/sshd_config"
		if _, err := os.Stat("/etc/ssh/sshd_config.d/manage.conf"); err == nil {
			sshConfigPath = "/etc/ssh/sshd_config.d/manage.conf"
		}
	}

	file, err := os.Open(sshConfigPath)
	if err != nil {
		return true // Assume vulnerable if can't check
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "PermitRootLogin") {
			fields := strings.Fields(line)
			if len(fields) >= 2 && fields[1] == "no" {
				return false
			}
			return true
		}
	}

	return true // Default to vulnerable if not explicitly set
}