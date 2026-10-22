package detect_test

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"slices"
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
		{"openrc-run unreadable", deny(with(nil), detect.OpenRCRun), detect.InitUnknown, detect.Unknown, fs.ErrPermission},
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

// vendorGenerator installs Ubuntu's sshd-socket-generator: a non-empty
// executable, since an empty file masks a generator.
func vendorGenerator(f *hostfake.System) {
	f.AddFile(detect.SocketGenerator, []byte("\x7fELF"), 0o755, 0, 0)
}

var (
	systemdBooted = detect.Init{Kind: detect.Systemd, Running: detect.Yes, Source: detect.SystemdRunDir}
	openRCBooted  = detect.Init{Kind: detect.OpenRC, Running: detect.Yes, Source: detect.OpenRCRunDir}
)

func TestDetectSSH(t *testing.T) {
	ctx := context.Background()
	errNoSystemctl := fmt.Errorf("fixture: %w", host.ErrNotFound)
	states := func(sockEnabled, sockActive, svcEnabled, svcActive string) func(*hostfake.System) {
		return func(f *hostfake.System) { systemctlStates(f, sockEnabled, sockActive, svcEnabled, svcActive) }
	}
	withGenerator := func(setup func(*hostfake.System)) func(*hostfake.System) {
		return func(f *hostfake.System) { vendorGenerator(f); setup(f) }
	}
	tests := []struct {
		name      string
		setup     func(f *hostfake.System)
		in        detect.Init
		mode      detect.SSHMode
		generator detect.Tri
		wantErr   error
	}{
		{"Ubuntu 24.04/26.04 fresh: socket, service not yet started", withGenerator(states("enabled", "active", "disabled:1", "inactive:3")), systemdBooted, detect.SSHSocket, detect.Yes, nil},
		{"Ubuntu socket after a connection", withGenerator(states("enabled", "active", "disabled:1", "active")), systemdBooted, detect.SSHSocket, detect.Yes, nil},
		{"Debian 12/13, PVE 9, Ubuntu 22.04: service", states("disabled:1", "inactive:3", "enabled", "active"), systemdBooted, detect.SSHService, detect.No, nil},
		{"Debian opted into the socket: no generator", states("enabled", "active", "disabled:1", "inactive:3"), systemdBooted, detect.SSHSocket, detect.No, nil},
		{"masked socket", states("masked:1", "inactive:3", "enabled", "active"), systemdBooted, detect.SSHService, detect.No, nil},
		{"socket enabled, stopped; service not running", states("enabled", "inactive:3", "disabled:1", "inactive:3"), systemdBooted, detect.SSHSocket, detect.No, nil},
		{"socket started by hand", states("disabled:1", "active", "disabled:1", "inactive:3"), systemdBooted, detect.SSHSocket, detect.No, nil},
		{"service enabled, stopped", states("disabled:1", "inactive:3", "enabled", "inactive:3"), systemdBooted, detect.SSHService, detect.No, nil},
		{"service started by hand", states("disabled:1", "inactive:3", "disabled:1", "active"), systemdBooted, detect.SSHService, detect.No, nil},

		{"both enabled", states("enabled", "active", "enabled", "active"), systemdBooted, detect.SSHUnknown, detect.No, detect.ErrConflict},
		{"socket enabled but inactive, service running without it", states("enabled", "inactive:3", "disabled:1", "active"), systemdBooted, detect.SSHUnknown, detect.No, detect.ErrConflict},
		{"socket active, service enabled and running", states("disabled:1", "active", "enabled", "active"), systemdBooted, detect.SSHUnknown, detect.No, detect.ErrConflict},
		{"socket active, service enabled and stopped", states("disabled:1", "active", "enabled", "inactive:3"), systemdBooted, detect.SSHUnknown, detect.No, detect.ErrConflict},
		{"no openssh-server", states("not-found:4", "inactive:4", "not-found:4", "inactive:4"), systemdBooted, detect.SSHUnknown, detect.No, detect.ErrNoSignal},
		{"socket starting", states("static", "activating:3", "disabled:1", "inactive:3"), systemdBooted, detect.SSHUnknown, detect.No, detect.ErrUnrecognised},
		{"socket enabled, stopped; service starting", states("enabled", "inactive:3", "disabled:1", "activating:3"), systemdBooted, detect.SSHUnknown, detect.No, detect.ErrUnrecognised},
		{"unrecognised output", states("Failed to connect to bus:1", "inactive:3", "enabled", "active"), systemdBooted, detect.SSHUnknown, detect.No, detect.ErrUnrecognised},
		{"systemctl missing", func(f *hostfake.System) {
			f.Handler = func([]string, []byte) hostfake.Response { return hostfake.Response{Err: errNoSystemctl} }
		}, systemdBooted, detect.SSHUnknown, detect.No, host.ErrNotFound},

		{"systemd installed, not booted: nothing runs", states("enabled", "active", "disabled:1", "inactive:3"), detect.Init{Kind: detect.Systemd, Running: detect.No}, detect.SSHUnknown, detect.Unknown, detect.ErrNoSignal},
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
		if (tt.in.Kind != detect.Systemd || tt.in.Running != detect.Yes) && len(f.Calls()) > 0 {
			t.Errorf("%s: looked at the host (%v) without a booted systemd", tt.name, f.Calls())
		}
		readOnly(t, f)
	}
}

