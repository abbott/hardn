package screen

import (
	"fmt"

	"github.com/abbott/hardn/pkg/ui/component"
	"github.com/abbott/hardn/pkg/ui/flow"
)

// The User Management screen (refactor plan slice 5, unit (c),
// next/s5-screen): Figma's 01-USER, USER-MANAGE_USER-SELECT_USER,
// USER-MANAGE_USER-MANAGEMENT, USER-SSH_KEYS and USER-CREATE_USER as the
// maintainer adopted their mocks on 2026-11-03
// (docs/engine/gallery-users.md §3 and §5, "Adopt all, start (b)", with the
// words of §6). Four screens: the users (Users), the one to manage
// (UsersSelect), one user's (UsersManage) and its keys (UsersKeys); and
// each change's words. pkg/ui cannot reach the host: pkg/screens/users
// reads it through the User control (pkg/engine/controls/users), fills
// these with its rows, and runs the items (docs/engine/screen-users.md).

// The screen's words: decided (gallery-users.md §6), Figma's where it has
// them; the heading and the prompt Figma's (fixtureHeading, figmaPrompt).
const (
	usersTitle         = "User Management"
	usersManageTitle   = "Manage User"
	usersCreateTitle   = "Create User"
	usersSelectHeading = "Select a user"
	usersSection       = "Users"
	usersConfigSection = "User Configuration"
	usersKeysSection   = "SSH Keys"
	usersNoOtherUser   = "No user besides root"
	usersNoKeys        = "No SSH keys"
	usersManaging      = "Managing"
	usersManagingKeys  = "Managing SSH keys for"

	usersManageLabel  = "Manage User"
	usersManageAbout  = "Configure %s, SSH keys"
	usersCreateLabel  = "Create User"
	usersCreateAbout  = "Configure a new user"
	usersGrantLabel   = "Grant admin rights"
	usersGrantAbout   = "Let %s become root"
	usersRemoveLabel  = "Remove admin rights"
	usersRemoveAbout  = "Take them away"
	usersAskLabel     = "Ask for password"
	usersStopLabel    = "Don't ask password"
	usersPasswordFor  = "For admin rights"
	usersKeysLabel    = "SSH Keys"
	usersKeysAbout    = "Add or remove SSH keys"
	usersAddKeyLabel  = "Add SSH key"
	usersDelKeyLabel  = "Remove SSH key"
	usersKeysNone     = "None"
	usersKeysOne      = "1 key"
	usersKeysMany     = "%d keys"
	usersPasswordNone = "None"
)

// The keys a review shares with the screen's rows (pkg/screens/users'
// rows.go), decided (gallery-users.md §6).
const (
	UsersByKey       = "Granted by"
	UsersKeysKey     = "SSH Keys"
	UsersPasswordKey = "Password"
)

// Create's and Remove SSH key's questions, decided (gallery-users.md §6),
// in Figma's polarity for the password (P-11).
const (
	UsersNameField     = "Username"
	UsersAdminField    = "Grant admin rights?"
	UsersPasswordField = "Require password for admin rights?"
	UsersAddKeyField   = "Add SSH key?"
	UsersKeyField      = "Paste SSH public key"
	UsersWhichKeyField = "Remove which key?"
)

// The screens' items, by key. Each keeps its key where another is left
// out: 3 is always SSH Keys, and Create keeps 2 with no user to manage
// (gallery-users.md §3).
const (
	UsersManageItem    = "1" // Manage User, with a user besides root
	UsersCreateItem    = "2" // Create User
	UsersAdminItem     = "1" // Grant or Remove admin rights
	UsersPasswordItem  = "2" // Ask or Don't ask password, for an admin
	UsersKeysItem      = "3" // SSH Keys
	UsersAddKeyItem    = "1" // Add SSH key
	UsersRemoveKeyItem = "2" // Remove SSH key, with any
)

// UsersSection is one user's section: its name, the header, and its rows.
type UsersSection struct {
	Name string
	Rows []component.KeyValue
}

// Users is the users' screen's reading of the host, which pkg/screens/users
// fills from the User control's.
type Users struct {
	// Status is the User row, as the main menu draws it.
	Status component.StatusList
	// Tool is the tool Manage User's detail names: sudo, or doas.
	Tool string
	// Users are the sections, one per user besides root.
	Users []UsersSection
	// NotRead, with no user, is why none is read, in the control's words;
	// "" with none says there is no user besides root.
	NotRead string
}

