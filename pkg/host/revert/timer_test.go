package revert

import (
	"context"
	"errors"
	"io/fs"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/hostfake"
	"github.com/abbott/hardn/pkg/host/selinux"
)

func TestTimerFor(t *testing.T) {
	tests := []struct {
		name    string
		in      detect.Init
		id      string
		hardn   string
		timeout time.Duration
		want    Timer
		wantErr error
	}{
		{"systemd", systemd, testID, testHardn, 180 * time.Second,
			Timer{Kind: TimerSystemd, Unit: "hardn-revert-" + testID, Argv: systemdRunArgv(testID)}, nil},
		{"openrc", openrc, testID, testHardn, 240 * time.Second,
			Timer{Kind: TimerSetsid, PIDFile: "/run/hardn/revert-" + testID + ".pid",
				Argv: []string{"/bin/sh", "-c", `sleep "$1" && "$0" rollback "$2"`, testHardn, "240", testID}}, nil},
		{"openrc installed, not running (a container)", detect.Init{Kind: detect.OpenRC, Running: detect.No}, testID, testHardn, 180 * time.Second, Timer{}, ErrNoTimer},
		{"init unknown", detect.Init{Err: detect.ErrConflict}, testID, testHardn, 180 * time.Second, Timer{}, ErrNoTimer},
		{"a bad id", systemd, "x;reboot", testHardn, 180 * time.Second, Timer{}, ErrID},
		{"hardn by name", systemd, testID, "hardn", 180 * time.Second, Timer{}, ErrExecutable},
		{"hardn by an unclean path", systemd, testID, "/usr/local/bin/../bin/hardn", 180 * time.Second, Timer{}, ErrExecutable},
		{"below the floor", systemd, testID, testHardn, 30 * time.Second, Timer{}, ErrTimeout},
		{"not whole seconds", systemd, testID, testHardn, 90*time.Second + 1, Timer{}, ErrTimeout},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := TimerFor(tt.in, tt.id, tt.hardn, tt.timeout, DefaultRunDir)
			if !errors.Is(err, tt.wantErr) || (tt.wantErr == nil) != (err == nil) || !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("TimerFor = %+v, %v\nwant %+v, %v", got, err, tt.want, tt.wantErr)
			}
		})
	}
}

func TestCheckExecutable(t *testing.T) {
	tests := []struct {
		name  string
		setup func(f *hostfake.System)
		path  string
		ok    bool
	}{
		{"root's, 0755, in root's directories", nil, testHardn, true},
		{"relative", nil, "usr/local/bin/hardn", false},
		{"missing", nil, "/usr/local/bin/hardn2", false},
		{"a symlink: resolve it first", func(f *hostfake.System) { f.AddSymlink("/usr/bin/hardn", testHardn) }, "/usr/bin/hardn", false},
		{"not executable", func(f *hostfake.System) { f.AddFile(testHardn, nil, 0o644, 0, 0) }, testHardn, false},
		{"group-writable", func(f *hostfake.System) { f.AddFile(testHardn, nil, 0o775, 0, 0) }, testHardn, false},
		{"alice's", func(f *hostfake.System) { f.AddFile(testHardn, nil, 0o755, 1000, 1000) }, testHardn, false},
		{"in alice's directory", func(f *hostfake.System) {
			f.AddDir("/home/alice/bin", 0o755, 1000, 1000)
			f.AddFile("/home/alice/bin/hardn", nil, 0o755, 0, 0)
		}, "/home/alice/bin/hardn", false},
		{"in a world-writable directory", func(f *hostfake.System) {
			f.AddDir("/opt/drop", 0o777, 0, 0)
			f.AddFile("/opt/drop/hardn", nil, 0o755, 0, 0)
		}, "/opt/drop/hardn", false},
		{"a directory on the way is a symlink", func(f *hostfake.System) {
			f.AddSymlink("/opt/hardn", "/usr/local/bin")
		}, "/opt/hardn/hardn", false},
		// Where SELinux enforces, systemd runs only a binary of the policy's
		// label (rocky10-test-110: exit 203 for user_tmp_t, admin_home_t).
		{"SELinux enforcing, bin_t", func(f *hostfake.System) {
			f.AddFile(selinux.EnforceFile, []byte("1"), 0o644, 0, 0)
			f.Respond(hostfake.Response{Stdout: "system_u:object_r:bin_t:s0\n"}, "stat", "-c", "%C", "--", testHardn)
		}, testHardn, true},
		{"SELinux enforcing, moved in from /tmp", func(f *hostfake.System) {
			f.AddFile(selinux.EnforceFile, []byte("1"), 0o644, 0, 0)
			f.Respond(hostfake.Response{Stdout: "unconfined_u:object_r:user_tmp_t:s0\n"}, "stat", "-c", "%C", "--", testHardn)
		}, testHardn, false},
		{"SELinux enforcing, in /root, as the policy labels it", func(f *hostfake.System) {
			f.AddFile(selinux.EnforceFile, []byte("1"), 0o644, 0, 0)
			f.AddFile("/root/hardn", nil, 0o755, 0, 0)
			f.Respond(hostfake.Response{Stdout: "unconfined_u:object_r:admin_home_t:s0\n"}, "stat", "-c", "%C", "--", "/root/hardn")
		}, "/root/hardn", false},
		{"SELinux permissive: not asked", func(f *hostfake.System) { f.AddFile(selinux.EnforceFile, []byte("0"), 0o644, 0, 0) }, testHardn, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newHost(t)
			if tt.setup != nil {
				tt.setup(f)
			}
			err := CheckExecutable(context.Background(), f, tt.path)
			if strings.HasPrefix(tt.name, "SELinux") && !tt.ok && !errors.Is(err, selinux.ErrNotRunnable) {
				t.Errorf("err %v, want ErrNotRunnable", err)
			}
			if tt.ok != (err == nil) || err != nil && !errors.Is(err, ErrExecutable) && !errors.Is(err, fs.ErrNotExist) {
				t.Fatalf("CheckExecutable(%s) = %v; want ok %v", tt.path, err, tt.ok)
			}
		})
	}
}
