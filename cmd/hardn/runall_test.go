package main

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	hardncmd "github.com/abbott/hardn/pkg/cmd"
	"github.com/abbott/hardn/pkg/config"
	"github.com/abbott/hardn/pkg/engine"
	planner "github.com/abbott/hardn/pkg/engine/runall"
	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/detect"
	hostdns "github.com/abbott/hardn/pkg/host/dns"
	"github.com/abbott/hardn/pkg/host/hostfake"
	runallscreen "github.com/abbott/hardn/pkg/screens/runall"
	"github.com/abbott/hardn/pkg/style"
	"github.com/abbott/hardn/pkg/ui/nav"
	"github.com/abbott/hardn/pkg/ui/prompt"
	"github.com/abbott/hardn/pkg/ui/term"
)

// The Run All item's and -r's tests: the SSH Login host (newSSHHost:
// Debian 13, george in sudo logging in by key over SSH, root by key, no
// ufw), hardn.yml asking Run All for root login off and the nameservers
// (runAllYML). Synthetic values only: george, web-01, 192.0.2.x.

// runAllTestVersion is hardn's version in these tests.
const runAllTestVersion = "0.4.0"

// runAllYML is hardn.yml asking Run All for root login off, the
// nameservers and the firewall (which the host, without ufw, refuses):
// one lockout-risk plan of root login off and the nameservers, the
// firewall skipped.
const runAllYML = "disableRootSSH: true\nenableUfwSshPolicy: true\nconfigureDns: true\nnameservers:\n  - 192.0.2.53\n  - 192.0.2.54\n"

// newRunAllHost is newSSHHost's host name with hardn.yml yml, as hardn
// reads it there, and the commands Auto Updates' check runs on Debian
// answered as on a host without unattended-upgrades and its timers off
// (runAllUpdates): the row's state, which nothing here asks to change.
func newRunAllHost(t *testing.T, name, yml string) (*sshTestHost, *config.Config) {
	t.Helper()
	h, _ := newSSHHost(t, name)
	ssh := h.Handler
	h.Handler = func(argv []string, in []byte) hostfake.Response {
		if r, ok := runAllUpdates(argv); ok {
			return r
		}
		return ssh(argv, in)
	}
	h.AddFile(dnsTestYML, []byte(yml), 0o644, 0, 0)
	cfg, err := config.Read(context.Background(), h, dnsTestYML)
	if err != nil {
		t.Fatal(err)
	}
	return h, cfg
}

// onDNSHost points the engine's seams at the DNS tests' Debian 13 host
// (useDNSHost), for a test that runs -r past hardn.yml's load: Run All
// reads it there, its rows skipped where the host has nothing to read.
func onDNSHost(t *testing.T) {
	t.Helper()
	h, _ := newDNSHost(t, "debian13", nil)
	useDNSHost(t, h)
}

// runAllUpdates answers Auto Updates' check on Debian: unattended-upgrades
// not installed, apt's periodic settings empty, apt's timers off; false
// for any other command.
func runAllUpdates(argv []string) (hostfake.Response, bool) {
	switch {
	case argv[0] == "apt-config":
		return hostfake.Response{}, true
	case argv[0] == "dpkg-query" && argv[len(argv)-1] == "unattended-upgrades":
		return hostfake.Response{Stderr: "dpkg-query: no packages found matching unattended-upgrades\n", ExitCode: 1}, true
	case len(argv) == 3 && argv[0] == "systemctl" && argv[1] == "is-active" && strings.HasPrefix(argv[2], "apt-daily"):
		return hostfake.Response{Stdout: "inactive\n", ExitCode: 3}, true
	case len(argv) == 3 && argv[0] == "systemctl" && argv[1] == "is-enabled" && strings.HasPrefix(argv[2], "apt-daily"):
		return hostfake.Response{Stdout: "disabled\n", ExitCode: 1}, true
	}
	return hostfake.Response{}, false
}

