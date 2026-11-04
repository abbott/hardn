package main

import (
	"context"
	"errors"
	"io/fs"
	"os"
	osuser "os/user"
	"slices"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/abbott/hardn/pkg/config"
	"github.com/abbott/hardn/pkg/engine/header"
	"github.com/abbott/hardn/pkg/engine/hold"
	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/backup"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/hostfake"
	"github.com/abbott/hardn/pkg/host/revert"
	"github.com/abbott/hardn/pkg/host/sudoers"
	"github.com/abbott/hardn/pkg/output"
	"github.com/abbott/hardn/pkg/screens/mainmenu"
	"github.com/abbott/hardn/pkg/ui/nav"
	"github.com/abbott/hardn/pkg/ui/prompt"
)

// The Environment item's tests (refactor plan slice 9, unit (d)): the
// Environment screen on hostfake, as the Environment control's and
// screen's tests have their hosts (pkg/screens/environment's
// helpers_test.go, which a test of another package cannot share), reached
// through this package's seams (useEnvHost): nothing on this machine is
// read, run or written. visudo and doas -C pass every file but where a
// host says otherwise: the tests' model, not the tools. Synthetic values
// only: web-01, alice, 192.0.2.0/24.

// envTestFile is hardn's file keeping HARDN_CONFIG for alice on sudo.
const envTestFile = sudoers.DropInDir + "/hardn-env-alice"

// envTestSudoers is Debian 13's /etc/sudoers in shape, its includedir
// last.
const envTestSudoers = "Defaults\tenv_reset\nroot\tALL=(ALL:ALL) ALL\n%sudo\tALL=(ALL:ALL) ALL\n\n@includedir /etc/sudoers.d\n"

// The environments hardn runs in: alice's sudo or doas, a root login.
var (
	envViaSudo = map[string]string{"SUDO_USER": "alice", "USER": "root"}
	envViaDoas = map[string]string{"DOAS_USER": "alice", "USER": "root"}
	envAtRoot  = map[string]string{"USER": "root"}
)

// envTestHost is one Environment host: the fake with its facts (a
// dnsHost, which useDNSHost's seams take), and hardn's environment there.
type envTestHost struct {
	*dnsHost
	env map[string]string
}

