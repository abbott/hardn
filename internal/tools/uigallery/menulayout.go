package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/colorprofile"

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
	hdr "github.com/abbott/hardn/pkg/engine/header"
	"github.com/abbott/hardn/pkg/engine/risk"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/output"
	"github.com/abbott/hardn/pkg/ui/component"
	"github.com/abbott/hardn/pkg/ui/screen"
	"github.com/abbott/hardn/pkg/ui/term"
)

// The main menu's layout (docs/engine/gallery-menu-layout.md, adopted
// 2026-11-14): "+" for "Configured", SSH Auth in the SSH Login row, a
// Kernel row, a row for DNS, NTP or Syslog only with a problem, "~" under
// a row, and the items' words. Each mock is reports, written here and not
// read from a host, drawn through hardn's own mapping (risk.Assess,
// output.StatusViewOf) and its own items
// (screen.MainMenuItems, held to mainmenu.MenuOf by TestMenuLayoutFits). The mocks of the forms that were not adopted
// (brackets, a sign for every state, option A's rows) are gone.

// mlPrefix names the listing and its goldens.
const mlPrefix = "menu-layout"

// mlLabel is the source label every mock's note opens with.
const mlLabel = "Adopted 2026-11-14: no Figma frame."

// The values the maintainer drew.
const (
	mlServer = "10.2.8.1"
	mlSyslog = "10.2.8.240:514 TCP"
)

// The controls' IDs and titles, by a short name.
var (
	mlUser, mlLogin, mlAuth, mlPort engine.Control = users.Admin{}, ssh.Login{}, ssh.Auth{}, ssh.Port{}
	mlFirewall, mlKernel            engine.Control = firewall.Incoming{}, kernelsettings.Settings{}
	mlUpdates, mlAppArmor           engine.Control = updates.Auto{}, apparmor.Enforce{}
	mlDNS, mlNTP, mlLogs            engine.Control = dns.Nameservers{}, timesync.Sync{}, logforward.Forward{}
)

// mlReport is c's report: state s, detail d.
func mlReport(c engine.Control, s engine.State, d string) engine.Report {
	return engine.Report{Control: c.ID(), Title: c.Title(), Status: engine.Status{State: s, Detail: d}}
}

// mlOK is c's report, Configured.
func mlOK(c engine.Control, d string) engine.Report { return mlReport(c, engine.Configured, d) }

// mlProblem is c's report with a problem the main menu shows: state s,
// the problem's words and the host's value.
func mlProblem(c engine.Control, s engine.State, problem, value string) engine.Report {
	r := mlReport(c, s, strings.ToLower(problem))
	r.Status.Problem, r.Status.Value = problem, value
	return r
}

// mlConfigured are the reports of a Debian 12 host with everything
// configured, the User row's sudo without a password: the scored rows',
// then DNS, NTP and Syslog, each fine.
func mlConfigured() []engine.Report {
	user := mlOK(mlUser, "non-root, sudo")
	user.Status.Findings = []engine.Finding{{Code: users.CodeNoPassword}}
	return []engine.Report{user, mlOK(mlLogin, "root disallowed"), mlOK(mlAuth, "key-only auth"), mlOK(mlPort, "non-default (2230)"),
		mlOK(mlFirewall, "deny policy"), mlOK(mlKernel, kernelsettings.Applied(6)), mlOK(mlUpdates, "unattended-upgrades"),
		mlOK(mlAppArmor, "10 profiles enforced"), mlOK(mlDNS, mlServer), mlOK(mlNTP, mlServer), mlOK(mlLogs, mlSyslog)}
}

// mlWith is reports with each of changed in place of its control's.
func mlWith(reports []engine.Report, changed ...engine.Report) []engine.Report {
	out := append([]engine.Report(nil), reports...)
	for _, c := range changed {
		for i, r := range out {
			if r.Control == c.Control {
				out[i] = c
			}
		}
	}
	return out
}

