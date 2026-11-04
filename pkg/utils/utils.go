package utils

import (
	"fmt"

	"github.com/abbott/hardn/pkg/style"
)

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
