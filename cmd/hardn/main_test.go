package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	osuser "os/user"
	"path/filepath"
	"strings"
	"testing"

	"github.com/abbott/hardn/pkg/config"
	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/hostfake"
	"github.com/abbott/hardn/pkg/ui/term"
)

// These tests drive rootCmd as the hardn binary does, with every step that
// reaches the host replaced: the root check, config load and OS
// detection by stubs, and the engine's host by a fake that fails the test
// when a run reaches it unasked (a test of -g gives it one: useDNSHost).
// Nothing here reads this machine's user, config or OS, runs a command or
// writes a file.

// exited is what the stubbed exit panics with: the status hardn would
// have ended with.
type exited int

// result is one hardn run.
type result struct {
	stderr  string
	stdout  string   // what the command wrote to stdout (engineStdout), unless setup points it elsewhere
	code    int      // exit status, when ended
	ended   bool     // the run ended by exit
	reached []string // the host steps past the dry-run refusal that ran
}

// hostMode sets what the stubbed host steps do. Each records that it was
// reached. hostUnreachable makes each fail, so a run that gets past the
// refusal ends at the first (exit status 1). asRoot makes them succeed:
// root, a fixture config for alice on port 2230, Debian 13 with
// /etc/sudoers.d.
type hostMode int

const (
	hostUnreachable hostMode = iota
	asRoot
	// asRootBoolRootLogin is asRoot with a configuration file, in the
	// test's temporary directory, whose permitRootLogin is the deprecated
	// bool (§3.17).
	asRootBoolRootLogin
)

func resetFlags() {
	noColor, configFile, username, dryRun = false, "", "", false
	createUser, disableRootSSH, configureUfw, configureDns, runAll, printLogs = false, false, false, false, false, false
	showVersion, setupSudoEnv, assumeYes = false, false, false
	colorMode, asciiOutput, plainOutput, themeMode = term.ColorUnset, false, false, term.ThemeUnset
	confirmTimeoutFlag, confirmTimeoutGiven, themeAnswered = "", false, term.ThemeUnset
	cfg = nil
	// A flag given in one run stays given in pflag for the next (dryRunGiven).
	for _, name := range []string{"dry-run", "print-logs", "create-user", "disable-root", "configure-dns", "configure-ufw", "run-all", "setup-sudo-env",
		"color", "ascii", "plain", "theme", "confirm-timeout", "no-color", "yes"} {
		rootCmd.PersistentFlags().Lookup(name).Changed = false
	}
	if c, _, err := rootCmd.Find([]string{"system-details"}); err == nil {
		for _, name := range []string{"output", "export", "section", "format", "json", "yaml"} {
			f := c.Flags().Lookup(name)
			_ = f.Value.Set(f.DefValue)
			f.Changed = false
		}
	}
}

func runHardn(t *testing.T, mode hostMode, args ...string) result {
	t.Helper()
	return runHardnWith(t, mode, nil, args...)
}

