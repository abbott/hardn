package detect_test

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"strings"
	"testing"

	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/hostfake"
)

func TestDetectInit(t *testing.T) {
	ctx := context.Background()
	with := func(dirs []string, files ...string) *hostfake.System {
		f := newFake(t, "")
		for _, d := range dirs {
			f.AddDir(d, 0o755, 0, 0)
		}
		for _, p := range files {
			f.AddFile(p, nil, 0o755, 0, 0)
		}
		return f
	}
	tests := []struct {
		name    string
		sys     host.System
		kind    detect.InitKind
		running detect.Tri
		wantErr error
	}{
		{"systemd booted", with([]string{detect.SystemdRunDir}), detect.Systemd, detect.Yes, nil},
		{"OpenRC booted", with([]string{detect.OpenRCRunDir}, detect.OpenRCRun), detect.OpenRC, detect.Yes, nil},
		{"OpenRC installed, not booted (container)", with(nil, detect.OpenRCRun), detect.OpenRC, detect.No, nil},
		{"systemd installed is not booted", with(nil, "/usr/lib/systemd/systemd"), detect.InitUnknown, detect.Unknown, detect.ErrNoSignal},
		{"a file is not the run directory", with(nil, detect.SystemdRunDir), detect.InitUnknown, detect.Unknown, detect.ErrNoSignal},
		{"both run directories", with([]string{detect.SystemdRunDir, detect.OpenRCRunDir}), detect.InitUnknown, detect.Unknown, detect.ErrConflict},
		{"run directory unreadable", deny(with([]string{detect.OpenRCRunDir}), detect.SystemdRunDir), detect.InitUnknown, detect.Unknown, fs.ErrPermission},
	}
	for _, tt := range tests {
		got := detect.DetectInit(ctx, tt.sys)
		if got.Kind != tt.kind || got.Running != tt.running || !matches(got.Err, tt.wantErr) {
			t.Errorf("%s: DetectInit = %s running %s err %v; want %s running %s err %v", tt.name, got.Kind, got.Running, got.Err, tt.kind, tt.running, tt.wantErr)
		}
	}
}

// systemctlStates scripts systemctl is-enabled/is-active for ssh.socket and
// ssh.service; each state is "<stdout>" or "<stdout>:<exit>".
func systemctlStates(f *hostfake.System, sockEnabled, sockActive, svcEnabled, svcActive string) {
	for _, s := range []struct{ verb, unit, state string }{
		{"is-enabled", "ssh.socket", sockEnabled}, {"is-active", "ssh.socket", sockActive},
		{"is-enabled", "ssh.service", svcEnabled}, {"is-active", "ssh.service", svcActive},
	} {
		stdout, code, _ := strings.Cut(s.state, ":")
		exit := 0
		if code != "" {
			_, _ = fmt.Sscan(code, &exit)
		}
		f.Respond(out(stdout+"\n", exit), "systemctl", s.verb, s.unit)
	}
}

