package style

import (
	"os"
	"regexp"
	"strings"
)

const (
	Gray10 = "\033[38;5;240m"

	Reset = "\033[0m"

	// Text colors - normal intensity
	Royal = "\033[38;5;39m"

	Red   = "\033[31m"
	Green = "\033[32m"

	BgGray02 = "\033[48;5;232m"
	BgGray03 = "\033[48;5;233m"

	BgDarkRed = "\033[1;37;48;5;160m" // white bold

	// Text effects
	Bold = "\033[1m"
	Dim  = "\033[2m"

	SymDash = "-"
)

var UseColors = true

func init() {
	// Check for NO_COLOR environment variable
	if os.Getenv("NO_COLOR") != "" {
		UseColors = false
	}
}

// Apply bold style with an optional color
func Bolded(text string, color ...string) string {
	if len(color) > 0 {
		return Bold + color[0] + text + Reset
	}
	return Bold + text + Reset
}

// Apply dim style with an optional color
func Dimmed(text string, color ...string) string {
	if len(color) > 0 {
		return Dim + color[0] + text + Reset
	}
	return Dim + text + Reset
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

// StatusFormatter provides consistent formatting for status lines with dynamic alignment
type StatusFormatter struct {
	labels      []string
	maxLabelLen int
	buffer      int
	initialized bool
}

// NewStatusFormatter creates a new formatter with the given labels and buffer size
func NewStatusFormatter(labels []string, buffer int) *StatusFormatter {
	formatter := &StatusFormatter{
		labels: labels,
		buffer: buffer,
	}
	formatter.Initialize()
	return formatter
}

// Initialize calculates the maximum label length
func (sf *StatusFormatter) Initialize() {
	sf.maxLabelLen = 0
	for _, label := range sf.labels {
		if len(label) > sf.maxLabelLen {
			sf.maxLabelLen = len(label)
		}
	}
	// buffer for spacing
	sf.maxLabelLen += sf.buffer
	sf.initialized = true
}

// FormatLine formats a status line with proper alignment
func (sf *StatusFormatter) FormatLine(symbol string, symbolColor string,
	label string, status string, statusColor string, description string, opts ...string) string {

	if !sf.initialized {
		sf.Initialize()
	}

	// Check if padding should be disabled (optional parameter)
	warnDescription := false
	darkDescription := false
	setBold := false
	padSpacing := true
	padSymbol := true
	for _, opt := range opts {
		switch opt {
		case "bold":
			setBold = true
		case "no-spacing", "nospacing":
			padSpacing = false
		case "no-indent", "noindent":
			padSymbol = false
		case "dark":
			darkDescription = true
		case "warn":
			warnDescription = true
		}
	}
	// Calculate padding needed for label (strip ANSI codes for accuracy)
	labelText := StripAnsi(label)

	var symbolPadding string
	if !padSymbol {
		// Use fixed minimal padding when padding is disabled
		symbolPadding = "" // Just one space between label and status
	} else {
		symbolPadding = "  "
	}

	var padding string
	if !padSpacing {
		// Use fixed minimal padding when padding is disabled
		padding = " " // Just one space between label and status
	} else {
		// Fix: Ensure padding size is never negative
		paddingSize := sf.maxLabelLen - len(labelText)
		if paddingSize < 0 {
			paddingSize = 0 // Prevent negative repeat count
		}

		if !padSymbol {
			paddingSize += 10
		} else {
			paddingSize += 5
		}

		padding = strings.Repeat(" ", paddingSize)
	}

	symbol = Colored(symbolColor, symbol)

	// Check if the color is a gray value
	isGrayColor := strings.Contains(statusColor, "Gray")

	if setBold {
		status = Bolded(status, statusColor)
	} else if isGrayColor {
		// For gray colors, apply both Dimmed and Colored
		status = Dimmed(Colored(statusColor, status))
	} else {
		status = Colored(statusColor, status)
	}

	var descripitionStyle string
	if darkDescription {
		descripitionStyle = Dimmed(description, Gray10)
	} else if warnDescription {
		descripitionStyle = Colored(Red, description)
	} else {
		descripitionStyle = Dimmed(description)
	}

	return symbol + symbolPadding + label + padding + status + " " + descripitionStyle
}

func (sf *StatusFormatter) FormatBullet(label string, status string, description string, opts ...string) string {
	return sf.FormatLine(SymDash, Dim, label, status, Dim, description, opts...)
}
