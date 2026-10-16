package component

import (
	"fmt"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"

	"github.com/abbott/hardn/pkg/ui/term"
	"github.com/abbott/hardn/pkg/ui/theme"
	"github.com/abbott/hardn/pkg/ui/uitest"
)

// renderer is any component's view-model.
type renderer interface{ Render(Ctx) string }

const (
	repo   = "https://github.com/abbott/hardn"
	uptime = "182 days, 2 hours, 26 minutes"
)

// Synthetic fixtures; BOS-SAN-L01 is Figma's mock hostname.
var (
	figmaHost  = HostBlock{Hostname: "BOS-SAN-L01", Address: "192.0.2.10", Uptime: uptime}
	figmaRisk  = RiskSummary{Band: RiskModerate, Level: "Moderate", Gloss: "medium security"}
	longHost   = "ci-runner-0042.build-cluster.eu-west-1.web-01.example.net"
	riskFactor = []string{
		"AppArmor not configured",
		"Auto Updates not configured",
		"Uptime of 182 days with automatic updates not configured",
	}
)

// states is every component state, by golden name: <component>/<state>.
var states = []struct {
	name string
	vm   renderer
}{
	{"header/normal", Header{Version: "v0.4.0", URL: repo}},
	{"header/no-version", Header{URL: repo}},
	{"header/no-url", Header{Version: "v0.4.0"}},
	{"contextbar/normal", ContextBar{Name: "Debian", Version: "12", Codename: "Bookworm"}},
	{"contextbar/unknown", ContextBar{}},
	{"contextbar/no-codename", ContextBar{Name: "Alpine Linux", Version: "3.24"}},
	{"contextbar/long", ContextBar{Name: "Proxmox Virtual Environment", Version: "9.0.10", Codename: "Trixie, vendor build"}},
	{"titlebar/normal", TitleBar{Title: "User Management"}},
	{"titlebar/long", TitleBar{Title: "Firewall application profiles and port rules for web-01.example.net"}},
	{"hostblock/normal", figmaHost},
	{"hostblock/unknown-address", HostBlock{Hostname: "BOS-SAN-L01", Uptime: uptime}},
	{"hostblock/unknown-uptime", HostBlock{Hostname: "BOS-SAN-L01", Address: "192.0.2.10"}},
	{"hostblock/no-hostname", HostBlock{Address: "192.0.2.10", Uptime: uptime}},
	{"hostblock/long-hostname", HostBlock{Hostname: longHost, Address: "192.0.2.10", Uptime: uptime}},
	{"hostblock/reboot-pending", HostBlock{Hostname: "BOS-SAN-L01", Address: "192.0.2.10", Uptime: uptime, RebootPending: true}},
	{"risksummary/moderate", figmaRisk},
	{"risksummary/low", RiskSummary{Band: RiskLow, Level: "Low", Gloss: "strong security"}},
	{"risksummary/high", RiskSummary{Band: RiskHigh, Level: "Critical", Gloss: "no security"}},
	{"risksummary/unknown", RiskSummary{}},
	{"risksummary/factors", RiskSummary{Band: RiskModerate, Level: "Moderate", Gloss: "medium security", Factors: riskFactor}},
}

// maxWidth is the widest a line of vm may be at terminal width w, worked
// out from the §3.15 constants rather than from Ctx: the content width
// under theme.CapContent, and for a bar the terminal's width when
// theme.BarsFillContent is off.
func maxWidth(vm renderer, w int) int {
	switch vm.(type) {
	case ContextBar, TitleBar:
		if !theme.BarsFillContent {
			return w
		}
	}
	if theme.CapContent {
		return min(w, theme.ContentWidth)
	}
	return w
}

// TestGolden pins every state on the full component matrix.
func TestGolden(t *testing.T) {
	for _, s := range states {
		t.Run(s.name, func(t *testing.T) {
			uitest.Golden(t, s.name, uitest.Full(), func(env term.Env) string {
				return s.vm.Render(NewCtx(env))
			})
		})
	}
}

