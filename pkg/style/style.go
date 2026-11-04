package style

import (
	"os"
	"regexp"
)

const (
	Reset = "\033[0m"
	Green = "\033[32m"
)

var UseColors = true

func init() {
	// Check for NO_COLOR environment variable
	if os.Getenv("NO_COLOR") != "" {
		UseColors = false
	}
}

// Colored applies a color to text and resets afterwards
func Colored(color string, text string) string {
	if !UseColors {
		return text
	}
	return color + text + Reset
}

var ansiRegex = regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]`)

// StripAnsi removes ANSI escape codes from a string to get its true display length
func StripAnsi(str string) string {
	return ansiRegex.ReplaceAllString(str, "")
}
