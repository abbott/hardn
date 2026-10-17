package screen

import (
	"context"
	"errors"
	"fmt"

	"github.com/abbott/hardn/pkg/diff"
	"github.com/abbott/hardn/pkg/ui/component"
	"github.com/abbott/hardn/pkg/ui/flow"
	"github.com/abbott/hardn/pkg/ui/form"
)

// The fixtures of Figma's other frames, and of origin/main's DNS screen,
// for the gallery's mocks (Mocks). Each sub-screen's title bar is the
// frame's, and its trail the sections above it, such as "User
// Management" above "Manage User", shown only when TitleTrail is set
// (P-08; DEV-053, DEV-054). Where a frame's mock data is a real-looking
// name, the fixture has george or alice; its other words, Figma's drift
// included, are the frame's, as UserFixture's are.

// trail returns the title bar of the screen at the end of items: the last
// item its title, the ones before it its parents, outermost first, such
// as trail("User Management", "Manage User"). A trail names the sections
// alone, never the product (P-08).
func trail(items ...string) component.TitleBar {
	n := len(items) - 1
	return component.TitleBar{Title: items[n], Parents: items[:n:n]}
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
		Title: trail("Manage Root SSH Login"),
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
		Title: trail("Manage Hardn Backups"),
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
		Title:   trail("User Management", "Manage User"),
		Heading: "Select a user",
		Menu: component.Menu{Items: []component.MenuItem{{Key: "1", Label: "george"}, {Key: "2", Label: "alice"}},
			Exit: returnItem()},
		Prompt: figmaPrompt,
	}
}

// manageUserFixture returns Figma's USER-MANAGE_USER-MANAGEMENT: a
// subject line over the user's menu.
func manageUserFixture() SubMenu {
	return SubMenu{
		Title:   trail("User Management", "Manage User"),
		Subject: component.SubjectLine{Label: "Managing", Subject: "george"},
		Heading: fixtureHeading,
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
		Title:   trail("User Management", "Manage User"),
		Subject: component.SubjectLine{Label: "Managing SSH keys for", Subject: "george"},
		Heading: fixtureHeading,
		Menu:    component.Menu{Items: []component.MenuItem{{Key: "1", Label: "Add SSH key"}}, Exit: returnItem()},
		Prompt:  figmaPrompt,
	}
}

