package theme

import (
	"reflect"
	"slices"
	"strings"
	"testing"
	"unicode"

	"charm.land/lipgloss/v2"
	"golang.org/x/text/width"
)

// glyphFields returns every glyph of g by field name.
func glyphFields(g Glyphs) map[string]string {
	v := reflect.ValueOf(g)
	out := map[string]string{}
	for i := range v.NumField() {
		out[v.Type().Field(i).Name] = v.Field(i).String()
	}
	return out
}

func TestGlyphSetsComplete(t *testing.T) {
	sets := []struct {
		name string
		g    Glyphs
	}{{"unicode", unicodeGlyphs}, {"ascii", asciiGlyphs}}
	for _, set := range sets {
		for field, glyph := range glyphFields(set.g) {
			if glyph == "" {
				t.Errorf("%s.%s is empty", set.name, field)
			}
		}
	}
	for field, glyph := range glyphFields(asciiGlyphs) {
		for _, r := range glyph {
			if r > unicode.MaxASCII || !unicode.IsPrint(r) {
				t.Errorf("ascii.%s = %q is not printable ASCII", field, glyph)
			}
		}
	}
}

// TestStatusMarkers checks the glyphs that mark a state: they fit the
// marker column, even where ambiguous-width glyphs are drawn two cells
// wide, and no two states share one, so a state never rests on colour
// alone.
func TestStatusMarkers(t *testing.T) {
	for name, g := range map[string]Glyphs{"unicode": unicodeGlyphs, "ascii": asciiGlyphs} {
		markers := []string{g.Configured, g.NotConfigured, g.Partial, g.Unknown, g.NotApplicable}
		for _, m := range append(markers, g.Risk) {
			if w := wideWidth(m); w > MarkerWidth {
				t.Errorf("%s: marker %q is %d cells wide when ambiguous is wide, over MarkerWidth %d", name, m, w, MarkerWidth)
			}
			if w := lipgloss.Width(m); w > MarkerWidth {
				t.Errorf("%s: marker %q is %d cells wide, over MarkerWidth %d", name, m, w, MarkerWidth)
			}
		}
		for i, m := range markers {
			if slices.Contains(markers[i+1:], m) {
				t.Errorf("%s: marker %q marks two states", name, m)
			}
		}
	}
}

// TestMessageMarkers checks the glyphs of a message's kind: they fit the
// marker column, even where ambiguous-width glyphs are drawn two cells
// wide, and no two kinds share one, so a kind never rests on colour alone.
func TestMessageMarkers(t *testing.T) {
	for name, g := range map[string]Glyphs{"unicode": unicodeGlyphs, "ascii": asciiGlyphs} {
		kinds := []string{g.OK, g.Warn, g.Bad, g.Info, g.Cancelled}
		for i, m := range kinds {
			if w := max(wideWidth(m), lipgloss.Width(m)); w > MarkerWidth {
				t.Errorf("%s: kind glyph %q is %d cells wide, over MarkerWidth %d", name, m, w, MarkerWidth)
			}
			if slices.Contains(kinds[i+1:], m) {
				t.Errorf("%s: glyph %q marks two kinds", name, m)
			}
		}
	}
}

// TestAmbiguousGlyphs keeps the Glyphs doc comment's list of East Asian
// ambiguous-width glyphs in step with the Unicode set.
func TestAmbiguousGlyphs(t *testing.T) {
	const documented = "⛛ ⭘ ⛬ ≈ … █ ─"
	var found []string
	for _, glyph := range glyphFields(unicodeGlyphs) {
		for _, r := range glyph {
			if width.LookupRune(r).Kind() == width.EastAsianAmbiguous && !slices.Contains(found, string(r)) {
				found = append(found, string(r))
			}
		}
	}
	want := strings.Fields(documented)
	slices.Sort(found)
	slices.Sort(want)
	if !slices.Equal(found, want) {
		t.Errorf("ambiguous-width glyphs are %q, documented %q", found, want)
	}
}

// wideWidth is s's width in a terminal that draws East Asian ambiguous
// characters two cells wide.
func wideWidth(s string) int {
	n := 0
	for _, r := range s {
		switch width.LookupRune(r).Kind() {
		case width.EastAsianAmbiguous, width.EastAsianWide, width.EastAsianFullwidth:
			n += 2
		default:
			n++
		}
	}
	return n
}
