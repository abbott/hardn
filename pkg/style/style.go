package style

// Style provides consistent terminal formatting constants and functions
// Example usage: fmt.Printf("Important: %s\n", style.Bolded("This is highlighted text", style.Red))

import (
	"strings"
	"regexp"
	"fmt"
)

const (
	// Reset all styles
	Reset 					= "\033[0m"

	// Text colors - normal intensity
	Black   				= "\033[30m"
	Red     				= "\033[31m"
	Green   				= "\033[32m"
	Yellow  				= "\033[33m"
	Blue    				= "\033[34m"
	Magenta 				= "\033[35m"
	Cyan    				= "\033[36m"
	White   				= "\033[37m"

	BoldRed         = "\033[1;31;22m"

	// Text colors - bright/light intensity
	BrightBlack   	= "\033[90m"
	BrightRed     	= "\033[91m"
	BrightGreen   	= "\033[92m"
	BrightYellow  	= "\033[93m"
	BrightBlue    	= "\033[94m"
	BrightMagenta 	= "\033[95m"
	BrightCyan    	= "\033[96m"
	BrightWhite   	= "\033[97m"

	// Special colors
	DeepRed 				= "\033[38;5;88m"  // A more intense/deeper red

	// Background colors - normal intensity
	BgBlack   			= "\033[40m"
	BgRed     			= "\033[41m"
	BgGreen   			= "\033[42m"
	BgYellow  			= "\033[43m"
	BgBlue    			= "\033[44m"
	BgMagenta 			= "\033[45m"
	BgCyan    			= "\033[46m"
	BgWhite   			= "\033[47m"

	// Background colors - bright/light intensity
	BgBrightBlack   = "\033[100m"
	BgBrightRed     = "\033[101m"
	BgBrightGreen   = "\033[102m"
	BgBrightYellow  = "\033[103m"
	BgBrightBlue    = "\033[104m"
	BgBrightMagenta = "\033[105m"
	BgBrightCyan    = "\033[106m"
	BgBrightWhite   = "\033[107m"

	// Text effects
	Bold      			= "\033[1m"
	Dim       			= "\033[2m"
	Italic    			= "\033[3m"
	Underline 			= "\033[4m"
	Blink     			= "\033[5m"
	Reverse   			= "\033[7m"
	Hidden    			= "\033[8m"

	// Cursor control
	CursorOn  			= "\033[?25h"
	CursorOff 			= "\033[?25l"

	// Common symbols
	SymAsterisk  		= "✱"
	SymDotTri       = "⛬"
	SymInfo      		= "ℹ" 
	SymCheckMark 		= "✓"
	SymCrossMark 		= "✗"

	SymEmDash    		= "—"
	SymEnDash    		= "–"
	SymDash    	  	= "-"
	SymEllipsis  		= "..."

	SymArrowUp    	= "↑"
	SymArrowDown  	= "↓"
	SymArrowLeft  	= "←"
	SymArrowRight 	= "→"
	SymDoubleLeft   = "«"
	SymDoubleRight  = "»"

	SymMultiply     = "×"
	SymInfinity     = "∞"
	SymDegree       = "°"
	SymApprox       = "≈"
	SymPercent      = "%"

	SymEnabled      = "◎"
	SymBolt         = "⌁"
	SymFlag         = "⚑"
	SymWarning      = "▲"
	SymStatus       = "▣"

	// Additional constants for layout
	Indent     = "    "
	BulletItem = Bold + SymArrowRight + Reset + " "
)

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

// Apply italic style with an optional color
func Italicized(text string, color ...string) string {
	if len(color) > 0 {
		return Italic + color[0] + text + Reset
	}
	return Italic + text + Reset
}

// Apply underline style with an optional color
func Underlined(text string, color ...string) string {
	if len(color) > 0 {
		return Underline + color[0] + text + Reset
	}
	return Underline + text + Reset
}

// Colored applies a color to text and resets afterwards
func Colored(color string, text string) string {
	return color + text + Reset
}

// StyledText applies multiple styles to text and resets afterwards
func StyledText(text string, styles ...string) string {
	combined := ""
	for _, style := range styles {
		combined += style
	}
	return combined + text + Reset
}

// Success formats text in green with a checkmark prefix
func Success(text string) string {
	return Green + SymCheckMark + Reset + " " + text
}

// Error formats text in red with a cross mark prefix
func Error(text string) string {
	return Red + SymCrossMark + Reset + " " + text
}

// Warning formats text in yellow with a warning symbol prefix
func Warning(text string) string {
	return Yellow + SymBolt + Reset + " " + text
}

// Info formats text in cyan
func Info(text string) string {
	return Cyan + text + Reset
}

// Header creates a section header with bold blue text
func Header(text string) string {
	return "\n" + Bold + Blue + text + Reset + "\n" + Blue + strings.Repeat("-", len(text)) + Reset
}

func SubHeader(text string) string {
	return "\n" + Underline + Bold + Blue + text + Reset + "\n"
}

// Section creates a formatted section with an indented title
func Section(title string, indent int) string {
	indentation := strings.Repeat(" ", indent)
	return indentation + Bold + title + Reset
}

// Hyperlink creates a terminal hyperlink (works in some terminals)
func Hyperlink(text, url string) string {
	return "\033]8;;" + url + "\033\\" + text + "\033]8;;\033\\"
}

// Text utility functions
func CenterText(text string, width int) string {
	if len(text) >= width {
		return text
	}
	
	leftPadding := (width - len(text)) / 2
	rightPadding := width - len(text) - leftPadding
	
	return strings.Repeat(" ", leftPadding) + text + strings.Repeat(" ", rightPadding)
}