// mlMixed are the reports of a host in mixed states.
func mlMixed() []engine.Report {
	return mlWith(mlConfigured(),
		mlReport(mlLogin, engine.Partial, "root key-only"),
		mlReport(mlAuth, engine.NotConfigured, "password auth on"),
		mlReport(mlFirewall, engine.Unknown, "can't read"),
		mlReport(mlKernel, engine.Partial, fmt.Sprintf(kernelsettings.DetailOutside, 1)),
		mlReport(mlUpdates, engine.NotConfigured, "no unattended-upgrades"),
		mlReport(mlAppArmor, engine.NotApplicable, "set by the host"))
}

// mlHeader is a host read with nothing pending: up two days, no reboot due.
func mlHeader() hdr.Header {
	var h hdr.Header
	h.Reboot.Pending = detect.No
	h.Uptime.Known, h.Uptime.Duration = true, 48*time.Hour
	return h
}

// mlView is reports as the main menu draws them: assessed and mapped,
// which leaves out the row of what has no problem.
func mlView(reports []engine.Report) output.StatusView {
	return output.StatusViewOf(reports, risk.Assess(reports, mlHeader()))
}

// mlBars are the context bars of the mocks' hosts that are not the
// fixture's Debian 12; the versions are the mock's.
var mlBars = map[detect.Distro]component.ContextBar{
	detect.DistroRocky:   {Name: "Rocky Linux", Version: "9.6", Codename: "Blue Onyx"},
	detect.DistroProxmox: {Name: "Proxmox VE", Version: "9.0.10", Codename: "Trixie"},
}

// mlMenu is the main menu of a host with facts f and reports: the
// fixture's header and host, its bar or f's distribution's (mlBars);
// hardn's rows and items.
func mlMenu(f detect.Facts, reports []engine.Report) screen.MainMenu {
	vm := screen.MainMenuFixture()
	if bar, ok := mlBars[f.Platform.Distro]; ok {
		vm.Context = bar
	}
	v := mlView(reports)
	vm.Risk, vm.Status, vm.Info = v.Risk, v.Status, v.Info
	vm.Menu.Items = screen.MainMenuItems(f.Proxmox.Installed == detect.Yes, f.Platform.Distro.EL())
	return vm
}

// mlBlock is a labelled group of rows.
type mlBlock struct {
	label   string
	reports []engine.Report
	// items are menu items under the rows, a blank line between.
	items []component.MenuItem
}

// mlRows are b's rows as the main menu draws them: the scored rows b has a
// report for, then, after a blank line, the rows of what has a problem.
func (b mlBlock) rows(c component.Ctx) string {
	has := map[string]bool{}
	for _, r := range b.reports {
		has[r.Title] = true
	}
	v := mlView(b.reports)
	var scored component.StatusList
	for _, r := range v.Status.Rows {
		if has[r.Label] {
			scored.Rows = append(scored.Rows, r)
		}
	}
	var out []string
	for _, s := range []string{scored.Render(c), v.Info.Render(c), component.Menu{Items: b.items}.Render(c)} {
		if s != "" {
			out = append(out, s)
		}
	}
	return strings.Join(out, "\n\n")
}

// mlBlocks draws each block: its label, then its rows.
func mlBlocks(c component.Ctx, blocks ...mlBlock) string {
	var out []string
	for _, b := range blocks {
		out = append(out, strings.TrimPrefix(b.label+"\n", "\n")+b.rows(c))
	}
	return apart(out...)
}

// mlNoColour is c with no colour and ASCII glyphs, at c's width: NO_COLOR,
// or output that is not a terminal.
func mlNoColour(c component.Ctx) component.Ctx {
	return component.NewCtx(term.Fixed(c.Env.Width, colorprofile.NoTTY, false, c.Env.Dark))
}

// Details as wide as the value column after "+ ", 35 cells, and wider.
const (
	mlDetail35 = "deny policy, 9 rules, 3 profiles on"
	mlDetail40 = "deny policy, 14 rules, 3 profiles in use"
)