// newEnvHost returns the Environment host named name: its elevation files
// and tools, the directories the engine writes in, the kernel's hostname,
// and hardn's binary, as the revert timer of a lockout-risk plan needs it
// (revertTestHardn, useDNSHost's revertExecutable). The hosts:
//
//   - debian13: sudo, alice ran sudo hardn, HARDN_CONFIG not kept;
//   - kept: the same, hardn-env-alice keeping it;
//   - edited: hardn-env-alice edited by hand (SUDOERS_FILE_EDITED);
//   - visudo-fails: visudo -c refuses the configuration;
//   - root-login: Debian 13, root logged in (ENVIRONMENT_NO_USER);
//   - alpine324: doas through 20-wheel.conf, no rule of hardn's for alice
//     (SUDOERS_ENV_NEEDS_GRANT);
//   - doas-password: Alpine 3.23's cloud image, doas, hardn's rule for
//     alice asking for a password and keeping nothing, so that keeping
//     HARDN_CONFIG rewrites it: a lockout-risk plan;
//   - pve9: Proxmox VE 9, neither tool, root logged in (SUDOERS_NO_TOOL).
func newEnvHost(t *testing.T, name string) *envTestHost {
	t.Helper()
	h := &envTestHost{dnsHost: &dnsHost{System: hostfake.New(host.Options{})}, env: envViaSudo}
	for _, d := range []string{"/etc", "/etc/hardn", "/etc/init.d", "/etc/runlevels", "/etc/runlevels/default", "/var", "/var/lib",
		"/var/backups", "/run", "/var/log", "/usr", "/usr/local", "/usr/local/bin", "/proc", "/proc/sys", "/proc/sys/kernel"} {
		h.AddDir(d, 0o755, 0, 0)
	}
	h.AddFile(revertTestHardn, []byte("ELF"), 0o755, 0, 0)
	h.AddFile(header.HostnameFile, []byte("web-01\n"), 0o644, 0, 0)
	visudoFails := false
	switch name {
	case "debian13", "kept", "edited", "visudo-fails", "root-login":
		h.AddFile(sudoers.MainConfig, []byte(envTestSudoers), 0o440, 0, 0)
		h.AddDir(sudoers.DropInDir, 0o755, 0, 0)
		h.facts = dnsFacts(detect.DistroDebian, "13", detect.Systemd)
		h.facts.Elevation = detect.Elevation{Sudo: detect.Tool{Present: detect.Yes, Path: "/usr/bin/sudo"}, SudoFlavor: detect.SudoOriginal, Doas: detect.Tool{Present: detect.No}}
	case "alpine324", "doas-password":
		file, rule, version := "20-wheel.conf", "permit persist :wheel\n", "3.24"
		if name == "doas-password" {
			file, rule, version = "wheel.conf", "permit nopass :wheel\n", "3.23"
		}
		h.AddFile(sudoers.DoasConfig, []byte("# see doas.conf(5) for configuration details\n\n# permit persist :wheel\n"), 0o640, 0, 0)
		h.AddDir(sudoers.DoasDir, 0o750, 0, 0)
		h.AddFile(sudoers.DoasDir+"/"+file, []byte(rule), 0o640, 0, 0)
		h.facts = dnsFacts(detect.DistroAlpine, version, detect.OpenRC)
		h.facts.Elevation = detect.Elevation{Sudo: detect.Tool{Present: detect.No}, Doas: detect.Tool{Present: detect.Yes, Path: "/usr/bin/doas"}}
		h.env = envViaDoas
	case "pve9":
		h.facts = dnsFacts(detect.DistroProxmox, "9", detect.Systemd)
		h.facts.Elevation = detect.Elevation{Sudo: detect.Tool{Present: detect.No}, Doas: detect.Tool{Present: detect.No}}
		h.env = envAtRoot
	default:
		t.Fatalf("no host %q", name)
	}
	switch name {
	case "kept":
		h.with(t, sudoers.Policy{User: "alice", Env: []string{"HARDN_CONFIG"}})
	case "edited":
		h.AddFile(envTestFile, []byte(sudoers.Header+"Defaults:alice env_keep += \"HARDN_CONFIG\"\nalice ALL=(ALL) NOPASSWD: ALL\n"), 0o440, 0, 0)
	case "visudo-fails":
		visudoFails = true
	case "root-login":
		h.env = envAtRoot
	case "doas-password":
		h.with(t, sudoers.Policy{User: "alice", Grant: sudoers.GrantPassword})
	}
	h.Handler = func(argv []string, _ []byte) hostfake.Response {
		cmd := strings.Join(argv, " ")
		switch {
		case cmd == "visudo -c" && visudoFails:
			return hostfake.Response{Stderr: sudoers.DropInDir + "/alice:1:21: syntax error\n", ExitCode: 1}
		case cmd == "visudo -c":
			return hostfake.Response{Stdout: sudoers.MainConfig + ": parsed OK\n"}
		case len(argv) == 3 && argv[0] == "visudo" && argv[1] == "-cf":
			return hostfake.Response{Stdout: argv[2] + ": parsed OK\n"}
		case len(argv) == 3 && argv[0] == "doas" && argv[1] == "-C":
			return hostfake.Response{}
		}
		return hostfake.Response{Err: hostfake.ErrUnscripted, ExitCode: -1}
	}
	return h
}

// with writes hardn's files for pol on h as sudoers.PlanFor plans them,
// so that their bytes are the package's own.
func (h *envTestHost) with(t *testing.T, pol sudoers.Policy) {
	t.Helper()
	st, err := sudoers.Read(context.Background(), h, sudoers.Choose(h.facts, sudoers.ToolUnknown), []string{pol.User})
	if err != nil {
		t.Fatal(err)
	}
	p, err := sudoers.PlanFor(st, []sudoers.Policy{pol})
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range p.Changes {
		h.AddFile(c.Path, c.Data, c.Mode, 0, 0)
	}
}

// useEnvHost points the seams at h as useDNSHost does, hardn's
// environment h's (environmentGetenv), the user typing lines.
func useEnvHost(t *testing.T, h *envTestHost, lines ...string) *dnsSeams {
	t.Helper()
	s := useDNSHost(t, h.dnsHost, lines...)
	environmentGetenv = func(k string) string { return h.env[k] }
	return s
}

