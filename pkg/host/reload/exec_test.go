package reload_test

import (
	"context"
	"errors"
	"io/fs"
	"net/netip"
	"slices"
	"strings"
	"syscall"
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
	ssArgv   = []string{"ss", "-Htln"}
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
// against ss's bound sockets, per family or "*". ss is read first: the port
// the change adds must be free.
func TestExecuteSocketPort(t *testing.T) {
	for _, tc := range []struct{ listen, before, after string }{
		{listenBoth, ss22, ssBoth},
		{"Listen=[::]:22 (Stream)\nListen=[::]:2230 (Stream)\n", "LISTEN 0 4096 *:22 *:*\n", ssDual},
	} {
		h := hostfake.New(host.Options{})
		respond(h, hostfake.Response{Stdout: tc.before}, ssArgv...)
		respond(h, hostfake.Response{Stdout: tc.after}, ssArgv...)
		respond(h, ok, "systemctl", "daemon-reload")
		respond(h, hostfake.Response{Stdout: tc.listen}, "systemctl", "show", "-p", "Listen", "ssh.socket")
		respond(h, active, isActive("ssh.socket")...)
		respond(h, ok, "systemctl", "restart", "ssh.socket")
		var waits int
		rep, err := reload.Execute(context.Background(), h, plan(t, reload.Input{SSH: ubuntu2604, Change: port(at22, both), DropIns: none}), noWait(&waits))
		if err != nil || !rep.Verified || waits != 0 || len(rep.Bound) == 0 || rep.Reason != reload.ReasonNone {
			t.Fatalf("Execute = %+v, %v", rep, err)
		}
		checkRuns(t, h, "ss -Htln", "systemctl daemon-reload", "systemctl show -p Listen ssh.socket", "systemctl is-active ssh.socket",
			"systemctl restart ssh.socket", "systemctl is-active ssh.socket", "ss -Htln")
	}
}

// TestExecuteSocketNotConfigured: the socket is never restarted onto
// listeners that miss one the files name: the generator did not pick the
// new port up, or the socket is IPv4 only (0.0.0.0 is not dual-stack).
func TestExecuteSocketNotConfigured(t *testing.T) {
	for _, tc := range []struct {
		name, listen string
		missing      []sshd.Listener
		bad          int
	}{
		{"new port missing", listen22 + "Listen=/run/x.sock (Stream)\n", at2230, 1},
		{"IPv4 only", "Listen=0.0.0.0:22 (Stream)\nListen=0.0.0.0:2230 (Stream)\n", []sshd.Listener{l("::", 22), l("::", 2230)}, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := hostfake.New(host.Options{})
			respond(h, hostfake.Response{Stdout: ss22}, ssArgv...)
			respond(h, ok, "systemctl", "daemon-reload")
			respond(h, hostfake.Response{Stdout: tc.listen}, "systemctl", "show", "-p", "Listen", "ssh.socket")
			rep, err := reload.Execute(context.Background(), h, plan(t, reload.Input{SSH: ubuntu2604, Change: port(at22, both), DropIns: none}), reload.Options{})
			var le *reload.ListenerError
			if !errors.Is(err, reload.ErrSocketConfig) || !errors.As(err, &le) || !slices.Equal(le.Missing, tc.missing) || len(le.Bad) != tc.bad {
				t.Fatalf("err = %v, want ErrSocketConfig missing %v with %d unread lines", err, tc.missing, tc.bad)
			}
			if len(rep.Steps) != 2 || rep.Verified {
				t.Errorf("report = %+v", rep)
			}
			checkRuns(t, h, "ss -Htln", "systemctl daemon-reload", "systemctl show -p Listen ssh.socket")
		})
	}
}

