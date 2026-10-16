package component

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"

	"github.com/abbott/hardn/pkg/ui/keys"
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
	// quit is the navigation grammar's q, whose help gives the Exit row its
	// words (brief 1C); next/p1c-nav will own the real binding.
	quit = keys.Binding{Keys: []string{"q"}, Help: "quit"}
	// figmaMenu is Figma's main menu, its Exit row in the brief's words.
	figmaMenu = Menu{Items: []MenuItem{
		{Key: "1", Label: "User Management", Description: "Create, Configure", Aside: "sudo, SSH keys"},
		{Key: "2", Label: "SSH Login", Description: "Toggle root SSH access"},
		{Key: "3", Label: "DNS", Description: "Configure Nameservers"},
		{Key: "4", Label: "Firewall", Description: "Configure UFW rules"},
		{Key: "5", Label: "Backup", Description: "Configure Hardn backup settings"},
		{Key: "6", Label: "Dry-Run", Description: "Simulate changes"},
		{Key: "7", Label: "Run All", Description: "Execute hardening operations"},
		{Key: "8", Label: "Environment", Description: "Configure environment variable"},
		{Key: "9", Label: "System Details", Description: "View system information"},
		{Key: "10", Label: "Logs", Description: "View log file"},
	}, Exit: MenuItem{Key: "0", Label: "Exit", Description: keys.Set{quit}.Help()}}
	// sshMenu is Figma's SSH Login sub-menu (02-SSH_LOGIN).
	sshMenu = Menu{Items: []MenuItem{
		{Key: "1", Label: "Root SSH Login", Description: "Toggle root SSH access"},
		{Key: "2", Label: "View SSH Settings", Description: "Show SSH configuration"},
	}, Exit: MenuItem{Key: "0", Label: "Return"}}
	figmaHeading = MenuHeading{Title: "Select an option", Mode: &ModeIndicator{}}
	figmaPrompt  = PromptLine{Label: "Enter your choice", Accepts: append(figmaMenu.Bindings(), quit)}
	// figmaUser is Figma's details for george on 01-USER, its "Enabled,"
	// plain and "No password" red.
	figmaUser = BulletList{Items: []KeyValue{
		{Key: "Sudo", Value: "Enabled,", State: ValueBad, Note: "No password"},
		{Key: "SSH Keys", Value: "7 keys"},
		{Key: "UID:GID", Value: "1000:1000"},
		{Key: "Directory", Value: "/home/george"},
	}}
	// figmaSSH is Figma's SSH Configuration details (02-SSH_LOGIN), with
	// alice as the allowed user.
	figmaSSH = BulletList{Items: []KeyValue{
		{Key: "Root SSH Login", Value: "Enabled", State: ValueBad},
		{Key: "SSH Port", Value: "2230"},
		{Key: "Allowed Users", Value: "alice"},
	}}
	// figmaChecklist is the checklist under Figma's warning
	// (SSH_LOGIN-TOGGLE_CONFIRMATION).
	figmaChecklist = BulletList{Items: []KeyValue{
		{Key: "Non-root user has sudo privileges"},
		{Key: "Non-root user SSH access has been tested"},
		{Key: "If SSH fails, a fallback access method is available"},
	}}
	figmaWarning = Callout{Kind: MessageWarn, Title: "Warning", Body: []string{"Before disabling root SSH login, ensure:"}}
	// yesNo is a y/n question's inputs; next/p1c-nav owns the default and
	// its hint form (y/N).
	yesNo  = keys.Set{{Keys: []string{"y"}}, {Keys: []string{"n"}}}
	sshKey = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIExampleKeyForTestsOnlyNotARealKey0000000000 alice@web-01.example.net"
)

