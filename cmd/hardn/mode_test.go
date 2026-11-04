package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	hardncmd "github.com/abbott/hardn/pkg/cmd"
	"github.com/abbott/hardn/pkg/config"
	hostdns "github.com/abbott/hardn/pkg/host/dns"
	"github.com/abbott/hardn/pkg/osdetect"
)

// The session's mode (refactor plan §3.19, "explicit flag wins, otherwise
// config"; slice 2b, unit (b), decisions 2 and 9): every start, through the
// root command, on a hardn.yml in the test's temporary directory.

// hardnYML is a test's hardn.yml: none (""), or a file whose dryRun line is
// dryRun, read through the real loader at
// path, which args name with -f, so that no search reaches this machine's
// files. Its other keys are asRoot's configuration, its log in the
// temporary directory (-p reads it).
type hardnYML struct {
	path, dryRun string
	logFile      string
}

// newHardnYML writes the file for dryRun ("" for no file).
func newHardnYML(t *testing.T, dryRun string) hardnYML {
	t.Helper()
	t.Setenv("HOME", t.TempDir()) // the loader's default location, should a search reach it
	dir := t.TempDir()
	y := hardnYML{dryRun: dryRun, logFile: filepath.Join(dir, "hardn.log")}
	if dryRun == "" {
		return y
	}
	y.path = filepath.Join(dir, "hardn.yml")
	body := "username: alice\nlogFile: " + y.logFile + "\ndryRun: " + dryRun +
		"\nsshPort: 2230\nsshAllowedUsers:\n  - alice\nnameservers:\n  - 192.0.2.53\n"
	if err := os.WriteFile(y.path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return y
}

// args is args after -f naming the file, when there is one.
func (y hardnYML) args(args ...string) []string {
	if y.path == "" {
		return args
	}
	return append([]string{"-f", y.path}, args...)
}

// setup points runHardnWith's loader at the file, failing the test if
// hardn asks it for another path; with no file, the loader gives the
// defaults (no file found, none created).
func (y hardnYML) setup(t *testing.T) func() {
	return func() {
		load := loadConfig // runHardnWith's, which records the step
		loadConfig = func(p string) (*config.Config, error) {
			if _, err := load(p); err != nil {
				return nil, err
			}
			if y.path == "" {
				c := config.DefaultConfig()
				c.Username, c.LogFile, c.SshPort = "alice", y.logFile, 2230
				c.Nameservers = []string{"192.0.2.53"}
				return c, nil
			}
			if p != y.path {
				t.Fatalf("loaded %q, not -f's %q", p, y.path)
			}
			return config.LoadConfigWithEnvPriority(p)
		}
	}
}

// TestDryRunPrecedence: each start's mode and its source, -n/--dry-run
// given (true or false) winning over hardn.yml's dryRun, which wins over
// the default, live; live names no source (decision 2). detectOS fails, so that
// the run ends past where the mode is set and the menus are not opened (-p alone,
// which this ran with, now runs ahead of the load); the menus take the same
// configuration.
func TestDryRunPrecedence(t *testing.T) {
	for _, tc := range []struct {
		name, file string // file: hardn.yml's dryRun, "" for no file
		flag       []string
		planOnly   bool
		source     config.DryRunSource
	}{
		{"no flag, no file", "", nil, false, config.DryRunDefault},
		{"no flag, file false", "false", nil, false, config.DryRunDefault},
		{"no flag, file true", "true", nil, true, config.DryRunFile},
		{"-n, file false", "false", []string{"-n"}, true, config.DryRunFlag},
		{"-n, file true", "true", []string{"-n"}, true, config.DryRunFlag},
		{"--dry-run=false, file true", "true", []string{"--dry-run=false"}, false, config.DryRunDefault},
		{"--dry-run=true, file false", "false", []string{"--dry-run=true"}, true, config.DryRunFlag},
		{"-n, no file", "", []string{"-n"}, true, config.DryRunFlag},
		{"--dry-run=false, no file", "", []string{"--dry-run=false"}, false, config.DryRunDefault},
	} {
		t.Run(tc.name, func(t *testing.T) {
			y := newHardnYML(t, tc.file)
			setup := y.setup(t)
			r := runHardnWith(t, asRoot, func() {
				setup()
				detectOS = func() (*osdetect.OSInfo, error) { return nil, errors.New("test: past the mode") }
			}, y.args(tc.flag...)...)
			if !r.ended || r.code != 1 {
				t.Fatalf("ended %v with %d, want status 1 at detectOS; stderr:\n%s", r.ended, r.code, r.stderr)
			}
			if cfg == nil {
				t.Fatal("no configuration loaded")
			}
			if cfg.DryRun != tc.planOnly || cfg.DryRunSource() != tc.source {
				t.Errorf("plan only %v, source %d; want %v, %d", cfg.DryRun, cfg.DryRunSource(), tc.planOnly, tc.source)
			}
		})
	}
}

// TestConfigureDNSFlagPlanOnlyFromFile: -g is decision 9's exception, as
// for -n: under hardn.yml's dryRun: true it shows the plan, writes nothing
// and exits 3 where there are changes; --dry-run=false applies it; with
// another operation flag it is refused, the refusal naming that flag alone.
func TestConfigureDNSFlagPlanOnlyFromFile(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
		code int
	}{
		{"-g", []string{"-g"}, 3},
		{"--dry-run=false -g", []string{"--dry-run=false", "-g"}, 0},
		{"-g -r", []string{"-g", "-r"}, hardncmd.ExitUsage},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h, _ := newDNSHost(t, "debian13", dnsTestList)
			h.AddFile(dnsTestYML, []byte(h.content(dnsTestYML)+"dryRun: true\n"), 0o644, 0, 0)
			cfg, err := config.Read(context.Background(), h, dnsTestYML)
			if err != nil {
				t.Fatal(err)
			}
			yml := h.content(dnsTestYML)
			r, s := runG(t, h, cfg, nil, tc.args...)
			if got := map[bool]int{true: r.code}[r.ended]; got != tc.code {
				t.Fatalf("exit status %d (ended %v), want %d; report:\n%s\nstderr:\n%s", r.code, r.ended, tc.code, s.out.String(), r.stderr)
			}
			written := h.writes()
			switch tc.code {
			case 3:
				if len(written) > 0 || !strings.Contains(s.out.String(), "No actual changes were made to your system.") {
					t.Errorf("wrote %q; report:\n%s", written, s.out.String())
				}
			case 0:
				if !strings.Contains(h.content(hostdns.ResolvConf), "nameserver 192.0.2.53\n") {
					t.Errorf("not applied; report:\n%s", s.out.String())
				}
			default:
				if len(written) > 0 || len(r.reached) != 0 || !strings.Contains(r.stderr, errRunAllAlone.Error()) {
					t.Errorf("wrote %q after %v; stderr:\n%s", written, r.reached, r.stderr)
				}
			}
			if got := h.content(dnsTestYML); got != yml {
				t.Errorf("hardn.yml changed:\n%s", got)
			}
		})
	}
}
