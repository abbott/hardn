// Package fixture is the catalogue of pkg/ui/component's states: every
// component view-model the golden tests render, by golden name (States),
// and the named fixtures the states and the component tests are built
// from. The component goldens and the dev-only gallery
// (internal/tools/uigallery) both take their states from States, so the
// gallery shows exactly what the goldens pin and cannot drift from them.
//
// It is test and tool support, never imported by production code: the
// view-models hold mock data. The data is synthetic: Figma's mock host
// BOS-SAN-L01 and user george, alice and bob, web-01.example.net,
// addresses from 192.0.2.0/24 and the SSH port 2230.
//
// Each function returns a fresh value, so that a caller that changes one
// changes no other caller's.
package fixture

import (
	"fmt"
	"slices"
	"strings"

	"github.com/abbott/hardn/pkg/diff"
	"github.com/abbott/hardn/pkg/ui/component"
	"github.com/abbott/hardn/pkg/ui/keys"
)

// Renderer is any component's view-model.
type Renderer interface{ Render(component.Ctx) string }

// State is one state of one component.
type State struct {
	// Name is "<component>/<state>", the component's type name in lower
	// case: the golden file testdata/golden/<Name>.golden under
	// pkg/ui/component.
	Name string
	// VM is the view-model. An Animated one renders with Env.Animate set.
	VM Renderer
}

// Animated is a view-model rendered on a terminal that can animate, such
// as a running StepProgress's spinner: Render sets Env.Animate. The golden
// test also sets Env.Animate in every cell of such a state, so that the
// golden file's headers say so.
type Animated struct{ VM Renderer }

// Render renders a.VM with Env.Animate set.
func (a Animated) Render(c component.Ctx) string {
	c.Env.Animate = true
	return a.VM.Render(c)
}

// URL is the repository link the header shows.
const URL = "https://github.com/abbott/hardn"

// SSHKey is a public key in the OpenSSH format, made up for the tests.
const SSHKey = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIExampleKeyForTestsOnlyNotARealKey0000000000 alice@web-01.example.net"

// The plan group's files: changes as the engine would plan them on
// web-01.example.net, with alice as the admin user.
const (
	DropIn      = "/etc/ssh/sshd_config.d/00-hardn.conf"
	Resolv      = "/etc/resolv.conf"
	SudoersFile = "/etc/sudoers.d/hardn-alice"
	UFWRules    = "/etc/ufw/user.rules"
)

// Values only the states use.
const (
	uptime   = "182 days, 2 hours, 26 minutes"
	longHost = "ci-runner-0042.build-cluster.eu-west-1.web-01.example.net"
	// longLine is wider than the 35 cells a line under a status row has
	// beside the label at 64 columns, as no line the status model makes
	// is (pkg/engine/risk TestFactorsFit).
	longLine = "Up 182 days without automatic updates or a reboot"
	// sshOutput is what a failed reload of ssh.service writes.
	sshOutput = "Job for ssh.service failed because the control process exited with error code.\n" +
		"See \"systemctl status ssh.service\" and \"journalctl -xeu ssh.service\" for details.\n"
)

// stepTitles are the steps of the SSH port change, as it applies.
var stepTitles = []string{"Allow 2230/tcp in UFW", "Set Port 2230", "Reload ssh.service", "Remove the rule for port 22"}

// FigmaRisk is Figma's main-menu risk line: Moderate, medium security.
func FigmaRisk() component.RiskSummary {
	return component.RiskSummary{Band: component.RiskModerate, Level: "Moderate", Gloss: "medium security"}
}

// FigmaStatus is Figma's main-menu status rows, with port 2230.
func FigmaStatus() component.StatusList {
	return component.StatusList{Rows: []component.StatusRow{
		{Label: "User", State: component.StatusConfigured, Detail: "non-root, sudo"},
		{Label: "Firewall", State: component.StatusConfigured, Detail: "deny policy"},
		{Label: "SSH Login", State: component.StatusConfigured, Detail: "root disallowed"},
		{Label: "SSH Auth", State: component.StatusConfigured, Detail: "key-only auth"},
		SSHPort(),
		{Label: "AppArmor", State: component.StatusNotConfigured},
		{Label: "Auto Updates", State: component.StatusNotConfigured},
	}}
}

