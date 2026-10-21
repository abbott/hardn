package risk_test

import (
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/abbott/hardn/pkg/engine"
	"github.com/abbott/hardn/pkg/engine/controls/apparmor"
	"github.com/abbott/hardn/pkg/engine/controls/firewall"
	"github.com/abbott/hardn/pkg/engine/controls/ssh"
	"github.com/abbott/hardn/pkg/engine/controls/updates"
	"github.com/abbott/hardn/pkg/engine/controls/users"
	"github.com/abbott/hardn/pkg/engine/header"
	"github.com/abbott/hardn/pkg/engine/risk"
	"github.com/abbott/hardn/pkg/host/detect"
)

// The rows' control IDs.
const (
	idUser   = "users.admin"
	idFW     = "firewall.incoming"
	idLogin  = "ssh.login"
	idAuth   = "ssh.auth"
	idPort   = "ssh.port"
	idAA     = apparmor.ID
	idUpdate = updates.ID
)

const day = 24 * time.Hour

// figmaUptime is Figma's 00-TUI-MAIN uptime: 182 days, 2 hours, 26
// minutes.
const figmaUptime = 182*day + 2*time.Hour + 26*time.Minute

// rep is a report of control id in state s with detail d and a finding
// per code.
func rep(id string, s engine.State, d string, codes ...string) engine.Report {
	r := engine.Report{Control: id, Status: engine.Status{State: s, Detail: d}}
	for _, c := range codes {
		r.Status.Findings = append(r.Status.Findings, engine.Finding{Code: c, Detail: "synthetic"})
	}
	return r
}

// hardened is every row Configured, with Figma's details.
func hardened() []engine.Report {
	return []engine.Report{
		rep(idUser, engine.Configured, "non-root, sudo"),
		rep(idFW, engine.Configured, "deny policy"),
		rep(idLogin, engine.Configured, "root disallowed"),
		rep(idAuth, engine.Configured, "key-only auth"),
		rep(idPort, engine.Configured, "non-default (2230)"),
		rep(idAA, engine.Configured, "34 profiles enforced"),
		rep(idUpdate, engine.Configured, updates.DetailUnattended),
	}
}

// with is hardened's reports with each of rs in place of its control's.
func with(rs ...engine.Report) []engine.Report {
	out := hardened()
	for _, r := range rs {
		i := slices.IndexFunc(out, func(x engine.Report) bool { return x.Control == r.Control })
		out[i] = r
	}
	return out
}

// without is hardened's reports less the controls ids.
func without(ids ...string) []engine.Report {
	return slices.DeleteFunc(hardened(), func(r engine.Report) bool { return slices.Contains(ids, r.Control) })
}

// figma is Figma's host: User, Firewall and the SSH rows Configured,
// AppArmor and Auto Updates Not Configured.
func figma() []engine.Report {
	return with(rep(idAA, engine.NotConfigured, apparmor.DetailNoProfiles), rep(idUpdate, engine.NotConfigured, updates.DetailNoUnattended))
}

// uptimeOf is a header up d, no reboot pending.
func uptimeOf(d time.Duration) header.Header {
	return header.Header{Uptime: header.Uptime{Duration: d, Known: true}, Reboot: header.Reboot{Pending: detect.No}}
}

// quiet is a header up 3 days, no reboot pending.
var quiet = uptimeOf(3 * day)

// inContainer is quiet inside a container: the kernel is the host's.
func inContainer(p detect.Tri) header.Header {
	h := quiet
	h.Reboot = header.Reboot{Pending: p, HostKernel: true}
	return h
}

func pending(p detect.Tri, fs ...engine.Finding) header.Header {
	h := quiet
	h.Reboot = header.Reboot{Pending: p, Findings: fs}
	return h
}

var newerKernel = engine.Finding{Code: header.CodeNewerKernel, Detail: header.DetailNewerKernel, Source: "/boot/vmlinuz-6.12.48+deb13-amd64"}

type want struct {
	band    risk.Band
	score   float64
	factors []string
	forced  bool
}

