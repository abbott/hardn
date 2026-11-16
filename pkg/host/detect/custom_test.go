package detect_test

import (
	"context"
	"errors"
	"io/fs"
	"testing"

	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/hostfake"
)

// dpkgRpi is the argv DetectCustom runs, last of Raspberry Pi OS's signals.
var dpkgRpi = []string{"dpkg-query", "--show", `--showformat=${Status}\t${Version}\n`, "raspberrypi-sys-mods"}

// noRpiPkg is dpkg-query's answer on a host without the Pi's package.
var noRpiPkg = hostfake.Response{Stderr: "dpkg-query: no packages found matching raspberrypi-sys-mods\n", ExitCode: 1}

// Each signal alone; none; and one that can't be read, alone and beside a
// signal that is there, before it or after it. Not in a container.
func TestDetectCustom(t *testing.T) {
	ctx := context.Background()
	const raspiList = "deb http://archive.raspberrypi.com/debian/ trixie main\n"
	tests := []struct {
		name    string
		files   map[string]string
		dpkg    *hostfake.Response // nil: not scripted, so it must not run
		deny    []string
		truenas detect.Tri
		raspios detect.Tri
		source  string
		variant detect.Variant
		ok      bool
		err     error
	}{
		{name: "TrueNAS: freenas-debug", files: map[string]string{detect.TrueNASDebug: ""},
			truenas: detect.Yes, raspios: detect.Unknown, source: detect.TrueNASDebug, variant: detect.VariantTrueNAS, ok: true},
		{name: "TrueNAS: disable-rootfs-protection", files: map[string]string{detect.TrueNASRootfs: ""},
			truenas: detect.Yes, raspios: detect.Unknown, source: detect.TrueNASRootfs, variant: detect.VariantTrueNAS, ok: true},
		{name: "TrueNAS: the kernel release", files: map[string]string{detect.KernelRelease: "6.12.15-production+truenas\n"},
			truenas: detect.Yes, raspios: detect.Unknown, source: detect.KernelRelease, variant: detect.VariantTrueNAS, ok: true},
		{name: "Raspberry Pi OS: rpi-issue", files: map[string]string{detect.RpiIssue: "Raspberry Pi reference 2025-11-24\n"},
			truenas: detect.No, raspios: detect.Yes, source: detect.RpiIssue, variant: detect.VariantRaspiOS, ok: true},
		{name: "Raspberry Pi OS: raspi.list", files: map[string]string{detect.RpiSourcesList: raspiList},
			truenas: detect.No, raspios: detect.Yes, source: detect.RpiSourcesList, variant: detect.VariantRaspiOS, ok: true},
		{name: "Raspberry Pi OS: raspi.sources", files: map[string]string{detect.RpiSources: "Types: deb\nURIs: http://archive.raspberrypi.com/debian/\nSuites: trixie\n"},
			truenas: detect.No, raspios: detect.Yes, source: detect.RpiSources, variant: detect.VariantRaspiOS, ok: true},
		{name: "Raspberry Pi OS: the package", dpkg: &hostfake.Response{Stdout: "install ok installed\t20251120\n"},
			truenas: detect.No, raspios: detect.Yes, source: "dpkg-query: raspberrypi-sys-mods installed", variant: detect.VariantRaspiOS, ok: true},
		{name: "none", files: map[string]string{detect.KernelRelease: "6.12.57+deb13-amd64\n"}, dpkg: &noRpiPkg,
			truenas: detect.No, raspios: detect.No, variant: detect.VariantNone, ok: true},
		{name: "the Pi's archive commented out", files: map[string]string{detect.RpiSourcesList: "#" + raspiList}, dpkg: &noRpiPkg,
			truenas: detect.No, raspios: detect.No, variant: detect.VariantNone, ok: true},
		{name: "a directory is no signal", files: map[string]string{detect.RpiIssue + "/": ""}, dpkg: &noRpiPkg,
			truenas: detect.No, raspios: detect.No, variant: detect.VariantNone, ok: true},
		{name: "TrueNAS unreadable: not established", deny: []string{detect.TrueNASRootfs},
			truenas: detect.Unknown, raspios: detect.Unknown, variant: detect.VariantNone, ok: false, err: fs.ErrPermission},
		{name: "TrueNAS unreadable, another signal there", files: map[string]string{detect.TrueNASDebug: ""}, deny: []string{detect.KernelRelease},
			truenas: detect.Yes, raspios: detect.Unknown, source: detect.TrueNASDebug, variant: detect.VariantTrueNAS, ok: true},
		{name: "TrueNAS unreadable, a later signal there", files: map[string]string{detect.TrueNASRootfs: ""}, deny: []string{detect.TrueNASDebug},
			truenas: detect.Yes, raspios: detect.Unknown, source: detect.TrueNASRootfs, variant: detect.VariantTrueNAS, ok: true},
		{name: "Raspberry Pi OS unreadable, a later signal there", files: map[string]string{detect.RpiSourcesList: raspiList}, deny: []string{detect.RpiIssue},
			truenas: detect.No, raspios: detect.Yes, source: detect.RpiSourcesList, variant: detect.VariantRaspiOS, ok: true},
		{name: "Raspberry Pi OS unreadable: plain Debian", deny: []string{detect.RpiIssue}, dpkg: &noRpiPkg,
			truenas: detect.No, raspios: detect.Unknown, variant: detect.VariantNone, ok: true, err: fs.ErrPermission},
		{name: "dpkg fails: plain Debian", dpkg: &hostfake.Response{Stderr: "dpkg-query: error: ...\n", ExitCode: 2},
			truenas: detect.No, raspios: detect.Unknown, variant: detect.VariantNone, ok: true, err: new(host.ExitError)},
	}
	for _, tt := range tests {
		f := hostfake.New(host.Options{})
		for name, data := range tt.files {
			if dir, ok := cutSlash(name); ok {
				f.AddDir(dir, 0o755, 0, 0)
				continue
			}
			f.AddFile(name, []byte(data), 0o644, 0, 0)
		}
		if tt.dpkg != nil {
			f.Respond(*tt.dpkg, dpkgRpi[0], dpkgRpi[1:]...)
		}
		got := detect.DetectCustom(ctx, deny(f, tt.deny...), detect.No)
		if got.TrueNAS != tt.truenas || got.RaspiOS != tt.raspios || got.Source != tt.source {
			t.Errorf("%s: DetectCustom = %+v, want TrueNAS %s, Raspberry Pi OS %s, source %q", tt.name, got, tt.truenas, tt.raspios, tt.source)
		}
		if v, ok := got.Variant(); v != tt.variant || ok != tt.ok {
			t.Errorf("%s: Variant = %q, %v; want %q, %v", tt.name, v, ok, tt.variant, tt.ok)
		}
		var xe *host.ExitError
		switch {
		case tt.err == nil && got.Err != nil:
			t.Errorf("%s: Err = %v, want none", tt.name, got.Err)
		case errors.As(tt.err, &xe):
			if !errors.As(got.Err, &xe) {
				t.Errorf("%s: Err = %v, want an exit error", tt.name, got.Err)
			}
		case tt.err != nil && !errors.Is(got.Err, tt.err):
			t.Errorf("%s: Err = %v, want %v", tt.name, got.Err, tt.err)
		}
		readOnly(t, f)
		if tt.dpkg == nil {
			for _, c := range f.Calls() {
				if c.Op == "run" {
					t.Errorf("%s: ran %v after a Yes", tt.name, c.Argv)
				}
			}
		}
	}
}

