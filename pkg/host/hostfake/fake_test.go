package hostfake_test

import (
	"bytes"
	"context"
	"errors"
	"io/fs"
	"reflect"
	"strings"
	"syscall"
	"testing"

	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/hostfake"
)

func TestScriptedRuns(t *testing.T) {
	ctx := context.Background()
	f := hostfake.New(host.Options{TailLines: 2})
	f.Respond(hostfake.Response{Stdout: "inactive\n", ExitCode: 3}, "systemctl", "is-active", "ssh")
	f.Respond(hostfake.Response{Stdout: "active\n"}, "systemctl", "is-active", "ssh")
	errNoUfw := errors.New("fixture: ufw not installed")
	f.Respond(hostfake.Response{Err: errNoUfw}, "ufw", "status", "verbose")
	f.Handler = func(argv []string, stdin []byte) hostfake.Response {
		if argv[0] == "sshd" {
			return hostfake.Response{Stderr: "line 1\nline 2\n/etc/ssh/sshd_config line 3: Bad configuration option: Prot\n", ExitCode: 255}
		}
		return hostfake.Response{Stdout: "handled " + strings.Join(argv, " ")}
	}

	tests := []struct {
		name     string
		argv     []string
		wantOut  string
		wantExit int
		wantTail []string
		wantErr  func(error) bool
	}{
		{"first scripted response", []string{"systemctl", "is-active", "ssh"}, "inactive\n", 3, []string{"inactive"}, isExit(3)},
		{"second response", []string{"systemctl", "is-active", "ssh"}, "active\n", 0, []string{"active"}, isNil},
		{"last response repeats", []string{"systemctl", "is-active", "ssh"}, "active\n", 0, []string{"active"}, isNil},
		{"scripted error verbatim", []string{"ufw", "status", "verbose"}, "", -1, nil, func(err error) bool { return err == errNoUfw }},
		{"handler, non-zero exit, tail", []string{"sshd", "-t"}, "", 255, []string{"line 2", "/etc/ssh/sshd_config line 3: Bad configuration option: Prot"}, isExit(255)},
		{"handler answers the rest", []string{"id", "alice"}, "handled id alice", 0, []string{"handled id alice"}, isNil},
	}
	for _, tt := range tests {
		res, err := f.Run(ctx, tt.argv[0], tt.argv[1:]...)
		if !tt.wantErr(err) || string(res.Stdout) != tt.wantOut || res.ExitCode != tt.wantExit || !reflect.DeepEqual(res.Tail, tt.wantTail) {
			t.Errorf("%s: Run(%q) = stdout %q, exit %d, tail %q, err %v; want %q, %d, %q", tt.name, tt.argv, res.Stdout, res.ExitCode, res.Tail, err, tt.wantOut, tt.wantExit, tt.wantTail)
		}
	}
}

// A check nothing scripted fails instead of passing; a Handler returning
// Response{} opts into MockCommander's empty success.
func TestUnscriptedRun(t *testing.T) {
	ctx := context.Background()
	f := hostfake.New(host.Options{})
	for _, argv := range [][]string{
		{"sshd", "-t", "-f", "/tmp/candidate/sshd_config"},
		{"systemctl", "is-active", "ssh.socket"},
		{"ufw", "status", "verbose"},
	} {
		res, err := f.Run(ctx, argv[0], argv[1:]...)
		var xe *host.ExitError
		if !errors.Is(err, hostfake.ErrUnscripted) || errors.As(err, &xe) || res.ExitCode != -1 {
			t.Errorf("unscripted Run(%q) = exit %d, %v; want ErrUnscripted, no ExitError, exit -1", argv, res.ExitCode, err)
		}
	}
	if c := f.Calls()[0]; !errors.Is(c.Err, hostfake.ErrUnscripted) || !strings.Contains(c.String(), "unscripted command") {
		t.Errorf("recorded %s; want the ErrUnscripted error", c)
	}

	lenient := hostfake.New(host.Options{})
	lenient.Handler = func([]string, []byte) hostfake.Response { return hostfake.Response{} }
	if res, err := lenient.Run(ctx, "uname", "-r"); err != nil || len(res.Combined) != 0 || res.ExitCode != 0 {
		t.Errorf("Run with an empty-success Handler = %+v, %v; want empty success, as MockCommander", res, err)
	}
}