// TestAssess: every rule of risk.md §5, each band, NotApplicable and
// Unknown.
func TestAssess(t *testing.T) {
	cases := []struct {
		name    string
		reports []engine.Report
		h       header.Header
		want    want
	}{
		// The bands, each reachable.
		{"hardened: the top band", hardened(), quiet, want{band: risk.Minimal, score: 100}},
		{"figma's host", figma(), uptimeOf(figmaUptime), want{band: risk.Moderate, score: 60, factors: []string{"up 182 days, no auto updates"}}},
		{"figma's host, up 3 days", figma(), quiet, want{band: risk.Moderate, score: 70}},
		{"AppArmor not configured", with(rep(idAA, engine.NotConfigured, apparmor.DetailNoProfiles)), quiet, want{band: risk.Low, score: 90}},
		{"weak: the SSH auth, firewall, AppArmor and updates off", with(rep(idAuth, engine.NotConfigured, "password auth on"),
			rep(idFW, engine.NotConfigured, "inactive"), rep(idAA, engine.NotConfigured, apparmor.DetailOffInKernel),
			rep(idUpdate, engine.NotConfigured, updates.DetailUpgradesOff)), quiet, want{band: risk.High, score: 30}},
		{"nothing configured", []engine.Report{rep(idUser, engine.NotConfigured, "root only"), rep(idFW, engine.NotConfigured, "inactive"),
			rep(idLogin, engine.NotConfigured, "root allowed"), rep(idAuth, engine.NotConfigured, "password auth on"),
			rep(idPort, engine.NotConfigured, "default (22)"), rep(idAA, engine.NotConfigured, apparmor.DetailNoProfiles),
			rep(idUpdate, engine.NotConfigured, updates.DetailNoUnattended)}, quiet, want{band: risk.Critical, score: 0}},
		{"no row established", nil, quiet, want{band: risk.Unknown, score: 0, factors: []string{"User unknown", "Firewall unknown",
			"SSH Login unknown", "SSH Auth unknown", "SSH Port unknown", "AppArmor unknown", "Auto Updates unknown"}}},

		{"a Proxmox VE node (risk.md §5.5)", []engine.Report{rep(idUser, engine.NotConfigured, "root only"), rep(idFW, engine.NotConfigured, "disabled"),
			rep(idLogin, engine.Configured, "root key-only (Proxmox)"), rep(idAuth, engine.Configured, "key-only auth"),
			rep(idPort, engine.Configured, "default (22)"), rep(idAA, engine.Configured, "12 profiles enforced"),
			rep(idUpdate, engine.NotConfigured, updates.DetailNoUnattended)}, uptimeOf(41*day + time.Hour),
			want{band: risk.High, score: 40, factors: []string{"up 41 days, no auto updates"}}},

		// The bounds.
		{"25 is High", with(rep(idFW, engine.NotConfigured, "inactive"), rep(idAuth, engine.NotConfigured, "password auth on"),
			rep(idPort, engine.NotConfigured, "default (22)"), rep(idAA, engine.NotConfigured, apparmor.DetailNoProfiles),
			rep(idUpdate, engine.NotConfigured, updates.DetailUpgradesOff)), quiet, want{band: risk.High, score: 25}},
		{"50 is Moderate", with(rep(idLogin, engine.NotConfigured, "root allowed"), rep(idPort, engine.NotConfigured, "default (22)"),
			rep(idAA, engine.NotConfigured, apparmor.DetailNoProfiles), rep(idUpdate, engine.NotConfigured, updates.DetailUpgradesOff)),
			quiet, want{band: risk.Moderate, score: 50}},
		{"75 is Low", with(rep(idPort, engine.NotConfigured, "default (22)"), rep(idUpdate, engine.NotConfigured, updates.DetailUpgradesOff)),
			quiet, want{band: risk.Low, score: 75}},
		{"95 is Minimal: the port weighs least", with(rep(idPort, engine.NotConfigured, "default (22)")), quiet, want{band: risk.Minimal, score: 95}},
		{"below 95 is Low", with(rep(idLogin, engine.Partial, "root key-only")), quiet, want{band: risk.Low, score: 92.5}},

		// The port weighs less than the auth settings.
		{"port vs login", with(rep(idLogin, engine.NotConfigured, "root allowed")), quiet, want{band: risk.Low, score: 85}},
		{"port vs auth", with(rep(idAuth, engine.NotConfigured, "password auth on")), quiet, want{band: risk.Low, score: 80}},

		// Partial earns half; a Partial that protects nothing now, none.
		{"firewall off after restart: half", with(rep(idFW, engine.Partial, "off after restart")), quiet, want{band: risk.Low, score: 90}},
		{"firewall allow policy: none", with(rep(idFW, engine.Partial, firewall.DetailAllowPolicy)), quiet, want{band: risk.Low, score: 80}},
		{"firewall all ports: none", with(rep(idFW, engine.Partial, firewall.DetailAllPorts)), quiet, want{band: risk.Low, score: 80}},
		{"pve-firewall accept policy: none", with(rep(idFW, engine.Partial, firewall.DetailAcceptPolicy)), quiet, want{band: risk.Low, score: 80}},
		{"AppArmor logging only: none", with(rep(idAA, engine.Partial, apparmor.DetailLoggingOnly)), quiet, want{band: risk.Low, score: 90}},
		{"AppArmor off after restart: half", with(rep(idAA, engine.Partial, apparmor.DetailOffAfterRestart)), quiet, want{band: risk.Minimal, score: 95}},
		{"updates lists not refreshed: none", with(rep(idUpdate, engine.Partial, updates.DetailListsStale)), quiet, want{band: risk.Low, score: 80}},
		{"updates off after restart: half", with(rep(idUpdate, engine.Partial, updates.DetailOffAfterRestart)), quiet, want{band: risk.Low, score: 90}},
		{"user without sudo: half", with(rep(idUser, engine.Partial, "non-root, no sudo")), quiet, want{band: risk.Minimal, score: 95}},

		// NotApplicable is left out of the share and the whole.
		{"AppArmor in a container", with(rep(idAA, engine.NotApplicable, apparmor.DetailByHost)), inContainer(detect.No),
			want{band: risk.Minimal, score: 100}},
		{"Alpine without AppArmor, port 22", with(rep(idAA, engine.NotApplicable, apparmor.DetailAlpineDefault), rep(idPort, engine.NotConfigured, "default (22)")),
			quiet, want{band: risk.Low, score: 100 * 85.0 / 90}},
		{"every row not applicable", []engine.Report{rep(idUser, engine.NotApplicable, "x"), rep(idFW, engine.NotApplicable, "x"),
			rep(idLogin, engine.NotApplicable, "x"), rep(idAuth, engine.NotApplicable, "x"), rep(idPort, engine.NotApplicable, "x"),
			rep(idAA, engine.NotApplicable, "x"), rep(idUpdate, engine.NotApplicable, "x")}, quiet, want{band: risk.Unknown}},

		// Unknown counts as not configured, and adds a factor.
		{"port unknown", with(rep(idPort, engine.Unknown, "can't read")), quiet, want{band: risk.Low, score: 95, factors: []string{"SSH Port unknown"}}},
		{"firewall unknown", with(rep(idFW, engine.Unknown, "can't tell ssh ports")), quiet, want{band: risk.Low, score: 80, factors: []string{"Firewall unknown"}}},
		{"a failed check is unknown", with(engine.Report{Control: idAuth, Status: engine.Status{State: engine.Configured}, Err: errors.New("sshd -T")}),
			quiet, want{band: risk.Low, score: 80, factors: []string{"SSH Auth unknown"}}},
		{"a state outside the five is unknown", with(rep(idLogin, engine.State(9), "")), quiet,
			want{band: risk.Low, score: 85, factors: []string{"SSH Login unknown"}}},
		{"a row with no report is unknown", without(idUser), quiet, want{band: risk.Low, score: 90, factors: []string{"User unknown"}}},
		{"unknown rows in Figma's order", with(rep(idUpdate, engine.Unknown, "can't read"), rep(idUser, engine.Unknown, "can't read")), quiet,
			want{band: risk.Moderate, score: 70, factors: []string{"User unknown", "Auto Updates unknown"}}},

		// The lockout forces Critical, once.
		{"firewall blocks ssh", with(rep(idFW, engine.Partial, "ssh blocked", firewall.CodeSSHBlocked)), quiet,
			want{band: risk.Critical, score: 90, forced: true, factors: []string{"firewall blocks ssh"}}},
		{"firewall blocks ssh, two findings", with(rep(idFW, engine.Partial, "ssh blocked", firewall.CodeSSHBlocked, firewall.CodeSSHBlocked)), quiet,
			want{band: risk.Critical, score: 90, forced: true, factors: []string{"firewall blocks ssh"}}},
		{"firewall blocks ssh, the check failed", with(engine.Report{Control: idFW, Err: errors.New("ss"),
			Status: engine.Status{State: engine.Unknown, Findings: []engine.Finding{{Code: firewall.CodeSSHBlocked}}}}), quiet,
			want{band: risk.Critical, score: 80, forced: true, factors: []string{"firewall blocks ssh", "Firewall unknown"}}},
		{"blocked beats unchecked", with(rep(idFW, engine.Partial, "ssh blocked", firewall.CodeSSHBlocked, firewall.CodeSSHUnchecked)), quiet,
			want{band: risk.Critical, score: 90, forced: true, factors: []string{"firewall blocks ssh"}}},
		{"ufw would block ssh at start", with(rep(idFW, engine.NotConfigured, "enabled, not running", "UFW_SSH_NOT_ALLOWED_AT_START")), quiet,
			want{band: risk.Low, score: 80, factors: []string{"firewall blocks ssh after restart"}}},
		{"ssh through the firewall unchecked", with(rep(idFW, engine.Partial, "off after restart", firewall.CodeSSHUnchecked)), quiet,
			want{band: risk.Low, score: 90, factors: []string{"can't tell if firewall blocks ssh"}}},

		// The users' exposure.
		{"second root account", with(rep(idUser, engine.Configured, "non-root, sudo", users.CodeUID0)), quiet,
			want{band: risk.Low, score: 85, factors: []string{"second root account"}}},
		{"no password to root: keeps the top band out", with(rep(idUser, engine.Configured, "non-root, sudo", users.CodeNoPassword, users.CodeNoPassword)), quiet,
			want{band: risk.Low, score: 95, factors: []string{"no password to become root"}}},

		// The pending reboot.
		{"reboot pending", hardened(), pending(detect.Yes, newerKernel), want{band: risk.Low, score: 90, factors: []string{"reboot pending"}}},
		{"reboot not established", hardened(), pending(detect.Unknown), want{band: risk.Low, score: 90, factors: []string{"can't tell if reboot pending"}}},
		{"container: a package's request counts", with(rep(idAA, engine.NotApplicable, apparmor.DetailByHost)), inContainer(detect.Yes),
			want{band: risk.Low, score: 90, factors: []string{"reboot pending"}}},
		{"container: no source is not counted", with(rep(idAA, engine.NotApplicable, apparmor.DetailByHost)), inContainer(detect.Unknown),
			want{band: risk.Minimal, score: 100}},
		{"the zero header establishes nothing", hardened(), header.Header{}, want{band: risk.Low, score: 90, factors: []string{"can't tell if reboot pending"}}},
		{"the zero header, no auto updates", figma(), header.Header{}, want{band: risk.Moderate, score: 50,
			factors: []string{"can't tell if reboot pending", "uptime unknown"}}},

		// The long uptime without automatic updates.
		{"up 30 days, no updates", with(rep(idUpdate, engine.NotConfigured, updates.DetailTimerOff)), uptimeOf(30 * day),
			want{band: risk.Moderate, score: 70, factors: []string{"up 30 days, no auto updates"}}},
		{"just under 30 days", with(rep(idUpdate, engine.NotConfigured, updates.DetailTimerOff)), uptimeOf(30*day - time.Minute),
			want{band: risk.Low, score: 80}},
		{"up long, updates configured", hardened(), uptimeOf(figmaUptime), want{band: risk.Minimal, score: 100}},
		{"up long, updates off after restart", with(rep(idUpdate, engine.Partial, updates.DetailOffAfterRestart)), uptimeOf(figmaUptime),
			want{band: risk.Low, score: 90}},
		{"up long, off after restart with a schedule not established", with(rep(idUpdate, engine.Partial, updates.DetailOffAfterRestart, updates.CodeScheduleUnknown)),
			uptimeOf(figmaUptime), want{band: risk.Low, score: 80, factors: []string{"up 182 days, no auto updates"}}},
		{"up long, off after restart with a value not ranked", with(rep(idUpdate, engine.Partial, updates.DetailOffAfterRestart, updates.CodeAptValueUnknown)),
			uptimeOf(figmaUptime), want{band: risk.Low, score: 80, factors: []string{"up 182 days, no auto updates"}}},
		{"up long, lists not refreshed", with(rep(idUpdate, engine.Partial, updates.DetailListsStale)), uptimeOf(figmaUptime),
			want{band: risk.Moderate, score: 70, factors: []string{"up 182 days, no auto updates"}}},
		{"up long, updates unknown", with(rep(idUpdate, engine.Unknown, updates.DetailCantRead)), uptimeOf(figmaUptime),
			want{band: risk.Moderate, score: 70, factors: []string{"Auto Updates unknown", "up 182 days, auto updates unknown"}}},
		{"up long, no updates report", without(idUpdate), uptimeOf(figmaUptime),
			want{band: risk.Moderate, score: 70, factors: []string{"Auto Updates unknown", "up 182 days, auto updates unknown"}}},
		{"up long, Proxmox not upgraded", with(rep(idUpdate, engine.Configured, updates.DetailUnattended, updates.CodeProxmoxNotUpgraded)),
			uptimeOf(figmaUptime), want{band: risk.Low, score: 90, factors: []string{"up 182 days, Proxmox not upgraded"}}},
		{"up long, updates not applicable", with(rep(idUpdate, engine.NotApplicable, "x")), uptimeOf(figmaUptime), want{band: risk.Minimal, score: 100}},
		{"up long in a container", with(rep(idAA, engine.NotApplicable, apparmor.DetailByHost), rep(idUpdate, engine.NotConfigured, updates.DetailNoUnattended)),
			func() header.Header { h := inContainer(detect.No); h.Uptime.Duration = figmaUptime; return h }(),
			want{band: risk.Low, score: 100 * 70.0 / 90}},
		{"uptime not read, updates on", hardened(), pending(detect.No), want{band: risk.Minimal, score: 100}},

		// The penalties add up, and the score stays at 0 or more.
		{"every penalty", []engine.Report{rep(idUser, engine.NotConfigured, "root only", users.CodeUID0)}, header.Header{Reboot: header.Reboot{Pending: detect.Yes}},
			want{band: risk.Critical, score: 0, factors: []string{"Firewall unknown", "SSH Login unknown", "SSH Auth unknown", "SSH Port unknown",
				"AppArmor unknown", "Auto Updates unknown", "second root account", "reboot pending", "uptime unknown"}}},

		// What the score does not weigh.
		{"a control the score does not weigh", append(hardened(), rep("dns.servers", engine.NotConfigured, "x")), quiet, want{band: risk.Minimal, score: 100}},
		{"a row's first report counts", append(hardened(), rep(idFW, engine.NotConfigured, "inactive")), quiet, want{band: risk.Minimal, score: 100}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			l := risk.Assess(c.reports, c.h)
			var factors []string
			for _, f := range l.Factors {
				factors = append(factors, f.Detail)
			}
			if l.Band != c.want.band || l.Forced != c.want.forced || !near(l.Score, c.want.score) || !slices.Equal(factors, c.want.factors) {
				t.Errorf("got  %v forced %v score %v factors %q\nwant %v forced %v score %v factors %q",
					l.Band, l.Forced, l.Score, factors, c.want.band, c.want.forced, c.want.score, c.want.factors)
			}
		})
	}
}

