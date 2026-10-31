package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	osuser "os/user"
	"slices"
	"strings"
	"testing"

	"github.com/abbott/hardn/pkg/config"
	"github.com/abbott/hardn/pkg/engine/logview"
	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/hostfake"
	"github.com/abbott/hardn/pkg/osdetect"
	"github.com/abbott/hardn/pkg/ui/nav"
	"github.com/abbott/hardn/pkg/ui/prompt"
	"github.com/abbott/hardn/pkg/ui/term"
	"github.com/abbott/hardn/pkg/ui/uitest"
)

const (
	readerLogLine = "2026/10/29 08:12:44 [INFO] a line of the log"
	readerLog     = readerLogLine + "\n"
)

// refusingRead is a host whose log refuses the read, as to a user other than root.
type refusingRead struct{ host.System }

func (refusingRead) ScanLines(_ context.Context, name string, _ func(host.Line) bool) (fs.FileInfo, error) {
	return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrPermission}
}

// logHost is a host holding the log; "" holds none.
func logHost(log string) *hostfake.System {
	h := hostfake.New(host.Options{})
	if log != "" {
		h.AddFile(logview.File, []byte(log), 0o644, 0, 0)
	}
	return h
}

// readerSeams replace what runHardn leaves: the host, stdout and its terminal.
type readerSeams struct {
	out      bytes.Buffer
	reached  int // readerHost asked
	terminal bool
	failing  bool
}

func (s *readerSeams) Write(p []byte) (int, error) {
	if s.failing {
		return 0, errors.New("no space left on device")
	}
	return s.out.Write(p)
}

func useReaderHost(t *testing.T, sys host.System, s *readerSeams) {
	t.Helper()
	hostWas, envWas, stdoutWas := readerHost, stdoutEnv, engineStdout
	t.Cleanup(func() { readerHost, stdoutEnv, engineStdout = hostWas, envWas, stdoutWas })
	readerHost = func() host.System { s.reached++; return sys }
	stdoutEnv = func() term.Env {
		if s.terminal {
			return term.Resolve(term.Options{}, term.Facts{StdoutTTY: true, StdinTTY: true, Width: 80, Height: 24, Environ: []string{"TERM=xterm", "LANG=C.UTF-8"}})
		}
		return term.Resolve(term.Options{}, term.Facts{Width: 80, Height: 24})
	}
	engineStdout = s
}

// runReader is hardn with args on sys.
func runReader(t *testing.T, mode hostMode, sys host.System, s *readerSeams, args ...string) result {
	t.Helper()
	return runHardnWith(t, mode, func() { useReaderHost(t, sys, s) }, args...)
}

func asUser(t *testing.T) {
	t.Helper()
	lookupCurrentUser = func() (*osuser.User, error) { return &osuser.User{Uid: "1000", Username: "alice"}, nil }
}

// TestPrintLogAlone: -p as the only operation flag, -n being none, runs ahead of the
// root check, the config load and detectOS; stdout holds the log's lines alone, and
// nothing goes through the provider (decisions 9 (a), 10 (a)).
func TestPrintLogAlone(t *testing.T) {
	for _, args := range [][]string{{"-p"}, {"-n", "-p"}} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			s := &readerSeams{}
			r := runReader(t, hostUnreachable, logHost(readerLog), s, args...)
			if r.ended {
				t.Fatalf("ended with %d; stderr:\n%s", r.code, r.stderr)
			}
			if len(r.reached) != 0 || r.peeks != 0 || len(r.calls) != 0 {
				t.Errorf("reached %v, peeks %d, calls %v; want none", r.reached, r.peeks, r.calls)
			}
			if got := s.out.String(); got != readerLog {
				t.Errorf("stdout %q, want the log alone", got)
			}
			if r.stderr != "" || r.stdout != "" {
				t.Errorf("stderr %q, the logger's %q; want nothing", r.stderr, r.stdout)
			}
		})
	}
}

