package reload_test

import (
	"context"
	"errors"
	"io/fs"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/hostfake"
	"github.com/abbott/hardn/pkg/host/reload"
	"github.com/abbott/hardn/pkg/host/sshd"
)

// The answers below are written in the format of their sources (systemctl
// show's Listen= lines, ss -Htln's columns from iproute2 misc/ss.c), not
// captured from a host.
const (
	listen22   = "Listen=0.0.0.0:22 (Stream)\nListen=[::]:22 (Stream)\n"
	listenBoth = listen22 + "Listen=0.0.0.0:2230 (Stream)\nListen=[::]:2230 (Stream)\n"
	ss22       = "LISTEN 0      4096         0.0.0.0:22        0.0.0.0:*\nLISTEN 0      4096            [::]:22           [::]:*\n"
	ssBoth     = ss22 + "LISTEN 0      4096         0.0.0.0:2230      0.0.0.0:*\nLISTEN 0      4096            [::]:2230         [::]:*\n"
	// ssDual is systemd's dual-stack socket: ss prints "*".
	ssDual = "LISTEN 0      4096               *:22              *:*\nLISTEN 0      4096               *:2230            *:*\n"
)

var (
	isActive = func(u string) []string { return []string{"systemctl", "is-active", u} }
	ok       = hostfake.Response{}
	active   = hostfake.Response{Stdout: "active\n"}
	inactive = hostfake.Response{Stdout: "inactive\n", ExitCode: 3}
)

func respond(h *hostfake.System, r hostfake.Response, argv ...string) {
	h.Respond(r, argv[0], argv[1:]...)
}

