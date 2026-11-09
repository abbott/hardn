package screen

import (
	"fmt"

	"github.com/abbott/hardn/pkg/ui/component"
	"github.com/abbott/hardn/pkg/ui/flow"
)

// The SSH Login screen (refactor plan slice 6, unit (c), next/s6-screen):
// Figma's 02-SSH_LOGIN and SSH_LOGIN-TOGGLE_CONFIRMATION as the maintainer
// adopted their mocks on 2026-11-03 (docs/engine/gallery-ssh.md §3 and §5,
// "Adopt all, start (b)", with the words of §6). One screen: the SSH Login,
// SSH Auth and SSH Port rows, the SSH Configuration section, and the items
// named by their result (P-12); and each change's words. pkg/ui cannot reach
// the host: pkg/screens/ssh reads it through the SSH Login control
// (pkg/engine/controls/sshchange), fills these with its rows, and runs the
// items (docs/engine/screen-ssh.md).

// The screen's words: decided (gallery-ssh.md §6), Figma's where it has
// them; the heading and the prompt Figma's (fixtureHeading, figmaPrompt). The
// questions, "Now not read" and "held until confirmed" are the adopted
// mocks'.
const (
	sshTitle   = "SSH Login"
	sshSection = "SSH Configuration"

	sshDisallowLabel = "Disallow root login"
	sshDisallowAbout = "Root can't log in over SSH"
	sshKeyRootLabel  = "Allow root key-only"
	sshKeyRootAbout  = "Root logs in by key alone"
	sshPVERootLabel  = "Root key-only"
	sshPVERootAbout  = "Kept for the Proxmox VE cluster"
	sshKeyOnlyLabel  = "Key-only login"
	sshKeyOnlyAbout  = "Turn password login off"
	sshPassOnLabel   = "Password login on"
	sshPassOnAbout   = "Let passwords log in again"
	sshPortLabel     = "Change SSH port"
	sshPortAbout     = "Now %s"
	sshPortsNotRead  = "not read"
	sshMigrateLabel  = "Move old settings"
	sshMigrateAbout  = "Into 00-hardn.conf"
	sshUpgradeLabel  = "Upgrade openssh"
	sshUpgradeAbout  = "Fix CVE-2026-123158"
	sshUpgradeTo     = "%s, to %s or later"
	sshCantRead      = "Can't read"
	sshHeld          = "%s held until confirmed"
	sshMoved         = "%s, into 00-hardn.conf"
)

// The section's keys, which a review shares with the screen's rows
// (pkg/screens/ssh's rows.go), decided (gallery-ssh.md §6).
const (
	SSHRootKey     = "Root SSH Login"
	SSHPasswordKey = "Password Login"
	SSHPortKey     = "SSH Port"
	SSHUsersKey    = "Allowed Users"
	SSHMatchKey    = "Match Block"
	SSHOldKey      = "Old Settings"
	SSHYouKey      = "Logged in as"
)

// The port form's words: its field, decided (gallery-ssh.md §6), and its
// refusals, the adopted mock's; its default's, proposed (screen-ssh.md), as
// Create User's (screen-users.md §5).
const (
	SSHPortField = "New SSH port"
	// SSHPortOutOfRange refuses a port that is no number from 1 to 65535.
	SSHPortOutOfRange = "Type a port from 1 to 65535."
	sshPortHave       = "sshd listens on %d already."
	sshPortDefault    = "Default: %d"
)

// The screen's items, by key; each keeps its key, 4 offered only with
// hardn's old files (gallery-ssh.md §3), 5 only on Alpine where sshd lacks
// CVE-2026-123158's fix (the maintainer, 2026-11-08).
const (
	SSHRootItem     = "1" // Disallow root login, Allow root key-only, Root key-only
	SSHPasswordItem = "2" // Key-only login, Password login on
	SSHPortItem     = "3" // Change SSH port
	SSHMigrateItem  = "4" // Move old settings, with hardn's old files
	SSHUpgradeItem  = "5" // Upgrade openssh, where sshd lacks the fix
)

// SSHRoot is what item 1 does.
type SSHRoot uint8

// The root login items.
const (
	// SSHDisallowRoot is Disallow root login: PermitRootLogin no.
	SSHDisallowRoot SSHRoot = iota
	// SSHAllowRootKeyOnly is Allow root key-only, where root login is
	// disallowed: prohibit-password, never yes.
	SSHAllowRootKeyOnly
	// SSHPVERoot is Root key-only on Proxmox VE, which root keeps.
	SSHPVERoot
)

// SSH is the SSH Login screen's reading of the host, which pkg/screens/ssh
// fills from the SSH Login control's.
type SSH struct {
	// Status is the SSH Login, SSH Auth and SSH Port rows, as the main menu
	// draws them.
	Status component.StatusList
	// Rows are the SSH Configuration section: what sshd runs with, each
	// value's deciding file its note.
	Rows []component.KeyValue
	// Root is what item 1 does.
	Root SSHRoot
	// PasswordsOff: passwords don't log in, so item 2 is Password login on.
	PasswordsOff bool
	// Ports are the ports sshd listens on, "22" or "22 and 2230"; "" where
	// they are not read.
	Ports string
	// OldFiles: hardn's old SSH files are here, so item 4 moves them.
	OldFiles bool
	// NotRead: sshd's settings are not read, so each item's description
	// says so.
	NotRead bool
	// Unfixed: on Alpine, the openssh-server package sshd runs lacks
	// CVE-2026-123158's fix, so item 5 upgrades openssh; its description
	// stands with NotRead, as the upgrade reads no sshd setting.
	Unfixed bool
}