// TestExecuteNotUp: a unit that is not up, where that is not hardn's doing
// or, on a socket host, ssh.socket starts sshd at the next connection. The
// reason comes before "no listener changed".
func TestExecuteNotUp(t *testing.T) {
	other := reload.Change{Keywords: []string{"PasswordAuthentication"}}
	tests := []struct {
		name       string
		ssh        detect.SSH
		change     reload.Change
		setup      func(*hostfake.System)
		runs       []string
		unverified string
	}{
		{"socket stopped: not restarted", ubuntu2604, port(at22, both), func(h *hostfake.System) {
			respond(h, hostfake.Response{Stdout: ss22}, ssArgv...)
			respond(h, ok, "systemctl", "daemon-reload")
			respond(h, hostfake.Response{Stdout: listenBoth}, "systemctl", "show", "-p", "Listen", "ssh.socket")
			respond(h, inactive, isActive("ssh.socket")...)
		}, []string{"ss -Htln", "systemctl daemon-reload", "systemctl show -p Listen ssh.socket", "systemctl is-active ssh.socket"},
			"ssh.socket is inactive: the installed files take effect when it next starts"},
		{"ubuntu 24.04 before the first connection", ubuntu2404, other, func(h *hostfake.System) {
			respond(h, inactive, isActive("ssh.service")...)
		}, []string{"systemctl is-active ssh.service"}, "ssh.service is inactive: the installed files take effect when it next starts"},
		{"ubuntu, ssh.service stopped while reloading", ubuntu2604, other, func(h *hostfake.System) {
			respond(h, active, isActive("ssh.service")...)
			respond(h, inactive, isActive("ssh.service")...)
			respond(h, hostfake.Response{Stderr: "ssh.service is not active, cannot reload.\n", ExitCode: 1}, "systemctl", "reload", "ssh.service")
		}, []string{"systemctl is-active ssh.service", "systemctl reload ssh.service", "systemctl is-active ssh.service"},
			"ssh.service is inactive after systemctl reload ssh.service: the installed files take effect when it next starts"},
		{"debian, ssh.service stopped before", debian, other, func(h *hostfake.System) {
			respond(h, inactive, isActive("ssh.service")...)
		}, []string{"systemctl is-active ssh.service"}, "ssh.service is inactive: the installed files take effect when it next starts"},
		{"alpine, sshd not started", alpine, port(at22, both), func(h *hostfake.System) {
			respond(h, hostfake.Response{Stdout: ss22}, ssArgv...)
		}, []string{"ss -Htln"}, "OpenRC has not started sshd: the installed files take effect when it next starts"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h := hostfake.New(host.Options{})
			tc.setup(h)
			rep, err := reload.Execute(context.Background(), h, plan(t, reload.Input{SSH: tc.ssh, Change: tc.change, DropIns: none}), reload.Options{})
			if err != nil || rep.Verified || rep.Unverified != tc.unverified || rep.Reason != reload.ReasonNotUp {
				t.Fatalf("Execute = %+v, %v\nwant unverified %q", rep, err, tc.unverified)
			}
			checkRuns(t, h, tc.runs...)
		})
	}
}

// TestExecuteUnitDown: where nothing else listens (ssh.service on Debian and
// Proxmox VE, OpenRC's sshd), and for ssh.socket itself, a unit that is down
// after its step fails the plan, whether the step failed or not.
func TestExecuteUnitDown(t *testing.T) {
	failed := hostfake.Response{Stdout: "failed\n", ExitCode: 3}
	rejected := hostfake.Response{Stderr: "Job for ssh.service failed.\n", ExitCode: 1}
	permit := reload.Change{Keywords: []string{"PermitRootLogin"}}
	tests := []struct {
		name   string
		in     reload.Input
		setup  func(*hostfake.System)
		failed bool // the step's own *host.ExitError is wrapped too
	}{
		{"debian reload fails and sshd stops", reload.Input{SSH: debian, Change: permit}, func(h *hostfake.System) {
			respond(h, active, isActive("ssh.service")...)
			respond(h, failed, isActive("ssh.service")...)
			respond(h, rejected, "systemctl", "reload", "ssh.service")
		}, true},
		{"debian port change, sshd stops", reload.Input{SSH: debian, Change: port(at22, both)}, func(h *hostfake.System) {
			respond(h, hostfake.Response{Stdout: ss22}, ssArgv...)
			respond(h, active, isActive("ssh.service")...)
			respond(h, inactive, isActive("ssh.service")...)
			respond(h, rejected, "systemctl", "reload", "ssh.service")
		}, true},
		{"debian reload succeeds, then sshd is down", reload.Input{SSH: debian, Change: permit}, func(h *hostfake.System) {
			respond(h, active, isActive("ssh.service")...)
			respond(h, failed, isActive("ssh.service")...)
			respond(h, ok, "systemctl", "reload", "ssh.service")
		}, false},
		{"alpine reload fails and OpenRC no longer has sshd started", reload.Input{SSH: alpine, Change: permit}, func(h *hostfake.System) {
			h.AddFile(detect.OpenRCStarted+"/sshd", nil, 0o644, 0, 0)
			h.Handler = func(argv []string, _ []byte) hostfake.Response {
				if err := h.Remove(context.Background(), detect.OpenRCStarted+"/sshd"); err != nil {
					return hostfake.Response{Err: err}
				}
				return hostfake.Response{Stderr: " * ERROR: sshd failed to reload\n", ExitCode: 1}
			}
		}, true},
		{"socket restart fails and leaves it down", reload.Input{SSH: ubuntu2604, Change: port(at22, both), DropIns: none}, func(h *hostfake.System) {
			respond(h, hostfake.Response{Stdout: ss22}, ssArgv...)
			respond(h, ok, "systemctl", "daemon-reload")
			respond(h, hostfake.Response{Stdout: listenBoth}, "systemctl", "show", "-p", "Listen", "ssh.socket")
			respond(h, active, isActive("ssh.socket")...)
			respond(h, failed, isActive("ssh.socket")...)
			respond(h, hostfake.Response{Stderr: "Job for ssh.socket failed.\n", ExitCode: 1}, "systemctl", "restart", "ssh.socket")
		}, true},
		{"socket restart succeeds, then the socket is down", reload.Input{SSH: ubuntu2604, Change: port(at22, both), DropIns: none}, func(h *hostfake.System) {
			respond(h, hostfake.Response{Stdout: ss22}, ssArgv...)
			respond(h, ok, "systemctl", "daemon-reload")
			respond(h, hostfake.Response{Stdout: listenBoth}, "systemctl", "show", "-p", "Listen", "ssh.socket")
			respond(h, active, isActive("ssh.socket")...)
			respond(h, failed, isActive("ssh.socket")...)
			respond(h, ok, "systemctl", "restart", "ssh.socket")
		}, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h := hostfake.New(host.Options{})
			tc.setup(h)
			rep, err := reload.Execute(context.Background(), h, plan(t, tc.in), reload.Options{})
			var xe *host.ExitError
			if !errors.Is(err, reload.ErrUnitDown) || errors.As(err, &xe) != tc.failed {
				t.Fatalf("err = %v, want ErrUnitDown (with the step's *host.ExitError: %v)", err, tc.failed)
			}
			if rep.Verified || rep.Reason != reload.ReasonNone || len(rep.Steps) == 0 || !rep.Steps[len(rep.Steps)-1].Ran {
				t.Errorf("report = %+v", rep)
			}
		})
	}
}

