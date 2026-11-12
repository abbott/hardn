package main

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/abbott/hardn/pkg/config"
	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/hostfake"
	configscreen "github.com/abbott/hardn/pkg/screens/config"
	"github.com/abbott/hardn/pkg/ui/component"
)

// configYML is a typical hardn.yml, with one key nothing reads.
const configYML = "username: alice\nsshPort: 2230\ndisableRootSSH: true\nconfigureDns: true\nnameservers:\n  - 192.0.2.53\n  - 192.0.2.54\nenableLynis: true\n"

// configReport is hardn config off a terminal for configYML at PATH, found on the
// search path.
const configReport = `# Configuration ################################################

  File ---------------------------------------------------------

   -  Path                 PATH
   -  From                 Search path

  Users --------------------------------------------------------

   -  username             alice
   -  adminNoPassword      false (default)
   -  sshKeys              None (default)

  SSH ----------------------------------------------------------

   -  sshPort              2230
   -  disableRootSSH       true
   -  sshConfigFile        /etc/ssh/sshd_config.d/hardn.conf (default)

  Firewall -----------------------------------------------------

   -  enableUfwSshPolicy   false (default)
   -  ufwAppProfiles       None (default)
   -  ufwTrustedInterfaces None (default)

  DNS ----------------------------------------------------------

   -  configureDns         true
   -  nameservers          2: 192.0.2.53, 192.0.2.54

  Auto Updates -------------------------------------------------

   -  enableUnattendedUpgrades
      false (default)

  Backup -------------------------------------------------------

   -  enableBackups        true (default)
   -  backupPath           /var/backups/hardn (default)

  General ------------------------------------------------------

   -  dryRun               false (default)
   -  confirmTimeout       3m (default)

  Not used -----------------------------------------------------

  [ ] enableLynis          true`

