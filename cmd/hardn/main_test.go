package main

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	osuser "os/user"
	"strings"
	"testing"

	hardncmd "github.com/abbott/hardn/pkg/cmd"
	"github.com/abbott/hardn/pkg/config"
	"github.com/abbott/hardn/pkg/interfaces"
	"github.com/abbott/hardn/pkg/osdetect"
)

// These tests drive rootCmd as the hardn binary does, with every step that
// reaches the host replaced: the provider's file system and commander by the
// repo's mocks behind a call log, and the root check, config load and OS
// detection by stubs. Nothing here reads this machine's user, config or OS,
// runs a command or writes a file.

// callLog records every call made through the provider, in order.
type callLog struct{ calls []string }

func (l *callLog) add(format string, args ...any) {
	l.calls = append(l.calls, fmt.Sprintf(format, args...))
}

type loggingFS struct {
	inner interfaces.FileSystem
	log   *callLog
}

func (f loggingFS) ReadFile(name string) ([]byte, error) {
	f.log.add("read %s", name)
	return f.inner.ReadFile(name)
}

func (f loggingFS) WriteFile(name string, data []byte, perm fs.FileMode) error {
	f.log.add("write %s", name)
	return f.inner.WriteFile(name, data, perm)
}

func (f loggingFS) MkdirAll(path string, perm fs.FileMode) error {
	f.log.add("mkdir %s", path)
	return f.inner.MkdirAll(path, perm)
}

func (f loggingFS) Stat(name string) (os.FileInfo, error) {
	f.log.add("stat %s", name)
	return f.inner.Stat(name)
}

func (f loggingFS) Remove(name string) error {
	f.log.add("remove %s", name)
	return f.inner.Remove(name)
}

func (f loggingFS) RemoveAll(path string) error {
	f.log.add("rmall %s", path)
	return f.inner.RemoveAll(path)
}

type loggingCommander struct {
	inner interfaces.Commander
	log   *callLog
}

func (c loggingCommander) Execute(command string, args ...string) ([]byte, error) {
	c.log.add("exec %s", strings.Join(append([]string{command}, args...), " "))
	return c.inner.Execute(command, args...)
}

func (c loggingCommander) ExecuteWithInput(input string, command string, args ...string) ([]byte, error) {
	c.log.add("exec %s (stdin)", strings.Join(append([]string{command}, args...), " "))
	return c.inner.ExecuteWithInput(input, command, args...)
}

// exited is what the stubbed exit panics with: the status hardn would
// have ended with.
type exited int

// result is one hardn run.
type result struct {
	stderr  string
	code    int  // exit status, when ended
	ended   bool // the run ended by exit
	calls   []string
	reached []string          // the host steps past the dry-run refusal that ran
	files   map[string]string // the mock file system's files afterwards
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
)

func resetFlags() {
	noColor, configFile, username, dryRun = false, "", "", false
	createUser, disableRootSSH, installLinux, installPython, installAll = false, false, false, false, false
	configureUfw, configureDns, runAll, updateSources, printLogs = false, false, false, false, false
	showVersion, setupSudoEnv, debugUpdates, testUpdateAvailable, testSecurityUpdate = false, false, false, false, false
	cfg = nil
}