// A container shares the host's kernel: inside one, or where that is
// Unknown, a "+truenas" kernel release is not TrueNAS, and the files still
// are.
func TestDetectCustomContainer(t *testing.T) {
	ctx := context.Background()
	const kernel = "6.12.15-production+truenas\n"
	tests := []struct {
		name      string
		container detect.Tri
		debug     bool // freenas-debug there
		truenas   detect.Tri
		variant   detect.Variant
	}{
		{"a Debian container on TrueNAS", detect.Yes, false, detect.No, detect.VariantNone},
		{"container Unknown", detect.Unknown, false, detect.No, detect.VariantNone},
		{"container Unknown, freenas-debug there", detect.Unknown, true, detect.Yes, detect.VariantTrueNAS},
	}
	for _, tt := range tests {
		f := hostfake.New(host.Options{})
		f.AddFile(detect.KernelRelease, []byte(kernel), 0o444, 0, 0)
		if tt.debug {
			f.AddFile(detect.TrueNASDebug, nil, 0o755, 0, 0)
		} else {
			f.Respond(noRpiPkg, dpkgRpi[0], dpkgRpi[1:]...)
		}
		got := detect.DetectCustom(ctx, f, tt.container)
		if v, _ := got.Variant(); got.TrueNAS != tt.truenas || v != tt.variant {
			t.Errorf("%s: DetectCustom = %+v, variant %q; want TrueNAS %s, %q", tt.name, got, v, tt.truenas, tt.variant)
		}
		for _, c := range f.Calls() {
			if c.Path == detect.KernelRelease {
				t.Errorf("%s: read %s", tt.name, c.Path)
			}
		}
	}
}

