package output_test

import (
	"errors"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/abbott/hardn/pkg/engine"
	"github.com/abbott/hardn/pkg/engine/controls/apparmor"
	"github.com/abbott/hardn/pkg/engine/controls/dns"
	"github.com/abbott/hardn/pkg/engine/controls/firewall"
	"github.com/abbott/hardn/pkg/engine/controls/kernelsettings"
	"github.com/abbott/hardn/pkg/engine/controls/logforward"
	"github.com/abbott/hardn/pkg/engine/controls/ssh"
	"github.com/abbott/hardn/pkg/engine/controls/timesync"
	"github.com/abbott/hardn/pkg/engine/controls/updates"
	"github.com/abbott/hardn/pkg/engine/controls/users"
	"github.com/abbott/hardn/pkg/engine/header"
	"github.com/abbott/hardn/pkg/engine/risk"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/output"
	"github.com/abbott/hardn/pkg/ui/component"
	"github.com/abbott/hardn/pkg/ui/term"
	"github.com/abbott/hardn/pkg/ui/theme"
	"github.com/abbott/hardn/pkg/ui/uitest"
)

// row is a report as Engine.Check gives it: the control's id and title,
// its state, detail and findings by code.
func row(id, title string, s engine.State, detail string, codes ...string) engine.Report {
	r := engine.Report{Control: id, Title: title, Status: engine.Status{State: s, Detail: detail}}
	for _, c := range codes {
		r.Status.Findings = append(r.Status.Findings, engine.Finding{Code: c, Detail: "synthetic"})
	}
	return r
}

// figmaReports is Figma's 00-TUI-MAIN host, as the checks report it, in
// an order not the rows' (risk.Rows), with the Kernel row, applied, and
// DNS, neither of which Figma draws, its nameservers hardn.yml's.
func figmaReports() []engine.Report {
	return []engine.Report{
		row("ssh.login", "SSH Login", engine.Configured, "root disallowed"),
		row("ssh.auth", "SSH Auth", engine.Configured, "key-only auth"),
		row("ssh.port", "SSH Port", engine.Configured, "non-default (2230)"),
		row("firewall.incoming", "Firewall", engine.Configured, "deny policy"),
		row("users.admin", "User", engine.Configured, "non-root, sudo"),
		row(apparmor.ID, "AppArmor", engine.NotConfigured, apparmor.DetailNoProfiles),
		row(updates.ID, "Auto Updates", engine.NotConfigured, updates.DetailNoUnattended),
		row(dns.ID, "DNS", engine.Configured, "192.0.2.54, 2001:db8::54"),
		row(kernelsettings.ID, "Kernel", engine.Configured, "6 items applied"),
	}
}

// replaced is rs with r in place of its control's report.
func replaced(rs []engine.Report, r engine.Report) []engine.Report {
	out := slices.Clone(rs)
	out[slices.IndexFunc(out, func(x engine.Report) bool { return x.Control == r.Control })] = r
	return out
}

// upFor is a header up d, no reboot pending.
func upFor(d time.Duration) header.Header {
	return header.Header{Uptime: up(d), Reboot: header.Reboot{Pending: detect.No}}
}

