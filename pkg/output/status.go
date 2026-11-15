package output

import (
	"fmt"
	"slices"
	"strings"

	"github.com/abbott/hardn/pkg/engine"
	"github.com/abbott/hardn/pkg/engine/controls/ssh"
	"github.com/abbott/hardn/pkg/engine/risk"
	"github.com/abbott/hardn/pkg/ui/component"
	"github.com/abbott/hardn/pkg/ui/screen"
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
	portID  = ssh.Port{}.ID()
)

// StatusViewOf maps Engine.Check's reports and their risk level l
// (risk.Assess over the same reports) to the main menu's blocks
// (docs/engine/gallery-menu-layout.md, adopted 2026-11-14). The rows are
// the score's, in their order (risk.Rows: User, SSH, Firewall, Kernel,
// Auto Updates, AppArmor), whatever the reports' order, with SSH Login,
// SSH Auth and SSH Port drawn as the one SSH row (mergeSSH; the
// maintainer, 2026-11-15): one row for the screen that changes the three,
// the controls, their weights, findings and codes as they were. A row
// with no report shows Unknown, as the score counts it. A control's first
// report counts, and a report with an error is Unknown, as risk.Assess has
// them. Each of l's factors is a line under the row its Control names, the
// three SSH controls' under SSH, after the row's own line, and a line they
// share said once, in l's order (risk.Kind's), status.warn for a cost
// established (the maintainer, 2026-10-21); the pending reboot, with no row, is the host block's
// (HeaderViewOf). The main menu and the SSH screen draw the CVE's established
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
		i, merged := at[loginID]
		switch {
		case rw.ID == loginID:
			part := func(id string) component.StatusRow { return rowOf(risk.Row{ID: id}, first) }
			at[rw.ID] = len(v.Status.Rows)
			v.Status.Rows = append(v.Status.Rows, mergeSSH(part(loginID), part(authID), part(portID)))
		case merged && (rw.ID == authID || rw.ID == portID): // drawn in the SSH row
			at[rw.ID] = i
		default:
			at[rw.ID] = len(v.Status.Rows)
			v.Status.Rows = append(v.Status.Rows, rowOf(rw, first))
		}
	}
	for _, f := range l.Factors {
		if i, ok := at[f.Control]; ok {
			// The SSH controls share a row: a line more than one has
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
// row a control: the score's rows in their order (risk.Rows), SSH Login,
// SSH Auth and SSH Port each its own, a row with no report Unknown; then every
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

// SSHRow is the label of the main menu's one SSH row, and of the SSH
// screen's (the maintainer, 2026-11-15).
const SSHRow = screen.SSHTitle

// The SSH row's words (the maintainer, 2026-11-15;
// gallery-menu-layout.md §7). sshRowWords are a Configured part's, by the
// control's detail: "key+password, no root, port 2230" is his text. On
// Proxmox VE root logs in by key, so the row does not say "no root",
// and "(Proxmox)" is left out to fit the row's 35 cells after "+"
// (proposed). A detail not listed is said as it is.
var sshRowWords = map[string]string{
	ssh.DetailKeyOnly:        "key-only",
	ssh.DetailKeyAndPassword: "key+password",
	ssh.DetailRootDisallowed: "no root",
	ssh.DetailProxmoxKeyOnly: "root key-only",
}

const (
	sshPort        = "port "             // then the port: "port 2230"
	sshPorts       = "ports "            // then several: "ports 22 and 2230", "ports 2230, 2231, not 22"
	sshNPorts      = "%d ports"          // several off 22, Configured: "2 ports", to fit the 35 cells
	sshDefaultPort = "default port (22)" // his text
	sshNot22       = ", not 22"          // Proxmox VE, off its port: "port 2230, not 22"
	sshPortUnknown = "port: "            // then why the port is not read: "port: can't read"
)

// portWords are the SSH Port row p's words in the SSH row: "port 2230"
// off 22, "2 ports" for several, which the SSH screen's section lists,
// "default port (22)" on 22,
// "ports 22 and 2230" on both. Proxmox VE's baseline is 22: Configured
// there is "port 22", and off it "port 2230, not 22". Any other detail is
// said as it is ("other ports configured").
func portWords(p component.StatusRow) string {
	ok := p.State == component.StatusConfigured
	others, off22 := ssh.OtherPorts(p.Detail)
	lead, n := sshPort, strings.Count(others, ",")+1
	if n > 1 {
		lead = sshPorts
	}
	switch {
	case p.Detail == ssh.DetailDefaultPort && ok:
		return sshPort + "22"
	case p.Detail == ssh.DetailDefaultPort:
		return sshDefaultPort
	case off22 && ok && n > 1:
		return fmt.Sprintf(sshNPorts, n)
	case off22 && ok:
		return lead + others
	case off22:
		return lead + others + sshNot22
	case strings.HasPrefix(p.Detail, ssh.DetailWith22):
		return sshPorts + p.Detail
	}
	return p.Detail
}

// mergeSSH is the main menu's SSH row: SSH Login's, SSH Auth's and SSH
// Port's rows as one (the maintainer, 2026-11-15; gallery-menu-layout.md
// §7), for display alone.
//
// Login and auth first, as v0.10.1 merged them (2026-11-14, decisions 14
// (a) and 16 (a)): the worse of the two states (worse), and the details of
// the parts in that state, SSH Login's first, else what is off alone
// ("password auth on"), so that the state's word and the detail are about
// the same part. Two parts that say the same say it once.
//
// Then the port. Where that pair is Configured: with the port Configured
// too, the three parts in the row's words, auth, login, port ("key-only,
// no root, port 2230"); with the port on 22, or on 22 and another,
// the row is Partial and says the port alone ("default port (22)"), never
// Not Configured for the port's sake; with the port not read it is Unknown
// ("port: can't read"). Where the pair is not Configured the row is the
// pair's, and a port on 22 is a line under it in the warning colour
// ("Default port (22)"); a port off 22, or not read, adds nothing there:
// the level's factor says a part not read. On Proxmox VE, whose port is
// 22, the line is for a port off it ("Port 2230, not 22"). The row is
// Configured for the three Configured alone: with a part Not Applicable
// (no SSH server, read between two checks) and none off or not read, it
// is Not Applicable, "no SSH server".
func mergeSSH(login, auth, port component.StatusRow) component.StatusRow {
	out := login
	out.Label = SSHRow
	for _, s := range worse {
		if login.State == s || auth.State == s {
			out.State = s
			break
		}
	}
	// "+" is for three parts Configured, no fewer: a part Not Applicable
	// beside one Configured (sshd gone between two reads) makes the row Not
	// Applicable, in that part's words.
	if na := component.StatusNotApplicable; out.State == component.StatusConfigured && (login.State == na || auth.State == na) {
		out.State = na
	}
	configured := out.State == component.StatusConfigured
	var details []string
	say := func(d string) {
		if d != "" && !slices.Contains(details, d) {
			details = append(details, d)
		}
	}
	parts := []component.StatusRow{login, auth}
	if configured {
		parts = []component.StatusRow{auth, login}
	}
	for _, r := range parts {
		if d := r.Detail; r.State == out.State {
			if w, ok := sshRowWords[d]; ok && configured {
				d = w
			}
			say(d)
		}
	}
	switch off := port.State == component.StatusNotConfigured || port.State == component.StatusPartial; {
	case !configured:
		if off && out.State != component.StatusNotApplicable {
			out.Lines = append(out.Lines, component.StatusLine{Text: upperFirst(portWords(port)), Warn: true})
		}
	case port.State == component.StatusConfigured:
		out.State = component.StatusConfigured
		say(portWords(port))
	case off:
		out.State, details = component.StatusPartial, []string{portWords(port)}
	case port.State == component.StatusUnknown:
		out.State, details = component.StatusUnknown, nil
		if port.Detail != "" {
			details = []string{sshPortUnknown + port.Detail}
		}
	default: // Not Applicable: the row is never Configured without the port
		out.State, details = port.State, nil
		say(port.Detail)
	}
	out.Detail = strings.Join(details, ", ")
	return out
}

// upperFirst is s with its first letter a capital, as a line under a row
// starts.
func upperFirst(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
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
