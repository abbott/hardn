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
