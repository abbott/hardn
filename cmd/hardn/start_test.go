package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"os"
	osuser "os/user"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/abbott/hardn/pkg/config"
	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/hostfake"
	"github.com/abbott/hardn/pkg/output"
	"github.com/abbott/hardn/pkg/screens/mainmenu"
	"github.com/abbott/hardn/pkg/ui/term"
)

// startRun is what one run of start wrote to the process's two streams.
type startRun struct {
	stdout, stderr string
	code           int
	ended          bool
	root           string // what stands for /, where the log and the example are written
}

// captureStreams runs fn with os.Stdout and os.Stderr on pipes, and returns what each
// took: fmt.Print and cobra look them up when they write.
func captureStreams(t *testing.T, fn func()) (stdout, stderr string) {
	t.Helper()
	capture := func(f **os.File) func() string {
		r, w, err := os.Pipe()
		if err != nil {
			t.Fatal(err)
		}
		was := *f
		*f = w
		done := make(chan string)
		go func() {
			b, _ := io.ReadAll(r)
			done <- string(b)
		}()
		return func() string {
			*f = was
			w.Close()
			return <-done
		}
	}
	endOut, endErr := capture(&os.Stdout), capture(&os.Stderr)
	defer func() {
		stdout, stderr = endOut(), endErr()
	}()
	fn()
	return "", ""
}

// runStart is start with args on sys, as a user other than root, its log and example
// hardn.yml under a temporary root (startRun's root), so nothing reaches this machine.
// The command's stdout is the process's.
func runStart(t *testing.T, sys host.System, args ...string) startRun {
	t.Helper()
	return runStartWith(t, sys, nil, args...)
}

// runStartWith is runStart with setup, when set, run once every stub is in place and
// before start runs, to point a seam elsewhere (asRootStart).
func runStartWith(t *testing.T, sys host.System, setup func(), args ...string) startRun {
	t.Helper()
	root := t.TempDir()

	was := struct {
		ensure   func() error
		exit     func(int)
		host     func() host.System
		out      io.Writer
		errOut   io.Writer
		stdout   func() term.Env
		stderr   func() term.Env
		log      *output.Log
		logErr   bool
		logPath  string
		opened   bool
		user     func() (*osuser.User, error)
		load     func(string) (*config.Config, error)
		existing func(string) (*config.Config, error)
		find     func(string) config.Found
		detect   func() error
		menu     func(context.Context, mainmenu.Options)
		engine   func() host.System
	}{ensureExampleConfig, exit, readerHost, engineStdout, stderr, stdoutEnv, stderrEnv, hardnLog, logOnStderr, startLogPath, logOpened,
		lookupCurrentUser, loadConfig, loadExisting, findConfig, detectOS, startMenu, engineHost}
	t.Cleanup(func() {
		ensureExampleConfig, exit, readerHost, engineStdout, stderr, stdoutEnv, stderrEnv, hardnLog, logOnStderr, startLogPath, logOpened =
			was.ensure, was.exit, was.host, was.out, was.errOut, was.stdout, was.stderr, was.log, was.logErr, was.logPath, was.opened
		lookupCurrentUser, loadConfig, loadExisting, findConfig, detectOS, startMenu, engineHost =
			was.user, was.load, was.existing, was.find, was.detect, was.menu, was.engine
		resetFlags()
		// --help and -h stay set in cobra's flag for the next run, as any flag does.
		if f := rootCmd.Flags().Lookup("help"); f != nil {
			_ = f.Value.Set("false")
			f.Changed = false
		}
		rootCmd.SetArgs(nil)
	})
	plain := func() term.Env { return term.Resolve(term.Options{}, term.Facts{Width: 80, Height: 24}) }
	stdoutEnv, stderrEnv = plain, plain
	readerHost = func() host.System { return sys }
	engineHost = func() host.System {
		t.Error("the engine's host was reached")
		return hostfake.New(host.Options{})
	}
	ensureExampleConfig = func() error { return config.EnsureExampleConfigAt(filepath.Join(root, startExample)) }
	lookupCurrentUser = func() (*osuser.User, error) { return &osuser.User{Uid: "1000", Username: "alice"}, nil }
	unreached := func(string) (*config.Config, error) { return nil, errors.New("test: hardn.yml loaded") }
	loadConfig, loadExisting = unreached, unreached
	findConfig = func(string) config.Found { return config.Found{} }
	detectOS = func() error { return errors.New("test: the OS check") }
	startMenu = func(context.Context, mainmenu.Options) { t.Error("the main menu was run") }
	exit = func(code int) { panic(exited(code)) }
	if setup != nil {
		setup()
	}

	r := startRun{root: root}
	resetFlags()
	rootCmd.SetArgs(args)
	r.stdout, r.stderr = captureStreams(t, func() {
		engineStdout, stderr = os.Stdout, os.Stderr
		defer func() {
			if v := recover(); v != nil {
				code, ok := v.(exited)
				if !ok {
					panic(v)
				}
				r.code, r.ended = int(code), true
			}
		}()
		start(filepath.Join(root, startLog))
	})
	return r
}

