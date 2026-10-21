package output

import (
	"github.com/abbott/hardn/pkg/engine"
	"github.com/abbott/hardn/pkg/engine/risk"
	"github.com/abbott/hardn/pkg/ui/component"
)

// The risk level's words, by band: origin/main's level and gloss
// (pkg/security GetSecurityRiskLevel), Figma's "Moderate ≈ medium
// security" among them. A band with none (risk.Unknown) shows the
// component's "Unknown" and no gloss.
var levelWords = map[risk.Band]struct{ level, gloss string }{
	risk.Critical: {"Critical", "no security"},
	risk.High:     {"High", "weak security"},
	risk.Moderate: {"Moderate", "medium security"},
	risk.Low:      {"Low", "strong security"},
	risk.Minimal:  {"Minimal", "hardened security"},
}

// LockoutGloss is the gloss of a Critical the lockout forced, in place of
// "no security", which the score may not say (proposed wording,
// docs/engine/risk.md §6).
const LockoutGloss = "lockout risk"

// riskBands are the components' tones of the bands: Critical and High
// share High, Low and Minimal share Low, as origin/main's colours do
// (DEV-016).
var riskBands = map[risk.Band]component.RiskBand{risk.Critical: component.RiskHigh, risk.High: component.RiskHigh,
	risk.Moderate: component.RiskModerate, risk.Low: component.RiskLow, risk.Minimal: component.RiskLow}

// StatusView is the main menu's risk level and status rows (refactor plan
// Phase 4, next/p4-risk), the fields of screen.MainMenu that carry them.
type StatusView struct {
	Risk   component.RiskSummary
	Status component.StatusList
}

// StatusViewOf maps Engine.Check's reports and their risk level l
// (risk.Assess over the same reports) to the main menu's blocks. The rows
// are the score's, in Figma's order (risk.Rows: User, Firewall, SSH Login,
// SSH Auth, SSH Port, AppArmor, Auto Updates), whatever the reports'
// order; a row with no report shows Unknown, as the score counts it, and a
// report for a control the score does not weigh comes after them, in its
// order. A control's first report counts, and a report with an error is
// Unknown, as risk.Assess has them. The factors are l's, one line each. It
// is pure.
func StatusViewOf(reports []engine.Report, l risk.Level) StatusView {
	first := map[string]engine.Report{}
	var others []engine.Report
	for _, r := range reports {
		if _, ok := first[r.Control]; !ok {
			first[r.Control] = r
			others = append(others, r)
		}
	}
	var v StatusView
	for _, rw := range risk.Rows() {
		r, ok := first[rw.ID]
		if !ok {
			v.Status.Rows = append(v.Status.Rows, component.StatusRow{Label: rw.Title, State: component.StatusUnknown})
			continue
		}
		v.Status.Rows = append(v.Status.Rows, statusRow(r))
		delete(first, rw.ID)
	}
	for _, r := range others {
		if _, ok := first[r.Control]; ok {
			v.Status.Rows = append(v.Status.Rows, statusRow(r))
		}
	}
	v.Risk = RiskSummaryOf(l)
	return v
}

// statusRow is report r's row: its title, its state's glyph and word, its
// detail; Unknown when the check failed.
func statusRow(r engine.Report) component.StatusRow {
	st := r.Status.State
	if r.Err != nil {
		st = engine.Unknown
	}
	return component.StatusRow{Label: r.Title, State: statusStates[stateWord(st)], Detail: r.Status.Detail}
}

// RiskSummaryOf is the risk level l as the main menu's RiskSummary: the
// band's tone, its level word and gloss (LockoutGloss when the lockout
// forced it), and l's factors. It is pure.
func RiskSummaryOf(l risk.Level) component.RiskSummary {
	w := levelWords[l.Band]
	s := component.RiskSummary{Band: riskBands[l.Band], Level: w.level, Gloss: w.gloss}
	if l.Forced {
		s.Gloss = LockoutGloss
	}
	for _, f := range l.Factors {
		s.Factors = append(s.Factors, f.Detail)
	}
	return s
}
