// Package risk is the main menu's risk level (refactor plan Phase 4,
// next/p4-risk): one weighted score over the status rows' reports and the
// header probe's cross-checks, with the factors that explain it.
// docs/engine/risk.md is the one place the scoring is documented: every
// weight, threshold and factor, and why.
//
// The score is the share of the rows' weight the host earns, in points of
// 100: a Configured row earns its weight, a Partial row half of it (none
// for a Partial line that protects nothing now), a NotConfigured or Unknown
// row nothing, and a NotApplicable row is left out of both the share and
// the whole. Each cross-check the host fails then costs points: a reboot
// pending, a long uptime without automatic updates, a second root
// account, a user account that becomes root without a password. A
// firewall that refuses SSH forces Critical, whatever the score. Unknown
// counts as not configured and adds a factor, and so does a row with no
// report: nothing is taken as hardened that was not established. Assess is
// pure: it reads no host and renders nothing; pkg/output maps a Level to
// the main menu's RiskSummary.
package risk

import (
	"fmt"
	"slices"
	"time"

	"github.com/abbott/hardn/pkg/engine"
	"github.com/abbott/hardn/pkg/engine/controls/apparmor"
	"github.com/abbott/hardn/pkg/engine/controls/firewall"
	"github.com/abbott/hardn/pkg/engine/controls/ssh"
	"github.com/abbott/hardn/pkg/engine/controls/updates"
	"github.com/abbott/hardn/pkg/engine/controls/users"
	"github.com/abbott/hardn/pkg/engine/header"
	"github.com/abbott/hardn/pkg/engine/steps"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/ufw"
)

// Band is a risk level: origin/main's five (pkg/security
// GetSecurityRiskLevel), the worst first. The zero value is Unknown: no
// row's state was established.
type Band uint8

// The bands.
const (
	Unknown Band = iota
	Critical
	High
	Moderate
	Low
	Minimal
)

var bandWords = [...]string{"unknown", "critical", "high", "moderate", "low", "minimal"}

// String returns the band's word, lower case; pkg/output holds the words
// the main menu shows.
func (b Band) String() string {
	if int(b) < len(bandWords) {
		return bandWords[b]
	}
	return fmt.Sprintf("band(%d)", uint8(b))
}

// The bands' lower bounds, in points of 100 (risk.md §5.4): below HighFrom
// is Critical; from HighFrom High; from ModerateFrom Moderate; from LowFrom
// Low; from MinimalFrom Minimal, when no factor is left to explain.
const (
	HighFrom     = 25
	ModerateFrom = 50
	LowFrom      = 75
	MinimalFrom  = 95
)

// The cross-checks' costs, in points of 100 (risk.md §5.3).
const (
	PenaltyReboot     = 10 // a reboot pending, or not established
	PenaltyUptime     = 10 // up LongUptime or more without automatic updates, or either not established
	PenaltySecondRoot = 15 // an account other than root has uid 0
	PenaltyNoPassword = 5  // a user account becomes root without a password
)

// LongUptime is the uptime from which a host that does not upgrade itself
// counts as running software with published fixes it lacks (risk.md
// §5.3).
const LongUptime = 30 * 24 * time.Hour

// CodeUptimeNoUpdates: the host has been up LongUptime or more and does
// not upgrade its packages by itself (on Proxmox VE, not Proxmox VE's
// own). pkg/output's registry holds it.
const CodeUptimeNoUpdates = "UPTIME_NO_AUTO_UPDATES"

// Row is one status row the score weighs.
type Row struct {
	ID, Title string // the control's
	Weight    int    // points of 100
}

// controls are the main menu's status rows in Figma's order, and weights
// their weights, together 100 (risk.md §5.1).
var (
	controls = []engine.Control{users.Admin{}, firewall.Incoming{}, ssh.Login{}, ssh.Auth{}, ssh.Port{}, apparmor.Enforce{}, updates.Auto{}}
	weights  = []int{10, 20, 15, 20, 5, 10, 20}
	rows     = func() []Row {
		out := make([]Row, len(controls))
		for i, c := range controls {
			out[i] = Row{ID: c.ID(), Title: c.Title(), Weight: weights[i]}
		}
		return out
	}()
)

// Rows returns the rows the score weighs, in Figma's order: User,
// Firewall, SSH Login, SSH Auth, SSH Port, AppArmor, Auto Updates.
func Rows() []Row { return slices.Clone(rows) }

// Controls returns the controls the score weighs, in Rows' order, for
// Engine.Check: its reports, and so the document's status[], then come
// in Figma's order too.
func Controls() []engine.Control { return slices.Clone(controls) }

// noCredit are the Partial lines, by control, that protect nothing now:
// a firewall that refuses only what a rule denies, AppArmor in complain
// mode, upgrades that run on package lists nothing refreshes. They earn
// nothing, as NotConfigured (risk.md §5.2).
var noCredit = map[string][]string{
	firewall.Incoming{}.ID(): {firewall.DetailAllowPolicy, firewall.DetailAllPorts, firewall.DetailAcceptPolicy},
	apparmor.ID:              {apparmor.DetailLoggingOnly},
	updates.ID:               {updates.DetailListsStale},
}