// Generator is resolved as systemd resolves generators: the first
// directory holding the name decides, and a mask there wins.
func TestDetectSSHGenerator(t *testing.T) {
	ctx := context.Background()
	const (
		inRun   = "/run/systemd/system-generators/sshd-socket-generator"
		inEtc   = "/etc/systemd/system-generators/sshd-socket-generator"
		inLocal = "/usr/local/lib/systemd/system-generators/sshd-socket-generator"
	)
	tests := []struct {
		name      string
		setup     func(f *hostfake.System) host.System
		generator detect.Tri
		path      string
		wantErr   error // SSH.Err; the mode stands either way
	}{
		{"vendor generator", func(f *hostfake.System) host.System {
			vendorGenerator(f)
			return f
		}, detect.Yes, detect.SocketGenerator, nil},
		{"not installed", func(f *hostfake.System) host.System { return f }, detect.No, "", nil},
		{"masked in /etc by a link to /dev/null", func(f *hostfake.System) host.System {
			vendorGenerator(f)
			f.AddSymlink(inEtc, "/dev/null")
			return f
		}, detect.No, inEtc, nil},
		{"masked in /run by an empty file", func(f *hostfake.System) host.System {
			vendorGenerator(f)
			f.AddFile(inRun, nil, 0o644, 0, 0)
			return f
		}, detect.No, inRun, nil},
		{"masked by a link to an empty file", func(f *hostfake.System) host.System {
			vendorGenerator(f)
			f.AddFile("/etc/systemd/empty", nil, 0o644, 0, 0)
			f.AddSymlink(inEtc, "../empty")
			return f
		}, detect.No, inEtc, nil},
		{"/run's mask beats /etc's program", func(f *hostfake.System) host.System {
			f.AddSymlink(inRun, "/dev/null")
			f.AddFile(inEtc, []byte("#!/bin/sh\n"), 0o755, 0, 0)
			return f
		}, detect.No, inRun, nil},
		{"overridden by another program", func(f *hostfake.System) host.System {
			vendorGenerator(f)
			f.AddFile(inLocal, []byte("#!/bin/sh\n"), 0o755, 0, 0)
			return f
		}, detect.Unknown, inLocal, detect.ErrUnrecognised},
		{"dangling link", func(f *hostfake.System) host.System {
			vendorGenerator(f)
			f.AddSymlink(inEtc, "/nonexistent")
			return f
		}, detect.Unknown, inEtc, detect.ErrUnrecognised},
		{"vendor file not executable", func(f *hostfake.System) host.System {
			f.AddFile(detect.SocketGenerator, []byte("\x7fELF"), 0o644, 0, 0)
			return f
		}, detect.Unknown, detect.SocketGenerator, detect.ErrUnrecognised},
		{"unreadable entry", func(f *hostfake.System) host.System {
			vendorGenerator(f)
			return deny(f, detect.SocketGenerator)
		}, detect.Unknown, detect.SocketGenerator, fs.ErrPermission},
		{"unreadable /etc entry shadows the vendor's", func(f *hostfake.System) host.System {
			vendorGenerator(f)
			f.AddSymlink(inEtc, "/dev/null")
			return deny(f, inEtc)
		}, detect.Unknown, inEtc, fs.ErrPermission},
	}
	for _, tt := range tests {
		f := newFake(t, "")
		systemctlStates(f, "enabled", "active", "disabled:1", "inactive:3")
		got := detect.DetectSSH(ctx, tt.setup(f), systemdBooted)
		if got.Mode != detect.SSHSocket || got.Generator != tt.generator || got.GeneratorPath != tt.path || !matches(got.Err, tt.wantErr) {
			t.Errorf("%s: DetectSSH = %s, generator %s at %q, err %v; want ssh.socket, generator %s at %q, err %v",
				tt.name, got.Mode, got.Generator, got.GeneratorPath, got.Err, tt.generator, tt.path, tt.wantErr)
		}
		readOnly(t, f)
	}
}