// SubMenu returns the SSH Login screen (02-SSH_LOGIN): the title bar; the
// three rows; the section; root login, password login and the port, each
// named by its result, Move old settings with hardn's old files, and
// Upgrade openssh where sshd lacks CVE-2026-123158's fix, which the alert
// over the section points to (the maintainer, 2026-11-08);
// Return; "Select an option"; "Enter your choice". Every section at every
// height (Firewall's decision 9).
func (s SSH) SubMenu() SubMenu {
	root := component.MenuItem{Key: SSHRootItem, Label: sshDisallowLabel, Description: sshDisallowAbout}
	switch s.Root {
	case SSHAllowRootKeyOnly:
		root.Label, root.Description = sshKeyRootLabel, sshKeyRootAbout
	case SSHPVERoot:
		root.Label, root.Description = sshPVERootLabel, sshPVERootAbout
	}
	pass := component.MenuItem{Key: SSHPasswordItem, Label: sshKeyOnlyLabel, Description: sshKeyOnlyAbout}
	if s.PasswordsOff {
		pass.Label, pass.Description = sshPassOnLabel, sshPassOnAbout
	}
	ports := s.Ports
	if ports == "" {
		ports = sshPortsNotRead
	}
	items := []component.MenuItem{root, pass, {Key: SSHPortItem, Label: sshPortLabel, Description: fmt.Sprintf(sshPortAbout, ports)}}
	if s.OldFiles {
		items = append(items, component.MenuItem{Key: SSHMigrateItem, Label: sshMigrateLabel, Description: sshMigrateAbout})
	}
	if s.NotRead { // sshd -T unread: each item's result not known
		for i := range items {
			items[i].Description = sshCantRead
		}
	}
	var alert *Alert
	if s.Unfixed {
		items = append(items, component.MenuItem{Key: SSHUpgradeItem, Label: sshUpgradeLabel, Description: sshUpgradeAbout})
		alert = &Alert{Title: CVEUnfixed, Item: sshUpgradeLabel}
	}
	return SubMenu{
		Title:    trail(sshTitle),
		Status:   s.Status,
		Alert:    alert,
		Sections: []Section{{Header: component.SectionHeader{Title: sshSection}, Rows: component.BulletList{Items: s.Rows}}},
		Heading:  fixtureHeading,
		Menu:     component.Menu{Items: items, Exit: returnItem()},
		Prompt:   figmaPrompt,
	}
}

// SSHChange is one change's words: the title bar over its flow, its
// question, and its review.
type SSHChange struct {
	Step     WorkflowStep
	Question string
	Review   flow.Review
}

// sshChange is a change named label, its review row the section's row as
// the change leaves it.
func sshChange(label, question string, row component.KeyValue) SSHChange {
	return SSHChange{Step: WorkflowStep{Title: trail(sshTitle, label)}, Question: question,
		Review: flow.Review{Title: sshSection, Items: []component.KeyValue{row}}}
}

// SSHRootChange is item 1's change, its Root SSH Login row value as it
// leaves it.
func SSHRootChange(root SSHRoot, value string) SSHChange {
	row := component.KeyValue{Key: SSHRootKey, Value: value}
	switch root {
	case SSHAllowRootKeyOnly:
		return sshChange(sshKeyRootLabel, "Allow root to log in by key?", row)
	case SSHPVERoot:
		return sshChange(sshPVERootLabel, "Make root key-only?", row)
	}
	return sshChange(sshDisallowLabel, "Disallow root login?", row)
}

// SSHPasswordChange is item 2's change: Password login on where on, else
// Key-only login; its Password Login row value as it leaves it.
func SSHPasswordChange(on bool, value string) SSHChange {
	row := component.KeyValue{Key: SSHPasswordKey, Value: value}
	if on {
		return sshChange(sshPassOnLabel, "Turn password login on?", row)
	}
	return sshChange(sshKeyOnlyLabel, "Turn password login off?", row)
}

// SSHPortChange is Change SSH port: the ports sshd listens on now, held
// until the change is confirmed.
func SSHPortChange(now string) SSHChange {
	return sshChange(sshPortLabel, "Change the SSH port?", component.KeyValue{Key: SSHPortKey, Value: fmt.Sprintf(sshHeld, now)})
}

// SSHMigrateChange is Move old settings: hardn's old files, named, into
// 00-hardn.conf.
func SSHMigrateChange(names string) SSHChange {
	return sshChange(sshMigrateLabel, "Move hardn's old settings?", component.KeyValue{Key: SSHOldKey, Value: fmt.Sprintf(sshMoved, names)})
}

// SSHUpgradeChange is Upgrade openssh: the server package sshd runs, its
// version and the branch's first with the fix.
func SSHUpgradeChange(pkg, installed, wanted string) SSHChange {
	return sshChange(sshUpgradeLabel, "Upgrade openssh?", component.KeyValue{Key: pkg, Value: fmt.Sprintf(sshUpgradeTo, installed, wanted)})
}

// SSHPortHave refuses port, one sshd listens on already.
func SSHPortHave(port uint16) string { return fmt.Sprintf(sshPortHave, port) }

// SSHPortDefault is the port field's description of its default, port.
func SSHPortDefault(port int) string { return fmt.Sprintf(sshPortDefault, port) }
