package config

import (
	"reflect"
	"slices"
	"strings"

	"go.yaml.in/yaml/v3"
)

// retired are hardn.yml's keys that nothing in hardn reads since the legacy
// package, sources and localization code went (refactor plan, the unit after
// slice 9, part 1): the DMZ subnet, the package lists, the repositories,
// useUvPackageManager and the locale settings. They still load, so that a
// file that sets one is told so (Retired) rather than refused, and a save
// writes one only where the file gave it a value other than the default
// (SaveConfig).
var retired = []string{
	"dmzSubnet",
	"linuxCorePackages", "linuxDmzPackages", "linuxLabPackages",
	"pythonPackages", "nonWslPythonPackages", "pythonPipPackages",
	"alpineCorePackages", "alpineDmzPackages", "alpineLabPackages", "alpinePythonPackages",
	"debianRepos", "proxmoxSrcRepos", "proxmoxCephRepo", "proxmoxEnterpriseRepo",
	"proxmoxPackagePatterns", "alpineTestingRepo",
	"useUvPackageManager",
	"lang", "language", "lcAll", "tz", "pythonUnbuffered",
}

// Retired returns the keys of retired that c holds a value other than
// DefaultConfig's for, in the configuration's order. An empty list and
// none are the same value.
func (c *Config) Retired() []string {
	d := reflect.ValueOf(DefaultConfig()).Elem()
	v := reflect.ValueOf(c).Elem()
	var keys []string
	t := v.Type()
	for i := range t.NumField() {
		name, _, _ := strings.Cut(t.Field(i).Tag.Get("yaml"), ",")
		if slices.Contains(retired, name) && !sameValue(v.Field(i), d.Field(i)) {
			keys = append(keys, name)
		}
	}
	return keys
}

// sameValue reports whether a and b are equal, an empty slice equal to nil.
func sameValue(a, b reflect.Value) bool {
	if a.Kind() == reflect.Slice && a.Len() == 0 && b.Len() == 0 {
		return true
	}
	return reflect.DeepEqual(a.Interface(), b.Interface())
}

// withoutRetired is n, a configuration's YAML mapping, without each key of
// retired that c holds at its default (Retired): what SaveConfig writes.
func withoutRetired(n *yaml.Node, c *Config) {
	set := c.Retired()
	var kept []*yaml.Node
	for i := 0; i+1 < len(n.Content); i += 2 {
		k := n.Content[i].Value
		if slices.Contains(retired, k) && !slices.Contains(set, k) {
			continue
		}
		kept = append(kept, n.Content[i], n.Content[i+1])
	}
	n.Content = kept
}
