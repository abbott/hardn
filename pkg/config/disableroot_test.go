package config

import (
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

// TestDisableRoot (refactor plan §3.11, approved 2026-11-04): v0.3.2's
// disableRoot is read as disableRootSSH, with a deprecation naming
// disableRootSSH; with both set, disableRootSSH wins, and the warning
// says so; a value that is not a bool is the bool field's error; a file
// without it records nothing.
func TestDisableRoot(t *testing.T) {
	for _, tc := range []struct {
		name, yaml string
		want       bool
		warning    string // the Deprecation's words; "": none
		err        string
	}{
		{"disableRoot true", "username: alice\ndisableRoot: true\n", true, "disableRoot (line 2) is now disableRootSSH", ""},
		{"disableRoot false", "disableRoot: false\n", false, "disableRoot (line 1) is now disableRootSSH", ""},
		{"a YAML 1.1 spelling", "disableRoot: yes\n", true, "disableRoot (line 1) is now disableRootSSH", ""},
		{"both, disableRootSSH false wins", "disableRoot: true\ndisableRootSSH: false\n", false,
			"disableRoot (line 1) is now disableRootSSH, which this file sets too; disableRootSSH is used", ""},
		{"both, disableRootSSH true wins", "disableRootSSH: true\ndisableRoot: false\n", true,
			"disableRoot (line 2) is now disableRootSSH, which this file sets too; disableRootSSH is used", ""},
		{"disableRootSSH alone", "disableRootSSH: true\n", true, "", ""},
		{"not a bool", "disableRoot: sometimes\n", false, "", "cannot unmarshal !!str `sometimes` into bool"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := DefaultConfig()
			err := yaml.Unmarshal([]byte(tc.yaml), c)
			if tc.err != "" {
				if err == nil || !strings.Contains(err.Error(), tc.err) {
					t.Fatalf("Unmarshal = %v, want %q", err, tc.err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if c.DisableRootSSH != tc.want {
				t.Errorf("DisableRootSSH = %t, want %t", c.DisableRootSSH, tc.want)
			}
			var got []string
			for _, d := range c.Deprecations() {
				got = append(got, d.String())
			}
			if want := []string{tc.warning}; tc.warning == "" && len(got) != 0 || tc.warning != "" && (len(got) != 1 || got[0] != want[0]) {
				t.Errorf("deprecations %q, want %q", got, tc.warning)
			}
		})
	}
}

// TestAutoUpdatesRenamed: enableUnattendedUpgrades is read as
// enableAutoUpdates, as disableRoot is (renamed), through a merge key too;
// with both set, enableAutoUpdates wins; with both old names set, two
// warnings.
func TestAutoUpdatesRenamed(t *testing.T) {
	for _, tc := range []struct {
		name, yaml string
		want       bool
		warnings   []string
	}{
		{"old name true", "username: alice\nenableUnattendedUpgrades: true\n", true,
			[]string{"enableUnattendedUpgrades (line 2) is now enableAutoUpdates"}},
		{"both, enableAutoUpdates false wins", "enableUnattendedUpgrades: true\nenableAutoUpdates: false\n", false,
			[]string{"enableUnattendedUpgrades (line 1) is now enableAutoUpdates, which this file sets too; enableAutoUpdates is used"}},
		{"enableAutoUpdates alone", "enableAutoUpdates: true\n", true, nil},
		{"both old names", "enableUnattendedUpgrades: yes\ndisableRoot: true\n", true,
			[]string{"disableRoot (line 2) is now disableRootSSH", "enableUnattendedUpgrades (line 1) is now enableAutoUpdates"}},
		{"old name by a merge key", "base: &b {enableUnattendedUpgrades: true}\n<<: *b\n", true,
			[]string{"enableUnattendedUpgrades (line 1) is now enableAutoUpdates"}},
		{"enableAutoUpdates by a merge key wins", "base: &b {enableAutoUpdates: false}\n<<: *b\nenableUnattendedUpgrades: true\n", false,
			[]string{"enableUnattendedUpgrades (line 3) is now enableAutoUpdates, which this file sets too; enableAutoUpdates is used"}},
		{"own key wins over a merged one", "base: &b {enableUnattendedUpgrades: false}\n<<: *b\nenableUnattendedUpgrades: true\n", true,
			[]string{"enableUnattendedUpgrades (line 3) is now enableAutoUpdates"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := DefaultConfig()
			if err := yaml.Unmarshal([]byte(tc.yaml), c); err != nil {
				t.Fatal(err)
			}
			if c.EnableAutoUpdates != tc.want {
				t.Errorf("EnableAutoUpdates = %t, want %t", c.EnableAutoUpdates, tc.want)
			}
			var got []string
			for _, d := range c.Deprecations() {
				got = append(got, d.String())
			}
			if strings.Join(got, "\n") != strings.Join(tc.warnings, "\n") {
				t.Errorf("deprecations %q, want %q", got, tc.warnings)
			}
		})
	}
	if c := DefaultConfig(); yaml.Unmarshal([]byte("enableUnattendedUpgrades: sometimes\n"), c) == nil {
		t.Error("enableUnattendedUpgrades: sometimes read with no error")
	}
}

// TestAutoUpdatesRenamedEdit: Edit of enableAutoUpdates on a file with
// only the old name adds the new key, which wins, so false reads back.
func TestAutoUpdatesRenamedEdit(t *testing.T) {
	c := load(t, "enableUnattendedUpgrades: true\n")
	c.EnableAutoUpdates = false
	out, err := Edit([]byte("enableUnattendedUpgrades: true\n"), c, autoUpdatesKey)
	if err != nil {
		t.Fatal(err)
	}
	if back := load(t, string(out)); back.EnableAutoUpdates || !strings.Contains(string(out), "enableAutoUpdates: false") {
		t.Errorf("Edit wrote %q, which reads back %t", out, back.EnableAutoUpdates)
	}
}