// linedStatus is Figma's main-menu status rows with lines under three of
// them, as the main menu draws the risk level's factors (the maintainer,
// 2026-10-21): a cost under User, AppArmor not read with its line, and
// two under Auto Updates; the rows stay one block.
func linedStatus(noPassword, countedOff component.StatusLine) component.StatusList {
	l := FigmaStatus()
	l.Rows[0].Lines = []component.StatusLine{noPassword}
	l.Rows[5] = component.StatusRow{Label: "AppArmor", Detail: "can't read", Lines: []component.StatusLine{countedOff}}
	l.Rows[6] = component.StatusRow{Label: "Auto Updates", State: component.StatusPartial, Detail: "off after restart",
		Lines: []component.StatusLine{{Text: "Can't tell if fully on"}, {Text: "Up 182 days"}}}
	return l
}

// SSHPort is Figma's SSH Port status row, with port 2230.
func SSHPort() component.StatusRow {
	return component.StatusRow{Label: "SSH Port", State: component.StatusConfigured, Detail: "non-default (2230)"}
}

// FigmaMenu is Figma's main menu, its Exit row in the brief's words: the
// help of the navigation grammar's q (brief 1C).
func FigmaMenu() component.Menu {
	return component.Menu{Items: []component.MenuItem{
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
	}, Exit: component.MenuItem{Key: "0", Label: "Exit", Description: keys.Set{keys.Quit()}.Help()}}
}

// CompactMenu is Figma's main menu in two columns, as the main menu's
// compact form lays it out on a short terminal.
func CompactMenu() component.Menu {
	m := FigmaMenu()
	m.Compact = true
	return m
}

// SSHMenu is Figma's SSH Login sub-menu (02-SSH_LOGIN).
func SSHMenu() component.Menu {
	return component.Menu{Items: []component.MenuItem{
		{Key: "1", Label: "Root SSH Login", Description: "Toggle root SSH access"},
		{Key: "2", Label: "View SSH Settings", Description: "Show SSH configuration"},
	}, Exit: component.MenuItem{Key: "0", Label: "Return"}}
}

// FigmaHeading is Figma's menu heading, "Select an option", with the live
// mode beside it.
func FigmaHeading() component.MenuHeading {
	return component.MenuHeading{Title: "Select an option", Mode: &component.ModeIndicator{}}
}

// FigmaPrompt is Figma's menu prompt for FigmaMenu, with q.
func FigmaPrompt() component.PromptLine {
	return component.PromptLine{Label: "Enter your choice", Accepts: append(FigmaMenu().Bindings(), keys.Quit())}
}

// FigmaUser is Figma's details for george on 01-USER, its "Enabled,"
// plain and "No password" red.
func FigmaUser() component.BulletList {
	return component.BulletList{Items: []component.KeyValue{
		{Key: "Sudo", Value: "Enabled,", State: component.ValueBad, Note: "No password"},
		{Key: "SSH Keys", Value: "7 keys"},
		{Key: "UID:GID", Value: "1000:1000"},
		{Key: "Directory", Value: "/home/george"},
	}}
}

// FigmaChecklist is the checklist under Figma's warning
// (SSH_LOGIN-TOGGLE_CONFIRMATION).
func FigmaChecklist() component.BulletList {
	return component.BulletList{Items: []component.KeyValue{
		{Key: "Non-root user has sudo privileges"},
		{Key: "Non-root user SSH access has been tested"},
		{Key: "If SSH fails, a fallback access method is available"},
	}}
}

// FigmaWarning is Figma's warning callout (SSH_LOGIN-TOGGLE_CONFIRMATION).
func FigmaWarning() component.Callout {
	return component.Callout{Kind: component.MessageWarn, Title: "Warning", Body: []string{"Before disabling root SSH login, ensure:"}}
}

// Users are a form choice's options, george and alice. Figma draws a
// choice of user only as a menu screen (USER-MANAGE_USER-SELECT_USER),
// which nav builds; no Figma screen draws a form's choice (DEV-046).
func Users() []component.MenuItem {
	return []component.MenuItem{{Key: "1", Label: "george"}, {Key: "2", Label: "alice"}}
}

// SSHChange moves sshd to port 2230 in DropIn: a lockout risk.
func SSHChange() component.PlanChange {
	return component.PlanChange{
		Title: "Set Port 2230", Target: DropIn, LockoutRisk: true,
		Diff: diff.Unified(DropIn, DropIn,
			"# Managed by hardn\nPort 22\nPermitRootLogin no\n",
			"# Managed by hardn\nPort 2230\nPermitRootLogin no\n"),
		Commands: []string{"sshd -t", "systemctl reload ssh"},
	}
}

