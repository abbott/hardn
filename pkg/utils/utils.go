package utils

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/abbott/hardn/pkg/config"
	"github.com/abbott/hardn/pkg/interfaces"
	"github.com/abbott/hardn/pkg/logging"
	"github.com/abbott/hardn/pkg/style"
)

// PrintHeader prints a standard header
func PrintPounds() {
	fmt.Print(style.Colored(style.Green, "#######################################################################"))
}

func PrintTilda() {
	fmt.Print(style.Colored(style.Green, "~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~"))
}

func ClearScreen() {
	fmt.Print("\033[H\033[2J")
}

// PrintHeader prints a standard header
func PrintHeader() {
	// Clear screen
	ClearScreen()

	// Print header without an extra newline
	PrintTilda()
	// PrintPounds()
	fmt.Println()
}

// PrintLogo prints the script logo
// func PrintLogo() {
// 	fmt.Print(style.Dimmed(`
// |      _   _               _            _     _                       |
// |     | | | | __ _ _ __ __| |_ __      | |   (_)_ __  _   ___  __     |
// |     | |_| |/ _  | '__/ _  | '_ \     | |   | | '_ \| | | \ \/ /     |
// |     |  _  | (_| | | | (_| | | | |    | |___| | | | | |_| |>  <      |
// |     |_| |_|\__,_|_|  \__,_|_| |_|    |_____|_|_| |_|\__,_/_/\_\     |
// `))
// 	fmt.Println()
// }

func PrintLogo() {
	fmt.Print(style.Dimmed(`
       _   _               _            _     _
      | | | | __ _ _ __ __| |_ __      | |   (_)_ __  _   ___  __
      | |_| |/ _  | '__/ _  | '_ \     | |   | | '_ \| | | \ \/ /
      |  _  | (_| | | | (_| | | | |    | |___| | | | | |_| |>  <
      |_| |_|\__,_|_|  \__,_|_| |_|    |_____|_|_| |_|\__,_/_/\_\
`))
	fmt.Println()
}

// func PrintLogo() {
// 	fmt.Print("\033[H\033[2J")
// 	fmt.Print(style.Colored(style.Green, "#######################################################################"))
// 	fmt.Print(`
//        _   _               _            _     _
//       | | | | __ _ _ __ __| |_ __      | |   (_)_ __  _   ___  __
//       | |_| |/ _  | '__/ _  | '_ \     | |   | | '_ \| | | \ \/ /
//       |  _  | (_| | | | (_| | | | |    | |___| | | | | |_| |>  <
//       |_| |_|\__,_|_|  \__,_|_| |_|    |_____|_|_| |_|\__,_/_/\_\
// `)
// 	fmt.Println()
// }

// run a command and returns its output
func RunCommand(name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return string(output), fmt.Errorf("command %s %v failed: %w", name, args, err)
	}
	return string(output), nil
}

// check if the specified subnet is present in the system's interfaces
func CheckSubnet(subnet string, networkOps interfaces.NetworkOperations) (bool, error) {
	return networkOps.CheckSubnet(subnet)
}

// create a .hushlogin file in the home directory
func SetupHushlogin(cfg *config.Config) error {
	if cfg.DryRun {
		logging.LogInfo("[DRY-RUN] Write ~/.hushlogin")
		return nil
	}

	homeDir, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("failed to get home directory: %w", err)
	}

	hushloginPath := filepath.Join(homeDir, ".hushlogin")
	if _, err := os.Stat(hushloginPath); os.IsNotExist(err) {
		file, err := os.Create(hushloginPath)
		if err != nil {
			return fmt.Errorf("failed to create .hushlogin file at %s: %w", hushloginPath, err)
		}
		defer file.Close()

		logging.LogInfo("Created ~/.hushlogin")
	}

	return nil
}
