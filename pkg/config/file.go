package config

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"

	"go.yaml.in/yaml/v3"

	"github.com/abbott/hardn/pkg/host"
)

// hardn.yml through host.System, for the plan/apply engine's save of it
// (refactor plan §3.19; slice 2, next/s2-control): the file read as
// LoadConfig reads it, and whether its bytes are still the ones loaded.
// The save edits them in place (Edit, next/s2-config-edit).

// ErrInvalidFile: hardn.yml's bytes do not decode as the configuration
// (Read).
var ErrInvalidFile = errors.New("hardn.yml does not decode as the configuration")

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
// it finds: the defaults, then the file's keys (UnmarshalYAML). A symlink
// is followed, as LoadConfig
// follows it (Save, which writes no symlink, refuses one). path must be
// absolute and clean (fs.ErrInvalid otherwise); it is recorded as where the
// configuration was loaded from (Path, Save), with the bytes read
// (LoadedFrom). Bytes that do not decode are ErrInvalidFile.
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
		return nil, fmt.Errorf("config: %s: %w: %w", path, ErrInvalidFile, err)
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
