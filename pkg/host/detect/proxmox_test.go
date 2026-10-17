package detect_test

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"reflect"
	"testing"

	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/hostfake"
)

// dpkgQuery is the argv DetectProxmox runs.
var dpkgQuery = []string{"dpkg-query", "--show", `--showformat=${Status}\t${Version}\n`, "pve-manager"}

// mounts is a /proc/self/mounts; pmxcfs's type string is a FUSE type with
// an unverified subtype.
const (
	hostMounts = "/dev/vda1 / ext4 rw,relatime 0 0\nproc /proc proc rw,nosuid,nodev,noexec,relatime 0 0\n"
	pveMounts  = hostMounts + "/dev/fuse /etc/pve fuse rw,nosuid,nodev,relatime,user_id=0,group_id=0,default_permissions,allow_other 0 0\n"
)

func TestDetectProxmox(t *testing.T) {
	ctx := context.Background()
	errNoDpkg := fmt.Errorf("fixture: %w", host.ErrNotFound)
	type want struct {
		installed detect.Tri
		version   string
		clusterFS detect.Tri
		etcPVE    detect.Tri
		err       error
	}
	tests := []struct {
		name   string
		dpkg   hostfake.Response
		pvebin bool
		mounts string
		etcPVE bool
		want   want
	}{
		{"PVE 9, pmxcfs up", out("install ok installed\t9.2.22\n", 0), true, pveMounts, true,
			want{detect.Yes, "9.2.22", detect.Yes, detect.Yes, nil}},
		{"PVE 9, pmxcfs down: /etc/pve left empty", out("install ok installed\t9.2.22\n", 0), true, hostMounts, true,
			want{detect.Yes, "9.2.22", detect.No, detect.Yes, nil}},
		{"Debian: no such package", hostfake.Response{Stderr: "dpkg-query: no packages found matching pve-manager\n", ExitCode: 1}, false, hostMounts, false,
			want{detect.No, "", detect.No, detect.No, nil}},
		{"Debian: removed, config left", out("deinstall ok config-files\t9.2.22\n", 0), false, hostMounts, false,
			want{detect.No, "", detect.No, detect.No, nil}},
		{"Alpine: no dpkg-query", hostfake.Response{Err: errNoDpkg}, false, hostMounts, false,
			want{detect.No, "", detect.No, detect.No, nil}},
		{"/etc/pve alone is not Proxmox", hostfake.Response{ExitCode: 1}, false, hostMounts, true,
			want{detect.No, "", detect.No, detect.Yes, nil}},
		{"pveversion alone: no version", hostfake.Response{ExitCode: 1}, true, hostMounts, false,
			want{detect.Yes, "", detect.No, detect.No, nil}},
		{"half-configured", out("install ok half-configured\t9.2.22\n", 0), false, hostMounts, false,
			want{detect.Unknown, "", detect.No, detect.No, detect.ErrUnrecognised}},
		{"dpkg database error", hostfake.Response{Stderr: "dpkg-query: error: ...\n", ExitCode: 2}, false, hostMounts, false,
			want{detect.Unknown, "", detect.No, detect.No, new(host.ExitError)}},
		{"something else at /etc/pve", hostfake.Response{ExitCode: 1}, false, hostMounts + "tmpfs /etc/pve tmpfs rw 0 0\n", true,
			want{detect.No, "", detect.Unknown, detect.Yes, detect.ErrUnrecognised}},
		{"last mount at /etc/pve wins", out("install ok installed\t9.2.22\n", 0), true, hostMounts + "tmpfs /etc/pve tmpfs rw 0 0\n/dev/fuse /etc/pve fuse.pmxcfs rw 0 0\n", true,
			want{detect.Yes, "9.2.22", detect.Yes, detect.Yes, nil}},
	}
	for _, tt := range tests {
		f := newFake(t, "")
		f.Respond(tt.dpkg, dpkgQuery[0], dpkgQuery[1:]...)
		if tt.pvebin {
			f.AddFile(detect.PVEVersionBin, nil, 0o755, 0, 0)
		}
		if tt.etcPVE {
			f.AddDir(detect.PVEDir, 0o755, 0, 0)
		}
		f.AddFile(detect.SelfMounts, []byte(tt.mounts), 0o444, 0, 0)
		got := detect.DetectProxmox(ctx, f)
		if got.Installed != tt.want.installed || got.Version != tt.want.version || got.ClusterFS != tt.want.clusterFS || got.EtcPVE != tt.want.etcPVE || !matchesErr(got.Err, tt.want.err) {
			t.Errorf("%s: DetectProxmox = installed %s %q, pmxcfs %s, /etc/pve %s, err %v; want %+v",
				tt.name, got.Installed, got.Version, got.ClusterFS, got.EtcPVE, got.Err, tt.want)
		}
		readOnly(t, f)
	}

	t.Run("mount table unreadable", func(t *testing.T) {
		f := newFake(t, "")
		f.Respond(hostfake.Response{ExitCode: 1}, dpkgQuery[0], dpkgQuery[1:]...)
		got := detect.DetectProxmox(ctx, f)
		if got.Installed != detect.No || got.ClusterFS != detect.Unknown || !matches(got.Err, fs.ErrNotExist) {
			t.Errorf("DetectProxmox = %+v, want not installed, pmxcfs unknown", got)
		}
	})
}

// matchesErr is matches, with a *host.ExitError want matched by type.
func matchesErr(err, want error) bool {
	if _, isExit := want.(*host.ExitError); isExit {
		var xe *host.ExitError
		return errors.As(err, &xe)
	}
	return matches(err, want)
}

func TestParseMounts(t *testing.T) {
	in := "/dev/vda1 / ext4 rw 0 0\nshort line\n/dev/vdb1 /srv/a\\040b ext4 rw,x\\134y 0 0\n\n"
	want := []detect.Mount{
		{Source: "/dev/vda1", Dir: "/", Type: "ext4", Options: "rw"},
		{Source: "/dev/vdb1", Dir: "/srv/a b", Type: "ext4", Options: `rw,x\y`},
	}
	if got := detect.ParseMounts([]byte(in)); !reflect.DeepEqual(got, want) {
		t.Errorf("ParseMounts = %q, want %q", got, want)
	}
}
