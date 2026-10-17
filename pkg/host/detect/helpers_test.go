package detect_test

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/hostfake"
)

// osRelease returns a fixture from testdata/os-release: the files Debian's
// base-files, Ubuntu's base-files and Alpine's alpine-base generate (sources
// in docs/safety/detect.md).
func osRelease(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "os-release", name))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// newFake returns a fake with os-release laid out as the vendors ship it:
// /etc/os-release a relative link to /usr/lib/os-release.
func newFake(t *testing.T, release string) *hostfake.System {
	t.Helper()
	f := hostfake.New(host.Options{})
	if release != "" {
		f.AddFile("/usr/lib/os-release", osRelease(t, release), 0o644, 0, 0)
		f.AddSymlink("/etc/os-release", "../usr/lib/os-release")
	}
	return f
}

// denied is a fake whose reads of the listed paths fail with EACCES, as an
// unprivileged read of /proc/1/environ does; the fake itself acts as root.
type denied struct {
	*hostfake.System
	paths map[string]bool
}

func deny(f *hostfake.System, paths ...string) denied {
	d := denied{f, map[string]bool{}}
	for _, p := range paths {
		d.paths[p] = true
	}
	return d
}

func (d denied) err(op, name string) error {
	if d.paths[name] {
		return &fs.PathError{Op: op, Path: name, Err: fs.ErrPermission}
	}
	return nil
}

func (d denied) ReadFile(ctx context.Context, name string) ([]byte, error) {
	if err := d.err("open", name); err != nil {
		return nil, err
	}
	return d.System.ReadFile(ctx, name)
}

func (d denied) Stat(ctx context.Context, name string) (fs.FileInfo, error) {
	if err := d.err("stat", name); err != nil {
		return nil, err
	}
	return d.System.Stat(ctx, name)
}

func (d denied) Readlink(ctx context.Context, name string) (string, error) {
	if err := d.err("readlink", name); err != nil {
		return "", err
	}
	return d.System.Readlink(ctx, name)
}

// readOnly fails t when f recorded anything but a read: detection writes
// nothing.
func readOnly(t *testing.T, f *hostfake.System) {
	t.Helper()
	for _, c := range f.Calls() {
		switch c.Op {
		case "read", "stat", "lstat", "readlink", "readdir", "run":
		default:
			t.Errorf("detection made a %s call: %v", c.Op, c)
		}
	}
}

func out(stdout string, exit int) hostfake.Response {
	return hostfake.Response{Stdout: stdout, ExitCode: exit}
}
