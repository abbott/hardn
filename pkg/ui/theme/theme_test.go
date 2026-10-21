package theme

import (
	"bytes"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/colorprofile"

	"github.com/abbott/hardn/pkg/ui/term"
)

var profiles = []colorprofile.Profile{
	colorprofile.TrueColor, colorprofile.ANSI256, colorprofile.ANSI, colorprofile.ASCII, colorprofile.NoTTY,
}

// envs returns every combination of profile, background and glyph set.
func envs() []term.Env {
	var out []term.Env
	for _, p := range profiles {
		for _, dark := range []bool{true, false} {
			for _, unicode := range []bool{true, false} {
				if p == colorprofile.NoTTY && unicode {
					continue // plain: Fixed uses ASCII glyphs regardless
				}
				out = append(out, term.Fixed(term.DefaultWidth, p, unicode, dark))
			}
		}
	}
	return out
}

func envName(e term.Env) string {
	return fmt.Sprintf("%v/dark=%t/unicode=%t", e.Profile, e.Dark, e.Unicode)
}

// slots returns every style slot of th by its path, such as "Menu.Key" or
// "Callout.OK.Title".
func slots(th *Theme) map[string]lipgloss.Style {
	out := map[string]lipgloss.Style{}
	var walk func(prefix string, v reflect.Value)
	walk = func(prefix string, v reflect.Value) {
		for i := range v.NumField() {
			f, path := v.Field(i), prefix+v.Type().Field(i).Name
			if s, ok := f.Interface().(lipgloss.Style); ok {
				out[path] = s
			} else if f.Kind() == reflect.Struct {
				walk(path+".", f)
			}
		}
	}
	walk("", reflect.ValueOf(th).Elem())
	return out
}

// setsLayout reports whether s sets any layout: a size or maximum size,
// padding, margins, a border, an alignment other than the default, or
// inline.
func setsLayout(s lipgloss.Style) bool {
	return s.GetWidth() != 0 || s.GetHeight() != 0 ||
		s.GetMaxWidth() != 0 || s.GetMaxHeight() != 0 ||
		s.GetHorizontalFrameSize() != 0 || s.GetVerticalFrameSize() != 0 ||
		s.GetAlignHorizontal() != lipgloss.Left || s.GetAlignVertical() != lipgloss.Top ||
		s.GetInline()
}

// TestSetsLayout checks that setsLayout sees each kind of layout.
func TestSetsLayout(t *testing.T) {
	base := lipgloss.NewStyle().Foreground(lipgloss.Red).Bold(true)
	tests := []struct {
		name string
		s    lipgloss.Style
		want bool
	}{
		{"colour and bold", base, false},
		{"width", base.Width(10), true},
		{"height", base.Height(2), true},
		{"max width", base.MaxWidth(10), true},
		{"max height", base.MaxHeight(2), true},
		{"padding", base.PaddingLeft(1), true},
		{"margin", base.MarginTop(1), true},
		{"border", base.Border(lipgloss.NormalBorder()), true},
		{"horizontal alignment", base.Align(lipgloss.Center), true},
		{"vertical alignment", base.AlignVertical(lipgloss.Bottom), true},
		{"inline", base.Inline(true), true},
	}
	for _, tt := range tests {
		if got := setsLayout(tt.s); got != tt.want {
			t.Errorf("%s: setsLayout = %t, want %t", tt.name, got, tt.want)
		}
	}
}

// TestSlotsSetWithoutBannedAttributes checks every slot in every
// environment: each is set, none uses SGR faint (muted and faint text are
// gray tokens, DEV-009), blink or reverse, and none carries layout, which
// belongs to the components.
func TestSlotsSetWithoutBannedAttributes(t *testing.T) {
	for _, env := range envs() {
		all := slots(New(env))
		if len(all) < 60 {
			t.Fatalf("%s: walked %d slots; the walk is broken", envName(env), len(all))
		}
		for path, s := range all {
			if reflect.ValueOf(s).IsZero() {
				t.Errorf("%s: %s is not set", envName(env), path)
			}
			if s.GetFaint() || s.GetBlink() || s.GetReverse() {
				t.Errorf("%s: %s sets faint, blink or reverse", envName(env), path)
			}
			if setsLayout(s) {
				t.Errorf("%s: %s sets layout", envName(env), path)
			}
		}
	}
}

