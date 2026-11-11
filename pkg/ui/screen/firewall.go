package screen

import (
	"strings"

	"github.com/abbott/hardn/pkg/ui/component"
	"github.com/abbott/hardn/pkg/ui/flow"
)

// The Firewall screen (refactor plan slice 4, unit (c), next/s4-screen):
// origin/main's main menu 4 on the sub-menu layout, as the maintainer adopted
// its mocks on 2026-11-01 (docs/engine/gallery-firewall.md §5, all ten
// decisions, with the words of §6). Three screens: ufw's (Firewall), Proxmox
// VE's, read only (FirewallPVE), and the application profiles' (FirewallProfiles).
// pkg/ui cannot reach the host: pkg/screens/firewall reads it through the
// Firewall control (pkg/engine/controls/firewall), fills these with its rows,
// and runs the items (docs/engine/screen-firewall.md).

// The screen's words: origin/main's titles, labels and "Install UFW"; Backup's
// heading and prompt (fixtureHeading, menuPrompt); the rest decided with
// decision 10 (gallery-firewall.md §6), but the sub-screen's item descriptions
// and the review's "Package", which are the adopted mocks' and proposed
// (screen-firewall.md).
const (
	firewallTitle         = "UFW Firewall Configuration"
	firewallPVETitle      = "Firewall Configuration"
	firewallProfilesTitle = "Manage UFW Application Profiles"
	// The trusted interfaces' words, as the profiles' screen words its own:
	// approved by the maintainer, 2026-11-07 (gallery-firewall.md §6).
	firewallInterfacesTitle = "Manage UFW Trusted Interfaces"

	firewallSectionUFW      = "ufw"
	firewallSectionPVE      = "pve-firewall"
	firewallSectionSSH      = "SSH"
	firewallSectionRules    = "Rules"
	firewallSectionProfiles = "Application Profiles"
	firewallSectionIfaces   = "Trusted Interfaces"

	firewallNoChange = "hardn reads pve-firewall and doesn't change it: a wrong rule can cut off the web UI and the cluster."
	// In a container, the last line of ufw's section and firewalld's (the
	// maintainer, 2026-11-11; proposed words, screen-firewall.md).
	firewallContainerNote = "The container's host may filter too; hardn can't see it."

	firewallDisableLabel  = "Disable firewall"
	firewallDisableAbout  = "Stop ufw (not recommended)"
	firewallProfilesLabel = "Application Profiles"
	firewallProfilesAbout = "Add, remove, apply"
	firewallAddLabel      = "Add profile"
	firewallAddAbout      = "Save a profile in hardn.yml"
	firewallRemoveLabel   = "Remove profile"
	firewallRemoveAbout   = "Take one out of hardn.yml"
	firewallApplyLabel    = "Apply profiles"
	firewallApplyAbout    = "Let their ports through ufw"

	firewallIfacesLabel      = "Trusted Interfaces"
	firewallIfacesAbout      = "Add, remove, apply"
	firewallAddIfaceLabel    = "Add interface"
	firewallAddIfaceAbout    = "Save an interface in hardn.yml"
	firewallRemoveIfaceLabel = "Remove interface"
	firewallRemoveIfaceAbout = "Take one out of hardn.yml"
	firewallApplyIfaceLabel  = "Apply interfaces"
	firewallApplyIfaceAbout  = "Let everything on them through ufw"

	firewallDisableWarning = "This stops the firewall and leaves every port open."
	firewallRemoveNotice   = "This takes the profile out of hardn.yml. A rule already in ufw stays."
	firewallTrustNotice    = "Everything arriving on this interface gets through ufw: every port, from every address on it."
	firewallNeedsSSNotice  = "hardn needs ss, from iproute2, to check which ports SSH uses before it starts ufw."
	firewallApplyInactive  = "Enable the firewall first"
	firewallUntrustNotice  = "This takes the interface out of hardn.yml. A rule already in ufw stays."
	firewallPackageKey     = "Package"
)

// The ufw section's keys a review shares with the screen's rows
// (pkg/screens/firewall's rows.go), decided (gallery-firewall.md §6).
const (
	FirewallStatusKey = "Status"
	FirewallPolicyKey = "Incoming policy"
	FirewallBootKey   = "At boot"
)

// The Firewall screen's items, by key: origin/main's Profiles moved from 3 to
// 2, its "Configure firewall" gone (gallery-firewall.md §5); Trusted Interfaces
// 3 since 2026-11-07, Disable after it, so that the last item is the one shown
// only on an active ufw.
const (
	FirewallFirstItem      = "1" // by what its plan does (FirewallFirst)
	FirewallProfilesItem   = "2" // Application Profiles, where ufw is installed
	FirewallInterfacesItem = "3" // Trusted Interfaces, where ufw is installed
	FirewallDisableItem    = "4" // Disable, on an active ufw whose item 1 is not it
)

