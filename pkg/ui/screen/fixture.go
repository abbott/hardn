package screen

import (
	"errors"

	"github.com/abbott/hardn/pkg/diff"
	"github.com/abbott/hardn/pkg/ui/component"
	"github.com/abbott/hardn/pkg/ui/flow"
	"github.com/abbott/hardn/pkg/ui/keys"
)

// The fixtures' shared words: the header's, the menu heading and the two
// menu prompts, whose choice P-29 (a) puts to the maintainer.
const (
	fixtureVersion = "v0.4.0"
	fixtureURL     = "https://github.com/abbott/hardn"
	fixtureHeading = "Select an option"
	// mainPrompt is origin/main's menu prompt (pkg/style style.go:668),
	// the baseline for the main menu's words.
	mainPrompt = "Enter selection"
	// figmaPrompt is the prompt Figma's menu frames end with, kept where
	// a fixture draws a sub-menu frame as Figma fills it.
	figmaPrompt = "Enter your choice"
)

// fixtureHeader is the header every fixture opens with.
func fixtureHeader() component.Header {
	return component.Header{Version: fixtureVersion, URL: fixtureURL}
}

// MainMenuFixture returns the main menu as Figma's 00-TUI-MAIN fills it,
// with synthetic data: Figma's mock host BOS-SAN-L01 at 192.0.2.10, its
// SSH port 2230, Figma's seven status rows and its Moderate risk, and the
// live mode. The menu's items are origin/main's (pkg/menu main_menu.go),
// the baseline: they differ from the PNG only in item 2, "Toggle SSH root
// access" where Figma has "Toggle root SSH access" (P-07 (a), P-12). The
// Exit item says "q to quit" (DEV-030), and the prompt is origin/main's
// "Enter selection" where Figma's says "Enter your choice" (P-29 (a)).
func MainMenuFixture() MainMenu {
	configured := func(label, detail string) component.StatusRow {
		return component.StatusRow{Label: label, State: component.StatusConfigured, Detail: detail}
	}
	return MainMenu{
		Header:  fixtureHeader(),
		Context: component.ContextBar{Name: "Debian", Version: "12", Codename: "Bookworm"},
		Host:    component.HostBlock{Hostname: "BOS-SAN-L01", Address: "192.0.2.10", Uptime: "182 days, 2 hours, 26 minutes"},
		Risk:    component.RiskSummary{Band: component.RiskModerate, Level: "Moderate", Gloss: "medium security"},
		Status: component.StatusList{Rows: []component.StatusRow{
			configured("User", "non-root, sudo"),
			configured("Firewall", "deny policy"),
			configured("SSH Login", "root disallowed"),
			configured("SSH Auth", "key-only auth"),
			configured("SSH Port", "non-default (2230)"),
			{Label: "AppArmor", State: component.StatusNotConfigured},
			{Label: "Auto Updates", State: component.StatusNotConfigured},
		}},
		Heading: fixtureHeading,
		Menu: component.Menu{Items: []component.MenuItem{
			{Key: "1", Label: "User Management", Description: "Create, Configure", Aside: "sudo, SSH keys"},
			{Key: "2", Label: "SSH Login", Description: "Toggle SSH root access"},
			{Key: "3", Label: "DNS", Description: "Configure Nameservers"},
			{Key: "4", Label: "Firewall", Description: "Configure UFW rules"},
			{Key: "5", Label: "Backup", Description: "Configure Hardn backup settings"},
			{Key: "6", Label: "Dry-Run", Description: "Simulate changes"},
			{Key: "7", Label: "Run All", Description: "Execute hardening operations"},
			{Key: "8", Label: "Environment", Description: "Configure environment variable"},
			{Key: "9", Label: "System Details", Description: "View system information"},
			{Key: "10", Label: "Logs", Description: "View log file"},
		}, Exit: component.MenuItem{Key: "0", Label: "Exit", Description: keys.Set{keys.Quit()}.Help()}},
		Prompt: mainPrompt,
	}
}