// dnsFixture returns origin/main's DNS screen (pkg/menu dns_menu.go) on
// the sub-menu layout, which no Figma frame draws: its title, "DNS
// Configuration", is its title bar's, and its two headings are sections,
// without their colons, of key-value rows. The nameserver in use is
// 192.0.2.1, and the two configured are not applied yet. The words are
// origin/main's, the prompt "Enter selection" included.
func dnsFixture() SubMenu {
	return SubMenu{
		Title: trail("DNS Configuration"),
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

// rootLoginPlan is what disabling root SSH login plans, two steps, as
// brief Phase 2.3 orders them: PermitRootLogin no in hardn's drop-in,
// checked with sshd -t, a lockout risk; then the SSH service reloaded,
// not restarted, so that open sessions survive. The second step gives the
// failure path one to roll back. Illustrative, as sudoPlan is.
func rootLoginPlan() flow.Plan {
	return flow.Plan{Risk: flow.RiskLockout, View: component.PlanView{Changes: []component.PlanChange{{
		Title: "Set PermitRootLogin no", Target: dropIn, LockoutRisk: true,
		Diff: diff.Unified(dropIn, dropIn,
			"# Managed by hardn\nPort 2230\nPermitRootLogin yes\n",
			"# Managed by hardn\nPort 2230\nPermitRootLogin no\n"),
		Commands: []string{"sshd -t"},
	}, {
		Title:    "Reload the SSH service",
		Commands: []string{"systemctl reload ssh"},
	}}}}
}

// rootLoginFails fails rootLoginPlan's reload, so that the drop-in's
// change is rolled back.
func rootLoginFails() flow.Fixture {
	return flow.Fixture{Planned: rootLoginPlan(), FailAt: 2, Detail: "exit status 1", Failure: flow.Failure{
		Err:     errors.New("Reloading the SSH service failed: exit status 1"),
		Command: "systemctl reload ssh",
		LogPath: "/var/log/hardn.log",
		Next:    "Run systemctl status ssh to see why it failed.",
	}}
}

// rootLogin returns Figma's SSH_LOGIN-TOGGLE_CONFIRMATION as a workflow
// step and its flow on engine e, in dry-run mode when dryRun is set:
// disabling root SSH login, under Figma's "Manage Root SSH Login" title.
// The review is Figma's warning, its checklist and its "SSH
// Configuration" rows; the plan (rootLoginPlan) is a lockout risk, so the
// flow asks Figma's question above a typed confirmation of the hostname,
// as brief 1C has it, where Figma asks (y/n): P-10 is pending.
func rootLogin(e flow.Engine, dryRun bool) (WorkflowStep, flow.Flow) {
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
		Engine:  e,
		Confirm: typedHostname("Disable SSH login for 'root'?"),
		DryRun:  dryRun,
	}
	return WorkflowStep{Title: trail("Manage Root SSH Login")}, f
}

// typedHostname returns a flow's Confirm that asks question, and above a
// typed confirmation, which the flow asks for a plan with lockout risk,
// asks for the hostname: nav's example label, under the question.
func typedHostname(question string) func(flow.Plan) flow.Confirm {
	return func(flow.Plan) flow.Confirm {
		return flow.Confirm{Question: question, Label: "Type the hostname", Phrase: hostname}
	}
}

// newUser is the create-user form's answers.
type newUser struct {
	name, key              string
	sudo, password, addKey bool
}

// plan is what creating u plans: the user; with sudo, the sudo group;
// with sudo and no password for it, a drop-in that lets the user use sudo
// with none, checked with visudo -cf; with a key, the user's
// authorized_keys. For Figma's answers (george, with sudo and no
// password, no key) that is three steps. Illustrative, as sudoPlan is.
func (u *newUser) plan() flow.Plan {
	changes := []component.PlanChange{{
		Title: "Create user " + u.name, Commands: []string{"useradd --create-home --shell /bin/bash " + u.name},
	}}
	if u.sudo {
		changes = append(changes, component.PlanChange{
			Title: "Add " + u.name + " to the sudo group", Commands: []string{"usermod -aG sudo " + u.name},
		})
	}
	if u.sudo && !u.password {
		file := sudoersPrefix + u.name
		changes = append(changes, component.PlanChange{
			Title: "Let " + u.name + " use sudo without a password", Target: file,
			Diff:     diff.Unified(file, file, "", u.name+" ALL=(ALL:ALL) NOPASSWD: ALL\n"),
			Commands: []string{"visudo -cf " + file},
		})
	}
	if u.addKey {
		file := "/home/" + u.name + "/.ssh/authorized_keys"
		changes = append(changes, component.PlanChange{
			Title: "Add an SSH key for " + u.name, Target: file, Diff: diff.Unified(file, file, "", u.key+"\n"),
		})
	}
	return flow.Plan{View: component.PlanView{Changes: changes}}
}

// fails fails u's plan at its second step, when it has one: the sudo
// group, for Figma's answers, as on a host with no sudo group, so that
// the user is rolled back and the drop-in skipped.
func (u *newUser) fails() flow.Fixture {
	return flow.Fixture{Planned: u.plan(), FailAt: 2, Detail: "exit status 6", Failure: flow.Failure{
		Err:     fmt.Errorf("Adding %s to the sudo group failed: exit status 6", u.name),
		Command: "usermod -aG sudo " + u.name,
		Output:  "usermod: group 'sudo' does not exist\n",
		LogPath: "/var/log/hardn.log",
		Next:    "Check that the sudo group exists, then try again.",
	}}
}

// answered is a flow.Engine whose plan follows a form's answers: the
// fixture it returns is made when the flow asks for the plan, after the
// form, and applied as flow.Fixture applies it.
type answered func() flow.Fixture

func (a answered) Plan(ctx context.Context) (flow.Plan, error) { return a().Plan(ctx) }

func (a answered) Apply(ctx context.Context, report flow.Report) error { return a().Apply(ctx, report) }

// createUser returns Figma's USER-CREATE_USER as a workflow step and its
// flow, under Figma's "Create User" title, in dry-run mode when dryRun is
// set: the form asks Figma's four questions, and origin/main's "Paste SSH
// public key" after a yes to "Add SSH key?"; the review sums the answers
// up as Figma does; the plan follows the answers (newUser.plan), failing
// at its second step when failing is set; and Figma's question, "Create
// user 'george'?", is asked after the plan, unnumbered, not as the form's
// fifth field (DEV-048, P-10).
func createUser(failing, dryRun bool) (WorkflowStep, flow.Flow) {
	u := new(newUser)
	engine := answered(func() flow.Fixture {
		if failing {
			return u.fails()
		}
		return flow.Fixture{Planned: u.plan()}
	})
	f := flow.Flow{
		Form: form.Form{Fields: []form.Field{
			&form.Text{Title: "Username", Value: &u.name},
			&form.YesNo{Title: "Grant sudo privilege?", Value: &u.sudo},
			&form.YesNo{Title: "Require sudo password?", Value: &u.password, Skip: func() bool { return !u.sudo }},
			&form.YesNo{Title: "Add SSH key?", Value: &u.addKey},
			&form.Text{Title: "Paste SSH public key", Value: &u.key, Skip: func() bool { return !u.addKey }},
		}},
		Review: func() flow.Review {
			sudoRow := component.KeyValue{Key: "Sudo", Value: "Enabled, No password", State: component.ValueBad}
			switch {
			case !u.sudo:
				sudoRow = component.KeyValue{Key: "Sudo", Value: "Disabled"}
			case u.password:
				sudoRow = component.KeyValue{Key: "Sudo", Value: "Enabled", State: component.ValueOK}
			}
			keyRow := component.KeyValue{Key: "SSH Keys", Value: "None", State: component.ValueWarn}
			if u.addKey {
				keyRow = component.KeyValue{Key: "SSH Keys", Value: "1 key"}
			}
			return flow.Review{Title: "User Configuration", Items: []component.KeyValue{sudoRow, keyRow}}
		},
		Engine:  engine,
		Confirm: func(flow.Plan) flow.Confirm { return flow.Confirm{Question: fmt.Sprintf("Create user '%s'?", u.name)} },
		DryRun:  dryRun,
	}
	return WorkflowStep{Title: trail("User Management", "Create User")}, f
}
