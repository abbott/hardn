package logview

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/abbott/hardn/pkg/host"
)

// The words this package shows (proposed; readers-system-details.md).
const (
	// cutMark ends a line host.ScanLines cut at host.MaxLine.
	cutMark = "<cut>"
	// notRegular is the reason for what is neither a file nor a directory.
	notRegular = "not a regular file"
)

// Show is a line of the log as the screen and -p show it: Safe, and marked
// where it was cut. A cut can fall inside a character, whose bytes then show
// as bytes.
func Show(l host.Line) string {
	s := Safe(string(l.Text))
	if l.Cut {
		return s + cutMark
	}
	return s
}

// Safe makes a line safe to write to a terminal, keeping it readable: what a
// terminal would act on is shown, not stripped, so a line still says what it
// held.
//
//   - C0 and DEL as less and cat -v show them, ^[ for ESC, ^G for BEL, ^? for
//     DEL, a tab as 4 spaces, as a plan's captured output shows one;
//   - C1, U+0080 to U+009F, as its code point, <U+009B>: CSI by another name;
//   - what draws as nothing, or as a blank other than the space, as its code
//     point: bidi overrides and isolates, format characters, line and paragraph
//     separators, no-break spaces, variation selectors, other default
//     ignorables and noncharacters;
//   - a byte that is not UTF-8 as <0xe9>, each on its own, so a raw 0x9B, an
//     overlong encoding (0xc0 0x9b) or a surrogate cannot reach a terminal that
//     decodes them more loosely than this does.
//
// Everything else keeps its characters, typographic ones too. What it returns
// is valid UTF-8, holds no control character, and Safe leaves it as it is.
func Safe(s string) string {
	var b strings.Builder
	for len(s) > 0 {
		r, n := utf8.DecodeRuneInString(s)
		switch {
		case r == utf8.RuneError && n == 1:
			fmt.Fprintf(&b, "<0x%02x>", s[0])
		case r == '\t':
			b.WriteString("    ")
		case r < 0x20:
			b.WriteByte('^')
			b.WriteByte(byte(r) + 0x40)
		case r == 0x7f:
			b.WriteString("^?")
		case invisible(r):
			fmt.Fprintf(&b, "<U+%04X>", r)
		default:
			b.WriteRune(r)
		}
		s = s[n:]
	}
	return b.String()
}

// invisible reports what Safe shows by its code point.
func invisible(r rune) bool {
	return unicode.IsControl(r) || r != ' ' && unicode.In(r, unicode.Cf, unicode.Zs, unicode.Zl, unicode.Zp,
		unicode.Variation_Selector, unicode.Other_Default_Ignorable_Code_Point, unicode.Noncharacter_Code_Point)
}
