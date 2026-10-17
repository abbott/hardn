package theme

import (
	"fmt"
	"maps"
	"reflect"
	"slices"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"

	"github.com/abbott/hardn/pkg/ui/term"
	"github.com/abbott/hardn/pkg/ui/uitest"
)

// TestSwatch pins the theme on the full component matrix
// (testdata/golden/swatch.golden): every semantic token, the glyph set and
// every typography role, as the terminal receives them.
func TestSwatch(t *testing.T) {
	uitest.Golden(t, "swatch", uitest.Full(), func(env term.Env) string {
		return swatch(t, env)
	})
}

// swatch renders, for env:
//   - each token's name in the token's colour, then a block with the
//     token as its background, in palette order;
//   - each glyph of the set, by role;
//   - each typography role: the slots that set bold or underline, each
//     slot's path in the slot's own style, sorted by path.
func swatch(t *testing.T, env term.Env) string {
	th := New(env)
	p := lightPalette
	if env.Dark {
		p = darkPalette
	}
	const nameWidth = 16
	var b strings.Builder
	b.WriteString("Tokens: the name in its colour, then as a background\n")
	for _, tk := range tokens(t, p) {
		c, ok := profileColors(tk.token)[profileKey(env.Profile)]
		if !ok {
			c = lipgloss.NoColor{}
		}
		fmt.Fprintf(&b, "  %s%s%s\n",
			lipgloss.NewStyle().Foreground(c).Render(tk.name),
			strings.Repeat(" ", max(1, nameWidth-len(tk.name))),
			lipgloss.NewStyle().Background(c).Render("      "))
	}
	b.WriteString("\nGlyphs\n")
	g := reflect.ValueOf(th.Glyphs)
	for i := range g.NumField() {
		fmt.Fprintf(&b, "  %-*s%s\n", nameWidth, g.Type().Field(i).Name, g.Field(i).String())
	}
	b.WriteString("\nTypography: the slots that set bold or underline\n")
	all := slots(th)
	for _, path := range slices.Sorted(maps.Keys(all)) {
		if s := all[path]; s.GetBold() || s.GetUnderline() {
			fmt.Fprintf(&b, "  %s\n", s.Render(path))
		}
	}
	return strings.TrimSuffix(b.String(), "\n")
}