// TestRunAllScreen: the main menu's Run All item runs the Run All screen
// on the host hardn builds, through the planner and the engine with
// engineOptions, its session the engine's (george's over SSH) and hardn's
// environment read for the session user (os.Getenv: george, through
// sudo): the rows (root login off, the nameservers, the firewall refused);
// Apply all says the firewall skipped, then "Apply this plan?" above the
// hostname typed, the apply writing PermitRootLogin no and the
// nameservers and arming the revert timer, sudo hardn confirm said after;
// under plan only it stops at its plan, nothing written or armed; 0 goes
// back (nil), q quits (nav.ErrQuit), the end of the input aborts
// (prompt.ErrAborted). The signals are registered for the apply alone.
// No change saves hardn.yml.
func TestRunAllScreen(t *testing.T) {
	for _, tc := range []struct {
		name    string
		dryRun  bool
		lines   []string
		want    error
		applied bool
		said    []string
	}{
		{"back", false, []string{"0"}, nil, false, []string{"Run All", "Controls", "Firewall", "Refused:", "SSH Login", "Root login off", "DNS", "Set 2 nameservers", "1) Apply all"}},
		{"Apply all", false, []string{"1", "web-01", "0"}, nil, true,
			[]string{"Firewall skipped:", "Apply this plan?", "Type the hostname", "Operation Successful", "sudo hardn confirm", "Change pending"}},
		{"Apply all under plan only", true, []string{"1", "0"}, nil, false, []string{"Firewall skipped:", "No actual changes were made to your system."}},
		{"quit", false, []string{"q"}, nav.ErrQuit, false, nil},
		{"the end of the input", false, nil, prompt.ErrAborted, false, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h, cfg := newRunAllHost(t, "debian13", runAllYML)
			yml, conf := h.content(dnsTestYML), h.content(hostdns.ResolvConf)
			s := useSSHHost(t, h, tc.lines...)
			t.Setenv("SUDO_USER", "george")
			cfg.DryRun = tc.dryRun
			err := runRunAllScreen(context.Background(), cfg, runAllTestVersion)
			if tc.want == nil && err != nil || tc.want != nil && !errors.Is(err, tc.want) {
				t.Fatalf("runRunAllScreen = %v, want %v; screen:\n%s", err, tc.want, s.out.String())
			}
			out := style.StripAnsi(s.out.String())
			if w := inOrder(out, tc.said...); w != "" {
				t.Errorf("the screen lacks %q:\n%s", w, out)
			}
			dns := strings.Contains(h.content(hostdns.ResolvConf), "nameserver 192.0.2.53\n")
			if got := h.rootLogin(); (got == "no") != tc.applied || h.armed() != tc.applied || dns != tc.applied ||
				!tc.applied && (len(h.sshdWrites()) > 0 || h.content(hostdns.ResolvConf) != conf) {
				t.Errorf("PermitRootLogin %s, armed %t, nameservers %t, wrote %q; want applied %t", got, h.armed(), dns, h.sshdWrites(), tc.applied)
			}
			if want := map[bool]int{true: 1}[tc.applied]; s.applies != want || s.live != 0 || s.atPrompt != 0 {
				t.Errorf("signals registered %d times (want %d), %d left, %d prompts read under them", s.applies, want, s.live, s.atPrompt)
			}
			if got := h.content(dnsTestYML); got != yml {
				t.Errorf("hardn.yml saved:\n%s", got)
			}
		})
	}
}

// sessionProbe is a control that records the session the engine plans it
// with (engine.Input.Session) and plans nothing.
type sessionProbe struct{ got *[]any }

func (sessionProbe) ID() string                          { return "test.session-probe" }
func (sessionProbe) Title() string                       { return "Session probe" }
func (sessionProbe) Applies(detect.Facts) (bool, string) { return true, "" }
func (sessionProbe) Check(context.Context, host.System, detect.Facts) (engine.Status, error) {
	return engine.Status{}, nil
}
func (p sessionProbe) Plan(_ context.Context, _ host.System, in engine.Input) ([]engine.Change, error) {
	*p.got = append(*p.got, in.Session)
	return nil, nil
}

