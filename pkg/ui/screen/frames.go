package screen

import (
	"fmt"

	"github.com/abbott/hardn/pkg/diff"
	"github.com/abbott/hardn/pkg/ui/component"
	"github.com/abbott/hardn/pkg/ui/flow"
	"github.com/abbott/hardn/pkg/ui/form"
)

// The fixtures of Figma's other frames, and of origin/main's DNS screen,
// for the gallery's mocks (Mocks). Each sub-screen's breadcrumb is
// "hardn" and the frame's title bar, under its parent's (DEV-053,
// DEV-054). Where a frame's mock data is a real-looking name, the
// fixture has george or alice; its other words, Figma's drift included,
// are the frame's, as UserFixture's are.

// trail returns the breadcrumb "hardn", then items.
func trail(items ...string) component.Breadcrumb {
	return component.Breadcrumb{Items: append([]string{"hardn"}, items...)}
}

// returnItem is the Return row of Figma's sub-menus.
func returnItem() component.MenuItem { return component.MenuItem{Key: "0", Label: "Return"} }

// sshSettings are Figma's "SSH Configuration" rows (02-SSH_LOGIN,
// SSH_LOGIN-TOGGLE_CONFIRMATION), with alice as the allowed user.
func sshSettings() []component.KeyValue {
	return []component.KeyValue{
		{Key: "Root SSH Login", Value: "Enabled", State: component.ValueBad},
		{Key: "SSH Port", Value: "2230"},
		{Key: "Allowed Users", Value: "alice"},
	}
}

// sshPortRow is the SSH Port status row of Figma's sub-menus, the port
// shown as the state's word.
func sshPortRow() component.StatusRow {
	return component.StatusRow{Label: "SSH Port", State: component.StatusConfigured, Word: "2230/tcp", Detail: "non-default"}
}

// sshLoginFixture returns the SSH Login sub-menu as Figma's 02-SSH_LOGIN
// fills it. Its status row says root login is disallowed and its section
// that it is enabled: Figma's drift, drawn as the frame has it.
func sshLoginFixture() SubMenu {
	return SubMenu{
		Header:     fixtureHeader(),
		Breadcrumb: trail("Manage Root SSH Login"),
		Status: component.StatusList{Rows: []component.StatusRow{
			{Label: "SSH Login", State: component.StatusConfigured, Detail: "root disallowed"},
			sshPortRow(),
		}},
		Sections: []Section{{
			Header: component.SectionHeader{Title: "SSH Configuration"},
			Rows:   component.BulletList{Items: sshSettings()},
		}},
		Heading: fixtureHeading,
		Menu: component.Menu{Items: []component.MenuItem{
			{Key: "1", Label: "Root SSH Login", Description: "Toggle root SSH access"},
			{Key: "2", Label: "View SSH Settings", Description: "Show SSH configuration"},
		}, Exit: returnItem()},
		Prompt: figmaPrompt,
	}
}

// backupFixture returns the Backup sub-menu as Figma's 05-BACKUP fills
// it. Its status rows are Figma's drift, copied from other screens ("UFW
// installed, deny policy" beside Backups, and the SSH port), drawn as the
// frame has them.
func backupFixture() SubMenu {
	return SubMenu{
		Header:     fixtureHeader(),
		Breadcrumb: trail("Manage Hardn Backups"),
		Status: component.StatusList{Rows: []component.StatusRow{
			{Label: "Backups", State: component.StatusConfigured, Word: "Enabled", Detail: "UFW installed, deny policy"},
			sshPortRow(),
		}},
		Sections: []Section{{
			Header: component.SectionHeader{Title: "Backup Configuration"},
			Rows: component.BulletList{Items: []component.KeyValue{
				{Key: "Backup State", Value: "Enabled", State: component.ValueOK},
				{Key: "Backup Path", Value: "/var/backups/hardn", State: component.ValueOK, Note: "Writable"},
			}},
		}},
		Heading: fixtureHeading,
		Menu: component.Menu{Items: []component.MenuItem{
			{Key: "1", Label: "Backup State", Description: "Toggle backups of modified files"},
			{Key: "2", Label: "Backup Path", Description: "Change backup path"},
			{Key: "3", Label: "Verify Directory", Description: "Test if path is writable"},
		}, Exit: returnItem()},
		Prompt: figmaPrompt,
	}
}