// SubMenu returns the users' screen (01-USER): the title bar; the User row;
// a section per user, or the line that there is none; Manage User with a
// user to manage, Create User; Return; "Select an option"; "Enter your
// choice". Every section at every height (Firewall's decision 9).
func (u Users) SubMenu() SubMenu {
	vm := SubMenu{Title: trail(usersTitle), Status: u.Status, Heading: fixtureHeading, Prompt: figmaPrompt, Menu: component.Menu{Exit: returnItem()}}
	for _, s := range u.Users {
		vm.Sections = append(vm.Sections, usersRows(s.Name, s.Rows))
	}
	if len(u.Users) == 0 {
		vm.Sections = []Section{usersRows(usersSection, []component.KeyValue{usersNone(u.NotRead, usersNoOtherUser)})}
	} else {
		vm.Menu.Items = append(vm.Menu.Items, component.MenuItem{Key: UsersManageItem, Label: usersManageLabel, Description: fmt.Sprintf(usersManageAbout, u.Tool)})
	}
	vm.Menu.Items = append(vm.Menu.Items, component.MenuItem{Key: UsersCreateItem, Label: usersCreateLabel, Description: usersCreateAbout})
	return vm
}

// usersNone is the muted line of a screen with nothing to list: why it was
// not read, or else none.
func usersNone(notRead, none string) component.KeyValue {
	if notRead != "" {
		return component.KeyValue{Key: notRead, State: component.ValueNone}
	}
	return component.KeyValue{Key: none, State: component.ValueNone}
}

// usersRows is a section of rows.
func usersRows(title string, rows []component.KeyValue) Section {
	return Section{Header: component.SectionHeader{Title: title}, Rows: component.BulletList{Items: rows}}
}

// UsersSelect is Manage User's choice of a user.
type UsersSelect struct {
	// Names are the users, each item's label, in the users' screen's order.
	Names []string
	// NotRead is why no user is read, in the control's words; "" for none.
	NotRead string
}

// SubMenu returns Manage User's Select a user: an item per user; Return.
// With the users not read, the line saying why, and Return alone.
func (s UsersSelect) SubMenu() SubMenu {
	vm := SubMenu{Title: trail(usersTitle, usersManageTitle), Heading: usersSelectHeading, Prompt: figmaPrompt, Menu: component.Menu{Exit: returnItem()}}
	if s.NotRead != "" {
		vm.Sections = []Section{usersRows(usersSection, []component.KeyValue{usersNone(s.NotRead, "")})}
	}
	for i, n := range s.Names {
		vm.Menu.Items = append(vm.Menu.Items, component.MenuItem{Key: fmt.Sprint(i + 1), Label: n})
	}
	return vm
}

// UsersManage is one user's screen.
type UsersManage struct {
	// Name is the user's.
	Name string
	// Rows are its rows, as the users' screen draws them.
	Rows []component.KeyValue
	// Admin: a tool makes it an admin, so item 1 removes the rights and
	// item 2 is the password's; NoPassword: they ask no password, so item 2
	// asks one.
	Admin, NoPassword bool
	// NotRead is why the user is not read, or no longer listed, in the
	// control's words: the line saying so in its rows' place, and Return
	// alone. "" for none.
	NotRead string
}

// SubMenu returns one user's screen (USER-MANAGE_USER-MANAGEMENT): Managing
// and the name; its rows; Grant or Remove admin rights, with them Ask or
// Don't ask password, then SSH Keys, each named by its result; Return.
func (m UsersManage) SubMenu() SubMenu {
	vm := SubMenu{Title: trail(usersTitle, usersManageTitle), Subject: component.SubjectLine{Label: usersManaging, Subject: m.Name},
		Heading: fixtureHeading, Prompt: figmaPrompt, Menu: component.Menu{Exit: returnItem()}}
	if m.NotRead != "" {
		vm.Sections = []Section{usersRows(usersConfigSection, []component.KeyValue{usersNone(m.NotRead, "")})}
		return vm
	}
	vm.Sections = []Section{usersRows(usersConfigSection, m.Rows)}
	admin := component.MenuItem{Key: UsersAdminItem, Label: usersGrantLabel, Description: fmt.Sprintf(usersGrantAbout, m.Name)}
	if m.Admin {
		admin = component.MenuItem{Key: UsersAdminItem, Label: usersRemoveLabel, Description: usersRemoveAbout}
	}
	vm.Menu.Items = []component.MenuItem{admin}
	if m.Admin {
		pw := component.MenuItem{Key: UsersPasswordItem, Label: usersStopLabel, Description: usersPasswordFor}
		if m.NoPassword {
			pw.Label = usersAskLabel
		}
		vm.Menu.Items = append(vm.Menu.Items, pw)
	}
	vm.Menu.Items = append(vm.Menu.Items, component.MenuItem{Key: UsersKeysItem, Label: usersKeysLabel, Description: usersKeysAbout})
	return vm
}

// UsersKeys is one user's keys' screen.
type UsersKeys struct {
	// Name is the user's.
	Name string
	// Keys are its keys, each its fingerprint and its comment.
	Keys []component.KeyValue
	// NotRead is why the user is not read, or no longer listed: as
	// UsersManage's.
	NotRead string
}

