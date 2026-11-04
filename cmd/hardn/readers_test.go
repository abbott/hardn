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
	"github.com/abbott/hardn/pkg/screens/mainmenu"
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
			if len(r.reached) != 0 {
				t.Errorf("reached %v; want none", r.reached)
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
// root: Printed.Stderr's lines with the tool or none (as root, or with neither sudo
// nor doas); a failed write is a failure.
func TestPrintLogStates(t *testing.T) {
	const missing = "hardn: no log yet: " + logview.File
	const denied = "hardn: can't read " + logview.File + ": permission denied"
	doas := func() host.System {
		h := hostfake.New(host.Options{})
		h.AddFile("/usr/bin/doas", nil, 0o755, 0, 0)
		return refusingRead{h}
	}
	sudo := func() host.System { // the words say sudo (with neither tool, no line)
		h := logHost(readerLog)
		h.AddFile("/usr/bin/sudo", nil, 0o755, 0, 0)
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
		{name: "refused, a user", sys: sudo(), user: true, stderr: denied + "\nRun it with sudo.\n", code: 1},
		{name: "refused, a user, neither sudo nor doas", sys: refusingRead{logHost(readerLog)}, user: true, stderr: denied + "\n", code: 1},
		{name: "refused, a user on doas", sys: doas(), user: true, stderr: denied + "\nRun it with doas.\n", code: 1},
		{name: "refused, root", sys: refusingRead{logHost(readerLog)}, stderr: denied + "\n", code: 1},
		{name: "refused, a terminal", sys: sudo(), user: true, terminal: true, stderr: denied + "\nRun it with sudo.\n", code: 1},
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
	// After -g, on a host whose resolv.conf is as hardn.yml lists it (newDNSHost's
	// configured), so that -g writes nothing: its report on stdout first. (-g, not
	// -d, since -d left the provider for the engine: no legacy flag runs before
	// -p now.)
	afterG := func(t *testing.T, sys host.System, s *readerSeams) result {
		t.Helper()
		h, cfg := newDNSHost(t, "configured", dnsTestList)
		cfg.LogFile = "/srv/log/hardn.log"
		r, _ := runG(t, h, cfg, func(*dnsSeams) { useReaderHost(t, sys, s) }, "-g", "-p")
		if w := h.writes(); len(w) > 0 {
			t.Errorf("-g wrote %q", w)
		}
		return r
	}
	t.Run("after -g", func(t *testing.T) {
		s := &readerSeams{terminal: true}
		r := afterG(t, logHost(readerLog), s)
		if r.ended {
			t.Fatalf("ended with %d; stderr:\n%s", r.code, r.stderr)
		}
		if w := inOrder(s.out.String(), "Nothing needs changing.", row); w != "" {
			t.Errorf("stdout lacks %q, -g's report then the row:\n%s", w, s.out.String())
		}
	})
	t.Run("after -g, refused", func(t *testing.T) {
		s := &readerSeams{}
		r := afterG(t, refusingRead{logHost(readerLog)}, s)
		if !r.ended || r.code != 1 {
			t.Fatalf("ended %v with %d, want 1; stderr:\n%s", r.ended, r.code, r.stderr)
		}
		if !strings.Contains(r.stderr, "can't read") || strings.Contains(r.stderr, "Run it with") {
			t.Errorf("as root stderr %q", r.stderr)
		}
		if !strings.Contains(s.out.String(), "Nothing needs changing.") {
			t.Errorf("the operation before it did not finish:\n%s", s.out.String())
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
				onDNSHost(t)
				loadConfig = func(string) (*config.Config, error) {
					c := config.DefaultConfig()
					c.Username, c.LogFile = "alice", tc.logFile
					return c, nil
				}
			}, "-r")
			const words = "hardn.yml's logFile, /srv/log/hardn.log, isn't used"
			if got := strings.Contains(r.stderr, words); got != tc.warned {
				t.Errorf("warned %v, want %v; stderr:\n%s", got, tc.warned, r.stderr)
			}
		})
	}
}

