package main

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/abbott/hardn/pkg/config"
	"github.com/abbott/hardn/pkg/engine/header"
	"github.com/abbott/hardn/pkg/engine/hold"
	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/hostfake"
	"github.com/abbott/hardn/pkg/host/sudoers"
	"github.com/abbott/hardn/pkg/output"
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
//     alice asking her password and keeping nothing, so that keeping
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
	was := environmentGetenv
	t.Cleanup(func() { environmentGetenv = was })
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
// end of the input aborts (prompt.ErrAborted). No audit record is
// written, and the main menu's configuration is as it was. The signals
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
				if strings.HasPrefix(c, "append ") || strings.HasPrefix(c, "write-atomic /etc/hardn") {
					t.Errorf("an audit record or hardn.yml was written: %s", c)
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
	set := &hookSet{}
	if got := withEnvironmentScreen(set, cfg); got != set || set.environment == nil {
		t.Fatal("withEnvironmentScreen did not set the Environment item's screen on the menu given")
	}
	cfg.DryRun = true
	if err := set.environment(); err != nil {
		t.Fatalf("the Environment item = %v; screen:\n%s", err, s.out.String())
	}
	if got := h.writes(); len(got) > 0 || !strings.Contains(s.out.String(), "No actual changes were made to your system.") || s.applies != 0 {
		t.Errorf("wrote %q, %d applies; screen:\n%s", got, s.applies, s.out.String())
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