func plan(t *testing.T, in reload.Input) reload.Plan {
	t.Helper()
	p, err := reload.PlanFor(in)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// noWait counts the waits between listener checks.
func noWait(n *int) reload.Options {
	return reload.Options{Attempts: 3, Wait: func(context.Context, time.Duration) error { *n++; return nil }}
}

func runs(h *hostfake.System) []string {
	var out []string
	for _, r := range h.Runs() {
		out = append(out, strings.Join(r, " "))
	}
	return out
}

func checkRuns(t *testing.T, h *hostfake.System, want ...string) {
	t.Helper()
	if got := runs(h); !slices.Equal(got, want) {
		t.Errorf("runs = %q\nwant   %q", got, want)
	}
	for _, c := range h.Calls() {
		if c.Op != "run" && c.Op != "stat" {
			t.Errorf("unexpected call %s", c)
		}
	}
}

// TestExecuteSocketPort: sshd's 0.0.0.0 and [::] listeners against the
// socket's, written per family or as systemd's one dual-stack [::], and
// against ss's bound sockets, per family or "*".
func TestExecuteSocketPort(t *testing.T) {
	for _, tc := range []struct{ listen, ss string }{
		{listenBoth, ssBoth},
		{"Listen=[::]:22 (Stream)\nListen=[::]:2230 (Stream)\n", ssDual},
	} {
		h := hostfake.New(host.Options{})
		respond(h, ok, "systemctl", "daemon-reload")
		respond(h, hostfake.Response{Stdout: tc.listen}, "systemctl", "show", "-p", "Listen", "ssh.socket")
		respond(h, active, isActive("ssh.socket")...)
		respond(h, ok, "systemctl", "restart", "ssh.socket")
		respond(h, hostfake.Response{Stdout: tc.ss}, "ss", "-Htln")
		var waits int
		rep, err := reload.Execute(context.Background(), h, plan(t, reload.Input{SSH: ubuntu2604, Change: port(at22, both), DropIns: none}), noWait(&waits))
		if err != nil || !rep.Verified || waits != 0 || len(rep.Bound) == 0 {
			t.Fatalf("Execute = %+v, %v", rep, err)
		}
		checkRuns(t, h, "systemctl daemon-reload", "systemctl show -p Listen ssh.socket", "systemctl is-active ssh.socket",
			"systemctl restart ssh.socket", "ss -Htln")
	}
}

// TestExecuteSocketNotConfigured: the generator did not pick the new port
// up, so the socket is never restarted onto the wrong listeners.
func TestExecuteSocketNotConfigured(t *testing.T) {
	h := hostfake.New(host.Options{})
	respond(h, ok, "systemctl", "daemon-reload")
	respond(h, hostfake.Response{Stdout: listen22 + "Listen=/run/x.sock (Stream)\n"}, "systemctl", "show", "-p", "Listen", "ssh.socket")
	rep, err := reload.Execute(context.Background(), h, plan(t, reload.Input{SSH: ubuntu2604, Change: port(at22, both), DropIns: none}), reload.Options{})
	var le *reload.ListenerError
	if !errors.Is(err, reload.ErrSocketConfig) || !errors.As(err, &le) || !slices.Equal(le.Missing, at2230) || len(le.Bad) != 1 {
		t.Fatalf("err = %v, want ErrSocketConfig missing %v with one unread line", err, at2230)
	}
	if len(rep.Steps) != 2 || rep.Verified {
		t.Errorf("report = %+v", rep)
	}
	checkRuns(t, h, "systemctl daemon-reload", "systemctl show -p Listen ssh.socket")
}

func TestExecuteNotUp(t *testing.T) {
	tests := []struct {
		name       string
		ssh        detect.SSH
		change     reload.Change
		setup      func(*hostfake.System)
		runs       []string
		unverified string
	}{
		{"socket stopped: not restarted", ubuntu2604, port(at22, both), func(h *hostfake.System) {
			respond(h, ok, "systemctl", "daemon-reload")
			respond(h, hostfake.Response{Stdout: listenBoth}, "systemctl", "show", "-p", "Listen", "ssh.socket")
			respond(h, inactive, isActive("ssh.socket")...)
		}, []string{"systemctl daemon-reload", "systemctl show -p Listen ssh.socket", "systemctl is-active ssh.socket"},
			"ssh.socket is inactive: nothing listens on the new configuration until it starts"},
		{"ubuntu 24.04 before the first connection", ubuntu2404, reload.Change{Keywords: []string{"PasswordAuthentication"}}, func(h *hostfake.System) {
			respond(h, inactive, isActive("ssh.service")...)
		}, []string{"systemctl is-active ssh.service"}, "no listener changed"},
		{"debian, stopped while reloading", debian, port(at22, both), func(h *hostfake.System) {
			respond(h, active, isActive("ssh.service")...)
			respond(h, inactive, isActive("ssh.service")...)
			respond(h, hostfake.Response{Stderr: "ssh.service is not active, cannot reload.\n", ExitCode: 1}, "systemctl", "reload", "ssh.service")
		}, []string{"systemctl is-active ssh.service", "systemctl reload ssh.service", "systemctl is-active ssh.service"},
			"ssh.service is inactive (it stopped while systemctl reload ssh.service ran): nothing listens on the new configuration until it starts"},
		{"alpine, sshd not started", alpine, port(at22, both), func(*hostfake.System) {}, nil,
			"OpenRC has not started sshd: nothing listens on the new configuration until it starts"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h := hostfake.New(host.Options{})
			tc.setup(h)
			rep, err := reload.Execute(context.Background(), h, plan(t, reload.Input{SSH: tc.ssh, Change: tc.change, DropIns: none}), reload.Options{})
			if err != nil || rep.Verified || rep.Unverified != tc.unverified {
				t.Fatalf("Execute = %+v, %v\nwant unverified %q", rep, err, tc.unverified)
			}
			checkRuns(t, h, tc.runs...)
		})
	}
}

func TestExecuteReloadPolls(t *testing.T) {
	for _, tc := range []struct {
		name  string
		ssh   detect.SSH
		setup func(*hostfake.System)
		up    string
	}{
		{"debian", debian, func(h *hostfake.System) {
			respond(h, active, isActive("ssh.service")...)
			respond(h, ok, "systemctl", "reload", "ssh.service")
		}, "systemctl is-active ssh.service"},
		{"alpine", alpine, func(h *hostfake.System) {
			h.AddSymlink(detect.OpenRCStarted+"/sshd", detect.OpenRCSSHD)
			h.AddFile(detect.OpenRCSSHD, []byte("#!/sbin/openrc-run\n"), 0o755, 0, 0)
			respond(h, ok, "rc-service", "sshd", "reload")
		}, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := hostfake.New(host.Options{})
			tc.setup(h)
			respond(h, hostfake.Response{Stdout: ss22}, "ss", "-Htln") // sshd has not rebound yet
			respond(h, hostfake.Response{Stdout: ssBoth}, "ss", "-Htln")
			var waits int
			rep, err := reload.Execute(context.Background(), h, plan(t, reload.Input{SSH: tc.ssh, Change: port(at22, both)}), noWait(&waits))
			if err != nil || !rep.Verified || waits != 1 {
				t.Fatalf("Execute = %+v, %v; %d waits", rep, err, waits)
			}
			want := []string{tc.up, "systemctl reload ssh.service", "ss -Htln", "ss -Htln"}
			if tc.up == "" {
				want = []string{"rc-service sshd reload", "ss -Htln", "ss -Htln"}
			}
			checkRuns(t, h, want...)
		})
	}
}