// codeAtStart is ufw's FindingAtStart as steps.UFWFinding codes it: ufw
// inactive, and its saved rules would refuse an SSH listener once it
// starts.
var codeAtStart = steps.FindingCode("UFW", ufw.FindingAtStart.String())

// Kind is what a factor is about. Factors come in this order.
type Kind uint8

// The factors' kinds.
const (
	KindLockout        Kind = iota // the firewall refuses SSH (FIREWALL_SSH_BLOCKED): Critical
	KindLockoutAtStart             // ufw would refuse SSH once it starts (UFW_SSH_NOT_ALLOWED_AT_START)
	KindUnknownRow                 // a row Unknown, or with no report
	KindSecondRoot                 // an account other than root has uid 0 (USERS_UID0)
	KindSSHUnchecked               // whether the firewall lets SSH through not established (FIREWALL_SSH_UNCHECKED)
	KindReboot                     // a reboot pending, or not established
	KindUptime                     // up long without automatic updates, or either not established
	KindNoPassword                 // a user account becomes root without a password (USERS_NOPASSWD)
)

var kindWords = [...]string{"lockout", "lockout-at-start", "unknown-row", "second-root", "ssh-unchecked", "reboot", "uptime", "no-password"}

// String returns the kind's word.
func (k Kind) String() string {
	if int(k) < len(kindWords) {
		return kindWords[k]
	}
	return fmt.Sprintf("kind(%d)", uint8(k))
}

// Factor is one line of the RiskSummary: what the rows below it do not
// show of why the level is what it is.
type Factor struct {
	Kind Kind
	// Control is the row it is about: the Unknown row, or the row whose
	// finding it is; "" for the header's cross-checks.
	Control string
	// Detail is the line the RiskSummary shows (risk.md §6, proposed).
	Detail string
	// Penalty is the points it cost; 0 when it costs none of its own (an
	// Unknown row has lost its weight already; the lockout forces the
	// band).
	Penalty int
}

// Level is the host's risk level.
type Level struct {
	Band Band
	// Forced is true when a firewall refusing SSH made the band Critical,
	// whatever the score.
	Forced bool
	// Score is the points the host earns of 100, after the penalties, at
	// least 0: the rows' share less each factor's Penalty.
	Score float64
	// Factors explain the level, in Kind order, a row's in Figma's.
	Factors []Factor
	// Findings are the cross-checks' own findings, which no report
	// carries: the header's on a pending reboot, and CodeUptimeNoUpdates.
	Findings []engine.Finding
}

// Assess scores the host from Engine.Check's reports and the header
// probe's h. A row's first report counts; a row with none counts as
// Unknown; a report for a control the score does not weigh is left out.
// The zero Header establishes nothing, so it costs both cross-checks.
func Assess(reports []engine.Report, h header.Header) Level {
	byID := map[string]engine.Report{}
	for _, r := range reports {
		if _, ok := byID[r.Control]; !ok {
			byID[r.Control] = r
		}
	}
	var l Level
	var unknown []Factor
	earned, whole := 0, 0 // in half points
	established := false
	for _, rw := range rows {
		r, ok := byID[rw.ID]
		switch state(r, ok) {
		case engine.NotApplicable:
			continue
		case engine.Configured:
			earned += 2 * rw.Weight
			established = true
		case engine.Partial:
			if !slices.Contains(noCredit[rw.ID], r.Status.Detail) {
				earned += rw.Weight
			}
			established = true
		case engine.NotConfigured:
			established = true
		default:
			unknown = append(unknown, Factor{Kind: KindUnknownRow, Control: rw.ID, Detail: rw.Title + " unknown"})
		}
		whole += 2 * rw.Weight
	}

	fw, fwOK := byID[firewall.Incoming{}.ID()]
	us, usOK := byID[users.Admin{}.ID()]
	add := func(f Factor) { l.Factors = append(l.Factors, f) }
	if fwOK && has(fw, firewall.CodeSSHBlocked) {
		l.Forced = true
		add(Factor{Kind: KindLockout, Control: fw.Control, Detail: "firewall blocks ssh"})
	}
	if fwOK && has(fw, codeAtStart) {
		add(Factor{Kind: KindLockoutAtStart, Control: fw.Control, Detail: "firewall blocks ssh after restart"})
	}
	l.Factors = append(l.Factors, unknown...)
	if usOK && has(us, users.CodeUID0) {
		add(Factor{Kind: KindSecondRoot, Control: us.Control, Detail: "second root account", Penalty: PenaltySecondRoot})
	}
	if fwOK && !l.Forced && has(fw, firewall.CodeSSHUnchecked) {
		add(Factor{Kind: KindSSHUnchecked, Control: fw.Control, Detail: "can't tell if firewall blocks ssh"})
	}
	if f, ok := reboot(h.Reboot); ok {
		add(f)
		if h.Reboot.Pending == detect.Yes {
			l.Findings = append(l.Findings, h.Reboot.Findings...)
		}
	}
	up, upOK := byID[updates.ID]
	if f, finding, ok := uptime(h, upgradesOf(up, upOK)); ok {
		add(f)
		if finding != nil {
			l.Findings = append(l.Findings, *finding)
		}
	}
	if usOK && has(us, users.CodeNoPassword) {
		add(Factor{Kind: KindNoPassword, Control: us.Control, Detail: "no password to become root", Penalty: PenaltyNoPassword})
	}

	penalty := 0
	for _, f := range l.Factors {
		penalty += f.Penalty
	}
	if whole > 0 {
		l.Score = max(0, 100*float64(earned)/float64(whole)-float64(penalty))
	}
	// from reports whether the score is at least t, compared in integers
	// so that a bound is never missed by rounding: 100*earned/whole -
	// penalty >= t.
	from := func(t int) bool { return 100*earned-penalty*whole >= t*whole }
	switch {
	case l.Forced:
		l.Band = Critical
	case !established:
		l.Band = Unknown
	case from(MinimalFrom) && len(l.Factors) == 0:
		l.Band = Minimal
	case from(LowFrom):
		l.Band = Low
	case from(ModerateFrom):
		l.Band = Moderate
	case from(HighFrom):
		l.Band = High
	default:
		l.Band = Critical
	}
	return l
}

