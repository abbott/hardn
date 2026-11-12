package config

import (
	"fmt"
	"path/filepath"
	"slices"

	"go.yaml.in/yaml/v3"
)

// What the file itself said, key by key, for a reader that shows hardn.yml
// (pkg/screens/config): which top-level keys it gives a value, so that a
// value is told from a default without decoding the file again.

// FileSets reports whether the file c was read from gives the top-level key
// a value: false for a key it leaves out or leaves null, which keeps the
// default as the loader reads it, and for a Config read from no file.
// disableRootSSH is set by its old name, disableRoot, too (readDisableRoot).
// After Save it is the file as saved.
func (c *Config) FileSets(key string) bool {
	if _, ok := c.loaded.set[key]; ok {
		return true
	}
	if _, old := c.loaded.set[disableRootKey]; key != disableRootSSHKey || !old {
		return false
	}
	// A disableRootSSH the file has, null too, shadows the old name.
	return !slices.ContainsFunc(c.loaded.deprecations, func(d Deprecation) bool { return d.Key == disableRootKey && d.Shadowed })
}

// FileWord is the value the file gives the top-level key where it is one
// string, bool or whole number, as yaml.v3 reads it; "" for any other value,
// a key not set (FileSets) and an empty string.
func (c *Config) FileWord(key string) string { return c.loaded.set[key] }

// FileUsername is username as the file gave it, as the loader read it (a
// 0755 is "0755", where FileWord's is the number); "" for none. Username is
// the session's, which -u sets too. Save does not write the key.
func (c *Config) FileUsername() string { return c.loaded.username }

// OnSearchPath reports whether path, absolute, is one of the places hardn
// looks for hardn.yml when neither -f nor HARDN_CONFIG names it.
func OnSearchPath(path string) bool {
	return slices.ContainsFunc(defaultSearchPath(), func(p string) bool {
		abs, err := filepath.Abs(p)
		return err == nil && abs == path
	})
}

// readSet is m's keys that have a value, hardn.yml's top-level mapping, each
// with its word (FileWord); the last of a repeated key wins. A merge key's
// keys ("<<": a mapping, an alias to one, or a sequence of them) count as
// the decoder reads them: a key m has itself wins, a null too, then the
// first mapping merged that has it.
func readSet(m *yaml.Node) map[string]string {
	set := map[string]string{}
	mergeSet(m, set, map[string]bool{}, map[*yaml.Node]bool{})
	return set
}

// mergeSet adds m's keys to set, then those of the mappings m merges, but
// for the keys in seen, which a mapping read before m has. in holds the
// mappings being read, so that one that merges itself ends.
func mergeSet(m *yaml.Node, set map[string]string, seen map[string]bool, in map[*yaml.Node]bool) {
	if m.Kind == yaml.AliasNode && m.Alias != nil {
		m = m.Alias
	}
	if m.Kind != yaml.MappingNode || in[m] {
		return
	}
	in[m] = true
	defer delete(in, m)
	var merge *yaml.Node
	own := map[string]bool{}
	for i := 0; i+1 < len(m.Content); i += 2 {
		k, v := m.Content[i], m.Content[i+1]
		if k.Kind != yaml.ScalarNode {
			continue
		}
		if k.Value == "<<" && (k.Tag == "" || k.Tag == "!" || k.ShortTag() == "!!merge") { // yaml.v3's isMerge
			merge = v
			continue
		}
		if seen[k.Value] && !own[k.Value] {
			continue
		}
		seen[k.Value], own[k.Value] = true, true
		delete(set, k.Value)
		var val any
		if err := v.Decode(&val); err != nil || val == nil {
			continue
		}
		set[k.Value] = ""
		switch val.(type) {
		case string, bool, int:
			set[k.Value] = fmt.Sprint(val)
		}
	}
	switch {
	case merge == nil:
	case merge.Kind == yaml.SequenceNode:
		for _, n := range merge.Content {
			mergeSet(n, set, seen, in)
		}
	default:
		mergeSet(merge, set, seen, in)
	}
}

// setOf is readSet of data, a hardn.yml's bytes; nil where they hold no
// mapping.
func setOf(data []byte) map[string]string {
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil || len(doc.Content) != 1 || doc.Content[0].Kind != yaml.MappingNode {
		return nil
	}
	return readSet(doc.Content[0])
}

// FileDryRun is dryRun as the file gave it, false for none: the key's value,
// where DryRun is the session's mode, which -n sets too (ResolveDryRun).
func (c *Config) FileDryRun() bool { return c.loaded.dryRun }