// The log's and the example's paths under a start run's root.
const (
	startLog     = "var/log/hardn.log"
	startExample = "etc/hardn/hardn.yml.example"
)

// written lists the files and directories a start run made under its root.
func (r startRun) written(t *testing.T) []string {
	t.Helper()
	var made []string
	err := filepath.WalkDir(r.root, func(path string, _ fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path != r.root {
			rel, _ := filepath.Rel(r.root, path)
			made = append(made, rel)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return made
}

// asRootStart is runStartWith's setup for root, with hardn.yml's defaults found
// nowhere, on Debian 13, and setup, when set, after it.
func asRootStart(setup func()) func() {
	return func() {
		lookupCurrentUser = func() (*osuser.User, error) { return &osuser.User{Uid: "0", Username: "root"}, nil }
		defaults := func(string) (*config.Config, error) { return config.DefaultConfig(), nil }
		loadConfig, loadExisting = defaults, defaults
		detectOS = func() error { return nil }
		if setup != nil {
			setup()
		}
	}
}

// startUpLines are the warnings start said before every command, while it wrote the
// log and the example ahead of the flags; now said only where a command that writes
// them cannot.
var startUpLines = []string{
	"Failed to create log directory",
	"Failed to open log file",
	"Unable to create example configuration file",
}

// TestStartUpLeavesStdoutAlone: system-details, its --output json and -p write neither
// hardn's log nor the example hardn.yml, and say nothing of them, so that > file holds the
// report alone, --output json the document alone and -p the log alone (defect 28;
// unreleased.md; cmd-engine.md §2.11; refactor plan §3.23).
func TestStartUpLeavesStdoutAlone(t *testing.T) {
	for _, tc := range []struct {
		args  []string
		check func(t *testing.T, stdout string)
	}{
		{[]string{"system-details"}, func(t *testing.T, stdout string) {
			if !strings.Contains(stdout, "Memory") {
				t.Errorf("no report:\n%s", stdout)
			}
			if first := strings.SplitN(stdout, "\n", 2)[0]; strings.Contains(first, "Failed") || strings.Contains(first, "Warning") {
				t.Errorf("stdout starts with %q", first)
			}
		}},
		{[]string{"system-details", "--output", "json"}, func(t *testing.T, stdout string) {
			var doc map[string]json.RawMessage
			if err := json.Unmarshal([]byte(stdout), &doc); err != nil || len(doc["details"]) == 0 {
				t.Errorf("not the document alone (%v):\n%s", err, stdout)
			}
		}},
		{[]string{"-p"}, func(t *testing.T, stdout string) {
			if stdout != readerLog {
				t.Errorf("stdout %q, want the log alone", stdout)
			}
		}},
	} {
		t.Run(strings.Join(tc.args, " "), func(t *testing.T) {
			r := runStart(t, logHost(readerLog), tc.args...)
			if r.ended {
				t.Fatalf("ended with %d; stderr:\n%s", r.code, r.stderr)
			}
			tc.check(t, r.stdout)
			for _, l := range startUpLines {
				if strings.Contains(r.stdout+r.stderr, l) {
					t.Errorf("%q said:\n%s%s", l, r.stdout, r.stderr)
				}
			}
			if w := r.written(t); len(w) != 0 {
				t.Errorf("wrote %v", w)
			}
		})
	}
}

// TestStartWritesNothing: nothing is written before the flags are parsed, nor by a
// command that refuses before its operation, nor in plan only: --help, --version, a
// usage error, the root check's refusal, the OS check's, -c with no user, a bad
// --confirm-timeout, the main menu under -n, and confirm and rollback under -n (refused
// before their log opens) write neither hardn's log nor /etc/hardn (refactor plan §3.23,
// next/p3-startup).
func TestStartWritesNothing(t *testing.T) {
	menu := func() { startMenu = func(context.Context, mainmenu.Options) {} }
	for _, tc := range []struct {
		name  string
		setup func()
		args  []string
	}{
		{"--help", nil, []string{"--help"}},
		{"-h", nil, []string{"-h"}},
		{"--version", nil, []string{"--version"}},
		{"-v", nil, []string{"-v"}},
		{"an unknown flag", nil, []string{"--bogus"}},
		{"-r with -c", asRootStart(nil), []string{"-r", "-c", "-u", "carol"}},
		{"a bad --confirm-timeout", asRootStart(nil), []string{"-g", "--confirm-timeout", "soon"}},
		{"not root, the main menu", nil, nil},
		{"not root, -g", nil, []string{"-g"}},
		{"root, the OS refused", asRootStart(func() { detectOS = func() error { return errors.New("test: the OS check") } }), []string{"-g"}},
		{"root, -c with no user", asRootStart(nil), []string{"-c"}},
		{"root, plan only, the main menu", asRootStart(menu), []string{"-n"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := runStartWith(t, logHost(readerLog), tc.setup, tc.args...)
			if w := r.written(t); len(w) != 0 {
				t.Errorf("wrote %v; ended %v (%d), stderr:\n%s", w, r.ended, r.code, r.stderr)
			}
			for _, l := range startUpLines {
				if strings.Contains(r.stdout+r.stderr, l) {
					t.Errorf("%q said:\n%s", l, r.stderr)
				}
			}
		})
	}
	for _, args := range [][]string{{"-n", "confirm"}, {"-n", "rollback", revertTestID}, {"-n", "rollback", "--abandon", revertTestID}, {"-n", "rollback", "--boot"}} {
		t.Run("root, "+strings.Join(args, " "), func(t *testing.T) {
			r := runStartWith(t, logHost(readerLog), revertStart(t, 0, args[len(args)-1] == "--boot"), args...)
			if !r.ended || r.code != 2 {
				t.Errorf("ended %v with %d, want 2; stderr:\n%s", r.ended, r.code, r.stderr)
			}
			if w := r.written(t); len(w) != 0 {
				t.Errorf("wrote %v; stderr:\n%s", w, r.stderr)
			}
		})
	}
}

// startYML is the hardn.yml the first-run offer writes under a start run's root.
const startYML = "etc/hardn/hardn.yml"

// offerAnsweredYes is runStartWith's setup for root with no hardn.yml found, the
// first-run question on a terminal answered yes: loadConfig writes hardn.yml under
// the run's root (start's log path less startLog), as the offer does; loadExisting,
// which never offers, the defaults; the OS check's error osErr, nil for Debian 13.
// asked records that the offer was made.
func offerAnsweredYes(osErr error, asked *bool) func() {
	return asRootStart(func() {
		loadConfig = func(string) (*config.Config, error) {
			*asked = true
			p := filepath.Join(strings.TrimSuffix(startLogPath, startLog), startYML)
			if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
				return nil, err
			}
			return config.DefaultConfig(), os.WriteFile(p, []byte("username: \"\"\n"), 0o600)
		}
		detectOS = func() error { return osErr }
		startMenu = func(context.Context, mainmenu.Options) {}
	})
}

// TestStartFirstRunOffer: the first-run question is asked after the OS check, so that
// a host hardn refuses is never offered hardn.yml, and not under -n, which loads
// hardn.yml where one is found, the defaults where none is (loadExisting), and so
// writes nothing; a yes on a live run writes hardn.yml, asked for, even where a refusal
// follows (-c with no user).
func TestStartFirstRunOffer(t *testing.T) {
	for _, tc := range []struct {
		name  string
		osErr error
		args  []string
		asked bool
		code  int // 0: not ended
		wrote []string
	}{
		{"an unsupported OS", errors.New("test: Fedora isn't supported"), []string{"-g"}, false, 1, nil},
		{"an unsupported OS, the main menu", errors.New("test: Fedora isn't supported"), nil, false, 1, nil},
		{"-n, the main menu", nil, []string{"-n"}, false, 0, nil},
		{"-n -c with no user", nil, []string{"-n", "-c"}, false, 1, nil},
		{"live, -c with no user", nil, []string{"-c"}, true, 1, []string{"etc", "etc/hardn", startYML}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			asked := false
			r := runStartWith(t, logHost(readerLog), offerAnsweredYes(tc.osErr, &asked), tc.args...)
			if got := map[bool]int{true: r.code}[r.ended]; got != tc.code || asked != tc.asked {
				t.Errorf("ended %v with %d (want %d), offered %v (want %v); stderr:\n%s", r.ended, r.code, tc.code, asked, tc.asked, r.stderr)
			}
			if w := r.written(t); !slices.Equal(w, tc.wrote) {
				t.Errorf("wrote %v, want %v; stderr:\n%s", w, tc.wrote, r.stderr)
			}
		})
	}
}

// TestNotRootRefusedFirst: a user other than root is refused before anything else is
// said or written: the refusal and how to run hardn as root, alone on stderr, exit
// status 1.
func TestNotRootRefusedFirst(t *testing.T) {
	for _, args := range [][]string{nil, {"-g"}, {"-n", "-r"}} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			r := runStart(t, logHost(readerLog), args...)
			want := "Operation Failed This script needs to be run as root. " + rootHint()
			if !r.ended || r.code != 1 || flat(r.stderr) != want || r.stdout != "" {
				t.Errorf("ended %v with %d; stderr %q, want %q; stdout %q", r.ended, r.code, flat(r.stderr), want, r.stdout)
			}
			if w := r.written(t); len(w) != 0 {
				t.Errorf("wrote %v", w)
			}
		})
	}
	// confirm and rollback refuse a user other than root as their first words, the
	// log not opened (revertPreflight).
	for _, tc := range []struct {
		args []string
		name string // as the refusal names the command
	}{
		{[]string{"confirm"}, "confirm"},
		{[]string{"rollback", revertTestID}, "rollback"},
		{[]string{"rollback", "--abandon", revertTestID}, "rollback --abandon"},
		{[]string{"rollback", "--boot"}, "rollback --boot"},
	} {
		t.Run(strings.Join(tc.args, " "), func(t *testing.T) {
			r := runStartWith(t, logHost(readerLog), revertStart(t, 1000, false), tc.args...)
			want := "Operation Failed hardn " + tc.name + " must be run as root: sudo hardn " + tc.name
			if !r.ended || r.code != 1 || flat(r.stderr) != want || r.stdout != "" {
				t.Errorf("ended %v with %d; stderr %q, want %q; stdout %q", r.ended, r.code, flat(r.stderr), want, r.stdout)
			}
			if w := r.written(t); len(w) != 0 {
				t.Errorf("wrote %v", w)
			}
		})
	}
}