// TestEnvironmentScreen: the main menu's Environment item runs the
// Environment screen on the host hardn builds, through the engine with
// engineOptions: keeping HARDN_CONFIG on Debian 13 plans, asks, and writes
// hardn-env-alice alone, then says it takes effect at the next sudo
// hardn; a dry run, as the configuration says when the item is chosen,
// stops at the plan and writes nothing, as do a decline and the guide; a
// host the control refuses (doas, no rule of hardn's) is refused before
// asking, nothing planned; 0 goes back (nil), q quits (nav.ErrQuit), the
// end of the input aborts (prompt.ErrAborted). The main menu's
// configuration is as it was. The signals
// are registered for the apply alone, and never while a prompt waits.
func TestEnvironmentScreen(t *testing.T) {
	for _, tc := range []struct {
		name, host string
		dryRun     bool
		lines      []string
		want       error  // what the screen returns: nil, nav.ErrQuit, prompt.ErrAborted
		said       string // on the screen
		written    bool   // hardn-env-alice written
	}{
		{"keep, then return", "debian13", false, []string{"1", "y", "0"}, nil, "Takes effect at the next sudo hardn.", true},
		{"keep as a dry run", "debian13", true, []string{"1", "0"}, nil, "No actual changes were made to your system.", false},
		{"keep declined", "debian13", false, []string{"1", "n", "0"}, nil, "Keep HARDN_CONFIG under sudo?", false},
		{"the guide", "debian13", false, []string{"2", "0"}, nil, "export HARDN_CONFIG=/path/to/hardn.yml", false},
		{"refused before asking", "alpine324", false, []string{"1", "0"}, nil, "Under doas stays Not kept by hardn.", false},
		{"quit", "debian13", false, []string{"q"}, nav.ErrQuit, "Environment Variable Settings", false},
		{"the end of the input", "debian13", false, nil, prompt.ErrAborted, "Environment Variable Settings", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newEnvHost(t, tc.host)
			s := useEnvHost(t, h, tc.lines...)
			cfg := config.DefaultConfig()
			cfg.DryRun = tc.dryRun // as -n or hardn.yml leaves it before 8 is chosen
			was := *cfg
			err := runEnvironmentScreen(context.Background(), cfg)
			if tc.want == nil && err != nil || tc.want != nil && !errors.Is(err, tc.want) {
				t.Fatalf("runEnvironmentScreen = %v, want %v; screen:\n%s", err, tc.want, s.out.String())
			}
			out := s.out.String()
			if !strings.Contains(out, tc.said) {
				t.Errorf("the screen does not say %q:\n%s", tc.said, out)
			}
			if want := map[bool]int{true: 1}[tc.written]; s.applies != want || s.live != 0 || s.atPrompt != 0 {
				t.Errorf("signals registered %d times (want %d), %d left, %d prompts read under them", s.applies, want, s.live, s.atPrompt)
			}
			got := h.writes()
			if !tc.written {
				if len(got) > 0 {
					t.Errorf("wrote %q", got)
				}
			} else if mode, uid, gid := entryOf(h, envTestFile); h.content(envTestFile) != sudoers.Header+"Defaults:alice env_keep += \"HARDN_CONFIG\"\n" ||
				mode != 0o440 || uid != 0 || gid != 0 {
				t.Errorf("%s: %04o %d:%d\n%s\nwrites %q", envTestFile, mode, uid, gid, h.content(envTestFile), got)
			}
			for _, c := range got {
				if strings.HasPrefix(c, "write-atomic /etc/hardn") {
					t.Errorf("hardn.yml was written: %s", c)
				}
			}
			if cfg.DryRun != was.DryRun || cfg.BackupPath != was.BackupPath || cfg.EnableBackups != was.EnableBackups {
				t.Errorf("the main menu's configuration changed: %+v", cfg)
			}
		})
	}
}

// entryOf is name's mode and owner on h.
func entryOf(h *envTestHost, name string) (fs.FileMode, int, int) {
	e, _ := h.Entry(name)
	return e.Mode.Perm(), e.UID, e.GID
}

// TestEnvironmentHook: withEnvironmentScreen sets the Environment item of
// the menu given to the Environment screen on the configuration main
// gives it, not a copy: plan only set after the hook, as the session
// stands when 8 is chosen, stops item 1 at its plan, nothing written.
func TestEnvironmentHook(t *testing.T) {
	h := newEnvHost(t, "debian13")
	s := useEnvHost(t, h, "1", "0")
	cfg := config.DefaultConfig()
	set := withEnvironmentScreen(mainmenu.Options{}, cfg)
	if set.Environment == nil {
		t.Fatal("withEnvironmentScreen did not set the Environment item's screen on the menu given")
	}
	cfg.DryRun = true
	if err := set.Environment(); err != nil {
		t.Fatalf("the Environment item = %v; screen:\n%s", err, s.out.String())
	}
	if got := h.writes(); len(got) > 0 || !strings.Contains(s.out.String(), "No actual changes were made to your system.") || s.applies != 0 {
		t.Errorf("wrote %q, %d applies; screen:\n%s", got, s.applies, s.out.String())
	}
}