// TestStatusViewOfFigma: Figma's host, its rows in the rows' order
// (risk.Rows) with its details, SSH Auth in the SSH Login row, no row for
// DNS, which is fine, and its Moderate level, Figma's one line, with the
// long uptime its one factor, a cost established under Auto Updates.
func TestStatusViewOfFigma(t *testing.T) {
	rs := figmaReports()
	v := output.StatusViewOf(rs, risk.Assess(rs, upFor(figmaUptime)))
	want := output.StatusView{
		Risk: component.RiskSummary{Band: component.RiskModerate, Level: "Moderate", Gloss: "medium security"},
		Status: component.StatusList{Rows: []component.StatusRow{
			{Label: "User", State: component.StatusConfigured, Detail: "non-root, sudo"},
			{Label: "SSH Login", State: component.StatusConfigured, Detail: "root disallowed, key-only auth"},
			{Label: "SSH Port", State: component.StatusConfigured, Detail: "non-default (2230)"},
			{Label: "Firewall", State: component.StatusConfigured, Detail: "deny policy"},
			{Label: "Kernel", State: component.StatusConfigured, Detail: "6 items applied"},
			{Label: "Auto Updates", State: component.StatusNotConfigured, Detail: "no unattended-upgrades",
				Lines: []component.StatusLine{{Text: "Up 182 days, no updates", Warn: true}}},
			{Label: "AppArmor", State: component.StatusNotConfigured, Detail: "no profiles loaded"},
		}},
	}
	if !reflect.DeepEqual(v, want) {
		t.Errorf("got  %+v\nwant %+v", v, want)
	}
}

// TestStatusRows: the rows' order whatever the reports', a row with no
// report Unknown, a failed check Unknown, each with its line, SSH Auth's
// state, detail and line in the SSH Login row, a control's first report;
// a control the score does not weigh has no row while it says no problem,
// and one under the rows, in Info, when it does.
func TestStatusRows(t *testing.T) {
	rs := figmaReports()
	rs = slices.DeleteFunc(rs, func(r engine.Report) bool { return r.Control == "users.admin" })
	rs = append([]engine.Report{row("extra.row", "Extra", engine.Partial, "one server")}, rs...)
	rs = replaced(rs, engine.Report{Control: "ssh.auth", Title: "SSH Auth", Status: engine.Status{State: engine.Configured, Detail: "sshd not running"},
		Err: errors.New("sshd -T")})
	wrong := row("more.row", "More", engine.Partial, "wrong")
	wrong.Status.Problem, wrong.Status.Value = "Wrong", "192.0.2.9"
	rs = append(rs, row("ssh.port", "SSH Port", engine.NotConfigured, "default (22)"), row("extra.row", "Extra", engine.NotConfigured, "x"), wrong)
	v := output.StatusViewOf(rs, risk.Assess(rs, upFor(time.Hour)))
	counted := []component.StatusLine{{Text: "Counted as not configured"}}
	want := []component.StatusRow{
		{Label: "User", State: component.StatusUnknown, Lines: counted},
		{Label: "SSH Login", State: component.StatusUnknown, Detail: "sshd not running", Lines: counted},
		{Label: "SSH Port", State: component.StatusConfigured, Detail: "non-default (2230)"},
		{Label: "Firewall", State: component.StatusConfigured, Detail: "deny policy"},
		{Label: "Kernel", State: component.StatusConfigured, Detail: "6 items applied"},
		{Label: "Auto Updates", State: component.StatusNotConfigured, Detail: "no unattended-upgrades"},
		{Label: "AppArmor", State: component.StatusNotConfigured, Detail: "no profiles loaded"},
	}
	if !reflect.DeepEqual(v.Status.Rows, want) {
		t.Errorf("got  %+v\nwant %+v", v.Status.Rows, want)
	}
	info := []component.StatusRow{{Label: "More", Info: true, Detail: "192.0.2.9", Lines: []component.StatusLine{{Text: "Wrong", Warn: true}}}}
	if !reflect.DeepEqual(v.Info.Rows, info) {
		t.Errorf("info rows %+v\nwant %+v", v.Info.Rows, info)
	}
}