func TestDetectSSH(t *testing.T) {
	ctx := context.Background()
	systemd := detect.Init{Kind: detect.Systemd, Running: detect.Yes}
	errNoSystemctl := fmt.Errorf("fixture: %w", host.ErrNotFound)
	tests := []struct {
		name      string
		setup     func(f *hostfake.System)
		in        detect.Init
		mode      detect.SSHMode
		generator detect.Tri
		wantErr   error
	}{
		{"Ubuntu 24.04/26.04 fresh: socket, service not yet started", func(f *hostfake.System) {
			f.AddFile(detect.SocketGenerator, nil, 0o755, 0, 0)
			systemctlStates(f, "enabled", "active", "disabled:1", "inactive:3")
		}, systemd, detect.SSHSocket, detect.Yes, nil},
		{"Ubuntu socket after a connection", func(f *hostfake.System) {
			f.AddFile(detect.SocketGenerator, nil, 0o755, 0, 0)
			systemctlStates(f, "enabled", "active", "disabled:1", "active")
		}, systemd, detect.SSHSocket, detect.Yes, nil},
		{"Debian 12/13, PVE 9, Ubuntu 22.04: service", func(f *hostfake.System) {
			systemctlStates(f, "disabled:1", "inactive:3", "enabled", "active")
		}, systemd, detect.SSHService, detect.No, nil},
		{"Debian opted into the socket: no generator", func(f *hostfake.System) {
			systemctlStates(f, "enabled", "active", "disabled:1", "inactive:3")
		}, systemd, detect.SSHSocket, detect.No, nil},
		{"masked socket", func(f *hostfake.System) {
			systemctlStates(f, "masked:1", "inactive:3", "enabled", "active")
		}, systemd, detect.SSHService, detect.No, nil},
		{"both enabled", func(f *hostfake.System) {
			systemctlStates(f, "enabled", "active", "enabled", "active")
		}, systemd, detect.SSHUnknown, detect.No, detect.ErrConflict},
		{"no openssh-server", func(f *hostfake.System) {
			systemctlStates(f, "not-found:4", "inactive:4", "not-found:4", "inactive:4")
		}, systemd, detect.SSHUnknown, detect.No, detect.ErrNoSignal},
		{"socket starting", func(f *hostfake.System) {
			systemctlStates(f, "static", "activating:3", "disabled:1", "inactive:3")
		}, systemd, detect.SSHUnknown, detect.No, detect.ErrUnrecognised},
		{"unrecognised output", func(f *hostfake.System) {
			systemctlStates(f, "Failed to connect to bus:1", "inactive:3", "enabled", "active")
		}, systemd, detect.SSHUnknown, detect.No, detect.ErrUnrecognised},
		{"systemctl missing", func(f *hostfake.System) {
			f.Handler = func([]string, []byte) hostfake.Response { return hostfake.Response{Err: errNoSystemctl} }
		}, systemd, detect.SSHUnknown, detect.No, host.ErrNotFound},
		{"Alpine: OpenRC sshd", func(f *hostfake.System) {
			f.AddFile(detect.OpenRCSSHD, nil, 0o755, 0, 0)
		}, detect.Init{Kind: detect.OpenRC, Running: detect.Yes}, detect.SSHOpenRC, detect.Unknown, nil},
		{"OpenRC without sshd", func(*hostfake.System) {}, detect.Init{Kind: detect.OpenRC, Running: detect.Yes}, detect.SSHUnknown, detect.Unknown, detect.ErrNoSignal},
		{"init not established: nothing runs", func(*hostfake.System) {}, detect.Init{Err: detect.ErrConflict}, detect.SSHUnknown, detect.Unknown, detect.ErrConflict},
	}
	for _, tt := range tests {
		f := newFake(t, "")
		tt.setup(f)
		got := detect.DetectSSH(ctx, f, tt.in)
		if got.Mode != tt.mode || got.Generator != tt.generator || !matches(got.Err, tt.wantErr) {
			t.Errorf("%s: DetectSSH = %s generator %s err %v; want %s generator %s err %v", tt.name, got.Mode, got.Generator, got.Err, tt.mode, tt.generator, tt.wantErr)
		}
		for _, argv := range f.Runs() {
			if argv[0] != "systemctl" || len(argv) != 3 || (argv[2] != "ssh.socket" && argv[2] != "ssh.service") {
				t.Errorf("%s: ran %q; detection queries ssh.socket and ssh.service only, never sshd", tt.name, argv)
			}
		}
		if tt.in.Kind != detect.Systemd && len(f.Runs()) > 0 {
			t.Errorf("%s: ran %q without systemd", tt.name, f.Runs())
		}
		readOnly(t, f)
	}
}

// The mode stands when only the generator cannot be looked for.
func TestDetectSSHGeneratorUnknown(t *testing.T) {
	f := newFake(t, "")
	systemctlStates(f, "enabled", "active", "disabled:1", "inactive:3")
	got := detect.DetectSSH(context.Background(), deny(f, detect.SocketGenerator), detect.Init{Kind: detect.Systemd, Running: detect.Yes})
	if got.Mode != detect.SSHSocket || got.Generator != detect.Unknown || !errors.Is(got.Err, fs.ErrPermission) {
		t.Errorf("DetectSSH = %+v; want ssh.socket, generator unknown (permission)", got)
	}
}

// matches is errors.Is, with a nil want meaning no error.
func matches(err, want error) bool {
	if want == nil {
		return err == nil
	}
	return errors.Is(err, want)
}
