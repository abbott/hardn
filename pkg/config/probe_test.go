package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestLoadConfigRunsNoProbe: LoadConfig runs nothing and prints no notice
// under sudo with HARDN_CONFIG dropped (refactor plan slice 9, unit (d),
// decision 4; side-effects row 5): where origin/main ran su - <SUDO_USER>
// -c "echo $HARDN_CONFIG" from root on every load and, the user's login
// shell printing anything, said "NOTICE: The HARDN_CONFIG environment
// variable is set in your user environment …", frozen in
// pkg/screens/environment/testdata/legacy/environment/notice_*. A su on
// PATH that records being run, and prints the path as alice's profile
// would, is not run, and the file named is loaded.
func TestLoadConfigRunsNoProbe(t *testing.T) {
	dir := t.TempDir()
	ran := filepath.Join(dir, "su-ran")
	su := "#!/bin/sh\necho \"$@\" >> " + ran + "\necho /home/alice/hardn.yml\n"
	if err := os.WriteFile(filepath.Join(dir, "su"), []byte(su), 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "hardn.yml")
	if err := os.WriteFile(path, []byte("username: alice\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	t.Setenv("HOME", dir)
	t.Setenv("SUDO_UID", "1000")
	t.Setenv("SUDO_USER", "alice")
	t.Setenv("HARDN_CONFIG", "")
	var cfg *Config
	var err error
	out, _, ended := ask(t, func() { cfg, err = LoadConfig(path) })
	if err != nil || ended || cfg.Username != "alice" {
		t.Fatalf("LoadConfig(%s) = %+v, %v (ended %v)", path, cfg, err, ended)
	}
	if _, serr := os.Stat(ran); serr == nil {
		b, _ := os.ReadFile(ran)
		t.Errorf("su ran: %s", b)
	}
	for _, w := range []string{"NOTICE", "setup-sudo-env", "not preserved"} {
		if strings.Contains(out, w) {
			t.Errorf("LoadConfig printed %q:\n%s", w, out)
		}
	}
}