// TestMergeSSH: the SSH Login row with SSH Auth in it (the maintainer,
// 2026-11-14): the worse of the two states, a part read as off worse than
// one not read; both details when both are Configured; else the details of
// the parts in the row's state alone, SSH Login's first, a part less bad
// not said; two that say the same, once.
func TestMergeSSH(t *testing.T) {
	ok, off, part, na, unk := engine.Configured, engine.NotConfigured, engine.Partial, engine.NotApplicable, engine.Unknown
	for _, c := range []struct {
		login       engine.State
		loginDetail string
		auth        engine.State
		authDetail  string
		want        component.StatusState
		detail      string
	}{
		{ok, "root disallowed", ok, "key-only auth", component.StatusConfigured, "root disallowed, key-only auth"},
		{ok, "root disallowed", off, "password auth on", component.StatusNotConfigured, "password auth on"},
		{off, "root allowed", ok, "key-only auth", component.StatusNotConfigured, "root allowed"},
		{part, "root key-only", off, "password auth on", component.StatusNotConfigured, "password auth on"},
		{off, "root allowed", off, "password auth on", component.StatusNotConfigured, "root allowed, password auth on"},
		{off, "root allowed", part, "match block allows passwords", component.StatusNotConfigured, "root allowed"},
		{part, "root key-only", ok, "key-only auth", component.StatusPartial, "root key-only"},
		{ok, "root disallowed", unk, "can't read", component.StatusUnknown, "can't read"},
		{unk, "can't read", unk, "can't read", component.StatusUnknown, "can't read"},
		// A part read as off outranks one not read: what hardn established is said.
		{off, "root allowed", unk, "can't read", component.StatusNotConfigured, "root allowed"},
		{unk, "can't tell if root key works", off, "password auth on", component.StatusNotConfigured, "password auth on"},
		{part, "root key-only", unk, "can't read", component.StatusPartial, "root key-only"},
		// Proxmox VE's pair fits the row: 28 cells.
		{ok, ssh.DetailProxmoxKeyOnly, ok, "key-only auth", component.StatusConfigured, "root key-only, key-only auth"},
		{ok, ssh.DetailProxmoxKeyOnly, off, "password auth on", component.StatusNotConfigured, "password auth on"},
		{ok, ssh.DetailProxmoxKeyOnly, na, "no SSH server", component.StatusConfigured, ssh.DetailProxmoxKeyOnly},
		{na, "no SSH server", na, "no SSH server", component.StatusNotApplicable, "no SSH server"},
		{ok, "root disallowed", na, "no SSH server", component.StatusConfigured, "root disallowed"},
		{ok, "", ok, "key-only auth", component.StatusConfigured, "key-only auth"},
		{ok, "", ok, "", component.StatusConfigured, ""},
	} {
		rs := []engine.Report{row("ssh.auth", "SSH Auth", c.auth, c.authDetail), row("ssh.login", "SSH Login", c.login, c.loginDetail)}
		rows := output.StatusViewOf(rs, risk.Level{}).Status.Rows
		if len(rows) != len(risk.Rows())-1 || rows[1].Label != "SSH Login" || rows[2].Label != "SSH Port" {
			t.Fatalf("rows %+v", rows)
		}
		if got := rows[1]; got.State != c.want || got.Detail != c.detail {
			t.Errorf("login %v %q, auth %v %q: %v %q, want %v %q", c.login, c.loginDetail, c.auth, c.authDetail, got.State, got.Detail, c.want, c.detail)
		}
	}
	// Each control's factor is a line under the one row, in the level's order.
	l := risk.Level{Factors: []risk.Factor{{Control: "ssh.login", Detail: "Counted as not configured"}, {Control: "ssh.auth", Detail: "Can't tell if sshd is patched"}}}
	rows := output.StatusViewOf(nil, l).Status.Rows
	if got := rows[1].Lines; len(got) != 2 || got[0].Text != "Counted as not configured" || got[1].Text != "Can't tell if sshd is patched" {
		t.Errorf("lines under SSH Login: %+v", got)
	}
	// A line both have is said once: neither report read.
	none := output.StatusViewOf(nil, risk.Assess(nil, upFor(time.Hour))).Status.Rows[1]
	if none.State != component.StatusUnknown || len(none.Lines) != 1 || none.Lines[0].Text != "Counted as not configured" {
		t.Errorf("neither read: %+v", none)
	}
}

