package detect_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/hostfake"
)

// release is a supported release as a fake host: its os-release, init
// system, SSH units, Proxmox VE and elevation tools, from the facts in
// docs/safety/detect.md §3.
type release struct {
	osRelease string
	openrc    bool
	socket    bool   // ssh.socket enabled (Ubuntu 24.04+), else ssh.service
	pve       string // pve-manager version; "" when not installed
	sudo      string // sudo --version's first line; "" when sudo is absent
	doas      bool
}

func (r release) host(t *testing.T) *hostfake.System {
	t.Helper()
	f := bareHost(t)
	mounts := hostMounts
	switch {
	case r.openrc:
		f.AddDir(detect.OpenRCRunDir, 0o755, 0, 0)
		f.AddFile(detect.OpenRCRun, nil, 0o755, 0, 0)
		f.AddFile(detect.OpenRCSSHD, nil, 0o755, 0, 0)
		f.Respond(hostfake.Response{Err: errNotFound}, dpkgQuery[0], dpkgQuery[1:]...)
	case r.socket:
		f.AddDir(detect.SystemdRunDir, 0o755, 0, 0)
		f.AddFile(detect.SocketGenerator, nil, 0o755, 0, 0)
		systemctlStates(f, "enabled", "active", "disabled:1", "inactive:3")
	default:
		f.AddDir(detect.SystemdRunDir, 0o755, 0, 0)
		systemctlStates(f, "disabled:1", "inactive:3", "enabled", "active")
	}
	switch {
	case r.pve != "":
		f.Respond(out("install ok installed\t"+r.pve+"\n", 0), dpkgQuery[0], dpkgQuery[1:]...)
		f.AddFile(detect.PVEVersionBin, nil, 0o755, 0, 0)
		f.AddDir(detect.PVEDir, 0o755, 0, 0)
		mounts = pveMounts
	case !r.openrc:
		f.Respond(hostfake.Response{Stderr: "dpkg-query: no packages found matching pve-manager\n", ExitCode: 1}, dpkgQuery[0], dpkgQuery[1:]...)
	}
	f.AddFile(detect.SelfMounts, []byte(mounts), 0o444, 0, 0)
	if r.sudo != "" {
		f.AddFile("/usr/bin/sudo", nil, 0o755, 0, 0)
		f.Respond(out(r.sudo+"\n", 0), "sudo", "--version")
	}
	if r.doas {
		f.AddFile("/usr/bin/doas", nil, 0o755, 0, 0)
	}
	f.AddFile("/usr/lib/os-release", osRelease(t, r.osRelease), 0o644, 0, 0)
	f.AddSymlink("/etc/os-release", "../usr/lib/os-release")
	return f
}

var errNotFound = errors.Join(errors.New("fixture: dpkg-query"), host.ErrNotFound)

// Each supported release: every fact established, none of them guessed.
func TestDetectSupportedReleases(t *testing.T) {
	tests := []struct {
		name    string
		release release
		distro  detect.Distro
		version string
		init    detect.InitKind
		ssh     detect.SSHMode
		pve     detect.Tri
		flavor  detect.SudoFlavor
		doas    detect.Tri
	}{
		{"Debian 13", release{osRelease: "debian-13", sudo: "Sudo version 1.9.16p2"}, detect.DistroDebian, "13", detect.Systemd, detect.SSHService, detect.No, detect.SudoOriginal, detect.No},
		{"Debian 12", release{osRelease: "debian-12", sudo: "Sudo version 1.9.13p3"}, detect.DistroDebian, "12", detect.Systemd, detect.SSHService, detect.No, detect.SudoOriginal, detect.No},
		{"Ubuntu 26.04", release{osRelease: "ubuntu-26.04", socket: true, sudo: "sudo-rs 0.2.13"}, detect.DistroUbuntu, "26.04", detect.Systemd, detect.SSHSocket, detect.No, detect.SudoRS, detect.No},
		{"Ubuntu 24.04", release{osRelease: "ubuntu-24.04", socket: true, sudo: "Sudo version 1.9.15p5"}, detect.DistroUbuntu, "24.04", detect.Systemd, detect.SSHSocket, detect.No, detect.SudoOriginal, detect.No},
		{"Alpine 3.24", release{osRelease: "alpine-3.24", openrc: true, doas: true}, detect.DistroAlpine, "3.24", detect.OpenRC, detect.SSHOpenRC, detect.No, detect.SudoFlavorUnknown, detect.Yes},
		{"Alpine 3.23", release{osRelease: "alpine-3.23", openrc: true, doas: true}, detect.DistroAlpine, "3.23", detect.OpenRC, detect.SSHOpenRC, detect.No, detect.SudoFlavorUnknown, detect.Yes},
		{"Proxmox VE 9", release{osRelease: "debian-13", pve: "9.2.22"}, detect.DistroProxmox, "9", detect.Systemd, detect.SSHService, detect.Yes, detect.SudoFlavorUnknown, detect.No},
	}
	for _, tt := range tests {
		f := tt.release.host(t)
		got, err := detect.Detect(context.Background(), f)
		if err != nil {
			t.Fatalf("%s: Detect: %v", tt.name, err)
		}
		p := got.Platform
		if p.Distro != tt.distro || p.Version != tt.version || p.Support != detect.Supported || p.Err != nil {
			t.Errorf("%s: Platform = %+v, want %s %s supported", tt.name, p, tt.distro, tt.version)
		}
		if got.Init.Kind != tt.init || got.Init.Running != detect.Yes || got.SSH.Mode != tt.ssh || got.SSH.Err != nil {
			t.Errorf("%s: Init %+v, SSH %+v; want %s running, %s", tt.name, got.Init, got.SSH, tt.init, tt.ssh)
		}
		if got.Proxmox.Installed != tt.pve || got.Proxmox.ClusterFS != tt.pve || got.Proxmox.Err != nil {
			t.Errorf("%s: Proxmox = %+v, want installed and pmxcfs %s", tt.name, got.Proxmox, tt.pve)
		}
		if got.Container.Detected != detect.No || got.Container.Err != nil {
			t.Errorf("%s: Container = %+v, want no", tt.name, got.Container)
		}
		e := got.Elevation
		if e.SudoFlavor != tt.flavor || e.Doas.Present != tt.doas || e.Sudo.Err != nil || e.Doas.Err != nil || e.Err != nil {
			t.Errorf("%s: Elevation = %+v, want sudo %s, doas %s", tt.name, e, tt.flavor, tt.doas)
		}
		readOnly(t, f)
	}
}

