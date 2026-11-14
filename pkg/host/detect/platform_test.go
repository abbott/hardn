package detect_test

import (
	"context"
	"errors"
	"testing"

	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/detect"
)

func TestClassify(t *testing.T) {
	ctx := context.Background()
	read := func(name string) detect.OSRelease { return detect.ReadOSRelease(ctx, newFake(t, name)) }
	notPVE := detect.Proxmox{Installed: detect.No}
	pve := func(v string) detect.Proxmox { return detect.Proxmox{Installed: detect.Yes, Version: v} }
	errFixture := errors.New("fixture: read failed")
	errExit := &host.ExitError{Argv: []string{"dpkg-query"}, ExitCode: 2}

	tests := []struct {
		name    string
		osr     detect.OSRelease
		pve     detect.Proxmox
		distro  detect.Distro
		version string
		support detect.Support
		wantErr error
	}{
		{"Debian 13", read("debian-13"), notPVE, detect.DistroDebian, "13", detect.Supported, nil},
		{"Debian 12", read("debian-12"), notPVE, detect.DistroDebian, "12", detect.Supported, nil},
		{"Ubuntu 26.04", read("ubuntu-26.04"), notPVE, detect.DistroUbuntu, "26.04", detect.Supported, nil},
		{"Ubuntu 24.04", read("ubuntu-24.04"), notPVE, detect.DistroUbuntu, "24.04", detect.Supported, nil},
		{"Alpine 3.24", read("alpine-3.24"), notPVE, detect.DistroAlpine, "3.24", detect.Supported, nil},
		{"Alpine 3.23", read("alpine-3.23"), notPVE, detect.DistroAlpine, "3.23", detect.Supported, nil},
		{"Proxmox VE 9", read("debian-13"), pve("9.2.22"), detect.DistroProxmox, "9", detect.Supported, nil},
		{"Ubuntu 22.04", read("ubuntu-22.04"), notPVE, detect.DistroUbuntu, "22.04", detect.BestEffort, nil},
		{"Alpine 3.22", read("alpine-3.22"), notPVE, detect.DistroAlpine, "3.22", detect.BestEffort, nil},
		{"Alpine 3.21", read("alpine-3.21"), notPVE, detect.DistroAlpine, "3.21", detect.OutOfScope, nil},
		{"Alpine 3.20", read("alpine-3.20"), notPVE, detect.DistroAlpine, "3.20", detect.OutOfScope, nil},
		{"Ubuntu interim 25.10", read("ubuntu-25.10"), notPVE, detect.DistroUbuntu, "25.10", detect.OutOfScope, nil},
		{"Ubuntu interim 27.04", detect.OSRelease{ID: "ubuntu", VersionID: "27.04"}, notPVE, detect.DistroUbuntu, "27.04", detect.OutOfScope, nil},
		{"Proxmox VE 8", read("debian-12"), pve("8.4.22"), detect.DistroProxmox, "8", detect.OutOfScope, nil},
		{"Ubuntu 20.04 LTS", detect.OSRelease{ID: "ubuntu", VersionID: "20.04"}, notPVE, detect.DistroUbuntu, "20.04", detect.Unlisted, nil},
		{"Debian 11", detect.OSRelease{ID: "debian", VersionID: "11"}, notPVE, detect.DistroDebian, "11", detect.Unlisted, nil},
		{"Debian sid has no VERSION_ID", read("debian-sid"), notPVE, detect.DistroDebian, "", detect.Unlisted, nil},
		{"Alpine edge", detect.OSRelease{ID: "alpine", VersionID: "3.25.0_alpha20261001"}, notPVE, detect.DistroAlpine, "3.25", detect.Unlisted, nil},
		{"Alpine release candidate of a supported row", detect.OSRelease{ID: "alpine", VersionID: "3.24.0_rc1"}, notPVE, detect.DistroAlpine, "3.24", detect.Unlisted, nil},
		{"Alpine pre-release of an out-of-scope row", detect.OSRelease{ID: "alpine", VersionID: "3.21.0_alpha20241001"}, notPVE, detect.DistroAlpine, "3.21", detect.Unlisted, nil},
		{"Alpine two-part VERSION_ID", detect.OSRelease{ID: "alpine", VersionID: "3.24"}, notPVE, detect.DistroAlpine, "3.24", detect.Unlisted, nil},
		{"Alpine VERSION_ID missing", detect.OSRelease{ID: "alpine"}, notPVE, detect.DistroAlpine, "", detect.Unlisted, nil},
		{"Alpine VERSION_ID not numeric", detect.OSRelease{ID: "alpine", VersionID: "edge"}, notPVE, detect.DistroAlpine, "", detect.Unlisted, nil},
		{"Ubuntu VERSION_ID not YY.MM", detect.OSRelease{ID: "ubuntu", VersionID: "24.04.1"}, notPVE, detect.DistroUbuntu, "24.04.1", detect.Unlisted, nil},
		{"Proxmox on Ubuntu not established is still Ubuntu", read("ubuntu-24.04"), detect.Proxmox{}, detect.DistroUbuntu, "24.04", detect.Supported, nil},
		// Rocky Linux and AlmaLinux: 10 and 9 (el-changes.md).
		{"Rocky Linux 10.2 (rocky10-test-110)", read("rocky-10.2"), notPVE, detect.DistroRocky, "10", detect.Supported, nil},
		{"Rocky Linux 9.8", detect.OSRelease{ID: "rocky", IDLike: []string{"rhel", "centos", "fedora"}, VersionID: "9.8"}, notPVE, detect.DistroRocky, "9", detect.Supported, nil},
		{"AlmaLinux 10.1", detect.OSRelease{ID: "almalinux", IDLike: []string{"rhel", "centos", "fedora"}, VersionID: "10.1"}, notPVE, detect.DistroAlma, "10", detect.Supported, nil},
		{"AlmaLinux 9.7", detect.OSRelease{ID: "almalinux", VersionID: "9.7"}, notPVE, detect.DistroAlma, "9", detect.Supported, nil},
		{"Rocky Linux 8.10", detect.OSRelease{ID: "rocky", VersionID: "8.10"}, notPVE, detect.DistroRocky, "8", detect.Unlisted, nil},
		{"Rocky Linux with Proxmox not established", read("rocky-10.2"), detect.Proxmox{Err: errFixture}, detect.DistroRocky, "10", detect.Supported, nil},
		{"Rocky VERSION_ID with no minor", detect.OSRelease{ID: "rocky", VersionID: "10"}, notPVE, detect.DistroRocky, "", detect.Unlisted, nil},
		{"AlmaLinux VERSION_ID of three parts", detect.OSRelease{ID: "almalinux", VersionID: "10.1.1"}, notPVE, detect.DistroAlma, "", detect.Unlisted, nil},
		{"AlmaLinux VERSION_ID not numeric", detect.OSRelease{ID: "almalinux", VersionID: "10.x"}, notPVE, detect.DistroAlma, "", detect.Unlisted, nil},
		// RHEL: EL, 10 and 9 (el-rhel.md).
		{"RHEL 10.3 (rhel10-test-114)", read("rhel-10.3"), notPVE, detect.DistroRHEL, "10", detect.Supported, nil},
		{"RHEL 9.9 (rhel9-test-115)", read("rhel-9.9"), notPVE, detect.DistroRHEL, "9", detect.Supported, nil},
		{"RHEL 8.10", detect.OSRelease{ID: "rhel", IDLike: []string{"fedora"}, VersionID: "8.10"}, notPVE, detect.DistroRHEL, "8", detect.Unlisted, nil},
		{"RHEL VERSION_ID with no minor", detect.OSRelease{ID: "rhel", VersionID: "10"}, notPVE, detect.DistroRHEL, "", detect.Unlisted, nil},
		// CentOS Stream and Oracle Linux: EL, no row yet (design-centos-oracle.md
		// §6). Their fixtures are written as best known, NOT read from a host.
		{"CentOS Stream 10 (fixture not from a host)", read("centos-stream-10"), notPVE, detect.DistroCentOS, "10", detect.Unlisted, nil},
		{"CentOS Stream 9 (fixture not from a host)", read("centos-stream-9"), notPVE, detect.DistroCentOS, "9", detect.Unlisted, nil},
		{"Oracle Linux 10.1 (fixture not from a host)", read("oracle-10.1"), notPVE, detect.DistroOracle, "10", detect.Unlisted, nil},
		{"Oracle Linux 9.6 (fixture not from a host)", read("oracle-9.6"), notPVE, detect.DistroOracle, "9", detect.Unlisted, nil},
		{"CentOS Linux 8", detect.OSRelease{ID: "centos", IDLike: []string{"rhel", "fedora"}, VersionID: "8"}, notPVE, detect.DistroCentOS, "8", detect.Unlisted, nil},
		{"CentOS VERSION_ID with a minor", detect.OSRelease{ID: "centos", VersionID: "10.1"}, notPVE, detect.DistroCentOS, "10", detect.Unlisted, nil},
		{"CentOS VERSION_ID missing", detect.OSRelease{ID: "centos"}, notPVE, detect.DistroCentOS, "", detect.Unlisted, nil},
		{"CentOS VERSION_ID not numeric", detect.OSRelease{ID: "centos", VersionID: "stream"}, notPVE, detect.DistroCentOS, "", detect.Unlisted, nil},
		{"Oracle Linux VERSION_ID with no minor", detect.OSRelease{ID: "ol", VersionID: "10"}, notPVE, detect.DistroOracle, "", detect.Unlisted, nil},
		{"Oracle Linux with Proxmox not established", read("oracle-10.1"), detect.Proxmox{Err: errFixture}, detect.DistroOracle, "10", detect.Unlisted, nil},
		{"Fedora is not EL", detect.OSRelease{ID: "fedora", VersionID: "43"}, notPVE, detect.DistroOther, "", detect.Unlisted, nil},

		{"os-release unreadable", detect.OSRelease{Err: errFixture}, notPVE, detect.DistroUnknown, "", detect.SupportUnknown, errFixture},
		{"empty OSRelease", detect.OSRelease{}, notPVE, detect.DistroUnknown, "", detect.SupportUnknown, detect.ErrNoSignal},
		{"Debian with Proxmox not established", read("debian-13"), detect.Proxmox{Err: errFixture}, detect.DistroUnknown, "", detect.SupportUnknown, errFixture},
		{"Proxmox VE version not established", read("debian-13"), pve(""), detect.DistroProxmox, "", detect.SupportUnknown, detect.ErrNoSignal},
		{"Proxmox VE version: dpkg failed", read("debian-13"), detect.Proxmox{Installed: detect.Yes, Err: errExit}, detect.DistroProxmox, "", detect.SupportUnknown, errExit},
		{"Proxmox VE version unparsable", read("debian-13"), pve("nine"), detect.DistroProxmox, "", detect.SupportUnknown, detect.ErrUnrecognised},
	}
	for _, tt := range tests {
		got := detect.Classify(tt.osr, tt.pve)
		if got.Distro != tt.distro || got.Version != tt.version || got.Support != tt.support {
			t.Errorf("%s: Classify = %s %q %s, want %s %q %s", tt.name, got.Distro, got.Version, got.Support, tt.distro, tt.version, tt.support)
		}
		if (tt.wantErr == nil) != (got.Err == nil) || (tt.wantErr != nil && !errors.Is(got.Err, tt.wantErr)) {
			t.Errorf("%s: Classify err = %v, want %v", tt.name, got.Err, tt.wantErr)
		}
	}
}