// On OpenRC, sshd is OpenRC's when OpenRC has started it or it is in a
// runlevel, as a unit is systemd's when it is active or enabled.
func TestDetectSSHOpenRC(t *testing.T) {
	ctx := context.Background()
	script := func(f *hostfake.System) { f.AddFile(detect.OpenRCSSHD, []byte("#!/sbin/openrc-run\n"), 0o755, 0, 0) }
	started := func(f *hostfake.System) { f.AddSymlink(detect.OpenRCStarted+"/sshd", detect.OpenRCSSHD) }
	runlevel := func(f *hostfake.System, level string) {
		f.AddDir(detect.OpenRCRunlevels+"/"+level, 0o755, 0, 0)
		f.AddSymlink(detect.OpenRCRunlevels+"/"+level+"/sshd", detect.OpenRCSSHD)
	}
	runlevels := func(f *hostfake.System) {
		for _, l := range []string{"boot", "default", "nonetwork", "shutdown", "sysinit"} {
			f.AddDir(detect.OpenRCRunlevels+"/"+l, 0o755, 0, 0)
		}
	}
	tests := []struct {
		name       string
		setup      func(f *hostfake.System) host.System
		in         detect.Init
		mode       detect.SSHMode
		started    detect.Tri
		inRunlevel detect.Tri
		levels     []string
		wantErr    error
	}{
		{"Alpine: started, in default", func(f *hostfake.System) host.System {
			script(f)
			runlevels(f)
			runlevel(f, "default")
			started(f)
			return f
		}, openRCBooted, detect.SSHOpenRC, detect.Yes, detect.Yes, []string{"default"}, nil},
		{"in default, stopped", func(f *hostfake.System) host.System {
			script(f)
			runlevels(f)
			runlevel(f, "default")
			return f
		}, openRCBooted, detect.SSHOpenRC, detect.No, detect.Yes, []string{"default"}, nil},
		{"started by hand, in no runlevel", func(f *hostfake.System) host.System {
			script(f)
			runlevels(f)
			started(f)
			return f
		}, openRCBooted, detect.SSHOpenRC, detect.Yes, detect.No, nil, nil},
		{"a file among the runlevels holds nothing", func(f *hostfake.System) host.System {
			script(f)
			runlevels(f)
			f.AddFile(detect.OpenRCRunlevels+"/README", nil, 0o644, 0, 0)
			runlevel(f, "boot")
			return f
		}, openRCBooted, detect.SSHOpenRC, detect.No, detect.Yes, []string{"boot"}, nil},
		{"installed, neither started nor in a runlevel (another daemon serves)", func(f *hostfake.System) host.System {
			script(f)
			runlevels(f)
			return f
		}, openRCBooted, detect.SSHUnknown, detect.No, detect.No, nil, detect.ErrNoSignal},
		{"no runlevel directory", func(f *hostfake.System) host.System {
			script(f)
			return f
		}, openRCBooted, detect.SSHUnknown, detect.No, detect.No, nil, detect.ErrNoSignal},
		{"started; runlevels unreadable", func(f *hostfake.System) host.System {
			script(f)
			runlevels(f)
			started(f)
			return deny(f, detect.OpenRCRunlevels)
		}, openRCBooted, detect.SSHOpenRC, detect.Yes, detect.Unknown, nil, fs.ErrPermission},
		{"stopped; a runlevel unreadable", func(f *hostfake.System) host.System {
			script(f)
			runlevels(f)
			return deny(f, detect.OpenRCRunlevels+"/default/sshd")
		}, openRCBooted, detect.SSHUnknown, detect.No, detect.Unknown, nil, fs.ErrPermission},
		{"no sshd", func(f *hostfake.System) host.System { return f }, openRCBooted, detect.SSHUnknown, detect.Unknown, detect.Unknown, nil, detect.ErrNoSignal},
		{"init script unreadable", func(f *hostfake.System) host.System {
			script(f)
			return deny(f, detect.OpenRCSSHD)
		}, openRCBooted, detect.SSHUnknown, detect.Unknown, detect.Unknown, nil, fs.ErrPermission},
		{"OpenRC installed, not booted (container): nothing read", func(f *hostfake.System) host.System {
			script(f)
			runlevels(f)
			runlevel(f, "default")
			started(f)
			return f
		}, detect.Init{Kind: detect.OpenRC, Running: detect.No, Source: detect.OpenRCRun}, detect.SSHUnknown, detect.Unknown, detect.Unknown, nil, detect.ErrNoSignal},
	}
	for _, tt := range tests {
		f := newFake(t, "")
		got := detect.DetectSSH(ctx, tt.setup(f), tt.in)
		o := got.OpenRC
		if got.Mode != tt.mode || o.Started != tt.started || o.InRunlevel != tt.inRunlevel || !slices.Equal(o.Runlevels, tt.levels) || !matches(got.Err, tt.wantErr) {
			t.Errorf("%s: DetectSSH = %s, started %s, in runlevel %s %q, err %v; want %s, started %s, in runlevel %s %q, err %v",
				tt.name, got.Mode, o.Started, o.InRunlevel, o.Runlevels, got.Err, tt.mode, tt.started, tt.inRunlevel, tt.levels, tt.wantErr)
		}
		if len(f.Runs()) > 0 {
			t.Errorf("%s: ran %q on OpenRC", tt.name, f.Runs())
		}
		if tt.in.Running != detect.Yes && len(f.Calls()) > 0 {
			t.Errorf("%s: looked at the host (%v) without a booted OpenRC", tt.name, f.Calls())
		}
		readOnly(t, f)
	}
}

