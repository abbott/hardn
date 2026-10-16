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
	figmaHost = HostBlock{Hostname: "BOS-SAN-L01", Address: "192.0.2.10", Uptime: uptime}
	figmaRisk = RiskSummary{Band: RiskModerate, Level: "Moderate", Gloss: "medium security"}
	longHost  = "ci-runner-0042.build-cluster.eu-west-1.web-01.example.net"
	// riskFactor fits the 35 cells a factor has beside the label (DEV-022);
	// longFactor does not.
	riskFactor = []string{
		"AppArmor not configured",
		"Auto Updates not configured",
		"Up 182 days without auto updates",
	}
	longFactor = "Uptime of 182 days with automatic updates not configured"
	// figmaStatus is Figma's main-menu status rows, with port 2230.
	figmaStatus = StatusList{Rows: []StatusRow{
		{Label: "User", State: StatusConfigured, Detail: "non-root, sudo"},
		{Label: "Firewall", State: StatusConfigured, Detail: "deny policy"},
		{Label: "SSH Login", State: StatusConfigured, Detail: "root disallowed"},
		{Label: "SSH Auth", State: StatusConfigured, Detail: "key-only auth"},
		{Label: "SSH Port", State: StatusConfigured, Detail: "non-default (2230)"},
		{Label: "AppArmor", State: StatusNotConfigured},
		{Label: "Auto Updates", State: StatusNotConfigured},
	}}
	sshPort = StatusRow{Label: "SSH Port", State: StatusConfigured, Detail: "non-default (2230)"}
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
	{"titlebar/empty", TitleBar{}},
	{"hostblock/normal", figmaHost},
	{"hostblock/unknown-address", HostBlock{Hostname: "BOS-SAN-L01", Uptime: uptime}},
	{"hostblock/unknown-uptime", HostBlock{Hostname: "BOS-SAN-L01", Address: "192.0.2.10"}},
	{"hostblock/no-hostname", HostBlock{Address: "192.0.2.10", Uptime: uptime}},
	{"hostblock/long-hostname", HostBlock{Hostname: longHost, Address: "192.0.2.10", Uptime: uptime}},
	{"hostblock/reboot-pending", HostBlock{Hostname: "BOS-SAN-L01", Address: "192.0.2.10", Uptime: uptime, RebootPending: true}},
	{"hostblock/all-unknown", HostBlock{}},
	{"risksummary/moderate", figmaRisk},
	{"risksummary/low", RiskSummary{Band: RiskLow, Level: "Low", Gloss: "strong security"}},
	{"risksummary/high", RiskSummary{Band: RiskHigh, Level: "Critical", Gloss: "no security"}},
	{"risksummary/unknown", RiskSummary{}},
	{"risksummary/factors", RiskSummary{Band: RiskModerate, Level: "Moderate", Gloss: "medium security", Factors: riskFactor}},
	{"risksummary/factors-no-gloss", RiskSummary{Band: RiskModerate, Level: "Moderate", Factors: riskFactor}},
	{"risksummary/long-factor", RiskSummary{Band: RiskModerate, Level: "Moderate", Gloss: "medium security", Factors: []string{longFactor}}},
	{"statusrow/configured", sshPort},
	{"statusrow/not-configured", StatusRow{Label: "AppArmor", State: StatusNotConfigured}},
	{"statusrow/partial", StatusRow{Label: "Auto Updates", State: StatusPartial, Detail: "crond not enabled"}},
	{"statusrow/not-applicable", StatusRow{Label: "AppArmor", State: StatusNotApplicable, Detail: "container"}},
	{"statusrow/unknown", StatusRow{Label: "SSH Port", Detail: "sshd -T failed"}},
	{"statusrow/word", StatusRow{Label: "SSH Port", State: StatusConfigured, Word: "2230/tcp", Detail: "non-default"}},
	{"statusrow/long-label", StatusRow{Label: "Unattended Upgrades Mail Reports", State: StatusConfigured, Detail: "root"}},
	{"statusrow/long-detail", StatusRow{Label: "SSH Auth", State: StatusNotConfigured, Detail: "password auth also allowed for alice and bob"}},
	{"statuslist/normal", figmaStatus},
	{"statuslist/empty", StatusList{}},
	{"divider/normal", Divider{}},
	{"modeindicator/live", ModeIndicator{}},
	{"modeindicator/dry-run", ModeIndicator{DryRun: true}},
}

