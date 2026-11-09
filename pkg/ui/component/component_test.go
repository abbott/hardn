package component_test

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"

	. "github.com/abbott/hardn/pkg/ui/component"
	"github.com/abbott/hardn/pkg/ui/component/fixture"
	"github.com/abbott/hardn/pkg/ui/keys"
	"github.com/abbott/hardn/pkg/ui/term"
	"github.com/abbott/hardn/pkg/ui/theme"
	"github.com/abbott/hardn/pkg/ui/uitest"
)

// The tests are in package component_test, with the component package
// dot-imported, because they take their fixtures from package fixture,
// which imports package component; export_test.go names what they test
// that the package does not export.

// renderer is any component's view-model.
type renderer interface{ Render(Ctx) string }

// animated renders its view-model on a terminal that can animate.
type animated = fixture.Animated

// The fixtures the goldens render are defined once, in package fixture,
// which the gallery (internal/tools/uigallery) shares. These names keep
// the tests' own.
const (
	repo   = fixture.URL
	sshKey = fixture.SSHKey
)

var (
	figmaRisk      = fixture.FigmaRisk()
	figmaStatus    = fixture.FigmaStatus()
	sshPort        = fixture.SSHPort()
	figmaMenu      = fixture.FigmaMenu()
	compactMenu    = fixture.CompactMenu()
	sshMenu        = fixture.SSHMenu()
	figmaHeading   = fixture.FigmaHeading()
	figmaPrompt    = fixture.FigmaPrompt()
	figmaUser      = fixture.FigmaUser()
	figmaChecklist = fixture.FigmaChecklist()
	figmaWarning   = fixture.FigmaWarning()
	users          = fixture.Users()
	// quit is the navigation grammar's q. yesNo is a y/N question's
	// answers, no by default; yesDefault's default is yes.
	quit       = keys.Quit()
	yesNo      = keys.YesNo(false)
	yesDefault = keys.YesNo(true)
)

// withItem returns m with item i replaced by it.
func withItem(m Menu, i int, it MenuItem) Menu {
	m.Items = slices.Clone(m.Items)
	m.Items[i] = it
	return m
}

// maxWidth is the widest a line of vm may be at terminal width w, worked
// out from the §3.15 constants rather than from Ctx: the content width
// under theme.CapContent, and for a bar or the divider the terminal's
// width when theme.BarsFillContent is off.
func maxWidth(vm renderer, w int) int {
	switch vm.(type) {
	case ContextBar, TitleBar, Divider, SectionHeader:
		if !theme.BarsFillContent {
			return w
		}
	}
	if theme.CapContent {
		return min(w, theme.ContentWidth)
	}
	return w
}

// TestGolden pins every state of fixture.States on the full component
// matrix.
func TestGolden(t *testing.T) {
	for _, s := range fixture.States() {
		t.Run(s.Name, func(t *testing.T) {
			cells := uitest.Full()
			if _, ok := s.VM.(animated); ok {
				for i := range cells {
					cells[i].Env.Animate = true
				}
			}
			uitest.Golden(t, s.Name, cells, func(env term.Env) string {
				return s.VM.Render(NewCtx(env))
			})
		})
	}
}

// empty reports whether vm renders nothing by design: a list or menu with
// no items, a menu heading with neither title nor mode, and a block with
// nothing to show.
func empty(vm renderer) bool {
	switch v := vm.(type) {
	case StatusList:
		return len(v.Rows) == 0
	case Menu:
		return len(v.Items) == 0 && v.Exit == MenuItem{}
	case MenuHeading:
		return v == MenuHeading{}
	case Badge:
		return v.Text == ""
	case SubjectLine:
		return v.Subject == ""
	case Breadcrumb:
		return len(v.Items) == 0
	case KeyValue:
		return v == KeyValue{}
	case BulletList:
		return len(v.Items) == 0
	case CodeBlock:
		return len(v.Lines) == 0
	case Message:
		return v.Text == ""
	case Messages:
		for _, m := range v {
			if strings.TrimSpace(m.Text) != "" {
				return false
			}
		}
		return true
	case InlineResult:
		return v.Text == ""
	case AnsweredField:
		return v.Label == ""
	case FieldHead:
		return v.Title == ""
	case PlanView:
		return len(v.Changes) == 0
	case StepProgress:
		for _, st := range v.Steps {
			if st.Title != "" {
				return false
			}
		}
		return true
	}
	return false
}