// TestUfwPolicyWarning: where hardn.yml loads, a warning for each ufw policy
// the file sets, in decision 8's words (slice 4), the file still accepted:
// none where it leaves the key out, or writes "" as origin/main's SaveConfig
// does, and none for ufwAllowedPorts, which waits for slice 8.
func TestUfwPolicyWarning(t *testing.T) {
	const incoming, outgoing = "hardn.yml's ufwDefaultIncomingPolicy isn't used", "hardn.yml's ufwDefaultOutgoingPolicy isn't used"
	for _, tc := range []struct {
		name, yml string
		inWarned  bool
		outWarned bool
	}{
		{"neither named", "username: alice\n", false, false},
		{"both empty, as SaveConfig writes", "username: alice\nufwDefaultIncomingPolicy: \"\"\nufwDefaultOutgoingPolicy: \"\"\n", false, false},
		{"incoming", "username: alice\nufwDefaultIncomingPolicy: deny\n", true, false},
		{"outgoing", "username: alice\nufwDefaultOutgoingPolicy: allow\n", false, true},
		{"both", "username: alice\nufwDefaultIncomingPolicy: deny\nufwDefaultOutgoingPolicy: allow\n", true, true},
		{"ufwAllowedPorts", "username: alice\nufwAllowedPorts: [22]\n", false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			const path = "/etc/hardn/hardn.yml"
			loaded := false
			r := runHardnWith(t, asRoot, func() {
				onDNSHost(t)
				loadConfig = func(string) (*config.Config, error) {
					f := hostfake.New(host.Options{})
					f.AddFile(path, []byte(tc.yml), 0o644, 0, 0)
					c, err := config.Read(context.Background(), f, path)
					loaded = err == nil
					return c, err
				}
			}, "-r")
			if !loaded {
				t.Fatalf("hardn.yml did not load; stderr:\n%s", r.stderr)
			}
			if got := strings.Contains(r.stderr, incoming); got != tc.inWarned {
				t.Errorf("incoming warned %v, want %v; stderr:\n%s", got, tc.inWarned, r.stderr)
			}
			if got := strings.Contains(r.stderr, outgoing); got != tc.outWarned {
				t.Errorf("outgoing warned %v, want %v; stderr:\n%s", got, tc.outWarned, r.stderr)
			}
			if r.ended {
				t.Errorf("-r did not run on the file:\n%s\nstderr:\n%s", r.stdout, r.stderr)
			}
		})
	}
}

// TestSudoNoPasswordWarning: where hardn.yml loads, sudoNoPassword: false
// warns once, in slice 5's decision 7 (a) words, the file still accepted
// (since slice 8, Run All's decision 4 (a) too);
// true, set or left out (the default), never warns, and neither do
// username and sshKeys, Create's defaults.
func TestSudoNoPasswordWarning(t *testing.T) {
	const words = "hardn.yml's sudoNoPassword: false isn't used"
	for _, tc := range []struct {
		name, yml string
		warned    bool
	}{
		{"left out", "username: alice\n", false},
		{"true", "username: alice\nsudoNoPassword: true\n", false},
		{"false", "username: alice\nsudoNoPassword: false\n", true},
		{"username and sshKeys", "username: deploy\nsshKeys:\n  - ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIAEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEB deploy@ci\n", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			const path = "/etc/hardn/hardn.yml"
			loaded := false
			r := runHardnWith(t, asRoot, func() {
				onDNSHost(t)
				loadConfig = func(string) (*config.Config, error) {
					f := hostfake.New(host.Options{})
					f.AddFile(path, []byte(tc.yml), 0o644, 0, 0)
					c, err := config.Read(context.Background(), f, path)
					loaded = err == nil
					return c, err
				}
			}, "-r")
			if !loaded {
				t.Fatalf("hardn.yml did not load; stderr:\n%s", r.stderr)
			}
			flat := strings.ReplaceAll(r.stderr, "\n# ", " ") // the callout wraps its lines
			if n := strings.Count(flat, words); n != map[bool]int{true: 1}[tc.warned] {
				t.Errorf("warned %d times, want warned %v; stderr:\n%s", n, tc.warned, r.stderr)
			}
			if r.ended {
				t.Errorf("-r did not run on the file:\n%s\nstderr:\n%s", r.stdout, r.stderr)
			}
		})
	}
	if w := loadWarnings(config.DefaultConfig()); strings.Contains(w, "sudoNoPassword") {
		t.Errorf("hardn's defaults warn:\n%s", w)
	}
}