// TestPrintLogStates: each state's stdout, stderr and exit status, as a user and as
// root: Printed.Stderr's lines with the tool or none; a failed write is a failure.
func TestPrintLogStates(t *testing.T) {
	const missing = "hardn: no log yet: " + logview.File
	const denied = "hardn: can't read " + logview.File + ": permission denied"
	doas := func() host.System {
		h := hostfake.New(host.Options{})
		h.AddFile("/usr/bin/doas", nil, 0o755, 0, 0)
		return refusingRead{h}
	}
	for _, tc := range []struct {
		name     string
		sys      host.System
		user     bool
		terminal bool
		failing  bool
		stdout   string
		stderr   string
		code     int
	}{
		{name: "lines, a user", sys: logHost(readerLog), user: true, stdout: readerLog},
		{name: "no log, a user", sys: logHost(""), user: true, stderr: missing + "\n"},
		{name: "no log, a terminal", sys: logHost(""), user: true, terminal: true, stderr: missing + "\n"},
		{name: "refused, a user", sys: refusingRead{logHost(readerLog)}, user: true, stderr: denied + "\nRun it with sudo.\n", code: 1},
		{name: "refused, a user on doas", sys: doas(), user: true, stderr: denied + "\nRun it with doas.\n", code: 1},
		{name: "refused, root", sys: refusingRead{logHost(readerLog)}, stderr: denied + "\n", code: 1},
		{name: "refused, a terminal", sys: refusingRead{logHost(readerLog)}, user: true, terminal: true, stderr: denied + "\nRun it with sudo.\n", code: 1},
		{name: "a failed write", sys: logHost(readerLog), failing: true, stderr: "hardn: logs print: write: no space left on device\n", code: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := &readerSeams{terminal: tc.terminal, failing: tc.failing}
			r := runHardnWith(t, asRoot, func() {
				useReaderHost(t, tc.sys, s)
				if tc.user {
					asUser(t)
				}
			}, "-p")
			if tc.code == 0 && r.ended || tc.code != 0 && (!r.ended || r.code != tc.code) {
				t.Errorf("ended %v with %d, want %d", r.ended, r.code, tc.code)
			}
			if got := s.out.String(); got != tc.stdout {
				t.Errorf("stdout %q, want %q", got, tc.stdout)
			}
			if r.stderr != tc.stderr {
				t.Errorf("stderr %q, want %q", r.stderr, tc.stderr)
			}
		})
	}
}

// TestPrintLogPositions: alone, nothing has loaded hardn.yml, so a terminal's
// form has no "isn't used" row; after a load, with another flag, it has.
func TestPrintLogPositions(t *testing.T) {
	elsewhere := func(string) (*config.Config, error) {
		c := config.DefaultConfig()
		c.Username, c.LogFile = "alice", "/srv/log/hardn.log"
		return c, nil
	}
	const row = "/srv/log/hardn.log, isn't used"
	t.Run("alone", func(t *testing.T) {
		s := &readerSeams{terminal: true}
		r := runHardnWith(t, asRoot, func() { useReaderHost(t, logHost(readerLog), s); loadConfig = elsewhere }, "-p")
		if len(r.reached) != 0 || r.ended {
			t.Errorf("reached %v, ended %v", r.reached, r.ended)
		}
		if !strings.Contains(s.out.String(), readerLogLine[20:]) || strings.Contains(s.out.String(), "logFile") {
			t.Errorf("stdout:\n%s", s.out.String())
		}
	})
	t.Run("after -d", func(t *testing.T) {
		s := &readerSeams{terminal: true}
		var loaded bool
		r := runHardnWith(t, asRoot, func() {
			useReaderHost(t, logHost(readerLog), s)
			loadConfig = func(p string) (*config.Config, error) { loaded = true; return elsewhere(p) }
		}, "-d", "-p")
		if !loaded || r.ended {
			t.Fatalf("loaded %v, ended %v with %d; stderr:\n%s", loaded, r.ended, r.code, r.stderr)
		}
		if !strings.Contains(s.out.String(), row) {
			t.Errorf("no row %q:\n%s", row, s.out.String())
		}
		if !strings.Contains(r.stdout, "Script completed") {
			t.Errorf("the other operation's lines left stdout:\n%s", r.stdout)
		}
	})
	t.Run("after -d, refused", func(t *testing.T) {
		s := &readerSeams{}
		r := runReader(t, asRoot, refusingRead{logHost(readerLog)}, s, "-d", "-p")
		if !r.ended || r.code != 1 {
			t.Fatalf("ended %v with %d, want 1; stderr:\n%s", r.ended, r.code, r.stderr)
		}
		if !strings.Contains(r.stderr, "can't read") || strings.Contains(r.stderr, "Run it with") {
			t.Errorf("as root stderr %q", r.stderr)
		}
		if !strings.Contains(r.stdout, "Script completed") {
			t.Errorf("the operation after it did not finish:\n%s", r.stdout)
		}
	})
}