// UserFixture returns the User Management sub-menu as Figma's 01-USER
// fills it: the "Non-root" status row, the "User Configuration" section
// for george, whose sudo needs no password, and Figma's two items. The
// breadcrumb's current item is Figma's title bar, "User Management". Its
// words are the frame's, where origin/main's differ: "Manage a user" and
// "Create a user", "Configure sudo, SSH Keys", the section
// "Configuration", "Enabled no password" and the prompt "Enter
// selection" (P-29 (b)).
func UserFixture() SubMenu {
	return SubMenu{
		Header:     fixtureHeader(),
		Breadcrumb: component.Breadcrumb{Items: []string{"hardn", "User Management"}},
		Status: component.StatusList{Rows: []component.StatusRow{
			{Label: "Non-root", State: component.StatusConfigured, Detail: "UID ≥ 1000"},
		}},
		Sections: []Section{{
			Header: component.SectionHeader{Title: "User Configuration"},
			Badge:  component.Badge{Text: "george"},
			Rows: component.BulletList{Items: []component.KeyValue{
				{Key: "Sudo", Value: "Enabled,", State: component.ValueBad, Note: "No password"},
				{Key: "SSH Keys", Value: "7 keys"},
				{Key: "UID:GID", Value: "1000:1000"},
				{Key: "Directory", Value: "/home/george"},
			}},
		}},
		Heading: fixtureHeading,
		Menu: component.Menu{Items: []component.MenuItem{
			{Key: "1", Label: "Manage User", Description: "Configure sudo, SSH keys"},
			{Key: "2", Label: "Create User", Description: "Configure a new user"},
		}, Exit: component.MenuItem{Key: "0", Label: "Return"}},
		Prompt: figmaPrompt,
	}
}

// sudoersFile is the drop-in the sudo fixture changes, sudoersPrefix and
// the user's name. Figma draws no plan, so the plan is illustrative, named
// as the component and flow fixtures name theirs: origin/main writes
// /etc/sudoers.d/george, as "george ALL=(ALL) ALL" (pkg/adapter/secondary
// os_user_repository.go), and Phase 3's engine decides the file.
const (
	sudoersPrefix = "/etc/sudoers.d/hardn-"
	sudoersFile   = sudoersPrefix + "george"
)

// sudoPlan is what requiring george's password for sudo plans, two steps:
// the drop-in's rule, the file checked alone with visudo -cf, then the
// whole sudo configuration checked with visudo -c, so that the failure
// path has a step to roll back. Illustrative, as sudoersFile is.
func sudoPlan() flow.Plan {
	return flow.Plan{View: component.PlanView{Changes: []component.PlanChange{{
		Title:  "Require george's password for sudo",
		Target: sudoersFile,
		Diff: diff.Unified(sudoersFile, sudoersFile,
			"george ALL=(ALL:ALL) NOPASSWD: ALL\n", "george ALL=(ALL:ALL) ALL\n"),
		Commands: []string{"visudo -cf " + sudoersFile},
	}, {
		Title:    "Check the sudo configuration",
		Commands: []string{"visudo -c"},
	}}}}
}

// sudoFails fails sudoPlan's check, so that the drop-in's change is
// rolled back.
func sudoFails() flow.Fixture {
	return flow.Fixture{Planned: sudoPlan(), FailAt: 2, Detail: "exit status 1", Failure: flow.Failure{
		Err:     errors.New("Checking the sudo configuration failed: exit status 1"),
		Command: "visudo -c",
		LogPath: "/var/log/hardn.log",
		Next:    "Run sudo visudo -c to see which file it rejects.",
	}}
}

// ManageSudoFixture returns Figma's USER-MANAGE_SUDO-CONFIRMATION as a
// workflow step and the flow it runs: requiring a password for george's
// sudo. The screen names george under Figma's "Manage User" title; the
// flow asks nothing before its review, which warns as Figma does ("A user
// password is required for sudo usage.") and sums up the change as
// "Sudo Method: Password required"; it plans sudoPlan on a flow.Fixture,
// which touches nothing; and it asks Figma's question, "Require password
// for sudo?", as a y/N question, as Figma does (P-10 proposes a typed
// confirmation for sudo-method changes).
func ManageSudoFixture() (WorkflowStep, flow.Flow) {
	return manageSudo(flow.Fixture{Planned: sudoPlan()}, false)
}

// manageSudo is ManageSudoFixture on engine e, in dry-run mode when
// dryRun is set.
func manageSudo(e flow.Engine, dryRun bool) (WorkflowStep, flow.Flow) {
	step := WorkflowStep{
		Header:     fixtureHeader(),
		Breadcrumb: component.Breadcrumb{Items: []string{"hardn", "User Management", "Manage User"}},
		Subject:    component.SubjectLine{Label: "Managing sudo method for", Subject: "george"},
	}
	f := flow.Flow{
		Review: func() flow.Review {
			return flow.Review{
				Notice: &flow.Notice{Callout: component.Callout{
					Kind: component.MessageWarn,
					Body: []string{"A user password is required for sudo usage."},
				}},
				Title: "User Configuration",
				Items: []component.KeyValue{{Key: "Sudo Method", Value: "Password required", State: component.ValueOK}},
			}
		},
		Engine:  e,
		Confirm: func(flow.Plan) flow.Confirm { return flow.Confirm{Question: "Require password for sudo?"} },
		DryRun:  dryRun,
	}
	return step, f
}