// selectUserFixture returns Figma's USER-MANAGE_USER-SELECT_USER: a menu
// of users, george and alice, under its own heading.
func selectUserFixture() SubMenu {
	return SubMenu{
		Header:     fixtureHeader(),
		Breadcrumb: trail("User Management", "Manage User"),
		Heading:    "Select a user",
		Menu: component.Menu{Items: []component.MenuItem{{Key: "1", Label: "george"}, {Key: "2", Label: "alice"}},
			Exit: returnItem()},
		Prompt: figmaPrompt,
	}
}

// manageUserFixture returns Figma's USER-MANAGE_USER-MANAGEMENT: a
// subject line over the user's menu.
func manageUserFixture() SubMenu {
	return SubMenu{
		Header:     fixtureHeader(),
		Breadcrumb: trail("User Management", "Manage User"),
		Subject:    component.SubjectLine{Label: "Managing", Subject: "george"},
		Heading:    fixtureHeading,
		Menu: component.Menu{Items: []component.MenuItem{
			{Key: "1", Label: "Sudo Method", Description: "Toggle password requirement"},
			{Key: "2", Label: "Manage SSH keys", Description: "Add or remove SSH keys"},
		}, Exit: returnItem()},
		Prompt: figmaPrompt,
	}
}

// sshKeysFixture returns Figma's USER-SSH_KEYS: a subject line over one
// item, for a user with no key to remove.
func sshKeysFixture() SubMenu {
	return SubMenu{
		Header:     fixtureHeader(),
		Breadcrumb: trail("User Management", "Manage User"),
		Subject:    component.SubjectLine{Label: "Managing SSH keys for", Subject: "george"},
		Heading:    fixtureHeading,
		Menu:       component.Menu{Items: []component.MenuItem{{Key: "1", Label: "Add SSH key"}}, Exit: returnItem()},
		Prompt:     figmaPrompt,
	}
}

// dnsFixture returns origin/main's DNS screen (pkg/menu dns_menu.go) on
// the sub-menu layout, which no Figma frame draws: its title, "DNS
// Configuration", is the breadcrumb's current item, and its two headings
// are sections, without their colons, of key-value rows. The nameserver
// in use is 192.0.2.1, and the two configured are not applied yet. The
// words are origin/main's, the prompt "Enter selection" included.
func dnsFixture() SubMenu {
	return SubMenu{
		Header:     fixtureHeader(),
		Breadcrumb: trail("DNS Configuration"),
		Sections: []Section{{
			Header: component.SectionHeader{Title: "Current DNS Configuration"},
			Rows: component.BulletList{Items: []component.KeyValue{
				{Key: "DNS Implementation", Value: "direct (/etc/resolv.conf)"},
				{Key: "Nameservers", Value: "192.0.2.1"},
			}},
		}, {
			Header: component.SectionHeader{Title: "Configured Nameservers"},
			Rows: component.BulletList{Items: []component.KeyValue{
				{Key: "Nameserver 1", Value: "192.0.2.53"},
				{Key: "Nameserver 2", Value: "192.0.2.54"},
			}},
		}},
		Heading: fixtureHeading,
		Menu: component.Menu{Items: []component.MenuItem{
			{Key: "1", Label: "Configure DNS", Description: "Apply nameserver settings from configuration"},
			{Key: "2", Label: "Add nameserver", Description: "Add a new DNS server to configuration"},
			{Key: "3", Label: "Remove nameserver", Description: "Remove a DNS server from configuration"},
			{Key: "4", Label: "Use Cloudflare DNS", Description: "Set nameservers to 1.1.1.1, 1.0.0.1"},
			{Key: "5", Label: "Use Google DNS", Description: "Set nameservers to 8.8.8.8, 8.8.4.4"},
			{Key: "6", Label: "Use Quad9 DNS", Description: "Set nameservers to 9.9.9.9, 149.112.112.112"},
		}, Exit: returnItem()},
		Prompt: mainPrompt,
	}
}

// The root-login fixture's host and file: Figma's mock host, and hardn's
// sshd drop-in (brief Phase 2.2).
const (
	hostname = "bos-san-l01"
	dropIn   = "/etc/ssh/sshd_config.d/00-hardn.conf"
)

