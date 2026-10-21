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
	"github.com/abbott/hardn/pkg/engine/controls/firewall"
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
// Engine.Check's order (the areas' packages), not Figma's.
func figmaReports() []engine.Report {
	return []engine.Report{
		row("ssh.login", "SSH Login", engine.Configured, "root disallowed"),
		row("ssh.auth", "SSH Auth", engine.Configured, "key-only auth"),
		row("ssh.port", "SSH Port", engine.Configured, "non-default (2230)"),
		row("firewall.incoming", "Firewall", engine.Configured, "deny policy"),
		row("users.admin", "User", engine.Configured, "non-root, sudo"),
		row(apparmor.ID, "AppArmor", engine.NotConfigured, apparmor.DetailNoProfiles),
		row(updates.ID, "Auto Updates", engine.NotConfigured, updates.DetailNoUnattended),
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

// TestStatusViewOfFigma: Figma's host, its rows in Figma's order with its
// details, and its Moderate level, with the long uptime as its one factor.
func TestStatusViewOfFigma(t *testing.T) {
	rs := figmaReports()
	v := output.StatusViewOf(rs, risk.Assess(rs, upFor(figmaUptime)))
	want := output.StatusView{
		Risk: component.RiskSummary{Band: component.RiskModerate, Level: "Moderate", Gloss: "medium security",
			Factors: []string{"up 182 days, no auto updates"}},
		Status: component.StatusList{Rows: []component.StatusRow{
			{Label: "User", State: component.StatusConfigured, Detail: "non-root, sudo"},
			{Label: "Firewall", State: component.StatusConfigured, Detail: "deny policy"},
			{Label: "SSH Login", State: component.StatusConfigured, Detail: "root disallowed"},
			{Label: "SSH Auth", State: component.StatusConfigured, Detail: "key-only auth"},
			{Label: "SSH Port", State: component.StatusConfigured, Detail: "non-default (2230)"},
			{Label: "AppArmor", State: component.StatusNotConfigured, Detail: "no profiles loaded"},
			{Label: "Auto Updates", State: component.StatusNotConfigured, Detail: "no unattended-upgrades"},
		}},
	}
	if !reflect.DeepEqual(v, want) {
		t.Errorf("got  %+v\nwant %+v", v, want)
	}
}

// TestStatusRows: Figma's order whatever the reports', a row with no
// report Unknown, a failed check Unknown, a control the score does not
// weigh after the rows, a control's first report.
func TestStatusRows(t *testing.T) {
	rs := figmaReports()
	rs = slices.DeleteFunc(rs, func(r engine.Report) bool { return r.Control == "users.admin" })
	rs = append([]engine.Report{row("dns.servers", "DNS", engine.Partial, "one server")}, rs...)
	rs = replaced(rs, engine.Report{Control: "ssh.auth", Title: "SSH Auth", Status: engine.Status{State: engine.Configured, Detail: "sshd not running"},
		Err: errors.New("sshd -T")})
	rs = append(rs, row("ssh.port", "SSH Port", engine.NotConfigured, "default (22)"), row("dns.servers", "DNS", engine.NotConfigured, "x"))
	v := output.StatusViewOf(rs, risk.Assess(rs, upFor(time.Hour)))
	want := []component.StatusRow{
		{Label: "User", State: component.StatusUnknown},
		{Label: "Firewall", State: component.StatusConfigured, Detail: "deny policy"},
		{Label: "SSH Login", State: component.StatusConfigured, Detail: "root disallowed"},
		{Label: "SSH Auth", State: component.StatusUnknown, Detail: "sshd not running"},
		{Label: "SSH Port", State: component.StatusConfigured, Detail: "non-default (2230)"},
		{Label: "AppArmor", State: component.StatusNotConfigured, Detail: "no profiles loaded"},
		{Label: "Auto Updates", State: component.StatusNotConfigured, Detail: "no unattended-upgrades"},
		{Label: "DNS", State: component.StatusPartial, Detail: "one server"},
	}
	if !slices.Equal(v.Status.Rows, want) {
		t.Errorf("got  %+v\nwant %+v", v.Status.Rows, want)
	}
	if f := v.Risk.Factors; !slices.Equal(f, []string{"User unknown", "SSH Auth unknown"}) {
		t.Errorf("factors %q", f)
	}
}

// TestRiskSummaryOf: each band's tone, level word and gloss, origin/main's
// (DEV-016); the lockout's gloss; no level for Unknown.
func TestRiskSummaryOf(t *testing.T) {
	for _, c := range []struct {
		l    risk.Level
		want component.RiskSummary
	}{
		{risk.Level{Band: risk.Critical}, component.RiskSummary{Band: component.RiskHigh, Level: "Critical", Gloss: "no security"}},
		{risk.Level{Band: risk.Critical, Forced: true, Factors: []risk.Factor{{Detail: "firewall blocks ssh"}}},
			component.RiskSummary{Band: component.RiskHigh, Level: "Critical", Gloss: output.LockoutGloss, Factors: []string{"firewall blocks ssh"}}},
		{risk.Level{Band: risk.High}, component.RiskSummary{Band: component.RiskHigh, Level: "High", Gloss: "weak security"}},
		{risk.Level{Band: risk.Moderate}, component.RiskSummary{Band: component.RiskModerate, Level: "Moderate", Gloss: "medium security"}},
		{risk.Level{Band: risk.Low}, component.RiskSummary{Band: component.RiskLow, Level: "Low", Gloss: "strong security"}},
		{risk.Level{Band: risk.Minimal}, component.RiskSummary{Band: component.RiskLow, Level: "Minimal", Gloss: "hardened security"}},
		{risk.Level{Band: risk.Unknown, Factors: []risk.Factor{{Detail: "User unknown"}}},
			component.RiskSummary{Band: component.RiskUnknown, Factors: []string{"User unknown"}}},
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
		row(apparmor.ID, "AppArmor", engine.NotApplicable, apparmor.DetailByHost)}
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
		}
		return strings.Join(out, "\n\n") + "\n"
	})
}