// SubMenu returns one user's keys (USER-SSH_KEYS): Managing SSH keys for
// and the name; the keys by fingerprint, or that there are none; Add SSH
// key, and with any Remove SSH key; Return.
func (k UsersKeys) SubMenu() SubMenu {
	vm := SubMenu{Title: trail(usersTitle, usersManageTitle), Subject: component.SubjectLine{Label: usersManagingKeys, Subject: k.Name},
		Heading: fixtureHeading, Prompt: figmaPrompt, Menu: component.Menu{Exit: returnItem()}}
	if k.NotRead != "" {
		vm.Sections = []Section{usersRows(usersKeysSection, []component.KeyValue{usersNone(k.NotRead, "")})}
		return vm
	}
	rows := k.Keys
	if len(rows) == 0 {
		rows = []component.KeyValue{{Key: usersNoKeys, State: component.ValueWarn}}
	}
	vm.Sections = []Section{usersRows(usersKeysSection, rows)}
	vm.Menu.Items = []component.MenuItem{{Key: UsersAddKeyItem, Label: usersAddKeyLabel}}
	if len(k.Keys) > 0 {
		vm.Menu.Items = append(vm.Menu.Items, component.MenuItem{Key: UsersRemoveKeyItem, Label: usersDelKeyLabel})
	}
	return vm
}

// UsersKeysRow is the SSH Keys row of a user with n keys: None, a warning,
// with none.
func UsersKeysRow(n int) component.KeyValue {
	switch {
	case n == 1:
		return component.KeyValue{Key: UsersKeysKey, Value: usersKeysOne}
	case n > 1:
		return component.KeyValue{Key: UsersKeysKey, Value: fmt.Sprintf(usersKeysMany, n)}
	}
	return component.KeyValue{Key: UsersKeysKey, Value: usersKeysNone, State: component.ValueWarn}
}

// UsersChange is one change's words: the title bar over its flow, its
// question, and its review.
type UsersChange struct {
	Step     WorkflowStep
	Question string
	Review   flow.Review
}

// usersStep is the title bar of a change, named by its item.
func usersStep(title string) WorkflowStep { return WorkflowStep{Title: trail(usersTitle, title)} }

// usersAdmin is an admin rights change: the tool's row as it leaves it.
func usersAdmin(label, question string, row component.KeyValue) UsersChange {
	return UsersChange{Step: usersStep(label), Question: question, Review: flow.Review{Title: usersConfigSection, Items: []component.KeyValue{row}}}
}

// UsersGrantChange is Grant admin rights for name, row its tool's row once
// granted.
func UsersGrantChange(name string, row component.KeyValue) UsersChange {
	return usersAdmin(usersGrantLabel, "Grant "+name+" admin rights?", row)
}

// UsersRemoveChange is Remove admin rights for name, row its tool's row
// once removed.
func UsersRemoveChange(name string, row component.KeyValue) UsersChange {
	return usersAdmin(usersRemoveLabel, "Remove "+name+"'s admin rights?", row)
}

// UsersAskChange is Ask for password for name's admin rights, row its
// tool's row then.
func UsersAskChange(name string, row component.KeyValue) UsersChange {
	return usersAdmin(usersAskLabel, "Ask "+name+"'s password for admin rights?", row)
}

// UsersStopAskingChange is Don't ask password for name's admin rights, row
// its tool's row then.
func UsersStopAskingChange(name string, row component.KeyValue) UsersChange {
	return usersAdmin(usersStopLabel, "Stop asking "+name+"'s password for admin rights?", row)
}

// UsersAddKeyChange is Add SSH key for name, who has n keys before it.
func UsersAddKeyChange(name string, n int) UsersChange {
	return UsersChange{Step: usersStep(usersAddKeyLabel), Question: "Add the key for " + name + "?",
		Review: flow.Review{Title: usersKeysSection, Items: []component.KeyValue{UsersKeysRow(n + 1)}}}
}

// UsersRemoveKeyChange is Remove SSH key from name, who has n keys before
// it.
func UsersRemoveKeyChange(name string, n int) UsersChange {
	return UsersChange{Step: usersStep(usersDelKeyLabel), Question: "Remove the key from " + name + "?",
		Review: flow.Review{Title: usersKeysSection, Items: []component.KeyValue{UsersKeysRow(n - 1)}}}
}

// UsersCreateStep is the title bar over Create's form and flow.
func UsersCreateStep() WorkflowStep { return usersStep(usersCreateTitle) }

// UsersCreateReview is Create's review of the user answered: tool, its
// tool's row, with a key or none, and no password; with no key, the plan's
// finding says the user can't log in.
func UsersCreateReview(tool component.KeyValue, key bool) flow.Review {
	keys := UsersKeysRow(0)
	if key {
		keys = UsersKeysRow(1)
	}
	return flow.Review{Title: usersConfigSection, Items: []component.KeyValue{tool, keys, {Key: UsersPasswordKey, Value: usersPasswordNone}}}
}

// UsersCreateQuestion is Create's question.
func UsersCreateQuestion(name string) string { return fmt.Sprintf("Create user '%s'?", name) }