// TestExecuteRollback: a rollback starts the unit a failed change left
// down: ssh.socket is restarted, ssh.service and OpenRC's sshd started.
func TestExecuteRollback(t *testing.T) {
	failed := hostfake.Response{Stdout: "failed\n", ExitCode: 3}
	permit := reload.Change{Keywords: []string{"PermitRootLogin"}, Rollback: true}
	tests := []struct {
		name  string
		in    reload.Input
		setup func(*hostfake.System)
		runs  []string
	}{
		{"ubuntu: the failed socket is restarted", reload.Input{SSH: ubuntu2604, Change: rollback(at22), DropIns: none}, func(h *hostfake.System) {
			respond(h, ok, "systemctl", "daemon-reload")
			respond(h, hostfake.Response{Stdout: listen22}, "systemctl", "show", "-p", "Listen", "ssh.socket")
			respond(h, failed, isActive("ssh.socket")...)
			respond(h, active, isActive("ssh.socket")...)
			respond(h, ok, "systemctl", "restart", "ssh.socket")
			respond(h, hostfake.Response{Stdout: ss22}, ssArgv...)
		}, []string{"systemctl daemon-reload", "systemctl show -p Listen ssh.socket", "systemctl is-active ssh.socket",
			"systemctl restart ssh.socket", "systemctl is-active ssh.socket", "ss -Htln"}},
		{"debian: ssh.service is started", reload.Input{SSH: debian, Change: rollback(at22)}, func(h *hostfake.System) {
			respond(h, inactive, isActive("ssh.service")...)
			respond(h, active, isActive("ssh.service")...)
			respond(h, ok, "systemctl", "start", "ssh.service")
			respond(h, hostfake.Response{Stdout: ss22}, ssArgv...)
		}, []string{"systemctl is-active ssh.service", "systemctl start ssh.service", "systemctl is-active ssh.service", "ss -Htln"}},
		{"rocky: sshd.service waiting to restart is started", reload.Input{SSH: rocky, Change: permit}, func(h *hostfake.System) {
			respond(h, hostfake.Response{Stdout: "activating\n", ExitCode: 3}, isActive("sshd.service")...)
			respond(h, hostfake.Response{Stdout: "auto-restart\n"}, "systemctl", "show", "-p", "SubState", "--value", "sshd.service")
			respond(h, active, isActive("sshd.service")...)
			respond(h, ok, "systemctl", "start", "sshd.service")
		}, []string{"systemctl is-active sshd.service", "systemctl show -p SubState --value sshd.service", "systemctl start sshd.service", "systemctl is-active sshd.service"}},
		{"alpine: sshd is started", reload.Input{SSH: alpine, Change: permit}, func(h *hostfake.System) {
			h.Handler = func(argv []string, _ []byte) hostfake.Response {
				h.AddFile(detect.OpenRCStarted+"/sshd", nil, 0o644, 0, 0)
				return hostfake.Response{}
			}
		}, []string{"rc-service sshd start"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h := hostfake.New(host.Options{})
			tc.setup(h)
			rep, err := reload.Execute(context.WithoutCancel(context.Background()), h, plan(t, tc.in), reload.Options{})
			if err != nil || !rep.Steps[len(rep.Steps)-1].Started || rep.Verified != (len(tc.in.Change.After) > 0) {
				t.Fatalf("Execute = %+v, %v", rep, err)
			}
			checkRuns(t, h, tc.runs...)
		})
	}
	t.Run("the start fails", func(t *testing.T) {
		h := hostfake.New(host.Options{})
		respond(h, failed, isActive("ssh.service")...)
		respond(h, hostfake.Response{Stderr: "Job for ssh.service failed.\n", ExitCode: 1}, "systemctl", "start", "ssh.service")
		var xe *host.ExitError
		if _, err := reload.Execute(context.Background(), h, plan(t, reload.Input{SSH: debian, Change: rollback(at22)}), reload.Options{}); !errors.As(err, &xe) || !errors.Is(err, reload.ErrUnitDown) {
			t.Fatalf("err = %v, want the start's *host.ExitError and ErrUnitDown", err)
		}
	})
}