// mlSSHCases are the merged SSH Login row's cases: SSH Login's and SSH
// Auth's reports on each host.
var mlSSHCases = []struct {
	label       string
	login, auth engine.Report
}{
	{"Both configured", mlOK(mlLogin, "root disallowed"), mlOK(mlAuth, "key-only auth")},
	{"sshd allows passwords", mlOK(mlLogin, "root disallowed"), mlReport(mlAuth, engine.NotConfigured, "password auth on")},
	{"Root may log in", mlReport(mlLogin, engine.NotConfigured, "root allowed"), mlOK(mlAuth, "key-only auth")},
	{"Both off, one worse", mlReport(mlLogin, engine.Partial, "root key-only"), mlReport(mlAuth, engine.NotConfigured, "password auth on")},
	{"Both off alike", mlReport(mlLogin, engine.NotConfigured, "root allowed"), mlReport(mlAuth, engine.NotConfigured, "password auth on")},
	{"A long detail", mlOK(mlLogin, "root disallowed"), mlReport(mlAuth, engine.Partial, "match block allows passwords")},
	{"Proxmox VE, both configured", mlOK(mlLogin, ssh.DetailProxmoxKeyOnly), mlOK(mlAuth, "key-only auth")},
	{"Passwords on, root not read", mlReport(mlLogin, engine.Unknown, "can't tell if root key works"), mlReport(mlAuth, engine.NotConfigured, "password auth on")},
	{"sshd not read", mlReport(mlLogin, engine.Unknown, "can't read"), mlReport(mlAuth, engine.Unknown, "can't read")},
}

// mlRocky and mlPVE are a Rocky Linux host's facts and a Proxmox VE
// node's, and their reports, in the controls' words; the counts are the
// mock's.
func mlRocky() (detect.Facts, []engine.Report) {
	var f detect.Facts
	f.Platform.Distro = detect.DistroRocky
	return f, mlWith(mlConfigured(), mlOK(mlFirewall, "firewalld running"), mlOK(mlUpdates, updates.DetailDnfSecurity),
		mlReport(mlAppArmor, engine.NotApplicable, "uses SELinux"))
}

func mlPVE() (detect.Facts, []engine.Report) {
	var f detect.Facts
	f.Platform.Distro, f.Proxmox.Installed = detect.DistroProxmox, detect.Yes
	return f, mlWith(mlConfigured(), mlOK(mlLogin, ssh.DetailProxmoxKeyOnly), mlOK(mlFirewall, "drop policy"),
		mlOK(mlKernel, kernelsettings.Applied(4)), mlReport(mlUpdates, engine.NotApplicable, updates.DetailByHand))
}

