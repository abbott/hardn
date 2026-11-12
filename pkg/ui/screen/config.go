package screen

import "github.com/abbott/hardn/pkg/ui/component"

// The Configuration screen (docs/ROADMAP.md, "View and manage hardn.yml"; unit
// (a), next/config-gallery): a proposal, read only, on the sub-menu layout as the
// Logs screen is. There is no Figma frame. Nothing in hardn draws it yet: the
// gallery's config listing fills Config (docs/engine/gallery-config.md). pkg/ui
// cannot reach the host or load hardn.yml: the caller reads both and fills Config.

// The Configuration screen's words, all proposed (gallery-config.md, "Words").
const (
	configTitle       = "Configuration"
	configFileSection = "File"
	configPathKey     = "Path"
	configFoundByKey  = "From"
	configNoFile      = "None found"
	configNotFound    = "not found"
	configDefaults    = "hardn is using its defaults."
	configDefault     = "(default)"
	configNotSet      = "Not set"
	configUnused      = "Not used"
)

// ConfigFound is how hardn.yml was looked for.
type ConfigFound uint8

const (
	// ConfigNoFile: neither -f nor HARDN_CONFIG, and no file on the search path.
	ConfigNoFile ConfigFound = iota
	// ConfigByFlag: -f named it.
	ConfigByFlag
	// ConfigByEnv: HARDN_CONFIG named it.
	ConfigByEnv
	// ConfigBySearch: the first file of the search path.
	ConfigBySearch
)

// configFoundBy are the "From" row's values.
var configFoundBy = map[ConfigFound]string{ConfigByFlag: "-f", ConfigByEnv: "HARDN_CONFIG", ConfigBySearch: "Search path"}

// configGroups are the screen's sections and their rows, each row a hardn.yml key
// as the file writes it, grouped by the screen that uses it.
var configGroups = []struct {
	title string
	keys  []string
}{
	{"Users", []string{"username", "adminNoPassword", "sshKeys"}},
	{"SSH", []string{"sshPort", "disableRootSSH", "sshConfigFile"}},
	{"Firewall", []string{"enableUfwSshPolicy", "ufwAppProfiles", "ufwTrustedInterfaces"}},
	{"DNS", []string{"configureDns", "nameservers"}},
	{"Auto Updates", []string{"enableUnattendedUpgrades"}},
	{"Backup", []string{"enableBackups", "backupPath"}},
	{"General", []string{"dryRun", "confirmTimeout"}},
}

// ConfigKeys returns the hardn.yml keys the screen has a row for, in its order.
func ConfigKeys() []string {
	var keys []string
	for _, g := range configGroups {
		keys = append(keys, g.keys...)
	}
	return keys
}

// ConfigValue is one key's value as the screen says it.
type ConfigValue struct {
	// Text is the value, in words: "true", "2: 1.1.1.1, 1.0.0.1"; "" is "Not set".
	Text string
	// Set: the file sets the key. A value it does not set carries the note
	// "default".
	Set bool
}

// ConfigUnused is a key the file sets that nothing in hardn reads.
type ConfigUnused struct {
	// Key is the key as the file writes it; Value its value, "" for none to show.
	Key, Value string
}

// Config is the Configuration screen's reading of hardn.yml, which its caller
// fills.
type Config struct {
	// Path is the file, "" with By ConfigNoFile.
	Path string
	// By is how it was looked for.
	By ConfigFound
	// Missing: -f or HARDN_CONFIG names Path and no file is there.
	Missing bool
	// Values are the keys' values, by key (ConfigKeys); a key with none has no row.
	Values map[string]ConfigValue
	// Unused are the keys the file sets that nothing reads, in order.
	Unused []ConfigUnused
}

// fileSection is the section that names the file and how it was found, ending,
// where no file was read, with the line that says the defaults are in use.
func (v Config) fileSection() Section {
	path := component.KeyValue{Key: configPathKey, Value: v.Path}
	switch {
	case v.By == ConfigNoFile:
		path.Value = configNoFile
	case v.Missing:
		path.State, path.Note = component.ValueBad, configNotFound
	}
	rows := []component.KeyValue{path}
	if by := configFoundBy[v.By]; by != "" {
		rows = append(rows, component.KeyValue{Key: configFoundByKey, Value: by})
	}
	if v.By == ConfigNoFile || v.Missing {
		rows = append(rows, component.KeyValue{Key: configDefaults, State: component.ValueNone})
	}
	return configSection(configFileSection, rows)
}

// configSection is a section of rows under title.
func configSection(title string, rows []component.KeyValue) Section {
	return Section{Header: component.SectionHeader{Title: title}, Rows: component.BulletList{Items: rows}}
}

// SubMenu returns the screen: the title bar; "File"; a section for each group of
// keys; "Not used" where the file sets a key nothing reads, each a warning row;
// Return alone; "Option". It has no item: the screen only reads.
func (v Config) SubMenu() SubMenu {
	vm := SubMenu{
		Title:    trail(configTitle),
		Sections: []Section{v.fileSection()},
		Menu:     component.Menu{Exit: returnItem()},
		Prompt:   menuPrompt,
	}
	for _, g := range configGroups {
		var rows []component.KeyValue
		for _, k := range g.keys {
			val, ok := v.Values[k]
			if !ok {
				continue
			}
			row := component.KeyValue{Key: k, Value: val.Text}
			if row.Value == "" {
				row.Value = configNotSet
			}
			if !val.Set {
				row.Note = configDefault
			}
			rows = append(rows, row)
		}
		vm.Sections = append(vm.Sections, configSection(g.title, rows))
	}
	var unused []component.KeyValue
	for _, u := range v.Unused {
		unused = append(unused, component.KeyValue{Key: u.Key, Value: u.Value, State: component.ValueWarn})
	}
	vm.Sections = append(vm.Sections, configSection(configUnused, unused))
	return vm
}