// TestLogFileWarning: where hardn.yml loads, a warning only where the key names
// another path, in decision 6 (a)'s words.
func TestLogFileWarning(t *testing.T) {
	for _, tc := range []struct {
		logFile string
		warned  bool
	}{
		{"", false},
		{logview.File, false},
		{"/var/log/../log/hardn.log", false},
		{"/srv/log/hardn.log", true},
	} {
		t.Run(tc.logFile, func(t *testing.T) {
			r := runHardnWith(t, asRoot, func() {
				loadConfig = func(string) (*config.Config, error) {
					c := config.DefaultConfig()
					c.Username, c.LogFile = "alice", tc.logFile
					return c, nil
				}
			}, "-d")
			const words = "hardn.yml's logFile, /srv/log/hardn.log, isn't used"
			if got := strings.Contains(r.stderr, words); got != tc.warned {
				t.Errorf("warned %v, want %v; stderr:\n%s", got, tc.warned, r.stderr)
			}
		})
	}
}

// TestWarningsFollowDetectOS: detectOS clears the terminal (utils.PrintHeader), so what
// loading hardn.yml warns of is said after it, or no one reads it (run: the VMs).
func TestWarningsFollowDetectOS(t *testing.T) {
	const cleared, words = "the terminal cleared", "isn't used"
	r := runHardnWith(t, asRoot, func() {
		loadConfig = func(string) (*config.Config, error) {
			c := config.DefaultConfig()
			c.Username, c.LogFile = "alice", "/srv/log/hardn.log"
			return c, nil
		}
		detected := detectOS
		detectOS = func() (*osdetect.OSInfo, error) {
			_, _ = io.WriteString(stderr, cleared+"\n")
			return detected()
		}
	}, "-d")
	at, said := strings.Index(r.stderr, cleared), strings.Index(r.stderr, words)
	if at < 0 || said < at {
		t.Errorf("the warning at %d, the clear at %d; stderr:\n%s", said, at, r.stderr)
	}
}

