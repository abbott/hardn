package component

import (
	"charm.land/lipgloss/v2"

	"github.com/abbott/hardn/pkg/ui/theme"
)

// riskLabel is the RiskSummary's label, as Figma and origin/main show it.
const riskLabel = "Risk Level"

// RiskBand is the tone of a risk level. The status model maps its levels
// onto the bands: origin/main's Critical and High are RiskHigh, Moderate is
// RiskModerate, and Low and Minimal are RiskLow (DEV-016).
type RiskBand int

// The risk bands. The zero value is RiskUnknown: a level hardn could not
// work out.
const (
	RiskUnknown RiskBand = iota
	RiskLow
	RiskModerate
	RiskHigh
)

// String returns the band's name, which a RiskSummary shows when it has no
// Level. A value outside the bands is "Unknown".
func (b RiskBand) String() string {
	switch b {
	case RiskLow:
		return "Low"
	case RiskModerate:
		return "Moderate"
	case RiskHigh:
		return "High"
	}
	return unknown
}

// style returns the band's style in t.
func (b RiskBand) style(t theme.RiskStyles) lipgloss.Style {
	switch b {
	case RiskLow:
		return t.Low
	case RiskModerate:
		return t.Moderate
	case RiskHigh:
		return t.High
	}
	return t.Unknown
}

// RiskSummary is the risk level line, Figma's one line; here with no
// colour, under the column numbers:
//
//	0 2   6                    27
//	   ⛬  Risk Level           Moderate ≈ medium security
//
// The risk icon sits in the marker column and "Risk Level" in the label
// column; the icon and the level word take the band's colour, so the
// level never rests on colour alone. The gloss follows Glyphs.Approx in
// text.faint. The factors that explain the level are drawn under the
// status rows they are about (StatusRow.Lines), and the pending reboot in
// the HostBlock, not here (the maintainer, 2026-10-21; DEV-022, DEV-059).
// Below theme.StackBelow the level stacks under the label (DEV-021).
type RiskSummary struct {
	// Band sets the colour of the icon and level word.
	Band RiskBand
	// Level is the level word, such as "Moderate" or origin/main's
	// "Critical". Empty shows the band's name.
	Level string
	// Gloss is shown after Glyphs.Approx, such as "medium security"; empty
	// shows neither.
	Gloss string
}

// Render renders the summary for c. See RiskSummary for the layout.
func (vm RiskSummary) Render(c Ctx) string {
	t, g := c.Theme.Risk, c.Theme.Glyphs
	band := vm.Band.style(t)
	label := append(marker(g.Risk, band).to(theme.LabelCol), span{riskLabel, t.Label})
	level := c.text(vm.Level)
	if level == "" {
		level = vm.Band.String()
	}
	value := line{{level, band}}
	if gloss := c.text(vm.Gloss); gloss != "" {
		value = append(value, gap(1), span{g.Approx + " " + gloss, t.Gloss})
	}
	return c.columns(label, value)
}