// maxWidth is the widest a line of vm may be at terminal width w, worked
// out from the §3.15 constants rather than from Ctx: the content width
// under theme.CapContent, and for a bar or the divider the terminal's
// width when theme.BarsFillContent is off.
func maxWidth(vm renderer, w int) int {
	switch vm.(type) {
	case ContextBar, TitleBar, Divider:
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

// empty reports whether vm renders nothing by design: a list with no
// items.
func empty(vm renderer) bool {
	l, ok := vm.(StatusList)
	return ok && len(l.Rows) == 0
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
				if out == "" && !empty(s.vm) || strings.HasSuffix(out, "\n") {
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
	// cut is the room a status detail has beside the label at width from,
	// after "Configured " and the three-cell ASCII ellipsis.
	cut := maxWidth(StatusRow{}, from) - theme.ValueCol - len("Configured ") - 3
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
		{"host: zero-width values are unknown", HostBlock{Hostname: "\u200b", Address: "\u200b", Uptime: "\u200d"}, 80, false,
			"  Unknown                  Unknown\n                           Unknown"},
		{"risk: a Unicode icon is centred in the marker column", figmaRisk, 80, true,
			"   ⛬  Risk Level           Moderate ≈ medium security"},
		{"risk: an ASCII icon fills it", RiskSummary{Band: RiskBand(9)}, 80, false,
			"  [*] Risk Level           Unknown"},
		{"bar: fills its width", ContextBar{Name: "Debian"}, 120, false,
			"# Debian " + strings.Repeat("#", maxWidth(ContextBar{}, 120)-9)},
		{"bar: long text is cut", TitleBar{Title: strings.Repeat("x", 70)}, 60, false,
			"# " + strings.Repeat("x", 53) + "... #"},
		{"bar: a cut between spans drops the spaces before it", ContextBar{Name: "Debian", Version: "12", Codename: "Bookworm"}, 17, false,
			"# Debian 12... ##"},
		{"bar: no text is all fill", TitleBar{}, 60, true, strings.Repeat("█", 60)},
		{"bar: view-model text is cleaned", ContextBar{Name: " Deb\x1b[31mian\x00\t", Version: "1\n2"}, 60, false,
			"# Debian 1 2 " + strings.Repeat("#", 47)},
		{"status: side by side from StackBelow", sshPort, from, false,
			"  [+] SSH Port             Configured non-default (2230)"},
		{"status: a Unicode glyph is centred in the marker column", figmaStatus.Rows[0], from, true,
			"   ⛛  User                 Configured non-root, sudo"},
		{"status: stacked below StackBelow", sshPort, below, false,
			"  [+] SSH Port\n      Configured non-default (2230)"},
		{"status: each state has its word", StatusList{Rows: []StatusRow{
			{Label: "a", State: StatusNotConfigured}, {Label: "b", State: StatusPartial},
			{Label: "c", State: StatusNotApplicable}, {Label: "d", State: StatusState(9)},
		}}, from, false,
			"  [ ] a                    Not Configured\n  [*] b                    Partial\n" +
				"  [-] c                    Not Applicable\n  [?] d                    Unknown"},
		{"status: Word replaces the state's word", StatusRow{Label: "SSH Port", State: StatusConfigured, Word: " 2230/tcp ", Detail: "non-default"}, from, false,
			"  [+] SSH Port             2230/tcp non-default"},
		{"status: a long label is cut beside the value", StatusRow{Label: "Unattended Upgrades Mail Reports", State: StatusConfigured}, from, true,
			"   ⛛  Unattended Upgrades… Configured"},
		{"status: a cut label drops the spaces before the ellipsis", StatusRow{Label: "Security Updates Mail", State: StatusConfigured}, from, false,
			"  [+] Security Updates...  Configured"},
		{"status: a long label is whole when stacked", StatusRow{Label: "Unattended Upgrades Mail Reports", State: StatusConfigured}, below, false,
			"  [+] Unattended Upgrades Mail Reports\n      Configured"},
		{"status: a long detail is cut at the content width", StatusRow{Label: "SSH Auth", State: StatusConfigured, Detail: strings.Repeat("x", 60)}, from, false,
			"  [+] SSH Auth             Configured " + strings.Repeat("x", cut) + "..."},
		{"status: no label ends with the glyph", StatusRow{}, below, false,
			"  [?]\n      Unknown"},
		{"status: no label leaves the label column blank", StatusRow{}, from, true,
			"   ?                       Unknown"},
		{"status: an empty list is nothing", StatusList{}, 80, false, ""},
		{"divider: fills the bar width", Divider{}, 120, false, strings.Repeat("-", maxWidth(Divider{}, 120))},
		{"divider: Unicode rule", Divider{}, 60, true, strings.Repeat("─", 60)},
		{"mode: live at the value column", ModeIndicator{}, from, false,
			strings.Repeat(" ", theme.ValueCol) + "Dry-run Mode Disabled"},
		{"mode: dry run stacked", ModeIndicator{DryRun: true}, below, false,
			strings.Repeat(" ", theme.StackCol) + "Dry-run Mode Enabled"},
		{"mode: too narrow, the label is cut, not the word", ModeIndicator{}, min(below, 20), false,
			strings.Repeat(" ", theme.StackCol) + "Dr... Disabled"},
		{"mode: with no letter of the label left, the word alone", ModeIndicator{DryRun: true}, min(below, 17), false,
			strings.Repeat(" ", theme.StackCol) + "Enabled"},
		{"mode: the word moves left to stay whole", ModeIndicator{}, 10, false, "  Disabled"},
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

// TestModeWord checks that the ModeIndicator ends with its whole word, the
// one thing that tells the modes apart, at every width the word fits, in
// both glyph sets.
func TestModeWord(t *testing.T) {
	modes := []struct {
		vm   ModeIndicator
		word string
	}{{ModeIndicator{}, modeLive}, {ModeIndicator{DryRun: true}, modeDryRun}}
	for w := 1; w <= 130; w++ {
		for _, unicode := range []bool{true, false} {
			for _, m := range modes {
				got := ansi.Strip(m.vm.Render(NewCtx(term.Fixed(w, colorprofile.ASCII, unicode, true))))
				if w >= lipgloss.Width(m.word) && !strings.HasSuffix(got, m.word) {
					t.Errorf("width %d, unicode %v: got %q, want it to end with %q", w, unicode, got, m.word)
				}
			}
		}
	}
}

// TestCutStyle checks that a cut falling between spans ends the last span
// that keeps any text, in that span's style, as a cut inside a span does.
func TestCutStyle(t *testing.T) {
	c := NewCtx(term.Fixed(17, colorprofile.TrueColor, false, true))
	got := ContextBar{Name: "Debian", Version: "12", Codename: "Bookworm"}.Render(c)
	if want := c.Theme.ContextBar.Version.Render("12" + c.Theme.Glyphs.Ellipsis); !strings.Contains(got, want) {
		t.Errorf("got %q, want it to hold %q", got, want)
	}
}

// TestTruncate checks the plain-text cut: it drops the spaces before the
// ellipsis, as fit does, and shows as much of the ellipsis as fits when
// no text does.
func TestTruncate(t *testing.T) {
	tests := []struct {
		s       string
		limit   int
		unicode bool
		want    string
	}{
		{"web-01", 6, false, "web-01"},
		{"web-01.example.net", 10, false, "web-01...."},
		{"web-01.example.net", 10, true, "web-01.ex…"},
		{"Security Updates Mail", 20, false, "Security Updates..."},
		{"Security Updates Mail", 18, true, "Security Updates…"},
		{"   x", 3, false, "..."},
		{"web-01", 2, false, ".."},
		{"web-01", 0, true, ""},
	}
	for _, tt := range tests {
		c := NewCtx(term.Fixed(80, colorprofile.ASCII, tt.unicode, true))
		if got := c.truncate(tt.s, tt.limit); got != tt.want {
			t.Errorf("truncate(%q, %d), unicode %v = %q, want %q", tt.s, tt.limit, tt.unicode, got, tt.want)
		}
	}
}

// TestStatusStates checks that each state shows its own glyph and word,
// both in the state's style, and that no two states share both, so that
// no state rests on colour alone. A value outside the states is Unknown.
func TestStatusStates(t *testing.T) {
	c := NewCtx(term.Fixed(80, colorprofile.TrueColor, true, true))
	st, g := c.Theme.Status, c.Theme.Glyphs
	tests := []struct {
		state       StatusState
		glyph, word string
		style       lipgloss.Style
	}{
		{StatusConfigured, g.Configured, "Configured", st.Configured},
		{StatusNotConfigured, g.NotConfigured, "Not Configured", st.NotConfigured},
		{StatusPartial, g.Partial, "Partial", st.Partial},
		{StatusNotApplicable, g.NotApplicable, "Not Applicable", st.NotApplicable},
		{StatusUnknown, g.Unknown, "Unknown", st.Unknown},
		{StatusState(-1), g.Unknown, "Unknown", st.Unknown},
	}
	shown := map[string]bool{}
	for _, tt := range tests {
		got := StatusRow{Label: "SSH Port", State: tt.state}.Render(c)
		for _, want := range []string{tt.style.Render(tt.glyph), tt.style.Render(tt.word)} {
			if !strings.Contains(got, want) {
				t.Errorf("state %d: got %q, want it to hold %q", tt.state, got, want)
			}
		}
		if tt.state.String() != tt.word {
			t.Errorf("state %d: String() = %q, want %q", tt.state, tt.state.String(), tt.word)
		}
		shown[tt.glyph+" "+tt.word] = true
	}
	if len(shown) != 5 {
		t.Errorf("the five states show %d different glyph and word pairs, want 5: %v", len(shown), shown)
	}
}