// rootLoginFixture returns Figma's SSH_LOGIN-TOGGLE_CONFIRMATION as a
// workflow step and its flow: disabling root SSH login, under Figma's
// "Manage Root SSH Login" title. The review is Figma's warning, its
// checklist and its "SSH Configuration" rows; the plan sets
// PermitRootLogin no in hardn's drop-in, a lockout risk, so the flow asks
// Figma's question above a typed confirmation of the hostname, as brief
// 1C has it, where Figma asks (y/n): P-10 is pending.
func rootLoginFixture() (WorkflowStep, flow.Flow) {
	plan := flow.Plan{Risk: flow.RiskLockout, View: component.PlanView{Changes: []component.PlanChange{{
		Title: "Set PermitRootLogin no", Target: dropIn, LockoutRisk: true,
		Diff: diff.Unified(dropIn, dropIn,
			"# Managed by hardn\nPort 2230\nPermitRootLogin yes\n",
			"# Managed by hardn\nPort 2230\nPermitRootLogin no\n"),
		Commands: []string{"sshd -t", "systemctl reload ssh"},
	}}}}
	f := flow.Flow{
		Review: func() flow.Review {
			return flow.Review{
				Notice: &flow.Notice{
					Callout: component.Callout{Kind: component.MessageWarn, Body: []string{"Before disabling root SSH login, ensure:"}},
					Items: []string{
						"Non-root user has sudo privileges",
						"Non-root user SSH access has been tested",
						"If SSH fails, a fallback access method is available",
					},
				},
				Title: "SSH Configuration",
				Items: sshSettings(),
			}
		},
		Engine: flow.Fixture{Planned: plan},
		Confirm: func(flow.Plan) flow.Confirm {
			return flow.Confirm{Question: "Disable SSH login for 'root'?", Label: "Type the hostname", Phrase: hostname}
		},
	}
	return WorkflowStep{Header: fixtureHeader(), Breadcrumb: trail("Manage Root SSH Login")}, f
}

// createUserFixture returns Figma's USER-CREATE_USER as a workflow step
// and its flow, under Figma's "Create User" title: the form asks Figma's
// four questions, and origin/main's "Paste SSH public key" after a yes to
// "Add SSH key?"; the review sums the answers up as Figma does; the plan
// is fixed, for Figma's answers (george, with sudo and no password); and
// Figma's question, "Create user 'george'?", is asked after the plan,
// unnumbered, not as the form's fifth field (DEV-048, P-10).
func createUserFixture() (WorkflowStep, flow.Flow) {
	var (
		name, key              string
		sudo, password, addKey bool
	)
	plan := flow.Plan{View: component.PlanView{Changes: []component.PlanChange{
		{Title: "Create user george", Commands: []string{"useradd --create-home --shell /bin/bash george"}},
		{Title: "Add george to the sudo group", Commands: []string{"usermod -aG sudo george"}},
		{
			Title: "Let george use sudo without a password", Target: sudoersFile,
			Diff:     diff.Unified(sudoersFile, sudoersFile, "", "george ALL=(ALL:ALL) NOPASSWD: ALL\n"),
			Commands: []string{"visudo -cf " + sudoersFile},
		},
	}}}
	f := flow.Flow{
		Form: form.Form{Fields: []form.Field{
			&form.Text{Title: "Username", Value: &name},
			&form.YesNo{Title: "Grant sudo privilege?", Value: &sudo},
			&form.YesNo{Title: "Require sudo password?", Value: &password, Skip: func() bool { return !sudo }},
			&form.YesNo{Title: "Add SSH key?", Value: &addKey},
			&form.Text{Title: "Paste SSH public key", Value: &key, Skip: func() bool { return !addKey }},
		}},
		Review: func() flow.Review {
			sudoRow := component.KeyValue{Key: "Sudo", Value: "Enabled, No password", State: component.ValueBad}
			switch {
			case !sudo:
				sudoRow = component.KeyValue{Key: "Sudo", Value: "Disabled"}
			case password:
				sudoRow = component.KeyValue{Key: "Sudo", Value: "Enabled", State: component.ValueOK}
			}
			keyRow := component.KeyValue{Key: "SSH Keys", Value: "None", State: component.ValueWarn}
			if addKey {
				keyRow = component.KeyValue{Key: "SSH Keys", Value: "1 key"}
			}
			return flow.Review{Title: "User Configuration", Items: []component.KeyValue{sudoRow, keyRow}}
		},
		Engine:  flow.Fixture{Planned: plan},
		Confirm: func(flow.Plan) flow.Confirm { return flow.Confirm{Question: fmt.Sprintf("Create user '%s'?", name)} },
	}
	return WorkflowStep{Header: fixtureHeader(), Breadcrumb: trail("User Management", "Create User")}, f
}