// TestBackupThenEnvironment: a path change through the Backup item leaves
// the main menu's configuration holding the new path, and the Environment
// item's next change backs hardn-env-alice up under it, nothing under the
// old: each visit assembles engineOptions from the configuration as it
// then is (runEnvironmentScreen), as TestBackupThenDNS holds for DNS.
// Environment is visited, with a change, before the path change, so that
// options assembled once, when the hook is set, at the first visit or at
// the first change, back the last change up under the old path. Both
// hooks as main sets them, on one host, the user typing on through the
// three visits, as from the main menu: keep, the path change, then stop
// keeping.
func TestBackupThenEnvironment(t *testing.T) {
	h := newEnvHost(t, "debian13")
	h.AddDir("/srv", 0o755, 0, 0)
	h.AddFile(dnsTestYML, []byte("# hardn's configuration (web-01.example.net)\nusername: alice\n"), 0o644, 0, 0)
	cfg, err := config.Read(context.Background(), h, dnsTestYML)
	if err != nil {
		t.Fatal(err)
	}
	s := useEnvHost(t, h, "1", "y", "0", "2", backupTestPath, "y", "0", "1", "y", "0")
	set := withEnvironmentScreen(withBackupScreen(mainmenu.Options{}, cfg), cfg)
	if set.Backup == nil || set.Environment == nil {
		t.Fatal("withBackupScreen and withEnvironmentScreen did not set the Backup and Environment items' screens on the menu given")
	}
	if err := set.Environment(); err != nil {
		t.Fatalf("the Environment item, first = %v; screen:\n%s", err, s.out.String())
	}
	if h.content(envTestFile) == "<absent>" {
		t.Fatalf("the first visit did not keep HARDN_CONFIG; screen:\n%s", s.out.String())
	}
	if err := set.Backup(); err != nil {
		t.Fatalf("the Backup item = %v; screen:\n%s", err, s.out.String())
	}
	if cfg.BackupPath != backupTestPath {
		t.Fatalf("the main menu's configuration says backupPath %q after the path change, want %q; screen:\n%s", cfg.BackupPath, backupTestPath, s.out.String())
	}
	before := len(h.writes())
	if err := set.Environment(); err != nil {
		t.Fatalf("the Environment item, again = %v; screen:\n%s", err, s.out.String())
	}
	envWrites := h.writes()[before:]
	if !slices.Contains(envWrites, "remove "+envTestFile) || len(under(envWrites, backupTestPath)) == 0 {
		t.Errorf("stopping wrote %q: want %s removed and its backup under %s; screen:\n%s", envWrites, envTestFile, backupTestPath, s.out.String())
	}
	if got := under(envWrites, backup.DefaultPath); len(got) > 0 {
		t.Errorf("stopping wrote under the old path: %q", got)
	}
	if s.applies != 3 || s.live != 0 || s.atPrompt != 0 {
		t.Errorf("signals registered %d times (want 3), %d left, %d prompts read under them", s.applies, s.live, s.atPrompt)
	}
}

// envSignalHost is an Environment host whose write of hardn-env-alice
// calls at first, and is made even if a signal at sent has cancelled ctx
// since, as a write the kernel has begun completes.
type envSignalHost struct {
	*dnsHost
	at func()
}

func (h envSignalHost) WriteFileAtomic(ctx context.Context, name string, data []byte, perm fs.FileMode, uid, gid int) error {
	if name != envTestFile {
		return h.dnsHost.WriteFileAtomic(ctx, name, data, perm, uid, gid)
	}
	h.at()
	return h.dnsHost.WriteFileAtomic(context.WithoutCancel(ctx), name, data, perm, uid, gid)
}

