package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestSaveConfigRetiredLogFile: decision 6 (a). A legacy save, the first-run offer's
// among them, writes logFile only where the file named another path, as it named it, so
// that a person who removed the key does not find it back, and the warning stays until
// they remove it.
func TestSaveConfigRetiredLogFile(t *testing.T) {
	for _, tc := range []struct {
		name, logFile, want string
	}{
		{"the default", "", ""},
		{"hardn's path", "/var/log/hardn.log", ""},
		{"hardn's path, spelled otherwise", "/var/log/../log/hardn.log", ""},
		{"another path", "/srv/log/hardn.log", "logFile: /srv/log/hardn.log\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := DefaultConfig()
			if tc.logFile != "" {
				c.LogFile = tc.logFile
			}
			was := c.LogFile
			name := filepath.Join(t.TempDir(), "hardn.yml")
			if err := SaveConfig(c, name); err != nil {
				t.Fatal(err)
			}
			out, err := os.ReadFile(name)
			if err != nil {
				t.Fatal(err)
			}
			if tc.want == "" && strings.Contains(string(out), "logFile") {
				t.Errorf("saved logFile:\n%s", out)
			}
			if !strings.Contains(string(out), tc.want) {
				t.Errorf("saved:\n%s\nwant %q", out, tc.want)
			}
			if c.LogFile != was {
				t.Errorf("the save changed the session's logFile to %q", c.LogFile)
			}
		})
	}
}