func TestExecuteFails(t *testing.T) {
	debianPort := reload.Input{SSH: debian, Change: port(at22, both)}
	tests := []struct {
		name  string
		in    reload.Input
		setup func(*hostfake.System)
		check func(*testing.T, error)
	}{
		{"sshd -t rejects the files", debianPort, func(h *hostfake.System) {
			respond(h, active, isActive("ssh.service")...)
			respond(h, hostfake.Response{Stderr: "Job for ssh.service failed because the control process exited with error code.\n", ExitCode: 1}, "systemctl", "reload", "ssh.service")
		}, func(t *testing.T, err error) {
			var xe *host.ExitError
			if !errors.As(err, &xe) || xe.ExitCode != 1 || !strings.Contains(err.Error(), "systemctl reload ssh.service") {
				t.Errorf("err = %v, want the reload's *host.ExitError", err)
			}
		}},
		{"never bound", debianPort, func(h *hostfake.System) {
			respond(h, active, isActive("ssh.service")...)
			respond(h, ok, "systemctl", "reload", "ssh.service")
			respond(h, hostfake.Response{Stdout: ss22 + "LISTEN 0 128 nonsense\n"}, "ss", "-Htln")
		}, func(t *testing.T, err error) {
			var le *reload.ListenerError
			if !errors.Is(err, reload.ErrNotBound) || !errors.As(err, &le) || !slices.Equal(le.Missing, at2230) || len(le.Bad) != 1 {
				t.Errorf("err = %v, want ErrNotBound missing %v", err, at2230)
			}
		}},
		{"socket restart fails and leaves it down", reload.Input{SSH: ubuntu2604, Change: port(at22, both), DropIns: none}, func(h *hostfake.System) {
			respond(h, ok, "systemctl", "daemon-reload")
			respond(h, hostfake.Response{Stdout: listenBoth}, "systemctl", "show", "-p", "Listen", "ssh.socket")
			respond(h, active, isActive("ssh.socket")...)
			respond(h, hostfake.Response{Stdout: "failed\n", ExitCode: 3}, isActive("ssh.socket")...)
			respond(h, hostfake.Response{Stderr: "Job for ssh.socket failed.\n", ExitCode: 1}, "systemctl", "restart", "ssh.socket")
		}, func(t *testing.T, err error) {
			var xe *host.ExitError
			if !errors.As(err, &xe) || !strings.Contains(err.Error(), "systemctl restart ssh.socket") {
				t.Errorf("err = %v, want the restart's *host.ExitError", err)
			}
		}},
		{"unit state not established", debianPort, func(h *hostfake.System) {
			respond(h, hostfake.Response{Stdout: "activating\n", ExitCode: 3}, isActive("ssh.service")...)
		}, func(t *testing.T, err error) {
			if !errors.Is(err, reload.ErrUnitState) {
				t.Errorf("err = %v, want ErrUnitState", err)
			}
		}},
		{"ss fails", debianPort, func(h *hostfake.System) {
			respond(h, active, isActive("ssh.service")...)
			respond(h, ok, "systemctl", "reload", "ssh.service")
			respond(h, hostfake.Response{ExitCode: 1}, "ss", "-Htln")
		}, func(t *testing.T, err error) {
			var xe *host.ExitError
			if !errors.As(err, &xe) {
				t.Errorf("err = %v, want ss's *host.ExitError", err)
			}
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h := hostfake.New(host.Options{})
			tc.setup(h)
			var waits int
			rep, err := reload.Execute(context.Background(), h, plan(t, tc.in), noWait(&waits))
			if rep.Verified {
				t.Error("Verified on a failure")
			}
			tc.check(t, err)
		})
	}
}