// cutSlash is name without a trailing slash, the tests' mark of a
// directory.
func cutSlash(name string) (string, bool) {
	if n := len(name); n > 0 && name[n-1] == '/' {
		return name[:n-1], true
	}
	return "", false
}

// Detect reads the custom distributions on Debian alone, Proxmox VE
// excluded, and places them on the platform.
func TestDetectCustomPlatform(t *testing.T) {
	ctx := context.Background()
	truenas := release{osRelease: "debian-12", sudo: "Sudo version 1.9.13p3"}.host(t)
	truenas.AddFile(detect.TrueNASDebug, nil, 0o755, 0, 0)
	pi := release{osRelease: "debian-13", sudo: "Sudo version 1.9.16p2"}.host(t)
	pi.AddFile(detect.RpiIssue, []byte("Raspberry Pi reference\n"), 0o644, 0, 0)
	pve := release{osRelease: "debian-13", pve: "9.2.22"}.host(t)
	pve.AddFile(detect.RpiIssue, nil, 0o644, 0, 0)
	kernel := release{osRelease: "debian-12", sudo: "Sudo version 1.9.13p3"}.host(t)
	kernel.AddFile(detect.KernelRelease, []byte("6.12.15-production+truenas\n"), 0o444, 0, 0)
	guest := release{osRelease: "debian-13", sudo: "Sudo version 1.9.16p2"}.host(t)
	guest.AddFile(detect.KernelRelease, []byte("6.12.15-production+truenas\n"), 0o444, 0, 0)
	guest.AddFile("/.dockerenv", nil, 0o644, 0, 0)
	ubuntu := release{osRelease: "ubuntu-24.04", socket: true, sudo: "Sudo version 1.9.15p5"}.host(t)
	ubuntu.AddFile(detect.TrueNASDebug, nil, 0o755, 0, 0)
	tests := []struct {
		name    string
		f       *hostfake.System
		distro  detect.Distro
		variant detect.Variant
		read    bool
	}{
		{"TrueNAS", truenas, detect.DistroDebian, detect.VariantTrueNAS, true},
		{"Raspberry Pi OS", pi, detect.DistroDebian, detect.VariantRaspiOS, true},
		{"TrueNAS by its kernel", kernel, detect.DistroDebian, detect.VariantTrueNAS, true},
		{"a Debian container on TrueNAS", guest, detect.DistroDebian, detect.VariantNone, true},
		{"Proxmox VE: not read", pve, detect.DistroProxmox, detect.VariantNone, false},
		{"Ubuntu: not read", ubuntu, detect.DistroUbuntu, detect.VariantNone, false},
	}
	for _, tt := range tests {
		got, err := detect.Detect(ctx, tt.f)
		if err != nil {
			t.Fatal(err)
		}
		if got.Platform.Distro != tt.distro || got.Platform.Variant != tt.variant || got.Platform.Support != detect.Supported {
			t.Errorf("%s: Platform = %+v, want %s %q supported", tt.name, got.Platform, tt.distro, tt.variant)
		}
		if read := got.Custom != (detect.Custom{}); read != tt.read {
			t.Errorf("%s: Custom = %+v, read %v", tt.name, got.Custom, tt.read)
		}
		readOnly(t, tt.f)
	}
}
