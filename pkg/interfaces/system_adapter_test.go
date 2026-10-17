package interfaces

import (
	"context"
	"errors"
	"os/exec"
	"testing"

	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/hostfake"
)

var (
	_ Commander  = OSCommander{}
	_ FileSystem = OSFileSystem{}
)

func TestSystemCommander(t *testing.T) {
	f := hostfake.New(host.Options{})
	errFixture := errors.New("fixture: id: 'alice': no such user")
	f.Respond(hostfake.Response{Stdout: "Status: active\n", Stderr: "WARN: x\n"}, "ufw", "status")
	f.Respond(hostfake.Response{Err: errFixture}, "id", "alice")
	f.Respond(hostfake.Response{Stdout: "partial\n", ExitCode: 1}, "visudo", "-c", "-f", "/tmp/x")
	var stdin []byte
	f.Handler = func(_ []string, in []byte) hostfake.Response { stdin = in; return hostfake.Response{} }
	c := SystemCommander{Sys: f}

	out, err := c.Execute("ufw", "status")
	if err != nil || string(out) != "Status: active\nWARN: x\n" {
		t.Errorf("Execute = %q, %v; want stdout then stderr, combined", out, err)
	}
	if _, err := c.Execute("id", "alice"); err != errFixture {
		t.Errorf("Execute(scripted error) = %v, want it verbatim", err)
	}
	out, err = c.Execute("visudo", "-c", "-f", "/tmp/x")
	var xe *host.ExitError
	if !errors.As(err, &xe) || xe.ExitCode != 1 || string(out) != "partial\n" {
		t.Errorf("Execute(exit 1) = %q, %v; want the output and an ExitError", out, err)
	}
	if _, err := c.ExecuteWithInput("ssh-ed25519 KEY alice\n", "su", "-", "alice", "-c", "cat >> ~/.ssh/authorized_keys"); err != nil || string(stdin) != "ssh-ed25519 KEY alice\n" {
		t.Errorf("ExecuteWithInput: stdin %q, err %v", stdin, err)
	}
}

func TestLegacyErrorUnwrapsExecError(t *testing.T) {
	inner := &exec.ExitError{}
	if got := legacyError(&host.ExitError{Argv: []string{"ufw", "enable"}, ExitCode: 1, Err: inner}); got != inner {
		t.Errorf("legacyError = %v, want the *exec.ExitError legacy callers saw before", got)
	}
	plain := errors.New("x")
	if got := legacyError(plain); got != plain {
		t.Errorf("legacyError(plain) = %v", got)
	}
}

// SystemFileSystem keeps OSFileSystem's semantics, write in place included.
func TestSystemFileSystem(t *testing.T) {
	ctx := context.Background()
	f := hostfake.New(host.Options{})
	f.AddFile("/etc/resolvconf/run/resolv.conf", []byte("nameserver 192.0.2.53\n"), 0o644, 0, 0)
	f.AddSymlink("/etc/resolv.conf", "/etc/resolvconf/run/resolv.conf")
	fsys := SystemFileSystem{Sys: f}

	if err := fsys.WriteFile("/etc/resolv.conf", []byte("nameserver 192.0.2.54\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if e, _ := f.Entry("/etc/resolvconf/run/resolv.conf"); string(e.Data) != "nameserver 192.0.2.54\n" || e.Mode != 0o644 {
		t.Errorf("write through the symlink: target %q mode %v; want the new data, mode kept", e.Data, e.Mode)
	}
	if err := fsys.WriteFile("/etc/ufw/applications.d/hardn", []byte("[x]\n"), 0o644); err != nil {
		t.Fatalf("WriteFile with a missing parent = %v (OSFileSystem creates it)", err)
	}
	if e, _ := f.Entry("/etc/ufw/applications.d"); !e.Mode.IsDir() || e.Mode.Perm() != 0o755 {
		t.Errorf("parent mode = %v, want a 0755 directory", e.Mode)
	}
	if fi, err := fsys.Stat("/etc/resolv.conf"); err != nil || fi.IsDir() {
		t.Errorf("Stat = %v, %v", fi, err)
	}
	if err := fsys.MkdirAll("/etc/sudoers.d", 0o755); err != nil {
		t.Errorf("MkdirAll = %v", err)
	}
	if err := fsys.Remove("/etc/ufw/applications.d/hardn"); err != nil {
		t.Errorf("Remove = %v", err)
	}
	if err := fsys.RemoveAll("/etc/ufw"); err != nil {
		t.Errorf("RemoveAll = %v", err)
	}
	if _, err := f.Lstat(ctx, "/etc/ufw"); err == nil {
		t.Error("/etc/ufw still there after RemoveAll")
	}
	if got, err := fsys.ReadFile("/etc/resolv.conf"); err != nil || string(got) != "nameserver 192.0.2.54\n" {
		t.Errorf("ReadFile = %q, %v", got, err)
	}
}
