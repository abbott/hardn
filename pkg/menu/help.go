// pkg/menu/help.go

package menu

import (
	"fmt"

	"github.com/abbott/hardn/pkg/style"
	"github.com/abbott/hardn/pkg/utils"
)

// HelpMenu displays usage information and command-line options
func HelpMenu() {
	utils.PrintLogo()
	fmt.Println(style.Bolded("~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~", style.BrightGreen))
	
	fmt.Println(style.Bolded("\nCommand Line Usage:", style.Blue))
	fmt.Println(style.Dimmed("-----------------------------------------------------"))
	fmt.Println("  hardn [options]")
	
	fmt.Println(style.Bolded("\nCommand Line Options:", style.Blue))
	fmt.Println(style.Dimmed("-----------------------------------------------------"))
	
	// Create a formatter with appropriate column widths
	formatter := style.NewStatusFormatter([]string{"Option", "Description"}, 4)

	// Display all command line options
	fmt.Println(formatter.FormatLine(style.SymInfo, style.Cyan, "-f, --config-file string", 
		"Configuration file path", style.Cyan, "", "light"))
	fmt.Println(formatter.FormatLine(style.SymInfo, style.Cyan, "-u, --username string", 
		"Specify username to create", style.Cyan, "", "light"))
	fmt.Println(formatter.FormatLine(style.SymInfo, style.Cyan, "-c, --create-user", 
		"Create user", style.Cyan, "", "light"))
	fmt.Println(formatter.FormatLine(style.SymInfo, style.Cyan, "-d, --disable-root", 
		"Disable root SSH access", style.Cyan, "", "light"))
	fmt.Println(formatter.FormatLine(style.SymInfo, style.Cyan, "-g, --configure-dns", 
		"Configure DNS resolvers", style.Cyan, "", "light"))
	fmt.Println(formatter.FormatLine(style.SymInfo, style.Cyan, "-w, --configure-ufw", 
		"Configure UFW", style.Cyan, "", "light"))
	fmt.Println(formatter.FormatLine(style.SymInfo, style.Cyan, "-r, --run-all", 
		"Run all hardening operations", style.Cyan, "", "light"))
	fmt.Println(formatter.FormatLine(style.SymInfo, style.Cyan, "-n, --dry-run", 
		"Preview changes without applying them", style.Cyan, "", "light"))
	fmt.Println(formatter.FormatLine(style.SymInfo, style.Cyan, "-l, --install-linux", 
		"Install specified Linux packages", style.Cyan, "", "light"))
	fmt.Println(formatter.FormatLine(style.SymInfo, style.Cyan, "-i, --install-python", 
		"Install specified Python packages", style.Cyan, "", "light"))
	fmt.Println(formatter.FormatLine(style.SymInfo, style.Cyan, "-a, --install-all", 
		"Install all specified packages", style.Cyan, "", "light"))
	fmt.Println(formatter.FormatLine(style.SymInfo, style.Cyan, "-s, --configure-sources", 
		"Configure package sources", style.Cyan, "", "light"))
	fmt.Println(formatter.FormatLine(style.SymInfo, style.Cyan, "-p, --print-logs", 
		"View logs", style.Cyan, "", "light"))
	fmt.Println(formatter.FormatLine(style.SymInfo, style.Cyan, "-h, --help", 
		"View usage information", style.Cyan, "", "light"))
	
	// Usage examples
	fmt.Println(style.Bolded("\nExamples:", style.Blue))
	fmt.Println(style.Dimmed("-----------------------------------------------------"))
	fmt.Printf("%s Run all hardening operations:\n", style.BulletItem)
	fmt.Printf("    %s\n", style.Colored(style.Cyan, "sudo hardn -r"))
	
	fmt.Printf("\n%s Create a non-root user with SSH access:\n", style.BulletItem)
	fmt.Printf("    %s\n", style.Colored(style.Cyan, "sudo hardn -u george -c"))
	
	fmt.Printf("\n%s Using a custom configuration file:\n", style.BulletItem)
	fmt.Printf("    %s\n", style.Colored(style.Cyan, "sudo hardn -f /path/to/config.yml"))

	fmt.Printf("\n%s Using environment variable for configuration:\n", style.BulletItem)
	fmt.Printf("    %s\n", style.Colored(style.Cyan, "export HARDN_CONFIG=/path/to/config.yml"))
	fmt.Printf("    %s\n", style.Colored(style.Cyan, "sudo hardn"))

	fmt.Printf("\n%s Press any key to return to the main menu...", style.BulletItem)
	ReadKey()
}