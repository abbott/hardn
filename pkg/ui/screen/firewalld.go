package screen

import (
	"github.com/abbott/hardn/pkg/ui/component"
	"github.com/abbott/hardn/pkg/ui/flow"
)

// The Firewall screen on Rocky Linux and AlmaLinux (docs/engine/el-firewalld.md
// §1.6): firewalld's section, item 1 (Install, Enable or Disable), Disable
// beside Enable where firewalld starts at boot, and Return. No profiles or
// interfaces items: those are ufw's. pkg/screens/firewall fills it from the
// Firewall control's reading. Its words are proposed.

const (
	firewalldTitle   = "Firewall Configuration"
	firewalldSection = "firewalld"

	firewalldDisableLabel   = "Disable firewalld"
	firewalldDisableAbout   = "Stop firewalld (not recommended)"
	firewalldDisableWarning = "This stops firewalld and leaves the host with no firewall."
	firewalldBootOffAbout   = "Don't start firewalld at boot"
	firewalldBootOffWarning = "firewalld won't start at the next boot, which leaves the host with no firewall then."
	firewalldSSHKey         = "SSH"
)

// The firewalld section's keys, which pkg/screens/firewall's rows use.
const (
	FirewalldInstalledKey = "Installed"
	FirewalldRunningKey   = "Running"
	FirewalldDefaultKey   = "Default zone"
	FirewalldActiveKey    = "Active zones"
)

// The firewalld screen's items, by key: item 1, then Disable beside it, so
// that the menu reads 1, 2, 0.
const (
	FirewalldFirstItem   = "1" // by what it does (FirewalldFirst)
	FirewalldDisableItem = "2" // Disable, beside an item 1 Enable where firewalld starts at boot
)

// FirewalldFirst is what item 1 does on firewalld's screen: the control's
// Reading.First there.
type FirewalldFirst uint8

const (
	FirewalldNoFirst   FirewalldFirst = iota // no item 1
	FirewalldInstall                         // firewalld not installed: Install
	FirewalldInstallSS                       // ss missing: Install iproute, which brings it there
	FirewalldEnable                          // stopped: Enable, or its refusal
	FirewalldDisable                         // running: Disable
)

// firewalldFirsts are each FirewalldFirst's words.
var firewalldFirsts = map[FirewalldFirst]firewallWords{
	FirewalldInstall:   {"Install firewalld", "Install the firewalld package", "Install firewalld", "Install firewalld?"},
	FirewalldInstallSS: {"Install iproute", "Needed to check SSH gets through", "Install iproute", "Install iproute?"},
	FirewalldEnable:    {"Enable firewalld", "Let SSH through, then start firewalld", "Enable firewalld", "Enable firewalld?"},
	FirewalldDisable:   {firewalldDisableLabel, firewalldDisableAbout, "Disable firewalld", "Disable firewalld?"},
}

// Firewalld is firewalld's screen's reading of the host.
type Firewalld struct {
	// Status is the Firewall row, as the main menu draws it.
	Status component.StatusList
	// Rows are the section firewalld's.
	Rows []component.KeyValue
	// First is what item 1 does; Risk, that its plan is lockout-risk, which
	// marks the item.
	First FirewalldFirst
	Risk  bool
	// DisableBeside: item 1 is Enable and firewalld starts at boot: item 2
	// is Disable.
	DisableBeside bool
}

// SubMenu returns firewalld's screen: the title bar; the Firewall row; the
// section firewalld; item 1 by First, item 2 where Disable is beside it;
// Return; "Select an option"; "Enter selection".
func (f Firewalld) SubMenu() SubMenu {
	vm := SubMenu{Title: trail(firewalldTitle), Status: f.Status, Heading: fixtureHeading, Prompt: mainPrompt, Menu: component.Menu{Exit: returnItem()}}
	if len(f.Rows) > 0 {
		vm.Sections = append(vm.Sections, firewallSection(firewalldSection, f.Rows))
	}
	if w, ok := firewalldFirsts[f.First]; ok {
		vm.Menu.Items = append(vm.Menu.Items, component.MenuItem{Key: FirewalldFirstItem, Label: w.label, Description: w.about, Risk: f.Risk})
	}
	if f.DisableBeside {
		vm.Menu.Items = append(vm.Menu.Items, component.MenuItem{Key: FirewalldDisableItem, Label: firewalldDisableLabel, Description: firewalldBootOffAbout})
	}
	return vm
}

// firewalldStep is the title bar of a change on firewalld's screen.
func firewalldStep(title string) WorkflowStep {
	return WorkflowStep{Title: trail(firewalldTitle, title)}
}

// FirstChange is item 1's change by First: Install's review, the package,
// not running and not started at boot after it; Install iproute's, the
// package; Enable's, running, at boot, SSH let through; Disable's
// (FirewalldDisableChange). No item has none.
func (f Firewalld) FirstChange() FirewallChange {
	w := firewalldFirsts[f.First]
	switch f.First {
	case FirewalldInstall:
		return FirewallChange{Step: firewalldStep(w.title), Question: w.question, Review: flow.Review{Title: firewalldSection, Items: []component.KeyValue{
			{Key: firewallPackageKey, Value: "firewalld"}, {Key: FirewallStatusKey, Value: "Not running"}, {Key: FirewallBootKey, Value: "No"}}}}
	case FirewalldInstallSS:
		return FirewallChange{Step: firewalldStep(w.title), Question: w.question, Review: flow.Review{Title: firewallSectionSSH,
			Items: []component.KeyValue{{Key: firewallPackageKey, Value: "iproute"}}}}
	case FirewalldEnable:
		return FirewallChange{Step: firewalldStep(w.title), Question: w.question, Review: flow.Review{Title: firewalldSection, Items: []component.KeyValue{
			{Key: FirewallStatusKey, Value: "Running", State: component.ValueOK}, {Key: FirewallBootKey, Value: "Yes", State: component.ValueOK},
			{Key: firewalldSSHKey, Value: "Let through", State: component.ValueOK}}}}
	case FirewalldDisable:
		return FirewalldDisableChange(true)
	}
	return FirewallChange{}
}

// FirewalldDisableChange is Disable's change: where it stops firewalld
// (item 1, firewalld running), the warning that the host keeps no
// firewall, above not running and not started at boot; beside Enable,
// where firewalld is stopped and Disable only keeps it from starting at
// boot, the warning that it won't then, above not started at boot. y/N.
func FirewalldDisableChange(stops bool) FirewallChange {
	w := firewalldFirsts[FirewalldDisable]
	notice, items := firewalldBootOffWarning, []component.KeyValue{{Key: FirewallBootKey, Value: "No", State: component.ValueWarn}}
	if stops {
		notice = firewalldDisableWarning
		items = append([]component.KeyValue{{Key: FirewallStatusKey, Value: "Not running", State: component.ValueWarn}}, items...)
	}
	return FirewallChange{Step: firewalldStep(w.title), Question: w.question, Review: flow.Review{
		Notice: firewallWarning(notice), Title: firewalldSection, Items: items}}
}
