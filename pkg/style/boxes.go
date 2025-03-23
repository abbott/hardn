package style

import (
	"fmt"
	"strings"

	"github.com/mattn/go-runewidth"
)

type BoxConfig struct {
	Width               int
	BorderColor         string
	ShadeColor          string
	ShowEmptyRow        bool
	ShowTopBorder       bool
	ShowTopBlock        bool
	ShowTopShade        bool
	ShowLeftBorder      bool
	ShowBottomBorder    bool
	ShowRightBorder     bool
	ShowBottomPadding   bool
	ShowBottomSeparator bool
	Indentation         int
	Title               string
	TitleColor          string
}

// Box methods
type Box struct {
	width         int
	borderColor   string
	shadeColor    string
	showEmptyRow  bool
	showTopBorder bool
	// showTopBlock bool
	showTopShade        bool
	showLeftBorder      bool
	showBottomBorder    bool
	showRightBorder     bool
	showBottomPadding   bool
	showBottomSeparator bool
	indentation         int
	title               string
	titleColor          string

	// Unicode box characters
	horizontal  string
	vertical    string
	topLeft     string
	topRight    string
	bottomLeft  string
	bottomRight string

	// ASCII box characters
	asciiHorizontal  string
	asciiVertical    string
	asciiTopLeft     string
	asciiTopRight    string
	asciiBottomLeft  string
	asciiBottomRight string

	block      string
	shade      string
	asciiBlock string

	space          string
	emptyLineCache string
}

// NewBox with default settings
func NewBox(config BoxConfig) *Box {
	box := &Box{
		width:               config.Width,
		borderColor:         config.BorderColor,
		shadeColor:          config.ShadeColor,
		showEmptyRow:        config.ShowEmptyRow,
		showTopBorder:       config.ShowTopBorder,
		showTopShade:        config.ShowTopShade,
		showLeftBorder:      config.ShowLeftBorder,
		showBottomBorder:    config.ShowBottomBorder,
		showRightBorder:     config.ShowRightBorder,
		showBottomPadding:   config.ShowBottomPadding,
		showBottomSeparator: config.ShowBottomSeparator,
		indentation:         config.Indentation,
		title:               config.Title,
		titleColor:          config.TitleColor,

		// Unicode box characters (rounded corners)
		horizontal:  "─", // U+2500 Box Drawings Light Horizontal
		vertical:    "│", // U+2502 Box Drawings Light Vertical
		topLeft:     "╭", // U+256D Box Drawings Light Arc Down and Right
		topRight:    "╮", // U+256E Box Drawings Light Arc Down and Left
		bottomLeft:  "╰", // U+256F Box Drawings Light Arc Up and Right
		bottomRight: "╯", // U+2570 Box Drawings Light Arc Up and Left

		// ASCII box characters
		asciiHorizontal:  "-",
		asciiVertical:    "|",
		asciiTopLeft:     "+",
		asciiTopRight:    "+",
		asciiBottomLeft:  "+",
		asciiBottomRight: "+",

		block:      "█",
		shade:      "░", // U+2591 Light Shade
		asciiBlock: "#",

		space: " ", // U+0020 Space
	}

	// Set defaults for zero values
	if box.width == 0 {
		box.width = 64
	}

	if box.borderColor == "" {
		box.borderColor = Gray04
	}

	if box.shadeColor == "" {
		box.shadeColor = Gray08
	}

	// Use border color if title color not specified
	if box.titleColor == "" {
		box.titleColor = Gray15
	}

	// Only override the defaults if explicitly set to false
	if !config.ShowTopBorder {
		box.showTopBorder = false
	}

	if !config.ShowTopShade {
		box.showTopShade = true
	}

	if !config.ShowLeftBorder {
		box.showLeftBorder = false
	}

	if !config.ShowBottomBorder {
		box.showBottomBorder = false
	}

	if !config.ShowBottomSeparator {
		box.showBottomSeparator = false
	}

	if !config.ShowRightBorder {
		box.showRightBorder = false
	}

	if !config.ShowBottomPadding {
		box.showBottomPadding = false
	}

	// Initialize emptyLineCache
	box.updateEmptyLineCache()

	return box
}