// TestExecuteInUse: a port the change adds that another socket holds is
// refused before anything runs. On a socket host the restart would fail and
// close port 22 too; elsewhere sshd would skip the port and ss would show
// the other program's socket as if it were sshd's.
func TestExecuteInUse(t *testing.T) {
	tests := []struct {
		name string
		ssh  detect.SSH
		ss   hostfake.Response
		want error // nil: ss's *host.ExitError
	}{
		{"ubuntu, 2230 taken", ubuntu2604, hostfake.Response{Stdout: ss22 + "LISTEN 0 128 0.0.0.0:2230 0.0.0.0:*\n"}, reload.ErrInUse},
		{"debian, 2230 taken on one address", debian, hostfake.Response{Stdout: ss22 + "LISTEN 0 128 192.0.2.10:2230 0.0.0.0:*\n"}, reload.ErrInUse},
		{"alpine, a line ss printed that is not read", alpine, hostfake.Response{Stdout: ss22 + "LISTEN 0 128 nonsense\n"}, reload.ErrInUse},
		{"ubuntu, ss missing", ubuntu2604, hostfake.Response{Err: host.ErrNotFound}, host.ErrNotFound},
		{"ubuntu, ss fails", ubuntu2604, hostfake.Response{ExitCode: 1}, nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h := hostfake.New(host.Options{})
			respond(h, tc.ss, ssArgv...)
			rep, err := reload.Execute(context.Background(), h, plan(t, reload.Input{SSH: tc.ssh, Change: port(at22, both), DropIns: none}), reload.Options{})
			var xe *host.ExitError
			if tc.want == nil && !errors.As(err, &xe) || tc.want != nil && !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
			if len(rep.Steps) != 0 {
				t.Errorf("steps ran: %+v", rep.Steps)
			}
			checkRuns(t, h, "ss -Htln")

			// CheckFree, the same check on its own for the plan, refuses alike.
			h = hostfake.New(host.Options{})
			respond(h, tc.ss, ssArgv...)
			err = reload.CheckFree(context.Background(), h, plan(t, reload.Input{SSH: tc.ssh, Change: port(at22, both), DropIns: none}))
			if tc.want == nil && !errors.As(err, &xe) || tc.want != nil && !errors.Is(err, tc.want) {
				t.Fatalf("CheckFree: err = %v, want %v", err, tc.want)
			}
			checkRuns(t, h, "ss -Htln")
		})
	}
}

// TestCheckFree: the plan-time check passes a port nothing holds and the
// listeners Hold keeps, reads nothing for a change that adds no listener,
// lets ss be missing where Execute does, and refuses a done ctx.
func TestCheckFree(t *testing.T) {
	ctx := context.Background()
	h := hostfake.New(host.Options{})
	respond(h, hostfake.Response{Stdout: ss22}, ssArgv...)
	if err := reload.CheckFree(ctx, h, plan(t, reload.Input{SSH: debian, Change: port(at22, both)})); err != nil {
		t.Errorf("a free port: %v", err)
	}
	checkRuns(t, h, "ss -Htln")

	h = hostfake.New(host.Options{})
	keyOnly := reload.Input{SSH: debian, Change: reload.Change{Keywords: []string{"PasswordAuthentication"}}}
	if err := reload.CheckFree(ctx, h, plan(t, keyOnly)); err != nil || len(h.Calls()) != 0 {
		t.Errorf("no listener added: %v, %d calls", err, len(h.Calls()))
	}

	h = hostfake.New(host.Options{})
	respond(h, hostfake.Response{Err: host.ErrNotFound}, ssArgv...)
	if err := reload.CheckFree(ctx, h, plan(t, reload.Input{SSH: alpine, Change: port(at22, both)})); err != nil {
		t.Errorf("ss missing off a socket host: %v", err)
	}

	done, cancel := context.WithCancel(ctx)
	cancel()
	if err := reload.CheckFree(done, hostfake.New(host.Options{}), plan(t, reload.Input{SSH: debian, Change: port(at22, both)})); !errors.Is(err, context.Canceled) {
		t.Errorf("done ctx: %v", err)
	}
}

// TestExecuteOwnListener: the sockets of the listeners the change keeps are
// sshd's, so widening ListenAddress 192.0.2.10 to 0.0.0.0 is not refused.
func TestExecuteOwnListener(t *testing.T) {
	h := hostfake.New(host.Options{})
	respond(h, hostfake.Response{Stdout: "LISTEN 0 128 192.0.2.10:22 0.0.0.0:*\n"}, ssArgv...)
	respond(h, hostfake.Response{Stdout: "LISTEN 0 128 0.0.0.0:22 0.0.0.0:*\n"}, ssArgv...)
	respond(h, active, isActive("ssh.service")...)
	respond(h, ok, "systemctl", "reload", "ssh.service")
	in := reload.Input{SSH: debian, Change: reload.Change{Keywords: []string{"ListenAddress"},
		Before: known([]sshd.Listener{l("192.0.2.10", 22)}), After: []sshd.Listener{l("0.0.0.0", 22)}}}
	if rep, err := reload.Execute(context.Background(), h, plan(t, in), reload.Options{}); err != nil || !rep.Verified {
		t.Fatalf("Execute = %+v, %v", rep, err)
	}
}