// TestRunAllScreenOptions: runRunAllScreen gives the screen the session
// engineOptions read (george's over SSH, from sshd's log) as the planner's
// Session, and the engines it builds plan with that same session; the
// main menu's configuration, hardn's version, os.Getenv, the screens'
// apply context and hardn's log; DryRun as the configuration says when
// the item is chosen.
func TestRunAllScreenOptions(t *testing.T) {
	for _, dryRun := range []bool{false, true} {
		h, cfg := newRunAllHost(t, "debian13", runAllYML)
		useSSHHost(t, h)
		var got runallscreen.Options
		was := runAllRun
		t.Cleanup(func() { runAllRun = was })
		runAllRun = func(_ context.Context, o runallscreen.Options) error { got = o; return nil }
		cfg.DryRun = dryRun
		if err := runRunAllScreen(context.Background(), cfg, runAllTestVersion); err != nil {
			t.Fatal(err)
		}
		if !got.Session.Remote || got.Session.Source == "" {
			t.Errorf("the planner's session %+v, want george's over SSH", got.Session)
		}
		var planned []any
		if _, err := got.NewEngine(nil).Plan(context.Background(), []engine.Control{sessionProbe{&planned}}, cfg); err != nil {
			t.Fatal(err)
		}
		if len(planned) != 1 || !reflect.DeepEqual(planned[0], got.Session) {
			t.Errorf("the engine planned with the session %+v, the planner's is %+v", planned, got.Session)
		}
		if got.Config != cfg || got.Version != runAllTestVersion || got.DryRun != dryRun || got.Getenv == nil || got.ApplyContext == nil ||
			got.System == nil || got.Prompter.In == nil || got.Prompter.Out == nil || got.LogPath == "" || got.Now == nil {
			t.Errorf("the screen's options: %+v", got)
		}
	}
}

// TestRunAllHook: withRunAllScreen sets the Run All item's screen on the
// configuration main gives it, plan only set after the hook stopping
// Apply all at its plan.
func TestRunAllHook(t *testing.T) {
	h, cfg := newRunAllHost(t, "debian13", runAllYML)
	s := useSSHHost(t, h, "1", "0")
	t.Setenv("SUDO_USER", "george")
	set := &hookSet{}
	if got := withRunAllScreen(set, cfg, runAllTestVersion); got != set || set.runAll == nil {
		t.Fatal("withRunAllScreen did not set the Run All item on the menu given")
	}
	cfg.DryRun = true
	if err := set.runAll(); err != nil {
		t.Fatalf("the Run All item = %v; screen:\n%s", err, s.out.String())
	}
	if got := h.sshdWrites(); len(got) > 0 || !strings.Contains(s.out.String(), "No actual changes were made to your system.") || s.applies != 0 {
		t.Errorf("wrote %q, %d applies; screen:\n%s", got, s.applies, s.out.String())
	}
}

// rRefusalWords are -r's refusal off a terminal (gallery-runall.md §6), as
// output.Human says it.
const rRefusalWords = "hardn -r asks you to type the hostname, and this isn't a terminal (PLAN_REFUSED)"

// rSkipped is what -r says on stderr of the firewall on the SSH Login
// host, which has no ufw.
const rSkipped = "Firewall skipped: "

// runAllDNSYML asks Run All for the nameservers alone: a plain plan.
const runAllDNSYML = "configureDns: true\nnameservers:\n  - 192.0.2.53\n  - 192.0.2.54\n"