// TestSystemDetailsFlags: each form's outcome and exit status (decision 1 (a)): no
// root check and no hardn.yml; a refusal reads nothing and writes nothing on stdout.
func TestSystemDetailsFlags(t *testing.T) {
	const useJSON, runAlone = "use --output json", "run it without system-details"
	for _, tc := range []struct {
		args   []string
		code   int
		json   bool
		stderr []string
	}{
		{args: nil},
		{args: []string{"--output", "json"}, json: true},
		{args: []string{"--json"}, json: true, stderr: []string{"--json is deprecated: " + useJSON}},
		{args: []string{"-O", "json"}, json: true, stderr: []string{"-O json is deprecated: " + useJSON}},
		{args: []string{"--format=json"}, json: true, stderr: []string{"-O json is deprecated: " + useJSON}},
		{args: []string{"-O", "text"}, stderr: []string{"-O text is deprecated: text is the default"}},
		{args: []string{"--output", "json", "-O", "text"}, json: true, stderr: []string{"-O text is deprecated"}},
		{args: []string{"--json=false"}, stderr: []string{"--json is deprecated: " + useJSON}},
		{args: []string{"--output", "json", "--json=false"}, json: true, stderr: []string{"--json is deprecated: " + useJSON}},
		{args: []string{"--yaml"}, code: 2, stderr: []string{"hardn: --yaml is refused: " + useJSON}},
		{args: []string{"--yaml=false"}, code: 2, stderr: []string{"hardn: --yaml is refused: " + useJSON}},
		{args: []string{"-O", "yaml"}, code: 2, stderr: []string{"hardn: -O yaml is refused: " + useJSON}},
		{args: []string{"-O", "nonsense"}, code: 2, stderr: []string{"hardn: -O nonsense is refused: " + useJSON}},
		{args: []string{"--output", "yaml"}, code: 2, stderr: []string{"hardn: --output yaml is refused: " + useJSON}},
		{args: []string{"-S", "disk"}, code: 2, stderr: []string{"hardn: -S is refused: nothing filters the report"}},
		{args: []string{"--section=disk", "--json"}, code: 2, stderr: []string{"hardn: -S is refused"}},
		{args: []string{"-E", "out.txt"}, code: 2, stderr: []string{"hardn: -E is refused: use > file"}},
		{args: []string{"-p"}, code: 2, stderr: []string{"hardn: -p is refused: " + runAlone}},
		{args: []string{"--print-logs", "--json"}, code: 2, stderr: []string{"hardn: -p is refused: " + runAlone}},
		{args: []string{"-c"}, code: 2, stderr: []string{"hardn: -c is refused: " + runAlone}},
		{args: []string{"-d"}, code: 2, stderr: []string{"hardn: -d is refused: " + runAlone}},
		{args: []string{"-d=false"}, code: 2, stderr: []string{"hardn: -d is refused: " + runAlone}},
		{args: []string{"-g"}, code: 2, stderr: []string{"hardn: -g is refused: " + runAlone}},
		{args: []string{"-w"}, code: 2, stderr: []string{"hardn: -w is refused: " + runAlone}},
		{args: []string{"-r"}, code: 2, stderr: []string{"hardn: -r is refused: " + runAlone}},
		{args: []string{"-e"}, code: 2, stderr: []string{"hardn: -e is refused: " + runAlone}},
		{args: []string{"-n"}},
		{args: []string{"--no-color"}},
		{args: []string{"-f", "hardn.yml"}},
	} {
		t.Run(strings.Join(tc.args, " "), func(t *testing.T) {
			s := &readerSeams{}
			r := runReader(t, hostUnreachable, logHost(readerLog), s, append([]string{"system-details"}, tc.args...)...)
			if (tc.code != 0) != r.ended || r.code != tc.code {
				t.Fatalf("ended %v with %d, want %d; stderr:\n%s", r.ended, r.code, tc.code, r.stderr)
			}
			if len(r.reached) != 0 {
				t.Errorf("reached %v: no root check, no hardn.yml", r.reached)
			}
			for _, want := range tc.stderr {
				if !strings.Contains(r.stderr, want) {
					t.Errorf("stderr lacks %q:\n%s", want, r.stderr)
				}
			}
			if len(tc.stderr) == 0 && r.stderr != "" {
				t.Errorf("stderr %q, want nothing", r.stderr)
			}
			if tc.code != 0 {
				if s.out.Len() != 0 || s.reached != 0 {
					t.Errorf("a refusal wrote %q, the host asked %d times", s.out.String(), s.reached)
				}
				return
			}
			var doc struct {
				Mode    string          `json:"mode"`
				Details json.RawMessage `json:"details"`
				Result  struct {
					ExitCode int `json:"exitCode"`
				} `json:"result"`
			}
			err := json.Unmarshal(s.out.Bytes(), &doc)
			var fields map[string]json.RawMessage
			if tc.json && (json.Unmarshal(s.out.Bytes(), &fields) != nil || len(fields) != 10) {
				t.Errorf("%d top-level fields, want the nine and details", len(fields))
			}
			if tc.json && (err != nil || doc.Mode != "check" || len(doc.Details) == 0 || doc.Result.ExitCode != 0) {
				t.Errorf("not the document (%v):\n%s", err, s.out.String())
			}
			if !tc.json && (err == nil || !strings.Contains(s.out.String(), "Memory")) {
				t.Errorf("not the report (%v):\n%s", err, s.out.String())
			}
		})
	}
}

// TestSystemDetailsRootFlagFirst: root's operation flag is refused ahead of the
// command as after it, nothing read.
func TestSystemDetailsRootFlagFirst(t *testing.T) {
	s := &readerSeams{}
	r := runReader(t, hostUnreachable, logHost(readerLog), s, "-p", "system-details")
	if !r.ended || r.code != 2 || !strings.Contains(r.stderr, "hardn: -p is refused") || s.out.Len() != 0 || s.reached != 0 {
		t.Errorf("ended %v with %d; stdout %q, the host asked %d times; stderr:\n%s", r.ended, r.code, s.out.String(), s.reached, r.stderr)
	}
}

// TestSystemDetailsWriteFails: a failed write is said on stderr, exit status 1, in
// either form.
func TestSystemDetailsWriteFails(t *testing.T) {
	for _, args := range [][]string{{"system-details"}, {"system-details", "--output", "json"}} {
		s := &readerSeams{failing: true}
		r := runReader(t, hostUnreachable, logHost(""), s, args...)
		if !r.ended || r.code != 1 || !strings.Contains(r.stderr, "no space left") {
			t.Errorf("%v: ended %v with %d; stderr %q", args, r.ended, r.code, r.stderr)
		}
	}
}

