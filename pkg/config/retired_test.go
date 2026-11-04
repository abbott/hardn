package config

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// TestRetired: Retired names each retired key c holds a value other than
// the default for, in the configuration's order; an empty list is the
// default's none.
func TestRetired(t *testing.T) {
	if keys := DefaultConfig().Retired(); keys != nil {
		t.Errorf("the defaults: %v", keys)
	}
	c := DefaultConfig()
	c.LinuxCorePackages, c.PythonPackages = nil, []string{}
	if keys := c.Retired(); keys != nil {
		t.Errorf("empty lists: %v", keys)
	}
	c.DmzSubnet, c.Tz, c.PythonUnbuffered, c.AlpineTestingRepo = "192.0.2", "UTC", "", true
	c.DebianRepos = []string{"deb http://192.0.2.1/debian trixie main"}
	want := []string{"dmzSubnet", "debianRepos", "alpineTestingRepo", "tz", "pythonUnbuffered"}
	if keys := c.Retired(); !slices.Equal(keys, want) {
		t.Errorf("Retired = %v, want %v", keys, want)
	}
}

// TestSaveConfigLeavesRetired: the first-run save writes no retired key at
// its default, and writes back one the file set, so that its warning stays.
func TestSaveConfigLeavesRetired(t *testing.T) {
	save := func(c *Config) string {
		t.Helper()
		name := filepath.Join(t.TempDir(), "hardn.yml")
		if err := SaveConfig(c, name); err != nil {
			t.Fatal(err)
		}
		out, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		return string(out)
	}
	out := save(DefaultConfig())
	for _, key := range retired {
		if strings.Contains(out, "\n"+key+":") || strings.HasPrefix(out, key+":") {
			t.Errorf("the defaults' save writes %s:\n%s", key, out)
		}
	}
	if !strings.Contains(out, "\nsshPort: 22\n") {
		t.Errorf("the save lost the other keys:\n%s", out)
	}
	c := DefaultConfig()
	c.Tz = "UTC"
	if out := save(c); !strings.Contains(out, "\ntz: UTC\n") || strings.Contains(out, "\nlang:") {
		t.Errorf("a set tz is not written back alone:\n%s", out)
	}
}