// withItem returns m with item i replaced by it.
func withItem(m Menu, i int, it MenuItem) Menu {
	m.Items = slices.Clone(m.Items)
	m.Items[i] = it
	return m
}

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
	{"menuheading/normal", figmaHeading},
	{"menuheading/no-mode", MenuHeading{Title: "Select an option"}},
	{"menuheading/long", MenuHeading{Title: "Select an option for web-01.example.net", Mode: &ModeIndicator{DryRun: true}}},
	{"menuheading/empty", MenuHeading{}},
	{"menu/main", figmaMenu},
	{"menu/sub", Menu{Items: []MenuItem{
		{Key: "1", Label: "Manage User", Description: "Configure sudo, SSH keys"},
		{Key: "2", Label: "Create User", Description: "Configure a new user"},
	}, Exit: MenuItem{Key: "0", Label: "Return"}}},
	{"menu/risk", withItem(sshMenu, 0, MenuItem{Key: "1", Label: "Root SSH Login", Description: "Toggle root SSH access", Risk: true})},
	{"menu/disabled", withItem(sshMenu, 0, MenuItem{Key: "1", Label: "Root SSH Login", Description: "Toggle root SSH access", Aside: "Proxmox", Disabled: true})},
	{"menu/long", Menu{Items: []MenuItem{
		{Key: "1", Label: "Unattended Upgrades Mail Reports", Description: "Send the daily report to alice"},
		{Key: "2", Label: "Firewall", Description: "Allow port 2230/tcp from 192.0.2.0/24 and deny the rest"},
	}, Exit: MenuItem{Key: "0", Label: "Return"}}},
	{"menu/empty", Menu{}},
	{"promptline/normal", figmaPrompt},
	{"promptline/no-hint", PromptLine{Label: "Username"}},
	{"promptline/empty", PromptLine{}},
	{"badge/normal", Badge{Text: "george"}},
	{"badge/long", Badge{Text: longHost}},
	{"badge/empty", Badge{}},
	{"subjectline/normal", SubjectLine{Label: "Managing", Subject: "george"}},
	{"subjectline/long-label", SubjectLine{Label: "Managing sudo method for", Subject: "george"}},
	{"subjectline/long", SubjectLine{Label: "Managing sudo method for", Subject: "alice-backup-replication-service"}},
	{"subjectline/no-label", SubjectLine{Subject: "george"}},
	{"subjectline/empty", SubjectLine{Label: "Managing"}},
	{"sectionheader/normal", SectionHeader{Title: "User Configuration"}},
	{"sectionheader/long", SectionHeader{Title: "Backup configuration for every host in the web-01.example.net cluster"}},
	{"sectionheader/empty", SectionHeader{}},
	{"breadcrumb/normal", Breadcrumb{Items: []string{"hardn", "Firewall"}}},
	{"breadcrumb/deep", Breadcrumb{Items: []string{"hardn", "User Management", "Manage User", "SSH Keys"}}},
	{"breadcrumb/long", Breadcrumb{Items: []string{"hardn", "Firewall application profiles", "Manage application profiles", "Allow 2230/tcp from 192.0.2.0/24"}}},
	{"breadcrumb/empty", Breadcrumb{}},
	{"keyvalue/plain", KeyValue{Key: "SSH Port", Value: "2230"}},
	{"keyvalue/ok", KeyValue{Key: "Sudo Method", Value: "Password required", State: ValueOK}},
	{"keyvalue/warn", KeyValue{Key: "SSH Keys", Value: "None", State: ValueWarn}},
	{"keyvalue/bad", KeyValue{Key: "Root SSH Login", Value: "Enabled", State: ValueBad}},
	{"keyvalue/unknown", KeyValue{Key: "Allowed Users", State: ValueUnknown}},
	{"keyvalue/not-applicable", KeyValue{Key: "UFW", State: ValueNotApplicable}},
	{"keyvalue/note", KeyValue{Key: "Backup Path", Value: "/var/backups/hardn", State: ValueOK, Note: "Writable"}},
	{"keyvalue/long-key", KeyValue{Key: "Unattended Upgrades Mail Reports", Value: "alice"}},
	{"keyvalue/long-value", KeyValue{Key: "Allowed Users", Value: "alice bob carol dave erin frank grace heidi ivan judy mallory"}},
	{"keyvalue/item", KeyValue{Key: "If SSH fails, a fallback access method is available"}},
	{"keyvalue/empty", KeyValue{}},
	{"bulletlist/user", figmaUser},
	{"bulletlist/ssh", figmaSSH},
	{"bulletlist/checklist", figmaChecklist},
	{"bulletlist/long", BulletList{Items: []KeyValue{
		{Key: "Backup State", Value: "Enabled", State: ValueOK},
		{Key: "Unattended Upgrades Mail Reports", Value: "alice"},
	}}},
	{"bulletlist/empty", BulletList{}},
	{"message/ok", Message{Kind: MessageOK, Text: "DNS configured successfully"}},
	{"message/warn", Message{Kind: MessageWarn, Text: "No nameservers configured"}},
	{"message/bad", Message{Kind: MessageBad, Text: "Failed to configure sudo: exit status 1"}},
	{"message/info", Message{Text: "No automatic backups will be created"}},
	{"message/cancelled", Message{Kind: MessageCancelled, Text: "Operation cancelled. No changes were made."}},
	{"message/long", Message{Kind: MessageBad, Text: "Failed to restart ssh.service: Job for ssh.service failed because the control process exited with error code; port 2230 is still in use"}},
	{"message/empty", Message{Kind: MessageOK}},
	{"callout/warn", figmaWarning},
	{"callout/ok", Callout{Kind: MessageOK, Title: "Operation Successful"}},
	{"callout/cancelled", Callout{Kind: MessageCancelled, Title: "Operation Cancelled"}},
	{"callout/bad", Callout{Kind: MessageBad, Body: []string{"sshd -t rejected the new configuration, so nothing was changed."}}},
	{"callout/info", Callout{Body: []string{"Dry-run mode: the plan was not applied."}}},
	{"callout/no-title", Callout{Kind: MessageWarn}},
	{"callout/long", Callout{Kind: MessageWarn, Title: "Changing the SSH port of web-01.example.net from 22 to 2230", Body: []string{
		"A user password is required for sudo usage.",
		"Keep this session open until a new connection to port 2230 from 192.0.2.0/24 succeeds.",
	}}},
	{"answeredfield/text", AnsweredField{Number: 1, Label: "Username", Answer: "george"}},
	{"answeredfield/yes-no", AnsweredField{Number: 2, Label: "Grant sudo privilege?", Accepts: yesNo, Answer: "y"}},
	{"answeredfield/confirm", AnsweredField{Label: "Disable SSH login for 'root'?", Accepts: yesNo, Answer: "y"}},
	{"answeredfield/unanswered", AnsweredField{Number: 3, Label: "Require sudo password?", Accepts: yesNo}},
	{"answeredfield/long-answer", AnsweredField{Number: 4, Label: "Paste SSH public key", Answer: sshKey}},
	{"answeredfield/empty", AnsweredField{Answer: "george"}},
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
	case Message:
		return v.Text == ""
	case AnsweredField:
		return v.Label == ""
	}
	return false
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
		{"heading: the mode at the value column", figmaHeading, from, false,
			"  Select an option         Dry-run Mode Disabled"},
		{"heading: the mode under it below StackBelow", figmaHeading, below, false,
			"  Select an option\n      Dry-run Mode Disabled"},
		{"heading: a title reaching the value column puts the mode under it", MenuHeading{Title: strings.Repeat("x", 25), Mode: &ModeIndicator{}}, from, false,
			"  " + strings.Repeat("x", 25) + "\n      Dry-run Mode Disabled"},
		{"heading: no mode", MenuHeading{Title: " Select an option\t"}, from, false, "  Select an option"},
		{"heading: no title is the mode alone", MenuHeading{Mode: &ModeIndicator{DryRun: true}}, from, false,
			strings.Repeat(" ", theme.ValueCol) + "Dry-run Mode Enabled"},
		// At a fixed width, so that it also runs with theme.StackBelow
		// refused: the mode then goes under the title only because its
		// word would not fit beside it.
		{"heading: under a cut title the mode's word stays whole", figmaHeading, 10, false,
			"  Selec...\n  Disabled"},
		{"menu: keys right-aligned, descriptions at the value column", Menu{Items: []MenuItem{
			{Key: "1", Label: "User Management", Description: "Create, Configure", Aside: "sudo, SSH keys"},
			{Key: "10", Label: "Logs", Description: "View log file"},
		}}, from, false,
			"   1) User Management      Create, Configure (sudo, SSH keys)\n  10) Logs                 View log file"},
		{"menu: the exit item after the section gap", Menu{Items: []MenuItem{{Key: "1", Label: "DNS"}}, Exit: figmaMenu.Exit}, from, false,
			"   1) DNS" + strings.Repeat("\n", theme.SectionGap+1) + "   0) Exit                 q to quit"},
		{"menu: a risky item's note line", Menu{Items: []MenuItem{{Key: "2", Label: "SSH Login", Description: "Toggle root SSH access", Risk: true}}}, from, false,
			"   2) SSH Login            Toggle root SSH access\n                           [*] Lockout risk"},
		{"menu: a disabled item's note line, and no risk", Menu{Items: []MenuItem{{Key: "3", Label: "DNS", Description: "Configure Nameservers", Disabled: true, Risk: true}}}, from, true,
			"   3) DNS                  Configure Nameservers\n                           ∅ Unavailable"},
		{"menu: stacked below StackBelow", Menu{Items: []MenuItem{{Key: "2", Label: "SSH Login", Description: "Toggle root SSH access", Risk: true}}}, below, false,
			"   2) SSH Login\n      Toggle root SSH access\n      [*] Lockout risk"},
		{"menu: a note with no description sits beside the label", Menu{Items: []MenuItem{{Key: "7", Label: "Run All", Risk: true}}}, from, false,
			"   7) Run All              [*] Lockout risk"},
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
			"   2) SSH Login\n      Toggle root SSH access\n      [*] Lockout risk\n   3) DNS\n      Configure Nameservers\n      [-] Unavailable"},
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
		{"prompt: glyph, label, generated hint and colon", figmaPrompt, 80, true,
			"  ❯ Enter your choice [0-10 or q]:"},
		{"prompt: with no hint the colon follows the label", PromptLine{Label: "Username"}, 80, false, "  > Username:"},
		{"prompt: too narrow, the label is cut, not the hint", figmaPrompt, 30, false,
			"  > Enter your... [0-10 or q]:"},
		{"prompt: with no letter of the label left, the hint alone", figmaPrompt, 20, false,
			"  > [0-10 or q]:"},
		{"prompt: empty is the glyph", PromptLine{}, 80, false, "  >"},
		{"badge: at the indent", Badge{Text: "web-01"}, 80, false, "  [web-01]"},
		{"badge: a long name is cut inside the brackets", Badge{Text: strings.Repeat("x", 70)}, 60, false,
			"  [" + strings.Repeat("x", 53) + "...]"},
		{"subject: the badge one cell after the colon", SubjectLine{Label: "Managing", Subject: " george "}, 80, false,
			"  Managing: [george]"},
		{"subject: too long for one line, the badge goes under the label", SubjectLine{Label: "Managing sudo method for", Subject: "alice-backup-replication-service"}, 60, false,
			"  Managing sudo method for:\n      [alice-backup-replication-service]"},
		{"subject: no label is the badge alone", SubjectLine{Subject: "george"}, 80, true, "  [george]"},
		{"section: stub, title and rule", SectionHeader{Title: "User Configuration"}, 80, false,
			"# User Configuration " + strings.Repeat("-", maxWidth(SectionHeader{}, 80)-21)},
		{"section: no title is the stub and the rule", SectionHeader{}, 60, true, "█ " + strings.Repeat("─", 58)},
		{"section: a long title is cut", SectionHeader{Title: strings.Repeat("x", 70)}, 60, false,
			"# " + strings.Repeat("x", 53) + "... -"},
		{"breadcrumb: items joined by the separator", Breadcrumb{Items: []string{"hardn", " Firewall", ""}}, 80, true,
			"  hardn › Firewall"},
		{"breadcrumb: too long, the middle items go first", Breadcrumb{Items: []string{"hardn", "User Management", "Manage User", "SSH Keys"}}, 40, false,
			"  hardn > ... > SSH Keys"},
		{"breadcrumb: then the first item", Breadcrumb{Items: []string{"hardn", "User Management", "Manage User", "SSH Keys"}}, 20, false,
			"  ... > SSH Keys"},
		{"breadcrumb: and last the current item is cut", Breadcrumb{Items: []string{"hardn", "Firewall application profiles"}}, 20, false,
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
			"  [+] a                    ok\n  [*] b                    warn\n  [ ] c                    bad\n" +
				"  [?] d                    Unknown\n  [-] e                    Not Applicable\n   -  f                    plain"},
		{"keyvalue: a note follows the plain value", KeyValue{Key: "Backup Path", Value: "/var/backups/hardn", State: ValueOK, Note: "Writable"}, from, true,
			"   ⛛  Backup Path          /var/backups/hardn Writable"},
		{"keyvalue: a long key stacks the row", KeyValue{Key: "Unattended Upgrades Mail Reports", Value: "alice"}, 80, false,
			"   -  Unattended Upgrades Mail Reports\n      alice"},
		{"keyvalue: a long value is cut", KeyValue{Key: "Allowed Users", Value: strings.Repeat("x", 60)}, from, false,
			"   -  Allowed Users        " + strings.Repeat("x", maxWidth(KeyValue{}, from)-theme.ValueCol-3) + "..."},
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
		{"message: the glyph in the marker column, the word at the label column, then the text", Message{Kind: MessageOK, Text: "DNS configured successfully"}, 80, false,
			"  [+] Success: DNS configured successfully"},
		{"message: a Unicode glyph is centred", Message{Kind: MessageBad, Text: "Failed"}, 80, true, "   ✗  Error: Failed"},
		{"message: a long text wraps under the word", Message{Text: "No automatic backups will be created"}, 20, false,
			"  [i] Info: No\n      automatic\n      backups will\n      be created"},
		{"message: a word too wide for its line breaks, as the text does", Message{Kind: MessageCancelled, Text: "Stopped"}, 12, false,
			"  [-] Cancel\n      led:\n      Stoppe\n      d"},
		{"callout: bar, glyph, title and body", figmaWarning, 80, false, "# [*] Warning\n# Before disabling root SSH login, ensure:"},
		{"callout: no title is the kind's own", Callout{Kind: MessageCancelled, Body: []string{""}}, 80, true, "█ ⊘ Operation Cancelled"},
		{"callout: a title of its own follows the kind's word", Callout{Kind: MessageBad, Title: " Port 2230 refused "}, 80, true, "█ ✗ Error: Port 2230 refused"},
		{"callout: a title that is the kind's word is not repeated", Callout{Kind: MessageOK, Title: "Success"}, 80, false, "# [+] Success"},
		{"callout: a body wraps beside the bar", Callout{Kind: MessageOK, Title: "Success", Body: []string{"aaa bbb ccc ddd"}}, 13, false,
			"# [+] Success\n# aaa bbb ccc\n# ddd"},
		{"callout: a long title is cut", Callout{Kind: MessageOK, Title: "Done"}, 9, false, "# [+]..."},
		{"field: the number right-aligned, the answer after the colon", AnsweredField{Number: 1, Label: "Username", Answer: "george"}, 80, false,
			"   1. Username: george"},
		{"field: the hint generated from the inputs", AnsweredField{Number: 2, Label: "Grant sudo privilege?", Accepts: yesNo, Answer: "y"}, 80, false,
			"   2. Grant sudo privilege? [y or n]: y"},
		{"field: unnumbered at the indent", AnsweredField{Label: "Disable SSH login for 'root'?", Accepts: yesNo, Answer: "y"}, 80, false,
			"  Disable SSH login for 'root'? [y or n]: y"},
		{"field: unanswered ends at the colon", AnsweredField{Number: 3, Label: "Require sudo password?", Accepts: yesNo}, 80, false,
			"   3. Require sudo password? [y or n]:"},
		{"field: a long answer goes under the question", AnsweredField{Number: 4, Label: "Paste SSH public key", Answer: sshKey}, 80, false,
			"   4. Paste SSH public key:\n      " + sshKey[:maxWidth(AnsweredField{}, 80)-theme.StackCol-3] + "..."},
		{"field: the question is cut to keep a short answer beside it", AnsweredField{Label: "Disable SSH login for 'root'?", Accepts: yesNo, Answer: "y"}, 40, false,
			"  Disable SSH login for '... [y or n]: y"},
		{"field: a wide number pushes the question", AnsweredField{Number: 12345, Label: "Username"}, 80, false, "12345. Username:"},
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
			{"(key-only)", m.Disabled}, {g.NotApplicable + " " + unavailable, m.Disabled},
		}},
		{"risk", Menu{Items: []MenuItem{{Key: "1", Label: "Run All", Risk: true}}}, []part{{g.Risk + " " + lockoutRisk, m.Risk}}},
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
			{" ", th.Section.Fill}, {"User Configuration", th.Section.Title},
			{strings.Repeat(th.Glyphs.Rule, maxWidth(SectionHeader{}, 80)-21), th.Section.Rule},
		}},
		{"breadcrumb", Breadcrumb{Items: []string{"hardn", "Firewall"}}, []part{
			{"hardn", th.Breadcrumb.Item}, {th.Glyphs.Separator, th.Breadcrumb.Separator}, {"Firewall", th.Breadcrumb.Current},
		}},
		{"key-value", figmaUser, []part{
			{th.Glyphs.Bullet, th.KeyValue.Bullet}, {"Sudo", th.KeyValue.Key},
			{"Enabled,", th.KeyValue.Value}, {"No password", th.KeyValue.ValueBad}, {"7 keys", th.KeyValue.Value},
		}},
		{"bullet item", figmaChecklist, []part{{"Non-root user has sudo privileges", th.KeyValue.Key}}},
		{"message", Message{Kind: MessageWarn, Text: "No nameservers configured"}, []part{
			{th.Glyphs.Warn, th.Message.Warn}, {"Warning:", th.Message.Warn}, {"No nameservers configured", th.Message.Text},
		}},
		{"callout", figmaWarning, []part{
			{" ", th.Callout.Warn.Bar}, {th.Glyphs.Warn + " Warning", th.Callout.Warn.Title}, {"Before disabling root SSH login, ensure:", th.Callout.Body},
		}},
		{"field", AnsweredField{Number: 2, Label: "Grant sudo privilege?", Accepts: yesNo, Answer: "y"}, []part{
			{"2.", th.Field.Number}, {"Grant sudo privilege?", th.Field.Label}, {"[y or n]:", th.Field.Hint}, {"y", th.Field.Answer},
		}},
		{"field with no hint", AnsweredField{Label: "Username", Answer: "george"}, []part{{":", th.Field.Label}}},
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
// colour and as its own status marker in place of the bullet, in the same
// colour, and that the marker is there without colour too, so that no
// state rests on colour alone. A plain value keeps the bullet.
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
		{ValueState(-1), kv.Value, kv.Bullet, g.Bullet},
	}
	shown := map[string]bool{}
	for _, tt := range tests {
		vm := KeyValue{Key: "SSH Port", Value: "2230", State: tt.state}
		got := vm.Render(colour)
		if !strings.Contains(got, tt.style.Render("2230")) || !strings.Contains(got, tt.mark.Render(tt.glyph)) {
			t.Errorf("state %d with colour: got %q, want the marker %q and the value in their styles", tt.state, got, tt.glyph)
		}
		want := marker(tt.glyph, lipgloss.NewStyle()).String() + " "
		for _, c := range []Ctx{colour, none} {
			if plain := ansi.Strip(vm.Render(c)); !strings.HasPrefix(plain, want) {
				t.Errorf("state %d, %v: got %q, want it to start with %q", tt.state, c.Env.Profile, plain, want)
			}
		}
		shown[tt.glyph] = true
	}
	if len(shown) != 6 {
		t.Errorf("the six states show %d different markers, want 6: %v", len(shown), shown)
	}
}