// writeYML writes a hardn.yml holding yml in the test's directory.
func writeYML(t *testing.T, yml string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "hardn.yml")
	if err := os.WriteFile(path, []byte(yml), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// runConfigCmd is hardn config with args, the real load and search of hardn.yml in
// place of runHardn's stubs once setup, when set, has run: a test with neither -f nor
// HARDN_CONFIG sets findConfig, so that none reads this machine's search path.
func runConfigCmd(t *testing.T, s *readerSeams, setup func(), args ...string) result {
	t.Helper()
	return runHardnWith(t, hostUnreachable, func() {
		useReaderHost(t, hostfake.New(host.Options{}), s)
		loadExisting, findConfig = config.LoadExisting, config.FindConfigFile
		loadConfig = func(string) (*config.Config, error) {
			t.Error("loadConfig, which may offer to create hardn.yml, was called")
			return config.DefaultConfig(), nil
		}
		asUser(t)
		if setup != nil {
			setup()
		}
	}, append([]string{"config"}, args...)...)
}

// TestConfigCommand: hardn config prints what main menu 8 shows, with no menu and no
// prompt, exit status 0, as a user other than root: for a file on the search path, by
// -f and by HARDN_CONFIG, for none, and for a named file that is not there. Nothing on
// stderr, the load's warnings neither; no root check, no host.
func TestConfigCommand(t *testing.T) {
	path := writeYML(t, configYML)
	missing := filepath.Join(t.TempDir(), "typo.yml")
	onSearchPath := func() {
		findConfig = func(string) config.Found { return config.Found{Path: path, By: config.FoundSearch, Exists: true} }
		loadExisting = func(string) (*config.Config, error) { return config.LoadExisting(path) }
	}
	for _, tc := range []struct {
		name  string
		setup func()
		args  []string
		want  []string
		lacks string
	}{
		{"the search path", onSearchPath, nil, []string{"   -  Path                 " + path + "\n", "   -  From                 Search path\n", "   -  sshPort              2230\n"}, "not found"},
		{"-f", nil, []string{"-f", path}, []string{"   -  From                 -f\n", "   -  sshPort              2230\n"}, "not found"},
		{"HARDN_CONFIG", func() { t.Setenv("HARDN_CONFIG", path) }, nil, []string{"   -  From                 HARDN_CONFIG\n", "   -  sshPort              2230\n"}, "not found"},
		{"no file", func() {
			findConfig = func(string) config.Found { return config.Found{} }
			loadExisting = func(string) (*config.Config, error) { return config.DefaultConfig(), nil }
		}, nil, []string{"   -  Path                 None found\n", "   -  hardn is using its defaults.\n", "   -  sshPort              22 (default)\n"}, "From"},
		{"-f, not there", nil, []string{"-f", missing}, []string{missing, "not found", "   -  From                 -f\n", "   -  hardn is using its defaults.\n", "   -  sshPort              22 (default)\n"}, "2230"},
		{"HARDN_CONFIG, not there", func() { t.Setenv("HARDN_CONFIG", missing) }, nil, []string{"not found", "   -  From                 HARDN_CONFIG\n", "   -  hardn is using its defaults.\n"}, "2230"},
		{"-n and -u change nothing", onSearchPath, []string{"-n", "-u", "carol"}, []string{"   -  username             alice\n", "   -  dryRun               false (default)\n"}, "carol"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := &readerSeams{}
			r := runConfigCmd(t, s, tc.setup, tc.args...)
			out := s.out.String()
			if r.ended || r.stderr != "" || len(r.reached) != 0 || s.reached != 0 {
				t.Fatalf("ended %v (%d), reached %v, the host %d times; stderr:\n%s", r.ended, r.code, r.reached, s.reached, r.stderr)
			}
			for _, w := range append(tc.want, "# Configuration ###") {
				if !strings.Contains(out, w) {
					t.Errorf("no %q in:\n%s", w, out)
				}
			}
			if strings.Contains(out, tc.lacks) || strings.Contains(out, "0) Return") || strings.Contains(out, "Option") || strings.Contains(out, "\x1b") {
				t.Errorf("%q, a menu, a prompt or an escape in:\n%s", tc.lacks, out)
			}
		})
	}

	// The report whole, and the same as the screen's view of the same load.
	s := &readerSeams{}
	runConfigCmd(t, s, onSearchPath)
	if got, want := s.out.String(), strings.ReplaceAll(configReport, "PATH", path)+"\n"; got != want {
		t.Errorf("the report:\n%s\nwant:\n%s", got, want)
	}
	c, err := config.LoadExisting(path)
	if err != nil {
		t.Fatal(err)
	}
	view := configscreen.View(c, config.Found{Path: path, By: config.FoundSearch, Exists: true})
	off := component.OffTerminal()
	var want bytes.Buffer
	_, _ = io.WriteString(off.Env.Writer(&want), view.Report(off)+"\n")
	if got := s.out.String(); got != want.String() {
		t.Errorf("the report is not the screen's view:\n%s\nwant:\n%s", got, want.String())
	}

	// To a terminal it is the screen's drawing: colour, and a cut where a row is full.
	s = &readerSeams{terminal: true}
	runConfigCmd(t, s, onSearchPath)
	if got, want := s.out.String(), view.Report(component.NewCtx(stdoutEnv()))+"\n"; got != want || !strings.Contains(got, "\x1b[") {
		t.Errorf("on a terminal:\n%q\nwant:\n%q", got, want)
	}
}

