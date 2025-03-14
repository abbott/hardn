// pkg/style/boxes.go
package style

import (
	"fmt"
	"strings"

	"github.com/mattn/go-runewidth"
)

// BoxConfig holds configuration for drawing a box
type BoxConfig struct {
	Width          int    // Width of the box content area
	BorderColor    string // Color for the border
	ShowEmptyRow   bool   // Whether to show empty rows between content sections
	ShowTopBorder  bool   // Whether to show the top border of the box
	ShowLeftBorder bool   // Whether to show the left border of the box
	Indentation    int    // Number of spaces to indent content when left border is hidden
	Title          string // Title to display over the top border
	TitleColor     string // Color for the title (default is BorderColor)
}

// Box provides methods for drawing boxes with borders
type Box struct {
	width          int
	borderColor    string
	showEmptyRow   bool
	showTopBorder  bool
	showLeftBorder bool
	indentation    int
	title          string
	titleColor     string
	horizontal     string
	vertical       string
	topLeft        string
	topRight       string
	bottomLeft     string
	bottomRight    string
	space          string
	emptyLineCache string
}

// NewBox creates a new Box with default settings
func NewBox(config BoxConfig) *Box {
	box := &Box{
		width:          config.Width,
		borderColor:    config.BorderColor,
		showEmptyRow:   config.ShowEmptyRow,
		showTopBorder:  true, // Default to true
		showLeftBorder: true, // Default to true
		indentation:    config.Indentation,
		title:          config.Title,
		titleColor:     config.TitleColor,
		horizontal:     "─", // U+2500 Box Drawings Light Horizontal
		vertical:       "│", // U+2502 Box Drawings Light Vertical
		topLeft:        "╭", // U+256D Box Drawings Light Arc Down and Right
		topRight:       "╮", // U+256E Box Drawings Light Arc Down and Left
		bottomLeft:     "╰", // U+256F Box Drawings Light Arc Up and Right
		bottomRight:    "╯", // U+2570 Box Drawings Light Arc Up and Left
		space:          " ", // U+0020 Space
	}

	// Set defaults for zero values
	if box.width == 0 {
		box.width = 64
	}

	if box.borderColor == "" {
		box.borderColor = Gray04
	}

	// Use border color if title color not specified
	if box.titleColor == "" {
		box.titleColor = box.borderColor
	}

	// Only override the defaults if explicitly set to false
	if !config.ShowTopBorder {
		box.showTopBorder = false
	}

	if !config.ShowLeftBorder {
		box.showLeftBorder = false
	}

	// Pre-compute the empty line for efficiency - respect left border setting
	box.emptyLineCache = ""
	if box.showLeftBorder {
		box.emptyLineCache += Colored(box.borderColor, box.vertical)
	} else if box.indentation > 0 {
		// Add indentation if left border is hidden
		box.emptyLineCache += strings.Repeat(box.space, box.indentation)
	}
	box.emptyLineCache += strings.Repeat(box.space, box.width) + Colored(box.borderColor, box.vertical)

	return box
}

// DrawTop draws the top border of the box
func (b *Box) DrawTop() {
	// If we have a title, draw the title with borders on each side
	if b.title != "" && b.showTopBorder {
		topBorder := ""

		// Add indentation if there's no left border but indentation is set
		if !b.showLeftBorder && b.indentation > 0 {
			topBorder = strings.Repeat(b.space, b.indentation)
		}

		// Only draw left corner if showing left border
		if b.showLeftBorder {
			topBorder += b.topLeft
		}

		// Calculate space needed before and after title
		titleLen := CalculateVisualWidth(b.title)
		beforeTitle := 0 // minimum spacing before title

		// Generate the border with title
		rightSide := Colored(b.borderColor, strings.Repeat(b.horizontal, b.width-beforeTitle-titleLen-1)+b.topRight)

		topBorder += strings.Repeat(b.horizontal, beforeTitle) +
			"" + Colored(b.titleColor, b.title) + " " + rightSide

		fmt.Println(topBorder)

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
		topBorder += b.topLeft
	}

	topBorder += strings.Repeat(b.horizontal, b.width) + b.topRight
	fmt.Println(Colored(b.borderColor, topBorder))
}

