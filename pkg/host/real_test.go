package host

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"testing"
)

// seamed is a Real whose search path is dirs and whose exec seam records the
// *exec.Cmd and calls fake instead of running it. Nothing is ever executed:
// the "executables" are empty files in t.TempDir().
type seamed struct {
	*Real
	log  *bytes.Buffer
	cmds []*exec.Cmd
}

func newSeamed(t *testing.T, opts Options, dirs []string, fake func(cmd *exec.Cmd) (int, error)) *seamed {
	t.Helper()
	s := &seamed{log: &bytes.Buffer{}}
	opts.RunLog = s.log
	s.Real = NewReal(opts)
	s.dirs = dirs
	s.exec = func(cmd *exec.Cmd) (int, error) {
		s.cmds = append(s.cmds, cmd)
		if fake == nil {
			return 0, nil
		}
		return fake(cmd)
	}
	return s
}

// touch creates an empty file at dir/name with perm and returns its path.
func touch(t *testing.T, dir, name string, perm fs.FileMode) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, nil, perm); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestRealRunArgvNeverThroughShell(t *testing.T) {
	dir := t.TempDir()
	tool := touch(t, dir, "a b;c", 0o755)
	s := newSeamed(t, Options{}, []string{dir}, nil)

	args := []string{"x y", "; rm -rf /", "$(id)", "", "*", "a\nb"}
	if _, err := s.Run(context.Background(), "a b;c", args...); err != nil {
		t.Fatal(err)
	}
	if len(s.cmds) != 1 {
		t.Fatalf("exec seam called %d times, want 1", len(s.cmds))
	}
	cmd := s.cmds[0]
	if cmd.Path != tool {
		t.Errorf("cmd.Path = %q, want %q (the name as one file name, not split)", cmd.Path, tool)
	}
	if want := append([]string{"a b;c"}, args...); !reflect.DeepEqual(cmd.Args, want) {
		t.Errorf("cmd.Args = %q, want %q (one element each)", cmd.Args, want)
	}
	if want := []string{"PATH=" + SearchPath, "LC_ALL=C"}; !reflect.DeepEqual(cmd.Env, want) {
		t.Errorf("cmd.Env = %q, want %q", cmd.Env, want)
	}
	if cmd.Dir != "/" || cmd.Stdin != nil || cmd.WaitDelay != pipeWaitDelay {
		t.Errorf("cmd.Dir = %q, cmd.Stdin = %v, cmd.WaitDelay = %v; want \"/\", nil, %v", cmd.Dir, cmd.Stdin, cmd.WaitDelay, pipeWaitDelay)
	}
}

func TestRealResolve(t *testing.T) {
	d1, d2 := t.TempDir(), t.TempDir()
	first := touch(t, d1, "both", 0o755)
	touch(t, d2, "both", 0o755)
	second := touch(t, d2, "second", 0o700)
	touch(t, d1, "plain", 0o644)
	if err := os.Mkdir(filepath.Join(d1, "adir"), 0o755); err != nil {
		t.Fatal(err)
	}
	abs := touch(t, t.TempDir(), "abs", 0o755)
	absPlain := touch(t, t.TempDir(), "absplain", 0o644)

	tests := []struct {
		name     string
		cmd      string
		wantPath string // "" means ErrNotFound and no exec
	}{
		{"first dir wins", "both", first},
		{"later dir", "second", second},
		{"missing", "missing", ""},
		{"not executable", "plain", ""},
		{"directory", "adir", ""},
		{"empty name", "", ""},
		{"relative path refused", "./both", ""},
		{"relative subdir refused", filepath.Base(d1) + "/both", ""},
		{"absolute executable", abs, abs},
		{"absolute not executable", absPlain, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := newSeamed(t, Options{}, []string{d1, d2}, nil)
			res, err := s.Run(context.Background(), tt.cmd)
			if tt.wantPath == "" {
				if !errors.Is(err, ErrNotFound) || len(s.cmds) != 0 || res.ExitCode != -1 {
					t.Fatalf("err = %v, exec calls = %d, exit = %d; want ErrNotFound, 0, -1", err, len(s.cmds), res.ExitCode)
				}
				return
			}
			if err != nil || len(s.cmds) != 1 || s.cmds[0].Path != tt.wantPath {
				t.Fatalf("err = %v, cmds = %d; want path %s", err, len(s.cmds), tt.wantPath)
			}
		})
	}
}

