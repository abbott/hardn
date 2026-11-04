package config

import (
	"slices"

	"go.yaml.in/yaml/v3"
)

// retiredKind is how a retired key's value was read when Config had a
// field for it, and so which value of it is the old default.
type retiredKind int

const (
	retiredList retiredKind = iota // a list: the default none
	retiredFlag                    // a bool: the default false
	retiredText                    // a string: the default retiredKey.text
)

// retiredKey is a hardn.yml key nothing in hardn reads, and Config has no
// field for, with the value its field defaulted to.
type retiredKey struct {
	name string
	kind retiredKind
	text string // retiredText's default
}

// retired are hardn.yml's keys that nothing in hardn reads since the legacy
// package, sources and localization code went (refactor plan, the unit
// after slice 9): the DMZ subnet, the package lists, the repositories,
// useUvPackageManager and the locale settings. Config has no field for
// them (docs/engine/legacy-left.md §2, step 8). A file that sets one
// still loads, and is told so (Retired); a save edits only the keys it
// changes (Save, Edit), so the file keeps it.
var retired = []retiredKey{
	{name: "dmzSubnet", kind: retiredText},
	{name: "linuxCorePackages"}, {name: "linuxDmzPackages"}, {name: "linuxLabPackages"},
	{name: "pythonPackages"}, {name: "nonWslPythonPackages"}, {name: "pythonPipPackages"},
	{name: "alpineCorePackages"}, {name: "alpineDmzPackages"}, {name: "alpineLabPackages"}, {name: "alpinePythonPackages"},
	{name: "debianRepos"}, {name: "proxmoxSrcRepos"}, {name: "proxmoxCephRepo"}, {name: "proxmoxEnterpriseRepo"},
	{name: "proxmoxPackagePatterns"}, {name: "alpineTestingRepo", kind: retiredFlag},
	{name: "useUvPackageManager", kind: retiredFlag},
	{name: "lang", kind: retiredText}, {name: "language", kind: retiredText}, {name: "lcAll", kind: retiredText},
	{name: "tz", kind: retiredText}, {name: "pythonUnbuffered", kind: retiredText, text: "1"},
}

// Retired returns the keys of retired that the file c was read from sets
// to a value other than the old default, in retired's order: what
// UnmarshalYAML found in the file's own keys (readRetired). At the default,
// as origin/main's SaveConfig wrote each into every file it saved, a key
// is hardn's, not the file's, and is not returned.
func (c *Config) Retired() []string { return slices.Clone(c.loaded.retired) }

// readRetired records the keys of retired that m, hardn.yml's top-level
// mapping, sets to a value other than the old default.
func (c *Config) readRetired(m *yaml.Node) {
	set := map[string]bool{}
	for i := 0; i+1 < len(m.Content); i += 2 {
		if k := m.Content[i]; k.Kind == yaml.ScalarNode {
			set[k.Value] = set[k.Value] || !atDefault(k.Value, m.Content[i+1])
		}
	}
	for _, r := range retired {
		if set[r.name] {
			c.loaded.retired = append(c.loaded.retired, r.name)
		}
	}
}

// atDefault reports whether v, the value of the key name, is that key's old
// default; false for a key that is not retired. A null kept the default,
// as it did when the key had a field; an empty list is none.
func atDefault(name string, v *yaml.Node) bool {
	i := slices.IndexFunc(retired, func(r retiredKey) bool { return r.name == name })
	if i < 0 {
		return false
	}
	for v.Kind == yaml.AliasNode && v.Alias != nil {
		v = v.Alias
	}
	if v.Kind == yaml.ScalarNode && v.ShortTag() == "!!null" {
		return true
	}
	switch r := retired[i]; r.kind {
	case retiredList:
		return v.Kind == yaml.SequenceNode && len(v.Content) == 0
	case retiredFlag:
		var b bool
		return v.Kind == yaml.ScalarNode && v.Decode(&b) == nil && !b
	default:
		return v.Kind == yaml.ScalarNode && v.Value == r.text
	}
}
