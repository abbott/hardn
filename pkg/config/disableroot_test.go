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
