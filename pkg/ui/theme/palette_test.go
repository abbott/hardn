package theme

import (
	"fmt"
	"image/color"
	"math"
	"reflect"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// namedToken is a palette token with its design-system name.
type namedToken struct {
	name string
	token
}

// tokens returns every token of p in field order, named by its token tag.
func tokens(t *testing.T, p palette) []namedToken {
	t.Helper()
	v := reflect.ValueOf(p)
	var out []namedToken
	for i := range v.NumField() {
		f := v.Type().Field(i)
		name := f.Tag.Get("token")
		if name == "" {
			t.Fatalf("palette.%s has no token tag", f.Name)
		}
		tk, ok := v.Field(i).Interface().(token)
		if !ok {
			t.Fatalf("palette.%s is not a token", f.Name)
		}
		out = append(out, namedToken{name, tk})
	}
	return out
}

var palettes = []struct {
	name string
	p    palette
	page color.Color // the terminal background the palette is drawn on
}{
	{"dark", darkPalette, color.Black},
	{"light", lightPalette, color.White},
}

// profileColors returns t's colour at each colour profile, by profile name.
func profileColors(t token) map[string]color.Color {
	return map[string]color.Color{
		"truecolor": lipgloss.Color(t.hex),
		"256":       t.ansi256,
		"16":        t.ansi,
	}
}

func TestTokensComplete(t *testing.T) {
	for _, pt := range palettes {
		seen := map[string]bool{}
		for _, tk := range tokens(t, pt.p) {
			t.Run(pt.name+"/"+tk.name, func(t *testing.T) {
				if seen[tk.name] {
					t.Errorf("token name %q is used twice", tk.name)
				}
				seen[tk.name] = true
				if _, ok := lipgloss.Color(tk.hex).(color.RGBA); !ok || len(tk.hex) != len("#RRGGBB") {
					t.Errorf("truecolor %q is not #RRGGBB", tk.hex)
				}
				if tk.ansi256 < 16 {
					t.Errorf("256-colour index %d is one of the 16 terminal colours", tk.ansi256)
				}
				if tk.ansi > lipgloss.BrightWhite {
					t.Errorf("16-colour index %d is out of range", tk.ansi)
				}
			})
		}
	}
}

// contrastFloors are the WCAG contrast minimums of each token against the
// page background, by palette. Tokens with no floor are listed with 0: the
// bar and badge backgrounds are surfaces, and badge text is checked against
// the badge instead. The dark divider's floor is lower: a rule is
// decorative, the blank lines around it already part the blocks, and it is
// kept quieter than the faintest text (TestDividerBelowFaint; DEV-002).
var contrastFloors = map[string]map[string]float64{
	"dark": {
		"text.primary": 7, "text.muted": 4.5, "text.faint": 3,
		"status.ok": 4.5, "status.bad": 4.5, "status.warn": 4.5, "status.unknown": 4.5,
		"divider": 1.5, "accent.ok": 3, "accent.warn": 3,
		"badge.bg": 0, "badge.fg": 0, "bar.bg": 0,
	},
	"light": {
		"text.primary": 4.5, "text.muted": 3, "text.faint": 3,
		"status.ok": 4.5, "status.bad": 4.5, "status.warn": 4.5, "status.unknown": 4.5,
		"divider": 3, "accent.ok": 3, "accent.warn": 3,
		"badge.bg": 0, "badge.fg": 0, "bar.bg": 0,
	},
}

// contrastExceptions are profile values allowed under their floor, by
// "palette/profile/token", with the reason. 16-colour values are measured
// in x/ansi's RGB table for the 16 colours (color.go:269-284 in v0.11.8),
// which stands in for the unknown terminal palette.
var contrastExceptions = map[string]string{
	"light/16/status.warn": "no 16-colour yellow reaches 4.5:1 on white there; Yellow (#808000, 4.2:1) is the closest hue",
}

func TestContrast(t *testing.T) {
	for _, pt := range palettes {
		floors := contrastFloors[pt.name]
		toks := tokens(t, pt.p)
		if len(floors) != len(toks) {
			t.Errorf("%s: %d contrast floors for %d tokens", pt.name, len(floors), len(toks))
		}
		for _, tk := range toks {
			floor, ok := floors[tk.name]
			if !ok {
				t.Errorf("%s: no contrast floor for %s", pt.name, tk.name)
			}
			for profile, c := range profileColors(tk.token) {
				key := fmt.Sprintf("%s/%s/%s", pt.name, profile, tk.name)
				got := contrast(c, pt.page)
				_, excused := contrastExceptions[key]
				switch {
				case got < floor && !excused:
					t.Errorf("%s: contrast %.2f:1, want at least %.1f:1", key, got, floor)
				case got >= floor && excused:
					t.Errorf("%s: contrast %.2f:1 meets %.1f:1; drop the exception", key, got, floor)
				}
			}
		}
		// Badge text sits on the badge, not on the page.
		fg, bg := profileColors(pt.p.BadgeFG), profileColors(pt.p.BadgeBG)
		for profile := range fg {
			if got := contrast(fg[profile], bg[profile]); got < 4.5 {
				t.Errorf("%s/%s: badge.fg on badge.bg is %.2f:1, want at least 4.5:1", pt.name, profile, got)
			}
		}
	}
}

// ansi256Overrides are 256-colour picks that are deliberately not the
// nearest index, by "palette/token".
var ansi256Overrides = map[string]struct {
	pick   ansi.IndexedColor
	reason string
}{
	"dark/text.faint": {59, "240 (#585858) is nearest (ΔE 10.04) but 2.95:1 on black, under faint's 3:1; 59 (#5F5F5F, ΔE 10.07) is 3.29:1"},
}

// TestDividerBelowFaint holds a rule no louder than the faintest text, so
// that it never competes with the words around it (DEV-002). At 16 colours
// both are BrightBlack, the only gray between black and white.
func TestDividerBelowFaint(t *testing.T) {
	for _, pt := range palettes {
		div, faint := profileColors(pt.p.Divider), profileColors(pt.p.TextFaint)
		for _, profile := range []string{"truecolor", "256"} {
			d, f := contrast(div[profile], pt.page), contrast(faint[profile], pt.page)
			if d > f {
				t.Errorf("%s/%s: divider is %.2f:1, above text.faint's %.2f:1", pt.name, profile, d, f)
			}
		}
	}
}

func TestANSI256Nearest(t *testing.T) {
	for _, pt := range palettes {
		for _, tk := range tokens(t, pt.p) {
			key := pt.name + "/" + tk.name
			nearest := nearest256(lipgloss.Color(tk.hex))
			want := nearest
			if o, ok := ansi256Overrides[key]; ok {
				if o.pick == nearest {
					t.Errorf("%s: %d is the nearest index; drop the override", key, nearest)
				}
				want = o.pick
			}
			if tk.ansi256 != want {
				t.Errorf("%s: 256-colour pick is %d, want %d (nearest by CIE76 ΔE: %d)", key, tk.ansi256, want, nearest)
			}
		}
	}
}

// TestDeliberateANSIPicks pins the 16-colour picks the deviation log
// records.
func TestDeliberateANSIPicks(t *testing.T) {
	tests := []struct {
		name string
		got  ansi.BasicColor
		want ansi.BasicColor
	}{
		{"dark text.faint (DEV-001)", darkPalette.TextFaint.ansi, lipgloss.BrightBlack},
		{"dark text.muted (DEV-001)", darkPalette.TextMuted.ansi, lipgloss.BrightBlack},
		{"dark divider (DEV-002)", darkPalette.Divider.ansi, lipgloss.BrightBlack},
		{"dark badge.fg (DEV-003)", darkPalette.BadgeFG.ansi, lipgloss.BrightWhite},
		{"dark bar.bg (DEV-004)", darkPalette.BarBG.ansi, lipgloss.BrightBlack},
	}
	for _, tt := range tests {
		if tt.got != tt.want {
			t.Errorf("%s: 16-colour pick is %d, want %d", tt.name, tt.got, tt.want)
		}
	}
}

// nearest256 returns the xterm-256 index from 16 to 255 nearest to c by CIE76
// ΔE, using x/ansi's RGB values for the indexes.
func nearest256(c color.Color) ansi.IndexedColor {
	target := lab(c)
	best, bestDist := ansi.IndexedColor(16), math.Inf(1)
	for i := 16; i < 256; i++ {
		if d := deltaE76(target, lab(ansi.IndexedColor(i))); d < bestDist {
			best, bestDist = ansi.IndexedColor(i), d
		}
	}
	return best
}

// linear returns c's linear-light sRGB components (IEC 61966-2-1).
func linear(c color.Color) (r, g, b float64) {
	cr, cg, cb, _ := c.RGBA()
	f := func(v uint32) float64 {
		s := float64(v) / 0xffff
		if s <= 0.04045 {
			return s / 12.92
		}
		return math.Pow((s+0.055)/1.055, 2.4)
	}
	return f(cr), f(cg), f(cb)
}

// contrast returns the WCAG 2 contrast ratio of a and b.
func contrast(a, b color.Color) float64 {
	lum := func(c color.Color) float64 {
		r, g, b := linear(c)
		return 0.2126*r + 0.7152*g + 0.0722*b
	}
	la, lb := lum(a), lum(b)
	return (max(la, lb) + 0.05) / (min(la, lb) + 0.05)
}

// lab returns c in CIE L*a*b* with the D65 white point, by the same
// constants as go-colorful.
func lab(c color.Color) [3]float64 {
	r, g, b := linear(c)
	x := (0.41239079926595948*r + 0.35758433938387796*g + 0.18048078840183429*b) / 0.95047
	y := 0.21263900587151036*r + 0.71516867876775593*g + 0.072192315360733715*b
	z := (0.019330818715591851*r + 0.11919477979462599*g + 0.95053215224966058*b) / 1.08883
	f := func(t float64) float64 {
		const d = 6.0 / 29.0
		if t > d*d*d {
			return math.Cbrt(t)
		}
		return t/(3*d*d) + 4.0/29.0
	}
	fx, fy, fz := f(x), f(y), f(z)
	return [3]float64{116*fy - 16, 500 * (fx - fy), 200 * (fy - fz)}
}

// deltaE76 is the CIE76 colour difference: the distance in L*a*b*.
func deltaE76(p, q [3]float64) float64 {
	return math.Sqrt((p[0]-q[0])*(p[0]-q[0]) + (p[1]-q[1])*(p[1]-q[1]) + (p[2]-q[2])*(p[2]-q[2]))
}
