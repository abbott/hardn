package screen

import (
	"strconv"

	"github.com/abbott/hardn/pkg/ui/component"
	"github.com/abbott/hardn/pkg/ui/theme"
)

// Editing hardn.yml from the Configuration screen
// (docs/engine/design-config-edit.md, adopted 2026-11-15): the screen's one
// item, "Edit a setting"; the keys it edits, numbered, each with its value
// (ConfigEdit); a list key's screen, its entries and Add and Remove
// (ConfigList); and the title bar a key's flow runs under (ConfigStep). Here
// with no colour and ASCII glyphs:
//
//	# Edit a setting ###############################################
//
//	   1) username             george
//	   2) sshKeys              1 key
//	   ...
//	  12) enableUnattendedUpgrades  false
//
//	   0) Return
//
//	    > Option [0-12 or q]:
//
// There is no Figma frame.

// ConfigEditItem is the Configuration screen's item, by key.
const ConfigEditItem = "1"

// The list screen's items, by key: Remove only where the list has an entry,
// and Change protocol only for syslogServers with one.
const (
	ConfigListAdd    = "1"
	ConfigListRemove = "2"
	ConfigListChange = "3"
)

// configChangeProto is syslogServers' change of a server's protocol.
const configChangeProto = "Change protocol"

// The editing words (design-config-edit.md §3).
const (
	configEditLabel = "Edit a setting"
	configEditDesc  = "Change a key in hardn.yml"
	configCreate    = "Start hardn again to create one."
	configHeading   = "Select an option"
	configNone      = "None"
)

// configEditKeys are the keys Configuration edits, in the menu's order
// (decision 1 (a)): the keys no other screen saves, checkReleases, and
// enableUnattendedUpgrades, whose true no screen saves.
var configEditKeys = []string{
	"username", "sshKeys", "sshPort", "disableRootSSH", "enableUfwSshPolicy", "configureDns",
	"syslogServers", "ntpServers", "kernel", "confirmTimeout", "checkReleases", "enableUnattendedUpgrades",
}

// configListWords are a list key's words: its section, its rows' key, and
// its items.
var configListWords = map[string]struct{ title, row, add, remove string }{
	"sshKeys":       {"Keys", "Key", "Add key", "Remove key"},
	"ntpServers":    {"Servers", "Server", "Add server", "Remove server"},
	"syslogServers": {"Servers", "Server", "Add server", "Remove server"},
}

// ConfigEditKeys returns the keys Configuration edits, in the menu's order:
// the key of item n is the n-th.
func ConfigEditKeys() []string { return append([]string(nil), configEditKeys...) }

// ConfigEdit is "Edit a setting": the keys, each with its value as the
// Configuration screen words it.
type ConfigEdit struct {
	// Values are the values, by key (ConfigEditKeys); "" is "Not set".
	Values map[string]string
}

// SubMenu returns the screen: the title bar; an item for each key, its value
// beside it; Return; "Option".
func (v ConfigEdit) SubMenu() SubMenu {
	items := make([]component.MenuItem, len(configEditKeys))
	for i, k := range configEditKeys {
		value := v.Values[k]
		if value == "" {
			value = configNotSet
		}
		items[i] = component.MenuItem{Key: strconv.Itoa(i + 1), Label: k, Description: value}
		if len(k) >= theme.LabelWidth {
			// A key that reaches the value column would stack the whole
			// menu (component.Menu): its value follows it on its line.
			items[i] = component.MenuItem{Key: items[i].Key, Label: k + "  " + value}
		}
	}
	return SubMenu{
		Title:  trail(configTitle, configEditLabel),
		Menu:   component.Menu{Items: items, Exit: returnItem()},
		Prompt: menuPrompt,
	}
}

// ConfigStep is the title bar a key's flow runs under: the key.
func ConfigStep(key string) WorkflowStep {
	return WorkflowStep{Title: trail(configTitle, configEditLabel, key)}
}

// ConfigList is a list key's screen: sshKeys, ntpServers or syslogServers.
type ConfigList struct {
	// Key is the key as the file writes it.
	Key string
	// Entries are the list's, in the file's order, each as it is shown: a
	// server, or an SSH key's type, comment and fingerprint.
	Entries []string
	// Note is a warning under the entries; "" for none.
	Note string
}

// SubMenu returns the screen: the title bar; a row for each entry, None with
// none; the note; Add, and Remove (and syslogServers' Change protocol) where
// there is an entry; Return; "Option".
func (v ConfigList) SubMenu() SubMenu {
	w := configListWords[v.Key]
	var rows []component.KeyValue
	for _, e := range v.Entries {
		rows = append(rows, component.KeyValue{Key: w.row, Value: e, StackLong: true})
	}
	if len(rows) == 0 {
		rows = []component.KeyValue{{Key: w.row, Value: configNone}}
	}
	if v.Note != "" {
		rows = append(rows, component.KeyValue{Key: v.Note, State: component.ValueWarn, Finding: true})
	}
	items := []component.MenuItem{{Key: ConfigListAdd, Label: w.add}}
	if len(v.Entries) > 0 {
		items = append(items, component.MenuItem{Key: ConfigListRemove, Label: w.remove})
		if v.Key == "syslogServers" {
			items = append(items, component.MenuItem{Key: ConfigListChange, Label: configChangeProto})
		}
	}
	return SubMenu{
		Title:    trail(configTitle, configEditLabel, v.Key),
		Sections: []Section{configSection(w.title, rows)},
		Menu:     component.Menu{Items: items, Exit: returnItem()},
		Prompt:   menuPrompt,
	}
}