// TestConfigCommandFails: a hardn.yml that does not parse, and one the user cannot
// read, are said on stderr in the words of every load, exit status 1, nothing on
// stdout; so is a failed write.
func TestConfigCommandFails(t *testing.T) {
	bad := writeYML(t, "sshPort: [\n")
	s := &readerSeams{}
	r := runConfigCmd(t, s, nil, "-f", bad)
	if got := flat(r.stderr); !r.ended || r.code != 1 || !strings.Contains(got, "Failed to load configuration: failed to parse YAML in config file") || s.out.Len() != 0 {
		t.Errorf("unparsed: ended %v with %d, stdout %q; stderr:\n%s", r.ended, r.code, s.out.String(), r.stderr)
	}

	if os.Geteuid() != 0 { // root reads any file
		secret := writeYML(t, configYML)
		if err := os.Chmod(secret, 0); err != nil {
			t.Fatal(err)
		}
		s = &readerSeams{}
		r = runConfigCmd(t, s, nil, "-f", secret)
		if got := flat(r.stderr); !r.ended || r.code != 1 || !strings.Contains(got, "Failed to load configuration: failed to read config file") || !strings.Contains(got, "permission denied") || s.out.Len() != 0 {
			t.Errorf("unreadable: ended %v with %d, stdout %q; stderr:\n%s", r.ended, r.code, s.out.String(), r.stderr)
		}
	}

	if os.Geteuid() != 0 { // a directory this user can't enter: not "not found"
		shut := writeYML(t, configYML)
		dir := filepath.Dir(shut)
		if err := os.Chmod(dir, 0); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })
		s = &readerSeams{}
		r = runConfigCmd(t, s, nil, "-f", shut)
		if got := flat(r.stderr); !r.ended || r.code != 1 || !strings.Contains(got, "Failed to load configuration") || !strings.Contains(got, "permission denied") || s.out.Len() != 0 {
			t.Errorf("a shut directory: ended %v with %d, stdout %q; stderr:\n%s", r.ended, r.code, s.out.String(), r.stderr)
		}
	}

	s = &readerSeams{failing: true}
	r = runConfigCmd(t, s, nil, "-f", writeYML(t, configYML))
	if !r.ended || r.code != 1 || !strings.Contains(r.stderr, "hardn: configuration report: write: no space left") {
		t.Errorf("a failed write: ended %v with %d; stderr %q", r.ended, r.code, r.stderr)
	}
}

// TestConfigCommandRefuses: root's operation flags are refused in system-details'
// words, exit status 2, ahead of the command as after it, hardn.yml not looked for;
// an argument is cobra's usage error.
func TestConfigCommandRefuses(t *testing.T) {
	for _, args := range [][]string{{"config", "-c"}, {"config", "-d"}, {"config", "-g"}, {"config", "-w"}, {"config", "-r"}, {"config", "-p"}, {"-p", "config"}, {"config", "-d=false"}} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			s := &readerSeams{}
			r := runHardnWith(t, hostUnreachable, func() {
				useReaderHost(t, hostfake.New(host.Options{}), s)
				findConfig = func(string) config.Found { t.Error("hardn.yml looked for"); return config.Found{} }
			}, args...)
			flag := strings.TrimSuffix(strings.Join(args, ""), "=false")
			flag = strings.ReplaceAll(flag, "config", "")
			if want := "hardn: " + flag + " is refused: run it without config\n"; !r.ended || r.code != 2 || r.stderr != want || s.out.Len() != 0 || len(r.reached) != 0 {
				t.Errorf("ended %v with %d, reached %v, stdout %q; stderr %q, want %q", r.ended, r.code, r.reached, s.out.String(), r.stderr, want)
			}
		})
	}
	r := runStart(t, hostfake.New(host.Options{}), "config", "extra")
	if !r.ended || r.code != 2 || r.stdout != "" || !strings.Contains(r.stderr, `unknown command "extra" for "hardn config"`) {
		t.Errorf("an argument: ended %v with %d, stdout %q; stderr:\n%s", r.ended, r.code, r.stdout, r.stderr)
	}
}

// TestConfigCommandWritesNothing: through start, as a user other than root, hardn
// config opens no log and writes no example hardn.yml, and stdout holds the report
// alone; hardn --help lists the command.
func TestConfigCommandWritesNothing(t *testing.T) {
	path := writeYML(t, configYML)
	r := runStartWith(t, hostfake.New(host.Options{}), func() {
		loadExisting, findConfig = config.LoadExisting, config.FindConfigFile
	}, "config", "-f", path)
	if r.ended || r.stderr != "" || !strings.HasPrefix(r.stdout, "# Configuration ###") || !strings.Contains(r.stdout, "   -  sshPort              2230\n") {
		t.Errorf("ended %v (%d); stdout:\n%s\nstderr:\n%s", r.ended, r.code, r.stdout, r.stderr)
	}
	if w := r.written(t); len(w) != 0 || logOpened {
		t.Errorf("wrote %v, the log opened: %v", w, logOpened)
	}
	help := runStart(t, hostfake.New(host.Options{}), "--help")
	if !strings.Contains(help.stdout, "  config         Show hardn.yml: the file hardn reads and each key's value\n") {
		t.Errorf("--help does not list it:\n%s", help.stdout)
	}
}