// mlMocks are the listing's mocks, in order.
func mlMocks() []listMock {
	menu := func(host func() (detect.Facts, []engine.Report)) func(context.Context, component.Ctx) (string, error) {
		return func(_ context.Context, c component.Ctx) (string, error) { return mlMenu(host()).Render(c), nil }
	}
	blocks := func(b func() []mlBlock) func(context.Context, component.Ctx) (string, error) {
		return func(_ context.Context, c component.Ctx) (string, error) { return mlBlocks(c, b()...), nil }
	}
	debian := func() (detect.Facts, []engine.Report) { return detect.Facts{}, mlConfigured() }
	scored := func(reports []engine.Report) []engine.Report { return reports[:8] }
	problem := func(rs ...engine.Report) []engine.Report {
		return append([]engine.Report{mlOK(mlAppArmor, "10 profiles enforced")}, rs...)
	}
	return []listMock{
		{name: "menu", draw: menu(debian),
			note: "A Debian 12 host with everything configured: \"+\" for \"Configured\", SSH Auth in the SSH Login row, a Kernel row, no DNS, NTP or Syslog row, \"~\" under a row, item 4 Kernel."},
		{name: "mixed", draw: func(_ context.Context, c component.Ctx) (string, error) {
			return apart(mlBlocks(c, mlBlock{label: "Mixed states:", reports: scored(mlMixed())}),
				mlBlocks(mlNoColour(c), mlBlock{label: "No colour:", reports: scored(mlMixed())})), nil
		}, note: "A host in mixed states: a row that is not Configured keeps its word. A line under a row starts two cells in, whatever the row."},
		{name: "ssh", draw: blocks(func() []mlBlock {
			var out []mlBlock
			for _, k := range mlSSHCases {
				out = append(out, mlBlock{label: k.label + ":", reports: []engine.Report{k.login, k.auth}})
			}
			return out
		}), note: "The SSH Login row with SSH Auth in it: the worse of the two states, and the details of the parts in that state. A part read as off outranks one not read. Both off alike is the one pair cut at 64 columns."},
		{name: "kernel", draw: blocks(func() []mlBlock {
			var out []mlBlock
			for _, k := range []struct {
				label string
				r     engine.Report
			}{
				{"hardn's file sets 6 settings:", mlOK(mlKernel, kernelsettings.Applied(6))},
				{"It sets one:", mlOK(mlKernel, kernelsettings.Applied(1))},
				{"At the baseline, nothing applied:", mlOK(mlKernel, kernelsettings.DetailBaseline)},
				{"Applied, then 3 fell below:", mlReport(mlKernel, engine.Partial, fmt.Sprintf(kernelsettings.DetailToSet, 3))},
				{"Never applied:", mlReport(mlKernel, engine.NotConfigured, fmt.Sprintf(kernelsettings.DetailToSet, 6))},
				{"In a container:", mlReport(mlKernel, engine.NotApplicable, kernelsettings.DetailContainer)},
			} {
				out = append(out, mlBlock{label: k.label, reports: []engine.Report{mlOK(mlFirewall, "deny policy"), k.r, mlOK(mlUpdates, "unattended-upgrades")}})
			}
			return out
		}), note: "The Kernel row, between Firewall and Auto Updates, on every host. The row reads the host: it needs no visit to the screen."},
		{name: "problems", draw: blocks(func() []mlBlock {
			return []mlBlock{
				{label: "All fine: no row.", reports: problem(mlOK(mlDNS, mlServer), mlOK(mlNTP, mlServer), mlOK(mlLogs, mlSyslog))},
				{label: "The syslog server unreachable, DNS and NTP fine:", reports: problem(mlOK(mlDNS, mlServer), mlOK(mlNTP, mlServer),
					mlProblem(mlLogs, engine.Partial, logforward.ProblemNotReached, mlSyslog))},
				{label: "All three:", reports: problem(mlProblem(mlDNS, engine.NotConfigured, dns.ProblemNotHardn, "192.168.1.1"),
					mlProblem(mlNTP, engine.Partial, timesync.ProblemNotInSync, mlServer), mlProblem(mlLogs, engine.Partial, "1 unreachable", "3 servers"))},
				{label: "hardn.yml names servers, the host has none:", reports: problem(mlProblem(mlNTP, engine.NotConfigured, timesync.ProblemNoService, ""),
					mlProblem(mlLogs, engine.NotConfigured, logforward.ProblemOff, ""))},
				{label: "Not read, and a service stopped:", reports: problem(mlProblem(mlDNS, engine.Unknown, dns.ProblemCantRead, ""),
					mlProblem(mlNTP, engine.NotConfigured, "chrony not running", ""))},
			}
		}), note: "DNS, NTP and Syslog have a row only with a problem, after a blank line under AppArmor: the host's value and the problem under it, or the problem alone. None counts in the risk level."},
		{name: "long-detail", draw: blocks(func() []mlBlock {
			return []mlBlock{{label: "35 and 40 cells:", reports: []engine.Report{mlOK(mlFirewall, mlDetail35)}},
				{reports: []engine.Report{mlOK(mlFirewall, mlDetail40)}}}
		}), note: "Details of 35 and 40 cells, made up: no scored row's is this long. At 64 columns a detail after \"+\" has 35 cells; a longer one is cut."},
		{name: "config-menu", draw: func(_ context.Context, c component.Ctx) (string, error) {
			return screen.ConfigGroup{}.SubMenu().Render(c), nil
		},
			note: "Main menu 8: hardn.yml, DNS, NTP, Syslog. Kernel Settings is main menu 4."},
		{name: "el", draw: menu(mlRocky),
			note: "Rocky Linux: firewalld has no rules to manage in hardn, so item 3 says so. The same on AlmaLinux, RHEL, CentOS Stream and Oracle Linux."},
		{name: "pve", draw: menu(mlPVE),
			note: "Proxmox VE: hardn reads pve-firewall and enables no updates there, so items 3 and 5 keep v0.10.0's words."},
	}
}

// mlUsage is the listing's usage line.
const mlUsage = "usage: go run ./internal/tools/uigallery menu-layout [flags] [mock | all]..."

// mlListing is the menu layout's listing.
var mlListing = listing{prefix: mlPrefix, usage: mlUsage, label: mlLabel, mocks: mlMocks}
