package config

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

// TestLoadExisting: setup-sudo-env's load (refactor plan slice 9, unit
// (d)). A file found is read as LoadConfig reads it; none found is the
// defaults, with no question asked and nothing written; a file -f or
// HARDN_CONFIG names that is not there is fs.ErrNotExist, with no question
// either.
func TestLoadExisting(t *testing.T) {
	if _, err := os.Stat("/etc/hardn/hardn.yml"); err == nil {
		t.Skip("/etc/hardn/hardn.yml exists on this machine: the search path would find it")
	}
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("HARDN_CONFIG", "")
	t.Chdir(dir)

	var cfg *Config
	var err error
	out, _, ended := ask(t, func() { cfg, err = LoadExisting("") })
	if err != nil || ended || out != "" || cfg.Username != DefaultConfig().Username {
		t.Fatalf("none found: %+v, %v (ended %v), said %q", cfg, err, ended, out)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Errorf("none found: wrote %v", entries)
	}

	path := filepath.Join(dir, "hardn.yml")
	if err := os.WriteFile(path, []byte("username: alice\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, give := range []string{path, ""} { // -f, then ./hardn.yml found on the search path
		if cfg, err = LoadExisting(give); err != nil || cfg.Username != "alice" || cfg.Path() != path && give != "" {
			t.Errorf("LoadExisting(%q) = %+v, %v", give, cfg, err)
		}
	}

	missing := filepath.Join(dir, "missing.yml")
	out, _, ended = ask(t, func() { cfg, err = LoadExisting(missing) })
	if !errors.Is(err, fs.ErrNotExist) || ended || out != "" {
		t.Errorf("-f missing: %+v, %v (ended %v), said %q", cfg, err, ended, out)
	}
	t.Setenv("HARDN_CONFIG", missing)
	out, _, ended = ask(t, func() { cfg, err = LoadExisting("") })
	if !errors.Is(err, fs.ErrNotExist) || ended || out != "" {
		t.Errorf("HARDN_CONFIG missing: %+v, %v (ended %v), said %q", cfg, err, ended, out)
	}
}