// A done context fails Detect, since any fact may be Unknown for that alone.
func TestDetectCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	f := release{osRelease: "debian-13"}.host(t)
	got, err := detect.Detect(ctx, f)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Detect(cancelled) err = %v, want context.Canceled", err)
	}
	if got.OS.Known() || got.Platform.Support != detect.SupportUnknown || got.Init.Kind != detect.InitUnknown || got.Container.Detected != detect.Unknown {
		t.Errorf("Detect(cancelled) = %+v, want every fact unknown", got)
	}
}

// A host where nothing can be read establishes nothing and claims nothing.
func TestDetectNothing(t *testing.T) {
	f := hostfake.New(host.Options{})
	got, err := detect.Detect(context.Background(), f)
	if err != nil {
		t.Fatal(err)
	}
	if got.Platform.Support != detect.SupportUnknown || got.Init.Kind != detect.InitUnknown || got.SSH.Mode != detect.SSHUnknown ||
		got.Proxmox.Installed != detect.Unknown || got.Container.Detected != detect.Unknown {
		t.Errorf("Detect(empty host) = %+v, want unknown facts", got)
	}
	if !errors.Is(got.Proxmox.Err, hostfake.ErrUnscripted) {
		t.Errorf("Proxmox.Err = %v, want the unscripted dpkg-query", got.Proxmox.Err)
	}
	readOnly(t, f)
}

// Each fact enum's words, which Source strings and errors carry; a value
// out of range is "unknown", never another fact.
func TestStrings(t *testing.T) {
	tests := []struct {
		got  fmt.Stringer
		want string
	}{
		{detect.Unknown, "unknown"}, {detect.No, "no"}, {detect.Yes, "yes"}, {detect.Tri(9), "unknown"},
		{detect.InitUnknown, "unknown"}, {detect.Systemd, "systemd"}, {detect.OpenRC, "openrc"}, {detect.InitKind(9), "unknown"},
		{detect.SSHUnknown, "unknown"}, {detect.SSHSocket, "ssh.socket"}, {detect.SSHService, "ssh.service"}, {detect.SSHOpenRC, "openrc sshd"}, {detect.SSHMode(9), "unknown"},
		{detect.SupportUnknown, "unknown"}, {detect.Supported, "supported"}, {detect.BestEffort, "best effort"}, {detect.OutOfScope, "out of scope"}, {detect.Unlisted, "unlisted"}, {detect.Support(9), "unknown"},
		{detect.SudoFlavorUnknown, "unknown"}, {detect.SudoOriginal, "sudo"}, {detect.SudoRS, "sudo-rs"}, {detect.SudoFlavor(9), "unknown"},
	}
	for _, tt := range tests {
		if s := tt.got.String(); s != tt.want {
			t.Errorf("%T(%v).String() = %q, want %q", tt.got, tt.got, s, tt.want)
		}
	}
}
