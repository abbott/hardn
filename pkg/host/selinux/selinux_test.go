package selinux_test

import (
	"context"
	"errors"
	"io/fs"
	"testing"

	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/hostfake"
	"github.com/abbott/hardn/pkg/host/selinux"
)

func host1(enforce string) *hostfake.System {
	f := hostfake.New(host.Options{})
	if enforce != "" {
		f.AddFile(selinux.EnforceFile, []byte(enforce), 0o644, 0, 0)
	}
	return f
}

// TestEnforcing: "1" is Yes, "0" and no selinuxfs No, anything else
// Unknown with an error.
func TestEnforcing(t *testing.T) {
	for _, c := range []struct {
		enforce string
		want    detect.Tri
		err     bool
	}{{"1", detect.Yes, false}, {"1\n", detect.Yes, false}, {"0", detect.No, false}, {"", detect.No, false}, {"2", detect.Unknown, true}} {
		got, err := selinux.Enforcing(context.Background(), host1(c.enforce))
		if got != c.want || (err != nil) != c.err {
			t.Errorf("enforce %q: %s, %v; want %s, error %v", c.enforce, got, err, c.want, c.err)
		}
	}
}

// TestCheck: matchpathcon -V's two forms (rocky10-test-110's), a path with
// no default, and what Check makes of each; nothing asked where SELinux
// doesn't enforce.
func TestCheck(t *testing.T) {
	ctx := context.Background()
	for _, c := range []struct {
		name string
		out  hostfake.Response
		want error // nil, ErrMislabeled, ErrUnrecognised
	}{
		{"verified", hostfake.Response{Stdout: "/etc/ssh/sshd_config verified.\n/usr/local/bin/hardn verified.\n"}, nil},
		{"one mislabeled", hostfake.Response{Stdout: "/etc/ssh/sshd_config verified.\n/usr/local/bin/hardn has context unconfined_u:object_r:user_tmp_t:s0, should be system_u:object_r:bin_t:s0\n", ExitCode: 1}, selinux.ErrMislabeled},
		{"no default is not a mismatch", hostfake.Response{Stdout: "/etc/ssh/sshd_config verified.\n/usr/local/bin/hardn has context unconfined_u:object_r:user_tmp_t:s0, should be <<none>>\n"}, nil},
		{"a line in no known form", hostfake.Response{Stdout: "/etc/ssh/sshd_config verified.\nmatchpathcon(/usr/local/bin/hardn) failed: No such file or directory\n", ExitCode: 1}, detect.ErrUnrecognised},
		{"fewer answers than paths", hostfake.Response{Stdout: "/etc/ssh/sshd_config verified.\n"}, detect.ErrUnrecognised},
		{"matchpathcon missing", hostfake.Response{Err: host.ErrNotFound}, host.ErrNotFound},
	} {
		f := host1("1")
		f.Respond(c.out, "matchpathcon", "-V", "/etc/ssh/sshd_config", "/usr/local/bin/hardn")
		err := selinux.Check(ctx, f, "/etc/ssh/sshd_config", "/usr/local/bin/hardn")
		if (c.want == nil) != (err == nil) || c.want != nil && !errors.Is(err, c.want) {
			t.Errorf("%s: %v, want %v", c.name, err, c.want)
		}
	}
	for _, enforce := range []string{"0", ""} {
		f := host1(enforce)
		if err := selinux.Check(ctx, f, "/etc/ssh/sshd_config"); err != nil || len(f.Runs()) != 0 {
			t.Errorf("enforce %q: %v, ran %q", enforce, err, f.Runs())
		}
	}
	f := denied{host1("1")}
	if err := selinux.Check(ctx, f, "/x"); !errors.Is(err, fs.ErrPermission) {
		t.Errorf("selinuxfs unreadable: %v", err)
	}
}

type denied struct{ *hostfake.System }

func (denied) ReadFile(context.Context, string) ([]byte, error) { return nil, fs.ErrPermission }

// TestCheckRunnable: hardn's binary is let through as bin_t alone, by the
// type it has, whatever the policy would label its path; nothing is asked
// where SELinux doesn't enforce.
func TestCheckRunnable(t *testing.T) {
	ctx := context.Background()
	for _, c := range []struct {
		name string
		out  hostfake.Response
		want error
	}{
		{"bin_t", hostfake.Response{Stdout: "system_u:object_r:bin_t:s0\n"}, nil},
		{"admin_home_t, /root's own label", hostfake.Response{Stdout: "unconfined_u:object_r:admin_home_t:s0\n"}, selinux.ErrNotRunnable},
		{"user_tmp_t", hostfake.Response{Stdout: "unconfined_u:object_r:user_tmp_t:s0\n"}, selinux.ErrNotRunnable},
		{"no context", hostfake.Response{Stdout: "?\n"}, detect.ErrUnrecognised},
		{"stat fails", hostfake.Response{ExitCode: 1, Stderr: "stat: cannot stat\n"}, host.ErrNotFound},
	} {
		f := host1("1")
		f.Respond(c.out, "stat", "-c", "%C", "--", "/usr/local/bin/hardn")
		err := selinux.CheckRunnable(ctx, f, "/usr/local/bin/hardn")
		switch {
		case c.want == nil && err != nil, c.want != nil && err == nil:
			t.Errorf("%s: %v, want %v", c.name, err, c.want)
		case c.want != nil && c.want != host.ErrNotFound && !errors.Is(err, c.want):
			t.Errorf("%s: %v, want %v", c.name, err, c.want)
		}
	}
	f := host1("0")
	if err := selinux.CheckRunnable(ctx, f, "/root/hardn"); err != nil || len(f.Runs()) != 0 {
		t.Errorf("permissive: %v, ran %q", err, f.Runs())
	}
}