func runHardn(t *testing.T, mode hostMode, args ...string) result {
	t.Helper()
	t.Setenv("SUDO_USER", "alice")
	t.Setenv("USER", "root")
	t.Setenv("HARDN_CONFIG", "")

	var r result
	log := &callLog{}
	prev := struct {
		provider *interfaces.Provider
		user     func() (*osuser.User, error)
		load     func(string) (*config.Config, error)
		detect   func() (*osdetect.OSInfo, error)
		exit     func(int)
	}{provider, lookupCurrentUser, loadConfig, detectOS, exit}
	prevStderr := stderr
	t.Cleanup(func() {
		provider, lookupCurrentUser, loadConfig, detectOS, exit = prev.provider, prev.user, prev.load, prev.detect, prev.exit
		stderr = prevStderr
		resetFlags()
		rootCmd.SetArgs(nil)
	})

	mockFS := interfaces.NewMockFileSystem()
	if mode == asRoot {
		mockFS.Directories["/etc/sudoers.d"] = true // -e writes a drop-in there
	}
	provider = &interfaces.Provider{
		FS:        loggingFS{inner: mockFS, log: log},
		Commander: loggingCommander{inner: interfaces.NewMockCommander(), log: log},
		Network:   interfaces.NewMockNetworkOperations(),
	}
	var errOut bytes.Buffer
	stderr = &errOut
	exit = func(code int) { panic(exited(code)) }
	unreachable := errors.New("test: a host step was reached")
	lookupCurrentUser = func() (*osuser.User, error) {
		r.reached = append(r.reached, "root check")
		if mode == asRoot {
			return &osuser.User{Uid: "0", Username: "root"}, nil
		}
		return nil, unreachable
	}
	loadConfig = func(string) (*config.Config, error) {
		r.reached = append(r.reached, "config load")
		if mode == asRoot {
			c := config.DefaultConfig()
			c.Username = "alice"
			c.SshPort = 2230
			c.SshAllowedUsers = []string{"alice"}
			c.Nameservers = []string{"192.0.2.53"}
			return c, nil
		}
		return nil, unreachable
	}
	detectOS = func() (*osdetect.OSInfo, error) {
		r.reached = append(r.reached, "OS detection")
		if mode == asRoot {
			return &osdetect.OSInfo{OsType: "debian", OsVersion: "13", OsCodename: "trixie"}, nil
		}
		return nil, unreachable
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
	r.calls = log.calls
	r.files = map[string]string{}
	for p, b := range mockFS.Files {
		r.files[p] = string(b)
	}
	return r
}

// operationRuns are the CLI operations, each with the arguments that run it
// and the name the refusal gives it.
var operationRuns = []struct {
	args []string
	name string
}{
	{[]string{"-r"}, hardncmd.FlagRunAll},
	{[]string{"-c", "-u", "alice"}, hardncmd.FlagCreateUser},
	{[]string{"-d"}, hardncmd.FlagDisableRoot},
	{[]string{"-w"}, hardncmd.FlagConfigureUFW},
	{[]string{"-g"}, hardncmd.FlagConfigureDNS},
	{[]string{"-e"}, hardncmd.FlagSetupSudoEnv},
	{[]string{"setup-sudo-env"}, hardncmd.CommandSetupSudoEnv},
}

// TestDryRunRefusesOperations checks V1's gate: -n with any operation is
// refused with exit status 2 and a message naming the operation, before
// the root check, and nothing at all goes through the provider.
func TestDryRunRefusesOperations(t *testing.T) {
	for _, op := range operationRuns {
		for _, n := range []string{"-n", "--dry-run"} {
			args := append([]string{n}, op.args...)
			t.Run(strings.Join(args, " "), func(t *testing.T) {
				r := runHardn(t, hostUnreachable, args...)
				if !r.ended || r.code != hardncmd.ExitUsage {
					t.Fatalf("ended %v with %d, want exit status %d; stderr:\n%s", r.ended, r.code, hardncmd.ExitUsage, r.stderr)
				}
				for _, want := range []string{op.name, "-n/--dry-run only previews", "Nothing was changed."} {
					if !strings.Contains(r.stderr, want) {
						t.Errorf("stderr lacks %q:\n%s", want, r.stderr)
					}
				}
				if len(r.reached) != 0 {
					t.Errorf("the refusal came after %v", r.reached)
				}
				if len(r.calls) != 0 {
					t.Errorf("calls through the provider under -n: %v", r.calls)
				}
			})
		}
	}
	t.Run("several operations are all named", func(t *testing.T) {
		r := runHardn(t, hostUnreachable, "-d", "-p", "-n", "-w", "-g")
		if !r.ended || r.code != hardncmd.ExitUsage || len(r.calls) != 0 || len(r.reached) != 0 {
			t.Fatalf("ended %v with %d, reached %v, calls %v", r.ended, r.code, r.reached, r.calls)
		}
		want := "can't be combined with " + hardncmd.FlagDisableRoot + ", " + hardncmd.FlagConfigureUFW + " and " + hardncmd.FlagConfigureDNS + ", which apply them."
		if !strings.Contains(r.stderr, want) {
			t.Errorf("stderr lacks %q:\n%s", want, r.stderr)
		}
		if strings.Contains(r.stderr, "print-logs") {
			t.Errorf("-p only reads, but the refusal names it:\n%s", r.stderr)
		}
	})
}

// TestDryRunAloneIsNotRefused checks that -n without an operation, or with
// only -p, still reaches the root check (and then the menus, in dry-run
// mode); the stubbed root check fails, so the run ends there with status 1.
func TestDryRunAloneIsNotRefused(t *testing.T) {
	for _, args := range [][]string{{"-n"}, {"-n", "-p"}, {"-d"}} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			r := runHardn(t, hostUnreachable, args...)
			if !r.ended || r.code != 1 || len(r.reached) != 1 || r.reached[0] != "root check" {
				t.Fatalf("ended %v with %d after %v, want status 1 at the root check; stderr:\n%s", r.ended, r.code, r.reached, r.stderr)
			}
			if r.stderr != "" || len(r.calls) != 0 {
				t.Errorf("stderr %q, calls %v", r.stderr, r.calls)
			}
		})
	}
}