func TestRealRunExitCodeTailAndLog(t *testing.T) {
	dir := t.TempDir()
	touch(t, dir, "tool", 0o755)
	s := newSeamed(t, Options{TailLines: 3}, []string{dir}, func(cmd *exec.Cmd) (int, error) {
		for i := 1; i <= 3; i++ {
			fmt.Fprintf(cmd.Stdout, "out %d\n", i)
			fmt.Fprintf(cmd.Stderr, "err %d\n", i)
		}
		return 3, nil
	})

	res, err := s.Run(context.Background(), "tool", "--check")
	var xe *ExitError
	if !errors.As(err, &xe) || xe.ExitCode != 3 {
		t.Fatalf("err = %v, want *ExitError with exit 3", err)
	}
	wantTail := []string{"err 2", "out 3", "err 3"}
	if res.ExitCode != 3 || !reflect.DeepEqual(res.Tail, wantTail) || !reflect.DeepEqual(xe.Tail, wantTail) {
		t.Errorf("ExitCode = %d, Tail = %q, ExitError.Tail = %q; want 3, %q", res.ExitCode, res.Tail, xe.Tail, wantTail)
	}
	if got, want := string(res.Stdout), "out 1\nout 2\nout 3\n"; got != want {
		t.Errorf("Stdout = %q, want %q", got, want)
	}
	if got, want := string(res.Stderr), "err 1\nerr 2\nerr 3\n"; got != want {
		t.Errorf("Stderr = %q, want %q", got, want)
	}
	if got, want := string(res.Combined), "out 1\nerr 1\nout 2\nerr 2\nout 3\nerr 3\n"; got != want {
		t.Errorf("Combined = %q, want %q (write order)", got, want)
	}
	wantLog := "run 1: $ tool --check\n" +
		"run 1: | out 1\nrun 1: | err 1\nrun 1: | out 2\nrun 1: | err 2\nrun 1: | out 3\nrun 1: | err 3\n" +
		"run 1: exit 3 (host: tool --check: exit status 3)\n"
	if got := s.log.String(); got != wantLog {
		t.Errorf("run log:\n%s\nwant:\n%s", got, wantLog)
	}
}

func TestRealRunInputNotLogged(t *testing.T) {
	dir := t.TempDir()
	touch(t, dir, "chpasswd", 0o755)
	var got []byte
	s := newSeamed(t, Options{}, []string{dir}, func(cmd *exec.Cmd) (int, error) {
		var err error
		got, err = io.ReadAll(cmd.Stdin)
		return 0, err
	})
	if _, err := s.RunInput(context.Background(), []byte("alice:s3cret\n"), "chpasswd"); err != nil {
		t.Fatal(err)
	}
	if string(got) != "alice:s3cret\n" {
		t.Errorf("stdin = %q", got)
	}
	if log := s.log.String(); strings.Contains(log, "s3cret") || !strings.Contains(log, "stdin: 13 bytes (not logged)") {
		t.Errorf("run log leaks or misses stdin:\n%s", log)
	}
}

func TestRealRunErrors(t *testing.T) {
	dir := t.TempDir()
	touch(t, dir, "tool", 0o755)
	startErr := errors.New("fork/exec: resource temporarily unavailable")

	t.Run("context done before start", func(t *testing.T) {
		s := newSeamed(t, Options{}, []string{dir}, nil)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		_, err := s.Run(ctx, "tool")
		if !errors.Is(err, context.Canceled) || len(s.cmds) != 0 {
			t.Fatalf("err = %v, exec calls = %d; want context.Canceled and none", err, len(s.cmds))
		}
		if !strings.Contains(s.log.String(), "run 1: not run or not finished") {
			t.Errorf("run log has no entry for the refused run:\n%s", s.log)
		}
	})
	t.Run("context done during the run", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		// What os/exec reports once CommandContext has killed the process.
		s := newSeamed(t, Options{}, []string{dir}, func(*exec.Cmd) (int, error) {
			cancel()
			return -1, errors.New("signal: killed")
		})
		res, err := s.Run(ctx, "tool")
		var xe *ExitError
		if !errors.Is(err, context.Canceled) || errors.As(err, &xe) || res.ExitCode != -1 {
			t.Fatalf("err = %v, exit = %d; want context.Canceled, not an ExitError, and -1", err, res.ExitCode)
		}
		if !strings.Contains(s.log.String(), "run 1: not run or not finished: host: run tool: context canceled") {
			t.Errorf("run log:\n%s", s.log)
		}
	})
	t.Run("exit 0 with pipes held past the wait delay", func(t *testing.T) {
		s := newSeamed(t, Options{}, []string{dir}, func(*exec.Cmd) (int, error) { return 0, exec.ErrWaitDelay })
		if res, err := s.Run(context.Background(), "tool"); err != nil || res.ExitCode != 0 {
			t.Fatalf("err = %v, exit = %d; want success", err, res.ExitCode)
		}
	})
	t.Run("start failure", func(t *testing.T) {
		s := newSeamed(t, Options{}, []string{dir}, func(*exec.Cmd) (int, error) { return -1, startErr })
		res, err := s.Run(context.Background(), "tool")
		var xe *ExitError
		if !errors.Is(err, startErr) || errors.As(err, &xe) || res.ExitCode != -1 {
			t.Fatalf("err = %v, exit = %d; want the start error, not an ExitError, and -1", err, res.ExitCode)
		}
	})
}

