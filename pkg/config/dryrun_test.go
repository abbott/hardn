package config

import (
	"errors"
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
// false, then the file's dryRun as loaded, then live; each with its source.
func TestResolveDryRun(t *testing.T) {
	for _, tt := range []struct {
		name        string
		file        string // hardn.yml; "-" for none loaded
		given, flag bool
		want        bool
		source      DryRunSource
	}{
		{"no flag, no file", "-", false, false, false, DryRunDefault},
		{"no flag, no key", "username: alice\n", false, false, false, DryRunFile},
		{"no flag, file false", "dryRun: false\n", false, false, false, DryRunFile},
		{"no flag, file true", "dryRun: true\n", false, false, true, DryRunFile},
		{"no flag, file yes (YAML 1.1)", "dryRun: yes\n", false, false, true, DryRunFile},
		{"-n, no file", "-", true, true, true, DryRunFlag},
		{"-n, file false", "dryRun: false\n", true, true, true, DryRunFlag},
		{"--dry-run=false, file true", "dryRun: true\n", true, false, false, DryRunFlag},
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

// TestDryRunSourceOnlyAsSet: a toggle inside the session (SetDryRun) sets
// no source, and the source does not come back with the value it set; nor
// does it outlive a write to DryRun.
func TestDryRunSourceOnlyAsSet(t *testing.T) {
	c := loadFile(t, "dryRun: true\n")
	c.ResolveDryRun(false, false)
	c.SetDryRun(false)
	if c.DryRun || c.DryRunSource() != DryRunDefault {
		t.Fatalf("toggled off: %v from %d", c.DryRun, c.DryRunSource())
	}
	c.SetDryRun(true)
	if !c.DryRun || c.DryRunSource() != DryRunDefault {
		t.Errorf("toggled on again: %v from %d, want no source", c.DryRun, c.DryRunSource())
	}
	c.ResolveDryRun(true, true)
	c.DryRun = false
	if c.DryRunSource() != DryRunDefault {
		t.Errorf("a write to DryRun kept the source %d", c.DryRunSource())
	}
}

// TestSaveConfigDryRunAsLoaded: decision 8. A legacy save writes dryRun as
// the file gave it, never the session's mode: not -n's, not
// --dry-run=false's, not a toggle's (item 6, Run All's option 2); the
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
		{"file true, toggled off", "dryRun: true\n", func(c *Config) { c.ResolveDryRun(false, false); c.SetDryRun(false) }, "dryRun: true\n"},
		{"file false, toggled on", "dryRun: false\n", func(c *Config) { c.ResolveDryRun(false, false); c.SetDryRun(true) }, "dryRun: false\n"},
		{"no file, toggled on", "-", func(c *Config) { c.ResolveDryRun(false, false); c.SetDryRun(true) }, "dryRun: false\n"},
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

// TestFileDryRun: the file LoadConfig reads for -f or HARDN_CONFIG, read
// quietly, and its dryRun; none for a path that is not there; a file that
// does not load is an error. The search path's other files are not read
// here: they are this machine's.
func TestFileDryRun(t *testing.T) {
	t.Setenv("HARDN_CONFIG", "")
	dir := t.TempDir()
	write := func(name, data string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(data), 0o600); err != nil {
			t.Fatal(err)
		}
		return p
	}
	on, off := write("on.yml", "dryRun: true\n"), write("off.yml", "username: alice\n")
	bad := write("bad.yml", "dryRun: [\n")
	rootLogin := write("pp.yml", "permitRootLogin: prohibit-password\ndryRun: true\n")
	for _, tt := range []struct {
		explicit, env string
		path          string
		want          bool
		err           bool
	}{
		{explicit: on, path: on, want: true},
		{explicit: off, path: off},
		{env: on, path: on, want: true},
		{explicit: off, env: on, path: off},
		{explicit: filepath.Join(dir, "none.yml")},
		{env: filepath.Join(dir, "none.yml")},
		{explicit: bad, err: true},
		{explicit: rootLogin, err: true},
	} {
		t.Setenv("HARDN_CONFIG", tt.env)
		path, got, err := FileDryRun(tt.explicit)
		if path != tt.path || got != tt.want || (err != nil) != tt.err {
			t.Errorf("-f %q, HARDN_CONFIG %q: %q, %v, %v; want %q, %v, error %v", tt.explicit, tt.env, path, got, err, tt.path, tt.want, tt.err)
		}
		if tt.explicit == rootLogin && !errors.Is(err, ErrRootLoginNotApplied) {
			t.Errorf("prohibit-password: %v, want the loader's error", err)
		}
	}
}