// update the cached empty line string based on current settings
func (b *Box) updateEmptyLineCache() {
	b.emptyLineCache = ""

	// Choose the appropriate vertical character based on UseColors
	vertChar := b.vertical
	if !UseColors {
		vertChar = b.asciiVertical
	}

	if b.showLeftBorder {
		b.emptyLineCache += (Dimmed(vertChar, b.borderColor))
	} else if b.indentation > 0 {
		// Add indentation if left border is hidden
		b.emptyLineCache += strings.Repeat(b.space, b.indentation)
	}

	b.emptyLineCache += strings.Repeat(b.space, b.width)

	if b.showRightBorder {
		b.emptyLineCache += (Dimmed(vertChar, b.borderColor))
	}
}

// draw an empty row
func (b *Box) DrawEmpty() {
	fmt.Println(b.emptyLineCache)
}

// DrawTop draws the top border of the box
func (b *Box) DrawTop() {
	// Choose the appropriate characters based on UseColors
	horizChar := b.horizontal
	topLeftChar := b.topLeft
	topRightChar := b.topRight

	if !UseColors {
		horizChar = b.asciiHorizontal
		topLeftChar = b.asciiTopLeft
		topRightChar = b.asciiTopRight
	}

	// If we have a title, draw the title with borders on each side
	if b.title != "" && b.showTopBorder {
		topBorder := ""

		// Add indentation if there's no left border but indentation is set
		if !b.showLeftBorder && b.indentation > 0 {
			topBorder = Dimmed(strings.Repeat(b.space, b.indentation), b.borderColor)
		}

		// Only draw left corner if showing left border
		if b.showLeftBorder {
			topBorder += topLeftChar
		}

		// Calculate space needed before and after title
		titleLen := CalculateVisualWidth(b.title)
		beforeTitle := 0 // minimum spacing before title

		// Generate the border with title
		rightSide := Dimmed(strings.Repeat(horizChar, b.width-beforeTitle-titleLen-1)+topRightChar, b.borderColor)

		line := ""
		topBorder += strings.Repeat(horizChar, beforeTitle)
		line += (Dimmed(topBorder, b.borderColor))

		BoldedTitle := Bolded(b.title)

		if UseColors {
			line += "" + Dimmed(BoldedTitle, b.titleColor) + " " + rightSide
		} else {
			line += "" + b.title + " " + rightSide
		}

		fmt.Println(line)
		return
	}

	// Otherwise draw a regular top border
	topBorder := ""

	// Add indentation if there's no left border but indentation is set
	if !b.showLeftBorder && b.indentation > 0 {
		topBorder = strings.Repeat(b.space, b.indentation)
	}

	// Only draw left corner if showing left border
	if b.showLeftBorder {
		topBorder += topLeftChar
	}

	topBorder += strings.Repeat(horizChar, b.width) + topRightChar

	fmt.Println(Dimmed(topBorder, b.borderColor))
}

func (b *Box) DrawTopHeader() {
	// Choose the appropriate characters based on UseColors
	headerChar := b.shade
	// shadeColor := Gray08

	if !UseColors {
		headerChar = b.asciiBlock
	}

	// If we have a title, draw the title with borders on each side
	if b.title != "" && b.showTopShade {
		leftSide := ""

		// Calculate space needed before and after title
		titleLen := CalculateVisualWidth(b.title)
		beforeTitle := 1 // minimum spacing before title

		line := ""
		leftSide += strings.Repeat(headerChar, beforeTitle)
		line += (Colored(b.shadeColor, leftSide))

		rightSide := Colored(b.shadeColor, strings.Repeat(headerChar, b.width-beforeTitle-titleLen-1))

		BoldedTitle := Bolded(b.title)

		if UseColors && b.titleColor != "skip" {
			line += " " + Dimmed(BoldedTitle, b.titleColor) + " " + rightSide
		} else {
			line += " " + b.title + " " + rightSide
		}

		fmt.Println(line)
		// fmt.Println()

		return
	}

	// Otherwise draw a regular top border
	line := ""

	// Add indentation if there's no left border but indentation is set
	if !b.showLeftBorder && b.indentation > 0 {
		line = strings.Repeat(b.space, b.indentation)
	}

	line += strings.Repeat(headerChar, b.width)

	fmt.Println(line)
}