// TestUnusedKeysWarning: where hardn.yml loads, each key nothing reads
// warns once, in slice 6's decision 9 (a) and slice 8's decision 4 (a)
// words, where its value is not config.DefaultConfig's: permitRootLogin,
// as RootLogin reads it ("yes", prohibit-password, which loads since the
// legacy-left unit's decision 6 (a), or the deprecated true, whose deprecation
// is said once beside it; false and "no", the default, only the
// deprecation or nothing), sshAllowedUsers, sshListenAddress, sshKeyPath,
// ufwAllowedPorts, enableAppArmor, enableLynis and logsconfig; set to the default, as
// origin/main's SaveConfig writes it, none warns. disableRootSSH, which
// Run All reads, sshPort and sshConfigFile never warn. Through hardn,
// the warning is on stderr, once.
func TestUnusedKeysWarning(t *testing.T) {
	const deprecated = "is deprecated"
	words := func(key string) string { return "hardn.yml's " + key + " isn't used" }
	keys := []string{"permitRootLogin", "sshAllowedUsers", "sshListenAddress", "sshKeyPath", "ufwAllowedPorts", "enableAppArmor", "enableLynis",
		"disableRootSSH", "sshPort", "sshConfigFile", "logsconfig"}
	for _, tc := range []struct {
		name, yml  string
		warned     []string // the keys warned of
		deprecated bool     // the bool's deprecation said
	}{
		{"left out", "username: alice\n", nil, false},
		{"the defaults, as SaveConfig writes them", "username: alice\npermitRootLogin: \"no\"\nsshAllowedUsers: []\nsshListenAddress: 0.0.0.0\n" +
			"disableRootSSH: false\nsshKeyPath: .ssh_%u\nsshPort: 22\nsshConfigFile: /etc/ssh/sshd_config.d/hardn.conf\n" +
			"ufwAllowedPorts: []\nenableAppArmor: false\nenableLynis: false\n", nil, false},
		{"permitRootLogin yes", "username: alice\npermitRootLogin: \"yes\"\n", []string{"permitRootLogin"}, false},
		{"permitRootLogin prohibit-password, loaded (decision 6 (a))", "username: alice\npermitRootLogin: prohibit-password\n", []string{"permitRootLogin"}, false},
		{"permitRootLogin true, deprecated", "username: alice\npermitRootLogin: true\n", []string{"permitRootLogin"}, true},
		{"permitRootLogin false, deprecated", "username: alice\npermitRootLogin: false\n", nil, true},
		{"sshAllowedUsers", "username: alice\nsshAllowedUsers:\n  - george\n", []string{"sshAllowedUsers"}, false},
		{"sshListenAddress", "username: alice\nsshListenAddress: 192.0.2.10\n", []string{"sshListenAddress"}, false},
		{"sshKeyPath", "username: alice\nsshKeyPath: .ssh/authorized_keys\n", []string{"sshKeyPath"}, false},
		{"logsconfig, origin/main's as its save wrote it", "username: alice\nlogsconfig:\n  logfilepath: /var/log/hardn.log\n", nil, false},
		{"logsconfig", "username: alice\nlogsconfig:\n  logfilepath: /srv/log/hardn.log\n", []string{"logsconfig"}, false},
		{"ufwAllowedPorts", "username: alice\nufwAllowedPorts: [22, 443]\n", []string{"ufwAllowedPorts"}, false},
		{"enableAppArmor", "username: alice\nenableAppArmor: true\n", []string{"enableAppArmor"}, false},
		{"enableLynis", "username: alice\nenableLynis: true\n", []string{"enableLynis"}, false},
		{"disableRootSSH, read by Run All", "username: alice\ndisableRootSSH: true\n", nil, false},
		{"sshPort and sshConfigFile", "username: alice\nsshPort: 2230\nsshConfigFile: /etc/ssh/sshd_config.d/old.conf\n", nil, false},
		{"all seven", "username: alice\npermitRootLogin: \"yes\"\nsshAllowedUsers: [george]\nsshListenAddress: 192.0.2.10\n" +
			"sshKeyPath: .ssh/authorized_keys\nufwAllowedPorts: [22]\nenableAppArmor: true\nenableLynis: true\n", keys[:7], false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			const path = "/etc/hardn/hardn.yml"
			f := hostfake.New(host.Options{})
			f.AddFile(path, []byte(tc.yml), 0o644, 0, 0)
			c, err := config.Read(context.Background(), f, path)
			if err != nil {
				t.Fatal(err)
			}
			w := strings.ReplaceAll(loadWarnings(c), "\n# ", " ") // the callout wraps its lines
			for _, key := range keys {
				if n := strings.Count(w, words(key)); n != map[bool]int{true: 1}[slices.Contains(tc.warned, key)] {
					t.Errorf("%s warned %d times, want warned %v:\n%s", key, n, slices.Contains(tc.warned, key), w)
				}
			}
			if n := strings.Count(w, deprecated); n != map[bool]int{true: 1}[tc.deprecated] {
				t.Errorf("deprecated said %d times, want said %v:\n%s", n, tc.deprecated, w)
			}
		})
	}
	if w := loadWarnings(config.DefaultConfig()); w != "" {
		t.Errorf("hardn's defaults warn:\n%s", w)
	}
	r := runHardnWith(t, asRoot, func() {
		onDNSHost(t)
		loadConfig = func(string) (*config.Config, error) {
			const path = "/etc/hardn/hardn.yml"
			f := hostfake.New(host.Options{})
			f.AddFile(path, []byte("username: alice\nenableLynis: true\n"), 0o644, 0, 0)
			return config.Read(context.Background(), f, path)
		}
	}, "-r")
	flat := strings.ReplaceAll(r.stderr, "\n# ", " ")
	if r.ended || strings.Count(flat, words("enableLynis")) != 1 || strings.Contains(r.stdout, "isn't used") {
		t.Errorf("ended %v with %d; stderr:\n%s\nstdout:\n%s", r.ended, r.code, r.stderr, r.stdout)
	}
}

