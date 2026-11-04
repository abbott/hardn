package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// loadFile writes data to hardn.yml in a temporary directory and loads it
// as hardn does with -f naming it.
func loadFile(t *testing.T, data string) *Config {
	t.Helper()
	name := filepath.Join(t.TempDir(), "hardn.yml")
	if err := os.WriteFile(name, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := LoadConfigWithEnvPriority(name)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// TestResolveDryRun: §3.19's precedence, a flag given winning, true or
// false, then the file's dryRun as loaded, then live; plan only with its
// source, live with none (decision 2).
func TestResolveDryRun(t *testing.T) {
	for _, tt := range []struct {
		name        string
		file        string // hardn.yml; "-" for none loaded
		given, flag bool
		want        bool
		source      DryRunSource
	}{
		{"no flag, no file", "-", false, false, false, DryRunDefault},
		{"no flag, no key", "username: alice\n", false, false, false, DryRunDefault},
		{"no flag, file false", "dryRun: false\n", false, false, false, DryRunDefault},
		{"no flag, file true", "dryRun: true\n", false, false, true, DryRunFile},
		{"no flag, file yes (YAML 1.1)", "dryRun: yes\n", false, false, true, DryRunFile},
		{"-n, no file", "-", true, true, true, DryRunFlag},
		{"-n, file false", "dryRun: false\n", true, true, true, DryRunFlag},
		{"--dry-run=false, file true", "dryRun: true\n", true, false, false, DryRunDefault},
	} {
		t.Run(tt.name, func(t *testing.T) {
			c := DefaultConfig()
			if tt.file != "-" {
				c = loadFile(t, tt.file)
			}
			c.ResolveDryRun(tt.given, tt.flag)
			if c.DryRun != tt.want || c.DryRunSource() != tt.source {
				t.Errorf("DryRun %v from %d, want %v from %d", c.DryRun, c.DryRunSource(), tt.want, tt.source)
			}
		})
	}
}

// TestDryRunSourceOnlyAsSet: the source does not outlive a write to
// DryRun, the file's or the flag's.
func TestDryRunSourceOnlyAsSet(t *testing.T) {
	c := loadFile(t, "dryRun: true\n")
	c.ResolveDryRun(false, false)
	c.DryRun = false
	if c.DryRunSource() != DryRunDefault {
		t.Errorf("a write to DryRun kept the file's source %d", c.DryRunSource())
	}
	c.ResolveDryRun(true, true)
	c.DryRun = false
	if c.DryRunSource() != DryRunDefault {
		t.Errorf("a write to DryRun kept the flag's source %d", c.DryRunSource())
	}
}

// TestSaveConfigDryRunAsLoaded: decision 8. A legacy save writes dryRun as
// the file gave it, never the session's mode: not -n's, not
// --dry-run=false's, not one written to DryRun after the start; the
// defaults' false when no file was loaded.
func TestSaveConfigDryRunAsLoaded(t *testing.T) {
	for _, tt := range []struct {
		name    string
		file    string // "-" for none loaded
		session func(*Config)
		want    string
	}{
		{"-n, file false", "dryRun: false\n", func(c *Config) { c.ResolveDryRun(true, true) }, "dryRun: false\n"},
		{"-n, no file", "-", func(c *Config) { c.ResolveDryRun(true, true) }, "dryRun: false\n"},
		{"--dry-run=false, file true", "dryRun: true\n", func(c *Config) { c.ResolveDryRun(true, false) }, "dryRun: true\n"},
		{"file true, written off", "dryRun: true\n", func(c *Config) { c.ResolveDryRun(false, false); c.DryRun = false }, "dryRun: true\n"},
		{"file false, written on", "dryRun: false\n", func(c *Config) { c.ResolveDryRun(false, false); c.DryRun = true }, "dryRun: false\n"},
		{"no file, written on", "-", func(c *Config) { c.ResolveDryRun(false, false); c.DryRun = true }, "dryRun: false\n"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			c := DefaultConfig()
			if tt.file != "-" {
				c = loadFile(t, tt.file)
			}
			tt.session(c)
			session := c.DryRun
			name := filepath.Join(t.TempDir(), "hardn.yml")
			if err := SaveConfig(c, name); err != nil {
				t.Fatal(err)
			}
			out, err := os.ReadFile(name)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(out), "\n"+tt.want) {
				t.Errorf("saved:\n%s\nwant %q", out, tt.want)
			}
			if c.DryRun != session {
				t.Errorf("the save changed the session's mode to %v", c.DryRun)
			}
		})
	}
}

// TestSaveOfASettingKeepsDryRun: decision 8 for the new saves. A save of
// the settings a control names (Edit, which steps.PlanSave and Save use)
// writes those alone: a session's mode differing from the file's dryRun is
// not written.
func TestSaveOfASettingKeepsDryRun(t *testing.T) {
	for _, in := range []string{"dryRun: true\nnameservers: [192.0.2.1]\n", "nameservers: [192.0.2.1]\n"} {
		c := load(t, in)
		c.ResolveDryRun(true, !c.DryRun) // -n, or --dry-run=false over a true
		c.Nameservers = []string{"192.0.2.53"}
		out, err := Edit([]byte(in), c, "nameservers")
		if err != nil {
			t.Fatal(err)
		}
		if want := strings.Replace(in, "192.0.2.1", "192.0.2.53", 1); string(out) != want {
			t.Errorf("Edit of nameservers on %q:\n%s\nwant\n%s", in, out, want)
		}
	}
}