func (b *Box) SectionHeader(label string, color ...string) {
	// Choose the appropriate characters based on UseColors
	headerChar := b.shade
	// horizChar := "~"
	horizChar := b.horizontal
	// shadeColor := Gray08

	labelColor := Gray15
	// msgColor := Gray08

	// if secColor == "" {
	// 	labelColor = b.titleColor
	// } else {
	// 	labelColor = secColor
	// }

	if len(color) > 0 && color[0] != "" {
		labelColor = color[0]
	}

	if !UseColors {
		headerChar = b.asciiBlock
	}

	// If we have a title, draw the title with borders on each side
	if label != "" && b.showTopShade {
		leftSide := ""

		// Calculate space needed before and after title
		labelLen := CalculateVisualWidth(label)
		beforeLabel := 1 // minimum spacing before title

		line := ""
		leftSide += strings.Repeat(headerChar, beforeLabel)
		line += (Colored(b.shadeColor, leftSide))

		rightSide := Dimmed(strings.Repeat(horizChar, b.width-beforeLabel-labelLen-1), b.borderColor)

		// boldLabel := Bolded(label)
		// b.titleColor
		dimLabel := Dimmed(label, labelColor)

		if UseColors && labelColor != "skip" {
			line += " " + dimLabel + " " + rightSide
		} else {
			line += " " + label + " " + rightSide
		}

		// if UseColors && b.titleColor != "skip" {
		// 	line += " " + Dimmed(label, labelColor) + " " + rightSide
		// } else {
		// 	line += " " + b.title + " " + rightSide
		// }

		fmt.Println(line)
		fmt.Println()

		return
	}

	// Otherwise draw a regular top border
	line := ""

	// Add indentation if there's no left border but indentation is set
	if !b.showLeftBorder && b.indentation > 0 {
		line = strings.Repeat(b.space, b.indentation)
	}

	line += strings.Repeat(headerChar, b.width)

	fmt.Println(line)
}

func (b *Box) SectionNotice(label string, message string, notice ...string) {
	// Choose the appropriate characters based on UseColors

	labelColor := ""
	secColor := ""
	for _, opt := range notice {
		switch opt {
		case "warning":
			secColor = Yellow
		case "success":
			secColor = Green
		}
	}

	headerChar := b.block
	// horizChar := "~"
	// horizChar := b.horizontal
	// shadeColor := Gray08

	// msgColor := Gray08

	if secColor != "" {
		labelColor = secColor
	} else {
		labelColor = b.titleColor
		secColor = b.shadeColor
	}

	// if len(color) > 0 && color[0] != "" {
	// 	msgColor = color[0]
	// }

	if !UseColors {
		headerChar = b.asciiBlock
	}

	// If we have a title, draw the title with borders on each side
	if label != "" && b.showTopShade {
		leftSide := ""

		// Calculate space needed before and after title
		// messageLen := CalculateVisualWidth(message)
		beforeLabel := 1 // minimum spacing before title

		labelLine := ""
		messageLine := ""
		leftSide += strings.Repeat(headerChar, beforeLabel)
		labelLine += (Colored(secColor, leftSide))
		messageLine += (Colored(secColor, leftSide))

		// rightSide := Dimmed(strings.Repeat(horizChar, b.width-beforeLabel-labelLen-1), b.borderColor)

		// BoldedLabel := Bolded(label)

		labelLine += " " + Colored(labelColor, label)
		messageLine += " " + Dimmed(message)

		// if UseColors && b.titleColor != "skip" {
		// 	labelLine += " " + Dimmed(label, labelColor) + " " + rightSide
		// } else {
		// 	labelLine += " " + b.title + " " + rightSide
		// }

		fmt.Printf("\n")
		fmt.Println(labelLine)
		if message != "" {
			fmt.Println(messageLine)
		}
		fmt.Printf("\n\n")
		// fmt.Println()

		return
	}

	// Otherwise draw a regular top border
	line := ""

	// Add indentation if there's no left border but indentation is set
	if !b.showLeftBorder && b.indentation > 0 {
		line = strings.Repeat(b.space, b.indentation)
	}

	line += strings.Repeat(headerChar, b.width)

	fmt.Println(line)
}

func (b *Box) WarningNotice(label string, message string) {
	if label == "" {
		label = "Warning"
	}
	b.SectionNotice(label, message, "warning")
}

func (b *Box) SuccessNotice(label string, message string) {
	if label == "" {
		label = "Success"
	}
	b.SectionNotice(label, message, "success")
}