func TestExecuteReloadPolls(t *testing.T) {
	for _, tc := range []struct {
		name  string
		ssh   detect.SSH
		setup func(*hostfake.System)
		runs  []string
	}{
		{"debian", debian, func(h *hostfake.System) {
			respond(h, active, isActive("ssh.service")...)
			respond(h, ok, "systemctl", "reload", "ssh.service")
		}, []string{"ss -Htln", "systemctl is-active ssh.service", "systemctl reload ssh.service", "systemctl is-active ssh.service", "ss -Htln", "ss -Htln"}},
		{"alpine", alpine, func(h *hostfake.System) {
			h.AddSymlink(detect.OpenRCStarted+"/sshd", detect.OpenRCSSHD)
			h.AddFile(detect.OpenRCSSHD, []byte("#!/sbin/openrc-run\n"), 0o755, 0, 0)
			respond(h, ok, "rc-service", "sshd", "reload")
		}, []string{"ss -Htln", "rc-service sshd reload", "ss -Htln", "ss -Htln"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := hostfake.New(host.Options{})
			tc.setup(h)
			respond(h, hostfake.Response{Stdout: ss22}, ssArgv...) // before the change
			respond(h, hostfake.Response{Stdout: ss22}, ssArgv...) // sshd has not rebound yet
			respond(h, hostfake.Response{Stdout: ssBoth}, ssArgv...)
			var waits int
			rep, err := reload.Execute(context.Background(), h, plan(t, reload.Input{SSH: tc.ssh, Change: port(at22, both)}), noWait(&waits))
			if err != nil || !rep.Verified || waits != 1 {
				t.Fatalf("Execute = %+v, %v; %d waits", rep, err, waits)
			}
			checkRuns(t, h, tc.runs...)
		})
	}
}

