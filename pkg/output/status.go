package output

import (
	"slices"
	"strings"

	"github.com/abbott/hardn/pkg/engine"
	"github.com/abbott/hardn/pkg/engine/controls/ssh"
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
	// Info are the rows under Status, after a blank line: a control the
	// main menu shows only with a problem (DNS, NTP, Syslog), each drawn
	// only while it has one.
	Info component.StatusList
}

// The SSH rows the main menu draws as one.
var (
	loginID = ssh.Login{}.ID()
	authID  = ssh.Auth{}.ID()
)

// StatusViewOf maps Engine.Check's reports and their risk level l
// (risk.Assess over the same reports) to the main menu's blocks
// (docs/engine/gallery-menu-layout.md, adopted 2026-11-14). The rows are
// the score's, in their order (risk.Rows: User, SSH Login, SSH Port,
// Firewall, Kernel, Auto Updates, AppArmor), whatever the reports' order,
// with SSH Auth drawn in the SSH Login row (mergeSSH): one row for the
// screen that changes both, the two controls, their weights, findings and
// codes as they were. A row with no report shows Unknown, as the score
// counts it. A control's first report counts, and a report with an error
// is Unknown, as risk.Assess has them. Each of l's factors is a line under
// the row its Control names, SSH Auth's under SSH Login and a line the two
// share said once, in l's order (risk.Kind's), status.warn for a cost
// established (the maintainer, 2026-10-21); the pending reboot, with no row, is the host block's
// (HeaderViewOf). The main menu and SSH Login draw the CVE's established
// ones as their alert instead (AlertOf).
//
// A report for a control that is no row of the score (DNS, NTP, Syslog) is
// an Info row, in the reports' order, where it has a problem
// (engine.Status.Problem, or a check that failed): the host's value and
// the problem a line under it, or the problem alone where there is no
// value. One with no problem has no row. It is pure.
func StatusViewOf(reports []engine.Report, l risk.Level) StatusView {
	rows := risk.Rows()
	scored := map[string]bool{}
	for _, rw := range rows {
		scored[rw.ID] = true
	}
	first := map[string]engine.Report{}
	var v StatusView
	for _, r := range reports {
		if _, ok := first[r.Control]; ok {
			continue
		}
		first[r.Control] = r
		if row, ok := infoRow(r); ok && !scored[r.Control] {
			v.Info.Rows = append(v.Info.Rows, row)
		}
	}
	at := map[string]int{} // a row's index, by control
	for _, rw := range rows {
		if rw.ID == authID {
			if i, ok := at[loginID]; ok { // SSH Auth is drawn in SSH Login's row
				v.Status.Rows[i] = mergeSSH(v.Status.Rows[i], rowOf(rw, first))
				at[rw.ID] = i
				continue
			}
		}
		at[rw.ID] = len(v.Status.Rows)
		v.Status.Rows = append(v.Status.Rows, rowOf(rw, first))
	}
	for _, f := range l.Factors {
		if i, ok := at[f.Control]; ok {
			// SSH Login's and SSH Auth's share a row: a line both have
			// ("Counted as not configured") is said once.
			if line := (component.StatusLine{Text: f.Detail, Warn: f.Established}); !slices.Contains(v.Status.Rows[i].Lines, line) {
				v.Status.Rows[i].Lines = append(v.Status.Rows[i].Lines, line)
			}
		}
	}
	v.Risk = RiskSummaryOf(l)
	return v
}

// ReportRows are the reports as the report to hand over lists them, one
// row a control: the score's rows in their order (risk.Rows), SSH Login
// and SSH Auth each its own, a row with no report Unknown; then every
// other control's report, in the reports' order. No row has a line. It is
// pure.
func ReportRows(reports []engine.Report) []component.StatusRow {
	first := map[string]engine.Report{}
	var others []engine.Report
	for _, r := range reports {
		if _, ok := first[r.Control]; !ok {
			first[r.Control] = r
			others = append(others, r)
		}
	}
	var out []component.StatusRow
	for _, rw := range risk.Rows() {
		out = append(out, rowOf(rw, first))
		others = slices.DeleteFunc(others, func(r engine.Report) bool { return r.Control == rw.ID })
	}
	for _, r := range others {
		out = append(out, statusRow(r))
	}
	return out
}

// rowOf is the row of the score's rw: its first report's, or Unknown with
// none.
func rowOf(rw risk.Row, first map[string]engine.Report) component.StatusRow {
	if r, ok := first[rw.ID]; ok {
		return statusRow(r)
	}
	return component.StatusRow{Label: rw.Title, State: component.StatusUnknown}
}

// worse are the states from the worst, as a merged row shows them: one
// part off, then one partly on, then one not established. A part hardn
// read as off outranks one it could not read, so that what it did
// establish is said; the part not read is the line under the row
// ("Counted as not configured"). Not Applicable is no worse than any: a
// part that does not apply says nothing of the other.
var worse = []component.StatusState{component.StatusNotConfigured, component.StatusPartial, component.StatusUnknown,
	component.StatusConfigured, component.StatusNotApplicable}

// proxmoxNote ends SSH Login's Configured detail on Proxmox VE
// (ssh.DetailProxmoxKeyOnly). The merged pair leaves it out, to fit the
// row's 35 cells after "+": "root key-only, key-only auth".
const proxmoxNote = " (Proxmox)"

// mergeSSH is the SSH Login row with SSH Auth's in it (the maintainer,
// 2026-11-14; gallery-menu-layout.md, decisions 14 (a) and 16 (a)): SSH
// Login's label, the worse of the two states, and the details of the parts
// in that state, SSH Login's first: both where both are Configured ("root
// disallowed, key-only auth"), else what is off alone ("password auth
// on"), so that the state's word and the detail are about the same part
// and a row with one part off fits its 37 cells. A part less bad than the
// row is not said: the SSH Login screen says it. Two parts that say the
// same say it once.
func mergeSSH(login, auth component.StatusRow) component.StatusRow {
	out := login
	for _, s := range worse {
		if login.State == s || auth.State == s {
			out.State = s
			break
		}
	}
	var details []string
	for _, r := range []component.StatusRow{login, auth} {
		if r.State == out.State && r.Detail != "" && !slices.Contains(details, r.Detail) {
			details = append(details, r.Detail)
		}
	}
	if len(details) > 1 {
		details[0] = strings.TrimSuffix(details[0], proxmoxNote)
	}
	out.Detail = strings.Join(details, ", ")
	return out
}

// cantRead is an Info row's problem where the check failed and its status
// names none.
const cantRead = "Can't read"

// infoRow is report r's Info row, and whether it has one: a problem
// (engine.Status.Problem), or a check that failed. The host's value is
// the row's, the problem a line under it in the warning colour; with no
// value the problem is the row's value.
func infoRow(r engine.Report) (component.StatusRow, bool) {
	problem := r.Status.Problem
	if problem == "" && r.Err != nil {
		problem = cantRead
	}
	if problem == "" {
		return component.StatusRow{}, false
	}
	if r.Status.Value == "" {
		return component.StatusRow{Label: r.Title, Info: true, Detail: problem, Problem: true}, true
	}
	return component.StatusRow{Label: r.Title, Info: true, Detail: r.Status.Value,
		Lines: []component.StatusLine{{Text: problem, Warn: true}}}, true
}

// StatusRowOf is report r's row: its title, its state's glyph and word,
// its detail; Unknown when the check failed. A screen draws its own
// control's row with it (pkg/screens/screenkit). It is pure.
func StatusRowOf(r engine.Report) component.StatusRow { return statusRow(r) }

// statusRow is StatusRowOf.
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
