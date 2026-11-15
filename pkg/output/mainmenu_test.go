package output_test

import (
	"net/netip"
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/abbott/hardn/pkg/engine"
	"github.com/abbott/hardn/pkg/engine/controls/ssh"
	"github.com/abbott/hardn/pkg/engine/header"
	"github.com/abbott/hardn/pkg/engine/risk"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/output"
	"github.com/abbott/hardn/pkg/ui/component"
	"github.com/abbott/hardn/pkg/ui/screen"
)

// TestMainMenuOf checks that MainMenuOf fills the blocks the status model
// owns as HeaderViewOf and StatusViewOf map them, the rows of what has a
// problem among them, with base's version, and keeps the rest of base,
// which it does not change.
func TestMainMenuOf(t *testing.T) {
	h := upFor(182*24*time.Hour + 2*time.Hour + 26*time.Minute)
	h.OS = header.OS{Name: "Debian", Version: "13", Codename: "trixie"}
	h.Hostname = header.Hostname{Name: "web-01"}
	h.Address.IPv4 = header.Family{Route: detect.Yes, Iface: "eth0", Addr: netip.MustParseAddr("192.0.2.20")}
	rs := figmaReports()
	rs[len(rs)-2].Status.State, rs[len(rs)-2].Status.Problem, rs[len(rs)-2].Status.Value = engine.NotConfigured, "Not hardn.yml's", "192.0.2.1" // DNS
	l := risk.Assess(rs, h)
	base := screen.MainMenuFixture()
	base.Header.Version = "v9.9.9"

	got := output.MainMenuOf(base, h, rs, l)
	hv, sv := output.HeaderViewOf("v9.9.9", h), output.StatusViewOf(rs, l)
	want := base
	want.Header, want.Context, want.Host, want.Risk, want.Status, want.Info = hv.Header, hv.Context, hv.Host, sv.Risk, sv.Status, sv.Info
	if !reflect.DeepEqual(got, want) || got.Alert != nil {
		t.Errorf("MainMenuOf = %+v\nwant %+v", got, want)
	}
	if got.Host.Hostname != "web-01" || got.Context.Codename != "Trixie" || got.Header.Version != "v9.9.9" || len(got.Status.Rows) != len(risk.Rows())-2 ||
		len(got.Info.Rows) != 1 || got.Info.Rows[0].Label != "DNS" || got.Info.Rows[0].Detail != "192.0.2.1" {
		t.Errorf("header %+v, bar %+v, host %+v, %d rows", got.Header, got.Context, got.Host, len(got.Status.Rows))
	}
	again := screen.MainMenuFixture()
	again.Header.Version = "v9.9.9"
	if !reflect.DeepEqual(base, again) {
		t.Error("MainMenuOf changed its base")
	}
}

// TestAlertOf checks the main menu's alert: each established
// CVE-2026-123158 factor of SSH Auth raises it, with the approved words and
// the route to SSH, and is then no line under the SSH row, which SSH Auth
// is drawn in, while the
// risk level, its band and gloss, stays l's; whether sshd is patched not
// established stays a line, with no alert; l is not changed; and
// StatusViewOf over l keeps the line: only AlertOf drops it.
func TestAlertOf(t *testing.T) {
	want := &screen.Alert{Title: "CVE-2026-123158", Fix: "Upgrade openssh.", Item: "SSH"}
	for _, c := range []struct {
		code  string
		alert bool
		line  string
	}{
		{ssh.CodePasswordBypass, true, "Anyone can log in: upgrade openssh"},
		{ssh.CodeSSHDUnfixed, true, "sshd unpatched: upgrade openssh"},
		{ssh.CodeSSHDUnfixedLatent, true, "sshd unpatched: upgrade openssh"},
		{ssh.CodeFixUnknown, false, "Can't tell if sshd is patched"},
	} {
		rs := replaced(figmaReports(), row("ssh.auth", "SSH Auth", engine.Configured, "key-only auth", c.code))
		l := risk.Assess(rs, upFor(time.Hour))
		before := slices.Clone(l.Factors)
		got := output.MainMenuOf(screen.MainMenuFixture(), upFor(time.Hour), rs, l)
		if !slices.Equal(l.Factors, before) {
			t.Errorf("%s: MainMenuOf changed l's factors", c.code)
		}
		if c.alert != (got.Alert != nil) || c.alert && *got.Alert != *want {
			t.Errorf("%s: alert %+v, want one: %v", c.code, got.Alert, c.alert)
		}
		if got.Risk != output.RiskSummaryOf(l) {
			t.Errorf("%s: risk %+v, want l's %+v", c.code, got.Risk, output.RiskSummaryOf(l))
		}
		var lines []string
		for _, r := range got.Status.Rows {
			for _, x := range r.Lines {
				lines = append(lines, r.Label+": "+x.Text)
			}
		}
		if has := slices.Contains(lines, "SSH: "+c.line); has == c.alert {
			t.Errorf("%s: lines %q; the line drawn: %v, want %v", c.code, lines, has, !c.alert)
		}
		var kept bool
		for _, r := range output.StatusViewOf(rs, l).Status.Rows {
			kept = kept || r.Label == "SSH" && slices.ContainsFunc(r.Lines, func(x component.StatusLine) bool { return x.Text == c.line })
		}
		if !kept {
			t.Errorf("%s: StatusViewOf lost SSH Auth's line %q", c.code, c.line)
		}
		_, rest := output.AlertOf(l)
		if rest.Band != l.Band || rest.Bypass != l.Bypass || rest.Forced != l.Forced || len(rest.Factors) != len(l.Factors)-btoi(c.alert) {
			t.Errorf("%s: rest %+v of %+v", c.code, rest, l)
		}
	}
	if a, _ := output.AlertOf(risk.Level{}); a != nil {
		t.Errorf("no factor: alert %+v", a)
	}
}