// DrawBottom draws the bottom border of the box
func (b *Box) DrawBottom() {
	bottomBorder := ""

	// Add indentation if there's no left border but indentation is set
	if !b.showLeftBorder && b.indentation > 0 {
		bottomBorder = strings.Repeat(b.space, b.indentation)
	}

	// Only draw left corner if showing left border
	if b.showLeftBorder {
		bottomBorder += b.bottomLeft
	}

	bottomBorder += strings.Repeat(b.horizontal, b.width) + b.bottomRight
	fmt.Println(Colored(b.borderColor, bottomBorder))
}

// DrawEmpty draws an empty row in the box
func (b *Box) DrawEmpty() {
	fmt.Println(b.emptyLineCache)
}

// DrawLine draws a line of content in the box
func (b *Box) DrawLine(content string) {
	// Calculate the visual width of the content using go-runewidth
	visibleLen := CalculateVisualWidth(content)

	padding := b.width - visibleLen
	if padding < 0 {
		padding = 0
	}

	line := ""
	if b.showLeftBorder {
		line += Colored(b.borderColor, b.vertical)
	} else if b.indentation > 0 {
		// Add indentation if left border is hidden
		line += strings.Repeat(b.space, b.indentation)
	}

	line += content + strings.Repeat(b.space, padding) + Colored(b.borderColor, b.vertical)
	fmt.Println(line)
}

// CalculateVisualWidth returns the visual width of a string as it would appear in a terminal
// Using the go-runewidth package for accurate width calculation
func CalculateVisualWidth(s string) int {
	// First strip ANSI codes to get the displayable text
	plainText := StripAnsi(s)

	// Use go-runewidth to calculate the string width
	return runewidth.StringWidth(plainText)
}

// DrawBox draws a complete box with the provided content function
func (b *Box) DrawBox(contentFn func(printLine func(string))) {
	if b.showTopBorder {
		b.DrawTop()
	}

	if b.showEmptyRow {
		b.DrawEmpty()
	}

	if contentFn != nil {
		contentFn(func(line string) {
			b.DrawLine(line)
		})
	}

	if b.showEmptyRow {
		b.DrawEmpty()
	}

	b.DrawBottom()
}

// DrawCenteredText draws text centered in the box
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

	line := ""
	if b.showLeftBorder {
		line += Colored(b.borderColor, b.vertical)
	} else if b.indentation > 0 {
		// Add indentation if left border is hidden
		line += strings.Repeat(b.space, b.indentation)
	}

	line += strings.Repeat(b.space, leftPadding) + text + strings.Repeat(b.space, rightPadding) + Colored(b.borderColor, b.vertical)
	fmt.Println(line)
}

// DrawTruncatedText draws text, truncating if it exceeds the box width
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

	// Simplified handling for ANSI codes - in a more complete solution,
	// we would need to preserve the ANSI codes from the original text
	b.DrawLine(truncatedText)
}

// DrawRightAlignedText draws text aligned to the right side of the box
func (b *Box) DrawRightAlignedText(text string) {
	visibleLen := CalculateVisualWidth(text)
	padding := b.width - visibleLen

	if padding < 0 {
		padding = 0
	}

	line := ""
	if b.showLeftBorder {
		line += Colored(b.borderColor, b.vertical)
	} else if b.indentation > 0 {
		// Add indentation if left border is hidden
		line += strings.Repeat(b.space, b.indentation)
	}

	line += strings.Repeat(b.space, padding) + text + Colored(b.borderColor, b.vertical)
	fmt.Println(line)
}

// DrawPaddedText draws text with specified left padding
func (b *Box) DrawPaddedText(text string, leftPadding int) {
	visibleLen := CalculateVisualWidth(text)
	rightPadding := b.width - visibleLen - leftPadding

	if leftPadding < 0 {
		leftPadding = 0
	}

	if rightPadding < 0 {
		rightPadding = 0
	}

	line := ""
	if b.showLeftBorder {
		line += Colored(b.borderColor, b.vertical)
	} else if b.indentation > 0 {
		// Add indentation if left border is hidden
		line += strings.Repeat(b.space, b.indentation)
	}

	line += strings.Repeat(b.space, leftPadding) + text + strings.Repeat(b.space, rightPadding) + Colored(b.borderColor, b.vertical)
	fmt.Println(line)
}