// TestRetiredKeysWarning: each key that nothing reads since the legacy
// package, sources and localization code went (config.Retired) warns once,
// as ufwAllowedPorts does, where its value is not config.DefaultConfig's;
// at the default, as origin/main's SaveConfig wrote it, none warns, and
// the example hardn.yml sets none of them.
func TestRetiredKeysWarning(t *testing.T) {
	words := func(key string) string { return "hardn.yml's " + key + " isn't used" }
	set := map[string]string{
		"dmzSubnet": "dmzSubnet: 192.0.2\n", "linuxCorePackages": "linuxCorePackages: [git]\n",
		"linuxDmzPackages": "linuxDmzPackages: [fail2ban]\n", "linuxLabPackages": "linuxLabPackages: [tree]\n",
		"pythonPackages": "pythonPackages: [python3-pip]\n", "nonWslPythonPackages": "nonWslPythonPackages: [python3-venv]\n",
		"pythonPipPackages": "pythonPipPackages: [requests]\n", "alpineCorePackages": "alpineCorePackages: [bash]\n",
		"alpineDmzPackages": "alpineDmzPackages: [jq]\n", "alpineLabPackages": "alpineLabPackages: [mosh]\n",
		"alpinePythonPackages": "alpinePythonPackages: [py3-pip]\n", "debianRepos": "debianRepos: [deb http://192.0.2.1/debian trixie main]\n",
		"proxmoxSrcRepos": "proxmoxSrcRepos: [x]\n", "proxmoxCephRepo": "proxmoxCephRepo: [x]\n",
		"proxmoxEnterpriseRepo": "proxmoxEnterpriseRepo: [x]\n", "proxmoxPackagePatterns": "proxmoxPackagePatterns: [x]\n",
		"alpineTestingRepo": "alpineTestingRepo: true\n", "useUvPackageManager": "useUvPackageManager: true\n",
		"lang": "lang: en_US.UTF-8\n", "language": "language: en_US:en\n", "lcAll": "lcAll: en_US.UTF-8\n",
		"tz": "tz: America/New_York\n", "pythonUnbuffered": "pythonUnbuffered: \"0\"\n",
	}
	read := func(t *testing.T, yml string) *config.Config {
		t.Helper()
		const path = "/etc/hardn/hardn.yml"
		f := hostfake.New(host.Options{})
		f.AddFile(path, []byte(yml), 0o644, 0, 0)
		c, err := config.Read(context.Background(), f, path)
		if err != nil {
			t.Fatal(err)
		}
		return c
	}
	for key, yml := range set {
		t.Run(key, func(t *testing.T) {
			w := strings.ReplaceAll(loadWarnings(read(t, "username: alice\n"+yml)), "\n# ", " ")
			if n := strings.Count(w, "isn't used"); n != 1 || strings.Count(w, words(key)) != 1 {
				t.Errorf("warned:\n%s\nwant %q once, alone", w, words(key))
			}
		})
	}
	defaults := "username: alice\ndmzSubnet: \"\"\nlinuxCorePackages: []\nlinuxDmzPackages: []\nlinuxLabPackages: []\n" +
		"pythonPackages: []\nnonWslPythonPackages: []\npythonPipPackages: []\nalpineCorePackages: []\nalpineDmzPackages: []\n" +
		"alpineLabPackages: []\nalpinePythonPackages: []\ndebianRepos: []\nproxmoxSrcRepos: []\nproxmoxCephRepo: []\n" +
		"proxmoxEnterpriseRepo: []\nproxmoxPackagePatterns: []\nalpineTestingRepo: false\nuseUvPackageManager: false\n" +
		"lang: \"\"\nlanguage: \"\"\nlcAll: \"\"\ntz: \"\"\npythonUnbuffered: \"1\"\n"
	if w := loadWarnings(read(t, defaults)); strings.Contains(w, "isn't used") {
		t.Errorf("the defaults, as SaveConfig wrote them, warn:\n%s", w)
	}
	if keys := read(t, config.ExampleConfigContent).Retired(); len(keys) != 0 {
		t.Errorf("the example hardn.yml sets %v", keys)
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
				c := config.DefaultConfig()
				c.LogFile, c.DryRun = "/srv/log/hardn.log", true
				set := withReaderScreens(mainmenu.Options{}, c)
				run := set.SystemDetails
				if screen == "logs" {
					run = set.Logs
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
		"create-user": &createUser, "disable-root": &disableRootSSH, "configure-ufw": &configureUfw,
		"configure-dns": &configureDns, "run-all": &runAll, "setup-sudo-env": &setupSudoEnv,
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
	set := withReaderScreens(mainmenu.Options{}, config.DefaultConfig())
	if err := set.Logs(); err != nil {
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