func btoi(b bool) int {
	if b {
		return 1
	}
	return 0
}

// TestNoticeOf: the main menu's end-of-support notice is none before 90
// days to the release's last day, "ends" up to that day and "ended" after
// it, for good; it names the newest supported release to upgrade to, none
// from that release; says that hardn refuses every change where the
// engine's gate does, and only there; and is none for a release with no
// date or no row. It reads the platform and the day alone.
func TestNoticeOf(t *testing.T) {
	classify := func(id, v string) detect.Platform {
		return detect.Classify(detect.OSRelease{ID: id, VersionID: v}, detect.Proxmox{Installed: detect.No})
	}
	for _, tt := range []struct {
		p     detect.Platform
		today string
		want  *screen.Alert
	}{
		{classify("alpine", "3.22.4"), "2027-01-30", nil},
		{classify("alpine", "3.22.4"), "2027-01-31", &screen.Alert{Title: "Alpine 3.22 support ends 2027-05-01", Fix: "Upgrade to Alpine 3.24."}},
		{classify("alpine", "3.22.4"), "2027-05-01", &screen.Alert{Title: "Alpine 3.22 support ends 2027-05-01", Fix: "Upgrade to Alpine 3.24."}},
		{classify("alpine", "3.22.4"), "2027-05-02", &screen.Alert{Title: "Alpine 3.22 support ended 2027-05-01", Fix: "Upgrade to Alpine 3.24."}},
		{classify("alpine", "3.22.4"), "2031-01-01", &screen.Alert{Title: "Alpine 3.22 support ended 2027-05-01", Fix: "Upgrade to Alpine 3.24."}},
		{classify("alpine", "3.24.1"), "2028-05-30", &screen.Alert{Title: "Alpine 3.24 support ends 2028-06-01"}},
		{classify("alpine", "3.20.9"), "2026-11-13", &screen.Alert{Title: "Alpine 3.20 support ended 2026-04-01", Why: screen.OutOfScopeRefused, Fix: "Upgrade to Alpine 3.24."}},
		{classify("ubuntu", "22.04"), "2027-05-31", &screen.Alert{Title: "Ubuntu 22.04 support ends May 2027", Fix: "Upgrade to Ubuntu 26.04."}},
		{classify("ubuntu", "22.04"), "2027-06-01", &screen.Alert{Title: "Ubuntu 22.04 support ended May 2027", Fix: "Upgrade to Ubuntu 26.04."}},
		{classify("rocky", "9.8"), "2032-05-01", &screen.Alert{Title: "Rocky Linux 9 support ends 2032-05-31", Fix: "Upgrade to Rocky Linux 10."}},
		{classify("almalinux", "9.8"), "2032-06-01", &screen.Alert{Title: "AlmaLinux 9 support ended 2032-05-31", Fix: "Upgrade to AlmaLinux 10."}},
		{classify("rhel", "10.3"), "2035-05-01", &screen.Alert{Title: "RHEL 10 support ends 2035-05-31"}},
		{detect.Platform{Distro: detect.DistroProxmox, Version: "8", Support: detect.OutOfScope, Ends: "2026-08"}, "2026-11-13",
			&screen.Alert{Title: "Proxmox VE 8 support ended August 2026", Why: screen.OutOfScopeRefused, Fix: "Upgrade to Proxmox VE 9."}},
		{detect.Platform{Distro: detect.DistroProxmox, Version: "9", Support: detect.Supported}, "2040-01-01", nil},
		{classify("alpine", "3.19.9"), "2031-01-01", nil},
		{classify("ubuntu", "25.10"), "2031-01-01", nil},
		{detect.Platform{}, "2031-01-01", nil},
	} {
		today, err := time.Parse(time.DateOnly, tt.today)
		if err != nil {
			t.Fatal(err)
		}
		got := output.NoticeOf(tt.p, today)
		if (got == nil) != (tt.want == nil) || got != nil && *got != *tt.want {
			t.Errorf("%s %s on %s: %+v, want %+v", tt.p.Distro, tt.p.Version, tt.today, got, tt.want)
		}
	}
}
