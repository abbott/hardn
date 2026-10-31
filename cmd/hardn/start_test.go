package main

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/abbott/hardn/pkg/config"
	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/logging"
	"github.com/abbott/hardn/pkg/output"
	"github.com/abbott/hardn/pkg/ui/term"
)

// startRun is what one run of start wrote to the process's two streams.
type startRun struct {
	stdout, stderr string
	code           int
	ended          bool
}

// captureStreams runs fn with os.Stdout and os.Stderr on pipes, and returns what each
// took: fmt.Print, InitLogging and cobra look them up when they write.
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

// runStart is start with args on sys, and a log whose directory cannot be made and an
// example file that cannot be written, as for a user other than root: what main runs,
// but for the paths, so nothing reaches this machine. The command's stdout is the
// process's.
func runStart(t *testing.T, sys host.System, args ...string) startRun {
	t.Helper()
	file := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}

	was := struct {
		ensure  func() error
		exit    func(int)
		host    func() host.System
		out     io.Writer
		errOut  io.Writer
		stdout  func() term.Env
		stderr  func() term.Env
		fallbck io.Writer
	}{ensureExampleConfig, exit, readerHost, engineStdout, stderr, stdoutEnv, stderrEnv, logging.FallbackOutput}
	t.Cleanup(func() {
		ensureExampleConfig, exit, readerHost, engineStdout, stderr, stdoutEnv, stderrEnv, logging.FallbackOutput =
			was.ensure, was.exit, was.host, was.out, was.errOut, was.stdout, was.stderr, was.fallbck
		logging.InitLogging(os.DevNull)
		resetFlags()
		rootCmd.SetArgs(nil)
	})
	plain := func() term.Env { return term.Resolve(term.Options{}, term.Facts{Width: 80, Height: 24}) }
	stdoutEnv, stderrEnv = plain, plain
	readerHost = func() host.System { return sys }
	ensureExampleConfig = func() error { return errors.New("mkdir /etc/hardn: permission denied") }
	exit = func(code int) { panic(exited(code)) }

	var r startRun
	resetFlags()
	rootCmd.SetArgs(args)
	r.stdout, r.stderr = captureStreams(t, func() {
		engineStdout, stderr = os.Stdout, os.Stderr
		logging.FallbackOutput = os.Stderr
		defer func() {
			if v := recover(); v != nil {
				code, ok := v.(exited)
				if !ok {
					panic(v)
				}
				r.code, r.ended = int(code), true
			}
		}()
		start(filepath.Join(file, "dir", "hardn.log"))
	})
	return r
}

// TestStartUpLeavesStdoutAlone: as a user other than root, hardn's three start-up lines
// go to stderr, each of them, so that > file holds the report alone, --output json the
// document alone and -p the log alone (defect 28; unreleased.md; cmd-engine.md §2.11).
func TestStartUpLeavesStdoutAlone(t *testing.T) {
	lines := []string{
		"Failed to create log directory",
		"Failed to open log file",
		"Warning: Unable to create example configuration file",
	}
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
			for _, l := range lines {
				if strings.Contains(r.stdout, l) {
					t.Errorf("%q on stdout:\n%s", l, r.stdout)
				}
				if !strings.Contains(r.stderr, l) {
					t.Errorf("%q not on stderr:\n%s", l, r.stderr)
				}
			}
		})
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

// TestRunAllFlagNamesHardnsLog: -r's closing line names the file hardn writes, whatever
// hardn.yml's logFile says (decision 6 (a); defect 21).
func TestRunAllFlagNamesHardnsLog(t *testing.T) {
	const elsewhere = "/srv/log/elsewhere.log"
	stdout, _ := captureStreams(t, func() {
		r := runHardnWith(t, asRoot, func() {
			loadConfig = func(string) (*config.Config, error) {
				c := config.DefaultConfig()
				c.Username, c.LogFile, c.SshPort = "alice", elsewhere, 2230
				c.SshAllowedUsers = []string{"alice"}
				return c, nil
			}
		}, "-r")
		if r.ended {
			t.Errorf("ended with %d; stderr:\n%s", r.code, r.stderr)
		}
	})
	if !strings.Contains(stdout, "Check the log file at "+output.LegacyLogPath+" for details.") {
		t.Errorf("does not name %s:\n%s", output.LegacyLogPath, stdout)
	}
	if strings.Contains(stdout, elsewhere) {
		t.Errorf("names hardn.yml's logFile:\n%s", stdout)
	}
}