// The application profiles' items, by key.
const (
	FirewallAddItem    = "1" // Add profile
	FirewallRemoveItem = "2" // Remove profile, with any
	FirewallApplyItem  = "3" // Apply profiles, with any
)

// FirewallFirst is what item 1 does, by which it is worded: the control's
// Reading.First, and on Enable what its plan does (Reading.Does).
type FirewallFirst uint8

const (
	FirewallNoFirst      FirewallFirst = iota // no item 1
	FirewallInstall                           // no ufw: Install
	FirewallEnable                            // ufw not running: Enable, or its refusal
	FirewallSetDeny                           // running, not deny: Let SSH through, deny by default
	FirewallLetSSH                            // running: Add its rules, other rules staying
	FirewallLetSSHBoot                        // running: Add its rules, start at boot
	FirewallStartBoot                         // running, let through: start at boot
	FirewallRepair                            // running, and Enable refuses: selecting it says why
	FirewallDisableFirst                      // active and set: Disable
	FirewallEnableNoSSH                       // ufw not running, no SSH server: Enable, denying by default
	FirewallSetDenyNoSSH                      // running, not deny, no SSH server: deny by default
	FirewallInstallSS                         // ufw running, ss not: Install iproute2 (the maintainer, 2026-11-07)
	FirewallEnableSS                          // ufw not running, ss not: Enable, iproute2 installed first (the maintainer, 2026-11-07)
)

// firewallWords are an item 1's words and its flow's.
type firewallWords struct{ label, about, title, question string }

// firewallFirsts are each FirewallFirst's words. FirewallRepair has no flow:
// it is refused before asking.
var firewallFirsts = map[FirewallFirst]firewallWords{
	FirewallInstall:      {"Install UFW", "Install the ufw package", "Install UFW", "Install ufw?"},
	FirewallEnable:       {"Enable firewall", "Let SSH through, then start ufw", "Enable Firewall", "Enable the firewall?"},
	FirewallSetDeny:      {"Set deny policy", "Let SSH through, deny by default", "Set Deny Policy", "Set the deny policy?"},
	FirewallLetSSH:       {"Let SSH through", "Add its rules; other rules stay", "Let SSH Through", "Let SSH through?"},
	FirewallLetSSHBoot:   {"Let SSH through", "Add its rules, start at boot", "Let SSH Through", "Let SSH through?"},
	FirewallStartBoot:    {"Start ufw at boot", "Add it to the boot services", "Start at Boot", "Start ufw at boot?"},
	FirewallRepair:       {label: "Repair firewall", about: "Refused here: select to see why"},
	FirewallDisableFirst: {firewallDisableLabel, firewallDisableAbout, "Disable Firewall", "Disable the firewall?"},
	// No SSH server (the maintainer, 2026-11-07): nothing for SSH to let through.
	FirewallEnableNoSSH:  {"Enable firewall", "Deny by default, then start ufw", "Enable Firewall", "Enable the firewall?"},
	FirewallSetDenyNoSSH: {"Set deny policy", "Deny incoming by default", "Set Deny Policy", "Set the deny policy?"},
	// No ss: Enable needs it to read the bound sockets (approved by the
	// maintainer, 2026-11-07; gallery-firewall.md §6).
	FirewallInstallSS: {"Install iproute2", "Needed to check SSH gets through", "Install iproute2", "Install iproute2?"},
	// ufw not running and no ss: Enable, its first step the install, its
	// question the install's (the maintainer, 2026-11-07).
	FirewallEnableSS: {"Enable firewall", "Installs iproute2, then starts ufw", "Enable Firewall", "Install iproute2?"},
}

// Firewall is ufw's screen's reading of the host, which pkg/screens/firewall
// fills from the Firewall control's.
type Firewall struct {
	// Status is the Firewall row, as the main menu draws it.
	Status component.StatusList
	// UFW, SSH, Rules, Profiles and Interfaces are the sections' rows; a
	// section with none is left out.
	UFW, SSH, Rules, Profiles, Interfaces []component.KeyValue
	// First is what item 1 does; Risk, that its plan is lockout-risk, which
	// marks the item.
	First FirewallFirst
	Risk  bool
	// Installed: ufw is installed, so item 2 leads to its profiles and item 3
	// to its trusted interfaces.
	Installed bool
	// DisableBeside: ufw is active and item 1 is not Disable: item 4 is.
	DisableBeside bool
	// KeepsReject: Enable leaves the default incoming policy reject, which its
	// review says.
	KeepsReject bool
	// Container: hardn runs in a container, so the section ufw ends with the
	// muted line that its host may filter traffic too.
	Container bool
}