// The fake refuses the names host.Real refuses before looking for a file.
func TestCommandNames(t *testing.T) {
	ctx := context.Background()
	f := hostfake.New(host.Options{})
	f.Respond(hostfake.Response{}, "/usr/sbin/sshd", "-t")
	f.Handler = func([]string, []byte) hostfake.Response { return hostfake.Response{} }
	for _, name := range []string{"", "./sshd", "sbin/sshd"} {
		if _, err := f.Run(ctx, name, "-t"); !errors.Is(err, host.ErrNotFound) {
			t.Errorf("Run(%q) = %v, want ErrNotFound", name, err)
		}
	}
	if _, err := f.Run(ctx, "/usr/sbin/sshd", "-t"); err != nil {
		t.Errorf("Run(absolute) = %v", err)
	}
}

// A scripted non-zero exit carries an error with os/exec's text, as host.Real's
// does, so the legacy adapter shows the same "exit status N".
func TestExitErrorText(t *testing.T) {
	f := hostfake.New(host.Options{})
	f.Respond(hostfake.Response{ExitCode: 1}, "visudo", "-c", "-f", "/tmp/hardn_sudoers_temp")
	_, err := f.Run(context.Background(), "visudo", "-c", "-f", "/tmp/hardn_sudoers_temp")
	var xe *host.ExitError
	if !errors.As(err, &xe) || xe.Err == nil || xe.Err.Error() != "exit status 1" {
		t.Fatalf("err = %v; want an ExitError whose Err reads \"exit status 1\"", err)
	}
	if got := err.Error(); got != "host: visudo -c -f /tmp/hardn_sudoers_temp: exit status 1" {
		t.Errorf("Error() = %q", got)
	}
}

func TestRunDoneContext(t *testing.T) {
	f := hostfake.New(host.Options{})
	called := false
	f.Handler = func([]string, []byte) hostfake.Response { called = true; return hostfake.Response{} }
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := f.Run(ctx, "true"); !errors.Is(err, context.Canceled) || called {
		t.Errorf("err = %v, handler called = %t; want context.Canceled and no answer", err, called)
	}
}

