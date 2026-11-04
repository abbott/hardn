package main

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/abbott/hardn/pkg/config"
	"github.com/abbott/hardn/pkg/engine"
	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/detect"
	hostdns "github.com/abbott/hardn/pkg/host/dns"
	"github.com/abbott/hardn/pkg/host/hostfake"
	runallscreen "github.com/abbott/hardn/pkg/screens/runall"
	"github.com/abbott/hardn/pkg/style"
	"github.com/abbott/hardn/pkg/ui/nav"
	"github.com/abbott/hardn/pkg/ui/prompt"
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