// TestRunAllFlag: -r plans with Run All's planner and applies its one
// plan through the engine (refactor plan slice 8, unit (d);
// gallery-runall.md §5, decision 8 (a), adopted): the firewall, refused
// on a host without ufw, said skipped on stderr before anything else; on
// a terminal the plan, "Apply this plan?" above the hostname typed,
// never y/N, the apply, PermitRootLogin no and the nameservers written
// and the revert timer armed, then sudo hardn confirm, 0; a plain plan
// (the nameservers alone) asked the same, applied, nothing armed, 0; 0
// and q declining, nothing written, 3. Off a terminal any plan with
// changes refused, PLAN_REFUSED in §6's words, nothing written or armed,
// 1, a plain one too. -n the plan alone, no question, nothing written, 3,
// on a terminal or off; nothing asked (no username either), nothing to
// change, 0, under -n too. -u names Run All's user (Create carol). Nothing
// goes through the legacy provider; hardn.yml is never saved.
func TestRunAllFlag(t *testing.T) {
	for _, tc := range []struct {
		name, yml   string
		tty, dryRun bool
		args        []string // after -r (and -n)
		lines       []string
		code        int      // the exit status; 0: hardn did not exit
		said        []string // in order, on stdout
		unsaid      []string
		skipped     bool // the firewall said skipped on stderr
		applied     bool // root login off and the nameservers written
		armed       bool
		dns         bool // the nameservers written
	}{
		{name: "on a terminal", yml: runAllYML, tty: true, lines: []string{"web-01"}, skipped: true, applied: true, armed: true, dns: true,
			said: []string{"Lockout risk", "Apply this plan?", "Type the hostname [web-01, 0 or q]", "Operation Successful", "sudo hardn confirm"}},
		{name: "0 declines", yml: runAllYML, tty: true, lines: []string{"0"}, code: 3, skipped: true, said: []string{"Type the hostname", "Operation Cancelled"}},
		{name: "q declines", yml: runAllYML, tty: true, lines: []string{"q"}, code: 3, skipped: true, said: []string{"Type the hostname"}},
		{name: "off a terminal", yml: runAllYML, code: 1, skipped: true,
			said:   []string{"Lockout risk", "No actual changes were made to your system.", rRefusalWords, "To go on: run hardn -r from a terminal."},
			unsaid: []string{"Type the hostname"}},
		{name: "a plain plan on a terminal", yml: runAllDNSYML, tty: true, lines: []string{"web-01"}, dns: true,
			said: []string{"Apply this plan?", "Type the hostname", "Operation Successful"}, unsaid: []string{"Lockout risk", "sudo hardn confirm"}},
		{name: "a plain plan off a terminal", yml: runAllDNSYML, code: 1,
			said: []string{"No actual changes were made to your system.", rRefusalWords}, unsaid: []string{"Type the hostname"}},
		{name: "-n on a terminal", yml: runAllYML, tty: true, dryRun: true, code: 3, skipped: true,
			said: []string{"Lockout risk", "No actual changes were made to your system."}, unsaid: []string{"Type the hostname", rRefusalWords}},
		{name: "-n off a terminal", yml: runAllYML, dryRun: true, code: 3, skipped: true,
			said: []string{"Lockout risk", "No actual changes were made to your system."}, unsaid: []string{rRefusalWords}},
		{name: "nothing asked", yml: "", tty: true, said: []string{"Nothing needs changing."}, unsaid: []string{"Type the hostname"}},
		{name: "nothing asked off a terminal", yml: "", said: []string{"Nothing needs changing."}, unsaid: []string{rRefusalWords}},
		{name: "nothing asked, -n", yml: "", dryRun: true, said: []string{"Nothing needs changing."}},
		{name: "-u names the user", yml: "sshKeys:\n  - \"" + usrTestKey(5, "carol@laptop") + "\"\n", dryRun: true, args: []string{"-u", "carol"}, code: 3,
			said: []string{"Create user carol", "No actual changes were made to your system."}},
	} {
		args := append([]string{"-r"}, tc.args...)
		if tc.dryRun {
			args = append([]string{"-n"}, args...)
		}
		t.Run(tc.name, func(t *testing.T) {
			h, cfg := newRunAllHost(t, "debian13", tc.yml)
			yml, conf, was := h.content(dnsTestYML), h.content(hostdns.ResolvConf), h.rootLogin()
			var env func() term.Env
			if tc.tty {
				env = onTTY
			}
			r, s := runD(t, h, cfg, env, tc.lines, nil, args...)
			out := style.StripAnsi(s.out.String())
			if got := map[bool]int{true: r.code}[r.ended]; got != tc.code {
				t.Fatalf("exit status %d (ended %v), want %d; report:\n%s\nstderr:\n%s", r.code, r.ended, tc.code, out, r.stderr)
			}
			if w := inOrder(out, tc.said...); w != "" {
				t.Errorf("the report lacks %q after the text before it:\n%s", w, out)
			}
			for _, w := range append([]string{"y/N", "[SUCCESS]", "[ERROR]", "System hardening", rSkipped}, tc.unsaid...) {
				if strings.Contains(out+r.stdout, w) {
					t.Errorf("printed %q on stdout:\n%s%s", w, out, r.stdout)
				}
			}
			if strings.Contains(r.stderr, rSkipped+"ufw isn't installed") != tc.skipped {
				t.Errorf("the firewall said skipped %t, want %t; stderr:\n%s", !tc.skipped, tc.skipped, r.stderr)
			}
			if got := h.rootLogin(); tc.applied != (got == "no") || !tc.applied && (got != was || len(h.sshdWrites()) > 0) {
				t.Errorf("PermitRootLogin %s (was %s), wrote %q; want applied %t", got, was, h.sshdWrites(), tc.applied)
			}
			if dns := h.content(hostdns.ResolvConf) != conf; dns != tc.dns || h.armed() != tc.armed {
				t.Errorf("the nameservers written %t (want %t), armed %t (want %t)", dns, tc.dns, h.armed(), tc.armed)
			}
			if len(r.calls) != 0 {
				t.Errorf("-r went through the legacy provider: %v", r.calls)
			}
			if got := h.content(dnsTestYML); got != yml {
				t.Errorf("hardn.yml saved:\n%s", got)
			}
		})
	}
}