// TestEnvironmentScreenSignals: Ctrl-C, SIGTERM and a hangup, each sent
// to this test process as keeping HARDN_CONFIG writes hardn-env-alice on
// the Environment screen, as the kernel delivers it, cancel that apply:
// the change is undone under the engine's own hold, hardn-env-alice
// absent as before, the report saying the change failed, cancelled, and
// naming hardn's log, and the screen goes on, the variable not kept; the
// user then goes back (nil). After the hangup hardn ends (endHangup,
// recorded here), once, as the apply returns, the file absent; never
// after the others.
func TestEnvironmentScreenSignals(t *testing.T) {
	for _, sig := range []syscall.Signal{syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP} {
		t.Run(sig.String(), func(t *testing.T) {
			signals := engineSignals // the real one: useDNSHost replaces it
			h := newEnvHost(t, "debian13")
			s := useEnvHost(t, h, "1", "y", "0")
			engineHold = func() func() { s.held++; return hold.Signals(nil) } // the engine's own, counted
			var applying context.Context
			stopped := 0
			engineSignals = func(ctx context.Context) (context.Context, context.CancelFunc) {
				s.applies++
				c, stop := signals(ctx)
				applying = c
				return c, func() { stopped++; stop() }
			}
			type end struct {
				stopped, held int
				file          string
			}
			var ended []end
			endHangup = func() { ended = append(ended, end{stopped, s.held, h.content(envTestFile)}) }
			engineHost = func() host.System {
				return envSignalHost{h.dnsHost, func() {
					if err := syscall.Kill(os.Getpid(), sig); err != nil {
						t.Error(err)
					}
					select {
					case <-applying.Done():
					case <-time.After(5 * time.Second):
						t.Errorf("%v did not cancel the apply", sig)
					}
				}}
			}
			if err := runEnvironmentScreen(context.Background(), config.DefaultConfig()); err != nil {
				t.Fatalf("runEnvironmentScreen = %v; screen:\n%s", err, s.out.String())
			}
			out := s.out.String()
			if s.applies != 1 || s.held != 1 || h.content(envTestFile) != "<absent>" {
				t.Errorf("%d applies, %d holds; %s:\n%s\nscreen:\n%s", s.applies, s.held, envTestFile, h.content(envTestFile), out)
			}
			if sig == syscall.SIGHUP {
				if want := []end{{1, 1, "<absent>"}}; len(ended) != 1 || ended[0] != want[0] {
					t.Errorf("hardn ended %+v, want %+v", ended, want)
				}
				return
			}
			if len(ended) != 0 {
				t.Errorf("%v ended hardn (%d)", sig, len(ended))
			}
			if !strings.Contains(out, "Keep HARDN_CONFIG    Failed CANCELLED") || !strings.Contains(out, "Check the log file at "+output.LegacyLogPath) {
				t.Errorf("the report does not say the change was cancelled, naming %s:\n%s", output.LegacyLogPath, out)
			}
			if strings.Contains(out, "Takes effect at the next sudo hardn.") {
				t.Errorf("a cancelled change said it takes effect:\n%s", out)
			}
			if last := out[strings.LastIndex(out, "# Environment Variable Settings #"):]; !strings.Contains(last, "Not kept") {
				t.Errorf("the screen did not go on, the variable not kept:\n%s", last)
			}
		})
	}
}

// TestEnvironmentScreenFailure: visudo refusing the configuration fails
// the change at its first check, rolled back, the report naming hardn's
// log and visudo's refusal, hardn-env-alice never written.
func TestEnvironmentScreenFailure(t *testing.T) {
	h := newEnvHost(t, "visudo-fails")
	s := useEnvHost(t, h, "1", "y", "0")
	if err := runEnvironmentScreen(context.Background(), config.DefaultConfig()); err != nil {
		t.Fatalf("runEnvironmentScreen = %v; screen:\n%s", err, s.out.String())
	}
	out := s.out.String()
	for _, w := range []string{"SUDOERS_INVALID_BEFORE", "Check the log file at " + output.LegacyLogPath} {
		if !strings.Contains(out, w) {
			t.Errorf("the report lacks %q:\n%s", w, out)
		}
	}
	if h.content(envTestFile) != "<absent>" || strings.Contains(out, "Takes effect") {
		t.Errorf("%s:\n%s\nscreen:\n%s", envTestFile, h.content(envTestFile), out)
	}
}

