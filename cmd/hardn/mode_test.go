package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
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
// dryRun, read through the real loader and the real config.FileDryRun at
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

// setup points runHardnWith's loader and fileDryRun at the file, each
// failing the test if hardn asks it for another path; with no file, the
// loader gives the defaults (no file found, none created) and fileDryRun
// none.
func (y hardnYML) setup(t *testing.T) func() {
	return func() {
		load, peek := loadConfig, fileDryRun // runHardnWith's, which record the step
		loadConfig = func(p string) (*config.Config, error) {
			if _, err := load(p); err != nil {
				return nil, err
			}
			if y.path == "" {
				c := config.DefaultConfig()
				c.Username, c.LogFile, c.SshPort = "alice", y.logFile, 2230
				c.SshAllowedUsers, c.Nameservers = []string{"alice"}, []string{"192.0.2.53"}
				return c, nil
			}
			if p != y.path {
				t.Fatalf("loaded %q, not -f's %q", p, y.path)
			}
			return config.LoadConfigWithEnvPriority(p)
		}
		fileDryRun = func(p string) (string, bool, error) {
			if _, _, err := peek(p); err != nil || y.path == "" {
				return "", false, err
			}
			if p != y.path {
				t.Fatalf("read dryRun from %q, not -f's %q", p, y.path)
			}
			return config.FileDryRun(p)
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
			if r.peeks != 0 {
				t.Errorf("hardn.yml read before the root check, with no operation flag")
			}
		})
	}
}

// TestPlanOnlyRefusesOperations: decision 9, each operation flag under each
// source (operationRuns: -g, -e and setup-sudo-env show their plans
// instead, TestConfigureDNSFlagPlanOnlyFromFile and
// TestKeepEnvironmentPlanOnlyFromFile). hardn.yml's dryRun: true, no -n/--dry-run given, refuses it as -n
// does, exit status 2 before the root check, nothing run, naming the file
// and --dry-run=false; -n refuses it whatever the file says; and it runs
// live with no file, under dryRun: false, and under --dry-run=false over
// dryRun: true.
func TestPlanOnlyRefusesOperations(t *testing.T) {
	const (
		applies = iota
		refusedByFile
		refusedByFlag
	)
	for _, op := range operationRuns {
		for _, src := range []struct {
			name, file string
			flag       []string
			want       int
		}{
			{"no file", "", nil, applies},
			{"file false", "false", nil, applies},
			{"file true", "true", nil, refusedByFile},
			{"-n, file false", "false", []string{"-n"}, refusedByFlag},
			{"-n, file true", "true", []string{"-n"}, refusedByFlag},
			{"--dry-run=false, file true", "true", []string{"--dry-run=false"}, applies},
		} {
			t.Run(strings.Join(op.args, " ")+", "+src.name, func(t *testing.T) {
				y := newHardnYML(t, src.file)
				r := runHardnWith(t, asRoot, y.setup(t), y.args(append(slices.Clone(src.flag), op.args...)...)...)
				if src.want == applies {
					if r.ended || !changesHost(r.calls) {
						t.Fatalf("ended %v with %d, calls %v; stderr:\n%s", r.ended, r.code, r.calls, r.stderr)
					}
					return
				}
				if !r.ended || r.code != hardncmd.ExitUsage || len(r.reached) != 0 || len(r.calls) != 0 {
					t.Fatalf("ended %v with %d after %v, calls %v; want exit status 2 before the root check; stderr:\n%s",
						r.ended, r.code, r.reached, r.calls, r.stderr)
				}
				want := []string{op.name, "-n/--dry-run only previews", "Nothing was changed."}
				if src.want == refusedByFile {
					want = []string{"hardn: " + y.path + " sets plan only (dryRun: true)", op.name, "Nothing was changed.", "add --dry-run=false"}
				}
				for _, w := range want {
					if !strings.Contains(r.stderr, w) {
						t.Errorf("stderr lacks %q:\n%s", w, r.stderr)
					}
				}
			})
		}
	}
}

// changesHost reports whether calls hold a file write or a command.
func changesHost(calls []string) bool {
	return slices.ContainsFunc(calls, func(c string) bool {
		return strings.HasPrefix(c, "write ") || strings.HasPrefix(c, "exec ")
	})
}

// TestPlanOnlyRefusedAsLoaded: should hardn.yml say dryRun: true when it is
// loaded, where it did not before the root check, the operation is still
// refused, exit status 2, after the load and before anything else.
func TestPlanOnlyRefusedAsLoaded(t *testing.T) {
	y := newHardnYML(t, "true")
	r := runHardnWith(t, asRoot, func() {
		y.setup(t)()
		fileDryRun = func(string) (string, bool, error) { return y.path, false, nil }
	}, y.args("-d")...)
	if !r.ended || r.code != hardncmd.ExitUsage || len(r.calls) != 0 {
		t.Fatalf("ended %v with %d, calls %v; stderr:\n%s", r.ended, r.code, r.calls, r.stderr)
	}
	if want := []string{"root check", "config load"}; !slices.Equal(r.reached, want) {
		t.Errorf("reached %v, want %v", r.reached, want)
	}
	if !strings.Contains(r.stderr, y.path+" sets plan only") {
		t.Errorf("stderr:\n%s", r.stderr)
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
		{"-g -d", []string{"-g", "-d"}, hardncmd.ExitUsage},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h, _ := newDNSHost(t, "debian13", dnsTestList)
			h.AddFile(dnsTestYML, []byte(h.content(dnsTestYML)+"dryRun: true\n"), 0o644, 0, 0)
			cfg, err := config.Read(context.Background(), h, dnsTestYML)
			if err != nil {
				t.Fatal(err)
			}
			yml := h.content(dnsTestYML)
			r, s := runG(t, h, cfg, func(*dnsSeams) {
				fileDryRun = func(string) (string, bool, error) { return dnsTestYML, true, nil }
			}, tc.args...)
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
				if len(written) > 0 || len(r.reached) != 0 || !strings.Contains(r.stderr, hardncmd.FlagDisableRoot) ||
					strings.Contains(r.stderr, hardncmd.FlagConfigureDNS) {
					t.Errorf("wrote %q after %v; stderr:\n%s", written, r.reached, r.stderr)
				}
			}
			if got := h.content(dnsTestYML); got != yml {
				t.Errorf("hardn.yml changed:\n%s", got)
			}
		})
	}
}