// TestTypography checks the typography roles read from the screenshots:
// exactly these slots are bold, and only the menu heading is underlined.
func TestTypography(t *testing.T) {
	bold := []string{
		"Header.Wordmark", "ContextBar.Name", "TitleBar.Title", "Risk.Label",
		"Menu.Key", "Menu.ExitKey", "Prompt.Glyph", "Prompt.Label",
		"Message.OK", "Message.Warn", "Message.Bad", "Message.Info", "Message.Cancelled",
		"Callout.OK.Title", "Callout.Warn.Title", "Callout.Bad.Title", "Callout.Info.Title",
		"Callout.Cancelled.Title",
		"Badge", "Section.Title", "Breadcrumb.Current", "Plan.LockoutRisk", "Menu.Risk",
		"Confirm.Waiting", "Confirm.Urgent", "Confirm.Command",
	}
	underline := []string{"Menu.Heading"}
	for _, env := range envs() {
		for path, s := range slots(New(env)) {
			if got, want := s.GetBold(), slices.Contains(bold, path); got != want {
				t.Errorf("%s: %s bold is %t, want %t", envName(env), path, got, want)
			}
			if got, want := s.GetUnderline(), slices.Contains(underline, path); got != want {
				t.Errorf("%s: %s underline is %t, want %t", envName(env), path, got, want)
			}
		}
	}
}

// TestColourRoles checks that the main slots take the colour of their
// token, at every profile: the token's value for the profile, or no colour
// at ASCII and NoTTY.
func TestColourRoles(t *testing.T) {
	roles := []struct {
		slot  string
		bg    bool // the slot's background rather than its foreground
		token string
	}{
		{"Header.Wordmark", false, "text.primary"},
		{"Header.URL", false, "text.faint"},
		{"ContextBar.Fill", false, "bar.shade"},
		{"ContextBar.Unknown", false, "text.muted"},
		{"TitleBar.Fill", false, "bar.shade"},
		{"TitleBar.Parent", false, "text.muted"},
		{"TitleBar.Separator", false, "text.faint"},
		{"TitleBar.Title", false, "text.primary"},
		{"Host.Unknown", false, "text.muted"},
		{"Risk.Moderate", false, "status.warn"},
		{"Risk.Unknown", false, "text.muted"},
		{"Risk.Gloss", false, "text.faint"},
		{"Risk.Factor", false, "text.muted"},
		{"Status.Label", false, "text.primary"},
		{"Status.Detail", false, "text.faint"},
		{"Status.Configured", false, "status.ok"},
		{"Status.NotConfigured", false, "status.bad"},
		{"Status.Partial", false, "status.warn"},
		{"Status.NotApplicable", false, "text.muted"},
		{"Status.Unknown", false, "text.muted"},
		{"Divider", false, "divider"},
		{"Mode.Live", false, "status.warn"},
		{"Menu.Heading", false, "text.muted"},
		{"Menu.Key", false, "text.primary"},
		{"Menu.Description", false, "text.muted"},
		{"Menu.Aside", false, "text.faint"},
		{"Menu.Disabled", false, "text.faint"},
		{"Menu.Risk", false, "status.warn"},
		{"Field.Label", false, "text.primary"},
		{"Field.Answer", false, "text.muted"}, // drift D4
		{"Field.Description", false, "text.muted"},
		{"Callout.OK.Bar", true, "accent.ok"},
		{"Callout.Warn.Bar", true, "accent.warn"},
		{"Callout.Bad.Bar", true, "status.bad"},
		{"Callout.Info.Bar", true, "text.muted"},
		{"Callout.Info.Title", false, "text.primary"},
		{"Section.Fill", false, "bar.shade"},
		{"KeyValue.ValueUnknown", false, "text.muted"},
		{"KeyValue.ValueNotApplicable", false, "text.muted"},
		{"Badge", false, "badge.fg"},
		{"Badge", true, "badge.bg"},
		{"Plan.Title", false, "text.primary"},
		{"Plan.Target", false, "text.muted"},
		{"Plan.Added", false, "status.ok"},
		{"Plan.Removed", false, "status.bad"},
		{"Plan.Meta", false, "text.faint"},
		{"Plan.Command", false, "text.muted"},
		{"Plan.LockoutRisk", false, "status.warn"},
		{"Step.Pending", false, "text.muted"},
		{"Step.Done", false, "status.ok"},
		{"Step.Failed", false, "status.bad"},
		{"Step.RolledBack", false, "status.warn"},
		{"Step.Skipped", false, "text.muted"},
		{"Step.Detail", false, "text.faint"},
		{"Output", false, "text.muted"},
		{"Confirm.Waiting", false, "status.warn"},
		{"Confirm.Urgent", false, "status.bad"},
		{"Confirm.Command", false, "text.primary"},
	}
	for _, env := range envs() {
		p := lightPalette
		if env.Dark {
			p = darkPalette
		}
		byName := map[string]token{}
		for _, tk := range tokens(t, p) {
			byName[tk.name] = tk.token
		}
		all := slots(New(env))
		for _, r := range roles {
			tk, ok := byName[r.token]
			if !ok {
				t.Fatalf("no token %q", r.token)
			}
			want, ok := profileColors(tk)[profileKey(env.Profile)]
			if !ok {
				want = lipgloss.NoColor{}
			}
			got := all[r.slot].GetForeground()
			if r.bg {
				got = all[r.slot].GetBackground()
			}
			if got != want {
				t.Errorf("%s: %s is %v, want %v (%s)", envName(env), r.slot, got, want, r.token)
			}
		}
	}
}

