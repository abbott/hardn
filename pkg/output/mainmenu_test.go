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

// TestMainMenuOf checks that MainMenuOf fills the five blocks the status
// model owns as HeaderViewOf and StatusViewOf map them, with base's
// version, and keeps the rest of base, which it does not change.
func TestMainMenuOf(t *testing.T) {
	h := upFor(182*24*time.Hour + 2*time.Hour + 26*time.Minute)
	h.OS = header.OS{Name: "Debian", Version: "13", Codename: "trixie"}
	h.Hostname = header.Hostname{Name: "web-01"}
	h.Address.IPv4 = header.Family{Route: detect.Yes, Iface: "eth0", Addr: netip.MustParseAddr("192.0.2.20")}
	rs := figmaReports()
	l := risk.Assess(rs, h)
	base := screen.MainMenuFixture()
	base.Header.Version = "v9.9.9"

	got := output.MainMenuOf(base, h, rs, l)
	hv, sv := output.HeaderViewOf("v9.9.9", h), output.StatusViewOf(rs, l)
	want := base
	want.Header, want.Context, want.Host, want.Risk, want.Status = hv.Header, hv.Context, hv.Host, sv.Risk, sv.Status
	if !reflect.DeepEqual(got, want) || got.Alert != nil {
		t.Errorf("MainMenuOf = %+v\nwant %+v", got, want)
	}
	if got.Host.Hostname != "web-01" || got.Context.Codename != "Trixie" || got.Header.Version != "v9.9.9" || len(got.Status.Rows) != len(risk.Rows()) {
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
// the route to SSH Login, and is then no line under SSH Auth, while the
// risk level, its band and gloss, stays l's; whether sshd is patched not
// established stays a line, with no alert; l is not changed; and
// StatusViewOf over l keeps the line: only AlertOf drops it.
func TestAlertOf(t *testing.T) {
	want := &screen.Alert{Title: "CVE-2026-123158", Fix: "Upgrade openssh.", Item: "SSH Login"}
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
		if has := slices.Contains(lines, "SSH Auth: "+c.line); has == c.alert {
			t.Errorf("%s: lines %q; the line drawn: %v, want %v", c.code, lines, has, !c.alert)
		}
		var kept bool
		for _, r := range output.StatusViewOf(rs, l).Status.Rows {
			kept = kept || r.Label == "SSH Auth" && slices.ContainsFunc(r.Lines, func(x component.StatusLine) bool { return x.Text == c.line })
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