// DNSChange sets two nameservers in Resolv.
func DNSChange() component.PlanChange {
	return component.PlanChange{
		Title: "Set the nameservers", Target: Resolv,
		Diff: diff.Unified(Resolv, Resolv,
			"# Generated by hardn\nnameserver 192.0.2.1\n",
			"# Generated by hardn\nnameserver 192.0.2.53\nnameserver 192.0.2.54\n"),
	}
}

// UFWChange enables UFW, a change of state with no file: a lockout risk.
func UFWChange() component.PlanChange {
	return component.PlanChange{
		Title: "Enable UFW", Target: "ufw", LockoutRisk: true,
		Before: "inactive", After: "active\ndeny (incoming), allow (outgoing)",
		Commands: []string{"ufw allow 2230/tcp", "ufw --force enable"},
	}
}

// SSHFailure is the report of a failed reload of ssh.service, after the
// two steps before it were rolled back.
func SSHFailure() component.ErrorReport {
	return component.ErrorReport{
		Failed:     "Reloading ssh.service failed: exit status 1",
		RolledBack: []string{"Set Port 2230", "Allow 2230/tcp in UFW"},
		Command:    "systemctl reload ssh", Output: sshOutput,
		LogPath: "/var/log/hardn.log", Next: "Run sudo sshd -t to see what sshd rejected.",
	}
}

// Steps returns the steps of the SSH port change in the given states, in
// order: at most four.
func Steps(states ...component.StepState) []component.Step {
	out := make([]component.Step, len(states))
	for i, st := range states {
		out[i] = component.Step{Title: stepTitles[i], State: st}
	}
	return out
}

// PortSteps returns the five steps of the engine's 2230 change on Debian
// 13 (the gallery's ssh-port-debian13 and failed-reload-debian13
// scenarios), at its reload's failure: done to it, the reload failed with
// its code, the check after it skipped.
func PortSteps() []component.Step {
	return []component.Step{
		{Title: "Install hardn's boot unit", State: component.StepDone},
		{Title: "ufw: allow 2230/tcp", State: component.StepDone},
		{Title: "Add SSH port 2230", State: component.StepDone},
		{Title: "Reload sshd (ssh.service)", State: component.StepFailed, Detail: "COMMAND_FAILED"},
		{Title: "sshd -T shows ports 22, 2230", State: component.StepSkipped},
	}
}

