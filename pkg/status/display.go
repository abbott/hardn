// pkg/status/display.go
package status

import (
	"strings"

	"github.com/abbott/hardn/pkg/style"
)

// RiskStatus formats a risk status line with appropriate styling
func RiskStatus(symbol string, color string, label string, status string, description string) string {
	padding := strings.Repeat(" ", 6) // "Risk" is short, so hardcode reasonable padding

	return style.Colored(color, symbol) + " " + label +
		padding + style.Bolded(status, color) + " " + style.Dimmed(description)
}

// Additional display-related functions can be added here in the future