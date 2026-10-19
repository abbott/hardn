package detect_test

import (
	"context"
	"io/fs"
	"testing"

	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/hostfake"
)

func TestDetectElevation(t *testing.T) {
	ctx := context.Background()
	sudoVersion := []string{"sudo", "--version"}
	tests := []struct {
		name     string
		setup    func(f *hostfake.System) host.System
		sudo     detect.Tri
		sudoPath string
		flavor   detect.SudoFlavor
		version  string
		doas     detect.Tri
		wantErr  error // Elevation.Err
	}{
		{"Debian: sudo", func(f *hostfake.System) host.System {
			f.AddFile("/usr/bin/sudo", nil, 0o755, 0, 0)
			f.Respond(out("Sudo version 1.9.16p2\nSudoers policy plugin version 1.9.16p2\n", 0), sudoVersion[0], sudoVersion[1:]...)
			return f
		}, detect.Yes, "/usr/bin/sudo", detect.SudoOriginal, "1.9.16p2", detect.No, nil},
		{"Ubuntu 26.04: sudo-rs through alternatives", func(f *hostfake.System) host.System {
			f.AddFile("/usr/lib/cargo/bin/sudo", nil, 0o755, 0, 0)
			f.AddSymlink("/etc/alternatives/sudo", "/usr/lib/cargo/bin/sudo")
			f.AddSymlink("/usr/bin/sudo", "/etc/alternatives/sudo")
			f.Respond(out("sudo-rs 0.2.13\n", 0), sudoVersion[0], sudoVersion[1:]...)
			return f
		}, detect.Yes, "/usr/bin/sudo", detect.SudoRS, "0.2.13", detect.No, nil},
		{"Alpine: doas only", func(f *hostfake.System) host.System {
			f.AddFile("/usr/bin/doas", nil, 0o755, 0, 0)
			return f
		}, detect.No, "", detect.SudoFlavorUnknown, "", detect.Yes, nil},
		{"Alpine with sudo from community", func(f *hostfake.System) host.System {
			f.AddFile("/usr/bin/doas", nil, 0o755, 0, 0)
			f.AddFile("/usr/bin/sudo", nil, 0o755, 0, 0)
			f.Respond(out("Sudo version 1.9.17p2\n", 0), sudoVersion[0], sudoVersion[1:]...)
			return f
		}, detect.Yes, "/usr/bin/sudo", detect.SudoOriginal, "1.9.17p2", detect.Yes, nil},
		{"Proxmox VE: neither", func(f *hostfake.System) host.System { return f }, detect.No, "", detect.SudoFlavorUnknown, "", detect.No, nil},
		{"not executable is not sudo", func(f *hostfake.System) host.System {
			f.AddFile("/usr/sbin/sudo", nil, 0o644, 0, 0)
			return f
		}, detect.No, "", detect.SudoFlavorUnknown, "", detect.No, nil},
		{"first in SearchPath", func(f *hostfake.System) host.System {
			f.AddFile("/usr/sbin/sudo", nil, 0o755, 0, 0)
			f.AddFile("/usr/bin/sudo", nil, 0o755, 0, 0)
			f.Respond(out("sudo-rs 0.2.13\n", 0), sudoVersion[0], sudoVersion[1:]...)
			return f
		}, detect.Yes, "/usr/sbin/sudo", detect.SudoRS, "0.2.13", detect.No, nil},
		{"sudo --version fails", func(f *hostfake.System) host.System {
			f.AddFile("/usr/bin/sudo", nil, 0o755, 0, 0)
			f.Respond(hostfake.Response{Stderr: "sudo: /usr/bin/sudo must be owned by uid 0 and have the setuid bit set\n", ExitCode: 1}, sudoVersion[0], sudoVersion[1:]...)
			return f
		}, detect.Yes, "/usr/bin/sudo", detect.SudoFlavorUnknown, "", detect.No, new(host.ExitError)},
		{"unrecognised version line", func(f *hostfake.System) host.System {
			f.AddFile("/usr/bin/sudo", nil, 0o755, 0, 0)
			f.Respond(out("sudo 2.0\n", 0), sudoVersion[0], sudoVersion[1:]...)
			return f
		}, detect.Yes, "/usr/bin/sudo", detect.SudoFlavorUnknown, "", detect.No, detect.ErrUnrecognised},
	}
	for _, tt := range tests {
		f := newFake(t, "")
		got := detect.DetectElevation(ctx, tt.setup(f))
		if got.Sudo.Present != tt.sudo || got.Sudo.Path != tt.sudoPath || got.SudoFlavor != tt.flavor || got.SudoVersion != tt.version || got.Doas.Present != tt.doas || !matchesErr(got.Err, tt.wantErr) {
			t.Errorf("%s: DetectElevation = %+v; want sudo %s %q %s %q, doas %s, err %v", tt.name, got, tt.sudo, tt.sudoPath, tt.flavor, tt.version, tt.doas, tt.wantErr)
		}
		readOnly(t, f)
	}

	t.Run("unreadable directory", func(t *testing.T) {
		f := newFake(t, "")
		got := detect.DetectElevation(ctx, deny(f, "/usr/sbin/sudo"))
		if got.Sudo.Present != detect.Unknown || !matches(got.Sudo.Err, fs.ErrPermission) || got.Doas.Present != detect.No {
			t.Errorf("DetectElevation = %+v; want sudo unknown (permission), doas no", got)
		}
	})
}