// withSudo gives the DNS host h sudo as Debian 13 has it (/etc/sudoers,
// /etc/sudoers.d, visudo passing every file), so that -e, in a test of -g
// with it, keeps HARDN_CONFIG for alice there; and returns whether -e
// ran: any call on /etc/sudoers.d, which -g never makes.
func withSudo(h *dnsHost) (ran func() bool) {
	h.AddFile(sudoers.MainConfig, []byte(envTestSudoers), 0o440, 0, 0)
	h.AddDir(sudoers.DropInDir, 0o755, 0, 0)
	h.facts.Elevation = detect.Elevation{Sudo: detect.Tool{Present: detect.Yes, Path: "/usr/bin/sudo"}, SudoFlavor: detect.SudoOriginal, Doas: detect.Tool{Present: detect.No}}
	answer := h.Handler
	h.Handler = func(argv []string, in []byte) hostfake.Response {
		switch {
		case strings.Join(argv, " ") == "visudo -c":
			return hostfake.Response{Stdout: sudoers.MainConfig + ": parsed OK\n"}
		case len(argv) == 3 && argv[0] == "visudo" && argv[1] == "-cf":
			return hostfake.Response{Stdout: argv[2] + ": parsed OK\n"}
		}
		return answer(argv, in)
	}
	return func() bool {
		for _, c := range h.Calls() {
			if c.Path == sudoers.DropInDir || strings.HasPrefix(c.Path, sudoers.DropInDir+"/") {
				return true
			}
		}
		return false
	}
}

// runE runs hardn with args as root (runG) on the Environment host h, its
// configuration hardn's defaults, hardn's environment h's, setup, when
// set, run once the seams are in place; the session SSH's, as a
// lockout-risk plan's journal needs it (revert.ReadSession).
func runE(t *testing.T, h *envTestHost, setup func(*dnsSeams), args ...string) (result, *dnsSeams) {
	t.Helper()
	return runG(t, h.dnsHost, config.DefaultConfig(), func(s *dnsSeams) {
		environmentGetenv = func(k string) string { return h.env[k] }
		revertGetenv = func(k string) string {
			return map[string]string{"SSH_CONNECTION": "192.0.2.10 50022 192.0.2.20 22"}[k]
		}
		if setup != nil {
			setup(s)
		}
	}, args...)
}

// inOrder returns the first of want that out does not show after what
// came before it, each compared with its white space, a callout's bar
// at a wrapped line's start among it, as single spaces; "" when it shows
// them all, in order.
func inOrder(out string, want ...string) string {
	f := strings.Join(strings.Fields(strings.ReplaceAll(out, "\n# ", " ")), " ")
	at := 0
	for _, w := range want {
		i := strings.Index(f[at:], strings.Join(strings.Fields(w), " "))
		if i < 0 {
			return w
		}
		at += i + 1
	}
	return ""
}

// lockoutWords are -e's and setup-sudo-env's refusal of a lockout-risk
// plan (proposed, refactor plan slice 9, unit (d)), as output.Human says
// it.
const lockoutWords = "Keeping HARDN_CONFIG here risks a lockout (PLAN_REFUSED)"

