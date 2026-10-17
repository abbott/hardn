package reload_test

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"syscall"
	"testing"

	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/hostfake"
	"github.com/abbott/hardn/pkg/host/reload"
)

func TestReadSocketDropIns(t *testing.T) {
	h := hostfake.New(host.Options{})
	// v0.3.2's listen.conf (pkg/ssh/ssh.go at v0.3.2), port 2230.
	h.AddFile("/etc/systemd/system/ssh.socket.d/listen.conf", []byte("[Socket]\nListenStream=\nListenStream=2230\n"), 0o644, 0, 0)
	h.AddFile("/etc/systemd/system/ssh.socket.d/20-cont.conf", []byte("[Service]\nListenStream=1\n[Socket]\n; a comment\nListenStream=\\\n# skipped\n  192.0.2.10:2230\n"), 0o644, 0, 0)
	h.AddFile("/etc/systemd/system/ssh.socket.d/notes.txt", []byte("[Socket]\nListenStream=9\n"), 0o644, 0, 0)
	h.AddFile("/run/systemd/system/ssh.socket.d/10-bind.conf", []byte("[Socket]\n# ListenStream=1\nBindIPv6Only=both\n"), 0o644, 0, 0)
	h.AddSymlink("/usr/lib/systemd/system/ssh.socket.d/50-vendor.conf", "/dev/null")
	h.AddFile("/run/systemd/generator/ssh.socket.d/addresses.conf", []byte("[Socket]\nListenStream=\nListenStream=0.0.0.0:2230\n"), 0o644, 0, 0)

	d, err := reload.ReadSocketDropIns(context.Background(), h)
	if err != nil {
		t.Fatal(err)
	}
	want := reload.SocketDropIns{Read: true, Files: []reload.SocketDropIn{
		{Path: "/etc/systemd/system/ssh.socket.d/20-cont.conf", Listen: []string{"192.0.2.10:2230"}},
		{Path: "/etc/systemd/system/ssh.socket.d/listen.conf", Listen: []string{"", "2230"}},
		{Path: "/run/systemd/system/ssh.socket.d/10-bind.conf"},
		{Path: "/usr/lib/systemd/system/ssh.socket.d/50-vendor.conf", Masked: true},
	}}
	if !reflect.DeepEqual(d, want) {
		t.Errorf("ReadSocketDropIns =\n%+v\nwant\n%+v", d, want)
	}
	for _, c := range h.Calls() {
		if strings.Contains(c.Path, "generator") || (c.Op != "readdir" && c.Op != "read" && c.Op != "readlink") {
			t.Errorf("unexpected call %s", c)
		}
	}
}

// TestReadSocketDropInsFailsClosed: a directory it cannot list may hold a
// drop-in that moves the listeners.
func TestReadSocketDropInsFailsClosed(t *testing.T) {
	h := hostfake.New(host.Options{})
	h.AddFile("/etc/systemd/system/ssh.socket.d", nil, 0o644, 0, 0)
	d, err := reload.ReadSocketDropIns(context.Background(), h)
	if !errors.Is(err, syscall.ENOTDIR) || d.Read {
		t.Fatalf("ReadSocketDropIns = %+v, %v; want ENOTDIR, not Read", d, err)
	}
}

func TestOpenRCFlavour(t *testing.T) {
	gT := func(bin string) []string { return []string{bin, "-f", "/etc/ssh/sshd_config", "-G"} }
	tests := []struct {
		name  string
		setup func(*hostfake.System)
		want  string
		err   error
	}{
		{"plain", func(*hostfake.System) {}, reload.BinarySSHD, nil},
		{"pam", func(h *hostfake.System) {
			h.AddFile(reload.BinaryPAM, nil, 0o755, 0, 0)
			respond(h, hostfake.Response{Stdout: "port 22\nusepam yes\n"}, gT(reload.BinaryPAM)...)
		}, reload.BinaryPAM, nil},
		{"pam installed, UsePAM no", func(h *hostfake.System) {
			h.AddFile(reload.BinaryPAM, nil, 0o755, 0, 0)
			respond(h, hostfake.Response{Stdout: "UsePAM no\n"}, gT(reload.BinaryPAM)...)
		}, reload.BinarySSHD, nil},
		{"pam not executable", func(h *hostfake.System) { h.AddFile(reload.BinaryPAM, nil, 0o644, 0, 0) }, reload.BinarySSHD, nil},
		{"krb5 before pam, mixed case", func(h *hostfake.System) {
			h.AddFile(reload.BinaryKrb5, nil, 0o755, 0, 0)
			h.AddFile(reload.BinaryPAM, nil, 0o755, 0, 0)
			respond(h, hostfake.Response{Stdout: "GSSAPIAuthentication yes\n"}, gT(reload.BinaryKrb5)...)
		}, reload.BinaryKrb5, nil},
		{"conf.d comment", func(h *hostfake.System) {
			h.AddFile(reload.OpenRCConfd, []byte("# SSHD_BINARY=/opt/sshd\nrc_need=\"net.eth0\"\n"), 0o644, 0, 0)
		}, reload.BinarySSHD, nil},
		{"conf.d sets the binary", func(h *hostfake.System) {
			h.AddFile(reload.OpenRCConfd, []byte("SSHD_BINARY=/opt/sshd\n"), 0o644, 0, 0)
		}, "", reload.ErrConfd},
		{"conf.d sets the config file", func(h *hostfake.System) {
			h.AddFile(reload.OpenRCConfd, []byte("cfgfile=/etc/ssh/other_config\n"), 0o644, 0, 0)
		}, "", reload.ErrConfd},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h := hostfake.New(host.Options{})
			tc.setup(h)
			got, err := reload.OpenRCFlavour(context.Background(), h)
			if got != tc.want || !errors.Is(err, tc.err) || (tc.err == nil) != (err == nil) {
				t.Errorf("OpenRCFlavour = %q, %v; want %q, %v", got, err, tc.want, tc.err)
			}
		})
	}
	t.Run("-G fails", func(t *testing.T) {
		h := hostfake.New(host.Options{})
		h.AddFile(reload.BinaryPAM, nil, 0o755, 0, 0)
		respond(h, hostfake.Response{Stderr: "/etc/ssh/sshd_config line 3: Bad configuration option: Bogus\n", ExitCode: 255}, gT(reload.BinaryPAM)...)
		var xe *host.ExitError
		if _, err := reload.OpenRCFlavour(context.Background(), h); !errors.As(err, &xe) || xe.ExitCode != 255 {
			t.Errorf("err = %v, want -G's *host.ExitError", err)
		}
	})
}