// TestMessageKinds checks that each kind has its own glyph and word, both
// in its style, on a Message, and its own bar, glyph, title style and
// title on a Callout; and that without colour, at the ASCII profile and in
// plain mode, the five kinds' messages, and their callouts given one title
// of their own, still differ. A value outside the kinds is MessageInfo.
func TestMessageKinds(t *testing.T) {
	c := NewCtx(term.Fixed(80, colorprofile.TrueColor, true, true))
	m, co, g := c.Theme.Message, c.Theme.Callout, c.Theme.Glyphs
	tests := []struct {
		kind        MessageKind
		glyph       string
		style       lipgloss.Style
		bar         theme.CalloutKind
		word, title string
	}{
		{MessageInfo, g.Info, m.Info, co.Info, "Info", "Note"},
		{MessageOK, g.OK, m.OK, co.OK, "Success", "Operation Successful"},
		{MessageWarn, g.Warn, m.Warn, co.Warn, "Warning", "Warning"},
		{MessageBad, g.Bad, m.Bad, co.Bad, "Error", "Operation Failed"},
		{MessageCancelled, g.Cancelled, m.Cancelled, co.Cancelled, "Cancelled", "Operation Cancelled"},
		{MessageKind(9), g.Info, m.Info, co.Info, "Info", "Note"},
	}
	words, titles := map[string]bool{}, map[string]bool{}
	for _, tt := range tests {
		got := Message{Kind: tt.kind, Text: "x"}.Render(c)
		for _, want := range []string{tt.style.Render(tt.glyph), tt.style.Render(tt.word + ":")} {
			if !strings.Contains(got, want) {
				t.Errorf("kind %d: message %q, want it to hold %q", tt.kind, got, want)
			}
		}
		got = Callout{Kind: tt.kind}.Render(c)
		if want := tt.bar.Bar.Render(" ") + " " + tt.bar.Title.Render(tt.glyph+" "+tt.title); got != want {
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
	// A word broken over lines keeps its style on each.
	narrow := NewCtx(term.Fixed(12, colorprofile.TrueColor, false, true))
	got := Message{Kind: MessageCancelled, Text: "Stopped"}.Render(narrow)
	for _, want := range []string{m.Cancelled.Render("Cancel"), m.Cancelled.Render("led:"), m.Text.Render("Stoppe")} {
		if !strings.Contains(got, want) {
			t.Errorf("narrow message %q, want it to hold %q", got, want)
		}
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