func TestExecuteSSMissing(t *testing.T) {
	h := hostfake.New(host.Options{})
	h.AddFile(detect.OpenRCStarted+"/sshd", nil, 0o644, 0, 0)
	respond(h, ok, "rc-service", "sshd", "reload")
	respond(h, hostfake.Response{Err: host.ErrNotFound}, "ss", "-Htln")
	rep, err := reload.Execute(context.Background(), h, plan(t, reload.Input{SSH: alpine, Change: port(at22, both)}), reload.Options{})
	if err != nil || rep.Verified || !strings.Contains(rep.Unverified, "ss is not installed") {
		t.Fatalf("Execute = %+v, %v", rep, err)
	}
}

func TestExecuteRefusesDoneContext(t *testing.T) {
	h := hostfake.New(host.Options{})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	p := plan(t, reload.Input{SSH: debian, Change: reload.Change{Keywords: []string{"PermitRootLogin"}}})
	if _, err := reload.Execute(ctx, h, p, reload.Options{}); !errors.Is(err, context.Canceled) || len(h.Calls()) != 0 {
		t.Fatalf("err = %v, calls %v", err, h.Calls())
	}
	// A rollback after Ctrl-C runs under context.WithoutCancel.
	respond(h, active, isActive("ssh.service")...)
	respond(h, ok, "systemctl", "reload", "ssh.service")
	if _, err := reload.Execute(context.WithoutCancel(ctx), h, p, reload.Options{}); err != nil {
		t.Fatal(err)
	}
}

func TestExecuteRefusesMalformedStep(t *testing.T) {
	h := hostfake.New(host.Options{})
	for _, st := range []reload.Step{{Kind: reload.StepRun}, {Argv: []string{"systemctl", "daemon-reload"}}} {
		if _, err := reload.Execute(context.Background(), h, reload.Plan{Steps: []reload.Step{st}}, reload.Options{}); !errors.Is(err, fs.ErrInvalid) {
			t.Errorf("step %+v: err = %v, want fs.ErrInvalid", st, err)
		}
	}
	if len(h.Calls()) != 0 {
		t.Errorf("calls %v", h.Calls())
	}
}

func TestParseBound(t *testing.T) {
	out := ssBoth + ssDual +
		"LISTEN 0      128    127.0.0.53%lo:53        0.0.0.0:*\n" +
		"LISTEN 0      128    [fe80::1]%eth0:2230        [::]:*\n" +
		"UNCONN 0      0            0.0.0.0:68        0.0.0.0:*\n" +
		"LISTEN 0      128          0.0.0.0:0         0.0.0.0:*\n" +
		"LISTEN 0      128       [::1:2230           [::]:*\n"
	bound, bad := reload.ParseBound([]byte(out))
	var got []string
	for _, b := range bound {
		got = append(got, b.String())
	}
	want := []string{"0.0.0.0:22", "[::]:22", "0.0.0.0:2230", "[::]:2230", "*:22", "*:2230", "127.0.0.53%lo:53", "[fe80::1]%eth0:2230"}
	if !slices.Equal(got, want) || len(bad) != 2 {
		t.Errorf("bound = %q, bad %q", got, bad)
	}
	if !bound[4].Dual || bound[1].Dual {
		t.Errorf("Dual: %+v %+v", bound[4], bound[1])
	}
}

// TestCovers: the address rules of the hold check (sshd's own lines, which
// bind :: IPv6-only) and of the bound check (ss's "*" is dual-stack).
func TestCovers(t *testing.T) {
	tests := []struct {
		name         string
		before, have []sshd.Listener
		ok           bool
	}{
		{"same", at22, at22, true},
		{"wildcard covers an address", []sshd.Listener{l("192.0.2.10", 22)}, []sshd.Listener{l("0.0.0.0", 22)}, true},
		{"mapped address", []sshd.Listener{l("::ffff:192.0.2.10", 22)}, []sshd.Listener{l("0.0.0.0", 22)}, true},
		{"IPv4 wildcard does not cover IPv6", []sshd.Listener{l("::", 22)}, []sshd.Listener{l("0.0.0.0", 22)}, false},
		{"another address", []sshd.Listener{l("192.0.2.10", 22)}, []sshd.Listener{l("192.0.2.11", 22)}, false},
		{"another port", at22, at2230, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := reload.PlanFor(reload.Input{SSH: debian, Change: reload.Change{Keywords: []string{"ListenAddress"}, Before: tc.before, After: tc.have}})
			if (err == nil) != tc.ok {
				t.Errorf("err = %v, want covered %v", err, tc.ok)
			}
		})
	}
}