// TestInfoRows: DNS, NTP and Syslog have a row only with a problem (the
// maintainer, 2026-11-14), in the reports' order, after the scored rows:
// the host's value with the problem a warning line under it, or, with no
// value, the problem as the row's value; a check that failed and names no
// problem says "Can't read". None changes the risk level's rows.
func TestInfoRows(t *testing.T) {
	problem := func(id, title string, s engine.State, problem, value string) engine.Report {
		r := row(id, title, s, "x")
		r.Status.Problem, r.Status.Value = problem, value
		return r
	}
	failed := row(logforward.ID, "Syslog", engine.Unknown, "can't read")
	failed.Err = errors.New("not read")
	warn := func(text string) []component.StatusLine { return []component.StatusLine{{Text: text, Warn: true}} }
	for _, c := range []struct {
		name    string
		reports []engine.Report
		want    []component.StatusRow
	}{
		{"all fine", []engine.Report{row(dns.ID, "DNS", engine.Configured, "192.0.2.54"), row(timesync.ID, "NTP", engine.Configured, "192.0.2.10"),
			row(logforward.ID, "Syslog", engine.Configured, "192.0.2.50:514 TCP")}, nil},
		{"nothing asked", []engine.Report{row(dns.ID, "DNS", engine.NotApplicable, dns.DetailNoneConfigured), row(timesync.ID, "NTP", engine.NotConfigured, timesync.DetailNoService),
			row(logforward.ID, "Syslog", engine.NotConfigured, logforward.DetailOff)}, nil},
		{"each with a value", []engine.Report{problem(dns.ID, "DNS", engine.NotConfigured, dns.ProblemNotHardn, "192.168.1.1"),
			problem(timesync.ID, "NTP", engine.Partial, timesync.ProblemNotInSync, "10.2.8.1"), problem(logforward.ID, "Syslog", engine.Partial, "1 unreachable", "3 servers")},
			[]component.StatusRow{{Label: "DNS", Info: true, Detail: "192.168.1.1", Lines: warn("Not hardn.yml's")},
				{Label: "NTP", Info: true, Detail: "10.2.8.1", Lines: warn("Not in sync")}, {Label: "Syslog", Info: true, Detail: "3 servers", Lines: warn("1 unreachable")}}},
		{"no value", []engine.Report{problem(timesync.ID, "NTP", engine.NotConfigured, timesync.ProblemNoService, ""), problem(logforward.ID, "Syslog", engine.NotConfigured, logforward.ProblemOff, "")},
			[]component.StatusRow{{Label: "NTP", Info: true, Detail: "No time service", Problem: true}, {Label: "Syslog", Info: true, Detail: "Not forwarding", Problem: true}}},
		{"a check that failed", []engine.Report{failed}, []component.StatusRow{{Label: "Syslog", Info: true, Detail: "Can't read", Problem: true}}},
		{"a control's first report", []engine.Report{row(dns.ID, "DNS", engine.Configured, "192.0.2.54"), problem(dns.ID, "DNS", engine.NotConfigured, dns.ProblemNotHardn, "x")}, nil},
	} {
		base := output.StatusViewOf(nil, risk.Level{})
		v := output.StatusViewOf(c.reports, risk.Level{})
		if !reflect.DeepEqual(v.Info.Rows, c.want) {
			t.Errorf("%s: %+v\nwant %+v", c.name, v.Info.Rows, c.want)
		}
		if !reflect.DeepEqual(v.Status, base.Status) {
			t.Errorf("%s: the scored rows changed: %+v", c.name, v.Status)
		}
	}
	// A scored row's problem, were one set, makes no Info row.
	scored := problem("ssh.port", "SSH Port", engine.NotConfigured, "Default", "22")
	if v := output.StatusViewOf([]engine.Report{scored}, risk.Level{}); len(v.Info.Rows) != 0 {
		t.Errorf("a scored row in Info: %+v", v.Info.Rows)
	}
}