func near(a, b float64) bool { return a-b < 1e-9 && b-a < 1e-9 }

// TestFigma: Figma's 00-TUI-MAIN host is Moderate ("Moderate ≈ medium
// security"), with the long uptime its one factor and finding.
func TestFigma(t *testing.T) {
	l := risk.Assess(figma(), uptimeOf(figmaUptime))
	if l.Band != risk.Moderate {
		t.Fatalf("band %v, want moderate", l.Band)
	}
	want := []engine.Finding{{Code: risk.CodeUptimeNoUpdates, Detail: "up 182 days, no auto updates", Source: "/proc/uptime"}}
	if !slices.Equal(l.Findings, want) {
		t.Errorf("findings %+v, want %+v", l.Findings, want)
	}
	if f := l.Factors[0]; f.Kind != risk.KindUptime || f.Penalty != risk.PenaltyUptime || f.Control != "" {
		t.Errorf("factor %+v", f)
	}
}

// TestFindings: the cross-checks' findings, and only theirs: the header's
// on a reboot pending, the uptime's once established; none for what is
// not established, nor the reports' own.
func TestFindings(t *testing.T) {
	uptime := engine.Finding{Code: risk.CodeUptimeNoUpdates, Detail: "up 182 days, no auto updates", Source: header.UptimeFile}
	long := uptimeOf(figmaUptime)
	long.Reboot = header.Reboot{Pending: detect.Yes, Findings: []engine.Finding{newerKernel}}
	for _, c := range []struct {
		name    string
		reports []engine.Report
		h       header.Header
		want    []engine.Finding
	}{
		{"reboot and uptime", figma(), long, []engine.Finding{newerKernel, uptime}},
		{"reboot not established", hardened(), pending(detect.Unknown, newerKernel), nil},
		{"updates not established", with(rep(idUpdate, engine.Unknown, updates.DetailCantRead)), uptimeOf(figmaUptime), nil},
		{"uptime not established", figma(), pending(detect.No), nil},
		{"the lockout is the report's", with(rep(idFW, engine.Partial, "ssh blocked", firewall.CodeSSHBlocked)), quiet, nil},
	} {
		if got := risk.Assess(c.reports, c.h).Findings; !slices.Equal(got, c.want) {
			t.Errorf("%s: %+v, want %+v", c.name, got, c.want)
		}
	}
}

