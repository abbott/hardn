package config

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"path/filepath"

	"go.yaml.in/yaml/v3"

	"github.com/abbott/hardn/pkg/host"
)

// hardn.yml through host.System, for the plan/apply engine's save of it
// (refactor plan §3.19; slice 2, next/s2-control): the file read as
// LoadConfig reads it, whether its bytes are still the ones loaded, and
// whether saving it loses comments.

// setLoaded records name, made absolute, as where c was loaded from, and
// data as the bytes read there (LoadedFrom).
func (c *Config) setLoaded(name string, data []byte) {
	c.setPath(name)
	c.loaded.sum = sum(data)
}

// sum is data's SHA-256, in hex.
func sum(data []byte) string {
	s := sha256.Sum256(data)
	return hex.EncodeToString(s[:])
}

// Read loads hardn.yml from path through sys, as LoadConfig loads the file
// it finds: the defaults, then the file's keys (UnmarshalYAML), and
// prohibit-password refused while this build's legacy SSH code would apply
// it as "no" (ErrRootLoginNotApplied). A symlink is followed, as LoadConfig
// follows it (Save, which writes no symlink, refuses one). path must be
// absolute and clean (fs.ErrInvalid otherwise); it is recorded as where the
// configuration was loaded from (Path, Save), with the bytes read
// (LoadedFrom).
func Read(ctx context.Context, sys host.System, path string) (*Config, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return nil, fmt.Errorf("config: %q: not an absolute, clean path: %w", path, fs.ErrInvalid)
	}
	data, err := sys.ReadFile(ctx, path)
	if err != nil {
		return nil, fmt.Errorf("config: %w", err)
	}
	c := DefaultConfig()
	if err := yaml.Unmarshal(data, c); err != nil {
		return nil, fmt.Errorf("config: %s: %w", path, err)
	}
	if err := checkLoaded(c); err != nil {
		return nil, fmt.Errorf("config: %s: %w", path, err)
	}
	c.setLoaded(path, data)
	return c, nil
}

// LoadedFrom reports whether data are the bytes c was loaded from (LoadConfig,
// Read) or Save last wrote; false when c records none. A save that finds
// other bytes at Path would undo whatever changed them.
func (c *Config) LoadedFrom(data []byte) bool {
	return c.loaded.sum != "" && c.loaded.sum == sum(data)
}

// HasComments reports whether data, hardn.yml's bytes, hold a comment,
// which Save does not keep: it writes the configuration's keys alone
// (§3.19). Data that is not YAML count as commented when a line starts
// with "#", so that the warning errs towards being given.
func HasComments(data []byte) bool {
	var n yaml.Node
	if err := yaml.Unmarshal(data, &n); err != nil || n.Kind == 0 {
		// Not YAML, or no node at all, as for a file of comments only.
		for _, l := range bytes.Split(data, []byte("\n")) {
			if bytes.HasPrefix(bytes.TrimSpace(l), []byte("#")) {
				return true
			}
		}
		return false
	}
	return commented(&n)
}

// commented reports whether n, or a node under it, holds a comment.
func commented(n *yaml.Node) bool {
	if n.HeadComment != "" || n.LineComment != "" || n.FootComment != "" {
		return true
	}
	for _, k := range n.Content {
		if commented(k) {
			return true
		}
	}
	return false
}