// TestReportRows: the report to hand over lists one row a control, SSH
// Login and SSH Auth each its own, in the rows' order, then the others in
// the reports' order, fine or not; no line.
func TestReportRows(t *testing.T) {
	rows := output.ReportRows(figmaReports())
	var got []string
	for _, r := range rows {
		got = append(got, r.Label+": "+r.State.String()+" "+r.Detail)
		if len(r.Lines) != 0 || r.Info {
			t.Errorf("%s: %+v", r.Label, r)
		}
	}
	want := []string{"User: Configured non-root, sudo", "SSH Login: Configured root disallowed", "SSH Auth: Configured key-only auth",
		"SSH Port: Configured non-default (2230)", "Firewall: Configured deny policy", "Kernel: Configured 6 items applied",
		"Auto Updates: Not Configured no unattended-upgrades", "AppArmor: Not Configured no profiles loaded", "DNS: Configured 192.0.2.54, 2001:db8::54"}
	if !slices.Equal(got, want) {
		t.Errorf("got  %q\nwant %q", got, want)
	}
	if rows := output.ReportRows(nil); len(rows) != len(risk.Rows()) || rows[0].State != component.StatusUnknown {
		t.Errorf("no report: %+v", rows)
	}
}

// TestFactorLines: every factor of a level is drawn once, each kind
// among them: under the row its Control names, in its colour and in
// risk.Kind's order there, or, the pending reboot alone, as the host
// block's line; no other line is drawn, and none under the risk level.
func TestFactorLines(t *testing.T) {
	titles := map[string]string{}
	for _, r := range risk.Rows() {
		titles[r.ID] = r.Title
	}
	titles["ssh.auth"] = titles["ssh.login"] // one row for both (TestMergeSSH)
	long := upFor(9999 * 24 * time.Hour)
	long.Reboot.Pending = detect.Yes
	every := []engine.Report{
		row("users.admin", "User", engine.Configured, "non-root, sudo", users.CodeNoPassword, users.CodeUID0),
		row("firewall.incoming", "Firewall", engine.NotConfigured, "enabled, not running", "UFW_SSH_NOT_ALLOWED_AT_START", firewall.CodeSSHUnchecked),
		row(apparmor.ID, "AppArmor", engine.Unknown, "can't read"),
		row(updates.ID, "Auto Updates", engine.Partial, updates.DetailOffAfterRestart, updates.CodeScheduleUnknown),
	}
	notRead := replaced(figmaReports(), row("users.admin", "User", engine.Configured, "non-root, sudo", users.CodeRuleNotRead))
	kinds := map[risk.Kind]bool{}
	for _, c := range []struct {
		name    string
		reports []engine.Report
		h       header.Header
	}{
		{"every kind", every, long},
		{"figma", figmaReports(), upFor(figmaUptime)},
		{"nothing read", nil, header.Header{}},
		{"a grant not read", notRead, upFor(time.Hour)},
	} {
		l := risk.Assess(c.reports, c.h)
		v := output.StatusViewOf(c.reports, l)
		host := output.HeaderViewOf("", c.h).Host
		if !reflect.DeepEqual(v.Risk, output.RiskSummaryOf(l)) {
			t.Errorf("%s: risk %+v", c.name, v.Risk)
		}
		want := map[string][]component.StatusLine{}
		for _, f := range l.Factors {
			kinds[f.Kind] = true
			switch {
			case f.Control == "" && f.Kind == risk.KindReboot:
				if host.Reboot == component.RebootNone || (host.Reboot == component.RebootPending) != f.Established {
					t.Errorf("%s: %q, the host block's line %d", c.name, f.Detail, host.Reboot)
				}
			case titles[f.Control] == "":
				t.Errorf("%s: %s %q under %q, a row the main menu does not show", c.name, f.Kind, f.Detail, f.Control)
			default:
				// A line SSH Login and SSH Auth share is under their row once.
				if line := (component.StatusLine{Text: f.Detail, Warn: f.Established}); !slices.Contains(want[titles[f.Control]], line) {
					want[titles[f.Control]] = append(want[titles[f.Control]], line)
				}
			}
		}
		for _, r := range v.Status.Rows {
			if !slices.Equal(r.Lines, want[r.Label]) {
				t.Errorf("%s: under %s %+v, want %+v", c.name, r.Label, r.Lines, want[r.Label])
			}
		}
	}
	if len(kinds) != 8 {
		t.Errorf("%d kinds drawn, want 8", len(kinds))
	}
}