// TestKeepEnvironmentFlag: -e and setup-sudo-env plan and apply the
// Environment control through the engine, without a question (refactor
// plan slice 9, unit (d); gallery-environment.md §5, Made here, adopted
// 2026-10-29), after the root check and hardn.yml's load, the report
// output.Human's, the exit status Phase 3's: hardn-env-alice written on
// Debian 13, nothing else; a host already as asked unchanged; every host
// the control refuses reported with its code and how to go on, nothing
// written, exit status 1 (V25's fix, as -g's); a lockout-risk plan (doas,
// hardn's password rule) refused naming main menu 8, nothing written, the
// revert timer never armed and hardn's boot unit not installed, exit
// status 1; visudo refusing the configuration failed, nothing left
// written, 1; -n the plan alone, nothing written, 3 with changes to make,
// 0 without, 1 refused, the lockout-risk plan's refusal said there too.
// hardn.yml is never written, nothing goes through the legacy provider,
// and the signals are registered for an apply alone.
func TestKeepEnvironmentFlag(t *testing.T) {
	for _, spelling := range [][]string{{"-e"}, {"setup-sudo-env"}} {
		for _, tc := range []struct {
			name, host string
			dryRun     bool
			code       int      // the exit status; 0: hardn did not exit
			said       []string // in the report, in order
			written    bool     // hardn-env-alice written
		}{
			{name: "debian13", host: "debian13", said: []string{"Keep HARDN_CONFIG", "Done", "Operation Successful"}, written: true},
			{name: "already kept", host: "kept", said: []string{"Nothing needs changing."}},
			{name: "doas, no rule of hardn's", host: "alpine324", code: 1,
				said: []string{"Operation Failed", "hardn has no doas rule for alice to keep HARDN_CONFIG on (SUDOERS_ENV_NEEDS_GRANT)", "To go on: add setenv { HARDN_CONFIG } to the rule that lets alice use doas."}},
			{name: "no tool", host: "pve9", code: 1, said: []string{"Operation Failed", "sudo isn't installed here (SUDOERS_NO_TOOL)"}},
			{name: "a root login", host: "root-login", code: 1,
				said: []string{"hardn wasn't run with sudo, so there's no user to keep it for (ENVIRONMENT_NO_USER)", "To go on: run sudo hardn as that user."}},
			{name: "a file edited by hand", host: "edited", code: 1,
				said: []string{envTestFile + " holds a rule hardn didn't write (SUDOERS_FILE_EDITED)", "To go on: put hardn's back, or remove it."}},
			{name: "lockout risk", host: "doas-password", code: 1,
				said: []string{"Lockout risk", "No actual changes were made to your system.", lockoutWords, "To go on: keep it from main menu 8, Environment, which asks you to confirm."}},
			{name: "visudo refuses", host: "visudo-fails", code: 1, said: []string{"Keep HARDN_CONFIG", "SUDOERS_INVALID_BEFORE"}},
			{name: "-n", host: "debian13", dryRun: true, code: 3,
				said: []string{"Keep HARDN_CONFIG", "+Defaults:alice env_keep += \"HARDN_CONFIG\"", "No actual changes were made to your system."}},
			{name: "-n, already kept", host: "kept", dryRun: true, said: []string{"Nothing needs changing."}},
			{name: "-n, refused", host: "pve9", dryRun: true, code: 1, said: []string{"SUDOERS_NO_TOOL"}},
			{name: "-n, lockout risk", host: "doas-password", dryRun: true, code: 1,
				said: []string{"Lockout risk", "No actual changes were made to your system.", lockoutWords}},
		} {
			args := append([]string{"--yes"}, spelling...) // a script's run (§3.11): TestYesFlag runs it without
			if tc.dryRun {
				args = append([]string{"-n"}, args...)
			}
			t.Run(strings.Join(args, " ")+", "+tc.name, func(t *testing.T) {
				h := newEnvHost(t, tc.host)
				before := map[string]string{}
				for _, f := range []string{envTestFile, func() string { n, _ := sudoers.FileNames(sudoers.ToolDoas, "alice"); return n.Grant }()} {
					before[f] = h.content(f)
				}
				r, s := runE(t, h, nil, args...)
				report := s.out.String()
				if got := map[bool]int{true: r.code}[r.ended]; got != tc.code {
					t.Fatalf("exit status %d (ended %v), want %d; report:\n%s\nstdout:\n%s\nstderr:\n%s", r.code, r.ended, tc.code, report, r.stdout, r.stderr)
				}
				want := []string{"root check", "OS detection", "config load"}
				if spelling[0] == "setup-sudo-env" {
					want = []string{"root check", "config load"} // it detects no OS: the engine does
				}
				if strings.Join(r.reached, ",") != strings.Join(want, ",") {
					t.Errorf("reached %v, want %v: after the root check and the configuration's load", r.reached, want)
				}
				if w := inOrder(report, tc.said...); w != "" {
					t.Fatalf("the report lacks %q after the text before it:\n%s", w, report)
				}
				for _, w := range []string{"y/N", "Type the hostname", "[SUCCESS]", "[ERROR]", "Script completed", "Takes effect"} {
					if strings.Contains(report+r.stdout, w) {
						t.Errorf("printed %q:\n%s%s", w, report, r.stdout)
					}
				}
				got := h.writes()
				if !tc.written {
					// visudo refusing the configuration fails the change
					// once it is journalled and backed up, rolled back:
					// nothing is left under /etc
					if len(got) > 0 && tc.host != "visudo-fails" || slices.ContainsFunc(got, func(w string) bool { return strings.Contains(w, " /etc/") }) {
						t.Errorf("wrote %q", got)
					}
					for f, was := range before {
						if h.content(f) != was {
							t.Errorf("%s changed:\n%s", f, h.content(f))
						}
					}
				} else if h.content(envTestFile) != sudoers.Header+"Defaults:alice env_keep += \"HARDN_CONFIG\"\n" {
					t.Errorf("%s:\n%s\nwrites %q", envTestFile, h.content(envTestFile), got)
				}
				for _, c := range h.Calls() {
					if c.Op == "start-detached" || c.Op == "run" && (strings.HasPrefix(strings.Join(c.Argv, " "), "rc-update") || strings.Contains(strings.Join(c.Argv, " "), revert.BootUnitName)) {
						t.Errorf("the revert timer or hardn's boot unit was reached: %v", c)
					}
					if strings.HasPrefix(c.Path, "/etc/hardn") && c.Op != "read" && c.Op != "stat" && c.Op != "lstat" {
						t.Errorf("hardn.yml's directory written: %v", c)
					}
				}
				applied := !tc.dryRun && (tc.code == 0 || tc.host == "visudo-fails") // nothing to change is applied too, as -g's
				if want := map[bool]int{true: 1}[applied]; s.applies != want || s.live != 0 {
					t.Errorf("signals registered %d times, want %d; %d left", s.applies, want, s.live)
				}
			})
		}
	}
}