// TestFactors: each factor's kind, row and cost, in Kind order.
func TestFactors(t *testing.T) {
	h := uptimeOf(figmaUptime)
	h.Reboot.Pending = detect.Yes
	l := risk.Assess([]engine.Report{
		rep(idUser, engine.Configured, "non-root, sudo", users.CodeNoPassword, users.CodeUID0),
		rep(idFW, engine.NotConfigured, "enabled, not running", "UFW_SSH_NOT_ALLOWED_AT_START", firewall.CodeSSHUnchecked),
		rep(idLogin, engine.Configured, "root disallowed"), rep(idAuth, engine.Configured, "key-only auth"),
		rep(idPort, engine.Unknown, "can't read"), rep(idAA, engine.Configured, "34 profiles enforced"),
		rep(idUpdate, engine.NotConfigured, updates.DetailNoUnattended),
	}, h)
	want := []risk.Factor{
		{Kind: risk.KindLockoutAtStart, Control: idFW, Detail: "firewall blocks ssh after restart"},
		{Kind: risk.KindUnknownRow, Control: idPort, Detail: "SSH Port unknown"},
		{Kind: risk.KindSecondRoot, Control: idUser, Detail: "second root account", Penalty: risk.PenaltySecondRoot},
		{Kind: risk.KindSSHUnchecked, Control: idFW, Detail: "can't tell if firewall blocks ssh"},
		{Kind: risk.KindReboot, Detail: "reboot pending", Penalty: risk.PenaltyReboot},
		{Kind: risk.KindUptime, Detail: "up 182 days, no auto updates", Penalty: risk.PenaltyUptime},
		{Kind: risk.KindNoPassword, Control: idUser, Detail: "no password to become root", Penalty: risk.PenaltyNoPassword},
	}
	if !slices.Equal(l.Factors, want) {
		t.Errorf("got  %+v\nwant %+v", l.Factors, want)
	}
	// User 10, SSH Login 15, SSH Auth 20 and AppArmor 10 earned, less
	// 15 + 10 + 10 + 5.
	if l.Band != risk.Critical || !near(l.Score, 15) {
		t.Errorf("band %v score %v, want critical 15", l.Band, l.Score)
	}
}