func PadRight(text string, width int) string {
	if len(text) >= width {
		return text
	}
	
	return text + strings.Repeat(" ", width-len(text))
}

var ansiRegex = regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]`)

// StripAnsi removes ANSI escape codes from a string to get its true display length
func StripAnsi(str string) string {
	return ansiRegex.ReplaceAllString(str, "")
}

// StatusLine creates a formatted status line with a symbol, label, status, and description
func StatusLine(symbol string, symbolColor string, label string, status string, statusColor string, description string) string {
	return Colored(symbolColor, symbol) + " " + label + ": " + Bolded(status, statusColor) + " " + Dimmed(description)
}

func Status(label string, status string, description string) string {
	return StatusLine(SymCrossMark, BrightRed, label, status, BrightRed, description)
}

// ErrorStatus creates a red formatted error status line with an X symbol
func ErrorStatus(label string, status string, description string) string {
	return StatusLine(SymCrossMark, BrightRed, label, status, BrightRed, description)
}

// SuccessStatus creates a green formatted success status line with a checkmark
func SuccessStatus(label string, status string, description string) string {
	return StatusLine(SymCheckMark, Green, label, status, Green, description)
}

// WarningStatus creates a yellow formatted warning status line
func WarningStatus(label string, status string, description string) string {
	return StatusLine(SymBolt, Yellow, label, status, Yellow, description)
}

// PrintHeader prints a header with proper spacing above and below
func PrintHeader(header string, color string) {
	fmt.Println()
	fmt.Println(Bolded(header, color))
	fmt.Println()
}

// PrintSeparator prints a decorative separator with the given text centered
func PrintSeparator(text string, width int, sepChar string, color string) {
	if len(text) > 0 {
		// If there's text, center it within the separator
		textWithSpaces := " " + text + " "
		textLen := len(StripAnsi(textWithSpaces))
		leftLen := (width - textLen) / 2
		rightLen := width - textLen - leftLen
		
		fmt.Println()
		fmt.Println(
			Bolded(strings.Repeat(sepChar, leftLen), color) +
			Bolded(textWithSpaces, color) +
			Bolded(strings.Repeat(sepChar, rightLen), color),
		)
		fmt.Println()
	} else {
		// Just a plain separator line
		fmt.Println()
		fmt.Println(Bolded(strings.Repeat(sepChar, width), color))
		fmt.Println()
	}
}

// PrintStatusLine prints a status line with consistent formatting
func PrintStatusLine(symbol string, symbolColor string, label string, status string, statusColor string, description string) {
	fmt.Println(StatusLine(symbol, symbolColor, label, status, statusColor, description))
}

// PrintBlankLine prints a blank line for spacing
func PrintBlankLine() {
	fmt.Println()
}

// StatusFormatter provides consistent formatting for status lines with dynamic alignment
type StatusFormatter struct {
	labels       []string
	maxLabelLen  int
	buffer       int
	initialized  bool
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
	// Add buffer for spacing
	sf.maxLabelLen += sf.buffer
	sf.initialized = true
}


// FormatLine formats a status line with proper alignment
func (sf *StatusFormatter) FormatLine(symbol string, symbolColor string, 
	label string, status string, statusColor string, description string, statusWeight string) string {

	if !sf.initialized {
		sf.Initialize()
	}

	// Calculate padding needed for label (strip ANSI codes for accuracy)
	labelText := StripAnsi(label)
	padding := strings.Repeat(" ", sf.maxLabelLen-len(labelText))
	symbol = Colored(symbolColor, symbol)

	if statusWeight == "bold" {
		status = Bolded(status, statusColor)
	} else {
		status = Colored(statusColor, status)
	}

	return symbol + " " + label + padding + status + " " + Dimmed(description)
}

func (sf *StatusFormatter) FormatEmLine(symbol string, label string, status string, 
	statusColor string, description string, statusWeight string) string {

	if !sf.initialized {
		sf.Initialize()
	}

	// Calculate padding needed for label (strip ANSI codes for accuracy)
	labelText := StripAnsi(label)
	padding := strings.Repeat(" ", sf.maxLabelLen-len(labelText))

	if statusWeight == "bold" {
		status = Bolded(status, statusColor)
	} else {
		status = Colored(statusColor, status)
	}

	return symbol + " " + label + padding + status + " " + Dimmed(description)
}



// FormatSuccess creates a success status line
func (sf *StatusFormatter) FormatSuccess(label string, status string, description string) string {
	return sf.FormatLine(SymEnabled, Green, label, status, Green, description, "light")
}

// FormatError creates an error status line
func (sf *StatusFormatter) FormatCheck(label string, status string, description string) string {
	return sf.FormatEmLine(SymCheckMark, label, status, BrightRed, description, "bold")
}

// FormatError creates an error status line
func (sf *StatusFormatter) FormatError(label string, status string, description string) string {
	return sf.FormatLine(SymCrossMark, BrightRed, label, status, BrightRed, description, "bold")
}

// FormatWarning creates a warning status line
func (sf *StatusFormatter) FormatWarning(label string, status string, description string) string {
	return sf.FormatLine(SymWarning, Red, label, status, Red, description, "light")
}

func PrintDivider(char string, length int, style ...string) {
	// Default to dimmed style if none provided
	styleCode := Dim
	if len(style) > 0 {
			styleCode = style[0]
	}
	
	// Print the divider
	fmt.Println(Colored(styleCode, strings.Repeat(char, length)))
}