// TestKeepEnvironmentNotRoot: setup-sudo-env now has the root command's
// root check (gallery-environment.md §5, Made here, adopted 2026-10-29;
// defect 9): a user other than root is refused in its words, on stderr
// (say), exit status 1, before hardn.yml is loaded or the host reached; -e
// had it already.
func TestKeepEnvironmentNotRoot(t *testing.T) {
	for _, args := range [][]string{{"setup-sudo-env"}, {"-e"}, {"-n", "setup-sudo-env"}} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			h := newEnvHost(t, "debian13")
			r, _ := runE(t, h, func(*dnsSeams) {
				lookupCurrentUser = func() (*osuser.User, error) { return &osuser.User{Uid: "1000", Username: "alice"}, nil }
			}, args...)
			if !r.ended || r.code != 1 || !strings.HasPrefix(flat(r.stderr), "Operation Failed This script needs to be run as root. Run `") || r.stdout != "" || len(r.reached) != 0 || len(h.Calls()) != 0 {
				t.Errorf("ended %v with %d after %v, %d host calls; stdout:\n%s\nstderr:\n%s", r.ended, r.code, r.reached, len(h.Calls()), r.stdout, r.stderr)
			}
		})
	}
}

// TestSetupSudoEnvOffersNoConfig: setup-sudo-env loads hardn.yml with
// loadExisting (config.LoadExisting), never the root command's loadConfig,
// which offers to create a file where none is found and, answered yes or
// at the end of input, writes one, under -n too: the command keeps
// HARDN_CONFIG, so root finding no file is its usual case, and -n writes
// nothing (gallery-environment.md §5, Made here, adopted 2026-10-29).
func TestSetupSudoEnvOffersNoConfig(t *testing.T) {
	for _, args := range [][]string{{"setup-sudo-env"}, {"-n", "setup-sudo-env"}} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			h := newEnvHost(t, "debian13")
			var existing bool
			r, _ := runE(t, h, func(*dnsSeams) {
				load := loadExisting
				loadConfig = func(string) (*config.Config, error) {
					t.Error("setup-sudo-env took the root command's load, which offers to create hardn.yml")
					return config.DefaultConfig(), nil
				}
				loadExisting = func(p string) (*config.Config, error) { existing = true; return load(p) }
			}, args...)
			if !existing || strings.Contains(r.stdout, "Would you like to create one") {
				t.Errorf("loadExisting reached %v; stdout:\n%s", existing, r.stdout)
			}
		})
	}
}

// TestKeepEnvironmentPlanOnlyFromFile: as -g, -e and setup-sudo-env show
// the plan under hardn.yml's dryRun: true, write nothing and exit 3 where
// there are changes; --dry-run=false applies; -e with another operation
// flag is refused, the refusal naming that flag alone.
func TestKeepEnvironmentPlanOnlyFromFile(t *testing.T) {
	for _, tc := range []struct {
		args []string
		code int
	}{
		{[]string{"-e"}, 3},
		{[]string{"setup-sudo-env"}, 3},
		{[]string{"--yes", "--dry-run=false", "-e"}, 0},
		{[]string{"--yes", "--dry-run=false", "setup-sudo-env"}, 0},
		{[]string{"-e", "-r"}, output.ExitUsage},
	} {
		t.Run(strings.Join(tc.args, " "), func(t *testing.T) {
			h := newEnvHost(t, "debian13")
			h.AddFile(dnsTestYML, []byte("username: alice\ndryRun: true\n"), 0o644, 0, 0)
			cfg, err := config.Read(context.Background(), h, dnsTestYML)
			if err != nil {
				t.Fatal(err)
			}
			r, s := runG(t, h.dnsHost, cfg, nil, tc.args...)
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
				if h.content(envTestFile) == "<absent>" {
					t.Errorf("not applied; report:\n%s", s.out.String())
				}
			default:
				if len(written) > 0 || len(r.reached) != 0 || !strings.Contains(r.stderr, errRunAllAlone.Error()) {
					t.Errorf("wrote %q after %v; stderr:\n%s", written, r.reached, r.stderr)
				}
			}
		})
	}
}