// TestFactorsFit: each factor's line fits the 35 cells a factor has
// beside the label at 64 columns (DEV-022), the longest uptime the header
// shows included.
func TestFactorsFit(t *testing.T) {
	var all []risk.Factor
	long := uptimeOf(9999*day + 23*time.Hour)
	for _, u := range []engine.Report{
		rep(idUpdate, engine.NotConfigured, updates.DetailNoUnattended),
		rep(idUpdate, engine.Unknown, updates.DetailCantRead),
		rep(idUpdate, engine.Configured, updates.DetailUnattended, updates.CodeProxmoxNotUpgraded),
	} {
		all = append(all, risk.Assess(with(u), long).Factors...)
	}
	all = append(all, risk.Assess([]engine.Report{
		rep(idUser, engine.Configured, "x", users.CodeNoPassword, users.CodeUID0),
		rep(idFW, engine.NotConfigured, "x", "UFW_SSH_NOT_ALLOWED_AT_START", firewall.CodeSSHUnchecked),
	}, header.Header{}).Factors...)
	all = append(all, risk.Assess(with(rep(idFW, engine.Partial, "x", firewall.CodeSSHBlocked)), pending(detect.Yes)).Factors...)
	kinds := map[risk.Kind]bool{}
	for _, f := range all {
		kinds[f.Kind] = true
		if n := len([]rune(f.Detail)); n > 35 {
			t.Errorf("%q: %d cells, over 35", f.Detail, n)
		}
	}
	if len(kinds) != 8 {
		t.Errorf("%d kinds checked, want 8", len(kinds))
	}
}

