package style

import (
	"fmt"
	"os"
	"regexp"
	"strings"
)

type MenuOption struct {
	Number      int
	Title       string
	Description string
	Style       string
}

type Menu struct {
	title          string
	options        []MenuOption
	exitOption     *MenuOption
	prompt         string
	maxNumLen      int
	titleWidth     int
	dryRunEnabled  bool
	showDryRunInfo bool
	indentation    string
}

const (
	Gray01 = "\033[38;5;231m"
	Gray02 = "\033[38;5;232m"
	Gray03 = "\033[38;5;233m"
	Gray04 = "\033[38;5;234m"
	Gray05 = "\033[38;5;235m"
	Gray06 = "\033[38;5;236m"
	Gray07 = "\033[38;5;237m"
	Gray08 = "\033[38;5;238m"
	Gray09 = "\033[38;5;239m"
	Gray10 = "\033[38;5;240m"
	Gray11 = "\033[38;5;241m"
	Gray12 = "\033[38;5;242m"
	Gray13 = "\033[38;5;243m"
	Gray14 = "\033[38;5;244m"
	Gray15 = "\033[38;5;245m"
	Gray16 = "\033[38;5;246m"
	Gray17 = "\033[38;5;247m"

	Reset = "\033[0m"

	// Text colors - normal intensity
	Royal = "\033[38;5;39m"

	Black = "\033[30m"
	Red   = "\033[31m"
	Green = "\033[32m"
	// DarkGreen = "\033[38;5;40m"
	DarkGreen = "\033[38;5;76m"
	Yellow    = "\033[33m"
	Blue      = "\033[34m"
	Magenta   = "\033[35m"
	Cyan      = "\033[36m"
	White     = "\033[37m"

	BoldRed = "\033[1;31;22m"

	// Text colors - bright/light intensity
	BrightBlack   = "\033[90m"
	BrightRed     = "\033[91m"
	BrightGreen   = "\033[92m"
	BrightYellow  = "\033[93m"
	BrightBlue    = "\033[94m"
	BrightMagenta = "\033[95m"
	BrightCyan    = "\033[96m"
	BrightWhite   = "\033[97m"

	// Special colors
	DeepRed = "\033[38;5;88m" // A more intense/deeper red

	BgGray01 = "\033[48;5;231m"
	BgGray02 = "\033[48;5;232m"
	BgGray03 = "\033[48;5;233m"
	BgGray04 = "\033[48;5;234m"
	BgGray05 = "\033[48;5;235m"
	BgGray06 = "\033[48;5;236m"
	BgGray07 = "\033[48;5;237m"
	BgGray08 = "\033[48;5;238m"
	BgGray09 = "\033[48;5;239m"
	BgGray10 = "\033[48;5;240m"
	BgGray11 = "\033[48;5;241m"
	BgGray12 = "\033[48;5;242m"
	BgGray13 = "\033[48;5;243m"

	// Background colors - normal intensity
	BgBlack = "\033[40m"
	BgRed   = "\033[41m"
	BgGreen = "\033[42m"
	// BgDarkGreen = "\033[22;48;5;28m" // white
	BgDarkGreen = "\033[1;37;48;5;28m"  // white bold
	BgDarkBlue  = "\033[1;37;48;5;20m"  // white bold
	BgDarkRed   = "\033[1;37;48;5;160m" // white bold
	// BgDarkGreen = "\033[30;48;5;40m" // dark green
	BgYellow  = "\033[43m"
	BgBlue    = "\033[44m"
	BgMagenta = "\033[45m"
	BgCyan    = "\033[46m"
	BgWhite   = "\033[47m"

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
	Bold      = "\033[1m"
	Dim       = "\033[2m"
	Italic    = "\033[3m"
	Underline = "\033[4m"
	Blink     = "\033[5m"
	Reverse   = "\033[7m"
	Hidden    = "\033[8m"
	Strike    = "\033[9m"

	// Cursor control
	CursorOn  = "\033[?25h"
	CursorOff = "\033[?25l"

	// Common symbols
	SymAsterisk  = "✱"
	SymDotTri    = "⛬"
	SymInfo      = "ℹ"
	SymCheckMark = "✓"
	SymCrossMark = "✗"
	SymSpecial   = "⬗"

	SymEmDash   = "—"
	SymEnDash   = "–"
	SymDash     = "-"
	SymEllipsis = "…"

	SymArrowUp         = "↑"
	SymArrowDown       = "↓"
	SymArrowLeft       = "←"
	SymArrowRight      = "→"
	SymPointerRight    = "➤"
	SymArrowRightThick = "➜"
	SymDoubleLeft      = "«"
	SymDoubleRight     = "»"
	SymRightCarrot     = "❯" // SymRightCarrot = "ᐳ"
	SymGreaterThan     = ">"

	SymMultiply = "×"
	SymInfinity = "∞"
	SymDegree   = "°"
	SymApprox   = "≈"
	SymPercent  = "%"
	// 233E
	SymEnabled    = "⛛"
	SymBolt       = "⌁"
	SymFlag       = "⚑"
	SymFlagStripe = "⛿"
	SymWarning    = "⭘"
	SymStatus     = "▣"

	// SymEnabled    = "⦿" // 233E
	// SymWarning    = "⟁"

	// Additional constants for layout
	Indent        = "    "
	BulletItem    = Bold + Dim + SymDash + Reset + " "
	BulletArrow   = Bold + Dim + SymRightCarrot + Reset + " "
	BulletSpecial = Bold + SymSpecial + Reset + " "
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

// Apply dim style with an optional color
func DarkBorder(text string, color ...string) string {
	if len(color) > 0 {
		return Gray04 + color[0] + text + Reset
	}
	return Gray04 + text + Reset
}

func Striked(text string, color ...string) string {
	if len(color) > 0 {
		return Strike + color[0] + text + Reset
	}
	return Strike + text + Reset
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
	if !UseColors {
		return text
	}
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

// PadRight adds spaces to the right of text to reach the specified width
// Uses StripAnsi to correctly calculate visible text length for styled text
func PadRight(text string, width int) string {
	// Get the visible length by removing ANSI escape sequences
	visibleLen := len(StripAnsi(text))

	if visibleLen >= width {
		return text
	}

	// Calculate the correct amount of padding based on visible length
	padding := width - visibleLen

	return text + strings.Repeat(" ", padding)
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
	} else {
		descripitionStyle = Dimmed(description)
	}

	return symbol + symbolPadding + label + padding + status + " " + descripitionStyle
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

func (sf *StatusFormatter) FormatBullet(label string, status string, description string, opts ...string) string {
	return sf.FormatLine(BulletItem, Dim, label, status, Dim, description, opts...)
}

// success status
func (sf *StatusFormatter) FormatSuccess(label string, status string, description string, opts ...string) string {
	return sf.FormatLine(SymEnabled, Green, label, status, Green, description, opts...)
}

// Format Configured
func (sf *StatusFormatter) FormatConfigured(label string, status string, description string, opts ...string) string {
	symbol := Dimmed(SymEnabled)
	return sf.FormatLine(symbol, Green, label, status, Green, description, opts...)
}

func (sf *StatusFormatter) FormatCheck(label string, status string, description string, opts ...string) string {
	return sf.FormatEmLine(SymCheckMark, label, status, BrightRed, description, "bold")
}

// error status
func (sf *StatusFormatter) FormatError(label string, status string, description string, opts ...string) string {
	return sf.FormatLine(SymCrossMark, BrightRed, label, status, BrightRed, description, opts...)
}

// warning status
func (sf *StatusFormatter) FormatWarning(label string, status string, description string, opts ...string) string {
	return sf.FormatLine(SymWarning, Red, label, status, Red, description, opts...)
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

// IndentText adds a specific number of spaces at the start of a string
func IndentText(text string, spaces int) string {
	if spaces <= 0 {
		return text
	}
	return strings.Repeat(" ", spaces) + text
}

// IndentPrinter wraps a print function to add indentation to each line
func IndentPrinter(printFn func(string), spaces int) func(string) {
	if spaces <= 0 {
		return printFn
	}

	indentPrefix := strings.Repeat(" ", spaces)
	return func(line string) {
		printFn(indentPrefix + line)
	}
}

// NewMenu creates a new menu with the given title and options
func NewMenu(title string, options []MenuOption) *Menu {
	// Calculate maximum number length and title width
	maxNumLen := 1   // At least 1 digit
	titleWidth := 15 // Minimum width

	for _, opt := range options {
		// Track the longest option number
		numLen := len(fmt.Sprintf("%d", opt.Number))
		if numLen > maxNumLen {
			maxNumLen = numLen
		}

		// Track the longest title
		titleLen := len(StripAnsi(opt.Title))
		if titleLen > titleWidth {
			titleWidth = titleLen
		}
	}

	// This creates uniform spacing for all menu items
	const fixedBuffer = 5
	titleWidth += fixedBuffer

	return &Menu{
		title:      title,
		options:    options,
		prompt:     "Enter your choice",
		maxNumLen:  maxNumLen,
		titleWidth: titleWidth,
	}
}

// SetExitOption sets a custom exit option (default is 0: Exit)
func (m *Menu) SetExitOption(option MenuOption) {
	m.exitOption = &option

	// Update maxNumLen if necessary
	numLen := len(fmt.Sprintf("%d", option.Number))
	if numLen > m.maxNumLen {
		m.maxNumLen = numLen
	}

	// Update titleWidth if necessary
	titleLen := len(StripAnsi(option.Title))
	if titleLen > m.titleWidth {
		m.titleWidth = titleLen
	}
}

// SetPrompt sets a custom prompt
func (m *Menu) SetPrompt(prompt string) {
	m.prompt = prompt
}

// SetDryRunStatus sets whether to show dry run status and whether it's enabled
func (m *Menu) SetDryRunStatus(show bool, enabled bool) {
	m.showDryRunInfo = show
	m.dryRunEnabled = enabled
}

// SetIndentation sets the indentation for menu options
func (m *Menu) SetIndentation(spaces int) {
	m.indentation = strings.Repeat(" ", spaces)
}

// GetValidRange returns the valid range of option numbers as a string
func (m *Menu) GetValidRange() string {
	if len(m.options) == 0 {
		return "0"
	}

	min := m.options[0].Number
	max := m.options[0].Number

	for _, opt := range m.options {
		if opt.Number < min {
			min = opt.Number
		}
		if opt.Number > max {
			max = opt.Number
		}
	}

	// Include exit option in the range
	exitNum := 0
	if m.exitOption != nil {
		exitNum = m.exitOption.Number
	}

	if exitNum < min {
		min = exitNum
	}

	if exitNum > max {
		max = exitNum
	}

	if min == max {
		return fmt.Sprintf("%d", min)
	}

	return fmt.Sprintf("%d-%d", min, max)
}

// FormatOption formats a single menu option
func (m *Menu) FormatOption(opt MenuOption) string {
	// Format number with consistent padding
	numStr := fmt.Sprintf("%d)", opt.Number)

	// Add extra space for single-digit numbers to align with double-digit numbers
	if opt.Number < 10 {
		numStr = " " + numStr
	}

	numPadded := Bolded(numStr)

	// Add spacing after the number
	numPadded += " "

	titlePadded := ""
	// Format title with consistent padding
	if opt.Style == "" {
		// opt.Title = Colored(opt.Style, opt.Title)
		titlePadded += PadRight(opt.Title, m.titleWidth)
		// titlePadded += PadRight(opt.Title, m.titleWidth + 4)
	} else if opt.Style == "strike" {
		// opt.Title = Bolded(opt.Title)
		strikeTitle := Striked(opt.Title)
		dimmedStrikeTitle := Dimmed(strikeTitle)
		titlePadded += PadRight(dimmedStrikeTitle, m.titleWidth)
		// titlePadded += PadRight(dimmedStrikeTitle, m.titleWidth + 4)
	}

	// Add description
	desc := Dimmed(opt.Description)

	// Apply indentation if set
	if m.indentation != "" {
		return m.indentation + numPadded + titlePadded + desc
	}

	return numPadded + titlePadded + desc
}

// Render returns the formatted menu as a string
func (m *Menu) Render() string {
	var sb strings.Builder

	sb.WriteString("\n")

	// Format the title with dry-run status if needed
	if m.showDryRunInfo {
		dryRunSymbol := "" // SymAsterisk
		dryRunLabel := "Dry-run Mode"
		dryRunStatus := "Disabled"
		dryRunColorFn := Yellow

		if m.dryRunEnabled {
			dryRunStatus = "Enabled"
			dryRunColorFn = Green
		}

		spacing := 8

		// Add indentation to the title if set
		titlePrefix := ""
		if m.indentation != "" {
			titlePrefix = m.indentation
		}

		titleText := Underline + Bold + m.title + Reset

		// Create formatter for dry run status
		formatter := NewStatusFormatter([]string{dryRunLabel}, 2)

		dryRunLabel = Dimmed(dryRunLabel)

		// Use FormatLine to format the dry run status
		dryRunInfo := formatter.FormatLine(dryRunSymbol, dryRunColorFn, dryRunLabel, dryRunStatus, dryRunColorFn, "", "no-indent", "no-spacing")

		// Write all on one line with appropriate indentation
		sb.WriteString(titlePrefix + titleText + strings.Repeat(" ", spacing) + dryRunInfo + "\n")
	} else {
		// Add indentation to the title if set
		if m.indentation != "" {
			sb.WriteString(m.indentation)
		}
		// Just write the title without dry-run info, with indentation if set
		sb.WriteString(SubHeader(m.title))
	}

	// Options
	for _, opt := range m.options {
		sb.WriteString("\n")
		sb.WriteString(m.FormatOption(opt))
	}

	// Exit option
	sb.WriteString("\n\n")
	if m.exitOption != nil {
		sb.WriteString(m.FormatOption(*m.exitOption))
	} else {
		// Default exit option
		exit := MenuOption{
			Number:      0,
			Title:       "Exit",
			Description: "Tip: Press 'q' to exit immediately",
		}
		sb.WriteString(m.FormatOption(exit))
	}

	instruct := Bold + m.prompt + Reset

	// Prompt
	sb.WriteString("\n\n\n")

	// Apply the same indentation to the prompt as we do for menu items
	if m.indentation != "" {
		sb.WriteString(m.indentation)
	}

	sb.WriteString(fmt.Sprintf("%s %s [%s or q]: ", Dimmed(SymRightCarrot), instruct, m.GetValidRange()))

	return sb.String()
}

// Print displays the menu on stdout
func (m *Menu) Print() {
	fmt.Print(m.Render())
}