// DrawBottom draws the bottom border of the box
func (b *Box) DrawBottom() {
	// Choose the appropriate characters based on UseColors
	horizChar := b.horizontal
	bottomLeftChar := b.bottomLeft
	bottomRightChar := b.bottomRight

	if !UseColors {
		horizChar = b.asciiHorizontal
		bottomLeftChar = b.asciiBottomLeft
		bottomRightChar = b.asciiBottomRight
	}

	bottomBorder := ""

	// Add indentation if there's no left border but indentation is set
	if !b.showLeftBorder && b.indentation > 0 {
		// bottomBorder = Dimmed(strings.Repeat(b.space, b.indentation), b.borderColor)
		bottomBorder = strings.Repeat(b.space, b.indentation)
	}

	// Only draw left corner if showing left border
	if b.showLeftBorder {
		bottomBorder += bottomLeftChar
	}

	bottomBorder += strings.Repeat(horizChar, b.width) + bottomRightChar
	fmt.Println(Dimmed(bottomBorder, b.borderColor))
}

func (b *Box) DrawSeparator() {

	b.DrawEmpty()
	// Choose the appropriate characters based on UseColors
	// horizChar := "~"
	horizChar := b.horizontal

	bottomBorder := ""

	// Add indentation if there's no left border but indentation is set
	if !b.showLeftBorder && b.indentation > 0 {
		// bottomBorder = Dimmed(strings.Repeat(b.space, b.indentation), b.borderColor)
		bottomBorder = strings.Repeat(b.space, b.indentation)
	}

	bottomBorder += strings.Repeat(horizChar, b.width+1)
	fmt.Println(Dimmed(bottomBorder, b.borderColor))

	b.DrawEmpty()
}

func (b *Box) DrawBottomSeparator() {

	b.DrawEmpty()
	// Choose the appropriate characters based on UseColors
	// horizChar := "~"
	horizChar := b.horizontal

	bottomBorder := "  "

	// Add indentation if there's no left border but indentation is set
	if !b.showLeftBorder && b.indentation > 0 {
		// bottomBorder = Dimmed(strings.Repeat(b.space, b.indentation), b.borderColor)
		bottomBorder = strings.Repeat(b.space, b.indentation)
	}

	bottomBorder += strings.Repeat(horizChar, b.width-1)
	fmt.Println(Dimmed(bottomBorder, b.borderColor))
}

// draw a line of content in the box
func (b *Box) DrawLine(content string) {
	// Calculate the visual width of the content using go-runewidth
	visibleLen := CalculateVisualWidth(content)

	padding := b.width - visibleLen
	if padding < 0 {
		padding = 0
	}

	// Choose the appropriate vertical character based on UseColors
	vertChar := b.vertical
	if !UseColors {
		vertChar = b.asciiVertical
	}

	line := ""
	if b.showLeftBorder {
		line += (Dimmed(vertChar, b.borderColor))
	} else if b.indentation > 0 {
		// Add indentation if left border is hidden
		line += strings.Repeat(b.space, b.indentation)
	}

	line += content + strings.Repeat(b.space, padding)
	if b.showRightBorder {
		line += (Dimmed(vertChar, b.borderColor))
	}

	fmt.Println(line)
}

// return the visual width of a string as it would appear in a terminal
// Using the go-runewidth package for accurate width calculation
func CalculateVisualWidth(s string) int {
	// First strip ANSI codes to get the displayable text
	plainText := StripAnsi(s)

	// Use go-runewidth to calculate the string width
	return runewidth.StringWidth(plainText)
}

// draw a complete box with the provided content function
func (b *Box) DrawBox(contentFn func(printLine func(string))) {

	if b.showTopBorder {
		b.DrawTop()
	}

	// if b.showTopBlock {
	// 	b.DrawTopHeader()
	// }

	if b.showTopShade {
		fmt.Println()
		b.DrawTopHeader()
	}

	// top padding
	if b.showEmptyRow {
		b.DrawEmpty()
	}

	if contentFn != nil {
		contentFn(func(line string) {
			b.DrawLine(line)
		})
	}

	if b.showBottomSeparator {
		b.DrawBottomSeparator()
		// b.DrawEmpty()
		fmt.Println()
	}

	// bottom padding
	if b.showBottomPadding {
		b.DrawEmpty()
	}

	if b.showBottomBorder {
		b.DrawBottom()
	}

	// fmt.Println()
}