// TestOutputRules renders every state at every width from 1 to 130, with
// colour, without colour and in plain mode, and checks the package's
// output rules: no line wider than maxWidth, no line holding a tab, no
// trailing newline, and no trailing space outside a painted background.
func TestOutputRules(t *testing.T) {
	for _, s := range states {
		for w := 1; w <= 130; w++ {
			for _, env := range []term.Env{
				term.Fixed(w, colorprofile.TrueColor, true, true),
				term.Fixed(w, colorprofile.ASCII, true, false),
				term.Fixed(w, colorprofile.NoTTY, false, true),
			} {
				out := s.vm.Render(NewCtx(env))
				where := fmt.Sprintf("%s, width %d, %v", s.name, w, env.Profile)
				if out == "" || strings.HasSuffix(out, "\n") {
					t.Errorf("%s: output %q is empty or ends in a newline", where, out)
				}
				limit := maxWidth(s.vm, w)
				for _, l := range strings.Split(out, "\n") {
					if lw := lipgloss.Width(l); lw > limit || strings.Contains(l, "\t") {
						t.Errorf("%s: line %q is %d cells or holds a tab", where, ansi.Strip(l), lw)
					}
					if strings.HasSuffix(l, " ") || !env.HasColor() && strings.HasSuffix(ansi.Strip(l), " ") {
						t.Errorf("%s: line %q ends in a space", where, ansi.Strip(l))
					}
				}
			}
		}
	}
}

// TestLayout checks the grid on plain text: columns, stacking, the marker,
// the bar, cuts and the Unknown word. The cases that test a §3.15 rule
// take their widths and fills from its constant, and the others use widths
// at which no such rule changes the result, so that refusing a rule
// (CapContent or BarsFillContent false, StackBelow 0) changes only the
// constants and the goldens.
func TestLayout(t *testing.T) {
	host := HostBlock{Hostname: "web-01", Address: "192.0.2.10", Uptime: "3 hours, 5 minutes"}
	// below is the widest terminal that stacks, under 1 when nothing
	// stacks; from is the narrowest that does not, or ContentWidth when
	// nothing stacks.
	below, from := theme.StackBelow-1, theme.StackBelow
	if from < 1 {
		from = theme.ContentWidth
	}
	tests := []struct {
		name    string
		vm      renderer
		width   int
		unicode bool
		want    string
	}{
		{"header: URL at the value column", Header{Version: "v0.4.0", URL: repo}, 80, false,
			"hardn  v0.4.0              " + repo},
		{"header: stacks below StackBelow", Header{Version: "v0.4.0", URL: repo}, below, false,
			"hardn  v0.4.0\n" + repo},
		{"header: a URL too long for its column moves down", Header{URL: repo + "/tree/main/docs/design-system"}, 80, false,
			"hardn\n" + repo + "/tree/main/docs/design-system"},
		{"host: side by side from StackBelow", host, from, false,
			"  [web-01]                 192.0.2.10\n                           3 hours, 5 minutes"},
		{"host: stacked below StackBelow", host, below, false,
			"  [web-01]\n      192.0.2.10\n      3 hours, 5 minutes"},
		{"host: a badge reaching the value column stacks", HostBlock{Hostname: "web-01.staging.example.net", Address: "192.0.2.10", Uptime: "5 minutes"}, 80, false,
			"  [web-01.staging.example.net]\n      192.0.2.10\n      5 minutes"},
		{"host: unknown values", HostBlock{}, 80, false,
			"  Unknown                  Unknown\n                           Unknown"},
		{"risk: a Unicode icon is centred in the marker column", figmaRisk, 80, true,
			"   ⛬  Risk Level           Moderate ≈ medium security"},
		{"risk: an ASCII icon fills it", RiskSummary{Band: RiskBand(9)}, 80, false,
			"  [*] Risk Level           Unknown"},
		{"bar: fills its width", ContextBar{Name: "Debian"}, 120, false,
			"# Debian " + strings.Repeat("#", maxWidth(ContextBar{}, 120)-9)},
		{"bar: long text is cut", TitleBar{Title: strings.Repeat("x", 70)}, 60, false,
			"# " + strings.Repeat("x", 53) + "... #"},
		{"bar: no text is all fill", TitleBar{}, 60, true, strings.Repeat("█", 60)},
		{"bar: view-model text is cleaned", ContextBar{Name: " Deb\x1b[31mian\x00\t", Version: "1\n2"}, 60, false,
			"# Debian 1 2 " + strings.Repeat("#", 47)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.width < 1 {
				t.Skip("theme.StackBelow is 0: nothing stacks")
			}
			got := ansi.Strip(tt.vm.Render(NewCtx(term.Fixed(tt.width, colorprofile.ASCII, tt.unicode, true))))
			if got != tt.want {
				t.Errorf("got\n%s\nwant\n%s", got, tt.want)
			}
		})
	}
}
