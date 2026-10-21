package theme

import (
	"reflect"
	"slices"
	"strings"
	"testing"
	"unicode"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/colorprofile"
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
// wide, and only Partial shares a marker, Not Configured's, as a warning
// does (the maintainer, 2026-10-21). Two states with one marker are told
// apart by colour (TestSharedMarkerColours) and by the word on the line
// (pkg/ui/component's TestStatusStates), never by colour alone.
func TestStatusMarkers(t *testing.T) {
	for name, g := range map[string]Glyphs{"unicode": unicodeGlyphs, "ascii": asciiGlyphs} {
		markers := []string{g.Configured, g.NotConfigured, g.Unknown, g.NotApplicable}
		for _, m := range append(markers, g.Partial, g.Risk) {
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
		if g.Partial != g.NotConfigured || g.Warn != g.Partial {
			t.Errorf("%s: Partial %q and Warn %q, want Not Configured's %q", name, g.Partial, g.Warn, g.NotConfigured)
		}
	}
}

// TestSharedMarkerColours checks the other half of the shared marker's
// rule: wherever there is colour, Partial's style differs from Not
// Configured's, on a status row and on a key-value row's marker, so that
// the two read apart by colour as well as by their words.
func TestSharedMarkerColours(t *testing.T) {
	for _, env := range envs() {
		th := New(env)
		if env.Profile <= colorprofile.ASCII {
			continue // no colour: the word alone tells them, as it always does
		}
		for _, pair := range [][2]lipgloss.Style{
			{th.Status.Partial, th.Status.NotConfigured},
			{th.KeyValue.ValueWarn, th.KeyValue.ValueBad},
		} {
			if pair[0].GetForeground() == pair[1].GetForeground() {
				t.Errorf("%s: Partial and Not Configured share the colour %v", envName(env), pair[0].GetForeground())
			}
		}
	}
}

// TestRiskGlyphAlone checks that the risk icon, ⛬ or [*], is no other
// role's glyph and is in none, so that only the code that names
// Glyphs.Risk draws it: RiskSummary alone (pkg/ui/component's
// TestRiskGlyphDrawn). The lockout-risk lines take Warn (the maintainer,
// 2026-10-21).
func TestRiskGlyphAlone(t *testing.T) {
	for name, g := range map[string]Glyphs{"unicode": unicodeGlyphs, "ascii": asciiGlyphs} {
		for field, glyph := range glyphFields(g) {
			if field != "Risk" && strings.Contains(glyph, g.Risk) {
				t.Errorf("%s.%s = %q holds the risk icon %q", name, field, glyph, g.Risk)
			}
		}
	}
}

// TestMessageMarkers checks the glyphs of a message's kind: they fit the
// marker column, even where ambiguous-width glyphs are drawn two cells
// wide, and no two kinds share one, so a kind never rests on colour alone.
// A warning's glyph is Partial's (TestStatusMarkers), its word "Warning".
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

// TestStepMarkers checks the glyphs of a step's state, each spinner frame
// and the countdown's urgent mark: they fit the marker column, even where
// ambiguous-width glyphs are drawn two cells wide, and no two states share
// one, so a state never rests on colour alone. The spinner has frames to
// cycle through, and none is another state's glyph.
func TestStepMarkers(t *testing.T) {
	for name, g := range map[string]Glyphs{"unicode": unicodeGlyphs, "ascii": asciiGlyphs} {
		states := []string{g.Pending, g.Running, g.OK, g.Bad, g.RolledBack, g.Skipped}
		frames := strings.Split(g.Spinner, "")
		if len(frames) < 2 {
			t.Errorf("%s: the spinner has %d frames", name, len(frames))
		}
		for _, m := range slices.Concat(states, frames, []string{g.Urgent}) {
			if w := max(wideWidth(m), lipgloss.Width(m)); w > MarkerWidth {
				t.Errorf("%s: glyph %q is %d cells wide, over MarkerWidth %d", name, m, w, MarkerWidth)
			}
		}
		for i, m := range states {
			if slices.Contains(states[i+1:], m) || slices.Contains(frames, m) {
				t.Errorf("%s: glyph %q marks two step states", name, m)
			}
		}
	}
}

// TestAmbiguousGlyphs keeps the Glyphs doc comment's list of East Asian
// ambiguous-width glyphs in step with the Unicode set.
func TestAmbiguousGlyphs(t *testing.T) {
	const documented = "⛛ ⭘ ⛬ – ≈ … █ ─"
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