// SubMenu returns ufw's screen: the title bar; the Firewall row; the sections
// ufw, SSH, Rules, Application Profiles, Trusted Interfaces, ufw's ending with
// the container line where Container holds; item 1 by First, items 2 and 3
// where ufw is installed, item 4 where Disable is beside item 1; Return;
// "Select an option"; "Option". Every section at every height
// (decision 9).
func (f Firewall) SubMenu() SubMenu {
	vm := SubMenu{Title: trail(firewallTitle), Status: f.Status, Heading: fixtureHeading, Prompt: menuPrompt, Menu: component.Menu{Exit: returnItem()}}
	for _, s := range []struct {
		title string
		rows  []component.KeyValue
	}{{firewallSectionUFW, containerRows(f.UFW, f.Container)}, {firewallSectionSSH, f.SSH}, {firewallSectionRules, f.Rules}, {firewallSectionProfiles, f.Profiles}, {firewallSectionIfaces, f.Interfaces}} {
		if len(s.rows) > 0 {
			vm.Sections = append(vm.Sections, firewallSection(s.title, s.rows))
		}
	}
	if w, ok := firewallFirsts[f.First]; ok {
		vm.Menu.Items = append(vm.Menu.Items, component.MenuItem{Key: FirewallFirstItem, Label: w.label, Description: w.about, Risk: f.Risk})
	}
	if f.Installed {
		vm.Menu.Items = append(vm.Menu.Items, component.MenuItem{Key: FirewallProfilesItem, Label: firewallProfilesLabel, Description: firewallProfilesAbout},
			component.MenuItem{Key: FirewallInterfacesItem, Label: firewallIfacesLabel, Description: firewallIfacesAbout})
	}
	if f.DisableBeside {
		vm.Menu.Items = append(vm.Menu.Items, component.MenuItem{Key: FirewallDisableItem, Label: firewallDisableLabel, Description: firewallDisableAbout})
	}
	return vm
}

// containerRows are rows, a section's, with the container line, muted, after
// them where in holds and there are any; a copy, rows unchanged.
func containerRows(rows []component.KeyValue, in bool) []component.KeyValue {
	if !in || len(rows) == 0 {
		return rows
	}
	return append(append([]component.KeyValue(nil), rows...), component.KeyValue{Key: firewallContainerNote, State: component.ValueNone})
}

// firewallSection is a section of rows.
func firewallSection(title string, rows []component.KeyValue) Section {
	return Section{Header: component.SectionHeader{Title: title}, Rows: component.BulletList{Items: rows}}
}

// FirewallChange is one change's words: the title bar over its flow, its
// question, and its review.
type FirewallChange struct {
	Step     WorkflowStep
	Question string
	Review   flow.Review
}

// firewallStep is the title bar of a change on ufw's screen.
func firewallStep(title string) WorkflowStep {
	return WorkflowStep{Title: trail(firewallTitle, title)}
}

// FirstChange is item 1's change by First: Install's review, the package and
// ufw inactive after it; Enable's in any of its words, ufw as its plan leaves
// it, active, its policy, and at boot; Disable's (FirewallDisableChange). Its
// service not read, Enable refuses before this. FirewallRepair and no item have
// none.
func (f Firewall) FirstChange() FirewallChange {
	w := firewallFirsts[f.First]
	switch f.First {
	case FirewallNoFirst, FirewallRepair:
		return FirewallChange{}
	case FirewallInstall:
		return FirewallChange{Step: firewallStep(w.title), Question: w.question, Review: flow.Review{Title: firewallSectionUFW,
			Items: []component.KeyValue{{Key: firewallPackageKey, Value: "ufw"}, {Key: FirewallStatusKey, Value: "Inactive"}}}}
	case FirewallInstallSS:
		return FirewallChange{Step: firewallStep(w.title), Question: w.question, Review: flow.Review{Title: firewallSectionSSH,
			Items: []component.KeyValue{{Key: firewallPackageKey, Value: "iproute2"}}}}
	case FirewallEnableSS:
		return FirewallChange{Step: firewallStep(w.title), Question: w.question, Review: flow.Review{
			Notice: &flow.Notice{Callout: component.Callout{Kind: component.MessageInfo, Body: []string{firewallNeedsSSNotice}}},
			Title:  firewallSectionSSH, Items: []component.KeyValue{{Key: firewallPackageKey, Value: "iproute2"}}}}
	case FirewallDisableFirst:
		return FirewallDisableChange()
	}
	policy := "Deny"
	if f.KeepsReject {
		policy = "Reject"
	}
	return FirewallChange{Step: firewallStep(w.title), Question: w.question, Review: flow.Review{Title: firewallSectionUFW, Items: []component.KeyValue{
		{Key: FirewallStatusKey, Value: "Active", State: component.ValueOK}, {Key: FirewallPolicyKey, Value: policy, State: component.ValueOK},
		{Key: FirewallBootKey, Value: "Yes", State: component.ValueOK}}}}
}