// draw text centered in the box
func (b *Box) DrawCenteredText(text string) {
	// Use the same CalculateVisualWidth method for consistency with DrawLine
	visibleLen := CalculateVisualWidth(text)
	leftPadding := (b.width - visibleLen) / 2
	rightPadding := b.width - visibleLen - leftPadding

	if leftPadding < 0 {
		leftPadding = 0
	}

	if rightPadding < 0 {
		rightPadding = 0
	}

	// Choose the appropriate vertical character based on UseColors
	vertChar := b.vertical
	if !UseColors {
		vertChar = b.asciiVertical
	}

	line := ""
	if b.showLeftBorder {
		line += (Dimmed(vertChar, b.borderColor))
	} else if b.indentation > 0 {
		// Add indentation if left border is hidden
		line += strings.Repeat(b.space, b.indentation)
	}

	line += strings.Repeat(b.space, leftPadding) + text + strings.Repeat(b.space, rightPadding)

	if b.showRightBorder {
		line += (Dimmed(vertChar, b.borderColor))
	}

	fmt.Println(line)
}

// draw text, truncating if it exceeds the box width
func (b *Box) DrawTruncatedText(text string, truncateIndicator string) {
	if truncateIndicator == "" {
		truncateIndicator = "..."
	}

	// If text already fits, just draw it normally
	visibleLen := CalculateVisualWidth(text)
	if visibleLen <= b.width {
		b.DrawLine(text)
		return
	}

	// Use go-runewidth's built-in truncation function
	plainText := StripAnsi(text)
	truncatedText := runewidth.Truncate(plainText, b.width-len(truncateIndicator), truncateIndicator)

	// Simplified handling for ANSI codes
	// we would need to preserve the ANSI codes from the original text
	b.DrawLine(truncatedText)
}

// draw text aligned to the right side of the box
func (b *Box) DrawRightAlignedText(text string) {

	visibleLen := CalculateVisualWidth(text)
	padding := b.width - visibleLen

	if padding < 0 {
		padding = 0
	}

	// Choose the appropriate vertical character based on UseColors
	vertChar := b.vertical
	if !UseColors {
		vertChar = b.asciiVertical
	}

	line := ""
	if b.showLeftBorder {
		line += (Dimmed(vertChar, b.borderColor))
	} else if b.indentation > 0 {
		// Add indentation if left border is hidden
		line += strings.Repeat(b.space, b.indentation)
	}

	line += strings.Repeat(b.space, padding) + text

	if b.showRightBorder {
		line += (Dimmed(vertChar, b.borderColor))
	}

	fmt.Println(line)
}

// draw text with specified left padding
func (b *Box) DrawPaddedText(text string, leftPadding int) {
	visibleLen := CalculateVisualWidth(text)
	rightPadding := b.width - visibleLen - leftPadding

	if leftPadding < 0 {
		leftPadding = 0
	}

	if rightPadding < 0 {
		rightPadding = 0
	}

	// Choose the appropriate vertical character based on UseColors
	vertChar := b.vertical
	if !UseColors {
		vertChar = b.asciiVertical
	}

	line := ""
	if b.showLeftBorder {
		line += (Dimmed(vertChar, b.borderColor))
	} else if b.indentation > 0 {
		// Add indentation if left border is hidden
		line += strings.Repeat(b.space, b.indentation)
	}

	line += strings.Repeat(b.space, leftPadding) + text + strings.Repeat(b.space, rightPadding)

	if b.showRightBorder {
		line += (Dimmed(vertChar, b.borderColor))
	}

	fmt.Println(line)
}

// SectionDivider creates a section divider with a title, matching the box style
func SectionDivider(title string, width int, color ...string) string {
	// Set default color if not provided
	dividerColor := Gray04
	if len(color) > 0 && color[0] != "" {
		dividerColor = color[0]
	}

	// Choose the appropriate horizontal character based on UseColors
	horizChar := "─" // U+2500 Box Drawings Light Horizontal
	if !UseColors {
		horizChar = "-"
	}

	// Calculate space needed before and after title
	titleLen := CalculateVisualWidth(title)
	fullWidth := width
	targetWidth := (fullWidth - titleLen - 2) // -2 for spacing around title

	// Ensure we have at least some divider on each side
	if targetWidth < 4 {
		targetWidth = 4
	}

	leftWidth := targetWidth / 2
	rightWidth := targetWidth - leftWidth

	// Build the divider
	leftSide := strings.Repeat(horizChar, leftWidth)
	rightSide := strings.Repeat(horizChar, rightWidth)

	divider := ""

	// Format with color if enabled
	if UseColors {
		divider = Dimmed(leftSide, dividerColor) + " " + Colored(dividerColor, title) + " " + Dimmed(rightSide, dividerColor)
	} else {
		divider = leftSide + " " + title + " " + rightSide
	}

	return divider
}