// state is the state a row's report gives it: Unknown with no report, or
// with a check that failed (as Engine.Check reports one), and for a state
// outside the five.
func state(r engine.Report, ok bool) engine.State {
	switch {
	case !ok, r.Err != nil:
		return engine.Unknown
	case r.Status.State <= engine.NotApplicable:
		return r.Status.State
	}
	return engine.Unknown
}

// has reports whether r carries a finding with code.
func has(r engine.Report, code string) bool {
	return slices.ContainsFunc(r.Status.Findings, func(f engine.Finding) bool { return f.Code == code })
}

// reboot is the pending-reboot factor: Yes costs PenaltyReboot; so does
// Unknown, by the rule for Unknown, except inside a container, whose
// kernel is the host's (header.Reboot.HostKernel): there only a package's
// request counts.
func reboot(r header.Reboot) (Factor, bool) {
	switch {
	case r.Pending == detect.Yes:
		return Factor{Kind: KindReboot, Detail: "reboot pending", Penalty: PenaltyReboot}, true
	case r.Pending == detect.No, r.HostKernel:
		return Factor{}, false
	}
	return Factor{Kind: KindReboot, Detail: "can't tell if reboot pending", Penalty: PenaltyReboot}, true
}

// upgrades is whether the host upgrades its packages by itself, as the
// Auto Updates row says (check-updates.md §6).
type upgrades uint8

const (
	upgradesUnknown    upgrades = iota // the row Unknown, or no report: counts as no
	upgradesYes                        // Configured; Partial "off after restart", until the next boot
	upgradesNo                         // NotConfigured; Partial "lists not refreshed", or with a schedule or value not established
	upgradesNotProxmox                 // Proxmox VE: upgrades run, Proxmox VE's own packages not among them
	upgradesNA                         // NotApplicable: no cross-check
)

// upgradesOf reads the Auto Updates row r (ok false: no report).
func upgradesOf(r engine.Report, ok bool) upgrades {
	st := r.Status
	switch state(r, ok) {
	case engine.NotApplicable:
		return upgradesNA
	case engine.NotConfigured:
		return upgradesNo
	case engine.Partial:
		if st.Detail != updates.DetailOffAfterRestart || has(r, updates.CodeAptValueUnknown) || has(r, updates.CodeScheduleUnknown) {
			return upgradesNo
		}
	case engine.Configured:
	default:
		return upgradesUnknown
	}
	if has(r, updates.CodeProxmoxNotUpgraded) {
		return upgradesNotProxmox
	}
	return upgradesYes
}

// uptime is the long-uptime factor, for a host that does not upgrade
// itself (or whose upgrades are not established) and has been up
// LongUptime or more (or whose uptime is not established), with its
// finding when both are established. Inside a container it is not made:
// the uptime may be the host's (header.md §2.6).
func uptime(h header.Header, u upgrades) (Factor, *engine.Finding, bool) {
	if u == upgradesYes || u == upgradesNA || h.Reboot.HostKernel {
		return Factor{}, nil, false
	}
	f := Factor{Kind: KindUptime, Penalty: PenaltyUptime}
	if !h.Uptime.Known {
		f.Detail = "uptime unknown"
		return f, nil, true
	}
	if h.Uptime.Duration < LongUptime {
		return Factor{}, nil, false
	}
	days := int64(h.Uptime.Duration / (24 * time.Hour))
	switch u {
	case upgradesNo:
		f.Detail = fmt.Sprintf("up %d days, no auto updates", days)
	case upgradesNotProxmox:
		f.Detail = fmt.Sprintf("up %d days, Proxmox not upgraded", days)
	default:
		f.Detail = fmt.Sprintf("up %d days, auto updates unknown", days)
		return f, nil, true
	}
	return f, &engine.Finding{Code: CodeUptimeNoUpdates, Detail: f.Detail, Source: header.UptimeFile}, true
}