// TestRiskSummaryOf: each band's tone, level word and gloss, origin/main's
// (DEV-016); the lockout's gloss, and CVE-2026-123158's bypass's before
// it; no level for Unknown; never a factor,
// which the rows and the host block draw.
func TestRiskSummaryOf(t *testing.T) {
	for _, c := range []struct {
		l    risk.Level
		want component.RiskSummary
	}{
		{risk.Level{Band: risk.Critical}, component.RiskSummary{Band: component.RiskHigh, Level: "Critical", Gloss: "no security"}},
		{risk.Level{Band: risk.Critical, Forced: true}, component.RiskSummary{Band: component.RiskHigh, Level: "Critical", Gloss: output.LockoutGloss}},
		{risk.Level{Band: risk.Critical, Forced: true, Factors: []risk.Factor{{Kind: risk.KindReboot, Detail: "Reboot pending"}}},
			component.RiskSummary{Band: component.RiskHigh, Level: "Critical", Gloss: output.LockoutGloss}},
		{risk.Level{Band: risk.Critical, Bypass: true}, component.RiskSummary{Band: component.RiskHigh, Level: "Critical", Gloss: output.BypassGloss}},
		{risk.Level{Band: risk.Critical, Bypass: true, Forced: true}, component.RiskSummary{Band: component.RiskHigh, Level: "Critical", Gloss: output.BypassGloss}},
		{risk.Level{Band: risk.High}, component.RiskSummary{Band: component.RiskHigh, Level: "High", Gloss: "weak security"}},
		{risk.Level{Band: risk.Moderate}, component.RiskSummary{Band: component.RiskModerate, Level: "Moderate", Gloss: "medium security"}},
		{risk.Level{Band: risk.Low}, component.RiskSummary{Band: component.RiskLow, Level: "Low", Gloss: "strong security"}},
		{risk.Level{Band: risk.Minimal}, component.RiskSummary{Band: component.RiskLow, Level: "Minimal", Gloss: "hardened security"}},
		{risk.Level{Band: risk.Unknown, Factors: []risk.Factor{{Kind: risk.KindUnknown, Control: "ssh.port", Detail: "Counted as not configured"}}},
			component.RiskSummary{Band: component.RiskUnknown}},
		{risk.Level{Band: risk.Band(9)}, component.RiskSummary{}},
	} {
		if got := output.RiskSummaryOf(c.l); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%v: got %+v, want %+v", c.l.Band, got, c.want)
		}
	}
}

// TestRiskFindings: a check's document carries the risk level's
// cross-check findings with no control, after the reports' own; none
// without a level.
func TestRiskFindings(t *testing.T) {
	rs := figmaReports()
	h := upFor(figmaUptime)
	h.Reboot = header.Reboot{Pending: detect.Yes, Findings: []engine.Finding{{Code: header.CodeRebootRequired, Detail: header.DetailRebootRequired,
		Source: header.RebootRequiredFile}}}
	rs = replaced(rs, row("users.admin", "User", engine.Configured, "non-root, sudo", users.CodeUID0))
	l := risk.Assess(rs, h)
	d := output.Build(output.Run{Mode: output.ModeCheck, Reports: rs, RiskLevel: &l})
	want := []output.Finding{
		{Code: users.CodeUID0, Detail: "synthetic", Control: "users.admin"},
		{Code: header.CodeRebootRequired, Detail: "package request", Source: "/var/run/reboot-required"},
		{Code: risk.CodeUptimeNoUpdates, Detail: "up 182 days, no auto updates", Source: "/proc/uptime"},
	}
	if !reflect.DeepEqual(d.Findings, want) {
		t.Errorf("got  %+v\nwant %+v", d.Findings, want)
	}
	if got := output.Build(output.Run{Mode: output.ModeCheck, Reports: rs}).Findings; len(got) != 1 {
		t.Errorf("without a level: %+v", got)
	}
	if d.Result.ExitCode != output.ExitChanges {
		t.Errorf("exit %d: the level changes no exit status", d.Result.ExitCode)
	}
}