// profileKey maps a profile to its key in profileColors.
func profileKey(p colorprofile.Profile) string {
	switch p {
	case colorprofile.TrueColor:
		return "truecolor"
	case colorprofile.ANSI256:
		return "256"
	case colorprofile.ANSI:
		return "16"
	}
	return ""
}

// TestVersionBox checks DEV-005: the header's version sits in a bar.bg box
// above 16 colours and in none at 16 colours, where bar.bg is the colour of
// muted text.
func TestVersionBox(t *testing.T) {
	for _, env := range envs() {
		got := New(env).Header.Version.GetBackground() != lipgloss.NoColor{}
		if want := env.Profile > colorprofile.ANSI; got != want {
			t.Errorf("%s: version box is %t, want %t", envName(env), got, want)
		}
	}
}

// TestBarsUnpainted checks the bars' slots in every environment: none sets
// a background, so the stub, the fill and the text of a context bar, a
// title bar and a section header sit on the terminal's own (the
// maintainer, 2026-10-21; DEV-018, DEV-033).
func TestBarsUnpainted(t *testing.T) {
	for _, env := range envs() {
		for path, s := range slots(New(env)) {
			bar := strings.HasPrefix(path, "ContextBar.") || strings.HasPrefix(path, "TitleBar.") || strings.HasPrefix(path, "Section.")
			if bar && s.GetBackground() != (lipgloss.NoColor{}) {
				t.Errorf("%s: %s paints a background, %v", envName(env), path, s.GetBackground())
			}
		}
	}
}

// TestWriterKeepsPicks checks that every slot's output is already at the
// environment's profile: Env.Writer passes it through unchanged, so the
// deliberate 256- and 16-colour picks are what the terminal receives.
func TestWriterKeepsPicks(t *testing.T) {
	for _, env := range envs() {
		if env.Profile == colorprofile.NoTTY {
			continue // the writer strips every sequence; nothing is kept
		}
		for path, s := range slots(New(env)) {
			in := s.Render("x")
			var buf bytes.Buffer
			if _, err := fmt.Fprint(env.Writer(&buf), in); err != nil {
				t.Fatal(err)
			}
			if got := buf.String(); got != in {
				t.Errorf("%s: %s: writer changed %q to %q", envName(env), path, in, got)
			}
		}
	}
}

func TestGlyphsFollowEnv(t *testing.T) {
	for _, env := range envs() {
		want := asciiGlyphs
		if env.Unicode {
			want = unicodeGlyphs
		}
		if got := New(env).Glyphs; got != want {
			t.Errorf("%s: wrong glyph set: %+v", envName(env), got)
		}
	}
}

// TestGrid pins the grid to the Figma export (DEV-008): label column 6,
// value column 27, content width 64; a stacked value under the label
// (DEV-021).
func TestGrid(t *testing.T) {
	tests := []struct {
		name      string
		got, want int
	}{
		{"MarkerCol", MarkerCol, 2},
		{"LabelCol", LabelCol, 6},
		{"ValueCol", ValueCol, 27},
		{"StackCol", StackCol, 6},
		{"ContentWidth", ContentWidth, 64},
	}
	for _, tt := range tests {
		if tt.got != tt.want {
			t.Errorf("%s = %d, want %d", tt.name, tt.got, tt.want)
		}
	}
}
