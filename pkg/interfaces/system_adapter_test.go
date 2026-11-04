package interfaces

import (
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
	c := SystemCommander{Sys: f}

	out, err := c.Execute("ufw", "status")
	if err != nil || string(out) != "Status: active\nWARN: x\n" {
		t.Errorf("Execute = %q, %v; want stdout then stderr, combined", out, err)
	}
	if _, err := c.Execute("id", "alice"); err != errFixture {
		t.Errorf("Execute(scripted error) = %v, want it verbatim", err)
	}
	out, err = c.Execute("visudo", "-c", "-f", "/tmp/x")
	if err == nil || err.Error() != "exit status 1" || string(out) != "partial\n" {
		t.Errorf("Execute(exit 1) = %q, %v; want the output and \"exit status 1\", as over host.Real", out, err)
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

// SystemFileSystem keeps OSFileSystem's semantics: a read follows a symlink.
func TestSystemFileSystem(t *testing.T) {
	f := hostfake.New(host.Options{})
	f.AddFile("/etc/resolvconf/run/resolv.conf", []byte("nameserver 192.0.2.53\n"), 0o644, 0, 0)
	f.AddSymlink("/etc/resolv.conf", "/etc/resolvconf/run/resolv.conf")
	fsys := SystemFileSystem{Sys: f}

	if fi, err := fsys.Stat("/etc/resolv.conf"); err != nil || fi.IsDir() {
		t.Errorf("Stat = %v, %v", fi, err)
	}
	if got, err := fsys.ReadFile("/etc/resolv.conf"); err != nil || string(got) != "nameserver 192.0.2.53\n" {
		t.Errorf("ReadFile = %q, %v", got, err)
	}
}