// TestRows: the rows are the five areas' controls, once each, in Figma's
// order, their weights 100 together, the port lighter than either auth
// setting, the firewall one row.
func TestRows(t *testing.T) {
	var controls []engine.Control
	for _, cs := range [][]engine.Control{users.Controls(), firewall.Controls(), ssh.Controls(), apparmor.Controls(), updates.Controls()} {
		controls = append(controls, cs...)
	}
	rows := risk.Rows()
	if len(rows) != len(controls) {
		t.Fatalf("%d rows for %d controls", len(rows), len(controls))
	}
	weight := map[string]int{}
	sum := 0
	for i, r := range rows {
		if r.ID != controls[i].ID() || r.Title != controls[i].Title() {
			t.Errorf("row %d is %s %q, want %s %q", i, r.ID, r.Title, controls[i].ID(), controls[i].Title())
		}
		if r.Weight <= 0 {
			t.Errorf("%s weighs %d", r.ID, r.Weight)
		}
		weight[r.ID] = r.Weight
		sum += r.Weight
	}
	var titles []string
	for _, r := range rows {
		titles = append(titles, r.Title)
	}
	if want := []string{"User", "Firewall", "SSH Login", "SSH Auth", "SSH Port", "AppArmor", "Auto Updates"}; !slices.Equal(titles, want) {
		t.Errorf("titles %q, want Figma's %q", titles, want)
	}
	if sum != 100 {
		t.Errorf("weights sum to %d, want 100", sum)
	}
	if weight[idPort] >= weight[idLogin] || weight[idPort] >= weight[idAuth] {
		t.Errorf("port %d, login %d, auth %d: the port must weigh less", weight[idPort], weight[idLogin], weight[idAuth])
	}
	if !(0 < risk.HighFrom && risk.HighFrom < risk.ModerateFrom && risk.ModerateFrom < risk.LowFrom && risk.LowFrom < risk.MinimalFrom && risk.MinimalFrom <= 100) {
		t.Error("the bands' bounds are not increasing within 0 to 100")
	}
	rows[0].Weight = 0
	if risk.Rows()[0].Weight == 0 {
		t.Error("Rows shares its table")
	}
}

// TestBandWords: each band's word, and one outside them.
func TestBandWords(t *testing.T) {
	for b, w := range map[risk.Band]string{risk.Unknown: "unknown", risk.Critical: "critical", risk.High: "high",
		risk.Moderate: "moderate", risk.Low: "low", risk.Minimal: "minimal", risk.Band(9): "band(9)"} {
		if b.String() != w {
			t.Errorf("%d: %q, want %q", b, b.String(), w)
		}
	}
	if risk.KindNoPassword.String() != "no-password" || risk.Kind(9).String() != "kind(9)" {
		t.Error("kind words")
	}
}