// FirewallDisableChange is Disable's change, from item 1 or its own: the warning
// above ufw inactive, y/N (decision 6).
func FirewallDisableChange() FirewallChange {
	w := firewallFirsts[FirewallDisableFirst]
	return FirewallChange{Step: firewallStep(w.title), Question: w.question, Review: flow.Review{
		Notice: firewallWarning(firewallDisableWarning),
		Title:  firewallSectionUFW, Items: []component.KeyValue{{Key: FirewallStatusKey, Value: "Inactive", State: component.ValueWarn}}}}
}

// firewallWarning is a review's warning.
func firewallWarning(words string) *flow.Notice {
	return &flow.Notice{Callout: component.Callout{Kind: component.MessageWarn, Body: []string{words}}}
}

// FirewallPVE is the screen on Proxmox VE, read only (brief 2.5; decision 7).
type FirewallPVE struct {
	// Status is the Firewall row.
	Status component.StatusList
	// Rows are the section pve-firewall's, above the line saying why hardn does
	// not change it.
	Rows []component.KeyValue
}

// SubMenu returns the screen on Proxmox VE: its own title bar, as origin/main's
// says UFW, which is false here; the Firewall row; the section pve-firewall;
// Return alone, with no heading.
func (f FirewallPVE) SubMenu() SubMenu {
	rows := append(append([]component.KeyValue(nil), f.Rows...), component.KeyValue{Key: firewallNoChange, State: component.ValueNone})
	return SubMenu{Title: trail(firewallPVETitle), Status: f.Status, Sections: []Section{firewallSection(firewallSectionPVE, rows)},
		Menu: component.Menu{Exit: returnItem()}, Prompt: menuPrompt}
}

// FirewallProfiles is the application profiles' screen's reading of hardn.yml
// and ufw.
type FirewallProfiles struct {
	// Rows are the section's, each profile's or the muted line that there are
	// none.
	Rows []component.KeyValue
	// Any: hardn.yml has a profile, so there is one to remove or apply.
	Any bool
	// Inactive: ufw is installed and not running, so Apply says to enable the
	// firewall first, as its refusal does.
	Inactive bool
}

// SubMenu returns the application profiles' screen: the section; Add, and with
// any profile Remove and Apply; Return.
func (f FirewallProfiles) SubMenu() SubMenu {
	vm := SubMenu{Title: trail(firewallTitle, firewallProfilesTitle), Sections: []Section{firewallSection(firewallSectionProfiles, f.Rows)},
		Heading: fixtureHeading, Prompt: menuPrompt, Menu: component.Menu{Exit: returnItem(),
			Items: []component.MenuItem{{Key: FirewallAddItem, Label: firewallAddLabel, Description: firewallAddAbout}}}}
	if f.Any {
		vm.Menu.Items = append(vm.Menu.Items, component.MenuItem{Key: FirewallRemoveItem, Label: firewallRemoveLabel, Description: firewallRemoveAbout},
			component.MenuItem{Key: FirewallApplyItem, Label: firewallApplyLabel, Description: applyAbout(firewallApplyAbout, f.Inactive)})
	}
	return vm
}

// applyAbout is Apply's description: about, or with ufw not running, that the
// firewall is to be enabled first (the maintainer, 2026-11-07).
func applyAbout(about string, inactive bool) string {
	if inactive {
		return firewallApplyInactive
	}
	return about
}

// firewallProfileStep is the title bar of a change on the profiles' screen.
func firewallProfileStep(title string) WorkflowStep {
	return WorkflowStep{Title: trail(firewallTitle, firewallProfilesTitle, title)}
}

// ApplyChange is Apply's change: the section's rows as they stand, y/N.
func (f FirewallProfiles) ApplyChange() FirewallChange {
	return FirewallChange{Step: firewallProfileStep("Apply Profiles"), Question: "Apply the profiles?",
		Review: flow.Review{Title: firewallSectionProfiles, Items: f.Rows}}
}

