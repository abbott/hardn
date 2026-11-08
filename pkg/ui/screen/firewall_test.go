package screen

import (
	"strings"
	"testing"

	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"

	"github.com/abbott/hardn/pkg/ui/component"
	"github.com/abbott/hardn/pkg/ui/term"
)

// The Firewall screens as pkg/screens/firewall fills them; the hosts behind
// each are that package's goldens. Synthetic values only.
var (
	fwTestStatus = component.StatusList{Rows: []component.StatusRow{{Label: "Firewall", State: component.StatusPartial, Detail: "ssh blocked"}}}
	fwTestUFW    = []component.KeyValue{{Key: "Installed", Value: "Yes", State: component.ValueOK}, {Key: FirewallStatusKey, Value: "Active", State: component.ValueOK},
		{Key: FirewallPolicyKey, Value: "Deny", State: component.ValueOK}, {Key: FirewallBootKey, Value: "Yes", State: component.ValueOK}}
	fwTestSSH     = []component.KeyValue{{Key: "[::]:2230", Value: "Refused", State: component.ValueBad}, {Key: "0.0.0.0:2230", Value: "Refused", State: component.ValueBad}}
	fwTestRules   = []component.KeyValue{{Key: "443/tcp", Value: "Allow", State: component.ValueOK}, {Key: "443/tcp (v6)", Value: "Allow", State: component.ValueOK}}
	fwTestLockout = Firewall{Status: fwTestStatus, UFW: fwTestUFW, SSH: fwTestSSH, Rules: fwTestRules, First: FirewallLetSSH, Risk: true, Installed: true, DisableBeside: true}
	fwTestNoUFW   = Firewall{Status: fwTestStatus, UFW: []component.KeyValue{{Key: "Installed", Value: "No", State: component.ValueWarn}}, SSH: fwTestSSH, First: FirewallInstall}
	fwTestPVE     = FirewallPVE{Status: fwTestStatus, Rows: []component.KeyValue{{Key: "Service", Value: "Running"}, {Key: "Enabled", Value: "No"},
		{Key: FirewallPolicyKey, Value: "Drop"}, {Key: "SSH (22)", Value: "Not filtered"}, {Key: "Web UI (8006)", Value: "Not filtered"}}}
	fwTestProfiles = FirewallProfiles{Rows: []component.KeyValue{{Key: "WebServer", Value: "80/tcp, 443/tcp", Note: "Not applied", State: component.ValueWarn},
		{Key: "Metrics", Value: "9100/tcp", Note: "Applied", State: component.ValueOK}}, Any: true}
	fwTestNoProfiles = FirewallProfiles{Rows: []component.KeyValue{{Key: "No application profiles configured", State: component.ValueNone}}}
)

// TestFirewallHeights (from the gallery's, whose counts these are, §4): each
// screen at 80x24 and at 80x40, in Unicode and ASCII, is as many lines at either
// height, the prompt on its last: over 24 rows the title and the Firewall row
// scroll off (decision 9). pkg/screens/firewall's holds every host's.
func TestFirewallHeights(t *testing.T) {
	draws := map[string]struct {
		vm    SubMenu
		lines int
	}{
		"lockout": {fwTestLockout.SubMenu(), 32}, "no ufw": {fwTestNoUFW.SubMenu(), 20}, "proxmox": {fwTestPVE.SubMenu(), 17},
		"profiles": {fwTestProfiles.SubMenu(), 16}, "no profiles": {fwTestNoProfiles.SubMenu(), 13},
	}
	for name, d := range draws {
		for _, rows := range []int{24, 40} {
			for _, unicode := range []bool{true, false} {
				env := term.Fixed(80, colorprofile.TrueColor, unicode, true)
				env.Height = rows
				lines := strings.Split(ansi.Strip(d.vm.Render(component.NewCtx(env))), "\n")
				if len(lines) != d.lines || !strings.HasSuffix(lines[len(lines)-1], " or q]:") {
					t.Errorf("%s at 80x%d, unicode %v: %d lines, the last %q; want %d, the prompt", name, rows, unicode, len(lines), lines[len(lines)-1], d.lines)
				}
			}
		}
	}
}