// statusFixtures are the status blocks' fixtures: Figma's host, a
// hardened one, a firewall refusing SSH, a container with nothing read
// but its users, and a host whose every row is Unknown.
func statusFixtures() []struct {
	name    string
	reports []engine.Report
	h       header.Header
} {
	figma := figmaReports()
	hardened := replaced(replaced(figma, row(apparmor.ID, "AppArmor", engine.Configured, "34 profiles enforced")),
		row(updates.ID, "Auto Updates", engine.Configured, updates.DetailUnattended))
	lockout := replaced(hardened, row("firewall.incoming", "Firewall", engine.Partial, "ssh blocked", firewall.CodeSSHBlocked))
	container := []engine.Report{row("users.admin", "User", engine.Configured, "non-root, doas", users.CodeNoPassword),
		row(apparmor.ID, "AppArmor", engine.NotApplicable, apparmor.DetailByHost), row(dns.ID, "DNS", engine.NotApplicable, dns.DetailContainer),
		row(kernelsettings.ID, "Kernel", engine.NotApplicable, kernelsettings.DetailContainer)}
	for _, id := range []string{"firewall.incoming", "ssh.login", "ssh.auth", "ssh.port", updates.ID} {
		container = append(container, engine.Report{Control: id, Title: titles[id], Status: engine.Status{State: engine.Unknown, Detail: "can't read"},
			Err: errors.New("denied")})
	}
	var unknown []engine.Report
	for _, r := range figma {
		unknown = append(unknown, engine.Report{Control: r.Control, Title: r.Title, Status: engine.Status{State: engine.Unknown, Detail: "can't read"}})
	}
	pendingReboot := upFor(41 * 24 * time.Hour)
	pendingReboot.Reboot.Pending = detect.Yes
	return []struct {
		name    string
		reports []engine.Report
		h       header.Header
	}{
		{"figma", figma, upFor(figmaUptime)},
		{"hardened", hardened, upFor(3 * 24 * time.Hour)},
		{"reboot-pending", hardened, pendingReboot},
		{"lockout", lockout, upFor(3 * 24 * time.Hour)},
		{"container", container, header.Header{Uptime: up(figmaUptime), Reboot: header.Reboot{Pending: detect.No, HostKernel: true}}},
		{"unknown", unknown, header.Header{}},
	}
}

var titles = map[string]string{"firewall.incoming": "Firewall", "ssh.login": "SSH Login", "ssh.auth": "SSH Auth", "ssh.port": "SSH Port",
	updates.ID: "Auto Updates"}

// TestStatusBlocks pins the risk level and the status rows of each
// fixture, as the main menu lays them out (screen.MainMenu: a section gap
// apart), on the core cells.
func TestStatusBlocks(t *testing.T) {
	gap := strings.Repeat("\n", theme.SectionGap+1)
	uitest.Golden(t, "human/status", uitest.Core(), func(env term.Env) string {
		c := component.NewCtx(env)
		var out []string
		for _, fx := range statusFixtures() {
			v := output.StatusViewOf(fx.reports, risk.Assess(fx.reports, fx.h))
			out = append(out, "["+fx.name+"]\n"+v.Risk.Render(c)+gap+v.Status.Render(c))
			if info := v.Info.Render(c); info != "" {
				out[len(out)-1] += gap + info
			}
		}
		return strings.Join(out, "\n\n") + "\n"
	})
}