// EL is Rocky Linux, AlmaLinux, RHEL, CentOS Stream and Oracle Linux alone,
// and each has a name for the refusals to say.
func TestEL(t *testing.T) {
	names := map[detect.Distro]string{detect.DistroRocky: "Rocky Linux", detect.DistroAlma: "AlmaLinux", detect.DistroRHEL: "RHEL",
		detect.DistroCentOS: "CentOS Stream", detect.DistroOracle: "Oracle Linux"}
	for _, d := range []detect.Distro{detect.DistroUnknown, detect.DistroDebian, detect.DistroUbuntu, detect.DistroAlpine,
		detect.DistroProxmox, detect.DistroRocky, detect.DistroAlma, detect.DistroRHEL, detect.DistroCentOS, detect.DistroOracle,
		detect.DistroOther, "centos", "ol", "fedora"} {
		name, want := names[d]
		if d.EL() != want {
			t.Errorf("%q.EL() = %v, want %v", d, d.EL(), want)
		}
		if want && d.Name() != name {
			t.Errorf("%q.Name() = %q, want %q", d, d.Name(), name)
		}
	}
}

// A derivative names what it is built from, and is not in the matrix.
func TestClassifyDerivative(t *testing.T) {
	osr := detect.OSRelease{ID: "examplelinux", IDLike: []string{"examplebase", "ubuntu", "debian"}, VersionID: "7"}
	got := detect.Classify(osr, detect.Proxmox{Installed: detect.No})
	if got.Distro != detect.DistroOther || got.Like != detect.DistroUbuntu || got.Support != detect.Unlisted || got.Err != nil {
		t.Errorf("Classify(derivative) = %+v, want other, like ubuntu, unlisted", got)
	}
}

// The matrix holds brief §2's rows, and Matrix hands out a copy.
func TestMatrix(t *testing.T) {
	m := detect.Matrix()
	count := map[detect.Support]int{}
	for _, r := range m {
		count[r.Support]++
	}
	if count[detect.Supported] != 13 || count[detect.BestEffort] != 2 || count[detect.OutOfScope] != 3 {
		t.Errorf("matrix rows by support = %v, want 13 supported, 2 best effort, 3 out of scope", count)
	}
	m[0].Support = detect.Unlisted
	if detect.Matrix()[0].Support == detect.Unlisted {
		t.Error("Matrix returned the package's own slice")
	}
}
