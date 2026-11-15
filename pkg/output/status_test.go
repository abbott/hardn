package output_test

import (
	"errors"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

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
// (risk.Rows) with its details, the three SSH controls in the SSH row, no row for
// DNS, which is fine, and its Moderate level, Figma's one line, with the
// long uptime its one factor, a cost established under Auto Updates.
func TestStatusViewOfFigma(t *testing.T) {
	rs := figmaReports()
	v := output.StatusViewOf(rs, risk.Assess(rs, upFor(figmaUptime)))
	want := output.StatusView{
		Risk: component.RiskSummary{Band: component.RiskModerate, Level: "Moderate", Gloss: "medium security"},
		Status: component.StatusList{Rows: []component.StatusRow{
			{Label: "User", State: component.StatusConfigured, Detail: "non-root, sudo"},
			{Label: "SSH", State: component.StatusConfigured, Detail: "key-only, no root, port 2230"},
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
		{Label: "SSH", State: component.StatusUnknown, Detail: "sshd not running", Lines: counted},
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

// TestMergeSSH: the one SSH row (the maintainer, 2026-11-15): SSH Login,
// SSH Auth and SSH Port, labelled "SSH". All three Configured, his words,
// auth, login, port. Login and auth Configured and the port on 22: Partial,
// "default port (22)", no line. Login and auth not both Configured: their
// state and detail as v0.10.1 merged them (the worse of the two, a part
// read as off worse than one not read, the details of the parts in that
// state, SSH Login's first), with a line under the row where the port is
// on 22, and nothing for a port off 22 or not read.
func TestMergeSSH(t *testing.T) {
	ok, off, part, na, unk := engine.Configured, engine.NotConfigured, engine.Partial, engine.NotApplicable, engine.Unknown
	type st struct {
		state  engine.State
		detail string
	}
	type want struct {
		state  component.StatusState
		detail string
		line   string
	}
	const defaultLine = "Default port (22)"
	set, def, unread := st{ok, "non-default (2230)"}, st{off, ssh.DetailDefaultPort}, st{unk, "can't read"}
	// Every pair of login and auth, with the port off 22, on 22 and not read.
	for _, c := range []struct {
		login, auth      st
		set, def, unread want
	}{
		{st{ok, "root disallowed"}, st{ok, "key-only auth"},
			want{component.StatusConfigured, "key-only, no root, port 2230", ""},
			want{component.StatusPartial, "default port (22)", ""},
			want{component.StatusUnknown, "port: can't read", ""}},
		{st{ok, "root locked"}, st{ok, "key and password"},
			want{component.StatusConfigured, "key+password, root locked, port 2230", ""},
			want{component.StatusPartial, "default port (22)", ""},
			want{component.StatusUnknown, "port: can't read", ""}},
		{st{ok, "root disallowed"}, st{off, "password auth on"},
			want{component.StatusNotConfigured, "password auth on", ""},
			want{component.StatusNotConfigured, "password auth on", defaultLine},
			want{component.StatusNotConfigured, "password auth on", ""}},
		{st{off, "root allowed"}, st{ok, "key-only auth"},
			want{component.StatusNotConfigured, "root allowed", ""},
			want{component.StatusNotConfigured, "root allowed", defaultLine},
			want{component.StatusNotConfigured, "root allowed", ""}},
		{st{part, "root key-only"}, st{off, "password auth on"},
			want{component.StatusNotConfigured, "password auth on", ""},
			want{component.StatusNotConfigured, "password auth on", defaultLine},
			want{component.StatusNotConfigured, "password auth on", ""}},
		{st{off, "root allowed"}, st{off, "password auth on"},
			want{component.StatusNotConfigured, "root allowed, password auth on", ""},
			want{component.StatusNotConfigured, "root allowed, password auth on", defaultLine},
			want{component.StatusNotConfigured, "root allowed, password auth on", ""}},
		{st{off, "root allowed"}, st{part, "match block allows passwords"},
			want{component.StatusNotConfigured, "root allowed", ""},
			want{component.StatusNotConfigured, "root allowed", defaultLine},
			want{component.StatusNotConfigured, "root allowed", ""}},
		{st{part, "root key-only"}, st{ok, "key-only auth"},
			want{component.StatusPartial, "root key-only", ""},
			want{component.StatusPartial, "root key-only", defaultLine},
			want{component.StatusPartial, "root key-only", ""}},
		{st{part, "root disallowed"}, st{part, "key auth off"}, // a Partial part keeps the control's words
			want{component.StatusPartial, "root disallowed, key auth off", ""},
			want{component.StatusPartial, "root disallowed, key auth off", defaultLine},
			want{component.StatusPartial, "root disallowed, key auth off", ""}},
		{st{ok, "root disallowed"}, st{unk, "can't read"},
			want{component.StatusUnknown, "can't read", ""},
			want{component.StatusUnknown, "can't read", defaultLine},
			want{component.StatusUnknown, "can't read", ""}},
		{st{unk, "can't read"}, st{unk, "can't read"},
			want{component.StatusUnknown, "can't read", ""},
			want{component.StatusUnknown, "can't read", defaultLine},
			want{component.StatusUnknown, "can't read", ""}},
		// A part read as off outranks one not read: what hardn established is said.
		{st{off, "root allowed"}, st{unk, "can't read"},
			want{component.StatusNotConfigured, "root allowed", ""},
			want{component.StatusNotConfigured, "root allowed", defaultLine},
			want{component.StatusNotConfigured, "root allowed", ""}},
		{st{unk, "can't tell if root key works"}, st{off, "password auth on"},
			want{component.StatusNotConfigured, "password auth on", ""},
			want{component.StatusNotConfigured, "password auth on", defaultLine},
			want{component.StatusNotConfigured, "password auth on", ""}},
		{st{part, "root key-only"}, st{unk, "can't read"},
			want{component.StatusPartial, "root key-only", ""},
			want{component.StatusPartial, "root key-only", defaultLine},
			want{component.StatusPartial, "root key-only", ""}},
		// A part Not Applicable beside one Configured (sshd gone between two
		// reads): never "+", whatever the port.
		{st{ok, "root disallowed"}, st{na, "no SSH server"},
			want{component.StatusNotApplicable, "no SSH server", ""},
			want{component.StatusNotApplicable, "no SSH server", ""},
			want{component.StatusNotApplicable, "no SSH server", ""}},
		{st{na, "no SSH server"}, st{ok, "key-only auth"},
			want{component.StatusNotApplicable, "no SSH server", ""},
			want{component.StatusNotApplicable, "no SSH server", ""},
			want{component.StatusNotApplicable, "no SSH server", ""}},
		{st{na, "no SSH server"}, st{off, "password auth on"}, // what is read as off is still said
			want{component.StatusNotConfigured, "password auth on", ""},
			want{component.StatusNotConfigured, "password auth on", defaultLine},
			want{component.StatusNotConfigured, "password auth on", ""}},
		{st{ok, ""}, st{ok, "key-only auth"},
			want{component.StatusConfigured, "key-only, port 2230", ""},
			want{component.StatusPartial, "default port (22)", ""},
			want{component.StatusUnknown, "port: can't read", ""}},
	} {
		for _, p := range []struct {
			port st
			want want
		}{{set, c.set}, {def, c.def}, {unread, c.unread}} {
			if got := sshRow(t, c.login, c.auth, p.port); got != p.want {
				t.Errorf("login %+v, auth %+v, port %+v: %+v, want %+v", c.login, c.auth, p.port, got, p.want)
			}
		}
	}
	// The port's other details, and Proxmox VE, whose baseline is port 22
	// and root by key: the row does not say "no root" there.
	both, pve := [2]st{{ok, "root disallowed"}, {ok, "key-only auth"}}, [2]st{{ok, ssh.DetailProxmoxKeyOnly}, {ok, "key-only auth"}}
	keyOnly := [2]st{{part, "root key-only"}, {ok, "key-only auth"}}
	for _, c := range []struct {
		pair [2]st
		port st
		want want
	}{
		{both, st{ok, "non-default (2230, 2231)"}, want{component.StatusConfigured, "key-only, no root, 2 ports", ""}},
		{both, st{ok, "non-default (22300)"}, want{component.StatusConfigured, "key-only, no root, port 22300", ""}},
		{both, st{part, "22 and 2230"}, want{component.StatusPartial, "ports 22 and 2230", ""}},
		{keyOnly, st{part, "22 and 2230"}, want{component.StatusPartial, "root key-only", "Ports 22 and 2230"}},
		{both, st{unk, "not listening on 2230"}, want{component.StatusUnknown, "port: not listening on 2230", ""}},
		{both, st{unk, ""}, want{component.StatusUnknown, "", ""}},
		{both, st{na, "no SSH server"}, want{component.StatusNotApplicable, "no SSH server", ""}},
		{keyOnly, st{na, "no SSH server"}, want{component.StatusPartial, "root key-only", ""}},
		{[2]st{{na, "no SSH server"}, {na, "no SSH server"}}, st{ok, "non-default (2230)"}, want{component.StatusNotApplicable, "no SSH server", ""}},
		{[2]st{{na, "no SSH server"}, {na, "no SSH server"}}, st{na, "no SSH server"}, want{component.StatusNotApplicable, "no SSH server", ""}},
		{pve, st{ok, ssh.DetailDefaultPort}, want{component.StatusConfigured, "key-only, root key-only, port 22", ""}},
		{pve, st{off, "non-default (2230)"}, want{component.StatusPartial, "port 2230, not 22", ""}},
		{pve, st{off, "non-default (2230, 2231)"}, want{component.StatusPartial, "ports 2230, 2231, not 22", ""}},
		{pve, st{off, "22 and 2230"}, want{component.StatusPartial, "ports 22 and 2230", ""}},
		{pve, st{off, "other ports configured"}, want{component.StatusPartial, "other ports configured", ""}},
		{[2]st{{ok, ssh.DetailProxmoxKeyOnly}, {off, "password auth on"}}, st{ok, ssh.DetailDefaultPort}, want{component.StatusNotConfigured, "password auth on", ""}},
		{[2]st{{part, "root key login blocked"}, {ok, "key-only auth"}}, st{off, "non-default (2230)"}, want{component.StatusPartial, "root key login blocked", "Port 2230, not 22"}},
	} {
		if got := sshRow(t, c.pair[0], c.pair[1], c.port); got != c.want {
			t.Errorf("login %+v, auth %+v, port %+v: %+v, want %+v", c.pair[0], c.pair[1], c.port, got, c.want)
		}
	}
	// Every Configured row's detail fits the 35 cells after "+", with a
	// port of five digits, but "key+password" beside "root locked" or on
	// Proxmox VE, which have no shorter words.
	const cells = 35
	type combo struct{ login, auth, port string }
	long := map[combo]string{
		{"root locked", ssh.DetailKeyAndPassword, "non-default (2230)"}:             "key+password, root locked, port 2230",
		{"root locked", ssh.DetailKeyAndPassword, "non-default (65535)"}:            "key+password, root locked, port 65535",
		{ssh.DetailProxmoxKeyOnly, ssh.DetailKeyAndPassword, ssh.DetailDefaultPort}: "key+password, root key-only, port 22",
	}
	for _, login := range []string{ssh.DetailRootDisallowed, "root locked", ssh.DetailProxmoxKeyOnly} {
		ports := []string{"non-default (2230)", "non-default (65535)", "non-default (65534, 65535)"}
		if login == ssh.DetailProxmoxKeyOnly {
			ports = []string{ssh.DetailDefaultPort}
		}
		for _, auth := range []string{ssh.DetailKeyOnly, ssh.DetailKeyAndPassword} {
			for _, port := range ports {
				got := sshRow(t, st{ok, login}, st{ok, auth}, st{ok, port})
				want, over := long[combo{login, auth, port}]
				switch n := utf8.RuneCountInString(got.detail); {
				case got.state != component.StatusConfigured:
					t.Errorf("%s, %s, %s: %+v", login, auth, port, got)
				case over && (got.detail != want || n <= cells):
					t.Errorf("%q is %d cells, want %q, over %d", got.detail, n, want, cells)
				case !over && n > cells:
					t.Errorf("%q is %d cells of %d", got.detail, n, cells)
				}
			}
		}
	}
	if d := sshRow(t, both[0], st{ok, ssh.DetailKeyAndPassword}, st{ok, "non-default (65535)"}).detail; d != "key+password, no root, port 65535" {
		t.Errorf("%q", d)
	}
	// Each control's factor is a line under the one row, in the level's
	// order, after the port's own line.
	l := risk.Level{Factors: []risk.Factor{{Control: "ssh.login", Detail: "Counted as not configured"}, {Control: "ssh.auth", Detail: "Can't tell if sshd is patched"},
		{Control: "ssh.port", Detail: "Counted as not configured"}}}
	rows := output.StatusViewOf([]engine.Report{row("ssh.port", "SSH Port", off, ssh.DetailDefaultPort)}, l).Status.Rows
	if got := rows[1].Lines; len(got) != 3 || got[0] != (component.StatusLine{Text: "Default port (22)", Warn: true}) ||
		got[1].Text != "Counted as not configured" || got[2].Text != "Can't tell if sshd is patched" {
		t.Errorf("lines under SSH: %+v", got)
	}
	// A line the three have is said once: no report read.
	none := output.StatusViewOf(nil, risk.Assess(nil, upFor(time.Hour))).Status.Rows[1]
	if none.Label != "SSH" || none.State != component.StatusUnknown || len(none.Lines) != 1 || none.Lines[0].Text != "Counted as not configured" {
		t.Errorf("none read: %+v", none)
	}
}

// sshRow is the SSH row of the three controls' reports, given in an order
// not the rows': its state, its detail and its one line, "" for none. The
// row is the second of six, with no SSH Port row after it, and its line is
// in the warning colour.
func sshRow(t *testing.T, login, auth, port struct {
	state  engine.State
	detail string
}) struct {
	state  component.StatusState
	detail string
	line   string
} {
	t.Helper()
	rs := []engine.Report{row("ssh.port", "SSH Port", port.state, port.detail), row("ssh.auth", "SSH Auth", auth.state, auth.detail),
		row("ssh.login", "SSH Login", login.state, login.detail)}
	rows := output.StatusViewOf(rs, risk.Level{}).Status.Rows
	if len(rows) != len(risk.Rows())-2 || rows[1].Label != "SSH" || rows[2].Label != "Firewall" {
		t.Fatalf("rows %+v", rows)
	}
	got := struct {
		state  component.StatusState
		detail string
		line   string
	}{rows[1].State, rows[1].Detail, ""}
	switch lines := rows[1].Lines; {
	case len(lines) > 1 || len(lines) == 1 && !lines[0].Warn:
		t.Fatalf("lines %+v", lines)
	case len(lines) == 1:
		got.line = lines[0].Text
	}
	return got
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
	titles["ssh.login"], titles["ssh.auth"], titles["ssh.port"] = "SSH", "SSH", "SSH" // one row for the three (TestMergeSSH)
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
				// A line the SSH controls share is under their row once.
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