// TestStartLiveWrites: root's main menu, live, opens hardn's log and writes the example
// hardn.yml once the root check, hardn.yml's load and the OS check are past; a failure
// to write the example is a warning, and the menu still runs. confirm and rollback open the
// log once their refusals are past (revertPreflight, openStartLog), and write their steps
// to it.
func TestStartLiveWrites(t *testing.T) {
	t.Run("the main menu", func(t *testing.T) {
		ran := false
		r := runStartWith(t, logHost(readerLog), asRootStart(func() {
			startMenu = func(context.Context, mainmenu.Options) { ran = true }
		}))
		if r.ended || !ran {
			t.Fatalf("ended %v (%d), menu run %v; stderr:\n%s", r.ended, r.code, ran, r.stderr)
		}
		if _, err := os.Stat(filepath.Join(r.root, startLog)); err != nil {
			t.Errorf("the log: %v", err)
		}
		if b, err := os.ReadFile(filepath.Join(r.root, startExample)); err != nil || string(b) != config.ExampleConfigContent {
			t.Errorf("the example: %v", err)
		}
		if r.stderr != "" || r.stdout != "" {
			t.Errorf("stderr %q, stdout %q", r.stderr, r.stdout)
		}
	})
	t.Run("the example cannot be written", func(t *testing.T) {
		ran := false
		r := runStartWith(t, logHost(readerLog), asRootStart(func() {
			startMenu = func(context.Context, mainmenu.Options) { ran = true }
			ensureExampleConfig = func() error { return errors.New("mkdir /etc/hardn: permission denied") }
		}))
		if r.ended || !ran {
			t.Fatalf("ended %v (%d), menu run %v; stderr:\n%s", r.ended, r.code, ran, r.stderr)
		}
		if want := "Warning Unable to create example configuration file: mkdir /etc/hardn: permission denied"; flat(r.stderr) != want {
			t.Errorf("stderr %q, want %q", flat(r.stderr), want)
		}
	})
	// An operation flag, live, as root, opens the log and writes the example once its
	// refusals are past, ahead of its run (here -g on a host with nothing on it).
	t.Run("-g", func(t *testing.T) {
		r := runStartWith(t, logHost(readerLog), asRootStart(func() {
			engineHost = func() host.System { return hostfake.New(host.Options{}) }
		}), "--yes", "-g")
		if _, err := os.Stat(filepath.Join(r.root, startLog)); err != nil {
			t.Errorf("the log: %v; stderr:\n%s", err, r.stderr)
		}
		if b, err := os.ReadFile(filepath.Join(r.root, startExample)); err != nil || string(b) != config.ExampleConfigContent {
			t.Errorf("the example: %v", err)
		}
	})
	// confirm and rollback, as root, past their refusals (revertPreflight): the log
	// opened, and the steps written to it; rollback --boot's, as the boot unit runs
	// it, among them.
	for _, tc := range []struct {
		name   string
		args   []string
		reboot bool
		logged string // in the log
	}{
		{"confirm", []string{"confirm"}, false, "Change " + revertTestID + " confirmed"},
		{"rollback", []string{"rollback", revertTestID}, false, "rolled back"},
		{"rollback --abandon", []string{"rollback", "--abandon", revertTestID}, false, revertTestID},
		{"rollback --boot", []string{"rollback", "--boot"}, true, "revert: boot: change " + revertTestID},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := runStartWith(t, logHost(readerLog), revertStart(t, 0, tc.reboot), tc.args...)
			b, err := os.ReadFile(filepath.Join(r.root, startLog))
			if err != nil || !strings.Contains(string(b), tc.logged) {
				t.Errorf("the log (%v) lacks %q:\n%s\nstderr:\n%s", err, tc.logged, b, r.stderr)
			}
		})
	}
}