func TestRecording(t *testing.T) {
	ctx := context.Background()
	var log bytes.Buffer
	f := hostfake.New(host.Options{RunLog: &log})
	f.AddDir("/etc/ssh/sshd_config.d", 0o755, 0, 0)
	f.Respond(hostfake.Response{Stdout: "Rule added\n"}, "ufw", "allow", "2230/tcp")
	f.Respond(hostfake.Response{}, "visudo", "-c", "-f", "-")

	_, _ = f.Run(ctx, "ufw", "allow", "2230/tcp")
	_, _ = f.RunInput(ctx, []byte("Defaults:alice env_keep += \"HARDN_CONFIG\"\n"), "visudo", "-c", "-f", "-")
	_ = f.WriteFileAtomic(ctx, "/etc/ssh/sshd_config.d/00-hardn.conf", []byte("Port 2230\n"), 0o600, 0, 0)
	_ = f.Rename(ctx, "/etc/ssh/sshd_config.d/00-hardn.conf", "/etc/ssh/sshd_config.d/00-hardn.conf.bak")
	_ = f.Chown(ctx, "/etc/ssh/missing", 1000, -1)
	_ = f.Chmod(ctx, "/etc/ssh/sshd_config.d/00-hardn.conf.bak", 0o640)
	_, _ = f.Readlink(ctx, "/etc/ssh/sshd_config.d/00-hardn.conf.bak")
	_, _ = f.ReadFile(ctx, "/etc/ssh/sshd_config.d/00-hardn.conf.bak")

	want := []string{
		"run ufw allow 2230/tcp: exit 0",
		"run visudo -c -f - <stdin 42 bytes>: exit 0",
		"write-atomic /etc/ssh/sshd_config.d/00-hardn.conf 0600 0:0 10 bytes",
		"rename /etc/ssh/sshd_config.d/00-hardn.conf -> /etc/ssh/sshd_config.d/00-hardn.conf.bak",
		"chown /etc/ssh/missing 1000:-1: error: lchown /etc/ssh/missing: no such file or directory",
		"chmod /etc/ssh/sshd_config.d/00-hardn.conf.bak 0640",
		"readlink /etc/ssh/sshd_config.d/00-hardn.conf.bak: error: readlink /etc/ssh/sshd_config.d/00-hardn.conf.bak: invalid argument",
		"read /etc/ssh/sshd_config.d/00-hardn.conf.bak",
	}
	var got []string
	for _, c := range f.Calls() {
		got = append(got, c.String())
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("calls:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	if runs := f.Runs(); !reflect.DeepEqual(runs, [][]string{{"ufw", "allow", "2230/tcp"}, {"visudo", "-c", "-f", "-"}}) {
		t.Errorf("Runs() = %q", runs)
	}
	if c := f.Calls()[1]; string(c.Stdin) != "Defaults:alice env_keep += \"HARDN_CONFIG\"\n" {
		t.Errorf("recorded stdin = %q", c.Stdin)
	}
	wantLog := "run 1: $ ufw allow 2230/tcp\nrun 1: | Rule added\nrun 1: exit 0\n" +
		"run 2: $ visudo -c -f -\nrun 2: stdin: 42 bytes (not logged)\nrun 2: exit 0\n"
	if log.String() != wantLog {
		t.Errorf("run log:\n%s\nwant:\n%s", log.String(), wantLog)
	}
}

// A Handler models a command's effect on the host through the fake itself.
func TestHandlerMayChangeTheFake(t *testing.T) {
	ctx := context.Background()
	f := hostfake.New(host.Options{})
	f.Handler = func(argv []string, _ []byte) hostfake.Response {
		if argv[0] == "adduser" {
			f.AddDir("/home/"+argv[len(argv)-1], 0o750, 1000, 1000)
			f.AddUser(host.User{Name: argv[len(argv)-1], UID: 1000, GID: 1000, HomeDir: "/home/alice"})
		}
		return hostfake.Response{}
	}
	if _, err := f.LookupUser(ctx, "alice"); !errors.Is(err, host.ErrUnknownUser) {
		t.Fatalf("before adduser: err = %v, want ErrUnknownUser", err)
	}
	if _, err := f.Run(ctx, "adduser", "--disabled-password", "--gecos", "", "alice"); err != nil {
		t.Fatal(err)
	}
	u, err := f.LookupUser(ctx, "alice")
	fi, serr := f.Stat(ctx, "/home/alice")
	uid, gid, ok := host.Owner(fi)
	if err != nil || u.UID != 1000 || serr != nil || !ok || uid != 1000 || gid != 1000 || fi.Mode().Perm() != 0o750 {
		t.Errorf("after adduser: %+v %v; home %v %v owner %d:%d %t", u, err, fi, serr, uid, gid, ok)
	}
	if _, err := f.LookupGroup(ctx, "bob"); !errors.Is(err, host.ErrUnknownGroup) {
		t.Errorf("LookupGroup(bob) = %v, want ErrUnknownGroup", err)
	}
	f.AddGroup(host.Group{Name: "sudo", GID: 27})
	if g, err := f.LookupGroup(ctx, "sudo"); err != nil || g.GID != 27 {
		t.Errorf("LookupGroup(sudo) = %+v, %v", g, err)
	}
}

func TestFakeFileSystemDetails(t *testing.T) {
	ctx := context.Background()
	f := hostfake.New(host.Options{})
	f.AddFile("/etc/pve/priv/authorized_keys", []byte("ssh-ed25519 KEY root@web-01.example.net\n"), 0o600, 0, 33)
	f.AddSymlink("/root/.ssh/authorized_keys", "/etc/pve/priv/authorized_keys")
	f.AddSymlink("/loop/a", "/loop/b")
	f.AddSymlink("/loop/b", "a")

	if err := f.WriteFileAtomic(ctx, "/root/.ssh/authorized_keys", []byte("x"), 0o600, 0, 0); !errors.Is(err, host.ErrSymlink) {
		t.Errorf("atomic write over the PVE keys symlink = %v, want ErrSymlink", err)
	}
	e, ok := f.Entry("/etc/pve/priv/authorized_keys")
	if !ok || e.GID != 33 || e.Mode != 0o600 || !strings.HasPrefix(string(e.Data), "ssh-ed25519") {
		t.Errorf("Entry(target) = %+v, %t", e, ok)
	}
	if _, err := f.ReadFile(ctx, "/loop/a"); !errors.Is(err, syscall.ELOOP) {
		t.Errorf("symlink loop: err = %v, want ELOOP", err)
	}
	if _, err := f.ReadFile(ctx, "etc/passwd"); !errors.Is(err, fs.ErrInvalid) {
		t.Errorf("relative path: err = %v, want ErrInvalid", err)
	}
	if err := f.MkdirAll(ctx, "relative/dir", 0o755); !errors.Is(err, fs.ErrInvalid) {
		t.Errorf("relative MkdirAll: err = %v, want ErrInvalid", err)
	}
	if err := f.WriteFileAtomic(ctx, "/etc/hardn/new", []byte("x"), 0o640, 0, 0); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("atomic write without its parent = %v, want ErrNotExist", err)
	}
	if err := f.Rename(ctx, "/etc/pve", "/etc/pve/priv/inside"); !errors.Is(err, syscall.EINVAL) {
		t.Errorf("rename into itself = %v, want EINVAL", err)
	}
	// A trailing slash is refused, never cleaned away: on a host it makes
	// a regular file an error (ENOTDIR).
	if _, err := f.Stat(ctx, "/etc/pve/priv/authorized_keys/"); !errors.Is(err, fs.ErrInvalid) {
		t.Errorf("trailing slash: Stat = %v, want ErrInvalid", err)
	}
	if err := f.WriteFileAtomic(ctx, "/etc/pve/priv/authorized_keys/", []byte("x"), 0o600, 0, 0); !errors.Is(err, fs.ErrInvalid) {
		t.Errorf("trailing slash: WriteFileAtomic = %v, want ErrInvalid", err)
	}
	if err := f.MkdirAll(ctx, "/etc/hardn/", 0o755); !errors.Is(err, fs.ErrInvalid) {
		t.Errorf("trailing slash: MkdirAll = %v, want ErrInvalid", err)
	}
	if _, err := f.Stat(ctx, "/"); err != nil {
		t.Errorf("Stat(/) = %v", err)
	}
	// /proc/<pid>/ns/pid is a symlink whose content names the namespace and
	// its inode (namespaces(7)); p2-detect reads it with Readlink.
	f.AddSymlink("/proc/1/ns/pid", "pid:[4026531836]")
	if got, err := f.Readlink(ctx, "/proc/1/ns/pid"); err != nil || got != "pid:[4026531836]" {
		t.Errorf("Readlink(pidns) = %q, %v", got, err)
	}
}

func isNil(err error) bool { return err == nil }

func isExit(code int) func(error) bool {
	return func(err error) bool {
		var xe *host.ExitError
		return errors.As(err, &xe) && xe.ExitCode == code
	}
}