// States returns every component state, by golden name, in the order of
// the design-system README's component table (§9.11): each component's
// states together.
func States() []State {
	// figmaHost is Figma's host block; BOS-SAN-L01 is its mock hostname.
	figmaHost := component.HostBlock{Hostname: "BOS-SAN-L01", Address: "192.0.2.10", Uptime: uptime}
	// The lines under a status row: a cost hardn established, in
	// status.warn, and one that rests on what it could not establish.
	noPassword := component.StatusLine{Text: "No password to become root", Warn: true}
	countedOff := component.StatusLine{Text: "Counted as not configured"}
	// figmaSSH is Figma's SSH Configuration details (02-SSH_LOGIN), with
	// alice as the allowed user.
	figmaSSH := component.BulletList{Items: []component.KeyValue{
		{Key: "Root SSH Login", Value: "Enabled", State: component.ValueBad},
		{Key: "SSH Port", Value: "2230"},
		{Key: "Allowed Users", Value: "alice"},
	}}
	// yesNo is a y/N question's answers, no by default; yesDefault's
	// default is yes. answeredfield/unanswered asks Figma's "Require sudo
	// password?" with it to show the capital Y: that is the default P-11
	// proposes, not a decision; the screens set each question's default.
	yesNo, yesDefault := keys.YesNo(false), keys.YesNo(true)
	return []State{
		{"header/normal", component.Header{Version: "v0.4.0", URL: URL}},
		{"header/no-version", component.Header{URL: URL}},
		{"header/no-url", component.Header{Version: "v0.4.0"}},
		{"contextbar/normal", component.ContextBar{Name: "Debian", Version: "12", Codename: "Bookworm"}},
		{"contextbar/unknown", component.ContextBar{}},
		{"contextbar/no-codename", component.ContextBar{Name: "Alpine Linux", Version: "3.24"}},
		{"contextbar/long", component.ContextBar{Name: "Proxmox Virtual Environment", Version: "9.0.10", Codename: "Trixie, vendor build"}},
		{"titlebar/normal", component.TitleBar{Title: "User Management"}},
		{"titlebar/long", component.TitleBar{Title: "Firewall application profiles and port rules for web-01.example.net"}},
		// The trail, P-08: off on the screens by default (screen.TitleTrail),
		// pinned here on.
		{"titlebar/trail", component.TitleBar{Title: "Manage User", Parents: []string{"User Management"}}},
		{"titlebar/trail-deep", component.TitleBar{Title: "Remove an SSH key for alice", Parents: []string{"User Management", "Manage User"}}},
		{"titlebar/trail-long", component.TitleBar{Title: "Allow 2230/tcp from 192.0.2.0/24", Parents: []string{"Firewall application profiles", "Manage application profiles"}}},
		{"titlebar/empty", component.TitleBar{}},
		{"hostblock/normal", figmaHost},
		{"hostblock/unknown-address", component.HostBlock{Hostname: "BOS-SAN-L01", Uptime: uptime}},
		{"hostblock/unknown-uptime", component.HostBlock{Hostname: "BOS-SAN-L01", Address: "192.0.2.10"}},
		{"hostblock/no-hostname", component.HostBlock{Address: "192.0.2.10", Uptime: uptime}},
		{"hostblock/long-hostname", component.HostBlock{Hostname: longHost, Address: "192.0.2.10", Uptime: uptime}},
		{"hostblock/reboot-pending", component.HostBlock{Hostname: "BOS-SAN-L01", Address: "192.0.2.10", Uptime: uptime, Reboot: component.RebootPending}},
		{"hostblock/reboot-unknown", component.HostBlock{Hostname: "BOS-SAN-L01", Address: "192.0.2.10", Uptime: uptime, Reboot: component.RebootUnknown}},
		{"hostblock/all-unknown", component.HostBlock{}},
		{"risksummary/moderate", FigmaRisk()},
		{"risksummary/low", component.RiskSummary{Band: component.RiskLow, Level: "Low", Gloss: "strong security"}},
		{"risksummary/high", component.RiskSummary{Band: component.RiskHigh, Level: "Critical", Gloss: "no security"}},
		{"risksummary/unknown", component.RiskSummary{}},
		{"statusrow/configured", SSHPort()},
		{"statusrow/not-configured", component.StatusRow{Label: "AppArmor", State: component.StatusNotConfigured}},
		{"statusrow/partial", component.StatusRow{Label: "Auto Updates", State: component.StatusPartial, Detail: "crond not enabled"}},
		{"statusrow/not-applicable", component.StatusRow{Label: "AppArmor", State: component.StatusNotApplicable, Detail: "container"}},
		{"statusrow/unknown", component.StatusRow{Label: "SSH Port", Detail: "sshd -T failed"}},
		{"statusrow/word", component.StatusRow{Label: "SSH Port", State: component.StatusConfigured, Word: "2230/tcp", Detail: "non-default"}},
		{"statusrow/long-label", component.StatusRow{Label: "Unattended Upgrades Mail Reports", State: component.StatusConfigured, Detail: "root"}},
		{"statusrow/long-detail", component.StatusRow{Label: "SSH Auth", State: component.StatusNotConfigured, Detail: "password auth also allowed for alice and bob"}},
		{"statusrow/line-warn", component.StatusRow{Label: "User", State: component.StatusConfigured, Detail: "non-root, sudo",
			Lines: []component.StatusLine{noPassword}}},
		{"statusrow/line-muted", component.StatusRow{Label: "AppArmor", Detail: "can't read", Lines: []component.StatusLine{countedOff}}},
		{"statusrow/two-lines", component.StatusRow{Label: "User", State: component.StatusConfigured, Detail: "non-root, sudo",
			Lines: []component.StatusLine{{Text: "Second root account", Warn: true}, {Text: "Maybe no password to become root"}}}},
		{"statusrow/long-line", component.StatusRow{Label: "Auto Updates", State: component.StatusNotConfigured, Detail: "no unattended-upgrades",
			Lines: []component.StatusLine{{Text: longLine, Warn: true}}}},
		{"statuslist/normal", FigmaStatus()},
		{"statuslist/lines", linedStatus(noPassword, countedOff)},
		{"statuslist/empty", component.StatusList{}},
		{"divider/normal", component.Divider{}},
		{"modeindicator/live", component.ModeIndicator{}},
		{"modeindicator/dry-run", component.ModeIndicator{DryRun: true}},
		{"menuheading/normal", FigmaHeading()},
		{"menuheading/no-mode", component.MenuHeading{Title: "Select an option"}},
		{"menuheading/long", component.MenuHeading{Title: "Select an option for web-01.example.net", Mode: &component.ModeIndicator{DryRun: true}}},
		{"menuheading/empty", component.MenuHeading{}},
		{"menu/main", FigmaMenu()},
		{"menu/sub", component.Menu{Items: []component.MenuItem{
			{Key: "1", Label: "Manage User", Description: "Configure sudo, SSH keys"},
			{Key: "2", Label: "Create User", Description: "Configure a new user"},
		}, Exit: component.MenuItem{Key: "0", Label: "Return"}}},
		{"menu/risk", withItem(SSHMenu(), 0, component.MenuItem{Key: "1", Label: "Root SSH Login", Description: "Toggle root SSH access", Risk: true})},
		{"menu/disabled", withItem(SSHMenu(), 0, component.MenuItem{Key: "1", Label: "Root SSH Login", Description: "Toggle root SSH access", Aside: "Proxmox", Disabled: true})},
		{"menu/long", component.Menu{Items: []component.MenuItem{
			{Key: "1", Label: "Unattended Upgrades Mail Reports", Description: "Send the daily report to alice"},
			{Key: "2", Label: "Firewall", Description: "Allow port 2230/tcp from 192.0.2.0/24 and deny the rest"},
		}, Exit: component.MenuItem{Key: "0", Label: "Return"}}},
		{"menu/compact", CompactMenu()},
		{"menu/empty", component.Menu{}},
		{"promptline/normal", FigmaPrompt()},
		{"promptline/no-hint", component.PromptLine{Label: "Username"}},
		{"promptline/empty", component.PromptLine{}},
		{"badge/normal", component.Badge{Text: "george"}},
		{"badge/long", component.Badge{Text: longHost}},
		{"badge/empty", component.Badge{}},
		{"subjectline/normal", component.SubjectLine{Label: "Managing", Subject: "george"}},
		{"subjectline/long-label", component.SubjectLine{Label: "Managing sudo method for", Subject: "george"}},
		{"subjectline/long", component.SubjectLine{Label: "Managing sudo method for", Subject: "alice-backup-replication-service"}},
		{"subjectline/no-label", component.SubjectLine{Subject: "george"}},
		{"subjectline/empty", component.SubjectLine{Label: "Managing"}},
		{"sectionheader/normal", component.SectionHeader{Title: "User Configuration"}},
		{"sectionheader/long", component.SectionHeader{Title: "Backup configuration for every host in the web-01.example.net cluster"}},
		{"sectionheader/empty", component.SectionHeader{}},
		{"breadcrumb/normal", component.Breadcrumb{Items: []string{"User Management", "Manage User"}}},
		{"breadcrumb/deep", component.Breadcrumb{Items: []string{"User Management", "Manage User", "SSH Keys"}}},
		{"breadcrumb/long", component.Breadcrumb{Items: []string{"Firewall application profiles", "Manage application profiles", "Allow 2230/tcp from 192.0.2.0/24"}}},
		{"breadcrumb/empty", component.Breadcrumb{}},
		{"keyvalue/plain", component.KeyValue{Key: "SSH Port", Value: "2230"}},
		{"keyvalue/ok", component.KeyValue{Key: "Sudo Method", Value: "Password required", State: component.ValueOK}},
		{"keyvalue/warn", component.KeyValue{Key: "SSH Keys", Value: "None", State: component.ValueWarn}},
		{"keyvalue/bad", component.KeyValue{Key: "Root SSH Login", Value: "Enabled", State: component.ValueBad}},
		{"keyvalue/unknown", component.KeyValue{Key: "Allowed Users", State: component.ValueUnknown}},
		{"keyvalue/not-applicable", component.KeyValue{Key: "UFW", State: component.ValueNotApplicable}},
		{"keyvalue/note", component.KeyValue{Key: "Backup Path", Value: "/var/backups/hardn", State: component.ValueOK, Note: "Writable"}},
		{"keyvalue/long-key", component.KeyValue{Key: "Unattended Upgrades Mail Reports", Value: "alice"}},
		{"keyvalue/long-value", component.KeyValue{Key: "Allowed Users", Value: "alice bob carol dave erin frank grace heidi ivan judy mallory"}},
		{"keyvalue/item", component.KeyValue{Key: "If SSH fails, a fallback access method is available"}},
		{"keyvalue/item-warn", component.KeyValue{Key: "dhcpcd may rewrite /etc/resolv.conf on a lease", State: component.ValueWarn}},
		{"keyvalue/item-none", component.KeyValue{Key: "No nameservers configured", State: component.ValueNone}},
		{"keyvalue/empty", component.KeyValue{}},
		{"bulletlist/user", FigmaUser()},
		{"bulletlist/ssh", figmaSSH},
		{"bulletlist/checklist", FigmaChecklist()},
		{"bulletlist/long", component.BulletList{Items: []component.KeyValue{
			{Key: "Backup State", Value: "Enabled", State: component.ValueOK},
			{Key: "Unattended Upgrades Mail Reports", Value: "alice"},
		}}},
		{"bulletlist/empty", component.BulletList{}},
		{"message/ok", component.Message{Kind: component.MessageOK, Text: "DNS configured successfully"}},
		{"message/warn", component.Message{Kind: component.MessageWarn, Text: "No nameservers configured"}},
		{"message/bad", component.Message{Kind: component.MessageBad, Text: "Failed to configure sudo: exit status 1"}},
		{"message/info", component.Message{Text: "No automatic backups will be created"}},
		{"message/cancelled", component.Message{Kind: component.MessageCancelled, Text: "No changes were made."}},
		{"message/long", component.Message{Kind: component.MessageBad, Text: "Failed to restart ssh.service: Job for ssh.service failed because the control process exited with error code; port 2230 is still in use"}},
		{"message/empty", component.Message{Kind: component.MessageOK}},
		// A run of messages (P-42 R5): hardn confirm's clean-up report, what
		// was done, what is left and how to finish it; and pkg/output's
		// further errors, each with its fix.
		{"messages/clean-up", component.Messages{
			{Kind: component.MessageOK, Text: "Port 22 is dropped from /etc/ssh/sshd_config.d/00-hardn.conf, and sshd, reloaded, no longer listens on it."},
			{Kind: component.MessageOK, Text: "ufw's rule for 22/tcp, which hardn added, is deleted."},
			{Kind: component.MessageWarn, Text: "Port 2222 stays open: this session is not connected to a port the clean-up keeps."},
			{Text: "Run `sudo hardn confirm` again from a new SSH connection to port 2230, made from another machine."},
		}},
		{"messages/errors", component.Messages{
			{Kind: component.MessageBad, Text: "Set the nameservers: dns: /etc/resolv.conf is a link (DNS_MANAGED)"},
			{Text: "To go on: set the nameservers in NetworkManager."},
			{Kind: component.MessageBad, Text: "Allow 2230/tcp in UFW: ufw is not installed (UFW_MISSING)"},
			{Kind: component.MessageBad, Text: "Reload ssh.service: not run (NOT_RUN)"},
		}},
		{"messages/empty", component.Messages{{Kind: component.MessageWarn}, {Text: " "}}},
		// A form field's inline results (P-42, the maintainer, 2026-10-22):
		// origin/main's Add nameserver words (pkg/menu dns_menu.go), its
		// kinds kept, and an illustrative info line, as the maintainer's
		// mocks show them.
		{"inlineresult/bad", component.InlineResult{Kind: component.MessageBad, Text: "Invalid IP address format"}},
		{"inlineresult/warn", component.InlineResult{Kind: component.MessageWarn, Text: "Nameserver 192.0.2.54 is already configured"}},
		{"inlineresult/ok", component.InlineResult{Kind: component.MessageOK, Text: "Nameserver 192.0.2.53 added to configuration"}},
		{"inlineresult/info", component.InlineResult{Text: "An IPv4 or IPv6 address, such as 192.0.2.53"}},
		{"inlineresult/long", component.InlineResult{Kind: component.MessageBad, Text: "Username must start with a lowercase letter and contain only lowercase letters, numbers, hyphens, and underscores"}},
		{"inlineresult/empty", component.InlineResult{Kind: component.MessageBad}},
		{"callout/warn", FigmaWarning()},
		{"callout/ok", component.Callout{Kind: component.MessageOK, Title: "Operation Successful"}},
		{"callout/cancelled", component.Callout{Kind: component.MessageCancelled, Title: "Operation Cancelled"}},
		{"callout/bad", component.Callout{Kind: component.MessageBad, Body: []string{"sshd -t rejected the new configuration, so nothing was changed."}}},
		{"callout/info", component.Callout{Body: []string{"Dry-run mode: the plan was not applied."}}},
		{"callout/no-title", component.Callout{Kind: component.MessageWarn}},
		{"callout/long", component.Callout{Kind: component.MessageWarn, Title: "Changing the SSH port of web-01.example.net from 22 to 2230", Body: []string{
			"A user password is required for sudo usage.",
			"Keep this session open until a new connection to port 2230 from 192.0.2.0/24 succeeds.",
		}}},
		{"answeredfield/text", component.AnsweredField{Number: 1, Label: "Username", Answer: "george"}},
		{"answeredfield/yes-no", component.AnsweredField{Number: 2, Label: "Grant sudo privilege?", Accepts: yesNo, Answer: "y"}},
		{"answeredfield/confirm", component.AnsweredField{Label: "Disable SSH login for 'root'?", Accepts: yesNo, Answer: "y"}},
		{"answeredfield/unanswered", component.AnsweredField{Number: 3, Label: "Require sudo password?", Accepts: yesDefault}}, // [Y/n]: P-11's proposal, not a decision
		{"answeredfield/unnumbered", component.AnsweredField{Label: "Username", Answer: "george"}},
		{"answeredfield/long-answer", component.AnsweredField{Number: 4, Label: "Paste SSH public key", Answer: SSHKey}},
		{"answeredfield/empty", component.AnsweredField{Answer: "george"}},
		// The field heads' descriptions are Figma's "Configure sudo, SSH keys"
		// (01-USER; origin/main's pkg/menu user_options.go:84 has "SSH Keys")
		// and origin/main's "Specify username to create" (:99), and the long
		// one origin/main's username rule (:39).
		{"fieldhead/description", component.FieldHead{Number: 1, Title: "Username", Description: "Specify username to create"}},
		{"fieldhead/options", component.FieldHead{Number: 2, Title: "Select a user", Options: Users()}},
		{"fieldhead/both", component.FieldHead{Number: 2, Title: "Select a user", Description: "Configure sudo, SSH keys", Options: Users()}},
		{"fieldhead/unnumbered", component.FieldHead{Title: "Select a user", Options: Users()}},
		{"fieldhead/title", component.FieldHead{Number: 4, Title: "Paste SSH public key"}},
		{"fieldhead/long", component.FieldHead{
			Number: 1, Title: "Select the user whose sudo method to change on web-01.example.net",
			Description: "Username must start with a lowercase letter and contain only lowercase letters, numbers, hyphens, and underscores",
			Options:     []component.MenuItem{{Key: "1", Label: "backup-replication-service", Description: "Sudo without password"}},
		}},
		{"fieldhead/empty", component.FieldHead{Description: "Specify username to create", Options: Users()}},
		{"planview/normal", component.PlanView{Changes: []component.PlanChange{DNSChange(), {Title: "Install apk-cron", Target: "apk-cron", Before: "not installed", After: "installed", Commands: []string{"apk add apk-cron"}}}}},
		{"planview/lockout-risk", component.PlanView{Changes: []component.PlanChange{UFWChange(), SSHChange()}}},
		{"planview/dry-run", component.PlanView{Changes: []component.PlanChange{DNSChange()}, DryRun: true}},
		{"planview/new-file", component.PlanView{Changes: []component.PlanChange{{
			Title: "Require a password for sudo", Target: SudoersFile,
			Diff:     diff.Unified("/dev/null", SudoersFile, "", "# Managed by hardn\nalice ALL=(ALL:ALL) ALL\n"),
			Commands: []string{"visudo -cf " + SudoersFile},
		}}}},
		{"planview/no-newline", component.PlanView{Changes: []component.PlanChange{{
			Title: "Set the domain", Target: "/etc/hostname",
			Diff: diff.Unified("/etc/hostname", "/etc/hostname", "web-01", "web-01.example.net\n"),
		}}}},
		{"planview/long", component.PlanView{Changes: []component.PlanChange{{
			Title:  "Allow SSH logins for alice, bob and the backup replication service on web-01.example.net",
			Target: DropIn, LockoutRisk: true,
			Diff: diff.Unified(DropIn, DropIn, "AllowUsers alice\n",
				"AllowUsers alice bob backup-replication-service monitoring-agent deploy-bot\n\tMatch Address 192.0.2.0/24\n"),
			Commands: []string{"systemd-run --on-active=180s --unit=hardn-revert-20261016 hardn rollback 20261016"},
		}}}},
		{"planview/unknown", component.PlanView{Changes: []component.PlanChange{{Target: "ssh.service", Commands: []string{"systemctl reload ssh"}}}}},
		{"planview/empty", component.PlanView{DryRun: true}},
		{"stepprogress/apply", component.StepProgress{Steps: Steps(component.StepDone, component.StepDone, component.StepRunning, component.StepPending)}},
		{"stepprogress/animated", Animated{component.StepProgress{Steps: Steps(component.StepDone, component.StepDone, component.StepRunning, component.StepPending), Frame: 3}}},
		{"stepprogress/failed", component.StepProgress{Steps: append(Steps(component.StepRolledBack, component.StepRolledBack),
			component.Step{Title: stepTitles[2], State: component.StepFailed, Detail: "exit status 1"},
			component.Step{Title: stepTitles[3], State: component.StepSkipped, Detail: "not run after the failure"})}},
		{"stepprogress/skipped", component.StepProgress{Steps: []component.Step{
			{Title: "Allow 2230/tcp in UFW", State: component.StepSkipped, Detail: "already allowed"},
			{Title: "Install apk-cron", State: component.StepDone},
		}}},
		{"stepprogress/long", component.StepProgress{Steps: []component.Step{
			{Title: "Set Port 2230 in /etc/ssh/sshd_config.d/00-hardn.conf", State: component.StepFailed, Detail: "sshd -t: Bad configuration option: Portt"},
		}}},
		// The engine's 2230 change on Debian 13 at its reload's failure,
		// whose widest title puts the status column at 36 (P-42 R2).
		{"stepprogress/aligned", component.StepProgress{Steps: PortSteps()}},
		{"stepprogress/empty", component.StepProgress{Steps: []component.Step{{State: component.StepDone}}}},
		{"errorreport/normal", SSHFailure()},
		{"errorreport/aligned", component.ErrorReport{
			Failed:     "Reload sshd (ssh.service): exit status 1 (COMMAND_FAILED)",
			RolledBack: []string{"Add SSH port 2230", "ufw: allow 2230/tcp"}, Steps: PortSteps(),
			Command: "systemctl reload ssh.service", Output: "Job for ssh.service failed because the control process exited with error code.",
			LogPath: "/var/log/hardn.log",
		}},
		{"errorreport/tail", component.ErrorReport{
			Failed: "Installing unattended-upgrades failed: exit status 100", Command: "apt-get install -y unattended-upgrades",
			Output: installLog(), LogPath: "/var/log/hardn.log",
		}},
		{"errorreport/unknown", component.ErrorReport{
			Failed: "Reloading ssh.service failed: exit status 1", RolledBack: []string{"Set Port 2230", ""}, LogPath: "/var/log/hardn.log",
		}},
		{"errorreport/nothing-rolled-back", component.ErrorReport{Failed: "Writing /etc/ssh/sshd_config.d/00-hardn.conf failed: read-only file system", LogPath: "/var/log/hardn.log"}},
		{"errorreport/long", component.ErrorReport{
			Failed:     "Rolling back Set Port 2230 failed: sshd -t rejected the restored /etc/ssh/sshd_config.d/00-hardn.conf, so ssh.service still runs the new configuration",
			RolledBack: []string{"Allow SSH logins for alice, bob and the backup replication service"},
			Command:    "sshd -t -f /etc/ssh/sshd_config.d/00-hardn.conf.hardn-backup-20261016",
			Output:     "/etc/ssh/sshd_config.d/00-hardn.conf.hardn-backup-20261016: line 3: Bad configuration option: AllowUsersForReplication",
			LogPath:    "/var/log/hardn.log",
			Next:       "Restore /etc/ssh/sshd_config.d/00-hardn.conf from /var/backups/hardn before you close this session.",
		}},
		{"errorreport/empty", component.ErrorReport{}},
		{"confirmcountdown/waiting", component.ConfirmCountdown{Remaining: 165}},
		{"confirmcountdown/port", component.ConfirmCountdown{Remaining: 180, Port: 2230}},
		{"confirmcountdown/urgent", component.ConfirmCountdown{Remaining: 25, Port: 2230}},
		{"confirmcountdown/zero", component.ConfirmCountdown{Remaining: -3}},
		{"confirmcountdown/command", component.ConfirmCountdown{Remaining: 3600, Command: "sudo /usr/local/bin/hardn confirm --config /etc/hardn/web-01.example.net.yml"}},
	}
}

// installLog is the output of a failed apt-get install: 14 lines, one of
// them a progress line overwritten with carriage returns.
func installLog() string {
	var b strings.Builder
	for i := 1; i <= 12; i++ {
		fmt.Fprintf(&b, "Get:%d http://deb.example.net/debian trixie/main amd64 package-%d [1024 B]\n", i, i)
	}
	b.WriteString("Progress: 10%\rProgress: 60%\rProgress: 100%\r\n")
	b.WriteString("E: Sub-process /usr/bin/dpkg returned an error code (1)\n")
	return b.String()
}

// withItem returns m with item i replaced by it.
func withItem(m component.Menu, i int, it component.MenuItem) component.Menu {
	m.Items = slices.Clone(m.Items)
	m.Items[i] = it
	return m
}
