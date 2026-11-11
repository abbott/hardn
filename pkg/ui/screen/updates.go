package screen

import (
	"github.com/abbott/hardn/pkg/ui/component"
	"github.com/abbott/hardn/pkg/ui/flow"
)

// The Auto Updates screen (refactor plan slice 7, unit (c), next/s7-screen):
// the mock the maintainer adopted on 2026-11-03 (docs/engine/gallery-updates.md
// §4, "Adopt all, start (b)", with the words of §5), on the sub-menu layout
// (no Figma frame). One screen: the Auto Updates row, the Update
// Configuration section, and one item named by its result (P-12), shown as
// unavailable (P-22) for a state of the host itself; and each direction's
// words. pkg/ui cannot reach the host: pkg/screens/updates reads it through
// the Auto Updates control (pkg/engine/controls/updates), fills these with
// its rows, and runs the item (docs/engine/screen-updates.md).

// The screen's words: decided (gallery-updates.md §5).
const (
	updatesTitle   = "Auto Updates"
	updatesSection = "Update Configuration"

	updatesEnable      = "Enable automatic updates"
	updatesDisable     = "Disable automatic updates"
	updatesDescInstall = "Install unattended-upgrades"
	updatesDescApt     = "Daily, by unattended-upgrades"
	updatesDescApk     = "Daily, by apk-cron"
	updatesDescDnf     = "Daily security updates, by dnf-automatic" // proposed, el-updates.md §1.4
	updatesDescDnfInst = "Install dnf-automatic"                    // proposed, el-updates.md §1.4
	updatesDescUndo    = "Undo hardn's changes"
	updatesDescOutside = "Turned on outside hardn"
	updatesDescPVE     = "Not on Proxmox VE"

	updatesAskOn  = "Turn automatic updates on?"
	updatesAskOff = "Turn automatic updates off?"
	updatesOn     = "On"
	updatesOff    = "Off"
)

// The section's keys (gallery-updates.md §5), which pkg/screens/updates'
// rows.go fills with the control's reading.
const (
	UpdatesPackageKey  = "unattended-upgrades"
	UpdatesUpgradesKey = "Upgrades"
	UpdatesListsKey    = "Package Lists"
	UpdatesPeriodicKey = "Periodic Jobs"
	UpdatesUpTimerKey  = "Upgrade Timer"
	UpdatesLsTimerKey  = "Lists Timer"
	UpdatesApkCronKey  = "apk-cron"
	UpdatesScriptKey   = "Daily Script"
	UpdatesCrondKey    = "crond"
	UpdatesLegacyKey   = "Old Script"
	UpdatesOthersKey   = "Other Jobs"
	UpdatesPVEKey      = "Proxmox VE"
	UpdatesDnfAutoKey  = "dnf-automatic" // Rocky Linux and AlmaLinux: el-updates.md §1.4
	UpdatesInTimerKey  = "Install Timer"
	UpdatesTypeKey     = "Updates"
	UpdatesRebootKey   = "Restarts"
	UpdatesByKey       = "Turned on by"
)

// UpdatesItemKey is the screen's one item's key.
const UpdatesItemKey = "1"

// UpdatesItem is what the screen's item does.
type UpdatesItem uint8

// The item's states (gallery-updates.md decisions 1 and 7).
const (
	// UpdatesEnable is Enable automatic updates: off, or partial.
	UpdatesEnable UpdatesItem = iota
	// UpdatesDisable is Disable automatic updates: on by hardn.
	UpdatesDisable
	// UpdatesOutside is Disable shown as unavailable: on, not by hardn.
	UpdatesOutside
	// UpdatesPVE is Enable shown as unavailable: Proxmox VE, updated by
	// hand.
	UpdatesPVE
)

// Updates is the Auto Updates screen's reading of the host, which
// pkg/screens/updates fills from the Auto Updates control's.
type Updates struct {
	// Status is the Auto Updates row, as the main menu draws it, with the
	// "changed since hardn" line where the control finds it (decision 9).
	Status component.StatusList
	// Rows are the Update Configuration section: what the host has, never
	// hardn.yml.
	Rows []component.KeyValue
	// Item is what the item does.
	Item UpdatesItem
	// Apk: Alpine, whose updates run by apk-cron.
	Apk bool
	// Dnf: Rocky Linux and AlmaLinux, whose updates run by dnf-automatic
	// (docs/engine/el-updates.md §1.4).
	Dnf bool
	// Install: the package (unattended-upgrades, or dnf-automatic with
	// Dnf) is not installed, so Enable installs it.
	Install bool
	// Blocked is what is in the way of the item, in the control's words
	// (R1: updates.Reading.Blocked); "" for nothing. An unavailable item
	// keeps its own description.
	Blocked string
}

// Off reports whether the item turns updates off.
func (u Updates) Off() bool { return u.Item == UpdatesDisable || u.Item == UpdatesOutside }

// MenuItem is the screen's one item, named by its result (P-12).
func (u Updates) MenuItem() component.MenuItem {
	it := component.MenuItem{Key: UpdatesItemKey, Label: updatesEnable, Description: updatesDescApt}
	switch {
	case u.Apk:
		it.Description = updatesDescApk
	case u.Dnf && u.Install:
		it.Description = updatesDescDnfInst
	case u.Dnf:
		it.Description = updatesDescDnf
	case u.Install:
		it.Description = updatesDescInstall
	}
	switch u.Item {
	case UpdatesPVE:
		it.Description, it.Disabled = updatesDescPVE, true
	case UpdatesDisable:
		it.Label, it.Description = updatesDisable, updatesDescUndo
	case UpdatesOutside:
		it.Label, it.Description, it.Disabled = updatesDisable, updatesDescOutside, true
	}
	if u.Blocked != "" && !it.Disabled {
		it.Description = u.Blocked
	}
	return it
}

// SubMenu returns the Auto Updates screen: the title bar; the row; the
// section; the item; Return; "Select an option"; "Option". Every
// section at every height (Firewall's decision 9).
func (u Updates) SubMenu() SubMenu {
	return SubMenu{
		Title:    trail(updatesTitle),
		Status:   u.Status,
		Sections: []Section{{Header: component.SectionHeader{Title: updatesSection}, Rows: component.BulletList{Items: u.Rows}}},
		Heading:  fixtureHeading,
		Menu:     component.Menu{Items: []component.MenuItem{u.MenuItem()}, Exit: returnItem()},
		Prompt:   menuPrompt,
	}
}

// UpdatesChange is the item's change: the title bar over its flow, its
// question, and its review.
type UpdatesChange struct {
	Step     WorkflowStep
	Question string
	Review   flow.Review
}

// Change is the item's change, its review the row as the change leaves it,
// then notes, the control's lines beyond the change (updates.Auto's Notes).
func (u Updates) Change(notes []component.KeyValue) UpdatesChange {
	label, question, value := updatesEnable, updatesAskOn, updatesOn
	if u.Off() {
		label, question, value = updatesDisable, updatesAskOff, updatesOff
	}
	items := append([]component.KeyValue{{Key: updatesTitle, Value: value}}, notes...)
	return UpdatesChange{Step: WorkflowStep{Title: trail(updatesTitle, label)}, Question: question,
		Review: flow.Review{Title: updatesSection, Items: items}}
}
