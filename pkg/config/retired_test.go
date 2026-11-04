package config

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/hostfake"
)

// TestRetired: Retired names each retired key the file sets to a value
// other than its old default, in retired's order, read from the file's own
// keys: at the default, as origin/main's SaveConfig wrote it, null, or an
// empty list, none; a value the old field would have refused is set, and
// the file still loads.
func TestRetired(t *testing.T) {
	if keys := DefaultConfig().Retired(); keys != nil {
		t.Errorf("the defaults: %v", keys)
	}
	for _, tt := range []struct {
		yml  string
		want []string
	}{
		{"username: alice\n", nil},
		{"dmzSubnet: \"\"\nlinuxCorePackages: []\npythonPackages:\nalpineTestingRepo: false\nuseUvPackageManager: off\n" +
			"lang: \"\"\ntz:\npythonUnbuffered: \"1\"\n", nil},
		{"pythonUnbuffered: 1\n", nil},
		{"tz: UTC\ndmzSubnet: 192.0.2\npythonUnbuffered: \"\"\nalpineTestingRepo: true\n" +
			"debianRepos:\n  - deb http://192.0.2.1/debian trixie main\n",
			[]string{"dmzSubnet", "debianRepos", "alpineTestingRepo", "tz", "pythonUnbuffered"}},
		{"base: &b [git]\nlinuxCorePackages: *b\n", []string{"linuxCorePackages"}},
		{"tz: [UTC]\nuseUvPackageManager: maybe\n", []string{"useUvPackageManager", "tz"}},
	} {
		if keys := load(t, tt.yml).Retired(); !slices.Equal(keys, tt.want) {
			t.Errorf("%q: Retired = %v, want %v", tt.yml, keys, tt.want)
		}
	}
}

// TestSaveKeepsRetired: a save edits only the settings it changes, so a
// retired key the file sets stays in it, its line as it was, and still
// warns when the file is read again.
func TestSaveKeepsRetired(t *testing.T) {
	ctx := context.Background()
	const path = "/etc/hardn/hardn.yml"
	data := "username: alice\n# old\ndmzSubnet: 192.0.2\ntz: UTC  # local\nnameservers:\n  - 192.0.2.53\n"
	sys := hostfake.New(host.Options{})
	sys.AddFile(path, []byte(data), 0o600, 0, 0)
	c, err := Read(ctx, sys, path)
	if err != nil {
		t.Fatal(err)
	}
	c.Nameservers = []string{"192.0.2.54"}
	if err := Save(ctx, sys, c); err != nil {
		t.Fatal(err)
	}
	saved, _ := sys.Entry(path)
	if want := strings.Replace(data, "192.0.2.53", "192.0.2.54", 1); string(saved.Data) != want {
		t.Errorf("saved:\n%s\nwant:\n%s", saved.Data, want)
	}
	again, err := Read(ctx, sys, path)
	if err != nil || !slices.Equal(again.Retired(), []string{"dmzSubnet", "tz"}) {
		t.Errorf("read again: Retired %v, %v", again.Retired(), err)
	}
}

// TestSaveConfigWritesNoRetired: the first-run save writes no retired key.
func TestSaveConfigWritesNoRetired(t *testing.T) {
	name := filepath.Join(t.TempDir(), "hardn.yml")
	if err := SaveConfig(DefaultConfig(), name); err != nil {
		t.Fatal(err)
	}
	out, err := os.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range retired {
		if strings.Contains("\n"+string(out), "\n"+r.name+":") {
			t.Errorf("the defaults' save writes %s:\n%s", r.name, out)
		}
	}
	if !strings.Contains(string(out), "\nsshPort: 22\n") {
		t.Errorf("the save lost the other keys:\n%s", out)
	}
}
