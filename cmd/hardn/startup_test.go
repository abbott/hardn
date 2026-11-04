package main

import (
	"errors"
	"strings"
	"testing"

	"github.com/abbott/hardn/pkg/config"
	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/hostfake"
)

// TestConfigLine: which hardn.yml was read is said once, on stderr, for
// the flags and setup-sudo-env, in FindConfigFile's words as they were,
// with no [INFO]; an -f or HARDN_CONFIG file that is not there is an
// error; the defaults, the offer to create one declined, are said after
// it; nothing is said before the main menu (decision 4 (a) of
// docs/engine/legacy-left.md). The OS check fails each run, ending it there.
func TestConfigLine(t *testing.T) {
	const yml = "/etc/hardn/hardn.yml"
	for _, tc := range []struct {
		name  string
		args  []string
		found config.Found
		want  string
	}{
		{"the search path, -g", []string{"-g"}, config.Found{Path: yml, By: config.FoundSearch, Exists: true}, "Using configuration from: " + yml},
		{"-f, -w", []string{"-w", "-f", yml}, config.Found{Path: yml, By: config.FoundFlag, Exists: true}, "Using configuration from command-line flag: " + yml},
		{"HARDN_CONFIG, -d", []string{"-d"}, config.Found{Path: yml, By: config.FoundEnv, Exists: true}, "Using configuration from HARDN_CONFIG environment variable: " + yml},
		{"-f not there, -c", []string{"-c", "-u", "carol", "-f", yml}, config.Found{Path: yml, By: config.FoundFlag},
			"Operation Failed Configuration file specified by command-line flag not found: " + yml + " Using default configuration (no config file found)"},
		{"HARDN_CONFIG not there, -r", []string{"-r"}, config.Found{Path: yml, By: config.FoundEnv},
			"Operation Failed Configuration file specified by HARDN_CONFIG environment variable not found: " + yml + " Using default configuration (no config file found)"},
		{"none, -e", []string{"-e"}, config.Found{}, "No configuration file found in any location Using default configuration (no config file found)"},
		{"the main menu", nil, config.Found{Path: yml, By: config.FoundSearch, Exists: true}, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := runHardnWith(t, asRoot, func() {
				findConfig = func(string) config.Found { return tc.found }
				detectOS = func() error { return errors.New("test: the OS check") }
			}, tc.args...)
			got := strings.TrimSuffix(flat(r.stderr), "Operation Failed Failed to detect OS: test: the OS check")
			if !r.ended || r.code != 1 || strings.TrimSpace(got) != tc.want || r.stdout != "" {
				t.Errorf("ended %v with %d; stderr %q, want %q; stdout %q", r.ended, r.code, got, tc.want, r.stdout)
			}
		})
	}
	t.Run("setup-sudo-env", func(t *testing.T) {
		r := runHardnWith(t, asRoot, func() {
			findConfig = func(string) config.Found { return config.Found{Path: yml, By: config.FoundSearch, Exists: true} }
			loadExisting = func(string) (*config.Config, error) { return nil, errors.New("test: past the line") }
		}, "setup-sudo-env")
		if got := flat(r.stderr); !r.ended || r.code != 1 || got != "Using configuration from: "+yml+" Operation Failed Failed to load configuration: test: past the line" {
			t.Errorf("ended %v with %d; stderr %q", r.ended, r.code, got)
		}
	})
}

// TestHostOS: the OS check reads os-release through pkg/host/detect,
// refuses none and an OS other than Debian, Ubuntu or Alpine, and writes nothing: no clear, no ~ line,
// no "detected" line (decision 4 (a)).
func TestHostOS(t *testing.T) {
	was := osHost
	t.Cleanup(func() { osHost = was })
	for _, tc := range []struct {
		name, release string
		err           string
	}{
		{"debian", "ID=debian\nVERSION_ID=\"13\"\nVERSION_CODENAME=trixie\n", ""},
		{"ubuntu", "ID=ubuntu\nVERSION_ID=\"24.04\"\nVERSION_CODENAME=noble\n", ""},
		{"alpine", "ID=alpine\nVERSION_ID=3.22.1\n", ""},
		{"another OS", "ID=arch\n", "unsupported OS type detected: arch"},
		{"no os-release", "", "cannot detect OS type: "},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := hostfake.New(host.Options{})
			if tc.release != "" {
				f.AddFile("/etc/os-release", []byte(tc.release), 0o644, 0, 0)
			}
			osHost = func() host.System { return f }
			var err error
			stdout, stderr := captureStreams(t, func() { err = hostOS() })
			switch {
			case tc.err == "" && err != nil:
				t.Errorf("hostOS() = %v; want nil", err)
			case tc.err != "" && (err == nil || !strings.HasPrefix(err.Error(), tc.err)):
				t.Errorf("hostOS() = %v; want the error %q", err, tc.err)
			}
			if stdout != "" || stderr != "" {
				t.Errorf("wrote stdout %q, stderr %q", stdout, stderr)
			}
		})
	}
}