// The unit states map to facts as systemctl(1) documents them.
func TestUnitState(t *testing.T) {
	tests := []struct {
		state            detect.UnitState
		enabled, running detect.Tri
	}{
		{detect.UnitState{Enabled: "enabled", Active: "active"}, detect.Yes, detect.Yes},
		{detect.UnitState{Enabled: "enabled-runtime", Active: "reloading"}, detect.Yes, detect.Yes},
		{detect.UnitState{Enabled: "disabled", Active: "inactive"}, detect.No, detect.No},
		{detect.UnitState{Enabled: "masked", Active: "failed"}, detect.No, detect.No},
		{detect.UnitState{Enabled: "not-found", Active: "refreshing"}, detect.No, detect.Yes},
		{detect.UnitState{Enabled: "static", Active: "activating"}, detect.Unknown, detect.Unknown},
		{detect.UnitState{Enabled: "indirect", Active: "deactivating"}, detect.Unknown, detect.Unknown},
		{detect.UnitState{}, detect.Unknown, detect.Unknown},
		{detect.UnitState{Enabled: "Enabled", Active: "up"}, detect.Unknown, detect.Unknown},
	}
	for _, tt := range tests {
		if got := tt.state.IsEnabled(); got != tt.enabled {
			t.Errorf("%+v.IsEnabled() = %s, want %s", tt.state, got, tt.enabled)
		}
		if got := tt.state.IsActive(); got != tt.running {
			t.Errorf("%+v.IsActive() = %s, want %s", tt.state, got, tt.running)
		}
	}
}

// matches is errors.Is, with a nil want meaning no error.
func matches(err, want error) bool {
	if want == nil {
		return err == nil
	}
	return errors.Is(err, want)
}

// TestSystemctlIsActive: is-active only, the state printed whatever the
// exit status, and a state systemctl(1) does not list ErrUnrecognised
// (next/p4-check-updates).
func TestSystemctlIsActive(t *testing.T) {
	ctx := context.Background()
	for _, tt := range []struct {
		res   hostfake.Response
		state string
		err   error
	}{
		{out("active\n", 0), "active", nil},
		{out("inactive\n", 3), "inactive", nil},
		{out("failed\n", 3), "failed", nil},
		{out("activating\n", 3), "activating", nil},
		{out("", 1), "", detect.ErrUnrecognised},
		{out("enabled\n", 0), "", detect.ErrUnrecognised}, // is-enabled's word, not is-active's
		{hostfake.Response{Err: fmt.Errorf("fixture: %w", host.ErrNotFound)}, "", host.ErrNotFound},
	} {
		f := newFake(t, "")
		f.Respond(tt.res, "systemctl", "is-active", "apt-daily.timer")
		got, err := detect.SystemctlIsActive(ctx, f, "apt-daily.timer")
		if got != tt.state || !matches(err, tt.err) {
			t.Errorf("%q: SystemctlIsActive = %q, %v; want %q, %v", tt.res.Stdout, got, err, tt.state, tt.err)
		}
		if calls := f.Calls(); len(calls) != 1 || strings.Join(calls[0].Argv, " ") != "systemctl is-active apt-daily.timer" {
			t.Errorf("%q: calls %v", tt.res.Stdout, calls)
		}
	}
}