// runHardnWith is runHardn with setup, when set, run once every stub is in
// place and before hardn runs, to point a seam elsewhere.
func runHardnWith(t *testing.T, mode hostMode, setup func(), args ...string) result {
	t.Helper()
	t.Setenv("SUDO_USER", "alice")
	t.Setenv("USER", "root")
	t.Setenv("HARDN_CONFIG", "")

	var r result
	prev := struct {
		user     func() (*osuser.User, error)
		load     func(string) (*config.Config, error)
		existing func(string) (*config.Config, error)
		find     func(string) config.Found
		detect   func() error
		exit     func(int)
	}{lookupCurrentUser, loadConfig, loadExisting, findConfig, detectOS, exit}
	prevStderr, prevStderrEnv := stderr, stderrEnv
	prevEngineHost, prevEngineDetect, prevEngineStdout := engineHost, engineDetect, engineStdout
	stderrEnv = func() term.Env { return term.Resolve(term.Options{}, term.Facts{}) } // not a terminal: plain
	engineHost = func() host.System {
		t.Error("the engine's host was reached, and the test gave it none (useDNSHost)")
		return hostfake.New(host.Options{})
	}
	engineDetect = func(context.Context, host.System) (detect.Facts, error) { return detect.Facts{}, nil }
	engineStdout = io.Discard
	t.Cleanup(func() {
		lookupCurrentUser, loadConfig, loadExisting, findConfig, detectOS, exit = prev.user, prev.load, prev.existing, prev.find, prev.detect, prev.exit
		stderr, stderrEnv = prevStderr, prevStderrEnv
		engineHost, engineDetect, engineStdout = prevEngineHost, prevEngineDetect, prevEngineStdout
		resetFlags()
		rootCmd.SetArgs(nil)
	})

	var errOut, out bytes.Buffer
	stderr = &errOut
	engineStdout = &out
	exit = func(code int) { panic(exited(code)) }
	unreachable := errors.New("test: a host step was reached")
	lookupCurrentUser = func() (*osuser.User, error) {
		r.reached = append(r.reached, "root check")
		if mode != hostUnreachable {
			return &osuser.User{Uid: "0", Username: "root"}, nil
		}
		return nil, unreachable
	}
	confPath := filepath.Join(t.TempDir(), "hardn.yml")
	loadConfig = func(string) (*config.Config, error) {
		r.reached = append(r.reached, "config load")
		if mode == asRootBoolRootLogin {
			if err := os.WriteFile(confPath, []byte("username: alice\nsshPort: 2230\npermitRootLogin: false\nnameservers:\n  - 192.0.2.53\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			return config.LoadConfigWithEnvPriority(confPath)
		}
		if mode != hostUnreachable {
			c := config.DefaultConfig()
			c.Username = "alice"
			c.SshPort = 2230
			c.Nameservers = []string{"192.0.2.53"}
			return c, nil
		}
		return nil, unreachable
	}
	loadExisting = loadConfig // setup-sudo-env's, the same configurations
	// The file the stubbed loads stand for, which hardn says it read.
	findConfig = func(string) config.Found { return config.Found{Path: confPath, By: config.FoundSearch, Exists: true} }
	detectOS = func() error {
		r.reached = append(r.reached, "OS detection")
		if mode != hostUnreachable {
			return nil // Debian 13
		}
		return unreachable
	}

	if setup != nil {
		setup()
	}
	resetFlags()
	rootCmd.SetArgs(args)
	func() {
		defer func() {
			if v := recover(); v != nil {
				code, ok := v.(exited)
				if !ok {
					panic(v)
				}
				r.code, r.ended = int(code), true
			}
		}()
		if err := rootCmd.Execute(); err != nil {
			t.Fatalf("hardn %s: %v", strings.Join(args, " "), err)
		}
	}()
	r.stderr = errOut.String()
	r.stdout = out.String()
	return r
}

// TestDryRunAloneIsNotRefused checks that -n without an operation, or with
// -g, -e, -w, -c, -d, -r or setup-sudo-env, whose dry runs are their plans
// (-r's Run All's, slice 8, unit (d)), still reaches the
// root check (and then the menus in dry-run mode, or the plan); the stubbed
// root check fails, so the run ends there with status 1. -n -p no longer
// does: -p alone runs ahead of the root check (decisions 9 (a), 10 (a);
// TestPrintLogAlone).
func TestDryRunAloneIsNotRefused(t *testing.T) {
	for _, args := range [][]string{{"-n"}, {"-n", "-g"}, {"-n", "-e"}, {"-n", "-w"}, {"-n", "-c", "-u", "carol"}, {"-n", "-d"}, {"-n", "-r"}, {"-n", "setup-sudo-env"}, {"-d"}} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			r := runHardn(t, hostUnreachable, args...)
			if !r.ended || r.code != 1 || len(r.reached) != 1 || r.reached[0] != "root check" {
				t.Fatalf("ended %v with %d after %v, want status 1 at the root check; stderr:\n%s", r.ended, r.code, r.reached, r.stderr)
			}
			if flat(r.stderr) != "Operation Failed Failed to get current user: test: a host step was reached" || r.stdout != "" {
				t.Errorf("stderr %q, stdout %q", r.stderr, r.stdout)
			}
		})
	}
}

// TestUpdateFlagsGone: the update-check flags, which did nothing while
// pkg/version's updateCheckEnabled is false (V26), are gone (legacy-left.md
// §3, decision 3 (a)); TestUsageErrors runs each as an unknown flag, exit
// status 2.
func TestUpdateFlagsGone(t *testing.T) {
	for _, name := range []string{"debug-updates", "test-update", "test-security-update"} {
		if f := rootCmd.PersistentFlags().Lookup(name); f != nil {
			t.Errorf("--%s is still a flag: %+v", name, f)
		}
	}
}

// TestDeprecationWarning: a bool permitRootLogin in the configuration is
// warned about on stderr, once, when it loads (§3.17, approved), with the
// file and line and what to write instead; the operation goes on, and
// stdout does not repeat it. A configuration without
// it warns nothing. (Run with -r, which plans Run All on the engine's
// host, here the DNS tests' (useDNSHost), whose hardn.yml asks nothing.)
func TestDeprecationWarning(t *testing.T) {
	onDNSHost := func() {
		h, _ := newDNSHost(t, "debian13", nil)
		useDNSHost(t, h)
	}
	r := runHardnWith(t, asRootBoolRootLogin, onDNSHost, "-r")
	if r.ended {
		t.Fatalf("ended with %d; stderr:\n%s", r.code, r.stderr)
	}
	// The file read (decision 4 (a)), then one Warning callout (P-42 R5):
	// its title, then the warning as its body, read here without the bar.
	got := flat(r.stderr)
	if !strings.HasPrefix(got, "Using configuration from: ") || strings.Count(got, "Warning ") != 1 || !strings.Contains(got, `hardn.yml: permitRootLogin: false (line 3) is deprecated; write "no", which means the same`) ||
		strings.Count(got, "deprecated") != 1 || strings.Contains(r.stderr, "\x1b") {
		t.Errorf("stderr:\n%s", r.stderr)
	}
	if strings.Contains(r.stdout, "deprecated") {
		t.Errorf("stdout repeats it:\n%s", r.stdout)
	}
	if r := runHardnWith(t, asRoot, onDNSHost, "-r"); strings.Contains(r.stderr, "deprecated") {
		t.Errorf("a configuration without the bool warns:\n%s", r.stderr)
	}
}