// FirewallAddStep is the title bar over Add's form and flow.
func FirewallAddStep() WorkflowStep { return firewallProfileStep("Add Profile") }

// FirewallAddReview is Add's review of the profile answered: its name and ports,
// " on <interface>" after them where it is limited to one, as its row says.
func FirewallAddReview(name string, ports []string, iface string) flow.Review {
	v := strings.Join(ports, ", ")
	if iface != "" {
		v += " on " + iface
	}
	return flow.Review{Title: firewallSectionProfiles, Items: []component.KeyValue{{Key: name, Value: v}}}
}

// FirewallAddQuestion is Add's question.
func FirewallAddQuestion(name string) string { return "Add " + name + "?" }

// FirewallRemoveStep is the title bar over Remove's form and flow.
func FirewallRemoveStep() WorkflowStep { return firewallProfileStep("Remove Profile") }

// FirewallRemoveReview is Remove's review: that a rule already in ufw stays.
func FirewallRemoveReview() flow.Review {
	return flow.Review{Notice: firewallWarning(firewallRemoveNotice)}
}

// FirewallRemoveQuestion is Remove's question.
func FirewallRemoveQuestion(name string) string { return "Remove " + name + "?" }

// FirewallInterfaces is the trusted interfaces' screen's reading of hardn.yml
// and ufw (the maintainer, 2026-11-07).
type FirewallInterfaces struct {
	// Rows are the section's, each interface's or the muted line that there
	// are none.
	Rows []component.KeyValue
	// Any: hardn.yml has one, so there is one to remove or apply.
	Any bool
	// Inactive: ufw is installed and not running (FirewallProfiles').
	Inactive bool
}

// SubMenu returns the trusted interfaces' screen: the section; Add, and with
// any interface Remove and Apply; Return. Its items are keyed as the
// profiles' screen's.
func (f FirewallInterfaces) SubMenu() SubMenu {
	vm := SubMenu{Title: trail(firewallTitle, firewallInterfacesTitle), Sections: []Section{firewallSection(firewallSectionIfaces, f.Rows)},
		Heading: fixtureHeading, Prompt: menuPrompt, Menu: component.Menu{Exit: returnItem(),
			Items: []component.MenuItem{{Key: FirewallAddItem, Label: firewallAddIfaceLabel, Description: firewallAddIfaceAbout}}}}
	if f.Any {
		vm.Menu.Items = append(vm.Menu.Items, component.MenuItem{Key: FirewallRemoveItem, Label: firewallRemoveIfaceLabel, Description: firewallRemoveIfaceAbout},
			component.MenuItem{Key: FirewallApplyItem, Label: firewallApplyIfaceLabel, Description: applyAbout(firewallApplyIfaceAbout, f.Inactive)})
	}
	return vm
}

// firewallIfaceStep is the title bar of a change on the trusted interfaces'
// screen.
func firewallIfaceStep(title string) WorkflowStep {
	return WorkflowStep{Title: trail(firewallTitle, firewallInterfacesTitle, title)}
}

// ApplyChange is Apply's change: the section's rows as they stand, y/N.
func (f FirewallInterfaces) ApplyChange() FirewallChange {
	return FirewallChange{Step: firewallIfaceStep("Apply Interfaces"), Question: "Apply the interfaces?",
		Review: flow.Review{Title: firewallSectionIfaces, Items: f.Rows}}
}

// FirewallTrustStep is the title bar over Add's form and flow.
func FirewallTrustStep() WorkflowStep { return firewallIfaceStep("Add Interface") }

// FirewallTrustReview is Add's review: what trusting the interface lets
// through, then the interface.
func FirewallTrustReview(iface string) flow.Review {
	return flow.Review{Notice: firewallWarning(firewallTrustNotice), Title: firewallSectionIfaces,
		Items: []component.KeyValue{{Key: iface, Value: "Every port"}}}
}

// FirewallTrustQuestion is Add's question.
func FirewallTrustQuestion(iface string) string { return "Trust " + iface + "?" }

// FirewallUntrustStep is the title bar over Remove's form and flow.
func FirewallUntrustStep() WorkflowStep { return firewallIfaceStep("Remove Interface") }

// FirewallUntrustReview is Remove's review: that a rule already in ufw stays.
func FirewallUntrustReview() flow.Review {
	return flow.Review{Notice: firewallWarning(firewallUntrustNotice)}
}

// FirewallUntrustQuestion is Remove's question.
func FirewallUntrustQuestion(iface string) string { return "Remove " + iface + "?" }
