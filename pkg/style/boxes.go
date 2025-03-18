package style

import (
	"fmt"
	"strings"

	"github.com/mattn/go-runewidth"
)

type BoxConfig struct {
	Width          int
	BorderColor    string
	ShowEmptyRow   bool
	ShowTopBorder  bool
	ShowLeftBorder bool
	Indentation    int
	Title          string
	TitleColor     string
}

// Box methods
type Box struct {
	width          int
	borderColor    string
	showEmptyRow   bool
	showTopBorder  bool
	showLeftBorder bool
	indentation    int
	title          string
	titleColor     string

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

	space          string
	emptyLineCache string
}

// NewBox with default settings
func NewBox(config BoxConfig) *Box {
	box := &Box{
		width:          config.Width,
		borderColor:    config.BorderColor,
		showEmptyRow:   config.ShowEmptyRow,
		showTopBorder:  true,
		showLeftBorder: true,
		indentation:    config.Indentation,
		title:          config.Title,
		titleColor:     config.TitleColor,

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

		space: " ", // U+0020 Space
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
		// if UseColors {
		// 	b.emptyLineCache += (Dimmed(vertChar, b.borderColor))
		// 	// b.emptyLineCache += Colored(b.borderColor, vertChar)
		// } else {
		// 	b.emptyLineCache += vertChar
		// }
	} else if b.indentation > 0 {
		// Add indentation if left border is hidden
		b.emptyLineCache += strings.Repeat(b.space, b.indentation)
	}

	b.emptyLineCache += strings.Repeat(b.space, b.width)

	b.emptyLineCache += (Dimmed(vertChar, b.borderColor))
	// if UseColors {

	// 	b.emptyLineCache += (Dimmed(vertChar, b.borderColor))
	// 	// b.emptyLineCache += Colored(b.borderColor, vertChar)
	// } else {
	// 	b.emptyLineCache += vertChar
	// }
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
		// if UseColors {
		// 	// rightSide = Colored(b.borderColor, strings.Repeat(horizChar, b.width-beforeTitle-titleLen-1)+topRightChar)
		// 	rightSide = Dimmed(strings.Repeat(horizChar, b.width-beforeTitle-titleLen-1)+topRightChar, b.borderColor)
		// } else {
		// 	rightSide = Dimmed(strings.Repeat(horizChar, b.width-beforeTitle-titleLen-1)+topRightChar, b.borderColor)
		// 	// rightSide = strings.Repeat(horizChar, b.width-beforeTitle-titleLen-1) + topRightChar
		// }

		topBorder += strings.Repeat(horizChar, beforeTitle)

		if UseColors {
			topBorder += "" + Colored(b.titleColor, b.title) + " " + rightSide
		} else {
			topBorder += "" + b.title + " " + rightSide
		}

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
		topBorder += topLeftChar
	}

	topBorder += strings.Repeat(horizChar, b.width) + topRightChar

	fmt.Println(topBorder)
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

	// if UseColors {
	// 	fmt.Println(Dimmed(bottomBorder, b.borderColor))
	// 	// fmt.Println(Colored(b.borderColor, bottomBorder))
	// } else {
	// 	fmt.Println(bottomBorder)
	// }

}

// draw an empty row
func (b *Box) DrawEmpty() {
	fmt.Println(b.emptyLineCache)
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
		// line += vertChar
		// if UseColors {
		// 	line += (Dimmed(vertChar, b.borderColor))
		// 	// line += Colored(b.borderColor, vertChar)
		// } else {
		// 	line += vertChar
		// }
	} else if b.indentation > 0 {
		// Add indentation if left border is hidden
		line += strings.Repeat(b.space, b.indentation)
	}

	line += content + strings.Repeat(b.space, padding)

	line += (Dimmed(vertChar, b.borderColor))
	// if UseColors {
	// 	line += (Dimmed(vertChar, b.borderColor))
	// 	// line += Colored(b.borderColor, vertChar)
	// } else {
	// 	line += vertChar
	// }

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

	// top padding
	if b.showEmptyRow {
		b.DrawEmpty()
	}

	if contentFn != nil {
		contentFn(func(line string) {
			b.DrawLine(line)
		})
	}

	// bottom padding
	if b.showEmptyRow {
		b.DrawEmpty()
	}

	b.DrawBottom()
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
		// if UseColors {
		// 	line += Colored(b.borderColor, vertChar)
		// } else {
		// 	line += vertChar
		// }
	} else if b.indentation > 0 {
		// Add indentation if left border is hidden
		line += strings.Repeat(b.space, b.indentation)
	}

	line += strings.Repeat(b.space, leftPadding) + text + strings.Repeat(b.space, rightPadding)

	line += (Dimmed(vertChar, b.borderColor))
	// if UseColors {

	// 	line += (Dimmed(vertChar, b.borderColor))
	// 	// line += Colored(b.borderColor, vertChar)
	// } else {
	// 	line += vertChar
	// }

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
		// if UseColors {
		// 	line += Colored(b.borderColor, vertChar)
		// } else {
		// 	line += vertChar
		// }
	} else if b.indentation > 0 {
		// Add indentation if left border is hidden
		line += strings.Repeat(b.space, b.indentation)
	}

	line += strings.Repeat(b.space, padding) + text

	line += (Dimmed(vertChar, b.borderColor))

	// if UseColors {

	// 	line += (Dimmed(vertChar, b.borderColor))
	// 	// line += Colored(b.borderColor, vertChar)
	// } else {
	// 	line += vertChar
	// }

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
		// if UseColors {
		// 	line += Colored(b.borderColor, vertChar)
		// } else {
		// 	line += vertChar
		// }
	} else if b.indentation > 0 {
		// Add indentation if left border is hidden
		line += strings.Repeat(b.space, b.indentation)
	}

	line += strings.Repeat(b.space, leftPadding) + text + strings.Repeat(b.space, rightPadding)

	line += (Dimmed(vertChar, b.borderColor))

	// if UseColors {

	// 	line += (Dimmed(vertChar, b.borderColor))
	// 	// line += Colored(b.borderColor, vertChar)
	// } else {
	// 	line += vertChar
	// }

	fmt.Println(line)
}