func TestRealWriteFileAtomic(t *testing.T) {
	ctx := context.Background()
	r := NewReal(Options{})
	uid, gid := os.Getuid(), os.Getgid()

	t.Run("new and replaced files get the given mode", func(t *testing.T) {
		dir := t.TempDir()
		name := filepath.Join(dir, "00-hardn.conf")
		for i, perm := range []fs.FileMode{0o640, 0o600} {
			data := []byte("Port 2230\n" + strconv.Itoa(i))
			if err := r.WriteFileAtomic(ctx, name, data, perm, uid, gid); err != nil {
				t.Fatal(err)
			}
			got, err := os.ReadFile(name)
			fi, serr := os.Lstat(name)
			if err != nil || serr != nil || !bytes.Equal(got, data) || fi.Mode() != perm {
				t.Fatalf("after write %d: %q %v mode %v (%v); want %q mode %v", i, got, err, fi.Mode(), serr, data, perm)
			}
		}
		assertOnly(t, dir, "00-hardn.conf")
	})

	t.Run("refuses a symlink and leaves link and target alone", func(t *testing.T) {
		dir := t.TempDir()
		target := filepath.Join(dir, "cluster_keys")
		if err := os.WriteFile(target, []byte("ssh-ed25519 KEY root@web-01.example.net\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		for _, link := range []struct{ name, to string }{{"authorized_keys", target}, {"dangling", filepath.Join(dir, "nowhere")}} {
			name := filepath.Join(dir, link.name)
			if err := os.Symlink(link.to, name); err != nil {
				t.Fatal(err)
			}
			err := r.WriteFileAtomic(ctx, name, []byte("replaced\n"), 0o600, -1, -1)
			if !errors.Is(err, ErrSymlink) {
				t.Fatalf("%s: err = %v, want ErrSymlink", link.name, err)
			}
			if got, err := os.Readlink(name); err != nil || got != link.to {
				t.Errorf("%s: link now %q (%v), want %q", link.name, got, err, link.to)
			}
		}
		if got, _ := os.ReadFile(target); string(got) != "ssh-ed25519 KEY root@web-01.example.net\n" {
			t.Errorf("symlink target changed: %q", got)
		}
		assertOnly(t, dir, "authorized_keys", "cluster_keys", "dangling")
	})

	t.Run("refuses a directory, a missing parent and a done context", func(t *testing.T) {
		dir := t.TempDir()
		if err := r.WriteFileAtomic(ctx, dir, nil, 0o600, -1, -1); !errors.Is(err, syscall.EISDIR) {
			t.Errorf("directory: err = %v, want EISDIR", err)
		}
		if err := r.WriteFileAtomic(ctx, filepath.Join(dir, "no", "file"), nil, 0o600, -1, -1); !errors.Is(err, fs.ErrNotExist) {
			t.Errorf("missing parent: err = %v, want ErrNotExist", err)
		}
		done, cancel := context.WithCancel(ctx)
		cancel()
		if err := r.WriteFileAtomic(done, filepath.Join(dir, "f"), nil, 0o600, -1, -1); !errors.Is(err, context.Canceled) {
			t.Errorf("done context: err = %v, want context.Canceled", err)
		}
		assertOnly(t, dir)
	})
}

// A step that fails after the temporary file exists leaves no temporary
// file and the target as it was. Chowning to root fails unprivileged.
func TestRealWriteFileAtomicFailureCleansUp(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("chown to root succeeds as root; this needs an unprivileged user")
	}
	dir := t.TempDir()
	name := filepath.Join(dir, "00-hardn.conf")
	if err := os.WriteFile(name, []byte("old\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := NewReal(Options{}).WriteFileAtomic(context.Background(), name, []byte("Port 2230\n"), 0o600, 0, 0)
	if !errors.Is(err, fs.ErrPermission) {
		t.Fatalf("err = %v, want a permission error from chown", err)
	}
	assertOnly(t, dir, "00-hardn.conf")
	if got, _ := os.ReadFile(name); string(got) != "old\n" {
		t.Errorf("target now %q, want it untouched", got)
	}
}

// Chmod refuses what it would have to open to change, without opening it.
func TestRealChmodRefusesAFIFO(t *testing.T) {
	fifo := filepath.Join(t.TempDir(), "fifo")
	if err := syscall.Mkfifo(fifo, 0o600); err != nil {
		t.Skipf("mkfifo: %v", err)
	}
	if err := NewReal(Options{}).Chmod(context.Background(), fifo, 0o644); !errors.Is(err, fs.ErrInvalid) {
		t.Errorf("Chmod(fifo) = %v, want ErrInvalid", err)
	}
	if fi, err := os.Lstat(fifo); err != nil || fi.Mode().Perm() != 0o600 {
		t.Errorf("fifo mode now %v (%v), want 0600", modeOrNil(fi), err)
	}
}

func modeOrNil(fi fs.FileInfo) any {
	if fi == nil {
		return nil
	}
	return fi.Mode()
}

func TestRealChownRefusesSymlink(t *testing.T) {
	ctx := context.Background()
	r := NewReal(Options{})
	dir := t.TempDir()
	file := touch(t, dir, "authorized_keys", 0o600)
	link := filepath.Join(dir, "link")
	if err := os.Symlink(file, link); err != nil {
		t.Fatal(err)
	}
	if err := r.Chown(ctx, file, os.Getuid(), os.Getgid()); err != nil {
		t.Errorf("Chown(file) = %v", err)
	}
	if err := r.Chown(ctx, link, os.Getuid(), -1); !errors.Is(err, ErrSymlink) {
		t.Errorf("Chown(symlink) = %v, want ErrSymlink", err)
	}
}

func TestRealLookup(t *testing.T) {
	cur, err := user.Current()
	if err != nil {
		t.Skipf("no current user in this environment: %v", err)
	}
	ctx := context.Background()
	r := NewReal(Options{})
	u, err := r.LookupUser(ctx, cur.Username)
	if err != nil || u.UID != os.Getuid() || u.Name != cur.Username {
		t.Errorf("LookupUser(%q) = %+v, %v; want uid %d", cur.Username, u, err, os.Getuid())
	}
	if _, err := r.LookupUser(ctx, "hardn-no-such-user"); !errors.Is(err, ErrUnknownUser) {
		t.Errorf("LookupUser(missing) = %v, want ErrUnknownUser", err)
	}
	if g, gerr := user.LookupGroupId(cur.Gid); gerr == nil {
		got, err := r.LookupGroup(ctx, g.Name)
		if err != nil || strconv.Itoa(got.GID) != cur.Gid {
			t.Errorf("LookupGroup(%q) = %+v, %v; want gid %s", g.Name, got, err, cur.Gid)
		}
	}
	if _, err := r.LookupGroup(ctx, "hardn-no-such-group"); !errors.Is(err, ErrUnknownGroup) {
		t.Errorf("LookupGroup(missing) = %v, want ErrUnknownGroup", err)
	}
}

// assertOnly fails unless dir holds exactly names (no temporary files left).
func assertOnly(t *testing.T, dir string, names ...string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, e := range entries {
		got = append(got, e.Name())
	}
	want := append([]string(nil), names...)
	if len(want) == 0 {
		want = nil
	}
	slices.Sort(want)
	if !reflect.DeepEqual(got, want) {
		t.Errorf("%s holds %q, want %q", dir, got, want)
	}
}