// TestReaderScreens: the hooks run the screens on readerHost and map what they
// return: 0 back (nil), q quits (nav.ErrQuit), the end of the input aborts
// (prompt.ErrAborted); the facts detected per visit, through details.Bounded; the
// Logs screen says c's logFile, under plan only as live.
func TestReaderScreens(t *testing.T) {
	for _, tc := range []struct {
		name  string
		lines []string
		want  error
	}{
		{"back", []string{"0"}, nil},
		{"quit", []string{"q"}, nav.ErrQuit},
		{"the end of the input", nil, prompt.ErrAborted},
	} {
		for _, screen := range []string{"systemDetails", "logs"} {
			t.Run(screen+", "+tc.name, func(t *testing.T) {
				h := logHost(readerLog)
				s := &readerSeams{}
				useReaderHost(t, h, s)
				inputWas, detectWas := engineInput, engineDetect
				t.Cleanup(func() { engineInput, engineDetect = inputWas, detectWas })
				in := prompt.New(uitest.NewTypist(io.Discard, tc.lines...), false)
				engineInput = func() *prompt.Reader { return in }
				var bounded bool
				detects := 0
				engineDetect = func(_ context.Context, sys host.System) (detect.Facts, error) {
					detects++
					bounded = sys != host.System(h)
					return detect.Facts{}, errors.New("detect: not established")
				}
				set := &hookSet{}
				c := config.DefaultConfig()
				c.LogFile, c.DryRun = "/srv/log/hardn.log", true
				withReaderScreens(set, c)
				run := set.systemDetails
				if screen == "logs" {
					run = set.logs
				}
				if err := run(); !errors.Is(err, tc.want) || (tc.want == nil && err != nil) {
					t.Fatalf("= %v, want %v; screen:\n%s", err, tc.want, s.out.String())
				}
				if screen == "logs" {
					if !strings.Contains(s.out.String(), "/srv/log/hardn.log, isn't used") {
						t.Errorf("no logFile row:\n%s", s.out.String())
					}
					if detects != 0 {
						t.Errorf("Logs detected %d times", detects)
					}
				} else if detects != 1 || !bounded {
					t.Errorf("detected %d times, bounded %v; want once, bounded", detects, bounded)
				}
			})
		}
	}
}

// TestPrintOnly: -p runs ahead of the root check only where no other operation flag
// is given, as each of those would otherwise be dropped without a word.
func TestPrintOnly(t *testing.T) {
	printWas := printLogs
	t.Cleanup(func() { printLogs = printWas })
	printLogs = true
	if !printOnly() {
		t.Error("-p alone is not print only")
	}
	for name, flag := range map[string]*bool{
		"create-user": &createUser, "disable-root": &disableRootSSH, "install-linux": &installLinux,
		"install-python": &installPython, "install-all": &installAll, "configure-ufw": &configureUfw,
		"configure-dns": &configureDns, "run-all": &runAll, "configure-sources": &updateSources,
		"setup-sudo-env": &setupSudoEnv,
	} {
		was := *flag
		*flag = true
		if printOnly() {
			t.Errorf("-p with %s is print only: %s would not run", name, name)
		}
		*flag = was
	}
	printLogs = false
	if printOnly() {
		t.Error("no -p is print only")
	}
}

// TestLogsOpenTerm: Open gives less hardn's TERM, which the Logs hook reads.
func TestLogsOpenTerm(t *testing.T) {
	const want = "TERM=screen-256color"
	t.Setenv("TERM", "screen-256color")
	h := logHost(readerLog)
	h.Handler = func(argv []string, _ []byte) hostfake.Response {
		if slices.Equal(argv, []string{"less", "--version"}) {
			return hostfake.Response{Stdout: "less 668 (GNU regular expressions)\n"}
		}
		return hostfake.Response{}
	}
	s := &readerSeams{}
	useReaderHost(t, h, s)
	inputWas := engineInput
	t.Cleanup(func() { engineInput = inputWas })
	in := prompt.New(uitest.NewTypist(io.Discard, "1", "0"), false)
	engineInput = func() *prompt.Reader { return in }
	set := &hookSet{}
	withReaderScreens(set, config.DefaultConfig())
	if err := set.logs(); err != nil {
		t.Fatalf("= %v; screen:\n%s", err, s.out.String())
	}
	opened := false
	for _, c := range h.Calls() {
		if c.Op == "run-interactive" {
			opened = true
			if !slices.Contains(c.Env, want) {
				t.Errorf("less's environment %v, want %s", c.Env, want)
			}
		}
	}
	if !opened {
		t.Errorf("Open ran nothing; screen:\n%s", s.out.String())
	}
}
