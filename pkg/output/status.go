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

// BypassGloss is the gloss of a Critical CVE-2026-123158's bypass forced
// (risk.Level.Bypass), in place of "no security" or LockoutGloss: anyone
// can log in, which outranks a lockout (proposed wording,
// docs/engine/risk.md §6).
const BypassGloss = "password bypass"

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
// are the score's, in their order (risk.Rows: User, SSH Login, SSH Auth,
// SSH Port, Firewall, DNS, shown and not weighed, Auto Updates, AppArmor),
// whatever the reports' order; a row with no report shows
// Unknown, as the score counts it (DNS: Unknown, with no line, as it is
// not counted), and a report for a control that is no row comes after
// them, in its order. A control's first report counts, and a report with an error is
// Unknown, as risk.Assess has them. Each of l's factors is a line under
// the row its Control names, in l's order (risk.Kind's), status.warn for
// a cost established (the maintainer, 2026-10-21); the pending reboot,
// with no row, is the host block's (HeaderViewOf). The lines are the main
// menu's alone: no other screen's rows carry them. It is pure.
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
	at := map[string]int{} // a row's index, by control
	for _, rw := range risk.Rows() {
		at[rw.ID] = len(v.Status.Rows)
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
	for _, f := range l.Factors {
		if i, ok := at[f.Control]; ok {
			v.Status.Rows[i].Lines = append(v.Status.Rows[i].Lines, component.StatusLine{Text: f.Detail, Warn: f.Established})
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

// RiskSummaryOf is the risk level l as the main menu's RiskSummary, Figma's
// one line: the band's tone, its level word and gloss (BypassGloss when
// CVE-2026-123158's bypass forced it, else LockoutGloss when the lockout
// did). l's factors are drawn under the status rows
// (StatusViewOf) and in the host block (HeaderViewOf). It is pure.
func RiskSummaryOf(l risk.Level) component.RiskSummary {
	w := levelWords[l.Band]
	s := component.RiskSummary{Band: riskBands[l.Band], Level: w.level, Gloss: w.gloss}
	switch {
	case l.Bypass:
		s.Gloss = BypassGloss
	case l.Forced:
		s.Gloss = LockoutGloss
	}
	return s
}