func TestExecuteFails(t *testing.T) {
	debianPort := reload.Input{SSH: debian, Change: port(at22, both)}
	before := func(h *hostfake.System) { respond(h, hostfake.Response{Stdout: ss22}, ssArgv...) }
	exitError := func(t *testing.T, _ reload.Report, err error) {
		var xe *host.ExitError
		if !errors.As(err, &xe) || errors.Is(err, reload.ErrUnitDown) {
			t.Errorf("err = %v, want a *host.ExitError, the unit up", err)
		}
	}
	tests := []struct {
		name  string
		in    reload.Input
		setup func(*hostfake.System)
		check func(*testing.T, reload.Report, error)
	}{
		{"sshd -t rejects the files, the old sshd runs", debianPort, func(h *hostfake.System) {
			before(h)
			respond(h, active, isActive("ssh.service")...)
			respond(h, hostfake.Response{Stderr: "Job for ssh.service failed because the control process exited with error code.\n", ExitCode: 1}, "systemctl", "reload", "ssh.service")
		}, exitError},
		{"never bound", debianPort, func(h *hostfake.System) {
			before(h)
			respond(h, active, isActive("ssh.service")...)
			respond(h, ok, "systemctl", "reload", "ssh.service")
			respond(h, hostfake.Response{Stdout: ss22 + "LISTEN 0 128 nonsense\n"}, ssArgv...)
		}, func(t *testing.T, _ reload.Report, err error) {
			var le *reload.ListenerError
			if !errors.Is(err, reload.ErrNotBound) || !errors.As(err, &le) || !slices.Equal(le.Missing, at2230) || len(le.Bad) != 1 {
				t.Errorf("err = %v, want ErrNotBound missing %v", err, at2230)
			}
		}},
		{"unit state not established", debianPort, func(h *hostfake.System) {
			before(h)
			respond(h, hostfake.Response{Stdout: "activating\n", ExitCode: 3}, isActive("ssh.service")...)
		}, func(t *testing.T, _ reload.Report, err error) {
			if !errors.Is(err, reload.ErrUnitState) {
				t.Errorf("err = %v, want ErrUnitState", err)
			}
		}},
		{"after the reload, sshd.service waiting to restart: down", reload.Input{SSH: rocky, Change: reload.Change{Keywords: []string{"PermitRootLogin"}}}, func(h *hostfake.System) {
			respond(h, active, isActive("sshd.service")...)
			respond(h, hostfake.Response{Stdout: "activating\n", ExitCode: 3}, isActive("sshd.service")...)
			respond(h, ok, "systemctl", "reload", "sshd.service")
			respond(h, hostfake.Response{Stdout: "auto-restart\n"}, "systemctl", "show", "-p", "SubState", "--value", "sshd.service")
		}, func(t *testing.T, rep reload.Report, err error) {
			if !errors.Is(err, reload.ErrUnitDown) || errors.Is(err, reload.ErrUnitState) || len(rep.Steps) != 1 || !rep.Steps[0].Ran {
				t.Errorf("err = %v, want ErrUnitDown after the reload ran", err)
			}
		}},
		{"activating, not waiting to restart: not established", reload.Input{SSH: rocky, Change: reload.Change{Keywords: []string{"PermitRootLogin"}}}, func(h *hostfake.System) {
			respond(h, hostfake.Response{Stdout: "activating\n", ExitCode: 3}, isActive("sshd.service")...)
			respond(h, hostfake.Response{Stdout: "start\n"}, "systemctl", "show", "-p", "SubState", "--value", "sshd.service")
		}, func(t *testing.T, _ reload.Report, err error) {
			if !errors.Is(err, reload.ErrUnitState) {
				t.Errorf("err = %v, want ErrUnitState", err)
			}
		}},
		{"after the reload, ssh.service activating again", debianPort, func(h *hostfake.System) {
			before(h)
			respond(h, active, isActive("ssh.service")...)
			respond(h, hostfake.Response{Stdout: "activating\n", ExitCode: 3}, isActive("ssh.service")...)
			respond(h, ok, "systemctl", "reload", "ssh.service")
		}, func(t *testing.T, rep reload.Report, err error) {
			if !errors.Is(err, reload.ErrUnitState) || len(rep.Steps) != 1 || !rep.Steps[0].Ran {
				t.Errorf("err = %v, want ErrUnitState after the reload ran", err)
			}
		}},
		{"a failed reload stands when the state after it is not established", debianPort, func(h *hostfake.System) {
			before(h)
			respond(h, active, isActive("ssh.service")...)
			respond(h, hostfake.Response{Stdout: "deactivating\n", ExitCode: 3}, isActive("ssh.service")...)
			respond(h, hostfake.Response{ExitCode: 1}, "systemctl", "reload", "ssh.service")
		}, exitError},
		{"systemctl missing", debianPort, func(h *hostfake.System) {
			before(h)
			respond(h, hostfake.Response{Err: host.ErrNotFound}, isActive("ssh.service")...)
		}, func(t *testing.T, _ reload.Report, err error) {
			if !errors.Is(err, host.ErrNotFound) {
				t.Errorf("err = %v, want host.ErrNotFound", err)
			}
		}},
		{"OpenRC's state unreadable", reload.Input{SSH: alpine, Change: reload.Change{Keywords: []string{"PermitRootLogin"}}}, func(h *hostfake.System) {
			h.AddFile(detect.OpenRCStarted, nil, 0o644, 0, 0)
		}, func(t *testing.T, _ reload.Report, err error) {
			if !errors.Is(err, syscall.ENOTDIR) {
				t.Errorf("err = %v, want ENOTDIR", err)
			}
		}},
		{"ss fails after the reload", debianPort, func(h *hostfake.System) {
			before(h)
			respond(h, active, isActive("ssh.service")...)
			respond(h, ok, "systemctl", "reload", "ssh.service")
			respond(h, hostfake.Response{ExitCode: 1}, ssArgv...)
		}, exitError},
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
			tc.check(t, rep, err)
		})
	}
}

// TestExecuteCancelledWhilePolling: Ctrl-C after the reload ran stops the
// listener check with ctx's error and a report of the steps, for the
// caller's rollback; with Options.Wait unset, the wait ends at once.
func TestExecuteCancelledWhilePolling(t *testing.T) {
	for _, wait := range []bool{true, false} {
		ctx, cancel := context.WithCancel(context.Background())
		h := hostfake.New(host.Options{})
		respond(h, active, isActive("ssh.service")...)
		respond(h, ok, "systemctl", "reload", "ssh.service")
		var reads int
		h.Handler = func(argv []string, _ []byte) hostfake.Response {
			if reads++; reads == 2 {
				cancel()
			}
			return hostfake.Response{Stdout: ss22}
		}
		opts := reload.Options{Interval: time.Hour}
		if wait {
			opts.Wait = func(ctx context.Context, _ time.Duration) error { return ctx.Err() }
		}
		rep, err := reload.Execute(ctx, h, plan(t, reload.Input{SSH: debian, Change: port(at22, both)}), opts)
		if !errors.Is(err, context.Canceled) || len(rep.Steps) != 1 || !rep.Steps[0].Ran || len(rep.Bound) != 2 {
			t.Errorf("Wait set %v: Execute = %+v, %v; want context.Canceled after the reload", wait, rep, err)
		}
		cancel()
	}
}