// TestOutputRules renders every state of fixture.States at every width
// from 1 to 130, with colour, without colour and in plain mode, and checks
// the package's output rules: no line wider than maxWidth, no line holding
// a tab, no trailing newline, and no trailing space outside a painted
// background.
func TestOutputRules(t *testing.T) {
	for _, s := range fixture.States() {
		for w := 1; w <= 130; w++ {
			for _, env := range []term.Env{
				term.Fixed(w, colorprofile.TrueColor, true, true),
				term.Fixed(w, colorprofile.ASCII, true, false),
				term.Fixed(w, colorprofile.NoTTY, false, true),
			} {
				out := s.VM.Render(NewCtx(env))
				where := fmt.Sprintf("%s, width %d, %v", s.Name, w, env.Profile)
				if out == "" && !empty(s.VM) || strings.HasSuffix(out, "\n") {
					t.Errorf("%s: output %q is empty or ends in a newline", where, out)
				}
				limit := maxWidth(s.VM, w)
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
	// after "Configured " and the ASCII ellipsis.
	ascii := NewCtx(term.Fixed(from, colorprofile.ASCII, false, true))
	cut := maxWidth(StatusRow{}, from) - theme.ValueCol - lipgloss.Width("Configured ") - lipgloss.Width(ascii.Theme.Glyphs.Ellipsis)
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
		{"host: a pending reboot under the uptime", HostBlock{Hostname: "web-01", Address: "192.0.2.10", Uptime: "41 days", Reboot: RebootPending}, from, true,
			"  [web-01]                 192.0.2.10\n                           41 days\n                           - Reboot pending"},
		{"host: a reboot not established", HostBlock{Hostname: "web-01", Address: "192.0.2.10", Uptime: "41 days", Reboot: RebootUnknown}, from, false,
			"  [web-01]                 192.0.2.10\n                           41 days\n                           - Can't tell if reboot pending"},
		{"host: the reboot line stacked", HostBlock{Hostname: "web-01", Uptime: "41 days", Reboot: RebootPending}, below, false,
			"  [web-01]\n      Unknown\n      41 days\n      - Reboot pending"},
		{"host: no reboot line for a value outside the three", HostBlock{Hostname: "web-01", Address: "192.0.2.10", Uptime: "41 days", Reboot: RebootLine(9)}, from, false,
			"  [web-01]                 192.0.2.10\n                           41 days"},
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
		{"bar: no text is all fill", TitleBar{}, 60, true, strings.Repeat("░", 60)},
		{"bar: no title is all fill, whatever the parents", TitleBar{Parents: []string{"User Management"}}, 60, true, strings.Repeat("░", 60)},
		{"bar: the Unicode shade", ContextBar{Name: "Debian", Version: "12", Codename: "Bookworm"}, 80, true,
			"░ Debian 12 (Bookworm) " + strings.Repeat("░", maxWidth(ContextBar{}, 80)-23)},
		{"bar: a trail before the title", TitleBar{Title: "Manage User", Parents: []string{" User Management", ""}}, 80, false,
			"# User Management > Manage User " + strings.Repeat("#", maxWidth(TitleBar{}, 80)-32)},
		{"bar: too long, the middle parents go first", TitleBar{Title: "SSH Keys", Parents: []string{"User Management", "Manage User"}}, 40, false,
			"# User Management > ... > SSH Keys #####"},
		{"bar: then the first parent", TitleBar{Title: "SSH Keys", Parents: []string{"User Management", "Manage User"}}, 25, false,
			"# ... > SSH Keys ########"},
		{"bar: then the parents are dropped", TitleBar{Title: "SSH Keys", Parents: []string{"User Management", "Manage User"}}, 15, false,
			"# SSH Keys ####"},
		{"bar: and last the title is cut", TitleBar{Title: "Manage User", Parents: []string{"User Management"}}, 12, false,
			"# Manag... #"},
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
			"  [ ] a                    Not Configured\n  [ ] b                    Partial\n" +
				"  [-] c                    Not Applicable\n  [?] d                    Unknown"},
		{"status: Word replaces the state's word", StatusRow{Label: "SSH Port", State: StatusConfigured, Word: " 2230/tcp ", Detail: "non-default"}, from, false,
			"  [+] SSH Port             2230/tcp non-default"},
		{"status: a shared marker keeps the state's word, not Word", StatusList{Rows: []StatusRow{
			{Label: "Firewall", State: StatusPartial, Word: "Enabled", Detail: "no rules"},
			{Label: "Firewall", State: StatusNotConfigured, Word: "Enabled", Detail: "no rules"},
		}}, from, false,
			"  [ ] Firewall             Partial no rules\n  [ ] Firewall             Not Configured no rules"},
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
		{"status: lines under the row at the value column, an empty one skipped", StatusRow{Label: "User", State: StatusConfigured, Detail: "non-root, sudo",
			Lines: []StatusLine{{Text: "Second root account", Warn: true}, {Text: " \t"}, {Text: "Maybe no password to become root"}}}, from, true,
			"   ⛛  User                 Configured non-root, sudo\n" +
				"                           - Second root account\n                           - Maybe no password to become root"},
		{"status: lines under the stacked value, at its indent", StatusRow{Label: "User", State: StatusConfigured, Detail: "non-root, sudo",
			Lines: []StatusLine{{Text: "No password to become root", Warn: true}}}, below, false,
			"  [+] User\n      Configured non-root, sudo\n      - No password to become root"},
		{"status: a long line is cut at the content width, not wrapped", StatusRow{Label: "SSH Port", State: StatusConfigured,
			Lines: []StatusLine{{Text: strings.Repeat("x", 60)}}}, from, false,
			"  [+] SSH Port             Configured\n" + strings.Repeat(" ", theme.ValueCol) + "- " +
				strings.Repeat("x", maxWidth(StatusRow{}, from)-theme.ValueCol-2-lipgloss.Width(ascii.Theme.Glyphs.Ellipsis)) + "..."},
		{"status: a long line under a stacked value is cut at the content width", StatusRow{Label: "SSH Port", State: StatusConfigured,
			Lines: []StatusLine{{Text: strings.Repeat("x", 70)}}}, below, false,
			"  [+] SSH Port\n      Configured\n      - " +
				strings.Repeat("x", max(0, maxWidth(StatusRow{}, below)-theme.StackCol-2-lipgloss.Width(ascii.Theme.Glyphs.Ellipsis))) + "..."},
		{"status: a row's lines keep the rows one block", StatusList{Rows: []StatusRow{
			{Label: "AppArmor", Detail: "can't read", Lines: []StatusLine{{Text: "Counted as not configured"}}},
			{Label: "Auto Updates", State: StatusNotConfigured},
		}}, from, false,
			"  [?] AppArmor             Unknown can't read\n                           - Counted as not configured\n" +
				"  [ ] Auto Updates         Not Configured"},
		{"divider: fills the bar width", Divider{}, 120, false, strings.Repeat("-", maxWidth(Divider{}, 120))},
		{"divider: Unicode rule", Divider{}, 60, true, strings.Repeat("╌", 60)},
		{"mode: live at the value column", ModeIndicator{}, from, false,
			strings.Repeat(" ", theme.ValueCol) + "Mode Live"},
		{"mode: live names no source", ModeIndicator{Source: ModeSourceFile}, from, false,
			strings.Repeat(" ", theme.ValueCol) + "Mode Live"},
		{"mode: plan only set in the session names none", ModeIndicator{DryRun: true}, from, false,
			strings.Repeat(" ", theme.ValueCol) + "Mode Plan only"},
		{"mode: plan only from -n", ModeIndicator{DryRun: true, Source: ModeSourceFlag}, from, false,
			strings.Repeat(" ", theme.ValueCol) + "Mode Plan only (-n)"},
		{"mode: plan only from hardn.yml, stacked", ModeIndicator{DryRun: true, Source: ModeSourceFile}, below, false,
			strings.Repeat(" ", theme.StackCol) + "Mode Plan only (hardn.yml)"},
		// At fixed widths that stack, so that they also run with
		// theme.StackBelow refused: the room is then the value column's.
		{"mode: too narrow, the label gives way first", ModeIndicator{DryRun: true, Source: ModeSourceFile}, min(below, 30), false,
			strings.Repeat(" ", theme.StackCol) + "Plan only (hardn.yml)"},
		{"mode: the source stays down to 27 columns", ModeIndicator{DryRun: true, Source: ModeSourceFile}, min(below, 27), false,
			strings.Repeat(" ", theme.StackCol) + "Plan only (hardn.yml)"},
		{"mode: then the source, the word kept", ModeIndicator{DryRun: true, Source: ModeSourceFile}, min(below, 26), false,
			strings.Repeat(" ", theme.StackCol) + "Plan only"},
		{"mode: the word moves left to stay whole", ModeIndicator{DryRun: true, Source: ModeSourceFlag}, 10, false, " Plan only"},
		{"mode: the word is cut last", ModeIndicator{DryRun: true, Source: ModeSourceFlag}, 5, false, "Pl..."},
		{"heading: the mode at the value column", figmaHeading, from, false,
			"  Select an option         Mode Live"},
		{"heading: plan only and its source beside it", MenuHeading{Title: "Select an option", Mode: &ModeIndicator{DryRun: true, Source: ModeSourceFile}}, from, false,
			"  Select an option         Mode Plan only (hardn.yml)"},
		{"heading: the mode under it below StackBelow", figmaHeading, below, false,
			"  Select an option\n      Mode Live"},
		{"heading: a title reaching the value column puts the mode under it", MenuHeading{Title: strings.Repeat("x", 25), Mode: &ModeIndicator{}}, from, false,
			"  " + strings.Repeat("x", 25) + "\n      Mode Live"},
		{"heading: no mode", MenuHeading{Title: " Select an option\t"}, from, false, "  Select an option"},
		{"heading: no title is the mode alone", MenuHeading{Mode: &ModeIndicator{DryRun: true, Source: ModeSourceFlag}}, from, false,
			strings.Repeat(" ", theme.ValueCol) + "Mode Plan only (-n)"},
		// At a fixed width, so that it also runs with theme.StackBelow
		// refused: the mode then goes under the title only because its
		// word would not fit beside it.
		{"heading: under a cut title the mode's word stays whole", MenuHeading{Title: "Select an option", Mode: &ModeIndicator{DryRun: true}}, 10, false,
			"  Selec...\n Plan only"},
		{"menu: keys right-aligned, descriptions at the value column", Menu{Items: []MenuItem{
			{Key: "1", Label: "User Management", Description: "Create, Configure", Aside: "sudo, SSH keys"},
			{Key: "10", Label: "Logs", Description: "View log file"},
		}}, from, false,
			"   1) User Management      Create, Configure (sudo, SSH keys)\n  10) Logs                 View log file"},
		{"menu: the exit item after the section gap", Menu{Items: []MenuItem{{Key: "1", Label: "DNS"}}, Exit: figmaMenu.Exit}, from, false,
			"   1) DNS" + strings.Repeat("\n", theme.SectionGap+1) + "   0) Exit                 q to quit"},
		{"menu: a risky item's note line", Menu{Items: []MenuItem{{Key: "2", Label: "SSH Login", Description: "Toggle root SSH access", Risk: true}}}, from, false,
			"   2) SSH Login            Toggle root SSH access\n                           [ ] Lockout risk"},
		{"menu: a disabled item's note line, and no risk", Menu{Items: []MenuItem{{Key: "3", Label: "DNS", Description: "Configure Nameservers", Disabled: true, Risk: true}}}, from, true,
			"   3) DNS                  Configure Nameservers\n                           – Unavailable"},
		{"menu: stacked below StackBelow", Menu{Items: []MenuItem{{Key: "2", Label: "SSH Login", Description: "Toggle root SSH access", Risk: true}}}, below, false,
			"   2) SSH Login\n      Toggle root SSH access\n      [ ] Lockout risk"},
		{"menu: a note with no description sits beside the label", Menu{Items: []MenuItem{{Key: "7", Label: "Run All", Risk: true}}}, from, false,
			"   7) Run All              [ ] Lockout risk"},
		{"menu: a long label stacks the item rather than being cut", Menu{Items: []MenuItem{{Key: "1", Label: "Unattended Upgrades Mail Reports", Description: "Daily"}}}, 80, false,
			"   1) Unattended Upgrades Mail Reports\n      Daily"},
		{"menu: a long label stacks the whole menu", Menu{Items: []MenuItem{
			{Key: "1", Label: "Firewall", Description: "Configure UFW rules"},
			{Key: "2", Label: "Manage application profiles", Description: "Allow 2230/tcp"},
		}, Exit: MenuItem{Key: "0", Label: "Return to the firewall menu"}}, 80, false,
			"   1) Firewall\n      Configure UFW rules\n   2) Manage application profiles\n      Allow 2230/tcp" +
				strings.Repeat("\n", theme.SectionGap+1) + "   0) Return to the firewall menu"},
		{"menu: a long label with nothing beside it stacks nothing", Menu{Items: []MenuItem{
			{Key: "1", Label: "DNS", Description: "Configure Nameservers"},
		}, Exit: MenuItem{Key: "0", Label: "Return to the firewall menu"}}, 80, false,
			"   1) DNS                  Configure Nameservers" + strings.Repeat("\n", theme.SectionGap+1) + "   0) Return to the firewall menu"},
		// The next two hold whatever theme.StackBelow is: the menu stacks
		// because its value column is too narrow, not only below StackBelow.
		{"menu: no room beside for a whole note stacks the menu", Menu{Items: []MenuItem{
			{Key: "2", Label: "SSH Login", Description: "Toggle root SSH access", Risk: true},
			{Key: "3", Label: "DNS", Description: "Configure Nameservers", Disabled: true},
		}}, 40, false,
			"   2) SSH Login\n      Toggle root SSH access\n      [ ] Lockout risk\n   3) DNS\n      Configure Nameservers\n      [-] Unavailable"},
		{"menu: no room beside for a letter of a description stacks the menu", Menu{Items: []MenuItem{{Key: "3", Label: "DNS", Description: "Configure Nameservers"}}}, 29, false,
			"   3) DNS\n      Configure Nameservers"},
		{"menu: a key of up to five cells grows into the indent", Menu{Items: []MenuItem{{Key: "100", Label: "Logs"}, {Key: "1234", Label: "DNS"}}}, 80, false,
			" 100) Logs\n1234) DNS"},
		{"menu: a wider key pushes its label", Menu{Items: []MenuItem{{Key: "12345", Label: "DNS"}}}, 80, false,
			"12345) DNS"},
		{"menu: the exit item has no states", Menu{Exit: MenuItem{Key: "0", Label: "Return", Disabled: true, Risk: true}}, 80, false,
			"   0) Return"},
		{"menu: an item with no label is not shown", Menu{Items: []MenuItem{{Key: "1", Description: "x"}, {Key: "2", Label: "DNS"}}}, 80, false,
			"   2) DNS"},
		{"menu: compact, two columns down then across, the exit item apart", compactMenu, from, false,
			"   1) User Management       6) Dry-Run\n   2) SSH Login             7) Run All\n   3) DNS                   8) Environment\n" +
				"   4) Firewall              9) System Details\n   5) Backup               10) Logs" +
				strings.Repeat("\n", theme.SectionGap+1) + "   0) Exit                 q to quit"},
		// 60 columns stacks a one-column menu while StackBelow is 64; two
		// columns, and the exit item beside its label, do not stack.
		{"menu: compact, the exit item stays beside its label wherever the columns fit", compactMenu, 60, false,
			"   1) User Management       6) Dry-Run\n   2) SSH Login             7) Run All\n   3) DNS                   8) Environment\n" +
				"   4) Firewall              9) System Details\n   5) Backup               10) Logs" +
				strings.Repeat("\n", theme.SectionGap+1) + "   0) Exit                 q to quit"},
		{"menu: compact, an odd item ends the first column", Menu{Items: []MenuItem{{Key: "1", Label: "DNS"}, {Key: "2", Label: "Logs"}, {Key: "3", Label: "Backup"}}, Compact: true}, 80, false,
			"   1) DNS                   3) Backup\n   2) Logs"},
		{"menu: compact, a note line keeps one column", Menu{Items: []MenuItem{
			{Key: "1", Label: "Root SSH Login", Description: "Toggle root SSH access", Disabled: true},
			{Key: "2", Label: "View SSH Settings", Description: "Show SSH configuration"},
		}, Compact: true}, 80, false,
			"   1) Root SSH Login       Toggle root SSH access\n                           [-] Unavailable\n   2) View SSH Settings    Show SSH configuration"},
		{"menu: compact, a first-column label reaching the value column keeps one column", Menu{Items: []MenuItem{
			{Key: "1", Label: "Manage application profiles"}, {Key: "2", Label: "DNS"},
		}, Compact: true}, 80, false,
			"   1) Manage application profiles\n   2) DNS"},
		{"menu: compact, a second column too wide keeps one column", Menu{Items: []MenuItem{
			{Key: "1", Label: "DNS"}, {Key: "2", Label: "System Details"},
		}, Compact: true}, 44, false,
			"   1) DNS\n   2) System Details"},
		{"menu: compact, a key wider than the marker column keeps one column", Menu{Items: []MenuItem{
			{Key: "1", Label: "DNS"}, {Key: "100", Label: "Logs"},
		}, Compact: true}, 80, false,
			"   1) DNS\n 100) Logs"},
		{"prompt: glyph, label, generated hint and colon", figmaPrompt, 80, true,
			"    ❯ Enter your choice [0-10 or q]:"},
		{"prompt: with no hint the colon follows the label", PromptLine{Label: "Username"}, 80, false, "    > Username:"},
		{"prompt: too narrow, the label is cut, not the hint", figmaPrompt, 32, false,
			"    > Enter your... [0-10 or q]:"},
		{"prompt: with no letter of the label left, the hint alone", figmaPrompt, 22, false,
			"    > [0-10 or q]:"},
		{"prompt: empty is the glyph", PromptLine{}, 80, false, "    >"},
		// 用户名称 is double-width: beside the ellipsis, its first letter
		// needs 5 cells, which width 17 leaves 4 of.
		{"prompt: a wide letter that cannot fit leaves the label out", PromptLine{Label: "用户名称", Accepts: yesNo}, 17, false,
			"    > [y/N]:"},
		{"prompt: a wide letter that fits is kept", PromptLine{Label: "用户名称", Accepts: yesNo}, 18, false,
			"    > 用... [y/N]:"},
		{"badge: at the indent", Badge{Text: "web-01"}, 80, false, "  [web-01]"},
		{"badge: a long name is cut inside the brackets", Badge{Text: strings.Repeat("x", 70)}, 60, false,
			"  [" + strings.Repeat("x", 53) + "...]"},
		{"subject: the badge one cell after the colon", SubjectLine{Label: "Managing", Subject: " george "}, 80, false,
			"  Managing: [george]"},
		{"subject: too long for one line, the badge goes under the label", SubjectLine{Label: "Managing sudo method for", Subject: "alice-backup-replication-service"}, 60, false,
			"  Managing sudo method for:\n      [alice-backup-replication-service]"},
		{"subject: no label is the badge alone", SubjectLine{Subject: "george"}, 80, true, "  [george]"},
		{"section: title from column 2 and rule, no stub", SectionHeader{Title: "User Configuration"}, 80, false,
			"  User Configuration " + strings.Repeat("-", maxWidth(SectionHeader{}, 80)-21)},
		{"section: no title is the rule from column 2", SectionHeader{}, 60, true, "  " + strings.Repeat("╌", 58)},
		{"section: a long title is cut", SectionHeader{Title: strings.Repeat("x", 70)}, 60, false,
			"  " + strings.Repeat("x", 53) + "... -"},
		{"breadcrumb: items joined by the separator", Breadcrumb{Items: []string{"User Management", " Manage User", ""}}, 80, true,
			"  User Management › Manage User"},
		{"breadcrumb: too long, the middle items go first", Breadcrumb{Items: []string{"User Management", "Manage User", "SSH Keys", "Remove a key"}}, 40, false,
			"  User Management > ... > Remove a key"},
		{"breadcrumb: then the first item", Breadcrumb{Items: []string{"User Management", "Manage User", "SSH Keys", "Remove a key"}}, 30, false,
			"  ... > Remove a key"},
		{"breadcrumb: and last the current item is cut", Breadcrumb{Items: []string{"Firewall", "Firewall application profiles"}}, 20, false,
			"  Firewall applic..."},
		{"keyvalue: side by side from StackBelow", KeyValue{Key: "SSH Port", Value: "2230"}, from, false,
			"   -  SSH Port             2230"},
		{"keyvalue: stacked below StackBelow", KeyValue{Key: "SSH Port", Value: "2230"}, below, false,
			"   -  SSH Port\n      2230"},
		{"keyvalue: a state's marker replaces the bullet", BulletList{Items: []KeyValue{
			{Key: "a", Value: "ok", State: ValueOK}, {Key: "b", Value: "warn", State: ValueWarn},
			{Key: "c", Value: "bad", State: ValueBad}, {Key: "d", State: ValueUnknown},
			{Key: "e", State: ValueNotApplicable}, {Key: "f", Value: "plain", State: ValueState(9)},
		}}, from, false,
			"  [+] a                    ok\n  [ ] b                    warn\n  [ ] c                    bad\n" +
				"  [?] d                    Unknown\n  [-] e                    Not Applicable\n   -  f                    plain"},
		{"keyvalue: a note follows the plain value", KeyValue{Key: "Backup Path", Value: "/var/backups/hardn", State: ValueOK, Note: "Writable"}, from, true,
			"   ⛛  Backup Path          /var/backups/hardn Writable"},
		{"keyvalue: a long key stacks the row", KeyValue{Key: "Unattended Upgrades Mail Reports", Value: "alice"}, 80, false,
			"   -  Unattended Upgrades Mail Reports\n      alice"},
		{"keyvalue: a long value is cut", KeyValue{Key: "Allowed Users", Value: strings.Repeat("x", 60)}, from, false,
			"   -  Allowed Users        " + strings.Repeat("x", maxWidth(KeyValue{}, from)-theme.ValueCol-3) + "..."},
		{"keyvalue: an extra that fits follows the value", KeyValue{Key: "/", Value: "/dev/sda1", Extra: "ext4"}, from, false,
			"   -  /                    /dev/sda1 ext4"},
		{"keyvalue: an extra that doesn't fit is left out, the value whole", KeyValue{Key: "/", Value: "/dev/mapper/pods-vm--103--disk--0", Extra: "ext4"}, 80, false,
			"   -  /                    /dev/mapper/pods-vm--103--disk--0"},
		{"keyvalue: an extra filling the room exactly is kept", KeyValue{Key: "/", Value: "/dev/mapper/pve-vm--103--disk--0", Extra: "ext4"}, 80, false,
			"   -  /                    /dev/mapper/pve-vm--103--disk--0 ext4"},
		{"keyvalue: no extra with a note", KeyValue{Key: "a", Note: "n", Extra: "x"}, from, false,
			"   -  a" + strings.Repeat(" ", theme.ValueCol-7) + "n"},
		{"keyvalue: a state with no value shows nothing", KeyValue{Key: "Sudo", State: ValueBad}, 80, false, "   -  Sudo"},
		{"keyvalue: no key leaves the key column blank", KeyValue{Value: "2230"}, from, false,
			"   -                       2230"},
		{"bulletlist: a long key stacks the whole list", BulletList{Items: []KeyValue{
			{Key: "Backup State", Value: "Enabled", State: ValueOK},
			{Key: "Unattended Upgrades Mail Reports", Value: "alice"},
			{Key: "Non-root user has sudo privileges"},
		}}, 80, false,
			"  [+] Backup State\n      Enabled\n   -  Unattended Upgrades Mail Reports\n      alice\n   -  Non-root user has sudo privileges"},
		// The next three hold whatever theme.StackBelow is.
		{"bulletlist: no room beside for a letter stacks the list", BulletList{Items: []KeyValue{{Key: "SSH Port", Value: "2230"}}}, 29, false,
			"   -  SSH Port\n      2230"},
		{"bulletlist: a bullet item wraps under itself", BulletList{Items: []KeyValue{{Key: "If SSH fails, a fallback access method is available"}}}, 40, false,
			"   -  If SSH fails, a fallback access\n      method is available"},
		{"bulletlist: an empty list is nothing", BulletList{Items: []KeyValue{{}, {Key: " "}}}, 80, false, ""},
		{"message: ok is its callout, the kind's title over the text", Message{Kind: MessageOK, Text: "DNS configured successfully"}, 80, false,
			"# Operation Successful\n# DNS configured successfully"},
		{"message: bad is its callout, with no glyph", Message{Kind: MessageBad, Text: "Failed"}, 80, true, "█ Operation Failed\n█ Failed"},
		{"message: info is prose at the indent, wrapped, with no glyph and no word", Message{Text: "No automatic backups will be created"}, 20, false,
			"  No automatic\n  backups will be\n  created"},
		{"message: a word too wide for its line breaks", Message{Text: "Unattended"}, 8, false, "  Unatte\n  nded"},
		{"messages: a run of one kind is one callout, a run of info one block of prose", Messages{
			{Kind: MessageOK, Text: "Held port 22 removed"}, {Kind: MessageOK, Text: "ufw rule for 22/tcp removed"},
			{Kind: MessageWarn, Text: "The revert timer is still armed"}, {Text: "Run sudo hardn confirm again."}, {Kind: MessageKind(9), Text: "Then close this session."},
		}, 80, false, "# Operation Successful\n# Held port 22 removed\n# ufw rule for 22/tcp removed\n\n# Warning\n# The revert timer is still armed\n\n" +
			"  Run sudo hardn confirm again.\n  Then close this session."},
		{"messages: a kind again after another is a callout again; empty texts are skipped", Messages{
			{Kind: MessageBad, Text: "a"}, {Kind: MessageBad, Text: " "}, {Text: "b"}, {Kind: MessageBad, Text: "c"},
		}, 80, false, "# Operation Failed\n# a\n\n  b\n\n# Operation Failed\n# c"},
		{"messages: nothing to show is nothing", Messages{{Kind: MessageOK}, {Text: "\t"}}, 80, false, ""},
		{"inline: the glyph in the marker column, the text at the label column, no word", InlineResult{Kind: MessageBad, Text: "Invalid IP address format"}, 80, true,
			"   ⨳  Invalid IP address format"},
		{"inline: ASCII (x) at columns 2 to 4", InlineResult{Kind: MessageWarn, Text: "Nameserver 192.0.2.54 is already configured"}, 80, false,
			"  (x) Nameserver 192.0.2.54 is already configured"},
		{"inline: a long text wraps under itself", InlineResult{Text: "An IPv4 or IPv6 address, such as 192.0.2.53"}, 24, false,
			"  (x) An IPv4 or IPv6\n      address, such as\n      192.0.2.53"},
		{"inline: no text is nothing", InlineResult{Kind: MessageOK, Text: " "}, 80, false, ""},
		{"callout: bar, title and body, no glyph", figmaWarning, 80, false, "# Warning\n# Before disabling root SSH login, ensure:"},
		{"callout: no title is the kind's own", Callout{Kind: MessageCancelled, Body: []string{""}}, 80, true, "█ Operation Cancelled"},
		{"callout: a title of its own follows the kind's word", Callout{Kind: MessageBad, Title: " Port 2230 refused "}, 80, true, "█ Error: Port 2230 refused"},
		{"callout: a title that is the kind's word is not repeated", Callout{Kind: MessageOK, Title: "Success"}, 80, false, "# Success"},
		{"callout: a body wraps beside the bar", Callout{Kind: MessageOK, Title: "Success", Body: []string{"aaa bbb ccc ddd"}}, 13, false,
			"# Success\n# aaa bbb ccc\n# ddd"},
		{"callout: a long title is cut", Callout{Kind: MessageOK, Title: "Done"}, 9, false, "# Succ..."},
		{"callout: an indent moves the bar and narrows the lines", Callout{Kind: MessageOK, Title: "Success", Body: []string{"aaa bbb ccc dd"}, Indent: 3}, 16, false,
			"   # Success\n   # aaa bbb ccc\n   # dd"},
		{"callout: an indented title is cut at the width", Callout{Kind: MessageOK, Title: "Done", Indent: 3}, 12, false, "   # Succ..."},
		{"field: the number right-aligned, the answer after the colon", AnsweredField{Number: 1, Label: "Username", Answer: "george"}, 80, false,
			"   1. Username: george"},
		{"field: the hint generated from the inputs", AnsweredField{Number: 2, Label: "Grant sudo privilege?", Accepts: yesNo, Answer: "y"}, 80, false,
			"   2. Grant sudo privilege? [y/N]: y"},
		{"field: unnumbered at the indent", AnsweredField{Label: "Disable SSH login for 'root'?", Accepts: yesNo, Answer: "y"}, 80, false,
			"  Disable SSH login for 'root'? [y/N]: y"},
		{"field: unanswered ends at the colon", AnsweredField{Number: 3, Label: "Require sudo password?", Accepts: yesDefault}, 80, false,
			"   3. Require sudo password? [Y/n]:"},
		{"field: a long answer goes under the question", AnsweredField{Number: 4, Label: "Paste SSH public key", Answer: sshKey}, 80, false,
			"   4. Paste SSH public key:\n      " + sshKey[:maxWidth(AnsweredField{}, 80)-theme.StackCol-3] + "..."},
		{"field: the question is cut to keep a short answer beside it", AnsweredField{Label: "Disable SSH login for 'root'?", Accepts: yesNo, Answer: "y"}, 37, false,
			"  Disable SSH login for '... [y/N]: y"},
		{"field: typographic quotes are shown as ASCII (C12)", AnsweredField{Label: "Disable SSH login for \u2018root\u2019?", Accepts: yesNo, Answer: "y"}, 80, true,
			"  Disable SSH login for 'root'? [y/N]: y"},
		{"message: typographic double quotes are shown as ASCII (C12)", Message{Kind: MessageBad, Text: "sshd: \u201cPort 2230\u201d is in use"}, 80, true,
			"█ Operation Failed\n█ sshd: \"Port 2230\" is in use"},
		{"field: a wide number pushes the question", AnsweredField{Number: 12345, Label: "Username"}, 80, false, "12345. Username:"},
		{"field: too narrow for the colon, an unnumbered question is cut, not left as its indent", AnsweredField{Label: "Username", Answer: "george"}, 6, false,
			"  U...\n..."},
		{"field: with no letter left, an unnumbered question is the ellipsis", AnsweredField{Label: "Username"}, 5, false, "..."},
		{"field: a numbered question keeps its number", AnsweredField{Number: 1, Label: "Username"}, 6, false, "   1."},
		{"field head: a title with no colon, the description under it", FieldHead{Number: 1, Title: "Username", Description: "Specify username to create"}, 80, false,
			"   1. Username\n      Specify username to create"},
		{"field head: the options as a menu's rows", FieldHead{Number: 2, Title: "Select a user", Options: users}, 80, false,
			"   2. Select a user\n\n   1) george\n   2) alice"},
		{"field head: unnumbered at the indent", FieldHead{Title: "Select a user", Description: "Configure sudo, SSH keys"}, 80, false,
			"  Select a user\n      Configure sudo, SSH keys"},
		{"field head: a wide number pushes the title", FieldHead{Number: 12345, Title: "Username"}, 80, false, "12345. Username"},
		{"field head: a long title is cut", FieldHead{Number: 1, Title: "Paste SSH public key"}, 16, false, "   1. Paste S..."},
		{"field head: the description wraps under itself", FieldHead{Number: 1, Title: "Username", Description: "Specify username to create"}, 20, false,
			"   1. Username\n      Specify\n      username to\n      create"},
		{"field head: no title is nothing", FieldHead{Description: "Specify username to create", Options: users}, 80, false, ""},
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

// TestASCIIText checks the typography map (DEV-036): with the ASCII glyph
// set each component shows view-model text's ≥ ≤ ≠ … – — × → and • as
// >= <= != ... - - x -> and *, so that its output holds nothing but
// ASCII; with the Unicode set the text keeps them. The typographic quotes
// are shown as ASCII in both sets (C12). What must be typed or opened as
// shown, the hint generated from a prompt's inputs and an ErrorReport's
// log path, and code, a plan's diff, target and commands (DEV-038), keep
// their characters in both. An answer says what was typed or taken, not
// what must be typed, so it is mapped.
func TestASCIIText(t *testing.T) {
	const (
		typo   = "≥1 ≤2 ≠3 …4 –5 —6 ×7 →8 •9"
		mapped = ">=1 <=2 !=3 ...4 -5 -6 x7 ->8 *9"
	)
	tests := []struct {
		name string
		vm   renderer
	}{
		{"header", Header{Version: typo}},
		{"context bar", ContextBar{Name: typo}},
		{"title bar", TitleBar{Title: typo}},
		{"title bar trail", TitleBar{Title: "x", Parents: []string{typo}}},
		{"host block", HostBlock{Hostname: "web-01", Address: typo, Uptime: typo}},
		{"risk summary", RiskSummary{Band: RiskModerate, Level: typo}},
		{"status row", StatusRow{Label: "Non-root", State: StatusConfigured, Detail: "UID ≥ 1000"}},
		{"status row lines", StatusRow{Label: "User", State: StatusConfigured, Lines: []StatusLine{{Text: typo}, {Text: typo, Warn: true}}}},
		{"menu heading", MenuHeading{Title: typo}},
		{"menu", Menu{Items: []MenuItem{{Key: "1", Label: "Users", Description: typo}}}},
		{"prompt", PromptLine{Label: typo, Accepts: yesNo}},
		{"badge", Badge{Text: typo}},
		{"subject line", SubjectLine{Label: typo, Subject: "george"}},
		{"section header", SectionHeader{Title: typo}},
		{"breadcrumb", Breadcrumb{Items: []string{"Firewall", typo}}},
		{"key-value", BulletList{Items: []KeyValue{{Key: typo}, {Key: "SSH", Value: typo}}}},
		{"message", Message{Kind: MessageInfo, Text: typo}},
		{"messages", Messages{{Kind: MessageWarn, Text: typo}, {Text: typo}}},
		{"inline result", InlineResult{Kind: MessageBad, Text: typo}},
		{"callout", Callout{Kind: MessageWarn, Title: typo, Body: []string{typo}}},
		{"answered field", AnsweredField{Number: 1, Label: typo, Answer: typo}},
		{"an answer says what was typed", AnsweredField{Number: 1, Label: "Comment", Answer: typo}},
		{"field head", FieldHead{Number: 1, Title: typo, Description: typo}},
		{"plan title", PlanView{Changes: []PlanChange{{Title: typo}}}},
		{"steps", StepProgress{Steps: []Step{{Title: typo, State: StepFailed, Detail: "–"}}}},
		{"report", ErrorReport{RolledBack: []string{typo}}},
	}
	for _, tt := range tests {
		ascii := ansi.Strip(tt.vm.Render(NewCtx(term.Fixed(80, colorprofile.ASCII, false, true))))
		if strings.ContainsFunc(ascii, func(r rune) bool { return r > '~' }) {
			t.Errorf("%s, ASCII: a character outside ASCII in\n%s", tt.name, ascii)
		}
		unicode := ansi.Strip(tt.vm.Render(NewCtx(term.Fixed(80, colorprofile.ASCII, true, true))))
		if tt.name == "status row" {
			if !strings.Contains(ascii, "UID >= 1000") || !strings.Contains(unicode, "UID ≥ 1000") {
				t.Errorf("status row: ASCII\n%s\nUnicode\n%s\nwant UID >= 1000 and UID ≥ 1000", ascii, unicode)
			}
			continue
		}
		if !strings.Contains(ascii, mapped) || strings.Contains(ascii, typo) {
			t.Errorf("%s, ASCII: want %q in\n%s", tt.name, mapped, ascii)
		}
		if !strings.Contains(unicode, typo) {
			t.Errorf("%s, Unicode: want %q in\n%s", tt.name, typo, unicode)
		}
	}
	quoted := Message{Kind: MessageInfo, Text: "‘root’ “Port”"}
	for _, unicode := range []bool{false, true} {
		if got := ansi.Strip(quoted.Render(NewCtx(term.Fixed(80, colorprofile.ASCII, unicode, true)))); !strings.Contains(got, `'root' "Port"`) {
			t.Errorf("unicode %v: quotes in %q, want ASCII ones", unicode, got)
		}
	}
	kept := []struct {
		name string
		vm   renderer
		want string
	}{
		{"a hint names what is typed", PromptLine{Label: "Next", Accepts: keys.Set{{Keys: []string{"→"}}}}, "[→]:"},
		{"a log path names a file to open", ErrorReport{LogPath: "/var/log/hardn—a→b…c.log"}, "at /var/log/hardn—a→b…c.log for"},
		{"code keeps its characters", PlanView{Changes: []PlanChange{{
			Title: "Set", Target: "/etc/a…b", Before: "x ≤ 1\n", After: "x ≥ 1\n", Commands: []string{"echo a — b"},
		}}}, "/etc/a…b\n      -x ≤ 1\n      +x ≥ 1\n      $ echo a — b"},
	}
	for _, tt := range kept {
		for _, unicode := range []bool{false, true} {
			if got := ansi.Strip(tt.vm.Render(NewCtx(term.Fixed(80, colorprofile.ASCII, unicode, true)))); !strings.Contains(got, tt.want) {
				t.Errorf("%s, unicode %v: want %q in\n%s", tt.name, unicode, tt.want, got)
			}
		}
	}
}

// TestModeWord checks that the ModeIndicator shows its whole word, the
// one thing that tells the modes apart, at every width the word fits, in
// both glyph sets; that on a terminal too narrow for the whole text the
// label gives way first, then the source, each whole or not at all
// (decision 3); and that live names no source, whatever it is given
// (decision 2).
func TestModeWord(t *testing.T) {
	sources := map[ModeSource]string{ModeSourceNone: "", ModeSourceFlag: " (" + ModeFromFlag + ")", ModeSourceFile: " (" + ModeFromFile + ")"}
	for _, live := range []bool{true, false} {
		for source, from := range sources {
			vm, word := ModeIndicator{DryRun: !live, Source: source}, ModePlanOnly
			if live {
				word, from = ModeLive, ""
			}
			for w := 1; w <= 130; w++ {
				col := theme.ValueCol
				if w < theme.StackBelow {
					col = theme.StackCol
				}
				room := min(w, theme.ContentWidth) - col
				want := word
				for _, text := range []string{ModeLabel + " " + word + from, word + from} {
					if len(text) <= room {
						want = text
						break
					}
				}
				for _, unicode := range []bool{true, false} {
					got := ansi.Strip(vm.Render(NewCtx(term.Fixed(w, colorprofile.ASCII, unicode, true))))
					if w >= len(word) && strings.TrimSpace(got) != want {
						t.Errorf("%+v, width %d, unicode %v: got %q, want %q", vm, w, unicode, got, want)
					}
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
		if got := Truncate(c, tt.s, tt.limit); got != tt.want {
			t.Errorf("truncate(%q, %d), unicode %v = %q, want %q", tt.s, tt.limit, tt.unicode, got, tt.want)
		}
	}
}

// TestShowsWhole checks that ShowsWhole agrees with Render at every width
// from 1 to 130, in both glyph sets: it reports true exactly when the
// question asked renders whole, number, question and hint uncut.
func TestShowsWhole(t *testing.T) {
	navigation := keys.Navigation()
	tests := []struct {
		vm    AnsweredField
		whole string // the question asked, rendered with room to spare
	}{
		{AnsweredField{Number: 2, Label: "Grant sudo privilege?", Accepts: append(slices.Clone(yesNo), navigation...)},
			"   2. Grant sudo privilege? [y/N, 0 or q]:"},
		{AnsweredField{Label: "Username"}, "  Username:"},
		{AnsweredField{Number: 1, Label: "Username", Accepts: navigation}, "   1. Username [0 or q]:"},
		// origin/main's label (pkg/menu user_options.go:580) beside the
		// navigation fills the content, 64 columns, exactly: whole from 64
		// up. Two cells more are wider than the content: never whole.
		{AnsweredField{Number: 1, Label: "Enter new username (leave empty to keep current)", Accepts: navigation},
			"   1. Enter new username (leave empty to keep current) [0 or q]:"},
		{AnsweredField{Number: 1, Label: "Enter a new username (leave empty to keep current)", Accepts: navigation}, ""},
		{AnsweredField{Accepts: navigation}, ""},
	}
	for _, tt := range tests {
		for w := 1; w <= 130; w++ {
			for _, unicode := range []bool{true, false} {
				c := NewCtx(term.Fixed(w, colorprofile.ASCII, unicode, true))
				want := tt.whole != "" && ansi.Strip(tt.vm.Render(c)) == tt.whole
				if got := tt.vm.ShowsWhole(c); got != want {
					t.Errorf("%q at width %d, unicode %v: ShowsWhole %v, want %v", tt.vm.Label, w, unicode, got, want)
				}
			}
		}
	}
}

// TestExtraOffTerminal: off a terminal nothing is cut (Whole), so an extra that
// would not fit beside the label is kept.
func TestExtraOffTerminal(t *testing.T) {
	vm := KeyValue{Key: "/", Value: "/dev/mapper/pods-vm--103--disk--0", Extra: "ext4"}
	if got := ansi.Strip(vm.Render(OffTerminal())); !strings.HasSuffix(got, "/dev/mapper/pods-vm--103--disk--0 ext4") {
		t.Errorf("off a terminal: %q", got)
	}
}

// TestStatusStates checks that each state shows its glyph and its own
// word, both in the state's style, and that no two states share both, so
// that no state rests on colour alone: two states that share a glyph,
// Partial and Not Configured (the maintainer, 2026-10-21), differ by word
// and by colour. A value outside the states is Unknown.
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
	states := tests[:5]
	for i, a := range states {
		for _, b := range states[i+1:] {
			if a.glyph == b.glyph && (a.word == b.word || a.style.GetForeground() == b.style.GetForeground()) {
				t.Errorf("%s and %s share the glyph %q, and their word or colour too", a.word, b.word, a.glyph)
			}
		}
	}
}

// TestLineStyles checks that a line under a status row takes its colour,
// bullet and text alike: Status.LineWarn for a cost hardn established,
// Status.Line otherwise; and that the host block's reboot line takes
// Host.Reboot when a reboot is pending and Host.Unknown when that is not
// established, each after the bullet, never a state's glyph (the
// maintainer, 2026-10-21).
func TestLineStyles(t *testing.T) {
	for _, unicode := range []bool{true, false} {
		c := NewCtx(term.Fixed(80, colorprofile.TrueColor, unicode, true))
		st, h, b := c.Theme.Status, c.Theme.Host, c.Theme.Glyphs.Bullet+" "
		tests := []struct {
			name string
			vm   renderer
			want string
		}{
			{"warn line", StatusRow{Label: "User", Lines: []StatusLine{{Text: "Second root account", Warn: true}}}, st.LineWarn.Render(b + "Second root account")},
			{"muted line", StatusRow{Label: "User", Lines: []StatusLine{{Text: "Counted as not configured"}}}, st.Line.Render(b + "Counted as not configured")},
			{"reboot pending", HostBlock{Reboot: RebootPending}, h.Reboot.Render(b + "Reboot pending")},
			{"reboot not established", HostBlock{Reboot: RebootUnknown}, h.Unknown.Render(b + "Can't tell if reboot pending")},
		}
		for _, tt := range tests {
			if got := tt.vm.Render(c); !strings.Contains(got, "\n"+strings.Repeat(" ", theme.ValueCol)+tt.want) {
				t.Errorf("%s, unicode %t: got %q, want a line at the value column of %q", tt.name, unicode, got, tt.want)
			}
		}
		if st.Line.GetForeground() == st.LineWarn.GetForeground() || h.Reboot.GetForeground() == h.Unknown.GetForeground() {
			t.Errorf("unicode %t: the two colours of a line are one", unicode)
		}
	}
}

// TestRiskGlyphDrawn checks that the risk icon, ⛬ or [*], is the risk
// level's alone: rendering every state of fixture.States, in both glyph
// sets and at widths that stack and that do not, it is drawn only by
// RiskSummary, before "Risk Level", each state's component taken from its
// fixture name. The menu's and the plan's "Lockout risk" lines are
// warnings (the maintainer, 2026-10-21; DEV-028, DEV-038): every line that
// says it draws Glyphs.Warn before it, and Menu and PlanView each draw one.
// No other glyph is the risk icon (pkg/ui/theme's TestRiskGlyphAlone).
func TestRiskGlyphDrawn(t *testing.T) {
	const owner, words = "risksummary", "Risk Level"
	warners := []string{"menu", "planview"} // the components with a "Lockout risk" line
	for _, unicode := range []bool{true, false} {
		for _, w := range []int{40, 64, 80} {
			c := NewCtx(term.Fixed(w, colorprofile.TrueColor, unicode, true))
			icon, warn := c.Theme.Glyphs.Risk, c.Theme.Glyphs.Warn
			seen, warned := false, map[string]bool{}
			for _, s := range fixture.States() {
				comp, _, _ := strings.Cut(s.Name, "/")
				out := ansi.Strip(s.VM.Render(c))
				for _, l := range strings.Split(out, "\n") {
					before, _, ok := strings.Cut(l, LockoutRisk)
					if !ok {
						continue
					}
					if !strings.HasSuffix(strings.TrimRight(before, " "), warn) {
						t.Errorf("%s, unicode %t, width %d: %q not after the warning glyph %q: %q", s.Name, unicode, w, LockoutRisk, warn, l)
						continue
					}
					warned[comp] = true
				}
				for rest := out; ; {
					i := strings.Index(rest, icon)
					if i < 0 {
						break
					}
					rest = rest[i+len(icon):]
					after := strings.TrimLeft(rest, " ")
					if comp == owner && strings.HasPrefix(after, words) {
						seen = true
						continue
					}
					t.Errorf("%s, unicode %t, width %d: the risk icon %q before %q", s.Name, unicode, w, icon, strings.SplitN(after, "\n", 2)[0])
				}
			}
			if !seen {
				t.Errorf("unicode %t, width %d: no %s state drew the risk icon before %q", unicode, w, owner, words)
			}
			for _, comp := range warners {
				if !warned[comp] {
					t.Errorf("unicode %t, width %d: no %s state drew %q after the warning glyph %q", unicode, w, comp, LockoutRisk, warn)
				}
			}
		}
	}
}

// TestMenuStyles checks that each part of the menu group takes its slot:
// a disabled item is faint in every part and its note line, and a risky
// item's note line is in Menu.Risk, so each state has a glyph and a word
// as well as its colour.
func TestMenuStyles(t *testing.T) {
	c := NewCtx(term.Fixed(80, colorprofile.TrueColor, true, true))
	m, g, p := c.Theme.Menu, c.Theme.Glyphs, c.Theme.Prompt
	type part struct {
		text  string
		style lipgloss.Style
	}
	tests := []struct {
		name  string
		vm    renderer
		parts []part
	}{
		{"heading", figmaHeading, []part{{"Select an option", m.Heading}}},
		{"item", figmaMenu, []part{
			{"1)", m.Key}, {"User Management", m.Label},
			{"Create, Configure", m.Description}, {"(sudo, SSH keys)", m.Aside},
		}},
		{"exit", figmaMenu, []part{{"0)", m.ExitKey}, {"Exit", m.Exit}, {"q to quit", m.Exit}}},
		{"disabled", Menu{Items: []MenuItem{{Key: "1", Label: "Root SSH Login", Description: "Toggle root SSH access", Aside: "key-only", Disabled: true}}}, []part{
			{"1)", m.Disabled}, {"Root SSH Login", m.Disabled}, {"Toggle root SSH access", m.Disabled},
			{"(key-only)", m.Disabled}, {g.NotApplicable + " " + Unavailable, m.Disabled},
		}},
		{"risk", Menu{Items: []MenuItem{{Key: "1", Label: "Run All", Risk: true}}}, []part{{g.Warn + " " + LockoutRisk, m.Risk}}},
		{"prompt", figmaPrompt, []part{{g.Prompt, p.Glyph}, {"Enter your choice", p.Label}, {"[0-10 or q]:", p.Hint}}},
		{"prompt with no hint", PromptLine{Label: "Username"}, []part{{"Username", p.Label}, {":", p.Label}}},
	}
	for _, tt := range tests {
		got := tt.vm.Render(c)
		for _, want := range tt.parts {
			if !strings.Contains(got, want.style.Render(want.text)) {
				t.Errorf("%s: got %q, want it to hold %q in its style", tt.name, got, want.text)
			}
		}
	}
}

// TestMenuBindings checks the inputs a menu accepts, through the hint a
// PromptLine generates from them with q added: every shown item that can
// be chosen, and the exit item.
func TestMenuBindings(t *testing.T) {
	tests := []struct {
		name string
		m    Menu
		want string
	}{
		{"Figma's main menu", figmaMenu, "0-10 or q"},
		{"Figma's sub-menu", sshMenu, "0-2 or q"},
		{"a disabled item is not accepted", withItem(figmaMenu, 2, MenuItem{Key: "3", Label: "DNS", Disabled: true}), "0-2, 4-10 or q"},
		{"nor one with no key or no label", Menu{Items: []MenuItem{{Label: "DNS"}, {Key: "2"}, {Key: " 3 ", Label: "Logs"}}}, "3 or q"},
		{"the exit item is, whatever its Disabled", Menu{Exit: MenuItem{Key: "0", Label: "Return", Disabled: true}}, "0 or q"},
		{"an empty menu accepts nothing", Menu{}, "q"},
	}
	for _, tt := range tests {
		b := tt.m.Bindings()
		if got := append(b, quit).Hint(); got != tt.want {
			t.Errorf("%s: hint %q, want %q", tt.name, got, tt.want)
		}
		if h := b.Help(); h != "" {
			t.Errorf("%s: the item bindings have help %q, want none", tt.name, h)
		}
	}
}

// TestMenuItemShown checks that an item is shown exactly when it renders.
func TestMenuItemShown(t *testing.T) {
	c := NewCtx(term.Fixed(80, colorprofile.NoTTY, false, true))
	for _, label := range []string{"DNS", " DNS ", "", "  ", "\x1b[0m", "\u200b", "\t\n", "\x1b[1mDNS\x1b[0m"} {
		it := MenuItem{Key: "1", Label: label}
		rendered := Menu{Items: []MenuItem{it}}.Render(c) != ""
		if it.Shown() != rendered {
			t.Errorf("label %q: Shown() = %v, but the menu renders it: %v", label, it.Shown(), rendered)
		}
	}
}

// TestBars checks the bars (the maintainer, 2026-10-21; DEV-018) at every
// colour profile, on both backgrounds, in both glyph sets and at two
// widths: a ContextBar and a TitleBar open with a one-cell stub of
// Glyphs.Shade in their Fill slot, then a space and the text from column
// 2, a space and Glyphs.Shade in Fill to the bar width. Fill has
// bar.shade's colour wherever there is colour and none without
// (pkg/ui/theme's TestColourRoles), and no part of the line is painted
// behind: no SGR in it sets a background. A SectionHeader, which had the
// stub until 2026-11-06 (DEV-033, DEV-073), opens with two plain spaces
// in its place, the title from column 2, then a space and its rule.
func TestBars(t *testing.T) {
	profiles := []colorprofile.Profile{colorprofile.TrueColor, colorprofile.ANSI256, colorprofile.ANSI, colorprofile.ASCII, colorprofile.NoTTY}
	for _, p := range profiles {
		for _, dark := range []bool{true, false} {
			for _, unicode := range []bool{true, false} {
				for _, w := range []int{40, 80} {
					env := term.Fixed(w, p, unicode, dark)
					c := NewCtx(env)
					th, g := c.Theme, c.Theme.Glyphs
					tests := []struct {
						name       string
						vm         renderer
						fill       lipgloss.Style
						text, tail string // tail is the glyph after the text: the fill's or the rule's
					}{
						{"context bar", ContextBar{Name: "Debian", Version: "12", Codename: "Bookworm"}, th.ContextBar.Fill, "Debian 12 (Bookworm)", g.Shade},
						{"title bar", TitleBar{Title: "Manage User"}, th.TitleBar.Fill, "Manage User", g.Shade},
					}
					for _, tt := range tests {
						where := fmt.Sprintf("%s, %v, dark %t, unicode %t, width %d", tt.name, p, dark, env.Unicode, w)
						out := tt.vm.Render(c)
						n := maxWidth(tt.vm, w) - 3 - lipgloss.Width(tt.text)
						if got, want := ansi.Strip(out), g.Shade+" "+tt.text+" "+strings.Repeat(tt.tail, n); got != want {
							t.Errorf("%s: got\n%s\nwant\n%s", where, got, want)
						}
						if !strings.HasPrefix(out, tt.fill.Render(g.Shade)+" ") {
							t.Errorf("%s: %q does not open with the stub in Fill and a space", where, out)
						}
						if tt.tail == g.Shade && !strings.HasSuffix(out, " "+tt.fill.Render(strings.Repeat(g.Shade, n))) {
							t.Errorf("%s: %q does not end with a space and the fill in Fill", where, out)
						}
						if got := tt.fill.GetForeground() != (lipgloss.NoColor{}); got != env.HasColor() {
							t.Errorf("%s: Fill is coloured %t, want %t", where, got, env.HasColor())
						}
						if paintsBackground(out) {
							t.Errorf("%s: %q paints a background", where, out)
						}
					}
					sh := SectionHeader{Title: "User Configuration"}
					out := sh.Render(c)
					n := maxWidth(sh, w) - theme.Indent - 1 - lipgloss.Width(sh.Title)
					if got, want := ansi.Strip(out), "  "+sh.Title+" "+strings.Repeat(g.Rule, n); got != want || !strings.HasPrefix(out, "  ") || paintsBackground(out) {
						t.Errorf("section header, %v, dark %t, unicode %t, width %d: %q; want %q, opening with two plain spaces, no background", p, dark, env.Unicode, w, out, want)
					}
				}
			}
		}
	}
}

// paintsBackground reports whether any SGR sequence in s sets a
// background colour: 40-47, 100-107, or 48 with an indexed or RGB colour.
// The colour arguments of 38 and 58, the foreground and underline, are
// skipped, so 38;5;41 is not read as background 41.
func paintsBackground(s string) bool {
	for _, m := range sgr.FindAllStringSubmatch(s, -1) {
		ps := strings.Split(m[1], ";")
		for i := 0; i < len(ps); i++ {
			switch p := ps[i]; {
			case p == "38" || p == "58":
				if i+1 < len(ps) && ps[i+1] == "5" {
					i += 2
				} else {
					i += 4
				}
			case p == "48",
				len(p) == 2 && p >= "40" && p <= "47",
				len(p) == 3 && p >= "100" && p <= "107":
				return true
			}
		}
	}
	return false
}

// sgr matches an SGR escape sequence, its parameters in the group.
var sgr = regexp.MustCompile(`\x1b\[([0-9;]*)m`)

// TestBlockStyles checks that each part of the block group takes its slot.
func TestBlockStyles(t *testing.T) {
	c := NewCtx(term.Fixed(80, colorprofile.TrueColor, true, true))
	th := c.Theme
	type part struct {
		text  string
		style lipgloss.Style
	}
	tests := []struct {
		name  string
		vm    renderer
		parts []part
	}{
		{"badge", Badge{Text: "george"}, []part{{" george ", th.Badge}}},
		{"subject", SubjectLine{Label: "Managing", Subject: "george"}, []part{{"Managing:", th.Text.Muted}, {" george ", th.Badge}}},
		{"section", SectionHeader{Title: "User Configuration"}, []part{
			{"User Configuration", th.Section.Title},
			{strings.Repeat(th.Glyphs.Rule, maxWidth(SectionHeader{}, 80)-21), th.Section.Rule},
		}},
		{"breadcrumb", Breadcrumb{Items: []string{"User Management", "Manage User"}}, []part{
			{"User Management", th.Breadcrumb.Item}, {th.Glyphs.Separator, th.Breadcrumb.Separator}, {"Manage User", th.Breadcrumb.Current},
		}},
		{"title bar trail", TitleBar{Title: "Manage User", Parents: []string{"User Management"}}, []part{
			{th.Glyphs.Shade, th.TitleBar.Fill}, {"User Management", th.TitleBar.Parent},
			{th.Glyphs.Separator, th.TitleBar.Separator}, {"Manage User", th.TitleBar.Title},
		}},
		{"key-value", figmaUser, []part{
			{th.Glyphs.Bullet, th.KeyValue.Bullet}, {"Sudo", th.KeyValue.Key},
			{"Enabled,", th.KeyValue.Value}, {"No password", th.KeyValue.ValueBad}, {"7 keys", th.KeyValue.Value},
		}},
		{"bullet item", figmaChecklist, []part{{"Non-root user has sudo privileges", th.KeyValue.Key}}},
		{"warning bullet item", KeyValue{Key: "dhcpcd may rewrite /etc/resolv.conf on a lease", State: ValueWarn}, []part{
			{th.Glyphs.Bullet, th.KeyValue.ValueWarn}, {"dhcpcd may rewrite /etc/resolv.conf on a lease", th.KeyValue.ValueWarn},
		}},
		{"muted bullet item", KeyValue{Key: "No nameservers configured", State: ValueNone}, []part{
			{th.Glyphs.Bullet, th.KeyValue.Bullet}, {"No nameservers configured", th.KeyValue.Value},
		}},
		{"a none value draws as a plain one", KeyValue{Key: "SSH Keys", Value: "None", State: ValueNone}, []part{
			{th.Glyphs.Bullet, th.KeyValue.Bullet}, {"SSH Keys", th.KeyValue.Key}, {"None", th.KeyValue.Value},
		}},
		{"message", Message{Kind: MessageWarn, Text: "No nameservers configured"}, []part{
			{" ", th.Callout.Warn.Bar}, {"Warning", th.Callout.Warn.Title}, {"No nameservers configured", th.Callout.Body},
		}},
		{"info message", Message{Text: "No automatic backups will be created"}, []part{
			{"No automatic backups will be created", th.Text.Muted},
		}},
		{"inline result", InlineResult{Kind: MessageBad, Text: "Invalid IP address format"}, []part{
			{th.Glyphs.Inline, th.Inline.Bad}, {"Invalid IP address format", th.Inline.Bad},
		}},
		{"callout", figmaWarning, []part{
			{" ", th.Callout.Warn.Bar}, {"Warning", th.Callout.Warn.Title}, {"Before disabling root SSH login, ensure:", th.Callout.Body},
		}},
		{"field", AnsweredField{Number: 2, Label: "Grant sudo privilege?", Accepts: yesNo, Answer: "y"}, []part{
			{"2.", th.Field.Number}, {"Grant sudo privilege?", th.Field.Label}, {"[y/N]:", th.Field.Hint}, {"y", th.Field.Answer},
		}},
		{"field with no hint", AnsweredField{Label: "Username", Answer: "george"}, []part{{":", th.Field.Label}}},
		{"field head", FieldHead{Number: 2, Title: "Select a user", Description: "Configure sudo, SSH keys", Options: users}, []part{
			{"2.", th.Field.Number}, {"Select a user", th.Field.Label}, {"Configure sudo, SSH keys", th.Field.Description},
			{"1)", th.Menu.Key}, {"george", th.Menu.Label},
		}},
	}
	for _, tt := range tests {
		got := tt.vm.Render(c)
		for _, want := range tt.parts {
			if !strings.Contains(got, want.style.Render(want.text)) {
				t.Errorf("%s: got %q, want it to hold %q in its style", tt.name, got, want.text)
			}
		}
	}
}

// TestValueStates checks that each value state shows in the value's
// colour and as its status marker in place of the bullet, in the same
// colour, and that the marker is there without colour too. A warn value
// takes a bad one's marker, as Partial takes Not Configured's, and differs
// from it by colour; without colour the value's words tell them apart. A
// plain value keeps the bullet, and a none value draws as a plain one.
func TestValueStates(t *testing.T) {
	colour := NewCtx(term.Fixed(80, colorprofile.TrueColor, true, true))
	none := NewCtx(term.Fixed(80, colorprofile.ASCII, true, true))
	kv, g := colour.Theme.KeyValue, colour.Theme.Glyphs
	tests := []struct {
		state       ValueState
		style, mark lipgloss.Style
		glyph       string
	}{
		{ValuePlain, kv.Value, kv.Bullet, g.Bullet},
		{ValueOK, kv.ValueOK, kv.ValueOK, g.Configured},
		{ValueWarn, kv.ValueWarn, kv.ValueWarn, g.Partial},
		{ValueBad, kv.ValueBad, kv.ValueBad, g.NotConfigured},
		{ValueUnknown, kv.ValueUnknown, kv.ValueUnknown, g.Unknown},
		{ValueNotApplicable, kv.ValueNotApplicable, kv.ValueNotApplicable, g.NotApplicable},
		{ValueNone, kv.Value, kv.Bullet, g.Bullet},
		{ValueState(-1), kv.Value, kv.Bullet, g.Bullet},
	}
	shown := map[string]bool{}
	for _, tt := range tests {
		vm := KeyValue{Key: "SSH Port", Value: "2230", State: tt.state}
		got := vm.Render(colour)
		if !strings.Contains(got, tt.style.Render("2230")) || !strings.Contains(got, tt.mark.Render(tt.glyph)) {
			t.Errorf("state %d with colour: got %q, want the marker %q and the value in their styles", tt.state, got, tt.glyph)
		}
		want := Marker(tt.glyph, lipgloss.NewStyle()).String() + " "
		for _, c := range []Ctx{colour, none} {
			if plain := ansi.Strip(vm.Render(c)); !strings.HasPrefix(plain, want) {
				t.Errorf("state %d, %v: got %q, want it to start with %q", tt.state, c.Env.Profile, plain, want)
			}
		}
		shown[tt.glyph] = true
	}
	if len(shown) != 5 {
		t.Errorf("the seven states show %d different markers, want 5, warn's being bad's and none's plain's: %v", len(shown), shown)
	}
	if kv.ValueWarn.GetForeground() == kv.ValueBad.GetForeground() {
		t.Errorf("a warn and a bad value share the marker %q and the colour %v", g.NotConfigured, kv.ValueBad.GetForeground())
	}
}

// TestMessageKinds checks each kind (P-42 R5, the maintainer,
// 2026-10-22): a Message of kind info is muted prose with no glyph and no
// word; of any other kind it is that kind's Callout, its bar, title style
// and own title over the text, and no glyph: at every profile and in both
// glyph sets (uitest.Full). Each kind has its own title and word, and
// without colour, at the ASCII profile and in plain mode, the five kinds'
// messages, and their callouts given one title of their own, still
// differ. A value outside the kinds is MessageInfo.
func TestMessageKinds(t *testing.T) {
	c := NewCtx(term.Fixed(80, colorprofile.TrueColor, true, true))
	co := c.Theme.Callout
	tests := []struct {
		kind        MessageKind
		bar         theme.CalloutKind
		word, title string
	}{
		{MessageInfo, co.Info, "Info", "Note"},
		{MessageOK, co.OK, "Success", "Operation Successful"},
		{MessageWarn, co.Warn, "Warning", "Warning"},
		{MessageBad, co.Bad, "Error", "Operation Failed"},
		{MessageCancelled, co.Cancelled, "Cancelled", "Operation Cancelled"},
		{MessageKind(9), co.Info, "Info", "Note"},
	}
	for _, cell := range uitest.Full() {
		cc := NewCtx(cell.Env)
		for _, tt := range tests {
			got, want := Message{Kind: tt.kind, Text: "x"}.Render(cc), Callout{Kind: tt.kind, Body: []string{"x"}}.Render(cc)
			if tt.word == "Info" {
				want = "  " + cc.Theme.Text.Muted.Render("x")
			}
			if got != want {
				t.Errorf("%s, kind %d: message %q, want %q", cell.Name, tt.kind, got, want)
			}
		}
	}
	words, titles := map[string]bool{}, map[string]bool{}
	for _, tt := range tests {
		got := Callout{Kind: tt.kind}.Render(c)
		if want := tt.bar.Bar.Render(" ") + " " + tt.bar.Title.Render(tt.title); got != want {
			t.Errorf("kind %d: callout %q, want %q", tt.kind, got, want)
		}
		if tt.kind.String() != tt.word {
			t.Errorf("kind %d: String() = %q, want %q", tt.kind, tt.kind.String(), tt.word)
		}
		words[tt.word], titles[tt.title] = true, true
	}
	if len(words) != 5 || len(titles) != 5 {
		t.Errorf("the five kinds have %d different words and %d different titles, want 5: %v %v", len(words), len(titles), words, titles)
	}
	for _, env := range []term.Env{
		term.Fixed(80, colorprofile.ASCII, false, true),
		term.Fixed(80, colorprofile.ASCII, true, true),
		term.Fixed(80, colorprofile.NoTTY, false, true),
	} {
		messages, callouts := map[string]bool{}, map[string]bool{}
		for k := MessageInfo; k <= MessageCancelled; k++ {
			messages[Message{Kind: k, Text: "Port 2230 changed"}.Render(NewCtx(env))] = true
			callout := Callout{Kind: k, Title: "Changing the SSH port", Body: []string{"Keep this session open."}}.Render(NewCtx(env))
			if !strings.Contains(ansi.Strip(callout), k.String()+": Changing the SSH port") {
				t.Errorf("%v, unicode %v: callout %q, want the kind's word before its title", env.Profile, env.Unicode, callout)
			}
			callouts[callout] = true
		}
		if len(messages) != 5 || len(callouts) != 5 {
			t.Errorf("%v, unicode %v: the five kinds show %d different messages and %d different callouts, want 5: %v %v",
				env.Profile, env.Unicode, len(messages), len(callouts), messages, callouts)
		}
	}
}

// TestInlineKinds checks an InlineResult of each kind (P-42, the
// maintainer, 2026-10-22; DEV-062): Glyphs.Inline in the marker column
// and the text at the label column, both in the kind's colour, regular,
// with no word, at every profile and in both glyph sets: failed in
// status.bad, a warning (and a cancelled result) in status.warn, done in
// status.ok, information in text.muted. Without colour the kinds draw
// alike but for their words, by the maintainer's decision ("no word"); a
// value outside the kinds is MessageInfo.
func TestInlineKinds(t *testing.T) {
	for _, cell := range uitest.Full() {
		env := cell.Env
		c := NewCtx(env)
		in := c.Theme.Inline
		for _, tt := range []struct {
			kind  MessageKind
			style lipgloss.Style
		}{
			{MessageBad, in.Bad}, {MessageWarn, in.Warn}, {MessageOK, in.OK}, {MessageInfo, in.Info},
			{MessageCancelled, in.Warn}, {MessageKind(9), in.Info},
		} {
			got := InlineResult{Kind: tt.kind, Text: "Invalid IP address format"}.Render(c)
			lead := Marker(c.Theme.Glyphs.Inline, tt.style).String()
			want := lead + strings.Repeat(" ", theme.LabelCol-lipgloss.Width(lead)) + tt.style.Render("Invalid IP address format")
			if got != want {
				t.Errorf("%s, kind %d: %q, want %q", env.Profile, tt.kind, got, want)
			}
			if tt.style.GetBold() {
				t.Errorf("%s, kind %d: the result is bold", env.Profile, tt.kind)
			}
			for _, word := range []string{"Error", "Warning", "Success", "Info", "Failed", "Done"} {
				if strings.Contains(ansi.Strip(got), word) {
					t.Errorf("%s, kind %d: %q holds the word %q", env.Profile, tt.kind, got, word)
				}
			}
		}
	}
}