// BoxedTitle creates a boxed title with the specified text, matching the box style
func BoxedTitle(title string, width int, color ...string) string {
	// Set default color if not provided
	boxColor := Gray04
	if len(color) > 0 && color[0] != "" {
		boxColor = color[0]
	}

	// Choose the appropriate characters based on UseColors
	horizChar := "─"       // U+2500 Box Drawings Light Horizontal
	vertChar := "│"        // U+2502 Box Drawings Light Vertical
	topLeftChar := "╭"     // U+256D Box Drawings Light Arc Down and Right
	topRightChar := "╮"    // U+256E Box Drawings Light Arc Down and Left
	bottomLeftChar := "╰"  // U+256F Box Drawings Light Arc Up and Right
	bottomRightChar := "╯" // U+2570 Box Drawings Light Arc Up and Left

	if !UseColors {
		horizChar = "-"
		vertChar = "|"
		topLeftChar = "+"
		topRightChar = "+"
		bottomLeftChar = "+"
		bottomRightChar = "+"
	}

	// Calculate space needed for title
	titleLen := CalculateVisualWidth(title)

	// Ensure the box has enough space for the title with padding
	boxWidth := width
	innerWidth := boxWidth - 2 // -2 for the left and right borders
	titlePadding := (innerWidth - titleLen) / 2

	if titlePadding < 1 {
		titlePadding = 1
	}

	// Build the box
	topBorder := topLeftChar + strings.Repeat(horizChar, innerWidth) + topRightChar
	emptyLine := vertChar + strings.Repeat(" ", innerWidth) + vertChar
	titleLine := vertChar + strings.Repeat(" ", titlePadding) + title
	titleLine += strings.Repeat(" ", innerWidth-titlePadding-titleLen) + vertChar
	bottomBorder := bottomLeftChar + strings.Repeat(horizChar, innerWidth) + bottomRightChar

	result := ""

	// Apply colors if enabled
	if UseColors {
		topBorder = Dimmed(topBorder, boxColor)
		emptyLine = Dimmed(vertChar, boxColor) + strings.Repeat(" ", innerWidth) + Dimmed(vertChar, boxColor)
		titleLine = Dimmed(vertChar, boxColor) + strings.Repeat(" ", titlePadding) + Colored(boxColor, title)
		titleLine += strings.Repeat(" ", innerWidth-titlePadding-titleLen) + Dimmed(vertChar, boxColor)
		bottomBorder = Dimmed(bottomBorder, boxColor)
	}

	// Combine all lines
	result = topBorder + "\n" + emptyLine + "\n" + titleLine + "\n" + emptyLine + "\n" + bottomBorder

	return result
}

func ScreenHeader(title string, width int, options ...string) string {

	// Set default color and border character
	borderColor := Gray08 // Default border color // Gray07

	// Default border character based on terminal capabilities
	borderCharacter := "░" // Unicode block // "░"  // "█"
	if !UseColors {
		borderCharacter = "#" // ASCII fallback
	}

	if len(options) > 0 {
		// Check if the last option is a three characters
		if len(options[len(options)-1]) == 3 {
			borderCharacter = options[len(options)-1]
			options = options[:len(options)-1] // Remove the last option
		}

		if len(options) > 0 {
			borderColor = options[0]
		}
	}

	// Calculate space needed for title
	titleLen := CalculateVisualWidth(title)
	beforeTitle := 1 // minimum spacing before title

	// Format header elements
	leftSide := Colored(borderColor, strings.Repeat(borderCharacter, beforeTitle))
	rightSide := Colored(borderColor, strings.Repeat(borderCharacter, width-beforeTitle-titleLen-1)+borderCharacter)

	// leftSide := Dimmed(borderCharacter, borderColor)
	// rightSide := Dimmed(strings.Repeat(borderCharacter, width-beforeTitle-titleLen-1)+borderCharacter, borderColor)

	// Build header
	header := leftSide + " " + title + " " + rightSide

	return header
	// return header + "\n"
}