// revertStart is runStartWith's setup for confirm and rollback: newRevertSeams's change
// waiting for confirmation, the effective uid euid, rebooted a minute after the change
// where reboot, and revertExit ending the run as start's exit does.
func revertStart(t *testing.T, euid int, reboot bool) func() {
	return func() {
		s := newRevertSeams(t, euid)
		if reboot {
			rebootSeams(s, time.Minute)
		}
		revertExit = func(code int) { exit(code) }
	}
}

// TestUsageErrors: a bad command line is said once, on stderr, exit status 2, as every
// refusal of these flags is, whichever command it is; stdout holds nothing, so that
// hardn system-details --output > r.json leaves r.json empty.
func TestUsageErrors(t *testing.T) {
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"system-details", "--output"}, "flag needs an argument: --output"},
		{[]string{"system-details", "--bogus"}, "unknown flag: --bogus"},
		{[]string{"-p", "--bogus"}, "unknown flag: --bogus"},
		{[]string{"--bogus"}, "unknown flag: --bogus"},
		{[]string{"--test-update"}, "unknown flag: --test-update"},
		{[]string{"--test-security-update"}, "unknown flag: --test-security-update"},
		{[]string{"--debug-updates"}, "unknown flag: --debug-updates"},
		{[]string{"bogus"}, `unknown command "bogus"`},
		{[]string{"confirm", "extra"}, `unknown command "extra"`},
	} {
		t.Run(strings.Join(tc.args, " "), func(t *testing.T) {
			r := runStart(t, logHost(readerLog), tc.args...)
			if !r.ended || r.code != 2 {
				t.Errorf("ended %v with %d, want exit status 2; stderr:\n%s", r.ended, r.code, r.stderr)
			}
			if r.stdout != "" {
				t.Errorf("stdout %q, want nothing", r.stdout)
			}
			if got := strings.Count(r.stderr, tc.want); got != 1 {
				t.Errorf("said %d times on stderr, want once:\n%s", got, r.stderr)
			}
		})
	}
}