func TestExecuteSSMissing(t *testing.T) {
	h := hostfake.New(host.Options{})
	h.AddFile(detect.OpenRCStarted+"/sshd", nil, 0o644, 0, 0)
	respond(h, ok, "rc-service", "sshd", "reload")
	respond(h, hostfake.Response{Err: host.ErrNotFound}, ssArgv...)
	rep, err := reload.Execute(context.Background(), h, plan(t, reload.Input{SSH: alpine, Change: port(at22, both)}), reload.Options{})
	if err != nil || rep.Verified || rep.Reason != reload.ReasonNoSS || !strings.Contains(rep.Unverified, "ss is not installed") {
		t.Fatalf("Execute = %+v, %v", rep, err)
	}
	checkRuns(t, h, "ss -Htln", "rc-service sshd reload", "ss -Htln")
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
	rep, err := reload.Execute(context.WithoutCancel(ctx), h, p, reload.Options{})
	if err != nil || rep.Reason != reload.ReasonNoListenerChange || rep.Unverified != "no listener changed" {
		t.Fatalf("Execute = %+v, %v", rep, err)
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

func TestReportStrings(t *testing.T) {
	le := &reload.ListenerError{Source: "ss -Htln", Missing: at2230, Bad: []string{"LISTEN 0 128 nonsense"}, Err: reload.ErrNotBound}
	if want := `reload: ss -Htln lacks [0.0.0.0:2230 [::]:2230]: not every listener is bound (unread lines ["LISTEN 0 128 nonsense"])`; le.Error() != want {
		t.Errorf("ListenerError = %q, want %q", le.Error(), want)
	}
	var got []string
	for r := reload.ReasonNone; r <= reload.ReasonNoSS+1; r++ {
		got = append(got, r.String())
	}
	if want := []string{"", "not-up", "no-listener-change", "no-ss", "unknown"}; !slices.Equal(got, want) {
		t.Errorf("Reason strings = %q, want %q", got, want)
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

// TestParseListening reads ss -Htlnp's and ss -Hulnp's lines in the form
// deb13-vm (iproute2 6.15.0), ubuntu26-vm (6.19.0) and rocky10-test-110
// (6.17.0) printed them on 2026-11-11, the addresses made synthetic: TCP
// LISTEN and UDP UNCONN, the Process column's users:(("<name>",pid=N,fd=M)),
// two programs on one socket (ssh.socket's), a padded trailing space, "*",
// "[::]", "%lo" and "%eth0", a line with no Process column (ss run without
// root), and lines of another state or that it cannot read.
func TestParseListening(t *testing.T) {
	tcp := "LISTEN 0      4096         0.0.0.0:5355 0.0.0.0:* users:((\"systemd-resolve\",pid=62455,fd=12))\n" +
		"LISTEN 0      4096   127.0.0.53%lo:53   0.0.0.0:* users:((\"systemd-resolve\",pid=62455,fd=21))\n" +
		"LISTEN 0      4096         0.0.0.0:22 0.0.0.0:* users:((\"sshd\",pid=347637,fd=3),(\"systemd\",pid=1,fd=244))\n" +
		"LISTEN 0      128             [::]:22      [::]:* users:((\"sshd\",pid=62444,fd=7))            \n" +
		"LISTEN 0      4096               *:9090       *:* users:((\"systemd\",pid=1,fd=86))\n" +
		"LISTEN 0      4096         0.0.0.0:8000 0.0.0.0:* users:((\"web server\",pid=4242,fd=5))\n" +
		"LISTEN 0      4096         0.0.0.0:8080 0.0.0.0:*\n" +
		"UNCONN 0      0            0.0.0.0:68   0.0.0.0:*\n" +
		"LISTEN 0      128       [::1:2230           [::]:* users:((\"sshd\",pid=1,fd=3))\n"
	udp := "UNCONN 0      0              0.0.0.0:5355 0.0.0.0:* users:((\"systemd-resolve\",pid=62455,fd=11))\n" +
		"UNCONN 0      0      192.0.2.20%eth0:68   0.0.0.0:* users:((\"systemd-network\",pid=593,fd=34))  \n" +
		"UNCONN 0      0                [::1]:323    [::]:* users:((\"chronyd\",pid=853,fd=5))\n" +
		"LISTEN 0      4096         0.0.0.0:22 0.0.0.0:*\n" +
		"UNCONN 0      0              0.0.0.0:0 0.0.0.0:*\n"
	for _, tc := range []struct {
		proto, out string
		want       []string
		bad        int
	}{
		{"tcp", tcp, []string{"tcp 0.0.0.0:5355 systemd-resolve", "tcp 127.0.0.53%lo:53 systemd-resolve", "tcp 0.0.0.0:22 sshd", "tcp [::]:22 sshd",
			"tcp *:9090 systemd", "tcp 0.0.0.0:8000 web server", "tcp 0.0.0.0:8080 "}, 1},
		{"udp", udp, []string{"udp 0.0.0.0:5355 systemd-resolve", "udp 192.0.2.20%eth0:68 systemd-network", "udp [::1]:323 chronyd"}, 1},
	} {
		socks, bad := reload.ParseListening([]byte(tc.out), tc.proto)
		var got []string
		for _, s := range socks {
			got = append(got, s.Proto+" "+s.String()+" "+s.Process)
		}
		if !slices.Equal(got, tc.want) || len(bad) != tc.bad {
			t.Errorf("%s: %q, bad %q\nwant %q", tc.proto, got, bad, tc.want)
		}
	}
}

// TestReadListening: TCP's table, then UDP's, each its own run; a run
// that fails is the error, and ss missing is host.ErrNotFound.
func TestReadListening(t *testing.T) {
	ctx := context.Background()
	h := hostfake.New(host.Options{})
	respond(h, hostfake.Response{Stdout: "LISTEN 0 4096 0.0.0.0:8000 0.0.0.0:* users:((\"vaultwarden\",pid=7,fd=9))\n"}, reload.ListenTCPArgv...)
	respond(h, hostfake.Response{Stdout: "UNCONN 0 0 0.0.0.0:3478 0.0.0.0:* users:((\"turnserver\",pid=8,fd=3))\nUNCONN 0 0 nowhere 0.0.0.0:*\n"}, reload.ListenUDPArgv...)
	socks, bad, err := reload.ReadListening(ctx, h)
	if err != nil || len(socks) != 2 || socks[0].Proto != "tcp" || socks[0].Process != "vaultwarden" || socks[1].Proto != "udp" || socks[1].Port != 3478 || len(bad) != 1 {
		t.Errorf("%+v, bad %q, %v", socks, bad, err)
	}
	checkRuns(t, h, "ss -Htlnp", "ss -Hulnp")

	h = hostfake.New(host.Options{})
	respond(h, hostfake.Response{}, reload.ListenTCPArgv...)
	respond(h, hostfake.Response{Stderr: "ss: netlink error\n", ExitCode: 1}, reload.ListenUDPArgv...)
	if socks, _, err := reload.ReadListening(ctx, h); err == nil || socks != nil {
		t.Errorf("a failed run: %+v, %v", socks, err)
	}
	h = hostfake.New(host.Options{})
	respond(h, hostfake.Response{Err: host.ErrNotFound}, reload.ListenTCPArgv...)
	if _, _, err := reload.ReadListening(ctx, h); !errors.Is(err, host.ErrNotFound) {
		t.Errorf("ss missing: %v", err)
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
			_, err := reload.PlanFor(reload.Input{SSH: debian, Change: reload.Change{Keywords: []string{"ListenAddress"}, Before: known(tc.before), After: tc.have}})
			if (err == nil) != tc.ok {
				t.Errorf("err = %v, want covered %v", err, tc.ok)
			}
		})
	}
}

// TestCheckInEffect: a forward listener change is refused where the
// configured listeners are not what is bound, or do not cover the port
// the applying session is connected to (next/p2-revert-plans): a
// daemon-reload since ssh.socket was last restarted (configured 2230,
// bound 22), and a session on 22 where only 2230 is configured. ss
// missing fails it on a socket host only.
func TestCheckInEffect(t *testing.T) {
	ss2230 := "LISTEN 0      4096               *:2230              *:*\n"
	on22 := netip.MustParseAddrPort("192.0.2.20:22")
	on2230 := netip.MustParseAddrPort("192.0.2.20:2230")
	v6on22 := netip.MustParseAddrPort("[2001:db8::20]:22")
	socket2230 := []sshd.Listener{l("::", 2230)}
	tests := []struct {
		name       string
		mode       detect.SSHMode
		configured []sshd.Listener
		ss         hostfake.Response
		server     netip.AddrPort
		want       error
	}{
		{"socket, as bound, session on 22", detect.SSHSocket, at22, hostfake.Response{Stdout: ss22}, on22, nil},
		{"socket on [::]:2230, dual-stack, session on 2230", detect.SSHSocket, socket2230, hostfake.Response{Stdout: ss2230}, on2230, nil},
		{"service, IPv6 session", detect.SSHService, at22, hostfake.Response{Stdout: ss22}, v6on22, nil},
		{"console session: no port to keep", detect.SSHSocket, socket2230, hostfake.Response{Stdout: ss2230}, netip.AddrPort{}, nil},
		{"socket configured 2230, bound 22", detect.SSHSocket, socket2230, hostfake.Response{Stdout: ss22}, netip.AddrPort{}, reload.ErrNotInEffect},
		{"session on a port not configured", detect.SSHSocket, socket2230, hostfake.Response{Stdout: ss2230}, on22, reload.ErrNotInEffect},
		{"service: sshd configured 2230 still on 22", detect.SSHService, at2230, hostfake.Response{Stdout: ss22}, netip.AddrPort{}, reload.ErrNotInEffect},
		{"service: [::] of sshd is not dual-stack", detect.SSHService, at22[1:], hostfake.Response{Stdout: ss22}, on22, reload.ErrNotInEffect},
		{"socket, ss missing", detect.SSHSocket, at22, hostfake.Response{Err: host.ErrNotFound}, on22, host.ErrNotFound},
		{"openrc, ss missing", detect.SSHOpenRC, at22, hostfake.Response{Err: host.ErrNotFound}, on22, nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h := hostfake.New(host.Options{})
			respond(h, tc.ss, ssArgv...)
			err := reload.CheckInEffect(context.Background(), h, tc.mode, tc.configured, tc.server)
			if !errors.Is(err, tc.want) || (tc.want == nil) != (err == nil) {
				t.Errorf("CheckInEffect = %v, want %v", err, tc.want)
			}
			for _, c := range h.Calls() {
				if c.Op != "run" {
					t.Errorf("unexpected call %s", c)
				}
			}
		})
	}
}