// TestOperationsApplyWithoutDryRun is the control for the refusal test:
// the same operations, without -n, past a root check, config load and OS
// detection that succeed, do write files and run commands through the same
// provider, so the empty call log under -n is the gate's doing.
func TestOperationsApplyWithoutDryRun(t *testing.T) {
	for _, op := range operationRuns {
		t.Run(strings.Join(op.args, " "), func(t *testing.T) {
			r := runHardn(t, asRoot, op.args...)
			if r.ended {
				t.Fatalf("ended with %d; stderr:\n%s", r.code, r.stderr)
			}
			changes := 0
			for _, c := range r.calls {
				if strings.HasPrefix(c, "write ") || strings.HasPrefix(c, "exec ") {
					changes++
				}
			}
			if changes == 0 {
				t.Errorf("no write or command recorded; calls: %v", r.calls)
			}
		})
	}
}

// TestCreateUserAuthorizedKeysFile checks V5's gate through the real -c
// path: hardn.conf names both v0.3.2's files on one AuthorizedKeysFile
// line, the first of which is where -c writes alice's key.
func TestCreateUserAuthorizedKeysFile(t *testing.T) {
	r := runHardn(t, asRoot, "-c", "-u", "alice")
	if r.ended {
		t.Fatalf("ended with %d; stderr:\n%s", r.code, r.stderr)
	}
	conf := r.files["/etc/ssh/sshd_config.d/hardn.conf"]
	var lines []string
	for _, l := range strings.Split(conf, "\n") {
		if strings.HasPrefix(l, "AuthorizedKeysFile") {
			lines = append(lines, l)
		}
	}
	if want := "AuthorizedKeysFile .ssh/authorized_keys .ssh_%u/authorized_keys"; len(lines) != 1 || lines[0] != want {
		t.Errorf("AuthorizedKeysFile lines %q, want just %q; hardn.conf:\n%s", lines, want, conf)
	}
}

// TestUpdateFlagsSayDisabled checks that the update-check flags, which do
// nothing while pkg/version's updateCheckEnabled is false (V26), say so in
// hardn --help.
func TestUpdateFlagsSayDisabled(t *testing.T) {
	for _, name := range []string{"debug-updates", "test-update", "test-security-update"} {
		f := rootCmd.PersistentFlags().Lookup(name)
		if f == nil || !strings.HasSuffix(f.Usage, "(no effect: update checks are disabled)") {
			t.Errorf("--%s: %+v", name, f)
		}
	}
}
