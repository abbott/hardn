// pkg/menu/input.go
package menu

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// Shared reader for all menus
var reader = bufio.NewReader(os.Stdin)

// ReadInput reads a line of input from the user
func ReadInput() string {
	input, _ := reader.ReadString('\n')
	return strings.TrimSpace(input)
}

// ReadKey reads a single key pressed by the user
func ReadKey() string {
	// Configure terminal for raw input
	exec.Command("stty", "-F", "/dev/tty", "cbreak", "min", "1").Run()
	defer exec.Command("stty", "-F", "/dev/tty", "-cbreak").Run()

	// Read the first byte
	var firstByte = make([]byte, 1)
	os.Stdin.Read(firstByte)

	// If it's an escape character (27), read and discard the sequence
	if firstByte[0] == 27 {
		// Read and discard the next two bytes (common for arrow keys)
		var discardBytes = make([]byte, 2)
		os.Stdin.Read(discardBytes)

		// Return empty to indicate a special key was pressed
		return ""
	}

	return string(firstByte)
}

// ReadMenuInput reads input for a menu, handling 'q' as a special case for quitting
// Returns the user's choice, or "q" to indicate the user wants to quit
func ReadMenuInput() string {
	// First check if q is pressed immediately without Enter
	firstKey := ReadKey()
	if firstKey == "q" || firstKey == "Q" {
			fmt.Println("q")
			return "q" // Special exit code
	}

	// If we received a key, combine it with the rest of the input
	if firstKey != "" {
			// Read the rest of the line (if any)
			input, _ := reader.ReadString('\n')
			return firstKey + strings.TrimSpace(input)
	}

	// If no key was detected (e.g., arrow key), fall back to regular input
	input, _ := reader.ReadString('\n')
	return strings.TrimSpace(input)
}