// TestRunAllFlagPending: -r after an applied lockout-risk plan not yet
// confirmed says the change pending on stderr, in the planner's words,
// and plans nothing, 1, under -n too.
func TestRunAllFlagPending(t *testing.T) {
	h, cfg := newRunAllHost(t, "debian13", runAllYML)
	if r, s := runD(t, h, cfg, onTTY, []string{"web-01"}, nil, "-r"); r.ended || !h.armed() {
		t.Fatalf("the first -r: ended %v with %d, armed %t; report:\n%s\nstderr:\n%s", r.ended, r.code, h.armed(), s.out.String(), r.stderr)
	}
	for _, args := range [][]string{{"-r"}, {"-n", "-r"}} {
		before := len(h.writes())
		r, s := runD(t, h, cfg, onTTY, []string{"web-01"}, nil, args...)
		if !r.ended || r.code != 1 || !strings.Contains(r.stderr, planner.PendingWords) || strings.Contains(s.out.String(), "Type the hostname") || len(h.writes()) != before {
			t.Errorf("%q: ended %v with %d, wrote %q; report:\n%s\nstderr:\n%s", args, r.ended, r.code, h.writes()[before:], s.out.String(), r.stderr)
		}
	}
}

// TestRunAllFlagPlanOnlyFromFile: hardn.yml's dryRun: true makes -r the
// plan alone, as -n does, on a terminal too: no question, nothing written
// or armed, 3; --dry-run=false over it asks and applies as live.
func TestRunAllFlagPlanOnlyFromFile(t *testing.T) {
	for _, tc := range []struct {
		args  []string
		lines []string
		code  int
	}{
		{[]string{"-r"}, nil, 3},
		{[]string{"--dry-run=false", "-r"}, []string{"web-01"}, 0},
	} {
		t.Run(strings.Join(tc.args, " "), func(t *testing.T) {
			h, cfg := newRunAllHost(t, "debian13", runAllYML+"dryRun: true\n")
			r, s := runD(t, h, cfg, onTTY, tc.lines, nil, tc.args...)
			out := style.StripAnsi(s.out.String())
			if got := map[bool]int{true: r.code}[r.ended]; got != tc.code {
				t.Fatalf("exit status %d (ended %v), want %d; report:\n%s\nstderr:\n%s", r.code, r.ended, tc.code, out, r.stderr)
			}
			live := tc.code == 0
			if strings.Contains(out, "Type the hostname") != live || strings.Contains(out, "No actual changes were made to your system.") == live {
				t.Errorf("asked %t, want %t; report:\n%s", !live, live, out)
			}
			if h.armed() != live || (h.rootLogin() == "no") != live {
				t.Errorf("armed %t, PermitRootLogin %s; want applied %t", h.armed(), h.rootLogin(), live)
			}
		})
	}
}

// TestRunAllFlagAlone: -r with -c, -d, -w, -g or -e, under -n too, is a
// usage error in §6's words, 2, before the root check, nothing run
// (slice 9's defect 10, where -r -e ran -r alone).
func TestRunAllFlagAlone(t *testing.T) {
	for _, args := range [][]string{{"-r", "-c"}, {"-r", "-d"}, {"-r", "-w"}, {"-r", "-g"}, {"-r", "-e"}, {"-e", "-r"},
		{"-n", "-r", "-g"}, {"-d", "-p", "-n", "-r", "-w", "-g", "-e", "-c"}} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			r := runHardn(t, hostUnreachable, args...)
			if !r.ended || r.code != hardncmd.ExitUsage || len(r.reached) != 0 || len(r.calls) != 0 ||
				strings.TrimSpace(r.stderr) != "hardn -r runs alone: it can't be combined with -c, -d, -w, -g or -e." {
				t.Errorf("ended %v with %d after %v, calls %v; stderr:\n%s", r.ended, r.code, r.reached, r.calls, r.stderr)
			}
		})
	}
}