// TestFirewallItems: item 1 by what it does, marked where lockout-risk; Profiles
// where ufw is installed, keeping 2; Disable beside item 1, keeping 3; Proxmox
// VE's Return alone; the profiles' Remove and Apply only with a profile.
func TestFirewallItems(t *testing.T) {
	items := func(vm SubMenu) string {
		var out []string
		for _, it := range vm.Menu.Items {
			out = append(out, it.Key+" "+it.Label)
		}
		return strings.Join(out, ", ")
	}
	for _, tt := range []struct {
		name string
		vm   SubMenu
		want string
	}{
		{"lockout", fwTestLockout.SubMenu(), "1 Let SSH through, 2 Application Profiles, 3 Trusted Interfaces, 4 Disable firewall"},
		{"no ufw", fwTestNoUFW.SubMenu(), "1 Install UFW"},
		{"disable", Firewall{First: FirewallDisableFirst, Installed: true}.SubMenu(), "1 Disable firewall, 2 Application Profiles, 3 Trusted Interfaces"},
		{"repair", Firewall{First: FirewallRepair, Installed: true, DisableBeside: true}.SubMenu(), "1 Repair firewall, 2 Application Profiles, 3 Trusted Interfaces, 4 Disable firewall"},
		{"proxmox", fwTestPVE.SubMenu(), ""},
		{"profiles", fwTestProfiles.SubMenu(), "1 Add profile, 2 Remove profile, 3 Apply profiles"},
		{"no profiles", fwTestNoProfiles.SubMenu(), "1 Add profile"},
		{"interfaces", FirewallInterfaces{Any: true}.SubMenu(), "1 Add interface, 2 Remove interface, 3 Apply interfaces"},
		{"no interfaces", FirewallInterfaces{}.SubMenu(), "1 Add interface"},
	} {
		if got := items(tt.vm); got != tt.want {
			t.Errorf("%s: %q, want %q", tt.name, got, tt.want)
		}
	}
	if !fwTestLockout.SubMenu().Menu.Items[0].Risk {
		t.Error("lockout: item 1 not marked Lockout risk")
	}
}

// TestFirewallChanges: each change's question and title by what item 1 does; the
// review as its plan leaves ufw.
func TestFirewallChanges(t *testing.T) {
	for first, want := range map[FirewallFirst][2]string{
		FirewallInstall: {"Install UFW", "Install ufw?"}, FirewallEnable: {"Enable Firewall", "Enable the firewall?"},
		FirewallSetDeny: {"Set Deny Policy", "Set the deny policy?"}, FirewallLetSSH: {"Let SSH Through", "Let SSH through?"},
		FirewallLetSSHBoot: {"Let SSH Through", "Let SSH through?"}, FirewallStartBoot: {"Start at Boot", "Start ufw at boot?"},
		FirewallDisableFirst: {"Disable Firewall", "Disable the firewall?"},
	} {
		c := Firewall{First: first}.FirstChange()
		if c.Step.Title.Title != want[0] || c.Question != want[1] {
			t.Errorf("%d: %q, %q; want %q", first, c.Step.Title.Title, c.Question, want)
		}
	}
	if c := (Firewall{First: FirewallRepair}).FirstChange(); c.Question != "" {
		t.Errorf("repair has a flow: %+v", c)
	}
	if got := (Firewall{First: FirewallLetSSH, KeepsReject: true}).FirstChange().Review.Items[1]; got.Key != FirewallPolicyKey || got.Value != "Reject" {
		t.Errorf("a reject policy kept: %+v", got)
	}
	if c := FirewallDisableChange(); c.Review.Notice == nil || c.Review.Notice.Callout.Body[0] != firewallDisableWarning {
		t.Errorf("Disable's warning: %+v", c.Review)
	}
	if r := FirewallAddReview("Tailscale", []string{"41641/udp"}, "tailscale0"); r.Items[0].Value != "41641/udp on tailscale0" {
		t.Errorf("add review on an interface: %+v", r)
	}
	if r := FirewallAddReview("Web", []string{"80/tcp", "443/tcp"}, ""); r.Items[0].Value != "80/tcp, 443/tcp" {
		t.Errorf("Add's review: %+v", r)
	}
}
