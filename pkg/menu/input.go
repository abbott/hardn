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

// ReadMenuInput reads input for a menu, supporting both immediate 'q' exit and
// normal buffered input with backspace support for other entries
func ReadMenuInput() string {
	// fmt.Print("> ")

	var buffer strings.Builder
	var displayedChars int

	for {
		// Read a single key in raw mode
		key := ReadRawKey()

		// Handle Enter (return the result)
		if key == "\r" || key == "\n" {
			fmt.Println() // Move to next line
			return buffer.String()
		}

		// Handle immediate 'q' exit if it's the first key
		if buffer.Len() == 0 && (key == "q" || key == "Q") {
			fmt.Println("q")
			return "q"
		}

		// Handle backspace/delete
		if key == "\b" || key == "\x7f" { // \b = backspace, \x7f = delete
			if buffer.Len() > 0 {
				// Remove last character from our buffer
				str := buffer.String()
				buffer.Reset()
				buffer.WriteString(str[:len(str)-1])

				// Update display (backspace, space, backspace)
				fmt.Print("\b \b")
				displayedChars--
			}
			continue
		}

		// Only accept digits, q/Q and control characters
		if (key >= "0" && key <= "9") || key == "q" || key == "Q" {
			buffer.WriteString(key)
			fmt.Print(key) // Echo the character
			displayedChars++
		}
	}
}

// ReadRawKey reads a single key in raw mode
func ReadRawKey() string {
	// Configure terminal for raw input
	exec.Command("stty", "-F", "/dev/tty", "raw", "-echo").Run()
	defer exec.Command("stty", "-F", "/dev/tty", "sane").Run()

	var b = make([]byte, 1)
	os.Stdin.Read(b)

	// Convert control characters to strings
	if b[0] == 13 {
		return "\r" // Return/Enter key
	} else if b[0] == 127 {
		return "\x7f" // Delete key
	} else if b[0] == 8 {
		return "\b" // Backspace key
	} else if b[0] == 27 {
		// Possibly an arrow key or other escape sequence
		// Read and discard two more bytes
		var seq = make([]byte, 2)
		os.Stdin.Read(seq)
		return "" // Ignore escape sequences
	}

	return string(b)
}
